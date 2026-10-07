package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/agent/skills"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// maxMessageChips 是一条消息里最多引用多少个不同的对象，防止用一条消息触发大量校验。
const maxMessageChips = 50

// AgentChip 是消息里的一个行内引用：@[名字](类型:id)。
type AgentChip struct {
	Type string // node / model / skill / asset
	ID   string // 节点 id、模型 key、技能名、素材 id
	Name string // 用户看到的名字，错误提示里用它指明是哪个引用
}

var chipPattern = regexp.MustCompile(`@\[([^\]]*)\]\((node|model|skill|asset):([^)\s]+)\)`)

// ParseAgentChips 取出消息里的行内引用，按出现顺序，同一个对象只算一次。写法不合法的当普通文字。
func ParseAgentChips(msg string) []AgentChip {
	var out []AgentChip
	seen := map[string]bool{}
	for _, m := range chipPattern.FindAllStringSubmatch(msg, -1) {
		key := m[2] + ":" + m[3]
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, AgentChip{Type: m[2], ID: m[3], Name: m[1]})
	}
	return out
}

// checkRefs 校验消息里的引用，并把选中的节点里已经不存在的去掉（界面状态过期不该拦住发送）：
//   - 节点必须在这张画布上；
//   - 模型必须是已发布的生成模型（agent 模型不是生成模型，不能引用）；没有配置注册表时不校验；
//   - 技能必须是内置技能；
//   - 素材附件暂不支持（Agent 没有按素材 id 取用素材的工具，图片请先放进画布再引用节点）。
//
// 不合法返回 10001 并点名是哪个引用。返回过滤后的选中节点。
func (s *AgentService) checkRefs(ctx context.Context, userID, canvasID uint64, msg string, selection []string) ([]string, error) {
	chips := ParseAgentChips(msg)
	if len(chips) > maxMessageChips {
		return nil, errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("一条消息最多引用 %d 个对象", maxMessageChips))
	}
	var g *canvasgraph.Graph
	if len(selection) > 0 || hasChipType(chips, "node") {
		var err error
		if g, err = s.loadGraph(ctx, userID, canvasID); err != nil {
			return nil, err
		}
	}
	for _, c := range chips {
		if err := s.checkChip(ctx, g, c); err != nil {
			return nil, err
		}
	}
	return existingNodes(g, selection), nil
}

// checkChip 校验一个引用。
func (s *AgentService) checkChip(ctx context.Context, g *canvasgraph.Graph, c AgentChip) error {
	bad := func(what string) error {
		return errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("引用的%s「%s」不可用：%s", chipLabel(c.Type), c.Name, what))
	}
	switch c.Type {
	case "node":
		if g.Node(c.ID) == nil {
			return bad("画布上没有这个节点，可能已被删除")
		}
	case "skill":
		if _, ok := skills.Read(c.ID); !ok {
			return bad("没有这个技能")
		}
	case "model":
		return s.checkModelChip(ctx, c, bad)
	default:
		return errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("暂不支持引用素材附件「%s」，请先把图片放进画布，再引用对应的节点", c.Name))
	}
	return nil
}

// checkModelChip 校验模型引用：必须是已发布、已上架的生成模型。
func (s *AgentService) checkModelChip(ctx context.Context, c AgentChip, bad func(string) error) error {
	if s.registry == nil {
		return nil
	}
	infos, err := s.registry.ListModels(ctx, "")
	if err != nil {
		return err
	}
	for _, m := range infos {
		if m.Key == c.ID && m.Kind != "agent" {
			return nil
		}
	}
	return bad("模型未发布或已下线")
}

// loadGraph 读用户这张画布的内容。
func (s *AgentService) loadGraph(ctx context.Context, userID, canvasID uint64) (*canvasgraph.Graph, error) {
	cv, err := s.repo.GetCanvas(ctx, userID, canvasID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrCanvasNotFound
	}
	if err != nil {
		return nil, err
	}
	g, err := canvasgraph.Parse(cv.PayloadJSON)
	if err != nil {
		return nil, errcode.ErrCanvasPayload
	}
	return g, nil
}

func hasChipType(chips []AgentChip, typ string) bool {
	for _, c := range chips {
		if c.Type == typ {
			return true
		}
	}
	return false
}

// existingNodes 只留下画布上还在的节点；g 为空（没读画布）时原样返回。
func existingNodes(g *canvasgraph.Graph, ids []string) []string {
	if g == nil || len(ids) == 0 {
		return ids
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if g.Node(id) != nil {
			out = append(out, id)
		}
	}
	return out
}

// chipLabel 是引用种类的中文名。
func chipLabel(typ string) string {
	switch typ {
	case "node":
		return "节点"
	case "model":
		return "模型"
	case "skill":
		return "技能"
	}
	return "对象"
}
