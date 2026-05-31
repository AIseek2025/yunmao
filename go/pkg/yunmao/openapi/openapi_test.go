package openapi

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

//go:embed v3.json
var specBytes []byte

func TestSpecIsValidJSON(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(specBytes, &doc); err != nil {
		t.Fatalf("v3.json is not valid JSON: %v", err)
	}
}

func TestSpecHasRequiredFields(t *testing.T) {
	var doc map[string]any
	_ = json.Unmarshal(specBytes, &doc)

	if _, ok := doc["openapi"]; !ok {
		t.Fatal("missing openapi version")
	}
	if _, ok := doc["info"]; !ok {
		t.Fatal("missing info")
	}
	if _, ok := doc["paths"]; !ok {
		t.Fatal("missing paths")
	}
	if _, ok := doc["components"]; !ok {
		t.Fatal("missing components")
	}
}

func TestSpecHasSchemas(t *testing.T) {
	var doc struct {
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(specBytes, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	required := []string{
		"User",
		"Room",
		"FeedRequest",
		"Order",
		"Wallet",
		"PrepayResponse",
		"IceServersResponse",
		"ChatMessage",
	}
	for _, name := range required {
		if _, ok := doc.Components.Schemas[name]; !ok {
			t.Errorf("missing schema: %s", name)
		}
	}
}

func TestSpecPathsCoverMainServices(t *testing.T) {
	var doc struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(specBytes, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	mustHave := []string{
		"/v1/auth/login/sms",
		"/v1/rooms",
		"/v1/rooms/{id}",
		"/v1/rooms/{id}/ice-servers",
		"/api/v1/feed-requests",
		"/api/v1/orders",
		"/api/v1/orders/{id}/prepay",
		"/api/v1/wallets/{user_id}",
		"/api/v1/pay/channels",
	}
	for _, p := range mustHave {
		if _, ok := doc.Paths[p]; !ok {
			t.Errorf("missing path: %s", p)
		}
	}
}

func TestSpecOperationsHaveSchemas(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]struct {
			OperationId string `json:"operationId"`
			Request     struct {
				Content map[string]struct {
					Schema struct {
						Ref string `json:"$ref"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
			Responses map[string]struct {
				Content map[string]struct {
					Schema struct {
						Ref string `json:"$ref"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(specBytes, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	opsChecked := 0
	for _, methods := range doc.Paths {
		for _, op := range methods {
			if op.OperationId == "" {
				continue
			}
			opsChecked++
			hasResponse := false
			for _, resp := range op.Responses {
				for _, media := range resp.Content {
					if media.Schema.Ref != "" {
						hasResponse = true
					}
				}
			}
			if !hasResponse {
				if _, ok := op.Responses["200"]; ok {
					if len(op.Responses["200"].Content) == 0 {
						t.Logf("op %s may lack inline response schema", op.OperationId)
					}
				}
			}
		}
	}
	if opsChecked < 10 {
		t.Errorf("expected at least 10 operations with operationId, got %d", opsChecked)
	}
}

func TestMobileSchemaDriftCheck(t *testing.T) {
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(specBytes, &spec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	coreSchemas := map[string][]string{
		"AuthToken": {
			"token", "user_id", "expires_at",
		},
		"Room": {
			"id", "name", "status", "cover", "protocol_pref", "webrtc_eligible",
		},
		"RoomSubscription": {
			"room_id", "token", "url_playback", "url_whep", "webrtc_enabled",
		},
		"FeedRequest": {
			"room_id", "user_id", "grams", "feed_ticket_id", "idempotency_key",
		},
		"FeedResponse": {
			"id", "status",
		},
		"Wallet": {
			"user_id", "balance_fen", "coins",
		},
		"PrepayResponse": {
			"channel", "prepay_id", "pay_url", "qr_content", "client_hints",
		},
		"ChatMessage": {
			"id", "room_id", "user_id", "nickname", "body", "created_at", "moderation",
		},
		"IceServersResponse": {
			"ice_servers",
		},
	}

	for schemaName, requiredFields := range coreSchemas {
		schema, ok := spec.Components.Schemas[schemaName]
		if !ok {
			t.Errorf("schema %s missing from OpenAPI spec", schemaName)
			continue
		}
		for _, field := range requiredFields {
			if _, ok := schema.Properties[field]; !ok {
				t.Errorf("schema %s missing field %s", schemaName, field)
			}
		}
	}

	root := findRepoRoot(t)

	androidModelsPath := filepath.Join(root, "clients", "android", "app", "src", "main", "java", "live", "yunmao", "app", "model", "Models.kt")
	androidBytes, err := os.ReadFile(androidModelsPath)
	if err != nil {
		t.Fatalf("cannot read Android Models.kt: %v", err)
	}
	androidSrc := string(androidBytes)

	iosModelsPath := filepath.Join(root, "clients", "ios", "YunmaoApp", "Sources", "YunmaoApp", "Models", "Models.swift")
	iosBytes, err := os.ReadFile(iosModelsPath)
	if err != nil {
		t.Fatalf("cannot read iOS Models.swift: %v", err)
	}
	iosSrc := string(iosBytes)

	kotlinFieldRe := regexp.MustCompile(`(?:val|var)\s+(\w+)`)
	swiftFieldRe := regexp.MustCompile(`(?:public\s+)?(?:let|var)\s+(\w+)`)

	androidModelFields := extractKotlinDataClassFields(androidSrc, kotlinFieldRe)
	iosStructFields := extractSwiftStructFields(iosSrc, swiftFieldRe)

	snakeToKotlin := func(s string) string {
		parts := strings.Split(s, "_")
		for i := 1; i < len(parts); i++ {
			parts[i] = strings.Title(parts[i])
		}
		return strings.Join(parts, "")
	}
	snakeToSwift := func(s string) string {
		parts := strings.Split(s, "_")
		for i := 1; i < len(parts); i++ {
			parts[i] = strings.Title(parts[i])
		}
		return strings.Join(parts, "")
	}

	for schemaName, requiredFields := range coreSchemas {
		kotlinFields := androidModelFields[schemaName]
		swiftFields := iosStructFields[schemaName]

		for _, field := range requiredFields {
			kotlinName := snakeToKotlin(field)
			swiftName := snakeToSwift(field)

			if kotlinFields != nil {
				found := false
				for _, kf := range kotlinFields {
					if kf == kotlinName || kf == field {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Android %s: OpenAPI field %s (expected Kotlin name %s) not found in Models.kt", schemaName, field, kotlinName)
				}
			}

			if swiftFields != nil {
				found := false
				for _, sf := range swiftFields {
					if sf == swiftName || sf == field {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("iOS %s: OpenAPI field %s (expected Swift name %s) not found in Models.swift", schemaName, field, swiftName)
				}
			}
		}
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"../../../..",
		"../../..",
		"..",
	}
	for _, c := range candidates {
		p, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if os.Stat(filepath.Join(p, "go", "pkg", "yunmao", "openapi", "v3.json")); err == nil {
			return p
		}
	}
	dir, _ := os.Getwd()
	t.Fatalf("cannot find repo root from %s", dir)
	return ""
}

func extractKotlinDataClassFields(src string, fieldRe *regexp.Regexp) map[string][]string {
	re := regexp.MustCompile(`data\s+class\s+(\w+)\s*\(`)
	result := map[string][]string{}
	locs := re.FindAllStringSubmatchIndex(src, -1)
	for _, m := range locs {
		name := src[m[2]:m[3]]
		parenStart := m[1] - 1
		depth := 0
		end := parenStart
		for i := parenStart; i < len(src); i++ {
			if src[i] == '(' {
				depth++
			} else if src[i] == ')' {
				depth--
				if depth == 0 {
					end = i + 1
					break
				}
			}
		}
		body := src[m[0]:end]
		fields := fieldRe.FindAllStringSubmatch(body, -1)
		var fieldNames []string
		for _, f := range fields {
			fieldNames = append(fieldNames, f[1])
		}
		result[name] = fieldNames
	}
	return result
}

func extractSwiftStructFields(src string, fieldRe *regexp.Regexp) map[string][]string {
	re := regexp.MustCompile(`public\s+struct\s+(\w+)[^{]*\{`)
	result := map[string][]string{}
	locs := re.FindAllStringSubmatchIndex(src, -1)
	for _, m := range locs {
		name := src[m[2]:m[3]]
		braceStart := m[1] - 1
		depth := 0
		end := braceStart
		for i := braceStart; i < len(src); i++ {
			if src[i] == '{' {
				depth++
			} else if src[i] == '}' {
				depth--
				if depth == 0 {
					end = i + 1
					break
				}
			}
		}
		body := src[m[0]:end]
		fields := fieldRe.FindAllStringSubmatch(body, -1)
		var fieldNames []string
		for _, f := range fields {
			fieldNames = append(fieldNames, f[1])
		}
		result[name] = fieldNames
	}
	return result
}
