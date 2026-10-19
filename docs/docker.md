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

仓库每半小时（每小时第 23、53 分钟，UTC）检查 `alfredxw/denova` 的 `master` 分支，合并上游代码后显式触发源码 CI。源码检查通过后，由 CI 直接调用可复用的双架构镜像工作流。`latest` 跟随上游开发分支，可能包含尚未发布的改动。程序版本和 OCI version 标签来自上游 `web/package.json`，具体构建由 `sha-xxxxxxxxxxxx` 标签及 OCI revision 标签标识。

同步使用 GitHub 自带的 `GITHUB_TOKEN`，无需保存个人令牌。上游的工作流文件不合入，保留本 fork 的自动化；Docker 文件发生冲突时保留 fork 版本，其余源码冲突采用上游版本。即使没有新的上游提交，也会检查当前提交的 CI（含镜像发布）：已成功或仍在运行则跳过，失败或取消则下次检查重试。源码 CI 和两个架构的容器检查全部通过后，才发布新的 `latest`；失败时保留上一次可用的版本。

源码 CI 使用上游现有的按变更检查、成品包浏览器测试和完整回归分片。普通提交运行基础检查、关键流程和此前失败的界面流程；每日定时与手动 `full` 运行覆盖完整回归，避免依赖开发服务器串行跑两小时。

可在 Actions 中手动运行 **Sync upstream and ensure image publication** 立即同步，或运行 **CI**（选择 `smoke` 或 `full`）完成检查并重新构建镜像。镜像发布直接依赖 CI 检查结果，不轮询 Actions API 等待状态。GitHub 定时任务可能延迟；公共仓库长期无活动时可能被 GitHub 停用，需在 Actions 中重新启用。
