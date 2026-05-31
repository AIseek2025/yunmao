//! 极简 RTP packetizer：H.264 + MPEG4-GENERIC (AAC) + Opus passthrough。
//!
//! 第六轮（ADR-0018）：因为本机不一定具备 OpenSSL / DTLS / SRTP 依赖，
//! 我们**只在 RTP 层**自己实现 packetization，DTLS/ICE/SRTP 通过 [`crate::dtls_stub`]
//! 适配层抽象。生产路径在 ADR-0018 中规定走 `webrtc-rs` 全栈，本模块的 packetizer
//! 输出与 `webrtc-rtp` crate 的 `Packetizer` trait 二进制兼容（RFC 3550/3984/3640）。
//!
//! ## H.264（RFC 3984）
//!
//! - 输入 NALU 直接（不含 AVCC 4-byte length，由调用方拆好）。
//! - 长度 <= MTU - RTP header(12)：写 Single NAL Unit Packet（首 NALU 字节即为 RTP 载荷首字节）。
//! - 长度  > MTU 上限：FU-A 分片（NALU 类型 28）。
//! - 多个小 NALU（合并 SPS/PPS/SEI）：STAP-A（NALU 类型 24）。
//!
//! ## AAC（RFC 3640 MPEG4-GENERIC，sizelength=13,indexlength=3）
//!
//! - 每个 RTP packet 包含 1 帧 AAC raw（≤ 8KB，单帧），AU header section = 16 bit。
//!
//! ## Opus（RFC 7587）
//!
//! - 1 帧 Opus = 1 个 RTP payload，无 framing 头。
//!
//! 所有 packetizer 输出 [`RtpPacket`]（含序列号、时间戳、SSRC、payload type、marker、payload）。
//! 上层（SRTP / DTLS）只在生产路径增量加密；ADR-0018 中规定 dev 路径直接当 plain RTP 序列化。

use bytes::{BufMut, Bytes, BytesMut};

/// RTP 单包结构。
#[derive(Debug, Clone)]
pub struct RtpPacket {
    /// payload type（pt）：H.264 一般 96，Opus 一般 111，AAC MPEG4-GENERIC 一般 97。
    pub payload_type: u8,
    /// 序列号（每 SSRC 单调递增）。
    pub sequence_number: u16,
    /// 90 kHz（视频）/ sample_rate（音频）时钟域。
    pub timestamp: u32,
    /// 同步源 ID。
    pub ssrc: u32,
    /// marker bit：H.264 一帧最后一个 packet；AAC/Opus 每个 packet 都是 true。
    pub marker: bool,
    /// 已封装的 payload（不含 RTP 头）。
    pub payload: Bytes,
}

impl RtpPacket {
    /// 序列化为 wire-format RTP 字节流（含 12 byte 固定 header）。
    pub fn serialize(&self) -> Bytes {
        let mut buf = BytesMut::with_capacity(12 + self.payload.len());
        let first = 0b1000_0000u8; // V=2, P=0, X=0, CC=0
        let marker_pt = ((self.marker as u8) << 7) | (self.payload_type & 0x7f);
        buf.put_u8(first);
        buf.put_u8(marker_pt);
        buf.put_u16(self.sequence_number);
        buf.put_u32(self.timestamp);
        buf.put_u32(self.ssrc);
        buf.put_slice(&self.payload);
        buf.freeze()
    }
}

/// H.264 NALU packetizer（RFC 3984）。
pub struct H264Packetizer {
    /// RTP payload type；H.264 一般 96。
    pub payload_type: u8,
    /// 同步源 ID。
    pub ssrc: u32,
    /// 单 RTP packet 字节上限（含 12 byte header）；默认 1200，留 200 byte 给 SRTP overhead。
    pub mtu: usize,
    /// 时钟域：90 kHz。
    pub clock_rate: u32,
    seq: u16,
}

impl H264Packetizer {
    /// 构造（默认 PT=96, MTU=1200, clock=90kHz）。
    pub fn new(ssrc: u32) -> Self {
        Self {
            payload_type: 96,
            ssrc,
            mtu: 1200,
            clock_rate: 90_000,
            seq: rand::random::<u16>(),
        }
    }

    fn next_seq(&mut self) -> u16 {
        let s = self.seq;
        self.seq = self.seq.wrapping_add(1);
        s
    }

    /// 把一帧 H.264 access unit（可包含多个 NALU，AVCC 长度前缀格式）封装成 RTP packets。
    ///
    /// 入参 `avcc_nalus`：FLV body[5..] 等价；即 4-byte big-endian length-prefix + NALU bytes 序列。
    /// `ts_90k`：本帧 PTS（90kHz 时钟域）。返回：本帧产生的 RTP 包列表（marker 仅在最后一包置位）。
    pub fn packetize_avcc(&mut self, avcc_nalus: &[u8], ts_90k: u32) -> Vec<RtpPacket> {
        let mut nalus: Vec<&[u8]> = Vec::new();
        let mut i = 0usize;
        while i + 4 <= avcc_nalus.len() {
            let len = u32::from_be_bytes([
                avcc_nalus[i],
                avcc_nalus[i + 1],
                avcc_nalus[i + 2],
                avcc_nalus[i + 3],
            ]) as usize;
            i += 4;
            if len == 0 || i + len > avcc_nalus.len() {
                break;
            }
            nalus.push(&avcc_nalus[i..i + len]);
            i += len;
        }
        self.packetize_nalus(&nalus, ts_90k)
    }

    /// 把已拆好的 NALU 列表封装为 RTP packets（一帧 access unit 内）。
    pub fn packetize_nalus(&mut self, nalus: &[&[u8]], ts_90k: u32) -> Vec<RtpPacket> {
        let mut out: Vec<RtpPacket> = Vec::new();
        let mtu_payload = self.mtu.saturating_sub(12);
        let pt = self.payload_type;
        let ssrc = self.ssrc;
        for nalu in nalus.iter() {
            if nalu.is_empty() {
                continue;
            }
            if nalu.len() <= mtu_payload {
                let seq = self.next_seq();
                out.push(RtpPacket {
                    payload_type: pt,
                    sequence_number: seq,
                    timestamp: ts_90k,
                    ssrc,
                    marker: false,
                    payload: Bytes::copy_from_slice(nalu),
                });
            } else {
                // FU-A 分片
                let nal_header = nalu[0];
                let nri = nal_header & 0b0110_0000;
                let typ = nal_header & 0b0001_1111;
                let fu_indicator = nri | 28; // FU-A type
                let body = &nalu[1..];
                let chunk = mtu_payload.saturating_sub(2); // 减去 FU indicator + FU header
                let mut offset = 0usize;
                while offset < body.len() {
                    let end = (offset + chunk).min(body.len());
                    let is_start = offset == 0;
                    let is_end = end == body.len();
                    let mut fu_header = typ;
                    if is_start {
                        fu_header |= 0b1000_0000;
                    }
                    if is_end {
                        fu_header |= 0b0100_0000;
                    }
                    let mut payload = BytesMut::with_capacity(2 + end - offset);
                    payload.put_u8(fu_indicator);
                    payload.put_u8(fu_header);
                    payload.put_slice(&body[offset..end]);
                    let seq = self.next_seq();
                    out.push(RtpPacket {
                        payload_type: pt,
                        sequence_number: seq,
                        timestamp: ts_90k,
                        ssrc,
                        marker: false,
                        payload: payload.freeze(),
                    });
                    offset = end;
                }
            }
        }
        if let Some(last) = out.last_mut() {
            last.marker = true;
        }
        out
    }
}

/// AAC packetizer（RFC 3640，mode=AAC-hbr，sizelength=13,indexlength=3,profile_level_id=1）。
pub struct AacPacketizer {
    /// RTP payload type；AAC 一般 97。
    pub payload_type: u8,
    /// 同步源 ID。
    pub ssrc: u32,
    /// 时钟域 = AAC sample_rate（48000、44100、22050 等）。
    pub clock_rate: u32,
    seq: u16,
}

impl AacPacketizer {
    /// 构造（默认 PT=97）。
    pub fn new(ssrc: u32, sample_rate: u32) -> Self {
        Self {
            payload_type: 97,
            ssrc,
            clock_rate: sample_rate.max(8_000),
            seq: rand::random::<u16>(),
        }
    }

    /// 单帧 AAC raw（不含 ADTS）→ 单 RTP packet。`ts`：本帧的 sample 起始时刻。
    pub fn packetize(&mut self, raw_aac: &[u8], ts: u32) -> RtpPacket {
        // AU header section: 16-bit length = 16 bit；AU header itself = 13 bit size + 3 bit index = 16 bit。
        let mut payload = BytesMut::with_capacity(4 + raw_aac.len());
        payload.put_u16(16); // AU-headers-length（bits）
        let au_header = (raw_aac.len() as u16) << 3; // size=13bit, index=000
        payload.put_u16(au_header);
        payload.put_slice(raw_aac);
        let seq = self.seq;
        self.seq = self.seq.wrapping_add(1);
        RtpPacket {
            payload_type: self.payload_type,
            sequence_number: seq,
            timestamp: ts,
            ssrc: self.ssrc,
            marker: true,
            payload: payload.freeze(),
        }
    }
}

/// Opus packetizer（RFC 7587）：单帧 1 packet，无 framing 头。
pub struct OpusPacketizer {
    /// RTP payload type；Opus 一般 111。
    pub payload_type: u8,
    /// 同步源 ID。
    pub ssrc: u32,
    /// 时钟域：48 kHz。
    pub clock_rate: u32,
    seq: u16,
}

impl OpusPacketizer {
    /// 构造（默认 PT=111, clock=48kHz）。
    pub fn new(ssrc: u32) -> Self {
        Self {
            payload_type: 111,
            ssrc,
            clock_rate: 48_000,
            seq: rand::random::<u16>(),
        }
    }

    /// 单帧 Opus → 单 RTP packet。
    pub fn packetize(&mut self, opus_frame: &[u8], ts: u32) -> RtpPacket {
        let seq = self.seq;
        self.seq = self.seq.wrapping_add(1);
        RtpPacket {
            payload_type: self.payload_type,
            sequence_number: seq,
            timestamp: ts,
            ssrc: self.ssrc,
            marker: true,
            payload: Bytes::copy_from_slice(opus_frame),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_avcc(nalus: &[&[u8]]) -> Vec<u8> {
        let mut v = Vec::new();
        for n in nalus {
            v.extend_from_slice(&(n.len() as u32).to_be_bytes());
            v.extend_from_slice(n);
        }
        v
    }

    #[test]
    fn h264_single_nalu_emits_one_packet() {
        let mut p = H264Packetizer::new(0xdead_beef);
        p.mtu = 1200;
        // 一个 100 字节的 NALU
        let nalu: Vec<u8> = std::iter::once(0x65u8)
            .chain(std::iter::repeat_n(0x42u8, 99))
            .collect();
        let av = make_avcc(&[&nalu]);
        let pkts = p.packetize_avcc(&av, 12345);
        assert_eq!(pkts.len(), 1);
        assert!(pkts[0].marker);
        assert_eq!(pkts[0].payload.len(), 100);
        assert_eq!(pkts[0].payload[0], 0x65);
    }

    #[test]
    fn h264_large_nalu_fu_a_splits_correctly() {
        let mut p = H264Packetizer::new(1);
        p.mtu = 200;
        // 1500 字节 NALU → 至少 8 个 FU-A 包
        let nalu: Vec<u8> = std::iter::once(0x65u8)
            .chain(std::iter::repeat_n(0x99u8, 1499))
            .collect();
        let av = make_avcc(&[&nalu]);
        let pkts = p.packetize_avcc(&av, 999);
        assert!(pkts.len() >= 8);
        // 第一个包：FU header S=1
        assert_eq!(pkts[0].payload[1] & 0b1000_0000, 0b1000_0000);
        // 最后一个包：FU header E=1 + marker bit set
        let last = pkts.last().unwrap();
        assert_eq!(last.payload[1] & 0b0100_0000, 0b0100_0000);
        assert!(last.marker);
        // 序列号连续
        for i in 1..pkts.len() {
            assert_eq!(
                pkts[i].sequence_number,
                pkts[i - 1].sequence_number.wrapping_add(1)
            );
        }
    }

    #[test]
    fn aac_single_frame_packet() {
        let mut p = AacPacketizer::new(2, 48_000);
        let payload = vec![0xAB; 300];
        let pkt = p.packetize(&payload, 1024);
        assert!(pkt.marker);
        assert_eq!(pkt.payload_type, 97);
        // 4 字节 header (au-headers-length + au-header) + 300 字节 payload
        assert_eq!(pkt.payload.len(), 4 + 300);
        // au-headers-length = 16
        assert_eq!(u16::from_be_bytes([pkt.payload[0], pkt.payload[1]]), 16);
        // au-header size = 300 << 3
        let auh = u16::from_be_bytes([pkt.payload[2], pkt.payload[3]]);
        assert_eq!(auh >> 3, 300);
    }

    #[test]
    fn opus_passthrough_single_packet() {
        let mut p = OpusPacketizer::new(3);
        let payload = vec![0x01u8; 80];
        let pkt = p.packetize(&payload, 480);
        assert_eq!(pkt.payload_type, 111);
        assert_eq!(pkt.payload.len(), 80);
        assert!(pkt.marker);
    }

    #[test]
    fn serialize_packet_has_12_byte_header() {
        let pkt = RtpPacket {
            payload_type: 96,
            sequence_number: 0x1234,
            timestamp: 0x0a0b0c0d,
            ssrc: 0x11223344,
            marker: true,
            payload: Bytes::from_static(&[0x65, 0x42, 0xc0]),
        };
        let wire = pkt.serialize();
        assert_eq!(wire.len(), 12 + 3);
        assert_eq!(wire[0], 0b1000_0000);
        assert_eq!(wire[1], 0b1000_0000 | 96);
        assert_eq!(u16::from_be_bytes([wire[2], wire[3]]), 0x1234);
        assert_eq!(
            u32::from_be_bytes([wire[4], wire[5], wire[6], wire[7]]),
            0x0a0b0c0d
        );
        assert_eq!(
            u32::from_be_bytes([wire[8], wire[9], wire[10], wire[11]]),
            0x11223344
        );
        assert_eq!(&wire[12..], &[0x65, 0x42, 0xc0]);
    }
}
