package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
)

// taskView 把任务行转换成对外视图；损坏的输出按空数组降级，避免查询接口返回不可序列化的数据。
func taskView(t *model.GenerationTask) *model.GenerationTaskView {
	outputs := []model.TaskOutput{}
	if len(t.OutputJSON) > 0 {
		var decoded []model.TaskOutput
		if err := json.Unmarshal(t.OutputJSON, &decoded); err != nil {
			logger.Warn("解析任务 output_json 失败", logger.TaskID(t.ID), zap.Error(err))
		} else if decoded != nil {
			outputs = decoded
		}
	}
	return &model.GenerationTaskView{
		ID:              t.ID,
		TaskRef:         taskRefOf(t),
		CanvasProjectID: canvasRef(t.CanvasProjectID),
		NodeID:          t.NodeID,
		Kind:            t.Kind,
		ModelID:         t.ModelKey,
		Status:          t.Status,
		Progress:        t.Progress,
		Outputs:         outputs,
		ErrorCode:       t.ErrorCode,
		ErrorMessage:    t.ErrorMessage,
		Credits:         t.Credits,
		ChargedCredits:  t.ChargedCredits,
		Version:         t.Version,
		DeadlineAt:      t.DeadlineAt,
		CreatedAt:       t.CreatedAt,
		SubmittedAt:     t.SubmittedAt,
		FinishedAt:      t.FinishedAt,
	}
}

// taskRefOf 取任务编号：新任务库里有，加这一列之前创建的旧任务没有，按 id 现算（结果相同）。
func taskRefOf(t *model.GenerationTask) string {
	if t.TaskRef != "" {
		return t.TaskRef
	}
	return idcodec.Encode(t.ID)
}

// providerKeyOf 取快照里的渠道 key，写入任务表用于查询与日志关联。
func providerKeyOf(snap *provider.Snapshot) string {
	return snap.Channel.Key
}

// taskDeadline 取模型配置的任务超时时长，未配置时使用默认值。
func taskDeadline(snap *provider.Snapshot) time.Duration {
	if d := snap.Model.Deadline.D(); d > 0 {
		return d
	}
	return defaultTaskDeadline
}

// parseTaskIDs 解析逗号分隔的任务 ID，去重后返回。
func parseTaskIDs(raw string) ([]uint64, error) {
	parts := strings.Split(raw, ",")
	if len(parts) > maxReconcileIDs {
		return nil, fmt.Errorf("ids 最多 %d 个", maxReconcileIDs)
	}
	seen := make(map[uint64]struct{}, len(parts))
	ids := make([]uint64, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseUint(strings.TrimSpace(part), 10, 64)
		if err != nil || id == 0 {
			return nil, errors.New("ids 格式错误，应为逗号分隔的任务 id")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

// toUint64 把输入中的素材 ID 转成 uint64。
func toUint64(value any) (uint64, bool) {
	switch number := value.(type) {
	case uint64:
		return number, number > 0
	case uint:
		return uint64(number), number > 0
	case int:
		return uint64(number), number > 0
	case int64:
		return uint64(number), number > 0
	case float64:
		return uint64(number), number >= 1 && number == float64(uint64(number))
	case json.Number:
		id, err := strconv.ParseUint(number.String(), 10, 64)
		return id, err == nil && id > 0
	case string:
		id, err := strconv.ParseUint(strings.TrimSpace(number), 10, 64)
		return id, err == nil && id > 0
	default:
		return 0, false
	}
}

// canvasRef 把任务行里的画布主键转成对外的画布 ID；没有所属画布时为空。
func canvasRef(id *uint64) *idcodec.ID {
	if id == nil {
		return nil
	}
	ref := idcodec.ID(*id)
	return &ref
}
