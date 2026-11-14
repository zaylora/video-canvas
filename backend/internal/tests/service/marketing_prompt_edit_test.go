package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
)

// renderedPrompt 取脚本第 i 次出片任务的提示词。
func (e *mktEnv) renderedPrompt(sc idcodec.ID, i int) string {
	e.t.Helper()
	var input map[string]any
	_ = json.Unmarshal(e.scriptTasks(sc)[i].InputJSON, &input)
	prompt, _ := input["prompt"].(string)
	return prompt
}

// editNarration 把脚本第一镜旁白改掉，让拼出的原稿变化。
func (e *mktEnv) editNarration(sc model.MarketingScriptView, text string) {
	e.t.Helper()
	narr := make([]string, len(sc.Shots))
	for i, s := range sc.Shots {
		narr[i] = s.Narration
	}
	narr[0] = text
	if _, err := e.svc.UpdateScript(context.Background(), e.user, uint64(sc.ID), &model.UpdateMarketingScriptReq{Narrations: narr, Revision: sc.Revision}); err != nil {
		e.t.Fatal(err)
	}
}

func TestMarketing_PromptEdit_SeedanceUsedForRenderAndKeptWhenScriptChanges(t *testing.T) {
	env := newMktEnv(t, 300)
	pid, scripts := env.readyScripts()
	ctx := context.Background()
	sc := scripts[0]
	auto, err := env.svc.Prompt(ctx, env.user, uint64(sc.ID), "")
	if err != nil {
		t.Fatal(err)
	}
	if auto.Edited || auto.Stale {
		t.Fatalf("没手改时不应标记：%+v", auto)
	}

	pv, err := env.svc.EditPrompt(ctx, env.user, uint64(sc.ID), &model.EditMarketingPromptReq{Format: "seedance", Prompt: "  手改的出片提示词  "})
	if err != nil {
		t.Fatal(err)
	}
	if pv.Prompt != "手改的出片提示词" || !pv.Edited || pv.Stale {
		t.Fatalf("手改后预览：%+v", pv)
	}
	if _, err := env.render(sc.ID, "k1"); err != nil {
		t.Fatal(err)
	}
	if got := env.renderedPrompt(sc.ID, 0); got != "手改的出片提示词" {
		t.Fatalf("出片应使用手改稿：%s", got)
	}

	// 脚本改了：手改稿保留，标记过期，出片仍用手改稿。
	env.finishTask(env.scriptTasks(sc.ID)[0], model.TaskFailed, "上游生成失败", nil)
	cur := env.round(pid, sc.RoundID).Scripts[0]
	env.editNarration(cur, "换一句开场白")
	pv, err = env.svc.Prompt(ctx, env.user, uint64(sc.ID), "")
	if err != nil {
		t.Fatal(err)
	}
	if pv.Prompt != "手改的出片提示词" || !pv.Edited || !pv.Stale {
		t.Fatalf("脚本改后应保留手改并标过期：%+v", pv)
	}
	if _, err := env.render(sc.ID, "k2"); err != nil {
		t.Fatal(err)
	}
	if got := env.renderedPrompt(sc.ID, 1); got != "手改的出片提示词" {
		t.Fatalf("过期也照用手改稿：%s", got)
	}

	// 还原：回到按脚本拼出的提示词。
	pv, err = env.svc.RestorePrompt(ctx, env.user, uint64(sc.ID), &model.RestoreMarketingPromptReq{Format: "seedance"})
	if err != nil {
		t.Fatal(err)
	}
	if pv.Edited || pv.Stale || !strings.Contains(pv.Prompt, "换一句开场白") {
		t.Fatalf("还原后应按新脚本拼：%+v", pv)
	}
}

func TestMarketing_PromptEdit_TooLongRejectedAtRender(t *testing.T) {
	env := newMktEnv(t, 300)
	_, scripts := env.readyScripts()
	sc := scripts[0]
	if _, err := env.svc.EditPrompt(context.Background(), env.user, uint64(sc.ID), &model.EditMarketingPromptReq{Format: "seedance", Prompt: strings.Repeat("长", 6001)}); err != nil {
		t.Fatal(err)
	}
	_, err := env.render(sc.ID, "k1")
	var biz *errcode.Error
	if !errors.As(err, &biz) || biz.Code != errcode.ErrMarketingPromptLong.Code {
		t.Fatalf("手改稿超出模型上限应报 68008：%v", err)
	}
}

func TestMarketing_PromptEdit_H3AdaptsFromEditedSource(t *testing.T) {
	env := newMktEnv(t, 300)
	env.useH3()
	var adaptPrompts []string
	env.caller.byText[mktAdaptStep] = func(prompt string) (string, error) {
		adaptPrompts = append(adaptPrompts, prompt)
		return h3Reply(prompt), nil
	}
	_, scripts := env.h3Scripts()
	ctx := context.Background()
	sc := scripts[0]
	if _, err := env.svc.Adapt(ctx, env.user, uint64(sc.ID), &model.AdaptMarketingScriptReq{}); err != nil {
		t.Fatal(err)
	}

	pv, err := env.svc.EditPrompt(ctx, env.user, uint64(sc.ID), &model.EditMarketingPromptReq{Format: "h3", Prompt: "[分镜]\n镜头1 手改的原稿"})
	if err != nil {
		t.Fatal(err)
	}
	if pv.Prompt != "[分镜]\n镜头1 手改的原稿" || !pv.Edited || pv.Adaptation == nil || !pv.Adaptation.Stale {
		t.Fatalf("手改原稿后转写稿应过期：%+v", pv)
	}
	if _, err := env.render(sc.ID, "k1"); err != nil {
		t.Fatal(err)
	}
	last := adaptPrompts[len(adaptPrompts)-1]
	if !strings.Contains(last, "手改的原稿") {
		t.Fatalf("应按手改原稿重新转写：%s", last)
	}
	if _, shots := adaptVars(last); len(shots) != 0 {
		t.Fatalf("手改原稿时镜头表以原稿为准，不再给脚本的镜头表：%+v", shots)
	}
	pv, err = env.svc.Prompt(ctx, env.user, uint64(sc.ID), "")
	if err != nil {
		t.Fatal(err)
	}
	if pv.Adaptation == nil || pv.Adaptation.Stale {
		t.Fatalf("按手改原稿转写后不应过期：%+v", pv.Adaptation)
	}
}
