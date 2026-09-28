import { useQuery } from "@tanstack/react-query";
import { Check, Copy, Info } from "lucide-react";
import { useMemo, useState } from "react";
import { Button } from "../../components/ui/Button";
import { Input } from "../../components/ui/Input";
import { useI18n } from "../../i18n";
import { getEnvConfig } from "../systemConfig/api";

const PROXY_TOKEN_STORAGE_KEY = "resin_proxy_token";
const EXTERNAL_HOST_STORAGE_KEY = "resin_external_proxy_host";
const EXTERNAL_PORT_STORAGE_KEY = "resin_external_proxy_port";
const TOKEN_PLACEHOLDER = "<token>";

type ProxyEndpoint = { scheme: string; host: string };

function loadStoredProxyToken(): string {
  if (typeof window === "undefined") {
    return "";
  }
  return window.localStorage.getItem(PROXY_TOKEN_STORAGE_KEY) ?? "";
}

function persistProxyToken(value: string): void {
  if (typeof window === "undefined") {
    return;
  }
  if (value) {
    window.localStorage.setItem(PROXY_TOKEN_STORAGE_KEY, value);
  } else {
    window.localStorage.removeItem(PROXY_TOKEN_STORAGE_KEY);
  }
}

function loadStoredValue(key: string): string {
  if (typeof window === "undefined") {
    return "";
  }
  return window.localStorage.getItem(key) ?? "";
}

function persistStoredValue(key: string, value: string): void {
  if (typeof window === "undefined") {
    return;
  }
  if (value) {
    window.localStorage.setItem(key, value);
  } else {
    window.localStorage.removeItem(key);
  }
}

function formatHostWithPort(hostname: string, port: number): string {
  const host = hostname.includes(":") && !hostname.startsWith("[") ? `[${hostname}]` : hostname;
  return port ? `${host}:${port}` : host;
}

function configuredApiEndpoint(): ProxyEndpoint | null {
  const apiBase = import.meta.env.VITE_API_BASE_URL?.trim();
  if (!apiBase || !/^https?:\/\//i.test(apiBase)) {
    return null;
  }
  try {
    const url = new URL(apiBase);
    return { scheme: url.protocol.replace(/:$/, ""), host: url.host };
  } catch {
    return null;
  }
}

function defaultExternalHost(): string {
  const apiBase = import.meta.env.VITE_API_BASE_URL?.trim();
  if (apiBase && /^https?:\/\//i.test(apiBase)) {
    try {
      return new URL(apiBase).hostname;
    } catch {
      // Fall through to the browser host when the build-time API URL is invalid.
    }
  }
  if (typeof window !== "undefined" && window.location.hostname) {
    return window.location.hostname;
  }
  return "";
}

function currentProxyEndpoint(fallbackPort: number): ProxyEndpoint {
  const configured = configuredApiEndpoint();
  if (configured) {
    return configured;
  }

  if (typeof window === "undefined") {
    return { scheme: "http", host: fallbackPort ? `127.0.0.1:${fallbackPort}` : "127.0.0.1:2260" };
  }

  const scheme = currentScheme();
  const expectedPort = fallbackPort || 2260;
  const currentPort = Number(window.location.port);
  if (import.meta.env.DEV && window.location.hostname && currentPort && currentPort !== expectedPort) {
    return { scheme, host: formatHostWithPort(window.location.hostname, expectedPort) };
  }
  if (window.location.host) {
    return { scheme, host: window.location.host };
  }
  return {
    scheme,
    host: window.location.hostname
      ? formatHostWithPort(window.location.hostname, expectedPort)
      : `127.0.0.1:${expectedPort}`,
  };
}

function currentScheme(): string {
  if (typeof window === "undefined" || !window.location.protocol) {
    return "http";
  }
  return window.location.protocol.replace(/:$/, "");
}

// Encode a URL segment/userinfo component, but keep the literal <token>
// placeholder readable so users can see where to paste the real token.
function encodeSegment(value: string): string {
  return value === TOKEN_PLACEHOLDER ? value : encodeURIComponent(value);
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

type ParsedTarget = { protocol: string; rest: string };

function parseTarget(raw: string): ParsedTarget | null {
  const trimmed = raw.trim();
  if (!trimmed) {
    return null;
  }
  const withScheme = /^[a-zA-Z][\w+.-]*:\/\//.test(trimmed) ? trimmed : `https://${trimmed}`;
  let url: URL;
  try {
    url = new URL(withScheme);
  } catch {
    return null;
  }
  const protocol = url.protocol.replace(/:$/, "").toLowerCase();
  if (protocol !== "http" && protocol !== "https") {
    return null;
  }
  const path = url.pathname === "/" ? "" : url.pathname;
  return { protocol, rest: `${url.host}${path}${url.search}` };
}

type CopyFieldProps = {
  label: string;
  value: string;
  hint?: string;
  copyLabel: string;
  copiedLabel: string;
};

function CopyField({ label, value, hint, copyLabel, copiedLabel }: CopyFieldProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(value);
      } else {
        const area = document.createElement("textarea");
        area.value = value;
        area.style.position = "fixed";
        area.style.opacity = "0";
        document.body.appendChild(area);
        area.select();
        document.execCommand("copy");
        document.body.removeChild(area);
      }
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  };

  return (
    <div className="platform-access-field">
      <div className="platform-access-field-head">
        <span className="platform-access-field-label">{label}</span>
        {hint ? <span className="platform-access-field-hint">{hint}</span> : null}
      </div>
      <div className="platform-access-field-body">
        <code className="platform-access-value" title={value}>
          {value}
        </code>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => void handleCopy()}
          className="platform-access-copy-btn"
        >
          {copied ? <Check size={14} /> : <Copy size={14} />}
          {copied ? copiedLabel : copyLabel}
        </Button>
      </div>
    </div>
  );
}

type PlatformAccessPanelProps = {
  platformName: string;
};

export function PlatformAccessPanel({ platformName }: PlatformAccessPanelProps) {
  const { t } = useI18n();
  const [account, setAccount] = useState("");
  const [token, setToken] = useState(loadStoredProxyToken);
  const [target, setTarget] = useState("https://api.ipify.org");
  const [externalHost, setExternalHost] = useState(
    () => loadStoredValue(EXTERNAL_HOST_STORAGE_KEY) || defaultExternalHost(),
  );
  const [externalPort, setExternalPort] = useState(() => loadStoredValue(EXTERNAL_PORT_STORAGE_KEY));

  const envQuery = useQuery({
    queryKey: ["system-env-config"],
    queryFn: getEnvConfig,
    staleTime: 60_000,
  });

  const env = envQuery.data;
  const proxyTokenConfigured = env?.proxy_token_set ?? true;
  const endpoint = currentProxyEndpoint(env?.resin_port ?? 2260);
  const host = endpoint.host;
  const scheme = endpoint.scheme;
  const externalPortForDisplay = externalPort || String(env?.resin_port ?? 2260);

  const handleTokenChange = (value: string) => {
    setToken(value);
    persistProxyToken(value.trim());
  };

  const handleExternalHostChange = (value: string) => {
    setExternalHost(value);
    persistStoredValue(EXTERNAL_HOST_STORAGE_KEY, value.trim());
  };

  const handleExternalPortChange = (value: string) => {
    setExternalPort(value);
    persistStoredValue(EXTERNAL_PORT_STORAGE_KEY, value.trim());
  };

  const urls = useMemo(() => {
    const platform = platformName.trim() || "Default";
    const acc = account.trim();
    const tokenRaw = token.trim();
    const sep = ".";

    // Raw identity/credential are used verbatim for the curl -U value.
    const identityRaw = acc ? `${platform}${sep}${acc}` : platform;
    // Encoded identity is reused for URL userinfo and reverse path segment.
    const identityEnc = acc
      ? `${encodeSegment(platform)}${sep}${encodeSegment(acc)}`
      : encodeSegment(platform);
    // Production endpoints always require a token. Keep a visible placeholder
    // until the administrator enters the configured value.
    const forwardToken = tokenRaw || TOKEN_PLACEHOLDER;

    const forwardCredential = forwardToken ? `${identityRaw}:${forwardToken}` : identityRaw;
    const userInfo = forwardToken ? `${identityEnc}:${encodeSegment(forwardToken)}` : identityEnc;

    const httpForward = `http://${userInfo}@${host}`;
    const socksForward = `socks5h://${userInfo}@${host}`;

    const parsedExternalPort = Number(externalPortForDisplay.trim());
    const externalPortValid =
      /^\d+$/.test(externalPortForDisplay.trim()) &&
      Number.isInteger(parsedExternalPort) &&
      parsedExternalPort >= 1 &&
      parsedExternalPort <= 65535;
    const externalAddress =
      externalHost.trim() && externalPortValid ? formatHostWithPort(externalHost.trim(), parsedExternalPort) : "";
    const externalHttp = externalAddress ? `http://${userInfo}@${externalAddress}` : "";
    const externalSocks = externalAddress ? `socks5://${userInfo}@${externalAddress}` : "";

    const reverseTokenSeg = encodeSegment(tokenRaw || TOKEN_PLACEHOLDER);
    const parsed = parseTarget(target);
    const reverseUrl = parsed
      ? `${scheme}://${host}/${reverseTokenSeg}/${identityEnc}/${parsed.protocol}/${parsed.rest}`
      : "";

    const curlForward = [
      "curl",
      "-x",
      shellQuote(`http://${host}`),
      "-U",
      shellQuote(forwardCredential),
      shellQuote("https://api.ipify.org"),
    ].join(" ");
    const curlReverse = reverseUrl ? `curl ${shellQuote(reverseUrl)}` : "";

    return {
      httpForward,
      socksForward,
      externalHttp,
      externalSocks,
      externalAddress,
      reverseUrl,
      curlForward,
      curlReverse,
    };
  }, [platformName, account, token, host, scheme, target, externalHost, externalPortForDisplay]);

  const copyLabel = t("复制");
  const copiedLabel = t("已复制");
  const tokenMissing = !token.trim();
  const tokenInputValue = token;

  return (
    <section className="platform-detail-tabpanel platform-access-section">
      <div className="platform-drawer-section-head">
        <h4>{t("接入方式")}</h4>
        <p>{t("填写账号与代理 token，一键复制正向/反向代理地址。")}</p>
      </div>

      <div className="platform-access-inputs">
        <div className="field-group">
          <label className="field-label" htmlFor="access-account">
            {t("业务账号（可选）")}
          </label>
          <Input
            id="access-account"
            placeholder={t("例如 user_tom，留空则只按平台路由")}
            value={account}
            onChange={(event) => setAccount(event.target.value)}
          />
        </div>

        <div className="field-group">
          <label className="field-label field-label-with-info" htmlFor="access-token">
            <span>{t("代理 token")}</span>
            <span
              className="subscription-info-icon"
              title={t("即后端 RESIN_PROXY_TOKEN。仅保存在浏览器本地，不会上传服务器。")}
              aria-label={t("即后端 RESIN_PROXY_TOKEN。仅保存在浏览器本地，不会上传服务器。")}
              tabIndex={0}
            >
              <Info size={13} />
            </span>
          </label>
          <Input
            id="access-token"
            type="password"
            placeholder={proxyTokenConfigured ? t("填写 RESIN_PROXY_TOKEN") : t("请先配置非空 RESIN_PROXY_TOKEN")}
            value={tokenInputValue}
            onChange={(event) => handleTokenChange(event.target.value)}
            disabled={false}
            autoComplete="off"
          />
          {!proxyTokenConfigured ? <p className="muted" style={{ marginTop: 4, fontSize: 12 }}>{t("后端未配置非空代理 token，代理请求会被拒绝")}</p> : null}
          {tokenMissing ? (
            <p className="muted" style={{ marginTop: 4, fontSize: 12 }}>
              {t("尚未填写 token，地址中将以 <token> 占位，请替换为实际值。")}
            </p>
          ) : null}
        </div>
      </div>

      <div className="platform-access-group">
        <h5>{t("正向代理")}</h5>
        <CopyField
          label={t("HTTP 正向代理")}
          value={urls.httpForward}
          copyLabel={copyLabel}
          copiedLabel={copiedLabel}
        />
        <CopyField
          label={t("SOCKS5 正向代理")}
          value={urls.socksForward}
          copyLabel={copyLabel}
          copiedLabel={copiedLabel}
        />
        <CopyField
          label={t("curl 示例")}
          value={urls.curlForward}
          copyLabel={copyLabel}
          copiedLabel={copiedLabel}
        />
      </div>

      <div className="platform-access-group">
        <h5>{t("外部导入")}</h5>
        <p className="platform-access-note">
          <Info size={14} />
          <span>{t("填写客户端可访问的公网主机和端口，生成的地址会包含端口号。")}</span>
        </p>
        <div className="platform-access-inputs">
          <div className="field-group">
            <label className="field-label" htmlFor="access-external-host">
              {t("外部主机")}
            </label>
            <Input
              id="access-external-host"
              placeholder={t("例如 resin.example.com")}
              value={externalHost}
              onChange={(event) => handleExternalHostChange(event.target.value)}
              autoComplete="off"
            />
          </div>
          <div className="field-group">
            <label className="field-label" htmlFor="access-external-port">
              {t("外部端口")}
            </label>
            <Input
              id="access-external-port"
              type="number"
              min={1}
              max={65535}
              placeholder={t("例如 2261")}
              value={externalPortForDisplay}
              onChange={(event) => handleExternalPortChange(event.target.value)}
              inputMode="numeric"
            />
          </div>
        </div>
        {urls.externalAddress ? (
          <>
            <CopyField
              label={t("HTTP 外部导入")}
              value={urls.externalHttp}
              copyLabel={copyLabel}
              copiedLabel={copiedLabel}
            />
            <CopyField
              label={t("SOCKS5 外部导入")}
              value={urls.externalSocks}
              copyLabel={copyLabel}
              copiedLabel={copiedLabel}
            />
            <p className="muted" style={{ margin: 0, fontSize: 12 }}>
              {t("这里生成的是普通 TCP 代理地址；TLS SOCKS5 需要在客户端单独配置 TLS。")}
            </p>
          </>
        ) : (
          <p className="muted" style={{ margin: 0, fontSize: 12 }}>
            {t("请输入外部主机和 1-65535 范围内的端口。")}
          </p>
        )}
      </div>

      <div className="platform-access-group">
        <h5>{t("反向代理")}</h5>
        <div className="field-group">
          <label className="field-label" htmlFor="access-target">
            {t("目标网址")}
          </label>
          <Input
            id="access-target"
            placeholder={t("例如 https://api.ipify.org")}
            value={target}
            onChange={(event) => setTarget(event.target.value)}
          />
        </div>
        {urls.reverseUrl ? (
          <>
            <CopyField
              label={t("反向代理地址")}
              value={urls.reverseUrl}
              copyLabel={copyLabel}
              copiedLabel={copiedLabel}
            />
            <CopyField
              label={t("curl 示例")}
              value={urls.curlReverse}
              copyLabel={copyLabel}
              copiedLabel={copiedLabel}
            />
          </>
        ) : (
          <p className="muted" style={{ fontSize: 12 }}>
            {t("请输入合法的 http/https 目标网址以生成反向代理地址。")}
          </p>
        )}
      </div>
    </section>
  );
}
