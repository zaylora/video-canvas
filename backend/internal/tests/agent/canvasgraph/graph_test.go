package canvasgraph_test

import (
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

func TestParseRoundTripKeepsUnknownFields(t *testing.T) {
	g := mustParse(t, basePayload)
	got := marshal(t, g)
	for _, want := range []string{`"futureField":{"a":1}`, `"viewport":{"x":10,"y":20,"zoom":0.8}`, `"extra":"keep-me"`} {
		if !strings.Contains(got, want) {
			t.Errorf("往返后丢了 %s\n%s", want, got)
		}
	}
}

func TestParseEmptyAndInvalid(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		wantErr  bool
	}{
		{"空对象", `{}`, false},
		{"空串", ``, false},
		{"null", `null`, false},
		{"数组", `[]`, true},
		{"nodes 不是数组", `{"nodes":1}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := canvasgraph.Parse([]byte(tc.in))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, canvasgraph.ErrPayload) {
				t.Errorf("应包装 ErrPayload: %v", err)
			}
			if err == nil && len(g.Nodes) != 0 {
				t.Errorf("应为空图")
			}
		})
	}
}

func TestCloneIsDeep(t *testing.T) {
	g := mustParse(t, basePayload)
	c := g.Clone()
	node(t, c, "n_script")["data"].(map[string]any)["label"] = "改过"
	if node(t, g, "n_script")["data"].(map[string]any)["label"] != "剧本" {
		t.Fatal("Clone 不是深拷贝")
	}
}
