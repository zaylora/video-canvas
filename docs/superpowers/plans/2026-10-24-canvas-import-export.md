# 画布导入 / 导出 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 画布内一键导出 `.canvas.json`，在另一个账号的画布列表导入成一张与原画布完全隔离的新画布，素材按内容哈希在后端复制、角色卡内嵌新建。

**Architecture:** 新增纯 Go 包 `internal/canvasdoc`（文档类型、校验、导出改写、导入还原，无 gorm/gin 依赖，表驱动可测）；`AssetService` 增加哈希补算与按哈希复制；新增 `CanvasTransferService` 编排导出 / 导入（导入时先复制文件、再在一个仓储事务里写素材 + 角色卡 + 画布）；前端只做「保存→请求→下载」和「选文件→预检→请求→结果」，流程逻辑抽成可注入依赖的纯函数以便单测。

**Tech Stack:** Go 1.27 + Gin + GORM + PostgreSQL（标准库 `crypto/sha256`、`encoding/json`）；React + TypeScript，测试 `bun test`，格式化 oxfmt / oxlint。不新增任何依赖。

**Spec:** `docs/design/画布导入导出/画布导入导出.md`（准绳）；演示 `docs/design/画布导入导出/画布导入导出-演示.html`（含 JSON 字段浏览器和导出字段对照表）。

## Global Constraints

- 文件识别：`format` 固定 `"video-canvas/canvas"`；`version` 当前 `1`，高于当前拒绝（错误码 `30006`），格式不对 `30005`，超限 `30007`。
- 文件后缀 `.canvas.json`；导入画布标题 = 原标题末尾加 `（导入）`，原标题超过 60 字截断前部。
- 素材引用：JSON 里只写 SHA-256（64 位小写十六进制），不写 `/files/` 地址、素材数字 ID、`user_id`、画布 ID、`revision`、任务 ID、角色卡十六进制 ID、`viewport`。
- 不带：`taskId`、`pendingTaskIds`、`errorTaskRef`、`labelCache`、`src`、`activeOutputId`；`status: running` 导出为 `idle`；`outputs` 只保留当前选中的一版。
- 节点 / 连线 / 分组 ID 原样保留；不认识的字段原样透传。
- 导入素材：在存储里**复制**，新 `storage_key`，`user_id` = 导入者，`source = "upload"`，`task_id` 为空；元信息以后端查到的素材为准，不信 JSON。找不到的素材：节点保留、素材位置置空、记入报告。
- 角色卡：内容内嵌；导入时给导入者新建；重名（不区分大小写）改名为 `名字 (2)`、`名字 (3)`…（≤64 字）；主图找不到则不建卡；不进资产库文件夹。
- 上限（示意默认值，集中在 `canvasdoc.DefaultLimits()`）：文件 10 MB、节点 2000、素材 200 个、素材总字节 1 GB。
- 新画布 `revision = 1`，`viewport` 写 `{x:0,y:0,zoom:1}`；成功响应走 `response.OK`（规则 A6，所以是 HTTP 200，不是设计稿里写的 201）。
- 后端：分层规则 A1–A9，service 方法按 R6 写中文步骤注释，事务只经仓储 `WithTx`，用户数据一律带 `user_id`，测试放 `backend/internal/tests/<包路径>/` 且只用标准库 `testing`；结构变更必须配迁移（规则 15–19）。
- 前端：测试放 `web/src/tests/`；格式化 / lint 只用 `bun node_modules/oxfmt/bin/oxfmt <文件>` 和 `bun node_modules/oxlint/bin/oxlint src`；界面改动按 `.agents/skills/ui-design/SKILL.md`，沿用 `components/ui` 现有组件。
- 不用 Playwright；不在 worktree 里做，直接在当前分支改；提交信息 Conventional Commits（type 后半角冒号），末尾带 `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`；**提交只在用户同意执行计划后进行**。

## Review Focus

1. 手改过的文件：哈希大写 / 长度不对 / 引用了 `assets` 里没有的哈希 / `nodes` 里混进非对象 → 返回 `30005`，绝不 panic、不 500。（Task 3）
2. 同一个素材同时被节点主素材、`outputs`、`params.images`、`paramAssets`、角色卡主图用 → 只复制一次，所有引用指向同一个新素材 ID。（Task 5、7）
3. 空画布 / 只有分组 / 没有角色卡 → `assets`、`characters` 输出 `{}` 而不是 `null`，导入后画布能正常打开。（Task 4、5）
4. 同一个文件连续导入两次 → 两张独立画布，角色卡依次改名 `(2)` `(3)`，名字恰好 64 字时截断后不撞唯一索引。（Task 7）
5. 导出时某个素材的存储文件已读不到 → 导出仍成功，该素材被跳过并计数，对应节点不留素材 ID。（Task 4、6）

---

## 文件结构

| 文件 | 职责 |
| --- | --- |
| `backend/migrations/0012_asset_sha256.sql`、`0013_asset_sha256_index.notx.sql` | 加列、并发建索引 |
| `backend/internal/model/asset.go` | `Asset.Sha256` |
| `backend/internal/repository/asset.go` | `FindBySha256`、`SetSha256IfEmpty` |
| `backend/internal/service/asset.go`、新增 `asset_transfer.go` | ingest 算哈希；`EnsureSha256`、`CopyBySha256`、`DiscardCopies` |
| `backend/internal/canvasdoc/`（`document.go`、`refs.go`、`export.go`、`restore.go`） | 纯函数：解析校验、收集引用、导出改写、导入还原 |
| `backend/internal/service/canvas_transfer.go` | `CanvasTransferService.Export / Import` |
| `backend/internal/repository/canvas_transfer.go` | 导入事务 `WithTx` |
| `backend/internal/pkg/errcode/errcode.go` | `30005 / 30006 / 30007` |
| `backend/internal/handler/canvas_transfer.go`、`router/router.go`、`initialize/app.go`、`backend/README.md` | 接口、路由、装配、接口表 |
| `web/src/api/canvas/`、`web/src/utils/canvas/transfer-file.ts`、`export-flow.ts`、`import-flow.ts` | API 映射、文件名 / 下载 / 预检、两个流程函数 |
| `web/src/pages/canvas/chrome/top-left-bar.tsx` | 菜单「导出画布」 |
| `web/src/components/home/import-canvas-button.tsx`、`import-result-dialog.tsx`、`web/src/hooks/use-canvas-import.ts`、`web/src/pages/home/canvas-list/index.tsx` | 列表页导入 |

---

### Task 1: `assets.sha256` 列、索引与仓储方法

**Files:**
- Modify: `backend/internal/model/asset.go`（`Asset` 加字段）
- Create: `backend/migrations/0012_asset_sha256.sql`、`backend/migrations/0013_asset_sha256_index.notx.sql`
- Modify: `backend/internal/repository/asset.go`
- Test: `backend/internal/tests/repository/asset_test.go`（沿用该文件的建库方式）；`backend/internal/tests/migration/schema_test.go` 不改，只运行

**Interfaces:**
- Produces: `model.Asset.Sha256 string`（`gorm:"size:64;not null;default:'';index:idx_assets_sha256" json:"-"`，空串 = 还没算）；`func (r *AssetRepository) FindBySha256(ctx, sha string, byteSize int64) ([]model.Asset, error)`（按 `id` 升序，最多 5 条，不带用户条件，查不到返回空切片）；`func (r *AssetRepository) SetSha256IfEmpty(ctx, id uint64, sha string) error`（`UPDATE … WHERE id = ? AND sha256 = ''`，幂等，行不存在不算错）。

- [ ] **Step 1: 写失败测试** 在 `tests/repository/asset_test.go` 加 `TestAssetRepository_FindBySha256`：建三条素材（A、B 同哈希同大小，C 同哈希不同大小），断言 `FindBySha256(h, size)` 返回 `[A.ID, B.ID]` 且按 id 升序；哈希不存在返回长度 0。加 `TestAssetRepository_SetSha256IfEmpty`：第一次写入生效，第二次用别的哈希调用后库里仍是第一次的值；对不存在的 id 调用返回 nil。
- [ ] **Step 2: 运行确认失败** `cd backend && go test ./internal/tests/repository/ -run 'Sha256' -v`，预期编译失败（方法 / 字段未定义）。
- [ ] **Step 3: 实现** 模型加字段；`0012` 里 `ALTER TABLE assets ADD COLUMN sha256 varchar(64) NOT NULL DEFAULT '';`；`0013`（一条语句，幂等）`CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_assets_sha256 ON assets USING btree (sha256);`（assets 可能是大表，遵循规则 19）；两个仓储方法按上面签名实现并写仓储注释（查询条件、哨兵错误）。
- [ ] **Step 4: 运行确认通过** 上一条命令 + `go test ./internal/tests/migration/... -v`（结构一致性测试必须仍然通过）。
- [ ] **Step 5: 提交** `feat(asset): assets 新增 sha256 列及按哈希查询`

### Task 2: 上传时算哈希；`EnsureSha256`、`CopyBySha256`、`DiscardCopies`

**Files:**
- Modify: `backend/internal/service/asset.go`（`AssetRepo` 接口加两个方法；`ingest` 写入 `Sha256`）
- Create: `backend/internal/service/asset_transfer.go`
- Modify: 所有实现了 `service.AssetRepo` 的测试假实现（`grep -rn "GetByStorageKey" backend/internal/tests` 找全）补上两个新方法
- Test: `backend/internal/tests/service/asset_test.go`（用现有 `newAssetTestService` / `fakeAssetStore`）

**Interfaces:**
- Consumes: Task 1 的 `FindBySha256`、`SetSha256IfEmpty`。
- Produces（均在 `*AssetService` 上）：
  - `EnsureSha256(ctx, userID, assetID uint64) (string, *model.Asset, error)`：按 `user_id` 取素材；`Sha256` 为空则从素材所在存储读出、算哈希、`SetSha256IfEmpty` 写回。素材不存在或不属于该用户、或文件读不到，都返回 `ErrAssetUnreadable`（包级 `errors.New`）。
  - `CopyBySha256(ctx, userID uint64, sha string, byteSize int64) (*model.Asset, error)`：`FindBySha256` 取候选，依次尝试读源文件，第一个读得通的复制到**当前默认存储**的新 key（`u{userID}/{yyyymm}/{uuid}{扩展名}`，与 `ingest` 同格式），返回**尚未入库**的 `*model.Asset`（`UserID`=导入者、`StorageID`/`StorageKey`=新位置、`Kind/MimeType/ByteSize/Width/Height/DurationMs/FileName/Sha256` 取自源、`Source="upload"`、`TaskID=nil`）。没有可用候选返回 `ErrAssetNotFoundByHash`。
  - `DiscardCopies(ctx, assets []*model.Asset)`：对每个 `StorageKey` 调用已有的 `cleanupObject`，用于事务失败回收。
  - `FileURL(key string) string` 已存在，后续任务复用。

- [ ] **Step 1: 写失败测试**（`tests/service/asset_test.go`）：
  - `TestAssetService_Upload_StoresSha256`：上传已知字节，断言返回素材的 `Sha256` 等于 `hex(sha256(字节))`。
  - `TestAssetService_EnsureSha256_BackfillsOnce`：库里一条 `Sha256==""` 的素材、存储里有字节 → 返回正确哈希，且 fake repo 里该行已写回；再调用一次不再读存储（fake store 记录 Open 次数为 1）。
  - `TestAssetService_EnsureSha256_Unreadable`：存储里没有该对象 → `errors.Is(err, ErrAssetUnreadable)`；别的用户的素材 → 同样返回 `ErrAssetUnreadable`。
  - `TestAssetService_CopyBySha256_CopiesForNewUser`：用户 1 的素材，用户 2 调用 → 返回素材 `UserID==2`、`StorageKey != 源 key`、存储里新 key 的字节与源一致、源对象仍在；`Source=="upload"`。
  - `TestAssetService_CopyBySha256_SkipsUnreadableCandidate`：两个同哈希候选，第一个文件不存在 → 用第二个复制成功。
  - `TestAssetService_CopyBySha256_NotFound`：哈希不存在 / 大小对不上 → `ErrAssetNotFoundByHash`。
  - `TestAssetService_DiscardCopies_RemovesObjects`。
- [ ] **Step 2: 运行确认失败** `go test ./internal/tests/service/ -run 'Sha256|DiscardCopies' -v`。
- [ ] **Step 3: 实现** `ingest` 在 `n == 0` 校验之后对临时文件整体算 SHA-256（`io.Copy` 到 `sha256.New()` 用 `io.NewSectionReader(f, 0, n)`，算完再 `Seek` 回头部）；三个新方法写在 `asset_transfer.go`，R6 步骤注释写清为什么复制而不共用（`storage_key` 全局唯一）、为什么跨用户查询（拿到 JSON 即可认领，设计 §11.1-5）、写回幂等所以不需要 singleflight。
- [ ] **Step 4: 运行确认通过** `go test ./internal/tests/service/ ./internal/tests/handler/ -v`（假实现改动不能破坏其他测试）。
- [ ] **Step 5: 提交** `feat(asset): 上传时计算 SHA-256，支持补算与按哈希复制素材`

### Task 3: `canvasdoc` 文档类型、`Parse`、`Validate`

**Files:**
- Create: `backend/internal/canvasdoc/document.go`
- Test: `backend/internal/tests/canvasdoc/document_test.go`

**Interfaces:**
- Produces（包 `canvasdoc`）：
  - 常量 `Format = "video-canvas/canvas"`、`CurrentVersion = 1`。
  - 类型：`Document{Format string; Version int; ExportedAt, AppVersion string; Canvas CanvasPart; Assets map[string]AssetInfo; Characters map[string]CharacterInfo}`；`CanvasPart{Title string; Nodes []map[string]any; Edges []map[string]any}`；`AssetInfo{Kind, MimeType string; ByteSize int64; Width, Height int; DurationMs int64; FileName string}`；`CharacterInfo{Name, Description, Avatar string; Extras []string; Voice *string}`（JSON 字段名按设计 §7：`format`、`version`、`exportedAt`、`appVersion`、`canvas.{title,nodes,edges}`、`assets`、`characters`，素材元信息 `kind/mimeType/byteSize/width/height/durationMs/fileName`）。
  - `Limits{MaxBytes int64; MaxNodes, MaxAssets int; MaxAssetBytes int64}`、`DefaultLimits() Limits`（10 MB / 2000 / 200 / 1 GB）。
  - 哨兵错误 `ErrInvalid`、`ErrTooNew`、`ErrLimit`（具体原因用 `fmt.Errorf("%w: …")` 包装，原因文案中文，可直接给用户看）。
  - `Parse(raw []byte) (*Document, error)`：用 `json.Decoder.UseNumber()` 解码，保证数字（如 `createdAt` 毫秒时间戳）原样往返；`format` 不对 / 不是对象 → `ErrInvalid`；`version` 大于 `CurrentVersion` → `ErrTooNew`；`Assets` / `Characters` 为 null 时规整成空 map。
  - `(*Document).Validate(l Limits) error`。

- [ ] **Step 1: 写失败测试**（表驱动，外部测试包 `canvasdoc_test`）：
  - `TestParse`：缺 `format` / `format` 写错 / 顶层不是对象 → `errors.Is(err, ErrInvalid)`；`version: 2` → `ErrTooNew`；`version: 1` 且 `createdAt: 1761300000000` 解析后重新 `json.Marshal` 仍是 `1761300000000`（无科学计数法）。
  - `TestValidate`：合法文档通过；`assets` 键为大写 hex / 63 位 / 含非 hex → `ErrInvalid`；节点 `data.assetHash` 引用了 `assets` 里没有的哈希 → `ErrInvalid`；`characters[c1].avatar` 缺失或不在 `assets` 里 → `ErrInvalid`；节点 `characterRef` 指向不存在的角色卡 → `ErrInvalid`；`nodes` 里混入字符串元素 → `ErrInvalid`；节点数超 `MaxNodes`、素材数超 `MaxAssets`、`assets` 的 `byteSize` 之和超 `MaxAssetBytes` → `ErrLimit`；`Assets` 为空 map 的空画布通过。引用位置包括 `data.assetHash`、`data.outputs[].assetHash`、`data.params.images/videos/audios[]`、`data.paramAssets` 的键、`data.scene.thumbHash`（五处都要各有一条用例）。
- [ ] **Step 2: 运行确认失败** `go test ./internal/tests/canvasdoc/ -v`。
- [ ] **Step 3: 实现** `document.go`；校验逻辑里「文档中出现的哈希引用」由一个包内函数 `hashRefs(node map[string]any) []string` 统一取出，Task 4 / 5 复用它。
- [ ] **Step 4: 运行确认通过。**
- [ ] **Step 5: 提交** `feat(canvas): 新增 canvasdoc 导入导出文档格式与校验`

### Task 4: `CollectRefs` 与 `Build`（导出改写）

**Files:**
- Create: `backend/internal/canvasdoc/refs.go`、`backend/internal/canvasdoc/export.go`
- Test: `backend/internal/tests/canvasdoc/export_test.go`

**Interfaces:**
- Consumes: Task 3 的类型。
- Produces：
  - `Refs{AssetIDs, CharacterIDs []string}`（去重、按出现顺序；素材 ID 是十进制串，角色卡 ID 是十六进制串）；`CollectRefs(payload []byte) (Refs, error)`：从 `payload_json` 的节点里取 `data.assetId`、`data.outputs[].assetId`、`data.params.images/videos/audios[]`、`data.paramAssets` 的键、`data.scene.thumb.assetId`、`data.characterId`。
  - `AssetEntry{Hash string; Info AssetInfo}`、`CharacterEntry{Name, Description, AvatarAssetID string; ExtraAssetIDs []string; VoiceAssetID string}`（`VoiceAssetID` 空串表示没有）。
  - `ExportInput{Title string; Payload []byte; Assets map[string]AssetEntry /*键=素材 ID*/; Characters map[string]CharacterEntry /*键=角色卡十六进制 ID*/; AppVersion string; Now time.Time}`。
  - `ExportReport{SkippedAssets int}`；`Build(in ExportInput) (*Document, ExportReport, error)`。改写规则严格按设计 §7.4：`assetId→assetHash`、删 `src`、`outputs` 只留 `activeOutputId` 对应那一版（`assetHash/mediaType/model/createdAt`）、删 `activeOutputId`、`params.images|videos|audios` 的 ID→哈希、`paramAssets` 的键→哈希且值去掉 `url`、`scene.thumb`→`scene.thumbHash`、`characterId→characterRef`（`c1`、`c2` 按首次出现编号，卡信息进 `Characters`）、`running→idle`、删 `taskId/pendingTaskIds/errorTaskRef/labelCache`、整个丢弃 `viewport`。`Assets` 里不存在的素材 ID（读不到）：该节点对应素材字段整体去掉，`SkippedAssets` 按不同素材 ID 计数；`Characters` 里不存在的角色卡：去掉 `characterId`，不产生 `characterRef`。输出的 `Assets` / `Characters` 恒为非 nil map。

- [ ] **Step 1: 写失败测试**（`export_test.go`，夹具用易辨认的素材 ID 如 `"90417"`，且至少一个节点同时覆盖所有引用位置）：
  - `TestCollectRefs_FindsEveryAssetField`：断言返回的 `AssetIDs` 恰好是夹具里全部素材 ID（去重、顺序稳定）和 `CharacterIDs`。
  - `TestBuild_ReplacesAssetIDsWithHashes`：断言 `assetHash`、`params.images`、`paramAssets` 的键、`outputs[0].assetHash` 都是哈希。
  - `TestBuild_NoLeak`：`json.Marshal(doc)` 后的字符串中不含 `"/files/"`、不含任何原素材 ID（带引号）、不含 `"taskId"`、`"revision"`、`"viewport"`、`"src"`、`"activeOutputId"`、`"errorTaskRef"`、`"labelCache"`。
  - `TestBuild_KeepsOnlyActiveOutput`：三版产物只剩被选中的那一版，且字段只有 `assetHash/mediaType/model/createdAt`。
  - `TestBuild_RunningBecomesIdle`、`TestBuild_KeepsUnknownFieldsAndIDs`（自定义字段 `x-extra` 原样保留；节点 / 连线 / 分组 ID、`parentId`、提示词里的 `@[名字](n2)` 不变）。
  - `TestBuild_UnresolvedAssetIsSkippedAndCounted`：某素材 ID 不在 `Assets` → 节点不含 `assetHash`/`outputs`，`SkippedAssets==1`，文档里不含该 ID。
  - `TestBuild_EmbedsCharacter`：角色卡按 `c1` 编号，`avatar/extras/voice` 是哈希，且这些哈希都在 `doc.Assets`；已被删除的卡对应节点无 `characterRef`。
  - `TestBuild_EmptyCanvas`：`nodes` 为空 → `Assets`、`Characters` 为非 nil 空 map，序列化后是 `{}`。
- [ ] **Step 2: 运行确认失败。**
- [ ] **Step 3: 实现。** 全程用 `map[string]any` 变换并保持其余字段透传；`payload` 用 `UseNumber` 解码。`AppVersion`、`Now` 由调用方传入（便于测试）。
- [ ] **Step 4: 运行确认通过。**
- [ ] **Step 5: 提交** `feat(canvas): canvasdoc 支持把画布内容改写成导出文档`

### Task 5: `Restore`（导入还原）

**Files:**
- Create: `backend/internal/canvasdoc/restore.go`
- Test: `backend/internal/tests/canvasdoc/restore_test.go`

**Interfaces:**
- Consumes: Task 3 的 `Document`、`hashRefs`。
- Produces：`RestoredAsset{ID, URL string}`（ID 为十进制串）；`RestoreInput{Doc *Document; Assets map[string]RestoredAsset /*键=哈希*/; Characters map[string]string /*键=文档里的 c1，值=新卡的十六进制 ID*/}`；`MissingAsset{NodeID, FileName string}`、`InvalidCharacter{NodeID, Name string}`；`RestoreReport{MissingAssets []MissingAsset; InvalidCharacters []InvalidCharacter; EdgesDropped int}`；`Restore(in RestoreInput) (payload []byte, rep RestoreReport, err error)`。返回的 payload 形如 `{"nodes":[…],"edges":[…],"viewport":{"x":0,"y":0,"zoom":1}}`，是 `Build` 的逆向：`assetHash→assetId`+`src`、`outputs[0]` 补 `id`（= 新素材 ID）和 `src`/`assetId` 并设 `activeOutputId` 同值、`params` 数组与 `paramAssets` 键换回新 ID 并按新素材重建 `paramAssets[*].url`、`thumbHash→thumb{assetId,url}`、`characterRef→characterId`。哈希不在 `Assets` 里：去掉该节点的相关素材字段并记 `MissingAssets`（`FileName` 取 `doc.Assets[hash].FileName`）；`characterRef` 不在 `Characters` 里：去掉并记 `InvalidCharacters`；`source`/`target` 指向不存在节点的连线丢弃并计 `EdgesDropped`。节点和连线其余字段、顺序原样；分组节点原样。

- [ ] **Step 1: 写失败测试：**
  - `TestRestore_RoundTripWithBuild`：用 Task 4 的夹具先 `Build` 再 `Restore`（映射到新 ID / 新 URL），断言：`assetId`、`src`、`outputs[0].id == activeOutputId == 新 ID`、`params.images`、`paramAssets` 键与 `url`、`characterId` 都是新值；提示词、节点 ID、连线 ID 与原夹具逐字相同；`viewport` 为默认值。
  - `TestRestore_MissingAssetKeepsNode`：映射里缺一个哈希 → 节点仍在、无 `assetId/src/outputs`，报告含对应 `NodeID` 和 `FileName`；其他节点不受影响。
  - `TestRestore_SameHashUsedEverywhereMapsToOneID`：主素材 / outputs / params / 角色卡引用同一哈希 → 所有位置得到同一个新 ID。
  - `TestRestore_DropsDanglingEdges`、`TestRestore_InvalidCharacterRef`（节点保留，无 `characterId`，报告含节点和角色名）、`TestRestore_EmptyCanvas`（`nodes` / `edges` 序列化为 `[]` 而不是 `null`）。
- [ ] **Step 2: 运行确认失败。**
- [ ] **Step 3: 实现。**
- [ ] **Step 4: 运行确认通过。**
- [ ] **Step 5: 提交** `feat(canvas): canvasdoc 支持把导入文档还原成画布内容`

### Task 6: `CanvasTransferService.Export`

**Files:**
- Create: `backend/internal/service/canvas_transfer.go`
- Modify: `backend/internal/pkg/errcode/errcode.go`（本任务不加码，只确认 `ErrCanvasNotFound` 复用）
- Test: `backend/internal/tests/service/canvas_transfer_test.go`

**Interfaces:**
- Consumes: Task 2 的 `EnsureSha256`；Task 4 的 `CollectRefs`、`Build`；现有 `CanvasProjectRepo`、`CharacterCardRepo.ListByIDs`、`idcodec.Decode`。
- Produces：在 `service` 包声明依赖接口 `TransferAssets`（`EnsureSha256`、`CopyBySha256`、`DiscardCopies`、`FileURL`）、`TransferCharacters`（`ListByIDs(ctx, userID uint64, ids []uint64) ([]model.CharacterCard, error)`）；`CanvasTransferDeps{Canvases CanvasProjectRepo; Assets TransferAssets; Characters TransferCharacters; Tx TransferRepo; AppVersion string; Limits canvasdoc.Limits; Now func() time.Time}`；`NewCanvasTransferService(d CanvasTransferDeps) *CanvasTransferService`；`ExportResult{Document *canvasdoc.Document \`json:"document"\`; Report ExportReport \`json:"report"\`}`、`ExportReport{SkippedAssets int \`json:"skipped_assets"\`}`；`(*CanvasTransferService).Export(ctx, userID, id uint64) (*ExportResult, error)`。`TransferRepo`（`WithTx`）在 Task 7 定义，本任务测试用的 `CanvasTransferDeps` 不设置它。

- [ ] **Step 1: 写失败测试**（fake canvas repo / assets / characters，表驱动）：
  - `TestCanvasTransfer_Export_Success`：画布 payload 含图片节点（`assetId:"101"`）与角色卡节点 → 返回文档的 `assets` 恰为这些素材的哈希，`characters["c1"]` 的名字、设定正确，`report.skipped_assets == 0`。
  - `TestCanvasTransfer_Export_OtherUsersCanvas`：用户 2 导出用户 1 的画布 → `errcode` 码 `30001`（比较 `Code`，不比指针）。
  - `TestCanvasTransfer_Export_UnreadableAssetIsSkipped`：`EnsureSha256` 对 `"101"` 返回 `ErrAssetUnreadable` → 导出成功，`skipped_assets == 1`，文档里没有 `"101"`。
  - `TestCanvasTransfer_Export_DeletedCharacter`：节点引用的角色卡查不到 → 节点无 `characterRef`，`characters` 为空 map。
  - `TestCanvasTransfer_Export_CharacterAssetsAreHashed`：角色卡的主图 / 补充图 / 声音都经 `EnsureSha256`，且出现在 `assets` 里。
- [ ] **Step 2: 运行确认失败。**
- [ ] **Step 3: 实现 `Export`**，R6 步骤注释：1 取画布（不存在或不属于该用户 → `ErrCanvasNotFound`）；2 `CollectRefs`；3 十六进制角色卡 ID 用 `idcodec.Decode`，解不开的忽略，批量 `ListByIDs`；4 把角色卡的素材 ID 并入待处理集合，逐个 `EnsureSha256`，`ErrAssetUnreadable` 的不放进 `Assets`（说明原因：导出不能因个别素材失败而整体失败）；5 `canvasdoc.Build`，其他错误（payload 不是合法 JSON 等）原样返回。
- [ ] **Step 4: 运行确认通过。**
- [ ] **Step 5: 提交** `feat(canvas): 支持导出画布为文档`

### Task 7: 导入事务仓储与 `CanvasTransferService.Import`

**Files:**
- Create: `backend/internal/repository/canvas_transfer.go`
- Modify: `backend/internal/service/canvas_transfer.go`、`backend/internal/pkg/errcode/errcode.go`
- Test: `backend/internal/tests/service/canvas_transfer_import_test.go`；`backend/internal/tests/repository/canvas_transfer_integration_test.go`

**Interfaces:**
- Consumes: Task 2 的 `CopyBySha256`、`DiscardCopies`、`FileURL`；Task 3 的 `Parse/Validate`；Task 5 的 `Restore`。
- Produces：
  - 错误码 `ErrCanvasImportInvalid = New(30005, "导入文件格式不对", 400)`、`ErrCanvasImportTooNew = New(30006, "导入文件来自更新版本，请升级后再导入", 400)`、`ErrCanvasImportTooLarge = New(30007, "导入文件或素材数量超限", 413)`（具体原因用 `WithMsg`）。
  - 仓储：`repository.CanvasTransferRepository`（`NewCanvasTransferRepository(db)`），`WithTx(ctx, fn func(tx TransferTx) error) error`；`TransferTx` 由 service 包声明：`CreateAsset(ctx, *model.Asset) error`、`CountCharacters(ctx, userID uint64) (int64, error)`、`CharacterNameExists(ctx, userID uint64, name string) (bool, error)`、`CreateCharacter(ctx, *model.CharacterCard) error`、`CreateCanvas(ctx, *model.CanvasProject) error`（回填 ID）。
  - `ImportResult{Canvas ImportedCanvas \`json:"canvas"\`; Report ImportReport \`json:"report"\`}`；`ImportedCanvas{ID idcodec.ID \`json:"id"\`; Title string \`json:"title"\`; Revision uint64 \`json:"revision"\`}`；`ImportReport{Assets ImportAssets \`json:"assets"\`; Characters ImportCharacters \`json:"characters"\`; EdgesDropped int \`json:"edges_dropped"\`}`，其中 `ImportAssets{Total, Copied int; Missing []{NodeID \`json:"node_id"\`; FileName \`json:"file_name"\`}}`、`ImportCharacters{Created int; Renamed []{From, To string}; Invalid []{NodeID \`json:"node_id"\`; Name string}}`（JSON 键 snake_case）。
  - `(*CanvasTransferService).Import(ctx, userID uint64, document []byte) (*ImportResult, error)`。

- [ ] **Step 1: 写失败测试**（`canvas_transfer_import_test.go`，fake assets 记录 `CopyBySha256` 调用次数与 `DiscardCopies` 参数，fake tx 内存实现并可注入失败）：
  - `TestImport_Success`：全部素材找到 → 画布 `revision==1`、归导入者、标题 = 原标题 + `（导入）`；payload 里素材 ID 是新素材 ID；报告 `copied == total`。
  - `TestImport_SameHashCopiedOnce`：三处引用同一哈希 → `CopyBySha256` 只调一次。
  - `TestImport_MissingAssetReported`：某哈希复制返回 `ErrAssetNotFoundByHash` → 仍成功，`report.assets.missing` 含节点 ID 与文件名。
  - `TestImport_CharacterRename`：导入者已有同名卡 → 新卡名 `林夏 (2)`；再来一次变 `林夏 (3)`；名字 64 字时改名后长度仍 ≤ 64（截断名字部分）；`report.characters.renamed` 正确。
  - `TestImport_CharacterAvatarMissing`：主图找不到 → 不建卡，节点无 `characterId`，`invalid` 含节点；补充图找不到 → 卡照建，只少这一张。
  - `TestImport_CharacterLimit`：该用户已有 100 张卡 → 新卡不建、记入 `invalid`（与 `Create` 同一上限常量 `maxCharacterCards`）。
  - `TestImport_TxFailureDiscardsCopies`：`CreateCanvas` 返回错误 → `DiscardCopies` 被调用且参数是本次复制出的素材；没有画布留下。
  - `TestImport_RejectsBadDocuments`（表驱动）：非 JSON / `format` 错 → `Code==30005`；`version:2` → `30006`；节点超限 → `30007`；原文大小超 `MaxBytes` → `30007`。
  - `TestImport_TitleTruncation`：70 字标题 → 结果标题 ≤ 60 字且以 `（导入）` 结尾。
  - 集成（`canvas_transfer_integration_test.go`，沿用该目录集成测试的建库方式）：`TestCanvasTransferRepository_WithTx_RollsBackEverything`：回调里先 `CreateAsset` 再返回错误 → 素材表无新行；成功路径三类行都落库。
- [ ] **Step 2: 运行确认失败。**
- [ ] **Step 3: 实现。** `Import` 的 R6 步骤：1 `canvasdoc.Parse`，错误映射（`ErrTooNew→30006`、`ErrLimit→30007`、其余→30005）；2 `Validate(limits)`；3 对 `doc.Assets` 的哈希（排序保证确定性）逐个 `CopyBySha256`，`ErrAssetNotFoundByHash` 记缺失，其他错误整体失败并 `DiscardCopies`（说明原因：文件复制是网络 I/O，必须在事务外完成）；4 `WithTx`：插入素材拿 ID → 按 `c1,c2…` 顺序建角色卡（主图缺失 / 超上限记 invalid；重名加后缀，用 `CharacterNameExists`，撞唯一索引视为重名再试）→ `Restore` → 建画布；5 事务出错则 `DiscardCopies`；6 汇总报告。
- [ ] **Step 4: 运行确认通过** `go test ./internal/tests/service/ ./internal/tests/repository/ -run 'Import|CanvasTransfer' -v`。
- [ ] **Step 5: 提交** `feat(canvas): 支持从导出文档导入为新画布`

### Task 8: handler、路由、装配与接口表

**Files:**
- Create: `backend/internal/handler/canvas_transfer.go`
- Modify: `backend/internal/router/router.go`（`Handlers` 加字段；在 `canvas` 分组加 `GET /:id/export`、`POST /import`）、`backend/internal/initialize/app.go`（组装 `CanvasTransferService`：`Canvases: canvasProjectRepo`、`Assets: assetSvc`、`Characters: 角色卡仓储`、`Tx: repository.NewCanvasTransferRepository(db)`、`Limits: canvasdoc.DefaultLimits()`、`AppVersion` 取 `backend/pkg/version` 提供的应用版本，读不到写 `"unknown"`）、`backend/README.md`（接口表）
- Test: `backend/internal/tests/handler/canvas_transfer_test.go`

**Interfaces:**
- Consumes: Task 6、7 的 `Export`、`Import`。
- Produces：`GET /canvas/:id/export` → `response.OK(ExportResult)`；`POST /canvas/import`，请求体 `{"document": <对象>}`（`model` 或 handler 内声明的 `ImportCanvasReq{Document json.RawMessage \`json:"document" binding:"required"\`}`），成功 `response.OK(ImportResult)`；请求体超过 `Limits.MaxBytes + 1MB` 用 `http.MaxBytesReader` 截断并返回 `30007`。

- [ ] **Step 1: 写失败测试**（沿用 `canvas_project_test.go` 的 `fakeCanvasRepo` 与路由搭法）：导出成功（200，`data.document.format == "video-canvas/canvas"`）；导出不存在的画布 → 404 + `30001`；导入成功（200，`data.canvas.id` 非空、`data.report.assets.total` 正确）；导入缺 `document` → 400 + `10001`；导入 `format` 错 → 400 + `30005`；请求体超限 → 413 + `30007`。
- [ ] **Step 2: 运行确认失败。**
- [ ] **Step 3: 实现** handler（只取参、调 service、`response.*`）、路由、装配；`backend/README.md` 接口表增加两行。
- [ ] **Step 4: 运行确认通过** `cd backend && go test ./internal/tests/handler/ -run 'CanvasTransfer' -v`，再跑 `make fmt && make lint && make test`（规则 21，必须 0 issues）。
- [ ] **Step 5: 提交** `feat(canvas): 新增画布导出与导入接口`

### Task 9: 前端 API 映射与文件工具

**Files:**
- Modify: `web/src/api/canvas/index.ts`、`web/src/api/canvas/type.d.ts`
- Create: `web/src/utils/canvas/transfer-file.ts`
- Test: `web/src/tests/api/canvas-transfer.test.ts`、`web/src/tests/utils/canvas/transfer-file.test.ts`

**Interfaces:**
- Produces：
  - `exportCanvas(id: string): Promise<{ document: CanvasExportDocument; skippedAssets: number }>`（`GET /canvas/${id}/export`，`skipped_assets→skippedAssets`）。
  - `importCanvas(document: unknown): Promise<CanvasImportResultDto>`，`CanvasImportResultDto{ canvas:{id:string;title:string;revision:number}; report:{ assets:{total:number;copied:number;missing:{nodeId:string;fileName:string}[]}; characters:{created:number;renamed:{from:string;to:string}[];invalid:{nodeId:string;name:string}[]}; edgesDropped:number } }`（`POST /canvas/import`，body `{document}`，snake→camel 映射）；`CanvasExportDocument` 在 `type.d.ts` 里按设计 §7 声明（`format/version/exportedAt/appVersion/canvas/assets/characters`）。
  - `transfer-file.ts`：`exportFileName(title: string): string`（非法文件名字符 `\/:*?"<>|` 和控制字符换成 `_`，去首尾空白，空标题用 `未命名画布`，加 `.canvas.json`）；`downloadJson(fileName: string, data: unknown): void`（`Blob` + `<a download>`，下载后 `URL.revokeObjectURL`）；`MAX_IMPORT_BYTES = 10 * 1024 * 1024`；`precheckImportText(size: number, text: string): { ok: true; document: unknown } | { ok: false; message: string }`（超大 → `文件过大，上限 10 MB`；不是 JSON → `不是有效的 JSON 文件`；`format` 不对 → `不是本系统导出的画布文件`；`version` 大于 1 → `文件来自更新版本，请升级后再导入`）。

- [ ] **Step 1: 写失败测试**：API 测试用 `spyOn(service, "get"/"post")`（模式见 `tests/api/character.test.ts`，`afterEach` 还原）：断言路径、body 形状、snake→camel 映射（含 `missing[].node_id→nodeId`）；工具测试：`exportFileName("a/b:c")==="a_b_c.canvas.json"`、空标题回退、`precheckImportText` 的四种失败文案和一条合法文件。
- [ ] **Step 2: 运行确认失败** `cd web && bun test src/tests/api/canvas-transfer.test.ts src/tests/utils/canvas/transfer-file.test.ts`。
- [ ] **Step 3: 实现。**
- [ ] **Step 4: 运行确认通过。**
- [ ] **Step 5: 提交** `feat(web): 画布导入导出的 API 与文件工具`

### Task 10: 画布内「导出画布」

**Files:**
- Create: `web/src/utils/canvas/export-flow.ts`
- Modify: `web/src/pages/canvas/chrome/top-left-bar.tsx`（新增 `canvasId: string` 属性并在调用处传入；Logo 菜单「新建画布」下加分隔后的「导出画布」，图标 `FileDown`）
- Test: `web/src/tests/utils/canvas/export-flow.test.ts`

**Interfaces:**
- Consumes: Task 9 的 `exportCanvas`、`exportFileName`、`downloadJson`。
- Produces：`runCanvasExport(deps: { canvasId: string; title: string; saveNow: () => Promise<boolean>; exportCanvas: typeof exportCanvas; download: (name: string, data: unknown) => void; notify: { success(msg: string): void; warning(msg: string): void; error(msg: string): void } }): Promise<void>`。

- [ ] **Step 1: 写失败测试：** 保存失败（`saveNow` 返回 `false`）→ 不调用 `exportCanvas`、`notify.error` 一次，文案含 `没保存上`；成功 → `download` 以 `exportFileName(title)` 和返回的 `document` 调用一次、`notify.success` 一次；`skippedAssets: 2` → 额外 `notify.warning`，文案含 `2`；`exportCanvas` 抛错 → `notify.error('导出失败，请稍后重试')` 且不下载。
- [ ] **Step 2: 运行确认失败。**
- [ ] **Step 3: 实现** `runCanvasExport`；菜单项点击调用它（`notify` 接 `sonner` 的 `toast`），导出中禁用菜单项防重复点击；界面细节按 `.agents/skills/ui-design/SKILL.md`。
- [ ] **Step 4: 运行确认通过**，并 `bun node_modules/oxfmt/bin/oxfmt` 格式化改动文件、`bun node_modules/oxlint/bin/oxlint src` 无新增问题。
- [ ] **Step 5: 提交** `feat(web): 画布内新增导出画布`

### Task 11: 列表页「导入画布」

**Files:**
- Create: `web/src/utils/canvas/import-flow.ts`、`web/src/hooks/use-canvas-import.ts`、`web/src/components/home/import-canvas-button.tsx`、`web/src/components/home/import-result-dialog.tsx`
- Modify: `web/src/pages/home/canvas-list/index.tsx`（工具条里搜索框左侧放按钮，图标 `FileUp`）
- Test: `web/src/tests/utils/canvas/import-flow.test.ts`

**Interfaces:**
- Consumes: Task 9 的 `importCanvas`、`precheckImportText`、`CanvasImportResultDto`。
- Produces：`runCanvasImport(deps: { file: { size: number; text(): Promise<string> }; importCanvas: typeof importCanvas }): Promise<{ kind: "invalid"; message: string } | { kind: "error"; message: string } | { kind: "opened"; canvasId: string; title: string } | { kind: "report"; canvasId: string; title: string; report: CanvasImportResultDto["report"] }>`：预检失败 → `invalid`（不发请求）；请求抛错 → `error`（取业务错误文案，没有则 `导入失败，请稍后重试`）；报告里 `missing`、`renamed`、`invalid`、`edgesDropped` 全空 → `opened`，否则 `report`。

- [ ] **Step 1: 写失败测试：** 超大文件 → `invalid` 且 `importCanvas` 未调用；非 JSON / `format` 错 → `invalid`；干净报告 → `opened`；`missing` 非空 → `report` 且带回报告；`importCanvas` 抛带 message 的错误 → `error` 且文案原样；各一条。
- [ ] **Step 2: 运行确认失败。**
- [ ] **Step 3: 实现。** `use-canvas-import`：隐藏 `<input type="file" accept=".json,application/json">`、导入中禁用按钮；`opened` → toast `已导入「…」` 并 `navigate` 到新画布；`report` → 打开 `ImportResultDialog`（列出缺失素材节点与文件名、改名的角色卡、主图缺失的角色卡，按钮「打开画布」）；`invalid`/`error` → `toast.error(message)`。导入成功后刷新列表（`list.reload`）。界面按 ui-design 技能和现有 `components/ui` 组件。
- [ ] **Step 4: 运行确认通过**，格式化与 lint 同上。
- [ ] **Step 5: 提交** `feat(web): 画布列表页新增导入画布`

### Task 12: 全量验证与文档收口

**Files:**
- Modify: `docs/design/画布导入导出/画布导入导出.md`（§13 实现记录；实现中若与设计有出入，先改设计再改 HTML）

- [ ] **Step 1: 后端全量检查** `cd backend && make fmt && make lint && make test`，预期 0 issues、全部 PASS；涉及复制 / 并发读写，再跑 `make test-race`。
- [ ] **Step 2: 前端全量检查** `cd web && bun test && bun node_modules/oxlint/bin/oxlint src && bun node_modules/oxfmt/bin/oxfmt --check src`（以 `package.json` 里的类型检查脚本补一条 `tsc`），预期全部通过。
- [ ] **Step 3: 对照设计 §10 验收 7 条逐条指出对应测试**，写进 §13 实现记录；§11.4 两条「未验证」（大视频同步导入是否超时、`source=upload` 与资产页筛选的关系）、「新画布默认视口下节点是否在视野内」在记录里标注「需用户手动实测」，不要写成已验证。
- [ ] **Step 4: 让用户本机手动验证**（不用 Playwright）：A 账号导出、B 账号导入，检查素材、角色卡、提示词 `@` 引用、改名提示。
- [ ] **Step 5: 提交** `docs(canvas): 补充画布导入导出实现记录`

---

## Self-Review 记录

- **Spec 覆盖**：§10 验收 1（无泄露）→ Task 4 `NoLeak`；2（A→B 一致）→ Task 5 `RoundTrip`、Task 7 `Success`；3（新素材归属）→ Task 2、7；4（缺素材）→ Task 5、7；5（角色卡）→ Task 7；6（格式 / 版本 / 超限）→ Task 3、7、8；7（保存失败不导出）→ Task 10。入口（§6.1）→ Task 10、11；视口不带 → Task 4 `NoLeak`、Task 5 默认视口。
- **类型一致性**：`ErrAssetUnreadable` / `ErrAssetNotFoundByHash`（Task 2）被 Task 6、7 使用；`Refs/AssetEntry/CharacterEntry/ExportInput/ExportReport`（Task 4）与 Task 6 一致；`RestoreInput/RestoreReport`（Task 5）与 Task 7 一致；报告 JSON 键 snake_case（Task 7）与 Task 9 的映射一致。
- **与最初设计稿的三处差异（写计划时已同步回设计文档的 Markdown 和 HTML）**：迁移拆成 `0012` + `0013`；成功响应 HTTP 200（规则 A6）；接口报告键 snake_case。另外设计稿里「并发补算合并为一次」改为「写回幂等、并发各算一次」，因为 `singleflight` 所在的 `golang.org/x/sync` 只是间接依赖，不为一个优化新增依赖。
- **设计稿更正**：写计划时核对代码发现 `paramAssets` 的键是素材 ID 而非字段名，设计文档已更正，Task 4、5 按更正后的规则实现。
