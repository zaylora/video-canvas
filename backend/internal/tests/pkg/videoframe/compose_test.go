package videoframe_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"video-canvas/internal/pkg/videoframe"
)

func composeColor(t *testing.T, dir, color, size string, audio bool) string {
	t.Helper()
	if !videoframe.New(videoframe.Options{}).Available() {
		t.Skip("本机没有 ffmpeg / ffprobe，跳过合成测试")
	}
	path := filepath.Join(dir, color+".mp4")
	args := []string{"-v", "error", "-f", "lavfi", "-i", "color=c=" + color + ":s=" + size + ":r=12:d=1"}
	if audio {
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:a", "aac")
	}
	args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-y", path)
	if out, err := exec.CommandContext(context.Background(), "ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("创建色块视频: %v\n%s", err, out)
	}
	return path
}

func TestCompose_NormalizesAndAddsSilence(t *testing.T) {
	dir := t.TempDir()
	first := composeColor(t, dir, "red", "160x120", true)
	second := composeColor(t, dir, "blue", "240x160", false)
	dst := filepath.Join(dir, "final.mp4")
	e := videoframe.New(videoframe.Options{Timeout: 30 * time.Second})
	if err := e.Compose(context.Background(), []string{first, second}, dst); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(context.Background(), "ffprobe", "-v", "error", "-show_streams", "-show_format", "-of", "json", dst).Output()
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Streams []struct {
			CodecType  string `json:"codec_type"`
			CodecName  string `json:"codec_name"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			Rate       string `json:"r_frame_rate"`
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatal(err)
	}
	video, audio := false, false
	for _, stream := range probe.Streams {
		if stream.CodecType == "video" {
			video = stream.CodecName == "h264" && stream.Width == 160 && stream.Height == 120 && stream.Rate == "30/1"
		}
		if stream.CodecType == "audio" {
			audio = stream.CodecName == "aac" && stream.SampleRate == "48000" && stream.Channels == 2
		}
	}
	if !video || !audio {
		t.Fatalf("成片编码或尺寸错误: %s", out)
	}
	duration, err := e.Duration(context.Background(), dst)
	if err != nil || math.Abs(duration-2) > 0.2 {
		t.Fatalf("成片时长 %f: %v", duration, err)
	}
	for _, at := range []float64{0.25, 1.25} {
		if _, err := e.Frame(context.Background(), dst, at, videoframe.FrameOpts{}); err != nil {
			t.Fatalf("成片无法播放 %.2f: %v", at, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 3 {
		t.Fatalf("临时转码文件未清理: %v %v", entries, err)
	}
}

func TestCompose_EmptyCanceledAndTimeout(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.mp4")
	if err := videoframe.New(videoframe.Options{}).Compose(context.Background(), nil, dst); err == nil {
		t.Fatal("空片段不能合成")
	}
	src := composeColor(t, dir, "green", "160x120", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := videoframe.New(videoframe.Options{}).Compose(ctx, []string{src}, dst); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消未传播: %v", err)
	}
	if err := videoframe.New(videoframe.Options{Timeout: time.Nanosecond}).Compose(context.Background(), []string{src}, dst); !errors.Is(err, videoframe.ErrTimeout) {
		t.Fatalf("超时未传播: %v", err)
	}
	if _, err := os.Stat(dst); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("失败残留成片: %v", err)
	}
}
