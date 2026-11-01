# Бэклог: группа finance (BILL-04), часть 1

### FIN-00 — каркас этапа группы `finance`

- Выход: Создать каркас этапа по образцу quota-этапа (копировать структуру, не логику): `contracts/finance-tables.json`, `contracts/finance-functions.json`, `contracts/finance-functions.sql`, `scripts/generate-billing-finance.py` (копия `scripts/generate-billing-quota.py`), `tests/contracts/test_billing_finance_contract.py` (копия `tests/contracts/test_billing_quota_contract.py`), `tests/integration/test_billing_finance.py` (копия структуры `tests/integration/test_billing_quota.py`).
- Тесты (R33): сами файлы `tests/contracts/test_billing_finance_contract.py` и `tests/integration/test_billing_finance.py` создаются здесь (пустой набор проходит, не skip).
- Критерий: A. Генератор `--check` PASS на пустых источниках. B. Пустой contract-тест PASS. C. Миграция-пустышка не создаётся, пока нет таблиц.
- Запреты: R9, R28.
