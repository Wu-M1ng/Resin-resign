import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Clipboard, Eye, Pencil, Plus, RefreshCw, RotateCw, Trash2, X } from "lucide-react";
import { useMemo, useState } from "react";
import { Badge } from "../../components/ui/Badge";
import { Button } from "../../components/ui/Button";
import { Card } from "../../components/ui/Card";
import { Input } from "../../components/ui/Input";
import { Select } from "../../components/ui/Select";
import { Switch } from "../../components/ui/Switch";
import { Textarea } from "../../components/ui/Textarea";
import { ToastContainer } from "../../components/ui/Toast";
import { useToast } from "../../hooks/useToast";
import { useI18n } from "../../i18n";
import { formatApiErrorMessage } from "../../lib/error-message";
import { formatDateTime } from "../../lib/time";
import { listPlatforms } from "../platforms/api";
import type { Platform } from "../platforms/types";
import { listSubscriptions } from "../subscriptions/api";
import type { Subscription } from "../subscriptions/types";
import {
  createFeed,
  deleteFeed,
  listFeeds,
  previewFeed,
  rotateFeedToken,
  updateFeed,
} from "./api";
import type { FeedFormat, FeedInput, FeedPreview, SubscriptionFeed } from "./types";

type FeedSourceMode = "platform" | "subscriptions";

const FORMATS: Array<{ value: FeedFormat; label: string }> = [
  { value: "clash-meta", label: "Clash Meta / Mihomo" },
  { value: "singbox", label: "sing-box JSON" },
  { value: "v2ray-base64", label: "V2Ray Base64" },
  { value: "uri", label: "URI 列表" },
];

const DEFAULT_FORM: FeedInput = {
  name: "",
  platform_id: "",
  subscription_ids: [],
  default_format: "clash-meta",
  enabled_formats: ["clash-meta", "singbox", "v2ray-base64", "uri"],
  unsupported_policy: "skip",
  pretty: true,
  enabled: true,
};

function formatLabel(format: FeedFormat, t: (text: string) => string): string {
  return t(FORMATS.find((item) => item.value === format)?.label ?? format);
}

function toForm(feed: SubscriptionFeed): FeedInput {
  return {
    name: feed.name,
    platform_id: feed.platform_id,
    subscription_ids: feed.subscription_ids,
    default_format: feed.default_format,
    enabled_formats: feed.enabled_formats,
    unsupported_policy: feed.unsupported_policy,
    pretty: feed.pretty,
    enabled: feed.enabled,
  };
}

function publicURL(token: string, format: FeedFormat): string {
  return `${window.location.origin}/sub/${encodeURIComponent(token)}/${format}`;
}

export function FeedsPage() {
  const { t } = useI18n();
  const { toasts, showToast, dismissToast } = useToast();
  const queryClient = useQueryClient();
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<SubscriptionFeed | null>(null);
  const [form, setForm] = useState<FeedInput>(DEFAULT_FORM);
  const [sourceMode, setSourceMode] = useState<FeedSourceMode>("platform");
  const [token, setToken] = useState<{ feed: SubscriptionFeed; value: string } | null>(null);
  const [preview, setPreview] = useState<FeedPreview | null>(null);
  const [previewFeedID, setPreviewFeedID] = useState<string | null>(null);
  const [previewFormat, setPreviewFormat] = useState<FeedFormat>("clash-meta");

  const feedsQuery = useQuery({ queryKey: ["feeds"], queryFn: listFeeds, refetchInterval: 30_000 });
  const platformsQuery = useQuery({ queryKey: ["platforms", "feed-picker"], queryFn: () => listPlatforms({ limit: 200 }) });
  const subscriptionsQuery = useQuery({ queryKey: ["subscriptions", "feed-picker"], queryFn: () => listSubscriptions({ limit: 200 }) });
  const feeds = feedsQuery.data?.items ?? [];
  const platforms: Platform[] = platformsQuery.data?.items ?? [];
  const subscriptions: Subscription[] = subscriptionsQuery.data?.items ?? [];

  const selectedPlatform = useMemo(
    () => platforms.find((platform) => platform.id === form.platform_id),
    [form.platform_id, platforms],
  );

  const toggleSubscription = (id: string) => {
    setForm((current) => ({
      ...current,
      subscription_ids: current.subscription_ids.includes(id)
        ? current.subscription_ids.filter((item) => item !== id)
        : [...current.subscription_ids, id],
    }));
  };

  const invalidate = async () => queryClient.invalidateQueries({ queryKey: ["feeds"] });
  const createMutation = useMutation({
    mutationFn: createFeed,
    onSuccess: async (created) => {
      await invalidate();
      setModalOpen(false);
      setEditing(null);
      if (created.public_token) setToken({ feed: created, value: created.public_token });
      showToast("success", t("订阅输出 {{name}} 创建成功", { name: created.name }));
    },
    onError: (error) => showToast("error", formatApiErrorMessage(error, t)),
  });
  const updateMutation = useMutation({
    mutationFn: ({ id, input }: { id: string; input: FeedInput }) => updateFeed(id, input),
    onSuccess: async (updated) => {
      await invalidate();
      setModalOpen(false);
      setEditing(null);
      showToast("success", t("订阅输出 {{name}} 已更新", { name: updated.name }));
    },
    onError: (error) => showToast("error", formatApiErrorMessage(error, t)),
  });
  const deleteMutation = useMutation({
    mutationFn: deleteFeed,
    onSuccess: async () => {
      await invalidate();
      showToast("success", t("订阅输出已删除"));
    },
    onError: (error) => showToast("error", formatApiErrorMessage(error, t)),
  });
  const rotateMutation = useMutation({
    mutationFn: rotateFeedToken,
    onSuccess: async (rotated) => {
      await invalidate();
      if (rotated.public_token) setToken({ feed: rotated, value: rotated.public_token });
      showToast("success", t("令牌已轮换，旧链接立即失效"));
    },
    onError: (error) => showToast("error", formatApiErrorMessage(error, t)),
  });
  const previewMutation = useMutation({
    mutationFn: ({ id, format }: { id: string; format: FeedFormat }) => previewFeed(id, format),
    onSuccess: (result) => setPreview(result),
    onError: (error) => showToast("error", formatApiErrorMessage(error, t)),
  });

  const openCreate = () => {
    setEditing(null);
    setSourceMode("platform");
    setForm({ ...DEFAULT_FORM });
    setModalOpen(true);
  };
  const openEdit = (feed: SubscriptionFeed) => {
    setEditing(feed);
    setSourceMode(feed.platform_id ? "platform" : "subscriptions");
    setForm(toForm(feed));
    setModalOpen(true);
  };
  const chooseSourceMode = (mode: FeedSourceMode) => {
    setSourceMode(mode);
    setForm((current) => ({
      ...current,
      platform_id: mode === "platform" ? current.platform_id : "",
      subscription_ids: mode === "subscriptions" ? current.subscription_ids : [],
    }));
  };
  const toggleFormat = (format: FeedFormat) => {
    setForm((current) => {
      const enabled = current.enabled_formats.includes(format);
      const next = enabled
        ? current.enabled_formats.filter((item) => item !== format)
        : [...current.enabled_formats, format];
      const defaultFormat = next.includes(current.default_format) ? current.default_format : (next[0] ?? "clash-meta");
      return { ...current, enabled_formats: next, default_format: defaultFormat };
    });
  };
  const save = () => {
    if (!form.name.trim()) {
      showToast("error", t("订阅输出名称不能为空"));
      return;
    }
    const platformID = sourceMode === "platform" ? form.platform_id.trim() : "";
    const subscriptionIDs = sourceMode === "subscriptions" ? form.subscription_ids : [];
    if (!platformID && !subscriptionIDs.length) {
      showToast("error", t("请选择一个节点来源"));
      return;
    }
    if (!form.enabled_formats.length) {
      showToast("error", t("至少启用一种输出格式"));
      return;
    }
    const input = { ...form, name: form.name.trim(), platform_id: platformID, subscription_ids: subscriptionIDs };
    if (editing) {
      updateMutation.mutate({ id: editing.id, input });
    } else {
      createMutation.mutate(input);
    }
  };
  const copy = async (value: string) => {
    try {
      await navigator.clipboard.writeText(value);
      showToast("success", t("已复制到剪贴板"));
    } catch {
      showToast("error", t("复制失败，请手动复制"));
    }
  };

  return (
    <section className="feeds-page">
      <header className="module-header">
        <div>
          <h2>{t("订阅输出")}</h2>
          <p className="module-description">{t("将节点池按平台或原始订阅来源整理为客户端可直接使用的订阅地址。")}</p>
        </div>
      </header>
      <ToastContainer toasts={toasts} onDismiss={dismissToast} />

      <Card className="feeds-list-card platform-directory-card">
        <div className="list-card-header">
          <div><h3>{t("整合订阅列表")}</h3><p>{t("共 {{count}} 个整合订阅", { count: feeds.length })}</p></div>
          <div className="feeds-toolbar-actions">
            <Button variant="secondary" size="sm" onClick={() => void feedsQuery.refetch()} disabled={feedsQuery.isFetching}>
              <RefreshCw size={15} className={feedsQuery.isFetching ? "spin" : undefined} />{t("刷新")}
            </Button>
            <Button size="sm" onClick={openCreate}><Plus size={15} />{t("新建整合订阅")}</Button>
          </div>
        </div>
        {feedsQuery.isLoading ? <p className="muted">{t("正在加载订阅输出数据...")}</p> : null}
        {feedsQuery.isError ? <div className="callout callout-error"><AlertTriangle size={14} /><span>{formatApiErrorMessage(feedsQuery.error, t)}</span></div> : null}
        {!feedsQuery.isLoading && !feeds.length ? <div className="empty-box"><Eye size={16} /><p>{t("暂无整合订阅，请先创建一个。")}</p></div> : null}
        {feeds.length ? (
          <div className="data-table-wrap feeds-table-wrap">
            <table className="data-table feeds-table">
              <thead><tr><th>{t("名称")}</th><th>{t("来源")}</th><th>{t("输出格式")}</th><th>{t("状态")}</th><th>{t("令牌")}</th><th>{t("更新时间")}</th><th>{t("操作")}</th></tr></thead>
              <tbody>
                {feeds.map((feed) => (
                  <tr key={feed.id}>
                    <td><strong>{feed.name}</strong></td>
                    <td>{feed.platform_id
                      ? (platforms.find((item) => item.id === feed.platform_id)?.name ?? feed.platform_id)
                      : (feed.subscription_ids.map((id) => subscriptions.find((item) => item.id === id)?.name ?? id).join(", ") || t("未选择来源"))}</td>
                    <td><div className="feeds-format-list">{feed.enabled_formats.map((format) => <Badge key={format} variant={format === feed.default_format ? "info" : "neutral"}>{formatLabel(format, t)}</Badge>)}</div></td>
                    <td><Badge variant={feed.enabled ? "success" : "muted"}>{feed.enabled ? t("已启用") : t("已停用")}</Badge></td>
                    <td><code className="feeds-token-prefix">{feed.token_prefix ? `${feed.token_prefix}...` : t("未生成")}</code></td>
                    <td>{formatDateTime(feed.updated_at)}</td>
                    <td><div className="feeds-row-actions">
                      <Button variant="ghost" size="sm" title={t("预览")} aria-label={t("预览")} onClick={() => { setPreviewFeedID(feed.id); setPreviewFormat(feed.default_format); previewMutation.mutate({ id: feed.id, format: feed.default_format }); }}><Eye size={15} /></Button>
                      <Button variant="ghost" size="sm" title={t("编辑")} aria-label={t("编辑")} onClick={() => openEdit(feed)}><Pencil size={15} /></Button>
                      <Button variant="ghost" size="sm" title={t("轮换令牌")} aria-label={t("轮换令牌")} onClick={() => { if (window.confirm(t("轮换令牌后旧链接立即失效，确认继续？"))) rotateMutation.mutate(feed.id); }}><RotateCw size={15} /></Button>
                      <Button variant="ghost" size="sm" title={t("删除")} aria-label={t("删除")} onClick={() => { if (window.confirm(t("确认删除整合订阅 {{name}}？", { name: feed.name }))) deleteMutation.mutate(feed.id); }}><Trash2 size={15} /></Button>
                    </div></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </Card>

      {token ? (
        <Card className="feeds-token-card">
          <div className="detail-header"><div><h3>{t("订阅地址")}</h3><p>{t("完整令牌只显示这一次，请立即复制保存。")}</p></div><Button variant="ghost" size="sm" onClick={() => setToken(null)} aria-label={t("关闭")} title={t("关闭")}><X size={16} /></Button></div>
          <div className="callout callout-warning"><AlertTriangle size={14} /><span>{t("令牌相当于访问凭证，泄露后请立即轮换。")}</span></div>
          <div className="feeds-url-list">
            {token.feed.enabled_formats.map((format) => { const url = publicURL(token.value, format); return <div className="feeds-url-row" key={format}><span>{formatLabel(format, t)}</span><code>{url}</code><Button variant="secondary" size="sm" onClick={() => void copy(url)}><Clipboard size={14} />{t("复制")}</Button></div>; })}
          </div>
        </Card>
      ) : null}

      {modalOpen ? <div className="modal-overlay" role="dialog" aria-modal="true"><Card className="modal-card feeds-modal-card">
        <div className="modal-header"><h3>{editing ? t("编辑整合订阅") : t("新建整合订阅")}</h3><Button variant="ghost" size="sm" onClick={() => setModalOpen(false)} aria-label={t("关闭")}><X size={16} /></Button></div>
        <div className="form-grid">
          <div className="field-group field-span-2"><label className="field-label" htmlFor="feed-name">{t("名称")}</label><Input id="feed-name" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} placeholder={t("例如：全部节点")} /></div>
          <div className="field-group field-span-2"><span className="field-label">{t("节点来源")}</span><p className="muted">{t("平台和原始订阅来源二选一")}</p><div className="feeds-source-modes"><label className="feeds-source-mode"><input type="radio" name="feed-source-mode" checked={sourceMode === "platform"} onChange={() => chooseSourceMode("platform")} /><span>{t("节点平台")}</span></label><label className="feeds-source-mode"><input type="radio" name="feed-source-mode" checked={sourceMode === "subscriptions"} onChange={() => chooseSourceMode("subscriptions")} /><span>{t("原始订阅来源")}</span></label></div></div>
          {sourceMode === "platform" ? <div className="field-group field-span-2"><label className="field-label" htmlFor="feed-platform">{t("节点平台")}</label><Select id="feed-platform" value={form.platform_id} onChange={(event) => setForm({ ...form, platform_id: event.target.value })}><option value="">{t("请选择平台")}</option>{platforms.map((platform) => <option key={platform.id} value={platform.id}>{platform.name} ({platform.routable_node_count})</option>)}</Select>{selectedPlatform ? <p className="muted">{t("当前平台有 {{count}} 个可路由节点", { count: selectedPlatform.routable_node_count })}</p> : null}</div> : <div className="field-group field-span-2"><span className="field-label">{t("原始订阅来源")}</span><p className="muted">{t("选择一个或多个已启用的原始订阅")}</p><div className="feeds-format-checks">{subscriptions.map((subscription) => <label key={subscription.id} className="feeds-format-check"><input type="checkbox" checked={form.subscription_ids.includes(subscription.id)} onChange={() => toggleSubscription(subscription.id)} /><span>{subscription.name}</span></label>)}</div></div>}
          <div className="field-group field-span-2"><span className="field-label">{t("启用输出格式")}</span><div className="feeds-format-checks">{FORMATS.map((item) => <label key={item.value} className="feeds-format-check"><input type="checkbox" checked={form.enabled_formats.includes(item.value)} onChange={() => toggleFormat(item.value)} /><span>{t(item.label)}</span></label>)}</div></div>
          <div className="field-group"><label className="field-label" htmlFor="feed-default-format">{t("默认格式")}</label><Select id="feed-default-format" value={form.default_format} onChange={(event) => setForm({ ...form, default_format: event.target.value as FeedFormat })}>{form.enabled_formats.map((format) => <option key={format} value={format}>{formatLabel(format, t)}</option>)}</Select></div>
          <div className="field-group"><label className="field-label" htmlFor="feed-policy">{t("不支持协议的处理")}</label><Select id="feed-policy" value={form.unsupported_policy} onChange={(event) => setForm({ ...form, unsupported_policy: event.target.value as FeedInput["unsupported_policy"] })}><option value="skip">{t("跳过并继续输出")}</option><option value="error">{t("报错并拒绝输出")}</option></Select></div>
          <div className="feeds-switch-grid field-span-2"><label className="feeds-switch-item"><span>{t("启用此订阅")}</span><Switch checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} /></label><label className="feeds-switch-item"><span>{t("格式化输出")}</span><Switch checked={form.pretty} onChange={(event) => setForm({ ...form, pretty: event.target.checked })} /></label></div>
        </div>
        <div className="detail-actions"><Button onClick={save} disabled={createMutation.isPending || updateMutation.isPending}>{createMutation.isPending || updateMutation.isPending ? t("保存中...") : t("保存")}</Button><Button variant="secondary" onClick={() => setModalOpen(false)}>{t("取消")}</Button></div>
      </Card></div> : null}

      {preview ? <div className="modal-overlay" role="dialog" aria-modal="true"><Card className="modal-card feeds-preview-card"><div className="modal-header"><div><h3>{t("订阅预览")}</h3><p>{t("节点数：{{count}} · 跳过：{{skipped}}", { count: preview.node_count ?? 0, skipped: preview.skipped_count ?? 0 })}</p></div><Button variant="ghost" size="sm" onClick={() => { setPreview(null); setPreviewFeedID(null); }} aria-label={t("关闭")}><X size={16} /></Button></div><div className="feeds-preview-toolbar"><Select value={previewFormat} onChange={(event) => { const next = event.target.value as FeedFormat; setPreviewFormat(next); if (previewFeedID) previewMutation.mutate({ id: previewFeedID, format: next }); }}>{FORMATS.map((item) => <option key={item.value} value={item.value}>{t(item.label)}</option>)}</Select>{previewMutation.isPending ? <span className="muted">{t("加载中...")}</span> : null}</div><Textarea className="feeds-preview-text" value={preview.body} readOnly rows={18} spellCheck={false} /></Card></div> : null}
    </section>
  );
}
