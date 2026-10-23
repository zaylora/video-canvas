package videoframe_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"video-canvas/internal/pkg/videoframe"
)

// makeVideo 用 ffmpeg 合成一段 3 秒、320x240、10fps 的测试视频；本机没有 ffmpeg 时跳过。
func makeVideo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("本机没有 ffmpeg，跳过")
	}
	path := filepath.Join(t.TempDir(), "in.mp4")
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=d=3:s=320x240:r=10",
		"-pix_fmt", "yuv420p", "-y", path).CombinedOutput()
	if err != nil {
		t.Fatalf("合成测试视频失败: %v\n%s", err, out)
	}
	return path
}

func newExtractor() *videoframe.Extractor {
	return videoframe.New(videoframe.Options{Timeout: 20 * time.Second, MaxConcurrent: 2})
}

func TestDuration(t *testing.T) {
	src := makeVideo(t)
	d, err := newExtractor().Duration(context.Background(), src)
	if err != nil {
		t.Fatalf("Duration: %v", err)
	}
	if math.Abs(d-3) > 0.2 {
		t.Fatalf("时长 = %v，想要约 3 秒", d)
	}
}

func TestFrame_PNGKeepsOriginalSize(t *testing.T) {
	src := makeVideo(t)
	data, err := newExtractor().Frame(context.Background(), src, 1.0, videoframe.FrameOpts{Format: videoframe.PNG})
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("不是合法 PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 320 || b.Dy() != 240 {
		t.Fatalf("尺寸 = %dx%d，想要 320x240", b.Dx(), b.Dy())
	}
}

func TestFrame_JPEGScalesLongEdge(t *testing.T) {
	src := makeVideo(t)
	data, err := newExtractor().Frame(context.Background(), src, 0, videoframe.FrameOpts{Format: videoframe.JPEG, MaxEdge: 100})
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("不是合法 JPEG: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 100 || b.Dy() > 100 || b.Dy() < 70 {
		t.Fatalf("尺寸 = %dx%d，长边应缩到 100 且保持比例", b.Dx(), b.Dy())
	}
}

func TestFrame_SmallerThanMaxEdgeIsNotUpscaled(t *testing.T) {
	src := makeVideo(t)
	data, err := newExtractor().Frame(context.Background(), src, 0, videoframe.FrameOpts{Format: videoframe.PNG, MaxEdge: 1024})
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 320 {
		t.Fatalf("宽 = %d，小图不该被放大", cfg.Width)
	}
}

func TestFrame_LastFrameNearEnd(t *testing.T) {
	src := makeVideo(t)
	e := newExtractor()
	d, err := e.Duration(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	// 尾帧不取正好等于时长的时间点，用 LastAt 往回退一点。
	if _, err := e.Frame(context.Background(), src, videoframe.LastAt(d), videoframe.FrameOpts{Format: videoframe.PNG}); err != nil {
		t.Fatalf("尾帧应能取到: %v", err)
	}
}

func TestFrame_BeyondEndIsErrNoFrame(t *testing.T) {
	src := makeVideo(t)
	_, err := newExtractor().Frame(context.Background(), src, 100, videoframe.FrameOpts{Format: videoframe.PNG})
	if !errors.Is(err, videoframe.ErrNoFrame) {
		t.Fatalf("err = %v，想要 ErrNoFrame", err)
	}
}

func TestFrame_MissingFileIsErrDecode(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("本机没有 ffmpeg，跳过")
	}
	_, err := newExtractor().Frame(context.Background(), filepath.Join(t.TempDir(), "nope.mp4"), 0, videoframe.FrameOpts{Format: videoframe.PNG})
	if !errors.Is(err, videoframe.ErrDecode) {
		t.Fatalf("err = %v，想要 ErrDecode", err)
	}
}

func TestFrame_TimeoutIsErrTimeout(t *testing.T) {
	src := makeVideo(t)
	e := videoframe.New(videoframe.Options{Timeout: time.Nanosecond, MaxConcurrent: 1})
	_, err := e.Frame(context.Background(), src, 0, videoframe.FrameOpts{Format: videoframe.PNG})
	if !errors.Is(err, videoframe.ErrTimeout) {
		t.Fatalf("err = %v，想要 ErrTimeout", err)
	}
}

func TestUnavailable(t *testing.T) {
	e := videoframe.New(videoframe.Options{FFmpegPath: "definitely-not-a-real-ffmpeg-binary", FFprobePath: "definitely-not-a-real-ffprobe-binary"})
	if e.Available() {
		t.Fatal("找不到二进制时 Available 应为 false")
	}
	if _, err := e.Frame(context.Background(), "x.mp4", 0, videoframe.FrameOpts{Format: videoframe.PNG}); !errors.Is(err, videoframe.ErrUnavailable) {
		t.Fatalf("Frame err = %v，想要 ErrUnavailable", err)
	}
	if _, err := e.Duration(context.Background(), "x.mp4"); !errors.Is(err, videoframe.ErrUnavailable) {
		t.Fatalf("Duration err = %v，想要 ErrUnavailable", err)
	}
}

func TestSample_IncludesFirstAndLast(t *testing.T) {
	src := makeVideo(t)
	frames, dur, err := newExtractor().Sample(context.Background(), src, 4, videoframe.FrameOpts{Format: videoframe.JPEG, MaxEdge: 160})
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if len(frames) != 4 {
		t.Fatalf("帧数 = %d，想要 4", len(frames))
	}
	if math.Abs(dur-3) > 0.2 {
		t.Fatalf("时长 = %v", dur)
	}
	if frames[0].At != 0 {
		t.Fatalf("首帧时间 = %v，想要 0", frames[0].At)
	}
	if last := frames[3].At; last > dur-0.05 || last < dur-0.5 {
		t.Fatalf("尾帧时间 = %v，应略小于时长 %v", last, dur)
	}
	for i := 1; i < len(frames); i++ {
		if frames[i].At <= frames[i-1].At {
			t.Fatalf("时间应递增: %v", frames)
		}
		if len(frames[i].Data) == 0 {
			t.Fatalf("第 %d 帧没有数据", i)
		}
	}
}

func TestSample_SingleFrameIsMiddle(t *testing.T) {
	src := makeVideo(t)
	frames, dur, err := newExtractor().Sample(context.Background(), src, 1, videoframe.FrameOpts{Format: videoframe.JPEG})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || math.Abs(frames[0].At-dur/2) > 0.2 {
		t.Fatalf("单帧应取中间: %v (时长 %v)", frames, dur)
	}
}

func TestSample_RejectsBadCount(t *testing.T) {
	e := newExtractor()
	for _, n := range []int{0, -1, 7} {
		if _, _, err := e.Sample(context.Background(), "x.mp4", n, videoframe.FrameOpts{Format: videoframe.JPEG}); !errors.Is(err, videoframe.ErrBadCount) {
			t.Fatalf("n=%d err = %v，想要 ErrBadCount", n, err)
		}
	}
}

func TestLastAt(t *testing.T) {
	cases := []struct{ dur, want float64 }{
		{10, 9.9},
		{0.1, 0.05},
		{0, 0},
	}
	for _, c := range cases {
		if got := videoframe.LastAt(c.dur); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("LastAt(%v) = %v，想要 %v", c.dur, got, c.want)
		}
	}
}
