# Бэклог: группа usage (BILL-05/06/09), часть 1

### USA-00 — каркас этапа группы `usage`

- Выход: Создать каркас этапа по образцу quota-этапа (копировать структуру, не логику): `contracts/usage-tables.json`, `contracts/usage-functions.json`, `contracts/usage-functions.sql`, `scripts/generate-billing-usage.py` (копия `scripts/generate-billing-quota.py`), `tests/contracts/test_billing_usage_contract.py` (копия `tests/contracts/test_billing_quota_contract.py`), `tests/integration/test_billing_usage.py` (копия структуры `tests/integration/test_billing_quota.py`).
- Тесты (R33): сами файлы `tests/contracts/test_billing_usage_contract.py` и `tests/integration/test_billing_usage.py` создаются здесь (пустой набор проходит, не skip).
- Критерий: A. Генератор `--check` PASS на пустых источниках. B. Пустой contract-тест PASS. C. Миграция-пустышка не создаётся, пока нет таблиц.
- Запреты: R9, R28.
