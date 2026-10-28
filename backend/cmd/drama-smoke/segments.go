package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"video-canvas/internal/handler"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// smokeMediaOutput 仅替代模型调用，生成任务的队列、结算、素材归属与片段回写仍走真实实现。
func smokeMediaOutput(ctx context.Context, db *gorm.DB, dir, kind string, task provider.TaskRef, audio bool) (*provider.SubmitResult, error) {
	mime, ext := "video/mp4", ".mp4"
	if kind == model.KindImage {
		mime, ext = "image/png", ".png"
	}
	key := fmt.Sprintf("smoke/%d%s", task.ID, ext)
	filename := filepath.Join(dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(filename), 0o750); err != nil {
		return nil, err
	}
	if err := writeSmokeMedia(ctx, filename, kind, audio); err != nil {
		return nil, err
	}
	info, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}
	asset := model.Asset{UserID: task.UserID, Kind: kind, StorageID: 1, StorageKey: key, MimeType: mime, ByteSize: info.Size(), Source: model.AssetSourceGenerated, TaskID: &task.ID}
	if err := db.WithContext(ctx).Create(&asset).Error; err != nil {
		return nil, err
	}
	return &provider.SubmitResult{Immediate: &provider.QueryResult{Status: provider.StatusSucceeded, Outputs: []provider.Output{{Type: provider.OutputAsset, AssetID: asset.ID, AssetURL: "/files/" + asset.StorageKey, MediaType: kind}}}}, nil
}

// writeSmokeMedia 现生成真实小样本，视频的一段带音轨、一段静音，并故意使用不同尺寸。
func writeSmokeMedia(ctx context.Context, filename, kind string, audio bool) error {
	if kind == model.KindImage {
		f, err := os.Create(filename) //nolint:gosec // 路径来自本进程创建的临时目录
		if err != nil {
			return err
		}
		err = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 2)))
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	color, size := "blue", "240x160"
	if audio {
		color, size = "red", "160x120"
	}
	args := []string{"-v", "error", "-f", "lavfi", "-i", "color=c=" + color + ":s=" + size + ":r=12:d=1"}
	if audio {
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:a", "aac")
	}
	args = append(args, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-y", filename)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("生成验收视频失败: %w: %s", err, out)
	}
	return nil
}

// smokeStoryboardOutput 为实际分镜步骤提供有资产引用、完整镜头表和精确时长的模型输出。
func smokeStoryboardOutput(vars map[string]json.RawMessage) string {
	var assets []struct {
		NodeID string `json:"node_id"`
		Name   string `json:"name"`
	}
	must(json.Unmarshal(vars["assets"], &assets))
	require(len(assets) == 3, "分镜未收到本集全部资产")
	prompt := "母亲在老宅拿起玉佩。"
	if assets[0].NodeID != "" {
		prompt = fmt.Sprintf("@[母亲](%s)在老宅拿起玉佩。", assets[0].NodeID)
	}
	raw, err := json.Marshal(map[string]any{"segments": []map[string]any{
		{"seconds": 8, "prompt": prompt, "shots": []map[string]any{{"seconds": 8, "shot_size": "中景", "camera": "平视固定", "action": "母亲拿起玉佩", "dialogue": "我回来了"}}},
		{"seconds": 4, "prompt": "玉佩反射窗外光线。", "shots": []map[string]any{{"seconds": 4, "shot_size": "特写", "camera": "缓慢推进", "action": "玉佩晃动", "dialogue": ""}}},
	}})
	must(err)
	return string(raw)
}

// registerSegmentRoutes 与生产注册相同片段入口，验收HTTP绑定和业务层的组合行为。
func registerSegmentRoutes(r *gin.Engine, svc *service.DramaSegmentService) {
	h := handler.NewDramaSegmentHandler(svc)
	base := "/dramas/:id/episodes/:no/segments"
	r.GET(base, h.List)
	r.POST(base, h.Create)
	r.PUT(base+"/order", h.Order)
	r.PATCH("/drama-segments/:sid", h.Update)
	r.DELETE("/drama-segments/:sid", h.Delete)
	r.POST("/drama-segments/:sid/generate", h.Generate)
	r.POST("/drama-segments/:sid/keyframe", h.Keyframe)
}

// awaitSegment 轮询片段真实任务投影，直到采用视频和首帧均已就绪。
func awaitSegment(base, path string) map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, b := request(base, "GET", path, nil)
		require(status == 200, fmt.Sprint(b))
		segment := data(b)["items"].([]any)[0].(map[string]any)
		if segment["status"] == "error" {
			panic(fmt.Sprint(segment))
		}
		if segment["status"] == "done" && segment["chosen_task_id"] != "" && segment["keyframe"] != nil {
			return segment
		}
		select {
		case <-ctx.Done():
			panic("等待片段生成超时")
		case <-ticker.C:
		}
	}
}

// verifyStoryboardAndGeneration 从HTTP提交分镜，验证片段编辑、排序和真实生成任务的完整链路。
func verifyStoryboardAndGeneration(ctx context.Context, e env, id, canvasID string) {
	status, b := request(e.url, "POST", "/dramas/"+id+"/jobs", map[string]any{"step": "storyboard", "episodes": []int{1}, "max_seconds": 8})
	require(status == 202, fmt.Sprint(status, b))
	awaitReady(e.url, id)
	path := "/dramas/" + id + "/episodes/1/segments"
	status, b = request(e.url, "GET", path, nil)
	require(status == 200, fmt.Sprint(b))
	items := data(b)["items"].([]any)
	require(len(items) == 2, fmt.Sprint(items))
	sid := items[0].(map[string]any)["id"].(string)
	verifySegmentEditing(e.url, path, sid, items)
	status, b = request(e.url, "POST", "/drama-segments/"+sid+"/keyframe", map[string]any{"model_id": "smoke-image", "input": map[string]any{"prompt": "首帧图", "op": "t2i"}})
	require(status == 202, fmt.Sprint(status, b))
	verifyTaskBinding(data(b), "segkf:"+sid, canvasID)
	status, b = request(e.url, "POST", "/drama-segments/"+sid+"/generate", map[string]any{"model_id": "smoke-video", "input": map[string]any{"prompt": "母亲拿起玉佩", "op": "t2v"}})
	require(status == 202, fmt.Sprint(status, b))
	verifyTaskBinding(data(b), "seg:"+sid, canvasID)
	awaitStoredSegment(ctx, e.db, sid)
	segment := awaitSegment(e.url, path)
	require(len(segment["takes"].([]any)) == 1, fmt.Sprint(segment))
	status, b = request(e.url, "POST", "/dramas/"+id+"/jobs", map[string]any{"step": "storyboard", "episodes": []int{1}})
	require(status == 409 && b["code"].(float64) == 67013, fmt.Sprint(status, b))
	_, b = request(e.url, "GET", "/dramas/"+id+"/episodes/1", nil)
	ep := data(b)
	require(ep["storyboard_status"] == "ready" && ep["segment_count"].(float64) == 2 && ep["segment_seconds"].(float64) == 12 && ep["done_segment_count"].(float64) == 1, fmt.Sprint(ep))
	rec, err := repository.NewGenerationTaskRepository(e.db).Reconcile(ctx, 1)
	must(err)
	require(rec.FrozenDiff() == 0 && rec.BalanceDiff() == 0, fmt.Sprintf("二期流水不守恒: %+v", rec))
	var credit model.UserCredit
	must(e.db.WithContext(ctx).First(&credit, "user_id = ?", 1).Error)
	require(credit.Balance == 93 && credit.Frozen == 0, fmt.Sprintf("二期扣费错误: %+v", credit))
	e.events.verify(sid)
}

// verifyTaskBinding 验证服务端设置的生成节点前缀和短剧画布归属。
func verifyTaskBinding(response map[string]any, nodeID, canvasID string) {
	task := response["task"].(map[string]any)
	require(task["node_id"] == nodeID && task["canvas_id"] == canvasID, fmt.Sprint(task))
}

// verifySegmentEditing 验证片段追加、删除、revision冲突和全量排序语义。
func verifySegmentEditing(base, path, sid string, items []any) {
	status, b := request(base, "POST", path, map[string]any{"prompt": "临时片段", "seconds": 5})
	require(status == 201, fmt.Sprint(status, b))
	added := data(b)["id"].(string)
	status, b = request(base, "DELETE", "/drama-segments/"+added, nil)
	require(status == 204, fmt.Sprint(status, b))
	status, b = request(base, "PATCH", "/drama-segments/"+sid, map[string]any{"revision": 999, "prompt": "不能覆盖"})
	require(status == 409 && b["code"].(float64) == 30002, fmt.Sprint(status, b))
	status, b = request(base, "PUT", path+"/order", map[string]any{"ids": []string{sid}})
	require(status == 400 && b["code"].(float64) == 67012, fmt.Sprint(status, b))
	second := items[1].(map[string]any)["id"].(string)
	status, b = request(base, "PUT", path+"/order", map[string]any{"ids": []string{second, sid}})
	require(status == 200 && data(b)["items"].([]any)[0].(map[string]any)["id"] == second, fmt.Sprint(status, b))
	status, b = request(base, "PUT", path+"/order", map[string]any{"ids": []string{sid, second}})
	require(status == 200, fmt.Sprint(status, b))
}

// awaitStoredSegment 在读取片段接口前检查数据库，保证成功回写来自任务状态出口。
func awaitStoredSegment(ctx context.Context, db *gorm.DB, sid string) {
	id, ok := idcodec.Decode(sid)
	require(ok, "片段ID不是十六进制编码")
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var stored model.DramaSegment
		must(db.WithContext(waitCtx).First(&stored, id).Error)
		if stored.ChosenTaskID != nil && stored.KeyframeAssetID != nil {
			return
		}
		select {
		case <-waitCtx.Done():
			panic("任务状态出口未持久化自动采用或首帧")
		case <-ticker.C:
		}
	}
}
