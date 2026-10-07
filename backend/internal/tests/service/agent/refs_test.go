package agent_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
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
		Runtime: e.rt, Generator: e.gen, Broadcaster: e.bc, Now: func() time.Time { return e.now }, Registry: reg,
	})
	return e
}

func startWith(e *agentEnv, msg string, selection ...string) error {
	_, err := e.svc.StartRun(context.Background(), 1, e.sess, &model.StartAgentRunReq{Message: msg, Selection: selection})
	return err
}

func TestAgentStartRun_ChipValidation(t *testing.T) {
	t.Run("引用都合法：放行，消息原样交给运行时", func(t *testing.T) {
		e := refsEnv(t)
		msg := "把 @[剧本](node:n_script) 拆开，角色用 @[Seedream](model:seedream)，参考 @[拆镜](skill:script-breakdown)"
		if err := startWith(e, msg); err != nil {
			t.Fatal(err)
		}
		if len(e.rt.inputs) != 1 || e.rt.inputs[0].Message != msg {
			t.Errorf("inputs=%+v", e.rt.inputs)
		}
	})

	cases := map[string]struct{ msg, want string }{
		"节点不存在":           {"拆 @[幽灵](node:ghost)", "幽灵"},
		"模型没发布":           {"用 @[某模型](model:nope)", "某模型"},
		"agent 模型不能当生成模型": {"用 @[Claude](model:claude)", "Claude"},
		"技能不存在":           {"参考 @[瞎编](skill:made-up)", "瞎编"},
		"附件暂不支持":          {"看 @[图](asset:1)", "附件"},
	}
	for name, c := range cases {
		t.Run(name+"：10001，写明是哪个引用，不创建运行", func(t *testing.T) {
			e := refsEnv(t)
			err := startWith(e, c.msg)
			wantAgentCode(t, err, errcode.ErrInvalidParams)
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("错误应点名 %q: %v", c.want, err)
			}
			e.repo.mu.Lock()
			runs := len(e.repo.runs)
			e.repo.mu.Unlock()
			if len(e.rt.started) != 0 || runs != 0 {
				t.Error("校验不过不能创建运行、不能启动 runtime")
			}
		})
	}

	t.Run("引用太多", func(t *testing.T) {
		e := refsEnv(t)
		msg := strings.Repeat("@[剧本](node:n_script) ", 1) + func() string {
			var b strings.Builder
			for i := 0; i < 60; i++ {
				b.WriteString("@[x](skill:s" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ") ")
			}
			return b.String()
		}()
		wantAgentCode(t, startWith(e, msg), errcode.ErrInvalidParams)
	})

	t.Run("选中的节点里已经不存在的（界面状态过期）悄悄去掉，不拦发送", func(t *testing.T) {
		e := refsEnv(t)
		if err := startWith(e, "优化一下", "n_script", "ghost"); err != nil {
			t.Fatal(err)
		}
		if got := e.rt.inputs[0].Selection; len(got) != 1 || got[0] != "n_script" {
			t.Errorf("selection=%v", got)
		}
	})

	t.Run("没有配置注册表：模型引用不校验，节点和技能照常校验", func(t *testing.T) {
		e := newAgentEnv(t)
		if err := startWith(e, "用 @[随便](model:whatever)"); err != nil {
			t.Fatal(err)
		}
		wantAgentCode(t, startWith(e, "拆 @[幽灵](node:ghost)"), errcode.ErrInvalidParams)
	})
}

func TestAgentInterject_ChipValidation(t *testing.T) {
	e := refsEnv(t)
	run := e.runningRun(t)
	err := e.svc.Interject(context.Background(), 1, run.ID, "再加上 @[幽灵](node:ghost)")
	wantAgentCode(t, err, errcode.ErrInvalidParams)
	if len(e.rt.interjected) != 0 {
		t.Error("校验不过不能交给 runtime")
	}
	if err := e.svc.Interject(context.Background(), 1, run.ID, "再加上 @[剧本](node:n_script)"); err != nil {
		t.Fatal(err)
	}
}

func TestAgentStartRun_ImportedSkillChip(t *testing.T) {
	se := newSkillEnv(t)
	ctx := context.Background()
	mustConfirm(t, se, 1, standardPkg(t, "demo", "x"))

	e := newAgentEnv(t)
	e.svc = NewAgentService(AgentDeps{
		Repo: e.repo, Canvas: NewAgentCanvasService(e.repo, e.bc), Models: fakeAgentModels{list: defaultAgentModels},
		Runtime: e.rt, Broadcaster: e.bc, Skills: se.svc, Now: func() time.Time { return e.now },
	})
	msg := "用 @[演示](skill:demo) 做"
	err := startWith(e, msg)
	wantAgentCode(t, err, errcode.ErrInvalidParams) // 默认停用：被拒，点名技能
	if err == nil || !strings.Contains(err.Error(), "演示") {
		t.Errorf("应点名被拒的技能: %v", err)
	}
	_, _ = se.svc.SetEnabled(ctx, 1, "demo", true)
	se.svc.Sweep(ctx)
	// 启用立刻生效（本实例写操作使目录缓存失效）
	if err := startWith(e, msg); err != nil {
		t.Errorf("启用后应放行: %v", err)
	}
}
