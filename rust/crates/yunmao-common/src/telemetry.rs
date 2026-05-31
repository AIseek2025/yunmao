//! tracing-subscriber 初始化（结构化 JSON 日志 + env-filter）。
//!
//! 用法：每个二进制 `main` 起来后第一行调用 `telemetry::init("media-edge")`。

use tracing_subscriber::fmt::format::FmtSpan;
use tracing_subscriber::EnvFilter;

#[derive(Debug, Clone, Copy)]
pub enum LogFormat {
    Json,
    Pretty,
}

pub fn init(service_name: &str) {
    let format = std::env::var("YUNMAO_LOG_FORMAT")
        .map(|v| match v.as_str() {
            "json" => LogFormat::Json,
            _ => LogFormat::Pretty,
        })
        .unwrap_or(LogFormat::Json);
    init_with_format(service_name, format);
}

pub fn init_with_format(service_name: &str, format: LogFormat) {
    let env_filter = EnvFilter::try_from_env("YUNMAO_LOG")
        .unwrap_or_else(|_| EnvFilter::new("info,yunmao_=debug"));

    let builder = tracing_subscriber::fmt()
        .with_env_filter(env_filter)
        .with_target(true)
        .with_span_events(FmtSpan::CLOSE);

    let result = match format {
        LogFormat::Json => builder.json().with_current_span(true).try_init(),
        LogFormat::Pretty => builder.try_init(),
    };

    if result.is_ok() {
        tracing::info!(service = service_name, "telemetry initialized");
    }
}
