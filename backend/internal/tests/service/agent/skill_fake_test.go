package agent_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"sort"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service/agent"
	"video-canvas/internal/storage"
)

// memStore 是内存里的对象存储。
type memStore struct {
	mu   sync.Mutex
	objs map[string][]byte
	fail bool // 为真时 Put 失败
}

func newMemStore() *memStore { return &memStore{objs: map[string][]byte{}} }

func (m *memStore) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return io.ErrClosedPipe
	}
	b, _ := io.ReadAll(r)
	m.objs[key] = b
	return nil
}

func (m *memStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objs[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memStore) URL(context.Context, string, time.Duration) (string, error) { return "", nil }

func (m *memStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objs, key)
	return nil
}

func (m *memStore) keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.objs))
	for k := range m.objs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type memStores struct{ s *memStore }

func (m memStores) Default(context.Context) (*storage.Handle, error) {
	return &storage.Handle{ID: 1, Storage: m.s}, nil
}

func (m memStores) Get(_ context.Context, id uint64) (*storage.Handle, error) {
	if id != 1 {
		return nil, storage.ErrStorageNotFound
	}
	return &storage.Handle{ID: 1, Storage: m.s}, nil
}

// memSkillRepo 是 SkillRepo 的内存实现，语义对齐 repository.AgentSkillRepository。
type memSkillRepo struct {
	mu       sync.Mutex
	nextID   uint64
	skills   map[uint64]*model.AgentSkill
	versions map[uint64]*model.AgentSkillVersion
	imports  map[string]*model.AgentSkillImport
	now      func() time.Time
}

func newMemSkillRepo(now func() time.Time) *memSkillRepo {
	return &memSkillRepo{skills: map[uint64]*model.AgentSkill{}, versions: map[uint64]*model.AgentSkillVersion{}, imports: map[string]*model.AgentSkillImport{}, now: now}
}

func (r *memSkillRepo) id() uint64 { r.nextID++; return r.nextID }

func (r *memSkillRepo) byName(name string) *model.AgentSkill {
	for _, s := range r.skills {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func (r *memSkillRepo) ready(skillID uint64) []*model.AgentSkillVersion {
	var out []*model.AgentSkillVersion
	for _, v := range r.versions {
		if v.SkillID == skillID && v.State == model.SkillVersionReady {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

func (r *memSkillRepo) ListSkills(context.Context) ([]model.AgentSkill, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []model.AgentSkill
	for _, s := range r.skills {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *memSkillRepo) GetSkill(_ context.Context, name string) (*model.AgentSkill, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.byName(name)
	if s == nil {
		return nil, repository.ErrNotFound
	}
	c := *s
	return &c, nil
}

func (r *memSkillRepo) ListVersions(_ context.Context, skillID uint64) ([]model.AgentSkillVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rs := r.ready(skillID)
	out := make([]model.AgentSkillVersion, 0, len(rs))
	for i := len(rs) - 1; i >= 0; i-- {
		v := *rs[i]
		v.BodyText, v.FilesJSON, v.FrontmatterJSON = "", nil, nil
		out = append(out, v)
	}
	return out, nil
}

func (r *memSkillRepo) ListActiveVersions(context.Context) (map[uint64]model.AgentSkillVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[uint64]model.AgentSkillVersion{}
	for id, s := range r.skills {
		for _, v := range r.ready(id) {
			if s.ActiveVersion != nil && v.Version == *s.ActiveVersion {
				out[id] = *v
			}
		}
	}
	return out, nil
}

func (r *memSkillRepo) ListVersionNumbers(context.Context) (map[uint64][]int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[uint64][]int{}
	for id := range r.skills {
		for _, v := range r.ready(id) {
			out[id] = append(out[id], v.Version)
		}
	}
	return out, nil
}

func (r *memSkillRepo) GetVersion(_ context.Context, skillID uint64, version int) (*model.AgentSkillVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.ready(skillID) {
		if v.Version == version {
			c := *v
			return &c, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *memSkillRepo) GetVersionByID(_ context.Context, id uint64) (*model.AgentSkillVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v := r.versions[id]; v != nil && v.State == model.SkillVersionReady {
		c := *v
		return &c, nil
	}
	return nil, repository.ErrNotFound
}

func (r *memSkillRepo) FindVersionBySHA(_ context.Context, skillID uint64, sha string) (*model.AgentSkillVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.ready(skillID) {
		if v.SHA256 == sha {
			c := *v
			return &c, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *memSkillRepo) ReserveVersion(_ context.Context, in repository.ReserveSkillInput) (*model.AgentSkill, *model.AgentSkillVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.byName(in.Name)
	if s == nil {
		s = &model.AgentSkill{ID: r.id(), Name: in.Name, Title: in.Title, CreatedBy: in.CreatedBy}
		r.skills[s.ID] = s
	}
	s.LatestVersion++
	v := in.Version
	v.ID, v.SkillID, v.Version, v.State, v.UpdatedAt = r.id(), s.ID, s.LatestVersion, model.SkillVersionPending, r.now()
	r.versions[v.ID] = &v
	sc, vc := *s, v
	return &sc, &vc, nil
}

func (r *memSkillRepo) PromoteVersion(_ context.Context, versionID uint64) (*model.AgentSkill, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.versions[versionID]
	if v == nil || v.State != model.SkillVersionPending {
		return nil, repository.ErrNotFound
	}
	v.State = model.SkillVersionReady
	s := r.skills[v.SkillID]
	if s.ActiveVersion == nil {
		n := v.Version
		s.ActiveVersion = &n
	}
	c := *s
	return &c, nil
}

func (r *memSkillRepo) DiscardVersion(_ context.Context, versionID uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v := r.versions[versionID]; v != nil && v.State == model.SkillVersionPending {
		delete(r.versions, versionID)
	}
	return nil
}

func (r *memSkillRepo) update(skillID uint64, fn func(*model.AgentSkill)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.skills[skillID]
	if s == nil {
		return repository.ErrNotFound
	}
	fn(s)
	return nil
}

func (r *memSkillRepo) SetEnabled(_ context.Context, id uint64, e bool) error {
	return r.update(id, func(s *model.AgentSkill) { s.Enabled = e })
}

func (r *memSkillRepo) SetActiveVersion(_ context.Context, id uint64, v int) error {
	return r.update(id, func(s *model.AgentSkill) { s.ActiveVersion = &v })
}

func (r *memSkillRepo) SetTitle(_ context.Context, id uint64, t string) error {
	return r.update(id, func(s *model.AgentSkill) { s.Title = t })
}

func (r *memSkillRepo) MarkVersionDeleting(_ context.Context, skillID uint64, version int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.skills[skillID]
	if s == nil {
		return repository.ErrNotFound
	}
	if s.ActiveVersion != nil && *s.ActiveVersion == version {
		return repository.ErrInUse
	}
	for _, v := range r.ready(skillID) {
		if v.Version == version {
			v.State = model.SkillVersionDeleting
			return nil
		}
	}
	return repository.ErrNotFound
}

func (r *memSkillRepo) DeleteSkill(_ context.Context, skillID uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.skills[skillID]
	if s == nil {
		return repository.ErrNotFound
	}
	if s.Enabled {
		return repository.ErrInUse
	}
	for _, v := range r.versions {
		if v.SkillID == skillID {
			v.State = model.SkillVersionDeleting
		}
	}
	delete(r.skills, skillID)
	return nil
}

func (r *memSkillRepo) ListEnabled(context.Context) ([]repository.EnabledSkill, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []repository.EnabledSkill
	for id, s := range r.skills {
		if !s.Enabled || s.ActiveVersion == nil {
			continue
		}
		for _, v := range r.ready(id) {
			if v.Version == *s.ActiveVersion {
				out = append(out, repository.EnabledSkill{Name: s.Name, Title: s.Title, Description: v.Description, VersionID: v.ID, Version: v.Version})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *memSkillRepo) CreateImport(_ context.Context, imp *model.AgentSkillImport) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *imp
	r.imports[imp.ID] = &c
	return nil
}

func (r *memSkillRepo) GetImport(_ context.Context, id string) (*model.AgentSkillImport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if imp := r.imports[id]; imp != nil {
		c := *imp
		return &c, nil
	}
	return nil, repository.ErrNotFound
}

func (r *memSkillRepo) DeleteImport(_ context.Context, id string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.imports[id]
	delete(r.imports, id)
	return ok, nil
}

func (r *memSkillRepo) ListExpiredImports(_ context.Context, now time.Time, limit int) ([]model.AgentSkillImport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []model.AgentSkillImport
	for _, imp := range r.imports {
		if imp.ExpiresAt.Before(now) && len(out) < limit {
			out = append(out, *imp)
		}
	}
	return out, nil
}

func (r *memSkillRepo) ListGarbageVersions(_ context.Context, pendingBefore time.Time, limit int) ([]model.AgentSkillVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []model.AgentSkillVersion
	for _, v := range r.versions {
		if (v.State == model.SkillVersionDeleting || (v.State == model.SkillVersionPending && v.UpdatedAt.Before(pendingBefore))) && len(out) < limit {
			out = append(out, *v)
		}
	}
	return out, nil
}

func (r *memSkillRepo) DeleteVersionRow(_ context.Context, id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.versions, id)
	return nil
}

// auditSink 记录审计动作。
type auditSink struct {
	mu      sync.Mutex
	actions []string
}

func (a *auditSink) Insert(_ context.Context, l *model.AdminAuditLog) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actions = append(a.actions, l.Action)
	return nil
}

// zipOf 在内存里打一个 zip。
func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(files[n]))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func skillMD(name, desc, body string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\n" + body + "\n"
}

type skillEnv struct {
	svc   *SkillService
	repo  *memSkillRepo
	store *memStore
	audit *auditSink
	now   *time.Time
}

func newSkillEnv(t *testing.T) *skillEnv {
	t.Helper()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	e := &skillEnv{store: newMemStore(), audit: &auditSink{}, now: &now}
	e.repo = newMemSkillRepo(func() time.Time { return now })
	seq := 0
	e.svc = NewSkillService(e.repo, memStores{e.store}, e.audit, SkillOptions{
		Now:   func() time.Time { return now },
		NewID: func() string { seq++; return "id-" + string(rune('a'+seq)) },
	})
	return e
}

// ReserveVersionForTest 直接预留一个 pending 版本，模拟「写对象中途崩溃」的现场。
func (r *memSkillRepo) ReserveVersionForTest(name string) (*model.AgentSkill, *model.AgentSkillVersion, error) {
	return r.ReserveVersion(context.Background(), repository.ReserveSkillInput{Name: name, Title: name, Version: model.AgentSkillVersion{PackageKey: "agent-skills/" + name + "/crashed.zip", StorageID: 1}})
}
