//! CloudEvents 1.0 信封（参考 `04-设备接入数据模型与API边界.md` 第 10 节）。
//!
//! 在 yunmao 中，事件总线（Kafka）一律使用本结构作为统一信封。

use serde::{Deserialize, Serialize};
use time::OffsetDateTime;
use uuid::Uuid;

/// CloudEvents 1.0 信封 - JSON event format。
///
/// `data` 在 yunmao 中默认仍为 JSON（CloudEvents 也允许 binary base64 数据，但首期不引入二进制载荷）。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CloudEvent<T = serde_json::Value> {
    /// 事件唯一 ID，幂等键（UUIDv4）
    pub id: String,
    /// 来源服务（`service@instance`）
    pub source: String,
    /// 事件类型，与 Kafka topic 一致，如 `feed.command.requested`
    #[serde(rename = "type")]
    pub event_type: String,
    /// 业务主键（room_id / device_id / feed_request_id 等）
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub subject: Option<String>,
    /// 事件时间（UTC，ISO-8601）
    #[serde(with = "iso8601")]
    pub time: OffsetDateTime,
    /// CloudEvents specversion 固定为 "1.0"
    #[serde(default = "default_specversion")]
    pub specversion: String,
    /// 数据 schema URI（schema-registry 中的 `topic@version`），可空
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub dataschema: Option<String>,
    /// 数据内容类型，默认 `application/json`
    #[serde(default = "default_datacontenttype")]
    pub datacontenttype: String,
    /// 实际负载
    pub data: T,
}

fn default_specversion() -> String {
    "1.0".to_string()
}

fn default_datacontenttype() -> String {
    "application/json".to_string()
}

impl<T: Serialize> CloudEvent<T> {
    pub fn new(
        event_type: impl Into<String>,
        source: impl Into<String>,
        subject: Option<String>,
        data: T,
    ) -> Self {
        Self {
            id: Uuid::new_v4().to_string(),
            source: source.into(),
            event_type: event_type.into(),
            subject,
            time: OffsetDateTime::now_utc(),
            specversion: default_specversion(),
            dataschema: None,
            datacontenttype: default_datacontenttype(),
            data,
        }
    }

    pub fn with_dataschema(mut self, schema: impl Into<String>) -> Self {
        self.dataschema = Some(schema.into());
        self
    }
}

/// 把 `time` 字段按 `RFC3339` UTC 序列化（CloudEvents 标准）。
mod iso8601 {
    use serde::{Deserialize, Deserializer, Serializer};
    use time::format_description::well_known::Rfc3339;
    use time::OffsetDateTime;

    pub fn serialize<S: Serializer>(t: &OffsetDateTime, ser: S) -> Result<S::Ok, S::Error> {
        let s = t.format(&Rfc3339).map_err(serde::ser::Error::custom)?;
        ser.serialize_str(&s)
    }

    pub fn deserialize<'de, D: Deserializer<'de>>(de: D) -> Result<OffsetDateTime, D::Error> {
        let s = String::deserialize(de)?;
        OffsetDateTime::parse(&s, &Rfc3339).map_err(serde::de::Error::custom)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[derive(Serialize, Deserialize, PartialEq, Debug)]
    struct FeedRequested {
        feed_request_id: String,
        room_id: String,
        device_id: String,
        amount_grams: u32,
    }

    #[test]
    fn roundtrip_typed_event() {
        let evt = CloudEvent::new(
            "feed.command.requested",
            "feeding-svc@dev-1",
            Some("feed_demo".into()),
            FeedRequested {
                feed_request_id: "feed_01HZX".into(),
                room_id: "room_demo".into(),
                device_id: "dev_demo".into(),
                amount_grams: 5,
            },
        );
        let json = serde_json::to_string(&evt).unwrap();
        let back: CloudEvent<FeedRequested> = serde_json::from_str(&json).unwrap();
        assert_eq!(back.event_type, "feed.command.requested");
        assert_eq!(back.specversion, "1.0");
        assert_eq!(back.data.amount_grams, 5);
    }

    #[test]
    fn defaults_present() {
        let raw = r#"{
            "id":"00000000-0000-0000-0000-000000000001",
            "source":"x@y",
            "type":"foo.bar",
            "time":"2026-05-25T00:00:00Z",
            "data":{"k":1}
        }"#;
        let evt: CloudEvent = serde_json::from_str(raw).unwrap();
        assert_eq!(evt.specversion, "1.0");
        assert_eq!(evt.datacontenttype, "application/json");
    }
}
