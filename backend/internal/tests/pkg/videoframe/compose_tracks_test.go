package videoframe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/pkg/videoframe"
)

// needFFmpeg 没有 ffmpeg / ffprobe 的环境跳过，不假通过。
func needFFmpeg(t *testing.T) *videoframe.Extractor {
	t.Helper()
	e := videoframe.New(videoframe.Options{Timeout: 2 * time.Minute})
	if !e.Available() {
		t.Skip("本机没有 ffmpeg / ffprobe，跳过多轨合成测试")
	}
	return e
}

// needASS 当前 ffmpeg 没有 ass 滤镜（没带 libass）时跳过字幕相关测试。
func needASS(t *testing.T) {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), "ffmpeg", "-hide_banner", "-filters").Output()
	if err != nil || !strings.Contains(string(out), " ass ") {
		t.Skip("当前 ffmpeg 没有 ass 滤镜（缺 libass），跳过字幕测试")
	}
}

// toneFile 生成一段正弦波音频文件。
func toneFile(t *testing.T, dir, name, seconds string) string {
	t.Helper()
	path := filepath.Join(dir, name+".wav")
	args := []string{"-v", "error", "-f", "lavfi", "-i", "sine=frequency=880:duration=" + seconds, "-y", path}
	if out, err := exec.CommandContext(context.Background(), "ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("创建音频: %v\n%s", err, out)
	}
	return path
}

// silentVideo 生成没有音轨的色块视频。
func silentVideo(t *testing.T, dir, color, size, seconds string) string {
	t.Helper()
	path := filepath.Join(dir, color+"-silent.mp4")
	args := []string{"-v", "error", "-f", "lavfi", "-i", "color=c=" + color + ":s=" + size + ":r=12:d=" + seconds,
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-y", path}
	if out, err := exec.CommandContext(context.Background(), "ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("创建无声视频: %v\n%s", err, out)
	}
	return path
}

// hasAudio 成片有没有音轨。
func hasAudio(t *testing.T, path string) bool {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), "ffprobe", "-v", "error", "-select_streams", "a", "-show_entries", "stream=codec_type", "-of", "json", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Streams []struct{} `json:"streams"`
	}
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatal(err)
	}
	return len(p.Streams) > 0
}

// meanVolume 成片某一段时间内音频的平均音量（dB）；静音返回很小的数。
func meanVolume(t *testing.T, path string, from, to float64) float64 {
	t.Helper()
	args := []string{"-v", "info", "-ss", strconv.FormatFloat(from, 'f', 2, 64), "-t", strconv.FormatFloat(to-from, 'f', 2, 64), "-i", path, "-vn", "-af", "volumedetect", "-f", "null", "-"}
	out, _ := exec.CommandContext(context.Background(), "ffmpeg", args...).CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if _, rest, ok := strings.Cut(line, "mean_volume:"); ok {
			v, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), "dB")), 64)
			return v
		}
	}
	return -91
}

// pixel 取成片某一秒、某个相对位置（0–1）的像素颜色。
func pixel(t *testing.T, e *videoframe.Extractor, path string, sec, fx, fy float64) (int, int, int) {
	t.Helper()
	data, err := e.Frame(context.Background(), path, sec, videoframe.FrameOpts{})
	if err != nil {
		t.Fatalf("取 %.1f 秒的画面: %v", sec, err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	r, g, bl, _ := img.At(b.Min.X+int(fx*float64(b.Dx()-1)), b.Min.Y+int(fy*float64(b.Dy()-1))).RGBA()
	return int(r >> 8), int(g >> 8), int(bl >> 8)
}

// brightCount 成片某一秒画面里某个纵向区间内接近白色的像素数，用来判断字幕有没有画上去。
func brightCount(t *testing.T, e *videoframe.Extractor, path string, sec, fromY, toY float64) int {
	t.Helper()
	data, err := e.Frame(context.Background(), path, sec, videoframe.FrameOpts{})
	if err != nil {
		t.Fatalf("取 %.1f 秒的画面: %v", sec, err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	n := 0
	for y := b.Min.Y + int(fromY*float64(b.Dy())); y < b.Min.Y+int(toY*float64(b.Dy())); y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r>>8 > 200 && g>>8 > 200 && bl>>8 > 200 {
				n++
			}
		}
	}
	return n
}

func near(a, b, tol int) bool { return a >= b-tol && a <= b+tol }

func TestComposeTimeline_NoAttachedBehavesLikeComposeClips(t *testing.T) {
	e := needFFmpeg(t)
	dir := t.TempDir()
	video := composeLong(t, dir, "red", "320x240", "4")
	dst := filepath.Join(dir, "final.mp4")
	plan := videoframe.ComposePlan{Main: []videoframe.ComposeClip{{Path: video, InMS: 1000, OutMS: 3500}}}
	if err := e.ComposeTimeline(context.Background(), plan, "9:16", dst); err != nil {
		t.Fatal(err)
	}
	w, h, d := probeSize(t, dst)
	if w != 240 || h != 426 || math.Abs(d-2.5) > 0.2 {
		t.Fatalf("成片 %dx%d %.2fs，期望 240x426 约 2.5s", w, h, d)
	}
}

func TestComposeTimeline_OverlayOpacityAndTail(t *testing.T) {
	e := needFFmpeg(t)
	dir := t.TempDir()
	video := silentVideo(t, dir, "red", "320x240", "2")
	green := composeImage(t, dir, "green", "200x200")
	dst := filepath.Join(dir, "final.mp4")
	plan := videoframe.ComposePlan{
		Main: []videoframe.ComposeClip{{Path: video, OutMS: 2000}},
		// 叠加图片：1–4 秒，铺满画面中心，50% 不透明；叠加比主轨长，成片总长应为 4 秒
		Overlays: []videoframe.ComposeOverlay{{Path: green, Image: true, StartMS: 1000, OutMS: 3000, X: 50, Y: 50, Scale: 100, Opacity: 50}},
	}
	if err := e.ComposeTimeline(context.Background(), plan, "9:16", dst); err != nil {
		t.Fatal(err)
	}
	if _, _, d := probeSize(t, dst); math.Abs(d-4) > 0.25 {
		t.Fatalf("成片时长 %.2f，期望约 4 秒（取全部轨最晚结束点）", d)
	}
	// 0.5 秒：还没到叠加，主轨的红色
	if r, g, b := pixel(t, e, dst, 0.5, 0.5, 0.5); r < 200 || g > 60 || b > 60 {
		t.Fatalf("0.5 秒中心应是红色，得到 %d,%d,%d", r, g, b)
	}
	// 1.5 秒：红色和绿色各 50% 混合，约 (127,64,0)
	if r, g, _ := pixel(t, e, dst, 1.5, 0.5, 0.5); !near(r, 127, 40) || !near(g, 64, 35) {
		t.Fatalf("1.5 秒中心应是半透明混合，得到 r=%d g=%d", r, g)
	}
	// 3.5 秒：主轨已结束，尾部是黑底，只剩半透明的绿色约 (0,64,0)
	if r, g, _ := pixel(t, e, dst, 3.5, 0.5, 0.5); r > 40 || !near(g, 64, 35) {
		t.Fatalf("3.5 秒应是黑底上的半透明绿，得到 r=%d g=%d", r, g)
	}
	if r, g, b := pixel(t, e, dst, 3.5, 0.02, 0.02); r+g+b > 60 {
		t.Fatalf("尾部边角应是黑色，得到 %d,%d,%d", r, g, b)
	}
}

func TestComposeTimeline_OverlayPositionScaleAndFade(t *testing.T) {
	e := needFFmpeg(t)
	dir := t.TempDir()
	video := silentVideo(t, dir, "red", "320x240", "4")
	blue := silentVideo(t, dir, "blue", "160x120", "3")
	dst := filepath.Join(dir, "final.mp4")
	plan := videoframe.ComposePlan{
		Main: []videoframe.ComposeClip{{Path: video, OutMS: 4000}},
		// 右上角小窗：中心 (75%,25%)，宽度占 40%；淡入 1 秒，从素材 0.5 秒处开始，没有音轨的叠加视频不能让命令失败
		Overlays: []videoframe.ComposeOverlay{{Path: blue, StartMS: 0, InMS: 500, OutMS: 3000, X: 75, Y: 25, Scale: 40, Opacity: 100, FadeInMS: 1000}},
	}
	if err := e.ComposeTimeline(context.Background(), plan, "9:16", dst); err != nil {
		t.Fatal(err)
	}
	// 2 秒：右上角小窗中心是蓝色，其余位置仍是主轨红色
	if _, _, b := pixel(t, e, dst, 2, 0.75, 0.25); b < 200 {
		t.Fatalf("小窗中心应是蓝色，蓝通道 %d", b)
	}
	// 横屏素材放进 9:16 画布上下有黑边，取画面中心偏左（0.2,0.5）才是素材本身
	if r, _, b := pixel(t, e, dst, 2, 0.2, 0.5); r < 200 || b > 60 {
		t.Fatalf("画面左侧应是主轨红色，得到 r=%d b=%d", r, b)
	}
	// 0.1 秒还在淡入：小窗位置在上方黑边里，几乎看不到蓝色（完全显示时蓝通道是 252）
	if _, _, b := pixel(t, e, dst, 0.1, 0.75, 0.25); b > 60 {
		t.Fatalf("淡入起点小窗应几乎透明，蓝通道 %d", b)
	}
}

func TestComposeTimeline_AudioMixAndSilentTail(t *testing.T) {
	e := needFFmpeg(t)
	dir := t.TempDir()
	video := composeLong(t, dir, "red", "320x240", "2")
	tone := toneFile(t, dir, "voice", "3")
	dst := filepath.Join(dir, "final.mp4")
	plan := videoframe.ComposePlan{
		// 主轨静音，配音从 1 秒开始、取素材 0–2 秒、淡入 0.5 秒；成片总长 3 秒（配音更晚结束）
		Main:   []videoframe.ComposeClip{{Path: video, OutMS: 2000, Muted: true}},
		Audios: []videoframe.ComposeAudio{{Path: tone, StartMS: 1000, OutMS: 2000, Volume: 100, FadeInMS: 500}},
	}
	if err := e.ComposeTimeline(context.Background(), plan, "9:16", dst); err != nil {
		t.Fatal(err)
	}
	if !hasAudio(t, dst) {
		t.Fatal("成片应有音轨")
	}
	w, h, d := probeSize(t, dst)
	if w != 240 || h != 426 || math.Abs(d-3) > 0.25 {
		t.Fatalf("成片 %dx%d %.2fs，期望 240x426 约 3s", w, h, d)
	}
	if v := meanVolume(t, dst, 0, 0.9); v > -60 {
		t.Fatalf("配音开始前应是静音，平均音量 %.1f dB", v)
	}
	if v := meanVolume(t, dst, 1.8, 2.9); v < -35 {
		t.Fatalf("配音段应有声音，平均音量 %.1f dB", v)
	}
	// 尾部（主轨 2 秒后）画面是黑色
	if r, g, b := pixel(t, e, dst, 2.6, 0.5, 0.5); r+g+b > 60 {
		t.Fatalf("尾部应是黑色，得到 %d,%d,%d", r, g, b)
	}
}

func TestComposeTimeline_VolumeScalesAndMainVolume(t *testing.T) {
	e := needFFmpeg(t)
	dir := t.TempDir()
	video := composeLong(t, dir, "red", "320x240", "2")
	tone := toneFile(t, dir, "bgm", "2")
	loud, quiet := filepath.Join(dir, "loud.mp4"), filepath.Join(dir, "quiet.mp4")
	for dst, vol := range map[string]int{loud: 100, quiet: 25} {
		plan := videoframe.ComposePlan{
			Main:   []videoframe.ComposeClip{{Path: video, OutMS: 2000, Muted: true}},
			Audios: []videoframe.ComposeAudio{{Path: tone, OutMS: 2000, Volume: vol}},
		}
		if err := e.ComposeTimeline(context.Background(), plan, "9:16", dst); err != nil {
			t.Fatal(err)
		}
	}
	if l, q := meanVolume(t, loud, 0.2, 1.8), meanVolume(t, quiet, 0.2, 1.8); l-q < 8 {
		t.Fatalf("音量 25%% 应比 100%% 小约 12 dB，实际 %.1f dB vs %.1f dB", q, l)
	}
	// 主轨片段自己的音量同样生效（没有附属轨时也走 ComposeClips）
	half := filepath.Join(dir, "half.mp4")
	if err := e.ComposeClips(context.Background(), []videoframe.ComposeClip{{Path: video, OutMS: 2000, Volume: 25}}, "9:16", half); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(dir, "full.mp4")
	if err := e.ComposeClips(context.Background(), []videoframe.ComposeClip{{Path: video, OutMS: 2000}}, "9:16", full); err != nil {
		t.Fatal(err)
	}
	if f, h := meanVolume(t, full, 0.2, 1.8), meanVolume(t, half, 0.2, 1.8); f-h < 8 {
		t.Fatalf("主轨音量 25%% 应更小，实际 %.1f dB vs %.1f dB", h, f)
	}
}

func TestComposeTimeline_SubtitleBurnedInOnlyWhileShown(t *testing.T) {
	e := needFFmpeg(t)
	needASS(t)
	dir := t.TempDir()
	video := silentVideo(t, dir, "black", "320x240", "4")
	dst := filepath.Join(dir, "final.mp4")
	plan := videoframe.ComposePlan{
		Main: []videoframe.ComposeClip{{Path: video, OutMS: 4000}},
		Texts: []videoframe.ComposeText{
			{StartMS: 500, LenMS: 2000, Text: "你好，世界！这是一条会自动换行的比较长的中文字幕，不能溢出画面。", Style: "subtitle", Pos: "bottom", Size: 100},
			{StartMS: 500, LenMS: 2000, Text: "标题", Style: "title", Pos: "top", Size: 100},
		},
	}
	if err := e.ComposeTimeline(context.Background(), plan, "9:16", dst); err != nil {
		t.Fatal(err)
	}
	if n := brightCount(t, e, dst, 1.5, 0.75, 0.97); n < 150 {
		t.Fatalf("字幕显示期间底部应有白色文字像素，只有 %d 个", n)
	}
	if n := brightCount(t, e, dst, 1.5, 0.05, 0.25); n < 60 {
		t.Fatalf("标题显示期间顶部应有白色文字像素，只有 %d 个", n)
	}
	if n := brightCount(t, e, dst, 3.2, 0, 1); n != 0 {
		t.Fatalf("字幕结束后画面应干净，仍有 %d 个白色像素", n)
	}
	if n := brightCount(t, e, dst, 1.5, 0.4, 0.6); n != 0 {
		t.Fatalf("文字不应出现在画面中部，有 %d 个白色像素", n)
	}
}

func TestComposeTimeline_RejectsBadInputs(t *testing.T) {
	e := needFFmpeg(t)
	dir := t.TempDir()
	video := silentVideo(t, dir, "red", "160x120", "1")
	tone := toneFile(t, dir, "short", "1")
	dst := filepath.Join(dir, "out.mp4")
	main := []videoframe.ComposeClip{{Path: video, OutMS: 1000}}
	for name, plan := range map[string]videoframe.ComposePlan{
		"音频入出点越界":   {Main: main, Audios: []videoframe.ComposeAudio{{Path: tone, InMS: 0, OutMS: 5000}}},
		"音频段没有时长":   {Main: main, Audios: []videoframe.ComposeAudio{{Path: tone, InMS: 800, OutMS: 800}}},
		"叠加视频入出点越界": {Main: main, Overlays: []videoframe.ComposeOverlay{{Path: video, InMS: 0, OutMS: 9000, Scale: 50, Opacity: 100}}},
		"音频轨放了视频文件": {Main: main, Audios: []videoframe.ComposeAudio{{Path: video, OutMS: 500}}},
		"没有主轨":      {Audios: []videoframe.ComposeAudio{{Path: tone, OutMS: 500}}},
	} {
		if err := e.ComposeTimeline(context.Background(), plan, "9:16", dst); err == nil {
			t.Fatalf("%s：应该报错", name)
		}
	}
}

func TestWrapCaption_BreaksCJKAndLatinWithinWidth(t *testing.T) {
	// 字号 40px、可用宽 200px：中文每字 40px → 每行最多 5 个字
	got := videoframe.WrapCaption("一二三四五六七八九十", 40, 200)
	if len(got) != 2 || got[0] != "一二三四五" || got[1] != "六七八九十" {
		t.Fatalf("中文换行 %q", got)
	}
	// 拉丁文优先在空格处换行，不拆单词
	lines := videoframe.WrapCaption("hello wonderful world", 20, 160)
	for _, l := range lines {
		if strings.HasPrefix(l, " ") || strings.HasSuffix(l, " ") {
			t.Fatalf("行首尾不应有空格 %q", lines)
		}
	}
	if strings.Join(lines, " ") != "hello wonderful world" {
		t.Fatalf("拉丁文换行丢了内容 %q", lines)
	}
	// 用户自己的换行保留
	if got := videoframe.WrapCaption("上\n下", 40, 400); len(got) != 2 {
		t.Fatalf("手动换行 %q", got)
	}
	if got := videoframe.WrapCaption("", 40, 400); len(got) != 0 {
		t.Fatalf("空文字应没有行 %q", got)
	}
}

func TestBuildASS_TimingPositionAndEscaping(t *testing.T) {
	ass := videoframe.BuildASS([]videoframe.ComposeText{
		{StartMS: 1000, LenMS: 2500, Text: "底部字幕", Style: "subtitle", Pos: "bottom", Size: 100},
		{StartMS: 0, LenMS: 1000, Text: "顶部{标题}\\n", Style: "title", Pos: "top", Size: 120},
		{StartMS: 0, LenMS: 1000, Text: "中间", Style: "subtitle", Pos: "middle", Size: 100},
	}, 720, 1280)
	for _, want := range []string{"PlayResX: 720", "PlayResY: 1280", "0:00:01.00,0:00:03.50", `\an2`, `\an8`, `\an5`} {
		if !strings.Contains(ass, want) {
			t.Fatalf("ASS 缺少 %q:\n%s", want, ass)
		}
	}
	// 用户文字里的花括号和反斜杠不能被当成 ASS 覆盖标签
	if strings.Contains(ass, "{标题}") || strings.Contains(ass, `\n`) {
		t.Fatalf("文字未转义:\n%s", ass)
	}
}

// 旧版 ffmpeg（如 alpine 的 6.x）不认识 -/filter_complex：应自动退回带内联滤镜图的 -filter_complex，合成结果不变。
func TestComposeTimeline_FallsBackToFilterComplexScript(t *testing.T) {
	needFFmpeg(t)
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("没有 ffmpeg")
	}
	dir := t.TempDir()
	// 包装脚本：遇到 -/filter_complex 就按旧版报「不认识的选项」，其余原样交给真 ffmpeg。
	wrapper := filepath.Join(dir, "ffmpeg6")
	script := "#!/bin/sh\nfor a in \"$@\"; do\n  if [ \"$a\" = \"-/filter_complex\" ]; then echo \"Unrecognized option '/filter_complex'.\" >&2; exit 1; fi\ndone\nexec " + ffmpegPath + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	e := videoframe.New(videoframe.Options{FFmpegPath: wrapper, Timeout: time.Minute})
	video := silentVideo(t, dir, "red", "320x240", "2")
	green := composeImage(t, dir, "green", "200x200")
	dst := filepath.Join(dir, "final.mp4")
	plan := videoframe.ComposePlan{
		Main:     []videoframe.ComposeClip{{Path: video, OutMS: 2000}},
		Overlays: []videoframe.ComposeOverlay{{Path: green, Image: true, StartMS: 0, OutMS: 2000, X: 50, Y: 50, Scale: 100, Opacity: 100}},
	}
	if err := e.ComposeTimeline(context.Background(), plan, "9:16", dst); err != nil {
		t.Fatalf("旧版选项回退失败: %v", err)
	}
	if _, g, _ := pixel(t, e, dst, 1, 0.5, 0.5); g < 100 {
		t.Fatalf("叠加应已生效，绿通道 %d", g)
	}
}
