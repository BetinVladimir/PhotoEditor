# План доработки MVP — что и где менять

Дата: 2026-10-24. Основание — [ревью](../README.md). Все пути указаны от корня репозитория `PhotoEditor`; номера строк — на момент ревью (могут сместиться).

**Рамки.** MVP = `apps/photographer` (web) + `apps/portal` + backend + worker (CPU/media/archive на серверах; GPU — CUDA/Apple Silicon по VPN). Вне MVP: полный биллинг (Stripe/кабинет), видеоредактор, native-приложения, V23/V24/V30(расш.)/V31/V32. Принцип проекта «код не удалять» сохраняется: отложенное **выключается профилем**, а не стирается.

## Файлы плана

| Файл | Область |
|---|---|
| [01-worker-and-vpn.md](01-worker-and-vpn.md) | Worker: устойчивость к VPN, control-plane API, TLS брокера, докачка, спул результата, Apple Silicon |
| [02-backend-and-contracts.md](02-backend-and-contracts.md) | Go backend: IP за прокси (D-18), rate-limit, метрики, baseline миграций, профиль MVP, рефакторинг монолитных функций, биллинг для беты |
| [03-photographer.md](03-photographer.md) | Приложение фотографа: allowlist маршрутов, форматтер/линтер, журнал мутаций, `OwnerAPI`, темы/контраст, импорт |
| [04-portal.md](04-portal.md) | Портал: домен/метаданные, заголовки безопасности, идемпотентность, декомпозиция, кэш, e2e |
| [05-deploy-and-ops.md](05-deploy-and-ops.md) | VPN, кластер малых серверов, HA, наблюдаемость, бэкапы, CI/CD, секреты |
| [06-testing-ci-e2e.md](06-testing-ci-e2e.md) | e2e (20 сценариев), `make verify`, CI, skip=fail, покрытие, a11y |
| [07-plan-gaps.md](07-plan-gaps.md) | Ревью самого плана: чего не хватает до MVP (X-1…X-10) |
| [08-wave1-review.md](08-wave1-review.md) | Ревью первой волны реализации ([IMPLEMENTED.md](IMPLEMENTED.md)) |
| [agent/README.md](agent/README.md) | **План для кодогенерации моделью 35B/32k:** карточки задач (`agent/tasks/`), очередь ([INDEX](agent/INDEX.md)), что нельзя отдавать модели ([NOT-CODEGEN](agent/NOT-CODEGEN.md)), инструменты `docs/review/MVP/agent/tools/` |

## Сводный бэклог (P0 — блокирует MVP; P1 — до релиза; P2 — после)

| Приоритет | ID | Задача | Файл-ориентир |
|---|---|---|---|
| **P0** | B-0 | Корректный IP клиента за прокси (иначе OTP-лимит общий для всех) | `backend/internal/httpapi/auth.go:113`, `catalog.go:290,318` |
| **P0** | W-1 | Терпимость heartbeat задачи | `workers/photo_worker/runtime/executor.py:180-192` |
| **P0** | W-2 | Терпимость heartbeat ёмкости | `runtime/capacity.py:65-79`, `runtime/service.py` (`_run_built_worker`) |
| **P0** | W-3 | Повтор commit + спул результата | `runtime/executor.py:250-262`, `runtime/database.py` |
| **P0** | W-6 | TLS RabbitMQ, `amqps`, параметры соединения | `deploy/compose.yaml`, `deploy/framevela/provision.py:52`, `runtime/transport.py` |
| **P0** | O-1 | Схема VPN и правила доступа | `docs/ops/network-topology.md`, `deploy/vpn/` |
| **P0** | O-2 | Выровнять прод с репозиторием (миграции, scheduler, `/health/version`) | `deploy/framevela/README.md`, `backend/db/migrations` |
| **P0** | P-1 | Allowlist маршрутов MVP в клиенте | `apps/photographer/src/routes.ts`, `app/[locale]/_layout.tsx` |
| **P0** | T-1…T-3 | e2e-каркас, 20 сценариев, `make verify` + CI | `tests/e2e/`, `Makefile`, `.github/workflows/ci.yml` |
| P1 | B-1 | Rate-limit | `backend/internal/httpapi/ratelimit.go` |
| P1 | B-2 | Метрики, расширенная готовность | `backend/internal/httpapi/metrics.go`, `server.go:114-115` |
| P1 | B-3 | Baseline миграций | `scripts/build-baseline.py`, `backend/db/` |
| P1 | B-4 | Серверный профиль MVP | `backend/internal/capability/service.go` |
| P1 | B-5 | Разрезать `catalogRoutes` (3 811 строк) | `backend/internal/httpapi/catalog.go` |
| P1 | B-9 | Scheduler ×2, failover | `backend/internal/workflow/service.go` |
| P1 | W-4, W-5, W-7, W-8, W-11, W-12 | Пул/TLS БД, докачка, control-plane API, идентичность воркера, chaos, часы | `workers/photo_worker/runtime/*`, `backend/internal/workerctl` |
| P1 | P-3…P-8 | Форматтер/линтер, журнал мутаций, `OwnerAPI`, темы, импорт, web-сборка | `apps/photographer/**` |
| P1 | PT-1…PT-7 | Домен, заголовки, идемпотентность, декомпозиция, кэш, `429`, e2e | `apps/portal/**` |
| P1 | O-3…O-9 | HA, наблюдаемость, бэкапы, CI/CD, секреты, Caddy, GPU-провижининг | `deploy/**` |
| P1 | T-4…T-7 | skip=fail, покрытие, chaos, a11y | `scripts/check-skips.py`, `tests/e2e` |
| P2 | B-6…B-8, B-10 | `ApplyCoordinator.build`, запуск сервисов, аудит context/panic, биллинг беты | `backend/internal/workflow/apply.go:834`, `cmd/photo/main.go` |
| P2 | W-9, W-10 | Рефакторинг `build_worker`, Mac-пакет | `runtime/service.py:343-852`, `deploy/mac-worker/` |
| P2 | P-9, P-10, PT-8, PT-9, O-10, T-8…T-10 | Декомпозиция экранов, телеметрия, конфиг портала, бренд, нагрузка, визуальные регрессии, реестр «NOT RUN» | — |

## Предлагаемая последовательность (оценка для 3–4 разработчиков; ориентир, не обязательство)

| Этап | Недели | Содержание | Выход |
|---|---|---|---|
| 0. Быстрые исправления | 0,5–1 | B-0, W-1, W-2, PT-1, PT-3, P-1 (маршруты), O-8 (заголовки), форматтер (P-3) отдельным коммитом | Закрыты подтверждённые дефекты D-01, D-02, D-10, D-18; отложенные функции скрыты |
| 1. Основа проверки | 1–2 | T-1…T-4, CI, `make verify`, O-6 | Любой PR получает зелёный/красный по unit+integration; skip=fail |
| 2. Надёжность worker по VPN | 2–3 | W-3, W-4, W-6, W-12, W-11, O-1 | Chaos-набор зелёный; воркер подключается по `amqps` через VPN |
| 3. Сквозной путь | 2–3 (параллельно) | T-2 (20 сценариев), P-2/T-7 (контраст/шрифт), PT-4…PT-7, P-4…P-8 | Главный путь фотографа + портал автоматизированы; измерения UI |
| 4. Установка и эксплуатация | 2–3 | B-3, O-2, O-3, O-4, O-5, B-2, B-9 | Чистая установка и апгрейд в CI; HA-профиль; метрики/алерты; drill восстановления |
| 5. Архитектурное упрощение | 3–4 (может идти после первого релиза) | W-7 (control-plane API), W-8, B-5…B-7, P-9 | Воркер без DB-учётки; монолитные функции разрезаны |
| 6. Закрытая бета | — | B-10 (квоты), измерения на GPU, матрица камер/форматов | Решение о выпуске |

## Определение готовности MVP (Definition of Done)

1. `make verify` зелёный в CI на чистой машине (unit, integration без skip по окружению, e2e 20 сценариев).
2. Чистая установка и обновление до головной версии проходят автоматически; `/health/version` совпадает с манифестом релиза.
3. GPU-воркер на удалённом хосте по VPN: chaos-набор (разрыв 5/20/70 с, задержка, потери) — без потерянных и без дублированных результатов.
4. Контраст и размер шрифта измерены на всех выпускаемых экранах обеих тем; нарушений нет.
5. Drill восстановления выполнен, RPO/RTO зафиксированы; алерты настроены и проверены.
6. Отложенные функции не доступны ни в навигации, ни по прямым URL, ни на сервере (профиль MVP).
7. Нет подтверждённых дефектов уровня «Выс» из [../05-defects-and-gaps.md](../05-defects-and-gaps.md).

## Чего делать не стоит

* Не вводить Kubernetes ради MVP; достаточно Compose на узлах + Ansible ([05 O-3](05-deploy-and-ops.md)).
* Не переписывать SQL-функции допуска/квалификации в API целиком до MVP: сначала [W-1…W-6](01-worker-and-vpn.md), затем постепенный переход на control-plane API.
* Не удалять существующие миграции и пакеты доказательств; baseline добавляется рядом ([B-3](02-backend-and-contracts.md)).
* Не считать «исторический PASS» доказательством (принцип самого проекта): любое изменение — повторный прогон затронутых проверок.
* Не форматировать и не менять поведение в одном коммите ([P-3](03-photographer.md)).
