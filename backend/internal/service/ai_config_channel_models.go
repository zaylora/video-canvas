package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// 渠道下的模型管理：拉取时标记已存在的模型、导入“待配置”模型、渠道内排序。
// 渠道归属不在指针表里，而是在配置正文的 channels[0]，所以这里统一从正文宽松取出（半成品也能取到）。

// ImportedModel 是导入时新建的一个模型。
type ImportedModel struct {
	Key           string `json:"key"`
	UpstreamModel string `json:"upstream_model"`
}

// ImportModelsResult 是导入结果：新建的模型，和因“渠道下已有同名上游模型”而跳过的上游模型名。
type ImportModelsResult struct {
	Created []ImportedModel `json:"created"`
	Skipped []string        `json:"skipped"`
}

// aiChannelModelRef 是模型正文里宽松取出的渠道归属与上游模型名。
type aiChannelModelRef struct {
	Channels []struct {
		Channel       string `json:"channel"`
		UpstreamModel string `json:"upstream_model"`
	} `json:"channels"`
}

// channelModels 列出渠道下的模型：返回 key → 上游模型名（正文里没写为空串），以及渠道内当前最大的排序值。
// 正文损坏的模型取不到渠道，不算在任何渠道下。
func (s *AIConfigService) channelModels(ctx context.Context, channelKey string) (map[string]string, int, error) {
	cfgs, err := s.repo.ListModelConfigs(ctx)
	if err != nil {
		return nil, 0, err
	}
	ptrs, err := s.repo.ListModelPointers(ctx)
	if err != nil {
		return nil, 0, err
	}
	sorts := make(map[string]int, len(ptrs))
	for _, p := range ptrs {
		sorts[p.Key] = p.Sort
	}
	out := map[string]string{}
	maxSort := 0
	for _, c := range cfgs {
		var ref aiChannelModelRef
		if json.Unmarshal(c.BodyJSON, &ref) != nil || len(ref.Channels) == 0 || ref.Channels[0].Channel != channelKey {
			continue
		}
		out[c.TargetKey] = ref.Channels[0].UpstreamModel
		maxSort = max(maxSort, sorts[c.TargetKey])
	}
	return out, maxSort, nil
}

// ChannelUpstreamModels 实现 AIChannelModels：返回渠道下已有模型的上游模型名集合。
func (s *AIConfigService) ChannelUpstreamModels(ctx context.Context, channelKey string) (map[string]bool, error) {
	models, _, err := s.channelModels(ctx, channelKey)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(models))
	for _, up := range models {
		if up != "" {
			out[up] = true
		}
	}
	return out, nil
}

// aiImportKeyBadChars 是模型 key 里不允许的字符（key 的字符集见 aiConfigKeyRe）。
var aiImportKeyBadChars = regexp.MustCompile(`[^A-Za-z0-9_.\-]+`)

// aiImportKeyMaxBase 是导入时 key 主体的最大长度：留出余量给冲突时加的渠道前缀与序号，整体仍不超过 aiModelKeyMaxLen。
const aiImportKeyMaxBase = 80

// importModelKey 由上游模型 id 生成模型 key：不合法字符换成 -，以字母或数字开头，过长截断；
// 与已有的 key 冲突时先加渠道前缀（<渠道>-<id>），仍冲突再追加 -2、-3…。taken 是已占用的 key，调用方负责登记返回值。
func importModelKey(id, channelKey string, taken map[string]bool) string {
	base := strings.Trim(aiImportKeyBadChars.ReplaceAllString(id, "-"), "-._")
	if base == "" {
		base = "model"
	}
	if len(base) > aiImportKeyMaxBase {
		base = base[:aiImportKeyMaxBase]
	}
	if !taken[base] {
		return base
	}
	base = channelKey + "-" + base
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		if k := fmt.Sprintf("%s-%d", base, n); !taken[k] {
			return k
		}
	}
}

// ImportChannelModels 实现 AIChannelModels：为渠道创建一批“待配置”模型——未启用、kind 为空、没有选协议，
// 展示名取上游模型 id，排在渠道内现有模型之后。渠道下已有同名上游模型的跳过。
// 每个模型直接写库（半成品本来就允许保存），最后只刷新一次 Registry；中途写库失败时已创建的保留，错误原样返回。
func (s *AIConfigService) ImportChannelModels(ctx context.Context, actorID uint64, channelKey string, ids []string) (*ImportModelsResult, error) {
	// 1. 渠道下已有的上游模型与渠道内最大排序；全部模型的 key（key 全局唯一）
	have, maxSort, err := s.channelModels(ctx, channelKey)
	if err != nil {
		return nil, err
	}
	haveUp := make(map[string]bool, len(have))
	for _, up := range have {
		haveUp[up] = true
	}
	ptrs, err := s.repo.ListModelPointers(ctx)
	if err != nil {
		return nil, err
	}
	taken := make(map[string]bool, len(ptrs))
	for _, p := range ptrs {
		taken[p.Key] = true
	}
	// 2. 逐个创建
	res := &ImportModelsResult{Created: []ImportedModel{}, Skipped: []string{}}
	for _, id := range ids {
		if haveUp[id] {
			res.Skipped = append(res.Skipped, id)
			continue
		}
		key := importModelKey(id, channelKey, taken)
		maxSort += 10
		body, err := json.Marshal(map[string]any{
			"key": key, "kind": "", "label": id, "enabled": false, "sort": maxSort,
			"channels": []map[string]any{{"channel": channelKey, "upstream_model": id}},
		})
		if err != nil {
			return nil, fmt.Errorf("编码导入的模型失败：%w", err)
		}
		meta, err := aiParseMeta(body)
		if err != nil {
			return nil, err
		}
		if _, err := s.repo.SaveModel(ctx, repository.SaveModelInput{
			Pointer: aiPointer(meta), Body: body, CreatedBy: actorID, Note: "从渠道拉取", InitialEnabled: false, InitialSort: maxSort,
		}); err != nil {
			return nil, err
		}
		taken[key] = true
		haveUp[id] = true
		res.Created = append(res.Created, ImportedModel{Key: key, UpstreamModel: id})
	}
	// 3. 热生效：待配置的模型不影响用户，但渠道页的模型数等要对齐
	if len(res.Created) > 0 {
		s.notifyChanged(ctx, "import models "+channelKey)
	}
	return res, nil
}

// SetChannelModelOrder 调整渠道内模型的顺序：keys 是该渠道下全部模型的完整新顺序，按 10、20、30… 重写这些模型的 sort，
// 不影响其他渠道。keys 不是该渠道现有模型的全集（有别的渠道的、不存在的、重复的、漏掉的）返回 ErrInvalidParams，不改任何数据。
func (s *AIConfigService) SetChannelModelOrder(ctx context.Context, channelKey string, keys []string) error {
	// 1. 校验全集
	have, _, err := s.channelModels(ctx, channelKey)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		if _, ok := have[k]; !ok || seen[k] {
			return errcode.ErrInvalidParams.WithMsg("keys 必须恰好是该渠道下全部模型的 key（不能重复、不能遗漏）")
		}
		seen[k] = true
	}
	if len(seen) != len(have) {
		return errcode.ErrInvalidParams.WithMsg("keys 必须恰好是该渠道下全部模型的 key（不能重复、不能遗漏）")
	}
	// 2. 逐个写 sort（指针行的单列更新，彼此独立）
	for i, k := range keys {
		if err := s.repo.SetModelSort(ctx, k, (i+1)*10); err != nil {
			return aiNotFound(err)
		}
	}
	// 3. 热生效：渠道内顺序决定用户看到的模型顺序
	s.notifyChanged(ctx, "model order "+channelKey)
	return nil
}

// DeleteSecret 删除凭证（不存在也算成功），并清掉本实例的明文缓存。渠道的 Secret Key 被清除时用。
func (s *AIConfigService) DeleteSecret(ctx context.Context, name string) error {
	if err := s.repo.DeleteSecret(ctx, name); err != nil {
		return err
	}
	s.ForgetSecret(name)
	return nil
}
