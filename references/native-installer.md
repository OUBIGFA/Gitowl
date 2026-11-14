# Gitowl 原生测试安装器

产品版本固定为 `0.0.1`，应用标识固定为 `io.github.oubigfa.gitowl`，本地安装器固定为 `Gitowl-0.0.1-Setup.exe`。普通测试迭代不改变名称或用户数据目录，以 `yyyyMMddHHmmss` 内部构建号、Git 源码树和 SHA-256 区分包。该约定参考 OneSub 的构建交付方式，没有复制其 AGPL 应用代码。

## 构建

在根目录使用已有 Node/npm 锁定工具链完成 `npm ci`，准备 Go 1.27.3，再暂存本次可审查源文件。最终交付入口为：

```powershell
npm run build:native
```

**固定安装器必须附带当前源码的验收记录**。真实 Electron 内核装配原 `AppStore` / `Dispatcher`；首次运行、菜单、弹窗、统一设置与供应商管理已接入 MyGo。Windows CLI 已有 Gitowl 启动适配及 NSIS 安装、升级、卸载钩子。上述实现和局部回归不代表完整交付：全量测试、功能覆盖、独立 UI 审查与实际安装生命周期仍须通过。

验收记录由 `script/native-acceptance.ts` 绑定源码、运行时、可执行文件、真实命令输出和桌面证据。固定构建调用方式为 `npm run build:native -- -AcceptanceReceipt <验收记录路径>`。记录必须包含 17 组统一检查，以及同一源码的三轮通过审查、三语言 × 两主题 × 两尺寸的窗口检查和规定交互场景。独立审查记录要求审查者不同于实施者；同一助手复核必须使用 `single-agent-native-review`，记录实际实施者、复核原因和 `sourceReview`，不能宣称独立审查。两种方式使用相同的测试、截图和问题关闭门槛。缺失或过期的记录会在生成安装器前被拒绝。`-Preview` 另受 `third_party/native-runtimes.json` 中的预览资格约束，不能绕过固定交付验收。

原内核与 MyGo 控件的隔离验收入口为：

```powershell
npm run test:kernel
```

它编译真实 Electron main / 无 Web UI 挂载的 renderer，测试原数据库持久化和已接纳请求的 EOF 结果，再用生产 MyGo 视图验证文件选择、草稿、鼠标/快捷键提交、原 hook 取消/忽略和历史审阅。隔离 profile 与 Git 仓库都在 `_temp/`；不调用旧研究服务，不登录真实账号或发起付费请求。通过该局部入口不等于全部前端或安装器验收。

固定交付目录、身份、内部构建号和校验记录约定保持不变。运行时打包适配完成后仍须校验暂存源码快照、仓库边界、依赖与许可证、MyGo 有限补丁、原生桌面及安装器语言回归。构建入口不提交、推送、创建标签或发布 Release。

- 固定交付目录：`output/release/`。
- 固定文件：`Gitowl-0.0.1-Setup.exe`、同名 `.sha256`、`build-info.json`。
- 临时打包目录：`_temp/native-packages/<构建号>/`，同一构建号不重复写入。
- 复用运行时：`output/native-runtime/`；内容及输入校验相同才复用。
- Go/Node 缓存：`.local/`，不入 Git。Node 仅为构建工具，不进入内核资源。

### 原 Desktop Electron 运行时准备（不生成安装器）

```powershell
npm run prepare:native-runtime
```

该入口先检查锁文件、已登记 Electron dist 整树、Git、trampoline、process-proxy、printenvz、Copilot 平台包与 Koffi 字节；只在源码、锁定输入、二进制登记和全部产物（含许可证）均一致时复用。缓存失效时清理 `out/`，调用原 `app/webpack.production.ts` 的六个生产目标，再执行原 `script/build.ts` 的依赖、Git/凭据 helper、Copilot、emoji、平台资源与许可证复制规则；设置明确 `GITOWL_NATIVE_BUILD=1` 以从 MyGo 配置取得产品编译名称/版本（普通上游构建不变），不使用开发 seed-profile 或研究 `native-service.js`。原 `compile:prod` 硬编码 4 GiB heap 在当前 SDK 六目标构建中 OOM，适配入口使用同一配置/构建脚本并将 buildtool heap 增至 8 GiB，不改编译目标或内核。

产物布局为 `output/native-runtime/desktop-kernel/electron.exe` 和 `desktop-kernel/app/main.js`，安装后与 Gitowl 可执行文件同级，Go 与 CLI 均从 `desktop-kernel/...` 解析内核路径；Electron 按已匹配 `package.json.main` 的 app 目录启动，使官方 default-app loader 读取固定 `0.0.1` 产品版本（直接 main.js 文件启动会误报 Electron 版本）；隐藏 Chromium 仅加载 `native-kernel.html`，不打包兼容 React renderer。Electron 官方 MIT LICENSE / Chromium notices 原样保留；20 MiB Chromium notices 的公开材料按 UTF-8 换行边界拆为不足 8 MiB 的登记片段，拼接 SHA256 必须等于官方整文件（安装运行时仍保留一个完整原文件），遵守仓库单文件 10 MiB 上限。另附原 Git、npm allowlist 与原生模块许可证。`build-info.json` 明确 `originalCore: desktop/desktop`、`presentationComplete: false`、`channel: research-preview` 和 runtime inputHash；这不是发布或完整前端验收声明。

准备入口最后运行真实生产包 smoke（新建一次性空 profile、原内核 state/正常 shutdown、空 Web body、原 Git 与 Copilot/Koffi FFI 加载），不安装程序、不 seed profile、不访问真实账户或请求付费模型。实际 production 编译仍有 renderer/native-kernel 各四条 Copilot `createRequire` / Koffi 动态依赖 warnings，Bundle Analyzer 对 `out/174.js` 的 `import.meta` 报解析警告（报告仍生成），另有 license URL 弃用提示；不能称无警告，也未据此验证真实 Copilot 登录/会话。日志保存在 `_temp/kernel-reuse/runtime-packaging/`。官方 Electron archive 与 SHA256 清单须先缓存至 `.local/downloads/electron-v<锁定版本>-win32-x64.zip` / `electron-v<锁定版本>-SHASUMS256.txt`；登记工具核对官方清单与锁定 npm checksums 后逐文件比对 dist，缺失或字节不一致时明确失败，不下载替代来源。

Windows 的 Copilot SDK 仍启动原 `copilot-runtime.exe`。原生宿主通过自身的内部启动入口为它创建从首帧就隐藏的控制台，并原样转发 SDK 的输入、输出、错误和退出码。PowerShell 全局钩子继承同一控制台，避免启动其他命令时先分配可见控制台再隐藏。该入口复用现有 Windows job 生命周期；SDK 强制结束启动器时，运行时及钩子也结束。原 SDK、模型选择、认证、配置和钩子保留，未新增运行时二进制或修改第三方包。

源码/登记或父任务在编译中变化时拒绝写可复用 receipt，须待输入稳定后重跑。正常准备不会更新 `output/release/`；只有 runtime smoke 以及上述身份/CLI 成组验收通过后，才允许单独恢复 `_temp/native-previews/`。

`build-info.json` 记录测试包实际范围。`fullFeatureParity: false` 表示仍未完成所有上游功能，不能当作可发布版本。构建通过不等于桌面安装、升级或全部功能验收通过。当前没有 Windows Authenticode 签名，系统可能提示未知发布者；没有配置公开更新源。

## 实现边界

**Gitowl 仅重写前端，内核直接复用 `desktop/desktop`，所有功能和行为必须一致。** MyGo 默认启动器仅连接真实 Desktop 内核；低层 Node 服务保留为前期调查与回归材料，不是启动器选项或可交付内核。当前实现尚未取得完整交付验收记录；旧 UI 测试包不能作为当前源码的交付包。

共享运行时位于 `app/src/ui/app-runtime.ts`：原前端与原生内核 host 必须使用同一处上游 Store/Dispatcher 装配及错误处理链。原生表现层命令位于 `app/src/native-kernel/desktop-presenter.ts`，直接转发给原 Dispatcher，不持有另一个仓库、账户或业务状态缓存。Electron 的原存储、网络、IPC 和平台运行环境按需要保留为内核宿主；不得在隐藏窗口挂载 React 前端或将网页呈现冒充 MyGo。

MyGo 负责布局、本地化、原生控件和交互桥接。账户登录、Copilot、fetch/pull/push、普通/部分行提交、LFS/hook、stash、历史改写、冲突恢复、PR、通知、深链及所有平台行为必须逐项接到原内核并验证；不得把直接调用几个低层 Git 函数或声明清单一致当成完成迁移。

## 语言与来源

首次启动使用系统显示语言；显式选择保留到下一次启动。“跟随系统”恢复自动匹配。安装器和应用都带英文、简体中文、繁体中文资源，每次只显示当前语言。生成提交说明的语言使用独立设置，不与界面语言绑在一起。

当前锁定 MyGo v0.9.0。有限补丁覆盖安装器进程占用提示、差异编辑器菜单转发、原生编辑菜单角色、数字输入按钮的中英文读屏名称、系统菜单按键的消费标记、单实例启动消息边界和显式资源目录中的隐藏子目录复制。单实例补丁在完整 JSON 消息到达后解析，保留原尺寸限制与超时，避免 Windows 延迟连接结束信号时丢失 CLI/深链请求。补丁应用于忽略目录中的独立源码副本，不修改 Go module cache。来源、修订、补丁理由与校验见 `third_party/mygo.json`、`third_party/native-runtimes.json`；升级时重新核对每项补丁是否必要。完整桌面升级及交付验收以当前源码的实际结果为准。
