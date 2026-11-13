package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"go.uber.org/zap"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// imageDataURLs 读取商品图，编码成 data URL 交给模型看。素材不存在或读取失败返回给用户看的原因。
func (s *MarketingService) imageDataURLs(ctx context.Context, userID uint64, ids []uint64) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		f, err := s.assets.Open(ctx, userID, id)
		if errors.Is(err, provider.ErrAssetNotFound) {
			return nil, &prepareError{msg: "商品图不存在或已删除"}
		}
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(f.Body, marketingMaxImageBytes+1))
		_ = f.Body.Close()
		if err != nil {
			return nil, err
		}
		if len(body) > marketingMaxImageBytes {
			return nil, &prepareError{msg: "商品图太大，不能交给模型识别"}
		}
		mime := f.Asset.MimeType
		if !strings.HasPrefix(mime, "image/") {
			mime = "image/jpeg"
		}
		out = append(out, "data:"+mime+";base64,"+base64.StdEncoding.EncodeToString(body))
	}
	return out, nil
}

// reverseInput 组装拆解参考片的输入。看视频的方式：
//   - 所用文本模型 Video=true：把视频内联给模型（本地 ≤20MB，对象存储传签名地址）；本地超过 20MB 退回截帧；
//   - 否则退回 Vision + 截帧：镜头切点各 1 帧加每秒 2 帧，上限 32 帧，并给轮次加「拆解未包含声音」警告；
//   - 都不可用（模型不能看视频又没有 ffmpeg）失败，原因「服务器无法读取参考视频」。
func (s *MarketingService) reverseInput(ctx context.Context, round *model.MarketingRound, brief marketing.Brief, snap *provider.Snapshot) (*stepPrep, error) {
	if round.RefVideoAssetID == nil {
		return nil, &prepareError{msg: "这一轮没有参考视频"}
	}
	f, err := s.assets.Open(ctx, round.UserID, *round.RefVideoAssetID)
	if errors.Is(err, provider.ErrAssetNotFound) {
		return nil, &prepareError{msg: "参考视频不存在或已删除"}
	}
	if err != nil {
		return nil, err
	}
	defer f.Body.Close()
	caps := snap.Model.Capabilities
	var video *llmgateway.Video
	if caps.Video {
		if video, err = inlineVideo(f); err != nil {
			return nil, err
		}
	}
	m := reverseMedia{video: video, duration: float64(f.Asset.DurationMs) / 1000}
	if s.framesAvailable() && (video != nil || caps.Vision) {
		if err := s.probeVideo(ctx, f, &m); err != nil && video == nil {
			return nil, err
		}
	}
	prep := &stepPrep{}
	if video == nil {
		if len(m.frames) == 0 {
			return nil, &prepareError{msg: "服务器无法读取参考视频"}
		}
		prep.warnings = []string{marketingFramesWarning}
	}
	prep.input = marketing.ReverseInput(brief, m.duration, m.cuts, video, m.frames)
	return prep, nil
}

// reverseMedia 是拆解要给模型的视频材料：内联视频，或切点加截帧，以及时长。
type reverseMedia struct {
	video    *llmgateway.Video
	duration float64
	cuts     []float64
	frames   []string
}

// probeVideo 用 ffmpeg 补全时长、镜头切点，模型不能直接看视频时再截帧。
func (s *MarketingService) probeVideo(ctx context.Context, f *provider.AssetFile, m *reverseMedia) error {
	return s.withVideoSource(f, func(src string) error {
		if m.duration <= 0 {
			if d, err := s.frames.Duration(ctx, src); err == nil {
				m.duration = d
			}
		}
		if c, err := s.frames.Cuts(ctx, src, 0); err == nil {
			m.cuts = c
		}
		if m.video != nil {
			return nil
		}
		var err error
		m.frames, err = s.sampleFrames(ctx, src, m.duration, m.cuts)
		return err
	})
}

// inlineVideo 把视频交给模型：对象存储传签名地址，本地存储读进内存转 Base64；本地超过上限返回 nil（调用方退回截帧）。
func inlineVideo(f *provider.AssetFile) (*llmgateway.Video, error) {
	v := &llmgateway.Video{MimeType: f.Asset.MimeType}
	if f.Remote && f.URL != "" {
		v.URL = f.URL
		return v, nil
	}
	if f.Asset.ByteSize > marketingMaxInlineVideo {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(f.Body, marketingMaxInlineVideo+1))
	if err != nil {
		return nil, err
	}
	if len(body) > marketingMaxInlineVideo {
		return nil, nil
	}
	v.Data = base64.StdEncoding.EncodeToString(body)
	return v, nil
}

// withVideoSource 给 ffmpeg 一个可读的视频来源：对象存储用签名地址，本地存储落成临时文件，用完清理。
func (s *MarketingService) withVideoSource(f *provider.AssetFile, fn func(src string) error) error {
	if f.Remote && f.URL != "" {
		return fn(f.URL)
	}
	if f.Asset.ByteSize > marketingMaxVideoSpool {
		return &prepareError{msg: "参考视频太大，服务器无法读取"}
	}
	tmp, err := os.CreateTemp("", "marketing-ref-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	n, err := io.Copy(tmp, io.LimitReader(f.Body, marketingMaxVideoSpool+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n > marketingMaxVideoSpool {
		return &prepareError{msg: "参考视频太大，服务器无法读取"}
	}
	return fn(tmp.Name())
}

// sampleFrames 按取帧计划（切点各 1 帧加每秒 2 帧，上限 32）截帧并编码成 JPEG data URL。
func (s *MarketingService) sampleFrames(ctx context.Context, src string, duration float64, cuts []float64) ([]string, error) {
	if duration <= 0 {
		return nil, &prepareError{msg: "读不到参考视频的时长"}
	}
	times := videoframe.PlanTimes(duration, cuts, videoframe.MaxPlanFrames)
	got, err := s.frames.FramesAt(ctx, src, times, videoframe.FrameOpts{Format: videoframe.JPEG, MaxEdge: marketingFrameEdge})
	if err != nil {
		logger.Warn("参考视频截帧失败", zap.Error(err))
		return nil, &prepareError{msg: "服务器无法读取参考视频"}
	}
	out := make([]string, 0, len(got))
	for _, fr := range got {
		out = append(out, "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(fr.Data))
	}
	return out, nil
}

// assetViewOrNil 读取素材视图；素材已被删除返回 nil（视图上显示为没有），其他错误原样返回。
func (s *MarketingService) assetViewOrNil(ctx context.Context, userID, id uint64) (*model.AssetView, error) {
	v, err := s.assets.View(ctx, userID, id)
	var biz *errcode.Error
	if errors.Is(err, repository.ErrNotFound) || errors.Is(err, provider.ErrAssetNotFound) || (errors.As(err, &biz) && biz.Code == errcode.ErrAssetNotFound.Code) {
		return nil, nil //nolint:nilnil // 素材不存在是正常的「没有」，不是错误
	}
	if err != nil {
		return nil, fmt.Errorf("读取素材失败：%w", err)
	}
	return v, nil
}
