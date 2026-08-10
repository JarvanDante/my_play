// Package play 播放网关 M1: token 验签 → m3u8 重写 / ts 302 预签名。
package play

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	"my_play/internal/shared/store"
	"my_play/internal/shared/token"
)

// 允许的文件名: 字母数字._- ,防路径穿越。
var fileRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func secret(r *ghttp.Request) string {
	return g.Cfg().MustGet(r.Context(), "play.secret", "").String()
}

// Healthz 探活。
func Healthz(r *ghttp.Request) {
	r.Response.WriteJson(g.Map{"ok": true, "service": "my_play"})
}

// Hls 统一入口: GET /hls/:code/:file?e=&s=&sig=
// file 为 index.m3u8 → 拉取并重写(每个分片补相同 token);
// 其余(.ts 等) → 302 到 MinIO 短时效预签名地址。
func Hls(r *ghttp.Request) {
	ctx := r.Context()
	code := r.Get("code").String()
	file := r.Get("file").String()
	e := r.Get("e").Int64()
	site := r.Get("s").String()
	sig := r.Get("sig").String()

	if code == "" || !fileRe.MatchString(file) || strings.Contains(file, "..") {
		r.Response.WriteStatus(http.StatusBadRequest, "bad request")
		return
	}
	sec := secret(r)
	if sec == "" {
		r.Response.WriteStatus(http.StatusInternalServerError, "play.secret 未配置")
		return
	}
	if err := token.Verify(sec, code, site, e, sig); err != nil {
		r.Response.WriteStatus(http.StatusForbidden, err.Error())
		return
	}

	m, err := store.Get(ctx)
	if err != nil {
		g.Log().Errorf(ctx, "minio init: %v", err)
		r.Response.WriteStatus(http.StatusBadGateway, "storage unavailable")
		return
	}
	key := fmt.Sprintf("media/hls/%s/%s", code, file)

	// 播放清单: 拉取并重写
	if strings.HasSuffix(file, ".m3u8") {
		raw, err := m.Fetch(ctx, key)
		if err != nil {
			g.Log().Warningf(ctx, "fetch %s: %v", key, err)
			r.Response.WriteStatus(http.StatusNotFound, "playlist not found")
			return
		}
		q := fmt.Sprintf("e=%d&s=%s&sig=%s", e, site, sig)
		r.Response.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		r.Response.Header().Set("Cache-Control", "no-store")
		r.Response.Write(rewriteM3u8(string(raw), q))
		return
	}

	// 分片: 302 到预签名
	u, err := m.PresignGet(ctx, key)
	if err != nil {
		g.Log().Warningf(ctx, "presign %s: %v", key, err)
		r.Response.WriteStatus(http.StatusNotFound, "segment not found")
		return
	}
	r.Response.Header().Set("Cache-Control", "no-store")
	r.Response.RedirectTo(u, http.StatusFound)
}

// rewriteM3u8 给清单里每个 URI 行(分片/子清单)追加 token 查询串。
func rewriteM3u8(body, query string) string {
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		// 绝对地址(历史数据直链)不重写, 保持可播
		if strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
			continue
		}
		if strings.Contains(t, "?") {
			lines[i] = t + "&" + query
		} else {
			lines[i] = t + "?" + query
		}
	}
	return strings.Join(lines, "\n")
}
