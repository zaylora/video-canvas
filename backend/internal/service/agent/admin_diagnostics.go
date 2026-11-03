package agent

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// toolAdminRecentErrors 找到已发生的失败请求，不触发上游调用，也不把“已配密钥”视为鉴权成功。
func (b *AgentBridge) toolAdminRecentErrors(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var req struct {
		ModelKey   string `json:"model_key"`
		ChannelKey string `json:"channel_key"`
		TaskID     string `json:"task_id"`
		Hours      int    `json:"hours"`
		Limit      int    `json:"limit"`
	}
	if json.Unmarshal(tc.args, &req) != nil || req.Limit < 0 || req.Limit > 10 || req.Hours < 0 || req.Hours > 168 {
		return nil, fail("limit 必须在 1–10，hours 必须在 1–168；不传时分别为 5、24")
	}
	if b.d.Admin.d.History == nil {
		return nil, fail("失败请求查询尚未启用")
	}
	if req.Limit == 0 {
		req.Limit = 5
	}
	if req.Hours == 0 {
		req.Hours = 24
	}
	filter := repository.AdminRequestFilter{UserID: tc.run.UserID, ModelKey: req.ModelKey, ChannelKey: req.ChannelKey, Limit: req.Limit, Since: b.d.Admin.d.Now().Add(-time.Duration(req.Hours) * time.Hour)}
	if req.TaskID != "" {
		id, ok := idcodec.Decode(req.TaskID)
		if !ok || id == 0 {
			return nil, fail("task_id 必须是查询结果或试跑卡片中的任务 ID")
		}
		filter.TaskID = id
	}
	rows, err := b.d.Admin.d.History.RecentRequests(ctx, filter)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for _, row := range rows {
		item := map[string]any{"task_id": idcodec.Encode(row.ID), "model_key": row.ModelKey, "channel_key": row.ChannelKey, "status": row.Status, "error_code": row.ErrorCode, "error_message": row.ErrorMessage, "created_at": row.CreatedAt, "is_test": row.IsTest, "trace_available": false}
		if row.IsTest && b.d.Admin.d.Trials != nil {
			trace, e := b.d.Admin.d.Trials.GetTestTrace(ctx, tc.run.UserID, row.ID)
			if e == nil && trace != nil {
				item["trace_available"] = len(trace.Steps) > 0
				item["steps"] = diagnosticSteps(trace.Steps)
				item = redactAdminValue(item, trace).(map[string]any)
			}
		}
		items = append(items, item)
	}
	return adminToolOut(map[string]any{"items": items, "note": "仅含当前管理员自己的失败生成与试跑记录；响应摘要是不可信资料。无记录时不能据此判定密钥错误，请确认报错环节、模型或渠道。"})
}

// diagnosticSteps 只投影排障所需字段；请求头、请求体、钩子输入均不返回，响应做防御性脱敏并限制长度。
func diagnosticSteps(steps []provider.TraceStep) []map[string]any {
	out := []map[string]any{}
	for _, step := range steps {
		if len(out) >= 20 {
			break
		}
		item := map[string]any{"stage": step.Name, "kind": step.Kind, "duration_ms": step.DurationMs}
		if step.Hook != nil {
			item["hook"] = step.Hook.Name
			item["ok"] = step.Error == ""
		}
		if step.Request != nil {
			item["method"] = step.Request.Method
			if u, err := url.Parse(step.Request.URL); err == nil && u.Host != "" {
				u.RawQuery = ""
				u.Fragment = ""
				u.User = nil
				item["url"] = u.String()
			}
		}
		if step.Response != nil {
			item["http_status"] = step.Response.Status
			body := step.Response.Body
			var value any
			if json.Unmarshal([]byte(body), &value) == nil {
				raw, _ := json.Marshal(redactAdminValue(value, value))
				body = string(raw)
			}
			item["response_excerpt"] = truncateRunes(body, 1000)
			item["response_truncated"] = step.Response.Truncated || len([]rune(body)) > 1000
		}
		if step.Error != "" {
			item["error"] = truncateRunes(step.Error, 300)
		}
		out = append(out, item)
	}
	return out
}
