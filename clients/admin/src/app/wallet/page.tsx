"use client";

import { adminApi, type Wallet, type WalletHold } from "@/lib/adminApi";
import { useState } from "react";

export default function WalletPage() {
  const [userId, setUserId] = useState("");
  const [holdId, setHoldId] = useState("");
  const [mode, setMode] = useState<"user" | "hold">("user");
  const [wallet, setWallet] = useState<Wallet | null>(null);
  const [hold, setHold] = useState<WalletHold | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function lookup() {
    setErr(null);
    setWallet(null);
    setHold(null);
    setLoading(true);
    try {
      if (mode === "user") {
        if (!userId) {
          setErr("请输入 user_id");
          return;
        }
        const w = await adminApi.getWallet(userId);
        setWallet(w);
      } else {
        if (!holdId) {
          setErr("请输入 hold_id");
          return;
        }
        const h = await adminApi.getWalletHold(holdId);
        setHold(h);
      }
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setLoading(false);
    }
  }

  const balanceYuan = wallet
    ? (wallet.balance_fen / 100).toFixed(2)
    : null;

  return (
    <section className="space-y-4">
      <h2 className="text-xl font-bold">钱包流水</h2>
      <p className="text-sm text-neutral-500">
        admin-svc → billing-svc 只读代理。输入 user_id 查余额/持币，或 hold_id 查预扣状态。
      </p>

      <div className="flex gap-2 items-center text-sm">
        <button
          onClick={() => setMode("user")}
          className={`px-3 py-1 rounded ${
            mode === "user"
              ? "bg-neutral-900 text-white"
              : "bg-neutral-100 text-neutral-700"
          }`}
        >
          按用户
        </button>
        <button
          onClick={() => setMode("hold")}
          className={`px-3 py-1 rounded ${
            mode === "hold"
              ? "bg-neutral-900 text-white"
              : "bg-neutral-100 text-neutral-700"
          }`}
        >
          按 Hold
        </button>
      </div>

      <div className="flex gap-2">
        {mode === "user" ? (
          <input
            placeholder="user_id (UUID)"
            value={userId}
            onChange={(e) => setUserId(e.target.value)}
            className="border border-neutral-300 rounded px-2 py-1 w-80"
          />
        ) : (
          <input
            placeholder="hold_id (UUID)"
            value={holdId}
            onChange={(e) => setHoldId(e.target.value)}
            className="border border-neutral-300 rounded px-2 py-1 w-80"
          />
        )}
        <button
          onClick={lookup}
          disabled={loading}
          className="px-3 py-1 rounded bg-neutral-900 text-white disabled:opacity-50"
        >
          {loading ? "查询中…" : "查询"}
        </button>
      </div>

      {err && <div className="text-red-500 text-sm">{err}</div>}

      {wallet && (
        <div className="border border-neutral-200 rounded p-4 space-y-2 max-w-md">
          <div className="text-sm text-neutral-500">
            user_id: <span className="font-mono">{wallet.user_id}</span>
          </div>
          <div className="text-2xl font-bold">
            ¥{balanceYuan}
            <span className="text-sm font-normal text-neutral-500 ml-2">
              ({wallet.balance_fen} 分 / {wallet.coins} 币)
            </span>
          </div>
          {wallet.updated_at && (
            <div className="text-xs text-neutral-400">
              更新于 {wallet.updated_at}
            </div>
          )}
        </div>
      )}

      {hold && (
        <div className="border border-neutral-200 rounded p-4 space-y-2 max-w-md">
          <div className="flex justify-between">
            <span className="text-sm text-neutral-500">hold_id</span>
            <span className="font-mono text-xs">{hold.id}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-sm text-neutral-500">user_id</span>
            <span className="font-mono text-xs">{hold.user_id}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-sm text-neutral-500">金额</span>
            <span>¥{(hold.amount_fen / 100).toFixed(2)}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-sm text-neutral-500">状态</span>
            <span
              className={
                hold.status === "confirmed"
                  ? "text-green-700"
                  : hold.status === "cancelled"
                    ? "text-red-600"
                    : "text-amber-600"
              }
            >
              {hold.status}
            </span>
          </div>
          {hold.room_id && (
            <div className="flex justify-between">
              <span className="text-sm text-neutral-500">room_id</span>
              <span className="font-mono text-xs">{hold.room_id}</span>
            </div>
          )}
        </div>
      )}
    </section>
  );
}
