# 04. REST и прикладные сервисы

## Правила HTTP

Полный реестр: `contracts/endpoints.json`; URI не сокращать при генерации OpenAPI 3.1. Все тела JSON strict, неизвестные поля 422. OpenAPI — источник TS SDK и Go transport DTO; Python schemas публикуются как references с version/hash. Нельзя вручную дублировать client types. Внутренние database IDs не показывать в пользовательских сообщениях, но возвращать API.

Списки: `limit` 1..100 default50, opaque HMAC cursor `{tenant,principal,filter_hash,sort,last_position,last_id,revision}`. Коллекция изменилась →409 SNAPSHOT_STALE, клиент сохраняет explicit selection IDs и обновляет страницы; selection-all всегда server snapshot. Сортировка стабильна `(position,id)` или `(created_at,id)`. Cursor не является разрешением доступа. `If-Match: "revision"` обязателен для PATCH/PUT; body expected_revision допустим для actions; при обоих значения совпадают. Отсутствие обязательной revision →428.

Ошибка `{code,message_key,args,request_id,details?}`: 400 malformed JSON, 401 invalid session, 403 action denied, 404 hidden/missing, 409 revision/idempotency/reference conflict, 413 bytes limit, 415 format, 422 semantic validation, 428 missing precondition, 429 rate limit, 503 dependency unavailable. `details` только безопасные field errors, не stack/SQL/object paths. API 202 означает SQL commit job+outbox, не окончание обработки.

Idempotency-Key 1..128 ASCII обязателен для POST создающих persistent ресурс/job, кроме OTP/guest exchange (их собственные rate/single-use semantics). Scope tenant+principal+route+key, hash canonical body. Запись вместе с результатом в одной tx. Резервированные keys run/upload/download сохраняются минимум retention ресурса и 30 дней после terminal; остальные 24h. Повтор возвращает тот же resource даже после failed, не создаёт новый. Secret share create — исключение A03-security: plaintext не кэшировать, repeat без secret. External S3 side effects оформляются saga, API не держит SQL tx на сети.

## DTO и методы сервисов

Полные persistable модели заданы в 02/15. Response DTO — allowlist полей соответствующей сущности, не `SELECT *` serialization. Secret_hash, credentials, internal object keys, encrypted payloads никогда не входят response. Ниже нормативные request types; `ID`, `Revision`, `Hash`, `JSON` определены в 15.

| DTO | Поля / проверка |
|---|---|
| OTPRequest / OTPVerify | email, locale / challenge_id, code regexp ^[0-9]{6}$, client_type web/native |
| UserPreferences | locale ru/bg/en, theme light/dark/system, timezone IANA; только эти поля |
| CreateTenant / TenantPatch | name 1..120 / expected_revision, name?, quotas?, retention?; quotas не выше system limits |
| TransferOwner / MemberPatch | target_user_id, expected_revision / role admin/editor/viewer, status active/suspended |
| InviteRequest / InviteAccept | email, role, locale / token; token CSPRNG32 hash only TTL7d |
| ProjectCreate/Patch/Grant | name, description<=2000, visibility workspace/restricted, download_allowed / expected_revision+partial / role viewer/editor |
| CollectionCreate | project_id,name,description?,initial_share?:ShareCreate; create collection+share в одной tx; initial_share публикует пустую collection |
| CollectionPatch | expected_revision,name?,description?,cover_item_id?,sort_mode?,state?; published только через PublicationRequest; archive отзывает shares |
| ItemAdd / ItemPatch | expected_revision,asset_version_id,section_id?,position? / expected_revision,rating?,flag?,tags?,section_id?,position?; asset_version_id update forbidden |
| SectionCreate/Patch | expected_revision,name,position; deletion переносит items в root section атомарно |
| PublicationRequest | expected_revision,item_ids (1..500),publish:boolean; item ready+accepted; update collection state явным publish_collection:boolean |
| SelectionRequest | collection_revision,scope selected/all/filter,item_ids? OR filter?; limit total system quota; freeze ordered IDs |
| BulkRequest | expected_revision,selection_snapshot_id,action,params; action rating/flag/tag/hide/publish/copy/move/remove/retry_media; render-actions через Run.Apply |
| ShareCreate/Patch | label,expires_at (default30d, explicit null allowed),permissions{view,download,upload},limits{max_upload_files,max_upload_bytes_total,max_file_bytes,allowed_formats},moderation,allowed_presets; patch expected_revision=policy_version |
| ShareExchange | share_id,secret,locale; tenant/collection запрещены |
| UploadBegin | project_id,collection_id?,filename,bytes,format,assume_profile?,sha256?; guest не передаёт project/collection/share — scope из session |
| UploadPartRequest / Complete | part_numbers 1..10000 максимум100 за вызов / ordered parts[{part_no,etag}],expected_size; object_key запрещён |
| ModerationRequest | decision accept/reject,expected_revision; acceptance только verified; publish отдельно |
| StyleCreate/Patch | project_id,name,description / expected_revision,name?,description?,archived? |
| TrainingSetCreate / PairCreate/Patch | Empty / before_asset_version_id,after_asset_version_id,session_id,confirmed,included,ignore_mask_artifact_id?,expected_revision; пары immutable в run snapshot |
| AnalyzeRequest | training_set_id,training_set_revision,analysis_options{alignment_mode,geometry_enabled,seed},holdout_snapshot_id?; require ≥1 confirmed included |
| AcceptStyleRequest | candidate_run_id,expected_style_revision,decision=accept,acknowledge_low_confidence:boolean; candidate completed/awaiting profile review, same style/tenant |
| StyleImportRequest | project_id,name,upload_artifact_id,assets_manifest_artifact_id?; validate asynchronously, auto executable code forbidden |
| ApplyRequest | source_revision,style_version_id,scope all/selected/all_change_selected,item_ids? xor selection_snapshot_id?,strength 0..1,overrides_id?,job_overrides?,output_name?; in_place/target_collection_id запрещены |
| RunCreate | discriminated kind analyze/apply/cull/preview/export и соответствующий DTO; all apply paths вызывают один service method |
| RetryRequest | task_ids? only failed/retryable,expected_revision; unchanged snapshot/model hashes; succeeded immutable; explicit retry increments attempt same output collection |
| ReviewDecision | expected_revision,candidate_hash,decision accept/reject/skip,accepted_candidate_ids?,rejected_candidate_ids?; batch — explicit review IDs, no wildcard |
| OverrideCreate | asset_version_id,input_sha256,base_revision?,params:OverrideV1; source mismatch409; every mask binding must be a canonical same-version interactive mask artifact or `OVERRIDE_MASK_INVALID`409 |
| DraftSave / PreviewRequest | expected_revision,asset_version_id,params,mask_refs / asset_version_id,draft_revision,style_version_id?,params,roi?,scale proxy/full; generation=false unless explicit generate action |
| ExportRequest / PresetCreate | snapshot_id,presets / name,config:ExportPreset; pixels-changing request создаёт новую collection, download preset branch — derivative того же version |
| DownloadRequest | collection_id owner only,collection_revision,scope selected/all,item_ids? xor selection_snapshot_id?,preset_id; nonempty snapshot |
| SingleDownloadRequest / VolumeRequest | preset_id / volume_no>=1; ready and policy recheck |
| MediaLinkRequest | asset_version_id,variants 1..4; guest request item_ids<=100,variants; permitted derivative only |

Owner POST `/assets/{id}/media-links` требует активную user session, Origin/CSRF для web, tenant/project viewer ACL и точное соответствие path asset с request asset_version. Разрешены только `thumb320|thumb640|preview1600|preview2560`; duplicate/unknown variants отклоняются. Для каждой пары вычисляется тот же RFC8785 profile hash, что проверяет worker, и выбирается только ready unexpired artifact текущего профиля в configured bucket с exact object version. Все строки разрешаются до подписи; отсутствующая производная даёт `MEDIA_NOT_READY` без master fallback и без частичного ответа. URL закреплён versionId и живёт10min.
| DeleteRequest | expected_revision,confirm_resource_id; deletion explicit, checks refs before scheduling |
| EventQuery | after decimal cursor,limit<=100; expired cursor410 EVENTS_EXPIRED → full refetch |

PageQuery includes cursor/limit/search<=200; specialized filters whitelist project_id/status/rating/flag/tag/section and sort. No arbitrary SQL field/filter expressions. Response `Page<T>={items,next_cursor,revision?}`; `RunAccepted={run_id,status}`, ApplyAccepted adds output_collection_id; DownloadAccepted adds download_job_id. Empty=empty body. Remaining response names map directly to sanitized entity or status aggregate in 02; structured variants in 15.

## Транзакционные прикладные методы

Все методы имеют Go `(ctx context.Context, principal Principal, req T) (R,error)`; Get/List без мутаций. HTTP handler только decode→service→map error. Внутренние helpers принимают TenantTx и никогда не начинают вложенную transaction.

`TenantService.Create/Update/Delete`, `Invite/AcceptInvite` подключены к публичным маршрутам. Create использует global auth-scoped idempotency до появления tenant; update проверяет revision и bounded quotas/retention; delete возвращает202 с устойчивым deletion job и блокирует новый tenant work статусом deleting. Invite создаёт только admin/editor/viewer membership intent, шифрует mail payload до SQL-вызова и не возвращает bearer token через API. Accept выдаёт membership только пользователю с тем же normalized email. Реализовано безопасное ядро physical cleanup: `TenantCleaner` принимает только fenced UUID ticket, удаляет bounded batch≤256 и делает checkpoint до завершения; MinIO `PurgeTenantBatch` удаляет exact versions и incomplete uploads строго под `tenants/{uuid}/`, повторно проверяет пустоту prefix и не затрагивает соседний tenant. SQL claim/checkpoint/final row purge и подключение job к maintenance loop ещё не реализованы, поэтому202 пока не означает полное удаление данных.

`CollectionService.Create`: ACL project→quota→collection insert→optional share→audit→idempotency response. `Update/AddItem/RemoveItem/Publish`: lock revision→checks→mutation→revision+event+audit. `Archive`: state archived, shares revoke/version++, cancel guest jobs, soft-delete memberships только при отдельном delete.

Текущая реализация `backend/internal/collection` предоставляет list/get/create/update, list/add/update/remove item, section create/update/delete и publication. Все чтения повторно применяют tenant и project ACL; mutations сериализуются блокировкой collection и её revision. AddItem и CreateSection приобретают idempotency record до domain row lock, а replay всё равно проверяет текущий project ACL. Версия asset должна принадлежать тому же project, asset должен быть verified, master artifact — ready и не expired. Item patch не позволяет менять asset/version, строго проверяет rating/flag/tags/section/position; удаление cover item требует сначала сменить cover. Удаление section атомарно переносит items в root. Cursor связан с tenant, principal, collection и всеми фильтрами.

POST `/api/v1/tenants/{t}/collections/{id}/items/{item}/moderation` принимает `ItemModeration={expected_revision,decision:accept|reject}` только для immutable версии, созданной тем же `apply` run, который владеет output collection. Collection, item и version блокируются в одной transaction; решение синхронно переводит `asset_versions.review_state` и `collection_items.moderation_state` в accepted/rejected, оставляя item hidden до явной publication. Повтор того же решения при текущей revision возвращает тот же item, конфликтующее или устаревшее решение возвращает409. Переход pending_review→accepted через migration00156 атомарно ставит version в durable media queue; claim повторно доказывает apply-run provenance, accepted version и live verified asset до любого object I/O.

Publication принимает 1..500 уникальных явных item IDs, сортирует их перед блокировкой и требует точного совпадения найденного набора. Публикация разрешена только для verified asset, accepted version и ready/non-expired master; revoke не требует уже доступного master. В одной транзакции upsert publication увеличивает policy_version, меняет item visibility, опционально переводит collection draft↔published и увеличивает collection revision.

Share list/create/update/revoke подключены к owner API. Create нормализует и ограничивает permissions, upload quotas/formats, moderation, expiry и до100 active preset IDs. Bearer secret детерминированно выводится HMAC из server pepper и нового share UUID; БД хранит только отдельный domain-separated MAC. Idempotency response сохраняет sanitized Share без bearer: первый ответ получает вычисленный secret, replay — тот же ресурс без secret, как требует A03. Создание share публикует draft collection; patch увеличивает policy_version, отзывает старые sessions и отменяет незавершённые guest downloads. Revoke идемпотентно ставит revoked_at, отзывает sessions и отменяет jobs.

`CollectionCreate.initial_share` использует тот же internal insert primitive внутри idempotency callback: collection, initial link, публикация пустой collection, audit и сохранённый sanitized response имеют один commit. Первый ответ дополняется transient secret после `idempotency.Use`; replay читает сохранённый response без секрета. `DELETE collection` проверяет одновременно If-Match, body expected_revision и confirm_resource_id, переводит collection в archived, отзывает links/sessions, отменяет guest downloads, делает guest multipart недоступными и создаёт durable deletion job; физическое lifecycle удаление остаётся задачей maintenance.

Owner `selection-snapshots` поддерживает selected/all/filter по section/rating/flag/tag. Нормализованный request является idempotency identity; selected IDs сортируются и проверяются на уникальность. Под collection row lock сервис сверяет source revision, заново применяет viewer project ACL, загружает максимум5000 текущих verified asset/version/ready-master bindings в порядке `(position,id)`, требует полный selected set, вычисляет SHA256 JCS полного списка и атомарно записывает snapshot и snapshot_items с exact artifact refs. Snapshot принадлежит user principal и истекает через24h.

Bulk start/status/cancel API создаёт durable `runs(kind=bulk)` и `bulk_operations` из exact user-owned non-expired selection snapshot. Request params имеют отдельные strict схемы для rating/flag/tag/hide/publish/copy/move/remove/retry_media; unknown/null/extra поля отклоняются. Copy/move заранее проверяет обе project ACL, одинаковый project и блокирует коллекции в UUID order. Idempotency сохраняется30 дней. Status считает outcomes и выдаёт их подписанной ordinal pagination; cancel немедленно завершает ещё queued operation либо переводит running в cancelling вместе с run. Миграция00095 даёт scheduler только атомарную постановку одного fenced command; отдельная роль `collection-worker` последовательно потребляет `q.worker.collection` и выполняет bounded chunks до500 элементов с UUID-ordered collection locks, source/target revision fences, item savepoints, terminal run state и полным отчётом хвоста при cancel/revision conflict. Повторный event после committed chunk становится no-op. Metadata, publication, copy/move/remove применяются в транзакции chunk; retry_media пока только создаёт durable command и не считается завершённым без потребителя media retry. BULK01 подтверждён живой проверкой PostgreSQL между чанками и отдельным сквозным RabbitMQ→collection-worker→PostgreSQL тестом.

`StyleService.Analyze` реализован как идемпотентная атомарная фиксация входа: под блокировкой style и training set проверяются project editor ACL, точная revision, состояние `draft`, 1..32 included+confirmed пары, optional holdout snapshot и доступные квалифицированные fit/aggregate workers. Обе версии каждой пары должны принадлежать project стиля, быть verified и ссылаться на ready/non-expired master manifest с непустым object version. В immutable training snapshot сохраняются session, exact artifact ID/SHA/bucket/key/version/bytes/media type и optional ignore mask; digest считается по JCS всего упорядоченного набора. Fit backend и SHA256 runtime locks выбираются из свежих qualified registrations и замораживаются в config snapshot. Та же транзакция создаёт `runs(kind=analyze,status=queued)` и `style_analysis_builds(phase=registration)`.

Миграции00118–00119,00148 и00172 вместе со `StyleAnalysisCoordinator` исполняют versioned детерминированные графы. Graph v3 при `geometry_enabled=false` начинает с `registration→color_fit`, а при `geometry_enabled=true` — с `registration→features(mask.person+pose.detect для обеих сторон)→body_fit→geometry→geometry_registration→color_fit`. Обе ветви затем проходят `face_detect→face_semantics(face.landmarks+face.parse)→face_skin→effects_fit(pair.fit.beauty+pair.fit.defects)→aggregate`. При наличии отдельного immutable holdout snapshot добавляются `holdout_features(mask.person)→holdout_registration→validate→package`; без holdout aggregate завершает run с явно unvalidated candidate. `geometry.body` получает immutable `body_fit` artifact и самостоятельно проверяет его binding к before image, pose и person mask; коэффициенты не копируются и не интерпретируются scheduler-ом. Единственный pixel resample выполняется body warp, после него `pair.register` только измеряет новую similarity и связывает alignment с новым WorkingImage. Перед каждой стадией run открывается лишь после точного числа succeeded canonical predecessors: pair_count, 4×pair_count для feature epoch и один aggregate. Coordinator читает version-pinned JSON manifests, проверяет bytes/SHA/JCS/canvas, объявляет не только manifests, но и каждый nested pixel/alpha/mask artifact в task inputs, создаёт deterministic immutable node plans, публикует params через quota-accounted fenced repository и только затем атомарно делает стадию ready и sealed. `style_analysis_builds.graph_version` сохраняет upgrade-инвариант: существующие runs v1/v2 сохраняют прежние переходы, новые runs получают v3; immutable частично исполненный граф не переписывается и не отменяется миграцией. Capability selection требует общий qualified backend/lock для color fit, person, face detect/landmarks/parse и pose при включённой геометрии; выбранный CPU runtime обязан поддерживать face-skin, beauty/defect evidence, aggregate, validate/package и body fit/warp для geometry. Partial publication безопасно replay. Optional training ignore mask связывается с after canvas/transform и исключается из spatial train/holdout population bounded блоками. Color fit остаётся global-only; beauty/defect evidence связывается с точной парой, image refs, landmarks, skin mask и canonical alignment, а aggregate принимает ровно один набор evidence на пару. `AcceptCandidate`: lock style revision, verify candidate structurally and ready, require acknowledgement if low confidence, version_no=max+1 under style lock, JCS config/hash and provenance; old version never UPDATE. `Import`: inspect package, validate model IDs/local asset binding, produce candidate, explicit save flow. `Export`: JCS bytes exactly from immutable JSON, hash compare, optional style-package assets.

Материализация результата `style.aggregate` реализована отдельно от worker commit: миграция00117 выбирает только два ready artifacts (`style_candidate`,`aggregate_report`), которые принадлежат succeeded aggregate task и перечислены его canonical result. Scheduler читает exact version из настроенного bucket, проверяет size/SHA/JCS, связывает `style_id` с run и завершает fenced lease. Невалидный документ переводит candidate в failed; сеть/lease допускают безопасный повтор. Готовый candidate переводит run в waiting_review. `AcceptCandidate` идемпотентно блокирует style revision и candidate, требует явный low-confidence acknowledgement, повторно сверяет JCS hash, вставляет новую immutable `style_versions` строку, отмечает candidate accepted и увеличивает style revision одной транзакцией.

`RunService.Apply` и общий `Run.Create(kind=apply)` используют одну реализацию. В одной idempotency transaction выполняются project editor ACL, блокировка и точная revision source collection, разрешение `all|selected|all_change_selected`, создание либо повторная проверка user-owned selection snapshot, проверка ready/version-pinned masters, загрузка только accepted style из того же project и повторная проверка его JCS hash. Accepted style обязан иметь ready version-pinned export artifact с тем же config hash. Style JSON/artifact, snapshot digest, strength, optional owned override config/hash и bounded job overrides замораживаются в run config. Для результатов резервируется storage quota по консервативной оценке `2×original bytes + 1 MiB/item`; derived outputs не расходуют quota исходных файлов. Gateway атомарно переводит каждый output из run reservation в конкретный upload без двойного увеличения `tenant_usage.reserved_bytes`; completion/cleanup списывает этот slice, а финализатор освобождает остаток. Затем создаются draft output collection, queued apply run и `apply_builds`, а для all_change_selected неизменённые memberships копируются без создания новых bytes. Source collection не меняется.

Миграции00120–00124 сохраняют совместимый Apply graph v3. Миграции00174–00187 добавляют forward-only graph v4: immutable plan с item/aggregate/review cardinality, versioned lease, plan-bound регистрацию узлов, exact canonical input/output bindings и переход только после полного succeeded epoch. Реализованные v4 ветви включают fixed/adaptive resolve, person mask и Stats20, restoration (`image.denoise` перед geometry/color и explicit `image.upscale` перед master quality), subject-safe background (`transparent|solid|blur`) после beauty/defects; blur предварительно запускает pinned `depth.predict` на тех же финальных pixels, geometry (`pose.detect→geometry.body→mask.transform`), color, series measure/balance, local, face semantics (`face.detect→face.landmarks→face.parse→mask.face_skin`), beauty, explicit-mask portrait detail, reviewed defects и master quality. Denoise и upscale требуют точных SCUNet/Real-ESRGAN weight IDs и одного свежего worker с общим qualified backend/runtime lock; upscale заранее резервирует scratch/output по выходной площади, запрещает результат больше 200 MP, а quality после изменения Canvas работает без несопоставимого baseline. Geometry и series повторно проверяют Canvas/TransformChain после warp; beauty и defects строят параметры из сохранённых face object IDs и bound skin mask. Создание run требует точных model weight IDs и свежих qualified runtime locks; скрытого fallback нет. Каждый node plan immutable и deterministic, params публикуются отдельным fenced/quota-accounted repository, а declared inputs включают nested pixel/alpha/mask artifacts. Defects review открывает один fully-bound review на каждый canonical proposal и переводит run в `waiting_review`. Continuation закрывает все expansions одной транзакцией, активирует только реальные repair tasks через `activate_bound_task`, ждёт canonical success и возвращает `repair|base|skip` как полный ordinal-упорядоченный поток; base/skip используют immutable review-bound base, repair — fenced worker output. Quality выполняется для всех ordinals, после чего skip-aware adoption не создаёт version/membership для явно пропущенных items. Для остальных graph v4 сохраняет exact `quality.params.target`, повторно доказывая canonical apply producer либо review-bound repair producer attempt/fence; graph v3 продолжает брать исторический `image.local`. Одна transaction создаёт новые `asset_version`, `asset_version_sources(role=apply_source)` и hidden/pending-review memberships, освобождает reservation и завершает run. V4 admission разрешает defects только с `policy=review`; любой иной policy отклоняется. Automatic portrait-detail region discovery, geometry-transformed override regions и background `asset` остаются вне graph v4. Composition исполняется цепочкой `composition.structure→composition.plan→composition_render(crop|outpaint)→composition_review`: structure детерминированно измеряет линии на bounded preview и сохраняет subject bbox/head protection из provenance-bound person mask; composition_render выбирает по immutable CompositionPlan ровно `geometry.crop` либо `image.outpaint`; обе ветви меняют Canvas одним matrix step и всегда проходят candidate/base/skip review перед дальнейшим upscale/quality. Outpaint разрешён только явной style-политикой `review` с prompt/seed/guidance, pinned SD1.5 lock и qualified `image.outpaint` backend. Explicit-mask `image.inpaint`, `image.generate` и generated background допускаются только с provenance-bound `inpaint_target|generation_target`, pinned model/runtime lock и обязательным per-item candidate review. Review атомарно фиксирует `candidate|base|skip`; следующий epoch получает единый ordinal-упорядоченный поток, а skip сохраняется до adoption. Для identity SaveEdits stage discovery использует canonical overlay `OverrideV1.modules` поверх identity StyleV2 до runtime admission и регистрации immutable plan: beauty, reviewed defects, geometry, restoration и composition не могут быть сохранены без соответствующей DAG-ветви. Model operations для explicit override требуют свежий worker, qualified operation/backend и единый runtime lock; style model-reference requirement применяется к переносимым StyleV2, а identity override опирается на установленный hash-locked runtime. Общий RunCreate пока отклоняет discriminator, отличный от apply, до появления соответствующих compiler-ов.

`ReviewService.Decide`: lock review revision; hash matches exact candidate; actor editor; decision immutable after acceptance; accept/reject updates routing choice and unblocks scheduler in same tx. Reject generation uses base branch; user can explicitly skip whole photo. For defects accept subset, one decision hash freezes candidate IDs. Для creative candidate решение дополнительно фиксирует exact candidate/base ArtifactRef в `candidate_review_v1`; candidate обязан быть `working_image` канонического succeeded task, а base — объявленным входом той же задачи. Candidate и base до commit проходят строгий WorkingImage parse и обязаны иметь одинаковые Canvas, ColorSpace, dtype и TransformChain.

`BulkService.Start`: snapshot and permissions validation in tx; scheduler chunks ≤500. `ApplyChunk`: recheck membership/link/project state, lock collections sorted UUID to avoid deadlocks, check expected_next_revision, apply each item savepoint, item report, increment revision, update expected_next_revision, outbox summary. Unrecoverable conflict stops later chunks; no automatic overwrite. Для `retry_media` migration00149 и collection worker принимают отдельную strict command только через exact outbox event: bulk action/params, frozen snapshot item+version и существующий succeeded intake result проверяются до durable `media_retry_jobs`. Event replay возвращает ту же intent, другой event для того же bulk item не создаёт дубль. Scheduler task materialization/final derivative outcome остаются следующей стадией.

`EventService.List`: filters by current principal ACL at read, not only event-time membership. Guest event serializer emits collection_changed/upload_status/download_status only for permitted collection and own sessions. Large backend counters computed SQL, no process-local authority.

### Owner multipart upload: начало и parts (реализовано частично, SQL77)

`UploadService.Begin` валидирует generated-format allowlist, соответствие suffix,
имя ≤255, размер 1..512 MiB, optional expected SHA и assume_profile. В tenant
transaction `Idempotency-Key` резервирует один стабильный upload ID; callback
проверяет project editor ACL/collection ownership, блокирует tenant_usage,
сверяет storage/source_files quotas, создаёт quota_reservation и session
`initializing`, увеличивает reserved bytes/files. Replay снова проверяет ACL и
возвращает тот же ресурс, не увеличивая counters.

После commit API начинает multipart вне SQL. Вторая tenant transaction под
upload lock прикрепляет multipart ID и переводит session в uploading. Проигравший
конкурентный start abort-ит только собственный multipart. Ошибка storage оставляет
initializing+reservation для безопасного resume. Клиент никогда не передаёт и
не получает object_key/multipart_id.

Реализованы owner endpoints POST `/uploads`, GET `/uploads/{id}` и POST
`/uploads/{id}/parts`. Parts: 1..10000, максимум100 уникальных номеров за запрос,
TTL10min; URL подписан под `S3_PUBLIC_ENDPOINT` и содержит точные uploadId/
partNumber. API runtime проверяет S3 readiness при startup.

POST `/uploads/{id}/complete` сохраняет строго последовательные parts и stable
artifact/snapshot/run IDs в одной idempotent transaction, переводит session в
completing и сразу возвращает202. Scheduler claim с60s lease выполняет S3
Complete, восстанавливает потерянный ответ через exact generated key HEAD,
читает закреплённую version полностью и вычисляет SHA-256. Heartbeat работает
во время object IO; потеря lease запрещает SQL finish.

Finish одним commit создаёт ready staging artifact, immutable input snapshot с
canonical per-file payload, queued ingest run и upload binding, затем переводит
upload в verifying. Вызов не использует binder с обратным run→upload lock:
binding вставляется после всех проверок в той же completion transaction. POST
`/abort` разрешён только initializing/uploading, снимает reserved bytes/files
ровно один раз, пишет audit и повторяемо abort-ит сохранённый multipart.

Unit tests покрывают public signing origin, границы parts, redaction, canonical
completion documents, lost Complete recovery и lease loss до side effect.

Несовпадение ожидаемого SHA после полного чтения версии является terminal
`upload.verify`: SQL79 сохраняет фактический digest и completed object как ready
staging artifact, создаёт sealed failed ingest run и rejection receipt, переводит
зарезервированные bytes в storage до подтверждённого удаления, освобождает file
reservation и ставит обычную staging cleanup retention. Клиентский SHA никогда
не становится metadata объекта. Тот же canonical payload/config validator
используется для успешного и ошибочного результата.

SQL80 закрывает expiry до completion. Maintenance атомарно переводит просроченные
initializing/uploading sessions в expired, освобождает byte/file reservation и
триггером создаёт durable multipart-abort ticket. Owner `/abort` создаёт такой же
ticket; немедленный S3 abort остаётся ускорением, но не единственным доказательством.
Maintenance повторяет abort, считает NoSuchUpload успехом и закрывает ticket по
lease token. Неопределённая storage ошибка не закрывает задание.

Guest session/upload transport ещё не подключён; SQL77–80 и live MinIO не проверены.

Owner moderation transport подключён к POST
`/api/v1/tenants/{t}/uploads/{id}/moderation`. Он принимает accept/reject и
точную upload revision, требует editor ACL, `verified` upload с guest principal,
share identity и существующий ingest binding/item. Project, collection и upload
блокируются до изменения. Решение переводит одновременно collection item и
asset version из pending_review в accepted/rejected, оставляет visibility hidden,
увеличивает collection/upload revisions и пишет audit. Повтор со старой revision
даёт conflict. Публикация выполняется отдельным endpoint и этим действием не
подразумевается. Guest session/upload transport всё ещё не подключён.

### Portal session boundary (SQL81)

POST `/api/v1/portal/sessions` обменивает share ID+secret на opaque HttpOnly
portal cookie и отдельный CSRF token. API использует отдельный pgx pool с ролью
`photo_portal`; у роли нет table grants, только EXECUTE проверенных SECURITY
DEFINER functions. Secret, session token и CSRF сохраняются только как
domain-separated HMAC-SHA256 с server pepper. Ответ не сериализует cookie token.

Exchange блокирует share, проверяет revocation/expiry, collection lifecycle и
фиксирует точную policy_version; session живёт максимум24h и не дольше share.
Каждый authorize повторно проверяет session/share expiry, revocation, collection
и совпадение policy_version, затем конкретное permission. Tenant/share/collection
никогда не принимаются от portal request как authority.

GET `/api/v1/portal/collection` возвращает function-only sanitized projection.
Migration00168 делает download-only ссылку blind: capability download остаётся true, но cover, sections и item-derived данные пусты; создать можно только scope=all snapshot. Selected/filter требуют view. GET/download presign живёт не более60s, multipart PUT — не более600s. SECURITY DEFINER функции используют явно установленный `public.digest`, а portal role не имеет table grants.

Для upload-only ссылки cover, sections и item-derived данные пусты. View/download
становятся effective только для published collection; cover существует только
при visible+accepted+published item. Поля camera/GPS/internal review отсутствуют.
На уровне SQL81 был подключён только session/read boundary; multipart добавлен
следующими SQL82–83. Live/RLS проверка остаётся недоступна.

SQL82 подключает guest Begin/Get/Parts/Abort. Begin request содержит только file
metadata; tenant/project/collection/share/principal/object key всегда выводятся
из текущей portal session. Function повторно проверяет session CSRF, link expiry/
revocation/version, upload permission, collection lifecycle, allowed_formats,
max_file_bytes, share total bytes/files и tenant bytes/files под locks
usage→share→session→collection. Guest idempotency scope=session ID+route+key;
request hash canonical, replay возвращает тот же upload.

Multipart создаётся после SQL commit. Attach повторно блокирует link+session+upload
и проверяет актуальную policy; проигравший side effect abort-ится. Get и Parts
разрешают только upload, принадлежащий этой exact session/share/policy. Parts POST
проходит Origin+CSRF и подписывает только explicit part numbers. Abort атомарно
освобождает tenant/share reservations; SQL79 trigger снимает reserved file, SQL80
enqueue сохраняет multipart cleanup, а немедленный AbortMultipart остаётся
ускорением. JSON не содержит project/collection/object/multipart/media internals.

SQL83 подключает guest Complete. API требует exact ordered parts, expected size,
CSRF, current share policy и session-scoped Idempotency-Key; SQL повторяет все
проверки под link/session/upload locks, сохраняет stable artifact/snapshot/run IDs
и возвращает202. Общий scheduler completion теперь принимает user|guest и больше
не отбрасывает уже авторизованный completing upload после transfer expires_at.

Guest run.created_by — создатель share (существующий user FK), тогда как snapshot
и binding сохраняют principal_type=guest/principal_id=session. При expected SHA
mismatch фактический staging object и failed ingest сохраняются детерминированно,
tenant/share counters переходят reserved→storage/used и retention cleanup остаётся
обязательным. SQL81–83 live/RLS и live MinIO проверка недоступны.

SQL84 подключает GET `/api/v1/portal/items`. Function требует effective view и
published collection, перечисляет только membership visible+accepted с active
publication и accepted verified version/asset. Ответ — allowlist GuestItem:
item ID, canvas dimensions, безопасный title, section, thumbnail status и
allowlisted derivative variants. Asset/version/artifact/profile hashes, EXIF,
GPS, ratings, flags, tags, reviews и object metadata не возвращаются.

Пагинация keyset `(position,id)` имеет HMAC cursor, привязанный к tenant, guest
session, section filter, sort и collection revision. Изменение collection между
страницами даёт SNAPSHOT_STALE; неверный MAC/shape — INVALID_CURSOR. SQL повторно
проверяет текущую session/share policy, section membership и publication на каждом
запросе. Thumbnail может быть pending/missing; master URL не подставляется.

SQL85 и POST `/api/v1/portal/media-links` принимают 1..100 уникальных item IDs и
1..4 уникальных variants из thumb320/thumb640/preview1600/preview2560. POST требует
Origin+CSRF. SQL формирует полный Cartesian request в исходном порядке и для
каждой пары повторно доказывает current guest:view, published collection/item,
accepted version и ready unexpired derivative в настроенном bucket. Чужой либо
неопубликованный item даёт общий NOT_FOUND; отсутствующая производная —
MEDIA_NOT_READY. Частичного успешного ответа нет.

Только после полной проверки всех пар objectstore выдаёт GET URL на public origin,
закреплённый exact `versionId`, TTL5min и inline disposition. URL не подписывает
latest object, не содержит master fallback и не выдаётся для null/unversioned
artifact. Ответ содержит только item_id, variant, URL/expiry, dimensions и MIME.

SQL86 подключает POST `/api/v1/portal/selection-snapshots`. Запрос передаёт
`collection_revision` как decimal string и ровно один scope: `selected` с 1..5000
уникальных item IDs, `all` без selector либо `filter` с `section_id`. Endpoint
требует Origin+CSRF и session-scoped `Idempotency-Key`; canonical body hash делает
replay стабильным, другой body под тем же key даёт IDEMPOTENCY_CONFLICT.

Function повторно авторизует guest:view, блокирует link/session и берёт shared lock
опубликованной collection с точной revision. В snapshot входят только текущие
visible+accepted membership с active publication, accepted version, verified asset
и ready master artifact. `selected` работает all-or-nothing; пустой набор даёт
EMPTY_SELECTION. Snapshot сохраняет ordered items, exact version/artifact/SHA и
digest всего набора, привязан к exact guest session и живёт не более 24h/session/
share. Ответ `{id,digest,item_count,expires_at}` не содержит storage identity или
URL. SQL86 live/RLS и следующий download job lifecycle пока не проверены.

SQL87 подключает POST `/api/v1/portal/download-jobs` с
`{selection_snapshot_id,preset_id}` и возвращает202
`{download_job_id,status:"queued"}`. Транзакция требует current guest:download,
exact session-owned неистёкший selection snapshot и активный preset, явно
перечисленный в `share.allowed_presets`. Она повторно проверяет project
download_allowed, опубликованную collection и каждый snapshot item против текущих
publication/moderation/version/master readiness; revoke/unpublish не позволяет
создать новое задание из старого снимка.

Один commit создаёт queued download run с полным resolved preset/config hash и
download_job, закреплённый за share, session и policy_version. Идемпотентность
session+route+key сохраняется 30 дней и возвращает тот же job. Guest не передаёт
tenant, collection, share, policy или произвольный preset config.

SQL90–94 и scheduler теперь закрывают download lifecycle до ready volumes и
одноразовой выдачи. POST `/api/v1/portal/download-jobs/{id}/download-intent`
создаёт 256-битный bearer с TTL не более 60 секунд для конкретного ready volume;
обязательный Idempotency-Key хранит только intent UUID. Bearer детерминированно
выводится из UUID и server pepper, поэтому exact replay возвращает тот же секрет,
не сохраняя plaintext; в intent row остаётся только отдельный peppered MAC.
POST `/api/v1/downloads/redeem` под row lock
повторно проверяет session/share policy_version, download permission, job/artifact
TTL и атомарно отмечает intent consumed до выдачи version-pinned S3 URL с
`Content-Disposition: attachment`. Ответ содержит filename, bytes и SHA; bearer
не попадает в SQL, логи или status DTO. Подписанный URL действует до пяти минут и
технически повторяем в пределах своего TTL. SQL87–94 live/concurrency проверка
недоступна.

SQL88 подключает GET `/api/v1/portal/download-jobs/{id}` и POST
`.../{id}/cancel`. Обе функции принимают только session-owned job с exact
share/policy_version и на каждом вызове требуют current guest:download. Projection
содержит status, агрегаты файлов/байт, ordered volumes с delivery_kind, expiry и
sanitized error; run/task/artifact/object identity не сериализуется.

Cancel требует Origin+CSRF, блокирует job и связанный download run, проверяет их
snapshot invariant и идемпотентно переводит активные состояния в cancelling.
Shared task cancellation drain завершает run; SECURITY DEFINER trigger синхронно
переводит job в cancelled/failed, поэтому публичный status не зависает. Terminal
ready/expired/cancelled/failed повторный cancel не изменяет. SQL88 live и реальная
конкуренция cancel против worker commit пока не проверены.

Admin dead-letter metadata stores bounded envelope in restricted diagnostic artifact indexed by outbox/task; no raw messages/secret display. Replay verifies current schema, task status, no active lease, actor sysadmin; creates new dispatch event for same task, never republishes arbitrary payload.

SQL159 и `dead-letter-worker` вводят явный `system_admins`, digest-only intake
всех `q.dead.*`, allowlisted metadata projection, signed pagination и
idempotent replay. API не принимает broker payload: replay снимает terminal
состояние только с exact failed task без canonical result и активной lease, а
новую command/event/fence создаёт штатный scheduler admission. Raw body, token,
cookie и authorization не сохраняются и не возвращаются.


## Явные действия редактора и поиска

`SaveEditsRequest={source_revision,scope,selected_item_ids,overrides_id,style_version_id?,output_name?}`. `Run.SaveEdits` использует ту же атомарную функцию создания snapshot/run/output collection, что Apply. Если profile не выбран, строит полный identity-based StyleV2 в run.config_snapshot, накладывает user-set overrides, quality.status=needs_review, metrics=null, transfer_confidence=unvalidated; это пользовательский рецепт, не обученный/validated профиль. style_version_id nullable, config_hash обязателен. `example` не используется. Source hashes защищаются так же как Apply.

`GET /runs/{id}/outputs` выдаёт только canonical outputs успешных task attempts, перечисленные в `task_results.outputs` и совпадающие по attempt/fence. Доступ повторно проверяется через project ACL, bucket обязан совпадать с configured object store, наружу возвращается короткая 10-minute signed URL без bucket/key/version. Это путь доставки editor preview, prompt-mask manifest и generation candidates; master artifact через него не выдаётся.

`MaskCreate={asset_version_id,input_sha256,object_id?,selector,strokes?,polygons?,points?,box?,protected_refs}` → mask.rasterize либо mask.prompt task, `MaskAccepted={run_id,source_sha256}`; result MaskRef берётся run output. Brush и SAM не произвольные model paths. `GenerateRequest={asset_version_id,draft_revision,mask_ref,protected_refs,params:GenerationParams,variant_count:1..4}` → отдельный candidate run kind preview c explicit_generation=true, по одному task на вариант; ничего не сохраняется в collection до accepted SaveEdits. Сохранение generation override ссылается на accepted candidate/hash, не просит скрыто заново сгенерировать другое изображение.

`SimilarRequest={asset_version_id,project_id?,limit:1..100,min_cosine:0..1}` → tenant+ACL restricted embedding search, exact CPU cosine над approved DINO refs; paginated candidate metadata. No face identity search. Если feature нет, response409 FEATURES_NOT_READY и Editor может запросить analysis; Viewer не запускает вычисления.

`FeedbackRequest={training_set_id,expected_revision,source_asset_version_id,result_asset_version_id,session_id,confirmed:true}` проверяет lineage/доступ и explicit consent inclusion, создаёт обычную пару. Generative result помечен user_added_generated, не попадёт train молча. `ExportStatus` — sanitized export row+artifact readiness+error, выдача через intent; контактные листы — RunCreate kind export с output_kind contact_sheet и source snapshot.

Реализация feedback использует idempotency lock до блокировки training set, поэтому
повтор после увеличения revision возвращает исходную пару. Result обязан быть
membership указанной collection и иметь source как parent либо явный
`asset_version_sources` lineage. Пара всегда `confirmed=true,included=true`;
происхождение хранится как `feedback_edit` либо `user_added_generated` по frozen
run config. `Export.Get` возвращает только status, preset hash, готовность,
размер/MIME/геометрию и bounded terminal code; bucket/key/version остаются скрыты.


## Политика revision для небольших сущностей

PATCH /me использует If-Match users.revision; MemberPatch включает expected_revision membership, asset deletion expected_revision asset. Project ACL PUT проверяет project.revision; удаление session не требует revision. Token/session/auth CSRF операции не подпадают под entity revision правило. Resume/retry RunStatus возвращает runs.revision. SharePatch сверяет policy_version. POST review/save-edits/publication/bulk содержит отдельную ожидаемую revision, даже если метод POST.

`RefreshRequest={refresh_token?:string,client_type:web|native}` (web token cookie, native body), `CSRFToken={csrf_token,expires_at}`, `RevisionRequest={expected_revision}`, `ReplayRequest={reason<=500,expected_task_state}`, `RunQuery={status?,kind?,collection_id?,cursor?,limit?}`, `ReviewQuery={run_id?,state?,kind?,cursor?,limit?}`, `AssetQuery={project_id?,upload_state?,cursor?,limit?,search?}`, `CollectionQuery={project_id?,state?,cursor?,limit?,search?}`, `ItemQuery={section_id?,rating?,flag?,tag?,cursor?,limit?}`. `MediaLinks` batch expired errors возвращает per-item safe code; чужой ID не появляется в successful result.


### Реализованное ядро решения по дефектам

`backend/internal/review.PlanDefects(snapshot,actor,request)` — чистый валидатор/планировщик. Публичный `POST /api/v1/tenants/{t}/reviews/{id}/decision` реализован через `review.Service.Decide`: проверяет tenant/project editor ACL, загружает exact-version `WorkingImage` и `DefectCandidates` вне SQL-транзакции, сверяет размер и SHA256 исходных байтов, строго разбирает оба документа, затем повторно проверяет неизменность и срок жизни контекста в tenant-транзакции. `PersistDefectsAuthorized` блокирует run/review, проверяет canonical producer provenance, фиксирует решение, audit и graph-expansion gate в одном savepoint; внешняя tenant-транзакция делает единый commit. Snapshot поступает только из заблокированной строки reviews и проверенного immutable DefectCandidates; candidate_hash запроса равен SHA256 proposal artifact. Клиентский список никогда не используется как каталог разрешённых candidates.

`GET /api/v1/tenants/{t}/reviews` реализует `run_id/state/kind`, limit и подписанный cursor, привязанный к tenant, principal и hash фильтров. Запрос требует tenant editor и дополнительно скрывает restricted projects без project-level editor grant; owner/admin видят все доступные проекты. Сортировка стабильна по `(created_at,id)`, ответ включает revision, candidate hash, reasons и уже принятое решение/route.

Принимаются только pending review, точная expected_revision, SHA256 в lowercase hex, уникальные известные candidate IDs, непересекающиеся accepted/rejected. Решение reject/skip не принимает частичные списки. accept с пустым списком не означает «принять всё». Пропущенные кандидаты не добавляются ни в accepted, ни в rejected. Уже decided review не переписывается; idempotent replay должен возвращаться слоем idempotency до вызова planner.

DefectPlan содержит revision+1, worker-compatible DefectSelection(version=defect_review_v1, proposal/image/label refs, revision, sorted accepted/rejected), route repair/base/skip и SHA256 JCS decision envelope. Hash включает tenant/review/run/actor/decision и полную selection, включая exact artifact IDs и SHA. Route repair только для accept с непустым accepted; reject/пустой accept выбирает base; skip исключает изображение. Сортировка не мутирует request. Списки worker JSON всегда arrays, включая пустые.

Сам `PlanDefects` не меняет БД и не открывает scheduler gate; эти эффекты выполняет `Service.Decide` через `PersistDefectsAuthorized` и production `ReviewExpansionGate`. Unit tests проверяют stale revision/hash, unknown/duplicate/conflicting IDs, отсутствие неявного approval, immutable inputs и зависимость decision hash от контекста. Без live PostgreSQL проверки это не является доказательством реальных RLS/lock/concurrency semantics.


### Ограничения хранения review (миграция40)

Добавлены reviews.decision_hash (SHA256), decision_route (repair/base/skip, nullable для других видов), формат candidate_hash и пустые decision-поля pending review. Контекст tenant/id/run/task/candidate/base/hash/kind/reasons/created_at неизменяем. Новая запись только pending revision1. Переход pending→decided требует revision+1 и непустой decision_hash; existing CHECK требует decision/actor/time. Decided не допускает никаких изменений, pending — произвольных правок. No-op UPDATE разрешён; revision ограничена safe integer при переходе. DELETE отозван у photo_api; retention выполняется отдельным доверенным путём. Миграция не синтезирует approval/hash для старых записей; несовместимые старые строки обнаруживаются при добавлении CHECK, требуют явного анализа данных.

Схема не проверяет правильность decision hash, membership candidate IDs или право actor: это обязанности service/PlanDefects и транзакционного ACL. decision_route само по себе не разрешает запуск задач. Service сохраняет selection/audit и создаёт durable expansion; scheduler затем materializes выбранную ветвь. `TestIntegrationDecisionIntegrity` покрывает pending mutation, одно решение и запрет последующего изменения; без PHOTO_TEST_DATABASE_URL он пропускается. В текущем окружении SQL40 не проверена на живой БД. Unit review tests и contracts-check проходят; их результат не заменяет SQL integration.


### Проверка proposal перед сохранением review

`review.ReadDefectProposal(ctx,reader,ArtifactRef,DefectSource)` читает максимум1MiB+1, сверяет SHA256 исходных bytes (до canonicalization), разбирает JSON через RFC8785 и strict typed decode. Повторяющиеся/лишние/пропущенные поля, null primitive, не-pending candidate, неверные размеры/числа/refs/labels/IDs отклоняются. Geometry и pixels/image refs должны точно совпадать с доверенным WorkingImage, его TransformChain — canonical equality. Helper не повторяет весь алгоритмический валидатор TransformChain: DefectSource обязан происходить из уже проверенного WorkingImage. Cancellation проверяется до/после IO и между candidates; сетевой reader обязан поддерживать context во время Read.

Для каждого candidate ID повторно вычисляется SHA256 JCS {version,source_sha256,mask_sha256,label,bbox,diameter}, как в Python. VerifiedProposal хранит каталог в закрытых полях; Bind(snapshot) проверяет exact proposal/image/label refs и выдаёт новую копию candidate IDs для PlanDefects. Это не attestation авторства артефакта: repository отдельно проверяет tenant, ready state и принадлежность canonical successful result операции defects.propose. Также reader не скачивает физическую label map; её semantic validation остаётся worker-side перед repair.

Общая fixture `backend/internal/review/testdata/defect-proposal-python.json` сформирована текущими Python contracts с дробным diameter. Go tests проверяют ID parity, геометрию, SHA исходных bytes, дубликаты JSON, лимит чтения и отмену; Python test сохраняет совместимость fixture с DefectCandidates. Reader и planner подключены к публичному endpoint и транзакции сохранения через `review.Service.Decide`.


### Транзакционный primitive сохранения defects review

`PersistDefectsAuthorized(ctx,outer,tenantID,reviewID,actor,requestID,VerifiedProposal,Decision,DefectGate)` работает внутри уже авторизованной tenant transaction; editor ACL проекта и idempotency выполняются вызывающим service до входа. Функция сама не является публичным API и не заменяет эти проверки. Создаёт nested pgx transaction/savepoint. Порядок row locks: run через review→review. Run должен быть running/waiting_review и не cancelled. Kind=defects, task_id обязателен. Snapshot revision/state/proposal берутся из заблокированной строки; candidate catalog только из VerifiedProposal.Bind.

До записи проверяется DB provenance: task того же tenant/run, operation defects.propose v1, succeeded, canonical task_result содержит exact proposal и label ArtifactRefs; оба artifacts ready, с совпадающими SHA и created_by_task_id. Готовый, но не вошедший в canonical result orphan не даёт права review. Далее PlanDefects, guarded UPDATE review (selection/hash/route/actor/time/revision), audit_log(review.decide), обязательный DefectGate.ApplyDefectDecision на том же savepoint. Gate обязан делать только DB/outbox изменения без внешних вызовов/commit. Ошибка любого шага откатывает savepoint. Успех выпускает savepoint, но durable commit принадлежит outer transaction.

Production `ReviewExpansionGate`, tenant/project ACL service и HTTP endpoint подключены. Контракт endpoint не использует Idempotency-Key: повтор после успешного commit получает `REVISION_CONFLICT`, а неизвестный результат запроса клиент разрешает повторным GET review. Service repository загружает exact-version source WorkingImage и proposal, проверяет object metadata, bytes/SHA и повторно сверяет DB context перед записью. Тесты с transaction double проверяют границы callback/savepoint, запрет early gate, audit/gate failure rollback, неаттестованный result и cancelled run. Они не исполняют SQL и не доказывают реальные locks/RLS/concurrency; PostgreSQL integration всё ещё требуется.

### Решение defects review требует полного контекста

`PersistDefectsAuthorized` теперь перед изменением review требует сохранённый
base_artifact_id, совпадающий с Image проверенного proposal, и непустые resolved /
proposal report context refs. Старый неполный review отклоняется как
STALE_DEFECT_REVIEW до записи decision/audit/gate. В той же транзакции проверяются
ready image + exact SHA в declared inputs proposal task, роли candidate/labels/
report и их присутствие в canonical result, а также canonical style.resolve v1
того же run для сохранённого resolved. Это дополняет проверку JSON bytes, а не
заменяет её. Права editor проверяет внешний service в обеих tenant-транзакциях;
endpoint по контракту не требует idempotency key и использует revision conflict
semantics для повторного решения.

Тесты savepoint проверяют отказ при другом base или отсутствующем context и
отсутствие частичных записей. Сам SQL attestation ещё требует live PostgreSQL
проверки; controlled transaction test не доказывает RLS/SQL semantics.

### Реализованный asynchronous Style.Import (migrations 00160)

`POST /styles/import` фиксирует ready/version-pinned package artifact и optional manifest, создаёт новый style, immutable `style_import` snapshot/run и fenced build. HTTP не распаковывает недоверенный архив. `StyleImportCoordinator` публикует JCS params через quota-accounted backend document и материализует единственную `style.import` CPU task. Worker принимает raw StyleV2 JSON или ZIP, запрещает absolute/traversal/normalized aliases, duplicate/encrypted/link entries и всё вне `style.json`, `manifest.json`, `assets/<file>`; ограничивает число и суммарный размер, проверяет каждый SHA/media type, запрещает URL model refs и требует local lock SHA либо portable asset id. Он перепривязывает `style_id` к созданному style, публикует canonical candidate/report и отдельные immutable assets. Обычный candidate materializer сверяет report→config hash. Explicit AcceptCandidate сохраняет source file SHA и только provenance-bound `style_asset_bindings`; auto-accept отсутствует.

### Реализованный Asset.SearchSimilar (migration 00161)

Scheduler материализует только canonical successful `features.scene` output, проверяя object version/bytes/SHA, exact source WorkingImage ref, DINO model/preprocessing/lock, 384 finite values и L2 norm. Проекция `asset_embeddings` tenant-RLS/fenced. `POST /assets/search-similar` требует доступ к source project, не запускает вычисления для viewer, возвращает `FEATURES_NOT_READY` без approved source vector и считает exact CPU dot product только по тому же model lock среди ACL-visible projects, с deterministic score/id ordering.

### Реализованный Export.Create batch (migration 00162)

`POST /exports` принимает immutable user-owned selection snapshot и 1..16 различных preset IDs. В одной idempotent transaction он повторно проверяет snapshot expiry, project editor/download policy и фиксирует точные preset revision/hash/config. Родительский `export_batch` run связывает дочерние download runs/jobs, каждый из которых проходит существующий `export.encode` → metadata/QC → archive/single delivery workflow. Scheduler-функция `finalize_export_batch` завершает родителя только после terminal state всех детей и детерминированно распространяет failed/cancelled/succeeded.

### Реализованные Mask.Create и Generation.CreateCandidate (migration 00163)

Оба endpoint создают tenant/project-authorized immutable source snapshot, queued run и durable `interactive_tasks`. Scheduler читает version-pinned WorkingImage с exact object version/SHA, добавляет nested pixel/alpha refs к declared inputs, публикует fenced JCS params и только затем создаёт dispatchable task. Manual masks используют `mask.rasterize`: векторный ManualMaskV1 находится в неизменяемом params document, а RasterizedMask сохраняет exact params artifact как vector provenance. Prompt masks используют только qualified `mobilesam` runtime lock. Generation проверяет current editor draft revision, принимает 1..4 deterministic seeds, запрещает клиенту подменять image/mask/model/lock/confirmed и создаёт отдельную `image.generate` task на вариант; результат всегда review-required и не добавляется в collection. Финализатор переводит run в completed/waiting_review/failed только после terminal canonical results всех tasks.

Mask array contract унифицирован: `mask.rasterize` и `mask.prompt` публикуют float32 NPY probabilities [0,1]; `image.inpaint` и `image.generate` теперь читают именно float32 и используют threshold .5. Ранее uint16 reader не соответствовал producer output и делал эти пути неисполнимыми; PNG16 остаётся только display derivative.

### Проверка BULK01 на живой PostgreSQL

`tests/integration/test_bulk_revision_fence.py` создаёт selection snapshot из 501 элемента и выполняет production-функции `enqueue_bulk_chunk` / `process_bulk_chunk`. Первый вызов изменяет ровно 500 элементов, сохраняет их результаты и согласованно увеличивает `collections.revision` и `bulk_operations.expected_next_revision`. После внешнего изменения revision второй вызов не откатывает завершённый чанк: единственный оставшийся элемент получает `REVISION_CONFLICT`, а bulk и связанный run завершаются в `completed_with_errors` с полным per-item отчётом. Тест исполняет реальные миграции и ограничения PostgreSQL; доставку команды через RabbitMQ он не подменяет и не доказывает.

`backend/internal/collection/worker_integration_test.go` дополнительно проверяет транспортный путь на изолированном RabbitMQ vhost и временной PostgreSQL: production topology маршрутизирует persistent `collection.mutate`, `RunWorker` потребляет его с prefetch=1 под ролью `photo_worker`, fenced-функция атомарно меняет item и завершает bulk/run, после commit сообщение ACK и очередь пуста. Poison/dead-letter и повтор события проверяются отдельными тестами worker/queue.

### Live trust boundary Style Analyze

`photo_api` не читает `worker_registrations` и не пишет scheduler-owned `style_analysis_builds` напрямую. Миграция00171 предоставляет две узкие `SECURITY DEFINER` функции. `select_style_analysis_runtimes(tenant,geometry)` сначала сверяет `app.tenant_id`, затем выбирает свежие exact qualified fit/aggregate locks и возвращает пустые поля для отсутствующей capability, сохраняя разные публичные ошибки fit и aggregate. `create_style_analysis_build` принимает только queued unsealed run kind analyze с exact style ID в immutable config и pair count1..32; deferred FK выполняются внутри trusted boundary до ответа202. Поздно созданные analysis tables получили явные grants только для `photo_fn_owner`, не для API роли.

Live PostgreSQL test под реальной ролью `photo_api` подтверждает one-pair и multi-pair Analyze, исключение unconfirmed pair из snapshot и отсутствие run при полностью неподтверждённом наборе. `style.aggregate` дополнительно связывает каждый BeautyFit/DefectEvidence с уникальным существующим ColorFit по pair ID и exact before/after ArtifactRef; подмена evidence другой пары отклоняется до staging.

### Contact-sheet export

`POST /api/v1/tenants/{t}/contact-sheets` принимает `ContactSheetCreate={selection_snapshot_id,locale:ru|bg|en,columns:1..8,rows:1..10}` и требует роль Editor. Сервис повторно авторизует project immutable selection snapshot, требует готовый `preview1600` для каждого frozen item и ограничивает результат 128 страницами. Idempotency создаёт один run kind `export` с `output_kind=contact_sheet`; ответ `202 RunAccepted`. Отсутствующий preview даёт `MEDIA_NOT_READY`, отсутствие qualified media worker — `CONTACT_SHEET_CAPABILITY_UNAVAILABLE`. Страницы и manifest доступны через обычный run outputs только после canonical task result.


### GenerationResolution и прекращение uncertain запроса

`POST /api/v1/tenants/{t}/generations/abandon` принимает неизменный `GenerateRequest` и исходный ключ. `GenerationResolution` —200 `{status:"abandoned",key}` либо202 существующий `RunAccepted`. Оба действия сериализуются в scope `Generation.CreateCandidate`; после tombstone под тем же ключом новая генерация запрещена. Полный контракт, права и восстановление клиента определены в [generation-recovery.json](../implementation-v2/contracts/generation-recovery.json). Обычная ошибка запроса не является подтверждением отсутствия admission.
