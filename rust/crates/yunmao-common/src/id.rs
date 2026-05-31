//! 领域 ID（参考 `docs/finalproductplanning/11-数据指标与术语表.md` 第 7 节）。
//!
//! 形如 `usr_01HZX...`：
//!
//! - 前缀来自 [`IdPrefix`]
//! - 主体为 ULID（Crockford Base32，单调递增、URL safe）

use std::fmt;
use std::str::FromStr;

use serde::{Deserialize, Serialize};
use thiserror::Error;
use ulid::Ulid;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum IdPrefix {
    User,
    Cat,
    Room,
    Device,
    Feed,
    Cmd,
    Order,
    Stream,
    Session,
}

impl IdPrefix {
    pub const fn as_str(self) -> &'static str {
        match self {
            IdPrefix::User => "usr",
            IdPrefix::Cat => "cat",
            IdPrefix::Room => "room",
            IdPrefix::Device => "dev",
            IdPrefix::Feed => "feed",
            IdPrefix::Cmd => "cmd",
            IdPrefix::Order => "ord",
            IdPrefix::Stream => "stm",
            IdPrefix::Session => "sess",
        }
    }

    pub fn parse(prefix: &str) -> Option<Self> {
        match prefix {
            "usr" => Some(IdPrefix::User),
            "cat" => Some(IdPrefix::Cat),
            "room" => Some(IdPrefix::Room),
            "dev" => Some(IdPrefix::Device),
            "feed" => Some(IdPrefix::Feed),
            "cmd" => Some(IdPrefix::Cmd),
            "ord" => Some(IdPrefix::Order),
            "stm" => Some(IdPrefix::Stream),
            "sess" => Some(IdPrefix::Session),
            _ => None,
        }
    }
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum IdParseError {
    #[error("ID 缺少前缀分隔符 '_'")]
    MissingSeparator,
    #[error("未知 ID 前缀: {0}")]
    UnknownPrefix(String),
    #[error("ULID 部分非法: {0}")]
    InvalidUlid(#[from] ulid::DecodeError),
}

#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct DomainId {
    prefix: IdPrefix,
    ulid: Ulid,
}

impl DomainId {
    pub fn new(prefix: IdPrefix) -> Self {
        Self {
            prefix,
            ulid: Ulid::new(),
        }
    }

    pub fn from_parts(prefix: IdPrefix, ulid: Ulid) -> Self {
        Self { prefix, ulid }
    }

    pub fn prefix(&self) -> IdPrefix {
        self.prefix
    }

    pub fn ulid(&self) -> Ulid {
        self.ulid
    }
}

impl fmt::Display for DomainId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}_{}", self.prefix.as_str(), self.ulid)
    }
}

impl FromStr for DomainId {
    type Err = IdParseError;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        let (prefix_str, body) = s.split_once('_').ok_or(IdParseError::MissingSeparator)?;
        let prefix = IdPrefix::parse(prefix_str)
            .ok_or_else(|| IdParseError::UnknownPrefix(prefix_str.to_string()))?;
        let ulid: Ulid = body.parse()?;
        Ok(Self { prefix, ulid })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trip_format() {
        let id = DomainId::new(IdPrefix::User);
        let rendered = id.to_string();
        assert!(rendered.starts_with("usr_"));
        let parsed: DomainId = rendered.parse().unwrap();
        assert_eq!(parsed, id);
    }

    #[test]
    fn rejects_unknown_prefix() {
        let err: IdParseError = "xx_01H12345678901234567890123"
            .parse::<DomainId>()
            .unwrap_err();
        assert!(matches!(err, IdParseError::UnknownPrefix(_)));
    }

    #[test]
    fn rejects_missing_separator() {
        let err: IdParseError = "abc".parse::<DomainId>().unwrap_err();
        assert_eq!(err, IdParseError::MissingSeparator);
    }

    #[test]
    fn all_prefixes_round_trip() {
        for p in [
            IdPrefix::User,
            IdPrefix::Cat,
            IdPrefix::Room,
            IdPrefix::Device,
            IdPrefix::Feed,
            IdPrefix::Cmd,
            IdPrefix::Order,
            IdPrefix::Stream,
            IdPrefix::Session,
        ] {
            assert_eq!(IdPrefix::parse(p.as_str()), Some(p));
        }
    }
}
