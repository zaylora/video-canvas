package storage_test

import (
	"errors"
	"strings"
	"testing"

	. "video-canvas/internal/storage"
)

const r2Account = "0123456789abcdef0123456789abcdef"

func TestSpecNormalize(t *testing.T) {
	tests := []struct {
		name    string
		in      Spec
		wantErr string // 非空表示期望 ErrInvalidSpec 且消息包含它
		check   func(t *testing.T, got Spec)
	}{
		{
			name: "OSS 按地域推导 endpoint，强制 HTTPS 与虚拟主机寻址",
			in:   Spec{Provider: ProviderAliyunOSS, Region: "cn-hangzhou", Bucket: "vc-bucket", UseSSL: false},
			check: func(t *testing.T, got Spec) {
				if got.Endpoint != "oss-cn-hangzhou.aliyuncs.com" || !got.UseSSL || got.Addressing != AddressingVirtual {
					t.Errorf("实际 %+v", got)
				}
			},
		},
		{
			name:    "COS 桶名缺少 APPID 后缀",
			in:      Spec{Provider: ProviderTencentCOS, Region: "ap-guangzhou", Bucket: "canvas"},
			wantErr: "APPID",
		},
		{
			name: "COS 桶名带 APPID",
			in:   Spec{Provider: ProviderTencentCOS, Region: "ap-guangzhou", Bucket: "canvas-1250000000"},
			check: func(t *testing.T, got Spec) {
				if got.Endpoint != "cos.ap-guangzhou.myqcloud.com" || !got.UseSSL || got.Addressing != AddressingVirtual {
					t.Errorf("实际 %+v", got)
				}
			},
		},
		{
			name: "R2 用 Account ID 推导 endpoint，region 固定 auto，path 寻址",
			in:   Spec{Provider: ProviderR2, AccountID: r2Account, Region: "us-east-1", Bucket: "vc-bucket"},
			check: func(t *testing.T, got Spec) {
				if got.Endpoint != r2Account+".r2.cloudflarestorage.com" || got.Region != "auto" || got.Addressing != AddressingPath || !got.UseSSL {
					t.Errorf("实际 %+v", got)
				}
			},
		},
		{
			name:    "R2 Account ID 不是 32 位十六进制",
			in:      Spec{Provider: ProviderR2, AccountID: "abc", Bucket: "vc-bucket"},
			wantErr: "Account ID",
		},
		{
			name: "S3 按地域推导 endpoint，保留 UseSSL",
			in:   Spec{Provider: ProviderS3, Region: "us-east-1", Bucket: "vc-bucket", UseSSL: true},
			check: func(t *testing.T, got Spec) {
				if got.Endpoint != "s3.us-east-1.amazonaws.com" || !got.UseSSL || got.Addressing != AddressingAuto {
					t.Errorf("实际 %+v", got)
				}
			},
		},
		{
			name: "S3 自定义 endpoint：协议头决定是否 SSL 并被去掉",
			in:   Spec{Provider: ProviderS3, Endpoint: "http://minio.internal:9000", Bucket: "vc-bucket", UseSSL: true, Addressing: AddressingPath},
			check: func(t *testing.T, got Spec) {
				if got.Endpoint != "minio.internal:9000" || got.UseSSL || got.Addressing != AddressingPath {
					t.Errorf("实际 %+v", got)
				}
			},
		},
		{
			name:    "S3 既没有地域也没有 endpoint",
			in:      Spec{Provider: ProviderS3, Bucket: "vc-bucket"},
			wantErr: "地域",
		},
		{
			name:    "桶名为空",
			in:      Spec{Provider: ProviderS3, Region: "us-east-1"},
			wantErr: "Bucket",
		},
		{
			name:    "桶名含大写字符",
			in:      Spec{Provider: ProviderS3, Region: "us-east-1", Bucket: "VC_Bucket"},
			wantErr: "桶名",
		},
		{
			name:    "未知服务商",
			in:      Spec{Provider: "qiniu", Bucket: "vc-bucket"},
			wantErr: "服务商",
		},
		{
			name:    "local 不走对象存储配置",
			in:      Spec{Provider: ProviderLocal},
			wantErr: "服务商",
		},
		{
			name:    "公开域名必须是 http(s)",
			in:      Spec{Provider: ProviderS3, Region: "us-east-1", Bucket: "vc-bucket", PublicBaseURL: "ftp://cdn.example.com"},
			wantErr: "公开",
		},
		{
			name: "公开域名去掉末尾斜杠，前缀去掉首尾斜杠",
			in:   Spec{Provider: ProviderS3, Region: "us-east-1", Bucket: "vc-bucket", PublicBaseURL: "https://cdn.example.com/", PathPrefix: "/a/b/"},
			check: func(t *testing.T, got Spec) {
				if got.PublicBaseURL != "https://cdn.example.com" || got.PathPrefix != "a/b" {
					t.Errorf("实际 %+v", got)
				}
			},
		},
		{
			name:    "路径前缀含路径穿越",
			in:      Spec{Provider: ProviderS3, Region: "us-east-1", Bucket: "vc-bucket", PathPrefix: "../x"},
			wantErr: "前缀",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.in.Normalize()
			if tt.wantErr != "" {
				if !errors.Is(err, ErrInvalidSpec) || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("期望 ErrInvalidSpec 且含 %q，实际：%v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("不应报错：%v", err)
			}
			tt.check(t, got)
		})
	}
}

func TestPresetOf(t *testing.T) {
	tests := []struct {
		provider string
		direct   string
		ok       bool
	}{
		{ProviderS3, DirectPostPolicy, true},
		{ProviderAliyunOSS, DirectPostPolicy, true},
		{ProviderTencentCOS, DirectPostPolicy, true},
		{ProviderR2, DirectPresignedPut, true}, // R2 不支持 PostObject
		{ProviderLocal, "", false},
		{"unknown", "", false},
	}
	for _, tt := range tests {
		p, ok := PresetOf(tt.provider)
		if ok != tt.ok || (ok && p.DirectMethod != tt.direct) {
			t.Errorf("%s：ok=%v direct=%q，期望 ok=%v direct=%q", tt.provider, ok, p.DirectMethod, tt.ok, tt.direct)
		}
	}
}

func TestPresetRegions(t *testing.T) {
	for _, provider := range []string{ProviderAliyunOSS, ProviderTencentCOS, ProviderS3} {
		p, _ := PresetOf(provider)
		if len(p.Regions) == 0 {
			t.Errorf("%s 应提供地域列表", provider)
		}
		for _, r := range p.Regions {
			if r.ID == "" || r.Name == "" {
				t.Errorf("%s 的地域不完整：%+v", provider, r)
			}
		}
	}
	// R2 没有地域可选
	if p, _ := PresetOf(ProviderR2); len(p.Regions) != 0 {
		t.Errorf("R2 不应有地域列表：%+v", p.Regions)
	}
	// OSS / COS 的预设推导出的 endpoint 必须能通过各自的规范化（地域 id 与推导规则一致）
	for _, provider := range []string{ProviderAliyunOSS, ProviderTencentCOS, ProviderS3} {
		p, _ := PresetOf(provider)
		bucket := "vc-bucket"
		if provider == ProviderTencentCOS {
			bucket = "vc-1250000000"
		}
		for _, r := range p.Regions {
			if _, err := (Spec{Provider: provider, Region: r.ID, Bucket: bucket}).Normalize(); err != nil {
				t.Errorf("%s 地域 %s 应能规范化：%v", provider, r.ID, err)
			}
		}
	}
}
