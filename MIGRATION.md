# 生产环境：从官方镜像切换到新乌托邦二开镜像

适用于已通过 Docker Compose 部署官方 **New API `v1.0.0-rc.49`** 的站点。本版基于同一版本，继续使用原用户、渠道、API Key、余额和订单。安全修订版启动时会自动给充值订单表新增 `quota_snapshot` 字段，固定新支付宝订单的入账额度，不需要手工执行 SQL。其他版本应先验证兼容性，尤其不要将较新版本的数据库直接降级到 rc.49。

从早期二开版本升级时，先核清历史支付宝待支付订单，期间不要调整 `QuotaPerUnit`，历史订单没有快照。多应用节点应在维护窗口全部更新，避免旧节点保留 Redis 重复加额度的竞态。原余额缓存会继续使用并逐步补充代号，无需清空 Redis；不要在有延迟批量扣费时随意清缓存。回退前核清支付宝订单，保留新增字段，不要恢复过时的余额备份。

先按 [ALIPAY.md](ALIPAY.md) 在独立服务器测试一天。默认使用 `rc49-alipay` 标签，方便以后通过 `pull`、`up -d` 更新，**不把测试数据库同步覆盖到生产**。

## 1. 找到原 Compose，记录当前部署

进入原生产 Compose 目录。以下假设文件名为 `docker-compose.yml`、应用服务名为 `new-api`，均需按实际调整。后续命令在同一个 Bash 会话中执行：

```bash
cd /你的/原生产部署目录
dc() { docker compose -f docker-compose.yml "$@"; }
dc config --services
dc ps
container_id=$(dc ps -q new-api)
test -n "$container_id" || { echo '未找到应用，请核对项目和服务名'; exit 1; }
docker inspect --format 'Image: {{.Config.Image}} | Image ID: {{.Image}}' "$container_id"
docker inspect --format '{{range .Mounts}}{{println .Type .Source "->" .Destination}}{{end}}' "$container_id"
```

若平时使用 `-p 项目名`、`--env-file` 或多个 `-f`，将原参数完整加进 `dc()`。保持原目录、项目名和数据挂载，避免误连到新的空卷。确认后台版本为 rc.49，记录生产用户、余额、订单，以及数据库类型和连接。

保留原有 `SQL_DSN`、`LOG_SQL_DSN`、Redis、`SESSION_SECRET`、`CRYPTO_SECRET`、OAuth、域名、端口、反向代理和其他环境配置。不要在新 clone 的目录里直接启动另一套生产实例。

## 2. 备份原配置和镜像

以下变量会在后续步骤继续使用：

```bash
set -euo pipefail
umask 077
stamp=$(date +%Y%m%d-%H%M%S)
backup_dir="$PWD/utopia-backup-$stamp"
mkdir -p "$backup_dir"
cp docker-compose.yml "$backup_dir/docker-compose.yml"
if [ -f .env ]; then cp .env "$backup_dir/.env"; fi
dc config > "$backup_dir/compose.resolved.yml"
old_image_id=$(docker inspect --format '{{.Image}}' "$container_id")
rollback_image="new-utopia-rollback:$stamp"
docker image tag "$old_image_id" "$rollback_image"
printf '%s\n' "$rollback_image" > "$backup_dir/rollback-image.txt"
docker image save "$rollback_image" | gzip > "$backup_dir/old-image.tar.gz"
gzip -t "$backup_dir/old-image.tar.gz"
```

额外 Compose 文件、`env_file`、密钥文件、上传文件和其他挂载数据也要备份。`compose.resolved.yml` 可能包含凭据，只保存在服务器受限目录，不上传仓库或公开日志。提前确认磁盘空间能容纳镜像及数据库备份。

## 3. 使用默认二开镜像

默认镜像地址：

```text
ghcr.io/heartease1/new-utopia-api:rc49-alipay
```

直接在原 Compose 的应用服务中填写上面的 `image` 地址，无需新增 `UTOPIA_IMAGE` 环境变量。镜像已公开，支持 amd64/arm64，可提前拉取：

```bash
docker pull ghcr.io/heartease1/new-utopia-api:rc49-alipay
```

`rc49-alipay` 始终指向本分支最新成功发布的镜像，发布本身不会更新正在运行的容器。如果测试结束后标签已变化，生产拉取的可能是更新版本，应重新确认验收。

## 4. 维护窗口：停写并备份数据库

先停止应用写入，数据库服务继续运行。多副本时需停止所有连接生产库的应用写入者：

```bash
dc stop new-api
```

按实际数据库选下面一种。外置或托管数据库可使用现有快照/备份工具，单独配置了日志库时也要备份。

### PostgreSQL

示例数据库服务名为 `postgres`，容器内有 `POSTGRES_USER`、`POSTGRES_DB`，按实际调整：

```bash
dc exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > "$backup_dir/main.dump"
test -s "$backup_dir/main.dump"
dc exec -T postgres pg_restore --list < "$backup_dir/main.dump" > "$backup_dir/main.dump.list"
```

### MySQL

示例数据库服务名为 `mysql`，使用容器已有 root 密码变量和 `MYSQL_DATABASE`；其他认证方式使用你自己的备份账号：

```bash
dc exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqldump -u root --single-transaction --routines --triggers --events "$MYSQL_DATABASE"' > "$backup_dir/main.sql"
test -s "$backup_dir/main.sql"
```

### SQLite

应用停止后复制完整数据库目录，包含 WAL/SHM 文件。默认路径是 `/data`：

```bash
docker cp "$container_id":/data "$backup_dir/sqlite-data"
```

设置过 `SQLITE_PATH` 时复制实际数据库所在目录，不能只备份默认路径。

**先在隔离环境验证备份可恢复，再继续切换。** 文件非空或归档可读不能代替恢复验证。备份失败就停止升级，可用原配置执行 `dc start new-api` 恢复旧应用。已有支付方式的未完成订单要核对，应用恢复后检查延迟回调是否正常处理。

## 5. 先只换镜像，保留原生产配置

编辑原 Compose 的应用服务，将原行（实际可能是其他官方标签）：

```yaml
services:
  new-api:
    image: calciumion/new-api:v1.0.0-rc.49
```

改为：

```yaml
services:
  new-api:
    image: ghcr.io/heartease1/new-utopia-api:rc49-alipay
```

**以上只是片段，不是完整文件，不能覆盖原 Compose。** 保留所有原 `environment`、`volumes`、`ports`、`networks`、`depends_on`。有其他覆盖文件重复定义 `image:` 时也要调整；若有 `build:`，此次不执行本地构建。

此阶段不需要支付宝密钥，新增充值入口默认关闭。执行：

```bash
dc config --quiet
dc config --images
dc pull new-api
dc up -d --no-deps --no-build new-api
dc ps
dc logs --tail=100 new-api
```

`config --images` 中应用应为 `ghcr.io/heartease1/new-utopia-api:rc49-alipay`，数据库和 Redis 镜像保持原样。`--no-deps` 只替换应用，不升级数据库或 Redis。不要执行 `down -v`，不要用仓库的新建实例 Compose 替换原文件。

重新获取新容器 ID，检查实际镜像和状态接口：

```bash
container_id=$(dc ps -q new-api)
docker inspect --format 'Image: {{.Config.Image}} | Status: {{.State.Status}}' "$container_id"
docker exec "$container_id" wget -qO- http://127.0.0.1:3000/api/status
```

若容器内应用端口改过，修改上述检查端口。单实例重建会短暂中断服务。

## 6. 验收原业务，再开启当面付

1. 原域名、原管理员登录正常，用户、渠道、API Key、余额和订单与切换前一致。
2. 原 API Key 能调用模型，流式响应与扣费正常。
3. 原支付方式、回调和其他登录方式正常。

**如果出现初始化向导或数据为空，不要重新创建管理员。** 先核对项目名、原目录、数据卷和 `SQL_DSN`，确认连接的是原生产库。

进入 **系统设置 → 计费 → 支付网关 → 支付宝当面付**，填写正式应用 ID、商户 PID、应用私钥和支付宝公钥。回调地址使用正式站点的 HTTPS 域名，例如：

```text
https://你的正式域名/api/alipay-f2f/notify
```

先关闭新充值保存，检查连接并核对后台合规确认、充值价格和最低额度，再开启并保存。配置写入原数据库，立即生效；**无需追加支付宝环境变量、挂载密钥或重建容器**。详细字段见 [支付宝后台配置](ALIPAY.md#配置)。

完成一次小额真实支付，核对支付宝实付、订单和余额，重复查单不应重复入账。继续观察并保留备份和旧镜像。

## 7. 以后更新二开版本

使用默认标签时，每次更新前备份数据和当前镜像（可复用第 2、4 步），查看更新内容并等待 GitHub 镜像发布成功，然后在原项目目录执行：

```bash
dc pull new-api
dc up -d --no-deps --no-build new-api
dc logs --tail=100 new-api
```

无需每次编辑镜像地址。第 2 步备份的当前镜像既可以是官方镜像，也可以是上一版二开镜像；更新出问题时可按下一节恢复该镜像。若应用镜像原来使用 digest，将 `image` 直接改为第 3 步的标签。

## 8. 出问题如何回退镜像

### 没有未完成的当面付订单

从 `rollback-image.txt` 读取保留的旧镜像标签，将原 Compose 的 `image` 改为该实际标签，例如 `new-utopia-rollback:20261112-200000`（用自己的时间标签）。旧镜像不在本机时先恢复归档：

```bash
gzip -dc "$backup_dir/old-image.tar.gz" | docker image load
```

然后仅替换应用镜像：

```bash
dc config --quiet
dc up -d --no-deps --no-build --pull never new-api
dc ps
dc logs --tail=100 new-api
```

本机备份标签不要执行 `pull`。必要时从备份还原相关配置，但保留当前生产数据库与卷。

### 已开通当面付，仍有待支付或待入账订单

先在二开版本后台关闭“开启支付宝当面付充值”并保存，关闭新下单，保留原密钥、商户及回调。核对支付宝账单，用查单处理已付款未入账订单，确认剩余订单最终状态后再回退。

官方镜像不能处理 `/api/alipay-f2f/notify`。本地二维码过期不代表支付宝订单已关闭，仍需回调的订单处理完毕前不能直接切走。

**正常回退只换镜像，不还原旧数据库。** 有新充值、扣费或余额变化后恢复旧备份会丢失账务数据。数据库损坏等恢复场景需先停写、核对账务，再执行专门的恢复方案。

## 测试与生产不要混淆

| 项目 | 新建测试站 | 切换现有生产站 |
| --- | --- | --- |
| Compose | 根目录 `docker-compose.yml` | 编辑原生产 Compose |
| 数据库 | 独立库或隔离副本 | 原生产数据库 |
| 管理员 | 初始化测试管理员 | 原管理员，不重新初始化 |
| 镜像 | 默认 `rc49-alipay` | 默认 `rc49-alipay` |
| 回调 | 测试域名 | 正式域名 |
| 数据 | 保留在测试环境 | 不被测试数据覆盖 |
