# Этап 1Б.7: специализированные selected/final операции

## Граница этапа и состояние приёмки

Forward-only миграция `02609_billing_selected_workflow.sql` следует за paid-funding исправлением02608. Источник — `contracts/selected-workflow.sql`, signatures/ACL — `contracts/selected-functions.json`, генератор — `scripts/generate-billing-selected.py`. Исторические02607/02608 и общий семиаргументный allocator не изменяются. Перед заменой каждой из шести функций миграция проверяет body hash, signature/return type, owner, язык, security definer, search_path, options и raw/effective ACL. Drift прерывает транзакцию.

Реализация подготовлена; окончательная совместная PostgreSQL/Go приёмка ещё обязательна. Первичный изолированный прогон подтвердил native local auto/review и open-eyes selected/materialization; расширенный прогон выявил пропущенную live consent authority open-eyes. Она исправлена вызовом native selected authority, но повторный runtime-прогон исправления ещё не завершён. Статические PASS не заменяют runtime-приёмку. Когорты остаются выключенными, цены и production-qualified policies этим этапом не публикуются. Stripe не изменяется; разрешён только тестовый режим.

## Контракты успеха

| Operation version1 | success_contract | Native authority |
|---|---|---|
| portrait.glare | restoration_selected_v1 | restoration completion + glare completed-source authority + restoration selection |
| image.reflection | restoration_selected_v1 | restoration completion + reflection current authority внутри selection reader |
| portrait.open_eyes | open_eyes_selected_v1 | open-eyes completion + selected authority + selection reader |
| image.relight | relight_selected_v1 | relight completion + selection reader с live review authority |
| portrait.hair / portrait.clothing | local_portrait_selected_v1 | native local result reader для auto либо native local review selection |

Native source, qualification, consent, session, membership, candidate provenance и media fences не заменяются финансовыми признаками. Native getter open-eyes сам по себе недостаточен: проверка `v28_open_eyes_selected_authority` обязательна, как в Apply. Для glare отдельно вызывается `v28_glare_completed_source_ready`. Неизвестная версия операции не получает selected adapter. Назначить известной selected операции generic success contract нельзя: binder отказывает атомарно до финансирования.

Контекст policy определяется реальным workflow_type (`preview`/`standalone`). Наличие adapter не является доказательством benchmark/production qualification и не включает продажи. Для каждой операции оператор должен установить корректную context-specific policy и ресурс, затем отдельно включить tenant cohort.

## Admission и единственность списания

Prepare выполняется до input locks и берёт tenant lock. Источник единицы selected операции — immutable base asset version из native intent/descriptor, проверенная против frozen snapshot/original SHA, а не произвольный node ordinal. Session извлекается из соответствующего native config; legacy semantic session поддерживается. Донор open-eyes не становится второй коммерческой единицей.

Binder фиксирует `success_contract` в immutable billing_operation_items вместе с policy/source/quantity. Claim создаётся существующим quota kernel. Paid quantity выше chunk_quantity использует точный приватный `billing_allocate_paid_operation_lease` из02608; обычные chunk leases и конечный оптимистичный бюджет сохраняются. Повтор admission возвращает тот же request только при том же request hash.

Дочерние preview обнаруживаются только через native typed task ownership (`v26_derivative_task_id`/`v28_restoration_preview_task_id`), tenant, actor, kind и canonical source_result_id. Клиентский parent UUID не является полномочием. Private trigger регистрирует технического ребёнка; settlement повторяет discovery для ранее созданных preview. Уже отдельно профинансированный либо конфликтующий ребёнок вызывает отказ. Повтор materialization selected version не создаёт новый ticket/claim/debit. Отдельный пользовательский новый root требует собственного admission.

## Settlement и возвраты

Финансовый финализатор ждёт terminal root, tasks и всех зарегистрированных детей. Общая canonical proof проверка сохраняет current attempt/fence, единственный ready verified/versioned root document, точную task-root link и V2 adoption. Далее требуется ровно одна native selected outcome:

* candidate: consume один claim;
* base/reject или skip: release резерв;
* pending review, отозванная authority, отсутствующая/неверная canonical proof при completed root: HOLD, без consume и без возврата; повтор после восстановления может завершить settlement;
* failed/cancelled специализированный root: release после drain детей, даже если preview producer успел успешно завершиться.

Generic partial-success поведение02607 сохраняется для generic items. Результат финансовой финализации и proof hash фиксируют выбранный route/image/native decision. Повторный scanner ничего не списывает. Финализированный root/child нельзя воскресить в running.

Local hair/clothing review reject/skip получает private финализатор, использующий native selection reader. Он допускает завершение только точного single-task/single-review selected root без открытых expansions; reject/skip не допускает оставшихся content consumers, кроме точных read-only native previews. Tenant lock берётся до вызова lock-taking native predicates. Конкурирующие scanners используют SKIP LOCKED. Неготовая строка получает 30s backoff; финансовый scanner обрабатывает максимум16 requests за вызов. Ни worker step, ни pixel compute не выполняют дополнительный коммерческий admission.

## Таблицы и функции

`billing_operation_items.success_contract text NOT NULL`: default generic для исторических items; CHECK содержит generic и четыре selected contracts. Поле включено в immutable guard.

Новая append-only `billing_operation_selections`:

| Поле | Назначение |
|---|---|
| tenant_id,item_id | составной PK; FK на billing_operation_items |
| contract | зафиксированный success contract |
| route | CHECK candidate/base/skip |
| image_id,image_sha | выбранный artifact FK и SHA256; обязательны candidate/base, NULL при skip |
| review_id,decision_hash | native review FK и decision proof; NULL для auto |
| recorded_at | время фиксации |

RLS FORCE; runtime ролям таблица закрыта, owner имеет SELECT/INSERT. UPDATE запрещён immutable trigger, DELETE не выдан. PII в финансовую проекцию не копируется; tenant erasure и retention остаются отдельными обязательствами общей billing GDPR спецификации.

Private функции: `billing_selected_contract` — точное отображение operation/version; `billing_operation_session` — native session; `billing_selected_source` — base version; `billing_selected_outcome` — live native proof; `billing_link_selected_children` и trigger `billing_link_selected_preview` — trusted children; `billing_local_selected_ready` и `billing_finalize_local_selected` — local review завершение. Все SECURITY DEFINER с фиксированным search_path там, где требуется доступ к данным. Новых публичных низкоуровневых финансовых API нет. Prepare сохраняет API/scheduler доступ; seal/scanner — scheduler. Полный ABI/ACL авторитетно задан SQL/manifest.

## Требования к завершённости и тестам

1. Generated drift/manifest/ACL tests PASS; все шесть predecessor guards проверены на штатном upgrade и отрицательных metadata/body/ACL drift.
2. Изолированная реальная PostgreSQL: admission, consume, reject/skip refund, pending HOLD, failure/cancel drain, replay, missing live proof и восстановление; conservation bucket/lease/claim/ledger после каждого исхода.
3. Не менее шести одновременных scanner connections: единственный финансовый исход и отсутствие deadlock/двойного debit/refund. Повтор materialization и конкурирующий settlement не создают второй claim.
4. Native local auto hair/clothing, native reviewed accept/reject/skip, open-eyes selected finalize и повтор Apply проверяются без замены native функций/guards. Consent withdrawal после completed root удерживает резерв.
5. Для glare/reflection/relight обязательна отдельная совместная проверка на полном qualified native fixture. Текущие mapping/ACL/closed-proof тесты этих трёх adapters не являются положительной финансовой приёмкой. GPU/pixel qualification не заявляется relational fixtures.
6. Регрессия02608: units>chunk при достаточном paid остатке проходит; недостаток/allowance-only не финансирует oversize paid item. Предыдущие Apply/Render/Editor/generic billing suites и Go scheduler/db tests должны пройти на общей последовательности миграций.

Новые сценарии: `tests/integration/test_billing_selected.py`; native relational fixtures — `selected_local_fixture.py`, `selected_open_eyes_fixture.py`; статические контракты — `tests/contracts/test_billing_selected_contract.py`. Общий прогон выполняется последовательно с другими PostgreSQL/Go прогонами workspace. До выполнения перечисленного включение cohort и статус «этап принят» запрещены.
