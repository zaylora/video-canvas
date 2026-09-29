package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// AIConfigRepository 是 AI 配置（平台协议 / 模型 / 凭证）的数据访问。
// 指针表（ai_providers / ai_models）只记录“当前发布的是哪个 revision”和少量冗余字段，
// 正文都在 ai_config_revisions 里；所有状态切换都在事务里完成。
type AIConfigRepository struct {
	db *gorm.DB
}

func NewAIConfigRepository(db *gorm.DB) *AIConfigRepository {
	return &AIConfigRepository{db: db}
}

// ConfigPointer 标识一个配置目标，并携带要同步到指针表的冗余字段。
// provider 只用 Name；model 只用 Kind / ProviderKey。
type ConfigPointer struct {
	Target      string // model.ConfigTargetProvider / model.ConfigTargetModel
	Key         string
	Name        string // provider 的显示名
	Kind        string // model 的种类
	ProviderKey string // model 所属平台
}

// SaveDraftInput 保存草稿的参数。
type SaveDraftInput struct {
	Pointer   ConfigPointer
	Body      []byte
	CreatedBy uint64
	Note      string
	// InitialEnabled / InitialSort 只在 model 指针行首次创建时使用；
	// 之后上下架与排序只由管理接口修改，不受草稿正文影响。
	InitialEnabled bool
	InitialSort    int
}

// PublishedProvider 是已发布的平台配置（指针行 + 发布版本正文）。
type PublishedProvider struct {
	Key        string
	Name       string
	RevisionID uint64
	RevisionNo int
	Body       model.JSONText
}

// PublishedModel 是已发布的模型配置（指针行 + 发布版本正文）。
type PublishedModel struct {
	Key         string
	Kind        string
	ProviderKey string
	Enabled     bool
	Sort        int
	RevisionID  uint64
	RevisionNo  int
	Body        model.JSONText
}

// revisionMetaColumns 是不含正文的列，列表类查询用它避免读出大字段。
const revisionMetaColumns = "id, target, target_key, revision_no, status, created_by, note, created_at"

// SaveDraft 保存一个新草稿：
// 在事务里 upsert 指针行（同时锁住该行，串行化同一目标的并发保存）→ 把旧 draft 置 archived →
// 以 max(revision_no)+1 插入新的 draft。同一目标始终只有最新一个 draft。
func (r *AIConfigRepository) SaveDraft(ctx context.Context, in SaveDraftInput) (*model.AIConfigRevision, error) {
	var rev *model.AIConfigRevision
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. upsert 指针行：新建时写初始值，已存在时只同步冗余字段（不动 enabled / sort / 发布指针）
		if err := upsertPointer(tx, in); err != nil {
			return err
		}
		// 2. 旧草稿归档
		if err := tx.Model(&model.AIConfigRevision{}).
			Where("target = ? AND target_key = ? AND status = ?", in.Pointer.Target, in.Pointer.Key, model.RevisionDraft).
			Update("status", model.RevisionArchived).Error; err != nil {
			return err
		}
		// 3. 计算下一个版本号并插入新草稿
		var maxNo int
		if err := tx.Model(&model.AIConfigRevision{}).
			Where("target = ? AND target_key = ?", in.Pointer.Target, in.Pointer.Key).
			Select("COALESCE(MAX(revision_no), 0)").Scan(&maxNo).Error; err != nil {
			return err
		}
		n := &model.AIConfigRevision{
			Target:     in.Pointer.Target,
			TargetKey:  in.Pointer.Key,
			RevisionNo: maxNo + 1,
			BodyJSON:   model.JSONText(in.Body),
			Status:     model.RevisionDraft,
			CreatedBy:  in.CreatedBy,
			Note:       in.Note,
		}
		if err := tx.Create(n).Error; err != nil {
			return err
		}
		rev = n
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rev, nil
}

// upsertPointer 创建或更新指针行；冲突更新会锁住已有行直到事务结束。
func upsertPointer(tx *gorm.DB, in SaveDraftInput) error {
	p := in.Pointer
	if p.Target == model.ConfigTargetProvider {
		row := &model.AIProvider{Key: p.Key, Name: p.Name}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"name", "updated_at"}),
		}).Create(row).Error
	}
	sort := in.InitialSort
	if sort == 0 {
		sort = 100
	}
	row := &model.AIModel{Key: p.Key, Kind: p.Kind, ProviderKey: p.ProviderKey, Enabled: in.InitialEnabled, Sort: sort}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"kind", "provider_key", "updated_at"}),
	}).Create(row).Error
}

// GetDraft 返回目标最新的 draft；没有草稿返回 ErrNotFound。
func (r *AIConfigRepository) GetDraft(ctx context.Context, target, key string) (*model.AIConfigRevision, error) {
	var rev model.AIConfigRevision
	if err := r.db.WithContext(ctx).
		Where("target = ? AND target_key = ? AND status = ?", target, key, model.RevisionDraft).
		Order("revision_no DESC").First(&rev).Error; err != nil {
		return nil, translate(err)
	}
	return &rev, nil
}

// GetRevision 按 id 查询 revision（含正文）；不存在返回 ErrNotFound。
func (r *AIConfigRepository) GetRevision(ctx context.Context, id uint64) (*model.AIConfigRevision, error) {
	var rev model.AIConfigRevision
	if err := r.db.WithContext(ctx).First(&rev, id).Error; err != nil {
		return nil, translate(err)
	}
	return &rev, nil
}

// GetPublishedRevision 通过指针行找到当前发布的 revision；目标不存在或尚未发布返回 ErrNotFound。
func (r *AIConfigRepository) GetPublishedRevision(ctx context.Context, target, key string) (*model.AIConfigRevision, error) {
	var id *uint64
	var err error
	if target == model.ConfigTargetProvider {
		var p model.AIProvider
		err = r.db.WithContext(ctx).First(&p, "key = ?", key).Error
		id = p.PublishedRevisionID
	} else {
		var m model.AIModel
		err = r.db.WithContext(ctx).First(&m, "key = ?", key).Error
		id = m.PublishedRevisionID
	}
	if err != nil {
		return nil, translate(err)
	}
	if id == nil {
		return nil, ErrNotFound
	}
	return r.GetRevision(ctx, *id)
}

// ListRevisions 返回目标的历史版本（不含正文），revision_no 倒序，limit<=0 时默认 50。
func (r *AIConfigRepository) ListRevisions(ctx context.Context, target, key string, limit int) ([]model.AIConfigRevision, error) {
	if limit <= 0 {
		limit = 50
	}
	var list []model.AIConfigRevision
	err := r.db.WithContext(ctx).Select(revisionMetaColumns).
		Where("target = ? AND target_key = ?", target, key).
		Order("revision_no DESC").Limit(limit).Find(&list).Error
	return list, err
}

// ListRevisionHeads 返回某类目标所有当前的 draft 与 published revision，供列表页汇总状态。
// withBody=false 时不读正文。
func (r *AIConfigRepository) ListRevisionHeads(ctx context.Context, target string, withBody bool) ([]model.AIConfigRevision, error) {
	db := r.db.WithContext(ctx).Where("target = ? AND status IN ?", target, []string{model.RevisionDraft, model.RevisionPublished})
	if !withBody {
		db = db.Select(revisionMetaColumns)
	}
	var list []model.AIConfigRevision
	err := db.Order("target_key ASC, revision_no DESC").Find(&list).Error
	return list, err
}

// PublishDraft 发布指定的 draft：在事务里锁指针行，校验 revisionID 确实是该目标当前的 draft，
// 旧 published 置 archived，draft 置 published，并更新指针行的发布指针和冗余字段。
// 指针行不存在 / revision 不属于该目标返回 ErrNotFound；revision 不是 draft（已被新草稿顶替）返回 ErrRevisionConflict。
func (r *AIConfigRepository) PublishDraft(ctx context.Context, ptr ConfigPointer, revisionID uint64) (*model.AIConfigRevision, error) {
	return r.switchPublished(ctx, ptr, revisionID, model.RevisionDraft)
}

// Rollback 回滚：把指针改回指定的旧 revision（必须是 archived），该 revision 改回 published，当前 published 置 archived。
// 错误约定同 PublishDraft。
func (r *AIConfigRepository) Rollback(ctx context.Context, ptr ConfigPointer, revisionID uint64) (*model.AIConfigRevision, error) {
	return r.switchPublished(ctx, ptr, revisionID, model.RevisionArchived)
}

// switchPublished 是发布与回滚共用的状态切换，wantStatus 是目标 revision 切换前必须处于的状态。
func (r *AIConfigRepository) switchPublished(ctx context.Context, ptr ConfigPointer, revisionID uint64, wantStatus string) (*model.AIConfigRevision, error) {
	var out *model.AIConfigRevision
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 锁指针行，串行化同一目标的发布 / 回滚 / 保存草稿
		oldID, err := lockPointer(tx, ptr)
		if err != nil {
			return err
		}
		// 2. 校验目标 revision
		var rev model.AIConfigRevision
		if err := tx.Where("id = ? AND target = ? AND target_key = ?", revisionID, ptr.Target, ptr.Key).First(&rev).Error; err != nil {
			return translate(err)
		}
		if rev.Status != wantStatus {
			return ErrRevisionConflict
		}
		// 3. 旧 published 归档，目标 revision 置 published
		if oldID != nil && *oldID != rev.ID {
			if err := tx.Model(&model.AIConfigRevision{}).
				Where("id = ? AND status = ?", *oldID, model.RevisionPublished).
				Update("status", model.RevisionArchived).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&model.AIConfigRevision{}).Where("id = ?", rev.ID).
			Update("status", model.RevisionPublished).Error; err != nil {
			return err
		}
		rev.Status = model.RevisionPublished
		// 4. 更新指针行
		if err := updatePointerPublished(tx, ptr, rev.ID); err != nil {
			return err
		}
		out = &rev
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// lockPointer 用 FOR UPDATE 锁住指针行，返回它当前的发布指针；指针行不存在返回 ErrNotFound。
func lockPointer(tx *gorm.DB, ptr ConfigPointer) (*uint64, error) {
	lock := clause.Locking{Strength: "UPDATE"}
	if ptr.Target == model.ConfigTargetProvider {
		var p model.AIProvider
		if err := tx.Clauses(lock).First(&p, "key = ?", ptr.Key).Error; err != nil {
			return nil, translate(err)
		}
		return p.PublishedRevisionID, nil
	}
	var m model.AIModel
	if err := tx.Clauses(lock).First(&m, "key = ?", ptr.Key).Error; err != nil {
		return nil, translate(err)
	}
	return m.PublishedRevisionID, nil
}

// updatePointerPublished 把指针行指向 revID，同时同步冗余字段（provider 的 name / model 的 kind、provider_key）。
func updatePointerPublished(tx *gorm.DB, ptr ConfigPointer, revID uint64) error {
	now := time.Now()
	if ptr.Target == model.ConfigTargetProvider {
		fields := map[string]any{"published_revision_id": revID, "updated_at": now}
		if ptr.Name != "" {
			fields["name"] = ptr.Name
		}
		return tx.Model(&model.AIProvider{}).Where("key = ?", ptr.Key).Updates(fields).Error
	}
	fields := map[string]any{"published_revision_id": revID, "updated_at": now}
	if ptr.Kind != "" {
		fields["kind"] = ptr.Kind
	}
	if ptr.ProviderKey != "" {
		fields["provider_key"] = ptr.ProviderKey
	}
	return tx.Model(&model.AIModel{}).Where("key = ?", ptr.Key).Updates(fields).Error
}

// GetProviderPointer 返回平台指针行；不存在返回 ErrNotFound。
func (r *AIConfigRepository) GetProviderPointer(ctx context.Context, key string) (*model.AIProvider, error) {
	var p model.AIProvider
	if err := r.db.WithContext(ctx).First(&p, "key = ?", key).Error; err != nil {
		return nil, translate(err)
	}
	return &p, nil
}

// ListProviderPointers 返回所有平台指针行，按 key 升序。
func (r *AIConfigRepository) ListProviderPointers(ctx context.Context) ([]model.AIProvider, error) {
	var list []model.AIProvider
	err := r.db.WithContext(ctx).Order("key ASC").Find(&list).Error
	return list, err
}

// GetModelPointer 返回模型指针行；不存在返回 ErrNotFound。
func (r *AIConfigRepository) GetModelPointer(ctx context.Context, key string) (*model.AIModel, error) {
	var m model.AIModel
	if err := r.db.WithContext(ctx).First(&m, "key = ?", key).Error; err != nil {
		return nil, translate(err)
	}
	return &m, nil
}

// ListModelPointers 返回所有模型指针行，按 sort、key 升序。
func (r *AIConfigRepository) ListModelPointers(ctx context.Context) ([]model.AIModel, error) {
	var list []model.AIModel
	err := r.db.WithContext(ctx).Order("sort ASC, key ASC").Find(&list).Error
	return list, err
}

// SetModelEnabled 上架 / 下架模型；模型不存在返回 ErrNotFound。
func (r *AIConfigRepository) SetModelEnabled(ctx context.Context, key string, enabled bool) error {
	return r.updateModelPointer(ctx, key, map[string]any{"enabled": enabled, "updated_at": time.Now()})
}

// SetModelSort 修改模型排序值（升序排列）；模型不存在返回 ErrNotFound。
func (r *AIConfigRepository) SetModelSort(ctx context.Context, key string, sort int) error {
	return r.updateModelPointer(ctx, key, map[string]any{"sort": sort, "updated_at": time.Now()})
}

func (r *AIConfigRepository) updateModelPointer(ctx context.Context, key string, fields map[string]any) error {
	res := r.db.WithContext(ctx).Model(&model.AIModel{}).Where("key = ?", key).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// LoadPublishedProviders 一次取出所有已发布的平台配置（指针行 join 发布版本正文），Registry 刷新用。
func (r *AIConfigRepository) LoadPublishedProviders(ctx context.Context) ([]PublishedProvider, error) {
	var list []PublishedProvider
	err := r.db.WithContext(ctx).Table("ai_providers AS p").
		Select("p.key AS key, p.name AS name, r.id AS revision_id, r.revision_no AS revision_no, r.body_json AS body").
		Joins("JOIN ai_config_revisions AS r ON r.id = p.published_revision_id").
		Order("p.key ASC").Scan(&list).Error
	return list, err
}

// LoadPublishedModels 一次取出所有已发布的模型配置，按 sort、key 升序。
func (r *AIConfigRepository) LoadPublishedModels(ctx context.Context) ([]PublishedModel, error) {
	var list []PublishedModel
	err := r.db.WithContext(ctx).Table("ai_models AS m").
		Select("m.key AS key, m.kind AS kind, m.provider_key AS provider_key, m.enabled AS enabled, m.sort AS sort, " +
			"r.id AS revision_id, r.revision_no AS revision_no, r.body_json AS body").
		Joins("JOIN ai_config_revisions AS r ON r.id = m.published_revision_id").
		Order("m.sort ASC, m.key ASC").Scan(&list).Error
	return list, err
}

// UpsertSecret 写入或覆盖凭证密文（只写；按 name 冲突更新）。
func (r *AIConfigRepository) UpsertSecret(ctx context.Context, s *model.AISecret) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"ciphertext", "nonce", "key_version", "updated_by", "updated_at"}),
	}).Create(s).Error
}

// GetSecret 读取凭证（含密文，仅供解密使用）；不存在返回 ErrNotFound。
func (r *AIConfigRepository) GetSecret(ctx context.Context, name string) (*model.AISecret, error) {
	var s model.AISecret
	if err := r.db.WithContext(ctx).First(&s, "name = ?", name).Error; err != nil {
		return nil, translate(err)
	}
	return &s, nil
}

// ListSecrets 列出所有已设置的凭证元信息，不读取密文和 nonce，按 name 升序。
func (r *AIConfigRepository) ListSecrets(ctx context.Context) ([]model.AISecret, error) {
	var list []model.AISecret
	err := r.db.WithContext(ctx).Select("name, key_version, updated_by, updated_at").Order("name ASC").Find(&list).Error
	return list, err
}
