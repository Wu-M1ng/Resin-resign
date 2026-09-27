import { apiRequest } from "../../lib/api-client";
import type {
  FeedCreateResult,
  FeedFormat,
  FeedInput,
  FeedPage,
  FeedPreview,
  SubscriptionFeed,
} from "./types";

const basePath = "/api/v1/feeds";

type ApiFeed = Partial<SubscriptionFeed> & {
  enabled_formats?: unknown;
  enabled_formats_json?: string;
  public_token?: string;
  created_at_ns?: number;
  updated_at_ns?: number;
};

const formats: FeedFormat[] = ["singbox", "clash-meta", "v2ray-base64", "uri"];

function parseFormat(value: unknown): FeedFormat | null {
  return typeof value === "string" && formats.includes(value as FeedFormat) ? value as FeedFormat : null;
}

function parseFormats(raw: ApiFeed): FeedFormat[] {
  let values: unknown = raw.enabled_formats;
  if (values === undefined && typeof raw.enabled_formats_json === "string") {
    try {
      values = JSON.parse(raw.enabled_formats_json);
    } catch {
      values = [];
    }
  }
  if (!Array.isArray(values)) {
    return [];
  }
  return Array.from(new Set(values.map(parseFormat).filter((item): item is FeedFormat => item !== null)));
}

function parseTimestamp(value: unknown, ns: unknown): string {
  if (typeof value === "string") {
    return value;
  }
  if (typeof ns === "number" && Number.isFinite(ns) && ns > 0) {
    return new Date(ns / 1_000_000).toISOString();
  }
  return "";
}

export function normalizeFeed(raw: ApiFeed): SubscriptionFeed {
  const enabledFormats = parseFormats(raw);
  const defaultFormat = parseFormat(raw.default_format) ?? enabledFormats[0] ?? "clash-meta";
  const subscriptionIDs = Array.isArray(raw.subscription_ids)
    ? raw.subscription_ids.filter((item): item is string => typeof item === "string")
    : [];
  return {
    id: typeof raw.id === "string" ? raw.id : "",
    name: typeof raw.name === "string" ? raw.name : "",
    platform_id: typeof raw.platform_id === "string" ? raw.platform_id : "",
    subscription_ids: subscriptionIDs,
    default_format: defaultFormat,
    enabled_formats: enabledFormats,
    unsupported_policy: raw.unsupported_policy === "error" ? "error" : "skip",
    pretty: raw.pretty !== false,
    enabled: raw.enabled !== false,
    token_prefix: typeof raw.token_prefix === "string" ? raw.token_prefix : "",
    created_at: parseTimestamp(raw.created_at, raw.created_at_ns),
    updated_at: parseTimestamp(raw.updated_at, raw.updated_at_ns),
    created_at_ns: typeof raw.created_at_ns === "number" ? raw.created_at_ns : undefined,
    updated_at_ns: typeof raw.updated_at_ns === "number" ? raw.updated_at_ns : undefined,
    node_count: typeof raw.node_count === "number" ? raw.node_count : undefined,
    healthy_node_count: typeof raw.healthy_node_count === "number" ? raw.healthy_node_count : undefined,
  };
}

function normalizeList(raw: FeedPage | ApiFeed[]): FeedPage {
  if (Array.isArray(raw)) {
    return { items: raw.map(normalizeFeed), total: raw.length, limit: raw.length, offset: 0 };
  }
  return {
    items: Array.isArray(raw.items) ? raw.items.map(normalizeFeed) : [],
    total: typeof raw.total === "number" ? raw.total : 0,
    limit: typeof raw.limit === "number" ? raw.limit : 50,
    offset: typeof raw.offset === "number" ? raw.offset : 0,
  };
}

export async function listFeeds(): Promise<FeedPage> {
  const raw = await apiRequest<FeedPage | ApiFeed[]>(basePath);
  return normalizeList(raw);
}

export async function createFeed(input: FeedInput): Promise<FeedCreateResult> {
  const raw = await apiRequest<ApiFeed>(basePath, { method: "POST", body: input });
  return { ...normalizeFeed(raw), public_token: typeof raw.public_token === "string" ? raw.public_token : undefined };
}

export async function updateFeed(id: string, input: Partial<FeedInput>): Promise<SubscriptionFeed> {
  const raw = await apiRequest<ApiFeed>(`${basePath}/${encodeURIComponent(id)}`, { method: "PATCH", body: input });
  return normalizeFeed(raw);
}

export async function deleteFeed(id: string): Promise<void> {
  await apiRequest<void>(`${basePath}/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function rotateFeedToken(id: string): Promise<FeedCreateResult> {
  const raw = await apiRequest<ApiFeed>(`${basePath}/${encodeURIComponent(id)}/actions/rotate-token`, { method: "POST" });
  return { ...normalizeFeed(raw), public_token: typeof raw.public_token === "string" ? raw.public_token : undefined };
}

export async function previewFeed(id: string, format: FeedFormat): Promise<FeedPreview> {
  const raw = await apiRequest<FeedPreview>(`${basePath}/${encodeURIComponent(id)}/preview/${encodeURIComponent(format)}`);
  return {
    format,
    body: typeof raw.body === "string" ? raw.body : "",
    content_type: raw.content_type,
    node_count: raw.node_count,
    skipped_count: raw.skipped_count,
    skipped_types: raw.skipped_types,
    generated_at: raw.generated_at,
  };
}
