package fx_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	. "video-canvas/internal/provider/fx"
	"video-canvas/internal/service/pricingfake"
)

type fxTransport func(*http.Request) (*http.Response, error)

func (f fxTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFXRefreshSuccessFailureAndCancellation(t *testing.T) {
	store := pricingfake.New()
	s := New(store, nil)
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"成功", `{"base":"USD","date":"2026-10-30","rates":{"CNY":6.6529}}`, 200, true},
		{"HTTP 失败", ``, 503, false}, {"无效汇率", `{"base":"USD","date":"2026-10-30","rates":{"CNY":0}}`, 200, false}, {"日期错误", `{"base":"USD","date":"bad","rates":{"CNY":7}}`, 200, false}, {"JSON 错误", `x`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.SetClientForTest(&http.Client{Transport: fxTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://api.frankfurter.dev/v1/latest?from=USD&to=CNY" {
					t.Fatal(r.URL)
				}
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 10*time.Second {
					t.Fatal("需要 10 秒超时")
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
			})}, func() time.Time { return now })
			err := s.Refresh(context.Background())
			if (err == nil) != tc.ok {
				t.Fatal(err)
			}
			if store.Values[model.SettingPricingFXLast] != "6.6529" {
				t.Fatal("失败不得覆盖最近成功汇率", store.Values)
			}
		})
	}
	s.SetClientForTest(&http.Client{Transport: fxTransport(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })}, func() time.Time { return now })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Refresh(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
