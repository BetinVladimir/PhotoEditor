# MVP-02. Backend (Go), контракты, миграции

Пути — от корня репозитория. Размеры/строки — на 2026-10-24.

| ID | Приоритет | Работа | Трудоёмкость |
|---|---|---|---|
| B-0 | **P0** | Корректный IP клиента за прокси (D-18) | S |
| B-1 | P1 | Ограничение частоты запросов | M |
| B-2 | P1 | Метрики и расширенная готовность | M |
| B-3 | P1 | Baseline миграций для чистой установки | L |
| B-4 | P1 | Режим профиля MVP (серверный allowlist функций) | M |
| B-5 | P1 | Рефакторинг `catalogRoutes` (3 811 строк) | M |
| B-6 | P2 | Рефакторинг `ApplyCoordinator.build` (2 317 строк) | L |
| B-7 | P2 | Рефакторинг `runSchedulerWithDeliverySigning`, `runWithAPIDependencies` | M |
| B-8 | P2 | Аудит `context.Background()` и `panic(` | S |
| B-9 | P1 | Scheduler: два экземпляра + failover | M |
| B-10 | P2 | Биллинг: минимум для закрытой беты | M–L |

## B-0. Корректный IP клиента (P0)

* **Проблема:** `backend/internal/httpapi/auth.go:113`, `catalog.go:290`, `catalog.go:318` используют `net.SplitHostPort(r.RemoteAddr)`. За Caddy это адрес прокси; лимит OTP по IP (`backend/internal/auth/service.go:140-158`, `{"ip", ip, 20}`) общий для всех пользователей.
* **Изменить:**
  1. Новый файл `backend/internal/httpapi/clientip.go`: `func (s *Server) clientIP(r *http.Request) string`. Алгоритм: если пир (`RemoteAddr`) входит в `Config.TrustedProxyCIDRs` — взять **крайний справа** адрес из `X-Forwarded-For`, не входящий в доверенные сети; иначе — `RemoteAddr`. Заголовки от недоверенных пиров игнорировать. Для случая Cloudflare → Caddy → API: доверять `CF-Connecting-IP` только если пир в опубликованных диапазонах Cloudflare (список в конфиге).
  2. `backend/internal/config/config.go`: поле `TrustedProxyCIDRs []netip.Prefix` из `TRUSTED_PROXY_CIDRS` (пусто = не доверять ничему; в production для compose — подсеть `backend`).
  3. Заменить 3 вызова на `s.clientIP(r)` (`auth.go:113`, `catalog.go:290`, `catalog.go:318`); при ошибке разбора не отвечать `422`, а использовать запасное значение `"unknown"`.
  4. Caddy: `deploy/caddy/Caddyfile`, `deploy/framevela/Caddyfile` — `reverse_proxy … { header_up X-Forwarded-For {remote_host} }` (затереть клиентское значение) и `servers { trusted_proxies … }`, если перед Caddy стоит Cloudflare.
* **Тесты:** `backend/internal/httpapi/clientip_test.go` — таблица: прямой клиент, клиент через доверенный прокси, спуфинг `X-Forwarded-For` от недоверенного пира, цепочка из 2 прокси, IPv6; `auth_test`: два разных клиента → разные счётчики `photo.auth_rate_limits`.

## B-1. Ограничение частоты (P1)

* **Где добавить:** `backend/internal/httpapi/ratelimit.go` (новый), подключение в `server.go: (*Server).Routes` (рядом с `r.Use(middleware.RequestID, middleware.Recoverer)`) и точечно в `catalogRoutes` через `r.With(s.limit("portal_session"))`.
* **Классы и лимиты по умолчанию (настраиваются):**
  | Класс | Маршруты | Ключ | Лимит |
  |---|---|---|---|
  | `portal_session` | `POST /api/v1/portal/sessions` | IP + share_id | 10/мин |
  | `portal_download` | `…/download-intent`, `/api/v1/downloads/redeem`, `…/download-jobs` | сессия портала | 30/мин |
  | `portal_upload` | `/api/v1/portal/uploads*` | сессия портала | 120/мин (части) |
  | `auth_register` | регистрация, `/api/v1/auth/csrf` | IP | 30/час |
  | `owner_upload_begin` | начало загрузки владельца | пользователь | 600/мин |
* **Реализация:** токен-бакет в памяти процесса (достаточно для MVP при одном экземпляре API) с интерфейсом `Limiter`, чтобы позже подключить общий стор (PostgreSQL `photo.auth_rate_limits` по образцу OTP или Redis). Ответ `429` с `Retry-After`, код `RATE_LIMITED` (существующий `domain.NewError("RATE_LIMITED", 429)`).
* **Тесты:** unit для бакета, интеграционный — всплеск запросов к `portal/sessions`.

## B-2. Метрики и готовность (P1)

* **Где:** `backend/internal/httpapi/server.go` (`/health/live`, `/health/ready` — строки 114–115), новый `backend/internal/httpapi/metrics.go`, роль-специфичные счётчики в `backend/internal/queue/publisher.go` (outbox), `backend/internal/workflow/service.go` (scheduler), `backend/internal/gateway/service.go`.
* **Что добавить:**
  * Отдельный внутренний листенер `METRICS_LISTEN` (не публиковать через Caddy) с `/metrics` (Prometheus).
  * Метрики: `http_requests_total{route,status}`, `http_request_duration_seconds`; `outbox_pending`, `outbox_oldest_age_seconds`; `task_leases_expired_total`, `task_dead_letters_total`, `task_retries_total`; `scheduler_is_leader`; `worker_last_heartbeat_age_seconds{worker,class}`; `gateway_bytes_total{direction}`; размер очередей RabbitMQ (через management API или pull из `queue`-пакета).
  * `/health/ready`: добавить проверки «scheduler-лидер или standby жив», «outbox lag < порога», «≥1 здоровый воркер каждого обязательного класса» (как отдельный блок `degraded`, не как 503, чтобы не ронять API при отсутствии GPU).
  * Алерты: `deploy/alerts/*.yaml` (пример для Prometheus/Alertmanager).
* **Тесты:** `httpapi/metrics_test.go`; проверка, что `/metrics` недоступен через публичный маршрут Caddy.

## B-3. Baseline миграций (P1)

* **Проблема:** 2 626 файлов в `backend/db/migrations` (нумерация до `02659`), крупные 80–170 КБ; в плане релиза упомянут 27 МБ сгенерированный SQL (R6); чтение migration source ограничено 64 MiB (`backend/db/validation.go`, `ValidateBundle`).
* **Изменить:**
  1. Скрипт `scripts/build-baseline.py`: из зафиксированной принятой версии N создать `backend/db/baseline/<N>.sql` (схема + функции + ACL + сиды), плюс запись в таблицу версий goose.
  2. Установщик (`PHOTO_ROLE=migrate`, `backend/cmd/photo/main.go`) на пустой БД применяет baseline, затем миграции > N; на существующей — обычную цепочку.
  3. Тест эквивалентности `tests/migration/test_baseline_equivalence.py`: схема после baseline+tail == схема после полной цепочки (через `pg_dump --schema-only` + нормализация), включая `pg_proc` (владельцы `photo_fn_owner`, `prosecdef`, `proconfig`), ACL, индексы.
  4. Старые файлы **не удалять** (требование проекта) — перенести в `backend/db/migrations/` без изменений, baseline добавить рядом.
* **Критерий:** установка «с нуля» ≤ минут, а не десятков минут; единая команда «fresh + upgrade» в CI.

## B-4. Профиль MVP на сервере (P1)

* **Проблема:** в клиенте 42 экрана с отложенными функциями; защита — серверные capability, разбросанные по `httpapi/*_capabilities.go` (analysis, apply, capture, editor).
* **Изменить:**
  1. `backend/internal/capability/service.go`: введение профиля `PHOTO_PROFILE=mvp|full` и таблицы `mvpAllowed = {operation → true}`; для всего вне списка — `status: "unavailable", reason: "NOT_IN_MVP"`.
  2. `backend/internal/httpapi/*_capabilities.go`, `interchange*.go`, `delivery*.go`, `glare_*`, `lens_submit.go`, `multi*`, `perspective*`: для отложенных групп маршрутов возвращать `404/403 FEATURE_DISABLED` до бизнес-логики (middleware `s.requireProfile("interchange")` через `r.With(...)`).
  3. Список MVP формируется из `docs/implementation-v2/mvp-release-scope-2026-10-15.md` и фиксируется в `packages/contracts/mvp-operations.json` (golden-файл, проверяется `make contracts-check`).
* **Тесты:** `backend/internal/capability/mvp_profile_test.go`; контрактный тест: клиент не ссылается на маршруты вне профиля (см. [03](03-photographer.md), P-1).

## B-5. Декомпозиция `catalogRoutes` (P1)

* **Где:** `backend/internal/httpapi/catalog.go` — `func (s *Server) catalogRoutes(r chi.Router)` (≈3 811 строк, всё inline).
* **Как (без изменения поведения):**
  1. **Сначала зафиксировать таблицу маршрутов:** тест `httpapi/routes_golden_test.go` — `chi.Walk` → отсортированный список `METHOD path` в `testdata/routes.golden`.
  2. Разрезать по доменам: `routes_projects.go`, `routes_collections.go`, `routes_uploads.go`, `routes_exports.go`, `routes_portal.go`, `routes_capture.go`, `routes_interchange.go`, `routes_admin.go`; каждый — метод `func (s *Server) xRoutes(r chi.Router)`; inline-обработчики → именованные методы `func (s *Server) handleX(w, r)`.
  3. Проверка: golden не меняется; `make test-go` и `scripts/test_*_endpoint_contract.py` зелёные.

## B-6. Декомпозиция `ApplyCoordinator.build` (P2)

* **Где:** `backend/internal/workflow/apply.go:834` — `func (c ApplyCoordinator) build(ctx, l ApplyLease) (bool, error)` ≈ 2 317 строк.
* **Как:** выделить стадии с явными входами/выходами — `resolvePlan`, `reserveQuota`, `materializeNodes`, `sealGraph`, `publishTasks`, `recordReceipt` — в структуре `applyBuild` (значения передаются между стадиями; общий `tx` — явным параметром). Каждый шаг — отдельный файл `apply_build_<stage>.go`. Добавить логирование времени стадий. Использовать существующие тесты `internal/workflow/*_test.go`, `tests/platform/test_apply_*migration.py` как сетку безопасности; сначала писать характеризационные тесты на границах стадий.

## B-7. Рефакторинг запуска сервисов (P2)

* **Где:** `backend/internal/workflow/service.go` — `runSchedulerWithDeliverySigning` (≈1 395 строк); `backend/cmd/photo/main.go` — `runWithAPIDependencies` (≈638 строк), 60 файлов `cmd/photo/*_installation.go|*_create.go`.
* **Как:** единая модель «компонент»: `type Component interface{ Name() string; Start(ctx) error; Ready() error; Stop(ctx) error }`, реестр компонентов по ролям; `*_installation.go` → регистрации компонентов; общий `Supervisor` (старт в порядке зависимостей, остановка в обратном, readiness агрегирует `Ready()`).

## B-8. Аудит `context.Background()` и `panic(` (P2)

* 88 вхождений `context.Background()` вне тестов и 7 `panic(` (подсчёт по `backend/internal`, `backend/cmd`). Составить таблицу «файл:строка — причина — решение» (`docs/review/MVP/audit-context-panic.md` создаётся исполнителем); обработчики HTTP/consumer должны наследовать `r.Context()`/контекст сообщения; фоновые задачи — иметь отдельный корень с `WithCancel` от процесса.

## B-9. Scheduler: два экземпляра (P1)

* **Где:** `backend/internal/workflow/service.go` (лидер через `pg_try_advisory_lock(2026,32)` на выделенном соединении; `docs/implementation-v2/18-capacity-operations.md`), `deploy/compose.yaml` (сервис `scheduler`, профиль `v2`).
* **Изменить:** запустить 2 реплики (лидер + standby), readiness различает роли; тест отказа: убить лидера → standby подхватывает ≤ 15 с, нет двойного `claim`/выдачи lease (проверка fencing); метрика `scheduler_is_leader`.

## B-10. Биллинг для закрытой беты (P2)

* **Состояние:** схема и quota kernel (миграции `02598…02609`) готовы; enforcement — за флагами (`shadow`, `opt-in`, `default-disabled`); Stripe/ledger/UI нет (`docs/billing/STATUS.md`, этап 1Б.7 не принят).
* **Минимум для MVP (бесплатная закрытая бета):**
  1. Включить enforcement **хранилища и исходящего трафика** (kernel уже умеет) со значениями Trial из `02599_billing_catalog_seed.sql`; AI-операции — в режиме `shadow` с логированием расхода.
  2. Закрыть `docs/billing/OPEN.md` пункт OPEN-08 (selected/final) до включения.
  3. Страница «Использование и лимиты» (экран `apps/photographer/app/[locale]/settings/quotas.tsx` существует — проверить, что он читает данные ядра квот, и довести).
* **После MVP (платный запуск):** B2 — Stripe Checkout/Customer Portal, webhook `POST /api/v1/billing/stripe/webhook` (подпись, идемпотентность по `event.id`, ledger-записи); B3 — кабинет; B4 — сквозная приёмка. Код Stripe писать в `backend/internal/billing/stripe*.go`; ключи — только из секретов.
