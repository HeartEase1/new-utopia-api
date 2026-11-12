# 新乌托邦 · 支付宝当面付

基于官方 New API `v1.0.0-rc.49`（`48a6c87f9e7757fdc58e5be9bcb4f8281ad0dfd0`）。
第一版提供余额充值、电脑扫码、手机点击唤起支付宝、异步回调、主动查单补单。
不调用电脑网站/手机网站支付接口，不包含支付宝直购订阅或退款。
保留 New API 原始许可证、署名和项目标识。

**已有官方 rc.49 生产站点？直接查看[官方镜像切换到二开镜像完整教程](MIGRATION.md)**，包含 PostgreSQL/MySQL/SQLite 备份、原 Compose 修改前后对照、分阶段切换、验收和回退命令。

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

向本仓库默认分支推送后，[New Utopia Alipay checks](https://github.com/HeartEase1/new-utopia-api/actions/workflows/utopia-alipay.yml) 会先运行支付三数据库测试和前端检查，再原生构建 `linux/amd64`、`linux/arm64` 镜像，检查容器启动、状态接口和内嵌前端，最后发布到 GitHub Container Registry（GHCR）。服务器无需安装 Go、Bun 或自行编译，也不需要 Docker Hub 凭据。

- 默认镜像：`ghcr.io/heartease1/new-utopia-api:rc49-alipay`，测试和生产均可使用，随成功构建更新。

必须等待工作流的 **Publish multi-architecture image** 成功，单独测试通过不代表镜像已经发布。本仓库的 GHCR 镜像已公开，可匿名拉取，无需 Docker Hub 账号。若服务器访问不了 `ghcr.io`，需先解决该服务器的网络访问；GitHub 镜像不会自动同步到国内镜像站。

## 在独立服务器用 Docker Compose 测试

测试服务器也使用仓库根目录的 `docker-compose.yml`，与官方 rc.49 保持相同的服务、端口、数据卷和网络结构：**New API + PostgreSQL + Redis**。二开只更换应用镜像，额外提供默认注释的支付宝环境变量和只读密钥挂载。

测试请放在另一台服务器，使用独立数据库和测试域名，不连接生产数据库。若线上使用 MySQL/SQLite，测试时也应采用同类型数据库的隔离副本。

### 1. 使用官方相同的 Compose 方式部署

```sh
git clone https://github.com/HeartEase1/new-utopia-api.git
cd new-utopia-api
mkdir -p secrets
chmod 700 secrets
```

编辑根目录 `docker-compose.yml`，按官方部署方式设置数据库与 Redis 密码（服务端和连接串同时修改）、持久的随机 `SESSION_SECRET`。用 `openssl rand -hex 32` 生成随机值，替换示例值。保留官方服务结构，无需额外的测试 Compose 或覆盖文件。

```sh
docker compose config --quiet
docker compose pull
docker compose up -d
docker compose ps
docker compose logs --tail=100 new-api
```

默认与官方一样将宿主机 3000 映射到应用 3000。测试域名通过宿主机 Nginx/Caddy/宝塔反向代理到 `http://127.0.0.1:3000`，启用 HTTPS 并正确转发 Host、`X-Forwarded-Proto`。公网访问控制按你的原部署方式配置；数据库和 Redis 默认不发布宿主机端口。

通过 HTTPS 控制台访问时，取消官方模板中 `SESSION_COOKIE_SECURE` 和 `SESSION_COOKIE_TRUSTED_URL` 两行的注释，设置为 `true` 和实际测试站点的 HTTPS Origin。反向代理在容器内时通过共享 Docker 网络访问应用，不能用代理容器的 `127.0.0.1`。

### 2. 只追加支付宝配置

将应用私钥保存为 `secrets/alipay-app-private.pem`，支付宝公钥保存为 `secrets/alipay-public.pem`：

```sh
chmod 600 secrets/*.pem
```

编辑**同一份** `docker-compose.yml`，在 `new-api` 原有的 `volumes` 和 `environment` 中取消支付宝配置行的注释，填写你的真实值。下面是需要新增的片段，不能覆盖完整文件或原有字段：

```yaml
services:
  new-api:
    image: ghcr.io/heartease1/new-utopia-api:rc49-alipay
    volumes:
      # 保留原来的数据、日志挂载，仅追加下面两行。
      - ./secrets/alipay-app-private.pem:/run/secrets/alipay_app_private_key:ro
      - ./secrets/alipay-public.pem:/run/secrets/alipay_public_key:ro
    environment:
      # 保留原来的 SQL_DSN、REDIS_CONN_STRING 等变量，仅追加下面各行。
      - ALIPAY_F2F_ENABLED=false
      - ALIPAY_F2F_APP_ID=你的应用ID
      - ALIPAY_F2F_SELLER_ID=你的商户PID
      - ALIPAY_F2F_NOTIFY_URL=https://你的测试域名/api/alipay-f2f/notify
      - ALIPAY_F2F_PRIVATE_KEY_FILE=/run/secrets/alipay_app_private_key
      - ALIPAY_F2F_PUBLIC_KEY_FILE=/run/secrets/alipay_public_key
      - ALIPAY_F2F_SANDBOX=false
```

密钥文件必须先创建为普通文件再启动容器。后台完成支付合规确认、充值价格和最低额度配置，确保回调 POST 可以从公网访问，再把 `ALIPAY_F2F_ENABLED` 改为 `true`：

```sh
docker compose config --quiet
docker compose up -d --no-deps new-api
```

`up -d` 会应用环境变量和挂载更新；单纯 `restart` 不会重新加载 Compose 配置。`SANDBOX=false` 表示真实收款；沙箱模式必须搭配沙箱应用和密钥，不能混用正式凭据。

### 3. 观察一天

继续使用默认 `rc49-alipay` 标签即可。新镜像发布不会自动替换正在运行的容器；测试期间不执行 `pull` 更新即可保持当前版本。若测试结束前标签已有新构建，生产拉取的可能是更新版本，需要重新确认验收。

建议至少验证以下场景，并连续观察一天：

1. 电脑扫码、手机系统浏览器点击唤起支付宝，各完成一次小额支付，核对支付宝实付、站内订单和余额。
2. 付款后关闭支付弹窗，确认异步回调仍能到账；刷新后从充值历史查单，余额只增加一次。
3. 未付款订单不加余额；二维码过期后仍可手动查单。
4. 重建应用容器后，用户、订单和余额保留，已支付订单可继续查询。
5. 将 `ALIPAY_F2F_ENABLED=false` 并重建应用，确认不再创建新订单，已有订单仍能查单/处理回调。
6. 测试正常 API 调用、流式响应、充值后的余额扣费，以及后台日常操作。

这是一份人工验收流程，不表示上述真实商户与手机测试已完成。测试中产生的真实支付宝交易仍是真实收款，退款需在支付宝侧处理，本版没有站内退款接口。

## 测试通过后切换线上

逐步命令、备份示例和回退操作见 **[生产环境切换教程](MIGRATION.md)**。以下是流程概览。

**上线同步的是镜像和必要配置，不是测试数据库。不要把测试用户、订单或余额覆盖到生产。**

1. 记录测试和线上当前镜像，备份线上数据库、Compose、环境变量及数据文件，验证备份可恢复。
2. 在原生产 Compose 所在目录，按上面的配置片段追加支付宝环境变量与只读密钥挂载。保留原数据库、Redis、端口、数据卷、项目名和全部现有环境配置，不用仓库模板覆盖生产文件。
3. 将原应用服务的 `image` 改为 `ghcr.io/heartease1/new-utopia-api:rc49-alipay`。设置生产 `ALIPAY_F2F_NOTIFY_URL=https://正式域名/api/alipay-f2f/notify`，先保持 `ALIPAY_F2F_ENABLED=false`。密钥文件必须在生产服务器重新放置到对应路径。
4. 在生产 Compose 目录执行（服务名示例为 `new-api`，以实际名称为准）：

```sh
docker compose config --quiet
docker compose pull new-api
docker compose up -d --no-deps new-api
docker compose ps
docker compose logs --tail=100 new-api
```

5. 确认原用户、余额、API 调用正常后开启当面付，重建应用并做一次生产小额支付，确认真实回调和入账。

只使用原项目的 Compose 配置。若你之前使用了 `UTOPIA_IMAGE` 或 digest 写法，将应用 `image` 直接改为上面的地址即可，其他已有环境变量保留。

## 以后如何更新

默认标签无需每次修改 Compose 或 `.env`。先备份并查看更新内容，等待 GitHub 镜像发布成功，再在原部署目录执行：

```sh
docker compose pull new-api
docker compose up -d --no-deps new-api
docker compose logs --tail=100 new-api
```

只更新应用，不连带升级数据库和 Redis。使用自定义项目名、环境文件或多个 Compose 文件时，继续带上原来的全部参数。`pull` 下载该标签的最新成功发布镜像，`up -d` 应用更新；没有新镜像时不会为了更新而强制重建。若应用 `image` 以前固定了 digest，先改为上面的标签。

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
