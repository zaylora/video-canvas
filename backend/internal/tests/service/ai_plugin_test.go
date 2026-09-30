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
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
	"video-canvas/internal/service/aiconfigfake"
)

// aplEnv 是插件服务测试的环境。
type aplEnv struct {
	svc      *AIPluginService
	repo     *aiconfigfake.MemRepo
	checker  *aiconfigfake.Prechecker
	notifier *aiconfigfake.Notifier
}

func newAplEnv() *aplEnv {
	repo := aiconfigfake.NewMemRepo()
	checker := &aiconfigfake.Prechecker{ByCode: map[string]*provider.PrecheckResult{}}
	notifier := &aiconfigfake.Notifier{}
	return &aplEnv{svc: NewAIPluginService(repo, repo, repo, checker, notifier), repo: repo, checker: checker, notifier: notifier}
}

// code 登记一份“代码 → 预检结果”，返回代码字节。
func (e *aplEnv) code(text string, res *provider.PrecheckResult) []byte {
	e.checker.ByCode[text] = res
	return []byte(text)
}

func TestAIPluginService_Upload(t *testing.T) {
	ctx := context.Background()

	t.Run("成功：登记为 uploaded 来源的不可变版本，记 sha256 并写审计", func(t *testing.T) {
		e := newAplEnv()
		code := e.code("module.exports={}//a", adminPass(adminMetaJSON("kling", "1.0.0")))
		res, err := e.svc.Upload(ctx, 7, code)
		if err != nil || !res.Accepted || res.Version == nil {
			t.Fatalf("上传应被接受：%+v %v", res, err)
		}
		if len(res.Issues) != 0 || res.Issues == nil {
			t.Fatalf("issues 应为空数组而不是 nil：%#v", res.Issues)
		}
		v := res.Version
		if v.PluginKey != "kling" || v.Version != "1.0.0" || v.CreatedBy != 7 || v.ChannelCount != 0 || v.Meta == nil || v.Meta.Key != "kling" {
			t.Fatalf("版本视图不符合预期：%+v", v)
		}
		// sha256 是服务端对原文件计算的，不取 runner 回传值
		if len(v.SHA256) != 64 {
			t.Fatalf("sha256 应为 64 位十六进制：%q", v.SHA256)
		}
		p := e.repo.Plugins["kling"]
		if p == nil || p.Source != model.PluginSourceUploaded || !p.Enabled {
			t.Fatalf("插件行不符合预期：%+v", p)
		}
		if stored := e.repo.Versions[v.ID]; stored == nil || stored.Code != string(code) || stored.SHA256 != v.SHA256 {
			t.Fatalf("库里的版本不符合预期：%+v", stored)
		}
		if got := e.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditPluginUpload {
			t.Fatalf("应写一条上传审计：%v", got)
		}
		a := e.repo.Audits[0]
		if a.ActorID != 7 || a.TargetKey != "kling" || !strings.Contains(string(a.DetailJSON), v.SHA256) {
			t.Fatalf("审计内容不符合预期：%+v", a)
		}
		if strings.Contains(string(a.DetailJSON), "module.exports") {
			t.Fatalf("审计里不应包含插件代码：%s", a.DetailJSON)
		}
	})

	t.Run("预检不通过：accepted=false，问题精确到字段，不登记也不写审计", func(t *testing.T) {
		e := newAplEnv()
		issues := []modelcfg.Issue{{Path: "meta.endpoints.video.mode", Message: "必须是 sync / async"}}
		code := e.code("bad", &provider.PrecheckResult{OK: false, Issues: issues})
		res, err := e.svc.Upload(ctx, 1, code)
		if err != nil || res.Accepted || res.Version != nil {
			t.Fatalf("预检不通过应返回 accepted=false：%+v %v", res, err)
		}
		if len(res.Issues) != 1 || res.Issues[0].Path != "meta.endpoints.video.mode" {
			t.Fatalf("问题列表不符合预期：%+v", res.Issues)
		}
		if len(e.repo.Versions) != 0 || len(e.repo.Audits) != 0 {
			t.Fatal("预检不通过不该登记版本或写审计")
		}
	})

	t.Run("预检不通过且没有问题列表时 issues 仍是空数组", func(t *testing.T) {
		e := newAplEnv()
		res, err := e.svc.Upload(ctx, 1, e.code("bad2", &provider.PrecheckResult{OK: false}))
		if err != nil || res.Accepted || res.Issues == nil {
			t.Fatalf("结果不符合预期：%+v %v", res, err)
		}
	})

	t.Run("版本号已存在：报在 meta.version 上而不是 HTTP 错误", func(t *testing.T) {
		e := newAplEnv()
		if res, err := e.svc.Upload(ctx, 1, e.code("v1", adminPass(adminMetaJSON("kling", "1.0.0")))); err != nil || !res.Accepted {
			t.Fatalf("第一次应成功：%+v %v", res, err)
		}
		res, err := e.svc.Upload(ctx, 1, e.code("v1-changed", adminPass(adminMetaJSON("kling", "1.0.0"))))
		if err != nil || res.Accepted {
			t.Fatalf("重复版本应被拒绝：%+v %v", res, err)
		}
		if len(res.Issues) != 1 || res.Issues[0].Path != "meta.version" {
			t.Fatalf("应报在 meta.version：%+v", res.Issues)
		}
		if len(e.repo.Versions) != 1 || len(e.repo.Audits) != 1 {
			t.Fatalf("重复上传不该新增版本或审计：%d 个版本，%d 条审计", len(e.repo.Versions), len(e.repo.Audits))
		}
	})

	t.Run("并发上传同一版本：唯一索引冲突同样按版本号已存在处理", func(t *testing.T) {
		e := newAplEnv()
		e.repo.SaveVersionErr = repository.ErrDuplicate
		res, err := e.svc.Upload(ctx, 1, e.code("race", adminPass(adminMetaJSON("kling", "1.0.0"))))
		if err != nil || res.Accepted || len(res.Issues) != 1 || res.Issues[0].Path != "meta.version" {
			t.Fatalf("结果不符合预期：%+v %v", res, err)
		}
	})

	t.Run("同一插件的新版本可以继续上传，两个版本并存", func(t *testing.T) {
		e := newAplEnv()
		_, _ = e.svc.Upload(ctx, 1, e.code("a", adminPass(adminMetaJSON("kling", "1.0.0"))))
		res, err := e.svc.Upload(ctx, 1, e.code("b", adminPass(adminMetaJSON("kling", "1.1.0"))))
		if err != nil || !res.Accepted {
			t.Fatalf("新版本应被接受：%+v %v", res, err)
		}
		if len(e.repo.Versions) != 2 || len(e.repo.Plugins) != 1 {
			t.Fatalf("应有 1 个插件 2 个版本：%d/%d", len(e.repo.Plugins), len(e.repo.Versions))
		}
	})

	t.Run("往内置插件的 key 上传：409（50006），不登记", func(t *testing.T) {
		e := newAplEnv()
		adminSeedVersion(t, e.repo, adminMetaJSON("newapi", "1.0.0"), model.PluginSourceBuiltin, true)
		before := len(e.repo.Versions)
		_, err := e.svc.Upload(ctx, 1, e.code("hijack", adminPass(adminMetaJSON("newapi", "9.9.9"))))
		adminWantCode(t, err, errcode.ErrPluginBuiltin.Code)
		if len(e.repo.Versions) != before {
			t.Fatal("不该登记新版本")
		}
	})

	t.Run("空文件 400、超限 413，都不会调用预检", func(t *testing.T) {
		e := newAplEnv()
		_, err := e.svc.Upload(ctx, 1, nil)
		adminWantCode(t, err, errcode.ErrInvalidParams.Code)
		_, err = e.svc.Upload(ctx, 1, make([]byte, 512<<10+1))
		adminWantCode(t, err, errcode.ErrPluginTooLarge.Code)
		if e.checker.Calls != 0 {
			t.Fatalf("不该调用预检，实际 %d 次", e.checker.Calls)
		}
		// 刚好等于上限是允许的
		e.checker.Default = &provider.PrecheckResult{OK: false}
		if _, err := e.svc.Upload(ctx, 1, make([]byte, 512<<10)); err != nil {
			t.Fatalf("恰好 512KB 应放行到预检：%v", err)
		}
	})

	t.Run("runner 不可用返回 503（50021）；其他预检故障是内部错误", func(t *testing.T) {
		e := newAplEnv()
		e.checker.Err = &provider.Error{Class: provider.ClassRetryable, Code: provider.CodeRunnerUnavailable, Message: "down"}
		_, err := e.svc.Upload(ctx, 1, []byte("x"))
		adminWantCode(t, err, errcode.ErrRunnerUnavailable.Code)

		e.checker.Err = errors.New("boom")
		_, err = e.svc.Upload(ctx, 1, []byte("x"))
		var ec *errcode.Error
		if err == nil || errors.As(err, &ec) {
			t.Fatalf("未知故障不该伪装成业务错误：%v", err)
		}
	})
}

func TestAIPluginService_SetEnabled(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		key      string
		enabled  bool
		wantCode int
	}{
		{"停用", "kling", false, 0},
		{"重新启用", "kling", true, 0},
		{"插件不存在 404", "nope", true, errcode.ErrPluginNotFound.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newAplEnv()
			adminSeedVersion(t, e.repo, adminMetaJSON("kling", "1.0.0"), model.PluginSourceUploaded, true)
			err := e.svc.SetEnabled(ctx, 9, tt.key, tt.enabled)
			adminWantCode(t, err, tt.wantCode)
			if tt.wantCode != 0 {
				if len(e.repo.Audits) != 0 || len(e.notifier.Reasons) != 0 {
					t.Fatal("失败时不该写审计或刷新 Registry")
				}
				return
			}
			if e.repo.Plugins["kling"].Enabled != tt.enabled {
				t.Fatalf("启停状态未生效")
			}
			if got := e.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditPluginEnable {
				t.Fatalf("审计不符合预期：%v", got)
			}
			if len(e.notifier.Reasons) != 1 {
				t.Fatalf("启停后应刷新 Registry：%v", e.notifier.Reasons)
			}
		})
	}
}

func TestAIPluginService_DeleteVersion(t *testing.T) {
	ctx := context.Background()
	newEnv := func(t *testing.T, source string) (*aplEnv, uint64) {
		e := newAplEnv()
		id := adminSeedVersion(t, e.repo, adminMetaJSON("kling", "1.0.0"), source, true)
		return e, id
	}

	t.Run("成功：删除版本并写审计，最后一个版本删除后插件行一并消失", func(t *testing.T) {
		e, id := newEnv(t, model.PluginSourceUploaded)
		if err := e.svc.DeleteVersion(ctx, 3, "kling", "1.0.0"); err != nil {
			t.Fatal(err)
		}
		if _, ok := e.repo.Versions[id]; ok {
			t.Fatal("版本应被删除")
		}
		if _, ok := e.repo.Plugins["kling"]; ok {
			t.Fatal("没有版本的插件行应被删除")
		}
		if got := e.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditPluginDelete {
			t.Fatalf("审计不符合预期：%v", got)
		}
	})

	tests := []struct {
		name     string
		setup    func(e *aplEnv, id uint64)
		key      string
		version  string
		wantCode int
	}{
		{"内置插件不能删 409（50006）", func(e *aplEnv, _ uint64) { e.repo.Plugins["kling"].Source = model.PluginSourceBuiltin }, "kling", "1.0.0", errcode.ErrPluginBuiltin.Code},
		{"插件不存在 404", nil, "nope", "1.0.0", errcode.ErrPluginNotFound.Code},
		{"版本不存在 404", nil, "kling", "9.9.9", errcode.ErrPluginNotFound.Code},
		{"被渠道固定 409（50005）", func(e *aplEnv, id uint64) {
			e.repo.Channels["c1"] = &model.AIChannel{Key: "c1", PluginKey: "kling", PluginVersionID: id}
		}, "kling", "1.0.0", errcode.ErrPluginInUse.Code},
		{"被非终态任务引用 409（50005）", func(e *aplEnv, id uint64) { e.repo.ActiveTaskRefs[id] = 2 }, "kling", "1.0.0", errcode.ErrPluginInUse.Code},
		{"并发下被新引用（仓储事务内复查）409", func(e *aplEnv, _ uint64) { e.repo.DeleteInUse = true }, "kling", "1.0.0", errcode.ErrPluginInUse.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, id := newEnv(t, model.PluginSourceUploaded)
			if tt.setup != nil {
				tt.setup(e, id)
			}
			err := e.svc.DeleteVersion(ctx, 3, tt.key, tt.version)
			adminWantCode(t, err, tt.wantCode)
			if _, ok := e.repo.Versions[id]; !ok {
				t.Fatal("失败时版本不该被删除")
			}
			if len(e.repo.Audits) != 0 {
				t.Fatalf("失败时不该写审计：%v", e.repo.AuditActions())
			}
		})
	}
}

func TestAIPluginService_List(t *testing.T) {
	ctx := context.Background()

	t.Run("没有插件返回空数组而不是 nil", func(t *testing.T) {
		e := newAplEnv()
		list, err := e.svc.List(ctx)
		if err != nil || list == nil || len(list) != 0 {
			t.Fatalf("结果不符合预期：%#v %v", list, err)
		}
	})

	t.Run("版本新到旧，带渠道数与 meta", func(t *testing.T) {
		e := newAplEnv()
		v1 := adminSeedVersion(t, e.repo, adminMetaJSON("kling", "1.0.0"), model.PluginSourceUploaded, true)
		v2 := adminSeedVersion(t, e.repo, adminMetaJSON("kling", "1.1.0"), model.PluginSourceUploaded, true)
		adminSeedVersion(t, e.repo, adminMetaJSON("newapi", "1.0.0"), model.PluginSourceBuiltin, false)
		e.repo.Channels["a"] = &model.AIChannel{Key: "a", PluginKey: "kling", PluginVersionID: v1}
		e.repo.Channels["b"] = &model.AIChannel{Key: "b", PluginKey: "kling", PluginVersionID: v1}
		e.repo.Channels["c"] = &model.AIChannel{Key: "c", PluginKey: "kling", PluginVersionID: v2}

		list, err := e.svc.List(ctx)
		if err != nil || len(list) != 2 {
			t.Fatalf("应有 2 个插件：%+v %v", list, err)
		}
		kling, newapi := list[0], list[1]
		if kling.Key != "kling" || newapi.Key != "newapi" {
			t.Fatalf("应按 key 升序：%s %s", kling.Key, newapi.Key)
		}
		if newapi.Source != model.PluginSourceBuiltin || newapi.Enabled {
			t.Fatalf("newapi 应是停用的内置插件：%+v", newapi)
		}
		if len(kling.Versions) != 2 || kling.Versions[0].Version != "1.1.0" || kling.Versions[1].Version != "1.0.0" {
			t.Fatalf("版本应新到旧：%+v", kling.Versions)
		}
		if kling.Versions[0].ChannelCount != 1 || kling.Versions[1].ChannelCount != 2 {
			t.Fatalf("渠道数不符合预期：%d %d", kling.Versions[0].ChannelCount, kling.Versions[1].ChannelCount)
		}
		if kling.Versions[0].Meta == nil || len(kling.Versions[0].Meta.ChannelSettings) != 4 {
			t.Fatalf("meta 应带渠道设置声明：%+v", kling.Versions[0].Meta)
		}
		// 视图里不能带代码
		b, _ := json.Marshal(list)
		if strings.Contains(string(b), `"code"`) {
			t.Fatalf("响应不应包含插件代码：%s", b)
		}
	})

	t.Run("meta 损坏的版本输出 meta=null，不影响整个列表", func(t *testing.T) {
		e := newAplEnv()
		id := adminSeedVersion(t, e.repo, adminMetaJSON("kling", "1.0.0"), model.PluginSourceUploaded, true)
		e.repo.Versions[id].MetaJSON = model.JSONText(`{oops`)
		list, err := e.svc.List(ctx)
		if err != nil || len(list) != 1 || list[0].Versions[0].Meta != nil {
			t.Fatalf("结果不符合预期：%+v %v", list, err)
		}
	})
}

func TestAIPluginService_RegisterBuiltin(t *testing.T) {
	ctx := context.Background()

	t.Run("首次登记为 builtin，重复启动幂等", func(t *testing.T) {
		e := newAplEnv()
		src := [][]byte{e.code("newapi-src", adminPass(adminMetaJSON("newapi", "1.0.0")))}
		regs, err := e.svc.RegisterBuiltin(ctx, src)
		if err != nil || len(regs) != 1 || !regs[0].Registered || regs[0].Key != "newapi" || regs[0].Version != "1.0.0" {
			t.Fatalf("首次登记结果不符合预期：%+v %v", regs, err)
		}
		p := e.repo.Plugins["newapi"]
		if p == nil || p.Source != model.PluginSourceBuiltin || !p.Enabled {
			t.Fatalf("插件行不符合预期：%+v", p)
		}
		regs, err = e.svc.RegisterBuiltin(ctx, src)
		if err != nil || len(regs) != 1 || regs[0].Registered {
			t.Fatalf("第二次应是空操作：%+v %v", regs, err)
		}
		if len(e.repo.Versions) != 1 || len(e.repo.Audits) != 1 {
			t.Fatalf("重复登记不该新增版本或审计：%d/%d", len(e.repo.Versions), len(e.repo.Audits))
		}
		for _, v := range e.repo.Versions {
			if v.CreatedBy != 0 {
				t.Fatalf("内置插件的 created_by 应为 0：%d", v.CreatedBy)
			}
		}
	})

	t.Run("新版本随发版登记，与旧版本并存", func(t *testing.T) {
		e := newAplEnv()
		_, _ = e.svc.RegisterBuiltin(ctx, [][]byte{e.code("v1", adminPass(adminMetaJSON("newapi", "1.0.0")))})
		regs, err := e.svc.RegisterBuiltin(ctx, [][]byte{e.code("v2", adminPass(adminMetaJSON("newapi", "1.1.0")))})
		if err != nil || len(regs) != 1 || !regs[0].Registered || len(e.repo.Versions) != 2 {
			t.Fatalf("新版本应登记：%+v %v", regs, err)
		}
	})

	t.Run("版本号没升但代码变了：报错并保持旧代码", func(t *testing.T) {
		e := newAplEnv()
		_, _ = e.svc.RegisterBuiltin(ctx, [][]byte{e.code("old", adminPass(adminMetaJSON("newapi", "1.0.0")))})
		_, err := e.svc.RegisterBuiltin(ctx, [][]byte{e.code("changed", adminPass(adminMetaJSON("newapi", "1.0.0")))})
		if err == nil || !strings.Contains(err.Error(), "版本不可变") {
			t.Fatalf("应提示升级版本号：%v", err)
		}
		for _, v := range e.repo.Versions {
			if v.Code != "old" {
				t.Fatalf("已登记版本的代码不该被覆盖：%q", v.Code)
			}
		}
	})

	t.Run("key 被上传的插件占用：报错，不覆盖", func(t *testing.T) {
		e := newAplEnv()
		adminSeedVersion(t, e.repo, adminMetaJSON("newapi", "0.1.0"), model.PluginSourceUploaded, true)
		_, err := e.svc.RegisterBuiltin(ctx, [][]byte{e.code("b", adminPass(adminMetaJSON("newapi", "1.0.0")))})
		if err == nil || !strings.Contains(err.Error(), "占用") {
			t.Fatalf("应提示 key 被占用：%v", err)
		}
		if e.repo.Plugins["newapi"].Source != model.PluginSourceUploaded {
			t.Fatal("不该改掉已有插件的来源")
		}
	})

	t.Run("多实例并发登记：唯一索引冲突按已登记处理", func(t *testing.T) {
		e := newAplEnv()
		e.repo.SaveVersionErr = repository.ErrDuplicate
		regs, err := e.svc.RegisterBuiltin(ctx, [][]byte{e.code("b", adminPass(adminMetaJSON("newapi", "1.0.0")))})
		if err != nil || len(regs) != 1 || regs[0].Registered {
			t.Fatalf("结果不符合预期：%+v %v", regs, err)
		}
	})

	t.Run("一个失败不影响其他：预检不通过的报错，其余照常登记", func(t *testing.T) {
		e := newAplEnv()
		good := e.code("good", adminPass(adminMetaJSON("good", "1.0.0")))
		bad := e.code("bad", &provider.PrecheckResult{OK: false, Issues: []modelcfg.Issue{{Path: "meta.key", Message: "不合法"}}})
		regs, err := e.svc.RegisterBuiltin(ctx, [][]byte{bad, good})
		if err == nil || !strings.Contains(err.Error(), "meta.key") {
			t.Fatalf("应报出预检问题：%v", err)
		}
		if len(regs) != 1 || regs[0].Key != "good" || e.repo.Plugins["good"] == nil {
			t.Fatalf("好的插件应照常登记：%+v", regs)
		}
	})

	t.Run("runner 不可用的错误保留分类，启动流程据此重试", func(t *testing.T) {
		e := newAplEnv()
		e.checker.Err = &provider.Error{Class: provider.ClassRetryable, Code: provider.CodeRunnerUnavailable, Message: "down"}
		_, err := e.svc.RegisterBuiltin(ctx, [][]byte{[]byte("x")})
		if provider.CodeOf(err) != provider.CodeRunnerUnavailable {
			t.Fatalf("错误应能识别为 runner 不可用：%v", err)
		}
	})
}
