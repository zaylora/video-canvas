package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"gorm.io/datatypes"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

func TestDramaCanvasRepositoryAtomicWrites(t *testing.T) {
	for _, scenario := range []string{"commit", "stale CAS", "late failure", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			db := isolatedDB(t, &model.CanvasProject{}, &model.DramaProject{}, &model.DramaEpisode{}, &model.DramaJob{}, &model.CharacterCard{})
			ctx := context.Background()
			canvas := model.CanvasProject{UserID: 11, Title: "短剧画布", PayloadJSON: datatypes.JSON(`{"nodes":[],"edges":[]}`)}
			if err := db.Create(&canvas).Error; err != nil {
				t.Fatal(err)
			}
			project := model.DramaProject{UserID: 11, CanvasID: canvas.ID, Title: "测试剧", SourceText: "原文", StyleKey: "anime"}
			if err := db.Create(&project).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&canvas).Update("drama_id", project.ID).Error; err != nil {
				t.Fatal(err)
			}
			episode := model.DramaEpisode{UserID: 11, DramaID: project.ID, No: 1, ScriptStatus: "ready", ScriptMD: "剧本"}
			if err := db.Create(&episode).Error; err != nil {
				t.Fatal(err)
			}
			job := model.DramaJob{UserID: 11, DramaID: project.ID, Step: "breakdown", Status: "running", Output: datatypes.JSON(`{"result":{"assets":[]},"cost":7}`)}
			if err := db.Create(&job).Error; err != nil {
				t.Fatal(err)
			}
			repo := repository.NewDramaCanvasRepository(db)
			if _, err := repo.GetCanvas(ctx, 22, canvas.ID); !errors.Is(err, repository.ErrNotFound) {
				t.Fatalf("foreign canvas err=%v", err)
			}
			if scenario == "commit" {
				svc := service.NewDramaCanvasService(repo, nil)
				assets := []drama.Asset{{Type: "character", Name: "林川", Look: "童年", Aliases: []string{}, Episodes: []int{1}, Description: "少年", VoicePrompt: "少年声"}}
				created, err := svc.ApplyBreakdown(ctx, 11, project.ID, canvas.ID, job.ID, []int{1}, assets)
				if err != nil {
					t.Fatal(err)
				}
				if len(created) != 1 {
					t.Fatalf("created=%+v", created)
				}
				var cards []model.CharacterCard
				if err := db.Find(&cards).Error; err != nil {
					t.Fatal(err)
				}
				if len(cards) != 1 || cards[0].DramaID == nil || *cards[0].DramaID != project.ID || cards[0].AvatarAssetID != nil || cards[0].LookName != "童年" || cards[0].VoicePrompt != "少年声" {
					t.Fatalf("cards=%+v", cards)
				}
				saved, err := repo.GetCanvas(ctx, 11, canvas.ID)
				if err != nil {
					t.Fatal(err)
				}
				g, err := canvasgraph.Parse(saved.PayloadJSON)
				if err != nil {
					t.Fatal(err)
				}
				if saved.Revision != canvas.Revision+1 || g.Node(created[0].NodeID).Data()["characterId"] != idcodec.Encode(cards[0].ID) {
					t.Fatalf("canvas/card mismatch: %+v", saved)
				}
				if err := db.First(&episode, episode.ID).Error; err != nil {
					t.Fatal(err)
				}
				if episode.LockedAt == nil || episode.Revision != 2 {
					t.Fatalf("episode=%+v", episode)
				}
				if err := db.First(&job, job.ID).Error; err != nil {
					t.Fatal(err)
				}
				var checkpoint map[string]any
				if err := json.Unmarshal(job.Output, &checkpoint); err != nil {
					t.Fatal(err)
				}
				if checkpoint["canvas_committed"] != true || checkpoint["cost"] != float64(7) || checkpoint["result"] == nil {
					t.Fatalf("checkpoint=%+v", checkpoint)
				}
				replayed, err := svc.ApplyBreakdown(ctx, 11, project.ID, canvas.ID, job.ID, []int{1}, assets)
				if err != nil || !reflect.DeepEqual(replayed, created) {
					t.Fatalf("replay=%+v err=%v", replayed, err)
				}
				var count int64
				if err := db.Model(&model.CharacterCard{}).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				if count != 1 {
					t.Fatalf("duplicate card after replay: %d", count)
				}
				return
			}
			if scenario == "canceled" {
				if err := db.Model(&job).Update("status", "canceled").Error; err != nil {
					t.Fatal(err)
				}
				_, err := service.NewDramaCanvasService(repo, nil).ApplyBreakdown(ctx, 11, project.ID, canvas.ID, job.ID, []int{1}, []drama.Asset{{Type: "scene", Name: "街道", Aliases: []string{}, Episodes: []int{1}, Description: "街道"}})
				var business *errcode.Error
				if !errors.As(err, &business) || business.Code != 67006 {
					t.Fatalf("canceled job wrote canvas: %v", err)
				}
			} else {
				late := errors.New("fail after all writes")
				err := repo.WithTx(ctx, func(tx repository.DramaCanvasTx) error {
					if _, err := tx.JobForUpdate(ctx, 11, canvas.ID, job.ID); err != nil {
						return err
					}
					rows, err := tx.EpisodesForUpdate(ctx, 11, project.ID, canvas.ID, []int{1})
					if err != nil {
						return err
					}
					if len(rows) != 1 {
						t.Fatal("episode not locked")
					}
					card := model.CharacterCard{UserID: 11, DramaID: &project.ID, Name: "待回滚", LookName: "童年", ExtraAssetIDs: []uint64{}}
					if err := tx.CreateCharacter(ctx, &card); err != nil {
						return err
					}
					if err := tx.LockEpisodes(ctx, 11, project.ID, []int{1}); err != nil {
						return err
					}
					if err := tx.MarkCanvasCommitted(ctx, 11, job.ID, []byte(`{"canvas_committed":true}`)); err != nil {
						return err
					}
					revision := canvas.Revision
					if scenario == "stale CAS" {
						revision++
					}
					if err := tx.UpdateCanvas(ctx, 11, canvas.ID, revision, []byte(`{"nodes":[],"edges":[],"changed":true}`)); err != nil {
						return err
					}
					return late
				})
				want := late
				if scenario == "stale CAS" {
					want = repository.ErrRevisionConflict
				}
				if !errors.Is(err, want) {
					t.Fatalf("transaction err=%v want=%v", err, want)
				}
			}
			var count int64
			if err := db.Model(&model.CharacterCard{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("orphan cards after rollback: %d", count)
			}
			saved, err := repo.GetCanvas(ctx, 11, canvas.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Revision != canvas.Revision || !jsonEqual(saved.PayloadJSON, `{"nodes":[],"edges":[]}`) {
				t.Fatalf("canvas leaked failed write: %+v", saved)
			}
			if err := db.First(&episode, episode.ID).Error; err != nil {
				t.Fatal(err)
			}
			if episode.LockedAt != nil || episode.Revision != 1 {
				t.Fatalf("episode lock leaked: %+v", episode)
			}
			if err := db.First(&job, job.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !jsonEqual(job.Output, `{"result":{"assets":[]},"cost":7}`) {
				t.Fatalf("checkpoint leaked failed transaction: %s", job.Output)
			}
		})
	}
}
