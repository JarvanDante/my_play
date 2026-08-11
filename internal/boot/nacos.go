// Package boot 启动引导。Nacos 配置源接入(env 引导 + 本地兜底)。
package boot

import (
	"context"
	"strconv"
	"strings"

	nacos "github.com/gogf/gf/contrib/config/nacos/v2"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/genv"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

// InitNacosConfig 若设置了 NACOS_ADDR, 把 g.Cfg() 配置源切到 Nacos(引导信息只来自环境变量);
// 未设置 / 连接失败 / 该 dataId 未发布 —— 一律保留本地 config.yaml(平滑降级, 保证能起)。
func InitNacosConfig(ctx context.Context) {
	addr := strings.TrimSpace(genv.Get("NACOS_ADDR").String())
	if addr == "" {
		g.Log().Info(ctx, "NACOS_ADDR 未设置, 使用本地 config")
		return
	}
	host, port := parseAddr(addr)
	ns := genv.Get("NACOS_NAMESPACE", "dev").String()
	dataId := genv.Get("NACOS_DATA_ID", "my_play.yaml").String()
	group := genv.Get("NACOS_GROUP", "SERVICE").String()

	adapter, err := nacos.New(ctx, nacos.Config{
		ServerConfigs: []constant.ServerConfig{{IpAddr: host, Port: port}},
		ClientConfig: constant.ClientConfig{
			NamespaceId:         ns,
			Username:            genv.Get("NACOS_USER", "nacos").String(),
			Password:            genv.Get("NACOS_PASS", "nacos").String(),
			TimeoutMs:           5000,
			NotLoadCacheAtStart: true,
			LogDir:              "./temp/nacos/log",
			CacheDir:            "./temp/nacos/cache",
			LogLevel:            "warn",
		},
		ConfigParam: vo.ConfigParam{DataId: dataId, Group: group},
		Watch:       true,
	})
	if err != nil {
		g.Log().Warningf(ctx, "Nacos 配置初始化失败, 回退本地 config: %v", err)
		return
	}
	if !adapter.Available(ctx) {
		g.Log().Warningf(ctx, "Nacos 上无 %s(ns=%s)或不可达, 回退本地 config", dataId, ns)
		return
	}
	g.Cfg().SetAdapter(adapter)
	g.Log().Infof(ctx, "配置源已切到 Nacos: ns=%s dataId=%s group=%s", ns, dataId, group)
}

func parseAddr(a string) (string, uint64) {
	host, portStr := a, "8848"
	if i := strings.LastIndex(a, ":"); i >= 0 {
		host, portStr = a[:i], a[i+1:]
	}
	p, _ := strconv.ParseUint(portStr, 10, 64)
	if p == 0 {
		p = 8848
	}
	return host, p
}
