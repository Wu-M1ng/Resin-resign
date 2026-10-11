# Hysteria2 与 ShadowTLS 兼容性修复计划

**Goal:** 修复订阅中的证书指纹被误当作 uTLS，以及 SS 的 shadow-tls 插件无法构建；保留身份校验。

**Architecture:** 保持 sing-box v1.12.21，用独立文件扩展 Hysteria2 TLS 选项并复用 sing-quic 客户端；向现有 SIP003 注册表注册基于 sing-shadowtls 的适配器，组合节点仍是一个节点，不依赖跨节点 detour。原有普通节点保持原生构建路径。

**Tech Stack:** Go 1.25.5、sing-box v1.12.21、sing-quic v0.5.3、sing-shadowtls。

**Spec:** `docs/diagnostics/hysteria2-fingerprint/REPORT.zh-CN.md`、`docs/diagnostics/shadowtls-plugin/REPORT.zh-CN.md`，以及用户提供的原生公钥固定配置。用户已授权直接修复；在当前工作区执行并保留原有未跟踪材料。

## Task 1: Hysteria2 指纹

- [x] 添加 `internal/subscription/parser_tls_compat_test.go` 回归测试：Clash 证书 fingerprint、URI pinSHA256 必须生成 `certificate_sha256`，不得生成 uTLS；客户端模板提示不得启用 QUIC 不兼容的 uTLS。
- [x] 修改 `internal/subscription/parser.go` 和受影响旧测试，分别处理证书摘要、SPKI 摘要和客户端模板。
- [x] 添加 `internal/outbound/tls_pins.go`：严格解析 32 字节摘要，使用 TLS 验证回调执行完整证书/公钥校验；格式错误、错误 pin 均拒绝。证书 pin 保留普通证书验证开关；原生公钥 pin 按 sing-box 的固定密钥身份语义验证。
- [x] 添加 `internal/outbound/hysteria2_pinned.go`（with_quic）和注册 stub，在现有选项基础上扩展两个 pin 字段，复用 sing-quic 全部客户端选项；无 pin 节点交回原构造器。
- [x] 本地 QUIC 服务验证正确 pin 的实际数据往返，错误 pin 在 insecure=true 时仍拒绝；覆盖原生 JSON、Clash、URI。

## Task 2: ShadowTLS 插件

- [x] 添加 `internal/outbound/shadowtls_plugin.go`，注册 `shadow-tls` / `shadowtls`；支持版本 1/2/3、分离 SS 与 ShadowTLS 密码、SNI、ALPN、证书校验和客户端指纹。v1 + 命名指纹因 TLS 版本限制明确拒绝，v2/v3 支持命名指纹。
- [x] 修正 Clash plugin-opts 字符串转义及顶层 client-fingerprint 传递，避免密码中的分隔符改变参数；覆盖字符串末尾已有分隔符的情况。
- [x] 对插件参数进行验证，未知安全参数明确报错，避免静默忽略校验。默认版本遵循 Mihomo（2）。
- [x] 用本地 TLS 伪装服务 + ShadowTLS 服务 + SS 服务测试完整数据往返、错误密码拒绝及重复/并发连接。

## Task 3: 回归与交付

- [x] `go test -tags 'with_quic,with_utls' ./internal/subscription ./internal/outbound` 通过；审查修复后的新增边界用例复测通过。
- [x] 无标签相关包测试通过；已执行 Docker 标签完整 Go 测试和构建。全量测试的 Windows 连接重置识别、租约时间精度失败与本次改动无关；两个 WireGuard 用例提示缺少 `with_gvisor`。
- [x] 独立审查覆盖校验回调、资源释放和并发行为，发现的两个 P2 边界已修复并复核。文档见 `doc/outbound-compatibility.md`。用户凭据未写入文件，未访问真实节点，未提交或部署。
