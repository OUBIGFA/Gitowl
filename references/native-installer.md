# Gitowl 原生测试安装器

产品版本固定为 `0.1.0`，标识为 `io.github.oubigfa.gitowl`。测试迭代使用内部构建号、Git 源码树和 SHA-256 区分，不改变用户数据目录。界面使用 MyGo，生产后台使用同进程 Go；完整结构及旧数据迁移见 [后台说明](native-backend.md)。

## 本地构建

所有测试、编译和安装器打包在本地完成。线上只保留不编译应用的源码与仓库检查。发布时上传已核验的本地安装包及记录，发布本身仍需明确授权。

使用 `.node-version` 和依赖登记对应的 Node/npm 工具链，准备 Go 1.27.3，按锁文件执行 `npm ci`。本地缓存位于 `.local/`。暂存本轮可审查源码后执行：

```powershell
npm run build:native
```

固定输出：

- `output/release/Gitowl-0.1.0-Setup.exe`
- `output/release/Gitowl-0.1.0-Setup.exe.sha256`
- `output/release/build-info.json`

构建入口检查暂存源码树、运行时输入和最终安装器哈希。临时包放在 `_temp/native-packages/<构建号>/`，同一构建号不重复覆盖。安装包校验完成后才替换固定文件。该入口不提交、不推送、不创建标签或 Release，也不执行用户安装版的安装、升级和卸载验收。

## 运行目录

```powershell
npm run prepare:native-runtime
```

`output/native-runtime/backend/` 包含官方 Git/SSH/LFS、Copilot 下载校验清单、原生图片解码器、一次性旧数据导出工具及许可证。Sqlitecore.dll 单独作为 MyGo 资源。包内不含 Node、Electron 或 JavaScript 业务 bundle。

`third_party/native-runtimes.json` 登记实际输入字节，`runtime-receipt.json` 记录全部运行文件哈希。只有输入与所有输出一致才复用。冒烟从实际运行目录启动，验证初始状态、EOF 请求结算、Git、SQLite、原生模块和实际图像像素。

`GITOWL_SKIP_RUNTIME_SMOKE=1` 只在用户明确要求时使用。构建会记录 `runtimeSmoke: skipped-by-request`，不能写成通过。默认执行或复用相同输入下的有效成功记录。

Copilot 使用锁定官方 SDK，原生组件首次使用时下载并核验，详见 [后台说明](native-backend.md#copilot-首次使用)。构建仍验证本地官方输入以生成完整文件清单，不把约 155 MiB 的组件复制进基础安装包。平台数据和凭据继续使用 Gitowl 自身的用户目录与系统凭据命名空间。

## 验证与可选记录

```powershell
npm run test:kernel
npm run test:repo
npm run check:repo
```

`test:kernel` 验证 Go 业务层、真实 Git、旧格式迁移和生产 MyGo 控件；仓库、远端和用户资料均隔离。真实桌面交互与截图在私有测试桌面执行，不抢占用户桌面。

开发中可用 `npm run test:kernel -- --package ./internal/gitcli` 或 `--run TestNativeRefresh` 缩小检查范围，重复 `--package` 可检查多个模块。局部测试与基准不更新完整构建回执。完整回执同时绑定原生源码/资源、MyGo 补丁和准备脚本；继承的 `GOFLAGS` 会被明确拒绝，防止隐藏筛选影响验收。详见 [模块验证约定](architecture.md#并行开发与验证)。

可选来源绑定验收记录仍由 `script/native-acceptance.ts` 校验：

```powershell
npm run build:native -- -AcceptanceReceipt <验收记录路径>
```

记录必须对应当前源码、运行时和真实结果。独立审查与同一助手自行复核须区分。缺失可选平台记录不会阻止固定包构建或已授权上传，但代码、功能和 UI 质量检查仍需完成。

`build-info.json` 的 `originalCore: desktop/desktop` 表示业务来源；`fullFeatureParity: false` 表示未宣称完整上游功能一致。编译成功不能替代功能验收。当前没有代码签名，也没有配置公开自动更新源。

## MyGo 与界面约定

锁定 MyGo v0.10.0。有限补丁、来源和校验见 [mygo.json](../third_party/mygo.json)。升级时重新核对补丁，不修改全局 Go 模块缓存。图标、ArcMD 和传递依赖按各自登记分发。

界面支持英文、简体中文和繁体中文。首次启动匹配系统显示语言，显式选择跨重启保留；生成说明语言独立设置。编辑器和内容搜索的单文件上限为 8 MiB，超限文件继续出现在文件树并提示使用外部编辑器。

获取、拉取、保存和推送保持直接可见。保存版本只生成本地提交；保存并推送只有在本地提交成功后才推送。取消、失败、hooks、冲突和未提交改动继续通过原业务流程处理。

新包升级时使用旧安装的一次性读取能力迁移原资料，再将旧运行文件放入回收站。测试交付不会实际执行用户安装版的迁移或生命周期动作。
