package proxy

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 差距修复 P0-3：服务端 GitHub 令牌池。
//
// 来源与优先级：环境变量 GITHUB_TOKENS（逗号分隔）优先，
// 其次 GITHUB_TOKEN（单个）；两者都未设置时池为空，行为与原来一致
// （仅客户端透传）。令牌绝不写进 config.toml、代码或日志。
//
// 语义：
//   - 客户端自带 Authorization 时优先用客户端的，池不介入；
//   - 否则从池里挑第一个未冷却的令牌；
//   - GET/HEAD 遇 403/429 时冷却该令牌（Retry-After > X-RateLimit-Reset > 60s，
//     钳制 1s～3600s）并换下一个重试；
//   - 令牌只发给 IsTokenHost 主机，签名 CDN 主机一律剥离（见 redirect.go）。

// ParseTokens 解析逗号分隔的令牌列表：去空白、去重、去空。
func ParseTokens(raw string) []string {
	if raw == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, part := range strings.Split(raw, ",") {
		t := strings.TrimSpace(part)
		if t != "" {
			if _, ok := seen[t]; !ok {
				seen[t] = struct{}{}
				out = append(out, t)
			}
		}
	}
	return out
}

// TokenPool 线程安全的令牌池（冷却表带互斥锁）。
type TokenPool struct {
	mu     sync.Mutex
	tokens []string
	cooled map[string]time.Time
}

// NewTokenPool 创建令牌池；空列表表示未配置，Pick 永远返回 ""。
func NewTokenPool(tokens []string) *TokenPool {
	return &TokenPool{tokens: tokens, cooled: make(map[string]time.Time)}
}

// Len 返回池中令牌数。
func (p *TokenPool) Len() int {
	if p == nil {
		return 0
	}
	return len(p.tokens)
}

// Pick 返回第一个未冷却的令牌；全部冷却或未配置返回 ""。
func (p *TokenPool) Pick() string {
	if p == nil {
		return ""
	}
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range p.tokens {
		if until, ok := p.cooled[t]; !ok || !until.After(now) {
			return t
		}
	}
	return ""
}

// Cool 把令牌冷却 d 时长。
func (p *TokenPool) Cool(token string, d time.Duration) {
	if p == nil || token == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cooled[token] = time.Now().Add(d)
}

// CooldownFromHeaders 由 403/429 响应头算冷却时长：
// Retry-After（秒）优先，其次 X-RateLimit-Reset（Unix 秒），都没有则 60s。
// 上下限钳制到 1s～3600s，避免异常头把令牌封到天荒地老。
func CooldownFromHeaders(h http.Header) time.Duration {
	clamp := func(s float64) time.Duration {
		if s < 1 {
			s = 1
		}
		if s > 3600 {
			s = 3600
		}
		return time.Duration(s * float64(time.Second))
	}
	if ra := strings.TrimSpace(h.Get("Retry-After")); ra != "" {
		if n, err := strconv.ParseFloat(ra, 64); err == nil && n >= 0 {
			return clamp(n)
		}
	}
	if reset := strings.TrimSpace(h.Get("X-RateLimit-Reset")); reset != "" {
		if n, err := strconv.ParseFloat(reset, 64); err == nil {
			return clamp(n - float64(time.Now().Unix()))
		}
	}
	return 60 * time.Second
}

// tokenPool 全局池，由 main 在启动时从环境变量初始化；
// 未初始化时为 nil，Pick 返回 ""，调用方按无池处理。
var tokenPool *TokenPool

// InitTokenPool 初始化全局令牌池（启动时调用一次）。
func InitTokenPool(tokens []string) {
	tokenPool = NewTokenPool(tokens)
}

// PoolTokenAvailable 供 authCheck 判断 API 是否可因池存在而放行。
func PoolTokenAvailable() bool {
	return tokenPool != nil && tokenPool.Pick() != ""
}

// 差距修复 P0-1 / P1-3：流式字节上限。
//
// 语义：上限针对“本次传输的字节数”，不是文件总大小——
// Range 请求只传一段时，只计该段字节数，不会因原文件大而误杀。
// SizeLimit <= 0 表示不限（旧代码把 0 当成 0 字节上限，任何文件都 301，
// 这里明确修正为不限）。

// SizeLimitBytes 把配置的 MB 数换算成字节；<=0 返回 0（不限）。
func SizeLimitBytes(sizeLimitMB int) int64 {
	if sizeLimitMB <= 0 {
		return 0
	}
	return int64(sizeLimitMB) * 1024 * 1024
}

var errSizeLimitExceeded = errors.New("size limit exceeded")

// countingReader 边读边计数的 reader；读满 limit 后下一次 Read 返回
// errSizeLimitExceeded，调用方据此断流并记日志。注意：状态码在流开始时
// 已发出，断流只能截断响应体，无法事后改成 413——Content-Length 已知
// 超限的情况应在开始流之前就返回 413（见各路的 SizeLimit 检查）。
type countingReader struct {
	r          io.ReadCloser
	limit      int64
	n          int64
	exceeded   bool
	onExceeded func(int64)
}

func newCountingReader(r io.ReadCloser, limit int64) *countingReader {
	return &countingReader{r: r, limit: limit}
}

// Close 透传到底层响应体（否则 bodyReader 不能赋给 io.ReadCloser 变量）。
func (cr *countingReader) Close() error { return cr.r.Close() }

// withExceeded 在超限时回调一次（供 SetBodyStream 不返回错误的调用方记日志用）。
func (cr *countingReader) withExceeded(fn func(int64)) *countingReader {
	cr.onExceeded = fn
	return cr
}

func (cr *countingReader) Read(p []byte) (int, error) {
	if cr.exceeded {
		return 0, errSizeLimitExceeded
	}
	if cr.limit > 0 && cr.n >= cr.limit {
		cr.exceeded = true
		if cr.onExceeded != nil {
			cr.onExceeded(cr.n)
		}
		return 0, errSizeLimitExceeded
	}
	if cr.limit > 0 && int64(len(p)) > cr.limit-cr.n {
		p = p[:cr.limit-cr.n]
	}
	n, err := cr.r.Read(p)
	cr.n += int64(n)
	if err == nil && cr.limit > 0 && cr.n >= cr.limit {
		// 恰好读满上限：下一次 Read 再报错；若底层恰好 EOF，则属正常结束。
		// 这里不立即报错，让调用方在下一轮读到 EOF 或超限错误时区分。
	}
	return n, err
}

// Exceeded 报告是否因超限而断流。
func (cr *countingReader) Exceeded() bool { return cr.exceeded }

// 差距修复 P2-1：CORS 取值统一。
// 旧代码在三路里重复写 switch，且 "" 被映射成 "*"（空配置意外全放行）。
// 新语义："*" 才放行全部；"nil" 与 "" 都表示不放行（不发 Allow-Origin 头）；
// 其他值原样作为单个来源。
func corsAllowOrigin(cfgValue string) string {
	switch cfgValue {
	case "*":
		return "*"
	case "", "nil":
		return ""
	default:
		return cfgValue
	}
}

// PerSecondFromPerMinute 把“每分钟”换算成 golang.org/x/time/rate 的
// “每秒”（rate.Limit 单位）。旧 main.go 把 RatePerMinute 直接当成
// rate.Limit 传进去，实际生效的是 180/秒而非 180/分钟。
func PerSecondFromPerMinute(perMinute int) float64 {
	return float64(perMinute) / 60.0
}
