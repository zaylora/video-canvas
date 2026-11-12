package stepkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrSkillUnavailable 表示绑定的技能不能用了：技能不存在、被停用，或指定版本被删。
// 技能库实现用它区分「技能不可用」和「读库失败」：前者让环节退回内置方法，后者要把错误交给调用方。
var ErrSkillUnavailable = errors.New("技能不可用")

// Binding 是一个环节当前的方法绑定（step_bindings 的一行）。没有记录等于内置。
type Binding struct {
	StepKey      string // 环节 key
	Source       string // builtin / skill
	SkillID      uint64 // source=skill 时的技能 id
	SkillName    string // 技能名；技能已被删除时为空
	SkillVersion int    // 钉死的版本号，技能发新版本后不跟随
	TrialID      uint64 // 生效依据的试跑 id
}

// BindingStore 读取绑定。由 service 层包装 repository 实现。
type BindingStore interface {
	// GetBinding 返回环节的绑定；没有记录时 found=false（等于内置）。
	GetBinding(ctx context.Context, stepKey string) (b Binding, found bool, err error)
}

// SkillText 是技能某个版本里本环节要用的方法文本。
type SkillText struct {
	Name       string // 技能名
	Title      string // 显示名
	Version    int    // 版本号
	Body       string // SKILL.md 正文加上正文引用的文本文件，已按调用方给的上限截断；脚本不读取
	HasScripts bool   // 包里有脚本：脚本不会执行，后台标「仅用正文」
}

// SkillInfo 是后台候选列表用的技能概览。
type SkillInfo struct {
	ID            uint64
	Name          string
	Title         string
	Enabled       bool
	ActiveVersion int   // 生效版本，没有为 0
	Versions      []int // 全部就绪版本，升序
	HasScripts    bool  // 生效版本是否带脚本
	Chars         int   // 生效版本 SKILL.md 正文字数
}

// SkillLibrary 是环节读取导入技能的入口，由 service/agent 的 SkillService 实现。
type SkillLibrary interface {
	// LoadMethod 读取技能指定版本的方法文本，正文最多 limit 个字符。
	// 技能不存在、未启用、版本不存在返回 ErrSkillUnavailable；其他错误是读库或读存储失败。
	LoadMethod(ctx context.Context, name string, version, limit int) (*SkillText, error)
	// FindSkill 按名字取技能概览；不存在返回 ErrSkillUnavailable。
	FindSkill(ctx context.Context, name string) (*SkillInfo, error)
	// ListSkills 列出全部导入技能（含停用的），后台候选下拉用。
	ListSkills(ctx context.Context) ([]SkillInfo, error)
}

// Method 是一次运行实际使用的方法。
type Method struct {
	Source         string // builtin / skill
	SkillName      string
	SkillTitle     string
	Version        int    // 技能版本；内置为 0
	Body           string // 注入提示词的正文（技能已截断到环节上限）
	SHA256         string // Body 的 sha256，用于追查哪个技能出的问题
	HasScripts     bool
	Fallback       bool   // 绑定的技能不可用，已退回内置
	FallbackReason string // 退回原因，Fallback 为真时有值
}

// MethodSnapshot 是写进运行记录的方法快照（marketing_step_runs.method）。
type MethodSnapshot struct {
	Source     string `json:"source"`
	SkillName  string `json:"skill_name,omitempty"`
	SkillTitle string `json:"skill_title,omitempty"`
	Version    int    `json:"version,omitempty"`
	SHA256     string `json:"sha256"`
	Fallback   bool   `json:"fallback,omitempty"`
}

// Snapshot 返回方法快照。
func (m Method) Snapshot() MethodSnapshot {
	return MethodSnapshot{Source: m.Source, SkillName: m.SkillName, SkillTitle: m.SkillTitle, Version: m.Version, SHA256: m.SHA256, Fallback: m.Fallback}
}

// hashText 返回文本的 sha256 十六进制串。
func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// BuiltinMethod 返回环节的内置方法。
func BuiltinMethod(step Step) Method {
	return Method{Source: SourceBuiltin, Body: step.Builtin, SHA256: hashText(step.Builtin)}
}

// SkillMethod 读取技能指定版本作为方法，不做兜底：不可用时返回 ErrSkillUnavailable。
// 试跑候选用它（候选不可用要告诉管理员，而不是悄悄换成内置）。
func SkillMethod(ctx context.Context, lib SkillLibrary, step Step, name string, version int) (Method, error) {
	text, err := lib.LoadMethod(ctx, name, version, step.EffectiveMethodMax())
	if err != nil {
		return Method{}, err
	}
	body := clipRunes(text.Body, step.EffectiveMethodMax())
	return Method{Source: SourceSkill, SkillName: text.Name, SkillTitle: text.Title, Version: text.Version, Body: body, SHA256: hashText(body), HasScripts: text.HasScripts}, nil
}

// ResolveMethod 把绑定解析成方法：builtin 直接用内置；skill 读指定版本，
// 技能被停用、被删或版本被删（ErrSkillUnavailable）时退回内置并标 Fallback，不让用户端失败。
// 读库等基础设施错误原样返回。
func ResolveMethod(ctx context.Context, lib SkillLibrary, step Step, b Binding) (Method, error) {
	if b.Source != SourceSkill {
		return BuiltinMethod(step), nil
	}
	if b.SkillName == "" {
		return fallbackMethod(step, "绑定的技能已被删除"), nil
	}
	m, err := SkillMethod(ctx, lib, step, b.SkillName, b.SkillVersion)
	if errors.Is(err, ErrSkillUnavailable) {
		return fallbackMethod(step, fmt.Sprintf("技能 %s v%d 已停用或版本已删除", b.SkillName, b.SkillVersion)), nil
	}
	if err != nil {
		return Method{}, err
	}
	return m, nil
}

// fallbackMethod 返回标了兜底的内置方法。
func fallbackMethod(step Step, reason string) Method {
	m := BuiltinMethod(step)
	m.Fallback, m.FallbackReason = true, reason
	return m
}

// Resolver 取一个环节当前生效的方法：读绑定，解析，必要时兜底。后端业务（营销等）每次运行前调用它。
type Resolver struct {
	Bindings BindingStore
	Skills   SkillLibrary
	// OnFallback 在环节退回内置时调用（可为空），用来写日志和审计；它不能让解析失败。
	OnFallback func(ctx context.Context, step Step, b Binding, reason string)
}

// NewResolver 创建解析器。
func NewResolver(bindings BindingStore, skills SkillLibrary) *Resolver {
	return &Resolver{Bindings: bindings, Skills: skills}
}

// Binding 返回环节的绑定；没有记录时是内置。
func (r *Resolver) Binding(ctx context.Context, stepKey string) (Binding, error) {
	b, found, err := r.Bindings.GetBinding(ctx, stepKey)
	if err != nil {
		return Binding{}, err
	}
	if !found {
		return Binding{StepKey: stepKey, Source: SourceBuiltin}, nil
	}
	return b, nil
}

// Method 返回环节当前生效的方法（已兜底）。
func (r *Resolver) Method(ctx context.Context, step Step) (Method, error) {
	b, err := r.Binding(ctx, step.Key)
	if err != nil {
		return Method{}, err
	}
	m, err := ResolveMethod(ctx, r.Skills, step, b)
	if err != nil {
		return Method{}, err
	}
	if m.Fallback && r.OnFallback != nil {
		r.OnFallback(ctx, step, b, m.FallbackReason)
	}
	return m, nil
}
