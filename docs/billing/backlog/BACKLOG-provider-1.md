# Бэклог: группа provider (BILL-07/08), часть 1

### PRO-00 — каркас этапа группы `provider`

- Выход: Создать каркас этапа по образцу quota-этапа (копировать структуру, не логику): `contracts/provider-tables.json`, `contracts/provider-functions.json`, `contracts/provider-functions.sql`, `scripts/generate-billing-provider.py` (копия `scripts/generate-billing-quota.py`), `tests/contracts/test_billing_provider_contract.py` (копия `tests/contracts/test_billing_quota_contract.py`), `tests/integration/test_billing_provider.py` (копия структуры `tests/integration/test_billing_quota.py`).
- Тесты (R33): сами файлы `tests/contracts/test_billing_provider_contract.py` и `tests/integration/test_billing_provider.py` создаются здесь (пустой набор проходит, не skip).
- Критерий: A. Генератор `--check` PASS на пустых источниках. B. Пустой contract-тест PASS. C. Миграция-пустышка не создаётся, пока нет таблиц.
- Запреты: R9, R28.
