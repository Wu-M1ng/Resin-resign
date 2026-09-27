export type FeedFormat = "singbox" | "clash-meta" | "v2ray-base64" | "uri";
export type FeedUnsupportedPolicy = "skip" | "error";

export type SubscriptionFeed = {
  id: string;
  name: string;
  platform_id: string;
  subscription_ids: string[];
  default_format: FeedFormat;
  enabled_formats: FeedFormat[];
  unsupported_policy: FeedUnsupportedPolicy;
  pretty: boolean;
  enabled: boolean;
  token_prefix: string;
  created_at: string;
  updated_at: string;
  created_at_ns?: number;
  updated_at_ns?: number;
  node_count?: number;
  healthy_node_count?: number;
};

export type FeedPage = {
  items: SubscriptionFeed[];
  total: number;
  limit: number;
  offset: number;
};

export type FeedInput = {
  name: string;
  platform_id: string;
  subscription_ids: string[];
  default_format: FeedFormat;
  enabled_formats: FeedFormat[];
  unsupported_policy: FeedUnsupportedPolicy;
  pretty: boolean;
  enabled: boolean;
};

export type FeedCreateResult = SubscriptionFeed & {
  public_token?: string;
};

export type FeedPreview = {
  format: FeedFormat;
  body: string;
  content_type?: string;
  node_count?: number;
  skipped_count?: number;
  skipped_types?: string[];
  generated_at?: string;
};
