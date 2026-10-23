# Бэклог: группа legal (BILL-11/12), часть 2

### LEG-F04 — функция `account_issue_recent_auth`

- Статус: нет `FUNCTION photo.account_issue_recent_auth(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): нет.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/legal-functions.sql`, запись в `contracts/legal-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| account_issue_recent_auth | verified fresh OTP purpose binding → opaque one-use proof | photo_auth, user/session/epoch/purpose bound; refresh не доказательство |

### LEG-F05 — функция `account_prepare_deletion`

- Статус: нет `FUNCTION photo.account_prepare_deletion(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_prepare_operations`, `billing_prepare_editor`, `lock_semantic_preapply_preview_selection`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/legal-functions.sql`, запись в `contracts/legal-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| account_prepare_deletion | user,recent authenticated session → privacy case+ownership choices | Auth boundary до revoke; case deadline обязателен; чужие tenant details не раскрываются |

### LEG-F06 — функция `account_record_deletion_step`

- Статус: нет `FUNCTION photo.account_record_deletion_step(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `claim_collection_deletion`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/legal-functions.sql`, запись в `contracts/legal-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| account_record_deletion_step | request,step,fence,verified evidence → progress | Внешний deletion подтверждён; retained reason+deadline обязательны; stale worker отказ |

### LEG-F07 — функция `account_request_deletion`

- Статус: нет `FUNCTION photo.account_request_deletion(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `claim_collection_deletion`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/legal-functions.sql`, запись в `contracts/legal-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| account_request_deletion | user,recent-auth proof,key,ownership choices → deletion request | Проверить свежий purpose-bound proof и завершённый ownership preflight; на erasure commit увеличить auth_epoch, отозвать сессии/refresh/device tokens и блокировать только closing subject payments; общую transferred студию не удалять |

### LEG-F08 — функция `account_update_privacy_preferences`

- Статус: нет `FUNCTION photo.account_update_privacy_preferences(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): нет.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/legal-functions.sql`, запись в `contracts/legal-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| account_update_privacy_preferences | user,expected_revision,analytics/marketing,key → preferences | Event на каждое изменение; withdraw без потери основной услуги; новые consent-dependent SDK запрещены после commit |
