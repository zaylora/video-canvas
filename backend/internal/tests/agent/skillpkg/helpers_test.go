package skillpkg_test

import (
	"archive/zip"
	"bytes"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	. "video-canvas/internal/agent/skillpkg"
)

// zent 描述测试 zip 里的一个条目。
type zent struct {
	Name     string
	Data     string
	Mode     os.FileMode // 0 表示不设置
	Store    bool        // true 用 Store，否则 Deflate
	Modified time.Time   // 零值表示不设置
}

const validSkillMD = "---\nname: demo-skill\ndescription: 演示用技能，做什么、何时用\n---\n# 正文\n\n第一行说明\n"

// buildZip 在内存里构造 zip，不写磁盘。
func buildZip(t testing.TB, entries []zent) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.Name, Method: zip.Deflate}
		if e.Store {
			h.Method = zip.Store
		}
		if !e.Modified.IsZero() {
			h.Modified = e.Modified
		}
		if e.Mode != 0 {
			h.SetMode(e.Mode)
		}
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatalf("创建条目 %q 失败：%v", e.Name, err)
		}
		if _, err := fw.Write([]byte(e.Data)); err != nil {
			t.Fatalf("写入条目 %q 失败：%v", e.Name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭 zip 失败：%v", err)
	}
	return buf.Bytes()
}

// standardEntries 是验收条件里的标准包：4 个文件。
func standardEntries() []zent {
	return []zent{
		{Name: "SKILL.md", Data: validSkillMD},
		{Name: "scripts/x.py", Data: "print('hi')\n"},
		{Name: "references/a.md", Data: "# 参考\n"},
		{Name: "assets/t.png", Data: "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"},
	}
}

func skillWith(front string) string { return "---\n" + front + "\n---\n正文\n" }

func hasIssue(r *Result, level, code string) bool {
	for _, i := range r.Issues {
		if i.Code == code && (level == "" || i.Level == level) {
			return true
		}
	}
	return false
}

func issueCodes(r *Result) []string {
	out := make([]string, 0, len(r.Issues))
	for _, i := range r.Issues {
		out = append(out, i.Level+":"+i.Code)
	}
	return out
}

func fileByPath(r *Result, p string) *File {
	for i := range r.Files {
		if r.Files[i].Path == p {
			return &r.Files[i]
		}
	}
	return nil
}

// randBytes 生成只含 4 种字节的伪随机数据，压缩比约 4:1，不会触发压缩炸弹。
func randBytes(n int, seed int64) string {
	rng := rand.New(rand.NewSource(seed))
	b := make([]byte, n)
	for i := range b {
		b[i] = "abcd"[rng.Intn(4)]
	}
	return string(b)
}

func repeat(s string, n int) string { return strings.Repeat(s, n) }
