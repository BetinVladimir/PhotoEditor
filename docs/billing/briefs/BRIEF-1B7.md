# BRIEF 1B7 — Selected/final операции (приёмка НЕ завершена, см. OPEN-08)

Карточка сгенерирована `tools/gen-briefs.py` из `contracts/`. Вручную не править.

## Что читать
- Правила: `RULES.md` (всегда).
- Описание этапа: `18-selected-final-operations.md`.
- Зависит от этапов: 1B6, 1B6p. Их файлы читать только если задача трогает их функции.

## Выпуск
- Миграции: 02609 (строго после 02608).
- Генератор: `scripts/generate-billing-selected.py` (запуск с `--check`).
- Миграции forward-only; старые не править (R9).

## Функции (источник: JSON; ACL = execute_roles)

| Сигнатура | Владелец | EXECUTE |
|---|---|---|
| `billing_selected_contract(text,text)` | photo_fn_owner | никто (helper) |
| `billing_operation_session(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_selected_source(uuid,uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_selected_outcome(uuid,uuid,uuid,text)` | photo_fn_owner | никто (helper) |
| `billing_link_selected_children(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_link_selected_preview()` | photo_fn_owner | никто (helper) |
| `billing_prepare_operations(uuid,uuid)` | photo_fn_owner | photo_api, photo_scheduler |
| `billing_bind_operations(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `seal_v2_graph(uuid,uuid)` | photo_fn_owner | photo_scheduler |
| `billing_guard_operation_item()` | photo_fn_owner | никто (helper) |
| `billing_settle_operations(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_local_selected_ready(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_finalize_local_selected()` | photo_fn_owner | никто (helper) |
| `billing_settle_next_operations()` | photo_fn_owner | photo_scheduler |

## Новые таблицы (определения — в SQL)

- `billing_operation_selections`

## SQL-источники

- `contracts/selected-workflow.sql`
- `contracts/selected-functions.json`

## Блокировки (из RULES)

- R23: tenant SHARE → control SHARE → balance FOR UPDATE → allowance/buckets → lease/claim.
- R24: несколько ресурсов — по `resource_code`, до run/task/adoption locks.
- R25: не вызывать finalize/settle под task lock.

## Перед коммитом

1. `python3 scripts/generate-billing-selected.py --check`
2. `python3 docs/billing/tools/lint-docs.py`
3. Запись `execute_roles` в JSON = `GRANT EXECUTE` в SQL.
