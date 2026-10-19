# CI 与发布耗时

## 基线与验收口径

使用成功运行比较，不把失败、取消或重试累计时间作为优化前基线。

| 流程 | 成功基线 | 原耗时 | 减半目标 |
| --- | --- | --- | --- |
| CI | [35746679946，v0.5.0 提交](https://github.com/alfredxw/denova/actions/runs/35746679946) | 16 分 47 秒 | ≤ 8 分 23 秒 |
| Release | [34347053449，v0.4.5](https://github.com/alfredxw/denova/actions/runs/34347053449) | 作业执行 26 分 14 秒 | ≤ 13 分 7 秒 |

Release 基线另有 4 分 21 秒排队，总历时 30 分 36 秒。v0.5.0 的 Release 运行被取消，不作为成功基线。比较时分别记录排队与执行时间，不能用增加重试或放宽超时冒充提速。

原 CI 的浏览器分片按文件分配，两个 browser 分片分别执行约 891 秒和 403 秒；Go、单测、构建与生产烟测又在同一个作业串行执行。发布脚本重新跑全部测试，再串行编译五个平台。

## GitHub 验收（2026-09-23）

代码提交 `890a8da40d83163a3890149ae0a5459a886e4064` 的 [完整 CI 35840742398](https://github.com/alfredxw/denova/actions/runs/35840742398) 成功，12 个作业全部通过，未使用自动重试。

| 指标 | 原基线 | 优化后 | 减少 |
| --- | --- | --- | --- |
| CI 总历时（创建至完成，含排队） | 16 分 47 秒 | **7 分 24 秒** | **55.9%** |
| 前端单测步骤 | 250 秒 | 99 秒 | 60.4% |
| 所有作业执行时间之和 | 42 分 51 秒 | 30 分 49 秒 | 28.1% |

相比提交前最近一次 [CI 35762849689](https://github.com/alfredxw/denova/actions/runs/35762849689) 的 19 分 8 秒，总历时减少 61.3%。并行度提高后，累计作业执行时间也下降；此项按各作业开始/结束时间相加，不等同于计费分钟。

验收覆盖两个 Go module 的全量 test/vet、安装与打包测试、翻译检查、完整生产构建、134 文件中的 779 项单测，以及生产包上的 browser/e2e/production 测试：127 项通过、10 项既有条件跳过、0 失败、0 flaky。六片 Playwright 实际耗时约 194 / 168 / 158 / 180 / 134 / 224 秒。

第一次远端验证暴露了上传大小测试写死 Windows 包名的问题：Linux 生产包会先返回平台不匹配，开发后端则先返回版本错误。测试现使用宿主平台与固定旧版本 `0.0.0`，保留 73 MiB 上传和本地化错误断言；修复后重新运行上述完整 CI，全部成功。

## 2026-09-23 的流程与覆盖边界

- CI 并行执行两个 Go module、前端单测和完整发行目录构建。浏览器作业复用该目录，按用例分成六片，每片仍只有一个 worker，避免共享后端状态串扰。
- browser、e2e 与 production 用例在同一生产包上各执行一次；删除各分片中的重复 Go 构建、Vite 开发服务和独立烟测启动。失败仍保留 trace、截图和报告，不再全程录视频。
- 纯逻辑单测使用 Node，DOM 测试保留 jsdom、setup 与文件隔离。CI 固定两个 worker；本地按 CPU 数使用 50%。新增依赖 DOM 的 `.test.ts` 时需加入 `web/vite.config.ts` 的 `domTests`，`.test.tsx` 自动归入 DOM。
- browser 从 105 项收敛到 88 项：模型持久化、压缩与恢复不再重复所有主题/语言/宽度的笛卡尔积；仍覆盖各产品、运行时、两种主题、中英文和宽窄屏。移动导航完整遍历保留在 320/1023px，其他尺寸保留布局检查。
- 移动工作台测试保留草稿、布局和焦点断言；后台执行与并发会话由既有 e2e 覆盖。功能测试默认减少动画，导航反馈测试显式保留正常动画。
- 历史恢复测试将 1,000 次同构执行缩为 100 次、保留十批写入；首尾历史、未完成输入、两个产品 journal、冷启动与索引重建、索引大小比例及原始 journal 不变性断言全部保留。
- 仅说明文档变更跳过昂贵测试，但仍检查 whitespace 并生成 `Test and build` 必需状态；Skills Markdown 仍触发完整 CI。Go 依赖清单改变时检查两个 module 的 `go mod tidy -diff`。

当时 Release 等待**标签对应 SHA** 的 push/手动 CI 成功，并下载该次 CI 的发行目录。此门禁由下文 2026-10-16 的完整回归要求替代。五个平台仍在独立作业中并行打包，全部成功后才合并校验和并发布。

## 当前流程：按职责验证与分层浏览器回归（2026-10-16）

最近 30 天的 180 次 Actions 运行中，9 月已调整过流程，因此采用北京时间 10 月 2—15 日作为资源基线：63 次运行累计 1,631.5 个作业分钟，平均 116.5 分钟/天。Playwright 占 1,224.0 分钟，约 75%。这里相加各作业的开始至结束时间，不含排队，也不代表实际计费分钟。失败、取消和重试的实际资源消耗包含在负载基线中；比较单次流程提速时仍应使用成功运行。

- push / PR 运行现有 `@smoke` 用例，覆盖书籍创建与重载、写作保存与 Agent 编辑/撤销、General Agent 工具边界、游戏流式回合与分支，以及生产包的源码编辑器 worker、Review 加载。单片、单 worker，首个失败停止后续用例。完整回归的浏览器矩阵仍保留所有失败诊断。
- 每天北京时间 01:17 对最新 master 执行完整 CI；手动触发默认 `suite=full`，也可显式选择 `smoke`。完整浏览器回归改为三片，各片仍只有一个 worker，超时放宽至 35 分钟以容纳更大的分片。完整与 smoke 使用不同 concurrency group，新 push 不会取消正在执行的完整回归。
- `.github/scripts/ci-plan.mjs` 按实际 diff 选择职责。master push 比较同分支最近成功 push CI 的提交，避免连续推送取消前一次运行后漏掉未验证改动；PR 比较其 base，首次运行或完整 CI 验证所有职责。前端变更不运行 Go；Agent 变更同时验证 Agent 与根 module 消费方；Go 依赖清单或工作流变更扩大检查。前端单测仅在前端、工作流或完整 CI 中执行。未知产品路径和 Skills Markdown 保持产品检查，重命名也检查被移除的原路径。
- 更新器的 Windows/macOS/Linux 原生测试仅由更新器、其宿主/文件系统/版本依赖、相关入口、Go 依赖清单、工作流或完整 CI 触发。
- 文档、贡献指南、仓库宣传图片和独立评测工具不触发全套产品测试，但仍执行 whitespace 检查并生成 `Test and build` 必需状态。评测工具在同一 CI 中使用独立作业、按路径触发，完整 CI 也会验证它；其本地确定性模型和隔离后端不访问付费模型或真实用户数据，失败仍会使必需状态失败。
- 浏览器启动前必须完成 Go 检查、前端单测及全部 Playwright 用例收集；没有适用 Go 作业时允许其跳过。用例收集包含 production 项目，可在安装浏览器之前发现语法和导入错误。
- 模型设置、运行时切换及部分 Lore 流程的完整业务操作只验证一次；另一主题保留相关页面的宽窄屏、空状态或长文本检查。主题本身影响动画、语言或布局的独立场景继续保留，两种产品不合并成单一场景。

Release 只接受**标签对应 SHA** 的成功 `Full CI`（schedule 或手动完整运行），不会接受 push / PR smoke。没有完整 CI 或其产物过期时，明确触发该标签的 `suite=full`，只补跑一次；已有完整 CI 失败时阻止发布。构建产物同时保留生产包和相同提交的前端构建输入，供跨平台发布复用。完整 CI、发布等待和浏览器作业的超时均为基础设施边界。

本轮各项节省估算存在重叠，尤其是减少全量运行与减少分片安装，不能直接相加。最终收益应在变更推送后，按至少一周的实际作业分钟与失败复核成本重新评估。

```bash
# Inspect all production-package journeys without starting servers.
DENOVA_E2E_PACKAGE_DIR=/absolute/path/to/package pnpm --dir web exec playwright test --list
# Run ordinary CI smoke against a complete extracted native package.
DENOVA_CI_SUITE=smoke DENOVA_E2E_PACKAGE_DIR=/absolute/path/to/package pnpm --dir web exec playwright test
# Run one full shard; omit DENOVA_CI_SUITE or set it to full.
DENOVA_E2E_PACKAGE_DIR=/absolute/path/to/package pnpm --dir web exec playwright test --shard=1/3
# Verify path selection and the release gate without calling GitHub.
node --test .github/scripts/*.test.mjs
bash .github/scripts/require-full-ci_test.sh
```

### 本轮本地验收（Windows 原生）

| 检查 | 结果 |
| --- | --- |
| 工作流与门禁 | actionlint 通过；9 项路径/真实 Git/连续推送测试通过；发布门禁的复用、补跑、产物过期及失败阻断场景通过；必需状态覆盖文档跳过、工具失败、Go 跳过及浏览器失败 |
| 前端 | TypeScript、i18n 检查通过；169 文件、982 项单测通过；修正了工作区操作测试在 effect 发布新 Hook 快照前读取旧回调的时序问题 |
| 生产包 smoke | 最新 Windows 包上 11 项全部通过，约 40.7 秒 |
| 完整三片回归 | 317 项中 299 通过、15 条件跳过、3 失败；验证期间新增的两项工作台入口用例在更新 Windows 包后均通过，余下一项附件预览失败见下文 |
| 评测工具 | 四个正常场景、故意引入的双变体回归检测、报告与凭据清理、执行中取消验证通过 |
| 构建与平台 | 五平台完整打包及校验和、发布打包脚本测试通过；Windows updater 原生 integration test/vet 通过；macOS/Linux 仅交叉打包，未原生运行 |

本地验证时，工作区另有未提交的产品改动。附件用例 `web/tests/e2e/attachments.spec.ts` 的发送后预览在全新数据目录、无分片单独运行和最新 Windows 包上均复现：已发送消息只有图片文件名，缺少预览按钮。附件与会话产品逻辑未在本任务中修改；该用例仍保留在完整回归中。下面的 GitHub 验收只包含本任务已提交的代码，和上述工作区验证分别记录。

### GitHub 验收（2026-10-16）

CI 优化提交 `48875e380db7c5241da7beaf7000d424bcebbbf0` 已推送到 master。[日常 CI 38776704708](https://github.com/alfredxw/denova/actions/runs/38776704708) 首次运行即通过，未重试。远端干净 checkout 的 169 文件、981 项单测通过，307 项浏览器用例收集成功；smoke 的 11 项生产包用例全部通过。两个 Go module 的 test/vet、三平台原生 updater 检查、评测工具及完整发行目录构建均通过。

以最近一次成功的旧流程 [38573130938](https://github.com/alfredxw/denova/actions/runs/38573130938) 为单次比较基线。这次日常 CI 因修改工作流而验证了全部职责，尚未获得普通产品提交按路径跳过作业的额外收益。

| 指标 | 旧流程 | 新日常 CI | 减少 |
| --- | --- | --- | --- |
| 全部作业执行时间之和 | 58.33 分钟 | 17.55 分钟 | 69.9% |
| 浏览器作业执行时间之和 | 44.02 分钟（六片完整回归） | 1.83 分钟（单片 smoke） | 95.8% |
| 流程历时，含排队 | 12 分 52 秒 | 6 分 23 秒 | 50.4% |

同一提交的[完整回归 38776714411](https://github.com/alfredxw/denova/actions/runs/38776714411) 已结束：289 通过、15 条件跳过、3 失败，零重试。三片累计执行 39.33 分钟，所有作业累计 53.37 分钟，流程历时 18 分 44 秒。第一个分片的 103 项全部通过，其余失败为：

- `image-compaction.spec.ts`：Writing 压缩图片后第六轮 `5 accepted` 回复未显示。
- `context-compaction.spec.ts`：Writing 手动压缩前的 `recent turn complete` 回复未显示。
- `context-compaction.spec.ts`：Writing 自动压缩前的 `recent turn complete` 回复未显示。

前两项与优化前 [38736120942](https://github.com/alfredxw/denova/actions/runs/38736120942) 的用例和失败断言一致；第三项具有相同的回复不可见症状，尚未确认根因。这两个测试文件及其产品逻辑均未在本任务提交中修改。完整回归尚未通过，不能将这次运行作为成功的完整回归提速验收。附件预览用例在远端干净 checkout 上通过，与上文包含其他产品改动的本地工作区结果不同。

使用这次实际失败的 Full CI 验证发布脚本：即使同 SHA 的 push CI 成功，门禁仍退出失败且不产生可供后续打包的输出，没有补跑或发布操作。

每日负载按 10 月 2—15 日的 32 次 CI 文件范围回放：push 使用当时同分支最近已完成的成功 push 作为 diff 基线，PR 使用审计取得的文件列表，再用本次日常 CI 的各作业实测时长计价。回放共触发 30 次产品检查、根 module 27 次、Agent module 8 次、前端单测 25 次、三平台 updater 7 轮、评测工具 3 次；不需要前端单测的产品检查仍保留用例收集成本。

| 每日负载模型 | 作业分钟/天 |
| --- | ---: |
| 原工作流的历史实耗，含失败与取消 | 116.54 |
| 新日常 CI：回放 32 次，累计 362.60 分钟除以 14 天 | 25.90 |
| 每天一次完整 CI：采用本次实际运行成本 | 53.37 |
| 未改动的其他工作流：沿用历史日均 | 4.21 |
| 新流程预计合计 | **83.48** |
| 预计节省 | **33.06（28.4%）** |

这是一组路径回放与单次实测成本结合的估算，已计入每天完整回归的新增成本，没有将各项优化的独立收益相加。完整回归样本含失败等待时间；后续修复、重新验证的额外运行、runner 波动和实际提交频率都会影响结果，需要至少一周实际数据复核。日常 smoke 的缩减不代表完整用例等量提速；三片完整回归的流程历时也比旧六片更长。

## 本地验证（2026-09-23，Windows 原生）

工具链：PowerShell 7.6、Go 1.26.6、Node 24，打包脚本使用 Git Bash。GitHub 使用 Ubuntu 与 Node 22；本机耗时不能代替托管 runner 的最终验收。

| 项目 | 结果 |
| --- | --- |
| 前端单测基线 | 134 文件、778 项，原配置固定一个 worker：313.86 秒 |
| 前端单测优化后 | CI 的两个 worker 配置：118.88 秒（减少 62.1%）；134 文件、779 项全部通过。相比基线多一项现有工作新增的测试；默认本机并发约 60 秒 |
| 历史恢复测试 | 33.88 → 2.97 秒；两个产品子用例分别为 1.69 / 1.28 秒 |
| 浏览器与端到端 | 137 项；Windows 下 10 项按既有条件跳过，127 项通过（包含首次失败后的定向复核） |
| Go | agent module 全量 test/vet；根 module 全部受版本管理 package 的 test/vet 通过 |
| 工作流与打包 | actionlint、Bash 语法、五平台归档/覆盖/校验和测试、CI 门禁成功/等待/失败/缺失/过期场景通过；实际 Windows x64 完整打包及原生生产包浏览器验证通过 |

根目录存在已有的 ignored Go 草稿，直接 `go test ./...` 会收集它们；本地根 module 使用 `go test ./cmd/... ./config/... ./internal/... ./scripts/...` 与同范围 vet，覆盖全部受版本管理根 module 包。干净 CI checkout 继续使用 `./...`。

六组浏览器在同一 Windows 主机同时运行时发生资源竞争，首次有六项失败。修正故事加载、菜单关闭、收件箱 mock 与开场按钮统计时序后，定向复核六项在 36.6 秒内全部通过。除上文说明的重复模型执行段外，保留流程的行为断言不变，没有添加自动重试。

本地阶段按旧 CI 单项时长估算的六片耗时约 307 / 259 / 246 / 246 / 142 / 243 秒（最后一片另有两个生产包用例）；最终采用上文实际成功 CI 的 7 分 24 秒验收，已达到减半目标。Release 已完成流程改造与打包验证，总耗时仍需在下一次实际发布时确认。

## 本地入口

```bash
# 完整打包；不再隐式重复全部测试。
bash scripts/build-github-release.sh vX.Y.Z
# 单个平台；保留其他平台归档，重新生成当前版本校验和。
bash scripts/build-github-release.sh vX.Y.Z windows-x64
# 复用同一提交的已验证前端，跳过前端依赖安装与构建。
DENOVA_RELEASE_FRONTEND_DIR=/absolute/path/to/tested/web bash scripts/build-github-release.sh vX.Y.Z linux-x64
```

浏览器验证生产包使用 `DENOVA_E2E_PACKAGE_DIR`。同机并行验证需同时分配独立的 `DENOVA_E2E_RUNTIME_DIR`、后端端口和模型端口；runtime 目录只能位于 `web/test-results` 内。
