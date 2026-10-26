# MVP-06. Тесты, e2e, CI

Исходная оценка — [../04-testing-and-e2e.md](../04-testing-and-e2e.md). Цель: за ≈2–3 недели получить воспроизводимую проверку «зелёно/красно» главного пути MVP на чистой машине и в CI.

| ID | Приоритет | Работа | Трудоёмкость |
|---|---|---|---|
| T-1 | **P0** | Каркас e2e (Playwright) и тестовый стенд в репозитории | M |
| T-2 | **P0** | 20 e2e-сценариев главного пути | L |
| T-3 | **P0** | Единая команда `make verify` + CI | M |
| T-4 | P1 | Политика «skip = fail» для обязательных наборов | S |
| T-5 | P1 | Измерение покрытия и пороги | S–M |
| T-6 | P1 | Chaos-тесты сети worker↔сервер | M (см. [W-11](01-worker-and-vpn.md)) |
| T-7 | P1 | Проверка доступности и контраста | S |
| T-8 | P2 | Визуальные регрессии и регрессия качества результата | M |
| T-9 | P2 | Перенос полезных скриптов из `work/` в репозиторий | S |
| T-10 | P2 | Запуск «NOT RUN» тестов и реестр результатов | M |

## T-1. Каркас e2e и стенд (P0)

* **Структура (новая):**
  ```
  tests/e2e/
    playwright.config.ts          // projects: photographer-web, portal; locales ru/en/bg; trace on-first-retry
    package.json                  // @playwright/test, @axe-core/playwright
    fixtures/photos/              // 15–20 небольших лицензионно чистых файлов: JPEG (с ICC и без), TIFF16, PNG-alpha, 2 RAW (DNG малого размера), битый файл, дубликат
    fixtures/seed.ts              // создание tenant/пользователей/ролей через API/SQL
    helpers/{auth,mail,stack,assert-bytes}.ts
    specs/{photographer,portal}/*.spec.ts
  deploy/compose.e2e.yaml         // PostgreSQL, RabbitMQ (TLS), MinIO, migrate, api, object-gateway, publisher, scheduler, worker-cpu/media/archive, worker-gpu (backend=cpu где допустимо), photographer web, portal, caddy, mailpit
  ```
* **Вход по OTP:** SMTP-профиль `mail` уже есть в `deploy/compose.yaml`; в e2e — `mailpit`, хелпер `helpers/mail.ts` читает код из его API.
* **GPU-операции:** воркер `PHOTO_WORKER_CLASS=gpu PHOTO_WORKER_BACKEND=cpu` — допускается для операций с `backend=cpu` (`runtime/service.py: WorkerConfig.from_env`); для остальных — проверять корректное `unavailable` с причиной (это тоже сценарий).
* **Запуск:** `make e2e` → `docker compose -f deploy/compose.e2e.yaml up -d --wait` → `npx playwright test` → `down -v`. Один и тот же файл — локально и в CI.

## T-2. Сценарии (P0, 20 шт.)

Критерий каждого сценария — проверка **конечных данных**, а не только DOM (SHA оригинала, ICC, ориентация, размеры, права доступа).

| № | Приложение | Сценарий | Ключевые проверки |
|---|---|---|---|
| E-01 | photographer | Регистрация/вход по OTP, повторный вход, выход | cookie/сессия; выход очищает локальные журналы (все `pe:j:*`, [P-4](03-photographer.md)) |
| E-02 | photographer | Создание рабочей области, проекта, коллекции | идемпотентность двойного клика; i18n ru/en/bg |
| E-03 | photographer | Приглашение участника (роль `editor`), принятие, отзыв доступа | после отзыва — 403 на API и редирект в UI |
| E-04 | photographer | Импорт 10 файлов: JPEG/TIFF/PNG/DNG/битый/дубликат | оригиналы в хранилище бит-в-бит (SHA), отклонение битого с понятной причиной, дубликат помечен |
| E-05 | photographer | Пауза/возобновление/перезагрузка вкладки во время загрузки | докачка по журналу, без повторной отправки завершённых частей |
| E-06 | photographer | Отбор и сравнение, решения | сохранение, откат, конкурентное изменение (If-Match → 412/конфликт-UI) |
| E-07 | photographer | Редактор: правка → preview → отмена | недеструктивность: оригинал не изменён; отмена освобождает ресурсы |
| E-08 | photographer | Apply на серии (CPU-совместимые операции) | атомарность: либо все версии, либо ни одной; повтор запроса не дублирует |
| E-09 | photographer | Операция без квалификации/модели | кнопка недоступна с причиной; сервер возвращает `unavailable` |
| E-10 | photographer | Review результата: принять/отклонить | `reviewed` отражено в lineage; отклонённое не публикуется |
| E-11 | photographer | Экспорт JPEG и TIFF16 по пресету | ICC встроен, размеры, EXIF-ориентация=1, SHA соответствует манифесту |
| E-12 | photographer | Создание доставки и публикация ссылки | ссылка и секрет; отзыв ссылки |
| E-13 | photographer | Изоляция: чужой tenant/проект по прямым URL и API | 403/404, нет утечки существования |
| E-14 | photographer | Квоты/лимиты | превышение → понятное сообщение, нет частичных записей |
| E-15 | portal | Открытие по ссылке с секретом во фрагменте | секрет не уходит в запросы (проверка сетевого лога), `Referrer-Policy` |
| E-16 | portal | Невалидный/просроченный/отозванный секрет | экран `invalid`, без утечки данных |
| E-17 | portal | Просмотр, lightbox с клавиатуры, фокус-ловушка | axe без нарушений; Esc возвращает фокус |
| E-18 | portal | Скачивание одного файла и архива (тома) | байты совпадают с экспортом (SHA); отмена; повтор после обрыва сети |
| E-19 | portal | Гостевая загрузка с возобновлением | файл в хранилище, отображается владельцу, лимиты размера |
| E-20 | оба | Контраст/размер шрифта на всех выпускаемых экранах (обе темы) | `@axe-core/playwright` + порог font-size ([T-7]) |

Дополнительно (P1): E-21 разрыв API-соединения во время `Apply` и восстановление; E-22 отзыв доступа во время скачивания; E-23 `429` на портале и поведение `Retry-After` ([04 PT-6](04-portal.md)).

## T-3. `make verify` и CI (P0)

* **Makefile:** добавить

  ```make
  verify: lint typecheck test-unit test-web test-db e2e release-check
  lint:
  	cd apps/photographer && npm run lint && npm run format:check
  	cd apps/portal && npm run lint
  	$(RUN) go -C backend vet ./...
  	$(RUN) $(PYTHON) -m ruff check workers apps/studio-agent scripts
  typecheck:
  	cd apps/photographer && npm run typecheck
  	cd apps/portal && npx tsc --noEmit
  e2e:
  	docker compose -f deploy/compose.e2e.yaml up -d --wait
  	cd tests/e2e && npx playwright test
  	docker compose -f deploy/compose.e2e.yaml down -v
  ```

  и **включить** `test-web` и `test-db` в агрегирующий `test` (`Makefile:34`) либо ввести `test-all`.
* **CI (`.github/workflows/ci.yml` или аналог):** джобы — `static` (lint/typecheck), `unit` (Go/Python/JS параллельно), `integration` (services: postgres/rabbitmq/minio, `PHOTO_TEST_DATABASE_URL`, `PHOTO_REQUIRE_DB=1`), `e2e` (compose), `release` (images, SBOM, `make release-check`). Артефакты: отчёты, Playwright trace, покрытие. Срок хранения — 30 суток.
* **Matrix:** Go 1.26 (по `backend/go.mod`), Node 24 (по `@types/node` проекта), Python 3.11 (по `workers/pyproject.toml`); Linux обязательно, macOS — nightly для Metal-путей.

## T-4. «Skip = fail» (P1)

* **Проблема:** 413 Go-файлов и 76 Python-файлов завязаны на `PHOTO_TEST_DATABASE_URL`; пропуск реализован разрозненно (`if dsn=="" { t.Skip("isolated migrated database required") }` — несколько сотен копий, например `backend/internal/database/application_role_integration_test.go:39`).
* **Изменить (без массовой правки):**
  1. `scripts/check-skips.py`: читает `go test -json` и `pytest --junitxml`, собирает пропуски, **падает**, если среди них есть причины из списка обязательных (`isolated … database required`, `PHOTO_TEST_*`) при `PHOTO_REQUIRE_DB=1`; печатает таблицу «пакет → число пропусков».
  2. CI запускает `go test -json ./... | scripts/check-skips.py --require-db`; для Python — `pytest -rs --junitxml=… && scripts/check-skips.py`.
  3. Опционально (P2): общий хелпер `backend/internal/testenv/db.go` (`RequireDB(t)`) и codemod замены inline-проверок.
* **Критерий:** в CI нет пропусков по причине отсутствия БД/брокера/хранилища.

## T-5. Покрытие (P1)

* Go: `go test -coverprofile=cover.out ./...` + `go tool cover -func`; пороги: ≥ 70 % для `internal/{auth,tenant,collection,upload,portal,download,gateway,queue,run,workflow}`.
* Python worker: `pytest --cov=photo_worker --cov-report=xml`; пороги для `runtime/*` ≥ 75 %.
* JS: `vitest run --coverage` (provider `v8`) для `apps/portal/src/lib`, `apps/photographer/src/*-response.ts`, `*-store.ts`; порог 80 %.
* Публиковать тренд (Codecov/`coverage.json` в артефакте); пороги ужесточать постепенно.

## T-6. Chaos-тесты сети (P1)

См. [W-11](01-worker-and-vpn.md): `tests/integration/test_worker_network_faults.py`, профиль `fault` в `deploy/compose.e2e.yaml` (toxiproxy между воркером и PG/AMQP/gateway).

## T-7. Доступность и контраст (P1)

* `tests/e2e/specs/a11y.spec.ts`: для каждого выпускаемого маршрута (список из `src/routes.ts` + профиль MVP) и обеих тем — `new AxeBuilder({page}).withTags(['wcag2aa']).analyze()`; отдельная проверка `font-size ≥ 12px` для всех текстовых узлов (`page.evaluate` по `getComputedStyle`), исключения — только явный список.
* `scripts/check-contrast.mjs` ([P-6](03-photographer.md)) — токены тем; падение < 4,5:1.
* Результаты — в `docs/ui-audit/` как измерения UI-14 (закрывает обязательное условие №8 MVP-плана).

## T-8. Визуальная регрессия и качество результата (P2)

* Playwright `toHaveScreenshot` для 6–8 ключевых экранов (с маскированием динамики).
* Регрессия качества обработки — `resources/legacy-quality-baseline.json` + эталонный набор из `e2e/input/*.jpg`: nightly-задача сравнивает метрики (ΔE, SSIM, гистограммы) с допусками; расхождение → красный nightly, не блокирует PR.

## T-9. Перенос скриптов из `work/` (P2)

* Кандидаты: `work/browser-*/browser_cases.mjs`, `browser_owner.py`, инструмент Playwright (`work/tools/playwright-core-*`). Переписать как спецификации `tests/e2e/specs/*` (T-2), **не** копируя замороженные evidence-пакеты; `work/` остаётся неизменным архивом.

## T-10. «NOT RUN» тесты (P2)

* В `docs/implementation-v2/*.md` ≈300 записей «tests authored NOT RUN». Составить реестр `docs/ops/test-run-registry.json` (идентификатор теста → дата первого прогона → результат → ссылка на CI-запуск), автоматически заполняемый из CI; цель — 0 «NOT RUN» для включённых в MVP функций. Документацию править не вручную, а генерировать ссылку на реестр.
