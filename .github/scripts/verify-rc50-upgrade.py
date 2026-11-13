"""Exercise fresh installs and released rc.49/rc.50 upgrades on disposable CI databases."""
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile
import time
import urllib.request


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def sql(engine, database, statement):
    if engine == 'sqlite':
        with sqlite3.connect(database) as connection:
            rows = connection.execute(statement).fetchall()
        return '\n'.join('\t'.join(str(value) for value in row) for row in rows)
    if engine == 'mysql':
        container = run('docker', 'ps', '-q', '--filter', 'ancestor=mysql:8.0')
        return run('docker', 'exec', container, 'mysql', '-uroot', '-ptest-only',
                   '--batch', '--skip-column-names', database, '-e', statement)
    container = run('docker', 'ps', '-q', '--filter', 'ancestor=postgres:15')
    return run('docker', 'exec', container, 'psql', '-U', 'utopia', '-d', database,
               '-At', '-F', '\t', '-v', 'ON_ERROR_STOP=1', '-c', statement)


def boot(image, directory, env):
    args = ['docker', 'run', '-d', '--name', 'utopia-upgrade', '--network', 'host',
            '-v', f'{directory}:/data', '-e', 'PORT=3099', '-e', 'HOST=127.0.0.1',
            '-e', 'DEFAULT_LANGUAGE=en', '-e', 'NODE_NAME=upgrade-check']
    for key, value in env.items():
        args.extend(['-e', f'{key}={value}'])
    run(*args, image)
    try:
        for _ in range(60):
            try:
                with urllib.request.urlopen('http://127.0.0.1:3099/api/status', timeout=2) as response:
                    status = json.load(response)
                assert status['success'], status
                if image == new_image:
                    assert status['data']['version'].startswith('v1.0.0-rc.50-utopia-'), status
                print('Started', image, status['data']['version'], flush=True)
                return
            except (OSError, ValueError):
                if run('docker', 'inspect', '-f', '{{.State.Running}}', 'utopia-upgrade') != 'true':
                    break
                time.sleep(1)
        raise RuntimeError('startup failed')
    finally:
        print(run('docker', 'logs', 'utopia-upgrade'), flush=True)
        run('docker', 'stop', '-t', '10', 'utopia-upgrade')
        run('docker', 'rm', 'utopia-upgrade')


def indexes(engine, database, table):
    if engine == 'sqlite':
        return sql(engine, database, f"SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='{table}' ORDER BY name")
    if engine == 'mysql':
        return sql(engine, database, f"SELECT DISTINCT index_name FROM information_schema.statistics WHERE table_schema='{database}' AND table_name='{table}' ORDER BY index_name")
    return sql(engine, database, f"SELECT indexname FROM pg_indexes WHERE schemaname='public' AND tablename='{table}' ORDER BY indexname")


old_images = {
    'upgrade_rc49': 'ghcr.io/heartease1/new-utopia-api:sha-1217036ac92c61ef03f84c23bcab61628fa293be-amd64',
    'upgrade_rc50': 'ghcr.io/heartease1/new-utopia-api:sha-dd808b118c53f0837803c47b19bb09af57998611-amd64',
}
new_image = os.environ['UPGRADE_IMAGE']
for old_image in old_images.values():
    run('docker', 'pull', old_image)
run('docker', 'pull', new_image)
for engine in ['sqlite', 'mysql', 'postgres']:
    for mode in ['fresh', *old_images]:
        # sudo runs this script in CI so SQLite files created by the image are writable.
        with tempfile.TemporaryDirectory(prefix='utopia-rc50-') as tmp:
            directory = Path(tmp)
            main = f'rc50_{mode}'
            logs = f'rc50_{mode}_logs'
            env = {}
            if engine == 'sqlite':
                main = logs = str(directory / 'main.db')
                env['SQLITE_PATH'] = '/data/main.db'
            else:
                admin = 'mysql' if engine == 'mysql' else 'postgres'
                for database in [main, logs]:
                    sql(engine, admin, f'CREATE DATABASE {database}')
                if engine == 'mysql':
                    env['SQL_DSN'] = f'root:test-only@tcp(127.0.0.1:3306)/{main}?charset=utf8mb4&parseTime=true'
                    env['LOG_SQL_DSN'] = f'root:test-only@tcp(127.0.0.1:3306)/{logs}?charset=utf8mb4&parseTime=true'
                else:
                    env['SQL_DSN'] = f'postgres://utopia:test-only@127.0.0.1:5432/{main}?sslmode=disable'
                    env['LOG_SQL_DSN'] = f'postgres://utopia:test-only@127.0.0.1:5432/{logs}?sslmode=disable'
            boot(old_images.get(mode, new_image), directory, env)
            version_query = 'SELECT sqlite_version()' if engine == 'sqlite' else 'SELECT version()'
            print(engine, mode, sql(engine, main, version_query), flush=True)
            # Representative existing account, channel, API key, balance, paid order and log.
            sql(engine, main, "INSERT INTO users (username,password,quota,status,role,aff_quota,aff_history,created_at) VALUES ('upgrade_user','test-only-disabled',1234567,2,1,500000,1000000,1700000000)")
            uid = sql(engine, main, "SELECT id FROM users WHERE username='upgrade_user'")
            quote = '`' if engine == 'mysql' else '"'
            sql(engine, main, f"INSERT INTO channels (name,type,{quote}key{quote},status,models) VALUES ('upgrade_channel',1,'test-only-disabled',2,'gpt-test')")
            sql(engine, main, f"INSERT INTO tokens (user_id,{quote}key{quote},name,status,remain_quota) VALUES ({uid},'rc50-migration-test-key','upgrade_token',2,765432)")
            sql(engine, main, f"INSERT INTO top_ups (user_id,trade_no,amount,money,status,payment_method,payment_provider,quota_snapshot,expires_at) VALUES ({uid},'rc50-paid-order',2,10.01,'success','alipay_f2f','alipay_f2f',1000000,1)")
            sql(engine, logs, f"INSERT INTO logs (user_id,content,quota) VALUES ({uid},'rc50-migration-log',123)")
            for key, value in [('AlipayF2FConfigSecret', '{}'), ('CheckSensitiveEnabled', 'true'), ('CheckSensitiveOnPromptEnabled', 'true'), ('SensitiveWords', 'rc50-migration-blocked-word')]:
                sql(engine, main, f"INSERT INTO options ({quote}key{quote},value) VALUES ('{key}','{value}')")
            queries = [
                "SELECT username,password,quota,status,aff_quota,aff_history,created_at FROM users WHERE username='upgrade_user'",
                f"SELECT name,type,{quote}key{quote},status,models FROM channels WHERE name='upgrade_channel'",
                f"SELECT user_id,{quote}key{quote},name,status,remain_quota FROM tokens WHERE name='upgrade_token'",
                "SELECT user_id,trade_no,amount,money,status,payment_method,payment_provider,quota_snapshot,expires_at FROM top_ups WHERE trade_no='rc50-paid-order'",
                f"SELECT value FROM options WHERE {quote}key{quote}='AlipayF2FConfigSecret'",
            ]
            before = [sql(engine, main, query) for query in queries]
            log_before = sql(engine, logs, "SELECT user_id,content,quota FROM logs WHERE content='rc50-migration-log'")
            old_indexes = {table: indexes(engine, main, table) for table in ['users', 'tokens', 'top_ups', 'options']}
            (directory / 'key-volume-marker').write_text('preserve-data-volume')
            for restart in range(2):
                boot(new_image, directory, env)
                assert [sql(engine, main, query) for query in queries] == before
                assert sql(engine, main, "SELECT referral_user_id,referral_quota FROM top_ups WHERE trade_no='rc50-paid-order'") == '0\t0'
                # Released earnings columns were 32-bit on MySQL/PostgreSQL.
                # Prove widening preserves data and really accepts larger values.
                sql(engine, main, f"UPDATE users SET aff_quota=3000000000,aff_history=4000000000 WHERE id={uid}")
                assert sql(engine, main, f"SELECT aff_quota,aff_history FROM users WHERE id={uid}") == '3000000000\t4000000000'
                sql(engine, main, f"UPDATE users SET aff_quota=500000,aff_history=1000000 WHERE id={uid}")
                assert sql(engine, logs, "SELECT user_id,content,quota FROM logs WHERE content='rc50-migration-log'") == log_before
                assert (directory / 'key-volume-marker').read_text() == 'preserve-data-volume'
                for table, names in old_indexes.items():
                    assert set(names.splitlines()) <= set(indexes(engine, main, table).splitlines()), table
                instances = json.loads(sql(engine, main, f"SELECT value FROM options WHERE {quote}key{quote}='request_plugin_setting.instances'"))
                filters = [item for item in instances if item['plugin'] == 'request-filter']
                assert len(filters) == 1 and filters[0]['enabled'] and filters[0]['global']
                assert filters[0]['config']['keywords'] == 'rc50-migration-blocked-word'
                assert sql(engine, main, f"SELECT count(*) FROM options WHERE {quote}key{quote}='SensitiveWords'") == '0'
                try:
                    sql(engine, main, f"INSERT INTO top_ups (trade_no) VALUES ('rc50-paid-order')")
                except (sqlite3.IntegrityError, subprocess.CalledProcessError):
                    pass
                else:
                    raise AssertionError('duplicate order was accepted')
                try:
                    sql(engine, main, f"INSERT INTO tokens ({quote}key{quote}) VALUES ('rc50-migration-test-key')")
                except (sqlite3.IntegrityError, subprocess.CalledProcessError):
                    pass
                else:
                    raise AssertionError('duplicate API key was accepted')
                print(f'PASS {engine} {mode} restart={restart + 1}: data, referral balances, no backfill, indexes, unique order, filter migration, logs, volume', flush=True)
