package agent

import (
	"context"
	"errors"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// defaultRunMaxSteps 是一轮运行最多的工具调用步数（运行准备时给 Agent 服务的默认值）。
const defaultRunMaxSteps = 40

// AgentRepo 是会话侧校验所需的数据访问。会话、运行、审批、事件的存储在 Agent 服务里，Go 只读画布。
type AgentRepo interface {
	// GetCanvas 按 id + user_id 读画布，不存在或不属于该用户返回 repository.ErrNotFound。
	GetCanvas(ctx context.Context, userID, id uint64) (*model.CanvasProject, error)
}

// AgentModels 提供已发布的 Agent 模型清单。
type AgentModels interface {
	// List 返回当前可用的 Agent 模型；没有任何已发布的模型时返回空。
	List(ctx context.Context) ([]model.AgentModelView, error)
}

// AgentService 是 Agent 服务向 Go 要的会话侧平台能力：模型清单、画布归属、消息里引用的校验、批准内容的解析和执行。
// 会话、运行、审批本身由 Agent 服务管理；改画布本身在 AgentCanvasService。
type AgentService struct {
	repo   AgentRepo
	canvas *AgentCanvasService
	models AgentModels
	exec   AgentGenerationExecutor
	// registry 用来校验消息里引用的生成模型；为 nil 时不校验模型引用
	registry provider.Registry
	// skills 用来校验消息里引用的技能；为 nil 时只认内置技能
	skills SkillLibrary
	now    func() time.Time
	// harness 是当前代码的 Agent 版本，运行准备时交给 Agent 服务记下，版本对比靠它
	harness Harness
}

// AgentDeps 是创建 AgentService 需要的依赖。
type AgentDeps struct {
	Repo      AgentRepo               // 数据访问
	Canvas    *AgentCanvasService     // 改画布
	Models    AgentModels             // Agent 模型清单
	Generator AgentGenerationExecutor // 批准生成后执行，为 nil 时只记录审批不执行
	Registry  provider.Registry       // 校验消息里引用的生成模型，为 nil 时不校验
	Skills    SkillLibrary            // 校验消息里引用的技能，为 nil 时只认内置技能
	Harness   Harness                 // 当前的 Agent 版本；零值表示不记版本
	Now       func() time.Time        // 为 nil 取系统时间，测试里注入
}

// NewAgentService 创建服务。
func NewAgentService(d AgentDeps) *AgentService {
	s := &AgentService{repo: d.Repo, canvas: d.Canvas, models: d.Models, exec: d.Generator, registry: d.Registry, skills: d.Skills, now: d.Now, harness: d.Harness}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Models 返回当前可用的 Agent 模型清单。
func (s *AgentService) Models(ctx context.Context) ([]model.AgentModelView, error) {
	list, err := s.models.List(ctx)
	if list == nil {
		list = []model.AgentModelView{}
	}
	return list, err
}

// checkCanvas 确认画布属于该用户。
func (s *AgentService) checkCanvas(ctx context.Context, userID, canvasID uint64) error {
	if _, err := s.repo.GetCanvas(ctx, userID, canvasID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errcode.ErrCanvasNotFound
		}
		return err
	}
	return nil
}

// runErr 把仓储的「不存在」翻译成运行不存在，其余原样返回。
func runErr(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrAgentRunMissing
	}
	return err
}

// skillLib 返回引用校验使用的技能库：没有配置时只含内置技能。
func (s *AgentService) skillLib() SkillLibrary {
	if s.skills != nil {
		return s.skills
	}
	return builtinLibrary{}
}
