# BRIEF 1B1 — Quota kernel (учёт квот, shadow)

Таблицы вынесены в `briefs/TABLES-1B1.md` (грузить только если задача трогает таблицы).

Карточка сгенерирована `tools/gen-briefs.py` из `contracts/`. Вручную не править.

## Что читать
- Правила: `RULES.md` (всегда).
- Описание этапа: `12-quota-kernel.md`.
- Зависит от этапов: 1A. Их файлы читать только если задача трогает их функции.

## Выпуск
- Миграции: 02600, 02601.
- Генератор: `scripts/generate-billing-quota.py` (запуск с `--check`).
- Миграции forward-only; старые не править (R9).

## Функции (источник: JSON; ACL = execute_roles)

| Сигнатура | Владелец | EXECUTE |
|---|---|---|
| `billing_quota_lock(uuid,text)` | photo_fn_owner | никто (helper) |
| `billing_append_quota_event(uuid,text,uuid,uuid,uuid,uuid,text,bigint,text,text)` | photo_fn_owner | никто (helper) |
| `billing_grant_bootstrap(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_allocate_admission_lease(uuid,text,uuid,text,bigint,integer,text)` | photo_fn_owner | никто (helper) |
| `billing_issue_funding_claim(uuid,text,uuid,uuid,text,bytea,bigint)` | photo_fn_owner | никто (helper) |
| `billing_finalize_funding_claim(uuid,text,uuid,bigint,bytea)` | photo_fn_owner | никто (helper) |
| `billing_close_admission_lease(uuid,text,uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_expire_quota_buckets(uuid,text,integer)` | photo_fn_owner | никто (helper) |
| `billing_project_quota(uuid,text)` | photo_fn_owner | никто (helper) |
| `billing_quota_reject_immutable()` | photo_fn_owner | никто (helper) |
| `billing_quota_pin_budget()` | photo_fn_owner | никто (helper) |

## SQL-источники

- `contracts/quota-functions.sql`
- `contracts/quota-functions.json`
- `contracts/quota-tables.json`

## Блокировки (из RULES)

- R23: tenant SHARE → control SHARE → balance FOR UPDATE → allowance/buckets → lease/claim.
- R24: несколько ресурсов — по `resource_code`, до run/task/adoption locks.
- R25: не вызывать finalize/settle под task lock.

## Перед коммитом

1. `python3 scripts/generate-billing-quota.py --check`
2. `python3 docs/billing/tools/lint-docs.py`
3. Запись `execute_roles` в JSON = `GRANT EXECUTE` в SQL.
