package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	. "video-canvas/internal/service/agent"
)

func dramaApprovalCases() []struct{ kind, args string } {
	return []struct{ kind, args string }{
		{"drama_update_outline", `{"characters_md":"新人物"}`},
		{"drama_update_episode", `{"episode_no":1,"synopsis":"新梗概"}`},
		{"drama_update_segment", `{"segment_id":"` + idcodec.Encode(20) + `","seconds":9}`},
		{"drama_run_job", `{"step":"compose","episodes":[1]}`},
	}
}

func TestDrama_ApprovalIdentityModeAndTamper(t *testing.T) {
	for _, c := range dramaApprovalCases() {
		t.Run(c.kind, func(t *testing.T) {
			br, f, _ := newDramaBridge(t)
			ctx, rc := context.Background(), platformCtx(990, "drama")
			p, err := br.ProposeApproval(ctx, rc, c.kind, []byte(c.args))
			if err != nil || p.IsError {
				t.Fatalf("%+v %v", p, err)
			}
			o, err := br.ResolveApproval(ctx, rc, c.kind, p.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
			if err != nil {
				t.Fatal(err)
			}
			for _, bad := range []RunContext{
				{RunID: rc.RunID, UserID: 2, SessionID: rc.SessionID, CanvasID: rc.CanvasID, Mode: "drama"},
				{RunID: rc.RunID, UserID: 1, SessionID: rc.SessionID, CanvasID: rc.CanvasID, Mode: "all"},
				{RunID: rc.RunID + 1, UserID: 1, SessionID: rc.SessionID, CanvasID: rc.CanvasID, Mode: "drama"},
			} {
				proposal, err := br.ProposeApproval(ctx, bad, c.kind, []byte(c.args))
				if bad.RunID == rc.RunID && (err != nil || !proposal.IsError) {
					t.Fatalf("非法身份或模式预估 %+v %v", proposal, err)
				}
				if _, err := br.ResolveApproval(ctx, bad, c.kind, p.Payload, model.DecideAgentApprovalReq{Decision: "approve"}); err == nil {
					t.Fatal("跨运行批准")
				}
				if _, err := br.ExecuteApproval(ctx, bad, c.kind, 100, "w", *o); err == nil {
					t.Fatal("跨运行执行")
				}
			}
			if _, err := br.ExecutePlatformTool(ctx, rc, "w", c.kind, []byte(c.args)); err == nil {
				t.Fatal("通用工具入口绕过审批")
			}
			tamper := *o
			tamper.QuoteCredits++
			if _, err := br.ExecuteApproval(ctx, rc, c.kind, 100, "w", tamper); err == nil {
				t.Fatal("篡改结果积分")
			}
			rejected, err := br.ResolveApproval(ctx, rc, c.kind, p.Payload, model.DecideAgentApprovalReq{Decision: "reject"})
			if err != nil {
				t.Fatal(err)
			}
			r, err := br.ExecuteApproval(ctx, rc, c.kind, 100, "w", *rejected)
			if err != nil || r.Status != ExecNone || f.writes != 0 {
				t.Fatalf("拒绝仍写入 %+v %v", r, err)
			}
			rejected.Status = model.ApprovalApproved
			if _, err := br.ExecuteApproval(ctx, rc, c.kind, 100, "w", *rejected); err == nil {
				t.Fatal("拒绝篡改为批准")
			}
			if f.writes != 0 {
				t.Fatal("非法审批写入")
			}
		})
	}
}

func TestDrama_ReplayThenLaterEditConflict(t *testing.T) {
	for _, c := range dramaApprovalCases()[:3] {
		t.Run(c.kind, func(t *testing.T) {
			br, f, _ := newDramaBridge(t)
			ctx, rc := context.Background(), platformCtx(990, "drama")
			p, err := br.ProposeApproval(ctx, rc, c.kind, []byte(c.args))
			if err != nil || p.IsError {
				t.Fatalf("%+v %v", p, err)
			}
			o, err := br.ResolveApproval(ctx, rc, c.kind, p.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				r, err := br.ExecuteApproval(ctx, rc, c.kind, 100, "w", *o)
				if err != nil || r.Status != ExecExecuted {
					t.Fatalf("%+v %v", r, err)
				}
			}
			if f.writes != 1 {
				t.Fatalf("重放写入次数 %d", f.writes)
			}
			switch c.kind {
			case "drama_update_outline":
				if f.view.Revision != 2 {
					t.Fatal("重放增加版本")
				}
				f.view.CharactersMD = "用户的新人物"
				f.view.Revision++
			case "drama_update_episode":
				if f.ep.Revision != 2 {
					t.Fatal("重放增加版本")
				}
				f.ep.Synopsis = "用户新梗概"
				f.ep.Revision++
			case "drama_update_segment":
				if f.segments[0].Revision != 2 {
					t.Fatal("重放增加版本")
				}
				f.segments[0].Seconds = 11
				f.segments[0].Revision++
			}
			r, err := br.ExecuteApproval(ctx, rc, c.kind, 100, "w", *o)
			if err != nil || r.Status != ExecFailed || !strings.Contains(r.OutcomeText, errcode.ErrCanvasConflict.Msg) || f.writes != 1 {
				t.Fatalf("旧审批覆盖后续编辑 %+v %v", r, err)
			}
		})
	}
}

func TestDrama_LockedAfterApprovalAndInvalidRead(t *testing.T) {
	br, f, _ := newDramaBridge(t)
	ctx, rc := context.Background(), platformCtx(990, "drama")
	p, err := br.ProposeApproval(ctx, rc, "drama_update_episode", []byte(`{"episode_no":1,"script_md":"新内容"}`))
	if err != nil || p.IsError {
		t.Fatalf("%+v %v", p, err)
	}
	o, err := br.ResolveApproval(ctx, rc, "drama_update_episode", p.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	f.ep.Locked = true
	r, err := br.ExecuteApproval(ctx, rc, "drama_update_episode", 100, "w", *o)
	if err != nil || r.Status != ExecFailed || !strings.Contains(r.OutcomeText, errcode.ErrDramaEpisodeLocked.Msg) {
		t.Fatalf("锁定后仍写入 %+v %v", r, err)
	}
	for _, raw := range []string{`{"scope":"episode"}`, `{"scope":"segments","episode_no":0}`, `{"scope":"segments","episode_no":101}`, `{"scope":"bad"}`, `{"scope":"overview","drama_id":"elsewhere"}`, `{"scope":"episode","episode_no":1.5}`} {
		r, err := br.ExecutePlatformTool(ctx, rc, "r", "drama_read", json.RawMessage(raw))
		if err != nil || !r.IsError {
			t.Fatalf("非法读取 %s: %+v %v", raw, r, err)
		}
	}
}
