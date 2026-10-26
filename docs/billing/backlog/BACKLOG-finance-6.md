# Бэклог: группа finance (BILL-04), часть 6

### FIN-F11 — функция `billing_request_change`

- Статус: нет `FUNCTION photo.billing_request_change(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_quota_pin_budget`, `billing_project_quota`, `billing_quota_lock`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_finance_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_finance.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_request_change | tenant, quote_id, expected_revision,key → change | Только один unresolved immediate change; downgrade future date; upgrade payment pending | Owner; concurrent tab conflict с current revision |
