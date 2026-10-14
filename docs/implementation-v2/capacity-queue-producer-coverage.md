# V32: покрытие frozen queue_class

Статус: написанный код, NOT RUN. Не является разрешением на production rollout.
Класс задаётся доверенным producer до canonical config hash. Исторические snapshots не изменяются; наличие старого read/replay пути не делает старый запуск кандидатом V32.

| Producer | Новые configs | Проверка | Состояние |
|---|---|---|---|
| Session analysis V22 | batch | SQL begin 01641 | Написано, не проверено |
| Training V23 | training | SQL begin/recovery 01642, exact historical replay | Написано, не проверено |
| Capture V24 | capture | Go loader, SQL begin 01637 | Написано, не проверено |
| Lens V25 | batch | Go store, SQL begin 01638 | Написано, не проверено |
| Original composition V25 | batch | Go preflight, SQL begin 01639 | Написано, не проверено |
| Render V26 | interactive preview / batch bulk | Go store/recovery, SQL begin 01640 | Написано, не проверено |
| Semantic transfer V27 | batch | Generic Go shell, public SQL shell 01643 | Написано, не проверено |
| Semantic reference/target preparation V27 | batch, historical omission preserved | Go/SQL01658–01667 | Написано, не проверено |
| Semantic assembly V27 | Наследует batch исходного run | SQL00506/00530 добавляют task в тот же run | Runtime acceptance открыт |
| Semantic Apply V27 | kind=apply, legacy scheduler | SQL00589 | Coexistence acceptance открыт |
| Semantic replacements V27 | Инвентарь не завершён | Отдельные producer/recovery пути | Открыто |
| Relight initial preview V28 | interactive, historical omission preserved | Go transaction/compiler + SQL01649 | Написано, не проверено |
| Relight analysis V28 | interactive во внешнем run config; historical omission preserved | SQL outer producer01650, embedded plan unchanged | Написано, не проверено |
| Local portrait V28 | interactive, historical omission preserved | Go Begin/loader + SQL01651 | Написано, не проверено |
| Portrait/relight replacement V28 | Не покрыты полностью | Producer inventory | Открыто |
| Perspective primary/lines/multi/line detection V29 | interactive, historical omission preserved | SQL outer configs01652 | Написано, не проверено |
| Negative primary V29 | interactive, historical omission preserved | SQL01653 | Написано, не проверено |
| Perspective/multi preview и quality V29 | kind=preview, legacy scheduler | SQL01057/01080/01163/01187 | Не требуют V2 queue_class; coexistence acceptance открыт |
| Negative replacement V29 | interactive, historical omission preserved | SQL producer/reader/probe01654 | Написано, не проверено |
| Manual perspective replacement V29 | interactive, historical omission preserved | SQL producer/reader/probe01655 | Написано, не проверено |
| Auto perspective/detector replacement V29 | interactive, historical omission preserved | SQL producers/readers01656 | Написано, не проверено |
| Multi replacement V29 | interactive, historical omission preserved | SQL producer/reader/probe01657 | Написано, не проверено |
| Delivery root V30 | batch, historical omission preserved | Pending/Go begin + SQL01644 | Написано, не проверено |
| Delivery children V30 | batch для новых, historical omission preserved | Go config + SQL draft/activate/commit/receipt01645 | Написано, не проверено |
| Interchange import/parse V30 | batch, historical pending omission preserved | Go plan/pending + SQL01646 | Написано, не проверено |
| Interchange export V30 | batch, historical frozen omission preserved | Go frozen/recovery/config + SQL01647 | Написано, не проверено |
| Signing V31 | batch, historical pending omission preserved | Go pending/intent/config + SQL01648; execution_pool signer | Написано, не проверено; deployed signer открыт |

До включения PHOTO_V32_SCHEDULER_POLICY_SHA256 необходимо завершить инвентарь всех фактических INSERT runs и producer/recovery путей, проверить nonempty SQL fixtures, очереди и restart. Старые scheduler binaries останавливаются перед переключением: original picker не исключает V2. Legacy-only picker нового режима исключает все V2, поэтому отсутствие класса означает неназначаемый backlog, а не legacy fallback. Нельзя обходить это UPDATE immutable config/hash.

Дополнительно открыты: qualified timing binding вместо unknown cost, model affinity, heavy admission inflight, pagination полного eligible snapshot, lost-commit/coexistence acceptance, исторический rollout без переписывания immutable snapshots.

Delivery replay gap: BeginRun повторно компилирует точный root config; SQL сравнивает и полный config, и сохранённый SHA. Добавление queue_class требует сохранённого выбора класса в pending/replay либо отдельного совместимого loader. Простая вставка поля в producer не считается корректной реализацией historical replay. Nonempty legacy/V2 pool fixture написан в tests/integration/test_v32_capacity_queue_nonempty.py, NOT RUN.

Delivery root replay gap закрыт кодом01644: класс берётся из frozen pending; историческое отсутствие поля сохраняется. SQL/runtime acceptance не выполнен.

Initial relight SQL nonempty scope/replay/ACL coverage написано в tests/integration/test_v32_relight_initial_queue.py, NOT RUN. Analysis recovery требует согласования всех structural recompilers/full snapshot comparisons; пока открыто.

Analysis recovery gap уточнён:01650 меняет только authoritative SQL outer run config; встроенный plan и все structural recompilers сохраняют прежние bytes. Full runtime acceptance остаётся открытым.

Local portrait: queue-aware exact compiler/loader groundwork написан; producer остаётся прежним. SQL strict root/transactional class selection/repeated Begin открыты. Тесты NOT RUN.

Local portrait01651: transactional queue choice + strict SQL Begin написаны, new interactive/existing omission preserved. Предыдущее указание «producer остаётся прежним» superseded этим инкрементом. Full qualified SQL/concurrent Begin/recovery acceptance NOT RUN.

V27 detector preparation: batch, exact saved queue/historical keyed/no-key replay + shell guard01658 написаны, NOT RUN. Semantic apply kind=apply остаётся legacy. Остальные preparation/assembly/targets/replacements открыты.

V27 face preparation: batch и exact historical receipt propagation + Go/SQL guard01659 написаны, full native replay fixture расширен; NOT RUN. Остальные native producers открыты.

V27 skin preparation: batch и exact historical receipt propagation + Go/SQL guard01660 написаны; native replay fixture расширен, NOT RUN. Person/defect/targets/assembly открыты.

V27 person preparation: batch + exact historical receipt propagation + Go/SQL guard01661 написаны; person replay fixture расширен, NOT RUN. Defect/targets/assembly открыты.


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
