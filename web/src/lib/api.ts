export type User = {
  id: string;
  email: string;
  username?: string;
  name: string;
  avatar: string;
  role: string;
  status: string;
  plan: string;
  emailVerified: boolean;
};

let csrfToken = "";
const currentUserStorageKey = "tokhub.currentUser";
const activeWorkspaceStorageKey = "tokhub.activeWorkspaceId";
let currentUserCache: User | null | undefined = readStoredCurrentUser();
let currentUserRequest: Promise<User | null> | null = null;
let activeWorkspaceIdCache = readStoredActiveWorkspaceId();

function currentUserStorage(): Storage | null {
  if (typeof window === "undefined") return null;
  try {
    return window.sessionStorage;
  } catch {
    return null;
  }
}

function readStoredCurrentUser(): User | null | undefined {
  const storage = currentUserStorage();
  if (!storage) return undefined;
  const raw = storage.getItem(currentUserStorageKey);
  if (!raw) return undefined;
  try {
    return JSON.parse(raw) as User;
  } catch {
    storage.removeItem(currentUserStorageKey);
    return undefined;
  }
}

function storeCurrentUser(user: User | null) {
  const storage = currentUserStorage();
  if (!storage) return;
  if (!user) {
    storage.removeItem(currentUserStorageKey);
    return;
  }
  storage.setItem(currentUserStorageKey, JSON.stringify(user));
}

function emitCurrentUserChanged(user: User | null) {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent("tokhub:current-user-changed", { detail: user }));
}

function readStoredActiveWorkspaceId(): string {
  if (typeof window === "undefined") return "";
  try {
    return window.localStorage.getItem(activeWorkspaceStorageKey) || "";
  } catch {
    return "";
  }
}

function storeActiveWorkspaceId(orgId: string) {
  if (typeof window === "undefined") return;
  try {
    if (orgId) window.localStorage.setItem(activeWorkspaceStorageKey, orgId);
    else window.localStorage.removeItem(activeWorkspaceStorageKey);
  } catch {
    // ignore storage failures
  }
}

export function activeWorkspaceId() {
  return activeWorkspaceIdCache;
}

export function setActiveWorkspaceId(orgId: string) {
  activeWorkspaceIdCache = orgId.trim();
  storeActiveWorkspaceId(activeWorkspaceIdCache);
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent("tokhub:workspace-changed", { detail: activeWorkspaceIdCache }));
  }
}

function workspaceHeaders(input: RequestInfo | URL): HeadersInit {
  const orgId = activeWorkspaceIdCache;
  if (!orgId || !shouldAttachWorkspace(input)) return {};
  return { "X-TokHub-Workspace": orgId };
}

function shouldAttachWorkspace(input: RequestInfo | URL) {
  const raw = typeof input === "string" ? input : input instanceof URL ? input.pathname : input.url;
  const path = raw.startsWith("http") ? new URL(raw).pathname : raw;
  return path.startsWith("/api/console") || path.startsWith("/api/me/private-channels");
}

async function readJSON<T>(input: RequestInfo | URL, init?: RequestInit): Promise<T> {
  const response = await fetch(input, {
    ...init,
    headers: {
      ...workspaceHeaders(input),
      ...(init?.headers ?? {})
    }
  });
  const payload = await response.json();
  if (!response.ok) {
    throw new Error(payload?.error?.message ?? "请求失败");
  }
  return payload as T;
}

async function ensureCSRF(): Promise<string> {
  if (csrfToken) return csrfToken;
  const payload = await readJSON<{ csrfToken: string }>("/api/auth/csrf", { credentials: "include" });
  csrfToken = payload.csrfToken;
  return csrfToken;
}

async function writeJSONRequest<T>(input: RequestInfo | URL, body?: unknown, init: RequestInit = {}): Promise<T> {
  const send = async (token: string) => fetch(input, {
    ...init,
    method: init.method ?? "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      "X-CSRF-Token": token,
      ...workspaceHeaders(input),
      ...(init.headers ?? {})
    },
    body: body === undefined ? undefined : JSON.stringify(body)
  });
  let response = await send(await ensureCSRF());
  let payload = await response.json();
  if (!response.ok && payload?.error?.code === "csrf_invalid") {
    csrfToken = "";
    response = await send(await ensureCSRF());
    payload = await response.json();
  }
  if (!response.ok) {
    throw new Error(payload?.error?.message ?? "请求失败");
  }
  return payload as T;
}

async function publicWriteJSONRequest<T>(input: RequestInfo | URL, body?: unknown, init: RequestInit = {}): Promise<T> {
  const response = await fetch(input, {
    ...init,
    method: init.method ?? "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...workspaceHeaders(input),
      ...(init.headers ?? {})
    },
    body: body === undefined ? undefined : JSON.stringify(body)
  });
  const payload = await response.json();
  if (!response.ok) {
    throw new Error(payload?.error?.message ?? "请求失败");
  }
  return payload as T;
}

async function writeFormRequest<T>(input: RequestInfo | URL, body: FormData, init: RequestInit = {}): Promise<T> {
  const send = async (token: string) => fetch(input, {
    ...init,
    method: init.method ?? "POST",
    credentials: "include",
    headers: {
      "X-CSRF-Token": token,
      ...workspaceHeaders(input),
      ...(init.headers ?? {})
    },
    body
  });
  let response = await send(await ensureCSRF());
  let payload = await response.json();
  if (!response.ok && payload?.error?.code === "csrf_invalid") {
    csrfToken = "";
    response = await send(await ensureCSRF());
    payload = await response.json();
  }
  if (!response.ok) {
    throw new Error(payload?.error?.message ?? "请求失败");
  }
  return payload as T;
}

async function writeBlobRequest(input: RequestInfo | URL, body?: unknown, init: RequestInit = {}): Promise<{ blob: Blob; filename: string }> {
  const send = async (token: string) => fetch(input, {
    ...init,
    method: init.method ?? "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      "X-CSRF-Token": token,
      ...workspaceHeaders(input),
      ...(init.headers ?? {})
    },
    body: body === undefined ? undefined : JSON.stringify(body)
  });
  let response = await send(await ensureCSRF());
  if (!response.ok) {
    const payload = await response.json();
    if (payload?.error?.code === "csrf_invalid") {
      csrfToken = "";
      response = await send(await ensureCSRF());
    } else {
      throw new Error(payload?.error?.message ?? "请求失败");
    }
  }
  if (!response.ok) {
    const payload = await response.json();
    throw new Error(payload?.error?.message ?? "请求失败");
  }
  const disposition = response.headers.get("Content-Disposition") ?? "";
  const match = /filename="?([^";]+)"?/i.exec(disposition);
  return { blob: await response.blob(), filename: match?.[1] || "tokhub-platform-channels.csv" };
}

export async function login(identifier: string, password: string): Promise<User> {
  const payload = await writeJSONRequest<{ user: User }>("/api/auth/login", { identifier, email: identifier, password });
  currentUserCache = payload.user as User;
  storeCurrentUser(currentUserCache);
  emitCurrentUserChanged(currentUserCache);
  return payload.user as User;
}

export async function register(email: string, password: string, name?: string): Promise<{ user: User; verificationRequired: boolean; emailDelivery?: string; devVerificationToken?: string }> {
  const payload = await writeJSONRequest<{ user: User; verificationRequired: boolean; emailDelivery?: string; devVerificationToken?: string }>("/api/auth/register", {
    email,
    password,
    ...(name?.trim() ? { name: name.trim() } : {})
  });
  if (!payload.verificationRequired) {
    currentUserCache = payload.user;
    storeCurrentUser(payload.user);
    emitCurrentUserChanged(payload.user);
  }
  return payload;
}

export async function verifyEmail(token: string): Promise<void> {
  await writeJSONRequest("/api/auth/verify-email", { token });
}

export async function forgotPassword(email: string): Promise<{ status: string; devResetToken?: string }> {
  return writeJSONRequest<{ status: string; devResetToken?: string }>("/api/auth/forgot-password", { email });
}

export async function resetPassword(token: string, password: string): Promise<void> {
  await writeJSONRequest("/api/auth/reset-password", { token, password });
}

export async function revokeOtherSessions(): Promise<{ status: string }> {
  return writeJSONRequest<{ status: string }>("/api/auth/revoke-sessions");
}

export async function logout(): Promise<void> {
  await writeJSONRequest("/api/auth/logout");
  clearCurrentUserCache();
  setActiveWorkspaceId("");
  emitCurrentUserChanged(null);
}

export function cachedCurrentUser(): User | null | undefined {
  return currentUserCache;
}

export function clearCurrentUserCache() {
  currentUserCache = undefined;
  currentUserRequest = null;
  storeCurrentUser(null);
}

export async function currentUser(options: { force?: boolean } = {}): Promise<User | null> {
  if (!options.force && currentUserCache !== undefined) {
    return currentUserCache;
  }
  if (!options.force && currentUserRequest) {
    return currentUserRequest;
  }
  currentUserRequest = fetchCurrentUser().finally(() => {
    currentUserRequest = null;
  });
  return currentUserRequest;
}

async function fetchCurrentUser(): Promise<User | null> {
  const response = await fetch("/api/auth/me", { credentials: "include" });
  const payload = await response.json();
  currentUserCache = (payload.user ?? null) as User | null;
  storeCurrentUser(currentUserCache);
  return currentUserCache;
}

export async function updateMeProfile(input: { name: string }): Promise<User> {
  const payload = await writeJSONRequest<{ user: User }>("/api/me/profile", input, { method: "PATCH" });
  currentUserCache = payload.user;
  storeCurrentUser(payload.user);
  emitCurrentUserChanged(payload.user);
  return payload.user;
}

export type PublicOverview = {
  total: number;
  healthy: number;
  functionalDown: number;
  connectivityDown: number;
  degraded: number;
  unknown: number;
  healthyRate: number;
  p95LatencySeconds: number;
  averageLatencyMs: number;
  slowRate: number;
  probeCostToday: number;
  probeTokensToday: number;
  probeRunsToday: number;
  updatedAt: string;
};

export type PublicChannel = {
  id: string;
  publicSlug: string;
  name: string;
  provider: string;
  type: string;
  model: string;
  upstreamModel: string;
  endpoint: string;
  officialSiteUrl: string;
  introTitle: string;
  introSummary: string;
  introBody: string;
  introHighlights: string[];
  logoUrl: string;
  introSourceUrl: string;
  introUpdatedAt?: string;
  status: string;
  statusLabel: string;
  diagnosis: ChannelDiagnosis;
  score: number;
  uptime24h: number;
  successRate: number;
  latencyP95Ms: number;
  l1Status: string;
  l2Status: string;
  l3Status: string;
  l1LatencyMs: number;
  l2LatencyMs: number;
  l3LatencyMs: number;
  tokensUsed: number;
  costUsd: number;
  inputPerMtok: number;
  outputPerMtok: number;
  errorType?: string;
  lastProbeAt: string;
  trend: number[];
  trendBuckets?: TrendBucket[];
  mark: string;
};

export type TrendBucket = {
  key: string;
  label: string;
  value: number | null;
};

export type ChannelDiagnosis = {
  code: string;
  label: string;
  severity: "ok" | "info" | "warn" | "error" | string;
  hint?: string;
};

export function publicChannelPath(channel: Pick<PublicChannel, "id" | "publicSlug">) {
  const ref = (channel.publicSlug || channel.id || "").trim();
  return ref ? `/channels/${encodeURIComponent(ref)}` : "/dashboard";
}

export function externalHTTPHref(value?: string) {
  try {
    const parsed = new URL((value || "").trim());
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return "";
    return parsed.href;
  } catch {
    return "";
  }
}

export function officialExperienceHref(channel: Pick<PublicChannel, "officialSiteUrl" | "endpoint">) {
  return externalHTTPHref(channel.officialSiteUrl) || officialEntryURL(channel.endpoint);
}

export function officialEntryURL(endpoint?: string) {
  try {
    const parsed = new URL((endpoint || "").trim());
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return "";
    const hostname = publicWebsiteHost(parsed.hostname);
    return hostname ? `https://${hostname}` : "";
  } catch {
    return "";
  }
}

export function publicWebsiteHost(hostname: string) {
  const parts = hostname.toLowerCase().split(".").filter(Boolean);
  if (parts.length < 2) return "";
  const apiPrefixes = new Set(["api", "api2", "cc-api", "chat-api", "openapi", "gateway", "proxy", "relay", "upstream"]);
  while (parts.length > 2 && apiPrefixes.has(parts[0])) {
    parts.shift();
  }
  return parts.join(".");
}

export type PublicChannelList = {
  items: PublicChannel[];
  total: number;
  page: number;
  pageSize: number;
};

export type SiteConfig = {
  registrationOpen: boolean;
  showRegisterCta: boolean;
  emailVerificationRequired: boolean;
  adminPath: string;
  brandName: string;
  logoMark: string;
  subtitle: string;
  publicUrl: string;
  footerText: string;
  defaultGatewayPolicy: "latency" | "success" | "cost";
  timezone: string;
  navItems: NavItem[];
  footerLinks: NavItem[];
  monitorModels: MonitorModelConfig[];
  analyticsCode: string;
};

let siteConfigCache: SiteConfig | null = null;
let siteConfigPromise: Promise<SiteConfig> | null = null;
let siteConfigCacheExpiresAt = 0;
const SITE_CONFIG_CACHE_TTL_MS = 30_000;

function rememberSiteConfig(site: SiteConfig) {
  siteConfigCache = site;
  siteConfigCacheExpiresAt = Date.now() + SITE_CONFIG_CACHE_TTL_MS;
  return site;
}

function rememberSitePayload<T extends { site: SiteConfig }>(payload: T) {
  rememberSiteConfig(payload.site);
  return payload;
}

export type MonitorModelConfig = {
  key: string;
  label: string;
  model: string;
  upstreamModel: string;
  type: string;
  enabled: boolean;
  defaultSelected: boolean;
  inputPerMtok: number;
  outputPerMtok: number;
  aliases: string[];
};

export type AdminSettingsSummary = {
  platformChannels: number;
  users: number;
  orgs: number;
  recommendPicks: number;
  privateChannels: number;
  gateways: number;
  activeGateways: number;
  activeGatewayKeys: number;
  usageRequestsMonth: number;
  usageCostMonth: number;
  enabledAlertRules: number;
  enabledAdminAlertRules: number;
  enabledNotificationChannels: number;
  openApiSites: number;
  activeSessions: number;
  auditToday: number;
  platformOrg: AdminOrgItem;
};

export type NavItem = {
  label: string;
  href: string;
};

export type SeriesPoint = {
  date: string;
  healthIndex: number;
  successRate: number;
  latencyOpenMs: number;
  latencyCloseMs: number;
  latencyHighMs: number;
  latencyLowMs: number;
  probeCount: number;
  costUsd: number;
};

export type ProbeLayer = {
  name: string;
  status: string;
  latencyMs: number;
};

export type ProbeRecord = {
  time: string;
  layer: string;
  type: string;
  httpCode: number;
  latencyMs: number;
  result: string;
  errorType?: string;
};

export type ErrorBucket = {
  type: string;
  label: string;
  count: number;
};

export type CostPoint = {
  date: string;
  costUsd: number;
  tokens: number;
};

export type L3Stats = {
  successRate: number;
  contentValidRate: number;
  firstTokenMs: number;
  tokensPerSecond: number;
  averageTokens: number;
  quotaClass: string;
};

export type PublicChannelDetail = {
  channel: PublicChannel;
  layers: ProbeLayer[];
  l3: L3Stats;
  recentRecords: ProbeRecord[];
  errors: ErrorBucket[];
  costs: CostPoint[];
};

export type PrivateChannel = PublicChannel & {
  probeDaily: number;
  probesUsedToday: number;
  keyMask: string;
  keyFingerprint: string;
  quotaExhausted: boolean;
};

export type PrivateChannelSummary = {
  id: string;
  name: string;
  ownerEmail: string;
  provider: string;
  model: string;
  endpoint: string;
  status: string;
  probeDaily: number;
  probesUsedToday: number;
  lastProbeAt: string;
};

export type AdminPlatformChannel = PublicChannel & {
  probeDaily: number;
  probesUsedToday: number;
  publicVisible: boolean;
  gatewayEnabled: boolean;
  recommended: boolean;
  disabledAt?: string;
  deletedAt?: string;
  dataOrigin: string;
  keyMask: string;
  keyFingerprint: string;
  credentialUpdatedAt?: string;
  inputPerMtok: number;
  outputPerMtok: number;
  providerConfig: Record<string, unknown>;
};

export type PrivateChannelInput = {
  name: string;
  provider: string;
  type: string;
  model: string;
  endpoint: string;
  apiKey?: string;
  probeDaily: number;
};

export type AdminPlatformChannelInput = {
  name: string;
  provider: string;
  type: string;
  model: string;
  upstreamModel: string;
  endpoint: string;
  officialSiteUrl: string;
  introTitle: string;
  introSummary: string;
  introBody: string;
  introHighlights: string[];
  logoUrl: string;
  introSourceUrl: string;
  apiKey?: string;
  probeDaily: number;
  publicVisible: boolean;
  gatewayEnabled: boolean;
  enabled: boolean;
  inputPerMtok: number;
  outputPerMtok: number;
  providerConfig?: Record<string, unknown>;
  monitorModels?: MonitorModelConfig[];
};

export type AdminChannelImportResult = {
  rowNumber: number;
  id: string;
  name: string;
  action: "created" | "updated";
  keyMask: string;
  keyFingerprint: string;
};

export type AdminChannelImportResponse = {
  created: number;
  updated: number;
  rows: AdminChannelImportResult[];
  items: AdminPlatformChannel[];
  fileName: string;
};

export type AdminChannelSyncLog = {
  id: string;
  channelId: string;
  channelName: string;
  sampledAt: string;
  status: string;
  score: number;
  latencyP95Ms: number;
  l1Status: string;
  l2Status: string;
  l3Status: string;
  errorType: string;
};

export type AdminChannelSyncResponse = {
  created: number;
  updated: number;
  rows: AdminChannelImportResult[];
  items: AdminPlatformChannel[];
  source: string;
  syncedAt: string;
  logs: AdminChannelSyncLog[];
  endpoint: string;
  credentials: string;
};

export type ChannelIntroDraft = {
  introTitle: string;
  introSummary: string;
  introBody: string;
  introHighlights: string[];
  logoUrl: string;
  introSourceUrl: string;
};

export type ProviderRank = {
  provider: string;
  channels: number;
  healthy: number;
  successRate: number;
  latencyP95Ms: number;
  score: number;
};

export type GatewayUpstream = {
  id: string;
  gatewayId: string;
  channelId: string;
  name: string;
  provider: string;
  ownerType: string;
  type: string;
  model: string;
  endpoint: string;
  status: string;
  score: number;
  successRate: number;
  latencyP95Ms: number;
  costUsd: number;
  weight: number;
  priority: number;
  enabled: boolean;
  mark: string;
  providerConfig: Record<string, unknown>;
};

export type GatewayStats = {
  requestsToday: number;
  tokensToday: number;
  costToday: number;
  errorRate: number;
  successRate: number;
  p95LatencyMs: number;
  keysActive: number;
};

export type Gateway = {
  id: string;
  orgId: string;
  name: string;
  slug: string;
  baseUrl: string;
  policy: string;
  policyLabel: string;
  status: string;
  qpsLimit: number;
  quotaMonth: number;
  createdAt: string;
  upstreams: GatewayUpstream[];
  stats: GatewayStats;
};

export type GatewayKey = {
  id: string;
  orgId: string;
  gatewayId: string;
  gatewayName: string;
  name: string;
  keyPrefix: string;
  keyMask: string;
  plainKey?: string;
  quotaMonth: number;
  qpsLimit: number;
  requestsUsed: number;
  status: string;
  createdAt: string;
  lastUsedAt?: string;
  expiresAt?: string;
};

export type GatewayMember = {
  userId: string;
  email: string;
  name: string;
  avatar: string;
  role: string;
  groupName: string;
  status: string;
  requestsToday: number;
  lastActiveAt?: string;
};

export type WorkspaceSettings = {
  orgId: string;
  name: string;
  plan: string;
  status: string;
  role: string;
  timezone: string;
  defaultGatewayPolicy: "latency" | "success" | "cost";
  defaultGatewayPolicyLabel: string;
  defaultNotificationChannelId: string;
  defaultNotificationName: string;
  members: number;
  privateChannels: number;
  gateways: number;
  activeKeys: number;
  notificationChannels: NotificationChannel[];
  updatedAt: string;
};

export type WorkspaceOption = {
  orgId: string;
  name: string;
  plan: string;
  status: string;
  role: string;
  members: number;
  privateChannels: number;
  gateways: number;
  activeKeys: number;
};

export function workspaceCanOperate(role?: string) {
  const normalized = (role ?? "").toLowerCase();
  return normalized === "owner" || normalized === "admin" || normalized === "operator";
}

export function workspaceCanManage(role?: string) {
  const normalized = (role ?? "").toLowerCase();
  return normalized === "owner" || normalized === "admin";
}

export type ConnectionValidationResult = {
  ok: boolean;
  provider: string;
  type: string;
  endpoint: string;
  model: string;
  stage: string;
  statusCode: number;
  latencyMs: number;
  modelCount: number;
  tokens: number;
  usageEstimated: boolean;
  errorType?: string;
  message: string;
};

export type GatewayDebugResult = {
  ok: boolean;
  gatewayId: string;
  gateway: string;
  upstreamId: string;
  upstream: string;
  model: string;
  statusCode: number;
  latencyMs: number;
  tokens: number;
  usageEstimated: boolean;
  errorType?: string;
  message: string;
  preview?: string;
};

export type GatewayUsageSummary = {
  totals: { requests: number; tokens: number; costUsd: number; errorRate: number };
  quotaMonth: number;
  trend: Array<{ date: string; requests: number; tokens: number; costUsd: number }>;
  gateways: Array<{ id: string; name: string; requests: number; tokens: number; costUsd: number; errorRate: number }>;
  models: Array<{ id: string; name: string; requests: number; tokens: number; costUsd: number; errorRate: number }>;
  channels: Array<{ id: string; name: string; requests: number; tokens: number; costUsd: number; errorRate: number }>;
  rollups: UsageRollupItem[];
  recent: Array<{ id: string; gateway: string; keyName: string; channel: string; model: string; statusCode: number; tokens: number; costUsd: number; latencyMs: number; stream: boolean; createdAt: string; errorType?: string; estimated: boolean }>;
};

export type UsageRollupItem = {
  day: string;
  orgId: string;
  source: "gateway" | "probe";
  gatewayId: string;
  channelId: string;
  model: string;
  memberUserId: string;
  requests: number;
  tokens: number;
  costUsd: number;
  errors: number;
  probeRuns: number;
};

export type UsageFilter = {
  days?: string;
  source?: "" | "gateway" | "probe";
  gatewayId?: string;
  channelId?: string;
  model?: string;
  memberUserId?: string;
  recompute?: "0" | "1";
};

export type AlertRule = {
  id: string;
  orgId: string;
  scope: "admin" | "console";
  name: string;
  kind: "l3_consecutive_failures" | "cost_threshold" | "gateway_error_rate" | "quota_anomaly";
  severity: "info" | "warning" | "critical";
  threshold: number;
  windowMinutes: number;
  dedupeMinutes: number;
  enabled: boolean;
  config: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
};

export type NotificationChannel = {
  id: string;
  orgId: string;
  scope: "admin" | "console";
  name: string;
  type: "email" | "webhook" | "feishu";
  target?: string;
  targetMask: string;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
};

export type AlertDelivery = {
  id: string;
  orgId: string;
  scope: "admin" | "console";
  ruleId: string;
  ruleName: string;
  notificationChannelId: string;
  channelName: string;
  incidentId: string;
  dedupeKey: string;
  severity: string;
  status: "sent" | "failed" | "suppressed" | "recovered" | "test";
  title: string;
  message: string;
  error: string;
  metadata: Record<string, unknown>;
  createdAt: string;
  sentAt?: string;
};

export type IncidentItem = {
  id: string;
  orgId: string;
  channelId: string;
  channel: string;
  status: string;
  title: string;
  open: boolean;
  openedAt: string;
  resolvedAt?: string;
  metadata: Record<string, unknown>;
  events?: Array<{ id: string; type: string; message: string; metadata: Record<string, unknown>; createdAt: string }>;
};

export type AlertCenter = {
  summary: { enabledRules: number; openIncidents: number; sentToday: number; suppressedToday: number; failedToday: number; recoveredToday: number };
  rules: AlertRule[];
  channels: NotificationChannel[];
  deliveries: AlertDelivery[];
  incidents: IncidentItem[];
};

export type AuditLogItem = {
  id: string;
  actorType: string;
  actorId: string;
  actorEmail: string;
  action: string;
  objectType: string;
  objectId: string;
  ip: string;
  result: string;
  metadata: Record<string, unknown>;
  createdAt: string;
};

export type AuditLogResult = {
  items: AuditLogItem[];
  total: number;
};

export type ProbeLogItem = {
  id: string;
  channelId: string;
  channelName: string;
  provider: string;
  type: string;
  model: string;
  endpoint: string;
  layer: string;
  source: string;
  status: string;
  runStatus: string;
  step: string;
  httpStatus?: number;
  errorType: string;
  latencyMs: number;
  stepCount: number;
  startedAt: string;
  finishedAt?: string;
};

export type ProbeLogStep = {
  step: string;
  status: string;
  latencyMs: number;
  httpStatus?: number;
  errorType: string;
  metadata: Record<string, unknown>;
  createdAt: string;
};

export type ProbeLogDetail = ProbeLogItem & {
  steps: ProbeLogStep[];
};

export type ProbeLogResult = {
  items: ProbeLogItem[];
  total: number;
  page: number;
  pageSize: number;
  summary: {
    total: number;
    abnormal: number;
    authErrors: number;
    slowResponses: number;
    latestAt?: string;
  };
};

export type GovernanceSummary = {
  openIncidents: number;
  alertsToday: number;
  auditToday: number;
  costTodayUsd: number;
  recentAlerts: AlertDelivery[];
  recentAudit: AuditLogItem[];
  incidents: IncidentItem[];
};

export type RecommendPick = {
  id: string;
  channelId: string;
  position: number;
  title: string;
  ribbon: string;
  summary: string;
  points: string[];
  ctaLabel: string;
  ctaUrl: string;
  enabled: boolean;
  clicks: number;
  ctr: number;
  channel: PublicChannel;
};

export type RecommendReward = {
  id: string;
  channelId: string;
  providerName: string;
  rewardType: string;
  rewardValue: string;
  code: string;
  expiresAtText: string;
  enabled: boolean;
  clicks: number;
};

export type RecommendScenario = {
  id: string;
  title: string;
  icon: string;
  channelId: string;
  summary: string;
  position: number;
  enabled: boolean;
  clicks: number;
  channel: PublicChannel;
};

export type RecommendRankRule = {
  id: string;
  label: string;
  description: string;
  metric: "overall" | "speed" | "price" | "stable" | "custom";
  position: number;
  enabled: boolean;
};

export type RecommendConfig = {
  stats: { picks: number; ranked: number; rewards: number; scenarios: number; clicks: number; ctr: number; averageScore: number };
  picks: RecommendPick[];
  ranks: RecommendPick[];
  rankRules: RecommendRankRule[];
  rewards: RecommendReward[];
  scenarios: RecommendScenario[];
  updatedAt: string;
};

export type RecommendAdminData = RecommendConfig & {
  channels: PublicChannel[];
};

export type OpenAPIEndpoint = {
  scope: string;
  method: string;
  path: string;
  description: string;
  callsToday: number;
  averageMs: number;
  cache: string;
  status: string;
};

export type OpenAPISite = {
  id: string;
  name: string;
  siteKeyPrefix: string;
  siteKeyMask: string;
  plainKey?: string;
  scopes: string[];
  qpsLimit: number;
  status: string;
  callsToday: number;
  createdAt: string;
  lastUsedAt?: string;
};

export type AdminAgentScope = "admin:read" | "admin:write" | "admin:dangerous" | "admin:secrets" | "admin:export";

export type AdminAgentToken = {
  id: string;
  name: string;
  tokenPrefix: string;
  tokenMask: string;
  plainToken?: string;
  scopes: AdminAgentScope[];
  createdBy: string;
  createdByEmail: string;
  expiresAt?: string;
  revokedAt?: string;
  lastUsedAt?: string;
  createdAt: string;
};

export type AdminUserItem = {
  id: string;
  email: string;
  username: string;
  name: string;
  avatar: string;
  role: string;
  plan: string;
  status: string;
  emailVerified: boolean;
  dataOrigin: string;
  orgs: number;
  gateways: number;
  privateChannels: number;
  auditEvents: number;
  lastActiveAt?: string;
  createdAt: string;
  updatedAt: string;
  deletedAt?: string;
};

export type AdminUsersResult = {
  items: AdminUserItem[];
  stats: Record<string, number>;
};

export type AdminUserInput = {
  email?: string;
  username?: string;
  password?: string;
  name?: string;
  role?: string;
  plan?: string;
  status?: string;
  emailVerified?: boolean;
  dataOrigin?: string;
};

export type AdminUserFilter = {
  q?: string;
  status?: string;
  role?: string;
  plan?: string;
  origin?: string;
};

export type AdminOrgItem = {
  id: string;
  name: string;
  slug: string;
  plan: string;
  status: string;
  timezone: string;
  dataOrigin: string;
  members: number;
  gateways: number;
  privateChannels: number;
  activeKeys: number;
  auditEvents: number;
  createdAt: string;
  updatedAt: string;
  suspendedAt?: string;
  deletedAt?: string;
};

export type AdminOrgsResult = {
  items: AdminOrgItem[];
  stats: Record<string, number>;
};

export type AdminOrgInput = {
  name?: string;
  slug?: string;
  plan?: string;
  status?: string;
  timezone?: string;
  dataOrigin?: string;
};

export type AdminOrgFilter = {
  q?: string;
  status?: string;
  plan?: string;
  origin?: string;
};

export type ProductionHealthCheck = {
  id: string;
  label: string;
  status: "pass" | "warn" | "fail";
  severity: string;
  value: string;
  message: string;
  action: string;
};

export type ProductionHealth = {
  generatedAt: string;
  stats: Record<string, number>;
  summary: Record<string, number>;
  checks: ProductionHealthCheck[];
};

export type OpenAPICallLog = {
  id: string;
  siteName: string;
  endpoint: string;
  statusCode: number;
  latencyMs: number;
  ip: string;
  createdAt: string;
};

export type OpenAPISummary = {
  baseUrl: string;
  endpoints: OpenAPIEndpoint[];
  sites: OpenAPISite[];
  logs: OpenAPICallLog[];
  stats: { sites: number; active: number; endpoints: number; callsToday: number };
};

export type ChannelSiteNavItem = {
  id?: string;
  label: string;
  href: string;
  position: number;
};

export type ChannelSiteModules = {
  overview: boolean;
  channelBoard: boolean;
  recommend: boolean;
  providerRank: boolean;
  strategy: boolean;
};

export type ChannelSiteCopy = {
  homeIntro: string;
  recommendIntro: string;
  footerText: string;
};

export type ChannelSiteSEO = {
  title: string;
  description: string;
  canonicalUrl: string;
};

export type ChannelSitePackageExport = {
  id: string;
  siteId: string;
  version: string;
  fileName: string;
  fileSize: number;
  createdAt: string;
};

export type ChannelSiteRuntimeLog = {
  id: string;
  siteId: string;
  siteName: string;
  endpoint: string;
  statusCode: number;
  latencyMs: number;
  origin: string;
  ip: string;
  createdAt: string;
};

export type ChannelSite = {
  id: string;
  name: string;
  slug: string;
  domain: string;
  publicUrl: string;
  runtimeKeyPrefix: string;
  runtimeKeyMask: string;
  plainRuntimeKey?: string;
  title: string;
  description: string;
  logoMark: string;
  overviewLabel: string;
  recommendLabel: string;
  modules: ChannelSiteModules;
  copy: ChannelSiteCopy;
  seo: ChannelSiteSEO;
  navItems: ChannelSiteNavItem[];
  qpsLimit: number;
  status: string;
  callsToday: number;
  packageExports?: ChannelSitePackageExport[];
  createdAt: string;
  updatedAt: string;
  lastUsedAt?: string;
};

export type ChannelSiteInput = {
  name: string;
  domain: string;
  publicUrl: string;
  title: string;
  description: string;
  logoMark: string;
  overviewLabel: string;
  recommendLabel: string;
  modules: ChannelSiteModules;
  copy: ChannelSiteCopy;
  seo: ChannelSiteSEO;
  navItems: ChannelSiteNavItem[];
  qpsLimit: number;
  status: string;
};

export type ChannelSitesSummary = {
  baseUrl: string;
  sites: ChannelSite[];
  logs: ChannelSiteRuntimeLog[];
  stats: { sites: number; active: number; callsToday: number; exports: number };
};

export async function publicOverview(): Promise<PublicOverview> {
  return readJSON<PublicOverview>("/api/public/overview");
}

export async function publicChannels(params: Record<string, string | number | undefined> = {}): Promise<PublicChannelList> {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") {
      search.set(key, String(value));
    }
  }
  const suffix = search.toString() ? `?${search}` : "";
  return readJSON<PublicChannelList>(`/api/public/channels${suffix}`);
}

export async function publicChannel(channelID: string): Promise<PublicChannelDetail> {
  return readJSON<PublicChannelDetail>(`/api/public/channels/${encodeURIComponent(channelID)}`);
}

export async function publicChannelSeries(channelID: string, days: number): Promise<{ items: SeriesPoint[] }> {
  return readJSON<{ items: SeriesPoint[] }>(`/api/public/channels/${encodeURIComponent(channelID)}/series?days=${days}`);
}

export async function providerRank(range?: string): Promise<{ items: ProviderRank[] }> {
  const suffix = range ? `?range=${encodeURIComponent(range)}` : "";
  return readJSON<{ items: ProviderRank[] }>(`/api/public/providers/rank${suffix}`);
}

export async function errorsSummary(range?: string): Promise<{ items: ErrorBucket[] }> {
  const suffix = range ? `?range=${encodeURIComponent(range)}` : "";
  return readJSON<{ items: ErrorBucket[] }>(`/api/public/errors/summary${suffix}`);
}

export async function siteConfig(options: { force?: boolean } = {}): Promise<SiteConfig> {
  if (!options.force && siteConfigCache && Date.now() < siteConfigCacheExpiresAt) return siteConfigCache;
  if (!options.force && siteConfigPromise) return siteConfigPromise;

  siteConfigPromise = readJSON<SiteConfig>("/api/public/site-config", { cache: "no-store" })
    .then(rememberSiteConfig)
    .finally(() => {
      siteConfigPromise = null;
    });
  return siteConfigPromise;
}

export async function favoriteChannels(): Promise<{ items: PublicChannel[]; ids: string[] }> {
  return readJSON<{ items: PublicChannel[]; ids: string[] }>("/api/me/favorites", {
    credentials: "include"
  });
}

export async function addFavorite(channelID: string): Promise<void> {
  await writeJSONRequest(`/api/me/favorites/${channelID}`, {}, { method: "PUT" });
}

export async function removeFavorite(channelID: string): Promise<void> {
  await writeJSONRequest(`/api/me/favorites/${channelID}`, {}, { method: "DELETE" });
}

export async function bulkRemoveFavorites(ids: string[]): Promise<{ items: PublicChannel[]; ids: string[] }> {
  return writeJSONRequest<{ items: PublicChannel[]; ids: string[] }>("/api/me/favorites/bulk", { action: "delete", ids });
}

export async function privateChannels(): Promise<{ items: PrivateChannel[] }> {
  return readJSON<{ items: PrivateChannel[] }>("/api/me/private-channels", { credentials: "include" });
}

export async function createPrivateChannel(input: PrivateChannelInput): Promise<{ channel: PrivateChannel }> {
  return writeJSONRequest<{ channel: PrivateChannel }>("/api/me/private-channels", input);
}

export async function updatePrivateChannel(channelID: string, input: PrivateChannelInput): Promise<{ channel: PrivateChannel }> {
  return writeJSONRequest<{ channel: PrivateChannel }>(`/api/me/private-channels/${channelID}`, input, { method: "PATCH" });
}

export async function deletePrivateChannel(channelID: string): Promise<void> {
  await writeJSONRequest(`/api/me/private-channels/${channelID}`, {}, { method: "DELETE" });
}

export async function bulkPrivateChannels(input: { action: "status" | "disable" | "enable" | "delete"; ids: string[]; status?: string }): Promise<{ items: PrivateChannel[] }> {
  return writeJSONRequest<{ items: PrivateChannel[] }>("/api/me/private-channels/bulk", input);
}

export async function probePrivateChannel(channelID: string): Promise<{ channel: PrivateChannel }> {
  return writeJSONRequest<{ channel: PrivateChannel }>(`/api/me/private-channels/${channelID}/probe-now`);
}

export async function validatePrivateChannelDraft(input: PrivateChannelInput): Promise<{ result: ConnectionValidationResult }> {
  return writeJSONRequest<{ result: ConnectionValidationResult }>("/api/me/private-channels/validate", input);
}

export async function validatePrivateChannel(channelID: string): Promise<{ result: ConnectionValidationResult }> {
  return writeJSONRequest<{ result: ConnectionValidationResult }>(`/api/me/private-channels/${channelID}/validate`);
}

export async function adminChannels(): Promise<{ items: AdminPlatformChannel[]; private: PrivateChannelSummary[]; total: number; summary: { platform: number; private: number } }> {
  return readJSON<{ items: AdminPlatformChannel[]; private: PrivateChannelSummary[]; total: number; summary: { platform: number; private: number } }>("/api/admin/channels", {
    credentials: "include"
  });
}

export async function adminProbeNow(channelID: string): Promise<{ channel: AdminPlatformChannel }> {
  return writeJSONRequest<{ channel: AdminPlatformChannel }>(`/api/admin/channels/${channelID}/probe-now`);
}

export async function createAdminChannel(input: AdminPlatformChannelInput): Promise<{ channel: AdminPlatformChannel; channels?: AdminPlatformChannel[] }> {
  return writeJSONRequest<{ channel: AdminPlatformChannel; channels?: AdminPlatformChannel[] }>("/api/admin/channels", input);
}

export async function updateAdminChannel(channelID: string, input: AdminPlatformChannelInput): Promise<{ channel: AdminPlatformChannel }> {
  return writeJSONRequest<{ channel: AdminPlatformChannel }>(`/api/admin/channels/${channelID}`, input, { method: "PATCH" });
}

export async function fetchAdminChannelIntro(channelID: string, url: string): Promise<{ draft: ChannelIntroDraft }> {
  return writeJSONRequest<{ draft: ChannelIntroDraft }>(`/api/admin/channels/${channelID}/intro-fetch`, { url });
}

export async function rotateAdminChannelCredential(channelID: string, apiKey: string): Promise<{ channel: AdminPlatformChannel }> {
  return writeJSONRequest<{ channel: AdminPlatformChannel }>(`/api/admin/channels/${channelID}/credentials`, { apiKey });
}

export async function validateAdminChannel(channelID: string): Promise<{ channel: AdminPlatformChannel }> {
  return writeJSONRequest<{ channel: AdminPlatformChannel }>(`/api/admin/channels/${channelID}/validate`);
}

export async function disableAdminChannel(channelID: string): Promise<{ channel: AdminPlatformChannel }> {
  return writeJSONRequest<{ channel: AdminPlatformChannel }>(`/api/admin/channels/${channelID}/disable`);
}

export async function addAdminChannelRecommendation(channelID: string): Promise<{ channel: AdminPlatformChannel }> {
  return writeJSONRequest<{ channel: AdminPlatformChannel }>(`/api/admin/channels/${channelID}/recommend`);
}

export async function removeAdminChannelRecommendation(channelID: string): Promise<{ channel: AdminPlatformChannel }> {
  return writeJSONRequest<{ channel: AdminPlatformChannel }>(`/api/admin/channels/${channelID}/recommend`, {}, { method: "DELETE" });
}

export async function deleteAdminChannel(channelID: string): Promise<void> {
  await writeJSONRequest(`/api/admin/channels/${channelID}`, {}, { method: "DELETE" });
}

export async function bulkAdminChannels(input: { action: "status" | "disable" | "enable" | "delete"; ids: string[]; status?: string }): Promise<{ items: AdminPlatformChannel[]; total: number }> {
  return writeJSONRequest<{ items: AdminPlatformChannel[]; total: number }>("/api/admin/channels/bulk", input);
}

export async function exportAdminChannels(password: string): Promise<{ blob: Blob; filename: string }> {
  return writeBlobRequest("/api/admin/channels/export", { password });
}

export async function importAdminChannels(password: string, file: File): Promise<AdminChannelImportResponse> {
  const form = new FormData();
  form.set("password", password);
  form.set("file", file);
  return writeFormRequest<AdminChannelImportResponse>("/api/admin/channels/import", form);
}

export async function syncAdminChannelsFromAPI(input: { baseUrl: string; siteKey: string; password: string }): Promise<AdminChannelSyncResponse> {
  return writeJSONRequest<AdminChannelSyncResponse>("/api/admin/channels/sync", input);
}

export async function adminSettings(): Promise<{ site: SiteConfig; summary: AdminSettingsSummary }> {
  return readJSON<{ site: SiteConfig; summary: AdminSettingsSummary }>("/api/admin/settings", { credentials: "include" })
    .then(rememberSitePayload);
}

export async function updateAdminSettings(input: Partial<SiteConfig>): Promise<{ site: SiteConfig }> {
  return writeJSONRequest<{ site: SiteConfig }>("/api/admin/settings", input, { method: "PATCH" })
    .then(rememberSitePayload);
}

export async function adminAgentTokens(): Promise<{ items: AdminAgentToken[] }> {
  return readJSON<{ items: AdminAgentToken[] }>("/api/admin/agent-tokens", { credentials: "include" });
}

export async function createAdminAgentToken(input: { name: string; scopes: AdminAgentScope[]; ttlHours?: number; expiresAt?: string }): Promise<{ token: AdminAgentToken }> {
  return writeJSONRequest<{ token: AdminAgentToken }>("/api/admin/agent-tokens", input);
}

export async function revokeAdminAgentToken(tokenID: string): Promise<{ token: AdminAgentToken }> {
  return writeJSONRequest<{ token: AdminAgentToken }>(`/api/admin/agent-tokens/${tokenID}/revoke`, {});
}

function queryString(params: Record<string, string | undefined>): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value && value !== "all") query.set(key, value);
  }
  const raw = query.toString();
  return raw ? `?${raw}` : "";
}

export async function adminUsers(filter: AdminUserFilter = {}): Promise<AdminUsersResult> {
  return readJSON<AdminUsersResult>(`/api/admin/users${queryString(filter)}`, { credentials: "include" });
}

export async function createAdminUser(input: AdminUserInput): Promise<{ user: AdminUserItem }> {
  return writeJSONRequest<{ user: AdminUserItem }>("/api/admin/users", input);
}

export async function updateAdminUser(userID: string, input: AdminUserInput): Promise<{ user: AdminUserItem }> {
  return writeJSONRequest<{ user: AdminUserItem }>(`/api/admin/users/${userID}`, input, { method: "PATCH" });
}

export async function updateAdminUserStatus(userID: string, status: string): Promise<{ user: AdminUserItem }> {
  return writeJSONRequest<{ user: AdminUserItem }>(`/api/admin/users/${userID}/status`, { status }, { method: "PATCH" });
}

export async function deleteAdminUser(userID: string): Promise<void> {
  await writeJSONRequest(`/api/admin/users/${userID}`, {}, { method: "DELETE" });
}

export async function bulkAdminUsers(input: { action: "status" | "role" | "delete"; ids: string[]; status?: string; role?: string }): Promise<AdminUsersResult> {
  return writeJSONRequest<AdminUsersResult>("/api/admin/users/bulk", input);
}

export async function adminOrgs(filter: AdminOrgFilter = {}): Promise<AdminOrgsResult> {
  return readJSON<AdminOrgsResult>(`/api/admin/orgs${queryString(filter)}`, { credentials: "include" });
}

export async function createAdminOrg(input: AdminOrgInput): Promise<{ org: AdminOrgItem }> {
  return writeJSONRequest<{ org: AdminOrgItem }>("/api/admin/orgs", input);
}

export async function updateAdminOrg(orgID: string, input: AdminOrgInput): Promise<{ org: AdminOrgItem }> {
  return writeJSONRequest<{ org: AdminOrgItem }>(`/api/admin/orgs/${orgID}`, input, { method: "PATCH" });
}

export async function updateAdminOrgStatus(orgID: string, status: string): Promise<{ org: AdminOrgItem }> {
  return writeJSONRequest<{ org: AdminOrgItem }>(`/api/admin/orgs/${orgID}/status`, { status }, { method: "PATCH" });
}

export async function deleteAdminOrg(orgID: string): Promise<void> {
  await writeJSONRequest(`/api/admin/orgs/${orgID}`, {}, { method: "DELETE" });
}

export async function bulkAdminOrgs(input: { action: "status" | "plan" | "delete"; ids: string[]; status?: string; plan?: string }): Promise<AdminOrgsResult> {
  return writeJSONRequest<AdminOrgsResult>("/api/admin/orgs/bulk", input);
}

export async function productionHealth(): Promise<ProductionHealth> {
  return readJSON<ProductionHealth>("/api/admin/production-health", { credentials: "include" });
}

export async function adminGateways(): Promise<{ items: Gateway[]; upstreams: GatewayUpstream[]; allowPlatformUpstreams?: boolean }> {
  return readJSON<{ items: Gateway[]; upstreams: GatewayUpstream[]; allowPlatformUpstreams?: boolean }>("/api/admin/gateways", { credentials: "include" });
}

export async function consoleGateways(): Promise<{ items: Gateway[]; upstreams: GatewayUpstream[]; allowPlatformUpstreams?: boolean }> {
  return readJSON<{ items: Gateway[]; upstreams: GatewayUpstream[]; allowPlatformUpstreams?: boolean }>("/api/console/gateways", { credentials: "include" });
}

type GatewayInput = { name: string; policy: string; upstreamIds: string[]; qpsLimit?: number; quotaMonth?: number; status?: string };

type GatewayKeyInput = { gatewayId?: string; name?: string; quotaMonth?: number; qpsLimit?: number; status?: string };

export async function createGateway(input: { name: string; policy: string; upstreamIds: string[]; qpsLimit?: number; quotaMonth?: number }): Promise<{ gateway: Gateway }> {
  return writeJSONRequest<{ gateway: Gateway }>("/api/console/gateways", input);
}

export async function createAdminGateway(input: GatewayInput): Promise<{ gateway: Gateway }> {
  return writeJSONRequest<{ gateway: Gateway }>("/api/admin/gateways", input);
}

export async function updateAdminGateway(gatewayID: string, input: Partial<GatewayInput>): Promise<{ gateway: Gateway }> {
  return writeJSONRequest<{ gateway: Gateway }>(`/api/admin/gateways/${gatewayID}`, input, { method: "PATCH" });
}

export async function updateConsoleGateway(gatewayID: string, input: Partial<GatewayInput>): Promise<{ gateway: Gateway }> {
  return writeJSONRequest<{ gateway: Gateway }>(`/api/console/gateways/${gatewayID}`, input, { method: "PATCH" });
}

export async function deleteAdminGateway(gatewayID: string): Promise<void> {
  await writeJSONRequest(`/api/admin/gateways/${gatewayID}`, {}, { method: "DELETE" });
}

export async function deleteConsoleGateway(gatewayID: string): Promise<void> {
  await writeJSONRequest(`/api/console/gateways/${gatewayID}`, {}, { method: "DELETE" });
}

export async function bulkAdminGateways(input: { action: "status" | "delete"; ids: string[]; status?: string }): Promise<{ items: Gateway[]; upstreams: GatewayUpstream[] }> {
  return writeJSONRequest<{ items: Gateway[]; upstreams: GatewayUpstream[] }>("/api/admin/gateways/bulk", input);
}

export async function bulkConsoleGateways(input: { action: "status" | "delete"; ids: string[]; status?: string }): Promise<{ items: Gateway[]; upstreams: GatewayUpstream[] }> {
  return writeJSONRequest<{ items: Gateway[]; upstreams: GatewayUpstream[] }>("/api/console/gateways/bulk", input);
}

export async function debugGateway(gatewayID: string, input: { model?: string; prompt?: string; kind?: "chat" | "responses" }): Promise<{ result: GatewayDebugResult }> {
  return writeJSONRequest<{ result: GatewayDebugResult }>(`/api/console/gateways/${gatewayID}/debug`, input);
}

export async function adminGatewayKeys(): Promise<{ items: GatewayKey[] }> {
  return readJSON<{ items: GatewayKey[] }>("/api/admin/gateway-keys", { credentials: "include" });
}

export async function consoleGatewayKeys(): Promise<{ items: GatewayKey[] }> {
  return readJSON<{ items: GatewayKey[] }>("/api/console/gateway-keys", { credentials: "include" });
}

export async function createGatewayKey(input: { gatewayId: string; name: string; quotaMonth?: number; qpsLimit?: number }): Promise<{ key: GatewayKey }> {
  return writeJSONRequest<{ key: GatewayKey }>("/api/console/gateway-keys", input);
}

export async function createAdminGatewayKey(input: GatewayKeyInput): Promise<{ key: GatewayKey }> {
  return writeJSONRequest<{ key: GatewayKey }>("/api/admin/gateway-keys", input);
}

export async function updateAdminGatewayKey(keyID: string, input: GatewayKeyInput): Promise<{ key: GatewayKey }> {
  return writeJSONRequest<{ key: GatewayKey }>(`/api/admin/gateway-keys/${keyID}`, input, { method: "PATCH" });
}

export async function updateConsoleGatewayKey(keyID: string, input: GatewayKeyInput): Promise<{ key: GatewayKey }> {
  return writeJSONRequest<{ key: GatewayKey }>(`/api/console/gateway-keys/${keyID}`, input, { method: "PATCH" });
}


export async function revokeGatewayKey(keyID: string): Promise<void> {
  await writeJSONRequest(`/api/console/gateway-keys/${keyID}/revoke`, {});
}

export async function revokeAdminGatewayKey(keyID: string): Promise<void> {
  await writeJSONRequest(`/api/admin/gateway-keys/${keyID}/revoke`, {});
}

export async function deleteGatewayKey(keyID: string): Promise<void> {
  await writeJSONRequest(`/api/console/gateway-keys/${keyID}`, {}, { method: "DELETE" });
}

export async function deleteAdminGatewayKey(keyID: string): Promise<void> {
  await writeJSONRequest(`/api/admin/gateway-keys/${keyID}`, {}, { method: "DELETE" });
}

export async function bulkAdminGatewayKeys(input: { action: "status" | "delete" | "revoke"; ids: string[]; status?: string }): Promise<{ items: GatewayKey[] }> {
  return writeJSONRequest<{ items: GatewayKey[] }>("/api/admin/gateway-keys/bulk", input);
}

export async function bulkConsoleGatewayKeys(input: { action: "status" | "delete" | "revoke"; ids: string[]; status?: string }): Promise<{ items: GatewayKey[] }> {
  return writeJSONRequest<{ items: GatewayKey[] }>("/api/console/gateway-keys/bulk", input);
}

export async function adminMembers(): Promise<{ members: GatewayMember[]; keys: GatewayKey[] }> {
  return readJSON<{ members: GatewayMember[]; keys: GatewayKey[] }>("/api/admin/members", { credentials: "include" });
}

export async function consoleMembers(): Promise<{ members: GatewayMember[]; keys: GatewayKey[] }> {
  return readJSON<{ members: GatewayMember[]; keys: GatewayKey[] }>("/api/console/members", { credentials: "include" });
}

export async function inviteAdminMember(input: { email: string; role: string; groupName?: string }): Promise<{ member: GatewayMember }> {
  return writeJSONRequest<{ member: GatewayMember }>("/api/admin/members", input);
}

export async function updateAdminMember(userID: string, input: { role: string; groupName?: string }): Promise<{ member: GatewayMember }> {
  return writeJSONRequest<{ member: GatewayMember }>(`/api/admin/members/${userID}`, input, { method: "PATCH" });
}

export async function removeAdminMember(userID: string): Promise<void> {
  await writeJSONRequest(`/api/admin/members/${userID}`, {}, { method: "DELETE" });
}

export async function bulkAdminMembers(input: { action: "role" | "delete" | "remove"; ids: string[]; role?: string }): Promise<{ members: GatewayMember[]; keys: GatewayKey[] }> {
  return writeJSONRequest<{ members: GatewayMember[]; keys: GatewayKey[] }>("/api/admin/members/bulk", input);
}

export async function bulkConsoleMembers(input: { action: "role" | "delete" | "remove"; ids: string[]; role?: string }): Promise<{ members: GatewayMember[]; keys: GatewayKey[] }> {
  return writeJSONRequest<{ members: GatewayMember[]; keys: GatewayKey[] }>("/api/console/members/bulk", input);
}

export async function consoleSettings(): Promise<{ workspace: WorkspaceSettings; workspaces: WorkspaceOption[] }> {
  return readJSON<{ workspace: WorkspaceSettings; workspaces: WorkspaceOption[] }>("/api/console/settings", { credentials: "include" });
}

export async function updateConsoleSettings(input: Partial<WorkspaceSettings>): Promise<{ workspace: WorkspaceSettings }> {
  return writeJSONRequest<{ workspace: WorkspaceSettings }>("/api/console/settings", input, { method: "PATCH" });
}

export async function resetConsoleSettings(): Promise<{ workspace: WorkspaceSettings }> {
  return writeJSONRequest<{ workspace: WorkspaceSettings }>("/api/console/settings/reset", {});
}

export async function inviteConsoleMember(input: { email: string; role: string; groupName?: string }): Promise<{ member: GatewayMember }> {
  return writeJSONRequest<{ member: GatewayMember }>("/api/console/members", input);
}

export async function updateConsoleMember(userID: string, input: { role: string; groupName?: string }): Promise<{ member: GatewayMember }> {
  return writeJSONRequest<{ member: GatewayMember }>(`/api/console/members/${userID}`, input, { method: "PATCH" });
}

export async function removeConsoleMember(userID: string): Promise<void> {
  await writeJSONRequest(`/api/console/members/${userID}`, {}, { method: "DELETE" });
}

export async function adminUsage(filter: UsageFilter = {}): Promise<GatewayUsageSummary> {
  return readJSON<GatewayUsageSummary>(`/api/admin/usage${queryString(filter)}`, { credentials: "include" });
}

export async function consoleUsage(filter: UsageFilter = {}): Promise<GatewayUsageSummary> {
  return readJSON<GatewayUsageSummary>(`/api/console/usage${queryString(filter)}`, { credentials: "include" });
}

export async function recomputeUsageRollup(scope: "admin" | "console"): Promise<void> {
  await writeJSONRequest(`/api/${scope}/usage/rollup/recompute`, {});
}

export function usageExportURL(scope: "admin" | "console", filter: UsageFilter = {}): string {
  return `/api/${scope}/usage/export${queryString(scope === "console" ? withWorkspaceFilter(filter) : filter)}`;
}

export async function alerts(scope: "admin" | "console"): Promise<AlertCenter> {
  return readJSON<AlertCenter>(`/api/${scope}/alerts`, { credentials: "include" });
}

export async function createAlertRule(scope: "admin" | "console", input: Partial<AlertRule>): Promise<{ rule: AlertRule }> {
  return writeJSONRequest<{ rule: AlertRule }>(`/api/${scope}/alerts/rules`, input);
}

export async function updateAlertRule(scope: "admin" | "console", ruleID: string, input: Partial<AlertRule>): Promise<{ rule: AlertRule }> {
  return writeJSONRequest<{ rule: AlertRule }>(`/api/${scope}/alerts/rules/${ruleID}`, input, { method: "PATCH" });
}

export async function deleteAlertRule(scope: "admin" | "console", ruleID: string): Promise<void> {
  await writeJSONRequest(`/api/${scope}/alerts/rules/${ruleID}`, {}, { method: "DELETE" });
}

export async function bulkAlertRules(scope: "admin" | "console", action: "enable" | "disable" | "delete", ids: string[]): Promise<{ items: AlertRule[] }> {
  return writeJSONRequest<{ items: AlertRule[] }>(`/api/${scope}/alerts/rules/bulk`, { action, ids });
}

export async function createNotificationChannel(scope: "admin" | "console", input: Partial<NotificationChannel>): Promise<{ channel: NotificationChannel }> {
  return writeJSONRequest<{ channel: NotificationChannel }>(`/api/${scope}/alerts/channels`, input);
}

export async function updateNotificationChannel(scope: "admin" | "console", channelID: string, input: Partial<NotificationChannel>): Promise<{ channel: NotificationChannel }> {
  return writeJSONRequest<{ channel: NotificationChannel }>(`/api/${scope}/alerts/channels/${channelID}`, input, { method: "PATCH" });
}

export async function deleteNotificationChannel(scope: "admin" | "console", channelID: string): Promise<void> {
  await writeJSONRequest(`/api/${scope}/alerts/channels/${channelID}`, {}, { method: "DELETE" });
}

export async function bulkNotificationChannels(scope: "admin" | "console", action: "enable" | "disable" | "delete", ids: string[]): Promise<{ items: NotificationChannel[] }> {
  return writeJSONRequest<{ items: NotificationChannel[] }>(`/api/${scope}/alerts/channels/bulk`, { action, ids });
}

export async function testNotificationChannel(scope: "admin" | "console", channelID: string): Promise<{ delivery: AlertDelivery }> {
  return writeJSONRequest<{ delivery: AlertDelivery }>(`/api/${scope}/alerts/channels/${channelID}/test`, {});
}

export async function evaluateAlerts(scope: "admin" | "console"): Promise<{ deliveries: AlertDelivery[] }> {
  return writeJSONRequest<{ deliveries: AlertDelivery[] }>(`/api/${scope}/alerts/evaluate`, {});
}

export async function auditLogs(scope: "admin" | "console", params: Record<string, string | number | undefined> = {}): Promise<AuditLogResult> {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") {
      search.set(key, String(value));
    }
  }
  const suffix = search.toString() ? `?${search}` : "";
  return readJSON<AuditLogResult>(`/api/${scope}/audit${suffix}`, { credentials: "include" });
}

export async function auditLogDetail(scope: "admin" | "console", auditID: string): Promise<AuditLogItem> {
  return readJSON<AuditLogItem>(`/api/${scope}/audit/${auditID}`, { credentials: "include" });
}

export async function probeLogs(params: Record<string, string | number | boolean | undefined> = {}): Promise<ProbeLogResult> {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") search.set(key, String(value));
  }
  return readJSON<ProbeLogResult>(`/api/admin/probe-logs${search.toString() ? `?${search}` : ""}`, { credentials: "include" });
}

export async function probeLogDetail(runID: string): Promise<ProbeLogDetail> {
  return readJSON<ProbeLogDetail>(`/api/admin/probe-logs/${encodeURIComponent(runID)}`, { credentials: "include" });
}

export function probeLogExportURL(params: Record<string, string | number | boolean | undefined> = {}) {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "" && value !== false) search.set(key, String(value));
  }
  return `/api/admin/probe-logs/export${search.toString() ? `?${search}` : ""}`;
}

export function auditExportURL(scope: "admin" | "console", params: Record<string, string | number | undefined> = {}) {
  const search = new URLSearchParams();
  const scoped = scope === "console" ? withWorkspaceFilter(params) : params;
  for (const [key, value] of Object.entries(scoped)) {
    if (value !== undefined && value !== "") {
      search.set(key, String(value));
    }
  }
  return `/api/${scope}/audit/export${search.toString() ? `?${search}` : ""}`;
}

function withWorkspaceFilter<T extends Record<string, string | number | undefined>>(params: T): T & { orgId?: string } {
  const orgId = activeWorkspaceId();
  return orgId ? { ...params, orgId } : params;
}

export async function governanceSummary(scope: "admin" | "console"): Promise<GovernanceSummary> {
  return readJSON<GovernanceSummary>(`/api/${scope}/governance/summary`, { credentials: "include" });
}

export async function incidents(scope: "admin" | "console", params: Record<string, string | number | undefined> = {}): Promise<{ items: IncidentItem[] }> {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") {
      search.set(key, String(value));
    }
  }
  return readJSON<{ items: IncidentItem[] }>(`/api/${scope}/incidents${search.toString() ? `?${search}` : ""}`, { credentials: "include" });
}

export async function createIncident(scope: "admin" | "console", input: { channelId: string; status: string; title: string; message?: string }): Promise<IncidentItem> {
  return writeJSONRequest<IncidentItem>(`/api/${scope}/incidents`, input);
}

export async function updateIncident(scope: "admin" | "console", incidentID: string, input: { status: string; title: string; message?: string }): Promise<IncidentItem> {
  return writeJSONRequest<IncidentItem>(`/api/${scope}/incidents/${incidentID}`, input, { method: "PATCH" });
}

export async function resolveIncident(scope: "admin" | "console", incidentID: string, message = ""): Promise<IncidentItem> {
  return writeJSONRequest<IncidentItem>(`/api/${scope}/incidents/${incidentID}/resolve`, { message });
}

export async function reopenIncident(scope: "admin" | "console", incidentID: string, message = ""): Promise<IncidentItem> {
  return writeJSONRequest<IncidentItem>(`/api/${scope}/incidents/${incidentID}/reopen`, { message });
}

export async function deleteIncident(scope: "admin" | "console", incidentID: string, message = ""): Promise<void> {
  await writeJSONRequest(`/api/${scope}/incidents/${incidentID}`, { message }, { method: "DELETE" });
}

export async function bulkIncidents(scope: "admin" | "console", action: "resolve" | "reopen" | "delete", ids: string[], message = ""): Promise<{ items: IncidentItem[] }> {
  return writeJSONRequest<{ items: IncidentItem[] }>(`/api/${scope}/incidents/bulk`, { action, ids, message });
}

export async function publicRecommend(): Promise<RecommendConfig> {
  return readJSON<RecommendConfig>("/api/public/recommend", { cache: "no-store" });
}

export async function trackRecommendClick(input: { itemType: string; itemId: string; channelId?: string }): Promise<void> {
  await publicWriteJSONRequest("/api/public/recommend/click", input);
}

export async function adminRecommend(): Promise<RecommendAdminData> {
  return readJSON<RecommendAdminData>("/api/admin/recommend", { credentials: "include" });
}

export async function saveAdminRecommend(input: { picks: RecommendPick[]; rankRules?: RecommendRankRule[]; rewards: RecommendReward[]; scenarios: RecommendScenario[] }): Promise<RecommendAdminData> {
  return writeJSONRequest<RecommendAdminData>("/api/admin/recommend", input, { method: "PUT" });
}

export async function adminOpenAPI(): Promise<OpenAPISummary> {
  return readJSON<OpenAPISummary>("/api/admin/open-api", { credentials: "include" });
}

export async function createOpenAPISite(input: { name: string; scopes: string[]; qpsLimit: number }): Promise<{ site: OpenAPISite }> {
  return writeJSONRequest<{ site: OpenAPISite }>("/api/admin/open-api/sites", input);
}

export async function updateOpenAPISite(siteID: string, input: { name: string; scopes: string[]; qpsLimit: number; status: string }): Promise<{ site: OpenAPISite }> {
  return writeJSONRequest<{ site: OpenAPISite }>(`/api/admin/open-api/sites/${siteID}`, input, { method: "PATCH" });
}

export async function revokeOpenAPISite(siteID: string): Promise<{ site: OpenAPISite }> {
  return writeJSONRequest<{ site: OpenAPISite }>(`/api/admin/open-api/sites/${siteID}/revoke`, {});
}

export async function deleteOpenAPISite(siteID: string): Promise<{ site: OpenAPISite }> {
  return writeJSONRequest<{ site: OpenAPISite }>(`/api/admin/open-api/sites/${siteID}`, {}, { method: "DELETE" });
}

export async function bulkOpenAPISites(input: { action: "status" | "delete" | "revoke"; ids: string[]; status?: string }): Promise<{ sites: OpenAPISite[] }> {
  return writeJSONRequest<{ sites: OpenAPISite[] }>("/api/admin/open-api/sites/bulk", input);
}

export async function adminChannelSites(): Promise<ChannelSitesSummary> {
  return readJSON<ChannelSitesSummary>("/api/admin/channel-sites", { credentials: "include" });
}

export async function createChannelSite(input: ChannelSiteInput): Promise<{ site: ChannelSite }> {
  return writeJSONRequest<{ site: ChannelSite }>("/api/admin/channel-sites", input);
}

export async function updateChannelSite(siteID: string, input: ChannelSiteInput): Promise<{ site: ChannelSite }> {
  return writeJSONRequest<{ site: ChannelSite }>(`/api/admin/channel-sites/${siteID}`, input, { method: "PATCH" });
}

export async function deleteChannelSite(siteID: string): Promise<{ site: ChannelSite }> {
  return writeJSONRequest<{ site: ChannelSite }>(`/api/admin/channel-sites/${siteID}`, {}, { method: "DELETE" });
}

export async function rotateChannelSiteKey(siteID: string): Promise<{ site: ChannelSite }> {
  return writeJSONRequest<{ site: ChannelSite }>(`/api/admin/channel-sites/${siteID}/rotate-key`, {});
}

export async function buildChannelSitePackage(siteID: string): Promise<{ site: ChannelSite; export: ChannelSitePackageExport }> {
  return writeJSONRequest<{ site: ChannelSite; export: ChannelSitePackageExport }>(`/api/admin/channel-sites/${siteID}/build`, {});
}

export async function downloadChannelSitePackage(siteID: string): Promise<{ blob: Blob; filename: string }> {
  const response = await fetch(`/api/admin/channel-sites/${siteID}/download`, { credentials: "include", method: "GET" });
  if (!response.ok) {
    let message = "下载渠道站点包失败";
    try {
      const payload = await response.json();
      message = payload?.error?.message || message;
    } catch {
      // keep default message
    }
    throw new Error(message);
  }
  const disposition = response.headers.get("Content-Disposition") || "";
  const match = /filename="?([^";]+)"?/i.exec(disposition);
  const blob = await response.blob();
  return { blob, filename: match?.[1] || `tokhub-channel-site-${siteID}.zip` };
}

export async function adminWebConfig(): Promise<{ site: SiteConfig }> {
  return readJSON<{ site: SiteConfig }>("/api/admin/web", { credentials: "include" })
    .then(rememberSitePayload);
}

export async function saveAdminWebConfig(input: SiteConfig): Promise<{ site: SiteConfig }> {
  return writeJSONRequest<{ site: SiteConfig }>("/api/admin/web", input, { method: "PATCH" })
    .then(rememberSitePayload);
}

export async function resetAdminWebConfig(): Promise<{ site: SiteConfig }> {
  return writeJSONRequest<{ site: SiteConfig }>("/api/admin/web/reset", {})
    .then(rememberSitePayload);
}
