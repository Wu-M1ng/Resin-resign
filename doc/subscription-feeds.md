# Resin 多格式订阅输出

Resin 的“订阅输出”以当前 Platform 可路由节点为数据源。原始订阅仍由 Resin 按各自的更新间隔刷新；Feed 不复制原始链接，客户端继续使用同一个 Feed URL 即可获得刷新后的节点。

## 创建 Feed

1. 打开管理页面的“订阅输出”。
2. 选择 Platform。Platform 的健康、出口 IP、延迟和熔断规则会继续生效。
3. 可选地勾选原始订阅来源；不选择时使用该 Platform 的全部订阅节点。
4. 选择默认格式和其他启用格式。
5. 保存后立即复制一次性显示的公开 Token。

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
| `v2ray-base64` | v2rayN 等 | VMess、VLESS、Trojan、SS、Hysteria2、HTTP、HTTPS、SOCKS5 |
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

如果订阅为空，先检查 Platform 是否有可路由节点。节点必须已生成 outbound、完成出口和延迟探测，且没有被禁用或熔断。
