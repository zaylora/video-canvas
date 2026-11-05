package drama

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// UnitSource 按单元号拼完整原文，供规划和剧本使用。
func UnitSource(units []Unit) string {
	var b strings.Builder
	for _, u := range units {
		fmt.Fprintf(&b, "## [%d] %s\n%s\n", u.No, u.Title, u.Text)
	}
	return b.String()
}

// Sample 用四分之三预算读开篇，剩余均匀抽取十二段；标题也计入预算。
func Sample(units []Unit, budget int) string {
	budget = min(40000, max(0, budget))
	if utf8.RuneCountInString(UnitSource(units)) <= budget {
		return UnitSource(units)
	}
	var b strings.Builder
	used, index := 0, 0
	for index < len(units) {
		part := UnitSource(units[index : index+1])
		n := utf8.RuneCountInString(part)
		if used+n > budget*3/4 {
			break
		}
		b.WriteString(part)
		used += n
		index++
	}
	// 开篇单元超过预算时仍保留其开头，其余预算留给后文。
	if index == 0 && len(units) > 0 {
		part := Clip(UnitSource(units[:1]), budget*3/4)
		b.WriteString(part)
		used += utf8.RuneCountInString(part)
		index = 1
	}
	count := min(12, len(units)-index)
	for i := 0; i < count; i++ {
		pos := index
		if count > 1 {
			pos += i * (len(units) - index - 1) / (count - 1)
		}
		part := Clip(UnitSource(units[pos:pos+1]), (budget-used)/(count-i))
		b.WriteString(part)
		used += utf8.RuneCountInString(part)
	}
	return b.String()
}

// NextBatch 从已完成游标后取连续完整单元，标题计入上下文预算。
func NextBatch(units []Unit, cursor, end, budget int) ([]Unit, error) {
	out := []Unit{}
	used := 0
	for _, u := range units {
		if u.No <= cursor || u.No > end {
			continue
		}
		n := utf8.RuneCountInString(UnitSource([]Unit{u}))
		if len(out) == 12 || used+n > min(30000, budget) {
			break
		}
		out = append(out, u)
		used += n
	}
	if len(out) == 0 && cursor < end {
		return nil, fmt.Errorf("一个完整单元超过上下文预算")
	}
	return out, nil
}

// PlanEpisode 是规划结果，原文区间必须连续覆盖任务批次。
type PlanEpisode struct {
	Title       string `json:"title"`
	Synopsis    string `json:"synopsis"`
	Hook        string `json:"hook"`
	Cliffhanger string `json:"cliffhanger"`
	UnitFrom    int    `json:"unit_from"`
	UnitTo      int    `json:"unit_to"`
	ScriptMD    string `json:"-"`
}

// PlanOutput 是一次最多六十集的批次规划。
type PlanOutput struct {
	Episodes []PlanEpisode `json:"episodes"`
}

// Validate 校验规划基本结构；范围由 ValidateCoverage 对照任务校验。
func (o PlanOutput) Validate() error {
	if len(o.Episodes) < 1 || len(o.Episodes) > 60 {
		return invalidOutput("每批须包含 1–60 集")
	}
	for _, e := range o.Episodes {
		if !nonemptyText(e.Title) || utf8.RuneCountInString(e.Title) > 255 || !nonemptyText(e.Synopsis) || !nonemptyText(e.Hook) || !nonemptyText(e.Cliffhanger) || e.UnitFrom < 1 || e.UnitTo < e.UnitFrom {
			return invalidOutput("规划标题、梗概、钩子、悬念和单元区间无效")
		}
	}
	return nil
}

// ValidateCoverage 允许同单元跨集，拒绝跳单元、倒序和超出批次范围。
func (o PlanOutput) ValidateCoverage(from, to int) error {
	if err := o.Validate(); err != nil {
		return err
	}
	cursor := from
	for i, e := range o.Episodes {
		if i == 0 && e.UnitFrom != from || e.UnitFrom < cursor || e.UnitFrom > cursor+1 || e.UnitTo > to {
			return invalidOutput("规划必须按序连续覆盖本批全部单元")
		}
		cursor = e.UnitTo
	}
	if cursor != to {
		return invalidOutput("规划未覆盖批次末尾")
	}
	return nil
}

// ScriptEpisodes 带分集标记时一对一导入，否则按整场累计长度，绝不拆场。
func ScriptEpisodes(units []Unit, seconds int) []PlanEpisode {
	marked := false
	for _, u := range units {
		if u.Kind == "episode" {
			marked = true
			break
		}
	}
	out := []PlanEpisode{}
	seenEpisode := false
	var pending []Unit
	chars := 0
	flush := func() {
		if len(pending) == 0 {
			return
		}
		out = append(out, scriptEpisode(pending, marked))
		pending, chars = nil, 0
	}
	for _, u := range units {
		if marked && u.Kind == "episode" {
			if seenEpisode {
				flush()
			}
			seenEpisode = true
		}
		pending = append(pending, u)
		chars += u.Chars
		if !marked && chars >= max(1, seconds)*1000/60 {
			flush()
		}
	}
	flush()
	return out
}

// ScriptBatches 按十二集或三万字拆解，超长单集独占一批，正文保持完整。
func ScriptBatches(units []Unit) [][]Unit {
	out := [][]Unit{}
	batch := []Unit{}
	chars := 0
	for _, u := range units {
		if len(batch) > 0 && (len(batch) == 12 || chars+u.Chars > 30000) {
			out = append(out, batch)
			batch = nil
			chars = 0
		}
		batch = append(batch, u)
		chars += u.Chars
	}
	if len(batch) > 0 {
		out = append(out, batch)
	}
	return out
}

// scriptEpisode 只整理换行与场次头格式，序言并入首集且保留全部对白。
func scriptEpisode(units []Unit, marked bool) PlanEpisode {
	texts := make([]string, len(units))
	title := units[0].Title
	for i, u := range units {
		lines := strings.Split(u.Text, "\n")
		for k, line := range lines {
			if sceneHead.MatchString(strings.TrimSpace(line)) {
				lines[k] = "**" + line + "**"
			}
		}
		texts[i] = strings.Join(lines, "\n")
		if marked && u.Kind == "episode" {
			title = u.Title
		}
	}
	return PlanEpisode{Title: title, UnitFrom: units[0].No, UnitTo: units[len(units)-1].No, ScriptMD: strings.Join(texts, "\n\n")}
}
