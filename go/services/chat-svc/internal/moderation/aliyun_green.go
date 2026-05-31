package moderation

// 第八轮（C）：阿里云内容安全 Green SDK 真接路径。
//
// 不引入 `github.com/aliyun/alibaba-cloud-sdk-go/services/green` 重 SDK，
// 直接用 stdlib 实现 RPC-style 签名（POPv1）。算法与官方 SDK 一致：
//   1. 把 canonical query string 按 key 字典序拼接；
//   2. signingString = "POST&%2F&" + urlencode(canonical) ;
//   3. HMAC-SHA1(accessKeySecret + "&", signingString) → base64 → Signature 参数。
//
// 真实调用：text/scan：
//   POST https://green.{region}.aliyuncs.com/green/text/scan?Signature=...&AccessKeyId=...&Format=JSON&Version=2018-05-09&SignatureNonce=...&Timestamp=...&SignatureMethod=HMAC-SHA1&SignatureVersion=1.0
//
// 真值跑：YUNMAO_CHAT_MODERATION_PROVIDER=aliyun_green YUNMAO_CHAT_ALIYUN_AK=... YUNMAO_CHAT_ALIYUN_SK=... go test ...
// 决策见 ADR-0026。

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // 阿里云 POPv1 协议强制 SHA1
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// HTTPDoer 抽象网络层，便于单测注入 mock server。
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// AliyunGreenRealClient 真实 Green API 客户端。
//
// 调用方在 dev/CI 注入 `httptest.Server`，生产注入 `&http.Client{Timeout: 800ms}`。
type AliyunGreenRealClient struct {
	cfg    AliyunGreenConfig
	http   HTTPDoer
	endpoint string
	timeout time.Duration
}

// NewAliyunGreenRealClient 构造。
//
// `endpoint` 缺省 `https://green.{region}.aliyuncs.com`；可被 `cfg.Endpoint`（如存在）覆盖。
func NewAliyunGreenRealClient(cfg AliyunGreenConfig, doer HTTPDoer) *AliyunGreenRealClient {
	endpoint := "https://green." + cfg.Region + ".aliyuncs.com"
	if cfg.Endpoint != "" {
		endpoint = cfg.Endpoint
	}
	if doer == nil {
		doer = &http.Client{Timeout: 800 * time.Millisecond}
	}
	return &AliyunGreenRealClient{
		cfg: cfg, http: doer, endpoint: endpoint,
		timeout: 800 * time.Millisecond,
	}
}

// Inspect 真实调用 text/scan。
func (c *AliyunGreenRealClient) Inspect(ctx context.Context, text string) (Decision, error) {
	if c.cfg.AccessKey == "" || c.cfg.AccessSecret == "" {
		return Decision{}, errors.New("aliyun_green: missing AK/SK")
	}
	body := map[string]any{
		"tasks": []map[string]any{
			{
				"dataId":  "yunmao-chat-" + strconv.FormatInt(time.Now().UnixNano(), 10),
				"content": text,
			},
		},
		"scenes": []string{"antispam", "ad", "politics", "abuse"},
	}
	bodyBytes, _ := json.Marshal(body)

	nonce := strconv.FormatInt(time.Now().UnixNano(), 36)
	params := url.Values{}
	params.Set("Format", "JSON")
	params.Set("Version", "2018-05-09")
	params.Set("AccessKeyId", c.cfg.AccessKey)
	params.Set("SignatureMethod", "HMAC-SHA1")
	params.Set("Timestamp", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	params.Set("SignatureVersion", "1.0")
	params.Set("SignatureNonce", nonce)
	params.Set("Action", "TextScan")
	signature := signAliyunRPC("POST", params, c.cfg.AccessSecret)
	params.Set("Signature", signature)

	reqURL := c.endpoint + "/green/text/scan?" + params.Encode()
	cctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return Decision{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Decision{}, fmt.Errorf("aliyun_green: http: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return Decision{}, fmt.Errorf("aliyun_green: status=%d body=%s", resp.StatusCode, string(raw))
	}
	return parseAliyunGreenResponse(raw, text)
}

// Name returns provider 名（与 mock 共用）。
func (c *AliyunGreenRealClient) Name() string { return "aliyun_green" }

// signAliyunRPC POPv1 签名算法。
func signAliyunRPC(method string, params url.Values, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(percentEncode(k))
		sb.WriteByte('=')
		sb.WriteString(percentEncode(params.Get(k)))
	}
	stringToSign := method + "&" + percentEncode("/") + "&" + percentEncode(sb.String())
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// percentEncode 阿里云 POPv1 要求严格 RFC3986 percent-encode（spaces→%20，*→%2A，~ 保留）。
func percentEncode(s string) string {
	q := url.QueryEscape(s)
	q = strings.ReplaceAll(q, "+", "%20")
	q = strings.ReplaceAll(q, "*", "%2A")
	q = strings.ReplaceAll(q, "%7E", "~")
	return q
}

// parseAliyunGreenResponse 把 Green 返回 JSON 转 Decision。
func parseAliyunGreenResponse(raw []byte, originalText string) (Decision, error) {
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Code    int     `json:"code"`
			Msg     string  `json:"msg"`
			DataID  string  `json:"dataId"`
			Results []struct {
				Scene      string  `json:"scene"`
				Suggestion string  `json:"suggestion"` // pass/review/block
				Label      string  `json:"label"`
				Rate       float64 `json:"rate"`
			} `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Decision{}, fmt.Errorf("aliyun_green: parse: %w", err)
	}
	if resp.Code != 200 {
		return Decision{}, fmt.Errorf("aliyun_green: code=%d msg=%s", resp.Code, resp.Msg)
	}
	// 取最严重的 suggestion。
	severity := map[string]int{"pass": 0, "review": 1, "block": 2}
	worst := "pass"
	var worstLabel string
	var worstRate float64
	for _, d := range resp.Data {
		for _, r := range d.Results {
			if severity[r.Suggestion] > severity[worst] {
				worst = r.Suggestion
				worstLabel = r.Label
				worstRate = r.Rate
			}
		}
	}
	var action Action
	switch worst {
	case "block":
		action = ActionBlock
	case "review":
		action = ActionWarn
	default:
		action = ActionPass
	}
	return Decision{
		Action:      action,
		Reason:      "aliyun_green:" + worstLabel,
		Provider:    "aliyun_green",
		Score:       worstRate / 100.0,
		CleanedBody: originalText,
	}, nil
}
