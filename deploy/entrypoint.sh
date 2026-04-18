#!/usr/bin/env bash
# gpt2api 容器启动入口。
#
# 职责:
#   1. 等待 MySQL 可连接(最多 60 秒)
#   2. 执行 goose up(幂等)
#   3. exec 启动 server 主进程
#
# 读取的环境变量:
#   - MYSQL_HOST        (默认 mysql)
#   - MYSQL_PORT        (默认 3306)
#   - MYSQL_USER        (默认 gpt2api)
#   - MYSQL_PASSWORD    (默认 gpt2api)
#   - MYSQL_DATABASE    (默认 gpt2api)
#   - SKIP_MIGRATE=1    跳过自动迁移
set -euo pipefail

MYSQL_HOST=${MYSQL_HOST:-mysql}
MYSQL_PORT=${MYSQL_PORT:-3306}
MYSQL_USER=${MYSQL_USER:-gpt2api}
MYSQL_PASSWORD=${MYSQL_PASSWORD:-gpt2api}
MYSQL_DATABASE=${MYSQL_DATABASE:-gpt2api}

log() { echo "[entrypoint] $*"; }

wait_mysql() {
  log "waiting for mysql ${MYSQL_HOST}:${MYSQL_PORT}..."
  local i=0
  while (( i < 60 )); do
    if MYSQL_PWD="${MYSQL_PASSWORD}" mysqladmin ping \
        -h "${MYSQL_HOST}" -P "${MYSQL_PORT}" -u "${MYSQL_USER}" --silent 2>/dev/null; then
      log "mysql is up."
      return 0
    fi
    sleep 1
    i=$((i+1))
  done
  log "mysql did not become ready in 60s, continuing anyway."
  return 1
}

run_migrate() {
  if [[ "${SKIP_MIGRATE:-0}" == "1" ]]; then
    log "SKIP_MIGRATE=1, skipping migrations"
    return 0
  fi
  local dsn="${MYSQL_USER}:${MYSQL_PASSWORD}@tcp(${MYSQL_HOST}:${MYSQL_PORT})/${MYSQL_DATABASE}?parseTime=true&multiStatements=true&charset=utf8mb4,utf8"
  log "running goose migrations..."
  goose -dir /app/sql/migrations mysql "${dsn}" up
  log "migrations done."
}

wait_mysql || true
run_migrate || { log "migration failed"; exit 1; }

log "starting: $*"
exec "$@"
