package agent

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	"video-canvas/internal/stepkit"
)

// 下面三个方法让 SkillService 实现 stepkit.SkillLibrary：环节协议层经它读取后台导入的技能，
// 绑定环节时只用 SKILL.md 正文和正文引用到的文本文件，脚本永远不执行。

var _ stepkit.SkillLibrary = (*SkillService)(nil)

// LoadMethod 读取技能指定版本的方法文本，正文加引用文本最多 limit 个字符。
// 技能不存在、未启用、版本不存在（或还没就绪）都返回 stepkit.ErrSkillUnavailable，
// 让环节退回内置方法；读库或读存储失败原样返回。
func (s *SkillService) LoadMethod(ctx context.Context, name string, version, limit int) (*stepkit.SkillText, error) {
	// 1. 技能必须存在且启用：停用立即生效，不借用目录缓存。
	sk, err := s.repo.GetSkill(ctx, strings.TrimSpace(name))
	if errors.Is(err, repository.ErrNotFound) {
		return nil, stepkit.ErrSkillUnavailable
	}
	if err != nil {
		return nil, err
	}
	if !sk.Enabled {
		return nil, stepkit.ErrSkillUnavailable
	}
	// 2. 版本固定读绑定记录的那一个，不跟随生效版本；只有 ready 版本可读。
	v, err := s.repo.GetVersion(ctx, sk.ID, version)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, stepkit.ErrSkillUnavailable
	}
	if err != nil {
		return nil, err
	}
	text, err := s.skillText(ctx, v, limit)
	if err != nil {
		return nil, err
	}
	return &stepkit.SkillText{Name: sk.Name, Title: sk.Title, Version: v.Version, Body: text, HasScripts: v.HasScripts}, nil
}

// FindSkill 按名字取技能概览（含全部就绪版本）；技能不存在返回 stepkit.ErrSkillUnavailable。
func (s *SkillService) FindSkill(ctx context.Context, name string) (*stepkit.SkillInfo, error) {
	sk, err := s.repo.GetSkill(ctx, strings.TrimSpace(name))
	if errors.Is(err, repository.ErrNotFound) {
		return nil, stepkit.ErrSkillUnavailable
	}
	if err != nil {
		return nil, err
	}
	vs, err := s.repo.ListVersions(ctx, sk.ID)
	if err != nil {
		return nil, err
	}
	nums := make([]int, 0, len(vs))
	for _, v := range vs {
		nums = append(nums, v.Version)
	}
	info, err := s.skillInfo(ctx, sk, nums)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// ListSkills 列出全部导入技能（含停用的）及其就绪版本，后台候选下拉用。
func (s *SkillService) ListSkills(ctx context.Context) ([]stepkit.SkillInfo, error) {
	rows, err := s.repo.ListSkills(ctx)
	if err != nil {
		return nil, err
	}
	versions, err := s.repo.ListVersionNumbers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]stepkit.SkillInfo, 0, len(rows))
	for i := range rows {
		info, err := s.skillInfo(ctx, &rows[i], versions[rows[i].ID])
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

// skillInfo 组装技能概览：版本号升序；生效版本的正文字数和是否带脚本从该版本读取（没有生效版本时为空值）。
func (s *SkillService) skillInfo(ctx context.Context, sk *model.AgentSkill, nums []int) (stepkit.SkillInfo, error) {
	info := stepkit.SkillInfo{ID: sk.ID, Name: sk.Name, Title: sk.Title, Enabled: sk.Enabled, Versions: append([]int{}, nums...)}
	sort.Ints(info.Versions)
	if sk.ActiveVersion == nil {
		return info, nil
	}
	v, err := s.repo.GetVersion(ctx, sk.ID, *sk.ActiveVersion)
	if errors.Is(err, repository.ErrNotFound) {
		return info, nil // 生效版本还没就绪或已被清理：当作没有生效版本
	}
	if err != nil {
		return info, err
	}
	info.ActiveVersion = v.Version
	info.HasScripts = v.HasScripts
	info.Chars = utf8.RuneCountInString(v.BodyText)
	return info, nil
}
