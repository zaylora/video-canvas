package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// 拉取与导入的限制。
const maxImportModels = 500 // 单次导入的模型数上限；一般用不到，超出时提示分两次导入

// SetSecret 设置渠道 API Key（存 ai_secrets，名字固定为 channel:<key>），只写。审计只记“谁在什么时候改了哪个渠道的 Key”，不含值。
func (s *AIChannelService) SetSecret(ctx context.Context, actorID uint64, key, value string) error {
	// 1. 渠道必须存在：否则会往 ai_secrets 里写出没有主人的凭证
	if _, err := s.load(ctx, key); err != nil {
		return err
	}
	// 2. 加密入库（空值、超长、没有主密钥等错误由凭证服务给出）
	if err := s.secrets.SetSecret(ctx, model.ChannelSecretName(key), value, actorID); err != nil {
		return err
	}
	// 3. 审计：detail 故意为空
	aiAudit(ctx, s.audit, actorID, model.AuditChannelSecret, model.AuditTargetChannel, key, nil)
	return nil
}

// SetSecretKey 设置或清除渠道 Secret Key（可选的第二凭证，供 AK/SK 签名类协议用；存 ai_secrets，名字 channel:<key>:secret），只写。
// value 为空串表示清除。审计不含值。
func (s *AIChannelService) SetSecretKey(ctx context.Context, actorID uint64, key, value string) error {
	// 1. 渠道必须存在
	if _, err := s.load(ctx, key); err != nil {
		return err
	}
	// 2. 空串清除，否则加密入库
	name := model.ChannelSecretKeyName(key)
	if strings.TrimSpace(value) == "" {
		if err := s.secrets.DeleteSecret(ctx, name); err != nil {
			return err
		}
	} else if err := s.secrets.SetSecret(ctx, name, value, actorID); err != nil {
		return err
	}
	// 3. 审计：detail 故意为空；是设置还是清除，从后续的 secret_key_set 就能看出
	aiAudit(ctx, s.audit, actorID, model.AuditChannelSecKey, model.AuditTargetChannel, key, nil)
	return nil
}

// SetOrder 调整渠道顺序：keys 是全部渠道的完整新顺序，服务端按 10、20、30… 重写 sort。
// keys 不是现有渠道的全集（有不存在的、重复的、漏掉的）返回 ErrInvalidParams，不改任何数据。
func (s *AIChannelService) SetOrder(ctx context.Context, actorID uint64, keys []string) error {
	// 1. 写库：仓储在事务里校验全集
	err := s.repo.SetChannelOrder(ctx, keys)
	if errors.Is(err, repository.ErrNotFound) || errors.Is(err, repository.ErrOrderMismatch) {
		return errcode.ErrInvalidParams.WithMsg("keys 必须恰好是现有全部渠道的 key（不能重复、不能遗漏）")
	}
	if err != nil {
		return err
	}
	// 2. 审计与热生效：渠道顺序决定用户看到的模型顺序
	aiAudit(ctx, s.audit, actorID, model.AuditChannelSort, model.AuditTargetChannel, "", map[string]any{"keys": keys})
	s.notifyChanged(ctx, "channel sort")
	return nil
}

// PulledModel 是从上游拉到的一个模型；Exists 表示这个渠道下已经有同名上游模型（导入时不会重复创建）。
type PulledModel struct {
	ID     string `json:"id"`
	Exists bool   `json:"exists"`
}

// PullResult 是拉取上游模型的结果，不落库。
type PullResult struct {
	Models []PulledModel `json:"models"`
	Total  int           `json:"total"`
}

// PullModels 向上游拉取模型清单：宿主内置的通用请求（GET {base_url}/v1/models，渠道 API Key 作 Bearer），不经过协议插件。
// 只读：不落库，也不改渠道。渠道不存在返回 ErrChannelNotFound；API Key 没设置返回 ErrChannelSecretUnset；
// 上游不通、401/403、返回格式不符、超时、过大都返回 ErrChannelPullFailed（原因写进 msg，已脱敏）。
func (s *AIChannelService) PullModels(ctx context.Context, key string) (*PullResult, error) {
	// 1. 渠道与 Key：Key 没设置时直接给出明确的下一步，而不是发一个必然 401 的请求
	ch, err := s.load(ctx, key)
	if err != nil {
		return nil, err
	}
	if s.lister == nil {
		return nil, errcode.ErrInternal.WithMsg("拉取模型功能未启用")
	}
	set, err := s.secrets.SecretIsSet(ctx, model.ChannelSecretName(key))
	if err != nil {
		return nil, err
	}
	if !set {
		return nil, errcode.ErrChannelSecretUnset.WithMsg(fmt.Sprintf("渠道 %q 的 API Key 尚未设置，请先设置 Key", key))
	}
	apiKey, err := s.secrets.Get(ctx, model.ChannelSecretName(key))
	if err != nil {
		return nil, err
	}
	// 2. 请求上游；失败翻译成业务错误，消息里不含 Key 明文（宿主已脱敏，这里再兜一次）
	rt, err := provider.AgentRuntimeOf(ch) // 只要渠道部分：地址、请求头、限流
	if err != nil {
		return nil, errcode.ErrChannelInvalid.WithMsg("渠道配置无法解析：" + err.Error())
	}
	ids, err := s.lister.ListUpstreamModels(ctx, rt.Channel, apiKey)
	var pe *provider.PullError
	if errors.As(err, &pe) {
		return nil, errcode.ErrChannelPullFailed.WithMsg(aiRedactString(pe.Message, []string{apiKey}))
	}
	if err != nil {
		return nil, errcode.ErrChannelPullFailed.WithMsg(aiRedactString(err.Error(), []string{apiKey}))
	}
	// 3. 标记已存在的模型（同渠道、同上游模型名）
	have, err := s.models.ChannelUpstreamModels(ctx, key)
	if err != nil {
		return nil, err
	}
	out := make([]PulledModel, 0, len(ids))
	for _, id := range ids {
		out = append(out, PulledModel{ID: id, Exists: have[id]})
	}
	return &PullResult{Models: out, Total: len(out)}, nil
}

// ImportModels 把所选的上游模型导入成“待配置”模型：未启用、没有选协议，能力和协议由运营之后在模型里选。
// 渠道下已有的同名上游模型跳过（不重复导入）。ids 为空或超过上限返回 ErrInvalidParams；渠道不存在返回 ErrChannelNotFound。
func (s *AIChannelService) ImportModels(ctx context.Context, actorID uint64, key string, ids []string) (*ImportModelsResult, error) {
	// 1. 参数：去空白与重复，数量限制
	clean := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			clean = append(clean, id)
		}
	}
	if len(clean) == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("请选择要导入的模型")
	}
	if len(clean) > maxImportModels {
		return nil, errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("单次最多导入 %d 个模型，请分批导入", maxImportModels))
	}
	// 2. 渠道必须存在
	if _, err := s.load(ctx, key); err != nil {
		return nil, err
	}
	// 3. 创建模型
	res, err := s.models.ImportChannelModels(ctx, actorID, key, clean)
	if err != nil {
		return nil, err
	}
	// 4. 审计：记新建的模型 key
	created := make([]string, 0, len(res.Created))
	for _, c := range res.Created {
		created = append(created, c.Key)
	}
	aiAudit(ctx, s.audit, actorID, model.AuditModelImport, model.AuditTargetChannel, key, map[string]any{"created": created, "skipped": len(res.Skipped)})
	return res, nil
}
