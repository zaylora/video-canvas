package imageproc

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"

	"video-canvas/internal/storage"
)

// aliyun 适配阿里云 OSS 的图片处理（image/）与视频截帧（video/snapshot）。
// 公开读桶直接拼 x-oss-process；私有读桶用 OSS SDK v2 的 V4 预签名，把 x-oss-process 签入。
// 视频截帧不能走 S3 协议（S3 协议下 x-oss-process 只支持 image/ 和 style/），所以必须用原生 SDK。
type aliyun struct {
	mu      sync.Mutex
	clients map[string]*oss.Client
}

func newAliyun() *aliyun { return &aliyun{clients: map[string]*oss.Client{}} }

func (*aliyun) Supports(_ Config, variant string) bool {
	return variant == VariantThumb || variant == VariantPoster
}

func (a *aliyun) VariantURL(ctx context.Context, in URLInput) (string, error) {
	if !a.Supports(in.Config, in.Variant) {
		return "", fmt.Errorf("%w: %q", ErrUnsupported, in.Variant)
	}
	// 1. 处理参数。截帧时间单位是毫秒；m_fast 取最近的关键帧，更快
	var process string
	if in.Variant == VariantThumb {
		process = fmt.Sprintf("image/resize,l_%d/format,%s", in.Config.Width, in.Config.Format)
	} else {
		process = fmt.Sprintf("video/snapshot,t_%s,f_jpg,w_%d,m_fast",
			strconv.FormatInt(int64(in.Config.TimeSec*1000), 10), in.Config.Width)
	}

	// 2. 公开读：不需要签名
	if isPublic(in.Spec) {
		return "https://" + in.Config.Domain + "/" + objectURLPath(in.Spec, in.Key) + "?x-oss-process=" + process, nil
	}

	// 3. 私有读：V4 预签名，x-oss-process 作为查询参数参与签名（不能在签好的地址后面追加）
	if err := needCredentials(in.Spec); err != nil {
		return "", err
	}
	client := a.client(in)
	res, err := client.Presign(ctx, &oss.GetObjectRequest{
		Bucket:  oss.Ptr(in.Spec.Bucket),
		Key:     oss.Ptr(storage.ObjectPath(in.Spec.PathPrefix, in.Key)),
		Process: oss.Ptr(process),
	}, oss.PresignExpires(ttlOf(in.TTL)))
	if err != nil {
		return "", fmt.Errorf("阿里云签名失败: %w", err)
	}
	return res.URL, nil
}

// client 返回（并缓存）按访问域名与凭证创建的 OSS 客户端。访问域名按自定义域名（CNAME）处理，
// 默认的 bucket.oss-xx.aliyuncs.com 域名同样适用。预签名只在本地计算，不发网络请求。
func (a *aliyun) client(in URLInput) *oss.Client {
	id := in.Spec.Region + "|" + in.Config.Domain + "|" + in.Spec.AccessKey + "|" + in.Spec.SecretKey
	a.mu.Lock()
	defer a.mu.Unlock()
	if c, ok := a.clients[id]; ok {
		return c
	}
	cfg := oss.LoadDefaultConfig().
		WithRegion(in.Spec.Region).
		WithEndpoint("https://" + in.Config.Domain).
		WithUseCName(true).
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(in.Spec.AccessKey, in.Spec.SecretKey))
	c := oss.NewClient(cfg)
	a.clients[id] = c
	return c
}
