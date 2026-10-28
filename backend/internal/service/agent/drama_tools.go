package agent

import (
	"bytes"
	"context"
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
	if err := b.requestApproval(ctx, tc, kind, p, p.Estimate); err != nil {
		return nil, err
	}
	summary := "修改短剧，等待审批"
	if kind == "drama_run_job" {
		summary = fmt.Sprintf("运行短剧任务，预估 %d 积分", p.Estimate)
	}
	return &toolOut{content: "已提交给用户确认，等待决定。批准后才会修改或提交任务。", summary: summary, terminate: true}, nil
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
	if a.EpisodeNo < 1 || a.EpisodeNo > 100 || (a.Title == nil && a.Synopsis == nil && a.ScriptMD == nil) {
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
	if !slices.Contains([]string{"analyze", "outline", "scripts", "breakdown", "asset_prompts", "storyboard", "compose"}, req.Step) {
		return fail("不支持的短剧任务步骤")
	}
	if len(req.Episodes) > 100 {
		return fail("最多100集")
	}
	for _, no := range req.Episodes {
		if no < 1 || no > 100 {
			return fail("集号必须是1到100")
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
