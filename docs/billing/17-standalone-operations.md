# Квоты отдельных операций: этап 1Б.6

- Миграция `02607_billing_operations_workflow.sql`, источник [contracts/operations-workflow.sql](contracts/operations-workflow.sql), ACL [contracts/operations-functions.json](contracts/operations-functions.json).
- Генератор `scripts/generate-billing-operations.py --check`.
- Исторические SQL02600–02606 сохранены.

## Подключённая граница и условия включения

- Рабочий V2 graph admission вызывает bounded `PrepareOperations` до регистрации input closures; общий SQL `seal_v2_graph` связывает accounting и прежний seal в одной транзакции.
- Это покрывает вызовы общего V2 seal, включая SQL-only callers.
- Если native graph/qualification/physical-capacity guard возвращает false, все созданные operation requests/claims/allocations откатываются.
- Существующие transport, native qualification, review и physical reservation guards сохраняются.

- Новый переключатель `billing_operation_controls.enabled` отдельный для tenant, DEFAULT false, без включённых seed rows.
- Тарифы, operation offers и benchmark результаты миграция не выдумывает.
- До benchmark, approved policy и клиентского показа quote коммерческая публикация отдельных операций остаётся закрытой.
- Stripe остаётся test-only;
- API платежей и UI в этом этапе не меняются.
- Последующий UI только React Native Paper в apps/photographer.

- Поддерживаемая accounting policy: точные registry operation/version, `standalone` либо `preview`, `success_contract=v2_adopted_outputs_v1`, `rerun_policy=none`.
- Операция имеет явно квалифицированный конечный результат на фото: leaf node `operation:<ordinal>` соответствует immutable photo snapshot item.
- Промежуточные prerequisite tasks этого графа включены в стоимость результата и не получают отдельные credits.
- Один original SHA / policy / request допускает только один item; дубликат отклоняется целиком.

- Это не автоматическая квалификация всех исторических runtime операций одним универсальным proof.
- Learning/capture/session/global multi-input операции без указанной photo boundary, specialized selected-output workflows и иные success contracts требуют собственных утверждённых адаптеров.
- Для них неизвестная/disabled/included_standard standalone policy в enabled cohort закрывает новый admission, а не включает бесплатное выполнение.
- Наличие benchmark_ref в синтетических тестах не является benchmark продукта.

## Ресурсы и повторные запросы

- `paid_operation` использует исключительно свой `operation.*` ресурс и положительное `units_per_success`.
- `free_beta` использует отдельный `operation.beta.*`, единицу внутреннего budget на результат, конечный `beta_budget` на tenant/policy/period и отдельные resource grants.
- Paid и beta ресурсы не смешиваются.
- Стандартные фото не служат валютой для операций; operation credits не пополняют standard_photo.
- Бесплатные photo reruns не расходуются.

- Resource/admission policies и credits выдаются утверждённой отдельной публикацией/операторским закрытым rollout.
- В миграции нет operation цен, offers, implicit money grants или неограниченной beta.
- Policy version/hash, resource, units и original/source привязка фиксируются при admission.
- Изменение каталога не переписывает принятую стоимость.

- Повтор native request/key возвращает прежний V2 run/receipt и не создаёт второй request.
- Повтор accounting bind того же run сравнивает hash immutable config + task IDs/operation/version/input digest.
- Изменённый граф даёт conflict.
- Новый пользовательский run считается новой операцией: `rerun_policy=none`, без двух бесплатных photo reruns.

- При известном дефиците batch (сумма units>1), любой недостаточный resource отклоняет весь request до выделения claims.
- Порядок resource locks лексикографический.
- Одиночная единица может использовать остаток finite optimistic allowance существующего kernel; это не новый grant и не автоматический счёт.
- Успешно использованный allowance не сбрасывается restart/top-up/новым request.
- При overrun новые запросы ресурса закрыты до штатного покрытия.
- Внутри admission единицы ресурса переиспользуют общий chunk, новая порция выделяется только при нехватке текущей.
- Неиспользованная chunk capacity возвращается, accepted claims остаются funded.
- Между API instances не нужен новый cache/service; текущая реализация не удерживает неиспользованные operation chunks между пользовательскими запросами.

## Дочерние workflow

- Три закрытые границы не создают второй operation request: существующая `billing_usage_run_links` (standard Apply/routed/V2 roots и technical children), typed `editor_effect_builds.final_run_id`, dedicated `v26_render` command config, который обслуживается отдельным Render accounting.
- Эти исключения не читают клиентское поле parent UUID.

- Для нового технического run внутри отдельной операции trusted SQL coordinator вызывает private `billing_link_operation_child(tenant,parent,child)` **до child seal в той же транзакции**.
- Функция проверяет active accepted parent request, tenant, actor, project и unsealed draft child; runtime API/worker/scheduler не могут напрямую выдать себе exemption.
- Child не может уже иметь собственный operation request или быть привязан к другому parent.
- Финансовое закрытие ждёт terminal runs и tasks всех связанных children.

- Generic leaf contract нельзя назначать специализированному parent, чей пользовательский успех определяется отдельным selected/final workflow: сначала нужна его native authority integration и trusted link.
- Неизвестное отношение родителя не угадывается из client config.
- Прямой paid child в таких цепочках не должен публиковаться отдельной пользовательской командой в rollout allowlist.

## Таблицы и поля

| Таблица | Поля и инварианты |
|---|---|
| `billing_operation_controls` | tenant_id PK/FK, enabled boolean NOT NULL DEFAULT false; прямой runtime DML закрыт |
| `billing_operation_requests` | id UUID PK; tenant_id/run_id unique + composite run FK; period_id composite FK; request_hash bytea32; state accepted/settled; created_at; reconcile_after nullable |
| `billing_operation_items` | id PK; tenant/request composite FK; task_id composite FK и unique; source_version_id composite FK; original_digest bytea32; policy_hash bytea32; policy_id FK; resource_code FK; quantity positive integer |
| Funding item | claim_id unique FK; временно NULL внутри admission, deferred scope trigger требует настоящий funded claim к завершению транзакции; tenant/resource/period/quantity согласованы |
| Result item | state accepted/consumed/released; result_id canonical FK только для consumed; proof_hash bytea32 для terminal; claim terminal quantity/proof/state должны совпадать с item |
| `billing_operation_children` | tenant/run PK, request composite FK, run composite FK; без second item/claim |

- Идентичность request и стоимость/source/policy item неизменны.
- Настоящий claim после первоначальной привязки неизменен.
- Terminal item неизменен.
- Индекс accepted queue поддерживает bounded reconciliation.
- Все 4 таблицы FORCE RLS с owner-only policies, без API/worker/scheduler DML.
- Definer функции fixed search_path; privileged aliases закрыты.
- `seal_v2_graph` явно owned photo_fn_owner и разрешён только scheduler; функция не наследует default PUBLIC EXECUTE.

## Функции и транзакции

| Функция | Доступ / поведение |
|---|---|
| `billing_prepare_operations(tenant,run)` | API/scheduler; отдельный opt-in, current actor/session/project auth; tenant UPDATE before domain locks; current auth повторно после funding/native seal ожиданий, до commit; SQL lock_timeout200ms и Go cold deadline200ms. Cold contention → BILLING_UNAVAILABLE503 |
| `billing_bind_operations(tenant,run)` | Только definer; fresh draft, closed context policy, leaf/source inventory, immutable quote, locks sorted resources, whole-batch deficit, leases/claims и items в одной TX |
| `seal_v2_graph(tenant,run)` | Scheduler; bind перед прежним seal; false legacy seal откатывает accounting под savepoint. Commercial denial остаётся ошибкой |
| `billing_link_operation_child(tenant,parent,child)` | Только definer; server-authorized same-actor/project техническая связь без второй оплаты |
| `billing_settle_operations(tenant,run)` | Только definer; terminal request graph/children, pinned cost, canonical proof и all-or-nothing financial closure |
| `billing_settle_next_operations()` | Scheduler; до 16 candidates; active lifecycle, SKIP LOCKED tenant; 30s backoff неполных proofs; изолированный savepoint, не поглощает другие SQL errors |
| `billing_check_operation_claim()` | Deferred constraint trigger; реальный claim и полное совпадение финансовой привязки/terminal proof |
| `billing_guard_operation_item()` | Защищает identity, source, cost, funded claim и terminal fields |
| `billing_guard_operation_terminal()` | Не позволяет восстановить settled/refunded root или linked child в runnable status |

Scheduler `RenderBatchFinalizer` выполняет operation reconciliation отдельным opt-in флагом `OperationBilling: true` в рабочей конфигурации. Ранний финансовый прогресс сохраняется даже при ошибке следующего render finalizer. Ни worker step, ни object-store I/O не проверяют billing повторно.

## Канонический успех, удержание и возврат

- Generic success требует succeeded leaf task текущих attempt/fence, его canonical result с единственным root document, верные SHA/producer provenance, verified/versioned/unexpired artifact, точную V2 task-root artifact link и `v2-adopt` inbox event.
- Если review_required, нужна decided review именно этого canonical candidate/hash; accept списывает, reject/skip возвращают.
- Pending/чужой candidate/missing adoption/expired или unverified artifact удерживают accepted funding.
- Успех без достаточного proof не превращается в возврат.

- Root run и все tasks должны быть terminal; все linked technical children тоже завершаются до финансового закрытия.
- Canonical результат может быть оплачен при partial failure другого leaf; failed/cancelled/skipped leaf возвращает свой claim.
- Cancelled root не списывает результат.
- Terminal financial closure атомарно обновляет существующий append-only resource ledger/claim/budget и новые item/request states.
- Повтор scanner/bind/settlement не добавляет debit.
- Scanner recovery не зависит от памяти процесса; выключение admission flag не отменяет уже принятые обязательства.

## Требования проверки и rollout

- Новые тесты `tests/integration/test_billing_operations.py`: реальные миграции в isolated PostgreSQL, authentic actor/session/project и production RLS/trigger gates, canonical output metadata и реальные scheduler SQL functions.
- Native pixels/GPU и fresh public compiler этими синтетическими fixtures не квалифицируются.
- Тест Go `TestIntegrationOperationColdAdmission` запускается harness явно без skip: scheduler pool, locked tenant, bounded error mapping, настоящий seal refusal и committed absence of leaked funding, затем настоящий Run.Cancel с повтором receipt, native task drain и единственным возвратом.
- Отдельный concurrent test наблюдает quota-lock wait через pg_blocking_pids, отзывает сессию во время ожидания и проверяет полный rollback admission.

- Положительные: единичный/batch debit, повтор bind/settle, новый request после прежнего, finite allowance, beta budget, partial success, root+child один debit, scoped review accept.
- Отрицательные: отсутствующая или другой-context policy, known deficit нескольких ресурсов, stale/unverified/expired/missing-link/adoption/fence proof, pending/wrong-candidate review, reject/skip, fail/cancel before drain, closed alias/runtime DML, mutation принятой стоимости.
- Конкурентные: exact request replay, settlement, refund и independent requests за последней paid единицей + finite allowance.
- Каждый сценарий сверяет conservation и ledger independently from mutable counters.

- Перед rollout завершить/drain старые unaccounted drafts, применить 02607 штатно, установить квалифицированные immutable context policies/admission parameters, проверить units<=chunk и snapshot ordinal/source contract, выдать ограниченные scoped test grants, включить только закрытый tenant cohort.
- Пользовательские продажи требуют отдельно approved benchmarks, operation offers/plan included grants, quote UI, Stripe test scenarios и legal/publication gates.
- Production БД этим этапом не мигрируется.


- Фактическая проверка от 21.10.2026:151 PASS общего billing regression.
- Последний финальный прогон после post-wait current-authority check и native Cancel extension —46 PASS (33 operation integration scenarios,2 operation contract tests,11 V2 regressions).
- Числа пересекаются.
- Go billing/v2input/workflow/run/v2render/db и 7 generator drift checks PASS;
- Python syntax и whitespace PASS.
- Рабочая БД не менялась, cohort и operation prices не включались.
