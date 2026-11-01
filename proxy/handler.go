package proxy

import (
	"fmt"
	"ghproxy/config"
	"regexp"
	"strings"

	"github.com/infinite-iroha/touka"
)

var re = regexp.MustCompile(`^(http:|https:)?/?/?(.*)`) // 匹配http://或https://开头的路径

func NoRouteHandler(cfg *config.Config) touka.HandlerFunc {
	return func(c *touka.Context) {
		var ctx = c.Request.Context()
		var shoudBreak bool

		var (
			rawPath string
			matches []string
		)

		rawPath = strings.TrimPrefix(c.GetRequestURI(), "/") // 去掉前缀/
		matches = re.FindStringSubmatch(rawPath)             // 匹配路径

		// 匹配路径错误处理
		if len(matches) < 3 {
			c.Warnf("%s %s %s %s %s Invalid URL", c.ClientIP(), c.Request.Method, c.Request.URL.Path, c.UserAgent(), c.Request.Proto)
			// 错误页只回显路径，不回显完整 RequestURI（含查询串，可能是令牌）。
			ErrorPage(c, NewErrorWithStatusLookup(400, fmt.Sprintf("Invalid URL Format: %s", c.Request.URL.Path)))
			return
		}

		// 制作url，并在 Matcher 之前剥离客户端令牌参数、归一化 raw 主机
		// （P0-3/P1-4）：此后上游 URL 与所有日志都不再含 ?token=。
		rawPath = normalizeRawHost(stripAuthQueryParams("https://" + matches[2]))

		var (
			user    string
			repo    string
			matcher string
		)

		var matcherErr *GHProxyErrors
		user, repo, matcher, matcherErr = Matcher(rawPath, cfg)
		if matcherErr != nil {
			ErrorPage(c, matcherErr)
			return
		}

		shoudBreak = listCheck(cfg, c, user, repo, rawPath)
		if shoudBreak {
			return
		}

		shoudBreak = authCheck(c, cfg, matcher, rawPath)
		if shoudBreak {
			return
		}

		// 处理blob/raw路径
		if matcher == "blob" {
			rawPath = rawPath[18:]
			rawPath = "https://raw.githubusercontent.com" + rawPath
			rawPath = strings.Replace(rawPath, "/blob/", "/", 1)
			matcher = "raw"
		}

		switch matcher {
		case "releases", "blob", "raw", "gist", "api":
			ChunkedProxyRequest(ctx, c, rawPath, cfg, matcher)
		case "clone":
			GitReq(ctx, c, rawPath, cfg, "git")
		default:
			ErrorPage(c, NewErrorWithStatusLookup(500, "Matched But Not Matched"))
			c.Errorf("Matched But Not Matched Path: %s rawPath: %s matcher: %s", c.GetRequestURIPath(), rawPath, matcher)
			return
		}
	}
}
