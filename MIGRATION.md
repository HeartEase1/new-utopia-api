# 生产环境：从官方镜像切换到新乌托邦二开镜像

适用于已通过 Docker Compose 部署官方 **New API `v1.0.0-rc.49`** 的站点。本版基于同一版本，没有新增数据库结构，继续使用原用户、渠道、API Key、余额和订单。其他版本应先验证兼容性，尤其不要将较新版本的数据库直接降级到 rc.49。

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

## 3. 选择二开镜像（默认无需固定 digest）

默认镜像地址：

```text
ghcr.io/heartease1/new-utopia-api:rc49-alipay
```

无需在 `.env` 填写 digest。若已经设置 `UTOPIA_IMAGE=...@sha256:...`，把它改为以下标签，或删除这一行以使用第 5 步的默认镜像值，其他环境配置保留：

```dotenv
UTOPIA_IMAGE=ghcr.io/heartease1/new-utopia-api:rc49-alipay
```

如果使用 `--env-file`，编辑那个文件。镜像已公开，支持 amd64/arm64，无需登录；可在维护前预先下载，不改变运行中的容器：

```bash
docker pull ghcr.io/heartease1/new-utopia-api:rc49-alipay
```

`rc49-alipay` 始终指向本分支最新成功发布的镜像，发布本身不会更新正在运行的容器。如果测试结束后标签已变化，生产拉取的可能是更新版本，应重新确认验收。

### 可选：需要精确锁定时

在测试服务器获取实际运行的镜像 digest（使用测试实例自己的 Compose 参数）：

```bash
docker image inspect "$(docker inspect --format '{{.Image}}' "$(docker compose ps -q new-api)")" \
  --format '{{range .RepoDigests}}{{println .}}{{end}}'
```

只有需要精确复用该版本时，才在生产原 `.env` 中将 `UTOPIA_IMAGE` 改为：

```dotenv
UTOPIA_IMAGE=ghcr.io/heartease1/new-utopia-api@sha256:填写完整的测试镜像摘要
```

替换占位文字为实际 digest，也可从[成功构建的 Summary](https://github.com/HeartEase1/new-utopia-api/actions/workflows/utopia-alipay.yml)获取地址。锁定后 `pull` 不会自动升级到新版本；日后想恢复便捷更新，将 `UTOPIA_IMAGE` 改回默认标签即可。

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
    image: "${UTOPIA_IMAGE:-ghcr.io/heartease1/new-utopia-api:rc49-alipay}"
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

`config --images` 中应用应为 `ghcr.io/heartease1/new-utopia-api:rc49-alipay`（或你主动锁定的地址），数据库和 Redis 镜像保持原样。`--no-deps` 只替换应用，不升级数据库或 Redis。不要执行 `down -v`，不要用仓库的新建实例 Compose 替换原文件。

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

再按 [支付宝配置说明](ALIPAY.md#配置) 合并环境变量与 secret 挂载，[支付覆盖文件](docker-compose.alipay.example.yml)可作参考，服务名和路径按实际调整。正式密钥放在生产服务器，回调改成：

```text
https://你的正式域名/api/alipay-f2f/notify
```

先设置 `ALIPAY_F2F_ENABLED=false`，确认正式商户密钥、HTTPS 回调、后台合规确认及充值参数配置完整后，改为 `true`：

```bash
dc config --quiet
dc up -d --no-deps --no-build new-api
dc logs --tail=100 new-api
```

单纯 `restart` 不会载入修改后的环境变量。若新增覆盖文件，将其 `-f` 参数加进 `dc()`，此后所有命令都使用相同文件顺序。

完成一次小额真实支付，核对支付宝实付、订单和余额，重复查单不应重复入账。继续观察并保留备份和旧镜像。

## 7. 以后更新二开版本

使用默认标签时，每次更新前备份数据和当前镜像（可复用第 2、4 步），查看更新内容并等待 GitHub 镜像发布成功，然后在原项目目录执行：

```bash
dc pull new-api
dc up -d --no-deps --no-build new-api
dc logs --tail=100 new-api
```

无需每次编辑镜像地址。第 2 步备份的当前镜像既可以是官方镜像，也可以是上一版二开镜像；更新出问题时可按下一节恢复该镜像。若 `UTOPIA_IMAGE` 仍锁定旧 digest，先改回第 3 步的默认标签。

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

先在二开版本设置 `ALIPAY_F2F_ENABLED=false` 并重建应用，关闭新下单，保留原密钥、商户及回调。核对支付宝账单，用查单处理已付款未入账订单，确认剩余订单最终状态后再回退。

官方镜像不能处理 `/api/alipay-f2f/notify`。本地二维码过期不代表支付宝订单已关闭，仍需回调的订单处理完毕前不能直接切走。

**正常回退只换镜像，不还原旧数据库。** 有新充值、扣费或余额变化后恢复旧备份会丢失账务数据。数据库损坏等恢复场景需先停写、核对账务，再执行专门的恢复方案。

## 测试与生产不要混淆

| 项目 | 新建测试站 | 切换现有生产站 |
| --- | --- | --- |
| Compose | 使用 `docker-compose.alipay.test.yml` | 编辑原生产 Compose |
| 数据库 | 独立库或隔离副本 | 原生产数据库 |
| 管理员 | 初始化测试管理员 | 原管理员，不重新初始化 |
| 镜像 | 默认 `rc49-alipay` | 默认 `rc49-alipay`；精确复用版本时可选锁定 |
| 回调 | 测试域名 | 正式域名 |
| 数据 | 保留在测试环境 | 不被测试数据覆盖 |
