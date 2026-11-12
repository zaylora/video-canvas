// Package stepfake 提供环节协议层后台（service.StepAdminService）的内存 fake，供 service 与 handler 测试共用：
// 仓储（绑定、历史、试跑、积分流水）、技能库、模型清单、模型调用。
// 它不依赖 service 包（否则循环依赖），只按 service 里声明的接口结构实现，语义与真实 repository 保持一致：
// 积分流水 (step_trial_id, type) 唯一、StartTrial 只放行未结束的试跑、进行中判断带卡死时间窗。
package stepfake

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
	"video-canvas/internal/stepkit"
)

// ErrInjected 是测试注入的数据库故障。
var ErrInjected = errors.New("注入的数据库故障")

// Repo 是环节仓储的内存实现，同时实现 service.StepRepo 与 repository.StepTx。
type Repo struct {
	mu         sync.Mutex
	Bindings   map[string]*model.StepBinding // 当前绑定，按 step_key
	SkillNames map[uint64]string             // 技能 id → 名字（模拟 LEFT JOIN agent_skills；缺失表示技能已被删）
	Trials     map[uint64]*model.StepTrial
	Histories  []model.StepBindingHistory
	Actors     map[uint64]string // 用户 id → 用户名
	Credits    map[uint64]*model.UserCredit
	Ledger     []model.CreditLedger
	Enqueued   []uint64 // 已入队的试跑 id，按入队顺序
	NextID     uint64
	Err        error // 非空时 GetBinding 之外的读写都返回它（用来验证错误透传）
}

// NewRepo 创建空仓储。
func NewRepo() *Repo {
	return &Repo{Bindings: map[string]*model.StepBinding{}, SkillNames: map[uint64]string{}, Trials: map[uint64]*model.StepTrial{},
		Actors: map[uint64]string{}, Credits: map[uint64]*model.UserCredit{}}
}

func (r *Repo) id() uint64 {
	r.NextID++
	return r.NextID
}

// WithTx 直接在 fake 上执行：内存 fake 不模拟回滚，每个方法自己加锁。
func (r *Repo) WithTx(_ context.Context, fn func(repository.StepTx) error) error { return fn(r) }

// GetBinding 实现读绑定：没有记录返回 repository.ErrNotFound。
func (r *Repo) GetBinding(_ context.Context, key string) (*repository.StepBindingRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.Bindings[key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return r.row(b), nil
}

func (r *Repo) row(b *model.StepBinding) *repository.StepBindingRow {
	row := &repository.StepBindingRow{StepBinding: *b}
	if b.SkillID != nil {
		row.SkillName = r.SkillNames[*b.SkillID]
	}
	return row
}

// ListBindings 返回全部绑定。
func (r *Repo) ListBindings(context.Context) ([]repository.StepBindingRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []repository.StepBindingRow
	for _, b := range r.Bindings {
		out = append(out, *r.row(b))
	}
	return out, nil
}

// Trial 读取试跑，返回副本。
func (r *Repo) Trial(_ context.Context, id uint64, _ bool) (*model.StepTrial, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.Trials[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

// LastTrials 返回每个环节最近一次已结束的试跑。
func (r *Repo) LastTrials(context.Context) (map[string]model.StepTrial, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]model.StepTrial{}
	for _, t := range r.Trials {
		if t.Status != model.StepTrialSucceeded && t.Status != model.StepTrialFailed {
			continue
		}
		if cur, ok := out[t.StepKey]; !ok || t.CreatedAt.After(cur.CreatedAt) || (t.CreatedAt.Equal(cur.CreatedAt) && t.ID > cur.ID) {
			out[t.StepKey] = *t
		}
	}
	return out, nil
}

// History 返回环节的绑定历史，新的在前。
func (r *Repo) History(_ context.Context, key string, limit int) ([]repository.StepHistoryRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []repository.StepHistoryRow
	for _, h := range r.Histories {
		if h.StepKey == key {
			out = append(out, repository.StepHistoryRow{StepBindingHistory: h, ActorName: r.Actors[h.ActorID]})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// StartTrial 把 queued / running 的试跑置为 running。
func (r *Repo) StartTrial(_ context.Context, id uint64) (*model.StepTrial, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.Trials[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	if t.Status != model.StepTrialQueued && t.Status != model.StepTrialRunning {
		return nil, repository.ErrStateConflict
	}
	t.Status = model.StepTrialRunning
	cp := *t
	return &cp, nil
}

// LockStep 实现 StepTx：fake 不需要锁。
func (r *Repo) LockStep(context.Context, string) error { return nil }

// ActiveTrials 统计 since 之后创建且仍在进行的试跑。
func (r *Repo) ActiveTrials(_ context.Context, key string, since time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for _, t := range r.Trials {
		if t.StepKey == key && (t.Status == model.StepTrialQueued || t.Status == model.StepTrialRunning) && t.CreatedAt.After(since) {
			n++
		}
	}
	return n, nil
}

// InsertTrial 插入试跑并回填主键。
func (r *Repo) InsertTrial(_ context.Context, t *model.StepTrial) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	t.ID = r.id()
	cp := *t
	r.Trials[t.ID] = &cp
	return nil
}

// UpdateTrial 按 id 更新可变列，只支持服务实际会写的那些列。
func (r *Repo) UpdateTrial(_ context.Context, id uint64, fields map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.Trials[id]
	if !ok {
		return repository.ErrNotFound
	}
	for k, v := range fields {
		applyTrialField(t, k, v)
	}
	return nil
}

func applyTrialField(t *model.StepTrial, k string, v any) {
	switch k {
	case "compare_of":
		id := v.(uint64)
		t.CompareOf = &id
	case "status":
		t.Status = v.(string)
	case "passed":
		t.Passed = v.(bool)
	case "errors":
		t.Errors = v.([]byte)
	case "flags":
		t.Flags = v.([]byte)
	case "output":
		t.Output = v.([]byte)
	case "method":
		t.Method = v.([]byte)
	case "error":
		t.Error = v.(string)
	case "attempts":
		t.Attempts = v.(int)
	case "duration_ms":
		t.DurationMS = v.(int)
	case "input_tokens":
		t.InputTokens = v.(int)
	case "output_tokens":
		t.OutputTokens = v.(int)
	case "credits":
		t.Credits = v.(int)
	case "finished_at":
		ts := v.(time.Time)
		t.FinishedAt = &ts
	}
}

// EnqueueTrial 记录入队的试跑 id。
func (r *Repo) EnqueueTrial(_ context.Context, id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Enqueued = append(r.Enqueued, id)
	return nil
}

// SaveBinding 写入绑定（整行覆盖）。
func (r *Repo) SaveBinding(_ context.Context, b *model.StepBinding) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *b
	r.Bindings[b.StepKey] = &cp
	return nil
}

// InsertHistory 追加绑定历史。
func (r *Repo) InsertHistory(_ context.Context, h *model.StepBindingHistory) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	h.ID = r.id()
	h.CreatedAt = time.Now()
	r.Histories = append(r.Histories, *h)
	return nil
}

// EnsureCredit 惰性建立积分账户。
func (r *Repo) EnsureCredit(_ context.Context, userID uint64, initial int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Credits[userID]; !ok {
		r.Credits[userID] = &model.UserCredit{UserID: userID, Balance: initial}
	}
	return nil
}

// LockCredit 返回账户副本。
func (r *Repo) LockCredit(_ context.Context, userID uint64) (*model.UserCredit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.Credits[userID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *c
	return &cp, nil
}

// AddCredit 增减余额与冻结额。
func (r *Repo) AddCredit(_ context.Context, userID uint64, balance, frozen int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.Credits[userID]
	if !ok {
		return repository.ErrNotFound
	}
	c.Balance += balance
	c.Frozen += frozen
	return nil
}

// InsertLedger 幂等插入流水：同一试跑同一类型已存在时返回 false。
func (r *Repo) InsertLedger(_ context.Context, e *model.CreditLedger) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.StepTrialID != nil {
		for _, l := range r.Ledger {
			if l.StepTrialID != nil && *l.StepTrialID == *e.StepTrialID && l.Type == e.Type {
				return false, nil
			}
		}
	}
	r.Ledger = append(r.Ledger, *e)
	return true, nil
}

// LedgerOf 返回某次试跑的全部流水。
func (r *Repo) LedgerOf(trialID uint64) []model.CreditLedger {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []model.CreditLedger
	for _, l := range r.Ledger {
		if l.StepTrialID != nil && *l.StepTrialID == trialID {
			out = append(out, l)
		}
	}
	return out
}

// Library 是内存技能库，实现 stepkit.SkillLibrary。
type Library struct {
	mu     sync.Mutex
	Skills map[string]*Skill
	Err    error // 非空时 LoadMethod / FindSkill / ListSkills 都返回它
}

// Skill 是库里的一个技能。
type Skill struct {
	ID         uint64
	Title      string
	Enabled    bool
	Active     int            // 生效版本
	Versions   map[int]string // 版本号 → 正文
	HasScripts bool
}

// NewLibrary 创建空技能库。
func NewLibrary() *Library { return &Library{Skills: map[string]*Skill{}} }

// Add 新增或覆盖一个技能。
func (l *Library) Add(name string, s *Skill) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Skills[name] = s
}

// LoadMethod 实现 stepkit.SkillLibrary。
func (l *Library) LoadMethod(_ context.Context, name string, version, limit int) (*stepkit.SkillText, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.Err != nil {
		return nil, l.Err
	}
	sk, ok := l.Skills[name]
	if !ok || !sk.Enabled {
		return nil, stepkit.ErrSkillUnavailable
	}
	body, ok := sk.Versions[version]
	if !ok {
		return nil, stepkit.ErrSkillUnavailable
	}
	if r := []rune(body); limit > 0 && len(r) > limit {
		body = string(r[:limit])
	}
	return &stepkit.SkillText{Name: name, Title: sk.Title, Version: version, Body: body, HasScripts: sk.HasScripts}, nil
}

func (l *Library) info(name string, sk *Skill) stepkit.SkillInfo {
	vs := make([]int, 0, len(sk.Versions))
	for v := range sk.Versions {
		vs = append(vs, v)
	}
	sort.Ints(vs)
	return stepkit.SkillInfo{ID: sk.ID, Name: name, Title: sk.Title, Enabled: sk.Enabled, ActiveVersion: sk.Active, Versions: vs,
		HasScripts: sk.HasScripts, Chars: len([]rune(sk.Versions[sk.Active]))}
}

// FindSkill 实现 stepkit.SkillLibrary。
func (l *Library) FindSkill(_ context.Context, name string) (*stepkit.SkillInfo, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.Err != nil {
		return nil, l.Err
	}
	sk, ok := l.Skills[name]
	if !ok {
		return nil, stepkit.ErrSkillUnavailable
	}
	info := l.info(name, sk)
	return &info, nil
}

// ListSkills 实现 stepkit.SkillLibrary。
func (l *Library) ListSkills(context.Context) ([]stepkit.SkillInfo, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.Err != nil {
		return nil, l.Err
	}
	var out []stepkit.SkillInfo
	for name, sk := range l.Skills {
		out = append(out, l.info(name, sk))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Models 是内存模型清单，实现 provider.Registry。
type Models struct {
	Snapshots map[string]*provider.Snapshot
}

// NewModels 创建清单，默认带一个 key 为 text-a 的 agent 模型（按次计费 2 积分，能看图不能看视频）。
func NewModels() *Models {
	return &Models{Snapshots: map[string]*provider.Snapshot{"text-a": TextModel("text-a", true, false, 2)}}
}

// TextModel 构造一个按次计费的 agent 模型快照。
func TextModel(key string, vision, video bool, unit int) *provider.Snapshot {
	return &provider.Snapshot{Model: provider.ModelSnapshot{
		Key: key, Kind: modelcfg.KindAgent, Capabilities: modelcfg.Capabilities{Vision: vision, Video: video},
		Pricing: modelcfg.Pricing{Billing: modelcfg.BillingPerCall, Unit: unit},
	}}
}

// ListModels 返回全部模型信息。
func (m *Models) ListModels(context.Context, string) ([]provider.ModelInfo, error) {
	var out []provider.ModelInfo
	for k, s := range m.Snapshots {
		out = append(out, provider.ModelInfo{Key: k, Kind: s.Model.Kind})
	}
	return out, nil
}

// Snapshot 返回模型快照，不存在返回 provider.ErrModelUnavailable。
func (m *Models) Snapshot(_ context.Context, key string) (*provider.Snapshot, error) {
	s, ok := m.Snapshots[key]
	if !ok {
		return nil, provider.ErrModelUnavailable
	}
	return s, nil
}

// Caller 是脚本化的模型调用，实现 stepkit.Caller：按顺序返回 Replies，用完后返回错误。
type Caller struct {
	mu      sync.Mutex
	Replies []Reply
	Calls   int
}

// Reply 是一次预设回复。
type Reply struct {
	Text  string
	Usage llmgateway.Usage
	Err   error
}

// Complete 实现 stepkit.Caller。
func (c *Caller) Complete(context.Context, *provider.Snapshot, []llmgateway.Message) (*llmgateway.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Calls++
	if c.Calls > len(c.Replies) {
		return nil, errors.New("没有更多预设回复")
	}
	r := c.Replies[c.Calls-1]
	if r.Err != nil {
		return nil, r.Err
	}
	return &llmgateway.Result{Text: r.Text, Usage: r.Usage}, nil
}

// Policy 是固定的初始积分，实现 service.StepCreditPolicy。
type Policy struct{ Initial int }

// InitialCredits 返回固定的初始积分。
func (p Policy) InitialCredits(context.Context) (int, error) { return p.Initial, nil }

// Audit 是内存审计，实现 service.AdminAuditWriter。
type Audit struct {
	mu   sync.Mutex
	Logs []model.AdminAuditLog
}

// Insert 记录一条审计。
func (a *Audit) Insert(_ context.Context, l *model.AdminAuditLog) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Logs = append(a.Logs, *l)
	return nil
}

// RecentForUser 不用，返回空。
func (a *Audit) RecentForUser(context.Context, uint64, int) ([]model.AdminAuditView, error) {
	return nil, nil
}

// Actions 返回已记录的审计动作。
func (a *Audit) Actions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.Logs))
	for _, l := range a.Logs {
		out = append(out, l.Action)
	}
	return out
}
