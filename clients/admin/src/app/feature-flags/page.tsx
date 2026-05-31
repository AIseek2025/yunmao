"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { adminApi, type FeatureFlag } from "@/lib/adminApi";
import { useState } from "react";

export default function FeatureFlagsPage() {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["feature-flags"],
    queryFn: adminApi.listFlags,
  });
  const m = useMutation({
    mutationFn: adminApi.upsertFlag,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["feature-flags"] }),
  });

  return (
    <section>
      <h2 className="text-xl font-bold mb-4">Feature Flags</h2>
      {q.isError && (
        <div className="text-red-500">加载失败：{(q.error as Error).message}</div>
      )}
      <table className="w-full text-sm border-collapse">
        <thead>
          <tr className="border-b">
            <th className="text-left py-2">Key</th>
            <th className="text-left">启用</th>
            <th className="text-left">灰度 %</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {(q.data?.flags ?? []).map((f) => (
            <FlagRow key={f.key} flag={f} onSave={(x) => m.mutate(x)} />
          ))}
        </tbody>
      </table>
      <div className="mt-6">
        <NewFlag onSave={(f) => m.mutate(f)} />
      </div>
    </section>
  );
}

function FlagRow({
  flag,
  onSave,
}: {
  flag: FeatureFlag;
  onSave: (f: FeatureFlag) => void;
}) {
  const [draft, setDraft] = useState(flag);
  return (
    <tr className="border-b">
      <td className="py-2">{draft.key}</td>
      <td>
        <input
          type="checkbox"
          checked={draft.enabled}
          onChange={(e) => setDraft({ ...draft, enabled: e.target.checked })}
        />
      </td>
      <td>
        <input
          type="number"
          min={0}
          max={100}
          value={draft.percent}
          onChange={(e) => setDraft({ ...draft, percent: Number(e.target.value) })}
          className="w-16 border border-neutral-300 rounded px-2"
        />
      </td>
      <td>
        <button
          onClick={() => onSave(draft)}
          className="px-2 py-1 bg-neutral-800 text-white rounded text-xs"
        >
          保存
        </button>
      </td>
    </tr>
  );
}

function NewFlag({ onSave }: { onSave: (f: FeatureFlag) => void }) {
  const [key, setKey] = useState("");
  const [pct, setPct] = useState(0);
  return (
    <div className="flex gap-2 items-center">
      <input
        placeholder="key (e.g. room.webrtc.enabled)"
        value={key}
        onChange={(e) => setKey(e.target.value)}
        className="border border-neutral-300 rounded px-2 py-1"
      />
      <input
        type="number"
        min={0}
        max={100}
        value={pct}
        onChange={(e) => setPct(Number(e.target.value))}
        className="w-16 border border-neutral-300 rounded px-2 py-1"
      />
      <button
        disabled={!key}
        onClick={() => onSave({ key, enabled: pct > 0, percent: pct })}
        className="px-3 py-1 rounded bg-neutral-900 text-white"
      >
        新建
      </button>
    </div>
  );
}
