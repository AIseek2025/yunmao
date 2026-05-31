"use client";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { adminLogin, hasToken, setToken } from "@/lib/adminApi";

export default function LoginPage() {
  const router = useRouter();
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  if (hasToken()) {
    router.replace("/feature-flags");
    return null;
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      const resp = await adminLogin(password);
      setToken(resp.access_token);
      router.push("/feature-flags");
    } catch (err) {
      setError(err instanceof Error ? err.message : "login failed");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="flex min-h-[80vh] items-center justify-center">
      <form
        onSubmit={handleSubmit}
        className="w-full max-w-sm space-y-4 rounded border border-neutral-200 bg-white p-8 shadow-sm"
      >
        <h1 className="text-xl font-bold">yunmao 运营后台登录</h1>
        <p className="text-sm text-neutral-500">
          请输入管理员密码以签发 admin JWT。
        </p>

        <label className="block">
          <span className="text-sm font-medium">管理员密码</span>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoFocus
            required
            className="mt-1 block w-full rounded border border-neutral-300 px-3 py-2 text-sm focus:border-neutral-500 focus:outline-none"
          />
        </label>

        {error && (
          <p className="text-sm text-red-600">
            登录失败：{error}
          </p>
        )}

        <button
          type="submit"
          disabled={submitting || !password}
          className="w-full rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white hover:bg-neutral-800 disabled:opacity-50"
        >
          {submitting ? "登录中…" : "登录"}
        </button>
      </form>
    </div>
  );
}
