# Бэклог: группа legal (BILL-11/12), часть 3

### LEG-F08 — функция `account_update_privacy_preferences`

- Статус: нет `FUNCTION photo.account_update_privacy_preferences(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): нет.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/legal-functions.sql`, запись в `contracts/legal-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_legal_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_legal.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| account_update_privacy_preferences | user,expected_revision,analytics/marketing,key → preferences | Event на каждое изменение; withdraw без потери основной услуги; новые consent-dependent SDK запрещены после commit |
