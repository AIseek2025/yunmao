"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { adminApi, type FeedingPolicy } from "@/lib/adminApi";
import { useState } from "react";

export default function FeedingPolicyPage() {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["policies"],
    queryFn: adminApi.listPolicies,
  });
  const m = useMutation({
    mutationFn: adminApi.upsertPolicy,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["policies"] }),
  });

  return (
    <section>
      <h2 className="text-xl font-bold mb-4">投喂策略（按房间）</h2>
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b text-left">
            <th className="py-2">Room ID</th>
            <th>冷却秒</th>
            <th>每日上限 g</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {(q.data?.policies ?? []).map((p) => (
            <Row key={p.room_id} p={p} onSave={(x) => m.mutate(x)} />
          ))}
        </tbody>
      </table>
    </section>
  );
}

function Row({ p, onSave }: { p: FeedingPolicy; onSave: (p: FeedingPolicy) => void }) {
  const [d, setD] = useState(p);
  return (
    <tr className="border-b">
      <td className="py-1">{d.room_id}</td>
      <td>
        <input
          type="number"
          value={d.cooldown_seconds}
          onChange={(e) =>
            setD({ ...d, cooldown_seconds: Number(e.target.value) })
          }
          className="w-20 border rounded px-2"
        />
      </td>
      <td>
        <input
          type="number"
          value={d.daily_grams_limit}
          onChange={(e) =>
            setD({ ...d, daily_grams_limit: Number(e.target.value) })
          }
          className="w-20 border rounded px-2"
        />
      </td>
      <td>
        <button
          onClick={() => onSave(d)}
          className="px-2 py-1 bg-neutral-800 text-white rounded text-xs"
        >
          保存
        </button>
      </td>
    </tr>
  );
}
