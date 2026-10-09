package model

import (
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/pkg/idcodec" //nolint:depguard // 对外 id 用十六进制串，与画布、任务视图（存量代码）一致
)

// Conversation 是首页生成的一段对话：一组按时间排列的生成记录。
// 每个用户有且只有一段 IsDefault 的「默认创作」，首页提交都进它。
type Conversation struct {
	BaseModel
	UserID       uint64     `gorm:"not null;index:idx_conversations_user_last,priority:1;uniqueIndex:uk_conversations_default,where:is_default AND deleted_at IS NULL" json:"user_id"` // 所属用户；默认创作每人只有一段
	Title        string     `gorm:"size:50;not null;default:''" json:"title"`                                                                                                          // 标题，1–50 字
	IsDefault    bool       `gorm:"not null;default:false" json:"is_default"`                                                                                                          // 是否默认创作：不能删除
	RecordCount  int        `gorm:"not null;default:0" json:"record_count"`                                                                                                            // 记录条数
	LastRecordAt *time.Time `gorm:"index:idx_conversations_user_last,priority:2,sort:desc" json:"last_record_at"`                                                                      // 最近一条记录的时间，侧栏排序用
}

// TableName 表名。
func (Conversation) TableName() string { return "conversations" }

// ConversationRecord 是对话里的一条生成记录：一次提交对应一条，保存完整输入快照。
// 任务状态不存在这里，只认 generation_tasks（TaskIDs 指向它们）。
type ConversationRecord struct {
	BaseModel
	ConversationID   uint64         `gorm:"not null;index:idx_conv_records_conv,priority:1" json:"conversation_id"`                                                                                        // 所属对话
	UserID           uint64         `gorm:"not null;index:idx_conv_records_user,priority:1;uniqueIndex:uk_conv_records_idem,priority:1,where:idempotency_key <> '' AND deleted_at IS NULL" json:"user_id"` // 所属用户
	Kind             string         `gorm:"size:16;not null" json:"kind"`                                                                                                                                  // image / video / audio
	ModelKey         string         `gorm:"column:model_id;size:128;not null" json:"model_id"`                                                                                                             // 模型 key
	Prompt           string         `gorm:"type:text;not null;default:''" json:"prompt"`                                                                                                                   // 原始提示词
	InputJSON        datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"-"`                                                                                                                     // 提交给生成任务的完整输入快照，重新编辑和再次生成从这里复原
	Count            int            `gorm:"not null;default:1" json:"count"`                                                                                                                               // 生成数量（任务格子数）
	TaskIDsJSON      datatypes.JSON `gorm:"column:task_ids;type:jsonb;not null;default:'[]'" json:"-"`                                                                                                     // []uint64|null，长度等于 Count；null 表示该格提交失败
	SubmitErrorsJSON datatypes.JSON `gorm:"column:submit_errors;type:jsonb;not null;default:'[]'" json:"-"`                                                                                                // []RecordSubmitError
	QuoteCredits     int            `gorm:"not null;default:0" json:"quote_credits"`                                                                                                                       // 提交时冻结的积分合计
	IdempotencyKey   string         `gorm:"size:128;not null;default:'';uniqueIndex:uk_conv_records_idem,priority:2,where:idempotency_key <> '' AND deleted_at IS NULL" json:"-"`                          // 幂等键，同一用户下唯一
}

// TableName 表名。
func (ConversationRecord) TableName() string { return "conversation_records" }

// RecordSubmitError 是记录里某一格提交失败的原因。
type RecordSubmitError struct {
	Index   int    `json:"index"`   // 第几格，从 0 开始
	Status  int    `json:"status"`  // HTTP 状态
	Code    int    `json:"code"`    // 业务错误码
	Message string `json:"message"` // 给用户看的文案
}

// ConversationView 是对话列表里的一项。
type ConversationView struct {
	ID           idcodec.ID `json:"id"`             // 对话 ID（十六进制串）
	Title        string     `json:"title"`          // 标题
	IsDefault    bool       `json:"is_default"`     // 是否默认创作
	RecordCount  int        `json:"record_count"`   // 记录条数
	LastRecordAt *time.Time `json:"last_record_at"` // 最近记录时间
	Active       bool       `json:"active"`         // 是否有进行中的生成任务
	CreatedAt    time.Time  `json:"created_at"`     // 创建时间
}

// ConversationRecordView 是一条记录连同它的任务快照。
type ConversationRecordView struct {
	ID             idcodec.ID            `json:"id"`              // 记录 ID
	ConversationID idcodec.ID            `json:"conversation_id"` // 所属对话
	Kind           string                `json:"kind"`            // image / video / audio
	ModelID        string                `json:"model_id"`        // 模型 key
	Prompt         string                `json:"prompt"`          // 提示词
	Input          datatypes.JSON        `json:"input"`           // 输入快照
	Count          int                   `json:"count"`           // 生成数量
	Tasks          []*GenerationTaskView `json:"tasks"`           // 与格子一一对应，提交失败的格子是 null
	SubmitErrors   []RecordSubmitError   `json:"submit_errors"`   // 提交失败的格子和原因
	QuoteCredits   int                   `json:"quote_credits"`   // 冻结积分合计
	CreatedAt      time.Time             `json:"created_at"`      // 创建时间
}

// ConversationRecordPage 是一页记录，按时间倒序；Next 非空时用它作为下一页的 before。
type ConversationRecordPage struct {
	Items []ConversationRecordView `json:"items"` // 本页记录
	Next  *idcodec.ID              `json:"next"`  // 下一页游标，没有更多时为 null
}

// CreateConversationReq 新建对话。
type CreateConversationReq struct {
	Title string `json:"title" binding:"max=50" label:"标题"` // 不传时叫「新对话」
}

// RenameConversationReq 重命名对话。
type RenameConversationReq struct {
	Title string `json:"title" binding:"required,max=50" label:"标题"` // 新标题
}

// ListConversationRecordsReq 记录分页参数。
type ListConversationRecordsReq struct {
	Before string `form:"before" binding:"max=64" label:"游标"` // 上一页返回的 next；不传取最新一页
	Limit  int    `form:"limit" label:"条数"`                   // 每页条数，默认 20，最大 50
}

// SubmitConversationRecordReq 提交一条生成记录。Idempotency-Key 走请求头。
type SubmitConversationRecordReq struct {
	Kind    string         `json:"kind" binding:"required,oneof=image video audio" label:"生成种类"` // image / video / audio
	ModelID string         `json:"model_id" binding:"required,max=128" label:"模型"`               // 模型 key
	Prompt  string         `json:"prompt" binding:"max=4000" label:"提示词"`                        // 提示词，可以为空（只有参考图时）
	Input   map[string]any `json:"input" binding:"required" label:"生成参数"`                        // 完整的生成输入，按模型能力校验
	Count   int            `json:"count" binding:"required,min=1,max=4" label:"生成数量"`            // 生成数量，必须等于输入里的数量参数
	Title   string         `json:"title" binding:"max=50" label:"标题"`                            // 仅当对话是 new 时作为新对话标题；不传取提示词前 16 字
}

// SubmitConversationRecordResp 是提交结果。
type SubmitConversationRecordResp struct {
	ConversationID idcodec.ID             `json:"conversation_id"` // 记录所在的对话（default / new 时是解析出的真实对话）
	Record         ConversationRecordView `json:"record"`          // 新建的记录
}
