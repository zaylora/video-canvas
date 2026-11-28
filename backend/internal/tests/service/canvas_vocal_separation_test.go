package service_test

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

func TestCanvasVocalSeparationBindsSourceAndPreservesHistory(t *testing.T) {
	env := newGTIEnv(t, &fakeLimits{initial: 50, defMax: 4})
	ctx := t.Context()
	if err := env.db.AutoMigrate(&model.User{}, &model.Asset{}, &model.CanvasViewer{}, &model.StoryboardSplitJob{}, &model.CanvasCollaborationRecord{}, &model.CanvasCollaborationReceipt{}, &model.CanvasCollaborationEvent{}, &model.CanvasCollaborationCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	req := validCreateReq()
	canvasID := uint64(req.CanvasID)
	for _, id := range []uint64{9007199254740993, 9007199254740994} {
		asset := &model.Asset{ID: id, UserID: env.user, Kind: model.KindAudio, StorageKey: "voice/" + strconv.FormatUint(id, 10) + ".wav", MimeType: "audio/wav", Source: "upload"}
		env.assets.items[id] = asset
		if err := env.db.Create(asset).Error; err != nil {
			t.Fatal(err)
		}
	}
	payload := json.RawMessage(`{"nodes":[{"id":"n1","type":"canvas","data":{"kind":"audio","model":"m1","status":"done","assetId":"9007199254740993","src":"/files/original.wav","mediaType":"audio","uploaded":true}}],"edges":[]}`)
	if err := env.db.Model(&model.CanvasProject{}).Where("id = ?", canvasID).Update("payload_json", payload).Error; err != nil {
		t.Fatal(err)
	}
	canvas := repository.NewCanvasCollaborationRepository(env.db)
	project, err := canvas.SetActive(ctx, env.user, canvasID, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	env.registry.snap.Model.Kind = model.KindAudio
	env.registry.snap.Model.Capabilities = modelcfg.Capabilities{AudioOperation: modelcfg.AudioOperationVocalSeparation,
		Refs: modelcfg.Refs{Audio: modelcfg.RefSpec{On: true, Max: 1, MaxMB: 50}}}
	req.Kind, req.NodeID = model.KindAudio, "n1"
	req.Collaboration = &model.CanvasGenerationBinding{Epoch: project.CollaborationEpoch, Revision: project.Revision}
	submit := func(source string) (*model.GenerationTaskView, error) {
		req.Input = map[string]any{"audios": []any{source}}
		input, _ := json.Marshal(map[string]any{"model": req.ModelID, "input": req.Input})
		confirmation, err := canvas.Commit(ctx, env.user, canvasID, repository.CanvasCollaborationBatch{
			OpID: source, Epoch: project.CollaborationEpoch, BaseRevision: req.Collaboration.Revision,
			Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: req.NodeID, Fields: map[string]json.RawMessage{"data.generationInput": input}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		req.Collaboration.Revision = confirmation.Revision
		return createSingle(ctx, env.svc, env.user, source, req)
	}
	_, err = submit("9007199254740994")
	assertTaskCode(t, err, errcode.ErrCanvasConflict.Code)
	task, err := submit("9007199254740993")
	if err != nil {
		t.Fatal(err)
	}
	completeCollaborationOutputs(t, env, task.ID, []model.TaskOutput{
		{AssetID: 501, URL: "/files/vocals.wav", MediaType: model.KindAudio, Stem: "vocals"},
		{AssetID: 502, URL: "/files/accompaniment.wav", MediaType: model.KindAudio, Stem: "accompaniment"},
	})
	confirmed, _, err := canvas.Snapshot(ctx, env.user, canvasID)
	if err != nil || !bytes.Contains(confirmed.PayloadJSON, []byte(`"assetId":"9007199254740993"`)) ||
		!bytes.Contains(confirmed.PayloadJSON, []byte(`"stem":"vocals"`)) || !bytes.Contains(confirmed.PayloadJSON, []byte(`"stem":"accompaniment"`)) {
		t.Fatalf("协作历史必须保留原始音频和两个分离产物: %+v %v", confirmed, err)
	}
}
