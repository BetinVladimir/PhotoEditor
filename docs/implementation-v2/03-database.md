# База и миграции: нормативная версия 2.2

[database.json](contracts/database.json) перечисляет 40 таблиц, все колонки, nullability, defaults, unique/check и FK. [schema-reference.sql](contracts/schema-reference.sql) — генерируемая схема таблиц, FK, индексов и RLS, а не готовая production migration. Кодогенератор обязан дополнить её функциями/триггерами ниже и разделить по полю migration. Нельзя запускать этот reference SQL на production.

## Версионирование и документы

v2_document_streams — mutable указатели current/approved; v2_documents — immutable версии с UNIQUE(tenant,stream,version_no); v2_document_refs — immutable ссылки с UNIQUE(tenant,document,purpose,ordinal). Замена draft создаёт новый document_id и новые refs, поэтому не конфликтует со ссылками предыдущей версии. v2_document_links прежнего проекта исключена. History удерживает все artifacts до явно разрешённого retirement.

JSON в SQL ограничен 1 MiB, changed_paths имеет тип array; policy имеет тип object и SessionPolicy schema, capture_policy — nullable CapturePolicy. Остальные большие документы хранятся artifacts, до 8 MiB на JSON. Body HTTP до 1 MiB; если полный план больше — получаем artifact по авторизованному download intent. Ни произвольные storage URL, ни object keys клиенту не выдаются.

Каждая новая таблица имеет tenant_id/project_id, composite internal FK с обоими scopes. Существующие FK используют tenant/id; guard_v2_project_links дополнительно проверяет project через parent asset/collection/run. Global user FK не содержит tenant. Нет ON DELETE CASCADE. Nullable circular FK current_document_id, approved_document_id, active_version_id связываются после создания таблиц; при tenant purge сначала обнуляются в разрешённом deletion state.

## Обязательные функции и триггеры

SECURITY DEFINER owner photo_fn_owner; SET search_path=pg_catalog,photo; REVOKE ALL FROM PUBLIC. Вход principal вычисляется доверенным authenticated API; произвольный actor из JSON запрещён. SQL функции проверяют tenant binding, membership/role и lifecycle, даже когда API уже проверил.

- guard_v2_project_links(): BEFORE INSERT/UPDATE. Проверяет все external FK по chain ownership; asset→collection/project, run→snapshot/project. Export preset tenant-scoped и намеренно может использоваться в нескольких проектах; artifact требует authorized project binding. Для workflow проверяет run.kind=v2 и точное совпадение snapshot. Для correction — same stream/source/project и approved document evidence. FK face_id — не отдельная таблица: ID должен присутствовать в подписанном faces artifact соответствующей версии; validator запрещает другой face.
- guard_v2_immutable(): BEFORE UPDATE на всех mutable=false, всегда reject. DELETE доступен только lifecycle function после её собственных проверок; API/worker не имеют привилегии. Immutable history не UPDATE даже для отзыва согласий.
- bump_v2_revision(): BEFORE UPDATE на mutable; NEW.revision=OLD.revision+1, updated_at=statement_timestamp(); вызывающая функция сначала проверяет expected. Не допускается присваивание клиентом.
- replace_v2_draft(tenant uuid, asset uuid, expected bigint, document uuid, artifact uuid, sha text, actor uuid) returns uuid: lock stream, CAS, версия+1, validate recipe/layers, insert document+все nested refs; update current. Старая версия остаётся.
- approve_v2_draft(tenant uuid, asset uuid, expected bigint, document uuid, actor uuid) returns jsonb: lock stream, документ=current, correct source; immutable evidence в v2_document_approvals и audit; set approved_document_id, bump revision; вернуть DraftApprovalResult. Уже сохранённый документ не обновляется.
- adopt_v2_plan(workflow uuid, task uuid, attempt int, fence bigint, result uuid) returns boolean: полный алгоритм 09; canon result включает artifact/digest. false только stale; validation failure — domain error, не silent success.
- activate_learning_version(profile uuid, expected bigint, version uuid, actor uuid, reason text) returns bigint: locks grants→profile; evaluation eligible, all epochs, immutable lineage; set active, activation event, outbox. Rollback проходит те же проверки.
- change_v2_consent(consent uuid, expected bigint, action text, actor uuid, policy text) returns bigint: 11; grant новый epoch, revoke disables all dependent models.
- mutate_v2_resource(tenant uuid, project uuid, command text, expected bigint, body jsonb, actor uuid) returns jsonb: command allowlist ровно из17, switch с отдельной typed validator, никаких dynamic table names/SQL из body. Каждая branch имеет самостоятельный negative-role test.
- retire_v2_document(document uuid) returns boolean: maintenance only; TTL90days, не current/approved, не referenced by correction/model/workflow/publication; снять refs и удалить history только в этой функции, с audit. Строгая immutability означает отсутствие UPDATE, не вечное хранение после разрешённого retirement.
- cleanup_v2_consent(consent uuid, limit int) returns int: photo_maintenance, limit1..100; ledger алгоритм11. Storage delete вне TX, receipt/finalize внутри TX.

EXECUTE: photo_api получает draft/approval, activate/consent, mutate; photo_scheduler — adoption/finalization; lifecycle использует фактическую существующую maintenance role, проверенную на baseline. photo_worker не получает эти функции и DML новых таблиц. Reference grants owner DELETE перечислены по таблицам; запрет DEFAULT PRIVILEGES. Реализация не должна создавать новое общее право удаления API.

## Миграционный порядок

Фактический head репозитория — `00232`; `00196` не использовалась. Уже созданные миграции менять нельзя. Поле `migration` в `work-packages.json` и `contracts/database.json` — стабильный логический слот исходной спецификации; созданные физические номера перечислены здесь и в `physical_migrations` пакета. Начиная с `00233`, выделять номера по фактическому head для ещё не созданного пакета; никогда не перезаписывать применённый файл.

| Миграция | Пакет | Содержание |
|---|---|---|
| 00197 | V21 | workflows, versioned documents, qualifications, draft backend; runs/snapshots kind v2; finalizer/adoption |
| 00198 | V22 | sessions, persons, observations, consents, workflow lineage; session operations |
| 00199 | V22 | materialization ScenePlan, correction sources and locks |
| 00200 | V22 | atomic Analysis Apply decision and parent completion |
| 00201 | V22 | authorized InputSnapshot source for Analysis Apply |
| 00202 | V22 | authorized sealed replay of Analysis Apply |
| 00203 | V22/Apply | tenant-scoped API admission of Apply build row |
| 00204 | V23/V22 | девять V23 metadata tables, immutable guards, GC/purge refs; V22 decision added to exact purge allowlist; V23 writers and operation registry still disabled |
| 00205 | V23 | атомарная запись RecordCorrection с проверкой approval/source/session/consent и replay |
| 00206 | V23 | создание learning profile через project/person-scoped SECURITY DEFINER admission |
| 00207 | V23 | доверенный scoped список training candidates с consent и immutable artifact readiness |
| 00208 | V23 | run-scoped TrainingManifest transport с pinned SHA, quota и owner scope |
| 00209 | V23 | детерминированные ID TrainingManifest и ArtifactSnapshot документов для предварительного snapshot digest |
| 00210 | V23 | V2.Admit draft learning run с pending ArtifactSnapshot, quota и idempotency до публикации объектов |
| 00211 | V23 | freeze manifest после готовности двух документов, повторная проверка candidates/consent, immutable snapshot и seal guard |
| 00212 | V23/V22 | запрет изменения root artifact_id и created_at у sealed V2 snapshot |
| 00213 | V23 | JCS bytes recovery в V2.Admit idempotency record и scoped pending/replayed probe |
| 00214 | V23 | learning.fit CPU registry/output policy, v2_enabled=false до полного admission |
| 00215 | V23 | scoped fit Params document после freeze и идемпотентная blocked task stage |
| 00216 | V23 | атомарное открытие evaluation barrier после canonical adoption learning.fit |
| 00217 | V23 | scoped canonical CandidateProfile input и fenced claim evaluation barrier |
| 00218 | V23 | renew/release fenced evaluation lease с проверкой cancellation и canonical candidate |
| 00219 | V23 | fenced learning.evaluate Params backend document после canonical fit adoption |
| 00220 | V23 | learning.evaluate GPU registry/output policy, dispatch disabled до готового worker |
| 00221 | V23 | fenced blocked evaluate task stage с exact candidate/params и edge от fit |
| 00222 | V23 | атомарное закрытие learning.evaluate barrier после точного sealed input closure; dispatch остаётся disabled до готовности worker |
| 00223 | V23 | identity.compare GPU registry и три output policy; dispatch disabled до квалифицированного worker |
| 00224 | V23 | canonical fit/evaluate adoption создаёт immutable learning version, eligibility и consent lineage без активации |
| 00225 | V23 | CAS human safety review и явная activate/rollback с live consent epochs |
| 00226 | V23 | отзыв learning consent отключает зависимые версии и снимает active pointer |
| 00227 | V23 | постановка derived learning artifacts в consent cleanup ledger при отзыве |
| 00228 | V23 | tenant audit для safety review и активации профиля |
| 00229 | V23 | публикация draft с active learning dependency и immutable model_use_link |
| 00230 | V23 | approval повторно проверяет learning consent и eligibility после публикации |
| 00231 | V23 | approval также сверяет active version профиля после публикации |
| 00232 | V23 | approval атомарно закрепляет model и source descriptor refs в document closure |
| 00245 | V23 | run/event-scoped FrozenRecipe publication с детерминированными ID, SHA из recovery manifest и квотой |
| 00246 | V23 | freeze проверяет готовые FrozenRecipe отдельно от исходных EditingRecipeV3, сохраняя оба source-document ref |
| 00247 | V23 | новый SHA строгого RenderCase контракта при сохранении допуска прежнего SHA для уже запущенных V2 tasks |
| 00248 | V23 | SHA RenderCase с DescriptorShard provenance; seal выпускает новый SHA, ранее запечатанные V2 tasks сохраняют допустимые SHA |
| 00249 | V23 | scoped SECURITY DEFINER чтение base/approved EditingRecipeV3 для training admission: project/asset/document stream, approval, ready object и текущий consent; только photo_scheduler получает EXECUTE |
| 00250 | V23 | scheduler EXECUTE для locked candidate inventory и scoped SECURITY DEFINER чтение project/mode/person learning profile в той же транзакции |
| 00251 | V23 | scoped mask provenance для training: MaskRef в immutable closure correction event, binary child в canonical output успешного producer task того же проекта, live consent и role allowlist |
| 00252 | V23 | scoped перечисление ready JSON refs из immutable closure correction event для детерминированного выбора единственной protected mask; только photo_scheduler получает EXECUTE |
| 00253 | V23 | DBA-pinned `training_calibration_sha` в qualification policy и scheduler-only scoped чтение точного Calibration artifact при активной trusted qualification; forward-only |
| 00254 | V23 | scheduler-only scoped выбор active baseline модели и её training artifact SHA; отсутствие active version отличается от повреждённой/disabled версии; forward-only |
| 00255 | V23 | run/profile-scoped deterministic zero-profile publication с SHA из recovery manifest, квотой и запретом при active version; только photo_scheduler получает EXECUTE; forward-only |
| 00256 | V23 | freeze-time trigger сверяет baseline ref в recovery с ready zero-profile либо с текущей eligible active version; смена active version до freeze даёт конфликт; forward-only |
| 00257 | V23 | scheduler-only вывод model/dataset lock из единственной активной session.describe qualification по pinned Calibration SHA; forward-only |
| 00258 | V23 | scoped чтение exact ready FrozenRecipe refs из pending recovery manifest для повторного admission; только photo_scheduler получает EXECUTE; forward-only |
| 00259 | V23 | атомарное сохранение канонических FrozenRecipe bytes в pending idempotency recovery с проверкой SHA против manifest; лимит 32 MiB, после seal recovery заменяется JobTicket; forward-only |
| 00260 | V23 | разрешённый JSON output `mask_protected_ref` для mask.rasterize только при ненулевой явной защите; существующие outputs сохраняются; forward-only |
| ≥00233 | V23 | identity.compare worker, qualification, consent revoke/cleanup integration до включения writers |
| после V23 | V24 | devices/challenges/receipts/alerts/shot lists, capture policy |
| после V24 | V25 | optical.lens registry/policy и qualification, RAW version binding |
| после V25 | V26 | layer/region-preview registry integration, UI capabilities |
| после V26 | V27 | semantic tables, transfer/assembly policies |
| после V27 | V28 | portrait/reflection/relight policies; identity.compare уже в V23 |
| после V28 | V29 | optical multi-input policies и lineage |
| после V29 | V30 | delivery/interchange tables и assembly/parser policies |
| после V30 | V31 | provenance tables, isolated signer policy |
| после V31 | V32 | reconciliation assertions, capacity/restore deployment gates |

Каждая миграция атомарно: tables/constraints → functions/triggers → explicit grants → GC/purge refs → registry policies → assertions. Ни одного пакета с writers enabled до GC integration. Каждый пакет самостоятельно deployable после предыдущего; V32 не блокирует тестирование registry предыдущих пакетов.

Backfill: новые feature rows пусты; существующие users/assets/runs не переводятся в v2. Consent grants lazily provisioned под lock project при первом feature enable; draft stream+base checkpoint+current document создаются при первом PUT с expected_revision="0" и проверкой отсутствия stream, GET не пишет. Для отсутствующего draft GET=404; UI берёт base recipe из существующего editor. Initial stream revision1, после установки current первый save возвращает revision2; следующие записи CAS текущей revision, алгоритм17. Конкурентный first PUT: unique stream + CAS гарантирует один успех, другой409.

Forward-only. Откат релиза — flags off, drain/fence tasks, совместимый предыдущий бинарник; DROP колонок не является rollback. Purge порядок: leaf links → eligibility/activations/children → versions/documents → workflows/sessions/persons; grants после их links/events. Circular pointers обнуляются до удаления. Существующий finish_tenant_deletion расширяется закрытым allowlist и проверяет object-cleanup proof.

Acceptance: fresh install baseline→215 и populated upgrade; exact schema snapshot; каждый FK negative tenant/project; privilege matrix; immutable update denied; CAS races; purge после storage receipt; restore и mixed old/new runs. Reference SQL ещё не является доказательством успешного PostgreSQL upgrade.

Transport input closure и новая v2_task_inputs описаны19; графовые barriers используют существующий graph_expansions. Полная матрица EXECUTE и immutable approval —17.

### V26 current-draft frozen recipe registration fence

Migration `00463_v26_render_draft_freeze_fence.sql` adds nullable paired `render_draft_document_id`/`render_draft_revision` to existing `v2_draft_uploads`, with positive revision, tenant-bound draft document FK and no second publication ledger. Existing registrations remain compatible. New `register_v26_render_draft_recipe(...existing registration args..., draft_document, expected_revision)` is fixed-search-path SECURITY DEFINER owned by photo_fn_owner, executable only by photo_api among runtime roles. It serializes via tenant_usage → current draft stream → immutable upload locks, requires active editor/session authority, exact current document/revision and ready nonexpired authored artifact whose SHA equals frozen recipe_sha256, then invokes existing source/background registration and stores the draft provenance pair. Existing incompatible stamps cannot be rebound. A failed fence returns false; the Go adapter maps it to REVISION_CONFLICT. Upload IO occurs before this transaction and uses the existing cleanup lifecycle on failed registration. Successful old snapshots remain immutable; changing the current draft rejects a new registration or replay at the old expected revision. This fence does not prove full recipe semantics or model qualification and is not itself an HTTP endpoint.

Migration `00464_v26_render_frozen_draft_scope.sql` adds the API-only boolean v26_render_frozen_draft_scope proof for completed freeze replay. It binds tenant, asset/version, deterministic frozen document, authored document/revision, recipe artifact/SHA and original WorkingImage pin under fresh actor/session scope, then calls the existing live Render root scope. It retains fixed search_path and photo_fn_owner ownership. No SELECT grant on v2_draft_uploads is added. The proof describes a previously sealed immutable snapshot and therefore does not require the current draft revision to remain unchanged after a successful freeze.


Migration `00465_v26_render_frozen_learning_scope.sql` adds render_learning_checked (default false), paired nullable render_learning/model-lock to private v2_draft_uploads. Existing uploads are not backfilled from unverified object bytes. The original draft registration is renamed to an internal core with API execution revoked; an API-only result function returns registered/conflict/learning_expired and a compatible boolean wrapper retains the existing signature. A PL/pgSQL subtransaction rolls back core source/authored/draft stamps on denied learning scope even when a caller commits the false boolean result. Successful registration validates exact frozen learning shape and authored learning-version binding, proves the existing live model and adopted descriptor scopes and persists the pins. No new private table SELECT permission is granted.

The completed v26_render_frozen_draft_scope now requires checked metadata and revalidates stored model/descriptor evidence. Old unchecked completed snapshots return410; the caller must issue a new freeze/key or perform authorized registration with verified original bytes. Existing rows and objects are preserved. Learning-free new registrations explicitly record checked=true with null learning/model lock. Learning eligibility/consent/provenance revocation denies replay; live predicates do not by themselves prove exclusion against a concurrent revocation transaction. The migration is forward-only.


### 00901 — person admission scheduling

Forward-only migration00901_v27_person_admission_recovery.sql registers scheduler-only enable/discovery/claim/record functions for deterministic person children. It reuses existing guarded runs.v27_initial_recovery_* metadata, with independent person queue semantics. No new table/DELETE grant, no legacy backfill. Enable is atomic with blocked draft creation and authenticated by original saved stage; claim uses live original editor/session/project and SKIP LOCKED. Record requires unexpired matching token and an existing sealed admission receipt before done. Retry backoff5–300seconds; waiting5seconds. It schedules child admission only. Existing detector queue remains separate. Function EXECUTE deny tests use actual platform roles; fresh migration and interrupted transport recovery require physical PostgreSQL evidence.


### 00902 — ordinary-origin parent continuation

Forward-only00902_v27_ordinary_parent_continuation.sql reuses runs.v27_parent_recovery_* on independent person runs, leaving existing detector/manual queue semantics intact. Atomic person enable additionally enables parent continuation only for explicit ordinary origin and same-project owned policy in immutable recovery metadata. Scheduler-only next_v27_ordinary_parent_project, claim_v27_ordinary_parent and record_v27_ordinary_parent require live original actor/session/project/editor and deterministic sealed person child. Completion requires exact original parent202 receipt, user/key/body hash/workflow; retry/waiting keep bounded backoff and clear token lease. No new table/DELETE grant/backfill. Full immutable ordinary_policy contents are revalidated by Go before any parent action.

### 00903 — native person dispatch identity and atomic start

`00903_v27_person_dispatch_identity.sql` forward-only replaces existing `reserve_dispatch_resources` and `claim_task` bodies while retaining their OIDs, signatures, role grants and all existing resource/legacy/Apply guards. For `mask.person@1` renderer `v2.2`, dispatch uses the exact `model_lock_sha` as command model identity and rejects contradictory `model_lock_id`. Claim requires the worker registration lock and command model lock to match the pinned SHA, then invokes the existing claim machinery. A successful claim atomically starts the sealed `v27_person_prepare` run after checking original task/digest/closure/workflow bindings. No saved profiles/configs are rewritten; no tenant tables or DELETE grants are added. Checkpoints missing qualified identity pins remain unavailable, not silently repaired or qualified.


### 00904 — native face execution identity and atomic start

Forward-only `00904_v27_face_dispatch_identity.sql` extends the existing native model dispatch/claim guards to face.detect, face.landmarks and face.parse@1/v2.2. Dispatch uses the immutable model_lock_sha, refuses contradictory model_lock_id and retains original resource, input, worker qualification and quota checks. Worker claim requires exact task/command/registration model identity. Successful claim starts the original queued detector/face run atomically, after exact stage task/input_digest/operation, sealed graph, noncancelled run, complete input closure and semantic workflow guards. A stale stage raises SEMANTIC_FACE_START_STALE and rolls back the underlying claim. Person, defects and approved-semantic Apply branches remain in the wrapper.

No new table, DELETE privilege, function signature, function OID or service EXECUTE grant. No existing profile/config rewrite or recovery backfill. Identity-free historical native face checkpoints remain byte-identical and unavailable for model execution; migration cannot synthesize qualification. Incremental00899→00904 checks tenant data after every migration and function identity/ACL/security properties. Existing Apply execution-to-completion and crash/rollout acceptance still require separate evidence.

### 00905 — atomic native skin claim start

Forward-only `00905_v27_skin_claim_start.sql` replaces the existing claim_task wrapper without changing its signature, OID, owner or ACL. A successful mask.face_skin claim starts its original sealed v27_skin_prepare run atomically, after exact stage task/input_digest/operation, noncancelled run, closure and semantic workflow validation. A stale binding raises SEMANTIC_SKIN_START_STALE and rolls back the claim. Existing face/person/defect/Apply guards remain. No new table, grant, checkpoint rewrite or recovery backfill; Down refuses. Populated00899→00905 upgrade compares every tenant-scoped row after each migration and preserves existing function identity/security properties.

### 00906 — atomic native-input defect claim start

Forward-only `00906_v27_native_defect_claim_start.sql` extends the existing claim_task stage mapping to defects.propose with v27_defect_native_prepare. This is distinct from the imported v27_defect_prepare branch. After the underlying successful claim, the wrapper requires exact task ID/input digest/operation/version, sealed noncancelled queued/running run, ready closure and semantic v2.2 workflow. It atomically starts queued native defect runs; stale authority raises SEMANTIC_NATIVE_DEFECT_START_STALE and rolls back the claim. All earlier model/person/skin/imported-defect/Apply guards remain. No signature/OID/owner/ACL change, new table/grant, metadata rewrite or checkpoint backfill; Down refuses. Populated00899→00906 migration test compares existing tenant rows after each migration and preserves existing function identity/security metadata.

### 00907 — exact original person input object lock

Forward-only00907 creates lock_semantic_native_person_input_objects with photo_fn_owner ownership, fixed search_path and only photo_scheduler EXECUTE. It first repeats the existing source-bound person producer/profile lock, then compares exactly two ordered root/pixel catalog pins (ID/SHA/bucket/key/version/bytes/media) with the caller's physically verified observations. Both objects must remain ready, verified, unexpired and versioned. Root and child cannot coincide. Extra/missing/reordered/altered pin fields refuse through exact JSON equality. No table, DML grant, checkpoint/profile rewrite or parent admission; Down refuses. Existing functions/OIDs/ACL remain unchanged. Populated00899→00907 upgrade test validates the new function's exact service allowlist and unchanged tenant metadata after every migration.


### 00912 — immutable original target preparation checkpoint

Forward-only `00912_v27_target_preparation_checkpoint.sql` добавляет nullable `runs.v27_target_manifest text` и `runs.v27_target_manifest_sha text`. Существующие значения остаются NULL; capability activation, задачи, квоты и receipts не создаются. CHECK требует пару значений, размер bytes 2..8388608, lowercase SHA256 и равенство SHA исходным UTF-8 bytes. INSERT с заполненным checkpoint запрещён; UPDATE допускает только первую запись `photo_fn_owner`, сохранённую пару нельзя менять даже владельцу. Application DML grants не расширяются.

`save_v27_target_manifest(tenant uuid, detector uuid, evidence jsonb, body text) RETURNS jsonb` — SECURITY DEFINER, EXECUTE только scheduler. Контекст tenant обязателен. Исходный native admission anchor повторяет authorization, ordered inventory, intent, snapshot и current recipe heads под locks. Затем tenant/usage → parent → original detector; проверяются canonical config hash/bytes, actor, snapshot ID, sealed v2 run, отсутствие cancel/manual selection и допустимый queued/running/succeeded status. Из исходного snapshot SQL независимо строит namespaces и DAG всех targets по алгоритму15. Сравнивается точный canonical body. Успех первой записи или идентичного replay возвращает `{body,sha}`; отказ — NULL. Любой replay повторяет актуальную authority. Ошибочный JSON/UUID/числовое переполнение — NULL. Lock timeout/deadlock/IO errors не маскируются.

Private serializer `v27_target_manifest_json_text` применим только к серверной manifest grammar (ASCII names, UUID/hex/operation strings, nonnegative integer/boolean); он не является общим JCS API. Private helpers не имеют application EXECUTE. Metadata находится в существующей `runs`, новый purge allowlist не нужен. После подтверждённой object cleanup существующий tenant purge удаляет run вместе с checkpoint.


### 00913 — target entry scope lock

`lock_v27_target_preparation_entry(tenant uuid, detector uuid, evidence jsonb, manifest_sha text, ordinal integer, operation text) RETURNS jsonb` — SECURITY DEFINER, scheduler-only EXECUTE. Требует original native anchor с текущими authorization/inventory/documents/recipe-head locks, exact sealed original detector/config hash, active run без cancel/manual selection и persisted manifest SHA. Повторяет hash stored bytes, original parent/project/actor/session/request/intent/snapshot headers, count полного frozen inventory и границы ordinal0..4999. Для выбранного target независимо вычисляет namespace и весь шестистадийный DAG; сравнивает exact saved entry. Reference reuse возвращает NULL: исходные person/face/defect producers имеют отдельную authority. Unsupported operation, отсутствующий checkpoint, изменённый scope или entry возвращают NULL. Успех возвращает entry под locks до TX commit; readiness dependencies, output provenance, resource admission и seal этим не подтверждаются. Новых таблиц/прав DML/capability activation нет; Down запрещён.


### 00914–00915 — target person transport registration and pending replay

00914 adds private `lock_v27_target_person_graph_scope(tenant,run,task)` and scheduler-only `semantic_target_person_registration_state(tenant,run,task,params,params_sha,digest,profile,rows,manifest,chunks,direct,count,entries_sha)`. The scope gate locks active tenant/usage/original parent/detector/target run/task and independently checks immutable target manifest, namespace/ordinal/stage, original scope/session and deterministic workflow/task IDs. Registration returns missing only for an entirely empty blocked draft; exact registered rows, ordered transport, closure/profile and ready backend-document ownership return replayed. Partial or substituted state returns conflict and is never repaired. This metadata gate does not replace physical source, Params or qualification authorization.

00915 replaces the body of `v2_pending_shell_matches` preserving OID/owner/ACL. Its new target-only branch accepts an exact registered target person profile by passing the stored transport-augmented profile to the original shell matcher; base profile, Params/digest, deterministic namespace, singleton task/no edges and ready closure must match. Mixed source/target configs refuse. Existing skin and all other matcher branches remain intact. Both migrations are forward-only, add no tenant tables, DML grants or registry activation, and do not seal/queue tasks.


### 00916 — target person admission authority

Forward-only00916 adds private `semantic_target_person_registration_binding_state(tenant,run,binding)` with strict envelope/ref/contract/count bounds and target-specific registration replay, plus scheduler-only `lock_semantic_target_person_admission_evidence(tenant,run,packet)`. Packet contains schema_version1, original detector anchor and target person evidence. SQL obtains current original entry through00913, requires original anchor/request equality and target binding, exact immutable config byte hash/policy SHA, current session/source/Params/qualification locks, replayed registration and the existing singleton graph/live reservation budget gate. No source-reference compiler is repurposed, no existing function replaced, no DML/activation permission granted. This function proves admission prerequisites only; it never seals or queues.


### 00917 — dedicated target person atomic seal

Forward-only00917 adds scheduler-only `seal_semantic_target_person_preparation(tenant,run,packet)` and four private sealed-state validators (graph scope, registration rows, binding envelope, full evidence). It leaves existing reference/native admission functions unchanged. Before target locking it locks the exact current original checkpoint entry, preserving original-parent/detector→target order. Initial activation requires current00916 admission, ready closure, allowed operation registry, no consent links, live reservation and exact pending `target-person-prep:<run>` idempotency record. The transaction marks the graph sealed/run queued, activates the bound task to ready, writes one principal event/outbox and commits the immutable202 receipt. Any activation/event/ticket failure aborts the transaction.

Sealed replay independently rechecks original entry/scope/current session, immutable config/policy, target namespace/source/Params/qualification, exact closure/backend ownership and singleton graph. It requires the exact saved202 receipt and does not activate/write events again. It does not require an unused quota reservation after execution, but still rejects canceled/non-executable target scope. New private functions have no application EXECUTE; no tenant table or DELETE/DML grants are added. Worker target-specific atomic run start/result recovery remain separate work.


### 00918 — target person claim/start

Forward-only00918 replaces only the body of `claim_task` while retaining signature/OID/owner/ACL. All existing source person/face/skin/defect/Apply branches and qualified model command/worker-registration checks remain. A successful claim of a sealed `v27_target_person_prepare` requires mask.person@1/v2.2, queued/running noncancelled run, exact task/input digest, target namespace-derived run/workflow/task IDs and node key, model/runtime/device pins, ready closure and semantic workflow. It atomically starts the run; stale target identity raises SEMANTIC_TARGET_PERSON_START_STALE and rolls back the underlying claim. No profile rewrite/backfill, capability activation or grant is added.


### 00919 — target person canonical output guard

Forward-only00919 adds private `semantic_target_person_result_ready(tenant,task,attempt,fence,outputs)` and extends the existing `guard_v2_result_root` body without changing its OID/owner/ACL or any earlier branch. Target person has four ordered outputs: person_mask/octet-stream, person_mask_png/png, person_mask_ref/json, person_report/json. Each must be distinct, ready, verified, versioned/unexpired, owned by the exact producing task/attempt/fence, and match a completed ready output_upload in role, bytes, SHA, bucket/key/version/media. Run must be sealed/running/noncancelled, namespace/run/workflow/task/config/digest/closure exact. Generic single-root validation remains unchanged for other operations. This verifies output metadata at canonical commit, not semantic mask contents or downstream adoption authority.


### 00920 — exact target person canonical result authority

Forward-only00920 adds API-only `semantic_target_person_canonical_outputs(tenant,project,actor,session,run,config_sha)` and scheduler-only `lock_semantic_target_person_result(tenant,run,packet,result_id,outputs)`. Discovery reads only the deterministic target task's exact canonical task_results row/fence, matching immutable config, original detector checkpoint/selected entry, accepted target source and current editor authorization. It returns result ID and all four ordered outputs. It grants no mutation/adoption authority.

The lock repeats full current original/target sealed evidence, requires the same discovered canonical result ID and complete ordered output array, locks task/result and repeats exact output ownership/upload readiness and person origin. A source-wide matching mask, foreign result/report or old receipt cannot substitute this task's result. No state transition, table, metadata backfill or existing function replacement occurs. Dedicated target run finalization and consumer registration must retain this lock in their own transaction.


### 00921 — target person producer finalization and residual quota

Forward-only00921 corrects target terminal run checks to the existing `completed` status, retaining function OIDs/owner/ACL. Original source branches are not replaced. A new private canonical output readiness predicate accepts succeeded tasks in running/completed target runs with unchanged exact four-output upload provenance; the worker commit guard00919 remains running-only. Target sealed graph/registration/evidence and canonical discovery/consumer lock now support completed producer replay.

Scheduler-only `finish_semantic_target_person(tenant,run,packet,result_id,outputs)` first repeats the entire current result authority. It requires exactly one user-owned run reservation (files0), no unsettled uploads/backend documents and a nonexpired reserved residual. In the same transaction it subtracts only q.bytes from tenant reserved usage, releases that reservation to bytes0, marks run completed with bounded revision, and emits one principal v2.run.completed event/outbox. Inconsistent usage, activation races or event exhaustion abort. Exact replay revalidates result/current scope and requires released/zero reservation without writing/decrementing again. Revoked authorization cannot replay a historical finish. No new tenant tables or DML/DELETE grants.


### 00922 — target saved state and transport reconstruction

Adds API-only `semantic_target_person_saved(tenant,project,actor,session,parent,ordinal,manifest_sha)` returning absent only when deterministic target run and its idempotency key are absent. NULL means unauthorized/corrupt/unavailable and never permits repair. Saved response binds original checkpoint/session/project/actor, independently reconstructed target namespace, source/intent/snapshot/request, singleton person task/config SHA/Params/digest and base execution profile. It carries recovery locators and observed run/task/seal state, not admission authority.

Scheduler-only `semantic_target_person_registered_transport(tenant,run,task,actor,session)` reconstructs manifest/chunks/count/SHA from stored task profile/input rows and requires exact target registration or sealed-binding replay. It performs no PUT, repair, quota change or activation. Caller first retains original entry locks in the same transaction. Existing original transport reader remains unchanged. Both functions are forward-only with fixed definer search_path/owner and precise role allowlist; no tenant table or DML grant.

### 00923 — target detector registration

Adds private `lock_v27_target_detector_graph_scope(tenant,run,task)` and scheduler-only `semantic_target_detector_registration_state(tenant,run,task,params,params_sha,digest,profile,rows,manifest,chunks,direct,count,entries_sha)`. The graph predicate independently derives the original detector and target namespace from the saved full manifest, validates face.detect stage index1 and deterministic run/workflow/task/Params identities, current editor/session and singleton blocked draft scope. Registration returns missing only when all task inputs and transport profile are absent, replayed only for the complete exact rows/direct/profile/chunks/manifest/SHA/backend-document closure; otherwise conflict. Partial registration must never be repaired automatically.

Replaces `v2_pending_shell_matches` in place with an additional target-detector registered-profile branch while retaining target-person, skin and delegated source behavior. Existing OID/owner/ACL are preserved. No table grants, source matcher renames, activation or queue admission are added. Migration is forward-only.

### 00924–00925 — target detector admission and atomic seal

00924 adds private `semantic_target_detector_registration_binding_state` and scheduler-only `lock_semantic_target_detector_admission_evidence`. The packet has schema_version1, original and target evidence; the SQL gate locks the original immutable manifest entry, checks target config/policy hashes, current source/Params/qualification authority, complete registered transport and singleton graph/live reservation budget. It grants no activation.

00925 adds private sealed graph/registration/binding/evidence validators plus scheduler-only `seal_semantic_target_detector_preparation`. First seal retains original and target authority locks through blocked task activation, queued run revision, one queued event/outbox and202 idempotency receipt. Sealed replay repeats current source/Params/qualification/registration/session authority, requiring the exact receipt. Consent-bearing graph links refuse admission under the existing native contract. No app table privileges are added; both migrations are forward-only. Completed target detector finalization is a separate future lifecycle change.

### 00926–00927 — target detector worker start and canonical outputs

00926 replaces `claim_task` in place, preserving OID/owner/ACL and previous source/person/Apply branches. A successful qualified face.detect claim must match the target run namespace, task ID/node key/digest and sealed transport, explicit model/runtime/device pins and semantic workflow. It atomically advances a queued target run to running with bounded revision.

00927 adds private `semantic_target_detector_result_ready(tenant,task,attempt,fence,outputs)` and a dedicated branch in `guard_v2_result_root`, preserving other branches. The exact running/sealed target detector can commit only one face_boxes application/json output. Artifact and completed upload must match tenant/task/attempt/fence/role/MIME/SHA/bytes/bucket/key/version; artifact must be ready, verified and unexpired. Params cannot serve as output. Canonical commit replay remains governed by the existing fenced commit path. No application EXECUTE grant is added for the private predicate. Both migrations are forward-only.

### 00928 — exact target detector result authority

API-only `semantic_target_detector_canonical_outputs(tenant,project,actor,session,run,config_sha)` discovers the exact succeeded target face.detect task/canonical result under current editor/session/original manifest and target namespace/config authority. Output must satisfy the strict target task/attempt/fence upload predicate. It returns result_id plus one complete canonical output ref; source-wide candidate substitution is forbidden.

Scheduler-only `lock_semantic_target_detector_result(tenant,run,packet,result_id,outputs)` repeats sealed original/target admission authority and compares exact canonical result ID/full output array, task state/attempt/fence and ready output provenance under locks. It performs no finalization or downstream admission. Existing guards/functions/ACL are unchanged; migration is forward-only. Current target run must remain running; completed producer support is a separate lifecycle requirement.

### 00929 — target detector completed producer lifecycle

Adds private `semantic_target_detector_completed_output_ready` for succeeded canonical detector tasks in running/completed target runs and scheduler-only `finish_semantic_target_detector`. Existing target sealed graph/registration/evidence, canonical discovery and result lock functions are replaced in place to support actual terminal run status completed with preserved OID/owner/ACL. Worker canonical commit predicate remains running-only.

Finalizer revalidates full canonical consumer authority, exactly one user-owned reservation with files0 and all run uploads/backend documents ready or cleaned. First finish requires running run, live reserved quota and bounded revision; it subtracts only remaining reservation bytes from tenant reserved usage, releases reservation to bytes0 and commits completed revision/event/outbox once. Completed replay requires released/bytes0 and current result/session authority, with no repeated usage/event change. Expired quota refuses first finish. Original parent completion and downstream admission are separate.

### 00930 — target detector saved locators and transport restoration

API-only `semantic_target_detector_saved` independently derives the exact target detector namespace/run/task from original parent manifest and current editor/session. It returns absent only when both target run and target-detector admission key are absent; malformed/incompatible/unauthorized state returns NULL. Saved state includes exact config/hash, base profile, Params, digest and task/run lifecycle. Scheduler-only `semantic_target_detector_registered_transport` reconstructs exact stored manifest/chunks and validates dedicated unsealed or sealed registration binding; it never publishes, repairs or queues. Caller retains original entry locks. No table grants or old function replacements; forward-only migration.

### 00931 — target landmarks/parse registration

Forward-only migration adds private `lock_v27_target_face_graph_scope` plus scheduler-only `semantic_target_face_preparation_registration_state` and `semantic_target_face_registration_binding_state`. No new tables or data backfill. Graph scope locks tenant usage, original parent/reference detector, immutable target manifest entry and target child; it verifies target-stage deterministic IDs and exact completed target detector canonical result/attempt/fence/output. Registration distinguishes missing/replayed/conflict, verifies ordered full closure and bounded strict transport envelope, and requires run/task-owned backend transport documents. It does not seal or queue tasks.

`v2_pending_shell_matches` is replaced in place to add only target face registered replay while preserving all previous branches, OID, owner and ACL. Upgrade inventory must prove historical metadata unchanged and application grants restricted to the explicit scheduler entrypoints. Down migration refuses rollback.

### 00932 — qualified target face admission

Adds scheduler-only `lock_semantic_target_face_admission_evidence(tenant, run, packet)`. The strict bounded packet retains face evidence, full original/target detector admission envelope and exact canonical detector result/object pins. It locks the dedicated completed target detector evidence, immutable target stage entry, downstream qualification/Params, full file closure and live graph quota. Object pins must match ready artifact metadata. Target namespace/ordinal/manifest/source must agree across face and detector configs. Existing source admission functions and graph-budget implementation remain unchanged. No activation, queue event, schema columns or backfill. Forward-only rollback refusal.

### 00933 — target face atomic seal and sealed replay

Forward-only migration adds private target face sealed graph/registration/evidence validators and scheduler-only `seal_semantic_target_face_preparation`. Initial seal repeats dedicated admission and live quota, verifies registry support and absence of consent links, atomically seals/queues the run, activates the exact bound task, stores one 202 receipt and emits one queued event/outbox item. Sealed replay validates exact receipt, config/policy, current original/target/canonical detector authority, qualification, Params and complete registered closure before returning the original queued ticket. It never repairs partial state or creates another reservation/event. No existing functions replaced and no row backfill. Worker claim/output/finalization remain separate requirements.

### 00934–00935 — target face worker claim and result guard

00934 replaces `claim_task` in place, preserving existing branches/signature/OID/owner/ACL. A successful qualified target landmarks/parse claim revalidates dedicated sealed target scope, canonical completed detector dependency and ready closure, then atomically transitions queued run to running. Tenant context for this barrier is derived from the already claimed task, never from command JSON; the previous transaction-local tenant setting is restored after validation. Invalid scope raises and rolls back the claim. Existing pinned model identity/worker registration checks remain required.

00935 adds private `semantic_target_face_result_ready` and a dedicated target face branch in the existing canonical root trigger. Landmarks output is exactly one face_landmarks JSON; parse outputs are exactly ordered face_labels octet-stream and face_labels_ref JSON. Every output must match ready artifact/upload metadata, producer task/attempt/fence, object version, checksum, role and media type. Reordered, duplicate, foreign or stale outputs refuse. Worker commit guard accepts running runs only; result consumption/finalization remains separate. No data backfill; both migrations are forward-only.

### 00936 — target face canonical output catalog and consumer lock

Adds API-only `semantic_target_face_canonical_outputs` and scheduler-only `lock_semantic_target_face_result`. Catalog checks current editor/session, exact config hash, original target manifest lineage, deterministic operation/run/task IDs and exact succeeded canonical attempt/fence. Output count/order/ownership is enforced by the target face predicate. Consumer lock first repeats dedicated sealed face evidence, then locks exact canonical result and full ordered outputs. No activation or finalization. Catalog currently accepts running prerequisite runs; completed producer support requires the finalization migration. No tables/backfill; forward-only.

### 00937 — target parser labels closure

Replaces `semantic_proposal_face_parse_preparation_pixels` in place, preserving signature/OID/owner/API-only ACL and existing direct/source-native branches. Adds an explicit target-native branch requiring exact parser/detector canonical lineage, original request/session/source equality, equal target namespace/ordinal/manifest, deterministic parser run/task IDs and a ready registered FaceBoxes input. Existing canonical artifact/pixel role/attempt/fence/version/expiry checks remain unchanged. No tables/backfill; forward-only. This is required because target input transport keeps FaceBoxes in indexed closure rather than direct task inputs.

### 00938 — target face completed lifecycle

Adds private `semantic_target_face_completed_output_ready` for exact succeeded canonical task outputs under running/completed prerequisite runs. Existing worker `semantic_target_face_result_ready` and root guard remain unchanged and running-only. Five target consumer/sealed scope functions are replaced in place to support completed receipt/result replay; OID/owner/ACL must remain unchanged.

Scheduler-only `finish_semantic_target_face` repeats full current consumer authority, requires one matching user quota and no incomplete uploads/backend documents, then atomically releases residual reserved bytes, zeroes/releases reservation, transitions run to completed and emits one completion event/outbox item. First completion refuses expired quota; exact completed retry requires released zero-byte reservation and emits nothing. No original parent success, data backfill or migration-time tenant changes; forward-only.

### 00939 — target face saved state and transport recovery

Adds API-only `semantic_target_face_saved` for landmarks/parse and scheduler-only `semantic_target_face_registered_transport`. Saved reader validates current original request/session/manifest/selected entry, deterministic namespace/run/task identity, config hash and ready immutable Params; returns absent or exact saved config/profile without transport fields. Transport reader reconstructs only complete canonical persisted rows/documents and validates the dedicated sealed or unsealed target binding before returning. Partial/conflicting state refuses; no registration repair, reservation, queue activation or object publication. No tables, row backfill or replacement of existing functions; forward-only. Upgrade coverage extends 00899→00939 with exact new-function role allowlist.

### 00940 — completed target parser prerequisite barrier

Adds scheduler-only `lock_semantic_target_skin_parser_completed(tenant,project,actor,session,run,task,result)`. SECURITY DEFINER owner remains photo_fn_owner; PUBLIC execution is revoked. Requires matching local tenant and current editor/session, locks exact target parser run/task/canonical result, verifies completed/sealed/non-cancelled run, deterministic namespace run/task IDs, actor/project/session and succeeded attempt/fence canonical result. It is called after full dedicated physical target face consumer authority lock; this narrow lifecycle check does not replace complete admission evidence. No scheduler table grant, existing function replacement, row backfill or tenant mutation. Upgrade inventory extends 00899→00940. Forward-only.

### 00941 — target skin registration

Adds private `lock_v27_target_skin_graph_scope` validating original detector/request/session/immutable target manifest entry and actual skin stage DAG, namespace-derived run/task/Params identity, blocked CPU graph and completed canonical target parser dependency. Target parser must match source/manifest/namespace/ordinal/parent/request/intent/snapshot/session and exact result/output/Params.

Scheduler-only `semantic_target_skin_preparation_registration_state` and strict `semantic_target_skin_registration_binding_state` require exactly Params, FaceLabels19 manifest and NpyArray labels, complete ordered rows/direct/manifest/chunks, matching immutable config/profile and ready backend-document ownership. Only missing or exact replay succeeds; partial state is conflict. No repair or queue activation. Existing pending-shell function is replaced in place to add a dedicated CPU target skin registered replay branch while preserving all old branches/OID/owner/ACL. No tables, metadata backfill or migration-time tenant mutations; forward-only.

### 00942 — target skin admission evidence

Adds scheduler-only `lock_semantic_target_skin_admission_evidence`. It composes complete target face consumer evidence and exact canonical parser result/output identity, then validates skin immutable config hash, workflow/policy, original lineage and target namespace/ordinal/manifest/source. It requires exact Params/registration/task/input digest and three byte-verified parser object pins (labels, manifest, parser Params), rechecks ready physical artifact metadata, locks target version Params, requires dedicated registered replay and applies existing exact CPU skin graph/live quota budget barrier. No source-reference substitution, activation, receipt, object publication or repair. No existing functions replaced and no migration-time metadata changes; forward-only.

### 00943 — target skin atomic seal

Adds private sealed target skin graph/registration/binding/evidence functions. They accept only sealed active prerequisite lifecycle states, repeat current original/target/session/policy/Params/completed parser authority and exact complete transport without repairing state. Scheduler-only `seal_semantic_target_skin_preparation` checks original user admission key and immutable config/policy, then either returns the exact stored 202 queued receipt after current sealed evidence checks or repeats full dedicated admission/live quota before first activation. First seal atomically seals/queues the run, activates the bound task, persists receipt and emits one queued event/outbox item. Consent-bearing graphs refuse. No existing functions replaced, tables/backfill or migration-time tenant changes; forward-only. Worker claim/output guards, result/finalization and completed-run support remain separate requirements.

### 00944–00945 — target skin worker lifecycle and canonical guard

00944 replaces claim_task in place while retaining all existing source/target/Apply branches. Exact target skin claim derives local tenant context from the claimed task, repeats private sealed target scope and ready closure, restores previous context and atomically changes queued run to running. Invalid scope rolls back the claim.

00945 adds private running-only semantic_target_skin_result_ready and a dedicated target skin branch in the existing canonical root guard. Outputs are exactly ordered skin_mask/octet-stream, skin_mask_png/image/png and skin_mask_ref/application/json. Ready artifact/upload metadata must match task/attempt/fence, checksum, size, object version, role/media and expiry; duplicates/foreign/reordered/stale outputs refuse. No existing worker branches are removed. No backfill or tenant mutation; forward-only. Physical result consumer and finalization remain separate.

### 00946–00947 — target skin physical result handoff

00946 adds API-only exact target skin canonical result catalog and ordered output/Params object pins. It requires succeeded canonical task under running sealed producer, deterministic namespace run/task/stage/Params IDs, current editor/session and exact completed target parser canonical result; source/namespace/ordinal/manifest/original lineage must match. It validates output artifact/upload ownership/attempt/fence/role/media/version/checksum and ready accepted-version Params. No source-wide latest discovery.

00947 adds scheduler-only lock_semantic_target_skin_result. It repeats full target sealed admission evidence, locks task attempts/results/output artifacts/uploads and matches exact current catalog, all four ordered object pins and bounded byte-verifier skin pixel count against the supplied complete packet. No object IO, activation or completion. No existing functions replaced or data backfill; forward-only. Completed producer consumption requires subsequent finalization support.

### 00948 — target skin completed lifecycle

Adds private semantic_target_skin_completed_output_ready for succeeded canonical task outputs under running/completed producers. Existing worker result predicate and canonical root guard remain running-only. Four consumer/sealed functions are replaced in place to permit completed replay: target skin sealed scope, sealed registration state, sealed evidence and API result catalog. OID/owner/ACL must remain unchanged.

Scheduler-only finish_semantic_target_skin repeats the dedicated full physical consumer packet and exact target run identity, requires one matching user quota and no incomplete uploads/backend documents, then atomically releases residual tenant reserved bytes, zeroes/releases quota, completes run and emits one completion event/outbox. First finish refuses expired quota; completed retry requires zero-byte released quota and emits nothing. No original parent completion or migration-time row changes; forward-only.

### 00949 — target skin saved recovery

Adds API-only semantic_target_skin_saved and scheduler-only semantic_target_skin_registered_transport. Saved reader validates original request/session/manifest/selected entry and namespace-derived skin run/task/stage identity, exact immutable config/profile/Params and supported lifecycle data. Only mask.face_skin is accepted. Transport reader reconstructs exact persisted ordered rows/chunks/direct/manifest and requires dedicated sealed or unsealed binding replay. Partial/conflicting metadata refuses; no repair, reservation, publication or queueing. No existing functions replaced or data backfill; forward-only.

### 00950 — target defect completed prerequisite barrier

Additive scheduler-only lock_semantic_target_defect_completed_dependencies(uuid,uuid,uuid,uuid,uuid,jsonb), owner photo_fn_owner, SECURITY DEFINER, fixed search_path. No existing function replacement, table privilege expansion or metadata backfill. Requires three exact ordered namespace-derived completed canonical person/landmarks/skin producers with shared original lineage; acquires share row locks. Canonical result consumer locks and CPU qualification/Params authority remain mandatory in blocked begin. Down refuses: forward-only.

### 00951 — target defect registration ownership

Adds two private target scope/registration state helpers and scheduler-only strict binding gate. Ready backend manifest/chunk ownership is checked before missing state; exact replay only, partial state refuses. Existing pending shell matcher receives additive target CPU registered branch in place, preserving OID/owner/ACL. No new table grants/backfill; forward-only Down refuses. Seal and result authority remain separate.

### 00952 — target defect admission

Добавляет приватный `lock_semantic_target_defect_graph_budget(uuid,uuid,jsonb)`
и scheduler-only `lock_semantic_target_defect_admission_evidence(uuid,uuid,jsonb)`.
Обе SECURITY DEFINER функции принадлежат photo_fn_owner, с фиксированным
search_path и без PUBLIC EXECUTE. Новых табличных прав, backfill и активации нет.
Бюджет требует единственную blocked CPU задачу, отсутствие edges/expansions,
точные пять native output roles, ready closure, действующий резерв пользователя
не меньше output+8MiB и достаточный tenant reserved_bytes.
Admission повторяет 00951 original target manifest scope и 00950 completed
producers, точный config hash, канонические person/landmarks/skin results,
политику и qualification, namespace Params object pin и exact registered binding.
Ошибочный envelope, отозванная сессия или истёкший резерв возвращают false.
Down запрещён; deployment требует штатного forward-only migration workflow.

### 00953 — target defect atomic seal

Добавляет private sealed graph scope, exact sealed registration state/binding
и full sealed evidence; единственная новая scheduler-only функция —
`seal_semantic_target_defect_preparation(uuid,uuid,jsonb)`. Новых DML-прав нет.
Для draft: повторяет 00952 admission и live quota, проверяет v2_enabled native
CPU operation, запрещает непредусмотренные consent links, атомарно выставляет
sealed/queued, активирует exact bound task и сохраняет queued event/outbox/202
receipt. Ошибка любой фазы откатывает все изменения. Ключ V2.Admit —
`target-defect-prep:<run UUID>`; ticket принадлежит исходному actor/request.
Для sealed replay: возвращает исходный ticket только при точном receipt и
повторной проверке current authority, canonical dependencies и transport;
новых событий и активации не создаёт. Private helpers не дают scheduler
отдельного обхода admission. Миграция forward-only, без backfill.

### 00954 — target defect worker execution

Сохраняет OID/owner/ACL `claim_task` и `guard_v2_result_root`, добавляя только
ветку `v27_target_defect_prepare`. Claim повторяет target sealed scope и closure
после успешного worker claim, затем атомарно переводит queued run в running.
Неуспешная проверка откатывает claim с SEMANTIC_TARGET_DEFECT_START_STALE.
Private `semantic_target_defect_result_ready` требует running/sealed target run,
exact namespace/task/Params/input digest, ready closure, attempt/fence и пять
ordered native roles: defect_labels, defect_protection, defect_labels_png,
defect_candidates, defect_propose_report. Каждый output связан с ready artifact
и completed output_upload той же задачи/attempt/fence; SHA/bytes/object version/
media совпадают. Подмена порядка, дубли, неверный SHA/fence/attempt запрещены.
Trigger использует эту проверку до canonical result insert; новые табличные
права и PUBLIC EXECUTE не выдаются. Старые source/Apply branches сохранены.

### 00955 — target defect metadata finish

Private `semantic_target_defect_completed_output_ready` требует succeeded task
и running/completed sealed run с теми же пятью verified outputs. Worker guard
00954 остаётся running-only. Sealed scope/state/evidence допускают completed
для точного durable replay, сохраняя OID/owner/ACL. Новый scheduler-only
`finish_semantic_target_defect(uuid,uuid,jsonb,uuid,jsonb)` повторяет full current
sealed authority и связывает переданные result/output refs с canonical result
и актуальным attempt/fence. Завершение запрещено при чужом result, изменённом
output set, revoked access, незавершённых uploads/backend documents, истёкшем
резерве или неверном accounting. В одной TX освобождает единственный run quota,
уменьшает tenant reserved_bytes и создаёт completed event/outbox. Replay требует
released quota с bytes=0 и не создаёт событий. Новых табличных прав/backfill нет.

### 00956 — exact target defect result catalog

Добавляет API-only SECURITY DEFINER `semantic_target_defect_result_catalog`
(tenant/project/actor/session/original parent/request SHA/exact task) и
`semantic_target_defect_result_objects` с дополнительным exact result ID.
Каталог повторяет original target manifest/DAG и completed producer scope,
текущие canonical person/landmarks/skin refs, namespace Params origin, succeeded
canonical attempt/fence и пять verified native outputs. Не использует source-wide
latest candidate. Object-функция возвращает ordered пять outputs+Params только
при совпадении текущего canonical result, с non-null object version/ready/verified
и неистёкшими pins. Внутренние scope locks завершаются в короткой DB transaction,
до object IO в Go. Новых табличных прав, backfill и queue mutations нет.

### 00957 — target defect completed consumer lock

Scheduler-only `lock_semantic_target_defect_result` принимает точный исходный
parent/request/task scope и completed evidence. SECURITY DEFINER owner
photo_fn_owner, fixed search_path, без PUBLIC EXECUTE и новых табличных прав.
Повторяет full sealed target authority, требует completed run, удерживает
canonical task/results/attempts/artifacts/output_uploads до caller commit и
сравнивает полный ordered каталог шести object pins с переданным packet.
Result ID, outputs, Params, роли, bucket/key/version/SHA/bytes/media должны
совпадать точно. Unknown/missing/substituted fields и revoked access возвращают
false. Функция не активирует задачу, не меняет quota и не публикует события.

### 00958 — target defect saved recovery

API-only `semantic_target_defect_saved` возвращает absent либо точные saved
config/profile/Params/task/run locators после current editor/session/project,
original target manifest/ordinal/namespace/request checks. Dangling admission
receipt без run не считается absent. Target defect run UUID использует
`semantic.defect.prepare:reference`; stage ID — `stage:defects.propose`.
Scheduler-only `semantic_target_defect_registered_transport` восстанавливает
исключительно текущий exact registered manifest/chunks/count/entries SHA и
повторяет strict blocked либо sealed binding state. Никакой partial repair,
PUT/registration/queue mutation не выполняется. Функции SECURITY DEFINER с
фиксированным search_path, owner photo_fn_owner, без PUBLIC и DML grants.


## 00959 — provenance target defect protection

Forward-only, без новых таблиц и DELETE privileges. `register_semantic_target_defect_protection` (scheduler only) сначала блокирует полное completed evidence00957, затем проверяет SHA report/body, точное равенство body inline report.protection, canonical protection pixels, target namespace document UUID, ready accepted-version draft upload и pinned artifact. В существующий insert-only `semantic_native_protections` записывается distinct anchor `{schema_version:1,kind:target_defect,parent_run_id,request_sha,target_namespace,target_ordinal,target_manifest_sha}`. Повтор должен полностью совпадать с сохранённой строкой. Existing purge/GC уже учитывают эту таблицу; миграция не изменяет existing functions/data.

`semantic_target_defect_protection_object` (API only) возвращает exact registered ready root metadata лишь при текущей canonical completed target authority и полном совпадении anchor/source/result/pixels/report/upload. `lock_semantic_target_defect_protection` (scheduler only) повторяет completed dependency lock, блокирует registry/upload/artifact и сравнивает exact `{ref,bucket,key,version,bytes,media}` с физически прочитанным root pin. Не использовать generic/source origin lookup для этих target roots. Upgrade00899→00959 проверяет60 миграций и сохранение OID/owner/ACL существующих функций и tenant rows. На production миграция в этом этапе не применялась.
