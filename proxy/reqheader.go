package proxy

import (
	"ghproxy/config"
	"net/http"

	"github.com/infinite-iroha/touka"
)

var (
	respHeadersToRemove = map[string]struct{}{
		"Content-Security-Policy":   {},
		"Referrer-Policy":           {},
		"Strict-Transport-Security": {},
		"X-Github-Request-Id":       {},
		"X-Timer":                   {},
		"X-Served-By":               {},
		"X-Fastly-Request-Id":       {},
		// P0-4：上游 Set-Cookie 绝不能透传给客户端，否则会把上游会话带出来。
		"Set-Cookie": {},
	}

	reqHeadersToRemove = map[string]struct{}{
		"CF-IPCountry":     {},
		"CF-RAY":           {},
		"CF-Visitor":       {},
		"CF-Connecting-IP": {},
		"CF-EW-Via":        {},
		"CDN-Loop":         {},
		"Upgrade":          {},
		"Connection":       {},
		"Accept-Encoding":  {},
		// P0-4：客户端 Cookie 绝不能原样发给上游 GitHub。
		"Cookie": {},
	}

	cloneHeadersToRemove = map[string]struct{}{
		"CF-IPCountry":     {},
		"CF-RAY":           {},
		"CF-Visitor":       {},
		"CF-Connecting-IP": {},
		"CF-EW-Via":        {},
		"CDN-Loop":         {},
		// P0-4 + P1-1：clone 路径之前没删 Cookie 与 Accept-Encoding，
		// 与非 clone 路径不一致；Cookie 不应上送，Accept-Encoding 置空
		// 才能保证 Range / Content-Length 对得上。
		"Cookie":          {},
		"Accept-Encoding": {},
	}
)

// 预定义headers
var (
	defaultHeaders = map[string]string{
		"Accept":            "*/*",
		"Accept-Encoding":   "",
		"Transfer-Encoding": "chunked",
		"User-Agent":        "GHProxy/1.0",
	}
)

// copyHeader 将所有头部从 src 复制到 dst。
// 对于多值头部，它会为每个值调用 Add，从而保留所有值。
func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func setRequestHeaders(c *touka.Context, req *http.Request, cfg *config.Config, matcher string) {
	if matcher == "raw" && cfg.Httpc.UseCustomRawHeaders {
		// 使用预定义Header
		for key, value := range defaultHeaders {
			req.Header.Set(key, value)
		}
	} else if matcher == "clone" {
		copyHeader(req.Header, c.Request.Header)
		for key := range cloneHeadersToRemove {
			req.Header.Del(key)
		}
	} else {
		copyHeader(req.Header, c.Request.Header)
		for key := range reqHeadersToRemove {
			req.Header.Del(key)
		}
	}
}
