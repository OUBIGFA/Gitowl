# 同源二开参考与迁移取舍

核查日期：2026-11-06。本文记录源码调查和拟采用的行为，不表示 MyGo 迁移或桌面验收已完成。下载的源码只保存在忽略目录；未把其他 fork 的实现整包导入 Gitowl。文件修订与内容校验见 [research-sources.json](research-sources.json)。

## 参考来源

| 项目 | 核查修订 | 同源关系与用途 | 许可 |
| --- | --- | --- | --- |
| [desktop/desktop](https://github.com/desktop/desktop) | `a95e13a7429f4e7f9e45d7b38a68bc1a3eb972f7` | 本地源码比较基线；另外通过 GitHub 文件 API 核对了该提交的 `byok.ts` 与本地 Git blob 相同 | MIT，仓库已有 LICENSE |
| [desktop-plus/desktop-plus](https://github.com/desktop-plus/desktop-plus) | `8c9bb791c1845539965689542036a7a6b3b736dd` | GitHub fork 元数据及上游 forks 列表均确认同源；账号模型、仓库账号选择、凭据会话、自定义模型供应商、生成流程和测试的主要参考 | MIT，已读取该修订 LICENSE |
| [GI-STANDARDS/GITmaxed](https://github.com/GI-STANDARDS/GITmaxed) | `14c988c131a098c57df05cf894fef6ba295725c6` | 元数据 `fork=false`；保留 Desktop 源码结构和版权，README 明确称基于 Desktop 修改。按源码衍生项目对照，不能称为已确认 Git fork 关系 | MIT，已读取 LICENSE |
| [sj817/github-desktop-plus](https://github.com/sj817/github-desktop-plus) | 本轮只读取 README | CDP 运行时注入增强；不是完整 Desktop 源码 fork，不能替代同源参考或成为 MyGo 实现方案 | 本轮未完成许可及实现核查，不复制代码 |
| [egoist/mygo](https://github.com/egoist/mygo/tree/v0.7.4) | `v0.7.4`，`9bd34e1eb9525d9a1fe698aadc964c49c8ff8e7e` | 最新稳定版候选，Go 原生 UI 文档与源代码；本轮进行独立 Windows 技术探针 | MIT，尚未进入 Gitowl 发布构建 |

`perecorio/desktop-multiaccount`、`Ed-bb/GithubDesktop.fork` 已通过搜索发现，但未完成代码分析，不把它们计为实现依据。候选多不等于证据充分。

## 多账户：参考完整身份与调用链

Desktop Plus 的 [Account](https://github.com/desktop-plus/desktop-plus/blob/8c9bb791c1845539965689542036a7a6b3b736dd/app/src/models/account.ts) 用 endpoint、用户 ID 与 login 比较身份，并包含 GitHub、Enterprise、GitLab、Bitbucket、Forgejo、Gitea 的 API 类型。其 [AccountsStore](https://github.com/desktop-plus/desktop-plus/blob/8c9bb791c1845539965689542036a7a6b3b736dd/app/src/lib/stores/accounts-store.ts) 将元数据、凭据会话和安全存储分离，处理刷新、过时的 401、退出时注销会话，以及同一主机冲突的 API 类型。不是把 token 简单放进一个账号数组。

仓库通过 [getAccountForRepository](https://github.com/desktop-plus/desktop-plus/blob/8c9bb791c1845539965689542036a7a6b3b736dd/app/src/lib/get-account-for-repository.ts) 传递 endpoint 和 `loginForApi`；[trampoline/find-account.ts](https://github.com/desktop-plus/desktop-plus/blob/8c9bb791c1845539965689542036a7a6b3b736dd/app/src/lib/trampoline/find-account.ts) 用远端 origin 与 login 找凭据。Gitowl 应借鉴账号分组、显示当前仓库身份和显式选择的交互，将现有账户协调逻辑接到 MyGo 原生控件。

有两处行为不能照搬：

- Desktop Plus 的 [getAccountForEndpoint](https://github.com/desktop-plus/desktop-plus/blob/8c9bb791c1845539965689542036a7a6b3b736dd/app/src/lib/api.ts#L3436) 在非 strict 模式下回退到该 endpoint 的首个账号；[findAccountForRemoteURL](https://github.com/desktop-plus/desktop-plus/blob/8c9bb791c1845539965689542036a7a6b3b736dd/app/src/lib/find-account.ts) 最后会忽略 login 再找主机匹配。其 [测试](https://github.com/desktop-plus/desktop-plus/blob/8c9bb791c1845539965689542036a7a6b3b736dd/app/test/unit/find-account-test.ts) 明确包含提供 `foo` 却返回 `joan` 的情况。这不足以证明严格的多身份隔离。
- GITmaxed 的 [getAccountForRepositorySmart](https://github.com/GI-STANDARDS/GITmaxed/blob/14c988c131a098c57df05cf894fef6ba295725c6/app/src/lib/get-account-for-repository.ts) 使用显式 login、父目录/祖父目录名、endpoint 首账号三级选择。前两级只比较 login，未在该函数内约束 endpoint；同名跨主机账号或移动目录会带来歧义。不能把目录约定当认证身份。

保留当前基线更严格的 `endpoint + login` 归一化身份、仓库显式绑定、精确 origin 校验、token 租约、同仓库请求合并和不同仓库交互排队。认证账号和 Git author/email 是两个设置，切账号不隐式改写作者。该选择来自对 fork 差异的核查，并非另写一套简化认证。

必须验收：同主机双账号、不同主机同名账号、仓库分别绑定、克隆前选账号、退出后绑定保留/清除、刷新期间退出、取消选择、并发 pull/push、后台操作不弹抢焦点的登录框、Git/LFS/hook 认证一致。现有 `repository-account-test`、`accounts-store-test`、`credential-sessions-test`、`trampoline-credential-helper-test` 和账号 UI 测试作为保留基线；本轮未声称全部通过。

## 自定义供应商：沿用 BYOK，补齐调用条件

Desktop Plus 的 [byok.ts](https://github.com/desktop-plus/desktop-plus/blob/8c9bb791c1845539965689542036a7a6b3b736dd/app/src/lib/copilot/byok.ts) 与 Gitowl 比较基线的同文件仅有钥匙串服务品牌名差异，已执行文件 diff 核实。应直接保留该共享实现的行为，而非新建通用 HTTP 请求包装来替代它：

- OpenAI-compatible、Azure、Anthropic 类型；OpenAI completions/responses 格式；Azure API version。
- provider UUID、明确的模型 ID/显示名称、可选 reasoning effort 和请求超时。
- API key、bearer、无认证三种模式；密钥留在 OS 安全存储，provider JSON 不含密钥。
- provider/model 复合选择键、旧配置兼容、删除供应商后的失效选择处理。

[CopilotStore](https://github.com/desktop-plus/desktop-plus/blob/8c9bb791c1845539965689542036a7a6b3b736dd/app/src/lib/stores/copilot-store.ts) 负责会话配置、超时、取消后清理、生成结果 JSON 解析、仅传入 diff 的内容边界及仓库提交规则。相关 [BYOK 测试](https://github.com/desktop-plus/desktop-plus/blob/8c9bb791c1845539965689542036a7a6b3b736dd/app/test/unit/copilot-byok-test.ts) 和 [prompt 测试](https://github.com/desktop-plus/desktop-plus/blob/8c9bb791c1845539965689542036a7a6b3b736dd/app/test/unit/copilot-commit-message-prompt-test.ts) 是迁移依据；只读测试源码不等于运行通过。

明确缺项：当前 Gitowl 的 `_generateCommitMessage` 先要求有效 GitHub 账号；`createClient` 也取得 GitHub 凭据。仅增加“自定义供应商”表单仍不能满足没有 GitHub 登录、使用自带 key 的流程。应分离认证上下文和模型供应商选择，保留现有 diff 选择、amend 基线、取消、超时、密钥失败、结果校验和手动确认提交语义。

已检查的同源 fork 生成接口没有独立输出语言字段。不能声称已找到现成完整实现。Gitowl 要增加独立、持久化的输出语言配置，至少提供中文、English、自定义语言；与界面语言分开，明确传给生成请求，保留 JSON 的 `title`/`description` 键、代码标识符和原始 diff。语言指令与仓库规则冲突时应有明确优先级并通过测试，不能依赖某段全局 prompt 恰好生效。

## MyGo 与 UI 迁移边界

[MyGo 原生 UI](https://mygo.egoist.dev/docs/ui) 是 Go 控件和布局，Windows 使用 Direct3D 11；不是替换 React CSS 的组件库。已核查固定版本 `docs/ui/views.md`、`layout.md`、`styling.md`、`testing.md`、`diff-viewer.md` 与 `docs/i18n.md`。MyGo 提供原生差异视图、列表、输入、菜单、主题、测试器和 ICU 消息目录，应用应使用这些 API。

现有 AppStore/Dispatcher 在 renderer 内运行，依赖 DOM、localStorage、Dexie、Electron IPC、回调和模型类。因此迁移先分离命令/状态/进度/交互请求合同，再按功能分区替换 UI。不能把现有 IPC 当作完整 Git 后端，也不能以少量 Go git 子进程命令代替 hooks、LFS、凭据协调、操作锁和失败恢复。

[入口清单](../references/upstream-features.json) 当前包含 311 个声明：87 个菜单事件（含明确标注的测试事件）、97 类弹窗、15 类横幅、6 类展开面板、2 个仓库视图、9 个设置页、79 个 IPC 通道、3 类 URL action（含载荷字段）、13 类失败重试动作。它能检测声明增删及签名变化；不能单独证明右键菜单、快捷键、平台可见性、运行行为和无障碍已保留。所有迁移状态仍为 `not-assessed`。

界面方案采用“仓库/分支/同步 → 修改与历史 → 文件/diff/提交”层级。常用操作可见；高级历史操作收纳进菜单，保留快捷键和可发现性。中文和英文是两套可切换的完整界面，同一时刻只使用一种当前语言；菜单、弹窗、按钮、提示和无障碍标签一起切换，禁止逐项中英并排或混排。提交输出语言独立于界面语言。中文文案明确提交保存本地版本与推送上传远端的区别。本地使用不要求登录。正文保持可读尺寸，使用 MyGo 主题、系统字体、清晰分隔和用户 logo 的少量紫色强调。

首次安装和首次启动必须从系统显示语言自动选择支持的语言，不先要求用户手选。Windows 应使用 MyGo 已封装的 `GetUserPreferredUILanguages`/`GetUserDefaultUILanguage`，不以时区、地域格式或终端环境变量冒充系统 UI 语言。中文、英文语言资源同时进入安装包，界面只呈现当前语言；中文区域/脚本变体需要与安装器一起验证。未支持语言回退英文。手动选择后使用 MyGo `App.SetLocale` 的持久化机制；“跟随系统”保持自动状态，启动和升级不覆盖用户选择。

该能力有上游依据：MyGo v0.7.4 的 `docs/i18n.md`、`internal/windows/locale.go` 和 `cmd/mygo/languages.go` 已核对；`docs/distribution.md` 说明 Windows NSIS 安装器按语言目录生成资源并自动匹配系统显示语言。已确认 v0.7.4 的 `cmd/mygo/nsis.go:411–412` 将“程序正在运行”和“程序仍在运行”两条提示硬编码为英文。正式接入前必须通过有源码基线、校验和回归测试的有限补丁或已修复的官方版本补齐，不能仅凭标准 NSIS 页语言正确就认定安装器全部完成。

[Impeccable](https://github.com/pbakaus/impeccable) 与 [unslop](https://github.com/heydamir/unslop) 本轮仅作为 UI 方法参考；其 DOM/CSS 检查、CDP、浏览器注入不能验收原生 MyGo 界面，未安装或运行它们的外部脚本。原生界面仍需键盘、中文输入法、缩放、深浅主题和真实窗口验收。
