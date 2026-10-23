# Бэклог: группа finance (BILL-04), часть 4

### FIN-F06 — функция `billing_bind_provider_price`

- Статус: нет `FUNCTION photo.billing_bind_provider_price(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_bind_render`, `billing_bind_routed_apply`, `billing_bind_operations`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_bind_provider_price | account_id, local_target, Stripe verified snapshot hash → binding | Проверить XOR target, EUR, amount/interval/tax_behavior, active account; secret не принимается | Operator provisioning; unique local target/account |

### FIN-F07 — функция `billing_create_cost_quote`

- Статус: нет `FUNCTION photo.billing_create_cost_quote(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_project_quota`, `billing_selected_contract`, `billing_append_quota_event`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_create_cost_quote | actor/tenant,selection snapshot,workflow/params hash → finite max-cost lines | API с текущей workflow authority; не выдаёт ресурсы и не снимает деньги |

### FIN-F08 — функция `billing_create_quote`

- Статус: нет `FUNCTION photo.billing_create_quote(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_project_quota`, `billing_append_quota_event`, `billing_selected_outcome`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_create_quote | tenant, actor, selection, expected_revision, key → immutable quote | Читать только опубликованные разрешённые offers; проверить country/tax input; сохранить amounts, limits, effective date, expiry и policy hash | API owner; foreign offer/hidden plan/нулевой pack count отказ |

### FIN-F09 — функция `billing_ensure_profile`

- Статус: нет `FUNCTION photo.billing_ensure_profile(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_prepare_apply`, `billing_guard_source_item`, `list_v25_lens_profiles`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_ensure_profile | tenant, bootstrap_policy, key → profile | Lock lifecycle; создать trial/beta profile и один trial grant только по выбранной migration policy; deleting отказ | Существующий profile возвращается; internal bootstrap |

### FIN-F10 — функция `billing_publish_plan_version`

- Статус: нет `FUNCTION photo.billing_publish_plan_version(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_bind_operations`, `billing_bind_editor`, `billing_prepare_operations`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_publish_plan_version | version_id, expected_revision → published version | Проверить полный набор лимитов, retention manual, price, tax mode, operation mappings и capacity gates; freeze hash; неполный Custom/selfservice отказ | Только catalog operator; published exact replay без изменений |

### FIN-F11 — функция `billing_request_change`

- Статус: нет `FUNCTION photo.billing_request_change(` в `backend/db/migrations`. Возможно покрыта под другим именем (проверить до работы): `billing_quota_pin_budget`, `billing_project_quota`, `billing_quota_lock`.
- Вход: строка 04 ниже; типы аргументов по 09 §3 (в 04 даны имена, не типы); образец — `EXAMPLES.md` §1–2.
- Выход: тело в `contracts/finance-functions.sql`, запись в `contracts/finance-functions.json`, перегенерация миграции.
- Критерий: A. `--check` генератора PASS. B. `execute_roles` в JSON = `GRANT` в SQL, роли из колонки «права» строки 04. C. integration-тест: положительный путь, отказ авторизации, точный повтор и конфликт hash. D. Порядок блокировок R23–R26.
- Запреты: R11, R12, R16–R26; не писать сетевые вызовы.
- Из 04:

| billing_request_change | tenant, quote_id, expected_revision,key → change | Только один unresolved immediate change; downgrade future date; upgrade payment pending | Owner; concurrent tab conflict с current revision |
