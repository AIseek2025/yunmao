package pay

import (
	"fmt"
	"strings"
)

type CredentialStatus string

const (
	CredentialMissing CredentialStatus = "missing"
	CredentialPartial CredentialStatus = "partial"
	CredentialMock    CredentialStatus = "mock"
	CredentialSandbox CredentialStatus = "sandbox"
	CredentialReady   CredentialStatus = "ready"
)

type CredentialCheck struct {
	Channel Channel
	Field   string
	Status  CredentialStatus
	Detail  string
}

type CredentialReadiness struct {
	Checks []CredentialCheck
}

func (cr *CredentialReadiness) AllReady() bool {
	for _, c := range cr.Checks {
		if c.Status != CredentialReady && c.Status != CredentialMock && c.Status != CredentialSandbox {
			return false
		}
	}
	return true
}

func (cr *CredentialReadiness) HasRealMode() bool {
	for _, c := range cr.Checks {
		if c.Status == CredentialReady || c.Status == CredentialSandbox {
			return true
		}
	}
	return false
}

func (cr *CredentialReadiness) Summary() string {
	var lines []string
	for _, c := range cr.Checks {
		lines = append(lines, fmt.Sprintf("  %s/%s: %s (%s)", c.Channel, c.Field, c.Status, c.Detail))
	}
	return strings.Join(lines, "\n")
}

func CheckWeChatCredentials(cfg WeChatConfig) []CredentialCheck {
	var checks []CredentialCheck
	ch := ChannelWeChat

	if cfg.MockMode {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "mode", Status: CredentialMock, Detail: "MockMode=true"})
		return checks
	}

	if cfg.MchID == "" {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "mch_id", Status: CredentialMissing, Detail: "YUNMAO_WECHAT_MCH_ID not set"})
	} else {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "mch_id", Status: CredentialReady, Detail: "set"})
	}

	if cfg.APIv3Key == "" {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "api_v3_key", Status: CredentialMissing, Detail: "YUNMAO_WECHAT_APIV3_KEY not set"})
	} else {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "api_v3_key", Status: CredentialReady, Detail: "set"})
	}

	if cfg.SerialNo == "" {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "serial_no", Status: CredentialMissing, Detail: "YUNMAO_WECHAT_SERIAL_NO not set"})
	} else {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "serial_no", Status: CredentialReady, Detail: "set"})
	}

	if len(cfg.APIClientKey) == 0 {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "api_client_key", Status: CredentialMissing, Detail: "YUNMAO_WECHAT_API_CLIENT_KEY_PATH not set"})
	} else {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "api_client_key", Status: CredentialReady, Detail: fmt.Sprintf("%d bytes", len(cfg.APIClientKey))})
	}

	if len(cfg.PlatformPublicKey) == 0 {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "platform_public_key", Status: CredentialPartial, Detail: "optional for webhook verify; not set"})
	} else {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "platform_public_key", Status: CredentialReady, Detail: fmt.Sprintf("%d bytes", len(cfg.PlatformPublicKey))})
	}

	if cfg.SandboxBase != "" {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "sandbox", Status: CredentialSandbox, Detail: cfg.SandboxBase})
	}

	return checks
}

func CheckAlipayCredentials(cfg AlipayConfig) []CredentialCheck {
	var checks []CredentialCheck
	ch := ChannelAlipay

	if cfg.MockMode {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "mode", Status: CredentialMock, Detail: "MockMode=true"})
		return checks
	}

	if cfg.AppID == "" {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "app_id", Status: CredentialMissing, Detail: "YUNMAO_ALIPAY_APP_ID not set"})
	} else {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "app_id", Status: CredentialReady, Detail: "set"})
	}

	if len(cfg.PrivateKeyPEM) == 0 {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "merchant_private_key", Status: CredentialMissing, Detail: "YUNMAO_ALIPAY_PRIVATE_KEY_PEM_PATH not set"})
	} else {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "merchant_private_key", Status: CredentialReady, Detail: fmt.Sprintf("%d bytes", len(cfg.PrivateKeyPEM))})
	}

	if len(cfg.AlipayPublicKey) == 0 {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "alipay_public_key", Status: CredentialPartial, Detail: "optional for notify verify; not set"})
	} else {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "alipay_public_key", Status: CredentialReady, Detail: fmt.Sprintf("%d bytes", len(cfg.AlipayPublicKey))})
	}

	if cfg.SandboxBase != "" {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "sandbox", Status: CredentialSandbox, Detail: cfg.SandboxBase})
	}

	return checks
}

func CheckAppleIAPCredentials(cfg AppleIAPConfig) []CredentialCheck {
	var checks []CredentialCheck
	ch := ChannelAppleIAP

	if cfg.MockMode {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "mode", Status: CredentialMock, Detail: "MockMode=true"})
		return checks
	}

	if cfg.BundleID == "" {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "bundle_id", Status: CredentialMissing, Detail: "YUNMAO_APPLE_BUNDLE_ID not set"})
	} else {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "bundle_id", Status: CredentialReady, Detail: cfg.BundleID})
	}

	if cfg.AppAppleID == "" {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "app_apple_id", Status: CredentialPartial, Detail: "YUNMAO_APPLE_APP_APPLE_ID not set (optional for server-side receipt validation)"})
	} else {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "app_apple_id", Status: CredentialReady, Detail: cfg.AppAppleID})
	}

	if len(cfg.TrustedRoots) == 0 {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "trusted_roots", Status: CredentialPartial, Detail: "Apple Root CA PEM not set; JWS x5c chain validation will be skipped"})
	} else {
		checks = append(checks, CredentialCheck{Channel: ch, Field: "trusted_roots", Status: CredentialReady, Detail: fmt.Sprintf("%d bytes", len(cfg.TrustedRoots))})
	}

	return checks
}

func CheckAllCredentials(wc WeChatConfig, ac AlipayConfig, ic AppleIAPConfig) *CredentialReadiness {
	cr := &CredentialReadiness{}
	cr.Checks = append(cr.Checks, CheckWeChatCredentials(wc)...)
	cr.Checks = append(cr.Checks, CheckAlipayCredentials(ac)...)
	cr.Checks = append(cr.Checks, CheckAppleIAPCredentials(ic)...)
	return cr
}
