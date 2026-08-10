// Package store MinIO 只读封装: 拉取 m3u8 + 为 ts 生成短时效预签名 GET。
package store

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"sync"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	miniogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Minio struct {
	client  *miniogo.Client
	bucket  string
	presign time.Duration
}

var (
	inst *Minio
	once sync.Once
	iErr error
)

// Get 惰性单例。
func Get(ctx context.Context) (*Minio, error) {
	once.Do(func() {
		endpoint := g.Cfg().MustGet(ctx, "minio.endpoint", "127.0.0.1:19000").String()
		accessKey := g.Cfg().MustGet(ctx, "minio.access_key", "minioadmin").String()
		secretKey := g.Cfg().MustGet(ctx, "minio.secret_key", "minioadmin").String()
		bucket := g.Cfg().MustGet(ctx, "minio.bucket", "my-media").String()
		useSSL := g.Cfg().MustGet(ctx, "minio.use_ssl", false).Bool()
		presignSec := g.Cfg().MustGet(ctx, "play.presign_expire_sec", 300).Int64()
		if presignSec <= 0 {
			presignSec = 300
		}
		cli, err := miniogo.New(endpoint, &miniogo.Options{
			Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
			Secure: useSSL,
		})
		if err != nil {
			iErr = fmt.Errorf("minio new: %w", err)
			return
		}
		inst = &Minio{client: cli, bucket: bucket, presign: time.Duration(presignSec) * time.Second}
	})
	return inst, iErr
}

// Fetch 读取对象全文(用于 m3u8, 体积小)。
func (m *Minio) Fetch(ctx context.Context, key string) ([]byte, error) {
	obj, err := m.client.GetObject(ctx, m.bucket, key, miniogo.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	return io.ReadAll(obj)
}

// PresignGet 生成短时效下载地址(用于 ts 302 回源)。
func (m *Minio) PresignGet(ctx context.Context, key string) (string, error) {
	u, err := m.client.PresignedGetObject(ctx, m.bucket, key, m.presign, url.Values{})
	if err != nil {
		return "", err
	}
	return u.String(), nil
}
