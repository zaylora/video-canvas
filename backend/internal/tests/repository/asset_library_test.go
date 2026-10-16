package repository_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// 本文件是资产库与生成历史的仓储集成测试（连专用测试库，未设置 TEST_DATABASE_DSN 时跳过）。

func TestAssetRepository_ListGeneratedByUser(t *testing.T) {
	db := isolatedDB(t, &model.Asset{}, &model.GenerationTask{})
	r := NewAssetRepository(db)
	ctx := context.Background()
	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	const me, other = uint64(1), uint64(2)
	canvasA, canvasB := uint64(10), uint64(11)

	mkTask := func(user uint64, canvas *uint64, isTest bool, input string) uint64 {
		tk := model.GenerationTask{
			UserID: user, CanvasProjectID: canvas, Kind: "image", ModelKey: "m1", Provider: "p", Status: model.TaskSucceeded, IsTest: isTest,
			InputJSON: datatypes.JSON(input), ConfigSnapshot: datatypes.JSON(`{"model":{"label":"天才猴子"}}`),
			DeadlineAt: time.Now().Add(time.Hour), NextPollAt: time.Now(),
		}
		if err := db.Create(&tk).Error; err != nil {
			t.Fatal(err)
		}
		return tk.ID
	}
	n := 0
	mkAsset := func(user uint64, kind, source string, task *uint64, at time.Time) uint64 {
		n++
		a := &model.Asset{UserID: user, Kind: kind, Source: source, TaskID: task, StorageKey: "k/" + string(rune('a'+n)), MimeType: "x/y", CreatedAt: at}
		if err := r.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	tConv := mkTask(me, nil, false, `{"prompt":"雪地里的旧楼，黄昏"}`)
	tCanvasA := mkTask(me, &canvasA, false, `{"prompt":"Hello World"}`)
	tCanvasB := mkTask(me, &canvasB, false, `{"prompt":"100%_纯色"}`)
	tTest := mkTask(me, nil, true, `{"prompt":"试跑"}`)
	tOther := mkTask(other, nil, false, `{"prompt":"别人的"}`)

	aConv := mkAsset(me, "image", model.AssetSourceGenerated, &tConv, base)
	// 同一时刻的两个：id 大的排前
	aCanvasA := mkAsset(me, "image", model.AssetSourceGenerated, &tCanvasA, base.Add(time.Hour))
	aCanvasB := mkAsset(me, "image", model.AssetSourceGenerated, &tCanvasB, base.Add(time.Hour))
	aNewest := mkAsset(me, "image", model.AssetSourceGenerated, &tConv, base.Add(2*time.Hour))
	mkAsset(me, "image", model.AssetSourceGenerated, &tTest, base.Add(3*time.Hour))     // 试跑任务：不出现
	mkAsset(me, "image", model.AssetSourceUpload, nil, base.Add(4*time.Hour))           // 上传来源：不出现
	mkAsset(other, "image", model.AssetSourceGenerated, &tOther, base.Add(5*time.Hour)) // 别人的：不出现
	mkAsset(me, "video", model.AssetSourceGenerated, &tConv, base.Add(6*time.Hour))     // 别的类型：不出现

	ids := func(rows []model.GeneratedAssetRow) []uint64 {
		out := make([]uint64, 0, len(rows))
		for _, x := range rows {
			out = append(out, x.ID)
		}
		return out
	}
	list := func(f GeneratedAssetFilter) []model.GeneratedAssetRow {
		t.Helper()
		f.Kind = "image"
		if f.Limit == 0 {
			f.Limit = 50
		}
		rows, err := r.ListGeneratedByUser(ctx, me, f)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}

	t.Run("只含自己的、生成来源、非试跑、指定类型的素材，最新优先、同刻按 id 倒序", func(t *testing.T) {
		got := ids(list(GeneratedAssetFilter{}))
		if want := []uint64{aNewest, aCanvasB, aCanvasA, aConv}; !slices.Equal(got, want) {
			t.Fatalf("期望 %v，实际 %v", want, got)
		}
	})
	t.Run("带出任务的提示词、模型与画布来源", func(t *testing.T) {
		rows := list(GeneratedAssetFilter{})
		byID := map[uint64]model.GeneratedAssetRow{}
		for _, x := range rows {
			byID[x.ID] = x
		}
		if byID[aConv].CanvasProjectID != nil || byID[aCanvasA].CanvasProjectID == nil || *byID[aCanvasA].CanvasProjectID != canvasA {
			t.Fatalf("画布来源不对：%+v %+v", byID[aConv].CanvasProjectID, byID[aCanvasA].CanvasProjectID)
		}
		if byID[aConv].TaskModelKey != "m1" || !jsonEqual(byID[aConv].InputJSON, `{"prompt":"雪地里的旧楼，黄昏"}`) ||
			!jsonEqual(byID[aConv].ConfigSnapshot, `{"model":{"label":"天才猴子"}}`) {
			t.Fatalf("任务字段不对：%+v", byID[aConv])
		}
	})
	t.Run("order=asc 反向", func(t *testing.T) {
		got := ids(list(GeneratedAssetFilter{Asc: true}))
		if want := []uint64{aConv, aCanvasA, aCanvasB, aNewest}; !slices.Equal(got, want) {
			t.Fatalf("期望 %v，实际 %v", want, got)
		}
	})
	t.Run("source：conversation 只要没有画布的任务，canvas 只要有画布的任务", func(t *testing.T) {
		if got, want := ids(list(GeneratedAssetFilter{Source: "conversation"})), []uint64{aNewest, aConv}; !slices.Equal(got, want) {
			t.Fatalf("conversation 期望 %v，实际 %v", want, got)
		}
		if got, want := ids(list(GeneratedAssetFilter{Source: "canvas"})), []uint64{aCanvasB, aCanvasA}; !slices.Equal(got, want) {
			t.Fatalf("canvas 期望 %v，实际 %v", want, got)
		}
	})
	t.Run("canvas_id 只看这张画布", func(t *testing.T) {
		if got, want := ids(list(GeneratedAssetFilter{CanvasID: canvasA})), []uint64{aCanvasA}; !slices.Equal(got, want) {
			t.Fatalf("期望 %v，实际 %v", want, got)
		}
	})
	t.Run("created_from 含边界", func(t *testing.T) {
		from := base.Add(time.Hour)
		if got, want := ids(list(GeneratedAssetFilter{CreatedFrom: &from})), []uint64{aNewest, aCanvasB, aCanvasA}; !slices.Equal(got, want) {
			t.Fatalf("期望 %v，实际 %v", want, got)
		}
	})
	t.Run("q 在提示词上忽略大小写包含匹配，% 与 _ 不当通配符", func(t *testing.T) {
		if got, want := ids(list(GeneratedAssetFilter{Q: "hello"})), []uint64{aCanvasA}; !slices.Equal(got, want) {
			t.Fatalf("期望 %v，实际 %v", want, got)
		}
		if got, want := ids(list(GeneratedAssetFilter{Q: "100%_"})), []uint64{aCanvasB}; !slices.Equal(got, want) {
			t.Fatalf("期望 %v，实际 %v", want, got)
		}
		if got := list(GeneratedAssetFilter{Q: "%"}); len(got) != 1 {
			t.Fatalf("单个 %% 只应匹配含 %% 的提示词，实际 %d 条", len(got))
		}
	})
	t.Run("游标翻页：每页 2 条，不重不漏", func(t *testing.T) {
		var all []uint64
		var after *TimeCursor
		for page := 0; page < 5; page++ {
			rows := list(GeneratedAssetFilter{Limit: 2, After: after})
			if len(rows) == 0 {
				break
			}
			all = append(all, ids(rows)...)
			last := rows[len(rows)-1]
			after = &TimeCursor{At: last.CreatedAt, ID: last.ID}
		}
		if want := []uint64{aNewest, aCanvasB, aCanvasA, aConv}; !slices.Equal(all, want) {
			t.Fatalf("期望 %v，实际 %v", want, all)
		}
	})
}

func TestAssetFolderRepository(t *testing.T) {
	db := isolatedDB(t, &model.AssetFolder{}, &model.AssetLibraryItem{})
	fr := NewAssetFolderRepository(db)
	ir := NewAssetLibraryRepository(db)
	ctx := context.Background()
	const me, other = uint64(1), uint64(2)

	f1 := &model.AssetFolder{UserID: me, Name: "角色"}
	if err := fr.Create(ctx, f1); err != nil {
		t.Fatal(err)
	}

	t.Run("同一用户下重名（忽略大小写）返回 ErrDuplicate，别的用户可以同名", func(t *testing.T) {
		if err := fr.Create(ctx, &model.AssetFolder{UserID: me, Name: "Hero"}); err != nil {
			t.Fatal(err)
		}
		if err := fr.Create(ctx, &model.AssetFolder{UserID: me, Name: "hERO"}); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("期望 ErrDuplicate，实际 %v", err)
		}
		if err := fr.Create(ctx, &model.AssetFolder{UserID: other, Name: "Hero"}); err != nil {
			t.Fatalf("别的用户同名应可建：%v", err)
		}
		ok, err := fr.NameExists(ctx, me, "HERO", 0)
		if err != nil || !ok {
			t.Fatalf("NameExists 应为 true：%v %v", ok, err)
		}
	})
	t.Run("改名重名返回 ErrDuplicate，别人的文件夹返回 ErrNotFound", func(t *testing.T) {
		f2 := &model.AssetFolder{UserID: me, Name: "场景"}
		if err := fr.Create(ctx, f2); err != nil {
			t.Fatal(err)
		}
		if err := fr.Rename(ctx, me, f2.ID, "角色"); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("期望 ErrDuplicate，实际 %v", err)
		}
		if err := fr.Rename(ctx, other, f2.ID, "别人改"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
		if err := fr.Rename(ctx, me, f2.ID, "场景二"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("ListWithCount 只数本用户本文件夹里的归属", func(t *testing.T) {
		for _, it := range []*model.AssetLibraryItem{
			{UserID: me, AssetID: 101, FolderID: &f1.ID},
			{UserID: me, AssetID: 102, FolderID: &f1.ID},
			{UserID: me, AssetID: 103}, // 根目录
		} {
			if err := ir.Create(ctx, it); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := fr.ListWithCount(ctx, me)
		if err != nil {
			t.Fatal(err)
		}
		counts := map[string]int{}
		for _, x := range rows {
			counts[x.Name] = x.ItemCount
		}
		if counts["角色"] != 2 || counts["Hero"] != 0 {
			t.Fatalf("素材数不对：%v", counts)
		}
		if rows2, _ := fr.ListWithCount(ctx, other); len(rows2) != 1 {
			t.Fatalf("别的用户只应看到自己的文件夹：%d", len(rows2))
		}
	})
	t.Run("同一素材在同一用户的库里只能有一条归属", func(t *testing.T) {
		if err := ir.Create(ctx, &model.AssetLibraryItem{UserID: me, AssetID: 101}); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("期望 ErrDuplicate，实际 %v", err)
		}
		if err := ir.Create(ctx, &model.AssetLibraryItem{UserID: other, AssetID: 101}); err != nil {
			t.Fatalf("别的用户加同一素材应可：%v", err)
		}
	})
	t.Run("删除事务：回调出错整体回滚，成功则归属行与文件夹一起删、返回行数", func(t *testing.T) {
		boom := errors.New("boom")
		err := fr.WithTx(ctx, func(tx AssetFolderTx) error {
			if _, err := tx.DeleteItems(ctx, me, f1.ID); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("期望回调错误，实际 %v", err)
		}
		if cnt, _ := fr.CountItems(ctx, me, f1.ID); cnt != 2 {
			t.Fatalf("回滚后归属行应还在：%d", cnt)
		}

		var removed int64
		err = fr.WithTx(ctx, func(tx AssetFolderTx) error {
			n, err := tx.DeleteItems(ctx, me, f1.ID)
			removed = n
			if err != nil {
				return err
			}
			return tx.Delete(ctx, me, f1.ID)
		})
		if err != nil || removed != 2 {
			t.Fatalf("removed=%d err=%v", removed, err)
		}
		if _, err := fr.GetByID(ctx, me, f1.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("文件夹应已删除：%v", err)
		}
		// 根目录的归属行与别人的归属行不受影响
		if _, err := ir.GetByAssetID(ctx, me, 103); err != nil {
			t.Fatalf("根目录归属应保留：%v", err)
		}
		if _, err := ir.GetByAssetID(ctx, other, 101); err != nil {
			t.Fatalf("别人的归属应保留：%v", err)
		}
	})
}

func TestAssetLibraryRepository_List(t *testing.T) {
	db := isolatedDB(t, &model.Asset{}, &model.AssetFolder{}, &model.AssetLibraryItem{})
	ar := NewAssetRepository(db)
	ir := NewAssetLibraryRepository(db)
	ctx := context.Background()
	const me, other = uint64(1), uint64(2)
	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)

	mkAsset := func(user uint64, kind, name, key string) uint64 {
		a := &model.Asset{UserID: user, Kind: kind, Source: model.AssetSourceUpload, StorageKey: key, MimeType: "x/y", FileName: name}
		if err := ar.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	folder := &model.AssetFolder{UserID: me, Name: "角色"}
	if err := db.Create(folder).Error; err != nil {
		t.Fatal(err)
	}
	mkItem := func(user, asset uint64, fid *uint64, at time.Time) uint64 {
		it := &model.AssetLibraryItem{UserID: user, AssetID: asset, FolderID: fid, CreatedAt: at}
		if err := ir.Create(ctx, it); err != nil {
			t.Fatal(err)
		}
		return it.ID
	}
	rootImg := mkItem(me, mkAsset(me, "image", "Cat.png", "a"), nil, base)
	rootVid := mkItem(me, mkAsset(me, "video", "dog.mp4", "b"), nil, base.Add(time.Hour))
	inFolder := mkItem(me, mkAsset(me, "image", "cat-hero.png", "c"), &folder.ID, base.Add(2*time.Hour))
	mkItem(other, mkAsset(other, "image", "cat-other.png", "d"), nil, base.Add(3*time.Hour))

	ids := func(rows []model.AssetLibraryItem) []uint64 {
		out := make([]uint64, 0, len(rows))
		for _, x := range rows {
			out = append(out, x.ID)
		}
		return out
	}
	list := func(f LibraryItemFilter) []uint64 {
		t.Helper()
		if f.Limit == 0 {
			f.Limit = 50
		}
		rows, err := ir.List(ctx, me, f)
		if err != nil {
			t.Fatal(err)
		}
		return ids(rows)
	}

	if got, want := list(LibraryItemFilter{Scope: LibraryScopeRoot}), []uint64{rootVid, rootImg}; !slices.Equal(got, want) {
		t.Fatalf("根目录期望 %v，实际 %v", want, got)
	}
	if got, want := list(LibraryItemFilter{Scope: LibraryScopeFolder, FolderID: folder.ID}), []uint64{inFolder}; !slices.Equal(got, want) {
		t.Fatalf("文件夹期望 %v，实际 %v", want, got)
	}
	if got, want := list(LibraryItemFilter{Scope: LibraryScopeAll}), []uint64{inFolder, rootVid, rootImg}; !slices.Equal(got, want) {
		t.Fatalf("全部期望 %v，实际 %v（不应含别人的）", want, got)
	}
	if got, want := list(LibraryItemFilter{Scope: LibraryScopeAll, Kind: "image"}), []uint64{inFolder, rootImg}; !slices.Equal(got, want) {
		t.Fatalf("类型过滤期望 %v，实际 %v", want, got)
	}
	if got, want := list(LibraryItemFilter{Scope: LibraryScopeAll, Q: "CAT"}), []uint64{inFolder, rootImg}; !slices.Equal(got, want) {
		t.Fatalf("文件名搜索期望 %v，实际 %v", want, got)
	}
	// 游标
	rows, _ := ir.List(ctx, me, LibraryItemFilter{Scope: LibraryScopeAll, Limit: 1})
	after := &TimeCursor{At: rows[0].CreatedAt, ID: rows[0].ID}
	if got, want := list(LibraryItemFilter{Scope: LibraryScopeAll, After: after}), []uint64{rootVid, rootImg}; !slices.Equal(got, want) {
		t.Fatalf("游标后期望 %v，实际 %v", want, got)
	}

	// 移动与移出：别人的条目一律 ErrNotFound
	if err := ir.SetFolder(ctx, other, rootImg, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，实际 %v", err)
	}
	if err := ir.SetFolder(ctx, me, rootImg, &folder.ID); err != nil {
		t.Fatal(err)
	}
	if err := ir.Delete(ctx, other, rootImg); !errors.Is(err, ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，实际 %v", err)
	}
	if err := ir.Delete(ctx, me, rootImg); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasProjectRepository_BriefsByIDs(t *testing.T) {
	db := isolatedDB(t, &model.CanvasProject{})
	r := NewCanvasProjectRepository(db)
	ctx := context.Background()
	mk := func(user uint64, title string) uint64 {
		p := &model.CanvasProject{UserID: user, Title: title, PayloadJSON: datatypes.JSON(`{}`), Revision: 1}
		if err := r.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		return p.ID
	}
	live, gone, others := mk(1, "活着"), mk(1, "已删"), mk(2, "别人")
	if err := db.Delete(&model.CanvasProject{}, gone).Error; err != nil { // 软删除
		t.Fatal(err)
	}
	got, err := r.BriefsByIDs(ctx, 1, []uint64{live, gone, others, 987654321})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[uint64]model.CanvasBrief{}
	for _, b := range got {
		byID[b.ID] = b
	}
	if len(byID) != 2 || byID[live].Title != "活着" || byID[live].Deleted || !byID[gone].Deleted {
		t.Fatalf("结果不对：%+v", byID)
	}
	if got, err := r.BriefsByIDs(ctx, 1, nil); err != nil || len(got) != 0 {
		t.Fatalf("空 ids 应返回空：%v %v", got, err)
	}
}
