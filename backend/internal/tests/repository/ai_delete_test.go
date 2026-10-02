package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// 本文件测试删除相关的仓储方法：模型硬删除、渠道 / 插件的引用统计与删除。

// delTestDB 迁移删除涉及的全部表（模型、revision、凭证、插件、渠道、任务）。
func delTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return isolatedDB(t, &model.AIModel{}, &model.AIConfigRevision{}, &model.AISecret{},
		&model.AIPlugin{}, &model.AIPluginVersion{}, &model.AIChannel{}, &model.AIAuditLog{}, &model.GenerationTask{})
}

// delModelBody 生成一份引用渠道 channel 的模型正文。
func delModelBody(key, label, channel string) string {
	return `{"key":"` + key + `","label":"` + label + `","channels":[{"channel":"` + channel + `","upstream_model":"x"}]}`
}

// delPublish 保存并发布一个模型版本。
func delPublish(t *testing.T, r *AIConfigRepository, key, body string) {
	t.Helper()
	rev := aicRepoSaveModel(t, r, key, body)
	ptr := ConfigPointer{Target: model.ConfigTargetModel, Key: key, Kind: "video"}
	if _, err := r.PublishDraft(context.Background(), ptr, rev.ID); err != nil {
		t.Fatalf("发布失败：%v", err)
	}
}

// delChannelTask 插入一个快照冻结了渠道 channelKey 的任务。
func delChannelTask(t *testing.T, db *gorm.DB, status, channelKey string) {
	t.Helper()
	tk := gtTask(gtNewUserID(), status, 1, time.Now())
	tk.ConfigSnapshot = datatypes.JSON(`{"channel":{"key":"` + channelKey + `","plugin_version_id":1}}`)
	if ok, err := NewGenerationTaskRepository(db).InsertTask(context.Background(), tk); err != nil || !ok {
		t.Fatalf("插入任务失败：ok=%v err=%v", ok, err)
	}
}

func TestAIConfigRepository_DeleteModel(t *testing.T) {
	ctx := context.Background()
	db := delTestDB(t)
	r := NewAIConfigRepository(db)

	t.Run("上架中返回 ErrInUse，不存在返回 ErrNotFound", func(t *testing.T) {
		delPublish(t, r, "m-on", `{"a":1}`)
		if err := r.SetModelEnabled(ctx, "m-on", true); err != nil {
			t.Fatal(err)
		}
		if err := r.DeleteModel(ctx, "m-on"); !errors.Is(err, ErrInUse) {
			t.Fatalf("上架中应返回 ErrInUse：%v", err)
		}
		if err := r.DeleteModel(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("不存在应返回 ErrNotFound：%v", err)
		}
	})

	t.Run("硬删除指针行与全部 revision，其他模型不受影响；同名 key 可重新新建", func(t *testing.T) {
		delPublish(t, r, "m-del", `{"a":1}`)
		aicRepoSaveModel(t, r, "m-del", `{"a":2}`)
		aicRepoSaveModel(t, r, "m-keep", `{"b":1}`)
		if err := r.DeleteModel(ctx, "m-del"); err != nil {
			t.Fatal(err)
		}
		if _, err := r.GetModelPointer(ctx, "m-del"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("删除后 GetModelPointer 应返回 ErrNotFound：%v", err)
		}
		var left int64
		db.Model(&model.AIConfigRevision{}).Where("target_key = ?", "m-del").Count(&left)
		if left != 0 {
			t.Fatalf("revision 应全部删除：还剩 %d", left)
		}
		pub, _ := r.LoadPublishedModels(ctx)
		for _, m := range pub {
			if m.Key == "m-del" {
				t.Fatal("Registry 加载结果里不应有已删除的模型")
			}
		}
		if revs, _ := r.ListRevisions(ctx, model.ConfigTargetModel, "m-keep", 0); len(revs) != 1 {
			t.Fatalf("其他模型的 revision 不应受影响：%d", len(revs))
		}
		rev := aicRepoSaveModel(t, r, "m-del", `{"a":3}`)
		if rev.RevisionNo != 1 {
			t.Fatalf("同名 key 重新新建时 revision_no 应从 1 开始：%d", rev.RevisionNo)
		}
		if p, err := r.GetModelPointer(ctx, "m-del"); err != nil || p.Enabled || p.PublishedRevisionID != nil {
			t.Fatalf("重新新建的指针行应是全新的：%+v %v", p, err)
		}
	})
}

func TestAIChannelRepository_DeleteChannel(t *testing.T) {
	ctx := context.Background()
	db := delTestDB(t)
	pr := NewAIPluginRepository(db)
	cr := NewAIChannelRepository(db)
	mr := NewAIConfigRepository(db)
	vid := plSave(t, pr, "p", "1.0.0")
	for _, k := range []string{"c-used", "c-free", "c-task", "c-empty"} {
		if err := cr.CreateChannel(ctx, plChannel(k, "p", vid)); err != nil {
			t.Fatal(err)
		}
	}
	// m-pub：已发布版本引用 c-used（label 取已发布的）；m-draft：只有草稿引用；
	// m-arch：只有已归档的历史版本引用（不算）；m-gone：已删除（不算）
	delPublish(t, mr, "m-pub", delModelBody("m-pub", "发布名", "c-used"))
	aicRepoSaveModel(t, mr, "m-pub", delModelBody("m-pub", "草稿名", "c-used"))
	aicRepoSaveModel(t, mr, "m-draft", delModelBody("m-draft", "", "c-used"))
	delPublish(t, mr, "m-arch", delModelBody("m-arch", "旧", "c-used"))
	delPublish(t, mr, "m-arch", delModelBody("m-arch", "新", "c-free"))
	aicRepoSaveModel(t, mr, "m-gone", delModelBody("m-gone", "x", "c-used"))
	if err := mr.DeleteModel(ctx, "m-gone"); err != nil {
		t.Fatal(err)
	}
	delChannelTask(t, db, model.TaskRunning, "c-task")
	delChannelTask(t, db, model.TaskSucceeded, "c-empty") // 终态任务不算

	t.Run("统计口径：草稿 / 已发布算，归档版本与已删除模型不算", func(t *testing.T) {
		if refs, _ := cr.CountChannelRefs(ctx, "c-free"); len(refs.Models) != 1 || refs.Models[0].Key != "m-arch" || refs.Models[0].Label != "新" {
			t.Fatalf("c-free 只应被 m-arch 的当前发布版本引用：%+v", refs)
		}
		refs, err := cr.CountChannelRefs(ctx, "c-used")
		if err != nil {
			t.Fatal(err)
		}
		if len(refs.Models) != 2 || refs.Models[0].Key != "m-draft" || refs.Models[1].Key != "m-pub" ||
			refs.Models[1].Label != "发布名" || refs.Models[0].Label != "" || refs.ActiveTasks != 0 {
			t.Fatalf("引用不符合预期：%+v", refs)
		}
		refs, _ = cr.CountChannelRefs(ctx, "c-task")
		if len(refs.Models) != 0 || refs.ActiveTasks != 1 {
			t.Fatalf("任务引用不符合预期：%+v", refs)
		}
		if _, err := cr.CountChannelRefs(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("不存在应返回 ErrNotFound：%v", err)
		}
	})

	t.Run("被引用返回 ErrInUse 与引用数，不存在返回 ErrNotFound", func(t *testing.T) {
		refs, err := cr.DeleteChannel(ctx, "c-used")
		if !errors.Is(err, ErrInUse) || len(refs.Models) != 2 {
			t.Fatalf("应返回 ErrInUse：%+v %v", refs, err)
		}
		if _, err := cr.DeleteChannel(ctx, "c-task"); !errors.Is(err, ErrInUse) {
			t.Fatalf("被进行中的任务引用应返回 ErrInUse：%v", err)
		}
		if _, err := cr.DeleteChannel(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("不存在应返回 ErrNotFound：%v", err)
		}
	})

	t.Run("删除渠道连同它的 Key，其他凭证不动", func(t *testing.T) {
		for _, name := range []string{model.ChannelSecretName("c-empty"), model.ChannelSecretName("c-used")} {
			if err := mr.UpsertSecret(ctx, &model.AISecret{Name: name, Ciphertext: []byte("c"), Nonce: []byte("n"), KeyVersion: 1}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := cr.DeleteChannel(ctx, "c-empty"); err != nil {
			t.Fatal(err)
		}
		if _, err := cr.GetChannel(ctx, "c-empty"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("渠道应已删除：%v", err)
		}
		if _, err := mr.GetSecret(ctx, model.ChannelSecretName("c-empty")); !errors.Is(err, ErrNotFound) {
			t.Fatalf("渠道 Key 应一并删除：%v", err)
		}
		if _, err := mr.GetSecret(ctx, model.ChannelSecretName("c-used")); err != nil {
			t.Fatalf("其他渠道的 Key 不应受影响：%v", err)
		}
	})
}

func TestAIPluginRepository_DeletePlugin(t *testing.T) {
	ctx := context.Background()
	db := pluginTestDB(t)
	r := NewAIPluginRepository(db)
	cr := NewAIChannelRepository(db)

	t.Run("统计全部版本的渠道与任务引用", func(t *testing.T) {
		v1 := plSave(t, r, "p-ref", "1.0.0")
		v2 := plSave(t, r, "p-ref", "1.1.0")
		if err := cr.CreateChannel(ctx, plChannel("ch-b", "p-ref", v2)); err != nil {
			t.Fatal(err)
		}
		if err := cr.CreateChannel(ctx, plChannel("ch-a", "p-ref", v1)); err != nil {
			t.Fatal(err)
		}
		plTask(t, db, model.TaskQueued, v1)
		plTask(t, db, model.TaskRunning, v2)
		plTask(t, db, model.TaskFailed, v2) // 终态不算
		refs, err := r.CountPluginRefs(ctx, "p-ref")
		if err != nil {
			t.Fatal(err)
		}
		if len(refs.Channels) != 2 || refs.Channels[0].Key != "ch-a" || refs.Channels[1].Key != "ch-b" || refs.ActiveTasks != 2 {
			t.Fatalf("引用不符合预期：%+v", refs)
		}
		got, err := r.DeletePlugin(ctx, "p-ref")
		if !errors.Is(err, ErrInUse) || len(got.Channels) != 2 || got.ActiveTasks != 2 {
			t.Fatalf("被引用应返回 ErrInUse 与引用：%+v %v", got, err)
		}
	})

	t.Run("只被进行中的任务引用同样不能删", func(t *testing.T) {
		v := plSave(t, r, "p-task", "1.0.0")
		plTask(t, db, model.TaskPending, v)
		if _, err := r.DeletePlugin(ctx, "p-task"); !errors.Is(err, ErrInUse) {
			t.Fatalf("应返回 ErrInUse：%v", err)
		}
	})

	t.Run("不存在返回 ErrNotFound", func(t *testing.T) {
		if _, err := r.CountPluginRefs(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("CountPluginRefs 应返回 ErrNotFound：%v", err)
		}
		if _, err := r.DeletePlugin(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("DeletePlugin 应返回 ErrNotFound：%v", err)
		}
	})

	t.Run("未被引用：删除全部版本与插件行，其他插件不受影响", func(t *testing.T) {
		plSave(t, r, "p-free", "1.0.0")
		plSave(t, r, "p-free", "2.0.0")
		other := plSave(t, r, "p-other", "1.0.0")
		plTask(t, db, model.TaskRunning, other)
		if _, err := r.DeletePlugin(ctx, "p-free"); err != nil {
			t.Fatal(err)
		}
		if _, err := r.GetPlugin(ctx, "p-free"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("插件行应已删除：%v", err)
		}
		if vs, _ := r.ListVersions(ctx, "p-free"); len(vs) != 0 {
			t.Fatalf("版本应全部删除：%d", len(vs))
		}
		if _, err := r.GetVersionHead(ctx, other); err != nil {
			t.Fatalf("其他插件的版本不应受影响：%v", err)
		}
	})
}
