// 浅封装：连 gateway WS，做 Auth + Subscribe + 事件分发。

export type WSEvent =
  | { type: "feed.state_change"; payload: Record<string, unknown> }
  | { type: "chat.message"; payload: Record<string, unknown> }
  | { type: "chat.moderation"; message_id: string; action: string }
  | { type: "unknown"; payload: Record<string, unknown> };

export class GatewayWS {
  private ws?: WebSocket;
  private listeners = new Set<(ev: WSEvent) => void>();

  constructor(private wsBase: string, private token: string) {}

  connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(`${this.wsBase}/ws`);
      this.ws.onopen = () => {
        this.ws!.send(JSON.stringify({ type: "auth", token: this.token }));
        resolve();
      };
      this.ws.onerror = (e) => reject(e);
      this.ws.onmessage = (msg) => {
        try {
          const obj = JSON.parse(msg.data);
          const ev = this.parse(obj);
          this.listeners.forEach((l) => l(ev));
        } catch {
          // ignore parse errors
        }
      };
    });
  }

  subscribe(roomId: string) {
    this.ws?.send(JSON.stringify({ type: "subscribe", room_id: roomId }));
  }

  sendChat(roomId: string, body: string, clientMsgId: string) {
    this.ws?.send(
      JSON.stringify({
        type: "chat.send",
        room_id: roomId,
        body,
        client_msg_id: clientMsgId,
      }),
    );
  }

  onEvent(fn: (ev: WSEvent) => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  close() {
    this.ws?.close();
    this.ws = undefined;
    this.listeners.clear();
  }

  parse(obj: Record<string, unknown>): WSEvent {
    const t = String(obj.type ?? "unknown");
    if (t.startsWith("feed.")) return { type: "feed.state_change", payload: obj };
    if (t === "room.chat.message" || t === "chat.message")
      return { type: "chat.message", payload: obj };
    if (t === "room.chat.moderation" || t === "chat.moderation") {
      return {
        type: "chat.moderation",
        message_id: String(obj.message_id ?? ""),
        action: String(obj.action ?? "hide"),
      };
    }
    return { type: "unknown", payload: obj };
  }
}
