package agent

import (
	"context"
	"errors"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// AgentDramaService 复用短剧项目、分集和流水线的业务入口。
type AgentDramaService interface {
	// Get 按用户读取短剧及各集摘要。
	Get(context.Context, uint64, uint64) (*model.DramaView, error)
	// Update 按审批时的revision修改大纲和人物传记。
	Update(context.Context, uint64, uint64, *model.UpdateDramaReq) (*model.DramaView, error)
	// GetEpisode 读取当前用户的完整分集。
	GetEpisode(context.Context, uint64, uint64, int) (*model.DramaEpisodeView, error)
	// UpdateEpisode 沿用分集锁定和revision校验。
	UpdateEpisode(context.Context, uint64, uint64, int, *model.UpdateDramaEpisodeReq) (*model.DramaEpisodeView, error)
	// Estimate 复用提交任务的预检和积分估价。
	Estimate(context.Context, uint64, uint64, *model.DramaJobReq) (int, error)
	// CreateAgentJob 以用户与审批ID持久去重，原子提交任务和冻结积分。
	CreateAgentJob(context.Context, uint64, uint64, uint64, *model.DramaJobReq) (*model.DramaJobView, error)
}

// AgentDramaSegments 复用片段归属、revision和事件推送规则。
type AgentDramaSegments interface {
	// List 返回当前用户指定项目分集的片段。
	List(context.Context, uint64, uint64, int) ([]model.DramaSegmentView, error)
	// Update 按revision更新片段并推送drama.segment事件。
	Update(context.Context, uint64, uint64, *model.UpdateDramaSegmentReq) (*model.DramaSegmentView, error)
}

// CheckCanvasMode 校验会话模式及画布归属；drama仅允许绑定存活短剧的专属画布。
func (b *AgentBridge) CheckCanvasMode(ctx context.Context, userID, canvasID uint64, mode string) error {
	// 1. 保持旧模式的归属校验和错误语义；未知模式不传给Agent服务。
	if mode != "" && !model.ValidAgentMode(mode) {
		return errcode.ErrInvalidParams.WithMsg("任务模式不认识")
	}
	if mode != model.AgentModeDrama {
		return b.CheckCanvas(ctx, userID, canvasID)
	}
	_, err := b.dramaProject(ctx, userID, canvasID)
	return err
}

// dramaProject 从用户画布反查项目，工具参数不能指定其他项目。
func (b *AgentBridge) dramaProject(ctx context.Context, userID, canvasID uint64) (*model.DramaView, error) {
	if userID == 0 || canvasID == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("缺少 user_id 或 canvas_id")
	}
	c, err := b.d.Repo.GetCanvas(ctx, userID, canvasID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrCanvasNotFound
	}
	if err != nil {
		return nil, err
	}
	if c.DramaID == nil || *c.DramaID == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("只有短剧画布能使用 drama 模式")
	}
	if b.d.Drama == nil {
		return nil, errcode.ErrInvalidParams.WithMsg("短剧助手服务未启用")
	}
	p, err := b.d.Drama.Get(ctx, userID, *c.DramaID)
	if err != nil {
		return nil, err
	}
	if uint64(p.CanvasID) != canvasID {
		return nil, errcode.ErrDramaNotFound
	}
	return p, nil
}

// toolDramaRead 按需读取项目摘要、单集全文或片段列表。
func (b *AgentBridge) toolDramaRead(ctx context.Context, tc *toolCall) (*toolOut, error) {
	// 1. 项目由用户画布决定，不能通过参数跨用户、跨项目读取。
	p, err := b.dramaProject(ctx, tc.run.UserID, tc.run.CanvasID)
	if err != nil {
		return nil, err
	}
	var a struct {
		Scope     string `json:"scope"`
		EpisodeNo int    `json:"episode_no"`
	}
	if err := parseDramaArgs(tc.args, &a); err != nil {
		return nil, err
	}
	var value any
	switch a.Scope {
	case "overview":
		value = p
	case "episode", "segments":
		if a.EpisodeNo < 1 || a.EpisodeNo > 100 {
			return nil, fail("episode_no 必须是 1 到 100 的集号")
		}
		if a.Scope == "episode" {
			value, err = b.d.Drama.GetEpisode(ctx, tc.run.UserID, uint64(p.ID), a.EpisodeNo)
		} else if b.d.DramaSegments != nil {
			value, err = b.d.DramaSegments.List(ctx, tc.run.UserID, uint64(p.ID), a.EpisodeNo)
		} else {
			return nil, fail("短剧片段服务未启用")
		}
	default:
		return nil, fail("scope 必须是 overview、episode 或 segments")
	}
	if err != nil {
		return nil, err
	}
	content, err := jsonOut(value)
	return &toolOut{content: content, summary: "读取短剧 " + a.Scope}, err
}

// dramaSegment 只在本画布项目的分集中查找片段，防止同一用户跨项目修改。
func (b *AgentBridge) dramaSegment(ctx context.Context, userID uint64, p *model.DramaView, id uint64) (*model.DramaSegmentView, error) {
	if b.d.DramaSegments == nil {
		return nil, errcode.ErrInvalidParams.WithMsg("短剧片段服务未启用")
	}
	for _, ep := range p.Episodes {
		rows, err := b.d.DramaSegments.List(ctx, userID, uint64(p.ID), ep.No)
		if err != nil {
			return nil, err
		}
		for i := range rows {
			if uint64(rows[i].ID) == id {
				return &rows[i], nil
			}
		}
	}
	return nil, errcode.ErrDramaSegmentNotFound
}
