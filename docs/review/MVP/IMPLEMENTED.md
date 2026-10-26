# Что реализовано (волна 1) и что проверено

Дата: 2026-10-24. Реализованы пункты плана, которые можно сделать без GPU, кластера и VPN. Ничего не удалено, миграции не трогались.

## Изменения

| ID | Что | Где |
|---|---|---|
| W-1 | Heartbeat переживает транзиентные ошибки: ретрай с backoff 1–5 с, пока локальная аренда (`_lease_until`) не ближе 2 с к истечению; только тогда `_lost`. Отказ сервера (`accept_heartbeat=False`) по-прежнему останавливает сразу | `workers/photo_worker/runtime/executor.py` (`heartbeat()`, `HEARTBEAT_SAFETY_MARGIN_SECONDS`) |
| W-3 | Терминальный commit ретраится при `OperationalError/OSError/TimeoutError` (6 попыток, backoff до 15 с). Commit идемпотентен по fence (`replayed/stale`). Spool-файл на диск НЕ сделан | `executor.py` (`_commit_with_retry`, `COMMIT_RETRY_ATTEMPTS`) |
| W-2 | `CapacityReporter` терпит сбои отчёта 90 с с последнего успеха; явный отказ сервера (`RegistrationRejected`) закрывает воркер сразу | `workers/photo_worker/runtime/capacity.py` |
| B-0 | IP клиента из `X-Forwarded-For` только от доверенных прокси (`TRUSTED_PROXY_CIDRS`), берётся правый недоверенный hop; 3 места с `RemoteAddr` заменены | `backend/internal/httpapi/clientip.go`, `auth.go`, `catalog.go`, `backend/internal/config/config.go`, `deploy/compose.yaml` |
| O-8 | Caddy: `header_up X-Forwarded-For`, HSTS, X-Frame-Options, COOP, Permissions-Policy и базовый CSP (`frame-ancestors`, `base-uri`, `object-src`, `form-action`) для app и portal | `deploy/caddy/Caddyfile`, `deploy/framevela/Caddyfile` |
| PT-1 | Убран захардкоженный `framevela.com` из `LocaleLinks`; canonical/hreflang главной берут `NEXT_PUBLIC_SITE_ORIGIN` (по умолчанию прежний домен); с share-страницы `alternates` убраны (она noindex) | `apps/portal/src/components/LocaleLinks.tsx`, `src/app/(localized)/[locale]/page.tsx`, `.../s/[share_id]/page.tsx` |
| PT-3 | `redeemDownload`/`downloadItem` принимают `Idempotency-Key`; `Portal.tsx` держит один ключ на логическую загрузку до успеха | `apps/portal/src/lib/api.ts`, `src/components/Portal.tsx` |
| P-1 | MVP-профиль: `interchange/*`, `exports/sign`, `styles/[id]/training` закрыты гардом в layout (`EXPO_PUBLIC_MVP_PROFILE=full` включает всё), ключ `feature_not_in_mvp` в en/ru/bg | `apps/photographer/src/mvp-profile.ts`, `app/[locale]/_layout.tsx`, `locales/*/common.json` |
| T-1 | `scripts/check-skips.py` + baseline `scripts/skips-baseline.txt` (572 существующих skip в 448 файлах): новый skip = падение; цели `make check-skips`, `make verify` | `scripts/`, `Makefile` |
| CI | Первый workflow: skips, go vet/test, worker pytest, web (portal + photographer) | `.github/workflows/ci.yml` |

Новые тесты: `workers/tests/test_capacity_reporter_tolerance.py`, `backend/internal/httpapi/clientip_test.go`, `apps/photographer/src/mvp-profile.test.ts`.

## Что реально запускалось

- `clientip_test.go` — прошёл (`go test`, но в изолированном модуле со стабом `config.Config`, не в полном репозитории).
- `test_capacity_reporter_tolerance.py` — 3 теста прошли (прямой запуск, `psutil` замокан).
- `tsc --noEmit` в `apps/portal` и `apps/photographer` — 0 ошибок.
- `py_compile` для `executor.py` и `capacity.py` — ок.
- `check-skips.py` — работает на репозитории.

## Что НЕ проверено (важно)

- Полные наборы Go, worker pytest, vitest (портал и фотограф), `next build`, `expo export`: на устройстве нет Go, Python 3.11 с зависимостями и рабочего vitest (нет rolldown-binding). Запустить: `make verify` (или CI).
- Изменения W-1/W-3 в `executor.py` не покрыты новыми тестами и не прогонялись против существующих тестов воркера. Тест `test_heartbeat_chain_source_pins.py` завязан на текст `database.py` — он не менялся, но `executor.py`-тесты стоит прогнать первыми.
- `config.go` (поле + валидация CIDR) и правки `auth.go`/`catalog.go` не компилировались в полном модуле; логика `clientIP` проверена отдельно. Нужен `go build ./...`.
- Locale-тесты (паритет ключей, `ui-localization-boundary`) не запускались после добавления `feature_not_in_mvp`.
- Caddyfile не валидировался `caddy validate`. CSP намеренно минимальный (без `script-src`): полный CSP требует прогона портала в браузере.
- `ci.yml` ни разу не выполнялся.

## Не сделано из волны 0/1 (остаётся в плане)

- W-3 spool-файл результата; W-6 (RabbitMQ TLS 5671, `provision.py` amqps, таймауты/keepalive транспорта под VPN) — нужна выдача сертификатов и стенд.
- W-4/W-5/W-7/W-8, B-1…B-9, O-1…O-10 (бэкапы, мониторинг, секреты), T-4…T-10, e2e на Playwright, X-1…X-10.
- Сквозной прогон «фотограф → портал» на реальном стенде с воркером и VPN: без него MVP-готовность не подтверждена.
