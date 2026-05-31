import { describe, expect, it, vi, beforeEach } from "vitest";
import { GatewayWS } from "./ws";

describe("GatewayWS.parse", () => {
  let ws: GatewayWS;
  beforeEach(() => {
    ws = new GatewayWS("ws://localhost:0", "fake-token");
  });

  it("parses chat.send message format", () => {
    const ev = ws.parse({
      type: "chat.message",
      id: "msg-1",
      user_id: "user-1",
      body: "hello",
    });
    expect(ev.type).toBe("chat.message");
    if (ev.type === "chat.message") {
      expect(ev.payload).toEqual(
        expect.objectContaining({ id: "msg-1", body: "hello" }),
      );
    }
  });

  it("parses feed.state_change with status", () => {
    const ev = ws.parse({ type: "feed.state_change", status: "dispensing" });
    expect(ev.type).toBe("feed.state_change");
    if (ev.type === "feed.state_change") {
      expect(ev.payload).toEqual(expect.objectContaining({ status: "dispensing" }));
    }
  });

  it("parses room-prefixed chat.message the same as direct", () => {
    const ev = ws.parse({ type: "room.chat.message", id: "x", body: "hi" });
    expect(ev.type).toBe("chat.message");
  });

  it("parses chat.moderation with message_id and action", () => {
    const ev = ws.parse({ type: "chat.moderation", message_id: "m1", action: "hide" });
    expect(ev.type).toBe("chat.moderation");
    if (ev.type === "chat.moderation") {
      expect(ev.message_id).toBe("m1");
      expect(ev.action).toBe("hide");
    }
  });

  it("returns unknown for unrecognized types", () => {
    const ev = ws.parse({ type: "something.else", data: 1 });
    expect(ev.type).toBe("unknown");
  });
});
