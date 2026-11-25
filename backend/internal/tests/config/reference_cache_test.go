package config_test

import (
	"testing"
	"time"

	"video-canvas/internal/config"
)

func TestReferenceCacheConfigDefaultsAndEnvironment(t *testing.T) {
	const path = "../../../configs/config.yaml"
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.Storage.ReferenceCache
	if c.Enabled || c.Prefix != "ref-cache/" || c.MaxBytes != 5_000_000_000 || c.SignedTTL != 24*time.Hour {
		t.Fatalf("默认配置应关闭临时素材功能，并提供部署参数：%+v", c)
	}
	t.Setenv("APP_STORAGE_REFERENCE_CACHE_ENABLED", "true")
	t.Setenv("APP_STORAGE_REFERENCE_CACHE_STORAGE_ID", "42")
	t.Setenv("APP_STORAGE_REFERENCE_CACHE_SIGNED_TTL", "2h")
	t.Setenv("APP_STORAGE_REFERENCE_CACHE_PREFIX", "ref-cache/test/")
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c = cfg.Storage.ReferenceCache
	if !c.Enabled || c.StorageID != 42 || c.SignedTTL != 2*time.Hour || c.Prefix != "ref-cache/test/" {
		t.Fatalf("部署环境变量未覆盖临时素材配置：%+v", c)
	}
}
