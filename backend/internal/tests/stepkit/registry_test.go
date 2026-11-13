package stepkit_test

import (
	"encoding/json"
	"testing"

	"video-canvas/internal/stepkit"
)

func TestRegistryRegisterGetList(t *testing.T) {
	reg := stepkit.NewRegistry()
	a := newTestStep()
	b := newTestStep()
	b.Key = "test.other"
	for _, s := range []stepkit.Step{a, b} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	if got, ok := reg.Get("test.other"); !ok || got.Key != "test.other" {
		t.Fatalf("Get 失败：%v %v", got, ok)
	}
	if _, ok := reg.Get("nope"); ok {
		t.Fatal("不存在的环节不应返回")
	}
	list := reg.List()
	if len(list) != 2 || list[0].Key != "test.step" || list[1].Key != "test.other" {
		t.Fatalf("List 应保持注册顺序：%+v", list)
	}
	// 返回的是副本，改它不影响注册表
	list[0].Title = "被改"
	if got, _ := reg.Get("test.step"); got.Title == "被改" {
		t.Fatal("List 返回了内部切片")
	}
}

func TestRegistryRejectsInvalid(t *testing.T) {
	reg := stepkit.NewRegistry()
	if err := reg.Register(newTestStep()); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(s *stepkit.Step){
		"重复 key":         func(s *stepkit.Step) {},
		"空 key":          func(s *stepkit.Step) { s.Key = "" },
		"没有校验函数":         func(s *stepkit.Step) { s.Key = "x.a"; s.Validate = nil },
		"Schema 不是 JSON": func(s *stepkit.Step) { s.Key = "x.b"; s.Schema = json.RawMessage(`{`) },
		"媒体需求未知":         func(s *stepkit.Step) { s.Key = "x.c"; s.Media = "audio" },
		"没有职责说明":         func(s *stepkit.Step) { s.Key = "x.d"; s.Duty = "" },
	}
	for name, mutate := range cases {
		s := newTestStep()
		mutate(&s)
		if err := reg.Register(s); err == nil {
			t.Fatalf("%s：应拒绝注册", name)
		}
	}
	if len(reg.List()) != 1 {
		t.Fatal("拒绝的环节不应进入注册表")
	}
}

func TestStepMethodMaxDefault(t *testing.T) {
	s := newTestStep()
	if s.EffectiveMethodMax() != stepkit.DefaultMethodMax {
		t.Fatalf("默认上限应为 %d", stepkit.DefaultMethodMax)
	}
	s.MethodMax = 500
	if s.EffectiveMethodMax() != 500 {
		t.Fatal("显式上限应生效")
	}
}
