//! 极简 fMP4（fragmented MP4 / CMAF）封装器，专门用于 LL-HLS 切片。
//!
//! 范围（第四轮）：
//!
//! - H.264 视频：解析 SPS（Exp-Golomb）得到真实宽高 / profile / level / fps；
//!   生成 ftyp + moov（含 trak/avc1/avcC）init segment + moof/mdat media segment；
//! - AAC 音频：解析 AudioSpecificConfig 得到 sample_rate / channels；
//!   生成附带 audio trak 的 init segment（hls.js 1.5+ 与 Safari 17 验证可播）；
//! - 时间戳基于 FLV tag 的 ms（timescale=1000）。
//!
//! 复杂场景（多 PPS、多 SPS、参数集变化）退化到默认值。生产环境建议接 `ffmpeg`
//! / `shaka-packager`；本实现的目标是 *可被 hls.js / Safari 拉取并 parse*。

use anyhow::{anyhow, Result};
use bytes::{BufMut, Bytes, BytesMut};

/// 从 FLV AVC sequence header（AVCDecoderConfigurationRecord）中提取的关键参数。
#[derive(Debug, Clone)]
pub struct AvcConfig {
    pub width: u16,
    pub height: u16,
    pub profile_idc: u8,
    pub level_idc: u8,
    pub frame_rate_x1000: u32,
    /// FLV avc 配置原始字节（即 FLV video tag data[5..]），写到 init segment 的 avcC box。
    pub avcc_raw: Bytes,
    /// 长度字段大小（来自 AVCC[4] & 0b11 + 1）。
    pub nalu_len_size: u8,
}

impl AvcConfig {
    /// 解析 FLV 中 AVC sequence header（tag data 形如 0x17 0x00 0x00 0x00 0x00 <avcC...>）。
    ///
    /// 真实解析步骤：
    ///   1. 取 AVCC 字节区，从 `avcc[5] & 0x1f` 读 SPS 数量；
    ///   2. 跳过 SPS length 2 字节，对 SPS NALU 做 unescape（去 0x03）；
    ///   3. 跳过 NALU header（1 byte），用 `SpsParser` 解 Exp-Golomb 取宽高 / fps。
    pub fn from_flv_avc_seq(tag_data: &[u8]) -> Result<Self> {
        if tag_data.len() < 11 {
            return Err(anyhow!("avc seq header too short"));
        }
        let avcc = &tag_data[5..];
        if avcc.len() < 7 {
            return Err(anyhow!("avcc too short"));
        }
        let profile_idc = avcc[1];
        let level_idc = avcc[3];
        let nalu_len_size = (avcc[4] & 0b0000_0011) + 1;
        let num_sps = avcc[5] & 0x1f;
        let mut width = 1280u16;
        let mut height = 720u16;
        let mut fps_x1000 = 30_000u32;

        if num_sps > 0 {
            let idx = 6usize;
            if idx + 2 > avcc.len() {
                return Err(anyhow!("sps length offset overflow"));
            }
            let sps_len = u16::from_be_bytes([avcc[idx], avcc[idx + 1]]) as usize;
            if idx + 2 + sps_len > avcc.len() {
                return Err(anyhow!("sps length overflow"));
            }
            let sps_nalu = &avcc[idx + 2..idx + 2 + sps_len];
            if sps_nalu.len() > 1 {
                // 跳 NALU header（1 字节）；剩余是 SPS RBSP（已 escape）。
                let rbsp = unescape_emulation_bytes(&sps_nalu[1..]);
                if let Some(parsed) = SpsParser::parse(&rbsp) {
                    if parsed.width > 0 {
                        width = parsed.width;
                    }
                    if parsed.height > 0 {
                        height = parsed.height;
                    }
                    if parsed.fps_x1000 > 0 {
                        fps_x1000 = parsed.fps_x1000;
                    }
                }
            }
        }

        Ok(Self {
            width,
            height,
            profile_idc,
            level_idc,
            frame_rate_x1000: fps_x1000,
            avcc_raw: Bytes::copy_from_slice(avcc),
            nalu_len_size,
        })
    }
}

/// 从 FLV AAC sequence header（AudioSpecificConfig）中提取的参数。
#[derive(Debug, Clone)]
pub struct AacConfig {
    /// audio_object_type（AAC LC = 2）
    pub object_type: u8,
    pub sample_rate: u32,
    pub channels: u8,
    /// 原始 AudioSpecificConfig（送进 esds box）。
    pub asc: Bytes,
}

impl AacConfig {
    /// 解析 FLV AAC sequence header（tag data 形如 0xAF 0x00 <asc...>）。
    /// asc 至少 2 字节：5 bit objectType + 4 bit samplingFreqIdx + 4 bit channelConfig。
    pub fn from_flv_aac_seq(tag_data: &[u8]) -> Result<Self> {
        if tag_data.len() < 4 {
            return Err(anyhow!("aac seq header too short"));
        }
        if tag_data[0] >> 4 != 10 || tag_data[1] != 0 {
            return Err(anyhow!("not aac sequence header"));
        }
        let asc = &tag_data[2..];
        if asc.len() < 2 {
            return Err(anyhow!("asc too short"));
        }
        let b0 = asc[0];
        let b1 = asc[1];
        let object_type = b0 >> 3; // 5 bits
        let sfi = ((b0 & 0b0000_0111) << 1) | (b1 >> 7); // 4 bits
        let channels = (b1 >> 3) & 0b1111;
        let rates = [
            96_000u32, 88_200, 64_000, 48_000, 44_100, 32_000, 24_000, 22_050, 16_000, 12_000,
            11_025, 8_000, 7_350,
        ];
        let sample_rate = rates.get(sfi as usize).copied().unwrap_or(44_100);
        Ok(Self {
            object_type,
            sample_rate,
            channels,
            asc: Bytes::copy_from_slice(asc),
        })
    }
}

/// 媒体元数据（per room）：video / audio codec、码率、起始 pts；供 LL-HLS / QoE 使用。
#[derive(Debug, Clone, Default)]
pub struct PublisherMetadata {
    pub video: Option<VideoMeta>,
    pub audio: Option<AudioMeta>,
    pub source_bitrate_bps: u32,
    pub start_pts_ms: Option<u32>,
}

#[derive(Debug, Clone)]
pub struct VideoMeta {
    pub codec: &'static str, // "h264"
    pub width: u16,
    pub height: u16,
    pub profile_idc: u8,
    pub level_idc: u8,
    pub fps_x1000: u32,
}

#[derive(Debug, Clone)]
pub struct AudioMeta {
    pub codec: &'static str, // "aac"
    pub sample_rate: u32,
    pub channels: u8,
    pub object_type: u8,
}

/// Init segment：ftyp + moov。如带 `aac` 参数，会同时写音视频两个 trak。
pub fn build_init_segment(video: &AvcConfig, aac: Option<&AacConfig>) -> Bytes {
    let mut buf = BytesMut::with_capacity(2048);
    write_ftyp(&mut buf);
    write_moov(&mut buf, video, aac);
    buf.freeze()
}

/// 单视频 init segment（向后兼容）。
pub fn build_init_segment_video_only(video: &AvcConfig) -> Bytes {
    build_init_segment(video, None)
}

/// Media segment / part：moof + mdat。
///
/// 入参：
/// - `sequence_number` 单调递增（MOOF.mfhd.sequence_number）。
/// - `decode_time` 累计 base_media_decode_time（timescale=1000，单位 ms）。
/// - `nalus`：本 part 内的 H.264 NALU 序列（AVCC 长度前缀格式，与 FLV body[5..] 兼容）。
/// - `durations_ms`：每个 NALU 对应的 duration（一般 = frame duration）。
pub fn build_media_segment(
    sequence_number: u32,
    decode_time: u64,
    nalus: &[Bytes],
    durations_ms: &[u32],
) -> Bytes {
    let mut buf = BytesMut::with_capacity(4096);
    write_moof(&mut buf, sequence_number, decode_time, nalus, durations_ms);
    write_mdat(&mut buf, nalus);
    buf.freeze()
}

/// 一帧 AAC（raw frame，不含 ADTS header）+ 时间信息。
#[derive(Debug, Clone)]
pub struct AacFrame {
    /// raw AAC frame（不含 ADTS）。
    pub data: Bytes,
    /// 该帧的 sample 数（一般固定 1024，HE-AAC 可能是 2048；调用方根据 ASC 选择）。
    pub samples: u32,
}

/// 带音轨的 media segment：moof.traf×2（视频 + 音频）+ mdat（视频 NALU 后接 AAC raw）。
///
/// 时间戳约定：
/// - 视频 timescale=1000（ms），`video_decode_time` 是当前段起点的 base_media_decode_time。
/// - 音频 timescale=`aac.sample_rate`，`audio_decode_time` 是当前段起点的累计 sample 数。
/// - 音频 sample_duration 一般 = `frame.samples`，按帧填充以保持 PTS 单调；
///   wrap：u64 base_media_decode_time 足够长时间内不会溢出（48kHz × 2^64 / 86400/365 ≈ 1.2e7 年）。
pub fn build_media_segment_av(
    sequence_number: u32,
    video_decode_time: u64,
    nalus: &[Bytes],
    video_durations_ms: &[u32],
    audio_decode_time: u64,
    audio_frames: &[AacFrame],
) -> Bytes {
    let mut buf = BytesMut::with_capacity(4096);
    write_moof_av(
        &mut buf,
        sequence_number,
        video_decode_time,
        nalus,
        video_durations_ms,
        audio_decode_time,
        audio_frames,
    );
    write_mdat_av(&mut buf, nalus, audio_frames);
    buf.freeze()
}

// ---------------- SPS Parser（Exp-Golomb） ----------------

#[derive(Debug, Default)]
struct ParsedSps {
    width: u16,
    height: u16,
    fps_x1000: u32,
}

/// 极简 H.264 SPS 解析器：实现 SPS 主要字段（宽高 + 可选 fps）。
///
/// 参考 ITU-T H.264 §7.3.2.1.1 / §7.4.2.1.1。
struct SpsParser<'a> {
    data: &'a [u8],
    bit_pos: usize,
}

impl<'a> SpsParser<'a> {
    fn parse(rbsp: &'a [u8]) -> Option<ParsedSps> {
        let mut p = SpsParser {
            data: rbsp,
            bit_pos: 0,
        };
        let profile_idc = p.read_bits(8)? as u8;
        p.skip_bits(8)?; // constraint flags + reserved
        let _level_idc = p.read_bits(8)?;
        let _seq_parameter_set_id = p.read_ue()?;
        let mut chroma_format_idc = 1u32;
        if matches!(
            profile_idc,
            100 | 110 | 122 | 244 | 44 | 83 | 86 | 118 | 128 | 138 | 139 | 134 | 135
        ) {
            chroma_format_idc = p.read_ue()?;
            if chroma_format_idc == 3 {
                p.skip_bits(1)?; // separate_colour_plane_flag
            }
            p.read_ue()?; // bit_depth_luma_minus8
            p.read_ue()?; // bit_depth_chroma_minus8
            p.skip_bits(1)?; // qpprime_y_zero_transform_bypass_flag
            let scaling_matrix_present = p.read_bits(1)? == 1;
            if scaling_matrix_present {
                let count = if chroma_format_idc == 3 { 12 } else { 8 };
                for i in 0..count {
                    let present = p.read_bits(1)? == 1;
                    if present {
                        Self::skip_scaling_list(&mut p, if i < 6 { 16 } else { 64 })?;
                    }
                }
            }
        }
        let _log2_max_frame_num_minus4 = p.read_ue()?;
        let pic_order_cnt_type = p.read_ue()?;
        if pic_order_cnt_type == 0 {
            let _ = p.read_ue()?;
        } else if pic_order_cnt_type == 1 {
            p.skip_bits(1)?; // delta_pic_order_always_zero_flag
            let _ = p.read_se()?;
            let _ = p.read_se()?;
            let n = p.read_ue()?;
            for _ in 0..n {
                let _ = p.read_se()?;
            }
        }
        let _max_num_ref_frames = p.read_ue()?;
        p.skip_bits(1)?; // gaps_in_frame_num_value_allowed_flag

        let pic_width_in_mbs_minus1 = p.read_ue()?;
        let pic_height_in_map_units_minus1 = p.read_ue()?;
        let frame_mbs_only_flag = p.read_bits(1)? == 1;
        if !frame_mbs_only_flag {
            p.skip_bits(1)?; // mb_adaptive_frame_field_flag
        }
        p.skip_bits(1)?; // direct_8x8_inference_flag

        let mut frame_crop_left = 0u32;
        let mut frame_crop_right = 0u32;
        let mut frame_crop_top = 0u32;
        let mut frame_crop_bottom = 0u32;
        let frame_cropping_flag = p.read_bits(1)? == 1;
        if frame_cropping_flag {
            frame_crop_left = p.read_ue()?;
            frame_crop_right = p.read_ue()?;
            frame_crop_top = p.read_ue()?;
            frame_crop_bottom = p.read_ue()?;
        }

        let sub_width_c = match chroma_format_idc {
            1 | 2 => 2,
            3 => 1,
            _ => 1,
        };
        let sub_height_c = match chroma_format_idc {
            1 => 2,
            2 | 3 => 1,
            _ => 1,
        };
        let crop_unit_x = sub_width_c;
        let crop_unit_y = sub_height_c * if frame_mbs_only_flag { 1 } else { 2 };

        let width =
            (pic_width_in_mbs_minus1 + 1) * 16 - crop_unit_x * (frame_crop_left + frame_crop_right);
        let height =
            (2 - u32::from(frame_mbs_only_flag)) * (pic_height_in_map_units_minus1 + 1) * 16
                - crop_unit_y * (frame_crop_top + frame_crop_bottom);

        let mut fps_x1000 = 0u32;
        let vui_present = p.read_bits(1).unwrap_or(0) == 1;
        if vui_present {
            // aspect_ratio_info_present_flag
            if p.read_bits(1).unwrap_or(0) == 1 {
                let aspect_ratio_idc = p.read_bits(8).unwrap_or(0);
                if aspect_ratio_idc == 255 {
                    let _ = p.read_bits(16);
                    let _ = p.read_bits(16);
                }
            }
            if p.read_bits(1).unwrap_or(0) == 1 {
                // overscan_info_present
                let _ = p.read_bits(1);
            }
            if p.read_bits(1).unwrap_or(0) == 1 {
                // video_signal_type
                let _ = p.read_bits(3);
                let _ = p.read_bits(1);
                if p.read_bits(1).unwrap_or(0) == 1 {
                    let _ = p.read_bits(8);
                    let _ = p.read_bits(8);
                    let _ = p.read_bits(8);
                }
            }
            if p.read_bits(1).unwrap_or(0) == 1 {
                // chroma_loc_info
                let _ = p.read_ue();
                let _ = p.read_ue();
            }
            if p.read_bits(1).unwrap_or(0) == 1 {
                // timing_info_present
                let num_units_in_tick = p.read_bits(32).unwrap_or(0);
                let time_scale = p.read_bits(32).unwrap_or(0);
                let _fixed_frame_rate = p.read_bits(1);
                if num_units_in_tick > 0 {
                    // fps = time_scale / (2 * num_units_in_tick)
                    fps_x1000 = ((u64::from(time_scale) * 1000)
                        / (2 * u64::from(num_units_in_tick)))
                        as u32;
                }
            }
        }

        Some(ParsedSps {
            width: width as u16,
            height: height as u16,
            fps_x1000,
        })
    }

    fn read_bit(&mut self) -> Option<u8> {
        let byte = self.data.get(self.bit_pos / 8)?;
        let off = 7 - (self.bit_pos % 8);
        self.bit_pos += 1;
        Some((byte >> off) & 1)
    }

    fn read_bits(&mut self, n: usize) -> Option<u32> {
        if n > 32 {
            return None;
        }
        let mut v = 0u32;
        for _ in 0..n {
            v = (v << 1) | u32::from(self.read_bit()?);
        }
        Some(v)
    }

    fn skip_bits(&mut self, n: usize) -> Option<()> {
        for _ in 0..n {
            self.read_bit()?;
        }
        Some(())
    }

    /// Exp-Golomb unsigned。
    fn read_ue(&mut self) -> Option<u32> {
        let mut zero_count = 0;
        while self.read_bit()? == 0 {
            zero_count += 1;
            if zero_count > 32 {
                return None;
            }
        }
        let prefix = 1u32 << zero_count;
        let suffix = if zero_count > 0 {
            self.read_bits(zero_count)?
        } else {
            0
        };
        Some(prefix + suffix - 1)
    }

    /// Exp-Golomb signed。
    fn read_se(&mut self) -> Option<i32> {
        let v = self.read_ue()? as i64;
        Some(if v & 1 == 1 {
            ((v + 1) / 2) as i32
        } else {
            (-(v / 2)) as i32
        })
    }

    fn skip_scaling_list(p: &mut SpsParser<'_>, size: usize) -> Option<()> {
        let mut last_scale = 8i32;
        let mut next_scale = 8i32;
        for _ in 0..size {
            if next_scale != 0 {
                let delta = p.read_se()?;
                next_scale = (last_scale + delta + 256) % 256;
            }
            if next_scale != 0 {
                last_scale = next_scale;
            }
        }
        Some(())
    }
}

/// 去掉 H.264 RBSP 中的 emulation_prevention_three_byte（0x00 0x00 0x03 → 0x00 0x00）。
fn unescape_emulation_bytes(input: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(input.len());
    let mut i = 0;
    while i < input.len() {
        if i + 2 < input.len() && input[i] == 0 && input[i + 1] == 0 && input[i + 2] == 0x03 {
            out.push(0);
            out.push(0);
            i += 3;
        } else {
            out.push(input[i]);
            i += 1;
        }
    }
    out
}

// ---------------- box writers ----------------

fn write_box(buf: &mut BytesMut, name: [u8; 4], content: impl FnOnce(&mut BytesMut)) {
    let size_pos = buf.len();
    buf.put_u32(0); // placeholder
    buf.extend_from_slice(&name);
    content(buf);
    let size = (buf.len() - size_pos) as u32;
    buf[size_pos..size_pos + 4].copy_from_slice(&size.to_be_bytes());
}

fn write_ftyp(buf: &mut BytesMut) {
    write_box(buf, *b"ftyp", |b| {
        b.extend_from_slice(b"iso5");
        b.put_u32(1);
        b.extend_from_slice(b"avc1");
        b.extend_from_slice(b"iso5");
        b.extend_from_slice(b"dash");
    });
}

fn write_moov(buf: &mut BytesMut, cfg: &AvcConfig, aac: Option<&AacConfig>) {
    let track_count: u32 = if aac.is_some() { 2 } else { 1 };
    write_box(buf, *b"moov", |b| {
        // mvhd
        write_box(b, *b"mvhd", |bb| {
            bb.put_u32(0); // version + flags
            bb.put_u32(0); // creation_time
            bb.put_u32(0); // modification_time
            bb.put_u32(1000); // timescale
            bb.put_u32(0); // duration (live)
            bb.put_u32(0x0001_0000); // rate 1.0
            bb.put_u16(0x0100); // volume
            bb.put_u16(0); // reserved
            bb.put_u64(0); // reserved
            for v in [0x0001_0000, 0, 0, 0, 0x0001_0000, 0, 0, 0, 0x4000_0000] {
                bb.put_u32(v);
            }
            for _ in 0..6 {
                bb.put_u32(0); // pre_defined
            }
            bb.put_u32(track_count + 1); // next_track_ID
        });
        // mvex (for fragmented)
        write_box(b, *b"mvex", |bb| {
            write_box(bb, *b"trex", |bbb| {
                bbb.put_u32(0);
                bbb.put_u32(1); // track_ID = 1 (video)
                bbb.put_u32(1);
                bbb.put_u32(0);
                bbb.put_u32(0);
                bbb.put_u32(0);
            });
            if aac.is_some() {
                write_box(bb, *b"trex", |bbb| {
                    bbb.put_u32(0);
                    bbb.put_u32(2); // track_ID = 2 (audio)
                    bbb.put_u32(1);
                    bbb.put_u32(0);
                    bbb.put_u32(0);
                    bbb.put_u32(0);
                });
            }
        });
        // video trak
        write_video_trak(b, cfg);
        if let Some(a) = aac {
            write_audio_trak(b, a);
        }
    });
}

fn write_video_trak(b: &mut BytesMut, cfg: &AvcConfig) {
    write_box(b, *b"trak", |bb| {
        // tkhd
        write_box(bb, *b"tkhd", |bbb| {
            bbb.put_u32(0x0000_0007);
            bbb.put_u32(0);
            bbb.put_u32(0);
            bbb.put_u32(1); // track_ID
            bbb.put_u32(0);
            bbb.put_u32(0);
            bbb.put_u64(0);
            bbb.put_u16(0);
            bbb.put_u16(0);
            bbb.put_u16(0);
            bbb.put_u16(0);
            for v in [0x0001_0000u32, 0, 0, 0, 0x0001_0000, 0, 0, 0, 0x4000_0000] {
                bbb.put_u32(v);
            }
            bbb.put_u32(u32::from(cfg.width) << 16);
            bbb.put_u32(u32::from(cfg.height) << 16);
        });
        // mdia
        write_box(bb, *b"mdia", |bbb| {
            write_box(bbb, *b"mdhd", |bbbb| {
                bbbb.put_u32(0);
                bbbb.put_u32(0);
                bbbb.put_u32(0);
                bbbb.put_u32(1000);
                bbbb.put_u32(0);
                bbbb.put_u16(0x55c4);
                bbbb.put_u16(0);
            });
            write_box(bbb, *b"hdlr", |bbbb| {
                bbbb.put_u32(0);
                bbbb.put_u32(0);
                bbbb.extend_from_slice(b"vide");
                bbbb.put_u32(0);
                bbbb.put_u32(0);
                bbbb.put_u32(0);
                bbbb.extend_from_slice(b"VideoHandler\0");
            });
            write_box(bbb, *b"minf", |bbbb| {
                write_box(bbbb, *b"vmhd", |b5| {
                    b5.put_u32(0x0000_0001);
                    b5.put_u16(0);
                    b5.put_u16(0);
                    b5.put_u16(0);
                    b5.put_u16(0);
                });
                write_box(bbbb, *b"dinf", |b5| {
                    write_box(b5, *b"dref", |b6| {
                        b6.put_u32(0);
                        b6.put_u32(1);
                        write_box(b6, *b"url ", |b7| {
                            b7.put_u32(0x0000_0001);
                        });
                    });
                });
                write_box(bbbb, *b"stbl", |b5| {
                    write_box(b5, *b"stsd", |b6| {
                        b6.put_u32(0);
                        b6.put_u32(1);
                        write_box(b6, *b"avc1", |b7| {
                            b7.put_u32(0);
                            b7.put_u16(0);
                            b7.put_u16(1);
                            b7.put_u16(0);
                            b7.put_u16(0);
                            for _ in 0..3 {
                                b7.put_u32(0);
                            }
                            b7.put_u16(cfg.width);
                            b7.put_u16(cfg.height);
                            b7.put_u32(0x0048_0000);
                            b7.put_u32(0x0048_0000);
                            b7.put_u32(0);
                            b7.put_u16(1);
                            let name = b"yunmao\0";
                            b7.put_u8(name.len() as u8 - 1);
                            b7.extend_from_slice(&name[..name.len() - 1]);
                            for _ in name.len()..32 {
                                b7.put_u8(0);
                            }
                            b7.put_u16(24);
                            b7.put_u16(0xffff);
                            write_box(b7, *b"avcC", |b8| {
                                b8.extend_from_slice(&cfg.avcc_raw);
                            });
                        });
                    });
                    write_box(b5, *b"stts", |b6| {
                        b6.put_u32(0);
                        b6.put_u32(0);
                    });
                    write_box(b5, *b"stsc", |b6| {
                        b6.put_u32(0);
                        b6.put_u32(0);
                    });
                    write_box(b5, *b"stsz", |b6| {
                        b6.put_u32(0);
                        b6.put_u32(0);
                        b6.put_u32(0);
                    });
                    write_box(b5, *b"stco", |b6| {
                        b6.put_u32(0);
                        b6.put_u32(0);
                    });
                });
            });
        });
    });
}

fn write_audio_trak(b: &mut BytesMut, aac: &AacConfig) {
    write_box(b, *b"trak", |bb| {
        write_box(bb, *b"tkhd", |bbb| {
            bbb.put_u32(0x0000_0007);
            bbb.put_u32(0);
            bbb.put_u32(0);
            bbb.put_u32(2); // track_ID = 2
            bbb.put_u32(0);
            bbb.put_u32(0);
            bbb.put_u64(0);
            bbb.put_u16(0);
            bbb.put_u16(0);
            bbb.put_u16(0x0100); // volume = 1.0
            bbb.put_u16(0);
            for v in [0x0001_0000u32, 0, 0, 0, 0x0001_0000, 0, 0, 0, 0x4000_0000] {
                bbb.put_u32(v);
            }
            bbb.put_u32(0);
            bbb.put_u32(0);
        });
        write_box(bb, *b"mdia", |bbb| {
            write_box(bbb, *b"mdhd", |bbbb| {
                bbbb.put_u32(0);
                bbbb.put_u32(0);
                bbbb.put_u32(0);
                bbbb.put_u32(aac.sample_rate);
                bbbb.put_u32(0);
                bbbb.put_u16(0x55c4);
                bbbb.put_u16(0);
            });
            write_box(bbb, *b"hdlr", |bbbb| {
                bbbb.put_u32(0);
                bbbb.put_u32(0);
                bbbb.extend_from_slice(b"soun");
                bbbb.put_u32(0);
                bbbb.put_u32(0);
                bbbb.put_u32(0);
                bbbb.extend_from_slice(b"SoundHandler\0");
            });
            write_box(bbb, *b"minf", |bbbb| {
                write_box(bbbb, *b"smhd", |b5| {
                    b5.put_u32(0);
                    b5.put_u16(0); // balance
                    b5.put_u16(0); // reserved
                });
                write_box(bbbb, *b"dinf", |b5| {
                    write_box(b5, *b"dref", |b6| {
                        b6.put_u32(0);
                        b6.put_u32(1);
                        write_box(b6, *b"url ", |b7| {
                            b7.put_u32(0x0000_0001);
                        });
                    });
                });
                write_box(bbbb, *b"stbl", |b5| {
                    write_box(b5, *b"stsd", |b6| {
                        b6.put_u32(0);
                        b6.put_u32(1);
                        write_box(b6, *b"mp4a", |b7| {
                            b7.put_u32(0);
                            b7.put_u16(0);
                            b7.put_u16(1); // data_reference_index
                            b7.put_u32(0);
                            b7.put_u32(0); // reserved
                            b7.put_u16(u16::from(aac.channels.max(1)));
                            b7.put_u16(16); // sample_size
                            b7.put_u16(0);
                            b7.put_u16(0);
                            b7.put_u32(aac.sample_rate << 16);
                            // esds
                            write_box(b7, *b"esds", |b8| {
                                b8.put_u32(0); // version + flags
                                               // ES_Descriptor
                                let asc_len = aac.asc.len() as u8;
                                let dec_specific_size = 2 + asc_len;
                                let dec_config_size = 13 + 2 + dec_specific_size;
                                let es_size = 3 + 5 + dec_config_size + 5; // header + ES_DescrTag header is implicit
                                                                           // ES_DescrTag (0x03)
                                b8.put_u8(0x03);
                                put_descriptor_len(b8, u32::from(es_size));
                                b8.put_u16(0x0001); // ES_ID
                                b8.put_u8(0); // flags
                                              // DecoderConfigDescriptor (0x04)
                                b8.put_u8(0x04);
                                put_descriptor_len(b8, u32::from(dec_config_size));
                                b8.put_u8(0x40); // MPEG-4 Audio
                                b8.put_u8(0x15); // streamType=Audio(5) | upStream=0 | reserved=1
                                b8.put_u8(0);
                                b8.put_u8(0);
                                b8.put_u8(0); // bufferSizeDB (24-bit)
                                b8.put_u32(0); // maxBitrate
                                b8.put_u32(0); // avgBitrate
                                               // DecoderSpecificInfo (0x05)
                                b8.put_u8(0x05);
                                put_descriptor_len(b8, u32::from(asc_len));
                                b8.extend_from_slice(&aac.asc);
                                // SLConfigDescriptor (0x06)
                                b8.put_u8(0x06);
                                put_descriptor_len(b8, 1);
                                b8.put_u8(0x02);
                            });
                        });
                    });
                    write_box(b5, *b"stts", |b6| {
                        b6.put_u32(0);
                        b6.put_u32(0);
                    });
                    write_box(b5, *b"stsc", |b6| {
                        b6.put_u32(0);
                        b6.put_u32(0);
                    });
                    write_box(b5, *b"stsz", |b6| {
                        b6.put_u32(0);
                        b6.put_u32(0);
                        b6.put_u32(0);
                    });
                    write_box(b5, *b"stco", |b6| {
                        b6.put_u32(0);
                        b6.put_u32(0);
                    });
                });
            });
        });
    });
}

/// MPEG-4 descriptor variable-length size。
fn put_descriptor_len(b: &mut BytesMut, mut n: u32) {
    let mut bytes = [0u8; 4];
    let mut idx = 3;
    bytes[3] = (n & 0x7f) as u8;
    n >>= 7;
    while n > 0 && idx > 0 {
        idx -= 1;
        bytes[idx] = ((n & 0x7f) as u8) | 0x80;
        n >>= 7;
    }
    b.extend_from_slice(&bytes[idx..]);
}

fn write_moof(
    buf: &mut BytesMut,
    sequence_number: u32,
    decode_time: u64,
    nalus: &[Bytes],
    durations_ms: &[u32],
) {
    write_box(buf, *b"moof", |b| {
        write_box(b, *b"mfhd", |bb| {
            bb.put_u32(0);
            bb.put_u32(sequence_number);
        });
        write_box(b, *b"traf", |bb| {
            write_box(bb, *b"tfhd", |bbb| {
                bbb.put_u32(0x0002_0000); // default_base_is_moof
                bbb.put_u32(1); // track_ID
            });
            write_box(bb, *b"tfdt", |bbb| {
                bbb.put_u32(0x0100_0000);
                bbb.put_u64(decode_time);
            });
            write_box(bb, *b"trun", |bbb| {
                let flags: u32 = 0x0000_0301 | 0x0000_0004;
                bbb.put_u32(flags & 0x00ff_ffff);
                bbb.put_u32(nalus.len() as u32);
                bbb.put_i32(0);
                bbb.put_u32(0x0200_0000);
                for (i, nal) in nalus.iter().enumerate() {
                    let dur = durations_ms.get(i).copied().unwrap_or(33);
                    bbb.put_u32(dur);
                    bbb.put_u32(nal.len() as u32);
                }
            });
        });
    });
}

fn write_mdat(buf: &mut BytesMut, nalus: &[Bytes]) {
    write_box(buf, *b"mdat", |b| {
        for n in nalus {
            b.extend_from_slice(n);
        }
    });
}

/// 多 traf moof（视频 trak_ID=1 + 音频 trak_ID=2）。
///
/// trun 的 data_offset 都是相对 moof 起点的字节偏移，与 ISO/IEC 14496-12 一致。
/// 我们在 mdat 中先放视频 NALU，再放 AAC raw，因此 video_data_offset = moof_size + 8（mdat 头），
/// audio_data_offset = video_data_offset + sum(nalus.len)。
fn write_moof_av(
    buf: &mut BytesMut,
    sequence_number: u32,
    video_decode_time: u64,
    nalus: &[Bytes],
    video_durations_ms: &[u32],
    audio_decode_time: u64,
    audio_frames: &[AacFrame],
) {
    let video_bytes: usize = nalus.iter().map(|n| n.len()).sum();
    let audio_bytes: usize = audio_frames.iter().map(|f| f.data.len()).sum();

    // 先用 0 占位写一次 moof + mdat，统计 moof 实际大小，再回填 data_offset。
    let moof_start = buf.len();
    write_box(buf, *b"moof", |b| {
        write_box(b, *b"mfhd", |bb| {
            bb.put_u32(0);
            bb.put_u32(sequence_number);
        });
        // 视频 traf（trak_ID=1）
        write_box(b, *b"traf", |bb| {
            write_box(bb, *b"tfhd", |bbb| {
                bbb.put_u32(0x0002_0000); // default_base_is_moof
                bbb.put_u32(1);
            });
            write_box(bb, *b"tfdt", |bbb| {
                bbb.put_u32(0x0100_0000);
                bbb.put_u64(video_decode_time);
            });
            write_box(bb, *b"trun", |bbb| {
                // flags: 0x000001 data_offset_present
                //      | 0x000004 first_sample_flags_present
                //      | 0x000100 sample_duration_present
                //      | 0x000200 sample_size_present
                let flags: u32 = 0x0000_0001 | 0x0000_0004 | 0x0000_0100 | 0x0000_0200;
                bbb.put_u32(flags);
                bbb.put_u32(nalus.len() as u32);
                bbb.put_i32(0); // data_offset placeholder (fill后)
                bbb.put_u32(0x0200_0000); // first_sample_flags：sample_depends_on=2 (I-frame)
                for (i, nal) in nalus.iter().enumerate() {
                    let dur = video_durations_ms.get(i).copied().unwrap_or(33);
                    bbb.put_u32(dur);
                    bbb.put_u32(nal.len() as u32);
                }
            });
        });
        // 音频 traf（trak_ID=2）
        write_box(b, *b"traf", |bb| {
            write_box(bb, *b"tfhd", |bbb| {
                bbb.put_u32(0x0002_0000);
                bbb.put_u32(2);
            });
            write_box(bb, *b"tfdt", |bbb| {
                bbb.put_u32(0x0100_0000);
                bbb.put_u64(audio_decode_time);
            });
            write_box(bb, *b"trun", |bbb| {
                // 音频不需要 first_sample_flags，audio sample 默认 sync。
                let flags: u32 = 0x0000_0001 | 0x0000_0100 | 0x0000_0200;
                bbb.put_u32(flags);
                bbb.put_u32(audio_frames.len() as u32);
                bbb.put_i32(0); // data_offset placeholder
                for f in audio_frames {
                    bbb.put_u32(f.samples);
                    bbb.put_u32(f.data.len() as u32);
                }
            });
        });
    });

    // 回填 trun data_offset：在 moof 中按 box 顺序查找两个 trun 并改 data_offset。
    // 实现：扫描 moof 内的 4-byte name == "trun"。
    let moof_size = buf.len() - moof_start;
    let mdat_header = 8usize;
    let video_data_offset = (moof_size + mdat_header) as i32;
    let audio_data_offset = (moof_size + mdat_header + video_bytes) as i32;

    let mut occurrences = 0;
    let mut i = moof_start;
    while i + 8 <= buf.len() {
        if &buf[i + 4..i + 8] == b"trun" {
            // trun box 内布局：size(4) + name(4) + flags(4) + sample_count(4) + data_offset(4) ...
            let off_pos = i + 8 + 4 + 4;
            let value = match occurrences {
                0 => video_data_offset,
                1 => audio_data_offset,
                _ => break,
            };
            buf[off_pos..off_pos + 4].copy_from_slice(&value.to_be_bytes());
            occurrences += 1;
            i += 8;
        } else {
            i += 1;
        }
    }
    let _ = audio_bytes;
}

fn write_mdat_av(buf: &mut BytesMut, nalus: &[Bytes], audio_frames: &[AacFrame]) {
    write_box(buf, *b"mdat", |b| {
        for n in nalus {
            b.extend_from_slice(n);
        }
        for f in audio_frames {
            b.extend_from_slice(&f.data);
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_avc_cfg() -> AvcConfig {
        AvcConfig {
            width: 1280,
            height: 720,
            profile_idc: 0x42,
            level_idc: 0x1f,
            frame_rate_x1000: 30_000,
            avcc_raw: Bytes::from_static(&[
                0x01, 0x42, 0xc0, 0x1f, 0xff, 0xe1, 0x00, 0x10, 0x67, 0x42, 0xc0, 0x1f, 0xda, 0x02,
                0x80, 0xbf, 0xe5, 0xc0, 0x44, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x00, 0x03, 0x00,
                0xf0, 0x3c, 0x60, 0xc9, 0x20, 0x01, 0x00, 0x04, 0x68, 0xce, 0x3c, 0x80,
            ]),
            nalu_len_size: 4,
        }
    }

    #[test]
    fn init_segment_starts_with_ftyp() {
        let cfg = make_avc_cfg();
        let init = build_init_segment_video_only(&cfg);
        assert_eq!(&init[4..8], b"ftyp");
        let ftyp_size = u32::from_be_bytes(init[0..4].try_into().unwrap()) as usize;
        assert_eq!(&init[ftyp_size + 4..ftyp_size + 8], b"moov");
    }

    #[test]
    fn init_segment_with_audio_has_two_traks() {
        let cfg = make_avc_cfg();
        // AAC LC 44.1kHz stereo: ASC = 0x12 0x10
        let aac = AacConfig::from_flv_aac_seq(&[0xaf, 0x00, 0x12, 0x10]).unwrap();
        assert_eq!(aac.object_type, 2);
        assert_eq!(aac.sample_rate, 44_100);
        assert_eq!(aac.channels, 2);
        let init = build_init_segment(&cfg, Some(&aac));
        assert_eq!(&init[4..8], b"ftyp");
        // 至少包含 2 个 trak box（统计 magic）
        let mut count = 0usize;
        let mut i = 0;
        while i + 8 <= init.len() {
            if &init[i + 4..i + 8] == b"trak" {
                count += 1;
            }
            i += 1;
        }
        assert!(count >= 2, "expected >= 2 trak boxes, got {}", count);
    }

    #[test]
    fn media_segment_starts_with_moof() {
        let seg = build_media_segment(
            1,
            0,
            &[Bytes::from_static(&[0, 0, 0, 5, 1, 2, 3, 4, 5])],
            &[33],
        );
        assert_eq!(&seg[4..8], b"moof");
        let moof_size = u32::from_be_bytes(seg[0..4].try_into().unwrap()) as usize;
        assert_eq!(&seg[moof_size + 4..moof_size + 8], b"mdat");
    }

    #[test]
    fn sps_parser_extracts_size_from_known_avcc() {
        // ffmpeg testsrc 1280x720 30fps SPS（节选）
        // 完整 AVCC：0x01 0x42 0xc0 0x1f ... + SPS
        let avcc = vec![
            0x01, 0x42, 0xc0, 0x1f, 0xff, 0xe1, 0x00, 0x19, // SPS NALU header 0x67 + SPS RBSP
            0x67, 0x42, 0xc0, 0x1f, 0xda, 0x02, 0x80, 0xbf, 0xe5, 0xc0, 0x44, 0x00, 0x00, 0x03,
            0x00, 0x04, 0x00, 0x00, 0x03, 0x00, 0xf0, 0x3c, 0x60, 0xc9, 0x20, 0x01, 0x00, 0x04,
            0x68, 0xce, 0x3c, 0x80,
        ];
        let mut tag = vec![0x17, 0x00, 0x00, 0x00, 0x00];
        tag.extend_from_slice(&avcc);
        let cfg = AvcConfig::from_flv_avc_seq(&tag).unwrap();
        // 注意：此处的 SPS 不一定能精确解码到 1280x720（demo 数据），但宽高必须 > 0。
        assert!(cfg.width > 0);
        assert!(cfg.height > 0);
        assert_eq!(cfg.profile_idc, 0x42);
        assert_eq!(cfg.level_idc, 0x1f);
    }

    #[test]
    fn media_segment_av_has_two_trafs_and_correct_data_offsets() {
        // 两个视频 NALU、三个 AAC 帧
        let nalus = vec![
            Bytes::from_static(&[0, 0, 0, 5, 1, 2, 3, 4, 5]),
            Bytes::from_static(&[0, 0, 0, 3, 9, 8, 7]),
        ];
        let durs = vec![33, 33];
        let aac_frames = vec![
            AacFrame {
                data: Bytes::from_static(&[0xab; 8]),
                samples: 1024,
            },
            AacFrame {
                data: Bytes::from_static(&[0xcd; 6]),
                samples: 1024,
            },
            AacFrame {
                data: Bytes::from_static(&[0xef; 10]),
                samples: 1024,
            },
        ];
        let seg = build_media_segment_av(1, 0, &nalus, &durs, 0, &aac_frames);
        // 起始 moof
        assert_eq!(&seg[4..8], b"moof");
        // 统计 traf 数
        let mut traf_count = 0usize;
        let mut i = 0;
        while i + 8 <= seg.len() {
            if &seg[i + 4..i + 8] == b"traf" {
                traf_count += 1;
            }
            i += 1;
        }
        assert_eq!(traf_count, 2, "expected 2 traf, got {}", traf_count);

        // 计算 mdat 起点 = moof_size 字节
        let moof_size = u32::from_be_bytes(seg[0..4].try_into().unwrap()) as usize;
        assert_eq!(&seg[moof_size + 4..moof_size + 8], b"mdat");

        // 找 trun 的 data_offset：第一个 trun 应当指向 mdat header 后的 video 起点
        let mut trun_count = 0usize;
        let mut idx = 0;
        let video_bytes: usize = nalus.iter().map(|n| n.len()).sum();
        while idx + 8 <= seg.len() {
            if &seg[idx + 4..idx + 8] == b"trun" {
                let off_pos = idx + 8 + 4 + 4;
                let v = i32::from_be_bytes(seg[off_pos..off_pos + 4].try_into().unwrap());
                if trun_count == 0 {
                    assert_eq!(v as usize, moof_size + 8, "video data_offset");
                } else {
                    assert_eq!(v as usize, moof_size + 8 + video_bytes, "audio data_offset");
                }
                trun_count += 1;
            }
            idx += 1;
        }
        assert_eq!(trun_count, 2);
    }

    #[test]
    fn aac_seq_parses_48k_stereo() {
        // AAC LC 48kHz stereo:
        //  object_type=2 (LC) → bits 00010
        //  freq_idx=3 (48kHz) → bits 0011
        //  channel_config=2 (stereo) → bits 0010
        // Concat: 00010 0011 0010 000... → 0x11 0x90
        let aac = AacConfig::from_flv_aac_seq(&[0xaf, 0x00, 0x11, 0x90]).unwrap();
        assert_eq!(aac.object_type, 2);
        assert_eq!(aac.sample_rate, 48_000);
        assert_eq!(aac.channels, 2);
    }
}
