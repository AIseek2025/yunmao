"use client";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";
import { hasToken } from "@/lib/adminApi";

const NAV = [
  { href: "/feature-flags", label: "Feature Flags" },
  { href: "/feeding-policy", label: "投喂策略" },
  { href: "/chat/wordlist", label: "弹幕词表" },
  { href: "/rooms", label: "房间管理" },
  { href: "/wallet", label: "钱包流水" },
  { href: "/webrtc/gray-sim", label: "WebRTC 灰度" },
];

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const isLogin = pathname === "/login";

  useEffect(() => {
    if (!isLogin && !hasToken()) {
      router.replace("/login");
    }
  }, [isLogin, router]);

  if (isLogin) {
    return <main className="p-6">{children}</main>;
  }

  if (!hasToken()) {
    return null;
  }

  return (
    <div className="min-h-screen grid grid-cols-[200px,1fr]">
      <nav className="border-r border-neutral-200 p-4 space-y-1">
        <h1 className="text-lg font-bold mb-4">yunmao 运营</h1>
        {NAV.map((n) => (
          <Link
            key={n.href}
            href={n.href}
            className="block rounded px-2 py-1 hover:bg-neutral-100"
          >
            {n.label}
          </Link>
        ))}
      </nav>
      <main className="p-6">{children}</main>
    </div>
  );
}
