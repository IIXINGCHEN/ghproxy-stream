package api

import (
	"ghproxy/config"
	"ghproxy/middleware/nocache"

	"github.com/infinite-iroha/touka"
)

func InitHandleRouter(cfg *config.Config, r *touka.Engine, version string) {
	apiRouter := r.Group("/api", nocache.NoCacheMiddleware())
	{
		apiRouter.GET("/size_limit", func(c *touka.Context) {
			SizeLimitHandler(cfg, c)
		})
		apiRouter.GET("/whitelist/status", func(c *touka.Context) {
			WhiteListStatusHandler(cfg, c)
		})
		apiRouter.GET("/blacklist/status", func(c *touka.Context) {
			BlackListStatusHandler(cfg, c)
		})
		apiRouter.GET("/cors/status", func(c *touka.Context) {
			CorsStatusHandler(cfg, c)
		})
		apiRouter.GET("/healthcheck", func(c *touka.Context) {
			HealthcheckHandler(c)
		})
		apiRouter.GET("/ok", func(c *touka.Context) {
			HealthcheckHandler(c)
		})
		apiRouter.GET("/version", func(c *touka.Context) {
			VersionHandler(c, version)
		})
		apiRouter.GET("/rate_limit/status", func(c *touka.Context) {
			RateLimitStatusHandler(cfg, c)
		})
		apiRouter.GET("/rate_limit/limit", func(c *touka.Context) {
			RateLimitLimitHandler(cfg, c)
		})
		apiRouter.GET("/smartgit/status", func(c *touka.Context) {
			SmartGitStatusHandler(cfg, c)
		})
		apiRouter.GET("/shell_nest/status", func(c *touka.Context) {
			shellNestStatusHandler(cfg, c)
		})
		apiRouter.GET("/oci_proxy/status", func(c *touka.Context) {
			ociProxyStatusHandler(cfg, c)
		})
	}
}

func SizeLimitHandler(cfg *config.Config, c *touka.Context) {
	c.SetHeader("Content-Type", "application/json")
	c.JSON(200, SizeLimitResponse{
		MaxResponseBodySize: cfg.Server.SizeLimit,
	})
}

func WhiteListStatusHandler(cfg *config.Config, c *touka.Context) {
	c.SetHeader("Content-Type", "application/json")
	c.JSON(200, WhitelistStatusResponse{
		Whitelist: cfg.Whitelist.Enabled,
	})
}

func BlackListStatusHandler(cfg *config.Config, c *touka.Context) {
	c.SetHeader("Content-Type", "application/json")
	c.JSON(200, BlacklistStatusResponse{
		Blacklist: cfg.Blacklist.Enabled,
	})
}

func CorsStatusHandler(cfg *config.Config, c *touka.Context) {
	c.SetHeader("Content-Type", "application/json")
	c.JSON(200, CorsStatusResponse{
		Cors: cfg.Server.Cors,
	})
}

func HealthcheckHandler(c *touka.Context) {
	c.SetHeader("Content-Type", "application/json")
	// 复制预定义的固定响应，避免重复分配
	resp := baseHealthcheckResponse
	c.JSON(200, resp)
}

func VersionHandler(c *touka.Context, version string) {
	c.SetHeader("Content-Type", "application/json")
	// 复制预定义的固定响应并填充动态字段
	resp := baseVersionResponse
	resp.Version = version
	c.JSON(200, resp)
}

func RateLimitStatusHandler(cfg *config.Config, c *touka.Context) {
	c.SetHeader("Content-Type", "application/json")
	c.JSON(200, RateLimitStatusResponse{
		RateLimit: cfg.RateLimit.Enabled,
	})
}

func RateLimitLimitHandler(cfg *config.Config, c *touka.Context) {
	c.SetHeader("Content-Type", "application/json")
	c.JSON(200, RateLimitLimitResponse{
		RatePerMinute: cfg.RateLimit.RatePerMinute,
	})
}

func SmartGitStatusHandler(cfg *config.Config, c *touka.Context) {
	c.SetHeader("Content-Type", "application/json")
	c.JSON(200, SmartGitStatusResponse{
		Enabled: cfg.GitClone.Mode == "cache",
	})
}

func shellNestStatusHandler(cfg *config.Config, c *touka.Context) {
	c.SetHeader("Content-Type", "application/json")
	c.JSON(200, ShellNestStatusResponse{
		Enabled: cfg.Shell.Editor,
	})
}

func ociProxyStatusHandler(cfg *config.Config, c *touka.Context) {
	c.SetHeader("Content-Type", "application/json")
	c.JSON(200, OCIDockerResponse{
		Enabled: cfg.Docker.Enabled,
		Target:  cfg.Docker.Target,
	})
}
