package stepkit_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"video-canvas/internal/stepkit"
)

// fakeLibrary 是内存技能库：按「名字 + 版本」存正文，enabled 控制启停。
type fakeLibrary struct {
	skills map[string]*fakeSkill
	err    error
}

type fakeSkill struct {
	title    string
	enabled  bool
	versions map[int]string
	scripts  bool
}

func (f *fakeLibrary) LoadMethod(_ context.Context, name string, version, limit int) (*stepkit.SkillText, error) {
	if f.err != nil {
		return nil, f.err
	}
	sk, ok := f.skills[name]
	if !ok || !sk.enabled {
		return nil, stepkit.ErrSkillUnavailable
	}
	body, ok := sk.versions[version]
	if !ok {
		return nil, stepkit.ErrSkillUnavailable
	}
	r := []rune(body)
	if limit > 0 && len(r) > limit {
		r = r[:limit]
	}
	return &stepkit.SkillText{Name: name, Title: sk.title, Version: version, Body: string(r), HasScripts: sk.scripts}, nil
}

func (f *fakeLibrary) FindSkill(context.Context, string) (*stepkit.SkillInfo, error) {
	return nil, stepkit.ErrSkillUnavailable
}

func (f *fakeLibrary) ListSkills(context.Context) ([]stepkit.SkillInfo, error) { return nil, nil }

// memBindings 是内存绑定表。
type memBindings struct{ m map[string]stepkit.Binding }

func (b memBindings) GetBinding(_ context.Context, key string) (stepkit.Binding, bool, error) {
	v, ok := b.m[key]
	return v, ok, nil
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestResolveMethodBuiltin(t *testing.T) {
	step := newTestStep()
	m, err := stepkit.ResolveMethod(context.Background(), &fakeLibrary{}, step, stepkit.Binding{StepKey: step.Key, Source: stepkit.SourceBuiltin})
	if err != nil {
		t.Fatal(err)
	}
	if m.Source != stepkit.SourceBuiltin || m.Body != step.Builtin || m.Fallback || m.SHA256 != sha(step.Builtin) {
		t.Fatalf("内置方法不对：%+v", m)
	}
}

func TestResolveMethodSkillPinnedVersion(t *testing.T) {
	step := newTestStep()
	lib := &fakeLibrary{skills: map[string]*fakeSkill{"tvc": {title: "TVC", enabled: true, versions: map[int]string{1: "旧方法", 2: "新方法"}, scripts: true}}}
	m, err := stepkit.ResolveMethod(context.Background(), lib, step, stepkit.Binding{StepKey: step.Key, Source: stepkit.SourceSkill, SkillName: "tvc", SkillVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if m.Source != stepkit.SourceSkill || m.Version != 1 || m.Body != "旧方法" || m.SkillTitle != "TVC" || !m.HasScripts || m.Fallback {
		t.Fatalf("应钉死在 v1：%+v", m)
	}
	if m.SHA256 != sha("旧方法") {
		t.Fatalf("sha256 应是正文的哈希：%s", m.SHA256)
	}
	snap := m.Snapshot()
	if snap.Source != "skill" || snap.SkillName != "tvc" || snap.Version != 1 || snap.SHA256 != m.SHA256 {
		t.Fatalf("快照不对：%+v", snap)
	}
}

// 技能被停用、版本被删、技能被删时退回内置并标 fallback，不返回错误。
func TestResolveMethodFallsBackToBuiltin(t *testing.T) {
	step := newTestStep()
	lib := &fakeLibrary{skills: map[string]*fakeSkill{
		"off": {title: "停用的", enabled: false, versions: map[int]string{1: "x"}},
		"on":  {title: "启用的", enabled: true, versions: map[int]string{2: "y"}},
	}}
	cases := map[string]stepkit.Binding{
		"技能停用":  {StepKey: step.Key, Source: stepkit.SourceSkill, SkillName: "off", SkillVersion: 1},
		"版本被删":  {StepKey: step.Key, Source: stepkit.SourceSkill, SkillName: "on", SkillVersion: 1},
		"技能已删除": {StepKey: step.Key, Source: stepkit.SourceSkill, SkillName: "", SkillVersion: 1},
	}
	for name, b := range cases {
		m, err := stepkit.ResolveMethod(context.Background(), lib, step, b)
		if err != nil {
			t.Fatalf("%s：%v", name, err)
		}
		if m.Source != stepkit.SourceBuiltin || !m.Fallback || m.Body != step.Builtin || m.FallbackReason == "" {
			t.Fatalf("%s：应退回内置：%+v", name, m)
		}
	}
}

// 技能库读取失败（不是「不可用」）要把错误交给调用方，不能悄悄退回。
func TestResolveMethodPropagatesInfraError(t *testing.T) {
	step := newTestStep()
	boom := errors.New("库连接断了")
	_, err := stepkit.ResolveMethod(context.Background(), &fakeLibrary{err: boom}, step, stepkit.Binding{Source: stepkit.SourceSkill, SkillName: "tvc", SkillVersion: 1})
	if !errors.Is(err, boom) {
		t.Fatalf("应透传基础设施错误：%v", err)
	}
}

func TestResolverNoBindingMeansBuiltin(t *testing.T) {
	step := newTestStep()
	r := stepkit.NewResolver(memBindings{m: map[string]stepkit.Binding{}}, &fakeLibrary{})
	m, err := r.Method(context.Background(), step)
	if err != nil || m.Source != stepkit.SourceBuiltin || m.Fallback {
		t.Fatalf("没有绑定记录应等于内置（且不算兜底）：%+v %v", m, err)
	}
}

// 技能发新版本后绑定不跟随：绑定里记的是 v1，库里多了 v2，解析结果仍是 v1。
func TestResolverVersionStaysPinned(t *testing.T) {
	step := newTestStep()
	lib := &fakeLibrary{skills: map[string]*fakeSkill{"tvc": {title: "TVC", enabled: true, versions: map[int]string{1: "旧"}}}}
	r := stepkit.NewResolver(memBindings{m: map[string]stepkit.Binding{
		step.Key: {StepKey: step.Key, Source: stepkit.SourceSkill, SkillName: "tvc", SkillVersion: 1},
	}}, lib)
	before, err := r.Method(context.Background(), step)
	if err != nil {
		t.Fatal(err)
	}
	lib.skills["tvc"].versions[2] = "新"
	after, err := r.Method(context.Background(), step)
	if err != nil {
		t.Fatal(err)
	}
	if before.Version != 1 || after.Version != 1 || after.Body != "旧" {
		t.Fatalf("版本应钉死：%+v -> %+v", before, after)
	}
}

func TestResolverFallbackHook(t *testing.T) {
	step := newTestStep()
	var got string
	r := stepkit.NewResolver(memBindings{m: map[string]stepkit.Binding{
		step.Key: {StepKey: step.Key, Source: stepkit.SourceSkill, SkillName: "gone", SkillVersion: 1},
	}}, &fakeLibrary{})
	r.OnFallback = func(_ context.Context, s stepkit.Step, _ stepkit.Binding, reason string) { got = s.Key + ":" + reason }
	m, err := r.Method(context.Background(), step)
	if err != nil || !m.Fallback {
		t.Fatalf("%+v %v", m, err)
	}
	if got == "" {
		t.Fatal("兜底时应通知 OnFallback")
	}
}
