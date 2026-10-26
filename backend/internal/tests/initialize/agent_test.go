package initialize_test

import (
	"strings"
	"testing"

	"video-canvas/internal/config"
	. "video-canvas/internal/initialize"
	"video-canvas/internal/provider/webaccess"
	agentsvc "video-canvas/internal/service/agent"
)

func TestAgentWebAccess(t *testing.T) {
	web, disabled := AgentWebAccess(config.Agent{WebEnabled: false}, nil)
	if web != nil || len(disabled) != 2 || disabled[0] != "web_search" || disabled[1] != "web_fetch" {
		t.Errorf("关闭联网：没有联网能力，两个联网工具列入停用清单: %v %v", web, disabled)
	}
	web, disabled = AgentWebAccess(config.Agent{WebEnabled: true}, nil)
	c, ok := web.(*webaccess.Client)
	if !ok || len(disabled) != 0 {
		t.Fatalf("开启联网：有联网能力，没有停用的工具: %T %v", web, disabled)
	}
	if got := c.SearchSources(); len(got) != 3 || got[0] != "exa" {
		t.Errorf("没配 SearXNG 就只用免配置的源: %v", got)
	}
	web, _ = AgentWebAccess(config.Agent{WebEnabled: true, SearXNGURL: "http://searxng:8080"}, nil)
	if got := web.(*webaccess.Client).SearchSources(); len(got) != 4 || got[0] != "searxng" {
		t.Errorf("配了 SearXNG 排第一: %v", got)
	}
}

func TestAgentDisabledTools(t *testing.T) {
	if got := AgentDisabledTools(config.Agent{WebEnabled: true}); len(got) != 0 {
		t.Errorf("联网开着、没配停用: %v", got)
	}
	got := AgentDisabledTools(config.Agent{WebEnabled: false, DisabledTools: []string{"codemode", "web_search"}})
	want := map[string]bool{"codemode": true, "web_search": true, "web_fetch": true}
	if len(got) != 3 {
		t.Fatalf("配置的停用清单加上关联网时的联网工具，去重: %v", got)
	}
	for _, n := range got {
		if !want[n] {
			t.Errorf("不该出现 %q: %v", n, got)
		}
	}
}

// durable 模式：前端接口反向代理给 Agent 服务；别的模式或没启用 Agent 时不装配代理（路由继续走 Go 自己的实现）。
func TestAgentProxyFor(t *testing.T) {
	tok := strings.Repeat("a", 32)
	p0, err0 := AgentProxyFor(config.Agent{Enabled: false, URL: "http://agent:8090", ServiceToken: tok})
	if err0 != nil || p0 != nil {
		t.Errorf("没启用不装配代理: %v %v", p0, err0)
	}
	p, err := AgentProxyFor(config.Agent{Enabled: true, URL: "http://agent:8090", ServiceToken: tok})
	if err != nil || p == nil {
		t.Errorf("启用要装配代理: %v %v", p, err)
	}
	if _, err := AgentProxyFor(config.Agent{Enabled: true, URL: "agent:8090", ServiceToken: tok}); err == nil {
		t.Error("地址不合法应报错")
	}
}

func TestAgentRemoteRunsFor(t *testing.T) {
	tok := strings.Repeat("a", 32)
	if AgentRemoteRunsFor(config.Agent{Enabled: false, URL: "http://agent:8090", ServiceToken: tok}) != nil {
		t.Error("没启用 Agent 就不读 Agent 服务里的运行")
	}
	r, ok := AgentRemoteRunsFor(config.Agent{Enabled: true, URL: "http://agent:8090", ServiceToken: tok}).(*agentsvc.HTTPRemoteRuns)
	if !ok || r.BaseURL != "http://agent:8090" || r.ServiceToken != tok {
		t.Errorf("启用: %+v", r)
	}
}

// 平台 API 是内网信任的入口：没有合格的服务令牌就不监听。
func TestListenBridge(t *testing.T) {
	tok := strings.Repeat("a", 32)
	ln, err := ListenBridge(config.Agent{BridgeAddr: "127.0.0.1:0", ServiceToken: tok})
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()
	for _, bad := range []string{"", "short"} {
		if l, err := ListenBridge(config.Agent{BridgeAddr: "127.0.0.1:0", ServiceToken: bad}); err == nil {
			_ = l.Close()
			t.Errorf("令牌 %q 不合格，不该监听", bad)
		}
	}
}
