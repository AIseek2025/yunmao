"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { adminApi, type WordlistEntry } from "@/lib/adminApi";
import { useState } from "react";

export default function WordlistPage() {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["wordlist"],
    queryFn: adminApi.listWordlist,
  });
  const m = useMutation({
    mutationFn: adminApi.importWordlist,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["wordlist"] }),
  });
  const [csv, setCsv] = useState("");

  function parseCsv(): WordlistEntry[] {
    return csv
      .split(/\r?\n/)
      .map((l) => l.trim())
      .filter((l) => l && !l.startsWith("#"))
      .map((l) => {
        const [word, action = "hide", region = "cn", language = "zh-CN"] = l.split(",");
        return { word, action: action as WordlistEntry["action"], region, language };
      });
  }

  return (
    <section>
      <h2 className="text-xl font-bold mb-2">弹幕词表</h2>
      <p className="text-sm text-neutral-500 mb-4">
        当前版本：{q.data?.version ?? "—"}（admin-svc 保存后会向 chat-svc 广播
        <code className="mx-1 px-1 bg-neutral-100 rounded">chat.wordlist.updated</code>）。
      </p>
      <textarea
        className="w-full h-40 border rounded p-2 font-mono text-sm"
        placeholder={`# CSV: word,action(hide|review|ban),region,language\n吃饭,hide,cn,zh-CN`}
        value={csv}
        onChange={(e) => setCsv(e.target.value)}
      />
      <div className="mt-2 flex gap-2">
        <button
          onClick={() => m.mutate(parseCsv())}
          className="px-3 py-1 rounded bg-brand-700 text-white"
          disabled={!csv.trim()}
        >
          导入
        </button>
        <span className="text-sm text-neutral-500 self-center">
          条目：{q.data?.entries.length ?? 0}
        </span>
      </div>
      <ul className="mt-4 text-sm space-y-1 max-h-64 overflow-auto border rounded p-2">
        {(q.data?.entries ?? []).map((e, i) => (
          <li key={`${e.word}-${i}`} className="flex justify-between border-b py-1">
            <span>{e.word}</span>
            <span className="text-xs text-neutral-500">
              {e.action} · {e.region ?? "*"} · {e.language ?? "*"}
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}
