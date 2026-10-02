# 07. ColorProgram, обучение и перенос StyleV2

## Decode и цветовая математика

`decode_source(ref,DecodeParams)->WorkingImage`: signature/size→EXIF orientation ровно один раз→ICC conversion. RGB8/16 JPEG/PNG/TIFF; alpha запрещена для analyze, straight alpha разрешена apply; прозрачные пиксели исключены из fit/stats. Нет ICC и assume_profile — INPUT_PROFILE_REQUIRED. CMYK/PSD/video/multipage rejected. RAW: rawpy camera WB=true, auto WB=false, no_auto_bright=true, gamma=(1,1), output_bps16, ColorSpace.sRGB, AHD; линейный sRGB профиль, не encoded sRGB. Unsupported camera/demosaic →RAW_UNSUPPORTED.

`ICCRegistry.resolve(profile_id)->ProfileBytes` проверяет hash; `convert_icc(image,src,dst,intent=relative,bpc=False)->Image16` через libvips/LittleCMS2. Work profile primaries Rec.2020,D65,gamma2.4 — `work_rec2020_gamma24_v1`. Это не OETF BT.2020. `to_linear(C)=C**2.4`; `to_encoded(L)=clip(L,0,1)**(1/2.4)`. Все clipping точки измеряются до clamp. Master default TIFF16 work ICC; JPEG преобразуется sRGB. Preview моделей только отдельный sRGB адаптер.

`rgb_to_lab_d50(C)` для loss: linear Rec2020→XYZ D65 матрица
`[[.6369580483,.1446169036,.1688809752],[.2627002120,.6779980715,.0593017165],[0,.0280726930,1.0609850577]]`; Bradford D65→D50
`[[1.0478112,.0228866,-.0501270],[.0295424,.9904844,-.0170491],[-.0092345,.0150436,.7521316]]`; D50 white=(.96422,1,.82521). Lab f(t)=t^(1/3) при t>(6/29)^3, иначе t/(3*(6/29)^2)+4/29. Использовать согласованные float64 reference и float32 autograd implementations. Coefficients/white points versioned, сравнить с color patches, не менять после подгонки test.

## ColorProgramV1

```python
class ColorProgramV1:
    def validate(self, config: GlobalColor) -> None: ...
    def apply_global(self, rgb: FloatRGB, config: GlobalColor) -> FloatRGB: ...
    def apply_local(self, rgb: FloatRGB, person: ProbabilityMask, config: LocalColor) -> FloatRGB: ...
    def apply_strength(self, base_aligned: FloatRGB, processed: FloatRGB, strength: float) -> FloatRGB: ...
    def compile_absolute_lut(self, config: GlobalColor, size: int) -> LUT: ...
```

Порядок global: C→L; gains .25..4 по RGB; encode/clipping diagnostic; master curve→3 channel curves; decode; Y=.2627R+.6780G+.0593B; saturation0..2; encode; residual LUT ±.25; clamp. Curves7 knots x=[0,.0625,.125,.25,.5,.75,1], y∈[0,1] monotonic. local: person/background EV±1,saturation .7..1.3, смешать linear outputs по mask; strength linear blend с геометрически согласованной базой. Нет hidden exposure/WB/histogram matching.

Сервисный разрез: `image.color` global, `image.local` semantic/region corrections и завершение color strength; при series включённом сохранять отдельную explicit correction, не считать её частью извлечённого ColorProgram. Для base_aligned после warp брать тот же геометрический источник. Geometry strength умножается до warp, а не image blending. Beauty/generation имеют независимые explicit strength; общая style strength масштабирует их силы один раз в `resolve_effective_params`.

`trilinear(lut,points)` вычисляет q=C*(N-1), i=min(floor(q),N-2), t=q-i, gather8 и суммирует произведения весов. При C=1 t=1 в последней ячейке. Layout RGB_C: (((r*N)+g)*N+b)*3+c. `constant` residual `[0,0,0]`; absolute identity только dense, constant absolute не выдавать за identity. Проверять длины до allocate. Нет CUDA extensions и обязательного grid_sample5D.

## PairFit

`register_pair(before,after,options)->PairAlignment`: common oriented preview long1024, SIFT/RANSAC similarity; ECC Euclidean на gradients после scale. Median background reprojection<=1px, overlap>=.9; crop_aware>=.5, не участвует body fit. Ручная alignment матрица проходит те же overlap validation, confidence user_confirmed. Не использовать optical flow для скрытия пластики. Ignore mask только явная или доказанная unsupported object-edit area с review.

`split_blocks(valid_mask,seed)->FitSplit`: сетка 8×8 blocks, seeded shuffle, выбрать ~20% valid area для holdout целыми блоками; не пересекаются train pixels. `sample_pixels(image,split,seed,max_samples=65536)`: половина равномерно, половина из16 Y bins, пустые bins пропорционально оставшимся; выбор без replacement пока хватает. Сохранять seed/indices digest.

`fit_color(pair,samples,options)->ColorFit`: identity init; gain parameter через sigmoid .25..4; monotonic curves через positive softplus increments включая endpoints, cumulative normalized with black offset/white ceiling. Конкретно 8 положительных отрезков, cumulative первых7/sum8 даёт y7; identity seed соответствует x knots, endpoints допускают 0/1 через explicit constrained projection после шага. Projection сохраняет монотонность, фиксируется optimizer version. Saturation sigmoid0..2. Residual .25*tanh(raw). Для identity точных нулей и крайних curve endpoints задавать seed tensors напрямую и применять bounded projection; не требовать математически недостижимого sigmoid=0.

Loss: mean Huber RGB delta.02 + .2*mean(abs(LabDiff))/100 + .01 Ridentity + .1 Rsmooth + .1 Rfold. Ridentity mean normalized squared deviation params+LUT; нормировка gain диапазоном3.75, curve1,saturation2,localEV2,localSat.6,residual.5; усреднять каждый block затем mean blocks, чтобы размер LUT не подавлял параметры. Rsmooth mean second differences по осям residual; Rfold mean relu(-diagonal first differences(outputLUT)), не доказательство полной монотонности.

Stages: params400 lr.01; freeze; residual17³600 lr.003; one-pair lambda identity .1 smooth1; freeze; local200 lr.01. Holdout каждые25; early stop100 steps без relative improvement≥.001; best checkpoint. Инициализации identity плюс deterministic neutral gain [.95,1,1.05] и [1.05,1,.95], max3; выбирается holdout loss, tie index. Нет validation leakage в train. local только при mean probability на p>=.5≥.75 и area≥1%, retain если holdout loss улучшился≥5%.

`select_lut(param_fit,lut_fit)` сохраняет LUT если holdout median ΔE улучшилась≥.2 и p95 ухудшение<=.5; иначе zero residual. `compile_lut`33³ проверяется 100000 uniform seed42 samples против точной функции, p95 ΔE<=.3; иначе65³; повтор fail отключает adaptive. Reconstruction quality separate from loss.

## StyleAggregate / StyleKNN

`aggregate_color(fits,train_snapshot)->GlobalColor`: общий fixed fit с равным весом каждой принятой пары, а не среднее их нелинейных параметров. Агрегатор выполняет один joint numeric fit, streaming pair samples на CPU, minibatches равного вклада; тот же optimizer contract. `aggregate_effects` weighted median уверенных beauty/body estimates, weights confidence; не цветовой KNN. Все other extensions user_set или disabled.

`compute_stats20(linear_rgb,person,valid)->float[20]`: Y quantiles5/25/50/75/95; meanRGB3; stdRGB3; mean/std(max-min)2; fractions Y<.02/Y>.98; person fraction(mean p>=.5 over valid); meanY4 quadrants TL/TR/BL/BR. У пустого quadrant брать whole valid mean, flag in report; полностью transparent →EMPTY_VALID_REGION. std population ddof0. Person требуется adaptive даже если local=false.

`fit_normalizer(features)`: mean и population std across pair features, scale=max(std,.05). `distance` sqrt(mean(((f-fi)/scale)^2)); float64 CPU. `neighbors`: k=min(4,N), sort distance then pair_id; точное равенство всех feature float values →первый pair_id weight1. exp(-d²/2), stable log-sum-exp. `blend` absolute LUT same size/profile и local EV/saturation. При ненадёжной mask local skip+review, global сохраняется.

1 пара fixed unvalidated;2..19 общий fixed default; experimental adaptive только явное согласие, остаётся unvalidated. ≥20 и≥3 sessions grouped validation: compare fixed vs KNN median improvement>=10%, p95 worsening<=.5. `select_prototypes`: farthest-point in standardized stats20, start lexical pair_id, ties lexical; max32 LUT33/max8 LUT65; после сокращения повтор validation. Если LUT sizes разные, compile все на максимальную нужную grid, validate заново.

`calibrate_ood`: max(1,1.5*p95 nearest distance to prototype from other session), insufficient sessions threshold1 uncalibrated. Coverage bins8³ bool512, index (r*8+g)*8+b, clamp max7; coverage на исходном linear RGB. `resolve`: distance>threshold или uncovered>20% →fixed fallback+needs_review, strict→skip item. `group_fixed` выбирает один конкретный prototype для explicit group, никогда не меняет автоматически по порядку файлов.

## Профиль, сохранение, импорт

`StyleV2` extra forbid finite floats, schema2.0.0, renderer photo_renderer_v2. Full root: schema_version,renderer_version,style_id,mode,strength,color,raw_recipe,models,global,local,adaptation,beauty,defects,geometry,extensions,provenance,quality. Extension отсутствует=disabled; unknown active field rejected. Exact scalar ranges 06/08/15 и ТЗ37–39. Disabled union допускает только `{enabled:false}` либо известные inactive fields с валидными типами, чтобы принять иллюстративные fragments; генератор выпускает минимальный disabled object.

`canonicalize_style(raw)->CanonicalStyle`: UTF8 duplicate keys rejected, validate all lengths/finite, JCS IEEE754 binary64, no Unicode normalization внутри пользовательских строк, no newline; config_hash SHA bytes. Runtime вычисления float32 не изменяют сохранённые JSON float64. Import `source_file_sha256` отдельно; JCS JSONB reserialize должен дать тот же hash. Byte-equivalence Go/Python/TS проверяется golden vectors (unicode/non-BMP/negative zero/exponents).

`StylePackage.import`: allowlist style.json/assets/manifest.json, relative normalized names, no links/traversal/duplicate entries, sum bytes quota, hash/ICC verify. Portable asset IDs не DB UUID; binding таблица сопоставляет tenant-local artifact. Numeric-only style независим от исходных пар. model IDs references локальных locks, не remote URLs.

Quality: status example/measured/needs_review, transfer_confidence unvalidated/experimental/validated, reconstruction/validation/beauty_validation/geometry_validation nullable metric blocks, warnings, operation_evidence. `validated` только независимый test+acceptance из12, не пользовательская кнопка. Production apply example запрещён; диагностический `--allow-example` не доступен гостю/обычному UI.


## Уточнения, проверенные при реализации

ICC RGB16 transform использует LittleCMS relative intent без BPC и с `cmsFLAGS_NOOPTIMIZE`: стандартная предварительная LUT-оптимизация дала неприемлемую ошибку round-trip. Проверки `test_icc.py` фиксируют gamma и точность; эти флаги входят в renderer contract.

Явный `group_fixed` выбирает один prototype ID для группы. Покадровый OOD при таком выборе добавляет review reason, но не переключает prototype/fallback автоматически. Строгий автоматический OOD skip применяется к KNN-выбору; явный group_fixed остаётся осознанным выбором пользователя с предупреждением.

`color_fit_adam_v1` использует восемь softplus-интервалов; identity zero margins представлены epsilon1e-6 и constrained straight-through endpoint projection при расстоянии до0/1 менее2e-6. Это сохраняет конечные параметры/градиенты. Точно совпадающие train и holdout получают точный identity без скрытой коррекции. Перезапуски имеют одинаковые budgets и выбираются по независимому holdout.


### Ограниченная по памяти выборка: исполняемая реализация

`algorithms/sampling.py:sample_pixels(linear_rgb,mask,seed,max_samples=65536,check_active=...)` сохраняет исходный алгоритм: половина uniform без повторов, остаток поровну между16 bins линейной luma, затем uniform заполнение недостающего количества и детерминированное перемешивание. Train/holdout по-прежнему выбираются раздельно из spatial split; неизменность масок split и индексов результата сохраняется.

Полный список доступных индексов, remaining RGB и luma на весь кадр больше не материализуются. Первый проход считает доступные пиксели. RNG выбирает ранги популяции, `_Ranks.feed` переводит их в абсолютные индексы при блочном сканировании. Следующие проходы считают bins без uniform выборки, отображают ранги bins и дополняют остаток. Число выбранных индексов ограничено65536. Contiguous memmap читается через flat views; noncontiguous views обрабатываются строковыми срезами без полной копии. Блок содержит не более16384 пикселей, между блоками проверяется отмена. Конечность RGB проверяется и за пределами mask, как раньше.

12 reference fixtures (seed0/19/42, count1/17/2000/65536, shape257x263) получены из прежней реализации до её изменения; digests закреплены в tests/fixtures/sampling-reference.json. Новая реализация даёт те же индексы. Есть проверки noncontiguous inputs, отсутствия повторов, отмены, NaN вне mask и временной памяти.

Измерение work/evidence/sampling-memory.json: случайный RGB float32 2048x2048, выборка65536, traced Python/NumPy peak12388574 bytes, ~2.32s на текущем хосте. Это временные выделения sampler, без уже существующих входов и native RSS процесса; это не оценка общего fitting RAM и не аппаратная квалификация. Все117 platform tests прошли. Подключение полного pair.fit.color adapter, tiled warp и формирование его артефактов остаются отдельной незавершённой работой.


### Кооперативная отмена fitting

`fit_color(...,check_active=ctx.check_active)` передаёт callback в все этапы оптимизации, holdout и reconstruction_metrics. Он вызывается до проверки/переноса данных, между парами и инициализациями, перед шагом Adam, на границах holdout и после финальных метрик. Исключения отмены/lease loss не перехватываются как численная ошибка и не возвращают частичный FitResult. Runtime adapter обязан дополнительно проверить lease перед staging/commit. Уже запущенный PyTorch kernel не прерывается внутри; callback ограничивает продолжение на ближайшей границе, а не гарантирует мгновенную остановку GPU.

Настройки качества, learning rates, checkpoints, отбор residual/local и identity path не изменены. Шесть fitting tests прошли: identity, улучшение independent holdout, конечность residual/local, отмена до следующего Adam step, повторный запуск после отмены и отмена во время финальных метрик. Evidence: work/evidence/fitting-cancellation-tests.log. Это проверка вычислительного ядра; pair.fit.color task adapter и live runtime integration ещё не завершены.


### Тайловое совмещение обучающей пары

`algorithms/warp.py:warp_pair_tiles(before,alignment,target_shape,tile=512,check_active=...)` выдаёт WarpedTile(x,y,rgb,valid), readonly float32 RGB и boolean validity. Вход — float32 ndarray/memmap; output<=200MP, tile256/512/1024. Iterator надо потреблять последовательно, не собирать в list. `warp_pair_before` сохранён как явно выделяющий полный выход convenience wrapper; task adapter должен использовать iterator с зарезервированными scratch outputs.

Алгоритм `linear_bilinear_global_v1` вычисляет inverse similarity в глобальных float64 координатах центров пикселей (целочисленные центры согласно OpenCV registration). Четыре соседних RGB читаются непосредственно из исходного mapping и переводятся gamma2.4→linear; bilinear сумма переводится обратно. Полного linear RGB кадра и bounding-box ROI в RAM нет. За пределами источника — constant black. Для validity нужен полный source support, затем erosion3x3 в координатах target; halo1 вокруг каждого тайла устраняет разрывы валидности на стыках. Граница target исключается из обучающих пикселей.

Дробные координаты не квантуются таблицей OpenCV INTER_LINEAR. Поэтому новый warp не обещает побитового совпадения с прежним full-frame cv2.warpAffine на дробных переносах: версия алгоритма закреплена отдельно. На целочисленном identity проверена точность<2/65535; half-pixel подтверждён аналитически в линейном цвете, tile256/512/1024 дают побитово одинаковые RGB и validity при rotation/scale/translation. Корпус реальных before/after ещё не прошёл визуальную квалификацию.

Проверки memory: incremental4MP/tile256 не создаёт полного linear frame, traced temporary allocation<32MiB без входного массива. Контроль отмены между тайлами и четырьмя gather шагами, clipping далёких координат до integer conversion защищает от overflow. Все126 platform tests прошли, включая существующие registration/fitting; legacy source/config SHA baseline не изменён. Evidence: work/evidence/tiled-warp-platform-tests.log. Task adapter pair.fit.color и его scratch/manifest lifecycle остаются незавершёнными.


### Зарезервированные intermediates подготовки пары

`operations/pair/prepare.py:warped_pair(before,alignment,target_shape,scratch,tile,check_active)` — context manager, резервирующий13*target_pixels+8192 bytes до создания двух NPY: RGB float32 и boolean validity. Тайлы последовательно записываются в memmap; после flush и проверки reservation потребитель получает readonly WarpedPair. Закрытие контекста сначала закрывает mappings, затем удаляет оба файла и возвращает квоту. Исходные materialized artifacts имеют собственные одновременные резервы. Резерв не включает их повторно. Нельзя сохранять borrowed arrays после выхода из контекста.

`spatial_split(valid,seed,scratch,check_active)` аналогично резервирует2*pixels+8192 для train/holdout bool maps, вызывает split_blocks с явными output arrays и отдаёт readonly FitSplit. Суммарный дополнительный scratch при вложенных контекстах —15*pixels+16384. Downstream samples должны копироваться в ограниченные sample arrays до закрытия контекстов. Никакие временные NPY не публикуются в S3 автоматически.

`split_blocks` теперь допускает train_out/holdout_out: только оба вместе, writable C-contiguous boolean одинаковой формы, без alias между собой и входом. Выходные маски и JCS-независимый бинарный digest соответствуют прежнему алгоритму spatial8x8/20%. Запись масок и packbits/hash выполняются блоками262144 элементов, без полного inverted mask или packed-bit buffer. Отмена проверяется при подсчёте пространственных блоков, записи и хешировании.

132 platform tests прошли. Покрыты точность warp, readonly borrowed lifetimes, повторное использование освобождённой квоты, отмена/исключение потребителя, вложенные warp/split reservations, эквивалентность масок и digest при ширине не кратной8, запрет alias outputs. Evidence: work/evidence/pair-disk-prepare-platform-tests.log. Legacy SHA baseline неизменён. Это подготовка runtime fitting; полноценный pair.fit.color task adapter, GPU qualification и live service integration ещё не готовы.


### Подготовка PairSamples без полного linear frame

`sample_pixels(...,encoding='linear'|'gamma24')` сохраняет linear по умолчанию; gamma24 переводит только текущие выбранные пиксели блока в linear перед luma bins. Индексы совпадают с `sample_pixels(to_linear(full_image),...)`, но полный linear кадр не создаётся. Другие encoding отклоняются; sRGB transfer нельзя подставлять под gamma24.

`operations/pair/prepare.py:prepare_samples(pair_id,before,after,alignment,scratch,person=None,seed=42,max_samples=65536,tile=512,check_active=...)` соединяет warped_pair, spatial_split и две выборки. Split/train seed равен seed, holdout seed=(seed+1) modulo2^32. Person mask, если задана, уже должна иметь координаты after, float32[0,1] и точную форму; проверка provenance canvas остаётся обязанностью task adapter. Отсутствующая маска не синтезируется.

До закрытия contexts копируются только train/holdout before/after RGB и optional person samples, каждый набор<=65536 пикселей. Возвращается `(PairSamples,SampleEvidence)`, где evidence фиксирует split_digest, holdout_blocks, train/holdout index digests, оба seed и warp_version. PairSamples содержит encoded working RGB для существующего fitting. Full-frame inputs и scratch maps не покидают свои контексты; после возврата промежуточные резервы освобождены.

Проверки этой итерации:19 sampling tests и7 pair preparation tests прошли. Подтверждены точные индексы gamma24 против явной линеаризации при count1/2000/65536, повторяемость evidence, независимость train/holdout, срок жизни sample arrays после закрытия scratch и сохранение person values. Evidence: work/evidence/gamma-sampling-tests.log и work/evidence/prepared-samples-tests.log. Legacy SHA baseline сохранён. Сам task adapter pair.fit.color пока не подключён; backend selection, аттестация output и live execution ещё требуют реализации/проверки.


### Точный OOD coverage без полного linear RGB

`statistics.coverage_counts_bounded(rgb,alpha=None,encoding='linear'|'gamma24',check_active=...)` читает блоки<=16384 pixels и возвращает readonly int64[512] частот8³. Gamma преобразуется только для блока; alpha0 исключается. В отличие от coverage_bins bool, частоты сохраняют количество пикселей каждой ячейки и позволяют точно измерить uncovered_fraction. Полностью пустая валидная область отклоняется, а не интерпретируется как хорошее покрытие.

`adaptation.ood_reasons_from_counts(features,counts,adaptation)` принимает ровно512 неотрицательных integer counts, total1..200MP; отрицательные, float, overflow-sized и пустые counts отклоняются. Uncovered fraction — сумма частот вне style coverage / total. Distance, порог>20%, fallback и reasons совпадают с прежним попиксельным ood_reasons. Старый API остаётся доступен и делегирует общий OOD gate после подсчёта.

`resolve_style` принимает либо linear_pixels, либо coverage_counts; оба одновременно запрещены как неоднозначное evidence. Adaptive mode требует features и один источник coverage. Group-fixed сохраняет явный prototype независимо от OOD и оставляет review reasons; обычный adaptive переходит к fixed fallback либо strict skip по прежнему правилу. CPU task adapter публикует строгий ResolvedStyle и отдельный absolute LUT artifact для adaptive режима.

`style.aggregate` подключён к CPU runtime. `StyleAggregateParams` содержит 1..32 точных `ColorFit` refs, один session ID на pair, digest замороженного pair manifest, runtime lock и разрешённые model refs. Adapter повторно валидирует каждый `ColorFit`, запрещает duplicate pair и несовместимые LUT, затем считает равновесную покомпонентную медиану gains/curves/saturation/residual LUT. Медиана монотонных curves сохраняет монотонность. LocalColor включается только когда не менее половины всех fits имеют enabled local без warning; иначе модуль явно disabled. Выходы — полный строгий `StyleV2` candidate и bounded `StyleAggregateReport`; кандидат всегда `needs_review`, `transfer_confidence=unvalidated`, validation/reconstruction не выдумываются. Миграция00116 ограничивает JSON candidate100MB и report1MiB. Backend materialization и acceptance реализованы; Analyze coordinator ещё должен добавить validation/package epochs после aggregate.

`style.validate` подключён к CPU runtime и не переиспользует training `ColorFit` metrics. `StyleValidateParams` связывает candidate с 1..64 отдельными version-pinned holdout-парами, exact WorkingImage/PairAlignment, optional person/ignore masks, session IDs и training session allow-deny boundary. Пересечение training/holdout session IDs и повтор pair ID отклоняются до pixel IO. Для каждой пары используется тот же deterministic warp, 8×8 spatial split и bounded sampling; candidate и identity baseline рендерятся на одном независимом holdout population. ΔE2000 median/p95 и RGB SSIM агрегируются с равным весом пар. Профиль `style_validation_v1` требует ≥12 пар, ≥3 holdout sessions, improvement median≥10% и p95 regression≤0.5. Worker выставляет `measured/experimental` только при прохождении всех порогов; `validated` остаётся результатом отдельного acceptance gate. В остальных случаях публикуются `needs_review/unvalidated` и точные причины. Выходы `validated_style` и `quality_report` разрешены миграцией00130.

`style.package` принимает exact validated StyleV2 и связанный StyleQualityReport, повторно проверяет `config_hash`, quality binding и canonical size. Единственный переносимый numeric package — сами JCS bytes StyleV2 с `application/json`; SHA artifact обязан совпадать с `config_hash`. Отдельный bounded `StylePackage` manifest связывает package artifact и quality report. Миграция00131 задаёт output policies. Такой пакет непосредственно совместим с backend acceptance/export, который уже проверяет JSON hash и immutable artifact version. Assets-archive import для будущих переносимых LUT/ICC additions остаётся отдельным import path; текущий StyleV2 хранит LUT численно.

15 целевых tests прошли: popixel/count parity, точная доля.3, alpha exclusion, частоты вместо occupancy, bad counts и group selection. Evidence: work/evidence/counted-ood-tests.log. Результаты не заменяют full-photo и live service acceptance.


### Региональные цветовые коррекции: linear_rec2020_regions_v1

Исполняемое ядро `algorithms/regions.py:apply_regions(rgb,regions,diagnostic,check_active)` принимает один float32 RGB tile и до128 пар `(RegionAdjustment, probability_mask | None)`. Caller проверяет selector, owner и точное совпадение Canvas/TransformChain до вызова. Маски конечные float[0,1] формы tile; отсутствующая область возвращается в `RegionResult.skipped` по region_id. Нельзя синтезировать missing mask. Повторяющиеся region_id запрещены. Adapter image.local ещё не подключён.

Композиция согласована с08: группы priority по убыванию, внутри группы lexical region_id для воспроизводимой суммы. w=mask*strength. Для каждой группы delta=sum(w*delta_i)/max(1,sum(w)); она умножается на remaining coverage предыдущих групп. remaining*=1-min(1,sum(w)). Суммируются EV, saturation-1 и hue_deg; один общий transform предотвращает зависимость от порядка массива и многократное clipping перекрытий.

EV применяется в linear Rec2020. Y=.2627R+.6780G+.0593B; Cb=(B-Y)/(2*(1-.0593)), Cr=(R-Y)/(2*(1-.2627)). Hue положительного угла theta вращает (Cb,Cr) в (Cb*cos(theta)-Cr*sin(theta),Cb*sin(theta)+Cr*cos(theta)); обе компоненты умножаются на saturation. R/B восстанавливаются обратными формулами, G=(Y-.2627R-.0593B)/.6780. Это рабочее линейное chroma-пространство, не encoded YCbCr видео. Яркость сохраняется до clipping; clipping фиксируется перед gamma encode/clamp. Нулевая результирующая коррекция сохраняет исходные пиксели побитово. Style strength здесь не применяется: image.local смешивает завершённый цвет с aligned base один раз.

Память зависит от tile, не от размера фотографии. Caller не должен передавать полный200MP кадр; cancellation проверяется между правилами и перед/после transform. Проверки покрывают EV, soft priority coverage, нормирование перекрытий, перестановку правил, сохранение linear luma при hue, точное сохранение вне маски, tile equivalence, missing/invalid masks, clipping и отмену. Визуальная приёмка на реальных фото остаётся обязательной.


### Task adapter image.local

`LocalParams={image,base_aligned,resolved,person?:MaskRef,regions:[{region_id,mask:MaskRef}]}`; references обязаны быть разрешены входами task. Base и обработанное изображение требуют одинаковых Canvas, TransformChain, ColorSpace и alpha reference; оба float32. Source SHA и ICC должны совпадать с исходником ResolvedStyle. Все маски проверяются по полным Canvas/TransformChain до чтения массивов. Person selector строго person. Duplicate/unused region bindings отклоняются. Missing region, selector mismatch и confidence<.75 дают explicit skip; missing/unreliable person отключает только semantic local и требует review. Другие доступные коррекции и финальный strength продолжаются.

CPU backend, RAM admission>=512MiB, tile256/512/1024. Порядок: apply_local→apply_regions→apply_strength(base_aligned,processed,resolved.strength). Strength0 возвращает точные базовые пиксели; strength1 не выполняет лишний gamma round-trip. Каждая маска материализуется в отдельный резерв scratch, читается readonly memmap. Output резервируется12*pixels+8192; input mappings закрываются до публикации JSON manifests. Region transforms не получают style strength повторно. Report local_report содержит source refs, strength_applied=true, local skip, region skips и aggregate clipping fractions local/regions до clamp. Alpha передаётся неизменной ссылкой и должна быть declared input.

Установлен CPU operation image.local; SQL34 задаёт working_rgb/working_image/local_report output policies. SQL34 и сквозное broker→gateway→storage выполнение ещё не проверены на живых сервисах. Series corrections остаются отдельной незавершённой операцией; inference масок, полный apply graph и UI пока не готовы. Тесты на контролируемых artifact ports не являются production приёмкой.


### Производные параметры силы эффектов

`algorithms/effective.py:resolve_effective_params(ResolvedStyle)->EffectiveParams` принимает только исходный resolved contract с повторной проверкой JCS hash, отклоняет skip и уже EffectiveParams. Возвращает deep-derived modules, resolved_hash и color_strength; source не мутирует. Повторный вызов с тем же source даёт тот же результат, а не дополнительное умножение.

Общая strength умножает beauty.smoothing/tone_evenness, defects.strength, portrait_detail.teeth/shine/under_eye_strength, restoration.denoise_strength, background.blur_strength и generation.strength. Texture_keep, safety thresholds, seeds, guidance, model IDs и qualitative choices остаются исходными. Geometry.body_strength умножается на style strength; waist/hip/torso/leg controls остаются исходными и умножаются на effective body_strength только field generator. Geometry.face_slim умножается непосредственно на style strength, независимо от body_strength. Это предотвращает квадрат общей силы. Нельзя дополнительно смешивать warped pixels с исходником.

Local/region strength не масштабируется здесь: image.local завершает общий color_strength линейным смешиванием с aligned base. Дискретные действия вроде upscale/background replacement/explicit inpainting задаются собственными job policies, не превращаются в частичную геометрию через numeric scaling. Effect adapters должны принимать EffectiveParams и не повторять общую strength. Эти adapters и полный граф ещё не подключены; наличие EffectiveParams не означает выполнения beauty/generation.

### TypeScript JCS на клиентской границе

`packages/api-client/src/canonical.ts:canonicalJSON` сериализует все JSON-тела API по RFC 8785: object keys сортируются в UTF-16 порядке, `-0` становится `0`, числа используют ECMAScript binary64 representation, строки сохраняют обязательное JSON escaping. Невалидные surrogate sequences, non-finite числа, циклы, sparse arrays, `undefined`/functions и объекты с нестандартным prototype отклоняются до сетевого вызова. Лексическую проверку unsafe integer выполняют Go/Python readers по исходному JSON token: после преобразования в JavaScript `number` невозможно отличить integer token от допустимого exponent-form binary64 без ошибочного отклонения JCS значений `1e20`/`1e30`.

`ApiClient` использует `canonicalJSON` для каждого request body. Общие `packages/contracts/generated/canonical-vectors.json` теперь исполняются TypeScript-тестом и совпадают с Go/Python bytes. Проверка HTTP transport отдельно подтверждает канонический body. Это закрывает cross-language canonical bytes для P09; оркестрация полного Apply DAG учитывается в P11, а визуальная приёмка реальных фото — в P18.

`style.validate` теперь сначала публикует immutable `validated_style`, а затем формирует `StyleQualityReport.style` с exact ArtifactRef этого результата. Ранее report ссылался на aggregate input, поэтому строгая проверка `style.package` всегда отклоняла реальный последовательный запуск. Сквозной controlled-artifact test передаёт оба фактических refs без переписывания и подтверждает exact canonical package bytes. Backend epochs holdout registration/validate/package ещё должны материализовать эту уже согласованную worker-цепочку.

`style.package` является отдельным автоматическим acceptance gate для transfer confidence. При `accepted_by_metrics=true` он требует `quality.status=measured`, непустую validation metric, исходный `experimental`, отсутствие warnings и exact report/style binding; только тогда canonical package получает `transfer_confidence=validated`. При недостаточном/неуспешном holdout входной `needs_review/unvalidated` сохраняется без изменения. Manifest хеширует фактические package bytes, поэтому backend low-confidence gate видит доказанный результат, а не всегда-unvalidated промежуточный candidate.
