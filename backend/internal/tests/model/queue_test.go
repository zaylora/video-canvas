package model_test

import (
	"encoding/json"
	"testing"

	. "video-canvas/internal/model"
)

// 并发设置的键、默认值，和实例心跳里的统计格式：前端和心跳都按这个读。
func TestQueueConcurrencyDefaults(t *testing.T) {
	if SettingQueueConcurrency != "queue_concurrency" || DefaultQueueConcurrency != 16 {
		t.Fatalf("设置键或默认并发变了：%s %d", SettingQueueConcurrency, DefaultQueueConcurrency)
	}
	raw, _ := json.Marshal(QueueInstanceStats{Inflight: 3, Limit: 16})
	if string(raw) != `{"inflight":3,"limit":16}` {
		t.Fatalf("实例统计的 JSON 不对：%s", raw)
	}
}

// 总览的 JSON 形状是前端的契约：单个 queue 卡片，计数只有 4 项，不出现英文队列名。
func TestQueueOverviewShape(t *testing.T) {
	raw, _ := json.Marshal(QueueOverview{Queue: QueueCard{Paused: true, Concurrency: 8, HardCap: 64, Counts: QueueCounts{Waiting: 5, Running: 2, Retryable: 1, Discarded1h: 3}}})
	want := `{"queue":{"paused":true,"concurrency":8,"hard_cap":64,"counts":{"waiting":5,"running":2,"retryable":1,"discarded_1h":3}},"channels":null,"instances":null}`
	if string(raw) != want {
		t.Fatalf("总览 JSON 不符：\n%s\n%s", raw, want)
	}
}
