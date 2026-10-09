package repository_test

import (
	"context"
	"strings"
	"testing"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// 模型取消版本管理后，老库里的多版本数据要收敛成每个模型一份配置：有草稿以最新草稿为准，没有草稿用已发布版本。
func TestMigrateModelSingleConfig(t *testing.T) {
	ctx := context.Background()
	db := isolatedDB(t, &model.AIModel{}, &model.AIConfigRevision{})
	seed := []string{
		// both：已发布 v1（已归档 v1、发布 v2）+ 更新的草稿 v3，已启用
		`INSERT INTO ai_models (key, kind, enabled, sort, published_revision_id) VALUES ('both', 'video', true, 10, 2)`,
		`INSERT INTO ai_config_revisions (id, target, target_key, revision_no, body_json, status, created_at) VALUES
			(1, 'model', 'both', 1, '{"v":1}', 'archived', now()),
			(2, 'model', 'both', 2, '{"v":2}', 'published', now()),
			(3, 'model', 'both', 3, '{"v":3}', 'draft', now())`,
		// pubonly：只有已发布版本
		`INSERT INTO ai_models (key, kind, enabled, sort, published_revision_id) VALUES ('pubonly', 'image', true, 20, 5)`,
		`INSERT INTO ai_config_revisions (id, target, target_key, revision_no, body_json, status, created_at) VALUES
			(4, 'model', 'pubonly', 1, '{"v":1}', 'archived', now()),
			(5, 'model', 'pubonly', 2, '{"v":2}', 'published', now())`,
		// draftonly：从没发布过，只有草稿
		`INSERT INTO ai_models (key, kind, enabled, sort) VALUES ('draftonly', 'audio', false, 30)`,
		`INSERT INTO ai_config_revisions (id, target, target_key, revision_no, body_json, status, created_at) VALUES
			(6, 'model', 'draftonly', 1, '{"v":1}', 'archived', now()),
			(7, 'model', 'draftonly', 2, '{"v":2}', 'draft', now())`,
	}
	for _, s := range seed {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("准备数据失败（%s）：%v", s, err)
		}
	}

	for i := range 2 { // 幂等
		if err := MigrateModelSingleConfig(db); err != nil {
			t.Fatalf("第 %d 次执行失败：%v", i+1, err)
		}
	}

	r := NewAIConfigRepository(db)
	want := map[string]string{"both": `{"v":3}`, "pubonly": `{"v":2}`, "draftonly": `{"v":2}`}
	for key, body := range want {
		rev, err := r.GetModelConfig(ctx, key)
		if err != nil {
			t.Fatalf("%s 应有一份配置：%v", key, err)
		}
		if strings.ReplaceAll(string(rev.BodyJSON), " ", "") != body {
			t.Fatalf("%s 的配置不符合预期：%s", key, rev.BodyJSON)
		}
		if rev.RevisionNo != 1 || rev.Status != model.RevisionPublished {
			t.Fatalf("%s 的配置应为 revision_no=1 / published：%+v", key, rev)
		}
		var n int64
		db.Model(&model.AIConfigRevision{}).Where("target_key = ?", key).Count(&n)
		if n != 1 {
			t.Fatalf("%s 应只剩一行：%d", key, n)
		}
	}
	// 上线状态与排序不动
	if p, _ := r.GetModelPointer(ctx, "both"); !p.Enabled || p.Sort != 10 {
		t.Fatalf("启用状态与排序不应改变：%+v", p)
	}
	if p, _ := r.GetModelPointer(ctx, "draftonly"); p.Enabled {
		t.Fatalf("没发布过的模型不应被启用：%+v", p)
	}
}
