# 发布前依赖修复

2026-11-20 的 npm 公告检查覆盖根构建依赖及 `app/` 的生产和开发依赖。修复由 npm 更新锁文件，以下兼容覆盖保留原 Desktop 内核及 Copilot SDK 版本。

| 依赖 | 处理与理由 | 公告 |
| --- | --- | --- |
| Copilot SDK 的 Zod | 在 SDK 1.0.27-preview.1 下覆盖到 4.6.6，修复超大集合校验的内存耗尽；SDK 和配套原生运行时版本不变。 | [GHSA-mrhx-c7xc-m3w5](https://github.com/advisories/GHSA-mrhx-c7xc-m3w5) |
| legal-eagle 的 Underscore | 上游工具仍固定 `~1.6.0`，局部覆盖到 1.13.8；由原许可证生成和生产打包流程验证兼容性。 | [GHSA-cf4h-3jhx-xvhq](https://github.com/advisories/GHSA-cf4h-3jhx-xvhq)、[GHSA-qpx9-hpmf-5gmw](https://github.com/advisories/GHSA-qpx9-hpmf-5gmw) |
| glob 9 及以上、minimatch 7 及以上 | 分别覆盖到 14.0.6 和 11.0.9；保留构建代码直接使用的 glob 7，检查较新工具链使用的异步和匹配接口。 | [GHSA-3g3m-2fjr-6pw4](https://github.com/advisories/GHSA-3g3m-2fjr-6pw4)、[GHSA-g3hv-75wg-m249](https://github.com/advisories/GHSA-g3hv-75wg-m249) |
| Babel runtime、semver | 使用已修复版本；旧 semver 6 的覆盖只作用于受影响范围，semver 7 按原依赖范围更新。 | [GHSA-968p-4wvh-cqc8](https://github.com/advisories/GHSA-968p-4wvh-cqc8)、[GHSA-c2qf-rxjj-qqgw](https://github.com/advisories/GHSA-c2qf-rxjj-qqgw) |
| Markdown 检查器的 KaTeX | 覆盖到 0.19.1。此 npm 包属于构建检查；原生编辑器登记的 KaTeX 字体资源不变。 | [GHSA-238p-pmpm-9mq7](https://github.com/advisories/GHSA-238p-pmpm-9mq7) |
| app 开发服务器 | 在原版本范围内更新 webpack-hot-middleware 和 ansi-html，移除开发入口的已知公告。 | 以 `npm audit --prefix app` 的当前结果复核。 |

覆盖项需要随依赖更新复核；当直接依赖已约束到修复版本时移除相应覆盖。检查使用 `npm audit` 和 `npm audit --prefix app`；公告数据库会变化，一次零公告结果不表示不存在其他安全问题。

MyGo 同步至官方 v0.10.0，九项有限补丁逐项复核并重新登记原始及修改后 SHA-256。API 包装文件由官方生成器生成。新版本的测试器会像操作系统一样拦截输入法合成期间的普通按键；原生 Markdown 编辑器因此补齐空合成文本的取消处理，并验证恢复原文本、样式、选区及撤销记录。
