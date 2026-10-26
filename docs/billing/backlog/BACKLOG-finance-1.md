# Бэклог: группа finance (BILL-04), часть 1

### FIN-00 — каркас этапа группы `finance`

- Выход: Создать каркас этапа по образцу quota-этапа (копировать структуру, не логику): `contracts/finance-tables.json`, `contracts/finance-functions.json`, `contracts/finance-functions.sql`, `scripts/generate-billing-finance.py` (копия `scripts/generate-billing-quota.py`), `tests/contracts/test_billing_finance_contract.py` (копия `tests/contracts/test_billing_quota_contract.py`), `tests/integration/test_billing_finance.py` (копия структуры `tests/integration/test_billing_quota.py`).
- Тесты (R33): сами файлы `tests/contracts/test_billing_finance_contract.py` и `tests/integration/test_billing_finance.py` создаются здесь (пустой набор проходит, не skip).
- Критерий: A. Генератор `--check` PASS на пустых источниках. B. Пустой contract-тест PASS. C. Миграция-пустышка не создаётся, пока нет таблиц.
- Запреты: R9, R28.

### FIN-T01 — таблица `billing_change_requests`

- Статус: нет `CREATE TABLE photo.billing_change_requests` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_finance_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_finance.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_change_requests | tenant_id; subscription_id FK?; type upgrade/downgrade/cancel_ai/storage_add/storage_reduce; source_version_id FK?; target_version_id FK?; quote_id FK; effective_at; idempotency_key text; request_hash bytea; state quoted/payment_pending/paid/applying/applied/compensating/failed/canceled; order_id FK?; last_error_code text? | UNIQUE(tenant,idempotency_key); один unresolved immediate change на subscription; одинаковый key+другой hash=conflict |

### FIN-T02 — таблица `billing_contacts`

- Статус: нет `CREATE TABLE photo.billing_contacts` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_finance_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_finance.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_contacts | financial_subject_id uuid; email text?; legal_name text?; company_name text?; tax_id text?; country char(2)?; postal_address jsonb?; encryption_key_ref text; erased_at timestamptz? | PII отделён от ledger; адрес имеет закрытую schema; минимум для invoicing по налоговой конфигурации; restricted reads |
- Уточнения из 09:

| billing_contacts | financial_subject_id FK; status active/retained/erased. PII logical columns из 03 хранятся за encrypted envelope или column encryption, metadata/key refs отдельно; общий encryption_key_ref не является шифрованием сам по себе |

### FIN-T03 — таблица `billing_custom_assignments`

- Статус: нет `CREATE TABLE photo.billing_custom_assignments` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_finance_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_finance.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_custom_assignments | tenant_id; custom_plan_version_id FK; amount_net_minor; currency; billing_mode stripe/manual; starts_at; ends_at; state draft/active/ended; reason text; assigned_by uuid?; approved_at timestamptz? | Один active assignment, полный набор custom_limits; reason без лишней PII; publish только privileged process |

### FIN-T04 — таблица `billing_custom_limits`

- Статус: нет `CREATE TABLE photo.billing_custom_limits` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_finance_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_finance.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_custom_limits | assignment_id FK; resource_code FK; integer_value bigint?; enum_value text?; grant_period once/month/none; rollover_periods int | UNIQUE(assignment,resource); typed XOR; обязательные ресурсы полностью заданы |

### FIN-T05 — таблица `billing_customers`

- Статус: нет `CREATE TABLE photo.billing_customers` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_finance_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_finance.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_customers | tenant_id FK; provider_account_id FK; external_customer_id text; financial_subject_id uuid; state active/deleting/deleted | UNIQUE(account,external_customer_id); UNIQUE(tenant,account); metadata без email/фото |
