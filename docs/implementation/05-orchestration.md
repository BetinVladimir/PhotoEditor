# 05. Оркестрация, транзакционные границы и восстановление

## Scheduler API

| Функция | Контракт |
|---|---|
| `CompileAnalyze(snapshot,options,registry)->Graph` | детерминированные node_key/edges; pair fan-out, fit body before color, aggregate barrier; cycle check |
| `CompileApply(snapshot,style,overrides,profile)->Graph` | explicit disabled/skipped nodes с pass-through refs; cull/HDR units; output ordinals стабильны |
| `CompileIngest/Download/Bulk(spec)->Graph` | только Go порождает downstream tasks, включая retries |
| `PersistGraph(tx,Graph)` | task/edges UNIQUE(run,node_key); большие fan-out чанками с graph build cursor; run не dispatch до graph sealed |
| `AdvanceGraph(tx,event)` | inbox dedup→validate DB canonical result→resolve edges→ready nodes→terminal aggregation→UI events |
| `DispatchReady(ctx,limit)` | tenant round-robin по persisted virtual finish time в scheduler state (см. ниже); SKIP LOCKED; quotas/resources; queued + outbox |
| `ReapLeases(ctx,now)` | running lease expired→retry_wait/cancelled/failed, fence++; release slot once; no direct Rabbit requeue |
| `ScheduleRetries(tx,task,error)` | ordinary attempts 1+3, 5/30/120s deterministic ±20% jitter; OOM separate budget max2, total automatic attempts<=6 |
| `Reconcile(ctx)` | repair stranded queued/outbox, terminal-without-event transitions, expired reservations, stale slots |
| `CancelRun(tx,run)` | cancelling; waiting/ready/retry nodes cancelled; active cancel_requested; no new children |
| `FinalizeRun(tx,run)` | blocking reviews→waiting_review; outputs+failures→completed_with_errors; zero accepted with errors→failed; all accepted→completed |

Добавить в migration 00006 `scheduler_tenants(tenant_id PK,virtual_finish bigint default0,last_dispatch_at)` с FORCE RLS, scheduler function-only. Выбирать минимальный virtual_finish среди tenants с ready task, повышать на cost unit resource class (media1,cpu2,archive3,gpu5), tie tenant UUID; максимум 1 GPU heavy, по tenant GPU лимиту. Preview приоритет выше batch, но после 5 previews подряд обслужить старейшую batch. Это starvation-free проектный алгоритм, не обещание одинакового wall time.

## Состояния

Task blocked→ready→queued→running→succeeded. running→retry_wait→ready при retryable; running→failed при исчерпании; nonterminal→cancelled по cancel. disabled→skipped(reason=disabled,pass_through=input); dependency failure→skipped(reason=upstream_failed,no output). Барьер различает эти случаи: disabled является допустимым входом identity, upstream_failed исключает item и отражается в summary. Review gate — отдельная запись/edge condition, не `task.state=needs_review`.

Run draft→queued→running→waiting_review→running→completed/completed_with_errors/failed. active→cancelling→cancelled; running повторно после review. completed immutable; осознанный reprocess создаёт новый run. `retry` допускается для failed tasks в failed/completed_with_errors run с тем же snapshot; run revision повышается, output collection сохраняется, успешные nodes не переделываются. Отменённый run автоматически не возобновлять: новый run с тем же snapshot по явному запуску.

Download queued→preparing→building→uploading→ready→expired. Любая nonterminal→cancelling→cancelled либо failed. Mixed volume phases отображать как earliest unfinished phase; ready только все volumes ready. Published collection status не зависит от processing run автоматически.

## Claim / commit

1. Publisher берёт unsent outbox batch коротким lease, публикует persistent envelope, mandatory+confirms. Mark sent только после positive confirm и отсутствия basic.return. Потеря соединения оставляет unsent, дубль ожидаем.
2. Consumer validates schema/operation/message size≤64 KiB. `claim_task(task_id,attempt,worker_id)` сверяет DB operation/inputs/lock/resource, queued, cancellation; row lock, attempt/fence++, lease60s. При mismatch command attempt stale ack без вычисления. В занятой running task дубликат ack: восстановление принадлежит DB reaper, не nack loop.
3. Heartbeat10s продлевает lease только при актуальном fence и state running. Lease loss запрещает commit; worker прекращает дальнейшие writes по возможности. DB heartbeat выполняется отдельным connection/thread, чтобы тяжёлый inference не блокировал продление.
4. Worker загружает immutable attempt objects, пересчитывает SHA и HEAD; локальные tmp не принимаются как output. `commit_task_success(task,attempt,fence,manifest)` блокирует task: lease>now, fence matches, cancel=false. Inserts artifacts/results, updates canonical pointer+succeeded, outbox task.succeeded. SQL timeout/unknown outcome →read task canonical by fence перед повторным commit.
5. ACK command только после SQL commit. Повтор после commit видит terminal и ack. Stale attempt output остаётся orphan и не доступен пользователю. `commit_failure` аналогично durable event/error, дальнейший retry решает scheduler.
6. Event-consumer доверяет DB accepted result, а не произвольным outputs из Rabbit. Inbox insert и graph advance одна tx. Reconcile повторяет advance из terminal tasks если event delivery задержан.

Task attempt numbering: dispatch envelope содержит `attempt=tasks.attempt+1`; claim устанавливает именно это число, увеличивает fence; повтор одного dispatch не увеличивает attempt. Reaper invalidates fence, следующий dispatch новый attempt. Нормальный retry счётчик не растёт от Rabbit duplicate. OOM retry сохраняет изменённый execution_profile; output hash/cache key включает profile/backend/precision.

## Графы

Analyze: verify/inspect/decode обоих входов → pair.register → independent features/masks/landmarks → pair.fit.body (если разрешён) → geometry.body для aligned before → pair.fit.color → pair.fit.beauty → pair.fit.defects → pair report. Geometry crop-aware не обучает body. Ошибка/непринятая пара блокирует aggregate; пользователь исключает в наборе и делает новый analysis snapshot, не меняет текущий. style.aggregate → style.validate → style.package → style acceptance. Validation может быть measured/needs_review, не обязана false-pass.

Apply: inspect/decode → features/color.stats → collection.group/cull → selection review → hdr.register/merge если bracket → denoise → masks/landmarks/depth → style.resolve → geometry.body (merge body+face) → image.color → series.measure/balance → image.local → portrait.beauty/detail → defects.propose/review/repair → dust → inpaint/background/generate/review → composition.plan→composition_render(geometry.crop|image.outpaint)→review → image.upscale → quality.check(master)→review→export.encode(master)→commit output asset/version/item. От master идут derivative resize→sharpen→watermark→encode→metadata.write→quality.check(export) и media.thumbnail/preview. Полная collection становится готовой независимо от отдельного download archive job; автоархив только при явном export option.

SeriesMatch needs stats после global ColorProgram: series.measure отдельный CPU node для каждого кадра, series.balance aggregate→per-item corrections, которые применяет image.local прежде semantic local. Не выполнять скрытый ColorProgram в scheduler. Disabled series identity. Masks следуют warp через mask.transform отдельный node; after crop/upscale обновляется transform chain. Для генерации mask/landmarks изменённого ROI invalidated и пересчитываются при downstream demand.

HDR group — один logical work item; source_versions список и reference primary. Unchanged memberships в all_change_selected добавляются сразу в output draft, но не published. Review candidates вне collection до принятия. Partial run список missing items содержит исходные ordinal/reason.

## Routing

`photo.commands.v1`: cpu.*→q.worker.cpu, gpu.*→q.worker.gpu, mail.*→q.worker.mail, io.thumbnail/io.preview/io.verify→q.worker.media, io.archive→q.worker.archive, collection.mutate→q.worker.collection. Не использовать перекрывающие bindings. Atomic operation и routing key различны (asset.verify→io.verify). Media inspect/decode могут идти cpu.* по registry, не два consumer queues. Storage cleanup не является image task и не проходит command exchange: единственный authority — maintenance leader с SQL claim/token fencing и exact object-version deletion.

Events exchange photo.events.v1→q.orchestrator.events; task terminal+progress. Dead exchange photo.dead.v1, отдельные q.dead.<class>. Durable quorum queues, GPU/archive prefetch1, concurrency1. Для poison schema reject(requeue=false), диагностический artifact; no infinite requeue. Delivery limit и dead-letter policy фиксируются конфигом брокера. Ack timeout >max op timeout+lease recovery buffer, начальный 4h для генерации при op timeout<=2h. AMQP heartbeat30s.

Progress ограничить одним событием/сек/task, допускает coalescing; terminal только durable outbox. UI progress по persisted completed units, нельзя выдавать 100% до publication-ready branch. ETA появится только после measured comparable operations.


## Ключи кэша и динамические узлы

`compute_cache_key` = SHA256(JCS({tenant_id,operation,operation_version,ordered_input_refs_with_hashes,params_hash,model_lock_id,profile_lock_id,renderer_version,backend,precision,execution_profile,seed,source_transform_hash,export_preset_hash,override_hash,asset_package_hash})). Только роли/параметры, значимые для данной операции, включаются в её manifest; manifest schema запрещает случайно выкинуть dependency. Смена color slider не инвалидирует face.detect, но смена decode/profile/model — инвалидирует. Reviews не кэшируются как автоматическое согласие для нового run: физический candidate может reused, пользовательское acceptance привязано к current run/candidate policy.

`CompileDownloadExports` строит один stable `export.encode/{ordinal:016}` node
на каждый item immutable selection. Task ID = UUIDv5(run ID,node key). Compiler
получает только уже hash-validated WorkingImage и locked target ICC SHA, сортирует
по ordinal и требует плотный диапазон 0..N-1. Ordered inputs всегда
`[working_image,working_rgb,alpha?]`; canonical params полностью разрешают format,
ICC, size/upscale, quality/444 и alpha/matte. Metadata на этом этапе только strip.
Compiler не даёт прав и не публикует документы: SQL repository обязан повторно
доказать provenance/readiness всех refs, а Publisher — создать immutable params.

SQL89 + `DownloadExportLoader.Load` реализуют read boundary перед compiler.
`download_export_context` возвращает только active download run/job, exact
selection rows и ready version-pinned JSON master. `download_export_inputs`
возвращает RGB/alpha только когда artifact создан той же exact successful
WorkingImage-producing task, присутствует вместе с master в её canonical task
result. Разрешённый список синхронизирован с operation registry v1 и включает
decode и все операции обработки, реально возвращающие WorkingImage.

Loader сначала полностью читает и закрывает оба SQL cursor, затем открывает каждый
master по exact object version, ограничивает JSON 1 MiB, сверяет bytes+SHA и строго
парсит WorkingImage. Nested refs должны один-в-один совпасть с SQL-attested roles;
лишняя/повторная alpha/RGB строка закрывает batch. Source work ICC и target ICC
берутся из trusted lock maps, preset config hash пересчитывается canonical JCS.
Только после этого вызывается CompileDownloadExports.

SQL90 и `DownloadExportCoordinator.Build` закрывают write boundary. Отдельный
60-секундный lease с heartbeat сериализует подготовку run, но корректность не
зависит от exactly-once: `download_export_plans` принимает только точный replay
UUIDv5/node/ordered refs/params. План регистрируется до object I/O. После этого
fenced backend-document publisher получает узкое право на `task_params` только
для зарегистрированного node незапечатанного download run. Каждый params artifact
повторно сверяется по SHA при materialization.

`PrepareDownloadExportTask` строит CPU/float32 profile из проверенных размеров
всех exact inputs и canvas; SQL повторно проверяет registry, документы, ready refs,
profile и immutable plan. Задачи создаются blocked. `seal_download_exports`
под row locks требует ровно snapshot.item_count plans и tasks, проверяет их
соответствие, одним commit переводит корни в ready, run в running+graph_sealed и
download_job в building. Частично опубликованный batch остаётся возобновляемым и
никогда не попадает в dispatcher. Scheduler запускает этот coordinator отдельным
bounded loop и включает его состояние в readiness/metrics.

SQL91–SQL93 продолжают тот же run через явные durable-фазы `encoding →
packaging → archiving`. После терминального encode barrier scheduler читает только
canonical `encoded_image` outputs, сохраняет реальные bytes и целевое расширение,
применяет stable volume planner и одной транзакцией создаёт все job items/volumes.
Лишь затем граф снова открывается для archive expansion.

`CompileDownloadArchives` создаёт UUIDv5 `archive.build/{volume:04}` только для
ZIP-томов. Params содержат locked snapshot ID/digest и exact ordered entries;
SQL заново строит ожидаемый JSON из persisted job items и требует полного
равенства. Каждый archive task получает success-edge от producer encode tasks.
Archive seal проверяет полное число ZIP-томов; single-only job завершается без
пустых архивных задач. Финализатор принимает ровно canonical `archive` и
`archive_manifest`, обновляет все volumes и только после общей полноты переводит
job в ready, run в completed и назначает 24-часовой TTL.

Динамическое fan-out после cull/face detection/generation variants создаёт только scheduler. Node key `stage/item_ordinal/object_id/variant/phase`, uniqueness включает run. `graph_cursor` хранит cursor expansion, `graph_sealed` относится к текущему expansion barrier; run terminal запрещён если есть незакрытые expansion records. Для этого migration00006 добавляет `graph_expansions(id,tenant_id,run_id,parent_task_id,kind,state,cursor,expected_count,created_at)` с UNIQUE(tenant_id,run_id,parent_task_id,kind), composite FK и FORCE RLS. Event consumer atomically records expansion; batches sealed only when all children persisted. Отмена исключает новые expansion batches.


## Глобальный admission control

`ReserveResources(tx,task,estimate,node)` locks physical node slots (sorted node/slot) и tenant quota row; проверяет суммы активных RAM/VRAM/scratch reservations плюс measured free headroom, выделяет GPU/archive slot и записывает resource_reservations вместе с dispatch. Иначе task остаётся ready с reason waiting_resource. Несколько scheduler процессов используют SKIP LOCKED+row lock, не локальные semaphores. Resource slots global function-only; lease синхронизирован task lease. Heartbeat продлевает обе записи атомарно. Commit/reaper освобождают reservation CAS, stale fence не освобождает чужой slot. При ошибке node free-disk report scheduler не overcommit; worker повторяет disk check непосредственно перед allocate.

CPU fallback после OOM использует оставшуюся общую retry попытку внутри max6, не добавляет седьмую. После исчерпания общего cap — failed RESOURCE_OOM; no infinite alternation CPU/GPU. Timeout фиксируется absolute deadline attempt, heartbeat не продлевает hard deadline.

## Адресная доставка и физический reservation (00024)

Обнаруженное противоречие прежней topology исправляется адресацией конкретному worker: общая class queue не гарантирует попадание на node, где уже зарезервированы ресурсы. `reserve_dispatch` теперь в одной транзакции выбирает enabled/fresh/qualified worker на зарезервированном node, сохраняет dispatch_worker_id и routing_key `worker.{worker_uuid}.{operation_routing_key}`. `claim_task` отвергает другого worker до claim; underlying resource/claim helpers недоступны service roles напрямую. Payload TaskCommand не меняется; tenant/run/attempt/fence остаются canonical DB checks.

Привилегированный `PHOTO_ROLE=bootstrap-queue` с PHOTO_WORKER_ID и PHOTO_WORKER_CLASS создаёт `q.worker.{uuid}.{class}` и соответствующие bindings к прежнему commands exchange. Без этих двух settings выполняется общая bootstrap topology для events/mail/dead и legacy drain. Runtime worker не получает configure permissions, read permission ограничивается собственной очередью. Python Transport по умолчанию выбирает адресную очередь; targeted=False только для явного legacy drain.

Rollout: создать worker queues и permissions до enabling registration/нового scheduler; применить миграцию и перезапустить service pools. Уже queued legacy rows с NULL dispatch_worker_id можно дренировать старым consumer либо дождаться60s reservation expiry/reaper; новые dispatch всегда адресные. Старые class consumers не получают новых prefixed messages. Не удалять старые очереди с сообщениями автоматически. Unit routing tests проходят; SQL/broker rollout ещё не подтверждён на недоступных dev-сервисах.


## Барьер расширения после решения defects review

`workflow.ReviewExpansionGate` реализует review.DefectGate и выполняется внутри savepoint PersistDefectsAuthorized. Один SQL CTE проверяет сохранённый decided review (tenant/run/id/revision/decision_hash/decision_route), активный неотменённый run и task_id; вставляет open graph_expansions с kind=`review.defects/{review_id}`, parent_task_id=proposal task. Expected_count=1 для repair,0 для base/skip: это число новых repair nodes данной expansion, не число всех downstream nodes. Cursor оставлен NULL для scheduler. Уникальность expansion не маскируется ON CONFLICT: повторный service вызов должен возвращать idempotency response, а несовпадение — откат.

Run получает graph_sealed=false,status=running,revision+1 в той же SQL statement. Нельзя считать такое решение завершённой обработкой или поставить repair ready до материализации параметров/selection artifact и dependencies. Планировщик expansion должен создать ровно один defects.repair для repair; для base привязать прежний image к продолжению, для skip отметить item исключённым. После записи всех branch bindings закрыть expansion и восстановить graph_sealed только при отсутствии других open expansions. Эти compiler/advance действия пока не реализованы; gate не установлен в публичный service/runtime, чтобы не открывать неисполняемый пользовательский путь.

Unit tests с pgx double проверяют три маршрута, привязку immutable decision, незакрытый barrier, invalid plan и SQL failure/zero updated rows. Они не выполняют SQL: реальная совместимость CTE, locks/RLS и rollback требует PostgreSQL integration. В этой среде Docker остаётся недоступным; live проверка не заявляется.


### Материализация repair params после review

`review.BuildRepairDocument(VerifiedProposal,DefectSelection,selectionRef,resolvedRef,ProtectionMask,alphaRef?)` создаёт канонический JSON DefectRepairParams и полный список declared inputs. Selection должна быть загружена из неизменяемого decided review; helper не принимает решение за пользователя. Версия/revision>=2, exact proposal/image/label refs, известные уникальные непересекающиеся candidate IDs, непустой accepted и SHA256 canonical selection должны совпасть с опубликованным selectionRef. Для base/skip helper не вызывается.

ProtectionMask обязательна, protected=true, Canvas/TransformChain совпадают с proposal; confidence/selector/model lock проверяются. Params включают image,proposal,review,resolved,protected. Inputs: эти JSON refs плюс source_pixels,label_map,protection artifact и alpha при наличии. Список дедуплицируется по artifact ID, несовпадающие SHA для одного ID запрещены, порядок UUID детерминирован. Params<=1MiB; JSON не содержит bytes или object keys. Alpha и resolved/protection provenance проверяет service по источнику; helper проверяет content shape/binding, не repository ownership.

Go unit tests сравнивают Params побайтово с Python DefectRepairParams fixture и проверяют declared inputs; Python tests повторно валидируют selection/params и SHA между ними. Файлы fixture расположены в backend/internal/review/testdata. Публикация JSON в object store, регистрация artifact rows, scheduler insertion/expansion completion остаются незавершёнными. Наличие RepairDocument не означает готовую исполняемую задачу.


### Публикация backend JSON для expansion

`artifactjson.Publisher.Publish(ctx,Scope,raw)` реализует protocol Reserve→PUT/recover→Complete для task_params/review_selection. Scope={tenant,run,owner,purpose}; repository обязан проверить ownership и сохранить стабильную reservation до object IO, отвергать другой SHA для той же scope, fencing Complete по token. JSON object<=1MiB валидируется RFC8785 до Reserve, публикуется в canonical bytes. Key генерируется `tenants/{tenant}/runs/{run}/documents/{artifact}`. Ready replay не загружает объект повторно, но проверяет возвращённые ID/SHA. ObjectStore IO вне SQL transaction.

При ErrExists либо неопределённой storage error Publisher вызывает `objectstore.RecoverImmutable`: HEAD проверяет ожидаемые size/type/SHA и непустую version, затем GET именно этой версии читает до EOF и сверяет actual SHA. Это предотвращает принятие объекта по одним пользовательским metadata. Метод ничего не перезаписывает/не удаляет. Cancellation не порождает новую reservation; следующий вызов должен продолжить прежнюю. После успешного PUT/recovery проверяется exact object contract, затем Complete. SQL failure оставляет объект для восстановления по той же reservation.

Реализованы publisher/ports, MinIO recovery method и controlled unit tests (в том числе HTTP S3 stub с повреждёнными bytes при корректных metadata). SQL Repository, pending-document cleanup и startup wiring ещё отсутствуют; поэтому это пока не доступный пользователю upload endpoint. Live MinIO integration skipped без сервиса; protocol tests не заменяют полный DB/S3 crash-recovery прогон.


### SQL lifecycle backend_documents (миграция41)

`artifactjson.SQLRepository` вызывает reserve_backend_document и complete_backend_document через отдельные autocommit statements photo_scheduler. Table backend_documents доступна только trusted function owner с FORCE RLS; API/worker не получают прямых прав. Unique scope=(tenant,run,owner,purpose). Для review_selection owner=decided defects review; для task_params сейчас owner=open review.defects expansion (ровно один repair node). Это не универсальный owner для произвольного fan-out; расширять ownership отдельно при реализации других compiler stages.

Lock order usage→run→document. Reserve проверяет активный неотменённый run/владельца, SHA/bytes/bucket и квоту storage+reserved; новая запись резервирует bytes один раз. Pending retry сохраняет artifact_id и меняет token, поэтому старый Complete не сможет завершить запись. Ready replay проверяет наличие ready artifact с exact version/SHA. Конкурентные retries одного scope допустимы, но compiler должен координировать свои leases, чтобы не вытеснять токены бесконечно.

Complete повторяет scope/owner/run checks, требует актуальный token и exact metadata, создаёт ready artifacts row с generated key/version/application-json, переводит reserved→storage один раз и отмечает документ ready. Повтор того же complete идемпотентен. SQL доверяет storage attestation от привилегированного publisher; actual SHA проверяется PutImmutable/RecoverImmutable до вызова. Никакой object IO внутри SQL transaction.

SQLRepository и integration test добавлены. Тест готов проверить стабильный ID, stale token rejection, content conflict, ready replay, quota once и чужой tenant, но без PostgreSQL он skipped. Миграция41 не live-проверена. Cleanup abandoned reservations/objects, compiler startup и полный end-to-end остаются в работе; пока publisher не включён в production scheduler.


### Очистка незавершённых backend documents (миграция42)

Добавлены cleaning/cleaned, cleanup_token/lease_until/after; default grace3h. Claim выбирает только pending/cleaning/cleaned у cancelled/cancel-requested/terminal run и без artifacts row. После lock usage→run→document все predicates проверяются повторно. Ready никогда не становится cleanup ticket. Cleaning навсегда закрывает reservation для Reserve/Complete; ранее созданный PUT может закончиться в S3, но не стать ready в БД. Эти отказы добавлены в ещё не применённую в данной среде миграцию41; version-state CHECK получил явное имя для миграции42.

Claim lease60s; DocumentCleaner проверяет bucket и все UUID, вызывает PurgeDocument с таймаутом40s, и лишь после успешного purge вызывает Finish с5s. Object key строится из tenant/run/artifact, не принимается от клиента. Общая exact-key purge реализация используется и существующим PurgeAttempt: abort multipart, только совпадающие keys/версии (до64 за проход), повторный list подтверждает отсутствие. Finish освобождает reserved bytes только из cleaning; cleaned tombstone остаётся и проверяется снова через24h для поздних S3 completions. Готовые/канонические артефакты этим механизмом не удаляются.

Go DocumentCleaner/SQLDocumentCleanup готовы; controlled tests проверяют отсутствие Finish при ошибке purge, неверном bucket/UUID или отсутствии ticket. SQL42 и реальные delete-race сценарии пока не проверены на PostgreSQL/MinIO. DocumentCleaner ещё не подключён к циклу maintenance; основной runtime пока запускает только OutputCleaner. Общий GC ready artifacts остаётся отдельным незавершённым требованием.


### DocumentCleaner подключён к maintenance runtime

Maintenance.Run теперь запускает RunJobs с OutputCleaner и DocumentCleaner в одном последовательном цикле. На каждом проходе выбирается следующая очередь, включая случаи ошибки/пустой очереди; one active job, timeout50s, idle5s либо100ms после очистки. Раздельные LoopState хранят readiness, cleaned и failures. HTTP readiness требует обе Ready=true плюс доступность DB/storage; до первой проверки обеих очередей false. Success другой очереди не очищает чужой failure status. Shutdown отменяет активную job и сбрасывает обе Ready.

Startup требует claim/finish функций обеих очередей, то есть миграцию42. Новые document counters отделены от прежних output counters. RunLoop остаётся совместимым wrapper для одной job. Race tests подтвердили поочерёдность, отсутствие потери failure state, recovery, cancellation и запрет общей LoopState у разных jobs. Live startup/purge/SQL42 ещё не выполнены без dev dependencies; это подключение к коду runtime, не production qualification.


### Материализация выбранной ветви defects

`workflow.MaterializeDefectBranch(ctx,ContractPublisher,DefectExpansionInput)` связывает verified proposal, saved selection и publisher. Input содержит tenant/run/expansion/review IDs, route, verified proposal, сохранённую selection, resolved ref, resolved origin image ref, protection и alpha. Проверяет версию/revision/известность/непересечение списков для всех routes. Base возвращает exact прежний image ref; skip не возвращает image; оба не публикуют новые JSON и не создают repair.

Repair сначала валидирует bindings без IO, затем публикует canonical selection под scope owner=reviewID, строит repair params со ссылкой на фактический опубликованный selection artifact, публикует params под owner=expansionID. Возвращает refs/declared inputs только после обоих успешных шагов. Ошибка второго upload не возвращает runnable branch; retry использует те же reservation scopes и canonical bytes. Saved selection/expanded source берутся из доверенного repository, не HTTP request.

Исправлена зависимость BuildRepairDocument: обязательный resolvedImage — exact ResolvedStyle.params.image. Repair worker читает этот origin image, если текущий image уже изменён предыдущими этапами. Поэтому он включён в declared inputs наряду с пикселями/label/protection/alpha. Проверкa источника resolved принадлежит loader. Тест подтверждает отдельный origin ref в input set.

MaterializedDefectBranch ещё не является SQL task: compiler обязан повторно проверить lease/run/canonical decision, записать task или base/skip binding и закрыть expansion одной транзакцией. Эти repository claim/finalize и запуск в scheduler пока не подключены. Controlled publisher tests подтверждают последовательность, ref binding, repeatability, отсутствие публикации для base/skip и отказ invalid review до IO.


### Подготовка SQL task plan для repair

`workflow.PrepareDefectTask(runID,reviewID,canvas,MaterializedDefectBranch,[]ArtifactSize)` принимает только repair route и метаданные ready artifacts, загруженные доверенным repository. Требует ровно один metadata record для каждого declared input и params, exact ID/SHA, positive bytes, params<=1MiB, selection среди input refs и отличный от params ID. Duplicate/missing/extra metadata и overflow safe integer отклоняются. Canvas<=200MP. Ownership/state при реальной вставке следует повторно проверять в БД.

Profile cpu/float32/tile256/max_pixels200MP, RAM2GiB, VRAM0. Output budget=12*pixels+8192+2MiB (float32 RGB NPY и два control manifests). Scratch=output budget+сумма bytes всех inputs и params; streaming JSON намеренно также учтён консервативно. Это admission estimate, не измеренный universal peak: worker по-прежнему ограничивает отдельную repair ROI4MP и не меняет качество ради прохождения бюджета. Большие ROI/memory qualification остаются отдельной задачей.

Operation/version/resource_class/timeout берутся из registry defects.repair; model должен отсутствовать. NodeKey=defects.repair/review/{reviewID}; UUIDv5 namespace=runID даёт стабильный task ID. InputDigest=SHA256 JCS {version:task_input_v1,operation,operation_version,params,inputs_sorted_by_UUID}; это identity входов task, не полный compute cache key из раздела cache. PreparedDefectTask содержит bytes execution_profile и refs, не выполняет INSERT или dispatch. Claim/finalize expansion остаются незавершёнными.

Tests подтверждают устойчивость при перестановке inputs, разные task IDs разных runs, наличие всех input sizes, bounds/overflow, отсутствие подмены selection на params и profile, принимаемый ParseResourceEstimate. Live dispatch/worker allocation этим unit gate не подтверждаются.


### Аренда defects expansion (миграция43)

graph_expansions получила expansion_token/lease_until (оба NULL либо оба заполнены) и next_expansion_at. claim_defect_expansion выбирает open review.defects expansion с due time, неистёкшей чужой аренды нет, run активен/не cancelled/не sealed, review decided с route/hash и parent task canonical succeeded defects.propose. После hint выполняется lock run→expansion, SKIP LOCKED и повторная проверка predicates. Возвращает UUID token, lease60s, tenant/run/review/parent IDs, decision hash/route и сохранённую selection.

renew продлевает только текущий неистёкший token на активном run; release снимает только свой token и ставит next attempt через5s. Просроченная аренда может быть захвачена новым scheduler с другим token; старый процесс не может продлить/освободить новый claim. Функции доступны только photo_scheduler, прямых новых прав на таблицы нет. Токен необходимо повторно проверять при будущем finalize, иначе lease сама по себе не защищает INSERT.

SQLExpansionLeases реализует Claim/Renew/Release. Empty queue→nil,nil; selection<=1MiB проходит canonical strict decode, envelope IDs/route/hash/version проверяются, semantic proposal binding выполняется последующим loader/materializer. Адаптер ещё не запускает heartbeat и не вставляет tasks. Claim/finalize runtime, bounded failure policy и atomic completion остаются незавершёнными. SQL43 не проверена на живом PG; тесты adapter используют controlled row results и не доказывают concurrency/RLS.


### Атомарная фиксация materialized branch (миграция44)

`SQLExpansionLeases.Materialize(lease,branch,task?)` сериализует точный JSON payload route/decision_hash/base?/selection?/task?. PreparedDefectTask имеет wire tags; execution_profile — JSON object, не base64. Адаптер сверяет route, стабильный task ID/node key, task params/inputs и input digest, SHA saved selection и resource estimate. Для base/skip task отсутствует, base ref совпадает с saved review image. Вызывает scheduler-only materialize_defect_branch(expansion,token,payload).

SQL блокирует run→expansion, проверяет активность, unsealed, open state, неистёкший token и immutable decision. Repair требует ready backend_documents правильных owner scopes для params/selection, ready tenant-owned input artifacts с exact SHA, уникальные inputs, registry timeout и достаточный resource profile по текущим размерам. Все numeric estimates ограничены safe integer. Вставляет blocked defects.repair task с причиной awaiting_continuation и accepted edge от proposal. Base проверяет ready image; skip не создаёт task/image. В одной транзакции добавляет append-only defect_branches payload и переводит expansion в materialized, очищая lease. Повтор с тем же token/payload возвращает true без повторного INSERT; другое содержимое не принимается.

**Materialized не равно closed.** graph_sealed остаётся false, repair blocked. Следующий continuation compiler должен связать последующие этапы с repair output/base либо исключить item, затем закрыть barrier и разрешить dispatch. Это предотвращает запуск неполного Apply. Scheduler loop/loader/heartbeat/continuation ещё не завершены. SQL44 пока не проверена на живом PostgreSQL; Go tests проверяют wire, digest/identity и stale-return handling через controlled query port, не реальную атомарность БД.


### Обработчик expansion lease/heartbeat

`ExpansionProcessor.ProcessOne` связывает Claim→initial Renew→Build с heartbeat→Materialize. Defaults: claim5s, renew каждые10s с timeout5s, общий Build/finalize budget5min. Тайминги можно уменьшать в тестах, но не увеличивать сверх этих bounds. Первое renew обязательно до Build. False renewal либо ошибка БД отменяет context Build через cancel cause; context-aware artifact IO должен прекратиться. Ошибка/timeout не передаёт частичный результат в Materialize.

Heartbeat отменяется и join выполняется **до** final SQL (timeout5s), чтобы успешный commit, очистивший token, не вызвал ложную потерю аренды в параллельном renew. SQL finalize всё равно проверяет актуальность token/expiry. При любой незафиксированной попытке выполняется best-effort Release со своим token и отдельным ограниченным5s context; после successful Materialize release не вызывается. При неопределённом исходе commit SQL same-token/payload replay остаётся безопасным, а уже materialized row не изменится от Release.

Race tests покрывают renew false/error→cancellation/no commit, initial lease loss, work deadline, остановку in-flight renew перед finalization. Они проверяют управление goroutines через controlled leases. Processor не установлен в scheduler.RunScheduler: сначала нужны DB-backed source loader/size lookup и continuation compiler. Полный SQL/objectstore pipeline остаётся незавершённым.


### Доступ source loader к артефактам (миграция45)

Reviews получили nullable pair resolved_artifact_id/proposal_report_artifact_id с tenant-composite FK и immutable trigger. Новые defects reviews должны фиксировать эти ссылки при INSERT из graph context; прежние записи без context не получают выдуманный resolved/report и не загружаются source loader. Создание таких fully-bound reviews в compiler ещё требуется.

Scheduler-only defect_expansion_context проверяет действующую lease, активный unsealed run, exact immutable decision hash, canonical succeeded style.resolve resolved_style и defects.propose defect_propose_report данного run/parent. Возвращает ID/SHA контекстных JSON. expansion_artifact(expansion,token,id,sha) выдаёт metadata только ready artifact того же tenant при активной lease; producer role/operation/canonical-result membership возвращаются отдельно. Это внутренний scheduler read, не разрешение клиенту на произвольный tenant artifact. Source loader обязан требовать нужную provenance для каждого типа.

`ExpansionArtifacts.Context/Lookup/JSON` проверяют refs, bucket, version и SHA metadata; JSON<=1MiB читается по exact version до EOF, пересчитывается SHA исходных bytes и проверяется RFC8785 (включая duplicate keys). Reader закрывается при ошибке. JSON reader не заменяет typed semantic validation WorkingImage/ResolvedStyle/report — следующий loader должен связать эти контракты и проверить происхождение.

Controlled tests проходят для exact version/ref binding, corruption/duplicate JSON/size limit, чужого bucket и отсутствующего объекта. SQL45/tenant RLS/canonical provenance queries не live-проверены без PostgreSQL. Это общий доступ к артефактам для будущего loader; полный Build ещё не подключён к scheduler.


### Go WorkingImage validation для source loader

`review.ParseWorkingImage(raw)` строго разбирает<=1MiB canonical-compatible JSON, требует все wire fields (включая alpha:null), отклоняет unknown/missing/null primitives. Pixels/alpha refs, Canvas dimensions/orientation/source SHA/space_id, dtype и известная пара profile/encoding проверяются; рабочее изображение ограничено200MP. Pixel arrays не читаются. Source(ref) возвращает DefectSource с отдельной копией bytes chain.

TransformChain<=128 steps и exact hash SHA256 JCS{steps}. Проверяются continuity From/To, одинаковый source SHA каждого шага, valid_region и конечный target canvas. Discriminated matrix/crop/resize/dense_warp не допускают чужих variant fields. Matrix:9 finite values<=1e12, normalized determinant>=1e-12 и отсутствие horizon crossing на corners. Crop: rect внутри source и размеры равны target. Resize только half_pixel. Dense inverse_field — valid ArtifactRef (поле не загружается, значения проверит consuming worker). Canvas внутри chain сохраняет portable safe-integer правила Python; текущий target дополнительно имеет platform200MP cap.

Пять общих Python-generated image fixtures проверяются обоими языками. Go tests отдельно изменяют hash-consistent geometry для singular/horizon/source-change/crop/resize/target/continuity ошибок; это semantic checks, не только отказ по старому hash. Source SHA/provenance самого image artifact по-прежнему проверяет ExpansionArtifacts.JSON перед parsing. ResolvedStyle/report semantic loader и полноценный ExpansionBuild ещё не готовы.

### Проверка привязки resolved style при загрузке expansion

`review.ParseResolvedBinding(raw) -> (ResolvedBinding, error)` проверяет JSON до
1 MiB, точный набор верхних полей, обязательные nullable поля ResolveParams,
валидность ссылок, режим и привязку group/strict OOD. SHA-256 вычисляется над
каноническим полным телом без `hash`; непрочитанные численные модули также входят
в хеш. Неизвестные/повторяющиеся поля и изменённое тело отклоняются.

Это проекция для загрузчика, а не замена полной модели ResolvedStyle: численные
ограничения модулей проверяет worker. Загрузчик обязан отдельно подтвердить
канонического производителя style.resolve и готовность tenant-owned артефакта.
Функция пока не подключена к runtime expansion; загрузка report/protection и
компилятор продолжения графа остаются незавершёнными. Python fixture проверяет
совместимость канонического представления с существующей worker-моделью.

### Привязка protection из DefectProposeReport

`review.ParseProposalProtection(raw, proposalRef, source)` читает точную модель
отчёта размером не более 1 MiB. Проверяются версия алгоритма, ссылка proposal,
исходное изображение, геометрия и каноническая цепочка преобразований protection,
флаг protected, диапазоны confidence/diameter, обязательные nullable поля,
режим и ссылки масок лиц, допустимые причины и предупреждения отчёта.
Возвращается типизированная ProtectionMask. Проверка не заменяет SQL-аттестацию
канонического производителя и проверку байтов артефакта в ExpansionArtifacts.JSON.
Пиксели масок проверяются worker; runtime source loader пока не подключён.

### DefectBranchBuilder

`workflow.DefectBranchBuilder.Build(ctx, lease)` реализует callback для
ExpansionProcessor: через lease-bound reader загружает WorkingImage и проверенный
proposal; для repair читает SQL-attested context, resolved binding, origin image,
proposal report и metadata масок. Проверяет одинаковые color/source bindings,
канонические producer/role и запрет repair для resolved skip. Затем публикует
selection/params через MaterializeDefectBranch, перечитывает размеры всех inputs
и params и возвращает PreparedDefectTask. Ошибка не возвращает частичную задачу.
Для base/skip чтение style/report и публикация не нужны; сохранённый proposal и
решение по-прежнему проверяются. Reader обязан обеспечивать проверку байтов и
принадлежности tenant; тестовый reader проверяет композицию, не заменяет live S3.

Runtime scheduler пока не устанавливает этот callback: нужны создание полных
review context и продолжение графа после materialized barrier. SQL20–45 и live
объектное хранилище для этого пути ещё не проверены в текущем окружении.

### Создание полного defects review

Migration 46 добавляет `photo.create_defect_review(tenant,run,proposalTask,image,resolved)`
для photo_scheduler. Compiler передаёт собственный bound context. Функция блокирует
run→task, требует успешную canonical defects.propose v1, ровно по одному ready
candidate/report output, image в declared inputs и canonical style.resolve того
же run. Unique partial index ограничивает defects review одной записью на task.
Повтор с тем же неизменяемым контекстом возвращает review ID (включая decided);
конфликт возвращает NULL. Новый pending review и перевод активного run в
waiting_review/unsealed выполняются атомарно, revision повышается один раз.
Для cancelled/terminal run новый review не создаётся. Legacy неполные review
автоматически не дополняются. `DefectReviewCreator.Create` — Go adapter функции.

SQL не читает JSON из S3: семантическая привязка image/proposal/report остаётся
обязательной в loader/decision service. Runtime compiler ещё не вызывает creator.
Проверка Docker повторена: недоступен; migration 46, RLS, конкурентные повторы и
rollback на живой PostgreSQL пока не подтверждены. Unit tests adapter не доказывают
эти свойства SQL.

### Runtime expansion в scheduler

RunScheduler теперь принимает objectstore.Config и подключает ExpansionProcessor
с DefectBranchBuilder, SQLExpansionLeases, ExpansionArtifacts и artifactjson.Publisher.
Один отдельный последовательный RunExpansionLoop не задерживает dispatch/recovery
при S3 IO. Ошибка делает expansion readiness false, увеличивает failures и даёт
5s backoff; успешная материализация увеличивает отдельный счётчик. На остановке
context отменяется, текущая обработка завершается до закрытия DB pool.
Startup требует функции SQL41/43/44/45 и готовое versioned storage. Scheduler
теперь требует S3_ENDPOINT/S3_BUCKET/S3_ACCESS_KEY/S3_SECRET_KEY (и S3_REGION).
Health readiness учитывает оба цикла и storage; live не проверяет зависимости.

Это runtime до стадии materialized, не полное продолжение apply: созданная repair
задача остаётся blocked, expansion не закрывается, graph_sealed не устанавливается.
Creator полных reviews и continuation compiler ещё не установлены в advancement.
Без live PostgreSQL/S3 проверена только компиляция и controlled-loop cancellation /
error recovery; production readiness всей системы не заявляется.

### Планирование AdvanceGraph

`PlanAdvancement(AdvanceSnapshot, Registry)` валидирует DAG и полный набор состояний,
затем возвращает детерминированную топологическую последовательность AdvanceChange.
Ready разрешён только при sealed graph, отсутствии open expansions, разрешённых
зависимостях и BindingsReady. Последнее означает проверенные compiler/repository
ссылки input/params, не просто успешный predecessor. Pending review оставляет узел
blocked. Неуспешная обязательная зависимость порождает skipped/upstream_failed с
распространением по цепочке в одном плане. Disabled pass-through требует успешного
источника даже для terminal edge; ссылки на артефакты planner не придумывает.

При отмене blocked/ready/retry_wait переходят в cancelled, queued/running получают
cancel_requested, terminal не меняются. План не изменяет входной snapshot. Для
записи нужен ещё repository AdvanceGraph с блокировкой run, повторной проверкой
состояний и атомарной записью transitions/inbox/events. Он пока не реализован;
planner сам не закрывает expansion и не запускает созданную repair-задачу.
AggregateRun теперь отдаёт waiting_review для blocking review и unsealed graph;
отмена сохраняет приоритет. Unit tests покрывают барьеры, review, отсутствие
bindings, disabled/failure propagation, отмену и стабильный порядок.

### Атомарная активация связанного узла

SQL47 `activate_bound_task(task,digest,params)` использует общий lock_task порядок,
сравнивает immutable input digest/params, проверяет blocked/ready, отсутствие
cancel/terminal_reason, sealed graph и отсутствие expansions вне closed. Ready
JSON params и каждый exact SHA input должны принадлежать tenant и не быть expired.
Зависимости проверяются из DB: terminal допускает любое terminal состояние;
success требует succeeded/canonical либо skipped/disabled; accepted/rejected
дополнительно требуют непустой набор reviews, все decided с нужным решением.
Только после этих проверок выполняется blocked→ready; одинаковый ready replay
возвращает true. Очередь/outbox создаются позже существующим resource admission.

`AdvancementRepository.Activate` вызывает функцию по compiler BoundTaskActivation.
False означает stale/ineligible и запрещает dispatch. Awaiting_continuation repair
не проходит terminal_reason проверку, пока continuation не связан. Этот primitive
не заменяет весь AdvanceGraph: inbox, disabled pass-through persistence, propagation,
закрытие expansions и compiler binding ещё нужны. SQL47 не проверена live; тесты
Go подтверждают лишь CAS аргументы и обработку результата/ошибок.

SQL47 уточнена до применения: success-предикат dependency проверяется через
`IS NOT TRUE`, поэтому skipped с NULL terminal_reason не становится успешным
из-за трёхзначной логики SQL. Добавлен rollback-only PostgreSQL integration test:
NULL/upstream_failed отказ, disabled pass-through, digest mismatch, unsealed
barrier, ready и idempotent replay через роль photo_scheduler. Без
PHOTO_TEST_DATABASE_URL тест явно skipped, это не подтверждение live SQL.

### CompileIngest

`CompileIngest(IngestSpec, Registry) -> IngestPlan` компилирует batch immutable
snapshot items (1..10000) в verify→inspect→decode для каждого ordinal. Ordinal
0..2^53−1 уникален; порядок входного списка не влияет на результат. Node key
`asset.<stage>/<ordinal padded to 16 digits>`; task UUIDv5 namespace=run ID.
Одинаковые source refs с разными ordinals сохраняются отдельными work items.

Каждый узел явно объявляет source artifact, поскольку nested reference в JSON
не предоставляет gateway доступ. Params соответствуют SourceParams/DecodeParams:
размер 1..512MiB, suffix allowlist, явный nullable assume_profile, analyze, tile
256/512/1024 (default512). Нет неявного назначения профиля при его отсутствии.
Реестр обязан содержать v1 CPU backend; verify класс media, inspect/decode CPU.
Параметры представлены canonical JSON, не публикуются во время компиляции.

Plan не является готовым run: нужны проверка snapshot ownership, публикация params,
оценка ресурсов с учётом metadata, PersistGraph, привязка выходов и регистрация
итоговых asset/version. Ingest API и thumbnail/preview continuation здесь пока
не реализованы. Тесты проверяют стабильность ключей/IDs и rejection некорректного
snapshot batch; worker decode/quality алгоритмы не менялись.

Graph.Order использует lexicographic min-heap в алгоритме Kahn: O(V log V + E),
вместо повторной сортировки всего ready списка. Детерминированный порядок сохранён.
Тест графа root→10000 leaves→join проверяет все зависимости и тот же результат
после перестановки nodes/edges. BenchmarkGraphOrderWide измеряет 10000 независимых
узлов локально; это не SLA обработки изображений и не замена live DB fan-out тесту.

### Ресурсы ingest-задач

PrepareIngestTask проверяет SHA опубликованных params, canonical params против
повторно скомпилированного узла, registry и exact source size/ref. Decode требует
canonical inspect metadata того же source; metadata provenance проверяет caller.
RGB output бюджет =12*pixels+2MiB, alpha добавляет2*pixels; scratch включает source,
params и output. Verify/inspect имеют JSON output1MiB. Неизвестные размеры до
inspect резервируют pixel_count=200MP. RAM raster=1GiB+128*tile², RAW=32*200MP+1GiB:
это консервативная политика admission, не измеренная верхняя граница LibRaw.
Квалификация decoder/backend на реальных файлах и enforcement памяти остаются
обязательными; один расчёт не разрешает worker operation автоматически.
PreparedTask общий формат task payload, PreparedDefectTask сохранён alias.
Publication params, transactional ingest persistence и запуск пока не подключены.

ReadIngestMetadata(ctx, reader, metadataRef, sourceRef) ограничивает чтение1MiB,
проверяет SHA исходных байтов, канонический JSON без дубликатов и точную схему
AssetMetadata, включая обязательный nullable icc_sha256. Проверяет exact source,
предел200MP, depth8/16, orientation1..8, format и ICC digest. Только после проверки
возвращает IngestMetadata для ресурсного расчёта. Размеры сохраняются как в inspect:
ориентация меняет стороны, но не pixel count. Caller обязан подтвердить canonical
asset.inspect, tenant/version и закрыть reader; функция не выдаёт artifact access.
Tests отклоняют подмену байтов/source, unknown/missing/null поля и malformed values.

### Сохраняемые ingest node plans

SQL48 вводит function-only FORCE RLS таблицу ingest_node_plans. Register узла
блокирует unsealed активный ingest run, сверяет source ID/SHA/bytes с immutable
snapshot item соответствующего ordinal и ready artifact. UNIQUE run/ordinal/op
и run/node_key обеспечивают один план каждого этапа. Повтор возвращает true только
при exact ID/params/hash/source; конфликт false. Подмена snapshot или другой run
не разрешается. SQL не вычисляет JCS hash: доверенный Go compiler валидирует полные
params и вычисляет params_sha перед вызовом scheduler-only функции.

IngestPlanRepository.Register заново компилирует spec и сохраняет узлы отдельными
короткими autocommit вызовами. Частичный batch допускается как незавершённый план;
повтор использует те же IDs, runnable task не создаётся. Ни публикация документов,
ни graph_sealed здесь не выполняются. Для следующего этапа требуется расширить
владение task_params на сохранённый ingest node и реализовать task/edge insertion.
Migration48 live PostgreSQL не проверена; adapter tests подтверждают лишь stop /
resume стабильных IDs и отмену до SQL.

### Публикация ingest params

SQL49 расширяет reserve/complete_backend_document: task_params owner может быть
сохранённым ingest_node_plans.id того же tenant/run, только для ingest unsealed run
и exact params_sha. Существующие fencing tokens, storage reservations, version pin,
ready replay и orphan cleanup сохранены. Review expansion ownership не меняется.
Миграция заменяет функции SQL41 целиком; live SQL49 ещё не проверена.

IngestDocumentBuilder.Publish сначала вызывает Register для всего плана, затем
последовательно Publisher.Publish(scope owner=node.ID,purpose=task_params).
Каждый возвращённый ref проверяется по SHA node.Params. Ошибка не возвращает
частичный готовый план. Успешно сохранённые immutable документы остаются для
повторного использования; отменённые pending reservations обрабатываются
существующим cleanup. Тесты controlled ports проверяют порядок register→publish,
частичный сбой, стабильные owner scopes при повторе и запрет публикации при
конфликте регистрации. Task/edge insertion и ingest runtime ещё не подключены.

### Materialize ingest task

SQL50 materialize_ingest_task(tenant,run,PreparedTask,metadataID?) под блокировкой
unsealed ingest run сверяет saved plan ID/key/op, params document owner/hash,
ready source, operation registry и минимальный resource budget. Task создаётся
blocked; inspect edge идёт от verify того же ordinal, decode edge от inspect.
Decode требует metadata ID из canonical succeeded inspect output role metadata.
Go обязан предварительно прочитать JSON и проверить его source/размеры; SQL
проверяет provenance, но не получает bytes JSON metadata. Переданный params/profile
не превращает worker в квалифицированный: dispatch сохраняет admission проверки.
Повтор задачи сравнивает params/input digest/inputs/profile; конфликт false.
Task и edge вставляются в одной транзакции функции. IngestPlanRepository.Materialize
проверяет digest/resource schema и передаёт profile как JSON object, не base64.

Пока нет coordinator stage barriers: verify/inspect должны быть materialized и
sealed перед исполнением, после inspect отдельный expansion unseals и связывает
decode. Без этого эти primitives сами не запускают ingest. SQL50, concurrent
replays и RLS требуют live проверки; controlled Go adapter tests её не заменяют.

### Initial ingest barrier и фазы

SQL51 вводит function-only ingest_progress(phase inspection/decode/catalog/complete,
item_count). SealInspection сверяет snapshot count1..10000, ровно3 plans и2 tasks
на каждый item, опубликованные params всех этапов, blocked verify/inspect и exact
success edges verify→inspect. Отсутствие/лишние узлы/edges не закрывают barrier.
В одной транзакции записывается inspection phase и graph_sealed=true. Повтор в
той же sealed phase идемпотентен. Задачи ещё требуют Activate и admission.

Trigger guard_ingest_completion запрещает completed/completed_with_errors до
phase complete; failed/cancelled не блокируются. Это отделяет готовность текущего
барьера от завершения полного ingest. Phase transition decode/catalog/complete
ещё не реализованы, поэтому завершённый ingest не заявляется. SQL51 live SQL/RLS
не проверена; Go SealInspection adapter unit test проверяет scope/result handling.

### Coordinator начального ingest-этапа

IngestInspectionCoordinator.PrepareInspection объединяет CompileIngest,
IngestDocumentBuilder.Publish, PrepareIngestTask для verify/inspect,
IngestPlanRepository.Materialize и SealInspection. Проверяет весь возвращённый
набор документов и SHA до первого task insert. Decode params публикуются, но
сам decode task ждёт inspect metadata. Частичная ошибка task insertion не вызывает
seal; повтор до seal сохраняет стабильные task IDs и документы. После успешного
seal/неизвестного исхода seal вызывающий сервис должен читать persisted phase и
продолжать reconcile, а не заново unseal; такой runtime entrypoint пока отсутствует.
Controlled ports tests проверяют частичный сбой, resume и отсутствие decode без
metadata. Coordinator ещё не установлен в scheduler: необходим persisted run
selection, sealed replay reconciliation и продвижение verify→inspect→decode.

### Runtime activation inspection-stage

SQL52 pending_ingest_activations(limit1..100) выдаёт blocked verify/inspect из sealed
inspection phase с готовыми params/source и succeeded canonical predecessors.
Это discovery без разрешения на dispatch: IngestActivator вызывает SQL47 для
каждого кандидата отдельным CAS. False пропускается как stale/not eligible;
ошибка останавливает batch, возвращая уже подтверждённое число переходов.
Scheduler вызывает activation после recovery, перед dispatch; startup требует
SQL47/52. Счётчик photo_scheduler_ingest_activation_confirmations_total допускает
idempotent ready replay при гонке schedulers и не равен числу broker messages.

Initial coordinator ещё не выбирает run автоматически. Decode expansion,
распространение failed verify на inspect, catalog commit и дальнейшие фазы ещё
нужны. Поэтому установленный activation loop не означает готовый ingest целиком.
SQL52 и реальный startup пока не проверены из-за недоступной инфраструктуры.

### Ошибка verify в inspection phase

SQL53 skip_failed_ingest_inspection переводит один blocked inspect в
skipped/upstream_failed только для окончательного failed/cancelled verify.
Retry_wait/running/queued не подходят. Discovery повторно проверяется после
lock_task; отменённый run не меняется этим обработчиком. Scheduler вызывает
bounded propagation после recovery перед activation; каждый переход committed
отдельно, число переходов отражено в photo_scheduler_ingest_upstream_skips_total.

Это изменение authoritative tasks, не worker execution event: попытка inspect
не фабрикуется. Worker TaskEvent пока не имеет task.skipped; UI graph-state event
и inbox advancement ещё требуют реализации. Decode/catalog continuation должны
учитывать skipped/upstream_failed как failed item, а не identity output. SQL53
live не проверена, adapter tests проверяют batch/error/cancel semantics.

### План перехода inspection→decode

PlanIngestDecode сопоставляет каждый immutable snapshot ordinal с canonical
InspectionOutcome. Pending/retry_wait или ещё не распространённый verify failure
удерживают весь barrier; частичный decode plan не возвращается. Successful inspect
требует hash-verified metadata/source binding; PrepareIngestTask создаёт decode
profile и input digest. Failed/cancelled verify + skipped/upstream_failed inspect
либо failed/cancelled inspect становятся IngestItemFailure с исходным ordinal/source.
Невозможные состояния (например inspect success при незавершённом verify) ошибочны.
All-failed batch готов с пустым Tasks и непустым Failures, не считается success.

Это pure planner, а не DB phase transition: persisted outcome reader, atomic unseal /
decode insertion/seal и итоговая запись failed items ещё нужны. MetadataRef provenance
проверяет repository; JSON metadata передаётся после ReadIngestMetadata. Tests
покрывают mixed success/failure, удержание барьера, retry_wait и impossible states.

### Загрузка canonical inspection outcomes

SQL54 ingest_inspection_outcomes(tenant,run) возвращает verify/inspect states и
metadata только при ровно одном ready metadata artifact из canonical inspect
result. Возвращаемая локация содержит bucket/key/version/SHA/bytes; доступ только
photo_scheduler, scope ограничен активным ingest inspection run.
IngestOutcomeReader.Load сверяет полный набор ordinals/source refs со spec,
закрывает SQL rows перед S3 IO, проверяет ожидаемый bucket, ненулевую immutable
version, MIME application/json и лимит1MiB. ReadIngestMetadata проверяет JSON и
SHA/source; limited reader дополнительно подтверждает exact persisted bytes.
Reader закрывается при успехе и ошибке. Перед phase transition repository обязан
повторно проверить canonical task state: чтение не удерживает транзакцию за IO.
Тест controlled object port проверяет version pin, size mismatch и закрытие ресурсов.
SQL54/live S3 и соединение reader с phase persistence ещё не подтверждены.

### Начало decode phase

SQL55 begin_ingest_decode блокирует run/progress, повторно читает canonical
inspection outcomes и сравнивает successful metadata refs с manifest Go planner.
Весь набор проверяется до первой записи. Затем атомарно сохраняются
 ingest_decode_items (decode metadata или failed stage/reason), decode_manifest,
phase=decode_build, graph_sealed=false и revision+1. Pending/retry_wait и
несогласованные outcomes не изменяют БД. Same-manifest replay после перехода
идемпотентен; другой manifest отвергается. All-failed имеет пустой metadata
manifest, но сохраняет failed items, не превращаясь в success.

BeginDecode adapter сортирует manifest по ordinal и не передаёт клиентские
failure reasons: SQL выводит их из task states. Decode task insertion, закрытие
нового barrier и resume loading из decode_build ещё надо соединить. SQL55 live,
нагрузка10000 items и конкуренция не проверены; Go wire tests не доказывают их.

### Decode barrier и materialization coordinator

SQL56 требует decode_build + сохранённый metadata ID в ingest_decode_items при
materialize decode. SealDecode проверяет count items/tasks/edges, blocked decode
для каждого successful ordinal, canonical predecessor metadata и exact success
edge. Незавершённые verify/inspect и open expansions удерживают barrier. После
проверки atomically graph_sealed=true и phase=decode; all-failed идёт в catalog
без decode tasks, не в completed. Pending activation discovery теперь включает
asset.decode только в sealed decode phase, поэтому существующий scheduler loop
может активировать такие задачи через SQL47.

IngestDecodeCoordinator.Build выполняет BeginDecode→Materialize nodes→SealDecode.
Ошибка до последнего шага не закрывает граф; повтор внутри decode_build сохраняет
IDs и metadata bindings. Runtime discovery/resume loader для decode_build ещё
нужен: coordinator не должен заново строить уже sealed выполняющийся decode graph.
SQL56/live concurrency не проверены. Controlled tests проверяют ошибку середины
batch, отсутствие premature seal, metadata IDs и all-failed без decode execution.

### Resume decode_build без повторной публикации

SQL57 расширяет inspection outcomes чтением decode_build только при совпадении
текущих canonical metadata/terminal reasons с ingest_decode_items. Изменённый
результат исчезает из выборки и вызывает incomplete-snapshot error в loader.
IngestPublishedReader читает ранее опубликованные task_params, сверяет все IDs /
keys / operation / SHA с повторной детерминированной компиляцией и восстанавливает
порядок независимо от SQL row order. Никаких PUT/reserve при таком чтении нет.
IngestDecodeAdvancer соединяет documents→outcomes→PlanIngestDecode→coordinator.
Pending phase возвращает false без mutation. Runtime выбор run/phase и защита от
повторного build после уже sealed decode ещё требуют orchestration loop. SQL57
live не проверена; unit tests покрывают unordered rows, missing docs и wrong SHA.

### Загрузка ingest snapshot и маршрутизация фазы

`IngestSnapshotPayload` фиксирует обязательный JSON каждого snapshot_items.payload:
`{"schema_version":1,"suffix":".jpg","assume_profile":null,"analyze":false,"tile":512}`.
Все пять полей обязательны, неизвестные поля запрещены. schema_version=1;
suffix — allowlist CompileIngest; assume_profile — null либо один из трёх
поддерживаемых profile IDs; analyze — boolean; tile — 256/512/1024 без
неявного default в persisted payload. Источник/размер берутся из tenant-bound
snapshot item и ready artifact, а не из payload. Создатель ingest snapshot
обязан сериализовать этот контракт (API создания пока не реализован).

SQL58 `ingest_run_context(tenant,run)` читает phase, graph_sealed, snapshot
item_count и ready source bindings одним statement/MVCC snapshot. Только
scheduler имеет EXECUTE. Отсутствующий progress означает planning; завершённые,
отменённые и expired snapshots исключаются. Лимит 10001 позволяет loader
обнаружить превышение максимума 10000. Пропущенные/expired artifact rows
не превращаются в уменьшенный успешный batch: loader сверяет полный item_count.

`IngestRunReader.Load` проверяет required поля через строгий canonical roundtrip,
source digest/bytes, уникальные ordinal, постоянство phase/count, полноту snapshot
и CompileIngest constraints. planning/decode_build требуют graph_sealed=false;
inspection/decode/catalog — true. Результат сортируется по ordinal. Чтение не
заменяет lease и повторные SQL проверки под блокировкой.

`IngestRunner.Advance` каждый раз загружает persisted context: planning вызывает
PrepareInspection; inspection/decode_build — IngestDecodeAdvancer; decode/catalog
возвращают false без сборки. После потерянного ответа seal следующий вызов
читает новую фазу вместо повторной публикации. false не означает completed.
Неизвестные фазы и чужой run ID отклоняются. Discovery/сериализация runtime
по run, подключение runner к scheduler и catalog completion ещё необходимы.
SQL58 пока не проверена на живой БД. Unit tests и contracts-check прошли;
legacy quality source/presets unchanged.

### Scheduler ingest build loop

SQL59 хранит ingest_build_token/until и next_ingest_build_at в runs. Claim
выбирает active non-cancelled ingest planning/inspection/decode_build, блокируя
только run через FOR UPDATE SKIP LOCKED. Lease=60s, release откладывает следующий
claim на 5s. Renew/release требуют текущий случайный token; прежний owner не
может продлить/сбросить новую аренду. Decode/catalog исключены из discovery до
реализации catalog coordinator. Claim упорядочен по next_at/id для ротации runs.

IngestProcessor выполняет начальный renew, затем heartbeat каждые 10s с timeout
5s и cancellable runner budget 5min. Потеря lease/ошибка renew отменяет работу;
heartbeat останавливается и join выполняется до release. Release вызывается
также после ошибки/cancel с отдельным timeout; потерянный release восстанавливается
истечением lease. SQL/S3 операции не удерживают общую БД транзакцию.
Lease ограничивает дублирование работы, **не гарантирует exactly-once**: при
истечении аренды возможна завершившаяся операция старого процесса. Корректность
обеспечивают immutable plans, document reservation tokens, stable task IDs,
phase/barrier проверки и run locks внутри отдельных SQL mutators. Результат
ProcessOne после неизвестного commit не является доказательством отсутствия
изменений; следующий вызов перечитывает persisted phase.

RunScheduler создаёт IngestRunReader→IngestRunner→IngestProcessor с реальными
SQLRepository/S3 адаптерами и отдельным RunExpansionLoop. Dispatch/recovery
продолжаются независимо от длительного object IO. Shutdown отменяет/join оба
build loops до pool.Close. Readiness включает ingest loop; counters
photo_scheduler_ingest_build_transitions_total и
photo_scheduler_ingest_build_failures_total показывают подтверждённые переходы
и ошибки (не количество завершённых run). Startup проверяет необходимые SQL
функции 48–59 и objectstore. SQL59/live concurrency/E2E пока не проверены;
controlled tests проверяют initial/heartbeat lease loss, release, idle и build
errors. Создание ingest run через API и decode→catalog completion остаются
необходимыми для пользовательского сквозного сценария.

### Проверка decode перед каталогом

SQL60 ingest_decode_outcomes возвращает каждый сохранённый ingest_decode_item,
source binding, prior failure либо decode task/state и canonical output object
locations. Ready/unexpired выходы берутся только из canonical task_results и
created_by_task_id; loader обязан сверить полноту batch, уникальные роли и refs.
Функция read-only scheduler-only; она не меняет фазу и не создаёт asset version.

ReadDecodedImage читает <=1MiB JSON с проверкой точных SHA bytes, строгим
WorkingImage parser и сверкой pixels/alpha с отдельно подтверждёнными выходами
decode. Требуются float32, work_rec2020_gamma24_v1/gamma24, SHA ICC из доверенного
registry, source SHA, space_id=oriented:<source SHA>, empty transform chain.
Manifest/pixels/alpha/source IDs различны. Alpha presence соответствует inspect.
IngestMetadata теперь сохраняет orientation и optional attested `sensor_alignment`. Для RAW mapping публикуется только при LibRaw `flip=0`, точных physical raw dimensions/margins и наличии Make+Model+BodySerialNumber; `camera_id=raw-<SHA256(camera-v1\0make\0model\0serial)>` не раскрывает серийный номер. JPEG/PNG/TIFF и RAW без серийного номера получают `sensor_alignment:null`, поэтому sensor.dust для них закрыто недоступен. Миграция00189 передаёт canonical inspect metadata в атомарную catalog adoption и сохраняет её под `metadata.ingest`, сохраняя верхнеуровневый WorkingImage contract. Для raster orientation 5–8 меняет
местами width/height; 1–4 сохраняет. Для RAW текущий inspect возвращает размеры
до ориентации LibRaw и orientation=1: допускается только исходная пара размеров
или её перестановка, не произвольный canvas равной площади. Точную RAW orientation
нельзя подтвердить этим metadata контрактом; это явно ограниченная проверка.

ReadDecodedImage не проверяет сами массивы пикселей и не подтверждает producer
по refs без БД. Canonical producer attestation, version-pinned S3 loader и
transactional catalog commit ещё нужно соединить. SQL60/live не проверена.
Тесты проверяют все raster orientation, alpha, чужие refs/source/profile,
подмену bytes, cancellation и RAW dimensions. Legacy quality код не изменён.

### IngestCatalogLoader

Load(tenant,spec) возвращает IngestCatalogBatch{Ready,Images,Failures}. Spec
повторно проходит CompileIngest; SQL60 rows должны покрыть все ordinal ровно
один раз с exact source ID/SHA. Prior failed items сохраняют stage/reason без
выдуманного decode task. Decode blocked/ready/queued/running/retry_wait удерживает
весь batch (Ready=false, без частичного Images/Failures и без object IO). Failed/
cancelled decode сохраняется как item failure. Другие состояния отклоняются.

Succeeded decode требует ровно working_image + working_rgb и optional alpha,
без повторных ролей/IDs, со строгими bucket/version/media_type/bytes/digest
ограничениями. После проверки всех строк cursor закрывается до S3. Loader
читает только metadata и working_image по зафиксированным версиям, проверяя
объявленный размер, SHA исходных bytes и контракты ReadIngestMetadata /
ReadDecodedImage. Все readers закрываются и при ошибках. Pixel arrays остаются
в хранилище; их полный download здесь не нужен. WorkICCSHA передаётся из
доверенного registry и не берётся из результата worker. Результат сортируется
по ordinal. Повтор catalog commit должен заново подтвердить canonical task,
outputs и phase в SQL; загрузчик сам в каталог ничего не записывает.

Тесты покрывают успешное чтение двух JSON, version pinning, закрытие SQL до IO,
ожидание целого batch без частичных результатов, ошибки ранних стадий и decode,
missing/duplicate rows, роли outputs, size/SHA mismatch и закрытие ресурсов.
Интеграция trusted ICC registry в scheduler, catalog transaction и переход run
к завершению остаются последующими задачами. SQL60 пока live не проверена.

### Финальный барьер ingest (SQL67)

finish_ingest_run(tenant,run) возвращает NULL пока durable барьер не выполнен,
либо completed/completed_with_errors/failed. Проверки под run→progress locks:
active sealed ingest decode/catalog, отсутствие cancel/open expansions/pending
reviews/nonterminal tasks; равенство snapshot/progress/bindings/decode-items counts;
для каждого snapshot ordinal exact source binding и **ровно один** settled catalog
receipt + verified upload с version ID либо rejection receipt + rejected upload.
Недостаточно просто равенства количества successes+failures: проверяется покрытие
ordinal и отсутствие одновременных success/failure. Частичный upload settlement
не закрывает run. All-success→completed, mixed→completed_with_errors,
all-failed→failed. Pending guest moderation не равна незавершённому импорту:
успешно сохранённый, но скрытый guest source считается imported item.

В одной транзакции progress.phase=complete, runs.status/revision обновляются и
выделяется tenant_event_cursors.last_cursor+1 с safe integer guard. В run_events
пишется type=run.finished, audience=principal, principal_id=run.created_by,
payload {run_id,status,revision,imported_items,failed_items}. Event delivery API
обязан проверять этот principal и текущий доступ к run; гостю нельзя выдавать
owner event через tenant-wide stream. Повтор complete возвращает сохранённый
статус без нового cursor/event. Это DB durable event, не реализованный SSE/API.

Go IngestCatalogRepository.Finish сохраняет NULL как empty status; неизвестные
статусы отвергаются. Adapter tests/контракты прошли; SQL67 live/concurrency и
атомарность cursor/event не проверены. Catalog coordinator/scheduler discovery
для decode/catalog, trusted ICC loading и staging cleanup ещё требуются для
сквозной работы. Finish не выдаёт разрешения на удаление snapshot/task inputs:
retention/ref checks и отдельный cleanup ticket обязательны.

### IngestCatalogCoordinator

Advance(tenant,spec) объединяет validated IngestCatalogLoader, OriginalCoordinator
и IngestCatalogRepository. До mutation сверяет full count, уникальные ordinal,
source identity и допустимые failure stage/reason; Ready=false удерживает весь
batch. Смешанные success/failure сортируются общим ordinal order, поэтому порядок
membership стабилен. Для success Prepare original→AdoptImage; для failure
RejectFile без object IO. False mutation result — conflict/error, не успех.

Каждый файл фиксируется отдельной idempotent транзакцией; ошибка середины batch
сохраняет предыдущие commit и **не вызывает Finish**. Повтор перечитывает canonical
batch и повторяет stable scopes; ready originals/adoption/rejection receipts
предотвращают повторное списание/создание. После всех файлов Finish проверяет
SQL67 durable barrier. Empty status означает пока не завершено, terminal status
означает подтверждённое завершение. Частичные commit при error не выдаются как
успешный run. Worker-produced JSON integrity и producer attestation принадлежат
Source; SQL повторно подтверждает актуальность перед mutation.

Controlled tests проходят: общий ordinal порядок, mid-batch failure без finish,
retry, pending/missing/duplicate/wrong source без mutation, all-failed без original
копирования. Contracts/legacy check прошли. Coordinator ещё требует trusted ICC
loader и scheduler decode/catalog route/discovery; cleanup жизненного цикла
staging/originals и live SQL integration остаются невыполненными.

### Catalog wiring и trusted ICC

colorprofile.LoadWorkSHA(PHOTO_ICC_ROOT) читает локальный deployment registry через
os.OpenRoot, запрещает symlink файлов, ограничивает lock 64KiB/profile 1MiB,
проверяет strict canonical manifest version1 с ровно тремя известными IDs,
basename path=id.icc, declared bytes/SHA и ICC header/tag ranges. Проверка ICC
соответствует Python validate_icc; это структурная проверка, не новый color engine.
Рабочий SHA берётся только после проверки реальных profile bytes. Trusted lock
и root — deployment input, не данные пользователя/worker; runtime не загружает
профили из сети. Scheduler startup требует PHOTO_ICC_ROOT явно, проверяет SQL
adoption/rejection/finish/original dependencies и objectstore.

SQL68 расширяет claim_ingest_build на decode/catalog. IngestRunner в этих фазах
вызывает Catalog coordinator вместо no-op; отсутствие coordinator — ошибка.
RunScheduler соединяет IngestCatalogLoader, OriginalRepository/Coordinator,
IngestCatalogRepository с одним проверенным WorkICCSHA. Object IO остаётся в
отдельном heartbeat build loop. Сохранённые terminal runs не выбираются discovery.
Неполные decode tasks возвращают pending, complete batch выполняет settlement и
finish. Это подключение runtime, не подтверждение работы всей системы: upload/
ingest creation API, cleanup staging/abandoned originals и live SQL20–68/E2E ещё
не завершены. ICC corruption/path/duplicate tests, runner routing и весь backend
suite прошли; integration tests без живых сервисов не считаются live проверкой.

### Отмена ожидающих задач (реализовано, SQL73)

`photo.cancel_idle_task()` выбирает одну задачу blocked/ready/queued/retry_wait,
если task.cancel_requested либо run.cancel_requested. После стандартного
`lock_task` (node→slot→usage→run→task) повторно проверяет состояние и отмену,
освобождает ресурсы текущего fence, сохраняет cancelled/cancel_requested,
повышает fencing_token и обнуляет dispatch_command, next_attempt_at, lease_until.
Повторный вызов не меняет терминальную задачу и не расходует retry budget.
Отложенный retry отменяется сразу, независимо от next_attempt_at.

Recovery выполняет до limit отдельных вызовов перед reaper/retry planning;
каждый вызов — отдельная транзакция, никаких удерживаемых locks между задачами.
Scheduler требует SQL73 при startup. Running не переводится этим helper:
worker получает отмену через heartbeat, commit success запрещён при cancel,
истёкшую аренду завершает существующий reaper. Уже доставленная queued-команда
отсеивается claim по cancelled/fence. Helper не публикует attempt event для
задачи, которая могла ещё не иметь attempt; состояние БД является источником
истины.

Проверки: workflow Go tests и contracts-check прошли; legacy quality hashes
сохранены. `TestIntegrationCancelIdleTasks` покрывает task-local/run cancel,
четыре idle состояния, future retry, повтор и неизменность running/succeeded.
Он требует isolated PostgreSQL с SQL73 и в текущей среде skipped; это не
подтверждение исполнения миграции или конкуренции на живой БД.

### Запрос и финализация отмены (реализовано, SQL74)

`POST /api/v1/tenants/{t}/runs/{id}/cancel` проходит общую session/CSRF
проверку и `tenant.WithTenant(..., Editor)`. Первый вызов под блокировкой run
ставит `cancel_requested=true`, статус `cancelling`, повышает revision и пишет
единственный audit `run.cancel_requested`; повтор для cancelling/cancelled
возвращает текущее состояние без новой записи. Завершённые run дают
RUN_NOT_CANCELLABLE. `GET /runs/{id}` доступен viewer и возвращает RunStatus с
устойчивым revision, task/review counts и вычисленным terminal progress.
`GET /runs` использует тот же агрегат, limit 1..100, whitelist status и
подписанный cursor, привязанный к tenant, principal, filter hash и сортировке
created_at/id; подмена фильтра или пользователя делает cursor недействительным.

`photo.finalize_run_cancellation()` выбирает один cancelling run и повторно под
его row lock требует терминальность всех задач. Pending reviews становятся
`cancelled` без поддельного решения пользователя; незакрытые expansions
закрываются с очисткой lease. Затем одним commit run становится cancelled,
graph запечатывается, revision повышается и выделяется tenant cursor для
`run.cancelled`. Повтор не создаёт событие. Scheduler вызывает finalizer
короткими отдельными транзакциями после drain idle tasks.

Для ingest финализация дополнительно запрещена, пока у существующих upload
bindings нет settlement receipt (catalog item либо rejection). Это защищает
квоту от молчаливого освобождения.

Полный backend suite, contracts/db generation checks и legacy quality check
проходят. Интеграционные тесты проверяют idempotent request/audit и finalizer
barrier/event, но без PHOTO_TEST_DATABASE_URL они skipped; SQL73–74 и реальные
конкурентные транзакции остаются live-непроверенными.

### Settlement отменённого ingest (реализовано, SQL75–76)

`photo.cancel_ingest_upload()` после терминальности всех task выбирает один
неурегулированный binding отменённого run. Под locks
usage→share→run→upload→quota он повторно связывает immutable source, upload,
principal/share и reservation. Reserved bytes снимаются, фактический staging
object добавляется в storage usage, share reserved counters освобождаются,
reservation становится released, upload — rejected. В `ingest_rejections`
пишется terminal outcome `run.cancel/cancelled`; существующий trigger ставит
staging cleanup на семь суток. Повтор не меняет счётчики. Scheduler выполняет
settlement перед `finalize_run_cancellation`.

При финализации cancelled ingest его `ingest_progress` атомарно становится
complete, поэтому staging cleaner допускает cancelled наряду с прочими
terminal status. Снимок остаётся историческим и не перепривязывается.

SQL76 расширяет тот же original cleanup protocol для original, который успел
стать ready до отмены, но не получил catalog receipt. Claim требует cancelled
run, matching rejection и отсутствие catalog item, проверяет generated key и
точную artifact identity, переводит artifact ready→deleting и выдаёт token.
После `PurgeOriginal` finish проверяет fence, снижает storage_bytes ровно один
раз и ставит artifact deleted. Pending-copy cleanup продолжает снижать
reserved_bytes по прежней ветке. Adopted originals сюда не попадают.

Контролируемые Go tests maintenance/objectstore/workflow/run и contracts checks
проходят, legacy quality hashes не изменились. SQL75–76 не исполнялись на живом
PostgreSQL; row-lock, rollback, quota/share counters и cleanup replay требуют
обязательного integration fault-injection прогона при доступной dev-БД.

### Apply v4 variable group cardinality

Миграция00190 вводит отдельную scheduler-only `apply_v4_inputs_with_metadata`, не меняя return type ранее опубликованной функции, и передаёт version-pinned `asset_versions.metadata` компилятору. `parseApplySensorAlignment` принимает только сохранённый RAW mapping, exact current Canvas и opaque image. `compileApplyDustGroups` сортирует camera/mapping keys и snapshot ordinals; группы 5–64 кадров, серии больше64 делятся сбалансированно, без недопустимого короткого хвоста.

Миграция00191 добавляет cardinality `group` и immutable `expected_nodes`, которое может быть установлено один раз только владельцем текущей Apply lease. Регистрация node, activation и последующий claim используют сохранённое число, а group node обязан ссылаться на реальный snapshot item. Operation allowlist уже включает `sensor.dust`.

Миграция00192 и backend review service создают один `sensor_dust` review на canonical group task, повторно проверяют proposal и сохраняют content-bound subset selection; registry содержит dedicated `sensor.dust_repair` с ограниченными output policies. Миграция00193 завершает Apply continuation: compiler принимает только policy `review`, фиксирует `dust_detect(group) → dust_review → dust_repair(item)`, допускает ноль пригодных групп, создаёт review только при `task_result.review_required=true`, а пустой detection автоматически связывает с base route. После всех решений fenced barrier снова запечатывает граф; scheduler получает canonical selection только через lease-bound SQL и встраивает её в immutable repair params. Для кадров вне групп создаётся явный pass-through repair node, поэтому последующий item stream всегда сохраняет исходную cardinality. `conservative_auto` отклоняется и не может превратить score в автоматическое удаление.

Live PostgreSQL integration строит пятикадровую group stage, проверяет однократное открытие review, отсутствие повторного claim пока решение pending, переход к `dust_repair` только после decided selection и lease-bound выдачу proposal/selection. Тот же полный integration suite повторно проверяет существующий defects barrier; это выявило и устранило регрессию повторного claim pending defect review в последней общей claim-функции. Полный integration результат после исправления: 76 passed.

### Contact-sheet task

Contact-sheet использует однозадачный durable interactive build с operation `report.contact_sheet`. Declared inputs сохраняют порядок snapshot previews и завершаются exact font artifact; coordinator не сортирует этот список и сверяет его с params до публикации task document. После fenced materialization общий dispatcher, gateway и `finalize_interactive_run` применяются без отдельной очереди. Run outputs разрешает только canonical `contact_page` и `contact_sheet` artifacts.
