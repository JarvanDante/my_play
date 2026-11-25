// Package token 播放签名 v4: HMAC-SHA256(secret, code|site|exp|d|ip|iat|p)，p=用途(img/hls)；无 p 的 v3 旧签名仅过渡期兼容。
// 与 my_media / my_service 的签发保持一致; 支持主/副双密钥平滑轮换。
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

// 签名用途：封面/页图只能取图片，播放清单与分片只能走 hls。
const (
	PurposeImg = "img"
	PurposeHls = "hls"
)

func sign(secret, code, site string, exp int64, d int, ip string, iat int64, p string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	if p == "" {
		_, _ = fmt.Fprintf(mac, "%s|%s|%d|%d|%s|%d", code, site, exp, d, ip, iat)
	} else {
		_, _ = fmt.Fprintf(mac, "%s|%s|%d|%d|%s|%d|%s", code, site, exp, d, ip, iat, p)
	}
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify 依次尝试多个密钥(轮换期主+副), 任一匹配即通过；p 为空按 v3 旧格式校验。
func Verify(secrets []string, code, site string, exp int64, d int, ip string, iat int64, p, sig string) error {
	if exp <= 0 || sig == "" {
		return gerror.New("缺少播放凭证")
	}
	if p != "" && p != PurposeImg && p != PurposeHls {
		return gerror.New("播放凭证无效")
	}
	if time.Now().Unix() > exp {
		return gerror.New("播放凭证已过期")
	}
	for _, sec := range secrets {
		if sec == "" {
			continue
		}
		if hmac.Equal([]byte(sign(sec, code, site, exp, d, ip, iat, p)), []byte(sig)) {
			return nil
		}
	}
	return gerror.New("播放凭证无效")
}
