# BRIEF 1B2 — Standard Apply

Карточка сгенерирована `tools/gen-briefs.py` из `contracts/`. Вручную не править.

## Что читать
- Правила: `RULES.md` (всегда).
- Описание этапа: `13-standard-apply-workflow.md`.
- Зависит от этапов: 1B1. Их файлы читать только если задача трогает их функции.

## Выпуск
- Миграции: 02602, 02603.
- Генератор: `scripts/generate-billing-workflow.py` (запуск с `--check`).
- Миграции forward-only; старые не править (R9).

## Функции (источник: JSON; ACL = execute_roles)

| Сигнатура | Владелец | EXECUTE |
|---|---|---|
| `billing_prepare_apply(uuid,uuid,uuid,text)` | photo_fn_owner | photo_api |
| `billing_bind_apply(uuid,uuid,uuid,uuid,text,bytea,text)` | photo_fn_owner | photo_api |
| `billing_lock_apply_run(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_settle_apply_run(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `adopt_apply_results(uuid,uuid,uuid)` | photo_fn_owner | photo_scheduler |
| `fail_apply_build(uuid,uuid,text)` | photo_fn_owner | photo_scheduler |
| `billing_guard_terminal_run()` | photo_fn_owner | никто (helper) |

## SQL-источники

- `contracts/workflow-schema.sql`
- `contracts/workflow-functions.sql`
- `contracts/workflow-functions.json`
- `contracts/workflow-tables.json`

## Блокировки (из RULES)

- R23: tenant SHARE → control SHARE → balance FOR UPDATE → allowance/buckets → lease/claim.
- R24: несколько ресурсов — по `resource_code`, до run/task/adoption locks.
- R25: не вызывать finalize/settle под task lock.

## Перед коммитом

1. `python3 scripts/generate-billing-workflow.py --check`
2. `python3 docs/billing/tools/lint-docs.py`
3. Запись `execute_roles` в JSON = `GRANT EXECUTE` в SQL.

## Таблицы (источник: `contracts/workflow-tables.json`)

- `billing_workflow_controls` (поля, FK, CHECK — в `contracts/workflow-schema.sql`)
- `billing_photo_identities` (поля, FK, CHECK — в `contracts/workflow-schema.sql`)
- `billing_cost_quotes` (поля, FK, CHECK — в `contracts/workflow-schema.sql`)
- `billing_usage_requests` (поля, FK, CHECK — в `contracts/workflow-schema.sql`)
- `billing_usage_charge_items` (поля, FK, CHECK — в `contracts/workflow-schema.sql`)
- `billing_photo_free_slots` (поля, FK, CHECK — в `contracts/workflow-schema.sql`)
- `billing_usage_tickets` (поля, FK, CHECK — в `contracts/workflow-schema.sql`)
- `billing_usage_run_links` (поля, FK, CHECK — в `contracts/workflow-schema.sql`)
- `billing_result_references` (поля, FK, CHECK — в `contracts/workflow-schema.sql`)
