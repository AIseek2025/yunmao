"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { api } from "@/lib/api";
import { useSession } from "@/lib/session";

export default function LoginPage() {
  const router = useRouter();
  const session = useSession();
  const [phone, setPhone] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  async function submit() {
    setBusy(true);
    setErr("");
    try {
      const t = await api.login(phone, code);
      session.login(t);
      router.push("/rooms");
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="min-h-screen flex items-center justify-center px-4">
      <div className="w-full max-w-sm space-y-4 rounded-lg border border-neutral-200 p-6 shadow-sm">
        <h1 className="text-xl font-semibold">登录 yunmao</h1>
        <input
          aria-label="phone"
          value={phone}
          onChange={(e) => setPhone(e.target.value)}
          placeholder="手机号"
          className="w-full rounded-md border border-neutral-300 px-3 py-2"
        />
        <input
          aria-label="code"
          value={code}
          onChange={(e) => setCode(e.target.value)}
          placeholder="验证码"
          className="w-full rounded-md border border-neutral-300 px-3 py-2"
        />
        {err && <div className="text-sm text-red-500">{err}</div>}
        <button
          onClick={submit}
          disabled={busy || !phone || !code}
          className="w-full rounded-md bg-brand text-white py-2 disabled:bg-neutral-300"
        >
          {busy ? "登录中…" : "登录"}
        </button>
      </div>
    </main>
  );
}
