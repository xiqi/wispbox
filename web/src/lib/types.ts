// API response shapes (mirrors the Go JSON).

export interface Folder {
  name: string;
  role: "inbox" | "sent" | "drafts" | "trash" | "junk" | "custom";
  total: number;
  unseen: number;
}

export interface Address {
  name: string;
  email: string;
}

export interface MessageHeader {
  id: string;
  uid: number;
  folder: string;
  from: Address[] | null;
  to: Address[] | null;
  subject: string;
  date: string;
  seen: boolean;
  answered: boolean;
  flagged: boolean;
  has_attachments: boolean;
  size: number;
}

export interface AttachmentMeta {
  id: string;
  index: number;
  filename: string;
  mime_type: string;
  size: number;
  content_id?: string;
}

export interface MessageDetail {
  id: string;
  header: Omit<MessageHeader, "id">;
  cc: Address[] | null;
  reply_to: Address[] | null;
  message_id: string;
  text_body: string;
  html_body: string;
  has_blocked_images: boolean;
  attachments: AttachmentMeta[];
}

export interface MessagePage {
  messages: MessageHeader[];
  total: number;
  next_cursor?: string;
}

export interface Domain {
  id: number;
  name: string;
  mail_hostname: string;
  status: "pending" | "active" | "error";
  created_at: string;
  updated_at: string;
  mailbox_count?: number;
  cert_status?: string;
  delivery_mode?: string;
  delivery_source?: string;
}

export interface Mailbox {
  id: number;
  domain_id: number;
  local_part: string;
  email: string;
  quota_mb: number;
  enabled: boolean;
  created_at: string;
}

export interface Alias {
  id: number;
  domain_id: number;
  source: string;
  destination: string;
  is_catch_all: boolean;
  enabled: boolean;
}

export interface Relay {
  id: number;
  name: string;
  provider: string;
  host: string;
  port: number;
  username: string;
  tls_mode: "starttls" | "tls";
  enabled: boolean;
  has_secret: boolean;
}

export interface RelayPreset {
  provider: string;
  label: string;
  host: string;
  port: number;
  tls_mode: "starttls" | "tls";
  username_hint: string;
  spf_include: string;
  note: string;
}

export interface Policy {
  id: number;
  scope_type: "global" | "domain" | "mailbox";
  scope_id: number;
  mode: "direct" | "relay" | "inherit";
  relay_id: number | null;
  enabled: boolean;
}

export interface DnsRecord {
  type: string;
  name: string;
  value: string;
  purpose: string;
  explanation: string;
  status: "" | "ok" | "missing" | "mismatch" | "unknown";
  found?: string;
}

export interface Certificate {
  id: number;
  domain_id: number;
  hostname: string;
  status: "pending" | "dns_wait" | "issuing" | "active" | "error";
  challenge_type: string;
  not_before: string;
  not_after: string;
  last_renewed_at: string;
  renew_after: string;
  last_error: string;
}

export interface QueueItem {
  queue_id: string;
  sender: string;
  recipients: string[];
  size_bytes: number;
  arrived_at: string;
  reason: string;
  queue: string;
}

export interface LogLine {
  time: string;
  service: string;
  message: string;
}

export interface AuditEntry {
  id: number;
  actor_type: string;
  actor_id: number;
  action: string;
  target_type: string;
  target_id: string;
  ip: string;
  created_at: string;
}

export interface ServiceEvent {
  id: number;
  service: string;
  event_type: string;
  status: string;
  message: string;
  created_at: string;
}

export interface Overview {
  mode: string;
  uptime_seconds: number;
  services: Record<string, boolean>;
  process_memory: { heap_bytes: number; sys_bytes: number };
  system_memory: { total_bytes?: number; available_bytes?: number };
  disk: { total_bytes?: number; free_bytes?: number; used_bytes?: number };
  queue_count: number;
  domains: Domain[];
  mailbox_count: number;
  certificates: Certificate[];
  recent_errors: ServiceEvent[];
}

export interface UpgradeStatus {
  available: boolean;
  state: "idle" | "running" | "succeeded" | "failed";
  current_version: string;
  current_commit: string;
  current_date: string;
  latest_version?: string;
  update_available: boolean;
  target_version?: string;
  started_at?: string;
  finished_at?: string;
  message?: string;
  error?: string;
}

export interface SetupStatus {
  initialized: boolean;
  has_admin: boolean;
  authenticated: boolean;
  primary_hostname: string;
  server_ipv4: string;
  domains: Domain[] | null;
  mailbox_count: number;
  outbound_smtp_25_open: boolean | null;
  checks: { name: string; ok: boolean; detail: string; required: boolean }[];
}
