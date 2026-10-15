package plugin_test

import (
	"context"
	"regexp"
	"testing"

	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/plugins"
)

var builtinSemver = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// 全部内置插件都能通过 runner 预检：meta.key 等于文件名（plugins.Source 靠它取源码）、key 不重复、
// 版本号是合法 semver（内置插件不走自动调版本，写错启动登记会报错）、每个插件只管一种类型。
func TestAllBuiltinPluginsPassPrecheck(t *testing.T) {
	sources, err := plugins.Builtin()
	if err != nil || len(sources) == 0 {
		t.Fatalf("读取内置插件失败：%v", err)
	}
	runner := plugin.NewInProcessRunnerClient(pluginrunner.NewServer(pluginrunner.Options{}).Handler())
	seen := map[string]bool{}
	for _, src := range sources {
		pre, err := runner.Precheck(context.Background(), string(src))
		if err != nil || !pre.OK {
			t.Fatalf("内置插件预检没有通过：%+v %v", pre.Issues, err)
		}
		meta, err := pluginmeta.Parse(pre.Meta)
		if err != nil {
			t.Fatal(err)
		}
		if seen[meta.Key] {
			t.Fatalf("内置插件 key 重复：%s", meta.Key)
		}
		seen[meta.Key] = true
		byKey, err := plugins.Source(meta.Key)
		if err != nil || string(byKey) != string(src) {
			t.Fatalf("内置插件 %s 的文件名应等于 meta.key：%v", meta.Key, err)
		}
		if len(meta.Endpoints) != 1 {
			t.Fatalf("内置插件 %s 应只支持一种类型，实际 %v", meta.Key, meta.Endpoints)
		}
		if !builtinSemver.MatchString(meta.Version) {
			t.Fatalf("内置插件 %s 的版本号不是合法 semver：%q", meta.Key, meta.Version)
		}
		// 模型弹窗的协议列表要显示厂商和请求路径（渠道模型重构）：内置插件都声明了，版本统一 1.1.0
		if meta.Version != "1.1.0" || meta.Vendor == "" {
			t.Fatalf("内置插件 %s 应是 1.1.0 并声明 vendor：version=%q vendor=%q", meta.Key, meta.Version, meta.Vendor)
		}
		for kind, ep := range meta.Endpoints {
			if ep.Method == "" || ep.Path == "" {
				t.Fatalf("内置插件 %s 的 %s endpoint 应声明展示用的 method 与 path：%+v", meta.Key, kind, ep)
			}
		}
	}
	if len(seen) < 18 {
		t.Fatalf("内置插件应有 18 个以上，实际 %d：%v", len(seen), seen)
	}
}
