import type {
  AuthToken,
  ChatMessage,
  FeedRequest,
  FeedResponse,
  IceServersResponse,
  PrepayResponse,
  Room,
  RoomSubscription,
  Wallet,
} from "./types";

export class YunmaoApiError extends Error {
  constructor(public status: number, msg: string) {
    super(msg);
    this.name = "YunmaoApiError";
  }
}

const baseUrl = (): string =>
  (typeof process !== "undefined" && process.env?.NEXT_PUBLIC_API_BASE) ||
  "http://localhost:18000";

const mediaBaseUrl = (): string =>
  (typeof process !== "undefined" && process.env?.NEXT_PUBLIC_MEDIA_BASE) ||
  baseUrl();

type RoomSubscriptionWire = {
  token: string;
  expires_at?: string;
  room?: {
    id?: string;
    protocol_pref?: string;
    gray_hit_webrtc?: boolean;
  };
  room_id?: string;
  url_playback?: string;
  url_whep?: string;
  webrtc_enabled?: boolean;
};

let tokenGetter: () => string | null = () => {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem("yunmao.token");
};

export function setTokenGetter(fn: () => string | null) {
  tokenGetter = fn;
}

function currentUserId(): string {
  const token = tokenGetter();
  if (!token) return "guest-user";
  try {
    const payload = token.split(".")[1];
    if (!payload) return "guest-user";
    const json = JSON.parse(atob(payload.replace(/-/g, "+").replace(/_/g, "/")));
    return typeof json.sub === "string" && json.sub ? json.sub : "guest-user";
  } catch {
    return "guest-user";
  }
}

async function request<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const token = tokenGetter();
  const headers = new Headers(init.headers ?? {});
  headers.set("Accept", "application/json");
  if (!headers.has("Content-Type") && init.body) {
    headers.set("Content-Type", "application/json");
  }
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const resp = await fetch(`${baseUrl()}${path}`, {
    ...init,
    headers,
  });
  if (!resp.ok) {
    const text = await resp.text().catch(() => "");
    throw new YunmaoApiError(resp.status, text || resp.statusText);
  }
  if (resp.status === 204) return undefined as T;
  return (await resp.json()) as T;
}

function normalizeSubscription(roomId: string, payload: RoomSubscriptionWire): RoomSubscription {
  const mediaBase = mediaBaseUrl().replace(/\/$/, "");
  const resolvedRoomId = payload.room?.id || payload.room_id || roomId;
  const protocolPref = payload.room?.protocol_pref;
  const grayHitWebRtc = payload.room?.gray_hit_webrtc ?? false;
  const webrtcEnabled =
    payload.webrtc_enabled ?? (protocolPref === "webrtc" || grayHitWebRtc);

  return {
    room_id: resolvedRoomId,
    token: payload.token,
    url_playback:
      payload.url_playback || `${mediaBase}/live/${resolvedRoomId}/index_ll.m3u8`,
    url_whep: payload.url_whep || (webrtcEnabled ? `${mediaBase}/whep/${resolvedRoomId}` : undefined),
    webrtc_enabled: webrtcEnabled,
  } as RoomSubscription;
}

export const api = {
  async login(phone: string, code: string): Promise<AuthToken> {
    void code;
    return request("/api/v1/auth/login", {
      method: "POST",
      body: JSON.stringify({ phone_e164: phone }),
    });
  },
  async listRooms(): Promise<Room[]> {
    const r = await request<{ rooms: Room[] }>("/api/v1/rooms");
    return r.rooms;
  },
  subscription(roomId: string): Promise<RoomSubscription> {
    return request<RoomSubscriptionWire>(
      `/api/v1/rooms/${roomId}/subscriptions`,
      { method: "POST", body: JSON.stringify({}) },
    ).then((resp) => normalizeSubscription(roomId, resp));
  },
  iceServers(roomId: string): Promise<IceServersResponse> {
    return request(`/api/v1/rooms/${roomId}/ice-servers`);
  },
  feed(req: FeedRequest): Promise<FeedResponse> {
    return request("/api/v1/feed-requests", {
      method: "POST",
      body: JSON.stringify({
        room_id: req.room_id,
        user_id: req.user_id,
        amount_grams: req.grams,
        idempotency_key: req.idempotency_key,
      }),
    });
  },
  wallet(userId: string): Promise<Wallet> {
    return request(`/api/v1/wallets/${userId}`);
  },
  async createPrepay(
    orderId: string,
    channel: PrepayResponse["channel"],
    amountFen: number,
  ): Promise<PrepayResponse> {
    const order = await request<{ id: string }>("/api/v1/orders", {
      method: "POST",
      body: JSON.stringify({
        user_id: currentUserId(),
        channel,
        biz_type: "membership",
        amount_cny: Math.max(1, Math.round(amountFen / 100)),
        idempotency_key: orderId,
      }),
    });
    return request(`/api/v1/orders/${order.id}/prepay?channel=${channel}`, {
      method: "POST",
      body: JSON.stringify({ amount_fen: amountFen, subject: "yunmao 充值" }),
    });
  },
  recentMessages(roomId: string): Promise<ChatMessage[]> {
    return request<{ messages?: ChatMessage[] } | ChatMessage[]>(
      `/api/v1/rooms/${roomId}/chat`,
    ).then((resp) => (Array.isArray(resp) ? resp : resp.messages ?? []));
  },
};
