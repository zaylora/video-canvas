// Package aiconfigfake 提供 AI 配置服务的内存 fake（仓储、校验器、dry-run、试跑任务、HTTP 客户端工厂），
// 只给 service 与 handler 的单元测试共用，避免两边各写一份。它不依赖 service 包，因此不会产生循环引用。
package aiconfigfake

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/repository"
)

// MemRepo 是 AIConfigRepo 的内存实现，状态机语义与真实仓储保持一致（草稿归档、发布、回滚、指针行）。
type MemRepo struct {
	mu        sync.Mutex
	nextID    uint64
	Revs      []*model.AIConfigRevision
	Providers map[string]*model.AIProvider
	Models    map[string]*model.AIModel
	Secrets   map[string]*model.AISecret

	// LoadPublishedCalls 统计 LoadPublishedModels 被调用的次数，用来断言缓存是否生效。
	LoadPublishedCalls int
	// FailLoad 非空时 Load* 返回该错误，用来模拟数据库故障。
	FailLoad error
}

func NewMemRepo() *MemRepo {
	return &MemRepo{
		Providers: map[string]*model.AIProvider{},
		Models:    map[string]*model.AIModel{},
		Secrets:   map[string]*model.AISecret{},
	}
}

func (m *MemRepo) find(id uint64) *model.AIConfigRevision {
	for _, r := range m.Revs {
		if r.ID == id {
			return r
		}
	}
	return nil
}

func (m *MemRepo) SaveDraft(_ context.Context, in repository.SaveDraftInput) (*model.AIConfigRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := in.Pointer
	if p.Target == model.ConfigTargetProvider {
		if row, ok := m.Providers[p.Key]; ok {
			row.Name = p.Name
		} else {
			m.Providers[p.Key] = &model.AIProvider{Key: p.Key, Name: p.Name}
		}
	} else if row, ok := m.Models[p.Key]; ok {
		row.Kind, row.ProviderKey = p.Kind, p.ProviderKey
	} else {
		sort := in.InitialSort
		if sort == 0 {
			sort = 100
		}
		m.Models[p.Key] = &model.AIModel{Key: p.Key, Kind: p.Kind, ProviderKey: p.ProviderKey, Enabled: in.InitialEnabled, Sort: sort}
	}
	maxNo := 0
	for _, r := range m.Revs {
		if r.Target == p.Target && r.TargetKey == p.Key {
			if r.RevisionNo > maxNo {
				maxNo = r.RevisionNo
			}
			if r.Status == model.RevisionDraft {
				r.Status = model.RevisionArchived
			}
		}
	}
	m.nextID++
	rev := &model.AIConfigRevision{
		ID: m.nextID, Target: p.Target, TargetKey: p.Key, RevisionNo: maxNo + 1,
		BodyJSON: model.JSONText(append([]byte(nil), in.Body...)), Status: model.RevisionDraft,
		CreatedBy: in.CreatedBy, Note: in.Note, CreatedAt: time.Now(),
	}
	m.Revs = append(m.Revs, rev)
	cp := *rev
	return &cp, nil
}

func (m *MemRepo) GetDraft(_ context.Context, target, key string) (*model.AIConfigRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best *model.AIConfigRevision
	for _, r := range m.Revs {
		if r.Target == target && r.TargetKey == key && r.Status == model.RevisionDraft && (best == nil || r.RevisionNo > best.RevisionNo) {
			best = r
		}
	}
	if best == nil {
		return nil, repository.ErrNotFound
	}
	cp := *best
	return &cp, nil
}

func (m *MemRepo) GetRevision(_ context.Context, id uint64) (*model.AIConfigRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(id)
	if r == nil {
		return nil, repository.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (m *MemRepo) publishedID(target, key string) (*uint64, bool) {
	if target == model.ConfigTargetProvider {
		p, ok := m.Providers[key]
		if !ok {
			return nil, false
		}
		return p.PublishedRevisionID, true
	}
	mo, ok := m.Models[key]
	if !ok {
		return nil, false
	}
	return mo.PublishedRevisionID, true
}

func (m *MemRepo) GetPublishedRevision(_ context.Context, target, key string) (*model.AIConfigRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.publishedID(target, key)
	if !ok || id == nil {
		return nil, repository.ErrNotFound
	}
	cp := *m.find(*id)
	return &cp, nil
}

func (m *MemRepo) ListRevisions(_ context.Context, target, key string, limit int) ([]model.AIConfigRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.AIConfigRevision
	for _, r := range m.Revs {
		if r.Target == target && r.TargetKey == key {
			cp := *r
			cp.BodyJSON = nil
			out = append(out, cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RevisionNo > out[j].RevisionNo })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemRepo) ListRevisionHeads(_ context.Context, target string, withBody bool) ([]model.AIConfigRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.AIConfigRevision
	for _, r := range m.Revs {
		if r.Target == target && (r.Status == model.RevisionDraft || r.Status == model.RevisionPublished) {
			cp := *r
			if !withBody {
				cp.BodyJSON = nil
			}
			out = append(out, cp)
		}
	}
	return out, nil
}

func (m *MemRepo) switchPublished(ptr repository.ConfigPointer, id uint64, want string) (*model.AIConfigRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldID, ok := m.publishedID(ptr.Target, ptr.Key)
	if !ok {
		return nil, repository.ErrNotFound
	}
	rev := m.find(id)
	if rev == nil || rev.Target != ptr.Target || rev.TargetKey != ptr.Key {
		return nil, repository.ErrNotFound
	}
	if rev.Status != want {
		return nil, repository.ErrRevisionConflict
	}
	if oldID != nil && *oldID != rev.ID {
		if old := m.find(*oldID); old != nil && old.Status == model.RevisionPublished {
			old.Status = model.RevisionArchived
		}
	}
	rev.Status = model.RevisionPublished
	rid := rev.ID
	if ptr.Target == model.ConfigTargetProvider {
		p := m.Providers[ptr.Key]
		p.PublishedRevisionID = &rid
		if ptr.Name != "" {
			p.Name = ptr.Name
		}
	} else {
		mo := m.Models[ptr.Key]
		mo.PublishedRevisionID = &rid
		if ptr.Kind != "" {
			mo.Kind = ptr.Kind
		}
		if ptr.ProviderKey != "" {
			mo.ProviderKey = ptr.ProviderKey
		}
	}
	cp := *rev
	return &cp, nil
}

func (m *MemRepo) PublishDraft(_ context.Context, ptr repository.ConfigPointer, id uint64) (*model.AIConfigRevision, error) {
	return m.switchPublished(ptr, id, model.RevisionDraft)
}

func (m *MemRepo) Rollback(_ context.Context, ptr repository.ConfigPointer, id uint64) (*model.AIConfigRevision, error) {
	return m.switchPublished(ptr, id, model.RevisionArchived)
}

func (m *MemRepo) GetProviderPointer(_ context.Context, key string) (*model.AIProvider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.Providers[key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (m *MemRepo) ListProviderPointers(_ context.Context) ([]model.AIProvider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.AIProvider
	for _, p := range m.Providers {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (m *MemRepo) GetModelPointer(_ context.Context, key string) (*model.AIModel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mo, ok := m.Models[key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *mo
	return &cp, nil
}

func (m *MemRepo) ListModelPointers(_ context.Context) ([]model.AIModel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.AIModel
	for _, mo := range m.Models {
		out = append(out, *mo)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func (m *MemRepo) SetModelEnabled(_ context.Context, key string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mo, ok := m.Models[key]
	if !ok {
		return repository.ErrNotFound
	}
	mo.Enabled = enabled
	return nil
}

func (m *MemRepo) SetModelSort(_ context.Context, key string, sort int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mo, ok := m.Models[key]
	if !ok {
		return repository.ErrNotFound
	}
	mo.Sort = sort
	return nil
}

func (m *MemRepo) LoadPublishedProviders(_ context.Context) ([]repository.PublishedProvider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailLoad != nil {
		return nil, m.FailLoad
	}
	var out []repository.PublishedProvider
	for _, p := range m.Providers {
		if p.PublishedRevisionID == nil {
			continue
		}
		r := m.find(*p.PublishedRevisionID)
		out = append(out, repository.PublishedProvider{Key: p.Key, Name: p.Name, RevisionID: r.ID, RevisionNo: r.RevisionNo, Body: r.BodyJSON})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (m *MemRepo) LoadPublishedModels(_ context.Context) ([]repository.PublishedModel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LoadPublishedCalls++
	if m.FailLoad != nil {
		return nil, m.FailLoad
	}
	var out []repository.PublishedModel
	for _, mo := range m.Models {
		if mo.PublishedRevisionID == nil {
			continue
		}
		r := m.find(*mo.PublishedRevisionID)
		out = append(out, repository.PublishedModel{
			Key: mo.Key, Kind: mo.Kind, ProviderKey: mo.ProviderKey, Enabled: mo.Enabled, Sort: mo.Sort,
			RevisionID: r.ID, RevisionNo: r.RevisionNo, Body: r.BodyJSON,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func (m *MemRepo) UpsertSecret(_ context.Context, s *model.AISecret) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	cp.Ciphertext = append([]byte(nil), s.Ciphertext...)
	cp.Nonce = append([]byte(nil), s.Nonce...)
	m.Secrets[s.Name] = &cp
	return nil
}

func (m *MemRepo) GetSecret(_ context.Context, name string) (*model.AISecret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.Secrets[name]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *s
	return &cp, nil
}

// ListSecrets 与真实仓储一致：不返回密文和 nonce。
func (m *MemRepo) ListSecrets(_ context.Context) ([]model.AISecret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.AISecret
	for _, s := range m.Secrets {
		out = append(out, model.AISecret{Name: s.Name, KeyVersion: s.KeyVersion, UpdatedBy: s.UpdatedBy, UpdatedAt: s.UpdatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ---------------------------------------------------------------------------
// 校验器 fake
// ---------------------------------------------------------------------------

// Validator 是 ConfigValidator 的 fake：用 encoding/json 直接解析；正文里 "bad": true 时返回一条校验问题；
// 输入里 "invalid": true 时返回一条字段错误。
type Validator struct {
	Schema []byte
}

func (v *Validator) ParseProvider(body []byte) (*dsl.ProviderConfig, []dsl.Issue) {
	var cfg dsl.ProviderConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, []dsl.Issue{{Message: err.Error()}}
	}
	return &cfg, badIssues(body)
}

func (v *Validator) ParseModel(body []byte, provider *dsl.ProviderConfig) (*dsl.ModelConfig, []dsl.Issue) {
	var cfg dsl.ModelConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, []dsl.Issue{{Message: err.Error()}}
	}
	return &cfg, badIssues(body)
}

func (v *Validator) ValidateInput(_ dsl.InputSchema, input map[string]any) (map[string]any, []dsl.FieldError) {
	if b, _ := input["invalid"].(bool); b {
		return nil, []dsl.FieldError{{Field: "invalid", Message: "不合法"}}
	}
	out := map[string]any{}
	for k, val := range input {
		out[k] = val
	}
	out["_normalized"] = true
	return out, nil
}

func (v *Validator) JSONSchema(target string) ([]byte, error) {
	if v.Schema == nil {
		return []byte(`{"title":"` + target + `"}`), nil
	}
	return v.Schema, nil
}

func badIssues(body []byte) []dsl.Issue {
	var probe struct {
		Bad bool `json:"bad"`
	}
	if json.Unmarshal(body, &probe) == nil && probe.Bad {
		return []dsl.Issue{{Path: "bad", Message: "配置不合法"}}
	}
	return nil
}

// ---------------------------------------------------------------------------
// dry-run / 试跑 / HTTP 工厂 fake
// ---------------------------------------------------------------------------

// DryRunner 记录调用并返回预设结果。
type DryRunner struct {
	Result any
	Err    error

	Snap  *dsl.Snapshot
	Input map[string]any
}

func (d *DryRunner) DryRun(_ context.Context, snap *dsl.Snapshot, input map[string]any) (any, error) {
	d.Snap, d.Input = snap, input
	return d.Result, d.Err
}

// TestTasks 记录 SubmitTest 的调用，并按 id 返回试跑任务视图。
type TestTasks struct {
	View *model.GenerationTaskView
	Err  error

	UserID uint64
	Snap   *dsl.Snapshot
	Input  map[string]any
	// Views 是 GetTestTask 能查到的任务：id -> {所属用户, 视图}。
	Views map[uint64]TestView
}

// TestView 是 TestTasks 里保存的一个试跑任务。
type TestView struct {
	UserID uint64
	View   *model.GenerationTaskView
}

func (t *TestTasks) SubmitTest(_ context.Context, userID uint64, snap *dsl.Snapshot, input map[string]any) (*model.GenerationTaskView, error) {
	t.UserID, t.Snap, t.Input = userID, snap, input
	if t.Err != nil {
		return nil, t.Err
	}
	if t.View == nil {
		return &model.GenerationTaskView{ID: 1, Status: model.TaskPending}, nil
	}
	return t.View, nil
}

func (t *TestTasks) GetTestTask(_ context.Context, userID, taskID uint64) (*model.GenerationTaskView, error) {
	v, ok := t.Views[taskID]
	if !ok || v.UserID != userID {
		return nil, repository.ErrNotFound
	}
	return v.View, nil
}

// HTTPFactory 返回固定的 http.Client（通常是 httptest.Server.Client()），并记录收到的 allowed_hosts。
type HTTPFactory struct {
	Client       *http.Client
	AllowedHosts []string
	Timeout      time.Duration
}

func (f *HTTPFactory) NewClient(allowedHosts []string, timeout time.Duration) *http.Client {
	f.AllowedHosts, f.Timeout = allowedHosts, timeout
	if f.Client == nil {
		return http.DefaultClient
	}
	return f.Client
}

// Invalidator 记录失效广播。
type Invalidator struct {
	mu      sync.Mutex
	Reasons []string
}

func (i *Invalidator) Invalidate(_ context.Context, reason string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.Reasons = append(i.Reasons, reason)
}
