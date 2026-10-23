// Package videoframe 用 ffmpeg / ffprobe 从视频里抽帧，给画布 Agent 看视频和截帧用。
//
// 外部二进制只通过 os/exec 调用（不链接），输入只允许本地文件和 http(s)，
// 每次调用有超时，同时运行的数量有上限，超出直接返回 ErrBusy 而不是排队。
package videoframe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// MaxSampleFrames 是一次最多抽的帧数。
const MaxSampleFrames = 6

// lastMargin 是取尾帧时往回退的秒数：正好取到总时长多数解码器给不出画面。
const lastMargin = 0.1

// 默认值。
const (
	defaultTimeout       = 30 * time.Second
	defaultMaxConcurrent = 2
	defaultProtocols     = "file,http,https,tcp,tls,crypto"
	maxStderr            = 512 // 错误信息里最多带多少字节的 ffmpeg 输出
)

// 抽帧的失败原因。调用方用 errors.Is 判断，再决定给模型或用户什么说明。
var (
	ErrUnavailable = errors.New("当前环境不能处理视频（没有安装 ffmpeg）")
	ErrBusy        = errors.New("视频处理繁忙，请稍后再试")
	ErrTimeout     = errors.New("处理视频超时")
	ErrDecode      = errors.New("视频无法解码")
	ErrNoFrame     = errors.New("该时间点没有画面")
	ErrBadCount    = errors.New("抽帧数量不合法")
)

// Format 是输出图片的编码。
type Format string

// 支持的输出编码。
const (
	PNG  Format = "png"  // 无损，落帧用
	JPEG Format = "jpeg" // 体积小，看视频给模型用
)

// FrameOpts 是单帧输出选项。
type FrameOpts struct {
	Format  Format // 默认 PNG
	MaxEdge int    // 长边上限（像素），0 表示不缩放；只缩小不放大
}

// Frame 是抽出的一帧。
type Frame struct {
	At   float64 // 秒
	Data []byte
}

// Options 配置抽帧器。零值字段取默认值。
type Options struct {
	FFmpegPath    string        // 默认 "ffmpeg"
	FFprobePath   string        // 默认 "ffprobe"
	Timeout       time.Duration // 单次调用超时，默认 30 秒
	MaxConcurrent int           // 同时运行的 ffmpeg / ffprobe 数，默认 2
	Protocols     string        // ffmpeg 允许的输入协议，默认 file,http,https,tcp,tls,crypto
}

// Extractor 是抽帧器，可并发使用。
type Extractor struct {
	ffmpeg, ffprobe string
	timeout         time.Duration
	protocols       string
	sem             chan struct{}
}

// New 创建抽帧器。
func New(o Options) *Extractor {
	e := &Extractor{ffmpeg: o.FFmpegPath, ffprobe: o.FFprobePath, timeout: o.Timeout, protocols: o.Protocols}
	if e.ffmpeg == "" {
		e.ffmpeg = "ffmpeg"
	}
	if e.ffprobe == "" {
		e.ffprobe = "ffprobe"
	}
	if e.timeout <= 0 {
		e.timeout = defaultTimeout
	}
	if e.protocols == "" {
		e.protocols = defaultProtocols
	}
	n := o.MaxConcurrent
	if n <= 0 {
		n = defaultMaxConcurrent
	}
	e.sem = make(chan struct{}, n)
	return e
}

// Available 报告 ffmpeg 与 ffprobe 是否都能找到。
func (e *Extractor) Available() bool {
	if e == nil {
		return false
	}
	_, err1 := exec.LookPath(e.ffmpeg)
	_, err2 := exec.LookPath(e.ffprobe)
	return err1 == nil && err2 == nil
}

// LastAt 返回取尾帧用的时间点：略早于总时长。
func LastAt(duration float64) float64 {
	switch {
	case duration <= 0:
		return 0
	case duration <= lastMargin*2:
		return duration / 2
	default:
		return duration - lastMargin
	}
}

// Duration 返回视频时长（秒）。
func (e *Extractor) Duration(ctx context.Context, src string) (float64, error) {
	out, err := e.run(ctx, e.ffprobe, "-v", "error", "-protocol_whitelist", e.protocols,
		"-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", src)
	if err != nil {
		return 0, err
	}
	d, perr := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if perr != nil || d <= 0 {
		return 0, fmt.Errorf("%w: 读不到时长", ErrDecode)
	}
	return d, nil
}

// Frame 取 at 秒处的一帧。时间点超出视频末尾返回 ErrNoFrame。
func (e *Extractor) Frame(ctx context.Context, src string, at float64, o FrameOpts) ([]byte, error) {
	if at < 0 {
		at = 0
	}
	codec := "png"
	if o.Format == JPEG {
		codec = "mjpeg"
	}
	args := []string{"-v", "error", "-protocol_whitelist", e.protocols,
		"-ss", strconv.FormatFloat(at, 'f', 3, 64), "-i", src, "-frames:v", "1"}
	if o.MaxEdge > 0 {
		m := strconv.Itoa(o.MaxEdge)
		// 逗号要转义，否则会被当成滤镜分隔符；min 保证只缩小不放大。
		args = append(args, "-vf", `scale=w='if(gt(iw\,ih)\,min(`+m+`\,iw)\,-2)':h='if(gt(iw\,ih)\,-2\,min(`+m+`\,ih))'`)
	}
	args = append(args, "-c:v", codec, "-f", "image2pipe", "-")
	out, err := e.run(ctx, e.ffmpeg, args...)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNoFrame
	}
	return out, nil
}

// Sample 在整段视频里均匀取 n 帧（1 ≤ n ≤ MaxSampleFrames），同时返回视频时长。
// n ≥ 2 时包含首帧和略早于末尾的尾帧；n = 1 取中间一帧。
func (e *Extractor) Sample(ctx context.Context, src string, n int, o FrameOpts) ([]Frame, float64, error) {
	if n < 1 || n > MaxSampleFrames {
		return nil, 0, fmt.Errorf("%w: 一次最多 %d 帧", ErrBadCount, MaxSampleFrames)
	}
	dur, err := e.Duration(ctx, src)
	if err != nil {
		return nil, 0, err
	}
	frames := make([]Frame, 0, n)
	for _, at := range sampleTimes(dur, n) {
		data, err := e.Frame(ctx, src, at, o)
		if err != nil {
			return nil, 0, err
		}
		frames = append(frames, Frame{At: at, Data: data})
	}
	return frames, dur, nil
}

// sampleTimes 把 [0, LastAt(dur)] 均分成 n 个时间点；n = 1 取中间。
func sampleTimes(dur float64, n int) []float64 {
	if n == 1 {
		return []float64{dur / 2}
	}
	end := LastAt(dur)
	ts := make([]float64, n)
	for i := range ts {
		ts[i] = end * float64(i) / float64(n-1)
	}
	return ts
}

// run 在并发上限和超时内执行一个外部命令，返回标准输出。
func (e *Extractor) run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath(bin); err != nil {
		return nil, ErrUnavailable
	}
	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	default:
		return nil, ErrBusy
	}
	cctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	// 二进制路径来自配置，参数全部由本包拼接，不经 shell；输入协议已用白名单限制。
	cmd := exec.CommandContext(cctx, bin, args...) //nolint:gosec // 见上
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return nil, ErrTimeout
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > maxStderr {
			msg = msg[:maxStderr]
		}
		return nil, fmt.Errorf("%w: %s", ErrDecode, msg)
	}
	return stdout.Bytes(), nil
}
