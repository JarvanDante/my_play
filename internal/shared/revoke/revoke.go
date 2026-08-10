// Package revoke 从 my_media 定时拉取「链接失效闸」并缓存。
// 令牌签发时间 iat < not_before(站点级或资产级取较大者) 即视为失效。
package revoke

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

type item struct {
	SiteCode  string `json:"site_code"`
	AssetCode string `json:"asset_code"`
	NotBefore int64  `json:"not_before"`
}

var (
	mu    sync.RWMutex
	table = map[string]map[string]int64{} // site -> asset('*'|code) -> not_before
)

// NotBefore 返回站点/资产的失效基线。取四类记录的最大值:
// 本站整站(site/*)、本站资产(site/asset)、跨站整平台(*/*)、跨站资产(*/asset)。无记录返回 0。
func NotBefore(site, asset string) int64 {
	mu.RLock()
	defer mu.RUnlock()
	var nb int64
	for _, sc := range [2]string{site, "*"} {
		m := table[sc]
		if m == nil {
			continue
		}
		if v := m["*"]; v > nb {
			nb = v
		}
		if v, ok := m[asset]; ok && v > nb {
			nb = v
		}
	}
	return nb
}

// Revoked iat 早于失效基线即为失效。iat<=0(无签发时间)且存在任一失效闸时也判失效。
func Revoked(site, asset string, iat int64) bool {
	nb := NotBefore(site, asset)
	if nb <= 0 {
		return false
	}
	return iat < nb
}

// StartSync 后台定时同步(media.base_url + X-Play-Token)。
func StartSync(ctx context.Context) {
	base := strings.TrimRight(g.Cfg().MustGet(ctx, "media.base_url", "").String(), "/")
	secret := g.Cfg().MustGet(ctx, "play.secret", "").String()
	interval := g.Cfg().MustGet(ctx, "media.revoke_sync_interval_sec", 15).Int()
	if base == "" || secret == "" {
		g.Log().Warning(ctx, "revoke sync 未启用(media.base_url 或 play.secret 缺失)")
		return
	}
	if interval < 5 {
		interval = 5
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
			List []item `json:"list"`
		} `json:"data"`
	}
	resp, err := g.Client().Timeout(5*time.Second).
		Header(map[string]string{"X-Play-Token": secret}).
		Get(ctx, base+"/gw/play/revokes")
	if err != nil {
		g.Log().Warningf(ctx, "revoke fetch: %v", err)
		return
	}
	defer resp.Close()
	if err := json.Unmarshal([]byte(resp.ReadAllString()), &out); err != nil {
		g.Log().Warningf(ctx, "revoke decode: %v", err)
		return
	}
	if out.Code != 0 {
		g.Log().Warningf(ctx, "revoke fetch code=%d", out.Code)
		return
	}
	next := make(map[string]map[string]int64, len(out.Data.List))
	for _, it := range out.Data.List {
		asset := it.AssetCode
		if asset == "" {
			asset = "*"
		}
		if next[it.SiteCode] == nil {
			next[it.SiteCode] = map[string]int64{}
		}
		next[it.SiteCode][asset] = it.NotBefore
	}
	mu.Lock()
	table = next
	mu.Unlock()
}
