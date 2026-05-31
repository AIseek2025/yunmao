//! Topic 命名集中收口（与 Go pkg/yunmao/eventbus 同步）。

const DLQ_SUFFIX: &str = ".dlq";

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct Topic(String);

impl Topic {
    pub fn new(s: impl Into<String>) -> Self {
        Self(s.into())
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }

    pub fn dlq(&self) -> Self {
        Topic(format!("{}{}", self.0, DLQ_SUFFIX))
    }
}

impl std::fmt::Display for Topic {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}

/// 常用 topic 常量；与 Go 端 [`pkg/yunmao/eventbus`] 一致。
pub mod names {
    pub const FEED_REQUEST_CREATED: &str = "feed.request.created";
    pub const FEED_COMMAND_REQUESTED: &str = "feed.command.requested";
    pub const FEED_COMMAND_DISPATCHED: &str = "feed.command.dispatched";
    pub const FEED_COMMAND_ACKED: &str = "feed.command.acked";
    pub const FEED_COMMAND_COMPLETED: &str = "feed.command.completed";
    pub const FEED_COMMAND_FAILED: &str = "feed.command.failed";
    pub const DEVICE_STATE_CHANGED: &str = "device.state.changed";
    pub const LIVE_STREAM_ONLINE: &str = "live.stream.online";
    pub const LIVE_STREAM_OFFLINE: &str = "live.stream.offline";
    // 第六轮新增：弹幕事件（chat-svc → kafka → gateway 扩散）。
    pub const ROOM_CHAT_MESSAGE: &str = "room.chat.message";
    pub const ROOM_CHAT_MODERATION: &str = "room.chat.moderation";
    // 第六轮新增：钱包 saga（billing-svc → outbox → kafka）。
    pub const WALLET_RESERVED: &str = "wallet.reserved";
    pub const WALLET_CONFIRMED: &str = "wallet.confirmed";
    pub const WALLET_CANCELLED: &str = "wallet.cancelled";
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn dlq_suffix() {
        let t = Topic::new(names::FEED_COMMAND_REQUESTED);
        assert_eq!(t.dlq().as_str(), "feed.command.requested.dlq");
    }
}
