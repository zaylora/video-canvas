package idcodec_test

import (
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/pkg/idcodec"
)

func TestRoundTrip(t *testing.T) {
	for _, id := range []uint64{1, 2, 12345, 1 << 53, 1<<64 - 1} {
		s := idcodec.Encode(id)
		if len(s) != idcodec.EncodedLen || strings.ToLower(s) != s {
			t.Fatalf("编码格式不对：%q", s)
		}
		got, ok := idcodec.Decode(s)
		if !ok || got != id {
			t.Fatalf("id=%d 往返得到 %d ok=%v", id, got, ok)
		}
	}
	if idcodec.Encode(1) == idcodec.Encode(2) {
		t.Fatal("不同 id 编码相同")
	}
}

func TestDecodeRejectsInvalid(t *testing.T) {
	valid := idcodec.Encode(7)
	tampered := valid[:31] + map[bool]string{true: "1", false: "0"}[valid[31] == '0']
	for name, s := range map[string]string{
		"空串":    "",
		"纯数字":   "1",
		"长度不对":  valid[:30],
		"非十六进制": strings.Repeat("z", 32),
		"被改过":   tampered,
	} {
		if id, ok := idcodec.Decode(s); ok {
			t.Errorf("%s 不应通过，得到 %d", name, id)
		}
	}
	// 其他密钥编码的串不能通过
	other := idcodec.Encode(7)
	idcodec.Init("another-key")
	defer idcodec.Init("video-canvas-dev-id-key")
	if _, ok := idcodec.Decode(other); ok {
		t.Error("不同密钥的串不应通过")
	}
}

func TestIDJSON(t *testing.T) {
	type wrap struct {
		ID  idcodec.ID  `json:"id"`
		Ref *idcodec.ID `json:"ref"`
	}
	ref := idcodec.ID(9)
	b, err := json.Marshal(wrap{ID: 5, Ref: &ref})
	if err != nil {
		t.Fatal(err)
	}
	var got wrap
	if err := json.Unmarshal(b, &got); err != nil || got.ID != 5 || got.Ref == nil || *got.Ref != 9 {
		t.Fatalf("往返失败：%s %+v %v", b, got, err)
	}
	if b, _ := json.Marshal(wrap{}); string(b) != `{"id":null,"ref":null}` {
		t.Fatalf("零值应输出 null：%s", b)
	}
	for _, in := range []string{`{"id":5}`, `{"id":"5"}`, `{"id":true}`} {
		if err := json.Unmarshal([]byte(in), &got); err == nil {
			t.Errorf("%s 应被拒绝", in)
		}
	}
	if err := json.Unmarshal([]byte(`{"id":""}`), &got); err != nil || got.ID != 0 {
		t.Errorf("空串应视为没有值：%v %d", err, got.ID)
	}
}
