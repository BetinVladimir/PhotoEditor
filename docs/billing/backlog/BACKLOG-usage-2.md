# Бэклог: группа usage (BILL-05/06/09), часть 2

### USA-F01 — функция `billing_admit_usage_request`

- Статус: нет `FUNCTION photo.billing_admit_usage_request(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_guard_terminal_run`, `billing_settle_render`, `billing_prepare_render`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_admit_usage_request | cost_quote_id,request hash,lease allocations,root/child links → accepted receipt | API/scheduler wrapper; multi-resource/batch gate до publish; free slots и tickets в одной TX; same request replay |

### USA-F02 — функция `billing_apply_period_boundary`

- Статус: нет `FUNCTION photo.billing_apply_period_boundary(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_lock_apply_run`, `billing_settle_apply_run`, `billing_prepare_operations`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_apply_period_boundary | subscription, paid_period allocation → grants+changes | Pending downgrade/cancel; close old period; next grant; generation 0 rollover только при непрерывной оплате; add-ons independent | Unique subscription/period/line/resource; failed renewal не даёт новую квоту |

### USA-F03 — функция `billing_apply_storage_entitlement`

- Статус: нет `FUNCTION photo.billing_apply_storage_entitlement(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_guard_source_item`, `billing_settle_render`, `billing_guard_operation_item`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_apply_storage_entitlement | paid allocation+period+revision → effective capacity | Обновить commercial ceiling в единственном tenant storage policy через действующий admission contract; active reservation не потерять; category projection отдельно |

### USA-F04 — функция `billing_close_lease`

- Статус: нет `FUNCTION photo.billing_close_lease(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_close_admission_lease`, `billing_allocate_admission_lease`, `billing_settle_render`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_close_lease | lease,fence,recovery proof → returned+pending | Истёкший lease не разрешает новый issue; вернуть только невыданный quantity; issued stays attached until terminal proof; stale instance refused |

### USA-F05 — функция `billing_cover_overrun`

- Статус: нет `FUNCTION photo.billing_cover_overrun(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_settle_operations`, `billing_lock_apply_run`, `billing_check_operation_claim`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_cover_overrun | tenant,resource,new_grant_ref → covered quantity | Погасить соответствующий resource overrun до открытия новых paid admissions; ledger обеих сторон, no negative bucket; не выставлять деньги |

### USA-F06 — функция `billing_expire_buckets`

- Статус: нет `FUNCTION photo.billing_expire_buckets(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_expire_quota_buckets`, `billing_quota_pin_budget`, `billing_prepare_editor`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_expire_buckets | tenant/resource,server time,cursor,batch → cursor/count | Только remaining истёкших buckets; issued commitments не уничтожать; без двойного expire; не full table scan в request path |

### USA-F07 — функция `billing_grant_paid_allocation`

- Статус: нет `FUNCTION photo.billing_grant_paid_allocation(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_prepare_operations`, `billing_bind_operations`, `billing_guard_operation_item`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_grant_paid_allocation | invoice_allocation_id, payment proof → grant refs | Проверить фактическую оплату, line/resource/period, no pending erasure; создать bucket или capacity; cover overrun; изменить entitlement revision и outbox | Unique business grant key, не event_id; только fulfillment worker |
