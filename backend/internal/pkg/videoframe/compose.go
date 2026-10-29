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

// composeInfo 只读取转码所需的首视频流尺寸、时长和音轨存在性。
type composeInfo struct {
	width, height int
	duration      float64
	audio         bool
}

// Compose 将本地片段统一为第一段分辨率、30fps、H.264/AAC 后按给定顺序拼接。
// 无音轨补立体声静音；整个操作受 Options.Timeout 限制，失败清理半成片，所有路径清理中间文件。
func (e *Extractor) Compose(ctx context.Context, sources []string, destination string) (err error) {
	if len(sources) == 0 {
		return fmt.Errorf("%w: 没有待合成视频", ErrDecode)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	dir, err := os.MkdirTemp(filepath.Dir(destination), "vc-concat-*")
	if err != nil {
		return fmt.Errorf("创建合成临时目录失败：%w", err)
	}
	defer func() {
		_ = os.RemoveAll(dir) // 中间文件没有外部引用，无论成功或失败均回收。
		if err != nil {
			_ = os.Remove(destination) // 不保留可能被误认为可播放成片的半成品。
		}
	}()
	var first composeInfo
	var playlist strings.Builder
	for i, source := range sources {
		info, probeErr := e.composeProbe(ctx, source)
		if probeErr != nil {
			return probeErr
		}
		if i == 0 {
			first = info
		}
		name := fmt.Sprintf("part-%06d.mp4", i)
		if err := e.composeTranscode(ctx, source, filepath.Join(dir, name), first, info); err != nil {
			return err
		}
		fmt.Fprintf(&playlist, "file '%s'\n", name)
	}
	listPath := filepath.Join(dir, "concat.txt")
	if err := os.WriteFile(listPath, []byte(playlist.String()), 0600); err != nil {
		return fmt.Errorf("写入合成清单失败：%w", err)
	}
	_, err = e.run(ctx, e.ffmpeg, "-nostdin", "-v", "error", "-f", "concat", "-safe", "1", "-i", listPath,
		"-map", "0:v:0", "-map", "0:a:0", "-c", "copy", "-movflags", "+faststart", "-y", destination)
	return err
}

// composeProbe 使用与抽帧相同的二进制定位、协议白名单和受限执行器。
func (e *Extractor) composeProbe(ctx context.Context, source string) (composeInfo, error) {
	out, err := e.run(ctx, e.ffprobe, "-v", "error", "-protocol_whitelist", "file", "-show_entries", "stream=codec_type,width,height,duration:format=duration", "-of", "json", source)
	if err != nil {
		return composeInfo{}, err
	}
	var probe struct {
		Streams []struct {
			Type     string `json:"codec_type"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
			Duration string `json:"duration"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		return composeInfo{}, fmt.Errorf("%w: 视频信息无法解析", ErrDecode)
	}
	info := composeInfo{}
	for _, stream := range probe.Streams {
		if stream.Type == "audio" {
			info.audio = true
		}
		if stream.Type == "video" && info.width == 0 {
			info.width, info.height = stream.Width, stream.Height
			info.duration, _ = strconv.ParseFloat(stream.Duration, 64)
		}
	}
	if info.duration <= 0 {
		info.duration, _ = strconv.ParseFloat(probe.Format.Duration, 64)
	}
	if info.width <= 0 || info.height <= 0 || info.duration <= 0 {
		return composeInfo{}, fmt.Errorf("%w: 视频没有有效的尺寸或时长", ErrDecode)
	}
	return info, nil
}

// composeTranscode 将所有片段转换成相同的尺寸、帧率、像素格式、音频采样率与声道布局。
func (e *Extractor) composeTranscode(ctx context.Context, source, destination string, first, info composeInfo) error {
	args := []string{"-nostdin", "-v", "error", "-protocol_whitelist", "file", "-i", source}
	audio := "0:a:0"
	if !info.audio {
		args = append(args, "-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=48000")
		audio = "1:a:0"
	}
	filter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,fps=30", first.width, first.height, first.width, first.height)
	args = append(args, "-map", "0:v:0", "-map", audio, "-vf", filter,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-ar", "48000", "-ac", "2", "-af", "apad",
		"-t", strconv.FormatFloat(info.duration, 'f', 6, 64), "-video_track_timescale", "90000", "-y", destination)
	_, err := e.run(ctx, e.ffmpeg, args...)
	return err
}
