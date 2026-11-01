package proxy

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// 差距修复回归测试：跳转、令牌池、脱敏、计数上限、CORS、限速单位。
// 只测纯函数，不启动 Touka 服务（沙箱内真实 GitHub 跳转走 egress 代理，
// 不适合进单元测试；真实链路用部署后冒烟验证）。

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestResolveRedirect(t *testing.T) {
	base := mustURL(t, "https://github.com/owner/repo/releases/download/v1/a.zip")
	// 绝对跳转
	next, err := ResolveRedirect(base, "https://objects.githubusercontent.com/abc?sig=xxx")
	if err != nil || next.String() != "https://objects.githubusercontent.com/abc?sig=xxx" {
		t.Fatalf("absolute: %v, %v", next, err)
	}
	// 相对跳转
	next, err = ResolveRedirect(base, "/other/path?q=1")
	if err != nil || next.String() != "https://github.com/other/path?q=1" {
		t.Fatalf("relative: %v, %v", next, err)
	}
	// 非 http(s) 拒绝
	if _, err := ResolveRedirect(base, "ftp://example.com/x"); err == nil {
		t.Fatal("ftp scheme should be rejected")
	}
}

func TestIsTokenHost(t *testing.T) {
	yes := []string{"github.com", "api.github.com", "gist.github.com", "codeload.github.com", "raw.githubusercontent.com", "gist.githubusercontent.com"}
	no := []string{"objects.githubusercontent.com", "release-assets.githubusercontent.com", "media.githubusercontent.com", "example.com", "evilgithub.com"}
	for _, h := range yes {
		if !IsTokenHost(h) {
			t.Errorf("IsTokenHost(%q) should be true", h)
		}
	}
	for _, h := range no {
		if IsTokenHost(h) {
			t.Errorf("IsTokenHost(%q) should be false", h)
		}
	}
}

func TestRedactURL(t *testing.T) {
	got := RedactURL("https://objects.githubusercontent.com/abc?sig=xxx&jwt=yyy#frag")
	if got != "https://objects.githubusercontent.com/abc" {
		t.Fatalf("redact = %q", got)
	}
	if RedactURL("not a url :::") != "(invalid url)" {
		t.Fatal("unparsable url should be placeholder")
	}
}

func TestStripAuthQueryParams(t *testing.T) {
	cases := map[string]string{
		"https://github.com/o/r?raw=1&token=secret&x=2": "https://github.com/o/r?raw=1&x=2",
		"https://github.com/o/r?token=secret":           "https://github.com/o/r",
		"https://github.com/o/r?auth_token=secret&a=b":  "https://github.com/o/r?a=b",
		"https://github.com/o/r?a=b":                    "https://github.com/o/r?a=b",
		"https://github.com/o/r":                        "https://github.com/o/r",
	}
	for in, want := range cases {
		if got := stripAuthQueryParams(in); got != want {
			t.Errorf("strip(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeRawHost(t *testing.T) {
	if got := normalizeRawHost("https://raw.github.com/o/r/main/f"); got != "https://raw.githubusercontent.com/o/r/main/f" {
		t.Fatalf("normalize = %q", got)
	}
	if got := normalizeRawHost("https://raw.githubusercontent.com/o/r/main/f"); got != "https://raw.githubusercontent.com/o/r/main/f" {
		t.Fatalf("already normalized should be unchanged: %q", got)
	}
}

func TestTokenPoolPickCool(t *testing.T) {
	p := NewTokenPool([]string{"tok1", "tok2"})
	if p.Pick() != "tok1" {
		t.Fatal("first pick should be tok1")
	}
	p.Cool("tok1", time.Hour)
	if p.Pick() != "tok2" {
		t.Fatal("cooled tok1 should be skipped")
	}
	p.Cool("tok2", time.Hour)
	if p.Pick() != "" {
		t.Fatal("all cooled should return empty")
	}
	if len(ParseTokens(" a , ,b,a ")) != 2 {
		t.Fatal("ParseTokens should trim, drop empty, dedupe")
	}
}

func TestCooldownFromHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "120")
	if d := CooldownFromHeaders(h); d != 120*time.Second {
		t.Fatalf("Retry-After: %v", d)
	}
	h = http.Header{}
	h.Set("X-RateLimit-Reset", "9999999999") // 远未来，钳制到 3600s
	if d := CooldownFromHeaders(h); d != 3600*time.Second {
		t.Fatalf("Reset clamp: %v", d)
	}
	h = http.Header{}
	if d := CooldownFromHeaders(h); d != 60*time.Second {
		t.Fatalf("default: %v", d)
	}
}

func TestCountingReader(t *testing.T) {
	src := io.NopCloser(strings.NewReader("0123456789"))
	cr := newCountingReader(src, 5)
	buf := make([]byte, 4)
	if n, err := cr.Read(buf); n != 4 || err != nil {
		t.Fatalf("first read: %d, %v", n, err)
	}
	if _, err := io.ReadAll(cr); err == nil || !strings.Contains(err.Error(), "size limit exceeded") {
		t.Fatalf("should exceed at limit, got err=%v", err)
	}
	if !cr.Exceeded() {
		t.Fatal("Exceeded should be true")
	}
	// 未超限时正常读完
	cr2 := newCountingReader(io.NopCloser(strings.NewReader("abc")), 100)
	if data, err := io.ReadAll(cr2); err != nil || string(data) != "abc" {
		t.Fatalf("under limit: %q, %v", data, err)
	}
}

func TestCorsAllowOrigin(t *testing.T) {
	if corsAllowOrigin("*") != "*" || corsAllowOrigin("https://example.com") != "https://example.com" {
		t.Fatal("explicit origins should pass through")
	}
	if corsAllowOrigin("") != "" || corsAllowOrigin("nil") != "" {
		t.Fatal("empty and nil should mean no CORS header")
	}
}

func TestPerSecondFromPerMinute(t *testing.T) {
	if v := PerSecondFromPerMinute(180); v != 3 {
		t.Fatalf("180/min should be 3/s, got %v", v)
	}
	if v := PerSecondFromPerMinute(100); v != 100.0/60.0 {
		t.Fatalf("100/min: %v", v)
	}
}

func TestSizeLimitBytes(t *testing.T) {
	if SizeLimitBytes(2048) != 2048*1024*1024 {
		t.Fatal("2048MB conversion")
	}
	if SizeLimitBytes(0) != 0 || SizeLimitBytes(-1) != 0 {
		t.Fatal("<=0 means unlimited")
	}
}
