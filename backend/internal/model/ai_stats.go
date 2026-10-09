package model

// AIStatsDay 是总览页任务量柱形图里一天的任务数（按创建时间分天，试跑不算）。
// 三个桶合起来就是当天创建的全部正式任务，页面上所有数字都用这个口径。
type AIStatsDay struct {
	Date      string `json:"date"`      // YYYY-MM-DD，按统计时区的自然日
	Succeeded int    `json:"succeeded"` // 状态为 succeeded
	Failed    int    `json:"failed"`    // 状态为 failed 或 expired
	Other     int    `json:"other"`     // 已取消，以及仍在进行中（pending / queued / running / finalizing）
}

// AIStatsModelKind 是按（模型, 类型）分组的任务数，仓储层的一行；服务层再折成按模型、按类型两张表。
type AIStatsModelKind struct {
	Model string `json:"model"` // 模型 key（generation_tasks.model_id）
	Kind  string `json:"kind"`  // 任务类型：video / image / audio / text
	Count int    `json:"count"` // 任务数
}

// AIStatsModelCount 是一个模型的调用数。
type AIStatsModelCount struct {
	Model string `json:"model"` // 模型 key
	Label string `json:"label"` // 展示名；模型已被删除或没有展示名时回退为 key
	Count int    `json:"count"` // 区间内的任务数
}

// AIStatsKindCount 是一种任务类型的调用数。
type AIStatsKindCount struct {
	Kind  string `json:"kind"`  // video / image / audio / text
	Count int    `json:"count"` // 区间内的任务数
}

// AIStatsView 是 GET /admin/ai/stats 的响应：总览页的柱形图与占比图数据。
type AIStatsView struct {
	Days    int                 `json:"days"`     // 统计天数（7 或 30）
	Daily   []AIStatsDay        `json:"daily"`    // 恒为 Days 项，日期升序，没有任务的日子补 0，最后一项是今天
	ByModel []AIStatsModelCount `json:"by_model"` // 按任务数降序的全量模型，前端取前几名
	ByKind  []AIStatsKindCount  `json:"by_kind"`  // 按任务数降序
}

// AIStatsReq 是 GET /admin/ai/stats 的查询参数。
type AIStatsReq struct {
	Days int `form:"days" binding:"omitempty,oneof=7 30" label:"统计天数"` // 7 或 30，不传按 7
}
