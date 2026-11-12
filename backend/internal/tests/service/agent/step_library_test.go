package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"video-canvas/internal/stepkit"
)

// 本文件测试 SkillService 作为 stepkit.SkillLibrary：环节协议层经它读技能方法，
// 停用 / 版本不存在统一返回 ErrSkillUnavailable，读版本钉死不跟随生效版本。

func TestStepLibrary_LoadMethodPinnedVersionAndReferences(t *testing.T) {
	e := newSkillEnv(t)
	ctx := context.Background()
	mustConfirm(t, e, 1, standardPkg(t, "demo", "第一版正文，参考 [方法](references/a.md)"))
	if _, err := e.svc.SetEnabled(ctx, 1, "demo", true); err != nil {
		t.Fatal(err)
	}
	mustConfirm(t, e, 1, standardPkg(t, "demo", "第二版正文")) // 新版本不自动生效，但版本本身可读

	v1, err := e.svc.LoadMethod(ctx, "demo", 1, 16000)
	if err != nil || v1.Version != 1 || v1.Title == "" || v1.HasScripts {
		t.Fatalf("读 v1：%+v %v", v1, err)
	}
	if !strings.Contains(v1.Body, "第一版正文") || !strings.Contains(v1.Body, "# 参考") {
		t.Fatalf("应带正文和正文引用的文本文件：%q", v1.Body)
	}
	v2, err := e.svc.LoadMethod(ctx, "demo", 2, 16000)
	if err != nil || !strings.Contains(v2.Body, "第二版正文") || strings.Contains(v2.Body, "第一版") {
		t.Fatalf("钉死版本：读 v2 就是 v2：%+v %v", v2, err)
	}
}

func TestStepLibrary_LoadMethodClipsToLimit(t *testing.T) {
	e := newSkillEnv(t)
	ctx := context.Background()
	mustConfirm(t, e, 1, standardPkg(t, "demo", strings.Repeat("长", 500)))
	_, _ = e.svc.SetEnabled(ctx, 1, "demo", true)
	got, err := e.svc.LoadMethod(ctx, "demo", 1, 100)
	if err != nil || utf8.RuneCountInString(got.Body) != 100 {
		t.Fatalf("应按环节上限截断到 100 字：%d %v", utf8.RuneCountInString(got.Body), err)
	}
}

func TestStepLibrary_UnavailableCases(t *testing.T) {
	e := newSkillEnv(t)
	ctx := context.Background()
	mustConfirm(t, e, 1, standardPkg(t, "demo", "正文"))

	// 未启用
	if _, err := e.svc.LoadMethod(ctx, "demo", 1, 16000); !errors.Is(err, stepkit.ErrSkillUnavailable) {
		t.Fatalf("未启用应不可用：%v", err)
	}
	_, _ = e.svc.SetEnabled(ctx, 1, "demo", true)
	if _, err := e.svc.LoadMethod(ctx, "demo", 1, 16000); err != nil {
		t.Fatalf("启用后可用：%v", err)
	}
	// 版本不存在
	if _, err := e.svc.LoadMethod(ctx, "demo", 9, 16000); !errors.Is(err, stepkit.ErrSkillUnavailable) {
		t.Fatalf("版本不存在应不可用：%v", err)
	}
	// 技能不存在
	if _, err := e.svc.LoadMethod(ctx, "ghost", 1, 16000); !errors.Is(err, stepkit.ErrSkillUnavailable) {
		t.Fatalf("技能不存在应不可用：%v", err)
	}
	if _, err := e.svc.FindSkill(ctx, "ghost"); !errors.Is(err, stepkit.ErrSkillUnavailable) {
		t.Fatalf("FindSkill 不存在：%v", err)
	}
	// 停用后立即不可用
	_, _ = e.svc.SetEnabled(ctx, 1, "demo", false)
	if _, err := e.svc.LoadMethod(ctx, "demo", 1, 16000); !errors.Is(err, stepkit.ErrSkillUnavailable) {
		t.Fatalf("停用后应立即不可用：%v", err)
	}
}

// 删除某个非生效版本后，读该版本不可用，其余版本不受影响。
func TestStepLibrary_DeletedVersionUnavailable(t *testing.T) {
	e := newSkillEnv(t)
	ctx := context.Background()
	mustConfirm(t, e, 1, standardPkg(t, "demo", "第一版"))
	_, _ = e.svc.SetEnabled(ctx, 1, "demo", true)
	mustConfirm(t, e, 1, standardPkg(t, "demo", "第二版"))
	if err := e.svc.DeleteVersion(ctx, 1, "demo", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.LoadMethod(ctx, "demo", 2, 16000); !errors.Is(err, stepkit.ErrSkillUnavailable) {
		t.Fatalf("已删版本应不可用：%v", err)
	}
	if _, err := e.svc.LoadMethod(ctx, "demo", 1, 16000); err != nil {
		t.Fatalf("其他版本不受影响：%v", err)
	}
}

func TestStepLibrary_FindAndListSkills(t *testing.T) {
	e := newSkillEnv(t)
	ctx := context.Background()
	mustConfirm(t, e, 1, standardPkg(t, "alpha", "正文四个字"))
	_, _ = e.svc.SetEnabled(ctx, 1, "alpha", true)
	mustConfirm(t, e, 1, standardPkg(t, "alpha", "第二版"))
	mustConfirm(t, e, 1, standardPkg(t, "beta", "b"))

	info, err := e.svc.FindSkill(ctx, "alpha")
	if err != nil || !info.Enabled || info.ActiveVersion != 1 || len(info.Versions) != 2 || info.Versions[0] != 1 || info.Versions[1] != 2 {
		t.Fatalf("%+v %v", info, err)
	}
	if info.Chars == 0 || info.HasScripts || info.ID == 0 || info.Title == "" {
		t.Fatalf("应带生效版本的正文字数等：%+v", info)
	}
	list, err := e.svc.ListSkills(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("%+v %v", list, err)
	}
	if list[0].Name != "alpha" || list[1].Name != "beta" || list[1].Enabled {
		t.Fatalf("按名字排序，beta 未启用：%+v", list)
	}
}
