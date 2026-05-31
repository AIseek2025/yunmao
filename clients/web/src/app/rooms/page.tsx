"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import type { Route } from "next";
import { api } from "@/lib/api";
import { inGrayPercent } from "@/lib/gray";

const GRAY_PERCENT = 50;

export default function RoomsPage() {
  const { data, error, isPending } = useQuery({
    queryKey: ["rooms"],
    queryFn: api.listRooms,
  });

  if (isPending) return <main className="p-6">加载中…</main>;
  if (error)
    return <main className="p-6 text-red-500">加载失败：{(error as Error).message}</main>;

  return (
    <main className="min-h-screen p-6">
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-2xl font-bold">直播房间</h1>
        <Link className="text-sm text-neutral-500 hover:text-neutral-800" href="/me">
          我的
        </Link>
      </div>
      <ul className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {data!.map((r) => {
          const webrtc = inGrayPercent(r.id, GRAY_PERCENT);
          return (
            <li key={r.id} className="rounded-lg border border-neutral-200 p-4">
              <div className="flex justify-between">
                <div>
                  <div className="text-lg font-medium">{r.name}</div>
                  <div className="text-xs text-neutral-500">{r.id}</div>
                </div>
                <span className="text-xs rounded bg-neutral-100 px-2 py-1">
                  {webrtc ? "WebRTC" : "LL-HLS"}
                </span>
              </div>
              <Link
                href={`/rooms/${r.id}` as Route<`/rooms/${string}`>}
                className="mt-3 inline-block text-brand"
              >
                进入直播 →
              </Link>
            </li>
          );
        })}
      </ul>
    </main>
  );
}
