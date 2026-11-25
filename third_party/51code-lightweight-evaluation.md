# 51code 轻量化对照

核查日期：2026-11-25。固定来源为 [51code v0.5.2](https://github.com/ysicing/51code/tree/5b4697a342c72824d63e269a97ea24963e69147d)。本记录基于源码及官方 Release 文件长度，不是跨客户端运行速度排名。本轮没有复制 51code 源码。

## 为什么小

| 已查证的事实 | 对 Gitowl 的意义 |
| --- | --- |
| 两者都使用 MyGo 原生 UI。51code 的 [Git 命令层](https://github.com/ysicing/51code/blob/5b4697a342c72824d63e269a97ea24963e69147d/internal/gitrepo/command.go) 由 Go 直接启动 Git | Gitowl 的 MyGo 主程序本身约 16.6 MiB。主要差距在 Node 业务层和附带组件，换皮不解决这部分 |
| [Windows 打包](https://github.com/ysicing/51code/blob/5b4697a342c72824d63e269a97ea24963e69147d/scripts/windows-git.mjs) 完整提供官方 MinGit、SSH、LFS；Linux 使用系统 Git | 不能以 macOS 约 12 MiB 的 DMG 对比 Windows 完整安装包。v0.5.2 Windows 安装器为 41,832,451 字节（39.89 MiB），便携 ZIP 为 62,510,532 字节（59.61 MiB） |
| [AI 请求](https://github.com/ysicing/51code/blob/5b4697a342c72824d63e269a97ea24963e69147d/internal/commitai/client.go) 使用 Go HTTP 请求，没有随包 Copilot 平台运行时 | 可把重型可选组件移出基础包；不能据此声称保留了 Gitowl 的 Copilot 账户与高级辅助流程 |
| [README 功能边界](https://github.com/ysicing/51code/blob/5b4697a342c72824d63e269a97ea24963e69147d/README.md) 标明暂无克隆入口、交互式 rebase 或凭据配置界面；远程操作沿用 Git 凭据，Pull 为快进 | 产品范围较小。Gitowl 不把删除已有账户、恢复或 Git 功能算作无代价减重 |

## 可借鉴的控制点

| 固定源码 | 做法 | Gitowl 当前状态与后续取舍 |
| --- | --- | --- |
| [read_cache.go](https://github.com/ysicing/51code/blob/5b4697a342c72824d63e269a97ea24963e69147d/internal/gitrepo/read_cache.go) | 同一提交复用文件与父提交列表；blob 按不可变对象 ID 缓存。跨仓库共享 8 项、4 MiB 总额，避免每个仓库各占一份 | 后续优先测量历史、差异、切换项目后保留的缓存；以总字节上限和淘汰规则约束，而不是只限制条目数量 |
| [history_stream.go](https://github.com/ysicing/51code/blob/5b4697a342c72824d63e269a97ea24963e69147d/internal/gitrepo/history_stream.go)、[command.go](https://github.com/ysicing/51code/blob/5b4697a342c72824d63e269a97ea24963e69147d/internal/gitrepo/command.go) | Git 输出流式解析；达到所需页后停止读取；单条记录和输出有明确上限 | Gitowl 已分页读取提交，下一步应核对大仓库搜索、全量引用及重复快照，不能把已有分页重复算成新收益 |
| [repository_background.go](https://github.com/ysicing/51code/blob/5b4697a342c72824d63e269a97ea24963e69147d/internal/app/repository_background.go)、[refresh.go](https://github.com/ysicing/51code/blob/5b4697a342c72824d63e269a97ea24963e69147d/internal/app/refresh.go) | 文件通知合并；没有有效变化时逐步退避到 8 秒；有变化才局部更新；避免刷新与写操作争用 | Gitowl 已有 200 ms 合并、60 秒本地校准、5 分钟元数据校准和快照分区。可继续实测“无变化通知”与非当前项目监听，不能盲目再加轮询 |
| [background.go](https://github.com/ysicing/51code/blob/5b4697a342c72824d63e269a97ea24963e69147d/internal/gitrepo/background.go) | 自动 fetch 显式禁止附带自动维护与 commit-graph 写入 | Gitowl 需在真实 Trace2 证据显示有附带维护时再缩短等待链；手动操作及用户维护策略单独保留 |
| [memory_diagnostics.go](https://github.com/ysicing/51code/blob/5b4697a342c72824d63e269a97ea24963e69147d/internal/app/memory_diagnostics.go) | 真实 Go 内存与堆诊断 | Gitowl 需分别量 MyGo、Node 和 Copilot，区分安装大小、空闲工作集及峰值；本轮包体变化不能当作内存收益 |

## 本轮采用与未采用

用户选择把 Copilot 改为首次启用下载。继续直接复用官方 `@github/copilot-sdk` 及其完整 Windows 平台包，使用锁文件的 SHA-512 和构建生成的逐文件 SHA-256。官方 SDK [支持显式指定运行时路径](https://github.com/github/copilot-sdk/blob/main/nodejs/README.md)；解包使用 [Windows 自带 tar/libarchive](https://learn.microsoft.com/en-us/windows/tar/)。没有新增依赖或另写归档解析器。

同时排除依赖源码映射、类型声明和编译缓存。基础包仍提供 Git、SSH、LFS、凭据 helper、Node、数据库及图像解码。Node、IndexedDB 兼容层和 Markdown DOM 解析器是下一阶段需要测量和逐步替换的层；本轮没有未经测量重写完整后端。

51code 仓库标注 `AGPL-3.0-only`，Gitowl 当前采用 MIT。这里记录结构与行为思路，不将其代码直接纳入 Gitowl；如后续决定直接合入代码，需先明确相应许可和分发安排。
