// Package play 播放网关: token 验签 → 防盗链策略 → m3u8 重写/试看截断 / ts 302 预签名 → 统计。
package play

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	"my_play/internal/shared/policy"
	"my_play/internal/shared/stats"
	"my_play/internal/shared/store"
	"my_play/internal/shared/token"
)

// 允许的文件名: 字母数字._- ,防路径穿越。
var fileRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

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
	file := r.Get("file").String()
	e := r.Get("e").Int64()
	site := r.Get("s").String()
	sig := r.Get("sig").String()
	d := r.Get("d").Int()
	ip := r.Get("i").String()

	if code == "" || !fileRe.MatchString(file) || strings.Contains(file, "..") {
		r.Response.WriteStatus(http.StatusBadRequest, "bad request")
		return
	}
	secs := secrets(r)
	if secs[0] == "" {
		r.Response.WriteStatus(http.StatusInternalServerError, "play.secret 未配置")
		return
	}
	if err := token.Verify(secs, code, site, e, d, ip, sig); err != nil {
		r.Response.WriteStatus(http.StatusForbidden, err.Error())
		return
	}
	// IP 绑定(token 内嵌了 ip 才校验)
	if ip != "" && r.GetClientIp() != ip {
		r.Response.WriteStatus(http.StatusForbidden, "播放凭证与来源不符")
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
		q := fmt.Sprintf("e=%d&s=%s&sig=%s", e, site, sig)
		if d > 0 {
			q += fmt.Sprintf("&d=%d", d)
		}
		if ip != "" {
			q += "&i=" + ip
		}
		stats.AddPlay(site, code)
		r.Response.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		r.Response.Header().Set("Cache-Control", "no-store")
		r.Response.Write(rewriteM3u8(string(raw), q, d))
		return
	}

	// 分片: 302 到预签名
	u, err := m.PresignGet(ctx, key)
	if err != nil {
		g.Log().Warningf(ctx, "presign %s: %v", key, err)
		r.Response.WriteStatus(http.StatusNotFound, "segment not found")
		return
	}
	stats.AddSeg(site, code)
	r.Response.Header().Set("Cache-Control", "no-store")
	r.Response.RedirectTo(u, http.StatusFound)
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
