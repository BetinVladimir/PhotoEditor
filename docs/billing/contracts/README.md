# Исполняемые DB-контракты биллинга

Это внутренние контракты миграций, не wire DTO. Все SQL expressions и SQL bodies — доверенный код репозитория. Полная целевая модель описана в разделах 03/09; фактические закрытые подэтапы — в 11–17.

| Подэтап | Источники | Генератор | Миграции |
|---|---|---|---|
| 1А, каталог | tables.json,functions.json,seed.json | scripts/generate-billing-catalog.py | 02598,02599 |
| 1Б.1, quota kernel | quota-tables.json,quota-functions.json,quota-functions.sql | scripts/generate-billing-quota.py | 02600,02601 |
| 1Б.2, standard Apply | workflow-schema.sql,workflow-tables.json,workflow-functions.sql,workflow-functions.json | scripts/generate-billing-workflow.py | 02602,02603 |
| 1Б.3, SessionAnalysis.Apply / styled SaveEdits | routed-workflow.sql (DDL, полные signatures, ACL, реализации) | scripts/generate-billing-routed-workflow.py | 02604 |
| 1Б.4, V2 render | render-workflow.sql,render-functions.json | scripts/generate-billing-render.py | 02605 |
| 1Б.5, Editor effects | editor-workflow.sql,editor-functions.json | scripts/generate-billing-editor.py | 02606 |
| 1Б.6, отдельные операции | operations-workflow.sql,operations-functions.json | scripts/generate-billing-operations.py | 02607 |

Имена таблиц между каталогами не пересекаются. `functions.json` первого подэтапа остаётся пустым: новые quota signatures находятся в `quota-functions.json`. Публикационные ограничения 1А сохраняются. Миграции 1Б.1 добавляют только read policies resource/admission catalog для доверенного SQL owner; никакого PUBLIC/runtime EXECUTE нет.

Все генераторы поддерживают `--check` и проверяются drift tests. После публикации соответствующих миграций эти источники фиксируются вместе с ними: изменение поведения оформляется новой forward-only миграцией и новой версией source, а не переписыванием старого generated SQL.

Финансовый grant от Stripe, пользовательский admission ticket и funding claim — разные уровни. Нельзя выдать рабочим ролям вызов `billing_finalize_funding_claim` с клиентским proof hash: это внутренний accounting primitive без верификации canonical workflow authority. До следующего слоя kernel работает только в явно включённом shadow scope.

В 1Б.2 только scoped Prepare/Bind выдаются photo_api, а существующие scheduler Adopt/Fail оборачиваются без потери guards. Низкоуровневый funding finalize и privileged canonical settlement остаются закрыты. Поля workflow таблиц/FK/CHECK/ACL авторитетно заданы в workflow-schema.sql; JSON inventory ссылается на этот DDL source.


Selected/final: selected-workflow.sql и selected-functions.json → scripts/generate-billing-selected.py →02609, строго после paid-funding02608. См. [этап18](../18-selected-final-operations.md).
