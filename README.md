# my_play · 统一播放(PaaS)

HLS 播放网关:token 验签 / m3u8 重写 / ts 短时效预签名 302。各站共用播放能力。
**不做**:转码(my_transcode)、媒资元数据(my_media)、对象上传配额(my_storage)、站点用户鉴权(子站)。

## 端口

`:8006`。无数据库、无状态,可水平扩。

## 播放链路(M1)

```text
子站/总后台 ── my_media 接口返回签名地址 ──► http://{gateway}/hls/{code}/index.m3u8?e=&s=&sig=
播放器 ──► my_play 验签 → 拉取 m3u8 → 每个分片补相同 token → 返回
播放器 ──► /hls/{code}/{seg}.ts?e=&s=&sig= → 验签 → 302 到 MinIO 预签名(默认 300s)
```

- token:`sig = HMAC-SHA256(secret, code|site|exp)`,签发方为 my_media(`shared/playsign`),`play.secret` 两边必须一致。
- 对象约定:桶 `my-media`,key `media/hls/{code}/{file}`(与 my_transcode 输出一致)。
- 历史直链(m3u8 内绝对地址)不重写,平滑兼容。

## 快速开始

```bash
cp manifest/config/config.example.yaml manifest/config/config.yaml   # 已有可跳过
go mod tidy    # 拉取 minio-go
go build ./... && go run main.go
curl http://127.0.0.1:8006/healthz
```

联调:改 my_media 配置 `play_gateway.base_url/secret` → 重启 my_media → 打开总后台媒资详情,play_url 应变为
`http://127.0.0.1:8006/hls/{code}/index.m3u8?e=...&s=admin&sig=...`,浏览器可直接播放。

## 文档

- [M1 的作用与边界](docs/why-m1.md)

## M2 规划

Referer/UA/IP 防盗链策略(按站点)、播放统计上报、密钥轮换、试看、CDN 对接。
