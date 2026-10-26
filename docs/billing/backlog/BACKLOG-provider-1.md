# Бэклог: группа provider (BILL-07/08), часть 1

### PRO-00 — каркас этапа группы `provider`

- Выход: Создать каркас этапа по образцу quota-этапа (копировать структуру, не логику): `contracts/provider-tables.json`, `contracts/provider-functions.json`, `contracts/provider-functions.sql`, `scripts/generate-billing-provider.py` (копия `scripts/generate-billing-quota.py`), `tests/contracts/test_billing_provider_contract.py` (копия `tests/contracts/test_billing_quota_contract.py`), `tests/integration/test_billing_provider.py` (копия структуры `tests/integration/test_billing_quota.py`).
- Тесты (R33): сами файлы `tests/contracts/test_billing_provider_contract.py` и `tests/integration/test_billing_provider.py` создаются здесь (пустой набор проходит, не skip).
- Критерий: A. Генератор `--check` PASS на пустых источниках. B. Пустой contract-тест PASS. C. Миграция-пустышка не создаётся, пока нет таблиц.
- Запреты: R9, R28.

### PRO-T01 — таблица `billing_notification_marks`

- Статус: нет `CREATE TABLE photo.billing_notification_marks` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/provider-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-provider.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_provider_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_provider.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_notification_marks | tenant_id; resource_code; period_id; threshold_percent int; channel in_app/email; delivered_at?; business_key text | UNIQUE(tenant,resource,period,threshold,channel); thresholds 80/95/100; repeat после нового периода |

### PRO-T02 — таблица `billing_outbox`

- Статус: нет `CREATE TABLE photo.billing_outbox` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/provider-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-provider.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_provider_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_provider.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_outbox | tenant_id?; topic text; business_key text; payload jsonb; state pending/processing/sent/retry/dead; attempts int; next_retry_at?; processing_token uuid?; lease_until? | UNIQUE(topic,business_key); SKIP LOCKED claims; тема erasure не содержит PII; delivery at-least-once |

### PRO-T03 — таблица `billing_reconciliation_runs`

- Статус: нет `CREATE TABLE photo.billing_reconciliation_runs` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/provider-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-provider.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_provider_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_provider.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_reconciliation_runs | provider_account_id FK; scope payments/subscriptions/grants; cursor text?; window_start/end; state running/completed/failed; mismatch_count int; error_code text? | Пагинация, overlap window и business dedup; index(account,created_at) |

### PRO-T04 — таблица `billing_webhook_inbox`

- Статус: нет `CREATE TABLE photo.billing_webhook_inbox` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/provider-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-provider.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_provider_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_provider.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| billing_webhook_inbox | provider_account_id FK; event_id text; event_type text; object_id text; provider_created_at; payload_encrypted bytea?; payload_hash bytea; state received/processing/applied/ignored/retry/dead; attempts int; next_retry_at?; processing_token uuid?; lease_until?; last_error_code text? | UNIQUE(account,event_id); active retry index(state,next_retry_at); payload TTL; минимум PII, не вечный raw archive |

### PRO-F01 — функция `billing_apply_provider_snapshot`

- Статус: нет `FUNCTION photo.billing_apply_provider_snapshot(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_lock_apply_run`, `billing_settle_apply_run`, `billing_project_quota`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/provider-functions.sql`, запись в `contracts/provider-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_provider_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_provider.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_apply_provider_snapshot | inbox_id,fence,verified object snapshot → applied/retry/ignored | Проверить account/customer/tenant/order и authoritative payment state; monotonic business state; обновить invoice/payment/subscription; outbox fulfillment | Один event может описывать уже применённую оплату: grant не дублируется |
