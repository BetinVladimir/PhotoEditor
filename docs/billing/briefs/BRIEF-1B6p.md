# BRIEF 1B6p — Paid operation funding (исправление)

Карточка сгенерирована `tools/gen-briefs.py` из `contracts/`. Вручную не править.

## Что читать
- Правила: `RULES.md` (всегда).
- Описание этапа: `17-standalone-operations.md и 18-selected-final-operations.md`.
- Зависит от этапов: 1B6. Их файлы читать только если задача трогает их функции.

## Выпуск
- Миграции: 02608.
- Генератор: `scripts/generate-billing-paid-operation.py` (запуск с `--check`).
- Миграции forward-only; старые не править (R9).

## Функции (источник: JSON; ACL = execute_roles)

| Сигнатура | Владелец | EXECUTE |
|---|---|---|
| `billing_allocate_paid_operation_lease(uuid,text,uuid,text,bigint,integer,text,bigint)` | photo_fn_owner | никто (helper) |
| `billing_bind_operations(uuid,uuid)` | photo_fn_owner | никто (helper) |

## SQL-источники

- `contracts/paid-operation-funding.sql`
- `contracts/paid-operation-functions.json`

## Блокировки (из RULES)

- R23: tenant SHARE → control SHARE → balance FOR UPDATE → allowance/buckets → lease/claim.
- R24: несколько ресурсов — по `resource_code`, до run/task/adoption locks.
- R25: не вызывать finalize/settle под task lock.

## Перед коммитом

1. `python3 scripts/generate-billing-paid-operation.py --check`
2. `python3 docs/billing/tools/lint-docs.py`
3. Запись `execute_roles` в JSON = `GRANT EXECUTE` в SQL.
