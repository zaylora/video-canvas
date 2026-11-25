package config

import "time"

// ReferenceCache 控制本地参考素材的临时 URL 准备，独立于应用默认存储。
type ReferenceCache struct {
	Enabled        bool          `mapstructure:"enabled"`
	StorageID      uint64        `mapstructure:"storage_id"`
	Prefix         string        `mapstructure:"prefix"`
	SignedTTL      time.Duration `mapstructure:"signed_ttl"`
	MaxBytes       int64         `mapstructure:"max_bytes"`
	PrepareTimeout time.Duration `mapstructure:"prepare_timeout"`
	UnknownGrace   time.Duration `mapstructure:"unknown_grace"`
}

// WithDefaults 为配置文件和直接构造配置的调用方补齐默认值。
func (c ReferenceCache) WithDefaults() ReferenceCache {
	if c.Prefix == "" {
		c.Prefix = "ref-cache/"
	}
	if c.SignedTTL == 0 {
		c.SignedTTL = 24 * time.Hour
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = 5_000_000_000
	}
	if c.PrepareTimeout == 0 {
		c.PrepareTimeout = 20 * time.Second
	}
	if c.UnknownGrace == 0 {
		c.UnknownGrace = 24 * time.Hour
	}
	return c
}
