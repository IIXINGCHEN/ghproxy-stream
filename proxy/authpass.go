package proxy

import (
	"ghproxy/config"
	"net/http"

	"github.com/infinite-iroha/touka"
)

// AuthPassThrough 原有客户端透传逻辑保持不变；末尾新增服务端令牌池回退。
// 返回值：本次实际附上的池令牌（"" 表示未用池令牌——或是客户端自带，
// 或是池为空、或目标不是可发令牌主机）。调用方凭此返回值做 403/429 冷却。
func AuthPassThrough(c *touka.Context, cfg *config.Config, req *http.Request) string {
	if cfg.Auth.PassThrough {
		token := c.Query("token")
		if token != "" {
			switch cfg.Auth.Method {
			case "parameters":
				if !cfg.Auth.Enabled {
					req.Header.Set("Authorization", "token "+token)
				} else {
					c.Warnf("%s %s %s %s %s Auth-Error: Conflict Auth Method", c.ClientIP(), c.Request.Method, c.Request.URL.Path, c.UserAgent(), c.Request.Proto)
					ErrorPage(c, NewErrorWithStatusLookup(500, "Conflict Auth Method"))
					return ""
				}
			case "header":
				if cfg.Auth.Enabled {
					req.Header.Set("Authorization", "token "+token)
				}
			default:
				c.Warnf("%s %s %s %s %s Invalid Auth Method / Auth Method is not be set", c.ClientIP(), c.Request.Method, c.Request.URL.Path, c.UserAgent(), c.Request.Proto)
				ErrorPage(c, NewErrorWithStatusLookup(500, "Invalid Auth Method / Auth Method is not be set"))
				return ""
			}
		}
	}
	// P0-3 令牌池回退：客户端未自带 Authorization（既无复制来的请求头，
	// 也无上面透传逻辑附上的）时，从池里挑一个未冷却的令牌；目标不是
	// 可发令牌主机（签名 CDN）时不附带，调用方在跳转时也会再次剥离。
	if req.Header.Get("Authorization") == "" {
		if tok := tokenPool.Pick(); tok != "" && IsTokenHost(req.URL.Host) {
			req.Header.Set("Authorization", "token "+tok)
			return tok
		}
	}
	return ""
}
