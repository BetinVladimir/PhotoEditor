# Бэклог: группа usage (BILL-05/06/09), часть 4

### USA-F15 — функция `billing_register_photo_success`

- Статус: нет `FUNCTION photo.billing_register_photo_success(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_render_source_frames`, `billing_expire_quota_buckets`, `billing_operation_session`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_register_photo_success | tenant,photo_identity,charge_item,result,policy → success audit | Проверить admission free-slot/paid decision; first_success открывает окно и два free slots; не менять согласованную цену по порядку завершения; technical retries не новые user requests; внутри settlement TX |

### USA-F16 — функция `billing_release_ticket`

- Статус: нет `FUNCTION photo.billing_release_ticket(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_prepare_editor`, `billing_expire_quota_buckets`, `billing_prepare_render`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_release_ticket | ticket,terminal failure/cancel proof → release | Не отменять adopted result; вернуть funding в исходный bucket с исходным expiry; если expiry прошёл, сразу expire. Восстановленный technical retry сохраняет исходный ticket, не делает новый |

### USA-F17 — функция `billing_reserve_delivery_chunk`

- Статус: нет `FUNCTION photo.billing_reserve_delivery_chunk(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_settle_apply_run`, `billing_prepare_render`, `billing_prepare_editor`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_reserve_delivery_chunk | delivery,attempt,sequence,requested bytes → ticket/finite allocation | Delivery maintenance-scoped adapter; allowance cumulative; no chunk without current delivery permission |

### USA-F18 — функция `billing_rollover_bucket`

- Статус: нет `FUNCTION photo.billing_rollover_bucket(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_expire_quota_buckets`, `billing_quota_lock`, `billing_quota_pin_budget`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_rollover_bucket | origin_bucket,next_paid_period → next bucket | Только generation0 remainder, один раз; outstanding tickets исключены из transferred quantity; late release может добавить transfer только до next period end; generation1 не переносить; no continuous payment=no rollover |

### USA-F19 — функция `billing_settle_ticket`

- Статус: нет `FUNCTION photo.billing_settle_ticket(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_settle_render`, `billing_settle_operations`, `billing_settle_next_render`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_settle_ticket | ticket,canonical_result,terminal_proof,quantity → settlement | В adoption TX удостовериться в доступном accepted result; terminal CAS; перевести leased→consumed ровно один раз; allocation allowance в overrun; partial batch по каждому фото/operation |

### USA-F20 — функция `billing_transfer_rollover_remainder`

- Статус: нет `FUNCTION photo.billing_transfer_rollover_remainder(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_settle_render`, `billing_prepare_render`, `billing_settle_next_render`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_transfer_rollover_remainder | origin bucket,next paid period,unique terminal reference → transferred quantity | Maintenance/settlement; только nonleased свободный quantity; late failure добавляет к тому же child bucket один раз |
