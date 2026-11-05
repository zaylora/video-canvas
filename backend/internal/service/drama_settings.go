package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// DramaSettingsRepo 提供预检、原文和个人提示词查询；消费草稿通过项目事务执行。
type DramaSettingsRepo interface {
	// CleanupDrafts 物理清理过期原文。
	CleanupDrafts(context.Context) error
	// RecentDramaDurations 返回模型最近五十次同步骤成功耗时（秒）。
	RecentDramaDurations(context.Context, uint64, string) ([]int, error)
	// CreateDraft 登记并限制每人最多三份有效草稿。
	CreateDraft(context.Context, *model.DramaSourceDraft) error
	// Draft 读取本人有效草稿。
	Draft(context.Context, uint64, string, bool) (*model.DramaSourceDraft, error)
	// Units 返回本人项目原文范围。
	Units(context.Context, uint64, uint64, int, int) ([]model.DramaUnit, error)
	// Prompts 按用户及环节返回个人提示词。
	Prompts(context.Context, uint64, string) ([]model.DramaPrompt, error)
	// Prompt 读取本人提示词。
	Prompt(context.Context, uint64, uint64) (*model.DramaPrompt, error)
	// SavePrompt 保存本人正文，更新使用乐观锁。
	SavePrompt(context.Context, *model.DramaPrompt) error
	// DeletePrompt 删除本人正文，不改变快照。
	DeletePrompt(context.Context, uint64, uint64) error
}

// DramaSkills 提供管理员启用并标记环节的写作方法，不执行工具或脚本。
type DramaSkills interface {
	// DramaSkills 返回适用该环节的启用技能。
	DramaSkills(context.Context, string) ([]model.DramaSkillView, error)
	// DramaSkill 返回适用该环节的技能正文快照。
	DramaSkill(context.Context, string, string) (*model.DramaPromptSetting, error)
}

// DramaPromptPages 固定各环节允许替换的页面。
func DramaPromptPages(step string) []string {
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

// ListPrompts 只查询本人提示词，支持省略环节返回全部。
func (s *DramaService) ListPrompts(ctx context.Context, userID uint64, step string) ([]model.DramaPromptView, error) {
	// 1. 环节白名单避免无效筛选被静默接受。
	if step != "" && DramaPromptPages(step) == nil {
		return nil, errcode.ErrInvalidParams
	}
	rows, err := s.settings.Prompts(ctx, userID, step)
	if err != nil {
		return nil, err
	}
	out := make([]model.DramaPromptView, 0, len(rows))
	for _, row := range rows {
		out = append(out, promptView(row))
	}
	return out, nil
}

// SavePrompt 校验页面与长度后保存独立正文，更新必须提供 revision。
func (s *DramaService) SavePrompt(ctx context.Context, userID, id uint64, req *model.SaveDramaPromptReq) (*model.DramaPromptView, error) {
	// 1. 正文只允许该环节页面的子集；缺页在建剧快照时补内置。
	if err := validateDramaPrompt(req); err != nil {
		return nil, err
	}
	row := model.DramaPrompt{ID: id, UserID: userID, Step: req.Step, Name: strings.TrimSpace(req.Name), Revision: 1}
	if id != 0 {
		old, err := s.settings.Prompt(ctx, userID, id)
		if err != nil {
			return nil, promptError(err)
		}
		if req.Revision < 1 {
			return nil, errcode.ErrInvalidParams.WithMsg("更新提示词必须提供 revision")
		}
		row.Revision = req.Revision
		row.CreatedAt = old.CreatedAt
	}
	row.Bodies, _ = json.Marshal(req.Bodies)
	// 2. 唯一约束负责并发重名，CAS 负责并发修改。
	if err := s.settings.SavePrompt(ctx, &row); err != nil {
		return nil, promptError(err)
	}
	v := promptView(row)
	return &v, nil
}

// DeletePrompt 仅删除当前用户正文，已保存和入队的文本快照不变。
func (s *DramaService) DeletePrompt(ctx context.Context, userID, id uint64) error {
	// 1. 仓储带 user_id，避免暴露其他用户是否拥有此 ID。
	return promptError(s.settings.DeletePrompt(ctx, userID, id))
}

// validateDramaPrompt 校验非空页面、名称和每份正文两万字上限。
func validateDramaPrompt(req *model.SaveDramaPromptReq) error {
	pages := DramaPromptPages(req.Step)
	if pages == nil {
		return errcode.ErrInvalidParams
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(req.Name)); n == 0 || n > 40 || len(req.Bodies) == 0 {
		return errcode.ErrDramaPromptInvalid
	}
	for key, body := range req.Bodies {
		allowed := false
		for _, page := range pages {
			if key == page {
				allowed = true
			}
		}
		if !allowed || strings.TrimSpace(body) == "" || utf8.RuneCountInString(body) > 20000 {
			return errcode.ErrDramaPromptInvalid
		}
	}
	return nil
}

// promptView 解码正文为公开结构，返回独立 map。
func promptView(row model.DramaPrompt) model.DramaPromptView {
	v := model.DramaPromptView{ID: idcodec.ID(row.ID), Step: row.Step, Name: row.Name, Revision: row.Revision, UpdatedAt: row.UpdatedAt, Bodies: map[string]string{}}
	_ = json.Unmarshal(row.Bodies, &v.Bodies)
	return v
}

// promptError 将用户正文仓储哨兵转成契约错误。
func promptError(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return errcode.ErrDramaPromptNotFound
	case errors.Is(err, repository.ErrDuplicate):
		return errcode.ErrDramaPromptInvalid
	case errors.Is(err, repository.ErrStateConflict):
		return errcode.ErrCanvasConflict
	}
	return err
}

// snapshotPrompts 在事务外解析四环节来源并复制正文，避免入队后读到来源的新版本。
func (s *DramaService) snapshotPrompts(ctx context.Context, userID uint64, kind string, choices map[string]model.DramaPromptChoice) (map[string]model.DramaPromptSetting, error) {
	for key := range choices {
		if DramaPromptPages(key) == nil {
			return nil, errcode.ErrInvalidParams
		}
	}
	out := map[string]model.DramaPromptSetting{}
	for _, step := range []string{"adapt", "assets", "asset_prompts", "video"} {
		choice := choices[step]
		if kind == "script" && step == "adapt" {
			choice = model.DramaPromptChoice{Source: "builtin"}
		}
		setting, err := s.snapshotPrompt(ctx, userID, step, choice)
		if err != nil {
			return nil, err
		}
		out[step] = *setting
	}
	return out, nil
}

// snapshotPrompt 对照归属、技能启停和环节生成不可变正文。
func (s *DramaService) snapshotPrompt(ctx context.Context, userID uint64, step string, choice model.DramaPromptChoice) (*model.DramaPromptSetting, error) {
	setting := &model.DramaPromptSetting{Source: choice.Source, ID: choice.ID, Skill: choice.Skill, Label: "内置", Bodies: map[string]string{}}
	for _, page := range DramaPromptPages(step) {
		setting.Bodies[page] = drama.PromptBody(page)
	}
	switch choice.Source {
	case "", "builtin":
		setting.Source = "builtin"
		setting.ID = 0
		setting.Skill = ""
	case "mine":
		if s.settings == nil {
			return nil, errcode.ErrDramaPromptNotFound
		}
		row, err := s.settings.Prompt(ctx, userID, uint64(choice.ID))
		if err != nil {
			return nil, promptError(err)
		}
		if row.Step != step {
			return nil, errcode.ErrDramaPromptNotFound
		}
		for page, body := range promptView(*row).Bodies {
			setting.Bodies[page] = body
		}
		setting.Label = "我的 · " + row.Name
	case "skill":
		if s.skills == nil {
			return nil, errcode.ErrDramaSkillUnavailable
		}
		return s.skills.DramaSkill(ctx, choice.Skill, step)
	default:
		return nil, errcode.ErrInvalidParams
	}
	return setting, nil
}

// dramaStepGroup 将任务步骤映射到用户可配置的四环节。
func dramaStepGroup(step string) string {
	switch step {
	case "plan", "scripts":
		return "adapt"
	case "breakdown":
		return "assets"
	case "asset_prompts":
		return "asset_prompts"
	case "storyboard":
		return "video"
	}
	return ""
}

// promptSettings 解码项目快照；旧项目空配置自然使用内置。
func promptSettings(p *model.DramaProject) map[string]model.DramaPromptSetting {
	out := map[string]model.DramaPromptSetting{}
	_ = json.Unmarshal(p.PromptSettings, &out)
	return out
}

// promptViews 隐去正文并检查来源是否消失，不影响已保存快照的可用性。
func (s *DramaService) promptViews(ctx context.Context, p *model.DramaProject) (map[string]model.DramaPromptSetting, error) {
	out := promptSettings(p)
	for _, step := range []string{"adapt", "assets", "asset_prompts", "video"} {
		setting, ok := out[step]
		if !ok {
			setting = model.DramaPromptSetting{Source: "builtin", Label: "内置"}
		}
		setting.Bodies = nil
		missing, err := s.promptSourceMissing(ctx, p.UserID, step, setting)
		if err != nil {
			return nil, err
		}
		setting.Missing = missing
		out[step] = setting
	}
	return out, nil
}

// promptSourceMissing 只查来源元信息，不重新读取正文或技能资源包。
func (s *DramaService) promptSourceMissing(ctx context.Context, userID uint64, step string, setting model.DramaPromptSetting) (bool, error) {
	switch setting.Source {
	case "mine":
		if s.settings == nil {
			return true, nil
		}
		_, err := s.settings.Prompt(ctx, userID, uint64(setting.ID))
		if errors.Is(err, repository.ErrNotFound) {
			return true, nil
		}
		return false, err
	case "skill":
		if s.skills == nil {
			return true, nil
		}
		items, err := s.skills.DramaSkills(ctx, step)
		if err != nil {
			return false, err
		}
		for _, item := range items {
			if item.Name == setting.Skill {
				return false, nil
			}
		}
		return true, nil
	}
	return false, nil
}
