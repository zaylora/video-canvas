package videoframe

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strconv"
)

// 镜头切点检测与取帧计划的参数。
const (
	// DefaultCutThreshold 是场景切换分数阈值：偏低，宁可多切（过切只多几帧），不可漏切（漏切会丢镜头）。
	DefaultCutThreshold = 0.25
	// MaxPlanFrames 是一次取帧计划的帧数上限（拆解参考片退回截帧时用）。
	MaxPlanFrames = 32
	// planStep 是常规取帧间隔（秒）：每秒 2 帧。
	planStep = 0.5
	// cutDedupe 是两个取帧时间点小于它就合并成一个（秒）。
	cutDedupe = 0.2
	// cutNudge 是切点后移的秒数：取切点后的第一帧而不是切点前的最后一帧。
	cutNudge = 0.05
)

// ptsTime 匹配 ffmpeg showinfo 输出里的帧时间。
var ptsTime = regexp.MustCompile(`pts_time:([0-9]+(?:\.[0-9]+)?)`)

// Cuts 用 ffmpeg 场景检测找出镜头切点（秒，升序），不含 0 秒起点。threshold 是场景切换分数（0 到 1），
// 不大于 0 时用 DefaultCutThreshold。视频没有切点时返回空切片。
func (e *Extractor) Cuts(ctx context.Context, src string, threshold float64) ([]float64, error) {
	if threshold <= 0 || threshold >= 1 {
		threshold = DefaultCutThreshold
	}
	filter := "select='gt(scene," + strconv.FormatFloat(threshold, 'f', 3, 64) + ")',showinfo"
	_, stderr, err := e.runBoth(ctx, e.ffmpeg, "-hide_banner", "-protocol_whitelist", e.protocols,
		"-i", src, "-an", "-vf", filter, "-f", "null", "-")
	if err != nil {
		return nil, err
	}
	return parseCuts(string(stderr)), nil
}

// parseCuts 从 ffmpeg showinfo 的输出里取出帧时间，去掉 0 点并升序去重。
func parseCuts(log string) []float64 {
	cuts := []float64{}
	for _, m := range ptsTime.FindAllStringSubmatch(log, -1) {
		t, err := strconv.ParseFloat(m[1], 64)
		if err != nil || t <= 0 {
			continue
		}
		cuts = append(cuts, t)
	}
	sort.Float64s(cuts)
	return cuts
}

// PlanTimes 生成拆解参考片用的取帧时间（升序）：每个切点取 1 帧（略晚于切点）、整段每 0.5 秒取 1 帧，
// 相距不到 0.2 秒的合并，总数不超过 limit（≤0 或超过 MaxPlanFrames 时取 MaxPlanFrames）。
// 超出上限时优先保留切点帧，再从常规帧里均匀抽取。duration 不大于 0 返回空。
func PlanTimes(duration float64, cuts []float64, limit int) []float64 {
	if duration <= 0 {
		return []float64{}
	}
	if limit <= 0 || limit > MaxPlanFrames {
		limit = MaxPlanFrames
	}
	last := LastAt(duration)
	cutTimes := []float64{}
	for _, c := range cuts {
		if c > 0 && c < duration {
			cutTimes = append(cutTimes, min(c+cutNudge, last))
		}
	}
	sort.Float64s(cutTimes)
	cutTimes = dedupe(append([]float64{0}, cutTimes...))
	if len(cutTimes) >= limit {
		return spread(cutTimes, limit)
	}
	regular := []float64{}
	for t := planStep; t < last; t += planStep {
		regular = append(regular, t)
	}
	extra := []float64{}
	for _, t := range regular {
		if !near(cutTimes, t) {
			extra = append(extra, t)
		}
	}
	room := limit - len(cutTimes)
	if len(extra) > room {
		extra = spread(extra, room)
	}
	all := append(append([]float64{}, cutTimes...), extra...)
	sort.Float64s(all)
	return all
}

// near 判断 t 是否与 ts 里某个时间点相距不到 cutDedupe。
func near(ts []float64, t float64) bool {
	for _, x := range ts {
		if t-x < cutDedupe && x-t < cutDedupe {
			return true
		}
	}
	return false
}

// dedupe 对升序时间点去重：与前一个相距不到 cutDedupe 的丢掉。
func dedupe(ts []float64) []float64 {
	out := []float64{}
	for _, t := range ts {
		if len(out) == 0 || t-out[len(out)-1] >= cutDedupe {
			out = append(out, t)
		}
	}
	return out
}

// spread 从升序时间点里均匀取 n 个（含首尾）；n 不小于长度时原样返回。
func spread(ts []float64, n int) []float64 {
	if n <= 0 {
		return []float64{}
	}
	if n >= len(ts) {
		return append([]float64{}, ts...)
	}
	if n == 1 {
		return []float64{ts[0]}
	}
	out := make([]float64, 0, n)
	for i := range n {
		out = append(out, ts[i*(len(ts)-1)/(n-1)])
	}
	return out
}

// FramesAt 在给定时间点各取一帧；某个时间点没有画面（ErrNoFrame）时跳过，其他错误直接返回。
func (e *Extractor) FramesAt(ctx context.Context, src string, times []float64, o FrameOpts) ([]Frame, error) {
	frames := make([]Frame, 0, len(times))
	for _, at := range times {
		data, err := e.Frame(ctx, src, at, o)
		if err != nil {
			if errors.Is(err, ErrNoFrame) {
				continue
			}
			return nil, err
		}
		frames = append(frames, Frame{At: at, Data: data})
	}
	return frames, nil
}
