# Shadowsocks + ShadowTLS 兼容性诊断

> 本文及旁边的诊断脚本记录修复前行为。当前插件适配与使用方式见 `doc/outbound-compatibility.md`；本次通过内部插件组合解决问题，并未增加通用跨节点 detour 管理。

2026-10-11；本地仓库依赖为 sing-box v1.12.21。未修改业务代码或运行配置。

**结论：当前 Resin 未完整适配 Shadowsocks + ShadowTLS 组合。底层支持独立的 ShadowTLS 出站，但不支持通过 Shadowsocks 的 `plugin: shadow-tls` 字段调用它。**

`internal/subscription/parser.go:3928` 的 `setSSPluginFromClash` 将插件名及参数转换后直接写入 `plugin` / `plugin_opts`；`normalizeSSPlugin` 仅特殊处理 simple-obfs 等别名，没有 ShadowTLS 适配。SS URI 路径也会保留 `shadow-tls` 插件名。

sing-box 的 `protocol/shadowsocks/outbound.go:59` 调用 `sip003.CreatePlugin`。`transport/sip003/plugin.go:35` 在内存注册表找不到名称时返回 `plugin not found`；本版本内置注册的是 `obfs-local` 和 `v2ray-plugin`，没有 `shadow-tls`。这里不是到系统 PATH 查找插件程序。

与此同时，`include/registry.go:91` 明确注册了独立的 `shadowtls` 出站类型，因此不能由此推断整个底层缺少 ShadowTLS 协议。

## 实际验证

使用真实订阅解析器和 Resin 出站构建器，六项本地诊断均复现预期行为。所有地址均为回环地址，密码为假值，没有连接真实节点。

| 输入/操作 | 结果 |
| --- | --- |
| 普通 SS | 构建成功 |
| Clash SS，`plugin: shadow-tls` | 完整复现用户报告的构建错误 |
| SS URI，`plugin=shadow-tls;...` | 完整复现同一错误 |
| 将插件名改为 `shadowtls` | 仍然 `plugin not found: shadowtls` |
| 原生 `type: shadowtls`，版本 3 | 构建成功；未验证远端握手 |
| 分别构建原生 ShadowTLS 和通过 `detour` 引用它的 SS，再拨号 | `outbound detour not found: tls-layer` |

最后一项揭示另一个接入缺口：`internal/outbound/builder.go` 创建了 sing-box 出站管理器，但 `Build` 直接通过 registry 创建节点，没有将节点注册到该管理器。即使先构建 ShadowTLS 节点，SS 的 `detour` 仍无法找到它。因此只把订阅改成两个原生 outbounds 也不能保证当前 Resin 可用。

## 处理建议

这是功能兼容性问题，当前影响是该类节点无法使用。错误发生在出站构建阶段，不能说明服务器离线或密码错误。

正式支持需要把组合节点适配为 SS 经过 ShadowTLS 的完整连接链，映射两层各自的密码、ShadowTLS 版本、SNI 等参数，并实现依赖解析、注册与生命周期管理，或提供等价的内嵌组合适配。暂不支持时应给出明确的导入诊断。

不能通过删除插件字段来保持该节点原有协议；单纯改名也已验证无效。修复前可先使用其他兼容节点。

## 重现

仓库根目录执行：

```powershell
& ./docs/diagnostics/shadowtls-plugin/repro.ps1
```

脚本通过 Go overlay 注入 `repro_test.go.txt`，自动清理临时 overlay，不修改业务源码。通过表示缺陷和支持边界已被复现，不表示已修复。原始输出见 `results.txt`。本次没有运行状态需要回滚。
