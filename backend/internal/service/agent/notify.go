package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"video-canvas/internal/provider"
)

// notifyTimeout 是一次通知的时限：门铃只是一个很快的请求，慢就当作失败，交给队列退避重试。
const notifyTimeout = 10 * time.Second

// TaskNotifier 经 HTTP 把“Agent 发起的生成任务结束了”告诉 Agent 服务，实现 provider.AgentNotifier。
// 它只是门铃，不带产物：Agent 醒来后再向平台读任务的最新状态，所以重复、乱序、晚到都不影响结果。
type TaskNotifier struct {
	BaseURL      string       // Agent 服务地址（agent.url）
	ServiceToken string       // 服务令牌（agent.service_token）
	Client       *http.Client // 为 nil 时用带超时的默认客户端
}

var _ provider.AgentNotifier = (*TaskNotifier)(nil)

// TaskSettled 实现 provider.AgentNotifier。2xx 与 404（Agent 里没有对应的等待者：已取消等待或本来就不用等）都算送达；
// 其它情况返回错误，由队列重试。
func (n *TaskNotifier) TaskSettled(ctx context.Context, notice provider.TaskSettledNotice) error {
	c := n.Client
	if c == nil {
		c = &http.Client{Timeout: notifyTimeout}
	}
	// 雪花 id 超过 JSON 数字的安全整数范围，用字符串发
	body, err := json.Marshal(map[string]string{
		"task_id": strconv.FormatUint(notice.TaskID, 10),
		"run_id":  strconv.FormatUint(notice.AgentRunID, 10),
		"status":  notice.Status,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(n.BaseURL, "/")+"/internal/v1/watches/task-settled", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Service-Token", n.ServiceToken)
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("连接 Agent 服务失败：%w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300, resp.StatusCode == http.StatusNotFound:
		return nil
	default:
		return fmt.Errorf("通知被 Agent 服务拒绝（HTTP %d）", resp.StatusCode)
	}
}
