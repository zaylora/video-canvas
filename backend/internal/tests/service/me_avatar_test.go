package service_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"regexp"
	"strings"
	"testing"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
	"video-canvas/internal/storage"
)

// 本文件测试头像上传 / 移除，以及 /files/avatars/... 的反查。

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gifBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, image.NewPaletted(image.Rect(0, 0, w, h), []color.Color{color.Black, color.White}), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// webpVP8XBytes 构造只有 VP8X 头的 webp（足够让尺寸探测读出宽高）。
func webpVP8XBytes(w, h int) []byte {
	b := make([]byte, 40)
	copy(b, "RIFF")
	copy(b[8:], "WEBPVP8X")
	w--
	h--
	b[24], b[25], b[26] = byte(w), byte(w>>8), byte(w>>16)
	b[27], b[28], b[29] = byte(h), byte(h>>8), byte(h>>16)
	return b
}

var avatarKeyRe = regexp.MustCompile(`^avatars/1/[0-9a-f]{16}\.(png|jpg|webp|gif)$`)

func TestMeService_UploadAvatar(t *testing.T) {
	ctx := context.Background()

	ok := map[string]func(t *testing.T) []byte{
		"png":     func(t *testing.T) []byte { return pngBytes(t, 512, 512) },
		"jpeg":    func(t *testing.T) []byte { return jpegBytes(t, 64, 64) },
		"gif":     func(t *testing.T) []byte { return gifBytes(t, 32, 32) },
		"webp":    func(*testing.T) []byte { return webpVP8XBytes(512, 512) },
		"2048 边界": func(t *testing.T) []byte { return pngBytes(t, 2048, 2048) },
	}
	for name, mk := range ok {
		t.Run("成功："+name, func(t *testing.T) {
			e := newMeEnv(t)
			data := mk(t)
			v, err := e.svc.UploadAvatar(ctx, meUID, bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			u := e.repo.users[meUID]
			if !avatarKeyRe.MatchString(u.AvatarKey) || u.AvatarStorageID != 1 {
				t.Fatalf("avatar_key / storage 不对：%q %d", u.AvatarKey, u.AvatarStorageID)
			}
			if v.AvatarURL != "/files/"+u.AvatarKey {
				t.Fatalf("avatar_url = %q", v.AvatarURL)
			}
			if !bytes.Equal(e.store.objects[u.AvatarKey], data) {
				t.Fatal("存储里的内容不对")
			}
			if len(e.inval.ids) != 1 {
				t.Fatalf("应清缓存：%v", e.inval.ids)
			}
		})
	}

	t.Run("替换：删除旧 key（按旧头像所在的存储）", func(t *testing.T) {
		e := newMeEnv(t)
		oldStore := newFakeAssetStore()
		oldStore.objects["avatars/1/0000000000000000.png"] = []byte("old")
		e.reg.handles[2] = objectHandle(2, oldStore, 0)
		e.repo.users[meUID].AvatarKey = "avatars/1/0000000000000000.png"
		e.repo.users[meUID].AvatarStorageID = 2
		if _, err := e.svc.UploadAvatar(ctx, meUID, bytes.NewReader(pngBytes(t, 8, 8))); err != nil {
			t.Fatal(err)
		}
		if len(oldStore.deleted) != 1 || oldStore.deleted[0] != "avatars/1/0000000000000000.png" {
			t.Fatalf("应删除旧头像：%v", oldStore.deleted)
		}
		if e.repo.users[meUID].AvatarKey == "avatars/1/0000000000000000.png" {
			t.Fatal("avatar_key 没更新")
		}
	})

	t.Run("删除旧 key 失败只记日志，不报错", func(t *testing.T) {
		e := newMeEnv(t)
		e.store.delErr = errBoom
		e.repo.users[meUID].AvatarKey = "avatars/1/0000000000000000.png"
		e.repo.users[meUID].AvatarStorageID = 1
		if _, err := e.svc.UploadAvatar(ctx, meUID, bytes.NewReader(pngBytes(t, 8, 8))); err != nil {
			t.Fatalf("删除旧文件失败不应影响结果：%v", err)
		}
	})

	t.Run("txt 伪装成 png：55005", func(t *testing.T) {
		e := newMeEnv(t)
		_, err := e.svc.UploadAvatar(ctx, meUID, strings.NewReader("hello, I am a text file pretending to be png"))
		wantBizErr(t, err, errcode.ErrAvatarFormat)
		if len(e.store.objects) != 0 || len(e.repo.updates) != 0 {
			t.Fatal("拒绝时不应写存储 / 写库")
		}
	})
	t.Run("白名单外的媒体（mp3）：55005", func(t *testing.T) {
		e := newMeEnv(t)
		_, err := e.svc.UploadAvatar(ctx, meUID, bytes.NewReader(append([]byte("ID3"), make([]byte, 64)...)))
		wantBizErr(t, err, errcode.ErrAvatarFormat)
	})
	t.Run("文件头是 png 但无法解码：55005", func(t *testing.T) {
		e := newMeEnv(t)
		_, err := e.svc.UploadAvatar(ctx, meUID, bytes.NewReader([]byte("\x89PNG\r\n\x1a\ngarbage-garbage-garbage")))
		wantBizErr(t, err, errcode.ErrAvatarFormat)
	})
	t.Run("空文件：55005", func(t *testing.T) {
		e := newMeEnv(t)
		_, err := e.svc.UploadAvatar(ctx, meUID, bytes.NewReader(nil))
		wantBizErr(t, err, errcode.ErrAvatarFormat)
	})
	t.Run("超过 2MB：55006", func(t *testing.T) {
		e := newMeEnv(t)
		data := append(pngBytes(t, 8, 8), make([]byte, AvatarMaxBytes)...)
		_, err := e.svc.UploadAvatar(ctx, meUID, bytes.NewReader(data))
		ec := bizErrOf(t, err, errcode.ErrAvatarTooLarge)
		if ec.HTTPStatus() != 400 {
			t.Fatalf("55006 应为 400：%d", ec.HTTPStatus())
		}
	})
	for name, data := range map[string]func(t *testing.T) []byte{
		"宽超过 2048": func(t *testing.T) []byte { return pngBytes(t, 2049, 1) },
		"高超过 2048": func(t *testing.T) []byte { return pngBytes(t, 1, 2049) },
		"webp 超大":  func(*testing.T) []byte { return webpVP8XBytes(4000, 4000) },
	} {
		t.Run("尺寸超限："+name+"：55006", func(t *testing.T) {
			e := newMeEnv(t)
			_, err := e.svc.UploadAvatar(ctx, meUID, bytes.NewReader(data(t)))
			wantBizErr(t, err, errcode.ErrAvatarTooLarge)
		})
	}
	t.Run("写存储失败：内部错误，不改 avatar_key", func(t *testing.T) {
		e := newMeEnv(t)
		e.store.putErr = errBoom
		_, err := e.svc.UploadAvatar(ctx, meUID, bytes.NewReader(pngBytes(t, 8, 8)))
		if err == nil || codeOf(err) != -1 {
			t.Fatalf("应是内部错误：%v", err)
		}
		if e.repo.users[meUID].AvatarKey != "" {
			t.Fatal("不应改 avatar_key")
		}
	})
	t.Run("写库失败：清理刚写入的对象", func(t *testing.T) {
		e := newMeEnv(t)
		e.repo.errs["Update"] = errBoom
		if _, err := e.svc.UploadAvatar(ctx, meUID, bytes.NewReader(pngBytes(t, 8, 8))); err == nil {
			t.Fatal("应失败")
		}
		if len(e.store.objects) != 0 || len(e.store.deleted) != 1 {
			t.Fatalf("应清理孤儿对象：%v %v", e.store.objects, e.store.deleted)
		}
	})
}

func TestMeService_DeleteAvatar(t *testing.T) {
	ctx := context.Background()
	t.Run("清空 avatar_key，删除旧文件，清缓存", func(t *testing.T) {
		e := newMeEnv(t)
		e.repo.users[meUID].AvatarKey = "avatars/1/0000000000000000.png"
		e.repo.users[meUID].AvatarStorageID = 1
		v, err := e.svc.DeleteAvatar(ctx, meUID)
		if err != nil {
			t.Fatal(err)
		}
		if v.AvatarURL != "" || e.repo.users[meUID].AvatarKey != "" || e.repo.users[meUID].AvatarStorageID != 0 {
			t.Fatalf("%+v", v)
		}
		if len(e.store.deleted) != 1 || len(e.inval.ids) != 1 {
			t.Fatalf("%v %v", e.store.deleted, e.inval.ids)
		}
	})
	t.Run("本来就没有头像：直接成功，不写库", func(t *testing.T) {
		e := newMeEnv(t)
		v, err := e.svc.DeleteAvatar(ctx, meUID)
		if err != nil || v.AvatarURL != "" || len(e.repo.updates) != 0 {
			t.Fatalf("%v %+v %v", err, v, e.repo.updates)
		}
	})
}

// fakeAvatarLocator 实现 AvatarLocator。
type fakeAvatarLocator map[string]uint64

func (f fakeAvatarLocator) AvatarStorageID(_ context.Context, key string) (uint64, error) {
	if id, ok := f[key]; ok {
		return id, nil
	}
	return 0, repository.ErrNotFound
}

func TestAssetService_ResolveFile_Avatar(t *testing.T) {
	ctx := context.Background()
	repo, store := newFakeAssetRepo(), newFakeAssetStore()
	svc := NewAssetService(repo, newFakeRegistry(1, objectHandle(1, store, 0), objectHandle(2, store, 0)), config.Storage{SignedTTL: 0})
	svc.SetAvatarLocator(fakeAvatarLocator{"avatars/1/aaaaaaaaaaaaaaaa.webp": 2})

	t.Run("当前头像：按头像记录的存储签名跳转", func(t *testing.T) {
		tgt, err := svc.ResolveFile(ctx, "avatars/1/aaaaaaaaaaaaaaaa.webp")
		if err != nil || !strings.Contains(tgt.RedirectURL, "avatars/1/aaaaaaaaaaaaaaaa.webp") {
			t.Fatalf("%v %+v", err, tgt)
		}
	})
	t.Run("已被替换 / 不存在的头像：404", func(t *testing.T) {
		_, err := svc.ResolveFile(ctx, "avatars/1/bbbbbbbbbbbbbbbb.webp")
		wantBizErr(t, err, errcode.ErrAssetNotFound)
	})
	t.Run("非 avatars/ 前缀不走头像反查", func(t *testing.T) {
		svc2 := NewAssetService(repo, newFakeRegistry(1, objectHandle(1, store, 0)), config.Storage{})
		svc2.SetAvatarLocator(fakeAvatarLocator{"u1/x.png": 1})
		_, err := svc2.ResolveFile(ctx, "u1/x.png")
		wantBizErr(t, err, errcode.ErrAssetNotFound)
	})
}

var (
	_ = model.LedgerFreeze
	_ = storage.ErrNotFound
)
