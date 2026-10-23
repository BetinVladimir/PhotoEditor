# BRIEF 1B3 — SessionAnalysis.Apply и SaveEdits со стилем

Карточка сгенерирована `tools/gen-briefs.py` из `contracts/`. Вручную не править.

## Что читать
- Правила: `RULES.md` (всегда).
- Описание этапа: `14-routed-apply-workflow.md`.
- Зависит от этапов: 1B2. Их файлы читать только если задача трогает их функции.

## Выпуск
- Миграции: 02604.
- Генератор: `scripts/generate-billing-routed-workflow.py` (запуск с `--check`).
- Миграции forward-only; старые не править (R9).

## Функции (источник: SQL, `ALTER … OWNER` и `GRANT EXECUTE`)

| Сигнатура | Владелец | EXECUTE |
|---|---|---|
| `billing_prepare_apply(uuid,uuid,uuid,text)` | photo_fn_owner | photo_api |
| `billing_bind_routed_apply(uuid,uuid,uuid,uuid,text,bytea,text,text)` | photo_fn_owner | photo_api |
| `billing_bind_apply(uuid,uuid,uuid,uuid,text,bytea,text)` | photo_fn_owner | photo_api |

## SQL-источники

- `contracts/routed-workflow.sql`

## Блокировки (из RULES)

- R23: tenant SHARE → control SHARE → balance FOR UPDATE → allowance/buckets → lease/claim.
- R24: несколько ресурсов — по `resource_code`, до run/task/adoption locks.
- R25: не вызывать finalize/settle под task lock.

## Перед коммитом

1. `python3 scripts/generate-billing-routed-workflow.py --check`
2. `python3 docs/billing/tools/lint-docs.py`
3. Запись `execute_roles` в JSON = `GRANT EXECUTE` в SQL.
