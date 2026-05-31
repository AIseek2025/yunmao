//! FLV header / tag 数据结构（详见 Adobe FLV 1.0 规范）。
//!
//! 在 yunmao 的链路中：
//!
//! - ingest 收到的 RTMP 媒体数据会被打包成 FLV tag，再投递到 media-edge。
//! - media-edge 的 HTTP-FLV 出口会写出标准 FLV 流（header + 0 PrevTagSize + 每个 tag 的 11 字节 header + body + PrevTagSize）。

use bytes::{BufMut, Bytes, BytesMut};

/// 标准 FLV 头：`FLV` magic + 版本 + 标志位 + 9（header 长度）。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct FlvHeader {
    pub has_audio: bool,
    pub has_video: bool,
}

impl FlvHeader {
    pub const SIZE: usize = 9;

    pub fn encode(self) -> [u8; 13] {
        let mut buf = [0u8; 13];
        buf[0] = b'F';
        buf[1] = b'L';
        buf[2] = b'V';
        buf[3] = 1;
        let mut flags = 0u8;
        if self.has_audio {
            flags |= 0b0000_0100;
        }
        if self.has_video {
            flags |= 0b0000_0001;
        }
        buf[4] = flags;
        buf[5..9].copy_from_slice(&9u32.to_be_bytes());
        // PrevTagSize0 = 0 (4 字节)
        buf[9..13].copy_from_slice(&0u32.to_be_bytes());
        buf
    }
}

/// FLV tag 类型。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TagKind {
    Audio = 8,
    Video = 9,
    ScriptData = 18,
}

impl TagKind {
    pub fn from_u8(b: u8) -> Option<Self> {
        match b {
            8 => Some(Self::Audio),
            9 => Some(Self::Video),
            18 => Some(Self::ScriptData),
            _ => None,
        }
    }
}

/// 解码后的 FLV tag。
#[derive(Debug, Clone)]
pub struct FlvTag {
    pub kind: TagKind,
    /// FLV 中是 24 位时间戳 + 8 位扩展，这里直接用 u32 ms。
    pub timestamp_ms: u32,
    pub data: Bytes,
}

impl FlvTag {
    /// 编码为符合 FLV 文件格式的 `tag header + body + PrevTagSize` 字节。
    pub fn encode(&self) -> Bytes {
        let body_len = self.data.len();
        let total = 11 + body_len + 4;
        let mut buf = BytesMut::with_capacity(total);
        buf.put_u8(self.kind as u8);
        buf.put_u8(((body_len >> 16) & 0xff) as u8);
        buf.put_u8(((body_len >> 8) & 0xff) as u8);
        buf.put_u8((body_len & 0xff) as u8);
        let ts = self.timestamp_ms;
        buf.put_u8(((ts >> 16) & 0xff) as u8);
        buf.put_u8(((ts >> 8) & 0xff) as u8);
        buf.put_u8((ts & 0xff) as u8);
        buf.put_u8(((ts >> 24) & 0xff) as u8); // ext
        buf.put_u8(0); // StreamID 占位（始终 0）
        buf.put_u8(0);
        buf.put_u8(0);
        buf.extend_from_slice(&self.data);
        buf.put_u32(11 + body_len as u32);
        buf.freeze()
    }

    /// 用于日志：判断 video tag 是否为关键帧（FLV video 第一字节高 4 位 == 1）。
    pub fn is_video_keyframe(&self) -> bool {
        if !matches!(self.kind, TagKind::Video) {
            return false;
        }
        self.data.first().is_some_and(|b| (b >> 4) == 1)
    }

    /// 是否是 AVC sequence header（音视频解码器初始化包）。
    pub fn is_avc_sequence_header(&self) -> bool {
        if !matches!(self.kind, TagKind::Video) {
            return false;
        }
        self.data.len() >= 2 && self.data[0] == 0x17 && self.data[1] == 0x00
    }

    /// 是否是 AAC sequence header（AudioSpecificConfig 初始化包）。
    /// FLV audio tag body：
    /// - byte0 = soundFormat(4) | soundRate(2) | soundSize(1) | soundType(1)
    ///   soundFormat=10 => AAC，所以 byte0 高 4 位 == 10。
    /// - byte1 = AAC packet type，0 = AAC sequence header，1 = AAC raw。
    pub fn is_aac_sequence_header(&self) -> bool {
        if !matches!(self.kind, TagKind::Audio) {
            return false;
        }
        self.data.len() >= 2 && (self.data[0] >> 4) == 10 && self.data[1] == 0x00
    }

    /// AAC raw 数据（非 sequence header）。
    pub fn is_aac_raw(&self) -> bool {
        if !matches!(self.kind, TagKind::Audio) {
            return false;
        }
        self.data.len() >= 2 && (self.data[0] >> 4) == 10 && self.data[1] == 0x01
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn header_encodes_flags() {
        let bytes = FlvHeader {
            has_audio: true,
            has_video: true,
        }
        .encode();
        assert_eq!(&bytes[0..3], b"FLV");
        assert_eq!(bytes[3], 1);
        assert_eq!(bytes[4], 0b0000_0101);
        assert_eq!(u32::from_be_bytes(bytes[5..9].try_into().unwrap()), 9);
        assert_eq!(u32::from_be_bytes(bytes[9..13].try_into().unwrap()), 0);
    }

    #[test]
    fn tag_encodes_with_prev_size() {
        let tag = FlvTag {
            kind: TagKind::Video,
            timestamp_ms: 0,
            data: Bytes::from_static(&[0x17, 0x00, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03]),
        };
        let encoded = tag.encode();
        // 11 header + 8 body + 4 prev = 23
        assert_eq!(encoded.len(), 23);
        assert_eq!(encoded[0], 9);
        // body length 8
        assert_eq!(encoded[1..4], [0, 0, 8]);
        // PrevTagSize at end = 11+8 = 19
        let prev = u32::from_be_bytes(encoded[19..23].try_into().unwrap());
        assert_eq!(prev, 19);
    }

    #[test]
    fn keyframe_detection() {
        let kf = FlvTag {
            kind: TagKind::Video,
            timestamp_ms: 0,
            data: Bytes::from_static(&[0x17, 0x01, 0, 0, 0, 0, 0]),
        };
        assert!(kf.is_video_keyframe());
        assert!(!kf.is_avc_sequence_header());

        let seq = FlvTag {
            kind: TagKind::Video,
            timestamp_ms: 0,
            data: Bytes::from_static(&[0x17, 0x00, 0, 0, 0]),
        };
        assert!(seq.is_avc_sequence_header());
    }
}
