// Package skills 是画布 Agent 的内置技能库：随二进制发布的 Markdown 文件，Agent 用 skill_search 找、skill_read 读。
// 技能内容是写给模型看的方法说明，不是可执行代码；后台管理（草稿、发布）放在二期。
package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed skills/*/SKILL.md
var files embed.FS

// MaxResults 是一次搜索最多返回几个技能。
const MaxResults = 5

// Skill 是一个技能。
type Skill struct {
	Name        string   // 技能名，也是目录名，如 script-breakdown
	Title       string   // 中文显示名，@ 弹层和管理页用；缺省取 Name
	Description string   // 一句话说明，给模型判断要不要读
	Tags        []string // 搜索用的关键词
	Body        string   // 正文
}

// Brief 是技能的摘要，搜索结果里返回它，不含正文。
type Brief struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

var skills = mustLoad()

// mustLoad 解析全部内置技能；文件是随代码发布的，解析失败是编程错误，所以直接 panic，测试会先发现。
func mustLoad() []Skill {
	entries, err := fs.ReadDir(files, "skills")
	if err != nil {
		panic(err)
	}
	out := make([]Skill, 0, len(entries))
	for _, e := range entries {
		raw, err := files.ReadFile("skills/" + e.Name() + "/SKILL.md")
		if err != nil {
			panic(err)
		}
		s, err := parse(string(raw))
		if err != nil {
			panic(fmt.Sprintf("技能 %s：%v", e.Name(), err))
		}
		if s.Name != e.Name() {
			panic(fmt.Sprintf("技能目录 %s 与 name %q 不一致", e.Name(), s.Name))
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// parse 解析「---」包起来的头部（name、description、tags）和正文。
func parse(raw string) (Skill, error) {
	rest, ok := strings.CutPrefix(raw, "---\n")
	if !ok {
		return Skill{}, fmt.Errorf("缺少头部")
	}
	head, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return Skill{}, fmt.Errorf("头部没有结束标记")
	}
	var s Skill
	for _, line := range strings.Split(head, "\n") {
		k, v, _ := strings.Cut(line, ":")
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "name":
			s.Name = v
		case "title":
			s.Title = v
		case "description":
			s.Description = v
		case "tags":
			for _, t := range strings.Split(strings.Trim(v, "[]"), ",") {
				if t = strings.TrimSpace(t); t != "" {
					s.Tags = append(s.Tags, t)
				}
			}
		}
	}
	if s.Name == "" || s.Description == "" {
		return Skill{}, fmt.Errorf("头部缺少 name 或 description")
	}
	if s.Title == "" {
		s.Title = s.Name
	}
	s.Body = strings.TrimSpace(body)
	return s, nil
}

// All 返回全部技能的摘要，按名字排序。
func All() []Brief {
	out := make([]Brief, len(skills))
	for i, s := range skills {
		out[i] = Brief{Name: s.Name, Title: s.Title, Description: s.Description}
	}
	return out
}

// Search 按关键词搜索：query 按空白拆成词，在名字、说明和标签里做不区分大小写的包含匹配，
// 命中的词越多越靠前（同分按名字排），最多返回 MaxResults 个。query 为空返回全部（同样受数量上限）。
func Search(query string) []Brief {
	words := strings.Fields(strings.ToLower(query))
	type hit struct {
		b     Brief
		score int
	}
	var hits []hit
	for _, s := range skills {
		hay := strings.ToLower(s.Name + " " + s.Description + " " + strings.Join(s.Tags, " "))
		score := 0
		for _, w := range words {
			if strings.Contains(hay, w) {
				score++
			}
		}
		if len(words) == 0 || score > 0 {
			hits = append(hits, hit{Brief{Name: s.Name, Title: s.Title, Description: s.Description}, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := make([]Brief, 0, MaxResults)
	for _, h := range hits {
		if len(out) == MaxResults {
			break
		}
		out = append(out, h.b)
	}
	return out
}

// Read 返回技能的正文；name 不存在时第二个返回值为 false。
func Read(name string) (Skill, bool) {
	for _, s := range skills {
		if s.Name == strings.TrimSpace(name) {
			return s, true
		}
	}
	return Skill{}, false
}

// Tags 返回内置技能的搜索关键词；技能不存在返回 nil。后台技能库合并搜索时用它给内置技能同样的匹配范围。
func Tags(name string) []string {
	for _, s := range skills {
		if s.Name == name {
			return s.Tags
		}
	}
	return nil
}
