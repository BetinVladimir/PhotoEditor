# Бэклог: группа legal (BILL-11/12), часть 1

### LEG-00 — каркас этапа группы `legal`

- Выход: Создать каркас этапа по образцу quota-этапа (копировать структуру, не логику): `contracts/legal-tables.json`, `contracts/legal-functions.json`, `contracts/legal-functions.sql`, `scripts/generate-billing-legal.py` (копия `scripts/generate-billing-quota.py`), `tests/contracts/test_billing_legal_contract.py` (копия `tests/contracts/test_billing_quota_contract.py`), `tests/integration/test_billing_legal.py` (копия структуры `tests/integration/test_billing_quota.py`).
- Тесты (R33): сами файлы `tests/contracts/test_billing_legal_contract.py` и `tests/integration/test_billing_legal.py` создаются здесь (пустой набор проходит, не skip).
- Критерий: A. Генератор `--check` PASS на пустых источниках. B. Пустой contract-тест PASS. C. Миграция-пустышка не создаётся, пока нет таблиц.
- Запреты: R9, R28.

### LEG-T01 — таблица `legal_document_versions`

- Статус: нет `CREATE TABLE photo.legal_document_versions` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/legal-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-legal.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_legal_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_legal.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| legal_document_versions | document_key terms/privacy/cookies/dpa/photoeditor_addendum; locale text; version text; url text; content_sha256 bytea; published_at; effective_at; state draft/active/retired; reacceptance_required bool | UNIQUE(key,locale,version); URL allowlist https://legal.beeloy.org; archived exact text/hash, не только изменяемая ссылка |
- Уточнения из 09:

| legal_document_versions | UNIQUE(document_key,locale,version); content_sha256 не заменяет text archive. archive_ref text и archive_version text обязательны для active, archived plain text доступен по versioned legal URL |

### LEG-T02 — таблица `privacy_erasure_tombstones`

- Статус: нет `CREATE TABLE photo.privacy_erasure_tombstones` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/legal-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-legal.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_legal_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_legal.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| privacy_erasure_tombstones | subject_ref uuid; deletion_request_id FK; scope_type user/tenant/object; opaque_scope_ref text; erased_at; expires_at; backup_cutoff_at | Ни email, ни full object key; restore фильтрует до serving; хранение tombstone дольше самой старой допустимой backup |

### LEG-T03 — таблица `privacy_retained_records`

- Статус: нет `CREATE TABLE photo.privacy_retained_records` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/legal-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-legal.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_legal_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_legal.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| privacy_retained_records | subject_ref uuid; record_type text; record_id uuid; retention_policy_id FK; legal_hold_ref text?; retain_until; restricted_at; erased_at? | Restricted financial/legal retention; policy+expiry обязательны; pseudonymous != anonymous |

### LEG-T04 — таблица `privacy_retention_policies`

- Статус: нет `CREATE TABLE photo.privacy_retention_policies` в `backend/db/migrations`.
- Вход: строка 03 ниже; уточнения 09 §3 (если есть); образец формата — `contracts/quota-tables.json` (columns, primary_key, constraints, indexes).
- Выход: запись в `contracts/legal-tables.json`, затем перегенерация миграции генератором этапа.
- Критерий: A. `scripts/generate-billing-legal.py --check` PASS. B. contract-тест видит таблицу, PK, FK, CHECK. C. integration-тест: tenant composite FK, RLS, ограничения (образец `EXAMPLES-TESTS.md`). D. Новых колонок сверх 03/09 нет.
- Тесты (в этой же задаче, R33, `TESTING.md`): `tests/contracts/test_billing_legal_contract.py` — колонки, PK, FK, CHECK из JSON; `tests/integration/test_billing_legal.py` — по одному негативному тесту на каждый CHECK и FK, RLS чужого tenant. Без них задача не принята.
- Запреты: R7, R8, R9; типы по 09 §3 (`*_id uuid`, `*_at timestamptz`, деньги и количества `bigint`); неоднозначный тип → STOP и строка в `OPEN.md`.
- Из 03:

| privacy_retention_policies | code text; version int; data_category text; legal_basis text; retention_days int?; retention_trigger text; backup_max_days int?; approval_ref text?; state draft/approved/retired | UNIQUE(code,version); сроки обязательны для approved; нет unlimited NULL; draft блокирует live, не erasure |

### LEG-F01 — функция `account_accept_legal`

- Статус: нет `FUNCTION photo.account_accept_legal(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): нет.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/legal-functions.sql`, запись в `contracts/legal-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Тесты (в этой же задаче, R33, `TESTING.md` T4): `tests/contracts/test_billing_legal_contract.py` — ACL JSON = GRANT; `tests/integration/test_billing_legal.py` — успех, отказ авторизации, точный повтор, конфликт при другом hash; при общих остатках ещё конкурентный тест на двух соединениях; если функцию вызывает Go — harness под ролью БД. Без них задача не принята.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| account_accept_legal | user,document versions,explicit actions,client_event → onboarding status | Требуемые active version/hash, locale и DPA для B2B; contract accepted отдельно от privacy acknowledged; optional false по умолчанию; atomic account activation |
