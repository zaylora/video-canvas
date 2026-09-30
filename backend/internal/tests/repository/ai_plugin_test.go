package repository_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// pluginTestDB 为本用例创建独立 schema，并迁移插件、渠道、任务相关的表（引用统计要用到）。
func pluginTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return isolatedDB(t, &model.AIPlugin{}, &model.AIPluginVersion{}, &model.AIChannel{},
		&model.AIAuditLog{}, &model.GenerationTask{})
}

// plPlugin 构造一个待登记的插件。
func plPlugin(key string) *model.AIPlugin {
	return &model.AIPlugin{Key: key, Name: key + "-name", Source: model.PluginSourceUploaded}
}

// plVersion 构造一个待登记的版本（带 code 与 meta）。
func plVersion(key, version string) *model.AIPluginVersion {
	return &model.AIPluginVersion{
		PluginKey: key, Version: version, SHA256: "sha-" + key + "-" + version,
		Code: "export default {}", MetaJSON: model.JSONText(`{"name":"n"}`), CreatedBy: 9,
	}
}

// plSave 登记一个版本并返回其 id。
func plSave(t *testing.T, r *AIPluginRepository, key, version string) uint64 {
	t.Helper()
	v := plVersion(key, version)
	if err := r.SaveVersion(context.Background(), plPlugin(key), v); err != nil {
		t.Fatalf("登记版本失败：%v", err)
	}
	return v.ID
}

// plChannel 构造一个指向某版本的渠道。
func plChannel(key, pluginKey string, versionID uint64) *model.AIChannel {
	return &model.AIChannel{Key: key, Name: key, PluginKey: pluginKey, PluginVersionID: versionID, BaseURL: "https://up.example.com"}
}

// plTask 插入一个快照冻结了 versionID 的任务。
func plTask(t *testing.T, db *gorm.DB, status string, versionID uint64) *model.GenerationTask {
	t.Helper()
	snap := `{"channel":{"key":"c1","plugin_version_id":` + strconv.FormatUint(versionID, 10) + `}}`
	tk := gtTask(gtNewUserID(), status, 1, time.Now())
	tk.ConfigSnapshot = datatypes.JSON(snap)
	if ok, err := NewGenerationTaskRepository(db).InsertTask(context.Background(), tk); err != nil || !ok {
		t.Fatalf("插入任务失败：ok=%v err=%v", ok, err)
	}
	return tk
}

func TestAIPluginRepository_SaveVersion(t *testing.T) {
	ctx := context.Background()
	r := NewAIPluginRepository(pluginTestDB(t))

	t.Run("首次登记创建插件行并回填版本 id", func(t *testing.T) {
		v := plVersion("p-a", "1.0.0")
		if err := r.SaveVersion(ctx, plPlugin("p-a"), v); err != nil {
			t.Fatal(err)
		}
		if v.ID == 0 || v.CreatedAt.IsZero() {
			t.Fatalf("版本 id / created_at 未回填：%+v", v)
		}
		p, err := r.GetPlugin(ctx, "p-a")
		if err != nil || p.Name != "p-a-name" || p.Source != model.PluginSourceUploaded || !p.Enabled {
			t.Fatalf("插件行不符合预期：%+v %v", p, err)
		}
	})

	t.Run("再登记新版本只更新 name，不改 source 与 enabled", func(t *testing.T) {
		if err := r.SetPluginEnabled(ctx, "p-a", false); err != nil {
			t.Fatal(err)
		}
		before, _ := r.GetPlugin(ctx, "p-a")
		time.Sleep(5 * time.Millisecond) // 制造 updated_at 的先后差异
		p := &model.AIPlugin{Key: "p-a", Name: "新名字", Source: model.PluginSourceBuiltin}
		if err := r.SaveVersion(ctx, p, plVersion("p-a", "1.1.0")); err != nil {
			t.Fatal(err)
		}
		got, _ := r.GetPlugin(ctx, "p-a")
		if got.Name != "新名字" || got.Source != model.PluginSourceUploaded || got.Enabled || !got.UpdatedAt.After(before.UpdatedAt) {
			t.Fatalf("插件行不符合预期：%+v", got)
		}
	})

	t.Run("同一插件下版本号重复返回 ErrDuplicate 且整体回滚", func(t *testing.T) {
		err := r.SaveVersion(ctx, &model.AIPlugin{Key: "p-a", Name: "回滚后不应生效"}, plVersion("p-a", "1.0.0"))
		if !errors.Is(err, ErrDuplicate) {
			t.Fatalf("期望 ErrDuplicate，实际 %v", err)
		}
		got, _ := r.GetPlugin(ctx, "p-a")
		if got.Name != "新名字" {
			t.Fatalf("事务没有回滚，name=%s", got.Name)
		}
	})

	t.Run("不同插件可以有相同版本号", func(t *testing.T) {
		plSave(t, r, "p-b", "1.0.0")
	})

	t.Run("版本的 plugin_key 为空时取插件 key，不一致时报错", func(t *testing.T) {
		v := plVersion("", "2.0.0")
		if err := r.SaveVersion(ctx, plPlugin("p-c"), v); err != nil || v.PluginKey != "p-c" {
			t.Fatalf("应回填 plugin_key：%+v %v", v, err)
		}
		if err := r.SaveVersion(ctx, plPlugin("p-c"), plVersion("p-other", "1.0.0")); err == nil {
			t.Fatal("plugin_key 不一致应报错")
		}
	})
}

func TestAIPluginRepository_Queries(t *testing.T) {
	ctx := context.Background()
	r := NewAIPluginRepository(pluginTestDB(t))
	a1 := plSave(t, r, "qa", "1.0.0")
	a2 := plSave(t, r, "qa", "1.1.0")
	b1 := plSave(t, r, "qb", "1.0.0")

	t.Run("GetPlugin 不存在返回 ErrNotFound", func(t *testing.T) {
		if _, err := r.GetPlugin(ctx, "none"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
	})
	t.Run("ListPlugins 按 key 升序", func(t *testing.T) {
		list, err := r.ListPlugins(ctx)
		if err != nil || len(list) != 2 || list[0].Key != "qa" || list[1].Key != "qb" {
			t.Fatalf("%+v %v", list, err)
		}
	})
	t.Run("ListVersions 按 id 倒序且不含 code", func(t *testing.T) {
		list, err := r.ListVersions(ctx, "qa")
		if err != nil || len(list) != 2 || list[0].ID != a2 || list[1].ID != a1 {
			t.Fatalf("%+v %v", list, err)
		}
		for _, v := range list {
			if v.Code != "" || len(v.MetaJSON) == 0 || v.SHA256 == "" || v.CreatedBy != 9 {
				t.Fatalf("列表项不符合预期（不应含 code，应含 meta 与 sha256）：%+v", v)
			}
		}
		all, err := r.ListVersions(ctx, "")
		if err != nil || len(all) != 3 || all[0].ID != b1 {
			t.Fatalf("pluginKey 为空应返回全部版本：%+v %v", all, err)
		}
		if none, err := r.ListVersions(ctx, "none"); err != nil || len(none) != 0 {
			t.Fatalf("不存在的插件应返回空列表：%+v %v", none, err)
		}
	})
	t.Run("GetVersion 含 code，GetVersionHead 不含", func(t *testing.T) {
		full, err := r.GetVersion(ctx, a1)
		if err != nil || full.Code != "export default {}" || full.Version != "1.0.0" {
			t.Fatalf("%+v %v", full, err)
		}
		head, err := r.GetVersionHead(ctx, a1)
		if err != nil || head.Code != "" || head.SHA256 != "sha-qa-1.0.0" || len(head.MetaJSON) == 0 {
			t.Fatalf("%+v %v", head, err)
		}
	})
	t.Run("FindVersion 按插件与版本号查，不含 code", func(t *testing.T) {
		v, err := r.FindVersion(ctx, "qa", "1.1.0")
		if err != nil || v.ID != a2 || v.Code != "" {
			t.Fatalf("%+v %v", v, err)
		}
	})
	t.Run("版本不存在时三个查询都返回 ErrNotFound", func(t *testing.T) {
		if _, err := r.GetVersion(ctx, 999999); !errors.Is(err, ErrNotFound) {
			t.Fatalf("GetVersion：%v", err)
		}
		if _, err := r.GetVersionHead(ctx, 999999); !errors.Is(err, ErrNotFound) {
			t.Fatalf("GetVersionHead：%v", err)
		}
		if _, err := r.FindVersion(ctx, "qa", "9.9.9"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("FindVersion：%v", err)
		}
	})
}

func TestAIPluginRepository_SetPluginEnabled(t *testing.T) {
	ctx := context.Background()
	r := NewAIPluginRepository(pluginTestDB(t))
	plSave(t, r, "en", "1.0.0")

	t.Run("停用再启用，并更新 updated_at", func(t *testing.T) {
		before, _ := r.GetPlugin(ctx, "en")
		time.Sleep(5 * time.Millisecond) // 制造 updated_at 的先后差异
		if err := r.SetPluginEnabled(ctx, "en", false); err != nil {
			t.Fatal(err)
		}
		p, _ := r.GetPlugin(ctx, "en")
		if p.Enabled || !p.UpdatedAt.After(before.UpdatedAt) {
			t.Fatalf("停用未生效：%+v", p)
		}
		if err := r.SetPluginEnabled(ctx, "en", true); err != nil {
			t.Fatal(err)
		}
		if p, _ = r.GetPlugin(ctx, "en"); !p.Enabled {
			t.Fatal("启用未生效")
		}
	})
	t.Run("插件不存在返回 ErrNotFound", func(t *testing.T) {
		if err := r.SetPluginEnabled(ctx, "none", true); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
	})
}

func TestAIPluginRepository_CountVersionRefs(t *testing.T) {
	ctx := context.Background()
	db := pluginTestDB(t)
	r := NewAIPluginRepository(db)
	ch := NewAIChannelRepository(db)
	v1 := plSave(t, r, "rf", "1.0.0")
	v2 := plSave(t, r, "rf", "2.0.0")

	t.Run("没有引用时全为 0", func(t *testing.T) {
		refs, err := r.CountVersionRefs(ctx, v1)
		if err != nil || refs.Channels != 0 || refs.ActiveTasks != 0 {
			t.Fatalf("%+v %v", refs, err)
		}
	})

	if err := ch.CreateChannel(ctx, plChannel("rf-c1", "rf", v1)); err != nil {
		t.Fatal(err)
	}
	if err := ch.CreateChannel(ctx, plChannel("rf-c2", "rf", v1)); err != nil {
		t.Fatal(err)
	}
	plTask(t, db, model.TaskPending, v1)
	plTask(t, db, model.TaskRunning, v1)
	plTask(t, db, model.TaskSucceeded, v1) // 终态任务不计入
	plTask(t, db, model.TaskFailed, v1)
	plTask(t, db, model.TaskRunning, v2) // 其它版本的任务不计入

	t.Run("统计渠道与非终态任务，终态任务与其它版本不计入", func(t *testing.T) {
		refs, err := r.CountVersionRefs(ctx, v1)
		if err != nil || refs.Channels != 2 || refs.ActiveTasks != 2 {
			t.Fatalf("%+v %v", refs, err)
		}
		refs, err = r.CountVersionRefs(ctx, v2)
		if err != nil || refs.Channels != 0 || refs.ActiveTasks != 1 {
			t.Fatalf("%+v %v", refs, err)
		}
	})
	t.Run("快照缺少 channel 字段或格式异常的任务不会让统计报错", func(t *testing.T) {
		for _, snap := range []string{`{}`, `{"channel":{"plugin_version_id":"abc"}}`, `{"channel":null}`} {
			tk := gtTask(gtNewUserID(), model.TaskRunning, 1, time.Now())
			tk.ConfigSnapshot = datatypes.JSON(snap)
			if _, err := NewGenerationTaskRepository(db).InsertTask(ctx, tk); err != nil {
				t.Fatal(err)
			}
		}
		if refs, err := r.CountVersionRefs(ctx, v1); err != nil || refs.ActiveTasks != 2 {
			t.Fatalf("%+v %v", refs, err)
		}
	})
	t.Run("版本不存在返回 ErrNotFound", func(t *testing.T) {
		if _, err := r.CountVersionRefs(ctx, 999999); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
	})
}

func TestAIPluginRepository_DeleteVersion(t *testing.T) {
	ctx := context.Background()
	db := pluginTestDB(t)
	r := NewAIPluginRepository(db)
	ch := NewAIChannelRepository(db)

	t.Run("被渠道引用返回 ErrInUse，版本仍在", func(t *testing.T) {
		id := plSave(t, r, "d1", "1.0.0")
		if err := ch.CreateChannel(ctx, plChannel("d1-c", "d1", id)); err != nil {
			t.Fatal(err)
		}
		if err := r.DeleteVersion(ctx, id); !errors.Is(err, ErrInUse) {
			t.Fatalf("期望 ErrInUse，实际 %v", err)
		}
		if _, err := r.GetVersionHead(ctx, id); err != nil {
			t.Fatalf("版本不应被删除：%v", err)
		}
	})

	t.Run("被非终态任务引用返回 ErrInUse", func(t *testing.T) {
		id := plSave(t, r, "d2", "1.0.0")
		plTask(t, db, model.TaskFinalizing, id)
		if err := r.DeleteVersion(ctx, id); !errors.Is(err, ErrInUse) {
			t.Fatalf("期望 ErrInUse，实际 %v", err)
		}
	})

	t.Run("只被终态任务引用可以删除", func(t *testing.T) {
		id := plSave(t, r, "d3", "1.0.0")
		plTask(t, db, model.TaskSucceeded, id)
		plTask(t, db, model.TaskExpired, id)
		if err := r.DeleteVersion(ctx, id); err != nil {
			t.Fatalf("终态任务不应阻塞删除：%v", err)
		}
	})

	t.Run("删除非最后一个版本保留插件，删除最后一个版本顺带删插件", func(t *testing.T) {
		v1 := plSave(t, r, "d4", "1.0.0")
		v2 := plSave(t, r, "d4", "2.0.0")
		if err := r.DeleteVersion(ctx, v1); err != nil {
			t.Fatal(err)
		}
		if _, err := r.GetPlugin(ctx, "d4"); err != nil {
			t.Fatalf("还有版本时插件行应保留：%v", err)
		}
		if err := r.DeleteVersion(ctx, v2); err != nil {
			t.Fatal(err)
		}
		if _, err := r.GetPlugin(ctx, "d4"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("最后一个版本删除后插件行应一并删除：%v", err)
		}
	})

	t.Run("删除后同一版本号可以重新登记", func(t *testing.T) {
		id := plSave(t, r, "d5", "1.0.0")
		if err := r.DeleteVersion(ctx, id); err != nil {
			t.Fatal(err)
		}
		plSave(t, r, "d5", "1.0.0")
	})

	t.Run("版本不存在返回 ErrNotFound", func(t *testing.T) {
		if err := r.DeleteVersion(ctx, 999999); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
	})
}

// 并发删除同一插件的最后两个版本：两个事务串行后，插件行必须被删掉，不能留下没有版本的空插件。
func TestAIPluginRepository_DeleteVersion_ConcurrentLastTwo(t *testing.T) {
	ctx := context.Background()
	r := NewAIPluginRepository(pluginTestDB(t))
	v1 := plSave(t, r, "cc", "1.0.0")
	v2 := plSave(t, r, "cc", "2.0.0")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range []uint64{v1, v2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = r.DeleteVersion(ctx, id)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("并发删除 %d 失败：%v", i, err)
		}
	}
	if _, err := r.GetPlugin(ctx, "cc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("插件行应被删除，实际 %v", err)
	}
}

// 删除版本与渠道指向该版本并发：要么删除成功且渠道创建失败，要么渠道创建成功且删除返回 ErrInUse，不能出现悬空引用。
func TestAIPluginRepository_DeleteVersion_RaceWithChannel(t *testing.T) {
	ctx := context.Background()
	db := pluginTestDB(t)
	r := NewAIPluginRepository(db)
	ch := NewAIChannelRepository(db)

	for i := range 10 {
		key := "race" + strconv.Itoa(i)
		id := plSave(t, r, key, "1.0.0")
		var delErr, chErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); delErr = r.DeleteVersion(ctx, id) }()
		go func() { defer wg.Done(); chErr = ch.CreateChannel(ctx, plChannel(key+"-c", key, id)) }()
		wg.Wait()

		switch {
		case delErr == nil && errors.Is(chErr, ErrNotFound):
		case errors.Is(delErr, ErrInUse) && chErr == nil:
		default:
			t.Fatalf("第 %d 轮出现悬空引用或意外错误：delete=%v create=%v", i, delErr, chErr)
		}
	}
}
