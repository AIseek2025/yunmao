"use client";

import { useMemo, useState } from "react";
import { inGrayPercent } from "@/lib/gray";

export default function GraySimPage() {
  const [percent, setPercent] = useState(20);
  const [count, setCount] = useState(5000);
  const result = useMemo(() => {
    let hits = 0;
    const sample: string[] = [];
    for (let i = 0; i < count; i++) {
      const key = `room_${i}`;
      const hit = inGrayPercent(key, percent);
      if (hit) hits++;
      if (i < 20) sample.push(`${key}: ${hit ? "WHEP" : "HLS"}`);
    }
    return { hits, pct: ((hits / count) * 100).toFixed(2), sample };
  }, [percent, count]);

  return (
    <section className="space-y-4">
      <h2 className="text-xl font-bold">WebRTC 灰度模拟器</h2>
      <p className="text-sm text-neutral-500">
        本工具使用与后端 / iOS / Android / Web 完全一致的 FNV1a 32bit 哈希；
        给定 room_id 集合验证最终命中分布与配置的灰度百分比是否对得上。
      </p>
      <div className="flex gap-4 items-center">
        <label>
          百分比：
          <input
            type="number"
            min={0}
            max={100}
            value={percent}
            onChange={(e) => setPercent(Number(e.target.value))}
            className="ml-2 w-20 border rounded px-2 py-1"
          />
        </label>
        <label>
          样本数：
          <input
            type="number"
            min={1}
            max={100000}
            value={count}
            onChange={(e) => setCount(Number(e.target.value))}
            className="ml-2 w-24 border rounded px-2 py-1"
          />
        </label>
      </div>
      <div className="text-sm">
        命中：<span className="font-semibold">{result.hits}</span>（{result.pct}%）
      </div>
      <pre className="bg-neutral-100 p-3 rounded text-xs overflow-auto max-h-64">
        {result.sample.join("\n")}
      </pre>
    </section>
  );
}
