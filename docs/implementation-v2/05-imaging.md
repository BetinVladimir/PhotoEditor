# Фотооперации, модели и логика

## Общая сигнатура

Каждый adapter execute(ctx:ExecutionContext, artifacts:TaskArtifacts)->OperationResult. Params — strict Pydantic Contract. Входы только exact declared ArtifactRef с транзитивным closure15. Точные формулы всех новых image operations —16; эта таблица служит индексом. Публикация через stage_contract/stage_file и существующий gateway. Derived images возвращают WorkingImage+Report; encoded exports — отдельные artifacts. Финальная коллекция изменяется только trusted adoption.

Для новых эффектов с полем strength: strength0 exact bypass, immutable original, bounded memory, cancellation per tile, protected region preservation, alpha/ICC/transform validation. Quality report включает status pass/review/fail/unavailable, метрики и ссылки на evidence. У операции нового типа обязательно output policy и qualification.

## Registry новых операций

| ID version1 / файл operations | Params → output | Реализуемая логика |
|---|---|---|
| session.describe / session/describe.py | VersionBinding[]+calibration →descriptor shards | Proxy decode, frozen embeddings, statistics, faces; ≤128 frames/shard |
| session.plan / session/plan.py | descriptors+SessionPolicy →ScenePlan | grouping/reference/robust consistency из 02 |
| learning.fit / learning/fit.py | TrainingManifest+base →CandidateProfile | ridge residual model; evaluation отдельной learning.evaluate |
| learning.evaluate / learning/evaluate.py | candidate+manifest+baseline →Evaluation | holdout loss и rendered safety gates по10 |
| capture.assess / capture/assess.py | proxy+CapturePolicy →QAReport | ROI focus/eyes/clipping; unavailable fields explicit |
| identity.compare / identity/compare.py | before+after+protected+calibration →IdentityReport | identity/landmarks/changed map; per-face pairing |
| semantic.transfer / semantic/transfer.py | intent+reference+target →TransferCandidate | correspondences, segmentation, geometry validation |
| portrait.open_eyes / portrait/open_eyes.py | base+donor+eye masks+consent →candidate+report | same-person donor из той же серии; align landmarks, color match bounded, blend only eye mask; no donor →unsupported; review всегда |
| portrait.glare / portrait/glare.py | base+glasses mask+donor? →candidate+report | prefer donor; no donor требует qualified generative adapter, marked synthetic; запрещено обещать recovery скрытых деталей |
| image.reflection / image/reflection.py | base+target mask+model →candidate+report | locked reflection separation model; сохранить protected; review; no model →unavailable |
| portrait.hair / portrait/hair.py | base+hair mask+params →image+report | bounded local tint/detail; flyaway removal отдельный reviewed inpaint |
| portrait.clothing / portrait/clothing.py | base+clothing mask+params →candidate+report | chroma adjustment с сохранением luminance; wrinkles — low-frequency correction; logos protected |
| image.relight / image/relight.py | base+depth+subject mask+lights →candidate+report | v1 Gaussian exposure field с depth attenuation по16; EV[-1,1]; label approximate, not physical reconstruction |
| optical.lens / optical/lens.py | base+camera/lens profile →image+transform+report | inverse remap distortion/vignetting/chromatic aberration calibrated profile; unknown lens→review/manual |
| optical.perspective / optical/perspective.py | base+lines/manual corners →image+transform | robust vanishing points, bounded homography, reject horizon crossing; preview crop and review |
| optical.panorama / optical/panorama.py | frames[2..20]+projection →image+provenance | proxy registration, pose solve, tiled warp/seam blend; reject disconnected graph; output≤200MP |
| optical.focus_stack / optical/focus_stack.py | frames[2..30]+reference →image+report | align, Laplacian multiscale focus weights, edge-aware blending; motion mask→review; preserve reference provenance |
| optical.negative / optical/negative.py | scan+film_base samples+curve →image+report | subtract measured orange mask in log domain, invert density, pinned color calibration; no automatic film-faithful claim |
| provenance.sign / provenance/sign.py | export+history+policy →signed export+manifest | C2PA SDK, key handle from secret store, verify signature before ready |

Данные portrait detail teeth/sclera/iris/body уже частично присутствуют: расширять существующий portrait.detail и masks, не вводить дублирующий эффект. Clothes/eyes/head IDs обязаны быть face/object-specific, не один общий mask на весь кадр.

## Вспомогательные функции

select_donor(base, candidates, person_id, eye_state)->DonorChoice: confirmed same-person association, compatible pose/scale/light; сортировать по quality/pose residual, ambiguity→review. Нельзя искать внешние изображения человека.

align_local_patch(base_landmarks,donor_landmarks,roi)->PatchTransform: least-squares similarity без local warp, формулы16; determinant>0, displacement≤5% face diagonal; outside ROI source unchanged. Failure возвращает reason без частичного write.

build_light_field(depth,lights,calibration)->float32 tiles: lights≤4, each position normalized, radius[0.05,2], EV[-1,1]; smooth bounded field, foreground boundaries учитываются halo. Материал/точную физику света из одного изображения не заявлять.

compose_safe_delta(source,proposal,mask,protected)->WorkingImage: effective mask=mask*(1-protected), затем0 везде protected>0, finite/color validation, strength blending, exact copy where mask=0. Generated mask хранить отдельно от changed mask; последующие изменения геометрии трансформируют обе.

estimate_optical_canvas(transforms,input_sizes,cap)->Canvas: вычислить границы до allocation; ceil pixel count, reject>cap, reserve all intermediates. Panorama/focus stack записывают asset_version_sources для всех frames, reference source в Canvas не заменяет multi-source provenance.

qualify_operation(operation,model,device,dataset)->QualificationReport: actual inference, seeded samples, p50/p95, peak CUDA allocated+reserved, host RSS, scratch, quality, skips. Empty dataset или только mocked adapter не даёт passed. Проверка license и factory compatibility до запуска.

## Модели и исследования

Веса для новых reflection/eye/glare/identity операций не выбираются выдуманным названием. ModelFamilySpec определён schemas.json; точные tensor interfaces16, existing ModelLockEntry хранит revision/files/licenses. Не вводить вторую несовместимую lock schema. Расширять существующий MODEL_IDS только после выбора и проверки реального adapter.

Первая deliverable исследовательского пакета — benchmark кандидатов и утверждённый ModelLockEntry. До него полностью реализуются contracts, UI unavailable state и тестовый deterministic stub только в tests; production stub запрещён. TensorRT engine hash связан с GPU architecture, TRT/CUDA versions и model lock; fallback PyTorch допускается только отдельно qualified.

## RAW и цвет

Media presentation policy revision: новые задания и общий cache lookup используют canonical profile с `renderer_policy="declared_encoding_alpha8_icc_v2"`. Кодировка рабочего изображения определяет linear-premult resize. RGB16 с одинаковыми валидными ICC копируется точно. WebP alpha проверяется после той же 8-bit квантизации, что writer; RGB вместо RGBA допустим только при quantized alpha=255 на всём изображении. Реальный RGBA alpha сравнивается по всем samples; ICC/layout/metadata и декодируемость payload проверяются до публикации.

`runs.media_renderer_policy` — immutable execution pin: существующие при 02545 runs получают `legacy_v1`, новые — текущую policy. Historical task Params/profile hash не переписываются. Legacy worker dispatch сохраняет прежний gamma24 resize и LCMS transform; возобновляемые compilers повторно читают frozen run policy. Только source-bound historical review receipts допускают обе известные policy; новые cache readers не возвращают legacy bytes как текущую policy. Unknown policy и неполная SQL installation блокируют admission/startup. Это совместимость воспроизведения, а не подтверждение photographic qualification.

Переиспользовать существующий asset.decode/ICCRegistry; добавить camera/lens fixture matrix, explicit RAW development params (demosaic, WB source, exposure compensation, highlight policy), decoder version в cache key. Никакой автокоррекции без записи параметров. Metadata export использует whitelist EXIF/IPTC, GPS off по умолчанию. JPEG/PNG/TIFF — round-trip pixel/ICC/alpha проверки. PSD/PSB interoperability сначала отдельный exporter и format qualification, не переименование TIFF.


TIFF import alpha contract: RGB8/RGB16 с четырьмя samples принимает только явный ExtraSamples=2 (unassociated/straight alpha). RGB и alpha читаются раздельно; ICC преобразует RGB без деления на alpha, включая hidden RGB при alpha0. Associated (ExtraSamples=1), unspecified (0) и неоднозначные дополнительные samples отклоняются TIFF_ALPHA_SEMANTICS_UNSUPPORTED до публикации working image. Generic libvips loader не считается доказательством нормализации associated alpha; будущая поддержка потребует отдельного закреплённого decoder contract и физических fixtures. Трёхканальный TIFF RGB без extra samples сохраняется.
