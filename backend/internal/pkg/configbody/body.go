// Package configbody 为配置快照提供稳定 JSON 摘要，避免空白与键顺序引起伪冲突。
package configbody

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// SHA256 计算 JSON 值的规范化摘要；调用方只传入已经校验的 JSON。
func SHA256(raw []byte) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// ModelSHA256 将实际启用状态纳入快照，防止审批覆盖正文未变的启停操作。
func ModelSHA256(raw []byte, enabled bool) string {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return ""
	}
	value["enabled"] = enabled
	body, _ := json.Marshal(value)
	return SHA256(body)
}
