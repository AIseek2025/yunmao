"use client";

import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { api } from "@/lib/api";
import { useSession } from "@/lib/session";
import { attachWhep, pickProtocol } from "@/lib/playback";
import { GatewayWS, type WSEvent } from "@/lib/ws";

const GRAY_PERCENT = 50;
const WS_BASE = (typeof process !== "undefined" && process.env.NEXT_PUBLIC_WS_BASE) || "ws://localhost:18007";

interface ChatMsgVM {
  id: string;
  user_id: string;
  body: string;
  moderation: "ok" | "hide" | "review";
}

export default function RoomDetailPage() {
  const params = useParams<{ id: string }>();
  const roomId = params.id;
  const session = useSession();
  const videoRef = useRef<HTMLVideoElement>(null);
  const [feedStatus, setFeedStatus] = useState<string>("idle");
  const [chat, setChat] = useState<ChatMsgVM[]>([]);
  const [draft, setDraft] = useState("");
  const wsRef = useRef<GatewayWS | null>(null);

  const subQ = useQuery({
    queryKey: ["subscription", roomId],
    queryFn: () => api.subscription(roomId),
  });

  useEffect(() => {
    if (!subQ.data || !videoRef.current) return;
    const protocol = pickProtocol(subQ.data, GRAY_PERCENT);
    let pc: RTCPeerConnection | undefined;
    let hls: { destroy: () => void } | undefined;
    (async () => {
      if (protocol === "whep" && subQ.data!.url_whep) {
        try {
          const ice = await api.iceServers(roomId).catch(() => ({ ice_servers: [], expires_at: 0 }));
          pc = await attachWhep(videoRef.current!, subQ.data!.url_whep, subQ.data!.token, ice.ice_servers);
        } catch (e) {
          console.warn("WHEP fallback to HLS:", e);
          await attachHls(videoRef.current!, subQ.data!.url_playback);
        }
      } else {
        hls = (await attachHls(videoRef.current!, subQ.data!.url_playback)) ?? undefined;
      }
    })();
    return () => {
      pc?.close();
      hls?.destroy();
    };
  }, [subQ.data, roomId]);

  useEffect(() => {
    if (!subQ.data || !session.token) return;
    const ws = new GatewayWS(WS_BASE, session.token.access_token);
    let alive = true;
    ws.connect().then(() => {
      if (!alive) return;
      ws.subscribe(roomId);
    });
    const off = ws.onEvent((ev: WSEvent) => {
      if (ev.type === "feed.state_change") {
        const status = String((ev.payload as { status?: string }).status ?? "");
        if (status) setFeedStatus(status);
      } else if (ev.type === "chat.message") {
        const p = ev.payload as { id?: string; user_id?: string; body?: string };
        setChat((c) =>
          c.concat([
            {
              id: p.id ?? `${Date.now()}`,
              user_id: p.user_id ?? "anon",
              body: p.body ?? "",
              moderation: "ok",
            },
          ]),
        );
      } else if (ev.type === "chat.moderation") {
        setChat((c) =>
          c.map((m) =>
            m.id === ev.message_id ? { ...m, moderation: ev.action as ChatMsgVM["moderation"] } : m,
          ),
        );
      }
    });
    wsRef.current = ws;
    return () => {
      wsRef.current = null;
      alive = false;
      off();
      ws.close();
    };
  }, [subQ.data, session.token, roomId]);

  async function feed() {
    if (!session.token) return;
    setFeedStatus("pending");
    await api
      .feed({
        room_id: roomId,
        user_id: session.token.user?.id ?? "",
        grams: 5,
        feed_ticket_id: crypto.randomUUID(),
        idempotency_key: crypto.randomUUID(),
      })
      .catch(() => setFeedStatus("failed"));
  }

  return (
    <main className="min-h-screen p-4 max-w-4xl mx-auto">
      <div className="flex items-center justify-between mb-3">
        <Link className="text-sm text-neutral-500 hover:text-neutral-800" href="/rooms">
          ← 房间列表
        </Link>
        <Link className="text-sm text-neutral-500 hover:text-neutral-800" href="/me">
          我的
        </Link>
      </div>
      <div className="grid lg:grid-cols-[3fr,2fr] gap-4">
        <div>
          <video
            ref={videoRef}
            className="w-full aspect-video bg-black rounded-md"
            playsInline
            autoPlay
            controls
            muted
          />
          <div className="mt-3 flex items-center gap-3">
            <button
              onClick={feed}
              className="px-4 py-2 rounded-md bg-brand text-white"
            >
              投喂 5g
            </button>
            <span className="text-sm text-neutral-500">状态：{feedStatus}</span>
          </div>
        </div>
        <aside className="rounded-md border border-neutral-200 p-3 h-[60vh] flex flex-col">
          <h3 className="font-medium mb-2">弹幕</h3>
          <div className="flex-1 overflow-auto text-sm space-y-1">
            {chat.map((m) => (
              <div
                key={m.id}
                className={
                  m.moderation === "hide"
                    ? "text-neutral-400 italic"
                    : m.moderation === "review"
                      ? "text-amber-500"
                      : ""
                }
              >
                <span className="font-semibold">{m.user_id}:</span>{" "}
                {m.moderation === "hide" ? "[已撤回]" : m.body}
              </div>
            ))}
          </div>
          <div className="mt-2 flex gap-2">
            <input
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder="说点什么…"
              className="flex-1 border border-neutral-300 rounded px-2 py-1"
            />
            <button
              className="px-3 rounded bg-neutral-800 text-white text-sm"
              onClick={() => {
                if (!draft) return;
                const clientMsgId = crypto.randomUUID();
                wsRef.current?.sendChat(roomId, draft, clientMsgId);
                setChat((c) =>
                  c.concat([
                    {
                      id: clientMsgId,
                      user_id: session.currentUserId() || "me",
                      body: draft,
                      moderation: "ok",
                    },
                  ]),
                );
                setDraft("");
              }}
            >
              发送
            </button>
          </div>
        </aside>
      </div>
    </main>
  );
}

async function attachHls(
  video: HTMLVideoElement,
  url: string,
): Promise<{ destroy: () => void } | null> {
  if (video.canPlayType("application/vnd.apple.mpegurl")) {
    video.src = url;
    return { destroy: () => { video.src = ""; } };
  }
  const { default: Hls } = await import("hls.js");
  if (!Hls.isSupported()) {
    video.src = url;
    return { destroy: () => { video.src = ""; } };
  }
  const hls = new Hls({ lowLatencyMode: true });
  hls.loadSource(url);
  hls.attachMedia(video);
  return { destroy: () => hls.destroy() };
}
