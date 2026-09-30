// Package pluginrunner 在独立进程中执行协议插件，不访问数据库或上游网络。
package pluginrunner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/dop251/goja"

	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginproto"
)

// Options 控制单个插件版本的运行时数量与钩子预算；零值采用契约默认值。
type Options struct {
	PoolSize       int
	DefaultTimeout time.Duration
	// LoadTimeout 是执行插件顶层代码（装载、预检、补建运行时）的时限；零值取 2 秒。
	// 它比钩子时限宽松，因为顶层代码要一次性完成编译后的初始化，但同样会被中断。
	LoadTimeout time.Duration
}

// Server 是只执行插件代码的 HTTP 服务。
type Server struct {
	opts     Options
	mu       sync.Mutex
	versions map[string]*version
	order    []string
}

type version struct {
	program     *goja.Program
	loadTimeout time.Duration
	hooks       []string
	idle        chan *runtime
	slots       chan struct{}
}

type runtime struct {
	vm      *goja.Runtime
	exports *goja.Object
	hooks   []string // 顶层代码执行后导出的钩子名
	logs    []string
}

// NewServer 创建 runner；内存里至多保留 64 个插件版本。
func NewServer(opts Options) *Server {
	if opts.PoolSize <= 0 {
		opts.PoolSize = pluginproto.DefaultPoolSize
	}
	if opts.DefaultTimeout <= 0 {
		opts.DefaultTimeout = time.Duration(pluginproto.DefaultHookTimeoutMs) * time.Millisecond
	}
	if opts.LoadTimeout <= 0 {
		opts.LoadTimeout = 2 * time.Second
	}
	return &Server{opts: opts, versions: make(map[string]*version)}
}

// Handler 返回线协议的 HTTP handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(pluginproto.PathHealth, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "方法不支持", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc(pluginproto.PathLoad, s.load)
	mux.HandleFunc(pluginproto.PathCall, s.call)
	mux.HandleFunc(pluginproto.PathPrecheck, s.precheck)
	return mux
}

func readRequest(w http.ResponseWriter, r *http.Request, out any) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "方法不支持", http.StatusMethodNotAllowed)
		return false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 2*pluginproto.MaxPayloadBytes+1))
	if err != nil || len(body) > 2*pluginproto.MaxPayloadBytes || json.Unmarshal(body, out) != nil {
		http.Error(w, "请求 JSON 不合法或超过上限", http.StatusBadRequest)
		return false
	}
	return true
}

func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func issue(code, message string) *pluginproto.CallError {
	return &pluginproto.CallError{Code: code, Message: message}
}

func hash(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func (s *Server) remember(sha string, v *version) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.versions[sha]; exists {
		return
	}
	if len(s.order) == 64 {
		delete(s.versions, s.order[0])
		s.order = s.order[1:]
	}
	s.versions[sha] = v
	s.order = append(s.order, sha)
}

func (s *Server) lookup(sha string) *version {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.versions[sha]
}

func (s *Server) load(w http.ResponseWriter, r *http.Request) {
	var in pluginproto.LoadRequest
	if !readRequest(w, r, &in) {
		return
	}
	if len(in.Code) == 0 || len([]byte(in.Code)) > pluginproto.MaxPluginBytes {
		respond(w, pluginproto.LoadResponse{Error: issue(pluginproto.CodeTooLarge, "插件文件超过上限")})
		return
	}
	if in.SHA256 == "" || hash(in.Code) != in.SHA256 {
		respond(w, pluginproto.LoadResponse{Error: issue(pluginproto.CodeBadRequest, "插件代码 sha256 不匹配")})
		return
	}
	program, err := compile(in.Code)
	if err != nil {
		respond(w, pluginproto.LoadResponse{Error: issue(pluginproto.CodeException, err.Error())})
		return
	}
	rt, err := newRuntime(program, s.opts.LoadTimeout)
	if err != nil {
		respond(w, pluginproto.LoadResponse{Error: callError(err)})
		return
	}
	v := newVersion(program, rt.hooks, s.opts.PoolSize, s.opts.LoadTimeout)
	v.idle <- rt
	s.remember(in.SHA256, v)
	respond(w, pluginproto.LoadResponse{OK: true, Hooks: v.hooks})
}

func (s *Server) call(w http.ResponseWriter, r *http.Request) {
	var in pluginproto.CallRequest
	if !readRequest(w, r, &in) {
		return
	}
	start := time.Now()
	out := pluginproto.CallResponse{}
	defer func() {
		out.DurationMs = time.Since(start).Milliseconds()
		respond(w, out)
	}()
	v := s.lookup(in.SHA256)
	if v == nil {
		out.Error = issue(pluginproto.CodeUnknownPlugin, "runner 未装载该插件")
		return
	}
	if in.Hook == "" || len(in.Args) == 0 {
		out.Error = issue(pluginproto.CodeBadRequest, "hook 和 args 不能为空")
		return
	}
	total := 0
	for _, arg := range in.Args {
		if len(arg) > pluginproto.MaxPayloadBytes || !json.Valid(arg) {
			out.Error = issue(pluginproto.CodeTooLarge, "钩子参数超过上限或不是合法 JSON")
			return
		}
		total += len(arg)
	}
	if total > 2*pluginproto.MaxPayloadBytes {
		out.Error = issue(pluginproto.CodeTooLarge, "钩子参数总大小超过上限")
		return
	}
	timeout := s.opts.DefaultTimeout
	if in.TimeoutMs > 0 {
		timeout = time.Duration(in.TimeoutMs) * time.Millisecond
	}
	if timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	rt, err := v.acquire()
	if err != nil {
		out.Error = issue(pluginproto.CodeInternal, err.Error())
		return
	}
	result, err := invoke(rt, in.Hook, in.Args, timeout)
	if err != nil {
		v.release(rt, errors.Is(err, errTimeout))
		out.Error = callError(err)
		return
	}
	// 先复制日志再归还：归还后运行时可能立刻被别的请求取走并清空 logs
	out.Logs = append([]string(nil), rt.logs...)
	v.release(rt, false)
	if len(result) > pluginproto.MaxPayloadBytes {
		out.Error = issue(pluginproto.CodeTooLarge, "钩子返回值超过上限")
		return
	}
	out.Result = result
}

func (s *Server) precheck(w http.ResponseWriter, r *http.Request) {
	var in pluginproto.PrecheckRequest
	if !readRequest(w, r, &in) {
		return
	}
	out := pluginproto.PrecheckResponse{SHA256: hash(in.Code)}
	if len(in.Code) == 0 || len([]byte(in.Code)) > pluginproto.MaxPluginBytes {
		out.Issues = []pluginproto.Issue{{Path: "code", Message: "插件文件为空或超过上限"}}
		respond(w, out)
		return
	}
	program, err := compile(in.Code)
	if err != nil {
		out.Issues = []pluginproto.Issue{{Path: "code", Message: safeError(err)}}
		respond(w, out)
		return
	}
	// 预检与装载走同一套保护：顶层代码死循环在 LoadTimeout 内被中断，返回预检问题而不是挂住请求
	rt, err := newRuntime(program, s.opts.LoadTimeout)
	if err != nil {
		out.Issues = []pluginproto.Issue{{Path: "code", Message: safeError(err)}}
		respond(w, out)
		return
	}
	out.Hooks = rt.hooks
	metaRaw, err := encodeMeta(rt, s.opts.LoadTimeout)
	if errors.Is(err, errNoMeta) {
		out.Issues = []pluginproto.Issue{{Path: "meta", Message: "插件必须导出 meta 对象"}}
		respond(w, out)
		return
	}
	if err != nil {
		out.Issues = []pluginproto.Issue{{Path: "meta", Message: "meta 不能编码成 JSON：" + safeError(err)}}
		respond(w, out)
		return
	}
	if len(metaRaw) > 64<<10 {
		out.Issues = []pluginproto.Issue{{Path: "meta", Message: "meta 编码后超过 64KB"}}
		respond(w, out)
		return
	}
	out.Meta = metaRaw
	_, issues := pluginmeta.Validate(metaRaw, out.Hooks)
	for _, item := range issues {
		out.Issues = append(out.Issues, pluginproto.Issue{Path: item.Path, Message: item.Message})
	}
	out.OK = len(out.Issues) == 0
	respond(w, out)
}

func newVersion(program *goja.Program, hookNames []string, poolSize int, loadTimeout time.Duration) *version {
	return &version{
		program:     program,
		loadTimeout: loadTimeout,
		hooks:       append([]string(nil), hookNames...),
		idle:        make(chan *runtime, poolSize),
		slots:       make(chan struct{}, poolSize),
	}
}

func (v *version) acquire() (*runtime, error) {
	v.slots <- struct{}{}
	select {
	case rt := <-v.idle:
		return rt, nil
	default:
		rt, err := newRuntime(v.program, v.loadTimeout)
		if err != nil {
			<-v.slots
			return nil, err
		}
		return rt, nil
	}
}

func (v *version) release(rt *runtime, discard bool) {
	if !discard {
		select {
		case v.idle <- rt:
		default:
			discard = true
		}
	}
	if discard {
		rt.vm.ClearInterrupt()
	}
	<-v.slots
}

func callError(err error) *pluginproto.CallError {
	switch {
	case errors.Is(err, errMissing):
		return issue(pluginproto.CodeHookMissing, err.Error())
	case errors.Is(err, errTimeout):
		return issue(pluginproto.CodeTimeout, err.Error())
	case errors.Is(err, errInvalidResult):
		return issue(pluginproto.CodeInvalidResult, err.Error())
	default:
		return issue(pluginproto.CodeException, safeError(err))
	}
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}
