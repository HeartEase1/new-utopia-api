# 新乌托邦 · 支付宝当面付

基于官方 New API `v1.0.0-rc.49`（`48a6c87f9e7757fdc58e5be9bcb4f8281ad0dfd0`）。
第一版提供余额充值、电脑扫码、手机点击唤起支付宝、异步回调、主动查单补单。
不调用电脑网站/手机网站支付接口，不包含支付宝直购订阅或退款。
保留 New API 原始许可证、署名和项目标识。

## 配置

支付宝应用必须已开通当面付，并使用 **RSA2 公钥模式**。本版不支持证书模式。
应用私钥与支付宝公钥放在服务器本地文件；支付宝公钥不是应用公钥。
不要把私钥提交到 Git 或发到聊天中。`secrets/` 已加入忽略规则。

| 环境变量 | 用途 |
| --- | --- |
| `ALIPAY_F2F_ENABLED=true` | 开启新充值入口，默认关闭 |
| `ALIPAY_F2F_APP_ID` | 支付宝应用 ID |
| `ALIPAY_F2F_SELLER_ID` | 收款商户 PID，用于回调核对 |
| `ALIPAY_F2F_NOTIFY_URL` | `https://你的域名/api/alipay-f2f/notify` |
| `ALIPAY_F2F_PRIVATE_KEY_FILE` | 容器内应用私钥文件路径 |
| `ALIPAY_F2F_PUBLIC_KEY_FILE` | 容器内支付宝公钥文件路径 |
| `ALIPAY_F2F_SANDBOX=true` | 使用支付宝沙箱；默认正式环境 |

配置在进程中缓存，修改后需重建应用容器。回调地址必须可从公网访问，反向代理不能对它增加登录、验证码或缓存。
后台仍需完成原版支付合规确认，并配置充值最低额度、价格与折扣。当面付实付币种固定为人民币；充值额度与原版易支付的换算一致。
手机在支付弹窗中点击“打开支付宝”；浏览器限制唤起时使用系统浏览器或扫码备用流程，不保证所有内置浏览器均能直接唤起。

## GitHub 自动发布镜像

向 `utopia/alipay-rc49` 推送后，[New Utopia Alipay checks](https://github.com/HeartEase1/new-utopia-api/actions/workflows/utopia-alipay.yml) 会先运行支付三数据库测试和前端检查，再原生构建 `linux/amd64`、`linux/arm64` 镜像，检查容器启动、状态接口和内嵌前端，最后发布到 GitHub Container Registry（GHCR）。服务器无需安装 Go、Bun 或自行编译，也不需要 Docker Hub 凭据。

- 测试入口：`ghcr.io/heartease1/new-utopia-api:rc49-alipay`，随成功构建更新。
- 提交标签：`ghcr.io/heartease1/new-utopia-api:sha-完整40位提交哈希`。
- 正式部署使用 digest：`ghcr.io/heartease1/new-utopia-api@sha256:...`，从成功流水线的 Summary 复制。它唯一确定镜像内容。

必须等待工作流的 **Publish multi-architecture image** 成功，单独测试通过不代表镜像已经发布。首次发布的 GHCR 包可能默认为私有，仓库所有者需在包的 **Package settings → Change visibility → Public** 设置公开，公开后服务器可匿名拉取。若服务器访问不了 `ghcr.io`，需先解决该服务器的网络访问；GitHub 镜像不会自动同步到国内镜像站。

## 在独立服务器用 Docker Compose 测试

以下命令适用于 Linux，要求 Docker Engine 与 Compose v2。建议使用独立 HTTPS 测试域名，例如 `https://test.example.com`。示例提供独立 PostgreSQL 15 数据库，不连接线上数据库；若线上使用 MySQL/SQLite，还应在同类型数据库的隔离副本上验证启动与升级。

### 1. 下载配置与准备密钥

```sh
mkdir -p /opt/new-utopia-test/secrets
cd /opt/new-utopia-test
curl -fL https://raw.githubusercontent.com/HeartEase1/new-utopia-api/utopia/alipay-rc49/docker-compose.alipay.test.yml -o compose.yml
chmod 700 secrets
```

将应用私钥保存为 `secrets/alipay-app-private.pem`，支付宝公钥保存为 `secrets/alipay-public.pem`。私钥只放在服务器，不上传 GitHub。然后限制文件权限：

```sh
chmod 600 secrets/*.pem
openssl rand -hex 32
openssl rand -hex 32
```

上述两个随机值分别用于数据库密码和会话密钥。在当前目录创建 `.env`（替换全部占位值；`TEST_ORIGIN` 不带结尾斜杠）：

```dotenv
UTOPIA_IMAGE=ghcr.io/heartease1/new-utopia-api:rc49-alipay
POSTGRES_PASSWORD=替换为第一个随机值
SESSION_SECRET=替换为第二个随机值
TEST_ORIGIN=https://test.example.com
ALIPAY_F2F_APP_ID=你的应用ID
ALIPAY_F2F_SELLER_ID=你的商户PID
ALIPAY_F2F_ENABLED=false
ALIPAY_F2F_SANDBOX=false
```

```sh
chmod 600 .env
docker compose config --quiet
docker compose pull
docker compose up -d
docker compose ps
docker compose logs --tail=100 new-api
```

### 2. 配置 HTTPS 和后台

在宿主机 Nginx/Caddy/宝塔中，将测试域名反向代理到 `http://127.0.0.1:3001`，启用 HTTPS、转发原始 Host 与 `X-Forwarded-Proto`，允许 `/api/alipay-f2f/notify` 的 POST 请求。3001 端口只绑定本机。若反向代理本身也在容器内，需改为共享 Docker 网络访问应用，不能使用该代理容器的 `127.0.0.1`。

通过 HTTPS 测试域名完成管理员初始化，在后台设置测试站点地址、充值参数及支付合规确认。初次启动入口关闭，完成配置后将 `.env` 的 `ALIPAY_F2F_ENABLED` 改为 `true`，执行：

```sh
docker compose up -d --no-deps new-api
```

`.env` 修改后用 `up -d` 重建容器；单纯 `restart` 不会载入更新后的环境变量。`SANDBOX=false` 表示真实收款；沙箱必须使用沙箱应用和对应密钥，不能混用正式凭据。

### 3. 固定镜像并观察一天

先获取本次实际运行镜像的 digest：

```sh
docker image inspect "$(docker inspect --format '{{.Image}}' "$(docker compose ps -q new-api)")" --format '{{range .RepoDigests}}{{println .}}{{end}}'
```

把输出中的 `ghcr.io/heartease1/new-utopia-api@sha256:...` 写入 `.env` 的 `UTOPIA_IMAGE`，再次运行 `docker compose up -d --no-deps new-api`。后续测试和上线始终使用该地址，避免浮动标签在测试中途更新。

建议至少验证以下场景，并连续观察一天：

1. 电脑扫码、手机系统浏览器点击唤起支付宝，各完成一次小额支付，核对支付宝实付、站内订单和余额。
2. 付款后关闭支付弹窗，确认异步回调仍能到账；刷新后从充值历史查单，余额只增加一次。
3. 未付款订单不加余额；二维码过期后仍可手动查单。
4. 重建应用容器后，用户、订单和余额保留，已支付订单可继续查询。
5. 将 `ALIPAY_F2F_ENABLED=false` 并重建应用，确认不再创建新订单，已有订单仍能查单/处理回调。
6. 测试正常 API 调用、流式响应、充值后的余额扣费，以及后台日常操作。

这是一份人工验收流程，不表示上述真实商户与手机测试已完成。测试中产生的真实支付宝交易仍是真实收款，退款需在支付宝侧处理，本版没有站内退款接口。

## 测试通过后切换线上

**上线同步的是测试通过的镜像和必要配置，不是测试数据库。不要把测试用户、订单或余额覆盖到生产。**

1. 记录测试镜像 digest 和线上旧镜像 digest，备份线上数据库、Compose、环境变量及数据文件，验证备份可恢复。
2. 在原生产 Compose 所在目录，参考 `docker-compose.alipay.example.yml` 合并支付宝环境变量和 secret 挂载。保留原数据库类型、连接、数据卷、项目名、会话密钥及其余环境设置，不使用测试 Compose 替换线上文件。
3. 将原应用服务的 `image` 改为测试通过的 `ghcr.io/...@sha256:...`。设置生产 `ALIPAY_F2F_NOTIFY_URL=https://正式域名/api/alipay-f2f/notify`，先保持 `ALIPAY_F2F_ENABLED=false`。密钥文件必须在生产服务器重新放置到对应路径。
4. 在生产 Compose 目录执行（服务名示例为 `new-api`，以实际名称为准）：

```sh
docker compose config --quiet
docker compose pull new-api
docker compose up -d --no-deps new-api
docker compose ps
docker compose logs --tail=100 new-api
```

5. 确认原用户、余额、API 调用正常后开启当面付，重建应用并做一次生产小额支付，确认真实回调和入账。

如果使用额外 Compose 文件合并，所有 `config/pull/up/logs` 命令都必须同时传入原文件和覆盖文件：`docker compose -f docker-compose.yml -f docker-compose.alipay.example.yml ...`。示例依赖 `.env` 中的 `UTOPIA_IMAGE`、支付宝变量，以及当前目录下的密钥文件。

切换镜像不需要搬迁数据库，本版也不新增数据库结构。单实例重建会短暂中断服务，安排维护窗口。不要运行 `down -v`。

测试结束后先关闭新充值，核清测试订单，保留测试域名与服务直到未完成订单处理完毕；同一商户的测试订单和正式订单通过各自下单时的 `notify_url` 回调到对应站点。

需要离线自行构建时，仍可在源码目录执行 `docker build -t new-utopia-api:rc49-alipay.1 .`，但服务器直接拉取 GHCR 镜像是本文推荐流程。

## 入账与回退

- 下单先落库，然后请求当面付；支付宝异步通知验签并核对应用、收款方、订单总金额。
- 支付成功只依据后端验签回调或已验证签名的主动查单，前端跳转、唤起成功不代表已付款。
- 重复/并发通知和查单使用同一数据库事务，钱包上限检查失败会回滚订单状态。
- 页面每 5 秒查单；错误或二维码超时后停止自动查询，可以手动重试。
- 普通用户可在充值历史中对待支付的当面付订单点击“查询支付状态”，恢复刷新或关闭页面前的订单。
- 关闭弹窗和本地二维码超时不会把数据库订单强制判为失败，避免遗漏延迟通知。
- `ALIPAY_F2F_ENABLED=false` 只关闭新下单，保留完整凭据时仍处理已付款回调和查单。
- 本版不修改数据库结构。回退官方镜像前必须关闭新下单，核清/处理所有支付宝待支付订单；官方版本无法处理这些回调。不要在有新交易后直接恢复旧备份。
- 未完成订单处理完毕前，不要更换应用、收款账号、沙箱模式或密钥配置。

## 验证范围

自动测试使用本地模拟支付宝网关及真实 RSA2 签名，覆盖金额/商户/应用/签名不符、重复回调、查单补单、订单归属、钱包上限和并发入账。
`.github/workflows/utopia-alipay.yml` 在真实 SQLite、MySQL 8、PostgreSQL 15 上运行支付回归，并检查前端。
自动测试不能代替你的商户真实联调；正式启用前需完成沙箱或小额真实支付、手机唤起、回调公网可达和停机恢复验证。

实现参考了 New API PR #3457/#7055 与 Sub2API 的当面付交互思路；未整包合并其分支。
支付宝协议签名与响应验签使用 MIT 许可的 `github.com/smartwalle/alipay/v3` SDK。
