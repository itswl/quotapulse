# QuotaPulse

<!-- Chinese reference documentation. -->

监控多个平台的余额或配额，算出还能用几天，快见底或消耗突然放大时发 Webhook；顺带管订阅续费提醒，扫描邮箱里的欠费、续费邮件，每周推一份消耗汇总。一个 Go 二进制里跑看板、API、进程内定时任务和 Prometheus 指标。

默认只开余额检查、Webhook 告警和 Web 看板；数据库、动态配置、订阅、Prometheus 用 `ENABLE_*` 开关按需打开。

## 快速开始

```bash
cp .env.example .env            # 填 WEB_API_KEY、WEBHOOK_URL 和各平台的 *_API_KEY
go build -o quotapulse ./cmd/quotapulse
./quotapulse -show-config    # 自检：每个密钥从哪来、缺什么、时刻怎么理解
./quotapulse                 # http://localhost:8080
```

或者直接跑容器：`docker compose up -d`。

## MCP

设置 `ENABLE_MCP=true` 后，服务会在 `/mcp` 暴露只读 Streamable HTTP MCP
端点，并复用 `WEB_API_KEY` 鉴权。它提供当前余额、订阅、邮箱扫描、定时任务、
健康状态和历史告警查询，不提供配置写入、立即刷新或立即扫描。

**没有配置文件**：环境变量里有 `DEEPSEEK_API_KEY` 就会自动监控 DeepSeek，阈值取 `DEEPSEEK_THRESHOLD`。
要一次管很多账户、想在页面上增删改，打开数据库动态配置。

`.env` 的值后面不要写行内注释，注释单独一行。

## 配置

两个来源，一个值只有一个家：

| 来源 | 放什么 | 生效方式 |
| --- | --- | --- |
| 环境变量（`.env` / K8s Secret） | 密钥、Webhook、数据库连接、功能开关、定时任务时刻；**设了 `{PROVIDER}_API_KEY` 就自动成为一个受监控项目，设了 `EMAIL_HOST` 就自动纳入邮箱扫描** | 改完重启 |
| 数据库动态配置 | 业务清单 `projects` / `subscriptions` / `email`，在页面上或用 API 增删改 | 需 `ENABLE_DATABASE` + `ENABLE_DYNAMIC_CONFIG`，即时生效 |

数据库里的清单排在前面，环境变量发现的追加在后面，**已经声明过的 provider 和邮箱不会重复添加**。只配环境变量就能跑；订阅提醒只有数据库这一个去处，必须开动态配置。

### 环境变量自动发现

| 变量 | 作用 |
| --- | --- |
| `{PROVIDER}_API_KEY` | 有值就监控这个平台，项目名即 provider 名 |
| `{PROVIDER}_THRESHOLD` | 告警阈值，不填则只看不告警（自检会提示） |
| `{PROVIDER}_OWNER_PROJECT` | 分组标签，可选 |
| `EMAIL_HOST` + `EMAIL_USERNAME` + `EMAIL_PASSWORD` | 三个都设了就纳入邮箱扫描；`EMAIL_PORT`（993）、`EMAIL_USE_SSL`（true）、`EMAIL_NAME` 可选 |

同一平台多个账号用 `{PROVIDER}_1_API_KEY`、`{PROVIDER}_2_API_KEY`，项目名自动变成 `volc-1`、`volc-2`，阈值对应 `VOLC_1_THRESHOLD`；多个邮箱同理用 `EMAIL_1_HOST`、`EMAIL_2_HOST`。

自动发现的项目在页面上是只读的，点编辑保存一次就会固化进数据库，之后以数据库为准；要移除它得先去掉对应的环境变量。

### 项目字段

在页面上填，或 `POST /api/config/project`。除 `provider` 外都可省：

| 字段 | 说明 |
| --- | --- |
| `provider` | 必填，见下表 |
| `threshold` | 低于它告警；不填永不告警，自检会提示 |
| `name` | 默认用 provider 名；动态配置里是唯一键 |
| `type` | 展示用，按 provider 推导：`credits` / `balance` / `quota` |
| `owner_project` / `enabled` | 分组标签 / 是否启用，默认启用 |
| `api_key` | 留空则读环境变量 `{PROVIDER}_API_KEY`；同一 provider 多个账号按出现顺序取 `{PROVIDER}_{序号}_API_KEY` |

| 平台 | `provider` | 密钥格式 |
| --- | --- | --- |
| OpenRouter、UniAPI、微信排名、TikHub、DeepSeek | `openrouter` `uniapi` `wxrank` `tikhub` `deepseek` | 普通 API Key |
| 智谱 GLM Coding Plan | `glm` | `id.secret`。查的是套餐剩余配额百分比，`threshold` 按百分比填，如 `10` |
| 火山引擎 | `volc` | `AccessKeyId:SecretAccessKey` |
| 阿里云 | `aliyun` | `AccessKeyId:AccessKeySecret` |

接一个新平台：大多数平台是「GET 一次、从 JSON 里取个数」，在 `internal/provider/` 下新建一个文件，
用 `provider.RegisterSpec` 声明几行就够了，参考 `deepseek.go`。需要签名的（火山、阿里云）自己实现 `Provider` 接口。

### 订阅与邮箱字段

订阅 `cycle_type` 为 `weekly` / `monthly` / `yearly` / `lunar_yearly`：周付 `renewal_day` 写 1-7，月付写 1-31，公历年付写 `"03-15"`，农历年付写农历月日 `"05-03"`。这样可以把订阅改作公历或农历的生日、节日提醒；每条提醒的名称就是通知标题中的订阅名。`alert_days_before` 默认 3，提醒当天也发送，`amount` 与 `owner_project` 可选。农历日期会按当年的实际春节和闰月规则转换为公历发送日。

邮箱要 `host` `username` `password`，`port`（993）和 `use_ssl`（true）可省。匹配关键词默认覆盖中英文的欠费、续费、停机用语，要改用环境变量：`EMAIL_ALERT_KEYWORDS` 整体替换，`EMAIL_EXTRA_ALERT_KEYWORDS` 追加，都是逗号分隔。

## 环境变量

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `WEB_API_KEY` | 无 | `/api/*` 的访问密钥；未设置时接口一律 503 |
| `ENABLE_MCP` | `false` | 是否在 `/mcp` 开启只读 MCP；复用 `WEB_API_KEY` 鉴权 |
| `WEBHOOK_URL` / `WEBHOOK_TYPE` / `WEBHOOK_SOURCE` | 无 / `custom` / `credit-monitor` | 告警机器人；类型 `feishu` `dingtalk` `wecom` `custom` |
| `{PROVIDER}_API_KEY` | 无 | 各平台密钥，见上表 |
| `BALANCE_REFRESH_INTERVAL_SECONDS` | `3600` | 看板刷新间隔 |
| `ALERT_SCHEDULE` | `09:00,15:00` | 真实告警检查时刻，逗号分隔，`off` 关闭 |
| `EMAIL_HOST` / `EMAIL_USERNAME` / `EMAIL_PASSWORD` | 无 | 三个齐全就自动纳入邮箱扫描，另有 `EMAIL_PORT` `EMAIL_USE_SSL` `EMAIL_NAME` |
| `EMAIL_SCAN_SCHEDULE` / `EMAIL_SCAN_DAYS` | `10:00` / `1` | 定时邮箱扫描时刻、覆盖最近几天（1-30） |
| `EMAIL_ALERT_KEYWORDS` / `EMAIL_EXTRA_ALERT_KEYWORDS` | 无 | 邮件告警关键词，逗号分隔；前者替换默认词表，后者追加 |
| `WEEKLY_REPORT_SCHEDULE` | `Mon 09:00` | 周报时刻，格式「星期 时刻」，星期可写 `Mon` / `周一` / `1`，`off` 关闭 |
| `BURN_RATE_WINDOW_DAYS` | `7` | 算日均消耗看最近几天 |
| `RUNWAY_ALERT_DAYS` | `7` | 按当前速率还剩几天就告警，`0` 关闭 |
| `SPEND_SPIKE_RATIO` / `SPEND_SPIKE_MIN_AMOUNT` | `3` / `1` | 今日消耗达日常中位数的几倍算突增、低于多少绝对值不报，比例设 `0` 关闭 |
| `ENABLE_WEB_ALARM` | `false` | 看板刷新和页面操作是否也发真实告警 |
| `ALERT_COOLDOWN_SECONDS` / `SUBSCRIPTION_ALERT_COOLDOWN_SECONDS` | `86400` | 同一告警的冷却时长，需数据库 |
| `MAX_CONCURRENT_CHECKS` / `RESPONSE_CACHE_TTL` | `20` / `300` | 并发检查数（1-50）、余额结果缓存秒数 |
| `ENABLE_DATABASE` / `DATABASE_URL` | `false` / `sqlite:///./data/quotapulse.db` | 历史记录与动态配置的前提；启动自动建表，支持 PostgreSQL、MySQL |
| `ENABLE_DYNAMIC_CONFIG` | `false` | 业务清单改从数据库读 |
| `ENABLE_HISTORY_API` | `false` | 历史数据接口、趋势图、历史告警邮件 |
| `ENABLE_SUBSCRIPTIONS` | `false` | 订阅提醒 |
| `CONFIG_ENCRYPTION_KEY` | 无 | 设置后数据库里的 `api_key` 和邮箱密码加密存储（`enc:v1:` 前缀），接受 Fernet key 或任意口令；`AUTO_ENCRYPT_ON_READ`（默认 true）把读到的旧明文回写成密文 |
| `ENABLE_PROMETHEUS` / `METRICS_PORT` | `false` / `9100` | 指标端口 |
| `WEB_PORT` / `WEB_ENABLE_CORS` / `CORS_ORIGINS` | `8080` / `false` / 无 | Web 服务 |
| `SHUTDOWN_DELAY_SECONDS` | `0` | 收到 SIGTERM 后先继续服务几秒再关，等负载均衡摘干净；K8s 里建议 15 |
| `LOG_LEVEL` / `LOG_FORMAT` / `LOG_FILE` | `INFO` / `text` / 无 | 日志；格式可选 `json` |
| `STRICT_DATABASE_ERRORS` | `false` | 数据库初始化失败时直接退出，而不是降级继续跑 |

值写错（如 `ENABLE_DATABASE=enabled`）启动即报错，所有问题一次性列出；留空视为未设置。

## 命令行

```bash
./quotapulse                        # 起 Web 服务与定时任务
./quotapulse -show-config           # 配置自检，有问题时退出码非零
./quotapulse -check -dry-run        # 跑一次余额检查，不发告警
./quotapulse -check -project 火山-主账号
./quotapulse -check-subscriptions
./quotapulse -check-email -email-days 3
./quotapulse -healthcheck           # 探测本机 /live，容器健康检查用
```

## 定时任务

都在 Web 进程内调度，容器里没有 cron，时刻按容器 `TZ`：

| 任务 | 触发 | 发告警 |
| --- | --- | --- |
| `dashboard_refresh` | 启动即跑，之后每 `BALANCE_REFRESH_INTERVAL_SECONDS` 刷新看板 | 仅 `ENABLE_WEB_ALARM=true` 时 |
| `alert_check` | 每天 `ALERT_SCHEDULE`，检查余额与订阅 | 是 |
| `email_scan` | 每天 `EMAIL_SCAN_SCHEDULE`，扫最近 `EMAIL_SCAN_DAYS` 天的邮件 | 是 |
| `weekly_report` | 每周 `WEEKLY_REPORT_SCHEDULE`，汇总一周消耗、跑道与待续费 | 是 |

任一任务上次失败，`/health` 返回 503 并在 `failed_jobs` 列出，`GET /api/jobs` 看详情。

## 消耗与跑道

余额历史是一串快照，相邻两点余额下降就是消耗，上升就是充值。据此算出**日均消耗**和**跑道**（按当前速率还能用几天），比静态阈值更早也更准：同样是 430 元，日烧 5 元和日烧 200 元完全是两回事。

在此之上有两类告警，都需要 `ENABLE_DATABASE=true` 攒历史，数据不足时自动沉默，退回纯阈值告警：

- **跑道见底**：预计剩余天数低于 `RUNWAY_ALERT_DAYS` 时提醒，附上预计耗尽日期。已经在报余额不足的账户不重复打扰。
- **消耗突增**：今日消耗达到近 `BURN_RATE_WINDOW_DAYS` 天中位数的 `SPEND_SPIKE_RATIO` 倍时提醒。key 泄露、任务跑飞通常先表现为这个。

估算至少需要 4 个数据点、跨度 6 小时；跨度不足一天的结果标为低置信度，不用于告警。看板上每张卡片显示「还可用 N 天」，概览显示全部账户里最先见底的那个。每周的 `weekly_report` 会把本周消耗、跑道排名、未来 30 天的订阅支出汇成一张卡片推出去。

## 看板与 API

看板四个视图：全部项目、仅告警、订阅管理、邮箱扫描；地址栏加 `#alerts` `#subscriptions` `#email` 可直达。首次打开填 `WEB_API_KEY`。开了动态配置能在页面上增删改项目、订阅和邮箱；开了历史 API 有趋势图和历史告警邮件。接口清单见 [docs/API.md](docs/API.md)。

前端是 TypeScript，用 esbuild 打包，产物嵌进二进制，运行时不需要额外的静态文件目录。

## 部署

**Docker**：镜像发布在 `ghcr.io/itswl/quotapulse`，amd64 与 arm64 都有。运行镜像基于 `scratch`，里面只有一个静态二进制和 CA 证书，不到 30 MB。

```bash
docker compose up -d          # 拉已发布的镜像直接跑，部署机不需要装 Go 和 Node
docker compose pull && docker compose up -d   # 升级
```

版本默认跟 `latest`。生产上建议在 `.env` 里钉死：`QUOTAPULSE_VERSION=0.1.0`，否则 `docker compose pull` 可能在你不知情的时候换掉运行中的版本。

```bash
# 带 Prometheus + Grafana
docker compose -f docker-compose.yml -f docker-compose.monitoring.yml up -d

# 改了代码想跑自己这份，叠一层构建
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build

# 单独构建（国内换源）
docker build --build-arg GOPROXY=https://goproxy.cn,direct \
             --build-arg NPM_REGISTRY=https://registry.npmmirror.com -t quotapulse .
```

镜像由流水线在打 tag 时构建：`git tag v1.2.3 && git push origin v1.2.3`，产出 `1.2.3` / `1.2` / `1` / `latest` 四个标签。

**Kubernetes**：`k8s/common-prod.yaml` 含 Deployment、Service、Ingress，apply 前替换 `YOUR_REGISTRY` 与 `YOUR_DOMAIN`。

```bash
kubectl create secret generic quotapulse-secret --from-env-file=.env -n common-prod --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f k8s/common-prod.yaml
kubectl -n common-prod rollout status deploy/quotapulse
```

探针约定：`/live` 给 startup 与 liveness，只证明进程活着；`/health` 给 readiness，没数据、数据过期或任务失败时 503。镜像里没有 shell，优雅下线靠 `SHUTDOWN_DELAY_SECONDS` 而不是 `preStop`。改了 Secret 要 `kubectl rollout restart`。

## 监控

`ENABLE_PROMETHEUS=true` 后 `:9100/metrics` 暴露余额、订阅、邮箱扫描、定时任务、通知发送五组指标。指标含义、Grafana 面板和建议的自监控告警规则见 [grafana/README.md](grafana/README.md)。

## 排障

- **接口 503「API Key 未配置」**：进程没读到 `WEB_API_KEY`，检查 `.env` 或 Secret 后重启。
- **`/health` 503**：看返回体。`has_data=false` 是没有有效项目，`is_stale=true` 是刷新卡住，`failed_jobs` 非空去 `GET /api/jobs` 看错误原文。
- **startup probe 打到 `/health` 反复重启**：启动探针应指向 `/live`。
- **数据库里的密钥没加密**：确认进程有 `CONFIG_ENCRYPTION_KEY`；旧明文会在下一次读取时回写为密文。
- **看板打开是一句「前端产物未构建」**：先 `npm --prefix ui run build` 再编译二进制。
- **不确定配置到底生效了什么**：`./quotapulse -show-config`。

## 开发

```bash
go test ./...                    # Go 全部测试
go test -race ./...              # 并发相关的包用 -race 跑
gofmt -l .                       # 应该没有输出
npm --prefix ui run typecheck    # 前端类型检查
npm --prefix ui test             # 前端测试（桩 DOM，不依赖浏览器）
npm --prefix ui run build        # 重新打包前端，产物提交进仓库
```

数据库查询用 [sqlc](https://sqlc.dev) 从 SQL 生成，改了 `internal/store/queries/*.sql` 后跑 `sqlc generate`。

## 项目结构

```text
cmd/quotapulse/      入口：命令行开关、日志、信号处理
internal/
  model/         领域类型，同时是 API 响应结构
  config/        环境变量 + 自动发现 + 数据库清单合并
  provider/      各平台余额适配器；简单的用 RegisterSpec 声明几行就能接
  store/         持久化，sqlc 生成 sqlite / postgres / mysql 三套查询
  monitor/       余额检查：并发、缓存、阈值告警
  runway/        消耗速率与跑道，突增判断
  subscription/  续费日期推算与提醒
  mailscan/      IMAP 扫描与关键词命中
  notify/        Webhook 适配：feishu / dingtalk / wecom / custom
  report/        周报
  metrics/       Prometheus 指标
  scheduler/     进程内定时任务
  state/         看板状态（线程安全）
  httpapi/       路由、鉴权、请求校验
  selfcheck/     -show-config 的实现
  app/           把上面这些装配起来
ui/              TypeScript 前端，esbuild 打包后由 embed.FS 提供
grafana/ prometheus.yml   监控面板与抓取配置
k8s/             生产部署清单
```
