// Package observability 深度探针辅助：
//
//   - PgProbe（pgxpool）→ SELECT 1（200ms 超时）
//   - RedisProbe（cache.Store / *redis.Client）→ PING
//   - KafkaProbe（eventbus.Bus 或 brokers list）→ Metadata Dial
//   - MqttProbe（mqttx.Client）→ IsConnected + Ping
//
// 调用方在 server.New 里把可用的依赖装入 [`Probes`] 表，然后注入 [`WireFull`]。
package observability

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// PgProbe 返回一个 SELECT 1 的探针，超时 200ms。
//
//	probes := observability.Probes{"pg": observability.PgProbe(pool)}
//	observability.WireFull(r, probes, nil)
func PgProbe(pool *pgxpool.Pool) Probe {
	if pool == nil {
		return func() error { return errors.New("pg pool nil") }
	}
	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		var v int
		if err := pool.QueryRow(ctx, "SELECT 1").Scan(&v); err != nil {
			return fmt.Errorf("pg: %w", err)
		}
		if v != 1 {
			return fmt.Errorf("pg: unexpected value %d", v)
		}
		return nil
	}
}

// RedisProbe 用 go-redis client PING；超时 200ms。
func RedisProbe(cli *redis.Client) Probe {
	if cli == nil {
		return func() error { return errors.New("redis client nil") }
	}
	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		return cli.Ping(ctx).Err()
	}
}

// KafkaProbe 用 TCP Dial 试连一个 broker，timeout 200ms。
//
// 注意：不依赖 sarama / kafka-go SDK，避免在 observability 引入大量依赖。
func KafkaProbe(brokers []string) Probe {
	if len(brokers) == 0 {
		return func() error { return errors.New("kafka brokers empty") }
	}
	return func() error {
		dialer := net.Dialer{Timeout: 200 * time.Millisecond}
		var firstErr error
		for _, b := range brokers {
			conn, err := dialer.Dial("tcp", b)
			if err == nil {
				_ = conn.Close()
				return nil
			}
			if firstErr == nil {
				firstErr = err
			}
		}
		return fmt.Errorf("kafka: dial all brokers failed: %v", firstErr)
	}
}

// MqttProbe 是一个最小 MQTT 健康检查：调用者注入 `IsConnected() bool` 的客户端。
//
//	probes["mqtt"] = observability.MqttProbe(func() bool { return mqttClient.IsConnected() })
func MqttProbe(isConnected func() bool) Probe {
	if isConnected == nil {
		return func() error { return errors.New("mqtt client nil") }
	}
	return func() error {
		if !isConnected() {
			return errors.New("mqtt: not connected")
		}
		return nil
	}
}

// KeysProbe 校验 KeyProvider.Active() 返回非空 / 非 HS256（ADR-0019）。
// 用于发现 alg 配置错误（例如下放了 HS256 SigningKey）。
func KeysProbe(active func() (string, error)) Probe {
	if active == nil {
		return func() error { return errors.New("keys probe: nil getter") }
	}
	return func() error {
		alg, err := active()
		if err != nil {
			return fmt.Errorf("keys: %w", err)
		}
		if alg == "HS256" {
			return errors.New("keys: HS256 active (ADR-0019: removed)")
		}
		return nil
	}
}
