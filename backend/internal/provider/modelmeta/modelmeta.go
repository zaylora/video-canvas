// Package modelmeta 按模型名查上下文规格（上下文窗口和最大输出），给管理端新建 Agent / 文本模型时自动填写。
//
// 数据是内置快照 limits.json，由 scripts/update-model-limits.mjs 从 models.dev（MIT）生成，不会在运行时联网。
// 它只是「识别建议」：渠道上游自己返回的规格更准，运营也可以手改；查不到就由运营手填。
package modelmeta

import (
	_ "embed" // 快照用 go:embed 打进二进制
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed limits.json
var snapshot []byte

// 与 provider/modelcfg 校验的取值范围一致：窗口 1–1000 万，最大输出 256–100 万且小于窗口。
const (
	maxWindow = 10_000_000
	minOutput = 256
	maxOutput = 1_000_000
)

// Limits 是模型的上下文规格（Token）。
type Limits struct {
	Window int // 上下文窗口
	Output int // 最大输出，小于 Window
}

var (
	once sync.Once
	// exact 是快照：规整后的模型名 → [窗口, 输出]
	exact map[string][2]int
	// byStripped 是去掉日期后缀的模型名 → 快照里的原始名；同名多个日期版本取字典序最大的（最新的）
	byStripped map[string]string
)

// dateSuffix 匹配模型名末尾的日期版本：-2025-09-29、-20250929、-250929。
var dateSuffix = regexp.MustCompile(`-(\d{4}-\d{2}-\d{2}|\d{8}|\d{6})$`)

func load() {
	exact = map[string][2]int{}
	// 快照由脚本生成，随二进制发布，解析失败只可能是文件被改坏；这时当作没有快照，查什么都是「不认识」
	if err := json.Unmarshal(snapshot, &exact); err != nil {
		exact = map[string][2]int{}
	}
	byStripped = make(map[string]string, len(exact))
	for name := range exact {
		s := stripVersion(name)
		if cur, ok := byStripped[s]; !ok || name > cur {
			byStripped[s] = name
		}
	}
}

// normalize 规整模型名：小写、去首尾空白、去厂商前缀（最后一个 / 之后）、:标签（如 :free）和 @后缀。
func normalize(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	id, _, _ = strings.Cut(id, ":")
	id, _, _ = strings.Cut(id, "@")
	return strings.TrimSpace(id)
}

// stripVersion 去掉末尾的 -latest 或日期版本。
func stripVersion(name string) string {
	name = strings.TrimSuffix(name, "-latest")
	return dateSuffix.ReplaceAllString(name, "")
}

// Lookup 按模型名查上下文规格。依次尝试：规整后的原名 → 去掉日期 / latest 后缀 → 同一系列任一日期版本。
// 快照里最大输出不小于窗口的项取窗口的一半；不满足模型配置校验范围的项视为不认识。
func Lookup(id string) (Limits, bool) {
	once.Do(load)
	name := normalize(id)
	if name == "" {
		return Limits{}, false
	}
	stripped := stripVersion(name)
	for _, key := range []string{name, stripped, byStripped[stripped]} {
		if key == "" {
			continue
		}
		if v, ok := exact[key]; ok {
			return Clamp(v[0], v[1])
		}
	}
	return Limits{}, false
}

// Clamp 把一组规格收进模型配置的校验范围：最大输出不小于窗口时取窗口的一半；收不进去（窗口越界、输出太小）返回 false。
// 快照里的数据和渠道上游返回的数据都要先过它，才能直接填进模型配置。
func Clamp(window, output int) (Limits, bool) {
	if window < 1 || window > maxWindow {
		return Limits{}, false
	}
	output = min(output, maxOutput)
	if output >= window {
		output = window / 2
	}
	if output < minOutput {
		return Limits{}, false
	}
	return Limits{Window: window, Output: output}, true
}

// Names 返回快照里的全部模型名（有序），给测试和排查用。
func Names() []string {
	once.Do(load)
	out := make([]string, 0, len(exact))
	for name := range exact {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
