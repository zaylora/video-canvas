package model

// 删除预检的阻断原因（DeleteBlocker.Kind）。
const (
	BlockerModelEnabled   = "model_enabled"   // 模型还在上架
	BlockerChannelModels  = "channel_models"  // 渠道被模型（最新草稿或已发布版本）引用
	BlockerActiveTasks    = "active_tasks"    // 非终态任务的快照引用了它
	BlockerBuiltinPlugin  = "builtin_plugin"  // 内置插件不能删除
	BlockerPluginChannels = "plugin_channels" // 插件的某个版本被渠道固定
)

// DeleteCheck 是删除预检的结果（模型 / 渠道 / 插件共用）：blockers 为空数组表示可以删除。
// 预检只给界面展示用，真正能否删除以 DELETE 接口事务内的判断为准。
type DeleteCheck struct {
	Blockers []DeleteBlocker `json:"blockers"` // 阻断原因，永远是数组
}

// DeleteBlocker 是一条阻断删除的原因。
type DeleteBlocker struct {
	Kind    string      `json:"kind"`    // 见 Blocker* 常量
	Message string      `json:"message"` // 给运营看的整句中文，带数量
	Refs    []DeleteRef `json:"refs"`    // 引用方列表，没有可列的对象时为空数组
}

// DeleteRef 是阻断删除的一个引用方。
type DeleteRef struct {
	Key  string `json:"key"`  // 引用方的 key
	Name string `json:"name"` // 展示名
}
