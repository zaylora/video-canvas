package canvasgraph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// MaxOps 是单次编辑调用的操作数上限，防止模型一次写出过大的改动。
const MaxOps = 30

// 操作种类。
const (
	OpCreateNode  = "create_node"  // 新建普通节点
	OpUpdateNode  = "update_node"  // 改节点的标题、提示词、模型、参数
	OpCreateGroup = "create_group" // 新建组，可以直接把已有节点打进去
	OpSetGroup    = "set_group"    // 改组名、颜色，增删成员
	OpConnect     = "connect"      // 连线
	OpMove        = "move"         // 改位置
)

// 节点种类。
const (
	KindScript = "script" // 文本
	KindImage  = "image"  // 图片
	KindVideo  = "video"  // 视频
	KindAudio  = "audio"  // 音频
)

// downstream 是每种节点能直接驱动生成的下游种类，与前端 constants/canvas 的 DOWNSTREAM_KINDS 一致。
var downstream = map[string][]string{
	KindScript: {KindScript, KindImage, KindVideo, KindAudio},
	KindImage:  {KindImage, KindVideo},
	KindVideo:  {KindVideo},
	KindAudio:  {KindVideo},
}

// kindLabel 是节点的默认标题。
var kindLabel = map[string]string{KindScript: "文本", KindImage: "图片", KindVideo: "视频", KindAudio: "音频"}

// hues 是组可选的颜色，与前端 GroupHue 一致。
var hues = map[string]bool{"red": true, "orange": true, "yellow": true, "green": true, "cyan": true, "blue": true, "purple": true, "pink": true}

// mediaParamKeys 是 params 里引用素材的字段：素材归属没法在这里校验，所以不允许 Agent 直接写，
// 需要参考素材时让它连线。
var mediaParamKeys = []string{"images", "videos", "audios"}

// Op 是 Agent 提交的一个编辑操作。字段按操作种类使用，未知字段在解析时就被拒绝，
// 这样 src、outputs、taskId 这类产物字段不可能由 Agent 写入。
type Op struct {
	Op            string         `json:"op"`                      // 操作种类
	TempID        string         `json:"tempId,omitempty"`        // 新建时的临时 id，同一次调用内可以互相引用
	ID            string         `json:"id,omitempty"`            // 目标节点 id（可以是前面操作的 tempId）
	Kind          string         `json:"kind,omitempty"`          // create_node：节点种类
	Label         *string        `json:"label,omitempty"`         // 标题
	Prompt        *string        `json:"prompt,omitempty"`        // 提示词
	Model         *string        `json:"model,omitempty"`         // 模型 key
	Params        map[string]any `json:"params,omitempty"`        // 生成参数，浅合并
	ParentGroup   string         `json:"parentGroup,omitempty"`   // create_node：所属组
	Position      *Point         `json:"position,omitempty"`      // 位置，成员是相对组的
	Color         *string        `json:"color,omitempty"`         // 组背景色，空串清除
	LabelColor    *string        `json:"labelColor,omitempty"`    // 组名颜色，空串清除
	MemberIDs     []string       `json:"memberIds,omitempty"`     // create_group：初始成员
	AddMembers    []string       `json:"addMembers,omitempty"`    // set_group：加入的成员
	RemoveMembers []string       `json:"removeMembers,omitempty"` // set_group：移出的成员
	Source        string         `json:"source,omitempty"`        // connect：起点
	Target        string         `json:"target,omitempty"`        // connect：终点
}

// Issue 是某一项操作的问题，Index 从 0 开始，对应 ops 数组下标。
type Issue struct {
	Index   int    // 操作序号
	Message string // 中文说明，会原样回给模型
}

// ValidationError 是一次 Apply 里所有操作的问题汇总；有任何一项不合法，整次调用都不会生效。
type ValidationError struct {
	Issues []Issue // 全部问题
}

// Error 实现 error。
func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Issues))
	for i, is := range e.Issues {
		parts[i] = fmt.Sprintf("第 %d 项：%s", is.Index+1, is.Message)
	}
	return strings.Join(parts, "；")
}

// ParseOps 解析并粗校验 ops：不认识的字段、空数组、超过 MaxOps 项都会被拒绝。
func ParseOps(raw []byte) ([]Op, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var ops []Op
	if err := dec.Decode(&ops); err != nil {
		return nil, fmt.Errorf("ops 格式不对: %w", err)
	}
	if len(ops) == 0 {
		return nil, errors.New("ops 不能为空")
	}
	if len(ops) > MaxOps {
		return nil, fmt.Errorf("一次最多 %d 项操作，收到 %d 项", MaxOps, len(ops))
	}
	return ops, nil
}

// canLink 判断 from 种类的节点能不能连到 to 种类的节点。
func canLink(from, to string) bool {
	for _, k := range downstream[from] {
		if k == to {
			return true
		}
	}
	return false
}

// validKind 判断是不是认识的节点种类。
func validKind(k string) bool { _, ok := downstream[k]; return ok }
