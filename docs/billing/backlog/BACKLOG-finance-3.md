# Бэклог: группа finance (BILL-04), часть 3

### FIN-T11 — таблица `billing_quotes`

- Статус: нет `CREATE TABLE photo.billing_quotes` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_finance_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_finance.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_quotes | tenant_id; actor_id uuid?; kind text; plan_revision bigint; resource_snapshot jsonb; resource_snapshot_hash bytea; net_minor/tax_minor/gross_minor bigint; currency; tax_policy_version text; expires_at; accepted_at?; request_hash bytea | Immutable; default TTL=15 минут; membership, price и policy revalidation перед order |

### FIN-T12 — таблица `billing_refunds`

- Статус: нет `CREATE TABLE photo.billing_refunds` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_finance_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_finance.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_refunds | financial_subject_id; payment_id FK; external_refund_id text; amount_minor bigint; state pending/succeeded/failed; resource_reversal_key text?; reason_code text | UNIQUE(account,refund); partial resource reversal по immutable line mapping; consumed usage не исчезает |

### FIN-T13 — таблица `billing_subscription_items`

- Статус: нет `CREATE TABLE photo.billing_subscription_items` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_finance_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_finance.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_subscription_items | tenant_id; subscription_id FK; kind ai/storage; price_id FK?; offer_id FK?; external_item_id text; quantity int; starts_at/end_at timestamptz; state active/pending/canceled | XOR local target; ai quantity=1 и максимум один active AI item; storage quantity>0; unique account/item |
- Уточнения из 09:

| billing_subscription_items | provider_account_id FK; external_item_id text; UNIQUE(account,external_item_id); item starts_at/ends_at timestamptz; entitlement_status pending/active/ending/ended отдельно от provider presence |

### FIN-T14 — таблица `billing_subscriptions`

- Статус: нет `CREATE TABLE photo.billing_subscriptions` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_finance_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_finance.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_subscriptions | tenant_id FK; provider_account_id FK; provider_subscription_id text; state incomplete/trialing/active/past_due/unpaid/paused/canceled; period_id FK?; cancel_at_period_end bool; pending_change_id uuid?; provider_snapshot_hash bytea; synced_at timestamptz | UNIQUE(account,provider_subscription_id); максимум одна незакрытая subscription на tenant/account; incomplete не даёт paid grant |
- Уточнения из 09:

| billing_subscriptions | subject_id FK; provider_account_id FK; starts_at/ends_at живут в billing_periods; nullable period_id для incomplete; provider_subscription_id text UNIQUE(provider_account_id,provider_subscription_id). Local entitlement state отдельно от provider state |

### FIN-T15 — таблица `tenant_billing_profiles`

- Статус: нет `CREATE TABLE photo.tenant_billing_profiles` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_finance_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_finance.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| tenant_billing_profiles | tenant_id PK/FK; current_plan_version_id FK; current_price_id FK?; billing_mode beta/trial/stripe/manual/deleting; financial_subject_id uuid; billing_contact_id uuid?; entitlement_revision bigint; transition_at timestamptz? | Price относится к версии; нет Stripe customer ID без account namespace; beta переход только явный |
- Уточнения из 09:

| tenant_billing_profiles | financial_subject_id uuid UNIQUE FK; active_provider_account_id uuid? FK; lifecycle active/deleting/closed; lifecycle_epoch bigint>0; beta_policy_id uuid? FK; transition_at timestamptz?; current_price_id nullable только Trial/manual. UNIQUE(tenant,subject). Новый subject не создаётся при каждом входе |
