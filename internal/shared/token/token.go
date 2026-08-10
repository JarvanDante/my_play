// Package token 播放签名: HMAC-SHA256(code|site|exp)。与 my_media 的 playsign 保持一致。
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

// Sign 计算签名。payload = code|site|exp。
func Sign(secret, code, site string, exp int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%s|%s|%d", code, site, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify 校验签名与有效期。
func Verify(secret, code, site string, exp int64, sig string) error {
	if exp <= 0 || sig == "" {
		return gerror.New("缺少播放凭证")
	}
	if time.Now().Unix() > exp {
		return gerror.New("播放凭证已过期")
	}
	want := Sign(secret, code, site, exp)
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return gerror.New("播放凭证无效")
	}
	return nil
}
