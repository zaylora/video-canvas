package model

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/pkg/idcodec" //nolint:depguard // 对外 ID 沿用资产库文件夹的十六进制编码
)

// CharacterCard 用户私有的角色卡：只保存素材引用，不复制文件。
// 同一用户下名称忽略大小写唯一（表达式唯一索引 lower(name)，迁移里同名）；素材可用性在写入时校验、读取时降级。
type CharacterCard struct {
	ID            uint64                      `gorm:"primaryKey" json:"id"`                                                                                                          // 角色卡 ID
	UserID        uint64                      `gorm:"not null;uniqueIndex:uk_character_cards_user_name,priority:1;index:idx_character_cards_user_updated,priority:1" json:"user_id"` // 所属用户
	Name          string                      `gorm:"size:64;not null;uniqueIndex:uk_character_cards_user_name,priority:2,expression:lower(name)" json:"name"`                       // 角色名，1–64 字
	Description   string                      `gorm:"type:text;not null;default:''" json:"description"`                                                                              // 角色设定，≤ 500 字，只展示
	AvatarAssetID uint64                      `gorm:"not null" json:"avatar_asset_id"`                                                                                               // 主形象图（assets.kind = image）
	ExtraAssetIDs datatypes.JSONSlice[uint64] `gorm:"type:jsonb;not null;default:'[]'" json:"extra_asset_ids"`                                                                       // 补充图 ID，≤ 4 张，去重且不含主图
	VoiceAssetID  *uint64                     `json:"voice_asset_id"`                                                                                                                // 声音（assets.kind = audio），null 表示没有
	CreatedAt     time.Time                   `json:"created_at"`                                                                                                                    // 创建时间
	UpdatedAt     time.Time                   `gorm:"index:idx_character_cards_user_updated,priority:2,sort:desc" json:"updated_at"`                                                 // 更新时间，列表按它倒序
}

// TableName 返回角色卡表名。
func (CharacterCard) TableName() string { return "character_cards" }

// CreateCharacterReq 是新建角色卡的请求；长度、数量、素材归属与种类都在服务里校验。
type CreateCharacterReq struct {
	Name          string   `json:"name" label:"名称"`             // 名称，去首尾空白后 1–64 字
	Description   string   `json:"description" label:"设定"`      // 角色设定，默认空串
	AvatarAssetID uint64   `json:"avatar_asset_id" label:"主图"`  // 主形象图素材 ID，必填
	ExtraAssetIDs []uint64 `json:"extra_asset_ids" label:"补充图"` // 补充图素材 ID，最多 4 个
	VoiceAssetID  *uint64  `json:"voice_asset_id" label:"声音"`   // 声音素材 ID，可空
}

// UpdateCharacterReq 是部分更新角色卡的请求。
// 指针字段为 nil 表示请求里没出现、保持原值；声音用 raw JSON 区分「没传」和「传了 null 要清空」；
// 补充图用 nil 与空切片区分「没传」和「清空」。
type UpdateCharacterReq struct {
	Name          *string         `json:"name" label:"名称"`             // 新名称
	Description   *string         `json:"description" label:"设定"`      // 新设定
	AvatarAssetID *uint64         `json:"avatar_asset_id" label:"主图"`  // 新主图
	ExtraAssetIDs []uint64        `json:"extra_asset_ids" label:"补充图"` // 新补充图，[] 表示清空
	VoiceAssetID  json.RawMessage `json:"voice_asset_id" label:"声音"`   // null 清空，数字换成新声音
}

// CharacterView 是返回给前端的角色卡。
// 素材已不可用时：主图为 null（等同卡片失效），补充图直接略过，声音为 null。
type CharacterView struct {
	ID          idcodec.ID  `json:"id"`          // 角色卡 ID（十六进制串）
	Name        string      `json:"name"`        // 名称
	Description string      `json:"description"` // 设定
	Avatar      *AssetView  `json:"avatar"`      // 主形象图，必有；素材不可用时为 null
	Extras      []AssetView `json:"extras"`      // 补充图，0–4 张，按保存顺序
	Voice       *AssetView  `json:"voice"`       // 声音，可空
	CreatedAt   time.Time   `json:"created_at"`  // 创建时间
	UpdatedAt   time.Time   `json:"updated_at"`  // 更新时间
}

// CharacterListView 是角色卡列表与按 ID 批量读取的响应；批量读取时查不到的 ID 缺席。
type CharacterListView struct {
	Items []CharacterView `json:"items"` // 角色卡列表
}
