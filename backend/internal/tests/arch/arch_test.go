// Package arch_test 是架构测试：用 go/parser 扫描源码，拦住绕过统一日志的写法（设计见 docs/design/统一服务器日志 6.12.4）。
package arch_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// internalDir 是被扫描的 backend/internal 目录（相对本测试所在目录）。
const internalDir = "../.."

// outboundAllowed 是可以自己构造 HTTP 客户端的文件（相对 internal/）。新增必须写原因，并在评审里说明。
var outboundAllowed = map[string]string{
	"pkg/outbound/outbound.go":         "统一出站客户端本身",
	"provider/netguard/netguard.go":    "SSRF 防护传输层本身，是被包装的底层",
	"llmgateway/gateway.go":            "文本模型网关：每次调用由网关观察者记录（llm.call）",
	"provider/plugin/executor.go":      "插件宿主：每次处理由 worker 按钩子追踪记录（provider.call）",
	"provider/plugin/pull_models.go":   "后台拉取渠道模型列表：后台操作，不是用户运行",
	"provider/plugin/runner_client.go": "宿主调用 plugin-runner：内部进程之间的调用，外部调用已由插件宿主记录",
	"service/agent/notify.go":          "Go 调用 Agent 服务：内部进程之间的调用",
	"service/agent/observe_remote.go":  "Go 读取 Agent 服务的运行记录：内部进程之间的调用，供后台查看",
	"updater/client.go":                "Go 调用 updater：运维功能",
	"release/github.go":                "查 GitHub 新版本：运维功能，不是用户运行",
	"storage/probe.go":                 "后台检测存储配置：后台操作，已有操作审计",
	"service/image_processor_check.go": "后台检测图片服务配置：后台操作，已有操作审计",
	"imageproc/tencent.go":             "COS SDK 只用来在本地生成预签名地址，不发请求",
	"provider/webaccess/client.go":     "构造后立即交给 outbound.Client（SearXNG 走内网，不能用 netguard）",
	"handler/agent/proxy.go":           "Go 把前端请求反向代理到 Agent 服务：内部进程之间的调用",
	"initialize/payment.go":            "支付渠道接口：有独立的订单流水和回调记录，请求带签名参数，不进 payload.log",
	"provider/fx/fx.go":                "定时拉取汇率：系统任务，不是用户运行",
}

// netguardAllowed 是可以直接调用 netguard.NewClient / NewTransport 的目录前缀：它们自己负责记录。
var netguardAllowed = []string{"llmgateway/", "provider/plugin/", "pkg/outbound/", "provider/netguard/"}

// riverAllowed 是可以直接调用 river.AddWorker 的文件：只有 queue.AddWorker 的实现。
var riverAllowed = map[string]bool{"provider/queue/runlog.go": true}

// violation 是一处不允许的写法。
type violation struct {
	file string
	line int
	what string
}

func (v violation) String() string { return fmt.Sprintf("%s:%d %s", v.file, v.line, v.what) }

// scan 扫描 root 下的非测试 Go 文件，返回所有违规（文件路径相对 root）。
func scan(t *testing.T, root string) []violation {
	t.Helper()
	var out []violation
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "tests" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		out = append(out, scanFile(fset, f, rel)...)
		return nil
	})
	if err != nil {
		t.Fatalf("扫描源码失败：%v", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// scanFile 检查一个文件。
func scanFile(fset *token.FileSet, f *ast.File, rel string) []violation {
	names := importNames(f)
	httpName, netguardName, riverName, outboundName := names["net/http"], names["video-canvas/internal/provider/netguard"],
		names["github.com/riverqueue/river"], names["video-canvas/internal/pkg/outbound"]

	// 1. 直接交给 outbound.Client / outbound.Transport 的参数范围不算绕过
	type span struct{ from, to token.Pos }
	var wrapped []span
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && len(outboundName) > 0 && isSel(call.Fun, outboundName, "Client", "Transport") {
			wrapped = append(wrapped, span{call.Lparen, call.Rparen})
		}
		return true
	})
	inWrapped := func(p token.Pos) bool {
		for _, s := range wrapped {
			if p > s.from && p < s.to {
				return true
			}
		}
		return false
	}

	var out []violation
	add := func(n ast.Node, what string) {
		out = append(out, violation{file: rel, line: fset.Position(n.Pos()).Line, what: what})
	}
	_, outboundOK := outboundAllowed[rel]
	netguardOK := false
	for _, p := range netguardAllowed {
		netguardOK = netguardOK || strings.HasPrefix(rel, p)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if len(httpName) > 0 && isSel(x.Type, httpName, "Client", "Transport") && !outboundOK && !inWrapped(x.Pos()) {
				add(x, "自己构造了 http.Client / http.Transport：外部调用请用 outbound.Client（E9、E10）")
			}
		case *ast.SelectorExpr:
			if len(httpName) > 0 && isSel(x, httpName, "DefaultClient", "DefaultTransport", "Get", "Post", "PostForm", "Head") && !outboundOK && !inWrapped(x.Pos()) {
				add(x, "用了 http 包的默认客户端：外部调用请用 outbound.Client（E9、E10）")
			}
		case *ast.CallExpr:
			if len(netguardName) > 0 && isSel(x.Fun, netguardName, "NewClient", "NewTransport") && !netguardOK && !outboundOK && !inWrapped(x.Pos()) {
				add(x, "直接用了 netguard 客户端：外部调用请用 outbound.Client 包一层（E10）")
			}
			if len(riverName) > 0 && isSel(x.Fun, riverName, "AddWorker", "AddWorkerSafely") && !riverAllowed[rel] {
				add(x, "直接调用了 river.AddWorker：请实现 queue.RunArgs 并用 queue.AddWorker 注册（E10）")
			}
		}
		return true
	})
	return out
}

// importNames 返回导入路径到文件里使用的包名（含别名；同一个包可能被导入多次）。
func importNames(f *ast.File) map[string][]string {
	out := map[string][]string{}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		out[path] = append(out[path], name)
	}
	return out
}

// isSel 判断表达式是不是 pkg.Name（pkg 是这个包在文件里的任一名字，Name 在 names 里）。
func isSel(e ast.Expr, pkgs []string, names ...string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || !slices.Contains(pkgs, id.Name) {
		return false
	}
	for _, n := range names {
		if sel.Sel.Name == n {
			return true
		}
	}
	return false
}

// 业务代码里不能绕过统一日志：自己发外部 HTTP、直接注册队列任务。
func TestNoBypassOfUnifiedLogging(t *testing.T) {
	for _, v := range scan(t, internalDir) {
		t.Errorf("%s", v)
	}
}

// 白名单里的文件必须还存在，删掉的文件要从白名单里移除，免得白名单越积越宽。
func TestOutboundAllowlistIsCurrent(t *testing.T) {
	for rel, reason := range outboundAllowed {
		if reason == "" {
			t.Errorf("白名单 %s 没写原因", rel)
		}
		if _, err := os.Stat(filepath.Join(internalDir, rel)); err != nil {
			t.Errorf("白名单里的 %s 已不存在，请移除", rel)
		}
	}
}

// 扫描器自检：样例代码里的每种绕过写法都能报出文件和行号，交给 outbound 的不报。
func TestScannerCatchesBypasses(t *testing.T) {
	dir := t.TempDir()
	src := `package sample

import (
	"net/http"
	h2 "net/http"

	"github.com/riverqueue/river"
	"video-canvas/internal/pkg/outbound"
	"video-canvas/internal/provider/netguard"
)

func bad(w *river.Workers) {
	_ = &http.Client{}
	_ = h2.Transport{}
	_, _ = http.Get("http://x")
	_ = http.DefaultClient
	_ = netguard.NewClient(nil, nil, 0)
	river.AddWorker(w, nil)
}

func ok() {
	_ = outbound.Client("x", &http.Client{})
	_ = outbound.Client("x", netguard.NewClient(nil, nil, 0))
	_ = outbound.Transport("x", &http.Transport{})
	var _ *http.Client
}
`
	if err := os.MkdirAll(filepath.Join(dir, "sample"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sample", "sample.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got := scan(t, dir)
	wantLines := []int{13, 14, 15, 16, 17, 18}
	if len(got) != len(wantLines) {
		t.Fatalf("应报 %d 处，实际 %d：%v", len(wantLines), len(got), got)
	}
	lines := map[int]bool{}
	for _, v := range got {
		lines[v.line] = true
		if v.file != "sample/sample.go" {
			t.Errorf("文件路径应相对扫描根目录：%s", v.file)
		}
	}
	for _, l := range wantLines {
		if !lines[l] {
			t.Errorf("第 %d 行应被报出：%v", l, got)
		}
	}
}
