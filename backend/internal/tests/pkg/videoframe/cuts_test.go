package videoframe_test

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"video-canvas/internal/pkg/videoframe"
)

// makeThreeScenes 合成 3 段纯色（红、蓝、绿，各 1 秒）拼成的视频，切点在 1 秒和 2 秒；没有 ffmpeg 时跳过。
func makeThreeScenes(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("本机没有 ffmpeg，跳过")
	}
	path := filepath.Join(t.TempDir(), "scenes.mp4")
	out, err := exec.Command("ffmpeg", "-v", "error",
		"-f", "lavfi", "-i", "color=c=red:s=160x120:d=1:r=10",
		"-f", "lavfi", "-i", "color=c=blue:s=160x120:d=1:r=10",
		"-f", "lavfi", "-i", "color=c=green:s=160x120:d=1:r=10",
		"-filter_complex", "[0:v][1:v][2:v]concat=n=3:v=1:a=0[v]", "-map", "[v]",
		"-pix_fmt", "yuv420p", "-y", path).CombinedOutput()
	if err != nil {
		t.Fatalf("合成测试视频失败: %v\n%s", err, out)
	}
	return path
}

func TestCuts_FindsSceneChanges(t *testing.T) {
	src := makeThreeScenes(t)
	cuts, err := newExtractor().Cuts(context.Background(), src, 0)
	if err != nil {
		t.Fatalf("Cuts: %v", err)
	}
	if len(cuts) != 2 || math.Abs(cuts[0]-1) > 0.2 || math.Abs(cuts[1]-2) > 0.2 {
		t.Fatalf("切点 = %v，想要约 [1 2]", cuts)
	}
}

func TestCuts_NoSceneChangeReturnsEmpty(t *testing.T) {
	src := makeVideo(t) // 同一段连续画面
	cuts, err := newExtractor().Cuts(context.Background(), src, 0.9)
	if err != nil {
		t.Fatalf("Cuts: %v", err)
	}
	if cuts == nil || len(cuts) != 0 {
		t.Fatalf("没有切点应返回空切片，得到 %v", cuts)
	}
}

func TestFramesAt_SkipsMissingFrames(t *testing.T) {
	src := makeThreeScenes(t)
	frames, err := newExtractor().FramesAt(context.Background(), src, []float64{0, 1.2, 99}, videoframe.FrameOpts{Format: videoframe.JPEG, MaxEdge: 64})
	if err != nil {
		t.Fatalf("FramesAt: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("应只得到 2 帧（99 秒没有画面），得到 %d", len(frames))
	}
}

func TestPlanTimes_CutsPlusTwoPerSecond(t *testing.T) {
	got := videoframe.PlanTimes(4, []float64{1.7}, 0)
	if !sort.Float64sAreSorted(got) {
		t.Fatalf("时间点必须升序：%v", got)
	}
	// 4 秒每 0.5 秒 1 帧 = 0、0.5…3.5，共 8 个；切点帧 1.75 与常规的 1.5 / 2.0 相距 ≥0.2，保留。
	hasCut := false
	for _, x := range got {
		if math.Abs(x-1.75) < 0.01 {
			hasCut = true
		}
	}
	if !hasCut {
		t.Fatalf("应包含切点后的帧（约 1.75 秒）：%v", got)
	}
	if len(got) < 8 || len(got) > 10 {
		t.Fatalf("帧数 = %d：%v", len(got), got)
	}
}

func TestPlanTimes_MergesNearbyPoints(t *testing.T) {
	// 切点 1.0 的帧（1.05）与常规帧 1.0 相距 0.05 < 0.2，合并成一个。
	got := videoframe.PlanTimes(2, []float64{1.0}, 0)
	n := 0
	for _, x := range got {
		if x > 0.9 && x < 1.2 {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("1 秒附近应只剩一帧，得到 %v", got)
	}
}

func TestPlanTimes_CapsAtLimitKeepingCuts(t *testing.T) {
	cuts := []float64{3, 6, 9, 12}
	got := videoframe.PlanTimes(60, cuts, 0)
	if len(got) != videoframe.MaxPlanFrames {
		t.Fatalf("长视频应截到 %d 帧，得到 %d", videoframe.MaxPlanFrames, len(got))
	}
	for _, c := range cuts {
		found := false
		for _, x := range got {
			if math.Abs(x-(c+0.05)) < 0.01 {
				found = true
			}
		}
		if !found {
			t.Fatalf("超出上限时应优先保留切点帧 %v：%v", c, got)
		}
	}
}

func TestPlanTimes_EmptyForZeroDuration(t *testing.T) {
	if got := videoframe.PlanTimes(0, nil, 0); len(got) != 0 {
		t.Fatalf("时长 0 应返回空：%v", got)
	}
}
