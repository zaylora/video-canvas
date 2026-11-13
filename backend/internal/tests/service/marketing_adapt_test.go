package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
)

const mktH3Key = "video-h3"

// useH3 登记一个提示词格式为 h3 的视频模型（其余能力同 video-a），并让转写调用按提示词现拼合格回复。
func (e *mktEnv) useH3() {
	snap := mktVideoSnapshot()
	snap.Model.Key = mktH3Key
	snap.Model.Capabilities.PromptFormat = "h3"
	e.models.Snapshots[mktH3Key] = snap
	e.caller.byText[mktAdaptStep] = func(prompt string) (string, error) { return h3Reply(prompt), nil }
}

// h3Scripts 建一个用 H3 出片的无参考项目，跑完第一轮，返回项目 id 和脚本。
func (e *mktEnv) h3Scripts() (idcodec.ID, []model.MarketingScriptView) {
	e.t.Helper()
	st := mktSettings()
	st.VideoModel = mktH3Key
	v := e.createProject(st)
	e.runRound(&v.Rounds[0])
	return v.ID, e.round(v.ID, v.Rounds[0].ID).Scripts
}

// adaptVars 取出转写提示词末尾的输入 JSON。
func adaptVars(prompt string) (refs []struct{ Label string }, shots []struct {
	No        int
	Cut       string
	Narration string
}) {
	i := strings.LastIndex(prompt, "```json\n")
	j := strings.LastIndex(prompt, "\n```")
	var vars struct {
		References []struct{ Label string } `json:"references"`
		Shots      []struct {
			No        int    `json:"no"`
			Cut       string `json:"cut"`
			Narration string `json:"narration"`
		} `json:"shots"`
	}
	_ = json.Unmarshal([]byte(prompt[i+len("```json\n"):j]), &vars)
	for _, s := range vars.Shots {
		shots = append(shots, struct {
			No        int
			Cut       string
			Narration string
		}{s.No, s.Cut, s.Narration})
	}
	return vars.References, shots
}

// h3Reply 按转写输入拼一份合格的 H3 六段稿：标签全用上，镜头与切点、旁白原文照搬。
func h3Reply(prompt string) string {
	refs, shots := adaptVars(prompt)
	labels := make([]string, 0, len(refs))
	for _, r := range refs {
		labels = append(labels, r.Label)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "subject_definitions:\n<Subject 1> is the product in %s.\n\n", strings.Join(labels, ", "))
	b.WriteString("summary:\n[reference generation] A product video of <Subject 1>.\n\n")
	b.WriteString("retention_analysis:\n<Subject 1> (appears in [Shot 1]): fully_preserved - shape retained.\n\n")
	b.WriteString("detailed_description:\nA realistic vertical product video.\n")
	for _, s := range shots {
		if s.Cut == "" {
			fmt.Fprintf(&b, "[Shot %d] A close-up of <Subject 1>. The narrator (S1) says: <d>[Chinese] %s</d>\n", s.No, s.Narration)
		} else {
			fmt.Fprintf(&b, "[Shot %d] At %s, the shot cuts to <Subject 1>. The narrator (S1) says: <d>[Chinese] %s</d>\n", s.No, s.Cut, s.Narration)
		}
	}
	b.WriteString("\noverall_soundscape:\nQuiet room tone.\n\nnon_diegetic_music:\nLight piano.")
	raw, _ := json.Marshal(map[string]string{"prompt": b.String()})
	return string(raw)
}

// adaptLedger 数脚本的转写扣费流水。
func (e *mktEnv) adaptLedger(sc idcodec.ID) []model.CreditLedger {
	e.t.Helper()
	var rows []model.CreditLedger
	if err := e.db.Where("marketing_script_id = ?", uint64(sc)).Order("id").Find(&rows).Error; err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func TestMarketing_RenderH3_AdaptsOnceAndUsesAdaptation(t *testing.T) {
	env := newMktEnv(t, 300)
	env.useH3()
	_, scripts := env.h3Scripts()
	ctx := context.Background()
	sc := scripts[0]
	if sc.RenderCredits <= 20 {
		t.Fatalf("还没转写时，出片报价应含转写预估：%d", sc.RenderCredits)
	}
	before := env.credit().Balance
	got, err := env.render(sc.ID, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if n := env.caller.count(mktAdaptStep); n != 1 {
		t.Fatalf("应转写一次：%d", n)
	}
	var input map[string]any
	_ = json.Unmarshal(env.scriptTasks(sc.ID)[0].InputJSON, &input)
	prompt, _ := input["prompt"].(string)
	if !strings.HasPrefix(prompt, "subject_definitions:") || !strings.Contains(prompt, "<Picture 1>") || strings.Contains(prompt, "@图片") {
		t.Fatalf("出片应使用 H3 转写稿：%s", prompt)
	}
	ledger := env.adaptLedger(sc.ID)
	if len(ledger) != 2 || ledger[0].Type != model.LedgerFreeze || ledger[1].Type != model.LedgerSettle {
		t.Fatalf("转写应成对写冻结与结算流水：%+v", ledger)
	}
	if env.credit().Balance >= before {
		t.Fatalf("转写和出片都应扣费：%d → %d", before, env.credit().Balance)
	}
	if got.RenderCredits != 20 {
		t.Fatalf("已有有效转写稿，报价只剩出片费：%d", got.RenderCredits)
	}

	// 预览：h3 返回原稿（Seedance 写法）和未过期的转写稿。
	pv, err := env.svc.Prompt(ctx, env.user, uint64(sc.ID), "")
	if err != nil {
		t.Fatal(err)
	}
	if pv.Format != "h3" || !strings.Contains(pv.Prompt, "[分镜]") || pv.Adaptation == nil || pv.Adaptation.Stale || pv.Adaptation.Prompt != prompt {
		t.Fatalf("预览不对：%+v", pv)
	}
}

func TestMarketing_H3Adaptation_StaleAfterNarrationEditAndEditable(t *testing.T) {
	env := newMktEnv(t, 300)
	env.useH3()
	_, scripts := env.h3Scripts()
	ctx := context.Background()
	sc := scripts[0]
	if _, err := env.svc.Adapt(ctx, env.user, uint64(sc.ID), &model.AdaptMarketingScriptReq{}); err != nil {
		t.Fatal(err)
	}
	// 手改转写稿：标记已修改，仍未过期。
	pv, err := env.svc.EditAdaptation(ctx, env.user, uint64(sc.ID), &model.EditMarketingAdaptationReq{Format: "h3", Prompt: "手改过的稿"})
	if err != nil {
		t.Fatal(err)
	}
	if pv.Adaptation == nil || !pv.Adaptation.Edited || pv.Adaptation.Stale || pv.Adaptation.Prompt != "手改过的稿" {
		t.Fatalf("手改：%+v", pv.Adaptation)
	}
	// 改旁白：原稿变了，转写稿过期。
	narr := make([]string, len(sc.Shots))
	for i, s := range sc.Shots {
		narr[i] = s.Narration
	}
	narr[0] = "换一句开场白"
	if _, err := env.svc.UpdateScript(ctx, env.user, uint64(sc.ID), &model.UpdateMarketingScriptReq{Narrations: narr, Revision: sc.Revision}); err != nil {
		t.Fatal(err)
	}
	pv, err = env.svc.Prompt(ctx, env.user, uint64(sc.ID), "")
	if err != nil {
		t.Fatal(err)
	}
	if pv.Adaptation == nil || !pv.Adaptation.Stale {
		t.Fatalf("改了旁白，转写稿应过期：%+v", pv.Adaptation)
	}
	// 出片时自动重新转写，用的是新旁白。
	if _, err := env.render(sc.ID, "k2"); err != nil {
		t.Fatal(err)
	}
	if n := env.caller.count(mktAdaptStep); n != 2 {
		t.Fatalf("过期后出片应重新转写：%d", n)
	}
	var input map[string]any
	_ = json.Unmarshal(env.scriptTasks(sc.ID)[0].InputJSON, &input)
	if !strings.Contains(input["prompt"].(string), "换一句开场白") {
		t.Fatalf("应用新旁白重新转写：%v", input["prompt"])
	}
}

func TestMarketing_H3Adaptation_FailureChargesNothing(t *testing.T) {
	env := newMktEnv(t, 300)
	env.useH3()
	env.caller.byText[mktAdaptStep] = func(string) (string, error) { return `{"prompt":"随便写写"}`, nil }
	_, scripts := env.h3Scripts()
	sc := scripts[0]
	_, err := env.render(sc.ID, "k1")
	var biz *errcode.Error
	if !errors.As(err, &biz) || biz.Code != errcode.ErrMarketingAdaptFailed.Code {
		t.Fatalf("两次都不合格应报转写失败：%v", err)
	}
	if n := env.caller.count(mktAdaptStep); n != 2 {
		t.Fatalf("不合格应带反馈重写一次：%d", n)
	}
	if len(env.adaptLedger(sc.ID)) != 0 || len(env.scriptTasks(sc.ID)) != 0 {
		t.Fatal("转写失败不扣费、不出片")
	}
}

func TestMarketing_RenderRoundH3_AdaptsAllInParallel(t *testing.T) {
	env := newMktEnv(t, 300)
	env.useH3()
	var mu sync.Mutex
	seen := map[string]bool{}
	env.caller.byText[mktAdaptStep] = func(prompt string) (string, error) {
		mu.Lock()
		seen[prompt] = true
		mu.Unlock()
		return h3Reply(prompt), nil
	}
	_, scripts := env.h3Scripts()
	r, err := env.svc.RenderRound(context.Background(), env.user, uint64(scripts[0].RoundID), &model.RenderMarketingRoundReq{IdempotencyKey: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Skipped) != 0 || len(seen) != len(scripts) {
		t.Fatalf("每份脚本各转写一次并出片：skipped=%+v adapted=%d scripts=%d", r.Skipped, len(seen), len(scripts))
	}
	for _, sc := range scripts {
		if len(env.scriptTasks(sc.ID)) != 1 {
			t.Fatalf("脚本 %v 应出片", sc.ID)
		}
	}
}
