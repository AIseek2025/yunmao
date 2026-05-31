// Package e2e 提供 yunmao 投喂全链路集成测试（testcontainers-go 起 PG/Redis/Kafka/EMQX）。
//
// 默认 go test 不会编译实测，需要 `-tags=integration` 才会启用；详见 integration_test.go。
package e2e
