// Package ids 提供 yunmao 平台统一 ID 类型（与 Rust 的 yunmao-common::id 对齐）。
//
// 形如 `usr_01HZX...`，前缀含义见 docs/finalproductplanning/11 第 7 节。
package ids

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// Prefix 列出全部支持前缀。
type Prefix string

const (
	PrefixUser    Prefix = "usr"
	PrefixCat     Prefix = "cat"
	PrefixRoom    Prefix = "room"
	PrefixDevice  Prefix = "dev"
	PrefixFeed    Prefix = "feed"
	PrefixCmd     Prefix = "cmd"
	PrefixOrder   Prefix = "ord"
	PrefixStream  Prefix = "stm"
	PrefixSession Prefix = "sess"
	// 第六轮新增：弹幕消息 ID。
	PrefixChatMessage Prefix = "msg"
	// 第六轮新增：钱包冻结记录 ID（与 Order 区分）。
	PrefixWalletHold Prefix = "hold"
)

func (p Prefix) Valid() bool {
	switch p {
	case PrefixUser, PrefixCat, PrefixRoom, PrefixDevice,
		PrefixFeed, PrefixCmd, PrefixOrder, PrefixStream, PrefixSession,
		PrefixChatMessage, PrefixWalletHold:
		return true
	}
	return false
}

// New 生成一个新的领域 ID。
func New(p Prefix) string {
	if !p.Valid() {
		panic(fmt.Sprintf("invalid id prefix: %q", p))
	}
	id := ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader)
	return fmt.Sprintf("%s_%s", p, id.String())
}

// Parse 校验 ID 格式并返回 prefix。
func Parse(s string) (Prefix, ulid.ULID, error) {
	idx := strings.IndexByte(s, '_')
	if idx <= 0 {
		return "", ulid.ULID{}, errors.New("missing prefix separator")
	}
	p := Prefix(s[:idx])
	if !p.Valid() {
		return "", ulid.ULID{}, fmt.Errorf("unknown prefix %q", p)
	}
	id, err := ulid.Parse(s[idx+1:])
	if err != nil {
		return "", ulid.ULID{}, err
	}
	return p, id, nil
}
