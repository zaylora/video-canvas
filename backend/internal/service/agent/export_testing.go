package agent

import "io"

// ValidateRunContextForTest 暴露上下文校验给外部测试包。
func ValidateRunContextForTest(rc RunContext) error { return rc.validate() }

// SetAdminOrderRandomForTest 在发起请求前替换实例随机源，用于确定性复现单号碰撞和读取失败。
func SetAdminOrderRandomForTest(s *AdminAssistantService, random io.Reader) { s.orderRandom = random }
