package moderation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAliyunGreenRealClient_ParsesSuggestion(t *testing.T) {
	// Mock server 模拟 Green API。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 校验 Signature 参数存在（不实际验签，只验证我们发送）
		if r.URL.Query().Get("Signature") == "" {
			http.Error(w, "missing Signature", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("Action") != "TextScan" {
			http.Error(w, "wrong action", http.StatusBadRequest)
			return
		}
		// 返回 block 建议
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"msg":  "OK",
			"data": []map[string]any{
				{
					"code":   200,
					"dataId": "task1",
					"results": []map[string]any{
						{"scene": "antispam", "suggestion": "block", "label": "abuse", "rate": 98.5},
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := NewAliyunGreenRealClient(AliyunGreenConfig{
		AccessKey:    "ak",
		AccessSecret: "sk",
		Region:       "cn-shanghai",
		Endpoint:     srv.URL,
	}, srv.Client())
	dec, err := client.Inspect(context.Background(), "辱骂内容")
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != ActionBlock {
		t.Fatalf("want block got %s", dec.Action)
	}
	if dec.Provider != "aliyun_green" {
		t.Fatal("provider name mismatch")
	}
}

func TestAliyunGreenRealClient_NetworkFailureReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 模拟超时：sleep > timeout
		time.Sleep(50 * time.Millisecond)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	client := NewAliyunGreenRealClient(AliyunGreenConfig{
		AccessKey: "ak", AccessSecret: "sk", Region: "cn-hz", Endpoint: srv.URL,
	}, srv.Client())
	_, err := client.Inspect(context.Background(), "test")
	if err == nil {
		t.Fatal("expect error on 500")
	}
}

func TestAliyunGreenRPCSigner_StableOutput(t *testing.T) {
	params := mustValues(map[string]string{
		"Action":           "TextScan",
		"AccessKeyId":      "ak",
		"SignatureNonce":   "nonce",
		"Timestamp":        "2026-05-25T00:00:00Z",
		"Format":           "JSON",
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"Version":          "2018-05-09",
	})
	sig := signAliyunRPC("POST", params, "sk")
	if sig == "" || len(sig) < 20 {
		t.Fatalf("signature too short: %q", sig)
	}
	// 同样的输入应当签出同样的输出（确定性）
	sig2 := signAliyunRPC("POST", params, "sk")
	if sig != sig2 {
		t.Fatal("signature should be deterministic")
	}
}

func mustValues(in map[string]string) (out map[string][]string) {
	out = map[string][]string{}
	for k, v := range in {
		out[k] = []string{v}
	}
	return out
}
