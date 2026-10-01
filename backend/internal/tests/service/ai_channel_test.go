package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	. "video-canvas/internal/service"
	"video-canvas/internal/service/aiconfigfake"
)

// achEnv 是渠道服务测试的环境：真实的凭证服务（AES-GCM）+ 内存仓储 + 替身的插件宿主。
type achEnv struct {
	svc      *AIChannelService
	cfg      *AIConfigService
	repo     *aiconfigfake.MemRepo
	ops      *aiconfigfake.PluginOps
	notifier *aiconfigfake.Notifier
	v1       uint64 // kling@1.0.0（bearer，要求 tenant）
	v2       uint64 // kling@1.1.0
	loose    uint64 // loose@1.0.0（鉴权 none，没有渠道设置项）
}

func newAchEnv(t *testing.T) *achEnv {
	t.Helper()
	repo := aiconfigfake.NewMemRepo()
	cfg := NewAIConfigService(repo, repo, repo, "test-master-key")
	ops := &aiconfigfake.PluginOps{}
	notifier := &aiconfigfake.Notifier{}
	e := &achEnv{svc: NewAIChannelService(repo, repo, cfg, repo, ops, notifier), cfg: cfg, repo: repo, ops: ops, notifier: notifier}
	e.v1 = adminSeedVersion(t, repo, adminMetaJSON("kling", "1.0.0"), model.PluginSourceUploaded, true)
	e.v2 = adminSeedVersion(t, repo, adminMetaJSON("kling", "1.1.0"), model.PluginSourceUploaded, true)
	e.loose = adminSeedVersion(t, repo, adminMetaLoose("loose", "1.0.0"), model.PluginSourceUploaded, true)
	return e
}

// createInput 是一份合法的新建参数，用例在它的基础上改。
func createInput() ChannelCreateInput {
	return ChannelCreateInput{
		Key: "kling-main", Name: "可灵主渠道", PluginKey: "kling", PluginVersion: "1.0.0",
		BaseURL: "https://gw.example.com", Settings: map[string]any{"tenant": "t1"}, Enabled: true, ActorID: 5,
	}
}

func TestAIChannelService_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("成功：固定到版本、补全默认设置、写审计并刷新 Registry", func(t *testing.T) {
		e := newAchEnv(t)
		in := createInput()
		in.RateLimit = provider.RateLimit{RPS: 5, MaxConcurrency: 20}
		view, err := e.svc.Create(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if view.Key != "kling-main" || view.PluginKey != "kling" || view.PluginVersionID != e.v1 || view.PluginVersion != "1.0.0" ||
			view.BaseURL != "https://gw.example.com" || !view.Enabled || view.SecretSet || view.UpdatedBy != 5 {
			t.Fatalf("视图不符合预期：%+v", view)
		}
		if view.Settings["tenant"] != "t1" || view.Settings["region"] != "cn" {
			t.Fatalf("settings 应带上默认值 region=cn：%v", view.Settings)
		}
		if view.RateLimit.RPS != 5 || view.RateLimit.MaxConcurrency != 20 {
			t.Fatalf("限流不符合预期：%+v", view.RateLimit)
		}
		if got := e.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditChannelCreate {
			t.Fatalf("审计不符合预期：%v", got)
		}
		if len(e.notifier.Reasons) != 1 {
			t.Fatalf("应刷新 Registry：%v", e.notifier.Reasons)
		}
	})

	t.Run("enabled=false 与两个安全开关：创建时同样留痕", func(t *testing.T) {
		e := newAchEnv(t)
		in := createInput()
		in.Enabled, in.TrustedInternal, in.AllowCredentials = false, true, true
		view, err := e.svc.Create(ctx, in)
		if err != nil || view.Enabled || !view.TrustedInternal || !view.AllowCredentials {
			t.Fatalf("结果不符合预期：%+v %v", view, err)
		}
		got := strings.Join(e.repo.AuditActions(), ",")
		want := strings.Join([]string{model.AuditChannelCreate, model.AuditChannelTrusted, model.AuditChannelCred}, ",")
		if got != want {
			t.Fatalf("审计应为 %s，实际 %s", want, got)
		}
	})

	t.Run("key 重复 409（50012）", func(t *testing.T) {
		e := newAchEnv(t)
		if _, err := e.svc.Create(ctx, createInput()); err != nil {
			t.Fatal(err)
		}
		_, err := e.svc.Create(ctx, createInput())
		adminWantCode(t, err, errcode.ErrChannelExists.Code)
		if len(e.repo.Channels) != 1 {
			t.Fatal("不该新增渠道")
		}
	})

	invalid := []struct {
		name    string
		mutate  func(e *achEnv, in *ChannelCreateInput)
		wantMsg string
	}{
		{"key 含大写", func(_ *achEnv, in *ChannelCreateInput) { in.Key = "Kling" }, "key"},
		{"key 以短横线开头", func(_ *achEnv, in *ChannelCreateInput) { in.Key = "-a" }, "key"},
		{"key 超长", func(_ *achEnv, in *ChannelCreateInput) { in.Key = strings.Repeat("a", 65) }, "key"},
		{"key 含下划线", func(_ *achEnv, in *ChannelCreateInput) { in.Key = "a_b" }, "key"},
		{"name 为空白", func(_ *achEnv, in *ChannelCreateInput) { in.Name = "   " }, "name"},
		{"base_url 不是 http(s)", func(_ *achEnv, in *ChannelCreateInput) { in.BaseURL = "ftp://x.com" }, "http"},
		{"base_url 没有协议", func(_ *achEnv, in *ChannelCreateInput) { in.BaseURL = "gw.example.com" }, "base_url"},
		{"base_url 没有主机", func(_ *achEnv, in *ChannelCreateInput) { in.BaseURL = "https://" }, "base_url"},
		{"base_url 带用户名密码", func(_ *achEnv, in *ChannelCreateInput) { in.BaseURL = "https://u:p@x.com" }, "用户名"},
		{"base_url 只带用户名", func(_ *achEnv, in *ChannelCreateInput) { in.BaseURL = "https://u@x.com" }, "用户名"},
		{"base_url 带查询参数", func(_ *achEnv, in *ChannelCreateInput) { in.BaseURL = "https://x.com/?k=v" }, "查询"},
		{"base_url 带片段", func(_ *achEnv, in *ChannelCreateInput) { in.BaseURL = "https://x.com/#a" }, "片段"},
		{"插件版本不存在", func(_ *achEnv, in *ChannelCreateInput) { in.PluginVersion = "9.9.9" }, "不存在"},
		{"插件不存在", func(_ *achEnv, in *ChannelCreateInput) { in.PluginKey = "nope" }, "不存在"},
		{"没填插件版本", func(_ *achEnv, in *ChannelCreateInput) { in.PluginVersion = "" }, "plugin_version"},
		{"插件已停用", func(e *achEnv, _ *ChannelCreateInput) { _ = e.repo.SetPluginEnabled(ctx, "kling", false) }, "停用"},
		{"缺少必填设置项", func(_ *achEnv, in *ChannelCreateInput) { in.Settings = nil }, "settings.tenant"},
		{"未声明的设置项", func(_ *achEnv, in *ChannelCreateInput) { in.Settings["zzz"] = 1 }, "settings.zzz"},
		{"enum 取值不在 options", func(_ *achEnv, in *ChannelCreateInput) { in.Settings["region"] = "mars" }, "settings.region"},
		{"类型不对（string 填了数字）", func(_ *achEnv, in *ChannelCreateInput) { in.Settings["tenant"] = 3.0 }, "settings.tenant"},
		{"类型不对（number 填了字符串）", func(_ *achEnv, in *ChannelCreateInput) { in.Settings["burst"] = "x" }, "settings.burst"},
		{"类型不对（boolean 填了字符串）", func(_ *achEnv, in *ChannelCreateInput) { in.Settings["debug"] = "yes" }, "settings.debug"},
		{"rps 为负", func(_ *achEnv, in *ChannelCreateInput) { in.RateLimit.RPS = -1 }, "rate_limit"},
		{"max_concurrency 为负", func(_ *achEnv, in *ChannelCreateInput) { in.RateLimit.MaxConcurrency = -1 }, "rate_limit"},
	}
	for _, tt := range invalid {
		t.Run("校验失败 400（50013）："+tt.name, func(t *testing.T) {
			e := newAchEnv(t)
			in := createInput()
			tt.mutate(e, &in)
			_, err := e.svc.Create(ctx, in)
			ec := adminWantCodeErr(t, err, errcode.ErrChannelInvalid.Code)
			if !strings.Contains(ec.Msg, tt.wantMsg) {
				t.Fatalf("msg 应包含 %q：%s", tt.wantMsg, ec.Msg)
			}
			if len(e.repo.Channels) != 0 || len(e.repo.Audits) != 0 || len(e.notifier.Reasons) != 0 {
				t.Fatal("校验失败不该落库、写审计或刷新 Registry")
			}
		})
	}

	t.Run("多个问题一次报出", func(t *testing.T) {
		e := newAchEnv(t)
		in := createInput()
		in.Key, in.BaseURL, in.Settings = "BAD", "ftp://x", nil
		_, err := e.svc.Create(ctx, in)
		ec := adminWantCodeErr(t, err, errcode.ErrChannelInvalid.Code)
		for _, want := range []string{"key", "http", "settings.tenant"} {
			if !strings.Contains(ec.Msg, want) {
				t.Fatalf("msg 应包含 %q：%s", want, ec.Msg)
			}
		}
	})

	t.Run("没有渠道设置项的插件：空 settings 通过，任何取值都被拒绝", func(t *testing.T) {
		e := newAchEnv(t)
		in := createInput()
		in.Key, in.PluginKey, in.Settings = "loose-1", "loose", nil
		if _, err := e.svc.Create(ctx, in); err != nil {
			t.Fatalf("空 settings 应通过：%v", err)
		}
		in.Key, in.Settings = "loose-2", map[string]any{"a": 1}
		_, err := e.svc.Create(ctx, in)
		adminWantCode(t, err, errcode.ErrChannelInvalid.Code)
	})

	t.Run("插件版本在写库时已被删除：按不合法返回而不是 500", func(t *testing.T) {
		e := newAchEnv(t)
		in := createInput()
		// 让 FindVersion 能找到、CreateChannel 时却找不到：直接删掉 fake 里的版本行不经过 FindVersion 是不可能的，
		// 这里用“版本行在校验后消失”的替身仓储模拟
		svc := NewAIChannelService(vanishingRepo{e.repo, e.v1}, e.repo, e.cfg, e.repo, e.ops, e.notifier)
		_, err := svc.Create(ctx, in)
		adminWantCode(t, err, errcode.ErrChannelInvalid.Code)
	})
}

// vanishingRepo 让 CreateChannel 总是报“插件版本不存在”，模拟校验之后版本被并发删除。
type vanishingRepo struct {
	*aiconfigfake.MemRepo
	id uint64
}

func (v vanishingRepo) CreateChannel(ctx context.Context, c *model.AIChannel) error {
	delete(v.Versions, v.id)
	return v.MemRepo.CreateChannel(ctx, c)
}

func TestAIChannelService_Update(t *testing.T) {
	ctx := context.Background()
	seed := func(t *testing.T, e *achEnv) {
		t.Helper()
		if _, err := e.svc.Create(ctx, createInput()); err != nil {
			t.Fatal(err)
		}
		e.repo.Audits = nil
		e.notifier.Reasons = nil
	}
	str := func(s string) *string { return &s }
	yes, no := true, false

	t.Run("只改名字：其余字段不动，只写一条 update 审计", func(t *testing.T) {
		e := newAchEnv(t)
		seed(t, e)
		view, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", Name: str("新名字"), ActorID: 8})
		if err != nil {
			t.Fatal(err)
		}
		if view.Name != "新名字" || view.BaseURL != "https://gw.example.com" || view.PluginVersion != "1.0.0" || view.UpdatedBy != 8 ||
			view.Settings["tenant"] != "t1" || !view.Enabled {
			t.Fatalf("视图不符合预期：%+v", view)
		}
		if got := e.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditChannelUpdate {
			t.Fatalf("审计不符合预期：%v", got)
		}
		if !strings.Contains(string(e.repo.Audits[0].DetailJSON), `"name"`) {
			t.Fatalf("审计应记被改的字段名：%s", e.repo.Audits[0].DetailJSON)
		}
		if len(e.notifier.Reasons) != 1 {
			t.Fatalf("应刷新 Registry：%v", e.notifier.Reasons)
		}
	})

	t.Run("什么都没改：不写审计", func(t *testing.T) {
		e := newAchEnv(t)
		seed(t, e)
		if _, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", ActorID: 8}); err != nil {
			t.Fatal(err)
		}
		if len(e.repo.Audits) != 0 {
			t.Fatalf("没有变化不该写审计：%v", e.repo.AuditActions())
		}
	})

	t.Run("切换插件版本：settings 按新版本重新校验，审计记前后版本", func(t *testing.T) {
		e := newAchEnv(t)
		seed(t, e)
		view, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", PluginVersion: str("1.1.0"), ActorID: 8})
		if err != nil {
			t.Fatal(err)
		}
		if view.PluginVersionID != e.v2 || view.PluginVersion != "1.1.0" {
			t.Fatalf("应切到 1.1.0：%+v", view)
		}
		detail := string(e.repo.Audits[0].DetailJSON)
		if !strings.Contains(detail, `"from_version":"1.0.0"`) || !strings.Contains(detail, `"to_version":"1.1.0"`) {
			t.Fatalf("审计应记前后版本：%s", detail)
		}
	})

	t.Run("切换到 settings 不兼容的版本被拒绝，渠道保持原样", func(t *testing.T) {
		e := newAchEnv(t)
		seed(t, e)
		// 1.2.0 新增了必填项 must
		meta := json.RawMessage(`{"apiVersion":1,"key":"kling","name":"k","version":"1.2.0","auth":{"type":"bearer"},"endpoints":{"video":{"mode":"async"}},` +
			`"channelSettings":{"must":{"type":"string","label":"必填","required":true}}}`)
		adminSeedVersion(t, e.repo, meta, model.PluginSourceUploaded, true)
		_, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", PluginVersion: str("1.2.0")})
		ec := adminWantCodeErr(t, err, errcode.ErrChannelInvalid.Code)
		if !strings.Contains(ec.Msg, "settings.must") || !strings.Contains(ec.Msg, "settings.tenant") {
			t.Fatalf("应报出必填项与未声明项：%s", ec.Msg)
		}
		if e.repo.Channels["kling-main"].PluginVersionID != e.v1 {
			t.Fatal("渠道不该被改动")
		}
	})

	t.Run("同时切版本并给出新的 settings", func(t *testing.T) {
		e := newAchEnv(t)
		seed(t, e)
		meta := json.RawMessage(`{"apiVersion":1,"key":"kling","name":"k","version":"1.2.0","auth":{"type":"bearer"},"endpoints":{"video":{"mode":"async"}},` +
			`"channelSettings":{"must":{"type":"string","label":"必填","required":true}}}`)
		adminSeedVersion(t, e.repo, meta, model.PluginSourceUploaded, true)
		view, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", PluginVersion: str("1.2.0"), Settings: map[string]any{"must": "x"}})
		if err != nil || view.Settings["must"] != "x" || len(view.Settings) != 1 {
			t.Fatalf("结果不符合预期：%+v %v", view, err)
		}
	})

	t.Run("只改 settings 也要校验", func(t *testing.T) {
		e := newAchEnv(t)
		seed(t, e)
		_, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", Settings: map[string]any{"tenant": "t", "region": "mars"}})
		adminWantCode(t, err, errcode.ErrChannelInvalid.Code)
		view, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", Settings: map[string]any{"tenant": "t2", "region": "global"}})
		if err != nil || view.Settings["region"] != "global" || view.Settings["tenant"] != "t2" {
			t.Fatalf("结果不符合预期：%+v %v", view, err)
		}
	})

	t.Run("切到已停用插件的版本被拒绝；不切版本时停用的插件不挡其他修改", func(t *testing.T) {
		e := newAchEnv(t)
		seed(t, e)
		_ = e.repo.SetPluginEnabled(ctx, "kling", false)
		_, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", PluginVersion: str("1.1.0")})
		adminWantCode(t, err, errcode.ErrChannelInvalid.Code)
		// 重复指定当前版本不算切换
		if _, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", PluginVersion: str("1.0.0"), Enabled: &no}); err != nil {
			t.Fatalf("停用渠道不该被停用的插件挡住：%v", err)
		}
	})

	t.Run("安全开关与启停：各写一条带前后值的审计", func(t *testing.T) {
		e := newAchEnv(t)
		seed(t, e)
		view, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", TrustedInternal: &yes, AllowCredentials: &yes, Enabled: &no, ActorID: 9})
		if err != nil || !view.TrustedInternal || !view.AllowCredentials || view.Enabled {
			t.Fatalf("结果不符合预期：%+v %v", view, err)
		}
		got := strings.Join(e.repo.AuditActions(), ",")
		want := strings.Join([]string{model.AuditChannelTrusted, model.AuditChannelCred, model.AuditChannelEnable}, ",")
		if got != want {
			t.Fatalf("审计应为 %s，实际 %s", want, got)
		}
		if d := string(e.repo.Audits[0].DetailJSON); !strings.Contains(d, `"from":false`) || !strings.Contains(d, `"to":true`) {
			t.Fatalf("审计应带前后值：%s", d)
		}
		// 再次设成同样的值：没有变化，不再写审计
		e.repo.Audits = nil
		if _, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", TrustedInternal: &yes}); err != nil {
			t.Fatal(err)
		}
		if len(e.repo.Audits) != 0 {
			t.Fatalf("值没变不该写审计：%v", e.repo.AuditActions())
		}
	})

	t.Run("限流可以改、也可以清零", func(t *testing.T) {
		e := newAchEnv(t)
		seed(t, e)
		view, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", RateLimit: &provider.RateLimit{RPS: 2, MaxConcurrency: 3}})
		if err != nil || view.RateLimit.RPS != 2 || view.RateLimit.MaxConcurrency != 3 {
			t.Fatalf("结果不符合预期：%+v %v", view, err)
		}
		view, err = e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", RateLimit: &provider.RateLimit{}})
		if err != nil || view.RateLimit.RPS != 0 {
			t.Fatalf("应可清零：%+v %v", view, err)
		}
	})

	invalid := []struct {
		name string
		in   ChannelUpdateInput
	}{
		{"name 为空", ChannelUpdateInput{Name: str(" ")}},
		{"base_url 非法", ChannelUpdateInput{BaseURL: str("file:///etc/passwd")}},
		{"base_url 带凭证", ChannelUpdateInput{BaseURL: str("https://a:b@x.com")}},
		{"版本不存在", ChannelUpdateInput{PluginVersion: str("0.0.1")}},
		{"rate_limit 为负", ChannelUpdateInput{RateLimit: &provider.RateLimit{RPS: -0.5}}},
	}
	for _, tt := range invalid {
		t.Run("校验失败 400（50013）："+tt.name, func(t *testing.T) {
			e := newAchEnv(t)
			seed(t, e)
			tt.in.Key = "kling-main"
			_, err := e.svc.Update(ctx, tt.in)
			adminWantCode(t, err, errcode.ErrChannelInvalid.Code)
			if len(e.repo.Audits) != 0 {
				t.Fatal("失败不该写审计")
			}
		})
	}

	t.Run("渠道不存在 404（50011）", func(t *testing.T) {
		e := newAchEnv(t)
		_, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "nope", Name: str("x")})
		adminWantCode(t, err, errcode.ErrChannelNotFound.Code)
	})

	t.Run("当前版本已丢失：不指定版本的修改被拒绝，指定可用版本后能修复", func(t *testing.T) {
		e := newAchEnv(t)
		seed(t, e)
		delete(e.repo.Versions, e.v1)
		// 仓储写库时要锁住渠道指向的版本行，版本没了就写不进去
		_, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", Name: str("改名")})
		adminWantCode(t, err, errcode.ErrChannelInvalid.Code)
		_, err = e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", Settings: map[string]any{"tenant": "x"}})
		adminWantCode(t, err, errcode.ErrChannelInvalid.Code)
		if _, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "kling-main", PluginVersion: str("1.1.0"), Settings: map[string]any{"tenant": "x"}}); err != nil {
			t.Fatalf("指定可用版本后应能修复：%v", err)
		}
	})
}

func TestAIChannelService_GetAndList(t *testing.T) {
	ctx := context.Background()

	t.Run("没有渠道时列表是空数组", func(t *testing.T) {
		e := newAchEnv(t)
		list, err := e.svc.List(ctx)
		if err != nil || list == nil || len(list) != 0 {
			t.Fatalf("结果不符合预期：%#v %v", list, err)
		}
	})

	t.Run("列表按 key 升序，secret_set 与版本号正确；详情一致", func(t *testing.T) {
		e := newAchEnv(t)
		in := createInput()
		in.Key = "b-chan"
		_, _ = e.svc.Create(ctx, in)
		in.Key, in.PluginVersion = "a-chan", "1.1.0"
		_, _ = e.svc.Create(ctx, in)
		if err := e.svc.SetSecret(ctx, 1, "b-chan", "sk-secret"); err != nil {
			t.Fatal(err)
		}
		list, err := e.svc.List(ctx)
		if err != nil || len(list) != 2 || list[0].Key != "a-chan" || list[1].Key != "b-chan" {
			t.Fatalf("列表不符合预期：%+v %v", list, err)
		}
		if list[0].SecretSet || !list[1].SecretSet || list[0].PluginVersion != "1.1.0" || list[1].PluginVersion != "1.0.0" {
			t.Fatalf("secret_set / plugin_version 不符合预期：%+v", list)
		}
		one, err := e.svc.Get(ctx, "b-chan")
		if err != nil || !one.SecretSet || one.PluginVersion != "1.0.0" {
			t.Fatalf("详情不符合预期：%+v %v", one, err)
		}
		b, _ := json.Marshal(list)
		if strings.Contains(string(b), "sk-secret") {
			t.Fatalf("响应里不能出现 Key：%s", b)
		}
	})

	t.Run("详情：渠道不存在 404；版本行缺失时 plugin_version 为空串", func(t *testing.T) {
		e := newAchEnv(t)
		_, err := e.svc.Get(ctx, "nope")
		adminWantCode(t, err, errcode.ErrChannelNotFound.Code)

		_, _ = e.svc.Create(ctx, createInput())
		delete(e.repo.Versions, e.v1)
		one, err := e.svc.Get(ctx, "kling-main")
		if err != nil || one.PluginVersion != "" {
			t.Fatalf("结果不符合预期：%+v %v", one, err)
		}
	})
}

func TestAIChannelService_SetSecret(t *testing.T) {
	ctx := context.Background()

	t.Run("成功：加密存入 channel:<key>，审计不含值，视图 secret_set=true", func(t *testing.T) {
		e := newAchEnv(t)
		_, _ = e.svc.Create(ctx, createInput())
		e.repo.Audits = nil
		if err := e.svc.SetSecret(ctx, 6, "kling-main", "  sk-abc-123  "); err != nil {
			t.Fatal(err)
		}
		row := e.repo.Secrets["channel:kling-main"]
		if row == nil || strings.Contains(string(row.Ciphertext), "sk-abc-123") || row.UpdatedBy != 6 {
			t.Fatalf("凭证行不符合预期：%+v", row)
		}
		// 能读回去（首尾空白被去掉）
		if v, err := e.cfg.Get(ctx, "channel:kling-main"); err != nil || v != "sk-abc-123" {
			t.Fatalf("解密结果不符合预期：%q %v", v, err)
		}
		if got := e.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditChannelSecret {
			t.Fatalf("审计不符合预期：%v", got)
		}
		a := e.repo.Audits[0]
		b, _ := json.Marshal(a)
		if a.ActorID != 6 || a.TargetKey != "kling-main" || strings.Contains(string(b), "sk-abc") || len(a.DetailJSON) != 0 {
			t.Fatalf("审计不应带任何凭证内容：%s", b)
		}
		view, _ := e.svc.Get(ctx, "kling-main")
		if !view.SecretSet {
			t.Fatal("secret_set 应为 true")
		}
	})

	t.Run("渠道不存在 404，且不会写出无主凭证", func(t *testing.T) {
		e := newAchEnv(t)
		err := e.svc.SetSecret(ctx, 1, "nope", "sk-1")
		adminWantCode(t, err, errcode.ErrChannelNotFound.Code)
		if len(e.repo.Secrets) != 0 {
			t.Fatal("不该写入凭证")
		}
	})

	t.Run("空值 400；没有主密钥 500；都不写审计", func(t *testing.T) {
		e := newAchEnv(t)
		_, _ = e.svc.Create(ctx, createInput())
		e.repo.Audits = nil
		err := e.svc.SetSecret(ctx, 1, "kling-main", "   ")
		adminWantCode(t, err, errcode.ErrInvalidParams.Code)

		noKey := NewAIConfigService(e.repo, e.repo, e.repo, "")
		svc := NewAIChannelService(e.repo, e.repo, noKey, e.repo, e.ops, e.notifier)
		err = svc.SetSecret(ctx, 1, "kling-main", "sk-1")
		adminWantCode(t, err, errcode.ErrInternal.Code)
		if len(e.repo.Audits) != 0 {
			t.Fatalf("失败不该写审计：%v", e.repo.AuditActions())
		}
	})
}

func TestAIChannelService_Check(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T, withSecret bool) *achEnv {
		e := newAchEnv(t)
		if _, err := e.svc.Create(ctx, createInput()); err != nil {
			t.Fatal(err)
		}
		if withSecret {
			if err := e.svc.SetSecret(ctx, 1, "kling-main", "sk-secret-value"); err != nil {
				t.Fatal(err)
			}
		}
		return e
	}

	t.Run("成功：透传宿主的检查结果，运行时里是渠道 + 固定版本", func(t *testing.T) {
		e := setup(t, true)
		e.ops.CheckResult = &provider.CheckResult{OK: true, Message: "HTTP 200", DurationMs: 120}
		res, err := e.svc.Check(ctx, "kling-main")
		if err != nil || !res.OK || res.Message != "HTTP 200" || res.DurationMs != 120 {
			t.Fatalf("结果不符合预期：%+v %v", res, err)
		}
		rt := e.ops.GotRuntime
		if rt == nil || rt.Channel.Key != "kling-main" || rt.Plugin.Version != "1.0.0" || rt.Channel.Settings["tenant"] != "t1" {
			t.Fatalf("传给宿主的运行时不符合预期：%+v", rt)
		}
	})

	t.Run("插件没实现检查钩子：ok=false 与说明，不是 HTTP 错误", func(t *testing.T) {
		e := setup(t, true)
		e.ops.CheckErr = provider.ErrCheckUnsupported
		res, err := e.svc.Check(ctx, "kling-main")
		if err != nil || res.OK || res.Message != "插件不支持连通性检查" {
			t.Fatalf("结果不符合预期：%+v %v", res, err)
		}
	})

	t.Run("上游不通：宿主同时返回结果与错误，按 ok=false 返回", func(t *testing.T) {
		e := setup(t, true)
		e.ops.CheckResult = &provider.CheckResult{Message: "上游返回 HTTP 401", DurationMs: 30}
		e.ops.CheckErr = errors.New("http 401")
		res, err := e.svc.Check(ctx, "kling-main")
		if err != nil || res.OK || res.Message != "上游返回 HTTP 401" {
			t.Fatalf("结果不符合预期：%+v %v", res, err)
		}
	})

	t.Run("Key 未设置 409（50015），不调用宿主", func(t *testing.T) {
		e := setup(t, false)
		_, err := e.svc.Check(ctx, "kling-main")
		adminWantCode(t, err, errcode.ErrChannelSecretUnset.Code)
		if e.ops.Calls != 0 {
			t.Fatal("不该调用宿主")
		}
	})

	t.Run("鉴权 none 的插件不要求 Key", func(t *testing.T) {
		e := newAchEnv(t)
		in := createInput()
		in.Key, in.PluginKey, in.Settings = "loose-1", "loose", nil
		_, _ = e.svc.Create(ctx, in)
		e.ops.CheckResult = &provider.CheckResult{OK: true}
		if res, err := e.svc.Check(ctx, "loose-1"); err != nil || !res.OK {
			t.Fatalf("结果不符合预期：%+v %v", res, err)
		}
	})

	t.Run("runner 不可用 503（50021）", func(t *testing.T) {
		e := setup(t, true)
		e.ops.CheckErr = &provider.Error{Class: provider.ClassRetryable, Code: provider.CodeRunnerUnavailable, Message: "down"}
		_, err := e.svc.Check(ctx, "kling-main")
		adminWantCode(t, err, errcode.ErrRunnerUnavailable.Code)
	})

	t.Run("插件本身出错 502（50022），原因里的 Key 被脱敏", func(t *testing.T) {
		e := setup(t, true)
		e.ops.CheckErr = errors.New("钩子异常：token=sk-secret-value 无效")
		_, err := e.svc.Check(ctx, "kling-main")
		ec := adminWantCodeErr(t, err, errcode.ErrPluginOpFailed.Code)
		if strings.Contains(ec.Msg, "sk-secret-value") || !strings.Contains(ec.Msg, "***") {
			t.Fatalf("Key 应被脱敏：%s", ec.Msg)
		}
	})

	t.Run("检查结果里的 Key 同样脱敏", func(t *testing.T) {
		e := setup(t, true)
		e.ops.CheckResult = &provider.CheckResult{OK: false, Message: "echo sk-secret-value"}
		e.ops.CheckErr = errors.New("x")
		res, err := e.svc.Check(ctx, "kling-main")
		if err != nil || strings.Contains(res.Message, "sk-secret-value") {
			t.Fatalf("Key 应被脱敏：%+v %v", res, err)
		}
	})

	t.Run("渠道不存在 404", func(t *testing.T) {
		e := setup(t, true)
		_, err := e.svc.Check(ctx, "nope")
		adminWantCode(t, err, errcode.ErrChannelNotFound.Code)
	})

	t.Run("渠道固定的版本丢失：按渠道数据不合法返回", func(t *testing.T) {
		e := setup(t, true)
		delete(e.repo.Versions, e.v1)
		_, err := e.svc.Check(ctx, "kling-main")
		adminWantCode(t, err, errcode.ErrChannelInvalid.Code)
	})
}

func TestAIChannelService_Import(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T, withSecret bool) *achEnv {
		e := newAchEnv(t)
		if _, err := e.svc.Create(ctx, createInput()); err != nil {
			t.Fatal(err)
		}
		if withSecret {
			_ = e.svc.SetSecret(ctx, 1, "kling-main", "sk-secret-value")
		}
		return e
	}

	t.Run("成功：参数按声明校验并补默认值，草稿原样返回", func(t *testing.T) {
		e := setup(t, true)
		e.ops.Drafts = []provider.ModelDraft{{UpstreamModel: "kling-v2", Kind: "video", Label: "可灵 v2"}}
		auditsBefore := len(e.repo.Audits)
		res, err := e.svc.Import(ctx, "kling-main", map[string]any{"limit": 10.0})
		if err != nil || len(res.Drafts) != 1 || res.Drafts[0].UpstreamModel != "kling-v2" {
			t.Fatalf("结果不符合预期：%+v %v", res, err)
		}
		if e.ops.GotArgs["limit"] != 10.0 || e.ops.GotArgs["prefix"] != "" {
			t.Fatalf("传给宿主的参数应带默认值：%v", e.ops.GotArgs)
		}
		if len(e.repo.Audits) != auditsBefore {
			t.Fatalf("导入只读，不该写审计：%v", e.repo.AuditActions())
		}
	})

	t.Run("宿主返回 nil 草稿时输出空数组", func(t *testing.T) {
		e := setup(t, true)
		res, err := e.svc.Import(ctx, "kling-main", map[string]any{"limit": 1.0})
		if err != nil || res.Drafts == nil || len(res.Drafts) != 0 {
			t.Fatalf("结果不符合预期：%#v %v", res, err)
		}
	})

	t.Run("参数不合法 400（10001）：缺必填、未声明、类型不对", func(t *testing.T) {
		for name, args := range map[string]map[string]any{
			"缺必填 limit":   nil,
			"未声明的参数":      {"limit": 1.0, "zzz": true},
			"limit 类型不对":  {"limit": "ten"},
			"prefix 类型不对": {"limit": 1.0, "prefix": 3.0},
		} {
			e := setup(t, true)
			_, err := e.svc.Import(ctx, "kling-main", args)
			adminWantCode(t, err, errcode.ErrInvalidParams.Code)
			if e.ops.Calls != 0 {
				t.Fatalf("%s：参数不合法不该调用宿主", name)
			}
		}
	})

	t.Run("插件没实现导入 502（50022）", func(t *testing.T) {
		e := setup(t, true)
		e.ops.ImportErr = provider.ErrImportUnsupported
		_, err := e.svc.Import(ctx, "kling-main", map[string]any{"limit": 1.0})
		ec := adminWantCodeErr(t, err, errcode.ErrPluginOpFailed.Code)
		if !strings.Contains(ec.Msg, "不支持导入") {
			t.Fatalf("msg 不符合预期：%s", ec.Msg)
		}
	})

	t.Run("runner 不可用 503；其他错误 502 且脱敏", func(t *testing.T) {
		e := setup(t, true)
		e.ops.ImportErr = &provider.Error{Class: provider.ClassRetryable, Code: provider.CodeRunnerUnavailable, Message: "down"}
		_, err := e.svc.Import(ctx, "kling-main", map[string]any{"limit": 1.0})
		adminWantCode(t, err, errcode.ErrRunnerUnavailable.Code)

		e.ops.ImportErr = errors.New("boom sk-secret-value")
		_, err = e.svc.Import(ctx, "kling-main", map[string]any{"limit": 1.0})
		ec := adminWantCodeErr(t, err, errcode.ErrPluginOpFailed.Code)
		if strings.Contains(ec.Msg, "sk-secret-value") {
			t.Fatalf("Key 应被脱敏：%s", ec.Msg)
		}
	})

	t.Run("Key 未设置 409（50015）；渠道不存在 404", func(t *testing.T) {
		e := setup(t, false)
		_, err := e.svc.Import(ctx, "kling-main", map[string]any{"limit": 1.0})
		adminWantCode(t, err, errcode.ErrChannelSecretUnset.Code)
		_, err = e.svc.Import(ctx, "nope", nil)
		adminWantCode(t, err, errcode.ErrChannelNotFound.Code)
	})

	t.Run("没有声明导入参数的插件只接受空参数", func(t *testing.T) {
		e := newAchEnv(t)
		in := createInput()
		in.Key, in.PluginKey, in.Settings = "loose-1", "loose", nil
		_, _ = e.svc.Create(ctx, in)
		if _, err := e.svc.Import(ctx, "loose-1", nil); err != nil {
			t.Fatalf("空参数应通过：%v", err)
		}
		_, err := e.svc.Import(ctx, "loose-1", map[string]any{"a": 1.0})
		adminWantCode(t, err, errcode.ErrInvalidParams.Code)
	})
}
