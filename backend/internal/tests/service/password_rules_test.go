package service_test

import (
	"strings"
	"testing"

	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service"
)

// 本文件测试统一密码规则 validateNewPassword：注册、个人中心改密码、后台手填重置三处共用。

func TestPasswordRules_ValidateNewPassword(t *testing.T) {
	tests := []struct {
		name     string
		username string
		pw       string
		wantCode int // 0 表示通过
		wantMsg  string
	}{
		{"8 字节的普通密码通过", "alice", "k9#Tz!q2", 0, ""},
		{"72 字节（上限）通过", "alice", strings.Repeat("x7", 36), 0, ""},
		{"24 个汉字恰好 72 字节通过", "alice", strings.Repeat("密", 24), 0, ""},
		{"7 位太短：10001", "alice", "k9#Tz!q", errcode.ErrInvalidParams.Code, "密码至少 8 位"},
		{"空串太短：10001", "alice", "", errcode.ErrInvalidParams.Code, "密码至少 8 位"},
		{"73 字节太长：10001", "alice", strings.Repeat("a", 73), errcode.ErrInvalidParams.Code, "密码最多 72 字节（约 24 个汉字或 72 个英文字符）"},
		{"25 个汉字 75 字节太长：10001", "alice", strings.Repeat("密", 25), errcode.ErrInvalidParams.Code, ""},
		{"等于用户名：55003", "alice_2026", "alice_2026", errcode.ErrPasswordWeak.Code, "密码不能与用户名相同"},
		{"等于用户名（大小写不同也算）：55003", "Alice_2026", "alice_2026", errcode.ErrPasswordWeak.Code, "密码不能与用户名相同"},
		{"命中弱密码表：12345678", "alice", "12345678", errcode.ErrPasswordWeak.Code, ""},
		{"命中弱密码表：password", "alice", "password", errcode.ErrPasswordWeak.Code, ""},
		{"弱密码表不区分大小写：PassWord1", "alice", "PassWord1", errcode.ErrPasswordWeak.Code, ""},
		{"命中弱密码表：qwertyuiop", "alice", "qwertyuiop", errcode.ErrPasswordWeak.Code, ""},
		{"命中弱密码表：1qaz2wsx", "alice", "1qaz2wsx", errcode.ErrPasswordWeak.Code, ""},
		{"命中弱密码表：woaini1314", "alice", "woaini1314", errcode.ErrPasswordWeak.Code, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNewPassword(tt.username, tt.pw)
			if tt.wantCode == 0 {
				if err != nil {
					t.Fatalf("应通过：%v", err)
				}
				return
			}
			ec := bizErrOf(t, err, &errcode.Error{Code: tt.wantCode})
			if tt.wantMsg != "" && ec.Msg != tt.wantMsg {
				t.Fatalf("Msg = %q，期望 %q", ec.Msg, tt.wantMsg)
			}
		})
	}
}

func TestPasswordRules_WeakListSize(t *testing.T) {
	// 设计约定弱密码表约 1000 条；条目全部是小写、去重、且长度落在 8..72 字节（短于 8 的会先被长度规则拦住，放进表里没有意义）
	n := WeakPasswordCount()
	if n < 900 || n > 1500 {
		t.Fatalf("弱密码表条数 = %d，应约 1000 条", n)
	}
}
