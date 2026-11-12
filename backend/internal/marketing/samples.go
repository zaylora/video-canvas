package marketing

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/stepkit"
)

// sampleFS 是后台试跑样例的素材：商品图、10 秒参考片、兜底截帧，以及样例自带的事实卡与拆解 JSON。
//
//go:embed samples
var sampleFS embed.FS

// 样例参考片的信息：cream_ref.mp4 是 10 秒的护手霜带货片，软管上印着第三方品牌 HERALIFE。
const (
	creamDuration   = 10.0 // 秒
	creamFrameCount = 8    // cream_f1.jpg … cream_f8.jpg
)

// creamCuts 是样例参考片的镜头切点（秒），由 ffmpeg 场景检测核对。
var creamCuts = []float64{2.5, 5, 7.5}

// 三组样例的轮次设置。
var (
	perfumeBrief = Brief{Platform: "douyin", Language: LangZH, Duration: 15, AspectRatio: "9:16", Description: "木质调，留香 8 小时左右，适合通勤"}
	blenderBrief = Brief{Platform: "tiktok", Language: LangEN, Duration: 15, AspectRatio: "9:16", Description: "Portable USB-C rechargeable blender for smoothies on the go"}
	creamBrief   = Brief{Platform: "douyin", Language: LangZH, Duration: 10, AspectRatio: "9:16", Description: "质地清爽，容易推开"}
)

// sampleBytes 读取样例素材。
func sampleBytes(name string) ([]byte, error) {
	raw, err := sampleFS.ReadFile("samples/" + name)
	if err != nil {
		return nil, fmt.Errorf("读取样例素材 %s 失败：%w", name, err)
	}
	return raw, nil
}

// sampleImage 读取 jpeg 样例图，返回 data URL。
func sampleImage(name string) (string, error) {
	raw, err := sampleBytes(name)
	if err != nil {
		return "", err
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(raw), nil
}

// sampleJSON 读取样例自带的 JSON（事实卡、拆解）。
func sampleJSON[T any](name string) (T, error) {
	var out T
	raw, err := sampleBytes(name)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("解析样例 %s 失败：%w", name, err)
	}
	return out, nil
}

// factsSamples 是商品事实卡的试跑样例：看图，不需要参考片。
func factsSamples() []stepkit.Sample {
	build := func(brief Brief, file string) func(context.Context) (stepkit.Input, error) {
		return func(context.Context) (stepkit.Input, error) {
			img, err := sampleImage(file)
			if err != nil {
				return stepkit.Input{}, err
			}
			return FactsInput(brief, []string{img}), nil
		}
	}
	return []stepkit.Sample{
		{Key: "perfume", Name: "晨雾香水 · 抖音 · 无参考", Build: build(perfumeBrief, "perfume.jpg")},
		{Key: "blender", Name: "便携榨汁杯 · TikTok · 英文", Build: build(blenderBrief, "blender.jpg")},
	}
}

// reverseSamples 是拆解参考片的试跑样例：视频内联 Base64，另带 8 帧截帧，模型不支持看视频时协议层改用截帧。
func reverseSamples() []stepkit.Sample {
	return []stepkit.Sample{{
		Key: "cream", Name: "护手霜 · 抖音 · 带 10 秒参考片", NeedsRef: true,
		Build: func(context.Context) (stepkit.Input, error) {
			raw, err := sampleBytes("cream_ref.mp4")
			if err != nil {
				return stepkit.Input{}, err
			}
			video := &llmgateway.Video{MimeType: "video/mp4", Data: base64.StdEncoding.EncodeToString(raw)}
			frames := make([]string, 0, creamFrameCount)
			for i := 1; i <= creamFrameCount; i++ {
				f, err := sampleImage(fmt.Sprintf("cream_f%d.jpg", i))
				if err != nil {
					return stepkit.Input{}, err
				}
				frames = append(frames, f)
			}
			return ReverseInput(creamBrief, creamDuration, creamCuts, video, frames), nil
		},
	}}
}

// followSamples 是跟拍改写的试跑样例：自带一份拆解（含第三方品牌 HERALIFE）和「我方护手霜」事实卡，只测本环节。
func followSamples() []stepkit.Sample {
	return []stepkit.Sample{{
		Key: "cream", Name: "护手霜 · 抖音 · 带 10 秒参考片", NeedsRef: true,
		Build: func(context.Context) (stepkit.Input, error) {
			rev, facts, err := creamBundle()
			if err != nil {
				return stepkit.Input{}, err
			}
			return FollowInput(creamBrief, facts, rev, AssetNote{ProductImages: 1}), nil
		},
	}}
}

// createSamples 是创意脚本的试跑样例：perfume、blender 无参考，cream 带参考拆解、角度自动。
// 事实卡都随样例自带，不在样例里调模型。
func createSamples() []stepkit.Sample {
	plain := func(brief Brief, factsFile string, angles []string) func(context.Context) (stepkit.Input, error) {
		return func(context.Context) (stepkit.Input, error) {
			facts, err := sampleJSON[ProductFacts](factsFile)
			if err != nil {
				return stepkit.Input{}, err
			}
			return CreateInput(brief, facts, angles, len(angles), nil), nil
		}
	}
	return []stepkit.Sample{
		{Key: "perfume", Name: "晨雾香水 · 抖音 · 无参考", Build: plain(perfumeBrief, "perfume_facts.json", []string{"pain", "lifestyle"})},
		{Key: "blender", Name: "便携榨汁杯 · TikTok · 英文", Build: plain(blenderBrief, "blender_facts.json", []string{"feature", "unbox"})},
		{
			Key: "cream", Name: "护手霜 · 抖音 · 带 10 秒参考片", NeedsRef: true,
			Build: func(context.Context) (stepkit.Input, error) {
				rev, facts, err := creamBundle()
				if err != nil {
					return stepkit.Input{}, err
				}
				return CreateInput(creamBrief, facts, nil, 2, &rev), nil
			},
		},
	}
}

// creamBundle 读取护手霜样例自带的拆解与我方事实卡。
func creamBundle() (Reverse, ProductFacts, error) {
	rev, err := sampleJSON[Reverse]("cream_reverse.json")
	if err != nil {
		return Reverse{}, ProductFacts{}, err
	}
	facts, err := sampleJSON[ProductFacts]("cream_facts.json")
	if err != nil {
		return Reverse{}, ProductFacts{}, err
	}
	return rev, facts, nil
}
