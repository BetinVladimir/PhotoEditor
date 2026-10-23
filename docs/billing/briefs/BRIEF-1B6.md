# BRIEF 1B6 — Отдельные фото-операции

Карточка сгенерирована `tools/gen-briefs.py` из `contracts/`. Вручную не править.

## Что читать
- Правила: `RULES.md` (всегда).
- Описание этапа: `17-standalone-operations.md`.
- Зависит от этапов: 1B2, 1B4, 1B5. Их файлы читать только если задача трогает их функции.

## Выпуск
- Миграции: 02607.
- Генератор: `scripts/generate-billing-operations.py` (запуск с `--check`).
- Миграции forward-only; старые не править (R9).

## Функции (источник: JSON; ACL = execute_roles)

| Сигнатура | Владелец | EXECUTE |
|---|---|---|
| `billing_prepare_operations(uuid,uuid)` | photo_fn_owner | photo_api, photo_scheduler |
| `billing_bind_operations(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_settle_operations(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_settle_next_operations()` | photo_fn_owner | photo_scheduler |
| `billing_link_operation_child(uuid,uuid,uuid)` | photo_fn_owner | никто (helper) |
| `seal_v2_graph(uuid,uuid)` | photo_fn_owner | photo_scheduler |
| `billing_guard_operation_terminal()` | photo_fn_owner | никто (helper) |
| `billing_check_operation_claim()` | photo_fn_owner | никто (helper) |
| `billing_guard_operation_item()` | photo_fn_owner | никто (helper) |

## Новые таблицы (определения — в SQL)

- `billing_operation_children`
- `billing_operation_controls`
- `billing_operation_items`
- `billing_operation_requests`

## SQL-источники

- `contracts/operations-workflow.sql`
- `contracts/operations-functions.json`

## Блокировки (из RULES)

- R23: tenant SHARE → control SHARE → balance FOR UPDATE → allowance/buckets → lease/claim.
- R24: несколько ресурсов — по `resource_code`, до run/task/adoption locks.
- R25: не вызывать finalize/settle под task lock.

## Перед коммитом

1. `python3 scripts/generate-billing-operations.py --check`
2. `python3 docs/billing/tools/lint-docs.py`
3. Запись `execute_roles` в JSON = `GRANT EXECUTE` в SQL.
