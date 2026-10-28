package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service/agent"
)

// newAttachmentEnv 装一个能校验附件的桥：素材 1、2 是 owner(=1) 的图片，3 太大，4 是 svg，5 是别人的。
func newAttachmentEnv(t *testing.T, remote ...bool) *AgentBridge {
	t.Helper()
	b := newBridgeEnv(t)
	assets := &fakeAssets{owner: 1, remote: len(remote) > 0 && remote[0], files: map[uint64]*model.Asset{
		1: {ID: 1, Kind: "image", MimeType: "image/png", ByteSize: 3, Width: 800, Height: 600, FileName: "参考.png", StorageKey: "u1/a.png"},
		2: {ID: 2, Kind: "image", MimeType: "image/jpeg", ByteSize: 3, StorageKey: "u1/b.jpg"},
		3: {ID: 3, Kind: "image", MimeType: "image/png", ByteSize: 6 << 20, StorageKey: "u1/big.png"},
		4: {ID: 4, Kind: "image", MimeType: "image/svg+xml", ByteSize: 3, StorageKey: "u1/c.svg"},
		6: {ID: 6, Kind: "video", MimeType: "video/mp4", ByteSize: 3, StorageKey: "u1/v.mp4"},
	}, data: map[uint64][]byte{1: []byte("ABC"), 2: []byte("XYZ")}}
	return NewAgentBridge(BridgeDeps{
		Repo: b.repo, Canvas: NewAgentCanvasService(b.repo, b.bc), Agent: b.svc, Billing: NewAgentBilling(b.bill),
		Registry: b.reg, Secrets: fakeSecrets{"channel:ch1": "sk-test"}, Streamer: b.str, Assets: assets,
		AssetLinks: fakeLinks{},
	})
}

type fakeLinks struct{}

func (fakeLinks) FileURL(key string) string { return "/files/" + key }

func invalidMsg(t *testing.T, err error) string {
	t.Helper()
	var e *errcode.Error
	if !errors.As(err, &e) || e.Code != errcode.ErrInvalidParams.Code {
		t.Fatalf("应是 10001 业务错误: %v", err)
	}
	return e.Msg
}

func TestCheckAttachments_Success(t *testing.T) {
	br := newAttachmentEnv(t)
	got, err := br.CheckAttachments(context.Background(), 1, []string{"1", "2"})
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	a := got[0]
	if a.AssetID != "1" || a.Name != "参考.png" || a.MimeType != "image/png" || a.Width != 800 || a.Height != 600 {
		t.Errorf("展示信息: %+v", a)
	}
	if a.URL != "/files/u1/a.png" {
		t.Errorf("记录的是稳定地址（不是签名直链）: %q", a.URL)
	}
	if a.Image.Data != "QUJD" || a.Image.MimeType != "image/png" || a.Image.URL != "" {
		t.Errorf("本地存储：交给模型的是 base64: %+v", a.Image)
	}
	if got[1].Name != "图片 2" {
		t.Errorf("没有原文件名就叫「图片 N」: %q", got[1].Name)
	}
}

func TestCheckAttachments_RemoteStorageGivesSignedURLToModel(t *testing.T) {
	br := newAttachmentEnv(t, true)
	got, err := br.CheckAttachments(context.Background(), 1, []string{"1", "3"})
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if got[0].Image.URL != "https://cdn.example.com/1?sig=x" || got[0].Image.Data != "" {
		t.Errorf("对象存储：给模型的是地址: %+v", got[0].Image)
	}
	if got[0].URL != "/files/u1/a.png" {
		t.Errorf("落库的仍是稳定地址，不是签名直链: %q", got[0].URL)
	}
	if got[1].Image.URL == "" {
		t.Errorf("对象存储的大图不受 5 MB 限制: %+v", got[1])
	}
}

func TestCheckAttachments_Rejects(t *testing.T) {
	br := newAttachmentEnv(t)
	for name, c := range map[string]struct {
		ids  []string
		want string
	}{
		"不是数字":        {[]string{"1", "abc"}, "图 2：图片素材无效"},
		"素材不存在":       {[]string{"99"}, "图 1：图片素材不存在或已删除"},
		"格式不支持":       {[]string{"4"}, "图 1：图片格式 image/svg+xml 不支持查看"},
		"本地存储超过 5 MB": {[]string{"1", "3"}, "图 2：图片太大"},
		"视频不能当图片附件":   {[]string{"6"}, "图 1：图片格式 video/mp4 不支持查看"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := br.CheckAttachments(context.Background(), 1, c.ids)
			if msg := invalidMsg(t, err); !strings.Contains(msg, c.want) {
				t.Errorf("want %q, got %q", c.want, msg)
			}
		})
	}
}

func TestCheckAttachments_OtherUsersAssetIsNotFound(t *testing.T) {
	br := newAttachmentEnv(t)
	_, err := br.CheckAttachments(context.Background(), 2, []string{"1"})
	if msg := invalidMsg(t, err); !strings.Contains(msg, "图 1：图片素材不存在或已删除") {
		t.Errorf("别人的素材和不存在一样：%q", msg)
	}
}

func TestCheckAttachments_LimitsDedupeAndEmpty(t *testing.T) {
	br := newAttachmentEnv(t)
	if got, err := br.CheckAttachments(context.Background(), 1, nil); err != nil || len(got) != 0 {
		t.Errorf("没有附件：空结果不报错: %+v %v", got, err)
	}
	ten := []string{"1", "1", "1", "1", "1", "1", "1", "1", "1", "1"}
	if _, err := br.CheckAttachments(context.Background(), 1, ten); !strings.Contains(invalidMsg(t, err), "一条消息最多 9 张图片") {
		t.Errorf("超过 9 张: %v", err)
	}
	got, err := br.CheckAttachments(context.Background(), 1, []string{"2", "1", "2"})
	if err != nil || len(got) != 2 || got[0].AssetID != "2" || got[1].AssetID != "1" {
		t.Errorf("同一个素材只留一次，保持首次出现的顺序: %+v %v", got, err)
	}
	if _, err := br.CheckAttachments(context.Background(), 0, []string{"1"}); err == nil {
		t.Error("缺 user_id 要拒绝")
	}
}
