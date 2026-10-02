# 06. Atomic workers и реестр операций

Полный нормативный registry: `contracts/operations.json`. Все 60+ операций имеют version, input/params/output type, model, resource class, timeout и decomposition методов. Это одна реализация registry; код генерирует из неё JSON Schema bindings и bootstrap checks. Документы 07–09 определяют математический/бизнес смысл методов.

`collection.mutate` исполняется Go worker; SMTP — Go worker вне tenant image registry, глобальные mail records и отдельный auth outbox `auth_mail_outbox(event_id,payload_id,sent_at,publish_lease_until)` (migration00001). Python не получает широкие права на memberships/collections. Image/media operations Python. Storage cleanup не регистрируется как worker operation: Go maintenance получает один fenced ticket только через maintenance SQL function, удаляет exact object version и подтверждает тем же token.

## Runtime классы и подписи

```python
class Operation(Protocol[InputT, ParamsT, OutputT]):
    name: str
    version: str
    def estimate(self, inputs: InputT, params: ParamsT) -> ResourceEstimate: ...
    def validate(self, inputs: InputT, params: ParamsT) -> None: ...
    def execute(self, ctx: TaskContext, inputs: InputT, params: ParamsT) -> OutputT: ...

class TaskContext:
    lease: Lease                 # task, attempt, fence, deadline, tenant
    artifacts: ArtifactStore
    scratch: ScratchSpace
    model_pool: ModelPool
    def check_cancelled(self) -> None: ...
    def progress(self, done: int, total: int, phase: str) -> None: ...

class ArtifactStore:
    def materialize(self, ref: ArtifactRef, scratch: ScratchSpace) -> Path: ...
    def open_stream(self, ref: ArtifactRef) -> BinaryIO: ...
    def stage(self, path: Path, role: str, metadata: ArtifactMeta) -> StagedArtifact: ...
    def verify(self, artifact: StagedArtifact) -> VerifiedObject: ...

class ModelPool:
    def acquire(self, model_id: str, lock_id: Hash, backend: Backend) -> ModelHandle: ...
    def release(self, handle: ModelHandle) -> None: ...
    def evict(self) -> None: ...
```

`Consumer.handle(message)->AckDecision`: parse bounded envelope, registry lookup, claim, download params/hash, validate, estimate/admit, execute one op, stage/verify, fenced commit, ack. `HeartbeatLoop.run(stop)` не зависит от GIL-heavy inference (separate IO thread/process). `Runtime.select_backend(operation,profile,doctor)` выбирает только проверенную комбинацию; вся смена записывается. `Runtime.on_oom` освобождает модель/буферы, публикует failure; следующий profile назначает Go. Самовольный retry внутри execute запрещён кроме повторов короткого network read в пределах deadline (max3, backoff1/2/4s).

`ScratchSpace.reserve(bytes)->Reservation`, `path(role)->Path`, `cleanup()` разрешают только `/work/tmp/{tenant}/{task}/{attempt}`; проверить realpath, no symlinks, 0700. Cleanup не следует путям из JSON. `ModelHandle.infer(tensors)->ModelOutput` чистый inference, weights eval/inference_mode. fit использует отдельный autograd context; модель сегментации выгружена до fit.

Все публичные методы в registry methods имеют шаблон `method(ctx, typed_inputs, typed_params)->typed_output` внутри execute; helpers чистые и не создают jobs. `validate` и `estimate` обязаны завершиться до тяжёлого decode/allocate. Любая возвратная численная array сохраняется artifact с metadata, не Rabbit bytes. Отсутствующая обязательная модель →MODEL_UNAVAILABLE, не silent skip. Низкая confidence →success with review/disabled effect, а не technical failed.

## IO и численные типы

`WorkingImage` — ref на TIFF16 или tiled float32 memmap artifact + Canvas, ColorSpace, AlphaPolicy, source hash. Intermediate float32 storage: header JSON (shape,dtype=<f4,order=C,space,sha) и raw binary; архивный NPZ только небольшие preview/depth, allow_pickle=False. Не округлять каждую стадию в uint16: quantization только decode contract/export. ROI reads bounded; полное frame в RAM допустимо только если estimate+profile разрешает, не default.

`TileReader.read(rect,halo)->Tile`: global pixel centers `(x+.5)*scale-.5`, clipped bounds, reflected edge для blur и explicit border mode для remap. `TileWriter.write_core(tile)` без overlap accumulation для color; restoration использует weighted overlap с нормализацией weights. `WarpReader.required_source_bounds(tile,field)` включает max displacement+interpolation support. `MaskRef` mask uint16/float32 probabilities, Canvas/TransformChain, object_id, confidence; labels nearest, soft masks bilinear+guided.

`ResourceEstimate={ram_bytes,vram_bytes,scratch_bytes,output_bytes,pixel_count}` использует uint64 overflow checks, stage-aware buffers, model weights и libvips cache. Upscale4x 45MP→720MP больше MAX_PIXELS200MP: request отклонён OUTPUT_PIXELS_LIMIT; тест4x выполняется на допустимом размере либо с явно поднятым системным лимитом после disk preflight. Нельзя silently resize до лимита.

## Параметры операций

Общие runtime limits не входят в переносимый style: `ExecutionProfile` хранит tile/sample/ROI/backend/precision/lock. Ниже все semantic params; наследование означает typed composition, не arbitrary dict.

| Семейство Params | Поля |
|---|---|
| Verify/Inspect/Decode | allowed_formats, expected_bytes, max_pixels, assume_profile nullable, purpose analyze/apply, raw_recipe_id, work_profile_id, orientation_policy once |
| Registration | mode similarity/crop_aware, long_edge1024, min_overlap .9/.5, max_bg_error_px1, manual_transform? |
| Scene/Person/FaceDetect/Landmark/Pose/FaceParse/SceneParse/Alpha/Depth | preprocessing_id из model lock; никаких пользовательских weight paths; confidence_threshold по algorithm version |
| Stats | feature_version stats20_v1, coverage_bins8, transparent_pixel_policy exclude |
| PromptMask/MaskRaster | source_sha256, object_id, selector, positive/negative normalized points, bbox?, strokes/polygons, protected mask refs, output_canvas |
| MaskTransform | transform_chain_ref, target_canvas, label_or_probability, refinement_radius |
| Matting | trimap_low .1, trimap_high .9, band_radius max(2,round(.005*long_side)), max_roi1024 |
| ColorFit | seed42, samples65536, block_holdout .2, stages400/600/200, lrs .01/.003/.01, loss_weights, early_stop100, checkpoints25, initializations<=3 |
| BodyFit/BeautyFit/DefectFit | algorithm_id, bounds из StyleV2, ignore/protect refs, min_confidence .75, evidence_mode fitted |
| Aggregate/StyleValidate/Package | accepted_pair_ids, session_split_ref, mode fixed/adaptive/experimental, test_policy, algorithm versions, canonicalization JCS |
| Group/Cull | thresholds pHash6/dino.94/time3s, clock_offsets, manual_groups/picks, target_count xor fraction, eye_policy review, never_delete true |
| Resolve | mode, strict boolean, group_fixed_prototype_id?, strength; style_ref/hash обязательны input |
| SeriesMeasure/Series | comparable_region_ids, reference medoid/id, ev_limit .35, rgb_limits [.95,1.05], min_confidence .75 |
| Denoise/Upscale | strength[0,1], model_id fixed, scale2/4, protected refs, color_adapter_id work_srgb_delta_v1, gamut_error_limit .02 |
| Body/Color/Local/Beauty/Defect/Portrait | соответствующие fully resolved schema fields 07/08/15; result всегда requested/applied + evidence |
| Repair | decision_ref/hash, accepted_candidate_ids, strength, max_diameter_face_fraction .012, protected refs |
| Dust | camera_id, sensor transforms, min_repeats5, policy review/conservative_auto, candidate masks |
| Composition/Crop | aspect pair positive ints, headroom[0,1], rotate_limit5, perspective_area_limit .15, protect_landmarks, confirmed_transform? |
| Inpaint | confirmed_mask_ref, context_px=max(32,round(.1*roi_long_side)), max_roi1024, feather_px<=64, protect refs |
| Generation/Outpaint | prompt<=2000,negative_prompt<=2000,seed uint32,steps1..50(default25),guidance0..20(default7.5),strength[0,1],canvas512²,scheduler DDIM,mask,feather,variant_no,side; require_acceptance=true |
| Compose/Blur/Sky | mode solid/asset/transparent,asset ref/hash?,solid_rgb?,matte?,position/scale,contact_shadow; blur_strength[0,1],layers8,focus_depth[0,1]; sky match_strength[0,.25] |
| HDRRegister/HDR/Window | explicit bracket refs,exposure_seconds positive,reference index,mode debevec/mertens,WB recipe,tonemap gamma1,window_mask?,review motion/parallax |
| Quality | baseline refs,metrics set,threshold_profile_id,stage master/export,protected masks,seam coords |
| Thumbnail/Preview | profile_version thumb_v1,variants allowlist,watermark policy,metadata preset guest_safe,matte? |
| Resize/Sharpen/Watermark/Encode/Metadata | ExportPreset fully resolved; resize aspect preserved; sharpen radius[.1,5]px,strength[0,1],threshold[0,.1],max_delta .03; watermark portable asset ref,opacity[0,1],anchor,margins; encode format/ICC/quality/alpha |

`export.encode` теперь имеет исполнимый CPU adapter. `EncodeParams` привязывает
exact WorkingImage manifest, pixel/alpha refs, target ICC ID+SHA, contain size,
upscale policy, format, JPEG quality/444 и alpha preserve/matte. Adapter требует
ordered declaration всех refs, проверяет work ICC по lock registry, читает только
validated float32 NPY, выполняет LCMS RGB16 transform и Lanczos/area resize.
JPEG всегда matte, 4:4:4 и embedded target ICC; PNG/TIFF сохраняют alpha при
preserve и 16-bit channels. TIFF пишет Deflate+predictor и ICC tag.

Decode contract хранит RGB как float32 NPY, но alpha как uint16 NPY. Encode читает
эти типы раздельно и нормализует alpha `/65535`; подмена float32 alpha отклоняется
проверкой NPY layout. Regression test прогоняет реальный uint16 alpha через PNG и
проверяет RGBA/крайние значения, поэтому JPEG-only тест не маскирует этот boundary.

Источник изображения не содержит EXIF, поэтому encode принимает только
`metadata_policy=strip`. Политики copyright_only/safe_camera применяет отдельная
будущая `metadata.write`; encode не заявляет, что сохранил metadata, которой не
получал. После записи файл повторно открывается, сверяются dimensions/alpha/ICC,
и только затем публикуются encoded image и строгий EncodedImage manifest.
| Archive | job_id,volume_no,manifest hash,scratch reservation,expires_policy; filenames из snapshot only |
| CollectionMutate/Cleanup/ContactSheet | bulk_id/chunk range/revision; cleanup candidate IDs+cutoff; paginated previews/locale/labels/font lock |

Progress/errors: use stable codes from15. `execute` не пишет business state напрямую; exception translated в TaskFailure. Go collection/mail adapters соблюдают те же claim/commit/ack semantics, исключая global mail tenant context.

## Реализованный scratch/transfer контракт

`runtime/scratch.py` создаёт только новый каталог `{root}/{tenant_uuid}/{task_uuid}/{attempt}`; существующий каталог попытки не переиспользуется. Root задаёт оператор и он не должен содержать symlink. Подкаталоги открываются через directory descriptors с O_NOFOLLOW, режимы каталогов/файлов — 0700/0600. Cleanup использует descriptor-safe rmtree и не следует symlink за пределы attempt.

`reserve(bytes)` возвращает Reservation; `reservation.path(role,suffix)` либо `scratch.path(role,reservation,suffix)` создаёт файл с генерируемым именем. Перед резервированием проверяются сумма активных reservations, task budget и фактически свободный диск с запасом. `Reservation.validate()` проверяет реальные размеры и типы файлов после завершения внешних writers. `release()` удаляет файлы перед возвратом бюджета; весь ScratchSpace имеет явный lifecycle/context manager. Ограничение записи внешним numerical writer остаётся ответственностью его заранее проверенного shape/estimate; неверный размер дополнительно обнаруживается validate перед stage/commit.

`runtime/materialize.materialize` принимает ArtifactRef, проверенный размер, open_stream port и check_active. Читает блоками до 1MiB, проверяет точный размер и SHA256, сохраняет suffix из allowlist форматов. Ни URL, ни пользовательский pathname не принимаются. MaterializedArtifact удерживает reservation до close; при ошибке чтения, отмене, неверном размере/хеше неполный файл удаляется. Объектный gateway и его сетевой adapter ещё не реализованы: локальные unit tests не считаются end-to-end проверкой MinIO.

## Реализованный контекст артефактов операции

`TaskArtifacts.create(ctx,ArtifactConfig,roles)` связывает одну DB lease с TaskCredentials/Gateway/ScratchSpace. Проверяет явно выделенные scratch_bytes/output_bytes и локальный лимит; отсутствие resource_estimate не заменяется произвольным большим budget. Root задаётся оператором, attempt directory создаётся эксклюзивно. `params(ContractType)` читает pinned params artifact до1MiB, SHA256, strict duplicate-free JSON/JCS и Pydantic contract. `input(ref,suffix)` допускает только объявленные command.inputs/params, берёт размер из проверенного gateway Content-Length и резервирует место до первого body read.

`stage(path,reservation,role,media_type)` требует файл из активной reservation этой attempt и проверяет фактический размер; `stage_contract` сохраняет JCS manifest до1MiB и освобождает его local reservation после upload. Большие image/style payloads передаются отдельными файлами через stage, а не control document. Выходные ArtifactRef возвращаются adapter и затем Executor.commit_task_success. Context manager удаляет только свой attempt scratch при успехе/ошибке; чужие пути не принимаются.

18 targeted tests покрывают strict params, ограничение размера/hash, отмену, mTLS transport, owned-file staging и cleanup. Подключение конкретных operation adapters и worker CLI остаётся следующим обязательным этапом; TaskArtifacts сам не исполняет обработку изображения.

## Asset adapters

`asset.adapters.asset_operations(ArtifactConfig)` возвращает executable Operation для asset.verify/media и asset.inspect/cpu. Params `{source:ArtifactRef,expected_bytes:1..512MiB,suffix}` не содержат локальных путей; suffix выбирается из raster/RAW allowlist, source обязан присутствовать в command.inputs. Verify выполняет существующий tiled full decode, inspect — размер/SHA/signature/metadata; обе публикуют JCS manifest через owned TaskArtifacts. `VerifiedAsset={source,bytes,metadata}` и `AssetMetadata={source,width,height,bit_depth,has_alpha,orientation,icc_sha256,format}` — исполняемые строгие контракты с генерируемыми schemas. Исходник не меняется, orientation фиксируется без повторного применения.

Выходные роли SQL23: verify→verified, inspect→metadata; JSON≤1MiB, count1. Source остается отдельным входным артефактом; downstream graph обязан явно включать source ref, поскольку вложенные refs не расширяют gateway права автоматически. Progress0..3, отмена проверяется между hash blocks и raster tiles. Ошибки malformed params/format/ICC/size возвращают INPUT_INVALID без decoder/path details. Registry остальных операций, worker CLI и event-driven обновление upload/asset state ещё не подключены; наличие adapter не означает завершённый ingest API.


### Исполняемый asset.decode: текущая реализация

CPU adapter `asset.decode` включается при явном `PHOTO_ICC_ROOT`, содержащем проверенный `profiles.lock.json` и профили srgb_v1, linear_srgb_v1, work_rec2020_gamma24_v1. Сборка без ICC root не рекламирует decode в runtime preflight. `DecodeParams` наследует SourceParams: source должен присутствовать в task.inputs, expected_bytes <=512MiB, suffix из allowlist; assume_profile принимает только три указанных идентификатора или null, analyze=false, tile=256/512/1024 (default512). ICC исходника имеет приоритет; отсутствующий профиль требует явного assume_profile. analyze запрещает альфа-канал.

Через TaskArtifacts материализуется и проверяется исходник, затем резервируется width*height*(12+2 при alpha)+1MiB диска дополнительно к исходнику. `Reservation.planned_path` регистрирует случайное имя без создания файла: декодер публикует его атомарным hard link без перезаписи. Частичные файлы имеют mode0600 внутри собственного attempt dir0700. Отмена проверяется между тайлами, при проверке SHA и до публикации; finally удаляет частичные файлы. Библиотечный RAW postprocess проверяется до/после вызова, но не прерывается внутри LibRaw. RAW корпус камер и пиковая память LibRaw ещё требуют квалификации.

Декодирование сохраняет существующие camera WB/AHD/linear-sRGB RAW настройки и LittleCMS relative intent, без изменения исходника. Orientation применяется один раз. Артефакты:

- working_rgb: NPY C-order float32 [height,width,3], gamma2.4 Rec.2020 D65, значения RGB16/65535; MIME application/octet-stream.
- alpha: опциональный NPY C-order uint16 [height,width], исходная альфа без цветовой конверсии; 8-bit alpha расширяется умножением на257; тот же MIME.
- working_image: WorkingImage JSON <=1MiB, pixels/alpha ArtifactRef, dtype=float32, color profile+SHA; canvas orientation=1, space_id=`oriented:<source_sha256>`, пустой TransformChain с проверенным JCS hash. Этот canvas — исходная нормализованная система координат для дальнейших преобразований.

Все бинарные ссылки и ссылка манифеста возвращаются в task outputs для SQL attestation. Следующий task обязан явно объявить нужные бинарные ссылки в inputs; ссылки внутри JSON сами по себе не дают право скачивания. После staging бинарные локальные файлы освобождаются перед резервированием манифеста. Миграция27 разрешает только working_rgb<=2401048576 bytes, alpha<=401048576 и working_image<=1048576, по одному каждого; суммарный output budget/tenant quota продолжают проверяться gateway SQL.

Проверено 86 platform tests, включая adapter с настоящими PNG16/LCMS, точное сохранение RGB16 в рабочем профиле, отдельный alpha, отмену и отказ по scratch quota. Transport в adapter-тестах заменён контролируемым портом. SQL20–27 и полный broker→gateway→MinIO→commit ещё не проверены на живых сервисах из-за недоступного Docker. Это не квалификация всей системы.


### Исполняемый pair.register и чтение массивов

`operations/pair/register.py:execute(ctx,artifacts)` подключён к CPU runtime. RegistrationParams содержит before/after ссылки на WorkingImage JSON, seed uint32(default42), mode similarity/crop_aware, optional manual_transform (9 чисел row-major), manual_confirmed, optional background_mask MaskRef. Наличие manual_transform эквивалентно manual_confirmed=true; иначе INPUT_INVALID. Маска — float32 NPY probabilities [0,1] на точном canvas/transform_chain before. Отсутствие маски отражено warning background_mask_not_provided и review_required для измеренной регистрации. Ручная подтверждённая матрица не выдаётся за измеренную.

До чтения пикселей нужен admitted resource_estimate.ram_bytes>=268435456, manifest dimensions min8 и <=200MP, RGB float32 в work_rec2020_gamma24_v1, одинаковый ICC hash пары, без alpha. Файлы двух изображений/маски занимают отдельные scratch reservations до конца регистрации. SIFT работает на preview long-edge1024; полные кадры остаются read-only memmap, конечность проверяется ограниченными блоками. OpenCV C-вызовы проверяют отмену до/после, но не внутри одного вызова.

`TaskArtifacts.contract(ref,type)` проверяет объявление ссылки в task и читает <=1MiB с SHA/strict JSON. `runtime/arrays.py:read_array(path,shape,dtype,check_active,unit_interval=False)` не выводит форму из недоверенного файла: разрешены только NPY1/2, header<=4096 проверяется до чтения тела заголовка, C order, little-endian float32/uint16, точные размеры и EOF, обычный файл без symlink/hard links. Pickle/object, лишние байты, NaN/Inf, неверная форма/тип отклоняются; mapping закрывается даже при отмене. Для проверок конечности создаётся не более262144 bool за блок. Манифест и все бинарные ссылки должны быть явно объявлены task inputs; вложенная ссылка сама не авторизует доступ.

Итоговый PairAlignment хранит before/after refs и canvas, before_to_after row-major3x3, overlap, median_reprojection, inlier_count, confidence, crop_aware, warnings. Автоматическая матрица проектируется в similarity после восстановления полного разрешения: это убирает анизотропию от округления размеров preview. Пороги >=12 inliers, median<=1px на preview и overlap>=.9/.5 проверяются для опубликованной матрицы. Ручной путь сохраняет confidence=user_confirmed, median=null, inliers=0. crop_aware не разрешает downstream body fit. Масштаб ограничен[.1,10], reflection/shear запрещены.

Миграция28 разрешает только role alignment, application/json, <=1MiB/count1. Адаптер возвращает immutable manifest reference и review_required при warnings. Пиксельный warp не является скрытым выходом регистрации: матрицу и источники потребляет следующий этап fitting.

Проверки: 102 platform tests прошли, включая реальный SIFT/RANSAC/ECC через adapter, ручное подтверждение, разные размеры preview, отмену, память preflight и malformed NPY. Controlled artifact ports в adapter tests не заменяют будущий broker/DB/gateway/MinIO integration. Миграции20–28 пока не проверены на живом PostgreSQL; граф analyze, маски моделей и style acceptance ещё не подключены. Legacy source/config SHA baseline не изменился.


### Адаптер pair.fit.color: контракт и граница запуска

`operations/pair/fit/color.py:execute(ctx,artifacts)` и factory `color_fit_operations(config)` реализуют одну пару, resource_class=gpu по catalog. Factory включён только для PHOTO_WORKER_CLASS=gpu с явным PHOTO_WORKER_BACKEND. CPU/media процесс не объявляет GPU operation установленной. Прямые adapter tests не доказывают готовность production worker или полного analyze graph.

ColorFitParams: pair_id UUID, before/after WorkingImage refs, alignment PairAlignment ref, optional person MaskRef, seed uint32(default42), samples=65536, local_requested=true, geometry_mode=similarity_only. Нельзя передать урезанные optimizer stages через params. Body fit в этой версии не подменяется similarity; geometry mode явно ограничен, полный geometry path остаётся отдельной обязательной реализацией.

Перед IO проверяются execution_profile.backend=cpu/mps/cuda, ram_bytes>=1GiB; mps/cuda требуют vram_bytes>=512MiB. tile_size=256/512/1024(default512); backend берётся только из уже admitted profile, недоступный backend отклоняется без fallback. Реальные пиковые CPU/MPS/CUDA ресурсы полного размера ещё требуют квалификации; эти минимумы не являются универсальной оценкой памяти всей задачи.

Alignment обязан ссылаться на точные before/after artifacts и совпадающие canvas. Оба изображения float32 gamma2.4 рабочего ICC с одинаковым profile hash, без alpha и <=200MP. Person mask — selector=person, float32[0,1], точные after canvas/transform_chain; binary artifact должен быть объявлен input. Отсутствующая маска или confidence<.75/ненадёжное sampled coverage отключает local fitting с явным warning и review_required. local_requested=false — явное global-only. Для корректной, но не улучшающей holdout local fit отключение остаётся решением optimizer, без выдуманных локальных параметров.

Adapter материализует входы, вызывает prepare_samples, закрывает все source/scratch mappings до оптимизации. Используются неизменённые FitOptions defaults:400/600/200, learning rates .01/.003/.01,3 initializations, evaluation25,patience100; local_steps=0 только при явном global-only или ненадёжной/отсутствующей маске. check_active проходит в подготовку и optimizer. Progress имеет пять этапов; runtime отдельно продлевает lease.

ColorFit JSON содержит исходные params/refs, GlobalColor, enabled LocalColor либо Disabled, holdout loss, median/p95 DeltaE00, выбранную initialization, optimizer_version, фактический backend, sample evidence/counts, историю checkpoint и warnings. transfer_confidence всегда unvalidated: reconstruction fit одной пары не доказывает перенос на новые фотографии. Warning регистрации переносится как alignment_review_required. Все numerical fields конечны, квантильp95>=median, seed evidence согласован с params.

Миграция29 разрешает role=color_fit, MIME application/json, максимум1MiB/count1. Публикация только через TaskArtifacts.stage_contract, fenced commit остаётся Executor/SQL. Сам адаптер не меняет style acceptance/version business state. Миграции20–29 и полный broker→DB→gateway→MinIO flow ещё не проверены на живых сервисах.

Проверка этой итерации:141 platform tests прошли; fitting adapter использует настоящий default optimizer на identity и нетождественной паре (controlled artifact transport). Ошибка independent holdout нетождественной пары снизилась более чем на30%. Evidence: work/evidence/color-fit-platform-tests.log. Исходный legacy SHA baseline не изменён.


### GPU runtime и capacity: миграция30

CLI принимает resource_class=gpu и требует PHOTO_WORKER_BACKEND=cpu/mps/cuda без default/auto. `pair.fit.color` доступен без model lock. Model operations добавляются только при одновременно заданных абсолютных `PHOTO_MODEL_ROOT` и `PHOTO_MODEL_LOCK`: JSON lock повторно hash-валидируется, а каждая загрузка проверяет approved flag, file size/SHA и `passed` evidence именно выбранного backend. TorchScript architecture IDs различаются для SCUNet, Real-ESRGAN x2/x4 и Big-LaMa; SD1.5 загружается Diffusers только из verified local tree с `model_index.json` и `local_files_only=true`. Operation wrapper отклоняет admitted profile другого backend до IO. CPU/media допускают только cpu. GPU preflight вызывает overload worker_runtime_operations(worker,'gpu',backend): enabled node, doctor_report_sha256, certificate, session_user, ровно один backend в регистрации, GPU class и qualified operation version1. Это проверка утверждённой оператором регистрации; процесс не присваивает себе квалификацию.

Миграция30 добавляет heartbeat_accelerator_worker с lock order node→registrations, точными class/backend/operation checks, safe integer bytes и обновлением capacity с ограничением budget. CPU fallback gpu-класса разрешён только при node.vram_budget=0 и report.vram=0. MPS/CUDA требуют положительный VRAM budget. Старый report_worker_capacity более не EXECUTE для photo_worker, иначе новые проверки можно было бы обойти. Внешнее provisioning обязано использовать новые функции; migration forward-only.

measure_accelerator сохраняет64MiB disk headroom. CUDA использует mem_get_info(device0), free<=total, минус64MiB VRAM; device_count должен быть1 (оператор ограничивает CUDA_VISIBLE_DEVICES). MPS использует min((host_available-64MiB)/2, recommended_max_memory-driver_allocated_memory-64MiB) для VRAM и половину оставшегося host_available для RAM. Учитывается driver memory с кешами, не только tensor memory; RAM+VRAM report не превышает host free. Это консервативное разделение unified memory, не два независимых пула. CPU worker на GPU-узле по-прежнему не освежает GPU capacity.

CapacityReporter публикует первый report до потребления, затем каждые10s; отказ останавливает consumer и активный context. Чистый CPU fallback не становится автоматической сменой backend: новую попытку/профиль назначает scheduler по квалификации. Минимальный tile_size для fitting256, поэтому профиль recovery должен иметь min_tile_size>=256.

Локальное измерение MPS записано в work/evidence/mps-capacity-measurement.json: удалось прочитать host/Metal availability. Это не smoke inference всех моделей и не CUDA проверка. CUDA coverage здесь с подменённым measurement port. SQL integration tests добавлены, но миграции20–30 ещё не прогнаны на живом PostgreSQL из-за недоступного dev Docker. Provisioning сертификатов/registrations, полная аппаратная квалификация и end-to-end обработка остаются открыты.


### Исполняемый color.stats

CPU registry включает `operations/color/stats.py`. StatsParams={image:WorkingImage ref,person:MaskRef}; person selector обязателен, canvas и transform_chain совпадают с WorkingImage. Неизвестная/отсутствующая маска не заменяется нулями. Вход RGB float32 work_rec2020_gamma24_v1; person float32[0,1], optional alpha uint16. Прозрачные alpha=0 пиксели исключаются, alpha>0 участвуют. Полностью прозрачный кадр возвращает INPUT_INVALID/EMPTY_VALID_REGION, не выдуманные признаки.

Нужен CPU profile с admitted RAM>=256MiB и scratch дополнительно8*pixels+8192 для luma float64 NPY. Source RGB/person/alpha имеют свои одновременно действующие резервы. Входы проверяются через declared TaskArtifacts contracts/arrays. Temporary workspace закрывается/удаляется до staging. Миграция31 разрешает только stats JSON<=1MiB/count1. Результат Stats20 сохраняет feature_version=stats20_v1,20 значений,512 coverage flags,mask_confidence; confidence<.75 добавляет person_mask_low_confidence и review_required.

`algorithms/statistics.py:compute_stats20_bounded` читает RGB блоками<=16384 пикселей, gamma24→linear только для блока; RGB/chroma population moments объединяются устойчивым методом Welford, собираются dark/bright/person fractions и quadrant means. Пустой quadrant получает global mean luma. Coverage8³ совпадает с эталоном. Luma записывается последовательно только для валидных пикселей; exact5/25/50/75/95 percentiles вычисляются in-place partition caller-owned workspace и линейной интерполяцией, без аппроксимации histogram. Cancellation проверяется между блоками и до/после partition; сам native partition не прерывается внутри.

Проверки Stats20:153 platform tests прошли, затем отдельный4MP memory test подтвердил traced temporaries<16MiB без уже выделенных RGB/person/workspace. Reference Stats20 parity<1e-11 на разных формах, включая1x1 и строки длиннее блока; отдельно проверены gamma conversion, пустая прозрачность, mask canvas, cleanup и review. Evidence: work/evidence/stats-platform-tests.log, work/evidence/stats-memory-tests.log. Legacy baseline сохранён. Все актуальные миграции применены на чистой локальной PostgreSQL в интеграционном наборе.

`mask.person` реализован как точный adapter torchvision LR-ASPP MobileNetV3 Large: version-pinned model lock, `person_guided_v1`, nonlinear sRGB long-side768, ImageNet normalization, softmax класса VOC person=15, bilinear full-size probability и существующий bounded guided refinement. Alpha=0 принудительно исключается. Публикуются float32 probability, PNG16, строгий MaskRef и PersonReport. Runtime не загружает веса из сети и регистрирует операцию только при наличии точного lock. Unit adapter проверен fake engine, включая shape21×H×W, softmax, alpha, публикацию и отказ неверного lock; реальная точность требует внешних лицензионных весов и visual corpus. Apply graph использует эту операцию перед `color.stats` для adaptive/local styles.

`pose.detect` реализован как version-locked adapter torchvision Keypoint R-CNN ResNet50 FPN COCO_V1. Вход переводится из рабочего Rec.2020 gamma2.4 в nonlinear sRGB; alpha=0 зануляется до inference. Torchvision transform закреплён min640/max1024, результат принимает только согласованные boxes/scores/keypoints/keypoint_scores, сортирует людей по bbox и ограничивает их число. Каждый Pose17 получает UUIDv5 из source SHA, runtime lock, устойчивого ordinal и нормированного bbox; точки сохраняются в координатах исходного Canvas с индивидуальной confidence и точным TransformChain. Операция публикует один bounded PoseSet JSON и review warnings для пустого/усечённого/неуверенного результата. Factory требует точные architecture, weight ID и preprocessing ID, сеть во время runtime не используется.

`face.detect` реализован через заранее экспортированный и hash-verified TorchScript adapter исходного face-alignment SFD checkpoint. Препроцессинг закреплён как nonlinear sRGB long-side1024 без upscale → BGR255 → mean[104,117,123]. Кандидаты обязаны быть finite Nx5 и лежать внутри preview; threshold и детерминированный IoU NMS выполняются вне модели. UUIDv5 лица зависит от source SHA, runtime lock, устойчивого ordinal и нормированного bbox. `face.landmarks` принимает только FaceBoxes того же image ref, строит square crop256 с expansion1.25, исполняет FAN batch=1 и отображает 68 точек обратно в исходный Canvas, сохраняя object_id лица. Оба factory требуют точные architecture/weight/preprocessing IDs, не скачивают веса и публикуют bounded JSON. Миграции00126–00127 задают единственные допустимые output roles. SFD/FAN код проверен controlled inference ports; реальная точность и hardware compatibility требуют внешнего lock, doctor и visual corpus. Следующий обязательный geometry шаг — association и `pair.fit.body`.

`pair.fit.body` реализован как CPU fitting task с полным provenance пары. Он отклоняет crop-aware registration, проверяет exact before/after WorkingImage, PairAlignment, PoseSet, person MaskRef Canvas/TransformChain и optional paired FaceLandmarks. До чтения массивов все artifacts должны быть declared inputs. Люди сопоставляются Hungarian assignment по стоимости `0.5*(1-IoU)+0.5*normalized_landmark_error` после преобразования before pose точной similarity-матрицей. Margin альтернативы `<0.1` даёт identity и `ambiguous`, без подгонки по выбранному наугад человеку. Silhouette widths берутся bounded cross-sections person mask у torso/waist/hips; длина ног — по hips/ankles; optional jaw0–16 даёт face_slim. Коэффициенты неотрицательны и ограничены StyleV2: face≤.05, torso/waist/hip≤.08, leg≤.03. Confidence учитывает pose, обе mask confidence и match cost; `<.75`, missing anchors и ambiguity публикуют нулевые applied coefficients с явной причиной. Миграция00128 разрешает только BodyFit JSON.

`geometry.body` реализован как CPU task `bodywarp_v1`. Его `BodyParams` связывает exact WorkingImage, PoseSet, person MaskRef, optional FaceLandmarks и коэффициенты по UUID позы. Алгоритм строит inverse output→input Gaussian RBF: torso/waist/hips/ankles и optional jaw дают управляющие точки, восемь границ ROI фиксируют identity, person probability ограничивает воздействие. Несколько людей объединяются один раз; пересечение активных полей считается `ambiguous_overlap` и даёт identity/review вместо двойной деформации. Для каждой из максимум четырёх попыток сила уменьшается вдвое; поле принимается только при displacement≤`.02*max(width,height)`, sampled Jacobian determinant≥`.5` с шагом≤4px и displacement защищённого фона≤`.1px` в масштабе preview1024. Небезопасный результат публикует identity с причиной. RGB ресемплируется ровно один раз bilinear в linear-light и возвращается в gamma24; alpha использует то же поле. Нулевая сила и safety skip копируют значения пикселей точно. Публикуются immutable inverse field, RGB/alpha, новый WorkingImage с одним DenseWarp TransformStep и BodyWarpReport. Политики ролей заданы миграцией00129; схемы `body-params` и `body-warp-report` генерируются из Pydantic. Corpus-проверка background-line acceptance и реальные model weights остаются внешними validation gates; следующий orchestration шаг — включение feature epochs и geometry task в Analyze/Apply DAG.


Уточнение Stats20 boundary checks: mutable luma workspace не может перекрывать RGB, person, valid или alpha. Перекрытие alpha отклоняется до записи. Для contiguous источников блок16384 может пересекать строки: global flat indices восстанавливают row/column quadrant; это устраняет пооперационный overhead на очень узких кадрах без изменения координат. Noncontiguous views сохраняют bounded row-slice fallback. Пустые dimensions и невещественный RGB отклоняются до вычисления. Проверены12 целевых tests, включая100000x1, strided inputs, alpha alias и4MP allocation bound; evidence work/evidence/stats-boundaries-tests.log. Общая квалификация системы этим не заменяется.
