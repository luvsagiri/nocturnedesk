# NocturneDesk

> **内网自托管的轻量团队协作台**  
> **项目 → 文件夹（文档 + 资源）｜ 日历（每人每天的纯文本记录）｜ 看板（Todo/Doing/Done）**  
> 面向 5–10 人小团队，Go 静态单二进制 + SQLite，`docker compose up -d` 一条命令启动，全离线可运行。

---

## ✨ 核心特性

- **真正单二进制零依赖**：Go 1.22+ 原生 `net/http`，纯 Go 实现 SQLite (`modernc.org/sqlite`，**零 CGO**)，所有静态文件与模板由 `go:embed` 内嵌打包，不依赖外部网络与 CDN，无需任何 Node.js 构建链。
- **开放自助注册**：团队成员自主注册账号，无需管理员审批或建号；密码无复杂度死板限制，服务端 bcrypt 安全哈希。
- **可见性铁律**：凭项目邀请码加入，成员仅能访问自己创建和加入的项目。非成员访问任何接口、页面或文件流均直接返回 404，杜绝资产探测。
- **经典三栏独立滚动工作台**：
  - **左侧一体化目录树**：子文件夹与文件一体化排列，支持鼠标拖拽无级调节侧边栏宽度并自动记忆，36px 严整等高行高；
  - **中间正文阅读与多媒体舞台**：支持图片、PDF 原生内嵌渲染，音视频播放无缝切换且杜绝重叠发声；
  - **右侧 Typora 级多层次大纲**：自动从 Markdown 标题同步提取 H1–H4 目录大纲树，支持带呼吸间距的精准平滑滚动跳转；
  - **滚轮完全物理隔离**：三栏各自独立滚动（`overscroll-contain`），阅读正文滚轮滑动时左右两侧稳如磐石。
- **Typora 级 Markdown 与全量代码高亮**：原生支持 Markdown 实时就地编辑与渲染，内嵌 Highlight.js 对 Go、Python、JS、TS、SQL、Shell、JSON 等常见代码块及上传代码文件进行富彩语法高亮，并支持文档一键导出为 `.md` 文件。
- **全流式大文件上传与实时进度**：前端提供实时百分比进度条与容量感知，后端采用无缓冲流式写入，彻底消除大文件上传导致的“网络中断/连接重置”异常。
- **每人每天一条纯文本日历**：按月视图呈现，成员每天一条工作进展记录，互相可见；保持纯粹文本记录，不做繁复流程。
- **固定三列看板**：标准化 `Todo / Doing / Done` 三列，基于 SortableJS 拖拽跨列与列内排序，采用中值浮点算法，自动重排防精度耗尽；各列高度顶满视口并独立内部滚动。
- **全站 FTS5 全文搜索**：内置 SQLite FTS5 `trigram` 分词器，中文精准检索，应用层毫秒级同步，支持全自动 LIKE 优雅退化。
- **应急 CLI 工具**：免管理后台设计，提供 CLI 命令行一键重置遗忘密码：`./nocturnedesk user passwd <username>`。

---

## 🚀 快速启动

### 方式一：下载预编译可执行文件（最简，即下即跑）

前往 [Releases 页面](https://github.com/luvsagiri/nocturnedesk/releases) 下载对应系统的预编译压缩包：

- **Windows (x86_64)**：解压 `nocturnedesk-windows-amd64.zip`，双击 `nocturnedesk.exe` 即可启动。
- **Linux (x86_64)**：解压 `nocturnedesk-linux-amd64.tar.gz`，执行 `./nocturnedesk --data ./data`。

服务默认监听 `http://127.0.0.1:8080`。

---

### 方式二：Docker Compose

1. 克隆代码或下载 `docker-compose.yml`：
   ```bash
   git clone https://github.com/luvsagiri/nocturnedesk.git
   cd nocturnedesk
   ```

2. 启动服务：
   ```bash
   docker compose up -d
   ```

3. 浏览器访问：`http://127.0.0.1:8080`，即可直接注册首个账号开始使用。所有数据（数据库、上传文件、密钥）均持久化保存在 `./data` 目录中。

---

### 方式三：源码编译

构建纯静态单二进制仅需 Go 编译器（无 CGO 依赖）：

```bash
# 静态编译二进制
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o nocturnedesk .

# 启动服务
./nocturnedesk --data ./data --port 8080
```

---

## ⚙️ 配置项说明

所有配置均支持通过环境变量或命令行标志传递：

| 配置项 | 环境变量 | 命令行参数 | 默认值 | 说明 |
|---|---|---|---|---|
| HTTP 端口 | `PORT` | `--port` | `8080` | 服务监听端口 |
| 数据目录 | `DATA_DIR` | `--data` | `./data` | SQLite 数据库、上传资源和签名密钥的存储路径 |
| 单文件上传上限 | `MAX_UPLOAD_MB` | `--max-upload-mb` | `200` | 单个资源文件的最大上传限制（单位：MB） |
| 会话签名密钥 | `SECRET_KEY` | - | 自动生成 | 用于 HMAC Cookie 签名。若未指定，首次启动将在 `data/secret.key` 自动生成 |

---

## 🛠️ 管理与运维

### 找回与重置密码

由于系统不设全局管理员角色，若团队成员遗忘密码，可在服务器终端直接执行：

```bash
# 本地直接运行
./nocturnedesk user passwd <username>

# Docker 容器内执行
docker compose exec nocturnedesk /app/nocturnedesk user passwd <username>
```

按终端提示输入新密码即可完成重置。

### 数据备份与迁移

系统遵循极简自托管原则，无外部数据库守护进程。备份只需整体复制 `./data` 文件夹；迁移至新服务器只需将 `./data` 放入相同目录重新启动即可。

---

## 📄 开源许可

本项目遵循 [MIT License](LICENSE) 开源许可证。
