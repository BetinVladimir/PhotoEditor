# Поведение сервисов и клиентские сценарии

Точный HTTP контракт: [openapi.json](contracts/openapi.json), список методов: [endpoints.json](contracts/endpoints.json), JSON: [schemas.json](contracts/schemas.json). Это additions к существующему API, не его замена. По умолчанию user Bearer; web mutation также существующие Origin/CSRF проверки; refresh cookie не является самостоятельной авторизацией API. Device endpoints используют отдельный aud=capture, не user JWT.

Каждая mutation кроме публичных challenge/token требует Idempotency-Key. Проверять auth/ACL до поиска replay; ключ (tenant,principal,route,key), JCS hash body, одинаковый replay возвращает исходный status/body, другой hash409. CAS bigint как decimal string, диапазон1..9223372036854775807; только первое ReplaceDraft допускает0. Sequence допускает0. Невалидный JSON400, hidden resource404, forbidden403, stale409, invalid schema422, quota429, unavailable503. Все NaN, duplicate JSON keys и неизвестные поля reject до hash.

Resource содержит id, revision и data — закрытая Row-схема с resource_type. Для immutable rows revision="1". document nullable ArtifactRef указывает связанный canonical документ, не произвольный JSON dump строки. Маппинг: analysis→plan_artifact, draft→current document artifact, alert→report, shot_list→document artifact, learning version→evaluation, target→candidate, delivery→готовый manifest, interchange→report; остальные null. GET v2-documents/{id} возвращает EditingRecipeV3/LayerStack/ShotList после актуальной проверки project/consent. ArtifactRef читается существующим authorized artifact download intent; прямой публичный storage GET не добавлять.

GET pages limit1..100 default50, stable created_at,id ascending; cursor HMAC(tenant,principal,route,filter hash,last tuple), expiry15min; запрос cursor с другим scope422. Все IDs из body проверяются на принадлежность URL tenant/project, не только FK. ListSessions требует project_id query. Для списка persons/consents project задан URL.

## Методы и логика

| Метод | Транзакция и результат |
|---|---|
| CreateSession | Editor, collection принадлежит project; policy квалифицирован; создать open session, consent_epoch1. Возврат row, revision1. |
| GetSession/ListSessions | Project ACL; текущая session row; page stable. Никаких implicit writes. |
| UpdateSession | CAS; open→closed→archived, closed→open явно; archived terminal. Policy замена создаёт новую revision, старые analyses pinned. |
| AnalyzeSession | CAS session, frozen snapshot той же collection, manual refs validation09; AdmitV2 session; не мутировать current assets. |
| GetAnalysis | Возвращает adopted plan ref; до adoption404, workflow статус отдельно. |
| ApplyAnalysis | exact plan SHA и approved_groups; reject duplicate/unknown group; `recipe` — export artifact принятого StyleV2 в том же project с SHA=config_hash; новые source-bound OverrideV1 из CorrectionPlan только выбранным groups; atomic child Apply admission09. |
| RecordCorrection | CorrectionCreate; load approved/base docs, compute allowlisted diff; derive actor/project; client_event_id unique tenant, same payload replay; consent validation11. |
| CreateProfile/GetProfile | mode/person consistency; created_by=principal, active=null; Get consent_state вычисляется по grants, не stale cache. |
| TrainProfile | lock profile CAS, только requested sessions текущего project; freeze по10; insufficient422; manifest+lineage+AdmitV2 learning. |
| ListVersions | Только versions данного profile; LearningVersionPage, revision=version_eligibility.revision, evaluation ref, eligibility и review_id по15. |
| ActivateProfile | creator или Owner; candidate eligible, epochs valid; CAS, activation event; rollback не обходит safety/consent. |
| ChangeConsent/ListConsents | Owner; grant row bootstrap в project/person provisioning; CAS existing purpose/person grant; алгоритм11. |
| CreatePerson/ListPersons | Editor, project scope; псевдоним, state active; grants revoked; suggestions не считаются confirmed. |
| ConfirmObservation | CAS person; evidence содержит exact face/version; active matching consent epoch; upsert same association idempotently, conflict different person409; human actor server-side. |
| RevokePerson | Owner CAS; state revoked, revoke всех person grants, остановить matching/learning; originals сохраняются11. |
| ReplaceShotList/GetShotList | CAS session revision; unique requirement IDs, persons того же project; immutable ShotList document; новый pointer, session revision++; GET absent→empty requirements revision=session revision. |
| RegisterDevice | Admin; Ed25519 public key canonical32 bytes, private key только на устройстве; epoch1, active. Никакого дополнительного plaintext shared credential. |
| RevokeDevice | Admin CAS; state revoked, credential_epoch++; текущие tokens немедленно invalid при каждой mutation; idempotent revocation. |
| CreateChallenge | Body device_id совпадает URL; nonce32 CSPRNG, expires60s, store SHA; для unknown/revoked одинаковый response shape с random challenge, token всегда401. Rate-limit scope IP+device. |
| ExchangeChallenge | Ed25519 verify JCS(device_id,challenge_id,nonce,epoch,server_origin); nonce предъявляется через сохранённый server challenge payload, хранить nonce encrypted либо reconstructible HMAC, один hash недостаточен. Consume в TX один раз, active/epoch repeat; token15min aud=capture. |
| AcceptReceipt | Device scope совпадает session; verified upload same project+SHA; unique(device,sequence), other SHA409; existing ingest admission, receipt_assets только после canonical ingest. |

Device multipart transport реализуется отдельным `/api/v1/tenants/{t}/capture-uploads` (POST) и `/capture-uploads/{id}` (GET), `/parts` (POST), `/complete` (POST). Все четыре маршрута требуют Bearer `aud=capture`; Begin содержит `session_id`, `project_id`, `collection_id` и обычные поля `BeginRequest`. Проект должен совпадать с токеном, сессия быть открытой с `capture_policy`, а коллекция совпадать с сессией. Begin/Complete используют `Idempotency-Key`; Get/Parts возвращают только upload своего device и credential epoch. Любая запись повторно проверяет active/epoch под транзакционной блокировкой через `capture_device_admit_tx`. Уже принятая Complete-задача завершается асинхронно после revoke, но новый токен старой epoch или новая мутация отклоняются. Receipt API `/shoot-sessions/{id}/capture-receipts` создаёт receipt и receipt_assets атомарно после verified canonical ingest; завершение upload само по себе не создаёт receipt. Повтор device/sequence с тем же SHA и session возвращает исходный receipt, другой SHA или session —409. HTTP replay также повторяет active/epoch admission.
| ListAlerts/Acknowledge | Project ACL; immutable report; ack CAS и actor/time одновременно; replay не меняет время. |
| ReplaceDraft | Source version current и immutable binding; first save semantics03; validate recipe и LayerStack; сохранить слои artifact, заменить recipe.layer_stack exact ref; никаких двух расходящихся stack. |
| GetDraft/ListDraftHistory | Current recipe или immutable versions; GET absent404; history refs остаются pinned, page. |
| ApproveDraft | Документ=current stream и expected revision; audit human approval; approved pointer CAS; correction запись отдельным idempotent запросом после успеха. |
| GetDocument | ACL по document stream, disabled learning dependency409; schema из document_type, verify digest, возврат decoded body. |
| PreviewRegion | Source binding, rect bounds, recipe permission; admit preview; latest requested revision wins только на клиенте, older result не затирает UI. |
| Propose/ListTargets | Intent strict; infer project из reference, targets same project; immutable snapshot; transfer+assembly workflow; list explicit unsupported reasons. |
| ApplyApproved | plan SHA, unique target_decisions≤100; только listed approved targets; record batch и child Apply atomically; parent остаётся waiting_review до CloseSemanticReview15, повтор решения409. |
| CreateDelivery/GetDelivery | Freeze snapshot/preset configs; disjoint required/optional, ≤8 каждого; existing exports; manifest only full required success. |
| PublishDelivery | CAS; required ready, explicit review acceptance в этой команде при package=review, provenance policy satisfied; один commit publication+audit; email default off. |
| CreateInterchange/GetInterchange | Tagged union import/export, only compatible fields; XML/ZIP limits06; import draft proposals only, no automatic overwrite; report unsupported mapping. |
| SignExport | Existing immutable ready export; policy qualified and key available; dedicated signer workflow; verify signed bytes before adoption. |
| GetWorkflow/CancelWorkflow | Project ACL, authoritative runs.status; cancel CAS run.revision по09; terminal replay возвращает текущий статус. |

Challenge persistence: capture_challenges дополнительно nonce_ciphertext bytea NOT NULL и key_version text NOT NULL; encrypt AEAD server auth key, aad=tenant/device/challenge/epoch. Hash используется для compare, ciphertext — для signature verification. TTL cleanup после expires+15min, узкая auth maintenance функция. Nonce и signatures не логировать.

## UI завершённые цепочки

Session create → list/get → policy edit → analysis → scene board/reference → новый analysis → approve groups → child Apply status. Learning: save draft → approve → CorrectionCreate → consent → train → evaluation → explicit activate/rollback. Person: create → consent → confirm observation → shot requirements; revoke доступен на той же странице. Layers: modify local stack → preview → PUT draft CAS → history/approve; reconnect конфликт показывает обе версии. Capture: device keypair → admin register → challenge/token → multipart ingest → receipt → QA/ack. У каждой mutation обязательны loading, success, retryable error, unavailable и conflict states с RU/BG/EN.

## Output policies

Каждая операция имеет строго указанные роли contracts/operations.json. document: application/json≤8MiB, Pydantic output schema; working_image: существующий WorkingImage contract и его pixel artifacts, canvas≤200MP; changed/generated mask: existing MaskRef, same source/canvas; report: strict report schema; signed_export: тот же квалифицированный MIME исходного экспорта и reservation; manifest: JSON/C2PA по signer contract. Общий byte cap — дополнительный guard, не замена byte reservation и per-role checks.

Вложенный artifact также должен принадлежать declared staged output bundle либо declared input, с exact SHA. Root envelope — отдельный обязательный artifact роли document и входит в max_output_count; multiplicity и input reuse определены15. Операция не может опубликовать произвольную nested ref. Технические assembly output documents перечислены в schemas.json. Никаких production test stubs.


JobStatus.revision — текущая runs.revision для CancelWorkflow CAS. ApplyAnalysis возвращает JobTicket с run_id дочернего legacy Apply; ApplyApproved возвращает ReviewBatchResult с child nullable и workflow_id родительского v2 planning workflow; статус дочернего исполнения читается существующим run API по run_id. GET v2-workflows всегда относится к родительскому orchestration. Событие завершения родителя не закрывает progress дочернего Apply в UI.

Snapshot и retained active capture window ограничены5000 frames; долгую съёмку разбивать на последовательные snapshots, receipt.sequence продолжает расти. В Train/Analysis/Semantic IDs уникальны; schema array bounds недостаточно — semantic validators проверяют distinct IDs, membership, reference_ordinal<len(frames), manual convex corners и enum-dependent params. Body revision никогда не берётся из actor/client metadata.

V24 `ConfirmFrame`: POST `/api/v1/tenants/{t}/capture-receipts/{id}/confirm`
with CaptureFrameConfirm and Idempotency-Key. Requires editor project access.
Bind confirmation to the exact receipt asset version and adopted alert report;
freeze previously confirmed people and their identity consent epochs. A repeat
with the same alert returns the same confirmation; another alert conflicts.
Confirmation never sets or overrides technical quality. A null quality remains
unknown and cannot contribute to shot coverage. Archived sessions reject new
confirmation. Alert acknowledgement is a separate action and does not confirm
a frame. The response is CaptureFrameConfirmation.


V24 `ListAlerts` выдаёт новые уведомления первыми: `created_at DESC, id DESC`. Cursor подписан для tenant/principal/session/filter и sort=`capture-alerts-created-id-desc`; продолжение использует `(created_at,id)<last_position`. Старые cursor с ascending sort несовместимы и возвращают `INVALID_CURSOR` (400); клиент получает новую первую страницу. Этот порядок требуется SSE/polling delta reader: он читает страницы до самого старого уже показанного alert, сохраняя историю и обновляя подтверждения/acknowledgement во всём загруженном диапазоне. Качество и приватные поля evidence не входят в публичную проекцию.


V24 GC roots: `capture_alerts.report_artifact_id` и calibration refs в `shoot_sessions.policy/capture_policy` входят в `v2_artifact_has_references`. Защита действует независимо от workflow artifact links; ссылка должна исчезнуть через разрешённый lifecycle/purge перед GC. Shot-list документы защищаются существующим root `v2_documents` и `v2_document_refs`.

`ListAlerts` returns `CaptureAlertPage`, whose public `CaptureAlertView` includes nullable `confirmation_id`. It resolves only an immutable confirmation matching tenant/project/session/receipt/alert/asset version under the existing project ACL. It exposes no confirmation identity evidence or person bindings. A confirmation may appear without an alert revision increment; client merge retains known immutable confirmations through stale reads and acknowledgment responses. Initial loads and manual/polling reads restore confirmation state from the server; local UI memory is not its authority. Acknowledgment remains independent of frame confirmation.

## V26 LayerStack artifact reference read

`GET /api/v1/tenants/{t}/assets/{id}/v2-layers/{artifact}?sha256=<lowercase hex64>` resolves the exact LayerStack artifact reference within the current asset project. Editor authorization and an existing current V2 draft stream are required. The server returns verified canonical LayerStack JSON only when it binds to the current accepted WorkingImage. Invalid reference →422, missing/foreign document →404, unavailable dependencies →503; response is no-store. The client validates the canonical document SHA against the recipe reference and the full source/canvas binding before installing a stack. No artifact ID is interpreted as a document ID.

### V26: GET region-preview descriptor

`GET /api/v1/tenants/{t}/assets/{id}/region-previews/{run}/descriptor` returns `RegionPreviewDescriptor` (HTTP 200) to a currently authorized tenant/project editor. It is private/no-store. No idempotency key is required for this read. The run must be a sealed, noncancelled V2 preview with the exact stored region task, matching runtime profile, complete ready input closure and verified version-pinned nonexpired Params. Source and frozen-recipe metadata must independently pass current authorized lookup. A missing run, wrong asset, invalid immutable evidence or absent scoped evidence returns 404; missing configured object reader returns 503. Tenant authentication/authorization retains the standard 401/403 behavior.

The server collects internal node and object metadata in a tenant-scoped DB-only transaction, reads bounded source and frozen-recipe documents at their exact object versions outside SQL transactions, checks actual byte digests and rebuilds the exact original plan. It repeats live scope and input proofs after IO and requires unchanged evidence before returning. The response exposes asset, submitted request, original source ref, verified source canvas dimensions, Params SHA, server sRGB output ICC SHA and alpha; never expose object keys, buckets, object versions, full recipes or model inventory. Canvas dimensions must be positive safe integers with total area at most 200 million pixels. They describe the source canvas currently supported by RegionPreview preparation; recipe geometry changes require a qualified compiler which supplies the final canvas consistently to both rendering and descriptor.

The client freezes its expected request/source before awaiting the read. It rejects additional fields, altered asset/source/request, malformed SHA, invalid canvas geometry or alpha type. The admitted Params and ICC SHA become independent expectations for manifest verification; never fill these expectations from the returned render manifest. Descriptor availability does not qualify the renderer or enable region.preview. A preview of the saved V2 recipe must not be represented as including unsaved local changes.
