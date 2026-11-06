# 第三方来源与许可证登记

Gitowl 基于 [GitHub Desktop](https://github.com/desktop/desktop) 的公开源码建立。当前基线保留上游代码、测试、构建入口和 `.gitmodules` 中声明的来源；上游提交基线为 `a95e13a7429f4e7f9e45d7b38a68bc1a3eb972f7`。项目许可文本见根目录 [LICENSE](../LICENSE)。

| 来源 | 用途 | 许可证状态 | 登记要求 |
| --- | --- | --- | --- |
| `desktop/desktop` | Gitowl 的上游应用源码、测试和构建定义 | 上游 MIT 许可随仓库保留 | 保留版权和许可证文本；GitHub 商标与图标按上游声明处理 |
| `gemoji`、`gitignore`、`choosealicense.com` | 上游通过 `.gitmodules` 引用的资源 | 以各自仓库和快照中的许可证为准 | 更新 submodule 修订前核对来源、许可证和变更 |
| `vendor/*` | 上游随源码发布的本地依赖 | 以各组件自身许可证文件和 `package.json` 为准 | 不把上游应用许可证推定为传递依赖许可证 |
| MyGo | Gitowl 后续 UI 迁移目标 | MIT，已核查 v0.7.4 源码，尚未进入发布构建 | 接入时补齐传递依赖、许可证与构建锁定；本轮研究修订和文件校验见下方登记 |

同源二开的实现调查与取舍见 [fork-evaluation.md](fork-evaluation.md)，具体源码修订、用途和 SHA-256 见 [research-sources.json](research-sources.json)。这些登记描述研究输入，不把参考项目声明的功能推定为 Gitowl 已完成的功能。

小型公开测试夹具可以进入 `references/` 或现有测试夹具目录；模型、字体、安装包、真实用户数据、下载缓存和未确认许可的二进制资源放在被忽略的 `.local/` 或发布资产中。需要公开分发的第三方材料先补齐来源、版权、SPDX 标识、文件 SHA-256 和修改范围，再进入源码或构建流程。

许可证登记用于说明来源和分发边界，不替代法律审查，也不授予 GitHub、GitHub Desktop 或其他第三方的商标权。
