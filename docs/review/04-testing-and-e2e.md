# 04. Покрытие тестами, e2e, CI

## 1. Инвентарь (подсчёт по исходникам, без запуска)

| Набор | Где | Объём | Как запускается | Требует |
|---|---|---|---|---|
| Go unit/integration | `backend/**/_test.go` | 3 416 файлов, ≈256 тыс. строк, **6 068** `func Test*`; 548 вызовов `t.Skip`; 413 файлов завязаны на `PHOTO_TEST_DATABASE_URL` | `make test-go` (`go -C backend test ./...`) | Go; для SQL-части — PostgreSQL/MinIO (`make dev-infra`) |
| Python platform/contract | `tests/platform`, `tests/contracts`, `tests/unit`, `tests/migration` | ≈5 600 тестов (770 файлов `platform`, 604 `integration`) | `make test-python`, `make test-db` | `.venv-platform`, для integration — БД |
| Python worker | `workers/tests` | 1 287 тестов | `pytest` (см. `workers/pyproject.toml`) | `uv sync --extra dev --extra ml --extra media` |
| Studio agent | `apps/studio-agent/tests` | 106 | `unittest discover` (в `make test-python`) | — |
| Скрипты контрактов | `scripts/test_*.py` | 26 файлов | в составе генераторов/`pytest` | — |
| JS/TS | `apps/photographer/src/*.test.*` (533 файла), `apps/portal/src/**.test.*`, `packages/api-client/src/*.test.ts` | ≈2 800 кейсов | `make test-web` / `npm test` | Node, vitest; photographer — jsdom + react-native-web |
| Legacy-редактор | `tests/test_pipeline.py`, `e2e/`, `run-e2e.sh`, `scripts/verify-e2e.py` | — | `make test-legacy` | Mac, `.venv` |
| Крупные I/O | `make test-zip64-large-io`, `test-s3-large-io`, `test-gateway-*-large-io` | opt-in через `PHOTO_RUN_*` | вручную | реальные объёмы |
| Gate-проверки | `scripts/verification-gate.py` (native, benchmark, restore, visual, hardware) | требуют внешние «evidence» | вручную | устройства/данные |

## 2. Что покрыто хорошо

* **Негативные и граничные сценарии:** подмена идентичности файла, обрыв потока, отмена, смена версии, повторы, конкурентный доступ, отзыв доступа, tenant-изоляция (по именам файлов: `*_cancellation_test.go`, `*_wire_integration_test.go`, `test_application_role_isolation.py`, `test_tenant_*`).
* **Контракты:** генераторы с `--check` (`make contracts-check`, `v2-check`) и контрактные тесты эндпоинтов (`scripts/test_*_endpoint_contract.py`).
* **Миграции:** проверка апгрейда с головной версии (`pytest` marker `migration_head`), SQL-ACL по ролям.
* **Клиент:** тесты разбора ответов (`*-response.test.ts`), журналов попыток (`*-attempt-store.test.ts`), DOM-тесты маршрутов (`*.dom.test.tsx`).
* **Портал:** `lib/*` покрыт точечно (скачивание, восстановление, отмена, виртуальная сетка).

## 3. Пробелы (по убыванию важности)

1. **Нет e2e-набора в репозитории.** Сценарии браузера существуют только как разрозненные скрипты в `work/*/browser_cases.mjs`, `browser_owner.py` и локально установленный Playwright (`work/tools/playwright-core-1.63.0-…`). Каталог `work/` игнорируется git (`.gitignore:231`) → воспроизвести e2e на чистой машине нельзя. Нет `playwright.config.*`, нет `tests/e2e/`.
2. **Нет CI.** Отсутствуют `.github/`, `.gitlab-ci.yml` и аналоги. Единственный «gate» — ручные `make` цели и журналы в `docs`.
3. **`make test` не включает** `test-web` и `test-db`:
   `test: contracts-check v2-check test-legacy test-python test-billing-contracts test-go` (`Makefile:34`). Т.е. «зелёный `make test`» не говорит ничего о photographer/portal и о SQL-интеграции.
4. **Покрытие не измеряется:** нет `--cov`, `go test -cover`, `vitest --coverage`. Метрика «6 000 тестов» не равна покрытию строк/веток.
5. **Тесты, зависящие от окружения, молча пропускаются.** 413 Go-файлов и 76 Python-файлов привязаны к `PHOTO_TEST_DATABASE_URL`; без переменной они skip → «зелёно». План MVP требует «без необъяснённых FAIL/SKIP», но механизма проверки нет.
6. **Незапущенные тесты.** В `docs/implementation-v2/*.md` ≈1 350 строк «NOT RUN», из них ≈300 — «tests authored NOT RUN». До первого прогона их надёжность не известна (возможны ложные «зелёные»/красные).
7. **Клиентские тесты не исполняют целевую платформу.** Vitest гоняет `react-native-web` в jsdom (`apps/photographer/vitest.config.mjs`): реальные Expo/iOS/Android, навигация `expo-router`, `SecureStore`, файловые API — подменены заглушками (`src/test/secure-store.ts`). Нативная приёмка вынесена в gate `native` и не автоматизирована.
8. **Нет тестов производительности и нагрузки** портала/API (только `make benchmark` через gate с внешними доказательствами).
9. **Нет «chaos»-тестов сети между worker и control plane** (разрыв VPN, высокая задержка, потеря пакетов) — критично для заявленной топологии. Существующие fault-тесты (`make test-fault`: `test_task_leases.py`, `test_dead_letters.py`, `test_generation_preview_continuation.py`, `test_media_retry_intake.py`) проверяют серверную часть восстановления, а не поведение worker при обрыве канала.
10. **Тесты ML-качества** — отдельно (визуальный gate, `legacy-quality-baseline.json`); автоматический контроль регрессии качества результата в CI не настроен.

## 4. Покрытие критических пользовательских путей MVP автоматизацией

| Путь | Unit/contract | Интеграция (БД, MinIO) | e2e в репозитории |
|---|---|---|---|
| Вход по OTP, сессия, выход (очистка локальных журналов) | ✅ | ✅ | ❌ |
| Создание проекта/коллекции, участники, приглашения | ✅ | ✅ | ❌ |
| Импорт файлов (RAW/JPEG/TIFF), пауза/возобновление, дубликаты | ✅ | ✅ (large-io opt-in) | ❌ |
| Отбор/сравнение, ручные решения | ✅ | ✅ | ❌ |
| Редактор: операции, preview, apply, отмена | ✅ | ✅ | ❌ |
| Экспорт и пресеты, пакет с ICC/metadata | ✅ | ✅ | ❌ |
| Публикация → ссылка → портал → скачивание | ✅ (lib) | ✅ (`test_portal_item_download.py`) | ❌ |
| Гостевая загрузка в портале | ✅ | ✅ | ❌ |
| Отказ чужому tenant/роли/отозванному доступу | ✅ | ✅ | ❌ |
| Потеря связи worker↔сервер | частично (сервер) | ❌ | ❌ |
| Backup/restore, purge | ✅ (операторы) | «NOT RUN» | ❌ |
| Контраст/размер шрифта | ❌ | — | ❌ |

## 5. Выводы и рекомендации

* Для MVP достаточно **15–20 e2e-сценариев** на двух приложениях и один «smoke» на каждую среду; не требуется воспроизводить 63 исторических браузерных сценария (сам MVP-план это допускает).
* До e2e нужно «заземлить» существующие тесты: **единая команда** (`make verify`), **CI** с PostgreSQL/RabbitMQ/MinIO-сервисами, **политика «skip = fail»** для обязательных наборов, **отчёт о покрытии** (хотя бы по пакетам и по критическим модулям).
* Подробный план и файлы — [MVP/06-testing-ci-e2e.md](MVP/06-testing-ci-e2e.md).
