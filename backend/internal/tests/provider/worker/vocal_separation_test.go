package worker_test

import (
	"encoding/json"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

func TestVocalSeparationRequiresTwoDistinctAudioStems(t *testing.T) {
	vocals := provider.Output{Type: provider.OutputAsset, AssetID: 501, AssetURL: "/files/501", Stem: "vocals"}
	music := provider.Output{Type: provider.OutputAsset, AssetID: 502, AssetURL: "/files/502", Stem: "accompaniment"}
	duplicate, wrongMedia, repeatedStem := music, music, music
	duplicate.AssetID, duplicate.AssetURL = vocals.AssetID, vocals.AssetURL
	wrongMedia.MediaType = model.KindVideo
	repeatedStem.Stem = vocals.Stem
	for _, scenario := range []struct {
		name    string
		outputs []provider.Output
		valid   bool
	}{
		{"two-stems", []provider.Output{music, vocals}, true},
		{"single-stem", []provider.Output{vocals}, false},
		{"same-asset", []provider.Output{vocals, duplicate}, false},
		{"same-role", []provider.Output{vocals, repeatedStem}, false},
		{"video-instead-of-audio", []provider.Output{vocals, wrongMedia}, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			env := newWkEnv(nil)
			env.task(1, model.TaskFinalizing, func(task *model.GenerationTask) {
				task.Kind = model.KindAudio
				snapshot := provider.Snapshot{Model: provider.ModelSnapshot{Kind: model.KindAudio}}
				snapshot.Model.Capabilities.AudioOperation = modelcfg.AudioOperationVocalSeparation
				task.ConfigSnapshot, _ = json.Marshal(snapshot)
				task.ProviderResult, _ = json.Marshal(scenario.outputs)
			})
			env.runOnce(t)
			outputs := env.store.completedOf(1)
			status := env.store.get(1).Status
			if scenario.valid {
				if status != model.TaskSucceeded || len(outputs) != 2 || outputs[0].Stem != music.Stem || outputs[1].Stem != vocals.Stem {
					t.Fatalf("两条音轨必须原样结算: %s %+v", status, outputs)
				}
			} else if status != model.TaskFailed || len(outputs) != 0 || env.saver.saveCount() != 0 {
				t.Fatalf("无效音轨应在转存前失败: %s %+v", status, outputs)
			}
		})
	}
}
