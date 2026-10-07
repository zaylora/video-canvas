package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// AgentSkillRepository 是导入技能（agent_skills）、技能版本（agent_skill_versions）和导入暂存（agent_skill_imports）的数据访问。
// 版本一经登记不可修改，只能整行删除；任何读版本的查询都只看 ready，pending 与 deleting 对读路径不可见。
type AgentSkillRepository struct {
	db *gorm.DB
}

// NewAgentSkillRepository 创建技能仓储。
func NewAgentSkillRepository(db *gorm.DB) *AgentSkillRepository {
	return &AgentSkillRepository{db: db}
}

// versionHeadOmit 是列表类查询不读的大字段：正文与文件清单只在读单个版本时取。
var versionHeadOmit = []string{"body_text", "files_json", "frontmatter_json"}

// EnabledSkill 是一个已启用、且生效版本已就绪的技能，给 Agent 目录和 @ 弹层用。
type EnabledSkill struct {
	Name        string
	Title       string
	Description string
	VersionID   uint64
	Version     int
	HasScripts  bool
}

// ReserveSkillInput 是为一个新版本预留版本号所需的信息。
type ReserveSkillInput struct {
	Name      string
	Title     string // 技能首次出现时用作显示名；技能已存在则忽略
	CreatedBy uint64
	Version   model.AgentSkillVersion // 除 SkillID / Version / State 外的内容；PackageKey 由调用方先生成
}

// ListSkills 返回全部导入技能，按名字排序。
func (r *AgentSkillRepository) ListSkills(ctx context.Context) ([]model.AgentSkill, error) {
	var out []model.AgentSkill
	err := r.db.WithContext(ctx).Order("name").Find(&out).Error
	return out, err
}

// GetSkill 按名字取技能；不存在返回 ErrNotFound。
func (r *AgentSkillRepository) GetSkill(ctx context.Context, name string) (*model.AgentSkill, error) {
	var s model.AgentSkill
	if err := r.db.WithContext(ctx).Where("name = ?", name).Take(&s).Error; err != nil {
		return nil, translate(err)
	}
	return &s, nil
}

// ListVersions 返回技能的全部 ready 版本（不含正文、文件清单），版本号倒序。
func (r *AgentSkillRepository) ListVersions(ctx context.Context, skillID uint64) ([]model.AgentSkillVersion, error) {
	var out []model.AgentSkillVersion
	err := r.db.WithContext(ctx).Omit(versionHeadOmit...).
		Where("skill_id = ? AND state = ?", skillID, model.SkillVersionReady).Order("version DESC").Find(&out).Error
	return out, err
}

// GetVersion 取技能的某个 ready 版本（含正文与文件清单）；不存在返回 ErrNotFound。
func (r *AgentSkillRepository) GetVersion(ctx context.Context, skillID uint64, version int) (*model.AgentSkillVersion, error) {
	var v model.AgentSkillVersion
	err := r.db.WithContext(ctx).Where("skill_id = ? AND version = ? AND state = ?", skillID, version, model.SkillVersionReady).Take(&v).Error
	if err != nil {
		return nil, translate(err)
	}
	return &v, nil
}

// GetVersionByID 按版本 id 取 ready 版本（含正文与文件清单）；不存在返回 ErrNotFound。运行期的版本固定用它。
func (r *AgentSkillRepository) GetVersionByID(ctx context.Context, id uint64) (*model.AgentSkillVersion, error) {
	var v model.AgentSkillVersion
	if err := r.db.WithContext(ctx).Where("id = ? AND state = ?", id, model.SkillVersionReady).Take(&v).Error; err != nil {
		return nil, translate(err)
	}
	return &v, nil
}

// FindVersionBySHA 找技能下同内容的 ready 版本；没有返回 ErrNotFound。
func (r *AgentSkillRepository) FindVersionBySHA(ctx context.Context, skillID uint64, sha string) (*model.AgentSkillVersion, error) {
	var v model.AgentSkillVersion
	err := r.db.WithContext(ctx).Omit(versionHeadOmit...).
		Where("skill_id = ? AND sha256 = ? AND state = ?", skillID, sha, model.SkillVersionReady).Take(&v).Error
	if err != nil {
		return nil, translate(err)
	}
	return &v, nil
}

// ReserveVersion 在一个事务里为技能分配下一个版本号并写入 pending 版本行：
// 技能行不存在就创建（停用、无生效版本）；已存在则锁行后 latest_version+1。
// 返回技能与 pending 版本；调用方随后写对象，成功后 PromoteVersion，失败则 DiscardVersion。
func (r *AgentSkillRepository) ReserveVersion(ctx context.Context, in ReserveSkillInput) (*model.AgentSkill, *model.AgentSkillVersion, error) {
	var skill model.AgentSkill
	var ver model.AgentSkillVersion
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 技能行：不存在就插入（并发创建时 ON CONFLICT 忽略），再 FOR UPDATE 锁住它，串行化同一技能的版本号分配
		seed := model.AgentSkill{Name: in.Name, Title: in.Title, CreatedBy: in.CreatedBy}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "name"}}, DoNothing: true}).Create(&seed).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("name = ?", in.Name).Take(&skill).Error; err != nil {
			return err
		}
		// 2. latest_version 只增不减：删除版本后号码也不复用
		skill.LatestVersion++
		if err := tx.Model(&model.AgentSkill{}).Where("id = ?", skill.ID).Update("latest_version", skill.LatestVersion).Error; err != nil {
			return err
		}
		ver = in.Version
		ver.ID, ver.SkillID, ver.Version, ver.State = 0, skill.ID, skill.LatestVersion, model.SkillVersionPending
		return tx.Create(&ver).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &skill, &ver, nil
}

// PromoteVersion 把 pending 版本置为 ready；技能还没有生效版本（首个版本）时同时把它设为生效。
// 版本不是 pending（已被清理任务回收）返回 ErrNotFound。返回更新后的技能。
func (r *AgentSkillRepository) PromoteVersion(ctx context.Context, versionID uint64) (*model.AgentSkill, error) {
	var skill model.AgentSkill
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.AgentSkillVersion{}).Where("id = ? AND state = ?", versionID, model.SkillVersionPending).
			Updates(map[string]any{"state": model.SkillVersionReady, "updated_at": time.Now()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		var v model.AgentSkillVersion
		if err := tx.Select("skill_id", "version").Take(&v, versionID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&skill, v.SkillID).Error; err != nil {
			return err
		}
		if skill.ActiveVersion == nil {
			skill.ActiveVersion = &v.Version
			return tx.Model(&model.AgentSkill{}).Where("id = ?", skill.ID).Update("active_version", v.Version).Error
		}
		return nil
	})
	if err != nil {
		return nil, translate(err)
	}
	return &skill, nil
}

// DiscardVersion 删除一个 pending 版本行（写对象失败时的回退）。只删 pending，ready 的版本不受影响。
func (r *AgentSkillRepository) DiscardVersion(ctx context.Context, versionID uint64) error {
	return r.db.WithContext(ctx).Where("id = ? AND state = ?", versionID, model.SkillVersionPending).Delete(&model.AgentSkillVersion{}).Error
}

// SetEnabled 改启用状态；技能不存在返回 ErrNotFound。
func (r *AgentSkillRepository) SetEnabled(ctx context.Context, skillID uint64, enabled bool) error {
	return r.updateSkill(ctx, skillID, map[string]any{"enabled": enabled})
}

// SetActiveVersion 改生效版本号；技能不存在返回 ErrNotFound。调用方先确认版本存在且 ready。
func (r *AgentSkillRepository) SetActiveVersion(ctx context.Context, skillID uint64, version int) error {
	return r.updateSkill(ctx, skillID, map[string]any{"active_version": version})
}

// SetTitle 改显示名；技能不存在返回 ErrNotFound。
func (r *AgentSkillRepository) SetTitle(ctx context.Context, skillID uint64, title string) error {
	return r.updateSkill(ctx, skillID, map[string]any{"title": title})
}

func (r *AgentSkillRepository) updateSkill(ctx context.Context, skillID uint64, fields map[string]any) error {
	fields["updated_at"] = time.Now()
	res := r.db.WithContext(ctx).Model(&model.AgentSkill{}).Where("id = ?", skillID).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkVersionDeleting 把一个 ready 版本标为 deleting（此后任何读路径都看不到它），对象由清理任务删除。
// 版本不存在返回 ErrNotFound；它是技能的生效版本时返回 ErrInUse（竞态下的兜底，service 已预检）。
func (r *AgentSkillRepository) MarkVersionDeleting(ctx context.Context, skillID uint64, version int) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var skill model.AgentSkill
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&skill, skillID).Error; err != nil {
			return err
		}
		if skill.ActiveVersion != nil && *skill.ActiveVersion == version {
			return ErrInUse
		}
		res := tx.Model(&model.AgentSkillVersion{}).Where("skill_id = ? AND version = ? AND state = ?", skillID, version, model.SkillVersionReady).
			Updates(map[string]any{"state": model.SkillVersionDeleting, "updated_at": time.Now()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	return translate(err)
}

// DeleteSkill 在一个事务里删除技能行，并把它的全部版本标为 deleting；对象由清理任务删除。
// 技能不存在返回 ErrNotFound；技能仍启用返回 ErrInUse（竞态兜底，service 已要求先停用）。
func (r *AgentSkillRepository) DeleteSkill(ctx context.Context, skillID uint64) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var skill model.AgentSkill
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&skill, skillID).Error; err != nil {
			return err
		}
		if skill.Enabled {
			return ErrInUse
		}
		if err := tx.Model(&model.AgentSkillVersion{}).Where("skill_id = ? AND state <> ?", skillID, model.SkillVersionDeleting).
			Updates(map[string]any{"state": model.SkillVersionDeleting, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.AgentSkill{}, skillID).Error
	})
	return translate(err)
}

// ListEnabled 返回已启用且生效版本为 ready 的技能，按名字排序。目录、搜索、@ 弹层都读它。
func (r *AgentSkillRepository) ListEnabled(ctx context.Context) ([]EnabledSkill, error) {
	var out []EnabledSkill
	err := r.db.WithContext(ctx).Table("agent_skills AS s").
		Select("s.name AS name, s.title AS title, v.description AS description, v.id AS version_id, v.version AS version, v.has_scripts AS has_scripts").
		Joins("JOIN agent_skill_versions v ON v.skill_id = s.id AND v.version = s.active_version AND v.state = ?", model.SkillVersionReady).
		Where("s.enabled = ?", true).Order("s.name").Scan(&out).Error
	return out, err
}

// CreateImport 登记一条导入暂存。
func (r *AgentSkillRepository) CreateImport(ctx context.Context, imp *model.AgentSkillImport) error {
	return r.db.WithContext(ctx).Create(imp).Error
}

// GetImport 按 id 取导入暂存；不存在返回 ErrNotFound。过期判断由 service 做。
func (r *AgentSkillRepository) GetImport(ctx context.Context, id string) (*model.AgentSkillImport, error) {
	var imp model.AgentSkillImport
	if err := r.db.WithContext(ctx).Where("id = ?", id).Take(&imp).Error; err != nil {
		return nil, translate(err)
	}
	return &imp, nil
}

// DeleteImport 删除暂存行，返回是否真的删到了（并发确认时只有一个调用方拿到 true）。
func (r *AgentSkillRepository) DeleteImport(ctx context.Context, id string) (bool, error) {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.AgentSkillImport{})
	return res.RowsAffected > 0, res.Error
}

// ListExpiredImports 返回已过期的暂存，最多 limit 条。
func (r *AgentSkillRepository) ListExpiredImports(ctx context.Context, now time.Time, limit int) ([]model.AgentSkillImport, error) {
	var out []model.AgentSkillImport
	err := r.db.WithContext(ctx).Where("expires_at < ?", now).Order("expires_at").Limit(limit).Find(&out).Error
	return out, err
}

// ListGarbageVersions 返回待清理的版本：deleting，以及超过 pendingBefore 还没转成 ready 的 pending（写对象中途崩溃留下的）。最多 limit 条。
func (r *AgentSkillRepository) ListGarbageVersions(ctx context.Context, pendingBefore time.Time, limit int) ([]model.AgentSkillVersion, error) {
	var out []model.AgentSkillVersion
	err := r.db.WithContext(ctx).Omit(versionHeadOmit...).
		Where("state = ? OR (state = ? AND updated_at < ?)", model.SkillVersionDeleting, model.SkillVersionPending, pendingBefore).
		Order("id").Limit(limit).Find(&out).Error
	return out, err
}

// DeleteVersionRow 硬删一个版本行（对象已删除之后）。行不存在不算错误。
func (r *AgentSkillRepository) DeleteVersionRow(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.AgentSkillVersion{}).Error
}

// ListActiveVersions 返回每个技能生效版本的头信息（不含正文、文件清单），按 skill_id 索引；列表页用它避免逐个技能查询。
func (r *AgentSkillRepository) ListActiveVersions(ctx context.Context) (map[uint64]model.AgentSkillVersion, error) {
	var vs []model.AgentSkillVersion
	err := r.db.WithContext(ctx).Table("agent_skill_versions AS v").
		Select("v.id, v.skill_id, v.version, v.state, v.sha256, v.description, v.unsupported_fields, v.file_count, v.total_bytes, v.has_scripts, v.issues_json, v.created_by, v.created_at").
		Joins("JOIN agent_skills s ON s.id = v.skill_id AND s.active_version = v.version").
		Where("v.state = ?", model.SkillVersionReady).Scan(&vs).Error
	if err != nil {
		return nil, err
	}
	out := make(map[uint64]model.AgentSkillVersion, len(vs))
	for _, v := range vs {
		out[v.SkillID] = v
	}
	return out, nil
}

// ListVersionNumbers 返回每个技能全部 ready 版本的版本号，按 skill_id 索引；列表页据此算版本数和「有新版本待启用」。
func (r *AgentSkillRepository) ListVersionNumbers(ctx context.Context) (map[uint64][]int, error) {
	var rows []struct {
		SkillID uint64
		Version int
	}
	err := r.db.WithContext(ctx).Model(&model.AgentSkillVersion{}).Select("skill_id, version").
		Where("state = ?", model.SkillVersionReady).Order("skill_id, version").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := map[uint64][]int{}
	for _, row := range rows {
		out[row.SkillID] = append(out[row.SkillID], row.Version)
	}
	return out, nil
}
