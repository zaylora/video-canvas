package canvasgraph

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/storyboard"
)

// ShotSizes、CameraMoves 是景别、运镜的可选值，规则在 storyboard 包里（和前端同一份）。
var (
	ShotSizes   = storyboard.ShotSizes
	CameraMoves = storyboard.CameraMoves
)

// 镜头字段的默认值和限制，取自 storyboard 包。
const (
	defaultShotSize   = storyboard.DefaultShotSize
	defaultCameraMove = storyboard.DefaultCameraMove
	defaultDuration   = storyboard.DefaultDuration
	shotTextMax       = storyboard.ShotTextMax
	maxShots          = storyboard.MaxShots
)

// StoryboardOp 是 Agent 对分镜表镜头的修改，对应节点的 data.storyboard。
// Shots 整表替换（新建、拆分镜用）；Edits 按顺序逐行改。两者都给时先替换再改。
type StoryboardOp struct {
	Shots *[]ShotFields `json:"shots,omitempty"` // 整表替换
	Edits []ShotEdit    `json:"edits,omitempty"` // 逐行修改
}

// ShotFields 是一行镜头的字段；nil 表示不改（新镜头取默认值）。
type ShotFields struct {
	Scene    *string  `json:"scene,omitempty"`    // 画面描述，新镜头必填
	Shot     *string  `json:"shot,omitempty"`     // 景别，只能取 ShotSizes
	Camera   *string  `json:"camera,omitempty"`   // 运镜，只能取 CameraMoves
	Action   *string  `json:"action,omitempty"`   // 动作 / 走位
	Dialogue *string  `json:"dialogue,omitempty"` // 对白，旁白也写在这里
	Duration *float64 `json:"duration,omitempty"` // 时长（秒），越界修正到 1–60
	// ImageNodeID 把这一镜的分镜图指向画布上已有的图片节点；VideoNodeID 同样指向已有视频节点。
	// 分镜表是后来加的、或者素材不是从这张表生成的时，靠这两个字段把已有素材挂进来。
	// 传空串表示取消关联；节点必须存在，种类也要对得上。
	ImageNodeID *string `json:"imageNodeId,omitempty"`
	VideoNodeID *string `json:"videoNodeId,omitempty"`
}

// ShotEdit 是一步逐行修改。镜号从 1 起，按这一步执行前的顺序算（前面的插入、删除会让后面的镜号变）。
type ShotEdit struct {
	Op    string      `json:"op"`              // update / insert / delete / move
	No    int         `json:"no,omitempty"`    // update / delete / move：第几镜
	After *int        `json:"after,omitempty"` // insert：插在第几镜后面，0 是最前面
	To    int         `json:"to,omitempty"`    // move：挪到第几镜的位置
	Set   *ShotFields `json:"set,omitempty"`   // update / insert：要写的字段
}

// storyboardEditor 在一份镜头列表上执行修改，收集不拦截的提醒。
type storyboardEditor struct {
	shots   []map[string]any
	newID   func() string
	node    func(string) Node // 按 id 取画布上的节点，挂已有素材时校验用
	clamped bool              // 有时长被修正过
}

// newStoryboard 按 Agent 给的数据构造新分镜表的 data.storyboard；不给就是空表。
func (a *applier) newStoryboard(op *StoryboardOp) (map[string]any, error) {
	e := &storyboardEditor{newID: func() string { return a.opt.newID("shot_") }, node: a.g.Node}
	if op != nil {
		if err := e.apply(op); err != nil {
			return nil, err
		}
	}
	a.noteClamp(e)
	return map[string]any{"shots": e.list()}, nil
}

// patchStoryboard 改已有分镜表的镜头；镜头里 Agent 不认识的字段（以后的分镜图、关联节点）原样保留。
func (a *applier) patchStoryboard(n Node, op *StoryboardOp) error {
	e := &storyboardEditor{newID: func() string { return a.opt.newID("shot_") }, node: a.g.Node, shots: existingShots(n)}
	if err := e.apply(op); err != nil {
		return err
	}
	a.noteClamp(e)
	n.Data()["storyboard"] = map[string]any{"shots": e.list()}
	return nil
}

// noteClamp 时长被修正过就提醒一次。
func (a *applier) noteClamp(e *storyboardEditor) {
	if e.clamped {
		a.warnings = append(a.warnings, fmt.Sprintf("第 %d 项：有镜头的时长超出 1–60 秒或多于 1 位小数，已修正", a.current+1))
	}
}

// existingShots 读出节点上的镜头，拷贝一份再改；坏数据丢掉。
func existingShots(n Node) []map[string]any {
	sb, _ := n.Data()["storyboard"].(map[string]any)
	raw, _ := sb["shots"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, v := range raw {
		if m, ok := v.(map[string]any); ok {
			out = append(out, deepCopy(m).(map[string]any))
		}
	}
	return out
}

// list 转成写回 data 的数组。
func (e *storyboardEditor) list() []any {
	out := make([]any, len(e.shots))
	for i, s := range e.shots {
		out[i] = s
	}
	return out
}

// apply 先整表替换，再按顺序逐行改。
func (e *storyboardEditor) apply(op *StoryboardOp) error {
	if op.Shots == nil && len(op.Edits) == 0 {
		return errors.New("storyboard 至少要给 shots（整表替换）或 edits（逐行修改）")
	}
	if op.Shots != nil {
		if len(*op.Shots) > maxShots {
			return fmt.Errorf("一张分镜表最多 %d 镜，收到 %d 镜", maxShots, len(*op.Shots))
		}
		e.shots = nil
		for i, f := range *op.Shots {
			s, err := e.newShot(f)
			if err != nil {
				return fmt.Errorf("shots 第 %d 镜：%w", i+1, err)
			}
			e.shots = append(e.shots, s)
		}
	}
	for i, ed := range op.Edits {
		if err := e.edit(ed); err != nil {
			return fmt.Errorf("edits 第 %d 步：%w", i+1, err)
		}
	}
	if len(e.shots) > maxShots {
		return fmt.Errorf("一张分镜表最多 %d 镜", maxShots)
	}
	return nil
}

// edit 执行一步逐行修改。
func (e *storyboardEditor) edit(ed ShotEdit) error {
	switch ed.Op {
	case "update":
		i, err := e.index(ed.No)
		if err != nil {
			return err
		}
		if ed.Set == nil {
			return errors.New("update 要写 set（要改的字段）")
		}
		return e.write(e.shots[i], *ed.Set, false)
	case "insert":
		if ed.After == nil || *ed.After < 0 || *ed.After > len(e.shots) {
			return fmt.Errorf("insert 的 after 要在 0 到 %d 之间（0 是插到最前面）", len(e.shots))
		}
		if ed.Set == nil {
			return errors.New("insert 要写 set，至少有画面描述 scene")
		}
		s, err := e.newShot(*ed.Set)
		if err != nil {
			return err
		}
		e.shots = slices.Insert(e.shots, *ed.After, s)
	case "delete":
		i, err := e.index(ed.No)
		if err != nil {
			return err
		}
		e.shots = slices.Delete(e.shots, i, i+1)
	case "move":
		i, err := e.index(ed.No)
		if err != nil {
			return err
		}
		if ed.To < 1 || ed.To > len(e.shots) {
			return fmt.Errorf("move 的 to 要在 1 到 %d 之间", len(e.shots))
		}
		s := e.shots[i]
		e.shots = slices.Insert(slices.Delete(e.shots, i, i+1), ed.To-1, s)
	default:
		return fmt.Errorf("op %q 不认识，可选 update / insert / delete / move", ed.Op)
	}
	return nil
}

// index 把镜号换成下标，越界时说清有几镜。
func (e *storyboardEditor) index(no int) (int, error) {
	if no < 1 || no > len(e.shots) {
		return 0, fmt.Errorf("没有第 %d 镜（现在共 %d 镜，镜号从 1 起）", no, len(e.shots))
	}
	return no - 1, nil
}

// newShot 造一行新镜头：画面必填，景别、运镜、时长不给取默认值。
func (e *storyboardEditor) newShot(f ShotFields) (map[string]any, error) {
	s := map[string]any{
		"id": e.newID(), "scene": "", "shot": defaultShotSize, "camera": defaultCameraMove,
		"action": "", "dialogue": "", "duration": defaultDuration,
	}
	if err := e.write(s, f, true); err != nil {
		return nil, err
	}
	return s, nil
}

// write 把给了的字段写进一行镜头，逐项校验。isNew 时画面必须给；已有镜头的画面不能改成空。
func (e *storyboardEditor) write(s map[string]any, f ShotFields, isNew bool) error {
	if isNew && f.Scene == nil {
		return errors.New("新镜头要写画面描述 scene")
	}
	if f == (ShotFields{}) {
		return errors.New("set 里没有要改的字段")
	}
	texts := []struct {
		key, label string
		v          *string
	}{
		{"scene", "画面描述", f.Scene}, {"action", "动作", f.Action}, {"dialogue", "对白", f.Dialogue},
	}
	for _, t := range texts {
		if t.v == nil {
			continue
		}
		v := strings.TrimSpace(*t.v)
		if utf8.RuneCountInString(v) > shotTextMax {
			return fmt.Errorf("%s最多 %d 字", t.label, shotTextMax)
		}
		if t.key == "scene" && v == "" {
			return errors.New("画面描述 scene 不能为空")
		}
		s[t.key] = v
	}
	if f.Shot != nil {
		if !slices.Contains(ShotSizes, *f.Shot) {
			return fmt.Errorf("景别 %q 不在可选范围，可选：%s", *f.Shot, strings.Join(ShotSizes, " / "))
		}
		s["shot"] = *f.Shot
	}
	if f.Camera != nil {
		if !slices.Contains(CameraMoves, *f.Camera) {
			return fmt.Errorf("运镜 %q 不在可选范围，可选：%s", *f.Camera, strings.Join(CameraMoves, " / "))
		}
		s["camera"] = *f.Camera
	}
	if f.Duration != nil {
		d := storyboard.ClampDuration(*f.Duration)
		e.clamped = e.clamped || d != *f.Duration
		s["duration"] = d
	}
	// 关联画布上已有的节点：空串取消关联，否则节点必须在画布上、种类还要对得上。
	// 这是「分镜表后来才加」「镜头不是从这张表生成的」两种情况下唯一能把已有素材挂进来的路。
	for _, link := range []struct {
		key, label, kind string
		v                *string
	}{
		{"imageNodeId", "分镜图节点", KindImage, f.ImageNodeID},
		{"videoNodeId", "视频节点", KindVideo, f.VideoNodeID},
	} {
		if link.v == nil {
			continue
		}
		id := strings.TrimSpace(*link.v)
		if id == "" {
			delete(s, link.key)
			continue
		}
		n := Node(nil)
		if e.node != nil {
			n = e.node(id)
		}
		if n == nil {
			return fmt.Errorf("%s %q 不在画布上（先用 canvas_get_state 读一下节点 id）", link.label, id)
		}
		if n.Kind() != link.kind {
			name := kindLabel[n.Kind()]
			if name == "" {
				name = n.Kind()
			}
			return fmt.Errorf("节点 %q 是%s，不能当%s", id, name, link.label)
		}
		s[link.key] = id
	}
	return nil
}

// storyboardSummary 是目录里分镜表的摘要：「N 镜」、已挂能用的视频数，加首镜画面。
// 视频数只数「节点还在、且已经出片」的：模型据此判断要不要先让用户补素材再合成。
func storyboardSummary(g *Graph, n Node, runes int) string {
	shots := existingShots(n)
	if len(shots) == 0 {
		return "0 镜"
	}
	videos := 0
	for _, s := range shots {
		up := g.Node(str(s["videoNodeId"]))
		if up != nil && up.Kind() == KindVideo && activeAssetID(up.Data()) != "" {
			videos++
		}
	}
	head := fmt.Sprintf("%d 镜", len(shots))
	if videos > 0 {
		head += fmt.Sprintf(" · 视频 %d 段已出片", videos)
	}
	return truncate(fmt.Sprintf("%s · %s", head, str(shots[0]["scene"])), runes)
}
