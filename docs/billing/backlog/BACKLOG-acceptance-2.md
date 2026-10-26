# Бэклог: Приёмка модулей (ACC), часть 2

### ACC-1B7 — приёмка этапа Selected/final операции (02609)

- Выполняет: человек или CI с PostgreSQL и Go. Модель только готовит блок доказательства.
- Условие входа: все задачи этапа закрыты, их тесты лежат в репозитории (R33).
- Команды (по порядку, остановиться на первой ошибке): 1) `python3 scripts/generate-billing-selected.py --check`; 2) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/contracts/test_billing_selected_contract.py`; 3) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/integration/test_billing_selected.py tests/integration/test_billing_selected_upgrade_guards.py` (нужна БД: `make dev-infra`); 4) `scripts/run-platform.sh go -C backend test ./internal/billing ./internal/v2input ./internal/workflow ./internal/run ./internal/v2render ./db` (нужны PostgreSQL и Go).
- Ожидается: `failed=0` и `skipped=0` в каждом наборе (`TESTING.md` T5); skip = провал.
- Доказательство: блок `### ACC-1B7 — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).
- Примечание: **Выполняет владелец (у него PostgreSQL и Go): это и есть `T-01`/`OPEN-08`.** Обязательны кейсы `v28_open_eyes_selected_authority` и `v28_glare_completed_source_ready`.

### ACC-finance — приёмка группы «Финансы (BILL-04)»

- Условие входа: закрыты все задачи `FIN-*` из `backlog/` вместе со своими тестами (R33) и `FIN-00` (каркас).
- Команды: 1) `python3 scripts/generate-billing-finance.py --check`; 2) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/contracts/test_billing_finance_contract.py`; 3) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/integration/test_billing_finance.py` (нужна БД).
- Ожидается: `failed=0`, `skipped=0`; для каждой таблицы и функции группы есть тест по `TESTING.md` (T4).
- Сценарии e2e этой темы (подсказка по ключевым словам, уточнить по 08/10): E07, E12, E20, E32, E33, E37, E56, E71, E72, E77, E82, E83.
- Доказательство: блок `### ACC-finance — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).

### ACC-provider — приёмка группы «Provider outbox/inbox (BILL-07/08)»

- Условие входа: закрыты все задачи `PRO-*` из `backlog/` вместе со своими тестами (R33) и `PRO-00` (каркас).
- Команды: 1) `python3 scripts/generate-billing-provider.py --check`; 2) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/contracts/test_billing_provider_contract.py`; 3) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/integration/test_billing_provider.py` (нужна БД).
- Ожидается: `failed=0`, `skipped=0`; для каждой таблицы и функции группы есть тест по `TESTING.md` (T4).
- Сценарии e2e этой темы (подсказка по ключевым словам, уточнить по 08/10): E05, E09, E10, E11, E13, E14, E15, E16, E17, E18, E19, E22, E23, E26, E27, E34, E35, E36, E40, E48, E49, E50, E52, E53, E55, E59, E64, E67, E69, E70, E74, E75, E76, E79, E81, E86, E87, E88, E89, E90.
- Доказательство: блок `### ACC-provider — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).

### ACC-usage — приёмка группы «Usage, storage, egress (BILL-05/06/09)»

- Условие входа: закрыты все задачи `USA-*` из `backlog/` вместе со своими тестами (R33) и `USA-00` (каркас).
- Команды: 1) `python3 scripts/generate-billing-usage.py --check`; 2) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/contracts/test_billing_usage_contract.py`; 3) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/integration/test_billing_usage.py` (нужна БД).
- Ожидается: `failed=0`, `skipped=0`; для каждой таблицы и функции группы есть тест по `TESTING.md` (T4).
- Сценарии e2e этой темы (подсказка по ключевым словам, уточнить по 08/10): E06, E21, E24, E25, E28, E29, E30, E31, E38, E39, E41, E42, E43, E45, E46, E47, E51, E68, E73.
- Доказательство: блок `### ACC-usage — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).

### ACC-legal — приёмка группы «Legal и privacy (BILL-11/12)»

- Условие входа: закрыты все задачи `LEG-*` из `backlog/` вместе со своими тестами (R33) и `LEG-00` (каркас).
- Команды: 1) `python3 scripts/generate-billing-legal.py --check`; 2) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/contracts/test_billing_legal_contract.py`; 3) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/integration/test_billing_legal.py` (нужна БД).
- Ожидается: `failed=0`, `skipped=0`; для каждой таблицы и функции группы есть тест по `TESTING.md` (T4).
- Сценарии e2e этой темы (подсказка по ключевым словам, уточнить по 08/10): E44, E57, E58, E60, E61, E62, E63, E65, E66, E78, E80.
- Доказательство: блок `### ACC-legal — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).

### ACC-api — приёмка API (BILL-10)

- Условие входа: закрыты все `API-*` с Go-тестами handler (R33).
- Команды: 1) `scripts/run-platform.sh go -C backend test ./internal/httpapi ./internal/billing`; 2) `make contracts-check`; 3) contract-тесты реестров endpoints и schemas.
- Ожидается: `ok` по всем пакетам, drift DTO нет, endpoint без handler не зарегистрирован.
- Доказательство: блок `### ACC-api — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).

### ACC-stripe — приёмка Stripe (fake, затем sandbox)

- Условие входа: закрыты `STR-00..STR-13`, `STR-16`, `TST-02`; ключей нет и не нужно.
- Команды: 1) Go-тесты пакета провайдера на fake (путь — из `STR-01`); 2) общий контрактный набор `TST-02` на fake.
- Ожидается: все PASS без сети и без секретов. Прогон того же набора на real — отдельная запись после `STR-14` (ключи, `OPEN-14`).
- Сценарии e2e: E05, E09, E10, E11, E13, E14, E15, E16, E17, E18, E19, E22, E23, E26, E27, E34, E35, E36, E40, E48, E49, E50, E52, E53, E55, E59, E64, E67, E69, E70, E74, E75, E76, E79, E81, E86, E87, E88, E89, E90 (сначала fake).
- Доказательство: блок `### ACC-stripe — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).

### ACC-ui — приёмка Paper UI

- Условие входа: закрыты все `UI-*` с dom-тестами (R33), `TST-04`.
- Команды: 1) `cd apps/photographer && npm test -- --run`; 2) `npm run typecheck`; 3) `make test-web`.
- Ожидается: все PASS; ключи bg/en/ru совпадают; native payment capability=false (`OPEN-05`).
- Сценарии e2e (UI-часть): E08, E54.
- Доказательство: блок `### ACC-ui — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).

### ACC-e2e — финальный прогон E01–E90

- Условие входа: приняты все остальные `ACC-*`, закрыты `TST-01`, `TST-03`.
- Команды: `make test-billing-e2e` (создаётся в `TST-03`) — все `tests/integration/test_billing_e2e_*.py` на одноразовой БД.
- Ожидается: для каждого из 90 сценариев есть положительный и отрицательный тест с docstring по шаблону; `failed=0`, `skipped=0`; список сценариев без теста пуст (проверяет `TST-03`).
- Не входит: real-Stripe (`STR-14`), native pixel (`OPEN-10`), live-продажи (`OPEN-09`).
- Доказательство: блок `### ACC-e2e — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).
