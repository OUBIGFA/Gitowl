# Gitowl 原生测试安装器

产品版本固定为 `0.1.0`，应用标识固定为 `io.github.oubigfa.gitowl`，本地安装器固定为 `Gitowl-0.1.0-Setup.exe`。普通测试迭代不改变名称或用户数据目录，以 `yyyyMMddHHmmss` 内部构建号、Git 源码树和 SHA-256 区分包。该约定参考 OneSub 的构建交付方式，没有复制其 AGPL 应用代码。

## 构建

Gitowl 的测试、编译和安装器打包全部在本地完成。发布时上传本地核验通过的安装包、同名 `.sha256` 和 `build-info.json`；不触发、重跑或等待线上构建。上游比较 CI 和 Release 草稿自动流程在 Gitowl 仓库禁用，源码中的上游任务仅保留作对照。线上保留仓库检查及无需编译的 JavaScript/TypeScript、Python 扫描；Go 检查和产品验收在本地运行。

仅运行本轮相关测试、不运行整包运行时冒烟时，在这次构建进程中设置 `GITOWL_SKIP_RUNTIME_SMOKE=1`。默认仍运行冒烟测试；显式跳过会输出提示，并在固定包 `build-info.json` 中记录 `runtimeSmoke: skipped-by-request`，不表示该检查通过。源码、依赖、资源清单与安装器校验保持不变。

在根目录使用已有 Node/npm 锁定工具链完成 `npm ci`，准备 Go 1.27.3，再暂存本次可审查源文件。最终交付入口为：

```powershell
npm run build:native
```

**固定安装器可附带当前源码的验收记录**。真实 Electron 内核装配原 `AppStore` / `Dispatcher`；首次运行、菜单、弹窗、统一设置与供应商管理已接入 MyGo。Windows CLI 已有 Gitowl 启动适配及 NSIS 安装、升级、卸载钩子。本地仍运行与改动相称的测试和 UI 审查；平台专项收据不作为固定包或线上发布的前置条件。不执行安装版生命周期检查，也不覆盖、升级或卸载用户已安装的 Gitowl。

可选验收记录由 `script/native-acceptance.ts` 绑定源码、运行时、可执行文件、真实命令输出和桌面证据。固定构建调用方式为 `npm run build:native -- -AcceptanceReceipt <验收记录路径>`。显式提供记录时，仍必须包含 17 组统一检查，以及同一源码的三轮通过审查、三语言 × 两主题 × 两尺寸的窗口检查和规定交互场景。独立审查记录要求审查者不同于实施者；同一助手复核必须使用 `single-agent-native-review`，记录实际实施者、复核原因和 `sourceReview`，不能宣称独立审查。两种方式使用相同的测试、截图和问题关闭门槛。缺失或过期的记录不会阻止生成固定安装器；显式传入收据时才会在本地构建中校验它。`-Preview` 另受 `third_party/native-runtimes.json` 中的预览资格约束；该预览检查与可选本地验收收据分开，不能把本地平台证据重新变成固定包发布门槛。

当前业务层与 MyGo 控件的隔离验收入口为：

```powershell
npm run test:kernel
```

它编译真实 Electron main / 无 Web UI 挂载的 renderer，测试原数据库持久化和已接纳请求的 EOF 结果，再用生产 MyGo 视图验证文件选择、草稿、鼠标/快捷键提交、原 hook 取消/忽略和历史审阅。隔离 profile 与 Git 仓库都在 `_temp/`；不调用旧研究服务，不登录真实账号或发起付费请求。通过该局部入口不等于全部前端或安装器验收。

固定交付目录、身份、内部构建号和校验记录约定保持不变。运行时打包适配完成后仍须校验暂存源码快照、仓库边界、依赖与许可证、MyGo 有限补丁和安装器语言回归。构建入口不提交、推送、创建标签或发布 Release。平台专项验收仍可在本地单独运行，但不阻断线上固定包发布。

- 固定交付目录：`output/release/`。
- 固定文件：`Gitowl-0.1.0-Setup.exe`、同名 `.sha256`、`build-info.json`。
- 临时打包目录：`_temp/native-packages/<构建号>/`，同一构建号不重复写入。
- 复用运行时：`output/native-runtime/`；内容及输入校验相同才复用。
- Go/Node 缓存：`.local/`，不入 Git。Node 仅为构建工具，不进入内核资源。

### 原 Desktop Electron 运行时准备（不生成安装器）

```powershell
npm run prepare:native-runtime
```

该入口先检查锁文件、已登记 Electron dist 整树、Git、trampoline、process-proxy、printenvz、Copilot 平台包与 Koffi 字节；只在源码、锁定输入、二进制登记和全部产物（含许可证）均一致时复用。缓存失效时清理 `out/`，由 `app/webpack.production.ts` 编译原生包使用的五个目标：main、native-kernel、crash、CLI 和 highlighter，再执行 `script/build.ts` 的依赖、Git/凭据 helper、Copilot、emoji、平台资源与许可证复制规则。`GITOWL_NATIVE_BUILD=1` 从 MyGo 配置取得产品编译名称/版本，并排除兼容 React renderer；不使用开发 seed-profile 或研究 `native-service.js`。编译进程使用 8 GiB heap 上限。

产物布局为 `output/native-runtime/desktop-kernel/electron.exe` 和 `desktop-kernel/app/main.js`，安装后与 Gitowl 可执行文件同级，Go 与 CLI 均从 `desktop-kernel/...` 解析内核路径；Electron 按已匹配 `package.json.main` 的 app 目录启动，使官方 default-app loader 读取固定 `0.1.0` 产品版本（直接 main.js 文件启动会误报 Electron 版本）；隐藏 Chromium 仅加载 `native-kernel.html`，不打包兼容 React renderer。Electron 官方 MIT LICENSE / Chromium notices 原样保留；20 MiB Chromium notices 的公开材料按 UTF-8 换行边界拆为不足 8 MiB 的登记片段，拼接 SHA256 必须等于官方整文件（安装运行时仍保留一个完整原文件），遵守仓库单文件 10 MiB 上限。另附原 Git、npm allowlist 与原生模块许可证。`build-info.json` 明确 `originalCore: desktop/desktop`、`presentationComplete: false`、`channel: research-preview` 和 runtime inputHash；这不是发布或完整前端验收声明。

准备入口最后运行真实生产包 smoke（新建一次性空 profile、原内核 state/正常 shutdown、空 Web body、原 Git 与 Copilot/Koffi FFI 加载），不安装程序、不 seed profile、不访问真实账户或请求付费模型。实际 production 编译仍有 renderer/native-kernel 各四条 Copilot `createRequire` / Koffi 动态依赖 warnings，Bundle Analyzer 对 `out/174.js` 的 `import.meta` 报解析警告（报告仍生成），另有 license URL 弃用提示；不能称无警告，也未据此验证真实 Copilot 登录/会话。日志保存在 `_temp/kernel-reuse/runtime-packaging/`。官方 Electron archive 与 SHA256 清单须先缓存至 `.local/downloads/electron-v<锁定版本>-win32-x64.zip` / `electron-v<锁定版本>-SHASUMS256.txt`；登记工具核对官方清单与锁定 npm checksums 后逐文件比对 dist，缺失或字节不一致时明确失败，不下载替代来源。

Windows 的 Copilot SDK 直接启动原 `copilot-runtime.exe`，不再经过原生控制台启动器。原 `CopilotStore` 为 SDK 指定应用数据目录下的独立 `copilot` 目录，避免读取用户在独立 Copilot CLI 中安装的全局钩子。这些钩子不受会话的 `enableFileHooks: false` 控制，会在生成说明时启动 PowerShell 等程序。账号认证、模型选择、提示与内存会话文件系统仍沿用原实现；不修改 SDK 或运行时二进制。回归使用真实 SDK、隔离用户目录和本地模型响应，分别验证默认目录会运行钩子、应用目录不会运行钩子，不访问真实账号或付费模型。

源码/登记或父任务在编译中变化时拒绝写可复用 receipt，须待输入稳定后重跑。正常准备不会更新 `output/release/`；runtime smoke、身份和 CLI 检查可作为本地证据，不是固定包或线上发布的必要条件。

`build-info.json` 记录测试包实际范围。`originalCore: desktop/desktop` 标识源码来源；`fullFeatureParity: false` 表示未声明完整上游功能一致，不再作为性能优先方案的发布否决条件。发布仍需明确授权及当前范围的功能验证。构建通过不等于全部功能验收通过；安装版生命周期检查不是发布门槛。当前没有代码签名，系统可能提示未知发布者；没有配置公开更新源。

## 实现边界

**2026-11-20 起，Gitowl 以快速响应为优先，不再要求严格复用上游内核或保留全部上游行为。** 允许复用其他成熟实现、替换内部模块及裁剪收益不足的重型功能；须先调研和测量，并保留核心 Git、凭据、本地改动保护与错误恢复。此次选择保留成熟 Git 命令和当前 Electron 宿主，直接精简同步及刷新路径。方案依据见 [运行性能调查](../third_party/performance-evaluation.md)，验证记录见 [运行性能](runtime-performance.md)。

当前共享运行时位于 `app/src/ui/app-runtime.ts`，装配 Store/Dispatcher 和错误处理链。原生表现层命令位于 `app/src/native-kernel/desktop-presenter.ts`。Electron 暂时继续承担存储、网络、IPC 和平台环境；这些是当前实现事实，不是今后必须保留的内核边界。界面仍使用 MyGo 原生控件。

MyGo 负责布局、本地化、原生控件和交互桥接。保留功能按实际用户行为验证；裁剪功能明确记录范围，不再要求逐项照搬上游入口。旧低层 Node 服务仍是研究材料，本轮没有切换到该服务；不把声明清单或编译通过当作功能验收。

差异看板收起时，文件改动页与历史页使用相同的居中宽度；展开后才使用右侧全部可用区域。两页共用同一个原生分栏、折叠开关与差异样式，左侧文件列表、草稿或历史记录保留原位。普通文件选择不自动展开，显式展开按钮或审阅入口才打开看板。标题和“纳入本次保存”区域保留 16 DIP 水平内边距，原生滚动条预留轨道空间。逐段上下文展开直接调用原 `text-diff-expansion`，保留原差异身份和行选择；MyGo 仅呈现上、下展开控件。

启动配置写入完成后，本地项目初始化改动数量，之后通过操作系统文件通知触发刷新；200 毫秒内的突发事件合并，选中项目优先，Git 操作占用时延后。工作树的独立 Git 目录与公共目录一起监听。每 60 秒校准本地状态，每 5 分钟校准完整元数据；失败监听每分钟重试，恢复时完整校准，失败写日志。普通文件改动只刷新选中项目的工作目录，元数据变化立即刷新仓库。后台计数不触发 fetch/pull。

“获取”和“拉取”复用 Git 同一连接更新远端 HEAD，移除额外的 `remote set-head -a` 远端请求。普通获取、拉取和推送不再自动快进未检出的其他本地分支；分支保护资料异步更新，不延长同步忙碌状态。专用辅助提交事务继续保留其已验证的发布收尾。用户显式设置的 `remote.<name>.followRemoteHEAD` 优先，应用不修改持久 Git 配置。

刷新使用已适配的 Dispatcher / AppStore。同一帧的多次状态变更合并为一次推送；命令结果与交互请求保持独立。原生表现层只生成当前文件页的上下文菜单，避免在改动页反复检查未打开的历史文件；工作目录、选择、菜单设置和操作状态未变化时复用原菜单的纯显示数据。执行菜单时仍用当前状态重新验证。

“保存版本”按本地摘要是否为空立即更新可点击状态，输入期间不等待草稿同步或后台规则校验。提交仍按队列先保存草稿，再执行原规则、警告和 Git 提交流程。MyGo 原生分体按钮的菜单用于选择“保存版本”或“保存版本并推送”，主按钮文字、提示和快捷键随选择一致变化，选择写入现有原生设置文件并在重启后保留。菜单按内容确定宽度，选择本身不执行 Git 操作。只有点击主按钮或使用快捷键后才保存；选择推送时，只有本地提交成功后才调用原推送流程。取消或提交失败不推送，推送失败保留本地提交并显示原错误。修改已推送提交时仍经过原强制推送确认。

## 语言与来源

首次启动使用系统显示语言；显式选择保留到下一次启动。“跟随系统”恢复自动匹配。安装器和应用都带英文、简体中文、繁体中文资源，每次只显示当前语言。生成提交说明的语言使用独立设置，不与界面语言绑在一起。

当前锁定 MyGo v0.10.0。有限补丁覆盖安装器进程占用提示、差异编辑器菜单转发、原生编辑菜单角色、数字输入按钮的中英文读屏名称、系统菜单按键的消费标记、单实例启动消息边界、固定分栏、差异滚动条和显式资源目录中的隐藏子目录复制。单实例补丁在完整 JSON 消息到达后解析，保留原尺寸限制与超时，避免 Windows 延迟连接结束信号时丢失 CLI/深链请求。补丁应用于忽略目录中的独立源码副本，不修改 Go module cache。来源、修订、补丁理由与校验见 `third_party/mygo.json`、`third_party/native-runtimes.json`；升级时重新核对每项补丁是否必要。完整功能验证及交付结果以当前源码的实际结果为准；平台专项验收可以作为本地证据，不是线上固定包发布的前置条件。

编辑器和内容搜索的单文件上限为 8 MiB。超限文件仍出现在文件树和文件名搜索中，打开时提示使用外部编辑器；内容搜索说明其大小范围。Markdown 网页链接交给系统浏览器，项目相对文件链接通过原有路径校验打开，不执行其他协议。输入法取消候选内容时恢复原文本和选区。

首次设置完成前，项目路径、克隆和仓库深链请求暂存；登录回调仍立即进入原内核。显式独立用户目录的开发启动不注册系统 OAuth 回调，避免测试覆盖已安装程序的处理器。正式安装器仍使用原有回调注册逻辑。
