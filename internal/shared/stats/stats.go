// Package stats 播放统计: 内存累计, 定时批量上报 my_media。
package stats

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

type counter struct {
	plays   int64
	segReqs int64
}

var (
	mu  sync.Mutex
	buf = map[[2]string]*counter{}
)

// AddPlay m3u8 拉取计一次播放。
func AddPlay(site, asset string) { add(site, asset, 1, 0) }

// AddSeg 分片请求计数。
func AddSeg(site, asset string) { add(site, asset, 0, 1) }

func add(site, asset string, p, s int64) {
	if asset == "" {
		return
	}
	if site == "" {
		site = "-"
	}
	mu.Lock()
	k := [2]string{site, asset}
	c := buf[k]
	if c == nil {
		c = &counter{}
		buf[k] = c
	}
	c.plays += p
	c.segReqs += s
	mu.Unlock()
}

// StartReporter 定时把缓冲上报 my_media(失败则并回缓冲, 不丢数)。
func StartReporter(ctx context.Context) {
	base := strings.TrimRight(g.Cfg().MustGet(ctx, "media.base_url", "").String(), "/")
	secret := g.Cfg().MustGet(ctx, "play.secret", "").String()
	interval := g.Cfg().MustGet(ctx, "media.report_interval_sec", 30).Int()
	if base == "" || secret == "" {
		g.Log().Warning(ctx, "stats reporter 未启用(media.base_url 或 play.secret 缺失)")
		return
	}
	if interval < 5 {
		interval = 5
	}
	go func() {
		for {
			time.Sleep(time.Duration(interval) * time.Second)
			flush(ctx, base, secret)
		}
	}()
}

func flush(ctx context.Context, base, secret string) {
	mu.Lock()
	if len(buf) == 0 {
		mu.Unlock()
		return
	}
	batch := buf
	buf = map[[2]string]*counter{}
	mu.Unlock()

	type item struct {
		SiteCode  string `json:"site_code"`
		AssetCode string `json:"asset_code"`
		Plays     int64  `json:"plays"`
		SegReqs   int64  `json:"seg_reqs"`
	}
	items := make([]item, 0, len(batch))
	for k, c := range batch {
		items = append(items, item{SiteCode: k[0], AssetCode: k[1], Plays: c.plays, SegReqs: c.segReqs})
	}
	resp, err := g.Client().Timeout(5*time.Second).
		Header(map[string]string{"X-Play-Token": secret}).
		ContentJson().
		Post(ctx, base+"/gw/play/stats", g.Map{"items": items})
	if err != nil {
		g.Log().Warningf(ctx, "stats report: %v", err)
		restore(batch)
		return
	}
	defer resp.Close()
	if resp.StatusCode != 200 {
		g.Log().Warningf(ctx, "stats report status=%d", resp.StatusCode)
		restore(batch)
	}
}

func restore(batch map[[2]string]*counter) {
	mu.Lock()
	for k, c := range batch {
		cur := buf[k]
		if cur == nil {
			buf[k] = c
		} else {
			cur.plays += c.plays
			cur.segReqs += c.segReqs
		}
	}
	mu.Unlock()
}
