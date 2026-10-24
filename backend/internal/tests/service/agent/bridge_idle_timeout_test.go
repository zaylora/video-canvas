package agent_test

import (
	"context"
	"testing"
	"time"

	"video-canvas/internal/provider/modelcfg"
)

// 模型上配的「上游响应超时」要交给网关：不同模型的首字耗时差很多，超时不能只有一个全局值。
func TestProxyModel_PassesModelIdleTimeoutToGateway(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{"model":"x","messages":[{"role":"user","content":"拆分镜"}]}`)

	t.Run("模型配了就用模型的", func(t *testing.T) {
		b := newBridgeEnv(t)
		b.reg.snap.Model.IdleTimeout = modelcfg.Duration(150 * time.Second)
		tok, _ := b.token(t, "all")
		if err := b.br.ProxyModel(ctx, tok, body, func([]byte) {}); err != nil {
			t.Fatal(err)
		}
		if got := b.str.tg.IdleTimeout; got != 150*time.Second {
			t.Errorf("目标的 IdleTimeout=%v，应为模型配置的 150s", got)
		}
	})

	t.Run("没配就是 0，由网关取默认值", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		if err := b.br.ProxyModel(ctx, tok, body, func([]byte) {}); err != nil {
			t.Fatal(err)
		}
		if got := b.str.tg.IdleTimeout; got != 0 {
			t.Errorf("目标的 IdleTimeout=%v，没配应为 0", got)
		}
	})
}
