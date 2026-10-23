# BRIEF 1B4 — V2 render

Карточка сгенерирована `tools/gen-briefs.py` из `contracts/`. Вручную не править.

## Что читать
- Правила: `RULES.md` (всегда).
- Описание этапа: `15-v2-render-workflow.md`.
- Зависит от этапов: 1B2. Их файлы читать только если задача трогает их функции.

## Выпуск
- Миграции: 02605.
- Генератор: `scripts/generate-billing-render.py` (запуск с `--check`).
- Миграции forward-only; старые не править (R9).

## Функции (источник: JSON; ACL = execute_roles)

| Сигнатура | Владелец | EXECUTE |
|---|---|---|
| `billing_prepare_render(uuid,uuid,uuid,uuid)` | photo_fn_owner | photo_scheduler |
| `billing_render_source_frames(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_bind_render(uuid,uuid,uuid,uuid,text)` | photo_fn_owner | photo_scheduler |
| `billing_settle_render(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_settle_next_render()` | photo_fn_owner | photo_scheduler |
| `billing_settle_apply_run(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_guard_render_terminal()` | photo_fn_owner | никто (helper) |
| `billing_guard_source_item()` | photo_fn_owner | никто (helper) |
| `seal_v2_graph(uuid,uuid)` | photo_fn_owner | photo_scheduler |

## Новые таблицы (определения — в SQL)

- `billing_render_controls`
- `billing_render_frames`
- `billing_render_results`

## SQL-источники

- `contracts/render-workflow.sql`
- `contracts/render-functions.json`

## Блокировки (из RULES)

- R23: tenant SHARE → control SHARE → balance FOR UPDATE → allowance/buckets → lease/claim.
- R24: несколько ресурсов — по `resource_code`, до run/task/adoption locks.
- R25: не вызывать finalize/settle под task lock.

## Перед коммитом

1. `python3 scripts/generate-billing-render.py --check`
2. `python3 docs/billing/tools/lint-docs.py`
3. Запись `execute_roles` в JSON = `GRANT EXECUTE` в SQL.
