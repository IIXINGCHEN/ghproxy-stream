# ghproxy-stream

> Streaming-first GitHub 代理（Go），基于 WJQSERVER-STUDIO/ghproxy 的加固分支。
> 本仓库沿用上游双许可（见 LICENSE 与 LICENSE-WSL），上游文档保留在下方；先读“与上游的区别”，再按上游文档部署。

# GHProxy

![GitHub Release](https://img.shields.io/github/v/release/WJQSERVER-STUDIO/ghproxy?display_name=tag&style=flat)
![pull](https://img.shields.io/docker/pulls/wjqserver/ghproxy.svg)
![Docker Image Size (tag)](https://img.shields.io/docker/image-size/wjqserver/ghproxy/latest)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/WJQSERVER-STUDIO/ghproxy)
[![Go Report Card](https://goreportcard.com/badge/github.com/WJQSERVER-STUDIO/ghproxy)](https://goreportcard.com/report/github.com/WJQSERVER-STUDIO/ghproxy)

一个基于Go的高性能Github资源代理程序, 同时支持Docker镜像代理与脚本嵌套加速等多种功能

## 项目说明

### 项目特点

- ⚡ **基于 Go 语言实现，跨平台的同时提供高并发性能**
- 🌐 **使用自有[Touka框架](https://github.com/infinite-iroha/touka)作为 HTTP服务端框架**
- 📡 **使用 [Touka-HTTPC](https://github.com/WJQSERVER-STUDIO/httpc) 作为 HTTP 客户端**
- 📥 **支持 Git clone、raw、releases 等文件拉取**
- 🐳 **支持反代Docker, GHCR等镜像仓库**
- 🎨 **支持多个前端主题**
- 🚫 **支持自定义黑名单/白名单**
- 🗄️ **支持 Git Clone 缓存（配合 [Smart-Git](https://github.com/WJQSERVER-STUDIO/smart-git)）**
- 🐳 **支持自托管与Docker容器化部署**
- ⚡ **支持速率限制**
- ⚡ **支持带宽速率限制**
- 🔒 **支持用户鉴权**
- 🐚 **支持 shell 脚本多层嵌套加速**

### 项目相关

[DEMO](https://ghproxy.1888866.xyz)

[TG讨论群组](https://t.me/ghproxy_go)

[GHProxy项目文档](https://wjqserver-docs.pages.dev/docs/ghproxy/) 感谢 [@redbunnys](https://github.com/redbunnys)的维护

[相关文章](https://blog.wjqserver.com/categories/my-program/)

### 使用示例

```bash 
# 下载文件
https://ghproxy.1888866.xyz/raw.githubusercontent.com/WJQSERVER-STUDIO/tools-stable/main/tools-stable-ghproxy.sh
https://ghproxy.1888866.xyz/https://raw.githubusercontent.com/WJQSERVER-STUDIO/tools-stable/main/tools-stable-ghproxy.sh

# 克隆仓库
git clone https://ghproxy.1888866.xyz/github.com/WJQSERVER-STUDIO/ghproxy.git
git clone https://ghproxy.1888866.xyz/https://github.com/WJQSERVER-STUDIO/ghproxy.git

# Docker(OCI) 代理
docker pull gh.example.com/wjqserver/ghproxy
docker pull gh.example.com/adguard/adguardhome

docker pull gh.example.com/docker.io/wjqserver/ghproxy
docker pull gh.example.com/docker.io/adguard/adguardhome

docker pull gh.example.com/ghcr.io/openfaas/queue-worker 
```

## 部署说明

可参考文章: https://blog.wjqserver.com/post/ghproxy-deploy-with-smart-git/

### Docker部署

- Docker-cli

```
docker run -p 7210:8080 -v ./ghproxy/log/run:/data/ghproxy/log -v ./ghproxy/log/caddy:/data/caddy/log -v ./ghproxy/config:/data/ghproxy/config  --restart always wjqserver/ghproxy
```

- Docker-Compose (建议使用)

    参看[docker-compose.yml](https://github.com/WJQSERVER-STUDIO/ghproxy/blob/main/docker/compose/docker-compose.yml)

### 二进制文件部署(不推荐)

一键部署脚本:

```bash
wget -O install.sh https://raw.githubusercontent.com/WJQSERVER-STUDIO/ghproxy/main/deploy/install.sh && chmod +x install.sh &&./install.sh
```

Dev一键部署脚本:

```bash
wget -O install-dev.sh https://raw.githubusercontent.com/WJQSERVER-STUDIO/ghproxy/dev/deploy/install-dev.sh && chmod +x install-dev.sh && ./install-dev.sh
```

## 配置说明

参看[项目文档](https://github.com/WJQSERVER-STUDIO/ghproxy/blob/main/docs/config.md)

### 前端页面

参看[GHProxy-Frontend](https://github.com/WJQSERVER-STUDIO/GHProxy-Frontend)

## 文档

*   [GHProxy项目文档](https://wjqserver-docs.pages.dev/docs/ghproxy/) 感谢 [@redbunnys](https://github.com/redbunnys)的维护

*   [![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/WJQSERVER-STUDIO/ghproxy) 可供参考, AI生成存在幻觉, 不完全可靠, 请注意辨别


## 与上游的区别（ghproxy-stream 改动，2026-11-01）

- **全程流式，不再 301 直连**：去掉 chunk / git / docker 三路的 125MB 上限 301，`Content-Length` 超限返回 413，分块体在上限处断流；`sizeLimit` 默认 2048MB，0 表示不限。
- **服务端跳转跟随**：301/302/303/307/308 最多 5 跳，相对 Location 按当前 URL 解析，跨主机跳时剥离 Authorization；非 GET/HEAD 不跟随，Location 改写回代理地址。
- **服务端令牌池**：环境变量 `GITHUB_TOKENS`（逗号分隔，备选单个 `GITHUB_TOKEN`），客户端自带令牌优先；GET/HEAD 遇 403/429 冷却该令牌（Retry-After > X-RateLimit-Reset > 60 秒，钳制 1～3600 秒）换下一个重试；API 在池可用时放行；`?token=` / `?auth_token=` 在构造上游目标前剥离。
- **头与日志清理**：请求头删 Cookie，响应头删 Set-Cookie；日志只打印 scheme://host/path，签名参数与令牌不落盘。
- **Range 显式透传**：206 / 416 原样回传，绝不由 200 合成；Range 请求跳过 Shell Editor 改写正文。
- **限速单位修正**：`ratePerMinute` 是每分钟，程序内部除以 60 换算成每秒（旧代码直接当每秒传，实际生效约 60 倍）；`rateLimit.enabled` 默认仍为关闭，生产开启时数值才是正确的每分钟。
- **归一化**：gist 只接受 `.../raw/...` 文件链接（网页链接 400）；`raw.github.com` 归一化到 `raw.githubusercontent.com`。
- **CORS 与自环**：CORS 的空值不再映射成 `*`（只有显式 `*` 才全放行）；cache 模式的 SmartGitAddr 默认改为空，未配置时启动即失败（旧默认就是本服务自身端口，会自环）。
- **冒烟结果（沙箱本地，真实 GitHub）**：Release Range 不加 -L 得 206 + 正确 Content-Range 且无 Location；Raw 得 200；`raw.github.com` 得 200；gist 网页得 400；无池 API 得 403 门禁；假池令牌 API 得上游 401（证明门禁打开且令牌已附带）；真实 git clone 成功；1MB 上限下 13MB 文件得 413 无 Location。尚未在沙箱验证：真令牌下的 API 5000 限额、Docker pull（无 docker 守护进程）——真实部署时补测。
- **部署前必清**：`deploy/config.toml` 与示例配置里的 Docker 默认凭证占位值在生产前必须清空或替换为真实凭证（此处不记录其值）；真令牌只通过环境变量 `GITHUB_TOKENS` 注入，绝不写进配置文件或代码。

## LICENSE

v3.5.2开始, 本项目使用 [WJQserver Studio License 2.1](https://wjqserver-studio.github.io/LICENSE/LICENSE.html) 和 [Mozilla Public License Version 2.0](https://mozilla.org/MPL/2.0/) 双重许可, 您可从中选择一个使用

前端位于单独仓库中, 且各个主题均存在各自的许可证, 本项目许可证并不包括前端

在v2.3.0之前, 本项目使用WJQserver Studio License 1.2

在v1.0.0版本之前,本项目继承于[WJQSERVER-STUDIO/ghproxy-go](https://github.com/WJQSERVER-STUDIO/ghproxy-go)的APACHE2.0 LICENSE VERSION

## 赞助

如果您觉得本项目对您有帮助,欢迎赞助支持,您的赞助将用于Demo服务器开支及开发者时间成本支出,感谢您的支持!

USDT(TRC20): `TNfSYG6F2vkiibd6J6mhhHNWDgWgNdF5hN`

### 捐赠列表

| 赞助人    |金额|
|--------|------|
| starry | 8 USDT (TRC20)   |
