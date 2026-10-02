# Workflow: обязательное решение

Нормативная версия 2.2; детализация15/17. Расширяет существующие runs/tasks, не создаёт второй scheduler. Точные wire types заданы schemas.json, уточнения транзакций/closure/transport15–19 обязательны; прежние краткие формулировки не отменяют эти дополнения.

## Владельцы состояния

Ввести runs.kind=v2, snapshots.kind=v2, сохранив ВСЕ уже допустимые значения CHECK. v2_workflows содержит run_id, workflow_type и input_snapshot_id, но НЕ содержит state. FK snapshot NOT NULL; значение должно совпадать с runs.input_snapshot_id. Даже одиночный export является frozen input snapshot. Snapshot principal — действительный автор admission; device привязывается к отдельному device principal существующим ingest-контуром, не поддельному user.

Внешний JobStatus.state — runs.status. Нет отображения completed в ready. ready разрешён только для бизнес-ресурса (delivery item, публикация). Все v2 runs имеют failure_policy=fail_fast: completed_with_errors для v2 запрещён. Unsupported является явным результатом элемента плана, а не успехом отсутствовавшей операции.

| Переход | Единственный владелец | Условие |
|---|---|---|
| draft → queued | SealV2Graph | граф полностью сохранён, sealed, квота зарезервирована |
| queued → running | существующий scheduler | первая lease |
| running → waiting_review | FinalizeV2 | все необходимые задачи adopted, нужен review |
| running → completed | FinalizeV2 | все задачи adopted, review не нужен |
| waiting_review → completed | CompleteReview | решения сохранены, CAS run.revision |
| queued/running/waiting_review → cancelling | RequestCancel | блокировка run, cancel_requested=true |
| cancelling → cancelled | FinalizeV2 | leases завершены/отозваны, adoption закрыт |
| queued/running/waiting_review → failed | FinalizeV2 | терминальная ошибка, граф остановлен |

Terminal не изменяется. Повторный запуск — новый run и новое резервирование. Session review создаёт один child Apply и закрывает parent. Semantic/import review сохраняет batches, parent закрывается только отдельной CloseReview при remaining0 по15; GPU lease во время review отсутствует. Родитель completed означает завершённое планирование/решение, а не успешный дочерний Apply. API возвращает дочерний ticket при apply; UI наблюдает именно его.

## Компиляция

CompileV2(snapshot, policy, capabilities) — чистая функция, возвращает nodes, edges, declared_inputs, output_roles, reservation. `run_id=UUIDv5(tenant_id,"V2.Admit:"+actor_id+":"+Idempotency-Key)`, `workflow_id=UUIDv5(run_id,"workflow")`, ID узла UUIDv5(namespace=workflow_id, name=stage+":"+ordinal). `node_key` хранит то же каноническое `stage:ordinal` без leading zeros. SQL shell получает уже выведенный workflow_id; Go coordinator проверяет все три связи до записи. Полный snapshot digest, renderer, schema/model/calibration hashes участвуют в cache key.

| workflow_type | Граф | Adoption | Review |
|---|---|---|---|
| session | describe shards по 128 → materialize plan после canonical adoption всех shards | session_analyses + groups/members + CorrectionPlan | да, выбор групп запускает Apply |
| learning | fit → evaluate | learning_versions + lineage | нет; activate отдельная команда |
| capture | assess | alert/report + receipt linkage | нет |
| semantic | transfer на каждую цель → server plan assembly | intent + targets | да |
| delivery | prepare → child exports → assembly | items + immutable manifest | policy.require_review |
| interchange | parse → existing ingest → resolve proposals либо export | mapping report + draft proposals | import да, export нет |
| provenance | sign | manifest + подписанный artifact | нет |
| preview | существующий renderer над pinned recipe | preview link | нет |

Server assembly/parser выполняются как leased CPU tasks с собственным существующим/новым operation policy в пакете, не как HTTP background goroutine. Новые технические IDs v2.delivery.assemble, v2.interchange.import, v2.interchange.export, v2.semantic.assemble должны иметь отдельные schemas/policies до приёмки пакета; их exact contracts — operations.json и15: DeliveryAssemblyInput с slot shards, ParsedInterchange+InterchangePlan после ingest, InterchangePlan для export, EditIntent+TransferCandidate refs. v2.interchange.parse — отдельная fenced CPU операция с выходом ParsedInterchange. Максимум 5000 элементов, JSON manifest до 8 MiB, архив по reservation. Они не считаются ML operations.

## Транзакционные функции

AdmitV2(ctx, principal, project, request, key): auth/ACL → validate strict body → read qualified capability → freeze immutable snapshot вне TX → draft-shell TX с повторной проверкой tenant, consent и capability revision, блокировкой квоты и записью idempotency/run/workflow/tasks/edges/reservation → publish task-scoped transport objects вне TX → closure+seal TX → outbox/commit. Нет вызовов storage/Rabbit внутри TX. Отказ seal оставляет только draft shell и staged objects с TTL до retry/cleanup; queued run не возникает.

Начальный граф использует две DB-транзакции, разделённые публикацией transport objects. Сначала `photo.create_v2_draft_shell` после авторизации повторно проверяет tenant, право редактирования проекта, принадлежность snapshot и storage quota; атомарно создаёт run/task shell, edges и reservation со статусом `draft`. Затем компилятор публикует task-scoped input documents вне DB-транзакции. Наконец `v2input.SealGraphInTransaction` в одной DB-транзакции регистрирует все input closures и вызывает `photo.seal_v2_graph`. Последняя функция проверяет V2-допуск каждой операции (`operation_registry.v2_enabled`, по умолчанию false), renderer/backend/resource/output policy, готовность closure каждой задачи, ацикличность и квоту; после CAS draft→queued активирует корни и записывает один `v2.run.queued` в run_events/outbox. Ошибка последней транзакции откатывает регистрацию входов, seal и событие, оставляя draft shell для безопасного retry/expiry cleanup. `Admission.CommitGraph` вызывает ту же функцию в собственной транзакции. Одиночный `Admission.Commit` предназначен только для контролируемого расширения незапечатанного графа.

Для Session Analyze до API admission `freeze_v22_analysis_snapshot` повторяет CAS/ACL/selection preflight и копирует точный ordinal inventory selection в snapshot kind=v2 с UUIDv5(run, `analysis.snapshot`); повтор с тем же run проверяет равенство состава и `InputSnapshot.snapshot_digest`, а другой состав или digest даёт конфликт. Selection digest и worker InputSnapshot digest различны: первый подтверждает временный выбор, второй вычисляется по каноническому InputSnapshot без поля snapshot_digest и сохраняется в V2 snapshot. Компилятор берёт InputSnapshot из frozen row, не из текущей collection. После атомарного копирования V2 snapshot запечатывается: триггеры блокируют поздние insert/update/delete его items и повторное открытие snapshot; DELETE допускается только в активном fenced tenant purge после очистки объектов. Для V22 session начальный admission начинается `begin_v22_analysis_draft`: после auth и deterministic run/workflow UUID он создаёт draft run, workflow и quota reservation без задач, чтобы backend мог опубликовать Params/InputSnapshot/ConsentSnapshot/SessionPlanContext под существующим run. Immutable config хранит `context_sha`, а готовый context ArtifactRef разрешается по scoped документу после публикации. Повтор pending проверяет сохранённые config/snapshot/policy/quota и не зависит от истечения исходной selection; первый вызов повторяет preflight. После публикации отдельная fenced стадия материализует describe tasks и seal. Для API admission используется `begin_v2_admission`: в первой TX он связывает `(tenant,user,V2.Admit,key)` с JCS-хешем запроса и тем же run_id, затем создаёт shell. Состояние `pending` после crash означает продолжить публикацию и seal того же run только при точном совпадении сохранённого shell (snapshot, config, policy, tasks, edges, quota reservation); расхождение compiler output при том же request hash даёт conflict. Иной hash даёт conflict, истёкший отменённый draft — expired. Вторая TX seal атомарно фиксирует исходный `202 JobTicket {run_id,workflow_id,state:queued}` в idempotency record. Повтор после seal возвращает сохранённый ticket, не создаёт вторую квоту или событие. Raw `create_v2_draft_shell` — низкоуровневый compiler primitive; production API вызывает idempotent wrapper после auth/ACL.

`v2input.Coordinator.Admit` реализует порядок вызовов: Begin TX → `BuildInputClosure` для каждой задачи через fenced publisher вне TX → `SealGraphInTransaction` → `v2_admission_ticket`. При `replayed` publication и seal пропускаются; при `pending` компилятор обязан открыть тот же frozen cursor и использовать сохранённые deterministic run/task IDs. `v2_admission_ticket` выдаёт только completed 202 body при совпадении tenant, actor, key и request hash; отсутствие ticket после seal считается ошибкой, которую клиент может безопасно повторить с тем же ключом. Publisher не вызывается из retryable DB callback.

Draft shell держит run quota reservation один час. `photo.expire_one_v2_draft` выбирает истёкший незапечатанный V2 run, а `photo.expire_v2_draft` под блокировками usage→run→reservation повторно проверяет отсутствие lease/attempt, помечает blocked tasks отменёнными, освобождает только run reservation, переводит run в `cancelled` и записывает событие. Опубликованные ready transport artifacts проходят обычный GC, а pending upload с отдельным reservation — существующий `claim_document_cleanup` после подтверждённого удаления объекта. Maintenance подключает этот job только когда миграция V21 установлена. Seal и publisher используют тот же порядок блокировок usage→run→task.

AdoptV2(task_id, attempt, fence, result_id): блокировать tenant lifecycle → consent grants по UUID → run → task → owner resource в этом порядке. Проверить active tenant, run running, cancel=false, текущие attempt/fence, canonical_result_id, declared roles и exact digests; проверить весь документ и вложенные refs до регистрации результата. В одной TX вставить immutable rows/refs, consumed adoption key, outbox. Повтор того же canonical result — no-op; другой result — STALE_RESULT. Только trusted server выбирает tenant/project, actor и artifact ownership.

FinalizeV2(run_id): SELECT FOR UPDATE; проверить kind=v2; если cancellation — закрыть после fencing незавершённых задач; если permanent failure — failed; если все canonical outputs adopted — waiting_review/completed согласно таблице; иначе no-op. Терминальный переход, освобождение reservation и outbox в одной TX с уникальным settlement key=run_id.

ReviewBatch/CompleteReview(run, expected_revision, decisions): блокировки и ACL повторно; все цели принадлежат плану, исходные версии не изменились; отклонённые цели не попадают в дочерний run; admission дочернего run и запись решения атомарны. Повтор не создаёт второй Apply. Session manual references задаются для предыдущего analysis: group_id обязан принадлежать session; новый анализ применяет reference только при exact прежнем membership группы, иначе409 REFERENCE_GROUP_CHANGED; split/merge требует нового выбора.

Все существующие финализаторы Apply/preview/export явно исключают kind=v2. Миграционный тест перечисляет каждый consumer runs.kind и сохраняет старую матрицу поведения. Schema CHECK расширяется через полный фактический allowlist на текущем head, не через список из старой миграции.

## Обязательные проверки

Cancel против adoption; revoke против activate/adoption; lease expiry и поздний результат; два finalizer; crash после commit до publish; retry settlement; смешанный старый Apply и v2; частичная assembly; неавторизованный child reference. После каждой гонки: один canonical result, одна квота, нет опубликованных непринятых artifacts.
