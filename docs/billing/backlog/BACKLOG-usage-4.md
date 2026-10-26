# Бэклог: группа usage (BILL-05/06/09), часть 4

### USA-F09 — функция `billing_issue_ticket`

- Статус: нет `FUNCTION photo.billing_issue_ticket(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_issue_funding_claim`, `billing_expire_quota_buckets`, `billing_quota_lock`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_issue_ticket | lease_id,fence,request_id,run_id,quantity → ticket | В existing run admission TX проверить lease expiry/fence, tenant, operation policy, remaining local allocation и current security authority; сохранить ticket+allocation+outbox; никакого полного billing catalog read |

### USA-F10 — функция `billing_open_delivery`

- Статус: нет `FUNCTION photo.billing_open_delivery(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_operation_session`, `billing_prepare_render`, `billing_bind_render`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_open_delivery | existing redemption authority+physical object proof → opaque capability | Private delivery adapter; no direct object URL; TTL/current permission повторяются |

### USA-F11 — функция `billing_project_usage`

- Статус: нет `FUNCTION photo.billing_project_usage(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_project_quota`, `billing_quota_reject_immutable`, `billing_prepare_apply`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_project_usage | tenant,resource,cursor → summary | Вычислить paid grant/leased/consumed/overrun и blocked; revision монотонна; UI updated_at; rebuild и live projection дают одинаковый итог |

### USA-F12 — функция `billing_reconcile_storage`

- Статус: нет `FUNCTION photo.billing_reconcile_storage(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_render_source_frames`, `billing_selected_source`, `billing_prepare_editor`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_reconcile_storage | tenant,cursor,verified object inventory → report | Сравнить tenant_usage/reservations/object versions; несовпадения quarantine/report, не произвольный отрицательный correction; maintenance не открывает новое entitlement |

### USA-F13 — функция `billing_reconcile_uncertain_chunk`

- Статус: нет `FUNCTION photo.billing_reconcile_uncertain_chunk(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_link_operation_child`, `billing_settle_operations`, `billing_check_operation_claim`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_reconcile_uncertain_chunk | receipt hash,verified transport evidence → confirmed/released/quarantine | Maintenance only; без proof нет возврата в available и нет billable full object charge |
