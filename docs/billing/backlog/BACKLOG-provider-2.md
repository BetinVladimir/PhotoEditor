# Бэклог: группа provider (BILL-07/08), часть 2

### PRO-F02 — функция `billing_claim_webhook`

- Статус: нет `FUNCTION photo.billing_claim_webhook(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_grant_bootstrap`, `billing_quota_lock`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/provider-functions.sql`, запись в `contracts/provider-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_provider_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_provider.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_claim_webhook | worker_id, bounded batch → event+fence | SKIP LOCKED, ограниченный lease; attempts++; expired claim можно переполучить | Maintenance worker; stale fence не пишет результат |

### PRO-F03 — функция `billing_record_webhook`

- Статус: нет `FUNCTION photo.billing_record_webhook(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_selected_outcome`, `billing_project_quota`, `billing_quota_lock`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/provider-functions.sql`, запись в `contracts/provider-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_provider_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_provider.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_record_webhook | verified account+event envelope+hash → inbox_id/duplicate | Сохранить durable inbox; одинаковый event с другим hash quarantine/security alert | Только webhook ingress после signature verification; duplicate 2xx |
