package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/stepkit"
)

// 环节后台的运行参数。
const (
	// stepBindingTrialTTL 是「设为生效」依据的试跑的有效期：试跑必须在这段时间内通过，过期要重跑。
	stepBindingTrialTTL = 24 * time.Hour
	// stepTrialStale 是进行中试跑的卡死判定：创建后超过这段时间仍是 queued / running，不再挡住新试跑
	//（job 丢失或实例崩溃后没有人会把它收尾，不能让环节永远无法试跑）。
	stepTrialStale = 30 * time.Minute
	// stepTrialTimeout 是单次试跑（含重写一次）调用模型的最长等待。
	stepTrialTimeout = 10 * time.Minute
	// stepHistoryLimit 是详情里返回的绑定历史条数。
	stepHistoryLimit = 50
)

// StepRepo 是环节后台依赖的数据访问，由 repository.StepRepository 实现。
type StepRepo interface {
	// WithTx 执行绑定、试跑和积分的原子变更。
	WithTx(ctx context.Context, fn func(repository.StepTx) error) error
	// GetBinding 读取环节绑定，没有记录返回 repository.ErrNotFound（等于内置）。
	GetBinding(ctx context.Context, stepKey string) (*repository.StepBindingRow, error)
	// ListBindings 返回全部环节的绑定。
	ListBindings(ctx context.Context) ([]repository.StepBindingRow, error)
	// Trial 读取试跑，不存在返回 repository.ErrNotFound。
	Trial(ctx context.Context, id uint64, lock bool) (*model.StepTrial, error)
	// LastTrials 返回每个环节最近一次已结束的试跑，按 step_key 索引。
	LastTrials(ctx context.Context) (map[string]model.StepTrial, error)
	// History 返回环节的绑定历史，新的在前。
	History(ctx context.Context, stepKey string, limit int) ([]repository.StepHistoryRow, error)
	// StartTrial 把 queued 的试跑置为 running；已结束返回 repository.ErrStateConflict，不存在返回 repository.ErrNotFound。
	StartTrial(ctx context.Context, id uint64) (*model.StepTrial, error)
}

// StepCreditPolicy 提供新积分账户的初始积分（与生成任务一致）。
type StepCreditPolicy interface {
	// InitialCredits 返回后台当前生效的新账户初始积分。
	InitialCredits(ctx context.Context) (int, error)
}

// StepStats 提供环节的运行统计。营销以后接 marketing_step_runs；没有实现时失败率按 0 返回。
type StepStats interface {
	// FailRate7d 返回环节近 7 天的失败率（0 到 1），没有运行记录返回 0。
	FailRate7d(ctx context.Context, stepKey string) (float64, error)
}

// StepAdminDeps 是环节后台服务的依赖。
type StepAdminDeps struct {
	Registry *stepkit.Registry    // 环节注册表（各业务包在启动时登记）
	Repo     StepRepo             // 绑定、历史、试跑
	Skills   stepkit.SkillLibrary // 导入技能库
	Models   provider.Registry    // 文本模型清单与快照
	Credits  StepCreditPolicy     // 新账户初始积分
	Audit    AdminAuditWriter     // 管理员审计；为空不写
	Stats    StepStats            // 失败率统计；为空按 0
	Caller   stepkit.Caller       // 调用文本模型
	Now      func() time.Time     // 时钟，默认 time.Now；测试注入
}

// StepAdminService 是环节协议层的后台业务：环节列表与详情、试跑、设为生效；
// 同时给业务包提供取当前生效方法的 Resolver，以及 River 试跑任务的执行入口 HandleStepTrial。
type StepAdminService struct {
	reg    *stepkit.Registry
	repo   StepRepo
	skills stepkit.SkillLibrary
	models provider.Registry
	credit StepCreditPolicy
	audit  AdminAuditWriter
	stats  StepStats
	runner *stepkit.Runner
	now    func() time.Time

	fallbackSeen sync.Map // 已经审计过的「环节 + 原因」，同一进程内每种兜底只审计一次
}

// NewStepAdminService 创建环节后台服务。
func NewStepAdminService(d StepAdminDeps) *StepAdminService {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &StepAdminService{reg: d.Registry, repo: d.Repo, skills: d.Skills, models: d.Models, credit: d.Credits,
		audit: d.Audit, stats: d.Stats, runner: stepkit.NewRunner(d.Caller), now: now}
}

// skillIndex 是一次请求内的技能快照：按名字和 id 索引，避免逐个环节查库。
type skillIndex struct {
	byName map[string]stepkit.SkillInfo
	byID   map[uint64]stepkit.SkillInfo
}

// loadSkills 读取全部导入技能。
func (s *StepAdminService) loadSkills(ctx context.Context) (*skillIndex, error) {
	list, err := s.skills.ListSkills(ctx)
	if err != nil {
		return nil, err
	}
	ix := &skillIndex{byName: make(map[string]stepkit.SkillInfo, len(list)), byID: make(map[uint64]stepkit.SkillInfo, len(list))}
	for _, sk := range list {
		ix.byName[sk.Name] = sk
		ix.byID[sk.ID] = sk
	}
	return ix, nil
}

// usable 判断技能版本能不能用：技能存在、已启用且版本就绪。语义与 stepkit.ResolveMethod 的兜底条件一致。
func (ix *skillIndex) usable(name string, version int) (stepkit.SkillInfo, string) {
	info, ok := ix.byName[name]
	switch {
	case name == "" || !ok:
		return info, "绑定的技能已被删除"
	case !info.Enabled:
		return info, "技能 " + name + " 已停用"
	}
	for _, v := range info.Versions {
		if v == version {
			return info, ""
		}
	}
	return info, "技能 " + name + " 的 v" + itoa(version) + " 已被删除"
}

// methodView 把绑定展示成接口里的方法。
func (ix *skillIndex) methodView(b stepkit.Binding) model.StepMethodView {
	if b.Source != stepkit.SourceSkill {
		return model.StepMethodView{Source: stepkit.SourceBuiltin}
	}
	title := ""
	if info, ok := ix.byName[b.SkillName]; ok {
		title = info.Title
	}
	return model.StepMethodView{Source: stepkit.SourceSkill, SkillName: b.SkillName, SkillTitle: title, Version: b.SkillVersion}
}

// effective 返回绑定实际生效的方法：技能不可用时退回内置并标明原因。
func (ix *skillIndex) effective(b stepkit.Binding) model.StepMethodView {
	if b.Source != stepkit.SourceSkill {
		return model.StepMethodView{Source: stepkit.SourceBuiltin}
	}
	if _, reason := ix.usable(b.SkillName, b.SkillVersion); reason != "" {
		return model.StepMethodView{Source: stepkit.SourceBuiltin, Fallback: true, FallbackReason: reason}
	}
	return ix.methodView(b)
}

// toBinding 把仓储行转成 stepkit 的绑定；没有记录（nil）等于内置。
func toBinding(stepKey string, row *repository.StepBindingRow) stepkit.Binding {
	if row == nil || row.Source != stepkit.SourceSkill {
		return stepkit.Binding{StepKey: stepKey, Source: stepkit.SourceBuiltin}
	}
	b := stepkit.Binding{StepKey: stepKey, Source: stepkit.SourceSkill, SkillName: row.SkillName}
	if row.SkillID != nil {
		b.SkillID = *row.SkillID
	}
	if row.SkillVersion != nil {
		b.SkillVersion = *row.SkillVersion
	}
	if row.TrialID != nil {
		b.TrialID = *row.TrialID
	}
	return b
}

// List 返回全部环节的列表行：当前绑定、实际生效方法、有无新版本、最近试跑和近 7 天失败率。
func (s *StepAdminService) List(ctx context.Context) ([]model.StepSummary, error) {
	// 1. 一次性取齐绑定、最近试跑和技能，逐个环节只在内存里组装。
	rows, err := s.repo.ListBindings(ctx)
	if err != nil {
		return nil, err
	}
	bound := make(map[string]repository.StepBindingRow, len(rows))
	for _, r := range rows {
		bound[r.StepKey] = r
	}
	lasts, err := s.repo.LastTrials(ctx)
	if err != nil {
		return nil, err
	}
	ix, err := s.loadSkills(ctx)
	if err != nil {
		return nil, err
	}
	// 2. 按注册顺序输出；空列表序列化为 []。
	steps := s.reg.List()
	out := make([]model.StepSummary, 0, len(steps))
	for _, st := range steps {
		var row *repository.StepBindingRow
		if r, ok := bound[st.Key]; ok {
			row = &r
		}
		var last *model.StepTrial
		if t, ok := lasts[st.Key]; ok {
			last = &t
		}
		sum, err := s.summary(ctx, st, toBinding(st.Key, row), ix, last)
		if err != nil {
			return nil, err
		}
		out = append(out, sum)
	}
	return out, nil
}

// summary 组装一个环节的列表行。
func (s *StepAdminService) summary(ctx context.Context, st stepkit.Step, b stepkit.Binding, ix *skillIndex, last *model.StepTrial) (model.StepSummary, error) {
	sum := model.StepSummary{Key: st.Key, Title: st.Title, Studio: st.Studio, Media: string(st.Media),
		Binding: ix.methodView(b), Effective: ix.effective(b)}
	// 绑定技能当前生效版本比钉死的版本新：给出角标，试跑通过后才能切。
	if b.Source == stepkit.SourceSkill {
		if info, ok := ix.byName[b.SkillName]; ok && info.ActiveVersion > b.SkillVersion {
			v := info.ActiveVersion
			sum.NewerVersion = &v
		}
	}
	if last != nil {
		sum.LastTrial = &model.StepLastTrial{ID: encodeID(last.ID), Passed: last.Status == model.StepTrialSucceeded && last.Passed, CreatedAt: last.CreatedAt}
	}
	if s.stats != nil {
		rate, err := s.stats.FailRate7d(ctx, st.Key)
		if err != nil {
			return sum, err
		}
		sum.FailRate7d = rate
	}
	return sum, nil
}

// Get 返回环节详情：摘要加协议、样例、绑定历史和候选方法。环节不存在返回 68012。
func (s *StepAdminService) Get(ctx context.Context, key string) (*model.StepDetail, error) {
	// 1. 环节必须已注册。
	st, ok := s.reg.Get(key)
	if !ok {
		return nil, errcode.ErrStepNotFound
	}
	row, err := s.repo.GetBinding(ctx, key)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if errors.Is(err, repository.ErrNotFound) {
		row = nil
	}
	ix, err := s.loadSkills(ctx)
	if err != nil {
		return nil, err
	}
	// 2. 摘要用与列表相同的口径，最近试跑单独取一次。
	lasts, err := s.repo.LastTrials(ctx)
	if err != nil {
		return nil, err
	}
	var last *model.StepTrial
	if t, ok := lasts[key]; ok {
		last = &t
	}
	sum, err := s.summary(ctx, st, toBinding(key, row), ix, last)
	if err != nil {
		return nil, err
	}
	// 3. 协议、样例、历史、候选。
	history, err := s.historyViews(ctx, key, ix)
	if err != nil {
		return nil, err
	}
	d := &model.StepDetail{StepSummary: sum, Duty: st.Duty, Schema: string(st.Schema),
		Rules:      model.StepRulesView{Hard: nonNil(st.Rules.Hard), Soft: nonNil(st.Rules.Soft)},
		Inputs:     make([]model.StepInputVar, 0, len(st.Inputs)),
		Samples:    make([]model.StepSampleView, 0, len(st.Samples)),
		History:    history,
		Candidates: candidateViews(ix)}
	for _, v := range st.Inputs {
		d.Inputs = append(d.Inputs, model.StepInputVar{Name: v.Name, Type: v.Type, Desc: v.Desc})
	}
	for _, sm := range st.Samples {
		d.Samples = append(d.Samples, model.StepSampleView{Key: sm.Key, Name: sm.Name, NeedsRef: sm.NeedsRef})
	}
	return d, nil
}

// candidateViews 返回候选方法：内置方法加全部导入技能（停用的也列出，enabled=false，前端置灰）。
func candidateViews(ix *skillIndex) []model.StepCandidateView {
	out := []model.StepCandidateView{{Source: stepkit.SourceBuiltin}}
	for _, info := range sortedSkills(ix) {
		scripts, chars, enabled := info.HasScripts, info.Chars, info.Enabled
		out = append(out, model.StepCandidateView{Source: stepkit.SourceSkill, SkillName: info.Name, SkillTitle: info.Title,
			Versions: nonNilInts(info.Versions), ActiveVersion: info.ActiveVersion, HasScripts: &scripts, Chars: &chars, Enabled: &enabled})
	}
	return out
}

// historyViews 把绑定历史转成接口视图。
func (s *StepAdminService) historyViews(ctx context.Context, key string, ix *skillIndex) ([]model.StepHistoryView, error) {
	rows, err := s.repo.History(ctx, key, stepHistoryLimit)
	if err != nil {
		return nil, err
	}
	out := make([]model.StepHistoryView, 0, len(rows))
	for _, r := range rows {
		v := model.StepHistoryView{ID: encodeID(r.ID), From: refView(r.FromBinding, ix), To: refView(r.ToBinding, ix), Actor: r.ActorName, CreatedAt: r.CreatedAt}
		if r.TrialID != nil {
			id := encodeID(*r.TrialID)
			v.TrialID = &id
		}
		out = append(out, v)
	}
	return out, nil
}
