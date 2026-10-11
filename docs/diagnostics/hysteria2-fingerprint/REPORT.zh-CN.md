# Hysteria2 订阅指纹转换诊断

> 本文及旁边的诊断脚本记录修复前行为。当前修复与使用方式见 `doc/outbound-compatibility.md`；正式回归测试位于 `internal/outbound` 和 `internal/subscription`，旧诊断脚本不作为修复后通过标准。

日期：2026-10-11。代码版本：`eb85f5405f05da12397672a5a8bce21a14ba70ee`。

**结论：Resin 的订阅转换存在可复现的字段语义混淆和底层兼容性缺陷，能够产生用户报告的完整错误。** 原始订阅格式尚未提供，所以已经确认的是代码缺陷及触发条件；尚未确认生产节点具体经过哪个解析分支。

## 根因与证据

Clash/Mihomo 的 `fingerprint` 表示服务器证书 SHA256 指纹，`client-fingerprint` 表示客户端 uTLS 握手模板，例如 `chrome`、`firefox`。二者不能互换。字段语义参见 [Mihomo 官方 TLS 文档](https://wiki.metacubex.one/config/proxies/tls/)。

`internal/subscription/parser.go:1303` 的 Hysteria2 分支将 `fingerprint`、`client-fingerprint`、`client_fingerprint`、`fp` 合并，且优先取 `fingerprint`；随后 `parser.go:3908` 的 `applyUTLSFromValue` 将原值写入 `tls.utls.fingerprint` 并启用 uTLS。

因此，输入 `fingerprint: c083d33c12db8a02d88a5169fb9cee3dae9291fffe38c579891a9dcff55304fb` 会生成：

```json
{"utls":{"enabled":true,"fingerprint":"c083d33c12db8a02d88a5169fb9cee3dae9291fffe38c579891a9dcff55304fb"}}
```

`go.mod:14` 固定的 `sing-box v1.12.21` 只接受命名的 uTLS 模板，不接受证书摘要作为模板名。其 `common/tls/utls_client.go` 的 `uTLSClientHelloID` 返回 `unknown uTLS fingerprint`；Resin 的 `internal/outbound/builder.go:150` 和 `internal/outbound/manager.go:74` 再包装成用户看到的错误。这发生在出站构建阶段，尚未连接订阅节点，与接入端口 2260/2261 的绑定或代理令牌无关。

## 实际复现

使用项目实际 `ParseGeneralSubscription → SingboxBuilder.Build`，构建标签为 `with_quic,with_utls`。仅使用假密码、回环地址和本机临时 UDP 端口，未连接真实节点。

| 输入 | 观察结果 |
| --- | --- |
| 不带指纹的 Hysteria2 | 构建成功；未验证远端连通性 |
| Clash `fingerprint` 使用用户提供的值 | 完整复现 `outbound build: create outbound [hysteria2]: unknown uTLS fingerprint: ...` |
| Clash `fingerprint` 使用另一组 64 位十六进制值 | 同样失败，说明并非仅该特定摘要触发 |
| 同时设置证书指纹和 `client-fingerprint: chrome` | 仍优先使用证书指纹并失败 |
| 仅 `client-fingerprint: chrome` | 构建成功；真实回环拨号失败：`unsupported usage for uTLS` |
| Hysteria2 URI 带 `pinSHA256` | 构建失败：`tls.certificate_public_key_sha256: json: unknown field "certificate_public_key_sha256"` |

后两项说明不能简单将摘要改成 `chrome` 或改名为 `pinSHA256` 来修复：

- 当前 `sing-quic v0.5.3` 的 Hysteria2 在首次连接时调用 `qtls.CreateTransport`，需要 TLS 配置的 `Config()`；`sing-box v1.12.21` 的 uTLS 实现会返回 `unsupported usage for uTLS`。这是此次本地拨号验证到的协议兼容性限制。
- `parser.go:3369` 将 URI 的 `pinSHA256` 转成 `certificate_public_key_sha256`，但当前依赖的 `option.OutboundTLSOptions` 并无该字段。即使将来升级到支持某种 pin 字段的版本，也必须核实完整证书摘要和公钥摘要的语义及编码，不能直接互换。

原有 `TestParseGeneralSubscription_ClashJSON_Hysteria2AdvancedFields` 和 `TestParseGeneralSubscription_HY2URIPinSHA256MapsToTLSCertPublicKeyPin` 均通过，但只断言生成的 JSON，未验证真实出站构建/使用。其中前者使用 `fingerprint: firefox` 固化了错误的字段解释。

## 影响与修复方向

这是会使部分订阅节点不可用的功能缺陷，建议优先修复。此次失败路径会拒绝构建或连接，未发现它本身导致令牌泄露或绕过证书校验。忽略证书指纹、统一替换为 `chrome`、设置 `insecure: true` 均不能作为保留原配置安全语义的修复。

建议按订阅格式及协议分别处理证书指纹和客户端指纹；Hysteria2 不应生成当前后端无法使用的 uTLS 配置。证书固定必须采用底层实际支持且语义匹配的校验方式；暂不支持时，应明确报告该节点的证书固定功能不兼容，避免静默丢弃校验。测试应覆盖解析、真实出站构建及必要的本地连接阶段。

如果原始订阅已经是 sing-box JSON，且自身将摘要放在 `tls.utls.fingerprint`，则源订阅也存在配置错误；需要原始节点的脱敏配置才能最终归属此次生产错误的入口。

## 重现命令与记录

在仓库根目录执行：

```powershell
& ./docs/diagnostics/hysteria2-fingerprint/repro.ps1
go test ./internal/subscription -run 'TestParseGeneralSubscription_(ClashJSON_Hysteria2AdvancedFields|HY2URIPinSHA256MapsToTLSCertPublicKeyPin)$' -count=1 -v
```

`repro.ps1` 使用 Go overlay 临时注入 `repro_test.go.txt`，结束时自动清理 overlay 文件，不修改业务源码。六个诊断用例通过表示当前行为已成功复现，并不表示缺陷已修复。原始运行输出见同目录 `results.txt`。

本次只新增诊断材料；业务代码、依赖版本和运行配置均未修改，无运行状态需要回滚。

## 补充：本地客户端可用配置

用户随后提供了另一客户端可用的 Hysteria2 配置：使用 `server_ports`、`tls.insecure: true` 和 `tls.certificate_public_key_sha256`，没有 `tls.utls`。未将真实服务器地址、密码或客户端身份信息写入复现材料。

这份客户端配置进一步区分了三个概念：Clash/Mihomo 的证书摘要、sing-box 的证书公钥摘要，以及 uTLS 客户端握手模板。用户给出的 Base64 公钥摘要解码后与先前错误中的十六进制值不同，不能视为同一摘要的编码转换，也不能通过直接改字段名来保证正确。

经在线核实，[官方 TLS 文档](https://sing-box.sagernet.org/configuration/shared/tls/#certificate_public_key_sha256) 明确标记 `certificate_public_key_sha256` 自 sing-box **1.13.0** 开始支持，格式为 Base64 编码的服务器证书公钥 SHA256 摘要。当前 Resin 依赖 **1.12.21**，缺少此字段。

将用户配置中的客户端元数据去除，替换为回环地址、假密码和测试公钥摘要，并包装为原生 `outbounds` 后，通过实际解析器和构建器验证：解析后没有新增 uTLS，但构建报 `tls.certificate_public_key_sha256: json: unknown field "certificate_public_key_sha256"`。新用例 `native_public_key_pin` 已通过，输出保存于 `native-pin-results.txt`，可单独执行：

```powershell
& ./docs/diagnostics/hysteria2-fingerprint/repro.ps1 -TestPattern '^TestDiagnosticHY2Fingerprint$/^native_public_key_pin$'
```

同时核实了 [sing-box v1.13.0 的标准 TLS 实现](https://github.com/SagerNet/sing-box/blob/v1.13.0/common/tls/std_client.go#L114)：配置公钥固定后会设置自定义 `VerifyPeerCertificate`，即使 `insecure: true` 也会调用公钥摘要验证。因此，不能将这一组合直接描述为完全不验证服务器身份；也不能只删除 pin 并保留 insecure，否则会失去这层校验。尚未确认用户客户端的实际核心及版本，不能据此保证其执行细节与该官方版本完全一致。

原生 sing-box 订阅在 `parser.go:176` 保留节点 JSON，不会把这一公钥字段转成 uTLS。先前的 `unknown uTLS fingerprint` 表明 Resin 当时收到或生成的是另一份配置。已确认的转换缺陷与依赖版本缺口均仍成立；要确定该生产节点的具体来源，还需要 Resin 实际收到的原始订阅节点，而非另一客户端转换后的配置。

修复必须同时处理字段语义和后端能力：按证书摘要/公钥摘要分别实现正确校验，评估升级或补充底层支持，并验证错误 pin 确实拒绝连接；单纯升级版本不会自动修复现有的 fingerprint → uTLS 错误映射。
