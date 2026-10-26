package agent_test

import (
	"testing"
	"time"

	"video-canvas/internal/provider"
	. "video-canvas/internal/service/agent"
)

func TestParseAgentChips(t *testing.T) {
	got := ParseAgentChips("把 @[剧本](node:n_1) 拆开，角色用 @[A](model:a)，参考 @[拆镜](skill:script-breakdown)，再看 @[剧本](node:n_1)")
	want := []AgentChip{{Type: "node", ID: "n_1", Name: "剧本"}, {Type: "model", ID: "a", Name: "A"}, {Type: "skill", ID: "script-breakdown", Name: "拆镜"}}
	if len(got) != len(want) {
		t.Fatalf("同一个引用只算一次: got=%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 个: got=%+v want=%+v", i, got[i], want[i])
		}
	}
	for _, text := range []string{"没有引用", "@[x](video:1)", "@[x](node:)", "@x(node:1)"} {
		if len(ParseAgentChips(text)) != 0 {
			t.Errorf("%q 不是合法写法，不应解析出引用", text)
		}
	}
}

// refsEnv 是带模型注册表的环境：已发布 seedream（图片）和 claude（agent）。
func refsEnv(t *testing.T) *agentEnv {
	t.Helper()
	e := newAgentEnv(t)
	reg := &fakeAgentRegistry{models: []provider.ModelInfo{
		{Key: "seedream", Kind: "image", Label: "Seedream"},
		{Key: "seedance", Kind: "video", Label: "Seedance"},
		{Key: "claude", Kind: "agent", Label: "Claude"},
	}}
	e.svc = NewAgentService(AgentDeps{
		Repo: e.repo, Canvas: NewAgentCanvasService(e.repo, e.bc), Models: fakeAgentModels{list: defaultAgentModels},
		Generator: e.gen, Now: func() time.Time { return e.now }, Registry: reg,
	})
	return e
}
