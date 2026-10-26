# Бэклог: Приёмка модулей (ACC), часть 1

### ACC-1A — приёмка этапа Каталог (02598–02599)

- Выполняет: человек или CI с PostgreSQL и Go. Модель только готовит блок доказательства.
- Условие входа: все задачи этапа закрыты, их тесты лежат в репозитории (R33).
- Команды (по порядку, остановиться на первой ошибке): 1) `python3 scripts/generate-billing-catalog.py --check`; 2) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/contracts/test_billing_catalog_contract.py`; 3) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/integration/test_billing_catalog.py` (нужна БД: `make dev-infra`).
- Ожидается: `failed=0` и `skipped=0` в каждом наборе (`TESTING.md` T5); skip = провал.
- Доказательство: блок `### ACC-1A — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).

### ACC-1B1 — приёмка этапа Quota kernel (02600–02601)

- Выполняет: человек или CI с PostgreSQL и Go. Модель только готовит блок доказательства.
- Условие входа: все задачи этапа закрыты, их тесты лежат в репозитории (R33).
- Команды (по порядку, остановиться на первой ошибке): 1) `python3 scripts/generate-billing-quota.py --check`; 2) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/contracts/test_billing_quota_contract.py`; 3) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/integration/test_billing_quota.py` (нужна БД: `make dev-infra`).
- Ожидается: `failed=0` и `skipped=0` в каждом наборе (`TESTING.md` T5); skip = провал.
- Доказательство: блок `### ACC-1B1 — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).

### ACC-1B2 — приёмка этапа Standard Apply (02602–02603)

- Выполняет: человек или CI с PostgreSQL и Go. Модель только готовит блок доказательства.
- Условие входа: все задачи этапа закрыты, их тесты лежат в репозитории (R33).
- Команды (по порядку, остановиться на первой ошибке): 1) `python3 scripts/generate-billing-workflow.py --check`; 2) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/contracts/test_billing_workflow_contract.py`; 3) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/integration/test_billing_apply.py` (нужна БД: `make dev-infra`); 4) `scripts/run-platform.sh go -C backend test ./internal/billing ./internal/workflow` (нужны PostgreSQL и Go).
- Ожидается: `failed=0` и `skipped=0` в каждом наборе (`TESTING.md` T5); skip = провал.
- Доказательство: блок `### ACC-1B2 — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).

### ACC-1B3 — приёмка этапа SessionAnalysis.Apply и SaveEdits (02604)

- Выполняет: человек или CI с PostgreSQL и Go. Модель только готовит блок доказательства.
- Условие входа: все задачи этапа закрыты, их тесты лежат в репозитории (R33).
- Команды (по порядку, остановиться на первой ошибке): 1) `python3 scripts/generate-billing-routed-workflow.py --check`; 2) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/contracts/test_billing_routed_workflow_contract.py`; 3) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/integration/test_billing_routed_apply.py` (нужна БД: `make dev-infra`); 4) `scripts/run-platform.sh go -C backend test ./internal/billing ./internal/v2sessions` (нужны PostgreSQL и Go).
- Ожидается: `failed=0` и `skipped=0` в каждом наборе (`TESTING.md` T5); skip = провал.
- Доказательство: блок `### ACC-1B3 — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).

### ACC-1B4 — приёмка этапа V2 render (02605)

- Выполняет: человек или CI с PostgreSQL и Go. Модель только готовит блок доказательства.
- Условие входа: все задачи этапа закрыты, их тесты лежат в репозитории (R33).
- Команды (по порядку, остановиться на первой ошибке): 1) `python3 scripts/generate-billing-render.py --check`; 2) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/contracts/test_billing_render_contract.py`; 3) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/integration/test_billing_render.py tests/integration/test_billing_render_go.py` (нужна БД: `make dev-infra`); 4) `scripts/run-platform.sh go -C backend test ./internal/billing ./internal/v2render` (нужны PostgreSQL и Go).
- Ожидается: `failed=0` и `skipped=0` в каждом наборе (`TESTING.md` T5); skip = провал.
- Доказательство: блок `### ACC-1B4 — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).

### ACC-1B5 — приёмка этапа Editor effects (02606)

- Выполняет: человек или CI с PostgreSQL и Go. Модель только готовит блок доказательства.
- Условие входа: все задачи этапа закрыты, их тесты лежат в репозитории (R33).
- Команды (по порядку, остановиться на первой ошибке): 1) `python3 scripts/generate-billing-editor.py --check`; 2) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/contracts/test_billing_editor_contract.py`; 3) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/integration/test_billing_editor.py` (нужна БД: `make dev-infra`); 4) `scripts/run-platform.sh go -C backend test ./internal/billing ./internal/v2input` (нужны PostgreSQL и Go).
- Ожидается: `failed=0` и `skipped=0` в каждом наборе (`TESTING.md` T5); skip = провал.
- Доказательство: блок `### ACC-1B5 — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).
- Примечание: Фикстура: `tests/integration/billing_editor_fixture.py`. Native pixel execution и fresh public compiler не квалифицированы (`OPEN-10`), в приёмку не входят.

### ACC-1B6 — приёмка этапа Отдельные фото-операции и paid funding (02607–02608)

- Выполняет: человек или CI с PostgreSQL и Go. Модель только готовит блок доказательства.
- Условие входа: все задачи этапа закрыты, их тесты лежат в репозитории (R33).
- Команды (по порядку, остановиться на первой ошибке): 1) `python3 scripts/generate-billing-operations.py --check`; 2) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/contracts/test_billing_operations_contract.py tests/contracts/test_billing_paid_operation_contract.py`; 3) `scripts/run-platform.sh .venv-platform/bin/python -m pytest -q -rs -p no:cacheprovider tests/integration/test_billing_operations.py` (нужна БД: `make dev-infra`); 4) `scripts/run-platform.sh go -C backend test ./internal/billing` (нужны PostgreSQL и Go).
- Ожидается: `failed=0` и `skipped=0` в каждом наборе (`TESTING.md` T5); skip = провал.
- Доказательство: блок `### ACC-1B6 — <дата> — <git rev-parse --short HEAD>` в `STATUS.md` (команда, `passed=N failed=0 skipped=0`, что не покрыто). Числа разных наборов не складывать (`TESTING.md` T6).
- Примечание: Go-harness: `backend/internal/billing/operations_integration_test.go`. Скрипт проверки paid funding — по `contracts/README.md` (в `gen-briefs` указан как «не указан»).
