Resin 风险审计——按 2260 本地管理、2261 TLS 正向代理部署重新评估（2026-10-11）

按用户提供的端口配置，公网入口已经隔离管理功能并关闭 HTTP 反向代理，风险重心是服务可用性、实际后端依赖版本以及订阅来源。原先按默认混合入口列出的后台同源、空管理令牌和明文访问问题，不再列为当前 2261 的直接公网高风险；日志凭据问题也必须按反向代理、普通 HTTP 正向代理、CONNECT/SOCKS 隧道分别判断。此次检查未发现明确的主动窃密或后门代码证据，也未获得当前部署已被利用的证据。

审计对象为 `E:\Cursor\Resin`，提交 `eb85f5405f05da12397672a5a8bce21a14ba70ee`。检查覆盖主要 Go 服务入口、认证、代理、订阅下载、日志、React 凭据存储、容器配置和依赖。业务源码及已有未跟踪计划文件未改动。审计新增文件均在本目录；安装依赖和构建产生了被 Git 忽略的 `webui/node_modules`、`webui/dist` 和 `.local/security-audit-tools`。

验证使用本机 Windows/amd64、Go 1.25.5，以及回环 HTTP 服务和假凭据。未连接用户实际代理节点，未做公网扫描或资源耗尽测试。浏览器同源风险验证到响应内容、响应头和配置链路，未实施真实管理员令牌读取。

**本次采用的部署信息与边界。**

| 项目 | 2260 | 2261 |
|---|---|---|
| 宿主机访问范围 | 按用户说明为 `127.0.0.1`，仅回环访问 | `0.0.0.0`，按可能被公网访问评估 |
| 管理 UI / 管理 API | 开启 | 关闭 |
| HTTP 反向代理 | 开启 | 关闭 |
| HTTP 正向代理 / SOCKS5 | 开启 | 开启 |
| 入口 TLS | 截图未启用 | 截图已启用 |
| 强制客户端认证选项 | 截图关闭；非空全局代理令牌仍会验证 | 截图开启 |

截图中两个端点都显示 `0.0.0.0`，因此 2260 的回环限制按用户提供的信息作为评估前提，尚未在实际宿主机核验。如果采用 Docker，宿主机映射 `127.0.0.1:2260:2260` 与容器内部监听 `0.0.0.0` 并不矛盾。还假定没有另一个公开反向代理把 2260 的管理路由发布出去。`0.0.0.0` 本身只表示监听所有接口，最终公网可达性仍由端口映射、路由和防火墙决定。

源码确认 `AllowManagement=false` 会阻止管理页面、`/api/` 和 `/sub/` 的直接访问，`/healthz` 为例外；`AllowHTTPReverse=false` 会阻止 URL 反向代理。普通代理请求仍需有效凭据。截图中的“强制客户端认证”是代理身份检查选项，不表示启用了 TLS 客户端证书认证（mTLS）。

**按当前部署重新排序。** “优先处理”表示整改顺序；依赖公告、镜像来源和未触发的条件风险不据此认定为已证实可远程利用。

| 编号 | 当前优先级 / 状态 | 当前部署的判断 |
|---|---|---|
| R5 | 中，优先处理：HTTP 连接读取超时缺失 | 2261 仍启用 HTTP 正向代理；认证前的连接占用不会被代理密码消除 |
| R8 | 优先核实实际版本、更新后端依赖 | Go/TLS/网络解析依然参与 2261 的运行；原扫描只代表本地构建环境，不代表运行镜像已命中全部公告 |
| R7 | 中，按来源处理：订阅/探测下载无大小上限 | 管理入口不公开仍会拉取远端订阅；需要恶意或异常响应，普通匿名客户端不能直接新增订阅 |
| R4 | 已降级，按流量类型判断：日志敏感数据 | 2261 不走反向代理的 API Key 自动提取路径；普通 HTTP 目标 URL 和显式代理账号仍会记录；CONNECT/SOCKS 不解密内部 HTTPS |
| R9 | 待核实：实际镜像发布者与版本 | 文档存在不同镜像来源，不足以证明用户实际使用了任何一个或镜像有恶意内容 |
| R1 | 2261 直接路径关闭；2260 留有条件风险 | 2261 无后台、无反向代理；本地 2260 打开恶意代理网页仍可能触发，需有效代理请求和管理员浏览器交互 |
| R2 | 2261 直接路径关闭；本地配置核查项 | 管理 API 不对 2261 开放；只有管理令牌实际为空时，本机可达者才有匿名管理问题 |
| R3 | 对外入口已有 TLS；保持证书校验 | 2261 不属于默认明文入口场景，2260 按回环访问评估 |
| R6 | 2261 直接路径关闭 | `/sub/` 同样被管理功能开关挡住；以后另行公开订阅分发时再提升优先级 |

**部署核验命令。** 以下用于实际部署主机，未在用户服务器执行：

```powershell
docker port resin 2260/tcp
docker port resin 2261/tcp
docker inspect resin --format '{{.Config.Image}} {{.Image}}'
```

如果容器名为 `resin`，第一条预期仅显示 `127.0.0.1:2260`（或明确的回环绑定），不应显示宿主机的 `0.0.0.0:2260` / `[::]:2260`。若使用 host 网络或直接运行二进制，应改查宿主机实际监听状态。镜像输出只确认名称/ID，不能单独证明镜像内容或 Go 编译版本安全。

下文保留各问题的源码证据，并分别注明当前部署适用性。

**R1：管理后台与不可信上游网页共享浏览器源。**

当前部署：2261 的这条直接路径已被反向代理开关阻断，且与 2260 属于不同端口、不同浏览器源。2260 本地仍同时提供管理和反向代理，因此需要“通过本地端点打开恶意代理网页、同源存在管理令牌”等条件才成立。若 2260 仅用于管理，不通过它浏览外部网页，此项当前优先级较低；管理员浏览器所在的主机和访问地址也必须纳入判断。

`internal/service/control_plane_endpoint.go:92` 的默认端点同时启用管理和 HTTP 反向代理。`cmd/resin/inbound_mux.go:41` 起按路径将两者分流，但浏览器的同源边界由协议、主机和端口决定，路径无法隔离权限。`internal/proxy/reverse.go:328` 使用 `httputil.ReverseProxy`；`ModifyResponse` 未强制设置隔离策略，上游 `text/html` 和脚本会原样返回。`webui/src/features/auth/auth-store.ts:23` 将完整管理令牌持久化到 `localStorage`。

持有普通代理令牌的人可以构造指向其网页的反向代理链接。如果已登录管理员打开该链接，网页脚本会运行在 Resin 的源下，可访问相同 localStorage，进而以管理员身份调用 `/api/v1/`。管理员通过代理浏览的目标本身被控制时也存在同样风险。需要上述浏览器访问条件，不能据此声称无交互的匿名接管。

`TestAuditReverseProxyReturnsActiveHTML` 验证固定标记脚本原样返回、类型为 HTML，且无隔离 CSP 或下载响应头。该测试走项目原有本地代理测试设施，不读取真实凭据。

建议：将管理 UI/API 与代理内容放在不同浏览器源；管理域名仅允许管理路径，代理域名不能承载管理页面。单纯更换 URL 路径或只在后台页面增加 CSP 无法解决。对仅供 API 使用的反向代理，可考虑在所有代理响应强制设置经过兼容性验证的 CSP sandbox 或下载策略；如必须支持浏览网页，优先进行源隔离。仅将令牌改成 HttpOnly Cookie 也不能代替源隔离，因为同源恶意脚本仍可能代用户调用接口。

**R2：空管理令牌关闭全部管理认证。**

当前部署：2261 关闭管理功能，所以不能沿该端口的直接管理路径利用空管理令牌。用户没有提供实际令牌值，本报告不推断它为空或薄弱。仍建议给本地后台设置独立的非空强令牌，防止本机其他进程或后续配置变更扩大影响。

`internal/api/middleware.go:14` 在 `adminToken == ""` 时直接进入管理处理器。`internal/config/env.go:171` 只检查环境变量是否存在，不拒绝显式空值。`docker-compose.yml.example:10` 使用 `${RESIN_ADMIN_TOKEN}`；没有设置该变量时，常规 Compose 插值会得到空字符串。该示例同时通过 `2260:2260` 发布到所有宿主机接口。此组合可能让匿名访问者读取配置、订阅和日志，修改平台或监听端点。

`TestAuthMiddleware_EmptyConfiguredToken_DisablesAuth` 和 `TestLoadEnvConfig_EmptyTokensAllowedWhenDefined` 均通过，证明此行为是当前实现的一部分。未在本机启动 Docker 来重复 Compose 插值。中文 README 的端口示例使用回环地址，与此 Compose 示例不同，应区分使用场景。

建议：生产启动拒绝空值和示例令牌；确有匿名本地使用需求时，以独立且显式的开发模式开关配合回环绑定。Compose 使用 `${RESIN_ADMIN_TOKEN:?set a non-empty admin token}`，代理令牌同样要求非空。不要照用 README 中的 `admin123` 或 `.env.example` 中的占位值。

需要纠正一个容易产生的误判：生产 HTTP 正向和反向代理在 `cmd/resin/app_runtime.go:417`、`:430` 设置了 `RequireProxyToken: true`，空代理令牌会拒绝代理请求。因此不能把空管理令牌问题扩大描述为“默认匿名开放全部代理协议”。

**R3：默认入口不加密客户端到 Resin 的链路。**

当前部署：2261 已开启 TLS，原来的公网明文风险已经缓解；结论以客户端正常校验服务端证书为前提。2260 按回环访问评估，不再按公网明文传输定级。若通过其他入口远程访问后台，需按那个入口的传输链路另行判断。

`internal/service/control_plane_endpoint.go:92` 构造的默认端点不启用 TLS；`cmd/resin/endpoint_runtime.go:110` 仅在 `TLSEnabled` 为真时包装 TLS listener。默认监听地址为 `0.0.0.0`（`internal/config/env.go:93`）。直接访问该 HTTP 入口时，管理 Bearer Token、代理认证或 URL 中的代理令牌缺乏链路保密性。后半段目标 URL 使用 `https`，不能保护前半段客户端到 Resin 的明文链路。

建议：将默认管理入口限制在回环或私网，通过 HTTPS 管理网关或受控隧道访问；对外提供单独启用 TLS 的代理端点。覆盖访问日志中的代理/订阅令牌路径。代码支持 TLS，这里报告的是默认部署配置，而非声称项目无法加密。

**R4：关闭详情日志后，业务凭据仍进入账号和目标 URL 字段。**

当前部署需要修正适用范围：此前验证的是反向代理，而 2261 已关闭反向代理，因此不能说使用 2261 就必然把目标站点的 API Key 写入日志。源码对不同模式的处理如下：

| 实际访问方式 | Resin 当前记录范围 / 凭据风险 |
|---|---|
| HTTP 正向代理访问 `http://` 目标 | `internal/proxy/forward.go:231` 保存完整 URL，`:233` 保存代理身份中的账号；URL 查询密钥或人为用密钥充当账号时仍有风险。此处不执行反向代理的 Authorization 自动提取规则 |
| HTTP CONNECT 隧道访问 HTTPS | `internal/proxy/forward.go:329` 记录目标主机/端口，目标 URL 为空；账号来自代理身份。内部 HTTPS 的路径、业务 Authorization 和正文不会被这条隧道日志逻辑解密捕获 |
| SOCKS5 隧道 | `internal/proxy/socks5.go:137` 记录目标地址和代理账号，目标 URL 为空；本身不按业务 HTTP 头解析和记录 API Key，内部 HTTPS 仍是密文 |
| 2260 的 HTTP 反向代理 | 仍适用下述自动提取业务密钥的原始证据；是否实际使用此模式尚未确认 |

因此先检查真实请求模式及日志内容；只有发现实际敏感值落盘时才据此确定清理和轮换范围，不应仅凭之前的反向代理用例推断所有 2261 流量已泄密。

`cmd/resin/main.go:451` 初始化 `* -> [Authorization, x-api-key]` 的默认账号规则。`internal/proxy/account_matcher.go:170` 返回完整头值，没有做摘要。`internal/proxy/reverse.go:286` 将其设置为日志账号；`:302` 记录包含查询参数的目标 URL。`internal/requestlog/repo.go:158`、`:159` 将这些值写入 SQLite。默认 `RequestLogEnabled=true`，而详情日志默认关闭（`internal/config/runtime.go:39`）。

`TestAuditDefaultLogsRetainCredentialAccountAndQuery` 使用相同默认规则，确认 `Bearer AUDIT_FAKE_API_KEY` 原样保存在日志事件的 `Account`，`access_token=AUDIT_FAKE_QUERY_TOKEN` 原样保存在 `TargetURL`，即使 `ReqHeaders` 为空也如此。事件至数据库的保存路径经源码核对。此外，租约账号会进入缓存持久化（`internal/state/repo_cache.go:196`）；仅删除请求日志不足以消除已保存的账号凭据。

影响包括数据库、备份、管理 API 和浏览器日志视图扩大业务密钥的接触范围。启用详情日志后，请求头和正文还会原样捕获，风险进一步增加。

建议：在进入路由账号模型之前对敏感身份做带服务端密钥的 HMAC，保留粘性路由能力；对展示和日志进行统一脱敏，过滤 URL 中的 `token`、`key`、`access_token` 等参数。临时措施可显式使用不敏感的账号标识，并按需求关闭请求日志。整改后评估轮换已落盘密钥以及清理日志、租约缓存和备份。

**R5：首字节之后缺少 HTTP 读取超时。**

当前部署仍适用，列为优先处理项。2261 的 TLS 能保护传输，但完成 TLS 握手并发送首字节后，HTTP 请求头解析仍位于代理令牌校验之前；未持有令牌的连接也可能在这个阶段占用资源。影响是服务可用性，目前未证明能借此取得管理权限。该项聚焦 HTTP 路径：SOCKS5 握手已有 15 秒超时（`internal/proxy/socks5.go:116`），不能笼统声称全部协议握手都没有超时。

生产端点使用 `&http.Server{Handler: httpHandler}`（`cmd/resin/endpoint_runtime.go:139`），`ReadHeaderTimeout`、`ReadTimeout`、`IdleTimeout` 均为零。协议分流只在读取首字节前设置超时，随后立即清除（`cmd/resin/inbound_demux.go:168`、`:175`）。因此发送首字节后停滞的 HTTP 客户端可能长期占用连接和 goroutine，认证处理器还未获得机会拒绝请求。

`TestAuditRuntimeHasNoHTTPReadDeadlines` 检查实际端点构造器产生的服务对象，确认三个超时均为零。未用大规模连接压测实际容量。

建议：设置合理的请求头读取期限、管理 API 读取期限和 HTTP 空闲期限，配合连接数限制；长流式代理和 CONNECT 应使用单独的策略，避免简单给所有请求套短 `WriteTimeout` 导致正常长连接中断。

**R6：公开订阅限流绕过和限流器自身内存增长。**

当前部署：`cmd/resin/inbound_mux.go:47` 起对管理路由执行能力检查，`/sub/` 也属于这些路由，因此 2261 不能直接进入此限流器。此问题保留为源码缺陷和将来公开订阅分发时的整改项，当前不列为 2261 的直接公网攻击面。

`internal/api/handler_feed_public.go:104` 优先信任请求的 `X-Real-IP`，未检查直接对端是否为可信反向代理。限流器又在验证订阅令牌之前，以任意输入的 token hash 建桶。即使 IP 维度已限流，代码仍创建新 token bucket，再把计数回退到零；桶条目没有删除，直到时间窗口清理（`:132` 起）。

本地证据：在限额为 2 的测试中，同一 RemoteAddr 变更伪造 `X-Real-IP` 和无效 token，4 次都被允许；在限额为 1 的另一测试中，8 次被 IP 限制拒绝的请求仍新增 8 个桶。使用不同假 token 不需要知道有效订阅令牌。

边界：已知的同一个有效 token 仍有独立 token 维度限制；此次不能证明该限制被绕过。问题是无效 token 请求和不受容量约束的限流状态可被滥用。

建议：只在对端属于明确可信代理名单时接受转发 IP，边缘网关覆盖而非透传该头；先检查 IP 限额，验证 token 后再建立 token 限流状态；对 map 设置全局容量和逐出策略，拒绝请求不新增状态。

**R7：远端下载使用无大小限制的 ReadAll。**

当前部署仍适用，但需要异常/恶意订阅或探测响应，不能描述为匿名客户端可直接上传任意订阅。只使用可信 HTTPS 订阅可降低发生概率；限制响应大小仍有必要，防止上游故障、被控制或误返回超大内容。

`internal/netutil/downloader.go:100`、`internal/netutil/outbound_http.go:104` 和 `internal/probe/fetcher.go:57` 将远端响应读入内存。现有请求超时限制的是时间，不限制字节数量；标准 HTTP 客户端还可能自动解压 gzip。并发订阅、探测或异常响应会放大内存占用。

`TestAuditDownloaderBuffersDecodedResponse` 只用一个本地小型 gzip 响应，确认解压后的 2,097,152 字节全部进入返回切片。更大响应无上限的判断来自源码；未制造 OOM，也未测量实际崩溃阈值。

建议：为订阅、GeoIP 和小型探测分别设置合理上限，对解压后的读取使用 `io.LimitReader(max+1)` 并拒绝超限；大文件改成流式落盘、限额与校验，限制刷新并发。单看 Content-Length 不能覆盖分块响应和解压膨胀。

**R8：依赖漏洞与构建工具风险。**

当前部署优先核实后端：2261 的 TLS、HTTP 解析、代理连接和节点协议仍会使用相关组件，关闭管理页面不等于这些组件停止运行。前端只在本地管理入口提供，Vite 若未启动则不是当前运行服务的一部分，因此前端/开发工具公告的公网优先级降低。下面 42 条 Go 公告属于本机 Go 1.25.5 / Windows 静态扫描结果；实际 Docker/Linux 镜像可能使用不同补丁版，Windows 专用公告也不能直接套用，应以实际二进制/SBOM 复扫。

`npm audit` 按当前锁文件报告 19 个受影响依赖：11 高、6 中、2 低。`npm audit --omit=dev` 报告 3 个中危依赖：`@remix-run/router`、`react-router`、`react-router-dom`。这些是依赖记录数量，包含上游漏洞向依赖树传播的记录，不能相加当作独立可利用漏洞。生产构建为静态 React SPA，并非 React Router SSR；SSR 相关公告不能直接认定适用于该部署。

开发依赖中的 Vite 命中多个文件读取/路径检查公告，例如 [GHSA-p9ff-h696-f583](https://github.com/advisories/GHSA-p9ff-h696-f583)。`webui/vite.config.ts:15` 明确将开发服务器绑定 `0.0.0.0`。开发机上运行 `npm run dev` 且端口对其他人可达时，风险应实际重视；正式 Go 内嵌静态页面不等于运行了 Vite 开发服务器。

Go 使用 `govulncheck v1.9.0`、生产构建标签 `with_quic,with_wireguard,with_grpc,with_utls`，按 **Go 1.25.5 / Windows** 扫描。数据库最后更新时间为 `2026-10-08T22:31:09Z`。扫描器标记 42 个不同公告具有潜在符号调用路径；其中 stdlib 涉及 37 个公告，`x/net` 8 个，grpc 2 个，`x/crypto` 和 `x/text` 各 1 个，模块之间存在公告重叠。

| 组件 | 检测版本 | 本次公告中提示的修复版本例子 |
|---|---|---|
| Go 标准库 | 1.25.5 | 多个后续补丁；部分最新公告修复版本为 1.26.9，应选择当前受支持分支的最新安全补丁 |
| `golang.org/x/net` | 0.50.0 | 0.60.0 |
| `google.golang.org/grpc` | 1.74.2 | 1.83.1 |
| `golang.org/x/crypto` | 0.48.0 | 0.52.0 |
| `golang.org/x/text` | 0.34.0 | 0.39.0 |

例如 [GO-2026-4341](https://pkg.go.dev/vuln/GO-2026-4341) 涉及 URL 参数解析的内存耗尽，[GO-2026-4971](https://pkg.go.dev/vuln/GO-2026-4971) 涉及 Windows 网络调用处理 NUL 字节时 panic；这里只记录扫描与公告，不声称已在 Resin 上触发。接口调用和依赖内部注册会让静态分析较保守，必须结合启用协议和实际输入可控性逐条验证。

推荐升级后重新测试 sing-box 集成与构建标签，不直接盲目执行 `npm audit fix --force`。Dockerfile 使用浮动的 Go 1.25 构建镜像，实际镜像补丁版和扫描时本机版本不一定相同，应对最终产物或 SBOM 复扫。

证据：`npm-audit.json`、`npm-audit-production.json`、`govulncheck-local-go.json`、去重后的 `dependency-summary.json`。`govulncheck.txt` 是首次工具自动切换 Go 1.26.9 的预检查，仅供对照；正式结论使用明确记录 Go 1.25.5 的 JSON 文件。JSON 模式即使有 finding 也可能退出 0，因此不能只看退出码断言无漏洞。

**R9：中文 README 指向另一发布者的镜像。**

当前部署尚未确认实际使用的镜像来源、digest 和构建版本。此项归为需要核实的供应链信息，不因 README 不一致就判断用户正在运行有问题的镜像。

`README.zh-CN.md:81` 使用 `ghcr.io/wu-m1ng/resin-resign:latest`；英文 README 和 `docker-compose.yml.example:6` 使用 `ghcr.io/resinat/resin:latest`。Git 历史可见 fork 合并记录，这可能是分支维护的选择，本身不构成后门证据。但照中文文档运行的镜像不能自动视为与本目录源码等价，且 `latest` 可变。

建议确认实际维护者和源代码对应关系，优先从已审计提交构建，或使用经过验证的发布镜像并固定 digest；本次未拉取或逆向分析该第三方镜像。

**可靠性检查结果。**

- 前端 `npm ci --ignore-scripts --no-fund --no-audit` 和 `npm run build` 成功。
- `npm run lint` 失败，5 个错误、8 个警告。错误集中于 PlatformMonitorPanel 的 `any`，以及 RequestLogsPage 的 effect 内状态更新和 memo 依赖。原始结果见 `eslint.json`（文件开头含 npm 命令提示）。
- `go test ./... -timeout 180s` 未全部通过，失败项见 `go-tests.txt`。
- `TestPumpPreparedTunnelReader_ClientReadResetAfterIngressDoesNotFail` 在 Windows 重跑 3 次均失败。诊断用例确认 WSAECONNRESET=10054 被当前 `normalizeErrno` 归为 `ERRNO_10054`，`isClientReadResetError` 未识别 Windows 错误文案。这会使已开始收取响应后的客户端重置被误判为上游失败，有影响健康统计/熔断判断的风险。建议补充 Windows socket errno 映射并做跨平台验证。
- `TestRouteRequest_SameIPRotationMissRecreatesLease` 复跑 10 次均在“新旧到期时间相等”断言处失败。源码已重新用 `now + TTL` 构造租约，测试却假定相邻 `time.Now()` 必须不同；证据倾向于当前时钟精度导致断言不稳定，尚不能据此认定租约迁移功能失败。建议使用可注入时钟或给旧租约明确不同的时间基准。
- 本次附加的 7 项小规模审计验证均通过，表示预期风险行为被复现，**不代表风险已修复**。原有空认证相关 2 项测试也通过。

部署补充复核：与端点能力开关、代理认证、HTTP/CONNECT/SOCKS 转发相关的现有定向测试全部通过，证据见 `deployment-endpoint-tests.txt` 和 `deployment-proxy-tests.txt`。`/sub/` 禁用路径和不同代理模式的日志字段另经源码核对。本轮没有修改业务代码，也没有连接用户的实际实例。前述 Windows 错误归类和时钟测试失败属于本机观察，若实际运行 Linux 容器，不能据此认定生产有相同故障。

**已有防护及使用边界。**

已看到管理 API 认证中间件、管理请求体大小限制、数据库参数绑定、Feed 随机令牌与 SHA-256 存储、容器进程降权、GeoIP SHA-256 校验，以及 Feed 中继凭据的 TLS/端点绑定。并未把“没有认证”“SQL 注入”“明文保存所有 Feed token”等不符源码的结论加入报告。

Platform 主要是节点筛选和路由分组，不应当作租户授权边界：普通代理使用全局共享代理令牌，并由客户端选择平台和账号。若启用直连 bypass 或导入直连节点，需用出口网络规则限制内网/元数据地址；这种情况下持有代理凭据的人还可能通过正向代理连接 Resin 能访问的本地服务，端点的“关闭管理”开关不能代替出口 ACL。这是附加配置条件下的风险，并非已确认用户启用了直连。本报告中“2261 直接路径关闭”不表示所有可能的转发路径都已验证隔离。订阅来源能决定节点与部分 TLS 选项，生产环境应使用可信 HTTPS 来源，避免导入跳过证书校验的配置。

**复核命令（PowerShell，在仓库根目录执行）。**

```powershell
# 若尚未构建 WebUI，Go embed 需要先具备 dist。
npm.cmd --prefix webui ci --ignore-scripts --no-fund --no-audit
npm.cmd --prefix webui run build

# 测试源码通过 Go overlay 注入，不改动业务包文件。
& './docs/security-audit-2026-10-11/run-checks.ps1'

go test ./internal/api ./internal/config -run 'Test(AuthMiddleware_EmptyConfiguredToken_DisablesAuth|LoadEnvConfig_EmptyTokensAllowedWhenDefined)$' -count=1 -v
go test ./... -timeout 180s
npm.cmd --prefix webui run lint
npm.cmd --prefix webui audit --omit=dev

# 本次安装到仓库忽略目录的扫描工具；使用 PATH 中的实际 Go 版本分析。
& './.local/security-audit-tools/govulncheck.exe' -json -tags 'with_quic,with_wireguard,with_grpc,with_utls' ./...
python './docs/security-audit-2026-10-11/summarize-dependencies.py'
```

附加测试是“当前行为证据”，修复后部分测试理应失败，应将断言改为期望的安全行为后纳入正式回归。运行脚本仅打开短暂回环监听，不写生产数据库或改动服务配置，测试退出会关闭监听；无需对现有部署回滚。重新扫描前应保存旧证据，避免覆盖用于对照的结果。

按当前部署的建议顺序：先核实 2260 在宿主机确为回环绑定，保留 2261 的 TLS、认证和管理/反向代理关闭设置；随后补齐 HTTP 请求头/空闲超时及连接数限制，核对并更新实际运行的 Go 和网络依赖；再限制订阅/探测响应大小并检查来源可信度。日志按真实流量模式检查，发现敏感值后再安排脱敏、清理和轮换。后台同源与公开订阅限流问题保留为本地使用条件或将来功能开放时的整改项，不再排在当前公网风险的最前面。
