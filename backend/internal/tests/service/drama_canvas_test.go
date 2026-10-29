package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type dramaCanvasFake struct {
	canvas                                                              model.CanvasProject
	episodes                                                            map[int]model.DramaEpisode
	jobs                                                                map[uint64]model.DramaJob
	reads, transactions, updates                                        int
	getErr, episodeErr, checkErr, updateErr, lockErr, jobErr, markerErr error
	beforeUpdate                                                        func(*dramaCanvasFake)
	// deletedEpisodes 记录已提交的删集（集号），用来确认失败时没删
	deletedEpisodes []int
}

type dramaCanvasFakeTx struct {
	parent   *dramaCanvasFake
	canvas   model.CanvasProject
	episodes map[int]model.DramaEpisode
	jobs     map[uint64]model.DramaJob
}

func newDramaCanvasFake() *dramaCanvasFake {
	return &dramaCanvasFake{canvas: model.CanvasProject{BaseModel: model.BaseModel{ID: 7}, UserID: 1, Revision: 1, PayloadJSON: datatypes.JSON(`{"nodes":[],"edges":[]}`)}, episodes: map[int]model.DramaEpisode{
		1: {UserID: 1, DramaID: 3, No: 1, ScriptStatus: "ready", Revision: 1},
		2: {UserID: 1, DramaID: 3, No: 2, ScriptStatus: "ready", Revision: 1},
	}, jobs: map[uint64]model.DramaJob{
		9:  {BaseModel: model.BaseModel{ID: 9}, UserID: 1, DramaID: 3, Step: "breakdown", Status: "running", Output: datatypes.JSON(`{"result":{"assets":[]},"cost":5}`)},
		10: {BaseModel: model.BaseModel{ID: 10}, UserID: 1, DramaID: 3, Step: "asset_prompts", Status: "running", Output: datatypes.JSON(`{"result":{"prompts":[]},"cost":3}`)},
		11: {BaseModel: model.BaseModel{ID: 11}, UserID: 1, DramaID: 3, Step: "breakdown", Status: "running"},
		12: {BaseModel: model.BaseModel{ID: 12}, UserID: 1, DramaID: 3, Step: "asset_prompts", Status: "running"},
	}}
}

func (f *dramaCanvasFake) GetCanvas(_ context.Context, userID, id uint64) (*model.CanvasProject, error) {
	f.reads++
	if f.getErr != nil {
		return nil, f.getErr
	}
	if userID != f.canvas.UserID || id != f.canvas.ID {
		return nil, repository.ErrNotFound
	}
	c := f.canvas
	c.PayloadJSON = append(datatypes.JSON(nil), c.PayloadJSON...)
	return &c, nil
}

func (f *dramaCanvasFake) WithTx(_ context.Context, fn func(repository.DramaCanvasTx) error) error {
	f.transactions++
	tx := &dramaCanvasFakeTx{parent: f, canvas: f.canvas, episodes: map[int]model.DramaEpisode{}, jobs: map[uint64]model.DramaJob{}}
	for no, ep := range f.episodes {
		tx.episodes[no] = ep
	}
	for id, job := range f.jobs {
		tx.jobs[id] = job
	}
	deleted := len(f.deletedEpisodes)
	if err := fn(tx); err != nil {
		f.deletedEpisodes = f.deletedEpisodes[:deleted]
		return err
	}
	f.canvas, f.episodes = tx.canvas, tx.episodes
	f.jobs = tx.jobs
	return nil
}

func (t *dramaCanvasFakeTx) EpisodesForUpdate(_ context.Context, userID, dramaID, canvasID uint64, nos []int) ([]model.DramaEpisode, error) {
	if t.parent.episodeErr != nil {
		return nil, t.parent.episodeErr
	}
	if userID != 1 || dramaID != 3 || canvasID != 7 {
		return nil, repository.ErrNotFound
	}
	var rows []model.DramaEpisode
	for _, no := range nos {
		if ep, ok := t.episodes[no]; ok {
			rows = append(rows, ep)
		}
	}
	return rows, nil
}

func (t *dramaCanvasFakeTx) CheckCanvasRevision(_ context.Context, userID, canvasID, revision uint64) error {
	if t.parent.checkErr != nil {
		return t.parent.checkErr
	}
	if userID != t.canvas.UserID || canvasID != t.canvas.ID {
		return repository.ErrNotFound
	}
	if revision != t.parent.canvas.Revision {
		return repository.ErrRevisionConflict
	}
	return nil
}

func (t *dramaCanvasFakeTx) UpdateCanvas(_ context.Context, userID, canvasID, revision uint64, payload []byte) error {
	t.parent.updates++
	if hook := t.parent.beforeUpdate; hook != nil {
		t.parent.beforeUpdate = nil
		hook(t.parent)
	}
	if t.parent.updateErr != nil {
		return t.parent.updateErr
	}
	if userID != t.canvas.UserID || canvasID != t.canvas.ID {
		return repository.ErrNotFound
	}
	if revision != t.parent.canvas.Revision {
		return repository.ErrRevisionConflict
	}
	t.canvas.PayloadJSON = datatypes.JSON(payload)
	t.canvas.Revision++
	return nil
}

func (t *dramaCanvasFakeTx) LockEpisodes(_ context.Context, _, _ uint64, nos []int) error {
	if t.parent.lockErr != nil {
		return t.parent.lockErr
	}
	now := time.Now()
	for _, no := range nos {
		ep := t.episodes[no]
		ep.LockedAt = &now
		ep.Revision++
		t.episodes[no] = ep
	}
	return nil
}

func (t *dramaCanvasFakeTx) LockProject(_ context.Context, userID, dramaID uint64) error {
	if userID != 1 || dramaID != 3 {
		return repository.ErrNotFound
	}
	return nil
}

func (t *dramaCanvasFakeTx) HasActiveJob(_ context.Context, _, _ uint64) (bool, error) {
	for _, job := range t.jobs {
		if job.Status == "queued" || job.Status == "running" {
			return true, nil
		}
	}
	return false, nil
}

func (t *dramaCanvasFakeTx) DeleteEpisode(_ context.Context, _, _ uint64, no int) error {
	if _, ok := t.episodes[no]; !ok {
		return repository.ErrNotFound
	}
	t.parent.deletedEpisodes = append(t.parent.deletedEpisodes, no)
	next := map[int]model.DramaEpisode{}
	for n, ep := range t.episodes {
		switch {
		case n < no:
			next[n] = ep
		case n > no:
			ep.No = n - 1
			next[n-1] = ep
		}
	}
	t.episodes = next
	return nil
}

type dramaCanvasBroadcast struct {
	channels []string
	messages []ws.Message
}

func (b *dramaCanvasBroadcast) Publish(_ context.Context, channel string, m ws.Message) {
	b.channels = append(b.channels, channel)
	b.messages = append(b.messages, m)
}

func dramaCanvasAssets(no int) []drama.Asset {
	return []drama.Asset{{Type: "character", Name: "林川", Look: "童年", Aliases: []string{}, Episodes: []int{no}, Description: "勇敢的少年", VoicePrompt: "明亮少年声"}, {Type: "scene", Name: "街道", Aliases: []string{}, Episodes: []int{no}, Description: "城市街道"}}
}

func dramaGraph(t *testing.T, f *dramaCanvasFake) *canvasgraph.Graph {
	t.Helper()
	g, err := canvasgraph.Parse(f.canvas.PayloadJSON)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestDramaCanvasBreakdownCreatesAndMergesAtomically(t *testing.T) {
	ctx := context.Background()
	f := newDramaCanvasFake()
	bc := &dramaCanvasBroadcast{}
	svc := service.NewDramaCanvasService(f, bc)
	created, err := svc.ApplyBreakdown(ctx, 1, 3, 7, 9, []int{1}, dramaCanvasAssets(1))
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 2 || f.episodes[1].LockedAt == nil || f.episodes[1].Revision != 2 {
		t.Fatalf("created=%+v episode=%+v", created, f.episodes[1])
	}
	g := dramaGraph(t, f)
	n := g.Node(created[0].NodeID)
	wantCharacter := map[string]any{
		"name": "林川", "look": "童年", "description": "勇敢的少年", "extras": []any{},
		"voice": nil, "voicePrompt": "明亮少年声",
	}
	if n.Kind() != "character" || !reflect.DeepEqual(n.Data()["character"], wantCharacter) {
		t.Fatalf("node=%+v", n)
	}
	if _, ok := n.Data()["characterId"]; ok {
		t.Fatalf("角色节点不应再带 characterId: %+v", n.Data())
	}
	if len(bc.messages) != 1 || bc.channels[0] != "user:1" || bc.messages[0].Type != ws.TypeCanvasPatch {
		t.Fatalf("messages=%+v", bc.messages)
	}
	patch := bc.messages[0].Data.(map[string]any)
	if patch["canvas_id"] != idcodec.ID(7) || patch["revision_before"] != uint64(1) || patch["revision_after"] != uint64(2) {
		t.Fatalf("patch=%+v", patch)
	}
	if _, ok := patch["run_id"]; ok {
		t.Fatal("public write fabricated agent run")
	}
	changes := patch["changes"].([]canvasgraph.Change)
	if !reflect.DeepEqual(changes[0].After["data.character"], wantCharacter) {
		t.Fatalf("patch 缺少角色元数据: %+v", changes)
	}
	var checkpoint map[string]any
	if err := json.Unmarshal(f.jobs[9].Output, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint["canvas_committed"] != true || checkpoint["cost"] != float64(5) || checkpoint["result"] == nil {
		t.Fatalf("checkpoint lost model result or billing cost: %+v", checkpoint)
	}
	replayed, err := svc.ApplyBreakdown(ctx, 1, 3, 7, 9, []int{1}, dramaCanvasAssets(1))
	if err != nil || !reflect.DeepEqual(replayed, created) || f.canvas.Revision != 2 || len(bc.messages) != 1 {
		t.Fatalf("durable replay changed canvas: replay=%+v err=%v", replayed, err)
	}
	if err := svc.ApplyPrompts(ctx, 1, 7, 10, []drama.AssetPrompt{{NodeID: created[0].NodeID, Prompt: "原有资产提示词"}}); err != nil {
		t.Fatal(err)
	}
	assets := dramaCanvasAssets(2)
	assets[0].Name = "小川"
	assets[0].Aliases = []string{" 林川 "}
	assets = append(assets, drama.Asset{Type: "prop", Name: "玉佩", Aliases: []string{}, Episodes: []int{2}, Description: "双鱼玉佩"})
	second, err := svc.ApplyBreakdown(ctx, 1, 3, 7, 11, []int{2}, assets)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].Asset.Type != "prop" || len(dramaGraph(t, f).Nodes) != 3 {
		t.Fatalf("second=%+v", second)
	}
	n = dramaGraph(t, f).Node(created[0].NodeID)
	if n.Data()["params"].(map[string]any)["prompt"] != "原有资产提示词" {
		t.Fatal("merge overwrote prompt")
	}
	b, _ := json.Marshal(n.Data()["drama"])
	var meta struct {
		Episodes []int `json:"episodes"`
	}
	_ = json.Unmarshal(b, &meta)
	if !reflect.DeepEqual(meta.Episodes, []int{1, 2}) {
		t.Fatalf("episodes=%v", meta.Episodes)
	}
}

func TestDramaCanvasCASReplaysWithoutDuplicateNodes(t *testing.T) {
	f := newDramaCanvasFake()
	bc := &dramaCanvasBroadcast{}
	f.beforeUpdate = func(f *dramaCanvasFake) {
		f.canvas.PayloadJSON = datatypes.JSON(`{"nodes":[{"id":"manual","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"用户新增"}}],"edges":[]}`)
		f.canvas.Revision++
	}
	created, err := service.NewDramaCanvasService(f, bc).ApplyBreakdown(context.Background(), 1, 3, 7, 9, []int{1}, dramaCanvasAssets(1))
	if err != nil {
		t.Fatal(err)
	}
	if f.reads != 2 || f.transactions != 2 || len(created) != 2 || f.canvas.Revision != 3 || dramaGraph(t, f).Node("manual") == nil || len(bc.messages) != 1 {
		t.Fatalf("reads=%d tx=%d created=%+v canvas=%+v broadcasts=%d", f.reads, f.transactions, created, f.canvas, len(bc.messages))
	}
}

func TestDramaCanvasFailureRollsBackEverything(t *testing.T) {
	boom := errors.New("database unavailable")
	cases := []struct {
		name          string
		setup         func(*dramaCanvasFake)
		user, dramaID uint64
		episodes      []int
		code          int
		raw           error
	}{
		{name: "foreign canvas", user: 2, code: 30001},
		{name: "foreign drama", dramaID: 4, code: 67001},
		{name: "missing episodes", episodes: []int{9}, code: 67006},
		{name: "empty selection", episodes: []int{}, code: 67006},
		{name: "not ready", setup: func(f *dramaCanvasFake) { ep := f.episodes[1]; ep.ScriptStatus = "pending"; f.episodes[1] = ep }, code: 67006},
		{name: "locked", setup: func(f *dramaCanvasFake) {
			ep := f.episodes[1]
			now := time.Now()
			ep.LockedAt = &now
			f.episodes[1] = ep
		}, code: 67006},
		{name: "corrupt payload", setup: func(f *dramaCanvasFake) { f.canvas.PayloadJSON = datatypes.JSON(`[]`) }, code: 30003},
		{name: "read failure", setup: func(f *dramaCanvasFake) { f.getErr = boom }, raw: boom},
		{name: "episode read failure", setup: func(f *dramaCanvasFake) { f.episodeErr = boom }, raw: boom},
		{name: "revision check failure", setup: func(f *dramaCanvasFake) { f.checkErr = boom }, raw: boom},
		{name: "canvas failure", setup: func(f *dramaCanvasFake) { f.updateErr = boom }, raw: boom},
		{name: "lock failure after canvas", setup: func(f *dramaCanvasFake) { f.lockErr = boom }, raw: boom},
		{name: "job read failure", setup: func(f *dramaCanvasFake) { f.jobErr = boom }, raw: boom},
		{name: "missing job", setup: func(f *dramaCanvasFake) { delete(f.jobs, 9) }, code: 67006},
		{name: "canceled job", setup: func(f *dramaCanvasFake) { j := f.jobs[9]; j.Status = "canceled"; f.jobs[9] = j }, code: 67006},
		{name: "wrong step", setup: func(f *dramaCanvasFake) { j := f.jobs[9]; j.Step = "scripts"; f.jobs[9] = j }, code: 67006},
		{name: "checkpoint failure after lock", setup: func(f *dramaCanvasFake) { f.markerErr = boom }, raw: boom},
		{name: "conflicts exhausted", setup: func(f *dramaCanvasFake) { f.updateErr = repository.ErrRevisionConflict }, code: 30002},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newDramaCanvasFake()
			if tc.setup != nil {
				tc.setup(f)
			}
			before := string(f.canvas.PayloadJSON)
			bc := &dramaCanvasBroadcast{}
			user, dramaID := tc.user, tc.dramaID
			if user == 0 {
				user = 1
			}
			if dramaID == 0 {
				dramaID = 3
			}
			episodes := tc.episodes
			if episodes == nil {
				episodes = []int{1}
			}
			_, err := service.NewDramaCanvasService(f, bc).ApplyBreakdown(context.Background(), user, dramaID, 7, 9, episodes, dramaCanvasAssets(1))
			if tc.raw != nil {
				if !errors.Is(err, tc.raw) {
					t.Fatalf("err=%v", err)
				}
			} else {
				var business *errcode.Error
				if !errors.As(err, &business) || business.Code != tc.code {
					t.Fatalf("err=%v want code=%d", err, tc.code)
				}
			}
			if f.canvas.Revision != 1 || string(f.canvas.PayloadJSON) != before || len(bc.messages) != 0 {
				t.Fatalf("partial write leaked: %+v", f)
			}
			if tc.name != "locked" && f.episodes[1].LockedAt != nil {
				t.Fatal("episode lock leaked")
			}
			if tc.name == "conflicts exhausted" && f.transactions != 3 {
				t.Fatalf("attempts=%d", f.transactions)
			}
		})
	}
}

func TestDramaCanvasPromptsRejectUnclassifiedAndPreserveParams(t *testing.T) {
	f := newDramaCanvasFake()
	f.canvas.PayloadJSON = datatypes.JSON(`{"nodes":[{"id":"scene","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","params":{"ratio":"16:9"},"drama":{"type":"scene","name":"街道","look":"","episodes":[]}}},{"id":"manual","type":"canvas","data":{"kind":"image"}}],"edges":[]}`)
	bc := &dramaCanvasBroadcast{}
	svc := service.NewDramaCanvasService(f, bc)
	ctx := context.Background()
	if err := svc.ApplyPrompts(ctx, 1, 7, 10, []drama.AssetPrompt{{NodeID: "scene", Prompt: "雨夜街道"}}); err != nil {
		t.Fatal(err)
	}
	params := dramaGraph(t, f).Node("scene").Data()["params"].(map[string]any)
	if params["ratio"] != "16:9" || params["prompt"] != "雨夜街道" {
		t.Fatalf("params=%+v", params)
	}
	for _, id := range []string{"manual", "missing"} {
		err := svc.ApplyPrompts(ctx, 1, 7, 12, []drama.AssetPrompt{{NodeID: id, Prompt: "不可写"}})
		var business *errcode.Error
		if !errors.As(err, &business) || business.Code != 67008 {
			t.Fatalf("id=%s err=%v", id, err)
		}
	}
	if f.canvas.Revision != 2 || len(bc.messages) != 1 {
		t.Fatalf("rev=%d broadcasts=%d", f.canvas.Revision, len(bc.messages))
	}
}

func (t *dramaCanvasFakeTx) JobForUpdate(_ context.Context, userID, canvasID, jobID uint64) (*model.DramaJob, error) {
	if t.parent.jobErr != nil {
		return nil, t.parent.jobErr
	}
	job, ok := t.jobs[jobID]
	if !ok || userID != 1 || canvasID != 7 || job.UserID != userID {
		return nil, repository.ErrNotFound
	}
	return &job, nil
}

func (t *dramaCanvasFakeTx) MarkCanvasCommitted(_ context.Context, userID, jobID uint64, marker []byte) error {
	if t.parent.markerErr != nil {
		return t.parent.markerErr
	}
	job, ok := t.jobs[jobID]
	if !ok || job.UserID != userID {
		return repository.ErrNotFound
	}
	output := map[string]any{}
	if len(job.Output) > 0 {
		if err := json.Unmarshal(job.Output, &output); err != nil {
			return err
		}
	}
	var update map[string]any
	if err := json.Unmarshal(marker, &update); err != nil {
		return err
	}
	for key, value := range update {
		output[key] = value
	}
	raw, err := json.Marshal(output)
	if err != nil {
		return err
	}
	job.Output = raw
	t.jobs[jobID] = job
	return nil
}

func TestDramaCanvasPromptsRollbackAndReplay(t *testing.T) {
	for _, failure := range []string{"canceled", "marker", "CAS", "none"} {
		t.Run(failure, func(t *testing.T) {
			f := newDramaCanvasFake()
			f.canvas.PayloadJSON = datatypes.JSON(`{"nodes":[{"id":"scene","type":"canvas","data":{"kind":"image","drama":{"type":"scene","name":"街道","look":"","episodes":[]}}}],"edges":[]}`)
			before := string(f.canvas.PayloadJSON)
			bc := &dramaCanvasBroadcast{}
			svc := service.NewDramaCanvasService(f, bc)
			if failure == "canceled" {
				j := f.jobs[10]
				j.Status = "canceled"
				f.jobs[10] = j
			}
			if failure == "marker" {
				f.markerErr = errors.New("checkpoint unavailable")
			}
			if failure == "CAS" {
				f.updateErr = repository.ErrRevisionConflict
			}
			prompts := []drama.AssetPrompt{{NodeID: "scene", Prompt: "夜色街道"}}
			err := svc.ApplyPrompts(context.Background(), 1, 7, 10, prompts)
			if failure != "none" {
				if err == nil || f.canvas.Revision != 1 || string(f.canvas.PayloadJSON) != before || len(bc.messages) != 0 {
					t.Fatalf("failed prompt transaction leaked: err=%v canvas=%+v", err, f.canvas)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.ApplyPrompts(context.Background(), 1, 7, 10, prompts); err != nil {
				t.Fatal(err)
			}
			if f.canvas.Revision != 2 || len(bc.messages) != 1 {
				t.Fatal("committed prompt replay changed canvas twice")
			}
			var checkpoint map[string]any
			_ = json.Unmarshal(f.jobs[10].Output, &checkpoint)
			if checkpoint["canvas_committed"] != true || checkpoint["cost"] != float64(3) || checkpoint["result"] == nil {
				t.Fatalf("checkpoint=%+v", checkpoint)
			}
		})
	}
}
