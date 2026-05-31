import { describe, expect, it } from "vitest";
import { pickProtocol } from "./playback";

describe("pickProtocol", () => {
  it("falls back to hls when no url_whep", () => {
    expect(
      pickProtocol(
        {
          room_id: "r",
          token: "tk",
          url_playback: "https://x/a.m3u8",
          webrtc_enabled: true,
        },
        100,
      ),
    ).toBe("hls");
  });
  it("returns whep when webrtc_enabled + url_whep + percent=100", () => {
    expect(
      pickProtocol(
        {
          room_id: "r",
          token: "tk",
          url_playback: "https://x/a.m3u8",
          url_whep: "https://x/whep",
          webrtc_enabled: true,
        },
        100,
      ),
    ).toBe("whep");
  });
  it("falls back when webrtc_enabled = false", () => {
    expect(
      pickProtocol(
        {
          room_id: "r",
          token: "tk",
          url_playback: "https://x/a.m3u8",
          url_whep: "https://x/whep",
          webrtc_enabled: false,
        },
        100,
      ),
    ).toBe("hls");
  });
});
