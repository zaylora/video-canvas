package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

// smokeStorageSource 为独立验收提供临时本地对象存储，不读取或改动开发环境配置。
type smokeStorageSource struct{ dir string }

func (s smokeStorageSource) DefaultID(context.Context) (uint64, error) { return 1, nil }
func (s smokeStorageSource) Entry(context.Context, uint64) (storage.Entry, error) {
	return storage.Entry{ID: 1, Version: 1, Provider: storage.ProviderLocal, Local: config.LocalStorage{Dir: s.dir}}, nil
}

// verifyCompose 验证实际 HTTP→River→ffmpeg→存储→成片视图，以及跳过、过期和零扣费。
func verifyCompose(ctx context.Context, e env, id string) {
	status, b := request(e.url, "POST", "/dramas/"+id+"/jobs", map[string]any{"step": "compose", "episodes": []int{2}})
	require(status == 400 && b["code"].(float64) == 67010, fmt.Sprint(status, b))
	first := runCompose(e.url, id)
	require(fmt.Sprint(first["skipped_seqs"]) == "[1]", fmt.Sprint(first))
	verifyFinalVideo(ctx, e, first["asset"].(map[string]any), 1)
	var credit model.UserCredit
	must(e.db.WithContext(ctx).First(&credit, "user_id = ?", 1).Error)
	require(credit.Balance == 93 && credit.Frozen == 0, "合成不得扣费或冻结积分")

	path := "/dramas/" + id + "/episodes/1/segments"
	_, b = request(e.url, "GET", path, nil)
	items := data(b)["items"].([]any)
	a, second := items[0].(map[string]any)["id"].(string), items[1].(map[string]any)["id"].(string)
	status, b = request(e.url, "POST", "/drama-segments/"+second+"/generate", map[string]any{"model_id": "smoke-video", "input": map[string]any{"prompt": "静音色块", "op": "t2v"}})
	require(status == 202, fmt.Sprint(status, b))
	awaitChosen(e.url, path, second)
	status, b = request(e.url, "POST", path, map[string]any{"prompt": "跳过这个未采用片段", "seconds": 1})
	require(status == 201, fmt.Sprint(status, b))
	third := data(b)["id"].(string)
	result := runCompose(e.url, id)
	require(fmt.Sprint(result["skipped_seqs"]) == "[2]", fmt.Sprint(result))
	asset := result["asset"].(map[string]any)
	verifyFinalVideo(ctx, e, asset, 2)
	_, b = request(e.url, "GET", "/dramas/"+id+"/episodes/1", nil)
	require(data(b)["final_stale"] == false && data(b)["final"].(map[string]any)["id"] == asset["id"], fmt.Sprint(b))
	status, b = request(e.url, "PUT", path+"/order", map[string]any{"ids": []string{second, a, third}})
	require(status == 200, fmt.Sprint(status, b))
	_, b = request(e.url, "GET", "/dramas/"+id, nil)
	ep := data(b)["episodes"].([]any)[0].(map[string]any)
	require(ep["final_stale"] == true && ep["final"] != nil, fmt.Sprint(ep))
	status, b = request(e.url, "DELETE", "/drama-segments/"+third, nil)
	require(status == 204, fmt.Sprint(status, b))
	must(e.db.WithContext(ctx).First(&credit, "user_id = ?", 1).Error)
	require(credit.Balance == 92 && credit.Frozen == 0, fmt.Sprintf("合成扣费错误: %+v", credit))
	rec, err := repository.NewGenerationTaskRepository(e.db).Reconcile(ctx, 1)
	must(err)
	require(rec.FrozenDiff() == 0 && rec.BalanceDiff() == 0, fmt.Sprintf("三期流水不守恒: %+v", rec))
}

// runCompose 通过公开接口提交并等待队列结束，返回实际持久化的合成结果。
func runCompose(base, id string) map[string]any {
	status, b := request(base, "POST", "/dramas/"+id+"/estimate", map[string]any{"step": "compose", "episodes": []int{1}})
	require(status == 200 && data(b)["credits"].(float64) == 0, fmt.Sprint(status, b))
	status, b = request(base, "POST", "/dramas/"+id+"/jobs", map[string]any{"step": "compose", "episodes": []int{1}})
	require(status == 202, fmt.Sprint(status, b))
	jobID := data(b)["id"]
	awaitReady(base, id)
	_, b = request(base, "GET", "/dramas/"+id+"/jobs", nil)
	for _, value := range data(b)["items"].([]any) {
		job := value.(map[string]any)
		if job["id"] != jobID {
			continue
		}
		require(job["status"] == "succeeded" && job["credits"].(float64) == 0, fmt.Sprint(job))
		return job["result"].(map[string]any)
	}
	panic("合成任务未保存在任务列表")
}

// awaitChosen 等待指定片段从真实任务状态出口写回采用结果。
func awaitChosen(base, path, sid string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, b := request(base, "GET", path, nil)
		for _, item := range data(b)["items"].([]any) {
			segment := item.(map[string]any)
			if segment["id"] == sid && segment["chosen_task_id"] != "" && segment["status"] == "done" {
				return
			}
		}
		select {
		case <-ctx.Done():
			panic("等待第二段静音视频超时")
		case <-ticker.C:
		}
	}
}

// verifyFinalVideo 探测落库素材并解码成片两端，证明输出为可播放的 H.264/AAC 视频。
func verifyFinalVideo(ctx context.Context, e env, asset map[string]any, seconds float64) {
	var row model.Asset
	must(e.db.WithContext(ctx).Where("user_id = ?", 1).First(&row, uint64(asset["id"].(float64))).Error)
	require(row.Kind == model.KindVideo && row.Source != model.AssetSourceGenerated && row.TaskID == nil, fmt.Sprint(row))
	filename := filepath.Join(e.mediaDir, filepath.FromSlash(row.StorageKey))
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, "ffprobe", "-v", "error", "-show_streams", "-of", "json", filename).Output() //nolint:gosec // 路径来自本进程创建的临时目录
	must(err)
	var probe struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			Rate      string `json:"r_frame_rate"`
		} `json:"streams"`
	}
	must(json.Unmarshal(out, &probe))
	video, audio := false, false
	for _, stream := range probe.Streams {
		if stream.CodecType == "video" {
			video = stream.CodecName == "h264" && stream.Width == 160 && stream.Height == 120 && stream.Rate == "30/1"
		}
		audio = audio || stream.CodecType == "audio" && stream.CodecName == "aac"
	}
	require(video && audio, string(out))
	frames := videoframe.New(videoframe.Options{})
	duration, err := frames.Duration(ctx, filename)
	must(err)
	require(math.Abs(duration-seconds) < 0.2, fmt.Sprint(duration))
	_, err = frames.Frame(ctx, filename, 0.25, videoframe.FrameOpts{})
	must(err)
	_, err = frames.Frame(ctx, filename, seconds-0.25, videoframe.FrameOpts{})
	must(err)
	require(strings.HasPrefix(asset["url"].(string), "/files/"), fmt.Sprint(asset))
}
