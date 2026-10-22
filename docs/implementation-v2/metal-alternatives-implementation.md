# Metal: план совместимых альтернатив

### M2 phased conservative repair: source addition, 2026-10-17

Region uses an additive color-renderer phase contract. Default `complete`
retains global → series → local → regions → one strength blend. `global` writes
only the global intermediate into private scratch. `local` consumes that exact
intermediate (possibly repaired by sensor dust) and blends once against the
original aligned base. Effective color is resolved once. Strength must never
be applied before dust or use repaired RGB as the original aligned base.

`sensor.dust_auto_repair` runs between global and local;
`defects.auto_repair` runs after portrait detail and before background. Both
reuse `prepare_conservative_repair`, including fixed thresholds, permanent-mark
protection, declared source/proposal/protection pins and exact current RGB/alpha
checks. Canvas equality alone cannot authorize repair. Review policies remain
unavailable until their human-decision authority is connected; they are never
translated into auto policy. Native order rejects unknown/reordered/duplicate
accepted image stages. The independent adopter repeats typed canonical
proposal/report bindings and physically verifies protection and uint16 labels.
Resource profiles charge retained intermediates and conservative engine buffers.

These changes and authored scenarios have not been executed. M2 remains open
for advanced providers and whole SQL lifecycle/concurrency scenarios. No
capability is activated by this addition.

Native inpainting now has a production startup mount for installed, approved,
passed `big_lama` on the exact selected backend/model lock. Its task-private
adapter consumes the canonical confirmed Params/masks from the accepted stage
trace, verifies exact predecessor RGB/alpha, uses the existing
`compute_inpaint_region`/candidate blend/preservation helpers, and preserves every
positive protected pixel. Uninstalled or unqualified model factories remain
unavailable. The independently scoped adopter repeats Params/mask geometry and
physical input validation. Authored CUDA/MPS numerical parity, fractional
protection, cleanup and stale-stage scenarios have not been executed. Generation
and outpainting are still separate open provider work; this addition does not
assert whole M2 readiness.

Дата статического инвентаря: 2026-10-17. Этот документ дополняет основной MVP-план и не увеличивает счётчик закрытых пунктов. Тесты, сборки, миграции и квалификация при подготовке не выполнялись.

## Статусы и обязательные правила

- `implemented/unqualified`: в исходниках существует backend-aware путь, но текущий Mac/runtime/model lock не проверен этим аудитом. Это не утверждение об установленной или квалифицированной функции.
- `planned`: требуются изменения кода, контрактов или admission; функцию нельзя объявлять работающей.
- `unavailable`: отсутствует установленный совместимый provider; UI получает реальную доступность, а не обещание будущего порта.

Приоритет выбора: установленный квалифицированный MPS adapter → явный квалифицированный CPU этап/worker → реализованный и квалифицированный Core ML adapter. Нельзя незаметно запускать CPU внутри MPS registration. Переключение backend создаёт новые execution profile, runtime/model/device pins; старый результат и retry сохраняют исходные pins. Metal использует измеряемый бюджет без фиксированных 10 ГБ, CUDA MVP — не более 10 000 000 000 байт. Отсутствие одного provider не отключает независимые операции.

## Текущая матрица

| Возможность | Код сейчас | Статус Mac в этом аудите | Решение |
|---|---|---|---|
| Общий render, denoise/upscale | render_task, render_resources, render_profiled_replay, render_denoise, render_upscale принимают cpu/cuda/mps | implemented/unqualified | Использовать существующий MPS путь с собственными pins |
| Маски/лица/landmarks/pose/scene/depth | MODEL_BACKENDS разрешает MPS для соответствующих PyTorch моделей | implemented/unqualified | Проверить установку adapter и model evidence по каждой операции |
| SCUNet, RealESRGAN, Big LaMa, SD1.5 | MODEL_BACKENDS включает MPS | implemented/unqualified | Сохранить модель/контракт; квалифицировать Mac lock |
| Semantic removal/background cleanup preview | adapters/installations принимают cpu/cuda/mps | implemented/unqualified | Не выключать вместе с полным semantic.transfer |
| Semantic exposure/retouch/assemble/adoption | Отдельные CPU operations | implemented/unqualified | Явный CPU worker на Mac, без переименования в MPS |
| Region preview | Backend-aware CUDA/MPS provider/admission/calibration/profile + migration02490 authored | implementing/unqualified | Production complete provider assembly и независимое Mac evidence ещё обязательны |
| Полный semantic.transfer | Transfer adapter и qualification SQL CUDA-only | planned | Отдельный MPS installation и pins |
| Learning evaluation | Provider, qualification/device и identity цепочка CUDA-only | planned | Backend-aware перенос после Region/transfer |
| identity512/eye_open/glare_mask/reflection_rgb | ONNX adapters cpu/cuda | unavailable на MPS; CPU путь требует установки/evidence | Явный CPU этап либо новый Core ML adapter |
| V28 reflection на Mac | Явный ONNX CPU provider/runtime lock; MPS provider не зарегистрирован | implemented/unqualified CPU | Отдельный backend=cpu, resource_class=gpu worker по существующему контракту, approved CPU model и qualification |
| U2Net | CPU-only | implemented/unqualified CPU | Явный CPU worker |

## M1. Регистрация отдельных возможностей

Файл `workers/photo_worker/runtime/service.py`: заменить `_select_cuda_only_backend` после готовности конкретных providers пооперационным выбором. Предлагаемый helper `select_installed_provider(operation, backend, installation)` возвращает installation либо None только при отсутствии совместимой реализации; неверные pins/повреждённая конфигурация установленного provider остаются ошибкой запуска. Не поглощать все ошибки как отсутствие функции.

Каждый provider публикует immutable backend, operation/version, runtime_sha, model_lock_sha и поддерживаемые модули. Проверять точное совпадение с config.backend и qualification, затем добавлять Operation. Region и semantic validation в service сейчас требуют CUDA; заменить на проверку совпадения backend только после завершения M2/M3. Production learning требует подписанного отчёта, это условие сохраняется.

## M2. Region поверх MPS render

1. `operations/region/adapter.py`: `_valid_renderer_metadata` допускает mps; `region_operations` регистрирует mps как `gpu`, а не существующую ветку `media` для не-CUDA. `execute` проверяет renderer.backend == execution_profile.backend до чтения артефактов.
2. `operations/region/renderer.py`: `PinnedRegionRenderer.__init__` принимает конкретный поддерживаемый backend; `_check_provider`, `verify_runtime`, `admit_resources`, `render` сохраняют immutability, hash/runtime/lease checks. Не ослаблять calibration/device checks.
3. `operations/region/admission.py`, `calibration.py`: заменить Literal[cuda] на явный cpu/cuda/mps контракт только для реально реализованных классов. Для первого порта разрешить cuda/mps; CPU Region не объявлять автоматически. Calibration catalog должен сопоставлять backend + device_profile_sha + dataset_sha + runtime/model pins.
4. Использовать существующие `v2/render_task.py`, `render_profiled_replay.py`, `render_resources.py`, `render_denoise.py`, `render_upscale.py`. Модули с ONNX-only зависимостями не включать в MPS provider без отдельного CPU task.
5. `backend/internal/v2region/region_node.go`: проверять допустимый backend и совпадение квалификации вместо literal CUDA.

SQL: выполнить forward-only миграцию актуальных определений `photo.resolve_v26_region_qualification`, `photo.v26_region_qualification_pin_ready`, `photo.v26_region_seal_scope`, Region ветки `photo.adopt_v2_plan`, descriptor/snapshot/params readiness functions. Исходные CUDA ограничения найдены в 00370, 00372, 00375, 00378, 00381, 00455, 00456. Перед изменением найти последнее определение каждой функции во всём дереве миграций; сохранить tenant/actor/session locks, лицензирование, enabled/admission_enabled, operation/version, exact backend, dataset и evidence. Не переписывать исторические миграции и не разрешать mps всем policies глобально.

## M3. Semantic

`operations/semantic/adapter.py: semantic_transfer_operations`: принимать backend выбранного immutable installation вместо literal CUDA. Для artifact installation сохранять единственного owner, runtime recheck до/после чтения и перед staging. CPU matching внутри transfer не означает, что весь GPU provider можно зарегистрировать CPU без отдельного профиля.

Removal/background cleanup previews уже backend-aware: `preview_removal_adapter.py`, `preview_background_cleanup_adapter.py`, соответствующие installation modules. Сохранить их независимую регистрацию. CPU exposure/retouch/assemble/adopt оставить отдельными узлами; план фиксирует их зависимости и артефакты.

SQL цепочка: forward-only обновить актуальные policy fingerprint, frozen qualification, frozen assembly qualification, proposal lock, worker qualification/calibration. Точки инвентаря: 00510–00516, 00521, 00632; именованные функции включают `photo.lock_semantic_proposal_qualification`, `photo.v27_semantic_worker_qualification`. Backend берётся из выбранной лицензированной policy и сравнивается со всеми frozen pins и registered worker operations. Допуск MPS не отменяет proof/evidence и не переиспользует CUDA calibration. Отдельно проверить identity/person evidence: `backend/internal/v2input/semantic_person_identity.go` содержит CUDA gate; CPU identity требует явного отдельного узла и authority связей.

## M4. Learning evaluation

1. `runtime/learning_config.py`: `learning_evaluation_from_environment` перестаёт безусловно возвращать None для MPS только после реализации всего installation; `install_signed_learning_evaluation` передаёт точный backend.
2. `operations/learning/qualified_installation.py`: заменить `installed_cuda_device` backend-aware чтением device evidence. CUDA сохраняет architecture/device memory; MPS использует утверждённый формат Apple device/runtime/OS и измеряемой памяти. Report schema версионировать; нельзя придумывать CUDA capability для Apple.
3. `qualification.py`: verifier сравнивает подписанный backend/device/model/runtime/dataset/expiry с текущей установкой. `installation.py: VerifiedLearningProvider` сверяет эти pins при старте и каждом task.
4. `pinned_provider.py: PinnedLearningProvider` получает immutable backend вместо class constant CUDA; все pool.acquire и replay profiles используют его. `depth.py: HoldoutDepthProvider.__post_init__/create` сейчас тоже CUDA-only: переносить отдельно, используя MPS-capable depth-модель и прежние contract/resource checks.
5. `_BASE_MODELS` включает identity512, не поддерживающий MPS adapter. До решения этой зависимости не объявлять полный learning.evaluate MPS. Вариант первого порта: отдельная qualified CPU identity operation, выходы с hash/version/model/runtime pins, связь с конкретным source и evaluation. Не скрытый pool.acquire(cpu) внутри MPS task. Альтернатива — новый ONNX CoreML adapter с отдельной qualification, precision и model lock.
6. NativeDenoiseProvider уже принимает cpu/cuda/mps; использовать его вместо нового denoise алгоритма. Подписанное evidence на CUDA не доказывает Mac качество.

## M5. Новые ONNX/Core ML альтернативы

Это planned, готового Core ML adapter данным аудитом не найдено. Для identity512, eye_open, glare_mask, reflection_rgb сначала использовать существующий явный CPU путь при наличии его qualification. Если нужен Core ML: реализовать backend registration и engine/provider selection, запретить неявный ORT CPU fallback, зафиксировать execution providers, converted model digest, preprocessing/postprocessing, precision, runtime version и device evidence. Сохранять исходный контракт outputs и численные критерии качества. Backend label вводить согласованно в contracts, SQL catalog, scheduler, ModelPool, doctor/evidence и UI, а не только MODEL_BACKENDS.

## Порядок и критерии кодирования

M1 независимые существующие previews/render → M2 Region → M3 transfer → M4 learning → M5 только необходимые отсутствующие adapters. README обновляется по фактическому коду после каждого завершённого переноса.

Для каждого этапа написать, но в текущей фазе не запускать, сценарии: exact backend routing; неизвестная модель/provider; отсутствие evidence; изменённые runtime/model/device pins; CPU fallback запрещён; cancelled lease до IO/перед staging; RAM/scratch/Metal allocation; совместимость старых CUDA профилей; tenant isolation; SQL admission/adoption mismatch. После кодирования отдельный обязательный этап: сборка/контрактные и интеграционные тесты, Mac runtime qualification и качество на реальных фотографиях. До него статус `implemented/unqualified`, выпуск не подтверждён.

### Реализованная часть M1: background cleanup selection

2026-10-17: runtime/service.py использует _select_background_cleanup_preview_installation перед регистрацией existing MPS-capable background cleanup preview. Проверяются passed Big LaMa backend evidence, approved model, installed factory и pinned файлы. Qualified MPS factory сохраняется; explicit unsupported исключает только этот provider; missing/untested evidence не объявляется поддержкой. Сценарии workers/tests/test_background_cleanup_backend_selection.py написаны, не запускались. Это coding implemented/unqualified, не завершение всех M1–M5 и не квалификация Metal.

## M2: текущий промежуточный source pass

### Обязательное продолжение M2: fresh task-private execution

Adopted Apply Params доказывают исходную конфигурацию, геометрию и решения
фотографа; они не доказывают RGB, полученный после изменения `effective_delta`.
Сравнение current RGB с `Params.image` сохраняется для точного replay. Для
изменённого preview необходим отдельный режим task-private execution; передача
старого image ref в качестве нового источника запрещена.

1. Trusted native installation создаёт attempt-owned execution ledger из
   физически проверенного original WorkingImage, exact FrozenRecipe, effective
   correction vector и live canonical Apply descriptor. Клиент не создаёт ledger
   и не передаёт промежуточные pixels/receipts.
2. Каждый реально выполненный оператор получает immutable private frame:
   RGB/alpha, canvas/chain, digest пикселей, предыдущий frame и exact operator
   policy. Private frame не является ArtifactRef и не изображает adopted output.
   Порядок, модель/backend/runtime и ресурсные лимиты проверяются до вычисления;
   новый frame фиксируется только после успешного завершения оператора.
3. Цвет, локальные коррекции и beauty рассчитываются заново с effective delta;
   свежий face/skin анализ относится к текущему private frame. Предыдущие Params
   и ResolvedStyle остаются неизменными документами исходной authority.
4. Authored inpaint/generation masks и подтверждённые prompt/seed/steps сохраняют
   исходную геометрию и решения пользователя; вызов модели получает текущий
   private frame. Background assets остаются exact portable/decode pins.
   Crop/body/upscale используют действительную frozen geometry, без resize-back.
5. Reviewed defects сохраняют подтверждённые spatial label/support инструкции,
   исходный proposal/review проверяются против исходного WorkingImage отдельно.
   Fresh repair не создаёт новый accepted DefectReview и не подменяет его image.
   Любое изменение поддерживаемой геометрии должно иметь явную projection;
   несовпадение области/защиты требует нового решения и отклоняет preview.
   Conservative auto selection требует нового расчёта strict fixed thresholds
   по свежим pixels и защиты permanent marks; старый auto report не разрешает
   новую автоматическую выборку. Sensor recurrence остаётся привязанной к
   исходной подтверждённой серии, не выводится из одиночного preview.
6. Independent adopter повторяет live authority, typed/physical closure,
   последовательность private frames и установленную requalification. Простой
   encoded-byte/ICC/ROI proof не доказывает fresh effect execution. Пока этот
   adopter/ledger путь не замкнут, соответствующий preview остаётся fail-closed.

Source continuation 2026-10-17 (не запускалось): `RegionPrivateSequence` mounted
в native assembly; global/local разделены и strength применяется один раз.
Fresh conservative defects используют private inventory без ArtifactRefs,
новые DoG/Face19 observations и общий strict selector; sensor recurrence
сохраняет исходную canonical series authority. Reviewed spatial инструкции
проверяются против original proposal/review отдельно. Inpaint/generation/outpaint
получают one-slot private source authority; ordinary exact replay branch
по-прежнему сравнивает RGB/alpha с admitted source.
`NativeRegionReplay` использует input-only evidence reader, отдельную qualified
CUDA/MPS installation и тот же полный native pipeline. Повторное представление
и кодирование должны дать exact derivative SHA/bytes/projection/cache key;
profile accessor связан с current canonical attempt/result и повторяется после
IO. Production adopter startup использует реальные installed model factories,
без environment callbacks. Необходима независимая source-проверка whole chain,
positive full native/SQL lifecycle/concurrency scenarios и последующая отдельная
квалификация; capability не активирована, M2 остаётся open.

Критерий кодирования: изменённые color/retouch delta проходят весь реально
поддерживаемый pipeline; неизменённый replay сохраняет прежние точные проверки.
Отдельно написать positive changed-delta сценарии и negatives подмены frame,
пропуска/перестановки stage, устаревшего решения/модели, чужого attempt, изменения
protected pixel и resource/cancel loss. Это обязательный незакрытый объём M2;
ни authored tests, ни этот контракт не являются qualification или PASS.

Написаны CUDA/MPS qualification/calibration/profile/worker/SQL gates (02490); новая установка `operations/region/installation.py` использует существующий full-canvas color pipeline. Опциональный установленный ModelPool LRASPP включает adaptive/local только при exact model lock, approved files, passed backend evidence и неизменном factory. Инференс вызывается на выбранном backend; CUDA не подставляется для MPS. Оба пути требуют отдельной полной calibration и live verifier. Это не закрытие M2 и не результат выполнения.

Открытая реализация: source-qualified typed analysis/Params closure для NativePortraitBackgroundFactory (лица/skin/person/depth/denoise), immutable selection и live recheck на seal/worker/adoption; связывание остальных доступных native factories с полным effective style; geometry-aware Region presentation для изменённого canvas. Текущий extract_rendered_region требует исходный canvas, поэтому crop/upscale/full geometry нельзя объявлять поддержанными на основании одной регистрации MPS. Неподдержанный модуль отклоняет весь preview, не пропускается.

Region native portrait добавлен как очередной промежуточный путь: свежие SFD/FAN/Face19 анализы вычисляются на выбранном backend из hash-verified original RGB; затем выполняются прежние color→beauty algorithms. Identity embeddings не вычисляются. Все три model factory требуют approved files, passed backend evidence, exact model lock и full Region workload calibration. Отсутствие модели отклоняет установку, слабые/усечённые анализы отклоняют preview. Остальные модули пока не объявляются поддержанными; closure/admission для внешних authored evidence, background assets и geometry остаются открыты.

### M2 geometry continuation: source implementation, not activation

Region keeps `request.rect` in the original frozen WorkingImage coordinate space.
A geometry-enabled path needs a server-selected `execution` descriptor built
from real adopted Apply Params/Plans and the current canonical image-producing
stage trace. Client JSON is never a descriptor authority. Non-geometric recipes
retain the preceding contract.

The descriptor records every intervening image stage, including stages with no
geometry change; geometry-only stage lists would incorrectly break source
identity between body/crop and a later upscale. Each stage binds its task,
canonical result, attempt/fence, exact Params/source/output refs and actual
source/target canvas. A changed geometry edge must equal the single appended
transform in the independently hash-read WorkingImage. A body edge also requires
the actual canonical BodyWarpReport to retain safety-skip semantics. Actual
`geometry.crop` appends a MatrixTransform from CompositionPlan, rather than a
synthetic axis-aligned CropTransform.

Source ROI projection uses crop/resize pixel-edge coordinates, matrix integer
pixel-centre semantics and every nonzero bilinear contributor of the actual
body inverse field. A removed ROI fails explicitly. The processed image is
never resized back to the source canvas. The projected bounds are extracted
from the actual target canvas and only then reduced to the requested max edge.
The optional result `projection` binds execution SHA, target canvas/chain and
projected/output bounds; its complete value participates in the derivative
cache key. The independent adopter re-reads the stage documents and physical
hash-verified inverse fields and recomputes those bounds before accepting the
encoded dimensions. Missing adoption scratch budget fails closed.

Authored source now includes `region/projection.py`, `region/execution.py`,
`region/geometry_native.py`, the projected presentation/result/adoption path,
Go execution decoder/compiler/catalog and forward-only migration 02495 metadata
selection/current-scope checks. None has been executed or qualified. The
production geometry capability remains unregistered. Required remaining wiring:
server Freeze caller and complete typed dependency closure; exact effective
geometry Params matching (base-style identity alone is insufficient in the
presence of Apply overrides); installed provider stage sequence/resource bounds;
public descriptor/client handling; positive current-authority SQL/adoption
scenarios. These are implementation gaps, not a testing-only backlog.


### M2 — mounted geometry source pass (2026-10-17, unexecuted)

SQLRegionPreparation now reads scoped immutable Style bytes, selects the unique current-canonical accepted source-bound execution when body/composition/upscale is enabled, orders its actual Params.image DAG and freezes descriptor plus bounded typed inputs. SQLRegionDependencies uses a dedicated execution resolver, re-reading current canonical receipts and physical documents before immutable Params publication; the generic Editor single-canvas resolver remains unchanged. The crop descriptor pins both the CompositionPlan and its canonical composition.plan producer Params. Migration02495 repeats these bindings at the live Region scope boundary.

Native Region production assembly now mounts adopted body fields, native crop and installed qualified Real-ESRGAN x2/x4 factories on the selected backend. Ordering is denoise→body→color/local/beauty→crop→upscale. Body uses shared tiled premultiplied linear RGB and alpha helpers; skipped body must retain identity fields. Adaptive/local person masks require exact original-model replay equality with accepted person bytes, then the same guided mask transform over actual body RGB. Independent adoption repeats effective geometry policy, composition producer policy, body report binding, physical field safety and ROI projection. Output is extracted from the actual target canvas, without resize-back. Resource estimates include simultaneously retained transformed canvases, body linear temporaries, fields/masks and mask-transform files; Python calibration independently reproduces the Go estimate.

The admitted descriptor exposes only execution SHA and final canvas. The client binds the adopted projection receipt, chain hash, target bounds, dimensions and cache key to that descriptor; display uses projected geometry. Existing nongeometry descriptors/manifests preserve their wire shape.

**Still implementing, no activation or coding-closure credit:** full typed external assets/LUT occurrences and advanced effective-style providers (background, defects, portrait_detail, generation/outpaint and regional adjustments); exact current-canonical locking review and complete positive SQL lifecycle fixtures; geometry native/independent-adopter lifecycle scenarios and normative schema/DTO reconciliation. Unknown typed occurrences fail closed rather than disappearing from the worker inventory. Tests authored so far have not been executed; installed model qualification and real-photo quality remain later verification obligations.
# M2 additional execution dependency fences — 2026-10-17

Source authoring only; nothing in this section is a test result or capability activation.

Region execution now validates each image-producing stage against its original operation Params model before replay. Canonical bytes and hash checks remain independent of schema validation; omitted Python defaults are not written back into historical Params. ResolvedStyle dependencies also undergo their full typed model and internal hash validation and must retain the frozen style reference. Historical resolution image references remain historical, rather than being rewritten to a post-body/post-denoise source.

The exact `ResolvedStyle.absolute_lut.artifact` occurrence is typed as NpyArray. Both worker and independent adopter physically verify declared, hash-checked LUT files with the existing bounded `read_lut`: only RGB_C float32 grids of 33 or 65, finite unit-range values, exact layout and EOF. The adoption mapping consumes the existing scratch budget and cannot use its LUT policy for output arrays, arbitrary shapes, alpha or inverse fields.

The execution descriptor records the actual final WorkingImage alpha presence. Projection encoding and adoption use that target alpha requirement; original non-execution previews retain their original source-alpha contract. This is infrastructure for background stages, not a claim that the advanced background provider is already mounted.

Still open before whole M2 closure: advanced effects/provider assembly, accepted portable-background asset provenance through decoded inputs, full typed dependencies for those modules, and positive SQL lifecycle/concurrency fixtures. The production startup, geometry/projection and dependency work must receive independent source review and later executable qualification. No quality qualification, module closure or measured performance is inferred from authored scenarios.

#### Region adopter: process allocator admission

Независимый Region adopter устанавливает allocator policy **до** создания ModelPool и любых model factories. CUDA использует общий `cuda_budget.install_cuda_budget` с потолком **10 000 000 000 байт**; политика действует до завершения процесса. Metal использует общий `metal_budget.install_metal_budget` с текущим `recommended_max_memory()` устройства, без искусственного потолка 10 ГБ. До повторного рендера adopter сверяет backend установленного renderer и проверяет measured remaining capacity против frozen `resource_estimate.vram_bytes`; общий calibrated admission затем повторяет workload/runtime/model/ICC/device pins. Ошибки измерения/доступности прекращают admission. Это ограничение PyTorch allocator, а не доказательство ограничения внешних GPU-библиотек: их использование по-прежнему требует квалифицированной установленной реализации. Сценарии authored, аппаратные проверки не выполнялись.

#### M2: дополнительная сверка native replay, 17.10.2026

Source-only сверка обнаружила и устранила два конкретных препятствия staged preview:

* External LUT: изменяемый Pydantic ArtifactRef нельзя использовать ключом множества. Physical gate теперь использует exact `(artifact_id, sha256)` и запрещает повторной occurrence менять размер RGB grid. Проверка NPY shape/dtype/finite/unit range сохраняется.
* Canonical JSON Params и human/culling selections содержат UUID как строки. Strict Python-domain `model_validate(dict)` ошибочно отвергал их. Typed Region readers теперь используют strict JSON-domain `model_validate_json` после проверки исходной canonical bytes/hash. Numeric coercion не включается; неизвестные поля и неверные типы по-прежнему отвергаются. Изменение охватывает execution Params/ResolvedStyle/decode, native background/detail/body/crop/upscale, automatic/reviewed repairs, inpaint/generation/outpaint, local/series и culling selection.

Текущий source inventory:

| Граница | Реальная production трасса | Авторские сценарии, без исполнения |
|---|---|---|
| Full native color replay | region_adopter_config → region_replay → installation/renderer/calibration → mapped/color_render → presentation/result | test_region_native_replay: физический входной NPY, full canvas color, ICC, JPEG и exact SHA; submitted derivative bytes недоступны |
| External LUT | geometry_replay.verify_region_luts → read_lut | test_region_external_lut_pixels: положительный RGB grid/repeated ref и отказ конфликтующих размеров/invalid values/lease loss |
| Body/crop/projection | geometry_replay/geometry_native → shared body_warp/prepare_crop_stage → projection | test_region_native_geometry_pixels, test_region_projection, test_region_geometry_policy |
| Portrait detail | detail → prepare_detail → protected hard-zero shared algorithms | test_region_detail_policy: реальные masks/strength-once/native pixels |
| Background | background → prepare_background/prepare_blur | test_region_background: transparent/solid/decoded portable asset/depth blur, exact shared math и alpha |
| Repairs | conservative/private_repairs/reviewed → shared native proposal/repair | private frame/inventory/strict-selection/no-face positives; selected review SQL authority отдельно |
| Inpaint/generation/outpaint | installed qualified model pools → operation-specific native helpers → preservation proof | policy/phase/protection tests; engine-math fixtures не являются installed-model qualification |
| Live authority | Go Freeze → immutable typed inputs → SQL execution/locked scopes → worker and adopter повторение | actual canonical SQL fixture, positive whole locked_scope, invalidations и independent-connection lock scenarios |

Остаток **не объявлен закрытым**. Полный staged replay с реально установленными SFD/FAN/Face19, LRASPP, SCUNet, RealESRGAN, BigLaMa/SD15 требует квалифицированного набора весов и calibration evidence соответствующего backend/device/workload. Подмена этих factories синтетическими renderer для зачёта M2 запрещена. Нужна независимая сверка новой сквозной трассы и всех authored fixtures, затем отложенное выполнение проверок. Из этого source pass не следует runtime PASS, аппаратная квалификация или изменение процента реализации.
