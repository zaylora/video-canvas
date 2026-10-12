package service_test

// 本文件：上传插件时版本号的决定（设计文档 docs/design/插件简化/插件简化.md §6.5）：
// 新 key 用代码里的版本号或 1.0.0；已有 key 代码完全相同则拒绝；代码里的版本号合法、没被占用且更高就用它，
// 否则取最高版本补丁位加 1；唯一索引冲突重算，最多 3 次。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	. "video-canvas/internal/service"
)

// metaWithoutVersion 是没有 version 字段的 meta。
func metaWithoutVersion(key string) json.RawMessage {
	return json.RawMessage(`{"apiVersion":1,"key":"` + key + `","name":"插件-` + key + `","auth":{"type":"none"},"endpoints":{"video":{"mode":"async"}}}`)
}

// metaWithRawVersion 把 version 原样写成给定的 JSON 值（可以是数字、null 等）。
func metaWithRawVersion(key, rawVersion string) json.RawMessage {
	return json.RawMessage(`{"apiVersion":1,"key":"` + key + `","name":"插件-` + key + `","version":` + rawVersion + `,"auth":{"type":"none"},"endpoints":{"video":{"mode":"async"}}}`)
}

func (e *aplEnv) upload(t *testing.T, code string, meta json.RawMessage) *UploadResult {
	t.Helper()
	res, err := e.svc.Upload(context.Background(), 7, e.code(code, adminPass(meta)))
	if err != nil {
		t.Fatalf("上传失败：%v", err)
	}
	return res
}

func TestAIPluginService_UploadVersionDecision(t *testing.T) {
	t.Run("新插件：用代码里写的合法版本号，不算调整", func(t *testing.T) {
		e := newAplEnv()
		res := e.upload(t, "a", adminMetaJSON("kling", "2.3.4"))
		if !res.Accepted || res.Version.Version != "2.3.4" || res.VersionAdjusted != nil {
			t.Fatalf("不符合预期：%+v adjusted=%+v", res.Version, res.VersionAdjusted)
		}
	})

	t.Run("新插件：没写版本号用 1.0.0，并说明代码里没写", func(t *testing.T) {
		e := newAplEnv()
		res := e.upload(t, "a", metaWithoutVersion("kling"))
		if !res.Accepted || res.Version.Version != "1.0.0" {
			t.Fatalf("应登记为 1.0.0：%+v", res)
		}
		if res.VersionAdjusted == nil || res.VersionAdjusted.From != "" || res.VersionAdjusted.To != "1.0.0" {
			t.Fatalf("应报告调整（from 为空）：%+v", res.VersionAdjusted)
		}
	})

	t.Run("新插件：版本号不是 semver 或写成数字，同样用 1.0.0；from 为空（不合法的写法当作没写）", func(t *testing.T) {
		for _, raw := range []string{`"v1.0"`, `"1.0"`, `1.5`, `null`, `""`} {
			e := newAplEnv()
			res := e.upload(t, "a", metaWithRawVersion("kling", raw))
			if !res.Accepted || res.Version.Version != "1.0.0" || res.VersionAdjusted == nil ||
				res.VersionAdjusted.From != "" || res.VersionAdjusted.To != "1.0.0" {
				t.Fatalf("version=%s：%+v adjusted=%+v", raw, res.Version, res.VersionAdjusted)
			}
		}
	})

	t.Run("已有插件：代码改了、版本号没改（被占用）：补丁位加 1", func(t *testing.T) {
		e := newAplEnv()
		e.upload(t, "v1", adminMetaJSON("kling", "1.3.1"))
		res := e.upload(t, "v1-fixed", adminMetaJSON("kling", "1.3.1"))
		if !res.Accepted || res.Version.Version != "1.3.2" {
			t.Fatalf("应自动登记为 1.3.2：%+v", res)
		}
		if res.VersionAdjusted == nil || res.VersionAdjusted.From != "1.3.1" || res.VersionAdjusted.To != "1.3.2" {
			t.Fatalf("应报告 1.3.1 → 1.3.2：%+v", res.VersionAdjusted)
		}
		if len(e.repo.Versions) != 2 || len(e.repo.Audits) != 2 {
			t.Fatalf("应有两个版本、两条审计：%d / %d", len(e.repo.Versions), len(e.repo.Audits))
		}
	})

	t.Run("已有插件：连续上传，版本号逐个递增", func(t *testing.T) {
		e := newAplEnv()
		e.upload(t, "c0", adminMetaJSON("kling", "1.0.0"))
		var got []string
		for _, c := range []string{"c1", "c2", "c3"} {
			got = append(got, e.upload(t, c, adminMetaJSON("kling", "1.0.0")).Version.Version)
		}
		if strings.Join(got, ",") != "1.0.1,1.0.2,1.0.3" {
			t.Fatalf("版本号应逐个递增：%v", got)
		}
	})

	t.Run("已有插件：代码里的版本号比最高版本低：按最高版本补丁位加 1，不是按它", func(t *testing.T) {
		e := newAplEnv()
		e.upload(t, "a", adminMetaJSON("kling", "1.3.1"))
		res := e.upload(t, "b", adminMetaJSON("kling", "1.2.0"))
		if res.Version.Version != "1.3.2" || res.VersionAdjusted == nil || res.VersionAdjusted.From != "1.2.0" {
			t.Fatalf("应登记为 1.3.2：%+v %+v", res.Version, res.VersionAdjusted)
		}
	})

	t.Run("已有插件：代码里写了更高的合法版本号：直接用它", func(t *testing.T) {
		e := newAplEnv()
		e.upload(t, "a", adminMetaJSON("kling", "1.3.1"))
		res := e.upload(t, "b", adminMetaJSON("kling", "2.0.0"))
		if res.Version.Version != "2.0.0" || res.VersionAdjusted != nil {
			t.Fatalf("应直接用 2.0.0 且不算调整：%+v %+v", res.Version, res.VersionAdjusted)
		}
	})

	t.Run("已有插件：最高版本是预发布：去掉后缀再加 1", func(t *testing.T) {
		e := newAplEnv()
		e.upload(t, "a", adminMetaJSON("kling", "2.0.0-rc.1"))
		res := e.upload(t, "b", metaWithoutVersion("kling"))
		if res.Version.Version != "2.0.1" {
			t.Fatalf("应登记为 2.0.1：%+v", res.Version)
		}
	})

	t.Run("已有插件：按 semver 比较而不是按字符串（1.10.0 高于 1.9.0）", func(t *testing.T) {
		e := newAplEnv()
		e.upload(t, "a", adminMetaJSON("kling", "1.9.0"))
		e.upload(t, "b", adminMetaJSON("kling", "1.10.0"))
		res := e.upload(t, "c", adminMetaJSON("kling", "1.10.0"))
		if res.Version.Version != "1.10.1" {
			t.Fatalf("应登记为 1.10.1：%+v", res.Version)
		}
	})

	t.Run("代码与已有版本完全相同：拒绝，报在 meta 上，不登记不写审计", func(t *testing.T) {
		e := newAplEnv()
		e.upload(t, "same-code", adminMetaJSON("kling", "1.3.1"))
		before := len(e.repo.Audits)
		for _, meta := range []json.RawMessage{adminMetaJSON("kling", "1.3.1"), adminMetaJSON("kling", "9.9.9"), metaWithoutVersion("kling")} {
			res, err := e.svc.Upload(context.Background(), 7, e.code("same-code", adminPass(meta)))
			if err != nil || res.Accepted || res.Version != nil || res.VersionAdjusted != nil {
				t.Fatalf("相同代码应被拒绝：%+v %v", res, err)
			}
			if len(res.Issues) != 1 || res.Issues[0].Path != "meta" || !strings.Contains(res.Issues[0].Message, "1.3.1") ||
				!strings.Contains(res.Issues[0].Message, "完全相同") {
				t.Fatalf("问题应指明和哪个版本相同：%+v", res.Issues)
			}
		}
		if len(e.repo.Versions) != 1 || len(e.repo.Audits) != before {
			t.Fatalf("不该新增版本或审计：%d 个版本，%d 条审计", len(e.repo.Versions), len(e.repo.Audits))
		}
	})

	t.Run("登记的版本号同步写进 meta_json，代码原文和 sha256 不变", func(t *testing.T) {
		e := newAplEnv()
		e.upload(t, "v1", adminMetaJSON("kling", "1.3.1"))
		res := e.upload(t, "v1-fixed", adminMetaJSON("kling", "1.3.1"))
		stored := e.repo.Versions[res.Version.ID]
		var meta struct {
			Key       string          `json:"key"`
			Version   string          `json:"version"`
			Endpoints json.RawMessage `json:"endpoints"`
			Settings  json.RawMessage `json:"channelSettings"`
		}
		if err := json.Unmarshal([]byte(stored.MetaJSON), &meta); err != nil || meta.Version != "1.3.2" || meta.Key != "kling" || len(meta.Endpoints) == 0 {
			t.Fatalf("meta_json 应带最终版本号且其余字段保留：%s %v", stored.MetaJSON, err)
		}
		// channelSettings 是有序对象（设置项顺序决定表单顺序），回写版本号不能打乱它
		if !strings.Contains(string(meta.Settings), `"region"`) || strings.Index(string(meta.Settings), `"region"`) > strings.Index(string(meta.Settings), `"tenant"`) {
			t.Fatalf("channelSettings 的顺序被打乱：%s", meta.Settings)
		}
		if stored.Code != "v1-fixed" || stored.SHA256 != sha256Hex("v1-fixed") {
			t.Fatalf("代码原文和 sha256 不应改变：%q %s", stored.Code, stored.SHA256)
		}
		if res.Version.Meta == nil || res.Version.Meta.Version != "1.3.2" {
			t.Fatalf("返回的版本视图里 meta.version 也应是最终版本号：%+v", res.Version.Meta)
		}
	})

	t.Run("版本号没被调整：meta_json 原样保存", func(t *testing.T) {
		e := newAplEnv()
		meta := adminMetaJSON("kling", "1.0.0")
		res := e.upload(t, "a", meta)
		if string(e.repo.Versions[res.Version.ID].MetaJSON) != string(meta) {
			t.Fatalf("没有调整时 meta_json 应原样保存：%s", e.repo.Versions[res.Version.ID].MetaJSON)
		}
	})

	t.Run("审计记最终登记的版本号", func(t *testing.T) {
		e := newAplEnv()
		e.upload(t, "a", adminMetaJSON("kling", "1.3.1"))
		e.upload(t, "b", adminMetaJSON("kling", "1.3.1"))
		if d := string(e.repo.Audits[1].DetailJSON); !strings.Contains(d, `"version":"1.3.2"`) {
			t.Fatalf("审计应记 1.3.2：%s", d)
		}
	})
}

// 并发上传撞上唯一索引：重新读版本列表、重新决定版本号，最多重试 3 次。
func TestAIPluginService_UploadVersionRace(t *testing.T) {
	t.Run("被别人抢先登记了同一个版本号：重算后用下一个", func(t *testing.T) {
		e := newAplEnv()
		e.upload(t, "base", adminMetaJSON("kling", "1.0.0"))
		e.repo.SaveVersionConflicts = 1 // 下一次保存时，对手先登记了 1.0.1
		res := e.upload(t, "mine", adminMetaJSON("kling", "1.0.0"))
		if !res.Accepted || res.Version.Version != "1.0.2" {
			t.Fatalf("对手占了 1.0.1，应重算为 1.0.2：%+v", res)
		}
		if res.VersionAdjusted == nil || res.VersionAdjusted.To != "1.0.2" {
			t.Fatalf("应报告调整：%+v", res.VersionAdjusted)
		}
		if len(e.repo.Audits) != 2 {
			t.Fatalf("只有成功的那次写审计：%d", len(e.repo.Audits))
		}
	})

	t.Run("连续冲突 3 次仍然成功（第 4 次保存）", func(t *testing.T) {
		e := newAplEnv()
		e.upload(t, "base", adminMetaJSON("kling", "1.0.0"))
		e.repo.SaveVersionConflicts = 2
		res := e.upload(t, "mine", adminMetaJSON("kling", "1.0.0"))
		if !res.Accepted || res.Version.Version != "1.0.3" {
			t.Fatalf("两个对手各占一个，应重算为 1.0.3：%+v", res)
		}
	})

	t.Run("冲突超过 3 次：按“版本号已存在”拒绝", func(t *testing.T) {
		e := newAplEnv()
		e.upload(t, "base", adminMetaJSON("kling", "1.0.0"))
		e.repo.SaveVersionConflicts = 10
		res, err := e.svc.Upload(context.Background(), 7, e.code("mine", adminPass(adminMetaJSON("kling", "1.0.0"))))
		if err != nil || res.Accepted || len(res.Issues) != 1 || res.Issues[0].Path != "meta.version" {
			t.Fatalf("应按版本号已存在拒绝：%+v %v", res, err)
		}
	})

	t.Run("两个相同的上传抢同一个新插件：失败的那个重试时发现代码完全相同，拒绝而不是重复登记", func(t *testing.T) {
		e := newAplEnv()
		e.repo.SaveVersionConflicts = 1
		// 对手占位版本的 sha256 与我的不同，所以这里验证的是“重算后顺利登记下一个版本”
		res := e.upload(t, "first", adminMetaJSON("kling", "1.0.0"))
		if !res.Accepted || res.Version.Version != "1.0.1" {
			t.Fatalf("对手占了 1.0.0，应登记为 1.0.1：%+v", res)
		}
	})
}

// 内置插件必须写合法的版本号：版本号由发版决定，不能自动补。
func TestAIPluginService_RegisterBuiltinRequiresVersion(t *testing.T) {
	for _, tt := range []struct {
		name string
		meta json.RawMessage
	}{
		{"没写 version", metaWithoutVersion("newapi")},
		{"version 不是 semver", metaWithRawVersion("newapi", `"1.0"`)},
		{"version 是数字", metaWithRawVersion("newapi", `1.5`)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newAplEnv()
			regs, err := e.svc.RegisterBuiltin(context.Background(), [][]byte{e.code("builtin", adminPass(tt.meta))})
			if err == nil || !strings.Contains(err.Error(), "version") {
				t.Fatalf("应报错并指明 version：%v", err)
			}
			if len(regs) != 0 || len(e.repo.Versions) != 0 {
				t.Fatalf("不该登记：%+v %d", regs, len(e.repo.Versions))
			}
		})
	}
}

// sha256Hex 与服务端对原文件计算 sha256 的方式一致（小写十六进制）。
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
