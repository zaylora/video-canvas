package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	. "video-canvas/internal/service/agent"
)

// HTTPLauncher 让 Agent 作为独立的 HTTP 服务时，运行时的接口不用变：
// 第一次 Send 是启动参数（POST /segments，响应是一条 NDJSON 流，一直开到片段结束），之后的 Send 是插话和中止，
// Kill 关掉那条连接，Done 在流结束（或断开、心跳超时）时给出退出信息。

const launcherToken = "launcher-token-0123456789abcdef0123456789"

// fakeAgent 是一个假的 Agent 服务，按 script 回放 /segments 的响应流，记录收到的请求。
type fakeAgent struct {
	*httptest.Server
	mu       sync.Mutex
	starts   []recordedReq
	controls []recordedReq
	ctxDone  chan struct{} // /segments 的请求上下文被取消（连接断开）时关闭
	once     sync.Once
	// 控制请求（插话、中止）的响应；默认 202
	controlStatus int
	controlBody   string
}

type recordedReq struct {
	Path   string
	Header http.Header
	Body   string
}

// script 决定响应流的内容；返回后连接结束
type script func(w http.ResponseWriter, r *http.Request, flush func())

func newFakeAgent(t *testing.T, s script) *fakeAgent {
	t.Helper()
	f := &fakeAgent{ctxDone: make(chan struct{})}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec := recordedReq{Path: r.URL.Path, Header: r.Header.Clone(), Body: string(body)}
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/segments" {
			f.starts = append(f.starts, rec)
			f.mu.Unlock()
			flush := func() { w.(http.Flusher).Flush() }
			go func() {
				<-r.Context().Done()
				f.once.Do(func() { close(f.ctxDone) })
			}()
			s(w, r, flush)
			f.mu.Lock()
			return
		}
		f.controls = append(f.controls, rec)
		if f.controlStatus != 0 {
			w.WriteHeader(f.controlStatus)
			_, _ = w.Write([]byte(f.controlBody))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeAgent) launcher() *HTTPLauncher {
	return &HTTPLauncher{BaseURL: f.URL, ServiceToken: launcherToken, HeartbeatTimeout: 2 * time.Second}
}

func (f *fakeAgent) startReqs() []recordedReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedReq(nil), f.starts...)
}

func (f *fakeAgent) controlReqs() []recordedReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedReq(nil), f.controls...)
}

func writeLine(w http.ResponseWriter, flush func(), v any) {
	b, _ := json.Marshal(v)
	_, _ = w.Write(append(b, '\n'))
	flush()
}

func waitDone(t *testing.T, p Proc, within time.Duration) ExitInfo {
	t.Helper()
	select {
	case e := <-p.Done():
		return e
	case <-time.After(within):
		t.Fatal("没等到 Done")
		return ExitInfo{}
	}
}

func finishScript(code int, tail string) script {
	return func(w http.ResponseWriter, _ *http.Request, flush func()) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		writeLine(w, flush, map[string]any{"type": "accepted"})
		writeLine(w, flush, map[string]any{"type": "heartbeat"})
		writeLine(w, flush, map[string]any{"type": "exit", "code": code, "stderr_tail": tail})
	}
}

func TestHTTPLauncher_StartPostsInputAndReportsExit(t *testing.T) {
	f := newFakeAgent(t, finishScript(0, "诊断末尾"))
	p, err := f.launcher().Launch(context.Background(), LaunchSpec{Name: "node", Args: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(f.startReqs()); n != 0 {
		t.Fatalf("Launch 本身不发请求，启动参数要等第一次 Send: %d", n)
	}
	input := `{"mode":"start","token":"tok-1"}`
	if err := p.Send([]byte(input)); err != nil {
		t.Fatal(err)
	}
	exit := waitDone(t, p, 3*time.Second)
	if exit.Code != 0 || exit.StderrTail != "诊断末尾" || exit.Err != nil {
		t.Errorf("exit=%+v", exit)
	}
	reqs := f.startReqs()
	if len(reqs) != 1 {
		t.Fatalf("请求数 %d", len(reqs))
	}
	r := reqs[0]
	if r.Body != input {
		t.Errorf("请求体应是启动参数原文: %q", r.Body)
	}
	if got := r.Header.Get("X-Agent-Service-Token"); got != launcherToken {
		t.Errorf("服务令牌: %q", got)
	}
	if id := r.Header.Get("X-Segment-Id"); !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(id) {
		t.Errorf("片段 id 应是 32 位十六进制: %q", id)
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type: %q", ct)
	}
}

func TestHTTPLauncher_ExitCodeOneMeansCouldNotReport(t *testing.T) {
	f := newFakeAgent(t, finishScript(1, "无法向桥报告结果"))
	p, _ := f.launcher().Launch(context.Background(), LaunchSpec{})
	if err := p.Send([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if exit := waitDone(t, p, 3*time.Second); exit.Code != 1 || !strings.Contains(exit.StderrTail, "无法向桥报告") {
		t.Errorf("exit=%+v", exit)
	}
}

func TestHTTPLauncher_StartRejected(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"令牌不对", 401, `{"code":401,"msg":"服务令牌无效"}`, "服务令牌无效"},
		{"片段重复", 409, `{"code":409,"msg":"这个片段已经在运行"}`, "这个片段已经在运行"},
		{"并发满了", 503, `{"code":503,"msg":"同时运行的片段已达上限（64）"}`, "已达上限"},
		{"响应不是 JSON", 502, `Bad Gateway`, "502"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeAgent(t, func(w http.ResponseWriter, _ *http.Request, _ func()) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			p, _ := f.launcher().Launch(context.Background(), LaunchSpec{})
			err := p.Send([]byte(`{}`))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("应返回含 %q 的错误: %v", tc.want, err)
			}
		})
	}
	t.Run("连不上", func(t *testing.T) {
		f := newFakeAgent(t, finishScript(0, ""))
		l := f.launcher()
		f.Close()
		p, _ := l.Launch(context.Background(), LaunchSpec{})
		if err := p.Send([]byte(`{}`)); err == nil {
			t.Error("连不上服务应返回错误，让 Go 把这次启动当成失败")
		}
	})
}

func TestHTTPLauncher_SteerAndAbort(t *testing.T) {
	hold := make(chan struct{})
	f := newFakeAgent(t, func(w http.ResponseWriter, _ *http.Request, flush func()) {
		writeLine(w, flush, map[string]any{"type": "accepted"})
		<-hold
		writeLine(w, flush, map[string]any{"type": "exit", "code": 0})
	})
	p, _ := f.launcher().Launch(context.Background(), LaunchSpec{})
	if err := p.Send([]byte(`{"mode":"start"}`)); err != nil {
		t.Fatal(err)
	}
	id := f.startReqs()[0].Header.Get("X-Segment-Id")

	if err := p.Send([]byte(`{"type":"steer","text":"再暖一点"}`)); err != nil {
		t.Fatal(err)
	}
	if err := p.Send([]byte(`{"type":"abort"}`)); err != nil {
		t.Fatal(err)
	}
	c := f.controlReqs()
	if len(c) != 2 {
		t.Fatalf("控制请求数 %d", len(c))
	}
	if c[0].Path != "/segments/"+id+"/steer" || c[0].Body != `{"text":"再暖一点"}` {
		t.Errorf("插话: %+v", c[0])
	}
	if c[1].Path != "/segments/"+id+"/abort" {
		t.Errorf("中止: %+v", c[1])
	}
	for _, r := range c {
		if r.Header.Get("X-Agent-Service-Token") != launcherToken {
			t.Errorf("控制请求也要带服务令牌: %+v", r.Header)
		}
	}
	close(hold)
	waitDone(t, p, 3*time.Second)
}

func TestHTTPLauncher_ControlErrors(t *testing.T) {
	hold := make(chan struct{})
	f := newFakeAgent(t, func(w http.ResponseWriter, _ *http.Request, flush func()) {
		writeLine(w, flush, map[string]any{"type": "accepted"})
		<-hold
	})
	defer close(hold)
	p, _ := f.launcher().Launch(context.Background(), LaunchSpec{})
	if err := p.Send([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := p.Send([]byte(`不是 JSON`)); err == nil {
		t.Error("控制消息不是 JSON 应报错")
	}
	if err := p.Send([]byte(`{"type":"unknown"}`)); err == nil {
		t.Error("未知的控制消息应报错")
	}
	if err := p.Send([]byte(`{"type":"steer","text":""}`)); err != nil {
		t.Errorf("空文字照样转给服务，由服务决定: %v", err)
	}
}

// 服务那边已经没有这个片段（刚好结束了）：插话要让调用方知道没送到
func TestHTTPLauncher_ControlNotDelivered(t *testing.T) {
	hold := make(chan struct{})
	f := newFakeAgent(t, func(w http.ResponseWriter, _ *http.Request, flush func()) {
		writeLine(w, flush, map[string]any{"type": "accepted"})
		<-hold
	})
	defer close(hold)
	f.controlStatus, f.controlBody = http.StatusNotFound, `{"code":404,"msg":"没有这个片段在运行"}`
	p, _ := f.launcher().Launch(context.Background(), LaunchSpec{})
	if err := p.Send([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{`{"type":"steer","text":"x"}`, `{"type":"abort"}`} {
		if err := p.Send([]byte(line)); err == nil || !strings.Contains(err.Error(), "没有这个片段") {
			t.Errorf("%s 没送到应返回服务给的原因: %v", line, err)
		}
	}
}

func TestHTTPLauncher_DropWithoutExitIsAbnormal(t *testing.T) {
	f := newFakeAgent(t, func(w http.ResponseWriter, _ *http.Request, flush func()) {
		writeLine(w, flush, map[string]any{"type": "accepted"})
		// 不写 exit 就结束：服务崩了
	})
	p, _ := f.launcher().Launch(context.Background(), LaunchSpec{})
	if err := p.Send([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	exit := waitDone(t, p, 3*time.Second)
	if exit.Code != -1 || exit.Err == nil || !strings.Contains(exit.Err.Error(), "断开") {
		t.Errorf("没有 exit 就断开应是异常退出（-1）: %+v", exit)
	}
}

func TestHTTPLauncher_HeartbeatTimeout(t *testing.T) {
	f := newFakeAgent(t, func(w http.ResponseWriter, r *http.Request, flush func()) {
		writeLine(w, flush, map[string]any{"type": "accepted"})
		<-r.Context().Done() // 之后一直沉默：服务假死
	})
	l := f.launcher()
	l.HeartbeatTimeout = 150 * time.Millisecond
	p, _ := l.Launch(context.Background(), LaunchSpec{})
	if err := p.Send([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	exit := waitDone(t, p, 3*time.Second)
	if exit.Code != -1 || exit.Err == nil || !strings.Contains(exit.Err.Error(), "心跳") {
		t.Errorf("心跳超时应是异常退出: %+v", exit)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("应在心跳超时后很快结束")
	}
	select {
	case <-f.ctxDone:
	case <-time.After(2 * time.Second):
		t.Error("超时后应关掉连接，服务那边才会放弃这个片段")
	}
}

func TestHTTPLauncher_HeartbeatKeepsConnectionAlive(t *testing.T) {
	f := newFakeAgent(t, func(w http.ResponseWriter, _ *http.Request, flush func()) {
		writeLine(w, flush, map[string]any{"type": "accepted"})
		for i := 0; i < 6; i++ {
			time.Sleep(60 * time.Millisecond)
			writeLine(w, flush, map[string]any{"type": "heartbeat"})
		}
		writeLine(w, flush, map[string]any{"type": "exit", "code": 0})
	})
	l := f.launcher()
	l.HeartbeatTimeout = 200 * time.Millisecond // 总时长 360ms 超过它，但每次心跳都在窗口内
	p, _ := l.Launch(context.Background(), LaunchSpec{})
	if err := p.Send([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if exit := waitDone(t, p, 3*time.Second); exit.Code != 0 || exit.Err != nil {
		t.Errorf("有心跳就不该超时: %+v", exit)
	}
}

func TestHTTPLauncher_KillClosesTheConnection(t *testing.T) {
	f := newFakeAgent(t, func(w http.ResponseWriter, r *http.Request, flush func()) {
		writeLine(w, flush, map[string]any{"type": "accepted"})
		<-r.Context().Done()
	})
	p, _ := f.launcher().Launch(context.Background(), LaunchSpec{})
	if err := p.Send([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	p.Kill()
	exit := waitDone(t, p, 3*time.Second)
	if exit.Code != -1 {
		t.Errorf("被杀应是 -1: %+v", exit)
	}
	select {
	case <-f.ctxDone:
	case <-time.After(2 * time.Second):
		t.Error("Kill 应关掉连接：服务那边靠它中止片段")
	}
}

func TestHTTPLauncher_KillBeforeStart(t *testing.T) {
	f := newFakeAgent(t, finishScript(0, ""))
	p, _ := f.launcher().Launch(context.Background(), LaunchSpec{})
	p.Kill()
	if err := p.Send([]byte(`{}`)); err == nil {
		t.Error("已经 Kill 的进程不能再启动")
	}
	if n := len(f.startReqs()); n != 0 {
		t.Errorf("不该发请求: %d", n)
	}
}

// 发起启动的请求（比如用户点了发送）取消了，片段不能跟着取消：它要比请求活得久
func TestHTTPLauncher_OutlivesLaunchContext(t *testing.T) {
	hold := make(chan struct{})
	f := newFakeAgent(t, func(w http.ResponseWriter, _ *http.Request, flush func()) {
		writeLine(w, flush, map[string]any{"type": "accepted"})
		<-hold
		writeLine(w, flush, map[string]any{"type": "exit", "code": 0})
	})
	ctx, cancel := context.WithCancel(context.Background())
	p, _ := f.launcher().Launch(ctx, LaunchSpec{})
	if err := p.Send([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	cancel()
	time.Sleep(100 * time.Millisecond)
	select {
	case e := <-p.Done():
		t.Fatalf("请求取消后片段不该结束: %+v", e)
	default:
	}
	close(hold)
	if exit := waitDone(t, p, 3*time.Second); exit.Code != 0 {
		t.Errorf("exit=%+v", exit)
	}
}

func TestHTTPLauncher_EachLaunchGetsItsOwnSegmentID(t *testing.T) {
	f := newFakeAgent(t, finishScript(0, ""))
	l := f.launcher()
	for i := 0; i < 3; i++ {
		p, _ := l.Launch(context.Background(), LaunchSpec{})
		if err := p.Send([]byte(fmt.Sprintf(`{"n":%d}`, i))); err != nil {
			t.Fatal(err)
		}
		waitDone(t, p, 3*time.Second)
	}
	seen := map[string]bool{}
	for _, r := range f.startReqs() {
		seen[r.Header.Get("X-Segment-Id")] = true
	}
	if len(seen) != 3 {
		t.Errorf("每次启动要有不同的片段 id: %v", seen)
	}
}
