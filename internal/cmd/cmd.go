// Package cmd 播放网关启动装配。
package cmd

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gcmd"

	"my_play/internal/controller/play"
	"my_play/internal/shared/policy"
	"my_play/internal/shared/stats"
)

// cors 播放器跨域(GET/HEAD 公共资源)。
func cors(r *ghttp.Request) {
	r.Response.CORSDefault()
	r.Middleware.Next()
}

var Main = gcmd.Command{
	Name:  "main",
	Usage: "main",
	Brief: "my_play 播放网关(HLS 验签/防盗链/试看/统计)",
	Func: func(ctx context.Context, parser *gcmd.Parser) (err error) {
		policy.StartSync(ctx)
		stats.StartReporter(ctx)

		s := g.Server()
		s.Use(cors)
		s.BindHandler("GET:/healthz", play.Healthz)
		s.BindHandler("GET:/hls/:code/:file", play.Hls)
		s.Run()
		return nil
	},
}
