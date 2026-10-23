# Бэклог: группа usage (BILL-05/06/09), часть 1

### USA-00 — каркас этапа группы `usage`

- Выход: Создать каркас этапа по образцу quota-этапа (копировать структуру, не логику): `contracts/usage-tables.json`, `contracts/usage-functions.json`, `contracts/usage-functions.sql`, `scripts/generate-billing-usage.py` (копия `scripts/generate-billing-quota.py`), `tests/contracts/test_billing_usage_contract.py` (копия `tests/contracts/test_billing_quota_contract.py`), `tests/integration/test_billing_usage.py` (копия структуры `tests/integration/test_billing_quota.py`).
- Критерий: A. Генератор `--check` PASS на пустых источниках. B. Пустой contract-тест PASS. C. Миграция-пустышка не создаётся, пока нет таблиц.
- Запреты: R9, R28.

### USA-T01 — таблица `billing_egress_ledger`

- Статус: нет `CREATE TABLE photo.billing_egress_ledger` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/usage-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-usage.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_egress_ledger | tenant_id; period_id FK; delivery_id uuid; chunk_sequence bigint; source gallery/preview/download; bytes_sent bigint; ticket_id FK; occurred_at; recorded_at | UNIQUE(tenant,delivery,chunk_sequence); реально отправленные bytes; повторный download новый delivery; partial flush dedup |

### USA-T02 — таблица `billing_photo_runs`

- Статус: нет `CREATE TABLE photo.billing_photo_runs` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/usage-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-usage.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_photo_runs | tenant_id; photo_identity_id FK; user_request_id text; run_id uuid; result_id uuid?; attempt_kind initial/user_rerun/technical_retry; billing_policy_version int; charged_quantity bigint; adopted_at? | UNIQUE(tenant,user_request); canonical adoption/result unique; retry refers same user_request; free-slot identity фиксируется при admission, success ordinal только audit и не меняет согласованную стоимость |

### USA-T03 — таблица `billing_storage_addons`

- Статус: нет `CREATE TABLE photo.billing_storage_addons` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/usage-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-usage.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_storage_addons | tenant_id; subscription_item_id FK; offer_id FK; quantity int; capacity_bytes bigint; period_id FK; state pending/active/ending/ended; cancel_at_period_end bool; paid_allocation_id FK? | Capacity только paid; recurring capacity не consumable bucket |

### USA-T04 — таблица `billing_storage_objects`

- Статус: нет `CREATE TABLE photo.billing_storage_objects` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/usage-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-usage.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_storage_objects | tenant_id; artifact_id uuid; bucket_name text; object_key text; object_version text; logical_size_bytes bigint; category original/result/preview/archive; state pending/ready/deleting/deleted; deleted_at? | UNIQUE(tenant,bucket,key,version); PII-bearing keys удаляются/обезличиваются по policy; readiness использует существующие object proofs |

### USA-T05 — таблица `billing_storage_usage`

- Статус: нет `CREATE TABLE photo.billing_storage_usage` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/usage-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-usage.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_storage_usage | tenant_id PK; included_bytes/additional_bytes bigint; committed_bytes/reserved_bytes bigint; original_bytes/result_bytes/preview_bytes/archive_bytes bigint; reconciled_at?; revision bigint | Представление/projection tenant_usage, не параллельный счётчик admission; category sum=committed |
- Уточнения из 09:

| billing_storage_usage | VIEW либо материализованная rebuildable projection, не новая mutable quota authority. tenant_usage остаётся admission truth; no separate bytes debit in billing settlement |

### USA-T06 — таблица `billing_usage_summaries`

- Статус: нет `CREATE TABLE photo.billing_usage_summaries` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/usage-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-usage.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_usage_summaries | tenant_id; resource_code; period_id FK; granted/consumed/leased/available/overrun bigint; blocked bool; entitlement_revision bigint; projected_at; projection_cursor text | UNIQUE(tenant,resource,period); rebuildable projection, не новый ledger; available никогда не отрицателен; overrun отдельно |

### USA-T07 — таблица `billing_usage_ticket_allocations`

- Статус: нет `CREATE TABLE photo.billing_usage_ticket_allocations` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/usage-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-usage.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_usage_ticket_allocations | tenant_id; ticket_id FK; lease_allocation_id FK; quantity bigint; settled_quantity bigint; released_quantity bigint | UNIQUE(ticket,allocation); суммы=quantity после terminal; allocations не позволяют fund дважды |
