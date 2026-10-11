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
| `singbox` | sing-box | 保留原生 outbound 字段，重写 `detour` 引用；SS 的 ShadowTLS 插件转换为原生双层出站 |
| `v2ray-base64` | v2rayN 等 | VMess、VLESS、Trojan、SS、Hysteria2、HTTP、HTTPS、非 TLS SOCKS5 |
| `uri` | 通用客户端 | 与 `v2ray-base64` 相同，返回纯文本 URI 行 |

不能可靠表达的协议按 Feed 的“不支持协议处理”执行：`skip` 跳过并通过 `X-Resin-Skipped-Count`、`X-Resin-Skipped-Types` 报告，`error` 直接拒绝本次输出。

Clash 输出会将 SS 的 SIP003 插件参数转换为 `plugin-opts` 对象。ShadowTLS 的 `host`、插件密码、整数 `version` 和 TLS 选项会保留，`client-fingerprint` 放在节点顶层；simple-obfs 使用 Clash 的 `obfs` 名称及 `mode`、`host` 字段。缺少必要参数或无法表达的选项按上述策略处理。如果客户端导入时提示 `Null check operator used on a null value`，且 SS 节点只有 `plugin: shadow-tls`、没有 `plugin-opts`，请更新 Resin 并重新刷新客户端订阅。

### SS + ShadowTLS 插件转原生 sing-box

Resin 运行时注册了 `shadow-tls` / `shadowtls` SIP003 插件，普通 sing-box 不包含此插件。因此，Resin HTTP 代理可以连接，并不表示原样输出的插件配置能在其他客户端使用。

`singbox` 输出会把已有的 `type: shadowsocks` + `plugin: shadow-tls` 转为两个原生出站：

```json
{
  "outbounds": [
    {
      "type": "shadowsocks",
      "tag": "订阅/节点",
      "server": "node.example.com",
      "server_port": 18801,
      "method": "2022-blake3-aes-256-gcm",
      "password": "<原 SS 密码>",
      "network": "tcp",
      "detour": "订阅/节点 [ShadowTLS]"
    },
    {
      "type": "shadowtls",
      "tag": "订阅/节点 [ShadowTLS]",
      "server": "node.example.com",
      "server_port": 18801,
      "version": 3,
      "password": "<原 ShadowTLS 插件密码>",
      "tls": {
        "enabled": true,
        "server_name": "www.php.net",
        "insecure": false,
        "alpn": ["h2", "http/1.1"]
      }
    }
  ]
}
```

两层密码分别保留，SS 入口沿用原来的显示名称；生成的传输层名称遇到重名会添加序号。客户端应选择、测速 SS 入口，带 `[ShadowTLS]` 后缀的是它的传输依赖。节点数按实际出站条目计算，一个组合节点通常输出两条，跳过数仍按无法转换的源节点计算。

插件省略版本时显式输出 **v2**，省略 ALPN 时保留插件的 `h2` / `http/1.1` 默认值；显式 `alpn=` 则保持无 ALPN。SNI、证书验证开关、支持的 uTLS 指纹和转义后的密码均会保留。已有 detour、DNS 解析器、绑定接口和连接超时等拨号设置移到外层，UoT/multiplex 设置留在 SS 层。未启用 UoT/multiplex 时默认输出 TCP；显式要求原始 UDP 的配置按不支持策略处理，因为原生 ShadowTLS 不能传输原始 UDP。

sing-box v1.12 的原生 TLS 字段无法表达 Resin 插件的证书指纹 `fingerprint` 和公钥固定 `certificate_public_key_sha256`，这类节点按 `skip` / `error` 处理，不会删除验证要求后继续输出。缺少 SS 密码、插件密码、SNI，或含未知/冲突参数的节点也按该策略处理；依赖失败节点的其他出站同时跳过，避免悬空 detour。

### 原生 SS → ShadowTLS 配对输出

如果原始订阅包含 `type: shadowsocks` 节点，且它的 `detour` 指向同一订阅中的 `type: shadowtls` 节点，导出会识别这对配置：

- `clash-meta`：合并为一个 `type: ss`、`plugin: shadow-tls` 节点，输出完整的 `plugin-opts`，并关闭 UDP。
- `uri` / `v2ray-base64`：合并为带 SIP003 `plugin` 查询参数的 SS 链接。该组合用于 TCP；客户端须支持 ShadowTLS 插件，不能把 UDP 当成经过 ShadowTLS 保护的流量。
- `singbox`：保留两个原生出站，同步修改 `tag` 与 `detour`；客户端应选择、测速 SS 内层节点。不会把 Resin 的内部 ShadowTLS 插件扩展写入这对原生配置。

内层 SS 的加密方式和密码、外层 ShadowTLS 的服务器、版本、密码及 TLS 参数分别来自原始配置。未填写版本的原生 ShadowTLS 按 **v1** 输出，不套用插件的 v2 默认值。多个 SS 节点可以共享同一个外层；节点去重后的原始标签别名也会保留用于配对。同名标签只在同一个来源订阅内解析；如果一个去重节点在不同订阅中指向不同外层，会按不支持策略处理，建议分别创建 Feed。

只有独立 ShadowTLS 时无法推导 SS 加密方式和密码，导出会跳过或报错。缺失引用、循环引用、歧义引用，以及合并时无法保留语义的选项（例如自定义 CA、多级 detour、额外插件和 UoT）也按 `skip` / `error` 处理；复杂原生配置可以使用 `singbox` 格式保留。合并掉的外层属于已使用的依赖，不计入跳过数量；Clash/URI 的节点数为合并后的数量，sing-box 为实际输出的出站条目数量。

对于 Karing 中的 `plugin not found: shadow-tls`，更新 Resin 二进制或镜像，启用 Feed 的 `singbox` 格式，并将客户端订阅地址切换到 `/sub/<token>/singbox` 后刷新。若旧的错误条目仍被缓存，可删除该客户端订阅再重新添加。客户端须保留两条出站及其 detour 引用；仅导入一个独立 `shadowtls` 或丢失引用仍无法连接。原生 sing-box 的 SS2022 + ShadowTLS 1/2/3 实际数据往返已有本地回归测试，Karing 的导入器行为仍需在客户端确认。Clash/Mihomo 可继续使用完整的 `clash-meta` 组合节点。

Clash 输出还通过了独立 Mihomo v1.19.32 的本地 SS2022 + ShadowTLS v2/v3 数据往返测试。Karing 历史转换器存在将此组合直接变成独立 ShadowTLS 的代码路径，与用户报告吻合；这不等于当前 Karing 已验证兼容。证据、测试命令和版本限制见 [Karing 的 ShadowTLS 导入核查](karing-shadowtls-compatibility.md)。

对于源订阅原本就是两条原生出站的情况，Feed 选择“原始订阅来源”，以便同时取得两层；平台来源仍只导出平台内的节点。转换仅在导出时生效，不改变 Resin 运行时的跨节点 detour 支持；源订阅本身不完整时仍需先从上游取得完整配置。

## 安全和反代

Token 是读取凭证，拿到完整 URL 即可读取该 Feed，但不能访问管理 API。轮换 Token 会立即使旧 URL 返回 404。生产环境使用 HTTPS，并保留现有 OpenResty 转发：

```nginx
location ^~ / {
    proxy_pass http://127.0.0.1:2260;
}
```

公开响应带有 `ETag` 和 30 秒私有缓存头；客户端发送匹配的 `If-None-Match` 时返回 304。公开接口带有进程内的 IP/Token 双维度限流，超过 120 次/分钟会返回 429；该限流在单进程内生效，多实例部署时仍需在 OpenResty/WAF 层设置限流。

Token 放在 URL 路径中，OpenResty 默认访问日志可能记录完整地址。建议为 `/sub/` 单独关闭访问日志或配置脱敏规则；Token 泄露后应立即在管理页面轮换。

启用“通过 Resin 中转”时，生成的 SOCKS5 节点密码使用该 Feed 自己的完整 Token，不再使用全局 `RESIN_PROXY_TOKEN`；普通 HTTP/SOCKS5 正向代理仍按全局密码认证。每个 Feed 的 Token 只允许访问该 Feed 当前绑定的平台、指定的中转端口和 TLS 端点，客户端提交的用户名中的平台名不会覆盖这个绑定；用户名仍可带一个可选的账号后缀用于粘性路由。中转 Feed 必须启用 TLS 并填写证书域名，旧的明文或缺少 SNI 的中转配置不会继续生效。轮换、禁用、修改或删除 Feed 会关闭已有中转连接，同时使公开订阅地址和该 Feed 的中转凭据失效，客户端需要重新刷新订阅。后台预览不会输出中转密码。

中转端口需要在 Resin 的“端点”中创建或启用 SOCKS5 监听，例如 `2261`，并在 VPS 防火墙放行该 TCP 端口。该端点必须启用 TLS，在 Feed 的中转设置中使用相同端口和证书域名：

- 监听地址：`0.0.0.0`
- 监听端口：`2261`
- SOCKS5：开启；管理页面、HTTP 正向和 HTTP 反向按需关闭
- TLS：开启
- 证书：`/etc/resin/tls/fullchain.pem`
- 私钥：`/etc/resin/tls/privkey.pem`

证书路径必须是 Resin 容器内的路径，并确保私钥对容器内的 `resin` 用户可读。Feed 的 Resin 中转选项中填写公网域名、`2261`、TLS 和证书域名（SNI）。Clash Meta 和 sing-box 可以表达 TLS SOCKS5；标准 `socks5://` URI 没有统一的 TLS 参数，因此 URI/V2Ray 输出会跳过该中转节点并在响应头中报告跳过类型。

HTTP 反代只负责 `/sub/` 订阅地址；SOCKS5 客户端必须直连该 TCP 端口，不能通过普通 `location proxy_pass` 转换。生产运行时即使 `RESIN_PROXY_TOKEN` 被显式设为空，HTTP 正向、HTTP 反向和普通 SOCKS5 也不会开放匿名访问；请使用非空全局代理密码。

如果订阅为空，先检查 Platform 是否有可路由节点。节点必须已生成 outbound、完成出口和延迟探测，且没有被禁用或熔断。
