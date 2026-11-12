package marketing

import "video-canvas/internal/stepkit"

// RegisterSteps 把营销工作室的四个环节登记到注册表：事实卡、拆解参考片、跟拍改写、创意脚本。
// 由 initialize 在启动时调用；定义有缺陷会 panic，让协议错误在启动时暴露。
func RegisterSteps(reg *stepkit.Registry) {
	reg.MustRegister(factsStep())
	reg.MustRegister(reverseStep())
	reg.MustRegister(followStep())
	reg.MustRegister(createStep())
}
