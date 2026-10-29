package agent

import (
	"context"
	"errors"
	"strconv"

	"video-canvas/internal/pkg/errcode"
)

// MaxTaskStatusBatch 是一次最多查多少个任务：一批生成最多 8 个节点，留出几批合并唤醒的余量。
const MaxTaskStatusBatch = 50

// 任务不存在或不属于这个用户时的状态：不区分两种情况，不泄露别人任务是否存在。
const taskStatusMissing = "missing"

// PlatformTaskOutput 是任务的一个产物：Agent 据此知道出了什么图、什么视频（再用看图、看视频工具去看内容）。
type PlatformTaskOutput struct {
	MediaType string `json:"media_type"`
	AssetID   string `json:"asset_id,omitempty"` // 十进制字符串；文本产物没有
	Text      string `json:"text,omitempty"`     // 文本产物的正文（截断）
}

// PlatformTaskState 是 Agent 查到的一个任务的最新状态。ID 都用十进制字符串：雪花 ID 超过 2^53，用 JSON 数字会在 JavaScript 里丢精度。
type PlatformTaskState struct {
	TaskID   string               `json:"task_id"`
	NodeID   string               `json:"node_id,omitempty"`
	Kind     string               `json:"kind,omitempty"`
	Status   string               `json:"status"` // 任务状态；任务不存在或不属于这个用户是 missing
	Progress *int                 `json:"progress,omitempty"`
	Error    string               `json:"error,omitempty"` // 失败、超时、取消时给用户看的原因
	Credits  int                  `json:"credits,omitempty"`
	Outputs  []PlatformTaskOutput `json:"outputs,omitempty"`
}

// maxTaskOutputText 是文本产物最多带多少字：Agent 只需要知道大概内容，完整内容它自己去读节点。
const maxTaskOutputText = 500

// PlatformTaskStates 读这个用户的一批生成任务的最新状态，顺序与 ids 一致（Agent 服务靠它补收没送到的通知，
// 以及在任务全部结束后取结果交给模型）。别人的和不存在的任务不报错，标成 missing。
func (b *AgentBridge) PlatformTaskStates(ctx context.Context, userID uint64, ids []uint64) ([]PlatformTaskState, error) {
	if userID == 0 || len(ids) == 0 || len(ids) > MaxTaskStatusBatch {
		return nil, errcode.ErrInvalidParams.WithMsg("需要 user_id，以及 1 到 " + strconv.Itoa(MaxTaskStatusBatch) + " 个任务 id")
	}
	if b.d.Tasks == nil {
		return nil, errcode.ErrInvalidParams.WithMsg("暂时无法查询任务")
	}
	out := make([]PlatformTaskState, len(ids))
	for i, id := range ids {
		out[i].TaskID = strconv.FormatUint(id, 10)
		v, err := b.d.Tasks.Get(ctx, userID, id)
		var ec *errcode.Error
		if errors.As(err, &ec) && ec.HTTPStatus() == 404 {
			out[i].Status = taskStatusMissing
			continue
		}
		if err != nil {
			return nil, err
		}
		s := &out[i]
		s.NodeID, s.Kind, s.Status, s.Progress, s.Error, s.Credits = v.NodeID, v.Kind, v.Status, v.Progress, v.ErrorMessage, v.Credits
		for _, o := range v.Outputs {
			po := PlatformTaskOutput{MediaType: o.MediaType, Text: truncateRunes(o.Text, maxTaskOutputText)}
			if o.AssetID != 0 {
				po.AssetID = strconv.FormatUint(o.AssetID, 10)
			}
			s.Outputs = append(s.Outputs, po)
		}
	}
	return out, nil
}
