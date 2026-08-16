// Package play 播放网关: token 验签 → 防盗链策略 → m3u8 重写/试看截断 / ts 302 预签名 → 统计。
package play

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	"my_play/internal/shared/cdn"
	"my_play/internal/shared/policy"
	"my_play/internal/shared/revoke"
	"my_play/internal/shared/stats"
	"my_play/internal/shared/store"
	"my_play/internal/shared/token"
)

// 允许的文件名: 字母数字._- ,防路径穿越。
var fileRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// validRelPath 允许 <file> 或 <dir>/<file>(一级子目录, 多码率档位 720p/480p),
// 每段仅字母数字._- ; 拒绝 .. 与多级目录, 防路径穿越。
func validRelPath(p string) bool {
	if p == "" || strings.Contains(p, "..") {
		return false
	}
	parts := strings.Split(p, "/")
	if len(parts) < 1 || len(parts) > 2 {
		return false
	}
	for _, seg := range parts {
		if seg == "" || !fileRe.MatchString(seg) {
			return false
		}
	}
	return true
}

func secrets(r *ghttp.Request) []string {
	ctx := r.Context()
	return []string{
		g.Cfg().MustGet(ctx, "play.secret", "").String(),
		g.Cfg().MustGet(ctx, "play.secret_secondary", "").String(),
	}
}

// Healthz 探活。
func Healthz(r *ghttp.Request) {
	r.Response.WriteJson(g.Map{"ok": true, "service": "my_play"})
}

// Hls 统一入口: GET /hls/:code/:file?e=&s=&sig=[&d=][&i=]
func Hls(r *ghttp.Request) {
	ctx := r.Context()
	code := r.Get("code").String()
	file := strings.TrimLeft(r.Get("file").String(), "/")
	e := r.Get("e").Int64()
	site := r.Get("s").String()
	sig := r.Get("sig").String()
	d := r.Get("d").Int()
	ip := r.Get("i").String()
	iat := r.Get("t").Int64()

	if code == "" || !validRelPath(file) {
		r.Response.WriteStatus(http.StatusBadRequest, "bad request")
		return
	}
	secs := secrets(r)
	if secs[0] == "" {
		r.Response.WriteStatus(http.StatusInternalServerError, "play.secret 未配置")
		return
	}
	if err := token.Verify(secs, code, site, e, d, ip, iat, sig); err != nil {
		r.Response.WriteStatus(http.StatusForbidden, err.Error())
		return
	}
	// IP 绑定(token 内嵌了 ip 才校验)
	if ip != "" && r.GetClientIp() != ip {
		r.Response.WriteStatus(http.StatusForbidden, "播放凭证与来源不符")
		return
	}
	// 链接一键失效闸: 令牌签发时间早于失效基线即拒
	if revoke.Revoked(site, code, iat) {
		r.Response.WriteStatus(http.StatusForbidden, "链接已失效")
		return
	}
	// 站点防盗链策略
	pol := policy.Get(site)
	if !policy.CheckReferer(pol, r.Header.Get("Referer")) {
		r.Response.WriteStatus(http.StatusForbidden, "来源不被允许")
		return
	}
	if !policy.CheckUA(pol, r.Header.Get("User-Agent")) {
		r.Response.WriteStatus(http.StatusForbidden, "客户端不被允许")
		return
	}

	m, err := store.Get(ctx)
	if err != nil {
		g.Log().Errorf(ctx, "minio init: %v", err)
		r.Response.WriteStatus(http.StatusBadGateway, "storage unavailable")
		return
	}
	key := fmt.Sprintf("media/hls/%s/%s", code, file)

	// 播放清单: 拉取并重写(可试看截断)
	if strings.HasSuffix(file, ".m3u8") {
		raw, err := m.Fetch(ctx, key)
		if err != nil {
			g.Log().Warningf(ctx, "fetch %s: %v", key, err)
			r.Response.WriteStatus(http.StatusNotFound, "playlist not found")
			return
		}
		q := fmt.Sprintf("e=%d&s=%s&t=%d&sig=%s", e, site, iat, sig)
		if d > 0 {
			q += fmt.Sprintf("&d=%d", d)
		}
		if ip != "" {
			q += "&i=" + ip
		}
		// 仅顶层清单(master.m3u8/旧 index.m3u8)计一次播放; 多码率子清单(720p/index.m3u8)不重复计数
		if !strings.Contains(file, "/") {
			stats.AddPlay(site, code)
		}
		r.Response.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		r.Response.Header().Set("Cache-Control", "no-store")
		r.Response.Write(rewriteM3u8(string(raw), q, d))
		return
	}

	// 封面图走网关代理直出, 避免 302 到 host.docker.internal 预签名导致后台 <img> 破图。
	if isImageFile(file) {
		obj, info, err := m.StreamGet(ctx, key)
		if err != nil {
			g.Log().Warningf(ctx, "cover get %s: %v", key, err)
			r.Response.WriteStatus(http.StatusNotFound, "cover not found")
			return
		}
		defer obj.Close()
		data, err := io.ReadAll(obj)
		if err != nil {
			g.Log().Warningf(ctx, "cover read %s: %v", key, err)
			r.Response.WriteStatus(http.StatusBadGateway, "cover read failed")
			return
		}
		ct := info.ContentType
		if ct == "" {
			ct = "image/jpeg"
		}
		r.Response.Header().Set("Content-Type", ct)
		r.Response.Header().Set("Cache-Control", "private, max-age=120")
		r.Response.Write(data)
		return
	}

	// 分片回源: 按 serve_mode 选择 presign(302预签名) / proxy(代理直出) / cdn(302到CDN签名URL)
	stats.AddSeg(site, code)
	r.Response.Header().Set("Cache-Control", "no-store")

	switch strings.ToLower(g.Cfg().MustGet(ctx, "play.serve_mode", "presign").String()) {
	case "proxy":
		obj, info, err := m.StreamGet(ctx, key)
		if err != nil {
			g.Log().Warningf(ctx, "proxy get %s: %v", key, err)
			r.Response.WriteStatus(http.StatusNotFound, "segment not found")
			return
		}
		defer obj.Close()
		data, err := io.ReadAll(obj)
		if err != nil {
			g.Log().Warningf(ctx, "proxy read %s: %v", key, err)
			r.Response.WriteStatus(http.StatusBadGateway, "segment read failed")
			return
		}
		ct := info.ContentType
		if ct == "" {
			ct = "video/mp2t"
		}
		r.Response.Header().Set("Content-Type", ct)
		r.Response.Write(data)
		return

	case "cdn":
		base := strings.TrimRight(g.Cfg().MustGet(ctx, "play.cdn.base_url", "").String(), "/")
		pkey := g.Cfg().MustGet(ctx, "play.cdn.private_key", "").String()
		if base != "" && pkey != "" {
			uri := "/" + strings.TrimLeft(key, "/")
			u := cdn.SignTypeA(base, uri, time.Now().Unix(), pkey)
			r.Response.RedirectTo(u, http.StatusFound)
			return
		}
		g.Log().Warning(ctx, "serve_mode=cdn 但 play.cdn.base_url/private_key 未配置, 回退 presign")
		fallthrough

	default: // presign
		u, err := m.PresignGet(ctx, key)
		if err != nil {
			g.Log().Warningf(ctx, "presign %s: %v", key, err)
			r.Response.WriteStatus(http.StatusNotFound, "segment not found")
			return
		}
		r.Response.RedirectTo(u, http.StatusFound)
	}
}

// rewriteM3u8 给每个 URI 行追加 token; previewSec>0 时按 EXTINF 累计时长截断(试看)。
func rewriteM3u8(body, query string, previewSec int) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	var cum float64
	truncated := false

	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		// 试看: 到达时长上限后丢弃剩余分片(保留尾部非分片标签由 ENDLIST 统一收尾)
		if truncated {
			break
		}
		if t == "" || strings.HasPrefix(t, "#") {
			if previewSec > 0 && strings.HasPrefix(t, "#EXTINF:") {
				dur := parseExtinf(t)
				if cum+dur > float64(previewSec) && cum > 0 {
					truncated = true
					break
				}
				cum += dur
			}
			out = append(out, lines[i])
			continue
		}
		// URI 行(分片/子清单): 历史绝对直链不重写
		if strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
			out = append(out, lines[i])
			continue
		}
		if strings.Contains(t, "?") {
			out = append(out, t+"&"+query)
		} else {
			out = append(out, t+"?"+query)
		}
	}

	res := strings.Join(out, "\n")
	if truncated && !strings.Contains(res, "#EXT-X-ENDLIST") {
		res = strings.TrimRight(res, "\n") + "\n#EXT-X-ENDLIST\n"
	}
	return res
}

func isImageFile(file string) bool {
	low := strings.ToLower(file)
	return strings.HasSuffix(low, ".jpg") || strings.HasSuffix(low, ".jpeg") ||
		strings.HasSuffix(low, ".png") || strings.HasSuffix(low, ".webp") ||
		strings.HasSuffix(low, ".gif")
}

func parseExtinf(line string) float64 {
	v := strings.TrimPrefix(line, "#EXTINF:")
	if idx := strings.IndexAny(v, ",\r"); idx >= 0 {
		v = v[:idx]
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return 0
	}
	return f
}
