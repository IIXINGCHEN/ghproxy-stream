package proxy

import (
	"context"
	"fmt"
	"ghproxy/config"
	"io"
	"net/http"
	"strconv"

	"github.com/WJQSERVER-STUDIO/go-utils/limitreader"
	"github.com/infinite-iroha/touka"
)

// ChunkedProxyRequest 流式代理（releases/raw/gist/api 等）。
//
// 差距修复后的行为（P0-1/P0-2/P0-3/P0-4/P1-1）：
//   - GET/HEAD：服务端跟随 301/302/303/307/308，最多 MaxRedirects 跳，
//     相对 Location 按当前 URL 解析，跨主机跳时剥离 Authorization；
//   - 非 GET/HEAD：不跟随（body 流不可重读，旧递归会发空 body），
//     收到跳转时把 Location 改写回代理地址（"/"+绝对 URL），客户端只
//     见代理主机；
//   - 不再有 125MB 的 301 直连：Content-Length 超限直接 413，
//     无 Content-Length 的分块体用计数 reader 在上限处断流；
//   - 令牌池：GET/HEAD 遇 403/429 且本次用的是池令牌时，冷却该令牌
//     并换下一个重试；客户端自带令牌时池不介入；
//   - Range：原样透传（请求头整批拷贝、状态码原样回传），绝不由 200
//     合成 206；Range 请求跳过 Shell Editor（改写正文会破坏分段）。
func ChunkedProxyRequest(ctx context.Context, c *touka.Context, u string, cfg *config.Config, matcher string) {
	var (
		req  *http.Request
		resp *http.Response
		err  error
	)

	go func() {
		<-ctx.Done()
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if req != nil && req.Body != nil {
			req.Body.Close()
		}
	}()

	isGetHead := c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead
	currentURL := u
	hops := 0
	tokenRetries := 0

	for {
		rb := client.NewRequestBuilder(c.Request.Method, currentURL)
		rb.NoDefaultHeaders()
		if !isGetHead {
			rb.SetBody(c.Request.Body)
		}
		rb.WithContext(ctx)

		req, err = rb.Build()
		if err != nil {
			HandleError(c, fmt.Sprintf("Failed to create request: %v", err))
			return
		}

		setRequestHeaders(c, req, cfg, matcher)
		poolToken := AuthPassThrough(c, cfg, req)
		// P0-4/P0-2：目标不是可发令牌主机时，客户端令牌与池令牌一律剥离。
		if !IsTokenHost(req.URL.Host) {
			req.Header.Del("Authorization")
			poolToken = ""
		}

		resp, err = client.Do(req)
		if err != nil {
			HandleError(c, fmt.Sprintf("Failed to send request: %v", err))
			return
		}

		// 错误处理(404)：最终响应才到这里，中间跳转已在下面处理。
		if resp.StatusCode == 404 {
			resp.Body.Close()
			ErrorPage(c, NewErrorWithStatusLookup(404, "Page Not Found (From Github)"))
			return
		}

		// 令牌池冷却重试：仅 GET/HEAD、仅池令牌、仅可发令牌主机。
		if isGetHead && (resp.StatusCode == 403 || resp.StatusCode == 429) &&
			poolToken != "" && IsTokenHost(req.URL.Host) &&
			tokenRetries < tokenPool.Len() {
			tokenRetries++
			cooldown := CooldownFromHeaders(resp.Header)
			tokenPool.Cool(poolToken, cooldown)
			c.Warnf("Token cooldown: %s %s status=%d cooldown=%s", c.Request.Method, RedactURL(currentURL), resp.StatusCode, cooldown)
			resp.Body.Close()
			continue // 同一 URL，下一轮 Pick 会自动跳过已冷却令牌
		}

		// 处理跳转。
		if _, isRedirect := RedirectStatuses[resp.StatusCode]; isRedirect {
			location := resp.Header.Get("Location")
			if location == "" {
				break // 无 Location 的跳转状态按最终响应原样回传
			}
			if !isGetHead {
				// 非 GET/HEAD 不跟随：改写 Location 回代理地址后按最终响应回传。
				if resolved, rerr := ResolveRedirect(req.URL, location); rerr == nil {
					resp.Header.Set("Location", "/"+resolved.String())
				}
				break
			}
			if hops >= MaxRedirects {
				resp.Body.Close()
				c.Warnf("Redirect hop limit exceeded: %s", RedactURL(currentURL))
				ErrorPage(c, NewErrorWithStatusLookup(502, "Too many redirects"))
				return
			}
			next, rerr := ResolveRedirect(req.URL, location)
			if rerr != nil {
				resp.Body.Close()
				c.Warnf("Invalid redirect Location: %s -> %s", RedactURL(currentURL), RedactURL(location))
				ErrorPage(c, NewErrorWithStatusLookup(502, "Invalid redirect Location"))
				return
			}
			c.Infof("Internal Redirecting to %s", RedactURL(next.String()))
			resp.Body.Close()
			currentURL = next.String()
			hops++
			continue
		}
		break
	}

	// 响应体大小限制（P0-1）：已知 Content-Length 超限直接 413，
	// 绝不再 301 到上游签名 URL；未知长度的走下面计数 reader。
	capBytes := SizeLimitBytes(cfg.Server.SizeLimit)
	var (
		bodySize      int64
		contentLength string
	)
	contentLength = resp.Header.Get("Content-Length")
	if contentLength != "" {
		if n, aerr := strconv.ParseInt(contentLength, 10, 64); aerr == nil {
			bodySize = n
		} else {
			c.Warnf("%s %s %s %s %s Content-Length header is not a valid integer: %v", c.ClientIP(), c.Request.Method, c.Request.URL.Path, c.UserAgent(), c.Request.Proto, aerr)
			bodySize = -1
		}
		if bodySize >= 0 && capBytes > 0 && bodySize > capBytes {
			resp.Body.Close()
			c.Warnf("%s %s %s %s %s Size-Limit-Exceeded: %d > %d url=%s", c.ClientIP(), c.Request.Method, c.Request.URL.Path, c.UserAgent(), c.Request.Proto, bodySize, capBytes, RedactURL(currentURL))
			ErrorPage(c, NewErrorWithStatusLookup(413, "Payload Too Large (Size Limit Exceeded)"))
			return
		}
	}

	// 复制响应头，排除需要移除的 header（含 Set-Cookie，见 reqheader.go）。
	c.SetHeaders(resp.Header)
	for key := range respHeadersToRemove {
		c.DelHeader(key)
	}

	// P2-1：CORS 统一取值（"" 不再映射成 "*"）。
	if origin := corsAllowOrigin(cfg.Server.Cors); origin != "" {
		c.Header("Access-Control-Allow-Origin", origin)
	}

	c.Status(resp.StatusCode)

	bodyReader := resp.Body

	if cfg.RateLimit.BandwidthLimit.Enabled {
		bodyReader = limitreader.NewRateLimitedReader(bodyReader, bandwidthLimit, int(bandwidthBurst), ctx)
	}

	// P1-3：分块体（无 Content-Length）也受上限约束，超限时断流。
	var counter *countingReader
	if capBytes > 0 {
		counter = newCountingReader(bodyReader, capBytes)
		bodyReader = counter
	}

	defer bodyReader.Close()

	hasRange := c.Request.Header.Get("Range") != ""

	if MatcherShell(u) && matchString(matcher) && cfg.Shell.Editor && !hasRange {
		c.Debugf("Use Shell Editor: %s %s %s %s %s", c.ClientIP(), c.Request.Method, u, c.UserAgent(), c.Request.Proto)
		c.DelHeader("Content-Length")
		c.DelHeader("Content-Encoding")

		var reader io.Reader
		reader, _, err = processLinks(bodyReader, c.Request.Host, cfg, c)
		c.WriteStream(reader)
		if err != nil {
			c.Errorf("%s %s %s %s %s Failed to copy response body: %v", c.ClientIP(), c.Request.Method, u, c.UserAgent(), c.Request.Proto, err)
			ErrorPage(c, NewErrorWithStatusLookup(500, fmt.Sprintf("Failed to copy response body: %v", err)))
			return
		}
	} else {
		// P1-1：Range/206 显式透传——状态码与 Content-Range 已由
		// 上面 SetHeaders + Status 原样回传，这里不再对状态码做任何合成；
		// 请求侧已删 Accept-Encoding，上游不会返回 gzip 分段，
		// Content-Length 与分段字节数才对得上。
		if contentLength != "" {
			c.SetHeader("Content-Length", contentLength)
		}
		_, werr := c.WriteStream(bodyReader)
		if werr != nil && counter != nil && counter.Exceeded() {
			c.Warnf("%s %s %s Size-Limit-Exceeded during streaming: url=%s", c.ClientIP(), c.Request.Method, c.Request.URL.Path, RedactURL(currentURL))
		}
	}
}
