# V29: поэтапная публикация заменяющего графа

Статус: код подготовки и проверок написан; публикация и recovery не завершены. Тесты не запускались, миграции не применялись.

## Решение о границах run

Topology schema_version=2. Каждая исходная affected task получает отдельный заменяющий run. Группировка по исходному run не подходит: при запечатывании такого run ещё неизвестны SHA результатов промежуточных родителей, необходимые для immutable Params зависимых операций. Подмена SHA идентификатором будущего результата запрещена.

Namespace = UUIDv5(tenant, `V29.NegativeReplacement/v2:<project>:<review>:<decision_hash>`).
Task = UUIDv5(namespace, `task:<original_task>`).
Run = UUIDv5(namespace, `run:<original_run>:<original_task>`).

Marker run.config_snapshot.v29_negative_replacement — точный объект с полями version=`v29_negative_replacement_v2`, project_id, review_id, decision_hash, original_run_id, original_task_id. UUID записываются строками. Старые grouped topology schema1 не восстанавливаются как schema2; namespace и marker изменены вместе.

## Порядок исполнения

1. Зафиксировать решение review, вычислить и независимо проверить invalidation closure, дождаться завершения отмены affected tasks и cross-run consumers.
2. Загрузить точный selected base, документы и массивы. Восстановить исходные frozen intent и сохранённые соответствия source→producer.
3. Выбрать готовую волну. Для rebuilt родителей нужны принятые физические результаты и SQL membership. Исходные неизменяемые входы проверяются отдельно; отсутствие любого входа отклоняет подготовку.
4. До SQL-транзакции собрать новые Params и выполнить квалифицированную компиляцию по реальным оригинальным документам и SHA. Сохранить пользовательские controls, asset identity и порядок кадров.
5. В транзакции повторить полный набор source/mapping/intent/settlement/qualification checks. Создать отдельный draft run с указанным marker и детерминированными owner/document/task IDs, зарегистрировать проверенные документы и транспорт, затем запечатать/поставить задачи в очередь атомарно с receipt. Этот шаг ещё требуется реализовать.
6. После adoption и обязательного review результата родителя подготовить следующую волну. Между runs не создавать task_edges: зависимая задача публикуется только после получения полного физического результата родителя; её source refs и сохранённая provenance фиксируют связь.
7. Recovery повторно читает сохранённые документы/marker/receipt и точные исходные ресурсы. Не копирует прежний execution profile и не создаёт повторные задачи после неопределённого commit. Координатор recovery ещё требуется реализовать.
8. Завершение исходного review допускается только после независимого подтверждения полного заменяющего графа; сама membership-проверка отдельной задачи не доказывает завершение всей ветки.

Положительные lifecycle, concurrency и crash-recovery проверки остаются обязательными на этапе глубокого тестирования. Существующие unit/ACL случаи не заменяют эти доказательства.

## Идентификатор публикации

PublicationIdentity фиксирует version2 tenant/project/review/decision, исходные и новые task/run, WorkflowID нового run, UUIDv5(new task, `params`), operation, Params SHA и marker SHA. Canonical digest служит границей совпадения повторной подготовки; это не receipt сохранённого run. Обычный NegativeDraft проверяет ordinal TaskID и исходный scope, поэтому заменяющим задачам требуется отдельный SQL save/registration path с теми же quota/document/qualification invariants. Нельзя ослаблять обычный draft ради приёма произвольного TaskID.

## Восстановление подготовленного negative draft

RestoreNegativeReplacementDraft ограничивает размер 3 MiB, принимает только точный canonical schema2 и ожидаемый snapshot исходного consumer scope. Затем повторно вызывает CompileNegativeReplacementDraft с текущими prepared physical inputs, deployment locks и qualification report/calibration и требует полного побайтового совпадения пакета. Сохранённый execution profile не становится авторитетным. Это восстановление подготовленного пакета, не доказательство сохранения run или receipt. Unit отрицательных случаев написаны, не запускались; положительный lifecycle остаётся открытым.

## Транзакционная привязка replacement draft

LockNegativeReplacementDraft повторно готовит всю ready wave и ищет ровно один negative draft с полным совпадением freshly compiled пакета. После этого удерживает LockCompleteMappedSources, original consumer snapshot и LockLocatedNegativeQualification в транзакции вызывающего кода. Миграция 01458 проверяет точное совпадение snapshot исходного run и workflow, project, operation/version, terminal original task и повторно читает frozen intent под блокировками. Только scheduler получает EXECUTE. Любой отказ требует rollback. Метод не сохраняет run, не резервирует quota и не выдаёт receipt; SQL save остаётся следующим этапом. Unit и SQL ACL/refusal тесты написаны, NOT RUN; 01458 NOT APPLIED, положительный lifecycle открыт.

## Предварительное сохранение replacement negative draft

Миграция 01459 и SaveNegativeReplacementDraft написаны. SQL повторно проверяет whole graph settlement, исходный snapshot, schema2 UUIDv5 identities и marker, identity/marker SHA, owner, recovery, форму Params/profile, полный input digest и текущую qualification. Создаются только draft run и workflow, с точным marker и неизменяемым v29_negative_replacement_draft_jcs. Повтор допустим только при совпадении всей конфигурации и workflow; чужой или изменённый пакет отклоняется. Go сохраняет в той же транзакции, где выполнены LockNegativeReplacementDraft и полный набор physical source/mapping/qualification locks. Ошибка commit возвращается без создания новых ID; durable reader и recovery ещё требуются.

Это не admission: output quota не резервируется, Params document не публикуется, task не создаётся, очередь и receipt не появляются. Дальнейшая регистрация обязана повторить проверки исходных frozen intent/physical inputs и квалификации; сохранённый профиль не является авторитетным. Unit и SQL ACL/refusal тесты написаны, NOT RUN; 01459 NOT APPLIED, положительный lifecycle не доказан.

## Чтение сохранённого предварительного draft

Миграция 01460 и LoadNegativeReplacementDraft написаны. Reader вычисляет v2 replacement IDs из исходной frozen intent, проверяет tenant/project, actor/session, исходный snapshot, draft/nonsealed/noncancelled состояние, workflow, marker, точные config keys и config hash. Go предварительно компилирует ожидаемый пакет из готовой волны и текущей qualification, читает полное совпадение, выполняет LockNegativeReplacementDraft, повторно читает те же сохранённые байты и завершает транзакцию до возврата. Возвращаемые bytes принадлежат draft.

Отсутствие draft, другой session, отмена, изменение конфигурации или уже sealed run не интерпретируются как разрешение повторной публикации. Reader ничего не создаёт; последующая admission обязана повторить проверки в собственной транзакции. Recovery опубликованного run требует отдельного receipt пути. Unit и SQL ACL/refusal cases написаны, NOT RUN; 01460 NOT APPLIED. Положительные commit-ambiguity/concurrency lifecycle остаются открытыми.

## Публикация replacement Params

Миграция 01461 и PublishNegativeReplacementParams написаны. Отдельный artifactjson.Repository восстанавливает точные Params из свежей квалифицированной компиляции всей ready wave. Перед immutable IO сохраняется preliminary draft; Reserve и Complete отдельно повторяют LockNegativeReplacementDraft. SQL сверяет сохранённый пакет и invalidation_plan, повторно проверяет original scope/qualification locators, сохраняет deterministic params_document_id из identity. Резервируется только размер JSON-документа; pending token меняется при resume, completion проверяет token/версию, storage accounting и ready replay. Object IO выполняется существующим artifactjson.Publisher за пределами SQL-транзакций, включая RecoverImmutable после неопределённого PUT.

Это ещё не задача: output reservation, registration, targeted transport, seal и receipt отсутствуют. Отрицательные unit и SQL ACL cases написаны, NOT RUN; 01461 NOT APPLIED. Положительные source fixtures, quota/concurrency/PUT recovery lifecycle остаются обязательными и пока не доказаны.

## Staging replacement negative task

Миграция 01462 и StageNegativeReplacementTask написаны. Dedicated descriptor восстанавливается по свежему draft и опубликованному Params ref; typed inventory и digest пересчитываются. SQL повторно проверяет replacement Params scope, неизменяемый descriptor, ready artifact, exact profile, полный input inventory и live output quota reservation. Создаётся только blocked shell с отдельным v29_negative_replacement_descriptor_jcs. Повторное staging не восстанавливает исчезнувшую или истёкшую квоту молча. Go повторяет physical/source/mapping/qualification locks в транзакции.

Текущие public wrappers v2_input_document_scope, register_v2_task_input_batch, seal_v2_task_inputs и seal_v2_graph дополнены отказом для replacement marker/draft/descriptor без замены предыдущих module guards. Dedicated transport/registration/seal ещё требуются. Миграция прекращается при неожиданной структуре function body, вместо удаления имеющихся проверок. Unit и SQL ACL/refusal tests написаны, NOT RUN; 01462 NOT APPLIED. Положительный staging/quota/concurrency lifecycle не доказан.

## Replacement input transport publication

Миграция 01463 и PublishNegativeReplacementTaskTransport написаны. Перед IO выполняется dedicated staging. Отдельный transport repository допускает только chunk0/manifest с точным owner и body hash/size; на Reserve и Complete повторяет весь LockNegativeReplacementDraft. SQL сверяет документы с immutable replacement descriptor и live blocked task/output reservation через stage replay, проверяет ready chunk перед manifest, quota/token/version/accounting и ready replay. Используется существующий BuildNegativeTaskTransport и artifactjson.Publisher: bounded original inventory, проверка пересечения transport refs с inputs, immutable IO вне SQL и RecoverImmutable.

Input registration и seal не выполняются. Read/recovery опубликованных transport metadata и admission receipt ещё требуются. Отрицательные unit/SQL ACL tests написаны, NOT RUN; 01463 NOT APPLIED. Положительные физические lifecycle, PUT recovery, quota и concurrency tests ещё не доказаны.

## Recovery опубликованного replacement транспорта

Миграция 01464 и ReadPublishedNegativeReplacementTransport написаны. Reader требует существующие blocked task, immutable replacement descriptor и live output reservation до replay validation. Проверяются ready chunk/manifest backend_documents и artifacts, точные sha/size/bucket/version/role/key и срок действия. Go предварительно восстанавливает свежий descriptor, в транзакции повторяет whole-wave physical/mapping/qualification locks, читает metadata и проверяет hashes перед commit. Общий restoreNegativePublishedTransport сохраняет прежнюю проверку ordinary negative транспорта и используется replacement reader: пересчитываются entries SHA, chunk SHA и manifest SHA с точным replacement TaskID; курсор возвращает owned entries.

Написан положительный protocol-only unit с новым TaskID, проверкой независимости cursor и отказами count/SHA/original-task tampering; он не доказывает физическую qualification replacement ветки. Pre-SQL refusal и SQL ACL cases дополнены. Всё NOT RUN; 01464 NOT APPLIED. Input registration/seal/receipt и положительный физический recovery lifecycle остаются открытыми.

## Replacement input registration

Миграция 01465 и RegisterNegativeReplacementInputs написаны. SQL сначала независимо читает exact published transport с existing blocked task/live quota, затем проверяет manifest/ready metadata, восстанавливает direct roots и полный ожидаемый inventory, регистрирует entries и transport refs и запечатывает input closure через private base protocol. Replay допускается только при полном совпадении v2_task_inputs, task.inputs и transport profile; частично заполненный inventory отклоняется. Missing task/reservation не восстанавливаются. Go перечитывает transport, повторяет full physical/source/mapping/qualification locks и выполняет регистрацию в одной транзакции; graph остаётся draft/unsealed.

Unit refusal и SQL ACL cases написаны, NOT RUN; 01465 NOT APPLIED. Положительные physical lifecycle и concurrency не доказаны. Dedicated graph seal/queued receipt и durable coordinator остаются открытыми.

## Replacement graph seal и queued receipt

Миграция 01466 и AdmitNegativeReplacementGraph написаны. Перед seal проверяются exact descriptor, один task без cross-run task_edges, complete input closure, повторная точная регистрация опубликованного транспорта, operation registry/backend и output policies. Dedicated SQL использует прежнюю private graph activation chain, сохраняя quota/events/outbox gates. После seal независимо проверяется v29_negative_replacement_member; отказ вызывает исключение и rollback. Schema2 queued receipt сохраняется атомарно и связывает run/workflow/task, actor/session, review/decision, original task/run, replacement identity SHA, descriptor SHA, qualification и transport. Go удерживает full physical/source/mapping/snapshot/qualification locks до commit и возвращает commit errors без blind retry.

Unit refusal и SQL ACL cases написаны, NOT RUN; 01466 NOT APPLIED. Положительные физические seal/outbox/rollback/concurrency lifecycle не доказаны. Reader/validation исторического receipt и durable wave coordinator остаются следующими этапами; receipt не равнозначен успешному исполнению или завершению whole branch.

## Исторический replacement queued receipt recovery

Миграция 01467 и ReadNegativeReplacementQueuedReceipt написаны. SQL проверяет sealed/noncancelled run, exact saved draft, original frozen intent/snapshot, schema2 namespace/task/run/marker, receipt decision/identity binding, workflow, qualification, task/profile/closure, registered inputs и ready versions всех input artifacts. Он не вызывает stage, reserve или seal. Go требует свежую подготовку ready wave, удерживает full physical/mapping/qualification locks и выполняет strict schema2 parse с независимой сверкой descriptor SHA и транспортных hashes. Receipt.Transport возвращается копией.

Receipt подтверждает прежнюю admission, а не текущий статус/успех задачи или whole branch completion. Изменившаяся qualification/session/source authority приводит к отказу, не к blind retry. Написаны положительный protocol-only unit, identity/review/decision/original task/run/schema/transport tamper и ownership cases, SQL ACL. Всё NOT RUN; 01467 NOT APPLIED. Положительный физический ambiguous commit/concurrency lifecycle и durable multi-operation wave coordinator остаются открытыми.

## Координатор одной negative replacement задачи

Миграция 01468 и ExecuteNegativeReplacement написаны. Probe возвращает absent только после authoritative original intent/snapshot и deterministic ID checks; для существующего run проверяет exact draft/config/hash, owner/session, workflow и marker. Draft и admitted различаются явно; NULL/conflict/stale authority не означает отсутствие. Go компилирует точную ready task по текущей qualification. Для absent/draft выполняет Params → staging/transport → input registration/admission → независимый receipt read. Для admitted выполняет только receipt read. Каждая стадия сохраняет собственные транзакционные проверки. Ошибки, включая ambiguous commit, возвращаются без blind retry; следующий вызов должен заново загрузить proofs и probe с теми же identities.

Unit tests фазового порядка, отказов каждой стадии, отсутствия повторной публикации admitted, cancellation и нулевого acknowledgement при ошибке написаны, NOT RUN; SQL ACL тоже NOT RUN, 01468 NOT APPLIED. Это координатор одной optical.negative задачи, не whole wave completion. Scheduler wiring, свежая загрузка proofs между волнами, adoption/review progression, остальные optical operations и положительные физические crash/concurrency lifecycle остаются открытыми.

## Загрузка исходной replacement ветки

ReadNegativeDependencyPackage и LoadNegativeReplacementInputs написаны. Общий dependency reader теперь возвращает committed full inventory вместе с независимо рассчитанным планом; прежние plan-only/service APIs используют тот же код и сохраняют прежние SQL/ACL checks. Loader для base route читает selected original WorkingImage, все его массивы и frozen Params каждой affected task через существующие проверенные readers. После IO повторно читает весь inventory/plan и требует совпадения canonical package hash перед PrepareNegativeReplacementInputs. Loader не выполняет cancel/stage/publication и не заменяет транзакционные settlement/proof locks.

Unit owned inventory/legacy-plan parity и отказов missing dependency/cancellation написаны, NOT RUN. Миграции в этом шаге не добавлялись. Scheduler wiring, загрузка adopted/retained/unchanged ready-wave sources, service orchestration, остальные optical operations и положительный физический lifecycle остаются открытыми.

## Загрузка inputs готовой replacement волны

LoadMappedReadySources написан: ReadyParentSources проверяет уже физически подтверждённые rebuilt/retained parents, затем loader читает exact affected mapping каждой ready task, повторно строит mapped wave, определяет все недостающие unchanged original bindings и загружает их документы/массивы через существующие scoped readers с numerical verification. Перед возвратом выполняется PrepareCompleteMappedSources; частичная готовая волна не возвращается. requiredUnchangedBindings используется и loader, и WithUnchangedSources, сохраняет frozen порядок, дедуплицирует одинаковые occurrences и игнорирует прежние attachments.

Unit ownership/changed-source/cancellation/refusal cases написаны, NOT RUN. Loader не даёт admission authority: full proof locks сохраняются в последующей транзакции. Автоматическое обнаружение adopted/retained parents, scheduler/service wiring, остальные optical operations и положительный физический lifecycle остаются открытыми.

## Обнаружение принятых replacement родителей

Миграция 01469 и LoadAdoptedReplacementParents написаны. SQL discovery проверяет bounded exact original/task/run tuples, original frozen intent, независимую replacement membership и adopted optical authority. Go сверяет closed sorted subset с независимо восстановленной topology, требует ancestor-closed completion IDs, физически читает оригинальные WorkingImage и массивы каждого rebuilt результата, затем только retained parents следующего frontier. ReadyParentSources повторно проверяет полный набор. Статус succeeded/queued receipt не заменяет adopted/review authority или bytes verification.

Unit subset/empty/ownership, identity/order/duplicate/unknown-field tampering и reader refusal cases, SQL ACL написаны, NOT RUN; 01469 NOT APPLIED. Discovery не доказывает whole branch completion. Reviewed nonnegative adopted authority, scheduler/service orchestration, остальные operation publication paths и положительные физические lifecycle остаются открытыми.

## Сервисный проход negative replacement волны

NegativeService.AdvanceReplacementWave написан. Он повторяет ReconcileNegativeInvalidation и возвращает WaitingForSettlement до завершения cancellation; затем заново загружает full original branch, сверяет план, обнаруживает/физически загружает adopted parents и complete mapped ready sources. Для выбранных negative tasks до первого write вычисляет mapped Params/source, ресурсы в deployment capacity, загружает текущую qualification и authoritative original consumer snapshot через новую миграцию 01470. Публикуется не больше limit (1..100), через ExecuteNegativeReplacement и независимое чтение receipt. При ошибке возвращаются уже подтверждённые receipts с ошибкой; следующий проход снова проверяет durable state.

Total/Adopted/Ready — наблюдения, не whole-branch completion. Не подключённые к publication оптические операции явно перечисляются в Deferred, а не учитываются как реализованные. Unit scope/deployment/cancellation и SQL ACL cases написаны, NOT RUN; 01470 NOT APPLIED. Scheduler wiring, остальные operation publication paths, authoritative whole-branch completion и положительные физические lifecycle остаются открытыми.

## Scheduler adapter и текущая identity

NegativeReplacementScheduler и project wave dispatcher написаны. Adapter сохраняет существующий SQLNegativeScheduler cancellation/skip путь, а для base route разрешает publication только после SQLNegativeReplacementIdentityResolver. Миграция 01471 читает actor/session из persisted original negative run (или replacement draft owner) и требует текущий v2_editor_authorized. Access token не читается, session не создаётся/продлевается. Истёкшая/отозванная session возвращает отказ, не service bypass. Deferred operations возвращают ErrUnqualified, а не успешное завершение ветки.

Adapter unit order/failure/skip/empty identity/deferred и SQL ACL tests написаны, NOT RUN; 01471 NOT APPLIED. Workflow bootstrap ещё не подключён к этому adapter. Recovery UX при истечении session, остальные optical publication paths и authoritative branch completion остаются открытыми.


## V29 optional scheduler bootstrap (2026-10-11)

Workflow startup now selects the replacement adapter when PHOTO_V29_NEGATIVE_CONFIG is provided, using PHOTO_V29_NEGATIVE_CONFIG_SHA256 to pin the exact bounded configuration bytes. Without configuration the existing SQL cancellation/skip backend remains selected. Configured startup checks photo_scheduler identity and installation/EXECUTE privileges for the 16 dedicated replacement functions before building project services. The signature inventory was compared by source reading with migration GRANT declarations; this is not database execution evidence.

Unit tests authored for missing/malformed/mismatched digest, oversized/invalid/schema-empty configuration, absent-config fallback, missing dependencies and cancellation. NOT RUN. No builds, validators or migrations executed. This closes authored bootstrap wiring only, not physical replacement lifecycle qualification. Other optical publication paths, reviewed nonnegative adoption authority, session recovery and authoritative whole-branch completion remain open. Registry 74/16/1 unchanged; whole-product implementation percentage remains unmeasured.


## V29 perspective replacement preliminary draft (2026-10-11)

CompilePerspectiveReplacementDraft and RestorePerspectiveReplacementDraft authored. Dedicated owned packet binds schema2 replacement identities, original consumer snapshot, actor/session, marker, freshly qualified perspective plan/profile, exact original source document, lines document and full task input digest. Restore requires byte-exact canonical packet equality with a new compilation against current prepared physical sources and qualification. Ordinary perspective ordinal run/task draft creation is not reused. Bytes returns owned copies.

Refusal and cancellation unit tests authored NOT RUN. No build/test/validator/migration executed. This is preliminary compiler output only: dedicated SQL persistence/proof locks/Params publication/transport/admission/receipt are still required, as is fresh automatic-lines generation and its persisted authority. No perspective replacement runtime completion credit is claimed. Registry 74/16/1 unchanged; whole-product implementation percent unmeasured.


## V29 manual perspective replacement transactional gate (2026-10-11)

LockManualPerspectiveReplacementDraft authored. It restores the exact fresh packet against complete mapped ready sources, requires a unique matching perspective task, locks the entire physical/mapping/unchanged wave, rechecks original consumer snapshot using dedicated01458 authority, then locks located current perspective qualification in caller transaction. No commit, persistence, reservation or queueing is performed. Automatic mode is explicitly rejected here until fresh-lines producer/object/version authority is wired; preliminary compiler output alone cannot bypass this gate.

Missing-transaction and cancellation unit tests authored NOT RUN. No tests/builds/validators/migrations executed. Dedicated perspective persistence and publication remain open, along with automatic-lines admission and positive physical lifecycle evidence. Registry unchanged74/16/1; whole-product implementation percentage unmeasured.


## V29 operation-bound replacement inventory digest correction (2026-10-11)

Source inspection found perspective preliminary draft incorrectly reused negativeDraftInputDigest, whose operation namespace was hardcoded optical.negative. Introduced opticalDraftInputDigest with explicit supported operation and nonzero task, preserved negative wrapper bytes for valid identities, and changed perspective draft to bind identity.Operation. Authored tests compare the exact wire hash independently, distinguish all four optical operations and task IDs, check negative compatibility and reject unsupported/nil identities. Tests NOT RUN. No validator/build/migration executed. Dedicated perspective SQL persistence was inspected but not implemented in this step; all publication/admission gaps remain open. Whole-product implementation percentage unmeasured.


## V29 manual perspective replacement preliminary persistence (2026-10-11)

Migration01472 and SaveManualPerspectiveReplacementDraft authored. Dedicated function validates closed schema2 identity/packet, scoped settled invalidation plan, original perspective frozen intent/snapshot, deterministic replacement run/workflow/task/Params IDs, marker/identity hashes, manual corners/control bounds, source/Params hashes, current perspective qualification and resource formulas. It independently reconstructs input inventory digest with optical.perspective namespace. It persists only immutable draft run/workflow; exact replay also checks config hash and workflow scope/policy. Generic replacement config marker preserves existing ordinary-publication rejection. No new table or purge allowlist change.

Go store preflights current complete wave and qualification, repeats LockManualPerspectiveReplacementDraft in transaction, saves through dedicated SQL, returns ambiguous commit errors without retry. No object publication or tasks/reservations/queueing. Unit pre-SQL refusal/cancellation and integration ACL/refusal tests authored NOT RUN;01472 NOT APPLIED. Positive lifecycle, recovery reader, Params/transport publication, admission and automatic-lines authority remain open. Registry unchanged74/16/1; product completion percentage unmeasured.


## V29 manual perspective preliminary recovery reader (2026-10-11)

Migration01473 and LoadManualPerspectiveReplacementDraft authored. SQL reader requires original scoped perspective intent/terminal consumer/snapshot, independently derived replacement IDs/marker, draft unsealed noncancelled run, exact config hash/key inventory, workflow/policy, packet actor/session, schema2 operation and manual-only Params. It never creates or repairs a run. Go freshly compiles the selected ready task before SQL, sets scheduler tenant scope, compares exact stored bytes, repeats complete physical-wave/snapshot/current qualification locks, rereads within the same transaction and returns only after successful commit. Sealed/queued recovery must use a future receipt path.

Pre-SQL refusal/cancellation and SQL ACL/refusal tests authored NOT RUN;01473 NOT APPLIED. No tests/builds/validators/migrations executed. Positive physical recovery/concurrency evidence, Params/transport publication, admission and fresh automatic-lines authority remain open. Registry74/16/1 unchanged; whole-product implementation percentage unmeasured.


## V29 manual perspective replacement Params publication (2026-10-11)

Migration01474 and manualPerspectiveReplacementParamsRepository/PublishManualPerspectiveReplacementParams authored. Dedicated SQL scope checks persisted exact preliminary packet, original invalidation/identity/snapshot via dedicated save function and current qualification object locators. Reserve/Complete retain existing document quota, token/version and ready replay protocol. Go derives Params only from freshly restored complete mapped wave, saves preliminary draft before object IO, repeats full physical-wave/snapshot/current qualification locks on reservation/completion, validates exact scope/body hash/size and Params artifact identity, and delegates immutable object publication/recovery to artifactjson.Publisher. Manual-only gate preserved.

Pre-SQL/IO refusal/cancellation and SQL ACL tests authored NOT RUN;01474 NOT APPLIED. No build/test/validator/migration execution. Positive physical publication/recovery/quota/concurrency, task staging/transport/admission/receipt and automatic-lines authority remain open. Registry unchanged74/16/1; whole-product implementation percentage unmeasured.


## V29 manual perspective replacement blocked staging (2026-10-11)

Migration01475 and StageManualPerspectiveReplacementTask authored. Fresh Go descriptor binds replacement task/Params/profile and perspective inventory digest. Dedicated SQL repeats persisted packet/qualification scope, requires ready exact Params document/artifact, independently reconstructs typed input inventory, validates ordered unique references and current ready artifacts, retains exact output reservation, and inserts only a blocked task. Replay requires same descriptor/task/live quota and never silently repairs missing reservation. Shared generic replacement descriptor column and existing ordinary-publication guards are retained. No seal/queue/admission occurs.

Pre-SQL staging refusal/cancellation and SQL ACL/refusal tests authored NOT RUN;01475 NOT APPLIED. No tests/builds/validators/migrations executed. Positive physical quota/concurrency lifecycle, input transport/registration/admission/receipt, automatic-lines and other optical operations remain open. Registry74/16/1 unchanged; whole-product implementation percentage unmeasured.


## V29 manual perspective replacement transport publication (2026-10-11)

Migration01476 and dedicated manual perspective transport repository/documents/publication authored. Staging occurs before immutable object IO. Every Reserve/Complete repeats exact Params/draft and full mapped physical/snapshot/current qualification locks. Dedicated SQL checks chunk0/manifest against staged descriptor and existing blocked task/live quota via stage replay, exact content SHA/size, and ready chunk versions before manifest. Existing document quota/fencing/version/recovery and BuildPerspectiveTaskTransport input-overlap/owned cursor protocol are reused. No input registration or graph seal performed.

Pre-SQL/IO refusal/cancellation and SQL ACL tests authored NOT RUN;01476 NOT APPLIED. No tests/builds/validators/migrations executed. Published-transport recovery reader, registration/admission/receipt, positive physical lifecycle and automatic-lines authority remain open. Registry74/16/1 unchanged; whole-product implementation percentage unmeasured.


## V29 manual perspective published transport recovery (2026-10-11)

Migration01477 and ReadPublishedManualPerspectiveReplacementTransport authored. Dedicated SQL reader requires existing blocked task/live output reservation and exact saved descriptor before scope replay; ready chunk/manifest metadata, versions, object keys and hashes are checked. Go repeats freshly compiled descriptor and full physical-wave/snapshot/current qualification locks in caller-owned transaction. Shared byte/hash verifier is reused for the four manual perspective roots (Params, WorkingImage, Calibration, Report); it independently recomputes entries/chunk/manifest hashes and returns owned cursors. No missing task/reservation repair, object IO or registration.

Pre-SQL refusal/cancellation and SQL ACL tests authored NOT RUN;01477 NOT APPLIED. No tests/builds/validators/migrations executed. A positive physical perspective lifecycle fixture is still missing; existing negative protocol tests are not perspective qualification evidence. Registration/admission/receipt, automatic-lines and other optical operations remain open. Registry unchanged74/16/1; whole-product implementation percentage unmeasured.


## V29 manual perspective replacement input registration (2026-10-11)

Migration01478 and RegisterManualPerspectiveReplacementInputs authored. Dedicated SQL requires independent exact published-transport read before registration, validates manifest bytes/ready object metadata and four direct roots from immutable perspective packet, rejects partial or divergent inventories, then registers and seals the complete input closure through existing private base protocol. Replay requires exact registered rows, task roots and transport profile. Go reads/validates existing transport, repeats fresh descriptor/full physical-wave/snapshot/current qualification locks in registration transaction and returns only after commit. Run remains draft/unsealed; no queue receipt produced.

Pre-SQL refusal/cancellation and SQL ACL/refusal tests authored NOT RUN;01478 NOT APPLIED. No tests/builds/validators/migrations executed. Positive physical lifecycle/concurrency, dedicated graph admission/receipt/coordinator, automatic-lines and other optical operations remain open. Registry74/16/1 unchanged; whole-product implementation percentage unmeasured.


## V29 manual perspective replacement graph admission (2026-10-11)

Migration01479 and AdmitManualPerspectiveReplacementGraph authored. Dedicated SQL rechecks one-task/no-edge graph, exact registered transport closure, operation registry/output policies, current saved scope/qualification and live reservation, then invokes existing private activation/event/outbox seal chain. Independent replacement membership is checked after seal; failure raises and rolls back. Schema2 queued receipt is written atomically in shared replacement receipt column with perspective qualification, exact descriptor/identity/review/decision/original task/run and transport. Go retains full physical-wave/snapshot/qualification locks through commit; ambiguous errors are returned, not retried. Receipt proves admission only, not execution or complete branch recovery.

Pre-SQL refusal/cancellation and SQL ACL tests authored NOT RUN;01479 NOT APPLIED. No tests/builds/validators/migrations executed. Historical perspective receipt reader/coordinator and positive physical seal/outbox/rollback/concurrency evidence remain open, as do automatic-lines and other optical paths. Registry74/16/1 unchanged; whole-product implementation percentage unmeasured.


## V29 manual perspective historical queued receipt recovery (2026-10-11)

Migration01480, ManualPerspectiveReplacementReceipt parser and ReadManualPerspectiveReplacementQueuedReceipt authored. SQL independently verifies original perspective intent, deterministic replacement identities/marker, saved exact packet, sealed noncancelled run/workflow/snapshot, current perspective qualification and exact registered task/closure/transport metadata with ready object versions. Go freshly reconstructs descriptor, retains complete physical-wave/snapshot/current qualification locks and strictly checks schema2 receipt identity/review/decision/original IDs, descriptor/qualification and independently recomputed transport hashes. Transport bytes are owned. Reader performs no staging/publication/admission retry. Historical acknowledgement does not mean execution success or branch completion.

Pre-SQL/parser refusal/cancellation and SQL ACL tests authored NOT RUN;01480 NOT APPLIED. No tests/builds/validators/migrations executed. Positive physical perspective receipt/crash/concurrency fixture and durable coordinator, automatic-lines and other optical paths remain open. Registry74/16/1 unchanged; whole-product implementation percentage unmeasured.


## V29 manual perspective durable publication coordinator (2026-10-11)

Migration01481 and ExecuteManualPerspectiveReplacement authored. Probe distinguishes explicit absent/draft/admitted only after scoped original intent/snapshot, deterministic identities and exact persisted config/workflow checks; errors/stale conflicts never imply absence. Coordinator freshly compiles one selected ready task; absent/draft invokes Params→transport→admission→independent receipt; admitted reads receipt only. Each phase retains its own proof locks. Errors including ambiguous commit are returned without blind retry. Cancellation is checked after receipt callback as well, preventing an acknowledgement after cancellation. This is one manual perspective publication, not scheduler wiring, execution success or whole-branch completion.

Phase/order/failure/skip-admitted/cancellation unit tests and SQL ACL tests authored NOT RUN;01481 NOT APPLIED. No tests/builds/validators/migrations executed. Scheduler/service qualification routing, positive physical lifecycle, automatic-lines, reviewed nonnegative adoption authority, panorama/focus-stack and final branch completion remain open. Registry74/16/1 unchanged; whole-product implementation percentage unmeasured.


## V29 manual perspective replacement service pass (2026-10-11)

PerspectiveService.AdvanceManualReplacementWave and exact PerspectiveProjects tenant/project dispatcher authored. Each bounded pass reconciles original invalidation, waits for settlement, reloads original-version package and adopted parents, rebuilds complete mapped sources and compares independent plan hashes. Manual perspective tasks are preflighted before writes: source proof, recomputed geometry/output bounds, resource budget capped by deployment capacity, current perspective qualification and authoritative original snapshot. Coordinator returns verified queued receipts only. Auto/nonperspective nodes are explicitly Deferred; counters do not finalize branch. Service uses existing deployment-owned perspective coordinator, not request-supplied policy.

Invalid scope/deployment/cancellation unit tests authored NOT RUN. No migrations added/executed; no tests/builds/validators executed. Shared scheduler routing/bootstrap integration remains open; this is a callable service pass only. Positive physical lifecycle, auto-lines, reviewed nonnegative adoption, panorama/focus-stack and authoritative completion remain open. Registry74/16/1 unchanged; whole-product implementation percentage unmeasured.


## V29 optical replacement service dispatcher (2026-10-11)

OpticalReplacementDispatcher authored for existing negative and manual perspective project services, preserving separate qualification/preparation and a shared per-pass publication budget. Negative cancellation/settlement pass runs first; perspective pass reloads proofs independently if deferred work and remaining budget exist. Confirmed receipts of both kinds are retained, including on later failure. Successful second pass reports its current deferred inventory excluding negative tasks already acknowledged in this dispatch. Waiting settlement and exhausted budget do not invoke perspective. NegativeReplacementWavePass now carries separate PerspectiveReceipts.

Budget/settlement/deferred/partial-error unit tests authored NOT RUN. No tests/builds/validators/migrations executed. Workflow deployment bootstrap has not yet constructed this dispatcher; no scheduler runtime credit is claimed. Positive physical qualification, automatic-lines, reviewed nonnegative adoption, panorama/focus-stack and final branch completion remain open. Registry unchanged74/16/1; product implementation percentage unmeasured.


## V29 workflow perspective dispatcher bootstrap (2026-10-11)

Workflow negative replacement bootstrap now optionally constructs OpticalReplacementDispatcher with PerspectiveProjects from existing PHOTO_V29_PERSPECTIVE_CONFIG and exact SHA256 pin. Bounded regular-file read, strict deployment decoder, scheduler role and EXECUTE checks for12 dedicated perspective lifecycle functions precede project construction. Without perspective configuration the negative-only path remains selected. Perspective configuration without required negative deployment is rejected explicitly. No synthesized qualification/session authority. Shared scheduler limit remains1; individual services retain operation policies/capacities.

Decoder/refusal/optional/dependency/orphan-config/cancellation tests authored NOT RUN; existing negative bootstrap tests isolate perspective environment. No tests/builds/validators/migrations executed. This is authored workflow wiring, not deployed or qualified operation. Positive physical lifecycle, reviewed nonnegative adoption, fresh automatic-lines, panorama/focus-stack and branch completion remain open. Registry unchanged74/16/1; whole-product implementation percentage unmeasured.


## V29 accepted perspective parent authority (2026-10-11)

Migration01482 authored. Private v29_accepted_perspective_parent requires the exact current quality review, immutable explicit Apply decision with geometry-reset confirmation/warnings acceptance, exact task result/root/WorkingImage, deterministic accepted geometry asset version and perspective_source provenance in the same tenant/project. Adopted optical parent reader gains this alternative only for review-required perspective results; existing adoption/event/producer/fence/object metadata checks remain. No approval or version is synthesized. Parent WorkingImage/NPY bytes still must be loaded and locked by existing physical proof path.

SQL ownership/ACL tests authored NOT RUN;01482 NOT APPLIED. No tests/builds/validators/migrations executed. Positive acceptance/revocation/tamper/concurrency fixtures remain missing. Existing quality/Apply authority may reject replacement runs until its dedicated producer wiring supports them; this migration does not claim that integration complete. Fresh automatic-lines, panorama/focus-stack and branch finalization remain open. Registry74/16/1 unchanged; whole-product implementation percentage unmeasured.


## V29 perspective execution-plan provenance resolver (2026-10-11)

Source inspection found adopted perspective binding and preview discovery hardcoded ordinary v29_perspective_plan and ordinary queued receipt. Migration01483 adds a private execution-plan resolver: ordinary plans reject mixed replacement config; replacement plans require exact dedicated historical receipt validation and stored actor/session. Both consumers now resolve plan through that gate; existing snapshot/source/version, adoption, producer/fence and quality checks remain. No review approval or source override is introduced.

SQL ACL tests authored NOT RUN;01483 NOT APPLIED. No tests/builds/validators/migrations executed. This closes plan/receipt extraction mismatch only: replacement rebound sources differing from original snapshot still require dedicated source/version authority; existing checks intentionally continue rejecting unsupported contexts. Further quality discovery consumers, positive lifecycle/concurrency and automatic-lines/other optical paths remain open. Registry unchanged74/16/1; whole-product implementation percentage unmeasured.


## V29 replacement perspective source metadata authority (2026-10-11)

Migration01484 authors private v29_perspective_replacement_source. It requires dedicated receipt-validated plan, exact actor/session/task/project, accepted original asset identity in original snapshot, ready current WorkingImage and registered input. Original affected-source inventory determines allowed producers: origin maps only to current selected base; affected parent maps only to independently derived replacement identity plus membership and adopted optical parent metadata; unchanged input requires exact original registered source. It returns exact object locator only, not physical bytes or canvas validation.

SQL ACL tests authored NOT RUN;01484 NOT APPLIED. No tests/builds/validators/migrations executed. The helper is not yet wired into adopted perspective binding; replacement source physical bytes/canvas/arrays and quality/Apply consumers still require integration. No replacement quality/Apply completion claimed. Positive lifecycle/concurrency, automatic-lines/other optical paths and finalization remain open. Registry unchanged74/16/1; product implementation percentage unmeasured.


## V29 adopted perspective rebound source binding (2026-10-11)

Migration01485 replaces adopted perspective binding with explicit split authority: immutable original asset version still must match accepted snapshot/master/project; ordinary tasks use that original source, replacement tasks resolve their actual WorkingImage through private01484 receipt/mapping/selected-base/adopted-parent gate. Params source and registered WorkingImage must match resolved ref. Existing exact task result/attempt/fence/output root, current editor, workflow and adoption event/link checks remain. Preview discovery and quality review consume this existing binding signature. No physical bytes/canvas verification is inferred from metadata alone.

SQL ACL tests authored NOT RUN;01485 NOT APPLIED. No tests/builds/validators/migrations executed. Dedicated downstream physical preview/quality loaders and producer discovery must still be audited for ordinary-only assumptions; positive replacement quality/Apply lifecycle not demonstrated. Automatic-lines, panorama/focus-stack and branch finalization remain open. Registry unchanged74/16/1; product implementation percentage unmeasured.


## V29 preview replacement task identity (2026-10-11)

Source inspection found validatePerspectivePreviewSource enforced ordinary ordinal task IDs, rejecting valid replacement IDs. Migration01486 adds scheduler-only receipt-validated replacement identity observation. Go source reader decodes closed bounded identity metadata in same tenant transaction; validator independently derives review-scoped namespace, per-original-task run/task and workflow IDs. Ordinary ordinal validation remains required when replacement metadata is absent. Unit identity/tamper/decoder tests authored NOT RUN;01486 NOT APPLIED. No tests/builds/validators/migrations executed. This closes one Go identity mismatch, not complete physical preview/quality qualification. Downstream loaders/discovery and positive lifecycle still need audit; automatic-lines/other optical paths remain open. Registry unchanged74/16/1; product implementation percentage unmeasured.


## V29 preview replacement observation value semantics (2026-10-11)

Source inspection found a regression in01486 Go integration: PerspectivePreviewSource repeated-read gates compare structs with !=, but the newly added replacement identity pointer compared addresses rather than content. Changed optional identity representation to a comparable value struct (zero value means ordinary task). Decoder returns owned values; validator retains full deterministic replacement identity checks. Existing bundle/array repeated-read gates now compare exact content, and exported source copies cannot alias retained identity memory. Authored repeated decode/equality/mutation-isolation/change-detection test and adjusted identity tamper cases. Tests NOT RUN. No build/validator/migration executed. Physical preview/quality SQL source-array assumptions and positive lifecycle remain open. Registry unchanged74/16/1; product implementation percentage unmeasured.


## V29 replacement perspective source arrays (2026-10-11)

Migration01487 extends the existing source-array reader. SHA-bound WorkingImage pixels/alpha membership, current preview producer/result/fence, ready immutable object metadata and limits remain mandatory. Ordinary original-version scope remains available. Replacement alternative additionally requires private receipt/mapping source authority matching the actual source and exact same-tenant/project/task registered NpyArray id/hash. No arbitrary task input or source JSON alone authorizes access.

Authored SQL ownership/role ACL test and Go numerical witness test retaining replacement identity, refusing changed receipt identity after IO, and proving returned identity copy isolation. HDR sample remains above1; no clipping or relaxed numerical checks. The Go fixture covers array verifier behavior only, not SQL replacement scope or complete quality/Apply lifecycle. Tests NOT RUN;01487 NOT APPLIED. No builds/typechecks/validators/migrations executed. Positive physical SQL source-array and quality/Apply lifecycle, automatic-lines, panorama/focus-stack and authoritative branch completion remain open. Registry74/16/1 unchanged; whole-product implementation percentage unmeasured.


## V29 perspective replacement downstream startup prerequisites (2026-10-11)

Workflow perspective deployment now requires scheduler EXECUTE and installed signatures for replacement preview identity reader and source-array reader in addition to12 publication lifecycle functions. Missing preview identity installation refuses startup before project construction. Inventory is returned as owned slice; authored prerequisite/duplicate/isolation unit contract NOT RUN. Signature presence cannot prove01487 body applied because the array signature predates that migration; migration application and SQL semantic evidence remain mandatory. No tests/builds/typechecks/validators/migrations executed. Full replacement preview/quality/Apply integration remains unqualified. Registry74/16/1 unchanged; full product percentage unmeasured.


## V29 adopted perspective actual source document (2026-10-11)

Inspection found API Describe/quality/Apply catalog used original asset-version scope for the actual rebound WorkingImage, rejecting valid replacements. Authored01488 source-only API object reader bound to current adopted result/root and exact binding source, immutable ready metadata and65KiB bound. Production catalog now routes Original WorkingImage through this reader; existing SHA/canonical/geometry and repeated locator checks remain. Other original descendants retain existing version authority: replacement reused dense-field authority remains an explicit integration gap. SQL ACL and missing-authority/cancellation unit tests authored NOT RUN;01488 NOT APPLIED. No tests/builds/typechecks/validators/migrations executed. Positive physical API replacement lifecycle and full completion remain open. Registry74/16/1 unchanged; whole-product percentage unmeasured.


## V29 actual source reused dense-field authority (2026-10-11)

Authored01489 API reader for exact dense_warp inverse_field at Document.transform_chain.steps[index].inverse_field in SHA-bound actual source. Requires current adopted binding/root/source, same-tenant/project/task registered NpyArray and ready immutable warp_field metadata. Production perspective source catalog owns verified source bytes and resolves reused fields through this reader; exact Go child ref/parent/contract/path comparison precedes SQL. Existing repeated lookup and geometry checks remain. SQL ACL and undeclared-field/nil-authority unit tests authored NOT RUN;01489 NOT APPLIED. No tests/builds/typechecks/validators/migrations executed. Positive SQL object/field/quality/Apply lifecycle, automatic-lines/other optical paths and final branch completion remain open. Registry74/16/1 unchanged; whole-product percent unmeasured.


## V29 actual source dense occurrence refusal and revocation (2026-10-11)

Extracted narrow production source-object interface to exercise actual source catalog routing independently from database. Nil source authority now fails closed before fallback. Added unit fixture with a valid dense source document: exact parent/path/raw forwarding; mutated path, parent, artifact and extra canvas binding rejected before authority call; repeated lookup propagates revocation without original-version fallback. Tests NOT RUN. This proves no runtime semantics until executed and does not replace positive SQL or complete quality/Apply lifecycle evidence. No tests/builds/typechecks/validators/migrations executed. Registry74/16/1 unchanged; whole-product percentage unmeasured.


## V29 final adopted catalog observation (2026-10-11)

Catalog previously repeated root binding before descendants but did not compare complete binding after all descendant/source IO. Added final full LoadAdoptedPerspectiveResult (immutable root bytes/hash/canonical checks and two SQL metadata observations), then exact binding and canonical root comparison before exposing catalog. Authored unchanged/changed-after-initial-observation catalog fixture tests NOT RUN. No builds/typechecks/validators/migrations/tests executed. Positive SQL and complete quality/Apply lifecycle still open; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 quality review canonical bytes and nonzero result authority (2026-10-11)

Go quality review validation now requires exact canonical report bytes before hash/target/policy/status validation and nonzero valid result/root/WorkingImage references. This applies before ValidatePerspectiveApplyDecision as well. Authored hash-recomputed whitespace, duplicate-status and zero/malformed result binding refusal tests, plus valid report control. Tests NOT RUN. No migrations/builds/typechecks/validators/tests executed. Whole SQL/physical quality/Apply lifecycle remains open; registry74/16/1 unchanged and whole-product percentage unmeasured.


## V29 recovered negative receipts and shared publication fairness (2026-10-11)

Source inspection confirmed negative coordinator re-acknowledges admitted tasks; dispatcher previously charged every receipt against limit1, starving independent ready manual perspective while negative awaited adoption/review. Added nonserialized local RecoveredExisting observation set only after successful admitted probe and independently verified receipt; historical receipt wire/hash unchanged. Negative service preflights complete eligible ready inventory, charges new admissions only and stops at publication limit, allowing recovery to advance past earlier admitted nodes. Dispatcher conservatively charges receipts not explicitly recovered. Authored dispatcher recovered/new limit1 regression test NOT RUN. No tests/builds/typechecks/validators/migrations executed. Perspective-internal repeated receipts, whole branch completion and full physical lifecycle remain open; registry74/16/1 unchanged, product percentage unmeasured.


## V29 perspective recovered receipt publication accounting (2026-10-11)

Manual perspective coordinator now sets nonserialized RecoveredExisting only after successful admitted probe and independent receipt read. Service preflights ready manual perspective inventory and charges new admissions only, allowing already admitted earlier nodes to be acknowledged without starving later nodes under limit1. Dispatcher reports combined new admissions while retaining both receipt kinds. Added local-observation wire-isolation and combined recovered/new perspective budget tests NOT RUN. No tests/builds/typechecks/validators/migrations executed. Positive physical scheduler recovery/concurrency, automatic-lines/other optical operations and authoritative whole branch completion remain open. Registry74/16/1 unchanged; whole-product percentage unmeasured.


## V29 capacity-pending replacement inventory (2026-10-11)

Ready qualified negative/manual perspective tasks beyond new-admission capacity were silently omitted from pass inventories. Added explicit Pending node lists, retaining owned parent slices for remaining preflighted requests when limit is reached. Dispatcher preserves both operation pending inventories including partial-error result, and excludes supported pending nodes from unqualified Deferred classification after successful composition. Pending is scheduling observation, not task completion or durable admission evidence. Authored pending merge/failure/classification regression tests NOT RUN. No tests/builds/typechecks/validators/migrations executed. Full physical lifecycle and authoritative branch finalization remain open; registry74/16/1 unchanged and whole-product percentage unmeasured.


## V29 failed publication pass inventory (2026-10-11)

Negative/manual perspective service passes now retain failed attempted node as Unconfirmed and untouched subsequent preflighted requests as Pending, together with previously independently verified receipts and original error. Unconfirmed does not mean absent, admitted or retryable; a later pass must fresh-probe durable state through existing coordinator. Dispatcher preserves perspective Unconfirmed before returning error and does not retry. Added ambiguous commit/no-synthetic-receipt/pending/confirmed-receipt regression test NOT RUN. No tests/builds/typechecks/validators/migrations executed. Authoritative whole branch completion and full physical lifecycle remain open; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 automatic perspective replacement detection source preflight (2026-10-11)

Extracted mappedReplacementBindings from existing mapped intent rebinding so detector preparation can resolve actual changed source without inventing a future line artifact. Existing RebindMapped preserves its unchanged-control branch and fresh-line requirements. Added PerspectiveDetectionSource for proven mapped automatic perspective only: independently checked persisted map/physical parent bindings, exact original base match, actual source WorkingImage validation and canvas digest; returns owned source bytes, no old/synthetic lines and no publication authority. Refusal/cancellation tests authored NOT RUN. Detector graph/source SQL authority, fresh detected-line adoption and automatic replacement publication lifecycle remain open. No tests/builds/typechecks/validators/migrations executed. Registry74/16/1 unchanged; product percentage unmeasured.


## V29 automatic replacement detector plan compilation (2026-10-11)

Added workflow CompilePerspectiveReplacementDetectionPlan consuming only PreparedMappedReplacementStep source preflight. Source preflight now carries verified frozen tenant/project/review/decision/editor identity and validates node-original identity. Detector run derives from replacement run using V29.PerspectiveReplacementDetection/v1; existing CompilePerspectiveLinesPlan computes task, params, resource profile and full source/NPY/dense provenance inputs against actual source ICC. Closed canonical descriptor binds original/replacement node, editor/review/decision, detector identities, source/params/profile bytes. This is compilation only; dedicated source SQL admission is required and ordinary accepted-version admission is not bypassed. Unproved/cancelled/copy-isolation unit tests authored NOT RUN. Detector persistence/transport/seal/adoption and fresh-line automatic execution remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector descriptor restoration (2026-10-11)

Replaced map-only detector descriptor with typed closed schema. Bounded decoder requires canonical exact fields, current descriptor kind/version/nonzero scope/editor/original identities, independently derived detector run, SHA-bound source and freshly recomputed existing detector task/params/resource profile. Restore then recompiles against freshly prepared mapped replacement proofs and compares whole descriptor; saved metadata cannot become source authority. Added valid compiler fixture plus altered run/task/profile/params/source hash/actor and valid-saved-without-proof refusal tests NOT RUN. Dedicated SQL detector persistence/transport/seal/adoption remains open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, product percentage unmeasured.


## V29 detector descriptor replacement topology identity (2026-10-11)

Before persistence inspection found structural descriptor decoding derived detector IDs but accepted replacement node IDs as supplied. Decoder now independently derives review-scoped namespace and replacement run/task from tenant/project/review/decision/original IDs. Parent identity checks match existing topology compiler: replacement UUIDv5, retained original task, selected_base null task; rejects duplicate/self/unknown parents. Compiler validates its own emitted descriptor before exposing it. Updated compiler fixture to actual derived identities and added altered review/replacement task/original task/parent refusal cases NOT RUN. This is structural validation only; current mapping/physical proofs still mandatory at restore/admission. SQL persistence not yet authored. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, product percentage unmeasured.


## V29 replacement detector private SQL identity gate (2026-10-11)

Authored01490 private lock_v29_replacement_detector_identity. Retains existing complete replacement-scope/review locks, reads locked original perspective Params locator and verifies supplied original bytes/hash/size and auto mode. Closed descriptor/node fields and scope/editor/review match required; independently derives replacement run/task, independent detector run and source-ICC/params-byte seeded detector task. Source hash and CPU float32 resource formula validated. This is identity/preliminary validation only: mapped physical source/array authority and node parent mapping must still be independently locked before persistence/publication; function is not exposed to scheduler/API and currently has no publication caller. SQL ACL test authored NOT RUN;01490 NOT APPLIED. No tests/builds/typechecks/validators/migrations executed. Detector persistence/transport/seal/adoption and automatic execution remain open; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 full detector mapped-source preparation locks (2026-10-11)

Authored01491 scheduler-only lock wrapper combining private01490 identity review gate with exact original snapshot lock. Go PreparedNegativeReplacementInputs.LockPerspectiveDetectionPreparation independently selects exactly one ready automatic perspective, compares complete node/base/actual WorkingImage to descriptor, then retains existing full wave mapped physical arrays/parents/source locks before SQL identity/snapshot gate. Workflow LockPerspectiveReplacementDetectionPlan strictly decodes before SQL and restores/recompiles whole descriptor after locks in caller-owned tx. No persistence/reservation/queue/commit performed. Refusal/cancellation unit and SQL ACL tests authored NOT RUN;01491 NOT APPLIED. Dedicated detector persistence/transport/seal/adoption remains open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector preliminary draft persistence (2026-10-11)

Authored01492 dedicated draft save after01491 review/identity/original snapshot lock and exact ready actual-source metadata. Deterministic detector run uses existing negative marker to reject generic admission, distinct replacement detector descriptor config, immutable context guard and exact retry with no tasks. No new tenant table, output quota, tasks/workflow or queue created. Go SavePerspectiveDetectionDraft retains full mapped physical wave locks; workflow strictly decodes and freshly restores complete descriptor in caller-owned transaction, requiring rollback on any error. SQL alone does not establish complete physical mapping authority; dedicated Go lock path remains mandatory before future publication. ACL and refusal/cancellation tests authored NOT RUN;01492 NOT APPLIED. Detector saved-draft reader, transport/seal/adoption and actual scheduler integration remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, product percentage unmeasured.


## V29 replacement detector saved draft read (2026-10-11)

Authored01493 exact existing preliminary detector draft reader. Repeats dedicated identity/review/original snapshot and ready source metadata checks, validates complete stored config/hash/actor/snapshot/draft/noncancelled/unsealed state and absence of tasks; returns exact descriptor only. No draft insertion, reservation, repair or admission. Go repeats full mapped physical-wave locks, requires exact saved expected bytes and returns owned raw; workflow strict decoder and fresh current source recompile follow in same caller transaction. Refusal/cancellation and SQL ACL tests authored NOT RUN;01493 NOT APPLIED. Publication documents/transport/staging/seal/adoption/scheduler integration remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector publication packet compiler (2026-10-11)

Added PreparePublication on owned replacement detector plan. Requires structurally validated descriptor and exact detector tenant/project/run/base/task/source/params/profile match, then existing PreparePerspectiveLinesTask and Entries recompile task input digest/profile/typed roots. Params document identity derives UUIDv5(task,params); artifactjson scope owned by exact detector task. Returned document bytes, task inputs and entries are owned copies. Refactored test fixture and authored task/scope/typed-root/deterministic document/copy isolation/mismatched plan/cancellation tests NOT RUN. This prepares publication only; no generic repository fallback, object writes or queue credit. Dedicated artifactjson reserve/complete repository and transport/seal/adoption/scheduler remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector Params publication (2026-10-11)

Authored01494 dedicated Params scope/reserve/complete using existing backend_documents/artifactjson immutable lifecycle. Exact saved detector draft/review/original scope and Params hash/size/owner required; deterministic UUIDv5(task,params), storage quota, fenced pending resume, exact ready version replay and accounting preserved. Go ReplacementDetectionDocuments implements artifactjson.Repository, retaining full current mapped physical-wave/source/snapshot and fresh descriptor checks in each reserve/complete transaction; existing Publisher performs object IO between transactions. Publish returns exact compiled task only after confirmed document ref, no activation. Added pre-SQL scope/size/version/cancellation tests and SQL ACL tests NOT RUN;01494 NOT APPLIED. Positive object publication/recovery/concurrency, output reservation/transport/staging/seal/adoption/scheduler integration remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, product percentage unmeasured.


## V29 replacement detector output reservation lifecycle (2026-10-11)

Authored01495 separate4MiB output run reservation using existing tenant storage accounting and exact saved detector proof. Live same-owner/size reservation replay only; expired/conflicting state rejected without renewal/repair. Go ReserveOutput repeats full current mapped source/array/snapshot/draft locks and Publish requires it before Params object IO. Authored01496 scheduler release accepts terminal run only after all tasks/output uploads/backend documents settled or cleaned, with exact quota accounting and idempotent released state. Release SQL still needs terminal scheduler cleanup integration. Cancellation/missing writer and SQL reserve/release ACL tests authored NOT RUN;01495/01496 NOT APPLIED. Transport/staging/seal/adoption and full detector scheduler lifecycle remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector terminal quota scheduler cleanup (2026-10-11)

Authored01497 dedicated cross-tenant scheduler next-terminal-reservation sweep with tenant context restoration and existing01496 terminal/task/upload/document gates. Candidate selection does not acquire run/quota locks before tenant locks; release owns existing tenant→usage→run lock order and idempotence. Workflow startup now requires installed/EXECUTE cleanup function and starts independent bounded5s cleanup expansion loop, separate from ordinary line detector enablement. Bounded true/idle/database-error/cancellation/missing DB and SQL ACL tests authored NOT RUN;01497 NOT APPLIED. This wires quota cleanup only, not detector execution finalization or transport/adoption. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector blocked task stage (2026-10-11)

Authored01498 draft reader accepting only exact blocked unadopted/noncancelled detector task identity/Params/profile for staging recovery; active/completed/foreign task state rejected. Authored01499 dedicated stage: full saved detector proof, exact ready immutable Params replay, live4MiB output reservation, source-derived input order/dense provenance, existing task_input_v1 digest, CPU/profile/registry checks, exact blocked task retry. No seal/events/outbox/dispatch. Go Stage compares complete compiled PreparedTask before DB and repeats current full physical source/draft proof within caller transaction helper. Pre-SQL task tamper/cancellation and SQL ACL tests authored NOT RUN;01498/01499 NOT APPLIED. Transport/input registration/seal/adoption/runtime scheduler remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.

## V29 replacement detector input transport compiler (2026-10-11)

Added PerspectiveReplacementDetectionPlan.BuildInputClosure. Revalidates complete descriptor/compiler packet and requires its exact deterministic Params ref before publication. Existing V2 single-chunk/manifest builder is wrapped with exact source-derived transport document comparison before each publisher call; manifest requires successfully returned chunk hash, output refs cannot alias input roots or each other. Cancellation, foreign Params, malformed chunk and premature manifest refusal tests authored NOT RUN, plus full two-document compilation fixture. This is transport compilation only: dedicated replacement transport repository, durable registration, seal, adoption and scheduler execution remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, product percentage unmeasured.


## V29 replacement detector SQL transport publication (2026-10-11)

Authored01500 private exact chunk/manifest reconstruction from blocked replacement detector task, using dedicated01499 materialization/proof checks. Scheduler-only reserve/complete retain tenant/storage/run locks, exact saved replacement descriptor, live4MiB run reservation, immutable transport bytes, ready chunk object/version before manifest and existing per-document storage accounting. Existing immutable transport column/guard reused. No ordinary detector source authority fallback and no seal/dispatch. SQL ACL integration test authored NOT RUN;01500 NOT APPLIED. Dedicated Go repository with repeated current physical source locks, transport registration/seal/adoption/scheduler remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector Go transport publication (2026-10-11)

Added dedicated replacement transport artifactjson repository and ReplacementDetectionDocuments.PublishInputClosure. Reuses exact source-derived chunk/manifest compilation and existing immutable object publisher, with01500 reserve/complete only. Each transaction repeats current full physical mapped-source/review/snapshot/draft proof and fresh descriptor restoration; object IO stays outside transactions. Scope/hash/size/object-key/version/format/cancellation rejected before SQL. Unit refusal tests authored NOT RUN; positive publication/recovery/concurrency remains unverified. Durable closure registration/seal/adoption and detector scheduler execution remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector ready transport admission (2026-10-11)

Authored01501 scheduler-only read_v29_replacement_detector_transport_admission: requires both ready immutable documents, reconstructs exact source-derived bodies via dedicated replacement task/proof gate, validates live ready artifact locators/versions and returns complete stored closure packet. No registration/workflow/seal/queue mutation. Go ConfirmInputClosure validates entire owned closure before SQL, repeats full current physical mapped-source/review/snapshot locks and compares expected packet with stored SQL JSONB; missing/mismatched packet is stale. Malformed closure/count/manifest/cancellation unit and SQL ACL tests authored NOT RUN;01501 NOT APPLIED. Dedicated workflow policy, source IO witnesses, registration/seal/adoption/scheduler remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector draft workflow policy (2026-10-11)

Authored01502 dedicated immutable replacement execution-policy column/guard and scheduler-only deterministic preview workflow creation. Requires current exact ready transport admission and existing blocked replacement task/source/snapshot proof, fail-fast draft run, exact CPU float32 detector policy and source ICC/runtime digest. Existing workflow retry requires exact identity/snapshot/policy bytes/hash; no policy replacement or activation. Go CreateDraftWorkflow validates policy and complete closure before SQL, repeats physical mapped-source locks and compares actual ready transport before creation in the same transaction. Foreign policy/closure/cancellation unit and SQL ACL tests authored NOT RUN;01502 NOT APPLIED. This draft policy is not source IO evidence or photographic qualification. Source IO witnesses, registration/seal/adoption/scheduler remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector stream-verification witness (2026-10-11)

Added replacement plan VerifyInputs and private VerifiedReplacementDetectionInputs. Reuses existing bounded WorkingImage byte equality, versioned object locators, RGB/alpha NPY shape/dtype/hash/numeric validation, close errors and post-IO locator rechecks. Actor/session derive from strict replacement descriptor; witness binds exact complete descriptor and snapshot, and AdmissionJSON rejects original/foreign/zero/unverified packets. Catalogs must resolve actual mapped replacement source; this adapter does not supply or weaken their SQL authority. Missing IO/snapshot, mismatched descriptor/snapshot, unverified witness and cancellation unit tests authored NOT RUN. Actual mapped catalog adapter and SQL witness comparison/registration/seal/adoption/scheduler remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector actual input catalog (2026-10-11)

Authored01503 scheduler-only input object metadata reader for exact descriptor WorkingImage/pixels/alpha membership and ready immutable artifact locators. Added private replacement catalog with exact run/base/actor/session/snapshot/source bytes and array spec checks; each resolution repeats full mapped physical source/review/snapshot proof in a dedicated transaction. VerifyInputObjects wires this catalog to existing stream verification and post-IO locator rechecks. SQL alone is metadata membership, not full physical mapping authority; mandatory Go locks retained. Foreign run/snapshot/actor/missing reader unit and SQL ACL tests authored NOT RUN;01503 NOT APPLIED. SQL witness comparison/registration/seal/adoption/scheduler remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector typed input registration (2026-10-11)

Authored01504 private IO-witness gate comparing exact task/actor/session/snapshot, current source and ordered RGB/alpha object locators via01503, and complete ready transport via01501. Scheduler-only registration requires exact stored closure and deterministic dedicated workflow/policy hash, unique full typed inputs, exact idempotent row order/project; inserts only after all checks. Go RegisterInputs rejects unverified/foreign closure/witness before SQL and retains full current mapped physical-source locks in the registration transaction. No ready/seal/outbox/dispatch. Refusal/cancellation unit and SQL ACL tests authored NOT RUN;01504 NOT APPLIED. Positive lifecycle/concurrency unverified; seal/adoption/producer authority/scheduler integration remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector initial transport seal (2026-10-11)

Authored01505 dedicated initial blocked transport seal. Repeats01504 complete registration/current IO locator and dedicated workflow checks, requires untouched attempt-zero blocked task, compiles exact worker manifest+Params wire/profile with contracts/runtime/policy hashes, and asserts existing v2_input_closure_ready or raises to roll back. Go SealTransport validates full owned closure and private replacement IO witness before SQL and repeats current physical mapped source locks in the same transaction. Task remains blocked/run draft; no queue activation. Refusal/cancellation unit and SQL ACL tests authored NOT RUN;01505 NOT APPLIED. Durable sealed replay is not yet supported: original draft reader/staging/physical preparation gates reject the changed sealed profile; recovery and fresh authority after seal must be implemented before scheduler integration. Activation/adoption/producer authority remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector durable blocked seal receipt (2026-10-11)

Authored01506 immutable bounded replacement seal receipt, persisted atomically after successful initial transport seal. Prior initial function is private; scheduler uses receipt-writing wrapper. Dedicated historical reader checks exact stored descriptor, draft/noncancelled run, attempt-zero blocked task, ready Params, exact sealed wire/profile policy pins and v2_input_closure_ready. Receipt acknowledges historical seal only and does not supply fresh source authorization. SQL ACL test authored NOT RUN;01506 NOT APPLIED. Fresh source preparation reader still rejects sealed profile and must be extended before Go recovery/activation; receipt alone does not resolve that gap. Activation/adoption/producer authority/scheduler remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector sealed draft recovery (2026-10-11)

Authored01507 extending exact draft read to accept only an original staged profile or exact durable blocked sealed profile validated by01506 receipt reader. All existing current review/source/snapshot/config identity checks remain; malformed/nonmatching receipt rejects even otherwise matching original profile. Seal wrapper now repeats current dedicated Params/source draft gate and acknowledges exact descriptor/closure/IO witness receipt without rewriting task/profile. Existing Go SealTransport repeats full current mapped physical-wave locks before this SQL path, enabling blocked sealed retry through the same method. Receipt is not graph activation or stale-source permission. SQL recovery ACL test authored NOT RUN;01507 NOT APPLIED. Positive recovery/concurrency remains unverified. Activation/adoption/producer authority/scheduler integration remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector initial graph activation (2026-10-11)

Authored01508 private sealed source gate: exact durable blocked receipt/witness, current source/RGB/alpha locators, live4MiB quota and locked ready Params/chunk/manifest object versions. Scheduler-only activation requires one untouched blocked CPU detector task and no edges/expansions, then invokes existing private V2 graph/outbox/event mechanism with registry/backend/output/closure gates retained. Go Activate requires private replacement IO witness and repeats full mapped physical-source locks before SQL. No registry enablement or result adoption bypass. Refusal/cancellation unit and SQL ACL tests authored NOT RUN;01508 NOT APPLIED. Initial activation only: committed activation retry still needs durable admission recovery because draft preparation rejects active run. Producer authority/adoption/scheduler integration remain open, so full runtime cycle is not credited. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector durable admission acknowledgement (2026-10-11)

Authored01509 immutable bounded admission receipt stored atomically after successful graph activation, with private prior activation function. Historical scheduler-only reader requires exact descriptor/run tenant/actor/snapshot, sealed graph and deterministic workflow, returns exact original queued acknowledgement regardless of later run outcome. Go WasAdmitted validates complete plan/context, scheduler role/tenant tx, bounded canonical receipt equality and distinguishes absent receipt from malformed data; no source IO, quota renewal or queue mutation. This is historical acceptance only, never fresh source/result authority. Missing snapshot/cancellation unit and SQL ACL tests authored NOT RUN;01509 NOT APPLIED. Scheduler must call this before draft-only preparation to recover committed activation; that wiring, producer authority and result adoption remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector preparation/recovery orchestration (2026-10-11)

Added PrepareAndActivate for already saved exact replacement detector draft. Validates operator policy, probes historical admission before draft-only work, freshly verifies source RGB/alpha IO, and probes exact blocked seal witness+policy under current full mapped-source locks. Sealed recovery skips document/task reconstruction and activates; unsealed path publishes Params, stages task, publishes closure, freezes workflow policy, seals then activates. Returns recovered-existing admission separately from newly activated; missing/conflicting source/policy/quota is not repaired. Refusal/cancellation unit tests authored NOT RUN. This is orchestration method only, not scheduler discovery/bootstrap wiring. Positive partial-failure/recovery/concurrency unverified; producer authority/result adoption and actual scheduler integration remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector idempotent draft entry (2026-10-11)

Inspection confirmed optical dispatcher defers auto perspective and workflow preparation previously assumed a separately saved draft. Authored01510 ensure function reads exact current staged/sealed draft first, otherwise invokes original non-overwriting save. Go EnsurePerspectiveDetectionDraft retains full mapped physical source locks, workflow EnsureDraft enforces scheduler/tenant transaction and fresh full descriptor restoration before commit. PrepareAndActivate now calls EnsureDraft after historical admission probe and before source IO, allowing missing draft creation and exact partial preparation recovery without overwriting conflicting/active state. Missing snapshot/cancellation unit and SQL ACL tests authored NOT RUN;01510 NOT APPLIED. Actual auto-perspective dispatcher/bootstrap wiring still absent; producer authority/result adoption remain required before full runtime credit. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector adoption evidence boundary (2026-10-11)

Authored01511 dedicated result evidence gate tying adoption context attempt/fence/result/root, exact Params and policy/profile pins, durable admission, current review/replacement/snapshot identity and version-pinned source/array locators. Added PerspectiveDetectionCurrentProof retaining complete mapped physical source locks without draft-only read, and caller-transaction AdoptionEvidence restoring full descriptor after locks. Evidence is metadata only: stream result validation and adoption commit remain required. Missing tx/cancellation unit and SQL ACL tests authored NOT RUN;01511 NOT APPLIED. No automatic adopter wiring or commit, no ordinary source-authority fallback. Scheduler/dispatcher integration and result adoption remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector pinned replay preparation (2026-10-11)

Extracted existing ordinary detector closed adoption evidence validation into a reusable pure validator without changing ordinary SQL loader/checkpoint behavior. Added replacement replay preparation binding canonical closed descriptor/task/run/base/Params/source SHA and operator installation to freshly loaded evidence; existing pinned detector replay verifies result content and rechecks complete evidence around IO. Caller must provide authoritative current mapped-source evidence loader; no ordinary SQL fallback. Missing loader/foreign malformed descriptor/cancellation tests authored NOT RUN. Automatic replacement evidence loader and final adoption commit are not wired. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector dedicated adoption commit (2026-10-11)

Authored01512 dedicated immutable result-adoption receipt and SQL commit: compares current01511 evidence and exact root/hash/base/status, preserves existing private V2 inventory/inbox adoption mechanism, stores exact evidence/result/Params/inventory/descriptor binding and supports exact receipt replay. Python statement selector reuses prepared detector hash/attempt/fence checks and binds complete replacement proof/descriptor before selecting11-argument dedicated commit. It is statement construction only; executor must retain full mapped physical-source locks and authoritative proof. During source inspection fixed incorrect9-argument GRANT/REVOKE signatures in01123 to match declared10-argument ordinary function;01512 uses exact11-argument grants. Unit statement binding/tamper and SQL ACL tests authored NOT RUN;01512 NOT APPLIED. Authoritative evidence loader/final executor and scheduler/dispatcher wiring remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector Go final commit boundary (2026-10-11)

Added caller-transaction CommitAdoption: bounded canonical inventory/evidence/root, task/run/result/attempt/fence/Params/root hash binding, scheduler role and exact tenant transaction context, full current mapped-source physical proof and fresh descriptor restoration before01512 commit. Caller must perform existing pinned replay first and rollback on any error; this boundary does not itself attest replay or commit the transaction. AdoptionEvidence now also checks scheduler/tenant transaction and attempt/fence bounds before physical locks. Missing caller tx/cancellation unit tests authored NOT RUN; positive binding/rollback lifecycle remains unverified. Actual Python↔Go authoritative evidence/commit bridge and scheduler/dispatcher wiring still absent. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector adoption transaction service (2026-10-11)

Added bounded scheduler-owned ReadAdoptionEvidence and CommitReplayedAdoption transaction methods around existing caller-tx physical source/evidence/commit boundary. Evidence snapshots finish before replay; final commit repeats complete physical authority and retains locks through commit, rolls back on error/cancellation. Incoming packet sizes bounded before owned copy. No object IO or replay inside transaction, no stale snapshot permission. Invalid attempt/cancellation tests authored NOT RUN; positive rollback/commit/recovery still unverified. These are service methods for a bridge, not a deployed Python↔Go transport. Bridge/provider installation, scheduler/dispatcher integration and durable adoption recovery remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector adoption recovery acknowledgement (2026-10-11)

Authored01513 scheduler-only historical adoption acknowledgement requiring exact descriptor/snapshot, active tenant, sealed noncancelled run, succeeded task with matching receipt result/attempt/fence, v2-adopt inbox event and matching v2.task.adopted inventory/sql digest event. Added Go WasAdopted bounded scheduler transaction wrapper, no source IO or mutation. This acknowledges detector adoption only and cannot authorize downstream perspective Apply or replace fresh source checks. Missing snapshot/cancellation unit and SQL ACL tests authored NOT RUN;01513 NOT APPLIED. Positive lost-response recovery remains unverified. Python↔Go bridge/provider and scheduler/dispatcher integration still absent. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector scoped Unix adoption bridge server (2026-10-11)

Added Go job-scoped adoption bridge server, caller-owned Unix listener only, cryptographic32-byte job bearer token and constant-time comparison. Canonical closed bounded requests bind exact tenant/run/task; evidence, commit and historical adopted operations invoke existing scheduler transaction services, with no public API mounting or DB credentials in payload. Generic errors/no-store responses and bounded HTTP lifecycle; invalid token/scope/route/noncanonical packets unit tests authored NOT RUN. Server is not started or installed yet; Python client/job launcher, operator provisioning and scheduler/dispatcher wiring remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector Unix Python bridge client (2026-10-11)

Added job-scoped Python AF_UNIX HTTP client with absolute short socket path, socket-type check, token/UUID/timeout validation, bounded canonical closed envelopes/responses and connection cleanup. Evidence uses existing strict validator; commit uses prepared replay hash/attempt/fence/root checks. replay_and_adopt probes durable adoption, executes pinned replacement replay, verifies typed inventory, rechecks current evidence then calls Go commit; ambiguous transport/adoption response invokes read-only receipt recovery, never automatic duplicate commit. Cancellation checked around IO. Client scope/canonical request/cleanup/nonboolean recovery/cancellation tests authored NOT RUN. Job launcher and operator installation, actual scheduler/dispatcher selection remain absent; positive Unix transaction/replay lifecycle unverified. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector scoped bridge job lifecycle (2026-10-11)

Added ReplacementDetectionBridgeJob.Run: operator absolute non-symlink non-writable-by-group/other root, private0700 ephemeral job directory,0600 bounded Unix socket, cryptographic job token supplied directly to runner, bounded lifetime and server/runner cancellation, listener and directory cleanup. HTTP requests inherit job context. Runner must finish configured Python process before returning; function does not yet provide a Python process launcher or credentials/config transfer. Invalid root/duration/cancellation tests authored NOT RUN. Positive socket permissions/cleanup/timeout lifecycle remains unverified. Concrete runner/operator provisioning and scheduler/dispatcher selection remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector concrete Python adoption job (2026-10-11)

Added canonical bounded stdin job entrypoint using existing adopter endpoint/config validation, pinned detector installation, explicit scheduler autocommit DB artifact reader and bounded versioned object-store IO. Job performs scoped Unix bridge replay/inventory/evidence/commit/recovery; generic failure exit avoids credential/token logs. Added Go PythonReplacementDetectionAdopter runner: absolute interpreter/worker roots, explicit allowlisted environment (no parent inheritance), capability only via stdin, bounded process lifetime/output, private bridge lifecycle and exact response parsing. Invalid process/cancellation and canonical job scope/bounds tests authored NOT RUN. Concrete runner exists but no operator bootstrap or scheduler/dispatcher job discovery wiring yet; positive cross-process lifecycle unverified. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector dedicated adoption discovery (2026-10-11)

Authored01514 dedicated UUID-cursor next pending detector adoption query using exact current v2_adoption_context, admission marker and absent adoption inbox/receipt. Ordinary pending adoption query excludes replacements before LIMIT64, preserving all original predicates and preventing fallback into ordinary detector authority without starving ordinary tasks. Go queue strictly decodes full canonical replacement descriptor and validates tenant/run/task/snapshot/attempt/fence/result/root and forward cursor. Discovery is not physical source authority. Missing DB/cancellation unit and SQL ACL tests authored NOT RUN;01514 NOT APPLIED. Scheduler runner must still reload decision and complete mapped-source proofs, provision operator and wire queue loop; those remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector current adoption proof loader (2026-10-11)

Authored01515 scheduler-only exact saved descriptor/snapshot→original review decision reader with existing live principal authorization and current base-route decision hash. Added adoption loader that reconstructs original-version physical input package, verifies authorized original snapshot, reloads adopted parents/mapped sources and freshly restores exact detector descriptor from one matching ready step. Exposed existing read-only original snapshot method for operation-specific recovery. Candidate metadata is not authority; final evidence/commit still repeat full physical locks. Missing reader/cancellation unit and SQL ACL tests authored NOT RUN;01515 NOT APPLIED. Queue processing/operator bootstrap/actual scheduler loop and downstream auto-perspective publication remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged, whole-product percentage unmeasured.


## V29 replacement detector adoption dispatcher (2026-10-11)

Added ReplacementDetectionAdoptionDispatcher.ProcessOne joining dedicated cursor discovery, full current physical source loader and pinned Python adoption runner. Bounded query/job contexts, strict candidate identity/bounds and exact restored descriptor/snapshot checks before replay. Cursor advances on failed work and wraps on empty page; concurrent ticks skip instead of accumulating behind replay. Durable adoption remains final idempotency authority. Authored unit tests for failed-source fairness, wrap, recovered acknowledgement, packet forwarding, cancellation and concurrent tick exclusion; NOT RUN. Operator bootstrap, actual scheduler loop, positive cross-process lifecycle and downstream auto-perspective publication remain open. No tests/builds/typechecks/validators/migrations executed; registry74/16/1 unchanged and whole-product percentage unmeasured.


## V29 replacement detector adoption scheduler installation (2026-10-11)

Added independent opt-in operator environment parser, explicit child environment and paths/resource budgets, scheduler SQL authority startup checks, queue/physical-loader/Python runner factory. RunScheduler now composes and starts dedicated five-second loop, includes enabled loop readiness and cancels/waits on shutdown. Authored config/dependency/cancellation unit tests NOT RUN and v29-replacement-detector-adoption-operator.md deployment contract. No tests/builds/typechecks/validators/migrations executed. Upstream automatic replacement detector preparation and downstream fresh-line auto-perspective publication remain open; deployment and positive cross-process behavior unverified. Registry74/16/1 unchanged; product percentage unmeasured.


## V29 automatic replacement detector preparation wave (2026-10-11)

Added selection-only AutomaticPerspectiveDetection and workflow ReplacementDetectionWaveService.Advance. Reconciles settlement then reloads complete original/mapped/adopted source package; validates every selected auto descriptor, policy and snapshot before object writes; invokes existing dedicated PrepareAndActivate. New admission limit excludes recovered historical receipts; failure preserves confirmed admissions, unconfirmed node and pending inventory. Detector admission never marks replacement branch complete. Cancellation/dependency/selection tests authored NOT RUN. Common replacement dispatcher and operator preparation bootstrap still open, as is downstream detected-line perspective publication. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 automatic detector preparation dispatcher wiring (2026-10-11)

Added workflow wrapper around common negative/manual perspective dispatcher and optional scheduler bootstrap preparation flag; working ICC passed from scheduler and policy pins detector runtime. Shared publication budget counts new detector admissions; retained Deferred means no prerequisite is promoted to completed perspective. Added separate DetectorAdmissions/DetectorPending/DetectorUnconfirmed wave evidence. Unit tests for budget, partial failure evidence, cancellation and unfinished inventory authored NOT RUN. Updated operator contract. Downstream fresh-line perspective publication and full installation/SQL qualification remain open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 replacement detector adopted lines observation (2026-10-11)

Authored01516 scheduler-only dedicated lines observation with current preparation identity/snapshot gate, durable adoption acknowledgement, exact task result attempt/fence, ready versioned producer-bound JSON artifact and workflow link, receipt SHA and mapped base equality. Added Go ReadDetectedLines scheduler transaction with current complete physical source locks, descriptor restoration, strict existing detected-lines parser and full base equality. Unsupported remains explicit; no old/synthetic lines fallback. Cancellation/missing-dependency unit tests authored NOT RUN;01516 NOT APPLIED. This is read-only observation, not auto-perspective admission authority. Final publication must re-lock lines/current source. Auto-perspective publication and installation qualification remain open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 transaction-bound lines and segment contract (2026-10-11)

Refactored ReadDetectedLines through LockDetectedLines caller transaction boundary: scheduler/tenant check, complete current physical proof and descriptor restoration, producer/result/artifact SHARE locks retained through caller commit. Private owned LockedReplacementDetectedLines exposes cloned result bytes and strict available-only LinesDocument compilation. Existing perspective expects a segment document distinct from detector status/root; root is not reused as a segment artifact. Missing transaction/zero evidence/owned bytes tests authored NOT RUN; modified01516 remains NOT APPLIED. Dedicated segment publication and repeated final admission checks remain open; no auto-perspective completion claim. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 replacement segment immutable publication plan (2026-10-11)

Added private ReplacementDetectionSegmentsPlan with deterministic versioned owner/artifact bound to adopted detector result/root and canonical segment hash, exact descriptor/snapshot/base, cloned bytes and separate task_params scope. PrepareSegmentsPlan reads current proof; LockSegmentsPlan reacquires complete physical/producer/result/object locks and reconstructs exact identity/content before each future mutation. Positive authored unit cases cover deterministic replay, result identity change, byte ownership, detector-root separation, foreign snapshot, missing authority and cancellation; NOT RUN. SQL segment reserve/complete and qualified auto-perspective admission remain open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 replacement segment reserve/complete publication (2026-10-11)

Authored01517 dedicated segment document scope/reserve/complete using current01516 adopted result, exact available segments JSON, result/root equality, bounded body/hash/size, deterministic versioned UUID owner and params artifact. Reuses established backend_documents fencing, ready replay integrity and tenant reserved/storage accounting lifecycle. Added Go repository/PublishSegments: full source/result re-lock and exact plan recompilation on each reserve/complete, bounded version/key/media validation, immutable publisher and exact returned ref. Versioned seed now uses fixed UUID/SHA string components for identical Go/SQL calculation. Invalid owner/size/key/missing-store unit tests authored NOT RUN;01517 NOT APPLIED. Positive accounting/recovery and deployment remain unverified; qualified auto-perspective publication/admission still open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 replacement segment ready and physical byte authority (2026-10-11)

Authored01518 scheduler-only ready metadata reader reusing current adopted detector segment scope; exact immutable backend_document/artifact identity, hash/size/bucket/version/key/expiry and SHARE locks. Refactored repository locked proof helper. Added caller-owned LockReadySegments, observation wrapper and VerifyReadySegments with bounded version-specific object read outside transaction, explicit stream cleanup/error handling, exact canonical-byte comparison and post-IO full current-source/locator recheck. Missing authority/cancellation unit tests authored NOT RUN;01518 NOT APPLIED. Final qualified auto-perspective publisher must repeat source/result/ready locks; admission still open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 qualified automatic replacement draft preparation (2026-10-11)

Added LockAutomaticPerspectiveDraft and CompileLockedAutomaticPerspectiveReplacement: caller tx current ready segment locator equality, complete detector/mapped source proof restoration, live located qualification lock, existing full replacement draft/profile/resource/line compiler. No manual admission gate relaxed, no draft save or queue authority claimed. Fixed zero-image-binding control-only RebindMapped for auto perspective to preserve frozen source/controls while accepting distinct valid fresh line ref; unrelated/manual/stale refs rejected. Missing authority/cancellation unit tests authored NOT RUN. Auto-specific SQL draft, Params/transport publication/admission, recovery and downstream lifecycle remain open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 automatic perspective SQL identity boundary (2026-10-11)

Authored01519 scheduler-only lock-only auto replacement identity gate. Requires01518 ready segment authority, deterministic replacement namespace/run/task/workflow/Params IDs, exact tenant/project/review/decision/original identity, actor/session/snapshot/base/source, distinct fresh lines and exact frozen user controls. Checks typed identity keys, marker content/hash and identity SHA. Go LockAutomaticPerspectiveDraft now invokes it after live qualification/full compiler and current segment/source locks. No draft/task/queue mutation; this gate does not attest quality alone.01519 NOT APPLIED; existing authored cancellation/dependency tests NOT RUN. Auto draft persistence, Params/transport publication/admission and lifecycle remain open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 automatic perspective draft persistence (2026-10-11)

Authored01520 dedicated auto draft persistence adapted from complete manual draft identity/resource/input-digest/qualification gate, with additional01519 adopted segment authority, auto null corners/segment ref and exact LinesJCS, five-root LineSegments typed closure and immutable auto segment configuration. Existing conflicting run refused; exact draft replay accepted. Added SaveAutomaticPerspectiveDraft recompiling current full physical/ready/qualification authority and saving in same bounded scheduler transaction. Missing source/cancellation unit cases authored NOT RUN;01520 NOT APPLIED. Recovery reader, Params and transport publication, stage/admission and lifecycle remain open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 automatic perspective exact draft recovery (2026-10-11)

Authored01521 scheduler-only exact preliminary draft reader: repeats current adopted segment identity and live qualification, exact saved config/hash/actor/snapshot/workflow/policy and SHARE locks. Added LoadAutomaticPerspectiveDraft recompiling full current physical/qualified expected draft in same bounded transaction and comparing exact saved bytes; absent/stale/conflicting state returns expired, never saves or repairs. Missing source/cancellation unit tests authored NOT RUN;01521 NOT APPLIED. Params and transport publication, stage/admission, post-seal recovery and lifecycle remain open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 automatic perspective Params publication (2026-10-11)

Authored01522 dedicated Params scope/reserve/complete requiring exact01521 saved draft and current adopted segments/qualification, explicit bucket equality, deterministic Params ID, established document fencing/replay/quota accounting. Added Go auto Params repository and PublishAutomaticPerspectiveParams loading exact saved draft, recompiling under current physical/ready/live qualification locks on each reserve/complete, bounded immutable publisher and exact key/version/ref checks. Missing draft/object-store unit tests authored NOT RUN;01522 NOT APPLIED. Stage, transport publication/registration, final graph admission/recovery and lifecycle remain open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 automatic perspective task stage (2026-10-11)

Authored01523 dedicated automatic blocked shell stage using01522 current saved draft/segments/qualification gate; exact typed LineSegments inventory added, ordinal/sorted UUID/input digest/profile validation and existing live output quota/immutable retry retained. Added AutomaticStageDescriptor pure compiler rebuilding full plan and typed inputs from current qualification and owned canonical draft, and StageAutomaticPerspectiveTask repeating physical/ready/live qualification locks before SQL. Invalid draft/cancellation unit cases authored NOT RUN;01523 NOT APPLIED. No queue created; transport publication/registration, graph seal/admission and recovery remain open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 automatic perspective transport publication (2026-10-11)

Authored01524 dedicated chunk/manifest scope/reserve/complete adapted from established exact staged descriptor protocol, current auto stage/source/qualification/segment proofs and explicit bucket binding. Added Go repository/documents and PublishAutomaticPerspectiveTransport: blocked stage before IO, current draft recompilation on every mutation, bounded immutable chunk/manifest builder, original task inventory/profile compilation and key/version checks. Missing authority/publisher unit tests authored NOT RUN;01524 NOT APPLIED. Input registration/final graph seal/admission/recovery and deployment qualification remain open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 automatic perspective transport recovery and registration (2026-10-11)

Authored01525 existing blocked transport reader and01526 exact input registration adapted from dedicated manual protocol, with auto scope/stage gates and additional LineSegments direct root. Added shared optical transport hash reconstruction helper preserving original four-root negative/manual behavior and auto five-root wrapper. Go RegisterAutomaticPerspectiveInputs checks current physical/segment/qualification authority, exact descriptor, chunk/manifest hashes and registers/seals typed input closure in same bounded transaction. Missing authority/cancellation unit tests authored NOT RUN; migrations NOT APPLIED. Final graph seal/admission/recovery, scheduler wave and lifecycle remain open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 automatic perspective graph seal/admission (2026-10-11)

Authored01527 dedicated automatic graph seal adapted from manual final admission: exact existing blocked graph/registered transport, current auto source/segments/qualification gates, enabled registry/output policies and preserved private V2 closure/quota/activation/events/outbox chain. Atomic shared replacement queued receipt and post-seal membership check. Go AdmitAutomaticPerspectiveGraph registers exact closure then repeats full current authority in seal transaction; ambiguous commit is not promoted to success. Missing authority/cancellation tests authored NOT RUN;01527 NOT APPLIED. Durable recovery reader/coordinator, scheduler wave and downstream lifecycle remain open; no module completion claim. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 automatic perspective durable queued receipt recovery (2026-10-11)

Authored01528 dedicated automatic historical acknowledgement adapted from complete existing manual receipt reader plus current adopted segment identity. Added mode-neutral receipt parser internals preserving manual four-root behavior and auto five-root transport checks; auto wire type shares existing historical acknowledgement schema. Go ReadAutomaticPerspectiveReceipt recompiles under current physical/ready/live qualification locks, reads exact saved admission and validates scope/identity/descriptor/qualification/transport. No mutation, retry admission or execution/Apply success claimed. Missing authority/cancellation/owned-draft tests authored NOT RUN;01528 NOT APPLIED. Recovery coordinator/wave orchestration and downstream lifecycle remain open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 automatic perspective publication coordinator (2026-10-11)

Authored01529 explicit absent/draft/admitted probe repeating current adopted segment identity and exact draft or historical receipt gates. Added ExecuteAutomaticPerspectiveReplacement composing probe/save/Params/transport/admit/receipt; admitted skips mutations, each phase repeats existing current authority, ambiguous response triggers only read-only durable acknowledgement and preserves original failure if unconfirmed. Recovered admission excluded from new publication capacity. Authored coordinator tests for absent/draft/admitted sequencing and ambiguous commit acknowledgement/no resend; NOT RUN.01529 NOT APPLIED. Scheduler wave orchestration/downstream lifecycle and positive deployment remain open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 automatic perspective wave orchestration (2026-10-11)

Added exact project qualification preparation from privately owned mapped step/fresh segments, automatic geometry/resource sizing and current qualification object loader. Detection wave now optionally uses configured PerspectiveProjects after durable detector adoption: explicit unsupported observation, segment publish/versioned byte verification, qualification/resource calculation and complete auto publication coordinator. New detector and perspective admissions share pass budget; recovered receipts consume none. Wrapper clears Deferred only for confirmed perspective receipts, never detector admissions. Bootstrap passes configured project services. Authored cancellation and receipt-vs-deferred tests NOT RUN. Production startup/SQL qualification, positive lifecycle and downstream preview/review/adoption/Apply remain open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 automatic startup and downstream execution plan routing (2026-10-11)

Added explicit scheduler installation inventory for dedicated detector/segment/auto publication functions through01529 with photo_scheduler role, SECURITY DEFINER/photo_fn_owner ownership and EXECUTE checks; preparation bootstrap invokes it. Startup also requires auto receipt routing in existing execution-plan resolver. Source inspection found resolver01483 hardwired to manual receipt; authored01530 auto-marker branch resolves segment bucket and invokes dedicated auto receipt while preserving ordinary/manual behavior. Existing source/adoption/preview helpers consume this resolver. Updated operator contract and authored nil DB/cancellation tests NOT RUN;01530 NOT APPLIED. Positive lifecycle and downstream review/Apply remain unverified/open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged; product percentage unmeasured.


## V29 replacement preview/quality source identity (2026-10-11)

Source inspection confirmed four plan/intent functions rejected replacement producers by reconstructing ordinary optical.perspective TaskID. Added exported structural ValidatePerspectivePreviewSource wrapper around existing complete ordinary/replacement validator; both preview/quality compilers and intent gates now reuse it, preserving current SQL/object authority requirements. Authored shared valid replacement bundle coverage through both compilers/intents and tenant/project/review/decision/original-run/original-task/missing-identity/ordinary-producer substitutions. Tests NOT RUN. No builds/validators/migrations executed. Downstream positive review/Apply lifecycle remains open; registry74/16/1 unchanged, product percentage unmeasured.


## V29 direct quality task source validation (2026-10-11)

PreparePerspectiveQualityTask previously checked derived quality IDs/profile/Params but not full source identity when called directly. Added complete ordinary/replacement source validator before task compilation. Authored direct-call rejection cases for replacement scope substitution plus ordinary workflow, base version/hash, principal, attempt and fence, retaining positive ordinary case. Tests NOT RUN; no builds/validators/migrations executed. SQL01484/01485 source inspection shows replacement source and adopted-binding resolution route through execution-plan authority; this is source evidence only, not lifecycle completion. Registry74/16/1 unchanged; product percentage unmeasured.


## V29 replacement worker/adoption source authority (2026-10-11)

Found execution blocker in01029/01030/01032: session read only legacy v29_perspective config and source authority accepted only original snapshot. Authored forward-only01531 private session resolver and execution source gate: ordinary scope unchanged; replacement requires current execution-plan receipt, exact actor/session/project/task/snapshot/base and current mapped replacement source. Tenant context restored; foreign active tenant rejected. Baseline-guarded definitions route leased source, worker qualification session and completed-task adoption through these helpers while preserving lease/closure/qualification/result/line checks. Private helper ownership/ACL and installation routing integration tests authored NOT RUN.01531 NOT APPLIED; positive worker/replay/review/Apply lifecycle still open. No builds/tests/validators/migrations executed; registry74/16/1 unchanged, whole-product percent unmeasured.


## V29 replacement execution installation startup gate (2026-10-11)

Added shared execution installation check to manual project deployment and automatic replacement startup. Requires01531 private helpers with exact owner/security mode, no PUBLIC/service-role EXECUTE, and all three worker/source/qualification/adoption definitions routed through session/source helpers. Prevents admission-enabled startup against pre01531 execution authority. Missing DB/cancellation tests and positive migrated scheduler-role SQL installation test authored NOT RUN. Worker source closure inspection confirms pixels/alpha/dense-warp arrays and lines checked against exact declared task inputs and SHA; no new bypass introduced. No tests/builds/validators/migrations executed. Positive replay/review/Apply remains open; registry74/16/1 unchanged, percentage unmeasured.


## V29 client replacement result reader (2026-10-11)

Added readPerspectiveReplacementResult with independently supplied current mapped base, owned scope, deterministic replacement namespace/run/task and ordinary workflow identity, live workflow run/scope/status, cancellation-after-IO and exact existing proposal validation. Does not derive ordinary perspective producer or reuse original intent base. Authored fixed UUIDv5 ABI/pending/scope substitution/mapped-base ownership/available/terminal/cancellation tests NOT RUN. This is a client primitive, not yet replacement discovery or mounted review screen; those remain open. No tests/builds/validators/migrations executed; registry74/16/1 unchanged, percentage unmeasured.


## V29 replacement review discovery backend (2026-10-11)

Authored01532 API-only read_v29_perspective_replacement_review_scope: current editor/invalidation authority, exact original project operation, deterministic replacement IDs, negative replacement membership, current execution-plan receipt/identity and mapped source. Returns only current review identity/base, no locators. Added bounded tenant API service with strict closed decoder and independent deterministic identity validation, and authenticated no-store GET projects/{id}/reviews/{review}/perspective-replacements/{original}?decision_hash=...; duplicate hash/invalid UUID/digest rejected. Decoder/substitution/unknown-field/missing dependency/cancellation tests authored NOT RUN.01532 NOT APPLIED. Client scope parser/API/component and mounted discovery integration remain open. No builds/tests/validators/migrations executed; registry counts74/16/1 still historical catalogue classification, new endpoint not yet catalogued. Product percentage unmeasured.


## V29 mounted replacement review client (2026-10-11)

Added strict closed discovery response decoder mapping wire fields into owned deterministic scope, OwnerAPI.perspectiveReplacementScope with UUID/hash validation and cancellation, and mounted /perspective-replacements/[original] route. Screen discovers current mapped base, observes workflow, renders existing preview and quality/explicit Apply components, displays saved accepted version, supports refresh and remounts on client/principal context change. Parser ownership/substitution and DOM discovery/client replacement/abort tests authored NOT RUN. Screen included as supplemental inventory; navigation from original negative review and full runtime lifecycle remain open. No tests/builds/validators/migrations executed; historical catalogue counts not promoted, product percentage unmeasured.


## V29 original negative review replacement navigation (2026-10-11)

Authored01533 API-only paginated immutable ledger enumeration for exact decided rejected negative review/hash and live project/editor authority. Sorted UUID cursor,100 items +lookahead, includes null current scope for unavailable admission; no false completion. Added bounded tenant service/closed page decoder and no-store authenticated GET reviews/{review}/perspective-replacements?decision_hash=&after=. Added strict client page parser/OwnerAPI and mounted PerspectiveReplacementLinks in original negative review base-result section, linking current admitted scope to mounted replacement-review route. Refresh/page changes abort old reads; client/context remount discards stale state. Go/TS page ordering/cursor/duplicate/foreign scope tests authored NOT RUN.01533 NOT APPLIED. Positive cross-tenant SQL/list/admission/replay/review/Apply and DOM navigation coverage remain open; endpoint catalogue/schema updates pending. Historical registry counts not promoted; product percentage unmeasured.


## V29 replacement API machine contracts and navigation coverage (2026-10-11)

Added strict Scope/Item/Page JSON Schemas, two OpenAPI GET operations with required decision_hash/optional list cursor/no-store semantics, endpoints and function-index entries; authored reusable spec_v29_replacement source extension and build-spec hooks without running generators. Current entrypoint ledger now93:76 requires_semantic_review,16 partial_runtime_path,1 missing_runtime_path;92/93=98.92% code presence only, whole-product completion null. Added DOM scoped-link/unavailable/pagination/refresh/client-change-abort cases and migrated SQL API-only ACL/absent-foreign authority tests, NOT RUN.01532/01533 NOT APPLIED. Positive lifecycle, full HTTP coverage and current-scope SQL pagination fixture remain open. No tests/builds/validators/migrations executed.


## V29 panorama/focus replacement draft compiler (2026-10-11)

Added owned MultiReplacementDraft compilation/restoration from PreparedMappedReplacementStep. Repeats current mapping/private physical source resolution and Multi qualification/compiler, preserves exact frame order/user controls, deterministic negative replacement run/workflow/task/Params IDs, marker/identity hashes, complete typed input digest and fresh profile/resources. Restore compares full canonical packet with fresh compilation; ordinary MultiDraft admission is not used. Authored positive private serialization tests for both operations with real existing qualified fixtures, deterministic replay/byte ownership/source order and identity/marker/source substitutions, plus missing physical authority/cancellation tests NOT RUN. SQL persistence/Params/transport/admission/wave/review/Apply integration for multi replacements remains open. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi replacement locked draft and descriptor (2026-10-11)

Added LockMultiReplacementDraft retaining complete mapped-wave source/parent/unchanged physical proofs, exact original consumer snapshot and current located Multi policy/report/calibration in caller transaction, then fresh exact restoration under locks. Added pure MultiReplacementDraft.Descriptor rebuilding qualified plan/profile/resources, deterministic identity packet and typed inventory/input digest with replacement task ID; ordinary MultiDraft remains rejecting that ID. Authored qualified descriptor positive cases for both operations, ordinary-path exclusion, digest/quota/task/source mutations and missing tx/cancellation tests NOT RUN. No persistence/reservation/admission authority introduced. Dedicated multi replacement SQL persistence/publication and scheduler/review lifecycle remain open. No tests/builds/validators/migrations executed; registry93=76/16/1 unchanged, completion percentage unmeasured.


## V29 multi replacement SQL identity prerequisite (2026-10-11)

Authored01534 lock_v29_multi_replacement_identity by retaining dedicated replacement scope/frozen original/snapshot/deterministic UUID/marker/identity checks and adding strict multi draft/Params operation/mode/frame order/source SHA/type/original snapshot membership/current qualification and exact conservative resource mirror. Lock-only gate does not persist, reserve, stage or admit; physical source authority and typed digest still come from full Go mapped/qualified recompilation. LockMultiReplacementDraft invokes gate only after retaining full physical and located qualification locks and fresh restoration. Scheduler-only owner/SECURITY DEFINER/denied-role/absent-scope SQL integration tests authored NOT RUN.01534 NOT APPLIED. Dedicated persistence and input-inventory SQL verification remain open before publication; no admission completion claim. No tests/builds/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi replacement SQL typed inventory (2026-10-11)

Authored01535 private pure SQL typed inventory mirror and scheduler-only scoped inventory lock gate. Reconstructs Params/Calibration/Report/unique WorkingImage and pixels/optional alpha/every inverse_field NpyArray references, exact closed lowercase UUID/SHA refs, bounded transform chains and inventory, conflict rejection, deduplication and UUID-sorted canonical input digest. Go LockMultiReplacementDraft now invokes scoped identity+inventory gate after full mapped physical/qualification locks and fresh restoration. Authored Go/SQL parity for both operations with repeated roots/physical refs and digest/task/root collision/missing source/pixels-alpha alias mutations; extended scheduler-only/absent-scope gate coverage. Tests NOT RUN;01535 NOT APPLIED. Persistence/recovery/Params/transport/stage/admission/wave/review/Apply remain open. No tests/builds/validators/migrations executed; registry93=76/16/1 unchanged, whole-product percentage unmeasured.


## V29 multi replacement preliminary persistence and recovery (2026-10-11)

Authored01536 scheduler-only save/read dedicated multi replacement draft functions. Both repeat scoped identity/resource/typed inventory checks; save inserts preliminary immutable run/workflow only, rejects mismatched replay, locks exact workflow and preserves fail_fast policy. Read is non-mutating, requires exact expected canonical draft/config hash/actor/snapshot/workflow/policy and unsealed noncancelled draft state. Added Go SaveMultiReplacementDraft/LoadMultiReplacementDraft sharing bounded transaction orchestration: preflight fresh mapped/qualified restore, complete physical and live qualification locks, scoped SQL gate, exact saved-byte comparison and ambiguous commit propagation. Authored missing authority/cancellation unit cases and extended SQL ACL/absent-scope tests to save/read. Tests NOT RUN;01536 NOT APPLIED. Positive persistence/recovery fixtures and Params/transport/stage/admission/wave/review/Apply remain open. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi replacement Params publication (2026-10-11)

Authored01537 dedicated private Params document scope and scheduler-only reserve/complete functions adapted from established immutable replacement protocol. Requires exact saved01536 draft, whole-wave identity/inventory/resource checks and current multi qualification object locators; explicit bucket proof binds reservation/completion. Preserves deterministic Params ID, fenced pending recovery, immutable ready replay, versioned JSON artifact and reserved/storage accounting. Added Go PublishMultiReplacementParams and repository: fresh complete mapped/qualified recompilation, preliminary save before IO, full physical/qualification locks on every reserve/complete, exact size/SHA/owner/ref/key/version checks. Authored incomplete-authority/no-SQL/no-IO/cancellation unit cases and ownership/ACL SQL tests NOT RUN;01537 NOT APPLIED. Positive quota/fencing/ambiguous PUT recovery fixtures, blocked task stage/transport/registration/admission/wave/review/Apply remain open. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, product percentage unmeasured.


## V29 multi replacement blocked task stage (2026-10-11)

Authored01538 dedicated scheduler-only multi replacement stage adapted from established immutable blocked shell protocol. Repeats01537 exact saved draft/current source/qualification scope, checks operation-derived node key and exact descriptor/profile/Params, reconstructs all frame WorkingImage and pixels/alpha/inverse-field typed references, deduplicates/sorts and verifies digest and every ready versioned artifact under locks. Creates one blocked CPU task and exact live output quota reservation, immutable stage replay refuses lost/expired reservation and conflicting state; never queues task. Added Go StageMultiReplacementTask with existing qualified owned Descriptor, current complete physical/qualification locks and bounded transaction. Authored pre-transaction authority/cancellation and SQL privilege/invalid-scope tests NOT RUN;01538 NOT APPLIED. Positive quota/staging replay fixtures and transport publication/registration/final admission/wave/review/Apply remain open. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi replacement chunk/manifest publication (2026-10-11)

Authored01539 dedicated private multi transport scope and scheduler-only reserve/complete protocol. Repeats01538 current exact stage/source/qualification authority, matches chunk payload to descriptor slice, manifest to complete ready versioned chunks and canonical entries hash, binds bucket proof and preserves fenced document recovery and storage quota accounting. Supports0..7 chunks for4096-entry maximum, rejects alternate purpose spellings. Added Go PublishMultiReplacementTaskTransport using qualified ordered multi originals/typed closure compiler, blocked stage before IO and full current physical/qualification locks at each publication phase. Authored incomplete authority/no-SQL/no-IO/cancellation, purpose-boundary and ownership/ACL tests NOT RUN;01539 NOT APPLIED. Positive quota/fence/ambiguous PUT fixtures, transport recovery reader, input registration/final admission/wave/review/Apply remain open. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi replacement published transport recovery (2026-10-11)

Authored01540 scheduler-only existing multi transport reader requiring exact descriptor, existing blocked task/live quota before replay validation, reconstructing every512-entry chunk and manifest, unique document refs and all ready versioned artifacts under locks. Added Go ReadPublishedMultiReplacementTransport with full current physical/qualification locks and exact qualified descriptor reconstruction. Extended private byte/hash restorer with explicit chunk bound; existing negative/manual/auto callers remain single-chunk, multi permits8 and validates each chunk hash, manifest hash, root count and all transport/input collisions. Authored513-entry positive recovery, single-chunk exclusion, reordered/duplicate/corrupt chunk and input alias substitutions, dependency/cancellation and SQL privilege tests NOT RUN;01540 NOT APPLIED. Input registration/final admission/wave/review/Apply and positive deployed recovery remain open. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi replacement input registration (2026-10-11)

Authored01541 scheduler-only dedicated registration adapted from exact existing replacement protocol. Repeats01540 published transport/current descriptor/source/qualification scope; checks exact canonical manifest and ready artifact, reconstructs sorted unique Params/frame-source/Report/Calibration wire roots, registers full entries in512-entry batches then seals task inputs through existing private core. Replay requires exact ordinal typed inventory including all chunks/manifest and matching task direct roots/profile; partial/conflicting state refused, no repair or queue admission. Added Go RegisterMultiReplacementInputs reconstructing current qualified descriptor and transport bytes and re-locking complete physical/qualification authority in bounded registration transaction. Authored incomplete-authority/cancellation and SQL owner/ACL tests NOT RUN;01541 NOT APPLIED. Positive deployed registration/replay, final graph admission/receipt/coordinator/wave/worker/review/Apply remain open. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi replacement atomic graph admission (2026-10-11)

Authored01542 scheduler-only dedicated multi graph seal retaining established registry/closure/quota/activation/event/outbox chain. Requires exact draft/descriptor, one task/no edges, ready input closure and exact dedicated registration replay, enabled panorama/focus operation and backend, then validates negative replacement membership after seal and writes versioned identity/qualification/transport queued receipt atomically. Go AdmitMultiReplacementGraph registers existing transport then recompiles and re-locks full current physical/intent/mapping/snapshot/qualification authority before final transaction; ambiguous commit propagates. Authored missing-authority/cancellation and SQL ownership/ACL tests NOT RUN;01542 NOT APPLIED. Receipt recovery/coordinator/wave/installation and multi worker/review/Apply remain open; registry enablement intentionally still requires qualification. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi replacement queued receipt recovery (2026-10-11)

Authored01543 scheduler-only historical multi admission receipt reader adapted from established perspective receipt protocol. Requires exact saved expected draft, deterministic per-node IDs/marker/original intent, actor/session/snapshot/live multi qualification, exact descriptor/profile/registered entry and chunk rows, closure metadata and all ready versioned input artifacts. Added owned MultiReplacementReceipt and Go current-proof read with bounded transaction, complete physical/qualification locks, closed canonical receipt decoder, exact identity/pin/descriptor hash and all-chunk/manifest restoration. Receipt is admission evidence only, never task completion or standalone execution authority. Authored missing-authority/cancellation/invalid receipt and SQL owner/ACL tests NOT RUN;01543 NOT APPLIED. Positive receipt recovery fixtures, coordinator/wave/installation, worker/adoption/review/Apply remain open. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi replacement publication coordinator (2026-10-11)

Authored01544 scheduler-only read-only multi publication probe with deterministic scope/original intent/snapshot/run/config/workflow/policy checks and explicit absent/draft/admitted distinction; conflicting state returns no permission to resume. Added ExecuteMultiReplacement: clones supplied wave slices, freshly compiles exactly one original consumer draft with current multi qualification, probes under full physical/qualification locks, publishes Params/transport/admission only for successful preliminary-state probe and independently reads queued receipt. Admitted recovery skips mutation and marks RecoveredExisting; no error or ambiguous commit is treated as absence. Authored phase order, failures at every stage, no admitted republish, cancellation during/after phases, missing dependencies and SQL owner/ACL tests NOT RUN;01544 NOT APPLIED. Scheduler wave/installation and multi worker/adoption/review/Apply integration remain open. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi replacement operation wave and shared dispatcher (2026-10-11)

Added MultiService/MultiProjects bounded replacement wave: reconciles settlement, reloads exact immutable plan/current physical sources/adopted parents, selects configured operation, derives ordered mapped frames, enforces deployment max pixels/resources, loads current multi qualification and original snapshot before first object write, then invokes complete coordinator retaining confirmed receipts/pending/unconfirmed nodes. Added MultiReplacementDispatcher wrapping existing negative/perspective base, optional panorama/focus services and shared new-admission budget; recovered receipts consume none, unsupported deferred inventory preserved, failure preserves original deferred plus confirmed evidence. Added MultiReceipts to common pass and authored invalid-scope/dependency/cancellation service tests NOT RUN. Production bootstrap/installation and dispatcher positive budget/failure fixtures remain open, as do worker/adoption/review/Apply. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi dispatcher budget and failure coverage (2026-10-11)

Authored dedicated dispatcher tests covering base publication budget carryover, recovered panorama receipt preserving focus budget, fresh panorama exhausting capacity, unsupported deferred retention, partial confirmed receipts plus unconfirmed/pending evidence on ambiguous failure, no subsequent operation after failure, settlement gate and cancellation. Tests NOT RUN. Source inspection located production composition in workflow/negative_replacement_bootstrap.go; bootstrap wiring remains deliberately pending multi worker/adoption execution authority routing, rather than claiming publication alone constitutes a runnable lifecycle. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, whole-product percent unmeasured.


## V29 multi execution plan authority prerequisite (2026-10-11)

Source inspection found multi qualification session still reads legacy config v29_multi and adopted binding reads v29_multi_plan plus original snapshot source checks. Authored01545 private v29_multi_execution_plan resolver preserving ordinary plan path without mixed replacement markers and resolving replacement only through exact tenant/run/project/task/workflow/Params/snapshot/actor identity, independently recomputed identity SHA/typed inventory and current dedicated01543 receipt. Private SECURITY DEFINER owner function grants no service-role EXECUTE; it supplies no standalone physical mapping/lease/adoption authority. Authored ownership/ACL and absent/foreign tenant scope tests NOT RUN;01545 NOT APPLIED. Worker/qualification/adoption callers and mapped source routing still require integration before production bootstrap wiring. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi execution session routing (2026-10-11)

Authored01546 private SECURITY DEFINER multi execution session resolver preserving legacy ordinary session and requiring current01545 execution plan plus exact persisted config for replacement session; foreign active tenant refused and previous tenant context restored. Baseline-guarded function-definition patch routes worker source-ready, worker qualification evidence and adoption evidence through resolver, retaining all existing lease/source/closure/qualification/result checks. Authored private owner/ACL, absent/foreign scope and installed caller-routing SQL tests NOT RUN;01546 NOT APPLIED. Existing source scope still uses original snapshot binding and must gain dedicated mapped-source authority; no claim of runnable replacement worker/adoption yet. Production bootstrap and review/Apply remain open. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi per-frame mapped execution source (2026-10-11)

Authored01547 private multi replacement source resolver: current execution plan/receipt identity, exact supplied frame membership, immutable original Params SHA from frozen intent and persisted original plan, original frame by asset version, snapshot asset identity, current ready declared WorkingImage and exact affected edge keyed by original source ID/SHA. Unchanged frame must remain exact original frame and pass existing unchanged reader; changed frame must resolve selected negative base or matching producer's adopted replacement membership. Added private tenant-restoring execution source wrapper preserving ordinary snapshot gate and baseline-guarded worker source/adoption routing; existing lease/qualification/closure/result checks retained. Authored private resolver ownership/ACL and absent/foreign scope tests NOT RUN;01547 NOT APPLIED. Positive mixed-frame/producer-substitution/worker/adoption fixtures, downstream adopted binding/preview/quality/Apply and startup integration remain open. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 adopted multi and preview execution routing (2026-10-11)

Authored01548 private replacement-frame adopted authority requiring exact current execution plan, replacement marker, frame ordinal/source bytes, current mapped source locator/size, accepted original snapshot version and verified original asset canvas source SHA. Baseline-guarded adopted-originals patch retains ordinary snapshot source branch and existing full canvas/pixels/alpha/dense-field typed artifact checks while allowing dedicated replacement frame gate; adopted binding now reads current execution plan and operation-appropriate admission receipt, preview source resolves same plan. Authored private owner/ACL and absent/foreign scope SQL tests NOT RUN;01548 NOT APPLIED. Positive mixed-frame/adopted preview fixtures, client/server ordinary-ID constraints, quality/Apply and startup integration remain open. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 Go adopted multi replacement identity (2026-10-11)

Authored01549 API/scheduler current-editor identity reader gated by exact multi execution plan/receipt. Added comparable runtime-only MultiReplacementBindingIdentity to adopted binding, strict closed bounded canonical decoder and deterministic namespace/run/task validator. SQL adopted API and scheduler preview readers load identity in the same transaction; preview independently enforces tenant/project. Ordinary task-ID validation remains unchanged when identity is absent. Authored both-operation positive binding and every tenant/project/review/decision/original-run/original-task substitution, missing identity and malformed decoder tests NOT RUN;01549 NOT APPLIED. Identity wire schema/client discovery and broader preview/quality/Apply ordinary-ID assumptions still require integration; startup remains open. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi replacement original inventory routing (2026-10-11)

Source inspection found original-object and complete inventory SQL still read ordinary v29_multi_plan; authored baseline-guarded01550 routing both through current admitted execution plan, retaining all role/hash/version/ordinal checks. Found Go inventory closed wire intentionally omits runtime replacement identity, causing comparison failure against current binding; decoder now compares unchanged wire with identity-stripped expected binding, then retains independently resolved private identity and performs full binding/inventory validation. Unknown injected identity remains rejected by canonical closed-wire comparison. Authored both-operation positive inventory ownership/private identity, substituted expected review and injected-wire identity tests NOT RUN;01550 NOT APPLIED. Remaining quality/Apply/client/startup and positive runtime lifecycle remain open. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi downstream scope guards and identity coverage (2026-10-11)

Source inspection found multi preview/quality identities reuse complete adopted binding validation and witness inventory already serializes unchanged wire with private identity retained by updated decoder; no extra ordinary TaskID bypass needed there. Added explicit replacement tenant/project equality at adopted root load before object IO and Apply evidence boundary. Authored both-operation positive preview/quality deterministic identity, distinct preview/quality runs, fence change and foreign tenant/project/missing/substituted replacement identity tests NOT RUN. Full content/witness/Apply positive replacement fixtures, client discovery and production startup still open. No builds/tests/validators/migrations executed; registry93=76/16/1 unchanged, percentage unmeasured.


## V29 multi replacement production scheduler composition (2026-10-11)

Added digest-pinned bounded panorama/focus deployment loading using the same configuration names and qualification parser as the API. Scheduler now wraps negative/perspective/detection waves with the multi dispatcher and shared admission budget; absent services remain nil interfaces, and multi configuration without negative deployment is refused. Startup checks23 publication/private functions for exact owner, SECURITY DEFINER versus pure inventory SECURITY INVOKER, PUBLIC/private service ACLs, and11 worker/adoption/binding/preview/original routing requirements before building services. Registry enablement and qualification are not bypassed. Authored dependency/cancellation/orphan-config/digest/size/malformed-operation tests NOT RUN. Positive installation SQL fixtures and full content/witness/Apply/client/parent lifecycle remain open. No tests/builds/validators/migrations executed; whole-product percentage unmeasured.


## V29 accepted multi parent authority (2026-10-11)

Authored forward-only01551 private accepted panorama/focus parent gate and baseline-guarded adopted-parent routing. Requires current quality review, exact explicit immutable Apply decision/result/root/report/target, accepted deterministic MultiAppliedVersion/reference parent, complete ordered accepted project source lineage, and retained root/image links from atomic Apply. Existing source bytes, arrays, attempt/fence/adoption and replacement membership checks remain separate. Startup now requires private helper and parent reader routing. Authored owner/private ACL, absent-authority/unsupported-operation and installed routing SQL tests NOT RUN;01551 NOT APPLIED. Positive accepted/rejected/stale-quality/mixed-frame lifecycle fixtures and client integration remain open. No tests/builds/validators/migrations executed; completion percentage unmeasured.


## V29 multi replacement client result authority (2026-10-11)

Added closed multi replacement scope decoder and owned result reader for panorama/focus. Validates invalidation namespace/run/task/workflow identity before network IO, freezes current mapped intent, checks live workflow identity/state, then reuses strict multi proposal validation with exact replacement producer and ordered sources. Existing ordinary producer-ID derivation remains unchanged; shared namespaced UUID implementation exported. Authored both-operation available/pending/failure/identity substitution/foreign workflow/cancellation/closed packet/ownership tests NOT RUN. Server discovery, mounted review screen/navigation and full review/Apply lifecycle remain open. No tests/builds/validators/migrations executed; whole-product percentage unmeasured.


## V29 multi replacement server discovery (2026-10-11)

Authored API-only forward-only01552 current multi replacement review scope reader. Derives exact invalidation namespace and original operation, requires replacement membership/current execution plan and exact immutable identity, resolves every mapped frame before exposing closed operation/intent packet. Added tenant Editor-scoped bounded service with strict canonical identity and full MultiIntent parser, authenticated GET project/review/multi-replacements/original route with exact decision hash/operation validation and no-store. Authored both-operation positive decoder and substituted identity/intent/operation/unknown field plus dependency/cancellation tests NOT RUN;01552 NOT APPLIED. HTTP/SQL positive fixtures, machine contracts/catalogue, OwnerAPI, mounted screen and navigation remain open. No tests/builds/validators/migrations executed; overall percentage unmeasured.


## V29 multi replacement mounted review (2026-10-11)

Added OwnerAPI multiReplacementScope with exact operation/UUID/hash validation, encoded authenticated GET and closed response decoder. Added mounted multi-replacements route and scoped review component: current discovery then live result observation, pending/unavailable/refresh states, existing preview and independent quality with explicit Apply, durable Apply recovery and accepted version display. Replacement review supplies null collection selection; it does not choose a collection target implicitly. API/identity/operation changes remount and abort stale reads. Authored DOM discovery-order and client-change abort tests NOT RUN. Original-review paginated navigation, machine contracts/catalogue, HTTP/SQL positive fixtures and full lifecycle remain open. No tests/builds/validators/migrations executed; whole-product percentage unmeasured.


## V29 original-review multi replacement navigation (2026-10-11)

Authored01553 API-only bounded immutable-invalidation list with100-item UUID pagination, exact rejected negative review decision and per-item current mapped multi scope. Added tenant service/strict closed ordered page parser, authenticated no-store list endpoint, OwnerAPI/client parser and mounted original-review links for both operations. Unavailable nodes remain visible without an open action; explicit operation travels to replacement review. Go/TS tests authored for both-operation unavailable items, ordering/duplicates, operation/decision substitution, unknown fields, ownership and cursor boundaries NOT RUN;01553 NOT APPLIED. Machine contracts/catalogue, positive HTTP/SQL/mounted navigation fixtures and complete lifecycle remain open. No tests/builds/validators/migrations executed; entrypoint count awaits catalogue reconciliation, overall percent unmeasured.


## V29 multi replacement machine contracts reconciliation (2026-10-11)

Added closed MultiReplacementReviewScope/Item/Page schemas, operation-conditioned mapped panorama/focus intent, both authenticated GET OpenAPI operations with decision_hash, operation and pagination query parameters, endpoint/function indexes and reusable spec_v29_replacement source extension. Updated registry to95 entries:78 requires_semantic_review,16 partial_runtime_path,1 missing_runtime_path;94/95=98.95% source presence only. Whole-product completion remains null. No generators/validators/tests/builds/migrations executed. Positive HTTP/SQL lifecycle and mounted navigation coverage remain open.


## V29 multi navigation and discovery isolation coverage (2026-10-11)

Authored mounted list DOM coverage for available/unavailable navigation, explicit panorama/focus operation forwarding, pagination/reset refresh and old-client abort/late response rejection. Authored isolated migrated SQL API-only ownership/SECURITY DEFINER/PUBLIC denial and all service-role ACL matrix, absent/foreign tenant scope for both operations and unsupported operation refusal. Tightened server page decoder to validate nonzero tenant/review and canonical decision hash even for empty pages; regression tests authored. Tests NOT RUN; migrations NOT APPLIED. Positive current-scope SQL pagination, full HTTP coverage and real accepted replacement content/witness/Apply lifecycle remain open. No tests/builds/validators/generators/migrations executed; no module completion credit.


## V29 multi discovery HTTP contract coverage (2026-10-11)

Separated package-private already-authenticated multi discovery handlers from routes retaining tenantRequest at both mounted routes. Set no-store before authentication and parsing, covering failure responses. Authored HTTP cases for scope/list success and exact identity forwarding, missing/bad/duplicate decision hash, malformed cursor, missing/unsupported/duplicate operation, service unavailable scope and unauthenticated mounted routes. Tests NOT RUN. Full positive SQL pagination/content/witness/Apply lifecycle remains open; no builds/validators/generators/migrations executed or completion credit.


## V29 multi replacement full content and Apply evidence fixtures (2026-10-11)

Authored full physical content→original inventory→Apply evidence fixtures using existing complete array/document pipeline under deterministic replacement namespace for planar panorama, cylindrical panorama and focus stack. Covers complete witnesses, owned root/image/witness/original buffers, foreign project/producer/review and stripped private identity rejection, missing output capture, corrupt physical shape/alpha/mask and stream cleanup. These tests exercise content/witness authority rather than only pure identity derivation; they do not prove SQL Apply/admission or deployed worker lifecycle. Tests NOT RUN; no builds/validators/generators/migrations executed. Positive SQL lifecycle, mixed original/replacement frames and concurrent/ambiguous Apply remain open.


## V29 multi replacement Apply installation closure (2026-10-11)

Source review traced Apply through complete→retained→version, current output witnesses and current original inventory. No extra ordinary plan read found in these wrappers: they use previously routed preview/inventory resolvers. Extended replacement scheduler installation gate with private Apply/witness/original helpers and six required routing links, so publication cannot start against an incomplete downstream Apply installation. Authored isolated migrated database startup-gate and API-complete-only/no internal Apply bypass privilege tests NOT RUN. No migrations/builds/tests/validators/generators executed. This is installation coverage, not a positive SQL admission/Apply lifecycle; mixed frames, concurrency and ambiguous commit fixtures remain open.
