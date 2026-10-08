package model_test

import (
	"encoding/json"
	"testing"

	. "video-canvas/internal/model"
)

// TestUpdateShowcaseItemReq_PosterAssetID 验证“没传 / 传 null / 传数字”三种情形能被可靠区分，
// 这是 PUT 条目时“清空封面”与“不改封面”的唯一依据。
func TestUpdateShowcaseItemReq_PosterAssetID(t *testing.T) {
	zero, twelve := uint64(0), uint64(12)
	tests := []struct {
		name    string
		body    string
		wantSet bool
		wantID  *uint64
		wantErr bool
	}{
		{"没传该字段", `{"prompt":"x"}`, false, nil, false},
		{"显式传 null", `{"poster_asset_id":null}`, true, nil, false},
		{"传数字", `{"poster_asset_id":12}`, true, &twelve, false},
		{"传 0 也算传了", `{"poster_asset_id":0}`, true, &zero, false},
		{"传字符串报错", `{"poster_asset_id":"12"}`, false, nil, true},
		{"传负数报错", `{"poster_asset_id":-1}`, false, nil, true},
		{"传小数报错", `{"poster_asset_id":1.5}`, false, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req UpdateShowcaseItemReq
			err := json.Unmarshal([]byte(tt.body), &req)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("期望解析失败，实际 %+v", req.PosterAssetID)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if req.PosterAssetID.Set != tt.wantSet {
				t.Fatalf("Set 期望 %v，实际 %v", tt.wantSet, req.PosterAssetID.Set)
			}
			if (req.PosterAssetID.ID == nil) != (tt.wantID == nil) || (tt.wantID != nil && *req.PosterAssetID.ID != *tt.wantID) {
				t.Fatalf("ID 期望 %v，实际 %v", tt.wantID, req.PosterAssetID.ID)
			}
		})
	}
}
