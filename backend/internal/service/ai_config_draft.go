package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// aiIssuesShown 是把校验问题拼进错误文案时最多列出的条数，超出的只报总数，避免错误文案过长。
const aiIssuesShown = 10

// aiIssuePathChannel 是跨对象检查（渠道 / 插件）问题的 JSON 路径：首期只有一个渠道。
const aiIssuePathChannel = "channels[0].channel"

// SaveDraft 保存模型配置草稿并返回校验发现的问题。in.Create=true 是新建（key 取自正文，已存在则报错），
// false 是更新（key 来自路径，必须已存在，且与正文里的 key 一致）。
// 有校验问题也照常保存（运营需要保存半成品），只是发布时会被拦下。
func (s *AIConfigService) SaveDraft(ctx context.Context, in ModelDraftInput) (*SaveDraftResult, error) {
	// 1. 解析正文里的 key / kind：这些字段要同步到指针行，所以正文必须至少是个带 key 的 JSON 对象，
	//    否则无法确定这份草稿属于谁，无法保存
	meta, err := aiParseMeta(in.Body)
	if err != nil {
		return nil, err
	}
	key := in.Key
	if in.Create {
		key = meta.Key
	} else if meta.Key != key {
		return nil, errcode.ErrInvalidParams.WithMsg("正文里的 key 与路径不一致")
	}
	if err := aiCheckMeta(meta); err != nil {
		return nil, err
	}

	// 2. 新建要求 key 未被占用，更新要求已存在（避免 PUT 拼错 key 悄悄新建一个配置）
	exists, err := s.pointerExists(ctx, key)
	if err != nil {
		return nil, err
	}
	if in.Create && exists {
		return nil, errcode.ErrConfigInvalid.WithMsg(fmt.Sprintf("模型 %q 已存在，请使用 PUT 更新草稿", key))
	}
	if !in.Create && !exists {
		return nil, errcode.ErrConfigNotFound
	}

	// 3. 校验正文（不阻塞保存）：正文本身的问题 + 渠道 / 插件的跨对象问题
	issues, err := s.collectIssues(ctx, key, in.Body)
	if err != nil {
		return nil, err
	}

	// 4. 写入草稿：repo 事务里同步指针行、归档旧草稿、递增版本号。
	//    enabled / sort 只在模型首次创建时取正文里的值，之后由上下架接口独占，避免改 JSON 时悄悄改变上架状态
	rev, err := s.repo.SaveDraft(ctx, repository.SaveDraftInput{
		Pointer:        aiPointer(meta),
		Body:           in.Body,
		CreatedBy:      in.AdminID,
		Note:           in.Note,
		InitialEnabled: meta.Enabled,
		InitialSort:    meta.Sort,
	})
	if err != nil {
		return nil, err
	}
	logger.Info("保存模型配置草稿", zap.String("key", key), zap.String("channel", meta.channel()),
		zap.Int("revision_no", rev.RevisionNo), zap.Uint64("admin_id", in.AdminID), zap.Int("issues", len(issues)))
	return &SaveDraftResult{Revision: rev, Issues: issues}, nil
}

// Validate 校验模型配置。body 非空时校验传入的正文（编辑器实时校验，不落库）；
// 为空时校验已保存的最新草稿，没有草稿返回 ErrConfigNoDraft。
func (s *AIConfigService) Validate(ctx context.Context, key string, body json.RawMessage) (*ValidateResult, error) {
	// 1. 没有传正文就取最新草稿
	if len(body) == 0 {
		draft, err := s.repo.GetDraft(ctx, model.ConfigTargetModel, key)
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrConfigNoDraft
		}
		if err != nil {
			return nil, err
		}
		body = json.RawMessage(draft.BodyJSON)
	}
	// 2. 收集问题；Issues 保证是非 nil 切片，前端可以直接遍历
	issues, err := s.collectIssues(ctx, key, body)
	if err != nil {
		return nil, err
	}
	return &ValidateResult{Valid: len(issues) == 0, Issues: issues}, nil
}

// JSONSchema 返回配置正文的 JSON Schema，供前端 Monaco 编辑器做补全。target 只接受 model（平台协议已改为插件，没有 provider 的 schema）。
func (s *AIConfigService) JSONSchema(target string) (json.RawMessage, error) {
	// 1. 校验目标类型
	if target != model.ConfigTargetModel {
		return nil, errcode.ErrInvalidParams.WithMsg("target 只能是 model")
	}
	// 2. modelcfg 每次返回缓存的副本，调用方可以随意修改
	return json.RawMessage(modelcfg.JSONSchema()), nil
}

// collectIssues 校验一份模型正文并返回问题列表（永不为 nil）：正文里的 key 必须与目标 key 一致；
// 正文本身通过后再做渠道 / 插件的跨对象检查（设计 6.6“保存时校验”），跨对象问题同样只记为 Issue、不阻塞保存。
// 只有仓储故障才返回 error。
func (s *AIConfigService) collectIssues(ctx context.Context, key string, body []byte) ([]modelcfg.Issue, error) {
	// 1. 正文本身的校验；没解析出配置就没法做跨对象检查
	_, cfg, issues := aiBodyIssues(key, body)
	if cfg == nil {
		return issues, nil
	}
	// 2. 跨对象检查：渠道存在且启用、插件启用、插件版本支持该 kind。
	//    这些问题的原因来自 errcode 文案，转成 Issue 挂在 channels[0].channel 上
	if _, err := s.resolveChannel(ctx, cfg); err != nil {
		var e *errcode.Error
		if !errors.As(err, &e) {
			return nil, err
		}
		issues = append(issues, modelcfg.Issue{Path: aiIssuePathChannel, Message: e.Msg})
	}
	return issues, nil
}

// aiBodyIssues 校验正文本身（不访问数据库），返回宽松解析的同步字段、解析成功的配置与问题列表（永不为 nil）。
// 正文不是带 key 的对象时 meta 为 nil；正文 key 与目标 key 不一致记一条问题；有任何问题时 cfg 为 nil。
func aiBodyIssues(key string, body []byte) (*aiConfigMeta, *modelcfg.ModelConfig, []modelcfg.Issue) {
	issues := []modelcfg.Issue{}
	meta, err := aiParseMeta(body)
	if err != nil {
		// modelcfg 也会报问题，这里只给一条更直白的提示
		return nil, nil, append(issues, modelcfg.Issue{Message: "配置正文必须是包含 key 的 JSON 对象"})
	}
	if meta.Key != key {
		issues = append(issues, modelcfg.Issue{Path: "key", Message: fmt.Sprintf("key 必须与目标一致（%q）", key)})
	}
	cfg, found := modelcfg.ParseModel(body)
	issues = append(issues, found...)
	if len(issues) > 0 {
		return meta, nil, issues
	}
	return meta, cfg, issues
}

// pointerExists 判断模型指针行是否存在。
func (s *AIConfigService) pointerExists(ctx context.Context, key string) (bool, error) {
	_, err := s.repo.GetModelPointer(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// aiParseMeta 从正文宽松取出同步字段；正文不是 JSON 对象、缺少 key、这几个字段类型不对都返回参数错误。
func aiParseMeta(body []byte) (*aiConfigMeta, error) {
	trimmed := strings.TrimSpace(string(body))
	if !strings.HasPrefix(trimmed, "{") {
		return nil, errcode.ErrInvalidParams.WithMsg("配置正文必须是 JSON 对象")
	}
	var m aiConfigMeta
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, errcode.ErrInvalidParams.WithMsg("配置正文不是合法的 JSON，或 key / kind / enabled / sort / channels 类型错误")
	}
	if strings.TrimSpace(m.Key) == "" {
		return nil, errcode.ErrInvalidParams.WithMsg("配置正文缺少 key")
	}
	return &m, nil
}

// aiCheckMeta 校验要写入指针行的字段：key 字符集与长度、kind 长度（超出列宽写库会报错，提前按参数错误返回）。
func aiCheckMeta(m *aiConfigMeta) error {
	if len(m.Key) > aiModelKeyMaxLen || !aiConfigKeyRe.MatchString(m.Key) {
		return errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("key 只能包含字母、数字、下划线、点和短横线，且以字母或数字开头，长度不超过 %d", aiModelKeyMaxLen))
	}
	if len(m.Kind) > aiModelKindMaxLen {
		return errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("kind 不能超过 %d 个字符", aiModelKindMaxLen))
	}
	return nil
}

// aiPointer 由正文里的字段生成要同步到指针行的冗余字段。
func aiPointer(m *aiConfigMeta) repository.ConfigPointer {
	return repository.ConfigPointer{Target: model.ConfigTargetModel, Key: m.Key, Kind: m.Kind}
}

// aiFormatIssues 把问题列表拼成一句话，最多列 aiIssuesShown 条。
func aiFormatIssues(issues []modelcfg.Issue) string {
	parts := make([]string, 0, aiIssuesShown+1)
	for i, is := range issues {
		if i >= aiIssuesShown {
			parts = append(parts, fmt.Sprintf("……共 %d 条", len(issues)))
			break
		}
		if is.Path != "" {
			parts = append(parts, is.Path+"："+is.Message)
		} else {
			parts = append(parts, is.Message)
		}
	}
	return strings.Join(parts, "；")
}

// aiNotFound 把仓储的“不存在”翻译成 ErrConfigNotFound（404），其他错误原样透传。
func aiNotFound(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrConfigNotFound
	}
	return err
}
