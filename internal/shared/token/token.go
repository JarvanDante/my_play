// Package token 播放签名 v2: HMAC-SHA256(secret, code|site|exp|d|ip)。
// 与 my_media 的 playsign 保持一致; 支持主/副双密钥平滑轮换。
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

func sign(secret, code, site string, exp int64, d int, ip string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%s|%s|%d|%d|%s", code, site, exp, d, ip)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify 依次尝试多个密钥(轮换期主+副), 任一匹配即通过。
func Verify(secrets []string, code, site string, exp int64, d int, ip, sig string) error {
	if exp <= 0 || sig == "" {
		return gerror.New("缺少播放凭证")
	}
	if time.Now().Unix() > exp {
		return gerror.New("播放凭证已过期")
	}
	for _, sec := range secrets {
		if sec == "" {
			continue
		}
		if hmac.Equal([]byte(sign(sec, code, site, exp, d, ip)), []byte(sig)) {
			return nil
		}
	}
	return gerror.New("播放凭证无效")
}
