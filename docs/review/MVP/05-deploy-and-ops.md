# MVP-05. Развёртывание, кластер, VPN, эксплуатация

Заданная топология: **всё, кроме worker — на небольших серверах в кластере; worker — на GPU-кластере (CUDA/Apple Silicon); связь по VPN, асинхронная.**

Состояние: один узел Docker Compose (`deploy/compose.yaml`, `deploy/framevela/compose.json`), единичные экземпляры PostgreSQL/RabbitMQ/MinIO, frontend — Cloudflare Workers (`deploy/framevela/*/wrangler.jsonc`) и/или контейнеры (`deploy/images/*`), провижининг — `deploy/framevela/provision.py`. Нет VPN-схемы, HA, метрик, CI.

| ID | Приоритет | Работа | Трудоёмкость |
|---|---|---|---|
| O-1 | **P0** | Сетевая схема и VPN (WireGuard) серверов ↔ GPU | M |
| O-2 | **P0** | Выровнять боевую установку с репозиторием (миграции, scheduler, дрейф) | M |
| O-3 | P1 | HA-профиль для «небольших серверов» | L |
| O-4 | P1 | Наблюдаемость: Prometheus/Alertmanager/Grafana/Loki | M |
| O-5 | P1 | Резервные копии и учебное восстановление | M |
| O-6 | P1 | CI/CD | M |
| O-7 | P1 | Секреты и гигиена репозитория | S |
| O-8 | P1 | Заголовки/ограничения на Caddy | S |
| O-9 | P1 | Провижининг GPU-хостов (CUDA, Apple Silicon) | M |
| O-10 | P2 | Нагрузочные тесты и SLO | M |

## O-1. Сетевая схема и VPN (P0)

* **Новый документ:** `docs/ops/network-topology.md` + конфиги `deploy/vpn/`.
* **Содержание:**
  1. **WireGuard-хаб** на серверной стороне (отдельный маленький узел или на балансировщике): подсеть `10.77.0.0/24`, каждый GPU-хост — пир `/32`, `PersistentKeepalive = 25`, MTU 1380 (проверить фрагментацию), ключи на хост, ротация раз в 90 дней.
  2. **Правила доступа (nftables/ufw) для пиров worker:** только исходящие `10.77.0.1:5671` (RabbitMQ, amqps), `10.77.0.1:8443` (object-gateway, mTLS) и, пока не сделан [W-7](01-worker-and-vpn.md), `:5432` PostgreSQL (через PgBouncer/прокси с TLS); входящие — только SSH с админ-сети.
  3. **DNS внутри VPN:** `queue.photo.internal`, `gateway.photo.internal`, `db.photo.internal` (имена в SAN сертификатов).
  4. **Сертификаты:** единый внутренний CA (уже создаётся `provision.py`); отдельные серверные сертификаты RabbitMQ/PostgreSQL/gateway и клиентские сертификаты воркеров ([W-8](01-worker-and-vpn.md)).
  5. **Time sync** (chrony/NTP) на всех узлах ([W-12](01-worker-and-vpn.md)).
  6. **Режимы отказа и ожидаемое поведение** (таблица): разрыв VPN < 20 с → задача продолжается; 20–60 с → продолжается, новые задачи не берутся; > lease → задача считается потерянной, переисполняется (идемпотентно); восстановление связи → спул результата доставляется ([W-3](01-worker-and-vpn.md)).
* **Проверка:** [W-11](01-worker-and-vpn.md) (chaos-тесты) на стенде с реальным WireGuard.

## O-2. Выровнять боевую установку с репозиторием (P0)

* **Состояние (по `deploy/framevela/README.md`):** на проде применены миграции до 2597; scheduler остановлен (профиль `qualified-render`, нет калибровочного каталога); «обработка фотографий целиком пока не введена в эксплуатацию».
* **Шаги:**
  1. Завершить baseline/цепочку миграций ([02 B-3](02-backend-and-contracts.md)) и применить на копии боевой БД (клон `pg_basebackup`) → прогнать `make test-db` против копии.
  2. Получить **измеренный** Render calibration catalog на целевом GPU (`PHOTO_RENDER_CALIBRATION_FILE`, `PHOTO_RENDER_CALIBRATION_SHA256`; оверлей `deploy/compose.render-calibration.yaml`) — без него scheduler остаётся выключенным (это корректная защита, обходить нельзя).
  3. Ввести эндпоинт `/health/version` (git sha, версия миграций, digests образов, sha model-lock) и команду `scripts/release_manifest.py --check` в пайплайне; сравнение «развёрнуто == манифест релиза» — часть smoke.
  4. Обновить документацию-«истину»: `docs/implementation-v2/README.md` (устаревшее «миграции до 00195»), `docs/repository-layout.md` — ссылка на актуальный статус.

## O-3. HA-профиль «небольшие серверы» (P1)

Рекомендуемый минимум для MVP (3 небольших узла + внешнее объектное хранилище):

| Компонент | Схема | Файлы/изменения |
|---|---|---|
| Caddy | 2 экземпляра (узлы A, B) + health-check балансировка/плавающий IP (keepalived) либо DNS с низким TTL | `deploy/caddy/Caddyfile` — `reverse_proxy api1:8080 api2:8080 { lb_policy least_conn; health_uri /health/ready }` |
| API, object-gateway, publisher, dead-letter, collection-worker, maintenance | ≥ 2 реплики stateless; `publisher`/`maintenance` — с лидерством через advisory lock либо строго 1 экземпляр | `deploy/compose.yaml` → `deploy/cluster/compose.node-a.yaml`, `…node-b.yaml` |
| Scheduler | 2 экземпляра (лидер/standby), см. [02 B-9](02-backend-and-contracts.md) | — |
| PostgreSQL | primary + синхронная реплика (Patroni или managed), WAL-архив в S3 (pgBackRest), PgBouncer для API/воркеров (session-режим; scheduler — прямое соединение из-за advisory lock) | `deploy/pg/` |
| RabbitMQ | 3 узла (quorum-очереди уже используются, `backend/internal/queue/topology.go`), TLS 5671 ([W-6](01-worker-and-vpn.md)) | `deploy/rabbitmq/` |
| Объектное хранилище | внешний S3-совместимый сервис с **включённым версионированием** (код восстановления использует version_id, `workers/photo_worker/runtime/restore_objects.py`) либо distributed MinIO (≥ 4 диска) | `deploy/minio/`, `S3_*` в окружении |
| Оркестрация | **Рекомендация MVP:** Docker Compose на каждом узле + Ansible/systemd (без k8s). При росте — k3s/Nomad. Не вводить Kubernetes ради MVP | `deploy/ansible/` |

Критерий приёмки: отключение любого одного узла (A/B/C) не прерывает вход, просмотр, скачивание клиентом; импорт и обработка продолжаются или корректно возобновляются.

## O-4. Наблюдаемость (P1)

* **Стек:** Prometheus + Alertmanager + Grafana, логи — Loki (или файловые с ротацией); `node_exporter`, `rabbitmq_prometheus`, `postgres_exporter` и собственные `/metrics` ([02 B-2](02-backend-and-contracts.md)).
* **Алерты (минимум):** возраст самого старого события outbox > 60 с; глубина очереди `q.worker.*` растёт > 10 мин; нет heartbeat воркера > 60 с; доля `lease_expired`/`retry` > 5 %; ошибки `commit_task_*`; `scheduler_is_leader` = 0 у всех; использование диска scratch > 85 %; замедление `/health/ready`; срок сертификатов < 21 день; задержка репликации PG; свободное место MinIO/PG.
* **Файлы:** `deploy/observability/{prometheus.yml,alerts.yml,grafana/dashboards/*.json}`.

## O-5. Резервные копии и восстановление (P1)

* **Состояние:** обширный код операторов восстановления (`workers/photo_worker/runtime/restore_*.py`, `backup_*.py`, `deploy/BACKUP_OPERATOR.md`), gate `make test-restore`; docs помечают проверки «NOT RUN»; `deploy/framevela/README.md`: автоматических внешних резервных копий нет.
* **Шаги:** (1) pgBackRest/WAL-архив + ежесуточный full/инкрементальный бэкап в S3; (2) версионирование бакета + манифест инвентаря; (3) ежемесячный drill по сценарию `RestoreCheckpoint` из `docs/implementation-v2/18-capacity-operations.md`, результаты — в `docs/ops/restore-drills/YYYY-MM.md` с RPO/RTO; (4) автоматизировать четыре зонда `run_restore_acceptance` (legacy_apply, v2_workflow, purge_negative, revoke_negative) в CI-ночной задаче.

## O-6. CI/CD (P1)

* **Файл:** `.github/workflows/ci.yml` (или `.gitlab-ci.yml`) — стадии: `lint+typecheck` (TS: `tsc`, eslint; Go: `go vet`, `golangci-lint`; Python: `ruff`) → `unit` (`make test-unit`, `npm test`) → `integration` (сервисы PostgreSQL/RabbitMQ/MinIO, `make test-db`, `PHOTO_TEST_DATABASE_URL` задан, **skip = fail** для обязательных наборов) → `build images` (digest-pin) → `e2e` (compose + Playwright) → `release-check` (`make release-evidence`/`release-check`). Подробности — [06](06-testing-ci-e2e.md).
* Кэширование: Go (`GOMODCACHE`, `GOCACHE`), npm, uv.

## O-7. Секреты и гигиена (P1)

* **Убрать из VCS:** IP сервера, идентификаторы Cloudflare (`account_id`, `zone_id`), логин SMTP-реле, адрес пересылки почты из `deploy/framevela/README.md`, `deploy/framevela/*/wrangler.jsonc`; хранить в приватной документации/секрет-менеджере; в репозитории — `*.example`.
* **Параметризовать:** `deploy/framevela/provision.py` (`/srv/photoeditor`, `/mnt/HC_Volume_<id>`) → переменные `PHOTO_ROOT`, `PHOTO_PG_VOLUME`.
* **Транспорт:** DSN `sslmode=verify-full&sslrootcert=…` ([W-4](01-worker-and-vpn.md)), `amqps://` ([W-6]); пароли ролей — из файлов секретов, ротация раз в 90 дней; хранение секретов — SOPS/age или Vault.
* **Сертификаты:** срок клиентских сертификатов воркеров 90 дней, автоматический перевыпуск (`deploy/vpn/rotate-certs.sh`).

## O-8. Caddy (P1)

* **Где:** `deploy/caddy/Caddyfile`, `deploy/framevela/Caddyfile`.
* **Добавить:** заголовки безопасности для `{$APP_DOMAIN}` (сейчас без них) и портала (CSP, HSTS, `X-Frame-Options`, [04 PT-2](04-portal.md)); `header_up X-Forwarded-For {remote_host}` ([02 B-0](02-backend-and-contracts.md)); тайм-ауты и лимиты тела на API; rate-limit (модуль `caddy-ratelimit` либо ограничение на уровне API); `encode` для storage-домена не нужен (медиа уже сжато).

## O-9. Провижининг GPU-хостов (P1)

* **CUDA:** `deploy/compose.rtx-worker.yaml`, `deploy/rtx_installation.py`, `deploy/env/rtx-worker.env.example` — оформить как `deploy/gpu-worker/install.sh` (NVIDIA container toolkit, `PHOTO_MODEL_ROOT`, `PHOTO_MODEL_LOCK`, `PHOTO_CAPACITY_POLICY` из `configs/capacity-rtx3060*.json` под реальную карту); проверка `photo-worker doctor` как healthcheck и как шаг перед включением в кластер (`activate-rtx-worker.sql` заменить командой регистрации, [W-8]).
* **Apple Silicon:** [W-10](01-worker-and-vpn.md) — launchd + `deploy/mac-worker/`.
* **Модели:** распространять через объектное хранилище с проверкой SHA по lock-файлу; запрет запуска при расхождении (`verify_model` уже есть).

## O-10. Нагрузка и SLO (P2)

* Сценарии k6/Locust: 50 одновременных клиентов портала на коллекции из 2 000 фото; 5 фотографов, каждый грузит 500 RAW; 100 задач в очереди на 2 GPU; цели — p95 API ≤ 400 мс, время ожидания задачи (interactive) ≤ 10 с, потеря задач = 0. Результаты — в `release/` как доказательство (`scripts/verification-gate.py --gate benchmark`).
