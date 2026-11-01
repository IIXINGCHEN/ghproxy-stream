package proxy

import (
	"context"
	"fmt"
	"ghproxy/config"
	"net/http"
	"strconv"

	"github.com/WJQSERVER-STUDIO/go-utils/limitreader"
	"github.com/infinite-iroha/touka"
)

// GitReq git clone 代理（bypass 直连 GitHub，或 cache 走 SmartGit）。
//
// 差距修复：
//   - 301 直连改为 413（P0-1），分块体用计数 reader 断流（P1-3）；
//   - GET/HEAD 跳转服务端跟随（最多 MaxRedirects 跳，相对路径解析），
//     POST 跳转改写 Location 回代理地址后回传（P0-2）；
//   - 响应头清理改为对客户端响应头生效（旧代码删的是上游 resp.Header，
//     删完拷贝已完成，等于没删），并补齐 Set-Cookie（P0-4）；
//   - CORS 用统一取值，"" 不再映射成 "*"。cache 模式下目标是用户配置的
//     SmartGit 内服，Authorization 不按 GitHub 令牌主机规则剥离。
func GitReq(ctx context.Context, c *touka.Context, u string, cfg *config.Config, mode string) {
	var resp *http.Response

	reqBodyReader, err := c.GetReqBodyBuffer()
	if err != nil {
		HandleError(c, fmt.Sprintf("Failed to read request body: %v", err))
		return
	}

	if cfg.GitClone.Mode == "cache" {
		userPath, repoPath, remainingPath, queryParams, err := extractParts(u)
		if err != nil {
			HandleError(c, fmt.Sprintf("Failed to extract parts from URL: %v", err))
			return
		}
		var paramStr string
		if len(queryParams) > 0 {
			paramStr = "?" + queryParams.Encode()
		}
		u = cfg.GitClone.SmartGitAddr + userPath + repoPath + remainingPath + paramStr
	}

	httpClient := client
	if cfg.GitClone.Mode == "cache" {
		httpClient = gitclient
	}

	isGetHead := c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead
	currentURL := u
	hops := 0

	for {
		rb := httpClient.NewRequestBuilder(c.Request.Method, currentURL)
		rb.NoDefaultHeaders()
		rb.SetBody(reqBodyReader)
		rb.WithContext(ctx)

		req, err := rb.Build()
		if err != nil {
			HandleError(c, fmt.Sprintf("Failed to create request: %v", err))
			return
		}

		setRequestHeaders(c, req, cfg, "clone")
		AuthPassThrough(c, cfg, req)
		// bypass 模式下，非令牌主机（签名 CDN）剥离 Authorization；
		// cache 模式目标是受信 SmartGit 内服，不剥离。
		if cfg.GitClone.Mode != "cache" && !IsTokenHost(req.URL.Host) {
			req.Header.Del("Authorization")
		}

		resp, err = httpClient.Do(req)
		if err != nil {
			HandleError(c, fmt.Sprintf("Failed to send request: %v", err))
			return
		}

		if _, isRedirect := RedirectStatuses[resp.StatusCode]; isRedirect {
			location := resp.Header.Get("Location")
			if location == "" || !isGetHead {
				if location != "" && !isGetHead {
					if resolved, rerr := ResolveRedirect(req.URL, location); rerr == nil {
						resp.Header.Set("Location", "/"+resolved.String())
					}
				}
				break
			}
			if hops >= MaxRedirects {
				resp.Body.Close()
				c.Warnf("Git redirect hop limit exceeded: %s", RedactURL(currentURL))
				ErrorPage(c, NewErrorWithStatusLookup(502, "Too many redirects"))
				return
			}
			next, rerr := ResolveRedirect(req.URL, location)
			if rerr != nil {
				resp.Body.Close()
				ErrorPage(c, NewErrorWithStatusLookup(502, "Invalid redirect Location"))
				return
			}
			resp.Body.Close()
			currentURL = next.String()
			hops++
			continue
		}
		break
	}
	defer resp.Body.Close()

	// P0-1：已知 Content-Length 超限直接 413，不再 301 到上游。
	capBytes := SizeLimitBytes(cfg.Server.SizeLimit)
	contentLength := resp.Header.Get("Content-Length")
	if contentLength != "" {
		size, err := strconv.Atoi(contentLength)
		if err != nil {
			c.Warnf("%s %s %s %s %s Content-Length header is not a valid integer: %v", c.ClientIP(), c.Request.Method, c.Request.URL.Path, c.UserAgent(), c.Request.Proto, err)
		}
		if err == nil && capBytes > 0 && int64(size) > capBytes {
			c.Warnf("%s %s %s %s %s Size-Limit-Exceeded: %d > %d url=%s", c.ClientIP(), c.Request.Method, c.Request.URL.Path, c.UserAgent(), c.Request.Proto, size, capBytes, RedactURL(currentURL))
			ErrorPage(c, NewErrorWithStatusLookup(413, "Payload Too Large (Size Limit Exceeded)"))
			return
		}
	}

	c.SetHeaders(resp.Header)
	// P0-4：对客户端响应头清理（旧代码误删上游头对象，等于没删），
	// 补齐 respHeadersToRemove（含 Set-Cookie）。
	for key := range respHeadersToRemove {
		c.DelHeader(key)
	}

	if origin := corsAllowOrigin(cfg.Server.Cors); origin != "" {
		c.Header("Access-Control-Allow-Origin", origin)
	}

	c.Status(resp.StatusCode)
	if cfg.GitClone.Mode == "cache" {
		c.SetHeader("Cache-Control", "no-store, no-cache, must-revalidate")
		c.SetHeader("Pragma", "no-cache")
		c.SetHeader("Expires", "0")
	}

	bodyReader := resp.Body

	if cfg.RateLimit.BandwidthLimit.Enabled {
		bodyReader = limitreader.NewRateLimitedReader(bodyReader, bandwidthLimit, int(bandwidthBurst), ctx)
	}

	if capBytes > 0 {
		bodyReader = newCountingReader(bodyReader, capBytes).withExceeded(func(n int64) {
			c.Warnf("%s %s %s Size-Limit-Exceeded during streaming: url=%s", c.ClientIP(), c.Request.Method, c.Request.URL.Path, RedactURL(currentURL))
		})
	}

	c.SetBodyStream(bodyReader, -1)
}
