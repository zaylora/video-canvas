package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"video-canvas/internal/agent/skills"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// maxSkillSearchResults 是 skill_search 一次最多返回的技能数，与内置库的上限一致。
const maxSkillSearchResults = skills.MaxResults

// SkillLibrary 是 Agent 运行时看到的技能库：内置技能加后台已启用的导入技能。
// 由 SkillService 实现；没有配置时桥退回只含内置技能的实现。
type SkillLibrary interface {
	// Enabled 返回目录：全部已启用技能的名字、显示名、说明。
	Enabled(ctx context.Context) ([]SkillBrief, error)
	// Search 按关键词搜索，最多 5 个；空关键词返回前 5 个。
	Search(ctx context.Context, query string) ([]SkillBrief, error)
	// Has 判断技能是否存在且已启用（引用校验用）。
	Has(ctx context.Context, name string) (bool, error)
	// ReadSkill 读技能正文和资源清单。pinned 是这次运行已固定的版本 id（0 表示还没固定）；
	// 返回的 VersionID 由调用方记下，之后读同一技能的文件都传它。技能不存在或未启用返回 61004。
	ReadSkill(ctx context.Context, name string, pinned uint64) (*SkillContent, error)
	// ReadSkillFile 读技能包内的一个文件。路径不在清单里返回 10001（Msg 里含清单）。
	ReadSkillFile(ctx context.Context, name, file string, pinned uint64) (*SkillFileContent, *SkillContent, error)
}

// Enabled 返回目录：内置技能在前，已启用的导入技能在后，各自按名字排序。
// 结果缓存 CatalogTTL（默认 30 秒）；本实例的写操作会立刻失效，多实例部署时其他实例最多晚一个 TTL。
func (s *SkillService) Enabled(ctx context.Context) ([]SkillBrief, error) {
	s.cacheMu.Lock()
	if s.catalog != nil && s.opt.Now().Sub(s.catalogAt) < s.opt.CatalogTTL {
		out := append([]SkillBrief(nil), s.catalog...)
		s.cacheMu.Unlock()
		return out, nil
	}
	s.cacheMu.Unlock()

	enabled, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SkillBrief, 0, 8+len(enabled))
	for _, b := range skills.All() {
		out = append(out, SkillBrief{Name: b.Name, Title: b.Title, Description: b.Description, Source: SkillSourceBuiltin})
	}
	for _, e := range enabled {
		out = append(out, SkillBrief{Name: e.Name, Title: e.Title, Description: e.Description, Source: SkillSourceImported})
	}
	s.cacheMu.Lock()
	s.catalog, s.catalogAt = out, s.opt.Now()
	s.cacheMu.Unlock()
	return append([]SkillBrief(nil), out...), nil
}

// invalidateCatalog 让目录缓存立刻失效：管理员启停、切版本、改名、删除之后调用。
func (s *SkillService) invalidateCatalog() {
	s.cacheMu.Lock()
	s.catalog, s.catalogAt = nil, time.Time{}
	s.cacheMu.Unlock()
}

// Search 把查询按空白拆成词，在名字、显示名、说明（内置技能还有标签）里做不区分大小写的包含匹配，
// 命中词越多越靠前，同分保持目录顺序，最多返回 5 个。
func (s *SkillService) Search(ctx context.Context, query string) ([]SkillBrief, error) {
	all, err := s.Enabled(ctx)
	if err != nil {
		return nil, err
	}
	words := strings.Fields(strings.ToLower(query))
	type hit struct {
		b     SkillBrief
		score int
	}
	var hits []hit
	for _, b := range all {
		hay := strings.ToLower(b.Name + " " + b.Title + " " + b.Description + " " + strings.Join(skills.Tags(b.Name), " "))
		score := 0
		for _, w := range words {
			if strings.Contains(hay, w) {
				score++
			}
		}
		if len(words) == 0 || score > 0 {
			hits = append(hits, hit{b, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := make([]SkillBrief, 0, maxSkillSearchResults)
	for _, h := range hits {
		if len(out) == maxSkillSearchResults {
			break
		}
		out = append(out, h.b)
	}
	return out, nil
}

// Has 判断技能是否存在且已启用：内置技能永远存在，导入技能看目录。
func (s *SkillService) Has(ctx context.Context, name string) (bool, error) {
	all, err := s.Enabled(ctx)
	if err != nil {
		return false, err
	}
	for _, b := range all {
		if b.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// resolved 是一次读取解析出的技能：内置（ver 为 nil）或导入技能的某个版本。
type resolved struct {
	content *SkillContent
	pkgID   uint64 // 导入技能的版本 id，读包时用
}

// resolve 找到要读的技能版本。内置技能优先；导入技能必须已启用；
// 运行里已固定的版本优先，固定的版本不在了（被删）退回当前生效版本。
func (s *SkillService) resolve(ctx context.Context, name string, pinned uint64) (*resolved, error) {
	name = strings.TrimSpace(name)
	if b, ok := skills.Read(name); ok {
		return &resolved{content: &SkillContent{Name: b.Name, Source: SkillSourceBuiltin, Body: b.Body, Files: []SkillFileView{}}}, nil
	}
	sk, err := s.repo.GetSkill(ctx, name)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && !sk.Enabled) {
		return nil, errcode.ErrSkillNotFound
	}
	if err != nil {
		return nil, err
	}
	var v *skillVersionRef
	if pinned != 0 {
		if pv, err := s.repo.GetVersionByID(ctx, pinned); err == nil && pv.SkillID == sk.ID {
			v = &skillVersionRef{id: pv.ID, version: pv.Version, body: pv.BodyText, files: pv.FilesJSON, hasScripts: pv.HasScripts}
		} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return nil, err
		}
	}
	if v == nil {
		if sk.ActiveVersion == nil {
			return nil, errcode.ErrSkillNotFound
		}
		av, err := s.repo.GetVersion(ctx, sk.ID, *sk.ActiveVersion)
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrSkillNotFound
		}
		if err != nil {
			return nil, err
		}
		v = &skillVersionRef{id: av.ID, version: av.Version, body: av.BodyText, files: av.FilesJSON, hasScripts: av.HasScripts}
	}
	files := []SkillFileView{}
	_ = json.Unmarshal(v.files, &files)
	return &resolved{pkgID: v.id, content: &SkillContent{Name: sk.Name, Source: SkillSourceImported, VersionID: v.id, Version: v.version, Body: v.body, Files: files, HasScripts: v.hasScripts}}, nil
}

type skillVersionRef struct {
	id         uint64
	version    int
	body       string
	files      []byte
	hasScripts bool
}

// ReadSkill 读技能正文和资源清单，见 SkillLibrary.ReadSkill。
func (s *SkillService) ReadSkill(ctx context.Context, name string, pinned uint64) (*SkillContent, error) {
	r, err := s.resolve(ctx, name, pinned)
	if err != nil {
		return nil, err
	}
	return r.content, nil
}

// ReadSkillFile 读技能包内的一个文件，见 SkillLibrary.ReadSkillFile。
func (s *SkillService) ReadSkillFile(ctx context.Context, name, file string, pinned uint64) (*SkillFileContent, *SkillContent, error) {
	r, err := s.resolve(ctx, name, pinned)
	if err != nil {
		return nil, nil, err
	}
	if r.content.Source == SkillSourceBuiltin {
		return nil, r.content, errcode.ErrInvalidParams.WithMsg("内置技能只有正文，没有资源文件")
	}
	// 二进制和清单外的路径不必取整包
	for _, f := range r.content.Files {
		if f.Path == file && !f.Text {
			return &SkillFileContent{Path: f.Path, Size: f.Size, Binary: true}, r.content, nil
		}
	}
	if !hasFile(r.content.Files, file) {
		return nil, r.content, errcode.ErrInvalidParams.WithMsg("包里没有这个文件：" + clipRunes(file, 120))
	}
	v, err := s.repo.GetVersionByID(ctx, r.pkgID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, r.content, errcode.ErrSkillNotFound
		}
		return nil, r.content, err
	}
	pkg, err := s.packageOf(ctx, v)
	if err != nil {
		return nil, r.content, err
	}
	fc, err := readPackageFile(pkg, r.content.Files, file)
	return fc, r.content, err
}

func hasFile(files []SkillFileView, path string) bool {
	for _, f := range files {
		if f.Path == path {
			return true
		}
	}
	return false
}

// builtinLibrary 是只含内置技能的技能库：没有配置后台技能服务时桥用它，行为与后台技能管理上线前一致。
type builtinLibrary struct{}

func (builtinLibrary) Enabled(context.Context) ([]SkillBrief, error) {
	all := skills.All()
	out := make([]SkillBrief, len(all))
	for i, b := range all {
		out[i] = SkillBrief{Name: b.Name, Title: b.Title, Description: b.Description, Source: SkillSourceBuiltin}
	}
	return out, nil
}

func (builtinLibrary) Search(_ context.Context, query string) ([]SkillBrief, error) {
	found := skills.Search(query)
	out := make([]SkillBrief, len(found))
	for i, b := range found {
		out[i] = SkillBrief{Name: b.Name, Title: b.Title, Description: b.Description, Source: SkillSourceBuiltin}
	}
	return out, nil
}

func (builtinLibrary) Has(_ context.Context, name string) (bool, error) {
	_, ok := skills.Read(name)
	return ok, nil
}

func (builtinLibrary) ReadSkill(_ context.Context, name string, _ uint64) (*SkillContent, error) {
	s, ok := skills.Read(name)
	if !ok {
		return nil, errcode.ErrSkillNotFound
	}
	return &SkillContent{Name: s.Name, Source: SkillSourceBuiltin, Body: s.Body, Files: []SkillFileView{}}, nil
}

func (b builtinLibrary) ReadSkillFile(ctx context.Context, name, _ string, _ uint64) (*SkillFileContent, *SkillContent, error) {
	c, err := b.ReadSkill(ctx, name, 0)
	if err != nil {
		return nil, nil, err
	}
	return nil, c, errcode.ErrInvalidParams.WithMsg("内置技能只有正文，没有资源文件")
}
