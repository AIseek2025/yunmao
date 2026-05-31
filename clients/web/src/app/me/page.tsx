"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api } from "@/lib/api";
import { useSession } from "@/lib/session";

export default function MePage() {
  const router = useRouter();
  const session = useSession();
  const userId = session.currentUserId();
  const wallet = useQuery({
    queryKey: ["wallet", userId],
    queryFn: () => api.wallet(userId),
    enabled: !!userId,
  });

  function handleLogout() {
    session.logout();
    router.push("/login");
  }

  return (
    <main className="min-h-screen p-6 max-w-2xl mx-auto">
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-2xl font-bold">我的</h1>
        <div className="flex gap-3">
          <Link className="text-sm text-neutral-500 hover:text-neutral-800" href="/rooms">
            直播房间
          </Link>
          {userId && (
            <button
              onClick={handleLogout}
              className="text-sm text-neutral-500 hover:text-neutral-800"
            >
              退出登录
            </button>
          )}
        </div>
      </div>
      <div className="rounded-md border border-neutral-200 p-4 space-y-2">
        <div>用户：{userId || "未登录"}</div>
        <div>
          钱包余额：
          {wallet.data
            ? `¥${(wallet.data.balance_fen / 100).toFixed(2)} · ${wallet.data.coins} 金币`
            : "—"}
        </div>
        <Link className="text-brand" href="/me/wallet">
          充值 →
        </Link>
      </div>
    </main>
  );
}
