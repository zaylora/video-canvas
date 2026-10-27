package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/provider"
)

// 看视频、截帧的限制。
const (
	maxVideoFrames   = videoframe.MaxSampleFrames // 一次最多看几帧、截几帧
	inspectFrameEdge = 1024                       // 看视频时每帧的长边（像素）：够模型看清，又不至于太费 token
	maxVideoSpool    = 512 << 20                  // 本地存储下，视频最大字节数（要先落成临时文件交给 ffmpeg）
	sameFrameWindow  = 1000                       // 判定「同一时间点」的精度（毫秒的倒数）
)

// VideoFrames 是抽帧能力，由 videoframe.Extractor 实现。声明在这里，服务只依赖接口。
type VideoFrames interface {
	Available() bool
	Duration(ctx context.Context, src string) (float64, error)
	Frame(ctx context.Context, src string, at float64, o videoframe.FrameOpts) ([]byte, error)
	Sample(ctx context.Context, src string, n int, o videoframe.FrameOpts) ([]videoframe.Frame, float64, error)
}

// FrameStore 把截出的一帧（PNG）存成用户素材，返回素材视图。
type FrameStore interface {
	SaveFrame(ctx context.Context, userID uint64, fileName string, png []byte) (*model.AssetView, error)
}

// manualHint 是视频处理出问题时给模型的退路：用户自己用功能区的「截取帧」。
const manualHint = "可以请用户在视频节点的功能区用「截取帧」手动截图。"

// videoFailure 把抽帧错误转成给模型看的说明。ffmpeg 的原始输出只进日志，不透给模型。
func videoFailure(err error) error {
	switch {
	case errors.Is(err, videoframe.ErrUnavailable):
		return fail("当前环境不能处理视频（服务器没有安装 ffmpeg）。" + manualHint)
	case errors.Is(err, videoframe.ErrBusy):
		return fail("视频处理繁忙，请稍后再试。")
	case errors.Is(err, videoframe.ErrTimeout):
		return fail("处理视频超时，视频可能太大或太长。可以稍后重试。" + manualHint)
	case errors.Is(err, videoframe.ErrNoFrame):
		return fail("该时间点没有画面，请换一个更靠前的时间点。")
	case errors.Is(err, videoframe.ErrDecode):
		logger.Warn("视频解码失败", zap.Error(err))
		return fail("这个视频无法解码，可能已损坏或格式不支持。" + manualHint)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	}
	logger.Error("视频处理失败", zap.Error(err))
	return errcode.ErrInternal
}

// requireFrames 确认有视频处理能力。
func (b *AgentBridge) requireFrames() error {
	if b.d.Frames == nil || !b.d.Frames.Available() {
		return videoFailure(videoframe.ErrUnavailable)
	}
	return nil
}

// openVideo 打开视频素材，返回可以交给 ffmpeg 的输入：对象存储给签名地址（不下载），
// 本地磁盘存储落成临时文件。cleanup 用完要调用。
func (b *AgentBridge) openVideo(ctx context.Context, userID uint64, assetID string) (src string, cleanup func(), err error) {
	noop := func() {}
	id, perr := strconv.ParseUint(assetID, 10, 64)
	if perr != nil || b.d.Assets == nil {
		return "", noop, errors.New("视频素材无效")
	}
	f, oerr := b.d.Assets.Open(ctx, userID, id)
	if errors.Is(oerr, provider.ErrAssetNotFound) {
		return "", noop, errors.New("视频素材不存在或已删除")
	}
	if oerr != nil {
		return "", noop, errcode.ErrInternal
	}
	defer f.Body.Close()
	if !strings.HasPrefix(f.Asset.MimeType, "video/") {
		return "", noop, fmt.Errorf("素材不是视频（%s）", f.Asset.MimeType)
	}
	if f.Remote && f.URL != "" {
		return f.URL, noop, nil
	}
	if f.Asset.ByteSize > maxVideoSpool {
		return "", noop, fmt.Errorf("视频太大（%d MB），超过 %d MB 的不能处理", f.Asset.ByteSize>>20, maxVideoSpool>>20)
	}
	tmp, cerr := os.CreateTemp("", "agent-video-*")
	if cerr != nil {
		return "", noop, errcode.ErrInternal
	}
	remove := func() { _ = os.Remove(tmp.Name()) }
	n, cerr := io.Copy(tmp, io.LimitReader(f.Body, maxVideoSpool+1))
	if closeErr := tmp.Close(); cerr == nil {
		cerr = closeErr
	}
	if cerr != nil {
		remove()
		return "", noop, errcode.ErrInternal
	}
	if n > maxVideoSpool {
		remove()
		return "", noop, fmt.Errorf("视频太大，超过 %d MB 的不能处理", maxVideoSpool>>20)
	}
	return tmp.Name(), remove, nil
}

// toolInspectVideo 看视频：均匀抽几帧随结果交给模型。只读，帧只在这一轮的上下文里出现，不保存、不写进历史。
func (b *AgentBridge) toolInspectVideo(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		NodeID string `json:"nodeId"`
		Count  *int   `json:"count"`
	}
	if err := json.Unmarshal(tc.args, &a); err != nil {
		return nil, fail("参数格式不对：%v", err)
	}
	count := maxVideoFrames
	if a.Count != nil {
		count = *a.Count
	}
	if count < 1 || count > maxVideoFrames {
		return nil, fail("count 需要 1 到 %d", maxVideoFrames)
	}
	snap, _, err := b.resolveModel(ctx, tc.sess.modelKey)
	if err != nil {
		return nil, err
	}
	if !snap.Model.Capabilities.Vision {
		return nil, fail("当前模型不能看图，也就看不了视频内容。可以用 canvas_get_state 读提示词来判断，或请用户换一个支持看图的模型。")
	}
	if err := b.requireFrames(); err != nil {
		return nil, err
	}
	ref, err := b.d.Canvas.VideoRef(ctx, tc.run, a.NodeID)
	if err != nil {
		return nil, err
	}
	src, cleanup, err := b.openVideo(ctx, tc.run.UserID, ref.AssetID)
	defer cleanup()
	if err != nil {
		return nil, fail("节点 %q（%s）：%v", ref.NodeID, ref.Label, err)
	}
	frames, dur, err := b.d.Frames.Sample(ctx, src, count, videoframe.FrameOpts{Format: videoframe.JPEG, MaxEdge: inspectFrameEdge})
	if err != nil {
		return nil, videoFailure(err)
	}
	out := &toolOut{summary: fmt.Sprintf("查看视频（%d 帧）", len(frames)), extra: map[string]any{"node_ids": []string{ref.NodeID}}}
	var text strings.Builder
	fmt.Fprintf(&text, "视频节点 %s「%s」，时长 %.1f 秒。已附上 %d 帧，按顺序是：", ref.NodeID, ref.Label, dur, len(frames))
	for i, f := range frames {
		out.images = append(out.images, ToolImage{MimeType: "image/jpeg", Data: base64.StdEncoding.EncodeToString(f.Data)})
		fmt.Fprintf(&text, "\n第 %d 帧：%.1f 秒", i+1, f.At)
	}
	out.content = text.String()
	return out, nil
}

// captureFrameArg 是 canvas_capture_frame 参数里的一帧。
type captureFrameArg struct {
	At        json.RawMessage `json:"at"`        // "first"、"last" 或秒数
	ConnectTo []string        `json:"connectTo"` // 作为参考连到这些节点；不写就只放在画布上
}

// frameReq 是截帧请求里的一帧，at 已经换算成秒。
type frameReq struct {
	at        float64
	name      string // 首帧 / 尾帧 / 3.5 秒，用于节点标题和文件名
	connectTo []string
}

// parseFrameAt 读出 at：first、last 或非负的秒数（数字或数字字符串）。
func parseFrameAt(raw json.RawMessage) (kind string, sec float64, err error) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return "", 0, fail(`at 只能是 "first"、"last" 或秒数`)
	}
	switch x := v.(type) {
	case string:
		if x == "first" || x == "last" {
			return x, 0, nil
		}
		sec, err = strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return "", 0, fail(`at 只能是 "first"、"last" 或秒数`)
		}
	case float64:
		sec = x
	default:
		return "", 0, fail(`at 只能是 "first"、"last" 或秒数`)
	}
	if math.IsNaN(sec) || math.IsInf(sec, 0) {
		return "", 0, fail(`at 只能是 "first"、"last" 或秒数`)
	}
	if sec < 0 {
		return "", 0, fail("at 不能为负数")
	}
	return "", sec, nil
}

// toolCaptureFrame 截帧：把视频里的指定画面存成用户素材，在画布上建成图片节点。
// 是否截、截几张、哪些帧连给下一镜当参考，都由模型决定；这里只校验上限和合法性。
func (b *AgentBridge) toolCaptureFrame(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		NodeID string            `json:"nodeId"`
		Frames []captureFrameArg `json:"frames"`
	}
	if err := json.Unmarshal(tc.args, &a); err != nil {
		return nil, fail("参数格式不对：%v", err)
	}
	if len(a.Frames) < 1 || len(a.Frames) > maxVideoFrames {
		return nil, fail("frames 需要 1 到 %d 项（一次最多截 %d 帧）", maxVideoFrames, maxVideoFrames)
	}
	kinds := make([]string, len(a.Frames))
	secs := make([]float64, len(a.Frames))
	for i, f := range a.Frames {
		var err error
		if kinds[i], secs[i], err = parseFrameAt(f.At); err != nil {
			return nil, err
		}
	}
	if err := b.requireFrames(); err != nil {
		return nil, err
	}
	if b.d.FrameStore == nil {
		return nil, videoFailure(videoframe.ErrUnavailable)
	}
	// 先不写任何东西地校验视频节点和连线目标，再抽帧、存素材
	connects := make([][]string, len(a.Frames))
	for i, f := range a.Frames {
		connects[i] = f.ConnectTo
	}
	ref, err := b.d.Canvas.CheckFrames(ctx, tc.run, a.NodeID, connects)
	if err != nil {
		return nil, err
	}
	src, cleanup, err := b.openVideo(ctx, tc.run.UserID, ref.AssetID)
	defer cleanup()
	if err != nil {
		return nil, fail("节点 %q（%s）：%v", ref.NodeID, ref.Label, err)
	}
	dur, err := b.d.Frames.Duration(ctx, src)
	if err != nil {
		return nil, videoFailure(err)
	}
	reqs, err := resolveFrames(kinds, secs, a.Frames, dur)
	if err != nil {
		return nil, err
	}
	specs, err := b.extractFrames(ctx, tc.run.UserID, src, ref.Label, reqs)
	if err != nil {
		return nil, err
	}
	res, err := b.d.Canvas.AddFrames(ctx, tc.run, tc.id, ref.NodeID, specs)
	if err != nil {
		return nil, err
	}
	type frameOut struct {
		NodeID      string   `json:"node_id"`
		At          float64  `json:"at"`
		Label       string   `json:"label"`
		ConnectedTo []string `json:"connected_to,omitempty"`
	}
	list := make([]frameOut, len(reqs))
	ids := make([]string, len(reqs))
	for i, r := range reqs {
		ids[i] = res.IDMap[fmt.Sprintf("frame%d", i)]
		list[i] = frameOut{NodeID: ids[i], At: r.at, Label: specs[i].Label, ConnectedTo: r.connectTo}
	}
	content, err := jsonOut(map[string]any{"ok": true, "frames": list, "revision": res.Revision})
	extra := map[string]any{"node_ids": ids, "created": res.Created}
	return &toolOut{content: content, summary: fmt.Sprintf("截取 %d 帧", len(reqs)), extra: extra}, err
}

// extractFrames 逐帧抽出 PNG（原分辨率）并存成用户素材，返回可以建节点的帧图描述。
func (b *AgentBridge) extractFrames(ctx context.Context, userID uint64, src, videoLabel string, reqs []frameReq) ([]canvasgraph.FrameSpec, error) {
	specs := make([]canvasgraph.FrameSpec, 0, len(reqs))
	for _, r := range reqs {
		png, err := b.d.Frames.Frame(ctx, src, r.at, videoframe.FrameOpts{Format: videoframe.PNG})
		if err != nil {
			return nil, videoFailure(err)
		}
		view, err := b.d.FrameStore.SaveFrame(ctx, userID, fmt.Sprintf("frame-%.1fs.png", r.at), png)
		if err != nil {
			return nil, err
		}
		specs = append(specs, canvasgraph.FrameSpec{
			Label: videoLabel + " · " + r.name, AssetID: strconv.FormatUint(view.ID, 10), URL: view.URL,
			FileName: view.FileName, Width: view.Width, Height: view.Height, ConnectTo: r.connectTo,
		})
	}
	return specs, nil
}

// resolveFrames 把请求里的 first / last / 秒数换成具体的秒，检查不超出时长，并把同一时间点的帧合并
// （连线目标取并集），免得同一个画面存好几份。
func resolveFrames(kinds []string, secs []float64, in []captureFrameArg, dur float64) ([]frameReq, error) {
	var out []frameReq
	index := map[int64]int{}
	for i := range in {
		var at float64
		var name string
		switch kinds[i] {
		case "first":
			at, name = 0, "首帧"
		case "last":
			at, name = videoframe.LastAt(dur), "尾帧"
		default:
			at = secs[i]
			if at > dur {
				return nil, fail("at=%v 超出了视频时长 %.1f 秒", at, dur)
			}
			name = fmt.Sprintf("%.1f 秒", at)
		}
		key := int64(math.Round(at * sameFrameWindow))
		if j, ok := index[key]; ok {
			out[j].connectTo = unionStrings(out[j].connectTo, in[i].ConnectTo)
			continue
		}
		index[key] = len(out)
		out = append(out, frameReq{at: at, name: name, connectTo: unionStrings(nil, in[i].ConnectTo)})
	}
	return out, nil
}

// unionStrings 把 more 里 base 没有的字符串按顺序追加到 base。
func unionStrings(base, more []string) []string {
	for _, s := range more {
		dup := false
		for _, b := range base {
			if b == s {
				dup = true
				break
			}
		}
		if !dup {
			base = append(base, s)
		}
	}
	return base
}
