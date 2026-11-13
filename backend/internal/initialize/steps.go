package initialize

import (
	"video-canvas/internal/marketing"
	"video-canvas/internal/stepkit"
)

// registerSteps 把各业务包的环节登记到注册表。
//
// 环节协议层（stepkit）本身不带任何业务环节：营销工作室的四个环节（marketing.facts / reverse / follow / create）在这里登记；
// 短剧以后迁移时同样在这里加一行。登记的环节自动出现在后台「环节技能」页，不需要再改路由和接口。
func registerSteps(reg *stepkit.Registry) {
	marketing.RegisterSteps(reg)
}
