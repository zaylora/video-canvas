package config_test

import (
	"os"
	"strings"
	"testing"

	. "video-canvas/internal/config"
)

// 并发上限、初始积分、登录记录保留天数已改为后台「注册设置」的库值 + service 里的代码常量，
// 默认配置文件里不应再出现这些键，且文件仍能正常加载。
func TestDefaultConfig_NoRemovedKeys(t *testing.T) {
	const path = "../../../configs/config.yaml"
	if _, err := Load(path); err != nil {
		t.Fatalf("默认配置应能加载：%v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"max_active_tasks_per_user", "initial_credits", "login_log_retention_days", "\nuser:"} {
		if strings.Contains(string(raw), key) {
			t.Errorf("默认配置不应再包含 %q", key)
		}
	}
}
