# Текущий triage кодирования V20–V32 — 2026-10-14

## Повторная сверка V30 production import — 2026-10-14

Ранние абзацы ниже являются историей инкрементальной реализации и не
описывают текущий остаток дословно. `NewImportRecipeCodecGraph` уже включает
typed remappers для local portrait, denoise, upscale, stats, `PortraitParams`,
defect и dust; `SQLImportEntryRecipeAuthorityFactory` подключает
`SQLImportLiveReviewReader` и текущие defect/dust issuers. Для defect label
есть `ImportDefectLabelBinaryRemapper` и occurrence publisher. Повторно писать
эти ветви не требуется.

Реальный кодовый разрыв запуска: `backend/cmd/photo/main.go:run()` вызывает
`runWithAPIDependencies(apiProcessDependencies{})`. При наличии
`PHOTO_V30_IMPORT_OPERATOR_CONFIG` scheduler loader ожидает одновременно
`ImportEntryBase` и `ImportPersonAcceptance`, которых обычный процесс не
создаёт. `ImportSchedulerOperatorFiles.Install` дополнительно требует
доверенные `Codecs.Params` и `Codecs.OverrideContent`; архив и HTTP payload
не могут поставлять эти authorities. Следующая реализация должна создать
process-owned bootstrap для этих semantic ports и независимой person
acceptance, установить его до `schedulerImportFactories` и покрыть
положительный запуск настоящей pinned deployment. Терминальный
`RejectUnsupportedImportDependency` допустим только для неизвестных Params,
но не заменяет override/model/asset admission. До этого production import
невозможно объявить включённым.

После bootstrap обязательны применение forward-only миграций и current-build
сквозной тест на БД/object storage/worker, включая отзыв review grant до
activation. Они отложены в соответствии с текущим code-first порядком и не
считаются выполненными по историческим PASS.

Это трассировка исходников для выбора следующей разработки. Она не является
процентом реализации, результатом тестирования или приёмкой модулей. Текущий
этап по согласованному порядку — дописать код и unit-тесты; запуск тестов,
миграций и проверку качества на Metal/MPS с лимитом 10 ГБ выполнить позже.

## Исправленные предположения из исторических журналов

| Участок | Фактический исходный путь | Вывод для разработки |
| --- | --- | --- |
| V28 `portrait.glare` и `image.reflection` | `workers/photo_worker/cli.py`, `runtime/glare_config.py`, `runtime/reflection_config.py`, `runtime/service.py`, `runtime/v28_installation.py` | Conditional production registration и admission factories существуют. Старую метку «runtime path отсутствует» не использовать. Квалифицированных весов, corpus evidence и пройденного исполнения это не доказывает. |
| V29 panorama/focus stack | `workers/photo_worker/operations/optical/multi_adapter.py`, `runtime/multi_config.py`, `v2/multi_adoption.py`, `tests/platform/test_panorama_blend.py`, `test_focus_pyramid.py` | Worker, adoption и авторские тесты присутствуют. Следующий кодовый разрыв не следует выводить только из даты старого отчёта; физическое качество и Apply требуют отдельной приёмки. |
| V30 delivery continuation | `backend/internal/workflow/delivery_child_observer.go`, `delivery_continuation_coordinator.go`, `delivery_assembly_pipeline.go` | Observer, continuation admission и assembly pipeline уже написаны. Журнал от 2026-10-10 описывает более раннюю стадию; надо сверить текущие final encode/adoption/publish, а не повторно писать observer. |
| V31 SignExport | `backend/cmd/photo/main.go`, `workers/photo_worker/operations/provenance/execute.py`, `runtime/signer_installation.py` | API installer, signer worker и физическая проверка байтов имеют исходный путь. Нужны реальные SDK/key/trust inputs и глубокая проверка; source presence не равна подписанной поставке. |
| V32 tenant purge | `backend/internal/maintenance/tenants.go`, `tenant_buckets.go`, `backend/internal/objectstore/tenant_purge.go` | Два прохода по версиям и multipart во всех настроенных бакетах, renewal lease и SQL Finish связаны. Применение миграций и физическая проверка очистки отложены. |

## Подтверждённый кодовый разрыв

V30 imported whole-person mask: `ImportPersonMaskQualificationInstallation`
требует внешнего `ImportMaskQualifier`, но основной процесс не устанавливает
независимый origin/consent authority для архивной маски. `MaskRef` содержит
селектор, пиксели и модельный lock, но не содержит надёжного person/consent ID;
переносить архивное согласие или выводить идентичность по совпадению хеша
нельзя. До появления явного подтверждения и текущей проверки consent импорт
этой семантики обязан оставаться unavailable. Нужны контракт пользовательского
подтверждения для новой asset version, tenant/project-scoped сохранение его
доказательства и epoch, SQL/Go проверка отзыва до/после IO, подключение к
`ImportRecipeDeploymentFiles.Install`, UI состояния и unit-тесты. Существующая
физическая проверка NPY/warp и model qualification остаётся обязательной.

Другой актуальный риск — наличие множества новых forward-only миграций и
незапущенных тестов в рабочем дереве. Перед объявлением рубежа «кодирование
закончено» нужен requirement-by-requirement проход по `07-execution-acceptance.md`
с трассой UI → API → transaction → task → worker → adoption → review/Apply,
включая новые маршруты. Исторический процент entrypoints этого не измеряет.

## Уточнение после V30 status/revoke кода

Новые миграции `02003`–`02006`, Go API и клиентский экран записывают grant/revoke,
читают сохранённый статус и дают отозвать grant после подготовки импорта. SQL
`v30_import_person_mask_confirmation_live`/`prepared_live` и
`SQLImportConfirmedMaskAuthority` проверяют текущий epoch; factory собирает
его в `ImportConfirmedMaskAuthority`, включая подготовленные override. Это
закрывает ранее указанное отсутствие независимого consent authority **на уровне
компонентов**, но не доказывает работающий production import.

Следующий кодовый разрыв найден по производственным ссылкам: вне тестов
`SQLImportSchedulerRecipeFactory`, `SQLImportEntryRecipeAuthorityFactory` и
`ImportRecipeDeploymentFiles.Install` пока не вызываются из `backend/cmd`.
Соответственно, до утверждения готовности V30 нужен production scheduler
installer с конкретной конфигурацией/закреплёнными operator reports и
подключением к leased import continuation. Проверить всю цепочку source →
prepared dependencies → final adoption/review; отсутствие вызова в `cmd`
само по себе не определяет объём недостающего кода, но исключает claim о
полностью установленном production пути. Unit/SQL-тесты этого прохода
написаны, но по code-first порядку не запускались.

Worker `PreparedRecipeDocumentQualifier` теперь имеет отдельный optional
`qualify_confirmation` port для подтверждённых импортированных whole-person
масок в Override/ResolvedStyle. Без него архивное подтверждение отвергается;
с ним проверка повторяется до и после runtime qualification.
`PreparedImportRecipeFactory` передаёт порт дальше, но production installer
ещё должен предоставить независимую live SQL проверку exact occurrence/epoch.
Произвольный callback без этой связи не закрывает V30.

`02007` добавляет worker-only SQL read текущего grant: сначала повторяется
полная `v30_interchange_import_ready` lease/source проверка, затем из
сохранённого parsed evidence вызывается `prepared_live` для точной целевой
версии и prepared mask. Worker DB adapter и `SQLImportPersonMaskConfirmation`
передают текущий epoch в recipe factory, которая требует неизменный epoch на
всех повторных проверках, включая проверку после array IO. Установка
`sql_prepared_import_installation` требует наличие worker SQL ABI. Этот слой
ещё не означает production enablement: импортная worker installation и Go
scheduler factory пока не подключены к запускаемым ролям, миграция не
применялась, тесты не запускались.

Worker `build_worker` дополнительно отказывает в регистрации
`v2.interchange.import`, если миграция worker-only person-mask SQL ABI не
установлена с ожидаемыми владельцем, `SECURITY DEFINER`, search path и
правами. Это защищает запускаемую роль даже при явной внешней инъекции
`ImportInstallation`; операторская конфигурация полной recipe/runtime
qualification и вызов этой установки из CLI остаются отдельным кодовым
разрывом.

`ImportSchedulerOperatorFiles.Install` теперь объединяет pinned native,
style, denoise, upscale, Stats и person deployment с текущими explicit
semantic ports в один `NativeImportSchedulerFactory`. Он требует полную
декларацию, проверяет operator pins и `ValidateImportRecipeInstallation` до
возврата factory; при любом отказе возвращает нулевой результат. Запускаемый
`backend/cmd/photo` ещё должен предоставить реальные `Params`/
`OverrideContent` authorities, независимый person acceptance и вызвать эту
композицию по явной конфигурации. До этого V30 import production path не
установлен.

`RejectUnsupportedImportDependency` теперь предоставляет явный терминальный
codec для неизвестных `Params`/контрактов: он не вызывает resolver и не
публикует архивные байты. Его можно установить только сознательно как
fallback. Он **не заменяет** недостающие typed remappers для дополнительных
операций (`PortraitParams`, dust/defect и другие схемы из worker
`PARAM_MODELS`); эти операции остаются кодовым разрывом для полной V30
совместимости.

Source audit дополнительных Params уточнил зависимость: `PortraitParams`
ссылается на `ResolvedStyle` и вложенные `MaskRef`, dust/defect Params — на
proposal/review и другие типизированные документы. `ImportRecipeCodecRouter`
сейчас маршрутизирует только StyleV2, CorrectionPlan, Override, LayerStack,
MaskRef и Params; отдельной remap/qualification ветки `ResolvedStyle` и
связанных proposal/review контрактов нет. Эти схемы нельзя закрыть общим
копированием JSON или одним терминальным fallback. Нужна полная
типизированная цепочка зависимостей для каждой операции.

`AbsoluteLUTArray` теперь направляется рекурсивным import remapper в
бинарный порт. Пока физический LUT codec не установлен, контракт получает
явный `binary_dependency_codec_not_installed`; JSON codec и публикация не
вызываются. Добавлен адресный unit test этого отказа, но он ещё не запускался.
Это исправляет маршрутизацию, а не закрывает импорт LUT или `ResolvedStyle`.

Добавлен `ImportResolvedCodec` для `ResolvedStyle`: обязательный порт полной
квалификации архивного и подготовленного документа, typed remap
image/style/stats/absolute LUT и вложенного Override, пересчёт body hash.
`ImportRecipeCodecRouter.WithResolved` устанавливает его только явно, не
меняя уже созданный router. Unit-тесты описывают перенос ссылок и отказ до
публикации при недоступной архивной квалификации; по code-first порядку ещё
не запускались. Production qualifier, физический LUT codec и подключение
физическая LUT и SQL-публикация всё ещё отсутствуют, поэтому полный
import `ResolvedStyle` пока не считается реализованным.

Граф `NewImportRecipeCodecGraph` теперь принимает явный `Resolved` qualifier
и только при его наличии устанавливает `ImportResolvedCodec`. Добавлен
`NativeImportResolvedQualifier`: он использует существующий schema-v4 native
probe для полного `ResolvedStyle` и отдельно требует source-scoped authority
для архивного и целевого документа; тест описывает обе проверки и отказ
при неверном source binding. Unit-тесты и native probe не запускались.

`BoundImportResolvedScope` связывает архивный `ResolvedStyle` с фиксированной
old/new парой, сверяет архивных детей с inventory, повторно квалифицирует
источник, а целевой WorkingImage проверяет через master reader, canvas и
установленный ICC pin. `SQLImportEntryRecipeAuthorityFactory` устанавливает
его из pinned style operator. End-to-end импорт всё ещё закрыт:
`ResolvedStyle`/`Stats20` и `AbsoluteLUTArray` отсутствовали в allowlist
prepared store, publisher, resolver, registry и SQL
`v30_interchange_prepared_scope`.

Добавлен typed `Stats20` codec: он проверяет полный статистический документ,
перепривязывает только `source_image` к новому WorkingImage и сохраняет
значения, coverage и warnings. Подготовленный JSON path теперь принимает
`Stats20`/`ResolvedStyle` в publisher, store, reader, registry и resolver;
миграция `02008` расширяет закрытый SQL allowlist с прежними проверками
tenant, source, происхождения, media type и размера. Квалификация closure
проецирует их дочерние ссылки. Миграция и unit-тесты не запускались.
`AbsoluteLUTArray` по-прежнему требует физический NPY codec, сертификат
происхождения и бинарный prepared path; до этого полный `ResolvedStyle`
импорт должен возвращать unsupported.

Добавлен архивный LUT geometry source: он ищет exact `AbsoluteLUTArray` edge
только в `ResolvedStyle` того же старого WorkingImage, вызывает полный
архивный resolved qualifier, сверяет размер 33/65, RGB_C, float32 и профиль.
`ImportLUTBinaryRemapper` использует этот source в существующем native
array probe, повторно сверяет scoped object receipt и исходный parent после
копирования. Scheduler устанавливает его вместе с MaskPixels через закрытый
binary router при наличии pinned style installation. Unit-тесты описывают
ошибку native probe до публикации и разделение ролей; не запускались.
Publication `AbsoluteLUTArray` пока закрыта Go/SQL prepared allowlist, а
prepared closure ещё не имеет LUT leaf resolver. До их появления end-to-end
путь остаётся неполным.

Исправлена миграция `02008`: она расширяет внутреннюю
`v30_interchange_prepared_scope_plain`, сохраняя уже установленную цепь
warp/background wrappers. Для LUT добавлен `ImportLUTOccurrence` с точными
original/parent/size, отдельный publisher method и Go-проверка производного
UUID. Миграция `02009` расширяет plain allowlist и добавляет внешнюю
LUT-обёртку, которая требует parent в архивном inventory, live staged file,
совпадение числа байтов с input receipt, допустимый размер NPY и исходную
ссылку; `02010` восстанавливает сертификат при
каталожном чтении. Прямой `PublishImportBinary` для LUT без occurrence
отказывает. Проверка миграций и тестов отложена по code-first порядку.
Worker также проверяет массив перед выполнением.

Prepared LUT leaf теперь установлен: после полного `ResolvedStyle` codec
validator сохраняется witness конкретного parent/geometry, а
`SQLPreparedImportLUTGeometry` заново читает parent, проверяет его exact
child edge и текущий SQL occurrence. Миграция `02011` сверяет origin
подготовленного parent с `lut_occurrence.parent` и вызывает оба текущих
`prepared_document` gate. Leaf затем повторяет физический native NPY probe
над опубликованным объектом. Добавлены unit-тесты отказа без witness/native
authority; миграции и тесты не запускались. End-to-end готовность V30 всё
ещё требует закрытия остальных typed контрактов, подключения запускаемых
ролей и полного requirement-by-requirement прохода.

Для `PortraitParams` добавлен typed import codec: архивная структура
проверяется, `image` переводится на целевой WorkingImage, `resolved` проходит
через `ResolvedStyle` remapper, каждый inline `MaskRef` — через общий
физический mask path; итог квалифицируется для `portrait.detail:1`.
`confirmed=true` требует независимый `ImportPortraitRegionConfirmation` с
проверкой после remap и до/после runtime qualification. Production authority
подтверждений регионов пока не установлен, поэтому такие архивные случаи
остаются unsupported, а не наследуют foreign consent. Unit-тест добавлен,
не запускался. Dust/defect proposal/review и их Params ещё требуют typed
цепочки.

Добавлен `ImportDefectRepairParamsCodec` и включён в граф Params. Он проверяет
каноническую структуру, точное совпадение архивного `image` с источником,
перепривязывает `DefectCandidates`, `DefectReview`, `ResolvedStyle` через
типизированный resolver и защищённый `MaskRef` через mask codec, затем требует
квалификацию `defects.repair:1` для целевой версии. Unit-тест описывает все
четыре зависимости и отказ при устаревшем target image; тест не запускался.
Полный импорт defect остаётся незавершённым: собственные кодеки
`DefectCandidates`/`DefectReview`, физический `DefectLabelMap`, независимое
подтверждение решений review и prepared publication ещё не установлены.
Dust-контракты также остаются открыты: candidate ID зависит от
упорядоченного набора SHA всех кадров, поэтому одиночная перепривязка кадра
некорректна.

Добавлен отдельный `ImportDefectReviewCodec` с опциональной установкой в
закрытый router. Он структурно проверяет архивное решение, переносит точные
`DefectCandidates` и `DefectLabelMap`, перепривязывает `image` и требует
`ImportDefectReviewAuthority` до и после публикации новых байтов. Без новой
authority вызов терминально отклоняется до resolver, то есть архивные
accepted/rejected IDs не становятся автоматически новым разрешением. Unit-тест
этого отказа и typed remap написан, не запускался. Production authority,
codec proposal/label map и prepared allowlist/SQL всё ещё открыты; один этот
codec не делает импорт defect завершённым.

`DefectLabelMap` теперь классифицируется recursive remapper как бинарная
зависимость. Без физического codec импорт останавливается с
`binary_dependency_codec_not_installed` до JSON codec и публикации; добавлен
адресный unit-тест, не запускался. Это устраняет риск трактовать PNG labels
как JSON, но physical binary remap и сертификат происхождения ещё нужны.

Уточнён контракт `ImportDefectReviewAuthority`: он теперь возвращает заново
выданный `DefectSelection` для target proposal, включая актуальные candidate
IDs и revision. Кодек сохраняет только точные перепривязанные image/proposal/
label-map ссылки и проверяет структуру нового решения до публикации; после
публикации authority повторно квалифицирует его. Это исключает молчаливое
копирование архивных candidate IDs при изменившихся source pixels. Unit-тест
проверяет замену ID, ещё не запускался. Реальная authority выдачи решений
пока не установлена.

Добавлен typed `ImportDefectCandidatesCodec` с опциональной установкой в
recipe router. Архивный proposal проходит полный structural parser и отдельную
provenance authority. Кодек читает целевой WorkingImage, требует совпадения
canvas/transform chain, берёт его реальные `pixels`, переносит `DefectLabelMap`
через binary resolver и пересчитывает каждый candidate ID из target pixels SHA
и label-map SHA. Итог повторно проходит полный parser и target qualifier;
unit-тест описывает пересчёт и отказ без provenance authority, не запускался.
Production qualifier, физический binary label remapper и prepared publication
пока не установлены, поэтому полный путь defect остаётся незавершённым.

Prepared JSON path расширен для `DefectCandidates` и `DefectReview`: publisher,
store, reader, codec validator, typed child projection, closure router и
registry теперь используют те же закрытые проверки, что прочие JSON-документы.
Миграция `02012` расширяет внутренний SQL
`v30_interchange_prepared_scope_plain`, сохраняя внешние warp/background/LUT
обёртки и строгие tenant/source/origin/media/size условия. Unit-тест описывает
точные роли дочерних ссылок обоих документов; миграция и тест не запускались.
Физический `DefectLabelMap` leaf и production authorities остаются открыты.

Добавлен `ImportArchivedDefectLabelSource`: он сканирует inventory одного
архива, читает точные `DefectCandidates`, требует соответствия старому
WorkingImage и exact label edge, выполняет полный proposal parser и
архивную provenance qualification. Источник выдаёт проверенный parent для
будущего occurrence certificate и может использовать существующий физический
uint16 NPY verifier. Unit-тест описывает положительный parent binding и отказ
без qualifier, не запускался. Код физического копирования, SQL certificate и
prepared leaf ещё не установлен.

Добавлен `ImportDefectLabelBinaryRemapper`: он требует exact archived parent,
сверяет размеры с целевым WorkingImage, выполняет полный физический verifier
`uint16` NPY по каждому архивному proposal, повторно читает object receipt и
master после копирования и принимает только отдельный occurrence publisher.
`ImportBinaryCodecRouter` теперь держит `DefectLabelMap` в отдельной роли;
unit-тесты описывают разделение ролей и отказ без occurrence publisher, не
запускались. Реальный publisher/SQL certificate, prepared label leaf и
production installation всё ещё отсутствуют.

Добавлен `ImportDefectLabelOccurrence` и отдельный publisher method. Go receipt
содержит exact original/parent/width/height, производный UUID и запрет
смешивания с другими occurrence-видами; прямой binary publish без сертификата
для `DefectLabelMap` отказывает. Миграция `02013` расширяет внутренний
prepared plain allowlist и добавляет внешнюю wrapper-проверку inventory parent,
архивного файла, размеров и byte receipt; `02014` восстанавливает сертификат
при каталожном чтении. Unit-тест сертификата написан. Миграции и тест не
запускались. До полного пути остаётся prepared label leaf с повторной
физической проверкой и production installation.

Prepared validator теперь сохраняет witness для каждого полностью
квалифицированного `DefectCandidates` с точной дочерней ролью
`DefectLabelMap`, source binding и размерами canvas. Unit-тест описывает
положительную запись и отказ при неверной роли/источнике. Миграция `02015`
добавляет SQL-функцию, которая сопоставляет текущие готовые parent и label
documents, их binding и origin parent в occurrence certificate. Тест и
миграция не запускались. Сам prepared binary leaf с повторной физической
проверкой ещё должен вызывать эти две authority.

Prepared `DefectLabelMap` leaf теперь установлен в resolver router. Его source
для каждого обращения заново читает квалифицированный prepared
`DefectCandidates`, сверяет точный label edge и source, требует актуальный
SQL occurrence через `02015`, а затем существующий `ExportDefectLabelQualifier`
повторно проверяет физический `uint16` NPY и стабильность catalog receipt.
Closure/registry/binary reader включают отдельную label роль. Unit-тесты
router и binary reader расширены для этой роли, ещё не запускались. Полный
production import defect всё ещё зависит от установки semantic authorities,
binary remapper и готовности миграций в окружении.

`SQLImportEntryRecipeAuthorityFactory.BuildImportEntryBinaryRemapper` теперь
устанавливает `DefectLabelMap` в отдельную binary роль только при наличии
`DefectCandidates` qualifier и occurrence publisher; остальные роли mask/LUT
сохраняются. Unit-тест описывает наличие и отсутствие этой условной установки,
не запускался. Запускаемый scheduler по-прежнему не предоставляет production
semantic authority для defect proposal/review, поэтому полный V30 import
остаётся закрыт.

Добавлен `BoundImportDefectCandidatesQualifier`: для конкретного adopted entry
он повторно проверяет old/new source pair через scoped source authority,
точные архивные bytes `DefectCandidates` через inventory и object reader, затем
вызывает независимую semantic producer authority. Factory связывает его с
конкретной версией до создания typed codec; binary label remapper строит такой
же binding по точному entry/old/new перед физической проверкой. Unit-тест
описывает валидный архив, изменённые bytes и чужую целевую версию; не
запускался. Производственная semantic producer authority и независимая выдача
review решения пока отсутствуют.

`BoundImportDefectReviewAuthority` теперь оборачивает независимую authority
переиздания решения в entry-scoped production graph. До принятия её ответа он
читает точный подготовленный `DefectCandidates`, проверяет immutable body,
вызывает semantic qualifier proposal и сверяет каждый принятый/отклонённый
candidate ID, image и label с этим proposal. После проверки решения authority
повторяет те же проверки, включая чтение prepared bytes. Unit-тест описывает
отказ при постороннем ID; тест не запускался. Самостоятельная production
authority получения нового пользовательского решения всё ещё нужна.

Для dust добавлен `review.RemapDustProposalFrames`: он строго читает исходный
proposal, требует перепривязать каждый кадр упорядоченной серии, отклоняет
дубли и несовпадение base, пересчитывает candidate ID из всех новых frame SHA
и повторно проверяет итоговый proposal. Unit-тест описывает замену всей серии
и отказ при дубле; тест не запускался. Это базовый чистый преобразователь,
не production import codec: scoped source authority, сохранение prepared
`DustCandidates`, новое review и `DustRepairParams` ещё требуются.

Добавлен `ImportDustCandidatesCodec` и закрытый router slot для него. Codec
требует отдельную `ImportDustSeriesAuthority`, проверяет архивный документ,
перепривязывает base через точный `WorkingImage` resolver, остальные кадры —
через series authority, затем пересчитывает ID всей серии и квалифицирует
готовый документ. Unit-тест описывает пересчёт и отказ без authority, не
запускался. Production series authority, prepared allowlist/closure и новый
dust review ещё отсутствуют; условная установка codec не делает импорт dust
рабочим end-to-end.

Для вложенного `DustReview` добавлены `ParseDustSelection`, структурная
проверка sorted/disjoint решений и `VerifiedDustProposal.ValidateSelection`.
Последняя требует точный proposal, base-кадр внутри серии и существование
каждого принятого/отклонённого candidate ID. Unit-тест описывает валидное
решение, посторонний ID и конфликт двух списков; тест не запускался.
Следующий обязательный слой — Params codec с независимым новым review, а не
перенос архивного выбора.

Добавлен `ImportDustRepairParamsCodec` в typed Params chain. Он принимает
точный трёхпольный контракт `image`/`proposal`/`review`, требует согласованного
отсутствия proposal и review, перепривязывает proposal через `DustCandidates`,
а для reviewed варианта требует `ImportDustReviewAuthority`, которая выдаёт
новое решение для целевого proposal. Новый выбор структурно проверяется,
перед qualification операции повторно квалифицируется authority. Unit-тест
описывает отказ без issuer и замену candidate ID; тест не запускался.
Production issuer и проверка решения против подготовленного proposal пока
отсутствуют, поэтому reviewed dust остаётся закрытым.

`BoundImportDustReviewAuthority` теперь устанавливается entry-scoped вокруг
независимого dust issuer. Он читает точный prepared `DustCandidates`, повторно
проверяет всю серию через semantic authority и сверяет выданные accepted/
rejected ID с её кандидатами; до и после qualification решения чтение и
проверка повторяются. Prepared reader допускает контракт `DustCandidates`.
Unit-тест описывает отказ при ID вне proposal; не запускался. SQL prepared
allowlist и физическая production series authority по-прежнему необходимы.

Prepared JSON route теперь включает `DustCandidates` в publisher, store,
reader, codec validator, registry и closure resolver. `v2draft.DocumentChildren`
структурно проверяет dust proposal и выводит все 5–64 кадра как упорядоченные
`WorkingImage` children; unit-тест описывает точные роли и порядок, не
запускался. Миграция `02016` расширяет только внутренний SQL plain allowlist
prepared scope, сохраняя существующие внешние wrappers и ограничения tenant,
source, origin, media и размера. Миграция не применялась. Полный cross-entry
series authority и разрешение всех prepared WorkingImage ещё требуют проверки.

Добавлен `BoundImportDustSeriesAuthority` для конкретного adopted entry.
Каждый кадр из dust-серии он сопоставляет с единственной old/new парой
`ImportCorrectionBinding`, заново выполняет scoped source qualification и
отклоняет чужие либо неоднозначные кадры; архивный proposal должен находиться
в inventory с точными байтами. Для prepared proposal он заново проверяет все
целевые кадры и затем вызывает независимую semantic authority для camera/
sensor identity. Factory связывает authority с entry до построения codec.
Unit-тест описывает допустимую sibling-пару и отказ при чужом кадре или
перенаправлении semantic authority; не запускался. Независимая production
camera/sensor authority ещё не установлена.

Добавлена `MetadataImportDustSeriesAuthority`: она сверяет `camera_id`, sensor
width/height, crop и orientation каждого нового кадра с текущими ingest
metadata точной asset version. `SQLImportDustSensorReader` читает эти поля из
tenant-scoped `asset_versions` при текущем editor ACL и exact master binding;
factory может установить reader как отдельную зависимость и собирает semantic
authority поверх всех принятых old/new bindings. Unit-тест описывает отказ
при чужой камере и crop; не запускался. Запускаемый scheduler ещё не передаёт
SQL reader, а сквозная проверка импорта по реальной БД остаётся позже.

`v2draft.DocumentChildren` теперь распознаёт точный `DustRepairParams` с
вложенным `DustReview`: выводит image как `WorkingImage`, proposal как
`DustCandidates`, требует согласованное null/null для нерецензированного
варианта и проверяет точное совпадение review base/proposal. Unit-тест
описывает две роли и отказ при чужом base; не запускался. Это закрывает
ошибочную generic `Artifact`-проекцию вложенного review в prepared closure.

`backend/cmd/photo` теперь передаёт установленную через доверенные process
dependencies `SchedulerInterchangeImportFactory` в запускаемый scheduler.
Отсутствующая установка передаётся как пустой список, не как nil factory;
unit-тест описывает оба случая, не запускался. Стандартный bootstrap пока
не создаёт этот factory из operator pins/semantic ports, поэтому production
import не считается включённым по умолчанию. Также `DustRepairParams` с
лишним полем теперь попадает в строгий decoder своего codec, а не в generic
fallback; unit-тест проверяет отсутствие вызова fallback, не запускался.

Scheduler dust sensor read теперь отделён от editor ACL: миграция `02017`
добавляет scheduler-only `SECURITY DEFINER` функцию, которая повторно вызывает
полный `v30_interchange_import_sources` и metadata receipts, требует
единственную adopted entry с точными version/master/SHA и возвращает только
валидный RAW sensor alignment этого tenant/project. Go reader выполняет вызов
в tenant transaction; workflow binder устанавливает его автоматически для
import dust при отсутствии другой доверенной semantic authority. Миграция
и сквозная SQL-проверка не запускались. Production review issuer и общий
operator bootstrap V30 всё ещё открыты.

После коммита `fb5bff5` bound issuer для импортируемых `DefectReview` и
`DustRepairParams` дополнительно проверяет до вызова независимого issuer,
что archived image/base принадлежит старому source, provisional image/base —
target source, версии и source artifact различаются, canvas hash совпадает,
а proposal (и defect label map) действительно получили новый artifact ID.
Unit-тесты описывают отказ при подмене каждого source, повторной выдаче на
ту же версию и оставленном архивном proposal; они пока не запускались. Это
ограничение входа в issuer не заменяет самостоятельное текущее решение
пользователя: production issuer остаётся открытым.

Структурный preflight `SQLImportEntryRecipeAuthorityFactory` теперь
отклоняет установленный defect review без defect-candidate authority,
dust review без series/sensor authority и конфликт двух способов задания
dust series. Для каждого случая написаны отрицательные unit-тесты; три
полных сочетания имеют положительные unit-сценарии. Это переносит отказ
с обработки конкретной entry на startup operator factory. Тесты пока не
запускались. Само подключение factory из доверенной runtime-конфигурации
и production issuer всё ещё открыты.

`backend/cmd/photo` теперь умеет собрать `ImportSchedulerOperatorFiles` в
scheduler startup из process-owned файлов, base semantic ports и независимого
person acceptance. Неполная или конфликтующая установка (включая
одновременную прямую factory injection) останавливает startup; ошибка pin
проверки не превращается в silent disabled import. Unit-тесты описывают
отсутствие установки, прежнюю прямую injection, отказ при неполной установке
и cancellation; не запускались. Обычный `run()` всё ещё создаёт пустые
process dependencies: ему нужны реальные operator config и semantic ports,
а также production issuer для дефектов и пыли. Поэтому этот bridge сам по
себе не включает импорт в production.

Для scheduler добавлена process-owned декларация
`PHOTO_V30_IMPORT_OPERATOR_CONFIG` + `PHOTO_V30_IMPORT_OPERATOR_CONFIG_SHA256`.
Файл имеет строгую canonical JSON schema
`interchange-import-scheduler-operator/v1`, лимит 64 KiB, точный SHA256,
проверку inode/size/mode/mtime до и после чтения, абсолютные пути и SHA всех
шести subordinate deployments, а также bounded archive/expanded/file limits.
Непарные env-переменные, конфликт с прямой декларацией и отсутствие
доверенных semantic ports останавливают scheduler startup. Unit-тесты
для decoder/file loader/env-path написаны, но не запускались. Обычный
`run()` пока не устанавливает необходимые semantic ports самостоятельно;
production import поэтому остаётся выключенным без доверенного process
bootstrap.

Добавлен unit-сценарий полного env → pinned declaration loader → operator
installer: корректная верхняя декларация достигает проверки subordinate
deployments, но отсутствующий native файл не создаёт scheduler factory;
одновременная env и injected декларация отвергается. Сценарий не запускался
и не заменяет положительную проверку реальных operator файлов.
Декларационный loader дополнительно покрыт unit-отказами для canonical JSON
с неизвестным полем, symlink, group/world-writable файла и отменённого
контекста; эти тесты также не запускались.

Для будущего отдельного пользовательского решения по импортированным
`DefectReview`/`DustRepairParams` добавлен `ImportReviewDecisionRequest`.
Он задаёт target version/base/proposal, для defect также label map,
отдельные grant/revoke и expected epoch; архивное решение в request не
входит. Валидатор отвергает чужие/пустые refs, несогласованные kind,
несортированные/повторные candidate IDs, отзыв без текущего epoch и
непредусмотренную передачу selection при revoke. Unit-тесты написаны,
не запускались. Это только входной контракт: отдельные SQL decision store,
API/UI и production issuer ещё нужны; существующий live-run review persist
не подходит без новой транзакционной authority.

Forward-only миграция `02018` создаёт отдельное tenant-scoped хранилище
`v30_import_review_decisions` для target proposal/base/label map, решения,
актора, request ID и epoch. Включены forced RLS, закрытые прямые права и
явный purge allowlist в `finish_tenant_deletion`; SQL integration test
проверяет эту изоляцию. Миграция не применялась, тест не запускался.
Таблица намеренно инертна до появления функций grant/revoke и issuer:
архивный review не получает путь прямой записи в неё. Необходимы также
пауза/возобновление import continuation для нового пользовательского
решения и экран просмотра подготовленного proposal.

Миграция `02019` добавляет первый mutation path:
`photo.grant_v30_import_review` доступна только API и перед записью проверяет
текущего editor, активный workflow/run, точные import sources и base,
опубликованный prepared `DefectCandidates`/`DustCandidates`, SHA реальных
байтов proposal и принадлежность выбранных candidate ID его массиву.
Запись сравнивает expected epoch; точный retry по request ID и digest
возвращает прежний receipt. Невалидная/чужая область возвращает NULL.
Написан SQL integration test для ACL и отсутствующей области; миграция и
тест не запускались. Для завершения контура ещё требуются revoke/status,
API-загрузка реальных proposal bytes, UI, ожидание нового решения в
continuation и scheduler issuer с повторной live-проверкой epoch.

Миграция `02020` добавляет API-only `revoke_v30_import_review` и точечное
чтение `v30_import_review_status`. Отзыв допускается после остановки run,
но требует текущего editor ACL, точного ключа proposal/base/label map и
expected epoch; точный повтор request ID с сохранённым request hash
идемпотентен. Status возвращает только одну запись после проверки проекта.
`02018` хранит отдельный `request_hash`, чтобы revoke не перезаписывал hash
исходного grant. Unit/SQL integration сценарии ACL и отсутствующего scope
написаны, но не запускались; миграции не применялись. Scheduler live reader,
Go API и экран всё ещё открыты.

Миграция `02021` добавляет API-only чтение одного опубликованного prepared
`DefectCandidates`/`DustCandidates` по точным workflow/entry/target/proposal.
Она повторяет текущие source и editor gates, сверяет сохранённый scope
`backend_documents` с master и immutable artifact metadata. Go reader читает
не более 1 MiB versioned object bytes, проверяет SHA и структуру proposal,
после IO повторяет SQL resolution, чтобы не принять заменённый объект или
scope. Написаны unit-тесты для успешного чтения, смены версии и порчи байтов,
а также SQL ACL/absent-scope тест; не запускались. API endpoint и grant
service должны вызывать этот reader и SQL mutation вместе с повторными
проверками; сам reader не выдаёт review approval.

Go `ImportReviewDecisionService` теперь выполняет grant/revoke/status через API-only
SQL. Для grant он сам загружает текущий parsed import и versioned prepared
proposal, сверяет exact proposal/base/label-map refs и повторно читает parsed
перед SQL CAS; revoke использует сохранённый точный ключ. HTTP-маршруты
`/projects/{id}/interchange-jobs/{workflow}/prepared-reviews` подключены к
tenant/session auth и основному серверу; POST требует UUID Idempotency-Key,
GET читает точный status. Добавлены unit-сценарии валидации сервиса и HTTP
request ID. Исправлена неоднозначность имён PL/pgSQL переменных в grant/revoke
UPDATE. Всё это написано, но тесты/сборка/миграции не запускались. Открыты
выдача proposal/candidate списка для экрана, UI, scheduler live issuer и
continuation ожидания нового решения.

Миграция `02022` добавляет API-only каталог до 50 текущих подготовленных
DefectCandidates/DustCandidates на import workflow. SQL требует editor ACL,
текущий parsed/source и ready versioned artifact; сохранённый decision status
присоединяется только по точным proposal/base refs. Go list service читает
каждый JSON через двукратно проверяемый proposal reader, повторно проверяет
parsed import и каталог после IO. GET `prepared-reviews` подключён к серверу.
Добавлены unit/SQL ACL тесты, но они и миграция ещё не запускались. Следующий
разрыв — полноценный UI review и scheduler continuation/issuer; каталог
ограничен 50 proposal, превышение даёт ошибку, а не усечённый ответ.

Клиентский `interchange-prepared-review.ts` теперь строго читает proposal list,
проверяет exact image/base или dust frames/base, candidate IDs, строит
отдельный CAS grant/revoke request только из отображённого item и сверяет
receipt. `OwnerAPI` подключил GET/POST `prepared-reviews`; написаны unit-тесты
контракта. Экран визуальной проверки ещё не подключён. Тесты/typecheck не
запускались согласно этапу кодирования.

Клиентский экран `/interchange/prepared/[id]` теперь доступен из import job.
Он загружает список proposal и adopted entry, получает preview целевой версии,
для defects рисует bbox только при совпадении aspect ratio с canvas, для dust
показывает сенсорные координаты текстом (проекция crop на display без
подтверждённой геометрии не выполняется). Grant требует загруженное фото и
явный выбор кандидатов; поддержаны skip, revoke и повтор того же CAS body с
тем же Idempotency-Key при неопределённом ответе. Добавлены локализации
EN/RU/BG и DOM-unit сценарий; тесты/typecheck не запускались. Остаётся
физическая dust-preview геометрия и возможность UI revoke после остановки run:
текущий каталог показывает только running workflow, хотя SQL revoke/status
разрешает отзыв после run.

Миграция `02023` и API GET `prepared-reviews/saved` дают отдельный список
сохранённых grant/revoke после остановки import run. Он не читает архив,
prepared proposal или object storage, требует текущий editor ACL и точный
project/workflow. Экран загружает его независимо от live proposal-каталога и
строит revoke из сохранённых refs и epoch; добавлены unit/DOM/SQL ACL тесты.
Тесты/typecheck/миграции не запускались. Scheduler continuation/issuer и
физически точная dust-preview проекция остаются открытыми.

Миграция `02024` добавляет scheduler-only `v30_import_review_live_decision`:
текущий running import, parsed sources, entry/target/base, опубликованный
proposal и сохранённый granted epoch проверяются по точным refs. Go reader
возвращает типизированное `ErrImportReviewAwaitingDecision` при отсутствии
текущего grant. Defect/Dust codec теперь получает IDs и revision из этого
reader, архивные selection IDs заменяются, а Bound-слой повторно проверяет
физический proposal и candidate membership. Дополнительный operator authority,
если установлен, может только отклонить результат. Scheduler processor
считает ожидание успешной обработкой claim с SQL cooldown, новый grant
сбрасывает retry_after. Unit/SQL ACL сценарии написаны, но не запускались,
миграции не применялись. Остаточный риск: финальная activation TX пока не
проверяет, что grant epoch не был отозван после последнего codec-read;
необходим отдельный activation fence. Также нужно проверить полный запуск
scheduler с настоящими semantic ports и реальным import workflow.

Миграция `02025` добавляет финальный review activation fence. Внутренний
`ImportReviewWitnesses` собирает только успешно прочитанные scheduler-only
решения с epoch, IDs и точными refs; снимок передаётся в закрытую
`QualifiedImportActivation`. Новый SQL wrapper выполняет существующую
активацию и в той же TX сверяет все DefectCandidates/DustCandidates из
фактической `v2_task_inputs` closure с witness и заблокированным текущим
`granted` решением. Несовпадение бросает исключение и откатывает ready
transition. Scheduler лишён EXECUTE старой нефенсированной функции; startup
installation проверяет новый ACL. Написан unit-сценарий снимка и обновлён SQL
ACL/absent-scope сценарий. Миграция, тесты и сборка не запускались. До
закрытия V30 нужны current-build интеграция на базе и физическое E2E; отдельно
проверить, что все review proposal действительно присутствуют в input closure
с контрактами DefectCandidates/DustCandidates, особенно для inline dust Params.

После push `4f3522f` проверена структурная проекция inline dust Params:
`v2draft.dustRepairParamRefs` выдаёт дочерний `DustCandidates`, а
`ImportPreparedSchemaChildren` переносит эту роль в подготовленную closure.
Добавлен прямой unit-сценарий этой связи; он ещё не запускался. В SQL fence
явно задано разрешение совпадающих имён PL/pgSQL-переменных и столбцов.
Физическую полноту input closure и атомарный отзыв grant всё ещё нужно
подтвердить current-build интеграционным тестом после кодового этапа.

При отзыве или замене grant после последнего codec-read SQL activation fence
откатывает ready transition с точным `V30_IMPORT_REVIEW_ACTIVATION_GRANT`.
`SQLImportActivator` теперь переводит только этот `P0001` в
`ErrImportReviewAwaitingDecision`, который scheduler оставляет на cooldown и
повторяет после нового решения. Остальные SQL ошибки сохраняются без
маскировки. Unit-сценарий гонки и отрицательные случаи написаны, не запускались.

Клиент prepared import review теперь проверяет у dust-предложения полный
структурный минимум серверного `ReadDustProposal`: camera/sensor размеры,
границы crop, 5..64 уникальных кадров с exact base, предупреждение для
пустого списка, ограничения radius/observed/eligible/recurrence каждого
кандидата. Это не заменяет серверную проверку candidate digest и не
утверждает, что обычный processed preview геометрически совмещён с RAW
sensor: точная dust-preview проекция остаётся открытой. Написаны позитивный
и отрицательные unit-сценарии; они не запускались.

Process-owned V30 import operator loader теперь требует единственную ссылку
на inode верхней декларации, native/style и каждого denoise/upscale/stats/
person/measurement deployment файла на всех трёх stat-снимках. Добавлены
hardlink-отказы для верхней декларации, native и style загрузчиков;
`gofmt` и `git diff --check` выполнены, тесты/сборка не запускались.
Production semantic ports и целый import workflow по-прежнему не включены.

V30 dust review экран теперь показывает отдельную карту RAW crop: кандидаты
проецируются из `sensor_x/sensor_y/radius_sensor_px` с обрезкой маркера по
границе crop. Карта не притворяется оверлеем на processed preview; это
отдельно объяснено на EN/RU/BG. Положительный/отрицательный unit-сценарий
геометрии и DOM-сценарий присутствия карты написаны, не запускались.
Точный RAW→preview transform и физически совмещённый просмотр остаются
открытым продуктовым разрывом.

V30 saved review catalogue больше не обязан ломаться после 500 решений.
Forward-only `02026` добавляет editor-only keyset page по
`entry_id,target_version_id,kind,proposal_id` (501-й ряд только для
`next_after`). Go API проверяет канонический составной курсор и отдаёт
страницу до 500 решений; клиент запрашивает следующие страницы и проверяет
продвижение/дубликаты до возврата полного списка для revoke. Написаны Go,
SQL ACL и TS unit-сценарии, но тесты и миграция не запускались. Список
ограничен 50 страницами; для существенно более крупных workflows потребуется
UI-серверная виртуализация вместо полной загрузки.

Следующий предел был в каталоге prepared proposals: `02022` отдавал только
первые 51 и Go прекращал выдачу при >50. Forward-only `02027` добавляет
API-only keyset page по тому же точному составному курсору. Go перечитывает
parsed и metadata после физических proposal reads на каждой странице,
сверяет также 51-й peek, а API выдаёт `next_after`. Клиент собирает все
страницы с проверкой сортировки, прогресса и дубликатов. Go/SQL ACL/TS
сценарии написаны, миграция и тесты не запускались. Для очень больших
каталогов UI-виртуализация остаётся отдельной задачей.

Клиентский экран prepared import review теперь запрашивает только одну
страницу proposals и одну страницу saved decisions за раз. Отдельные
«предыдущая/следующая» навигации хранят только курсоры, а выбранный review
сбрасывается при переходе; после grant/revoke каталог начинается заново.
`OwnerAPI` сохраняет полные методы для внутренних вызывающих и добавляет
постраничные методы для UI. Локализации EN/RU/BG и DOM/unit сценарии
добавлены, но не запускались. Это ограничивает память экрана размером
страницы (50 proposals / 500 saved), не является виртуализацией DOM внутри
одной страницы и не доказывает отсутствие межстраничного дрейфа при
параллельных изменениях каталога.
