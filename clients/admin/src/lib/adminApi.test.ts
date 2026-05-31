import { beforeEach, describe, expect, it, vi } from "vitest";
import { adminApi, adminLogin, clearToken, hasToken, setToken } from "./adminApi";

describe("adminApi", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: () => Promise.resolve({}),
        text: () => Promise.resolve(""),
      }),
    );
    window.localStorage.setItem("yunmao.admin.token", "test-admin-token");
  });

  it("listRooms calls /v1/admin/rooms without params", async () => {
    await adminApi.listRooms();
    expect(globalThis.fetch).toHaveBeenCalledTimes(1);
    const called = (globalThis.fetch as ReturnType<typeof vi.fn>).mock
      .calls[0];
    expect(called[0]).toMatch(/\/v1\/admin\/rooms$/);
  });

  it("listRooms forwards status and owner_id query params", async () => {
    await adminApi.listRooms({
      status: "live",
      owner_id: "u_demo",
      limit: 25,
    });
    const called = (globalThis.fetch as ReturnType<typeof vi.fn>).mock
      .calls[0];
    const url = called[0] as string;
    expect(url).toContain("status=live");
    expect(url).toContain("owner_id=u_demo");
    expect(url).toContain("limit=25");
  });

  it("getWallet calls /v1/admin/wallets/{user_id}", async () => {
    await adminApi.getWallet("u_abc");
    const called = (globalThis.fetch as ReturnType<typeof vi.fn>).mock
      .calls[0];
    expect(called[0]).toMatch(/\/v1\/admin\/wallets\/u_abc$/);
  });

  it("getWalletHold calls /v1/admin/wallets/holds/{hold_id}", async () => {
    await adminApi.getWalletHold("h_xyz");
    const called = (globalThis.fetch as ReturnType<typeof vi.fn>).mock
      .calls[0];
    expect(called[0]).toMatch(/\/v1\/admin\/wallets\/holds\/h_xyz$/);
  });

  it("rotateStreamKey POSTs to /v1/admin/rooms/{id}/rotate-stream-key", async () => {
    await adminApi.rotateStreamKey("room_z");
    const called = (globalThis.fetch as ReturnType<typeof vi.fn>).mock
      .calls[0];
    expect(called[0]).toMatch(/\/v1\/admin\/rooms\/room_z\/rotate-stream-key$/);
    expect((called[1] as RequestInit)?.method).toBe("POST");
  });

  it("sends Bearer token from localStorage", async () => {
    await adminApi.listFlags();
    const called = (globalThis.fetch as ReturnType<typeof vi.fn>).mock
      .calls[0];
    const headers = (called[1] as RequestInit)?.headers as Headers;
    expect(headers.get("Authorization")).toBe("Bearer test-admin-token");
  });
});

describe("adminLogin", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: () =>
          Promise.resolve({
            access_token: "jwt-123",
            expires_in: 28800,
            token_type: "Bearer",
          }),
        text: () => Promise.resolve(""),
      }),
    );
  });

  it("POSTs to /v1/auth/admin/login with password", async () => {
    const resp = await adminLogin("s3cret");
    expect(resp.access_token).toBe("jwt-123");
    const called = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(called[0]).toMatch(/\/v1\/auth\/admin\/login$/);
    expect((called[1] as RequestInit)?.method).toBe("POST");
    expect((called[1] as RequestInit)?.body).toBe(
      JSON.stringify({ password: "s3cret" }),
    );
  });

  it("throws on auth failure", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        status: 401,
        text: () => Promise.resolve("unauthorized"),
      }),
    );
    await expect(adminLogin("wrong")).rejects.toThrow(/401/);
  });
});

describe("token helpers", () => {
  it("setToken + hasToken + clearToken round-trips", () => {
    window.localStorage.removeItem("yunmao.admin.token");
    expect(hasToken()).toBe(false);
    setToken("tok-1");
    expect(hasToken()).toBe(true);
    clearToken();
    expect(hasToken()).toBe(false);
  });
});
