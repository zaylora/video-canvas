package service_test

import (
	"context"
	"errors"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/service"
)

func TestDramaRejectsEmptySource(t *testing.T) {
	s := service.NewDramaService(service.DramaDeps{})
	_, err := s.Create(context.Background(), 1, &model.CreateDramaReq{Text: "", StyleKey: "custom", StyleCustom: "写实"})
	var business *errcode.Error
	if !errors.As(err, &business) || business.Code != 67002 {
		t.Fatalf("empty source: %v", err)
	}
}
