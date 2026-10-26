# Бэклог: группа usage (BILL-05/06/09), часть 5

### USA-F14 — функция `billing_record_egress_chunk`

- Статус: нет `FUNCTION photo.billing_record_egress_chunk(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_guard_terminal_run`, `billing_operation_session`, `billing_prepare_render`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_record_egress_chunk | delivery_id,attempt_id,sequence,ticket,written_bytes,receipt_hash → consumed/uncertain | Проверить bounded allocated bytes, count body only; unique delivery/sequence; partial chunk terminal flush; failure повторяется через durable receipt |

### USA-F15 — функция `billing_register_photo_success`

- Статус: нет `FUNCTION photo.billing_register_photo_success(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_render_source_frames`, `billing_expire_quota_buckets`, `billing_operation_session`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_register_photo_success | tenant,photo_identity,charge_item,result,policy → success audit | Проверить admission free-slot/paid decision; first_success открывает окно и два free slots; не менять согласованную цену по порядку завершения; technical retries не новые user requests; внутри settlement TX |

### USA-F16 — функция `billing_release_ticket`

- Статус: нет `FUNCTION photo.billing_release_ticket(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_prepare_editor`, `billing_expire_quota_buckets`, `billing_prepare_render`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_release_ticket | ticket,terminal failure/cancel proof → release | Не отменять adopted result; вернуть funding в исходный bucket с исходным expiry; если expiry прошёл, сразу expire. Восстановленный technical retry сохраняет исходный ticket, не делает новый |

### USA-F17 — функция `billing_reserve_delivery_chunk`

- Статус: нет `FUNCTION photo.billing_reserve_delivery_chunk(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_settle_apply_run`, `billing_prepare_render`, `billing_prepare_editor`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_reserve_delivery_chunk | delivery,attempt,sequence,requested bytes → ticket/finite allocation | Delivery maintenance-scoped adapter; allowance cumulative; no chunk without current delivery permission |

### USA-F18 — функция `billing_rollover_bucket`

- Статус: нет `FUNCTION photo.billing_rollover_bucket(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_expire_quota_buckets`, `billing_quota_lock`, `billing_quota_pin_budget`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_rollover_bucket | origin_bucket,next_paid_period → next bucket | Только generation0 remainder, один раз; outstanding tickets исключены из transferred quantity; late release может добавить transfer только до next period end; generation1 не переносить; no continuous payment=no rollover |
