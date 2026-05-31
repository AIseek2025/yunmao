// admin-svc API 客户端：要求 role=admin 的 RS256 JWT。

const base = (): string =>
  (typeof process !== "undefined" && process.env?.NEXT_PUBLIC_ADMIN_API_BASE) ||
  "http://localhost:18006";

function token(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem("yunmao.admin.token");
}

export function setToken(t: string): void {
  if (typeof window !== "undefined") {
    window.localStorage.setItem("yunmao.admin.token", t);
  }
}

export function clearToken(): void {
  if (typeof window !== "undefined") {
    window.localStorage.removeItem("yunmao.admin.token");
  }
}

export function hasToken(): boolean {
  return typeof window !== "undefined" && !!window.localStorage.getItem("yunmao.admin.token");
}

export interface LoginResponse {
  access_token: string;
  expires_in: number;
  token_type: string;
}

export async function adminLogin(password: string): Promise<LoginResponse> {
  const headers = new Headers({ "Content-Type": "application/json" });
  const r = await fetch(`${base()}/v1/auth/admin/login`, {
    method: "POST",
    headers,
    body: JSON.stringify({ password }),
  });
  if (!r.ok) throw new Error(`${r.status} ${await r.text()}`);
  return (await r.json()) as LoginResponse;
}

async function call<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers ?? {});
  const tk = token();
  if (tk) headers.set("Authorization", `Bearer ${tk}`);
  if (!headers.has("Content-Type") && init.body) {
    headers.set("Content-Type", "application/json");
  }
  const r = await fetch(`${base()}${path}`, { ...init, headers });
  if (!r.ok) throw new Error(`${r.status} ${await r.text()}`);
  if (r.status === 204) return undefined as T;
  return (await r.json()) as T;
}

export interface FeatureFlag {
  key: string;
  enabled: boolean;
  percent: number;
  description?: string;
  updated_at?: number;
}

export interface FeedingPolicy {
  room_id: string;
  cooldown_seconds: number;
  daily_grams_limit: number;
  feeding_window?: string;
  updated_at?: number;
}

export interface WordlistEntry {
  word: string;
  action: "hide" | "review" | "ban";
  region?: string;
  language?: string;
}

export interface Room {
  id: string;
  name: string;
  status: "offline" | "live" | "idle";
  owner_id?: string;
  region_id?: string;
  cover?: string;
  protocol_pref?: string;
  webrtc_eligible?: boolean;
}

export interface RoomListResponse {
  rooms: Room[];
  total?: number;
}

export interface RotateStreamKeyResponse {
  id: string;
  stream_key: string;
}

export interface Wallet {
  user_id: string;
  balance_fen: number;
  coins: number;
  updated_at?: string;
}

export interface WalletHold {
  id: string;
  user_id: string;
  amount_fen: number;
  status: string;
  room_id?: string;
  created_at?: string;
}

export const adminApi = {
  listFlags: () => call<{ flags: FeatureFlag[] }>("/v1/admin/feature-flags"),
  upsertFlag: (f: FeatureFlag) =>
    call<FeatureFlag>("/v1/admin/feature-flags", {
      method: "PUT",
      body: JSON.stringify(f),
    }),

  listPolicies: () =>
    call<{ policies: FeedingPolicy[] }>("/v1/admin/feeding-policy"),
  upsertPolicy: (p: FeedingPolicy) =>
    call<FeedingPolicy>("/v1/admin/feeding-policy", {
      method: "PUT",
      body: JSON.stringify(p),
    }),

  listWordlist: () =>
    call<{ entries: WordlistEntry[]; version: number }>(
      "/v1/admin/chat/wordlist",
    ),
  importWordlist: (entries: WordlistEntry[]) =>
    call<{ version: number; count: number }>("/v1/admin/chat/wordlist", {
      method: "PUT",
      body: JSON.stringify({ entries }),
    }),

  listRooms: (params: {
    owner_id?: string;
    region_id?: string;
    status?: string;
    limit?: number;
  } = {}) => {
    const q = new URLSearchParams();
    if (params.owner_id) q.set("owner_id", params.owner_id);
    if (params.region_id) q.set("region_id", params.region_id);
    if (params.status) q.set("status", params.status);
    if (params.limit) q.set("limit", String(params.limit));
    const qs = q.toString();
    return call<RoomListResponse>(
      `/v1/admin/rooms${qs ? "?" + qs : ""}`,
    );
  },
  getRoom: (id: string) => call<Room>(`/v1/admin/rooms/${id}`),
  rotateStreamKey: (id: string) =>
    call<RotateStreamKeyResponse>(`/v1/admin/rooms/${id}/rotate-stream-key`, {
      method: "POST",
    }),

  getWallet: (userId: string) => call<Wallet>(`/v1/admin/wallets/${userId}`),
  getWalletHold: (holdId: string) =>
    call<WalletHold>(`/v1/admin/wallets/holds/${holdId}`),
};
