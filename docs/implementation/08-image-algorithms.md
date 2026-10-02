# 08. Маски и расширенные алгоритмы

Все функции получают typed artifacts и resolved params, возвращают новый image/mask/result artifact с requested/applied, evidence и reasons. Ни один оператор не изменяет input bytes. Численные defaults ниже — версии алгоритмов, их изменения требуют новой версии и regression. Модельные точные preprocess/weight IDs — 11.

## Маски и координаты

`Canvas={width,height,orientation:1,space_id,source_sha256}`. Нормированные точки (u,v) задаются относительно oriented source edges [0,1]; pixel center=(x+.5)/width. `TransformChain` ordered forward source→current homogeneous matrices и dense warp references с определённым inverse; dense warp не представлять одной affine. `map_points` возвращает invalid для вырезанных точек. Soft mask bilinear+guided, class map nearest. `MaskComposer.compose(target,exclude,protected)` = clamp(target)*(1-clamp(exclude))*(1-clamp(protected)). При overlapping same-property weights w_i: delta=sum(w_i*delta_i)/max(1,sum w_i); priority descending отсекает занятые pixels перед нормировкой; tie region_id.

`guided_filter(I,p,r,eps)` mean_I/mean_p, var/cov через boxFilter, a=cov/(var+eps), b=mean_p-a*mean_I, q=mean(a)*I+mean(b). Person r8 на1024, eps.0001; full r=round(8*long/1024), halo>=2r. Labels/person относятся к текущему transform version; после warp обязательно transform node или redetect. Mask object IDs стабильны в пределах source/version+detector hash+ordered bbox, не имена людей.

`ManualMask.rasterize(strokes,polygon,points)`: strokes имеют add/subtract/protect, radius fraction long side, hardness[0,1], sequence; rasterize source coordinates, хранить vector JSON + derived PNG16. Points для SAM separate model task. Brush overrides bound source hash; stale hash→OVERRIDE_SOURCE_MISMATCH. Manual mask itself не обучающий style.

## BeautyFilterV1 / PortraitDetail

`build_skin_mask(face68,parse,manual)` использует conservative polygon и skin labels, исключает eyes/brows/lips/nose/hair. Parse optional только явно capability manual mode; при lowconfidence<.75 or face width<64px skip auto. Protected permanent marks union manual polygons.

`beauty_filter(image,mask,face_width,params)`: encoded work float32, r=max(2,round(.025*face_width)); base=guided_filter(luma,image,r,.002); low=Gaussian(base,sigma=max(1,.04*face_width)); fine=image-Gaussian(image,max(.6,.004*face_width)); smooth_target=base+texture_keep*fine; tone_target=smooth_target + clamp(low_median-low,-.03,.03)*tone_evenness; blend image→target by smoothing*skin mask (tone_evenness independent contribution inside same mask); protected exact copy. Рассчитывать full ROI с support halo, не отдельно статистику каждого tile. Сохранять texture-energy ratio и на 100% reviewer QA.

`fit_beauty` bounded coordinate search smoothing/tone_evenness в {0,.1,.25,.5,.75,1}, texture_keep {.75,.9,1}; три последовательных coordinate passes, refine±.05; compare low/mid residual+penalty потерянной fine energy, holdout ROI blocks; confidence improvement over disabled and uncertainty, inconsistent pairs disabled. Не переносить дефекты в LUT.

Исполнимый `pair.fit.beauty` принимает exact aligned WorkingImage pair и 1..64 явных соответствия Face68+skin/protection. Canvas, TransformChain и color profile должны совпадать; full frame ограничен40MP, face ROI — каноническим4MP budget. Для каждого лица применяется полный grid `6×6×3`, затем три bounded coordinate passes±.05 тем же `BeautyFilterV1`. Loss объединяет low/mid residual и штраф потери fine energy; improvement<2%, confidence<.75, face<64px и invalid ROI дают disabled evidence. Межлицевой spread>.35 запрещает агрегирование. `BeautyFit` всегда `unvalidated` и требует review.

`pair.fit.defects` измеряет только компактные high-frequency residual components внутри явного skin union за вычетом permanent protection. Он не создаёт repair mask и не принимает решение об удалении: даже при положительном evidence результирующий `Defects.policy` принудительно `review`, transfer confidence остаётся `unvalidated`. `style.aggregate` опционально принимает BeautyFit/DefectEvidence, использует median только confidence-valid согласованных результатов и требует exact SFD/FAN model locks прежде включения модулей. Миграция00139 фиксирует единственные JSON outputs.

`PortraitDetail.apply_teeth_sclera`: face parsing mouth/eye ROI + bright low-chroma candidates; зубы не отдельный BiSeNet class, явная ручная маска при сомнении; EV<=.2,chroma reduction<=.25; iris/lips/glints protected. `apply_shine/under_eye`: low-frequency EV clamp±.3, requested strength scale, preserve high frequency. `reduce_cloth_folds`: confirmed clothing mask, low-frequency correction≤.3EV, logos/seams protected; no automatic any-clothes parser. `glasses`: confirmed mask tonal correction; clipped eye needs explicit generate candidate+review, не истинное восстановление. `flyaway_hair`: confirmed thin-line mask на простом фоне, protect hair boundary, small Telea else LaMa.

Исполнимый `portrait.detail` принимает exact WorkingImage/ResolvedStyle, уникальные explicit region masks и до32 permanent protected masks. Canvas/TransformChain проверяются до чтения. Effective strengths вычисляются один раз; teeth/sclera ограничены0.2EV и chroma reduction0.25, shine/under-eye/confirmed cloth — low-frequency0.3EV. Все нулевые веса и protected pixels остаются exact-copy. Glasses возвращает `review_required` и не выдумывает содержимое закрытого/clipped глаза. Alpha ref сохраняется без изменения и обязан быть declared input. Миграция00140 задаёт output roles; тесты фиксируют bounds и exact protection. Flyaway hair остаётся отдельным defect/inpaint workflow с явным подтверждением, не скрытой частью tonal adapter. Apply v4 материализует `portrait.detail` только для single-image override с типизированными explicit `portrait_*` masks, exact same Canvas/TransformChain и canonical interactive provenance; отсутствие region bindings либо одновременная geometry warp оставляет стиль вне v4, чтобы не выполнять скрытую identity-операцию или применять stale masks.

Исполнимые `features.scene` и `depth.predict` используют только SHA-проверенные ModelPool artifacts: network fallback запрещён. DINOv2 exact `facebook/dinov2-small` получает centered padded518 sRGB preview без изменения aspect, проверяет finite384D output и публикует L2-нормированный `Embedding384`. Depth Anything V2 Small exact HF architecture получает official local processor input, результат bicubic переводится на исходный Canvas и min/max-нормируется только при ненулевом диапазоне; constant result становится явной zero-depth review, прозрачные pixels отмечаются отдельным float32 invalid mask. Оба manifest содержат lock/backend/preprocess binding; миграция00141 ограничивает outputs. Реальные веса и backend doctor/визуальная depth acceptance остаются внешними аппаратными gates.

`scene.parse` загружает exact local SegFormer-B0 ADE150 через official processor, проверяет150-channel finite logits, вычисляет confidence softmax и возвращает uint16 labels на исходном Canvas только nearest interpolation. `face.parse` принимает exact FaceBoxes, делает deterministic expanded crop512, исполняет pinned TorchScript export BiSeNet Face19 и возвращает один full-canvas uint16 label artifact; overlap лиц разрешается по большей pixel confidence, tie сохраняет более ранний object UUID. TorchScript export является install-time artifact с собственным SHA/architecture/weight ID, а не runtime загрузкой кода. `alpha.predict` использует CPU-only ONNX U²-Net320, ImageNet normalization, bounded min/max output normalization, bicubic restore и пересечение с исходным alpha; публикует float32+PNG16+MaskRef. `mask.prompt` принимает до128 normalized positive/negative points и optional box, переводит их в padded long1024 coordinates, исполняет SHA-pinned MobileSAM TinyViT TorchScript runtime artifact, выбирает max-score candidate с tie по меньшему index и восстанавливает probability mask. Миграции00143–00145 фиксируют output roles. Реальные exports/checkpoints и visual segmentation/matting acceptance остаются внешними gates; при отсутствии doctor-qualified artifact операция не объявляется.

## DefectRepair и SensorDust

`propose_defects` LoG/DoG scales diameter .002.. .012 face width, connected components, skin/confidence filters, protected exclude; result candidate IDs from source hash+mask hash, score и bounding/PNG label map. Default review. `repair_defects` проверяет saved accepted_candidate_ids и mask hash; поканально cv2.inpaint float32 Telea radius clamp(1,.25*diameter,5), blended inside accepted mask; feather только внутри разрешённой маски. Не перевести master в uint8. `conservative_auto` explicit experimental: diameter<=.004, probability>=.95, redness residual high, no protected overlap; всё равно отчёт, не гарантия различения родинок.

`detect_sensor_dust` группирует same camera+sensor size+raw crop/orientation mapping; >=5 кадров, LoG компактные тёмные компоненты в low-gradient фоне; совпадение sensor coords radius2px normalized sensor; повторяемость>=80% пригодных кадров. Иначе только review. Исправление выполняет отдельная `sensor.dust_repair` после approval; camera metadata недостаточно для реконструкции sensor mapping cropped JPEG →manual review.

## BodyWarpV1

`match_objects` Hungarian assignment по IoU .5 weight + normalized landmark error .5; ambiguity margin<.1→review no fit. `build_anchors`: body axis плечи→таз, поперечные сечения person silhouette на waist/hip/torso; face anchors нижний контур, eyes/nose fixed; leg only reliable hips/knees/feet. Сидячая поза/occlusion/intersection людей/недостающие landmarks →skip body, сохраняется face если независимо безопасен.

`solve_inverse_field` Gaussian RBF displacement на local regular grid64×64 per person ROI, sigma=.2 ROI long side; ridge1e-4 least squares constrained zero background/boundary anchors. Точные anchors positions/style requested % входят fit.json. Inverse output→input field объединяет всех людей и face; overlap conflicting fields→review, не двойная деформация. `fit_body` scipy least_squares soft_l1 bounded params, objective landmark/silhouette/gradient/background-line residual с равными нормированными block weights, max_nfev200; confidence по holdout anchors и уменьшению residual, min.75.

`validate_field`: max displacement<=.02 image long side, det(J)>=.5 dense grid с шагом<=4px и corners; background line bending<=1px на preview1024, protected center displacement<=.1px preview. Нарушение →strength /2 до3 попыток; затем identity и reason unsafe. `apply_field` один bilinear remap full-res через source windows, masks тот же mapping. strength0 exact identity. body strength scales waist/hip/torso/leg, face only overall strength. После warp нельзя смешивать original/warped pixels.

Face slim[0,.05],waist/hip/torso[0,.08],leg[0,.03]; абсолютные проценты в стиле. Нельзя заменять их legacy max_displacement multiplier. Видимость uncovered background/straight-line damage →skip, no hidden generation.

## CullRank, SeriesMatch, AutoCompose

`group_exact` SHA tenant-local. `group_bursts`: candidate edges pHash<=6 AND DINO cosine>=.94 AND corrected time<=3s; complete-link clustering предотвращает chaining непохожих крайних кадров; stable sorted hash/id tie. Scene grouping agglomerative cosine+stats normalized weighted .7/.3 с distance threshold .25, explicit manual split/merge dominates. Missing EXIF time не создаёт burst, scene grouping остаётся.

`rank_frame`: score=.45 sharpness+.25(1-blur)+.15(1-clipping)+.15 eyes; компоненты robust percentile normalize внутри scene, unknown eye исключён и веса перенормированы. Face/object sharpness local; глаз EAR from FAN, initial open>.22 closed<.16, intermediate unknown; маленький/профиль/очки unknown. User intentional closed override. `select_n`: manual picks first, remaining квоты scenes пропорционально eligible count, largest remainder tie scene_id, rank tie item_id; shortage explicit. Не удалять originals.

Исполнимые `collection.group` и `collection.cull` используют immutable `SceneFeatures` refs. Feature document фиксирует item/image/content SHA, pHash64, нормированный DINO384, Stats20, corrected capture time и технические cull-компоненты. Group валидирует уникальность refs/items; exact, burst и scene clusters строятся детерминированным complete-link, поэтому цепочка A≈B≈C не объединяет непохожие A/C. Burst требует одновременно Hamming≤6, cosine≥.94 и corrected time≤3s; missing time не создаёт burst. Scene distance использует collection-wide Stats20 scale floor.05 и weighted `.7 DINO + .3 stats`, threshold.25. Manual merge/split проверяются на конфликт и имеют приоритет. Manifest хранит stable IDs от минимального UUID.

Cull повторно читает те же declared SceneFeatures и проверяет их exact binding к SceneGroups. Внутри каждой scene sharpness/blur/clipping нормируются p5/p95; unknown eye исключает eye weight с перенормировкой, intentional-closed override снимает penalty. Manual picks включаются первыми, остальные quota распределяются пропорционально eligible count методом largest remainder с tie по scene ID; rank tie — item UUID. Target shortage фиксируется явно. Результат — рекомендации и review flags, `never_delete=true`: originals не удаляются. Один task ограничен2000 items из-за control-document boundary; orchestration обязан chunk larger snapshot. Миграция00136 задаёт только JSON outputs. DINO weights/real-corpus top3 acceptance остаются внешним model/visual gate.

`balance_series`: medoid technically valid frame либо user reference; только соответствующие same-person explicit region IDs или neutrals, не межличностный skin match. Robust least_squares exposure/chroma normalized statistics, L2 regularization weight1 к identity; clampEV±.35,gains .95..1.05; mean gains geometric normalize1 до clamp. Confidence<.75 skip. Compare clipping delta<=.005 и residual to reference не ухудшился; иначе correction identity. Stats/weights одинаковы всем tiles, порядок inputs не влияет.

Исполнимый `series.measure` принимает global-colored WorkingImage и 1..32 explicit comparable regions с уникальными region IDs. Все masks обязаны совпадать по Canvas/TransformChain и не быть protected. Он измеряет weighted mean linear Rec2020 RGB/Y, coverage, confidence с coverage gate1% и clipping fraction; low coverage/confidence публикуются как review warnings. `series.balance` принимает 2..2000 exact SeriesStats refs, использует только пересечение одинаковых region IDs и выбирает medoid среди confidence-valid items либо explicit reference. Для каждого кадра `least_squares(loss=soft_l1,max_nfev=200)` решает EV+log RGB gains с L2 identity residual; gains геометрически нормализуются, затем EV clamp±.35 и gains clamp[.95,1.05]. Confidence<.75, отсутствие common regions, неулучшенный residual или predicted clipping delta>.005 дают точную identity correction и reason. Публикуются raw/applied значения, confidence и reference; порядок refs не влияет на tie благодаря item UUID. Миграция00137 задаёт bounded JSON outputs. Реальная межкадровая visual acceptance и region-mask quality остаются внешними gates.

`plan_composition`: LSD/Hough+RANSAC straight lines→rotation±5°, perspective area change<=15%; выбрать aspect crop rectangle, headroom target, penalize lost protected landmarks infinitely; maximize retained subject area затем minimize center offset, ties top-left. Нет допустимого crop→review/outpaint только explicit. Per-image homography/crop immutable image_state, не coordinates style. Большие исправления требуют review; perspective+rotation+crop объединяются одним transform для этой стадии.

Apply v4 формирует `ImageStructure` отдельной CPU operation `composition.structure`: Canny/Hough выполняются на preview с long side≤1024, линии сортируются по длине и координатам и обрезаются до explicit limit; person mask обязан совпадать по Canvas/TransformChain и даёт subject bbox и protected top-center point. Все manifest/pixel/mask refs объявляются task inputs. `composition.plan` принимает immutable structure, а `geometry.crop` добавляет ровно один matrix transform. Результат crop всегда проходит immutable per-item `composition_review`; accept выбирает новый Canvas, reject возвращает exact base, skip исключает item при adoption.

Исполнимый `composition.plan` принимает immutable `ImageStructure`: exact WorkingImage ref, нормализованные line segments, protected points и subject rectangles. Устойчивый median осевых отклонений ограничивает rotation ±5°. Детерминированный bounded search идёт от максимальной площади к меньшей, отбрасывает quadrilateral за source canvas и любой crop, теряющий protected point; tie-break — retained subject fraction, center offset, затем top-left. План содержит exact source/output dimensions, source quadrilateral, одну forward homography, измерения и disposition `apply|review|outpaint`. Отсутствие допустимого crop не создаёт скрытый fallback: план детерминированно вычисляет одну сторону и 8..8192 добавляемых pixels с лимитом 200 MP. Apply исполняет `image.outpaint` только при явной `outpaint_policy=review`, непустом prompt, pinned SD1.5 runtime и qualified backend; иначе run закрыто отклоняется. `geometry.crop` принимает только exact image/plan binding, отклоняет outpaint disposition и выполняет RGB в linear light и alpha одним `warpPerspective(Lanczos4)`. В `TransformChain` добавляется ровно один `MatrixTransform`; повторного rotate/crop resampling нет. Миграция00138 фиксирует bounded output roles. Perspective estimation по сходящимся линиям и real-corpus composition acceptance остаются внешним visual gate; текущий measured perspective correction равен identity/0, а не выдуманной оценке.

## Восстановление и фон

`work_srgb_delta_v1`: сохранять full-gamut work input; model input через float work→sRGB transform, model delta обратно через conversion difference: work(model_output)-work(model_input). Добавить delta к original linear work, blend strength. Если исходный sRGB conversion clipped или обратная delta выходит gamut с |excess|>.02 — отключить delta в pixels, feather valid boundary, report needs_review. Для upscale исходный linear work resample до output canvas, model delta на том же canvas. Это не гарантия сохранения непредставимых model-space цветов.

SCUNet before style, tile512/256 halo64; RealESRGAN после creative stages, tile256 halo32, output2x/4x, memory/disk preflight. `stitch_tiles`: cosine overlap weights, norm sum, protected edges regression. No automatic GFPGAN/CodeFormer.

### GPU adapter image.denoise

Исполняемый контракт `DenoiseParams` фиксирует `scunet_color_real_psnr`, SHA-256 `model_lock_id`, `work_srgb_delta_v1`, tile 256/512 и halo64. Фабрика принимает только заранее построенный `ModelPool`; lock сверяется до materialization пикселей, а pool разрешает только approved, hash-verified и qualified для выбранного backend вес. Каждый core tile вычисляется с контекстом halo64. Модель получает nonlinear sRGB float32; её correction переносится как разность двух linear Rec.2020 значений и смешивается с исходным linear work по strength. Пиксели, где исходное преобразование требует clipping или correction выходит за gamut более чем на .02, сохраняют original; граница safe region feather4. Alpha artifact и вся геометрия переиспользуются без изменений.

`DenoiseReport` фиксирует lock/backend/tile, доли source clipping, gamut excess и отключённой correction; любая такая доля требует review. Невалидная shape/range/non-finite model output завершает задачу без публикации. Миграция00109 разрешает только `working_rgb`, `working_image`, `denoise_report`. Численный adapter и полный artifact lifecycle проверяются controlled inference port; это не является квалификацией реальных SCUNet weights, CUDA или MPS. Runtime worker объявляет операцию только при паре `PHOTO_MODEL_ROOT`/`PHOTO_MODEL_LOCK`, валидном hash lock и наличии model entry; pool дополнительно требует approved files и `passed` evidence выбранного backend.

`image.upscale` использует тот же lock/backend fail-closed path для строго согласованной пары scale/model `2/realesrgan_x2` либо `4/realesrgan_x4`. Core256 вычисляется с halo32; модельный результат обязан иметь точный integer-scaled shape. Базовый мастер масштабируется bicubic в linear Rec.2020, nonlinear sRGB model input масштабируется отдельно, после чего high-resolution model correction переносится через `work_srgb_delta_v1`. Новый `Canvas` и `ResizeTransform(pixel_centers=half_pixel)` добавляются в непрерывную transform chain. Alpha uint16 масштабируется bilinear в отдельный immutable artifact. Миграция00110 задаёт output policies; `UpscaleReport` содержит обе площади, gamut evidence и факт resample alpha. Controlled inference проверяет геометрию и lifecycle, но не квалифицирует Real-ESRGAN weights или accelerator backend.

`geometry.body` имеет два взаимоисключающих источника параметров. В Analyze `fit` ссылается на pair-bound `BodyFit` и обязан точно совпасть с image/pose/person/faces исходной стороны пары. В Apply `coefficients` содержит переносимые `BodyCoefficients` из принятого стиля и применяется к каждому обнаруженному Pose17 текущего кадра; артефакт обучающей пары в Apply не используется. Одновременное отсутствие или наличие обоих источников отклоняется контрактом. Safety loop, предел displacement, Jacobian gate, background protection и identity fallback одинаковы для двух режимов. Тесты проверяют оба источника и точную identity при нулевой силе.

`image.inpaint` принимает только `confirmed=true`, target `MaskRef(protected=false)` и до32 явных `protected=true` masks в геометрии входного изображения. Model lock сверяется до materialization. Effective mask равен target минус union protected. ROI расширяется context32..512; для стороны более1024 сначала выполняется общий preview pass с long side1024, затем ROI refinement с padding до кратности8. Big-LaMa получает nonlinear sRGB, correction возвращается через wide-gamut delta adapter. Публикация копирует original по всему canvas и смешивает result только внутри effective mask с distance feather1..64; protected и outside pixels остаются bit-exact, alpha сохраняется по ссылке. Candidate всегда требует review. Миграция00111 разрешает `working_rgb`, `working_image`, `inpaint_report`. Controlled port не заменяет квалификацию реальных весов.

`image.generate` фиксирует local `sd15_inpaint`, DDIM v1, 25 steps, uint32 seed, guidance1..20, prompt lengths и confirmed mask. Square ROI с context64 приводится к512; после deterministic inference результат возвращается в ROI, correction переносится через wide-gamut adapter, а публикация разрешена только effective target mask с feather. Protected/outside и alpha остаются исходными. `image.outpaint` расширяет ровно одну сторону на8..8192px; initial added area получает edge continuation, mask покрывает только added canvas, source rectangle после inference восстанавливается bit-exact. Операция добавляет partial translation `MatrixTransform`, а при наличии alpha сохраняет source alpha и делает generated area opaque. Порядок сторон задаёт scheduler отдельными задачами. Оба результата всегда review candidates; migrations00112–00113 ограничивают роли публикации. Controlled inference проверяет seed/config/защиту/геометрию, но не качество или квалификацию SD1.5.

`refine_alpha`: U2NET→trimap(.1/.9), PyMatting только boundary ROI≤1024, uncertainty сохранена; transparent output straight alpha, RGB at alpha0=0 при export. `compose_background`: premultiplied linear, foreground protection, explicit position/scale, solid/asset, contact shadow blurred alpha offset opacity<=.3. `blur_background`: depth8 layers, fill hidden edge from background only, protected foreground overlay, blur max radius=.02 long side * strength; effect artistic, не f-number. `replace_sky`: SegFormer sky+manual mask, separate approved sky asset, bounded color match .25, wires/buildings protected.

Исполнимый `alpha.refine` проверяет exact WorkingImage/MaskRef geometry, строит trimap с thresholds .1/.9 и band radius `max(2,round(.005*long_side))`; band включает как исходную soft uncertainty, так и границу hard foreground/background. Closed-form PyMatting запускается только на bounding ROI boundary и fail-closed при стороне>1024, без уменьшения разрешения. За ROI и за uncertainty band исходный alpha остаётся exact; публикуются float32 mask, PNG16, MaskRef и report. Constant/no-boundary alpha даёт явный identity reason. Миграция00142 ограничивает роли. Реальная matting visual acceptance остаётся внешним gate.

`inpaint`: confirmed mask ROI +context, LaMa global downscaled pass для большой области затем ROI refinement; только mask+feather permitted, always candidate/review в выбранной консервативной продуктовой политике. `generate`: Diffusers SD1.5 inpaint, DDIM fixed config/seed, canvas512 batch1, steps default25, no cloud. После inference restore all protected/outside mask+feather original pixels. `outpaint`: scheduler создаёт отдельную operation на каждую сторону в порядке left/right/top/bottom, mask только added canvas; все итерации logged, review итогового candidate. `background generate`, product surroundings, twilight/staging — пресеты explicit prompt/mask этой операции, не другие модели.

## HDR

`register_bracket` accepts3..9 frames,positive exposure times,common WB,reference; deterministic SIFT/RANSAC/ECC preview registration, а при недостатке features/matches или недопустимой reprojection — отдельный exposure-invariant MTB translation fallback с gradient phase seed и coarse-to-fine median threshold bitmap refinement. Motion residual after exposure normalization threshold .05 linearY; deghost берёт reference, parallax with reprojection>1px1024→review. `merge_hdr` Debevec calibrated response+times→float radiance memmap; tonemap fixed Reinhard intensity0/light_adapt1/color_adapt0/gamma1→linear SDR then work encode; Mertens separate mode with its explicit non-radiometric provenance. `window_pull` confirmed window mask uses dark input aligned+exposure matched, сохраняет original вне mask. Все clipping во всех exposures = unrecoverable reason.

## QualityCheck

`check_master` checks finite,dimensions,ICC path,clipping,mask boundaries,warp Jacobian,protected diffs,review acceptance. `check_export` reopens actual encoded file, dimensions/bitdepth/profile/orientation/alpha/metadata, clipping/seams; metadata.write обязателен до проверки. `compute_reference_metrics` ΔE00 LabD50; SSIM/PSNR sRGB long1024; aggregation per-frame before dataset. `write_report` separates technical failure, review, unvalidated quality; отсутствие reference=null. Ни sharpness, ни embedding similarity не подтверждают истинность синтезированных деталей.

Исполнимый `quality.check` использует строгий discriminated режим: `working/master/master_v1` либо `encoded/export/export_v1`. Master повторно валидирует float32 WorkingImage, optional exact-geometry baseline и до32 protected MaskRef; считает boundary clipping, Laplacian sharpness, declared seam p99, MAE/PSNR и max delta только в union protected masks. Для master с alpha black/white clipping считается только по `alpha>0`: canonical RGB=0 полностью прозрачного background не является визуальным клиппингом; finite/range validation продолжает охватывать весь RGB artifact. Изменение protected более `2/65535`, nonfinite или грубый clipping дают fail; seam, flat sharpness и clipping>2% дают review. Без baseline reference остаётся null. Export открывает фактический JPEG/PNG/TIFF, проверяет orientation=1 и SHA embedded ICC против EncodedImage manifest, затем считает те же technical metrics; masks/baseline для encoded режима запрещены. QualityReport отделяет pass/review/fail и точные причины, не объявляет техническую sharpness проверку доказательством истинности content. Миграция00135 разрешает только bounded JSON report. Более полные ΔE00/SSIM long1024 и visual corpus acceptance остаются внешним quality gate.


### Исполняемая bounded mask composition

`algorithms/masks.py:compose(target,exclude=None,protected=None,out=None,check_active=...)` реализует формулу MaskComposer для2D float masks<=200MP. Exclude/protected None означает нулевую маску. Finite значения clamp[0,1]; NaN/Inf отклоняются. Explicit out — disjoint writable C-contiguous float32 ndarray либо принадлежащий scratch memmap. Alias с любым input запрещён. Без out выделяется полный результат, поэтому task adapter на full-res обязан передавать зарезервированный memmap.

Обработка блоками<=16384 элементов, strided inputs читаются строковыми slices без полной копии. Между блоками и на выходе проверяется отмена. При ошибке out может быть частично записан: caller обязан удалить reservation, не публиковать артефакт. Проверки Canvas/TransformChain остаются обязанностью будущего mask task adapter. Пока добавлено только численное ядро, inference/manual rasterization и регистрация операций отсутствуют.

mask-composer-tests.log:4 проверки прошли. На2MP traced temporary allocation<2MiB без входов/выхода; это не RSS всего worker и не аппаратная квалификация.


### ManualMaskV1: векторная маска и raster iterator

`contracts/manual_mask.py` задаёт source_sha256, source Canvas и до1024 shapes, суммарно<=10000 points. Shape — stroke либо polygon с уникальным sequence, mode add/subtract/protect. Stroke содержит1..4096 нормированных points, radius(0,1] относительно длинной стороны и hardness[0,1]; polygon3..4096 points. Порядок массива не важен, применяется sequence. Source SHA должен совпадать с Canvas и переданным actual source; несовпадение — OVERRIDE_SOURCE_MISMATCH. SAM positive/negative prompts остаются отдельной модельной задачей, не интерпретируются как мазки.

`algorithms/manual_mask.py:rasterize(mask,source_sha256,tile_size,check_active)` выдаёт readonly MaskTile(x,y,target,protected), каждый<=256/512/1024. Consumer обрабатывает iterator последовательно, не собирает список full-resolution tiles. Source normalized edge coordinates преобразуются x*width,y*height; центры pixels=(x+.5,y+.5). Stroke — минимальное расстояние до непрерывной polyline с круглыми концами. При hardness<1 профиль smoothstep от radius*hardness до radius; при hardness1 применяется однопиксельная краевая ramp clamp(radius+.5-distance,0,1). Polygon — even-odd fill с четырьмя subpixel samples(±.25,±.25), одинаковыми в глобальных координатах; self intersections имеют однозначное even-odd правило.

Add/protect используют max union, subtract target*=1-coverage. Protect хранится отдельно, не удаляя редактируемый target; effect consumer применяет MaskComposer. Выходы float32 probabilities; PNG16 staging и manual-mask task adapter ещё не реализованы. Входные векторы не изменяются. Callback отмены вызывается между tiles, shapes и segments/edges; bbox culling пропускает shapes вне tile. Allocation зависит от tile и ограниченного vector list.

manual-mask-tests.log:3 проверки прошли — sequence/add/subtract/protect, полигон по краям canvas, soft coverage, точное tile equivalence256/512/1024, duplicate sequence, stale source и отмена. Реальный редактор, PNG16 artifact publication и полный mask workflow остаются в работе.


### Потоковая PNG16 запись масок

`algorithms/mask_png.py:write_png16(mask,output,max_bytes,check_active)->written_bytes` принимает2D uint16 либо finite float probability[0,1]. Float quantization: nearest-even(float64(value)*65535). Записывается grayscale16 PNG, big-endian samples, filter0, zlib6, без gamma/ICC/RGB преобразований. IDAT<=65536 bytes; строки читаются блоками<=16384 pixels, поэтому даже широкая строка не материализуется целиком. Partial writes обрабатываются, неверный stream write вызывает ошибку. Нет зависимости от полного Pillow image allocation.

`png16_bound(width,height)` возвращает conservative reserve raw+2%+1MiB, где raw=height*(2*width+1). Фактический max_bytes проверяется перед каждой записью; budget/validation/cancellation оставляет неполный файл. Caller обязан использовать собственный ScratchSpace.open_writer и удалить reservation при ошибке; encoder не открывает пути и не публикует объект самостоятельно. Формат сверён с локальным Pillow PngImagePlugin, результат независимо декодирован Pillow с точным совпадением uint16, включая0/1/32768/65535.

mask-png-tests.log:12 mask tests прошли; PNG writer traced temporary memory<2MiB на2MP, без source/output. Это не общая RAM оценка worker. Публикация масок через gateway и task adapter остаётся незавершённой.


### mask.rasterize: подготовка и публикация task outputs

`MaskRasterParams={image:WorkingImageRef,vectors:ManualMaskRef,object_id?:UUID}`; CPU backend, RAM>=256MiB, tile256/512/1024. Проверяется точное совпадение Canvas, source SHA и пустая TransformChain: ручные векторы растеризуются в ориентированном source; warped targets получают маску через отдельный mask.transform. Нельзя молча считать warped canvas исходным. Pixel bytes source image для vector raster не скачиваются.

`prepare_raster` резервирует8*pixels+8192 для двух float32 NPY и2*png16_bound для PNG. Iterator заполняет target/protected memmaps; затем PNG16 writer кодирует оба через ScratchSpace.open_writer. Mapping закрываются до выдачи borrowed RasterFiles; consumer staging выполняется внутри context. По любому выходу context удаляет четыре локальных файла и освобождает reservations, включая отмену и ошибку consumer. NPY сохраняет исходные float probabilities для вычислений, PNG16 — portable derived representation.

CPU operation публикует mask_target/mask_protected NPY, mask_target_png/mask_protected_png и rasterized_mask JSON. Manifest RasterizedMask содержит image/vectors refs, два MaskRef с selector manual, confidence1, model_lock_id=null, object_id, Canvas/TransformChain, PNG refs и algorithm manual_mask_v1. Confidence1 означает явную ручную маску, не точность ML. Защищённая маска отмечена protected=true; consumer обязан применить её совместно с target. Target намеренно сохраняет editable content под protected pixels.

SQL35 задаёт output policies. Выходы каждого stage аттестуются существующим gateway; manifest создаётся после всех четырёх uploads. Если staging прерывается, DB task не получает успешный commit; общий GC уже ready orphan artifacts пока остаётся отдельной незавершённой работой. SQL35/live broker→DB→gateway→MinIO integration ещё не проверены. Mask transform/inference, editor и публичные API ещё не готовы.


### mask_transform: геометрическое ядро одного шага

`algorithms/mask_transform.py:transform_tiles(source,step,labels=False,inverse_field=None,tile_size,check_active)` выдаёт readonly TransformedMaskTile. Source строго float32[0,1] probability либо uint16 labels, ровно from_canvas shape, <=200MP. Float source проверяется bounded blocks<=16384 до выдачи первого результата. Caller проверяет artifact SHA/Canvas и владеет mappings. Iterator не собирается в список.

Matrix forward_matrix действует на integer pixel-center coordinates, согласованно с registration warp; inverse вычисляется float64. Crop — x_source=x_target+crop.x,y_source=y_target+crop.y. Resize half_pixel — (target+.5)*source_size/target_size-.5 с replicated borders. Dense inverse_field имеет shape(target_h,target_w,2), float32, absolute source pixel-center(x,y); это не displacement и не normalized coords. NaN/Inf field отклоняются. Проективный inverse horizon(abs denominator<=1e-12) даёт zero/invalid output; valid_region step дополнительно обнуляет pixels вне указанного rectangle.

Probability bilinear вычисляется по четырём соседям без gamma и OpenCV fractional lookup quantization. Labels используют nearest floor(coord+.5), без создания промежуточных классов. Crop/matrix/dense используют zero border; resize replicate. Далёкие coords ограничиваются до integer conversion, чтобы избежать overflow. Tile256/512/1024 используют глобальные coords и дают одинаковый результат. Отмена проверяется между tiles/gathers и при входной проверке.

mask-transform-tests.log:12 целевых тестов прошли. Guided edge refinement с target image, последовательность chain через scratch и runtime mask.transform остаются незавершёнными. Это базовая геометрическая часть, не готовый mask workflow.


### Guided mask refinement без швов

`algorithms/mask_refine.py:refine_tiles(probability,guide_rgb,radius=None,tile_size=512,max_work_pixels=4194304,valid_region=None,check_active)` требует float32 probability HxW и encoded work Rec2020 RGB HxWx3 в одном target canvas. Источник guide — целевое геометрически преобразованное изображение, не исходный unwarped кадр. Radius=max(1,round(8*long_side/1024)); eps1e-4, guide=.2627R+.6780G+.0593B в encoded рабочем пространстве. Model sRGB нельзя подставить без явного преобразования.

Каждая плитка читает halo2r, поскольку guided filter имеет две последовательные box means радиусаr. Используется существующий float64 guided_filter с BORDER_REFLECT_101; crop core копируется в собственный readonly float32 output и clamp[0,1]. Не материализуется полный luma frame. При valid_region всё вне rect повторно обнуляется после фильтра, чтобы уточнение не создало маску вне допустимой области.

Перед вычислением проверяются формы, finite/range всей маски и guide блоками<=16384 pixels. Максимальный read ROI min(h,tile+4r)*min(w,tile+4r) должен помещаться в max_work_pixels; превышение — GUIDED_WORK_BUDGET_EXCEEDED, без молчаливого уменьшения radius. Budget1..16777216 явно назначает caller; default4194304 ограничивает размер ROI, но не является измерением полного RAM worker. Task admission должен учитывать float64 intermediates. Отмена проверяется между validation blocks и до/после native filter; работающий OpenCV вызов внутри не прерывается.

mask-refine-tests.log:11 целевых tests прошли; full-frame reference расхождение<1e-6 для tile256/512/1024. Проверены sharp edge, valid region, strided inputs, budget и cancellation. Подключение всей цепочки к mask.transform, live gateway и реальная photo acceptance остаются в работе.


### Runtime mask.transform

`MaskTransformParams={mask:MaskRef,target:WorkingImageRef,step:TransformStep,dtype:float32|uint16}`. Операция переносит один явный шаг; несколько шагов представляются последовательными task nodes. Входная Canvas маски равна step.from_canvas, целевая WorkingImage.canvas равна step.to_canvas; target TransformChain должен точно совпадать с mask.transform_chain.steps+[step]. Проверяется<=200MP, без незаметной замены geometry chain или повторного шага. Float32 probability требует target guide float32 work Rec2020; uint16 class labels не скачивают RGB guide и не проходят guided refinement.

CPU RAM admission>=1GiB. max_work_pixels=min(4194304,ram_bytes//256) для guided ROI, tile256/512/1024. Dedicated `runtime.arrays.read_inverse_field` допускает только float32 NPY C-layout(target_h,target_w,2), exact EOF и finite coords; обычный image reader по-прежнему запрещает2 channels. Field coordinates могут выходить за canvas; это не probability range.

`prepare_transform` резервирует8*pixels+8192 для probability raw/refined maps либо2*pixels+8192 для labels, плюс png16_bound. Native mappings закрываются до borrowed file yield; exception/cancel/consumer error удаляет файлы. Probability: inverse resampling→guided target-image refinement→PNG16; labels: nearest→PNG16 без изменения класса. На task stage публикуются transformed_mask NPY, transformed_mask_png и mask_transform_result JSON. Manifest содержит params, новый MaskRef, dtype и guided_radius(null для labels). Selector/object_id/confidence/model_lock_id/protected сохраняются, artifact/Canvas/TransformChain заменяются результатом операции.

`mask.face_skin` является явной границей между `face.parse` и pair-effect fitting. CPU-операция принимает только `FaceLabels19`, проверяет его canvas/transform/model binding, выбирает исключительно класс Face19 `skin=1`, тем самым исключая глаза, брови, губы, волосы и остальные признаки, и публикует float32 NPY, PNG16 и `MaskRef(selector=skin, protected=false)`. Confidence наследуется от pinned parse manifest и становится0 для пустой маски. Это устраняет неявное построение skin mask внутри coordinator или fitting-операции и позволяет SHA-связать каждое evidence вычисление.

`pair.fit.beauty` принимает canonical `FaceLandmarks` sets обеих сторон, точный `PairAlignment` и одну связанную с after canvas консервативную skin mask. Before faces переводятся alignment matrix в after-space и сопоставляются one-to-one детерминированным greedy minimum-distance matching с порогом0.2 нормализованной диагонали; UUID используется только как стабильный tie-break. Для evidence source временно warp-ится в after-space без публикации промежуточного изображения. Пустой или несопоставленный face set даёт валидный disabled beauty evidence, а не ошибку контракта. Это соответствует фактическому output `face.landmarks`; отдельные Face68 artifacts pipeline не создаёт.

SQL36 задаёт output policies; живая проверка миграции и полного service chain ещё не выполнена. Graph generation должен предоставить целевое изображение каждого шага; автоматическая компиляция графа пока незавершена. Этот adapter не реализует ML mask inference и не заменяет визуальную оценку edge quality.


### BeautyFilterV1: численное ROI ядро

`algorithms/beauty.py:beauty_filter(rgb,skin,protected,face_width,params,confidence=1,automatic=True,max_work_pixels=4194304,check_active)` принимает encoded work float32 RGB одной полной face ROI с support halo, float32 skin/protected[0,1] и effective Beauty params. Source не мутирует. ROI должна включать лицо и поддержку фильтров: не менее max(2*r+ceil(3*sigma_low),ceil(3*sigma_fine)) вокруг обрабатываемой skin области, кроме действительных границ кадра. Caller отвечает за ROI extraction и ограничение skin до допустимой области; произвольные tiles не могут заменить общую face ROI. Max4MP на вызов — проверяемый working limit, большие ROI требуют отдельного bounded multi-pass adapter, пока не реализованного.

Base guided eps.002,r=max(2,round(.025*face_width)); Gaussian sigma_low=max(1,.04*face_width), sigma_fine=max(.6,.004*face_width), kernels2*ceil(3*sigma)+1, BORDER_REFLECT_101. Low median вычисляется поканально по skin>=.5 и protected<.5 в этой ROI, не по другой персоне. Effective mask=skin*(1-protected). Final=input+mask*(smoothing*(base+texture_keep*fine-input)+tone_evenness*clip(low_median-low,-.03,.03)). Это уточняет независимость tone: tone не умножается на smoothing, и общая style strength не применяется второй раз. Clipping измеряется перед clamp; mask0 pixels копируются точно из source.

Automatic confidence<.75 либо face_width<64 даёт unchanged result с reason; manual mode допускает явно заданную маску. Empty trusted skin и zero strengths также возвращают explicit skip. Texture energy ratio=sum(mask*fine_output²)/sum(mask*fine_input²) по каналам; denominator<=1e-12 даёт null, не бесконечность. Return содержит readonly RGB, applied/reason, ratio и clipping fractions. Native filters не прерываются внутри; callback проверяется между этапами.

beauty-core-tests.log:11 целевых tests прошли. Legacy source/presets SHA неизменны. Это численное ядро, не готовая ретушь: skin-mask construction, fitting, task adapter, большие full-resolution ROI и визуальная приёмка на реальных портретах ещё не реализованы/не завершены.


### Консервативная маска кожи Face68

`algorithms/skin_mask.py:build_skin_mask(face,roi,skin_probability,manual=None,permanent=None,manual_mode=False,max_work_pixels=4194304,check_active)` использует Face68 normalized source-edge coordinates: local pixel-center=(x*width-.5-roi.x,y*height-.5-roi.y). Все probability arrays должны уже иметь точную ROI; проверка artifact Canvas/TransformChain — обязанность будущего adapter. До morphology отклоняются вырожденные landmarks (нулевая ширина/высота или aspect>4), неполная ROI, auto confidence<.75 и face width<64. Confidence=min68 landmark confidence. Manual mode не отключает geometric/feature protection.

Hull jaw0..16+brows17..26 ограничивает лицо без выдуманной области лба. Из hull исключаются convex hulls eyebrows17..21/22..26, nose27..35, eyes36..41/42..47, mouth48..67; dilation ellipse radius=max(1,round(.01*face_width)). Support feather только внутрь разрешённой области через precise L2 distance/max(1,.01*face_width), clamp1. Auto mode требует semantic skin_probability; manual mode требует explicit manual mask. Skin умножается на semantic probability и optional manual restriction. Manual не расширяет hull и не закрашивает защищённые черты.

Protected=max(feature_binary,permanent). Skin при protected1 обнуляется точно; partial permanent остаётся отдельным каналом и применяется один раз в Beauty/MaskComposer, без квадрата(1-protection). Выход readonly SkinMaskResult(skin,protected,face_width,confidence,reason); empty/low-confidence/incomplete/degenerate cases explicit skip. Limit4MP относится к ROI, не к full-frame workflow.

skin-mask-tests.log:12 целевых tests прошли на синтетических Face68. Парсер/детектор, преобразование semantic parsing в skin_probability, typed task adapter и проверка качества на реальных портретах ещё не реализованы/не завершены. Старый MediaPipe-based pipeline не изменялся.


### Расчёт полной face ROI

`algorithms/beauty_roi.py:filter_support(face_width)` — единый источник radius/sigma/halo для BeautyFilter и planner. Halo=max(2*r+ceil(3*sigma_low),ceil(3*sigma_fine)); два box passes guided и последующий low Gaussian требуют суммарной поддержки. `plan_roi(Face68,max_work_pixels=4194304)->BeautyROI(rect,face_width,confidence,support)` переводит normalized edge coords в pixel-center coords, строит bbox всех68 points и расширяет на halo+1 для округления hull. Обрезается только по действительной Canvas. Превышение квоты даёт BEAUTY_ROI_BUDGET_EXCEEDED; радиус/разрешение не уменьшается автоматически.

Проверяются chain hash/target, degenerate landmarks и aspect>4. Face width берётся из jaw0..16, confidence=min68. Результат задаёт один полный face ROI; skin/parsing/permanent arrays извлекаются тем же rect. BeautyFilter использует ту же функцию support, исключая расхождение между planner и исполнением.

beauty-roi-tests.log:10 целевых tests прошли. На синтетической фотографии900x800 локальная skin mask совпадает с full-frame crop, ретушь отличается<2e-6. Это не приёмка на реальных лицах. Большие лица, для которых полная support ROI>4MP, требуют отдельного multi-pass execution; task adapter ещё не подключён.


### Композиция поправок нескольких лиц

`algorithms/face_composite.py:composite_faces(source,corrections,tile_size,check_active)` принимает immutable float32 working RGB и<=64 FaceCorrection(object_id,roi,delta,weight). Каждый delta=result-base уже включает effective skin mask; weight — та же маска, включая protection ровно один раз. Producer обязан вычислять каждый patch от одного source и применять общую union защитных областей всех лиц. Sequential retouch предыдущего результата запрещён. Source/patch arrays могут быть scratch memmaps, не full-frame RAM copies.

Порядок накопления фиксирован lexical object_id.hex; duplicate UUID запрещены. По tile256/512/1024 float64 sum_delta и sum_weight, затем result=clip(source+sum_delta/max(1,sum_weight),0,1). Weight повторно на delta не умножается. Нулевая weight сохраняет source точно. Validations: float32 shapes, ROI inside frame, finite weight[0,1],abs(delta)<=weight+1e-6, delta0 при weight0; blocks<=16384. Output readonly, caller пишет в own scratch; неполный output после отмены не публикуется.

face-composite-tests.log:13 целевых tests прошли. Проверены overlap normalization, soft weight once, permutation/tile invariance и leakage rejection. Глобальная protection union, подготовка patch artifacts и portrait.beauty runtime ещё не подключены; этот kernel не объявляет весь portrait workflow готовым.


### Scratch-backed подготовка многолицевого кадра

`operations/portrait/prepare.py:prepare_beauty(rgb,image,faces,params,scratch,permanent=None,tile=512,max_roi_pixels=4194304,check_active)` — context manager. FaceInput содержит Face68 и borrowed full-canvas semantic/manual arrays. Проверяются Canvas/TransformChain, shapes, уникальные object_id и<=64 faces. Artifact provenance и selectors проверяет будущий adapter. Params уже effective; повторного scale strength нет.

Первый проход строит маски каждой full-support ROI и объединяет protection через max с full-frame permanent в отдельном float32 scratch map. Второй проход пересчитывает skin с общей protection, запускает beauty_filter на исходном RGB и сохраняет delta/weight каждого применённого лица в отдельные NPY maps. Все лица читают один immutable source. Финальная композиция идёт tiles256/512/1024. Сортировка object_id стабилизирует evidence и accumulation. Empty faces даёт точную копию source.

Scratch reservations:12*pixels+8192 output;4*pixels+8192 global protection; каждый применённый face ROI —12*roi_pixels+8192 delta и4*roi_pixels+8192 weight. Source/materialized inputs резервируются отдельно caller. Full-frame permanent проверяется tiles256, не целой длинной строкой. Все intermediate mappings/reservations закрываются и удаляются до yield BeautyFiles(image,reservation,faces). Output существует только внутри context; consumer failure/отмена удаляет его. Нет автоматической публикации файла.

beauty-prepare-tests.log:16 targeted tests прошли, включая cross-face protection, permutation, empty identity, cleanup и отмену после создания face delta. Full portrait task adapter, большие ROI>4MP, реальные detector/parser и визуальная приёмка остаются в работе.


### Runtime portrait.beauty

`BeautyParams={image:WorkingImageRef,resolved:ResolvedStyleRef,faces:[{landmarks:Face68Ref|FaceLandmarksRef,object_id?:UUID,skin?:MaskRef,manual?:MaskRef,manual_mode:false}],permanent?:MaskRef}`. Отдельный `Face68Ref` сохраняет совместимость с уже созданными задачами; Apply использует один immutable `FaceLandmarksRef` и обязательный `object_id`, который должен однозначно выбрать лицо из набора. Максимум64 faces; duplicate `(landmarks,object_id)` запрещены. Auto требует selector skin, manual mode требует selector manual; face masks protected=false, global permanent protected=true. Canvas/TransformChain всех входов должны совпадать с рабочим изображением, object_id маски при наличии — с выбранным Face68. Full-frame source SHA/ICC сверяются с источником resolved style. Alpha передаётся неизменной declared input ссылкой.

CPU admission RAM>=1GiB,tile256/512/1024. Effective Beauty получается через resolve_effective_params один раз. Auto face confidence=min landmarks и заданных semantic/manual masks; manual mode явно отключает auto confidence gate, сохраняя feature/geometric guards. Max ROI=min(4194304,ram_bytes//256). Превышение до pixel downloads даёт nonretryable RESOURCE_OOM: это текущий явный предел, большие ROI требуют незавершённого multi-pass пути. Не объявлять такой случай успешной ретушью или автоматически менять качество.

Операция материализует readonly inputs, вызывает prepare_beauty, публикует working_rgb,working_image,beauty_report. Disabled module публикует image manifest с исходным pixel reference и report без скачивания/перезаписи pixels; отсутствие uploaded working_rgb допустимо в этой ветви. Report содержит requested/effective params, per-face applied/reason/texture_energy_ratio/clipping, warnings module_disabled/no_faces/face_skipped. No faces либо skipped face (кроме zero_strength) требует review. Итоговая Face68/skin модельная квалификация не выводится из confidence1 ручной маски.

SQL37 задаёт output policies. CPU operation установлена, но SQL37/live gateway pipeline, реальные detector/parser, большие ROI, beauty fitting и визуальная приёмка пока не завершены. Наличие operation не означает готовый полный portrait workflow.


### Float32 Telea: измеренная численная поправка

`algorithms/defect_repair.py:repair_accepted(rgb,accepted,protected,diameter,strength=1,max_work_pixels=4194304,check_active)` принимает одну ROI float32 working RGB, boolean accepted mask и float32 protection. Allowed=accepted & (protected==0): любая ненулевая защита исключает ремонт. Недостаточный source context (allowed касается границы ROI) отклоняется, а не заполняется скрытым padding. Task adapter обязан до вызова сверить source/mask hash и сохранённые accepted_candidate_ids; эта привязка и proposal stage ещё не реализованы.

OpenCV4.14.0 Telea на constant.4 normalized float32 дал max error1.4965807 в отверстии5x5,r2. Поэтому canonical numerical path — cv2.inpaint(float32(channel)*65535,mask,radius,INPAINT_TELEA)/65535 по трём каналам. Квантования и промежуточного uint8 нет. Тот же тест после масштабирования дал max error.0000213981. Измерение сохранено в work/evidence/telea-float32.json. Radius=clamp(.25*diameter,1,5).

Feather только внутри allowed: alpha=min(distanceTransform(allowed)/max(1,radius),1)*strength. Repaired gamut clipping измеряется на allowed до clamp; output=source+(clip(repaired)-source)*alpha, затем pixels вне allowed копируются точно. Strength0/empty allowed дают точную копию и applied_pixels0. Return readonly RGB, applied pixel count, radius, clipping fractions. Source/masks не мутируют, callback между каналами и этапами. Native Telea нельзя прервать внутри одного вызова.

10 целевых tests прошли; constant preservation<2/65535, локальное улучшение дефекта, protected/outside exact, context rejection, cancellation. Портретный корпус, approval binding, многокомпонентная оркестрация и defects.repair adapter остаются в работе. Старый pipeline сохранён по SHA.


### Привязка подтверждений дефектов к immutable artifacts

`contracts/defects.py:DefectCandidates` содержит image/source_pixels/label_map ArtifactRef, Canvas/TransformChain,<=4096 candidates. Label uint16 1..65535;0 — background. Candidate={id,label,bbox,diameter,score,decision:pending}. ID=SHA256(JCS{version:defect_candidate_v1,source_sha256:source_pixels.sha256,mask_sha256:label_map.sha256,label,bbox,diameter}); bbox canonical JSON, не native object. Unique labels/IDs, bbox bounds и chain hash проверяются. Score не является автоматически калиброванной вероятностью.

`DefectReview={proposal,image,label_map,revision>=1,accepted_candidate_ids,rejected_candidate_ids}`. Дубликаты и пересечение accept/reject запрещены. `accepted_candidates(proposal_ref,proposal,review,image_ref,image)` проверяет exact artifact refs всех версий, source pixels, Canvas/TransformChain, candidate hashes и известность всех решений; возвращает только явно accepted, сортируя по label. Omitted candidates остаются pending. Смена pixel version, label map, proposal reference или image reference не переносит approval.

Это проверка content binding, не доказательство полномочий пользователя. Будущий backend review API обязан проверять tenant/RBAC, сохранять immutable approved revision и формировать доверенный artifact. Worker не должен принимать review JSON из произвольного UI запроса. Физическая label map ещё должна проверяться adapter по SHA и соответствию bbox/label; contract-only tests не заменяют это.

Defect-review-tests.log:6 целевых проверок прошли вместе с repair kernel; schemas/legacy SHA согласованы. Review persistence/API, proposal inference и defects.repair adapter пока незавершены.


### Проверка физической label map дефектов

`algorithms/defect_labels.py:validate_labels(labels,DefectCandidates,check_active)->tuple[LabelEvidence]` вызывается после SHA-verified materialization и exact NPY layout reader. Требует uint16 HxW по proposal Canvas. Label0 — background; любой другой label обязан присутствовать среди candidates. Для каждого candidate должны существовать pixels, а минимальный bounding rectangle фактических pixels должен точно равняться сохранённому bbox. Неизвестные/missing labels или изменённые границы отклоняются до ремонта. Возвращаются label/pixel count в порядке label.

Сканирование блоками<=16384 pixels; contiguous map через flat view, strided через строки. Lookup65536 int32, counts/min/max только на<=4096 candidates; нет полного списка координат и отдельной бинарной200MP mask на каждый candidate. Callback проверяется между блоками и кандидатами. Проверка не оценивает качество detector и сама по себе не доказывает связность компоненты; proposal generator остаётся обязан вычислять connected components.

Defect-label-tests.log:9 целевых tests прошли вместе с review/repair. Live review authorization, proposal generation и runtime repair ещё не завершены.


### Подготовка результата подтверждённого ремонта

`operations/defects/prepare.py:prepare_repair(rgb,labels,protected,proposal,selected,scratch,strength,max_roi_pixels,check_active)` — context manager. Selected обязан поступать из accepted_candidates после доверенной review авторизации. Helper повторно сверяет точное содержимое selected с proposal, запрещает duplicate IDs и валидирует физическую label map до записи. Это внутренний вычислительный API, не endpoint принятия пользовательского решения.

ROI кандидата=bbox+halo max(16,ceil(4*radius)+1), обрезка только по настоящему кадру. ПлощадьROI>budget(<=4MP) — явная ошибка. Каждый patch читает immutable original RGB. Выход заранее резервируется12*pixels+8192 NPY и копируется tiles256 с bounded finite/range validation. Результат Telea записывается только в pixels соответствующего accepted label; перекрывающиеся support windows не перетирают соседние результаты. Protected pixels сохраняются kernel. Неподтверждённые labels не меняются.

Return RepairFiles содержит borrowed image/reservation и ordered per-candidate evidence(id,label,applied_pixels,reason,radius,clipping). Protected/zero_strength отмечаются явно; недостаточный context у края даёт unchanged candidate с insufficient_context, который будущий adapter обязан направить на review. Mapping закрывается до yield; exception/cancel/consumer error удаляет output. Никакая публикация или подтверждение пользователя этим helper не выполняется.

repair-prepare-tests.log:13 целевых tests прошли, включая selected-only mutation, protection, order invariance, edge-context skip и cleanup. Runtime repair adapter, trusted review API и proposal generation ещё не завершены.


### Runtime defects.repair

`DefectRepairParams={image,proposal,review,resolved:ArtifactRef,protected:MaskRef}`. Protected обязателен, protected=true, Canvas/TransformChain совпадают с WorkingImage. Сначала загружаются строгие metadata contracts, accepted_candidates сверяет immutable refs и source version; stale review/source и unknown IDs дают отдельные error codes до pixel IO. Проверяются рабочий float32 Rec2020, resolved origin/ICC и declared alpha. Effective defects strength вычисляется один раз через resolve_effective_params.

CPU admission RAM>=1GiB; ROI limit=min(4194304,ram_bytes//256). Input readers проверяют exact NPY dtype/shape/SHA: RGB float32[0,1],label uint16,protection float32[0,1]. prepare_repair выполняет физическую label/bbox проверку и approved-only changes. ROI budget failure — явный nonretryable RESOURCE_OOM, не автоматическое снижение качества. Insufficient context/protected skip требуют review; zero strength — безопасный identity.

Публикуются working_rgb,working_image,defect_repair_report. Disabled module после проверки metadata публикует исходный image manifest и report без pixel download/upload. Report хранит params/review_revision, requested/effective strength, per-candidate applied_pixels/reason/radius/clipping и warnings. No accepted candidates даёт unchanged copy с явным warning; omission не превращается в approval. Alpha неизменна.

SQL38 задаёт output policies. Установленная операция исполняет только explicit DefectReview; conservative_auto approval generation пока не реализован и не подменяется этим путём. Trusted backend обязан выпускать review artifact после RBAC/decision transaction и pin revision в run snapshot; worker content binding не доказывает сам факт human authorization. Review API/persistence, proposal generation, SQL38/live integration и реальные фото пока не завершены/не проверены.


### defect_dog_v1: численное предложение кандидатов

`algorithms/defect_propose.py:propose_defects(rgb,skin,protected,face_width,confidence,diameter_fraction=(.002,.012),max_work_pixels=4194304,check_active)` работает на полной face ROI encoded float32 work RGB. Auto gate confidence>=.75,face width>=64; eligible=skin>=.75 & protected==0. Все input finite[0,1], exact float32 shapes. Невалидные данные отклоняются, empty/subpixel/low-confidence/small-face случаи имеют reason.

Пять geometric scales diameter от max(1,lo*face_width) до hi*face_width; sigma=max(.5,diameter/(2*sqrt2)). Gaussian support ceil3sigma, BORDER_REFLECT_101. Dark response=G1.6sigma(luma)-Gsigma(luma), luma work Rec2020. Red response=Gsigma(R-(G+B)/2)-G1.6sigma той же redness. Response — max0 по обоим признакам и всем scales. Порог=max(.015,median(response_eligible)+4*1.4826*MAD).

Первый connectedComponents8 выделяет blob по порогу; mask core внутри каждой компоненты=response>=max(threshold,.5*component_peak). Повторная connectedComponents8 делает candidate labels связными после narrowing. Это отделяет поддерживающий DoG-ореол от области ремонта: без core тестовый дефект3x3 разрастался до21 pixels и ошибочно выходил за diameter limit. Equivalent diameter=sqrt(4*area/pi), compactness=area/bbox_area>=.35; допускаются только configured diameter range и компоненты вне границы ROI. Сортировка bbox(y,x), затем label; выходuint16,background0. Caps65535 initial components/4096 candidates дают явную ошибку, не молчаливое усечение.

Score=clamp(1-threshold/peak,0,1) — эвристика, не калиброванная вероятность. Return содержит readonly labels, local bbox/diameter/score, threshold и algorithm. Источник/глобальные координаты/immutable candidate IDs привязывает будущий proposal adapter после staging label map. Все результаты pending; ни один score не разрешает автоматический ремонт. Пороги и scale definition зафиксированы как версия, но требуют квалификации на реальных фото.

Defect-propose-tests.log:19 целевых tests прошли. Runtime defects.propose, многолицевая сборка labels, real-photo precision/recall и conservative_auto approval ещё не реализованы/не завершены.


### Многолицевая подготовка предложения дефектов

`operations/defects/propose_prepare.py:prepare_proposals(rgb,image,faces,scratch,permanent,diameter_fraction,max_roi_pixels,check_active)` создаёт borrowed ProposalFiles(labels,protection,png,reservation,candidates,faces). Используется проверенный FaceInput и full-support ROI planner;<=64 faces, unique object IDs, Canvas/chain совпадают с image. Первый проход объединяет feature/permanent protection всех лиц. Второй строит skin и DoG candidates, переводя local bbox в global coords и назначая единые labels1..4096.

Face order=object_id.hex; candidate order из detector. Если pixels нового candidate пересекают уже принятый label, весь новый candidate пропускается, без обрезки/разрыва mask. Overlapping_candidates записывается per-face; будущий adapter должен выставить review evidence. Это детерминированное устранение перекрытий, не утверждение, что оба предложения семантически идентичны. Не применяются повторные ремонта или automatic approval.

Scratch reservation=6*pixels+16384+png16_bound: labels uint16,protection float32,PNG16. Full-frame source/permanent проверяютсяtiles256, intermediates каждой face ROI освобождаются до следующей. Все mmap закрываются до yield; context exit/cancel удаляет файлы. PNG16 кодирует class labels точно без probability quantization. Manifest IDs можно вычислить только после staging label artifact SHA.

propose-prepare-tests.log:11 targeted tests прошли, включая global bbox, overlap reporting, deterministic permutation, protected label0, exact PNG16 и cleanup. Runtime defects.propose и real-photo calibration ещё не завершены.


### CPU runtime defects.propose

`operations/defects/propose.py:execute(ctx,artifacts)` установлен в CPU factory. Вход `DefectProposeParams`: image, до64 Face68 bindings (landmarks, skin/manual, manual_mode), optional permanent protected MaskRef и diameter_face_fraction(default[.002,.012]). Proposal не меняет изображение и не применяет style strength. Будущий compiler обязан передать настроенный диапазон из модуля defects. Минимальный admission RAM1GiB; max_roi_pixels=min(4194304,ram_bytes//256). Превышение ROI даёт RESOURCE_OOM без уменьшения радиусов/качества.

Canvas, TransformChain и object_id каждой маски проверяются перед пиксельным IO. Эффективная confidence лица — минимум landmarks и участвующих масок. Input maps readonly, float32 finite/unit interval. Подготовка публикует uint16 NPY labels, float32 NPY protection и PNG16 labels. После получения неизменяемого ArtifactRef label map формируются candidate IDs, затем DefectCandidates и DefectProposeReport. Все candidates pending; review_required при кандидатах или предупреждениях. Отчёт содержит protection MaskRef, PNG ref, per-face threshold/reason/count/overlap и warnings. Content binding можно проверить существующим accepted_candidates; это не авторизация человеческого решения.

Миграция39 добавляет пять output policies. Проверка адаптера использует контролируемые artifact ports: физическая согласованность label map/минимальных bbox, точный PNG16, отсутствие labels в protection, SHA-bound review и отклонение чужого mask object до публикации. Live SQL39/полный broker→gateway цикл, backend review API, detector/parser и квалификация DoG на реальных фото остаются незавершёнными. Результат не означает готовность всей системы.


### CPU runtime background.compose

`contracts/background.py:ComposeParams` связывает WorkingImage и подтверждённую subject/person MaskRef на точных Canvas/TransformChain. Поддержаны режимы transparent, solid и asset; solid_rgb/background ArtifactRef взаимоисключающие и обязательны для своего режима. Asset обязан быть opaque float32 в том же рабочем ICC. Параметры cover layout: asset_scale[1,10],position_x/y[0,1]. Contact shadow имеет opacity<=.3, blur_fraction<=.02 и offsets±.05 canvas. Mask с protected=true, selector другого класса, confidence<.75, несовпадающей геометрией или не объявленным artifact отклоняется до pixel materialization. Неуверенная сегментация не применяется как будто подтверждённая.

`algorithms/background.py` объединяет subject mask с существующей uint16 alpha умножением coverage. Transparent сохраняет straight RGB при ненулевом coverage, канонически обнуляет RGB при alpha=0 и публикует новую uint16 alpha. Solid/asset декодирует work gamma2.4 в linear, выполняет premultiplied composition `foreground*alpha + background*(1-alpha)` и возвращает в gamma2.4; выход становится opaque и не несёт alpha. Asset использует deterministic cover factor, explicit scale/position и bilinear remap по pixel centers; геометрия обязана покрывать каждый output pixel. Contact shadow строится из offset+Gaussian subject alpha, обнуляется внутри protected subject и затемняет только linear background. Работа выполняется tiles256/512/1024, source maps readonly, scratch заранее резервируется, отменённый output не публикуется.

Adapter публикует `working_rgb`, optional `working_alpha`, `working_image`, `background_report`; report фиксирует режим, confidence, факт объединения source alpha, asset scale, contact shadow и linear-light path. Confidence [.75,.9) требует review. Миграция00102 задаёт output policies, CPU worker factory регистрирует `background.compose`, JSON Schema генерируется из исполняемых контрактов. Десять compose-тестов проверяют solid/transparent/asset, coverage sampling, shadow protection, alpha и fail-before-upload; вместе с blur — 15 background tests. Общая platform/migration регрессия317 passed. Sky и generated background остаются отдельными операциями и этим срезом не подменяются; live SQL/broker/gateway и визуальная edge/matting/shadow оценка не выполнялись.


### CPU runtime background.blur

`BlurParams={image,subject,depth,strength,tile}` требует подтверждённый subject/person MaskRef и полный RelativeDepth без invalid_mask на тех же Canvas/TransformChain. Confidence<.75, empty subject, incomplete depth, undeclared artifact и не-work float32 image отклоняются до публикации. Existing alpha остаётся неизменяемой ссылкой и обязана быть объявлена task input.

Алгоритм определяет subject depth как детерминированное float64 среднее depth по mask>=.5 и строит восемь depth layers. Максимальный художественный радиус `ceil(.02*long_side*strength)`, радиус слоя пропорционален расстоянию его центра от subject depth. Каждый output tile читает halo максимального радиуса. RGB декодируется gamma2.4→linear; hidden-edge background восстанавливается normalized Gaussian convolution `blur(background*rgb)/blur(background)`, поэтому foreground color не участвует в заполнении. При нулевом background support алгоритм явно сохраняет source вместо деления на малую величину. Затем исходный foreground накладывается по soft subject alpha, результат возвращается в gamma2.4.

Публикуются working_rgb,working_image,blur_report; report фиксирует 8 layers, subject depth, maximum radius и linear-light path. Миграция00103 задаёт policies, CPU factory регистрирует operation, BlurParams/BlurReport имеют сгенерированные schemas. Пять blur-тестов и 12 совокупных background-тестов проверяют радиусы, защиту foreground, отсутствие red spill, output binding, ошибки и cancel cleanup. Общая platform/migration регрессия314 passed. Это artistic depth blur, не имитация физического f-number; качество depth и границ требует visual real-photo gate.


### CPU runtime background.sky

`SkyParams={image,sky_mask,protected,sky,match_limit,asset_scale,position_x,position_y,tile}` требует sky selector, отдельную protected=true mask для проводов/зданий и opaque approved sky WorkingImage в том же рабочем ICC. Обе mask должны иметь confidence>=.75 и точные Canvas/TransformChain source image. Asset использует тот же deterministic cover/pixel-center layout, что compose; все metadata и pixel artifacts должны быть declared task inputs.

Первый tiled pass вычисляет float64 weighted means исходного sky и asset в linear gamma2.4 decode только внутри `sky_mask*(1-protected)`. Поканальные gains=`source_mean/asset_mean` ограничиваются `[1-match_limit,1+match_limit]`, где match_limit<=.25. Второй pass применяет gain к linear asset и смешивает по soft effective mask. Pixels с effective mask=0 выбираются напрямую из source после encode, поэтому protected wires/buildings и область вне sky сохраняются побитно. Empty effective sky — явный MASK_EMPTY; никакой full-frame RAM copy не создаётся.

Публикуются working_rgb,working_image,sky_report; alpha source сохраняется. Report связывает все input refs, changed pixel count, match limit и фактические gains. Миграция00104 задаёт policies, CPU factory регистрирует operation, схемы генерируются из SkyParams/SkyReport. Четыре sky-теста и 19 совокупных background-тестов проверяют bounded match, exact protection, fail-before-upload и cancellation. Общая platform/migration регрессия321 passed. SegFormer inference и ручное подтверждение маски принадлежат mask/review стадиям; визуальная реалистичность sky asset и edge quality этим тестом не доказаны.


### CPU runtime hdr.window_pull

`WindowParams={image,dark,mask,reference_exposure_seconds,dark_exposure_seconds,confirmed:true,tile}` принимает только window/manual MaskRef с confidence>=.9. Dark обязан быть заранее зарегистрирован в точные Canvas/TransformChain reference, иметь тот же work ICC/dtype/alpha. Exposure положительные; dark должен быть на1–6 stops короче reference, ratio[1,64]. Все metadata и pixel refs объявляются task inputs. Этим операция не выдаёт незарегистрированные bracket frames за aligned.

Каждый tile декодируется gamma2.4→linear. Dark приводится к reference exposure умножением на `reference_time/dark_time`, затем смешивается с reference только по soft confirmed mask. Pixels с mask=0 выбираются напрямую из source, избегая gamma round-trip. Pixel внутри маски, где все три канала reference и exposure-matched dark >=.999, считается unrecoverable; fraction и warning `all_exposures_clipped` записываются в report и требуют review. Empty mask отклоняется до output reservation/upload.

Публикуются working_rgb,working_image,window_report; source alpha сохраняется. Миграция00105 задаёт policies, CPU factory регистрирует `hdr.window_pull`, WindowParams/WindowReport имеют generated schemas. Пять тестов проверяют exposure equivalence, exact outside-mask preservation, clipping review, fail-before-upload и cancel cleanup. Общая platform/migration регрессия326 passed. Регистрация brackets, deghost/parallax и Debevec/Mertens merge остаются отдельными незавершёнными HDR-операциями; реальная bracket/visual квалификация не выполнялась.


### CPU runtime hdr.register

`HDRRegisterParams` содержит 3–9 уникальных `{image,exposure_seconds}`, reference_index, общий wb_recipe и uint32 seed. Все кадры обязаны быть opaque float32 в одном work ICC; dimensions могут различаться, alpha запрещена. Pixel artifacts объявляются task inputs. Reference не регистрируется сам на себя: получает identity, overlap1, no invented reprojection/inliers. Остальные frames последовательно materialize по одному рядом с reference, чтобы RAM не росла на весь bracket.

Registration использует существующий SIFT4000→ratio.75→similarity RANSAC(seed+frame index)→gradient ECC kernel на preview long-edge1024. Full-resolution transform проецируется обратно в истинную similarity, overlap>=.9, median reprojection<=1px preview и inliers>=12 проверяются до публикации. Exposure-normalized motion evidence декодирует gamma2.4, приводит source к reference exposure, warps preview и считает долю valid pixels с abs linear Rec.2020 Y residual>.05. Motion fraction>.01 требует review. ECC warnings также сохраняются и требуют review; отсутствие background mask не переносится как ложный HDR warning.

Публикуется один `BracketTransforms` с исходным ordered bracket, reference Canvas/WB, матрицей/evidence/motion каждого frame, residual threshold .05 и review fraction .01. Миграция00106 задаёт output policy; CPU factory регистрирует operation; HDRRegisterParams/BracketTransforms имеют generated schemas. Четыре целевых теста проверяют exposure normalization, motion, порядок/reference, invalid inputs и cancellation. Общая platform/migration регрессия330 passed. Это SIFT/ECC registration; отдельный MTB fallback, deghost reference synthesis и parallax review beyond similarity остаются для merge/расширения, real-bracket visual gate не выполнен.


### CPU runtime sensor.dust

Apply может формировать такие группы только из сохранённого ingest `sensor_alignment`; детерминированный compiler сортирует mapping и ordinal, принимает группы от5 кадров и разбивает серии >64 сбалансированно без короткого хвоста; совпасть обязаны hashed physical-camera ID, sensor dimensions, raw crop и orientation1. Metadata raster-файла или RAW без body serial не считается доказательством общей физической матрицы.

`DustParams` требует 5–64 уникальных WorkingImage одного opaque float32 work ICC, одинаковых oriented dimensions, camera_id, physical sensor dimensions и явный raw_crop внутри sensor с orientation1. Эти поля являются заявленным sensor mapping; crop JPEG без такого подтверждённого mapping не должен попадать в операцию. Frames обрабатываются последовательно, а не удерживаются одновременно. Recurrence policy находится в диапазоне [.8,1].

`detect_frame_dust` вычисляет linear Rec.2020 luma, low-frequency Gaussian sigma4, dark residual `smooth-luma`, low-gradient gate на broad field<=.05 и robust threshold `max(.01,median+4*1.4826*MAD)`. Connected components допускаются только compactness>=.35, area3..512, radius .75..8 image px, не касаются границы. Центр/radius переводятся через raw_crop в sensor pixels. Кандидаты разных кадров объединяются детерминированно в radius2 sensor px, не более одного observation/frame; публикуются только observed>=ceil(recurrence*eligible frames). ID — JCS hash algorithm/camera/sensor/position/radius и ordered frame SHA.

`DustCandidates` всегда содержит decision pending и не превращает score в auto approval. При отсутствии повторяемых компонентов публикуется `no_repeated_dust` без review. Серверный `sensor_dust_review_v1` повторно проверяет JCS/SHA, ordered frame hashes, physical sensor/crop, recurrence и каждый content-derived candidate ID; решение хранит отсортированные непересекающиеся accepted/rejected ID. Миграция00192 создаёт только для успешного canonical `sensor.dust` идемпотентный `sensor_dust` review и регистрирует отдельную CPU-операцию `sensor.dust_repair`.

`sensor.dust_repair` принимает один frame, исходный proposal и авторизованный review, встроенный scheduler в immutable task params. Она проверяет принадлежность frame группе и accepted ID proposal, переводит sensor coordinates через exact raw crop в oriented pixels и применяет bounded OpenCV Telea к объединённой маске только выбранных кандидатов. Исходный alpha запрещён тем же opaque group contract; пустой выбор или кадр вне пригодной группы даёт неизменённый WorkingImage и явный warning. Публикуются `working_rgb` только при фактическом ремонте, `working_image` и bounded `dust_repair_report`; все контракты имеют generated JSON Schema. Семь sensor tests проверяют detection/mapping, recurrence, one-off rejection, binding review, фактическое осветление dust patch, output provenance, invalid inputs и cancellation. Реальные multi-camera false-positive rate и визуальное подтверждение ремонта остаются обязательным hardware/visual gate.


### CPU runtime hdr.merge

`HDRMergeParams={registration,mode:debevec|mertens,tile}` принимает только строгий BracketTransforms. Все 3–9 pinned WorkingImage/pixels должны быть task inputs, opaque float32 и одного work ICC. Reference Canvas/TransformChain становятся выходными. Full-frame arrays остаются readonly memmap; alignment выполняется существующим linear-bilinear global pixel-center warper по tiles.

Debevec path сначала выравнивает preview common overlap long-edge<=512 и калибрует deterministic OpenCV Debevec response(samples70,lambda10,random=false) по exposure times. Для каждого output tile triangular exposure weights применяются к `response[z]/time`; invalid samples исключаются. Exposure-normalized linear-Y residual>.05 помечает motion, и radiance такого pixel принудительно берётся из reference. Если все valid exposures находятся у code endpoints<=1/>=254, pixel учитывается в unrecoverable fraction. Float32 radiance пишется в отдельный scratch memmap. Глобальный log-average вычисляется первым pass, второй pass применяет фиксированный global Reinhard intensity0/light_adaptation1/color_adaptation0/gamma1 и возвращает work gamma2.4.

Mertens path использует contrast1/saturation1/exposure0. Перед fusion каждый invalid/moving sample заменяется reference; report всегда содержит `non_radiometric_mertens`, `radiometric=false`, `response_calibrated=false`, поэтому fusion не выдаётся за radiance HDR. Оба режима публикуют working_rgb,working_image,hdr_report с deghost/unrecoverable fractions и registration warnings. Radiance scratch не публикуется и удаляется после staging.

Миграция00108 задаёт policies, CPU factory регистрирует operation, HDRMergeParams/HDRReport имеют generated schemas. Четыре теста проверяют response-based tile merge, reference deghost, Reinhard finite/range, оба provenance режима, missing input и cancel cleanup. Общая platform/migration регрессия339 passed. Реальные bracket color/ghosting/tonemap visual gates и high-resolution performance не выполнены.
