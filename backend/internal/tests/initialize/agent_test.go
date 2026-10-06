package initialize_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"video-canvas/internal/config"
	. "video-canvas/internal/initialize"
)

func TestParseNodeVersion(t *testing.T) {
	cases := []struct {
		in      string
		wantOK  bool
		wantErr bool
	}{
		{"v23.6.0\n", true, false},
		{"v22.19.0", true, false},
		{"v22.20.1", true, false},
		{"v24.0.0", true, false},
		{"v22.18.9", false, false},
		{"v22.0.0", false, false},
		{"v20.11.1", false, false},
		{"garbage", false, true},
		{"", false, true},
		{"v22", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			ok, err := NodeVersionOK(tc.in)
			if (err != nil) != tc.wantErr || ok != tc.wantOK {
				t.Errorf("NodeVersionOK(%q)=%v,%v 期望 %v,err=%v", tc.in, ok, err, tc.wantOK, tc.wantErr)
			}
		})
	}
}

func TestResolveAgentRuntime_Errors(t *testing.T) {
	t.Run("找不到 node", func(t *testing.T) {
		_, err := ResolveAgentRuntime(config.Agent{Enabled: true, NodePath: "/nonexistent/node", RuntimeDir: t.TempDir()})
		if err == nil || !strings.Contains(err.Error(), "node") {
			t.Errorf("应说明找不到 node: %v", err)
		}
	})

	node, lookErr := exec.LookPath("node")
	if lookErr != nil {
		t.Skip("没有 node，跳过其余用例")
	}

	t.Run("目录里没有 main.mjs", func(t *testing.T) {
		_, err := ResolveAgentRuntime(config.Agent{Enabled: true, NodePath: node, RuntimeDir: t.TempDir()})
		if err == nil || !strings.Contains(err.Error(), "main.mjs") {
			t.Errorf("应指出缺 main.mjs: %v", err)
		}
	})

	t.Run("有脚本但没装依赖：提示 npm ci", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "src"), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "src", "main.mjs"), []byte("//"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := ResolveAgentRuntime(config.Agent{Enabled: true, NodePath: node, RuntimeDir: dir})
		if err == nil || !strings.Contains(err.Error(), "npm ci") {
			t.Errorf("应提示运行 npm ci: %v", err)
		}
	})

	t.Run("完整的目录：解析成绝对路径", func(t *testing.T) {
		rtDir, _ := filepath.Abs("../../../agent-runtime")
		if _, err := os.Stat(filepath.Join(rtDir, "node_modules")); err != nil {
			t.Skip("agent-runtime 没有安装依赖")
		}
		p, err := ResolveAgentRuntime(config.Agent{Enabled: true, NodePath: node, RuntimeDir: rtDir})
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsAbs(p.Node) || !filepath.IsAbs(p.Script) || !strings.HasSuffix(p.Script, filepath.Join("src", "main.mjs")) {
			t.Errorf("应是绝对路径: %+v", p)
		}
	})
}

func TestListenLoopback(t *testing.T) {
	ln, err := ListenLoopback("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if !strings.HasPrefix(ln.Addr().String(), "127.0.0.1:") {
		t.Errorf("addr=%s", ln.Addr())
	}
	// 即使配置校验被绕过，监听这一层也不能绑到非回环地址
	for _, bad := range []string{"0.0.0.0:0", ":0", "[::]:0", "192.168.1.1:0"} {
		if l, err := ListenLoopback(bad); err == nil {
			l.Close()
			t.Errorf("%q 不是回环地址，应拒绝", bad)
		}
	}
}
