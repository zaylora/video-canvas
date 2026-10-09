package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// defaultConversationTitle 是默认创作的标题。
const defaultConversationTitle = "默认创作"

// ConversationRepository 是首页对话与生成记录的数据访问层。所有查询都带 user_id。
type ConversationRepository struct {
	db *gorm.DB
}

// NewConversationRepository 创建对话仓储。
func NewConversationRepository(db *gorm.DB) *ConversationRepository {
	return &ConversationRepository{db: db}
}

// EnsureDefault 返回用户的默认创作，不存在时创建。并发创建时靠部分唯一索引 uk_conversations_default 兜底：
// 后到的请求撞上唯一冲突，改为读取先到的那一段。
func (r *ConversationRepository) EnsureDefault(ctx context.Context, userID uint64) (*model.Conversation, error) {
	var c model.Conversation
	err := r.db.WithContext(ctx).Where("user_id = ? AND is_default", userID).First(&c).Error
	if err == nil {
		return &c, nil
	}
	if !errors.Is(translate(err), ErrNotFound) {
		return nil, err
	}
	c = model.Conversation{UserID: userID, Title: defaultConversationTitle, IsDefault: true}
	if err := r.db.WithContext(ctx).Create(&c).Error; err != nil {
		if !isUniqueViolation(err) {
			return nil, err
		}
		if err := r.db.WithContext(ctx).Where("user_id = ? AND is_default", userID).First(&c).Error; err != nil {
			return nil, translate(err)
		}
	}
	return &c, nil
}

// Create 新建对话。
func (r *ConversationRepository) Create(ctx context.Context, c *model.Conversation) error {
	return r.db.WithContext(ctx).Create(c).Error
}

// GetByID 按 id + user_id 查询未删除的对话，查不到或不属于该用户返回 ErrNotFound。
func (r *ConversationRepository) GetByID(ctx context.Context, userID, id uint64) (*model.Conversation, error) {
	var c model.Conversation
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&c).Error; err != nil {
		return nil, translate(err)
	}
	return &c, nil
}

// List 返回用户全部未删除的对话：默认创作在前，其余按最近记录时间倒序（没有记录的排最后），时间相同按 id 倒序。
func (r *ConversationRepository) List(ctx context.Context, userID uint64) ([]model.Conversation, error) {
	var out []model.Conversation
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("is_default DESC, last_record_at DESC NULLS LAST, id DESC").Find(&out).Error
	return out, err
}

// Count 统计用户未删除的对话数（含默认创作）。
func (r *ConversationRepository) Count(ctx context.Context, userID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Conversation{}).Where("user_id = ?", userID).Count(&n).Error
	return n, err
}

// Rename 按 id + user_id 改标题，不存在或不属于该用户返回 ErrNotFound。
func (r *ConversationRepository) Rename(ctx context.Context, userID, id uint64, title string) error {
	res := r.db.WithContext(ctx).Model(&model.Conversation{}).
		Where("id = ? AND user_id = ?", id, userID).Update("title", title)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 按 id + user_id 软删除对话，不存在或不属于该用户返回 ErrNotFound。
func (r *ConversationRepository) Delete(ctx context.Context, userID, id uint64) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.Conversation{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// AddRecordCount 原子地给对话的记录数加 delta（不会减到 0 以下）；at 非空时同时更新最近记录时间。
// 对话不存在（含已软删除）返回 ErrNotFound。
func (r *ConversationRepository) AddRecordCount(ctx context.Context, id uint64, delta int, at *time.Time) error {
	fields := map[string]any{"record_count": gorm.Expr("GREATEST(record_count + ?, 0)", delta)}
	if at != nil {
		fields["last_record_at"] = *at
	}
	res := r.db.WithContext(ctx).Model(&model.Conversation{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateRecord 新建记录；同一用户下幂等键重复（uk_conv_records_idem）返回 ErrDuplicate。
func (r *ConversationRepository) CreateRecord(ctx context.Context, rec *model.ConversationRecord) error {
	if err := r.db.WithContext(ctx).Create(rec).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// GetRecord 按 id + user_id 查询未删除的记录，查不到或不属于该用户返回 ErrNotFound。
func (r *ConversationRepository) GetRecord(ctx context.Context, userID, id uint64) (*model.ConversationRecord, error) {
	var rec model.ConversationRecord
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&rec).Error; err != nil {
		return nil, translate(err)
	}
	return &rec, nil
}

// FindRecordByIdem 按 user_id + 幂等键查询未删除的记录，查不到返回 ErrNotFound。
func (r *ConversationRepository) FindRecordByIdem(ctx context.Context, userID uint64, key string) (*model.ConversationRecord, error) {
	var rec model.ConversationRecord
	if err := r.db.WithContext(ctx).Where("user_id = ? AND idempotency_key = ?", userID, key).First(&rec).Error; err != nil {
		return nil, translate(err)
	}
	return &rec, nil
}

// ListRecords 查询对话里的记录：id 倒序，before > 0 时只要 id 小于它的，最多 limit 条。
// 带 user_id 条件，别人的对话查出来是空。
func (r *ConversationRepository) ListRecords(ctx context.Context, userID, convID, before uint64, limit int) ([]model.ConversationRecord, error) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	db := r.db.WithContext(ctx).Where("user_id = ? AND conversation_id = ?", userID, convID)
	if before > 0 {
		db = db.Where("id < ?", before)
	}
	var out []model.ConversationRecord
	err := db.Order("id DESC").Limit(limit).Find(&out).Error
	return out, err
}

// SaveRecordTasks 写入提交结果：每格任务 id、失败原因和冻结积分合计。记录不存在返回 ErrNotFound。
func (r *ConversationRepository) SaveRecordTasks(ctx context.Context, id uint64, taskIDs, submitErrors datatypes.JSON, quote int) error {
	res := r.db.WithContext(ctx).Model(&model.ConversationRecord{}).Where("id = ?", id).Updates(map[string]any{
		"task_ids": taskIDs, "submit_errors": submitErrors, "quote_credits": quote,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteRecord 按 id + user_id 软删除记录，不存在或不属于该用户返回 ErrNotFound。
// 软删除后幂等键的唯一约束不再占用该 key（索引只含未删除的行）。
func (r *ConversationRepository) DeleteRecord(ctx context.Context, userID, id uint64) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.ConversationRecord{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ConversationIDsByRecordIDs 返回这些记录所属对话的 id（去重）。只统计该用户未删除的记录，
// 且对话本身也必须未删除：已删除对话里仍在跑的任务不该让侧栏亮起圆点。
func (r *ConversationRepository) ConversationIDsByRecordIDs(ctx context.Context, userID uint64, recordIDs []uint64) ([]uint64, error) {
	var ids []uint64
	live := r.db.Model(&model.Conversation{}).Select("id").Where("user_id = ?", userID)
	err := r.db.WithContext(ctx).Model(&model.ConversationRecord{}).
		Where("user_id = ? AND id IN ? AND conversation_id IN (?)", userID, recordIDs, live).
		Distinct().Pluck("conversation_id", &ids).Error
	return ids, err
}
