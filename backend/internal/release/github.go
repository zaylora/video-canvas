// Package release 读取 GitHub Releases 上的最新正式版本，业务层只依赖 service.ReleaseFetcher 接口。
package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"video-canvas/internal/model"
)

const (
	defaultBaseURL = "https://api.github.com"
	defaultTimeout = 10 * time.Second
	// maxBodyBytes 限制响应大小：Release 正文是更新日志，正常远小于这个值。
	maxBodyBytes = 1 << 20
)

// repoPattern 是 GitHub 仓库名 owner/name 的格式；不合规的不发请求，也避免拼出意外的 URL。
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`)

// Options 是 GitHubClient 的可选参数，零值取默认。
type Options struct {
	BaseURL   string        // 测试里指向 httptest 服务；默认 https://api.github.com
	UserAgent string        // GitHub 要求带 User-Agent
	Timeout   time.Duration // 默认 10 秒
}

// GitHubClient 调用 GitHub 的 releases/latest 接口（只返回正式版，自动排除预发布和草稿）。
// 用标准库默认的 Transport，会读取 HTTPS_PROXY 等代理环境变量。
type GitHubClient struct {
	repo string
	opts Options
	http *http.Client
}

// NewGitHubClient 创建客户端；repo 形如 zaylora/video-canvas。
func NewGitHubClient(repo string, opts Options) *GitHubClient {
	if opts.BaseURL == "" {
		opts.BaseURL = defaultBaseURL
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "video-canvas"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	return &GitHubClient{repo: repo, opts: opts, http: &http.Client{Timeout: opts.Timeout}}
}

type githubRelease struct {
	TagName     string    `json:"tag_name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

// Latest 返回最新的正式版本。失败返回的错误是给管理员看的中文原因。
func (c *GitHubClient) Latest(ctx context.Context) (*model.ReleaseInfo, error) {
	if !repoPattern.MatchString(c.repo) || strings.Contains(c.repo, "..") {
		return nil, fmt.Errorf("仓库名 %q 不合法，应为 owner/name", c.repo)
	}
	url := strings.TrimRight(c.opts.BaseURL, "/") + "/repos/" + c.repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", c.opts.UserAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 GitHub：%w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, errors.New("仓库还没有正式发布的版本（或仓库不存在、不公开）")
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return nil, errors.New("GitHub 接口限流，稍后再试")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GitHub 返回 %d", resp.StatusCode)
	}

	var r githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&r); err != nil {
		return nil, fmt.Errorf("解析 GitHub 返回失败：%w", err)
	}
	ver := strings.TrimPrefix(strings.TrimSpace(r.TagName), "v")
	if ver == "" {
		return nil, errors.New("GitHub 返回的版本号为空")
	}
	return &model.ReleaseInfo{Version: ver, Notes: r.Body, URL: r.HTMLURL, PublishedAt: r.PublishedAt}, nil
}
