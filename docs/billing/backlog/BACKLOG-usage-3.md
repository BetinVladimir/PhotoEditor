# Бэклог: группа usage (BILL-05/06/09), часть 3

### USA-F04 — функция `billing_close_lease`

- Статус: нет `FUNCTION photo.billing_close_lease(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_close_admission_lease`, `billing_allocate_admission_lease`, `billing_settle_render`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_close_lease | lease,fence,recovery proof → returned+pending | Истёкший lease не разрешает новый issue; вернуть только невыданный quantity; issued stays attached until terminal proof; stale instance refused |

### USA-F05 — функция `billing_cover_overrun`

- Статус: нет `FUNCTION photo.billing_cover_overrun(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_settle_operations`, `billing_lock_apply_run`, `billing_check_operation_claim`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_cover_overrun | tenant,resource,new_grant_ref → covered quantity | Погасить соответствующий resource overrun до открытия новых paid admissions; ledger обеих сторон, no negative bucket; не выставлять деньги |

### USA-F06 — функция `billing_expire_buckets`

- Статус: нет `FUNCTION photo.billing_expire_buckets(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_expire_quota_buckets`, `billing_quota_pin_budget`, `billing_prepare_editor`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_expire_buckets | tenant/resource,server time,cursor,batch → cursor/count | Только remaining истёкших buckets; issued commitments не уничтожать; без двойного expire; не full table scan в request path |

### USA-F07 — функция `billing_grant_paid_allocation`

- Статус: нет `FUNCTION photo.billing_grant_paid_allocation(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_prepare_operations`, `billing_bind_operations`, `billing_guard_operation_item`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_grant_paid_allocation | invoice_allocation_id, payment proof → grant refs | Проверить фактическую оплату, line/resource/period, no pending erasure; создать bucket или capacity; cover overrun; изменить entitlement revision и outbox | Unique business grant key, не event_id; только fulfillment worker |

### USA-F08 — функция `billing_hold_photo_slot`

- Статус: нет `FUNCTION photo.billing_hold_photo_slot(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_quota_lock`, `billing_operation_session`, `billing_bind_operations`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_hold_photo_slot | tenant/photo,charge_item,server time → free slot либо paid decision | Внутри admission; current first-result/window check; initial in flight409; технический retry тот же item |
