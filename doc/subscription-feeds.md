# Resin 多格式订阅输出

Resin 的“订阅输出”可以选择 Platform 的可路由节点，或直接选择一个或多个原始订阅作为数据源。两种来源需要二选一；原始订阅仍由 Resin 按各自的更新间隔刷新，Feed 不复制原始链接，客户端继续使用同一个 Feed URL 即可获得刷新后的节点。

## 创建 Feed

1. 打开管理页面的“订阅输出”。
2. 在“节点来源”中选择 Platform 或原始订阅来源。
3. 选择 Platform 时，Platform 的健康、出口 IP、延迟和熔断规则会继续生效；选择原始订阅时，Feed 直接输出所选已启用订阅中未淘汰的节点。
4. 选择默认格式和其他启用格式。
5. 保存后立即复制一次性显示的公开 Token。

如果源平台中的节点使用了 VPS 内部地址（例如 `socks5h://warp:1080`），打开“通过 Resin 中转”，填写客户端可访问的公网域名和 SOCKS5 监听端口（例如 `resin.example.com`、`2261`）。Feed 会输出一个指向 Resin 的 SOCKS5 节点，客户端不会看到或解析 `warp`；Resin 收到连接后按所选平台路由到内部节点。该模式要求源为节点平台，且该平台至少有一个可路由节点。

公开地址格式如下：

```text
https://你的域名/sub/<token>
https://你的域名/sub/<token>/clash-meta
https://你的域名/sub/<token>/singbox
https://你的域名/sub/<token>/v2ray-base64
https://你的域名/sub/<token>/uri
```

不带格式后缀时使用 Feed 的默认格式。同一 Feed 的格式地址共用 Token；不同 Feed 使用不同 Token。

## 格式边界

| 格式 | 适用客户端 | 第一版协议范围 |
| --- | --- | --- |
| `clash-meta` | Clash.Meta、Mihomo | HTTP、SOCKS、SS、VMess、Trojan、VLESS、Hysteria、Hysteria2、TUIC、WireGuard |
| `singbox` | sing-box | Resin 已解析的 sing-box outbound 原样输出 |
| `v2ray-base64` | v2rayN 等 | VMess、VLESS、Trojan、SS、Hysteria2、HTTP、HTTPS、非 TLS SOCKS5 |
| `uri` | 通用客户端 | 与 `v2ray-base64` 相同，返回纯文本 URI 行 |

不能可靠表达的协议按 Feed 的“不支持协议处理”执行：`skip` 跳过并通过 `X-Resin-Skipped-Count`、`X-Resin-Skipped-Types` 报告，`error` 直接拒绝本次输出。

## 安全和反代

Token 是读取凭证，拿到完整 URL 即可读取该 Feed，但不能访问管理 API。轮换 Token 会立即使旧 URL 返回 404。生产环境使用 HTTPS，并保留现有 OpenResty 转发：

```nginx
location ^~ / {
    proxy_pass http://127.0.0.1:2260;
}
```

公开响应带有 `ETag` 和 30 秒私有缓存头；客户端发送匹配的 `If-None-Match` 时返回 304。公开接口带有进程内的 IP/Token 双维度限流，超过 120 次/分钟会返回 429；该限流在单进程内生效，多实例部署时仍需在 OpenResty/WAF 层设置限流。

Token 放在 URL 路径中，OpenResty 默认访问日志可能记录完整地址。建议为 `/sub/` 单独关闭访问日志或配置脱敏规则；Token 泄露后应立即在管理页面轮换。

中转端口需要在 Resin 的“端点”中创建或启用 SOCKS5 监听，例如 `2261`，并在 VPS 防火墙放行该 TCP 端口。需要加密时，在端点中填写：

- 监听地址：`0.0.0.0`
- 监听端口：`2261`
- SOCKS5：开启；管理页面、HTTP 正向和 HTTP 反向按需关闭
- TLS：开启
- 证书：`/etc/resin/tls/fullchain.pem`
- 私钥：`/etc/resin/tls/privkey.pem`

证书路径必须是 Resin 容器内的路径，并确保私钥对容器内的 `resin` 用户可读。Feed 的 Resin 中转选项中填写公网域名、`2261`、TLS 和证书域名（SNI）。Clash Meta 和 sing-box 可以表达 TLS SOCKS5；标准 `socks5://` URI 没有统一的 TLS 参数，因此 URI/V2Ray 输出会跳过该中转节点并在响应头中报告跳过类型。

HTTP 反代只负责 `/sub/` 订阅地址；SOCKS5 客户端必须直连该 TCP 端口，不能通过普通 `location proxy_pass` 转换。

如果订阅为空，先检查 Platform 是否有可路由节点。节点必须已生成 outbound、完成出口和延迟探测，且没有被禁用或熔断。
