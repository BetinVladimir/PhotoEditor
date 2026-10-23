# BRIEF 1B5 — Editor effects

Карточка сгенерирована `tools/gen-briefs.py` из `contracts/`. Вручную не править.

## Что читать
- Правила: `RULES.md` (всегда).
- Описание этапа: `16-editor-effects-workflow.md`.
- Зависит от этапов: 1B2, 1B4. Их файлы читать только если задача трогает их функции.

## Выпуск
- Миграции: 02606.
- Генератор: `scripts/generate-billing-editor.py` (запуск с `--check`).
- Миграции forward-only; старые не править (R9).

## Функции (источник: JSON; ACL = execute_roles)

| Сигнатура | Владелец | EXECUTE |
|---|---|---|
| `billing_prepare_editor(uuid,uuid,uuid,uuid)` | photo_fn_owner | photo_api |
| `billing_bind_editor(uuid,uuid,uuid,uuid,text,text,bytea,text)` | photo_fn_owner | photo_api |
| `billing_link_editor_children(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_settle_render(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_settle_next_editor()` | photo_fn_owner | photo_scheduler |
| `billing_settle_apply_run(uuid,uuid)` | photo_fn_owner | никто (helper) |
| `billing_guard_source_item()` | photo_fn_owner | никто (helper) |
| `billing_guard_render_terminal()` | photo_fn_owner | никто (helper) |
| `begin_editor_effect_final_render(uuid,uuid,uuid,uuid,text,text,bigint,jsonb)` | photo_fn_owner | photo_scheduler |
| `lock_editor_effect_identity(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,bigint,uuid,uuid)` | photo_fn_owner | photo_api |
| `seal_v2_graph(uuid,uuid)` | photo_fn_owner | photo_scheduler |
| `lock_editor_effect_identity_before_billing(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,bigint,uuid,uuid)` | photo_fn_owner | никто (helper) |

## Новые таблицы (определения — в SQL)

- `billing_editor_controls`

## SQL-источники

- `contracts/editor-workflow.sql`
- `contracts/editor-functions.json`

## Блокировки (из RULES)

- R23: tenant SHARE → control SHARE → balance FOR UPDATE → allowance/buckets → lease/claim.
- R24: несколько ресурсов — по `resource_code`, до run/task/adoption locks.
- R25: не вызывать finalize/settle под task lock.

## Перед коммитом

1. `python3 scripts/generate-billing-editor.py --check`
2. `python3 docs/billing/tools/lint-docs.py`
3. Запись `execute_roles` в JSON = `GRANT EXECUTE` в SQL.
