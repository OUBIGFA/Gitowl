# Node 后台与 Electron 迁移

Gitowl 的可见界面继续使用 MyGo。生产后台使用独立 Node 进程，直接复用项目已有的 AppStore、Dispatcher、Git 命令、账户、凭据、模型供应商及失败恢复实现。Go 负责窗口、系统服务和子进程所有权，不把这些业务重写成另一套近似实现。

## 当前结构

`MyGo → 已有 JSON-RPC 管道 → Node 业务层 → 随包 Git / 官方模型 SDK`

后台的文件对话框、系统浏览器、剪贴板、回收站、通知、主题与系统代理由 MyGo 执行。命令完成结果与平台交互使用独立消息，避免等待对话框的 Git 操作堵住对话框回复。退出先停止新请求和后台调度，再等待已接纳 Git 操作结算。

原生 UI 命令、状态投影、部分暂存、Git/LFS/hooks、冲突弹窗和恢复继续使用原实现。协议中的 `core: desktop/desktop` 标识业务来源，不表示进程仍运行 Electron。

## 直接复用的组件

| 能力 | 实现 | 适配范围 |
| --- | --- | --- |
| Git 与恢复 | 现有派生业务层、Dugite、Git for Windows | 运行宿主和平台接口；不更换 Git 引擎 |
| 数据库 | 现有 Dexie schema + IndexedDBShim + SQLite | 通过公开依赖接口提供真实持久数据库，保留事务和迁移 |
| 设置 | Node 官方 Web Storage | 保留原 localStorage 契约，文件位于新数据目录 |
| 系统能力 | MyGo 原生 API | 将真实结果和失败返回原业务调用方 |
| HTTP 与代理 | Node / Undici + MyGo netproxy | 系统/PAC/环境代理及系统信任证书，解析失败不静默直连 |
| 实时通知 | 原 Alive/StableSocket + Undici WebSocket + MyGo | 复用上游 Origin 规则；通知事件在 SQLite 设置中保留，支持重启后点击 |
| 图像 | ImageMagick magick-wasm、Photopea UPNG | 解码、方向及动画合成在独立工作线程执行 |
| Markdown 链接 | 原 marked/DOMPurify/过滤器 + JSDOM | 独立无脚本、无资源加载解析文档；不安装全局 DOM，不显示网页 |
| Copilot/自定义供应商 | 原 SDK 与项目管理逻辑 | 保留模型选择、权限、取消和草稿行为；Copilot 原生运行时仍随包提供 |

精确版本、来源、校验和许可见 [运行依赖登记](../third_party/native-runtimes.json) 与 npm 锁文件。新包不含 Electron、隐藏 renderer、Chromium 资源或调试 source map。源码中的上游对照入口和用于旧数据验证的开发工具不属于生产运行资源。

## 旧数据迁移

新数据库和设置位于原用户数据目录内的 `backend-data/`。原 `desktop-kernel/` 用户数据目录继续保留恢复记录、Copilot 数据、日志和 GUID；它与安装目录内的旧 Electron 运行文件是两个不同目录。

1. 检查新数据目录的迁移完成记录。
2. 若存在旧 IndexedDB/localStorage，使用旧安装内的引擎和新包的一次性导出脚本，通过原存储 API 导出。
3. 使用 Typeson 的结构化数据 preset 跨引擎传递日期、键和值，保留 store/index 定义及自动递增键进度。
4. 导入独立目录。全部完成后写入记录，再原子重命名为 `backend-data/`。
5. 新后台返回首个有效状态后，正式应用将安装目录中已识别的旧 `desktop-kernel/` 运行文件移入回收站。

失败时保留原数据和恢复材料。旧数据存在但旧读取引擎缺失时，不能静默新建空资料。回收失败保留旧运行文件并提示，不退回永久删除。新安装不需要旧引擎；新包本身不携带 Electron。后续启动使用迁移完成记录，不再调用旧引擎。

## 验证入口

- `npm run prepare:native-runtime`：核对输入、编译并组装 `output/native-runtime/backend/`，验证真实 Node、SQLite、图像像素与资源清单。相同输入和字节可复用对应成功记录。
- `npm run test:node-kernel`：组件和完整 Node 启动回归。
- `npm run test:kernel`：Node 组件、真实 Git 操作与生产 MyGo 控件集成。
- `npm run test:kernel:production`：针对已核对运行目录的 MyGo 集成。
- `npm run build:native`：本地固定安装器，要求暂存源码快照明确。

旧数据库验证使用新建的私有测试资料，不迁移、安装、升级或卸载用户已安装程序。MyGo 控件集成与真实桌面截图分别说明，二者不能互相替代。最终构建、检查和证据通过后，才将固定安装器作为本轮交付。
