package skillpkg_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	. "video-canvas/internal/agent/skillpkg"
)

func TestFromZip_标准包(t *testing.T) {
	r, err := FromZip(buildZip(t, standardEntries()))
	if err != nil {
		t.Fatal(err)
	}
	if r.HasErrors() {
		t.Fatalf("标准包不应有错误：%v", issueCodes(r))
	}
	if r.Name != "demo-skill" || r.Title != "demo-skill" || !strings.Contains(r.Description, "演示") {
		t.Fatalf("元数据不对：%+v", r)
	}
	if !strings.HasPrefix(r.Body, "# 正文") {
		t.Fatalf("正文应去掉头部：%q", r.Body)
	}
	want := map[string]struct {
		kind, lang string
		text       bool
	}{
		"SKILL.md":        {KindSkill, "", true},
		"scripts/x.py":    {KindScript, "python", true},
		"references/a.md": {KindDoc, "", true},
		"assets/t.png":    {KindAsset, "", false},
	}
	if len(r.Files) != len(want) {
		t.Fatalf("应有 %d 个文件，实际 %d", len(want), len(r.Files))
	}
	for p, w := range want {
		f := fileByPath(r, p)
		if f == nil {
			t.Fatalf("缺少文件 %s", p)
		}
		if f.Kind != w.kind || f.Lang != w.lang || f.Text != w.text {
			t.Errorf("%s：kind=%s lang=%s text=%v，期望 %+v", p, f.Kind, f.Lang, f.Text, w)
		}
		if len(f.SHA256) != 64 || f.Size <= 0 {
			t.Errorf("%s：size/sha 不对 %+v", p, f)
		}
	}
	if !r.HasScripts || !hasIssue(r, "warn", "HAS_SCRIPTS") {
		t.Errorf("应标记 HasScripts 与 HAS_SCRIPTS：%v", issueCodes(r))
	}
	if r.SHA256 == "" || len(r.Package) == 0 || r.TotalBytes <= 0 {
		t.Errorf("应产出 SHA256/Package/TotalBytes")
	}
	// 按路径排序
	for i := 1; i < len(r.Files); i++ {
		if r.Files[i-1].Path > r.Files[i].Path {
			t.Fatalf("Files 应按路径排序")
		}
	}
}

func TestFromZip_多行YAML与未支持字段(t *testing.T) {
	md := `---
name: pdf-tools
description: >
  处理 PDF 文件：提取文字、合并拆分。
  当用户提到 PDF 时使用。
license: MIT
compatibility: 需要 python3
allowed-tools:
  - Bash(git:*)
  - Read
metadata:
  title: PDF 工具箱
  author: someone
hooks: {}
---
# 正文
`
	r, _ := FromZip(buildZip(t, []zent{{Name: "SKILL.md", Data: md}}))
	if r.HasErrors() {
		t.Fatalf("不应有错误：%v", issueCodes(r))
	}
	if !strings.Contains(r.Description, "提取文字、合并拆分。") || !strings.Contains(r.Description, "当用户提到 PDF 时使用。") {
		t.Fatalf("折行 description 解析不对：%q", r.Description)
	}
	if r.Title != "PDF 工具箱" {
		t.Fatalf("Title 应取 metadata.title：%q", r.Title)
	}
	got := strings.Join(r.Unsupported, ",")
	if got != "allowed-tools,hooks" {
		t.Fatalf("Unsupported = %q", got)
	}
	if !hasIssue(r, "warn", "UNSUPPORTED_FIELD") {
		t.Fatalf("缺 UNSUPPORTED_FIELD：%v", issueCodes(r))
	}
	if _, ok := r.Frontmatter["metadata"].(map[string]any); !ok {
		t.Fatalf("metadata 应为嵌套 map：%T", r.Frontmatter["metadata"])
	}
}

func TestFromZip_预检规则(t *testing.T) {
	bomb := repeat("\x00", 10<<20)
	many := make([]zent, 0, 502)
	many = append(many, zent{Name: "SKILL.md", Data: validSkillMD})
	for i := 0; i < 500; i++ {
		many = append(many, zent{Name: fmt.Sprintf("assets/f%03d.txt", i), Data: "x"})
	}
	tests := []struct {
		name    string
		entries []zent
		raw     []byte
		level   string
		code    string
	}{
		{"缺 SKILL.md", []zent{{Name: "a.md", Data: "x"}}, nil, "error", "NO_SKILL_MD"},
		{"SKILL.md 只在子目录里", []zent{{Name: "x/y/SKILL.md", Data: validSkillMD}, {Name: "x/b.md", Data: "b"}}, nil, "error", "NO_SKILL_MD"},
		{"多个 SKILL.md", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "sub/SKILL.md", Data: validSkillMD}}, nil, "error", "MULTI_SKILL_MD"},
		{"没有 frontmatter", []zent{{Name: "SKILL.md", Data: "# 只有正文\n"}}, nil, "error", "BAD_YAML"},
		{"frontmatter 未闭合", []zent{{Name: "SKILL.md", Data: "---\nname: a\n"}}, nil, "error", "BAD_YAML"},
		{"YAML 语法错误", []zent{{Name: "SKILL.md", Data: skillWith("name: [a\ndescription: x")}}, nil, "error", "BAD_YAML"},
		{"name 含大写", []zent{{Name: "SKILL.md", Data: skillWith("name: Demo\ndescription: d")}}, nil, "error", "BAD_NAME"},
		{"name 以连字符开头", []zent{{Name: "SKILL.md", Data: skillWith("name: -demo\ndescription: d")}}, nil, "error", "BAD_NAME"},
		{"name 连续连字符", []zent{{Name: "SKILL.md", Data: skillWith("name: a--b\ndescription: d")}}, nil, "error", "BAD_NAME"},
		{"name 超过 64", []zent{{Name: "SKILL.md", Data: skillWith("name: " + repeat("a", 65) + "\ndescription: d")}}, nil, "error", "BAD_NAME"},
		{"name 缺失", []zent{{Name: "SKILL.md", Data: skillWith("description: d")}}, nil, "error", "BAD_NAME"},
		{"name 不是字符串", []zent{{Name: "SKILL.md", Data: skillWith("name: 123\ndescription: d")}}, nil, "error", "BAD_NAME"},
		{"description 为空", []zent{{Name: "SKILL.md", Data: skillWith("name: demo\ndescription: \"\"")}}, nil, "error", "BAD_DESCRIPTION"},
		{"description 缺失", []zent{{Name: "SKILL.md", Data: skillWith("name: demo")}}, nil, "error", "BAD_DESCRIPTION"},
		{"description 超过 1024", []zent{{Name: "SKILL.md", Data: skillWith("name: demo\ndescription: " + repeat("好", 1025))}}, nil, "error", "BAD_DESCRIPTION"},
		{"路径穿越 ..", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "../evil.txt", Data: "x"}}, nil, "error", "PATH_ESCAPE"},
		{"路径中间 ..", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "a/../../evil.txt", Data: "x"}}, nil, "error", "PATH_ESCAPE"},
		{"绝对路径", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "/etc/passwd", Data: "x"}}, nil, "error", "PATH_ESCAPE"},
		{"盘符路径", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "C:/x.txt", Data: "x"}}, nil, "error", "PATH_ESCAPE"},
		{"反斜杠路径", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "a\\b.txt", Data: "x"}}, nil, "error", "PATH_ESCAPE"},
		{"NUL 路径", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "a\x00b.txt", Data: "x"}}, nil, "error", "PATH_ESCAPE"},
		{"符号链接", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "link", Data: "/etc/passwd", Mode: os.ModeSymlink | 0o777, Store: true}}, nil, "error", "SYMLINK"},
		{"重复路径", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "a.md", Data: "1"}, {Name: "a.md", Data: "2"}}, nil, "error", "DUP_PATH"},
		{"仅大小写不同的重复路径", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "a.md", Data: "1"}, {Name: "A.md", Data: "2"}}, nil, "error", "DUP_PATH"},
		{"文件名既非 UTF-8 也非 GBK", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "\xff\xfe.md", Data: "x"}}, nil, "error", "BAD_FILENAME_ENCODING"},
		{"文件超过 500 个", many, nil, "error", "TOO_MANY_FILES"},
		{"单文件超过 25MB", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "assets/big.bin", Data: repeat("\x00", MaxFileBytes+1), Store: true}}, nil, "error", "FILE_TOO_LARGE"},
		{"压缩包超过 50MB", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "assets/a.bin", Data: repeat("a", MaxFileBytes), Store: true}, {Name: "assets/b.bin", Data: repeat("b", MaxFileBytes), Store: true}, {Name: "assets/c.bin", Data: repeat("c", 1<<20), Store: true}}, nil, "error", "PACKAGE_TOO_LARGE"},
		{"压缩炸弹 100:1", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "assets/bomb.txt", Data: bomb}}, nil, "error", "ZIP_BOMB"},
		{"不是 zip", nil, []byte("this is not a zip file"), "error", "BAD_ZIP"},
		{"空输入", nil, []byte{}, "error", "BAD_ZIP"},
		{"name 与目录名不一致", []zent{{Name: "other-dir/SKILL.md", Data: validSkillMD}, {Name: "other-dir/a.md", Data: "x"}}, nil, "warn", "NAME_DIR_MISMATCH"},
		{"SKILL.md 超过 500 行", []zent{{Name: "SKILL.md", Data: validSkillMD + repeat("行\n", 501)}}, nil, "warn", "LONG_SKILL_MD"},
		{"引用了不存在的文件", []zent{{Name: "SKILL.md", Data: validSkillMD + "运行 scripts/run.sh 并阅读 [表单](FORMS.md)\n"}}, nil, "warn", "MISSING_REF"},
		{"compatibility 超过 500", []zent{{Name: "SKILL.md", Data: skillWith("name: demo\ndescription: d\ncompatibility: " + repeat("a", 501))}}, nil, "warn", "BAD_COMPATIBILITY"},
		{"包含 ELF 可执行文件", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "bin/tool", Data: "\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00"}}, nil, "warn", "BINARY_EXECUTABLE"},
		{"包含 Mach-O 可执行文件", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "bin/tool", Data: "\xcf\xfa\xed\xfe\x07\x00\x00\x01\x00\x00"}}, nil, "warn", "BINARY_EXECUTABLE"},
		{"包含 PE 可执行文件", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "bin/tool.exe", Data: "MZ\x90\x00\x03\x00\x00\x00\x04\x00\x00\x00\xff\xff"}}, nil, "warn", "BINARY_EXECUTABLE"},
		{"忽略 __MACOSX 与 .DS_Store", []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "__MACOSX/._SKILL.md", Data: "x"}, {Name: ".DS_Store", Data: "x"}}, nil, "info", "IGNORED_FILES"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := tt.raw
			if raw == nil {
				raw = buildZip(t, tt.entries)
			}
			r, err := FromZip(raw)
			if err != nil {
				t.Fatalf("预检不通过不应返回 error：%v", err)
			}
			if !hasIssue(r, tt.level, tt.code) {
				t.Fatalf("期望 %s:%s，实际 %v", tt.level, tt.code, issueCodes(r))
			}
			if tt.level == "error" && !r.HasErrors() {
				t.Fatalf("HasErrors 应为 true")
			}
			if tt.level != "error" && r.HasErrors() {
				t.Fatalf("%s 不应阻断：%v", tt.code, issueCodes(r))
			}
			for _, i := range r.Issues {
				if i.Message == "" {
					t.Errorf("Issue %s 缺中文说明", i.Code)
				}
			}
		})
	}
}

func TestFromZip_不应误报(t *testing.T) {
	t.Run("引用都存在不报 MISSING_REF", func(t *testing.T) {
		r, _ := FromZip(buildZip(t, []zent{
			{Name: "SKILL.md", Data: validSkillMD + "见 references/a.md，运行 `scripts/x.py`，链接 https://e.com/assets/z.png 与 [a](references/a.md#top)\n"},
			{Name: "references/a.md", Data: "a"},
			{Name: "scripts/x.py", Data: "pass"},
		}))
		if hasIssue(r, "", "MISSING_REF") {
			t.Fatalf("不应报：%v %+v", issueCodes(r), r.Issues)
		}
	})
	t.Run("小文件高压缩比不算炸弹", func(t *testing.T) {
		r, _ := FromZip(buildZip(t, []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "a.txt", Data: repeat("a", 2000)}}))
		if hasIssue(r, "", "ZIP_BOMB") {
			t.Fatalf("小文件不应判为炸弹")
		}
	})
	t.Run("恰好 500 个文件通过", func(t *testing.T) {
		es := []zent{{Name: "SKILL.md", Data: validSkillMD}}
		for i := 1; i < 500; i++ {
			es = append(es, zent{Name: fmt.Sprintf("assets/f%03d.txt", i), Data: "x"})
		}
		r, _ := FromZip(buildZip(t, es))
		if hasIssue(r, "", "TOO_MANY_FILES") || r.HasErrors() {
			t.Fatalf("500 个文件应通过：%v", issueCodes(r))
		}
	})
}

func TestFromZip_解压后总量超限(t *testing.T) {
	if testing.Short() {
		t.Skip("构造 100MB+ 数据较慢")
	}
	es := []zent{{Name: "SKILL.md", Data: validSkillMD}}
	for i := 0; i < 5; i++ {
		es = append(es, zent{Name: fmt.Sprintf("assets/f%d.txt", i), Data: randBytes(21<<20, int64(i))})
	}
	raw := buildZip(t, es)
	if len(raw) > MaxZipBytes {
		t.Fatalf("测试数据压缩后应 ≤ 50MB，实际 %d", len(raw))
	}
	r, _ := FromZip(raw)
	if !hasIssue(r, "error", "PACKAGE_TOO_LARGE") {
		t.Fatalf("期望 PACKAGE_TOO_LARGE：%v", issueCodes(r))
	}
}

func TestFromZip_规范化(t *testing.T) {
	t.Run("忽略文件被剔除", func(t *testing.T) {
		r, _ := FromZip(buildZip(t, []zent{
			{Name: "SKILL.md", Data: validSkillMD},
			{Name: "__MACOSX/._SKILL.md", Data: "x"},
			{Name: "sub/.DS_Store", Data: "x"},
			{Name: "Thumbs.db", Data: "x"},
			{Name: ".git/config", Data: "x"},
			{Name: "a.md", Data: "a"},
		}))
		if len(r.Files) != 2 {
			t.Fatalf("应只剩 SKILL.md 与 a.md：%+v", r.Files)
		}
	})
	t.Run("自动剥离唯一外层目录并带目录条目", func(t *testing.T) {
		r, _ := FromZip(buildZip(t, []zent{
			{Name: "demo-skill/", Data: ""},
			{Name: "demo-skill/SKILL.md", Data: validSkillMD},
			{Name: "demo-skill/scripts/x.py", Data: "pass"},
		}))
		if r.HasErrors() || fileByPath(r, "SKILL.md") == nil || fileByPath(r, "scripts/x.py") == nil {
			t.Fatalf("剥离失败：%v %+v", issueCodes(r), r.Files)
		}
		if hasIssue(r, "", "NAME_DIR_MISMATCH") {
			t.Fatalf("目录名一致不应提示")
		}
	})
	t.Run("SKILL.md 不区分大小写并规范为 SKILL.md", func(t *testing.T) {
		r, _ := FromZip(buildZip(t, []zent{{Name: "Skill.MD", Data: validSkillMD}}))
		if r.HasErrors() || fileByPath(r, "SKILL.md") == nil {
			t.Fatalf("应识别：%v %+v", issueCodes(r), r.Files)
		}
	})
	t.Run("GBK 文件名按 GBK 解码", func(t *testing.T) {
		// 「说明」的 GBK 编码是 CB B5 C3 F7
		r, _ := FromZip(buildZip(t, []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "references/\xcb\xb5\xc3\xf7.md", Data: "x"}}))
		if r.HasErrors() || fileByPath(r, "references/说明.md") == nil {
			t.Fatalf("GBK 解码失败：%v %+v", issueCodes(r), r.Files)
		}
	})
	t.Run("UTF-8 中文文件名保持不变", func(t *testing.T) {
		r, _ := FromZip(buildZip(t, []zent{{Name: "SKILL.md", Data: validSkillMD}, {Name: "references/说明.md", Data: "x"}}))
		if r.HasErrors() || fileByPath(r, "references/说明.md") == nil {
			t.Fatalf("%v %+v", issueCodes(r), r.Files)
		}
	})
	t.Run("assets 下的 md 是 asset，scripts 下任意文件是 script", func(t *testing.T) {
		r, _ := FromZip(buildZip(t, []zent{
			{Name: "SKILL.md", Data: validSkillMD},
			{Name: "assets/note.md", Data: "x"},
			{Name: "scripts/run", Data: "x"},
			{Name: "tool.sh", Data: "x"},
			{Name: "data.json", Data: "{}"},
			{Name: "notes.txt", Data: "x"},
			{Name: "weird.xyz", Data: "x"},
		}))
		want := map[string][2]string{
			"assets/note.md": {KindAsset, ""},
			"scripts/run":    {KindScript, ""},
			"tool.sh":        {KindScript, "bash"},
			"data.json":      {KindAsset, ""},
			"notes.txt":      {KindDoc, ""},
			"weird.xyz":      {KindOther, ""},
		}
		for p, w := range want {
			f := fileByPath(r, p)
			if f == nil || f.Kind != w[0] || f.Lang != w[1] {
				t.Errorf("%s 分类不对：%+v 期望 %v", p, f, w)
			}
		}
	})
	t.Run("text 判定：无效 UTF-8 与 NUL 为非文本", func(t *testing.T) {
		r, _ := FromZip(buildZip(t, []zent{
			{Name: "SKILL.md", Data: validSkillMD},
			{Name: "a.txt", Data: "你好"},
			{Name: "b.txt", Data: "a\x00b"},
			{Name: "c.txt", Data: "\xff\xfe"},
			{Name: "d.txt", Data: ""},
		}))
		want := map[string]bool{"a.txt": true, "b.txt": false, "c.txt": false, "d.txt": true}
		for p, w := range want {
			if f := fileByPath(r, p); f == nil || f.Text != w {
				t.Errorf("%s text 应为 %v：%+v", p, w, f)
			}
		}
	})
}

func TestFromZip_规范化哈希(t *testing.T) {
	base := standardEntries()
	t1 := time.Date(2020, 1, 2, 3, 4, 6, 0, time.UTC)
	t2 := time.Date(2024, 8, 9, 10, 11, 12, 0, time.UTC)

	reordered := []zent{base[3], base[1], base[0], base[2]}
	for i := range reordered {
		reordered[i].Modified = t2
		reordered[i].Mode = 0o755
		reordered[i].Store = i%2 == 0
	}
	withTime := append([]zent(nil), base...)
	for i := range withTime {
		withTime[i].Modified = t1
	}
	wrapped := make([]zent, 0, len(base)+3)
	wrapped = append(wrapped, zent{Name: "demo-skill/"})
	for _, e := range base {
		e.Name = "demo-skill/" + e.Name
		wrapped = append(wrapped, e)
	}
	wrapped = append(wrapped, zent{Name: "__MACOSX/._x", Data: "junk"}, zent{Name: ".DS_Store", Data: "junk"})

	ref, _ := FromZip(buildZip(t, base))
	for name, es := range map[string][]zent{"不同顺序/时间/权限/压缩方式": reordered, "不同时间戳": withTime, "外层目录与垃圾文件": wrapped} {
		t.Run(name+"得到同一哈希", func(t *testing.T) {
			r, _ := FromZip(buildZip(t, es))
			if r.HasErrors() {
				t.Fatalf("%v", issueCodes(r))
			}
			if r.SHA256 != ref.SHA256 || string(r.Package) != string(ref.Package) {
				t.Fatalf("哈希不同：%s vs %s", r.SHA256, ref.SHA256)
			}
		})
	}
	t.Run("内容不同哈希不同", func(t *testing.T) {
		es := append([]zent(nil), base...)
		es[1].Data = "print('changed')\n"
		r, _ := FromZip(buildZip(t, es))
		if r.SHA256 == ref.SHA256 {
			t.Fatalf("内容变了哈希应变")
		}
	})
	t.Run("文件名不同哈希不同", func(t *testing.T) {
		es := append([]zent(nil), base...)
		es[2].Name = "references/b.md"
		r, _ := FromZip(buildZip(t, es))
		if r.SHA256 == ref.SHA256 {
			t.Fatalf("路径变了哈希应变")
		}
	})
	t.Run("规范化包再次预检哈希不变", func(t *testing.T) {
		r, _ := FromZip(ref.Package)
		if r.SHA256 != ref.SHA256 || string(r.Package) != string(ref.Package) {
			t.Fatalf("规范化应幂等")
		}
	})
	t.Run("FromFiles 与 FromZip 同内容同哈希", func(t *testing.T) {
		in := make([]Input, 0, len(base))
		for _, e := range base {
			in = append(in, Input{Path: "demo-skill/" + e.Name, Data: []byte(e.Data)})
		}
		r, err := FromFiles(in)
		if err != nil {
			t.Fatal(err)
		}
		if r.SHA256 != ref.SHA256 {
			t.Fatalf("哈希不同：%v", issueCodes(r))
		}
	})
}

func TestFromFiles(t *testing.T) {
	t.Run("单个 SKILL.md", func(t *testing.T) {
		r, err := FromFiles([]Input{{Path: "SKILL.md", Data: []byte(validSkillMD)}})
		if err != nil || r.HasErrors() || r.Name != "demo-skill" || len(r.Files) != 1 || r.Files[0].Kind != KindSkill {
			t.Fatalf("%v %v %+v", err, issueCodes(r), r)
		}
	})
	t.Run("文件夹拖入带外层目录", func(t *testing.T) {
		r, _ := FromFiles([]Input{
			{Path: "my-dir/SKILL.md", Data: []byte(validSkillMD)},
			{Path: "my-dir/scripts/x.py", Data: []byte("pass")},
		})
		if r.HasErrors() || fileByPath(r, "scripts/x.py") == nil || !hasIssue(r, "warn", "NAME_DIR_MISMATCH") {
			t.Fatalf("%v %+v", issueCodes(r), r.Files)
		}
	})
	tests := []struct {
		name string
		in   []Input
		code string
	}{
		{"路径穿越", []Input{{Path: "SKILL.md", Data: []byte(validSkillMD)}, {Path: "../x", Data: []byte("x")}}, "PATH_ESCAPE"},
		{"路径为空", []Input{{Path: "", Data: []byte("x")}}, "PATH_ESCAPE"},
		{"重复路径", []Input{{Path: "SKILL.md", Data: []byte(validSkillMD)}, {Path: "skill.md", Data: []byte(validSkillMD)}}, "DUP_PATH"},
		{"缺 SKILL.md", []Input{{Path: "a.md", Data: []byte("x")}}, "NO_SKILL_MD"},
		{"空输入", nil, "NO_SKILL_MD"},
		{"单文件过大", []Input{{Path: "SKILL.md", Data: []byte(validSkillMD)}, {Path: "big.bin", Data: make([]byte, MaxFileBytes+1)}}, "FILE_TOO_LARGE"},
		{"name 大写", []Input{{Path: "SKILL.md", Data: []byte(skillWith("name: Demo\ndescription: d"))}}, "BAD_NAME"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := FromFiles(tt.in)
			if err != nil || !hasIssue(r, "error", tt.code) {
				t.Fatalf("期望 %s：%v %v", tt.code, err, issueCodes(r))
			}
		})
	}
	t.Run("文件超过 500 个", func(t *testing.T) {
		in := []Input{{Path: "SKILL.md", Data: []byte(validSkillMD)}}
		for i := 0; i < 500; i++ {
			in = append(in, Input{Path: fmt.Sprintf("a/%d.txt", i), Data: []byte("x")})
		}
		r, _ := FromFiles(in)
		if !hasIssue(r, "error", "TOO_MANY_FILES") {
			t.Fatalf("%v", issueCodes(r))
		}
	})
}

func TestParseFrontmatter(t *testing.T) {
	t.Run("多行与嵌套", func(t *testing.T) {
		head, body, err := ParseFrontmatter([]byte("---\r\nname: a\r\ndescription: >\r\n  第一行\r\n  第二行\r\nmetadata:\r\n  k: v\r\n---\r\n正文\r\n"))
		if err != nil {
			t.Fatal(err)
		}
		if head["name"] != "a" || !strings.Contains(head["description"].(string), "第二行") {
			t.Fatalf("%v", head)
		}
		if head["metadata"].(map[string]any)["k"] != "v" || strings.TrimSpace(body) != "正文" {
			t.Fatalf("%v %q", head, body)
		}
	})
	t.Run("带 BOM", func(t *testing.T) {
		head, _, err := ParseFrontmatter([]byte("\xef\xbb\xbf---\nname: a\n---\nx"))
		if err != nil || head["name"] != "a" {
			t.Fatalf("%v %v", head, err)
		}
	})
	t.Run("未加引号的冒号兜底", func(t *testing.T) {
		head, _, err := ParseFrontmatter([]byte("---\nname: a\ndescription: Use this when: the user asks about PDFs\n---\nx"))
		if err != nil {
			t.Fatalf("应兜底成功：%v", err)
		}
		if head["description"] != "Use this when: the user asks about PDFs" {
			t.Fatalf("%q", head["description"])
		}
	})
	for name, raw := range map[string]string{
		"没有头部":    "# hi\n",
		"没有结束分隔线": "---\nname: a\n",
		"YAML 错误": "---\nname: [a\n---\nx",
		"头部不是映射":  "---\n- a\n- b\n---\nx",
	} {
		t.Run(name+"返回错误", func(t *testing.T) {
			if _, _, err := ParseFrontmatter([]byte(raw)); err == nil {
				t.Fatalf("应返回错误")
			}
		})
	}
}
