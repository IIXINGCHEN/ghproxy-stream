package proxy

import (
	"errors"
	"net/url"
	"strings"
)

var errInvalidRedirectScheme = errors.New("invalid redirect scheme")

// 差距修复 P0-2 / P0-4 共享工具：跳转解析、令牌主机判定、日志脱敏。
//
// 背景（Phase 0 探明结论）：
//   - httpc.Client.Do 实际是 RoundTripper.RoundTrip，不跟随跳转（没有
//     Client.Do 的 CheckRedirect 逻辑），所以跳转只能由本仓库代码手动跟随；
//   - Touka 的 GetRequestURI 返回 RequestURI（含查询串），?token= 会进入
//     上游 URL 与日志，必须显式剥离（见 stripAuthQueryParams）。

// MaxRedirects 服务端跟随上限。GitHub Release 链通常 1～2 跳，5 跳留足余量。
const MaxRedirects = 5

// RedirectStatuses 需要服务端跟随的状态码。
var RedirectStatuses = map[int]struct{}{
	301: {},
	302: {},
	303: {},
	307: {},
	308: {},
}

// ResolveRedirect 解析 Location：相对路径按当前 URL 解析，非 http(s) 返回 nil。
func ResolveRedirect(current *url.URL, location string) (*url.URL, error) {
	next, err := url.Parse(location)
	if err != nil {
		return nil, err
	}
	next = current.ResolveReference(next)
	if next.Scheme != "http" && next.Scheme != "https" {
		return nil, errInvalidRedirectScheme
	}
	return next, nil
}

// IsTokenHost 判断主机是否可以接收 Authorization。
// 只有 github.com（含子域名，如 codeload）、api.github.com、
// raw/gist 的 githubusercontent 子域名可以；objects、release-assets、
// media 等签名 CDN 主机靠 URL 签名自认证，附带令牌无用且可能被拒绝
// 或记入 CDN 日志，必须剥离。注意 objects.githubusercontent.com 按后缀
// 确实属于 githubusercontent，但它不在可发令牌之列。
func IsTokenHost(host string) bool {
	h := strings.ToLower(host)
	if h == "github.com" || h == "api.github.com" || h == "gist.github.com" ||
		strings.HasSuffix(h, ".github.com") {
		return true
	}
	return h == "raw.githubusercontent.com" || h == "gist.githubusercontent.com"
}

// RedactURL 脱敏：只保留 scheme://host/path，查询串与 fragment 一律去掉。
// 用于所有打印 URL 的日志（签名参数 sig/jwt、令牌绝不能落盘）。
// 解析失败时返回占位符，不原样输出可能含敏感参数的字符串。
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(invalid url)"
	}
	return u.Scheme + "://" + u.Host + u.Path
}

// stripAuthQueryParams 从 URL 字符串（可带或不带 scheme，查询串可能含 token）
// 中剥离 token 与 auth_token 两个参数，其余查询参数原样保留。
// 用于 NoRouteHandler / RoutingHandler 在构造上游目标之前处理，
// 这样上游 URL 与后续日志都不再含客户端令牌；AuthPassThrough 读的是
// 原始请求的 c.Query，与这里剥离后的目标 URL 互不影响。
func stripAuthQueryParams(raw string) string {
	base, query, found := strings.Cut(raw, "?")
	if !found {
		return raw
	}
	// 查询串里可能还有 fragment，先把 fragment 摘出来最后拼回去。
	query, frag, hasFrag := strings.Cut(query, "#")
	vals, err := url.ParseQuery(query)
	if err != nil {
		// 解析失败时保守处理：整个查询串丢弃，避免令牌以残缺形式残留。
		if hasFrag {
			return base + "#" + frag
		}
		return base
	}
	vals.Del("token")
	vals.Del("auth_token")
	encoded := vals.Encode()
	out := base
	if encoded != "" {
		out += "?" + encoded
	}
	if hasFrag {
		out += "#" + frag
	}
	return out
}

// normalizeRawHost 把已废弃的 raw.github.com 归一化到 raw.githubusercontent.com。
// 仅处理前缀匹配，其余原样返回。
func normalizeRawHost(raw string) string {
	const oldPrefix = "https://raw.github.com/"
	const newPrefix = "https://raw.githubusercontent.com/"
	if strings.HasPrefix(raw, oldPrefix) {
		return newPrefix + raw[len(oldPrefix):]
	}
	return raw
}
