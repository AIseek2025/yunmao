//! 进程内总线（单测 / PoC 模式）。

use std::collections::HashMap;
use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::{broadcast, RwLock};
use tracing::warn;

use crate::{Bus, Envelope, EventBusError, Handler, Topic};

#[derive(Default)]
pub struct MemoryBus {
    inner: Arc<RwLock<HashMap<Topic, broadcast::Sender<Envelope>>>>,
}

impl MemoryBus {
    pub fn new() -> Self {
        Self::default()
    }
}

#[async_trait]
impl Bus for MemoryBus {
    async fn publish(&self, env: Envelope) -> Result<(), EventBusError> {
        let g = self.inner.read().await;
        if let Some(tx) = g.get(&env.topic) {
            let _ = tx.send(env);
        }
        Ok(())
    }

    async fn subscribe(
        &self,
        _group: &str,
        topics: Vec<Topic>,
        handler: Handler,
    ) -> Result<(), EventBusError> {
        let mut receivers = Vec::new();
        {
            let mut g = self.inner.write().await;
            for t in topics {
                let tx = g
                    .entry(t.clone())
                    .or_insert_with(|| broadcast::channel(1024).0);
                receivers.push(tx.subscribe());
            }
        }
        for mut rx in receivers {
            let h = handler.clone();
            tokio::spawn(async move {
                while let Ok(env) = rx.recv().await {
                    if let Err(e) = (h)(env).await {
                        warn!(error = %e, "memory bus handler failed");
                    }
                }
            });
        }
        Ok(())
    }

    async fn close(&self) -> Result<(), EventBusError> {
        let mut g = self.inner.write().await;
        g.clear();
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::topic::names;
    use crate::Topic;
    use std::sync::atomic::{AtomicU32, Ordering};
    use std::sync::Arc;

    #[tokio::test]
    async fn publish_then_subscribe_delivers() {
        let bus = MemoryBus::new();
        let count = Arc::new(AtomicU32::new(0));
        let count2 = count.clone();
        let handler: Handler = Arc::new(move |_env| {
            let c = count2.clone();
            Box::pin(async move {
                c.fetch_add(1, Ordering::SeqCst);
                Ok(())
            })
        });
        bus.subscribe("g", vec![Topic::new(names::FEED_COMMAND_ACKED)], handler)
            .await
            .unwrap();
        // small wait so subscriber is ready
        tokio::time::sleep(std::time::Duration::from_millis(20)).await;

        let env = Envelope {
            topic: Topic::new(names::FEED_COMMAND_ACKED),
            key: "dev_x".into(),
            headers: Default::default(),
            payload: b"{}".to_vec(),
        };
        bus.publish(env).await.unwrap();
        tokio::time::sleep(std::time::Duration::from_millis(40)).await;
        assert_eq!(count.load(Ordering::SeqCst), 1);
    }
}
