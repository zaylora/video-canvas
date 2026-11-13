package service_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"video-canvas/internal/config"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/plugins"
)

type imagePluginCode string

func (s imagePluginCode) Code(context.Context, uint64, string) (string, error) { return string(s), nil }

type imagePluginSecret struct{}

func (imagePluginSecret) Get(context.Context, string) (string, error) { return "test-key", nil }

// 真正的素材服务处理插件内联图片，并遵循存储配置的大小上限。
func TestOpenAIImageRealAssetService(t *testing.T) {
	image := assetTestPNG(t, 3, 3)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"data":[{"b64_json":"%s"}]}`, base64.StdEncoding.EncodeToString(image))
	}))
	defer upstream.Close()
	src, err := plugins.Source("openai-image")
	if err != nil {
		t.Fatal(err)
	}
	runner := plugin.NewInProcessRunnerClient(pluginrunner.NewServer(pluginrunner.Options{}).Handler())
	pre, err := runner.Precheck(context.Background(), string(src))
	if err != nil || !pre.OK {
		t.Fatalf("预检失败：%+v %v", pre, err)
	}
	meta, err := pluginmeta.Parse(pre.Meta)
	if err != nil {
		t.Fatal(err)
	}
	snap := &provider.Snapshot{Model: provider.ModelSnapshot{Key: "image", Kind: "image", UpstreamModel: "gpt-image-1", Capabilities: modelcfg.Capabilities{Prompt: modelcfg.PromptSpec{MaxLength: 1000}}}, Channel: provider.ChannelSnapshot{Key: "channel", PluginKey: "openai-image", PluginVersionID: 1, BaseURL: upstream.URL}, Plugin: provider.PluginSnapshot{Key: "openai-image", SHA256: pre.SHA256, Meta: *meta}, ModelRevisionID: 1}
	for _, tc := range []struct {
		name     string
		limit    int64
		succeeds bool
	}{{"success", int64(len(image)), true}, {"configured limit", int64(len(image) - 1), false}} {
		t.Run(tc.name, func(t *testing.T) {
			saver, repo, _ := newAssetTestService(config.Storage{MaxResult: tc.limit})
			exec := plugin.New(plugin.Options{Runner: runner, Codes: imagePluginCode(src), Secrets: imagePluginSecret{}, Saver: saver, IPAllowed: func(ip net.IP) bool { return ip.IsLoopback() }})
			sub, err := exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 42, UserID: 3}, Input: map[string]any{"prompt": "test"}})
			if !tc.succeeds {
				if err == nil || len(repo.rows) != 0 {
					t.Fatalf("超过管理员上限时不得保存：%v", err)
				}
				return
			}
			if err != nil || sub.Immediate == nil || len(sub.Immediate.Outputs) != 1 {
				t.Fatalf("保存失败：%+v %v", sub, err)
			}
			out := sub.Immediate.Outputs[0]
			asset := repo.rows[out.AssetID]
			if out.Type != provider.OutputAsset || asset == nil || asset.UserID != 3 || asset.TaskID == nil || *asset.TaskID != 42 || asset.MimeType != "image/png" || asset.Width != 3 || asset.Height != 3 {
				t.Fatalf("素材信息不符：%+v %+v", out, asset)
			}
		})
	}
}
