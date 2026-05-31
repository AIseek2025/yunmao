//! Kafka 实现，使用 `rskafka`（纯 Rust，无 librdkafka 依赖）。
//!
//! 设计折中：
//!
//! - 仅实现 producer + consumer 最小可用；不暴露 `rskafka` 类型给上层。
//! - 消费时按 topic 起一个 partition fetcher loop，offset 自管理（内存）。
//!   生产模式应当持久化到 Kafka `__consumer_offsets` 或外部 KV；
//!   本轮目标是“跑通端到端”，先做最小可用。
//!
//! 这与 Go 端 `pkg/yunmao/eventbus` 不完全对等（Go 端用 Kafka consumer group），
//! 但作为单实例 device-edge / gateway 消费端是足够的。

use std::collections::BTreeMap;
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use rskafka::client::partition::{Compression, OffsetAt, UnknownTopicHandling};
use rskafka::client::{Client, ClientBuilder};
use rskafka::record::Record;
use tokio::sync::Mutex;
use tracing::{debug, warn};

use crate::{Bus, Envelope, EventBusError, Handler, Topic};

pub struct KafkaBus {
    client: Arc<Client>,
    closed: Mutex<bool>,
}

impl KafkaBus {
    pub async fn connect(brokers: Vec<String>, _client_id: String) -> Result<Self, EventBusError> {
        let client = ClientBuilder::new(brokers)
            .build()
            .await
            .map_err(|e| EventBusError::Kafka(format!("{e}")))?;
        Ok(Self {
            client: Arc::new(client),
            closed: Mutex::new(false),
        })
    }
}

#[async_trait]
impl Bus for KafkaBus {
    async fn publish(&self, env: Envelope) -> Result<(), EventBusError> {
        if *self.closed.lock().await {
            return Err(EventBusError::Closed);
        }
        let partition_client = self
            .client
            .partition_client(env.topic.as_str(), 0, UnknownTopicHandling::Retry)
            .await
            .map_err(|e| EventBusError::Kafka(format!("partition_client: {e}")))?;
        let mut headers = BTreeMap::new();
        for (k, v) in env.headers.iter() {
            headers.insert(k.clone(), v.as_bytes().to_vec());
        }
        let record = Record {
            key: Some(env.key.into_bytes()),
            value: Some(env.payload),
            headers,
            timestamp: chrono_now(),
        };
        partition_client
            .produce(vec![record], Compression::default())
            .await
            .map_err(|e| EventBusError::Kafka(format!("produce: {e}")))?;
        Ok(())
    }

    async fn subscribe(
        &self,
        _group: &str,
        topics: Vec<Topic>,
        handler: Handler,
    ) -> Result<(), EventBusError> {
        for t in topics {
            let topic_name = t.as_str().to_string();
            let client = self.client.clone();
            let handler = handler.clone();
            tokio::spawn(async move {
                let pc = match client
                    .partition_client(topic_name.as_str(), 0, UnknownTopicHandling::Retry)
                    .await
                {
                    Ok(p) => p,
                    Err(e) => {
                        warn!(error = %e, topic = %topic_name, "kafka subscribe failed");
                        return;
                    }
                };
                let mut offset = match pc.get_offset(OffsetAt::Latest).await {
                    Ok(o) => o,
                    Err(e) => {
                        warn!(error = %e, "kafka get_offset failed; starting at 0");
                        0
                    }
                };
                loop {
                    match pc.fetch_records(offset, 1..1_000_000, 1_000).await {
                        Ok((records, _high_water)) => {
                            for r in records {
                                let env = record_to_env(&topic_name, r.record);
                                if let Err(e) = (handler)(env).await {
                                    warn!(error = %e, topic = %topic_name, "handler failed");
                                }
                                offset = r.offset + 1;
                            }
                        }
                        Err(e) => {
                            debug!(error = %e, "kafka fetch loop error; backing off");
                            tokio::time::sleep(Duration::from_millis(500)).await;
                        }
                    }
                }
            });
        }
        Ok(())
    }

    async fn close(&self) -> Result<(), EventBusError> {
        *self.closed.lock().await = true;
        Ok(())
    }
}

fn chrono_now() -> chrono::DateTime<chrono::Utc> {
    chrono::Utc::now()
}

fn record_to_env(topic: &str, r: Record) -> Envelope {
    let mut hdrs = std::collections::HashMap::new();
    for (k, v) in r.headers.into_iter() {
        if let Ok(s) = String::from_utf8(v) {
            hdrs.insert(k, s);
        }
    }
    Envelope {
        topic: Topic::new(topic),
        key: r
            .key
            .and_then(|k| String::from_utf8(k).ok())
            .unwrap_or_default(),
        headers: hdrs,
        payload: r.value.unwrap_or_default(),
    }
}
