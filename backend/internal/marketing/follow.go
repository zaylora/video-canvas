package marketing

import (
	_ "embed" // 内置方法正文
	"encoding/json"
	"fmt"
	"strings"

	"video-canvas/internal/stepkit"
)

// followMethod 是跟拍改写的内置方法正文。
//
//go:embed methods/follow.md
var followMethod string

// 跟拍脚本的镜头数范围。
const (
	followMinShots = 2
	followMaxShots = 12
)

// AssetNote 是素材清单说明：商品图张数与有无模特图，让模型知道人物该怎么处理。
type AssetNote struct {
	ProductImages int  `json:"product_images"` // 商品图张数
	HasModel      bool `json:"has_model"`      // 有模特图
}

// scriptShotSchema 是脚本镜头的 Schema 片段；withPurpose 为真时 purpose 必填（创意脚本）。
func scriptShotSchema(withPurpose bool) string {
	required := `"start", "end", "shot", "camera", "scene", "narration"`
	if withPurpose {
		required = `"start", "end", "purpose", "shot", "camera", "scene", "narration"`
	}
	return fmt.Sprintf(`{"type": "object", "required": [%s], "properties": {
        "start": {"type": "number", "minimum": 0, "description": "开始秒数"},
        "end": {"type": "number", "minimum": 0, "description": "结束秒数"},
        "purpose": {"type": "string", "maxLength": 20, "description": "镜头作用，如 hook / pain / proof / cta"},
        "shot": {"type": "string", "maxLength": 30, "description": "景别"},
        "camera": {"type": "string", "maxLength": 60, "description": "运镜"},
        "scene": {"type": "string", "minLength": 1, "maxLength": 300, "description": "画面：主体 + 动作 + 场景，主体和动作放在最前面"},
        "narration": {"type": "string", "maxLength": 200, "description": "本镜旁白"}
      }}`, required)
}

// followSchema 是跟拍改写的输出 Schema：脚本结构加 follow_map（angle 由系统固定为 follow，不用输出）。
var followSchema = json.RawMessage(fmt.Sprintf(`{
  "type": "object",
  "required": ["title", "shots", "music", "voice", "follow_map"],
  "properties": {
    "title": {"type": "string", "minLength": 1, "maxLength": 40, "description": "脚本标题"},
    "hook": {"type": "string", "maxLength": 60, "description": "开头钩子的一句话概括"},
    "shots": {"type": "array", "minItems": %d, "maxItems": %d, "description": "跟拍脚本的镜头表，首尾相接，总长等于成片时长",
      "items": %s},
    "music": {"type": "string", "maxLength": 60, "description": "配乐风格"},
    "voice": {"type": "string", "maxLength": 60, "description": "旁白音色"},
    "follow_map": {"type": "array", "description": "参考镜头与本脚本镜头的对应关系，必须覆盖拆解里的全部参考镜头",
      "items": {"type": "object", "required": ["ref_shot", "shot"], "properties": {
        "ref_shot": {"type": "integer", "minimum": 1, "description": "参考镜头 id（拆解 shots[].id）"},
        "shot": {"type": "integer", "description": "对应本脚本 shots 的下标，从 0 起"}
      }}}
  }
}`, followMinShots, followMaxShots, scriptShotSchema(false)))

// FollowInput 组装跟拍改写环节的输入：拆解（可能被用户改过）、事实卡、平台、语言、成片时长、素材清单。
func FollowInput(b Brief, facts ProductFacts, rev Reverse, assets AssetNote) stepkit.Input {
	vars := baseVars(b)
	vars["reverse"] = rev
	vars["facts"] = facts
	vars["assets"] = assets
	vars["narration_limit"] = NarrationLimit(resolveLanguage(b), b.Duration)
	if n := len(rev.Shots); n > 0 {
		ref := rev.Shots[n-1].End
		vars["ref_duration"] = ref
		if ref > 0 && b.Duration > 0 {
			vars["time_ratio"] = float64(b.Duration) / ref
		}
	}
	return stepkit.Input{Vars: vars}
}

// DecodeFollow 解码已通过校验的跟拍输出。
func DecodeFollow(raw json.RawMessage) (FollowOutput, error) {
	return decode[FollowOutput](raw, "跟拍脚本")
}

// FollowScript 把跟拍输出转成统一的脚本结构：Angle=follow，并按 follow_map 给每个镜头回填 RefShot
// （对应的参考镜头 id）。多个参考镜头压进同一个镜头时取 follow_map 里先出现的那个；没有对应的镜头 RefShot 为空。
func FollowScript(out FollowOutput) Script {
	shots := make([]ScriptShot, len(out.Shots))
	copy(shots, out.Shots)
	for i := range shots {
		shots[i].RefShot = nil
	}
	for _, m := range out.FollowMap {
		if m.Shot < 0 || m.Shot >= len(shots) || shots[m.Shot].RefShot != nil {
			continue
		}
		ref := m.RefShot
		shots[m.Shot].RefShot = &ref
	}
	return Script{Angle: AngleFollow, Title: out.Title, Hook: out.Hook, Shots: shots, Music: out.Music, Voice: out.Voice}
}

// validateFollow 是环节的 Validate：脚本结构、旁白非空、follow_map 覆盖；软问题为品牌 / 背书 / 极限词。
func validateFollow(out json.RawMessage, in stepkit.Input) (stepkit.Flags, error) {
	o, err := DecodeFollow(out)
	if err != nil {
		return nil, err
	}
	reasons := timelineProblems(scriptSpans(o.Shots), varFloat(in, "duration"))
	for i, s := range o.Shots {
		if blank(s.Narration) {
			reasons = append(reasons, fmt.Sprintf("镜头 %d 的旁白不能为空", i+1))
		}
	}
	rev, hasRev := varAs[Reverse](in, "reverse")
	reasons = append(reasons, followMapProblems(o, rev, hasRev)...)
	if err := stepkit.Hard(reasons...); err != nil {
		return nil, err
	}
	facts, _ := varAs[ProductFacts](in, "facts")
	var revPtr *Reverse
	if hasRev {
		revPtr = &rev
	}
	return ScanScript(o.Shots, varPlatformKey(in), facts, revPtr), nil
}

// followMapProblems 检查 follow_map：镜头下标合法、参考镜头存在且不重复、覆盖拆解里的全部参考镜头。
// 没有拆解（hasRev=false）时只查下标与重复。
func followMapProblems(o FollowOutput, rev Reverse, hasRev bool) []string {
	var out []string
	valid := map[int]bool{}
	for _, s := range rev.Shots {
		valid[s.ID] = true
	}
	seen := map[int]bool{}
	for _, m := range o.FollowMap {
		if m.Shot < 0 || m.Shot >= len(o.Shots) {
			out = append(out, fmt.Sprintf("follow_map 里参考镜头 %d 对应的镜头下标 %d 越界（本脚本有 %d 个镜头，下标从 0 起）", m.RefShot, m.Shot, len(o.Shots)))
		}
		if hasRev && !valid[m.RefShot] {
			out = append(out, fmt.Sprintf("follow_map 里的参考镜头 %d 不在拆解里", m.RefShot))
		}
		if seen[m.RefShot] {
			out = append(out, fmt.Sprintf("follow_map 里参考镜头 %d 重复出现，每个参考镜头只能对应一条", m.RefShot))
		}
		seen[m.RefShot] = true
	}
	if !hasRev {
		return out
	}
	var missing []string
	for _, s := range rev.Shots {
		if !seen[s.ID] {
			missing = append(missing, fmt.Sprint(s.ID))
		}
	}
	if len(missing) > 0 {
		out = append(out, "follow_map 没有覆盖参考镜头 "+strings.Join(missing, "、")+"，必须覆盖拆解里的全部参考镜头")
	}
	return out
}

// previewScript 把一份脚本写成人能读的几行。
func previewScript(b *strings.Builder, s Script) {
	fmt.Fprintf(b, "《%s》钩子：%s；配乐：%s；音色：%s\n", s.Title, s.Hook, s.Music, s.Voice)
	for i, sh := range s.Shots {
		fmt.Fprintf(b, "  镜头 %d（%s–%ss）%s %s：%s 旁白：%s\n", i+1, fmtSec(sh.Start), fmtSec(sh.End), sh.Shot, sh.Camera, sh.Scene, sh.Narration)
	}
}

// previewFollow 把跟拍输出写成人能读的摘要。
func previewFollow(out json.RawMessage) string {
	o, err := DecodeFollow(out)
	if err != nil {
		return clip(string(out), 300)
	}
	var b strings.Builder
	s := FollowScript(o)
	fmt.Fprintf(&b, "跟拍脚本，%d 个镜头，对应 %d 条参考镜头\n", len(s.Shots), len(o.FollowMap))
	previewScript(&b, s)
	return strings.TrimRight(b.String(), "\n")
}

// followStep 返回跟拍改写环节的协议定义。
func followStep() stepkit.Step {
	return stepkit.Step{
		Key:    StepFollow,
		Title:  "跟拍改写",
		Studio: Studio,
		Duty:   "按参考拆解写一份跟拍脚本：镜头结构、节奏、钩子不动，只换成我们的商品、卖点与数字；动作原样带入；去掉第三方品牌；人物不沿用原片外貌。",
		Inputs: []stepkit.Var{
			{Name: "reverse", Type: "object", Desc: "参考片拆解 JSON，可能被用户修改过"},
			{Name: "facts", Type: "object", Desc: "商品事实卡 JSON"},
			{Name: "platform", Type: "object", Desc: "投放平台 {key, name, style, bans}"},
			{Name: "language", Type: "zh | en", Desc: "旁白语言"},
			{Name: "duration", Type: "number", Desc: "成片时长（秒）"},
			{Name: "aspect_ratio", Type: "string", Desc: "画幅"},
			{Name: "assets", Type: "object", Desc: "素材清单 {product_images, has_model}"},
			{Name: "ref_duration", Type: "number", Desc: "参考片时长（秒）"},
			{Name: "time_ratio", Type: "number", Desc: "成片时长 / 参考片时长，镜头时间按它压缩或拉伸"},
			{Name: "narration_limit", Type: "number", Desc: "整份旁白的字数（中文）或词数（英文）参考上限"},
		},
		Media:  stepkit.MediaNone,
		Schema: followSchema,
		Rules: stepkit.Rules{
			Hard: []string{
				fmt.Sprintf("脚本结构合法：镜头 %d–%d 个、从 0 秒开始首尾相接（容差 0.3 秒）、总长与成片时长相差不超过 0.5 秒，每镜旁白非空", followMinShots, followMaxShots),
				"follow_map 覆盖拆解里的全部参考镜头，参考镜头存在且不重复，镜头下标在 0 到镜头数减 1 之间",
			},
			Soft: []string{
				"旁白或画面出现拆解里的第三方品牌（brand）",
				"旁白出现拆解里的背书话术，而事实卡没有依据（claim）",
				"广告法极限词（国内平台，forbidden）",
			},
		},
		Validate: validateFollow,
		Builtin:  followMethod,
		Samples:  followSamples(),
		Preview:  previewFollow,
	}
}
