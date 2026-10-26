# Бэклог: группа usage (BILL-05/06/09), часть 2

### USA-T06 — таблица `billing_usage_summaries`

- Статус: нет `CREATE TABLE photo.billing_usage_summaries` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/usage-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-usage.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_usage_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_usage.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_usage_summaries | tenant_id; resource_code; period_id FK; granted/consumed/leased/available/overrun bigint; blocked bool; entitlement_revision bigint; projected_at; projection_cursor text | UNIQUE(tenant,resource,period); rebuildable projection, не новый ledger; available никогда не отрицателен; overrun отдельно |

### USA-T07 — таблица `billing_usage_ticket_allocations`

- Статус: нет `CREATE TABLE photo.billing_usage_ticket_allocations` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/usage-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-usage.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_usage_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_usage.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_usage_ticket_allocations | tenant_id; ticket_id FK; lease_allocation_id FK; quantity bigint; settled_quantity bigint; released_quantity bigint | UNIQUE(ticket,allocation); суммы=quantity после terminal; allocations не позволяют fund дважды |

### USA-F01 — функция `billing_admit_usage_request`

- Статус: нет `FUNCTION photo.billing_admit_usage_request(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_guard_terminal_run`, `billing_settle_render`, `billing_prepare_render`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_admit_usage_request | cost_quote_id,request hash,lease allocations,root/child links → accepted receipt | API/scheduler wrapper; multi-resource/batch gate до publish; free slots и tickets в одной TX; same request replay |

### USA-F02 — функция `billing_apply_period_boundary`

- Статус: нет `FUNCTION photo.billing_apply_period_boundary(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_lock_apply_run`, `billing_settle_apply_run`, `billing_prepare_operations`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_apply_period_boundary | subscription, paid_period allocation → grants+changes | Pending downgrade/cancel; close old period; next grant; generation 0 rollover только при непрерывной оплате; add-ons independent | Unique subscription/period/line/resource; failed renewal не даёт новую квоту |

### USA-F03 — функция `billing_apply_storage_entitlement`

- Статус: нет `FUNCTION photo.billing_apply_storage_entitlement(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_guard_source_item`, `billing_settle_render`, `billing_guard_operation_item`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_apply_storage_entitlement | paid allocation+period+revision → effective capacity | Обновить commercial ceiling в единственном tenant storage policy через действующий admission contract; active reservation не потерять; category projection отдельно |
