"use client";

import { create } from "zustand";
import type { AuthToken } from "./types";
import { setTokenGetter } from "./api";

interface SessionState {
  token: AuthToken | null;
  login(t: AuthToken): void;
  logout(): void;
  isAuthenticated(): boolean;
  currentUserId(): string;
  hydrate(): void;
}

const STORAGE_KEY = "yunmao.session";

export const useSession = create<SessionState>((set, get) => ({
  token: null,
  login(t) {
    if (typeof window !== "undefined") {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(t));
      window.localStorage.setItem("yunmao.token", t.access_token);
    }
    set({ token: t });
  },
  logout() {
    if (typeof window !== "undefined") {
      window.localStorage.removeItem(STORAGE_KEY);
      window.localStorage.removeItem("yunmao.token");
    }
    set({ token: null });
  },
  isAuthenticated() {
    const t = get().token;
    return !!t;
  },
  currentUserId() {
    return get().token?.user?.id ?? "";
  },
  hydrate() {
    if (typeof window === "undefined") return;
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return;
    try {
      const t = JSON.parse(raw) as AuthToken;
      set({ token: t });
    } catch {
      // ignore
    }
  },
}));

// 让 api.ts 能拿到当前 token。
if (typeof window !== "undefined") {
  setTokenGetter(() => useSession.getState().token?.access_token ?? null);
}
