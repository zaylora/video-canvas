# Agent 模型 Gemini 接口格式（PR1）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 后台 Agent 模型可以选「接口格式」（OpenAI 兼容 / Gemini 原生）；Gemini 格式下由 Go 网关把 pi 发来的 OpenAI 请求翻译成 Gemini `streamGenerateContent`，再把响应翻译回 OpenAI SSE 行，pi 与计费无感。

**Architecture:** `modelcfg.ChannelRef` 加 `api_format`，快照带出到 `llmgateway.Target.Format`。`llmgateway` 新增 Gemini 翻译（请求体、端点、请求头、逐块响应翻译），翻译后的 OpenAI 行交给**现有** `accumulator` 与 `raw` 回调，用量、取消、断流、空闲重试、非流 JSON 全部复用。OpenAI 路径零行为变化。

**Tech Stack:** Go 1.x（backend/，标准库 `net/http`、`encoding/json`、`httptest`），React + TypeScript（web/，Vitest）。

**Spec:** `docs/design/Agent视频理解/Agent视频理解.md`（§4 现状、§5 部分一、§7 安全、§8 PR1 测试、§9 PR1 拆分）；界面以 `docs/design/Agent视频理解/后台模型配置-原型.html` 为准。

## Global Constraints

- 只动 PR1 范围：**不改** pi / Node（`agent/`）、`tool_decls.json`、计费口径、限流、SSRF 规则、拉取模型、OpenAI 格式的任何行为（spec §5.6）。
- `api_format` 取值只能是 `""`（等于 `openai`）、`"openai"`、`"gemini"`；只有 `kind = agent` 的模型能写（spec §5.2）。
- Gemini 端点：`{base_url}/v1beta/models/{upstream_model}:streamGenerateContent?alt=sse`；请求头同时写 `x-goog-api-key: <key>` 与 `Authorization: Bearer <key>`；渠道自定义头先写，覆盖不了这两个及 `Content-Type`、`Accept`（spec §5.3）。
- 合成的 tool call id 形如 `gc_<8 位 [a-z0-9]>` 或 `gc_<8 位>_<base64url 无填充(thoughtSignature)>`，只含 `[A-Za-z0-9_-]`，**绝不含 `|`**（spec §5.3、§4.2）。
- 留存继续记 `SanitizeRaw` 后的 **OpenAI 形状**请求体，不记翻译后的 Gemini 体；摘要 `Params` 里加 `api_format`（spec §5.5）。
- 后端测试放 `backend/internal/tests/<包路径>/`（项目约定，不放在源码目录），上游一律 `httptest` 假服务；前端测试放 `web/src/tests/`。
- 代码注释和用户可见文案用中文，风格对齐所在文件；错误给用户看的话要是人话。
- 验证命令：后端在 `backend/` 下 `go vet ./...`、`go test ./internal/tests/...`（先定向）；前端在 `web/` 下按 `CLAUDE.md` 的 bun 绕法跑 `bun node_modules/vitest/vitest.mjs run <文件>`、`bun node_modules/oxfmt/bin/oxfmt <文件>`、`bun node_modules/oxlint/bin/oxlint src`、`bun node_modules/typescript/bin/tsc -b --noEmit`（实际命令以 `CLAUDE.md` 为准）。
- 每个任务一个提交，提交信息用 Conventional Commits 中文描述，结尾带 `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`；不 push。

## Review Focus

- **tool 结果里 `tool_call_id` 在历史中找不到对应 assistant 调用**（pi 压缩上下文后可能出现）：应返回 `ErrInvalidRequest` 的人话错误，而不是 panic 或发出缺 `name` 的 `functionResponse`。→ Task 3 测试 `TestToGemini_ToolResultWithoutCall`。
- **一轮里 Gemini 返回多个 `functionCall`、且只有第一个带 `thoughtSignature`**：每个调用 id 唯一，签名只挂在第一个，下一轮带回时位置不变。→ Task 4 测试 `TestFromGemini_ParallelCallsSignatureOnFirst`。
- **流里先给 `usageMetadata`、最后一块没有 `finishReason`，随后连接正常关闭**：仍要补 `finish_reason` 与 `[DONE]`，不能被当成断流（否则 pi 报错、计费按估算）。→ Task 4 测试 `TestFromGemini_EOFWithoutFinishReason`。
- **assistant 的 `arguments` 是空串**（pi 对无参工具会发 `""` 或 `"{}"`）：应当翻译成 `args: {}`，不是请求不合法。→ Task 3 测试 `TestToGemini_EmptyArguments`。
- **工具 schema 里 `anyOf` 分支的 `type` 不同（`at` 字段）**：清洗后是合法的单一 `type:"string"`，且 Go 端能解析 `"3.5"` 这样的数字字符串。→ Task 3 测试 `TestCleanSchema_MixedAnyOfBecomesString` 与 Task 6 测试 `TestParseFrameAt_NumericString`。

---

## File Structure

| 文件 | 职责 |
| --- | --- |
| `backend/internal/provider/modelcfg/types.go` | `ChannelRef.APIFormat` 字段与格式常量 |
| `backend/internal/provider/modelcfg/validate.go` | `api_format` 校验 |
| `backend/internal/provider/modelcfg/jsonschema.go` | `channelRefSchema()` 加字段 |
| `backend/internal/provider/snapshot.go` | `ModelSnapshot.APIFormat` |
| `backend/internal/llmgateway/types.go` | `Target.Format` 与格式常量 |
| `backend/internal/llmgateway/gemini_request.go`（新） | OpenAI 请求体 → Gemini 请求体（含 id 解码签名） |
| `backend/internal/llmgateway/gemini_schema.go`（新） | 工具 parameters 清洗 |
| `backend/internal/llmgateway/gemini_response.go`（新） | Gemini SSE / JSON → OpenAI SSE 行（含 id 合成、签名编码） |
| `backend/internal/llmgateway/gateway.go` | 按格式选端点、请求头、请求体翻译与响应翻译 |
| `backend/internal/service/agent/platform_model.go` | `Target.Format` 取自快照 |
| `backend/internal/service/agent/observe_request.go` | 留存摘要带 `api_format` |
| `backend/internal/service/agent/video.go` | `parseFrameAt` 接受数字字符串 |
| `web/src/utils/admin/model-body.ts` | `readModelApiFormat` / `withModelApiFormat` |
| `web/src/pages/admin/ai/models/model-basic-form.tsx` | 「接口格式」下拉 |
| `web/src/pages/admin/ai/models/model-fields.ts` | 错误路径映射 |

---

### Task 1: 模型配置加 `api_format`（校验、Schema、快照）

**Files:**
- Modify: `backend/internal/provider/modelcfg/types.go:57-62`（`ChannelRef`）
- Modify: `backend/internal/provider/modelcfg/validate.go`（`validateChannels` 附近，约 180-205 行）
- Modify: `backend/internal/provider/modelcfg/jsonschema.go:151-163`（`channelRefSchema()`）
- Modify: `backend/internal/provider/snapshot.go:20-31,115-126`（`ModelSnapshot` 与 `NewSnapshot`）
- Test: `backend/internal/tests/provider/modelcfg/agent_model_test.go`（追加）、`backend/internal/tests/provider/modelcfg/jsonschema_test.go`（已有一致性测试应继续通过）

**Interfaces:**
- Produces:
  - `modelcfg.APIFormatOpenAI = "openai"`、`modelcfg.APIFormatGemini = "gemini"`
  - `ChannelRef.APIFormat string \`json:"api_format,omitempty"\``
  - `func (c ChannelRef) Format() string`：空串返回 `"openai"`，其余原样
  - `provider.ModelSnapshot.APIFormat string \`json:"api_format,omitempty"\``，`NewSnapshot` 从 `cfg.Channels[0].Format()` 填入（为 `openai` 时写空串，保持老快照 JSON 不变）

- [ ] **Step 1: 写失败测试**（追加到 `agent_model_test.go`，用该文件已有的构造 Agent 配置的辅助函数）

```go
func TestAgentModel_APIFormat(t *testing.T) {
	cases := []struct {
		name, kind, format string
		wantErrPath        string // 空表示应通过
	}{
		{"agent 默认", "agent", "", ""},
		{"agent openai", "agent", "openai", ""},
		{"agent gemini", "agent", "gemini", ""},
		{"agent 非法值", "agent", "claude", "channels[0].api_format"},
		{"非 agent 不能写", "text", "gemini", "channels[0].api_format"},
	}
	// 每个 case：构造合法模型 → 设置 Channels[0].APIFormat → 校验，
	// wantErrPath 为空时断言无错误，否则断言错误列表里有这个路径。
}

func TestChannelRef_FormatDefaultsToOpenAI(t *testing.T) {
	// ChannelRef{}.Format() == "openai"；ChannelRef{APIFormat: "gemini"}.Format() == "gemini"
}
```

- [ ] **Step 2: 跑测试，确认失败**

Run: `cd backend && go test ./internal/tests/provider/modelcfg/ -run 'TestAgentModel_APIFormat|TestChannelRef_Format' -v`
Expected: 编译失败（`APIFormat` 未定义）

- [ ] **Step 3: 实现**

在 `types.go` 加常量、字段、`Format()`；在 `validate.go` 校验：非 agent 且 `APIFormat != ""` 报「只有 Agent 模型可以选择接口格式」，agent 时取值不在 `openai/gemini` 报「接口格式只能是 openai 或 gemini」，路径 `channels[i].api_format`；`jsonschema.go` 的 `channelRefSchema()` 加 `"api_format": {"type":"string","enum":["openai","gemini"],"description":"接口格式（仅 agent），不填等于 openai"}`；`snapshot.go` 加字段并在 `NewSnapshot` 赋值。

- [ ] **Step 4: 跑测试，确认通过**

Run: `cd backend && go test ./internal/tests/provider/... -v -run 'APIFormat|Format|JSONSchema'` 然后 `go test ./internal/tests/provider/...`
Expected: PASS（含 `TestJSONSchema_与ParseModel一致`）

- [ ] **Step 5: 提交**

```bash
git add backend/internal/provider backend/internal/tests/provider
git commit -m "feat(modelcfg): Agent 模型新增接口格式 api_format"
```

---

### Task 2: 网关按格式选端点与请求头

**Files:**
- Modify: `backend/internal/llmgateway/types.go:84-94`（`Target`）
- Modify: `backend/internal/llmgateway/gateway.go:144-170`（`do`）
- Create: `backend/internal/llmgateway/gemini_request.go`（本任务只放 `geminiEndpoint`）
- Test: `backend/internal/tests/llmgateway/gemini_wire_test.go`（新）

**Interfaces:**
- Produces:
  - `llmgateway.FormatOpenAI = "openai"`、`llmgateway.FormatGemini = "gemini"`
  - `Target.Format string`（空等于 openai）
  - `func geminiEndpoint(base *url.URL, model string) string`：返回 `strings.TrimRight(base.String(), "/") + "/v1beta/models/" + url.PathEscape(model) + ":streamGenerateContent?alt=sse"`
  - `do` 在 Gemini 格式下写 `x-goog-api-key` 与 `Authorization: Bearer`（两者在渠道自定义头之后写）

- [ ] **Step 1: 写失败测试**（假上游记录收到的路径、query 与头，回一个最小 Gemini SSE：一块 `{"candidates":[{"content":{"parts":[{"text":"好"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1}}`）

```go
func TestGemini_EndpointAndAuthHeaders(t *testing.T) {
	// Target{Format:"gemini", BaseURL: srv.URL+"/antigravity", APIKey:"k-123", UpstreamModel:"gemini-3.8-flash",
	//        Headers: map[string]string{"x-goog-api-key":"evil","Authorization":"evil","X-Custom":"1"}}
	// StreamRaw 一个最小 OpenAI 体 {"messages":[{"role":"user","content":"hi"}]}
	// 断言：r.URL.Path == "/antigravity/v1beta/models/gemini-3.8-flash:streamGenerateContent"
	//       r.URL.Query().Get("alt") == "sse"
	//       头 x-goog-api-key == "k-123"，Authorization == "Bearer k-123"，X-Custom == "1"
	//       请求体里没有 "messages" 字段（已翻译，见 Task 3；本任务先断言路径和头，体的断言放 Task 3）
}

func TestOpenAI_EndpointUnchanged(t *testing.T) {
	// Format 为 "" 时路径仍是 /v1/chat/completions，Authorization 为 Bearer，且没有 x-goog-api-key 头
}
```

- [ ] **Step 2: 跑测试，确认失败**

Run: `cd backend && go test ./internal/tests/llmgateway/ -run 'TestGemini_EndpointAndAuthHeaders|TestOpenAI_EndpointUnchanged' -v`
Expected: FAIL（`Format` 未定义）

- [ ] **Step 3: 实现**

`types.go` 加常量与字段；`gateway.go` 的 `do` 按 `tg.Format` 选端点；Gemini 时在现有 `Authorization` 之后再 `Set("x-goog-api-key", tg.APIKey)`。`redactErr` 已按 Key 字符串脱敏，无需改。本任务**响应仍按 OpenAI 解析**，所以测试只断言请求侧；Task 4 接上响应翻译。

- [ ] **Step 4: 跑测试，确认通过；全包回归**

Run: `cd backend && go test ./internal/tests/llmgateway/ -v -run 'Endpoint' && go test ./internal/tests/llmgateway/`
Expected: PASS；原有测试全绿

- [ ] **Step 5: 提交**

```bash
git add backend/internal/llmgateway backend/internal/tests/llmgateway
git commit -m "feat(llmgateway): 按接口格式选择 Gemini 端点与鉴权头"
```

---

### Task 3: OpenAI 请求体 → Gemini 请求体（含 schema 清洗）

**Files:**
- Modify: `backend/internal/llmgateway/gemini_request.go`
- Create: `backend/internal/llmgateway/gemini_schema.go`
- Modify: `backend/internal/llmgateway/gateway.go`（`StreamRaw` 与 `Stream`：`SanitizeRaw` / `buildBody` 得到 OpenAI 体后，Gemini 格式再调 `ToGemini`）
- Test: `backend/internal/tests/llmgateway/gemini_request_test.go`（新）、`backend/internal/tests/llmgateway/gemini_schema_test.go`（新）

**Interfaces:**
- Consumes: Task 2 的 `Target.Format`、`FormatGemini`
- Produces:
  - `func ToGemini(openaiBody []byte) ([]byte, error)`（导出，测试与观测可用）：输入是 `SanitizeRaw` / `buildBody` 的输出；错误包 `ErrInvalidRequest`
  - `func CleanSchemaForGemini(schema json.RawMessage) json.RawMessage`（导出）
  - `func decodeCallID(id string) (signature string)`：`gc_xxxxxxxx_<b64>` 解出签名，其他形式返回空串（供 Task 4 的 `encodeCallID` 对称使用）

**翻译规则**：按 spec §5.3「请求翻译」表逐行实现（system → `systemInstruction`；user 文本 / `image_url` https → `fileData{mimeType,fileUri}`，`data:` → `inlineData{mimeType,data}`，https 的 mimeType 按扩展名推断，推不出用 `image/jpeg`；assistant → `role:"model"`，`tool_calls` → `functionCall{name,args}`，id 里有签名则同一 part 加 `thoughtSignature`；连续 `tool` 消息 → 一条 user，`functionResponse{name,response:{content}}`，name 按 `tool_call_id` 反查；紧跟的、首个 text 段等于 `Attached image(s) from tool result:` 的 user 消息，把图片 part 并入上一条 functionResponse 所在内容；相邻同角色合并；`tools` → `functionDeclarations` 且 parameters 过 `CleanSchemaForGemini`；`tool_choice`；`max_tokens/temperature/top_p/stop` → `generationConfig`）。

**schema 清洗规则**（spec §5.4，递归处理对象与数组）：
1. `anyOf` 的所有分支都是同 `type` 且各自只有 `const`（可带 description）→ `{type, enum:[…]}`；分支有 `enum` 的一并收集。
2. `anyOf` 分支 `type` 不同 → `{type:"string"}`，原 description 后追加「（也可以写秒数，例如 "3.5"）」——只有一处用到（`at`），措辞按此固定。
3. 删除 `patternProperties`、`additionalProperties`、`$schema`、`const`（未被规则 1 吃掉的）。
4. 删除 `minItems`/`maxItems`/`minimum`/`maximum`，并把范围追加进 description：数组写「（1 到 6 项）」样式，数值写「（0 到 600）」样式；只有下限或上限时写「（至少 N）」「（最多 N）」。

- [ ] **Step 1: 写失败测试**（表驱动，输入是 OpenAI JSON 字符串，断言输出 JSON 的关键路径；用 `encoding/json` 解成 `map[string]any` 后比较，不要比较整串）

```go
func TestToGemini_SystemAndUser(t *testing.T)                // systemInstruction.parts[0].text；contents[0].role=="user"
func TestToGemini_Images(t *testing.T)                       // https .png → fileData{mimeType:"image/png"}；无扩展名 → image/jpeg；data: → inlineData 拆出 mime 与 data
func TestToGemini_ToolCallsAndResults(t *testing.T)          // 同轮两个 tool_calls → model 两个 functionCall；随后两条 tool → 一条 user 两个 functionResponse，name 正确
func TestToGemini_ToolResultImagesMerged(t *testing.T)       // tool + "Attached image(s) from tool result:" user → 只有一条 user，含 functionResponse 与 fileData/inlineData
func TestToGemini_SignatureFromCallID(t *testing.T)          // id "gc_ab12cd34_" + base64url("sig") → functionCall part 带 thoughtSignature:"sig"；普通 id 不带
func TestToGemini_AdjacentSameRoleMerged(t *testing.T)       // 两条连续 user → 一条，parts 顺序保留
func TestToGemini_GenerationConfigAndToolChoice(t *testing.T) // max_tokens→maxOutputTokens；stop 字符串和数组都转 stopSequences；tool_choice "none"→NONE；{"type":"function","function":{"name":"x"}}→ANY+allowedFunctionNames["x"]
func TestToGemini_ToolResultWithoutCall(t *testing.T)        // tool_call_id 找不到 → errors.Is(err, ErrInvalidRequest)
func TestToGemini_EmptyArguments(t *testing.T)               // arguments "" 与 "{}" → args 为空对象；"{bad" → ErrInvalidRequest
func TestToGemini_NoMessagesKeyInOutput(t *testing.T)        // 输出里没有 messages / model / stream / stream_options

func TestCleanSchema_ConstAnyOfBecomesEnum(t *testing.T)     // plan_update.status 的 anyOf+const → {type:"string",enum:["todo","doing","done"]}
func TestCleanSchema_MixedAnyOfBecomesString(t *testing.T)   // at 的 anyOf(string enum, number minimum) → type "string"，description 以「（也可以写秒数，例如 "3.5"）」结尾
func TestCleanSchema_DropsUnsupportedKeywords(t *testing.T)  // patternProperties/additionalProperties 被删；minItems:1,maxItems:6 → description 含「（1 到 6 项）」
func TestCleanSchema_AllAgentToolsAreClean(t *testing.T)     // 读 backend/internal/service/agent/tool_decls.json 全部工具，清洗后递归不含 anyOf/const/patternProperties/additionalProperties/minItems/maxItems/minimum/maximum/$schema
```

- [ ] **Step 2: 跑测试，确认失败**

Run: `cd backend && go test ./internal/tests/llmgateway/ -run 'TestToGemini|TestCleanSchema' -v`
Expected: 编译失败（`ToGemini` 未定义）

- [ ] **Step 3: 实现 `ToGemini`、`CleanSchemaForGemini`、`decodeCallID`，并在 `StreamRaw` / `Stream` 里接上**（Gemini 格式时，把 `SanitizeRaw` / `buildBody` 的结果再过 `ToGemini` 后交给 `run`；`SanitizeRaw` 本身不改，它仍是留存的来源）

- [ ] **Step 4: 跑测试，确认通过；Task 2 的 `TestGemini_EndpointAndAuthHeaders` 补上「请求体有 contents、没有 messages」的断言并通过；全包回归**

Run: `cd backend && go test ./internal/tests/llmgateway/`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/llmgateway backend/internal/tests/llmgateway
git commit -m "feat(llmgateway): OpenAI 请求翻译为 Gemini 请求并清洗工具 schema"
```

---

### Task 4: Gemini 响应 → OpenAI SSE 行

**Files:**
- Create: `backend/internal/llmgateway/gemini_response.go`
- Modify: `backend/internal/llmgateway/gateway.go`（`do`：Gemini 格式时把响应体包一层翻译后再交给 `readSSE` / `readWhole`）
- Test: `backend/internal/tests/llmgateway/gemini_response_test.go`（新）

**Interfaces:**
- Consumes: Task 3 的 `decodeCallID`（对称）
- Produces:
  - `func encodeCallID(signature string) string`：生成 `gc_<8 位 [a-z0-9]>`，签名非空时追加 `_` + `base64.RawURLEncoding` 编码
  - `func newGeminiSSEReader(r io.Reader) io.Reader`：读 Gemini SSE（`data: {...}` 行），产出 OpenAI SSE 字节流（`data: {...}\n\n`，结尾 `data: [DONE]\n\n`）；`readSSE` 直接读它，现有 `raw`、`touch`、`accumulator`、`ErrStreamTruncated` 判定全部复用
  - `func geminiWholeToOpenAI(body []byte) ([]byte, error)`：非流 JSON（`Content-Type: application/json`）→ 一个 OpenAI 整段响应（`choices[0].message`、`finish_reason`、`usage`），交给现有 `readWhole`

**翻译规则**：按 spec §5.3「响应翻译」表实现。要点：
- 每个 Gemini 块的 `parts[].text`（`thought != true`）→ 一行 `delta.content`；`thought == true` → `delta.reasoning_content`。
- `functionCall` → `delta.tool_calls[{index, id: encodeCallID(part.thoughtSignature), type:"function", function:{name, arguments: JSON(args)}}]`，`index` 在整个响应里从 0 递增。
- 结束：读到 `finishReason` 或上游 EOF 时，产出一行 `finish_reason`（出现过 functionCall → `tool_calls`；`STOP`→`stop`；`MAX_TOKENS`→`length`；`SAFETY`/`RECITATION`/`BLOCKLIST`/`PROHIBITED_CONTENT`/`SPII`/其他 → `content_filter`），再一行 `{"choices":[],"usage":{...}}`（取最后一次 `usageMetadata`：`prompt_tokens=promptTokenCount`，`completion_tokens=candidatesTokenCount+thoughtsTokenCount`，`prompt_tokens_details.cached_tokens=cachedContentTokenCount`；从未收到 usage 时不写这一行，让现有逻辑标 `UsageMissing`），最后 `data: [DONE]`。
- 上游**一块都没收到就 EOF**：不补 `[DONE]`，原样 EOF，让现有逻辑报 `ErrStreamTruncated`。
- `promptFeedback.blockReason` 且无 candidates：产出 `finish_reason: "content_filter"`，`content` 为「请求被上游安全策略拦截（<blockReason>）」。

- [ ] **Step 1: 写失败测试**（直接对 `newGeminiSSEReader` 喂字符串、读出全部行断言；再加两个经 `StreamRaw` 的端到端测试）

```go
func TestFromGemini_TextStream(t *testing.T)                    // 两块 text → 两行 delta.content，然后 finish_reason "stop"、usage、[DONE]
func TestFromGemini_ThoughtBecomesReasoning(t *testing.T)       // thought:true → reasoning_content
func TestFromGemini_FunctionCall(t *testing.T)                  // functionCall → tool_calls[0]，id 匹配 ^gc_[a-z0-9]{8}(_[A-Za-z0-9_-]+)?$ 且不含 "|"；finish_reason "tool_calls"
func TestFromGemini_ParallelCallsSignatureOnFirst(t *testing.T) // 两个 functionCall，只有第一个带 thoughtSignature → 两个 id 不同；decodeCallID(id0)=="sig"，decodeCallID(id1)==""
func TestFromGemini_FinishReasonMapping(t *testing.T)           // 表驱动：STOP/MAX_TOKENS/SAFETY/RECITATION/OTHER
func TestFromGemini_UsageMapping(t *testing.T)                  // prompt 10、candidates 4、thoughts 2、cached 3 → 10 / 6 / 3
func TestFromGemini_EOFWithoutFinishReason(t *testing.T)        // 有 text 与 usage、无 finishReason、正常 EOF → 仍有 finish_reason "stop" 与 [DONE]
func TestFromGemini_EmptyStreamIsTruncated(t *testing.T)        // 经 StreamRaw：上游 200 立即关闭 → errors.Is(err, ErrStreamTruncated)
func TestFromGemini_PromptBlocked(t *testing.T)                 // promptFeedback.blockReason "SAFETY" → content_filter 与人话 content
func TestFromGemini_WholeJSON(t *testing.T)                     // 经 StreamRaw：上游回 application/json 整段 → raw 收到合成的 SSE，Result.Text 与 Usage 正确
func TestGemini_EndToEndMatchesOpenAIResult(t *testing.T)       // 同一请求打 OpenAI 假上游与 Gemini 假上游（内容等价），两边 Result 的 Text、ToolCalls[].Name/Arguments、FinishReason、Usage 相同
func TestGemini_SignatureRoundTrip(t *testing.T)                // 第 1 轮 Gemini 返回带签名的 functionCall → 取 raw 里的 id 拼第 2 轮 OpenAI 请求 → Gemini 假上游收到的 functionCall part 带同一 thoughtSignature
```

- [ ] **Step 2: 跑测试，确认失败**

Run: `cd backend && go test ./internal/tests/llmgateway/ -run 'TestFromGemini|TestGemini_' -v`
Expected: FAIL / 编译失败

- [ ] **Step 3: 实现 `encodeCallID`、`newGeminiSSEReader`、`geminiWholeToOpenAI`，并在 `do` 里按格式接上**（`touch` 仍按底层读到字节刷新，空闲超时与重试行为不变）

- [ ] **Step 4: 跑测试，确认通过；全包回归（含 `errors_test.go` 的 SSRF、不跟随重定向、错误脱敏，对 Gemini 格式各补一个用例：`TestGemini_BlocksInternalAddresses`、`TestGemini_DoesNotFollowRedirects`、`TestGemini_ErrorBodyRedactsKey`）**

Run: `cd backend && go test -race ./internal/tests/llmgateway/`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/llmgateway backend/internal/tests/llmgateway
git commit -m "feat(llmgateway): Gemini 流式与整段响应翻译为 OpenAI SSE"
```

---

### Task 5: 模型代理接上格式，留存记录格式

**Files:**
- Modify: `backend/internal/service/agent/platform_model.go:63-64`（构造 `Target`）
- Modify: `backend/internal/service/agent/observe_request.go:33-80`（`BuildModelRequestRecord` 的 `Params`）与 `:135`（`recordModelRequest` 签名）
- Test: `backend/internal/tests/service/agent/bridge_test.go`（追加）、现有留存测试文件（用 `grep -rn BuildModelRequestRecord backend/internal/tests` 找到）

**Interfaces:**
- Consumes: Task 1 的 `ModelSnapshot.APIFormat`，Task 2 的 `Target.Format`
- Produces: `BuildModelRequestRecord(callID, runID uint64, sent []byte, apiFormat string, now time.Time)`（在现有参数里加 `apiFormat`；为空时 `Params` 不写这个键，老记录形状不变）；`recordModelRequest(ctx, runID, callID uint64, sent []byte, apiFormat string)`

- [ ] **Step 1: 写失败测试**

```go
func TestProxyModelPlatform_PassesGeminiFormat(t *testing.T) // 快照 APIFormat "gemini" → 假 Streamer 收到的 Target.Format == "gemini"；为空时 Target.Format == ""
func TestBuildModelRequestRecord_IncludesAPIFormat(t *testing.T) // apiFormat "gemini" → Params 里 "api_format":"gemini"；为空时 Params 没有这个键；Body 仍是 OpenAI 形状（含 messages）
```

- [ ] **Step 2: 跑测试，确认失败**

Run: `cd backend && go test ./internal/tests/service/agent/ -run 'PassesGeminiFormat|IncludesAPIFormat' -v`
Expected: FAIL

- [ ] **Step 3: 实现**（`Target{..., Format: snap.Model.APIFormat}`；留存仍用 `SanitizeRaw(body, tg, lim)` 的结果，不调用 `ToGemini`）

- [ ] **Step 4: 跑测试，确认通过；agent 包回归**

Run: `cd backend && go test ./internal/tests/service/agent/`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/service/agent backend/internal/tests/service/agent
git commit -m "feat(agent): 模型代理按配置使用 Gemini 格式并在留存里记录"
```

---

### Task 6: `parseFrameAt` 接受数字字符串

**Files:**
- Modify: `backend/internal/service/agent/video.go:187-206`
- Test: 现有 `parseFrameAt` / 截帧测试所在文件（`grep -rn "parseFrameAt\|capture_frame" backend/internal/tests/service/agent` 找到），追加用例

**Interfaces:**
- Produces: `parseFrameAt` 行为扩展：字符串若能 `strconv.ParseFloat` 成有限非负数，按秒数返回；`"-1"` 报「at 不能为负数」；`"abc"` 仍报原错误文案

- [ ] **Step 1: 写失败测试**

```go
func TestParseFrameAt_NumericString(t *testing.T) {
	// `"3.5"` → kind "", sec 3.5, err nil
	// `"0"`   → sec 0
	// `"-1"`  → err 文案 "at 不能为负数"
	// `"abc"` → err 文案 `at 只能是 "first"、"last" 或秒数`
	// `"first"`、`3.5`（数字）行为不变
}
```

（`parseFrameAt` 未导出；若测试包访问不到，经 `canvas_capture_frame` 工具的公开入口测：`frames:[{"at":"3.5"}]` 与 `frames:[{"at":3.5}]` 截到同一时刻。按现有测试文件已有的方式选一种。）

- [ ] **Step 2: 跑测试，确认失败** — Run: `cd backend && go test ./internal/tests/service/agent/ -run 'FrameAt|CaptureFrame' -v`，Expected: FAIL
- [ ] **Step 3: 实现**（在 `case string:` 分支里，first/last 之后尝试 `strconv.ParseFloat(strings.TrimSpace(x), 64)`，复用数字分支的 NaN/Inf/负数判断）
- [ ] **Step 4: 跑测试，确认通过** — Run 同上，Expected: PASS
- [ ] **Step 5: 提交**

```bash
git add backend/internal/service/agent/video.go backend/internal/tests/service/agent
git commit -m "fix(agent): 截帧时刻 at 接受数字字符串（Gemini 清洗后的 schema 是字符串）"
```

---

### Task 7: 后台表单「接口格式」下拉

**Files:**
- Modify: `web/src/utils/admin/model-body.ts`（加读写函数）
- Modify: `web/src/pages/admin/ai/models/model-basic-form.tsx:110-159`（「模型身份」区块，`model-upstream` 字段之后）
- Modify: `web/src/pages/admin/ai/models/model-fields.ts:18-43`（路径映射）
- Modify: `web/src/api/admin/ai/type.d.ts` 或模型正文相关类型（若 `ChannelRef` 有 TS 类型，加 `api_format?: "openai" | "gemini"`）
- Test: `web/src/tests/utils/admin/model-body.test.ts`、`web/src/tests/pages/admin/ai/models/model-fields.test.ts`（追加）；新增 `web/src/tests/pages/admin/ai/models/model-basic-form.test.tsx`（若已有同类组件测试的写法可参照 `model-params-form.test.tsx`）

**Interfaces:**
- Consumes: Task 1 的字段名 `channels[0].api_format` 与取值
- Produces:
  - `export function readModelApiFormat(body: unknown): "openai" | "gemini"`：没有或非法值返回 `"openai"`
  - `export function withModelApiFormat(body: unknown, format: "openai" | "gemini"): Record<string, unknown>`：`openai` 时**删除**该键（保持老配置干净），`gemini` 时写入；其他字段原样
  - `fieldOfPath("channels[0].api_format")` → `{ tab: "basic", id: "model-api-format" }`

**界面（照原型 `后台模型配置-原型.html` 基本信息页签）**：
- 只在 `kind === "agent"` 时显示，位于「模型身份」网格里「模型 Logo」之后（同一 `md:grid-cols-2` 网格的下一格）。
- 标签「接口格式」，控件用项目现有 `Select`，id `model-api-format`，选项「OpenAI 兼容」（openai）、「Gemini 原生」（gemini）。
- 下方提示（用 `FormField` 的 hint 写法），文案固定：
  - openai：`请求 {base}/v1/chat/completions，鉴权 Authorization: Bearer。`
  - gemini：`请求 {base}/v1beta/models/{上游模型}:streamGenerateContent?alt=sse，鉴权 x-goog-api-key 与 Authorization: Bearer。渠道地址要指向 Gemini 原生接口的根路径（可能带专用前缀）。`
- 从 agent 切到别的能力时，`withModelKind` 的现有重置逻辑之外，确保 `channels[0].api_format` 被删除（非 agent 不能有这个字段，否则保存会被 Task 1 的校验拦住）。

- [ ] **Step 1: 写失败测试**

```ts
// model-body.test.ts
it("readModelApiFormat 默认 openai", () => { expect(readModelApiFormat({ channels: [{}] })).toBe("openai"); });
it("readModelApiFormat 读出 gemini", () => { expect(readModelApiFormat({ channels: [{ api_format: "gemini" }] })).toBe("gemini"); });
it("withModelApiFormat openai 删除键、gemini 写入", () => {
  const g = withModelApiFormat({ channels: [{ channel: "c", upstream_model: "m" }] }, "gemini");
  expect(g.channels).toEqual([{ channel: "c", upstream_model: "m", api_format: "gemini" }]);
  expect(withModelApiFormat(g, "openai").channels).toEqual([{ channel: "c", upstream_model: "m" }]);
});
// 切换能力：agent + gemini 的正文 → withModelKind(body, "text") 后 channels[0] 没有 api_format

// model-fields.test.ts
it("api_format 映射到基本信息", () => {
  expect(fieldOfPath("channels[0].api_format")).toEqual({ tab: "basic", id: "model-api-format" });
});

// model-basic-form.test.tsx
// kind=agent 时渲染 label「接口格式」，切到「Gemini 原生」后 onChange 得到的正文 channels[0].api_format === "gemini"，提示里含 "streamGenerateContent"；
// kind=text 时不渲染「接口格式」
```

- [ ] **Step 2: 跑测试，确认失败** — Run: `cd web && bun node_modules/vitest/vitest.mjs run src/tests/utils/admin/model-body.test.ts src/tests/pages/admin/ai/models/`，Expected: FAIL
- [ ] **Step 3: 实现**（辅助函数、表单下拉与提示、路径映射、切换能力时删键）
- [ ] **Step 4: 跑测试、格式化、lint、类型检查**

Run（在 `web/`）：上面的 vitest 命令；`bun node_modules/oxfmt/bin/oxfmt <改过的文件>`；`bun node_modules/oxlint/bin/oxlint src`；`bun node_modules/typescript/bin/tsc -b --noEmit`
Expected: 测试 PASS，lint 无新增告警，tsc 无错误

- [ ] **Step 5: 提交**

```bash
git add web/src
git commit -m "feat(admin): Agent 模型表单新增接口格式选择"
```

---

### Task 8: 全量验证与说明

**Files:**
- Modify: `docs/design/Agent视频理解/Agent视频理解.md`（文末新增「实现记录（PR1）」小节：完成了什么、与设计的偏差、未验证项）

- [ ] **Step 1: 后端全量** — Run: `cd backend && go vet ./... && go test -race ./...`（需要 PostgreSQL / Redis 的集成测试若本地跑不了，按 `CLAUDE.md` 说明跳过的方式跑，并在实现记录里写明哪些没跑）。Expected: 通过
- [ ] **Step 2: 前端全量** — Run（`web/`）：vitest 全量、oxlint、tsc（命令同 Task 7）。Expected: 通过
- [ ] **Step 3: 写实现记录**：列出新增 / 修改的文件、`ToGemini` 与响应翻译覆盖的规则、和 spec 不一致的地方（若有）、仍未验证的项（thought signature 真机、生产 Gemini 服务的鉴权头、清洗后 schema 被生产接受）
- [ ] **Step 4: 提交**

```bash
git add docs/design/Agent视频理解/Agent视频理解.md
git commit -m "docs(agent): 记录 Gemini 接口格式 PR1 的实现情况"
```

真机验证（对话 + 多轮工具调用 + 看图，用生产 Gemini 渠道）不在本计划的自动步骤里，由人工在合入前完成（spec §8 真机）。
