# 同步与刷新性能：成熟方案调查

核查日期：2026-11-20。调查先于代码改动，用户授权内部评估后直接实施。下列为源码和官方文档核查，不是第三方客户端之间的速度排名；未将参考代码复制进 Gitowl。

| 方案与固定来源 | 查证事实 | Gitowl 取舍 |
| --- | --- | --- |
| [VS Code，50b6495](https://github.com/microsoft/vscode/blob/50b64954152416d2a04cefe3add95c593b1e4192/extensions/git/src/repository.ts)，MIT | `onFileChange` 区分工作目录和 Git 元数据；合并通知，在空闲和聚焦时刷新；不对每次通知都启动完整扫描 | 借鉴调度方式。复用 Node 原生 `fs.watch`，200 ms 批次、选中优先、占用延后；60 s 校准本地状态，5 分钟校准完整元数据。包含 worktree 独立与公共元数据目录，不新增文件监视依赖 |
| [Lazygit，54cc6af](https://github.com/jesseduffield/lazygit/blob/54cc6af1d77b5a6cec3dc9ad0b203afdb4ad3451/pkg/commands/git_commands/sync.go)，MIT | 直接执行 Git fetch/pull；后台凭据提示与前台分开。[配置](https://github.com/jesseduffield/lazygit/blob/54cc6af1d77b5a6cec3dc9ad0b203afdb4ad3451/docs/Config.md) 分开控制自动获取、刷新、外部变化及分支自动快进 | 保留成熟 Git；普通同步只做用户请求的 Git 工作及必要状态刷新。去掉附带的其他本地分支自动快进，保留高级辅助提交事务原行为 |
| [Git v2.56.0 fetch 配置](https://github.com/git/git/blob/v2.56.0/Documentation/config/fetch.adoc)、[remote 配置](https://github.com/git/git/blob/v2.56.0/Documentation/config/remote.adoc)，GPL-2.0 | `fetch.followRemoteHEAD=always` 可在现有连接中跟随远端 HEAD；`remote.<name>.followRemoteHEAD` 优先 | 直接使用随包 Git 2.56.0.windows.4 的成熟能力。命令级配置，不落盘；删除额外 `remote set-head -a` 请求。真实仓库验证默认分支改变和用户显式配置 |
| [go-git，75f3ca8](https://github.com/go-git/go-git/blob/75f3ca83333c62fa9603b8b4f6b339bd3e9ec9f4/COMPATIBILITY.md)，Apache-2.0 | pull 仅支持快进；LFS 未支持；worktree 与 partial clone 有限制 | 本轮不采用为全功能 Git 引擎。另写补偿逻辑会扩大维护范围，且不能证明网络传输更快 |

## 内部方案评估

1. **保持上游所有行为，仅调小定时器**：不能消除多余网络连接、附加查询等待和其他分支更新；高频轮询还会增加占用，不采用。
2. **复用成熟 Git，缩短当前业务路径**：复用已验证的 Git/凭据/LFS/hooks 和恢复，删除无必要的附带工作，有明确回归与对照方法；本轮采用。
3. **立即重建全部 Go/libgit2 后端并移除 Electron**：可能降低宿主资源，但需要迁移账户、存储、Copilot、代理、深链和操作恢复；没有证据表明这是本轮获取/拉取耗时的主要来源，不采用整套重写。

采用方案 2 的当前实现仍经由 Electron 宿主，Git 运算由随包 Git 完成。未来可以替换宿主，须以独立测量和保留功能验收为依据。

## 已查证的原开销与修改点

- `app/src/lib/stores/git-store.ts` 的 `fetchRemote` 和 `app-store.ts` 的 `performPull` 原来在 Git 传输后再次执行 `remote set-head -a`。改为同一次 fetch/pull 更新默认分支信息。
- `app-store.ts` 的 `performFetch`、`performPull` 及普通 push 收尾原来自动快进其他分支，并同步等待分支保护 API。改为保留其他本地分支，保护资料异步补充；迟到结果须匹配当前账户、仓库和分支。
- `app/src/native-kernel/startup.ts` 原来每 5 秒遍历所有本地项目。改为变化通知和低频校准，仍用真实 Git 读状态。
- `renderer.ts` 原来对每次 presenter 通知立即投影、编码并推送。改为一帧最多一次待发送快照；菜单动作、对话回复和命令完成结果不经合并器。

Git 获取不会改工作目录；拉取仍遵循用户 merge/rebase 配置、hooks、LFS 和冲突恢复。不用缓存伪造同步成功，不自动推送，不改用户已安装程序。实测及验证见 [运行性能记录](../references/runtime-performance.md)。
