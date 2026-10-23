# TASKS — атомарные задачи

Перед любой задачей прочитать `RULES.md`. Взять одну задачу. Контекст собирать `tools/build-pack.py` (см. `QWEN-PROMPT.md`).
Задачи для ещё не реализованных таблиц, функций и API — в `backlog/` (сгенерированы, ID вида `FIN-T09`, `USA-F19`, `API-03`).

## Шаблон задачи (обязательные поля)

```
ID:        T-NN
Цель:      одно предложение, один результат
Вход:      точные файлы и разделы (и только они)
Выход:     точные файлы; для функций — полная signature
Права/ACL: кто получает EXECUTE (или «никто»)
Критерий:  нумерованные проверки A, B, C… (команда + ожидаемый результат)
Запреты:   ссылки на R-номера и специфичные
Стоп:      условие остановки и запись в OPEN.md
```

## Команды проверки (проверены 23.10.2026; в песочнице без PostgreSQL и Go работают первые три)

- Всё статическое одной командой: `bash docs/billing/tools/check-static.sh` (линтер, карточки, образцы, 9 генераторов `--check`, 42 контрактных теста). Подключено в `make test` как `test-billing-contracts`.
- Контрактные тесты: `python3 -m pytest -q -p no:cacheprovider tests/contracts/test_billing_*.py`.
- Генератор этапа: `python3 scripts/generate-billing-<имя>.py --check`, имена: catalog, quota, workflow, routed-workflow, render, editor, operations, paid-operation, selected.
- Integration (нужен PostgreSQL): `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q tests/integration/test_billing_*.py`.
- Go (нужен Go 1.26): `scripts/run-platform.sh go -C backend test ./internal/billing ./internal/run ./internal/v2sessions ./internal/workflow ./internal/v2input ./internal/v2render ./db`.

## Готовые к выполнению

### T-01 — Закрыть приёмку 1Б.7 (миграция 02609)
- **Цель:** зафиксировать в `STATUS.md` результат совместного прогона PostgreSQL и Go для 02609 после 02608.
- **Вход:** `18-selected-final-operations.md`, `contracts/selected-workflow.sql`, `contracts/selected-functions.json`, `contracts/paid-operation-funding.sql`.
- **Выход:** запись в `STATUS.md` (дата, команды, число PASS по каждому набору отдельно). При дефекте — правка source в `contracts/selected-workflow.sql`, перегенерация, новая версия source.
- **Критерий:** A. `python3 scripts/generate-billing-selected.py --check` PASS. B. `tests/contracts/test_billing_selected_contract.py`, `tests/integration/test_billing_selected.py`, `tests/integration/test_billing_selected_upgrade_guards.py` PASS без skip, включая open-eyes (`v28_open_eyes_selected_authority`) и glare (`v28_glare_completed_source_ready`). C. Go-команда выше PASS. D. Числа разных прогонов не суммировать.
- **Запреты:** R9, R14, R30; цены и policies не публиковать.
- **Стоп:** прогон невозможен без рабочей БД → `OPEN-08`.
- **Контекст:** `python3 docs/billing/tools/build-pack.py --task T-01 --brief 1B7 --section "18#Admission" --section "18#Контракты" --func selected-workflow.sql:billing_bind_operations --out pack.md`

### T-02 — Реализовать решение OPEN-11: обработка кодов ошибок и контрактный тест
- **Цель:** `MapError` отдаёт ответы по `contracts/error-classes.json`, и новый billing-SQL не может добавить код без классификации.
- **Вход:** `ERROR-CODES.md` (раздел «Решено, нужна правка Go»), `contracts/error-classes.json`, `backend/internal/billing/workflow.go` (`MapError`), `backend/internal/domain/domain.go`, образец `EXAMPLES-TESTS.md`.
- **Решение владельца:** группа A (26 кодов) — остаются 500, ничего не мапить; B (8) → `VALIDATION_FAILED` 422; C (8) → новый код `BILLING_STATE_CONFLICT` 409; D (3) → `BILLING_UNAVAILABLE` 503.
- **Выход:** правка `MapError` (19 кодов B, C, D); `tests/contracts/test_billing_error_codes.py`, который читает `RAISE EXCEPTION` из миграций, `error-classes.json` и `MapError` из Go-исходника и сверяет их.
- **Критерий:** A. Тест PASS на текущем дереве. B. Новый `RAISE EXCEPTION 'BILLING_…'` без записи в `error-classes.json` и без ветки в `MapError` даёт FAIL. C. `go test ./internal/billing` PASS. D. `python3 docs/billing/tools/gen-error-codes.py --check` PASS, раздел «Решено, нужна правка Go» пуст после правки Go. E. Код `BILLING_STATE_CONFLICT` проверен там, где клиенты перечисляют коды ошибок (`grep -rn BILLING_QUOTA_EXCEEDED apps packages locales`), добавлен рядом с ним.
- **Запреты:** R9; статусы и коды не менять сверх решения.
- **Стоп:** если для `BILLING_STATE_CONFLICT` нужен ключ локализации, формат которого не определён по соседним кодам — записать вопрос в `OPEN.md`.

### T-03 — Вынести повтор «холодного» Prepare в один helper (Go)
- **Цель:** убрать четыре копии логики «200 мс ожидания блокировки → `BILLING_UNAVAILABLE` 503».
- **Вход:** `backend/internal/billing/workflow.go`, `editor.go`, `render.go`, `operations.go` (функции Prepare*).
- **Выход:** приватная функция в пакете billing; четыре Prepare* используют её; экспортируемые сигнатуры не меняются.
- **Критерий:** A. `go build ./...` и `go vet ./internal/billing` PASS. B. `go test ./internal/billing ./internal/run ./internal/v2input` PASS. C. Константа таймаута одна, значение 200 мс. D. Поведение при таймауте и при отмене родительского контекста то же (проверить тестом по образцу `TestIntegrationOperationColdAdmission`).
- **Запреты:** R2; SQL не менять.

### T-04 — Unit-тест `MapError` без базы (Go)
- **Цель:** табличный тест сопоставления SQL-кодов и `domain.Error`.
- **Вход:** `backend/internal/billing/workflow.go`, `backend/internal/domain/domain.go`, `ERROR-CODES.md` (раздел «Обработаны»).
- **Выход:** `backend/internal/billing/maperror_test.go`.
- **Критерий:** A. Покрыты все 11 обработанных кодов и `55P03`. B. Необработанный код и не-Postgres ошибка возвращаются как есть. C. `go test ./internal/billing -run TestMapError` PASS.
- **Запреты:** не менять `MapError`.

## Блоки 1В–4

Вместо крупных блоков — сгенерированный бэклог `backlog/README.md`: 30 таблиц, 42 функции, 16 API-операций, 17 Stripe-задач, 17 UI-задач, 90 e2e-сценариев. Ключи Stripe не нужны до `STR-14` и `STR-15`: остальное проверяется на fake-провайдере (`STR-01`, порядок добавления ключей — `STRIPE-CONFIG.md`).
