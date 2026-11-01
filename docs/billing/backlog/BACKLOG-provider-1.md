# Бэклог: группа provider (BILL-07/08), часть 1

### PRO-00 — каркас этапа группы `provider`

- Выход: Создать каркас этапа по образцу quota-этапа (копировать структуру, не логику): `contracts/provider-tables.json`, `contracts/provider-functions.json`, `contracts/provider-functions.sql`, `scripts/generate-billing-provider.py` (копия `scripts/generate-billing-quota.py`), `tests/contracts/test_billing_provider_contract.py` (копия `tests/contracts/test_billing_quota_contract.py`), `tests/integration/test_billing_provider.py` (копия структуры `tests/integration/test_billing_quota.py`).
- Тесты (R33): сами файлы `tests/contracts/test_billing_provider_contract.py` и `tests/integration/test_billing_provider.py` создаются здесь (пустой набор проходит, не skip).
- Критерий: A. Генератор `--check` PASS на пустых источниках. B. Пустой contract-тест PASS. C. Миграция-пустышка не создаётся, пока нет таблиц.
- Запреты: R9, R28.

### PRO-F02 — функция `billing_claim_outbox`

- Статус: нет `FUNCTION photo.billing_claim_outbox(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_claim_webhook`, `billing_assign_custom`, `billing_selected_outcome`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/provider-functions.sql`, запись в `contracts/provider-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_provider_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_provider.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_claim_outbox / billing_finish_outbox | batch/fence → messages; message/fence/outcome → state | SKIP LOCKED claim; retry/backoff; stale finish отказ; dead-letter evidence | Только соответствующий service worker; at-least-once recipients |

### PRO-F04 — функция `billing_finish_outbox`

- Статус: нет `FUNCTION photo.billing_finish_outbox(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_bind_editor`, `billing_bind_operations`, `billing_selected_outcome`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/provider-functions.sql`, запись в `contracts/provider-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_provider_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_provider.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_claim_outbox / billing_finish_outbox | batch/fence → messages; message/fence/outcome → state | SKIP LOCKED claim; retry/backoff; stale finish отказ; dead-letter evidence | Только соответствующий service worker; at-least-once recipients |
