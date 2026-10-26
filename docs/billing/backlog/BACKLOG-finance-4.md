# Бэклог: группа finance (BILL-04), часть 4

### FIN-F01 — функция `billing_accept_quote`

- Статус: нет `FUNCTION photo.billing_accept_quote(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_project_quota`, `billing_append_quota_event`, `billing_expire_quota_buckets`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_finance_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_finance.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_accept_quote | quote_id, actor, key, hash → order+change | Revalidate role, lifecycle, revision, expiry и price mapping; freeze order lines; поставить Checkout create outbox | Duplicate key возвращает order; смена цены требует нового quote |

### FIN-F02 — функция `billing_apply_paid_upgrade`

- Статус: нет `FUNCTION photo.billing_apply_paid_upgrade(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_quota_pin_budget`, `billing_allocate_paid_operation_lease`, `billing_lock_apply_run`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_finance_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_finance.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_apply_paid_upgrade | change_id, verified invoice allocation → applied/applying | Проверить source/target и renewal race; atomic local delta quota+capacity+revision после подтверждения provider price update по saga | Exact replay no new quota; Stripe update не вызывается из SQL |

### FIN-F03 — функция `billing_apply_refund`

- Статус: нет `FUNCTION photo.billing_apply_refund(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_lock_apply_run`, `billing_settle_apply_run`, `billing_prepare_render`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_finance_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_finance.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_apply_refund | refund_id, verified refund state → reversal refs | Отозвать unissued остатки пропорционально строкам refund; issued funding пометить для correction; policy consumed handling | Только provider finance worker; partial repeat не отзывает дважды |

### FIN-F04 — функция `billing_assign_custom`

- Статус: нет `FUNCTION photo.billing_assign_custom(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_bind_editor`, `billing_quota_pin_budget`, `billing_close_admission_lease`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_finance_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_finance.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_assign_custom | tenant, complete assignment+limits,actor,reason,key → assignment | Полнота, dates, amount, manual/Stripe, no overlap; публичность false; atomic entitlement revision | Privileged audited operator; не owner public API |

### FIN-F05 — функция `billing_attach_checkout`

- Статус: нет `FUNCTION photo.billing_attach_checkout(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_quota_lock`, `billing_check_operation_claim`, `billing_append_quota_event`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_finance_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_finance.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_attach_checkout | order_id, provider session snapshot, operation_key → order | Проверить account/customer/mode/currency/order ownership и ещё не привязанный другой session | Provider worker; unique session; no grant |
