package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
)

// probeBody 是探针对象的内容，写入后再读回来比对。
const probeBody = "video-canvas storage probe"

// probeStepNames 是服务端能验证的步骤；“浏览器直传（CORS）”只能在浏览器里验证，不在这里。
var probeStepNames = []string{"鉴权与桶", "写入探针对象", "读取探针对象", "签名地址可访问", "删除探针对象"}

// ProbeIssue 是一次失败的可读说明：Title 说发生了什么，Hint 说怎么处理，Raw 是原始错误（可展开查看）。
type ProbeIssue struct {
	Title string `json:"title"`
	Hint  string `json:"hint"`
	Raw   string `json:"raw"`
}

// ProbeStep 是探针的一个步骤。Skipped 为真且 OK 为假，表示前面的步骤失败所以没执行；
// Skipped 与 OK 都为真，表示按选项主动跳过。
type ProbeStep struct {
	Index      int         `json:"index"`
	Name       string      `json:"name"`
	OK         bool        `json:"ok"`
	Skipped    bool        `json:"skipped"`
	DurationMs int64       `json:"duration_ms"`
	Issue      *ProbeIssue `json:"issue,omitempty"`
}

// ProbeResult 是整次探针的结果。
type ProbeResult struct {
	OK    bool        `json:"ok"`
	Steps []ProbeStep `json:"steps"`
}

// ProbeOptions 调整探针行为。
type ProbeOptions struct {
	HTTPClient   *http.Client // 访问签名地址用；为空时用 10 秒超时的默认客户端
	SkipURLCheck bool         // 跳过“签名地址可访问”：本地磁盘返回的是相对地址，没法由服务端自己去访问
}

// Probe 依次验证一个存储：鉴权与桶 → 写 → 读 → 签名地址访问 → 删。
// 某一步失败就停下，后面的步骤标记为跳过；写入过探针对象的话，无论哪一步失败都会尽力删掉它。
func Probe(ctx context.Context, st Storage, opt ProbeOptions) ProbeResult {
	client := opt.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	r := &probeRun{ctx: ctx, st: st, client: client, key: "_probe/" + probeID() + ".txt"}

	steps := []func() error{r.checkBucket, r.write, r.read, r.fetchURL, r.remove}
	res := ProbeResult{Steps: make([]ProbeStep, len(steps))}
	failed := false
	for i, run := range steps {
		step := &res.Steps[i]
		step.Index, step.Name = i+1, probeStepNames[i]
		switch {
		case failed:
			step.Skipped = true
		case i == probeURLStep && opt.SkipURLCheck:
			step.OK, step.Skipped = true, true
		default:
			failed = !runProbeStep(step, run)
		}
	}

	// 中途失败时探针对象可能已经写入桶里，用独立的 ctx 尽力清理，不依赖可能已取消的请求 ctx
	if r.written {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = st.Delete(cctx, r.key)
	}
	res.OK = !failed
	return res
}

// probeURLStep 是“签名地址可访问”在步骤列表里的下标。
const probeURLStep = 3

// runProbeStep 执行一个步骤并把结果写进 step，返回是否通过。
func runProbeStep(step *ProbeStep, run func() error) bool {
	start := time.Now()
	err := run()
	step.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		issue := ClassifyError(err)
		step.Issue = &issue
		return false
	}
	step.OK = true
	return true
}

// probeRun 保存一次探针的上下文：每个步骤是它的一个方法。
type probeRun struct {
	ctx     context.Context
	st      Storage
	client  *http.Client
	key     string
	written bool // 探针对象已写入且还没删除
}

// checkBucket 检查桶：优先用 HEAD 桶（404 = 桶不存在，能和对象不存在区分开）；
// 没有这个能力（本地磁盘）时，读一个肯定不存在的对象，返回“不存在”即可。
func (r *probeRun) checkBucket() error {
	if bc, ok := r.st.(BucketChecker); ok {
		return bc.CheckBucket(r.ctx)
	}
	rc, err := r.st.Open(r.ctx, r.key)
	if err == nil {
		_ = rc.Close()
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// write 写入探针对象。
func (r *probeRun) write() error {
	if err := r.st.Put(r.ctx, r.key, strings.NewReader(probeBody), int64(len(probeBody)), "text/plain"); err != nil {
		return err
	}
	r.written = true
	return nil
}

// read 读回并比对内容。
func (r *probeRun) read() error {
	rc, err := r.st.Open(r.ctx, r.key)
	if err != nil {
		return err
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	if !bytes.Equal(b, []byte(probeBody)) {
		return errors.New("读回的内容与写入的不一致")
	}
	return nil
}

// fetchURL 用签名（或公开）地址真实访问一次。
func (r *probeRun) fetchURL() error {
	u, err := r.st.URL(r.ctx, r.key, time.Minute)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("访问地址返回 HTTP %d", resp.StatusCode)
	}
	return nil
}

// remove 删除探针对象。
func (r *probeRun) remove() error {
	if err := r.st.Delete(r.ctx, r.key); err != nil {
		return err
	}
	r.written = false
	return nil
}

func probeID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("生成随机数失败: " + err.Error()) // 系统熵源不可用，无法安全继续
	}
	return hex.EncodeToString(b[:])
}

// ClassifyError 把云厂商和网络错误翻译成管理员看得懂的说明；原始错误保留在 Raw 里。
func ClassifyError(err error) ProbeIssue {
	raw := err.Error()
	var er minio.ErrorResponse
	if errors.As(err, &er) && er.Code != "" {
		raw = er.Code + ": " + er.Message
	}
	issue := func(title, hint string) ProbeIssue { return ProbeIssue{Title: title, Hint: hint, Raw: raw} }
	low := strings.ToLower(raw)

	switch {
	case strings.Contains(raw, "AccessDenied"):
		return issue("密钥无权访问该桶", "检查 AccessKey 是否正确，以及这个子账号是否被授予了该桶的读写权限。")
	case strings.Contains(raw, "InvalidAccessKeyId"):
		return issue("AccessKey ID 不存在", "确认 AccessKey ID 填写正确，并且没有被禁用或删除。")
	case strings.Contains(raw, "SignatureDoesNotMatch"):
		return issue("Secret 不正确", "AccessKey Secret 填错了，或者地域与桶不匹配导致签名失败。")
	case strings.Contains(raw, "NoSuchBucket"):
		return issue("桶不存在或地域不对", "确认桶名拼写正确，并且选择的地域就是桶所在的地域。")
	case strings.Contains(low, "virtual hosted style"):
		return issue("寻址方式不对", "该服务商只支持虚拟主机寻址（桶名在域名里），请不要使用 path 寻址。")
	case strings.Contains(low, "no such host"), strings.Contains(low, "connection refused"), strings.Contains(low, "dial tcp"):
		return issue("endpoint 不可达", "检查 endpoint / 地域是否正确，以及服务器能否访问该地址（网络、防火墙、DNS）。")
	case strings.Contains(low, "deadline exceeded"), strings.Contains(low, "timeout"):
		return issue("连接超时", "服务器到存储服务的网络较慢或不通，稍后重试，或检查网络与代理设置。")
	}
	return issue("未知错误", "请展开原始错误查看详情。")
}
