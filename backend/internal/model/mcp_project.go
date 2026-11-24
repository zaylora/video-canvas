package model

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
)

// MCPTokenPrefix 是 MCP 令牌明文的固定前缀。中间件靠它和登录 JWT、分析令牌区分开，三个互不干扰。
const MCPTokenPrefix = "vcm_"

// MCP 权限档位。默认什么都不给，由用户在「我的 MCP 接入」或管理员在后台按需勾选。
//
// 分档原则是「一次授权对应一类可感知的能力」，而不是粗粒度的读写：
// 只想让外部工具看看有哪些画布，不该连带把画布结构、图片视频一起给出去。
const (
	// MCPScopeCanvasBrowse 浏览目录：列画布、列模型、搜技能、读技能、查任务。全部无副作用。
	MCPScopeCanvasBrowse = "canvas:browse"
	// MCPScopeCanvasRead 读画布：读节点与连线结构。改画布之前必须先有它（读前写门禁）。
	MCPScopeCanvasRead = "canvas:read"
	// MCPScopeCanvasMedia 查看素材：看图片、看视频、从视频抓一帧。
	MCPScopeCanvasMedia = "canvas:media"
	// MCPScopeCanvasWrite 改画布：改节点属性与生成提示词。
	MCPScopeCanvasWrite = "canvas:write"
	// MCPScopeCanvasLayout 重排布局：移动节点与整理分组。
	MCPScopeCanvasLayout = "canvas:layout"
	// MCPScopeCanvasCompose 合成成片：触发合成。消耗算力不消耗积分。
	MCPScopeCanvasCompose = "canvas:compose"
	// MCPScopeCanvasGenerate 生成素材：出图、出视频、出配音。**花归属用户的积分**，所以单独一档，
	// 而且要用户主动勾——默认不勾的接入连生成工具都看不到。
	MCPScopeCanvasGenerate = "canvas:generate"
)

// 短剧是独立能力域，权限档不挂在 canvas: 前缀下。
//
// 它确实建立在画布之上（短剧项目通过 canvas_projects.drama_id 关联），但对外是一套完整能力：
// 剧本、分集、片段、流水线任务。放进 canvas:* 会让人以为「勾了画布权限就能改剧本」，
// 而实际上改的是用户辛苦写的剧本原文，必须单独授权。
const (
	// MCPScopeDramaRead 读短剧：列短剧、读大纲人物、读单集全文、读片段。
	MCPScopeDramaRead = "drama:read"
	// MCPScopeDramaWrite 改剧本：改大纲人物、改分集标题梗概剧本、改片段提示词与秒数。
	MCPScopeDramaWrite = "drama:write"
	// MCPScopeDramaRun 跑短剧任务：分集剧本、资产拆解、资产提示词、分镜、合成整集。**花归属用户的积分**。
	MCPScopeDramaRun = "drama:run"
)

// MCPScopeLabels 是权限档位的中文名，后台列表和接口回显共用。顺序即界面展示顺序。
var MCPScopeLabels = map[string]string{
	MCPScopeCanvasBrowse:   "浏览目录",
	MCPScopeCanvasRead:     "读画布",
	MCPScopeCanvasMedia:    "查看素材",
	MCPScopeCanvasWrite:    "改画布",
	MCPScopeCanvasLayout:   "重排布局",
	MCPScopeCanvasCompose:  "合成成片",
	MCPScopeCanvasGenerate: "生成素材",
	MCPScopeDramaRead:      "读短剧",
	MCPScopeDramaWrite:     "改剧本",
	MCPScopeDramaRun:       "跑短剧任务",
}

// MCPScopeOrder 是档位的固定顺序：按能力域分组，域内从最安全到影响最大。
var MCPScopeOrder = []string{
	MCPScopeCanvasBrowse, MCPScopeCanvasRead, MCPScopeCanvasMedia,
	MCPScopeCanvasWrite, MCPScopeCanvasLayout, MCPScopeCanvasCompose,
	MCPScopeCanvasGenerate,
	MCPScopeDramaRead, MCPScopeDramaWrite, MCPScopeDramaRun,
}

// MCPScopeRequires 是各档的隐含依赖。
//
// 这解决的是「权限互斥」问题：单给 write 不给 read 是坏配置——工具改画布前必须先读，
// 缺了 read 就等于给了把用不了的钥匙。任何一档都依赖 browse（否则连画布 code 都拿不到）。
//
// 短剧一族不依赖 canvas:*：短剧画布不在 canvas_list 里（网页端有意分开），
// 靠 drama_list 自己发现，所以给 drama:read 就够用，不该逼用户连带开画布权限。
var MCPScopeRequires = map[string][]string{
	MCPScopeCanvasRead:     {MCPScopeCanvasBrowse},
	MCPScopeCanvasMedia:    {MCPScopeCanvasRead},
	MCPScopeCanvasWrite:    {MCPScopeCanvasRead},
	MCPScopeCanvasLayout:   {MCPScopeCanvasRead},
	MCPScopeCanvasCompose:  {MCPScopeCanvasRead},
	MCPScopeCanvasGenerate: {MCPScopeCanvasRead},
	MCPScopeDramaWrite:     {MCPScopeDramaRead},
	MCPScopeDramaRun:       {MCPScopeDramaRead},
}

// ExpandMCPScopes 把权限补成闭包：缺的依赖一层层补上，结果按 MCPScopeOrder 排序。
// 写入时归一化、鉴权时再展开一次，老数据或直接调接口都不会留下「缺读只有写」的残废配置。
func ExpandMCPScopes(scopes []string) []string {
	have := make(map[string]bool, len(scopes)+4)
	var add func(string)
	add = func(scope string) {
		if have[scope] {
			return
		}
		have[scope] = true
		for _, dep := range MCPScopeRequires[scope] {
			add(dep)
		}
	}
	for _, s := range scopes {
		if MCPScopeKnown(s) {
			add(s)
		}
	}
	out := make([]string, 0, len(have))
	for _, s := range MCPScopeOrder {
		if have[s] {
			out = append(out, s)
		}
	}
	return out
}

// MCPScopeKnown 判断是不是认识的权限档位。不认识的直接拒绝，避免后台写进错值。
func MCPScopeKnown(scope string) bool {
	_, ok := MCPScopeLabels[scope]
	return ok
}

// MCPProject 是一个外部 AI 工具应用。
//
// 权限单位是**归属用户**，不是画布：令牌代表「这个用户授权了这个外部应用」，
// 它能访问这个用户名下的全部画布与素材。项目只是配置容器——名称、权限档、默认画布。
type MCPProject struct {
	ID          uint64         `gorm:"primaryKey" json:"id"`
	Name        string         `gorm:"size:64;not null" json:"name"`
	OwnerUserID uint64         `gorm:"not null;index:idx_mcp_projects_owner" json:"owner_user_id"`
	Scopes      datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"scopes"`
	// CanvasID 是**默认画布**，可空。只在调用方没显式指定 canvas 时用它，
	// 不构成权限边界——每次调用都会校验目标画布属于 OwnerUserID。
	CanvasID  *uint64   `gorm:"index:idx_mcp_projects_canvas" json:"canvas_id,omitempty"`
	Enabled   bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedBy uint64    `gorm:"not null" json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (MCPProject) TableName() string { return "mcp_projects" }

// MCPProjectToken 是项目令牌。指纹（sha256）用于鉴权，明文另存一份密文供「复制」用。
type MCPProjectToken struct {
	ID        uint64 `gorm:"primaryKey" json:"id"`
	ProjectID uint64 `gorm:"not null;index:idx_mcp_project_tokens_project" json:"project_id"`
	Name      string `gorm:"size:64;not null" json:"name"`
	Prefix    string `gorm:"size:12;not null" json:"prefix"`
	TokenHash string `gorm:"size:64;not null;uniqueIndex:uk_mcp_project_tokens_hash" json:"-"`
	// SecretCipher 是令牌明文的密文（base64(nonce||ciphertext)），主密钥由 APP_AI_SECRET_KEY 派生。
	// 登录用户在列表里点「复制」时解出来用；没配主密钥或迁移前签发的老令牌这里是空串。
	SecretCipher string     `gorm:"type:text;not null;default:''" json:"-"`
	ExpiresAt    time.Time  `gorm:"not null" json:"expires_at"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

// TableName 指定表名。
func (MCPProjectToken) TableName() string { return "mcp_project_tokens" }

// ScopeList 解出权限档位；内容坏掉时返回空集合（宁可少给权限，不多给）。
func (p *MCPProject) ScopeList() []string { return decodeStringList(p.Scopes) }

// ScopeSet 解出权限档位的集合，供逐档判断用。这里再展开一次依赖：
// 即使库里的值是历史数据（比如只有 canvas:write），鉴权时也按「已含 canvas:read」处理，
// 不会出现「给了写却读不了」的死配置。
func (p *MCPProject) ScopeSet() map[string]bool {
	raw := p.ScopeList()
	set := make(map[string]bool, len(raw)+2)
	for _, s := range ExpandMCPScopes(raw) {
		set[s] = true
	}
	return set
}

// SetScopes 写入权限档位。
func (p *MCPProject) SetScopes(scopes []string) { p.Scopes = encodeStringList(scopes) }

// MCPProjectView 是后台列表与详情共用的视图：不返回任何凭证。
type MCPProjectView struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	OwnerUserID string   `json:"owner_user_id"`
	OwnerName   string   `json:"owner_name,omitempty"`
	Scopes      []string `json:"scopes"`
	// CanvasCode 是默认画布地址里的那串十六进制 id；没设默认画布时为空。
	CanvasCode  string `json:"canvas_code,omitempty"`
	CanvasTitle string `json:"canvas_title,omitempty"`
	Enabled     bool   `json:"enabled"`
	// Token 是这个项目唯一的一枚令牌：一个项目就是一个接入，不存在「令牌列表」。
	// 还没生成过（老数据）时为 nil，界面据此提示「生成令牌」。
	Token      *MCPTokenView `json:"token,omitempty"`
	LastUsedAt *string       `json:"last_used_at,omitempty"`
	CreatedAt  string        `json:"created_at"`
	UpdatedAt  string        `json:"updated_at"`
}

// MCPTokenView 是令牌列表项：只给前 8 位，拿不到明文。
type MCPTokenView struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Prefix    string `json:"prefix"`
	// HasSecret 表示这枚令牌能取回明文（当初签发时存了密文）。
	// 迁移前签发的老令牌是 false：界面只给「重新签发」，不给一个点了会报错的复制按钮。
	HasSecret  bool    `json:"has_secret"`
	ExpiresAt  string  `json:"expires_at"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
	RevokedAt  *string `json:"revoked_at,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

// MCPIssuedToken 是签发结果：明文只在这一个响应里出现。
type MCPIssuedToken struct {
	Item  *MCPTokenView `json:"item"`
	Token string        `json:"token"`
	// Endpoint 是可直接粘贴进外部工具的端点地址。
	Endpoint string `json:"endpoint"`
}

// MCPProjectReq 是后台新建 / 修改项目的请求体。字段用字符串装雪花 id，避免 JS 丢精度。
type MCPProjectReq struct {
	Name string `json:"name" binding:"required,max=64" label:"名称"`
	// OwnerUserID 只在管理员代建时给；用户自助入口固定用当前登录用户，请求体里不用带（带了也忽略）。
	OwnerUserID string `json:"owner_user_id" binding:"omitempty,max=32" label:"归属用户"`
	// CanvasCode 是**可选的默认画布**，收画布地址里那串 32 位十六进制。
	// 留空表示不设默认：调用方每次都得自己指定 canvas（多画布场景本来就要指定）。
	CanvasCode string `json:"canvas_code" binding:"omitempty,max=32" label:"默认画布"`
	// Scopes 是勾选的权限档位。这里只防滥发（数组 ≤32 项、每个值 ≤32 字符），**不写死档位数量**：
	// 合法值由 service 的 MCPScopeKnown 判，未知档位直接拒。数量上限一旦写成档位总数，
	// 每加一档就会让「全选」被 422 拦住，而且提示只报「最多 N 项」，看不出是常量落后于档位表。
	Scopes  []string `json:"scopes" binding:"omitempty,max=32,dive,max=32" label:"权限"`
	Enabled *bool    `json:"enabled" binding:"omitempty" label:"启用"`
}

// MCPProjectCreated 是新建项目的结果：项目本身 + 首枚令牌（明文只在这一个响应里出现）。
//
// 一个项目就是一个接入点，所以建项目时令牌一起签好。只建项目不给令牌的话，用户还得再点一次
// 「签发」才拿得到能粘进客户端的配置，多一层也容易漏掉。
type MCPProjectCreated struct {
	Item   *MCPProjectView `json:"item"`
	Issued *MCPIssuedToken `json:"issued"`
}

// MCPTokenIssueReq 是签发令牌的请求体：有效期 1–90 天，不允许永久令牌。
// Name 留空时用默认名（新建项目自动签发就走的这条路）；Days 留空（不传或传 0）用后台默认有效期。
type MCPTokenIssueReq struct {
	Name string `json:"name" binding:"omitempty,max=64" label:"用途"`
	Days int    `json:"days" binding:"omitempty,min=1,max=90" label:"有效期"`
}

// decodeStringList 把 jsonb 数组解成字符串切片；坏数据当空处理。
func decodeStringList(raw datatypes.JSON) []string {
	if len(raw) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// encodeStringList 把字符串切片编码成 jsonb 数组；空切片固定存成 []。
func encodeStringList(items []string) datatypes.JSON {
	if items == nil {
		items = []string{}
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return datatypes.JSON([]byte("[]"))
	}
	return datatypes.JSON(raw)
}
