package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
)

// TestCanvasTransfer_ExportThenImport 走一遍完整往返：用户 1 导出一张内容丰富的画布，文件经 JSON 序列化后由用户 9 导入。
func TestCanvasTransfer_ExportThenImport(t *testing.T) {
	// 1. 导出方：importPayload 用到主素材 101（也是产物和导演台缩略图）、参考图 102、角色卡 7（素材 201/202/203）
	cardHex := idcodec.Encode(7)
	src := newTransferEnv(t, importPayload(cardHex))
	src.addCard(7)
	src.assets.assets[102] = &model.Asset{
		ID: 102, UserID: transferUser, Kind: "image", MimeType: "image/png", ByteSize: 102,
		FileName: "参考.png", Sha256: transferHash(102), StorageKey: "u1/102",
	}
	canvasBefore := cloneCanvas(src.canvases.canvases[5])
	assetsBefore := cloneAssets(src.assets.assets)

	exported, err := src.svc.Export(context.Background(), transferUser, 5)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if exported.Report.SkippedAssets != 0 {
		t.Fatalf("不应跳过素材: %+v", exported.Report)
	}
	raw, err := json.Marshal(exported.Document)
	if err != nil {
		t.Fatal(err)
	}

	// 2. 导入方：另一个用户导入这份文件
	dst := newDefaultImportEnv(t)
	res, err := dst.svc.Import(context.Background(), importUser, raw)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	// 3. 新画布归导入者、revision 1；素材全部复制，角色卡重建
	if len(dst.tx.st.canvases) != 1 {
		t.Fatalf("应建 1 张画布，实际 %d", len(dst.tx.st.canvases))
	}
	p := dst.tx.st.canvases[0]
	if p.UserID != importUser || p.Revision != 1 || res.Canvas.Revision != 1 || uint64(res.Canvas.ID) != p.ID {
		t.Errorf("画布归属 / 版本不对: user=%d rev=%d res=%+v", p.UserID, p.Revision, res.Canvas)
	}
	if a := res.Report.Assets; a.Total != 5 || a.Copied != 5 || len(a.Missing) != 0 || res.Report.Characters.Created != 1 {
		t.Errorf("报告不对: %+v", res.Report)
	}

	// 4. 新画布内容里不能残留原环境的素材 ID、角色卡 ID 和文件地址
	payload := string(p.PayloadJSON)
	for _, id := range []uint64{101, 102, 201, 202, 203} {
		if q := fmt.Sprintf("%q", fmt.Sprint(id)); strings.Contains(payload, q) {
			t.Errorf("新画布残留原素材 ID %s: %s", q, payload)
		}
	}
	if strings.Contains(payload, cardHex) || strings.Contains(payload, "/files/u1/") {
		t.Errorf("新画布残留原角色卡 ID 或原文件地址: %s", payload)
	}
	nodes := importedNodes(t, p)
	if nodes["n2"]["characterId"] != idcodec.Encode(dst.cardsOf(importUser)[0].ID) {
		t.Errorf("角色卡节点应指向导入者的新卡: %v", nodes["n2"])
	}

	// 5. 导出方的画布与素材一点没变
	if !reflect.DeepEqual(cloneCanvas(src.canvases.canvases[5]), canvasBefore) {
		t.Error("导出不应改动原画布")
	}
	if !reflect.DeepEqual(cloneAssets(src.assets.assets), assetsBefore) {
		t.Error("往返不应改动原用户的素材")
	}
}

// cloneCanvas 深拷贝一行画布，用于前后对比。
func cloneCanvas(p *model.CanvasProject) model.CanvasProject {
	c := *p
	c.PayloadJSON = datatypes.JSON(bytes.Clone(p.PayloadJSON))
	return c
}

// cloneAssets 深拷贝素材表，用于前后对比。
func cloneAssets(m map[uint64]*model.Asset) map[uint64]model.Asset {
	out := make(map[uint64]model.Asset, len(m))
	for id, a := range m {
		out[id] = *a
	}
	return out
}
