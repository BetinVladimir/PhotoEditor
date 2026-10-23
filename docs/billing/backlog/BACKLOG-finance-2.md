# Бэклог: группа finance (BILL-04), часть 2

### FIN-T07 — таблица `billing_invoices`

- Статус: нет `CREATE TABLE photo.billing_invoices` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_invoices | financial_subject_id uuid; tenant_id uuid?; provider_account_id FK; provider_invoice_id text; order_id uuid?; currency; net_minor/tax_minor/gross_minor bigint; state draft/open/paid/void/uncollectible; paid_at?; retention_policy_id uuid; hosted_url text? | UNIQUE(account,invoice); restricted retained scope после tenant erasure; URL сам является sensitive capability |

### FIN-T08 — таблица `billing_order_lines`

- Статус: нет `CREATE TABLE photo.billing_order_lines` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_order_lines | tenant_id; order_id FK; ordinal int; price_id FK?; offer_id FK?; resource_code FK; pack_count int; unit_net_minor bigint; net_minor/tax_minor/gross_minor bigint; resource_quantity bigint; snapshot jsonb; grant_starts_at?/expires_at? | UNIQUE(order,ordinal); pack_count>0; typed references; серверные snapshots; foreign offer текущего плана запрещён |
- Уточнения из 09:

| billing_order_lines | money_snapshot net/tax/gross/currency как typed columns; expiry_policy fixed_ttl/period_end/never/none; ttl_seconds bigint?; included_egress_bytes bigint; operation_policy_id uuid?; provider_price_binding_id uuid?; product label snapshot без PII |

### FIN-T09 — таблица `billing_orders`

- Статус: нет `CREATE TABLE photo.billing_orders` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_orders | tenant_id; quote_id FK; kind subscription/topup/operation_topup/upgrade/storage/egress; state created/checkout_pending/payment_pending/paid/fulfilling/fulfilled/failed/canceled/refunded/partially_refunded; net_minor/tax_minor/gross_minor bigint; currency; idempotency_key text; request_hash bytea; environment account FK; provider_checkout_id text?; paid_at? | UNIQUE(tenant,idempotency_key); unique account/session; refund не удаляет fulfilled history |

### FIN-T10 — таблица `billing_payments`

- Статус: нет `CREATE TABLE photo.billing_payments` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_payments | financial_subject_id; tenant_id?; provider_account_id FK; provider_payment_id text; invoice_id FK?; order_id FK?; amount_minor/refunded_minor bigint; currency; state pending/succeeded/failed/refunded/disputed; settled_at? | UNIQUE(account,payment); refunded<=amount; несколько платежей могут погашать invoice, не начислять grant на каждый повтор |

### FIN-T11 — таблица `billing_quotes`

- Статус: нет `CREATE TABLE photo.billing_quotes` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_quotes | tenant_id; actor_id uuid?; kind text; plan_revision bigint; resource_snapshot jsonb; resource_snapshot_hash bytea; net_minor/tax_minor/gross_minor bigint; currency; tax_policy_version text; expires_at; accepted_at?; request_hash bytea | Immutable; default TTL=15 минут; membership, price и policy revalidation перед order |

### FIN-T12 — таблица `billing_refunds`

- Статус: нет `CREATE TABLE photo.billing_refunds` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_refunds | financial_subject_id; payment_id FK; external_refund_id text; amount_minor bigint; state pending/succeeded/failed; resource_reversal_key text?; reason_code text | UNIQUE(account,refund); partial resource reversal по immutable line mapping; consumed usage не исчезает |

### FIN-T13 — таблица `billing_subscription_items`

- Статус: нет `CREATE TABLE photo.billing_subscription_items` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_subscription_items | tenant_id; subscription_id FK; kind ai/storage; price_id FK?; offer_id FK?; external_item_id text; quantity int; starts_at/end_at timestamptz; state active/pending/canceled | XOR local target; ai quantity=1 и максимум один active AI item; storage quantity>0; unique account/item |
- Уточнения из 09:

| billing_subscription_items | provider_account_id FK; external_item_id text; UNIQUE(account,external_item_id); item starts_at/ends_at timestamptz; entitlement_status pending/active/ending/ended отдельно от provider presence |
