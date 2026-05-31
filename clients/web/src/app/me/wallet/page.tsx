"use client";

import { useState } from "react";
import { api } from "@/lib/api";
import { useSession } from "@/lib/session";
import type { PrepayResponse } from "@/lib/types";

const PACKS = [
  { id: "small", label: "30 元 · 150 金币", amount_fen: 3000 },
  { id: "medium", label: "98 元 · 500 金币", amount_fen: 9800 },
  { id: "large", label: "298 元 · 1600 金币", amount_fen: 29800 },
];

export default function WalletPage() {
  const session = useSession();
  const [channel, setChannel] = useState<"wechatpay" | "alipay">("wechatpay");
  const [pending, setPending] = useState<PrepayResponse | null>(null);
  const [err, setErr] = useState("");

  async function buy(amount: number) {
    if (!session.token) return;
    setErr("");
    try {
      const orderId = `order_${Date.now()}`;
      const r = await api.createPrepay(orderId, channel, amount);
      setPending(r);
    } catch (e) {
      setErr((e as Error).message);
    }
  }

  return (
    <main className="min-h-screen p-6 max-w-2xl mx-auto space-y-4">
      <h1 className="text-2xl font-bold">充值</h1>
      <div className="flex gap-2">
        {(["wechatpay", "alipay"] as const).map((c) => (
          <button
            key={c}
            onClick={() => setChannel(c)}
            className={`px-3 py-1 rounded border ${channel === c ? "bg-brand text-white border-brand" : "border-neutral-300"}`}
          >
            {c === "wechatpay" ? "微信支付" : "支付宝"}
          </button>
        ))}
      </div>
      <ul className="space-y-2">
        {PACKS.map((p) => (
          <li
            key={p.id}
            className="flex justify-between rounded border border-neutral-200 p-3"
          >
            <span>{p.label}</span>
            <button
              onClick={() => buy(p.amount_fen)}
              className="px-3 py-1 rounded bg-brand text-white"
            >
              购买
            </button>
          </li>
        ))}
      </ul>
      {err && <div className="text-red-500 text-sm">{err}</div>}
      {pending && (
        <div className="rounded border border-neutral-200 p-3 text-sm">
          <div>渠道：{pending.channel}</div>
          <div>prepay_id：{pending.prepay_id}</div>
          {pending.qr_content && (
            <div className="mt-2">扫码内容：{pending.qr_content}</div>
          )}
          <div className="mt-2 text-neutral-500">
            支付完成后后端 webhook 自动到账，本页面会自动刷新余额。
          </div>
        </div>
      )}
    </main>
  );
}
