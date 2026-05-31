// sensitive.go：默认敏感词过滤；本地词表。
//
// 生产路径（外接评估）：把 SensitiveFilter 接口实现替换为
// 调用 SaaS（如 Aliyun Green / Tencent CMS）的 HTTP 客户端；
// 第七轮接入。当前阶段词表仅作冒烟，避免线上脏话。
package service

import "strings"

// 本地敏感词列表（精简）。
var defaultBadWords = []string{
	"草你妈", "操你妈", "傻逼", "fuck", "shit",
}

type localFilter struct {
	words []string
}

func (l *localFilter) Check(text string) (string, bool, string) {
	lower := strings.ToLower(text)
	for _, w := range l.words {
		if strings.Contains(lower, strings.ToLower(w)) {
			return strings.ReplaceAll(text, w, strings.Repeat("*", len(w))), true, "sensitive_word"
		}
	}
	return text, false, ""
}

// DefaultSensitiveFilter 默认本地词表过滤器。
func DefaultSensitiveFilter() SensitiveFilter {
	return &localFilter{words: defaultBadWords}
}
