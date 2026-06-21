package appstore

import (
	"fmt"
	"net/url"
	"strings"
)

// normalizeAuthEndpoint 兼容 Apple Bag 中新旧两种认证地址位置。
// 2026 年 6 月起 native 端点必须使用 /fast/ 且保留末尾斜杠，否则 Apple 会返回空响应或 HTML 重定向。
func normalizeAuthEndpoint(endpoints ...string) string {
	for _, endpoint := range endpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			continue
		}

		if normalized := normalizeNativeAuthEndpoint(endpoint); normalized != "" {
			return normalized
		}

		return endpoint
	}

	return fmt.Sprintf("https://%s%s", PrivateAuthDomain, PrivateAuthPathNative)
}

func normalizeNativeAuthEndpoint(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || !strings.EqualFold(parsed.Hostname(), PrivateAuthDomain) {
		return ""
	}

	path := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(path, "/fast") {
		path += "/fast"
	}

	parsed.Path = path + "/"

	return parsed.String()
}
