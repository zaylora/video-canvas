package service_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	. "video-canvas/internal/service"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/storage"
)

// 故事 / 剧本文档（.txt、.md）：作为附件上传，和图片走同一套存储，素材种类是 document。
// 文档没有 magic bytes，只能按扩展名 + 内容是合法的 UTF-8 文本判断，所以这里把「像文本但不是文本」的情形都挡住。

func TestAssetService_Upload_Document(t *testing.T) {
	assetIsolateTempDir(t)
	cfg := config.Storage{MaxUpload: 1 << 20, SignedTTL: 30 * time.Minute}
	story := []byte("第一场 雨夜\n林夏站在便利店门口。\n")

	upload := func(name string, body []byte) (*model.AssetView, *fakeAssetRepo, *fakeAssetStore, error) {
		repo, store := newFakeAssetRepo(), newFakeAssetStore()
		svc := NewAssetService(repo, newFakeRegistry(1, objectHandle(1, store, cfg.SignedTTL)), cfg)
		svc.SetNow(func() time.Time { return time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC) })
		v, err := svc.Upload(context.Background(), 7, UploadInput{FileName: name, Size: -1, Body: bytes.NewReader(body)})
		return v, repo, store, err
	}

	t.Run(".txt：种类是 document，类型 text/plain，原样存储，没有宽高", func(t *testing.T) {
		v, repo, store, err := upload("_剧本原文.txt", story)
		if err != nil {
			t.Fatal(err)
		}
		if v.Kind != "document" || v.MimeType != "text/plain" || v.ByteSize != int64(len(story)) || v.Width != 0 || v.Height != 0 || v.FileName != "_剧本原文.txt" {
			t.Fatalf("视图不对：%+v", v)
		}
		row := repo.rows[v.ID]
		if !strings.HasSuffix(row.StorageKey, ".txt") || row.Source != model.AssetSourceUpload || !bytes.Equal(store.objects[row.StorageKey], story) {
			t.Fatalf("入库不对：%+v", row)
		}
	})

	t.Run(".md：text/markdown，扩展名取自类型", func(t *testing.T) {
		v, repo, _, err := upload("故事.md", []byte("# 标题\n\n正文"))
		if err != nil || v.Kind != "document" || v.MimeType != "text/markdown" || !strings.HasSuffix(repo.rows[v.ID].StorageKey, ".md") {
			t.Fatalf("实际 %+v err=%v", v, err)
		}
	})

	t.Run("带 BOM 的 UTF-8 文本（Windows 记事本存的）可以", func(t *testing.T) {
		v, _, _, err := upload("a.txt", append([]byte{0xEF, 0xBB, 0xBF}, story...))
		if err != nil || v.Kind != "document" {
			t.Fatalf("实际 %+v err=%v", v, err)
		}
	})

	rejects := map[string]struct {
		name string
		body []byte
	}{
		"扩展名不是 txt / md（html 之类可携带脚本的不收）": {"页面.html", []byte("<script>alert(1)</script>")},
		"没有扩展名": {"剧本", story},
		"扩展名是 txt，内容是二进制（含 NUL）":        {"假.txt", []byte("abc\x00def")},
		"扩展名是 txt，内容不是合法 UTF-8（GBK 编码）": {"gbk.txt", []byte{0xB9, 0xCA, 0xCA, 0xC2, 0xD4, 0xAD, 0xCE, 0xC4}},
		"内容为空": {"空.txt", nil},
	}
	for name, c := range rejects {
		t.Run("拒绝："+name, func(t *testing.T) {
			_, _, _, err := upload(c.name, c.body)
			if err == nil {
				t.Fatal("应被拒绝")
			}
			if got := assetErrCode(t, err); got != errcode.ErrAssetInvalid.Code {
				t.Fatalf("应是素材不合法，实际 %d：%v", got, err)
			}
		})
	}

	t.Run("文本文件有上限（20 万字节）：超过的拒绝并说明", func(t *testing.T) {
		big := bytes.Repeat([]byte("字"), 70000) // 21 万字节
		_, _, _, err := upload("长篇.txt", big)
		if err == nil || !strings.Contains(err.Error(), "20 万字节") {
			t.Fatalf("应提示上限：%v", err)
		}
	})

	t.Run("图片仍按内容判断：改成 .txt 的 png 还是图片", func(t *testing.T) {
		png := assetTestPNG(t, 30, 20)
		v, _, _, err := upload("假装是文本.txt", png)
		if err != nil || v.Kind != "image" || v.MimeType != "image/png" {
			t.Fatalf("内容是 png 就是图片：%+v err=%v", v, err)
		}
	})
}

// 生成产物不走文本通道：平台声明 text/plain 的二进制不能借「兜底类型」混进素材。
func TestAssetService_SaveGenerated_DoesNotAcceptDocuments(t *testing.T) {
	assetIsolateTempDir(t)
	cfg := config.Storage{MaxUpload: 1 << 20, SignedTTL: time.Hour}
	repo, store := newFakeAssetRepo(), newFakeAssetStore()
	svc := NewAssetService(repo, newFakeRegistry(1, objectHandle(1, store, cfg.SignedTTL)), cfg)
	_, _, err := svc.SaveGenerated(context.Background(), provider.SaveGeneratedInput{UserID: 1, TaskID: 1, FileName: "out.txt", MimeType: "text/plain", Body: bytes.NewReader([]byte("一段文字"))})
	if err == nil {
		t.Fatal("生成产物里的文本不是素材，应被拒绝")
	}
}

func TestAssetService_DirectUpload_Document(t *testing.T) {
	ctx := context.Background()
	story := []byte("第一场 雨夜\n林夏站在便利店门口。\n")
	docIntent := func(name, mime string, size int) UploadIntentInput {
		return UploadIntentInput{FileName: name, Size: int64(size), MimeType: mime}
	}
	finish := func(t *testing.T, e *directEnv, in UploadIntentInput, content []byte) (*model.AssetView, error) {
		t.Helper()
		v, err := e.svc.CreateUploadIntent(ctx, 7, in)
		if err != nil {
			t.Fatal(err)
		}
		key := e.intent.rows[v.IntentID].StorageKey
		e.store.objects[key], e.store.types[key] = content, in.MimeType
		return e.svc.CompleteUpload(ctx, 7, v.IntentID)
	}

	t.Run("申请：text/plain 和 text/markdown 可以，意图里种类是 document，key 带 .txt / .md", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		v, err := e.svc.CreateUploadIntent(ctx, 7, docIntent("a.txt", "text/plain", len(story)))
		if err != nil {
			t.Fatal(err)
		}
		in := e.intent.rows[v.IntentID]
		if in.Kind != "document" || !strings.HasSuffix(in.StorageKey, ".txt") {
			t.Fatalf("意图不对：%+v", in)
		}
		v2, err := e.svc.CreateUploadIntent(ctx, 7, docIntent("b.md", "text/markdown", 10))
		if err != nil || !strings.HasSuffix(e.intent.rows[v2.IntentID].StorageKey, ".md") {
			t.Fatalf("实际 %+v err=%v", v2, err)
		}
	})

	t.Run("浏览器给 .md 的类型常常是空的：按文件名补成 text/markdown", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		v, err := e.svc.CreateUploadIntent(ctx, 7, docIntent("故事.md", "", 10))
		if err != nil || e.intent.rows[v.IntentID].MimeType != "text/markdown" {
			t.Fatalf("实际 %+v err=%v", v, err)
		}
		if _, err := e.svc.CreateUploadIntent(ctx, 7, docIntent("a.txt", "application/octet-stream", 10)); err != nil {
			t.Fatalf("octet-stream 的 txt 也按文件名补：%v", err)
		}
	})

	t.Run("申请：超过 20 万字节、不认识的类型和文件名都拒绝", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		if _, err := e.svc.CreateUploadIntent(ctx, 7, docIntent("长.txt", "text/plain", 200001)); err == nil || !strings.Contains(err.Error(), "20 万字节") {
			t.Fatalf("应提示上限：%v", err)
		}
		if _, err := e.svc.CreateUploadIntent(ctx, 7, docIntent("页面.html", "text/html", 10)); err == nil {
			t.Fatal("text/html 不在白名单")
		}
		if _, err := e.svc.CreateUploadIntent(ctx, 7, docIntent("剧本", "", 10)); err == nil {
			t.Fatal("没有类型也没有 txt/md 扩展名")
		}
	})

	t.Run("登记：内容是合法 UTF-8 文本就记成 document", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		v, err := finish(t, e, docIntent("_剧本原文.txt", "text/plain", len(story)), story)
		if err != nil {
			t.Fatal(err)
		}
		row := e.repo.rows[v.ID]
		if row.Kind != "document" || row.MimeType != "text/plain" || row.Width != 0 || row.ByteSize != int64(len(story)) || v.FileName != "_剧本原文.txt" {
			t.Fatalf("入库行不对：%+v", row)
		}
	})

	t.Run("登记：声明是文本、内容却是二进制：删掉对象并拒绝（直传绕过了后端，这里是唯一把关）", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		_, err := finish(t, e, docIntent("假.txt", "text/plain", 8), []byte("abc\x00defg"))
		if err == nil || assetErrCode(t, err) != errcode.ErrAssetInvalid.Code {
			t.Fatalf("应拒绝：%v", err)
		}
		if len(e.repo.rows) != 0 || len(e.store.deleted) == 0 {
			t.Fatalf("不应登记，对象应被删除：rows=%d deleted=%v", len(e.repo.rows), e.store.deleted)
		}
	})

	t.Run("登记：声明是图片、内容是文本：不当文档收（声明的类型必须和内容一致）", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		_, err := finish(t, e, pngIntent(len(story)), story)
		if err == nil {
			t.Fatal("声明 png、内容是文本，应拒绝")
		}
	})

}
