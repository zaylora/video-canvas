package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// dramaPages 只开放契约允许的四个环节。
func dramaPages(step string) []string {
	switch step {
	case "adapt":
		return []string{"plan", "scripts"}
	case "assets":
		return []string{"breakdown"}
	case "asset_prompts":
		return []string{"asset_prompts"}
	case "video":
		return []string{"storyboard"}
	}
	return nil
}

// SetDramaSteps 设置导入技能适用的短剧环节，不产生新版本，写操作审计。
func (s *SkillService) SetDramaSteps(ctx context.Context, actorID uint64, name string, steps []string) (*SkillItem, error) {
	// 1. 验证白名单和重复，空列表表示撤销全部短剧适用范围。
	seen := map[string]bool{}
	for _, step := range steps {
		if dramaPages(step) == nil || seen[step] {
			return nil, errcode.ErrInvalidParams
		}
		seen[step] = true
	}
	sk, err := s.writable(ctx, name)
	if err != nil {
		return nil, err
	}
	if steps == nil {
		steps = []string{}
	}
	// 2. 适用环节与启停同级，使用独立字段更新，不影响不可变版本。
	if err := s.repo.SetDramaSteps(ctx, sk.ID, steps); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrSkillNotFound
		}
		return nil, err
	}
	sk.DramaSteps = append([]string{}, steps...)
	s.auditSkill(ctx, actorID, "agent_skill.drama_steps", sk.ID, map[string]any{"name": name, "steps": steps})
	return s.itemFor(ctx, sk)
}

// DramaSkills 只列启用、生效版本就绪且管理员标记该环节的导入技能。
func (s *SkillService) DramaSkills(ctx context.Context, step string) ([]model.DramaSkillView, error) {
	// 1. 直接查当前状态，停用立即生效，不借用 Agent 目录缓存。
	if dramaPages(step) == nil {
		return nil, errcode.ErrInvalidParams
	}
	rows, err := s.repo.ListSkills(ctx)
	if err != nil {
		return nil, err
	}
	active, err := s.repo.ListActiveVersions(ctx)
	if err != nil {
		return nil, err
	}
	out := []model.DramaSkillView{}
	for _, sk := range rows {
		v, ok := active[sk.ID]
		if sk.Enabled && sk.ActiveVersion != nil && ok && slices.Contains(sk.DramaSteps, step) {
			out = append(out, model.DramaSkillView{Name: sk.Name, Title: sk.Title, Description: v.Description, HasScripts: v.HasScripts})
		}
	}
	return out, nil
}

// DramaSkill 读取可用技能及其正文引用的文本资源，最多一万六千字，不执行脚本。
func (s *SkillService) DramaSkill(ctx context.Context, name, step string) (*model.DramaPromptSetting, error) {
	// 1. 先检查管理员标记和启停；来源不符统一返回 67018。
	sk, err := s.repo.GetSkill(ctx, name)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrDramaSkillUnavailable
	}
	if err != nil {
		return nil, err
	}
	if !sk.Enabled || sk.ActiveVersion == nil || !slices.Contains(sk.DramaSteps, step) {
		return nil, errcode.ErrDramaSkillUnavailable
	}
	v, err := s.repo.GetVersion(ctx, sk.ID, *sk.ActiveVersion)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrDramaSkillUnavailable
	}
	if err != nil {
		return nil, err
	}
	// 2. 只有正文实际引用的文本才读取，按出现顺序附加并受同一总预算约束。
	text, err := s.dramaSkillText(ctx, v)
	if err != nil {
		return nil, err
	}
	setting := &model.DramaPromptSetting{Source: "skill", Skill: name, Version: v.Version, Label: "技能 · " + sk.Title, Bodies: map[string]string{}}
	for _, page := range dramaPages(step) {
		setting.Bodies[page] = drama.SkillBlock(sk.Title, text)
	}
	return setting, nil
}

// dramaSkillText 读取引用链，循环引用只附加一次，脚本和二进制永远不读取。
func (s *SkillService) dramaSkillText(ctx context.Context, v *model.AgentSkillVersion) (string, error) {
	text := drama.Clip(v.BodyText, 16000)
	var files []SkillFileView
	if err := json.Unmarshal(v.FilesJSON, &files); err != nil {
		return "", err
	}
	seen := map[string]bool{"SKILL.md": true}
	for utf8.RuneCountInString(text) < 16000 {
		refs := []SkillFileView{}
		for _, f := range files {
			if f.Text && f.Kind != "script" && !strings.HasPrefix(f.Path, "scripts/") && !seen[f.Path] && strings.Contains(text, f.Path) {
				refs = append(refs, f)
			}
		}
		if len(refs) == 0 {
			break
		}
		sort.SliceStable(refs, func(i, j int) bool { return strings.Index(text, refs[i].Path) < strings.Index(text, refs[j].Path) })
		pkg, err := s.packageOf(ctx, v)
		if err != nil {
			return "", err
		}
		for _, f := range refs {
			seen[f.Path] = true
			content, err := readPackageFile(pkg, files, f.Path)
			if err != nil {
				return "", err
			}
			text = drama.Clip(text+"\n\n## "+f.Path+"\n"+content.Text, 16000)
		}
	}
	return text, nil
}
