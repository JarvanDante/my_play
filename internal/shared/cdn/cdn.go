// Package cdn CDN 回源签名。当前实现阿里云 URL 鉴权 Type A。
package cdn

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
)

// SignTypeA 阿里云 CDN URL 鉴权 Type A:
//
//	auth_key = timestamp-rand-uid-md5hash
//	md5hash  = md5(URI-timestamp-rand-uid-privateKey)
//
// 有效时长由 CDN 控制台配置(timestamp 为签发时刻)。rand/uid 取 "0" 即可。
// base 形如 https://cdn.example.com ; uri 形如 /media/hls/xx/720p/seg0.ts
func SignTypeA(base, uri string, ts int64, privateKey string) string {
	const rand, uid = "0", "0"
	sstr := fmt.Sprintf("%s-%d-%s-%s-%s", uri, ts, rand, uid, privateKey)
	sum := md5.Sum([]byte(sstr))
	authKey := fmt.Sprintf("%d-%s-%s-%s", ts, rand, uid, hex.EncodeToString(sum[:]))
	return fmt.Sprintf("%s%s?auth_key=%s", base, uri, authKey)
}
