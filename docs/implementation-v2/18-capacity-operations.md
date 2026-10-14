# Реализуемые контракты масштабирования v2.2

Развивает [12](12-scale-product.md). Word-меморандум — подробный эксплуатационный контекст; при расхождении точные структуры и значения этого документа первичны. Tenant sharding и active-active regional sync не входят в v2; их внедрение требует новой миграционной спецификации. Масштабирование A→B в v2 не меняет authority PostgreSQL/object storage.

## Конфигурация и persistence

`CapacityPolicy` — валидируемый JSON server config с schema_version1: gpu_concurrency1, cpu_concurrency2, cpu_threads_per_task2, heavy_admission_inflight8, tenant_heavy_queued2, gpu_budget_bytes10200547328 (9.5GiB), photo_ram_budget_bytes17179869184, control_ram_budget_bytes8589934592, scratch_free_floor_fraction0.15, scratch_multiplier1.2, tile_edge512, heartbeat_seconds10, offline_seconds30, lease_seconds60, renew_seconds10, queue_weights{interactive:4,capture:3,batch:2,training:1}, unknown_cost_seconds1800, max_retries_after_oom1. Capacities для другого узла явно задаются operator config и валидируются; не обнаруживаются по общей RAM без резерва ОС. Config SHA в worker heartbeat и evidence. Схема contracts/schemas.json; числа ресурсов int64-safe, на wire decimal string для byte counters в API, JSON config допускает integers≤2^53−1.

Не вводить второй ledger: использовать photo.resource_slots.capacity(JSONB) для per-node budgets и photo.resource_reservations(ram_bytes,vram_bytes,scratch_bytes,task_id,node_id,state,expires_at) для активных task reservations. Quota output/storage reservations — существующий quota_reservations ledger. Run-reservation означает quota/output/scratch forecast; actual RAM/VRAM бронируется при lease на конкретном node. Forecast не считается второй фактической RAM reservation. V32 расширяет capacity JSON schema и scheduler functions; таблицы и legacy state enums не переопределяет. Deployment с policy SHA, не требующий schema migration, поставляется в V20; DB-aware enforcement новых полей включается в V21 foundation и проверяется повторно V32.

`ResourceBudget {host_bytes,gpu_bytes,scratch_bytes,output_bytes,cost_seconds}` immutable план. `EstimateBudget(operation, dimensions, qualified_profile)` берёт measured peak по nearest greater dimension qualified profile, масштабирует scratch/output линейно pixel count и ceil(*1.2); вне qualified dimensions →unavailable, не экстраполирует GPU memory. CPU deterministic tiles: host=tile workspace measured profile + model residency, scratch=сумма disk-backed intermediates по формуле operation graph; reserve перед allocation. Для ZIP output upper=sum(input bytes)+max(64KiB,files*512)+manifest_bytes, для NPY header budget4096/file. При root JSON cap8MiB и aggregate closure cap обе границы обязательны. Отсутствующая оценка cost допускает1800s, отсутствие memory profile НЕ допускает unlimited allocation.

## Методы scheduler

`AdmitBudget(run,forecast,key)`: existing tenant quota lock, sum planned output bytes плюс staging overhead, reserve once; unique run/key. Не резервировать все GPU tasks одновременно на узле.

`TryLease(task,node,now)`: node healthy/qualification exact, DB locks slot then existing reservation rows в existing scheduler order (это независимая TX от API lifecycle order17); compute sum active reserved resources, добавить candidate, сравнить каждого budget, scratch disk heartbeat с freshness≤30s. Если не хватает — skip task, не task failure. Lease/fence и reservation атомарны. Повтор task lease заменяет expired reservation только после fence старого attempt; старый worker capability закрыт. Scheduler TX не захватывает project/profile/stream locks, чтобы не создать обратный порядок с17.

`RenewLease(task,attempt,fence)` продлевает lease и resource reservation вместе на60s после authenticated heartbeat. `ReapExpired(now)` fences expired attempt прежде освобождения бюджета; sweeper idempotent. `ReleaseBudget(task,attempt,fence)` меняет state existing lifecycle API один раз; old attempt не освобождает новую reservation. `SettleRun(run)` output quota по canonical accepted bytes, compute cost отдельно независимо от user-billable export. Не считать сумму retry artifacts billable output.

`SelectTask`: очередь persistent tasks с class из frozen runs.config_snapshot.queue_class и created_at/id; class weights4/3/2/1. DRR deficit единицыseconds, на цикл quantum=weight*30; task charge=max(1,ceil(estimated_ms/1000)), unknown1800. Deficit cap=max(3600,max runnable task cost), держать deficit и last-tenant cursor в памяти единственного v2 scheduling leader. Leader держит session advisory lock pg_try_advisory_lock(2026,32) на выделенном PostgreSQL connection; потеря connection прекращает dispatch немедленно, каждое назначение проверяет connection/lock. Restart reset deficits0 разрешён, FIFO age остаётся DB. Никакого несуществующего JSON policy поля scheduler_tenants не добавлять. Legacy jobs сохраняют прежний scheduler; общие resource_slots/reservations предотвращают двойную выдачу ресурсов. Round robin tenants sorted UUID с in-memory last tenant cursor; cap per tenant2 queued heavy tasks. Age≥10min training→batch, batch→capture, capture→interactive; interactive не повышается. Это одно повышение относительно исходного класса, не повтор каждый цикл. Model affinity выбирает только среди уже eligible одинакового class/tenant, не обходит oldest task более30s. CPU и signer pools выбираются независимо от GPU.

`HandleOOM`: invalidate attempt outputs, release only its lease; ровно один retry если существует qualified tile256 profile с теми же model/renderer/ICC locks, иначе fail RESOURCE_EXHAUSTED. Нельзя менять precision/model/backend без отдельной qualification. Cancel checks между tiles; длительный uninterruptible model call может превысить2s, метрика cancel latency отражает это, обещание real-time preemption запрещено.

## Lifecycle узлов и масштабирование

`DrainNode(node)`: set draining через existing worker registration management, scheduler не выдаёт новые leases; дождаться active=0 либо timeout=max operation timeout+60s; затем fence оставшихся и stop. Safe autoscaler принимает только aggregate metrics из monitoring; не имеет доступа к фотографиям или signer key. Start node проверяет model/device/runtime hashes и storage gateway TLS до healthy heartbeat.

`ComputeScaleDecision(pool,window)`: up если oldest eligible wait>30s непрерывно180s ИЛИ reserved utilization>.75 непрерывно300s; delta1 per60s, maximum из operator config. down только idle900s, delta1 per300s, minimum1 для configured enabled pool. Unavailable model backlog не является eligible и не порождает бесконечный scale-up. Local RTX3060 mode min=max1, decision превращается в capacity alert. Не создавать оплачиваемый cloud node без настроенного operator provisioner и лимита; API adapter `ProvisionNode(pool,policy_sha,idempotency_key)` / `StopNode(node,drain_proof)` — deployment integration, credentials не в job params.

## Restore и upgrade runbook

`PrepareUpgrade`: закрыть feature flags writers только новых capabilities, drain соответствующих workers, backup DB+object inventory+keys, проверить baseline; migration forward-only, deploy backward-compatible readers, run role/old Apply/v2 smoke, затем flags on. Failure ⇒ flags off, fence incompatible tasks, совместимый binary; не DROP schema и не переписывать applied migration.

`RestoreCheckpoint`: DB base+WAL в isolated namespace, object versions/manifest до общего recovery timestamp; scheduler paused, external outgoing disabled. Сверить все active artifact metadata sha/size с объектами streaming, current consent epochs, tenant deletion tombstones из независимого append-only deletion backup журнала. Если deletion history новее checkpoint недоступна, запуск writers запрещён до reconciliation. Keys restore отдельным privileged step. Dangling object→quarantine, missing referenced bytes→restore fail, не fabricated success. Fence все восстановленные старые leases, reissue через scheduler, replay outbox idempotently; expired access/download intents invalidated. Запустить legacy Apply и один v2 workflow с fixtures, отдельно purge и revoke negative tests; только затем открыть writers и пользовательский access.

Evidence: backup timestamp, last WAL LSN, object manifest SHA, deletion journal watermark, missing/extra counts, key version IDs без secrets, restore start/end, probes outcomes, operator approval event. RPO≤15min/RTO≤4h — критерии реального drill, не выводятся из наличия runbook. Quarterly drill + после storage/DB major change. Алерты и метрики12; метрика evidence commit/lock hashes должна совпадать с deployed version.

## Приёмка

Два узла одновременно lease один task; node dies после stage до adopt; old fence renew; reservation expiry; GPU OOM retry; disk crosses15%; model unavailable backlog; noisy tenant fairness; review не держит GPU lease; signer не читает чужой project; restart scheduler и deficits; restore с tombstone новееbackup. Для каждого test требуются DB assertions и реальные worker/gateway traces. Documentation semantic checks не подменяют этот integration suite.


### Implemented restore object verification stage

`workers/photo_worker/runtime/restore_inventory.py` defines frozen `RestoreObject` and `RestoreInventoryEvidence`, and `verify_restore_inventory(references, open_version, check_active, maximum_objects, maximum_bytes)`. Catalogue order is UTF-8 tuple(bucket,key,version), strictly increasing. The adapter must open that exact immutable version with no latest-version fallback. Reads are bounded to1MiB; size, SHA and cancellation are checked before evidence returns. Digest is SHA256 of UTF-8 domain `photoeditor.restore-object-inventory/v1` plus LF, followed by canonical JSON rows `{bucket,key,version,sha256,size}` plus LF.

This is only the referenced-byte verification stage. SQL enumeration completeness, extra-object quarantine, deletion history, keys, leases/outbox and opening writers require separate orchestration. A successful empty catalogue is not proof that a database is empty. Tests authored NOT RUN.

`MinioRestoreObjects` in runtime/restore_objects.py supplies the object adapter: explicit installed bucket allowlist, exact version_id GET and matching response version/status/size, no decoding transformations, bounded reads, guaranteed response close/release. Operator installs read-only credentials, TLS endpoint and bounded HTTP timeouts. It never uses artifact request URLs or repairs/deletes objects. Adapter tests authored NOT RUN.

`verify_restore_database_objects` in runtime/restore_catalogue.py consumes every ready artifact under a dedicated idle operator connection, REPEATABLE READ READ ONLY and row_security=off. Operator must hold superuser/BYPASSRLS; no application grants are introduced. Server cursor batches512 rows ordered by C-collated bucket/key/version/SHA/size. Expired ready artifacts are retained; consistent shared physical references collapse, conflicting metadata refuses. Evidence records database/snapshot/transaction time and verified inventory. It does not establish isolated namespace, reconcile deletion history, inspect extras or unlock writers. Unit scenarios authored NOT RUN.

`plan_restore_quarantine` in runtime/restore_quarantine.py merges complete UTF-8 ordered expected and physical version inventories with constant working memory. Physical inventory includes historical non-delete-marker versions. Referenced versions must exist with exact size; content SHA is established by the separate streaming verifier. Extras are emitted to a staged operator sink. Only a complete return yields `RestoreQuarantineEvidence`; exceptions leave the plan incomplete and must not activate staged rows. Digest domain is `photoeditor.restore-quarantine-plan/v1` plus LF, followed by canonical `{bucket,key,version,size}` rows plus LF. No delete/quarantine mutation is performed by this planner. Listing/sorting, durable activation and full restore orchestration remain open; tests NOT RUN.

`sorted_restore_versions` in runtime/restore_physical_catalogue.py lists installed MinIO buckets recursively with include_version=True, excludes delete markers, validates immutable versions and sorts on disk via temporary SQLite BLOB keys. Entry and spool caps plus2MiB cache bound resources. Duplicate versions refuse. Context exit closes generators/cursors/database and removes spool. Object writers must remain externally paused: listing is not an atomic storage snapshot. Tests authored NOT RUN; durable quarantine and restore orchestration remain open.

`verify_restore_database_storage` uses one SQL snapshot for byte verification and a second catalogue traversal for physical-version comparison; matched count must equal verified count. `verify_minio_restore_storage` in runtime/restore_storage.py composes actual MinIO listing/sorting and exact-version readers with this SQL stage. Complete `RestoreStorageEvidence` returns only after both stages; staged extras remain inactive. Operator-controlled paused writers/isolation, durable sink activation and deletion reconciliation remain separate requirements. Tests NOT RUN.

`RestoreQuarantineJournal` stores extras.jsonl in exclusive `<run UUID>.incomplete` directory. Seal requires exact staged digest/count/bytes and combined storage evidence, rechecks the root directory's device/inode, fsyncs data/marker/directories and atomically publishes `<run UUID>` through one pinned root FD with platform `RENAME_NOREPLACE`/`RENAME_EXCL`; an existing final directory, including an empty one created concurrently, is never replaced. Unsupported platforms fail closed. The pinned FD also receives the final directory fsync; a changed configured root path refuses the seal. Only final directory plus complete.json denotes a complete report; incomplete directories remain inactive after failure. `verify_and_journal_restore_storage` connects the sink to real SQL/MinIO stages. This journal is a durable quarantine plan, not storage mutation, deletion reconciliation or approval to open writers. Tests authored NOT RUN.

The shared `runtime/atomic_directory.py` publisher accepts only local basename operands and a real pending directory; traversal, separators, empty names, symlink/file sources and invalid directory descriptors fail before rename. It compares the published final directory inode with the pending inode before returning success. Both the quarantine journal and deletion-history backup use this primitive.

`verify_restore_deletions` in runtime/restore_deletions.py requires trusted independent latest `DeletionHistoryCheckpoint(sequence,head_sha256)` and the full event chain from genesis (previous SHA starts64 zeroes). Closed events contain schema_version=tenant-deletion-history/v1, sequence, previous_sha256, tenant_id, phase=requested|completed, occurred_at. Head is SHA256(canonical event bytes). Each requested/completed tenant must be absent/deleting/deleted in restored state; active/suspended refuses. Missing history, gaps or wrong head refuse. Checkpoint provenance must be external to restored DB/file; event producer/backup integration and SQL status adapter remain open. No purge or writer-unlock occurs. Tests NOT RUN.

`verify_restore_database_deletions` in runtime/restore_deletion_catalogue.py validates explicit expected database, idle operator connection, repeatable-read read-only snapshot and RLS bypass, then reads each exact tenant ID using parameters. Missing tenant is safe; invalid/null stored status refuses. Complete independent history evidence is bound to database/snapshot/time. Database-name matching does not attest cluster isolation. Resurrected tenants require separate fenced remediation; journal production/backup authority remain open. Tests NOT RUN.

Maintenance enables independent requested-event history through V32_DELETION_HISTORY_PYTHON and V32_DELETION_HISTORY_ROOT; set V32_DELETION_HISTORY_REQUIRED=true to reject unconfigured deployment. Python must contain installed worker wheel, run with -I, and use an independent private journal directory excluded from restored DB namespace. Enabled startup checks01586 narrow SQL function and always records durable requested history before object purge. Partial config refuses; unconfigured legacy mode remains available and does not qualify V32 restore safety. External replication/backup and deployment acceptance remain required; tests NOT RUN,01586 NOT APPLIED.

The maintenance appender opens the private, trusted-owner journal root with `O_DIRECTORY|O_NOFOLLOW`, verifies its identity, and passes the descriptor to its isolated Python CLI. The CLI uses `fchdir` and relative SQLite paths, so renaming or substituting the former root path cannot redirect the append to a different directory. The appender also rechecks the configured path and rejects a changed root after the CLI exits. This protects the append path against directory replacement; independent backup/replication, qualified Python installation, parent-directory administration and restore acceptance remain separate requirements. Descriptor-path tests authored NOT RUN.

`export_deletion_history_backup` produces a consistent SQLite backup verified against its copied complete event chain. Private history.sqlite and canonical deletion-history-backup/v1 manifest include backup UUID, database SHA/bytes and sequence/head. Before publication it requires exactly those two files, rechecks the destination root device/inode, fsyncs and atomically publishes as UUID directory through a pinned root FD with exclusive no-replace rename, including when an empty final directory appears concurrently. Ordinary failed exports remove their partial directory; a root replacement fails closed and can leave an inactive partial directory under the moved root for operator inspection. Returned manifest SHA/checkpoint must be durably published by an independent backup authority; local export alone proves neither remote replication nor latest recovery watermark. Tests NOT RUN.

Backup restore requires a private UUID directory owned by root or the effective operator UID containing exactly `manifest.json` and `history.sqlite`, and reads both only from regular, single-link, private files with the same ownership rule. It verifies exact file identities, SHA and the directory file set before and after use, then compares the closed history chain with an independently supplied latest checkpoint. Group-readable copies/directories or added files are not accepted as qualified private backups. Tests authored NOT RUN.


### Isolated preflight and live independent history authority

`verify_isolated_restore` in `runtime/restore_preflight.py` verifies pinned deletion backup before storage, repeats installed isolation/paused-writer guard around stages and compares independent latest deletion checkpoint again before sealing the quarantine plan. Both SQL evidence records must match expected database. `verify_restore_with_live_history` supplies a concrete `DeletionHistoryStore.latest_verified_checkpoint` reader, validating the entire live chain on each authority read with bounded entry count and cancellation. Missing-prefix/corrupt live history refuses even if its tail appears valid. Operator must install the current production independent journal outside restored namespaces; a stale copied store cannot establish freshness. Preflight does not open writers or establish the remaining lease/consent/keys/outbox/probe/approval stages. Unit tests authored NOT RUN.


### Restored compute leases

`runtime/restore_leases.py::fence_restored_compute_leases` is an explicit privileged recovery step after isolated restore verification and before scheduler restart. A dedicated idle superuser connection must target expected_database. Installed guard must independently assert isolated namespace, paused scheduler/workers and outgoing IO disabled throughout. First transaction expires running task leases and queued/running reserved grants; existing `photo.reap_tasks(1)` then performs canonical fence increments, attempt closure and resource release, one transaction per task. Final queued/running count must be zero. Statement timeout30s, lock timeout5s, task bound10m. Failure after partial commits must keep writers paused and retry; it does not rollback already-invalidated grants or grant restore completion. Retry planning/reissue belongs to scheduler only after remaining restore gates. Other lease families require separate recovery. Unit scenarios NOT RUN.


### Recovery stage composition and expired download intents

`runtime/restore_recovery.py::recover_verified_restore` composes live-history preflight, compute fencing and expired download-intent invalidation, then repeats the independent latest deletion-head verification. Returns `RestoreComputeRecoveryEvidence` containing preflight, leases, intents and final checkpoint only after all stages. Failure may leave sealed storage report or committed fencing; these never grant writer access. `runtime/restore_intents.py::invalidate_expired_restore_intents` uses dedicated idle superuser connection with explicit database scope, statement30s/lock5s bounds and database transaction_timestamp cutoff. Only unconsumed download intents with expires_at<=cutoff are marked consumed; metadata and unexpired intents are preserved. Final same-transaction expired/unconsumed count must be zero. This does not reconcile other access families or revoked authority newer than backup. Consent, keys, quarantine activation, remaining leases, outbox, probes and operator writer approval remain required. Tests authored NOT RUN.


### Restored publisher claims

`runtime/restore_outbox.py::prepare_restore_outbox` runs within `recover_verified_restore` after compute fencing and expired intents, before final latest deletion-history verification. Dedicated idle superuser connection must match expected database; installed guard keeps scheduler/workers/publishers and outgoing IO paused. Atomic transaction (statement30s, lock5s) clears publish_lease_until on both photo.outbox and photo.auth_mail_outbox, including old sent rows, and verifies zero remaining claims. Evidence records invalidated claims and pending event counts. Event identity, payload, attempt and sent_at are preserved. Exact old-lease acknowledgement cannot match the cleared claim. No broker publishing occurs; existing publisher resumes only after full restore approval. Already-sent records are not blindly reset: broker-loss delivery reconciliation still needs an independent delivery checkpoint and consumer deduplication evidence. This stage is replay preparation, not completed replay or writer approval. Tests authored NOT RUN.


### Restored mail leases

`runtime/restore_mail.py::recover_restored_mail` is composed before outbox preparation. Operator must keep mail workers, publishers and SMTP outgoing paused. Dedicated idle superuser connection checks exact database; statement30s/lock5s and payload cap10m bound work. First transaction expires sending leases; canonical `photo.reconcile_mail(100)` executes in separate transactions until zero, incrementing fences and preserving existing retries, stable message IDs and expiry/cipher cleanup. Final sending/expired-nonterminal count must be zero. Evidence includes expired sending leases and reconciled payload count; no SMTP delivery occurs. Partial failure requires retry with outgoing paused. Terminal records remain terminal; duplicate SMTP delivery ambiguity across backup timestamp is not solved by this stage and requires provider-side reconciliation. Unit tests authored NOT RUN.


### Restored cleanup grants

`runtime/restore_cleanup.py::fence_restored_cleanup` is composed after compute fencing. Explicit allowlist: output_uploads, backend_documents, ingest_originals, deletion_jobs. Dedicated idle superuser connection must match expected database; operator guard must keep all cleanup workers and storage mutations paused. Atomic bounded transaction sets nonnull cleanup_lease_until to negative infinity and verifies all four tables. Existing state, token, cleanup schedule, quota and purge progress remain unchanged; subsequent canonical claims replace tokens. No storage deletion or purge occurs and no cleanup is declared completed. Other cleanup queue schemas remain separate required recovery work. Tests authored NOT RUN.


Cleanup fencing allowlist extended: staging_cleanup.lease_until, upload_abort_cleanup.lease_until and artifacts.gc_lease_until, in addition to the four cleanup_lease_until families above. All seven update/verification pairs are atomic. Keep nonnull token/lease pairs and terminal replay tokens unchanged; canonical claims replace tokens later. Consent cleanup has no lease column and requires consent-state reconciliation instead of fabricated lease fencing. Exact-column and late-family rollback tests authored NOT RUN.


### Restored consent catalogue

`runtime/restore_consent.py::verify_restore_consents` requires a complete independently current catalogue and trusted `RestoreConsentCheckpoint(grants,sha256)`. Canonical rows contain tenant_id/project_id/id/purpose/person_id/status/epoch/revision/policy_version, sorted tenant UUID/id UUID. SHA domain is photoeditor.restore-consent-catalogue/v1 plus LF, followed by canonical rows plus LF. Exact equality is required; old epochs, revoked-to-granted, missing/extra rows refuse and require canonical consent reconciliation. `verify_restore_database_consents` streams all restored grants with named cursor(batch512), exact DB scope and privileged RLS-bypass repeatable-read read-only snapshot. Current authority must be supplied externally, never copied from restored DB. Independent provisioning/remediation and mandatory recovery integration remain open. Tests authored NOT RUN.


Consent gate is now mandatory in recover_verified_restore. Operator supplies open_current_consents context yielding independently current checkpoint and full ordered catalogue, plus read_latest_consent_checkpoint independent live reader. Both are required before preflight. SQL consent verification and authority context closure must succeed before compute/cleanup/intents/mail/outbox mutations. Final consent checkpoint must equal initial checkpoint after deletion-head recheck. Result includes RestoreConsentEvidence; absent/changed authority refuses. Provisioning current independent source and canonical remediation remain open. Tests authored NOT RUN.


`runtime/restore_consent_authority.py::SQLCurrentConsentAuthority` supplies concrete current SQL consent authority. Operator installs independently current connection, exact source DB and assert_current_authority guard checking identity/freshness (including replica lag). Snapshot first hashes complete validated catalogue and then streams a second traversal under same read-only repeatable-read transaction; full consumption and digest equality are mandatory. latest_checkpoint opens fresh snapshot. recover_with_sql_consent_authority wires both methods and refuses same connection as restored database or competing callbacks. Two different connections alone do not prove separate/current namespaces; installed guard remains required. Independent authority deployment and reconciliation remain open. Tests authored NOT RUN.


### Scheduler materialization grants

`runtime/restore_scheduler_grants.py::fence_restored_scheduler_grants` expires upload_sessions.completion_lease_until and graph_expansions.expansion_lease_until atomically under dedicated idle superuser/exact DB authority and installed paused isolation guard. Token/lease pairs, workflow state, plans and retry schedules remain intact; no completion or expansion is executed. Canonical claims later replace tokens. Statement30s/lock5s bounds; final no-unexpired-claim checks required. Composed after compute fencing and before cleanup. Unit refusal/atomicity scenarios authored NOT RUN.


### Referenced encryption keys

`runtime/restore_keys.py::verify_restored_key_versions` is mandatory before recovery fencing. Privileged repeatable-read read-only snapshot with exact DB scope enumerates distinct mail key_id and capture key_version for nonempty encrypted payloads; bounded4096 versions, cursor128, canonical C order. Installed verify_key_version(domain,id) must perform actual cryptographic decrypt/known-answer verification using restored material, not ID existence, and return literal True. Evidence includes only domain/key version identifiers. Concrete crypto probe provisioning and signing/KMS domains remain separate required work; no private material is emitted. Unit scenarios authored NOT RUN.


Concrete `runtime/restore_key_probe.py::AESGCMRestoreKeyProbe` implements verify_key_version for mail/capture AES-256-GCM. Operator installs bounded owned keys and RestoreKeyWitness(sealed nonce12+ciphertext+tag, aad, plaintext_sha256). Witness must be independently pinned from historical correct material, never created with the candidate restored key. Probe authenticates/decrypts and checks plaintext SHA, returns literal True only after cancellation recheck; errors/repr expose no secret bytes. Install worker restore optional dependency. Secure provisioning/bootstrap and signing/KMS remain required. Real-AEAD unit scenarios authored NOT RUN.


### Private crypto installation

`runtime/restore_key_installation.py::load_restore_key_probe` loads normalized absolute current-user regular single-link files mode0600 or stricter with O_NOFOLLOW/O_NONBLOCK, bounded reads and final inode/metadata checks. Keys file closed canonical schema: schema_version=restore-aes-keys/v1, entries(domain,key_id,key_b64). Witness file: schema_version=restore-aes-witnesses/v1, entries(domain,key_id,sealed_b64,aad_b64,plaintext_sha256). Sorted unique scopes must match, max4096 entries; strict canonical base64, keys<=1MiB/witnesses<=8MiB. Independent witness SHA comes from trusted operator authority. recover_with_installed_crypto composes loading and SQL consent recovery, refuses competing verify_key_version. Never put key bytes in arguments, logs or evidence. Historical provisioning, actual operator deployment and full recovery gates remain required. Tests authored NOT RUN.


### Exclusive operator recovery session

`runtime/restore_lock.py::exclusive_restore` protects installed-crypto recovery with a fixed database-scoped two-int session advisory lock(namespace1346916180,operation32). Requires dedicated direct/session-bound idle superuser connection and exact DB; transaction pooling is unsupported. Checks pg_locks to refuse reentrant invocation, pg_try_advisory_lock refuses concurrent run. Lock survives per-stage transactions. Final database/backend PID and successful unlock required before evidence returns; cleanup/session failure closes connection and refuses success. Isolation/pause guard remains independently required: advisory lock only coordinates participating restore operators. Tests authored NOT RUN.


### Durable recovery stage report

`runtime/restore_report.py::persist_recovery_report` stores restore-recovery-report/v1 with writers_authorized=false, build SHA256, operator-provided backup_at/last_wal_lsn, process start/end and typed stage evidence. Private current-user report root, exclusive0600 pending file, file fsync, atomic no-replace hardlink publication and directory fsync. Receipt contains path/SHA only. Existing final run UUID refuses overwrite; cancellation before publication cleans pending. Failure after publication may leave report but never writer approval. recover_and_report composes installed recovery and report. Backup/WAL/build metadata must come from actual operator recovery/deployed version; report does not independently attest these declarations. Quarantine activation/replay/probes/operator approval remain open. Tests authored NOT RUN.


### Final recovery fencing check

`runtime/restore_fence_check.py::verify_recovery_fences` is mandatory after mutation stages and before final independent deletion/consent checkpoint reads. One exact-DB privileged repeatable-read read-only snapshot verifies absence of queued/running compute, sending mail, publisher leases, expired unconsumed intents and nonexpired recovery grants across seven cleanup plus two scheduler tables (14 predicates total). Revived grant or invalid receipt refuses common evidence. Operator pause/isolation must remain enforced; this check covers implemented grant families, not every product authority or writer approval. Family-specific negative tests authored NOT RUN.


### Remaining resource grants

`runtime/restore_resources.py::recover_restored_resources` follows compute fencing. Refuses queued/running tasks, releases each remaining reserved grant under canonical lock_task/exact reservation fence via release_task_resources, preserving GPU quota accounting. One reservation per transaction, bounded10m. Clears stale resource slot tenant/task/fence/lease bindings after release while retaining capacity/node metadata. active_gpu_tasks must equal zero; inconsistency fails without force-reset. Typed recovery report includes released reservation/cleared slot counts. Final invariant gate now checks17 predicates, adding reserved resources, occupied slots and GPU accounting. Tests authored NOT RUN.


Exclusive restore now yields an assert_owned guard. Installed-crypto path uses it for every recovery stage, composing original isolation/pause/cancellation checks with exact DB/backend PID/advisory-lock ownership. Idle checks use short transactions; INTRANS checks are read-only in the existing transaction without changing timeout or creating savepoints. Guard runs before/after body as well. Lost ownership refuses further progress/evidence; cleanup must still release lock or close uncertain session. Unit mid-body loss and transaction-preservation cases authored NOT RUN.


### Concrete isolated SQL target

`runtime/restore_isolation.py::SQLRestoreIsolationGuard` checks expected DB/system_identifier/data_directory, pg_is_in_recovery=false, datallowconn=false, no other client backends after pg_stat_clear_snapshot and superuser authority. Operator must pre-open dedicated connection, disable new connections only on isolated target and stop other clients. Guard never terminates sessions or alters database policy. Physical backup clones may share system_identifier; independent external adapter must attest target endpoint/namespace, storage isolation and paused outgoing services before/after SQL. recover_isolated_and_report installs this guard alongside concrete crypto, SQL consent authority, exclusive recovery lock and durable report. No writer authorization. Unit identity/isolation negative cases authored NOT RUN.


### SQL quarantine activation

Forward-only01587 creates private operator-only restore_quarantine_runs/versions and SECURITY DEFINER artifact trigger preventing ready publication of any exact quarantined bucket/key/version. No application grants, deletion or move. `runtime/restore_quarantine_activate.py::activate_restore_quarantine` requires sealed preflight journal, exact owned marker/storage evidence, private single-link extras file, complete canonical sorted rows, exact count/bytes/domain digest, no current ready reference and unchanged files. One SQL transaction activates full plan or rolls back. Same run replay must match declared plan/SQL receipt. Composed before final recovery checks; report includes activation. This establishes logical application admission quarantine; direct operator S3 access still requires external storage isolation. Migration NOT APPLIED, tests NOT RUN. Full replay/probes/operator approval remain required.


Quarantine activation now verifies exact stored bytes for every run/bucket/key/version after insert/replay, in addition to whole-plan digest and aggregate receipt. Final recovery invariants now include absence of ready artifacts matching quarantine (18 predicates). SQL integration scenarios cover ACL/definer ownership, ready insert/update and cross-tenant physical-version denial, different-version success and retained deleting metadata. Tests NOT RUN,01587 NOT APPLIED.


### Bounded isolated task replay

`backend/internal/queue/restore_replay.go::Outbox.ReplayRestoredTasks` drains pending task events through existing claim/mandatory broker confirm/mark path, retaining event IDs. No mail replay or reset of sent records. Installed guard must attest isolated DB/broker, paused consumers/outgoing and exclusive ownership before/after each batch; this is a later recovery phase with qualified publisher connection, not permission to start production consumers. Max100 events/batch,10m total,15min deadline. Queue growth/unknown publish/ack/stall/invalid count refuses evidence. Forward-only01588 grants only photo_publisher exact-DB pending counter, no table/payload SELECT. Actual process/bootstrap, broker qualification and already-sent external delivery reconciliation remain required; successful drain alone does not prove consumer deduplication or writer approval. Tests NOT RUN,01588 NOT APPLIED.


Replay integration scenario: backend/internal/queue/restore_replay_integration_test.go requires PHOTO_TEST_RESTORE_REPLAY=1, PHOTO_TEST_DATABASE_URL and PHOTO_TEST_AMQP_URL pointing at dedicated initially empty recovery DB/vhost. Publishes205 probe events via publisher role/real confirms; verifies unknown-first-outcome retention, same-ID retry, complete receipt persistence and no repeat after second drain. Unique exclusive probe queue/scoped cleanup; no shared pending outbox allowed. Authored NOT RUN; does not substitute production consumer deduplication, external broker-history reconciliation or full restore drill.


### Replay process installation

`queue.RunRestoreReplay` owns verified-TLS exact DB/publisher pool(MaxConns2), amqps exact nondefault vhost, actual confirming publisher and exclusive database advisory lock(namespace1346916180,operation33). Installed AssertIsolation must attest current recovery phase, paused consumers/outgoing and qualified target before IO and each batch. PID/lock ownership repeated; unlock failure refuses success/closes session. No reconnect, topology creation, mail or ordinary publisher fallback. PHOTO_ROLE=restore-replay dispatches this runner only with trusted process apiProcessDependencies.RestoreReplay installation; default missing authority fails before IO. Config flags alone cannot install authority. Operator bootstrap/deployment and durable replay receipt/full drill remain required. Unit tests NOT RUN.


### Durable replay receipt

PersistRestoreReplayReceipt stores canonical restore-task-replay-receipt/v1, binding run UUID, externally authoritative recovery SHA/build SHA, exact initial/confirmed counts and completion time; writers_authorized=false. Normalized current-user nonshared-writable root,0600 exclusive pending, file fsync, no-replace publication, directory fsync and SHA receipt. Partial/cancelled evidence refuses. RunRestoreReplayAndReport validates receipt installation before network/dispatch; restore-replay role requires both authority and receipt installation. Initial guard is now within15min deadline. Supplied recovery SHA must be tied to actual trusted recovery report; pin alone is not provenance. Final probes/approval remain required. Tests NOT RUN.


### Replay recovery report binding — 2026-10-11

V32: replay now requires private bounded canonical recovery report with exact external SHA, run/build/database binding, required stage schemas and quarantine run identity. Reads before network dispatch and again before receipt persistence; no writer authorization. Eleven actual-file binding/tamper scenarios authored NOT RUN. This validates report binding, not full nested stage semantics or external authority deployment; final probes/cutover remain open.
RestoreReplayReceiptInstallation.RecoveryReportPath must be an absolute normalized private current-user single-link regular file, at most8MiB, without symlink path components. RecoverySHA256 is independently supplied; unchanged pin is required on both reads. Failure after broker publication keeps writers paused and yields no completed replay receipt.


### Recovery/replay chronology and count binding

V32: recovery-report reader now requires exact top-level members, valid nonzero timezone timestamps backup<=start<=finish<=now and uppercase WAL LSN. Before publishing a successful replay receipt it binds initial/confirmed counts to recovery outbox.pending_task_events and requires replay completion after recovery finish. Count mismatch refuses completion after publishing; independent event-identity/broker-history reconciliation is still required. Added eight negative temporal/count/metadata scenarios (19 total), authored NOT RUN. Failed or interrupted replay may require a fresh recovery report; no automatic reset or writer unlock.


### Pre-dispatch recovery count gate

V32: RunRestoreReplayAndReport now extracts the expected pending task count from the fully pinned recovery report and passes it through the concrete replay process to the initial SQL pending-counter check. Exact count equality is required before first Dispatch; smaller/larger queue, negative count or installed-budget overflow refuse with no publication. Standalone diagnostic replay API remains explicitly unbound; it cannot produce a bound recovery receipt. Added six unit scenarios for matching/empty/mismatch/invalid budget, authored NOT RUN. Partial replay retry requires independently reconciled fresh recovery evidence; event identity and already-sent broker history remain open.


### Pending task envelope inventory

V32: recovery outbox evidence now carries pending_task_sha256, a domain-separated streaming SHA over UUID-sorted canonical envelopes (row/tenant/event IDs, destination, routing key and full payload). Dedicated named SQL cursor128 reads the entire pending catalogue inside preparation transaction; exact count, strict UUID/order, <=1MiB/envelope,10m rows and continuous isolation/cancellation checks required. Updated report/preparation fixtures and authored canonical/empty/same-count-substitution plus eight refusal unit scenarios NOT RUN. Go replay-side digest comparison and SQL narrow catalogue authority remain to implement; identity reconciliation is not yet claimed complete. Existing reports without this field must be regenerated once the consuming gate becomes mandatory.
Hash domain: literal `photoeditor.restore-task-outbox/v1` followed by LF; each canonical envelope followed by LF. Keys: id,tenant_id,event_id,destination,routing_key,payload. UUIDs canonical lowercase text; order by SQL UUID id agrees with canonical UUID text ordering. Attempts/claims/sent timestamps excluded because replay mutates them. This inventory describes pending events only, not already-sent broker retention.


### Go replay envelope inventory gate

V32: mandatory report pending_task_sha256 now feeds Go pre-dispatch inventory verification. Forward-only01589 publisher-only SECDEF exact-DB keyset page (<=128) avoids direct table grants. Go repeatable-read read-only traversal uses identical domain/canonical envelope/order to Python, validates count/digest/UUID/payload bounds and continuous isolation before first dispatch; same-count substitutions refuse. Added canonical/substitution/duplicate and eight invalid-envelope unit scenarios authored NOT RUN; migration NOT APPLIED. Actual SQL ACL/paging/native cross-language acceptance and per-dispatched-envelope mutation/delivery-history reconciliation remain open. Installed paused-writer authority is still required across snapshot-to-dispatch; this change does not unlock writers or establish production completion.


### SQL inventory bounds and integration coverage

V32: unapplied01589 now rejects nil row/tenant/event IDs, nonobject payload, payload SQL text>1MiB, destination empty/>4096 bytes and routing>4096 bytes before returning a page to Go; malformed rows are not silently filtered. Added actual SQL integration scenarios for publisher-only function/no direct SELECT, SECURITY DEFINER owner/search_path, five scope/limit refusals,205-event keyset traversal/sent exclusion/same-count payload substitution, and four malformed-envelope refusals. Authored NOT RUN;01589 NOT APPLIED. SQL text bound is conservative (jsonb formatting may be larger than canonical JSON); canonical full-envelope1MiB gate remains separate. Full cross-language/native/drill and already-sent broker reconciliation still open.


### Bound inventory SQL/broker acceptance scenario

V32: expanded opt-in real PostgreSQL/RabbitMQ205-event replay scenario to derive privileged catalogue digest, verify actual publisher-only paginated inventory, mutate one payload with unchanged count, require refusal before any claim/broker message, restore exact payload and complete confirmed bound replay. Inventory structural validation now refuses nil DB/missing database/bad SHA before guard/IO; unit scenario authored. Tests NOT RUN,01589 NOT APPLIED. This uses independent SQL reading but the same Go canonical accumulator; actual Python/Go golden-vector qualification and production consumer/external-delivery reconciliation remain open.


### Shared canonical inventory vectors

V32: authored shared tests/fixtures/restore-task-outbox-v1.json with independently written canonical envelope lines and matching Python/Go byte+inventory digest tests. Covers unordered JSON fields, nested objects, composed/decomposed Unicode, supplementary-plane character, negative zero, exponent1e-7, integral float and maximum interoperable integer. Expected byte lines are fixture data rather than another runtime canonicalizer. Tests NOT RUN; this supplies future cross-language acceptance coverage but claims no PASS. Full deployed restore/delivery-history/consumer/probe/operator cutover remains open.


### Acceptance replay-file prerequisite

V32: added concrete verify_restore_replay_files Python reader for actual private pinned recovery report and Go replay receipt. Closed canonical schemas, independent SHA pins, complete typed recovery evidence equality, run/database/build/count bindings, timezone-aware backup/start/finish/replay ordering and WAL format; re-reads both pinned files before returning VerifiedRestoreReplay. Never authorizes writers. Twelve actual-file valid/refusal unit scenarios authored NOT RUN. This is an acceptance-coordinator prerequisite; concrete legacy Apply/v2/purge/revoke probes, current independent authority rechecks, durable acceptance report and operator cutover still open.


### Required acceptance probe coordinator

V32: added run_restore_acceptance mandatory four-probe coordinator (legacy_apply,v2_workflow,purge_negative,revoke_negative), actual pinned recovery/replay file verification before/after, continuous operator probe-isolation/current independent authority checks, exact fixture/run/database/build binding, explicit complete named checks and invocation-bound aware timestamps. Failure stops remaining probes; success remains writers_authorized=false. Thirteen orchestration/refusal unit modes authored NOT RUN with file reader mocked; actual-file reader separately covered. Concrete fixture executors, independently verified probe evidence files, actual deletion/consent/broker/key authority deployment, durable acceptance report and operator cutover remain open; coordinator declarations are not production drill proof.
Required checks in fixed order:
- legacy_apply: preview_ready, apply_committed, download_verified.
- v2_workflow: workflow_completed, review_completed, download_verified.
- purge_negative: objects_absent, metadata_absent, access_denied.
- revoke_negative: consent_current, processing_denied, access_denied.

Each installed runner receives exact VerifiedRestoreReplay and independent fixture SHA plus continuous guard. RestoreProbeEvidence binds run/database/build/fixture, invocation-local start/end, immutable complete check tuple and evidence SHA. Concrete runner must execute only isolated test fixtures and verify the actual evidence artifact before returning; a synthetic hash/boolean return cannot qualify deployment. Current-authority verifier must reconcile latest independent deletion/consent, referenced keys and broker-delivery history for the recovery target, and raise on mismatch. This callback is a required operator port, not implemented authority attestation. Probe phase may permit fixture-scoped services while all production/user/outgoing paths remain fenced. No scheduler resume, permission changes or writer unlock are performed.


### Actual probe report gate

V32: acceptance now requires RestoreProbeReceipt pointing to an actual private bounded canonical restore-probe-report/v1 file. Concrete reader verifies SHA, closed schema, exact run/database/build plus recovery/replay pins, strict boolean check pairs and aware times, then re-reads file. Coordinator revalidates all four report files after final probe and recovery/replay checks; synthetic in-memory RestoreProbeEvidence is no longer accepted from runner. Expanded orchestration matrix to19 modes with actual private probe files (recovery/replay loader still mocked), including stale/tampered/noncanonical/cross-replay/extra-field/integer-check refusals, NOT RUN. Concrete API/native fixture executors and independently measured observation evidence still required; files declaring checks alone do not qualify production. Durable acceptance report/operator cutover remain open.
Installed run_probe must return RestoreProbeReceipt(path,sha256), not RestoreProbeEvidence. Artifact: private current-user regular single-link nonsymlink normalized absolute path, <=64KiB. Closed canonical fields: schema_version=restore-probe-report/v1,kind,run_id,database,build_sha256,fixture_sha256,recovery_sha256,replay_sha256,started_at,finished_at,checks. Checks are ordered arrays [name,true], exact per-kind list from coordinator. All source report artifacts must remain available and immutable through final operator acceptance. Reader returns typed evidence only after both file reads match. Runner must derive checks from actual isolated application/storage observations; serializing success declarations is insufficient deployment evidence.


### Durable acceptance report

V32: added persist_restore_acceptance durable private canonical restore-acceptance-report/v1, exclusively published UUID.acceptance.json with file/directory fsync and no replacement. Revalidates actual recovery/replay and all four preserved probe receipts, exact named checks/order/times plus current isolation/independent authorities before writing and immediately before publication; never writer authorization. Acceptance now retains immutable source receipt links; run_restore_acceptance_and_report composes coordinator/publication. Seven actual-file report success/tamper/partial/authority/time/approval scenarios authored NOT RUN. Concrete application/native fixture executors, deployed independent authorities, actual drill and operator cutover remain open.
Private operator report includes source probe file paths/SHA and full bound replay/probe evidence; it must be retained with source artifacts. Max256KiB, normalized current-user nonshared-writable directory,0600 exclusive pending, atomic no-replace hardlink final. Failure after publication can leave a valid report, never an authorization to resume production. Operator approval must separately bind this report SHA and current deployed target/authorities.

The recovery and acceptance report publishers now hold the verified output directory by file descriptor through exclusive hard-link publication, unlink and directory fsync; replacing the configured directory path during publication refuses completion. `verify_restore_acceptance_report` is the read-side prerequisite for operator approval: it re-reads a pinned private report, compares its canonical closed document with the expected typed acceptance evidence, rechecks the pinned recovery/replay pair and all four original probe receipts under current authority/isolation guards, then re-reads the report. This reader does not issue approval or unlock writers. Unit cases authored, NOT RUN.

The four probe report publisher uses the same pinned-directory publication and refuses a changed configured root before returning its receipt. This protects the evidence publication path but does not itself qualify the fixture observations or authorize a cutover. Directory-replacement unit case authored, NOT RUN.

The positive one-frame V2 batch baseline now enforces its installed overall deadline across preflight, admission, polling and physical output verification; a render that only finishes after the deadline cannot become successful fixture evidence. A late-physical-output unit case is authored, NOT RUN. This is a building block for the required `v2_workflow` probe, not its review/download completion.

The `purge_negative` runner now applies its deadline to baseline, DELETE, job wait, post-purge API/S3/SQL observations and report publication; the job wait receives only the remaining budget. A late post-purge observation refuses a success receipt even if the destructive job already completed. Reconcile that fixture before any retry. Unit case authored, NOT RUN.

The physical V2 batch evidence now retains its canonical parsed `ImageResult`. `observe_fixture_render_review` is a separate privileged read-only observer for a decided `render.compose` review: it requires the exact child run/workflow/task/result attempt and fence, adopted root and `working_image` links, matching ready candidate bytes, original WorkingImage input, accepted candidate route and the installed actor. Missing or duplicate decisions refuse. This only proves a review decision on that child, not parent workflow completion or download; it has not been wired as an all-true `v2_workflow` runner. Unit matrix authored, NOT RUN.

`request_fixture_render_review_decision` adds the actual one-shot native POST for an independently pinned pending render review. It fixes tenant/run/review/revision/candidate SHA, accepts only exact successful `accept → candidate` response and uses verified TLS plus continuous fixture scope checks. Network uncertainty is an unknown outcome: caller must reconcile the exact review in SQL, never repeat the POST blindly. Physical preview acceptance, combined `v2_workflow` report and final download remain open. Unit matrix authored, NOT RUN.

`find_fixture_pending_render_review` now discovers exactly one current pending review in a privileged read-only snapshot before that POST. It binds the sealed waiting-review child run, deterministic render task, canonical result attempt/fence, adopted root and child links, ready candidate and original WorkingImage input; it rejects missing/duplicate/stale rows and returns the current revision. Preview acceptance and final downloadable workflow evidence remain separate. Unit matrix authored, NOT RUN.

`run_fixture_render_review` now composes the pending SQL preflight, one native decision POST and independent decided SQL observation under an overall deadline and installed fixture fence. It requires the same review/task/candidate and exactly next revision; an unknown POST outcome stops without replay and must be reconciled externally. Both SQL observers additionally require the adopted task result's `review_required=true`, matching the review-creation migration 01406. This closes the review decision stage only; before/after preview, final export/download and the combined `v2_workflow` probe remain open. Unit matrix authored, NOT RUN.

`observe_fixture_render_terminal` now independently checks the one-frame bulk parent and reviewed V2 child are both completed, sealed and not cancelled, with exact parent part/config/session/snapshot linkage, one successful ready render task, and only the accepted review decision/candidate observed above. It uses a privileged read-only snapshot and rejects missing/duplicate/foreign terminal evidence. Parent completion still requires deployed scheduler finalization; this observer does not trigger it. Preview and final download remain outside the current `v2_workflow` probe. Unit matrix authored, NOT RUN.

`verify_render_preview_bytes` adds the physical before/after preview byte check for an externally pinned review/source/canvas: closed API response, exact source/ref/variant/profile/geometry, canonical derivative manifest, downloaded image SHA/size, actual JPEG or alpha WebP geometry, installed ICC and stripped metadata. It consumes already downloaded bytes; independent current review reread remains to be connected. Unit matrix authored, NOT RUN.

`download_fixture_render_preview` now performs the native authenticated review-preview GET, validates both signed links before using either, requires exact independent bucket/key/version and artifact SHA/size, downloads bounded manifest/image bytes and delegates physical validation to `verify_render_preview_bytes`. Signed URLs must use the installed HTTPS object origin, path-style exact object key, one matching versionId and short expiry; neither object URL receives the API bearer token. Current review reread and the before-side catalogue remain to be wired into the complete probe. Unit link/refusal matrix authored, NOT RUN.

`observe_fixture_after_preview_objects` now supplies that independent SQL catalogue for the after side: a privileged read-only snapshot binds pending render review/candidate to the completed preview run, exact derivative receipt, ready output uploads and physical manifest/image versions. It rejects missing/duplicate rows, foreign buckets, stale producer attempts/fences and unsupported media. Source canvas/alpha must still come from physically verified WorkingImage, and the full before/after predecision lifecycle remains to be composed. Unit matrix authored, NOT RUN.

`observe_fixture_before_asset_preview` now implements the separate live original-asset fallback used by the backend review API. It requires the pending review's exact base WorkingImage pin, accepted current asset version in the same project, completed media.preview task/result, exact two-output upload receipts and private physical object versions. Shared decoder refuses missing/duplicate/foreign objects. Preview source canvas/alpha, before/after orchestration and final download remain open. Unit matrix authored, NOT RUN.

The physically verified V2 batch evidence now retains the canonical decoded output `WorkingImage` alongside its `ImageResult`, with an explicit hash/reference recheck. A future before/after probe can derive the after-side geometry and alpha from verified source bytes instead of the API preview response. The before-side original remains the independently pinned source WorkingImage supplied to the batch fixture. Tests remain NOT RUN.


### Concrete purge metadata observation

V32: added concrete read-only verify_purged_tenant_metadata SQL probe. Dedicated idle superuser exact DB, repeatable-read read-only/RLS off, bounded statements/locks and operator-installed fixture scope throughout. Requires deleted tenant tombstone, exact completed tenant deletion job with cleared claim, suspended memberships and only tenant lifecycle audit. Enumerates current photo tenant_id tables from pg_catalog (<=512; mandatory core tables), checks all nonretained tables empty with quoted identifiers/parameterized tenant. Newly added tenant tables cannot silently evade probe. Eleven positive/authority/tombstone/remaining/new-table/catalogue/cancel unit modes authored NOT RUN. No purge/delete mutation. Actual object versions/multipart absence, HTTP access denial, canonical deletion fixture execution, report writer/runner integration and real SQL acceptance remain open.
Retained table exceptions are closed: memberships (all suspended), audit_log (tenant.* actions only), deletion_jobs (exact fixture tenant job completed/token+lease cleared). No broad table-name/prefix exception. Data from all other current nonpartition child photo tables containing tenant_id must be absent; unknown surviving rows refuse. Tombstone is retained in tenants.id. This observer consumes an already completed fixture purge, does not initiate deletion and alone cannot supply all purge_negative checks.


### Concrete purge storage observation

V32: added concrete Go Store.ObservePurgedTenant read-only MinIO all-version/incomplete-multipart observation. Installed exact fixture/paused-writer guard,30s deadline, two complete passes over tenants/UUID/,staging/tenants/UUID/,originals/tenants/UUID/ for the installed bucket. Any object/version/delete marker/upload/list error/nil stream refuses; early refusal cancels child listing context. Eleven namespace/remnant/error/late-visibility/guard/cancel unit modes authored NOT RUN. Caller must enumerate all independently installed buckets; this is not proof of deployment-wide coverage. Existing canonical PurgeTenantBatch only scans tenants/ prefix: staging/original object cleanup coverage must be reconciled before complete purge qualification, not hidden by narrowing this observer. HTTP denial/report-runner integration and real MinIO acceptance remain open.
No object deletion, multipart abort or repair occurs. Delete markers also count as physical remnants. Exact prefix derivation uses nonzero UUID and trailing slash; neighboring tenants are excluded. Two successful listings cannot replace independently enforced writer fencing or actual object-store consistency qualification. All installed buckets and tenant key namespaces must be covered before objects_absent can be asserted.


### Complete canonical tenant namespace purge

V32: fixed actual Store.PurgeTenantBatch namespace gap. Canonical shared tenantNamespaces now covers tenants/,staging/tenants/,originals/tenants/ for purge and read-only observer. Single deletion budget1..256 spans all namespaces and both exact object versions/incomplete uploads; successful completion requires second empty pass over every namespace. Validates UUID-scoped keys/exact versions, handles nil streams/errors/cancellation and cancels listing goroutines on early exit. No new privileges or SQL purge relaxation. Authored shared-budget/all-namespace/neighbor unit plus seven refusal/late-observation modes; expanded real MinIO integration from3 to9 objects across namespaces, NOT RUN. Actual multipart/versioned/native/deployment coverage and full purge report/HTTP probe integration remain open; no production purge executed.
This supersedes the previously recorded tenants/-only implementation gap. Maintenance history-before-purge and SQL lease/tombstone/metadata conditions remain unchanged. All separately installed buckets still need canonical cleaner coverage; this method operates on its installed Store bucket only. Conservative true at the exact batch limit requests an additional pass, even if the final deletion emptied the namespace.


### Concrete JSON HTTPS negative access observer

V32: added concrete bounded verified-TLS HTTPS JSON access-denial observer. Exact installed origin/routes/credential and fixture guard; no redirect/retry/proxy,5s per connection,16KiB body cap/identity encoding. Requires configured domain denial status+code (401/5xx/redirect never qualify), with before/after200 positive control using same credential and independently pinned control-body SHA. Sensitive routes/authorization excluded from target repr and errors discard transport detail. Thirteen response/control/network/scope unit scenarios authored NOT RUN with HTTPS transport mocked. This covers JSON API/gateway only; direct signed S3 XML denial, real isolated HTTP fixture qualification and purge report runner integration remain open.
RestoreAccessTarget installs HTTPS origin, private denied/control paths and authorization, exact denial status/code, independent SHA of stable positive-control JSON bytes. Operator scope guard must bind the route/resource/principal to this restore fixture and ensure credentials remain valid. Control resource belongs to a live separate fixture and must remain readable before/after negative request. The observer does not mint credentials or infer fixture identity from URL. Accepted pairs:403/FORBIDDEN,404/NOT_FOUND,409/DOWNLOAD_REVOKED,403/LEASE_LOST,404/ARTIFACT_MISSING. Lifecycle-specific runner must choose the documented expected pair and actual preexisting resource; an arbitrary404 path is not valid negative evidence.


### Signed S3 purge access observer

V32: added concrete path-style signed HTTPS S3 negative observer for exact fixture bucket/key/version. Closed SigV4 query scope/credential/session equality, exact installed origin, unexpired links with30s margin, verified TLS,5s connection/16KiB XML bounds, no redirects/retries; before/after positive control requires same signing principal and exact control version/body SHA. Only404 NoSuchVersion/NoSuchKey qualifies, not AccessDenied/expired signature/5xx. Rejects XML DTD/entity/duplicate Code. Sixteen transport/scope/signature/control/XML unit modes authored NOT RUN. Concrete original-link baseline/fixture provisioning, real S3 qualification and combined purge report runner remain open; observer does not itself prove link was readable before purge.
Operator installs path-style HTTPS URLs signed for exact versionId, a previously readable fixture object and independent live control. No endpoint/credential is selected from user job payload. Control may be in a separate active fixture tenant and must share signing credential/session; control object bytes <=1MiB. Historical denied object identity and positive pre-purge baseline must be retained by fixture executor; random nonexistent keys cannot qualify purge acceptance. Virtual-host URLs or additional presign query overrides are not silently rewritten; installation must use supported canonical path-style links or provide a separately qualified adapter.


### Concrete combined purge observation

V32: added concrete Python MinIO purge storage observer across all installed buckets and three namespaces, two all-version/multipart-empty passes. Private multipart SDK call is explicitly pinned minio==7.2.18 in restore optional dependencies; unsupported version refuses. Validates exact bucket/truncation/upload receipt, closes version iterator and treats any item/delete marker/error as refusal. Twelve SDK/bucket/namespace/remnant/error/scope unit modes authored NOT RUN. Added observe_completed_fixture_purge concrete composition of SQL metadata, MinIO storage, JSON HTTPS and signed-S3 observers, with final fresh SQL/storage checks and continuous installed fixture authority. This observes an already completed canonical purge; baseline provisioning/deletion execution, report publication/runner installation and real lifecycle qualification remain open.
SDK private API was inspected in upstream [MinIO Python7.2.18 api.py](https://github.com/minio/minio-py/blob/7.2.18/minio/api.py) and [datatypes.py](https://github.com/minio/minio-py/blob/7.2.18/minio/datatypes.py). Multipart query uses delimiter empty, exact prefix,max_uploads1; nonempty or truncated result refuses rather than following markers. Installed MinIO transport must have verified TLS and bounded timeouts; observer does not infer those from client object. Restore optional dependency pin has not been installed/resolved/qualified. Combined observer requires independent operator closure binding all actual endpoints, buckets, principal and historical positive baseline; no report-declared boolean substitutes that authority.


### Canonical purge fixture lifecycle request

V32: added concrete native HTTPS DELETE fixture tenant lifecycle request using existing /api/v1/tenants/{id}, Bearer identity and exact quoted If-Match revision, verified TLS/no redirects/5s/16KiB, strict202 queued job receipt. Transport failures explicitly unknown, never retried automatically. Added bounded<=300s dedicated privileged exact-DB read-only wait for that exact tenant/resource/job, requiring canonical completed/deleted/cleared-token+lease; each poll is a fresh short transaction with continuous fixture guard. Fourteen API/wait/refusal/timeout unit modes authored NOT RUN. These functions do not create baseline fixtures, start maintenance or bypass canonical cleanup/history; baseline binding/full report runner and real lifecycle acceptance remain open.
Installed operator fixture authority must verify isolated origin/tenant/principal/current revision before mutation. A maintenance process permitted only for this isolated fixture must perform existing deletion history/object purge/SQL finalization; read-only waiter does not do cleanup itself. Unknown HTTP outcome requires independent exact tenant-job reconciliation while production remains fenced; never bump revision or issue another destructive request blindly. Wait timeout/failure never authorizes metadata purge or fabricates completion.


### Pre-purge fixture baseline

V32: added pre-purge fixture baseline verifying exact pinned object bytes through existing MinioRestoreObjects/streaming inventory (<=512 refs,256MiB), mandatory tenants/staging/originals namespace coverage and exact selected signed-S3 bucket/key/version match. Added actual200 API body-SHA and signed-S3 version/size/SHA readable observation branches using shared guarded TLS/control/signature paths; refusal before deletion on missing/changed resources. Eight baseline orchestration modes plus three API/four S3 positive-baseline modes authored NOT RUN. Fixture provisioning, concrete lifecycle+observation report runner, actual native acceptance and production authority installation remain open; baseline proves supplied references, not whole-system completeness.
Fixture references must be sorted unique immutable RestoreObject entries scoped to the exact tenant. Each of the three canonical namespaces requires at least one known readable object; signed-S3 denial target must appear exactly once in the inventory and its positive sample is limited to1MiB. API readable target body independently SHA-pinned and limited16KiB, live control remains verified before/after. Baseline helpers only read and never initiate purge or provision arbitrary production resources. Installed fixture authority must bind endpoints, credentials, version pins, resource route and historical fixture specification before destructive API request.


### Concrete purge-negative runner

V32: added concrete run_purge_negative_probe sequencing independently pinned stable fixture descriptor, exact SQL active/revision+database prechecks before baseline and before DELETE, readable bytes/API/S3 baseline, canonical native DELETE, exact-job wait, combined SQL/MinIO/API/S3 post-purge observations and private atomic report. Descriptor excludes credentials/signed URL queries, capped128KiB before mutation; probe report now requires nonempty observations and supports<=256KiB. Shared writer binds detailed fixture/baseline/completion/purge observations to existing report/check schemas, no overwrite/fsync/re-read. Added nine runner ordering/refusal, five actual-file report and six SQL-start unit modes; updated existing probe fixtures, all NOT RUN. No production purge executed. Pre-provisioned fixture/maintenance authority installation, ambiguous-outcome resume, actual native lifecycle acceptance and remaining three probe executors remain open.
Operator adapter can dispatch purge_negative to run_purge_negative_probe with exact VerifiedRestoreReplay/fixture pin and independently installed isolated fixture fence. Pin is SHA256 canonical purge_fixture_descriptor: current DB, tenant/revision, installed buckets, sorted immutable refs, API/delete origins, SHA of private API routes, expected API baseline/control SHA/denial and S3 target/control identities. No credentials/query signatures are serialized. Pin must be derived from independently reviewed fixture specification, not trusted because runner computed it.

This supersedes the earlier <=64KiB probe-report limit: closed restore-probe-report/v1 now also requires observations, total<=256KiB. Old incomplete probe artifacts must be regenerated; no historical acceptance claimed. Final report contains verified baseline inventory digest, API/S3 readable witnesses, completed job identity and actual post-purge observer evidence. Report file `runUUID.purge_negative.json` is exclusive; operator fixture fence must also ensure exclusive lifecycle ownership. Destructive work may have completed if later publication fails: reconcile retained fixture/job/baseline rather than blindly retrying or recreating the tenant. Fixture provisioning and maintenance scheduling remain operator-installed prerequisites, not success booleans supplied by report.


### Concrete learning-consent revoke SQL observation

V32: added concrete read-only observe_fixture_learning_consent baseline/committed-revoke SQL observer. Exact privileged DB/idle readonly snapshot, active tenant and fixture grant purpose/person/policy/epoch/revision; baseline requires real immutable example lineage and eligible actively selected learning version. Post-revoke requires exact+1 epoch/revision/actor, immutable revoke event, all old-epoch linked versions disabled with none active, and cancellation requested for every linked nonterminal workflow run. Twelve unit authority/scope/lineage/baseline/event/dependency/cancellation modes authored NOT RUN. Covers learning_person/learning_photographer fixture scope; identity-matching-only acceptance separate. Native revoke request, actual denied processing/access request, full report runner and real concurrency/native acceptance remain open.
Fixture retains exact tenant/project/grant/person/purpose/old epoch+revision/policy/actor and a genuinely active eligible learning_version_id whose immutable example link used the old grant epoch. Post-revoke observation neither deletes originals/exports nor mutates immutable versions/events. Successful SQL observation alone does not prove processing admission denial or capability/download denial; those must be executed independently before revoke_negative report publication. No direct update of consent state is used by this observer.


## V32 canonical consent revoke lifecycle (2026-10-11)

V32: added canonical native consent POST revoke with exact CAS revision, stable caller idempotency key, verified TLS/bounded response/no automatic retry, strict projected resource binding to fixture grant/tenant/project/actor/policy and incremented epoch/revision. Added baseline -> API -> independent committed SQL observation composition. Thirteen transport/receipt/installation refusal modes and four ordering modes authored NOT RUN. Processing/access-denial executor, full revoke report runner, concurrency and real lifecycle acceptance remain open. No tests/builds/migrations executed; whole-product completion remains unestablished.


## V32 concrete consent-gated recipe access (2026-10-11)

V32: added concrete consent-gated immutable recipe access probe for exact GET /v2-documents/{id}. Read-only privileged exact-DB model_use_links/document/artifact lineage and immutable SHA are required before and after HTTPS observation. Baseline requires200 exact bytes; revoked requires409 PROFILE_CONSENT_REVOKED with live positive controls. Original photos/approved exports are not deletion targets. Ten scope/authority/lineage/late-change/transport unit modes authored NOT RUN. Processing-admission executor, full revoke runner, race/native acceptance remain open; no tests/builds/migrations executed.


## V32 composed revoke and access lifecycle (2026-10-11)

V32: concrete observe_fixture_revoke_lifecycle composes readable recipe baseline, canonical CAS consent revoke with independent SQL effects, exact consent-gated HTTP denial and final unchanged committed SQL evidence. Requires identical fixture/origin/credential and continuous installed scope; no automatic mutation retry. Nine ordering/scope/substitution/failure unit modes authored NOT RUN. This does not replace processing-admission denial or race acceptance and does not publish a complete revoke acceptance report. Full runner/deployment remain open; tests/builds/migrations not executed.


## V32 actual freeze denial transport (2026-10-11)

V32: traced actual render freeze native route and learning reader. ReadEligibleLearningProfile currently maps missing eligible/live consent scope to409 REVISION_CONFLICT. Added concrete bounded native freeze POST denial observer with exact request, fresh caller key, verified TLS, no retry, no replay acceptance and explicit ambiguous-code limitation. Ten transport/scope/refusal unit scenarios authored NOT RUN. Caller must independently prove current revision/qualification/revoked lineage/unused key; receipt alone deliberately does not satisfy processing-denied acceptance. Concrete SQL preconditions, positive freeze baseline, no-side-effect check and full revoke report runner remain open. Tests/builds/migrations not executed.


## V32 current freeze catalogue guard (2026-10-11)

V32: added independent read-only exact-DB current freeze catalogue guard: latest accepted asset version/master, current draft document/revision/SHA, ready immutable recipe, model-use version, active tenant and absent exact actor/route/key idempotency record. Composed with committed revoke SQL proof before/through/after actual native freeze denial; changed evidence refuses. Six catalogue authority/stale/replay/scope unit modes authored NOT RUN. Qualification/positive freeze baseline and complete processing acceptance/report remain open; no tests/builds/migrations executed.


## V32 positive render freeze baseline (2026-10-11)

V32: added actual native positive render-freeze baseline using shared bounded TLS/no-retry transport. Strict <=4096 closed result, Go-compatible UUIDv5 frozen document identity, exact draft/revision and independently supplied source version/artifact/SHA/canvas binding, canonical recipe pin; any Idempotency-Replayed header refuses. Ten positive/substitution/replay/denial/network unit modes authored NOT RUN. Physical frozen recipe/SQL registration verification, distinct baseline-vs-denial keys composition and complete revoke runner remain open; HTTP200 alone does not qualify full processing acceptance. No tests/builds/migrations executed.


## V32 registered physical frozen result (2026-10-11)

V32: added verify_fixture_frozen_result independent exact-DB registered upload/draft/source/learning provenance check and exact-version MinIO physical JSON verification (<=1MiB, bounded64KiB reads, SHA/size/header checks). Uses existing full FrozenRenderRecipe decoder; binds authored SHA, learning version, source version/artifact/SHA/canvas and rechecks complete physical catalogue after IO. Eight catalogue/transport/substitution unit modes authored NOT RUN with decoder mocked (not a full codec acceptance claim). Full runner and actual lifecycle/role/physical decoder acceptance remain open; tests/builds/migrations not executed.


## V32 executable revoke freeze lifecycle (2026-10-11)

V32: added concrete run_fixture_revoke_freeze_lifecycle binding one actor/tenant/document/asset/origin/credential and distinct positive/negative freeze keys. Both keys/current draft preflight before mutation; actual positive freeze and registered physical result verification, canonical revoke/access lifecycle, guarded fresh freeze denial and final unchanged consent evidence. Nine orchestration/binding/failure scenarios authored NOT RUN. This intentionally does not claim batch-queue/worker admission prohibition or complete restore probe report; those gates and native/race acceptance remain open. No tests/builds/migrations executed.


## V32 render batch refusal transport (2026-10-11)

V32: traced render-batch Queue/loader error boundary and added actual one-frame native batch refusal request using exact positive frozen recipe/source pins and immutable snapshot/policy. Strict scoped TLS/no retry/no replay/bounded response; accepts current422 VALIDATION_FAILED diagnostic only, never acceptance because generic validation does not identify consent cause. Nine transport/refusal/scope modes authored NOT RUN. Independent live snapshot/qualification/no adoption catalogue and worker admission revoke proof remain open; no tests/builds/migrations executed.


## V32 batch snapshot and no-adoption catalogue (2026-10-11)

V32: added independent read-only one-frame batch catalogue guard. Exact privileged DB, live actor-owned snapshot with exactly one ordinal0 source/version/SHA in the project and active tenant; deterministic Go-compatible V2.Admit batch ID; rejects any existing run/workflow/task or exact actor/key admission receipt. Seven authority/snapshot/partial-adoption scenarios authored NOT RUN. Snapshot-kind/full payload/qualification and actual before-after batch composition/worker revoke acceptance remain open; no tests/builds/migrations executed.


## V32 canonical snapshot guarded batch refusal (2026-10-11)

V32: batch catalogue now mandates actual v25_lens_snapshot_ready with explicit live actor session: canonical v2 kind/nonexpiring complete inventory, source/original readiness and editor ACL. Added guarded concrete batch refusal composition rechecking committed revoke plus canonical snapshot/no adoption before/through/after HTTP and equality of final evidence. Updated catalogue matrix (8 modes), added5 orchestration/refusal/change modes, all authored NOT RUN. Generic422 still needs qualified positive batch and worker evidence; complete revoke acceptance remains open. No tests/builds/migrations executed.


## V32 positive render batch ticket baseline (2026-10-11)

V32: added actual positive one-frame render-batch native POST baseline through shared bounded transport. Strict202 closed ticket requires Go-compatible V2.Admit batch UUIDv5 and render.batch:0 child UUIDv5, exactly one child and queued/running/waiting_review state; replay header refuses. Added9 positive/ticket-substitution/state/replay/refusal unit modes authored NOT RUN. SQL task/graph registration, completed worker output baseline and whole revoke scenario remain open; HTTP admission does not prove execution or qualify production acceptance. No tests/builds/migrations executed.


## V32 positive batch graph registration (2026-10-11)

V32: added independent exact-DB positive batch adoption SQL observation binding canonical project/request digest, saved202 ticket, deterministic parent/child/workflow IDs, exact snapshot/actor/session/project, one manifest part/config hash, sealed child graph and render.compose/1 task. Shared request-body compiler now feeds transport and observer. Six ticket/hash/authority/graph/scope scenarios authored NOT RUN. This proves supplied registration only, not completed worker output or full revoke acceptance; native SQL and execution baseline remain open. No tests/builds/migrations executed.


## V32 canonical worker result catalogue (2026-10-11)

V32: added independent canonical worker result catalogue observation: deterministic render.compose:0 task, succeeded/current canonical result, exact attempt/fence with completed error-free attempt and actual v26_render_task_result_ready, ready immutable tenant-scoped bounded JSON root. Six authority/not-ready/attempt/scope/size scenarios authored NOT RUN. Physical ImageResult/child bytes and full worker/revoke lifecycle remain open; SQL catalogue alone does not prove output quality or actual acceptance. No tests/builds/migrations executed.


## V32 physical ImageResult and direct children (2026-10-11)

V32: added exact-version physical canonical ImageResult verification using existing strict codec; unique nonzero/nonroot child refs, independent SQL child producer task/attempt/fence/ready registration and physical SHA/size verification of all3..4 direct JSON children. Rechecks child catalogue and canonical task result after IO. Six root/alias/registration/scope/change unit modes authored NOT RUN. Nested WorkingImage/mask arrays, report/source semantic quality and actual worker lifecycle remain open; direct bytes do not qualify full image output. No tests/builds/migrations executed.


## V32 render semantics and unchanged-source authority (2026-10-11)

V32: added strict real-codec render direct-document semantic validator (WorkingImage/ImageReport/MaskRef canonical bytes, render.compose pass/review, exact source binding/no conflicting version, coherent canvas/nonzero nested refs). Seven codec/report/source/canvas scenarios authored NOT RUN. Inspection also found valid unchanged render reuses source WorkingImage: physical child authority now permits exact registered WorkingImage task input for that sole role, retaining produced attempt/fence for other children. Updated physical fixture assertion. Semantic validator not yet composed with physical loader; nested NPY/quality qualification remain open. No tests/builds/migrations executed.


## V32 physical semantic render integration (2026-10-11)

V32: physical batch verifier now requires exact source VersionBinding, reopens/rechecks each direct child version/SHA and invokes strict semantic codecs before final catalogue/task recheck. Semantics aligned with actual render adoption: exact sole report source, no registrations, same source canvas/float32, changed/generated selectors and exact mask transform/confidence/protection fields. Physical matrix expanded8 modes (late bytes/semantic failure); real-codec semantic matrix10 modes. Authored NOT RUN. Parent report DAG inheritance, nested NPY arrays and actual lifecycle remain open; no tests/builds/migrations executed.


## V32 exact-version streamed render NPY (2026-10-11)

V32: added exact-version MinIO streaming adapter to existing production verify_lens_array validator. Bounded64KiB chunks, <=200MP, exact canonical float32 NPY header/shape/SHA/size, finite RGB, unit alpha and binary masks, continuous fixture/cancellation, cleanup on refusal. Nine real NPY/validator modes authored NOT RUN. Explicitly physical-only: producer/input reuse SQL authority and integration with nested image refs remain open; no fabricated ownership from validator output-source interface. No tests/builds/migrations executed.


## V32 canonical produced NPY authority (2026-10-11)

V32: added exact-DB produced NPY catalogue requiring artifact/SHA/role/MIME, canonical succeeded render.compose result, matching task/result/artifact attempt+fence, ready immutable scoped object. Concrete wrapper streams existing NPY verifier then repeats SQL physical identity; changed version refuses. Five authority/producer/scope/late-substitution modes authored NOT RUN. Source RGB/alpha reuse requires separate exact WorkingImage input proof; full nested result integration remains open. No tests/builds/migrations executed.


## V32 exact source array reuse authority (2026-10-11)

V32: added exact source RGB/alpha reuse verifier. Canonical physical WorkingImage SHA/source canvas/float32 and exact selected nested ref; privileged SQL requires same canonical succeeded task result/attempt/fence, registered WorkingImage+NpyArray inputs and exact source asset-version/master binding. Exact-version stream validation then fresh identical SQL object required. Six real source-codec/authority/lineage/substitution unit modes authored NOT RUN (array stream mocked; separate real NPY suite exists). Full nested orchestration and parent report DAG remain open; no tests/builds/migrations executed.


## V32 full nested render array integration (2026-10-11)

V32: integrated all nested render NPY routing into physical batch result verification. Exact pinned canonical source WorkingImage and source-preserving canvas/color/transform, RGB/alpha exact reuse uses input authority; other RGB/alpha and all mask arrays use canonical output authority and real streaming verifier. Four routing/source/failure scenarios authored NOT RUN; updated physical orchestration fixture. Parent report DAG, real native lifecycle and full revoke runner/report remain open; no tests/builds/migrations executed.


## V32 registered physical parent report reader (2026-10-11)

V32: added RestoreParentReportReader for existing bounded production DAG walker: exact registered ImageReport task input and canonical result/attempt/fence, private exact DB, ready versioned scoped JSON, physical SHA/canonical real codec and repeated catalogue identity. Six real codec/transport/DAG modes authored NOT RUN (catalogue mocked; SQL native qualification open). Root inventory derivation from frozen closure and integration/review inheritance into full batch probe remain open; no tests/builds/migrations executed.


## V32 authoritative parent DAG integration (2026-10-11)

V32: physical batch result now derives parent roots through actual v26_render_adoption_inputs via existing decoder (succeeded exact generation, live qualification/adoption readiness). Reader validates exact registered ImageResult roots plus ImageReport DAG; output must retain exact ordered roots and inherited review, scoped roots re-read after traversal. Five orchestration/inheritance/change modes authored NOT RUN; physical fixtures updated. Actual SQL/native/quality acceptance and full batch/revoke orchestration remain open; no tests/builds/migrations executed.


## V32 executed worker baseline composition (2026-10-11)

V32: added executable positive batch baseline composition: canonical snapshot/no-adoption preflight, one native POST, saved graph verification, <=300s fresh read-only task polling with cancel/terminal refusal and complete physical/semantic/nested-array/parent-DAG verification. Uses independently pinned source bytes; never starts services or retries POST. Six ordering/failure/timeout orchestration modes authored NOT RUN. Actual service installation/runtime fixture and full combined revoke report remain open; no tests/builds/migrations executed.


## V32 combined batch/revoke lifecycle (2026-10-11)

V32: combined revoke runner optionally preflights both batch keys before mutation, completes positive batch worker/physical proof before consent revoke and performs guarded negative batch admission afterwards, with one snapshot/policy/source/frozen recipe and distinct keys. Mandatory typed batch wrapper rejects missing installation. Added12 sequence/binding/failure modes plus missing-installation test, authored NOT RUN. Concurrent worker revoke race, independently pinned complete probe report and deployed restore lifecycle remain open; no tests/builds/validators/migrations executed.


## V32 independent revoke fixture installation (2026-10-11)

V32: added independent canonical revoke fixture descriptor and pinned execution wrapper binding database/tenant/actor/consent/document/access/source/freeze/batch snapshot-policy-session and hashed idempotency keys before any mutation. Credentials and source bytes excluded. Six pin/substitution/privacy orchestration scenarios authored NOT RUN. Wrapper intentionally returns observations only: generic422 consent causality, concurrent worker revoke and complete acceptance report remain open; no tests/builds/validators/migrations executed.


## V32 restore identity and polling deadline (2026-10-11)

V32: independently pinned revoke descriptor now also binds restore run/build/recovery/replay identities; added4 cross-restore substitution cases. Positive batch polling now refuses successful SQL observations returned after the total polling deadline; added late-success case. Tests authored NOT RUN. Source trace confirms FrozenRenderRecipe catalogue absence maps to generic ErrInvalid/422, so causal consent refusal and full acceptance report remain open. No tests/builds/validators/migrations executed.


## V32 explicit batch consent refusal (2026-10-11)

V32: CandidateProfile catalogue now diagnoses PROFILE_CONSENT_REVOKED409 only with editor-authorized frozen root and exact authored draft/stream/model-use/version/model lineage plus nonlive consent predicate. Unknown/foreign artifacts retain generic invalid. Restore batch probe now requires explicit409 and rejects generic422. Authored loader propagation, HTTP mapping and negative transport regression coverage NOT RUN. SQL-native lineage/ACL/race qualification and full acceptance report remain open; no tests/builds/validators/migrations executed.


## V32 private consent diagnostic authority (2026-10-11)

V32: source audit found photo_api cannot execute private v23_learning_version_consents_live. Replaced direct catalogue diagnostic SQL with narrow SECURITY DEFINER v26_render_model_consent_revoked in forward-only01590, preserving exact root/model/editor/session scope and granting only photo_api EXECUTE (no broad internal grant). Added native integration coverage for API-only diagnostic ACL and unknown/foreign tenant absence; authored NOT RUN, migration NOT APPLIED. Positive native learning fixture and concurrent revoke/full report remain open.


## V32 render consent startup contract (2026-10-11)

V32: render admission startup now requires exact01590 diagnostic signature, boolean return, SECURITY DEFINER/private fixed search_path, nonsuperuser/nonbypass photo_fn_owner, actual photo_api caller/execute, no PUBLIC/worker/scheduler EXECUTE. Installed render service refuses invalid diagnostic before constructing scheduler pool. Added unit missing/invalid/query-failure cases and native API-role installation invocation; authored NOT RUN, migration NOT APPLIED. Positive SQL lineage/concurrent revoke/full report remain open.


## V32 registered consent diagnostic matrix (2026-10-11)

V32: extended existing registered render model fixture invoked by test_v23_training_freeze with01590 live-consent false/revoked true checks, all10 argument substitutions (tenant/project/source-version/root/rootSHA/learning/model/modelSHA/actor/session), authored lineage tampering and rollback restoration. Metadata-only force-rollback fixture, no HTTP/worker or physical quality claim; authored NOT RUN and01590 NOT APPLIED. Positive native execution, concurrent revoke and complete restore report remain open.


## V32 running render revoke witness (2026-10-11)

V32: added read-only running-render witness for concurrent revoke fixture: exact privileged DB, deterministic child/workflow/task, linked live grant purpose/person/epoch, graph/running/noncancelled task+run, current unfinished attempt/fence/live lease, no result/adoption. Eight authority/generation/scope modes authored NOT RUN. Witness explicitly is not a race barrier or acceptance: post-revoke cancellation/adoption exclusion and full orchestration/report remain open.


## V32 cancelled render generation witness (2026-10-11)

V32: added strict post-revoke cancelled render observation tied to prior deterministic running witness and same attempt/fence. Requires finished cancelled task, run/task cancel flags, released lease, no canonical or other task result, exact linked revoked grant/purpose/person/epoch. Changed generation/not-settled refuses; no retry/cancel/acceptance side effects. Seven scope/authority/generation/settlement unit modes authored NOT RUN. Bounded race orchestration, actual concurrent native execution and complete restore report remain open.


## V32 bounded running revoke lifecycle (2026-10-11)

V32: added bounded installed running-render revoke lifecycle: baseline consent and twice-identical live generation, one native revoke POST, committed consent, readonly cancellation polling<=300s and unchanged final consent. Only not-settled observations poll; authority/unknown POST/generation failures never retry mutation; late success refuses. Seven orchestration/failure/deadline scenarios authored NOT RUN. Explicitly observations only: precise worker execution barrier, independently pinned installation/report and actual native race qualification remain open.


## V32 independently pinned running fixture (2026-10-11)

V32: added independent canonical running-revoke fixture descriptor and pinned executor binding restore run/database/build/recovery/replay, full consent identity/epoch, hashed request key, deterministic child/workflow/task and timeout before SQL/HTTP. Credentials excluded. Nine privacy/pin/substitution scenarios authored NOT RUN. Observations only; precise worker commit barrier and complete independently revalidated acceptance report/native execution remain open.


## V32 explicit profiled renderer barrier (2026-10-11)

V32: added explicitly injected RestoreBarrierRenderer retaining ProfiledTaskRenderer identity and real authoritative provider dispatch. Holds one exact tenant/task/generation before compute while executor heartbeats continue, exposes held generation, polls cancellation<=50ms, bounded<=300s, refuses reuse/foreign task/timeout; cache bypass cannot qualify held witness. Five dispatch/cancel/timeout/scope/reuse unit modes authored NOT RUN. Not production-default/env enabled. Barrier-to-revoke coordinator, actual native race and full acceptance report remain open.


## V32 held generation revoke binding (2026-10-11)

V32: running revoke coordinator accepts only explicitly typed renderer barrier, requires held SQL-matching tenant/task/attempt/fence before POST and TaskCancelled-finished same generation without release after SQL cancellation. Barrier records cancellation separately from timeout/provider failure; returned observation flags barrier qualification. Expanded real adapter unit cases for exact held/cancelled generation and release rejection, authored NOT RUN. Pinned barrier mode, full report composition and actual isolated native execution remain open.


## V32 pinned barrier mode (2026-10-11)

V32: running-revoke independent descriptor now pins explicit boolean barrier_required; executor accepts only typed optional RestoreBarrierRenderer and forwards same instance. Added removal/addition rejection and valid barrier routing unit modes; authored NOT RUN. Existing fixture pins must be regenerated independently for changed descriptor. Full combined restore report and actual native execution remain open.


## V32 combined revoke report runner (2026-10-11)

V32: added concrete combined revoke_negative runner with independent combined fixture pin, distinct same-scope grants, mandatory typed barrier, running cancellation first then positive-worker/fresh admission/access refusal chain, final consent rechecks and private durable probe report. Seven orchestration/refusal modes authored NOT RUN. Native deployment/isolation, fixture independence and full current acceptance remain unverified; tests/builds/migrations not executed.


## V32 fixture independence and late result guard (2026-10-11)

V32: combined report now preflights independent distinct learning versions/grants in exact privileged readonly DB, rejects cross-linked consent dependencies before first revoke; re-observes cancelled generation and unreleased cancelled barrier after second chain before report. Six independence plus late-result regression cases authored NOT RUN. Native fixture/SQL/execution/report qualification remains open.


## V32 physical report reference serialization (2026-10-11)

V32: source audit found real parent-report evidence retains strict Pydantic ArtifactRef objects after dataclasses.asdict, rejected by private report JSON serializer. Added explicit ArtifactRef id/SHA conversion only; four nested-pin/unknown-bytes/unknown-object/float regression cases authored NOT RUN. No generic stringify or secret bytes serialization. Native full report execution remains unverified; tests/builds/migrations not run.


## V32 combined pure admission preflight (2026-10-11)

V32: extracted pure shared validate_fixture_revoke_installation from standalone runner; combined report now requires typed batch installation and validates complete admission bindings/source/hash/snapshot/policy/key/session before first running revoke, preventing malformed second chain after irreversible first mutation. Added combined invalid-admission no-mutation regression; existing full binding matrices exercise shared validator. Authored NOT RUN; native execution/acceptance remains open.


## V32 legacy Apply admission transport (2026-10-11)

V32 legacy_apply: added actual bounded TLS native one-selected-item Apply POST binding collection revision/style/item, strict202 queued run/output ticket, no replay/redirect/retry, unknown outcome explicit. Eight transport/scope/ticket modes authored NOT RUN. This is admission only: preview baseline, SQL registration/completion, actual output/download bytes and full legacy report remain open.


## V32 legacy Apply registered request (2026-10-11)

V32 legacy_apply: added exact privileged readonly registration observer binding actual Run.Apply canonical fingerprint/saved202 response SHA/resource, actor/run/output/style, source collection revision, one-item snapshot source/version/artifact/SHA, apply_build and canonical config hash/semantic identity. Shared request compiler feeds POST and SQL proof. Six receipt/authority/lineage/config cases authored NOT RUN. Completion/preview/download/native full report remain open.


## V32 legacy Apply adopted output catalogue (2026-10-11)

V32 legacy_apply: added completed/adopted output observer after exact registration recheck: completed sealed noncancelled run, complete cleared build, sole output item/new version parent/apply_source lineage, canonical succeeded producer attempt/fence and ready scoped bounded WorkingImage JSON object. Six authority/readiness/scope/identity/registration scenarios authored NOT RUN. Physical WorkingImage/nested arrays/quality, preview, download and complete report remain open.


## V32 legacy physical WorkingImage root (2026-10-11)

V32 legacy_apply: added exact-version bounded physical WorkingImage root read with SHA/size, real strict canonical codec, bounded canvas and nonzero/nonaliased nested refs, repeated adopted SQL catalogue equality. Six real-codec/root-substitution cases authored NOT RUN (object adapter mocked; separate adapter tests exist). Nested array authority/bytes, quality/preview/download/full report remain open.


## V32 legacy streamed arrays and alpha compatibility (2026-10-11)

V32 legacy_apply: traced actual working_rgb/working_alpha octet-stream publication and uint16 alpha reuse. Added bounded exact-version canonical little-endian NPY1.0 verifier for float32 RGB and uint16/float32 alpha, shape/SHA/size/header/finite checks; preserves finite HDR RGB outside0..1, constrains float alpha. Eight real NPY modes authored NOT RUN with object adapter mocked. Explicit physical-only: SQL produced/input-reuse authority and complete root integration/preview/download/report remain open.


## V32 legacy nested array authority integration (2026-10-11)

V32 legacy_apply: added canonical root-producer array SQL authority for working_rgb/working_alpha octet-stream, exact current attempt/fence and result output membership; alpha reuse requires exact declared task input. SQL catalogue repeated after physical stream. Integrated RGB/optional alpha verification into full WorkingImage reader with explicit alpha dtype (uint16 default), retained uint16 RGB compatibility. Six authority/ownership/substitution/stream scenarios authored NOT RUN; physical orchestration fixtures updated. Preview/quality/download/full report/native acceptance remain open.


## V32 legacy master quality semantics (2026-10-11)

V32 legacy_apply: added real strict QualityReport master_v1 validation with exact WorkingImage target/canonical bytes and production _decision threshold recomputation instead of trusting pass. Selected restore baseline requires pass/no reasons; reviewed fixtures need separate review evidence. Eight real-codec/forged-pass/clipping/seam/protected/nonfinite cases authored NOT RUN. Physical report/canonical quality producer binding and integration, preview/download/full report remain open.


## V32 registered physical legacy quality (2026-10-11)

V32 legacy_apply: added unique exact-target master quality SQL producer catalogue (quality ordinal0 canonical succeeded task/result/attempt/fence, declared target input and registered report), exact-version bounded SHA physical read, production semantic threshold validator and catalogue recheck. Integrated quality into WorkingImage+arrays physical verifier. Five SQL authority/absence/duplicate/scope modes authored NOT RUN; physical root mocks updated. Preview/download/full report/native acceptance remain open.


## V32 legacy preview admission (2026-10-11)

V32 legacy_apply: added bounded native HTTPS preview admission with immutable canonical params, exact asset version/draft revision, optional style/ROI and fixed generation=false; strict202 queued ticket, no replay/redirect/retry, explicit unknown network outcome. Eighteen transport/parameter/privacy cases authored NOT RUN. Ticket is not readiness proof; preview SQL/result/physical/download and complete restore report remain open. No tests/builds/validators/migrations executed.


## V32 registered legacy preview (2026-10-11)

V32 legacy preview: added exact privileged read-only registration observer binding Preview.Request fingerprint/saved202 response, input snapshot digest, live source/version/project, unique CPU editor.preview task, canonical config and normalized modules/ROI/style/declared inputs. Nine registration/substitution cases authored NOT RUN. Registration is not completion; preview result/physical/download/full restore report remain open. No tests/builds/validators/migrations executed.


## V32 completed legacy preview catalogue (2026-10-11)

V32 legacy preview: added completed canonical output catalogue after exact registration recheck: sealed completed noncancelled run, succeeded exact editor.preview task/current attempt/fence, canonical result with exactly two outputs, ready verified bounded JPEG derivative and JSON manifest, distinct artifact IDs and tenant-scoped versioned object pins. Seven catalogue/authority/scope/duplication cases authored NOT RUN. Physical manifest/JPEG validation, download and full restore report remain open. No tests/builds/validators/migrations executed.


## V32 physical preview manifest (2026-10-11)

V32 legacy preview: added exact-version bounded physical DerivativeManifest reader with catalogue recheck, strict real codec/JCS/SHA/size, source and JPEG refs, independently supplied source geometry/ICC pins, production dimensions/profile hash including ROI/scale, JPEG/sRGB/stripped/no-alpha policy. Eleven real-codec substitution cases authored NOT RUN. Source geometry installation, physical JPEG decoding/download and full restore report remain open. No tests/builds/validators/migrations executed.


## V32 physical preview JPEG (2026-10-11)

V32 legacy preview: added bounded exact-version JPEG reader, SHA/size and derivative reference binding, real Pillow verify+full decode, RGB/manifest dimensions, exact independently supplied ICC bytes and production profile hash, EXIF/XMP/comment refusal, repeated completed catalogue after IO. Eight real encoded JPEG/PNG/gray/truncation/profile/metadata cases authored NOT RUN. No perceptual quality claim; native writer metadata compatibility, installed geometry/ICC, download and full restore report remain open. No tests/builds/validators/migrations executed.


## V32 preview API download (2026-10-11)

V32 legacy preview: added native runs outputs API plus exact-version signed JPEG download verification: bounded TLS/no redirects/retries, strict derivative identity/SHA/size/media/expiry, installed object origin and versionId allowlist, no API bearer forwarded to storage, streamed byte SHA; evidence excludes signed URL/credentials. Seven transport/substitution scenarios authored NOT RUN. Full outputs manifest parity, catalogue recheck orchestration, installed source geometry/ICC and complete restore report remain open. No tests/builds/validators/migrations executed.


## V32 complete preview download set (2026-10-11)

V32 preview API: closed incomplete outputs-set check. Requires both exact registered derivative and manifest IDs/roles/SHA/size/media, distinct entries, valid expiry and installed-origin/versionId links before any object IO; downloads and hashes both exact byte streams. Evidence includes manifest digest/size, excludes URLs. Expanded transport matrix to11 modes including manifest substitution/foreign origin/duplicate and no-object-IO assertions. Authored NOT RUN. Catalogue recheck lifecycle and full restore report remain open; no tests/builds/validators/migrations executed.


## V32 complete preview lifecycle (2026-10-11)

V32 preview: added installed native lifecycle with fresh idempotency-key SQL preflight, one POST/no retry, bounded readonly result polling, manifest and JPEG physical verification, complete two-object API download and final catalogue identity recheck. Overall1..300s budget covers admission/physical/download and post-IO checks. Seven orchestration/preflight/unknown-outcome/physical/late-change scenarios authored NOT RUN. Independent fixture source/ICC pin installation, full legacy Apply/download/report composition and actual native run remain open; no tests/builds/validators/migrations executed.


## V32 pure preview installation preflight (2026-10-11)

V32 preview lifecycle: source audit found download origin and ROI bounds could fail only after mutating POST. Extracted shared pure native-preview/download installation validators and invokes both plus source ROI bounds before database/POST; resolves URL port validity before IO. Updated orchestration fixtures to real verified SSL context; six invalid TLS/origin/port/ROI/key no-DB/no-POST regressions authored NOT RUN. Full source/ICC fixture authority, report composition and native acceptance remain open; no tests/builds/validators/migrations executed.


## V32 independently pinned preview fixture (2026-10-11)

V32 preview: added independent fixture descriptor/pinned executor binding restore run/database/build/recovery/replay, tenant/source/actor/project, request and secret-key hashes, API/storage origins, source geometry/ICC digest and timeout before lifecycle IO. Credentials/raw params/ICC bytes excluded. Ten pin/privacy/substitution cases authored NOT RUN. Pin is not source authority or acceptance: physical source/ICC installation and complete legacy report remain open. No tests/builds/validators/migrations executed.


## V32 preview source authority (2026-10-11)

V32 preview: added exact privileged readonly source version/project/master SHA catalogue with ready verified bounded versioned WorkingImage; physical SHA/JCS/real codec/float32 geometry and repeated catalogue. Pinned executor now verifies physical source root and rejects fixture geometry mismatch before preview lifecycle. Five SQL authority/scope/absence/transaction scenarios authored NOT RUN; pin tests updated. Nested source arrays/ICC registry authority, physical source regression matrix and full legacy report remain open. No tests/builds/validators/migrations executed.


## V32 physical source regression coverage (2026-10-11)

V32 preview source: added physical reader defense-in-depth1MiB/sourceSHA precheck and nonzero/nonroot/nonaliased pixels/alpha refs. Ten real WorkingImage codec cases for canonical/hash/truncation/catalogue change/dtype/ref aliases/oversize authored NOT RUN, SQL/object adapters mocked. Prevents invalid source fixture before preview mutation; nested pixel physical qualification and ICC installation/full report remain open. No tests/builds/validators/migrations executed.


## V32 installed preview ICC authority (2026-10-11)

V32 preview: pinned executor now requires explicit production ICCRegistry; resolves/validates source WorkingImage profile and srgb_v1, independently hashes bytes, requires exact source ICC SHA and target ICC bytes before preview mutation. Six real ICC-file/production-registry cases for source/target substitution, corrupt file, lock mismatch and missing profile authored NOT RUN. Pin tests retain mocked registry qualification explicitly. Nested source arrays, full legacy report/native acceptance remain open. No tests/builds/validators/migrations executed.


## V32 preview nested source arrays (2026-10-11)

V32 preview source: added canonical root-producer array catalogue/current attempt/fence/output membership (alpha exact declared-input reuse only), tenant/version/role/SHA readiness and physical streamed NPY verification via existing production-aligned validator, catalogue recheck. Physical WorkingImage source verifier now includes RGB and optional uint16/default or float32 alpha arrays. Six authority/scope/change/physical failure cases authored NOT RUN; source-codec tests explicitly mock array verifier. Actual ingest lineage compatibility, alpha dtype fixture pin, native composed source/report qualification remain open; no tests/builds/validators/migrations executed.


## V32 actual ingest alpha compatibility (2026-10-11)

V32 source audit: actual asset adapters publish alpha as output_role alpha (uint16), not working_alpha. Corrected preview and legacy Apply array catalogue allowlist to accept alpha only for logical working_alpha, retaining canonical producer/current attempt/fence/exact-input authority. Preview independent descriptor now pins alpha_dtype and forwards it into physical source verification; changed descriptor requires fresh independent pin. Added dtype substitution case (11 pin modes) and SQL role-binding assertions, authored NOT RUN. Actual native ingest/Apply acceptance and full report remain open; no tests/builds/validators/migrations executed.


## V32 native Apply lifecycle (2026-10-11)

V32 legacy Apply: added native installed lifecycle with readonly exact-DB fresh Run.Apply key preflight, one POST/no retry, bounded adopted-result polling, complete WorkingImage/arrays/master quality physical verifier, physical-result identity and final catalogue recheck. Overall1..300s guard covers IO. Seven orchestration/authority/existing-key/unknown-outcome/physical/late-change/poll modes authored NOT RUN. Independent Apply installation pin, output delivery/full legacy report and native qualification remain open; no tests/builds/validators/migrations executed.


## V32 independently pinned Apply fixture (2026-10-11)

V32 legacy Apply: added independent canonical fixture descriptor and pinned executor binding restore run/database/build/recovery/replay, tenant/project/actor/source, collection/item/revision/style, canonical request/key hashes, alpha dtype and timeout. Rejects mismatched operator pin before IO; credentials excluded, no report/writer activation. Ten fixture substitution/privacy scenarios authored NOT RUN. Apply output API delivery and combined preview/Apply independently revalidated report/native qualification remain open; no tests/builds/validators/migrations executed.


## V32 Apply pure installation preflight (2026-10-11)

V32 Apply: extracted shared pure native target/TLS/origin/port/request validator and invokes it before lifecycle database preflight/POST. Seven invalid TLS/http/port/revision/key/header/style no-DB/no-POST regressions authored NOT RUN; transport reuses same validator. API output delivery and complete legacy restore report remain open; no tests/builds/validators/migrations executed.


## V32 adopted Apply API download (2026-10-11)

V32 legacy Apply: added native runs outputs API/adopted WorkingImage signed download verifier, bounded256-item API envelope/exact unique selected artifact, strict role/SHA/size/JSON media/expiry, installed storage origin/versionId, no bearer forwarding/redirect/retry, streamed exact-byte SHA and URL-free evidence. Eight transport/substitution scenarios authored NOT RUN. This proves document delivery only, not encoded export; full lifecycle integration/catalogue recheck and combined report/native acceptance remain open. No tests/builds/validators/migrations executed.


## V32 Apply delivery integration (2026-10-11)

V32 legacy Apply: output WorkingImage API delivery is now mandatory in installed lifecycle before final catalogue recheck and covered by overall deadline. API/storage installation is validated before POST; independent Apply descriptor now pins object origin and forwards exact download target, requiring refreshed independent fixture SHA. Added download failure and storage-origin pin substitution regressions; lifecycle8/pin11 scenarios authored NOT RUN. Complete preview+Apply report and native/encoded-export acceptance remain open; no tests/builds/validators/migrations executed.


## V32 combined legacy observations (2026-10-11)

V32 legacy: added combined independently pinned preview+Apply observations runner, same tenant/origin/auth/source binding, distinct request keys, pure Apply preflight before preview mutation, ordered pinned lifecycles and final re-observation of both catalogues. Seven orchestration/preflight/partial/late-change cases authored NOT RUN. Deliberately no all-true legacy acceptance report: adopted WorkingImage document delivery does not yet prove encoded final export. Encoded export/full report/native qualification remain open; no tests/builds/validators/migrations executed.


## V32 native encoded export admission (2026-10-11)

V32 encoded export: inspected actual Download.Start contract and added bounded TLS native download-jobs POST with exact selection snapshot/preset, strict202 queued job ticket, nonzero UUID, no redirects/retries and explicit unknown outcome. Seven transport/scope/ticket cases authored NOT RUN. Backend route omits replay header, so fresh SQL key preflight/saved receipt binding is mandatory before acceptance; export selection creation/registration/completion/volume intent/encoded bytes/full report remain open. No tests/builds/validators/migrations executed.


## V32 export selection preflight (2026-10-11)

V32 encoded export: added readonly exact-DB fresh Download.Start key preflight and one-item selection/preset authority bound to adopted Apply collection/run/item/version/master artifact SHA, actor/project/snapshot digest/expiry, current item version, exact count and preset revision/canonical config SHA. Six authority/key/selection/preset scenarios authored NOT RUN. Selection creation/digest recomputation, saved202 registration, completion/volume intent/encoded bytes and full report remain open; no tests/builds/validators/migrations executed.


## V32 registered export admission (2026-10-11)

V32 encoded export: added exact saved Download.Start fingerprint/202/resource/canonical typed response binding and download job/run/snapshot/project/principal registration with exact frozen preset revision/config SHA and pipeline3 config. HTTP re-encodes Accepted, so saved canonical response is bound semantically rather than falsely equating wire SHA. Six authority/receipt/registration/preset/config cases authored NOT RUN. Completion/intent/encoded bytes/full report remain open; no tests/builds/validators/migrations executed.


## V32 encoded export result catalogue (2026-10-11)

V32 encoded export: added ready one-item job encoded-artifact observer after registration recheck, completed sealed noncancelled run/job expiry, exact adopted asset version/item ordinal/volume, canonical succeeded export.encode producer/current attempt/fence/output membership, ready bounded versioned encoded image and safe entry name. Six catalogue/authority/scope/registration scenarios authored NOT RUN. Cached-export source/preset authority, volume manifest/intent/physical encoded bytes and full report remain open; no tests/builds/validators/migrations executed.


## V32 export current plan authority (2026-10-11)

V32 encoded export: traced authoritative download_encode_outcomes/CompileDownload: current download plan task owns encoded_image (not arbitrary prior producer). Tightened result query to exact current run/ordinal plan/task, snapshot source ref/version, task declared inputs, plan image and frozen preset config equality excluding independently qualified ICC SHA; encoded_image role required. SQL unit assertions extended, NOT RUN. Cached reuse cannot bypass this canonical plan-task route; physical ICC/plan SHA/result verification, volumes/intents/full report remain open. No tests/builds/validators/migrations executed.


## V32 physical encoded export (2026-10-11)

V32 encoded export: added exact-version64KiB streamed scratch-file SHA/size verification, explicit installed scratch root/cleanup, independent typed EncodeParams and bounded geometry/alpha, production _verify JPEG/PNG/TIFF and extra exact JPEG/PNG container/header geometry, final catalogue recheck. Six real JPEG/PNG/ICC/geometry/hash/change scenarios authored NOT RUN (catalogue/object adapters mocked). Plan params/geometry authority installation, volume intent/delivery/full report remain open; no tests/builds/validators/migrations executed.


## V32 registered export parameter authority (2026-10-11)

V32 encoded export: bound physical verifier EncodeParams to exact registered current plan/task/encoded producer, declared inputs, canonical plan SHA and ready Params artifact digest/reference; strict typed semantics with Go optional-null omission; rechecks plan after physical IO. Six authority/missing/hash/parameter/ref cases authored NOT RUN; physical tests explicitly mock plan authority. Physical Params document bytes/geometry authority, volumes/intents/full report remain open; no tests/builds/validators/migrations executed.


## V32 export source-derived geometry (2026-10-11)

V32 encoded export: physical verifier now requires adopted physical WorkingImage evidence, exact asset version/root artifact+SHA/dtype and production _dimensions resize/no-upscale derivation; expected alpha follows actual source alpha and preserve/matte. Seven source/version/geometry/resize/no-upscale/alpha scenarios authored NOT RUN; file-decoder tests explicitly mock geometry authority. Full revalidation of source at combined coordinator, physical Params bytes, delivery volume/intent/report remain open; no tests/builds/validators/migrations executed.


## V32 physical export Params (2026-10-11)

V32 encoded export: registered Params catalogue now includes bounded ready versioned JSON object with expiry/tenant scope. Added exact-version streamed physical Params SHA/size and byte-for-byte canonical plan equality, repeated catalogue; physical encoded verifier requires this before image IO. Five corrupt/truncated/change/oversize physical-adapter scenarios authored NOT RUN; SQL and decoder fixtures updated. Native Params publication compatibility, volume/intent/full report remain open; no tests/builds/validators/migrations executed.


## V32 registered delivery volume (2026-10-11)

V32 encoded delivery: added ready one-item/one-volume catalogue, exact job/run/encoded item/expiry/counts; single requires exact encoded object identity and no manifest; ZIP+manifest require canonical same archive.build task/current attempt/fence/output membership and ready versioned bounded scoped objects. Six single/ZIP/authority/missing/scope/alias scenarios authored NOT RUN. Physical archive/manifest, intent delivery and full report/native qualification remain open; no tests/builds/validators/migrations executed.


## V32 physical archive manifest (2026-10-11)

V32 archive delivery: added real strict ArchiveManifest semantic/canonical validator and exact-version bounded physical reader with repeated volume catalogue. Binds snapshot/digest/volume/archive SHA+size and sole ordinal0 safe named encoded artifact/SHA/bytes. Seven real-codec substitution scenarios authored NOT RUN. ZIP physical entry verification, intent delivery and complete report/native qualification remain open; no tests/builds/validators/migrations executed.


## V32 physical delivery ZIP (2026-10-11)

V32 archive delivery: added exact-version64KiB streamed ZIP SHA/size into explicit scratch root, complete production cancellable _verify_archive exact names/CRC/encoded entry SHA+size/internal manifest payload, final volume catalogue and cleanup. Six real ZIP extra-entry/substitution/internal-manifest/hash/change scenarios authored NOT RUN; catalogue/object adapters mocked. Scratch resource admission, intent API delivery/complete report/native qualification remain open; no tests/builds/validators/migrations executed.


## V32 owner intent admission (2026-10-11)

V32 delivery: added bounded native owner download-intent POST exact job/volume, strict201 closed token/expiry envelope,43-char raw URL-safe MAC token, expiry check, no redirect/retry and explicit unknown outcome. Token and authorization excluded from repr. Six transport/expiry/token/privacy cases authored NOT RUN. Backend omits replay header: SQL fresh key/saved intent binding, redeem/byte delivery and full report remain open; no tests/builds/validators/migrations executed.


## V32 owner intent registered MAC authority (2026-10-11)

V32 owner intent: added readonly exact-DB Download.Intent saved receipt binding, native job+volume fingerprint, length-prefixed HMAC bearer/token-hash verification, exact owner/job/ready volume/artifact/expiry binding and secret-free evidence. Added HTTP request digest binding and malformed receipt handling. Nine SQL adapter scenarios plus independent UTF-8/length-prefix MAC checks authored NOT RUN. Fresh-key preflight, redeem/delivery coordinator, native qualification and full report remain open. No tests/builds/validators/migrations executed.


## V32 fresh owner intent lifecycle (2026-10-11)

V32 owner intent: added create_fixture_owner_intent orchestration with pure TLS/target validation before SQL, exact privileged restore DB readonly fresh Download.Intent key preflight, one native POST/no retries, mandatory saved receipt/MAC registration and repeated installed-fixture guards. Secret ticket excluded from result repr. Seven orchestration scenarios authored NOT RUN: valid, existing key, wrong authority, unknown POST outcome, registration refusal, changed fixture, invalid TLS before SQL. Freshness requires exclusive independently pinned fixture key ownership; SQL observation alone is not a concurrency lock. Redeem/physical final delivery, combined coordinator/report and native qualification remain open. No tests/builds/validators/migrations executed.


## V32 redeem exact final volume (2026-10-11)

V32 final delivery: added native public intent redeem followed by bounded64KiB streaming of exact path-style object storage bucket/key/versionId. Strict closed API envelope, live expiry, exact size/SHA, installed HTTPS origin, no redirects/retries/owner bearer forwarding, secret-free evidence and connection cleanup. Twelve transport/substitution/truncation/oversize/unknown-outcome scenarios authored NOT RUN. Path-style MinIO installation is explicit; virtual-host storage requires a separately pinned addressing contract. Combined coordinator must revalidate intent and volume catalogue after delivery; complete acceptance report/native qualification remain open. No tests/builds/validators/migrations executed.


## V32 consumed owner volume lifecycle (2026-10-11)

V32 owner volume delivery: inspected actual redeem SQL one-shot consumed_at behavior. Intent catalogue now requires explicit volume kind and exact unconsumed/default or consumed/final state. Added run_fixture_volume_delivery: pure origin/TLS checks, bounded1..60s guard, ready volume lookup, fresh intent admission, pre-redeem volume equality, streamed redeem delivery, consumed intent MAC/receipt recheck and final volume equality. No report or encoded qualification credit. Seven orchestration cases and catalogue matrix across both consumed states authored NOT RUN. Combined export coordinator/independent pin/physical media qualification/report/native execution remain open. No tests/builds/validators/migrations executed.


## V32 combined encoded export observations (2026-10-11)

V32 export lifecycle: combined independently re-read physical Apply source, native export admission/preflight, bounded ready result+volume polling, physical Params/encoded verifier, optional production ZIP verifier, fresh owner intent/redeem delivery and final encoded/source re-observation under overall1..300s guard. One export POST/no retry; only explicit readiness exceptions are polled. Seven orchestration scenarios authored NOT RUN. Observations only: independent combined installation pin, scratch/decode resource admission, selection creation/digest authority, full report and native qualification remain open. No tests/builds/validators/migrations executed.


## V32 pinned complete export fixture (2026-10-11)

V32 export installation: source review found geometry/EncodeParams/scratch validation after mutation; moved typed Params/alpha dtype, exact source geometry and scratch root checks before native export POST; unknown delivery kinds fail explicitly. Expanded orchestration to9 cases. Added independent complete export fixture descriptor/pinned executor binding replay/build/DB/recovery, Apply fixture+accepted ticket, selection snapshot/digest, preset revision/hash, exact EncodeParams hash, export/intent key hashes, scratch root and path-style object addressing. Eight substitution/privacy modes authored NOT RUN, pin mismatch before IO. Resource admission, selection creation/digest recomputation, complete legacy acceptance report and native qualification remain open. No tests/builds/validators/migrations executed.


## V32 independent selection digest (2026-10-11)

V32 selection digest: inspected actual collection.CreateSelection canonical selectionEntry shape. Added independent one-item snapshot digest recomputation from exact adopted IDs/master SHA and strict payload position/captured_at/camera_hash with explicit nullable fields. SQL preflight reads payload in same readonly transaction; mismatch fails before export POST. Added post-delivery authority recheck without repeating fresh-key requirement. Expanded preflight matrix across admission/final modes plus malformed payload and metadata digest sensitivity tests; orchestration expectation updated. Authored NOT RUN. Native selection creation, resource admission, full report/native qualification remain open. No tests/builds/validators/migrations executed.


## V32 native one-item selection (2026-10-11)

V32 native selection: inspected actual collection.CreateSelection and HTTP route. Added typed one-item selection target/ticket, pure TLS/origin/key/auth/revision validator and bounded native POST collection selection-snapshots with exact normalized revision-string/selected/item_ids body, strict201 closed snapshot/digest/count/expiry response, one attempt/no redirect/retry/unknown-outcome handling and connection cleanup. Nine native transport/invalid contract cases authored NOT RUN. Route omits replay header; SQL fresh Selection.Create key and exact saved response/registered snapshot binding are mandatory before integration or acceptance. Resource admission/combined report/native qualification remain open. No tests/builds/validators/migrations executed.


## V32 registered native selection (2026-10-11)

V32 selection catalogue: added readonly exact privileged DB observer binding Selection.Create fingerprint/status/resource and canonical saved response bytes to native HTTP response SHA, strict snapshot ticket/time/count, exact actor/collection/source revision, sole adopted Apply item/version/master artifact SHA, current collection revision/run, live verified artifact and independent payload digest recomputation. Eight receipt/authority/snapshot/payload/expiry/scope adapter cases authored NOT RUN. Native fresh-key selection lifecycle and complete export integration/resource admission/full report remain open. No tests/builds/validators/migrations executed.


## V32 fresh native selection lifecycle (2026-10-11)

V32 native selection lifecycle: added create_fixture_selection with pure TLS/target validation before SQL, exact privileged restore DB readonly fresh Selection.Create key preflight, current Apply collection revision/run/item/version/master SHA and verified live source checks, one POST/no retry, mandatory saved response/snapshot/digest observer and repeated fixture/cancellation guards. Seven orchestration cases authored NOT RUN. Freshness requires exclusive fixture key ownership; readonly observation is not a concurrency lock. Combined selection→export independently pinned coordinator/resource admission/full report/native qualification remain open. No tests/builds/validators/migrations executed.


## V32 independently pinned selection to export (2026-10-11)

V32 selection→export: added independently pinned descriptor/executor covering fresh selection request and key, Apply ticket/source installation, expected snapshot digest, frozen preset/EncodeParams, export+intent keys, object addressing/scratch/time/alpha dtype. Server-generated snapshot UUID is taken only from registered selection response, never predicted. Pure target/TLS/scratch/source geometry preflight before first POST; physical Apply source→native selection→digest equality→full export/delivery→final selection receipt/catalogue recheck under overall deadline. Five orchestration/privacy/dynamic-ID/refusal cases authored NOT RUN. Resource admission and full legacy acceptance report/native qualification remain open. No tests/builds/validators/migrations executed.


## V32 shared scratch reservation (2026-10-11)

V32 physical scratch: replaced unreserved export/ZIP temporary files with existing descriptor-owned ScratchSpace and exact object-size Reservation, min64MiB free-disk headroom, safe exclusive planned filenames, post-write reservation.validate_file and automatic reservation/attempt cleanup inside outer temporary directory. No new competing disk budget mechanism. Added disk-exhaustion refusal-before-object-read cases to real encoded/ZIP adapter matrices (7 each), authored NOT RUN. This is local scratch admission only; global restore resource scheduling and decode RAM budget/native qualification/full report remain open. No tests/builds/validators/migrations executed.


## V32 canonical export RAM profile (2026-10-11)

V32 codec RAM admission: added exact privileged readonly catalogue binding to current canonical export plan/task CPU profile, succeeded attempt/fence/encoded output membership. Physical encoded verifier now applies existing production admit_presentation estimate before object read/decode and repeats profile hash after physical verification; CPU-only, no GPU consumption. Eight profile/authority/missing/budget/boolean/compatibility/alpha cases authored NOT RUN; physical codec fixtures explicitly mock authority. Admission is not OS RAM reservation or global concurrent scheduling. Global restore resource admission/full report/native qualification remain open. No tests/builds/validators/migrations executed.


## Production download export dynamic RAM (2026-10-11)

V30/V32 production resource fix: source audit found PrepareDownloadExportTask hardcoded2GiB RAM despite worker presentation estimate scaling with source/target/alpha. Added shared encodeDimensions (production ties-to-even/no-upscale) and encodeRAMBytes; delivery encode profile reuses identical helpers. Download export now validates frozen encoder preset, derives bounded target geometry, reserves computed RAM and target-size output/scratch instead of fixed RAM/source-size output. Added large24MP/alpha formula and prepared-task regression tests authored NOT RUN. Scheduler existing capacity admission remains authoritative; no quality/downscale fallback introduced. Native qualification/global restore coordination remain open. No tests/builds/typecheck/validators/gofmt/migrations executed.


## Production encode source and upscale accounting (2026-10-11)

Production download export admission follow-up: tied prepared task to operation version1, exact frozen source manifest/Params.image/pixels/optional alpha input sequence and positive bounded float32 source geometry. PixelCount now counts derived target pixels, including upscale, consistent with dispatcher output pixel limit. Added ties-to-even/no-upscale dimensions, source/version/negative geometry substitutions and upscale RAM/output/pixel accounting unit cases, authored NOT RUN. Existing quality algorithms unchanged; incorrect authority fails instead of estimating another source. No tests/builds/typechecks/validators/gofmt/migrations executed. Native scheduler/worker resource qualification and full requirements audit remain open.


## Export geometry admission before registration (2026-10-11)

Production export compiler: moved derived output geometry/200MP admission into CompileDownloadExports before plan registration and Params publication, retaining shared worker-compatible rounding/no-upscale. Bound each source dimension before product to prevent int64 overflow bypass. Added oversized upscale/source-overflow/source-limit/no-upscale compiler cases and registration refusal case authored NOT RUN. Output/scratch worker publication inspected; existing encoded manifest fits reserved overhead, no codec/quality changes. No tests/builds/typechecks/validators/gofmt/migrations executed. Full runtime qualification and requirement completion audit remain open.


## Export exact canonical Params integrity (2026-10-11)

Production export task integrity: preparation now requires bounded exact canonical encodeParams reserialization (rejects unknown/duplicate/noncanonical fields even with matching publication SHA), valid ICC digest, valid distinct manifest/pixel/alpha refs and exact working color profile/encoding. Added six unknown/duplicate/ICC/whitespace/color/alias regression scenarios authored NOT RUN. Prevents resource estimation from admitting unsupported or ambiguous task semantics; no imaging algorithm changes. No tests/builds/typechecks/validators/gofmt/migrations executed. Full deployment/runtime qualification and whole-product completion audit remain open.


## V29/V30 current registry reconciliation (2026-10-11)

Registry reconciliation from current source: main.go already installs prepared pinned interchange export factory through buildInterchangeCreateWithFactory; previous CreateInterchange note claiming Export never supplied was stale. Multi panorama/focus client preparation/preview/review/Apply/recovery components also exist; removed blanket UI absence claim while retaining partial status and unresolved routed authoring/full semantic/native qualification. Updated evidence paths, no counts/whole-product percentage or completion credit changed. Next work must inspect actual remaining production authority/route gaps instead of recreating existing code. No tests/builds/validators/migrations executed.


## V29 acknowledged recovery collection binding (2026-10-11)

V29 routed authoring/recovery source audit: actual app/collections/[id]/multi.tsx already wires preparation/preview/review/Apply route. Fixed concrete recovery gap: successful current collection items are now retained independently when producer capabilities fail, preserving exact reference-version collection CAS for an acknowledged saved result. Fresh authoring still requires both capabilities and collection pages. Added DOM regression with review boundary stub checking retained item/version/Apply enablement and no new submission, authored NOT RUN. Server repeats review/Apply permissions; no cached capability authority introduced. No tests/builds/typechecks/validators/migrations executed. Full routed/native semantic acceptance remains open.


## V29 route scoped review and Apply (2026-10-11)

V29 route scope boundary: multi collection screen now gates review and collection Apply binding on current tenant/collection plus saved project/operation, and keys review instance by origin/tenant/collection/operation/intent. Old acknowledged state cannot expose review with new route collection props while load/reset is pending. Added route-change pending-load DOM regression and observable review boundary stub, authored NOT RUN. Existing saved project journal recovery remains supported once current collection authority is loaded. No tests/builds/typechecks/validators/migrations executed. Native/server/full module acceptance remains open.


## V29 pending review observation polling (2026-10-11)

V29 multi review lifecycle: added serialized2s read-only polling only after confirmed pending observation. At most one request/timer; available state stops polling, errors require explicit manual retry, scope replacement/manual refresh/unmount clears timer and aborts previous read. Admission/Apply mutations never auto-retried. Added fake-timer DOM cases for pending→inflight(no overlap)→available stop, unmount cleanup and no automatic error retry, authored NOT RUN. Existing manual refresh and durable Apply recovery retained. No tests/builds/typechecks/validators/migrations executed. Full browser/native/module acceptance remains open.


## V29 explicit terminal multi observations (2026-10-11)

V29 terminal multi workflow states: native and replacement readers now expose failed/cancelled as explicit terminal observations, cancelling as pending; neither fetches proposal before reviewable state. Both review views render localized EN/RU/BG failure/cancel/cancelling labels; main pending polling stops at terminal, durable Apply recovery remains separate. Expanded result state matrix and terminal no-poll DOM cases authored NOT RUN. Unknown/completed_with_errors still refused until defined by server contract. No tests/builds/typechecks/validators/migrations executed; full browser/native/module acceptance open.


## V29 replacement pending and Apply recovery (2026-10-11)

V29 replacement review: added serialized2s current-scope discovery/result polling only for confirmed pending state without applied acknowledgement; stops on errors/terminal/available/Apply acknowledgement/unmount. Removed refresh reset of recovered applied version; scope wrapper remount still resets different identities. Added pending cancellation→terminal polling stop and recovered Apply acknowledgement retention across manual refresh fake-timer DOM tests authored NOT RUN. No admission/Apply mutation auto-retry. No tests/builds/typechecks/validators/migrations executed; full browser/native/module acceptance remains open.


### 2026-10-11 — V29 recovery scope ownership

V29 Apply recovery: reset busy/lock on new scope and fence resume/clear finally by current AbortController identity. Late old completion cannot unlock a new request; aborted clear cannot remove new journal UI. MultiQualityView remounts Apply on collection selection CAS changes. Added deferred old/new resume and old clear/new resume DOM regressions, authored NOT RUN. No tests/builds/typechecks/validators/migrations executed; full module/native acceptance remains open.


### 2026-10-11 — V29 Apply frozen scope

V29 Apply ownership: MultiApplyView now owns a keyed inner scope per API identity and canonical origin/review/intent/collection selection. A changed API or frozen authority remounts confirmations, saved attempt and cancellation controller; equivalent cloned props retain confirmation. Added pending identity lookup/API replacement (aborted old lookup cannot dispatch), canonical-equivalent props and changed selection reset DOM cases, authored NOT RUN. No tests/builds/typechecks/validators/migrations executed. Full V29 normative/native acceptance remains open.


### 2026-10-11 — V29 transparent line evidence

V29 automatic perspective source audit: line detection producer already exists; no blanket missing-detector claim. Fixed sparse33-point alpha corridor check to bounded half-pixel sampling across full2px square corridor, rejecting narrow/diagonal transparent gaps. Reject out-of-proxy detector endpoints before sampling/normalization instead of clamping them into evidence. Added float32/uint16 alpha narrow-gap/diagonal/source-immutability and out-of-bounds detector cases, authored NOT RUN. Original HDR pixels unchanged; native optical lifecycle/qualification remains open. No tests/builds/validators/migrations executed.


### 2026-10-11 — V29 geometry admission

V29 geometry admission: manual perspective now enforces global200MP output cap independently of caller max_pixels before solving homography, matching auto/warp limits. Coverage rejects oversized source before matrix inverse; homography checker refuses empty/underspecified boundary domains. Added thin-source huge-output admission-before-solve, source-limit-before-inverse and malformed-domain tests authored NOT RUN. No image interpolation/quality algorithm changed. No tests/builds/validators/migrations executed; full optical native/module acceptance open.


### 2026-10-11 — V29 focus preflight

V29 focus orchestration: validate explicit reject/review motion policy and finite nonnegative normalized three-component working-ICC luma before scratch reservation, motion processing or pyramid allocation. Own admitted coefficients as immutable tuple before callbacks. Added six refusal-before-resource scenarios for unsupported policy, malformed/negative/nonfinite/unnormalized/non-numeric luma, authored NOT RUN. Focus blending/quality unchanged. No tests/builds/validators/migrations executed; full native optical/module acceptance remains open.


### 2026-10-11 — V31 explicit policy revisions

V31 signer deployment: reconciled loader identity with InstalledSigningOperator and SigningPolicy version contract. Explicit revisions of one policy may coexist under exact same SDK/key/certificate/trust installation; duplicate(id,revision), conflicting bytes and changed installation pins remain rejected. No implicit latest selection or relaxed native authority. Added explicit revision coexistence/order, conflicting same revision and changed-pin cases authored NOT RUN. Dedicated signer composition already exists; deployed native SDK/key/TSA qualification remains open and missing-runtime registry credit unchanged. No tests/builds/validators/migrations executed.


### 2026-10-11 — V31 callback lifetime

V31 native callback lifetime: signing task scopes now explicitly revoke captured guards on exit, including callbacks on foreign threads, and recheck cancellation before returning successful scope results. A retained native callback cannot outlive its originating scope even if external task guard remains permissive; nested scope exit leaves parent active. Added stale foreign callback, cancellation-during-invocation and nested lifetime cases; adjusted existing foreign-thread cancellation test to exercise active scope. Authored NOT RUN. SDK/key/TSA deployment qualification still open; no tests/builds/validators/migrations executed.


### 2026-10-11 — V31 signer resource parity

V31 signer worker resource parity: require_signer_profile now repeats backend CompileSigningTask envelope before source download/SDK: exact safe integer RAM/scratch/output/pixel estimates, pixel_count0, max_input_json_bytes1..128MiB, RAM>=JSON budget, scratch>=1GiB, output>=528MiB, VRAM0. Measured/native authority remains mandatory and may require larger budgets. Updated adapter fixture profiles and added12 malformed/undersized/overflow/JSON-limit cases authored NOT RUN. No tests/builds/validators/migrations executed; native deployment/full V31 acceptance open.


### 2026-10-11 — V31 upload receipt binding

V31 upload receipt binding: signer adapter compares actual staged signed-export/manifest refs against independently verified bridge SHA receipts, then repeats immutable source metadata after both uploads before returning SignedResult. Changed bytes between verification/upload or changed source version during upload fail without result root; already uploaded orphans remain uncommitted under existing fenced commit/cleanup. Added three adapter mutation scenarios, authored NOT RUN. No tests/builds/validators/migrations executed; native signer deployment/full acceptance remain open.


### 2026-10-11 — V31 bounded SDK verification

V31 SDK result verification: active manifest now independently respects8MiB canonical document cap in addition to validation report. Added signing-task cancellation checks before/after native Reader responses, through ingredient traversal and before receipt digest return. Existing trusted status, hard binding, timestamp/revocation and exact lineage checks retained. Added oversized active manifest and cancellation on validation/manifest reader cases authored NOT RUN. No tests/builds/validators/migrations executed; full native deployment/module acceptance open.


### 2026-10-11 — V30 building observation

V30 delivery client lifecycle: building package screen now observes native package GET every2s after completed read, with one request/timer and no automatic publish mutation. Polling pauses during item/inspection/publication activity and stops on ready/review/failed/cancelled, read error, saved publication retry or unmount. Existing explicit refresh, inspection confirmation and durable publication CAS/key unchanged. Added serialized deferred read→ready/no POST, read error stop and unmount timer cleanup DOM cases authored NOT RUN. No tests/builds/typechecks/validators/migrations executed; full delivery/native/module acceptance open.


### 2026-10-11 — V30 journal screen scope

V30 delivery publication journal boundary: added synchronous assertScope(tenant,project,package) including stale generation epoch check; screen invokes before journal read and any package HTTP. Wrong injected journal cannot restore another package/project publication intent or clear its receipt. Added store exact/scope substitution and three DOM tenant/project/package refusal-before-storage/HTTP cases authored NOT RUN. Original durable key/CAS recovery retained. No tests/builds/typechecks/validators/migrations executed; full native/module acceptance open.


### 2026-10-11 — V30 creation journal scope

V30 delivery creation journal boundary: added synchronous tenant/project/snapshot installation assertion with stale epoch fencing, required by screen before journal read/ticket recovery and again before submit journal IO. Wrong journal cannot navigate using another scope saved ticket or post another frozen creation request. Added exact/substituted scope store case and three DOM refusal-before-storage/HTTP/navigation cases, authored NOT RUN. Existing exact request/key recovery and accepted ticket retention unchanged. No tests/builds/typechecks/validators/migrations executed; full delivery native/module acceptance open.


### 2026-10-11 — V30 interchange client ownership

V30 interchange API ownership: InterchangeJobScreen now keys entire scoped lifecycle by API client identity as well as tenant/project/job. Replacing client aborts active mutation/poll and resets choices, saving flags and prior idempotency attempts; old responses cannot refresh or alter new connection. Added deferred old decision POST→client replacement→late valid receipt DOM regression, authored NOT RUN. No tests/builds/typechecks/validators/migrations executed; durable interchange review journaling/full native lifecycle acceptance remain open.


### 2026-10-11 — V30 interchange review journal

V30 interchange durable review foundation: added bounded8MiB canonical-owned journal for decide/close operations, frozen scoped inspection/report/revision/decisions/key, validated receipts, strict conflict and no receipt regression, receipt-only clear, principal/scope separation, cross-tab lock and logout generation fencing. Wired cleanup into auth logout. Added decide/close remount ownership/acknowledged clear, changed request/receipt refusals and logout/scope isolation unit cases authored NOT RUN. Screen and route recovery integration still required; no end-to-end durability credit. No tests/builds/typechecks/validators/migrations executed; full V30 native acceptance open.


### 2026-10-11 — V30 interchange review recovery integration

V30 interchange review journal integration: production route now resolves current owner before creating origin/user/tenant/project/workflow scoped journal, remounts on API identity changes. Screen recovers saved decide/close operations before authoring, disables replacement choices/actions while unresolved, persists intent before POST and validated receipt before clear. Explicit retry reuses saved inspection/revision/report/decisions/key; acknowledged receipt only retries cleanup, never POST. Added remount lost decision response exact replay and acknowledged cleanup/no mutation DOM cases authored NOT RUN. No tests/builds/typechecks/validators/migrations executed; close/revocation/concurrency/route qualification and full V30 acceptance remain open.


### 2026-10-11 — V30 recovery access authority

V30 interchange recovery authority: restored mutation now requires successful current native snapshot access, not journal availability alone. Access-denied401/403/404/410 clears displayed review/choices, disables replay and stops poll continuation until explicit refresh; original durable unresolved request remains. Added lost close-review response remount/exact CAS+key replay and deferred access read→revoked decision→no second POST/no displayed target cases authored NOT RUN. No tests/builds/typechecks/validators/migrations executed; concurrency/route/native/full V30 acceptance open.


### 2026-10-11 — V30 concurrent receipt reconciliation

V30 interchange concurrent receipt reconciliation: refresh/poll now re-read durable review journal, so another tab acknowledged receipt is adopted and cleaned without stale POST. Journal delivery is fenced against mutation generation before applying recovery state. Added storage-failure-before-POST, other-tab receipt refresh/no POST and failed cleanup→receipt retained→cleanup-only retry DOM cases authored NOT RUN. No tests/builds/typechecks/validators/migrations executed; route and full native/module acceptance remain open.


### 2026-10-11 — V30 interchange route ownership

V30 interchange route authority: validate tenant/project/workflow route before identity HTTP; invalid routes cannot create journal or reach screen. Added production-route DOM boundary tests for deferred current identity/principal-scoped key, invalid route refusal before IO, old tenant identity cancellation/late response and API replacement hiding prior journal until new identity. Tests authored NOT RUN; boundary stub does not establish full screen/native acceptance. No tests/builds/typechecks/validators/migrations executed; full V30 acceptance open.


### 2026-10-11 — V30 concurrent journal invariants

V30 interchange journal concurrency unit coverage: authored two concurrent distinct intents (one admission only), old acknowledgement cannot clear/overwrite newer operation, persisted foreign-realm generation change fences read/write/clear, and origin-specific logout isolation scenarios. Existing shared browser-lock protocol retained; tests use controlled lock adapter and do not prove native Web Locks implementation. Added cheap character cap before bounded UTF8 size allocation. No tests/builds/typechecks/validators/migrations executed; full browser/native V30 acceptance open.


### 2026-10-11 — V30 recovery localization

V30 interchange recovery localization: added EN/RU/BG recovery pending/retry/unknown outcome/access denied and route restore/loading messages. Recovery error state stores translation keys, so language changes update active alerts without replacing journal/request/key. Added locale switch RU→BG preserved original durable attempt/no POST DOM case authored NOT RUN. Existing broader interchange screen localization is not claimed complete. No tests/builds/typechecks/validators/migrations executed; full V30 native/module acceptance open.


### 2026-10-11 — V30 interchange create client

V30 actual client creation gap: current photographer source had interchange read/review/download but no create API. Added strict owned import/export tagged union matching native CreateRequest, compatible closed fields, explicit supported format IDs, nonzero refs/SHA/key validation and queued ticket validation. OwnerAPI.createInterchange scopes native POST and checks cancellation before/after; no auto retry. Added union substitution/alias ownership/format/key/ticket/transport/cancellation unit cases authored NOT RUN. Production create screen, durable attempt and entry routes still required; unsupported installed formats remain server-gated. No tests/builds/typechecks/validators/migrations executed; full V30 acceptance open.


### 2026-10-11 — V30 interchange create durability

V30 interchange creation durability: added import/export create attempt store pinned to complete canonical request plus origin/principal/tenant, strict original key/body/ticket conflict and no ticket regression, acknowledged-only clear, browser journal lock and persisted logout generation fencing. Auth logout clears create attempts. Added import/export remount/ticket progression, owned constructor scope/source SHA/direction/format/principal separation and origin logout unit cases authored NOT RUN. Create screen and user entry routes still open; no native/full module credit. No tests/builds/typechecks/validators/migrations executed.


### 2026-10-11 — Interchange creation client

V30 interchange create screen: required principal-scoped journal, explicit source/format confirmation, persist intent before POST, persist ticket before explicit job navigation, exact original request/key recovery, cross-tab receipt reconciliation, scope/API lifecycle abort and access denial gating. Added EN/RU/BG labels and DOM cases for lost response/remount replay, persistence failure before POST, saved ticket without POST and wrong journal scope. Authored NOT RUN. Production creation route and entry handoff remain open; no full module completion credit. No tests, builds, typechecks, validators or migrations executed.


### 2026-10-11 — Interchange create route

V30 interchange creation route: added /interchange/new internal frozen-source handoff for explicit import bundle+SHA or export snapshot and format, rejects incompatible union/source/tenant before identity IO, waits for current principal before journal construction, remounts on connection/scope changes and navigates acknowledged workflow to interchange review. Added four route boundary/union/late-identity DOM tests authored NOT RUN. Editor export snapshot compatibility and import upload entry still require implementation; native/full module acceptance open. No tests/builds/typechecks/validators/migrations executed.


### 2026-10-11 — Interchange snapshot boundary

V30 interchange acknowledged navigation fencing: opening saved workflow now re-reads journal under scope/logout generation fencing, requires unchanged acknowledged key/ticket and aborts on scope teardown before navigation. Added removed acknowledgement denial and acknowledgement-persistence-failure exact retry DOM cases authored NOT RUN. Source inspection confirms editor render-input-snapshots creates catalogue-only snapshot without artifact_id; native interchange export requires published canonical InputSnapshot artifact plus recipe closure. Direct editor button reuse deliberately not wired; proper snapshot materialization remains required. No tests/builds/typechecks/validators/migrations executed.


### 2026-10-11 — Export snapshot compiler

V30 export snapshot preparation foundation: added closed request compiler with exact SessionSnapshotItem source+recipe pins, 1MiB HTTP bound, existing 5000-item/8MiB canonical InputSnapshot validation, tenant/project/principal/idempotency-key deterministic identity stable across session rotation, owned canonical request/document accessors and cancellation checks. Added request ownership/recipe binding/session rotation and malformed/unpinned/cancelled unit tests authored NOT RUN. Compiler explicitly supplies no authority: source/recipe closure, immutable object publication, atomic catalogue attachment and endpoint/client wiring remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Export snapshot preparation

V30 export snapshot preparation orchestration: added mandatory authority/publication/transactional attachment ports and sequencing compile→authority→immutable publication→exact root SHA/canonical recompile→authority recheck→attach→receipt. Every adapter receives independent owned document/request bytes; cancellation gates prohibit acknowledgement after interrupted stages. Failed attachment produces no receipt; unreferenced publication remains subject to orphan cleanup. Added successful owned invocation, substituted SHA, revoked second authority and attachment failure unit scenarios authored NOT RUN. Concrete SQL authority/publication/attachment, migrations, endpoint/client entry remain open; this is orchestration foundation, not deployed export capability. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Export snapshot object publisher

V30 export snapshot object publisher: implemented immutable object IO adapter with canonical recompilation of owned request, deterministic artifact/key, mandatory reservation identity/token, exact ready receipt, uncertain PUT recovery on exists/unavailable, context gates and exact key/SHA/size/MIME/non-null version checks before fenced completion; mismatched completion ref fails without returned root. Added successful/lost PUT recovery and reservation/version/completion corruption tests authored NOT RUN. Durable SQL reservation/completion repository, source/recipe authority, atomic snapshot attachment and endpoint/client wiring remain open; no deployment credit. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Export snapshot SQL authority

V30 export snapshot concrete authority: added SQLExportSnapshotAuthority with tenant Editor/project authorization, active tenant/non-archived download-enabled project, exact accepted asset version lineage (no head substitution), verified retained versioned WorkingImage in configured bucket, native WorkingImage bytes/canvas hash verification, existing typed ClosureResolver.ResolveTaskInputs for FrozenRenderRecipe and WorkingImage roots, and source metadata relookup after object IO. No object IO under SQL transaction. Added missing-installation and cancellation unit guards authored NOT RUN; native SQL/closure integration tests still required. Reservation/completion repository, atomic attachment and endpoint/client remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Export snapshot repository adapter

V30 export snapshot SQL repository adapter: added tenant Editor transaction-bound Reserve/Complete calls, deterministic identity and canonical request revalidation before SQL, strict outcome/ready/token/artifact receipt validation, exact object key/size/SHA/MIME/version checks before completion and no result on refused completion. Added missing DB installation unit guards authored NOT RUN. reserve_v30_export_snapshot/complete_v30_export_snapshot database functions are NOT yet authored; adapter is not installed/runtime usable until migration plus atomic attachment and endpoint composition land. Native SQL tests remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Export snapshot reservation migration

V30 export snapshot migration01591 authored: tenant-scoped forced-RLS reservation ledger, API-only reserve/complete definer functions with scope/current authorization and pinned source/recipe checks, deterministic snapshot/artifact identity, exact key/body conflict, durable fencing token rotation, quota reservation→storage conversion, immutable object metadata binding and strict replay. Extended existing tenant purge closed allowlist for new table preserving current function gates through definition patch. Added isolated-DB privilege/missing-authority/no-reservation integration test authored NOT RUN. Migration NOT APPLIED; atomic attachment, source mutation lock audit, full successful quota/concurrency native tests and endpoint/client composition remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Export snapshot attachment

V30 export snapshot atomic attachment migration01592 and Go adapter authored: requires ready exact reservation/root, locks tenant/project plus ordered exact source versions/assets/objects and recipe/root artifacts, repeats SQL authority, checks versioned retained object metadata, atomically inserts snapshot and full pinned item payloads. Replay requires exact snapshot principal/root/digest/count/item pins/payload; final v25 snapshot preflight failure raises rollback rather than leaving partial catalogue. Extended native SQL privilege/absent-authority integration test to attach and zero ledger/snapshot/item mutations, authored NOT RUN. Migrations NOT APPLIED; successful concurrency/retention closure integration, endpoint and client source preparation remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Export snapshot HTTP composition

V30 export snapshot HTTP/runtime composition: added authenticated project POST interchange-export-snapshots with required key,1MiB body canonicalization/closed compiler validation, independent receipt identity/artifact SHA/digest/count binding and no success after cancellation. Wired SQL authority, object publisher/repository and attachment into API main startup. Added HTTP valid/corrupt receipt/invalid request/missing key/uninstalled scenarios authored NOT RUN. SQL migrations NOT APPLIED; no runtime acceptance. Client preparation, durable snapshot request journal and editor entry remain open; endpoint contract catalogue sync/native integration still required. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Client export snapshot API

V30 client export snapshot contract/API: added closed owned source+recipe item request, ordinal/unique source/ref/SHA/metadata validation, exact working-image binding and1MiB UTF8 bound. OwnerAPI prepares native project snapshot with original idempotency key and cancellation gates; receipt validation independently computes canonical snapshot digest and full document SHA over the submitted items. Added ownership, malformed union/duplicate/source/key refusal before transport, substituted receipt, exact POST and late cancellation unit scenarios authored NOT RUN. Durable snapshot request journal, editor source recipe freeze/entry and contract catalogue sync remain open; no full/native acceptance. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Export snapshot journal

V30 export snapshot durable journal: added full canonical request-scoped origin/principal/tenant/project store with hashed bounded storage key,2MiB record bound, owned source/recipe pins and exact original request/key, request-bound receipt validation, no receipt regression, acknowledged-only clear, shared browser lock and persisted logout generation fencing. Wired logout cleanup. Added remount/owned fields/ack-only clear, project/recipe scope, logout-origin isolation and substituted receipt/key unit scenarios authored NOT RUN. Editor snapshot preparation and journal UI integration remain open; no native/full completion credit. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Export snapshot preparation screen

V30 export snapshot journal UI integration: added InterchangeSnapshotScreen with explicit selected-photo/saved-edit confirmation, mandatory scoped journal recovery, exact original pending source/recipe/key replay, reconciliation of another tab receipt, persist-before-POST and persist-before-navigation, acknowledgement re-read and logout fencing before explicit continuation, API/scope remount cancellation and denied access gating. Added EN/RU/BG messages and three DOM lost reply/remount, failed storage-before-POST and removed acknowledgement cases authored NOT RUN. Production editor freeze/route handoff still open; no full native module acceptance. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Editor interchange export entry

V30 editor export bundle entry: added /interchange/prepare frozen source/recipe route with pre-identity validation and principal/project-scoped snapshot journal, explicit preparation screen and acknowledged snapshot handoff to native interchange creation. Editor button uses existing FrozenRenderRecipe only after prepareEditorRenderFreeze confirms current saved document/revision/source binding; unsaved/current-source mismatch requires save/reprepare. Added EN/RU/BG bundle action label and route boundary/source/recipe tests authored NOT RUN. Entry supports photoeditor_bundle; broader format selection/import upload and full editor DOM/native lifecycle qualification remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Interchange export formats

V30 export format entry: source inspection confirms native export compiler/factory branches for photoeditor_bundle,xmp,captureone_sidecar. Added explicit format selection screen and /interchange/format route after acknowledged frozen snapshot preparation; all choices preserve same snapshot and navigate to required creation confirmation/journal, no automatic mutation. Added EN/RU/BG labels and sidecar representability hints,3 format pin plus invalid-scope DOM cases authored NOT RUN; updated prepare handoff expectation. Native format qualification/contract catalogue sync, import UI and full module acceptance remain open; syntactic branches do not establish runtime support. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Interchange archive source

V30 import upload gap source inspection: existing photo upload endpoint accepts image formats only; import ResolveLinkedBundle requires physically verified application/zip already project-linked. Added dedicated bounded archive UploadSource preparation with64GiB cap,1MiB incremental SHA chunks, strict filename/metadata/ranges,64MiB max part read, cancellation before/after async IO/progress and source metadata/length mutation checks. Added chunked hash/read, invalid/truncated/oversize source, in-flight cancellation and changed-file/range unit scenarios authored NOT RUN. Does not add ZIP to image upload/ingest path. Dedicated server multipart reservation/completion/project adoption, native file adapter/picker and import screen remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive multipart IO

V30 archive multipart object IO: factored existing completion/recovery/verification internals by explicit bound while preserving normal photo upload512MiB wrappers. Added canonical tenant/interchange-uploads namespace, dedicated application/zip completion/recovery and mandatory expected SHA verification up to64GiB; version/size/read-all streamed SHA checks inherited. Added namespace, oversize/short ZIP and missing SHA unit guards authored NOT RUN. Server archive session ledger/quota/lease/adoption, endpoints/UI and native objectstore integration remain open; no runtime import completion credit. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive upload intent

V30 archive upload server intent: added strict project/filename/NFC/ZIP/size22..64GiB/mandatory lowercase SHA validation, principal+key deterministic upload/artifact/key and canonical owned request hash stable across session rotation. Added exact contiguous multipart coverage compiler with expected size, owned parts and bounded ETags; no ETag-as-content-authority. Added identity/ownership/coverage and invalid source/cancellation unit tests authored NOT RUN. Dedicated SQL upload ledger/quota/completion lease/verified archive project link and endpoints/client UI still open; not runtime upload capability. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive upload ledger

V30 archive upload migration01593 authored: forced-RLS tenant/project/actor/key ledger with exact canonical request/hash, expected bytes/SHA, deterministic IDs/key, storage quota reservation under tenant usage locks, strict replay/conflict/expiry states and one-time multipart binding under token/current authorization. New table added to existing closed tenant purge allowlist preserving gates. Added API-only privilege and invalid-source/no-ledger native SQL test authored NOT RUN. Migration NOT APPLIED. Completion claim/renew/adoption, abort/expiry quota cleanup and losing multipart Begin cleanup are required before runtime installation; repository/endpoints/UI remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive completion leases

V30 archive upload migration01594 authored: exact contiguous bounded multipart receipt queue with request SHA/size/current actor authorization, idempotent same-parts replay and immutable queued parts. Added scheduler-only SKIP LOCKED2min completion lease claim, fenced renew/release with expiry/current project/actor session authorization; completion session pinned when API queues. Added native DB API-vs-scheduler privilege and absent lease refusal tests authored NOT RUN. Migration NOT APPLIED; verified-object adoption, failure/expiry/abort cleanup, lease processor/repository/endpoints/client remain open. No completed upload capability credit. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive completion adoption

V30 archive completion migration01595 authored: scheduler-only finish requires live completion token/lease, current actor session/project authority and exact expected key/bucket/size/SHA/version; atomically creates ready versioned ZIP artifact, converts quota exactly once and records verified state. Same token exact ready replay does not double-account. Added API-only current-editor/project verified archive readiness function matching ledger+artifact metadata. Added absent lease/cross-project readiness/privilege native DB cases authored NOT RUN. Migration NOT APPLIED; readiness not yet integrated into parse admission/recovery SQL predicates; completion processor, cleanup and endpoints/UI remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Uploaded archive import admission

V30 uploaded archive import admission migration01596 authored: unified current-editor/project ZIP readiness accepts retained existing project artifact links or verified upload ledger+physical object pins. Go ResolveLinkedBundle now passes current actor/session and repeats typed readiness; begin parse run source gate replaced narrowly preserving all existing ledger/run/snapshot checks. RecoverParse now repeats current bundle readiness and original archive byte size before pending/completed recovery result. Added API privilege and absent/foreign-tenant gate native SQL tests authored NOT RUN. Migrations NOT APPLIED; successful upload→parse native proof, completion processor/cleanup/endpoints/UI remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive completion lease adapter

V30 archive completion SQL lease adapter: added scheduler Claim/Renew/Release/Finish implementation, bounded persisted request/parts decode, canonical intent recompile and exact tenant/actor/project/upload/artifact/key/request SHA/size/source SHA/filename/bucket binding, contiguous owned parts and token checks. Finish independently rejects object key/size/SHA/MIME/version mismatch before SQL adoption. Added missing-installation/intent and cancellation guards authored NOT RUN. Successful claim mutation mocks/native SQL, heartbeat object processor, cleanup/endpoints/client remain open; migrations NOT APPLIED and no runtime credit. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive completion processor

V30 archive completion processor authored: bounded claim/release, owned intent/parts, pre-IO renewal,10s heartbeat/5s renewal timeout,2h max streamed work budget, lost multipart complete recovery, exact pending version/key/size/MIME checks, mandatory full SHA verification and same-version object binding, joined heartbeat and final renewal before fenced Finish. Failure releases live lease without false acknowledgement; terminal integrity cleanup still required. Added success/lost response/corrupt SHA/pre-IO lease loss unit scenarios authored NOT RUN. Scheduler installation, cancellation-during-stream qualification, cleanup/endpoints/client remain open; no runtime completion credit. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive integrity failure

V30 archive integrity failure migration01597 and processor handling: added scheduler-only fenced Fail transition completing→aborting with ARCHIVE_INTEGRITY, same-token acknowledgement replay and quota deliberately retained until physical cleanup. Processor checks pending/verified object substitution and verification integrity error, renews current lease then persists failure rather than repeatedly releasing to completion queue. Updated corrupt SHA unit scenario to require failure fencing/no Finish/no release; added failure privilege/absent lease native DB test authored NOT RUN. Migration NOT APPLIED; cleanup claim/purge/finish, scheduler installation and endpoints/client remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive physical cleanup primitive

V30 archive cleanup objectstore primitive authored: PurgeInterchangeUpload derives exact canonical tenant/upload key, refuses missing storage installation, reuses bounded all-version removal and empty-version observation, then independently requires no exact-key incomplete multipart upload. Listing failures, cancellation and remaining uploads fail closed; neighboring keys are untouched. Unit scenarios authored NOT RUN. Exclusive cleanup lease/writer fencing, SQL cleanup claim/finish and quota release remain open; this primitive alone does not establish safe concurrent cleanup. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive cleanup leases

V30 migration01598 authored: integrity-failure cleanup_not_before conservatively waits until upload expiry plus maximum2h completion IO budget and5min margin; independent scheduler-only cleanup Claim/Renew/Release/Finish token and2min lease, canonical artifact/key collision exclusion, active-tenant fence, exact bucket/key completion, atomic reserved quota decrement and same-token aborted replay. Existing failures delayed from migration time. Added role/absent-lease integration cases NOT RUN. Cleanup adapter/heartbeat processor/scheduler, upload URL expiry and writer quiescence enforcement, expiry/user-abort cleanup and successful quota/concurrency tests remain open. Delay alone is not proof of writer quiescence; runtime must not install cleanup before enforcing that contract. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive cleanup SQL adapter

V30 cleanup SQL adapter authored: separate scheduler lease interface and exact canonical tenant/upload key plus configured bucket/token validation before Renew/Release/Finish; Claim decodes explicit columns, handles no-work distinctly and rejects missing timestamps/installation, cancellation and DB failures. Finish passes exact physical scope to SQL quota fence. Unit tests bind all five arguments and reject cross-bucket/key/upload/token mutation before DB IO, plus absent/cancelled/failing claims, authored NOT RUN. Writer-quiescence guard, heartbeat purge processor, scheduler installation and successful claim/native quota tests remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive cleanup processor

V30 archive cleanup processor authored: mandatory writer-quiescence authority before exact tenant/upload purge and again before quota commit, copied lease, bounded claim/renew/work/finish/release contexts,10s heartbeat, cancellation on lease loss, join before final renewal and fenced Finish, failed work releases lease without quota acknowledgement. Unit success/pre-IO lease loss/writer activity/purge failure/finish refusal/invalid key/missing guard cases authored NOT RUN. Production writer-quiescence implementation, scheduler installation, cancellation-during-purge test and native cleanup/quota lifecycle remain open; processor is deliberately not installed without guard. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive part signing bounds

V30 archive multipart creation/signing objectstore wrappers authored: generated identity-only archive key and ZIP MIME, declared22B..64GiB archive part cardinality, explicit unique1..N batch<=100,15min URL TTL cap, absolute authorized notAfter horizon with1s margin and deadline/returned-expiry checks, cancellation before signing. Unit deadline/size/part/cancellation cases authored NOT RUN. Durable signing authority recording and ledger-bound API orchestration still required; wrappers alone do not establish writer quiescence and cleanup remains uninstalled. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Durable archive signing authorization

V30 migration01599 authored: API-only authorize_v30_archive_parts with tenant context/current editor session, active tenant/project, actor/token/request SHA/uploading-state and exact bound multipart ledger, unique nonnull1..N requested parts<=100,15min horizon capped by upload expiry and61s minimum. Durably records monotonic parts_authorized_until and returns exact bucket/key/multipart/bytes/deadline; callers must commit before signing. Added privilege and absent/invalid-upload DB cases NOT RUN. Successful concurrency/expiry tests, Go transactional authorization adapter/API and writer-quiescence authority remain open; migration NOT APPLIED, no cleanup installation credit. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Committed archive signing permit

V30 Go archive signing authority authored: current Editor tenant transaction, recompiled frozen intent and exact identity/hash/key, owned unique bounded part numbers, SQL grant decode binds configured bucket/object/multipart/bytes/deadline, returns private permit only after successful tenant.WithTenant commit; zero permit cannot sign and signer receives copied parts. Unit parts ownership/bounds/missing installation/zero permit/cancellation cases authored NOT RUN. Native successful commit/rollback tests, API orchestration, writer guard and scheduler remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive upload SQL repository

V30 archive upload SQL repository authored: committed current-Editor Reserve with canonical frozen intent/body recompile, strict deterministic ID/token/state/multipart receipt and quota/conflict/stale errors; tenant-authorized Bind and Queue with bounded multipart identity and owned complete parts canonical JSON. Added reservation substitution/state/error mapping and missing-installation/cancellation unit tests NOT RUN. Multipart begin/bind concurrency/orphan recovery, successful native SQL quota paths, status API and end-to-end service/HTTP/client remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive multipart begin reconciliation

V30 archive multipart Begin service authored: reserve before object IO, replay already-bound states, bind created session, reconcile ambiguous commit through fresh committed ledger lookup, recover own winning session without abort, abort only exact distinct proven losing multipart ID with detached bounded cleanup context; unavailable/ambiguous authority leaves session for orphan cleanup instead of risking winner. Added identity-only exact-session abort wrapper and success/replay/lost-commit/loser/unavailable-authority unit cases NOT RUN. Durable orphan tracking/crash recovery, API/status/client and production writer guard remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive Begin HTTP endpoint

V30 archive Begin HTTP endpoint authored and composed in API startup: authenticated tenant/project POST interchange-archive-uploads,16KiB closed JSON filename/bytes/SHA, route-owned project, required idempotency key, frozen intent and independent returned ID/hash/key/token/state checks, quota/conflict/expiry errors, no success after cancellation. Public receipt exposes upload ID/state/part size/count/request SHA only, no multipart/object key/token. HTTP valid/corrupt/invalid/missing-key/uninstalled/unknown-field cases authored NOT RUN. Parts/completion/status endpoints, endpoint contract catalogue/client, crash orphan recovery and scheduler remain open; migrations NOT APPLIED. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Scoped archive upload lookup

V30 archive scoped lookup migration01600 and Go repository authored: read-only current tenant/project/actor/editor-session authorization, active downloadable project, no quota/reservation side effect, closed bounded stored request recompile with deterministic upload/artifact/request SHA/object key and configured bucket binding; strict states/error enum, expiry and independently authorized verified artifact readiness. Missing DB and role/absent-scope tests authored NOT RUN. Parts/completion/status services/HTTP/client and successful native scope/retention/concurrency tests remain open; migration NOT APPLIED. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive upload action services

V30 archive actions service authored: scoped current-user Lookup and recompiled identity/project/source request SHA before Parts/Complete, uploading-only signing, owned bounded requested parts, committed permit identity/multipart/size/parts binding, signed receipt count/unique expected part/deadline validation, completion allowed only uploading/completing/verified and full owned contiguous ETags queued for asynchronous verification. Unit request substitution/no-sign, valid sign, corrupt part receipt, owned completion input and aborting refusal cases authored NOT RUN. HTTP action/status endpoints, client, scheduler/writer guard and native lifecycle remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive action HTTP endpoints

V30 Parts/Complete HTTP endpoints authored and composed: authenticated tenant/project/upload routes, closed16KiB parts and1MiB completion JSON, request SHA and batch guards, duplicate rejection before signing, exact signed receipt membership, no-store URLs, asynchronous202 accepted acknowledgement never verified success, typed conflict/expiry/integrity errors and cancellation suppression. HTTP valid/corrupt/duplicate/invalid SHA/uninstalled/accepted-semantics cases authored NOT RUN. Status endpoint/client/contracts, scheduler/writer guard, orphan recovery and native lifecycle remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive upload status

V30 archive Status service/HTTP GET authored and composed: scoped read with exact request SHA query, upload identity and expiry/state receipt validation, no-store response, artifact ID/source SHA emitted only with independent authorized ready evidence and verified ledger; verified state alone yields ready=false/no artifact. Added unit ready-evidence versus ledger-state/mismatched readiness cases NOT RUN. HTTP status test suite, client/contracts, scheduler/writer guard/orphan lifecycle/native acceptance remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Client archive Begin contract

V30 client archive Begin contract and OwnerAPI method authored: strict closed filename/size/SHA/key request, Unicode-rune length/control character validation aligned with server,64GiB bound, independently recomputed canonical request SHA including route project, exact16MiB geometry/count and allowed bound-state receipt checks, copied request/receipt and tenant/project UUID/cancellation guards around HTTP. Unit ownership/hash/project/geometry/unknown fields/name/size/key cases authored NOT RUN. Client parts/complete/status transport, durable journal/upload UI, contracts and scheduler/native lifecycle remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Client archive action contracts

V30 client Parts/Complete/Status contracts and OwnerAPI methods authored: revalidate Begin receipt against frozen project/source request before every request, owned unique bounded parts/full completion receipts, signed URL exact membership/count/expiry/protocol/credential checks, closed202 upload acknowledgement, scoped status/source SHA and independent ready-artifact checks with no artifact on ready=false; cancellation checks around transport. Unit queued-vs-ready/source substitution/owned ETags/signed-part mutation cases authored NOT RUN. Upload executor/durable journal/UI/contracts/scheduler/native acceptance remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive multipart client executor

V30 archive client executor authored: sequential16MiB source reads, exact source metadata/SHA binding and cancellation around every await, owned bounded partial ETag checkpoint, skip persisted parts on resume, durable save before next part/completion, validated signed URL and ETag, explicit202 queued persistence then one status read without false readiness or hidden retry/poll; PUT omits credentials and rejects redirects. Unit persistence ordering/resume/no IO after journal failure cases authored NOT RUN. Concrete exclusive principal-scoped journal, UI/picker/native source, contracts/scheduler/writer guard/native acceptance remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Persistent archive upload journal

V30 persistent archive attempt journal authored: exact origin/user/tenant/project/request-hash key, bounded closed request/checkpoint decode, durable pre-Begin idempotency intent, immutable Begin receipt and monotonic part/ETag/stage progression, owned sorted receipts, queued-only explicit clear, semantic journal transaction lock plus in-process and persisted logout-generation fence. Auth logout clears archive journals. Unit restore/ownership/regression/ETag substitution/clear/principal/logout cases authored NOT RUN. Whole-executor exclusive coordination across tabs, journal-writer composition/UI/native picker/contracts/scheduler/native lifecycle remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive upload controller

V30 archive controller authored: explicit-action composition of principal-scoped journal and executor, in-process keyed serialization plus distinct cross-tab execution WebLock (avoids nested journal lock deadlock), persist original idempotency intent before Begin and immutable receipt before PUT, reuse stored key/checkpoint on retry, journal generation/scope fence before and after each API/PUT and before checkpoint save. Unit pre-Begin/pre-PUT durability and queued resume without repeated Begin/PUT/Complete cases authored NOT RUN. UI/picker/native source, lock-contention/logout-in-flight tests, contracts/scheduler/writer guard/native acceptance remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive upload screen

V30 archive upload screen authored: explicit ZIP selection/hash, current-principal request journal restore, progress and stop/retry controls, controller-driven upload or single status refresh, no automatic mutation/poll, fresh scoped status and journal logout fence before explicit ready-artifact import handoff, client/principal/project remount abort. Web picker and EN/RU/BG strings authored. Screen not routed yet; DOM unit tests/native picker/route/catalog entry/contracts/scheduler/native acceptance remain open. No completion credit. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive upload route and DOM cases

V30 archive upload route authored: strict tenant/project UUID and supported format before principal read, current user-scoped screen, route/client/origin remount fencing and explicit fresh-ready artifact handoff to existing import-create route. Added DOM tests no automatic picker/upload, explicit upload error path and unmount cancellation authored NOT RUN. Native picker, product navigation entry, full ready/logout/retry DOM cases, contracts/scheduler/writer guard/native lifecycle remain open; web route alone is not complete archive import. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Native archive selection and project entry

V30 archive native picker/source and project navigation authored: existing expo-document-picker/file-system dependencies, single ZIP cache selection, bounded native handle reads with metadata/size recheck and exact byte count, handles closed on failure, owned buffers, event-loop yield for cancellation during hash; upload route injects platform picker and project screen links archive upload. Native source handle/change/truncation/cancel unit cases authored NOT RUN. Native cache lifecycle/real device qualification, format chooser entry for sidecars, complete DOM/runtime contracts/scheduler/writer guard/orphan cleanup remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive completion scheduler installation

V30 archive completion scheduler composition authored: executable factory avoids interchange/workflow cycle, checks all five completion function privileges before install, builds SQL leases plus real objectstore processor, independent heartbeat-owned loop/readiness/shutdown join and handled/failure metrics (handled includes persisted integrity failure, not only verified success). Scheduler CLI installs factory; legacy embedded entry remains compatible without factory. Missing dependencies/cancellation factory unit guards authored NOT RUN. Cleanup intentionally not installed without writer-quiescence authority; native startup/stream cancellation/lifecycle/contracts remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Expired archive cleanup admission

V30 archive expiry migration01601 and processor authored: bounded one-row SKIP LOCKED selection of expired reserved/uploading/completing records without live completion lease or canonical artifact collision, transition to aborting/UPLOAD_EXPIRED and writer-horizon-based conservative cleanup delay, quota unchanged. Cleanup claim/finish now recognize expired uploads while retaining token/bucket/key/physical-purge/accounting fences. Adapter result/error/cancellation and scheduler-only privilege tests authored NOT RUN. Expiry scheduler installation, successful quota/concurrency fixtures, writer-quiescence authority/cleanup installation and native lifecycle remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive expiry scheduler loop

V30 archive expiry scheduler installation authored: optional companion factory on executable archive composition, startup migration/EXECUTE privilege check, independently bounded expiry loop/readiness/counters/shutdown join. Main scheduler archive factory now supplies both completion and expiry; expiry only persists aborting/UPLOAD_EXPIRED and does not purge storage or release quota. Missing DB/cancellation factory unit cases authored NOT RUN. Physical cleanup/writer-quiescence authority, startup/native quota/concurrency tests, contracts and user abort/orphan lifecycle remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Explicit archive upload abort

V30 explicit archive Abort migration01602/repository/actions authored: current tenant/actor/editor/project authorization and exact request SHA, reserved/uploading/completing→aborting/UPLOAD_ABORTED, fences completion token, replay aborting/aborted, refuses verified/canonical artifacts, conservatively delays cleanup through writer horizon, no quota release. Cleanup claim/finish admit user abort with existing physical/accounting fences. Unit verified/request substitution and repository-installation plus SQL privilege/absent-upload cases authored NOT RUN. Abort HTTP/client/confirmation UI, writer authority/cleanup installation/native concurrency remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive Abort HTTP and client

V30 Abort HTTP/client authored: authenticated tenant/project/upload POST with closed1KiB request SHA, current scoped service abort,202 exact upload acknowledgement explicitly cleanup_pending=true rather than quota/purge success. OwnerAPI revalidates frozen source/Begin receipt before mutation and checks cancellation/closed response. HTTP accepted/invalid/uninstalled and client substituted/missing cleanup acknowledgement unit cases authored NOT RUN. Confirmed UI/controller abort coordination, abort replay after already-aborted response semantics, writer authority/cleanup installation/native concurrency/contracts remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Durable confirmed archive cancellation

V30 confirmed abort UI/controller authored: explicit checkbox/action distinct from local Stop, same execution lock as uploader, durable aborting intent before Abort HTTP so lost response/remount cannot resume PUT, journal rejects abort-stage regression, executor aborting mode only reads status, fresh abort/status/journal fences and RU/EN/BG pending-cleanup feedback. Added unit no-upload-after-abort and monotonic abort journal cases NOT RUN; screen mock updated. Successful/ambiguous abort DOM/controller and already-purged acknowledgement semantics, cleanup writer authority/native lifecycle/contracts remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Ambiguous archive Abort recovery

V30 abort controller follow-up: strengthened uploader pre/post IO journal fence to reject newly persisted abort intent, abort controller now requires exact durable original key/upload/aborting stage both before status and before return (not just a successful journal read). Added lost Abort reply with persisted no-resume intent, explicit same-upload retry and no Abort before durable Begin receipt unit cases authored NOT RUN. Already-aborted HTTP acknowledgement semantics, complete DOM coverage, cleanup authority/native lifecycle/contracts remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Accurate archive Abort replay status

V30 Abort replay semantics corrected: actions re-read committed scoped state after Abort and return cleanup_pending=true only for aborting, false for already-aborted; unexpected state fails closed. HTTP propagates actual pending flag, client accepts strict boolean while still requiring exact upload/accepted closed acknowledgement. Added/updated service already-purged, HTTP pending/cleaned and client boolean-vs-string cases authored NOT RUN. No physical-cleanup success inferred from202 alone; cleanup writer authority/native lifecycle/contracts remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Archive cleanup authority guard

V30 cleanup authority migration01603/Go guard authored: scheduler-only exact tenant/upload/token/bucket/generated key, live cleanup lease, active tenant/aborting reason, expired upload/signing horizon/no completion lease and canonical artifact exclusion. Go guard requires independently installed ArchiveStorageDrain, checks ledger before and after bounded physical writer-drain attestation; absent drain refuses before DB IO. Unit successful double-check/drain failure/missing authority and DB privilege/absent scope cases authored NOT RUN. Concrete distributed storage writer coordinator and cleanup scheduler installation remain open; elapsed delay alone never grants purge authority. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Durable archive writer ledger

V30 migration01604 durable archive writer ledger authored: forced-RLS tenant/upload scoped begin/complete tickets, current API actor/session/reservation or scheduler live completion fence, exclusive open writer per upload under ledger row lock, kind-specific privileged close with immutable physical receipt replay, no automatic expiry of ambiguous/crashed writers. Cleanup SQL authority now refuses any open writer; tenant purge allowlist includes child ledger before uploads. Role/absent-scope integration cases authored NOT RUN. Go ticket coordinator and Begin/Complete adoption, ambiguous remote reconciliation/crash orphan management and cleanup installation remain open; existing runtime does not yet issue tickets. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Durable Begin writer integration

V30 durable Begin writer Go coordinator/runtime adoption authored: current-Editor committed Start ticket before S3, exact owned tenant/upload/kind/token ticket,30s bounded NewMultipartUpload, close only concrete successful multipart ID before Bind, detached5s close after client cancellation, ambiguous/error IO retains open durable writer to block cleanup. API startup supplies mandatory coordinator; Begin tests now require ticket-before-S3/close-before-Bind; missing DB/ticket/cancel unit guards authored NOT RUN. Completion writer adoption and ambiguous Begin/crash reconciliation still open; no cleanup installation credit. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Durable Complete writer integration

V30 Complete durable writer Go coordinator/runtime adoption authored: scheduler Start ticket bound to live canonical completion lease before S3, exact token/tenant/upload/key/bytes/kind, close only concrete recovered/completed version receipt before read-only SHA verification; bounded detached close retains ambiguous/unacknowledged tickets. Scheduler factory requires writer function privileges and installs mandatory coordinator. Unit writer lifecycle and physical receipt substitution cases authored NOT RUN. Lost-process/open-ticket reconciliation, Begin orphan recovery and distributed drain/cleanup installation/native lifecycle remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


### 2026-10-11 — Begin writer receipt crash recovery

V30 Begin receipt crash recovery migration01605 and Go adoption authored: current actor/session/project/reservation-locked recovery atomically binds an existing closed Begin physical receipt to reserved upload, refuses open writers/expired authority, exact uploading replay. StartBegin now refuses any prior Begin ticket so crash after closed receipt cannot create a second session. Begin service checks committed recovery before S3; closed-receipt zero-new-Begin unit and SQL role cases authored NOT RUN. Open/ambiguous Begin reconciliation and Complete receipt retry recovery, cleanup coordinator/native lifecycle remain open. No tests/builds/typechecks/validators/migrations/gofmt executed.


## V30: durable ready handoff fence — 2026-10-11

V30 ready-artifact handoff now requires the same complete durable attempt before and after the fresh status read; missing/replaced/aborting/changed journals and unmounted screens refuse navigation. Added unit cases for unchanged/deleted/key/upload/source/ETag/abort substitutions. Also added missing uuid import to archive completion processor found by source inspection.

NOT RUN; tests/build/typecheck/migrations deferred. Full DOM/native handoff, writer reconciliation and cleanup installation remain open. Whole-product completion remains unproven; no percentage credit.


## V30: closed Complete receipt recovery — 2026-10-11

V30 closed Complete receipt recovery authored: migration01606 authorizes scheduler recovery under active tenant/upload row lock/live completion token+lease, no open writers, bounded saved version. StartComplete refuses any previous Complete ticket. Go processor reuses the exact closed version with read-only SHA verification (existing objectstore GetObject VersionID), without another Complete/latest-key recovery/ticket close. Startup requires recovery privilege. Unit closed-receipt no-write and invalid-receipt refusal plus SQL role/missing-scope cases authored.

NOT RUN; migration NOT APPLIED. Open/ambiguous writer reconciliation, distributed drain/cleanup installation and full lifecycle/concurrency qualification remain open. No whole-product completion credit.


## V30: native archive cache ownership — 2026-10-11

V30 native archive source lifecycle authored: picker cache-root ownership check, cleanup on cancelled/failed preparation, private owned source release on replacement and screen unmount; disposed sources reject new/in-flight reads, unowned sources untouched and failed deletion permits retry. Durable upload journal is preserved for explicit source reselection/resume. Source filename validation aligned to Unicode rune bounds and Cc controls. Unit release/idempotency/inflight/deletion-failure/unowned/Unicode scenarios authored.

NOT RUN. Native SDK/device cache behavior and crash-time orphan cache sweeping remain open, as do full DOM lifecycle, ambiguous remote writer reconciliation and storage cleanup installation. No full-module acceptance credit.


## V30: explicit archive import format entry — 2026-10-11

V30 explicit archive import format entry authored: project navigation opens scoped import-format route; PhotoEditor bundle/XMP/Capture One sidecar choices pass explicit format plus unchanged tenant/project into existing upload and subsequent import creation. Invalid scope offers no choices; repeated selection is fenced. Added EN/RU/BG import-specific hints and DOM screen/route tests for each format, explicit-only navigation, duplicate selection and invalid scope.

NOT RUN; deep tests deferred. Full end-to-end parser/mapping/import qualification, native crash cache cleanup, open writer reconciliation and physical cleanup installation remain open. Whole-product implementation percent remains unestablished.


## V30: archive upload API catalogue — 2026-10-11

V30 archive upload API contract catalogue synchronized by direct edits (no generator execution): closed schemas for Begin/receipt/parts/Complete/Abort/status, five scoped OpenAPI/endpoints operations, explicit required status request_sha256, Begin-only idempotency header, exact ZIP64GiB/16MiB geometry, queued versus ready distinction, conditional artifact visibility and boolean cleanup_pending. Added archive-upload-contracts.json as source and wired build-spec.py to preserve additions on later generation.

NOT RUN; generators/validators/tests not executed. Dynamic NFC, exact part coverage, URL authority and ready artifact authorization remain runtime predicates; contract catalogue does not prove lifecycle completion. Function index/progress catalogue and full generated-contract reconciliation remain open. Overall completion remains unproven.


## V30: status HTTP boundary — 2026-10-11

V30 archive status HTTP boundary hardened: closed error_code allowlist and deterministic archive artifact UUID binding (upload namespace archive:artifact:1), in addition to existing exact upload/request SHA/state/ready/expiry checks. Authored HTTP matrix for pending/verified-not-ready/ready, substituted upload/hash/artifact, invalid state/error/SHA/expiry, forbidden artifact, absent provider, cancellation and missing/duplicate request SHA; successful responses require no-store.

NOT RUN; source edits and tests only. Provider boundary does not substitute scoped SQL/physical readiness, and full lifecycle/remote writer reconciliation/cleanup/native acceptance remain open. Overall completion remains unproven.


## V30: signed part public envelope — 2026-10-11

V30 signed archive part public envelope hardened with shared objectstore validator used by scoped actions and HTTP boundary: HTTP(S)/nonempty host/no userinfo or fragment/opaque/control chars, bounded URL32KiB, part1..4096, expiry>5s and<=15min, plus service durable permit horizon. Authored unit matrix for URL/expiry/authority bounds and HTTP provider-link rejection without exposing signed link.

NOT RUN; tests/build/validators deferred. Envelope validation does not prove storage signature authority or writer drain; exact requested-part matching remains in both callers. Full lifecycle, ambiguous writer recovery and physical cleanup installation remain open. Whole-product completion remains unproven.


## V30: final publication writer receipt — 2026-10-11

V30 migration01607 publication receipt fence authored: finish_v30_archive_completion retains current auth/project/live lease/quota checks and now refuses any open writer or absence of a closed Complete ticket matching the exact published object version before artifact INSERT/accounting. Function patched with explicit mismatch refusal and preserved OID/ACL. Authored DB installation-fence/absent-scope cases; these do not prove successful publication or concurrency.

NOT RUN; migration NOT APPLIED. Full successful SQL accounting/race fixtures, open writer reconciliation, physical drain/cleanup installation and native lifecycle remain open. Overall completion remains unproven.


## V30: cleanup SQL commit authority — 2026-10-11

V30 migration01608 cleanup commit authority authored: finish_v30_archive_cleanup repeats v30_archive_cleanup_authorized under its existing tenant/upload locks immediately before reserved quota decrement, preserving same-token aborted replay. This enforces no open writer, expired upload/signing horizons, no completion lease, exact scope and no canonical artifact at SQL commit, in addition to processor pre/post physical-drain checks. Added SQL installation/absent-scope cases.

NOT RUN; migration NOT APPLIED. Predicate does not attest remote storage drain or physical deletion. Concrete distributed drain/cleanup scheduler installation, open writer reconciliation and successful lifecycle/concurrency fixtures remain open. No whole-product completion credit.


## V30: current Complete writer authority — 2026-10-11

V30 migration01609 current writer admission authority authored: StartComplete and RecoverComplete revalidate persisted actor/completion session through v2_editor_authorized and lock active/download-enabled project before writer admission/receipt reuse, in addition to active tenant, locked upload and live completion lease. Exact source patch guards preserve OID/ACL. Added SQL installation assertions, explicitly not session-revocation/concurrency proof.

NOT RUN; migration NOT APPLIED. Open Complete/Begin ambiguity is not resolved by presence of a latest object; remains open until authoritative remote operation reconciliation. Physical drain/cleanup installation and successful lifecycle tests remain open. No full-module acceptance credit.


## V30: completion retry fairness — 2026-10-11

V30 migration01610 completion queue fairness authored: durable nonnull completion_retry_at,30s backoff on token-fenced Release without losing parts/closed receipts, Claim skips retry horizons and any open writer tickets so one ambiguous oldest upload does not starve ordinary completion. OID/ACL preserved. Added DB retry-column/role installation cases; multi-upload starvation/concurrency fixtures still required.

NOT RUN; migration NOT APPLIED. Open writers need a separate reconciliation contour and are not silently expired/closed. Full behavioral SQL fairness tests, distributed drain/cleanup installation and lifecycle acceptance remain open. No overall completion credit.


## V30: physical multipart revocation and cleanup installation — 2026-10-11

V30 concrete multipart revocation/drain and cleanup scheduler authored: exact known session Abort then ListParts must positively return NoSuchUpload; exact-key incomplete listing must be empty. No-bound-session path requires SQL no-open-writer authority plus empty listing. SQL drain adapter binds configured bucket/generated key, reads guarded multipart before/after physical IO; migrations01611 scope and01612 cleanup Claim writer/horizon fence. Main archive factory installs cleanup with privilege gates; independent scheduler loop/readiness/metrics/shutdown join. Authored storage revocation/errors/live-session/cancel tests, adapter authority-change/scope tests, factory guards and SQL role/absent-scope tests.

NOT RUN; migrations NOT APPLIED. Native MinIO/S3 Abort/ListParts semantics, SDK compilation, in-flight UploadPart/Complete races, successful physical cleanup/quota/concurrency and scheduler acceptance must be qualified later. Open ambiguous Begin/Complete tickets remain unresolved and block cleanup; they are never auto-expired. Installation code is authored, not deployed proof. Whole-product completion remains unproven.


## V30: terminal multipart completion observation — 2026-10-11

V30 read-only completion observation authored: exact generated key/multipart/size, ListParts must return NoSuchUpload before and after first versioned object read, second read must match exact version/key/size/MIME. Live session/access error/missing object/changed version/cancellation cannot produce receipt. Lost Complete response branch now uses this observer instead of latest-key recovery alone, then existing close/SHA verification/fenced publication. Unit matrix authored for all refusal paths and stable success; no destructive IO in observer.

NOT RUN. SDK/native S3 atomicity and race qualification remain required. This handles lost response inside a live processor; separate crash-time open-ticket reconciliation scheduler is still missing. Open ambiguous Begin/Complete and crash orphan cases remain blocked rather than guessed; full-module completion not claimed. Overall completion remains unproven.


## V30: open Complete crash recovery scheduler — 2026-10-11

V30 crash-time open Complete recovery scheduler authored: migration01613 claims exactly one open Complete (no open Begin), expired prior completion lease, retry horizon/current auth/active project; adopts existing ticket under new lease without INSERT. Recovery-specific SQL lease source and writer adapter reuse heartbeat, read-only terminal multipart observer, concrete receipt close, full SHA check and fenced Finish/Fail. Independent scheduler recovery factory privilege checks/loop/readiness/metrics/shutdown join installed. Unit observed/ambiguous/SHA-mismatch no-Complete matrix, factory guards and SQL role/absent adoption cases authored.

NOT RUN; migration NOT APPLIED; compilation/native S3/SQL concurrency not verified. Recovery currently covers still-completing/unexpired/currently-authorized uploads with completed physical result. Live session/no object remains open and retries with30s backoff. Open Begin, aborted/expired/revoked-authority open Complete reconciliation and cancellation of uncompleted remote operations remain open; no full V30 acceptance claimed. Overall implementation percent remains unestablished.


## V30: open Complete cancellation revocation — 2026-10-11

V30 open Complete cancellation/revocation contour authored: migration01614 explicit revoked writer state distinct from completed version receipt; claim only aborting uploads past upload/signing/cleanup horizons, no live completion lease, exactly one open Complete/no canonical artifact. Bounded processor positively revokes exact multipart via storage drain before token/writer/session/scope-fenced SQL commit, no quota release/purge acknowledgement. Independent scheduler factory/privilege gates/loop/readiness/metrics/shutdown join installed. Unit physical failure/refused commit/foreign scope/cancellation and factory guards plus SQL role/absent writer cases authored.

NOT RUN; migration NOT APPLIED; real S3 in-flight revocation and SQL constraints/locks/concurrency need qualification. Open Begin reconciliation and prompt handling of revoked-authority still-completing uploads remain open. Failed revocation retains open ticket and cleanup lease until expiry; ordinary cleanup independently purges/commits quota after successful revocation. No full-module acceptance credit. Overall completion remains unproven.


## V30: queued Complete authority invalidation — 2026-10-11

V30 lost-authority queued Complete invalidation authored: migration01615 bounded SKIP LOCKED selection of completing uploads with inactive original editor session or unavailable project, no live completion lease/canonical artifact; state→aborting/UPLOAD_ABORTED, clears completion fence, retains quota and conservative physical cleanup horizon. Dedicated5s adapter and independent scheduler factory privilege checks/loop/readiness/metrics/shutdown join installed. Authored DB error/cancel/result/factory dependency and SQL scheduler-only role tests.

NOT RUN; migration NOT APPLIED. Positive authorization-revoke/project-archive fixtures and races remain required. Reserved/uploading user-session loss still relies on expiry (original session is not persisted there); queued Complete now has explicit invalidation. Open Begin reconciliation, native storage races and full V20–V32 semantic qualification remain open. No whole-product completion credit.


## V30: pre-Complete session authority lifecycle — 2026-10-11

V30 migration01616 pre-Complete session authority authored: private nullable authority_session_id persisted at Reserve, refreshed only through already-authorized replay for reserved/uploading, Bind and committed Parts authorization. Current status reads do not refresh sessions. Invalidation now covers reserved/uploading/completing session loss and unavailable project, preserving active completion lease/no artifact/quota fences. Legacy sessions are backfilled only from known completion_session_id; unknown pre-Complete sessions remain expiry-managed. Added DB private-column/legacy compatibility installation cases.

NOT RUN; migration NOT APPLIED. Session expiry/reauthenticated resume versus invalidation row-lock races and positive SQL fixtures remain required. Code-generation/session semantics documented here; no verified runtime claim. Open Begin reconciliation and native/full lifecycle testing remain open. Whole-product completion remains unproven.


## V30: explicit restart after physical cleanup — 2026-10-11

V30 explicit restart after confirmed physical cleanup authored: resetAbortedArchiveUpload serializes with active uploader, freshly validates scoped server aborted/not-ready status, rechecks durable exact key/upload, persists aborting intent and clears only matching aborting journal. Screen distinguishes aborted/cleanup complete from aborting and exposes explicit Start again, releasing selected cache copy; no reset on202/aborting/verified or automatic mutation. EN/RU/BG strings and controller/journal terminal-scope refusal tests authored.

NOT RUN. Full DOM restart/logout/race and native cache failure cases remain required; server physical cleanup semantics still need qualification. Open Begin reconciliation and full V20–V32 semantic audit remain open. No overall percentage credit. Overall completion remains unproven.


## V32: capacity heartbeat measurement admission — 2026-10-11

V32 capacity heartbeat input admission hardened: CapacityPolicy.cap_measurement now rejects unknown worker classes, missing/wrong measurement object, bool/fraction/negative/unsafe counters, invalid scratch total or reported free scratch exceeding total before advertising. Exhausted zero capacity remains valid and never invents resources. Confirmed by source inspection that production SQL sums node resource reservations; no second global ledger introduced. Existing Metal verification profile retains exact10,000,000,000-byte VRAM cap. Unit malformed/exhausted measurement matrix authored.

NOT RUN; tests/build/validators deferred. This is input validation, not proof of OS-level hard memory enforcement or multi-worker fairness/capacity qualification. Full V32 global coordination, restore drill/native measurements and V20–V32 semantic audit remain open. Overall completion remains unproven.


## V32: configured Metal allocator ceiling — 2026-10-11

V32 MPS allocator byte ceiling authored: install_metal_budget validates operator bytes and driver counters, converts to nonzero downward-rounded fraction bounded by Metal recommended working set, refuses already-over-budget memory and rechecks after setter. Worker installs configured MPS GPU limit before build and again before consuming tasks (including externally built worker); CPU/CUDA/no-policy legacy paths unchanged. Unit10-decimal-GB/recommended cap/invalid counters/already-loaded/recheck/worker-routing cases authored with fake runtime, no hardware execution.

NOT RUN. Official PyTorch MPS fraction API consulted; zero means unlimited and is excluded. This limits PyTorch allocator only, not external Metal libraries/system RAM; full Metal peak measurement and concurrency qualification remain open. Existing capacity-metal-verification.json remains10,000,000,000 bytes. No overall completion credit. API reference: https://docs.pytorch.org/docs/stable/generated/torch.mps.set_per_process_memory_fraction.html


## V32: restored V30 archive lease fencing — 2026-10-11

V32 restore fencing extended to V30 archive lifecycle: cleanup allowlist now expires v30_archive_uploads.cleanup_lease_until; scheduler allowlist expires completion_lease_until, preserving tokens/plans/state/quota/writer receipts. Final read-only recovery fence imports both families and rejects revived archive grants. Unit atomic rollback matrices include archive update failure; final fence matrix distinguishes revived completion versus cleanup and expanded expected counts.

NOT RUN; tests/build/validators deferred. Restore remains isolated/paused and does not grant writer cutover. Open archive writer physical reconciliation and old-worker capability/session isolation remain separate requirements; full restore drill and Metal capacity qualification remain open. No whole-product completion credit.


## V30/V32: lease-fenced Complete writer close — 2026-10-11

V30/V32 migration01617 Complete writer close lease fence authored: retired4-argument close EXECUTE for all service roles, new5-argument close binds exact current completion token under active tenant/upload locks; open→closed requires live completing/unexpired lease, closed receipt replay also requires current token. Private Go tickets carry completion token for normal/recovery adoption, processor validates it and close passes it; startup requires new signature. Unit SQL argument binding/missing lease refusal and DB old-entrypoint denial/new-role/absent-scope cases authored.

NOT RUN; migration NOT APPLIED. Actual restore/new-claim versus late old-worker closure races need DB/native tests. Revoked tickets remain immutable; restore lease expiry blocks old open-ticket closure. Open Begin unknown outcome and full lifecycle/restore qualification remain open; no overall completion credit. Whole-product completion remains unproven.


## V30: reset consistency on cache failure — 2026-10-11

V30 reset/cache failure consistency authored: after server-confirmed journal reset, screen clears selected attempt/status before releasing the owned cache copy. Failed deletion preserves the fenced source reference for retry on next selection; server-reset failure retains selection. Added two DOM cases and corrected controller reset mock/source typing. NOT RUN; crash-time cache sweeping and full native lifecycle remain open.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V30: ready archive handoff DOM scenarios — 2026-10-11

V30 ready archive handoff DOM coverage authored with real durable attempt storage: fresh ready response permits exactly one navigation; fresh not-ready response, journal deletion during status IO and late response after unmount forbid handoff. Status request cancellation asserted. Unique origin scopes isolate ready fixtures; OwnerAPI spies restored. NOT RUN; native lifecycle and physical archive recovery qualification remain open.

No tests/builds/validators/migrations executed. No whole-product acceptance credit.


## V30: isolated native archive cache sweep — 2026-10-11

V30 native ZIP cache namespace and crash sweep authored: validated picker copy moved synchronously into photoeditor-interchange-v1/UUID.zip; sweep before native picking deletes only direct owned-name File entries, skips active selections and unknown/nested/external entries. Release drops active claim so failed deletion can retry on next sweep. Added four cache ownership/failure cases. NOT RUN; Expo move/delete semantics and native process lifecycle need qualification. Legacy picker leftovers outside dedicated namespace intentionally not swept.

No tests/builds/validators/migrations executed. Overall completion remains unproven.


## V30: native archive picker lifecycle — 2026-10-11

V30 native picker lifecycle coverage authored: cache sweep precedes OS picking; successful staged hash retains copy until explicit release; cancellation after picker return cleans raw copy; hashing failure releases staged copy; external original is refused without deletion; cancelled picker never stages. Preparation plus cleanup failure now preserves both causes in AggregateError instead of replacing the original error. Fixtures use installed Expo required lastModified field. NOT RUN; native cache/move/cancellation qualification remains open.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V30: signed URL lifetime across local reads — 2026-10-11

V30 multipart URL lifetime after file read hardened: executor revalidates the exact signed part URL immediately before PUT, preventing upload with a capability expired during asynchronous local reads. Added unit cases for URL expiration during read, changed source metadata, queued status-only resume and failed durable Complete acknowledgement with no status/handoff. NOT RUN; end-to-end resume and native storage qualification remain open.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V30: client/server signed URL envelope parity — 2026-10-11

V30 signed part URL envelope aligned with existing server authority: client accepts at most32768 UTF-8 bytes and15-minute lifetime, retains strict greater-than5-second lower horizon; rejects raw CR/LF/NUL, opaque browser-normalized URLs and even empty userinfo. Authored boundary/overlimit/non-ASCII/control/userinfo/opaque unit matrix. NOT RUN; signed URL envelopes are not signature or storage authority proof.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V30: archive entrypoint index and client execution contract — 2026-10-11

V30 function index synchronized by direct edit with five existing archive-upload HTTP endpoints using build-indexes.py record shape; generator already derives them from endpoints.json. Added normative local ZIP cache/resume/URL/ready handoff contracts to15-execution-contracts.md. No generator executed. Historical95-entry registry classifications not silently expanded: five newly indexed entries still require reconciliation into audited implementation registry; overall percent remains null.

NOT RUN; no tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V30: archive API implementation registry reconciliation — 2026-10-11

V30 five archive-upload endpoints reconciled into implementation registry after reading authenticated routes, main dependency wiring and service source. All five partial_runtime_path, no completion credit. Catalogue now100: requires_semantic_review=78, partial_runtime_path=21, missing_runtime_path=1; code presence99.0% is not whole-product readiness. Existing classifications otherwise retained, not re-audited.

Whole-product implementation percent remains null.


## V30: fresh authority before saved upload resume — 2026-10-11

V30 saved uploading resume now performs fresh fenced status before requesting part URLs/reading bytes/PUT/Complete. Non-uploading or expired server state returns status without new mutations, preserving durable identity; fresh active uploading continues. New Begin and queued/aborting existing status-only paths unchanged. Seven state/expiry/positive resume cases authored NOT RUN. Preflight is advisory; server authority checks remain mandatory against subsequent races.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V30: lost Complete acknowledgement recovery scenarios — 2026-10-11

V30 lost Complete response recovery scenarios authored using actual durable attempt store and controller/executor: server accepted-before-response-loss resumes via fresh status without repeated Complete; server did not accept resumes exact persisted ETag and retries same Complete. Both retain original key/upload, never repeat Begin/part authorization/local read/PUT, and never report ready from completing. NOT RUN; no physical S3/SQL race evidence implied.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V30: Begin receipt durable intent fence — 2026-10-11

V30 Begin response journal fence authored: after validated Begin reply, controller re-reads durable pending intent and requires same key/nonexistent checkpoint before persisting receipt, preventing deleted/replaced intent resurrection by late response. Added lost Begin reply, receipt persistence failure with same-key retry, and deleted intent no-PUT/no-resurrection cases. NOT RUN. This client fence does not solve server open Begin remote ambiguity.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V30: shared multipart receipt envelope — 2026-10-11

V30 shared multipart ID envelope authored: nonempty valid UTF-8, <=1024 bytes, no Unicode control characters. Applied to objectstore Begin receipt, completion observation/drain and service/repository Begin/Bind/recovery/revocation paths; empty no-bound-session drain remains allowed. Unit valid boundary/overlength/NUL/tab/CR/LF/DEL/C1/invalid UTF-8 matrix authored. NOT RUN; no migration applied and unknown remote Begin remains unresolved, not inferred from listing.

No tests/builds/validators/migrations/gofmt executed. Overall completion remains unproven.


## V30: Begin dependency authority boundary — 2026-10-11

V30 Begin service dependency boundary hardened: validates reservation token and closed allowed states before replay; validates multipart IDs on fresh/recovered/reconciled paths; writer ticket must match current actor/session as well as tenant/upload/kind; cancellation repeated after Reserve/Recover/Start before storage IO. Seven malformed dependency receipt/actor cases authored with existing success/replay/loser matrix retained. Invalid reconciliation never triggers destructive abort. NOT RUN; native unknown Begin recovery remains open.

No tests/builds/validators/migrations/gofmt executed. Whole-product completion remains unproven.


## V30: status and lookup envelope integrity — 2026-10-11

V30 status/lookup boundary hardened: SQL lookup rejects invalid present multipart ID, multipart on reserved, and absent multipart on uploading/completing/verified; terminal no-bound-session states remain possible. Status service independently enforces public closed error enum before response projection. Added malformed repository zero-expiry/unknown-state/unknown-error/ready-uploading unit cases. NOT RUN; positive SQL row fixtures and full physical ready lifecycle still require qualification.

No tests/builds/validators/migrations/gofmt executed. Whole-product completion remains unproven.


## V30: complete durable execution snapshot fence — 2026-10-11

V30 upload execution journal fence now compares complete canonical durable attempt, not only key/upload/abort intent. Expected snapshot advances only after successful owned checkpoint write, followed by fresh verification; detects changed receipt state, ETags and queued stage during capability IO before read/PUT/Complete. Three mutation scenarios authored NOT RUN. Exclusive execution and durable generation fences retained; this is not proof of distributed/native lifecycle.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V30: cancellation/reset complete checkpoint fence — 2026-10-11

V30 cancellation/reset journal fences strengthened to canonical complete attempt equality. Abort persists owned aborting snapshot then freshly fences before remote mutation and after response/status. Reset refuses any changed checkpoint during physical-cleanup status request before journal mutation/clear. Three checkpoint mutation phases authored; changed record retained, no false success/clear. NOT RUN; existing execution lock remains mandatory and native/full concurrency qualification open.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V30: conditional terminal journal deletion — 2026-10-11

V30 terminal journal clear now requires a mandatory owned expected complete attempt snapshot and compares canonical bytes inside existing journal transaction/lock immediately before deletion. Reset controller passes its exact persisted aborting snapshot; all call sites updated. Added stale-ETag snapshot refusal plus exact successful clear case. NOT RUN; cooperating storage lock semantics remain prerequisite, no native concurrency proof.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V32: remaining Metal budget in heartbeat — 2026-10-11

V32 MPS heartbeat available budget now subtracts current process driver allocations plus64MiB guard from configured10-billion-byte ceiling and takes minimum with measured/policy VRAM. Invalid or already-over-budget driver counters fail closed through existing CapacityReporter stop/abort lifecycle. Added remaining budget/exhaustion/invalid/overlimit cases and service sampling integration unit. NOT RUN; allocator covers PyTorch only, global DB admission and real Metal peak/concurrency qualification remain separate.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V32: Metal budget failure worker lifecycle scenarios — 2026-10-11

V32 authored built-worker lifecycle tests for MPS budget failures: initial valid residual capacity publishes once, subsequent over-budget measurement stops consumer/aborts active work and fails lifecycle without another heartbeat; invalid startup measurement prevents heartbeat and consumer start. Uses actual service/CapacityReporter orchestration with fake hardware/DB/transport; bounded waits authored, NOT RUN. No real Metal memory/concurrency evidence implied.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V32: capacity heartbeat policy/runtime parity — 2026-10-11

V32 capacity policy heartbeat contract aligned with existing CapacityReporter runtime1..10-second limit: Pydantic admission, JSON Schema and spec_v22 source now reject unsupported values at configuration loading rather than late startup. Authored invalid0/11/100/bool/fraction and accepted1/10 reporter construction cases. NOT RUN; no generator executed; existing default10-second Metal/RTX profiles unchanged.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V30: crash cache cleanup on upload route entry — 2026-10-11

V30 crash cache cleanup connected to archive upload route initialization through platform-aware prepareArchiveCache, before principal load/upload screen. Native sweep also retained before picking; web is no-op; cancellation checked before/after synchronous preparation. Three route DOM initialization/order/cache-error/late-principal-after-unmount scenarios authored NOT RUN. Cache cleanup is owned local files only, never server purge or legacy shared picker originals.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V30: explicit route initialization retry and picker binding — 2026-10-11

V30 upload route initialization has explicit retry after cache/principal failure, clearing stale loaded/error state and creating a fresh cancelled-on-teardown request. Added two cache/principal retry DOM cases; mocked screen asserts picker is actual selection function, not cache initializer. Source inspection found/fixed accidental comma-expression picker prop from preceding cache route edit. NOT RUN; no prior build/test success claimed.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V30: ready archive deterministic artifact identity — 2026-10-11

V30 ready artifact identity now verified by client as UUIDv5(upload_id,"archive:artifact:1"), matching Go intent/HTTP projection; matching source SHA with foreign artifact UUID is rejected. Added fixed interoperability vector and foreign namespace/refusal cases; screen ready fixture uses deterministic ID. NOT RUN; SHA1 here is UUID namespace derivation, content integrity remains SHA256. Full native lifecycle unqualified.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V30: fresh status replaces stale ready UI — 2026-10-11

V30 ready handoff screen now replaces stale displayed status with freshly validated/fenced response before readiness decision. Non-ready response removes stale ready message/open action and exposes current pending/aborting/aborted state; no navigation. Expanded DOM non-ready matrix to four server states. NOT RUN; no native or full-module acceptance credit.

No tests/builds/validators/migrations executed. Whole-product completion remains unproven.


## V30: delivery read public resource binding — 2026-10-11

V30 delivery reader now validates <=1MiB public resource envelope, exact root fields, package/tenant/data identity, canonical safe positive revision parity, resource kind and nonnull document SHA/exact manifest binding before returning owned bytes; repeats cancellation after DB response. Added seven foreign identity/revision/kind/extra-field/manifest refusal cases and corrected old placeholder fixture. NOT RUN; metadata validation does not prove physical manifest readiness or full delivery lifecycle.

No tests/builds/validators/migrations/gofmt executed. Whole-product completion remains unproven.


## V30: cached delivery publication response binding — 2026-10-11

V30 delivery publication validates idempotency.Use response on both fresh and cached branches before returning receipt: exact200/resourceID, canonical expected revision+1, tenant/package/manifest/publication identity and common bounded closed public envelope. Error/cancellation cannot return a populated receipt. Added positive and seven malformed cached response cases. NOT RUN; transaction/idempotency physical publication races remain unqualified.

No tests/builds/validators/migrations/gofmt executed. Whole-product completion remains unproven.


## V30: delivery state/publication consistency — 2026-10-11

V30 delivery public reader validates closed package state, requires manifest for review/ready and permits published_at only as valid nonzero timestamp on ready with manifest. Expanded negative resource matrix with four contradictory states/timestamps. Publication repeats cancellation before receipt assignment and returns empty receipt on transaction/commit failure. NOT RUN; metadata consistency is not physical required-bundle or revocation qualification.

No tests/builds/validators/migrations/gofmt executed. Whole-product completion remains unproven.


## V30: delivery HTTP reader boundary — 2026-10-11

V30 delivery HTTP read independently invokes shared ValidPackageResource before emitting JSON, fails unavailable503 on malformed reader envelope without payload disclosure, checks cancellation after reader IO and sets private,no-store. Exported shared validator reused by repository/publication; HTTP success fixture now uses real scoped resource. Added four malformed provider modes and late-cancellation no-body case. NOT RUN; authenticated DB/physical publication lifecycle unqualified.

No tests/builds/validators/migrations/gofmt executed. Whole-product completion remains unproven.


## V30: HTTP publication receipt boundary — 2026-10-11

V30 HTTP delivery publication now validates complete fresh/replayed receipt through shared exported ValidatePublicationResponse before replay header/body. Requires200/exact resource/revision/manifest/publication envelope; cancelled request emits no late body; private,no-store success. Legacy Publish provider projected into same expected receipt envelope. Updated valid provider fixture and added correct replay plus wrong status/resource/private body refusal matrix. NOT RUN; no DB/native lifecycle completion credit.

No tests/builds/validators/migrations/gofmt executed. Whole-product completion remains unproven.


## V30: publication CAS admission parity — 2026-10-11

V30 delivery publication CAS validation shared through ParsePublishRevision: canonical decimal1..9007199254740990 leaves safe increment room; used by service, receipt validation and HTTP before provider invocation. HTTP also requires valid Idempotency-Key before publisher call. Expanded HTTP malformed revision matrix and safe increment boundary unit. NOT RUN; full SQL CAS/replay/concurrent publication lifecycle remains open.

No tests/builds/validators/migrations/gofmt executed. Whole-product completion remains unproven.


### 2026-10-11 — V24 durable upload response binding

V24 studio-agent: Transfer now checks GET upload identity and immutable durable multipart layout before accepting uploading/pending/verified states. Journal admission rejects bool/float/null layout fields before SQLite. Authored 25 negative state/response combinations proving no PUT/Complete/receipt and unchanged journal on mismatch. Source checked against server upload.Session JSON fields. Tests NOT RUN; native camera adapters and full lifecycle qualification remain open.


### 2026-10-11 — V24 admission scope and lost Begin

V24 HTTP capture Begin/Get now validate canonical upload ID, exact configured project/collection and bounded integer multipart layout before journal admission; Begin also verifies expected bytes and exact part count. Authored transport rejection matrix and restart after accepted/lost Begin response retaining deterministic admission key and zero pre-receipt PUTs. Tests NOT RUN; no full module completion claim.


### 2026-10-11 — V24 initializing recovery

V24 initializing recovery: server GetDevice is read-only and ensureMultipartDevice runs through BeginDevice. Transfer now replays the original deterministic Begin for a persisted initializing upload, requires same upload ID/layout and defers PUT until next fresh GET. Authored initialization-to-done, foreign replay refusal and transport failure/backoff tests. NOT RUN; full native/SQL/S3 qualification remains open.


### 2026-10-11 — V24 read-only status and scope

V24 status CLI now opens Journal with SQLite URI mode=ro and query_only, without schema creation/migrations or parent creation; live WAL remains visible (immutable mode intentionally not used). Authored live-writer visibility/write refusal, absent database and legacy schema nonmigration tests. NOT RUN. Scope correction from06-clients-integrations.md: watch-folder is mandatory v2 baseline; native tethering is a separate hardware-qualified package, not automatically a missing baseline requirement.


### 2026-10-11 — V24 bounded source hashing

V24 watch-folder hashing now uses shared bounded exact-size reader for initial stable hash and pre-Complete rehash: unbuffered reads at most admitted size plus one byte, truncation/growth refusal, original metadata checks retained, <=1MiB chunks. Authored continuously growing fake-stream read-bound test and exact/truncated/oversized source cases. NOT RUN; no imaging algorithm changes or module completion claim.


### 2026-10-11 — V24 changing observation queue

V24 growing source queue: Journal.observe transactionally refreshes the latest same-path discovered/stable observation only when SHA/upload ID are absent, preserving sequence and resetting stability time; queue budget accounts replacement delta and rolls back oversized changes. Hashed/admitted entries remain immutable. Authored growing-file budget/stability/new-sequence, stable-unhashed reset and oversized replacement rollback tests. NOT RUN. Historical abandoned observations are not deleted or retroactively marked done.


### 2026-10-11 — V24 configuration boundary

V24 config module introduced and CLI wired before keychain/network: bounded64KiB read, duplicate/nonfinite JSON refusal, closed nonsecret field allowlist, nonzero UUID scope, HTTPS/loopback origin checks and bounded storage allowlist, nonempty paths/operator CA requirement, strict integer queue budget/default. Authored default/development, malformed/oversized/duplicate JSON, field/type/scope/path/limit matrix and CLI no-keychain/no-network-on-invalid tests. NOT RUN; platform keychain and end-to-end qualification still open.


### 2026-10-11 — V24 QA client response boundary

V24 QA client audit: capture/[id].tsx already installs capture.alert SSE plus5s polling; blanket absence claim superseded. Added capture-alert-response runtime decoder wired into OwnerAPI.captureAlerts, enforcing requested session, canonical nonzero IDs, exact positive int64 decimal revisions, severity/stale/timestamps/ack pair, duplicate and page/cursor bounds before display. Verified field shapes against ListAlerts SQL and00267; authored decoder positive/negative matrix. NOT RUN; live SSE/QA lifecycle qualification remains open.


### 2026-10-12 — V24 HTTP transfer restart scenario

V24 authored integrated local HTTP transfer scenario using real HTTPClient/Transfer/WatchFolder/SQLite and actual source bytes: parallel multipart PUT with credential separation, exact completion payload, accepted Complete with dropped response, journal close/reopen/recover, verified GET and bound receipt to done, no reupload or original mutation. The HTTP server is an explicit synthetic fixture and credential token is injected; this is not SQL/S3/ingest/QA/hardware evidence. NOT RUN.


### 2026-10-12 — V25 explicit RAW development foundation

V25 explicit RAW foundation: typed RAWDevelopment in Python/Go (AHD/DCB, camera/daylight/auto WB, EV[-2,3], clip/blend/reconstruct, pinned rawpy/LibRaw version). CompileIngest freezes optional policy into asset.decode Params and PrepareIngestTask recompiles it; nil omits field preserving historical canonical Params. Worker passes policy into rawpy, refuses decoder mismatch and unavailable explicit camera WB, retains legacy defaults when absent, refuses RAW policy on raster. Authored Go compiler/legacy/malformed tests and Python mocked native option/quality-preserving legacy/version/WB/contract matrix; NOT RUN. Public ingest request/snapshot selection and client controls, trusted decoder qualification/version installation, real camera matrix and image-quality qualification remain open; this is not complete V25.


### 2026-10-12 — V25 versioned RAW ingest snapshot

V25 RAW snapshot reader supports strict version2 carrying required RAWDevelopment; version1 retains exact historical shape and rejects policy injection. Reader preserves policy into IngestSpec then compiler/task Params; nullable/missing v2 policy and invalid/raster policy refused. Authored v2-to-task exact policy and v1 injection/raster/schema/WB/decoder negative tests, existing positional legacy fixture updated. NOT RUN. Public upload admission and completion SQL remain v1; coordinated forward-only schema/admission/completion expansion is still required before publishing RAW selection UI.


### 2026-10-12 — V25 persisted RAW admission/completion

V25 RAW admission/completion authored: shared rawdevelopment.Policy with strict complete JSON decoder; user/device Begin validates/copies and persists optional policy, completion claim v2 reads it and snapshot documents carry v2 only when present. Forward-only01618 adds checked immutable upload column, scheduler-only claim wrapper preserving original signature, and current finish/fail definitions with exact policy-bound snapshot gates for user/device/guest branches. No migration applied or checks run. Authored strict JSON, normalization copy/raster refusal and completion v2/legacy tests. SQL lifecycle/ACL/idempotency/restore tests, public schema/client configuration and trusted native qualification remain open; this increment is unqualified.


### 2026-10-12 — V25 RAW SQL/S3 completion scenarios

V25 completion integration source extended from2 to4 cases (legacy/RAW x unknown before/after storage Complete), with actual existing PostgreSQL API/scheduler roles and MinIO fixture: RAW admission replay, immutable policy update refusal, API denied completion-v2 claim privilege, exact v2 snapshot policy, unchanged v1 and existing physical/quota/replay checks. Synthetic bytes intentionally do not claim real RAW decode/camera qualification. Added explicit rawPolicyValue SQL NULL for absent policies in user/device inserts. Tests and01618 NOT RUN/APPLIED; public schema/UI and broader SQL ACL/restore qualification remain open.


### 2026-10-12 — V25 client RAW request persistence

V25 public client contract foundation: exported strict RAWDevelopment parser and raw-suffix validator; UploadQueue copies policy before initial persistence, restores with validation and retains it on resume; OwnerMultipartAPI sends exact raw_development only when selected. Added RAWDevelopment/V25UploadBegin JSON schemas and generator source declarations without running generator. Authored parser negative matrix and queue persistence/restart tests. NOT RUN. Selection UI awaits qualified decoder capability source; no default/native version guessed or feature qualification claimed.


### 2026-10-12 — V25 qualified RAW capability boundary

V25 RAW capability verification foundation: private immutable Capability constructed through existing v2qualification.Verify with independently trusted operation/version/CPU/runtime/model/device/dataset/issuer/fixture/evidence pins, mandatory checks for camera matrix, both demosaics, all WB/highlight modes, exposure bounds, ICC roundtrip and original preservation. Owned public view and exact decoder policy matching; nil/zero capability denies. Authored complete report fixture plus every failed-check/wrong-runtime/version/ownership tests, NOT RUN. No actual report qualification or public deployment/catalogue/admission wiring established; UI remains deferred until that source exists.


### 2026-10-12 — V25 RAW startup/restore/replay binding

V25 RAW maintenance integration: scheduler startup now requires completion claim-v2 EXECUTE and actual upload policy column (pg_attribute avoids information_schema privilege filtering).01618 policy/config exact gates moved ahead of successful/failed terminal replay so completed uploads cannot bypass RAW binding. Existing SQL/S3 RAW completion tests now simulate restore lease expiration, verify unchanged persisted policy, then recover through the normal scheduler claim. Tenant purge continues deleting the existing upload_sessions row/column; no new tenant metadata table or DELETE grants introduced. NOT RUN/APPLIED; full restore/replay qualification and public capability installation remain open.


### 2026-10-12 — V25 physical RAW qualification source

V25 authored RAW qualification physical source:01619 API-only SECURITY DEFINER lookup requires current editor authorization, trusted passed asset.decode/1/cpu qualification, matching enabled/license-cleared operator policy and exact expected locks/issuer, ready verified versioned unexpired JSON report. FOR SHARE holds qualification/artifact/policy during caller transaction. rawdevelopment.Source reads bounded exact object bytes, VerifyCapability binds SHA/report/deployment checks, re-queries authority/descriptor and fences cancellation. Authored positive and missing/foreign-bucket/revoked/version-change/corrupt/cancelled source tests. NOT RUN/APPLIED; service/main capability catalogue and admission wiring still required.


### 2026-10-12 — V25 bounded physical qualification reads

V25 physical qualification report reads bounded by locked descriptor bytes+1, nil stream refusal, cancellation before body read and close on cancellation; authored oversized/truncated/cancelled/nil-stream resource tests. NOT RUN; public capability catalogue and upload admission wiring remain open; no whole-product completion credit.


### 2026-10-12 — V25 transaction-bound policy admission

V25 transaction-scoped Source.Admit freezes explicit policy before adapter I/O, verifies current physical qualification and exact decoder match, fences cancellation, preserves nil legacy policy without qualification I/O. Authored valid/ownership/legacy/invalid/version/revocation/cancellation tests. NOT RUN; upload Service/main/catalogue wiring remains open, so no complete admission or module claim.


### 2026-10-12 — V25 owner admission wiring

V25 owner Upload.Begin now checks RAW admission inside tenant transaction before idempotency.Use, including replay, after project authorization. Missing installation fails closed; returned policy must exactly match owned request. Completion SQL/S3 fixture explicitly injects synthetic admission (not native evidence). Authored boundary tests. NOT RUN; production Source/main deployment configuration, capability catalogue/UI and device authority remain open.


### 2026-10-12 — V25 production RAW source installation

V25 API production wiring via optional PHOTO_RAW_QUALIFICATION_FILE: strict bounded operator JSON/pins loader with file identity/mode/change checks, exact asset.decode/1/cpu and decoder validation; SQL function availability check and physical Source installation into owner upload service. Added strict config/file tests and deployment instructions. NOT RUN; real qualification provisioning, device authority, capability catalogue/UI and full lifecycle qualification remain open.


### 2026-10-12 — V25 project RAW capability catalogue

V25 added project-scoped RAWCapabilities service and authenticated GET raw-capabilities with editor authorization, current physical report verification, unavailable response for absent/revoked qualification, cancellation fence and private/no-store. Completion integration fixture now checks synthetic adapter cannot advertise capability and rejects foreign project. Added dependency/scope units and API documentation. NOT RUN; catalogue machine contracts/client/UI and device authority still open.


### 2026-10-12 — V25 qualified RAW client catalogue

V25 exported strict project-bound RAW capability client decoder, owned option arrays and explicit selection helper with decoder bound to qualification; OwnerAPI.rawCapabilities validates scope and cancellation before/after transport. Authored scope/modes/version/ownership/unavailable-selection tests. NOT RUN; mounted selection UI, machine catalogue registration and device authority remain open.


### 2026-10-12 — V25 mounted RAW upload selection

V25 mounted RAWUploadSettings in imports/new: current qualified catalogue, explicit opt-in demosaic/WB/highlights/EV, invalid exposure disables admission, scope-bound selection, snapshot before picker/hash, RAW-only enqueue policy and unchanged resume policy. Authored DOM explicit/invalid/unavailable/unmount tests. NOT RUN; current new UI labels remain English like existing import screen; localization, machine catalogue contracts and device authority remain open.


### 2026-10-12 — V25 RAW localization and capability schemas

V25 RAW upload UI localized in en/ru/bg including WB/highlight option labels and validation; locale changes do not reset selected policy. DOM test uses English catalogue. Added strict RAWCapabilityView/RAWCapabilities schema definitions and generator declarations without generator execution. NOT RUN; endpoint machine registry/OpenAPI registration and device authority remain open.


### 2026-10-12 — V25 RAW catalogue contract registration

V25 GetRAWCapabilities registered in endpoint catalogue, generator source, OpenAPI and function/progress index. Registry now101 entrypoints:79 requires_semantic_review,21 partial_runtime_path,1 missing_runtime_path; code presence100/101=99.01%, not completion. NOT RUN; no generator/validator/test execution; whole-product implementation remains null.


### 2026-10-12 — V25 device RAW qualification admission

V25 device RAW admission wired before idempotency replay with distinct Source.AdmitDevice and forward-only01620: active epoch/tenant/project fence, locked open shoot-session/collection and current physical qualification. API startup requires both lookup functions. Added distinct authority/version/epoch/revocation tests and deployment update. NOT RUN/APPLIED; positive SQL/device lifecycle, real camera/quality acceptance and client device RAW selection remain open.


### 2026-10-12 — V25 device RAW transaction/replay scenarios

V25 authored PostgreSQL/API-role device upload scenarios: explicit RAW without source refuses before storage/persistence, synthetic qualified adapter proves tenant transaction context and checks on both initial/replay, revoked qualification refuses replay, rows/reservations remain singular. Explicit fixture is not physical/native qualification evidence. NOT RUN;01620 NOT APPLIED; positive physical qualification SQL, native camera quality and broader lifecycle remain open.


### 2026-10-12 — V25 native RAW tensor/WB boundary

V25 native RAW boundary reparses model_copy/model_construct settings, validates explicit camera WB shape/types/finite channels while accepting valid zero fourth channel, and enforces RGB16 positive geometry before ICC. Added malformed-model/WB/output tests. Owner/device qualification refusal now maps to public503 QUALIFICATION_UNAVAILABLE retaining sentinel cause. NOT RUN; valid native option/ICC algorithms unchanged; real camera and full module qualification remain unproven.


### 2026-10-12 — V30 durable pre-Begin intent fence

V30 archive controller now re-reads exact durable intent immediately before Begin and compares complete canonical intent after response, refusing removal/rekeying before network; authored mutation-before-network tests. Source audit confirms unknown S3 Begin receipt leaves an open writer; this server reconciliation gap remains open. NOT RUN; no claim of unknown-remote-outcome recovery or V30 completion.


### 2026-10-12 — V30 read-only multipart discovery evidence

V30 added read-only exact-key multipart discovery evidence with unique valid session requirement, duplicate/multiple refusal, bounded listing and cancellation-aware receive. Authored exact/foreign/empty/ambiguous/error/cancelled tests. Empty listing explicitly cannot authorize new Begin or close unknown writer; SQL reconciliation not yet wired. NOT RUN; unknown remote Begin outcome remains open until writer/receipt reconciliation authority is implemented.


### 2026-10-12 — V30 transaction-bound open writer inspection

V30 authored01621 authenticated open-Begin writer inspection and SQLArchiveBeginInspector: exact reservation/actor/session/active-project authority, single open writer, transaction-held authority and read-only storage discovery with before/after token binding. Does not close writer: source analysis shows listing cannot prove original remote Create terminated, so automatic close would invalidate purge safety. NOT RUN/APPLIED; unknown-outcome reconciliation still requires proven writer quiescence; inspector not yet exposed/wired.


### 2026-10-12 — V30 open writer SQL isolation fixture

V30 authored01621 PostgreSQL fixture with real photo_api role: creates reserved archive/open writer, verifies exact visible token and tenant/actor/session/upload/reservation substitution refusals, API-only grants, and unchanged open/no-receipt/reserved ledger after inspection. All fixture rows roll back. NOT RUN; inspection is read-only evidence, not writer quiescence or automatic unknown-outcome recovery.


### 2026-10-12 — V31 signer registration installation fence

V31 dedicated signer composition now independently requires exact provenance.sign-only CPU SQL registration before and after executor factory, refusing empty/mixed/changed registrations before transport construction. Updated composition fixtures and authored registration race tests. NOT RUN; deployed native SDK/key/TSA qualification and production launch wiring remain open; missing runtime classification unchanged.


### 2026-10-12 — V25 native RAW cancellation and geometry

V25 RAW decoder now receives task cancellation guard from ICC worker and checks before import/read, after native handle entry, before/after postprocess; native dimension types/positive sizes validated and Python-int multiplication prevents NumPy overflow bypass. Authored cancellation/handle-close and malformed/overflow geometry tests. NOT RUN; does not claim interruption inside a native call; cancellation discards its returned result and closes handle. Valid processing options remain unchanged.


### 2026-10-12 — V25 RAW catalogue HTTP response boundary

V25 RAW catalogue HTTP boundary extracted with exact project/availability/view/decoder/mode-set/EV validation before success, cancellation before/after provider and private/no-store. Authored valid/unavailable/substitution/missing-hidden-view/duplicate/version/bounds/cancel matrix. NOT RUN; does not substitute physical capability/source or real native acceptance.


### 2026-10-12 — V32 bounded capacity policy installation

V32 capacity policy loader now uses O_NOFOLLOW/O_NONBLOCK bounded regular-file descriptor read, exact admitted length and inode/size/time/mode comparison before/after/path, preventing symlink replacement/FIFO/growth from bypassing operator resource budget. Authored replacement/FIFO/symlink/oversize tests. Metal verification policy remains10,000,000,000 bytes. NOT RUN; no native hardware/heartbeat/lifecycle acceptance claimed.


### 2026-10-12 — V25/V26 import queue tenant/principal scope

V25/V26 import queue now scoped by origin/authenticated /me user/tenant/project/collection rather than shared legacy key; route loads principal before queue creation. Scoped storage refuses foreign rows, copies records, logout clears origin rows and fences stale instances with epoch/generation. Legacy rows lack owner provenance and are not restored. Authored isolation/logout/foreign-row tests. NOT RUN; upload active-task shutdown/unmount lifecycle still requires completion; no complete module credit.


### 2026-10-12 — V25/V26 upload queue shutdown lifecycle

V25/V26 UploadQueue local close aborts active signals, clears source/listeners and prevents late Begin/sign/read/PUT/Complete responses from advancing or persisting. Pump handles admission persistence failure and no longer restarts closed queue. Import route creates queue per effect lifecycle (StrictMode-safe) and closes on cleanup. Authored five delayed-boundary and restore-after-close tests. NOT RUN; remote unknown outcomes retain durable keys for replay; explicit user abort remains separate from local shutdown.


### 2026-10-12 — V25/V26 multipart batch response admission

V25/V26 generic UploadQueue validates bounded integer source/layout and exact ceil part count before assignment/part allocation, enforces unique requested batch numbers and bounded nonempty URLs before reads/PUT, validates returned ETags before checkpoint/Complete. Resume now uses guarded pump scheduling. Authored malformed-layout/batch/etag pipeline tests. NOT RUN; concrete owner transport URL/session-scope checks and broader import lifecycle still require implementation/review.


### 2026-10-12 — V25/V26 owner multipart wire binding

V25/V26 OwnerMultipartAPI now decodes unknown Begin responses against owned project/collection/name/size and fixed16MiB layout, and binds signed batch upload ID/unique requested parts/expiry/credential-free HTTPS URLs (HTTP loopback only for loopback development API). Added cancellation fences and response decoder tests. NOT RUN; restored upload current-state recovery and native/source immutability remain open.


### 2026-10-12 — V25/V26 upload current-state recovery

V25/V26 owner upload status GET now bound to frozen scope/layout before parts; queue recovers lost Complete from completing/verifying/verified without reupload, waits for verified before complete, maps rejected/expired/aborted, replays initializing Begin with same key/identity then fresh status. Import polls completing every5s and restore preserves waiting stage. Authored remote-state/no-reupload/substituted-ID tests. NOT RUN; current SQL/native/browser lifecycle acceptance remains open.


## Продолжение 2026-10-12: целостность файла перед Complete

Web/native источники фото выполняют повторное потоковое SHA256-чтение блоками1МиБ перед Complete; очередь сравнивает результат с сохранённым SHA256. Изменившийся файл отклоняется с UPLOAD_FILE_MISMATCH; cancellation после проверки запрещает поздний Complete. Добавлены unit-сценарии границ/остатка, короткого чтения, отмены, изменения после PUT и shutdown во время verify. NOT RUN. Это локальная предварительная проверка, не гарантия неизменяемости файловой системы и не замена серверной проверки checksum объекта. Процент всей реализации не установлен; модуль целиком не зачтён.


## Продолжение 2026-10-12: проверка multipart checkpoint

Восстановление очереди до замены records проверяет согласованность ограниченной multipart-разметки, уникальность localId и номеров частей, диапазон целых part_no, непустые безопасные ETag и отсутствие частей/layout без uploadId. Части копируются и сортируются. Перед продолжением и Complete checkpoint проверяется снова; Complete требует полного покрытия. Конструктор запрещает NaN/дробную/бесконечную concurrency. Добавлены unit-сценарии повреждения, aliasing, duplicate identity и concurrency. NOT RUN; полный модуль не зачтён, общий процент не установлен.


## Продолжение 2026-10-12: последовательность сохранения и pause/resume

UploadQueue сохраняет отдельные снимки последовательно, включая копию RAW policy; ожидающие записи пропускаются после close, отказ одной записи не блокирует последующие. Pump исключает незавершённые running attempts. Resume активной неотменённой/refreshing попытки возвращает UPLOAD_BUSY; после pause очередь сохраняет queued restart до завершения отменённой попытки. Отмена admission не превращает queued resume в error. Добавлены unit-сценарии delayed persistence/close и единственной активной попытки при pause/resume. NOT RUN; дальнейшие abort/refresh/restore гонки и native/browser acceptance остаются открытыми.


## Продолжение 2026-10-12: Abort и поздний refresh

Explicit Abort регистрирует controller для отмены при close, отменяет текущий refresh и запрещает конкурирующие resume/pump/refresh; terminal записи пропускаются. Поздние ответы transport не продвигают состояние после close. Pending Begin без uploadId сохраняет UPLOAD_ABORT_RETRY_REQUIRED и durable key для replay; remote abort не считается подтверждённым. Добавлены unit-сценарии stale status против Abort и close во время Abort. NOT RUN; unknown Begin требует replay и последующего server Abort, автоматическая очистка не заявлена.


## Продолжение 2026-10-12: управление импортом в UI

Экран импорта подключает pause/Abort очереди через UploadItemControls. Подписи состояний и восстановления переведены en/ru/bg. Синхронный lock строки и busy блокируют повторные действия; complete/aborted не показывают destructive controls. Позднее обновление action state/error ограничено текущим queue identity. Добавлены DOM-тесты actions/busy/terminal/recovery, NOT RUN. Полная route/browser/native приёмка и остальная локализация/отображение ошибок открыты; модуль целиком не зачтён.


## Продолжение 2026-10-12: отмена выбора и начального хеширования

Web/native initial hash использует bounded exact-length reader и AbortSignal; native initial reads передают управление между блоками. Import selection блокирует повторный запуск controller lock, отменяется при cleanup и проверяет signal после picker/перед enqueue; поздние catch/finally ограничены queue identity. Основные подписи/сообщения импортера переведены en/ru/bg. Добавлены source cancellation unit tests, NOT RUN. Системный picker не закрывается принудительно; поздний выбор игнорируется. Полная route/native приёмка и structured error localization открыты.


## Продолжение 2026-10-12: отменяемая повторная проверка и restore gate

Общий integrity reader передаёт управление между блоками1МиБ через отменяемую macrotask; это относится к initial и pre-Complete hash. Добавлен unit-сценарий scheduled navigation cancellation до второго чтения. Import блокирует добавление файлов до завершения durable restore; ошибка показывает retry с новым lifecycle очереди. NOT RUN; native/route acceptance открыты. Generic queue callers по-прежнему должны соблюдать порядок restore/admission.


## Продолжение 2026-10-12: завершение параллельного multipart пакета

Обработка пакета использует allSettled и удерживает running attempt до завершения всех запущенных read/PUT даже при ошибке одного. Failed batch не принимается в checkpoint; повтор не перекрывается с незавершёнными PUT прежней попытки. Добавлены unit-сценарии delayed sibling failure и pause/resume drain. NOT RUN; transport timeout и browser/native acceptance открыты. Успешная удалённая часть failed batch может быть перезаписана повтором с тем же part_no; ложный checkpoint не принимается.


## Продолжение 2026-10-12: ограниченный PUT transport

Owner PUT использует120s abort timeout, parent cancellation и post-response fence; credentials omit, redirect error и no-referrer заданы явно. Проверяются bounded nonempty part bytes и безопасный ETag; timer/listener/body освобождаются. Добавлены timeout/caller cancellation/request isolation/invalid-size unit tests, NOT RUN. Timeout не доказывает удалённый rollback; неопределённый PUT повторяется с тем же multipart part_no. Browser/CORS/native acceptance открыты.


## Продолжение 2026-10-12: Abort восстанавливает потерянный Begin

При отсутствии uploadId Abort повторяет Begin с исходным localId/key, проверяет recovered layout, сохраняет identity до server Abort и не вызывает sign/read/PUT/Complete. Failure сохраняет ключ для retry. Добавлены success/offline retry/malformed recovery tests, NOT RUN. Это заменяет прежний UPLOAD_ABORT_RETRY_REQUIRED-only путь. Для never-started durable row replay может сначала создать сессию; актуальные authorization/quota/RAW qualification могут отказать, тогда aborted не заявляется. Backend unknown S3 Begin — отдельный открытый контракт.


## Продолжение 2026-10-12: ручная сверка серверного состояния

Import rows с uploadId в paused/error/completing показывают локализованную проверку статуса под per-row action lock. Подтверждённые server states снимают устаревшую local error. Добавлены DOM action/busy matrix и unit-сценарий lost Complete→verified из persisted error без local file/sign/Complete. NOT RUN; полная route/native приёмка и rejected/expired recovery UX открыты.


## Продолжение 2026-10-12: restart terminal uploads и Abort preflight

Rejected/expired записи не возобновляются: reselect создаёт новую загрузку с новым ключом; UI объясняет restart en/ru/bg. Direct resume известных terminal errors отклоняется. Перед Abort выполняется status reconciliation; completing/verifying/terminal не отправляют недопустимый server Abort, cancel скрыт для completing и known terminal errors. Добавлены unit/DOM terminal resume, verifying reconciliation, restart tests. NOT RUN; изменение server state между preflight/Abort требует последующей сверки при отказе API.


## Продолжение 2026-10-12: интеграционные тесты import route

Добавлены DOM route tests: authenticated queue scope, admission gate до restore, explicit retry с close старой очереди, unmount close, late picker discard и identity failure без queue creation. Route очищает stale error/list при начале retry restore. NOT RUN; mocked boundaries не подтверждают runtime acceptance. Real storage/network/native end-to-end qualification остаётся обязательной.


## Продолжение 2026-10-12: полные метаданные persisted upload

До atomic restore и enqueue проверяются bounded nonempty scope/local identity, filename shape/255 codepoints, положительный bounded size, nonnegative safe-integer mtime, lowercase SHA256, известное local state, bounded string error и session identity для completing/complete. Добавлена malformed metadata unit matrix, NOT RUN. Concrete scoped storage сохраняет UUID/owner проверки; metadata validation не заменяет server authorization и file authenticity.


## Продолжение 2026-10-12: exclusive restore очереди

UploadQueue защищает restore отдельным lifecycle lock: concurrent restore/admission/mutations отклоняются, непустая/активная очередь не заменяется. Finally снимает lock после ошибок load/validation/persistence. Добавлены concurrent load/admission, failed-load retry и repeated restore unit tests, NOT RUN. После failure уже установленного snapshot нужен fresh queue для retry, как реализовано в import route. Полная приёмка не подтверждена.


## Продолжение 2026-10-12: bounded source range и native handle tests

Web/native read проверяет safe integer bounded range до file access; native adapter проверяет exact byte length/cancellation и закрывает handle через finally. Добавлены native mock tests: same-size content change, changed size, throwing/short read, invalid ranges, pre-cancel, open/close balance. NOT RUN; mock fixture не заменяет real Expo/iOS/Android filesystem acceptance. Настройки обработки изображений сохранены.


## Продолжение 2026-10-12: V31 cancellation deployment/PIN

Bounded deployment reads проверяют task cancellation до file/directory IO, вокруг каждого read и до передачи bytes, включая lazy PIN. Finally закрывает descriptor при отмене. Deployed loader работает в explicit cancellation scope. Dedicated signer отклоняет ICC installation наряду с imaging runtimes. Добавлены pre-open cancellation, cancellation-after-PIN-read/descriptor close и ICC isolation tests, NOT RUN. Native SDK/key/TSA/trust qualification и production launch не подтверждены; missing provenance.sign.execute остаётся.


## Продолжение 2026-10-12: V31 operator-owned deployment directories

Descriptor traversal проверяет root/nested directories на отсутствие group/other write до чтения deployment/PIN. Directory opener проверяет cancellation и закрывает fd при отказе. Добавлены writable-root matrix, nested PIN-directory refusal и descriptor closure tests, NOT RUN. Права каталогов дополняют pinned bytes/native authority и не подтверждают SDK/key/TSA qualification; production signer остаётся открытым.


## Продолжение 2026-10-12: V32 фактический Metal allocator ceiling

Built worker сохраняет MetalBudget receipt установки allocator и передаёт каждому accelerator capacity sample. Remaining budget ограничен installed allocator_bytes, а не большим configured ceiling; receipt проверяется по типу/configured limit/integer range. Добавлены lower-recommended-working-set и invalid-receipt tests, NOT RUN. Общий потолок остаётся10 000 000 000 bytes; hardware acceptance отложена. Внешние Metal libraries и node-wide admission этим не заменяются.


## Продолжение 2026-10-12: V32 обязательная MPS capacity policy

MPS startup требует explicit PHOTO_CAPACITY_POLICY при env parsing и direct allocator installation; отсутствие отклоняется до PyTorch. Verification fixture использует configs/capacity-metal-verification.json с10 000 000 000 bytes. Добавлены missing-policy/fixed-budget tests, существующие lifecycle doubles остаются hardware-free. NOT RUN; CPU/CUDA policy optionality сохранена, Metal acceptance отложена.


## Продолжение 2026-10-12: V32 CPU thread budget

Shared built-worker lifecycle применяет CapacityPolicy.cpu_threads_per_task через threadpoolctl BLAS/OpenMP и loaded PyTorch/OpenCV setters, не превышая host CPU count. ExitStack восстанавливает прежние настройки при normal/error exit. Добавлена dependency threadpoolctl>=3.6,<4 и apply/restore/invalid-limit/host-cap tests. NOT RUN, dependency не устанавливалась. Поздно загруженные libraries и отдельные процессы требуют deployment-level limits; quality/determinism acceptance обязательна.


## Продолжение 2026-10-12: V32 thread budget до build

Thread budget охватывает main run_worker construction и built-worker execution. OMP/OpenBLAS/MKL/NumExpr/vecLib environment caps задаются для поздних native imports и восстанавливаются при выходе; applied getter types/ranges проверяются. Добавлены environment restoration/nested-scope tests, NOT RUN. Libraries, игнорирующие standard controls, требуют independent qualification/deployment limits; quality/hardware acceptance не заявлена.


## Продолжение 2026-10-12: V32 policy SHA heartbeat

Forward-only01622 добавляет operator `worker_registrations.capacity_policy_sha`, observed `reported_capacity_policy_sha` и `capacity_policy_reported_at`. Worker передаёт canonical SHA загруженной CapacityPolicy через `heartbeat_worker_capacity_v2`; SECURITY DEFINER проверяет session_user/worker/node и exact operator pin под locks node→registrations. Прежние resource/operation checks сохранены в owner-only implementation. Legacy heartbeat не обновляет pinned registrations. CPU/media/GPU wire/pin/routing tests написаны, NOT RUN. Миграция NOT APPLIED. SQL role/concurrency integration и archive-class heartbeat остаются открытыми.

Порядок rollout: остановить/дренировать конфигурируемые воркеры; применить01622 штатным migrator; вычислить SHA тем же canonical CapacityPolicy.sha256 на доверенном config; оператором записать exact SHA во все enabled registration rows данного worker; установить тот же config и обновлённый worker; запустить и проверить reported SHA/time и last_seen. Worker не может сам назначить expected pin. Null pin оставлен только для legacy воркера без CapacityPolicy; policy-aware heartbeat с null/foreign pin возвращает false. После pinning legacy heartbeat отклоняется. Несовпадение требует операторского исправления deployment, автоматическое принятие worker-provided SHA запрещено. SQL evidence/реальный rollout не выполнены; схема не объявлена принятой.


## Продолжение 2026-10-12: V32 archive policy heartbeat

Неприменённая01622 расширена archive CPU heartbeat с existing registration/qualified operation/node capacity checks. Добавлены archive Python routing и rolled-back SQL integration fixture: exact/wrong SHA, unknown/disabled worker, observed pin/time, RAM/scratch, pinned legacy refusal, private/API/table ACL. NOT RUN,01622 NOT APPLIED. SQL concurrency/mixed-node/accelerator/CPU matrix и rollout открыты; hardware/module acceptance не заявлена. Это закрывает прежнее отсутствие archive ветви в новом policy heartbeat коде, не его приёмку.


## Продолжение 2026-10-12: V32 SQL resource matrix

Написана rolled-back SQL policy heartbeat matrix CPU/media/GPU CPU/MPS/CUDA и CPU на mixed GPU node: qualified-operation mismatch, wrong SHA, node capacity clamping, сохранение mixed-node GPU-authoritative counters, operator pin rotation и отказ old pin. NOT RUN; SQL concurrency/реальные role connections/rollout acceptance открыты. Synthetic registration/measurements не являются native hardware qualification.


## Продолжение 2026-10-12: V32 policy authority независимо от last_seen

Forward-only01623 усиливает central worker_authorized: existing principal/operation/V31 pool checks сохранены, pinned SHA должен совпадать с observed SHA, report age<30s, future timestamp запрещён. Task heartbeat last_seen не заменяет свежесть capacity policy report. SQL matrix дополнена no-report refusal, fresh acceptance, stale report при fresh last_seen, refresh report и operator pin rotation authority. NOT RUN;01622/01623 NOT APPLIED. Rollout требует обе миграции до запуска pinned workers. SQL concurrency и фактические claim/renew lifecycle scenarios открыты.


## Продолжение 2026-10-12: V32 policy gate до dispatch

Forward-only01624 добавляет matching/fresh capacity policy predicate в eligible_dispatch_nodes, reserve_dispatch_resources, targeted reserve_dispatch и обе style-analysis runtime selections. Текущие определения изменяются только после exact freshness-predicate occurrence checks; fairness/resource/model/V31 logic/ACL сохраняются. Добавлен functional style runtime SQL fixture: absent/fresh/stale/mismatched report при fresh last_seen. NOT RUN;01622–01624 NOT APPLIED. Dispatch task fixtures/concurrent pin rotation и фактическое SQL исполнение открыты; unexpected definition drift блокирует миграцию. Rollout требует все три миграции до старта pinned workers.


## Продолжение 2026-10-12: V32 dispatch task fixture

Добавлен rolled-back SQL scenario под photo_scheduler с persisted ready task/run/node/slot/worker. eligible_dispatch_nodes проверяется при absent/fresh/stale/mismatched/future policy report и legacy null pin. NOT RUN;01622–01624 NOT APPLIED. Reservation/claim/concurrent rotation и фактическое SQL исполнение открыты; процент завершённости не повышен.


## Продолжение 2026-10-12: V32 reservation отказ без побочных записей

Dispatch fixture вызывает public reserve_dispatch под photo_scheduler при absent/stale pinned report, включая устаревание после успешного eligible selection. Ожидает waiting_worker/null command и проверяет ready task, свободный slot, отсутствие reservation/outbox event. NOT RUN;01622–01624 NOT APPLIED. Успешный reserve, claim/renew и concurrent pin rotation открыты.


## Продолжение 2026-10-12: V32 successful reservation fixture

После восстановления fresh matching policy SQL fixture вызывает reserve_dispatch под scheduler и проверяет queued task, exact worker target, worker routing prefix, slot ownership, RAM/VRAM/scratch reservation и одинаковый command в ответе/task/outbox. Savepoint rollback сохраняет свободный slot для legacy case. NOT RUN;01622–01624 NOT APPLIED. Claim/renew/concurrency acceptance открыты.


## Продолжение 2026-10-12: V32 claim/renew policy lifecycle fixture

В public worker-role lifecycle добавлены stale report после reserve → unavailable claim без lease/attempt; fresh report → claimed; stale report → отказ heartbeat без изменения task lease/progress; refreshed report → successful heartbeat. Все действия внутри rollback fixture. NOT RUN;01622–01624 NOT APPLIED. Фактическое SQL исполнение и concurrent pin rotation остаются открытыми.


## Продолжение 2026-10-12: owner filename normalization

Owner multipart adapter согласует filename с backend strings.TrimSpace для Begin, format/RAW policy и Begin/status binding. Durable local filename/key не меняются; U+0085 учитывается, U+FEFF сохраняется как в Go. Добавлены whitespace matrix и adapter recovery tests. NOT RUN; browser/native integration открыта.


## Продолжение 2026-10-12: admitted upload source ownership

Enqueue/resume сохраняют frozen source wrapper с captured metadata/read/verify. Проверки до/после await отклоняют caller mutation metadata/methods как UPLOAD_FILE_MISMATCH; final verifier нельзя молча заменить/удалить. Добавлена deferred Begin mutation matrix, NOT RUN. Public mutable records и browser/native acceptance остаются открытыми.


## Продолжение 2026-10-12: detached multipart adapter records

Все Begin/status paths получают detached record/parts/RAW policy; adapter не меняет internal durable identity/checkpoint. Общий copy helper используется и persistence snapshot. Добавлен unit scenario adapter mutation+offline failure, NOT RUN. Public queue.records ownership остаётся открытым.


## Продолжение 2026-10-12: signed batch ownership

Sign adapter получает copy requested parts; validated signed part_no/URL копируются до asynchronous read. Adapter mutation не меняет membership validation или URL/checkpoint во время чтения. Добавлены unit scenarios requested-array mutation и late signed-link mutation. NOT RUN; browser/native acceptance и public record ownership открыты.


## Продолжение 2026-10-12: PUT byte ownership

Queue требует Uint8Array exact part length и копирует bounded bytes перед PUT, изолируя reusable source buffer/subarray. Добавлены wrong-type matrix и delayed PUT buffer mutation test, NOT RUN. Extra copy≤16MiB на active part при existing 2 files×2 parts; final digest/server integrity checks сохранены.


## Продолжение 2026-10-12: threadpoolctl lock entry

workers/uv.lock дополнен threadpoolctl3.6.0 и root dependency metadata из официального https://pypi.org/pypi/threadpoolctl/3.6.0/json (exact URLs/SHA256/sizes/timestamps; transitive dependencies отсутствуют). Resolver/generator/install/build NOT RUN. Existing restore/signer-key extras отсутствуют в lock и остаются отдельным packaging gap; frozen build не заявлен прошедшим. Native signer provisioning по-прежнему открыт.


## Продолжение 2026-10-12: restore/signer-key dependency graph

Вручную добавлены lock extras restore/signer-key, cryptography46.0.3 (existing cffi dependency), python-pkcs110.9.3, asn1crypto1.5.1 и exact official PyPI distribution metadata. MinIO согласован на7.2.18 для existing restore exact pin и root range. Imaging versions сохранены. Resolver/generator/install/build NOT RUN; manual graph требует deferred uv frozen validation. Native signer qualification/production launch не закрыты.


## Продолжение 2026-10-12: signer construction CPU budget

run_signer_worker/run_deployed_signer используют shared main-thread capacity CPU budget wrapper до deployment/native executor construction и shared loop. Добавлен scope/exception restoration test, NOT RUN. Native provisioning/CPU qualification не подтверждены; imaging processing logic не менялась.


## Продолжение 2026-10-12: capacity stop buffered AMQP delivery

Transport.consume возвращается до connection при установленном stop; delivery callback после stop не запускает executor и оставляет delivery unacked для requeue при close. Добавлены two mocked transport unit scenarios, NOT RUN. Real broker/thread race acceptance открыта; central SQL policy gate сохранён.


## Продолжение 2026-10-12: policy lease renewal cadence

Shared built-worker lifecycle устанавливает executor.heartbeat_seconds из policy.renew_seconds до IO. Python/schema bound1..10 соответствует existing Executor support. Добавлены bounds/wiring unit tests, NOT RUN. Configurable offline_seconds vs SQL fixed30s и lease_seconds vs SQL lease duration остаются явными открытыми контрактами; этот increment их не закрывает.


## Продолжение 2026-10-12: exact capacity acceptance

CapacityReporter принимает только boolean True; truthy string/int/object не подтверждают registration. Добавлены refusal matrix, success и stop/failed-before-abort-error tests. NOT RUN; real SQL/broker lifecycle не подтверждён. Дополнительных кодовых представлений renew_seconds вне Python и documentation schema текущий поиск не обнаружил.


## Продолжение 2026-10-12: shutdown cleanup despite abort failure

Shared lifecycle finally гарантирует reporter join и signal restoration при abort_active exception. Transport сохраняет close/bounded wait и поднимает WorkerNotStopped при failed cancellation, запрещая reconnect этого процесса. Добавлен mocked close/no-reconnect regression test, NOT RUN. Real process/signal/native acceptance открыта.


## Продолжение 2026-10-12: V29 snapshot receipt ownership и routed inventory

MultiPreparation проверяет/копирует snapshot receipt по digest/count исходных frames до awaited persistence. Source inspection подтверждает collection multi route preparation/review/apply inbox/recovery; registry note уточнён без semantic completion credit. NOT RUN; native qualification/полный lifecycle открыты.


## Продолжение 2026-10-12: V29 cancellation after snapshot

После snapshot persistence MultiPreparation явно проверяет cancellation до intent construction/admission. Late acknowledged snapshot остаётся durable для retry с исходным intent key; remote rollback не заявляется. Добавлены late-response/cancel-during-write tests, NOT RUN. Routed/native acceptance открыта.


## Продолжение 2026-10-12: V29 acknowledged ticket ownership

MultiAttempt передаёт persistence detached ticket; accepted in-memory replay identity не меняется adapter mutation. Unit test проверяет persistence/caller mutations, original ticket и ровно одну отправку. NOT RUN; cross-process/real API acceptance открыта.


## Продолжение 2026-10-12: V29 Apply journal/reconciliation ownership

Create/persist передают journal отдельную копию; adapter mutation не меняет internal selected/applied/warnings. Initial collection reconciliation принимает только applied/unchanged/conflict; неизвестный ответ не становится selected. Добавлены mutation isolation/invalid-state matrix tests, NOT RUN. CAS/concurrency/native lifecycle открыты.


## Продолжение 2026-10-12: V29 CAS selection ownership

Каждый reconcile/replace/reconcile-after-failure получает отдельную copy scalar selection; adapter mutation не меняет original scope следующего CAS/recovery. Добавлен lost-reply mutation regression test, NOT RUN. Real API/cross-tab concurrency открыты.


## Продолжение 2026-10-12: V29 lines post-execution runtime gate

После producer execution line installation повторно проверяет cancellation/runtime bytes до возврата результата. Drift/cancel отклоняет результат; staging artifacts могут существовать и остаются штатной orphan cleanup authority. Добавлены post-execution drift/cancel tests, NOT RUN. Native quality qualification открыта.


## Продолжение 2026-10-12: V29 correction/multi post-execution gate

Perspective/panorama/focus-stack installations повторно проверяют cancellation/runtime lock после adapter execution внутри TaskArtifacts scope. Added three-operation normal/drift matrix проверяет отказ и scope close. NOT RUN; algorithms unchanged, native qualification открыта; staging cleanup остаётся штатным.


## Продолжение 2026-10-12: V29 negative post-execution gate

Negative adapter повторно проверяет cancellation/runtime после SQL-backed execution до возврата результата внутри TaskArtifacts scope. Added normal/drift mock matrix со scope close, NOT RUN. Negative algorithm unchanged; runtime/photographic qualification открыта.


## Продолжение 2026-10-12: shared shutdown cleanup regression matrix

Добавлена regression matrix abort_active failure + optional reporter join failure: asserts join order и restoration SIGINT/SIGTERM. Все thread/signal/IO boundaries mocked. NOT RUN; real process lifecycle не подтверждён.


## Продолжение 2026-10-12: V29 profile type admission

Perspective/multi/negative wrappers отклоняют non-dict execution_profile как CONFIG_INVALID до runtime/artifact IO. Expanded mocked matrices, NOT RUN. SQL heartbeat operation superset не расширяет registration authority; inspected behavior сохранён.


## Продолжение 2026-10-12: V30 delivery cache/cancellation

Items/inspection HTTP responses получают private,no-store и late request cancellation fence. Extended items matrix + inspection cache/cancel tests, NOT RUN. SQL package project ACL inspected and retained; tenant/publication acceptance открыта.


## Продолжение 2026-10-12: V30 inspection response boundary

HTTP проверяет exact package/revision, valid manifest ref и bounded/count-consistent inspection. Canonical decoder bounded1MiB/revision и post-query cancellation. Added corruption/oversize unit matrix, NOT RUN. Public shape checks не заменяют SQL ACL/canonical manifest physical proof.


## Продолжение 2026-10-12: V30 items public allowlist

Reader/HTTP проверяют bounded envelope package/revision, manifest ref, page limit, exact item/cursor metadata field allowlists; post-query cancellation добавлена. Updated fixtures/private-response refusal tests, NOT RUN. Item field semantics/order/last-row cursor equality требуют продолжения; SQL ACL сохранён.


## Продолжение 2026-10-12: V30 items order/state/cursor

Добавлены typed/non-null ordinal/required, UUID/SHA, unique IDs, SQL-compatible state/reason/export checks, strict ordinal/preset order after request cursor и exact last-row next cursor. Added valid/invalid field+cursor unit matrix, NOT RUN. Full pagination/tenant/publication acceptance открыта.


## Продолжение 2026-10-12: V30 multi-preset pagination matrix

Added pending/ready/failed/omitted page matrix: same-frame UUID ordering, reverse refusal, same-ordinal continuation, duplicate cursor refusal. NOT RUN; compatibility coverage authored, runtime pagination acceptance не подтверждена.


## Продолжение 2026-10-12: V30 client reason/cursor consistency

Client требует meaningful failed/omitted reason, считает length code points как SQL и передаёт load отдельную cursor copy. Added reason/null/Unicode boundary/cursor mutation tests, NOT RUN. Full client/API pagination acceptance открыта.


### V30: omitted slot public boundary

V30 delivery items Go and client boundaries now reject omitted rows with required=true or a nonnull export, matching15-execution-contracts BuildAssemblyInput. Authored all four required/export combinations in Go and client tests, including valid optional omission.

Tests NOT RUN. No image processing changes. Whole-product implementation percentage remains unknown.


### V30: assembly identity and preset pins

V30 assembly worker now owns assembly ArtifactRef before callback execution, rejects nil assembly identity and requires each frozen preset config SHA to equal preset_sha, matching Go assembly admission. Updated valid fixture pins; authored required/optional config substitution, reader mutation isolation and nil-root pre-IO tests.

Tests NOT RUN; no processing algorithms changed. Whole-product completion remains unproven.


### V30: preparation ownership and profile rejection

V30 prepare_delivery captures an owned immutable input reference before cancellation/read/preset-validation callbacks, preventing late input identity substitution in preparation result. Delivery adapter rejects non-dict execution profiles with CONFIG_INVALID before authority/artifact IO. Added three callback mutation cases and malformed profile matrix for both operations.

Tests NOT RUN; image algorithms unchanged. Full-system completion unproven.


### V30: SQL admission params consistency

V30 SQLDeliveryAdmission now owns params before lease verification, passes an independent copy to source authority, and refuses parameter mutation after lease/source/final active checks. Direct admission rejects non-dict profiles before SQL. Authored malformed profile matrix and four authority-boundary mutation regressions.

Tests NOT RUN; no migration or processing algorithm changes. Whole-product completion remains unproven.


### V30: leased adapter parameter ownership

V30 leased adapter now snapshots Params on receipt, isolates root ArtifactRef passed to storage, passes independent Params to both admissions and refuses admission-side mutation before staging. Authored prepare/assemble x first/second admission mutation matrix and storage-reader mutation regression retaining original preparation identity.

Tests NOT RUN; image algorithms unchanged. Whole-product completion unproven.


### V30: shared physical adoption regression matrix

V30 authored shared prepare/assemble adoption physical-evidence matrix: params/result exact ref and SHA, integer byte bounds, MIME/source, nonbytes storage payload, and cancellation immediately after result physical read. Fixed two fixture WireRef decoders to use JSON strict UUID parsing matching production path.

Tests NOT RUN; no completion percentage credit.


### V30: publication acknowledgement ownership

V30 client publication journal now snapshots acknowledgement before asynchronous queue/lock/storage IO and validates complete receipt through readPublishedDelivery before clearing exact saved key/revision. Added malformed timestamp/revision/identity/name/hash metadata refusals and both valid-to-invalid/invalid-to-valid caller mutation regressions.

Tests NOT RUN; no migration or image algorithm changes.


### V30: Unicode package name parity

V30 package metadata and publication receipt name length now counts Unicode code points instead of UTF-16 units, matching existing creation validation and Go200-rune contract. Authored200/201 supplementary/BMP name boundary tests for GET/publication and durable acknowledgement cleanup with200 emoji.

Tests NOT RUN; no image processing changes.


### V32: interval coherence coverage and remaining SQL contract

V32 interval test corrected to reach coherence validator under renew1..10 bounds (lease=renew, rather than out-of-range renew60). Authored heartbeat/offline and renew/lease equality/inversion matrices plus valid custom timing retention and exact policy SHA binding tests. Source review confirms configurable offline/lease are still not fully propagated to SQL; no closure claimed.

Tests NOT RUN. Dynamic offline/lease must be operator-authorized and consistently used in selection, reservation, claim and renewal; arbitrary worker report cannot establish those intervals.


### V32: operator policy timing registry foundation

V32 forward-only01625 authored: operator-only global timing registry binds exact full policy UTF-8 SHA to offline/lease values, interval relations and immutable rows; no service-role grants. Added transactional Python operator installer with full policy revalidation, canonical bytes, idempotent exact-row comparison and refusal tests. Registry is groundwork and is not yet consumed by dispatch/claim/renew.

NOT RUN/NOT APPLIED. Operator installation requires an explicit transaction on an operator connection; worker/API/scheduler cannot self-install timing. Next: policy freshness and task lease consumers, existing pin rollout, SQL tests. This does not yet close dynamic offline/lease.


### V32: SQL timing consumers

V32 forward-only01626 authored: private operator-registry-backed freshness replaces fixed30s in central authority, dispatch selection/reservation and style runtime selection. Actual claim_task_reserved/heartbeat_task lease writers use policy lease seconds capped by task timeout/deadline, retaining wrappers/OIDs/ACLs via exact-definition drift checks. Missing timing refuses pinned workers; legacy remains30s/60s. Existing integration fixtures install true canonical policy SHA; added custom120s/300s freshness/lease cap/missing policy/future timestamp/ACL coverage.

NOT RUN/NOT APPLIED. Install every pinned full policy through the operator installer before enabling01626; missing policy deliberately blocks admission. Timings are global configuration, not tenant metadata. Actual claim/renew duration, concurrent rotation and worker startup proof remain follow-up work.


### V32: timing startup and heartbeat authority

V32 forward-only01627 authored: worker-only authenticated read-only timing readiness probe matches full SHA/offline/lease and all active principal registrations. Shared built-worker lifecycle requires exact True after ping and before operation registration/reporting/queue consumption. Policy heartbeat now locks and requires operator timing row before node/worker locks, so registry removal stops reports too. Added DB adapter input/truthiness, lifecycle order and SQL scope/interval startup matrices.

NOT RUN/NOT APPLIED. Deployment order: install all full policy bytes using operator transaction, pin registrations, then start workers. Policy registry is global operator configuration and must be included in disaster-recovery configuration restoration; worker cannot recreate it. No runtime or concurrency acceptance claimed.


### V32: actual claim/renew duration regression

V32 persisted dispatch integration now uses nondefault20-second operator lease and checks actual claim/renew timestamps against DB-clock brackets, identical task/slot/reservation expiry and fencing tokens. Added exact five-second task deadline clipping on real heartbeat without sleeping. Existing stale policy no-mutation and rollback-isolated legacy branches retained.

Tests NOT RUN; migrations NOT APPLIED. No full-module or overall percentage credit.


### V32: operator CAS rotation and lease serialization

V32 forward-only01628 authored: operator-only CAS rotation binds worker/principal/node/expected SHA, requires installed policy, locks timing->node->registrations, clears observed report on actual rotation and preserves report on same-pin no-op. Actual claim/renew lock their registration before authority validation; exclusive lock avoids concurrent SHARE-to-UPDATE last_seen upgrade deadlocks. Added transaction-owned Python rotation adapter and scope/result tests.

NOT RUN/NOT APPLIED. Rotation adapter uses explicit operator transaction and must roll back on any conflict; it does not commit. Old worker reports fail SHA matching, new startup/claim/renew requires fresh matching report. Claim/renew order retains slot->usage->run->task then registration; rotation takes no task/slot locks. Policy registry must be restored before pins/workers. Concurrency acceptance is not proven.


### V32: SQL policy rotation regression lifecycle

V32 archive SQL integration now exercises actual operator CAS rotation and policy heartbeat: observed receipt clearing, old SHA refusal, new SHA acceptance, same-pin no-op preserving exact report timestamp, stale expected pin/foreign worker/principal/node/uninstalled policy refusals with savepoint rollback, and no API/worker/scheduler EXECUTE grant.

NOT RUN/NOT APPLIED. This authors single-transaction runtime-function coverage, not proof of concurrent scheduling or operator rollout. No completion percentage credit.


### V32: restored capacity authority fencing

V32 restored-resource recovery now atomically expires all restored worker liveness and clears observed policy reports plus node available RAM/VRAM/scratch and report timestamp, retaining operator pins/budgets/qualification/enabled flags. Final read-only restore fence rejects revived worker reports or node capacity. Added malformed update receipt tests and both revived-grant families; existing evidence schema unchanged.

NOT RUN. Recovery remains isolated and paused; fresh worker reports are permitted only after the final fence and separately authorized controlled resume. Startup requires restored operator policy registry; old liveness/availability never counts as a fresh report. Runtime restore acceptance not claimed.


### V32: policy-bound physical node capacity freshness

V32 forward-only01629 authored: physical node capacity reports carry authenticated worker/policy SHA, private node freshness uses installed offline_seconds and current matching worker report; legacy reports retain30s and clear old provenance. Actual GPU or CPU-only reporters tag node, mixed CPU workers do not overwrite GPU capacity identity. Dispatch selection/reservation and tenant capability functions use unified freshness. Restore clears/fences new node report pins. Added custom120s node45s success and expired/future refusal SQL cases.

NOT RUN/NOT APPLIED. This removes the second fixed30s node freshness bottleneck for pinned reports. A rotated worker pin invalidates old node report provenance until an authorized fresh report. Existing legacy node reports retain30s, with no future timestamps accepted. Full lifecycle/concurrency verification remains outstanding.


### V32: operator provisioning CLI

V32 provisioning connected to existing photo-worker CLI: capacity-policy-install and capacity-policy-rotate. Uses only explicit PHOTO_OPERATOR_DATABASE_URL, validates policy/scope before connection, installs and CAS-rotates inside one bounded transaction, acknowledges only after commit and suppresses raw DB exceptions/credentials in CLI output. Rotation requires explicit expected SHA or legacy choice. Added transactional success/write/commit failure and no worker credential fallback tests.

NOT RUN. See capacity-policy-operator.md for migration rollout/rotation/recovery instructions. No completion credit.


### V32: uncertain operator commit reconciliation

V32 capacity-policy-reconcile CLI now observes exact installed policy bytes and all enabled worker/principal registrations in a bounded repeatable-read read-only transaction. Reports matches_next/matches_expected/conflict/missing_registration plus separate installed-policy flag; never writes, retries CAS or resumes workers. Added old/new/mixed/foreign-node/missing registration x installed/missing policy matrix.

NOT RUN. No overall completion credit.


### V32: SQL policy memory caps

V32 forward-only01630 authored: installed policy GPU/photo/control budgets are checked positive safe integers; authenticated capacity heartbeat validates raw RAM/VRAM before clamping to policy budget, then existing node caps apply. CPU control RAM differs from media/archive/GPU photo RAM. SQL matrix now uses700 VRAM/500 photo/300 control caps across CPU/media/GPU CPU/MPS/CUDA/mixed nodes; obsolete uninstalled-SHA success expectation fixed.

NOT RUN/NOT APPLIED. Worker-local10GB Metal allocator cap and physical resource admission remain separate, both required. Scratch free floor still relies on node measurement and the policy-aware worker because total physical scratch size is not sent to SQL. No full module acceptance claimed.


### V32: physical node concurrency admission

V32 forward-only01631 authored: node-locked reservation admission and candidate selection enforce operator policy GPU concurrency and a shared CPU/media/archive concurrency budget across tenants using live reserved tasks. Legacy retains GPU1 and existing CPU behavior. Policy fields checked positive safe integers. Added persisted-reservation integration checks showing one media task consumes CPU/archive pool but not GPU pool; transport prefetch1 remains per process.

NOT RUN/NOT APPLIED. To use cpu_concurrency2 provision suitable slots and distinct worker processes/identities; each consumer stays prefetch1, and SQL remains shared authority. Memory/scratch/qualification gates still apply independently.


### V32: public scheduler concurrency refusal

V32 persisted scheduler integration now creates a second ready task with spare same-class slot and ample node bytes under cpu_concurrency1. Tests actual eligible_dispatch_nodes refusal and public reserve_dispatch waiting_resource/null command, with unchanged ready/attempt/dispatch target, no reservation/outbox, spare slot free and original reservation retained.

Tests NOT RUN; migrations NOT APPLIED. No completion percentage credit.


### V32: конкурентная CAS-ротация, 2026-10-12

Добавлен `tests/integration/test_v32_capacity_rotation_concurrency.py`: отдельная база, две реальные транзакции, наблюдение блокировки через `pg_blocking_pids`, commit первой ротации и отказ второй с SQLSTATE 40001. Проверяется сохранение первого pin и очистка старого отчёта. Таймауты ограничены; блокировки освобождаются при ошибке.

NOT RUN: тест не запускался, миграции не применялись. Конкурентные claim/renew/reserve и проверка Metal остаются открыты. Процент завершения продукта не повышается.


### V32: heartbeat против ротации, 2026-10-12

Конкурентный тест дополнен старым heartbeat под ролью photo_worker: реальная блокировка за ротацией, после commit отказ False без изменения байтов, timestamp и provenance узла. Узел остаётся недоступным до принятого heartbeat с новым pin; затем проверяются freshness и точные RAM/scratch. Оба сценария используют отдельные соединения и изолированную базу.

NOT RUN; миграции не применялись. Это написанное покрытие, не доказательство выполнения. Конкурентные claim/renew/reserve и Metal остаются открыты.


### V32: откат конкурентной ротации, 2026-10-12

Матрица интеграционного теста теперь включает commit/rollback первой транзакции × heartbeat/ротацию второй. После rollback старый heartbeat должен приниматься с точными RAM/scratch и старым provenance, а вторая CAS-ротация должна успешно установить свой pin. После commit сохраняется отказ старого writer. Каждый сценарий наблюдает реальную блокировку до разрешения транзакции и проверяет heartbeat с итоговым pin.

NOT RUN; миграции не применялись. Это написанные сценарии, не evidence выполнения. Конкурентные claim/renew/reserve остаются открыты.


### V32: точный резерв scratch, 2026-10-12

`CapacityPolicy.cap_measurement` резервирует ceil(total × decimal fraction) через точное отношение целых чисел. Убрано float-умножение, способное занизить резерв у границы 2^53−1. Добавлены тесты целой/дробной границы, минимального объёма, максимального допустимого total и независимости от Decimal precision. Формат policy/SHA и алгоритмы обработки изображения сохранены.

NOT RUN. Полная реализация остальных параметров policy и runtime acceptance остаются открыты.


### V32: scratch multiplier в резервировании, 2026-10-12

Написана forward-only 01632: `scratch_multiplier` установленной policy проверяется в диапазоне 1..4 и применяется однократно через точный numeric ceil к исходной оценке при удерживаемой блокировке узла. Увеличенные байты используются существующим admission и INSERT reservation; профиль задачи не переписывается. Legacy сохраняет прежнюю оценку. Отсутствующая policy или выход расширенной оценки за safe integer дают waiting_resource до изменений slot/usage/task. Добавлены SQL-тесты границ, переполнения, отсутствующей policy и legacy.

NOT RUN; 01632 NOT APPLIED. Нужны persisted reservation, upgrade и конкурентные проверки; завершение модуля не заявляется.


### V32: persisted scratch admission, 2026-10-12

Расширен фактический reserve_dispatch integration: policy pin узла связан до admission, raw1024 помещается, ceil(1024×1.2)=1229 не помещается — ожидается waiting_resource без изменений task/slot/reservation/outbox. После увеличения capacity ожидается queued с reservation1229, исходный task profile1024 сохранён. Добавлены ACL-отказы приватных scratch helpers для API/worker/scheduler. Operator rollout обновлён до01632.

NOT RUN;01632 NOT APPLIED. Полное acceptance ещё не доказано.


### V32: сохранение scratch при lease и release, 2026-10-12

Persisted lifecycle тест теперь требует reservation scratch1229 и исходную оценку1024 после claim, renew и deadline clipping. Добавлена проверка приватного canonical release: неверный fence сохраняет slot/reservation; верный fence при двух вызовах освобождает ресурсы идемпотентно, сохраняет записанные байты и возвращает media concurrency admission. Это проверка примитива release, не полного terminal workflow.

NOT RUN; миграции не применялись. Конкурентные claim/renew/reserve и полное runtime acceptance остаются открыты.


### V32: правила стоимости и возраста очереди, 2026-10-12

Текущий Dispatcher использует legacy pick_ready_task; waiting_resource передаётся в существующую отсрочку. Добавлены CapacityQueuePolicy.Validate, CapacityTaskCharge и CapacityEffectiveQueue: точный набор весов, unknown только при отсутствующей оценке, безопасный ceil миллисекунд, одно повышение от frozen original class после10min. Написаны unit-тесты границ.

Примитивы ещё не подключены к DRR leader-loop. Leader advisory lock, eligible snapshot, deficits/cursor и фактический dispatch остаются открыты. NOT RUN.


### V32: DRR selection core, 2026-10-12

Добавлен CapacityDRR с owned policy, quantum seconds/cap, FIFO head per tenant, UUID round-robin, heavy queued cap и pending/Settle. Отказ резервирования не списывает deficit; успешный commit списывает один раз. Написаны тесты отказа, повторного settlement, FIFO, unknown cost, cap и атомарного отказа invalid snapshot.

NOT RUN. Ядро ещё не подключено к PostgreSQL leader/eligible snapshot/dispatch. Эти требования остаются открыты.


### V32: выделенная leader session, 2026-10-12

CapacityLeader удерживает advisory(2026,32) на выделенной физической PostgreSQL session, проверяет lock перед транзакцией и commit. Потеря lock/соединения или неопределённый commit закрывает session; соединение не возвращается в pool с session lock. Автоматический повтор commit запрещён, caller должен сбросить deficits и сверить durable dispatch. Написан integration сценарий двух лидеров, rollback, потери lock, замены и повторного close.

NOT RUN. Компонент ещё не подключён к production DRR loop/eligible snapshot.


### V32: candidate SQL snapshot, 2026-10-12

01633 добавляет scheduler-only capacity_queue_candidates(cpu|gpu|signer): active tenant, ready attempt<6, run v2/sealed/active, cancellation/admission delay, явный frozen queue_class и eligible node. Go adapter ограничивает snapshot10000 строк и отказывает целиком при overflow. Измеренный timing пока NULL: применяется policy unknown cost, не неавторизованная оценка из task JSON. Написаны SQL boundary tests.

NOT RUN;01633 NOT APPLIED. Nonempty fixtures, pagination, rollout queue_class и подключение DRR dispatch остаются открыты.


### V32: DRR к admission/outbox, 2026-10-12

01634 добавляет leader-session-only selected picker с scheduler tenant lock, recheck snapshot и heavy cap. CapacityDispatcher связывает DB time/candidates, DRR, selected picker и прежний reserve/outbox body Dispatcher. Settlement только после commit; ошибка закрывает leader и сбрасывает selector. Legacy picker сохранён. Добавлены тесты отказа без leader и ACL.

NOT RUN;01634 NOT APPLIED. Production wiring, nonempty/commit uncertainty/legacy coexistence и pagination остаются открыты.


### V32: clock race и leader precommit, 2026-10-12

DB clock цикла теперь читается после candidate snapshot, чтобы concurrent insert не создавал ложный future timestamp. Добавлены сценарии потери lock внутри transaction callback перед commit, deterministic UUID tenant round-robin/wrap/FIFO и независимости policy от последующего изменения caller map.

NOT RUN. Production coexistence legacy/V32, полные nonempty dispatch fixtures и pagination остаются открыты.


### V32: installed scheduler policy, 2026-10-12

01635 проверяет queue fields в operator registry и добавляет scheduler-only чтение exact policy bytes по SHA без table write grants. Go ReadCapacityQueuePolicy проверяет полный SHA и валидирует scheduler fields. Написаны decoder и SQL read/ACL/no-install тесты.

NOT RUN;01635 NOT APPLIED. Production loop/startup/runtime revocation, coexistence и полный dispatch acceptance остаются открыты.


### V32: runtime трёх пулов и policy revoke, 2026-10-12

CapacityRuntime создаёт один leader и CPU/GPU/signer selectors из installed policy; закрывается после ошибки назначения. Policy reader удерживает FOR SHARE, runtime assignment повторно читает/блокирует policy до commit, сериализуя operator revoke. Написаны invalid startup/closed runtime тесты.

NOT RUN;01635 NOT APPLIED. Production service composition, legacy exclusion, реальные revoke-race/nonempty dispatch остаются открыты.


### V32: scheduler service composition, 2026-10-12

Service принимает PHOTO_V32_SCHEDULER_POLICY_SHA256, создаёт installed-policy runtime до background loops и вызывает его перед legacy dispatch. 01636 добавляет legacy-only picker; V2 полностью передаётся DRR при включении. Ошибка runtime не включает fallback. Добавлены malformed pin pre-IO и ACL тесты.

NOT RUN;01636 NOT APPLIED; deployment не включён. Требуются stop old binaries, queue_class во всех producers/rollout existing runs и nonempty coexistence acceptance.


### V32: frozen capture queue class, 2026-10-12

Capture BeginDraft задаёт queue_class=capture до canonical config hash. Поле omitempty сохраняет исторические configs без queue class; LoadDraft отказывает чужому непустому классу. 01637 проверяет присутствующий класс на SQL begin без изменения immutable snapshots. Добавлен hash/field/legacy omission тест.

NOT RUN;01637 NOT APPLIED. Остальные V2 producers и полный capture restart/SQL acceptance остаются открыты.


### V32: frozen lens batch queue, 2026-10-12

Lens compiler задаёт queue_class=batch до config hash. Go store отказывает чужому классу даже с пересчитанными bytes/SHA; 01638 проверяет present queue_class в SQL begin. Compile/store тест дополнен batch assertion и отказом rehashed interactive до SQL. Историческое отсутствующее поле остаётся omitted.

NOT RUN;01638 NOT APPLIED. Другие producers, historical rollout и полный scheduler acceptance остаются открыты.


### V32: frozen original composition queue, 2026-10-12

Original composition compiler задаёт queue_class=batch до config hash. Preflight отказывает rehashed foreign class до DB IO;01639 повторяет present-field проверку в SQL begin. Расширены deterministic plan/mutation tests. Алгоритмы, precision/profile/model сохранены, исторические configs без поля остаются omitted.

NOT RUN;01639 NOT APPLIED. Другие producers, runtime restart/admission и historical rollout остаются открыты.


### V32: render preview/batch queues, 2026-10-12

Bulk compiler фиксирует batch, preview coordinator interactive до hash. Store/recovery и01640 SQL begin проверяют класс. Full evidence fixture дополнен hash binding, неизменностью task ID/input digest и отказом rehashed training. Historical configs не переписываются.

NOT RUN;01640 NOT APPLIED. Остальные producers и полные render batch/restart/SQL сценарии открыты; алгоритмы/profile не менялись.


### V32: session analysis batch queue, 2026-10-12

Session analysis compiler фиксирует batch до config hash;01641 проверяет present foreign class в SQL begin. Historical omitted field/recovery не переписываются. Добавлен class/hash/legacy omission тест. В training найден recompilation recovery: до добавления класса нужен versioned recovery handling, training остаётся открытым.

NOT RUN;01641 NOT APPLIED. Полные admission/recovery и остальные producers открыты.


### V32: training queue и exact recovery, 2026-10-12

Training config/recovery фиксируют training до hash. Resume использует сохранённый класс; историческое отсутствие поля сохраняет прежние config/recovery bytes.01642 расширяет strict SQL shape, отказывает foreign class и связывает recovery class с frozen run config. Написаны new/historical exact replay/hash/foreign class тесты.

NOT RUN;01642 NOT APPLIED. Реальные frozen wrapper/pending replay и оставшиеся producers/rollout открыты.


### V32: semantic transfer queue и producer registry, 2026-10-12

Semantic transfer compiler фиксирует batch; Go shell и01643 SQL shell проверяют присутствующий класс без обхода qualification delegates. Добавлены compile/hash и rehashed shell class тесты. Реестр capacity-queue-producer-coverage.md явно перечисляет открытые preparation/V28–V31/rollout пути.

NOT RUN;01643 NOT APPLIED. Полное producer coverage/deployment acceptance ещё не достигнуто.


### V32: nonempty legacy/V2 partition fixture, 2026-10-12

Написан SQL fixture реального tenant/Apply/V2 batch/tasks/node/slot/worker: CPU snapshot только V2, GPU/signer пусты, legacy picker только Apply, selected picker отказывает legacy/foreign tenant. Delivery source audit выявил exact config replay: до добавления класса нужен pending-compatible rollout. Delivery class пока не менялся.

NOT RUN. Полный reserve/outbox/runtime/commit acceptance открыт.


### V30/V32: delivery pending queue replay, 2026-10-12

Новые delivery pending сохраняют queue_class=batch; BeginRun использует сохранённый класс до canonical config hash. Исторические pending без поля воспроизводят config без поля. Decode отказывает foreign/null/empty explicit class, SQL01644 связывает exact expected config с сохранённым pending без UPDATE snapshots. Написаны new-class, historical replay bytes и foreign-class pre-IO тесты.

NOT RUN;01644 NOT APPLIED. Delivery child exports/interchange, historical rollout и полное SQL/runtime acceptance остаются открыты. Общий процент завершённости не пересчитан: code presence не доказывает полноту требований.


### V30/V32: delivery child export queue, 2026-10-12

ChildConfig фиксирует batch до hash.01645 единообразно меняет SQL draft/activation/cursor commit/receipt: новый child получает batch, существующий immutable child без поля сохраняет прежний exact config, foreign class отказывает. Private helper недоступен runtime ролям напрямую. Дополнены Go assertions и написаны SQL fresh-config/ACL тесты.

NOT RUN;01645 NOT APPLIED. Historical SQL fixture, concurrent create/replay и полный delivery runtime acceptance остаются открыты; Go ChildConfig описывает новый child, не заменяет recovery receipt старого. Interchange остаётся открытым.


### V30/V32: interchange parse queue, 2026-10-12

ParsePlan/new pending фиксируют batch; DecodeParsePending восстанавливает сохранённый класс, включая историческое отсутствие. BeginParseRun включает его до config hash;01646 связывает exact SQL expected config с pending. Написаны new/historical repeated config bytes и foreign/null/empty pending tests. Профили/params/snapshot digest не изменены.

NOT RUN;01646 NOT APPLIED. Interchange export queue, SQL replay/admission acceptance и исторический rollout остаются открыты. Общий процент полноты требований не повышается по одному наличию кода.


### V30/V32: interchange export queue, 2026-10-12

Frozen export evidence фиксирует batch до config hash. Recovery recompilation использует сохранённый класс, включая историческое отсутствие; foreign/null/empty explicit class отказывает.01647 связывает SQL expected config с frozen queue. Написаны fresh-class и historical repeated config replay/foreign-class tests. Export plan, snapshot, params и resource profile не изменены.

NOT RUN;01647 NOT APPLIED. SQL pending/admission acceptance, full producer inventory и historical rollout остаются открыты. Наличие кода не повышает подтверждённый процент завершённости.


### V31/V32: signing frozen batch queue, 2026-10-12

Signing pending/config фиксируют batch. Restore переносит historical omission через private intent flag, subsequent preparation clone сохраняет его; full canonical pending replay остаётся exact.01648 проверяет optional batch в pin и связывает draft config с saved pending. В service fixture написаны fresh/historical restore/recompile и normalized foreign/null/empty отказ тесты. Execution pool signer и криптографические параметры не изменены.

NOT RUN;01648 NOT APPLIED. Deployed provenance.sign.execute/HSM/TSA/trust contour всё ещё не подтверждён и не считается закрытым этой queue-интеграцией. SQL/runtime acceptance и remaining producer paths открыты.


### V28/V32: relight initial preview queue, 2026-10-12

Initial relight draft получает interactive до config hash.01649 добавляет scheduler-only authorized selector с tenant lock: new interactive, existing exact saved omission/interactive, foreign отказ. SQL begin повторяет выбор и exact config compare. Go выбирает класс в той же транзакции. Написаны config repeated bytes/historical omission/foreign class unit tests.

NOT RUN;01649 NOT APPLIED. Nonempty SQL/ACL/concurrency fixtures и initial-to-intent runtime acceptance открыты. Relight analysis/local/replacement и остальные portrait producers не считаются покрытыми.


### V28/V32: initial queue nonempty SQL fixtures, 2026-10-12

Написан tests/integration/test_v32_relight_initial_queue.py: реальные tenant/project/session/snapshot/run; new interactive, historical omission, saved interactive, foreign batch/null/empty/actor refusal, repeated selection, wrong session/tenant refusal, exact saved config unchanged, scheduler-only ACL. Изменения immutable configs не выполняются.

NOT RUN. Это authored coverage, не evidence SQL PASS. Analysis recovery source audit показывает multiple structural recompilers и full snapshot comparisons; simple field injection небезопасен. Analysis/local/replacement остаются открыты до согласованного saved-policy propagation.


### V28/V32: relight analysis outer run queue, 2026-10-12

Source tracing выявил authoritative SQL outer config assembly: embedded plan/config/descriptor/receipt не требуется менять.01650 добавляет interactive только во внешний config до существующего config hash. Tenant-serialized existing run lookup сохраняет historical omission; foreign saved class отказывает. Private immutable helper, SQL tests new/historical/replay/embedded plan unchanged/foreign/ACL написаны.

NOT RUN;01650 NOT APPLIED. Полный qualified SQL draft/stage/receipt/runtime acceptance открыт. Analysis structural recompilers остаются неизменными; local/replacement и остальные portrait producers открыты.


### V28/V32: local portrait exact config recovery groundwork, 2026-10-12

Выделен localPortraitOwnerConfigForQueue с допустимыми historical omission/interactive. Loader проверяет полный SHA одного из двух exact compiler outputs, не удаляет поле и не нормализует config для обхода hash. Full manual raster fixture дополнен queued owner restore, unchanged request/task plan, queue hash binding и foreign class отказ.

NOT RUN. Новый producer пока НЕ переключён: SQL strict root, transactional queue choice и repeated Begin должны быть согласованы отдельно. Это частичная реализация local queue, не закрытый producer coverage; миграций в этом инкременте нет.


### V28/V32: local portrait transactional queue Begin, 2026-10-12

SQLLocalPortraitOwnerScope.Begin выбирает queue внутри той же транзакции и компилирует полный canonical config.01651 scheduler-only helper берёт tenant lock и проверяет полный existing owner request; new interactive/existing omission или interactive, foreign отказ. SQL begin root допускает queue_class, повторяет выбор и exact class до hash/quota/run insert. Recovery compiler поддерживает оба exact hashes с предыдущего инкремента. Написаны malformed authority/ACL SQL tests.

NOT RUN;01651 NOT APPLIED. Qualified nonempty owner fixture, concurrent Begin и full recovery/admission acceptance остаются открыты. Replacement/other portrait paths требуют отдельного инвентаря.


### V29/V32: perspective primary/lines/multi/detection queues, 2026-10-12

01652 добавляет private frozen preview config helper и патчит четыре authoritative SQL producers после tenant-serialized existing run lookup: perspective primary, lines, multi, line detection. New interactive/existing omission или interactive; foreign class отказ. Queue входит во внешний config до существующего hash. Embedded plan/recovery bytes и algorithms/profiles не менялись. Написаны parameterized SQL helper tests по четырём producer keys, replay/foreign/ACL.

NOT RUN;01652 NOT APPLIED. Qualified nonempty full producer/admission/recovery tests открыты. Negative, preview/quality/replacement и V27 preparation paths ещё не покрыты.


### V29/V32: negative primary queue и legacy child inventory, 2026-10-12

01653 добавляет interactive во внешний negative primary config через frozen helper после serialized old run lookup и до config hash. Embedded draft/recovery JCS остаются неизменными; historical omission сохраняется. SQL helper parameter tests расширены negative producer key.

Source inventory: save_v29_perspective_preview_intent, save_v29_perspective_quality_intent, save_v29_multi_preview_intent, save_v29_multi_quality_intent создают kind=preview, не V2. Legacy-only picker исключает только V2, поэтому эти paths остаются в legacy dispatch; отсутствие queue_class у них само по себе не V32 backlog gap. Их runtime/coexistence acceptance всё равно открыто.

NOT RUN;01653 NOT APPLIED. Negative replacement strict config readers/probes и full qualified producer/recovery acceptance остаются открыты.


### V29/V32: negative replacement producer/reader/probe queue, 2026-10-12

01654 согласованно добавляет interactive во внешний config save_v29_negative_replacement_draft, расширяет strict reader только optional exact interactive, обновляет publication probe через full expected config helper с сохранённым классом. Historical omission и embedded draft JCS не переписываются; полный config hash продолжает проверяться. Написан exact embedded JCS fresh/historical replay SQL helper test.

NOT RUN;01654 NOT APPLIED. Qualified full replacement producer/reader/probe acceptance открыт; manual/auto perspective и multi replacement ещё требуют аналогичного согласования всех strict checks.


### V29/V32: manual perspective replacement queue, 2026-10-12

01655 согласует save/read/probe manual perspective replacement: interactive outer config before hash, strict optional exact class, full publication expected config binding. Historical omission и embedded replacement draft JCS сохраняются; existing full hash checks остаются. Exact embedded replay helper test параметризован optical.negative/optical.perspective.

NOT RUN;01655 NOT APPLIED. Qualified full manual replacement acceptance открыт; auto perspective/detector/multi replacements ещё не покрыты полностью.


### V29/V32: auto perspective и replacement detector queues, 2026-10-12

01656 обновляет auto replacement producer/reader и detector producer/current draft recovery reader: interactive outer config before hash, saved omission/interactive replay, foreign refusal. Auto reader hashes полную expected config с сохранённым классом. Detector staged/sealed recovery byte checks сохранены. Segments и descriptor JCS не меняются; auxiliary evidence helper tests написаны.

NOT RUN;01656 NOT APPLIED. Full qualified auto/detector lifecycle и concurrency acceptance открыты; multi replacement и V27 preparation/assembly ещё требуют реализации.


### V29/V32: multi replacement queue, 2026-10-12

01657 согласует multi replacement save/read/probe: interactive outer config до hash, exact saved omission/class replay, full config/hash checks. Embedded replacement JCS не меняется. Helper replay tests расширены фактическими optical.panorama/optical.focus_stack.

NOT RUN;01657 NOT APPLIED. Full qualified producer/reader/probe/lifecycle acceptance остаётся открытым. V27 preparation/assembly/apply и remaining producer inventory требуют продолжения; общий completion не объявляется.


### V27/V32: detector preparation queue/recovery, 2026-10-12

New detector compiler фиксирует batch до hash/seal. Recompile и saved detector restore используют сохранённый queue; historical no-key и keyed configs без queue остаются exact. Go shell и01658 SQL public shell проверяют optional exact batch для detector marker. Full detector fixture расширен new batch и keyed/no-key historical replay.

Semantic apply source inventory: create_semantic_apply_draft_shell создаёт kind=apply, остаётся legacy scheduler; отсутствие V2 queue не является backlog gap. NOT RUN;01658 NOT APPLIED. Full detector SQL/native acceptance и остальные native preparation/assembly/target/replacement paths открыты.


### V27/V32: face preparation queue/recovery, 2026-10-12

Face preparation compiler фиксирует batch до config hash/seal. Saved restore принимает only present batch или omission и переносит historical omission в private preparation receipt, сохраняемый всеми recompile callers. Go shell +01659 SQL guard проверяют face marker queue. Full native fixture дополнен fresh batch/historical omission/exact repeated seal+config replay.

NOT RUN;01659 NOT APPLIED. Full native/SQL acceptance открыто. Skin/person/defect/targets/assembly/replacements требуют продолжения; обработка/модельные профили не изменены.


### V27/V32: skin preparation queue/recovery, 2026-10-12

Skin compiler фиксирует batch до config hash/seal. Saved stage restore переносит historical omission в private skin preparation receipt, сохраняемый всеми recompile callers. Go shell/01660 SQL guard проверяют optional exact batch. Full native fixture дополнен fresh batch/historical omission/exact repeated config+seal replay. Model/params/profile unchanged.

NOT RUN;01660 NOT APPLIED. Full SQL/native acceptance и remaining person/defect/target/assembly producers открыты. Общий процент полноты требований не объявляется по authored code.


### V27/V32: person preparation queue/recovery, 2026-10-12

Person compiler фиксирует batch до config hash/seal. Saved restore переносит historical omission через private source preparation receipt, сохраняя continuation/ordinary policy. Go shell/01661 SQL guard проверяют person queue. Full person fixture дополнен fresh batch/historical omission/exact repeated seal+config replay. Params/profile/model unchanged.

NOT RUN;01661 NOT APPLIED. Full saved restore/SQL/native acceptance и remaining defect/target/assembly paths открыты.


### Native defect queue —01662

V27/V32 native defect preparation freezes batch before config hash/seal; saved restore and task recompiler retain historical omission. Shared manual assembly explicitly retains its historical format pending separate integration. Go shell and SQL01662 reject foreign present classes; full native fixture extended with fresh batch and historical repeated config/seal replay.

NOT RUN;01662 NOT APPLIED. Full qualified SQL/native acceptance and target/manual/assembly producer coverage remain open; no whole-product completion credit.


### Manual defect queue continuation

V27/V32 manual defect initial compiler now freezes batch via shared native defect shell; private task receipt retains historical omission across recompilers. Both manual restore compilers read saved queue, retain full saved config/hash matching, reject foreign classes. Shared saved queue decoder and malformed/null/empty/foreign/historical unit matrix authored. SQL01662 already covers shared marker; no additional migration required.

NOT RUN;01662 NOT APPLIED. Full nonempty manual lifecycle/recovery fixtures, target/assembly inventory and scheduler acceptance remain open. No processing algorithm/profile/model changes or whole-product completion credit.


### Target person queue —01663

V27/V32 target person compiler freezes batch before config hash/seal. Saved restore carries historical omission through existing source preparation receipt; full saved config hash/profile matching remains required. Go shell and SQL01663 validate optional target person batch class. Target compiler fixture extended with fresh batch and historical exact config/seal replay.

NOT RUN;01663 NOT APPLIED. Target detector/face/skin/defect and assembly producers, full qualified restore/SQL/native acceptance remain open. No processing model/profile changes or overall completion claim.


### Target detector queue —01664

V27/V32 target detector compiler freezes batch before canonical config hash/seal. Private target-only saved omission flag retained in source preparation and restored from authoritative config; existing reference detector queue replay unchanged. Go shell and SQL01664 reject foreign present target detector queue. Target fixture extended with fresh batch and exact repeated historical config/seal replay.

NOT RUN;01664 NOT APPLIED. Full target recovery/SQL/native acceptance and remaining face/skin/defect/assembly producers open. No imaging/model/profile changes; no whole-module completion claim.


### Target face queue —01665

V27/V32 target face landmarks/parse compiler freezes batch before hash/seal. Restore carries saved historical omission via existing native face receipt, retaining complete config/profile checks. Go shell and SQL01665 guard present target face class. Physical target face fixture extended for both operations with batch and exact historical repeated config/seal replay.

NOT RUN;01665 NOT APPLIED. Full target face saved lifecycle/SQL/native acceptance and skin/defect/assembly producer coverage remain open. Imaging parameters/models/profiles unchanged; no overall completion credit.


### Target skin queue —01666

V27/V32 target skin compiler freezes batch before canonical config hash/seal. Private preparation receipt preserves historical omission across task rebuilds; restore reads saved choice and retains full config/profile matching. Go shell and SQL01666 guard optional batch. Physical target skin fixture extended with fresh batch/historical exact repeated config/seal replay.

NOT RUN;01666 NOT APPLIED. Full target skin saved lifecycle/SQL/native acceptance, target defect/assembly producers and scheduler acceptance remain open. Processing algorithms/parameters unchanged; no overall completion credit.


### Target defect queue —01667

V27/V32 target defect compiler freezes batch before canonical config hash/seal. Private receipt propagates historical omission across task rebuild; restore reads saved class with full config/profile matching retained. Go shell and SQL01667 guard present target defect class. Physical plan fixture extended with fresh batch and repeated historical config/seal replay.

NOT RUN;01667 NOT APPLIED. Full target defect saved lifecycle/SQL/native acceptance, assembly producer inventory and remaining scheduler requirements open. Processing algorithms/parameters unchanged; no overall completion credit.


### Assembly queue inventory and all-target boundary

V27/V32 assembly source inventory confirms stage SQL00506/00530 inserts task into existing run; assembly inherits semantic run batch queue, no separate run config. Native all-target config embeds SemanticDraftConfig and retains base batch. Strengthened locator validation to reject foreign nonempty class; extended physical all-target fixture with batch assertion and rehashed interactive/training/empty/null refusal (empty/null fail canonical exact shape).

NOT RUN. Historical all-target recompilation/rollout remains open; inherited queue classification is source evidence only, not runtime acceptance. V28 replacement producer coverage and scheduler remaining requirements remain open.


### Historical all-target transfer replay

V27/V32 historical all-target transfer replay now retains saved queue omission in private TransferPreparation receipt. Capacity restore derives it from exact decoded saved config; config compiler verifies complete base before selecting historical serialization. Ownership copies retain flag and scope recompiler uses same receipt. Physical all-target fixture extended through historical compile, saved capacity restore, exact full config/hash/task replay and scope recheck.

NOT RUN. No migration needed; immutable historical snapshots are not rewritten. Full nonempty SQL/native recovery and historical scheduler rollout remain open; replacement inventory and remaining V32 requirements continue. No overall completion credit.


### Native transfer saved queue boundary

V27/V32 native transfer saved decoder now validates batch or historical absent queue through strict shared decoder before exposing locators. Protection restoration delegates to this full saved validator. Added batch acceptance and rehashed foreign queue rejection through both saved/protection APIs. Source inventory identifies exposure preview00615 as legacy kind=preview; only explicit V28 run insertions located are local owner00833, initial relight01012 and analysis01015 already covered; exhaustive indirect producer/replacement audit still open.

NOT RUN. No migration added. Historical native reference full recompilation remains open, as do indirect replacement inventory, SQL/native acceptance and remaining V32 scheduler requirements. No whole-product completion claim.


### Native reference historical queue replay

V27/V32 native reference and manual-reference recovery now retain saved queue omission in metadata/private TransferPreparation through saved loaders and resume paths. Their trusted workflow compilers reconstruct historical canonical config/hash after full batch compilation, preserving all other config fields. Added exact full-config/hash/source-ownership and invalid compiler/hash refusal unit scenarios. This is authored replay integration, not qualified full runtime proof.

NOT RUN. Full native/manual reference recompilation scope and SQL/native lifecycle acceptance remain open; no migration or immutable snapshot rewrite. Remaining replacement inventory and V32 requirements continue; no overall completion claim.


### Historical reference scope recompilation

V27/V32 native reference/manual-reference scope validators now apply the saved historical queue choice to independently recompiled expected core before exact full config/hash comparison. Fixes admission mismatch introduced by queue-aware historical plan compilers without bypassing authority comparisons. Existing full workflow task fixture expanded to independent historical graph recompile, exact config/hash/task pins and fresh batch isolation.

NOT RUN. Full scoped physical reference/manual fixture and SQL/native lifecycle acceptance remain open; no migration or immutable snapshot rewrite. Remaining V32 requirements and whole-product audit still open.


### Complete candidate snapshot —01668

V32 complete candidate snapshot:01668 removes SQL LIMIT10001 preserving all eligibility/ACL; Go streaming reader no longer refuses >10000 and checks cancellation while consuming. DRR receives every tenant in the statement snapshot. Added 10002-task two-tenant fairness regression. Full snapshot still retained in memory; bounded-memory pagination remains unresolved and is not claimed implemented.

NOT RUN;01668 NOT APPLIED. Real SQL >10000 fixture, full dispatch acceptance, bounded-memory design and remaining V32 requirements open; no overall completion credit.


### Targeted candidate admission —01669

V32 targeted selected-task eligibility:01669 introduces scheduler-only capacity_queue_candidate(pool,task) with same ready/run/tenant/pool/node/cancellation/heavy predicates restricted by task ID; selected picker delegates to it while retaining leader and tenant locks. Avoids full backlog function scan on every reservation. Nonempty candidate equivalence/legacy exclusion/wrong pool, ACL and malformed authority fixtures authored.

NOT RUN;01669 NOT APPLIED. Query-plan performance, full concurrency/native dispatch acceptance and bounded-memory full snapshot remain open. No full-module completion claim.


### Heavy inflight admission —01670

V32 GPU dispatch now checks installed policy heavy_admission_inflight inside leader reservation transaction before selected picker.01670 scheduler-only helper locks installed policy FOR SHARE and counts all GPU queued/running tasks including legacy; refusal does not charge DRR. Admission here means outstanding GPU dispatch count, distinct from running concurrency and tenant queued cap. Authored ACL/no leader/missing policy SQL fixtures.

NOT RUN;01670 NOT APPLIED. Nonempty limit-boundary/concurrency fixtures and legacy producer coexistence require implementation/acceptance; single V32 leader serializes its own admissions but external legacy dispatchers can race, so rollout requires stopping old binaries. Full V32 not complete.


### Heavy inflight real-row coverage

V32 heavy admission authored real-row SQL fixtures: exact 7/8 boundary, running inclusion, cancel_requested retains charge until terminal, cancellation/failure release, CPU and ready/blocked/retry/terminal exclusion, cross-tenant and legacy GPU inclusion. Fixtures exercise installed Metal policy and current helper under leader lock; no runtime evidence claimed.

NOT RUN. Nonempty fixtures may require repair in testing phase; concurrent reservation/legacy coexistence and full dispatch remain open. No production execution or overall completion credit.


### Heavy admission across execution pools

V32 heavy admission moved after selected-task SQL read and now branches on authoritative resource_class rather than pool label. GPU signer tasks cannot bypass global cap; GPU dispatch without installed policy refuses. CPU/media/archive/collection/mail retain no GPU budget query. Authored allowed/denied/SQL failure/cancellation/missing policy/unknown class unit matrix; original same-transaction reservation boundary retained.

NOT RUN;01670 NOT APPLIED. Actual signer/GPU SQL dispatch and concurrency acceptance remain open; full snapshot memory and remaining V32 requirements unresolved. No overall completion credit.


### DRR FIFO head selection

V32 DRR Next now scans/validates the full snapshot but materializes and sorts only one FIFO head per tenant/effective class. Removes repeated full-backlog item copy/sort while preserving duplicate refusal, aging class separation, heavy cap and expensive-head FIFO. Added unsorted cheap-follower refusal/promoted-head/hidden-duplicate regression. Complete snapshot and duplicate set still require O(backlog) memory; bounded-memory pagination not claimed.

NOT RUN. Full SQL/native dispatch and performance measurement remain open; no processing changes or whole-product completion credit.


### Large backlog SQL fixture

V32 nonempty SQL candidate/selected-picker fixture parameterized for backlog1 and10002. Real task insert set exercises complete candidate count/uniqueness, per-row tenant/queue/cost, direct candidate equivalence and selected admission of last row beyond former prefix. Legacy exclusion, wrong tenant and pool assertions retained. This covers authored SQL acceptance scope missing from pure DRR large-backlog unit.

NOT RUN. SQL fixture performance/authority correctness and full dispatch remain unverified; no migration added. Full backlog bounded memory, model affinity, qualified timing and other V32 requirements remain open.


### Model affinity selection groundwork

V32 model affinity pure selector authored with explicit model-lock evidence, same tenant/effective class, resident-only oldest preference and inclusive30s FIFO bypass bound. Rejects foreign/duplicate evidence, duplicate candidates, invalid age and non-FIFO head. Boundary/scope/refusal tests authored. NOT wired: current heartbeat has no trusted residency list; no fabricated cache authority or production affinity claim.

NOT RUN. Trusted residency publication/qualification/expiry and DRR cost/head integration remain required before production use. Model affinity remains partial, not completed; remaining V32 and full-product requirements open.


### Actual model pool residency source

V32 model residency local source added to actual ModelPool: immutable snapshot with model lock/id/backend/qualification report/generation only after successful qualified factory load. Separate short state mutex avoids inference-lock blocking heartbeat. Eviction/error/close withdraw snapshot before freeing model; doctor unqualified loads not advertised. Authored loaded/reuse/ownership/eviction/reload/doctor tests without real accelerator.

NOT RUN. Worker heartbeat publication, SQL scoped freshness/qualification and DRR affinity integration remain open; local observation alone grants no scheduler authority. Model affinity still partial; whole-product incomplete.


### Worker residency publication —01671

V32 residency persistence01671: worker-only report function stores bounded exact-shape model cache hints and DB report time on authenticated enabled registrations after locking/rechecking installed fresh capacity policy. Model lock/report SHA, backend, integer generation and duplicate identity validated. TaskDatabase publisher accepts immutable ResidentModel tuple. Hints do not confer model qualification; no scheduler reader or heartbeat wiring yet.

NOT RUN;01671 NOT APPLIED. ACL/malformed/missing-registration fixtures authored; positive freshness/principal/concurrency fixtures, actual heartbeat, expiry/qualification reader and DRR integration remain open.


### Worker heartbeat residency wiring

V32 production worker factory retains actual GPU/CPU ModelPool sources on executor as immutable snapshot provider. Capacity heartbeat publishes current residency only after exact successful capacity report; empty snapshot withdraws cached model hints, refusal propagates to existing CapacityReporter stop/abort behavior. Legacy/no-provider reporting remains unchanged. Authored ordering/refusal/empty withdrawal and detached DB wire tests.

NOT RUN;01671 NOT APPLIED. Actual native heartbeat/SQL acceptance, residency expiry/qualification scheduler reader and DRR affinity integration still open. External renderer pools not yet advertised; no overall completion credit.


### Eligible node model affinity —01672

V32 node affinity01672 wired only for V32 Dispatcher: reorder existing eligible nodes for exact DRR-selected task using fresh policy-bound same-tenant worker residency with exact model lock/backend and qualified operation. Explicit operation→model mappings cover face detect/landmarks/parse/person; unknown operations preserve order. Reserve remains authoritative and legacy dispatcher unchanged. Authored scheduler ACL/missing task/nonempty no-hint baseline-order SQL fixtures.

NOT RUN;01672 NOT APPLIED. Positive multi-node residency/stale/foreign-principal/qualification fixtures required; task-level30s affinity helper still unwired. Node hints do not confer qualification or change FIFO. Full V32 and product remain incomplete.


### Residency authority invalidation —01673

V32 residency authority lifecycle01673 adds private BEFORE UPDATE trigger clearing cache hints/timestamp on registration identity, tenant/node/principal, enabled, capacity policy, lock, backend/resource class or qualification changes. Unchanged heartbeat fields retain hints. Authored operator-seeded trigger-only matrix for policy removal/disable/lock/backend/qualification changes and unchanged heartbeat retention; not a publication qualification proof.

NOT RUN;01673 NOT APPLIED. Positive report publication, multi-node affinity freshness/qualification and concurrent rotation/report acceptance remain open; task-level affinity still partial and overall product incomplete.


### Residency publication lifecycle fixtures

V32 worker residency publication real registration lifecycle fixtures authored: exact worker-role successful report and empty withdrawal, stale heartbeat/foreign policy/disabled/foreign principal refusal with unchanged stored state. Added generation negative/fraction/unsafe/null/bool and duplicate model identity rejection fixtures. Operator seeds registration/capacity freshness only; publication runs through actual worker SQL function.

NOT RUN. Concurrent report/rotation, actual heartbeat process and positive multi-node affinity still unverified; no module or whole-product completion credit.


### V32 DRR affinity receipt integration — 2026-10-12

V32 DRR ApplyAffinity replaces only the current pending receipt using validated eligible candidates and scoped resident evidence. Preserves same tenant/effective class/30s bound, excludes heavy-at-cap tasks, refuses changed head identity/cost, falls back if replacement exceeds deficit. Replacement invalidates old receipt; Settle charges actual selected cost only on commit. Authored committed/refused/expensive/heavy/foreign/changed snapshot tests.

NOT RUN. Trusted task-level SQL evidence reader and production dispatch wiring still required. No migration or overall completion credit; full V32 and product remain incomplete.


### V32 task affinity transaction wiring — 2026-10-12

V32 migration01674 exposes scheduler-only task residency hints over exact eligible_dispatch_nodes and existing fresh qualified worker residency gates. Go reader narrows query to selected tenant/effective class/30s window. CapacityDispatcher reads hints and applies DRR receipt replacement inside reservation transaction after installed-policy check; canonical task picker and reservation remain authoritative. SQL ACL/empty/missing-scope tests authored.

NOT RUN;01674 NOT APPLIED. Positive multi-node qualification/expiry, native dispatch and commit-uncertainty acceptance remain open. Full candidate memory and qualified cost binding unresolved; whole product incomplete.


### V32 task affinity reader boundary — 2026-10-12

V32 task affinity SQL reader now rejects changed receipt scope, duplicate/nil candidate identity, foreign or duplicate returned rows, malformed lock digests and false resident hints; cancelled context refuses before query. Added fake pgx rows/transaction tests for exact tenant/class/inclusive30s SQL scope, owned results, cursor closure, SQL/scan/stream errors and cancellation.

NOT RUN. Positive real SQL multi-node residency and production dispatch acceptance still required. No migration added; full-product implementation percentage remains unproven.


### V32 two-node residency acceptance authored — 2026-10-12

V32 authored real two-node SQL fixture with same-tenant CPU face.detect task and worker-role residency publication. Fresh exact sfd/model-lock residency must reorder eligible nodes and emit task affinity hint; stale/future timestamps, wrong lock/model, empty withdrawal and disabled registration must remove influence. Asserts affinity never expands eligible node set; scheduler role performs both readers.

NOT RUN;01671–01674 NOT APPLIED. Fixture qualification/constraints and actual reservation/native dispatcher acceptance remain unverified. Existing photo processing code unchanged; whole-product completion unproven.


### V32 verified report timing — 2026-10-12

V32 qualification timing foundation uses existing fully verified report p95_ms via VerifiedTimingMilliseconds; ceil to safe integer milliseconds, zero measurement returns unknown, no estimate from unverified task JSON. Added fractional/exact/safe-integer/zero and wrong runtime lock tests alongside existing qualification verification fixture.

NOT RUN. This helper does not establish task dimension/device binding and is not wired to SQL candidates. Persisted qualified timing selection remains open; whole-product implementation unproven.


### V32 qualification timing persistence — 2026-10-12

V32 migration01675 persists nullable measured_p95_ms on newly imported immutable model qualifications. Exact-definition guarded patch preserves importer signature/ACL/hash/policy/replay path and derives ceil p95 only after existing trusted import checks. Private numeric helper rejects missing/non-number/negative/inverted/unsafe measurements; zero remains NULL. Historical qualifications unchanged. Authored SQL rounding/refusal/private ACL tests.

NOT RUN;01675 NOT APPLIED. Positive complete import persistence/replay and task dimension/device-qualified SQL binding still open. Scheduler continues unknown timing until authoritative task binding exists; whole product incomplete.


### V32 full qualification timing import fixture — 2026-10-12

V32 full maintenance-only qualification import fixture now parameterized for exact/fractional/zero p95. Executes actual existing operator/policy/hash/artifact-bound import twice, asserts stored rounded/NULL timing, one immutable qualification and one import audit event after replay, and refuses timing UPDATE through existing immutable trigger. Retains API ACL, runtime policy rotation, forged report/backend and disabled operator cases.

NOT RUN;01675 NOT APPLIED. Full fixture and trigger SQLSTATE unverified until testing phase. Persisted task dimension/device timing binding and broader V20–V32 completion remain open.


### V32 FIFO receipt timestamp binding — 2026-10-12

V32 source review found timing reports lack universal task dimension binding: existing workload-specific calibration remains authoritative, so no generic report timing fallback wired. DRR affinity receipt now freezes selected task CreatedAt and rejects a changed head timestamp before replacing receipt; replacement carries actual selected timestamp. Added atomic changed-created refusal and replacement timestamp preservation assertions.

NOT RUN. Persisted dimension/workload timing integration remains required; helper/report persistence does not establish universal timing authority. Full V20–V32 completion remains unproven.


### V32 scheduler SQL startup contract — 2026-10-12

V32 runtime startup now checks catalog readiness for seven required scheduler SQL signatures before loading installed policy or creating pool dispatchers. Requires SECURITY DEFINER, photo_fn_owner ownership, fixed pg_catalog/photo search_path, scheduler EXECUTE and absence of API/worker EXECUTE. Partial/misconfigured deployment closes leader through existing startup error path. Added readiness/refusal/query error/cancellation/owned inventory tests.

NOT RUN. Catalog search_path normalization and real ACL drift/startup fixtures remain unverified. No migration added; positive dispatch and remaining product requirements remain open.


### V32 catalog drift readiness matrix — 2026-10-12

V32 catalog readiness now coalesces each function predicate to false before bool_and, refusing absent proconfig instead of ignoring NULL rows. Authored actual Go/pgx integration matrix for all seven signatures: complete baseline and transactional missing function/API-worker-PUBLIC grant/scheduler revoke/owner/security invoker/wrong or unset search_path drift. Calls production CheckCapacitySchedulerContract as scheduler, rolls back every catalog mutation.

NOT RUN. Requires isolated fully migrated operator database. Catalog normalization, mutation permissions and readiness matrix remain unverified until testing phase; no full-product completion credit.


### V32 runtime SQL contract recheck — 2026-10-12

V32 CapacityRuntime now rechecks full scheduler SQL signature/owner/SECDEF/search_path/ACL contract on every Dispatch before policy validation and before any CPU/GPU/signer pool. Existing terminal error path closes leader and clears dispatchers on drift. Source review of V31 confirms callable deployed signer entrypoint exists but independent native inspectors/verifiers, qualified binary packaging and real key/trust provisioning remain unresolved; no deployed execution completion claimed.

NOT RUN. Catalog matrix authored previously covers production checker; actual running-runtime drift/lifecycle acceptance remains open. No unsafe catalog commits or fake nested-transaction fixture added. Whole-product completion unproven.


### V31 concrete certificate inspector — 2026-10-12

V31 concrete CertificateBoundInstallationInspector authored: exact policy certificate SHA, bounded PEM chain, all certificate validity windows before/after mandatory live authority, leaf RSA>=2048/PSS or exact EC curve/Ed25519 algorithm and digitalSignature KeyUsage. Rejects naive clock and policy mutation; never replaces native/trust/revocation/network authority. Added generated Ed25519 cert tests for pin/expiry/algorithm/usage/clock/live refusal/expiry during callback.

NOT RUN. Inspector not yet wired to deployed composition; real chain/HSM/native qualification and packaging remain required. No deployed signer completion claimed.


### V31 deployed certificate inspector composition — 2026-10-12

V31 SignerDeployment.build_executor now always wraps mandatory independent native_inspector with CertificateBoundInstallationInspector before PKCS11/native/file/key composition. Missing KeyUsage normalized to explicit refusal. Authored deployment factory type assertion, real generated certificate composition propagating live-authority refusal, and missing-authority refusal before factory/PIN work.

NOT RUN. Deployed composition source wired, but qualified native SDK/HSM/trust/revocation/network authorities, packaging and end-to-end execution remain open. No deployed signer completion credit.


### V31 certificate algorithm and cancellation boundaries — 2026-10-12

V31 deployed certificate inspector now enforces documented RSA2048–8192 bounds and refuses CA leaf certificates. Expanded generated-certificate coverage across PS256/384/512, P256/P384/P521 exact curves, Ed25519, weak/wrong/unknown algorithms, missing KeyUsage, CA leaf, callback policy mutation and cancellation. Independent live trust/native authority still mandatory; certificate checks do not replace chain trust/revocation.

NOT RUN. Synthetic certificates do not qualify production signer, SDK/HSM or deployment. Full lifecycle/packaging and remaining product requirements remain open.


### V30 cancellable bounded multipart drain — 2026-10-12

V30 multipart drain listing now uses cancellation-aware select and10000-row conservative bound instead of unbounded range waiting for producer close. Cancellation/overflow never grants quiescence; existing Abort+NoSuchUpload probe and exact-key absence evidence retained. Authored cancelled-open-channel, exact-limit, overflow, nil-channel and producer-context cleanup tests.

NOT RUN. Unknown Create completion and server writer quiescence still require actual storage/runtime acceptance; no cleanup or full V30 completion claimed.


### V30 physical writer version consistency — 2026-10-12

V30 completion writer and recovery now share validArchiveWriterVersion: nonempty/non-null, <=1024bytes, valid UTF8, no CR/LF/NUL. Prevents persisting physical receipt that recovery later rejects. Authored completion boundary matrix asserts invalid receipt never reaches SQL and exact valid version is preserved. Source confirms unknown Begin observation cannot close open writer safely; no false quiescence recovery added.

NOT RUN. Unknown Create/server writer quiescence and native storage completion acceptance remain open; no full V30 or product completion credit.


### V30 shared bounded absence listing — 2026-10-12

V30 purge had the same unbounded cancellation-insensitive incomplete-upload listing as drain. Extracted shared awaitInterchangeMultipartAbsent consumer and wired both purge/drain;10000row bound, error propagation and context cancellation never grant absence. Authored purge cancelled-open-channel/exact-limit/overflow and producer-context cleanup scenarios.

NOT RUN. Storage-native writer quiescence and unknown Begin remain open. This closes authored consumer behavior only, not backend eventual-consistency or complete cleanup acceptance.


### V30 observed receipt version and cancellation — 2026-10-12

V30 shared objectstore.ValidInterchangeVersionID now used by completion observation and SQL writer close/recovery, avoiding drift between physical and persisted receipts. Observation rejects empty configured bucket; late context cancellation after stable second read returns zero receipt plus error. Authored CR/LF/NUL/invalidUTF8/1025byte refusal,1024byte boundary and cancellation-on-second-read tests.

NOT RUN. Native storage completion/unknown Begin/writer quiescence remain unverified and incomplete; no full V30 or whole-product completion claim.


### V30 bounded ledger inspection and inventory refresh — 2026-10-12

V30 SQLArchiveBeginInspector now bounds its authenticated lock+storage observation+recheck transaction to30seconds and checks caller cancellation before any dependency validation. Added cancelled-before-dependencies test. Refreshed stale optical.perspective registry note using current automatic LSD detector and CPU adapter sources; no semantic completion reclassification.

NOT RUN. Inspector DB/storage timeout and full perspective lifecycle still require execution acceptance; unknown Begin remains open, no whole-product percentage credit.


### V30 Delivery normative gaps identified — 2026-10-12

V30 Delivery audit identified explicit missing behavior beyond unrun tests: required provenance returns503 in create/assembly, metadata preservation remains strip-only across compilers and encoded verifier, watermark rejected by child export request. Updated authoritative partial entrypoint limitations and added source-grounded functional gap/dependency/acceptance inventory. Next implementation must extend end-to-end contracts before removing refusals.

Source inspection only; NOT RUN. No implementation completion credit, percentages unchanged. Full Delivery required provenance/metadata/watermark remain explicitly incomplete.


### V30 Delivery closed metadata projection — 2026-10-12

V30 Delivery metadata foundation: closed projection over existing MetadataDocument for strip/copyright/approved_exif. Approved EXIF requires explicit unique frozen field allowlist; GPS never implicit, unknown fields refused. Owned canonical source prevents nested mutation. Matching verifier rejects extra/changed fields and requires actual encoded-byte extraction as upstream evidence. Authored projection/approval/ownership/substitution tests.

NOT RUN. Projection does not establish source approval; authenticated frozen metadata contract/SQL compiler/encoder/extraction/adoption/assembly wiring still required. Existing unavailable gates intentionally retained pending complete implementation; no Delivery completion credit.


### V30 encoded copyright EXIF verification — 2026-10-12

V30 copyright metadata verifier now extracts actual Artist/Copyright from opened encoded image EXIF, compares closed source projection and refuses extra camera/GPS/thumbnail/unknown fields or XMP/IPTC/comment containers. Structural orientation and top/nested raster dimensions checked; no writer-report/manifest authority. Authored actual PNG EXIF success/missing/substituted/camera/orientation/dimension/extra-container scenarios.

NOT RUN. Caller must still verify immutable file/hash/pixels/ICC/stability. Helper not yet wired to expanded Delivery adoption; approved EXIF/GPS and frozen admission/encoder/assembly integration remain open. No mode availability or completion credit.


### V30 physical encoded copyright integration — 2026-10-12

V30 physical Delivery encoded verifier now supports explicit post-metadata copyright verification for JPEG/PNG when authenticated coordinator supplies MetadataDocument and manifest is copyright_only. Existing hash/size/dimension/ICC/container/decoded raster/file-stability checks retained; manifest alone cannot authorize metadata. Default strip and unimplemented TIFF/safe_camera paths remain fail-closed. Added real JPEG success/missing authority/substitution/extra camera/strip mismatch tests.

NOT RUN. Production adoption does not yet supply frozen metadata authority; public copyright mode remains unavailable. TIFF/approved EXIF and full frozen source/compiler/metadata task/adoption/assembly wiring remain required; no module completion credit.


### V30 TIFF copyright physical verification — 2026-10-12

V30 physical copyright verification extended to TIFF: bounded header-only Pillow EXIF projection with explicit TIFF structural/ICC tag allowlist, while tifffile retains exact16bit RGB/alpha/layout/ICC/raster validation. Software/ImageDescription/camera/GPS/private metadata refused. Added real16bit TIFF Artist/Copyright success and extra camera refusal fixtures.

NOT RUN. TIFF header/library behavior and native metadata writer qualification still unverified. Frozen metadata admission/task/adoption/assembly wiring remains open; public mode not enabled and no full Delivery completion credit.


### V30 frozen metadata evidence — 2026-10-12

V30 authored FrozenDeliveryMetadata evidence binds exact VersionBinding, metadata artifact SHA, policy, canonical unique sorted approval fields and derived MetadataDocument projection. Freeze validates exact source canonical bytes/hash; independent restore verifies frozen SHA/canonical bytes/scope/source/projection, returns owned data. Added valid/different version/source/projection/digest tests.

NOT RUN. This private evidence contract does not establish authenticated ACL/approval and is not yet integrated into shared machine schema/Go freeze/SQL admission/task transport/adoption. No public mode availability or completion credit.


### V30 Go frozen metadata compiler — 2026-10-12

V30 Go MetadataDocument/GPS/FrozenMetadata and FreezeDeliveryMetadata compiler authored to match private worker frozen evidence: exact canonical source SHA, complete version/canvas/ref binding, validated bounded text/GPS/date, approved field allowlist, owned projection and sorted canonical receipt SHA. Added exact source/substitution/approval/ownership tests.

NOT RUN. Cross-language parity and integration into shared schemas/SQL authenticated source approval/task/adoption remain open; no mode enabled or completion credit.


### V30 shared frozen metadata wire corpus — 2026-10-12

V30 shared machine schema now declares DeliveryMetadataDocument and FrozenDeliveryMetadata; common four-case canonical JSON/SHA corpus covers strip/copyright/approved camera+GPS/empty approval. Go compiler and worker freeze/restore tests consume identical expected bytes and SHA. No implementation code or generator executed to produce test results.

NOT RUN. Shared schemas/corpus authored only, no validators/tests/builds run. SQL authenticated source approval, transport/adoption/assembly remain incomplete; no public mode availability or completion credit.


### V30 server metadata receipt restore — 2026-10-12

V30 Go RestoreDeliveryMetadata now independently recompiles exact current binding/source/policy/approval expectations and requires identical canonical receipt bytes/SHA, refusing receipt-selected policy or permission broadening. Shared corpus tests extended to restore, foreign version and caller policy mismatch. Source audit found ingest AssetMetadata is technical image metadata, not normalized copyright/GPS MetadataDocument; direct binding would be invalid and requires explicit original extraction/approved document lifecycle.

NOT RUN. Normalized metadata source creation and authenticated SQL approval/transport/adoption remain incomplete; no public mode availability or completion credit.


### V30 normalized original metadata extraction — 2026-10-12

V30 normalized original metadata extractor authored over caller-verified EXIF: allowlisted author/copyright/camera/lens/date fields only; unknown/private fields ignored. GPS explicit bool opt-in, DMS/hemisphere/finite/boundary/altitude validation, cancellation before/after. Actual JPEG source and GPS opt-in/malformed/hemisphere/range tests authored.

NOT RUN. Does not authenticate original SHA/ACL or create/adopt metadata artifact. RAW metadata extraction and complete source artifact/approval/SQL/task/adoption lifecycle still required; no mode enabled or completion credit.


### V30 physical metadata source preparation — 2026-10-12

V30 metadata source preparer now binds normalized EXIF to actual immutable original bytes: absolute nonsymlink regular single-link file, exact declared size and streamed SHA, MIME/frame/pixel bounds, same-descriptor parsing, before/after descriptor/path identity checks and cancellation. Returns immutable original/metadata canonical bytes+SHA, no publication or SQL authority. Added actual JPEG success/hash/size/MIME/symlink/hardlink/cancel tests.

NOT RUN. RAW source extraction, artifact reservation/publication and authenticated version/approval/SQL/task/adoption lifecycle remain open. No mode enabled or full Delivery completion credit.


### V30 Delivery original SQL provenance — 2026-10-12

V30 migration01676 API-only Delivery metadata original reader delegates existing exact asset version/master/original physical provenance and editor-session gate, adds explicit tenant context and active download-enabled project. Does not approve normalized metadata/GPS or establish canvas bytes. ACL/missing-scope tests authored; no new source-of-truth table duplicated.

NOT RUN;01676 NOT APPLIED. Positive original provenance/authorization race and Go loader/canvas/source preparation/publication integration still required. Delivery metadata public modes remain unavailable.

### 2026-10-12 — Delivery original loader

V30: authored SQLDeliveryMetadataOriginal Go loader using dedicated01676 Delivery authority after exact WorkingImage bytes/canvas verification; enforces original content SHA, configured bucket, immutable version, safe byte bound and current JPEG/PNG/TIFF extractor allowlist. Added source tampering, canvas substitution, cancelled lookup, immutable source and SQL authority unit cases.

NOT RUN;01676 NOT APPLIED. Loader is not yet wired into metadata artifact reservation/publication, approval, task freeze or adoption. RAW extraction and complete metadata modes remain open; no full-module completion credit.

### 2026-10-12 — External metadata approval authority

V30 Python frozen metadata restore now requires external task policy/approved_fields and recompiles exact expected canonical bytes, matching Go restore authority. Self-consistent receipts cannot widen GPS/camera approval, switch policy or silently remove it. Added permission substitution and owned restore unit cases; updated shared corpus callers.

Tests NOT RUN. This closes a restore contract gap only; task admission/metadata publication/adoption and public modes remain incomplete. No module completion credit.

### 2026-10-12 — Composed metadata preparation

V30 added composed worker prepare_delivery_metadata: validates binding/artifact separation and exact approval before private file IO; owns validated inputs; verifies original bytes through source preparer; enables GPS extraction only for explicitly approved_exif gps; returns normalized source/ref and exact frozen projection bytes+SHA. Authored actual JPEG strip/copyright/approved camera projection and pre-IO alias/approval/cancellation unit cases.

Tests NOT RUN. Caller must authenticate original/version lineage and approval; outputs require SQL reservation, immutable publication, completion and task wiring. No public mode enabled, no full-module completion credit.

### 2026-10-12 — Explicit GPS signs

Fixed existing metadata.write GPS EXIF emission: writes absolute coordinate/altitude magnitudes with explicit N/S, E/W and altitude reference tags, retaining GPS opt-in. Authored argv tests for negative coordinates/below-sea altitude, poles, zero, absent altitude and opt-out.

Tests NOT RUN; real ExifTool roundtrip pending. Pixel/ICC preservation checks remain in existing execution path. Delivery publication/admission remains open; no full-module completion credit.

### 2026-10-12 — Actual written metadata verification

Existing metadata.write now verifies actual rewritten EXIF before staging: exact allowed text projection, explicit GPS opt-in and bounded rational rounding, closed top/nested/GPS tag allowlists and rejection of private container metadata. Pixel/ICC checks retained. Added actual PNG missing/substituted/private/unapproved camera and allowed camera tests; controlled writer fixture now writes required author.

Tests NOT RUN. Real ExifTool JPEG/PNG/TIFF qualification and GPS rational roundtrip remain pending; Delivery task/publication/adoption not complete. No full module completion credit.

### 2026-10-12 — Written GPS and geometry integrity

metadata.write verifier now rejects contradictory top-level/nested EXIF dimensions and orientation before publication. Added controlled GPS IFD tests for southern/western coordinates, below-sea altitude, wrong references, missing/extra GPS, opt-out, bounded rational rounding and changed coordinates; private MakerNote/dimension/cancellation cases authored.

Tests NOT RUN; controlled GPS tests do not establish real ExifTool roundtrip. Existing pixel/ICC checks retained; Delivery lifecycle remains open. No completion percentage credit.

### 2026-10-12 — Frozen projection writer preparation

V30 Delivery writer preparation now verifies frozen bytes against external task policy/approvals and passes only frozen projection to existing EXIF writer. Copyright maps to copyright_only; approved_exif uses projected safe_camera fields with GPS explicit opt-in. Strip/non-strip source refusal; authored projection/empty selection/GPS signed args unit cases.

Tests NOT RUN. No new worker operation advertised; qualified execution, artifact reservation/publication, SQL task/adoption lifecycle remain required. Public non-strip gate unchanged; no module completion credit.

### 2026-10-12 — Approved EXIF physical verification

Delivery physical encoded verifier accepts explicit externally supplied safe_camera projection authority, independently checks actual EXIF through closed metadata verifier for JPEG/PNG/TIFF while retaining SHA/ICC/format/16bit TIFF checks. Manifest cannot select this mode by itself; wrong/missing authority refused. Actual JPEG camera-only approval/substitution/unapproved-author unit cases authored.

Tests NOT RUN. Caller must derive projection from task-pinned frozen metadata; production prepare still passes no metadata authority and remains strip-only. Full SQL task/publication/adoption lifecycle open; no module completion credit.

### 2026-10-12 — Frozen task to physical metadata verification

V30 added task-pinned frozen metadata physical verifier composition: restores exact policy/approvals/version/source document before inspecting encoded file, then verifies actual bytes against frozen projection with matching manifest mode. Added actual JPEG success, widened approval, foreign version, extra camera and manifest policy refusal cases.

Tests NOT RUN. Coordinator must authenticate task/source/recipe lineage and immutable refs. Production preparation remains strip-only pending metadata task/publication/SQL integration; no full-module credit.

### 2026-10-12 — Lease-aware metadata tool execution

metadata.write production tool execution now uses lease-aware Popen wait slices<=250ms with120s deadline, DEVNULL output (no unbounded PIPE allocation), kill+reap on cancellation/lease loss/timeout and pre-staging active check. Controlled injected runner compatibility retained. Authored process success/nonzero/deadline/cancel/pre-spawn refusal tests.

Tests NOT RUN. Real ExifTool execution/termination qualification pending; Delivery metadata task/publication lifecycle still open. No module completion credit.

### 2026-10-12 — Metadata publication refusal lifecycle

metadata.write adds explicit active checks before each manifest/report stage. Controlled actual PNG writer lifecycle tests now cover unchanged pixels but missing/substituted author/private EXIF and cancellation after tool completion, requiring zero publication on each refusal, alongside pixel mutation and valid path.

Tests NOT RUN. Scoped backend document publisher inspection confirms new metadata purpose requires explicit SQL task authority; no allowlist bypass introduced. Metadata reserve/complete/task lifecycle remains open. No full-module credit.

### 2026-10-12 — Original SQL positive lifecycle fixture

Authored positive V30 metadata original SQL lifecycle fixture using actual tenant/project/session, original/master artifacts and asset version. API-role reads exact original; foreign app tenant, disabled downloads, expired/unverified/unversioned original and revoked session refuse; restoration permits exact replay.

NOT RUN;01676 NOT APPLIED. Fixture is authored coverage, not current SQL evidence. No metadata approval/reservation lifecycle added; full Delivery remains incomplete.

### 2026-10-12 — Full-precision TIFF pixel identity

Fixed production decoded_image_fingerprint TIFF fidelity: header-bounded single-page layout validation, full sample precision via tifffile, canonical endian/dtype/shape-aware row hashing and cancellation. Existing metadata writer now detects RGB16 low-bit changes that Pillow RGB decode could truncate. Authored one-LSB RGB16 change and equivalent little/big-endian TIFF identity tests.

Tests NOT RUN. TIFF decode uses full bounded page allocation; resource qualification and real metadata writer roundtrip remain pending. Full V30 metadata lifecycle not complete; no module completion credit.

### 2026-10-12 — Physical frozen metadata rewrite

V30 implemented physical frozen metadata rewrite composition: validates private paths/immutable source raster, bounded SHA-checked exclusive copy, full precision decoded pixel fingerprint, lease-aware tool invocation, actual frozen EXIF/ICC/container/SHA verification, owned manifest receipt and failure cleanup. Source untouched. Authored actual JPEG APP1 insertion without recompression success and missing author/pixel mutation/tool error/cancel cleanup tests.

Tests NOT RUN. Function is not yet installed as worker operation or connected to SQL reservation/publication/adoption; caller must supply authenticated task lineage/private scratch and qualified tool. Full Delivery remains incomplete; no module completion credit.

### 2026-10-12 — Rewrite ownership and output stability

Physical Delivery metadata rewrite now owns all typed input contracts before callbacks and requires unchanged output file identity across SHA/EXIF/pixel verification. Validates output UUID and bounds physical verifier installation to safe integer bytes and<=200M pixels. Added caller model mutation during tool execution and unsafe/bool limit tests.

Tests NOT RUN. Private scratch and qualified tool remain caller responsibilities; production SQL task/publication integration still open. No full module completion credit.

### 2026-10-12 — Existing metadata worker task compiler

V30 Go CompileMetadataWrite compiles existing metadata.write/1 Params and projected MetadataDocument from externally restored frozen receipt. Validates canonical strip encoded manifest/SHA/layout/ICC, deterministic task-scoped projection/Params UUIDs, disjoint artifact IDs and exact three-input worker order. Owned getters, replay, exact projection and foreign version/approval refusal tests authored.

Tests NOT RUN. Plan is not task admission: SQL reserve/complete, frozen-pin persistence, graph expansion and adoption coordinator must still be wired. Existing worker operation reused; no public mode enabled or module completion credit.

### 2026-10-12 — Metadata tool timeout classification

metadata.write now distinguishes exhausted tool deadline (retryable TIMEOUT) from missing/broken tool dependency (retryable DEPENDENCY_UNAVAILABLE), retaining cancellation/lease-loss propagation. Authored operation-level timeout/missing-tool cases require correct classification and zero publication.

Tests NOT RUN. Inspected DeliveryPolicy and create-document SQL authority; metadata projection cannot be published through existing create-purpose allowlist without dedicated frozen task binding. No shortcut grants or public mode changes made; full lifecycle remains open.

### 2026-10-12 — Private SQL frozen metadata validation

Authored01677 private SQL frozen metadata value validator: bounded receipt/source SHA, exact external version/binding/source ref/policy/unique sorted approval, closed normalized field shape/text/GPS ranges and rederived exact projection. All runtime roles denied EXECUTE; this is no admission grant. Shared Go/Python corpus SQL positive/digest/duplicate/unknown approval/substituted projection tests authored.

NOT RUN;01677 NOT APPLIED. SQL validates parsed values; canonical byte serialization remains compiler responsibility. No reservation/task/publication authority introduced; full metadata lifecycle remains open.

### 2026-10-12 — Metadata workflow node compiler

V30 workflow CompileDeliveryMetadata now composes frozen metadata plan into existing CPU metadata.write/1 node with UUIDv5(workflow,metadata.write:0), exact ordered artifact inputs and owned Params. Rejects missing/wrong-version/non-CPU/model-bound registry and invalid timeout. Identity/inputs/owned replay/registry/pin tests authored.

Tests NOT RUN. Does not admit/stage node or alter graph finalization. Current child SQL config/receipt remains strip-only; metadata barrier, durable pin, document reservation, graph expansion/adoption integration still required. No completion credit.

### 2026-10-12 — SQL metadata worker plan proof

Authored01678 private SQL metadata.write plan verifier: independently composes frozen metadata validation, closed strip encoded manifest/SHA/layout/ICC, deterministic projection/Params identity, disjoint source/artifact IDs, exact projection and worker Params including GPS opt-in. Authored shared-corpus plan success/policy/GPS/extra field/alias/projection/task substitution and private ACL tests.

NOT RUN;01678 NOT APPLIED. Parsed value validation is not canonical byte proof or authenticated admission. Existing Delivery activation remains strip-only; metadata barrier/reservation/publication/adoption still required. No module completion credit.

### 2026-10-12 — Full-precision PNG16 pixel identity

Fixed production PNG16 decoded fingerprint precision: recognizes16bit header, validates bounded single-frame geometry, bounded compressed bytes and cv2 unchanged uint16 decode, hashes all color/alpha samples in canonical endian with ICC/orientation/transparency. Authored RGB/RGBA low-bit mutation and compression-independent identity tests. Existing metadata writer and Delivery rewrite inherit full precision checks.

Tests NOT RUN. Full-raster decode and compressed buffering need resource qualification; no runtime memory PASS claimed. Metadata graph expansion/pin/reservation/adoption remains open; no module completion credit.

### 2026-10-12 — Metadata write retry proof

V30 authored canonical bounded MetadataWriteProof bundle containing exact encode/frozen/source documents, deterministic projection and Params. Restore requires external task/encode ref/binding/source ref/policy/approval, fully recompiles and requires exact bytes/SHA; receipt-selected permissions or self-consistent modified Params refused. Extended compiler tests with proof replay/task/approval/substitution cases.

Tests NOT RUN. Proof is not durably pinned yet; graph expansion lease/pin storage, document reserve/complete and metadata adoption remain open. No public mode enabled or module completion credit.

### 2026-10-12 — Durable metadata expansion proof pin

Authored01679 durable immutable metadata proof columns on existing graph_expansions (existing tenant purge lifecycle retained), SHA/size/kind constraint and immutable-update trigger. Scheduler-only pin requires live metadata barrier lease, running unsealed child, config-bound policy/approval/binding, independently verified plan/ref identities, canonical encode producer and ready immutable input refs; exact replay only. ACL/missing-barrier tests authored.

NOT RUN;01679 NOT APPLIED. No positive barrier/pin/concurrency evidence. Barrier creation, original extraction lineage/approval admission, source publication, projection/Params reserve/complete, graph activation/adoption still required; existing strip-only gates unchanged. Pin cannot dispatch work; no full module credit.

### 2026-10-12 — Go metadata proof pin adapter

V30 SQLMetadataProofStore.Pin now connects exact metadata proof restore to01679 SQL lease boundary. Validates child workflow-derived task identity and external scope/policy/approval; owns body/approval before DB call; propagates DB errors/cancellation and maps refused pin to409. Authored positive adapter/stale/pre-and-post-SQL cancellation/foreign child cases.

Tests NOT RUN;01679 NOT APPLIED. Adapter is not installed in scheduler metadata loop; barrier creation/reservation/publication/adoption still open. Fake DB tests do not establish positive SQL pin lifecycle. No full-module credit.

### 2026-10-12 — Pending metadata proof recovery

Authored01680 scheduler-only pending metadata proof reader repeating01679 current lease/encode/source authority before returning immutable bytes. Go SQLMetadataProofStore.Load validates child task identity, restores against external scope/policy/approval and propagates missing/stale/cancelled/corrupt proof refusal. Adapter restore/corrupt/missing/late cancel and SQL ACL/missing scope tests authored.

Tests NOT RUN;01680 NOT APPLIED. Positive SQL barrier pin/read lifecycle and scheduler wiring not established. Barrier creation, source/projection/Params publication, graph activation/adoption remain open; no module credit.

### 2026-10-12 — Metadata document storage lifecycle

Authored01681 metadata projection/Params document scope and scheduler-only reserve/complete using exact durable proof, existing backend_documents quota and version lifecycle, deterministic refs, token fencing, ready replay and lease expiry rollback. Added projection purpose preserving existing constraint expression.01679/01680 pin/read lock ordering changed to run before expansion; reservation takes usage before run/expansion. ACL/missing proof/invalid version tests authored.

Tests NOT RUN;01681 NOT APPLIED. Positive storage/quota/replay/concurrency/GC/purge evidence absent. Physical publisher adapter and scheduler wiring, original lineage approval, barrier creation, metadata adoption still open. No public mode or completion credit.

### 2026-10-12 — Immutable metadata document publisher

V30 MetadataDocumentRepository and PublishMetadataDocuments now connect exact plan projection/Params to01681 reserve/complete around existing immutable object publisher. Bound tenant/child/task/lease, canonical body SHA/size and deterministic refs; strict object key/version/MIME/size/SHA receipt; projection then Params publication with ready replay. Object-key allowlist includes lease-fenced projection only. Authored actual buffer publication/replay/foreign scope adapter cases.

Tests NOT RUN;01681 NOT APPLIED. DB adapter must use tenant-scoped scheduler calls without an SQL transaction spanning object IO. Positive SQL/object lifecycle, recovery/quota/GC/purge evidence and scheduler wiring remain open; no public mode or module completion credit.

### 2026-10-12 — Tenant-isolated metadata SQL transactions

V30 production factories now compose metadata proof/storage repositories with MetadataTenantQueries: per-call30s transaction, SET LOCAL tenant, exact four-query allowlist, commit before returning to immutable IO, bounded cancellation-independent rollback. Foreign tenant/arbitrary SQL refused before pool access. Repository factory owns plan/approval and validates both publication purposes. Transaction ordering/error/cancel/isolation unit cases authored.

Tests NOT RUN. Factories do not yet run in scheduler metadata loop. Real SQL transaction/object IO lifecycle, barrier creation, source approval/adoption and full V30 remain open. No completion credit.

### 2026-10-12 — Metadata result logical binding

V30 metadata result contract verifier now independently restores frozen authority, revalidates/owns Params/manifests/report, derives exact task-scoped projection ref/SHA, requires matching policy/GPS permission/report/copied field order and unchanged raster claims with disjoint refs. Returns immutable canonical evidence only. Authored exact success and Params/policy/report/duplicate fields/projection/dimension/alias refusal cases.

Tests NOT RUN. This validates logical result binding only, never actual raster bytes or SQL adoption. Result evidence reader/physical pair verification and SQL metadata adoption/scheduler wiring still required. No module completion credit.

### 2026-10-12 — Independent metadata raster pair verification

V30 independent metadata file-pair verifier composes logical frozen result binding with original-strip/output-projection physical SHA/container/ICC/layout checks, full precision pixel fingerprints and whole-pair file identity guards. Refactored encoded verifier to shared owned manifest physical core with nonsymlink/single-link private file checks. Logical binding now checks exact original manifest SHA. Authored actual JPEG lossless APP1 success/missing-author/pixel-change cases.

Tests NOT RUN. Shared verifier refactor/current encode path regression not executed. SQL result evidence reader and metadata adoption remain open; no public mode or full module credit.


### 2026-10-12 — Metadata result evidence preparation

V30 metadata result preparation now loads SHA-checked canonical input/output documents, rejects alias and nonexact output closure, validates frozen policy/projection before raster IO, materializes bounded hash-checked streams in private temporary files, composes independent physical pair verification, and rechecks all evidence and output closure after IO. Authored adapter success and extra-output/digest/evidence-change/policy/oversized-block refusal cases with cleanup assertions.

Tests NOT RUN. Physical verifier is injected in adapter tests; actual raster verification is covered by separately authored file-pair tests, also NOT RUN. SQL evidence reader/adoption and scheduler installation, metadata barrier/source approval lifecycle remain open. No public mode or full module completion credit.


### 2026-10-12 — Metadata version-pinned evidence reader

V30 DeliveryMetadataEvidenceReader now provides bounded exact-version object IO behind explicit immutable tenant/task/attempt/fence/result authority scope. Every access rereads authority and pins bucket/key/version/SHA/size/role/source; exact three distinct output refs required. Streaming propagates lease cancellation, closes response and rechecks authority after IO. Preparation normalizes boundary refs. Authored version/key/scope/source/size/bucket substitution, output closure, unsafe versions, exact object pin and cancellation cleanup unit cases.

Tests NOT RUN. Authority interface has no production SQL implementation yet; this object reader does not establish metadata task admission, result context, SQL adoption or scheduler installation. Existing public metadata gates remain unchanged. No module completion credit.


### 2026-10-12 — SQL metadata result authority

V30 authored01682 private metadata result readiness and scheduler-only artifact/output SQL readers: current tenant/task/attempt/fence/canonical result, sealed child/closed metadata barrier, externally bound frozen proof and deterministic Params/projection, live encode producer, input closure and three ready versioned producer outputs. SQLMetadataResultAuthority reads each request in a short tenant-local transaction committed before object IO. Adapter scope/missing/digest/inventory tests and SQL ACL/missing scope cases authored. Operator migration range extended through01682.

Tests NOT RUN;01682 NOT APPLIED. Positive SQL metadata lifecycle, quota/replay/concurrency, source approval/barrier activation, final adoption and scheduler installation remain open. Read authority does not dispatch or publish exports. No full module completion credit.


### 2026-10-12 — Metadata physical verification receipt

V30 authored01683 metadata physical verification receipt under tenant/run/task/artifact/upload locks, repeating current result authority and exact pinned Params/before/projection, unchanged encoded claims, closed report policy/fields and output SHA/size/MIME. Added prepare_current_delivery_metadata composition of SQL authority/object reader/physical preparation and scoped record_prepared_delivery_metadata transaction with receipt hash/ref checks and cancellation rollback. Authored adapter transaction/substitution/refusal/late cancellation and SQL ACL/missing-result tests.

Tests NOT RUN;01683 NOT APPLIED. Receipt is verification only, not export/item adoption or publication. Positive metadata SQL lifecycle/concurrency and final adopter, source admission/barrier activation, scheduler installation remain open. No full-module completion credit.


### 2026-10-12 — Atomic metadata export adoption

V30 authored01684 atomic metadata export/item adoption reusing encode lock/event lifecycle: parent frozen policy/provenance-off gate, exact metadata barrier/encode parent, repeat verification including replay, retain inputs plus all three outputs, rebind export task to metadata producer, item revision and inbox/events/outbox atomic update. Added dedicated Python adopt_prepared_delivery_metadata using shared receipt checks and tenant-scoped write transaction. Authored adapter accepted/refused adoption and SQL ACL/missing-scope tests.

Tests NOT RUN;01684 NOT APPLIED. Public metadata modes remain disabled. Positive adoption/replay/cancellation/concurrency and downstream observation/assembly compatibility, original source approval/barrier activation and scheduler installation remain open. No complete V30 credit.


### 2026-10-12 — Policy-aware export observations

V30 authored01685 shared private policy-aware delivery producer readiness and exact-definition-guarded replacement of both expansion/current publication observations. Metadata policies require metadata.write producer, closed bound proof, current result/upload and both physical verification/adoption receipts; strip requires encode producer/verification. Preserves review/completed package observation behavior and existing role grants. Authored SQL private ACL/missing authority/policy cases plus metadata adopter late-cancellation rollback unit case.

Tests NOT RUN;01685 NOT APPLIED. No positive downstream SQL lifecycle/review/publication proof. Assembly/shard policy gates, source approval/barrier activation, scheduler installation and full metadata pipeline remain open. No full-module credit.


### 2026-10-12 — Metadata assembly matrix support

V30 BuildAssemblyInput now accepts the closed strip/copyright/approved_exif policy set and preserves it in canonical assembly across shards; unknown policy refused before publication. Authored01686 exact-definition-guarded shard scope and runtime assembly matrix policy support, retaining provenance-off and exact observed export/matrix requirements. Authored Go501-slot policy preservation/preflight tests and worker assembly SHA commitment/unknown policy tests.

Tests NOT RUN;01686 NOT APPLIED. Upstream admission/frozen prepare-source/child request gates remain strip-only until authenticated original metadata approval/barrier lifecycle is installed. No enabled public metadata mode, positive SQL lifecycle or complete assembly/Delivery credit.


### 2026-10-12 — Blocked metadata task registration

V30 authored01687 scheduler-only blocked metadata.write task registration from live pinned metadata proof: tenant/usage/run/expansion fencing, ready versioned exact Params/projection documents, deterministic task/CPU profile, unchanged blocked replay, encode success edge and run output quota reservation. No transport or activation occurs. Authored SQL ACL/missing-proof/invalid-reservation refusal cases.

Tests NOT RUN;01687 NOT APPLIED. Positive task creation/quota/replay/concurrency remains unverified. Authenticated source approval, barrier creation, input closure/transport/activation and scheduler wiring remain open. Registration cannot dispatch and public metadata modes remain disabled. No full-module credit.


### 2026-10-12 — Metadata input transport and activation

V30 authored01688 metadata transport authority requiring exact four proof-bound non-closure refs, ready immutable objects/documents, current blocked task/profile/input digest, sealed input closure and encode success edge. Authored01689 activation under usage/run/expansion/task/quota locks with repeated transport/lease checks, CPU registry, encode verification receipt, closed sibling barriers, graph seal, activate_bound_task and predecessor adoption marker. SQL ACL/missing-proof tests authored.

Tests NOT RUN;01688–01689 NOT APPLIED. Positive transport/activation/concurrency/retry lifecycle remains unverified. Input registration adapter, activation replay receipt, authenticated original/source approval, metadata barrier creation/reopening and scheduler installation remain open. Public metadata modes disabled; no full module credit.


### 2026-10-12 — Metadata activation recovery adapter

V30 authored01690 exact closed metadata activation recovery: original token, sealed current run, externally restored frozen approval/policy/plan, exact task/profile/input closure/documents/refs and encode verification/adoption markers. Added Go MetadataTaskLifecycle create/transport/activate/recovery adapter, deterministic scope/canonical task checks, cancellation propagation and tenant transaction query allowlist. Adapter authority/recovery/refusal/quota/cancellation and SQL ACL/missing barrier tests authored.

Tests NOT RUN;01690 NOT APPLIED. Positive activation recovery/concurrency not proven. Lifecycle adapter is not installed in scheduler; barrier creation/original approval/input registration remain open. No public metadata mode or full-module credit.


### 2026-10-12 — Metadata typed input closure compiler

V30 added typed exact metadata input closure compiler: closed canonical existing MetadataParams, deterministic Params/projection IDs, disjoint four refs, Params/EncodedImage/EncodedRaster/MetadataDocument inventory and owned digest/cursor. MetadataWritePlan now composes this closure and publishes transport using existing BuildInputClosure. Authored alias/projection substitution/owned Params cases.

Tests NOT RUN. Transport publisher must still be backed by metadata-specific reservation authority and graph registration must bind published transport; these are not installed in scheduler. Metadata resource profile, original approval/barrier creation and full integration remain open. No public metadata mode or complete module credit.


### 2026-10-12 — Metadata transport registration adapter

V30 metadata plan now binds published transport by reconstructing canonical chunks/manifest from owned exact inventory and checking all refs/count/digest/direct inputs without invoking caller cursor. Shared transport binding extracted preserving prepare path. RegisterInputsInTransaction validates exact draft/Params/input digest, checks activation and registered replay, creates fenced blocked shell if needed, registers through existing input batch/manifest lifecycle and repeats transport authority. Authored activated replay, hostile cursor and substituted inventory tests in compiled metadata fixture.

Tests NOT RUN. Shared prepare binding regression not executed. Metadata-specific transport reservation/object publisher and generic input registration compatibility require implementation/qualification; source approval/barrier creation/resource profile/scheduler wiring remain open. No complete module credit.


### 2026-10-12 — Metadata transport document publisher

V30 authored01691 exact-definition guarded metadata document scope extension deriving canonical four-entry transport chunk and manifest from pinned proof, deterministic task-scoped IDs, exact bytes/SHA and ready chunk prerequisite. Existing metadata reservation/completion lifecycle reused for quota/object versions. Added NewMetadataTransportPublisher with owned deterministic transport compilation, exact repository body/ref matching and scoped immutable publisher. Authored deterministic manifest/body ownership/extra chunk refusal tests.

Tests NOT RUN;01691 NOT APPLIED. SQL/Go canonical transport agreement and positive quota/object/replay/GC lifecycle remain unverified. Transport publisher not yet installed in scheduler; original approval/barrier creation/resource profile and end-to-end metadata pipeline remain open. No full module credit.


### 2026-10-12 — Metadata CPU resource profile

V30 added CompileDeliveryMetadataProfile with canonical node/Params/encoded manifest binding, full precision pixel and compressed file RAM allowance, CPU zero VRAM, three-copy scratch, output metadata overhead and safe input/pixel limits. PrepareDeliveryMetadataTask now composes frozen writer plan, exact closure digest and resource profile into blocked DraftTask. Authored estimate, underreserved RAM/scratch/output, byte bounds and task/closure consistency tests.

Tests NOT RUN. Estimates not measured hardware qualification. encodedBytes must be read from trusted current artifact evidence; SQL must independently enforce resource estimate and worker output limits. Scheduler integration/original source approval/barrier creation remain open. No full-module credit.


### 2026-10-12 — Independent metadata resource enforcement

V30 authored01692 independent SQL metadata resource floors from pinned encoded geometry/current immutable raster bytes, exact zero VRAM/pixel/output and RAM/scratch minima, enforced in task creation/transport/recovery with exact definition guards. Worker now checks supplied resource profile before fingerprint decode and output bound before publication. Authored SQL/Python matching floors, undersized fields, byte bounds and private ACL tests.

Tests NOT RUN;01692 NOT APPLIED. Resource formulas remain unmeasured reservations, not hardware qualification. Legacy metadata/resource-profile compatibility and full writer regression unexecuted. Original metadata approval/barrier creation and scheduler integration remain open; no full-module credit.


### 2026-10-12 — Metadata barrier transition

V30 authored01693 metadata barrier transition from independently verified current encode result: tenant/parent/child locks, frozen parent metadata policy/provenance-off binding, slot expansion authority, exact Params/source/encoded document hashes, encode verification receipt, deterministic single-stage barrier and graph reopening. Pending replay checks original producer/documents and does not reserve/dispatch. SQL ACL/missing-child/invalid size tests authored.

Tests NOT RUN;01693 NOT APPLIED. Parent expansion/admission source gates remain strip-only, so this transition is not yet reachable through public metadata admission. Normalized source approval, barrier claim/recovery scheduler composition and positive SQL/concurrency lifecycle remain open. No enabled metadata mode or full-module credit.


### 2026-10-12 — Metadata barrier lease adapter

V30 authored01694 per-tenant/child metadata barrier readiness/claim/renew/release with current parent delivery/encode verification checks, SKIP LOCKED claim,60s rotating token,5s release delay and pending index. Added Go tenant-transaction MetadataBarrierLeaseStore adapters with empty queue/invalid scope/stale/cancellation handling. SQL ACL/missing-scope cases authored.

Tests NOT RUN;01694 NOT APPLIED. Positive competing claim/renew/release/expiry/replay lifecycle unverified. Scheduler loop discovery/composition, original metadata approval/publication and upstream admission remain open; public metadata modes disabled. No full module credit.


### 2026-10-12 — Metadata materialization coordinator

V30 DeliveryMetadataStage now composes owned task/profile/proof compilation, activated recovery, lease renewal, durable proof pin, immutable projection/Params publication, blocked task creation, deterministic transport publication, tenant-scoped input registration transaction and activation. Object IO stays outside SQL TX; predecessor/child scope checked before callbacks. Authored activated replay with no materialization/object IO, foreign predecessor and cancelled stage tests.

Tests NOT RUN. Positive full coordinator lifecycle and fault recovery/concurrency not exercised. Stage requires caller-authenticated normalized source/approval/current raster byte evidence and existing claimed barrier; scheduler discovery/calls and upstream metadata admission remain open. No full-module credit.


### 2026-10-12 — Original metadata evidence materialization

V30 added original metadata preparation through authoritative version-pinned evidence: pre-IO binding/approval checks, bounded64KiB stream/hash/size validation, private temporary file, existing original extraction/frozen projection composition, post-IO authority recheck and cleanup. Authored actual JPEG Artist extraction plus evidence mutation/digest/oversized block/alias/approval refusal and stream/temp cleanup tests.

Tests NOT RUN. Reader authority must establish original asset lineage/current authorization; normalized metadata source SQL publication and admission approval remain open. This prepares source bytes only, not public enablement or scheduler installation. No full-module credit.


### 2026-10-12 — API-authorized original metadata reader

V30 DeliveryMetadataOriginalReader now resolves only the declared original via01676 API-authorized live tenant/project/version/WorkingImage/actor/session scope in short tenant-local SQL transactions with30s statement timeout. Pins exact original key/version/SHA/size/MIME, repeats authority each read and after stream, disallows unrelated refs/JSON/output APIs and closes object stream. Authored exact version/no SQL TX during IO, revocation/location substitution and unrelated ref refusal tests.

Tests NOT RUN. Reader requires photo_api connection and independently verified WorkingImage/canvas plus external explicit metadata approval. Source publication/admission API plumbing and scheduler integration remain open. No GPS approval or full-module credit from original ACL alone.


### 2026-10-12 — Metadata original compressed input ceiling

V30 original metadata admission now enforces the metadata worker 2 GiB compressed input ceiling independently of caller max_bytes, before object download or local file access. Added oversized evidence/no-download and oversized source/no-open unit tests.

Tests NOT RUN per implementation-first plan. This closes a resource admission mismatch only; normalized source publication, authenticated metadata admission and scheduler installation remain open. No full-module completion credit.


### 2026-10-12 — API-scoped original metadata coordinator

V30 added API-scoped prepare_authorized_metadata_original composition: owns VersionBinding, checks exact SQL-authorized version and WorkingImage ref/SHA before DB/object IO, composes live original reader with bounded extraction/freeze and returns scope-bound immutable preparation. Authored actual JPEG composition, version/source/SHA mismatch and live revocation tests.

Tests NOT RUN. Caller still verifies WorkingImage/Canvas and authenticated explicit metadata approval. Normalized source SQL reservation/publication/completion and admission/scheduler wiring remain open; returned preparation is not a ready artifact. No full-module completion credit.


### 2026-10-12 — Metadata source publication envelope

V30 added Compile/RestoreMetadataSourcePublication: owned canonical envelope pins authenticated tenant/project/actor/session, exact VersionBinding, original ref/bytes/MIME, normalized metadata source and independently compiled frozen policy/explicit allowlist. Enforces original/source alias refusal and 2 GiB original ceiling. Restore recompiles against external current authority; getters return owned copies. Authored scope/policy/approval/body/hash substitution, ownership and resource/alias refusal unit tests.

Tests NOT RUN per implementation-first plan. Publication envelope compiler is not SQL authorization or artifact readiness. SQL source reservation/completion, live source approval/lineage validation, API admission and scheduler integration remain open. No full-module completion credit.


### 2026-10-12 — SQL metadata source publication value validation

V30 authored01695 private SQL metadata source publication validator: external principal/version/original scope, original/source alias refusal,2 GiB/MIME bounds, source/frozen SHA and existing frozen value validation, external policy/approval and complete envelope equality. No runtime role receives EXECUTE. Authored shared metadata corpus positive SQL values, actor/alias/oversize/MIME/extra-field/hash/approval refusal and ACL tests.

Tests NOT RUN;01695 NOT APPLIED. SQL validator checks values only and does not establish original extraction lineage, principal consent, canonical outer envelope bytes or artifact readiness. Go restore independently checks exact canonical bytes. Source reservation/completion/live authorization/API admission and scheduler integration remain open. No full-module completion credit.


### 2026-10-12 — Live metadata source original authority

V30 authored01696 API-only live metadata source authority: externally supplied tenant/project/actor/session must match envelope and tenant context; private value validator runs before current original ACL resolution; exact original ref/SHA/bytes/MIME rechecked. Added Go AuthorizeMetadataSourcePublication with external canonical restoration, cancellation and no object IO. Authored API-only SQL ACL/missing scope and Go foreign actor/revocation/pre/post SQL cancellation tests.

Tests NOT RUN;01696 NOT APPLIED. Positive live original authority and transactional revocation/concurrency remain unqualified. Authority does not establish trusted extraction/Canvas bytes/explicit consent or reserve/publish artifacts; caller obligations remain. Source reservation/completion and API/scheduler integration remain open. No full-module completion credit.


### 2026-10-12 — Normalized metadata source SQL publication lifecycle

V30 authored01697 API-only normalized metadata source reservation/completion on backend_documents. Draft unsealed uncancelled delivery v2.2 run must belong to authenticated actor/project; repeat live original authority, exact pinned envelope/source SHA/bytes, stable source artifact ID, rotating reservation token, quota reserve/commit, immutable object version/key evidence and ready replay checks. Added immutable source proof columns/trigger, existing document lifecycle reuse and API-only ACL/missing-authority tests.

Tests NOT RUN;01697 NOT APPLIED. Positive reservation/object completion/replay/quota/concurrent revocation/GC and lock-order acceptance remain unqualified. Trusted extraction and explicit approval remain API caller obligations. Go source repository/publisher, draft admission integration, frozen source consumption and scheduler installation remain open. No public metadata mode or full-module completion credit.


### 2026-10-12 — Metadata source immutable publisher

V30 added NewMetadataSourceRepository/PublishMetadataSource backed by01697, private API-only tenant query allowlist with short committed transactions and immutable artifactjson object publication. Exact external envelope restore, source SHA/size, reservation outcome/ID/token, object key/version/MIME evidence and cancellation enforced. Dedicated source purpose admitted for key construction without lease. Authored successful publisher/replayed ready(no second PUT), quota/conflict/stale, foreign scope, substituted object and cancellation unit tests.

Tests NOT RUN. SQL positive lifecycle, constructor transaction/unknown commit recovery, shared artifactjson regression and object recovery remain unqualified. API original extraction/admission integration, source proof consumption and scheduler installation remain open; no public metadata mode or full-module completion credit.


### 2026-10-12 — Published metadata source consumption gate

V30 authored01698 private published source gate requiring exact ready parent delivery backend_document, immutable source publication envelope, normalized bytes/SHA, external policy/approved fields, exact binding/frozen receipt, original ready artifact evidence and current tenant/project/parent workflow. Exact-definition guarded pin_v30_delivery_metadata_proof and v30_metadata_result_ready now require this source gate. Authored private ACL/no-publication refusal tests.

Tests NOT RUN;01698 NOT APPLIED. Positive source publication/consumption, retention, revocation and concurrency remain unqualified. Runtime pin/result now depend on source publication rather than any ready metadata artifact, but public modes remain disabled pending API original preparation/admission/child freeze and scheduler wiring. No full-module completion credit.


### 2026-10-12 — Scheduler published metadata source resolver

V30 authored01699 scheduler-only normalized metadata publication read by external parent/version/binding/source ref/policy/approved fields, reusing published source readiness gate without original object location. Added MetadataSourceStore with short tenant SQL calls, no-row stale mapping, bounded SHA/canonical restore, owned bytes and external approval/scope refusal. Authored SQL ACL/missing publication and Go valid/tenant/canvas/policy/approval/hash/body/no-row/cancellation tests.

Tests NOT RUN;01699 NOT APPLIED. Positive SQL publication/read and transactional recovery remain unqualified. Store is not installed in metadata scheduler; API extraction/admission and child frozen source reference remain open. No public metadata mode or full-module completion credit.


### 2026-10-12 — Published metadata stage integration

V30 DeliveryMetadataStage.StagePublished now composes scheduler source resolver with existing fenced materialization coordinator. Published request has frozen source ref/binding/policy/approval only; normalized/frozen bytes load from source publication. Validates deterministic parent/slot/child/encode predecessor/lease and encoded byte bounds before SQL; owns request bytes/fields before callbacks and independently restores frozen metadata. Authored composition/ownership, binding/source/policy/approval substitution, foreign slot/no SQL and cancellation unit tests.

Tests NOT RUN. Positive source read to full stage lifecycle/replay/concurrency remains unqualified. Scheduler discovery/call loop and API admission/child source freeze still absent; caller must supply current trusted encoded byte evidence. No public mode or full-module completion credit.


### 2026-10-12 — Metadata child configuration freeze compiler

V30 added CompileMetadataChildConfig freezing source ref, sorted approved fields, frozen metadata SHA and publication SHA into canonical child config/hash. Validates deterministic child/slot/ordinal, external project/binding, closed publication restore, exact frozen preset hash/policy and no watermark. Retains existing strip child compiler/gates. Authored frozen fields/hash and project/child/binding/preset/policy/approval/export substitution unit tests.

Tests NOT RUN. Compiler is not integrated into SQL child admission or parent preparation; positive non-strip preset compatibility and end-to-end child freeze remain unqualified. Parent/slot/source publication authority must be repeated by SQL; public metadata modes remain disabled. No full-module completion credit.


### 2026-10-12 — Frozen child metadata SQL authority

V30 authored01700 private child-source authority requiring exact eight-field frozen metadata child config, source ref, sorted external allowlist, frozen SHA and publication SHA, request child/version/policy/project identity, ready parent source publication. Exact-definition guarded metadata proof pin/result authority now consume child config source gate. Authored private ACL/missing frozen fields tests.

Tests NOT RUN;01700 NOT APPLIED. Positive complete child config/publication/proof lifecycle, backward compatibility and concurrent mutation remain unqualified. Compiler still not connected to parent prepare/SQL child admission, scheduler discovery/calls absent. Public metadata modes remain disabled; no full-module completion credit.


### 2026-10-12 — Durable metadata stage input evidence

V30 authored01701 immutable verified encode manifest columns on metadata barrier, exact-definition guarded barrier creation stores checked encoded JCS/SHA and replay requires same evidence. Added scheduler-only stage input read fenced by current barrier token/readiness, frozen child/source gate and current encode attempt/result; returns child config, encoded manifest artifact/JCS and authoritative ready raster bytes. Authored SQL ACL/missing lease tests.

Tests NOT RUN;01701 NOT APPLIED. Historical barriers not backfilled; historical recovery requires explicit handling. Positive encode evidence persistence/read/replay and concurrent lease/source retirement unqualified. Go stage input loader/scheduler loop and upstream API/child admission remain open. No public metadata mode or full-module completion credit.


### 2026-10-12 — Lease-bound metadata stage loader and coordinator

V30 added typed shared MetadataChildConfig, MetadataStageInputStore loading exact lease-fenced SQL config/manifest ref/JCS/current raster size through short tenant transactions. Closed typed config must equal normalized JSON, source pins/types/bounds and exact canonical encoded bytes validated. DeliveryMetadataStage.StageClaimed composes authoritative stage input with published-source stage. Authored valid/owned evidence, extra config/foreign child/null approval/noncanonical encoded/oversize/no-row/cancellation tests.

Tests NOT RUN. Positive claimed lease through source load/materialization/recovery/concurrency remains unqualified. StageClaimed not installed in scheduler discovery/claim loop; upstream API/parent/child admission remains open. No public mode or full-module completion credit.


### 2026-10-12 — Metadata barrier scheduler installation

V30 authored01702 bounded32-candidate cross-tenant metadata barrier discovery with existing tenant-fenced SKIP LOCKED claim and tenant-context restoration. Added MetadataNextBarrierStore and45s DeliveryMetadataJob, token-fenced5s WithoutCancel cleanup on stage failure/unknown commit. NewDeliveryExpansionJob installs metadata job into existing enabled delivery scheduler readiness/metrics loop before parent expansion. Authored idle/claim failure/success/stage failure/cleanup failure/cancel cleanup unit tests and scheduler SQL ACL tests.

Tests NOT RUN;01702 NOT APPLIED. Positive cross-tenant concurrent discovery/fairness, activation unknown commit/release and full scheduler lifecycle remain unqualified. Metadata stage now wired into enabled delivery runtime, but barriers remain unreachable through public non-strip admission until API extraction/parent/child admission integration. Metadata result physical verification/adoption dispatcher also remains to integrate. No full-module completion credit.


### 2026-10-12 — Current metadata physical adoption service

V30 authored01703 scheduler-only current metadata adoption input read guarded by complete result authority, closed barrier proof and exact output manifest/report refs. Added Python MetadataWriteProof canonical restoration independently recompiling frozen projection/Params/deterministic IDs/encoded source, short tenant SQL input load and adopt_current_delivery_metadata composition through existing physical file verification then atomic adoption. Authored plan substitution/canonical refusal and SQL ACL/missing result tests.

Tests NOT RUN;01703 NOT APPLIED. Positive SQL input/physical verification/adoption, callback cancellation/TOCTOU and replay/concurrency remain unqualified. Runtime Python bridge/candidate dispatcher and public API/parent/child admission still open. No full-module completion credit.


### 2026-10-12 — Metadata physical adoption Python runtime

V30 added single-job Python delivery_metadata_job runtime: closed canonical scope-only stdin, strict UUID/attempt/fence/timeout, explicit bounded scratch/pixel/encoded/RAM installation, TLS bounded object client and scheduler DB role check. Invokes current SQL-authorized physical verification/adoption service and emits bounded canonical success without credential-bearing failure logs. Authored job identity/canonical/extra authority refusal and resource floor tests.

Tests NOT RUN. Runtime DB/S3/native physical verification and commit/timeout recovery unqualified. Limits are conservative admission reservations, not measured Metal qualification. Go subprocess runner/candidate discovery/adoption receipt recovery and public API/child admission remain open. No full-module completion credit.


### 2026-10-12 — Metadata adoption Go subprocess runner

V30 added PythonMetadataAdopter subprocess runner: scope-only canonical stdin, absolute clean Python/worker/scratch paths, explicit allowlisted environment without inherited credentials/settings, mirrored resource floors, bounded timeout/stdout, cancellation/kill/reap via CommandContext/WaitDelay and strict canonical adopted response. Authored environment injection/undersized budget/cancellation/response/stdout overflow tests.

Tests NOT RUN. No subprocess executed. Python launch/native decode/resource peaks/timeout/unknown commit unqualified. Runner process success is not SQL adoption proof; candidate discovery and independent adoption receipt/recovery dispatcher still required. API/parent/child admission remains open. No full-module completion credit.


### 2026-10-12 — Metadata adoption discovery and durable receipt dispatcher

V30 authored01704 scheduler-only metadata adoption discovery with task cursor/current result authority and tenant context restoration; durable receipt verifies current task attempt/fence/result, physical verification/adoption inbox markers and ready delivery item/export/current raster. Added tenant receipt queue and mutex/cursor bounded MetadataAdoptionDispatcher; pre/post process SQL receipt decides success including unknown commit, false process success refused. Authored success/recovered/already/false success/failure/idle/cursor wrap and SQL ACL/missing receipt tests.

Tests NOT RUN;01704 NOT APPLIED. Positive multi-tenant queue/receipt/physical process concurrency and unknown commit remain unqualified. Dispatcher not yet enabled/configured in service; Python resource settings/deployment and upstream public API/parent/child admission remain open. No full-module completion credit.


### 2026-10-12 — Metadata adoption service runtime installation

V30 added opt-in metadata adoption runtime environment parser and SQL installation constructor; service installs independent bounded adoption loop with readiness, metrics and cancellation/join shutdown, requiring delivery scheduler enabled. Explicit Python/resource environment owned and SQL scheduler role/function owner/security/privilege checked before start. Added operator runtime document and configuration/refusal/cancellation unit tests.

Tests NOT RUN. No deployment/config environment enabled and no subprocess/DB/S3 runtime executed. Full adoption loop, shutdown/unknown commit, SQL installation and resource qualification unverified. Public original extraction/parent/child admission still open; runtime installation does not enable non-strip user flow. No full-module completion credit.


### 2026-10-12 — API metadata preparation/publication coordinator

V30 added internal API PrepareAndPublishMetadataSource composition: verified WorkingImage/original resolver, authenticated tenant/project/actor/session scope, original bounds preflight, trusted preparer, exact external publication restore and immutable source publisher. MetadataSourceIdentity deterministically pins run/version/WorkingImage/canvas/policy/sorted explicit allowlist. Authored identity approval/canvas/order/duplicate/implicit GPS and cancellation tests.

Tests NOT RUN. Trusted Python source preparation adapter and short API original resolver installation remain to connect; public CreateDelivery still rejects non-strip because request allowlist, parent preparation and SQL child admission are unfinished. API coordinator is not a public enabled flow. No full-module completion credit.


### 2026-10-12 — API original resolver installation and Python publication compiler

V30 added API NewMetadataOriginalResolver using existing full WorkingImage/Canvas verifier with short tenant-scoped original query allowlist; Go original reader now matches2 GiB ceiling. Added Python canonical source publication envelope compiler from authorized original preparation, exact version/source/original binding, normalized source/frozen hashes and external approval. Authored API query commit/foreign tenant/scheduler SQL refusal/cancel and Python envelope/canonical/scope/source/bounds/approval refusal tests.

Tests NOT RUN. Shared Go/Python envelope parity and live resolver/source preparation remain unqualified. Python source job/Go preparer runtime adapter, request allowlist and parent/child admission still open. No public metadata mode or full-module completion credit.


### 2026-10-12 — Trusted original metadata Python source job

V30 added API-only delivery_metadata_source_job: closed canonical original scope/metadata ID/policy/allowlist input, independent original size/MIME/live ACL checks before download and after extraction, bounded scratch/RAM/encoded install, exact version/SHA preparation and cross-language publication envelope output. Explicit photo_api role, bounded TLS object IO/SQL, timeout and suppressed credential-bearing errors. Authored actual JPEG fake DB/object composition with no IO inside SQL, pre-download changed-size refusal, input/alias/approval/canonical/resource tests.

Tests NOT RUN. No real process/DB/S3 extraction executed; installed image header behavior/resources and source job authority/timeout unqualified. Go preparer adapter/configuration and request/parent/child admission still open. Public modes disabled; no full-module completion credit.


### 2026-10-12 — Go trusted metadata source process adapter

V30 added PythonMetadataSourcePreparer implementing the trusted Go source preparation interface: explicit API credential/resource environment, mirrored RAM/scratch bounds, canonical closed job input, sorted consent, original size preflight, bounded subprocess deadline/output and independent external scope/metadata ID/policy/approval publication restore. Authored response authority, environment injection, undersized budget, cancellation and output overflow unit tests.

Tests NOT RUN; no build, process, DB, S3 or cross-language execution performed. Adapter is not yet installed in the public delivery path. Request consent, parent preparation inventory and SQL child admission remain open; public non-strip modes remain disabled. No whole-module completion credit.


### 2026-10-12 — Metadata source installation configuration

V30 added MetadataSourceFromEnvironment with explicit opt-in flag, absolute interpreter/worker paths, closed credentials/resource configuration, owned environment and strict 1..600 second timeout. Corrected subprocess response bound to allow worst-case six-byte JSON escaping of a bounded 1 MiB publication. Authored configuration default/flag/timeout/credentials/ownership unit tests.

Tests NOT RUN. Configuration parser does not yet wire extraction into public CreateDelivery; parent inventory, explicit request approvals and SQL child admission remain unfinished. No process or deployment validation; no whole-module completion credit.


### 2026-10-12 — Explicit delivery metadata consent contract

V30 added optional approved_fields to Go delivery Policy with omission preserving legacy strip canonical bytes. Create intent and frozen input compilation now normalize owned sorted consent, reject duplicates/unknown fields and reject approvals outside approved_exif. Explicit null consent in HTTP request is rejected; empty approved_exif grants no fields. Authored closed-list/order/ownership/no-implicit-consent tests.

Tests NOT RUN. Public non-strip modes remain unavailable. Python delivery input, SQL frozen policy validators, parent source inventory and child admission must adopt the explicit approval contract before those modes can be enabled. No whole-module completion credit.


### 2026-10-12 — Python delivery metadata consent

V30 Python DeliveryPolicy now accepts a closed unique sorted explicit approved_fields list, rejects consent outside approved_exif and null/unknown fields, and omits empty approval lists during nested serialization to match legacy Go strip JCS. Authored legacy/nested serialization, sorted owned consent and invalid-consent unit tests.

Tests NOT RUN. Python/Go cross-language delivery policy parity remains unexecuted; SQL policy validators and parent/child source admission integration remain open. Public non-strip modes remain disabled; no whole-module completion credit.


### 2026-10-12 — Parent frozen metadata consent SQL gate

V30 migration01705 adds private policy consent validation and binds metadata child source readiness to the parent frozen prepare policy. Exact closed unique approvals must match canonical parent list; absent/empty consent cannot authorize GPS. Replacing child gate is guarded by previous SQL definition hash. Authored isolated rollback-only SQL value fixtures.

Migration NOT APPLIED; tests NOT RUN. Parent/child admission and physical pipeline remain unqualified, and public non-strip modes remain disabled. Value fixtures do not prove full parent/child runtime. No whole-module completion credit.


### 2026-10-12 — Parent metadata inventory compiler

V30 added CompileMetadataInventory: exact canonical snapshot and SHA, one externally rebound publication per version, shared principal/project/session scope, deterministic parent-scoped metadata identity and bounded canonical inventory with publication/frozen pins. Authored missing source, foreign parent/principal, consent and snapshot substitution tests.

Tests NOT RUN. Inventory compiler is not yet published or consumed by parent SQL preparation/child admission. Public non-strip modes remain disabled; no complete-module credit.


### 2026-10-12 — Restored metadata inventory and child config binding

V30 added immutable FrozenMetadataInventory restored by exact reconstruction against external snapshot, parent, scopes, publications, policy and approvals. Entry lookup binds ordinal and full version/source/canvas. Inventory-based child config compilation requires exact parent/snapshot/publication/frozen pins before delegating closed child config compiler. Authored owned bytes, foreign ordinal/canvas, noncanonical inventory and zero-value refusal tests.

Tests NOT RUN. Inventory persistence and SQL parent/child admission still need integration. These value contracts do not prove publication readiness or slot authority; public non-strip modes remain disabled. No whole-module completion credit.


### 2026-10-12 — API metadata source dependency installation

V30 added NewMetadataSourceDependencies binding verified original resolver, trusted Python preparer and immutable publisher to tenant/API pool. Installation requires photo_api role and four exact SECURITY DEFINER photo_fn_owner SQL entrypoints with EXECUTE rights; explicit process environment is copied and object-store bucket must match. Authored cancellation/missing dependency preflight tests.

Tests NOT RUN. Positive SQL installation, process and deployment remain unverified; public CreateDelivery and snapshot inventory orchestration still need integration. No whole-module completion credit.


### 2026-10-12 — Snapshot metadata preparation coordinator

V30 added PrepareMetadataInventory coordinator over trusted API publication service: whole canonical snapshot and working-image version/hash preflight before side effects, owned inputs, sequential per-version publication, authenticated scope recheck, 64 MiB retained evidence cap and exact restored final inventory only after complete success. Authored two-version completion, partial publication failure, invalid final image preflight and cancellation tests.

Tests NOT RUN. Coordinator is authored but not invoked by public CreateDelivery. Inventory persistence/parent proof/SQL child admission remain open. Existing publications may remain after failure for deterministic retry; no partial inventory is returned. No full-module completion credit.


### 2026-10-12 — Durable parent metadata inventory pin

V30 migration01706 adds immutable run-owned metadata inventory JCS/SHA and API-only pin function bound to live delivery idempotency key/request hash, exact saved frozen policy/snapshot, ready creation documents and each ready source publication/live source authority. Initial INSERT injection and later mutation are refused. Go CreateIntent.PinMetadataInventory repeats parent/snapshot/policy/consent binding before SQL. Authored preflight cancellation/zero-value tests.

Migration NOT APPLIED; tests NOT RUN. Positive SQL lifecycle fixtures and current artifact readiness/resource/lock qualification still required. Public CreateDelivery and child admission do not yet consume the pin; no full-module completion credit.


### 2026-10-12 — SQL child binding to pinned metadata inventory

V30 migration01707 binds existing metadata child source gate to parent run inventory JCS/SHA and exact ordinal/version/source/publication/frozen/policy/consent entry. Previous SQL definition hash guards replacement. Authored rollback-only SQL value fixture for parent/snapshot/ordinal/source/publication/frozen/consent substitutions.

Migration NOT APPLIED; tests NOT RUN. Value fixture does not prove positive persisted inventory lifecycle or public child admission. Parent creation/prepare proof and child draft integration remain unfinished; public non-strip modes disabled. No whole-module completion credit.


### 2026-10-12 — Lease-fenced metadata inventory entry reader

V30 migration01708 adds scheduler-only lease-fenced parent inventory entry reading with current source publication readiness. Go MetadataInventoryEntryStore rebinds returned ordinal/version/snapshot/project/tenant/policy/consent and publication/frozen pins before child config compilation; tenant short-transaction query allowlist extended. Authored exact response, snapshot/pin/entry/body substitution and missing evidence tests.

Migration NOT APPLIED; tests NOT RUN. Reader and compiler still need integration into expansion child draft flow; parent prepare/admission remain strip-gated. No public enabled mode or complete-module credit.


### 2026-10-12 — Child pipeline metadata inventory config wiring

V30 ExportRequest now matches metadata mode to validated frozen parent policy instead of hardcoding strip. PreparePlan exposes owned policy/consent; legacy ChildConfig explicitly refuses non-strip. FrozenDeliveryInitialTask can bind lease-read metadata config; quota calculation and draft use that exact owned config. DeliveryChildPipeline invokes the inventory reader for non-strip before child admission. Authored zero/unbound config refusal test.

Tests NOT RUN. Initial/final encode mapping and SQL child draft/cursor/recovery still contain strip-only contracts; public metadata mode remains disabled. Wiring does not establish a runnable end-to-end non-strip path. No whole-module completion credit.


### 2026-10-12 — Clean intermediate encoding for metadata delivery

V30 initial and final encode compilers now map delivery copyright/approved_exif to strip only for the intermediate encoder params, preserving immutable request/preset policy for separate authorized metadata.write. Delivery export fixture supports frozen metadata modes. Authored copyright/approved_exif request preservation, initial/final strip encoding and unbound child config refusal tests.

Tests NOT RUN. SQL encode source/child draft/recovery/cursor contracts still require corresponding non-strip parent integration and metadata barrier opening from encode adoption. Public metadata modes remain disabled; no full-module completion credit.


### 2026-10-12 — Metadata-aware SQL child draft/config policy

V30 migration01709 extends shared SQL child config constructor used by draft/activation/cursor/recovery to require exact pinned parent inventory and ready normalized source for non-strip, with batch queue and frozen/publication/consent fields. Initial encoder SQL expects strip intermediate; export rows/cursor retain requested metadata mode. Exact replacement occurrence guards preserve existing quota/shell fences. Authored isolated constructor strip compatibility and missing-parent metadata refusal fixture.

Migration NOT APPLIED; tests NOT RUN. Full positive parent/source/child lifecycle fixture remains required. Parent prepare source readiness still blocks non-strip, and encode adoption metadata barrier integration remains open. No end-to-end or whole-module completion credit.


### 2026-10-12 — Physical encode adoption to metadata barrier handoff

V30 migration01710 preserves the encode adoption ABI while dispatching strip to private legacy adoption and copyright/approved_exif to begin_v30_metadata_barrier. Metadata intermediates no longer take the final-export adoption path. Python encode commit sets transaction-local tenant scope and installation schema requires barrier ABI. Authored exact SQL handoff args/order and scope/refusal unit tests.

Migration NOT APPLIED; tests NOT RUN. Parent metadata preparation/admission still disabled. Full positive encode-to-metadata lifecycle, retry after unknown commit and scheduler runtime qualification remain required; no whole-module completion credit.


### 2026-10-12 — Parent prepare metadata inventory source gate

V30 migration01711 allows metadata modes in base parent prepare source validation only with a pinned complete inventory matching exact snapshot, parent policy/consent, scoped publication/frozen pins and current ready normalized artifacts. Existing tenant/resource wrappers remain intact. Go catalog now requires preset mode equal requested mode. Authored missing persisted inventory SQL refusal fixture.

Migration NOT APPLIED; tests NOT RUN. Positive parent inventory lifecycle, project/original retention qualification and API orchestration remain incomplete; FreezeCreateIntent public non-strip gate remains in place. No whole-module completion credit.


### 2026-10-12 — API metadata preparation and inventory pin orchestration

V30 API creation now accepts metadata intents as validated immutable values but requires explicit configured preparer before execution. StageCreatePrepare composes trusted source installation, per-version WorkingImage catalog resolution, full snapshot source publication and authenticated inventory pin after creation documents and before prepare materialization. Transport/PrepareDelivery propagate optional preparer while preserving strip calls. Authored consent sorting/ownership/order-stable hash/change-sensitive hash tests.

Tests NOT RUN; no process/SQL/object execution. Server buildDeliveryCreate does not yet install metadata preparer, so public deployment remains disabled by missing dependency. Full parent/child lifecycle, source-ready SQL chain and retry/session behavior remain unqualified. No whole-module completion credit.


### 2026-10-12 — Metadata source API startup and client consent contract

V30 API buildDeliveryCreate now parses explicit metadata source opt-in, rejects enabled extraction without delivery scheduler, checks matching bucket and exact API SQL installation and passes preparer to creation dependencies. DeliveryPolicy JSON Schema and photographer request type/freezer now support closed sorted owned explicit consent, preserve empty legacy omission and reject invalid consent. Authored startup invalid-config and TypeScript consent ownership/refusal tests.

Tests/build/schema validators NOT RUN. No flags enabled or deployment executed. UI consent controls, full cross-language/schema parity, positive SQL/process lifecycle and restart/unknown-commit qualification remain outstanding. No whole-module completion credit.


### 2026-10-12 — Explicit metadata consent creation UI

V30 photographer delivery creation UI now offers strip/copyright/approved_exif policy and seven explicit EXIF consent checkboxes with GPS unchecked. Policy change clears presets/consent; incompatible preset modes cannot be selected. Journal recovery restores policy/consent and immutable retry locks controls. Added EN/RU/BG labels, preset config metadata union and DOM tests for explicit GPS/retry and policy reset.

DOM/unit/typecheck/build NOT RUN. No rendered UI validation or accessibility qualification. Runtime capability display and preset editing support still need review; full SQL/worker/API lifecycle remains unverified. No whole-module completion credit.


### 2026-10-12 — Metadata delivery preset authoring and worker validation

V30 mutable Go export preset validation and Python frozen preset contract now admit strip/copyright/approved_exif while EncodeParams remains strip-only. Python prepare checks preset metadata mode equals frozen delivery policy. Export preset creation UI exposes localized metadata mode, marks delivery-only use and keeps legacy download invocation from attempting non-strip. Authored closed-mode Go/Python preset tests.

Tests/build/DOM checks NOT RUN. Export screen full scope/race behavior and positive metadata delivery lifecycle remain unqualified. UI change adds preset creation options; it does not establish full preset editing parity. No whole-module completion credit.


### 2026-10-12 — Direct and continuation encode parameter source alignment

V30 migration01712 aligns both initial parameter publication and post-resize/sharpen continuation SQL scopes with Go clean intermediate encoding: metadata_policy is strip in encoder params while the request retains its delivery policy. Exact replacement occurrence guards preserve existing source/lease/quota checks. Authored isolated SQL installation regression for both scopes and exact parameter comparison.

Migration NOT APPLIED; tests/build NOT RUN. Installation assertions do not prove positive metadata delivery lifecycle. Recovery, unknown-commit/session behavior and broader V20–V32 requirements remain open; no whole-module completion credit.


### 2026-10-12 — Parent metadata live original/project authorization

V30 migration01713 wraps the retained complete inventory validator with tenant context, workflow project/actor binding and live original authorization for every publication before prepare admission. Existing exact inventory/publication/artifact predicates remain intact. Authored tenant-context refusal SQL test.

Migration NOT APPLIED; tests/build NOT RUN. Positive project/source-retention/session lifecycle fixtures remain required. No full-module completion credit.


### 2026-10-12 — Metadata barrier replay raster evidence

V30 migration01714 binds existing-open-barrier replay to exact raster identity/SHA/bytes and ready/verified/versioned/unexpired artifact state. Authored SQL installation assertions; these do not prove positive unknown-commit lifecycle.

Migration NOT APPLIED; tests/build NOT RUN. Completed-barrier replay and API session retry remain open. No full-module completion credit.


### 2026-10-12 — Metadata repository deterministic identity boundary

Metadata source repository construction and each publication operation now require the source ID derived from parent run, exact binding, policy and approvals. Updated positive publication/replay fixture and authored foreign-run refusal before SQL/object IO.

Tests/build NOT RUN. Session rotation across a partially published immutable inventory remains an open design/runtime integration obligation; no full-module completion credit.


### 2026-10-12 — Scheduler metadata inventory identity reconstruction

MetadataInventoryEntryStore now independently derives source identity from parent run/binding/policy/consent and rejects foreign IDs even when entry, publication and SHA pins are mutually consistent. Entry decoding rejects unknown fields and trailing values. Updated positive fixture; authored self-consistent foreign source and unknown-field substitutions.

Tests/build NOT RUN; SQL positive lifecycle and session rotation remain open. No full-module completion credit.


### 2026-10-12 — Dedicated metadata extraction database credentials

API runtime supports PHOTO_DELIVERY_METADATA_SOURCE_DATABASE_URL overriding the general DATABASE_URL only for the explicitly installed extraction subprocess. The parent-only setting is mapped to the existing child allowlist; no extra ambient variables are inherited. Authored override, dedicated-only, invalid-value and disabled-mode tests; operator contract updated.

Tests/process/build NOT RUN. Python still forces and verifies photo_api; connectivity and privilege qualification remain outstanding. No full-module completion credit.


### 2026-10-12 — Shared metadata extraction concurrency bound

All API preparer instances now share one extraction permit per process. Queue waiting uses the extraction deadline and request cancellation; permit release covers process failure/cancellation and is idempotent. Authored cancellation, reuse and handoff unit tests. Runtime docs distinguish per-process concurrency from cluster-wide and OS resource admission.

Tests/build/process NOT RUN. Multi-replica admission and full lifecycle remain unqualified; no whole-module completion credit.


### 2026-10-12 — Revision-fenced preset metadata policy API

Added editor-authorized PATCH /api/v1/tenants/{t}/export-presets/{preset}/metadata-policy and OwnerAPI method. Service locks the tenant-owned active preset, checks exact decimal revision, uses idempotency bound to preset/change and updates only metadata policy in raw config while preserving other processing fields. Recomputes canonical hash and increments revision; already frozen delivery configs are not mutated. Authored request validation tests.

Tests/build NOT RUN. Mounted preset editing UI, contract catalogue updates and positive concurrent/idempotent SQL fixtures remain outstanding; endpoint is authored, not qualified. No full-module completion credit.


### 2026-10-12 — Mounted preset metadata policy editor

Exports screen mounts a localized three-mode preset metadata editor backed by revision-fenced API. Unknown response retry retains policy/key and locks edits; terminal access/revision errors require closing/reloading. Unmount aborts requests and ignores late results. Preset list loading now aborts/ignores old API scope responses. Authored retry and unmount DOM tests.

Tests/typecheck/build/DOM NOT RUN. Endpoint contract catalogue and positive SQL fixtures remain outstanding; editing covers metadata policy only, not all preset parameters. No full-module completion credit.


### 2026-10-12 — Preset metadata API machine and inventory contracts

Added closed request/response schema, OpenAPI operation, module inventory entrypoint/component states and explicit service/client editing contract. New endpoint is not included in historical 101-entrypoint recount; whole-product completion remains unproven.

Validators/tests/build NOT RUN. Positive SQL/race/replay/access fixtures remain open; no full-module completion credit.


### 2026-10-12 — Preset processing preservation and SQL replay fixtures

Extracted metadata policy config compiler preserving all raw non-policy fields; authored preservation/canonical-hash/invalid-config unit cases. Added opt-in isolated SQL service fixture for create, update, exact-key replay, stale revision refusal and persisted revision/hash/quality/size/sharpen preservation.

Tests/build NOT RUN; fixture is authored evidence only. Concurrent edits, cross-tenant denial and full V30 lifecycle remain outstanding.


### 2026-10-12 — Preset edit race, tenant denial and conflict reload

Expanded isolated SQL fixture with two concurrent revision-2 edits (one winner), original same-key replay after later edit, changed-payload key refusal and foreign tenant denial without revision mutation. Added mounted reload action clearing stale selection and refetching presets; authored conflict DOM test prohibiting automatic retry.

Tests/build/DOM NOT RUN. These authored fixtures do not establish current runtime correctness; full V30 lifecycle and session recovery remain open.


### 2026-10-12 — Frozen delivery preset mutation regression

Inspected pending recovery and prepare source predicates: recovery uses saved catalog configs and live source/recipe eligibility, not current mutable preset contents. Authored strip/copyright/approved_exif regression freezing pending config/hash, mutating same-ID catalog policy/revision/hash and proving reconstructed publication retains old config while incompatible new catalog is refused.

Tests/build NOT RUN. This value fixture does not prove persisted API/session retry or full metadata lifecycle. No full-module completion credit.


### 2026-10-12 — Immutable metadata provenance/current session separation

Migration01715 adds private source reauthorization helper: validates unchanged publication under its frozen scope, binds external tenant/project/actor and resolves exact original identity/bytes/MIME through the current live session. No public EXECUTE grants or existing authority paths changed. Authored missing-source refusal SQL fixture.

Migration NOT APPLIED; tests/build NOT RUN. Helper is not yet wired into source replay/inventory pin/parent prepare; session rotation remains unfinished. Positive rotated-session and revoked-access fixtures remain required.


### 2026-10-12 — Current session authority wiring without evidence mutation

Migration01716 routes source publication scope and parent inventory live checks through private reauthorization helper; retained inventory validator checks presence of immutable origin session instead of equating it to current session. Exact occurrence guards preserve other predicates. MetadataSourceRepository.WithCurrentSession supplies current SQL authority while retaining scope/body/SHA; authored ownership/session test.

Migration NOT APPLIED; tests/build NOT RUN. API still needs saved-publication reading/reuse and mixed-session inventory handling before rotated-session retry is complete. No full-module completion credit.


### 2026-10-12 — API saved metadata publication retry reader

Migration01717 adds API-only draft-run retry reader for pending/ready normalized publications under current editor/project/source authority. Go reader distinguishes absent publication from stale authority, derives source ID and rebinds all freshly resolved original scope fields except immutable origin session. No original object locations returned.

Migration NOT APPLIED; tests/build NOT RUN. Reader not yet invoked by API extraction coordinator; reuse, mixed-session inventory and positive lifecycle remain outstanding.


### 2026-10-12 — API metadata publication reuse and mixed origin sessions

API source coordinator now resolves current original authority, reads saved pending/ready publication before extraction, reuses immutable bytes and publishes under current SQL session. Startup checks retry reader ABI. Inventory supports different immutable extraction sessions across partial retries while retaining common tenant/project/actor and exact bindings. Authored rotated current-session restore, original/parent substitution, absence and stale-authority reader tests.

Tests/build/migrations NOT RUN. Positive persisted partial-publication/session-rotation lifecycle and concurrent retry remain unqualified; no full-module completion credit.


### 2026-10-12 — Metadata retry isolation installation and mixed session fixture

Metadata startup and per-request dependency installation now refuse PUBLIC EXECUTE or effective scheduler/worker/gateway/portal access to the six API metadata entrypoints. Extended coordinator value fixture with successful mixed immutable origin sessions and refusal of foreign actor publication. Runtime installation/retry contract updated.

Tests/build/SQL NOT RUN; positive installation and persisted session-rotation lifecycle remain open. No full-module completion credit.


### 2026-10-12 — Metadata prepare session recovery evidence boundaries

Inspected existing01301 proof recovery: only auth_session_id may change before activation under current editor authorization; sealed/attempted work is refused. Extended three-mode pending regression to verify rotated-session reconstruction preserves frozen input/policy/params and changes only proof session. Authored SQL role isolation fixture for API retry reader and private reauthorization helper.

Tests/build/SQL NOT RUN. These tests are not persisted end-to-end session rotation proof; full metadata delivery lifecycle remains outstanding.


### 2026-10-12 — Delivery watermark parameter geometry foundation

Added closed derivative.watermark Params compiler mapping preset pixel margins to normalized canvas margins, fixed linear_alpha_v1/max-width policy and Python-compatible ties-to-even resize geometry. Rejects overflow/tall marks, invalid refs/modes/nonfinite opacity and oversized canvases. Authored parameter/geometry/refusal tests.

Tests/build NOT RUN. Watermark source authorization, alpha/color verification, inventory/graph/SQL/stage adoption and publication integration remain open; public watermark gates retained. No full-module completion credit.


### 2026-10-12 — Watermark canonical WorkingImage value binding

Added watermark input compiler binding independently resolved asset ID to preset, exact canonical image/mark document hashes, matching working gamma24 color/ICC, float32 and required mark alpha before geometry compilation. Authored exact, missing alpha, ICC/dtype/asset/hash and noncanonical substitution cases.

Tests/build NOT RUN. Value checks do not prove physical arrays or live ACL; SQL source resolver, graph/resources/adoption remain open and public watermark gates stay closed.


### 2026-10-12 — Explicit watermark asset/version source authority

Migration01718 and API resolver bind an explicit watermark asset/version to accepted tenant/project WorkingImage master, ready versioned document and existing draft artifact ACL. No latest-version fallback. After the short binding transaction, SQLArtifactCatalog repeats provenance/readiness checks; physical image/alpha verification remains separate. Authored missing version/dependency/cancellation tests.

Migration NOT APPLIED; tests/build NOT RUN. Source physical verification, frozen inventory and graph/adoption wiring remain open; watermark gates retained.


### 2026-10-12 — Watermark manifest verification and array catalog evidence

Added LoadWatermarkSourceEvidence: rereads exact catalog-authorized versioned manifest through existing bounded SHA/WorkingImage verifier, requires alpha/gamma24, computes binding/canvas hash and resolves RGB/alpha separately under parent/binding authority. Rejects cross-contract aliases and array descriptor size/version/hash mismatch without loading arrays into API RAM. Authored preflight tests.

Tests/build NOT RUN. Only manifest bytes are physically verified here; worker NPY verification, positive loader tests, frozen inventory and graph/adoption remain open. Watermark gates retained.


### 2026-10-12 — Watermark loader positive and uint16 alpha compatibility

Watermark loader now checks binary MIME and versioned manifest descriptors and retains existing worker support for uint16 as well as float32 alpha. Added positive canonical manifest/catalog fixture and MIME/size/version/tampering substitutions, including no array lookup after invalid manifest.

Tests/build NOT RUN. API reads only manifest bytes; actual NPY type/shape/values remain worker verification. Frozen inventory/graph/adoption integration remains open.


### 2026-10-12 — Immutable watermark source pin compiler

Added owned FrozenWatermarkSource compiler/restore binding external tenant/project/actor/parent/preset/config SHA to exact asset/version, canonical manifest/canvas and RGB/alpha refs/byte counts. Reconstructs source evidence and rejects aliases or inconsistent inventory; bounded2MiB. Does not claim NPY physical or live SQL authority.

Tests/build NOT RUN. Positive/substitution unit fixtures, durable pin lifecycle, parent/child graph/resources/adoption remain open. Public watermark gates retained.


### 2026-10-12 — Pinned watermark Params and substitution fixtures

FrozenWatermarkSource now compiles Params only from its owned pinned manifest under exact external scope/config SHA. Added positive pin/restore/Params test, buffer ownership and zero/foreign scope refusal; config/asset/version/canvas/array/order/size/manifest substitutions covered.

Tests/build NOT RUN. Durable pin storage, current SQL authority, worker array verification and full graph/adoption remain open. No full-module completion credit.


### 2026-10-12 — Durable run-owned watermark source pins

Migration01719 adds bounded append-only per-preset run metadata (max16/32MiB), trigger preventing injected/changed existing pins and API-only pin function tied to pending idempotency/frozen preset, draft workflow, current editor and exact versioned source/array readiness. Uses existing runs retention/purge; no new table.

Migration NOT APPLIED; tests/build NOT RUN. Positive SQL fixture, Go pin wrapper, stricter SQL value/geometry parity and API/graph integration remain open. Public watermark gates retained.


### 2026-10-12 — API watermark pin persistence wrapper

CreateIntent.PinWatermarkSource checks owned pin hash, tenant/project/actor/parent and selected preset before API SQL call carrying current session plus idempotency key/request hash. Caller retains transaction ownership; SQL refusal is explicit. Authored positive ABI/current-session, scope substitutions, zero pin and cancellation tests.

Tests/build/SQL NOT RUN. SQL value parity/positive persistence fixture and public API preparation/graph wiring remain open. Public watermark gates retained.


### 2026-10-12 — Watermark pin geometry

V30 watermark pin geometry now has Go canvas resource bounds and private01720 SQL shape/array-size/alias gate wired into persistence; overflow and privileged SQL parity fixtures authored.

NOT RUN/NOT APPLIED; physical NPY, canonical canvas hash parity and full API/graph/adoption lifecycle remain open; no module completion credit.


### 2026-10-12 — Watermark Canvas authority

01721 SQL watermark geometry gate independently reconstructs canonical Canvas SHA and closes Canvas/binding fields; tamper/hash/identifier regression cases authored.

NOT RUN/NOT APPLIED; full physical NPY and API/graph/adoption lifecycle remain open.


### 2026-10-12 — Watermark pin recovery value

V30 persisted watermark pin decoder independently recompiles exact canonical value against external frozen request scope/config; recovery rejects malformed/foreign/noncanonical/oversized array pins and owns buffers.

Unit tests authored NOT RUN. Decoder grants no ACL/physical evidence; API retry reader/current source reauthorization and graph lifecycle remain open.


### 2026-10-12 — Watermark retry authority

01722 API-only watermark retry reader binds pending idempotency request, selected frozen preset, live draft/project/editor and reuses exact pin source authorization; Go wrapper distinguishes saved/absent/stale and recompiles external scope. Unit ABI/outcome/refusal fixtures authored.

NOT RUN/NOT APPLIED. Runtime composition/startup checks, physical arrays and export graph/adoption remain open.


### 2026-10-12 — Watermark recovery source reauthorization

Watermark pin service recovery now decodes frozen value, resolves exact saved asset/version under current session, physically reloads manifest and independently reauthorizes array descriptors before exact pin restore; physical loader canvas bounds added.

Preflight/cancellation/substitution tests authored NOT RUN. Positive persisted lifecycle, NPY worker verification, API composition and export graph remain open.


### 2026-10-12 — Watermark service recovery composition

V30 RecoverWatermarkPin composes authenticated API transaction retry read with exact-version physical manifest/current array descriptor reauthorization outside transaction; frozen config owned before adapters. No fallback on expired or invalid pin.

Preflight tests authored NOT RUN. First-source version pin selection, startup installation, public creation and export graph remain open.


### 2026-10-12 — Durable watermark version selection

01723 bounded immutable per-run watermark version selections freeze latest accepted version once under pending request/frozen preset/current editor; exact source identity is reauthorized on retry without fallback. Go typed selection wrapper and ABI/refusal tests authored.

NOT RUN/NOT APPLIED. Pin-to-selection SQL gate, service initial preparation, startup checks, positive concurrency lifecycle and export graph remain open.


### 2026-10-12 — Watermark source preparation

01724 pin persistence requires exact previously frozen watermark selection. PrepareWatermarkPin composes saved recovery or committed version selection, current exact descriptor/manifest/arrays, canonical pin compilation and API persistence with object IO outside transactions.

Preflight tests authored NOT RUN;01724 NOT APPLIED. Positive DB/retry/race lifecycle, startup installation, public creation and export graph remain open.


### 2026-10-12 — Whole-preset watermark preparation

V30 sequential whole-preset watermark inventory preparation validates all frozen configs/selection order before effects, owns config bytes, independently recompiles returned pins and bounds retained evidence to32MiB; no partial inventory on failure.

Complete/invalid-later/partial-failure/foreign-pin tests authored NOT RUN. Public create integration and export graph still open.


### 2026-10-12 — Watermark worker input compilation

V30 watermark stage compiler derives exact worker ordered image/RGB/optional-alpha plus pinned watermark/RGB/alpha roots, canonical Params and resource geometry; duplicate artifact identity refused across both sources.

Worker source inspected; input order/alpha/resource/alias tests authored NOT RUN. Graph task/resource/SQL/adoption/public creation integration still open.


### 2026-10-12 — Watermark workflow resources

Workflow watermark node compiler derives deterministic V2 task identity and CPU profile from validated pinned stage; bounded both-image RAM/scratch/output estimates include alpha and VRAM0, with exact-limit/undersized/overflow unit fixtures.

NOT RUN. Estimates are conservative policy, not measured hardware qualification. Closure/SQL/adoption/full graph/public create remain open.


### 2026-10-12 — Watermark frozen task closure

Dedicated watermark closure requires both exact WorkingImage roots in resolved and direct inventory while ordinary closure ABI remains restricted. Frozen watermark task binds real image/mark arrays, recipe/preset, canonical Params, deterministic task and CPU profile with existing transport publisher.

Closure substitution tests authored NOT RUN. SQL stage/admission/predecessor/adoption and public creation remain open.


### 2026-10-12 — Watermark result value verification

Watermark result validator reconstructs pinned stage and exact inputs/Params, requires unchanged canvas/color/dtype/transform chain/alpha presence, fresh nonalias output refs and exact bounded worker report including zero-opacity semantics.

Positive/zero-opacity/color/canvas/alias/report substitution matrix authored NOT RUN. Physical pixels and producer/fence SQL adoption remain open.


### 2026-10-12 — Watermark physical output verification

Watermark output now uses shared bounded streaming NPY physical verifier after independently reconstructed pin/Params/metadata/report checks. Shared stage verifier retains resize/sharpen behavior and adds canvas resource/final cancellation guard.

Actual RGB NPY positive/hash-corrupt/NaN/wrong-shape fixtures authored NOT RUN; alpha physical helper reused. SQL producer/fence adoption and graph activation remain open.


### 2026-10-12 — Watermark physical input arrays

Initial and recovered watermark source preparation now streams physical RGB/alpha NPY verification before pin acceptance, preserving float32 RGB and float32/uint16 alpha. Both descriptors preflight before IO; bounded hash/shape/finiteness/unit alpha checks reuse shared verifier.

Actual float32/uint16/hash corruption/NaN/out-of-range alpha/preflight fixtures authored NOT RUN. SQL producer/adoption and public graph remain open.


### 2026-10-12 — Running watermark source authority

Private01725 current pinned watermark source readiness works for draft/running delivery parents without pending-ledger dependency; rechecks current actor/session/project, config hash, pin scope/hash/canvas/selection and exact ready versioned manifest/arrays with existing artifact ACL.

NOT APPLIED/NOT RUN; helper not yet wired into scheduler admission/producer/adoption. Positive SQL/concurrency fixtures and full graph remain open.


### 2026-10-12 — Fenced scheduler watermark pin

01726 scheduler-only watermark pin read requires live parent expansion fence, prepare proof selected preset/config and private current source readiness; typed store restores external scope/config using short tenant transaction allowlist.

ABI/saved/foreign-scope/stale-fence/preflight unit fixtures authored NOT RUN;01726 NOT APPLIED. Startup checks, SQL integration and task lifecycle remain open.


### 2026-10-12 — Watermark API installation checks

Watermark API recovery/preparation now checks exact eight public/private SQL ABIs, fn owner, effective API permission, SECURITY DEFINER for API functions and absence of PUBLIC/forbidden service role execution before tenant transactions/object IO.

Installation acceptance/refusal/DB-error/cancellation unit tests authored NOT RUN. Scheduler startup and positive installed lifecycle/task admission remain open.


### 2026-10-12 — Watermark producer discovery

01727 dedicated watermark producer discovery derives existing strict task/upload/attempt/fence/output checks, restricts operation to watermark and adds current fenced source pin gate. Typed scheduler reader validates deterministic child task and producer envelope.

NOT APPLIED/NOT RUN. Existing child request still refuses watermark until full admission/continuation support; discovery alone enables no task. SQL installation and full lifecycle fixtures remain open.


### 2026-10-12 — Verified watermark loader

Verified watermark loader composes frozen scope/pin restore, deterministic node, fenced producer, physical NPY verification and repeated pin/producer current reads; output/producer evidence accessors own buffers.

Positive physical loader, revoked-pin-after-IO, changed-fence and output ownership scenarios authored NOT RUN. Final SQL read-to-commit fence/adoption and graph activation remain open.


### 2026-10-12 — Watermark verification receipt

01728 dedicated watermark physical-verification receipt reuses exact producer/attempt/fence/output byte attestations with distinct consumer marker. Opaque verified loader evidence records in caller transaction after scope/config/pin/deterministic-task checks; no adoption permission emitted.

Positive loader-to-receipt/refusal/foreign-scope fixtures authored NOT RUN;01728 NOT APPLIED. Source locking, predecessor authority, adoption and continuation graph remain open.


### 2026-10-12 — Watermark source receipt locks

01729 watermark verification receipt acquires NOWAIT SHARE source locks on tenant/parent/workflow/project/expansion, selected asset/version/master and sorted binary artifacts, then repeats fenced pin readiness before producer receipt. Private helper ACL fixture extended.

NOT RUN/NOT APPLIED. Lock privileges/contention/positive lifecycle need qualification; session/membership/ACL race coverage, predecessor adoption and continuation remain open. No full race-closure claim.


### 2026-10-12 — Watermark receipt authorization locks

01730 watermark receipt source lock also locks frozen prepare authority document, current actor/session/membership and matching project ACL under NOWAIT; current realtime session expiry is checked before repeated source authorization. Existing fn-owner privileges reused; no new grants.

NOT APPLIED/NOT RUN. Positive revoke/downgrade/ACL contention and expiry-at-final-commit qualification remain open; adoption/continuation still incomplete.


### 2026-10-12 — Encode after verified watermark

Opaque verified watermark evidence now compiles deterministic export.encode consuming exact watermarked manifest, clears already executed resize/sharpen/watermark and keeps clean strip intermediate for separate metadata.write; frozen config retained unchanged.

Loader-to-encode positive/foreign-child/no-repeat/metadata-strip scenarios authored NOT RUN. Durable watermark adoption and encoder SQL admission remain open.


### 2026-10-12 — Frozen watermark encoder continuation

Verified watermark loader owns predecessor source bytes/ref. Frozen encoder continuation retains previous image/arrays, exact mark roots, watermark output/report, original binding, recipe and frozen preset; task/profile/params closure and predecessor/stage exposed without activation API.

Positive freeze/deterministic identity/stage/missing-source inventory scenarios authored NOT RUN. Full transitive authority, SQL adoption/admission and runtime graph remain open.


### 2026-10-12 — Watermark durable encode barrier

01731 dedicated watermark-to-encode durable barrier records current locked verification and opens exactly one encode expansion with deterministic identity; replay repeats receipt/current producer, conflicting open child expansions refused. Opaque evidence exposes caller-transaction begin wrapper.

Go receipt-to-barrier ABI scenario authored NOT RUN;01731 NOT APPLIED. Barrier claim/materialization/transport/adoption/activation and positive persisted retry remain open.


### 2026-10-12 — Watermark encoder Params publication scope

01732 private watermark encoder Params scope requires current locked pin, exact durable barrier, verified watermark producer and exact frozen encoder fields with strip intermediate. Dedicated reserve/complete functions preserve existing immutable document/quota/retry protocol without extending ordinary scope.

NOT APPLIED/NOT RUN. SQL positive Params/unknown-commit/quota fixtures and Go publication adapter, task/resource/admission lifecycle remain open.


### 2026-10-12 — Watermark encoder Params publisher

Verified watermark evidence publishes encoder Params through existing immutable publisher/repository with closed reserve/complete routing to01732 functions. Each call sets tenant in short bounded transaction and commits before object IO; arbitrary SQL/foreign scope refused.

Query mapping/commit-before-return/foreign-scope tests authored NOT RUN. Positive object PUT/unknown-commit SQL recovery and task admission lifecycle remain open.


### 2026-10-12 — Watermark encoder blocked task

01733 dedicated blocked watermark encoder creation requires current locked Params/source/barrier/producer receipt, exact global stage and predecessor, published Params and independently reconstructed CPU RAM/scratch/output profile from exact producer manifest; immutable task replay and tenant/run quota reservation retained. Owned frozen Go descriptor exposes caller-transaction create with exact request/tenant identity; positive/refusal/foreign-child unit scenarios authored.

NOT RUN/NOT APPLIED. SQL positive quota/replay/concurrency fixtures, transport/input registration/adoption/activation, scheduler composition and public creation remain open. No full-module completion credit.


### 2026-10-12 — Watermark encoder transport

01734 dedicated watermark encode transport receipt requires current locked Params/pin/verified producer, exact one-stage barrier and global stage, blocked descriptor/profile, complete V2 closure and succeeded predecessor edge. Frozen Go task registers owned closure through shared transactional registrar with dedicated receipt and idempotent create; receipt positive/absent/foreign-slot and cancelled/empty/nil transaction scenarios authored.

NOT RUN/NOT APPLIED. Positive full registration/unknown-commit and SQL lifecycle fixtures, adoption/activation, production scheduler composition and public creation remain open.


### 2026-10-12 — Watermark encoder activation

01735 dedicated watermark encoder activation atomically checks registered transport/current locked source, closes one-task barrier, seals graph, assigns export task, activates encoder and writes watermark v2-adopt marker. Dedicated replay receipt validates closed barrier, deterministic predecessor/descriptor/Params/closure/edge/export ownership under current pin authorization. Go wrapper and first-dispatch/replay/refusal/foreign-slot scenarios authored; shared DB fake corrected to accept UUID predecessor ABI.

NOT RUN/NOT APPLIED. Positive installed SQL lifecycle, rollback/race/unknown-commit fixtures, scheduler composition, first watermark admission and public create still open. No module completion credit.


### 2026-10-12 — Watermark encode coordinator

Dedicated watermark encode admission coordinator resumes durable activation or registered transport before publication, creates blocked descriptor before object IO and atomically registers/activates in final tenant-scoped transaction. All SQL probes use short30s transactions with tenant context and cancellation-independent cleanup; object IO never holds SQL locks. Activation receipt exposed as owned frozen-task reader. Phase/recovery/failure and transaction commit/rollback unit scenarios authored.

NOT RUN. Full persisted SQL/transport/unknown-commit fixtures, scheduler composition, initial watermark admission and public delivery create remain open.


### 2026-10-12 — Watermark presentation prefix

Watermark prefix compiler replays required resize/sharpen results using existing strict validators before compiling watermark on actual output; pending prefix yields next real derivative without inventing future refs. Deterministic child/preset/pin/source identity and resource limits enforced. Existing ordinary presentation wrappers retain closed ExportRequest gate and delegate unchanged value logic to private request helpers. Four preset prefix variants plus source/child/preset/result substitutions and ownership unit scenarios authored.

NOT RUN. Positive completed resize/sharpen prefix fixtures, dedicated SQL admission/Params/barriers, scheduler and public create integration still open. Existing worker quality algorithms unchanged.


### 2026-10-12 — Watermark prepare proof and request

Dedicated watermark prepare proof/plan compilers require full unique selected frozen-preset pin inventory and exact externally supplied tenant/project/actor/parent/config identity. WatermarkExportRequest derives exact child from prepare proof and revalidates immutable pin; ordinary proof/request remain closed. Unit fixture covers dedicated proof/plan/request, missing/duplicate/foreign pins, request ownership and scope/child/source substitutions.

NOT RUN. SQL source/prepare proof gates, initial watermark admission and production API/scheduler composition remain open. These compilers prove values, not current physical/source authorization.


### 2026-10-12 — Watermark prepare source SQL

01736 prepare source gate replaces unconditional watermark rejection with exact persisted current watermark source readiness per frozen preset while retaining snapshot/recipe/project/editor/metadata gates and existing tenant wrapper/OID/ACL. SaveWatermarkPrepareProof validates full immutable pin inventory, current identity, deterministic prepare task and Params before existing SQL save/session-recovery protocol. Save ABI/refusal/foreign-task/cancellation and private role-isolation scenarios authored.

NOT RUN/NOT APPLIED. Positive SQL source/revocation/session rotation/concurrency fixtures, prepare activation/public source routing and complete first watermark child admission remain open.


### 2026-10-12 — Watermark create documents

CreateIntent.CompileWatermarkDocuments now builds immutable snapshot/preset/policy/frozen/Params documents with existing deterministic identities and dedicated pin-validated prepare plan. Ordinary compiler retains watermark refusal. Unit cases cover exact task/document IDs, session-rotation immutable document stability, missing/duplicate/foreign pins, owned config and cancellation.

NOT RUN. Watermark pending encode/decode/recovery, proof save/package admission, source catalog/public API and first child task still need composition. No activation or module completion claim.


### 2026-10-12 — Watermark pending and package admission

Watermark pending encode/decode now share existing bounded strict envelope and exact document recompilation while requiring independently supplied pin inventory; ordinary pending remains closed. Watermark prepared package admission composes exact pending restore, current pin-bound proof save and existing package SQL inside caller transaction without dispatch. Unit scenarios cover session-rotation restore, missing pin, ordinary-path rejection, proof/package sequence, SQL refusal short circuit and tampered document no-query.

NOT RUN. Durable pending storage/current pin recovery service composition, initial child admission, production scheduler/public API and positive DB lifecycle remain open.


### 2026-10-12 — Watermark catalog and bootstrap dependency

Dedicated watermark catalog observation reads existing authenticated preflight, preserving tenant/actor/session/snapshot/preset order/config hashes and metadata consent while ordinary catalog retains watermark refusal. Scope/hash/cancellation/ordinary refusal unit cases authored. Source inspection exposed first-create bootstrap dependency: SQL version selection requires pending selected documents while verified pending compilation requires pins; selection-intent projection must precede pin preparation and verified prepare-plan admission.

NOT RUN. No runtime service bypass introduced; bootstrap split, persisted source recovery and public API/scheduler composition remain open. Catalog observation is not execution authority.


### 2026-10-12 — Watermark selection bootstrap

Removed watermark first-create pending/pin value cycle: shared immutable document projection always returns zero executable Plan; selection pending encode/decode freezes identical future verified bytes without pin authority. Service.PinWatermarkSelectionPending reuses tenant/idempotency/current catalogue/current selection transaction with dedicated watermark observation and selection-only decoder; physical pin preparation follows commit. Unit cases assert selection/verified byte parity, zero plan, session retry and missing DB refusal.

NOT RUN. Parent shell/materialization bootstrap must use selection projection before pin preparation; current physical source pin preparation, verified admission/public API/scheduler and first watermark child still need composition. Selection persistence grants no dispatch authority.


### 2026-10-12 — Watermark parent/source bootstrap

BeginWatermarkSelectionRun validates selection-only pending/projection and reuses existing API shell SQL without task/plan materialization. StageWatermarkSelectionSources commits tenant-authorized parent shell before source object IO, composes exact-version current/physical pin inventory preparation, then recompiles original pending under verified pin path. Shell ABI/refusal/foreign-tenant and missing stage dependency unit scenarios authored.

NOT RUN. Persisted SQL shell/pin/retry/session/unknown-commit lifecycle still requires qualification. Verified prepare task publication/materialization/transport and production create orchestration plus initial watermark child admission remain open.


### 2026-10-12 — Watermark prepare materialization

Dedicated ResolveWatermarkPrepareTask composes exact pin-bound pending/documents with existing complete scoped recipe closure resolver and bounded prepare task freezer. StageWatermarkCreatePrepare composes shell/source bootstrap, current physical pins, verified prepare closure, immutable document publication, optional existing metadata preparation and blocked task materialization with object IO outside SQL. Unit fixture covers closure identity/missing-pin no-resolver, document publication/materialization and missing service dependencies.

NOT RUN/NOT APPLIED. Installed SQL publication/materialization/retry lifecycle, transport registration/activation composition, public API/scheduler loading and first watermark child remain open.


### 2026-10-12 — Watermark prepare transport recovery

Watermark selection recovery shares authorized idempotency/source-selection rechecks while decoding selection-only evidence. Dedicated prepare transport stage restores persisted selection, prepares verified physical source pins and blocked task, publishes/binds existing scoped V2 transport, then rereads exact pending bytes and repeats current physical watermark source authorization after IO. Unit scenarios cover missing/denied/conflicting/expired/foreign/cancelled recovery and unavailable transport dependencies.

NOT RUN. Positive persisted recovery/publication/unknown-commit fixtures, registered transport/package/activation composition and scheduler/public API integration remain open. Stage result is not executable admission.


### 2026-10-12 — Watermark prepare registration/package pipeline

PreparePinnedWatermarkDelivery composes existing persisted selection recovery, physical pin recovery, verified blocked prepare/transport publication, scheduler-role tenant registration and API-role locked proof/package admission. Dedicated AdmitPreparedWatermarkCreate shares current authorization/idempotency lock protocol and pin-bound admission. No graph seal/public activation emitted while child protocol incomplete. Cancelled/unavailable dependency unit scenarios authored.

NOT RUN. Positive orchestration/unknown-commit/session/source-revoke SQL fixtures and complete initial watermark child admission/scheduler loading remain open. Internal preparation is not public successful create.


### 2026-10-12 — Watermark scheduler plan restore

ExpansionLeaseStore.LoadWatermarkPlan requires externally supplied tenant/parent/project/actor scope, reconstructs full frozen preset inventory, reads all watermark pins under current expansion fence and compiles dedicated verified prepare plan. Repeats exact pin bytes/SHA and canonical persisted proof before returning; ordinary loader remains closed. Unit fixture covers positive plan identity/two pin reads, revoked second read and foreign-project no-pin-read.

NOT RUN. Scheduler actor/project acquisition and production branch routing, SQL positive expansion/pin lifecycle, initial child admission and final commit reauthorization still required. Loader is value evidence, not commit permission.


### 2026-10-12 — Watermark expansion actor/project scope

01737 scheduler-only expansion scope reader derives current parent actor/project under exact tenant/run/task/expansion token, existing complete expansion proof readiness and live editor session. Typed ReadWatermarkScope rejects stale/invalid/cancelled lease and exposes no admission permission; closed tenant SQL adapter includes dedicated query. ABI/absence/invalid fence/cancellation unit scenarios authored.

NOT RUN/NOT APPLIED. Positive installed SQL role/isolation/revocation fixtures, production scheduler routing and initial watermark child admission remain open.


### 2026-10-12 — Lease-bound watermark plan adapter

LoadPersistedWatermarkPlan derives current actor/project from SQL, restores full verified plan/pins and repeats scope after restoration. Dedicated closed adapter permits only exact scope/plan/pin queries bound to tenant/run/expansion/token/task, sets tenant in bounded30s transactions and commits before returning; arbitrary SQL and substituted lease arguments refused before BeginTx. Transaction success/scan/commit/cancel and forbidden query/scope unit scenarios authored.

NOT RUN. Production driver switching deferred until complete child admission; positive installed end-to-end load/authority revocation fixtures and first watermark child remain open.


### 2026-10-12 — Initial watermark SQL stage scope

01738 private initial watermark stage scope requires no pending resize/sharpen, current tenant/expansion request and locked persisted source pin, exact source manifest SHA/color/profile and bounded geometry. Reconstructs closed watermark Params from frozen config/source refs and independently reconstructs CPU RAM/scratch/output resource profile and deterministic task identity. Role-isolation SQL fixture extended.

NOT APPLIED/NOT RUN. This private gate creates no task and grants no direct role access; positive SQL/cross-language float geometry parity, placement validation, quota/Params publication/draft/transport/activation and derivative-to-watermark transition remain open.


### 2026-10-12 — Initial watermark Params publication

01739 dedicated initial watermark Params reserve/complete clone immutable publication/quota/retry protocol with explicit source manifest argument; tenant/usage quota locks precede current pin/source locks. PublishInitialWatermarkParams requires first required operation watermark and carries owned manifest to both SQL phases through closed short-transaction adapter; pending resize/sharpen refused. Query mapping/commit-before-return/foreign-scope unit fixture authored.

NOT RUN/NOT APPLIED. SQL positive reserve/complete/unknown-commit/parity/placement and object publisher fixtures, first child draft/transport/activation plus derivative-to-watermark admission remain open.


### 2026-10-12 — Blocked initial watermark child SQL

01740 dedicated initial watermark child draft derives current queue/metadata/export/shell/quota/retry semantics and replaces first-stage selection with private exact watermark source/Params/resource gate. Requires ready published Params artifact matching owner/SHA/bytes/version before blocked shell creation. Scheduler-only ownership/SECURITY DEFINER/PUBLIC/role isolation SQL fixture authored.

NOT APPLIED/NOT RUN. Positive SQL shell/quota/replay and placement parity, owned Go draft wrapper, transport/activation/cursor and derivative-to-watermark transition remain open.


### 2026-10-12 — Owned initial watermark descriptor

Owned watermark descriptor now retains copied source/config and immutable pin/scope; InitialRequest binds prepare child, tenant, parent, source, preset and deterministic stage-zero watermark identity, refusing pending resize/sharpen. Empty-descriptor refusal unit fixture authored.

NOT RUN; draft SQL invocation, metadata binding, transport/activation/cursor and derivative-to-watermark admission remain open.


### 2026-10-12 — Initial watermark draft Go admission

Initial watermark CreateDraft Go wrapper now binds owned stage-zero source, exact plan request and deterministic export identity, frozen strip/non-strip metadata config, quota reservation and ten-argument01740 ABI. Dedicated metadata binder uses fenced parent inventory. Metadata value compiler accepts watermark presets without changing publication/consent checks; positive metadata and invalid/cancelled draft fixtures authored.

NOT RUN/NOT APPLIED. Positive full draft SQL/Go fixture, transport/activation/cursor, derivative-to-watermark transition and production scheduler/API integration remain open.


### 2026-10-12 — Initial watermark transport

01741 initial watermark blocked-transport receipt checks current initial Params/source scope, exact registered task/profile/closure/Params publication and absence of predecessor edges. Go registration revalidates blocked draft before attaching owned published closure; current receipt handles registration replay. ACL and invalid/cancelled transport fixtures authored.

NOT RUN/NOT APPLIED. Positive registration/replay/SQL fixtures, activation/cursor, derivative-to-watermark transition and production wiring remain open.


### 2026-10-12 — Initial watermark activation and cursor

01742 dedicated initial watermark activation and private cursor clone preserve current queue/metadata/shared sealing predicates; require current source/Params scope, registered closure/profile/publication before atomic dispatch and parent cursor admission. Go activation wrapper and invalid/cancelled unit fixture authored.

NOT RUN/NOT APPLIED. Dynamic definition guards and positive full activation/replay/cursor fixtures need qualification; derivative-to-watermark transition and production scheduler/API wiring remain open.


### 2026-10-12 — Frozen watermark presentation prefix

FrozenDeliveryWatermarkPrefixTask compiles owned initial/continuation resize, sharpen or watermark descriptors from validated actual output prefix; preserves original source, recipe, preset, prior manifests/reports and watermark arrays when consumed. Separate prefix type exposes no ordinary admission authority. Four initial operation variants and missing-inventory unit fixtures authored.

NOT RUN. Positive completed resize/sharpen prefix, physical producer/adoption SQL admission and production wiring remain open; this value/closure compiler does not establish runtime completion.


### 2026-10-12 — Completed watermark prefix unit fixtures

Authored completed resize-only, sharpen-only and resize+sharpen watermark prefix fixtures using deterministic canvas/transform transitions, fresh output arrays and exact report hashes. Fixtures freeze final watermark closure, check predecessor/global stage and reject reordered prefixes or altered report bytes.

NOT RUN. These are authored value/closure fixtures, not physical pixel, producer adoption or runtime proof. SQL continuation admission and production wiring remain open.


### 2026-10-12 — SQL watermark scope after presentation

01743 private after-presentation watermark scope derives exact last canonical resize/sharpen output, requires current physical-verification receipt, and for resize+sharpen requires successful adopted resize predecessor edge. Reconstructs Params and CPU resources against actual output canvas plus persisted watermark pin, returning global stage/predecessor. ACL fixture extended.

NOT RUN/NOT APPLIED. Private scope creates no task or publication; positive SQL parity/producer-revocation fixtures, continuation publication/draft/barrier/activation and production wiring remain open.


### 2026-10-12 — Watermark presentation Params publication

01744 dedicated watermark-after-presentation Params reserve/complete preserve immutable publication/quota/retry semantics through private current-producer scope. Go publisher requires completed actual resize/sharpen prefix and sends owned output manifest to both short tenant transactions. Dedicated adapter routing fixture authored.

NOT RUN/NOT APPLIED. Positive SQL reserve/complete and unknown-commit recovery require qualification; continuation draft/barrier/activation and production wiring remain open.


### 2026-10-12 — Blocked watermark presentation task

01745 blocked watermark-after-presentation task admission requires dedicated open barrier, exact current producer/source/Params resource scope, published Params and quota; creates success edge without dispatch. Owned prefix Go wrapper binds full request, deterministic watermark/predecessor/global stage and output reservation. Invalid/cancelled unit fixture authored.

NOT RUN/NOT APPLIED. Dedicated barrier producer, transport/activation, positive SQL/Go lifecycle and production integration remain open; new admission cannot execute before barrier implementation.


### 2026-10-12 — Watermark presentation barrier and transport

01746 dedicated presentation-to-watermark barrier rechecks current source and exact verified producer, handles replay, and transitions the completed resize+sharpen barrier before opening one watermark slot.01747 blocked transport receipt and owned prefix registration require exact Params/resource/predecessor and current source; no dispatch occurs.

NOT RUN/NOT APPLIED. Positive barrier/transport fixtures, activation, initial resize/sharpen watermark admission and production scheduler/API integration remain open.


### 2026-10-12 — Watermark presentation activation

01748 atomically closes presentation-watermark barrier, seals graph, activates registered watermark task and acknowledges verified resize/sharpen predecessor. Dedicated current-authority activation receipt and owned Go activation/recovery wrappers authored; invalid/cancelled and SQL role-isolation fixtures extended.

NOT RUN/NOT APPLIED. Positive activation/replay fixtures, verified presentation loader/barrier Go bridge, initial watermark-preset derivatives and production API/scheduler integration remain open.


### 2026-10-12 — Verified watermark presentation loader

Watermark presentation loader compiles required resize/sharpen from exact prefix/pin, reuses physical RGB/alpha verifier and rereads canonical producer and source pin after IO. Opaque evidence opens01746 barrier only for last required derivative with owned request/producer/output. Invalid/cancelled evidence unit fixture authored.

NOT RUN. Positive physical loader/revocation and barrier fixtures, initial derivative admission and resize→sharpen watermark path plus production wiring remain open.


### 2026-10-12 — Watermark presentation admission coordinator

Owned watermark presentation admission coordinator now probes activation and transport receipts before object IO, commits blocked creation before immutable transport publication, then registers/activates atomically in short tenant transactions with cancellation-independent rollback. Fresh/recovered/registered/create/publish/commit failure fixtures authored and attached to completed presentation-prefix cases.

NOT RUN. Coordinator is not production scheduler wiring; positive SQL lifecycle, initial derivative admission and resize→sharpen watermark orchestration remain open.


### 2026-10-12 — Watermark activation lock ordering and ABI fixtures

Source review removed redundant pre-scope source SHARE locking from01748 activation receipt so tenant/usage quota FOR UPDATE locks remain first; removed unreachable encoder export mutations from watermark activation. Completed-prefix fixtures now exercise exact draft/transport/activation query routing, replay, SQL refusal and foreign request rejection for all three derivative combinations.

NOT RUN/NOT APPLIED. Source review is not SQL execution or concurrency proof. Initial derivative admission, loader positive fixtures and production scheduler/API wiring remain open.


### 2026-10-12 — Initial watermark derivative Params

01749 dedicated initial resize/sharpen watermark Params scope requires current tenant, exact initial derivative Params and persisted watermark source authority; separate reserve/complete retain quota/immutable retry semantics. Go initial derivative publisher and closed short-transaction adapter plus query-isolation fixture authored.

NOT RUN/NOT APPLIED. Initial derivative shell/activation, resize→sharpen continuation and production wiring remain open; published Params alone do not dispatch a task.


### 2026-10-12 — Initial watermark derivative draft

01750 dedicated initial resize/sharpen watermark draft preserves current ordinary shell/metadata/queue/quota predicates, but requires current watermark initial-derivative scope and ready immutable Params. Owned prefix initial request, metadata binder and draft Go wrapper bind stage-zero source/prepare request and deterministic child/export identity; refusal fixture authored.

NOT RUN/NOT APPLIED. Positive initial derivative draft/resource parity, transport/activation, resize→sharpen and production scheduler/API integration remain open.


### 2026-10-12 — Initial watermark derivative transport

01751 dedicated blocked initial resize/sharpen watermark transport receipt checks current initial derivative Params/source scope, exact descriptor/profile/closure and publication with no predecessor edges. Separate Go initial-derivative transport/register methods retain owned closure and revalidate shell; invalid/cancelled and SQL ACL fixtures authored.

NOT RUN/NOT APPLIED. Positive initial derivative shell/transport fixtures, resource parity, activation/cursor and production integration remain open.


### 2026-10-12 — Initial watermark derivative activation

01752 dedicated initial watermark resize/sharpen activation/private cursor clone preserve shared queue/metadata/registry/closure/quota sealing predicates and require current initial derivative Params/source publication. Go initial-only activation binds owned plan request/export identity; invalid/cancelled and SQL ACL fixtures authored.

NOT RUN/NOT APPLIED. Positive initial derivative lifecycle/resource parity, resize→sharpen continuation and production scheduler/API integration remain open.


### 2026-10-12 — Watermark resize-to-sharpen barrier

01753 dedicated watermark resize→sharpen barrier requires both frozen derivatives, current watermark source, exact canonical verified resize and replay re-verification. Opaque physical presentation evidence exposes separate BeginSharpenInTransaction with owned request/producer/output binding; invalid evidence fixture authored.

NOT RUN/NOT APPLIED. Sharpen Params/draft/transport/activation dedicated admission and production scheduler/API integration remain open; barrier alone dispatches nothing.


### 2026-10-12 — Watermark sharpen Params publication

01754 dedicated watermark sharpen-after-resize Params scope requires both derivatives, current verified producer via continuation scope, exact deterministic sharpen owner, persisted watermark pin and open two-stage cursor-zero barrier. Go publisher validates one real resize output and uses dedicated short tenant transaction adapter; routing fixture authored.

NOT RUN/NOT APPLIED. Sharpen blocked task/transport/activation admission and production scheduler/API integration remain open.


### 2026-10-12 — Blocked watermark sharpen task

01755 dedicated blocked sharpen-after-resize task derives current continuation admission with exact stage-one/sharpen restriction and current watermark sharpen Params/source scope. Go wrapper binds owned request, deterministic resize predecessor and sharpen descriptor/reservation. Invalid-descriptor fixture authored.

NOT RUN/NOT APPLIED. Positive sharpen task/resource parity, transport/activation and production scheduler/API integration remain open.


### 2026-10-12 — Watermark sharpen transport

01756 dedicated sharpen transport receipt derives current registered continuation checks with stage-one/sharpen restriction and watermark sharpen source/Params scope. Owned prefix Go transport/register methods independently bind resize predecessor, exact request and owned closure. Invalid/cancelled transport fixture authored.

NOT RUN/NOT APPLIED. Positive sharpen transport/activation fixtures and production scheduler/API integration remain open.


### 2026-10-12 — Watermark sharpen activation

01757 dedicated sharpen activation/replay derives current continuation semantics, restricts stage-one sharpen, requires current watermark transport/source authority and treats NULL task activation as refusal. Go owned prefix activation and durable replay wrappers plus invalid-descriptor fixture authored.

NOT RUN/NOT APPLIED. Positive sharpen lifecycle and concurrency/resource parity fixtures, admission coordinator and production scheduler/API integration remain open.


### 2026-10-12 — Watermark sharpen admission coordinator

Watermark sharpen admission coordinator now resumes activation/registered transport, commits blocked creation before object publication, then registers/activates in short tenant transactions. Intermediate resize→sharpen prefix fixtures exercise coordinator fresh/replay/failure phases and exact draft/transport/activation query routing.

NOT RUN. Positive SQL lifecycle/resource/concurrency and production scheduler/API integration remain open; local fixtures are authored code, not PASS evidence.


### 2026-10-12 — Watermark scheduler continuation installation gate

Source inspection confirmed production DeliveryExpansionJob still uses ordinary LoadPlan/pipeline. Added scheduler continuation installation validator for12 exact public and4 private ABIs, owner/SECDEF/effective-role/PUBLIC isolation; encode/presentation/sharpen public coordinators run it in a short transaction before admission/object IO. Refusal/error/cancellation fixtures authored.

NOT RUN. Installation validation does not enable production watermark routing. Initial child coordination, full SQL lifecycle/resource parity and production scheduler/API integration remain open.


### 2026-10-12 — Watermark presentation physical loader fixtures

Authored watermark resize/sharpen physical-loader fixtures with actual float32 RGB/alpha NPY bytes and exact producer metadata; cover valid reread, revoked second pin, changed fence and corrupted RGB, plus owned evidence outputs. Completed prefix fixtures now feed real array hashes.

NOT RUN. These fixtures do not prove runtime quality or lifecycle completion; initial child coordinator and production scheduler/API integration remain open.


### 2026-10-12 — Initial watermark activation acknowledgement

01758 initial watermark/derivative activation acknowledgement requires current pin/Params scope, exact task/closure/publication/request/export and committed parent cursor/delivery item binding. Owned Go receipt wrappers and invalid-descriptor fixture authored for both initial task types.

NOT RUN/NOT APPLIED. Positive initial receipt/recovery SQL fixtures, initial admission coordinator and production integration remain open.


### 2026-10-12 — Initial watermark admission coordinator

Common initial watermark/derivative admission coordinator now acknowledges committed activation, probes transport, commits blocked shell before object IO and rechecks/registers/activates in a short tenant transaction. Separate typed derivative adapter prevents continuation admission reuse. Missing-dependency/cancellation fixture authored.

NOT RUN. Positive initial coordinator phase/SQL fixtures and installation coverage remain open, as does production scheduler/API wiring.


### 2026-10-12 — Initial watermark installation gate

Scheduler installation gate expanded to19 public and9 private initial/continuation contracts, including01758 acknowledgement and private cursor functions. Initial coordinator now validates installation before shell/object phases; exported initial admission checks cancellation before descriptor validation. Cancellation/count fixtures updated.

NOT RUN. Positive initial coordinator phase/SQL lifecycle and production scheduler/API integration remain open; installation checks are only ABI/role evidence.


### 2026-10-12 — Initial watermark admission phase fixtures

Authored initial admission coordinator unit scenarios for fresh admission, committed activation recovery, registered transport recovery, concurrent activation, installation refusal, draft/publication/registration/activation failure and blocked-shell commit failure. Fixtures assert tenant transaction ordering and reject object IO inside transactions; failure paths require rollback and stop subsequent phases.

NOT RUN. These are coordinator control-flow fixtures, not SQL lifecycle, resource parity or production scheduler/API integration evidence. Full integration remains open.


### 2026-10-12 — Watermark initial child pipeline composition

Added dedicated DeliveryWatermarkChildPipeline composing authorized resolution, owned source/inventory, exact WatermarkExportRequest, first resize/sharpen-or-watermark selection, immutable Params, frozen task closure, metadata inventory, exact output/config/Params reservation and initial admission. Resolver contract explicitly requires current persisted pin/ACL and physical array verification. Missing dependency/cancellation fixtures authored.

NOT RUN. No concrete watermark child resolver or production route is installed by this increment. Continuation observation and assembly acceptance must be completed together before production routing; source quality algorithms unchanged.


### 2026-10-12 — Initial watermark source physical gate

Watermark child pipeline now requires an object reader and owned exact RGB/optional-alpha descriptors. Before Params publication it validates manifest SHA, float32 gamma24 working profile, bounded geometry, descriptor ordering/ref/SHA/version/MIME/size, then streams physical NPY verification with close/error propagation. Cancellation and absent-reader refusal fixtures authored. Descriptor SQL authorization still belongs to the concrete resolver, which remains unfinished.

NOT RUN. No production watermark resolver/routing claim; current image algorithms unchanged.


### 2026-10-12 — Dedicated watermark child source inventory

Authored forward-only01759 scheduler-only source inventory. It checks exact frozen child request, current tenant/lease and persisted pin, inherits adopted prepare inputs and adds only exact pinned watermark manifest/RGB/alpha refs. Missing/expired objects and inherited contract/SHA conflicts refuse the whole page; pin authority is repeated after assembly. Pagination preserves inherited ordinals and appends bounded mark ordinals. No arbitrary artifact lookup is exposed.

NOT RUN/NOT APPLIED. Go resolver/query adapter, SQL lifecycle/ACL fixtures and production integration remain open.


### 2026-10-12 — Lease-bound watermark inventory query adapter

Added private Go source-query adapter bound to tenant/parent/expansion/token/slot and exact frozen request. It accepts only01759 inventory ABI and bounded integer pagination, sets tenant context, commits each query before returning and rolls back on query/commit failure. Authored success/failure and query/scope/type/cancellation substitution fixtures.

NOT RUN. Resolver composition and production routing remain open; adapter alone does not resolve or authorize physical object reads.


### 2026-10-12 — Concrete watermark child resolver

Added SQLDeliveryWatermarkChildResolver: derives current persisted plan/scope, compares selected child config/request to supplied plan, reads current pin and lease-bound01759 inventory, verifies exact canonical source/recipe/preset/mark documents, projects transitive authorized closure, streams source and mark RGB/alpha verification and repeats pin authority after IO. Added owned WatermarkChildConfig accessor for pin reads (value evidence only). Descriptor substitution and absent-dependency/cancellation fixtures authored.

NOT RUN. Positive persisted-plan/object/SQL lifecycle scenarios remain unqualified. Resolver is not yet installed into production runtime; continuation observer and assembly remain open.


### 2026-10-12 — Watermark continuation Params recovery

Authored01760 scheduler-only saved Params observation for sharpen/watermark/encode under exact child request, current tenant/persisted watermark pin and ready versioned Params artifact. Added separate Go reader validating deterministic next-task identity and exact Params SHA, bounded closed ref JSON; positive/absent/substituted/unknown/trailing fixtures authored. Installation gate now checks21 public contracts.

NOT RUN/NOT APPLIED. Saved publication is not activation evidence. Observer composition, tenant-scoped recovery adapter and production/assembly integration remain open.


### 2026-10-12 — Watermark presentation observer composition

Added private advancePresentation orchestration: physically verified resize/sharpen prefix, authorized output inventory extension, saved Params/activation recovery before barrier mutation, dedicated sharpen-or-watermark barrier/publication/admission and bounded two-stage replay. Added closed tenant observation adapter for exact lease/slot/operation producer and saved-Params reads; every read commits before object IO.

NOT RUN. Watermark-output→encode observation, pending-slot discovery, end-to-end observer fixtures and production/assembly wiring remain open. This helper is not yet a production observer installation.


### 2026-10-12 — Watermark-to-encode observer composition

Added watermark pipeline ObserveChild: resolves current source/pin, reconstructs verified presentation, waits for required derivative, verifies watermark physical output/current producer, extends typed inventory, recovers saved encode Params/activation before opening encode barrier, publishes immutable Params and admits encode in dedicated short-transaction coordinator. Resolver now retains trusted target ICC pin. Missing-runtime/cancellation fixtures authored.

NOT RUN. Full observer lifecycle fixtures, pending-slot discovery, encode completion/assembly acceptance and production routing remain open. New admission is not delivery completion evidence.


### 2026-10-12 — Watermark pending-slot discovery

Authored01761 dedicated scheduler pending discovery covering unadopted succeeded resize/sharpen/watermark candidates, exact frozen child request and current persisted pin/tenant/parent fence. Watermark pipeline now implements DeliveryChildObserver with tenant-transaction PendingSlots; malformed/unordered/out-of-cursor candidate lists roll back and never escape as partial results. Discovery fixtures and SQL ACL catalogue updated; installation gate checks22 public contracts.

NOT RUN/NOT APPLIED. Discovery is not physical verification or completion. Encode completion/assembly acceptance, complete observer lifecycle fixtures and production routing remain open.


### 2026-10-12 — Watermark encode completion authority

Authored01762 private completion predicate and guarded current encode-context/adoption patches. Watermark exports require current persisted source authority, succeeded canonical watermark with physical verification/adoption receipts, and its exact WorkingImage in encoder inputs; adoption additionally binds the actual Params.image to that output. Ordinary non-watermark requests retain existing behavior. Installation private ABI count10 and ACL fixture updated.

NOT RUN/NOT APPLIED. Dynamic definition compatibility, concurrent source/ACL revocation lock coverage and positive completion/metadata lifecycle remain unqualified. Assembly and production integration remain open.


### 2026-10-12 — Watermark producer/assembly observation authority

Authored01763 guarded producer-ready integration: strip and metadata final outputs both require current watermark source authority and canonical adopted watermark ancestry through the deterministic encoder. Completion predicate supports open/closed parent expansion so retained completed package observations do not depend on an active scheduler lease. Existing child/project/output/provenance checks remain in their original functions.

NOT RUN/NOT APPLIED. Positive completed-package/metadata/revocation and lock-race scenarios remain unqualified; production routing and comprehensive lifecycle fixtures remain open.


01763 retained-source correction: separate private cloned source predicate supports waiting_review/completed parent runs while retaining tenant/project/session/pin/asset/array ACL checks. Original admission source predicate remains draft/running-only. Installation checks11 private contracts; ACL fixture extended. NOT RUN/NOT APPLIED.


### 2026-10-12 — Mixed delivery child routing

Added validated frozen ChildUsesWatermark selection and DeliveryChildRouter for per-slot admission/observation. Selection errors never fall back to another protocol. Pending discovery merges bounded ordered pages, deduplicates overlapping ordinary/watermark candidates and truncates after sorting. Ordinary routing, invalid selection, cancellation and merge/refusal fixtures authored; watermark plan fixture now asserts watermark selection.

NOT RUN. Router is not installed into production runtime yet; plan loading, lifecycle/assembly/source locking and startup/API integration remain open.


### 2026-10-12 — Production delivery scheduler watermark wiring

NewDeliveryExpansionJob now constructs authorized watermark resolver/pipeline and mixed child/observer router. DeliveryRuntimePlanStore preserves existing lease claim/renew/release and validates scheduler installation before current tenant-scoped plan restoration. Plan loading explicitly uses ordinary compiler only when all decoded configs contain no watermark; missing/rejected pin never falls back. Runtime construction and missing-loader/cancellation fixtures authored.

NOT RUN/NOT APPLIED. Source production constructor is wired, not deployed/qualified. Public API create wiring, complete ordinary/mixed lifecycle, SQL resource/locking and quality qualification remain open. Existing scheduler enabled flag remains required.


### 2026-10-12 — Durable watermark recipe selection preparation

Added separate watermark recipe selection/pending preparation using the existing idempotency selection ledger, exact original catalog and immutable V26 recipe freeze receipts. Selection decoder/catalog mode is explicit; promotion rechecks original selection in the same transaction and persists selection-only pending before source pin/prepare IO. Ordinary selection entrypoints remain closed to watermark. Missing-authority/cancellation fixtures authored.

NOT RUN. Public CreateDelivery routing to selected prepare protocol and activation/SQL lifecycle qualification remain open.


### 2026-10-12 — Public delivery create selected-protocol wiring

CreateDelivery now recovers selection-compatible pending evidence (including completed tickets before dependencies), then PrepareSelectedDelivery pins/freezes durable selection, validates all frozen preset configs and chooses ordinary or watermark prepare without failure fallback. Both branches retain API/scheduler credential separation, metadata preflight and existing atomic public activation/idempotency. Catalog mode/duplicate/SHA/refusal/cancellation fixtures authored.

NOT RUN/NOT APPLIED. Public source wiring is not successful deployment evidence. Full mixed/ordinary/watermark create+activate lifecycle, SQL source/resource/locking parity, physical quality and downstream qualification remain open.


### 2026-10-12 — Watermark completion source/authority locks

Authored01764 private retained-source lock predicate: verifies current source first, locks tenant/parent/workflow/project/expansion, exact watermark asset/version/master/arrays and actor/session/membership/optional project ACL with NOWAIT, then repeats complete retained-source readiness. Encode completion/producer observation invokes it through guarded private helper replacement. Installation private ABI count12 and ACL fixture updated.

NOT RUN/NOT APPLIED. Real concurrent ACL/source deletion/tenant lifecycle tests and exact DB lock-order qualification remain open; source implementation is not proof of race-free deployment.


### 2026-10-12 — Independent initial watermark derivative resource gate

Authored01765 SQL CPU resource derivation from exact source WorkingImage SHA/dimensions/alpha and resize/sharpen Params, including binary64 ties-to-even resize geometry. New10-argument initial derivative wrapper requires exact independently derived execution_profile and source binding before existing shell/quota checks. Go passes owned source bytes; old9-argument core is private to remove scheduler bypass. Installation/ACL fixtures updated (22 public/14 private).

NOT RUN/NOT APPLIED. SQL/Go boundary numeric parity and positive draft/quota/concurrency lifecycle remain unqualified; continuation sharpen resource parity remains open.


### 2026-10-12 — Continuation watermark sharpen resource gate

Authored01766 nine-argument sharpen draft wrapper requiring independently computed exact CPU profile/output reservation from owned actual resize WorkingImage, bound to current canonical resize producer and Params.image. Previous eight-argument core is private. Go forwards frozen actual source bytes; intermediate prefix fixture asserts actual resized source at SQL boundary. Installation/ACL fixtures updated (22 public/15 private).

NOT RUN/NOT APPLIED. SQL/Go numeric parity and complete positive/concurrency lifecycle remain unqualified; resource wrapper implementation alone is not measured hardware qualification.


### 2026-10-12 — runtime lease preflight
Runtime plan loading now rejects incomplete or out-of-bounds expansion leases before installation transactions, sharing the canonical delivery lease predicate. Twelve malformed-lease cases authored; tests NOT RUN, migrations NOT APPLIED. Full lifecycle and broader implementation gaps remain open.


### 2026-10-12 — assembly matrix tenant context
Delivery assembly matrix observation and failure-witness rereads now use a lease-bound allowlisted adapter with short tenant transactions; only exact expansion/token observation and non-release renewal are permitted. Refusal/cancellation tests authored. Other assembly publication/admission SQL tenant propagation remains under review. Tests NOT RUN; migrations NOT APPLIED.


### 2026-10-12 — assembly admission tenant transactions
Assembly registered receipt, blocked-shell creation, input registration and activation now execute in bounded short tenant transactions, with shared complete lease validation and cancellation-independent rollback. Renewal uses the closed tenant adapter. Commit/refusal ordering tests authored. Publication/recovery inventory paths still need tenant propagation review. Tests NOT RUN; migrations NOT APPLIED.


### 2026-10-12 — assembly recovery tenant context
Assembly preparation now restores root/shards/transitive inventory through a root-and-lease-bound tenant adapter; exact ref/hash, expansion/token and bounded shard index are required before SQL. Mutation queries are not admitted. Seven substitution/refusal cases authored. Publication repository tenant propagation and positive lifecycle qualification remain open. Tests NOT RUN; migrations NOT APPLIED.


### 2026-10-12 — assembly document publication tenant context
Assembly root/shard/Params reserve and complete plus root retention now use an explicit seven-query lease-bound publication adapter, validating exact tenant/run/expansion/token before short transactions. Object IO remains between committed calls. Foreign-scope and unsupported-query tests authored. Input transport publication and failure finalization still require review; full lifecycle remains unqualified. Tests NOT RUN; migrations NOT APPLIED.


### 2026-10-12 — assembly transport and failure tenant context
Assembly input transport reserve/complete now use short tenant transactions bound to deterministic assembly task, parent run and canonical manifest/chunk purposes. Failure finalization now shares bounded tenant admission transaction and cancellation-independent rollback. Refusal tests authored; positive transport/failure/concurrency lifecycle still unqualified. Tests NOT RUN; migrations NOT APPLIED.


### 2026-10-12 — shared watermark export identity
Fixed a blocking ordinary-only validation in shared NewChildExportID and single export observation: frozen watermark children now derive the same deterministic export identity through strict ChildUsesWatermark validation. Ordinary admission ExportRequest remains watermark-closed. Existing full watermark proof fixture extended with identity, single/page observation and foreign-child cases. Tests NOT RUN; migrations NOT APPLIED. Complete delivery lifecycle remains open.


### 2026-10-12 — watermark assembly matrix fixture
Expanded the existing fully pinned watermark prepare fixture through all frozen child export identities, completed matrix root/shard construction and exact required-input closure derivation. Asserts EncodedExport inclusion and rejects corrupted shard bytes. Inspection found remaining ExportRequest uses confined to ordinary child configuration/commit paths, which remain intentionally closed to watermark admission. Tests NOT RUN; migrations NOT APPLIED. Full SQL lifecycle remains open.


### 2026-10-12 — assembly adapter positive lifecycle fixtures
Authored positive transaction lifecycle tests for all four assembly SQL adapters (observation, recovery, publication, transport), checking exact tenant setup/query arguments, successful commit, query-error rollback and commit-error propagation. Twelve lifecycle cases added; these are adapter tests, not complete SQL/application lifecycle proof. Tests NOT RUN; migrations NOT APPLIED.


### 2026-10-12 — watermark failure matrix fixtures
Inspected metadata Stage/adoption/source SQL adapters: existing tenant setup retained. Extended fully pinned watermark fixture with failed/cancelled terminal witness identity, no-publication guarantee for failed required matrices, and foreign export refusal. Tests authored only; no positive SQL/runtime lifecycle claim. Tests NOT RUN; migrations NOT APPLIED.


### 2026-10-12 — derivative resource parity fixtures
Authored eight resource-profile cases for ties-to-even resize rounding, contain sizing, upscale policy, minimum dimension and sharpen with/without alpha; exact RAM/scratch/output/pixel expectations plus privileged SQL/Go parity integration fixture for01765/01766. Tests NOT RUN; SQL parity remains unproven until isolated migrated DB execution. Migrations NOT APPLIED.


### 2026-10-12 — strict resize profile fields
Closed inspected SQL/Go malformed resize discrepancy:01767 forward-only profile replacement rejects null/non-number dimensions and non-object Params/image; Go now requires explicit non-null allow_upscale for resize. Added five malformed field unit cases. Existing resource policy/round-to-even unchanged. Tests NOT RUN;01767 NOT APPLIED; SQL negative parity fixture remains to extend.


### 2026-10-12 — malformed resize SQL/Go fixtures
Expanded strict resize resource cases to ten shared Go/SQL malformed variants: null/string/fractional/zero/oversized long edge, missing/null upscale, null/incomplete box and mixed sizing. Added privileged SQL rejection integration test and updated ACL fixture database prerequisite to01767. Source inspection confirms presentation compiler emits explicit allow_upscale. Tests NOT RUN; migrations NOT APPLIED.


### 2026-10-12 — watermark installation runtime dependencies
Expanded watermark scheduler installation from22 to37 scheduler-only exact ABIs: all five Params reserve/complete pairs, three continuation barriers, current pin reader and watermark stage result. Existing owner/SECDEF/PUBLIC/role isolation checks apply before processing. Added duplicate/conflicting signature and required runtime dependency assertions. Shared ordinary dependencies remain separate installation scope. Tests NOT RUN; migrations NOT APPLIED.


### 2026-10-12 — worker derivative resource vectors
Authored worker counterparts for six Go/SQL resize dimension/resource vectors (ties-even, contain, upscale, minimum dimension), exact RAM acceptance and one-byte shortfall refusal, plus invalid budgets/dimensions. Source confirms worker rounding and RAM formula align; no pixel-processing changes. Tests NOT RUN; cross-runtime parity remains unverified until execution. Migrations NOT APPLIED.


### 2026-10-12 — watermark SQL placement admission
Authored01768 private watermark placement admission with binary64 ties-even sizing, pixel margin bounds, all five anchors and frame-fit checks. Exact-definition guarded patches apply it to both initial and after-presentation source gates; scheduler installation includes the private ABI (37 public/16 private). No worker pixel algorithm changed. SQL/Go placement parity fixtures and lifecycle execution remain open. Tests NOT RUN;01768 NOT APPLIED.


### 2026-10-12 — watermark worker RAM admission
Watermark worker now enforces the existing scheduler RAM reservation for original source plus original mark before opening either array, via the common derivative admission policy. Added exact budget/one-byte-shortfall original-mark cases; pixel compositing/resampling unchanged. Updated private placement ACL integration fixture through01768. Tests NOT RUN; migrations NOT APPLIED; operation-level pre-IO refusal fixture and full lifecycle remain open.


### 2026-10-12 — watermark pre-IO RAM fixtures
Authored operation-level watermark.execute RAM admission fixture: under-budget/missing/invalid v2.2 budget must refuse after source/mark validation and before first open_image; exact budget reaches opening. This covers production call ordering beyond arithmetic helper tests. Tests NOT RUN; physical quality and full scheduler/worker lifecycle remain open. Migrations NOT APPLIED.


### 2026-10-12 — watermark geometry before array IO
Watermark worker now computes unchanged ties-even size/margins/anchor and frame-fit validation before opening or resampling arrays. Extracted pure _placement; all five anchor/tall-mark cases and operation pre-IO overflow fixture authored. Existing linear premultiplied resampling/source-over untouched. Tests NOT RUN; quality and full lifecycle remain unverified. Migrations NOT APPLIED.


### 2026-10-12 — watermark numeric quality fixtures
Authored independent numeric watermark pixel-quality invariants: linear-light source-over for opaque/partial/transparent source alpha, original buffer ownership, exact untouched/hidden RGB preservation, premultiplied area downscale excluding transparent hidden color, and zero coverage. Pixel algorithms unchanged. Tests NOT RUN; no quality PASS claim. Migrations NOT APPLIED.


### 2026-10-12 — watermark assembly retained closure
Authored01769 exact-definition-guarded assembly retention and typed inventory extension for persisted watermark manifest/RGB/alpha refs. Existing readiness/artifact locks, duplicate/hash/contract checks and lease fence apply to the extended set. Ordinary empty pins add no refs; existing function ABI/ACL preserved. Positive retention/purge/replay SQL fixtures remain open;01769 NOT APPLIED. Tests NOT RUN.


### 2026-10-12 — independent Go watermark assembly roots
Watermark PreparePlan now owns manifest/RGB/alpha assembly roots derived only from validated immutable pins; AssemblyRequiredInputs independently requires them, matching01769 SQL inventory without changing initial prepare task closure. Full watermark matrix fixture asserts all three dependencies. Corrected review.ArtifactRef.ID field names in authored resource parity fixture by source inspection. Tests NOT RUN; migrations NOT APPLIED.


### 2026-10-12 — watermark retained inventory recovery fixtures
Extended full pinned watermark fixture to verify persisted-plan restore preserves three independent assembly roots, complete inventory recovery succeeds, and missing/wrong-contract manifest/RGB/alpha is rejected by Go recovery. Source inspection confirms SQL registered-transport exact inventory comparison consumes the extended01769 inventory. Tests NOT RUN; real SQL retention/replay still open. Migrations NOT APPLIED.


### 2026-10-12 — V32 complete candidate snapshot cancellation
V32 candidate reader now checks cancellation before SQL and after terminal stream completion, including empty snapshots, and never returns partial results on cancellation/error. Authored complete/stream-error/query-error/empty-cancelled/partial-cancelled/early-cancel scenarios with stream closure assertions. Full candidate SQL remains streamed without tenant truncation; DRR still retains full snapshot in memory. Tests NOT RUN.


### 2026-10-12 — V32 atomic invalid snapshot rejection
V32 DRR now rejects zero cycle clocks even for empty candidate snapshots before granting quantum or selecting work. Authored eleven invalid snapshot cases over preexisting deficits/tenant/class cursor, proving expected no-mutation behavior for IDs, queue, future age, cost/heavy bounds and duplicates. Existing validation precedes accounting; tests NOT RUN.


### 2026-10-12 — V32 affinity terminal cancellation
V32 affinity reader now checks cancellation at terminal stream completion, discarding empty/partial residency evidence instead of allowing a cancelled read to influence dispatch. Authored both terminal-cancellation cases. Source inspection confirms qualified SQL affinity is wired into CapacityDispatcher; corrected obsolete pure-helper comment. Tests NOT RUN; concurrent residency/revocation and full scheduler acceptance remain open.


### 2026-10-12 — Metal10GB heartbeat regression fixture
Inspected Metal verification policy: gpu_budget_bytes remains exactly10,000,000,000; existing MPS allocator receipt and heartbeat budget caps are wired. Added multi-heartbeat regression using installed allocator receipt and increasing driver allocations, checking decreasing residual VRAM and refusal one byte above allocator ceiling. No RTX or hardware execution; test NOT RUN.


### 2026-10-12 — V32 affinity FIFO admission
Source inspection confirms capacity_task_affinity has no array cardinality limit; no candidate truncation or batching introduced. Reader now rejects zero cycle clocks and non-FIFO selected heads (age and UUID tie-break within tenant/effective class) before SQL, matching the independent pure affinity selector. Three refusal unit cases authored. Tests NOT RUN; migrations NOT APPLIED; complete scheduler acceptance remains open.


### 2026-10-12 — V32 policy read cancellation boundary
Installed queue-policy reader now refuses cancellation before SQL and after row scan, preventing a cancelled leader read from returning policy state. Authored transaction-boundary cases for success, early/scan cancellation, SQL error, absent installation and digest mismatch. Existing digest pin and field validation retained. Tests NOT RUN; full scheduler acceptance remains open.


### 2026-10-12 — assembly coordinator lease preflight
RegisterPreparedAssembly now rejects structurally invalid expansion leases before invoking registration stores or publishing objects; live lease authority remains SQL-owned. Updated positive fixture with required package identity and authored six no-side-effect refusal cases (tenant/package/token/expiry/cursor/foreign parent). Tests NOT RUN; migrations NOT APPLIED; complete retention/replay lifecycle remains open.


### 2026-10-12 — assembly registration cancellation phases
Assembly registration coordinator now checks cancellation after successful store/publication phases before advancing or returning a receipt. Committed blocked/registered state remains replayable; cancellation never implies rollback of a completed phase. Authored five cancellation boundaries across registered/create/two renewals/register, checking no later store calls and no early object publication. Tests NOT RUN; full assembly SQL lifecycle remains open.


### 2026-10-12 — V32 render review before/after preview predecision proof
Added a predecision runner that pins the pending review, canonical source WorkingImage, source canvas, adopted child WorkingImage, both preview catalogues, signed API responses and exact object versions before any review decision. The `before` catalogue now follows the public service's primary V26 render preview lookup within the same project and falls back to the accepted original asset preview only when that primary receipt is absent; `after` remains bound to the adopted child result. The runner re-reads each catalogue after physical download and the pending review at the end, with one overall deadline. Added positive/fallback/change/deadline unit fixtures and primary-catalogue fixture. Tests NOT RUN; migrations NOT APPLIED. Full restore review decision, terminal observation and final download workflow remain open.


### 2026-10-12 — V32 accepted render output download
The after-side preview catalogue now supports a separate exact accepted-review revision read while retaining pending-only reads by default. Added a completed child-run `/outputs` downloader: before network access it requires the accepted SQL receipt and terminal parent/child state; it validates the two exact derivative and manifest signed links against pinned bucket/key/version/SHA/size, downloads both bounded streams without forwarding the API bearer to storage, and rechecks SQL receipt and terminal state after IO. Added success, stale catalogue/terminal, version, SHA, duplicate and foreign-origin unit fixtures. This is a final-download component; the complete `v2_workflow` probe runner/report and native integration remain open. Tests NOT RUN; migrations NOT APPLIED.


### 2026-10-12 — V32 positive v2_workflow probe composition
Added the one-frame V2 workflow runner and private probe-report wrapper. It composes fresh batch admission and physical result, pending-review discovery, both physical review previews, exactly one review decision POST, bounded polling for completed parent/child, and exact signed final output download under a shared 1–1200 s deadline and fixture guard. The wrapper emits all-true `v2_workflow` checks only after these stages and binds the batch/child/review/candidate/preview/download digests in the private report. Stage failure and global-deadline unit scenarios authored NOT RUN. Installation against the actual isolated restore deployment, complete persisted probe-report verification and full four-probe acceptance remain open; migrations NOT APPLIED.


### 2026-10-12 — V32 v2_workflow fixture pin before mutation
Corrected the report wrapper to recompute a credential-free canonical fixture descriptor before any batch POST: replay database, tenant/project/snapshot/policy, hashed idempotency key, actor/source/version/canvas, frozen recipe, session, installed origins/buckets/sRGB profile and deadline. The independently supplied `fixture_sha256` must match this descriptor; a preexisting report path also refuses before execution. The private report includes the same descriptor for later independent review. Added wrong-pin and bad-format preflight fixtures; tests NOT RUN. Real installation and four-probe acceptance remain open.


### 2026-10-12 — V32 v2_workflow report read-side closure
Expanded the fixture descriptor with restore run/build/recovery/replay identity and included the after-side manifest digest in observations. `read_restore_probe_report` now validates the exact V2 observation shape, recomputes the canonical fixture digest, checks replay identity, UUID/hash/poll bounds and requires final downloaded derivative/manifest SHA to equal the physically previewed after-side SHA. Added tampered replay/pin/delivery/shape unit scenarios, NOT RUN. This blocks a structurally empty all-true V2 report from qualifying; live native integration and the remaining legacy Apply encoded-export report are still open.


### 2026-10-12 — V32 acceptance fixture compatibility
Updated existing acceptance coordinator and persisted-report unit fixtures to construct a canonical independently hashed V2 probe document rather than a generic `unit_fixture` placeholder. This keeps the stricter read-side V2 closure exercised through both acceptance paths. Tests NOT RUN; no claim of live restore acceptance. The legacy Apply encoded-export chain still needs a dynamically linked yet independently pinned complete report; WorkingImage document delivery alone is not treated as final export qualification.


### 2026-10-12 — V32 legacy Apply output to Selection binding
Added a privileged read-only binder for the dynamic server-created Apply output collection. It requires the exact completed Apply run/project/source collection, sole new item/version/master artifact, live object and the independently pinned export preset revision/config SHA. It derives the current collection revision and one-item selection digest from the confirmed item payload, then creates a fresh Selection target using a separately supplied key; no output UUID is predicted before Apply. Added missing/duplicate/foreign DB/preset/payload/cancellation unit fixtures, NOT RUN. Full legacy Apply→Selection→encoded Export→volume probe report remains open; this binder is a prerequisite, not an all-true acceptance claim.


### 2026-10-12 — V32 complete legacy Apply encoded delivery runner
Added a pinned `legacy_apply` probe runner that composes physically verified preview and Apply, SQL-derived current Apply output/Selection binding, fresh native Selection, encoded Export with physical media/optional ZIP verification, owner intent redemption, exact final volume download and post-delivery source/preset recheck. Its pre-Apply descriptor fixes source, preset revision/config, credential-free key hashes, encode template excluding the unknown generated image ref, buckets, scratch root, pepper fingerprint and deadlines. After Apply, the exact image ref is inserted from independently verified output and the dynamic selection/export descriptor is hashed and checked by the existing executor. An all-true report is emitted only after encoded/volume byte identities match; partial or unknown POST outcomes do not retry. Orchestration refusal/unit fixtures authored NOT RUN. Read-side legacy probe observation validation, actual isolated native execution and full four-probe acceptance remain open; migrations NOT APPLIED.


### 2026-10-12 — V32 legacy report read-side closure
The private legacy probe report now retains its dynamic Selection/Export descriptor, Apply artifact identity, delivery kind and optional archive digest. The acceptance reader recomputes both static and dynamic descriptor hashes, checks replay identity, preset/key/EncodeParams template pins, Apply ticket and output collection, selection digest, encoded/single-or-ZIP volume relationship and exact downloaded volume SHA. The runner requires document API delivery to match the adopted Apply artifact and refuses missing ZIP proof or mismatched single-volume identity. Existing acceptance test fixtures were updated to carry a structurally valid legacy document; tampering scenarios authored NOT RUN. The private report serializer now accepts only finite fractional export values (needed for `matte_rgb`); NaN and infinity remain rejected. Live isolated restore execution and broader V20–V32 acceptance remain open; migrations NOT APPLIED.


### 2026-10-12 — V32 four-probe concrete acceptance installation
Added typed installations and a fixed-order dispatcher for legacy Apply, V2 workflow, purge-negative and revoke-negative runners. Before the first probe it validates the private report root, every fixture guard, all four canonical descriptors against independently supplied pins and absence of preexisting reports. Each stage rechecks its descriptor/pin and replay identity, forbids out-of-order or repeated calls, and passes the acceptance isolation/current-authority guard into its concrete runner. `run_installed_restore_acceptance` connects this dispatcher to the existing durable acceptance/report coordinator without writer cutover. Wrong-pin/order/change/fixture-guard unit cases authored NOT RUN. Operators still must supply actual isolated PostgreSQL/object-store/native TLS/model services, qualified fixtures and current-authority callbacks; this adapter is not live DR drill evidence. Migrations NOT APPLIED.

### 2026-10-12 — V32 purge-negative report read-side binding
The purge fixture descriptor now pins restore run/build/recovery/replay identity in addition to tenant, revision, object catalogue and API/S3 targets. The private report reader rehashes that descriptor and checks the physical baseline inventory digest/byte count, the same deletion job across completion and metadata absence, two all-bucket storage absence passes, and the formerly readable API/S3 target against the post-purge denials. Canonical report-only fixtures and tamper cases were authored; tests NOT RUN, migrations NOT APPLIED. This structural closure does not replace live isolated deletion, operator authority, or the remaining revoke-negative report verification.

### 2026-10-12 — V32 paired revoke-negative report binding
The private acceptance reader now recomputes the combined admission/running fixture pin, binds both nested descriptors to the restore replay, requires separate grants in the same tenant/project/actor, and verifies deterministic child/workflow/task identity. It checks the running attempt/fence against the cancelled no-result witness and held barrier, both incremented consent epochs/revisions and revoke receipts, a completed positive batch, and stable guarded freeze/batch denials after revocation. Report-only fixtures and tamper cases were authored; tests NOT RUN, migrations NOT APPLIED. This report closure is structural evidence only: actual isolated execution, complete physical cross-links and all V20–V32 module acceptance remain open.

The same reader now also binds the positive frozen recipe to the physical versioned object and authored document SHA, derives the exact freeze and batch request digests from pinned inputs, checks the guarded freeze catalogue, matches the prior readable document and later domain-specific access denial, and ties the successful batch ticket to its SQL adoption while requiring a distinct denied batch. Additional tamper scenarios were authored, not executed. The physical render result and installed-service drill still require deeper verification.

The positive batch's nested physical witness is now read-side bound to the adopted workflow's deterministic render task, attempt/fence, exact ImageResult child SHA catalogue, WorkingImage pixel identity, semantic canvas and sorted physical inventory digest/byte count. Empty array proof or missing parent proof refuses. Tamper fixtures for task, child SHA, array absence and inventory digest were authored NOT RUN. The underlying bytes still require live object-store replay, not report structure alone.

The ImageResult document in that witness must also hash to the exact canonical result-object SHA, and its child artifact IDs must be distinct from one another and from the result artifact. A separate altered-result-object fixture was authored NOT RUN.

The result object and each child now additionally require tenant-scoped keys, concrete object versions and bounded exact sizes; the canonical WorkingImage document must hash to its ImageResult ref, and result/child artifact identities must be nonzero UUIDs. Report-only substitutions for result size, foreign key and WorkingImage SHA were authored NOT RUN. Live physical object reads and installed service qualification remain separate.

### 2026-10-12 — V32 probe report prepublication body guard
The exclusive private report writer now runs the same kind-specific observation validator used by the independent reader against normalized canonical content before opening its pending file. A malformed or mismatched fixture/body therefore cannot leave an immutable final report that blocks a corrected probe. The reader still repeats validation after publication and on acceptance replay. Invalid-body/no-file and replaced-root fixtures were updated; tests NOT RUN. A post-publication authority failure remains a reconciliation event, not an automatic retry.

### 2026-10-12 — V32 acceptance report preflight before destructive probes
The four-probe dispatcher now refuses an existing acceptance report or pending acceptance publication before starting the first probe. It also treats dangling `.pending`/`.json` symlinks for any probe as occupied paths during preflight, instead of relying on `Path.exists()`. This prevents a full destructive drill from reaching a known exclusive-publication failure at the end. Existing-acceptance and dangling-symlink refusal fixtures were authored NOT RUN; native/operator installation and live drill remain open.

Each direct legacy, V2, purge and revoke probe runner now uses the same occupied-path semantics before its own first mutation, so bypassing the dispatcher cannot silently defer a dangling-symlink conflict until report publication. A direct purge-runner refusal fixture was added NOT RUN.

The dispatcher also enforces a separate purge tenant from every positive and revoke fixture before any probe, preventing deletion of a still-needed grant or source collection. Before each subsequent probe it rehashes all four independently pinned descriptors and rechecks the tenant separation, so a future fixture cannot drift into the purge scope between stages. A purge-alias refusal fixture was authored NOT RUN. This is an installation safety gate; no live DR drill or operator cutover is claimed.

### 2026-10-12 — V32 replay post-dispatch sent inventory
Forward-only01778 adds a publisher-only exact-database keyset reader for task outbox rows marked sent after the replay's database-clock boundary. A recovery-bound Go replay now checks the pre-dispatch pending digest, captures that DB clock before its first dispatch, drains with broker confirms and canonical mark-sent, then independently recomputes the same domain-separated envelope digest and count over newly sent rows before returning successful evidence. Failure returns no successful replay receipt and requires reconciliation; it does not reset sent records or retry uncertain publishes. The existing isolated DB/vhost integration fixture now checks the sent catalogue and same-count payload substitution after dispatch. Tests NOT RUN;01778 NOT APPLIED. Broker durability, delivery-history and full operator drill remain separate gates.

The integration fixture also checks that only `photo_publisher` can execute the new sent-page function and `photo_api` cannot. It requires the isolated restore database and vhost; it has not been run in the current code-first phase.

The Go replay evidence now carries package-private sent-inventory SHA and recovery/run/build binding. The bound path sets the sent SHA only after the post-dispatch SQL catalogue matches, then attaches recovery/run/build identity only after re-reading the pinned recovery report. `PersistRestoreReplayReceipt` rejects manually supplied count-only, diagnostic, foreign-run/build or unverified evidence before creating a file. Mechanical receipt unit fixtures were adapted and refusal cases authored NOT RUN; the wire receipt schema remains v1 and cannot authorize writers.

The proof also snapshots database, confirmed count and completion instant into package-private attested fields after the sent-inventory comparison. Changing exported count/time fields on returned evidence cannot later manufacture a different receipt; altered-count/time refusal fixtures were authored NOT RUN.

The recovery-bound replay now opens the new sent-page reader against an empty post-clock catalogue before the first broker dispatch. A missing01778 function, missing publisher grant or unexpectedly sent row therefore fails before a message is published. The same timestamp is retained for the full post-dispatch digest comparison; installed SQL/role integration remains NOT RUN.

The sent-page SQL authority accepts only the exact current database and a start clock within the last20 minutes, covering the15-minute replay deadline without exposing an unbounded historical sent catalogue. Isolated integration refusal cases for foreign database and old timestamp were authored NOT RUN.

The Go read-side replay inventory now additionally rejects any outbox destination outside the exact task publisher claim scope (`photo.commands.v1`, `photo.events.v1`) or payload above the publisher's 65,536-byte limit before dispatch. The canonical digest helper remains usable for the cross-language byte fixture; the installed SQL-page reader applies publishability to both pending and post-sent catalogues. A boundary unit matrix and an isolated DB/broker scenario with an independently digested same-count unclaimable row were authored NOT RUN. This prevents an unpublishable recovery catalogue from qualifying for partial broker replay.

The restore acceptance coordinator, four-probe dispatcher and acceptance-report publisher/reader now treat an explicit `False` from isolation, current-authority, active-run or installed-fixture callbacks as refusal, while preserving the established exception/`None` callback contract. This closes a path where a boolean-denial implementation could silently allow probe execution or publication. False-return refusal scenarios were authored for the coordinator, dispatcher and report publisher, NOT RUN. Live fixture installation and full drill remain open.

The four direct probe entrypoints (legacy Apply/export, V2 workflow, tenant purge and consent revoke) now apply the same explicit-`False` refusal in their own active/fixture guards, including the V2 report publication guard. Thus bypassing the four-probe dispatcher cannot turn a boolean fixture denial into a mutation or all-true report. Direct-entry refusal scenarios were authored NOT RUN; subordinate lower-level restore helpers still retain their existing exception-based callback contract.

Source review of the V30 public path confirms `CreateDelivery` already selects the persisted watermark protocol and `NewDeliveryExpansionJob` already constructs the mixed child router; those source connections still need deployment and physical lifecycle qualification. The direct watermark child processor, observer and SQL resolver now require a complete bounded expansion lease before resolver/SQL/object work, matching the existing runtime plan-store gate. Missing token/package/expiry/slots, impossible cursor and foreign parent no-resolver scenarios were authored NOT RUN. This closes a direct-entry gap without changing image-processing algorithms.

The private 01759 watermark source-query adapter now applies the same complete lease shape before beginning its short tenant transaction. Its existing exact SQL/argument whitelist remains intact; an incomplete package identity is newly refused before BeginTx. Query-adapter fixtures were updated to carry full leases and include the negative lease case, NOT RUN. The SQL page schema (`items`, `next`, versioned object fields) matches the Go decoder, with a nonempty final page followed by an explicit empty terminal page.

Forward-only01779 replaces the private V30 watermark derivative resource-profile function with strict JSON integer-number checks for canvas and resize dimensions. 01765 previously cast `"5"` to bigint while Go's Params decoder rejects a string; the mismatch could admit a SQL profile that the Go scheduler cannot reproduce. The replacement keeps the existing binary64 ties-to-even geometry and RAM/scratch/output formulas, owner and private ACL. Existing malformed-resize parity cases now target the corrected SQL; added string-canvas Go/SQL refusal fixtures. Tests NOT RUN;01779 NOT APPLIED. Numeric parity and the full watermark lifecycle still require the deferred integration phase.

The same01779 correction also requires Rec.2020 working-image `gamma24` encoding and orientation1, both required by Go `ParseWorkingImage` but omitted from the earlier SQL resource predicate. Paired malformed source-scalar fixtures now cover string canvas width, wrong color encoding and wrong orientation with updated source SHA/Params so refusal is attributable to content validation. Source review found the separate01768 placement formula and margin type guards already aligned with Go/worker; no placement migration was retained. These fixtures are authored, NOT RUN.

The private SQL profile now also enforces the same lowercase SHA-256 shape for source-canvas and ICC digests and the same bounded `space_id` pattern as Go `validCanvas`. Added Go/SQL negative source fixtures for all three with independently rehashed source and Params; tests NOT RUN. This is admission-shape parity, not a substitute for physical document and transform-chain verification.

V30 watermark worker compositing now unpremultiplies for every strictly positive combined alpha. The previous `1e-6` cutoff made very faint marks on a fully transparent source become black even though their straight RGB color is defined. Zero-coverage remains an exact no-op, and linear premultiplied area resampling is unchanged. Added a tiny-positive-alpha pixel-quality regression checking mark color, resulting alpha and source buffer ownership; test NOT RUN.

V32 private probe report publication and direct receipt reading now also reject an explicit `False` active-run callback. The writer checks before opening the private directory, before exclusive link publication and after readback; the reader passes a fail-closed wrapper into both private file reads. Added refusal fixtures for prepublication and direct read, NOT RUN. This complements the existing coordinator/runner boolean-denial guards and does not authorize writer cutover.

### 2026-10-12 — V31 signer cancellation boolean refusal

The shared signing task scope now rejects an explicit `False` from its own or an inherited activity callback, including native callbacks rebound on another thread. The deployed signer rejects that result before reading deployment files and preserves the refusal through its listener guard. Added scope, nested/foreign-thread callback and deployment pre-IO refusal fixtures. Tests/build NOT RUN; native SDK/HSM/trust qualification and full V31 acceptance remain open.

The standalone signing-challenge listener lifecycle now also refuses explicit `False` before binding its private TLS socket and from the live guard after startup. Added pre-bind and post-bind refusal fixtures; the deployed signer already supplies its fail-closed wrapper to this lifecycle. Tests NOT RUN.

### 2026-10-12 — V31 intermediate certificate signature verification

The deployed certificate-bound inspector now verifies the cryptographic signature of every child certificate against the immediately following issuer key after issuer CA/KeyUsage checks. A matching issuer/subject name with a forged signing key is refused before live authority is called. Added generated valid and forged two-certificate chain fixtures. Root trust, revocation, SDK and HSM qualification still require the separate mandatory live authority; tests/build NOT RUN.

### 2026-10-12 — V31 signed-file path identity at final verification

The adoption-side signed-file verifier now pins both source and signed file device/inode identities at open and rechecks their original paths after SDK, raster, manifest and second byte-hash verification. Replacement by byte-identical files at either path is refused; the old open descriptors alone can no longer produce a successful receipt for a different pathname target. Added source/signed path-swap fixtures. Tests/build NOT RUN; full native SDK/export acceptance remains open.

The signer worker's verified upload path now pins the signed file device/inode while hashing the SDK receipt, passes that identity through TaskArtifacts to the gateway, and requires the gateway's upload descriptor and post-response pathname to retain it. A byte-identical swap before the PUT is rejected before network I/O; the signed export adapter fixture checks identity propagation. The gateway's existing streamed SHA and returned artifact hash checks remain mandatory. Tests/build NOT RUN.

### 2026-10-12 — V31 signing adoption inventory ownership

The signing commit adapter now recomputes the canonical three-output inventory digest immediately before constructing the atomic SQL call and copies every inventory row into the JSON parameter. A caller mutation after physical preparation is refused, and a later mutation cannot change already constructed SQL arguments. Authored digest-mutation/argument-ownership fixtures. Tests/build NOT RUN; database and native signer acceptance remain open.

### 2026-10-12 — V31 pinned trust anchor verification

The deployed certificate inspector now checks the pinned trust bundle SHA and requires the validated certificate chain to terminate at an exact bundled anchor or at a certificate directly issued by a bundled CA with keyCertSign usage. This check runs before and after mandatory live native authority; a wrong root is refused before the callback. Explicitly pinned leaf certificates remain possible for private trust policies. Added generated wrong-root/pinned-leaf and valid/forged intermediate fixtures, and updated deployed composition fixture to carry a real pinned trust certificate. Online revocation, timestamp, native SDK/HSM and public trust qualification remain independent mandatory gates. Tests/build NOT RUN.

The same V31 inspector now enforces CA BasicConstraints path_length for in-chain issuers and out-of-chain pinned trust anchors. A root with path_length=0 cannot authorize an intermediate CA, while path_length=1 can; generated root/intermediate/leaf fixtures cover both branches. Tests/build NOT RUN.

### 2026-10-12 — V32 restore preflight boolean isolation refusal

Restore preflight now treats explicit `False` from the operator's isolation/writer-pause authority or activity callback as denial before backup reads, quarantine creation or sealing. The live deletion-history reader also checks activity before and after its snapshot, discarding a snapshot if the callback changes to `False`. Added no-side-effect preflight and post-snapshot refusal fixtures. Tests/build NOT RUN; actual isolated restore drill and writer cutover remain open.

The V32 recovery coordinator now uses one fail-closed activity/isolation guard before and between all recovery phases, passing wrapped callbacks into preflight, consent verification, key checks, lease/resource/scheduler/cleanup/intent/mail/outbox fencing, quarantine activation and final fence verification. Boolean denial before or after preflight cannot reach the next mutation. Added four phase-boundary refusal fixtures. Tests/build NOT RUN; full restore drill remains open.

### 2026-10-12 — V32 restore lock and SQL isolation boolean refusal

The dedicated PostgreSQL advisory-lock lifecycle now refuses explicit `False` activity or writer-pause checks before SQL, after acquisition and on every ownership recheck. The concrete SQL isolation guard likewise refuses explicit `False` from activity or external network/storage isolation before querying the restored database and at its later boundaries. Added four pre-SQL denial fixtures. Tests/build NOT RUN; actual isolated restore/concurrent lock qualification remains open.

### 2026-10-12 — V32 recovery report directory pin and boolean refusal

Recovery report publication now traverses every absolute directory component with held descriptors and O_NOFOLLOW before creating the private pending file, rather than reopening a previously resolved full path through replaceable ancestors. The activity guard refuses explicit `False` both before directory work and after the pending file is fsynced, prior to exclusive hard-link publication. Root-replacement fixture now triggers after the pending write; added two boolean-refusal fixtures asserting no final or pending artifact remains. Tests/build NOT RUN; full report read-side/restore acceptance remains open.

The V32 acceptance report publisher now reuses the pinned directory traversal after replay/probe revalidation. Replacing an ancestor with a symlink between the initial path check and private report publication is refused before creating a pending or final acceptance file. Added an ancestor-swap refusal case alongside existing exact-report/root-swap fixtures. Tests/build NOT RUN; report presence still cannot authorize writer cutover.

### 2026-10-12 — V32 shared private receipt read path pinning

The shared private-file reader used for restore key witnesses, replay, probe, quarantine and acceptance receipts now opens every ancestor through held O_NOFOLLOW directory descriptors, opens the leaf relative to its pinned parent, and repeats the no-symlink pathname check after reading. An ancestor replaced with a symlink between initial validation and open cannot redirect the read; explicit `False` activity is refused before any file descriptor is opened. Added ancestor-swap and pre-open refusal fixtures. Tests/build NOT RUN; live isolated restore evidence remains outstanding.

### 2026-10-12 — V32 key witness cancellation through cryptographic probe

The installed key/witness catalogue now refuses explicit `False` activity after private reads, during bounded entry parsing and before returning the AES-GCM probe. The probe itself repeats fail-closed activity checks during construction and immediately before and after decrypting each historical witness, so a revoked run cannot return a successful key-version result. Added post-read and pre/post-decrypt denial fixtures. Tests/build NOT RUN; actual KMS/old ciphertext qualification remains open.

### 2026-10-12 — V32 recovery/replay canonical file identity

The Python restore replay reader now requires the recovery report and Go publisher receipt to retain their actual run-scoped publication names (`<run>.json` and `<run>.replay.json`) before reading either private file, in addition to the existing independent SHA, schema, database, build and count bindings. A byte-identical receipt copied to an arbitrary name is refused; explicit `False` at the terminal activity boundary is also denied. Updated acceptance fixtures to use the Go writer's canonical filename and added wrong-name refusal. Tests/build NOT RUN; independent broker delivery history remains a separate gate.

### 2026-10-12 — V32 quarantine extras descriptor pinning

Quarantine activation now opens the potentially large extras.jsonl through pinned O_NOFOLLOW ancestor descriptors after safely reading its marker, rechecks the journal pathname before SQL commit, and uses fail-closed activity/writer-pause guards throughout. A root swapped to a symlink after marker verification is refused before the first SQL call. Added a swapped-root/no-SQL fixture. Tests/build NOT RUN; real isolated SQL activation and storage quarantine qualification remain open.

### 2026-10-12 — V32 physical quarantine publication race fence

Forward-only 01780 makes ready-artifact publication and quarantine activation acquire the same transaction advisory lock derived from the global bucket/key/version identity before their mutually exclusive checks. The trigger now requires READ COMMITTED so its post-lock lookup observes a concurrently committed quarantine; activation explicitly sets READ COMMITTED before its first SQL read and locks before querying ready artifacts. An unrelated hash collision merely serializes extra physical identities. Authored ordering and installed-function assertions. Migration NOT APPLIED; tests/build NOT RUN. Concurrent real-PostgreSQL activation/publication qualification and full isolated restore acceptance remain open.

Isolated PostgreSQL concurrency fixtures cover both lock orders: uncommitted quarantine makes competing ready publication wait and then refuse; uncommitted ready publication makes activation's physical lock wait and then exposes the committed ready row. Fixtures authored, NOT RUN; complete restore drill remains open.

The same 01780 migration also moves the reverse fence into a private `photo_fn_owner` quarantine-row trigger. Every direct quarantine insert or physical-identity update now takes the common advisory lock and rejects an existing ready artifact under READ COMMITTED, even if a caller bypasses the Python activator. The two SQL concurrency fixtures now drive the actual table triggers in both orders; the reverse fixture additionally requires the post-commit direct insert to fail. Tests/migration NOT RUN/APPLIED.

01780 also adds a partial ready-artifact index on `(bucket, object_key, object_version)` for the global reverse lookup. Without it, the trigger could hold each physical advisory lock across a full artifact-table scan. The existing quarantine physical index serves the opposite lookup. Installation assertion authored; migration remains NOT APPLIED and must be scheduled with writer pause because ordinary transactional index creation can briefly block writes.

Added isolated SQL refusal cases for both trigger paths under REPEATABLE READ. A stale transaction snapshot must never satisfy the post-lock absence checks. Cases authored, NOT RUN.

### 2026-10-12 — V32 final recovery fence boolean authority

The direct final-fence verifier now rejects explicit `False` from activity and external writer-pause/isolation callbacks before SQL, during each predicate scan and after its read-only transaction. Previously those booleans were discarded when the verifier was called without the recovery coordinator's wrapper. Authored pre-SQL and in-snapshot denial cases, NOT RUN. This does not qualify the signer production launcher or the complete isolated restore.

Seven direct restored-SQL mutators (compute leases, resources, cleanup, scheduler grants, expired download intents, mail and outbox) now share a fail-closed activity/isolation guard at their existing pre-SQL and in-transaction boundaries. The previous pattern called both callbacks but ignored an explicit boolean `False` unless the outer recovery coordinator had wrapped them. Added helper ordering and per-mutator no-SQL refusal cases; tests NOT RUN. The wider probe-only entrypoints still need a separate direct-call audit.

Four direct evidence readers now fail closed on boolean activity denial: exact-version object inventory, privileged database catalogue/quarantine snapshot, independent consent catalogue comparison, and append-only tenant deletion history. Every existing chunk/row/final boundary now uses the shared active guard. Authored pre-read, mid-read and post-read denial fixtures as applicable; tests NOT RUN. This prevents a direct reader from returning a successful evidence object after its caller has revoked the restore run.

The physical quarantine planner, durable quarantine journal and pending-outbox digest reader now use the same fail-closed active guard. The planner and outbox reader check before consuming the first iterator item; the journal checks before creating a pending directory, before and after appending an extra, and before final publication. A denied journal stage poisons the run so it cannot later seal after reactivation. Added no-consumption/no-directory and poisoned-stage fixtures, NOT RUN. Full isolated restore acceptance remains open.

The exact-version MinIO restore reader now rejects boolean activity denial before GET, after GET and before/after each bounded stream read, closing/releasing a response if denial occurs after network acquisition. The quarantine planner also rejects an explicit `False` from `stage_extra` instead of hashing an unacknowledged extra row into its sealed plan. Added transport-resource and stage-denial unit cases, NOT RUN.

The disk-sorted all-version MinIO catalogue and storage-verification composer now fail closed on boolean activity denial before listing, at each listed version, after spool commit, while streaming sorted rows and before returning combined evidence. SQLite temporary spool cleanup still runs on refusal; added pre-list/mid-list/post-list denial cases, NOT RUN. Complete all-bucket storage qualification remains outstanding.

Authored a direct storage-composer preflight case requiring explicit `False` to refuse before client access or spool creation. Tests NOT RUN.

The independent tenant-purge negative observers now reject explicit `False` from activity or fixture scope before any MinIO listing or SQL, and repeat those guards across their existing two-pass storage and tenant-table scans. This prevents a direct observer from producing absence evidence after the operator has denied its fixture. Added pre-I/O denial cases for both observers, NOT RUN; no destructive purge logic was changed.

The quarantine SQL fixture now exercises the actual artifact immutability rule separately from the ready-state trigger: changing `object_version` on an existing artifact must fail as immutable, while a separately inserted `deleting` artifact at a quarantined physical version must fail when promoted to `ready`. Added a direct quarantine-row UPDATE test that cannot retarget an extra version to an existing ready artifact. Fixtures authored, NOT RUN.

Source/schema review confirmed `artifacts.state='deleting'` is used by canonical GC and physical artifact content remains immutable. Added a cross-tenant reverse-fence fixture: because quarantine identity is global bucket/key/version, an extra-version insert must reject a ready artifact owned by any tenant. Authored only; isolated SQL test and 01780 migration NOT RUN/APPLIED.

No code path or normative lifecycle currently releases an activated restore quarantine row. 01780 therefore makes both quarantine run headers and physical versions append-only with private SECURITY DEFINER UPDATE/DELETE refusal triggers. This prevents post-activation changes to plan count, byte receipts, run ownership or physical identity. Direct mutation/refusal fixtures authored, NOT RUN; a future deliberate release requires a separately specified forward-only migration and storage proof.

The append-only fence also rejects `TRUNCATE` on either quarantine table with statement-level triggers; row-level UPDATE/DELETE triggers alone would not run for a table truncate. Installation and rollback-only direct-TRUNCATE refusal fixtures authored, NOT RUN. This is an operator-error guard, not a defense against a superuser intentionally disabling triggers or dropping tables.

The remaining direct restored-SQL key-version verifier now uses the same fail-closed activity/isolation guard before SQL, around every historical key probe and before returning evidence. Added no-SQL/no-probe explicit-denial fixtures. Tests NOT RUN; live old-ciphertext/KMS qualification remains open.

The direct HTTPS JSON and signed-S3 XML access observers now share a fail-closed fixture/activity guard before each request and before returning readable/denial evidence. Explicit boolean denial by either callback cannot send even the positive-control request. Added no-network denial modes to both existing probe matrices, NOT RUN. Live TLS/object-store/API restore qualification remains open.

The direct tenant purge lifecycle now uses the same guard before its irreversible authenticated DELETE request, after the response, before each completion poll, and around its exact-revision SQL preflight. Boolean fixture/activity denial cannot reach HTTP or SQL. Added no-request and no-SQL unit scenarios, NOT RUN; live tenant purge and two-pass storage/metadata acceptance remain outstanding.

The direct consent-revoke lifecycle and its independent baseline/committed SQL observer now reject boolean fixture/activity denial before HTTP or SQL and before returning the composed proof. The wrapper forwards its target-scope callback result into the observer instead of discarding `False`. Added no-request/no-SQL refusal modes to both existing matrices, NOT RUN. Actual consent revocation and downstream invalidation remain for the isolated restore drill.

The revoke-negative recipe-access observer and both running/cancelled render witnesses now apply the same direct fixture/activity guard before SQL and at their existing post-read boundaries. Explicit `False` cannot qualify a gated document denial or a task-cancellation witness. Added no-SQL/no-HTTP cases, NOT RUN; full concurrent revoke/Apply adoption proof remains open.

The three revoke-negative coordinator layers (bound access/revoke observation, running-render race lifecycle and freeze/batch runner) now also fail closed on explicit boolean installed-fixture/activity denial before their first child call and at repeated guards. Added no-child-effect denial modes to the existing sequence tests, NOT RUN. This closes callback propagation at the orchestrator boundary without claiming the live race has been qualified.

The direct freeze-negative HTTPS observer, its current-draft/idempotency SQL catalogue, and frozen-result physical reader now use the shared fail-closed fixture/activity guard. An explicit `False` is rejected before the first POST, SQL query or object-store read; repeated guards also reject authority loss during observation. Added no-I/O denial modes to their existing unit matrices, NOT RUN. This does not qualify the live freeze/revoke sequence or the deployed restore drill.

The render-batch refusal HTTP observer, its independent snapshot/no-adoption catalogue and physical child-result reader now reject explicit `False` fixture/activity callbacks. The adopted Apply output downloader also stops before its first API request on the same denial. Authored no-request/no-SQL/no-object-read unit cases, NOT RUN. Positive freeze/batch/Apply restore acceptance still requires the isolated live drill.

The canonical render-batch result SQL reader now also applies the same guard before its first query and after the snapshot. Added an explicit boolean-denial no-SQL unit case, NOT RUN. This keeps the physical child reader's upstream result observation behind the same authority boundary.

The positive render-batch baseline coordinator and its registered graph reader now refuse explicit `False` from installed-fixture/activity callbacks before the first child operation or SQL query. Added no-child/no-SQL cases, NOT RUN. This completes callback propagation across the authored one-frame positive batch path, while real scheduler/worker output and restore acceptance remain unqualified.

The pinned legacy Apply installation, one-item Apply lifecycle and its direct native POST observer now use the same fail-closed fixture/activity guard. Explicit `False` prevents the Apply request and prevents the lifecycle from entering SQL polling or physical/output download; repeated guards continue to fence authority changes. Added pre-I/O denial modes, NOT RUN. Full Apply restore qualification remains dependent on the isolated scheduler/worker and object-store drill.

The downstream legacy Apply registration and completed-result SQL readers now reject explicit boolean fixture/activity denial before their first query and at snapshot exit. The physical WorkingImage reader checks again before opening the exact object version, including when its upstream observer is replaced by a test double. Added no-SQL/no-object-read cases, NOT RUN. Nested array and quality readers remain separate qualification boundaries.

Nested legacy Apply array and quality readers now treat explicit `False` from activity checks as denial before SQL/object access, during streaming and before returning evidence. Physical readers pass a fail-closed wrapper into MinIO and report validation. Added direct array no-object-read and report refusal modes, NOT RUN; full nested physical qualification remains for the later isolated test phase.

Authored direct array/quality catalogue tests for activity denial before SQL and after the physical row has been read; neither reader may return evidence from a snapshot whose operator activity was withdrawn. Tests NOT RUN. These are unit boundary cases, not a replacement for the current-build Apply restore drill.

Extended the exact-version MinIO adapter fixture to withdraw activity immediately after a response body read, proving the stream cannot hand those bytes to its caller and still closes/releases the response. The legacy master-quality fixture also withdraws activity after semantic threshold recomputation and must not return a pass receipt. Tests NOT RUN; no production algorithm or quality threshold changed.

The V32 render-review pending/accepted SQL observers and one-shot native decision POST now reject explicit `False` from fixture/activity callbacks before their first query or HTTP request, with repeated guards around returned evidence. Added no-SQL/no-POST refusal modes to the existing review matrices, NOT RUN. Live human review and full render adoption acceptance remain unqualified.

The render-review decision coordinator and two-sided preview coordinator now apply the same fail-closed installed-fixture/activity guard before invoking any child. Explicit `False` cannot begin pending lookup, preview catalogue/download or the decision POST. Added no-child-effect denial modes, NOT RUN. Full current-build preview/decision lifecycle remains unverified until the later testing phase.

All three direct render-preview SQL catalogue readers (after, original asset before, and render before) and the signed-version HTTP downloader now reject explicit `False` fixture/activity callbacks before their first query or GET. Added no-SQL/no-network modes to each existing matrix, NOT RUN. Preview bytes, profile and physical version checks are unchanged; browser/native qualification remains open.

The direct render-preview byte verifier now rejects explicit `False` activity before API parsing, after manifest binding and around both image verification/decode passes. Added initial and post-manifest denial cases, NOT RUN. The image/ICC/metadata acceptance rules remain unchanged; this only prevents a withdrawn restore observation from returning preview evidence.

The reviewed-render terminal SQL observer and final output downloader now reject explicit `False` fixture/activity denial before their first query, catalogue recheck or GET. Added no-SQL/no-HTTP cases, NOT RUN. Exact reviewed candidate, signed-version link and physical SHA checks remain in place; full isolated end-to-end acceptance is still outstanding.

Source review found a concrete after-preview catalogue failure: its SQL referenced undefined alias `w`, and the supplied `accepted_revision`/candidate arguments were shifted against twelve placeholders. The query now joins the exact review workflow to `v.task_id`, then follows `r.v26_derivative_task_id` to that same render task; pending and accepted revision arguments match their predicates. Updated both SQL-boundary unit expectations, NOT RUN. The original asset and render before-side queries were inspected separately; this correction does not yet establish positive PostgreSQL lifecycle acceptance.

The nested render-array router, produced-array SQL catalogue and physical NPY reader now fail closed on explicit `False` fixture/activity callbacks before child dispatch, SQL or MinIO access and again before returning the assembled evidence. Added no-child/no-SQL/no-object-read modes. Tests NOT RUN; array geometry, alpha, mask and pixel-value validators remain unchanged.

The separate source-reuse RGB/alpha verifier now uses the same fail-closed fixture/activity guard before its lineage SQL and forwards that guard into exact-version NPY validation. Added no-SQL/no-physical-reader denial modes, NOT RUN. This covers both produced and original-array branches of the authored render-result physical proof without changing the original pixel data.

The registered parent-report reader now rejects explicit `False` fixture/activity callbacks through its single guard, which gates both SQL catalogue reads and MinIO streaming during bounded parent DAG traversal. Added no-object-read denial cases, NOT RUN. Report inheritance, review requirement and canonical bytes checks are unchanged.

The direct render-result document semantic verifier now treats explicit `False` activity as denial before each decode, after each canonical document check and before returning the assembled evidence. Added initial and final-boundary refusal cases, NOT RUN. This does not alter report status, mask, canvas or image-quality acceptance rules.

The isolated render worker barrier now maps an explicit `False` from the real executor context activity check to `TaskCancelled` before provider execution and records its cancelled witness. Added a boolean-cancellation path to the barrier unit matrix, NOT RUN. Production renderer dispatch and quality behavior are unchanged; the barrier remains an explicitly installed restore fixture adapter.

The restored original preview-source SQL reader, physical WorkingImage/array observer and installed ICC registry validator now reject explicit `False` activity before SQL, object-store access or profile resolution and again before returning evidence. Added pre-I/O and post-row denial modes, NOT RUN. No image pixels, color transforms or ICC acceptance rules were changed.

The nested original preview RGB/alpha catalogue now repeats the fail-closed activity guard before and after its read-only SQL snapshot; its exact-version array verifier forwards the guard through NPY streaming and rechecks activity before returning. Added no-SQL/no-physical-read and post-row refusal cases, NOT RUN. Existing producer/input ownership and array-value rules are unchanged.

The pinned preview installation and one-request preview lifecycle now reject explicit `False` installed-fixture/activity callbacks before source verification, SQL preflight or POST. Installation passes a shared guard through physical source and ICC validation and rechecks before the lifecycle handoff. Added no-child-effect denial cases, NOT RUN. This does not constitute live preview scheduler/worker acceptance.

The direct native legacy-preview POST, saved-request registration SQL observer and completed two-output SQL observer now reject explicit `False` fixture/activity callbacks before the first request/query and at their existing repeat checks. Added no-HTTP/no-SQL denial modes, NOT RUN. Idempotency, exact output roles and physical object pins remain unchanged.

The physical legacy-preview manifest and JPEG readers now apply a fail-closed fixture/activity guard before MinIO access, throughout streaming and before evidence return. Their standalone semantic validators reject activity withdrawal before parsing and after manifest/JPEG verification. The signed-output downloader likewise refuses explicit `False` before its first GET. Added direct refusal modes, NOT RUN; JPEG pixels, ICC and stripped-metadata requirements are unchanged.

The V32 selection-after-preview native POST and its coordinator now reject explicit `False` from the installed-fixture or activity callbacks before network or SQL access. The saved selection catalogue rejects explicit activity denial before and after its read-only snapshot. Added no-request/no-SQL cases for the direct and coordinator paths, NOT RUN. This does not establish a successful live Selection.Create and Apply adoption cycle.

The pinned selection-to-export coordinator now applies the same fail-closed authority guard before source-object observation, selection creation or export dispatch. Its saved selection receipt unit matrix also includes explicit activity denial before SQL. Added no-child-effect cases, NOT RUN. The export encoder, delivery download and live qualification still require the later full restore drill.

The direct legacy export-job POST and its one-job lifecycle now reject explicit `False` fixture/activity callbacks before their first HTTP request or source-object read. Added no-request and no-child-effect unit modes, NOT RUN. Ticket acceptance remains bounded to a queued job; it does not itself prove encoded export or delivery.

The export preflight SQL reader, canonical encoded-result reader and volume reader now refuse explicit `False` activity before SQL and after each read-only snapshot. The encoded-result reader checks before its nested registration observer as well. Preflight additionally binds the selected asset version to the collection item's asset and requires the collection revision to equal the saved snapshot source revision; previously those two predicates were absent from this second authority check. Added SQL-shape and no-SQL denial cases, NOT RUN. Positive PostgreSQL export lifecycle qualification remains open.

The saved export-job registration reader and direct encoded/ZIP physical verifiers now fail closed on explicit `False` activity before SQL, nested observation or object access, and at repeated stream/verification boundaries. Added no-SQL/no-observer refusal modes, NOT RUN. Production format, color profile, manifest and archive byte checks were preserved; actual physical export acceptance remains for the isolated test phase.

The archive manifest semantic/physical readers and export Params/memory proof readers now reject explicit `False` activity before parsing, SQL, MinIO observation and at their repeated stream/snapshot boundaries. Added no-catalogue/no-SQL refusal cases, NOT RUN. Exact-version archive manifest, canonical Params and resource-estimate rules remain unchanged; positive current-build qualification remains outstanding.

The owner download-intent POST and final volume-delivery coordinator now fail closed when fixture/activity callbacks explicitly return `False`, before any HTTP request or volume SQL observation. Added no-network/no-child-effect unit modes, NOT RUN. The live consumed-intent, signed-volume and owner-download proof remains part of the later end-to-end restore drill.

The intent creation coordinator, saved receipt catalogue and public redeem/object GET verifier now reject explicit `False` fixture/activity callbacks before their first SQL or HTTPS operation and at repeated proof boundaries. Added no-transaction/no-network/no-SQL refusal modes, NOT RUN. Exact token MAC, consumed flag, signed object version and byte SHA checks remain in force; live one-use delivery still needs isolated qualification.

The dynamic Apply-output-to-Selection binder now refuses explicit `False` fixture/activity callbacks before its first SQL snapshot and at repeated boundaries. Its collection revision range now includes the documented maximum safe integer (2^53−1), matching the downstream Selection target validation; previously that one valid boundary was rejected. Added no-SQL refusal modes, NOT RUN. Actual Apply/Selection/export execution and positive SQL authority remain unqualified.

The encoded-export result reader now rejects ASCII control characters and DEL in the saved delivery entry name, in addition to path separators, dot segments and byte length. This prevents a ready job with a newline-bearing name from being accepted into the ZIP/delivery proof. Added the control-name refusal and explicit maximum-safe-integer Selection binding fixture, NOT RUN. Positive current-build encoded export still needs physical qualification.

The pinned legacy preview/Apply coordinator and export installation now reject explicit `False` from installed-fixture/activity callbacks before preview dispatch or export lifecycle handoff. The purge baseline and post-purge observation coordinators apply the same fail-closed guard before MinIO, SQL or HTTPS child observers. Added no-child-effect unit modes for all four boundaries, NOT RUN. Live isolated acceptance and fresh current-build evidence remain open.

The pinned revoke batch and running-render revoke installations now reject explicit `False` fixture/activity callbacks before lifecycle dispatch. The read-only two-fixture independence check and restored-DB deletion-history reconciliation reject explicit inactivity before SQL and at their snapshot/status boundaries. Added no-dispatch/no-SQL modes, NOT RUN. These guard changes do not qualify a live concurrent revoke or tenant-deletion restore.

V31 signer execution now maps an explicit `False` from its task activity callback to `TaskCancelled` at every existing execution boundary, including before artifact reads, native SDK invocation, staging and final SignedResult publication. The same fail-closed guard is forwarded into the native bridge and signed-file receipt reader; receipt validation refuses inactivity before opening output bytes and during streaming. Added pre-open and post-signed-upload boolean-cancellation unit scenarios, NOT RUN. Native SDK/HSM/TSA/trust qualification and deployed provenance.sign.execute remain open.

The V31 C2PA SDK bridge now checks the active signing scope at every direct boundary and passes a bound task guard into file hashing, pixel decoding, container verification and seekable SDK streams. This catches explicit `False` during SDK work inside the bridge's cleanup scope, allowing it to remove its own newly created output instead of discovering cancellation only after the bridge returned. Added fake-SDK boolean cancellation and output-cleanup scenario, NOT RUN. This is local cancellation correctness, not native C2PA trust qualification.

The independent V31 adoption-side signed-file verifier now uses the active signing scope before opening source/signed files and at its terminal boundary, with the bound task guard propagated into both file hashes, raster fingerprints and embedded manifest verification. Added an explicit boolean cancellation case before operator/file work, NOT RUN. Actual native reader, trust, timestamp and revocation evidence remains required.

The seekable V31 SDK stream now independently treats explicit `False` from its callback as task inactivity before construction's first backing call and around every read/write/seek/tell/truncate callback. Cleanup via close remains available after denial. Added boolean-denial matrices for all callbacks and pre-backing initialization, NOT RUN. This protects direct stream use in addition to the bound signing-scope guard.

The shared V31 file hasher now rejects explicit `False` before seeking, immediately after seek/read and before returning a digest. A cancellation triggered inside a file read cannot yield a usable source or signed-output SHA. Added pre-I/O and post-read boolean-denial fixtures, NOT RUN. Native C2PA and deployment qualification remain open.

The V31 JPEG APP11 and PNG caBX manifest scanners now reject explicit `False` before and after their bounded reads, including the final EOF check, while retaining caller stream-position restoration on failure. Added cancellation triggered by the final read for both containers, NOT RUN. Binary store matching remains distinct from certificate/COSE trust qualification.

The V31 COSE signature verifier and physical JUMBF/SDK binding coordinator now fail closed on explicit `False` activity before CBOR/cryptographic work and before returning a verified claim. The coordinator forwards a guarded callback through nested parsers, checks around mutable Reader JSON/active-manifest calls and repeats the guard after the final equality check. Added no-reader/no-signature boolean-denial scenarios, NOT RUN. These do not replace independent certificate-chain and native trust acceptance.

The standalone V31 CBOR and JUMBF parsers now treat explicit `False` as cancellation before structural decoding, during bounded tree/box traversal and at the final return boundary. Added initial and terminal boolean-denial fixtures, NOT RUN. Deterministic CBOR ordering and JUMBF claim-byte preservation remain unchanged; positive native signing qualification is still open.

The standalone V31 assertion-inventory and lineage binder now rejects explicit `False` before reference processing, during each hash binding and after the final lineage comparison. Its nested CBOR decoder receives the same guarded callback. Added initial and terminal boolean-denial fixtures, NOT RUN. Physical assertion SHA and exact PhotoEditor lineage semantics are unchanged.

The offline V31 pinned SDK-wheel verifier now rejects explicit `False` activity before its first descriptor open, around each bounded wheel read and before returning its integrity receipt. Direct `_verify_open_wheel` calls share the same guard. Added pre-open and post-read descriptor-cleanup boolean-denial fixtures, NOT RUN. Wheel-byte integrity still does not attest a loaded native SDK or HSM.

The common V31 SDK Reader verifier now accepts a fail-closed activity callback and repeats it before/after native Reader methods, through bounded validation-result/manifest traversal and at the final receipt. The signing bridge, independent file verifier and physical binary binder forward their active guard instead of relying solely on ambient task scope. Added no-reader and cancellation-during-result-read boolean fixtures, NOT RUN. Trusted certificate, timestamp and revocation status rules remain unchanged.

The V31 offline signer deployment-file preflight now accepts an optional activity callback and runs the whole manifest/runtime-lock/certificate/file-pin sequence inside a signing task scope. Explicit `False` refuses before the first deployment read, while nested bounded readers retain the scope through their IO and terminal receipt. Added no-read boolean-denial fixture, NOT RUN. It still returns only file pins and cannot claim native SDK/HSM qualification.
V31 runtime-file descriptor traversal now checks signer activity before opening the root and immediately after opening root and leaf descriptors. A Boolean cancellation after the leaf open closes both leaf and directory descriptors before returning. Unit scenarios for denial before any open and cancellation after leaf open are authored, NOT RUN. Full native signer qualification and deployed provenance.sign.execute remain open.
V31 source reconciliation: the dedicated signer service already composes `signer_operations` through `build_signer_executor` and enforces exact `provenance.sign` registration. The earlier missing-runtime entry in the static audit is stale as a code-presence classification, but deployed native SDK/HSM/trust authority and execution evidence remain unproven. The signed-result receipt reader now checks task activity immediately after opening its file descriptor and closes that descriptor on Boolean cancellation; a regression scenario is authored, NOT RUN.
V31 native signer bridge now checks task activity immediately before and after source-file open and before output-file creation. A cancellation at either open boundary exits via ExitStack, closes the owned descriptor, and removes only the signer-created output inode. Added source-open and output-open Boolean-cancellation regression modes; authored but NOT RUN. The actual SDK/HSM/trust qualification and end-to-end deployed signer acceptance remain open.
V30 archive multipart Parts now re-reads the authoritative upload record after URL signing and refuses to return URLs if upload state left `uploading`, reservation token changed, or multipart session changed. This narrows the authorization-to-response race with Abort/completion/replacement; the object-store session cleanup remains authoritative after any later race. Added Abort-during-sign regression case, NOT RUN. Full V30 archive upload acceptance remains open.
V30 archive Begin recovery now binds a saved closed multipart receipt to the SQL reservation before reporting `uploading`. An ambiguous Bind result is reconciled against the durable reservation token, state and exact multipart ID; an unbound `reserved` row never yields an upload-ready response, and recovery never starts or aborts another session. Added bound-receipt and unbound-receipt unit scenarios, NOT RUN. Native SQL/storage acceptance remains open.
V30 Begin recovery contract reconciliation: migration 01605 already commits `reserved`→`uploading` when it returns a closed writer receipt. Service-side `Bind` is therefore an idempotent exact-ID fence for SQL recovery, while also refusing an alternate recovery adapter that returns an unbound receipt. Added a unit scenario modeling the SQL-committed recovery path without a second multipart create, NOT RUN.
V30 archive completion processor now independently re-freezes and checks claimed tenant, actor/session, deterministic upload/artifact/object key, canonical request body/SHA, nonzero lease token and multipart ID before renewal or physical Complete. This prevents a substituted claim from reaching object storage even if a future lease adapter omits SQLArchiveCompletionLeases validation. Added foreign-upload claim refusal scenario, NOT RUN; real DB/object-store acceptance remains open.
V30 lost-Complete observation now requires the exact multipart ID to be absent before, between and after two matching versioned object reads. If the session is visible on the final probe, recovery retains the open writer and returns no candidate version. Added late-live-session regression mode, NOT RUN. This tightens the observation window but does not replace the closed-writer receipt or exact-version SHA verification.
V30 object-store multipart completion now rejects NUL in an ETag at the storage adapter boundary, matching the archive completion intent/API contract. Added a direct adapter regression case so non-HTTP callers cannot forward that malformed receipt into S3 CompleteMultipartUpload; NOT RUN. Versioned completion, full archive SHA and DB publication still require deferred integration acceptance.
V30 photographer archive client now rejects remote HTTP part URLs before uploading ZIP bytes; HTTPS remains accepted, and HTTP is limited to localhost/loopback development URLs, aligned with objectstore.New public endpoint policy. Added remote-HTTP rejection and local loopback acceptance cases, NOT RUN. Full native upload, resume and server-storage integration acceptance remains open.
V30 server public part-URL envelope now rejects remote HTTP even when a substituted signing adapter returns it. HTTPS remains accepted and HTTP is restricted to loopback/localhost, matching objectstore.New and the photographer client. Added server envelope cases for remote refusal and local development acceptance, NOT RUN. Real signer authority and full archive transfer acceptance remain open.
V30 URL parity follow-up: photographer client local-HTTP acceptance now covers canonical IPv4 127/8 loopback hosts as Go's `net.IP.IsLoopback()` does, while remote HTTP stays denied. Added 127.0.0.2 regression case, NOT RUN. URL-scope parity still needs deferred client/server execution checks.
V30 browser multipart deployment now configures bucket CORS during privileged storage bootstrap using exact APP_ORIGIN/PORTAL_ORIGIN, PUT/GET/HEAD and exposed ETag, then reads back the installed policy. Origin parsing rejects wildcard, credentials, paths and production HTTP; duplicate app/portal origins are deduplicated. Added pure origin/readback unit scenarios, NOT RUN; real MinIO browser preflight/PUT/ETag and end-to-end archive acceptance remain open.
V30 storage CORS bootstrap readback now requires exactly GET/HEAD/PUT, wildcard allowed request headers and only exposed ETag for the exact configured origins; an extra method or exposed header is refused. Origin parser also rejects empty query/fragment suffixes such as `?` and `#`. Added negative unit scenarios, NOT RUN; live MinIO CORS/browser acceptance remains open.
V30 MinIO integration fixture authored for privileged bucket CORS bootstrap: on a disposable bucket it checks GET readback of the exact two browser origins and exposed ETag, then rotates to one origin and rejects stale-origin retention. The fixture is gated by PHOTO_TEST_MINIO_URL and is NOT RUN; actual browser OPTIONS/PUT/ETag and complete archive lifecycle still require the deferred test phase.
V30 DecideImport approval installation now refuses a draft service with a different DB or bucket, missing parsed object reader, or missing publication object port before approval source IO. Rejection-only batches retain their no-draft path. Added pure installation matrix for valid, split-bucket, split-DB and missing-ports cases, NOT RUN. Full review transaction/object-store/HTTP acceptance remains open.
V30 review publication dependency guard now runs for reject-only DecideImport batches and CloseImportReview as well as approvals. It refuses missing bucket/object publisher after replay recovery and before selection/SQL context lookup or physical publication; a previously committed idempotent replay remains readable without the publisher. Added pure reject/close dependency matrix, NOT RUN. Full review acceptance remains open.
V30 CreateDelivery preparation now refuses a shared or missing API/scheduler pgxpool in ordinary, selected and watermark paths before any publication/registration. Production startup still verifies actual SQL roles/ACL; pointer isolation is an additional service-level guard, not role attestation. Added pure pool-isolation unit cases, NOT RUN. Full delivery create/read/publish lifecycle and migrated SQL acceptance remain open.
V30 delivery pool isolation now has public-preparer unit scenarios for ordinary, selected and watermark paths: with otherwise non-nil dependency ports, a shared API/scheduler pool must yield DELIVERY_UNAVAILABLE before any port method is called. Tests authored, NOT RUN. Source review of Get/Publish found registered handlers and SQL CAS publication path; actual package lifecycle remains unverified until deferred migration/integration acceptance.
V30 CreateInterchange import activation now performs an authorized recovery read after an activation error. A committed ticket with lost acknowledgement is returned as replay; an uncommitted activation retains its original error, and denied recovery takes precedence. Unit scenarios authored, NOT RUN. Full migrated import/export lifecycle remains unverified.
V30 PrepareParse now refuses a shared API/scheduler pgxpool before pinning pending import state or publishing documents. The production builder already constructs separate role-checked pools; this closes direct-service installation bypass. Unit scenario authored, NOT RUN. Full migrated import registration and replay acceptance remain open.
V30 import ticket recovery now reads the completed idempotency ticket after current project ACL, before inspecting private pending state or the linked ZIP. A live matching ticket remains replayable if the source archive later expires; mismatched request hash, resource workflow, status or ticket body fail closed. Pending creation still requires fresh physical archive evidence. Added positive and substitution unit scenarios, NOT RUN. Migrated SQL/object-store acceptance remains open.
V30 completed import replay additionally verifies that the deterministic run is sealed and non-draft, the workflow matches the original project/snapshot/request policy, and the exact parse task exists. An orphaned or substituted ticket is refused before returning success. Unit fixture covers both committed and orphaned cases, NOT RUN. This refines the earlier archive-readiness note: physical archive readiness is required for pending preparation, while a committed ticket is proven by the sealed run and current ACL after source expiry.
V30 import committed-ticket replay now also requires the ready `task_params` backend document owned by the deterministic parse task, exact Params artifact, and stored import proof for the original project and bundle. This closes a sealed-run-only replay gap without depending on expired source bytes. Unit SQL-shape fixture authored, NOT RUN; migrated database acceptance is still pending.
