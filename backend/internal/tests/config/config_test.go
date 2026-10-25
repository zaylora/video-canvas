package config_test

import (
	"os"
	"strings"
	"testing"

	. "video-canvas/internal/config"
)

// 并发上限、初始积分、登录记录保留天数已改为后台「注册设置」的库值 + service 里的代码常量，
// 默认配置文件里不应再出现这些键，且文件仍能正常加载。
func TestDefaultConfig_NoRemovedKeys(t *testing.T) {
	const path = "../../../configs/config.yaml"
	if _, err := Load(path); err != nil {
		t.Fatalf("默认配置应能加载：%v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"max_active_tasks_per_user", "initial_credits", "login_log_retention_days", "\nuser:"} {
		if strings.Contains(string(raw), key) {
			t.Errorf("默认配置不应再包含 %q", key)
		}
	}
}

func TestAgentConfig_Defaults(t *testing.T) {
	a := Agent{Enabled: true}.WithDefaults()
	if a.NodePath != "node" || a.RuntimeDir != "../agent" || a.BridgeAddr != "127.0.0.1:0" {
		t.Errorf("默认值不对: %+v", a)
	}
	custom := Agent{Enabled: true, NodePath: "/opt/node", RuntimeDir: "/srv/rt", BridgeAddr: "127.0.0.1:9001", WorkDir: "/tmp/x"}.WithDefaults()
	if custom.NodePath != "/opt/node" || custom.RuntimeDir != "/srv/rt" || custom.BridgeAddr != "127.0.0.1:9001" || custom.WorkDir != "/tmp/x" {
		t.Errorf("已有的值不能被默认值覆盖: %+v", custom)
	}
}

// 桥的端点用一次性令牌鉴权，但它只该被本机的 Node 进程访问：监听地址必须是回环地址，
// 配成 0.0.0.0 或外网地址会让令牌之外没有任何屏障，所以直接拒绝启动。
func TestAgentConfig_BridgeAddrMustBeLoopback(t *testing.T) {
	cases := []struct {
		addr    string
		wantErr bool
	}{
		{"127.0.0.1:0", false},
		{"127.0.0.1:47001", false},
		{"[::1]:47001", false},
		{"localhost:47001", false},
		{"0.0.0.0:47001", true},
		{":47001", true},
		{"[::]:47001", true},
		{"192.168.1.10:47001", true},
		{"10.0.0.5:47001", true},
		{"example.com:47001", true},
		{"not-an-address", true},
		{"127.0.0.1", true},
	}
	for _, tc := range cases {
		t.Run(tc.addr, func(t *testing.T) {
			err := Agent{Enabled: true, BridgeAddr: tc.addr}.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate(%q) err=%v，期望出错=%v", tc.addr, err, tc.wantErr)
			}
		})
	}
}

// 配了服务令牌，桥才允许监听非回环地址（Agent 拆成独立容器后要从内网访问）：
// 令牌是唯一的屏障，所以必须足够长；没配令牌时仍只允许回环。
func TestAgentConfig_ServiceTokenOpensNonLoopbackBridge(t *testing.T) {
	long := strings.Repeat("a", 32)
	cases := []struct {
		name    string
		addr    string
		token   string
		wantErr bool
	}{
		{"回环不配令牌", "127.0.0.1:0", "", false},
		{"回环配了合格的令牌", "127.0.0.1:0", long, false},
		{"非回环不配令牌", "0.0.0.0:47001", "", true},
		{"非回环令牌太短", "0.0.0.0:47001", strings.Repeat("a", 31), true},
		{"非回环令牌够长", "0.0.0.0:47001", long, false},
		{"内网地址加令牌", "192.168.1.10:47001", long, false},
		{"容器名加令牌", "agent-bridge:47001", long, false},
		{"回环但令牌太短也拒绝", "127.0.0.1:0", "short", true},
		{"地址格式不对", "not-an-address", long, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Agent{Enabled: true, BridgeAddr: tc.addr, ServiceToken: tc.token}.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate(%q, token=%d 字符) err=%v，期望出错=%v", tc.addr, len(tc.token), err, tc.wantErr)
			}
		})
	}
	if err := (Agent{Enabled: true, BridgeAddr: "0.0.0.0:1"}).Validate(); err == nil || strings.Contains(err.Error(), "aaaa") {
		t.Errorf("报错里不能带出令牌内容: %v", err)
	}
}

func TestLoad_AgentServiceTokenFromEnv(t *testing.T) {
	cfg, err := Load("../../../configs/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.ServiceToken != "" {
		t.Errorf("默认不配服务令牌: %q", cfg.Agent.ServiceToken)
	}
	t.Setenv("APP_AGENT_SERVICE_TOKEN", strings.Repeat("t", 40))
	cfg, err = Load("../../../configs/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.ServiceToken != strings.Repeat("t", 40) {
		t.Error("服务令牌应能用环境变量 APP_AGENT_SERVICE_TOKEN 提供（不写进 YAML）")
	}
}

func TestAgentConfig_DisabledSkipsValidation(t *testing.T) {
	if err := (Agent{Enabled: false, BridgeAddr: "0.0.0.0:1"}).Validate(); err != nil {
		t.Errorf("没启用就不校验（配置里留着无效值也不该挡住启动）: %v", err)
	}
}

func TestLoad_AgentSectionAndEnvOverride(t *testing.T) {
	cfg, err := Load("../../../configs/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.Enabled {
		t.Error("默认配置里画布 Agent 应是关闭的：它依赖 Node，要显式开启")
	}
	t.Setenv("APP_AGENT_ENABLED", "true")
	t.Setenv("APP_AGENT_NODE_PATH", "/opt/node")
	cfg, err = Load("../../../configs/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Agent.Enabled || cfg.Agent.NodePath != "/opt/node" {
		t.Errorf("环境变量应能覆盖: %+v", cfg.Agent)
	}
}

// ai.allow_fake_ip：开发机上代理工具的 Fake-IP 模式会把所有域名解析到 198.18.0.0/15，默认关（线上保持严格），
// 开发环境用环境变量 APP_AI_ALLOW_FAKE_IP 打开；YAML 里没写这一项，也必须能被环境变量覆盖。
func TestLoad_AllowFakeIP(t *testing.T) {
	cfg, err := Load("../../../configs/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.AllowFakeIP {
		t.Error("默认必须是关闭的：线上不放行 198.18.0.0/15")
	}
	t.Setenv("APP_AI_ALLOW_FAKE_IP", "true")
	cfg, err = Load("../../../configs/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AI.AllowFakeIP {
		t.Error("环境变量 APP_AI_ALLOW_FAKE_IP=true 应能打开")
	}
}

// update：检查新版本默认开、默认仓库；老部署挂载的配置文件里没有 update 段也一样；环境变量能关闭。
func TestLoad_UpdateDefaultsAndEnvOverride(t *testing.T) {
	cfg, err := Load("../../../configs/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Update.CheckEnabled || cfg.Update.Repo != DefaultUpdateRepo {
		t.Fatalf("默认应开启检查、使用默认仓库：%+v", cfg.Update)
	}

	path := t.TempDir() + "/old.yaml"
	if err := os.WriteFile(path, []byte("server:\n  port: 8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Update.CheckEnabled || cfg.Update.Repo != DefaultUpdateRepo {
		t.Fatalf("配置文件里没有 update 段时也应取默认值：%+v", cfg.Update)
	}

	if cfg.Update.UpdaterSocket != "/run/updater/updater.sock" || cfg.Update.UpdaterToken != "" {
		t.Fatalf("updater 默认 socket 固定、token 为空（未启用）：%+v", cfg.Update)
	}

	t.Setenv("APP_UPDATE_UPDATER_TOKEN", "tok")
	t.Setenv("APP_UPDATE_CHECK_ENABLED", "false")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Update.CheckEnabled {
		t.Fatal("APP_UPDATE_CHECK_ENABLED=false 应能关闭")
	}
	if cfg.Update.UpdaterToken != "tok" {
		t.Fatal("APP_UPDATE_UPDATER_TOKEN 应能设置 token（YAML 里没有这一项）")
	}
}

// 联网工具（搜索、读网页）默认开启，用 APP_AGENT_WEB_ENABLED 关闭；SearXNG 地址可选，用 APP_AGENT_SEARXNG_URL 配。
func TestLoad_AgentWebSettings(t *testing.T) {
	cfg, err := Load("../../../configs/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Agent.WebEnabled {
		t.Error("联网默认应开启")
	}
	if cfg.Agent.SearXNGURL != "" {
		t.Errorf("SearXNG 默认不配: %q", cfg.Agent.SearXNGURL)
	}
	t.Setenv("APP_AGENT_WEB_ENABLED", "false")
	t.Setenv("APP_AGENT_SEARXNG_URL", "http://searxng:8080")
	cfg, err = Load("../../../configs/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.WebEnabled || cfg.Agent.SearXNGURL != "http://searxng:8080" {
		t.Errorf("环境变量应能覆盖: %+v", cfg.Agent)
	}
}

func TestAgentConfig_SearXNGURLMustBeHTTP(t *testing.T) {
	for _, tc := range []struct {
		url     string
		wantErr bool
	}{
		{"", false}, {"http://searxng:8080", false}, {"https://search.example.com", false},
		{"searxng:8080", true}, {"ftp://searxng", true}, {"file:///etc/passwd", true}, {"http://", true},
	} {
		err := Agent{Enabled: true, WebEnabled: true, SearXNGURL: tc.url}.Validate()
		if (err != nil) != tc.wantErr {
			t.Errorf("searxng_url=%q: err=%v, wantErr=%v", tc.url, err, tc.wantErr)
		}
	}
	if err := (Agent{Enabled: true, WebEnabled: false, SearXNGURL: "bad"}).Validate(); err != nil {
		t.Errorf("联网关闭时不校验 SearXNG 地址: %v", err)
	}
}

// agent.disabled_tools：运维想关掉某个工具（比如 codemode）时在配置里加一项；Node 靠它决定启用哪些 Agent 工具。
func TestLoad_AgentDisabledTools(t *testing.T) {
	cfg, err := Load("../../../configs/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Agent.DisabledTools) != 0 {
		t.Errorf("默认不停用任何工具: %v", cfg.Agent.DisabledTools)
	}
	t.Setenv("APP_AGENT_DISABLED_TOOLS", "codemode,web_fetch")
	cfg, err = Load("../../../configs/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Agent.DisabledTools; len(got) != 2 || got[0] != "codemode" || got[1] != "web_fetch" {
		t.Errorf("环境变量应能覆盖（逗号分隔）: %v", got)
	}
}
