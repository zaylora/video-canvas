package agent_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service/agent"
)

func codeOf(err error) int {
	var e *errcode.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}

// standardPkg 是一个含 SKILL.md、参考文档和图片的包（不含脚本：含脚本的包会被拦下）。
func standardPkg(t *testing.T, name, body string) []byte {
	return zipOf(t, map[string]string{
		name + "/SKILL.md":        skillMD(name, "做某件事的方法", body),
		name + "/references/a.md": "# 参考\n",
		name + "/assets/t.png":    "\x89PNG\r\n\x1a\n\x00\x00\x00",
	})
}

func mustImport(t *testing.T, e *skillEnv, actor uint64, zipBytes []byte) *SkillImportView {
	t.Helper()
	v, err := e.svc.Import(context.Background(), actor, SkillImportInput{Zip: zipBytes})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return v
}

func mustConfirm(t *testing.T, e *skillEnv, actor uint64, zipBytes []byte) *SkillItem {
	t.Helper()
	v := mustImport(t, e, actor, zipBytes)
	it, err := e.svc.ConfirmImport(context.Background(), actor, v.ID)
	if err != nil {
		t.Fatalf("ConfirmImport: %v", err)
	}
	return it
}

func TestSkillService_ImportPrecheck(t *testing.T) {
	e := newSkillEnv(t)
	v := mustImport(t, e, 1, standardPkg(t, "demo", "正文"))
	if !v.CanConfirm || v.ID == "" || v.Plan.Action != "create" || v.Plan.Version != 1 || v.Name != "demo" {
		t.Fatalf("标准包应可确认: %+v", v)
	}
	if len(v.Files) != 3 || v.HasScripts {
		t.Errorf("清单应有 3 个文件且不含脚本: %+v", v.Files)
	}
	if len(e.store.keys()) != 1 || !strings.HasPrefix(e.store.keys()[0], "agent-skills/_staging/") {
		t.Errorf("预检后应只有暂存对象: %v", e.store.keys())
	}
	if len(e.repo.skills) != 0 {
		t.Error("预检不应创建技能")
	}

	t.Run("预检不通过也是正常结果，不能确认", func(t *testing.T) {
		bad := mustImport(t, e, 1, zipOf(t, map[string]string{"README.md": "x"}))
		if bad.CanConfirm || bad.Plan.Action != "blocked" || len(bad.Issues) == 0 || bad.Issues[0].Code != "NO_SKILL_MD" {
			t.Errorf("缺 SKILL.md: %+v", bad)
		}
	})
	t.Run("与内置同名", func(t *testing.T) {
		b := mustImport(t, e, 1, zipOf(t, map[string]string{"SKILL.md": skillMD("keyframe-prompt", "d", "x")}))
		if b.CanConfirm || b.Issues[0].Code != "BUILTIN_NAME" {
			t.Errorf("%+v", b.Issues)
		}
		if _, err := e.svc.ConfirmImport(context.Background(), 1, b.ID); codeOf(err) != 61003 {
			t.Errorf("确认应 61003: %v", err)
		}
	})
	t.Run("含脚本的包不能导入：预检阻断，确认 61002", func(t *testing.T) {
		withScript := zipOf(t, map[string]string{"SKILL.md": skillMD("scripty", "d", "x"), "scripts/run.py": "print(1)\n"})
		v := mustImport(t, e, 1, withScript)
		if v.CanConfirm || v.Plan.Action != "blocked" || !hasCode(v.Issues, "HAS_SCRIPTS", "error") {
			t.Fatalf("应以 error 级 HAS_SCRIPTS 阻断: %+v", v)
		}
		if _, err := e.svc.ConfirmImport(context.Background(), 1, v.ID); codeOf(err) != 61002 {
			t.Errorf("确认应 61002: %v", err)
		}
		if _, err := e.repo.GetSkill(context.Background(), "scripty"); err == nil {
			t.Error("不应创建技能")
		}
	})
	t.Run("既不是 zip 也没有文件", func(t *testing.T) {
		if _, err := e.svc.Import(context.Background(), 1, SkillImportInput{}); codeOf(err) != 10001 {
			t.Errorf("%v", err)
		}
	})
}

func TestSkillService_ConfirmAndVersions(t *testing.T) {
	e := newSkillEnv(t)
	ctx := context.Background()
	it := mustConfirm(t, e, 1, standardPkg(t, "demo", "第一版"))
	if it.Enabled || it.ActiveVersion == nil || *it.ActiveVersion != 1 || it.Source != SkillSourceImported {
		t.Fatalf("首次导入应停用、v1 生效: %+v", it)
	}
	if keys := e.store.keys(); len(keys) != 1 || !strings.HasPrefix(keys[0], "agent-skills/demo/") {
		t.Errorf("确认后暂存应被清掉，只剩正式对象: %v", keys)
	}
	if len(e.repo.imports) != 0 {
		t.Error("暂存行应已删除")
	}

	t.Run("同名新版本：生效版本不变，列表标出待启用", func(t *testing.T) {
		it2 := mustConfirm(t, e, 2, standardPkg(t, "demo", "第二版"))
		if *it2.ActiveVersion != 1 || it2.LatestVersion != 2 || !it2.PendingVersion || it2.VersionCount != 2 {
			t.Errorf("%+v", it2)
		}
	})
	t.Run("同内容再导入：预检阻断，确认 61006", func(t *testing.T) {
		v := mustImport(t, e, 1, standardPkg(t, "demo", "第二版"))
		if v.CanConfirm || v.Issues[0].Code != "SAME_AS_VERSION" {
			t.Fatalf("%+v", v.Issues)
		}
		if _, err := e.svc.ConfirmImport(ctx, 1, v.ID); codeOf(err) != 61006 {
			t.Errorf("%v", err)
		}
	})
	t.Run("设为生效即回滚；版本不存在 61005", func(t *testing.T) {
		if _, err := e.svc.SetActiveVersion(ctx, 1, "demo", 2); err != nil {
			t.Fatal(err)
		}
		got, _ := e.svc.SetActiveVersion(ctx, 1, "demo", 1)
		if *got.ActiveVersion != 1 || got.PendingVersion != true {
			t.Errorf("回滚到 v1: %+v", got)
		}
		if _, err := e.svc.SetActiveVersion(ctx, 1, "demo", 9); codeOf(err) != 61005 {
			t.Errorf("%v", err)
		}
	})
	t.Run("删除版本：生效版本不能删，删后号码不复用", func(t *testing.T) {
		if err := e.svc.DeleteVersion(ctx, 1, "demo", 1); codeOf(err) != 61008 {
			t.Errorf("生效版本 61008: %v", err)
		}
		if err := e.svc.DeleteVersion(ctx, 1, "demo", 2); err != nil {
			t.Fatal(err)
		}
		formal := 0
		for _, k := range e.store.keys() {
			if strings.HasPrefix(k, "agent-skills/demo/") {
				formal++
			}
		}
		if formal != 1 {
			t.Errorf("删除版本后正式对象应只剩 v1 的: %v", e.store.keys())
		}
		if err := e.svc.DeleteVersion(ctx, 1, "demo", 2); codeOf(err) != 61005 {
			t.Errorf("重复删除 61005: %v", err)
		}
		if it3 := mustConfirm(t, e, 1, standardPkg(t, "demo", "第三版")); it3.LatestVersion != 3 {
			t.Errorf("版本号不应复用: %+v", it3)
		}
	})
	if want := []string{"agent_skill.import", "agent_skill.import", "agent_skill.activate_version", "agent_skill.activate_version", "agent_skill.delete_version", "agent_skill.import"}; strings.Join(e.audit.actions, ",") != strings.Join(want, ",") {
		t.Errorf("审计: %v", e.audit.actions)
	}
}

func TestSkillService_ConfirmGuards(t *testing.T) {
	ctx := context.Background()
	t.Run("别人的暂存、不存在的暂存都是 61001", func(t *testing.T) {
		e := newSkillEnv(t)
		v := mustImport(t, e, 1, standardPkg(t, "demo", "x"))
		if _, err := e.svc.ConfirmImport(ctx, 2, v.ID); codeOf(err) != 61001 {
			t.Errorf("他人: %v", err)
		}
		if _, err := e.svc.ConfirmImport(ctx, 1, "nope"); codeOf(err) != 61001 {
			t.Errorf("不存在: %v", err)
		}
	})
	t.Run("过期后 61001，清理任务删掉对象和行", func(t *testing.T) {
		e := newSkillEnv(t)
		v := mustImport(t, e, 1, standardPkg(t, "demo", "x"))
		*e.now = e.now.Add(2 * time.Hour)
		if _, err := e.svc.ConfirmImport(ctx, 1, v.ID); codeOf(err) != 61001 {
			t.Errorf("%v", err)
		}
		e.svc.Sweep(ctx)
		if len(e.store.keys()) != 0 || len(e.repo.imports) != 0 {
			t.Errorf("过期暂存应被清理: %v %d", e.store.keys(), len(e.repo.imports))
		}
	})
	t.Run("并发确认同一个暂存：只有一个成功，没有多余版本和对象", func(t *testing.T) {
		e := newSkillEnv(t)
		v := mustImport(t, e, 1, standardPkg(t, "demo", "x"))
		var wg sync.WaitGroup
		var okN, goneN int
		var mu sync.Mutex
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := e.svc.ConfirmImport(ctx, 1, v.ID)
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					okN++
				case codeOf(err) == 61001 || codeOf(err) == 61006:
					// 输家：认领暂存时落败（61001），或重新预检时发现赢家已入库而变成同内容（61006）
					goneN++
				default:
					t.Errorf("意外错误: %v", err)
				}
			}()
		}
		wg.Wait()
		if okN != 1 || goneN != 4 {
			t.Errorf("ok=%d gone=%d", okN, goneN)
		}
		e.svc.Sweep(ctx)
		if n := len(e.store.keys()); n != 1 || len(e.repo.versions) != 1 {
			t.Errorf("应只剩 1 个正式对象和 1 个版本: %v %d", e.store.keys(), len(e.repo.versions))
		}
	})
	t.Run("写对象失败：回退预留的版本，技能可重新导入", func(t *testing.T) {
		e := newSkillEnv(t)
		v := mustImport(t, e, 1, standardPkg(t, "demo", "x"))
		e.store.fail = true
		if _, err := e.svc.ConfirmImport(ctx, 1, v.ID); err == nil {
			t.Fatal("写对象失败应报错")
		}
		e.store.fail = false
		if len(e.repo.versions) != 0 {
			t.Errorf("pending 行应被回退: %d", len(e.repo.versions))
		}
		if _, err := e.svc.ConfirmImport(ctx, 1, v.ID); err != nil {
			t.Errorf("暂存仍在，应能重试: %v", err)
		}
	})
	t.Run("pending 超时被清理任务回收（写对象中途崩溃）", func(t *testing.T) {
		e := newSkillEnv(t)
		_, ver, _ := e.repo.ReserveVersionForTest("demo")
		_ = e.store.Put(ctx, ver.PackageKey, strings.NewReader("x"), 1, "")
		e.svc.Sweep(ctx)
		if len(e.repo.versions) != 1 {
			t.Fatal("刚建的 pending 不该被回收")
		}
		*e.now = e.now.Add(time.Hour)
		e.svc.Sweep(ctx)
		if len(e.repo.versions) != 0 || len(e.store.keys()) != 0 {
			t.Errorf("超时 pending 应连对象一起回收: %d %v", len(e.repo.versions), e.store.keys())
		}
	})
}

func TestSkillService_EnableAndDelete(t *testing.T) {
	e := newSkillEnv(t)
	ctx := context.Background()
	mustConfirm(t, e, 1, standardPkg(t, "demo", "x"))

	if _, err := e.svc.SetEnabled(ctx, 1, "nope", true); codeOf(err) != 61004 {
		t.Errorf("不存在: %v", err)
	}
	if _, err := e.svc.SetEnabled(ctx, 1, "keyframe-prompt", false); codeOf(err) != 61007 {
		t.Errorf("内置: %v", err)
	}
	if _, err := e.svc.Rename(ctx, 1, "keyframe-prompt", "x"); codeOf(err) != 61007 {
		t.Errorf("内置改名: %v", err)
	}
	if err := e.svc.DeleteSkill(ctx, 1, "keyframe-prompt"); codeOf(err) != 61007 {
		t.Errorf("内置删除: %v", err)
	}
	it, err := e.svc.SetEnabled(ctx, 1, "demo", true)
	if err != nil || !it.Enabled {
		t.Fatalf("启用: %+v %v", it, err)
	}
	n := len(e.audit.actions)
	if _, err := e.svc.SetEnabled(ctx, 1, "demo", true); err != nil || len(e.audit.actions) != n {
		t.Error("重复启用应幂等，不写审计")
	}
	if chk, _ := e.svc.DeleteCheck(ctx, "demo"); chk.CanDelete || chk.VersionCount != 1 {
		t.Errorf("启用中不能删: %+v", chk)
	}
	if err := e.svc.DeleteSkill(ctx, 1, "demo"); codeOf(err) != 61010 {
		t.Errorf("启用中删除 61010: %v", err)
	}
	if _, err := e.svc.Rename(ctx, 1, "demo", "  新名字 "); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Rename(ctx, 1, "demo", "   "); codeOf(err) != 10001 {
		t.Errorf("空名字: %v", err)
	}
	_, _ = e.svc.SetEnabled(ctx, 1, "demo", false)
	if err := e.svc.DeleteSkill(ctx, 1, "demo"); err != nil {
		t.Fatal(err)
	}
	if len(e.store.keys()) != 0 {
		t.Errorf("删除技能后对象存储不应有残留: %v", e.store.keys())
	}
	if _, err := e.svc.Get(ctx, "demo"); codeOf(err) != 61004 {
		t.Errorf("删除后: %v", err)
	}
}

func TestSkillService_ListAndRead(t *testing.T) {
	e := newSkillEnv(t)
	ctx := context.Background()
	mustConfirm(t, e, 1, standardPkg(t, "demo", "正文 v1"))

	list, err := e.svc.List(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var builtin, imported int
	for _, it := range list {
		if it.Source == SkillSourceBuiltin {
			builtin++
			if !it.Readonly || !it.Enabled {
				t.Errorf("内置应只读且启用: %+v", it)
			}
		} else {
			imported++
		}
	}
	if builtin != 5 || imported != 1 {
		t.Errorf("内置 5 + 导入 1: %d %d", builtin, imported)
	}
	if got, _ := e.svc.List(ctx, "", "disabled"); len(got) != 1 || got[0].Name != "demo" {
		t.Errorf("status=disabled: %+v", got)
	}
	if got, _ := e.svc.List(ctx, "关键帧", ""); len(got) != 1 || got[0].Name != "keyframe-prompt" {
		t.Errorf("q 按显示名过滤: %+v", got)
	}

	t.Run("停用时目录、搜索、读取都看不到", func(t *testing.T) {
		if ok, _ := e.svc.Has(ctx, "demo"); ok {
			t.Error("停用的技能不应在目录里")
		}
		if _, err := e.svc.ReadSkill(ctx, "demo", 0); codeOf(err) != 61004 {
			t.Errorf("%v", err)
		}
	})

	_, _ = e.svc.SetEnabled(ctx, 1, "demo", true)
	t.Run("启用后出现在目录和搜索里", func(t *testing.T) {
		cat, _ := e.svc.Enabled(ctx)
		if len(cat) != 6 || cat[5].Name != "demo" {
			t.Errorf("目录: %+v", cat)
		}
		if res, _ := e.svc.Search(ctx, "方法"); len(res) == 0 {
			t.Error("按说明能搜到")
		}
		if res, _ := e.svc.Search(ctx, "拆镜"); len(res) == 0 || res[0].Name != "script-breakdown" {
			t.Errorf("内置技能仍可按标签搜到: %+v", res)
		}
	})

	t.Run("读正文给资源清单；文本给内容、二进制给提示、清单外报错", func(t *testing.T) {
		c, err := e.svc.ReadSkill(ctx, "demo", 0)
		if err != nil || c.Body != "正文 v1" || len(c.Files) != 3 || c.VersionID == 0 {
			t.Fatalf("%+v %v", c, err)
		}
		f, _, err := e.svc.ReadSkillFile(ctx, "demo", "references/a.md", c.VersionID)
		if err != nil || !strings.Contains(f.Text, "参考") {
			t.Errorf("%+v %v", f, err)
		}
		if f, _, err = e.svc.ReadSkillFile(ctx, "demo", "assets/t.png", c.VersionID); err != nil || !f.Binary || f.Size == 0 {
			t.Errorf("二进制: %+v %v", f, err)
		}
		if _, _, err = e.svc.ReadSkillFile(ctx, "demo", "../../etc/passwd", c.VersionID); codeOf(err) != 10001 {
			t.Errorf("清单外: %v", err)
		}
		if _, _, err = e.svc.ReadSkillFile(ctx, "keyframe-prompt", "a.md", 0); codeOf(err) != 10001 {
			t.Errorf("内置没有资源文件: %v", err)
		}
	})

	t.Run("运行中固定版本：切到 v2 后，已固定的仍读 v1，新运行读 v2", func(t *testing.T) {
		pinned, _ := e.svc.ReadSkill(ctx, "demo", 0)
		mustConfirm(t, e, 1, standardPkg(t, "demo", "正文 v2"))
		_, _ = e.svc.SetActiveVersion(ctx, 1, "demo", 2)
		if c, _ := e.svc.ReadSkill(ctx, "demo", pinned.VersionID); c.Body != "正文 v1" || c.Version != 1 {
			t.Errorf("固定的版本应不变: %+v", c)
		}
		if c, _ := e.svc.ReadSkill(ctx, "demo", 0); c.Body != "正文 v2" || c.Version != 2 {
			t.Errorf("新运行读新版本: %+v", c)
		}
	})

	t.Run("目录缓存：本实例写操作立刻失效", func(t *testing.T) {
		_, _ = e.svc.SetEnabled(ctx, 1, "demo", false)
		if ok, _ := e.svc.Has(ctx, "demo"); ok {
			t.Error("停用后目录应立刻更新")
		}
	})
}

func TestSkillService_VersionFilesAndDownload(t *testing.T) {
	e := newSkillEnv(t)
	ctx := context.Background()
	mustConfirm(t, e, 1, standardPkg(t, "demo", "x"))

	vv, err := e.svc.Version(ctx, "demo", 1)
	if err != nil || vv.Body != "x" || len(vv.Files) != 3 || vv.Name != "demo" {
		t.Fatalf("%+v %v", vv, err)
	}
	if _, err := e.svc.Version(ctx, "demo", 5); codeOf(err) != 61005 {
		t.Errorf("%v", err)
	}
	if f, err := e.svc.File(ctx, "demo", 1, "SKILL.md"); err != nil || !strings.Contains(f.Text, "name: demo") {
		t.Errorf("SKILL.md 可预览: %+v %v", f, err)
	}
	if f, _ := e.svc.File(ctx, "demo", 1, "assets/t.png"); !f.Binary {
		t.Errorf("图片应是二进制: %+v", f)
	}
	if _, err := e.svc.File(ctx, "demo", 1, "nope.md"); codeOf(err) != 10001 {
		t.Errorf("%v", err)
	}
	data, name, err := e.svc.Download(ctx, "demo", 1)
	if err != nil || name != "demo-v1.zip" || len(data) < 50 {
		t.Errorf("下载: %q %d %v", name, len(data), err)
	}
	imp := mustImport(t, e, 1, standardPkg(t, "other", "y"))
	if f, err := e.svc.ImportFile(ctx, 1, imp.ID, "references/a.md"); err != nil || !strings.Contains(f.Text, "参考") {
		t.Errorf("暂存预览: %+v %v", f, err)
	}
	if _, err := e.svc.ImportFile(ctx, 2, imp.ID, "references/a.md"); codeOf(err) != 61001 {
		t.Errorf("他人不能预览: %v", err)
	}
	_ = e.svc.DiscardImport(ctx, 1, imp.ID)
	if len(e.repo.imports) != 0 {
		t.Error("放弃后暂存应删除")
	}
	if err := e.svc.DiscardImport(ctx, 1, imp.ID); err != nil {
		t.Errorf("放弃应幂等: %v", err)
	}
	_ = model.AgentSkill{}
}

func hasCode(issues []SkillIssue, code, level string) bool {
	for _, is := range issues {
		if is.Code == code && is.Level == level {
			return true
		}
	}
	return false
}
