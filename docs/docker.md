# Docker 部署

镜像：`ghcr.io/panda-995/denova:latest`，支持 Linux amd64 / arm64。Docker 会自动选择架构。

将仓库根目录的 `compose.yaml` 和 `docker/compose.env.example` 下载到同一目录，将后者重命名为 `.env`。修改 `.env` 的 `DENOVA_PASSWORD`（至少 12 个字符），然后运行：

```sh
docker compose up -d
```

访问 `http://服务器IP:8082`，默认用户名 `admin`，密码为 `.env` 中设置的值。登录后在设置中填写自己的模型服务地址、模型和 API Key。

首次启动会使用环境变量建立登录配置，只存储密码哈希。后续启动保留用户配置；更改 `.env` 不会重置已有密码，请在应用设置中修改。导入已有配置时请先启用局域网访问并设置登录凭据。

持久化卷 `denova_data` 挂载到 `/data`，用户配置、受管项目和会话位于 `/data/.denova`。备份时停止容器并备份整个卷。不要使用 `docker compose down -v`，该命令会删除数据。若改用主机目录挂载，目录需允许 UID/GID 1000 写入；外部项目路径必须另行挂载到容器。

镜像包含主程序、updater、前端、内嵌资源、Skills、ripgrep、Python、Git 和 Chromium。容器使用非 root 用户。浏览器工具使用容器中的 Chromium，镜像内的 Chromium 包装器关闭浏览器沙箱，容器本身保留 Docker 默认隔离。

更新通过 `docker compose pull && docker compose up -d` 完成；容器内不要使用应用的桌面安装更新功能。可将 `IMAGE_TAG` 改为构建提供的 `sha-xxxxxxxxxxxx` 固定版本。

构建流程在 GitHub 的 amd64 与 arm64 原生运行器上分别完整构建，并验证页面、登录保护、登录、内置工具、重启后的配置和会话保留，通过后合并发布双架构标签。运行镜像和下载镜像均不需要个人 GitHub 令牌。
