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
	if a.NodePath != "node" || a.RuntimeDir != "./agent-runtime" || a.BridgeAddr != "127.0.0.1:0" {
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
