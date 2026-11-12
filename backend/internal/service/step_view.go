package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/stepkit"
)

// stepPreviewRunes 是没有自定义 Preview 时，试跑预览取输出 JSON 的前多少个字符。
const stepPreviewRunes = 400

// encodeID 把主键编码成对外的十六进制串（同短剧）。
func encodeID(id uint64) string { return idcodec.Encode(id) }

// nonNil 让空切片序列化成 [] 而不是 null。
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// nonNilInts 同 nonNil，用于版本号列表。
func nonNilInts(s []int) []int {
	if s == nil {
		return []int{}
	}
	return s
}

// sortedSkills 按名字排序返回技能，候选下拉顺序稳定。
func sortedSkills(ix *skillIndex) []stepkit.SkillInfo {
	out := make([]stepkit.SkillInfo, 0, len(ix.byName))
	for _, info := range ix.byName {
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// refView 把历史里存的 from / to 转成接口视图；显示名按当前技能库补，技能已删时只剩名字。
func refView(raw datatypes.JSON, ix *skillIndex) model.StepMethodView {
	var ref model.StepMethodRef
	if err := json.Unmarshal(raw, &ref); err != nil || ref.Source != stepkit.SourceSkill {
		return model.StepMethodView{Source: stepkit.SourceBuiltin}
	}
	v := model.StepMethodView{Source: stepkit.SourceSkill, SkillName: ref.SkillName, Version: ref.SkillVersion}
	if info, ok := ix.byName[ref.SkillName]; ok {
		v.SkillTitle = info.Title
	}
	return v
}

// stepCandidate 是解析后的候选方法：内置，或技能 id、名字加钉死的版本。
type stepCandidate struct {
	Source    string
	SkillID   uint64
	SkillName string
	Version   int
}

// ref 返回写进绑定历史的形状。
func (c stepCandidate) ref() model.StepMethodRef {
	return model.StepMethodRef{Source: c.Source, SkillID: c.SkillID, SkillName: c.SkillName, SkillVersion: c.Version}
}

// sameAs 判断候选与试跑记录的候选是不是同一个（环节已由调用方确认相同）：来源、技能和版本都一致。
func (c stepCandidate) sameAs(t *model.StepTrial) bool {
	if c.Source != t.Source {
		return false
	}
	if c.Source != stepkit.SourceSkill {
		return true
	}
	return t.SkillID != nil && *t.SkillID == c.SkillID && t.SkillVersion != nil && *t.SkillVersion == c.Version
}

// resolveCandidate 校验并解析请求里的候选：技能必须存在、已启用，版本（省略取当前生效版本）必须就绪，
// 否则返回 68014。绑定和试跑共用，保证两边对「可用」的判断一致。
func (s *StepAdminService) resolveCandidate(ctx context.Context, req model.StepCandidateReq) (stepCandidate, error) {
	if req.Source != stepkit.SourceSkill {
		return stepCandidate{Source: stepkit.SourceBuiltin}, nil
	}
	name := strings.TrimSpace(req.SkillName)
	info, err := s.skills.FindSkill(ctx, name)
	if errors.Is(err, stepkit.ErrSkillUnavailable) {
		return stepCandidate{}, errcode.ErrStepSkillUnavail
	}
	if err != nil {
		return stepCandidate{}, err
	}
	version := req.Version
	if version == 0 {
		version = info.ActiveVersion
	}
	if !info.Enabled || version == 0 {
		return stepCandidate{}, errcode.ErrStepSkillUnavail
	}
	for _, v := range info.Versions {
		if v == version {
			return stepCandidate{Source: stepkit.SourceSkill, SkillID: info.ID, SkillName: info.Name, Version: version}, nil
		}
	}
	return stepCandidate{}, errcode.ErrStepSkillUnavail
}

// trialView 把试跑行转成接口视图。技能名按 id 在技能库里补。
func (s *StepAdminService) trialView(t *model.StepTrial, ix *skillIndex) model.StepTrialView {
	v := model.StepTrialView{ID: encodeID(t.ID), StepKey: t.StepKey, SampleKey: t.SampleKey, TextModel: t.TextModelID,
		Status: t.Status, Passed: t.Passed, Errors: decodeStrings(t.Errors), Flags: nonNilJSON(t.Flags), Output: t.Output,
		Error: t.Error, DurationMS: t.DurationMS, InputTokens: t.InputTokens, OutputTokens: t.OutputTokens,
		Credits: t.Credits, CreatedAt: t.CreatedAt}
	v.Candidate = model.StepTrialCandidate{Source: t.Source}
	if t.Source == stepkit.SourceSkill && t.SkillID != nil {
		if info, ok := ix.byID[*t.SkillID]; ok {
			v.Candidate.SkillName = info.Name
		}
		if t.SkillVersion != nil {
			v.Candidate.Version = *t.SkillVersion
		}
	}
	if t.CompareOf != nil {
		id := encodeID(*t.CompareOf)
		v.CompareOf = &id
	}
	if st, ok := s.reg.Get(t.StepKey); ok && len(t.Output) > 0 {
		v.Preview = previewOf(st, json.RawMessage(t.Output))
	}
	return v
}

// previewOf 生成人能读的输出摘要：环节自定义的优先，否则取紧凑 JSON 的前若干字符。
func previewOf(st stepkit.Step, out json.RawMessage) string {
	if st.Preview != nil {
		return st.Preview(out)
	}
	r := []rune(string(out))
	if len(r) > stepPreviewRunes {
		return string(r[:stepPreviewRunes]) + "…"
	}
	return string(r)
}

// decodeStrings 解析 JSON 字符串数组，失败或为空返回空切片。
func decodeStrings(raw datatypes.JSON) []string {
	out := []string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out) // 列由本服务写入，格式不对时按空处理，不影响查看
	}
	return out
}

// nonNilJSON 空值返回 []，flags 在接口里始终是数组。
func nonNilJSON(raw datatypes.JSON) datatypes.JSON {
	if len(raw) == 0 {
		return datatypes.JSON("[]")
	}
	return raw
}

// GetTrial 返回试跑结果；不存在返回 10002。
func (s *StepAdminService) GetTrial(ctx context.Context, id string) (*model.StepTrialView, error) {
	raw, ok := idcodec.Decode(id)
	if !ok {
		return nil, errcode.ErrNotFound
	}
	t, err := s.repo.Trial(ctx, raw, false)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	ix, err := s.loadSkills(ctx)
	if err != nil {
		return nil, err
	}
	v := s.trialView(t, ix)
	return &v, nil
}
