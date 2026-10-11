# Hysteria2 指纹与 Shadowsocks + ShadowTLS

这两项适配使用当前 sing-box v1.12.21，无需替换依赖版本。Hysteria2 的扩展使用 `with_quic` 构建标签；ShadowTLS 的命名客户端指纹需要 `with_utls`，与项目 Docker 构建标签一致。

## Hysteria2

以下字段具有不同含义，不能互换：

| 输入 | 转换/校验 |
| --- | --- |
| Clash `fingerprint` | 完整服务器叶证书的 SHA256，转换为 `tls.certificate_sha256` |
| Hysteria2 URI `pinSHA256` | 完整服务器叶证书的 SHA256，转换为 `tls.certificate_sha256` |
| 原生 `tls.certificate_public_key_sha256` | 服务器叶证书公钥的 SPKI SHA256，Base64 编码 |
| Clash `client-fingerprint`、URI 的 `fp=chrome` 等客户端模板 | 不为 Hysteria2 启用 uTLS；当前 QUIC 后端不支持该握手模板 |

`certificate_sha256` 支持十六进制（可含冒号）或 Base64。多项 pin 中任一匹配即可；同时指定证书与公钥 pin 时，两类约束都必须匹配。摘要格式错误在构建阶段拒绝，摘要不匹配在握手阶段拒绝，包括 `insecure: true` 的情况。

完整证书 pin 保留配置的普通证书验证策略；原生公钥 pin 与 sing-box 1.13 的行为一致，以固定公钥代替 CA/域名/有效期信任校验。原生公钥 pin 不能和 `certificate` / `certificate_path` 混用。

原生 JSON 应放在 `outbounds` 数组中。仅传出站字段；其他客户端保存的 `groupid`、`latency` 等界面元数据不属于 sing-box 配置。Hysteria2 的原生 `tls.utls.enabled: true` 会给出明确的不兼容错误，不会静默删除未知安全配置。

## Shadowsocks + ShadowTLS

支持 Clash 的 `plugin: shadow-tls`、SS URI 的相同插件参数，以及原生 SS 的 `plugin` / `plugin_opts`。`shadowtls` 作为插件名也被接受。

```yaml
proxies:
  - name: ss-shadowtls
    type: ss
    server: 127.0.0.1
    port: 443
    cipher: aes-128-gcm
    password: ss-password
    plugin: shadow-tls
    plugin-opts:
      host: cover.example.com
      version: 3
      password: shadowtls-password
```

两层密码分别用于 Shadowsocks 加密和 ShadowTLS 认证，不应混用。支持版本 1/2/3；未指定版本时遵循 Mihomo 插件默认值 2。支持的插件参数为 `host` / `sni`、`version`、`password`、`alpn`、`skip-cert-verify` / `insecure`、`fingerprint`、`certificate_public_key_sha256`、`client-fingerprint`。Clash 顶层的 `client-fingerprint` 会传给插件；插件内部显式设置优先。未支持的参数会明确报错，避免默默忽略验证要求。

版本 1 使用标准 TLS 1.2，不接受 `client-fingerprint`；该组合会在构建时明确报错，避免浏览器模板协商 TLS 1.3 后卡住。版本 2/3 支持命名的客户端指纹。

Clash map 形式的插件参数会正确转义密码中的 `;`、`=` 和反斜杠。URI 和原生 `plugin_opts` 字符串需遵循 SIP003 转义规则，URI 外层还需 URL 编码。

组合节点在内部通过插件连接，不需要生成两个节点或配置 `detour`。本次不提供通用跨节点 detour 管理。ShadowTLS 包装 TCP；UDP 行为沿用 sing-box 的 Shadowsocks/UoT 配置。

## 更新与验证

1. 重新构建并更新实际运行的 Resin 二进制或镜像。只修改源码不会更新正在运行的服务。
2. 在后台手动刷新受影响的订阅，使旧的错误节点 JSON 被重新解析。
3. 重新探测节点。已有 `tls.utls.fingerprint` 错误映射的缓存不会因为重启自动变成证书 pin。

定向验证：

```powershell
go test -tags 'with_quic,with_utls' ./internal/subscription ./internal/outbound
go build -trimpath -tags 'with_quic,with_wireguard,with_grpc,with_utls' -o .local/resin-compatibility.exe ./cmd/resin
```

测试使用本机临时端口、临时证书和假密码：覆盖 Hysteria2 真实 QUIC 数据往返、错误证书/公钥拒绝、ShadowTLS 1/2/3 上的 SS 数据往返、错误密码/指纹拒绝、命名 uTLS 的并发 v3 连接，以及含分隔符的插件密码。没有访问真实订阅节点。

2026-10-11 验证结果：相关两个包的定向完整测试通过，无标签测试通过，Windows Docker 标签构建成功。独立审查发现的 v1 命名指纹和插件字符串末尾分隔符问题已修复、复核并通过补充测试。

`go test -tags 'with_quic,with_wireguard,with_grpc,with_utls' ./...` 全量运行仍有四个无关用例失败：两个 WireGuard 用例提示需要 `with_gvisor`，另有 Windows 连接重置识别及租约时间精度各一项。原始输出保存在本机 `.local/outbound-compatibility-tests.txt`。尝试增加 gVisor 标签的额外验证停留在依赖下载阶段，已停止，未作为通过依据。本机 `CGO_ENABLED=0` 且无 C 编译器，因此没有执行 race detector；并发数据往返测试已执行。

没有数据库结构变更。回滚运行程序时使用更新前保留的二进制或镜像；旧版本不认识新的 pin 配置，回滚后需恢复原有可用节点或刷新订阅。
