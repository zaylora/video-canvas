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
		a := validAgent()
		a.WebEnabled, a.SearXNGURL = true, tc.url
		err := a.Validate()
		if (err != nil) != tc.wantErr {
			t.Errorf("searxng_url=%q: err=%v, wantErr=%v", tc.url, err, tc.wantErr)
		}
	}
	a := validAgent()
	a.WebEnabled, a.SearXNGURL = false, "bad"
	if err := a.Validate(); err != nil {
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

// validAgent 是一份完整、合法的 Agent 配置。
func validAgent() Agent {
	return Agent{Enabled: true, URL: "http://agent:8090", BridgeURL: "http://backend:47001", ServiceToken: strings.Repeat("a", 32)}
}

// 画布 Agent 是独立的常驻服务：启用时要知道服务地址、平台 API 对外的地址，并且两边靠服务令牌互相认。
func TestAgentConfig_Validate(t *testing.T) {
	if err := validAgent().Validate(); err != nil {
		t.Fatalf("完整的配置应通过: %v", err)
	}
	cases := []struct {
		name string
		mut  func(a *Agent)
		want string
	}{
		{"没有服务地址", func(a *Agent) { a.URL = "" }, "agent.url"},
		{"服务地址不是 http(s)", func(a *Agent) { a.URL = "agent:8090" }, "agent.url"},
		{"服务地址带路径", func(a *Agent) { a.URL = "http://agent:8090/x" }, "agent.url"},
		{"没有桥地址", func(a *Agent) { a.BridgeURL = "" }, "agent.bridge_url"},
		{"桥地址格式不对", func(a *Agent) { a.BridgeURL = "backend:47001" }, "agent.bridge_url"},
		{"没有服务令牌", func(a *Agent) { a.ServiceToken = "" }, "agent.service_token"},
		{"服务令牌太短", func(a *Agent) { a.ServiceToken = "short" }, "agent.service_token"},
		{"监听地址格式不对", func(a *Agent) { a.BridgeAddr = "47001" }, "agent.bridge_addr"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := validAgent()
			tc.mut(&a)
			if err := a.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("应报含 %q 的错误: %v", tc.want, err)
			}
		})
	}
	// 缺令牌的提示要告诉人怎么办：生成一个，或者关掉 Agent
	err := Agent{Enabled: true, URL: "http://agent:8090", BridgeURL: "http://backend:47001"}.Validate()
	if err == nil || !strings.Contains(err.Error(), "APP_AGENT_ENABLED=false") {
		t.Errorf("提示里要说明不用 Agent 怎么关: %v", err)
	}
}

// 平台 API 监听的是内网地址，Agent 服务在别的容器里要连得上就得是固定端口；显式写了的不被默认值覆盖。
func TestAgentConfig_BridgeAddrDefault(t *testing.T) {
	if got := (Agent{}).WithDefaults().BridgeAddr; got != "0.0.0.0:47001" {
		t.Errorf("默认监听内网的 47001: %q", got)
	}
	if got := (Agent{BridgeAddr: "10.0.0.5:9000"}).WithDefaults().BridgeAddr; got != "10.0.0.5:9000" {
		t.Errorf("显式配置不能被覆盖: %q", got)
	}
}

// 默认配置文件里 Agent 默认关闭（本机直接跑后端时没有 Agent 服务）；环境变量能开启并配齐。
// 默认文件里如果显式写了 bridge_addr 会盖住默认的 47001，Agent 容器就连不上，所以要经过真实的默认配置文件验证。
func TestLoad_AgentSectionAndEnvOverride(t *testing.T) {
	cfg, err := Load("../../../configs/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agent.Enabled {
		t.Error("默认配置里画布 Agent 应是关闭的：本机直接跑后端时没有 Agent 服务，要显式开启")
	}
	t.Setenv("APP_AGENT_ENABLED", "true")
	t.Setenv("APP_AGENT_URL", "http://agent:8090")
	t.Setenv("APP_AGENT_BRIDGE_URL", "http://backend:47001")
	t.Setenv("APP_AGENT_SERVICE_TOKEN", strings.Repeat("t", 40))
	cfg, err = Load("../../../configs/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Agent.Enabled || cfg.Agent.URL != "http://agent:8090" || cfg.Agent.BridgeURL != "http://backend:47001" || cfg.Agent.ServiceToken != strings.Repeat("t", 40) {
		t.Errorf("环境变量应能覆盖: %+v", cfg.Agent)
	}
	if got := cfg.Agent.WithDefaults().BridgeAddr; got != "0.0.0.0:47001" {
		t.Errorf("默认配置下平台 API 应监听内网 47001，实际 %q", got)
	}
}

// 升级时旧的 .env 里可能还留着 APP_AGENT_RUNTIME=process/http 和 node 相关的项：旧运行方式已经没有了，这些项被忽略，不能让启动失败。
func TestLoad_RemovedAgentSettingsAreIgnored(t *testing.T) {
	for _, rt := range []string{"process", "http", "durable"} {
		t.Setenv("APP_AGENT_RUNTIME", rt)
		t.Setenv("APP_AGENT_NODE_PATH", "/opt/node")
		t.Setenv("APP_AGENT_RUNTIME_DIR", "/agent")
		if _, err := Load("../../../configs/config.yaml"); err != nil {
			t.Errorf("APP_AGENT_RUNTIME=%s 不该让启动失败: %v", rt, err)
		}
	}
}
