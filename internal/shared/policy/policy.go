// Package policy 从 my_media 定时拉取站点防盗链策略并缓存。
package policy

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

type Policy struct {
	SiteCode         string `json:"site_code"`
	RefererWhitelist string `json:"referer_whitelist"`
	UaBlacklist      string `json:"ua_blacklist"`
	TokenTtlSec      int    `json:"token_ttl_sec"`
	Status           int    `json:"status"`
}

var (
	mu    sync.RWMutex
	table = map[string]*Policy{}
)

// Get 返回站点策略; 无策略或停用返回 nil(不做额外限制)。
func Get(site string) *Policy {
	mu.RLock()
	defer mu.RUnlock()
	p := table[site]
	if p == nil || p.Status != 1 {
		return nil
	}
	return p
}

// CheckReferer 白名单为空放行; 否则 Referer 必须包含任一子串。
func CheckReferer(p *Policy, referer string) bool {
	if p == nil || strings.TrimSpace(p.RefererWhitelist) == "" {
		return true
	}
	for _, w := range strings.Split(p.RefererWhitelist, ",") {
		w = strings.TrimSpace(w)
		if w != "" && strings.Contains(referer, w) {
			return true
		}
	}
	return false
}

// CheckUA 黑名单命中任一子串则拒绝。
func CheckUA(p *Policy, ua string) bool {
	if p == nil || strings.TrimSpace(p.UaBlacklist) == "" {
		return true
	}
	low := strings.ToLower(ua)
	for _, b := range strings.Split(p.UaBlacklist, ",") {
		b = strings.ToLower(strings.TrimSpace(b))
		if b != "" && strings.Contains(low, b) {
			return false
		}
	}
	return true
}

// StartSync 后台定时同步(media.base_url + X-Play-Token)。
func StartSync(ctx context.Context) {
	base := strings.TrimRight(g.Cfg().MustGet(ctx, "media.base_url", "").String(), "/")
	secret := g.Cfg().MustGet(ctx, "play.secret", "").String()
	interval := g.Cfg().MustGet(ctx, "media.sync_interval_sec", 60).Int()
	if base == "" || secret == "" {
		g.Log().Warning(ctx, "policy sync 未启用(media.base_url 或 play.secret 缺失)")
		return
	}
	if interval < 10 {
		interval = 10
	}
	go func() {
		for {
			fetch(ctx, base, secret)
			time.Sleep(time.Duration(interval) * time.Second)
		}
	}()
}

func fetch(ctx context.Context, base, secret string) {
	var out struct {
		Code int `json:"code"`
		Data struct {
			List []*Policy `json:"list"`
		} `json:"data"`
	}
	resp, err := g.Client().Timeout(5*time.Second).
		Header(map[string]string{"X-Play-Token": secret}).
		Get(ctx, base+"/gw/play/policies")
	if err != nil {
		g.Log().Warningf(ctx, "policy fetch: %v", err)
		return
	}
	defer resp.Close()
	if err := json.Unmarshal([]byte(resp.ReadAllString()), &out); err != nil {
		g.Log().Warningf(ctx, "policy decode: %v", err)
		return
	}
	if out.Code != 0 {
		g.Log().Warningf(ctx, "policy fetch code=%d", out.Code)
		return
	}
	next := make(map[string]*Policy, len(out.Data.List))
	for _, p := range out.Data.List {
		next[p.SiteCode] = p
	}
	mu.Lock()
	table = next
	mu.Unlock()
}
