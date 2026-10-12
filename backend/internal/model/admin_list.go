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

// ListChannelReq 是 GET /admin/ai/channels 的查询参数；关键字匹配名称 / key / 接口地址。
type ListChannelReq struct {
	AdminListReq
	Kind   string `form:"kind" binding:"omitempty,oneof=text image video audio agent" label:"能力"` // 渠道所用插件版本声明支持的生成方式
	Plugin string `form:"plugin" binding:"max=64" label:"插件"`                                     // 插件 key
	Status string `form:"status" binding:"omitempty,oneof=ok warn bad off upgrade" label:"状态"`    // 健康状态，或 upgrade（插件有更新版本）
}

// ListPluginReq 是 GET /admin/ai/plugins 的查询参数；关键字匹配名称 / key。
type ListPluginReq struct {
	AdminListReq
	Kind   string `form:"kind" binding:"omitempty,oneof=text image video audio agent" label:"能力"` // 按最新版本的声明筛
	Source string `form:"source" binding:"omitempty,oneof=builtin uploaded" label:"来源"`           // builtin / uploaded
	Status string `form:"status" binding:"omitempty,oneof=on off outdated" label:"状态"`            // on / off / outdated（有渠道固定在旧版本上）
}

// ListModelReq 是 GET /admin/ai/models 的查询参数；关键字匹配 key / 展示名 / 厂商 / 标签 / 渠道名。
type ListModelReq struct {
	AdminListReq
	Kind    string `form:"kind" binding:"omitempty,oneof=text image video audio agent" label:"类型"`
	Channel string `form:"channel" binding:"max=64" label:"渠道"`                               // 渠道 key
	Status  string `form:"status" binding:"omitempty,oneof=online broken offline" label:"状态"` // 在线 / 不可用 / 未上线
}
