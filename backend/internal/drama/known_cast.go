package drama

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
)

// CastEntry 是任务可读的实时画布身份，内部集号用于排序与近期优先。
type CastEntry struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	Look    string   `json:"look,omitempty"`
	Desc    string   `json:"desc,omitempty"`
	First   int      `json:"-"`
	Last    int      `json:"-"`
}

type castMeta struct {
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases"`
	Look        string   `json:"look"`
	Episodes    []int    `json:"episodes"`
	Description string   `json:"description"`
}

type castData struct {
	Drama     *castMeta `json:"drama"`
	Character struct {
		Description string `json:"description"`
	} `json:"character"`
	Description string `json:"description"`
	Params      struct {
		Prompt string `json:"prompt"`
	} `json:"params"`
}

// KnownCast 从带 data.drama 的资产读取名单，按首次出现排序并限制一万二千字。
func KnownCast(payload []byte, recentFrom int) ([]CastEntry, error) {
	var doc struct {
		Nodes []struct {
			Data castData `json:"data"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil, err
	}
	entries := []CastEntry{}
	for _, node := range doc.Nodes {
		if e, ok := castFromNode(node.Data); ok {
			// [] 表示全集资产，每一批都在用；null 才是尚未分类。
			if node.Data.Drama.Episodes != nil && len(node.Data.Drama.Episodes) == 0 {
				e.Last = recentFrom
			}
			entries = append(entries, e)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].First < entries[j].First })
	return boundedCast(entries, recentFrom), nil
}

// castFromNode 只读取有效资产身份和节点中用户可编辑的描述。
func castFromNode(data castData) (CastEntry, bool) {
	d := data.Drama
	if d == nil || strings.TrimSpace(d.Name) == "" || d.Type != "character" && d.Type != "scene" && d.Type != "prop" {
		return CastEntry{}, false
	}
	desc := data.Description
	if d.Description != "" {
		desc = d.Description
	}
	if d.Type == "character" && data.Character.Description != "" {
		desc = data.Character.Description
	}
	if desc == "" {
		desc = data.Params.Prompt
	}
	e := CastEntry{Type: d.Type, Name: d.Name, Aliases: append([]string{}, d.Aliases...), Look: d.Look, Desc: Clip(desc, 80)}
	for _, no := range d.Episodes {
		if e.First == 0 || no < e.First {
			e.First = no
		}
		e.Last = max(e.Last, no)
	}
	return e, true
}

// boundedCast 超预算时先保角色，再保近期场景道具，最终仍按首次出现排序。
func boundedCast(entries []CastEntry, recentFrom int) []CastEntry {
	priority := append([]CastEntry{}, entries...)
	for i := range priority {
		if priority[i].Type != "character" && priority[i].Last < recentFrom {
			priority[i].Desc = ""
			priority[i].Look = ""
		}
	}
	sort.SliceStable(priority, func(i, j int) bool { return castRank(priority[i], recentFrom) < castRank(priority[j], recentFrom) })
	out := []CastEntry{}
	used := 2
	for _, e := range priority {
		size := castSize(e)
		if used+size > 12000 {
			e.Desc = ""
			e.Look = ""
			size = castSize(e)
		}
		if used+size > 12000 {
			continue
		}
		out = append(out, e)
		used += size
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].First < out[j].First })
	return out
}

// castRank 定义预算不足时的身份优先级。
func castRank(e CastEntry, recentFrom int) int {
	if e.Type == "character" {
		return 0
	}
	if e.Last >= recentFrom {
		return 1
	}
	return 2
}

// castSize 将 JSON 结构和分隔符也计入字数预算。
func castSize(e CastEntry) int { raw, _ := json.Marshal(e); return utf8.RuneCount(raw) + 1 }
