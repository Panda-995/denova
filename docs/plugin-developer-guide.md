# Denova 插件开发手册

更新：2026-10-10，v0.6.0 工作区实现。接口以当前代码和运行实例的 OpenAPI 为准。

配套：[扩展能力开发手册](extension-development.md) · [游戏开发手册](game-developer-guide.md) · [HTTP API](plugin-platform-api.md)。

资源中心的「插件与游戏」管理已安装的插件与游戏；工作台承接源码开发。插件可以组合 Agent 工具、用户命令、独立面板和 Project 一级页面；游戏交付有独立存档生命周期的作品。Skills 与公开 Agents 继续由各自页面管理。插件不能访问写作正文、写作选区、编辑器内部或任意宿主组件；一级页面可以提供自己页面的受限上下文。

## 开发骨架与参考实现

完整示例源码供开发参考，不作为创建选项，默认不安装、不启用。创建源码项目、检查打包和准备隔离预览均不会将示例加入已安装扩展，也不会改变默认游戏；安装后才成为可用扩展。

| 示例源码目录 | 示例 | 演示能力 |
| --- | --- | --- |
| `internal/extensions/creative-toolkit/package` | 创作工具箱 | 文本统计与批量整理、独立笔记、资料库、图像模型槽、工具集、设置与权限 |
| `internal/platform/assets/starters/plugin` | 通用组合骨架 | 命令参数表单、面板保存草稿、独立 Agent 工具读取同一份项目内容 |

「创建插件」会自动复制通用骨架和共享 SDK。手动复制骨架时，还需从 `internal/platform/assets/sdk` 复制 `runtime.mjs`、`client.mjs`、`client.d.mts`；Node 后端要求 22 或以上。

## 声明命令、面板和 Agent 工具

贡献目录由清单计算，列出入口时不启动代码。`contexts` 只接受 `writing`、`game`、`general`，必须非空；`tools[].agentContexts` 默认为空，表示不自动暴露给宿主 Agent，但仍允许用户命令或显式依赖调用。

```json
{
  "apiMajor": 1,
  "minHostVersion": "0.6.0",
  "views": [{ "id": "main", "source": { "kind": "static", "path": "index.html" } }],
  "contributes": {
    "tools": [{ "id": "read-draft", "definition": "tools/read-draft.json", "agentContexts": ["writing", "general"] }],
    "commands": [{ "id": "inspect", "titleKey": "inspect", "contexts": ["writing", "general"], "target": { "kind": "tool", "id": "read-draft" } }],
    "panels": [{ "id": "board", "titleKey": "board", "contexts": ["writing", "game", "general"], "viewId": "main" }]
  }
}
```

这是清单片段；完整包还需身份、双语 locale、权限和分发白名单。`titleKey` 在中英文资源中均须存在。命令目标只能是工具或本包面板；外部工具用 `pluginId/toolId` 并在 `requires` 声明。工具的英文描述供模型使用；面向用户的参数表单复用 inputSchema，字段使用 `x-titleKey` 和可选 `x-descriptionKey`，不能拿英文模型描述替代翻译。格式不适合声明式表单的复杂交互放入面板，不引入另一套 UI DSL。

用户在写作、游戏和工作台的项目操作入口发现贡献；扩展详情的「打开插件」先选择 Project。工具命令不经过模型，支持输入校验、结果复制和取消。相同 Project、插件和面板重复打开会聚焦原页面；不同项目分别绑定。面板可收起，切换产品页面后仍保留原项目，不能跟随当前项目隐式换绑。

同一插件和 Project 的交互共享一次激活；每个面板或命令拥有独立消费者、凭证和取消范围。关闭一个页面不会停止其他页面，最后一个消费者退出后停止后端。通过 `client.setState({ busy, dirty })` 通知宿主关闭保护；原始协议使用相同的 `denova:state` 消息。浏览器正常卸载也释放消费者；进程异常退出由宿主关闭处理，不承诺崩溃页面自动恢复。插件不能拒绝用户强制关闭。

## 项目配置与内容

项目配置沿用 Project Store 的 `config.toml`，仅增加 `[extensions]` 下的 `disabled_plugins` 和 `[extensions.models]`。新项目默认不额外停用已全局启用的插件，模型绑定默认为空。模型保存的是用户 profile ID，不含密钥：`builtin/assistant` 提供文本 Agent，`<pluginId>/<slotId>` 提供声明的图像槽。一级页面的私有 Agent 可声明自己的文本 modelSlot，不注册为公开 Agent。必需槽未配置时阻止启动，可选槽缺失时保留其他功能；面板用 `/capabilities` 区分未授权、范围不适用和未配置。

Project 界面修改模型、发行设置或依赖后，已有运行继续使用冻结值；新增消费者遇到变化返回 `RUNTIME_RESTART_REQUIRED`，关闭旧面板后再打开。其他插件的无关模型绑定不影响此运行。全局停用、项目停用、卸载或撤销权限会停止受影响的运行及其依赖图，不影响其他 Project 的项目级设置。

面板和 Agent 工具共同编辑的作品数据使用 `/assets/document` 与 `/assets/upload`，保存于 `stores/<store_dir>/extensions/<pluginId>/content/`。JSON 写入必须提供上次读取的 revision；冲突保留本地内容，让用户重读处理，不自动覆盖。文档由插件声明自己的格式版本；`dataDir` 是执行范围数据，不能替代跨会话共享的项目内容。

项目插件菜单提供「导出插件内容」和「导入插件内容」：ZIP 只包含该插件的 JSON 与媒体，导入同身份插件的空内容目录，已有内容或正在运行时明确拒绝。它不包含代码、设置、模型密钥或 Agent/Story journal，也不冒充整本书/整个 Project 的导出。复制完整 `.denova` 时，稳定 ProjectID、不可变 Store 目录和相对资产引用共同保证可移动性。卸载和升级不删除作品内容。

## 在 App 中开发

1. 在「资源中心 → 开发扩展 → 创建插件」，填写名称和可选需求。创建后进入工作台，打开源码项目和普通开发会话；填写的需求会作为第一条开发消息发送。
2. 默认在应用数据目录的 projects 下创建新目录，自动生成稳定包 ID。插件与游戏各自生成一个通用开发骨架，不选择模板；高级选项只用于指定包 ID 或父目录。已有同名目录不会被覆盖。
3. 开发骨架在 Project 根生成 denova.plugin.json、源码和 DEVELOPMENT.md。ProjectID 是稳定身份；受管 location 持久化为规范相对路径。源码草稿只在工作台显示，安装不会创建新 Project；资源中心的「插件与游戏」的「打开源码」可关联多个独立源码副本。
4. 工作台「更多开发操作 → 源码清单」直接编辑并自动保存清单，保留未编辑字段；文件和源码版本沿用项目编辑器与版本历史。含根清单的已有 Project 自动显示开发工具，损坏清单保留修复入口。同一项目可通过显式子目录绑定开发多个扩展。
5. 工作台普通 Agent 会自动关联 extension-development Skill、当前所选源码位置和最近构建/测试反馈。反馈仅附在后续消息中，最多保留 16,384 字符，超出时保留末尾并标记省略；完整构建输出仍在终端中。切换扩展来源不会混用反馈。
6. 工作台主操作为「试运行」和「发布到本机」。发布只确认名称、说明和版本等基本信息，保存源码编辑器后检查并安装，启用声明的必需能力；模型、测试参数和逐项授权不进入发布表单。可选能力在扩展设置中管理。需要构建的项目通过「更多开发操作 → 构建」在可见终端执行；关闭构建窗口停止该终端。检查验证文件、清单、schema 和引用，不执行代码。发布完成后打开已安装扩展，详情页可导出其准确冻结版本；本机设置、测试存档和未发布源码不会进入安装包。对外分发由用户自行处理。
7. 试运行使用开发 Project 及独立测试数据。后续源码修改需要重新检查并开启新预览；同版本号的源码变化也可安装。已安装快照和真实存档不会随源码编辑改变。模型选择属于运行实例，扩展通用设置由资源中心的「插件与游戏」统一管理。

## Project 一级页面

插件可声明 `contributes.pages: [{ id, titleKey, viewId, agent? }]`。安装启用后加入一级导航，点击绑定当前 Project；停用、卸载后移除。页面使用既有 view、消费者与生命周期，切换写作或游戏时保留，切换 Project 时释放。只选中当前目的地，不新增全局创作模式。

可选 `agent` 使用 `{ instructions }`，instructions 使用英文且最多 16 KiB，说明页面任务和业务工具的用法。宿主直接复用 Project 创作 Agent 的前端、会话、配置、Skills、工具过程、子 Agent 与运行时控制；可用能力由所选运行时决定。插件不提供独立语言模型槽、聊天实现或 Agent profile，也不为侧栏申请 `agents.run`。业务工具在 `contributes.tools[].agentContexts` 中声明 `writing` / `general`，通过现有宿主工具桥接调用；图像模型仍使用插件 image 槽。

页面调用 `client.setContext(value)` 提供至多 32 KiB 的当前页、选区与参考资料。宿主仅接受当前 frame/origin 的消息，在用户提交标准创作请求时冻结为 `page_context: { plugin_id, page_id, data }`；后端检查 Project 插件启用状态并解析已安装页面的 instructions。上下文是用户数据，不授予工具或模型权限。页面指引与数据进入同一轮上下文查看、canonical journal 和恢复链路，稳定提示词前缀不受影响。页面任务不会自动选择小说 Skill 或小说插图预设，显式 Skills 仍可使用。

Agent 操作完成后宿主发送 `denova:refresh`，页面重读作品。会话归整个 Project 所有；每个插件页面独立记住选中的会话与输入草稿，切页不换会话，插件升级或模型切换不改变会话身份。旧试用版 `platform-*` 页面聊天在首次打开会话 Store 时保留 `.pre-project-agent.bak` 原始备份，并将完整消息和工具上下文转入普通 Project 会话；旧执行状态不会恢复或重放付费任务。游戏私有 Agent 保持原机制。

页面可调用 `client.setPresentation({ title, contextLabel, referenceCount, suggestions, actions })` 同步页面信息和创作输入框。`suggestions` 为 `{ label, prompt }[]`，点击只填入输入框，仍由用户发送；`actions` 为 `{ id, label }[]`，点击向原页面发送 `denova:page-action`，在事件 `detail.id` 中返回动作 ID，可用于打开插件自己的参考资料选择器。展示消息最多 8 KiB，标题/标签/动作 ID 最多 200 字符，最多 4 个建议（各 2000 字符）与 4 个动作；不携带权限，也不替代模型上下文。页面自行提供当前语言的文案。

插件页采用全高内容区与可收起、可调宽度的宿主 Agent 面板，不增加跨页面顶栏。插件自行设计作品标题、业务菜单和图像模型选择。宿主通过 `GET /images/models` 提供安全的模型目录，插件在生成请求中传 `modelId`，无需为切换模型重启插件。启停、权限及插件 schema 自定义配置统一在资源中心管理。左右收起仅改变布局，保留画布、会话及输入草稿；共享 Agent 的对话模型仍在会话内选择。

页面可在自己的侧栏调用 `client.openTab('lore')`，打开当前 Project 的原生设定 Tab。宿主复用写作的资料目录、搜索、编辑、自动保存、批注与引用，保留同一个 Agent 会话和输入草稿；返回前完成资料保存，插件 iframe 保持挂载。设定打开时，发送给 Agent 的页面数据为 `{ surface: 'lore', itemId: string | null }`，替代隐藏画布的页码、选区和业务快捷操作。“引用到 Agent”进入现有对话草稿，不修改插件作品的长期参考资料。该接口仅供一级插件页面使用，不接受 Project ID，不向插件返回资料内容或授予 `library.read/write` 权限；插件配置仍在资源中心管理。

作品可声明 `projectContent: ["comic"]`，并申请 `project.content.read/write`，使用 `/project-content/*` 保存到当前 Project ContentRoot 的 `comic/`。JSON、图片历史与上传参考图都属于作品；路径相对且可移动，不另建 Project 类型。写入沿用 Project 文件修订检查、变更锁与自动版本服务；图片仅可新增，相同字节重放可接受，已有不同内容拒绝覆盖。资料库继续使用 `/library/*`，按 stable item ID 引用，共享角色、场景与画风。旧 `/assets/document` 仍承载插件私有共享内容；其专属 ZIP 导出不包含 Project 的 `comic/`，作品按 Project 文件与版本流程管理。

已安装插件的 Project/Session 图像任务与 `generated` 素材统一归属同一 Project、同一插件，页面可用原 commandId 查询宿主 Agent 工具发起的任务并采用结果。commandId 应使用 UUID 等跨会话唯一值。执行生命周期仍归属发起任务的运行实例与消费者；页面仅查询进度不会改变该归属。不同 Project、插件、游戏范围和预览不共享这些任务。

完整示例见 [denova-index 漫画工坊](https://github.com/alfredxw/denova-index/tree/main/examples/comic-studio)。

## 从 GitHub 安装与更新

「资源中心 → 添加资源 → GitHub」接受公开的 `https://github.com/owner/repository` 地址，可填写分支、标签、完整 commit 和包所在子目录；默认使用默认分支和仓库根目录。每个包目录恰有一种 Denova 清单。同仓库的游戏与插件分别定位、检查和安装。

检查先解析并固定 commit，再下载源码归档，按 `distribution.files` 收集安装文件并冻结候选包。安装只使用已检查字节。安装无需系统 Git，也不创建 Project；源码归档不包含 Git 历史。下载与展开各不超过 256 MiB，归档不超过 10000 项、单文件不超过 16 MiB；拒绝不符合跨系统规则的路径、大小写冲突、符号链接和特殊文件。暂不支持私有仓库认证、SSH 或自动拉取子模块、Git LFS 内容。

归档直接从 GitHub 官方 codeload 按固定 commit 下载；已有完整 commit 时不再请求提交解析。默认分支与分支提交的 API 响应使用有界内存 ETag 缓存，每次重新检查都会向上游验证，缓存不参与安装或恢复。「设置 → 资源中心」可选填 GitHub Token，保存在用户级 `config.toml` 的 `market.github_token`；留空时依次读取 `GITHUB_TOKEN`、`GH_TOKEN`。Token 仅发送给 GitHub API，不发送给 codeload 或注册点；更换 Token 会清除对应 API 缓存。此设置提高公开仓库元数据请求的额度，不增加私有仓库下载支持。限流提示在 GitHub 提供恢复时间时显示该时间，也可改用本地 ZIP 导入。

作者应提交可直接运行的文件或构建产物。需要构建时，选择「导入源码到工作台」创建独立受管 Project，保留整个仓库的共享构建输入和所选子目录。导入不执行代码；依赖准备、编译由作者声明的配方和既有可见终端负责。构建后重新检查安装。导入记录的 commit 表示基础源码，最终产物以内容摘要为准；再次导入会创建独立副本，不覆盖本地修改。

详情页显示当前版本、所属资源包、仓库、ref 和 commit。「检查并更新已有安装」通过所属 Installation 预览并确认整包变化；混合包成员不会被单独替换。更新新增权限须明确同意，已有授权按当前发行保留。更新按上游提交判断，确认时固定该提交；更新过程中包身份变化或原安装已被替换会拒绝安装。同版本号可以包含新的源码快照，版本号由作者用于表达兼容性。已有游戏存档继续引用原发行；普通 Agent 会话的后续执行直接采用当前已安装插件。新依赖默认采用当前已安装插件并校验版本范围，不在历史发行中自动择优。旧快照仍用于恢复，界面不展示发行历史列表。

源码初始化失败时保留已创建 Project 和文件，进入工作台检查，不删除用户工作。目录显示名和清单中英文名称可分别修改。Node 程序拥有当前系统账户权限；平台范围凭证不等于操作系统沙箱。

资源中心的「插件与游戏」「使用配置与授权」提供独有设置表单、恢复默认值和独立权限管理。各发行独立保存于 settings/<releaseId>/settings.toml；保存校验、并发保护、备份、预览隔离及运行快照规则见[扩展设置标准](extension-settings.md)。修改设置在下次启动生效，修改权限会停止受影响实例。卸载保留源码、会话、发行、设置和数据。

## HTTP 工具参考示例

创作工具箱源码位于 `internal/extensions/creative-toolkit/package`。

专属行为测试 `tools.test.mjs` 和公开平台 API 集成测试 `integration_test.go` 位于同级扩展目录。复制包到独立开发项目时，还需从 `internal/platform/assets/sdk/` 复制 `runtime.mjs`；它是平台共享协议代码，不属于创作工具箱业务实现。

准备后的开发目录包含：

```text
denova.plugin.json
server.mjs
runtime.mjs
tools.mjs
tools/*.json
settings.schema.json
defaults.toml
locales/zh-CN.json
locales/en-US.json
```

开发目录还提供通用浏览器连接示例；只有 distribution.files 列出的文件和清单进入发布包。该示例无需下载依赖，构建配方为 node --check server.mjs。

ID 为 example.text-tools 的参考示例公开六个工具：count-characters、clean-text、read-notes、save-note、search-library、illustrate，分别归入 text-utils 和 creative-resources 工具集。笔记追加到宿主按会话或 Story 分支隔离的数据目录，相同 requestId 重试不重复写入；单作用域上限 1 MiB，超限明确报错。批量整理逐项报告错误。资料库、图像和写操作由可选权限控制；illustrate 需要调用方绑定本插件的 illustrator 图像模型槽，没有绑定时返回 NOT_CONFIGURED，普通文本工具仍可使用。工具统计 Unicode 码点，默认包括空白；在扩展设置中启用「忽略空白字符」后，新启动会跳过空白。定义使用 JSON Schema 2020-12，声明英文模型描述、输入、可选结构化输出和 effect（pure、read、propose、write）。宿主在调用前后分别验证输入及结构化输出。Agent 工具适配器使用现有参数修复与执行引擎。

提供器收到 POST /tools/count，HTTP JSON body 直接是工具输入；成功返回：

```json
{ "content": "The text contains 3 Unicode code points.", "data": { "count": 3 } }
```

工具 ID 使用 `[a-z][a-z0-9_-]*`，插件最长 57 个字符；宿主直接以 `plugin_<tool.id>` 注册给模型，不添加 hash、不替换字符、不截断。游戏私有工具使用 `game_<tool.id>`，含前缀最长 64 个字符。建议插件使用有业务含义的 ID，如 `comic_inspect`。安装、更新及批量导入会拒绝不同插件的同名工具，并报告工具名和双方插件 ID；禁用插件及保留发行仍占用名字，同一插件更新可复用自己的名字。卸载后释放名字，再安装时重新校验。历史会话保留原始工具调用记录。

公共引用是 pluginId/localId。工具集必须引用本包已有工具；跨插件引用需要 requires 中明确列出对应贡献。游戏私有定义使用 local:id，不注册为其他游戏可见能力。

## 清单和发布

需要进入资源市场时，在公开仓库默认分支根目录提供包含该插件的 `denova-pack.json`，提交可安装文件，并添加 GitHub topic `denova-package`。索引按计划自动发现，市场会标注来源；这不代替安装检查与授权。子目录、指定 ref 或 ZIP 来源继续支持手动注册，详见[资源包发布指南](https://github.com/alfredxw/denova-index/blob/main/docs/publishing.md)。

清单使用 denova.plugin.json，至少声明 manifestVersion: 1、id、严格语义版本 version、apiMajor: 1、minHostVersion: "0.6.0"、中英文 name、permissions 和 contributes。最低宿主版本与 API 主版本分别校验，不兼容时在检查与激活阶段拒绝。包及其他贡献 ID 使用小写字母、数字、点和连字符；builtin、local 保留。工具 ID 遵循上述模型工具名规则。一个产物根只能有一种清单。

- distribution.files 是文件或目录白名单；未列出的依赖、入口或定义会使检查失败。
- runtime.backend 支持 protocol: denova-runtime-v1 与 launch.kind: runtime、runtime: node、entry、args。
- 插件图像调用优先使用 `/images/models` 和显式 `modelId`，模型选择及作品状态属于插件。已有 image 槽位仍可用于声明式启动配置；插件文本对话复用宿主创作 Agent，不声明私有 text 槽。游戏仍可声明 text/image 私有模型槽。槽位 titleKey 必须存在于中英 locale 文件，不公开 provider 配置。
- settings 引用分发内的 JSON Schema 与 defaults TOML，可选 uiSchema 提供声明式布局；字段必须有中英文标题。详见[扩展设置标准](extension-settings.md)。
- requires 声明插件 ID、语义版本范围和所需贡献。运行时使用准确发行，不按加载顺序替换。
- 按实际调用声明 agents.run、tools.invoke、pluginData 等权限，纯前端插件不必声明不用的能力；消费者直接调用或游戏 NPC 使用 `effect: write/propose` 的工具还需显式授予 `tools.write`。尚未实现的能力会明确拒绝。

## 写作与游戏共用工具

插件管理统一在资源中心的「插件与游戏」中完成。启用、授权并通过项目策略后，仅 `agentContexts` 包含当前场景的工具进入宿主 Agent；工具集与成员不重复暴露。新执行读取当前已安装版本及其设置，现有会话同样采用最新配置。正在执行的任务使用已经装载的工具；暂停期间修改相关版本或设置后，原任务可能无法原地恢复，需要保留历史并按当前配置重新发起。坏插件不会阻止其他正常工具进入上下文；明确依赖坏插件的能力显示归属错误。

三个入口和委派子 Agent 使用同一 Toolset 适配器。`pure/read` 工具按只读执行，`write/propose` 按外部变更沿用宿主 Agent 审批；插件结果回到原 Agent 的模型上下文，写作内容与游戏回合仍由原流程接纳。第三方游戏私有 NPC 通过定义中的 `tools/toolsets` 引用相同插件，并受游戏已授予权限限制。公开 Agent 定义、动态 context 和编辑器面板仍不属于本版插件贡献。

工具 schema 的检查不启动插件代码；第一次实际调用才启动后端，本轮结束或取消后清理。数据按 Project、Product Session 或 Story 分支隔离。插件启停与当前版本归 installed.json，参数归 settings/<releaseId>/settings.toml；会话 JSONL／Story JSONL 不保存插件选择、版本或参数副本。插件进程路径、凭证和连接信息只存在于运行时。

安装使用已检查字节，内容摘要区分同版本号的不同快照。旧快照保留，停用立即停止受影响的运行并阻止新启动；重新启用后才能启动。缺少可用依赖的游戏不会出现在新建故事线的可选列表中。卸载保留数据与冻结快照。

重新安装相同发行可以调整授权；宿主先停止受影响的实例并撤销旧凭证，再保存新的授权快照。源码和发行字节保持不变。

## 后端启动协议

宿主以 Node 22 或更高版本启动已冻结入口，通过 stdin 的一行 JSON 发送 type: bootstrap、protocol、context、connection、packageDir、dataDir、tempDir、hostToken。纯静态面板无需 Node。路径仅是当前宿主运行时投影，不应写入业务身份。平台凭证和内部服务凭证分别使用，均不得写入 URL、argv、日志或发行包。处理工具请求时，调用宿主 API 必须转发 `X-Denova-Consumer`，将异步任务归属原页面/命令；`runtime.mjs` 的 requestHost 已处理转发。

提供器监听独立 loopback 端口，stdout 第一行返回：

```json
{ "type": "ready", "protocol": "denova-runtime-v1", "port": 42000 }
```

宿主再使用 hostToken 验证 GET /__denova/ready 返回 denova-runtime-v1。后台其他入口也必须验证该凭证。启动等待有基础设施超时；Agent 运行没有固定总时长或迭代上限。stderr 和后续 stdout 进入本地日志。

stdin 收到 shutdown 或关闭后停止服务。Windows 通过 Job Object 管理进程树；其他桌面系统使用进程组。当前变更在 Windows 原生验证；macOS 与 Linux/WSL 仍需目标平台验证。

需要组合资料库、资源、图像或现有 Story 时，使用[扩展能力开发手册](extension-development.md)中的授权接口。自定义游戏界面由游戏视图完整实现。
