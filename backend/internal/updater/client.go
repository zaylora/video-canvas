package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// requestTimeout 是一次调用 updater 的时限：两个接口都只读文件或启动后台任务，很快返回。
const requestTimeout = 5 * time.Second

// Client 是 backend 调用 updater 的客户端，通过共享卷里的 unix socket 通信。
type Client struct {
	token string
	http  *http.Client
}

// NewClient 创建客户端；network 为 unix（生产）或 tcp（测试）。
func NewClient(network, address, token string) *Client {
	dialer := &net.Dialer{Timeout: requestTimeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, address)
		},
	}
	return &Client{token: token, http: &http.Client{Transport: transport, Timeout: requestTimeout}}
}

// Status 读 updater 的状态。连不上时返回的错误是给管理员看的原因。
func (c *Client) Status(ctx context.Context) (*model.UpdaterStatus, error) {
	var st model.UpdaterStatus
	if err := c.do(ctx, http.MethodGet, "/status", nil, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// Upgrade 让 updater 开始升级到 target，立即返回；正在升级时返回 errcode.ErrUpgradeRunning。
func (c *Client) Upgrade(ctx context.Context, target string) error {
	body, _ := json.Marshal(map[string]string{"target": target})
	return c.do(ctx, http.MethodPost, "/upgrade", body, nil)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, out any) error {
	// host 随便写：实际连接由 DialContext 决定
	req, err := http.NewRequestWithContext(ctx, method, "http://updater"+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("连接 updater 失败：%w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusConflict:
		return errcode.ErrUpgradeRunning
	case resp.StatusCode >= 300:
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("updater 返回 %d：%s", resp.StatusCode, e.Error)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("解析 updater 返回失败：%w", err)
		}
	}
	return nil
}
