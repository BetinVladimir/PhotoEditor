# Бэклог: группа finance (BILL-04), часть 3

### FIN-T14 — таблица `billing_subscriptions`

- Статус: нет `CREATE TABLE photo.billing_subscriptions` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/finance-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-finance.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
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
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| tenant_billing_profiles | tenant_id PK/FK; current_plan_version_id FK; current_price_id FK?; billing_mode beta/trial/stripe/manual/deleting; financial_subject_id uuid; billing_contact_id uuid?; entitlement_revision bigint; transition_at timestamptz? | Price относится к версии; нет Stripe customer ID без account namespace; beta переход только явный |
- Уточнения из 09:

| tenant_billing_profiles | financial_subject_id uuid UNIQUE FK; active_provider_account_id uuid? FK; lifecycle active/deleting/closed; lifecycle_epoch bigint>0; beta_policy_id uuid? FK; transition_at timestamptz?; current_price_id nullable только Trial/manual. UNIQUE(tenant,subject). Новый subject не создаётся при каждом входе |

### FIN-F01 — функция `billing_accept_quote`

- Статус: нет `FUNCTION photo.billing_accept_quote(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_project_quota`, `billing_append_quota_event`, `billing_expire_quota_buckets`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_accept_quote | quote_id, actor, key, hash → order+change | Revalidate role, lifecycle, revision, expiry и price mapping; freeze order lines; поставить Checkout create outbox | Duplicate key возвращает order; смена цены требует нового quote |

### FIN-F02 — функция `billing_apply_paid_upgrade`

- Статус: нет `FUNCTION photo.billing_apply_paid_upgrade(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_quota_pin_budget`, `billing_allocate_paid_operation_lease`, `billing_lock_apply_run`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_apply_paid_upgrade | change_id, verified invoice allocation → applied/applying | Проверить source/target и renewal race; atomic local delta quota+capacity+revision после подтверждения provider price update по saga | Exact replay no new quota; Stripe update не вызывается из SQL |

### FIN-F03 — функция `billing_apply_refund`

- Статус: нет `FUNCTION photo.billing_apply_refund(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_lock_apply_run`, `billing_settle_apply_run`, `billing_prepare_render`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_apply_refund | refund_id, verified refund state → reversal refs | Отозвать unissued остатки пропорционально строкам refund; issued funding пометить для correction; policy consumed handling | Только provider finance worker; partial repeat не отзывает дважды |

### FIN-F04 — функция `billing_assign_custom`

- Статус: нет `FUNCTION photo.billing_assign_custom(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_bind_editor`, `billing_quota_pin_budget`, `billing_close_admission_lease`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_assign_custom | tenant, complete assignment+limits,actor,reason,key → assignment | Полнота, dates, amount, manual/Stripe, no overlap; публичность false; atomic entitlement revision | Privileged audited operator; не owner public API |

### FIN-F05 — функция `billing_attach_checkout`

- Статус: нет `FUNCTION photo.billing_attach_checkout(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_quota_lock`, `billing_check_operation_claim`, `billing_append_quota_event`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_attach_checkout | order_id, provider session snapshot, operation_key → order | Проверить account/customer/mode/currency/order ownership и ещё не привязанный другой session | Provider worker; unique session; no grant |
