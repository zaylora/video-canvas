package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// AIConfigRepository 是 AI 配置（模型 / 凭证）的数据访问。
// 指针表（ai_models）记录上下架、排序和“配置在哪一行”，配置正文在 ai_config_revisions 里，
// 每个模型只有一行（没有版本历史）；保存在事务里完成。插件与渠道不走 revision，见 AIPluginRepository / AIChannelRepository。
type AIConfigRepository struct {
	db *gorm.DB
}

// NewAIConfigRepository 创建 AI 配置仓储。
func NewAIConfigRepository(db *gorm.DB) *AIConfigRepository {
	return &AIConfigRepository{db: db}
}

// ConfigPointer 标识一个配置目标（目前只有模型），并携带要同步到指针表的冗余字段。
type ConfigPointer struct {
	Target string // model.ConfigTargetModel
	Key    string
	Kind   string // 模型种类
}

// SaveModelInput 保存模型配置的参数。
type SaveModelInput struct {
	Pointer   ConfigPointer
	Body      []byte
	CreatedBy uint64
	Note      string
	// InitialEnabled / InitialSort 只在 model 指针行首次创建时使用；
	// 之后上下架与排序只由管理接口修改，不受正文影响。
	InitialEnabled bool
	InitialSort    int
}

// PublishedModel 是一个模型的配置（指针行 + 配置正文）。
type PublishedModel struct {
	Key        string
	Kind       string
	Enabled    bool
	Sort       int
	RevisionID uint64
	RevisionNo int
	Body       model.JSONText
}

// SaveModel 保存模型配置：在事务里 upsert 指针行（同时锁住该行，串行化同一模型的并发保存）→
// 已有配置行就原地覆盖正文，没有就插入一行并让指针指向它。每个模型始终只有一份配置，没有版本历史；
// 配置是否对用户可见只看指针行的 enabled。
func (r *AIConfigRepository) SaveModel(ctx context.Context, in SaveModelInput) (*model.AIConfigRevision, error) {
	var rev *model.AIConfigRevision
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. upsert 指针行：新建时写初始值，已存在时只同步冗余字段（不动 enabled / sort）
		if err := upsertPointer(tx, in); err != nil {
			return err
		}
		var ptr model.AIModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&ptr, "key = ?", in.Pointer.Key).Error; err != nil {
			return err
		}
		// 2. 已有配置行：原地覆盖
		if ptr.PublishedRevisionID != nil {
			var cur model.AIConfigRevision
			if err := tx.First(&cur, *ptr.PublishedRevisionID).Error; err == nil {
				cur.BodyJSON, cur.CreatedBy, cur.Note = model.JSONText(in.Body), in.CreatedBy, in.Note
				if err := tx.Model(&cur).Select("body_json", "created_by", "note").Updates(&cur).Error; err != nil {
					return err
				}
				rev = &cur
				return nil
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		// 3. 第一次保存：插入唯一的一行并让指针指向它
		n := &model.AIConfigRevision{
			Target:     in.Pointer.Target,
			TargetKey:  in.Pointer.Key,
			RevisionNo: 1,
			BodyJSON:   model.JSONText(in.Body),
			Status:     model.RevisionPublished,
			CreatedBy:  in.CreatedBy,
			Note:       in.Note,
		}
		if err := tx.Create(n).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.AIModel{}).Where("key = ?", in.Pointer.Key).
			Update("published_revision_id", n.ID).Error; err != nil {
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

// upsertPointer 创建或更新模型指针行；冲突更新会锁住已有行直到事务结束。
func upsertPointer(tx *gorm.DB, in SaveModelInput) error {
	p := in.Pointer
	sort := in.InitialSort
	if sort == 0 {
		sort = 100
	}
	row := &model.AIModel{Key: p.Key, Kind: p.Kind, Enabled: in.InitialEnabled, Sort: sort}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"kind", "updated_at"}),
	}).Create(row).Error
}

// GetModelConfig 返回模型的配置（含正文）；模型不存在或还没有配置返回 ErrNotFound。
func (r *AIConfigRepository) GetModelConfig(ctx context.Context, key string) (*model.AIConfigRevision, error) {
	var m model.AIModel
	if err := r.db.WithContext(ctx).First(&m, "key = ?", key).Error; err != nil {
		return nil, translate(err)
	}
	if m.PublishedRevisionID == nil {
		return nil, ErrNotFound
	}
	var rev model.AIConfigRevision
	if err := r.db.WithContext(ctx).First(&rev, *m.PublishedRevisionID).Error; err != nil {
		return nil, translate(err)
	}
	return &rev, nil
}

// ListModelConfigs 返回所有模型的配置（含正文），按 key 升序，供列表页取展示名与渠道。
func (r *AIConfigRepository) ListModelConfigs(ctx context.Context) ([]model.AIConfigRevision, error) {
	var list []model.AIConfigRevision
	err := r.db.WithContext(ctx).Table("ai_config_revisions AS r").
		Select("r.*").
		Joins("JOIN ai_models AS m ON m.published_revision_id = r.id").
		Order("r.target_key ASC").Scan(&list).Error
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

// LoadModels 一次取出所有模型的配置（含未启用的，是否可见由 enabled 决定），按 sort、key 升序。
func (r *AIConfigRepository) LoadModels(ctx context.Context) ([]PublishedModel, error) {
	var list []PublishedModel
	err := r.db.WithContext(ctx).Table("ai_models AS m").
		Select("m.key AS key, m.kind AS kind, m.enabled AS enabled, m.sort AS sort, " +
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

// DeleteModel 在一个事务里硬删除模型：锁住指针行（FOR UPDATE，与保存串行）→ 锁内仍是上架状态返回 ErrInUse →
// 删除该模型的全部 revision（target=model、target_key=key）→ 删除指针行。指针行不存在返回 ErrNotFound。
// 删除后同名 key 可以重新新建，revision_no 从 1 开始（旧 revision 已不在，不会撞 uk_rev_target_no）。
func (r *AIConfigRepository) DeleteModel(ctx context.Context, key string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m model.AIModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&m, "key = ?", key).Error; err != nil {
			return translate(err)
		}
		if m.Enabled {
			return ErrInUse
		}
		if err := tx.Where("target = ? AND target_key = ?", model.ConfigTargetModel, key).
			Delete(&model.AIConfigRevision{}).Error; err != nil {
			return err
		}
		return tx.Where("key = ?", key).Delete(&model.AIModel{}).Error
	})
}
