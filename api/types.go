package api

// 预定义的固定响应内容，避免每次请求时创建 map

// HealthcheckResponse 健康检查响应
type HealthcheckResponse struct {
	Status string `json:"Status"`
	Repo   string `json:"Repo"`
	Author string `json:"Author"`
}

// VersionResponse 版本信息响应
type VersionResponse struct {
	Version string `json:"Version"`
	Repo    string `json:"Repo"`
	Author  string `json:"Author"`
}

// BoolStatusResponse 布尔状态响应
type BoolStatusResponse struct {
	Enabled bool `json:"enabled"`
}

// SizeLimitResponse 大小限制响应
type SizeLimitResponse struct {
	MaxResponseBodySize int `json:"MaxResponseBodySize"`
}

// WhitelistStatusResponse 白名单状态响应
type WhitelistStatusResponse struct {
	Whitelist bool `json:"Whitelist"`
}

// BlacklistStatusResponse 黑名单状态响应
type BlacklistStatusResponse struct {
	Blacklist bool `json:"Blacklist"`
}

// CorsStatusResponse CORS 状态响应
type CorsStatusResponse struct {
	Cors string `json:"Cors"`
}

// RateLimitStatusResponse 速率限制状态响应
type RateLimitStatusResponse struct {
	RateLimit bool `json:"RateLimit"`
}

// RateLimitLimitResponse 速率限制值响应
type RateLimitLimitResponse struct {
	RatePerMinute int `json:"RatePerMinute"`
}

// SmartGitStatusResponse SmartGit 状态响应
type SmartGitStatusResponse struct {
	Enabled bool `json:"enabled"`
}

// ShellNestStatusResponse Shell Nest 状态响应
type ShellNestStatusResponse struct {
	Enabled bool `json:"enabled"`
}

// OCIDockerResponse OCI/Docker 代理状态响应
type OCIDockerResponse struct {
	Enabled bool   `json:"enabled"`
	Target  string `json:"target,omitempty"`
}

// 预定义的固定响应实例，避免重复分配
var (
	// baseHealthcheckResponse 基础健康检查响应（固定部分）
	baseHealthcheckResponse = HealthcheckResponse{
		Status: "OK",
		Repo:   "WJQSERVER-STUDIO/GHProxy",
		Author: "WJQSERVER-STUDIO",
	}

	// baseVersionResponse 基础版本响应（固定部分）
	baseVersionResponse = VersionResponse{
		Repo:   "WJQSERVER-STUDIO/GHProxy",
		Author: "WJQSERVER-STUDIO",
	}
)
