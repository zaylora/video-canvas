package pluginmeta_test

// 本文件：插件版本号的辅助函数（设计文档 docs/design/插件简化/插件简化.md §6.5）：合法性、semver 比较、补丁位递增。

import (
	"strings"
	"testing"

	"video-canvas/internal/provider/pluginmeta"
)

func TestIsValidVersion(t *testing.T) {
	tests := []struct {
		v    string
		want bool
	}{
		{"1.0.0", true}, {"0.0.1", true}, {"10.20.30", true}, {"2.0.0-rc.1", true}, {"1.0.0-alpha", true},
		{"", false}, {"1.0", false}, {"v1.0.0", false}, {"1.0.0.0", false}, {"01.0.0", false}, {"1.0.0-", false},
		{"1.0.0-" + strings.Repeat("a", 30), false}, // 超过 32 个字符
		{" 1.0.0", false},
	}
	for _, tt := range tests {
		if got := pluginmeta.IsValidVersion(tt.v); got != tt.want {
			t.Errorf("IsValidVersion(%q) = %v，期望 %v", tt.v, got, tt.want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	// 每一行从小到大：前一个都小于后一个（semver 2.0 的优先级规则，含预发布）
	ordered := []string{"0.9.9", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2",
		"1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "1.10.0", "2.0.0"}
	for i := range ordered {
		for j := range ordered {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := pluginmeta.CompareVersions(ordered[i], ordered[j]); got != want {
				t.Errorf("CompareVersions(%q, %q) = %d，期望 %d", ordered[i], ordered[j], got, want)
			}
		}
	}
}

func TestHighestVersion(t *testing.T) {
	if got := pluginmeta.HighestVersion([]string{"1.3.0", "1.10.0", "1.9.9", "1.10.0-rc.1"}); got != "1.10.0" {
		t.Fatalf("最高版本应是 1.10.0，实际 %q", got)
	}
	if got := pluginmeta.HighestVersion(nil); got != "" {
		t.Fatalf("没有版本时应返回空串：%q", got)
	}
	// 库里混进了不合法的版本号：忽略它们，不能因此崩溃或选错
	if got := pluginmeta.HighestVersion([]string{"bad", "1.0.0", "x.y.z"}); got != "1.0.0" {
		t.Fatalf("应忽略不合法的版本：%q", got)
	}
}

func TestNextPatchVersion(t *testing.T) {
	tests := []struct {
		from, want string
	}{
		{"1.3.1", "1.3.2"},
		{"0.0.0", "0.0.1"},
		{"1.9.9", "1.9.10"},
		{"2.0.0-rc.1", "2.0.1"}, // 去掉预发布后缀
		{"1.0.0-alpha", "1.0.1"},
	}
	for _, tt := range tests {
		got, err := pluginmeta.NextPatchVersion(tt.from)
		if err != nil || got != tt.want {
			t.Errorf("NextPatchVersion(%q) = %q, %v，期望 %q", tt.from, got, err, tt.want)
		}
	}
	for _, bad := range []string{"", "1.0", "v1.0.0", "abc"} {
		if _, err := pluginmeta.NextPatchVersion(bad); err == nil {
			t.Errorf("NextPatchVersion(%q) 应报错", bad)
		}
	}
	// 数字太大放不进 uint64：报错而不是回绕
	if _, err := pluginmeta.NextPatchVersion("1.0.18446744073709551615"); err == nil {
		t.Error("补丁位溢出应报错")
	}
	if _, err := pluginmeta.NextPatchVersion("1.0.99999999999999999999999"); err == nil {
		t.Error("数字超出范围应报错")
	}
}

// Parse 读取预检返回的原始 meta：version 写成数字、null 等非字符串时当作“没写”，不能因此解析失败
// （预检已经放行了这种写法，上传时由服务层补版本号）；其他字段的类型错误仍然报错。
func TestParse_VersionNotAString(t *testing.T) {
	base := func(version string) []byte {
		return []byte(`{"apiVersion":1,"key":"k","name":"n"` + version + `,"auth":{"type":"none"},"endpoints":{"text":{"mode":"sync"}}}`)
	}
	for _, tt := range []struct {
		name, version, want string
	}{
		{"字符串", `,"version":"1.2.3"`, "1.2.3"},
		{"不合法的字符串原样保留", `,"version":"v1"`, "v1"},
		{"数字", `,"version":1.5`, ""},
		{"整数", `,"version":2`, ""},
		{"null", `,"version":null`, ""},
		{"对象", `,"version":{"a":1}`, ""},
		{"缺失", ``, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, err := pluginmeta.Parse(base(tt.version))
			if err != nil {
				t.Fatalf("version 的类型不该导致解析失败：%v", err)
			}
			if m.Version != tt.want || m.Key != "k" || m.Endpoints["text"].Mode != pluginmeta.ModeSync {
				t.Fatalf("解析结果不对：%+v", m)
			}
		})
	}
	if _, err := pluginmeta.Parse([]byte(`{"apiVersion":"1","key":"k"}`)); err == nil {
		t.Fatal("其他字段的类型错误仍应报错")
	}
	if _, err := pluginmeta.Parse([]byte(`not json`)); err == nil {
		t.Fatal("不是 JSON 应报错")
	}
}
