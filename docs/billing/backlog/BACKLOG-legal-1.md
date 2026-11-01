# Бэклог: группа legal (BILL-11/12), часть 1

### LEG-00 — каркас этапа группы `legal`

- Выход: Создать каркас этапа по образцу quota-этапа (копировать структуру, не логику): `contracts/legal-tables.json`, `contracts/legal-functions.json`, `contracts/legal-functions.sql`, `scripts/generate-billing-legal.py` (копия `scripts/generate-billing-quota.py`), `tests/contracts/test_billing_legal_contract.py` (копия `tests/contracts/test_billing_quota_contract.py`), `tests/integration/test_billing_legal.py` (копия структуры `tests/integration/test_billing_quota.py`).
- Тесты (R33): сами файлы `tests/contracts/test_billing_legal_contract.py` и `tests/integration/test_billing_legal.py` создаются здесь (пустой набор проходит, не skip).
- Критерий: A. Генератор `--check` PASS на пустых источниках. B. Пустой contract-тест PASS. C. Миграция-пустышка не создаётся, пока нет таблиц.
- Запреты: R9, R28.

### LEG-F02 — функция `account_claim_deletion_step`

- Статус: нет `FUNCTION photo.account_claim_deletion_step(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): нет.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/legal-functions.sql`, запись в `contracts/legal-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_legal_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_legal.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| account_claim_deletion_step | bounded batch → step+lease+fence | Retry после crash, сохранённая saga; secrets в status API не возвращать |

### LEG-F03 — функция `account_finalize_deletion`

- Статус: нет `FUNCTION photo.account_finalize_deletion(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `account_accept_legal`, `claim_collection_deletion`, `account_issue_recent_auth`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/legal-functions.sql`, запись в `contracts/legal-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_legal_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_legal.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| account_finalize_deletion | request,current proof set → completed/partially_retained | Проверить PII inventory, external tasks, backup tombstones и legally retained exceptions; физически не удалённые данные не называть полностью стёртыми |

### LEG-F05 — функция `account_prepare_deletion`

- Статус: нет `FUNCTION photo.account_prepare_deletion(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_prepare_operations`, `account_accept_legal`, `billing_prepare_editor`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/legal-functions.sql`, запись в `contracts/legal-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_legal_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_legal.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| account_prepare_deletion | user,recent authenticated session → privacy case+ownership choices | Auth boundary до revoke; case deadline обязателен; чужие tenant details не раскрываются |

### LEG-F06 — функция `account_record_deletion_step`

- Статус: нет `FUNCTION photo.account_record_deletion_step(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `claim_collection_deletion`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/legal-functions.sql`, запись в `contracts/legal-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_legal_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_legal.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| account_record_deletion_step | request,step,fence,verified evidence → progress | Внешний deletion подтверждён; retained reason+deadline обязательны; stale worker отказ |

### LEG-F07 — функция `account_request_deletion`

- Статус: нет `FUNCTION photo.account_request_deletion(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `account_accept_legal`, `claim_collection_deletion`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/legal-functions.sql`, запись в `contracts/legal-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_legal_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_legal.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| account_request_deletion | user,recent-auth proof,key,ownership choices → deletion request | Проверить свежий purpose-bound proof и завершённый ownership preflight; на erasure commit увеличить auth_epoch, отозвать сессии/refresh/device tokens и блокировать только closing subject payments; общую transferred студию не удалять |
