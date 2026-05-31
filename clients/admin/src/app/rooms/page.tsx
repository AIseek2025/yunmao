"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { adminApi, type Room } from "@/lib/adminApi";
import { useState } from "react";

export default function AdminRoomsPage() {
  const qc = useQueryClient();
  const [statusFilter, setStatusFilter] = useState("");
  const [ownerId, setOwnerId] = useState("");

  const q = useQuery({
    queryKey: ["rooms", statusFilter, ownerId],
    queryFn: () =>
      adminApi.listRooms({
        status: statusFilter || undefined,
        owner_id: ownerId || undefined,
        limit: 100,
      }),
  });

  const rotateMut = useMutation({
    mutationFn: (id: string) => adminApi.rotateStreamKey(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["rooms"] }),
  });

  const rooms = q.data?.rooms ?? [];

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-bold">房间管理</h2>
        <span className="text-xs text-neutral-500">
          共 {rooms.length} 间
          {q.isLoading && " (加载中…)"}
        </span>
      </div>

      <div className="flex gap-2 items-center text-sm">
        <select
          className="border border-neutral-300 rounded px-2 py-1"
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
        >
          <option value="">全部状态</option>
          <option value="live">直播中</option>
          <option value="idle">空闲</option>
          <option value="offline">离线</option>
        </select>
        <input
          placeholder="owner_id 过滤"
          className="border border-neutral-300 rounded px-2 py-1"
          value={ownerId}
          onChange={(e) => setOwnerId(e.target.value)}
        />
      </div>

      {q.isError && (
        <div className="text-red-500">
          加载失败：{(q.error as Error).message}
        </div>
      )}

      <table className="w-full text-sm border-collapse">
        <thead>
          <tr className="border-b text-left">
            <th className="py-2">ID</th>
            <th>名称</th>
            <th>状态</th>
            <th>区域</th>
            <th>WebRTC</th>
            <th className="text-right">操作</th>
          </tr>
        </thead>
        <tbody>
          {rooms.map((room) => (
            <RoomRow
              key={room.id}
              room={room}
              onRotate={() => rotateMut.mutate(room.id)}
              rotating={rotateMut.isPending}
            />
          ))}
          {rooms.length === 0 && !q.isLoading && !q.isError && (
            <tr>
              <td colSpan={6} className="py-6 text-center text-neutral-400">
                暂无房间数据。确认 room-svc 已配置且 YUNMAO_ROOM_URL 已注入。
              </td>
            </tr>
          )}
        </tbody>
      </table>

      {rotateMut.isSuccess && (
        <div className="text-green-700 text-xs">
          推流 key 已重置：{rotateMut.data?.stream_key?.slice(0, 8)}…
        </div>
      )}
    </section>
  );
}

function RoomRow({
  room,
  onRotate,
  rotating,
}: {
  room: Room;
  onRotate: () => void;
  rotating: boolean;
}) {
  const statusColor =
    room.status === "live"
      ? "text-red-600"
      : room.status === "idle"
        ? "text-amber-600"
        : "text-neutral-400";

  return (
    <tr className="border-b hover:bg-neutral-50">
      <td className="py-2 font-mono text-xs">{room.id.slice(0, 8)}</td>
      <td>{room.name}</td>
      <td className={statusColor}>{room.status}</td>
      <td>{room.region_id ?? "-"}</td>
      <td>{room.webrtc_eligible ? "✓" : "-"}</td>
      <td className="text-right">
        <button
          onClick={onRotate}
          disabled={rotating}
          className="px-2 py-1 bg-neutral-800 text-white rounded text-xs disabled:opacity-50"
        >
          重置 stream key
        </button>
      </td>
    </tr>
  );
}
