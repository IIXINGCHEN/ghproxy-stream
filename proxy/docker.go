package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"ghproxy/config"
	"ghproxy/weakcache"

	"github.com/WJQSERVER-STUDIO/go-utils/limitreader"
	"github.com/go-json-experiment/json"
	"github.com/infinite-iroha/touka"
)

var (
	dockerhubTarget = "registry-1.docker.io"
	ghcrTarget      = "ghcr.io"
)

// cache 用于存储认证令牌, 避免重复获取
var cache *weakcache.Cache[string]

// imageInfo 结构体用于存储镜像的相关信息
type imageInfo struct {
	User  string
	Repo  string
	Image string
}

// InitWeakCache 初始化弱引用缓存
func InitWeakCache() *weakcache.Cache[string] {
	// 使用默认过期时间和容量为100创建一个新的弱引用缓存
	cache = weakcache.NewCache[string](weakcache.DefaultExpiration, 100)
	return cache
}

var (
	authEndpoint = "/"
	passTypeMap  = map[string]struct{}{
		"manifests": {},
		"blobs":     {},
		"tags":      {},
		"index":     {},
	}
)

// 处理路径各种情况
func OciWithImageRouting(cfg *config.Config) touka.HandlerFunc {
	return func(c *touka.Context) {
		if !cfg.Docker.Enabled {
			ErrorPage(c, NewErrorWithStatusLookup(403, "Docker proxy is not enabled"))
			return
		}
		var (
			p1               string
			p2               string
			p3               string
			p4               string
			target           string
			user             string
			repo             string
			extpath          string
			p1IsTarget       bool
			ignorep3         bool
			imageNameForAuth string
			finalreqUrl      string
			iInfo            *imageInfo
		)
		ociPath := c.Param("path")
		if ociPath == authEndpoint {
			emptyJSON := "{}"
			c.Header("Content-Type", "application/json")
			c.Header("Content-Length", fmt.Sprint(len(emptyJSON)))

			c.Header("Docker-Distribution-API-Version", "registry/2.0")

			c.Status(200)
			c.Writer.Write([]byte(emptyJSON))
			return
		}

		// 根据/分割 /:target/:user/:repo/*ext
		ociPath = ociPath[1:]
		i := strings.IndexByte(ociPath, '/')
		if i <= 0 {
			ErrorPage(c, NewErrorWithStatusLookup(404, "Not Found"))
			return
		}
		p1 = ociPath[:i]

		// 开始判断p1是否为target
		if strings.Contains(p1, ".") || strings.Contains(p1, ":") {
			p1IsTarget = true
			if p1 == "docker.io" {
				target = dockerhubTarget
			} else {
				target = p1
			}
		} else {
			switch cfg.Docker.Target {
			case "ghcr":
				target = ghcrTarget
			case "dockerhub":
				target = dockerhubTarget
			case "":
				ErrorPage(c, NewErrorWithStatusLookup(500, "Default Docker Target is not configured in config file"))
				return
			default:
				target = cfg.Docker.Target
			}
		}

		ociPath = ociPath[i+1:]
		i = strings.IndexByte(ociPath, '/')
		if i <= 0 {
			ErrorPage(c, NewErrorWithStatusLookup(404, "Not Found"))
			return
		}
		p2 = ociPath[:i]
		ociPath = ociPath[i+1:]

		// 若p2和passTypeMap匹配
		if !p1IsTarget {
			if _, ok := passTypeMap[p2]; ok {
				ignorep3 = true
				switch cfg.Docker.Target {
				case "ghcr":
					target = ghcrTarget
				case "dockerhub":
					target = dockerhubTarget
				case "":
					ErrorPage(c, NewErrorWithStatusLookup(500, "Default Docker Target is not configured in config file"))
					return
				default:
					target = cfg.Docker.Target
				}
				user = "library"
				repo = p1
				extpath = "/" + p2 + "/" + ociPath
			}
		}

		if !ignorep3 {
			i = strings.IndexByte(ociPath, '/')
			if i <= 0 {
				ErrorPage(c, NewErrorWithStatusLookup(404, "Not Found"))
				return
			}
			p3 = ociPath[:i]

			ociPath = ociPath[i+1:]
			p4 = ociPath

			if p1IsTarget {
				if _, ok := passTypeMap[p3]; ok {
					user = "library"
					repo = p2
					extpath = "/" + p3 + "/" + p4
				} else {
					user = p2
					repo = p3
					extpath = "/" + p4
				}
			} else {
				switch cfg.Docker.Target {
				case "ghcr":
					target = ghcrTarget
				case "dockerhub":
					target = dockerhubTarget
				case "":
					ErrorPage(c, NewErrorWithStatusLookup(500, "Default Docker Target is not configured in config file"))
					return
				default:
					target = cfg.Docker.Target
				}
				user = p1
				repo = p2
				extpath = "/" + p3 + "/" + p4
			}
		}

		imageNameForAuth = user + "/" + repo
		finalreqUrl = "https://" + target + "/v2/" + imageNameForAuth + extpath
		if query := c.GetReqQueryString(); query != "" {
			finalreqUrl += "?" + query
		}

		iInfo = &imageInfo{
			User:  user,
			Repo:  repo,
			Image: imageNameForAuth,
		}

		GhcrRequest(c.Request.Context(), c, finalreqUrl, iInfo, cfg, target)
	}
}

// GhcrRequest 执行对Docker注册表的HTTP请求, 处理认证和重定向
func GhcrRequest(ctx context.Context, c *touka.Context, u string, image *imageInfo, cfg *config.Config, target string) {
	var (
		method string
		req    *http.Request
		resp   *http.Response
		err    error
	)

	method = c.Request.Method
	ghcrclient := c.GetHTTPC()
	bodyByte, err := c.GetReqBodyFull()
	if err != nil {
		HandleError(c, fmt.Sprintf("Failed to read request body: %v", err))
		return
	}

	// 构建初始请求
	rb := ghcrclient.NewRequestBuilder(method, u)
	rb.NoDefaultHeaders()                 // 不使用默认头部, 以便完全控制
	rb.SetBody(bytes.NewBuffer(bodyByte)) // 设置请求体
	rb.WithContext(ctx)                   // 设置请求上下文

	req, err = rb.Build()
	if err != nil {
		HandleError(c, fmt.Sprintf("Failed to create request: %v", err))
		return
	}

	// 复制客户端请求的头部到代理请求
	copyHeader(c.Request.Header, req.Header)

	// 确保 Accept 头部被正确设置
	if acceptHeader, ok := c.Request.Header["Accept"]; ok {
		req.Header["Accept"] = acceptHeader
	}

	// 设置 Host 头部为上游目标
	req.Header.Set("Host", target)

	// 尝试从缓存中获取并使用认证令牌
	if image != nil && image.Image != "" {
		token, exist := cache.Get(image.Image)
		if exist {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	// 发送初始请求
	resp, err = ghcrclient.Do(req)
	if err != nil {
		HandleError(c, fmt.Sprintf("Failed to send request: %v", err))
		return
	}

	// 处理 401 Unauthorized 或 404 Not Found 响应, 尝试重新认证并重试
	if resp.StatusCode == 401 || resp.StatusCode == 404 {
		// 对于 /v2/ 的请求不进行重试, 因为它通常用于发现认证端点
		shouldRetry := string(c.GetRequestURIPath()) != "/v2/"
		originalStatusCode := resp.StatusCode
		c.Debugf("Initial request failed with status %d. Retry eligibility: %t", originalStatusCode, shouldRetry)

		if shouldRetry {
			if image == nil || image.Image == "" {
				_ = resp.Body.Close() // 终止流程, 关闭当前响应体
				ErrorPage(c, NewErrorWithStatusLookup(originalStatusCode, "Unauthorized"))
				return
			}
			// 获取新的认证令牌
			token := ChallengeReq(target, image, ctx, c)

			if token != "" {
				c.Debugf("Successfully obtained auth token. Retrying request.")
				_ = resp.Body.Close() // 在发起重试请求前, 关闭旧的响应体

				// 更新kv
				c.Debugf("Update Cache Token: %s", token)
				cache.Put(image.Image, token)

				// 重新构建并发送请求
				rb_retry := ghcrclient.NewRequestBuilder(method, u)
				rb_retry.NoDefaultHeaders()
				rb_retry.SetBody(bytes.NewBuffer(bodyByte))
				rb_retry.WithContext(ctx)

				req_retry, err_retry := rb_retry.Build()
				if err_retry != nil {
					HandleError(c, fmt.Sprintf("Failed to create retry request: %v", err_retry))
					return
				}

				copyHeader(c.Request.Header, req_retry.Header) // 复制原始头部
				if acceptHeader, ok := c.Request.Header["Accept"]; ok {
					req_retry.Header["Accept"] = acceptHeader
				}

				req_retry.Header.Set("Host", target)                   // 设置 Host 头部
				req_retry.Header.Set("Authorization", "Bearer "+token) // 使用新令牌

				c.Debugf("Executing retry request. Method: %s, URL: %s", req_retry.Method, req_retry.URL.String())

				resp_retry, err_retry := ghcrclient.Do(req_retry)
				if err_retry != nil {
					HandleError(c, fmt.Sprintf("Failed to send retry request: %v", err_retry))
					return
				}
				c.Debugf("Retry request completed with status code: %d", resp_retry.StatusCode)
				resp = resp_retry // 更新响应为重试后的响应
			} else {
				c.Warnf("Failed to obtain auth token. Cannot retry.")
				// 获取令牌失败, 将继续处理原始的401/404响应, 其响应体仍然打开
			}
		}
	}

	// P0-2/P2-3：服务端跟随全部跳转状态（301/302/303/307/308），
	// 最多 MaxRedirects 跳，相对 Location 按当前 URL 解析；
	// 跳转请求只带 User-Agent，不带 Authorization（签名 CDN 靠 URL 签名，
	// 且 Docker 401 重试逻辑保持不变，不在此处改动）。
	for hops := 0; ; hops++ {
		if _, isRedirect := RedirectStatuses[resp.StatusCode]; !isRedirect {
			break
		}
		location := resp.Header.Get("Location")
		if location == "" {
			_ = resp.Body.Close()
			HandleError(c, "Redirect response missing Location header")
			return
		}
		if hops >= MaxRedirects {
			_ = resp.Body.Close()
			c.Warnf("Docker redirect hop limit exceeded")
			ErrorPage(c, NewErrorWithStatusLookup(502, "Too many redirects"))
			return
		}
		if resp.Request == nil || resp.Request.URL == nil {
			_ = resp.Body.Close()
			HandleError(c, "Redirect response missing request URL")
			return
		}
		redirectURL, err := ResolveRedirect(resp.Request.URL, location)
		if err != nil {
			_ = resp.Body.Close()
			HandleError(c, fmt.Sprintf("Failed to parse redirect location: %v", err))
			return
		}
		c.Debugf("Handling redirect. Status: %d, Final Location: %s", resp.StatusCode, RedactURL(redirectURL.String()))
		_ = resp.Body.Close()

		redirectReq, err := http.NewRequestWithContext(ctx, "GET", redirectURL.String(), nil)
		if err != nil {
			HandleError(c, fmt.Sprintf("Failed to create redirect request: %v", err))
			return
		}
		redirectReq.Header.Set("User-Agent", c.Request.UserAgent())

		c.Debugf("Executing redirect request to: %s", RedactURL(redirectURL.String()))
		redirectResp, err := ghcrclient.Do(redirectReq)
		if err != nil {
			HandleError(c, fmt.Sprintf("Failed to execute redirect request to %s: %v", RedactURL(redirectURL.String()), err))
			return
		}
		c.Debugf("Redirect request to %s completed with status %d", RedactURL(redirectURL.String()), redirectResp.StatusCode)
		resp = redirectResp
	}

	// 如果最终响应是 404, 则读取响应体并返回自定义错误页面
	if resp.StatusCode == 404 {
		defer resp.Body.Close() // 使用defer确保在函数返回前关闭响应体
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			c.Warnf("Failed to read upstream 404 response body: %v", err)
		} else {
			c.Warnf("Upstream 404 response body: %s", string(bodyBytes))
		}
		ErrorPage(c, NewErrorWithStatusLookup(404, "Page Not Found (From Upstream)"))
		return
	}

	// P0-1：已知 Content-Length 超限直接 413，不再 301 到上游；
	// 0 表示不限（旧代码把 0 当成 0 字节上限，任何层都 301）。
	capBytes := SizeLimitBytes(cfg.Server.SizeLimit)
	var (
		bodySize      int
		contentLength string
	)
	contentLength = resp.Header.Get("Content-Length")
	if contentLength != "" {
		var err error
		bodySize, err = strconv.Atoi(contentLength)
		if err != nil {
			c.Warnf("%s %s %s %s %s Content-Length header is not a valid integer: %v", c.ClientIP(), c.Request.Method, c.Request.URL.Path, c.UserAgent(), c.Request.Proto, err)
			bodySize = -1 // 无法解析则设置为 -1
		}
		if err == nil && capBytes > 0 && int64(bodySize) > capBytes {
			_ = resp.Body.Close()
			c.Warnf("%s %s %s %s %s Size-Limit-Exceeded: %d > %d url=%s", c.ClientIP(), c.Request.Method, c.Request.URL.Path, c.UserAgent(), c.Request.Proto, bodySize, capBytes, RedactURL(resp.Request.URL.String()))
			ErrorPage(c, NewErrorWithStatusLookup(413, "Payload Too Large (Size Limit Exceeded)"))
			return
		}
	}

	// 将上游响应头部复制到客户端响应，并清理需移除的头（含 Set-Cookie）。
	c.SetHeaders(resp.Header)
	for key := range respHeadersToRemove {
		c.DelHeader(key)
	}
	// 设置客户端响应状态码
	c.Status(resp.StatusCode)
	// bodyReader 的所有权将转移给 SetBodyStream, 不再由此函数管理关闭
	bodyReader := resp.Body

	// 如果启用了带宽限制, 则使用限速读取器
	if cfg.RateLimit.BandwidthLimit.Enabled {
		bodyReader = limitreader.NewRateLimitedReader(bodyReader, bandwidthLimit, int(bandwidthBurst), ctx)
	}

	// P1-3：分块体也受上限约束，超限时断流。
	if capBytes > 0 {
		bodyReader = newCountingReader(bodyReader, capBytes).withExceeded(func(n int64) {
			c.Warnf("%s %s %s Size-Limit-Exceeded during streaming: url=%s", c.ClientIP(), c.Request.Method, c.Request.URL.Path, RedactURL(resp.Request.URL.String()))
		})
	}

	// 根据 Content-Length 设置响应体流
	if contentLength != "" {
		c.SetBodyStream(bodyReader, bodySize)
		return
	}
	c.SetBodyStream(bodyReader, -1)
}

// AuthToken 用于解析认证响应中的令牌
type AuthToken struct {
	Token string `json:"token"`
}

// ChallengeReq 执行认证挑战流程, 获取新的认证令牌
func ChallengeReq(target string, image *imageInfo, ctx context.Context, c *touka.Context) (token string) {
	var resp401 *http.Response
	var req401 *http.Request
	var err error
	ghcrclient := c.GetHTTPC()

	// 对 /v2/ 端点发送 GET 请求以触发认证挑战
	rb401 := ghcrclient.NewRequestBuilder("GET", "https://"+target+"/v2/")
	rb401.NoDefaultHeaders()
	rb401.WithContext(ctx)
	req401, err = rb401.Build()
	if err != nil {
		HandleError(c, fmt.Sprintf("Failed to create request: %v", err))
		return
	}
	req401.Header.Set("Host", target) // 设置 Host 头部

	resp401, err = ghcrclient.Do(req401)
	if err != nil {
		HandleError(c, fmt.Sprintf("Failed to send request: %v", err))
		return
	}
	defer resp401.Body.Close() // 确保响应体关闭

	// 解析 Www-Authenticate 头部, 获取认证领域和参数
	bearer, err := parseBearerWWWAuthenticateHeader(resp401.Header.Get("Www-Authenticate"))
	if err != nil {
		c.Errorf("Failed to parse Www-Authenticate header: %v", err)
		return
	}

	// 构建认证范围 (scope), 通常是 repository:<image_name>:pull
	scope := fmt.Sprintf("repository:%s:pull", image.Image)

	// 使用解析到的 Realm 和 Service, 以及 scope 请求认证令牌
	getAuthRB := ghcrclient.NewRequestBuilder("GET", bearer.Realm).
		NoDefaultHeaders().
		WithContext(ctx).
		SetHeader("Host", bearer.Service).
		AddQueryParam("service", bearer.Service).
		AddQueryParam("scope", scope)

	getAuthReq, err := getAuthRB.Build()
	if err != nil {
		c.Errorf("Failed to create request: %v", err)
		return
	}

	authResp, err := ghcrclient.Do(getAuthReq)
	if err != nil {
		c.Errorf("Failed to send request: %v", err)
		return
	}
	defer authResp.Body.Close() // 确保响应体关闭

	// 读取认证响应体
	bodyBytes, err := io.ReadAll(authResp.Body)
	if err != nil {
		c.Errorf("Failed to read auth response body: %v", err)
		return
	}

	// 解码 JSON 响应以获取令牌
	var authToken AuthToken
	err = json.Unmarshal(bodyBytes, &authToken)
	if err != nil {
		c.Errorf("Failed to decode auth response body: %v", err)
		return
	}
	token = authToken.Token // 提取令牌

	return token
}
