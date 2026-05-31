// 协议选择：默认 LL-HLS（hls.js），灰度命中走 WHEP（WebRTC API）。

import type { IceServer, RoomSubscription } from "./types";
import { inGrayPercent } from "./gray";

export type Protocol = "hls" | "whep";

export function pickProtocol(sub: RoomSubscription, grayPercent: number): Protocol {
  if (!sub.url_whep) return "hls";
  if (sub.webrtc_enabled === false) return "hls";
  if (inGrayPercent(sub.room_id, grayPercent)) return "whep";
  return "hls";
}

export async function attachWhep(
  video: HTMLVideoElement,
  whepUrl: string,
  token: string,
  iceServers: IceServer[],
): Promise<RTCPeerConnection> {
  const pc = new RTCPeerConnection({
    iceServers: iceServers.map((s) => ({
      urls: s.urls,
      username: s.username,
      credential: s.credential,
    })),
  });
  pc.addTransceiver("video", { direction: "recvonly" });
  pc.addTransceiver("audio", { direction: "recvonly" });
  pc.ontrack = (e) => {
    if (e.track.kind === "video" && e.streams[0]) {
      video.srcObject = e.streams[0];
    }
  };
  const offer = await pc.createOffer();
  await pc.setLocalDescription(offer);
  const resp = await fetch(whepUrl, {
    method: "POST",
    headers: {
      "Content-Type": "application/sdp",
      Authorization: `Bearer ${token}`,
    },
    body: offer.sdp ?? "",
  });
  if (!resp.ok) {
    pc.close();
    throw new Error(`WHEP ${resp.status}`);
  }
  await pc.setRemoteDescription({ type: "answer", sdp: await resp.text() });
  return pc;
}
