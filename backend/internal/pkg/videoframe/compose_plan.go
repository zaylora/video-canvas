package videoframe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// rangeSlackMS 入出点超出素材实际时长多少毫秒以内算误差，收回到素材末尾；再多就是越界。
const rangeSlackMS = 150

// ComposeOverlay 叠加轨上的一段画面（视频或图片）。
type ComposeOverlay struct {
	Path  string
	Image bool
	// StartMS 在成片里的起点；InMS / OutMS 视频是素材内的位置，图片只看 OutMS（即时长）
	StartMS, InMS, OutMS int64
	// Muted 不要它自带的声音；Volume 音量百分比（0 表示 100%）
	Muted  bool
	Volume int
	// FadeInMS / FadeOutMS 淡入淡出，同时作用于透明度和声音
	FadeInMS, FadeOutMS int64
	// X / Y 画面中心点的位置（占成片宽高的百分比）；Scale 画面宽度占成片宽度的百分比（0 按 100）；Opacity 不透明度百分比
	X, Y, Scale, Opacity int
}

// ComposeAudio 音频轨上的一段声音。
type ComposeAudio struct {
	Path string
	// StartMS 在成片里的起点；InMS / OutMS 是素材内的位置
	StartMS, InMS, OutMS int64
	// Volume 音量百分比（0 表示 100%，静音的片段由调用方直接不传）
	Volume              int
	FadeInMS, FadeOutMS int64
}

// ComposePlan 一集的全部轨：主轨按顺序拼接，叠加轨按从下到上的顺序叠在上面，音频轨混音，字幕轨烧进画面。
type ComposePlan struct {
	Main     []ComposeClip
	Overlays []ComposeOverlay
	Audios   []ComposeAudio
	Texts    []ComposeText
	// Spec 输出规格（短边、帧率、画质）；零值即默认，与指定规格之前的行为一致
	Spec ComposeSpec
}

// hasAttached 有没有任何附属轨内容。
func (p ComposePlan) hasAttached() bool {
	return len(p.Overlays)+len(p.Audios)+len(p.Texts) > 0
}

// ComposeTimeline 合成多轨成片：先把主轨合成统一规格的底片，再叠加画面、混音、烧字幕。
// 成片总长是全部轨的最晚结束点，主轨比它短时尾部补黑画面和静音。没有附属轨时等同 ComposeClips。
// 整个操作受 Options.Timeout 限制，失败清理半成片，所有路径清理中间文件。
func (e *Extractor) ComposeTimeline(ctx context.Context, plan ComposePlan, ratio, destination string) (err error) {
	if !plan.hasAttached() {
		return e.composeClipsSpec(ctx, plan.Main, ratio, plan.Spec, destination)
	}
	if len(plan.Main) == 0 {
		return fmt.Errorf("%w: 没有待合成视频", ErrDecode)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	dir, err := os.MkdirTemp(filepath.Dir(destination), "vc-tracks-*")
	if err != nil {
		return fmt.Errorf("创建合成临时目录失败：%w", err)
	}
	defer func() {
		_ = os.RemoveAll(dir)
		if err != nil {
			_ = os.Remove(destination)
		}
	}()
	base := filepath.Join(dir, "base.mp4")
	target, baseSecs, err := e.composeBase(ctx, dir, plan.Main, ratio, plan.Spec, base)
	if err != nil {
		return err
	}
	g, err := e.graphOf(ctx, plan, target, int64(baseSecs*1000+0.5))
	g.spec = plan.Spec.withDefaults()
	if err != nil {
		return err
	}
	if len(plan.Texts) > 0 {
		g.assPath = filepath.Join(dir, "captions.ass")
		if err := os.WriteFile(g.assPath, []byte(BuildASS(plan.Texts, target.width, target.height)), 0600); err != nil {
			return fmt.Errorf("写入字幕失败：%w", err)
		}
	}
	scriptPath := filepath.Join(dir, "graph.txt")
	script := g.script()
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return fmt.Errorf("写入滤镜图失败：%w", err)
	}
	return e.renderGraph(ctx, base, g, scriptPath, script, destination)
}

// graphOf 探测所有附属素材、校验入出点，给每个输入编号，算出成片总长。
func (e *Extractor) graphOf(ctx context.Context, plan ComposePlan, target composeTarget, baseMS int64) (graphSpec, error) {
	g := graphSpec{target: target, baseMS: baseMS, totalMS: baseMS}
	next := 1
	for _, o := range plan.Overlays {
		info, err := e.composeProbe(ctx, o.Path, o.Image)
		if err != nil {
			return g, err
		}
		lenMS, err := attachedRange(&o.InMS, &o.OutMS, info.duration, o.Image)
		if err != nil {
			return g, err
		}
		g.overlays = append(g.overlays, graphOverlay{idx: next, c: o, lenMS: lenMS, audio: info.audio && !o.Image && !o.Muted})
		g.totalMS = max(g.totalMS, o.StartMS+lenMS)
		next++
	}
	for _, a := range plan.Audios {
		dur, err := e.composeProbeAudio(ctx, a.Path)
		if err != nil {
			return g, err
		}
		lenMS, err := attachedRange(&a.InMS, &a.OutMS, dur, false)
		if err != nil {
			return g, err
		}
		g.audios = append(g.audios, graphAudio{idx: next, c: a, lenMS: lenMS})
		g.totalMS = max(g.totalMS, a.StartMS+lenMS)
		next++
	}
	for _, t := range plan.Texts {
		g.totalMS = max(g.totalMS, t.StartMS+t.LenMS)
	}
	return g, nil
}

// attachedRange 校验附属素材的入出点并返回它在成片里的时长（毫秒）：图片只看出点（即时长）；
// 音视频入点不能为负、出点必须晚于入点，超出素材实际时长一点点收回到末尾，超出更多算越界。
// 素材时长未知（durationSec 为 0）时不检查上限。
func attachedRange(inMS, outMS *int64, durationSec float64, image bool) (int64, error) {
	if image {
		*inMS = 0
		if *outMS <= 0 {
			return 0, fmt.Errorf("%w: 图片段没有时长", ErrDecode)
		}
		return *outMS, nil
	}
	if *inMS < 0 || *outMS <= *inMS {
		return 0, fmt.Errorf("%w: 入出点不合法", ErrDecode)
	}
	if durMS := int64(durationSec * 1000); durMS > 0 {
		if *outMS > durMS+rangeSlackMS {
			return 0, fmt.Errorf("%w: 入出点超出素材长度", ErrDecode)
		}
		*outMS = min(*outMS, durMS)
	}
	if *outMS-*inMS <= 0 {
		return 0, fmt.Errorf("%w: 入出点不合法", ErrDecode)
	}
	return *outMS - *inMS, nil
}

// composeProbeAudio 读音频素材的时长（秒），没有音轨算无法解码。
func (e *Extractor) composeProbeAudio(ctx context.Context, source string) (float64, error) {
	out, err := e.run(ctx, e.ffprobe, "-v", "error", "-protocol_whitelist", "file", "-show_entries", "stream=codec_type:format=duration", "-of", "json", source)
	if err != nil {
		return 0, err
	}
	var probe struct {
		Streams []struct {
			Type string `json:"codec_type"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		return 0, fmt.Errorf("%w: 音频信息无法解析", ErrDecode)
	}
	for _, s := range probe.Streams {
		if s.Type == "audio" {
			d, _ := strconv.ParseFloat(probe.Format.Duration, 64)
			return d, nil
		}
	}
	return 0, fmt.Errorf("%w: 素材没有音轨", ErrDecode)
}

// renderGraph 用底片和各附属输入跑一遍滤镜图，编码成最终成片。
// 滤镜图放在文件里（图可能很长）：ffmpeg 7 起用 -/filter_complex，旧版本只认 -filter_complex_script，先试新的，不认再退回。
func (e *Extractor) renderGraph(ctx context.Context, base string, g graphSpec, scriptPath, script, destination string) error {
	args := []string{"-nostdin", "-v", "error", "-protocol_whitelist", "file", "-i", base}
	for _, o := range g.overlays {
		args = append(args, overlayInputArgs(o, g.spec.FPS)...)
	}
	for _, a := range g.audios {
		args = append(args, trimmedInputArgs(a.c.InMS, g.lenOf(a), a.c.Path)...)
	}
	tail := []string{"-map", "[vout]", "-map", "[aout]",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", strconv.Itoa(g.spec.CRF), "-pix_fmt", "yuv420p",
		"-r", strconv.Itoa(g.spec.FPS),
		"-c:a", "aac", "-ar", "48000", "-ac", "2", "-t", secs(g.totalMS), "-movflags", "+faststart", "-y", destination}
	var err error
	for _, filter := range []struct {
		option string
		value  string
	}{
		{option: "-/filter_complex", value: scriptPath},
		{option: "-filter_complex", value: script},
	} {
		all := append(append(append([]string{}, args...), filter.option, filter.value), tail...)
		if _, err = e.run(ctx, e.ffmpeg, all...); err == nil || !unknownOption(err) {
			return err
		}
	}
	return err
}

// lenOf 音频输入在成片里的时长。
func (graphSpec) lenOf(a graphAudio) int64 { return a.lenMS }

// unknownOption ffmpeg 报「不认识这个选项」。
func unknownOption(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "Unrecognized option") || strings.Contains(msg, "Option not found")
}

// overlayInputArgs 叠加画面的输入参数：图片按成片帧率循环成固定时长，视频按入点和时长截取。
func overlayInputArgs(o graphOverlay, fps int) []string {
	if o.c.Image {
		return []string{"-loop", "1", "-framerate", strconv.Itoa(fps), "-t", secs(o.lenMS), "-i", o.c.Path}
	}
	return trimmedInputArgs(o.c.InMS, o.lenMS, o.c.Path)
}

// trimmedInputArgs 从入点起截取 lenMS 毫秒的输入参数。
func trimmedInputArgs(inMS, lenMS int64, path string) []string {
	args := []string{}
	if inMS > 0 {
		args = append(args, "-ss", secs(inMS))
	}
	return append(args, "-t", secs(lenMS), "-i", path)
}
