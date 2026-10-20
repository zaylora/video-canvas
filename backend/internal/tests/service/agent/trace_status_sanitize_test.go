package agent_test

import (
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/model"
	. "video-canvas/internal/service/agent"
)

// 运行状态只有 model 里的 12 个：报告的「终态 xxx」、meta.json 的 status 都原样用它们，没有 done / error。
// 占用画布的 4 个（queued / running / waiting_approval / waiting_input）算「未结束」，报告要给出警告。
func TestExportTrace_RealRunStatuses(t *testing.T) {
	all := []string{model.RunQueued, model.RunRunning, model.RunWaitingApproval, model.RunWaitingInput, model.RunSucceeded, model.RunFailed,
		model.RunCanceled, model.RunBudgetExhausted, model.RunStepLimit, model.RunTimeout, model.RunInterrupted, model.RunExpired}
	if len(all) != 12 {
		t.Fatal("运行状态应是 12 个")
	}
	for _, st := range all {
		t.Run(st, func(t *testing.T) {
			a := newAdminEnv(t)
			run := a.recordedRun(t)
			a.repo.mu.Lock()
			a.repo.runs[run.ID].Status = st
			a.repo.mu.Unlock()
			b := a.export(t, run.ID, "json")

			report := b.file(t, "report.md")
			if !strings.Contains(report, "> 终态 "+st+" · ") {
				t.Errorf("报告应写真实状态 %q:\n%s", st, report)
			}
			var meta map[string]any
			if err := json.Unmarshal([]byte(b.file(t, "meta.json")), &meta); err != nil || meta["status"] != st {
				t.Errorf("meta.json 的 status 应是 %q: %v", st, meta["status"])
			}
			active := model.IsActiveRun(st)
			if got := strings.Contains(report, "⚠ 运行未结束，轨迹不完整"); got != active {
				t.Errorf("未结束警告: 状态 %s 期望 %v，实际 %v", st, active, got)
			}
			if st == model.RunStepLimit && !strings.Contains(report, "达到步数上限") {
				t.Errorf("step_limit 应进可疑信号:\n%s", report)
			}
			if st == model.RunBudgetExhausted && !strings.Contains(report, "积分预算用尽") {
				t.Errorf("budget_exhausted 应进可疑信号:\n%s", report)
			}
		})
	}
}

func sanitized(t *testing.T, body string) (map[string]any, string) {
	t.Helper()
	out := SanitizeChatRequest([]byte(body), "hash1234")
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := out.(map[string]any)
	return m, string(raw)
}

func TestSanitizeChatRequest_ImagesBecomeRefs(t *testing.T) {
	b64 := strings.Repeat("QUJD", 40)
	cases := []struct{ name, body string }{
		{"OpenAI image_url 的 URL", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://cdn.example/a.png?sig=SECRETSIG"}}]}]}`},
		{"OpenAI image_url 的 data URL", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,` + b64 + `"}}]}]}`},
		{"image_url 直接是字符串", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":"https://cdn.example/a.png?sig=SECRETSIG"}]}]}`},
		{"Anthropic 风格 base64", `{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + b64 + `"}}]}]}`},
		{"文本里内联的 data URL", `{"messages":[{"role":"tool","content":"结果 data:image/jpeg;base64,` + b64 + ` 结束"}]}`},
	}
	for _, tc := range cases {
		_, raw := sanitized(t, tc.body)
		if strings.Contains(raw, "SECRETSIG") || strings.Contains(raw, b64) || strings.Contains(raw, "cdn.example") {
			t.Errorf("%s：原图和签名地址不能留在轨迹里: %s", tc.name, raw)
		}
		if !strings.Contains(raw, "img_") {
			t.Errorf("%s：应换成稳定的引用: %s", tc.name, raw)
		}
	}
	_, a := sanitized(t, cases[0].body)
	_, b := sanitized(t, cases[0].body)
	if a != b {
		t.Error("同一张图的引用应稳定，才能看出前后请求是不是同一张")
	}
	m, _ := sanitized(t, cases[0].body)
	part := m["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if part["asset_id"] == nil {
		t.Errorf("图片部件应整个换成 {asset_id,…}: %v", part)
	}
}

func TestSanitizeChatRequest_ToolsAndOrdinaryContent(t *testing.T) {
	m, raw := sanitized(t, `{"model":"x","stream":true,"max_tokens":8192,"temperature":0.5,"tools":[{"type":"function","function":{"name":"a"}}],"messages":[{"role":"user","content":"普通文字 data: 不是图"}]}`)
	if m["tools"] != "hash1234" {
		t.Errorf("工具声明应换成 tools_hash 引用: %v", m["tools"])
	}
	if !strings.Contains(raw, "普通文字 data: 不是图") || !strings.Contains(raw, `"max_tokens":8192`) || !strings.Contains(raw, `"temperature":0.5`) {
		t.Errorf("普通内容和数字原样保留: %s", raw)
	}
	m, _ = sanitized(t, `{"model":"x","messages":[]}`)
	if _, ok := m["tools"]; ok {
		t.Error("请求本来没有 tools 就不要凭空加")
	}
}

func TestSanitizeChatRequest_NotJSONKeepsHead(t *testing.T) {
	long := strings.Repeat("坏", 5000)
	m, _ := sanitized(t, "不是 JSON "+long)
	text, _ := m["unparsed"].(string)
	if text == "" || len(text) > 4096 || !strings.HasPrefix(text, "不是 JSON") {
		t.Errorf("不是合法 JSON 时留下前 4KB 文本: len=%d", len(text))
	}
}
