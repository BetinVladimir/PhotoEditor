# Бэклог: группа usage (BILL-05/06/09), часть 6

### USA-F19 — функция `billing_settle_ticket`

- Статус: нет `FUNCTION photo.billing_settle_ticket(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_settle_render`, `billing_settle_operations`, `billing_settle_next_render`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_settle_ticket | ticket,canonical_result,terminal_proof,quantity → settlement | В adoption TX удостовериться в доступном accepted result; terminal CAS; перевести leased→consumed ровно один раз; allocation allowance в overrun; partial batch по каждому фото/operation |

### USA-F20 — функция `billing_transfer_rollover_remainder`

- Статус: нет `FUNCTION photo.billing_transfer_rollover_remainder(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_settle_render`, `billing_prepare_render`, `billing_settle_next_render`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/usage-functions.sql`, запись в `contracts/usage-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_usage_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_usage.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_transfer_rollover_remainder | origin bucket,next paid period,unique terminal reference → transferred quantity | Maintenance/settlement; только nonleased свободный quantity; late failure добавляет к тому же child bucket один раз |
