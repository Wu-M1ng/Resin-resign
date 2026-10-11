# Karing 的 Clash / ShadowTLS 导入兼容性

核查日期：2026-10-11。用户客户端版本为 Karing 1.2.24.2709 Windows。

## 已验证的边界

Resin 对 `2022-blake3-aes-256-gcm` + `plugin: shadow-tls` 输出一个完整的 Clash SS 节点，包含 SS 密码和 `plugin-opts` 内独立的 ShadowTLS 密码。使用官方 Mihomo v1.19.32 Windows 可执行文件导入实际导出结果后，本地 SS2022 → ShadowTLS → 回显服务的数据往返成功。

通过的测试包含默认 v2、v3、v3 Chrome 客户端指纹、v3 证书指纹，以及包含分号、等号和反斜杠的插件密码。测试只使用临时端口、生成的证书和测试密码，没有连接真实订阅节点，也没有操作运行中的 Karing。

测试入口为 `internal/feed/clashmeta_integration_test.go`，使用独立 Mihomo 进程，不经过 Resin 的 outbound builder：

```powershell
$env:RESIN_TEST_MIHOMO = 'E:\Cursor\Resin\.local\tools\mihomo-v1.19.32\mihomo-windows-amd64-compatible.exe'
go test -tags with_utls ./internal/feed -run TestClashMetaShadowTLSMihomoRoundTrip -count=1 -v
```

测试服务端使用 X25519 曲线。Mihomo 的 ShadowTLS 实现注明 v2 不兼容 X25519MLKEM768；最初允许测试服务端使用后量子曲线时，v2 回显失败，限定测试服务端曲线后通过。该限制与用户给出的 v3 配置不同，没有因此更改 Resin 导出参数。

## 与用户现象吻合的客户端代码

Karing 当前版本没有公开 `lib/app/utils/clash_to_singbox.dart`，但其历史版本可读取：

- [ClashToSingbox.convertShadowsocks，历史提交 e3c19f1](https://github.com/KaringX/karing/blob/e3c19f1eb1225dbd48a4862f0983b82d0af8de69/lib/app/utils/clash_to_singbox.dart#L66)
- [对应的 Clash YAML 参数解析](https://github.com/KaringX/karing/blob/e3c19f1eb1225dbd48a4862f0983b82d0af8de69/lib/app/utils/clash_yaml.dart)

该版本在 `plugin == "shadow-tls"` 时将输出类型直接设为 `shadowtls`，只复制插件版本、插件密码和 SNI，然后 `return null`。设置内层 SS 的 method/password 的逻辑位于这个返回语句之后，因此不会执行，也没有生成第二个出站。

这条路径能解释“Clash 原本有 SS + ShadowTLS 两层，导入后只剩独立 `type: shadowtls`、测速 EOF”的现象。它也能解释旧输出缺少 `plugin-opts` 时的空值断言异常：历史代码在空值检查之前访问了 `plugin_opts_shadowtls!`。

**这是历史代码证据，不是对当前 Karing 二进制实现的直接验证。** 当前用户提供的是导入后的单节点 JSON，尚缺 Resin 原始 Clash YAML 与完整导入配置的对照，不能排除存在另外一条 SS 出站的可能。

## 修复位置与后续验证

若当前 Karing 仍采用上述转换，客户端转换器应输出完整的两条原生出站：保留原名的 SS 入口（method/password，detour 指向外层）和唯一命名的 ShadowTLS 外层（服务器、插件密码、版本、TLS）。不能只替换原节点的 type。单纯增补标准 Clash 的插件字段无法改变这个提前返回的行为。

Resin 当前 `/clash-meta` 输出已经能被 Mihomo 使用，没有据此引入未经验证的 Karing 专用 YAML，也没有宣布 Karing 的 Clash 导入问题已修复。先前 sing-box 导出修复可以绕过 Clash 转换路径：启用并使用 `/sub/<token>/singbox`，刷新后确认 SS 的 detour 指向存在的 ShadowTLS 条目，选择 SS 入口进行测速。

进一步确认当前 Karing 的修复方式，需要同一节点的 Resin 原始导出 YAML 和完整导入配置。分享时将两层密码分别替换为 `SS_PASSWORD` 与 `SHADOWTLS_PASSWORD`，移除订阅 Token。不要仅根据导入后的一个外层节点推断整个配置的结构。
