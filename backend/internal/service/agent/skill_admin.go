package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"video-canvas/internal/agent/skills"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// List 返回管理页列表：内置技能（只读，永远启用）加导入技能。
// q 在名字、显示名、说明里做不区分大小写的包含匹配；status 为 enabled / disabled 时按启停过滤，空取全部。
func (s *SkillService) List(ctx context.Context, q, status string) ([]SkillItem, error) {
	// 1. 导入技能：三次查询（技能、生效版本头、版本号）凑齐一页，避免逐个技能查
	list, err := s.repo.ListSkills(ctx)
	if err != nil {
		return nil, err
	}
	active, err := s.repo.ListActiveVersions(ctx)
	if err != nil {
		return nil, err
	}
	nums, err := s.repo.ListVersionNumbers(ctx)
	if err != nil {
		return nil, err
	}
	// 2. 内置在前、导入在后；内置技能没有版本，也不能被改动
	out := make([]SkillItem, 0, len(list)+8)
	for _, b := range skills.All() {
		out = append(out, builtinItem(b))
	}
	for i := range list {
		v := active[list[i].ID]
		out = append(out, importedItem(&list[i], &v, nums[list[i].ID]))
	}
	// 3. 过滤
	q = strings.ToLower(strings.TrimSpace(q))
	res := out[:0]
	for _, it := range out {
		if q != "" && !strings.Contains(strings.ToLower(it.Name+" "+it.Title+" "+it.Description), q) {
			continue
		}
		if (status == "enabled" && !it.Enabled) || (status == "disabled" && it.Enabled) {
			continue
		}
		res = append(res, it)
	}
	return res, nil
}

func builtinItem(b skills.Brief) SkillItem {
	return SkillItem{Name: b.Name, Title: b.Title, Description: b.Description, Source: SkillSourceBuiltin, Readonly: true, Enabled: true, UnsupportedFields: []string{}}
}

// importedItem 由技能行、生效版本头和全部 ready 版本号组装列表项；生效版本缺失（不应出现）时字段留零值。
func importedItem(sk *model.AgentSkill, active *model.AgentSkillVersion, versions []int) SkillItem {
	it := SkillItem{
		Name: sk.Name, Title: sk.Title, Source: SkillSourceImported, Enabled: sk.Enabled,
		ActiveVersion: sk.ActiveVersion, LatestVersion: sk.LatestVersion, VersionCount: len(versions),
		UnsupportedFields: []string{}, UpdatedAt: sk.UpdatedAt,
	}
	if sk.ActiveVersion != nil && len(versions) > 0 && versions[len(versions)-1] > *sk.ActiveVersion {
		it.PendingVersion = true
	}
	if active != nil && active.ID != 0 {
		it.Description, it.FileCount, it.TotalBytes, it.HasScripts = active.Description, active.FileCount, active.TotalBytes, active.HasScripts
		_ = json.Unmarshal(active.UnsupportedFields, &it.UnsupportedFields)
		if it.UnsupportedFields == nil {
			it.UnsupportedFields = []string{}
		}
	}
	return it
}

// itemFor 组装一个导入技能的列表项（写操作的返回值）。
func (s *SkillService) itemFor(ctx context.Context, sk *model.AgentSkill) (*SkillItem, error) {
	vs, err := s.repo.ListVersions(ctx, sk.ID)
	if err != nil {
		return nil, err
	}
	nums := make([]int, 0, len(vs))
	var active *model.AgentSkillVersion
	for i := range vs {
		nums = append(nums, vs[i].Version)
		if sk.ActiveVersion != nil && vs[i].Version == *sk.ActiveVersion {
			active = &vs[i]
		}
	}
	sort.Ints(nums)
	it := importedItem(sk, active, nums)
	return &it, nil
}

// Get 返回技能详情：列表项加版本列表。内置技能附正文，没有版本。技能不存在返回 61004。
func (s *SkillService) Get(ctx context.Context, name string) (*SkillDetail, error) {
	if b, ok := skills.Read(name); ok {
		it := builtinItem(skills.Brief{Name: b.Name, Title: b.Title, Description: b.Description})
		return &SkillDetail{SkillItem: it, Versions: []SkillVersionHead{}, Body: b.Body}, nil
	}
	sk, err := s.skill(ctx, name)
	if err != nil {
		return nil, err
	}
	vs, err := s.repo.ListVersions(ctx, sk.ID)
	if err != nil {
		return nil, err
	}
	d := &SkillDetail{Versions: make([]SkillVersionHead, 0, len(vs))}
	nums := make([]int, 0, len(vs))
	var active *model.AgentSkillVersion
	for i := range vs {
		d.Versions = append(d.Versions, versionHead(&vs[i], sk.ActiveVersion))
		nums = append(nums, vs[i].Version)
		if sk.ActiveVersion != nil && vs[i].Version == *sk.ActiveVersion {
			active = &vs[i]
		}
	}
	sort.Ints(nums)
	d.SkillItem = importedItem(sk, active, nums)
	return d, nil
}

func versionHead(v *model.AgentSkillVersion, active *int) SkillVersionHead {
	h := SkillVersionHead{
		Version: v.Version, Active: active != nil && *active == v.Version, SHA256: v.SHA256, Description: v.Description,
		FileCount: v.FileCount, TotalBytes: v.TotalBytes, HasScripts: v.HasScripts, UnsupportedFields: []string{},
		CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt,
	}
	_ = json.Unmarshal(v.UnsupportedFields, &h.UnsupportedFields)
	if h.UnsupportedFields == nil {
		h.UnsupportedFields = []string{}
	}
	return h
}

// skill 按名字取导入技能，不存在返回 61004；内置技能不在这里（它们只读）。
func (s *SkillService) skill(ctx context.Context, name string) (*model.AgentSkill, error) {
	sk, err := s.repo.GetSkill(ctx, name)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrSkillNotFound
	}
	return sk, err
}

// writable 取一个可修改的导入技能：内置技能返回 61007，不存在返回 61004。
func (s *SkillService) writable(ctx context.Context, name string) (*model.AgentSkill, error) {
	if _, ok := skills.Read(name); ok {
		return nil, errcode.ErrSkillBuiltin
	}
	return s.skill(ctx, name)
}

// Version 返回某个版本的完整信息（清单、问题、正文）。技能不存在 61004，版本不存在 61005。
func (s *SkillService) Version(ctx context.Context, name string, version int) (*SkillVersionView, error) {
	sk, v, err := s.versionOf(ctx, name, version)
	if err != nil {
		return nil, err
	}
	out := &SkillVersionView{SkillVersionHead: versionHead(v, sk.ActiveVersion), Name: sk.Name, Body: v.BodyText,
		Frontmatter: map[string]any{}, Files: []SkillFileView{}, Issues: []SkillIssue{}}
	_ = json.Unmarshal(v.FrontmatterJSON, &out.Frontmatter)
	_ = json.Unmarshal(v.FilesJSON, &out.Files)
	_ = json.Unmarshal(v.IssuesJSON, &out.Issues)
	return out, nil
}

// versionOf 取技能和它的一个 ready 版本。
func (s *SkillService) versionOf(ctx context.Context, name string, version int) (*model.AgentSkill, *model.AgentSkillVersion, error) {
	sk, err := s.skill(ctx, name)
	if err != nil {
		return nil, nil, err
	}
	v, err := s.repo.GetVersion(ctx, sk.ID, version)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil, errcode.ErrSkillVersionMissing
	}
	if err != nil {
		return nil, nil, err
	}
	return sk, v, nil
}

// packageOf 取一个版本的整包字节，带进程内缓存（版本不可变，缓存不会过期）。
func (s *SkillService) packageOf(ctx context.Context, v *model.AgentSkillVersion) ([]byte, error) {
	if b, ok := s.pkgs.get(v.ID); ok {
		return b, nil
	}
	b, err := s.readObject(ctx, v.StorageID, v.PackageKey, maxStagedZipBytes)
	if err != nil {
		return nil, err
	}
	s.pkgs.put(v.ID, b)
	return b, nil
}

// File 读某个版本包内的一个文件：文本最多 1MB，二进制只返回大小。路径不在清单里返回 10001。
func (s *SkillService) File(ctx context.Context, name string, version int, path string) (*SkillFileContent, error) {
	_, v, err := s.versionOf(ctx, name, version)
	if err != nil {
		return nil, err
	}
	var files []SkillFileView
	if err := json.Unmarshal(v.FilesJSON, &files); err != nil {
		return nil, fmt.Errorf("解析文件清单失败：%w", err)
	}
	// 先按清单判断路径和类型，二进制和不存在的路径都不必取整包
	for _, f := range files {
		if f.Path == path && !f.Text {
			return &SkillFileContent{Path: f.Path, Size: f.Size, Binary: true}, nil
		}
	}
	pkg, err := s.packageOf(ctx, v)
	if err != nil {
		return nil, err
	}
	return readPackageFile(pkg, files, path)
}

// Download 返回某个版本的规范化整包和建议的文件名。不暴露对象存储地址，管理员只能经这个接口取包。
func (s *SkillService) Download(ctx context.Context, name string, version int) ([]byte, string, error) {
	sk, v, err := s.versionOf(ctx, name, version)
	if err != nil {
		return nil, "", err
	}
	pkg, err := s.packageOf(ctx, v)
	if err != nil {
		return nil, "", err
	}
	return pkg, fmt.Sprintf("%s-v%d.zip", sk.Name, v.Version), nil
}

// SetEnabled 启用或停用技能。启用要求有就绪的生效版本，否则返回 61009；内置技能 61007。
// 已是目标状态时不写审计也不报错（幂等）。
func (s *SkillService) SetEnabled(ctx context.Context, actorID uint64, name string, enabled bool) (*SkillItem, error) {
	sk, err := s.writable(ctx, name)
	if err != nil {
		return nil, err
	}
	if sk.Enabled == enabled {
		return s.itemFor(ctx, sk)
	}
	// 1. 启用前确认生效版本存在且就绪：停用状态下不会被删（生效版本不能删），但仍以库为准
	if enabled {
		if sk.ActiveVersion == nil {
			return nil, errcode.ErrSkillCannotEnable.WithMsg("没有生效版本，请先把一个版本设为生效")
		}
		if _, err := s.repo.GetVersion(ctx, sk.ID, *sk.ActiveVersion); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return nil, errcode.ErrSkillCannotEnable.WithMsg("生效版本不可用，请重新设置生效版本")
			}
			return nil, err
		}
	}
	// 2. 写入；技能在此期间被删返回 61004
	if err := s.repo.SetEnabled(ctx, sk.ID, enabled); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrSkillNotFound
		}
		return nil, err
	}
	sk.Enabled = enabled
	s.invalidateCatalog()
	action := model.AdminAuditSkillDisable
	if enabled {
		action = model.AdminAuditSkillEnable
	}
	s.auditSkill(ctx, actorID, action, sk.ID, map[string]any{"name": sk.Name})
	return s.itemFor(ctx, sk)
}

// SetActiveVersion 把某个版本设为生效；回滚就是把旧版本设为生效，不新建版本。
// 技能已启用时，之后新开始的 Agent 运行会用新版本，已在运行的按版本固定，不受影响。
func (s *SkillService) SetActiveVersion(ctx context.Context, actorID uint64, name string, version int) (*SkillItem, error) {
	sk, err := s.writable(ctx, name)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.GetVersion(ctx, sk.ID, version); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrSkillVersionMissing
		}
		return nil, err
	}
	from := sk.ActiveVersion
	if err := s.repo.SetActiveVersion(ctx, sk.ID, version); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrSkillNotFound
		}
		return nil, err
	}
	sk.ActiveVersion = &version
	s.invalidateCatalog()
	detail := map[string]any{"name": sk.Name, "version": version}
	if from != nil {
		detail["from"] = *from
	}
	s.auditSkill(ctx, actorID, model.AdminAuditSkillActiveVersion, sk.ID, detail)
	return s.itemFor(ctx, sk)
}

// Rename 改显示名（1~128 个字符）；不产生版本，技能名 name 不可变。
func (s *SkillService) Rename(ctx context.Context, actorID uint64, name, title string) (*SkillItem, error) {
	title = clipRunes(title, 1<<20)
	if title == "" || len([]rune(title)) > 128 {
		return nil, errcode.ErrInvalidParams.WithMsg("显示名需要 1 到 128 个字符")
	}
	sk, err := s.writable(ctx, name)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetTitle(ctx, sk.ID, title); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrSkillNotFound
		}
		return nil, err
	}
	from := sk.Title
	sk.Title = title
	s.invalidateCatalog()
	s.auditSkill(ctx, actorID, model.AdminAuditSkillRename, sk.ID, map[string]any{"name": sk.Name, "from": from, "to": title})
	return s.itemFor(ctx, sk)
}

// DeleteCheck 删除技能前的预检：必须先停用；返回版本数，界面据此写「将同时删除对象存储里的整包」。
func (s *SkillService) DeleteCheck(ctx context.Context, name string) (*SkillDeleteCheck, error) {
	sk, err := s.writable(ctx, name)
	if err != nil {
		return nil, err
	}
	vs, err := s.repo.ListVersions(ctx, sk.ID)
	if err != nil {
		return nil, err
	}
	out := &SkillDeleteCheck{CanDelete: !sk.Enabled, VersionCount: len(vs)}
	if sk.Enabled {
		out.Reason = "技能正在启用，请先停用"
	}
	return out, nil
}

// DeleteVersion 删除一个版本：不能删生效版本（61008）。行先标为 deleting，对象由清理任务删除。
func (s *SkillService) DeleteVersion(ctx context.Context, actorID uint64, name string, version int) error {
	sk, err := s.writable(ctx, name)
	if err != nil {
		return err
	}
	if sk.ActiveVersion != nil && *sk.ActiveVersion == version {
		return errcode.ErrSkillActiveVersion
	}
	if err := s.repo.MarkVersionDeleting(ctx, sk.ID, version); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return errcode.ErrSkillVersionMissing
		case errors.Is(err, repository.ErrInUse):
			return errcode.ErrSkillActiveVersion
		}
		return err
	}
	s.auditSkill(ctx, actorID, model.AdminAuditSkillDeleteVersion, sk.ID, map[string]any{"name": sk.Name, "version": version})
	s.Sweep(ctx)
	return nil
}

// DeleteSkill 删除技能：必须先停用（61010）；全部版本标为 deleting，对象随即由清理任务删除。
func (s *SkillService) DeleteSkill(ctx context.Context, actorID uint64, name string) error {
	sk, err := s.writable(ctx, name)
	if err != nil {
		return err
	}
	if sk.Enabled {
		return errcode.ErrSkillNeedDisable
	}
	if err := s.repo.DeleteSkill(ctx, sk.ID); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return errcode.ErrSkillNotFound
		case errors.Is(err, repository.ErrInUse):
			return errcode.ErrSkillNeedDisable
		}
		return err
	}
	s.invalidateCatalog()
	s.auditSkill(ctx, actorID, model.AdminAuditSkillDelete, sk.ID, map[string]any{"name": sk.Name})
	s.Sweep(ctx)
	return nil
}
