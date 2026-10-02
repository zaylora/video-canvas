// Package aiconfigfake 提供 AI 配置服务测试用的最小内存依赖。
package aiconfigfake

import (
	"context"
	"sort"
	"sync"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// MemRepo 是 AIConfigRepo、AIChannelReader 和 AIPluginReader 的内存实现。
type MemRepo struct {
	mu       sync.Mutex
	nextID   uint64
	Revs     []*model.AIConfigRevision
	Models   map[string]*model.AIModel
	Secrets  map[string]*model.AISecret
	Channels map[string]*model.AIChannel
	Plugins  map[string]*model.AIPlugin
	Versions map[uint64]*model.AIPluginVersion

	LoadPublishedCalls int
	FailLoad           error

	// 以下用来在测试里模拟插件 / 渠道 / 审计仓储的各种情形。
	Audits         []model.AIAuditLog
	AuditErr       error            // 非空时 InsertAudit 返回它
	SaveVersionErr error            // 非空时 SaveVersion 返回它
	ActiveTaskRefs map[uint64]int64 // 版本 id → 快照引用它的非终态任务数
	DeleteInUse    bool             // 为 true 时 DeleteVersion / DeleteChannel / DeletePlugin / DeleteModel 一律返回 ErrInUse（模拟并发下被新引用）

	ChannelTaskRefs map[string]int64 // 渠道 key → 快照引用它的非终态任务数
}

func NewMemRepo() *MemRepo {
	return &MemRepo{
		Models:   map[string]*model.AIModel{},
		Secrets:  map[string]*model.AISecret{},
		Channels: map[string]*model.AIChannel{},
		Plugins:  map[string]*model.AIPlugin{},
		Versions: map[uint64]*model.AIPluginVersion{},

		ActiveTaskRefs: map[uint64]int64{},

		ChannelTaskRefs: map[string]int64{},
	}
}

func (m *MemRepo) find(id uint64) *model.AIConfigRevision {
	for _, rev := range m.Revs {
		if rev.ID == id {
			return rev
		}
	}
	return nil
}

func (m *MemRepo) SaveDraft(_ context.Context, in repository.SaveDraftInput) (*model.AIConfigRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	row, ok := m.Models[in.Pointer.Key]
	if !ok {
		sortNo := in.InitialSort
		if sortNo == 0 {
			sortNo = 100
		}
		row = &model.AIModel{Key: in.Pointer.Key, Enabled: in.InitialEnabled, Sort: sortNo}
		m.Models[in.Pointer.Key] = row
	}
	row.Kind = in.Pointer.Kind

	maxNo := 0
	for _, rev := range m.Revs {
		if rev.Target != model.ConfigTargetModel || rev.TargetKey != in.Pointer.Key {
			continue
		}
		if rev.RevisionNo > maxNo {
			maxNo = rev.RevisionNo
		}
		if rev.Status == model.RevisionDraft {
			rev.Status = model.RevisionArchived
		}
	}
	m.nextID++
	rev := &model.AIConfigRevision{
		ID: m.nextID, Target: model.ConfigTargetModel, TargetKey: in.Pointer.Key,
		RevisionNo: maxNo + 1, BodyJSON: model.JSONText(append([]byte(nil), in.Body...)),
		Status: model.RevisionDraft, CreatedBy: in.CreatedBy, Note: in.Note, CreatedAt: time.Now(),
	}
	m.Revs = append(m.Revs, rev)
	copyRev := *rev
	return &copyRev, nil
}

func (m *MemRepo) GetDraft(_ context.Context, target, key string) (*model.AIConfigRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best *model.AIConfigRevision
	for _, rev := range m.Revs {
		if rev.Target == target && rev.TargetKey == key && rev.Status == model.RevisionDraft &&
			(best == nil || rev.RevisionNo > best.RevisionNo) {
			best = rev
		}
	}
	if best == nil {
		return nil, repository.ErrNotFound
	}
	copyRev := *best
	return &copyRev, nil
}

func (m *MemRepo) GetRevision(_ context.Context, id uint64) (*model.AIConfigRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rev := m.find(id)
	if rev == nil {
		return nil, repository.ErrNotFound
	}
	copyRev := *rev
	return &copyRev, nil
}

func (m *MemRepo) GetPublishedRevision(_ context.Context, target, key string) (*model.AIConfigRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if target != model.ConfigTargetModel {
		return nil, repository.ErrNotFound
	}
	row, ok := m.Models[key]
	if !ok || row.PublishedRevisionID == nil {
		return nil, repository.ErrNotFound
	}
	rev := m.find(*row.PublishedRevisionID)
	if rev == nil {
		return nil, repository.ErrNotFound
	}
	copyRev := *rev
	return &copyRev, nil
}

func (m *MemRepo) ListRevisions(_ context.Context, target, key string, limit int) ([]model.AIConfigRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.AIConfigRevision, 0)
	for _, rev := range m.Revs {
		if rev.Target != target || rev.TargetKey != key {
			continue
		}
		copyRev := *rev
		copyRev.BodyJSON = nil
		out = append(out, copyRev)
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
	out := make([]model.AIConfigRevision, 0)
	for _, rev := range m.Revs {
		if rev.Target != target || (rev.Status != model.RevisionDraft && rev.Status != model.RevisionPublished) {
			continue
		}
		copyRev := *rev
		if !withBody {
			copyRev.BodyJSON = nil
		}
		out = append(out, copyRev)
	}
	return out, nil
}

func (m *MemRepo) PublishDraft(_ context.Context, ptr repository.ConfigPointer, id uint64) (*model.AIConfigRevision, error) {
	return m.switchPublished(ptr, id, model.RevisionDraft)
}

func (m *MemRepo) Rollback(_ context.Context, ptr repository.ConfigPointer, id uint64) (*model.AIConfigRevision, error) {
	return m.switchPublished(ptr, id, model.RevisionArchived)
}

func (m *MemRepo) switchPublished(ptr repository.ConfigPointer, id uint64, want string) (*model.AIConfigRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.Models[ptr.Key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	rev := m.find(id)
	if rev == nil || rev.Target != model.ConfigTargetModel || rev.TargetKey != ptr.Key {
		return nil, repository.ErrNotFound
	}
	if rev.Status != want {
		return nil, repository.ErrRevisionConflict
	}
	if row.PublishedRevisionID != nil && *row.PublishedRevisionID != rev.ID {
		if old := m.find(*row.PublishedRevisionID); old != nil {
			old.Status = model.RevisionArchived
		}
	}
	rev.Status = model.RevisionPublished
	rid := rev.ID
	row.PublishedRevisionID = &rid
	row.Kind = ptr.Kind
	copyRev := *rev
	return &copyRev, nil
}

func (m *MemRepo) GetModelPointer(_ context.Context, key string) (*model.AIModel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.Models[key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	copyRow := *row
	return &copyRow, nil
}

func (m *MemRepo) ListModelPointers(_ context.Context) ([]model.AIModel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.AIModel, 0, len(m.Models))
	for _, row := range m.Models {
		out = append(out, *row)
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
	row, ok := m.Models[key]
	if !ok {
		return repository.ErrNotFound
	}
	row.Enabled = enabled
	return nil
}

func (m *MemRepo) SetModelSort(_ context.Context, key string, sortNo int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.Models[key]
	if !ok {
		return repository.ErrNotFound
	}
	row.Sort = sortNo
	return nil
}

func (m *MemRepo) LoadPublishedModels(_ context.Context) ([]repository.PublishedModel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LoadPublishedCalls++
	if m.FailLoad != nil {
		return nil, m.FailLoad
	}
	out := make([]repository.PublishedModel, 0)
	for _, row := range m.Models {
		if row.PublishedRevisionID == nil {
			continue
		}
		rev := m.find(*row.PublishedRevisionID)
		if rev == nil {
			continue
		}
		out = append(out, repository.PublishedModel{
			Key: row.Key, Kind: row.Kind, Enabled: row.Enabled, Sort: row.Sort,
			RevisionID: rev.ID, RevisionNo: rev.RevisionNo, Body: rev.BodyJSON,
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

func (m *MemRepo) UpsertSecret(_ context.Context, secret *model.AISecret) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copySecret := *secret
	copySecret.Ciphertext = append([]byte(nil), secret.Ciphertext...)
	copySecret.Nonce = append([]byte(nil), secret.Nonce...)
	m.Secrets[secret.Name] = &copySecret
	return nil
}

func (m *MemRepo) GetSecret(_ context.Context, name string) (*model.AISecret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	secret, ok := m.Secrets[name]
	if !ok {
		return nil, repository.ErrNotFound
	}
	copySecret := *secret
	return &copySecret, nil
}

func (m *MemRepo) ListSecrets(_ context.Context) ([]model.AISecret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.AISecret, 0, len(m.Secrets))
	for _, secret := range m.Secrets {
		out = append(out, model.AISecret{Name: secret.Name, KeyVersion: secret.KeyVersion, UpdatedBy: secret.UpdatedBy, UpdatedAt: secret.UpdatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *MemRepo) GetChannel(_ context.Context, key string) (*model.AIChannel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	channel, ok := m.Channels[key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	copyChannel := *channel
	return &copyChannel, nil
}

func (m *MemRepo) ListChannels(_ context.Context) ([]model.AIChannel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.AIChannel, 0, len(m.Channels))
	for _, channel := range m.Channels {
		out = append(out, *channel)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (m *MemRepo) GetPlugin(_ context.Context, key string) (*model.AIPlugin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	plugin, ok := m.Plugins[key]
	if !ok {
		return nil, repository.ErrNotFound
	}
	copyPlugin := *plugin
	return &copyPlugin, nil
}

func (m *MemRepo) ListVersions(_ context.Context, pluginKey string) ([]model.AIPluginVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.AIPluginVersion, 0, len(m.Versions))
	for _, version := range m.Versions {
		if pluginKey == "" || version.PluginKey == pluginKey {
			out = append(out, *version)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (m *MemRepo) GetVersionHead(_ context.Context, id uint64) (*model.AIPluginVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	version, ok := m.Versions[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	copyVersion := *version
	copyVersion.Code = ""
	return &copyVersion, nil
}

// DryRunner 记录 dry-run 调用。
type DryRunner struct {
	Result any
	Err    error
	Snap   *provider.Snapshot
	Input  map[string]any
}

func (d *DryRunner) DryRun(_ context.Context, snap *provider.Snapshot, input map[string]any) (any, error) {
	d.Snap, d.Input = snap, input
	return d.Result, d.Err
}

// TestTasks 记录试跑任务调用。
type TestTasks struct {
	View   *model.GenerationTaskView
	Err    error
	UserID uint64
	Snap   *provider.Snapshot
	Input  map[string]any
	Views  map[uint64]TestView
	Trace  []provider.TraceStep // GetTestTrace 返回的追踪；为 nil 时表示任务还没有追踪
}

type TestView struct {
	UserID uint64
	View   *model.GenerationTaskView
}

func (t *TestTasks) SubmitTest(_ context.Context, userID uint64, snap *provider.Snapshot, input map[string]any) (*model.GenerationTaskView, error) {
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
	view, ok := t.Views[taskID]
	if !ok || view.UserID != userID {
		return nil, repository.ErrNotFound
	}
	return view.View, nil
}

// GetTestTrace 按 GetTestTask 同样的归属规则返回预设的追踪（Trace 字段），别人的任务与不存在的任务返回 ErrNotFound。
func (t *TestTasks) GetTestTrace(_ context.Context, userID, taskID uint64) ([]provider.TraceStep, error) {
	view, ok := t.Views[taskID]
	if !ok || view.UserID != userID {
		return nil, repository.ErrNotFound
	}
	return t.Trace, nil
}

// Invalidator 记录 Registry 失效广播。
type Invalidator struct {
	mu      sync.Mutex
	Reasons []string
}

func (i *Invalidator) Invalidate(_ context.Context, reason string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.Reasons = append(i.Reasons, reason)
}
