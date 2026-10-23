package skills_test

import (
	"strings"
	"testing"

	"video-canvas/internal/agent/skills"
)

// 内置技能库目前是空的：技能都由后台导入。这几个用例锁住「空库也能正常工作」，
// 以后再加内置技能时，这里的断言要跟着改。
func TestEmptyBuiltInLibrary(t *testing.T) {
	if got := skills.All(); len(got) != 0 {
		t.Errorf("不应有内置技能: %+v", got)
	}
	if got := skills.Search(""); len(got) != 0 {
		t.Errorf("空库搜索应为空: %+v", got)
	}
	if got := skills.Search("拆镜 关键帧"); len(got) != 0 {
		t.Errorf("空库搜索应为空: %+v", got)
	}
	for _, name := range []string{"script-breakdown", "keyframe-prompt", "../etc/passwd", ""} {
		if _, ok := skills.Read(name); ok {
			t.Errorf("读 %q 应返回 false，不做任何路径解析", name)
		}
	}
	if skills.Tags("script-breakdown") != nil {
		t.Error("不存在的技能没有标签")
	}
}

func TestParse(t *testing.T) {
	doc := "---\nname: demo\ntitle: 演示\ndescription: 一句话说明\ntags: [甲, 乙 ,]\n---\n\n正文第一行\n第二行\n"
	s, err := skills.Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "demo" || s.Title != "演示" || s.Description != "一句话说明" {
		t.Errorf("头部解析错误: %+v", s)
	}
	if len(s.Tags) != 2 || s.Tags[0] != "甲" || s.Tags[1] != "乙" {
		t.Errorf("标签应去掉括号、空白和空项: %v", s.Tags)
	}
	if s.Body != "正文第一行\n第二行" || strings.HasPrefix(s.Body, "---") {
		t.Errorf("正文应去掉头部和首尾空白: %q", s.Body)
	}

	t.Run("没写 title 取 name", func(t *testing.T) {
		s, err := skills.Parse("---\nname: demo\ndescription: d\n---\n正文")
		if err != nil || s.Title != "demo" {
			t.Errorf("%+v %v", s, err)
		}
	})
	t.Run("格式不对报错", func(t *testing.T) {
		for name, raw := range map[string]string{
			"没有头部":     "正文",
			"头部没有结束标记": "---\nname: a\ndescription: d\n正文",
			"缺 name":   "---\ndescription: d\n---\n正文",
			"缺说明":      "---\nname: a\n---\n正文",
		} {
			if _, err := skills.Parse(raw); err == nil {
				t.Errorf("%s 应报错", name)
			}
		}
	})
}
