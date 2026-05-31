import { describe, expect, it } from "vitest";
import type {
  DeviceAuthToken,
  NewFeedResponse,
  Room,
} from "./types";

describe("contract type shape", () => {
  it("DeviceAuthToken fields match v3.json contract", () => {
    const token: DeviceAuthToken = {
      token: "t",
      user_id: "550e8400-e29b-41d4-a716-446655440000",
      expires_at: 1_700_000_000,
    };
    expect(token.token).toBe("t");
    expect(typeof token.expires_at).toBe("number");
  });

  it("NewFeedResponse status enum covers expected states", () => {
    const statuses: Array<NewFeedResponse["status"]> = [
      "pending",
      "queued",
      "dispensing",
      "completed",
      "failed",
      "timeout",
      "cancelled",
    ];
    expect(statuses).toHaveLength(7);
  });

  it("Room schema includes cover field", () => {
    const room: Room = {
      id: "id",
      name: "n",
      status: "offline",
      owner_id: "550e8400-e29b-41d4-a716-446655440000",
      cover: "cover.png",
    };
    expect(room.cover).toBe("cover.png");
  });
});
