package model

// AdminListReq 是后台列表接口共用的查询参数：关键字 + 分页。
// 各列表的筛选条件在自己的请求结构里内嵌它再追加。
type AdminListReq struct {
	Q        string `form:"q" binding:"max=64" label:"关键字"` // 模糊匹配，各列表自己定义匹配哪些字段
	Page     int    `form:"page" label:"页码"`                // 页码，从 1 开始
	PageSize int    `form:"page_size" label:"每页条数"`         // 每页条数，默认 20，最大 100
}

// ListStorageReq 是 GET /admin/storages 的查询参数；关键字匹配名称 / 服务商 / 桶 / endpoint。
type ListStorageReq struct {
	AdminListReq
}

// ListProcessorReq 是 GET /admin/image-processors 的查询参数；关键字匹配名称 / 厂商 / 绑定的存储名。
type ListProcessorReq struct {
	AdminListReq
	Status string `form:"status" binding:"omitempty,oneof=draft published disabled" label:"状态"` // draft / published / disabled
}

// ListSkillReq 是 GET /admin/agent/skills 的查询参数；关键字匹配技能名 / 显示名 / 说明。
type ListSkillReq struct {
	AdminListReq
	Status string `form:"status" binding:"omitempty,oneof=enabled disabled builtin" label:"状态"` // enabled / disabled / builtin
}
