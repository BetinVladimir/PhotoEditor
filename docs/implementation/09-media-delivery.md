# 09. Загрузка, коллекции, производные и архивы

## Multipart saga

`UploadService.Begin` short SQL tx: ACL/principal/link policy→lock tenant_usage/share→reserve expected bytes+1file→upload initializing with server random object key in staging→commit. S3 CreateMultipart outside tx; success attach multipart_id via CAS→uploading. Crash between calls: reconciliation lists exact server prefix and aborts orphan multipart after grace; never guesses client's key. Failure releases reservation exactly once. API201 содержит upload_id,state,part_bytes,expires_at,не storage secret.

Part size default16MiB, minimum except last5MiB, максимум10000; max file512MiB guest default. `SignParts` verifies owner principal, upload state/expiry, link current version and reservation; URLs PUT part<=600s, batch<=100. Client status uses S3 ListParts через Go and records verified ETags; ETag не SHA. Resume не создаёт другой asset. Complete request locks state uploading→completing, checks ordered parts and total expected bytes via S3; complete S3 outside tx. Повтор complete проверяет object HEAD и completion marker, а не снова резервирует квоту. DB commit staging ref→verifying + ingest run/outbox. Если complete S3 прошло, DB commit упал — reconciler узнаёт exact object by upload key и проверяет size, восстанавливает verify task.

`asset.verify` проверяет фактический SHA,mime,dimensions,decode sandbox. В одной commit/adoption transaction verified source регистрируется asset/version, reservation consumed по фактическому размеру<=reserved; разница release. Bytes staging объекта считаются ровно один раз; `originals` — immutable verified copy с новым key, staging удаляется после copy/hash/adopt; temporary двойное место зарезервировано staging headroom. Verification failure→rejected, release, cleanup. Owner upload acceptance не требует human moderation, guest default pending_review. Accepted source available internal editor после verification, guest publication только после moderation+publish или auto_after_validation и collection published.

Expired/revoked upload: новые parts/complete forbidden; старый PUT может дописать только staging, lifecycle уберёт. Abort выполняет S3 abort best-effort durable maintenance retry, reservation release в SQL, state aborted. Не считать network abort успехом физического удаления до подтверждения.

## Artifact lifecycle

Buckets private: originals,processing,outputs,derivatives,temporary-downloads,model-cache,staging. Originals `tenants/{t}/assets/{a}/versions/{v}/source`; derivatives `.../derivatives/{profile_hash}/{variant}`. Для worker запись только `tenants/{t}/runs/{r}/tasks/{task}/attempts/{attempt}/...`; canonical pointer DB, не S3 rename. Производная canonical key может оставаться attempt key: logical derivative path — назначенный server object destination при upload, уникальный task output suffix; никогда PUT overwrite готового key.

Multipart staging использует только server-generated
`staging/tenants/{tenant}/uploads/{upload}`. Internal SDK endpoint и public
signing origin разделены; production требует HTTPS, development разрешает
только loopback HTTP. Begin сохраняет Content-Type, part URL живёт 1..15 минут.
Complete принимает строго возрастающие part_no и bounded ETag, сверяет точную
версию/размер/Content-Type через HEAD. `VerifyPending` затем читает закреплённую
версию полностью и вычисляет SHA-256; ETag никогда не считается digest.

`StorageGateway.SignRead(principal,artifact,purpose)` разрешает только нужную связь asset version→item/publication/preset. Guest GET<=60s; owner default600s. Signed URL не сохранять в DB как источник истины. Asset access по SHA без ACL запрещён. Originals не public. Ссылки object_version при включённом bucket versioning фиксируются, overwrite deny policy остаётся обязательной.

`GarbageCollector.Mark(cutoff)` выбирает expired unreferenced candidates. `Sweep(id)` повторно проверяет publication/version refs, snapshot leases, active task/run/export, lock GC row; mark deleting, S3 delete exact key/version вне tx, mark deleted. Race: новые ссылки допускаются только artifact state=ready и под тем же row lock; deleting не reattach. Crash after delete→повтор delete success idempotent. Physical missing ready artifact→ARTIFACT_MISSING, invalidate result cache и разрешить rebuild если snapshot сохранён. Retention default processing7d,exports30d,temporary archives24h,originals retained until explicit delete; active leases имеют приоритет retention.

Миграция00100 и `maintenance.ArtifactCleaner` реализуют консервативную часть GC:
выбираются только `ready` artifacts с истёкшим `expires_at`, exact object version,
без active lease и без ссылок из assets/versions/uploads/styles/snapshots/tasks/
reviews/derivatives/exports/downloads. Claim под `FOR UPDATE SKIP LOCKED` переводит
artifact в deleting и выдаёт минутный token. Object-store удаление выполняется
вне SQL-транзакции через `RemoveConfirmed`; finish сверяет token/lease и весь
reference graph повторно, затем ставит deleted. Crash recovery возвращает только
истёкший deleting claim в ready. Связанные expired derivatives/exports требуют
отдельной detach policy, а tenant-wide resumable metadata purge ещё не реализован.

Tenant deletion: mark deleting/revoke sessions links/block new work→cancel runs→wait/fence workers→enumerate refs→delete private objects→SQL cleanup→retain minimal audit по system policy. Не удалять tenant FK раньше dependent metadata. State/deletion cursor resumable.

## Derivatives

`DerivativeService.Ensure(tx,version,profile)->TaskRef`: unique(version,variant,profile_hash), reuse verified canonical только same source; queued otherwise. thumb320/640 JPEGq82 sRGB; preview1600/2560 q88; no upscale, oriented aspect, strip EXIF/GPS. Alpha→WebP alpha либо explicit matte JPEG; API returns actual mime. Watermark policy/profile hash часть key. Thumbnail status независим от master: ready original может иметь pending thumb, guest item metadata со skeleton allowed, no master URL substitute.

Исполняемые adapters `media.thumbnail` и `media.preview` принимают строгий `MediaParams` и variant `thumb320|thumb640|preview1600|preview2560`. `source_kind=working` сохраняет прежний task-scoped WorkingImage path и требует exact manifest+pixel+alpha binding. `source_kind=master` принимает один immutable encoded master ref и обязательный allowlist suffix; decoder отвергает отсутствие ICC, неизвестную глубину/число каналов и более200MP, применяет orientation ровно один раз и не доверяет расширению для определения содержимого. Оба пути требуют CPU backend, не делают upscale, преобразуют через LittleCMS в закреплённый `srgb_v1`, создают JPEG 4:4:4 q82/q88 либо WebP с alpha, сохраняют ICC и проверяют декодирование/размер готового файла перед staging. Старый WorkingImage порядок resize→ICC сохранён для численной совместимости. Миграция00096 разрешает только bounded `derivative` и `derivative_manifest`. Durable DerivativeService/task materialization и привязка canonical derivative к asset version ещё не реализованы; наличие adapter само по себе не делает media URL готовым.

Scheduler-side `BuildMediaRetryDocuments` разворачивает пользовательские группы
`thumbnail|preview` в точные четыре version-pinned variants, выводит task UUID из
event UUID+variant, использует общий с API/worker profile hash и публикует строгий
`MediaParams`. Основной путь читает version-pinned `WorkingImage` master из exact
bucket/key/version, повторно проверяет bytes/SHA, строгую геометрию и объявляет
manifest+pixels+alpha. Encoded master остаётся отдельным allowlist path.
`PrepareMediaRetryTask` связывает params ref, exact inputs, operation/version,
input digest и проверенный RAM/scratch/output/pixel budget. План отклоняет
duplicate groups, неизвестный MIME, unsafe dimensions/hash. Миграция00150 даёт
scheduler bounded SKIP LOCKED claim с60s UUID lease, renew/release и отменяет job
при изменённом item/version или отменённом bulk run. Master обязан быть ready
canonical `working_image` JSON accepted asset version. Следующая граница —
register/materialize/finalize; до неё bulk outcome не является подтверждённой
производной.

`ExportPreset={format:tiff|png|jpeg,icc_id,size:{long_edge?,width?,height?,fit:contain},quality?,subsampling:444,alpha:preserve|matte,matte_rgb?,metadata_policy,watermark?,sharpen?,naming_template}`. Master TIFF16 Deflate work ICC no resize/watermark/sharpen; web JPEGq90 long2400 no upscale; user delivery JPEG default95. Version.master contains accepted creative result; presets не запускают новую творческую обработку. Bulk explicit resize/watermark как «сохранить новые фото» идёт новым apply run/collection, а download encoding preset — derivative.

`derivative.resize`, `derivative.sharpen` и `derivative.watermark` подключены к CPU runtime как отдельные immutable WorkingImage stages. Resize принимает long-edge либо contain box, не растягивает aspect ratio, запрещает неявный upscale, проверяет output pixel limit, ресемплирует RGB в linear-light через area/Lanczos и alpha тем же kernel, затем добавляет один half-pixel ResizeTransform в TransformChain. Sharpen выполняет Gaussian luma unsharp в linear-light с radius`.1..5`, threshold`0..1`, strength`0..1` и жёстким channel delta≤`.03`; strength0 копирует значения точно. Watermark принимает отдельный version-pinned WorkingImage с обязательной alpha, тем же work ICC, bounded max-width, anchor/margins/opacity; compositing выполняется в linear-light и не меняет Canvas/TransformChain исходника. Все три публикуют RGB, optional alpha, WorkingImage и bounded DerivativeReport; миграция00132 задаёт output policies. Metadata/encode остаются отдельными стадиями, поэтому creative pixels не получают скрытых EXIF или format-dependent изменений.

Metadata `internal_master` camera/lens/DateTimeOriginal, GPS only copy_gps; `guest_safe` no GPS/camera/private EXIF, optional author/copyright. `metadata.write` resets orientation1/dimensions/current ICC, removes stale embedded thumbnails. EXIFTool arguments array, no shell. Encoding stage TIFF16/PNG16; JPEG flatten requires matte when alpha present.

Исполнимый adapter `metadata.write` принимает exact EncodedImage со `metadata_policy=strip` и отдельный строгий MetadataDocument. Политика `copyright_only` допускает только Artist/Copyright; `safe_camera` дополнительно допускает Make/Model/LensModel/DateTimeOriginal, а GPS копируется только при отдельном `copy_gps=true`. Adapter копирует encoded artifact в attempt scratch, запускает закреплённый ExifTool через argv с `shell=False`, очищает EXIF/XMP/IPTC и stale Thumbnail/Preview, затем заново пишет allowlist, Orientation=1 и текущие dimensions. После запуска файл повторно декодируется, проверяются размеры/orientation и SHA embedded ICC; только после этого публикуются новый encoded artifact, manifest и MetadataReport. Миграция00133 задаёт роли. CPU runtime объявляет операцию лишь когда executable ExifTool реально найден и executable; в текущем dev-окружении он отсутствует, поэтому production execution остаётся явно заблокированным dependency gate, при этом argv/policy/qualification покрыты unit tests.

`report.contact_sheet` подключён к media runtime. `ContactSheetParams` содержит ordered immutable preview refs с MIME и подписями, locale `ru|bg|en`, exact TTF artifact и совпадающий `font_lock_id`, а также bounded grid. Task требует exact ordered input binding, использует только artifact font с deterministic BASIC layout, сохраняет пропорции thumbnails, обрезает слишком длинную подпись с ellipsis и выпускает PNG pages без PDF-зависимости. Каждый preview materialize закрывается до следующего, page scratch освобождается после upload. Число страниц ограничено128 согласованно с gateway output policy; более крупный snapshot orchestration разбивает на несколько tasks. Manifest фиксирует page refs, first index/count, locale и font lock. Миграция00134 разрешает pages и manifest; тест проверяет pagination4+2 и кириллические labels.

## Snapshots и download

`SnapshotService.Capture` locks collection revision, selects allowed published ready items by principal; preserves order and filenames and approved preset config hash. `scope=all` includes not-yet-loaded pages; empty422; removed IDs before snapshot409. Grant lease на исходные version artifacts на подготовку+TTL с renew пока job active. Изменения после snapshot не добавляют items; unpublish/revoke всё равно запрещают **новую** выдачу соответствующего архива (проверить все snapshot items currently downloadable при intent). Не переиспользовать по другому share_id/policy_version.

`DownloadPlanner.PlanVolumes(items,limits)` ожидает готовые preset exports, потом реальные bytes. Stable greedy ordered packing max5000/max10GiB source bytes; ZIP64 store images/deflate JSON. Если один файл превышает лимит — delivery_kind=single, volume самостоятельная external item entry, UI explicit individual download; manifest перечисляет без потери. Имена: basename sanitize NFKC для имени только, controls/path separators удаляются, empty→photo, casefold collision suffix __sha8 и ordinal если всё ещё коллизия; 180 UTF8 bytes max до extension. Архив не содержит локальные paths/секреты/tenant details.

`ArchiveBuilder.build(snapshot,params)->ArchiveManifest`:

1. Reserve scratch≥sum entry bytes+ZIP metadata estimate+manifest+max stream buffer, затем *1.2; reserve final temporary MinIO bytes отдельно. Если insufficient→DISK_QUOTA, не начать.
2. `/work/tmp/t/job/attempt/archive.part`, zipfile allowZip64=True, each `ZipFile.open(...,force_zip64=True)` для потенциально больших entries; bounded1MiB stream, incremental SHA compare snapshot, manifest added last.
3. Close central directory, verify entry names/count/sizes and testzip CRC streaming; hash whole ZIP; atomic local rename archive.zip.
4. Upload immutable key `temporary-downloads/tenants/t/jobs/job/attempts/attempt/volume-n.zip`; HEAD size + stored SHA, fenced commit artifact/manifest/progress.
5. Job ready только все volumes/oversized singles готовы; expires_at=ready_at+24h, leases до expiry. Cleanup local scratch после commit либо recovery sweep.

Missing/hash mismatch entry fails entire volume/job, не отдавать частичный архив как ready. Retry может reuse уже готовые volumes в том же snapshot, не повторять accepted output. Cancel сохраняет private artifacts до GC, запрещает новые intents.

`DownloadIntent.Create`: current principal/policy/publication, ready artifact and TTL; random256 token hash, expiry<=60s. `Redeem`: lock token unused/current ACL, consume, issue presigned URL with Content-Disposition attachment. Intent одноразовый, полученный S3 URL технически может повторяться до TTL — не называть его одноразовым URL. Browser получает URL и выполняет anchor navigation; autoplay download закрытой вкладки не обещается. Refused user gesture→кнопка «Скачать».

SQL94 и portal HTTP service реализуют этот контракт для готовых download volumes.
Create требует Origin/CSRF и Idempotency-Key; replay восстанавливает тот же bearer
из intent UUID и server pepper, поэтому секрет не хранится. Redeem принимает
только bearer, блокирует intent, заново проверяет current share/session policy и
download TTL, помечает consumed и подписывает exact object version. Object store
ставит `attachment` с серверным безопасным filename; ответ также содержит bytes
и SHA256 для контроля UI. Ошибка signer после consume требует создания нового
intent и не делает старый bearer повторно используемым.

Owner download API реализован в `backend/internal/download` и подключён к четырём
маршрутам `/api/v1/tenants/{t}/download-jobs...`. Start принимает только
user-owned immutable selection snapshot, заново применяет tenant/project viewer
ACL и `download_allowed`, фиксирует exact preset revision/hash/config и атомарно
создаёт run/job через tenant-scoped idempotency. Get/cancel повторно проверяют
текущий restricted-project ACL. Intent доступен только для целиком ready job и
ready exact-version artifact; перед созданием и при redeem проверяется, что все
snapshot items всё ещё относятся к тем же collection items/version. Bearer не
хранится в idempotency response: он выводится из intent UUID и server pepper.
Миграция00099 объединяет redeem guest/user intent, повторно проверяет live guest
publication policy либо active user membership/project ACL, потребляет token до
подписи и возвращает только exact object version. Live PostgreSQL/MinIO fault
injection остаётся непроверенным release gate.

## Коллекции и bulk

Публиковать можно accepted source/processed ready item одной immutable version; скрыть item означает publication state revoked и revision++. Изменение прав или публикации не удаляет уже скачанные bytes. Cover только visible item той же коллекции; при removal cover→null в той же tx. Position bigint шаг1024; при нехватке промежутка rebalance под collection lock, stable id tie. Remove membership не удаляет asset. Move/copy только same tenant, обе project ACL, sorted collection locks; asset bytes не копируются. Bulk chunks сохраняют per-item outcomes и cancellation не откатывает завершённые chunks.

## Реализованный примитив immutable original copy

`objectstore.Store.CopyOriginal(ctx,tenant,artifact,source)` использует generated
key `originals/tenants/<tenant>/<artifact>` и version-pinned Open(source), затем
потоковый PutImmutable с исходными SHA/bytes/content type. Размер 1..512MiB;
source и destination keys различны. PUT проверяет EOF/full SHA и сохранённую
версию, включая multipart. Reader закрывается при любом исходе. Existing key
или неизвестный storage PUT outcome обрабатывается через RecoverImmutable,
который проверяет все bytes destination; integrity/input failures не маскируются.
Повтор не перезаписывает original, source staging не удаляется.

Этот примитив **не** регистрирует artifact/asset/version и не расходует квоту.
Caller обязан предварительно сохранить ownership reservation generated key и
зарезервировать temporary double-storage headroom, затем commit returned version
и только после успешного adoption разрешить staging cleanup. Эти шаги ещё требуют
реализации, поэтому CopyOriginal пока не включён автоматически в ingest loop.
Controlled tests проверяют source version, close, retry/recovery и отказ при
integrity/cancellation. TestCopyOriginalMinIOReplayPreservesStaging добавлен для
живого MinIO, но без PHOTO_TEST_MINIO_* пропускается; live успех не заявляется.

## Резервирование original (SQL61)

`ingest_originals` — function-only RLS таблица immutable source binding по
(tenant,run,ordinal), stable target artifact_id, сменяемому token, bytes/bucket/
media_type и состоянию pending/ready. Reserve требует sealed active ingest
phase decode/catalog, exact source snapshot ID/SHA и canonical succeeded
verify/inspect/decode tasks. Источник ready, versioned, unexpired, 1..512MiB.
Lock order tenant_usage→run→reservation совпадает с backend document accounting.

Новая reservation добавляет source.bytes в reserved_bytes при проверке общего
storage_bytes+reserved_bytes против tenant quota. Это резерв временной второй
копии; уже учтённый staging не вычитается. Pending replay возвращает прежний
artifact ID, новый token и не увеличивает резерв. Ready replay возвращает
сохранённую version после проверки ready artifact. Conflicting binding отвергается.

Complete требует exact artifact/token и непустую version, вновь проверяет active
run/phase. В одной транзакции создаёт ready artifact с generated originals key,
reserved_bytes уменьшается, storage_bytes увеличивается на bytes ровно один раз,
reservation становится ready. Повтор ready completion проверяет ту же version.
Недостаточный reserved counter вызывает exception/rollback всех изменений.
CopyOriginal caller подтверждает SHA bytes до Complete; SQL не читает S3.

OriginalRepository реализует Reserve/Complete, проверяет nullable response shape,
IDs/version и передаёт scope/fence. Controlled adapter tests проходят. SQL61
на живой БД ещё не проверена. Эти функции пока не подключены к scheduler:
необходимы source object loader, original copy coordinator, catalog adoption и
cleanup pending/ready abandoned reservations. До их реализации reservation не
освобождается автоматически; нельзя считать жизненный цикл завершённым.

## OriginalCoordinator

SQL62 ingest_original_source читает source location только по exact pending
reservation scope/artifact/token, active sealed run и decode/catalog phase.
Source ID/SHA/bytes/media_type должны совпасть с reservation; объект ready,
unexpired и versioned. Scheduler получает key/version, публичный API — нет.
OriginalRepository.Source дополнительно проверяет настроенный bucket и digest.

OriginalCoordinator.Prepare выполняет Reserve→Source→CopyOriginal→Complete.
Сначала durable quota/key reservation, затем S3 вне SQL transaction, затем
fenced artifact commit. Ready replay возвращает original ref без повторного IO;
неизвестный результат Complete перечитывается через Reserve при следующем вызове.
Source и copied target проверяются по SHA/bytes/media_type и generated key;
ошибка копии не вызывает Complete. Lost fence возвращает ошибку без готового ref.
Ни одна ветка не удаляет staging. Это готовность original artifact, не готовность
asset/version/collection или всего ingest run.

Controlled tests подтверждают order, uncertain commit resume без повторной копии,
отказ при copy error/mismatch/lost fence и source token/bucket binding. Workflow
и contracts-check прошли. SQL62/live не проверена; coordinator пока не подключён
к scheduler до catalog transaction и lifecycle cleanup незавершённых originals.

## Явная привязка upload к catalog destination (SQL63)

До initial graph seal создатель ingest обязан вызвать bind_ingest_upload для
каждого (run,ordinal,upload_id). SQL не ищет upload по SHA или имени. Проверяются
verifying upload с staging ref, exact snapshot source ID/SHA, ready versioned
artifact, actual_size/object_key, совпадение principal_type/id upload и snapshot,
неархивный project и nondeleted collection того же project. Run active ingest,
ещё без ingest_progress. Binding создаётся под run→upload locks; одна upload
session может иметь только одну binding во всём tenant. Права пользователя на
создание run остаются обязанностью API; функция scheduler-only не заменяет ACL.

Function-only RLS ingest_upload_bindings сохраняет upload/source/project/collection/
principal и stable generated asset_id/version_id/item_id. Эти ID пока не означают
существование asset/version/item. Exact повтор возвращает те же IDs, другой
upload для занятого ordinal отвергается; неизменяемые bindings не обновляются.
При отсутствии collection item_id остаётся зарезервированной идентичностью, а
membership создаваться не должен. Catalog commit обязан заново проверить текущее
состояние destination и moderation policy; исходный binding не разрешает публикацию.

IngestUploadBinder — внутренний Go adapter с проверкой scope, ordinal и уникальных
nonnull IDs. Controlled tests/контракты прошли; SQL63 live/concurrency не проверены.
API создания ingest должен подключить Bind в свою транзакцию до запуска scheduler.
Catalog adoption, collection position/moderation, quota upload settlement и cleanup
всё ещё требуют реализации; созданием binding эти требования не считаются закрытыми.

## Регистрация source asset/version (SQL64)

register_ingest_asset принимает tenant/run/ordinal, canonical working_image ref
и проверенный WorkingImage JSON. Master source version — WorkingImage manifest;
content_hash — его canonical SHA. Original source сохраняется отдельно через
assets.original_artifact_id. IDs asset/version берутся только из SQL63 binding.

Под run lock проверяются active sealed ingest decode/catalog, exact upload binding,
неархивный project (lock), verifying upload (lock), совпадение исходника/проекта/
коллекции/principal и отсутствие verified_version_id. Ready original должен
совпасть с binding и зарегистрированным artifact. Decode task выбирается по
immutable plan ordinal/source; master и вложенные pixels/alpha подтверждаются
через canonical task_results и created_by_task_id/output_role/ready/expiry.
JSON byte/hash/full shape проверяет Go до SQL; SQL дополнительно проверяет source,
profile/dtype и вложенные refs. Raw JSON не принимается от публичного API.

Одной транзакцией создаются asset(upload_state=verified, provenance upload/run/
ordinal/source SHA), version_no=1 и function-only ingest_catalog_items receipt.
User source review_state=accepted, guest/другой principal — pending_review.
Receipt обеспечивает exact master ref replay без повторного INSERT. Run здесь
не завершается, upload остаётся verifying до settlement; collection membership
и publication ещё не создаются. Существование source asset не означает готовую
гостевую выдачу или завершённый импорт. Дальнейшее завершение должно атомарно
обработать membership/moderation, upload quota/state, item outcome и run aggregate.

IngestCatalogRepository.RegisterImage повторно canonical-сериализует WorkingImage
и вызывает ReadDecodedImage с trusted ICC перед SQL; изменённый DTO не пройдёт
manifest SHA. Tests/контракты проходят; SQL64 live/rollback/concurrency пока не
проверены. Transactional registration ещё не подключена к scheduler, пока не
завершены catalog coordinator, settlement и abandoned-original cleanup.

## Полная per-image adoption transaction (SQL65)

SQL65 заменяет внешнее применение SQL64: scheduler лишён EXECUTE
register_ingest_asset, Go метод переименован в AdoptImage и вызывает только
adopt_ingest_image. Helper SQL64 остаётся внутренним вызовом владельца функции.
Asset/version, receipt, membership, moderation/publication и upload settlement
фиксируются одной транзакцией; exception откатывает также вложенные INSERT SQL64.

Порядок locks usage→share (если есть)→run→project→collection→upload→quota.
Binding immutable используется лишь как hint. Перед mutation проверяются active
sealed phase, verifying upload, destination/principal/source, actual bytes,
reserved quota с holder_type=upload, holder_id=upload.id, files=1,
bytes=expected_size>=actual_size и counters tenant/share. Upload creation должен
сохранять именно этот holder contract. Ready original уже учтён SQL61 отдельно.
Upload source до adoption покрыт reserved_bytes, не storage_bytes: adoption
переводит expected reserve в actual staging storage и освобождает разницу.
Так original+физически существующий staging учитываются одновременно. Cleanup
после подтверждённого удаления staging должен вычесть receipt.staging_bytes;
раньше удалять этот учёт нельзя. quota_reservation становится consumed, bytes=actual;
share reserved→used bytes/files изменяются ровно один раз.

Membership создаётся только при collection_id; position=max+1024 под collection
lock, при исчерпании safe integer выполняется stable rebalance по position/id.
Collection revision увеличивается. User accepted, hidden без автоматической
публикации. Guest accepted автоматически только по действующей unrevoked ссылке,
совпадающей policy_version и collection, permissions.upload=true и
moderation=auto_after_validation. В этом случае published collection получает
visible membership + publication от link.created_by; для draft collection
membership остаётся hidden. Иначе guest pending_review/hidden. Publication не
обходит будущие проверки текущего share/ACL при выдаче media.

Upload становится verified с exact version ID и revision++; receipt.settled_at
и staging_bytes обеспечивают replay без повторной квоты/INSERT. Run здесь не
завершается. Future failure settlement и run aggregate ещё не реализованы.
Go adapter tests подтверждают выбор полной SQL функции и pre-SQL manifest
проверку; они не доказывают SQL rollback/locking/moderation. SQL65/live пока не
проверена. Scheduler catalog loop ещё не подключён до cleanup и завершения run.

## Неуспешный файл ingest (SQL66)

RejectFile принимает ordinal/source/stage/reason; разрешены только verify/inspect/
decode и failed/cancelled, никогда retry_wait. SQL66 вновь подтверждает explicit
upload binding, persisted ingest_decode_items и фактическую terminal task той же
стадии/source. Для раннего отказа stage/reason обязаны совпасть с зафиксированным
inspection outcome. Наличие original reservation или catalog receipt исключает
отказ через этот путь. Run cancellation требует отдельного recovery lifecycle.

Lock order usage→share→run→upload→quota. Проверяются verifying upload, exact
source bytes/principal/destination, holder_type=upload/holder_id, expected reserve
и share counters. Одна транзакция переводит tenant reserved expected bytes в
storage actual staging bytes, освобождает share reserved bytes/file без увеличения
used, quota state=released, upload=rejected с revision++ и создаёт function-only
immutable ingest_rejections receipt. Receipt хранит failure stage/reason и
staging_bytes. Повтор проверяет тот же отказ и rejected upload без повторного
изменения квоты. Staging ещё физически существует; cleanup должен освободить
storage bytes только после удаления. Asset/version/membership не создаются.

Go adapter tests проверяют source/reason binding и pre-SQL rejection nonterminal
states. Workflow/contracts-check прошли. SQL66 live/rollback/concurrency пока не
проверены; rejected-source cleanup, run aggregation и catalog runtime wiring
ещё необходимы. Наличие SQL receipt не означает завершение полного ingest.

## Cleanup незавершённых original reservations (SQL69)

SQL69 добавляет cleanup_state/token/lease/after отдельно от pending/ready original.
Claim выбирает только pending без зарегистрированного artifact, после grace3h и
при cancelled/terminal run либо cancel_requested. Locks usage→run→reservation,
повторная проверка state/artifact, 60s cleanup lease. Ready originals исключены:
даже unreferenced ready artifact не удаляется этим путём. Reserve/Complete
переопределены: cleanup_state permanently запрещает resume/adoption original.
Constraint дополнительно запрещает ready original с cleanup marker.

OriginalCleaner проверяет tenant/artifact IDs и bucket ticket, вызывает только
PurgeOriginal с generated key, удаляет multipart и все versions точного key через
существующий bounded purgeReservedKey. S3 IO вне SQL, timeout40s. Только после
успешного purge Finish проверяет token/lease и отсутствие artifact, освобождает
reserved_bytes один раз (cleaning→cleaned). Tombstone повторно проверяется через
24h без второго освобождения квоты, чтобы убрать late writes старого uploader.
Если purge/finish прерван, lease expiry допускает повтор. Ключи зарегистрированных
originals, staging и произвольные prefixes через этот путь не принимаются.

Maintenance подключает OriginalCleaner третьим round-robin handler наряду с
output/document cleaners, readiness включает все три. Counters
photo_original_cleanup_total / photo_original_cleanup_failures_total; startup
требует SQL69. Tests подтверждают purge-before-finish и отказ чужому bucket;
контракты/legacy check проходят. SQL69 live/concurrency пока не проверены.
Готовые, но не adopted originals и физический staging требуют отдельного
reference-aware lifecycle; этот cleaner их не покрывает.

## Защита новых artifact references (SQL70)

До включения reference-aware staging cleanup SQL70 устанавливает общий trigger:
новые/изменённые FK ссылки на artifacts.id, tasks.inputs и task_results.outputs
собираются в sorted unique UUID list; artifact каждого tenant блокируется FOR
SHARE и должен быть ready/unexpired. Отсутствие — FK-like 23503, deleting/expired
— 23514. FOR SHARE конфликтует с UPDATE state, в отличие от FOR KEY SHARE.
Cleanup обязан взять конфликтующую artifact row lock, повторно проверить ссылки
и перевести state в deleting перед object IO. После этого новые ссылки отклоняются.

Trigger генерируется для **всех текущих** FK к artifact.id по pg_constraint,
включая расширения после baseline. Новые миграции с artifact relations обязаны
добавлять guard явно. На обычном UPDATE неизменённые refs не проверяются заново,
чтобы исторические строки не блокировали статусные изменения; artifact_leases
проверяются при каждом UPDATE, включая продление срока. Это не замена ACL/SHA
контрактов и не разрешение игнорировать существующие ссылки при cleanup.
Продление snapshot retention с вложенными snapshot_items также должно проходить
через отдельную reference-aware API транзакцию (пока не реализована).

Integration test для ready/deleting/expired lease добавлен, но без PostgreSQL
пропущен. Повторная проверка dev-infra: Docker unavailable, контейнеры не менялись.
Workflow/contract checks проходят; SQL70 migration/locking concurrency live не
подтверждены. Сам staging cleanup ещё не включён: необходимы классификация
исторических refs, retention/leases и cleanup tickets с exact-version deletion.

## Подтверждение exact-version удаления

Store.RemoveConfirmed(ctx,Object) дополняет базовый Remove: после DELETE с
VersionID выполняет HEAD **того же key/version**. Только NoSuchKey/NoSuchVersion
подтверждает удаление. Успешный DELETE при всё ещё доступном объекте, unavailable/
forbidden HEAD и отсутствующий bucket не разрешают освобождение квоты. Если ответ
DELETE неизвестен/ошибочен, но HEAD доказывает отсутствие версии, повтор cleanup
может завершиться успешно. Latest version и другие versions не затрагиваются.
Unversioned/null version refs отвергаются до запросов. Отмена context сохраняется.

Вызывающий cleaner должен владеть DB cleanup ticket, ранее заблокировать новые
references и перевести artifact ready→deleting. RemoveConfirmed сам не выполняет
ACL/reference checks и не меняет usage. Это пригодный примитив для staging
cleanup, но discovery/ref checks/retention и ticket lifecycle ещё не подключены.
HTTP protocol tests подтверждают pinned DELETE/HEAD, отказ при still-present/
HEAD denied и успешное recovery absent version; это не live MinIO проверка.
Objectstore tests и contracts-check проходят; legacy наработки не изменены.

## Классификация staging references (SQL71)

staging_has_references — внутренний предикат, не разрешение на удаление. Caller
обязан доказать settled owner upload/run и держать artifact row lock. Предикат
консервативно проверяет все текущие FK к artifacts.id через pg_constraint,
а также JSON tasks.inputs/task_results.outputs. Постоянные ссылки asset/version/
style/review/export/derivative/download всегда блокируют cleanup. Active artifact
leases блокируют; expired leases игнорируются (SQL70 проверяет их продление).

Только исторические ссылки owner ingest исключаются: его upload.staging ref,
его snapshot_items, его source refs в node plans/original reservation/upload binding
и terminal task inputs. Любая ссылка другого upload/run/snapshot удерживает объект,
даже terminal foreign task или истёкший foreign snapshot: их retention cleanup
должен сначала удалить эти refs. Любой task result output тоже удерживает.
Если owner snapshot использован ещё одним run, источник остаётся защищённым.

Новый trigger runs при INSERT/смене snapshot/tenant и reactivation terminal run
берёт FOR SHARE на артефакты snapshot/items, проверяя ready/unexpired. Это закрывает
обход SQL70 через повторное использование уже существующего snapshot: такой run
не может заново присоединить deleting source. Нормальные статусные UPDATE без
смены snapshot не пересматривают исторические refs. Snapshot renewal API пока
отсутствует; он также обязан проверять доступность source и retention leases.

Integration test дополнен попыткой создать run на старом snapshot с deleting
source; без живого PostgreSQL тест пропущен. Workflow/contracts-check проходят,
SQL71 live/concurrent proof отсутствует. Следующий cleanup claim обязан выполнить
предикат после artifact lock в READ COMMITTED и сохранить exact-version ticket;
только после этого возможен RemoveConfirmed и однократное освобождение usage.

## Очередь staging cleanup (SQL72)

Settlement success/rejection теперь транзакционно создаёт staging_cleanup job;
backfill включает существующие receipts. Retention=7d от settled_at. Artifact ID
в queue — историческая identity, не live-use FK: queue не должна сама удерживать
удаляемый объект. Claim доступен maintenance, только READ COMMITTED. Locks
usage→owner run→job→artifact; run должен иметь phase complete и terminal
completed/completed_with_errors/failed, upload verified/rejected, bytes/ID bindings
совпадают. Cancelled partial runs пока требуют отдельного recovery lifecycle.

После artifact FOR UPDATE вызывается SQL71 reference predicate. Blocked job
откладывается на1h до возврата, освобождая очередь для других. Успешный claim
атомарно ready→deleting и сохраняет exact bucket/key/version/SHA/bytes/media type
с cleanup token/lease60s. Retry cleaning job сверяет неизменность object identity.
S3 запросов внутри tx нет. Новые references блокируются SQL70/71.

StagingCleaner передаёт exact Object в RemoveConfirmed (40s), затем Finish(5s).
Finish проверяет token/lease и deleting artifact identity, уменьшает storage_bytes
ровно на receipt.staging_bytes, ставит artifact deleted и job cleaned в одной tx.
Финальный token сохраняется для replay без второго списания. При unknown delete/
HEAD result Finish не вызывается; истёкший ticket можно получить снова. Artifact
строка и historical snapshot/task references сохраняются как tombstones, физические
bytes удалены; новая обработка должна использовать adopted original/version.

Maintenance подключает staging четвёртым round-robin handler, readiness/metrics
photo_staging_cleanup_total и photo_staging_cleanup_failures_total. Startup требует
SQL72. Tests maintenance/objectstore и contracts проходят; SQL72/live reference
races, retention и quota rollback ещё не проверены. Готовые orphan originals,
частично отменённые uploads и общая retention других artifacts остаются вне этого
специализированного cleaner и требуют дальнейшей реализации.

## Истечение upload и durable multipart abort (SQL80)

Сессии `initializing|uploading` после `expires_at` завершаются только функцией
maintenance в READ COMMITTED. Порядок locks: tenant_usage → upload_session →
quota_reservation. В одной транзакции reserved bytes уменьшаются, reservation
становится released, а SQL79 trigger уменьшает reserved files. Затем session
становится expired; повторный вызов не меняет counters.

Переход owner upload в aborted или expired с непустым multipart_id создаёт
`upload_abort_cleanup` в той же транзакции. Ticket содержит серверные object key
и multipart ID, имеет queued/cleaning/cleaned, token и lease60s. Claim использует
SKIP LOCKED. Внешний AbortMultipart выполняется после commit; NoSuchUpload
подтверждает отсутствие multipart, остальные ошибки оставляют ticket для retry.
Finish сверяет token, lease, terminal upload state и неизменные identifiers.

UploadCleaner выполняет за цикл не более одного expiry и одного abort, поэтому
большая очередь expiry не блокирует уже созданные cleanup tickets. Это пятый
round-robin maintenance handler с отдельной readiness и Prometheus counters.
Функции SQL80 обязательны при startup maintenance. Unit tests проверяют success,
NoSuchUpload и запрет finish после неопределённой ошибки; live PostgreSQL/S3
проверка пока отсутствует.

## Portal session storage (SQL81)

Публичный share secret проверяется только внутри `exchange_share_session` по
32-byte HMAC. Созданная share_session связывает tenant/share/collection и exact
link policy version, содержит только HMAC session/CSRF токенов и TTL≤24h. Роль
photo_portal не читает таблицы напрямую. `authorize_portal_session` на каждом
запросе повторно проверяет link/session expiry+revocation, policy version,
collection lifecycle и требуемое permission.

`get_portal_collection` является отдельной allowlist-проекцией: upload-only не
может обнаружить sections, cover или факт наличия item; view/download effective
только у published collection. Это session/read foundation; последующие
Begin/Get/Parts/Complete/Abort используют те же function-only rechecks.

## Guest multipart lifecycle (SQL82)

`begin_portal_upload` является единственной точкой создания guest reservation.
Она валидирует текущую session/CSRF/share policy, разрешённый формат и лимиты
share+tenant под общими aggregate locks. Project/collection берутся из share,
principal — session ID, policy_version фиксируется в upload. Идемпотентный replay
не резервирует повторно. S3 Begin выполняется после commit; attach ещё раз
проверяет актуальную policy под locks.

Get/Parts доступны только exact owner session. Abort освобождает share reserved
bytes/files и tenant reservation ровно один раз; переход aborted транзакционно
создаёт SQL80 cleanup ticket. SQL82 заменяет generic expiry так, чтобы guest
expiration также уменьшал share reserved counters в порядке usage→share→upload→
reservation. SQL83 завершает Complete: parts и stable IDs фиксируются
транзакционно, затем тот же lease/heartbeat scheduler выполняет S3 Complete/
recovery и exact-version SHA. Forward migration заменяет claim/finish/fail,
разрешая principal user|guest и не считая transfer deadline сроком scheduler
processing после принятого Complete. Guest run получает created_by=share.created_by
для FK/audit, но snapshot/binding сохраняют guest session principal. Hash mismatch
переводит share reservation в used bytes/files и создаёт обычный rejected staging
cleanup receipt.

## Portal item projection (SQL84)

Portal catalog строится только по intersection collection_items visible+accepted,
publications published, accepted asset version и verified nondeleted asset.
`list_portal_items` требует current guest:view и collection state published.
Projection не содержит asset/version/artifact IDs или metadata JSON. Dimensions
берутся только из validated WorkingImage canvas; некорректная запись закрывает
страницу ошибкой сервиса. Derivative summary ограничен thumb320/thumb640/
preview1600/preview2560 и не раскрывает profile hash либо storage reference.

Keyset cursor подписан HMAC и включает session/filter/revision; pagination не
является разрешением. Thumbnail pending/missing отображается skeleton, без выдачи
master как fallback. SQL84 live и реальные derivative rows пока не проверены.

## Portal media links (SQL85)

`resolve_portal_media` разворачивает item_ids×variants и сохраняет ordinality.
Каждая пара проходит текущую publication/visibility/moderation/version/asset
проверку; derivative ограничен четырьмя guest-safe variants, status=ready,
ready+unexpired artifact, exact configured bucket и обязательной object version.
Любой not_found/not_ready отменяет весь batch до подписания.

Store.SignRead является transport primitive: проверяет immutable Object, TTL
1..15min и подписывает GET public endpoint с exact versionId. Portal использует
TTL5min и response-content-disposition=inline. Unit test подтверждает public host,
version pin и отказ для null version. SQL85/live S3 остаются непроверенными.

## Portal selection snapshot (SQL86)

Guest download строится из серверного snapshot, а не из клиентского списка во
время упаковки. `create_portal_selection` под точной collection revision
материализует разрешённый ordered набор в `snapshots/snapshot_items`. Scope `all`
охватывает страницы, которые браузер ещё не загрузил; `filter` допускает только
существующий section; `selected` требует совпадения каждого переданного ID.

Каждая строка фиксирует item, accepted asset version, ready master artifact и
content SHA. Digest — SHA-256 ordered последовательности
`item_id:asset_version_id:content_hash`. Collection остаётся под shared lock до
записи items, поэтому source_revision и digest принадлежат одному состоянию.

Snapshot связан с exact guest session, истекает не позднее 24 часов/session/share
и не переиспользуется другой сессией. Последующие unpublish/revoke должны запретить
новый download intent, даже если snapshot существует. SQL86 ещё не создаёт leases
или download job: их атомарное создание и продление относятся к download lifecycle.
Live PostgreSQL не проверен.

## Color-managed export.encode

Объявленная registry operation `export.encode` теперь существует в worker
runtime и регистрируется только у CPU worker с валидным PHOTO_ICC_ROOT. Вход —
exact WorkingImage manifest плюс объявленные pixel/alpha artifacts; динамическое
чтение неуказанных refs запрещено. Размер вычисляется aspect-preserving contain,
upscale только explicit. Цвет переводится LittleCMS из locked work ICC в locked
target ICC через RGB16.

JPEG кодируется с explicit quality, 4:4:4, matte и embedded ICC. PNG/TIFF имеют
16-bit channels, preserve alpha либо explicit matte; TIFF использует Deflate,
predictor и ICC tag. Encode начинает с чистых pixel arrays и потому гарантирует
только metadata_policy=strip; разрешённые camera/copyright поля должен записать
следующий metadata.write из отдельного проверенного документа. Реальный JPEG test
проверяет размер, декодирование, embedded ICC SHA и отсутствие subsampling.
PNG/TIFF visual/third-party decoder matrix и аппаратные профили пока не проверены.

## Download export read boundary (SQL89)

Scheduler не доверяет `asset_versions.metadata` или вложенным artifact refs из
master JSON без producer attestation. SQL89 связывает selection item с exact
version/master и отдельно доказывает, что working_rgb/alpha созданы тем же
successful asset.decode task и перечислены в canonical task result. Все artifacts
должны быть ready, unexpired и иметь exact object version в настроенном bucket.

Go loader закрывает SQL rows до S3 IO, читает master с лимитом, повторно проверяет
bytes/SHA, строгий WorkingImage, locked work ICC и точное совпадение nested refs.
Preset envelope допускается только с active snapshot copy, canonical config hash
и target ICC из trusted registry map. Контролируемые тесты проверяют успешную
связку, закрытие cursors и отказ при rebound pixel artifact. SQL89 live не проверен;
lease, params publication и task insert остаются следующим commit boundary.

## Portal download status/cancel (SQL88)

Guest status является function-only проекцией exact session/share/policy job.
Files/bytes и volumes агрегируются сервером, URL и внутренние run/task/artifact
поля отсутствуют. Текущая download permission проверяется заново, поэтому
revoked/stale policy не позволяет продолжать наблюдение через старую cookie.

Cancel под job→run locks устанавливает cancel_requested и `cancelling` для обоих
ресурсов. Tasks останавливаются существующим heartbeat/reaper/idle drain, после
чего общий `finalize_run_cancellation` переводит run в cancelled. SQL88 trigger
атомарно отражает cancelled/failed terminal run в download_job, не меняя уже
ready/expired job. ZIP/output cleanup остаётся retention-driven. Live PostgreSQL
и cancel/commit race пока не проверены.

## Download volume planner и archive worker

`workflow.PlanDownloadVolumes` принимает только ordinal-ordered snapshot sources
с exact version/artifact/SHA/bytes. Он сохраняет порядок, детерминированно пакует
до 5000 файлов и 10 GiB на volume, а элемент больше лимита помечает `single` и
не теряет из manifest. Имена приводятся к NFKC, flatten до basename, очищаются от
controls/separators, ограничиваются 180 UTF-8 bytes. Casefold-коллизии получают
`__sha8` и при необходимости ordinal; `manifest.json` зарезервирован worker-ом.

`archive.build` теперь является исполнимой операцией resource class `archive`.
Worker принимает строгий `ArchiveParams`, требует exact ordered соответствие task
inputs, резервирует scratch до записи, потоково читает каждый gateway artifact
блоками до 1 MiB, повторно проверяет bytes/SHA и пишет ZIP64 без промежуточной
копии входов. Entry timestamps/permissions детерминированы; встроенный manifest
добавляется последним. После close worker перечитывает ZIP, проверяет CRC, имена,
считает SHA всего архива и публикует immutable archive+ArchiveManifest. Любое
несовпадение ломает весь task; частичный архив не коммитится.

WorkerConfig и registration preflight теперь разрешают отдельный class=archive;
CPU/GPU/media наборы операций не получают archive.build. Контролируемый тест
проверяет реальное содержимое ZIP, встроенный manifest и итоговый SHA. Durable
materialization encode-задач реализована SQL90; следующий барьер после их
канонических результатов должен заполнить `download_job_items`, применить volume
planner и материализовать `archive.build`/single delivery и `download_volumes`.

SQL90 вводит download build lease, immutable `download_export_plans`, fenced
публикацию params и полный seal export-стадии. До seal все `export.encode` tasks
blocked; полнота сверяется с immutable selection snapshot. При успешном seal они
атомарно становятся ready, run получает graph_sealed, а job — building. Повтор
после потери ответа принимает только побайтно эквивалентные canonical params и
ordered inputs. Никакая SQL-транзакция не держится во время чтения master из S3
или публикации params.

SQL91 фиксирует результаты encode в `download_job_items` и детерминированно
создаёт `download_volumes`; SQL92 материализует ZIP-задачи, а SQL93 принимает их
canonical результаты. Single volume указывает прямо на encoded artifact. ZIP
volume становится ready только при наличии двух output roles одной successful
archive task: `archive` и `archive_manifest`, оба перечислены в canonical result.
Публикация job атомарна относительно всех томов: частичный набор остаётся
building. Ошибка любого archive task переводит run/job в failed. Готовые delivery
artifacts получают тот же 24-часовой TTL, что и download job.

## Portal download start (SQL87)

`start_portal_download` связывает immutable selection с resolved active
`export_presets` row только если preset ID присутствует в текущем
`share.allowed_presets`. Под link/session locks повторно проверяются download
permission, exact policy version и TTL. До создания задания вся selection ещё раз
сверяется с published collection, project download_allowed, membership,
publication, accepted version и ready master. Любое отозванное фото отменяет
создание всего job.

Транзакция создаёт run kind=download и download_job со snapshot ID, share ID,
guest session principal, policy_version и копией `{id,revision,hash,config}` preset.
Run config включает snapshot digest и resolved preset hash, поэтому последующий
compiler не читает изменяемые defaults. API генерирует job/run IDs и использует
30-дневный session-scoped idempotency record. SQL90–SQL94 реализуют durable build,
тома и одноразовые intents для готовых заданий.

SQL157 реализует короткоживущий intent для одного item и разрешённого share
preset. Создание фиксирует текущие item, preset и готовый export точной принятой
версии; fallback на master отсутствует. Redeem повторно проверяет guest session,
policy version, download permission, allowlist пресетов, публикацию, moderation,
asset version, export и exact object version, затем атомарно помечает intent
использованным до подписи URL. PostgreSQL-сценарии проверяют успешную одноразовую
выдачу и отзыв после изменения политики.

## Durable retry_media lifecycle (SQL149–SQL152)

`retry_media` проходит через отдельный durable intake, а не попадает в очередь
исполнения как псевдо-TaskCommand. Collection worker принимает только точное
outbox-событие frozen bulk snapshot. Scheduler арендует intent с UUID fence,
повторно проверяет текущую collection item/version и загружает exact version
канонического WorkingImage manifest с проверкой bytes, SHA и вложенных refs.

Компилятор детерминированно разворачивает группы thumbnail/preview в
thumb320, thumb640, preview1600 и preview2560. Run ID и task IDs выводятся из
event ID. Для каждого узла SQL сохраняет variant, operation, profile hash,
ordered inputs и ожидаемый params SHA. Узел регистрируется до object IO; затем
fenced reservation публикует immutable params document. Task может быть создан
только при точном совпадении node, operation registry, params artifact, inputs,
input digest и bounded execution profile. Seal требует полный набор nodes,
ready documents и tasks, после чего единым commit переводит задачи в ready.

Финализатор принимает только два output каждой successful canonical попытки:
`derivative` и `derivative_manifest`. Artifact обязан совпасть по task,
attempt/fencing token, SHA из canonical result, state и object version. Только
после терминальности всей семьи cache `derivatives` обновляется по уникальному
ключу version/variant/profile. Успех и ошибка отражаются в media job, delivery
run и исходном bulk item result; ошибка переводит bulk в completed_with_errors.
Scheduler включает отдельные build/finalize loops, readiness и counters.
Live PostgreSQL тест покрывает intake replay, lease replay, полную материализацию
четырёх задач и canonical adoption четырёх derivative rows.

### Живая проверка UP01

`TestIntegrationConcurrentQuotaReservationHasOneWinner` запускает два `upload.Service.Begin` одновременно для одного tenant при лимите 1500 bytes / 1 source file. Блокировка `tenant_usage` сериализует решения: проходит одна резервация 1000 bytes, проигравшая транзакция не вызывает multipart storage, counters остаются 1000/1 и после abort победителя становятся 0/0. Тест входит в обязательный `make test-db` через `tests/integration/test_go_services.py`.

### Живая проверка UP02 и migration 00169

`TestIntegrationStorageCompletionUnknownOutcome` использует временную PostgreSQL и отдельный versioned MinIO bucket. Сценарий `before` имитирует отказ до `CompleteMultipart`: lease освобождается, объект ещё отсутствует, повтор завершает multipart. Сценарий `after` выполняет S3 complete и теряет ответ: processor находит versioned объект через `RecoverCompletedMultipart`, читает и хеширует все bytes, затем выполняет одну SQL adoption transaction. В обоих случаях появляются ровно один staging artifact, input snapshot и ingest run; reservation не теряется и terminal replay ничего не создаёт.

Тест выявил SQLSTATE 42702 в `claim_upload_completion`: OUT-параметр `tenant_id` затенял неуточнённый столбец `tenant_usage`. Forward-only migration `00169_upload_completion_qualification.sql` квалифицирует lookup и сохраняет прежние сигнатуру, grants и lease semantics.

### Закрытие UP03

Asset verification теперь обязательно сопоставляет magic и замороженный suffix: JPEG допускает только `.jpg/.jpeg`, PNG — `.png`, TIFF — `.tif/.tiff`, RAW — только allowlist camera suffix. Несовпадение завершается `FORMAT_SUFFIX_MISMATCH` до публикации output. Focused adversarial suite проверяет forged MIME, SVG/spoof, CMYK, multipage, размеры и decompression-bomb declarations свыше 200M pixels, missing ICC без явного assumption, повреждённые данные и отсутствие staging side effects.

### DL01 и структурная проверка DL02

`TestIntegrationOwnerSnapshotAndCurrentPolicyFence` исполняет owner `Download.Start` и `CreateIntent` на временной PostgreSQL. Job фиксирует exact snapshot ID/digest; последующее добавление item не попадает в него. Удаление исходного item не изменяет исторический snapshot/архив, но запрещает новый intent. Уже выданный bearer повторно проверяет текущий `project.download_allowed` при redeem и возвращает `revoked` после отключения политики.

Planner-тест строит максимальный разрешённый snapshot из 5000 элементов, вынуждает несколько ZIP volumes и explicit `single` для oversized entries, проверяет порядок и отсутствие пропусков/дубликатов; 5001-й элемент отклоняется. Worker-тест проверяет streaming hash/size и фактический ZIP64 extra field в local header. Это структурное доказательство ZIP64, а не физический прогон >4 GiB/>10 GiB; большой disk-backed corpus остаётся открытым DL02 gate.

DL03 дополнен failure-проверками: truncated/extra/hash-mismatched input и недостаточный scratch budget завершаются до `stage`/`stage_contract`, без частичного archive artifact. Live owner test переводит старый job в `expired` и подтверждает, что новый idempotency key создаёт отдельный queued job на том же immutable snapshot. PostgreSQL finalizer failure/crash atomicity и физический multi-gigabyte DL02 corpus ещё не доказаны, поэтому P07 остаётся `in_progress`.

### Atomic download finalization (migration 00170)

Первоначальный `finalize_download_archives` валидировал и обновлял volumes в одном цикле. Если поздний canonical result отсутствовал или был malformed, `RETURN false` сохранял уже выполненные UPDATE ранних volumes и нарушал правило no partial ready. Migration `00170_atomic_download_archive_finalize.sql` разделяет операцию на два прохода под теми же row locks: первый только валидирует все archive/manifest outputs и immutable object versions, второй публикует volumes. Ошибка SQL во втором проходе откатывает весь statement.

`test_download_finalizer_atomicity.py` исполняет функцию на временной PostgreSQL с двумя terminal tasks: первый имеет полный canonical result, второй — только manifest. Результат `false`; оба volumes остаются `queued`, job остаётся `building`. Статический тест дополнительно удерживает порядок validate-before-update в forward-only migration.

### Встроенный шрифт и запуск contact sheet

Миграция `00195` регистрирует Noto Sans Regular как tenant-scoped immutable `font/ttf` artifact с SHA-256 `b85c38ecea8a7cfb39c24e395a4007474fa5a4fc864f6ee33309eb4948d232d5`. Исходный font распространяется вместе с OFL 1.1. Reservation и completion выполняются двумя транзакциями вокруг object PUT, учитывают tenant quota и восстанавливают uncertain PUT по детерминированному ключу. Resource включён в закрытый tenant purge allowlist. API строит ordered entries из готовых `preview1600`, а приложение сначала фиксирует selection snapshot и затем запускает contact-sheet run.
