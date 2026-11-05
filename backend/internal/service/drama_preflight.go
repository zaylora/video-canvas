package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// DramaPreflightInput 是文件或粘贴文本与可选的预估参数。
type DramaPreflightInput struct {
	Text           string `form:"text"`
	Filename       string `form:"-"`
	Data           []byte `form:"-"`
	SourceKind     string `form:"source_kind" binding:"omitempty,oneof=script novel"`
	TextModel      string `form:"text_model" binding:"max=128"`
	EpisodeSeconds int    `form:"episode_seconds" binding:"omitempty,min=30,max=180"`
}

// DramaPreflightView 是仅含目录、不含正文的预检结果。
type DramaPreflightView struct {
	SourceToken string                    `json:"source_token"`
	Filename    string                    `json:"filename"`
	Chars       int                       `json:"chars"`
	KindGuess   string                    `json:"kind_guess"`
	KindSignals string                    `json:"kind_signals"`
	UnitKind    string                    `json:"unit_kind"`
	Units       []model.DramaUnit         `json:"units"`
	UnitsTotal  int                       `json:"units_total"`
	Issues      []drama.Issue             `json:"issues"`
	Estimate    model.DramaSourceEstimate `json:"estimate"`
}

// Preflight 解析原文、规则去噪切分，预估后保存二十四小时草稿。
func (s *DramaService) Preflight(ctx context.Context, userID uint64, req *DramaPreflightInput) (*DramaPreflightView, error) {
	// 1. 先对提取原文执行五百万字上限，不能依靠去噪绕过限制。
	if req.Filename != "" && strings.TrimSpace(req.Text) != "" {
		return nil, errcode.ErrDramaSourceInvalid.WithMsg("文件和文本只能选一个")
	}
	filename, data := req.Filename, req.Data
	if filename == "" {
		filename = "source.txt"
		data = []byte(req.Text)
	}
	source, err := drama.ParseSource(filename, data)
	if errors.Is(err, drama.ErrSourceTooLong) {
		return nil, errcode.ErrDramaSourceTooLong
	}
	if err != nil {
		return nil, errcode.ErrDramaSourceInvalid
	}
	p := drama.Preflight(source, req.SourceKind)
	if len(p.Units) == 0 {
		return nil, errcode.ErrDramaSourceInvalid
	}
	// 2. 规则认不出体裁、用户也没指定时，让 AI 看开头 / 中间 / 结尾的采样判断；失败保持 unknown 由用户手选。
	if p.KindGuess == "unknown" && req.SourceKind == "" {
		if kind, reason, ok := s.guessKindByAI(ctx, req.TextModel, p.Text); ok {
			ruleSignals := p.KindSignals
			p = drama.Preflight(source, kind)
			p.KindGuess = kind
			p.KindSignals = "AI 判断为" + map[string]string{"script": "剧本", "novel": "小说"}[kind] + aiReasonSuffix(reason) + "；规则统计：" + ruleSignals
		}
	}
	// 3. 报价只读取配置；模型不可用不创建草稿。
	estimate, err := s.estimateSource(ctx, p, model.DramaSourceEstimateReq{SourceKind: req.SourceKind, EpisodeSeconds: req.EpisodeSeconds, TextModel: req.TextModel}, true)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(p.Text))
	units, _ := json.Marshal(drama.IndexUnits(p.Text, p.Units))
	signals, _ := json.Marshal(map[string]any{"kind_signals": p.KindSignals, "unit_kind": p.UnitKind, "issues": p.Issues})
	draft := model.DramaSourceDraft{ID: uuid.NewString(), UserID: userID, Filename: drama.Clip(req.Filename, 255), Text: p.Text, UnitsJSON: units, KindGuess: p.KindGuess, SignalsJSON: signals, Chars: utf8.RuneCountInString(source), SHA256: hex.EncodeToString(hash[:]), ExpiresAt: time.Now().Add(24 * time.Hour), CreatedAt: time.Now()}
	if err = s.settings.CreateDraft(ctx, &draft); err != nil {
		return nil, err
	}
	return &DramaPreflightView{SourceToken: draft.ID, Filename: draft.Filename, Chars: draft.Chars, KindGuess: p.KindGuess, KindSignals: p.KindSignals, UnitKind: p.UnitKind, Units: unitModels(p.Units, userID, 0, false), UnitsTotal: len(p.Units), Issues: p.Issues, Estimate: *estimate}, nil
}

// kindGuessTimeout 预检里 AI 判断体裁的最长等待；超时按认不出处理，不拖慢上传。
const kindGuessTimeout = 30 * time.Second

// guessKindByAI 让文本模型判断原文是剧本还是小说。只发一小段采样，平台承担这次调用，不向用户扣积分；
// 模型不可用、超时、输出不合规一律返回 ok=false，调用方保持 unknown 让用户手选。
func (s *DramaService) guessKindByAI(ctx context.Context, modelKey, text string) (kind, reason string, ok bool) {
	snap, err := s.resolvePlanningModel(ctx, modelKey)
	if err != nil {
		return "", "", false
	}
	callCtx, cancel := context.WithTimeout(ctx, kindGuessTimeout)
	defer cancel()
	res, err := s.streamText(callCtx, snap, drama.KindPrompt(drama.KindSample(text)), func(c context.Context) error { return c.Err() })
	if err != nil {
		logger.Warn("预检 AI 判断体裁失败", zap.Error(err))
		return "", "", false
	}
	return drama.ParseKindGuess(res.Text)
}

// aiReasonSuffix 把模型给的一句话依据接成「（依据）」；没有依据为空。
func aiReasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return "（" + reason + "）"
}

// EstimateSource 根据当前类型重新切分有效草稿，只计算自动研读与规划。
func (s *DramaService) EstimateSource(ctx context.Context, userID uint64, token string, req *model.DramaSourceEstimateReq) (*model.DramaSourceEstimate, error) {
	// 1. 草稿不存在、已消费、过期和他人所有统一返回 67014。
	draft, err := s.settings.Draft(ctx, userID, token, false)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrDramaSourceExpired
	}
	if err != nil {
		return nil, err
	}
	p := drama.Preflight(draft.Text, req.SourceKind)
	return s.estimateSource(ctx, p, *req, true)
}

// estimateSource 采用设计的初始系数，规划报价按实际完整批次计算。
func (s *DramaService) estimateSource(ctx context.Context, p drama.PreflightResult, req model.DramaSourceEstimateReq, includeAnalyze bool) (*model.DramaSourceEstimate, error) {
	kind, end, seconds, key := req.SourceKind, req.RangeEnd, req.EpisodeSeconds, req.TextModel
	if end == 0 {
		end = len(p.Units)
	}
	if end < 1 || end > len(p.Units) {
		return nil, errcode.ErrInvalidParams.WithMsg("规划范围超出原文单元")
	}
	if seconds == 0 {
		seconds = 60
	}
	if kind == "" {
		kind = p.KindGuess
	}
	snap, err := s.resolvePlanningModel(ctx, key)
	if err != nil {
		return nil, err
	}
	chars := 0
	for _, u := range p.Units[:end] {
		chars += u.Chars
	}
	out := &model.DramaSourceEstimate{}
	if kind == "script" {
		out.EpisodesExact = true
		out.EpisodesMin = len(drama.ScriptEpisodes(p.Units, seconds))
		out.EpisodesMax = out.EpisodesMin
	} else {
		out.EpisodesMin = max(1, (chars*60+2500*seconds-1)/(2500*seconds))
		out.EpisodesMax = max(out.EpisodesMin, (chars*60+1500*seconds-1)/(1500*seconds))
	}
	project := &model.DramaProject{SourceText: p.Text, SourceChars: utf8.RuneCountInString(p.Text)}
	requests := 0
	if includeAnalyze {
		out.Credits = dramaQuote(project, "analyze", nil, snap)
		requests = 1
	}
	if kind != "script" {
		for cursor := 0; cursor < end; {
			batch, e := drama.NextBatch(p.Units, cursor, end, min(30000, dramaContextLimit(snap)-8192))
			if e != nil {
				return nil, errcode.ErrDramaContext
			}
			project.SourceChars = utf8.RuneCountInString(drama.UnitSource(batch))
			out.Credits += dramaQuote(project, "plan", nil, snap)
			requests++
			cursor = batch[len(batch)-1].No
		}
	}
	analyzeSeconds := 0
	if includeAnalyze {
		analyzeSeconds, err = s.dramaStepSeconds(ctx, snap.ModelRevisionID, "analyze")
		if err != nil {
			return nil, err
		}
		requests--
	}
	planSeconds, err := s.dramaStepSeconds(ctx, snap.ModelRevisionID, "plan")
	if err != nil {
		return nil, err
	}
	out.Minutes = (analyzeSeconds + requests*planSeconds + 59) / 60
	return out, nil
}

// resolvePlanningModel 建剧、修改与估价都拒绝未知或低于 128k 的上下文窗口。
func (s *DramaService) resolvePlanningModel(ctx context.Context, key string) (*provider.Snapshot, error) {
	snap, err := s.resolveModel(ctx, key)
	if err != nil {
		return nil, err
	}
	if c := snap.Model.Capabilities.Context; c == nil || c.Window < 128000 {
		return nil, errcode.ErrDramaContext.WithMsg("模型上下文低于 128k，装不下一批完整原文")
	}
	return snap, nil
}

// unitModels 复制纯单元到模型，目录不带正文。
func unitModels(units []drama.Unit, userID, dramaID uint64, withText bool) []model.DramaUnit {
	out := make([]model.DramaUnit, 0, len(units))
	for _, u := range units {
		row := model.DramaUnit{UserID: userID, DramaID: dramaID, No: u.No, Kind: u.Kind, Title: u.Title, Chars: u.Chars}
		if withText {
			row.Text = u.Text
		}
		out = append(out, row)
	}
	return out
}

// sourceUnits 复制有序数据库单元为纯函数输入。
func sourceUnits(rows []model.DramaUnit) []drama.Unit {
	out := make([]drama.Unit, 0, len(rows))
	for _, u := range rows {
		out = append(out, drama.Unit{No: u.No, Kind: u.Kind, Title: u.Title, Text: u.Text, Chars: u.Chars})
	}
	return out
}

// Units 返回本人原文目录或指定单元正文。
func (s *DramaService) Units(ctx context.Context, userID, id uint64, from, to int, withText bool) ([]model.DramaUnit, error) {
	// 1. 即使没有单元，也先确认项目归属，避免通过空目录泄露资源。
	if _, err := s.repo.Project(ctx, userID, id, false); err != nil {
		return nil, dramaError(err)
	}
	rows, err := s.settings.Units(ctx, userID, id, from, to)
	if err != nil {
		return nil, err
	}
	if withText && len(rows) == 0 {
		return nil, errcode.ErrDramaNotFound
	}
	if !withText {
		for i := range rows {
			rows[i].Text = ""
		}
	}
	if rows == nil {
		rows = []model.DramaUnit{}
	}
	return rows, nil
}
