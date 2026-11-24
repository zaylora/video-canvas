package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
)

// DramaApprovalPayload 是短剧审批的不可变内容；签名绑定用户、画布、会话、运行与工具。
type DramaApprovalPayload struct {
	DramaID   idcodec.ID      `json:"drama_id"`
	Title     string          `json:"title"`  // 审批卡标题，通用修改卡直接显示
	Reason    string          `json:"reason"` // 模型给出的修改理由，可为空
	EpisodeNo int             `json:"episode_no,omitempty"`
	SegmentID idcodec.ID      `json:"segment_id,omitempty"`
	Before    string          `json:"before"`
	After     string          `json:"after"`
	Args      json.RawMessage `json:"args"`
	Estimate  int             `json:"estimate"`
	Signature string          `json:"signature"`
}

// parseDramaArgs 不接收schema之外的字段，避免绕过工具限制修改标题、采用任务等数据。
func parseDramaArgs(raw json.RawMessage, out any) error {
	d := json.NewDecoder(bytes.NewReader(stripDramaReason(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return fail("短剧工具参数格式不对：%v", err)
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fail("短剧工具参数必须是一个 JSON 对象")
	}
	return nil
}

// stripDramaReason 去掉审批理由字段：它只用于展示，不属于业务服务的请求体。
func stripDramaReason(raw json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	if _, ok := m["reason"]; !ok {
		return raw
	}
	delete(m, "reason")
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// dramaReason 读取模型给的理由，截断到 500 字避免撑爆审批卡。
func dramaReason(raw json.RawMessage) string {
	var a struct {
		Reason string `json:"reason"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	if r := []rune(a.Reason); len(r) > 500 {
		return string(r[:500])
	}
	return a.Reason
}

// dramaTitle 为通用修改卡生成标题。
func dramaTitle(kind string, p DramaApprovalPayload) string {
	switch kind {
	case "drama_update_outline":
		return "修改大纲与人物传记"
	case "drama_update_episode":
		return fmt.Sprintf("修改第 %d 集", p.EpisodeNo)
	case "drama_update_segment":
		return fmt.Sprintf("修改第 %d 集的片段", p.EpisodeNo)
	}
	var a struct {
		Step     string `json:"step"`
		Episodes []int  `json:"episodes"`
	}
	_ = json.Unmarshal(p.Args, &a)
	return "运行短剧任务：" + dramaStepLabel(a.Step)
}

// dramaStepLabel 把流水线步骤翻成界面用语。
func dramaStepLabel(step string) string {
	labels := map[string]string{"analyze": "原文研读", "outline": "大纲", "scripts": "分集剧本", "breakdown": "资产拆解", "asset_prompts": "资产提示词", "storyboard": "分镜脚本", "compose": "合成整集"}
	if l, ok := labels[step]; ok {
		return l
	}
	return step
}

// dramaProposal 签名修改前后和原始版本，审批期间的数据变化必须由业务层的revision拒绝。
func (b *AgentBridge) dramaProposal(ctx context.Context, tc *toolCall, kind string, p DramaApprovalPayload) (*toolOut, error) {
	if len(b.d.DramaApprovalKey) == 0 {
		return nil, fail("短剧审批签名未配置")
	}
	p.Title, p.Reason = dramaTitle(kind, p), dramaReason(tc.args)
	p.Signature = b.dramaSignature(tc.run, kind, p)
	// 外部接入（MCP）没有审批卡片可点，勾了权限就直连执行：审批在这条通道上不是安全边界，
	// 权限档与余额才是。真正的业务校验（锁定集、revision、并发、积分）仍在短剧服务里，一份规则。
	if tc.external {
		return b.dramaDirect(ctx, tc, kind, p)
	}
	if err := b.requestApproval(ctx, tc, kind, p, p.Estimate); err != nil {
		return nil, err
	}
	summary := "修改短剧，等待审批"
	if kind == "drama_run_job" {
		summary = fmt.Sprintf("运行短剧任务，预估 %d 积分", p.Estimate)
	}
	return &toolOut{content: "已提交给用户确认，等待决定。批准后才会修改或提交任务。", summary: summary, terminate: true}, nil
}

// dramaDirect 是外部接入的短剧执行：直接落到 executeDramaWrite，与审批执行走同一套业务调用。
//
// 不新增一份写逻辑，是因为「锁定集只能改标题」「revision 不一致就失败」「同项目只能跑一个任务」
// 这些规则只能有一处实现——外部接入不该有第二套。
func (b *AgentBridge) dramaDirect(ctx context.Context, tc *toolCall, kind string, p DramaApprovalPayload) (*toolOut, error) {
	project, err := b.dramaProject(ctx, tc.run.UserID, tc.run.CanvasID)
	if err != nil {
		return nil, err
	}
	if project.ID != p.DramaID {
		return nil, errcode.ErrDramaNotFound
	}
	rc := RunContext{
		RunID: tc.run.ID, SessionID: tc.run.SessionID, UserID: tc.run.UserID,
		CanvasID: tc.run.CanvasID, Mode: tc.run.Mode,
		BudgetCredits: tc.run.BudgetCredits, SpentExtra: tc.run.SpentCredits,
	}
	value, err := b.executeDramaWrite(ctx, rc, kind, externalDramaKey(tc.id), project, p)
	if err != nil {
		return nil, err
	}
	content, err := jsonOut(value)
	if err != nil {
		return nil, err
	}
	return &toolOut{content: content, summary: p.Title, terminate: true}, nil
}

// externalDramaKey 把外部工具调用 ID 折成 createAgentJob 需要的幂等键。
//
// 短剧服务要求审批 ID 非零，并把它当幂等键：同一次审批重放不会重复建任务、重复扣积分。
// 外部接入没有审批单，用调用 ID 派生一个，最高位置 1 以免与自增审批 ID 相撞。
// 注意它挡的是「同一次调用被内部重放」，不是「客户端主动重发」——重发是新调用、新键，会新建任务。
func externalDramaKey(toolCallID string) uint64 {
	sum := sha256.Sum256([]byte("mcp-drama:" + toolCallID))
	return binary.BigEndian.Uint64(sum[:8]) | (1 << 63)
}

// toolDramaOutline 只允许审批大纲和人物传记；revision由实际读取结果提供。
func (b *AgentBridge) toolDramaOutline(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		OutlineMD    *string `json:"outline_md"`
		CharactersMD *string `json:"characters_md"`
	}
	if err := parseDramaArgs(tc.args, &a); err != nil {
		return nil, err
	}
	if a.OutlineMD == nil && a.CharactersMD == nil {
		return nil, fail("至少修改大纲或人物传记一项")
	}
	p, err := b.dramaProject(ctx, tc.run.UserID, tc.run.CanvasID)
	if err != nil {
		return nil, err
	}
	req := model.UpdateDramaReq{Revision: p.Revision, OutlineMD: a.OutlineMD, CharactersMD: a.CharactersMD}
	args, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	before, _ := jsonOut(map[string]string{"outline_md": p.OutlineMD, "characters_md": p.CharactersMD})
	after := map[string]string{"outline_md": p.OutlineMD, "characters_md": p.CharactersMD}
	if a.OutlineMD != nil {
		after["outline_md"] = *a.OutlineMD
	}
	if a.CharactersMD != nil {
		after["characters_md"] = *a.CharactersMD
	}
	text, _ := jsonOut(after)
	return b.dramaProposal(ctx, tc, "drama_update_outline", DramaApprovalPayload{DramaID: p.ID, Before: before, After: text, Args: args})
}

// toolDramaEpisode 预检锁定规则，并保存审批时分集版本，执行仍经过相同业务服务。
func (b *AgentBridge) toolDramaEpisode(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		EpisodeNo int     `json:"episode_no"`
		Title     *string `json:"title"`
		Synopsis  *string `json:"synopsis"`
		ScriptMD  *string `json:"script_md"`
	}
	if err := parseDramaArgs(tc.args, &a); err != nil {
		return nil, err
	}
	if a.EpisodeNo < 1 || a.EpisodeNo > 100000 || (a.Title == nil && a.Synopsis == nil && a.ScriptMD == nil) {
		return nil, fail("需要有效集号和至少一个修改字段")
	}
	if a.Title != nil && utf8.RuneCountInString(*a.Title) > 255 {
		return nil, fail("集标题最多255字")
	}
	p, err := b.dramaProject(ctx, tc.run.UserID, tc.run.CanvasID)
	if err != nil {
		return nil, err
	}
	ep, err := b.d.Drama.GetEpisode(ctx, tc.run.UserID, uint64(p.ID), a.EpisodeNo)
	if err != nil {
		return nil, err
	}
	if ep.Locked && (a.Synopsis != nil || a.ScriptMD != nil) {
		return nil, errcode.ErrDramaEpisodeLocked
	}
	req := model.UpdateDramaEpisodeReq{Revision: ep.Revision, Title: a.Title, Synopsis: a.Synopsis, ScriptMD: a.ScriptMD}
	args, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	before, _ := jsonOut(map[string]string{"title": ep.Title, "synopsis": ep.Synopsis, "script_md": ep.ScriptMD})
	after := map[string]string{"title": ep.Title, "synopsis": ep.Synopsis, "script_md": ep.ScriptMD}
	if a.Title != nil {
		after["title"] = *a.Title
	}
	if a.Synopsis != nil {
		after["synopsis"] = *a.Synopsis
	}
	if a.ScriptMD != nil {
		after["script_md"] = *a.ScriptMD
	}
	text, _ := jsonOut(after)
	return b.dramaProposal(ctx, tc, "drama_update_episode", DramaApprovalPayload{DramaID: p.ID, EpisodeNo: a.EpisodeNo, Before: before, After: text, Args: args})
}

// toolDramaSegment 用项目片段列表校验归属，参数不能选择本用户其他短剧的片段。
func (b *AgentBridge) toolDramaSegment(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		SegmentID idcodec.ID `json:"segment_id"`
		Prompt    *string    `json:"prompt"`
		Seconds   *int       `json:"seconds"`
	}
	if err := parseDramaArgs(tc.args, &a); err != nil {
		return nil, err
	}
	if a.SegmentID == 0 || (a.Prompt == nil && a.Seconds == nil) || (a.Seconds != nil && *a.Seconds < 1) {
		return nil, fail("需要有效片段ID和提示词或正整数秒数")
	}
	p, err := b.dramaProject(ctx, tc.run.UserID, tc.run.CanvasID)
	if err != nil {
		return nil, err
	}
	seg, err := b.dramaSegment(ctx, tc.run.UserID, p, uint64(a.SegmentID))
	if err != nil {
		return nil, err
	}
	req := model.UpdateDramaSegmentReq{Revision: seg.Revision, Prompt: a.Prompt, Seconds: a.Seconds}
	args, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	before, _ := jsonOut(map[string]any{"prompt": seg.Prompt, "seconds": seg.Seconds})
	after := map[string]any{"prompt": seg.Prompt, "seconds": seg.Seconds}
	if a.Prompt != nil {
		after["prompt"] = *a.Prompt
	}
	if a.Seconds != nil {
		after["seconds"] = *a.Seconds
	}
	text, _ := jsonOut(after)
	return b.dramaProposal(ctx, tc, "drama_update_segment", DramaApprovalPayload{DramaID: p.ID, SegmentID: a.SegmentID, EpisodeNo: seg.EpisodeNo, Before: before, After: text, Args: args})
}

// toolDramaJob 预估与任务执行共用短剧服务，不创建任务、不冻结积分。
func (b *AgentBridge) toolDramaJob(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var req model.DramaJobReq
	if err := parseDramaArgs(tc.args, &req); err != nil {
		return nil, err
	}
	if err := validateDramaJob(req); err != nil {
		return nil, err
	}
	p, err := b.dramaProject(ctx, tc.run.UserID, tc.run.CanvasID)
	if err != nil {
		return nil, err
	}
	quote, err := b.d.Drama.Estimate(ctx, tc.run.UserID, uint64(p.ID), &req)
	if err != nil {
		return nil, err
	}
	args, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	before, _ := jsonOut(map[string]any{"active_jobs": p.ActiveJobs})
	return b.dramaProposal(ctx, tc, "drama_run_job", DramaApprovalPayload{DramaID: p.ID, Before: before, After: string(args), Args: args, Estimate: quote})
}

// validateDramaJob 校验非HTTP调用同样要遵守的参数边界。
func validateDramaJob(req model.DramaJobReq) error {
	if !slices.Contains([]string{"scripts", "breakdown", "asset_prompts", "storyboard", "compose"}, req.Step) {
		return fail("不支持的短剧任务步骤")
	}
	if len(req.Episodes) > 200 {
		return fail("单次最多200集")
	}
	for _, no := range req.Episodes {
		if no < 1 || no > 100000 {
			return fail("集号必须是1到100000")
		}
	}
	if req.Ratio != "" && !slices.Contains([]string{"9:16", "16:9", "21:9", "3:4", "4:3", "1:1"}, req.Ratio) {
		return fail("画面比例不合法")
	}
	if req.MaxSeconds != 0 && (req.MaxSeconds < 4 || req.MaxSeconds > 60) {
		return fail("max_seconds 必须是4到60")
	}
	return nil
}
