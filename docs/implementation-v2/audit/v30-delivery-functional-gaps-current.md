# Delivery: подтверждённые функциональные разрывы

### Source reconciliation — 2026-10-13

`backend/internal/workflow/service.go` now constructs the Delivery job and, when `signingFactory` is installed, calls `InstallDeliverySigningStage`. `delivery_signing_runtime.go` installs tenant-projected signing policy/resource handles into both the bounded signing processor and the signed assembly processor. Earlier notes below saying that production scheduler never installs the signer stage are historical and superseded. Do not recreate this installation. Remaining work is to qualify the current deployment against migrated SQL, physical signed-object closure, required-mode creation/publication and complete API/client flows. No tests, builds, validators or migrations were run for this source reconciliation.

Сверка исходников 2026-10-12. Проверки не запускались. Это дополнение к V30, не сокращение требований.

| Возможность | Текущее ограничение | Необходимая согласованная реализация |
|---|---|---|
| Required provenance | `create_intent.go`, `assembly.go`: явный 503 | Заморозить signing policy и qualified signer identity; создавать signing dependency для каждого обязательного результата; принимать только независимо проверенный подписанный artifact; повторить provenance gate при assembly и PublishDelivery. |
| Copyright/approved EXIF | `create_intent.go`, `create_catalog.go`, `assembly.go`, `delivery_encoded_verify.py`: strip-only | Добавить проверенный источник разрешённых metadata и frozen projection; передать через дочерний export; выполнить encoder projection; независимо проверить итоговые encoded bytes и manifest; запретить перенос неразрешённых полей. |
| Watermark | `child_request.go`: `parsed.Watermark != nil` отвергается | Привязать frozen watermark config и физический source к графу export, ресурсной оценке и qualification; выполнить rendering до encode; проверить result lineage и не терять watermark при retry/recovery. |

Нельзя закрыть эти разрывы удалением отказов: worker и result verifier сейчас подтверждают более узкий strip-only контракт. Сначала расширить структуры и зависимости, затем все компиляторы/SQL admission/worker/adoption/assembly/publication, написать unit и интеграционные сценарии. Запуск — на согласованном позднем этапе тестирования.

Минимальная acceptance матрица: каждый metadata mode; watermark с/без alpha; required/optional slots; required provenance success/expired/revoked/tampered; retry/idempotency; partial optional failure; human review acceptance; atomic publication; tenant isolation. Наличие маршрутов и подготовленных тестов не подтверждает выполнение этих требований.


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

Placement integration fixture authored for 30 Go/SQL anchor/geometry combinations, including .5 ties, tall-mark overflow, margins and unknown anchor. NOT RUN.


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


### 2026-10-12 — assembly coordinator lease preflight
RegisterPreparedAssembly now rejects structurally invalid expansion leases before invoking registration stores or publishing objects; live lease authority remains SQL-owned. Updated positive fixture with required package identity and authored six no-side-effect refusal cases (tenant/package/token/expiry/cursor/foreign parent). Tests NOT RUN; migrations NOT APPLIED; complete retention/replay lifecycle remains open.


### 2026-10-12 — assembly registration cancellation phases
Assembly registration coordinator now checks cancellation after successful store/publication phases before advancing or returning a receipt. Committed blocked/registered state remains replayable; cancellation never implies rollback of a completed phase. Authored five cancellation boundaries across registered/create/two renewals/register, checking no later store calls and no early object publication. Tests NOT RUN; full assembly SQL lifecycle remains open.


### 2026-10-12 — watermark API installation dependencies
Delivery API startup now includes exact watermark source/version-selection/retry-pin/pin-write ABIs in existing SECURITY DEFINER/owner/effective EXECUTE/PUBLIC checks. Extended source ACL integration fixture to cover source reader. Source confirms metadata policies and watermark dedicated paths exist; replaced obsolete registry unsupported-mode limitations with accurate unqualified-lifecycle status. Required provenance still explicitly unavailable per V30 qualified-V31 dependency. Tests NOT RUN; migrations NOT APPLIED; no completion credit.


### 2026-10-12 — watermark pending payload ownership
Both public watermark selection/source and create/prepare stages now reconstruct validated canonical owned pending bytes before asynchronous SQL/object operations, eliminating repeated reads of caller-owned pending buffers after IO. Existing full pending/pin fixture covers exact payload identity, caller mutation isolation, verified pin re-decode, malformed input and cancellation refusal. Prepare proof startup ABI already matches actual ten-argument function; no redundant dependency added. Tests NOT RUN; full lifecycle remains open.

## 2026-10-13 — ordinary continuation observer inventory boundary — NOT RUN

Current production composition already installs `DeliveryChildPipeline.ObserveChild`; earlier open-observer statements in this historical journal do not describe the current source. The verified-stage-to-next-task inventory helper now independently requires exactly the working image, derivative report and RGB roles plus optional alpha, with unique role and artifact identities. This keeps incomplete or aliased producer outputs out of frozen continuation inputs even if an upstream parser changes. Existing inventory unit scenarios were updated to use complete producer outputs and new malformed-set cases were authored. Tests and migrations were not run; complete V30 lifecycle and physical acceptance remain open.

## 2026-10-13 — ambiguous archive Begin classification — NOT RUN

The production archive Begin service now installs its existing read-only open-writer inspector. When a retry finds no closed receipt and `StartBegin` reports stale, it inspects the still-open writer under current editor/tenant authority and returns `ARCHIVE_BEGIN_RECONCILIATION_REQUIRED` (409) if one exists. It does not use an S3 listing as a successful Begin receipt, close the writer, start another multipart session, release quota or abort an upload. An open-vs-ordinary-stale unit scenario was authored, NOT RUN. The ambiguous in-flight Create outcome still needs a separately designed terminal proof/generation-key recovery protocol; this classification is not automatic reconciliation or V30 completion.

The photographer archive screen now distinguishes this 409 from a generic upload failure, keeps the selected file and durable attempt, and tells the user that operator reconciliation is required before retry. EN/RU/BG text and a DOM scenario were authored, NOT RUN. No automatic resolution or unsafe second multipart writer was added.

## 2026-10-13 — archive Begin generation namespace — NOT RUN

The new `objectstore.InterchangeGenerationKey` and canonical parser give distinct object keys to distinct Begin generations without expanding the legacy upload-key validator or changing the current production route. Identity and namespace refusal tests were authored, NOT RUN. The remaining ledger, migration, storage-operation wiring and terminal-proof recovery are specified in `v30-ambiguous-begin-recovery-design-2026-10-13.md`; this helper alone does not resolve an ambiguous Create or grant V30 completion credit.

The object-store adapter now has generation-scoped Create, signed-parts and exact multipart Abort operations. They derive the key only from three UUID identities, retain the single-attempt Create transport and existing parts-expiry policy, and refuse malformed IDs/cancelled calls before IO. Unit refusal scenarios are authored, NOT RUN. No production caller uses these methods yet: admission must first be bound to the durable generation ledger, and Complete/Verify/cleanup must use the same generation key before the Begin route changes.

Generation-scoped Complete, completed-version recovery and full-byte SHA verification now use the same key derivation and the existing V30 bounded transport primitives. Verification explicitly rejects a pending object from another generation or the legacy key. Identity, cross-generation and cancellation unit scenarios are authored, NOT RUN. The SQL ledger, completion absence proof, discovery/drain/purge integration and production route remain unwired; no end-to-end recovery claim follows from these transport methods.

Generation-scoped multipart discovery and drain are now authored. Discovery remains read-only and returns an observation, never a Begin receipt or terminal Create proof. Drain requires a known multipart ID, aborts only that generation's key, probes `NoSuchUpload`, then requires a complete bounded listing with no exact-key remainder. The legacy helpers retain their legacy-only key validation. Unit cases for empty/foreign/duplicate discovery, live/listed multipart and cancellation are authored, NOT RUN. SQL admission and terminal Create proof are still required before these methods can be used to release a generation or tenant metadata.

Migration 01796 now authors the generation ledger: immutable key identity, writer-token FK, one live generation per upload, state/receipt checks, owner-only RLS and DELETE privilege, plus an explicit tenant-finish purge allowlist entry and unresolved-generation fence. It is forward-only and NOT APPLIED. The table is deliberately not used by production yet; state-transition functions, legacy reconciliation, route switching and physical lifecycle verification remain open.

Migration 01797 now authors tenant/editor-scoped generation Start, successful Create receipt close and exact bind functions. Start refuses unresolved writers and unmapped legacy Begin receipts; finish stores the S3-returned multipart ID in the writer and generation in one transaction; bind updates the parent key/ID only from that matching closed receipt. It also guards the legacy bind/recovery functions from binding generation IDs to the legacy object key. Migration is NOT APPLIED and no SQL tests have run. Definite failure/unknown-result resolution, generation-key completion/cleanup SQL and production service wiring remain open.

A separate Go `SQLArchiveBeginGenerations` adapter now maps Start/Close/Bind to 01797 under authenticated tenant transactions. Its ticket carries exact tenant/upload/generation/writer/reservation/request hash/key identity; malformed ticket and substitution tests are authored, NOT RUN. The adapter is intentionally not yet installed in `ArchiveUploadBeginService`, because Parts/Complete/cleanup still assume the legacy object key and switching Begin alone would create an unusable upload.

Migration 01798 adds a private SQL predicate for the current archive storage key: a generation key is accepted only when a matching bound generation, exact multipart ID and closed Begin writer receipt exist; the legacy key is accepted only without a bound generation. The scoped lookup and parts authorizer now invoke it. Go lookup carries the SQL-returned storage key separately from the deterministic intent key, validates its tenant/upload/generation shape, and Parts compares the same key before and after signing. The permit dispatches to generation-scoped signing only when the key contains a matching generation; legacy-only signers cannot sign it. Unit key and signer-boundary cases are authored, NOT RUN. Queue, completion, cleanup and production Begin switching remain open; 01798 is NOT APPLIED.

The scheduler completion lease now retains the exact SQL-claimed storage key; writer tickets, Complete, lost-reply observation, immutable-version verification and final adoption all compare against it. Object-store completion/observation primitives accept canonical legacy or generation keys, while cleanup helpers remain separately scoped. Existing processor fixtures were updated with the legacy key and a generation-key completion fixture was authored, NOT RUN. SQL queue/claim and completion publication still need explicit generation-key authority checks before production Begin can switch.

Migration 01799 now inserts the 01798 exact storage-key predicate into queue, ordinary/recovery claim, writer start/recovery/adoption and final completion publication. The definition patches refuse unexpected function bodies. A migrated-DB integration fixture checks owner/SECDEF/role ACLs for new entry points and presence of the key fence across all seven completion paths. NOT RUN/NOT APPLIED; physical generation completion, cleanup and tenant purge remain unqualified.

Object storage now has `PurgeInterchangeGeneration`, which derives the exact generation key, removes its incomplete upload and versions, then requires a complete exact-key multipart listing. Cancellation, failed purge, listing failure and remaining-upload refusal cases are authored, NOT RUN. The method is not installed in scheduler cleanup: a SQL cleanup lease must first prove the Create attempt terminal, all writers and URLs fenced, and no retained artifact. Parent cleanup still assumes one legacy key, so changing only its key check would be unsafe.

Migration 01800 now authors a separate generation cleanup lease for terminal successful Create receipts. Claim requires the parent in delayed abort, expired part authority, no open writer, no retained artifact and a closed matching Begin receipt. Authorize/Renew/Finish recheck those facts; `creating` cannot be claimed. Existing parent cleanup claim and authorization now refuse any generation not marked physically purged. SQL ACL/source fixtures are authored, NOT RUN; 01800 NOT APPLIED. The scheduler generation cleanup processor and parent handling of its bound generation key remain open, so the physical object-store purge method is still not composed.

The scheduler now composes `ArchiveGenerationCleanupProcessor` ahead of legacy archive cleanup after verifying all generation-cleanup function privileges at installation. The processor renews its lease, reauthorizes around exact-ID drain and exact-key purge, joins the heartbeat, and finishes SQL only after both physical operations. Unit cases for ordering, cancellation, failed authority/drain/purge and lease refusal are authored, NOT RUN. Parent cleanup still has a generation key in its upload row after the child is purged; its legacy-only key validation must be reconciled before this is a complete abort lifecycle. Unknown `creating` generations remain intentionally blocked pending terminal Create proof.

Migration 01801 now lets parent cleanup authorize a generation key only after the matching generation is `aborted` with `purged_at`, matching multipart ID and no other unresolved generation. The Go parent lease, quiescence check, exact-ID drain and purge route that validated key; legacy keys retain their existing path. The scheduler requires the new private SQL predicate at installation. Parent-generation and cross-key refusal fixtures are authored, NOT RUN; 01801 NOT APPLIED. This closes the known parent-key shape gap for terminal successful Create, but unknown Create recovery and real migrated/physical acceptance remain open.

Migration 01802 and `SQLArchiveBeginGenerations.Read` now expose a tenant/editor-authorized read of the one live Begin generation for idempotent retry. SQL checks creator/session/reservation/request hash and matching writer state; Go validates canonical generation key and state/receipt shape. A `creating` snapshot remains an unresolved Create, while `created`/`bound` carries the stored multipart ID. Read ACL fixture authored, NOT RUN; migration NOT APPLIED. The production Begin service has not yet been switched to this read/Start/Close/Bind path.

The production archive Begin service now uses the generation Read/Start/Create/Close/Bind path after first recovering any legacy closed receipt. It never repeats Create for `creating`; it rebinds a saved `created` receipt and reads the durable reservation after an ambiguous Bind response. Main startup requires the four new API ABIs. Unit scenarios for new, replay, unknown Create, lost Close and lost Bind replies are authored, NOT RUN; migrations NOT APPLIED. A known remaining edge is cancellation after a successfully committed Start but before S3 dispatch: the open generation needs an explicit `not-dispatched` terminal transition, otherwise it stays blocked. This is next work, not completion evidence.

The Create transport now reports a typed `ErrInterchangeCreateNotDispatched` only when its call-local tracked POST never reached the guarded RoundTripper. A sent POST is marked before base transport, so any later timeout/error stays unknown. Migration 01803 closes the generation writer and records `aborted/not-dispatched` without S3 cleanup only for that proven case; the Begin service invokes it after cancellation before IO or a typed no-dispatch return. Startup requires the new ABI. Transport-boundary and Begin refusal/retry tests authored, NOT RUN; 01803 NOT APPLIED. The larger unknown *dispatched* Create terminal-proof protocol remains unresolved, so V30 is not complete.

Source recheck confirmed the effective 01605 SQL returns no closed receipt while a Begin writer is open and refuses another `start_v30_archive_begin`; the new 409 path is therefore reachable. An HTTP unit case was authored for exact 409/code propagation without exposing multipart ID or ticket token, NOT RUN. The public API contract now names this state explicitly.

Archive status now classifies the persisted Begin generation through the existing tenant/editor-authorized SQL reader and publishes `begin_recovery` (`none`, `reconciliation_required`, `receipt_pending_bind`, `resolved`). The HTTP boundary, contract schemas, client decoder and archive screen handle this field; a unit classification matrix and decoder refusals were authored, NOT RUN. This status route still requires a successful Begin receipt, so it does not yet let a client with an ambiguous Create and no receipt discover the state after restart. A separate read-only attempt lookup by durable idempotency key remains required. No tests/builds/validators/migrations were run or applied.

The separate read-only attempt-status route is now authored. It derives the upload ID from tenant/actor/saved idempotency key, checks the original frozen body and key against the scoped SQL lookup, then reads generation state; it does not call Reserve or object storage. The client derives the same UUIDv5, verifies the scoped response, and exposes an explicit Check button when a saved attempt exists without a Begin receipt. The check holds the journal execution lock and rereads the attempt after network IO. API/catalogue and unit refusal scenarios were authored, NOT RUN; OpenAPI was edited directly and schema/route parity is NOT VALIDATED. Legacy pre-generation open writers are not classified by the generation reader. Most importantly, a dispatched Create whose reply was lost still lacks terminal proof and remains reconciliation_required. No full V30 completion claim.

Migration 01804 now exposes an owner-only, API-executable, tenant/project/editor-scoped SQL predicate for open legacy Begin writers. Status reads it when no live generation was found, so pre-generation ambiguous writers also report `reconciliation_required` instead of `none`. Production startup checks its function ABI and a role/SECDEF fixture plus unit classification cases were authored. Tests/builds/validators NOT RUN and migration NOT APPLIED. This reports an unresolved state; it does not prove Create absent or release writer/cleanup fences.

The no-receipt client status path now independently recomputes the deterministic upload UUID and canonical request SHA inside the journal execution lock before accepting an API result; it does not rely solely on the OwnerAPI decoder. The archive screen keeps receipt-gated Abort, Reset and Ready handoff hidden when only an attempt intent is stored. A mounted DOM case authors the restart/check/no-unsafe-action scenario. Server and client status validators now refuse `resolved` on a still-reserved parent, and unit classification fixtures cover bound→uploading. NOT RUN; full native/browser/backend acceptance remains open.

Migration 01805 patches the tenant-serialized Reserve transaction to reject a different idempotency key for the same actor/project and exact request or content hash/size while a prior reservation is live or its Begin writer/generation is unresolved. Go maps the new outcome to `ARCHIVE_BEGIN_RECONCILIATION_REQUIRED` 409; startup checks the installed fence marker. This closes the new-key bypass caused by deterministic upload IDs including the key. Unit outcome and SQL source fixtures are authored, NOT RUN/NOT APPLIED. It conservatively blocks identical bytes under a different filename until the prior attempt binds or expires without an open writer. It is not terminal proof for a dispatched Create.

An isolated rollback-only PostgreSQL lifecycle fixture now authors actual Reserve transitions for same-key replay, second-key refusal before Create dispatch, refusal with an open generation, refusal after a proven no-dispatch close while the parent remains live, and admission only after that parent expires without an open writer. The changed filename keeps the content-hash/size fence in scope. Fixture NOT RUN; current migration behavior and concurrency under two database sessions remain unverified until the agreed testing phase.

Migration 01806 adds the same cross-key fence at both generation and legacy Begin Start. Both now lock the tenant row before the upload, matching Reserve lock order; under that lock an older live sibling wins, and any sibling with an unresolved Begin writer/generation blocks Start regardless of age. This covers duplicate reservations that predate 01805. Startup checks both patched definitions. The rollback-only SQL fixture now inserts such a preexisting sibling, checks newer Start refusal, older generation Start, and subsequent generation/legacy Start refusal. Tests NOT RUN, migrations NOT APPLIED; a real two-session race still needs execution during verification.

Migration 01807 adds an API-only read-only cross-key status predicate for preexisting duplicate reservations. A generation or legacy Start refusal classified by this exact actor/project/reservation scope now returns `ARCHIVE_BEGIN_RECONCILIATION_REQUIRED` instead of the misleading generic expired-upload response. SQL reveals only a boolean, never the sibling upload/writer identity. Production startup requires the ABI; unit Start-refusal and rollback-only SQL winner/loser classification fixtures are authored, NOT RUN/NOT APPLIED. The diagnostic does not close an unknown Create or permit cleanup.

Migration 01808 strengthens irreversible deletion: `aborted` generations now require `purged_at` for new row updates, and tenant finish, parent cleanup claim/authorization/key selection plus idempotent parent Finish all refuse any child that is not both aborted and physically marked purged. The CHECK is `NOT VALID` so an existing malformed terminal row is retained and blocked rather than silently rewritten. Source/constraint installation checks were authored, NOT RUN/NOT APPLIED; actual migrated malformed-row and physical deletion acceptance remain for the later test phase.

Migration 01809 adds the reciprocal invariant: a generation with `purged_at` must be `aborted`. It is `NOT VALID` to retain pre-existing inconsistent rows for explicit repair while rejecting new writes, and the SQL installation fixture checks both purge-marker constraints. Tests NOT RUN; 01809 NOT APPLIED. Dispatched Create without a durable receipt remains unresolved and V30 acceptance is still open.

Migration 01810 and TenantCleaner now settle generation purge evidence after the configured tenant storage buckets report an empty prefix. This repairs the deleting-tenant deadlock: ordinary generation cleanup only accepts active tenants, while tenant finish requires physically purged generations. The maintenance-only transition requires the live deletion lease, deleting tenant, no open writer, and an exact closed S3 Create receipt for every non-aborted generation; unknown dispatched Create still refuses settlement. TenantCleaner refuses Finish when settlement fails. Startup ABI, unit refusal and SQL authority fixtures authored, NOT RUN; 01810 NOT APPLIED. Physical multi-bucket and race acceptance remain for the later test phase.

The maintenance startup now verifies the settlement function's owner, SECURITY DEFINER, fixed search path, effective EXECUTE grant and absence of PUBLIC EXECUTE before running any cleaner. TenantCleaner gives settlement its own bounded context and checks cancellation before opening a separate Finish context, so a slow settlement cannot silently consume the irreversible commit deadline. These source changes and associated tests are NOT RUN.

The tenant purge settlement now marks at most 256 receipt-proven generations per SQL call. It returns `more` while active generations remain; TenantCleaner checkpoints and retries the normal bounded tenant purge/empty-prefix observation before the next batch. `blocked` still refuses unknown Create or open writers, and only `complete` permits tenant metadata Finish. The bounded-path unit scenario and SQL installation assertion are authored, NOT RUN; migration 01810 remains NOT APPLIED.

Archive Begin now recovers a lost SQL Close reply in the same request by reading the generation under original tenant/editor/reservation authority. An exact persisted Create receipt proceeds to Bind without a second S3 Create; if still `creating`, it retries only the idempotent SQL Close with the same multipart ID. A substituted receipt refuses. Lost-reply, transient-close and unresolved-close unit scenarios authored, NOT RUN. Process crash before a durable Close and dispatched Create with no returned ID remain unresolved; this is not terminal proof or full V30 acceptance.

The first Begin call now classifies a dispatched S3 Create error as `ARCHIVE_BEGIN_RECONCILIATION_REQUIRED` 409 immediately, matching the persisted `creating` state and the existing read-only attempt-status UX. A proven not-dispatched error still takes the separate SQL close path and is not mislabeled unknown. Unit classification scenarios authored, NOT RUN. This improves public recovery guidance without treating a timeout or empty listing as terminal proof.

V30 Delivery create catalogue decoding now bounds the SQL JSON result to 8 MiB and rejects unknown top-level/nested preset fields and trailing JSON before selecting ordinary or watermark preparation. Existing exact snapshot, preset ID/order/revision/config SHA and metadata policy checks remain. Unit refusal scenarios authored, NOT RUN; this is a closed authority boundary improvement, not completion of required provenance or full Delivery acceptance.

The V30/V31 bridge now has a pure `CompileSigningDependencies` preflight: each ready slot produces one deterministic child signing key bound to parent, ordinal and preset, exact export ID/ref and explicit signing-policy ID. Omitted optional slots produce none; required omission, pending/failed state, aliasing and malformed version/export refs refuse. Unit ownership/replay/refusal scenarios authored, NOT RUN. This helper does not grant policy qualification, launch signer tasks or verify signed bytes; the public `required` 503 gate remains until those dependencies and assembly/publication checks are installed.

`PrepareSigningDependencies` now invokes the existing V31 read-only signer preparation for every frozen Delivery dependency with a tenant-projected registry. It rechecks live source/project/export ref and policy ID after V31 source/lineage/operator validation, and refuses any mismatch before exposing a plan. Replay identity, foreign project/export and invalid preflight scenarios authored, NOT RUN. It still does not admit signer tasks, persist dependency edges, verify signed results or enable `provenance=required`.

Migration 01811 extends new V31 `provenance_manifests` rows with immutable adopted task/result IDs and the physical `validation_sha` supplied to the atomic signer adoption call. Historical rows retain all three fields NULL and cannot be mistaken for receipt-bearing rows. The migration patches the existing SECURITY DEFINER adoption insert with a definition guard and updates the immutable-content trigger; SQL installation fixture authored. NOT RUN/NOT APPLIED. Delivery still needs scoped signed-result reader, reauthorization, content binding and assembly/publication barriers before required provenance is available.

Migration 01812 adds an API-only signed receipt reader scoped to current tenant/editor/project, exact ready unsigned export artifact, published signing policy, canonical succeeded V31 task/result and ready signed/manifest artifacts produced by the same attempt/fence. Legacy rows without 01811 receipt fields return no result. Go `ReadSignedDependencyReceipt` calls it inside the tenant transaction and validates disjoint refs/digests; unit and SQL role/source fixtures authored. NOT RUN/NOT APPLIED. This yields an authority pointer, not independent object-byte or current operator verification; Delivery assembly/publication still cannot enable required provenance.

The 01812 reader now also returns exact signed/manifest bucket, task-attempt object keys, versions, sizes and media types. `VerifySignedDependencyObjects` streams both immutable versions to EOF with independent SHA/size checks and cancellation, using the existing object-store Open contract; it refuses foreign task paths, role/media aliasing and partial reads. Physical byte substitution, foreign key and cancellation unit scenarios authored, NOT RUN. This verifies retained bytes, while cryptographic C2PA verification remains the qualified V31 adoption responsibility; Delivery still needs durable dependency edges and assembly/publication gates.

`VerifySigningDependencyCurrent` composes exact parent-derived signer key/task identity, V31 frozen params, scoped adopted receipt, current tenant-projected operator policy, full signed/manifest byte verification, then repeats receipt and operator checks after object IO. Changed receipt, foreign parent and exact replay scenarios authored, NOT RUN. The production wrapper uses the 01812 tenant-scoped reader; this remains pre-assembly evidence only and does not bypass the public required-provenance gate.

`CompileSignedAssemblyClosure` now produces bounded canonical bytes and SHA for an exact one-to-one set of ready Delivery signing dependencies and adopted V31 receipts. It binds parent, policy, ordinal/preset, unsigned export, deterministic signing key, adopted task/result, signed and manifest refs and validation SHA; missing, duplicate, aliased and substituted entries refuse. Deterministic replay and refusal unit cases authored, NOT RUN. This is a pure proposed admission input, not SQL authority: durable dependency edges, atomic assembly admission with current receipt/policy rechecks, publication, public `required` enablement and physical tests remain open.

The closure now also pins tenant/actor and derives each expected V31 workflow/task ID from that scope and the deterministic child key. `MatchSignedAssemblyClosure` strictly decodes saved bytes and accepts only the exact canonical recompile from current dependencies and receipts. Foreign task/tenant and trailing-body cases authored, NOT RUN. No durable SQL attachment or runtime assembly use is claimed yet.

`CompileSignedAssemblyClosureForSlots` derives the entire expected signer set from frozen Delivery slots and refuses a missing ready export, changed export, duplicate dependency or required omission before compiling a closure. The matching reader compares saved bytes to that full-matrix recompile. Complete/reordered/omitted and refusal unit cases authored, NOT RUN. SQL retention and atomic admission remain open; the public required mode stays unavailable.

Migration 01813 now authors a private RLS-protected immutable signing-closure table and scheduler-only retention function. Under an exact active expansion lease, it compares all ready Delivery items with ordered closure entries, current export artifacts, canonical successful V31 signer tasks/results, adopted provenance rows and exact signed/manifest attempt objects; replay requires identical bytes. The tenant-purge allowlist places this child table before delivery packages. `RetainVerifiedSigningClosure` compiles the complete frozen slot matrix and calls the retention function; accepted/stale/missing-receipt/cancel unit cases are authored. Tests NOT RUN, migration NOT APPLIED. This persists evidence only: assembly activation/publication and public `required` remain gated until an atomic admission and runtime path recheck current authority and physical bytes.

Migration 01814 adds scheduler-only lease-fenced read-back of the retained signing closure, checking active tenant/run/package, exact parent, stored content SHA and current signing-policy publication. `ReadRetainedSigningClosure` compares those bytes to a fresh complete-slot recompile from current dependency receipts, refusing drift or an unknown lease. Unit read-back/replay/missing/tamper tests authored, NOT RUN; 01814 NOT APPLIED. Read-back is restart evidence, not activation or publication authority. Required-provenance runtime orchestration and atomic final admission remain open.

`SignedClosureCoordinator` now collects the exact signer set from frozen Delivery slots, verifies each V31 intent against live tenant-scoped receipt/operator policy and streams signed/manifest exact-version bytes through the production verifier before retention or restart read-back. Any missing/mismatched signer stops the entire set before SQL retention. Collector success/missing/foreign-task unit scenarios authored, NOT RUN. The coordinator is not yet installed in the Delivery scheduler; signer launch, atomic assembly admission, publication and `required` public enablement remain open.

The migrated-DB Delivery storage fixture now includes `delivery_signing_closures` in direct runtime-read denial and forced-RLS checks, and checks scheduler-only function EXECUTE, SECURITY DEFINER owner, absence of PUBLIC/API/worker EXECUTE, and the explicit `photo_fn_owner` DELETE privilege required by tenant purge. This fixture is authored, NOT RUN; migrations 01813–01814 remain NOT APPLIED. Current `DeliveryAssemblyPipeline` still follows the `provenance=off` assembly path, so the signed coordinator is intentionally not yet installed there.

`StartDeliverySigningDependencies` now composes exact frozen ready slots with the installed V31 `SignExport` admission using deterministic per-slot idempotency keys and validates returned queued workflow/run tickets before returning any set. Partial launches retry under the same keys; optional omitted slots launch nothing. Replay/partial failure/foreign ticket/unready matrix unit cases authored, NOT RUN. This currently requires a live authorized editor session and is not a scheduler delegation mechanism; automatic background launch, completion observation, atomic assembly admission and required-provenance publication remain open.

Migration 01815 now authors a scheduler-only, parent-lease-fenced status reader for one exact ready Delivery slot. It derives the signer key/workflow/run/task from immutable parent `created_by`, distinguishes not-started, pending, failed, cancelled, succeeded-but-unverified and SQL-adopted states, and refuses a foreign workflow shape. Go `ReadDeliverySignerStatus` checks returned export/task identity and state vocabulary; unit and migrated-DB ACL/fabricated-lease cases are authored. Tests NOT RUN; 01815 NOT APPLIED. `adopted` remains metadata-only and must pass current V31 receipt/operator/physical verification before assembly. Automatic delegated signer launch and final signed assembly remain open.

The signed closure coordinator now checks this leased status before any V31 object reads: not-started/pending/succeeded-unverified defer, failed/cancelled refuse, and only SQL-adopted continues to live policy/receipt and exact-version byte verification. State gate unit cases authored, NOT RUN. This still does not make an editor session a durable scheduler delegation or enable public `required`.

`ObserveDeliverySigningSet` now scans the complete frozen ready-slot set in deterministic order, renews the parent lease every 50 statuses and reports failed/pending/adopted for the whole matrix; optional omitted slots are excluded. The signed closure coordinator invokes this batch gate before any V31 object stream, then independently verifies each adopted receipt and exact object version. Unit state/renewal/refusal cases authored, NOT RUN. This is still observation only; signer dispatch and required-mode assembly/publication are not installed.

The V31 status and retained-closure SQL now require the signer run's frozen `export_id`, project and provenance snapshot to match the Delivery item before classifying failures or accepting adopted evidence. This rejects a reused signer key bound to a different export rather than letting it fail or satisfy the parent slot. The Delivery policy contract now includes frozen `signing_policy_id`/`signing_policy_sha` only for `provenance=required`, and rejects a hidden signing choice under `off`. Go create/freeze validation, Python worker policy, JSON schema and source builder were updated together; unit cases authored, NOT RUN. Valid required mode remains 503 until automatic qualified child admission, full signed assembly and publication are implemented.

The photographer Delivery create request/journal freeze now accepts an owned ID/SHA signing choice only when `provenance=required`, rejects partial/null/invalid or hidden `off` values, and preserves existing `off` payload bytes. Client unit cases authored, NOT RUN. The mounted creation screen still offers `off` only while the server explicitly returns 503 for valid required requests; no misleading public feature toggle is exposed.

`DeliverySigningChoice` now derives the V31 policy ID/SHA only from the frozen Delivery input. The live editor signing launcher, V31 intent preflight and signed-closure coordinator use that binding instead of an independent caller-supplied policy argument; preflight also compares the resolved V31 policy content SHA to the frozen choice. Off-mode, changed-SHA and exact-choice unit cases authored, NOT RUN. The frozen input is still unavailable from public required-mode creation until qualified runtime admission is implemented.

The V30 signed receipt reader, signer status and closure retention now require the V31 task's exact `SigningPolicy` input artifact ID and SHA, derived from the parent frozen policy ID/SHA. Closure retention and read-back also compare the stored choice to the ready parent Delivery proof; signer status checks the same frozen choice and classifies a blocked signer task with no registered policy input as `pending` during materialization, while refusing a mismatched input. Migrated-DB definition/role fixtures were extended. Tests NOT RUN; migrations 01812–01815 NOT APPLIED. These checks do not replace automatic qualified signer admission or the atomic signed assembly/publication path.

The SQL signed receipt reader now also requires the V31 workflow's provenance type/version/project/policy and a matching frozen signer run actor, export, project and snapshot. Closure retention pins the deterministic signer run and snapshot IDs plus those same frozen fields before accepting adopted evidence. Definition fixtures were extended; tests NOT RUN and migrations NOT APPLIED. Retained proof still needs a final atomic Delivery assembly admission that rechecks live authority.

The signed receipt SQL additionally requires a sealed, non-cancelled V31 signer run, deterministic workflow/run/snapshot/task IDs and exact task node key. Closure retention now requires the same live run seal, project and task node identity. These reject an adopted-looking row from an unsealed or cancelled signer graph. Migrated-DB source fixtures authored, NOT RUN; migrations NOT APPLIED. Automatic signer dispatch and Delivery publication remain open.

The V30/V31 receipt, closure retention and signer-status SQL now bind the signer task's `QualifiedExport` input to the same ready original export artifact ID and SHA used by the Delivery item. During blocked materialization, a missing input remains pending; a present mismatched input is refused, and a terminal signer task without the exact input cannot satisfy the parent. Definition fixtures authored, NOT RUN; migrations NOT APPLIED. This closes an input-identity gap only; automatic signer delegation and signed assembly/publication remain unfinished.

Migration 01816 adds a scheduler-only signing gate to the existing atomic Delivery assembly activation. Off-mode follows the prior path. For required mode the gate reads the frozen parent policy, finds the retained exact-package closure and invokes the 01813 retention validator again under the active parent expansion lease before the activation transition. A predecessor-definition guard prevents silently patching a changed activation function. Migrated-DB ACL, definition and fabricated-lease fixtures authored; tests NOT RUN and 01816 NOT APPLIED. The gate is necessary but insufficient: required-mode root/worker/manifest contracts, durable signer delegation and publication authority are still open, so the public 503 remains.

A distinct schema-v2 `SignedDeliveryAssemblyEnvelope` now names the immutable ordinary assembly document and the published signing closure as disjoint refs. Go compilation requires a canonical required-mode assembly with its frozen signing policy and an exact full-slot closure recompiled from current dependency receipts; Python validates the strict wire shape. The machine schema source and emitted schema were updated, and Go/Python unit scenarios authored, NOT RUN. This is a proposed input contract, not yet a published task input or worker adoption path; ordinary off-mode assembly bytes are unchanged and required mode remains unavailable.

The worker now has a separate pure `assemble_signed_delivery` path: it reads canonical pinned envelope/assembly/closure/shards, binds frozen tenant/actor/parent/policy, checks deterministic per-slot signer keys/tasks and one-to-one ready slot/export refs, then returns a schema-v2 manifest pointing to the signed closure. The existing `assemble_delivery` call still refuses required provenance. Cross-shard matrix and rehashed missing/reordered/foreign export/task/extra-closure unit scenarios were authored, NOT RUN; machine schema source/output were extended. This pure reconstruction does not yet enforce SQL/object/operator authority during worker adoption, nor install signed task inputs/publication, so no required-mode completion is claimed.

`verify_signed_delivery_assembly` now composes the pure signed reconstruction with task-scoped input evidence for Params, envelope, assembly, closure and every shard, exact JSON byte/SHA/size checks, one staged output and canonical v2 manifest equality. It takes tenant/actor/parent from an external trusted SQL lease and refuses foreign scope or input/output role substitution. Unit success and refusal scenarios authored, NOT RUN. It is deliberately not dispatched from generic `v2.delivery.assemble` adoption yet: SQL task input registration, dedicated signed Params/adoption source, V31 current operator/physical rechecks and final package publication must be wired first.

Go now derives the signed task's exact document/object roots from the canonical envelope plus full frozen slot closure: envelope, ordinary assembly, signing closure, shards, preset configs, unsigned exports, signed exports and C2PA manifests. A separate v2input compiler pins those roots into a bounded-direct task closure while requiring equal encoded/signed/manifest counts and the signed envelope as direct Params root. Unit exact/replay/missing/alias/foreign-contract cases authored, NOT RUN. The parent coordinator must still resolve transitive WorkingImage/recipe authority, publish/register this inventory and use signed-specific SQL admission; these helpers alone do not activate a task.

The signed Go envelope compiler now also takes the actual shard bytes and checks each root shard ref against the canonical sorted slot slice, exact ordinal/preset matrix, ready/omitted rules and source-version uniqueness before accepting the full signing closure. Signed required-root derivation uses this stricter path, so a self-consistent signing closure cannot mask unrelated assembly shards. Substituted-shard unit refusal authored, NOT RUN. Parent frozen snapshot and physical object authority remain separate admission duties.

`BuildSignedAssemblyDocuments` now reuses the existing full frozen snapshot/matrix compiler through a private required-mode branch, after first compiling the entire exact V31 signing closure. The public `BuildAssemblyInput` still returns 503 for required mode and preserves off-mode bytes. A failed signing preflight cannot publish shards; successful private preparation returns canonical assembly and closure bytes/SHA for later immutable publication. Success, public-gate and pre-publication failure unit scenarios authored, NOT RUN. Trusted physical receipt provenance, publication, task transport/adoption and final package authority remain to be composed.

Migration 01817 replaces the shard-scope provenance-off predicate with the 01816 lease-fenced signing gate. Required-mode shard reserve/complete can therefore proceed only after the exact retained V31 closure is revalidated against current ready items; ordinary off-mode still passes the same frozen-policy branch. The migration refuses an unexpected predecessor function shape, and migrated-DB definition coverage was authored. NOT RUN/NOT APPLIED. Root/envelope publication, signed input transport, adoption and final manifest publication remain gated.

Migration 01818 reserves two private backend document purposes and bounded exact-byte constraints for signed closure (8 MiB) and signed envelope (64 KiB), without changing ordinary assembly/shard limits. Its private SQL scope predicate requires current parent lease, frozen required signing policy, revalidated retained closure and, for envelope, exact ready assembly/closure document refs and their current physical artifact authority. Migrated-DB definition/ACL/constraint fixtures authored, NOT RUN; 01818 NOT APPLIED. Reserve/complete and object publication still need to be composed, so no signed document is durably published yet.

Migration 01819 authors scheduler-only signed closure/envelope document reserve and complete, using the 01818 private scope, parent expansion fence, stable artifact ID, tenant quota accounting, object version and ready replay. The Go `artifactjson` publisher allowlist and size limits now admit only these two additional lease-fenced purposes; `signedDeliveryDocumentRepository` binds exact canonical bytes, tenant/run/assemble owner, expansion token and immutable object receipt to the two SQL phases. Unit tests cover scope/fence/purpose/size/object-version/refusal/replay; migrated-DB fixtures cover owner, ACL, private scope, function definitions and fabricated-lease refusal. Authored only: tests NOT RUN, 01818–01819 NOT APPLIED. The signed publisher is not yet invoked by the production assembly pipeline; signed task transport, worker dispatch/adoption, automatic signer delegation and final package publication remain open. Public `provenance=required` still returns 503.

The private `PublishSignedAssemblyBundle` now composes one complete required-mode document publication chain after whole-matrix signer preflight: exact shards, root assembly, signing closure and schema-v2 envelope. It requires shard SHA matches, creates the root publisher only after shard bodies exist, checks each returned ref/SHA and refuses aliased envelope IDs. An authored unit scenario covers document order, envelope binding and invalid signer preflight with zero object publications; NOT RUN. The bundle still takes trusted current receipts and publisher callbacks from its caller. A production scheduler must invoke it with 01813 closure retention, the lease-fenced 01817–01819 publishers, then bind envelope/task inputs and complete dedicated transport/adoption. This composition does not enable public required provenance.

`PublishSignedDeliveryAssemblyBundle` now supplies that composition with the concrete shard/root/signed-document repositories and enforces the exact completed parent lease, prepare task and slot count before publication. It is a private workflow entrypoint, not yet called from `DeliveryAssemblyPipeline.ProcessAssembly`; that pipeline still handles the ordinary off-mode root. The signed path must still install current V31 evidence collection, scheduler invocation, signed-specific task transport and worker adoption before it can replace the public 503. No tests or migrations were run/applied.

Migration 01820 adds scheduler-only signed-envelope read-back for restart. It requires an exact active expansion lease and requested artifact/SHA, re-runs the 01818 signed document scope (including retained V31 closure, current assembly and physical closure authority), then requires a matching ready envelope artifact version. Go `ReadSignedDeliveryEnvelope` checks the same ref, canonical schema-v2 bytes and disjoint document IDs before returning a transport candidate. Unit and migrated-DB ACL/fabricated-lease fixtures authored; NOT RUN, migration NOT APPLIED. Read-back alone grants no task registration, adoption or package-ready transition, so public `required` stays unavailable.

Migration 01821 extends the existing assembly `task_params` scope with a definition-guarded signed-envelope branch. The ordinary saved-assembly/retention branch is unchanged; a signed Params root is accepted only if 01820 read-back revalidates its exact envelope ref under the lease. The existing reserve/complete and object-version protocol can therefore publish the signed task's direct Params root without a second purpose or an unsafe generic repository. `PublishSignedDeliveryAssemblyParams` performs the same read-back before calling that publisher. Migrated-DB definition fixture authored; NOT RUN, 01821 NOT APPLIED. Existing blocked-task/transport/adoption SQL still expects the ordinary DeliveryAssemblyInput root, so signed task admission and public required mode remain disabled.

Migration 01822 adds a separate scheduler-only signed assembly transport receipt. Under the exact current lease and Params it requires the blocked assemble task, sealed v2 input closure, published envelope, `DeliveryAssemblyInput` and `SignedDeliveryAssemblyClosure` refs, each shard/preset config, and every encoded/signed/C2PA triple as typed task inputs. It also requires all non-Params inputs linked to the parent workflow and the succeeded prepare edge. `SignedAssemblyTransportReady` calls it only after the existing descriptor validator. Unit query/invalid-Params and migrated-DB definition/ACL/fabricated-lease fixtures authored; NOT RUN, 01822 NOT APPLIED. This receipt does not yet register transport, activate the signed task, dispatch the signed worker path, or adopt/publish its result; the public required mode remains 503.

`FrozenSignedDeliveryAssemblyTask` now compiles the separate signed v2 input closure from exact required roots and independently resolved inventory while preserving the fixed CPU assemble task identity/profile. Its transaction method creates the blocked shell, registers immutable inputs and requires the 01822 signed receipt before success; replay of a ready receipt skips registration. `FreezePublishedSignedDeliveryAssemblyTask` derives required roots from published envelope/assembly/shards/closure and complete signed receipts. Unit scenarios for fresh/ready registration and substituted signed contract authored, NOT RUN. The production SQL registration coordinator and signed activation/worker adoption remain to be installed before any public enablement.

`RegisterPreparedSignedAssembly` and `SQLSignedDeliveryAssemblyStore` now implement the production blocked-shell/transport sequence with short tenant transactions around SQL and immutable input-document IO between them. The scheduler-facing `DeliveryAssemblyPipeline.RegisterPublishedSignedAssembly` constructs the scoped input publisher and returns only a registered, still-blocked task. Unit scenarios cover fresh/ready replay and create/renew/publish/register failures; NOT RUN. The main `ProcessAssembly` still follows off mode. Signed activation, current V31 physical/operator recheck at activation, worker dispatch/adoption and final package publication remain open.

Static activation review found that `v30_delivery_assembly_source_context` requires a direct ordinary assembly root and `v30_delivery_assembly_source_ready` still requires `provenance=off`. The existing activation cannot safely dispatch a signed task by changing only its transport predicate. Before adding a signed source gate, migration 01822 was strengthened to require exact equality between the entire registered non-Params input inventory and the server-derived assembly inventory plus envelope, closure, signed exports and C2PA manifests; it also applies the existing resource floor to all signed documents/input rows. The SQL definition fixture was extended. Authored only, NOT RUN/NOT APPLIED; no signed activation credit is claimed.

The worker now has a private leased `execute_signed_assembly` adapter around the pure signed assembler. It requires a dedicated `admit_signed` authority returning tenant/actor/parent, verifies operation/profile and typed envelope/document reads through the task artifact index, repeats admission after reconstruction and refuses scope drift before staging one v2 manifest. Unit success/scope-drift scenarios authored, NOT RUN. The adapter is intentionally absent from the active delivery registry until a SQL signed source gate supplies that admission for activation and runtime; the existing off-mode worker path is unchanged.

Migration 01823 adds private, lease-independent ready-document and signed-runtime-document readers for after the parent expansion closes. Under current tenant/run/task and exact Params it checks the physically versioned envelope, assembly and closure documents, retained closure bytes, frozen required policy, published V31 policy, task input contracts and parent prepare adoption. It returns bounded trusted source context to a future source predicate; no runtime role receives direct EXECUTE. Migrated-DB definition/ACL/fabricated-scope fixtures authored; NOT RUN/NOT APPLIED. It does not yet re-evaluate the complete current export matrix, V31 signer/operator/physical signed bytes, source resources or task lease and cannot authorize dispatch alone. Existing prepare/assembly source gates still reject required provenance.

Migration 01824 adds private lease-independent `v30_delivery_signed_current_receipts` over the 01823 source context. It compares the entire retained ready-slot closure to current Delivery items, exact export artifact, deterministic sealed V31 workflow/run/task, adopted result/validation SHA, pinned SigningPolicy and QualifiedExport task inputs, and versioned signed/C2PA manifest artifacts with their original attempt/fence/object-key bindings. Project download authority and current policy publication are rechecked. Migrated-DB definition/fabricated-scope fixture authored; NOT RUN/NOT APPLIED. SQL artifact metadata does not prove bytes are still retrievable or cryptographically valid: the existing physical verifier and qualified V31 adoption must be integrated at final activation/runtime boundaries. The function is private and does not authorize worker dispatch by itself.

Migration 01825 adds a private lease-independent signed current-matrix predicate. It composes 01823 document scope and 01824 current signer receipts, then reconstructs every frozen row-major slot from the original snapshot/presets, checks each physically ready shard document and typed task input, compares current export observations and omission reasons, requires all prepare inputs to carry forward and enforces the CPU assembly resource floor including envelope and signing closure bytes. The shared private ready-document helper now accepts only bounded shard purposes in addition to the three signed roots. Migrated-DB definition/fabricated-scope fixture authored; NOT RUN/NOT APPLIED. This predicate still needs a worker/activation wrapper with task fencing and independent physical signed-byte verification; off-mode source gates remain unchanged and public required provenance stays disabled.

Migration 01826 patches the shared worker lease predicate under an exact predecessor-definition guard so an assemble task whose Params root is explicitly registered as `SignedDeliveryAssemblyEnvelope` uses that typed direct root; ordinary prepare/assembly contracts retain their existing branch. A worker-only `v30_delivery_signed_source_scope` then checks the live attempt/fence/worker lease before and after 01825 current matrix, returns the SQL-derived tenant/actor/parent and restores tenant context. Python `TaskDatabase.delivery_signed_source_scope` and `SQLSignedDeliveryAdmission` bind that scope to the leased command; success/foreign-task unit and migrated-DB ACL/fabricated-scope fixtures authored, NOT RUN/NOT APPLIED. The signed adapter remains unregistered until final activation performs independent physical V31 byte verification, and existing prepare/assembly gates still prevent required-mode execution.

The Delivery worker registry now selects the signed adapter only when the verified task input index declares a `SignedDeliveryAssemblyEnvelope` root; otherwise it preserves the ordinary adapter. The production Delivery installation constructs the SQL signed admission port alongside the existing off-mode port, and rejects malformed custom signed installations. Route-isolation unit scenarios authored, NOT RUN. This does not enable public required mode: no signed task can be activated by the current off-mode activation/source gate, and final pre-activation physical V31 verification remains open. The previously stated unregistered-adapter limitation is superseded by this conditional routing.

Generic `prepare_v2_adoption` now has an explicit SQL-scope-selected signed Delivery branch. It runs the independent signed reconstruction verifier and validates a `SignedDeliveryManifest` v2 root; absent a nonzero tenant/actor/parent scope it retains the ordinary `DeliveryManifest` path. Signed/ordinary selector and invalid-scope unit cases authored, NOT RUN. The production adoption coordinator does not yet obtain a trusted signed SQL scope or select this branch; the existing `adopt_v30_delivery_assembled` commit still authorizes only the ordinary manifest. No signed result is adopted or package published by this change.

Migration 01827 adds a scheduler-only signed adoption scope reader. It binds a succeeded CPU assemble task to its exact attempt/fence/result and Params bytes, then rechecks the lease-independent current signed matrix and returns SQL-derived tenant/actor/parent/package identity. Fabricated scope, function-shape and role ACL fixture cases authored, NOT RUN; migration NOT APPLIED. This is metadata authority for future reconstruction, not signed-byte verification or publication. The production adopter and final signed commit remain open, as do pre-activation physical V31 checks and public required mode.

Migration 01828 adds a private signed manifest authority. It recomputes the v2 manifest fields and ready/omitted counts from every current pinned shard after 01827 scope and current-matrix checks, then binds the exact result artifact SHA/size/media/task/attempt/fence/version to the one canonical output. Fabricated result, definition and ACL fixture cases authored, NOT RUN; migration NOT APPLIED. This is an input to a future atomic signed commit, not that commit itself. Adoption coordinator routing, independent V31 physical recheck, final publication and public required mode remain open.

Migration 01829 adds the atomic signed package/run transition by cloning the guarded ordinary V30 commit definition and replacing only the manifest authority with 01828. The signed authority now returns the exact shard-body inventory needed by that transition. Ordinary and signed functions have disjoint names and authority, while package item updates, generic staged-output adoption, review creation and event/outbox state changes share the same transaction logic. Function-shape, ACL and fabricated-commit fixtures authored, NOT RUN; 01828–01829 NOT APPLIED. The production adopter does not yet call the signed function or physically recheck current V31 signed bytes before commit, and signed task activation/public required mode remain blocked.

The production adoption coordinator now probes 01827 with exact Params bytes. A current signed scope selects independent signed result reconstruction and the separate 01829 commit; missing scope retains the ordinary verifier/commit, so a stale signed result cannot fall through to publication. Before signed commit it streams every closure-listed signed export and C2PA manifest from the task's exact versioned input catalog to EOF, checks media/source/size/SHA, repeats SQL scope, and sends the signed root to generic inventory validation. Substituted-byte unit coverage authored, NOT RUN. Migrations 01827–01829 NOT APPLIED. This closes the coordinator selection and physical-object recheck code path, but runtime signed task activation, final migrated-DB positive lifecycle and public required mode remain open; the new code has not been executed.

Migration 01830 adds a separate signed activation transition, cloned under an exact predecessor-shape guard from the ordinary fenced parent barrier. It substitutes only the signed transport receipt and lease-independent current signed matrix; activation still closes the expansion, seals the run, queues the bound CPU task and records the same durable activation receipt in one transaction. `ExpansionLeaseStore.ActivateSignedAssembly` binds the existing canonical task descriptor to this new function. Fabricated activation, definition and role ACL fixtures authored, NOT RUN; migration NOT APPLIED. No scheduler path calls the signed transition yet: physical V31 object verification and scheduler delegation must precede it, and public required mode remains 503.

Migration 01831 exposes a scheduler-only, active-expansion-lease-scoped inventory of exact signed export and C2PA manifest object versions, ordered by retained closure entry. It requires the entire current signed matrix first and returns no rows for a fabricated lease. Definition and ACL fixtures authored, NOT RUN; migration NOT APPLIED. The scheduler still needs to compare inventory cardinality and identity to the closure, stream all versions, renew/re-read, and call 01830; the reader alone grants no activation.

`ReadSignedActivationObjects` now consumes that exact-version SQL inventory. `VerifySignedActivationInventory` requires two ordered rows per retained signer, checks each identity and bucket against the closure, renews the expansion lease every 25 signers, and streams both objects to EOF through the existing SHA/size/key verifier. A second inventory read can be compared with `SignedActivationInventoryEqual` before activation. Missing row, changed object version and substituted-byte unit cases authored, NOT RUN. The orchestration call that composes these steps with 01830 is not installed yet; no signed task is queued by this change.

`DeliveryAssemblyPipeline.ActivatePublishedSignedAssembly` now composes the full pre-activation barrier: validate the published closure SHA/scope, acknowledge a committed activation on replay, load the 01831 inventory, stream every signed/C2PA immutable version with periodic lease renewals, require an identical second SQL inventory, then call 01830 inside a short tenant transaction. Runtime installation binds the object store's versioned Open capability when available. This method is authored but NOT RUN; 01830–01831 NOT APPLIED. The main `ProcessAssembly` still chooses only the off-mode path, and signed bundle publication/scheduler dispatch plus public required mode remain open.

`RegisterAndActivateSignedAssembly` now composes the existing signed envelope read-back/Params publisher, frozen signed input closure, blocked task registration and the physical pre-activation barrier into one replayable scheduler call. It requires a complete published bundle, signed receipts, resolved transitive inputs and a CPU profile; it does not invent signer authority. Authored, NOT RUN. The main `ProcessAssembly` still lacks a durable delegated V31 signer-launch path and a current-scope bundle builder, so this composition is not yet reached by public required mode.

Migration 01832 adds a scheduler-only parent-lease-scoped V31 child delegation reader. For one ready Delivery slot it binds the frozen actor, current export artifact, published signing policy, project download authority and deterministic child key/workflow/task, and permits only not-started or pending status. A fabricated lease and role ACL fixtures are authored, NOT RUN; migration NOT APPLIED. This is a scope reader only: the V31 service still requires a live editor session, so scheduler launch is not yet implemented and public required provenance remains unavailable.

`ReadSignerDelegationScope` now consumes 01832 and rejects any scope whose tenant, parent, package, ready slot/export ref or deterministic V31 key/workflow/task differs from the claimed expansion. Foreign export/key unit cases authored, NOT RUN. The reader does not turn an editor session into scheduler authority; a dedicated V31 delegated admission service and exact retry/qualification path remain necessary before it can dispatch children.

Migration 01833 adds a session-free V31 export/lineage source scope tied to the 01832 parent delegation. It repeats the normal V31 ready-export graph, task-result, artifact version, project download and exact source-pin checks, then additionally matches the delegated project and export artifact. Scheduler-only ACL and fabricated-scope fixtures authored, NOT RUN; migration NOT APPLIED. This is source authority for a future delegated lineage publisher, not a signing draft, Params publication, task activation or V31 operator qualification. The existing session-based V31 functions remain unchanged.

Migration 01834 adds the reservation half of delegated V31 lineage publication. Under 01832/01833 current parent/export scope it preserves the original V31 deterministic lineage artifact ID, tenant usage/quota locking order, pending-token recovery and exact ready replay checks, with scheduler-only EXECUTE. Fabricated lease and ACL fixture cases authored, NOT RUN; migration NOT APPLIED. Object upload/completion and later delegated policy/Params/draft/activation remain open; the reservation alone dispatches no signer.

Migration 01835 adds the matching completion half: it rechecks the delegation, exact V31 source pins, reservation token, immutable version and tenant accounting before marking lineage ready, with exact-version replay. Fabricated completion, function-shape and scheduler-only ACL fixtures authored, NOT RUN; 01835 NOT APPLIED. The Go publisher is not yet wired to these functions, and delegated policy/Params/draft/activation plus public required Delivery remain open.

`SQLDelegatedLineageStore` and `PublishDelegatedSigningLineage` now use 01834/01835 around immutable Put/Recover. The store binds the caller's ready-export evidence and decoded lineage bytes to the exact SQL delegation scope, checks deterministic lineage artifact ID and SHA, maps reserve/replay outcomes, and refuses a changed object key/version/size/media before completion. Cancel/missing-authority unit cases authored, NOT RUN. A session-free SQL catalog/source reader and delegated V31 policy/Params/draft/activation are still required before the scheduler can invoke this publisher; migrations remain NOT APPLIED.

Migration 01836 and `ReadDelegatedReadyExport` now provide that session-free source metadata under the parent lease. SQL derives exact versioned export evidence, then repeats 01833's full graph/pin predicate; Go requires matching 01832 scope, bucket, artifact/SHA and non-null version and repeats delegation after the read. Foreign artifact/version unit cases and fabricated-scope/ACL DB fixtures authored, NOT RUN; migration NOT APPLIED. This does not yet derive lineage claims or qualify/publish the V31 policy, Params, draft or task, so scheduler child dispatch remains unavailable.

`CompileAndPublishDelegatedLineage` now composes a parent-scoped execution graph reader with the existing V31 lineage compiler and delegated immutable publisher. It checks the graph's final task/export, owns canonical evidence, requires identical graph reads before and after publication, and derives the deterministic lineage ref from compiled bytes. Missing-reader/cancellation unit cases authored, NOT RUN. The required `DelegatedSigningExecutionReader` implementation must still prove every canonical task/result and operation-specific claim without session authority; this coordinator alone cannot make a signer runnable.

Migration 01837 introduces a scheduler-only final export-task evidence query under the current delegated parent lease. `ReadDelegatedFinalTask` checks its task/run and canonical output against the ready export, then rereads the delegated source and scope to reject changed evidence. Foreign-task/output unit cases authored, NOT RUN; migration NOT APPLIED. This reads only the final task and does not implement the complete ancestor graph or operation-specific claims, so V31 signer launch and public `provenance=required` remain unavailable.

Migration 01838 adds a scheduler-only, bounded same-run ancestor task/result reader under that same delegated source. It follows only `task_edges` within the export run, requires canonical fenced results and ready Params, and refuses a graph over 5000 tasks. Definition, fabricated-scope and ACL fixtures authored, NOT RUN; migration NOT APPLIED. Cross-run asset-version/WorkingImage transitions, operation-specific claims and Go integration remain open, so this is not a complete execution graph reader.

`ReadDelegatedSameRunTask` now consumes 01838, binds the returned canonical task to the requested ID and final export run, and repeats the delegated export/scope after the read. The shared evidence decoder also validates canonical output refs. Foreign task/run unit cases authored, NOT RUN. This supplies only same-run task evidence; cross-run transitions and claims remain required before a complete `DelegatedSigningExecutionReader` can be installed.

Migration 01839 ports the session-based canonical ancestry traversal into a scheduler-only function rooted in the exact current delegated export. It traverses same-run edges and cross-run accepted asset-version/WorkingImage boundaries with the original role, root-link, attempt/fence and 5000-task predicates. `ReadDelegatedAncestorTask` binds its SQL result to the requested task, repeats the source/scope and checks the final task run; fabricated-scope/ACL and decoder unit cases authored, NOT RUN; migration NOT APPLIED. This is task/result ancestry authority only: operation-specific immutable claims, physical bytes and the complete graph reader remain open.

`BoundDelegatedSigningExecutionSource` now assembles the whole ancestor graph from delegated task evidence and mandatory operation-specific claims callbacks. It enforces the existing 5000-node/64 MiB/100000-item limits, binds each claim to canonical task/result/attempt/fence/Params/outputs, rereads task evidence after the claim callback, and runs the original lineage compiler. `SQLDelegatedTaskCatalog` connects each node to 01839 under the frozen parent scope; `BoundDelegatedSigningExecutionReader` refuses to run without a concrete claims resolver. Valid/tampered/missing graph unit scenarios authored, NOT RUN. Production delegated claims readers are still absent, so this adapter is not installed for signer launch and no public required mode is enabled.

Migration 01840 and `DelegatedTaskDocumentReader` supply a task-scoped immutable JSON document primitive for delegated claims. SQL accepts only canonical Params, declared inputs, fenced outputs, permitted adopted render/negative children or registered `InputClosureChunk` under the current ancestor/export/parent scope. Go compares canonical task evidence before/after lookup, streams the exact object version with size/SHA/JCS verification, then repeats the SQL role lookup. Fabricated-scope/ACL and missing-authority/cancellation fixtures authored, NOT RUN; migration NOT APPLIED. Operation-specific claims, image producer/original qualification and full production wiring remain open.

Migration 01841 adds a scheduler-only direct image-producer lookup under the delegated ancestor scope. It requires the artifact's exact canonical result output, ready immutable version and matching produced attempt/fence. `DelegatedDirectImageProducer` binds this SQL tuple to the canonical producer task, rereads the tuple and producer/final tasks before returning the parent ID. Output-binding unit and fabricated-scope/ACL fixtures authored, NOT RUN; migration NOT APPLIED. Adopted render/negative WorkingImage children still need their separate result/role resolvers, and no production delegated claims router is installed.

Migration 01842 adds a separate delegated adopted-image producer predicate for render.compose and optical.negative. It requires the exact ready child role, deterministic workflow root/working-image links, sealed canonical result output, producer attempt/fence and current ancestor scope. `DelegatedAdoptedImageProducer` reads the exact immutable root through 01840, parses ImageResult, binds its WorkingImage to the requested child, rereads bytes/SQL/task and refuses negative generated-mask output. Single-root unit and fabricated-scope/ACL fixtures authored, NOT RUN; migration NOT APPLIED. The full operation-specific render/negative claims, review/qualification and production router remain open.

`DelegatedImageProducerRouter` now composes the direct and adopted producer paths. SQL NULL from the direct lookup is classified as a genuine no-row; only that condition permits adopted-child fallback. Malformed evidence, scope failures surfaced by Go, database errors and integrity failures remain terminal. Direct/missing/integrity routing unit scenarios authored, NOT RUN. The router is a claims dependency, not yet the complete operation-specific resolver or signer runtime installation.

`DelegatedEncodeSigningTaskResolver` now duplicates the existing export.encode claim checks without a fabricated editor session: canonical Params/image/encoded manifest bytes, image color and profile/resource constraints, output dimensions/ICC/alpha, final export MIME and current producer are required, then document and producer pins are repeated. Valid/substituted manifest/changed parent/MIME unit scenarios authored, NOT RUN. This covers only export.encode; asset.decode, metadata, derivatives, render/negative and their original/review/qualification sources still need delegated resolvers before the graph is usable in production.

`DelegatedMetadataSigningTaskResolver` now mirrors the final metadata.write claims under delegated document/producer ports. It binds the before/after encoded manifests, frozen metadata policy and copied-field report, exact final artifact/MIME, then rereads every document and the producer. Valid, substituted report and changed parent unit scenarios authored, NOT RUN. It covers only the final metadata.write node; other operations and production claims routing remain open.

`DelegatedPresentationTaskResolver` now explicitly routes delegated export.encode and final metadata.write to their immutable claims resolvers. Unsupported operation/version values fail before document or producer IO; cancellation and unsupported-operation unit cases authored, NOT RUN. The router is intentionally not installed as complete V31 signer authority until asset.decode, derivatives, render/negative and their qualification/review proofs are implemented.

`DelegatedResizeSigningTaskResolver` now mirrors the derivative.resize proof under delegated ports: exact WorkingImage source/pixel inputs, computed output canvas and transform chain, report dimensions/warnings, current producer and repeated document/producer pins. The explicit router includes it; valid geometry and substituted report unit cases authored, NOT RUN. Sharpen/watermark, decode, render/negative and their higher-order claims remain open.

`DelegatedSharpenSigningTaskResolver` now mirrors derivative.sharpen claims under delegated ports, including bounded finite tuning parameters, exact source/result WorkingImage shape, reported max-channel delta/warnings, current producer and repeated pins. The router includes it; valid, excessive-delta and changed-parent unit cases authored, NOT RUN. Watermark, decode and the render/negative claim families remain open.

`DelegatedWatermarkSigningTaskResolver` now binds both source and mark image ancestries, exact pixel/alpha task inputs, placement/opacity params, unchanged output descriptor geometry/color and report, then repeats documents and producer links. The router includes it; valid dual-parent and missing-mark-parent unit cases authored, NOT RUN. Original decode, render/negative and their generation/review/qualification claims remain open.

Migration 01843 and `DelegatedOriginalQualifier` now give asset.decode a session-free original-byte proof. SQL binds a verified original upload, same-project asset-version master, decode run/task/attempt/fence and canonical input/output refs to the current delegated ancestor. Go streams that exact object version to EOF with bounded size/SHA and repeats SQL/task evidence. Fabricated-scope/ACL and missing-authority/cancellation fixtures authored, NOT RUN; migration NOT APPLIED. The decode operation resolver itself and render/negative claim families remain open.

`DelegatedDecodeSigningTaskResolver` now uses 01843's physical original qualifier after validating decode Params, exact source/master refs, WorkingImage profile/ICC/source canvas, alpha and empty transform chain. It emits only the original encoded artifact as the lineage source and is routed by `DelegatedPresentationTaskResolver`. Valid, substituted source hash and rejected-original unit scenarios authored, NOT RUN. Render/negative generation, review and qualification claims remain open before full production graph installation.

`DelegatedRenderSigningTaskResolver` and `DelegatedNegativeSigningTaskResolver` now wrap the existing pure full-claim compilers behind mandatory delegated reviewed/causal readers. Each owns task evidence, resolves a complete receipt twice, and rejects a changed causal stamp or compiled graph. The presentation router selects these only when concrete delegated readers are supplied; nil-reader/cancellation unit cases authored, NOT RUN. These wrappers do not supply the readers themselves: session-free render/negative physical qualification, review and dependency resolution remain open, so signer runtime installation is still blocked.

Migration 01844 and `DelegatedCandidateReviewReader` add a parent-scoped render/negative candidate-decision primitive. SQL first proves the adopted child/root producer and distinguishes zero, one or duplicate reviews; Go reads the exact immutable ImageResult, validates the review candidate/base and append-once decision hash/selection, then rereads root, negative Params and SQL row. Fabricated-scope/ACL and missing-authority/cancellation fixtures authored, NOT RUN; migration NOT APPLIED. This primitive is not yet wired into complete delegated render/negative causal readers, and public signer launch remains unavailable.

`DelegatedAdoptedResultReader` now provides a separate session-free physical ImageResult proof for render.compose/optical.negative. It pins the single canonical root, parses the immutable result, rejects aliased child refs and forbidden negative generated-mask output, requires 01842's exact deterministic WorkingImage link/task/fence/root, then rereads bytes, SQL tuple and task evidence. Missing-scope/cancellation unit cases authored, NOT RUN. Qualification, pixel evidence, causal inventory and complete reviewed receipt still need delegated implementations before this can satisfy the render/negative wrappers.

`ResolveDelegatedRenderPixelAncestry` now binds every WorkingImage/ImageResult input in a compiled render causal inventory to its delegated direct/adopted producer twice. It rejects absent/self/changed producers, duplicate artifact IDs, invalid pins, a task/inventory mismatch, oversized input sets and cancellation. Unit cases authored, NOT RUN. This is a producer-ancestry component, not physical pixel/mask qualification or a complete reviewed render receipt; the delegated render/negative readers and public required-provenance route remain open.

`ResolveDelegatedNegativePixelAncestry` now binds the exact optical.negative source pin to a delegated producer with two reads and rejects absent/self/changed authority, invalid task or source and cancellation. Unit cases authored, NOT RUN. This does not yet verify negative pixel bytes, reviewed qualification or the complete causal receipt required by delegated signing.

`VerifyDelegatedNegativeTransport` now checks the canonical negative input manifest, ordered direct refs, chunk bodies, entry digest, unique artifact IDs and repeated exact document bytes through the task-scoped delegated reader. `BoundDelegatedNegativeCausalSource` composes that transport check with repeated evidence stamps and delegated source-producer ancestry, returning the receipt shape consumed by the negative graph compiler. Transport/refusal and unconfigured-authority unit cases authored, NOT RUN. A concrete session-free `DelegatedNegativeEvidenceReader`, physical pixel/mask qualification and production installation are still absent; this source cannot yet authorize public signing.

`ReadDelegatedNegativeReport` now starts from the current scoped adopted ImageResult, validates the exact report child and negative review-request semantics against frozen Params, repeats both immutable document reads and adopted-result authority, and refuses missing/cancelled authority. Refusal unit cases authored, NOT RUN. This report is algorithmic review evidence only; accepted human review must still be read independently through the delegated candidate-review port.

`ReadDelegatedNegativeQualificationContent` now binds a retained negative qualification pin to independently supplied operator deployment locks and exact task-scoped Params/source/report/calibration bytes. It verifies the source image, qualification evidence, repeated immutable documents and unchanged historical pin. Fixture-based valid/corrupt/changed/unlicensed unit cases authored, NOT RUN. A SQL delegated historical pin reader and current operator-policy source are still needed before this can be installed in the negative evidence reader.

Migration 01845 and `DelegatedNegativeQualificationCatalog` now author a scheduler-only read of the negative task's retained execution profile beneath the current delegated export/ancestor scope. Go requires a matching adopted root, decodes the historical pin against exact Params bytes, rereads Params and adopted authority, and refuses unconfigured or cancelled calls. Unit refusal cases authored, NOT RUN; migration NOT APPLIED. The full negative physical evidence reader remains open.

`BoundDelegatedNegativeQualificationContent` now resolves operator locks through the existing authenticated, process-provisioned `SigningNegativePolicyRegistry` using the exact retained policy ID/SHA, then runs delegated physical qualification and rechecks the historical pin. Missing-policy/cancellation unit cases authored, NOT RUN. The earlier note calling for a separate SQL current-policy reader was incorrect: this registry is the established operator authority. Deployment wiring, physical negative pixel/mask proof and complete evidence assembly remain open.

`ReadDelegatedNegativeMask` and `ReadDelegatedNegativePixelEvidence` now bind the adopted negative ImageResult to frozen Params, source/output WorkingImage, exact changed-mask descriptor, a mandatory delegated physical mask counter and repeated immutable reads. Mask geometry/selector/mutation and unconfigured/cancellation unit cases authored, NOT RUN. The SQL/object-store counter and physical output RGB/source-array verifiers remain missing, so the complete delegated negative evidence reader is still unavailable.

`BoundDelegatedNegativeMaskCounter` now streams the exact catalogued mask object through `CountBinaryMaskNPY`, checks version/SHA/media/size and rereads the catalog after IO. Physical version/substitution/truncation/cancellation unit cases authored, NOT RUN. A lease-scoped SQL catalog proving the adopted root → changed-mask descriptor → binary artifact links is still required; the counter cannot yet be installed as negative authority.

Migration 01846 and `DelegatedNegativeMaskCatalog` now author the lease-scoped root → `$/changed_mask` descriptor → `$/changed_mask/artifact` binary link, enforcing output role/media, exact task attempt/fence, object version, size and current adopted-result authority. Go independently parses the descriptor against the output image and rereads adopted root; the counter repeats this lookup after streaming NPY bytes. Missing-scope/cancellation unit cases authored, NOT RUN; migration NOT APPLIED. Positive migrated-DB ACL/role fixtures, full source/output RGB verification and evidence-reader installation remain open.

`BoundDelegatedNegativePixelsVerifier`, migration 01847 and `DelegatedNegativePixelsCatalog` now author physical output RGB proof for optical.negative: exact adopted root → `$/working_image` → `$/working_image/pixels` links, attempt/fence/role/media/size constraints, immutable object version, streaming finite float32 RGB NPY validation and repeated catalog lookup. HDR/corruption/version/cancellation and missing-scope unit cases authored, NOT RUN; migration NOT APPLIED. Positive migrated-DB fixtures, source-array verification and the complete delegated negative evidence reader remain open.

`BoundDelegatedNegativeSourceArraysVerifier` now streams source RGB, optional alpha and every dense-warp inverse-field occurrence under exact-version delegated catalog reads. It reuses the existing finite float32/alpha NPY validators and repeats each catalog lookup after IO. HDR/NaN/version/cancellation unit cases authored, NOT RUN. A lease-scoped SQL source-array catalog remains open; without it this verifier cannot provide signer authority.

Migration 01848 and `DelegatedNegativeSourceArrayCatalog` now author a task-registered NpyArray lookup under the current parent lease, requiring the same task's registered source WorkingImage. Go derives each array pin/shape from physical frozen Params/source bytes and the private pixel evidence, checks the current adopted root, then repeats root and documents after SQL. Missing-scope/cancellation unit cases authored, NOT RUN; migration NOT APPLIED. Positive migrated-DB role/ACL fixtures and complete negative evidence-reader composition are still open.

`BoundDelegatedNegativeEvidenceReader` now composes adopted ImageResult, exact negative pixel/mask evidence, source/output physical NPY verifiers, process-provisioned qualification policy, report and accepted human review; it repeats the full evidence read and compares canonical stamps. `NewDelegatedNegativeSigningClaims` binds those ports to one Delivery delegation scope and returns the negative graph claims resolver, failing closed on missing dependencies. Unconfigured/cancellation unit cases authored, NOT RUN. This constructor is not yet installed into the parent signer launch; positive migrated-DB/object-store cases and the render branch remain open, so public `required` mode is still unavailable.

`BoundDelegatedRenderCausalSource`, `ResolveDelegatedRenderOutputs` and `BoundDelegatedRenderReviewedCausalSource` now join render evidence/closure to stable per-input delegated producers and a repeated explicit review/action/child-output receipt. They preserve the session-based causal stamp and reread evidence, producer edges and review authority around external IO. Missing-authority/cancellation unit cases authored, NOT RUN. Concrete delegated render evidence and input-closure readers, physical pixels/masks and dependency claims remain open; this is not yet an installable render signer branch.

`ReadDelegatedRenderRecipe` and `ReadDelegatedRenderReport` now use task-scoped immutable document reads to validate frozen render Params and report semantics, including exact session/version binding, ordered parent report refs and repeated bytes. Recipe valid/hash/mutation/cancellation and report refusal unit cases authored, NOT RUN. These are components of a future delegated render evidence reader; result pixels/masks, qualification and closure authority remain open.

`ReadDelegatedRenderMask` and `ReadDelegatedRenderPixelEvidence` now bind the scoped adopted ImageResult to frozen recipe, source/output WorkingImage, exact changed/generated descriptors and mandatory physical mask counters. They recheck source/output bytes and adopted root after counter IO. Missing-authority/cancellation unit cases authored, NOT RUN. Scoped render mask SQL/object streaming, output RGB and qualification are still missing; this does not yet authorize render signing.

`BoundDelegatedRenderMaskCounter` now streams changed/generated NPY bytes through the binary-mask validator and rereads the exact versioned catalog entry after IO. Changed/generated/version/hash/cancellation unit cases authored, NOT RUN. A delegated SQL catalog proving each descriptor and binary child under the current adopted render root is still needed before this can be installed.

Migration 01849 and `DelegatedRenderMaskCatalog` now author both fixed changed/generated root → descriptor → binary-link paths under the current parent lease. SQL enforces output role, media, size and exact task attempt/fence; Go checks the adopted ImageResult, parses the mask against output WorkingImage and repeats adopted authority. Missing-scope/cancellation unit cases authored, NOT RUN; migration NOT APPLIED. Positive migrated-DB/object-store cases, output RGB qualification and complete render evidence assembly remain open.

`BoundDelegatedRenderPixelsVerifier`, migration 01850 and `DelegatedRenderPixelsCatalog` now author exact adopted render root → WorkingImage → pixels linkage and bounded streaming float32 RGB NPY verification, preserving finite HDR. Catalog lookup is repeated after object IO and enforces attempt/fence/role/media/version/size. HDR/NaN/version/cancellation and missing-scope unit cases authored, NOT RUN; migration NOT APPLIED. Render qualification, complete render evidence/closure readers and positive migrated-DB fixtures remain open.

Static fixture review corrected canonical task setup in newly authored delegated render recipe, render mask, render RGB and negative mask counter unit cases so their valid branches include run/result/attempt/fence, Params and canonical JSON columns. Tests remain NOT RUN; this is authoring correction, not a test result.

Migration 01851 and `DelegatedRenderQualificationCatalog` now read the retained render qualification pin and execution profile for an exact delegated ancestor task. Go checks current canonical task and adopted root before/after SQL, then decodes the pin against model/ICC locks from the frozen recipe. Missing-scope/cancellation unit cases authored, NOT RUN; migration NOT APPLIED. This is historical measured identity, not live runtime admission; complete render evidence and closure readers remain open.

`VerifyDelegatedRenderTransport` and `BoundDelegatedRenderClosureSource` now author session-free validation of the render input manifest/chunks, exact frozen Params witness, sorted typed-entry digest and repeated closure evidence. Missing-authority/cancellation unit cases authored, NOT RUN. A concrete delegated `ResolveDelegatedTaskInputEvidence` traversal/catalog implementation remains open, along with physical typed binary closure and render evidence composition; this source is not installable yet.

`BoundDelegatedRenderEvidenceSource` now composes adopted render pixel/mask evidence, physical output RGB, historical qualification, ordered parent ImageResults, inherited parent reports and the exact current render report. It repeats parent root documents, parent list, adopted root and qualification after external reads; unconfigured/cancellation unit cases authored, NOT RUN. Concrete delegated parent/report catalogs and the physical typed closure traversal are still absent, so the render claims branch remains uninstalled.

Migration 01852 and `DelegatedRenderParentCatalog` now author the ordered registered ImageResult parent list for an exact delegated render task. SQL rejects missing/duplicate/oversized or unadopted roots and checks each producer attempt/fence and canonical output link; Go reads each ancestor producer under the same lease and repeats task/adopted-root and list authority. Missing-scope/cancellation unit cases authored, NOT RUN; migration NOT APPLIED. A delegated parent-report catalog and typed render closure traversal remain open.

The delegated review reader now derives the candidate ref only from the verified ImageResult, removing caller-supplied candidate authority. Typed render and negative adapters convert its accepted decision to the existing claim structures; negative review remains mandatory. Wrong-operation adapter unit cases authored, NOT RUN. The reviewed/causal readers still need to consume these ports and establish the remaining physical/model/quality evidence.

Migration 01853 and `DelegatedParentReportReader` now author a parent-lease-scoped catalog for one inherited report attached to an ancestor's canonical ImageResult. The catalog accepts any ancestor operation only when its canonical result owns exactly one registered ImageResult root and task-owned report link; the reader rechecks producer, root and report bytes plus catalog authority after object IO. This includes valid exposure/perspective ancestors previously rejected by an operation allowlist. Missing-scope/cancellation unit cases are authored, NOT RUN; migration NOT APPLIED. Typed render closure traversal, full evidence composition and parent signer installation remain open.

The delegated parent report reader now validates the report's typed ImageReport shape before returning bytes and caps object-key/version metadata read from SQL. Source pins, review status, report-DAG depth/cycle checks and inherited report inventory remain the responsibility of `CollectSigningRenderParentReports`; the reader cannot be used as a stand-alone render qualification. Authored, NOT RUN.

Migration 01854 and `DelegatedRenderInputCatalog` now expose the registered non-transport render input inventory beneath the current Delivery parent lease. Go checks exact canonical task identity, sorted unique refs/contracts, the frozen task input digest and a repeated registry read. Missing-scope/cancellation unit cases authored, NOT RUN; migration NOT APPLIED. This is registration evidence only: a delegated per-occurrence artifact catalog, typed graph traversal, object-byte checks and render signer installation are still required.

The render input verifier now requires both frozen roots (`FrozenRenderRecipe` Params and the exact source `WorkingImage`) in the registered sorted inventory before accepting its digest. Valid, missing/wrong root, unsorted and substituted-digest unit cases authored, NOT RUN. The source ref is supplied by a separately decoded frozen recipe; the full resolver must bind the two reads around traversal.

Migration 01855 and `DelegatedRenderClosureResolver` now connect the registered inventory to the existing typed `v2draft.ClosureResolver` through an active-parent-lease per-occurrence object catalog. Each occurrence must be registered to the ancestor render task, preserve its parsed parent in the inventory and resolve to an exact ready versioned object. The walker physically reads and repeats JSON catalog lookups, and the adapter compares the typed traversal inventory with frozen task registration before and after. Missing-scope/cancellation/foreign-occurrence unit cases authored, NOT RUN; migration NOT APPLIED. This historical task-input registration is not by itself a live recheck of every model-consent and project/asset authorization encoded in the editor catalog; those delegated authority predicates and binary-leaf verification remain open before signer installation.

The delegated occurrence catalog now follows `v2draft`'s typed/generic alias rule: a generic `Artifact` occurrence may refer to a typed registered input, but SQL still queries its exact registered contract and ref. A typed occurrence must match the registered contract. Foreign-parent and wrong-typed-contract unit cases authored, NOT RUN.

The delegated render closure resolver now reuses the V31 exact-version physical hash checks for every registered NPY/binary leaf and background asset after typed traversal, with the work ICC lock re-read from the frozen recipe. It then repeats the registered input inventory. Unconfigured/cancellation unit cases authored, NOT RUN. This byte check does not replace live model-consent/project scope predicates, output qualification or end-to-end signer installation.

Delegated render physical verification now also reopens every `WorkingPixels`/`WorkingAlpha` occurrence against its witnessed WorkingImage and validates exact NPY dtype/shape, finite RGB values (including HDR), alpha range, SHA and byte count, repeating catalog authority around IO. Other binary occurrence types retain exact-version hash verification; their operation-specific format checks and positive object-store fixtures remain open. Authored, NOT RUN.

The WorkingImage array verifier now preserves both admitted RGB dtypes: float32 retains finite HDR validation, while little-endian uint16 HWC RGB receives a strict shape/type/byte/SHA parser. Alpha keeps its existing uint16-or-unit-float32 rule and the size envelope now permits both. Uint16 valid/wrong-dtype/shape/endian/length unit cases authored, NOT RUN.

Repeated render WorkingImage witnesses are now accepted only when they carry byte-identical canonical JSON; a changed body for the same ref refuses. This matches typed closure paths that may revisit an input through multiple branches while still requiring a fresh physical array check for each occurrence. Authored, NOT RUN.

Render closure physical verification now validates each `MaskPixels` occurrence whose witnessed parent is a `MaskRef`: exact mask artifact ref, canvas-sized float32 probability NPY, finite unit values, SHA/length and unchanged catalog authority around IO. Other parent contracts keep exact-version hash verification until their separate shape/role rules are installed. Authored, NOT RUN.

`BoundDelegatedRenderDependencyResolver` now derives model, background and control claims from a fresh delegated render causal read and rejects a caller-substituted inventory, mirroring the session-backed resolver. Missing-source/cancellation unit cases authored, NOT RUN. This does not yet install the render signer branch: live consent/project scopes for registered input occurrences and remaining operation-specific binary formats must be resolved before wiring public required provenance.

Migration 01856 adds a session-free live CandidateProfile guard beneath the exact delegated render task and its frozen Params. It repeats the editor model-use, active learning version, eligibility, model/training object and live consent joins, and the occurrence catalog calls it before returning each CandidateProfile object. Migration NOT APPLIED, tests NOT RUN. DescriptorShard, background and other special project/asset scopes still need delegated live predicates; render claims remain uninstalled.

Migration 01857 adds a delegated live DescriptorShard guard anchored to frozen learning, model lock, snapshot membership, qualified session analysis and the canonical session.describe result; it also requires the 01856 live model/consent check. The per-occurrence catalog invokes these guards by the registered contract, including generic `Artifact` aliases, so an alias cannot skip a special live predicate. Migration NOT APPLIED, tests NOT RUN. Background assets and other special scopes remain open.

Migration 01858 adds a delegated live StyleBackgroundAsset guard: the input must equal the frozen Params' stored background pin, and the existing portable asset/style-version binding function must still prove current project, approval, style config and exact ready bytes. The per-occurrence catalog invokes it by registered contract. Migration NOT APPLIED, tests NOT RUN. Source asset/project authority and remaining specialized contracts still need delegated live checks before render signer installation.

Migration 01859 adds a delegated live selected-source WorkingImage guard tied to frozen Params, current project asset/version, accepted review and exact ready master artifact. The occurrence catalog invokes it for the frozen recipe source ref. Migration NOT APPLIED, tests NOT RUN. Other WorkingImage frame occurrences require their own session/snapshot authority, and render claims are not yet installed.

Migration 01860 adds live snapshot-frame WorkingImage authority beneath frozen learning and the current qualified DescriptorShard. It checks the exact frame ordinal/version/source ref, accepted project asset and scene membership, and the occurrence catalog calls it only for typed frame nodes derived by the walker. Migration NOT APPLIED, tests NOT RUN. FaceObservations and frame RGB/alpha child authority remain separate gaps.

Migration 01861 adds delegated frame child authority: FaceObservations must be a ready output upload of the same canonical session.describe attempt as the live descriptor; frame RGB/alpha must be ready NPY artifacts produced with the exact source WorkingImage attempt and output role. The occurrence catalog routes typed frame nodes to source/faces/array guards and checks selected-version binding. Migration NOT APPLIED, tests NOT RUN. Positive migrated-DB/object-store cases and broader render authority remain open.

Migration 01862 adds the corresponding live selected-source RGB/alpha authority: each child must share the current master WorkingImage's producer attempt/fence and exact output role. The occurrence catalog invokes it for typed, non-frame children of the frozen recipe source; the physical NPY verifier still checks decoded shape/value/bytes. Migration NOT APPLIED, tests NOT RUN. Other source child contracts and complete renderer signer installation remain open.

`NewDelegatedRenderSigningClaims` now composes the delegated render adopted result, output mask/RGB, qualification, parent-report DAG, typed/physical input closure, causal producer map, accepted review and dependency claims under one Delivery scope. A missing-scope unit case is authored, NOT RUN. The constructor is not yet installed in the parent scheduler: current special-scope coverage and positive migrated-DB/object-store evidence must be completed before public `required` can be enabled.

`NewDelegatedSigningExecutionReader` now assembles one parent-scoped V31 graph reader across asset.decode, render.compose, optical.negative, derivatives, export.encode and metadata.write, with direct/adopted producer routing, physical original-source qualification, negative policy registry and explicit work ICC lock. Missing-scope/policy unit case authored, NOT RUN. The reader is not yet installed into delegated lineage publication or background signer dispatch; migration/application and positive end-to-end evidence remain open.

`CompileAndPublishCurrentDelegatedLineage` now installs that scoped graph reader into the existing repeated-read lineage compiler/publisher under the same SQL delegation store. Missing-runtime/cancellation unit cases authored, NOT RUN. The parent scheduler still does not call this entry point or dispatch V31 signers; public `required` remains gated.

`PrepareDelegatedSigningIntent` and `CompileDelegatedSigningTask` now author a separate session-free V31 child path. It retains deterministic workflow/run/task IDs, exact ready export, lineage and policy refs, live operator resolution and the existing resource floor, while the normal editor compiler continues to require a real session. Missing-evidence/cancellation/mode-separation unit cases authored, NOT RUN. This prepares no SQL draft or activation by itself; delegated policy/Params publication and atomic admission remain open.

`CompileDelegatedSignerChild` now binds that session-free task to the exact active Delivery expansion slot and delegated SQL scope: tenant/project/export artifact and digest, pinned policy digest, deterministic idempotency/workflow/run/task identifiers and compiled task identity must agree. Cancellation and unbound-scope unit cases authored, NOT RUN. Its caller still must publish immutable lineage/Params under delegated authority, repeat live scope and ready-export checks around object IO, and perform atomic child admission/activation. No public `required` capability or full-module completion credit.

The delegated child compiler now derives the tenant policy artifact identity from the frozen policy ID/SHA and requires an exact match with the installed tenant-projected registry plus live operator resolution. The caller cannot inject another policy ref. The parent delegation SQL already requires this policy to be ready; the remaining publication work concerns signer lineage/Params and their atomic admission, with current SQL and physical evidence rechecks. Authored, NOT RUN.

`CompileCurrentDelegatedSignerChild` now reads the live delegated scope and exact ready export before and after policy/operator resolution and session-free task compilation, refusing a changed lease, export version or project authority. The compiler requires the export-scoped deterministic lineage artifact ID and digest. Missing-reader/cancellation unit cases authored, NOT RUN. Atomic SQL draft/admission must recheck these predicates under locks; no public signer dispatch is installed.

Migration 01863 and `SQLDelegatedLineageStore.VerifyReady` now require the exact delegated lineage document and ready artifact under the current parent lease, export pins, source run, object version, digest and byte length. The current-child coordinator checks this receipt both before and after compilation. Migration NOT APPLIED; tests NOT RUN. The SQL draft, task Params publication, transport registration and atomic activation remain open.

`BuildDelegatedSigningDraftDocuments` now derives bounded canonical config, snapshot inputs and exact Params bytes from the compiled delegated intent/task, checking deterministic workflow/run/task/snapshot IDs, the three-input digest, Params ref and request SHA. Cancellation/empty-scope unit cases authored, NOT RUN. This is preparation only: a scheduler-only SQL draft function must still validate and persist these bytes under live parent authority before task Params publication.

Migration 01864 adds the scheduler-only `begin_v31_delivery_signer_draft` and Go `BeginDelegatedSigningDraft`: an exact current parent delegation, ready export source pins, published lineage receipt, ready installed policy, deterministic child IDs, canonical input digest, request hash and Params ref gate creation/replay of empty run/workflow/snapshot containers. No editor session or user pending ledger is synthesized. Migration NOT APPLIED; tests NOT RUN. This does not publish Params, materialize task inputs, seal/activate a task, or enable public `required`.

Migration 01865 adds the private `v31_delivery_signer_params_scope` read predicate for the exact delegated signer draft, parent lease, ready lineage/policy, run/workflow/snapshot and Params body. It grants no direct DML. Migration NOT APPLIED; tests NOT RUN.

Migration 01866 and `PublishDelegatedSigningParams` now add the dedicated scheduler-only reserve/immutable-object/complete lifecycle. Reservation and completion recheck the delegated parent, empty draft, ready lineage/policy, exact Params hash/size/ref, tenant quota, fenced token and versioned ready artifact; Go repeats lineage scope around object IO. Migration NOT APPLIED; tests NOT RUN. Task materialization, input transport, activation and parent dispatch remain open, so public `required` stays unavailable.

Migration 01867 and `MaterializeDelegatedSignerTask` now create or strictly replay one blocked provenance.sign task with a validated CPU/signer profile, exact ready Params object, frozen input digest, operation registry and reserved output quota under current parent authority. Params reserve/complete in 01866 remain restricted to the pre-task stage, while the shared scope reader permits materialization replay. Migration NOT APPLIED; tests NOT RUN. Transport registration, atomic activation and parent dispatch remain open.

Migration 01868 adds `v31_delivery_signer_transport_ready`, and `DelegatedSignerTransport` now builds the bounded three-input closure and registers it through the generic V2 registrar inside a scheduler transaction bracketed by this current-parent gate. Missing-publisher/transaction/cancellation unit cases authored, NOT RUN; migration NOT APPLIED. Transport document publication still needs a dedicated per-document parent-lease authority, and activation/parent dispatch remain open. Generic V2 document scope alone is insufficient proof of the Delivery delegation.

Migration 01869 now wraps the established fenced V2 chunk/manifest reserve and complete operations with the current delegated parent and signer transport gates in one scheduler-only SQL transaction per call, including a post-write recheck that aborts on stale authority. Migration NOT APPLIED; tests NOT RUN. Positive SQL/object-store evidence, activation and parent-scheduler installation remain open.

`DelegatedSignerInputPublisher` and `PublishCurrentDelegatedSignerInputs` now bind the sole canonical chunk and manifest to the 01869 lease-fenced reserve/complete functions and immutable object store. The publisher refuses caller-changed transport bodies, checks lineage authority around each publication and returns only exact ready refs; the existing transport registrar can consume these refs under the 01868 transaction gate. Missing-authority/cancellation unit cases authored, NOT RUN. Positive migrated-DB/object-store evidence, activation and scheduler installation remain open.

Migration 01870 and `ActivateDelegatedSignerInTransaction` now add a delegated atomic seal path: current parent/export/lineage/policy, exact Params, registered three-entry closure, reserved task and one-node graph are required before the established V31 V2 seal creates runnable/outbox state. The Go path registers inputs and activates in one caller-owned scheduler transaction, with a strict queued replay branch. Migration NOT APPLIED; tests NOT RUN. Caller-owned fresh operator/resource/physical checks and parent scheduler dispatch are still required; public `required` remains unavailable.

Migration 01871 and `RecoverDelegatedSignerTicket` now recover the queued creation acknowledgement from a sealed signer run under the currently leased Delivery slot, including exact actor/project/export/policy/task, ready Params and registered closure. It can distinguish a committed seal after an unknown client-side commit result without a fabricated editor session. Migration NOT APPLIED; tests NOT RUN. The parent scheduler must call it on ambiguous activation outcomes and still distinguish ticket recovery from signed-result adoption.

`MeasuredSigningResourceQualifier.QualifyDelegatedSigningResources` now qualifies the scheduler-only signer using the installed exact-workload measurement, current ready export pins, live tenant-projected policy/operator, SDK lock, delegated task compiler and measurement validity window. It does not synthesize an editor session or accept a caller profile. Exact path and actor/session/tenant/export/version/digest/MIME/operator/cancellation refusal unit cases authored, NOT RUN. The parent scheduler must still resolve and recheck the ready export, invoke this qualifier and connect draft/publication/activation/recovery before public `required` can be enabled.

`DispatchCurrentDelegatedSigner` now composes the existing delegated draft, immutable Params publication, blocked task materialization, fenced transport publication and atomic activation. It first recovers an already sealed exact child on retry, then repeats live parent/export/lineage plus installed resource/operator qualification before new staging and before activation; an ambiguous commit is acknowledged only through the current-slot recovery query. A missing-installation/cancellation unit case is authored, NOT RUN. The production scheduler does not yet call this coordinator, and positive migrated-DB/object-store qualification is missing. Public `required` remains gated.

`CompileQualifiedCurrentDelegatedSignerChild` now obtains the signer profile from the installed measured qualifier, not caller-supplied resource numbers. A provisional protocol-floor compile freezes only identity/policy, the qualifier resolves the exact measured workload, and a second current-scope/export/lineage compile must agree before returning the task. Missing-measurement/cancellation unit cases authored, NOT RUN. Source inspection also confirms the public required-provenance branch is rejected upstream in `ObserveExport`; scheduler installation alone cannot enable it. Required-mode gates must be changed only with full signing/adoption/assembly wiring and positive physical evidence.

The pure `ObserveExport` slot observer now permits `required` policy to classify the unsigned encoded export as ready/pending/failed using the same frozen binding checks. This is export readiness only; no signed-result or package credit is granted. Unknown provenance still refuses, and the existing public create/freeze/assembly gates remain closed. Updated unit expectation authored, NOT RUN. The scheduler still needs persisted slot discovery, delegated signer dispatch, signed-result adoption and assembly verification.

`PreparePlan.ReadReadySigningPage` now discovers at most 100 ready unsigned export slots for required provenance from two identical current observation-page reads under the parent expansion lease. It checks exact frozen slot expectations, returns an owned ready-slot page and renews the lease; signer admission must still repeat SQL source/policy authority. Missing-scope/cancellation unit cases authored, NOT RUN. The scheduler does not yet consume these pages, and neither queued signer tickets nor this discovery constitute signed-result adoption.

`DeliveryExpansionDriver` now has a signer stage before assembly when a signer processor is installed. It renews the parent lease, accepts only pending/failed/adopted states, and advances to assembly solely after adopted. A unit scenario covers all states and ordering, NOT RUN. The production runtime does not yet install this processor; public required mode remains gated at create/freeze and signed assembly remains unwired.

Ready signer-page unit coverage now includes a frozen required-policy plan with a ready encoded export, pending export, changed observation between the two reads and lost lease. These are authored cases, NOT RUN. The existing editor-session `SignedClosureCoordinator` cannot be installed for a scheduler child; a delegated physical receipt/adoption path and runtime signing configuration are still necessary.

The signing-page reader also copies artifact refs returned by each observation read before comparing passes, so a repository reusing a mutable pointer cannot silently change the first-pass evidence during the second read. Authored, NOT RUN.

Migration 01872 adds a separate persisted round-robin `v31_signing_scan_cursor` to the existing expansion row and scheduler-only `next_v31_delivery_signing_page`. It advances at most 100 slots only for an active, fully admitted Delivery parent under its current lease and a frozen `required` policy; the export admission cursor remains untouched. Go `ExpansionLeaseStore.NextSigningPage` validates the returned position and unit cases cover exact lease arguments, null/foreign position, partial admission and cancellation. Migration NOT APPLIED; tests NOT RUN. This is bounded discovery only; the production signer stage still must consume each page, dispatch current children, observe/adopt signed results and gate assembly.

Migration 01873 and `ReadSigningParentActor` expose only the parent run creator to the scheduler under the current complete Delivery lease and frozen required policy, without an editor session. `PreparePlan.SigningChoice` reads the frozen policy from its proof. `ProcessCurrentSigningPage` now composes the persisted cursor, repeated observation, deterministic per-slot signer status and delegated dispatch callback: it dispatches only not_started/pending children, skips adopted/succeeded_unverified and refuses failed/cancelled; it never returns assembly credit. Missing-installation/cancellation unit cases authored, NOT RUN; migrations NOT APPLIED. The production callback still must install lineage/compiler/dispatcher, and global physically verified closure remains open.

`DelegatedSigningSlotDispatcher.Dispatch` now installs the actual current-slot callback: parent-scoped ready export, V31 causal lineage publication, tenant-projected live policy, installed measured resource compilation, and durable Params/task/transport/atomic activation with strict queued ticket identity. Missing-installation/cancellation unit cases authored, NOT RUN. The callback is not yet installed in the workflow runtime; physically verified adopted-result closure, required-mode creation/freeze and signed assembly remain open.

Migration 01874 adds scheduler-only `v31_delivery_delegated_signed_receipt` for an adopted signer under the current parent lease, exact package slot/export, frozen policy, deterministic child workflow/task, canonical result and ready versioned signed/manifest artifacts. `ReadDelegatedSignedDependencyReceipt` binds the returned row to the caller's exact slot/dependency and rejects missing, foreign or malformed receipts. Unit scenarios authored, NOT RUN; migration NOT APPLIED. This SQL/Go receipt is not physical byte verification, and complete delegated closure/adoption assembly remains open.

`SigningPolicyRegistry.ResolveAdoptedPolicy` now rechecks the exact tenant-projected frozen policy ID/SHA, validity window, MIME and live signing operator without fabricating a signer request. `VerifyDelegatedSigningDependencyCurrent` brackets exact-version signed image/C2PA manifest streaming with two identical delegated receipt reads and policy/operator checks; it validates deterministic parent/slot task identity and refuses changed receipts. Exact and changed-receipt unit scenarios authored, NOT RUN. The scheduler still must construct the complete required-slot closure and repeat its authority at signed assembly/publication; this individual verifier grants no package credit.

`DelegatedSigningClosureCoordinator.VerifyAndRetain` now reads the entire frozen export matrix under the parent lease, observes every deterministic signer status, physically verifies each adopted signed image/C2PA manifest with repeated receipt and live policy checks, and retains only a complete closure under the SQL lease gate. Pending/failed exports or signer states return no closure. Missing-installation/cancellation unit cases authored, NOT RUN. The workflow runtime and signed assembly pipeline still do not install this coordinator; public required mode remains closed.

`DeliverySigningStage.ProcessSigning` now connects one bounded current signing page with whole-matrix delegated closure verification for the `DeliveryExpansionDriver` signer hook. It returns adopted only when the coordinator has retained the complete physically checked closure, so a queued child cannot enter assembly. Missing-installation/cancellation unit cases authored, NOT RUN. Current worker startup does not install signing deployment policy/resource dependencies into this stage; the existing signing installation is in the API process, and signed assembly/public required-mode gates remain open.

`CompileSignedDeliveryAssemblyProfile` now derives CPU RAM/scratch/output budget from the actual published required-mode assembly, signing closure, envelope, shards and resolved signed-object roots. `RegisterAndActivateSignedAssembly` no longer accepts a caller-provided profile and compiles it from those exact documents before freezing the task. Missing-bundle/cancellation unit cases authored, NOT RUN. The production assembly processor still calls only its off-mode path; complete signed bundle publication and runtime installation remain open.

`DeliveryAssemblyPipeline.VerifyAndPublishSignedAssembly` now takes the complete signer set exclusively from the delegated whole-matrix physical verifier, repeats exact retained-closure SQL authority, and publishes signed assembly documents from that set. It does not accept caller-supplied slots or receipts. Incomplete-installation and cancellation unit cases authored, NOT RUN. Transitive signed-input resolution, task registration/activation through this path, worker runtime installation, public required-mode gates and physical migrated-DB acceptance remain open; this is no module completion credit.

`ResolvePublishedSignedAssemblyInputs` now extends the current SQL-reconstructed ordinary assembly inventory with roots derived from exact signed documents and the verified signer set. It refuses digest/contract conflicts for the same artifact ID. `ProcessVerifiedSignedAssembly` carries that inventory into signed task registration and the existing physical activation barrier. Merge conflict/ordering unit cases authored, NOT RUN. Production worker installation, public required-mode creation/freeze gates and end-to-end migrated-DB/object-store evidence remain open; no module completion credit.

`PreparePlan.ProvenanceMode` now reads the immutable frozen policy for scheduler branch selection. The Delivery driver skips signing for `off` and requires both a signing stage and a distinct signed assembly processor for `required`; `SignedDeliveryAssemblyProcessor` repeats whole-matrix verification before the signed publication/activation path. The former off-mode driver test was updated to assert signer isolation, NOT RUN. Worker construction does not yet install the required-stage dependencies, and public required create/freeze gates remain closed.

`InstallDeliverySigningStage` now composes the scheduler's delegated slot dispatcher, lease-scoped observation reader, complete physical closure coordinator and signed assembly processor from one explicitly supplied trusted runtime. It rejects partial dependencies and leaves the off-mode job unchanged on failure. Partial-runtime unit case authored, NOT RUN. The scheduler process still needs a pinned deployment loader to supply policy, negative-qualification and measured-resource handles and call this installer; public required gates stay closed.

The signing installer now routes both the dispatch and signed assembly stages by the current lease tenant, validates and projects each installed policy registry to its declared tenant, and copies the tenant map at installation. Unknown tenants fail closed before dispatch or publication. Unknown-tenant unit case authored, NOT RUN. A pinned scheduler deployment loader and complete public required-mode admission remain open.

The signed publication pipeline now renews the parent lease and retains its exact published assembly root through `retain_v30_delivery_assembly` before resolving transitive inventory. Previously it published the root without the SQL retention required by `v30_delivery_saved_assembly` and `v30_delivery_assembly_inventory`, so the signed registration path could not proceed. This lifecycle change is authored, NOT RUN; physical migrated-DB acceptance and runtime installation remain open.

`InstallSigningSchedulerDeploymentFile` now reuses the pinned signing deployment document, its trusted process identity/native handles, live operator checks, tenant policy registries and exact resource reports to construct scheduler-owned tenant handles with a `Close` lifecycle. The worker converter owns a map copy and refuses a missing/closed deployment; an absent negative policy catalogue is represented by a registry that refuses negative claims. Missing-deployment unit case authored, NOT RUN. Scheduler startup still needs to create the distinct API/scheduler pools, invoke this installer, wire the Delivery job and manage shutdown before public `required` can open.

The scheduler process now has an explicit signing factory hook. With a pinned signing deployment declaration it opens a separate `photo_api` pool, verifies that role, installs the tenant catalogue, converts it to the Delivery runtime, and installs both required-policy driver stages; cleanup closes operator handles and the API pool. Disabled/unpinned and cancellation startup unit cases authored, NOT RUN. Public `required` create/freeze gates remain closed, and migrated-DB/object-store end-to-end acceptance is still missing.

Migration 01875 replaces the old prepare-source `provenance=off` clause with a private frozen signing-choice predicate that accepts only `off` with no signing fields or `required` with a nonzero policy UUID and lowercase SHA-256. It preserves the existing snapshot/project/session/preset/resource checks. Migrated-DB positive/negative choice cases authored, NOT RUN; migration NOT APPLIED. Other metadata/public create/freeze barriers and full required-mode acceptance remain open.

Migration 01876 changes the current metadata barrier and metadata adoption predicates to accept only the same validated frozen signing choice. Metadata still produces the original unsigned encoded export before the delegated signer and retains all source/result/fence checks. A migrated-definition fixture is authored, NOT RUN; migration NOT APPLIED. Public create/freeze capability gating, live installed-policy check, and end-to-end signed metadata acceptance remain open.

Public Create now freezes a structurally valid `required` policy and gates fresh preparation on the API process's tenant signing router: it checks the exact installed policy ID/digest, validity window and live operator before source IO, with per-child MIME qualification remaining at signing time. Off mode never calls the signing authority; completed idempotent replays can still return their existing receipt. The API constructs the Delivery creator after installing the signing router, and ordinary/selected/watermark preparation repeat the gate. Unit cases authored, NOT RUN. This is a coded capability path, not verified availability: migrations 01875–01876 are NOT APPLIED and signed package end-to-end acceptance remains open.

Migration 01877 adds a scheduler-only terminal signer failure transition. It re-reads the deterministic signer status under the complete current Delivery parent lease, then atomically fails the package/item/run, closes the expansion and emits one run-failed event/outbox receipt. The signing stage now distinguishes failed export observations from terminal signer failures and calls the respective fenced finalizer; the driver treats `finalized_failed` as terminal progress. Fabricated-lease/ACL integration cases authored, NOT RUN; migration NOT APPLIED. Positive failed-signer migrated-DB acceptance and quota release still need evidence.

Signed Delivery assembly adoption now streams and checks the original export bytes against the frozen SHA and size alongside the signed image and C2PA manifest. Previously it checked only catalog evidence for the original export. A substituted original export unit case was authored; tests NOT RUN. This closes one physical input-integrity gap but does not establish migrated-DB/object-store acceptance or full Delivery completion.

Signed assembly matrix reconstruction now requires distinct artifact IDs across the envelope, assembly, closure, slot shards and every export/signed/C2PA result. This rejects a binary result aliased to a task document before the worker can emit a manifest; the adoption path reconstructs the same matrix. A shard/signed alias case was authored, NOT RUN. SQL admission and migrated physical acceptance remain to be verified later.

Migration 01878 fixes the signed-current-receipts pre-commit state predicate: optional failed exports are still `pending` until assembly adoption records `omitted`, so requiring every item to be `ready`/`omitted` made valid signed packages impossible. The replacement permits `pending` only on nonrequired items; `v30_delivery_signed_current_matrix` still reconstructs every slot from the current export observation and frozen omission policy. The migrated-definition assertion was extended, NOT RUN; migration NOT APPLIED. Positive optional-omission DB/object-store lifecycle remains required.

The Delivery expansion driver now refuses a complete admission cursor when its ordinary assembly processor is missing. Previously it returned successful no-op progress and could leave a fully admitted package without any assembly path. A complete-cursor missing-installation unit case was authored, NOT RUN.

The bounded signer page now exposes a terminal failed/cancelled slot to the scheduler instead of silently skipping it. `DeliverySigningStage` immediately passes that exact slot to the existing fenced `fail_v31_delivery_signing` writer, which rechecks current signer state before closing the package. This avoids a later failure being hidden indefinitely by an earlier pending slot in the whole-matrix verifier. Cancellation/installation unit coverage was extended, NOT RUN; positive migrated-DB failure lifecycle remains required.

The signer page now reports the end of its persisted round-robin sweep. Intermediate pages return `scanning`, which keeps the scheduler promptly advancing bounded pages; only the final page invokes the full repeated-read/physical closure verifier. A terminal failure found on any page still goes directly through the fenced finalizer. Driver/job progress handling and admission-only unit fixtures were updated, NOT RUN. The first complete sweep and each later retry still require physical closure before assembly; large-matrix timing and fairness remain unqualified.

Public Delivery download is still a confirmed cross-layer gap. `GetDelivery` and item inspection expose a manifest ref and export IDs, but the available user download-intent route is scoped to `download_jobs` volumes; `run.Outputs` excludes Delivery manifest/encoded and signed results. A signed assembly can therefore finish without a package-scoped user retrieval path. The implementation must authorize current tenant/session/project and published package state, bind requested slot to the adopted manifest and exact ready artifact version (signed result for required provenance, encoded export for off), issue a short-lived intent, and repeat authority after object signing/redeeming. Required negative cases: foreign package/slot, unpublished review, revoked project/session/policy, substituted signed artifact or object version, expired bytes, retry/idempotency and tenant purge. No completion credit; no code for this retrieval path was added in this audit turn.

Migration 01879 adds an API-only `v30_delivery_download_target` reader for a published, completed Delivery package. It repeats current tenant/session/project download permission, exact ready manifest, frozen policy and slot/export binding; required provenance selects only the adopted signed artifact from the retained closure and checks current policy publication, while off mode selects the encoded export. It returns versioned object coordinates only to `photo_api`; fabricated-scope and ACL cases were authored, NOT RUN, migration NOT APPLIED. This is not a user download path yet: token creation, one-use redemption with repeated live target authority, HTTP route and physical object-signing recheck remain open. No product completion credit.

Migration 01880 extends existing download intents with a closed `delivery` kind, package/slot/session identity and frozen artifact SHA/object version. The API-only creation function calls 01879 under current authority and writes a ≤60-second one-use token hash; volume/item intent shapes remain constrained separately. Fabricated-scope/role checks authored, NOT RUN; migration NOT APPLIED. Redemption must still re-read the target and compare all frozen bytes/version before consuming, and the HTTP/API idempotent creation path is not installed. No user-download completion credit.

Migration 01881 adds the `delivery` branch to the existing one-use bearer redemption with an exact predecessor-definition guard. Under the locked intent row it restores the stored tenant GUC, repeats 01879 using the original user/session/package/slot, compares artifact ID/SHA/object version and bucket, then consumes the token and returns a bounded filename and versioned object coordinates. Existing item/volume branches are retained. Migrated-definition/fabricated-token checks authored, NOT RUN; migration NOT APPLIED. User intent creation route and positive end-to-end redemption/object signing are still open, so no complete download credit.

`DownloadIntentService.Create` and `POST /delivery-packages/{id}/download-intent` now expose the package-scoped intent creation path with closed manifest/slot request shape, tenant-authenticated viewer scope, idempotency bound to the user session, a 45-second token and fresh target read before response. It reuses the existing one-use `/api/v1/downloads/redeem` endpoint after 01881. An unauthenticated-route unit case was authored, NOT RUN. Positive API→SQL→redeem→object signing, post-signing authority recheck, expired/revoked/idempotent replay and browser client flows remain open; no full download credit.

Migration 01882 adds `v30_delivery_download_intent_current` for a consumed Delivery token: it reuses the stored user/session/package/slot to repeat 01879 and compares ID, SHA, object version, bucket, key, size and MIME with the object just signed. `portal.Service.RedeemDownloadIntent` calls this predicate after external URL signing and refuses to return a link on revocation; existing item/volume tokens retain their previous authorization path. Fake-DB revoked-after-signing and migrated-DB unknown-token/ACL cases authored, NOT RUN; migration NOT APPLIED. Positive physical download and browser client remain unqualified; one-minute presigned URLs cannot be revoked after issuance by this check.

The photographer Delivery package screen now offers a manifest download and ready-slot download only for a published ready package. `OwnerAPI.downloadDelivery` requests the package-scoped one-use intent, redeems it through the existing public endpoint and checks expiry, SHA/size and HTTPS URL before opening it. Locales and unit/UI cases for scope, unsafe URLs and visibility are authored, NOT RUN. Migrated SQL, native/web browser behavior, actual object retrieval and revocation/concurrency acceptance remain unverified; this does not certify full V30 delivery.

The client download response now binds a manifest link's SHA to the displayed package document and refuses oversized files, long URLs and links with less than five seconds to expiry. The run route inventory includes the package read, item read, intent and redemption calls. Tests authored, NOT RUN; no claim of downloaded-byte verification or whole-module completion.

The 01879 package target now repeats live required signing-policy readiness before either manifest or slot selection. Previously the check was inside the signed-slot branch, allowing a manifest intent after policy revocation. The migrated-definition gate-order assertion is authored, NOT RUN; migration 01879 NOT APPLIED. Positive revocation races and physical object retrieval remain for the testing phase.

The same pre-selection gate now requires the retained closure for a required-provenance package, with its SHA and tenant/actor/parent/policy identity, before either manifest or slot download. A signed slot additionally repeats the adopted signer task/result, workflow/run actor and object-key/fence chain against the selected artifact. Definition assertions authored, NOT RUN; migration 01879 NOT APPLIED. Positive migrated fixtures and physical byte retrieval remain open.

Delivery intent creation now compares the saved one-use row's user/session/package/slot, artifact ID, SHA, object version and unused state with the current download target on every idempotent response. A replay after consumption or object replacement cannot return an apparently valid old bearer. Code authored, NOT RUN; live concurrent replay and full browser/object-store acceptance remain open.

The shared redemption service now calls the object store's versioned `Check` before signing any download URL. Production `Check` matches exact version, byte count, media type and SHA metadata against the SQL-selected object. A failed physical check returns no URL; unit cases for check-before-sign and missing version are authored, NOT RUN. This is a bounded metadata/head check, not a full byte reread; actual download/hash and migrated-DB acceptance remain open.

The V30 OpenAPI contract and API behavior chapter now describe the package-scoped intent and common one-use redemption shapes, auth/CSRF, replay limits, live target/object checks and the presigned-URL revocation limit. Contracts authored, NOT VALIDATED; runtime and full-module acceptance remain open.

The Delivery slot download target now requires a current export producer chain for both provenance modes: the item's frozen preset SHA must equal the export preset hash; the export task/result must match attempt and fence in a sealed, noncancelled child run for the same project/snapshot/actor; the encoded artifact must be ready, verified and produced by that exact task/result; and adoption inbox evidence must exist. A migrated-function definition assertion was authored, NOT RUN. This closes an otherwise weaker off-mode file selection and protects the signed branch's original export dependency. Migration 01879 remains NOT APPLIED; positive package download and object-store acceptance remain open.

The same target now derives the row-major slot index from the frozen preset order and requested ordinal, then requires the deterministic child run and export ID plus the child's frozen parent/slot/snapshot/preset/required/asset-version request. This prevents a same-project export from being substituted for another package slot even when its task/result/artifact chain is internally valid. The installed-definition assertion was extended, NOT RUN; migration 01879 remains NOT APPLIED.
