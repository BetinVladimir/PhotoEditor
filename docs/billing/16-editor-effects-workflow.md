# Editor effects: учёт пользовательской операции

Реализация этапа 1Б.5, миграция `02606_billing_editor_workflow.sql`. Источник генерации — [contracts/editor-workflow.sql](contracts/editor-workflow.sql); ACL и сигнатуры — [contracts/editor-functions.json](contracts/editor-functions.json). Проверка дрейфа: `python3 scripts/generate-billing-editor.py --check`. Исторические миграции не переписываются.

## Граница оплаты

Пользовательский маршрут `Editor.Effects.<project UUID>` создаёт один request `workflow_kind=editor_effects`, один charge item и один ticket для original SHA в tenant. Разрешение, промежуточные image/quality задачи, seal и дочерний final render включены в этот ticket. Отдельные operation units не расходуются: policy `editor.effects_seal/1`, `included_standard`, `operation_units=0`, success contract `editor_final_adopted_v1`. Общие с Apply/V2 photo identities дают первый успешный результат и два бесплатных успешных повтора в течение168 часов. Неуспех не расходует успешный повтор.

Свежий admission проходит через `Service.apply`: bounded PrepareEditor до domain locks, построение frozen single-item snapshot, создание run/build, `editorMode.commit`, затем BindEditor в одной транзакции с idempotency receipt. Отказ funding откатывает run, snapshot, build и receipt. Принятый повтор возвращает прежний receipt без нового funding. Независимый initial по тому же original во время принятого initial возвращает `PHOTO_INITIAL_IN_PROGRESS`. Hash/key/route mismatch не считается повтором.

Внутренний `V26.AuthoredBase.*` имеет отдельную модель и не создаёт `editor_effect_builds`; этот этап не расширяет его коммерческую политику. Существующие готовые Editor builds не получают tickets задним числом. Включать cohort только после drain старых активных Editor builds: новый seal guard требует accepted ticket при включённом cohort.

## Состояния и доказательство результата

Промежуточный Apply может быть completed задолго до пользовательского результата. `billing_settle_apply_run` исключает `editor_effects`; generic adoption/failure/cancellation не завершает его финансы. Durable reconciliation выполняет Editor coordinator через `billing_settle_next_editor()`.

При `editor_effect_builds.state=complete` проверяются существующий `editor_effect_final_origin_ready`, актуальные source/frozen origin, canonical seal/quality/producer refs и терминальность всех связанных run. Для детерминированной `editor_effect_final_task_id(origin)` требуется `render.compose/1`, canonical result текущих attempt/fence, готовые verified/versioned/unexpired root и WorkingImage, правильные V2 artifact links, запись `v2-adopt` и разрешённый review outcome. Успех фиксируется в существующей `billing_render_results`; ticket/claim/identity/ledger и request завершаются атомарно. Время первого успеха берётся из adoption/review, а не времени сканирования.

Public accepted replay канонизирует JSONB receipt тем же способом, что `idempotency.Use`, перед проверкой строгого ApplyAccepted контракта. Форматирование PostgreSQL не вызывает ложный DEPENDENCY_UNAVAILABLE; unknown fields, другой run/collection/status и изменённый intent по-прежнему запрещены.

Недостаток или исчезновение доказательства при успешном результате удерживает accepted funding; автоматического возврата нет. Статус complete сам по себе не является финансовым доказательством. Repair восстанавливает доказательство, после чего reconciliation можно повторить. Backoff30 секунд предотвращает голодание других requests.

Возврат разрешён после `withdraw_editor_effect_build` → state failed и окончания всех stage runs/tasks. Промежуточный отказ запускает drain/cancellation; до его окончания funding остаётся принятым. Неуспех освобождает claim или free slot ровно один раз. Terminal guard запрещает возобновлять оплаченный/возвращённый root или связанный child. Неопределённый успех не преобразуется в failure ради освобождения лимита.

## Структуры и SQL функции

| Структура / поле | Назначение |
|---|---|
| `billing_editor_controls.tenant_id` | PK/FK tenant; отдельный opt-in Editor |
| `enabled` | boolean NOT NULL DEFAULT false; миграция не добавляет включённых tenants |
| `policy_version` | integer NOT NULL DEFAULT1, только1 |
| `billing_usage_requests.route/workflow_kind` | Editor.Effects.UUID / editor_effects, CHECK соответствия |
| `canonical_request_hash`, `request_key`, `actor_user_id` | durable idempotency; hash32 байта серверного intent |
| `run_id`, `period_id`, `quote_id` | пользовательский root и неизменная финансовая привязка |
| `render_reconcile_after` | общий с render backoff; отдельный partial queue index Editor accepted |
| `billing_usage_charge_items.source_item_id` | NULL допустим для Editor/V2; original identity закреплена отдельно |
| `billing_usage_run_links` | root + technical_child seal/final; один charge item |
| `billing_render_frames` | детерминированная final task, source version, ordinal0; запись после создания final child |
| `billing_render_results` | immutable canonical result и artifact refs; повтор settlement не добавляет debit |

| Функция | Доступ и обязанности |
|---|---|
| `billing_prepare_editor(tenant,actor,session,project)` | API; cohort/current auth, tenant UPDATE → quota mutex; bounded recovery expired leases; Go deadline200ms, timeout →503 |
| `billing_bind_editor(tenant,run,actor,session,key,route,hash,instance)` | API; queued unsealed trusted Editor root без задач; source/frozen snapshot сверка; fixed server quote; initial/free/paid selection; bounded claim; атомарный ticket |
| `billing_link_editor_children(tenant,origin)` | Только definer; повторяемые связи seal/final + render frame, без нового funding |
| `billing_settle_render(tenant,origin)` | Только definer; общий proof settlement V2/Editor; сначала проверка Editor terminal authority |
| `billing_settle_next_editor()` | Scheduler; до16 кандидатов, SKIP LOCKED tenant, только active lifecycle; false при отсутствии прогресса; 30s backoff при недоступном proof |
| `begin_editor_effect_final_render(...)` | Scheduler wrapper существующего admission; после успеха связывает child в той же транзакции |
| `seal_v2_graph(tenant,run)` | Сохраняет V2 funding guard; при enabled Editor запрещает seal без accepted root ticket |
| `lock_editor_effect_identity(...)` | API definer; отсутствовавшая граница существующего Go admission: frozen upload, draft/document stream и source/recipe row SHARE locks + повторная origin/auth проверка |

Все новые таблицы FORCE RLS. Runtime roles не получают прямую запись controls/ledger. Definer функции имеют fixed search_path. До-billing aliases final begin и seal закрыты от API/scheduler/worker/PUBLIC. Дополнительный UPDATE(id) на draft tables получает только non-login `photo_fn_owner`, он нужен PostgreSQL для row SHARE; runtime DML не расширяется.

Scheduler устанавливает tenant context только вокруг origin-ready и восстанавливает прежнее значение. Без этого existing origin scope возвращал false на чистом scheduler соединении — конкурентный тест проверяет именно такую конфигурацию.

## Ограниченный оптимистичный допуск

Editor использует существующие kernel leases/claims стандартных фото, общий с остальными workflow конечный budget периода. Нулевой paid balance допускает только остаток оптимистичного allowance. Его успешное списание не восстанавливается top-up или повторным запросом. Истощение даёт `BILLING_QUOTA_EXCEEDED` либо `BILLING_OVERRUN_BLOCKED` при уже списанном overrun; ограниченные бесплатные slots не увеличивают paid allowance. Ресурсные/physical capacity ограничения остаются отдельной обязательной границей.

## Проверка и критерии завершения

Новая integration suite `tests/integration/test_billing_editor.py` проверяет настоящее PostgreSQL применение миграции, role ACL, root binding, final canonical metadata, adoption, reconciliation и conservation. Go test `TestIntegrationBillingEditorAdmission` вызывается Python harness с изолированной БД и настоящим photo_api pool: свежий Service.apply, cold lock wait с точным BILLING_UNAVAILABLE, frozen snapshot, concurrent receipt replay и rollback независимого initial. Public Begin replay проверяется без fresh installation/object dependencies, с конфликтом intent и revoked session. Private mode представляет уже скомпилированный доверенный closure; этот тест не заявляет проверку публичного compiler, GPU или native pixel execution.

Обязательные положительные сценарии: accepted funding до выполнения; отсутствие раннего debit на Apply completed; один debit final success; repeat settlement no-op; один request между parent/final; finite allowance; два reserved free rerun slots. Отрицательные: hash mismatch, revoked session, exhausted allowance, missing adoption, expired/unverified output, withdrawn source, early refund до drain, прямой child Render debit и закрытые aliases. Конкурентные: exact bind replay, settlement с чистых scheduler соединений, independent initial for same original; conservation после каждого финансового исхода.

Перед включением: generator/manifest checks, Go suites и regression Apply/routed/V2 на head02606 должны пройти; миграция применяется штатным release процессом, затем отдельный закрытый cohort. Рабочая БД в рамках этого этапа не мигрируется. Stripe остаётся только test-only; live account, checkout и UI в данном этапе не включаются. UI требование React Native Paper в apps/photographer сохраняется.


Фактическая проверка от21.10.2026: общий billing regression116 PASS; финальный Editor/V2 regression30 PASS после исправления public JSONB replay (эти прогоны пересекаются). Финальный набор содержит17 Editor integration tests,2 contract tests и11 V2 regressions. Go run/workflow/v2render/db и6 generator drift checks PASS. Тестовые БД одноразовые; production migration/cohort не выполнялись.
