# Остаток реализации перед глубоким тестированием

Дата среза: 2026-10-14. Цель остаётся V20–V32 целиком. Этот документ задаёт порядок закрытия кода, а не принимает пакеты: последние миграции и написанные unit-сценарии не выполнялись в рамках текущего implementation-first этапа. Нормативные критерии — `07-execution-acceptance.md`; исходные пакеты — `work-packages.json`.

## Правила учёта

- Считать завершённым кодирование операции только при трассе `клиент/публичный API → авторизованный request → frozen plan/Params → долговечный admission и dispatch → worker → канонический result → transactional adoption → доступный review/Apply/download`, включая отказ, отмену, повтор и GC.
- Код, упомянутый в аудите 2026-10-07 как отсутствующий, повторно искать в текущем дереве. В частности, V30 delivery public create и mixed watermark scheduler constructor уже существуют; это не доказательство полного lifecycle.
- Для каждой новой миграции сохранить forward-only порядок, владельца/ACL/RLS и свежий/upgrade сценарии. До перехода к тестовой фазе сценарии написать, но не объявлять PASS и не применять миграции.
- Для каждой операции, зависящей от весов, камеры, ключей или измерений, отсутствие установленной квалификации даёт явное `unavailable`. Не заменять её synthetic fixture или конфигурационным SHA.
- Отмечать отдельно `coding_closed`, `configured`, `qualified`, `accepted`. Первое не подразумевает остальные три.

## Очередь реализации

### P0: основа первого MVP и всей системы

1. **Реестр трасс V20–V32.** Для каждой операции из `contracts/operations.json` и каждого HTTP route составить current-state строку: request/client, service, SQL, worker, result, adoption, UI, unit-сценарии, объектные роли, cleanup. Обновить старый `completion-audit-2026-10-07.json` только после проверки фактических связей. Блокер считается кодовым, если компонента нет или он не установлен; отсутствие запуска теста учитывать отдельно.
2. **V20/V21 инварианты.** Сверить capability/qualification и model locks для всех реально включённых операций; довести RLS, ACL, идемпотентность, quota, cancellation, output registry, GC и purge до новых V28–V31 артефактов. Проверить, что `photo_fn_owner` сохраняет DELETE только на разрешённом tenant-scoped allowlist для `finish_tenant_deletion`.
3. **Базовый маршрут V22/V25/V26/V30.** Замкнуть `import → session → editor/Apply → review → export → delivery/download` на поддерживаемых входах. Клиент должен показывать статус/ошибку/повтор и не обещать неподдерживаемую камеру/модель. Сохранить старые Apply v4, ICC, alpha, RAW и protected-pixel инварианты.
4. **Обычная V30 delivery.** Проверить установленную цепочку create, selected source, child admission, encoded output, assembly, CAS publish, read/download/revoke и recovery после сбоя. Разделить обычную, metadata и watermark ветки; общая сборка не должна публиковать неполный required bundle. Дописать unit-сценарии каждой ветки и ошибочного/повторного перехода.
5. **Минимальный V32 runtime.** Установить и связать startup-проверки, scheduler capacity, maintenance/reconciliation, безопасный restore и readiness для включённых функций. Не объявлять готовность по одному сформированному probe report.

### P1: обязательное для полного V20–V32, после P0 по зависимостям

6. **V23/V24.** Довести learning `fit → evaluate → safety review → promote/rollback` и consent epoch до runtime; camera/watch-folder capture — до restart/dedup, rotation и public QA. Эти возможности можно не открывать в первом MVP, но их код обязателен для полного плана.
7. **V27 semantic.** Подключить реальный downstream FAN/BiSeNet/skin result, долговечную родительскую оркестрацию, per-target review и Apply. Успешный receipt подготовительной задачи не равен успеху пользовательского workflow.
8. **V28/V29 операции.** Для open-eyes, glare, hair, clothing, reflection, relight, panorama, focus stack, negative и perspective последовательно проверить и дописать точную трассу из правила выше. Сопоставить каждый worker handler с регистрацией, admission, result adoption, GC и UI. Завершить real model/donor authority, а не только локальный compiler или синтетическое принятие.
9. **V30 interchange.** Подключить принятие canonical export result к долговечному рабочему пути по отдельному scheduler-контракту ниже; сохранить исходные actor/request identity, установить `BuildResultInstallerWithAuthority`, завершить run transition и replay. Существующий `ExportResultAdoptionService.AdoptCurrentExportResult` остаётся foreground-методом: сервис и builder есть, но production caller не найден. Для импорта закрыть parse → canonical ingest → review → final continuation/adoption; включить `FrozenInterchangeImportEvidence` в Params/closure согласно `15-execution-contracts.md`. Отдельно довести archive multipart, person-origin и metadata/watermark ветки до полного lifecycle.
10. **V31 provenance.** Установить реальный signer SDK/key/trust/TSA authority и процесс dispatch/adoption/public SignExport. Проверять итоговый подписанный файл после encode/watermark, rotation/revocation и privacy redaction; локальные CBOR/JUMBF и fake SDK тесты не равны production подписи.
11. **V32 полный lifecycle.** Завершить role matrix, restore, multi-bucket physical purge reconciliation, mixed workload/admission/fairness и acceptance-пробы для V20–V31. Production cutover требует независимо прочитанного физического evidence.

### Решение для фонового V30 export adoption

Текущий `ExportResultAdoptionService` использует `tenant.WithTenant(..., domain.Editor)` и `RecoverExportPending`, которые требуют текущую пользовательскую сессию. Она может истечь до завершения export worker. Поэтому этот сервис годится для авторизованного foreground восстановления, но не должен вызываться scheduler с выдуманным либо произвольно выбранным `session_id`. `photo_scheduler` не имеет права на `v30_export_adoption_root` и `adopt_v30_interchange_export`; простое расширение их EXECUTE также не является решением.

Кодовый путь реализации:

1. Добавить forward-only SQL claim для `photo_scheduler`, выбирающий только canonical succeeded `v2.interchange.export` без evidence/terminal completion; вернуть tenant/run/task/result/attempt/fence, исходные actor/key/request SHA из сохранённого pending ledger и точную frozen request. Claim имеет lease/token/cooldown, tenant-first порядок блокировок и не возвращает object URL или произвольный root из запроса.
2. Добавить scheduler-scoped read functions для frozen документов и exact output/result inventory. Они повторно проверяют active tenant/project, неизменный pending ledger, source snapshot, task result, output role/producer/attempt/fence и объектные версии. Не использовать user-session-gated `v25_lens_snapshot_ready` без специально определённой фоновой authority.
3. Установить результатный native validator из того же pinned deployment, что export creation, в scheduler bootstrap. Прочитать ZIP/sidecar/closure через точные versioned objects вне SQL-транзакции, затем recheck claim/fence и object identity.
4. В одной транзакции сохранить evidence, links/inbox/events и финализировать run либо передать его existing finalizer по явно зафиксированному барьеру. Повтор того же claim возвращает прежний результат; заменённый canonical result, отзыв доступа, отмена, истёкшая lease или mismatch SHA отказывают без частичной публикации.
5. Подключить processor к scheduler readiness/shutdown и написать unit-сценарии для каждого перехода, stale lease, logout до worker completion, unknown commit, replay и конфликтующей версии объекта. На тестовой фазе проверить real-role ACL и физические PostgreSQL/MinIO сценарии.

Начат кодовый путь: миграция `02034_v30_export_adoption_claim.sql` и `SQLExportAdoptionClaimStore` дают только приватный lease/discovery с исходным request и deterministic identity; они не читают объекты и не принимают результат. Пока нет scheduler-scoped reader, adoption transaction, processor и runtime installation, V30 export adoption остаётся незавершённым. Миграция и unit-сценарии написаны, но не выполнялись.

Миграция `02035_v30_export_adoption_claim_current.sql` добавляет read-only lease/result fence для повторной проверки перед следующим шагом; Go store и real-role negative fixture написаны. Этот predicate не заменяет snapshot/source, output и object-version authority. Ни 02034, ни 02035 не применялись; fixture не запускался.

Миграция `02036_v30_export_adoption_source_root.sql` добавляет отдельную проверку текущих actor/project прав и V25-эквивалентного snapshot/source inventory без зависимости от истёкшей HTTP-сессии, а также выдачу только точного первого canonical output с versioned object pin. Go reader повторяет claim до и после SQL lookup. Это пока root-only стадия: остальные outputs, transitive closure и финальный commit не установлены; 02036 и сценарии не запускались.

Миграция `02037_v30_export_adoption_outputs.sql` и `SQLSchedulerExportOutputCatalog` расширяют чтение на позиционные InterchangeResult/MappingReport/MappingShard/InterchangeArchive того же canonical result; каталог повторяет lease до и после lookup. Дальше нужно восстановить frozen документы из ledger, собрать `ExportResultReader`, установить input resolver для portable closure и сделать атомарный adoption. 02037 и сценарии не выполнялись.

Миграция `02038_v30_export_adoption_frozen.sql` и `SQLExportAdoptionClaimStore.FrozenExportAdoption` дают служебное чтение original frozen ledger и детерминированную реконструкцию через существующий `DecodeFrozenExportDocuments`. `exportIntentFromClaim` не создаёт фиктивную user session и не является самостоятельным разрешением. Следующие стадии — result reader, portable input closure, transactional adoption и scheduler installation. 02038 и сценарии не запускались.

`SchedulerExportResultReader` связывает frozen ledger, root и позиционный output catalog с существующим `ExportResultReader`, проверяя все JSON документы, SHA и версии до/после чтения. Его результат — только подготовленное структурное evidence, не adopted ticket. ZIP/portable closure, commit и runtime loop по-прежнему отсутствуют; сценарии не запускались.

`SchedulerExportArchiveReader` переиспользует bounded ZIP/manifest/XMP reader и повторно квалифицирует JSON результат после физического IO. Для `photoeditor_bundle` он требует отдельную scheduler-owned person-origin фабрику; session-bound фабрика API не подставляется. Пока такая фабрика не установлена, bundle путь закрыт. Portable source closure, commit и runtime loop ещё не реализованы; сценарии не запускались.

Миграция `02039_v30_export_adoption_person_origin_inputs.sql` и `SchedulerExportPersonOriginInputs` дают scheduler только точные `PersonOriginReceipt`, запечатанные во входах claimed task, с проверкой canonical result и lease до/после чтения. Код сверяет bucket, версию, размер, SHA и тип объекта; unit-сценарии написаны, не запускались. Это источник для будущей фабрики, но ещё не сама фабрика: в ней нужно переиспользовать проверку подписи, маски и текущего producer authority без пользовательской сессии, затем снова сверить object pins перед adoption.

Миграция `02040_v30_export_adoption_result_input.sql` и `SQLSchedulerExportInputCatalog` добавляют служебный каталог точных typed inputs для portable closure, также под canonical result и lease. Каталог не выдаёт документы вне запечатанного input inventory и не использует session-bound draft fallback. Остались физическая квалификация closure, scheduler person-origin producer lookup и общий transactional adoption; миграция и unit-сценарии не выполнялись.

Миграция `02041_v30_export_adoption_person_origin_current.sql`, `SchedulerExportPersonOriginCurrentChecker` и `SQLSchedulerExportPersonOriginInstallation` связывают подписанные receipts с текущим canonical mask.person producer под тем же claim. Фабрика сверяет frozen plan, запечатанный receipt inventory, подписи, MaskRef, Canvas SHA и повторно читает object pins после IO; архивный reader может принять её как `Origins`. Это не финальный adoption: portable closure, транзакционный commit и scheduler runtime ещё отсутствуют. Новая миграция и unit-сценарии написаны, но не выполнялись.

## Граница первого MVP

Обязательны пункты 1–5 на выбранной и явно опубликованной матрице камер/форматов/операций. V23 learning, V24 on-set, V27 semantic, расширенные V28/V29, interchange и V31 C2PA желательны после первого MVP, если продукт не обещает их пользователю. Любая включённая в интерфейсе или маркетинговом обещании функция сразу получает полный P0-критерий своей сквозной трассы. Такое разделение определяет релизную очередь, но не сокращает цель V20–V32.

## Переход к фазе тестирования

Перед переходом: по каждой включённой операции есть трасса и написанные unit-сценарии, все её миграции и installation paths присутствуют в репозитории, ни одна ветка не заканчивается только draft/receipt без результата, доступен явный отказ при отсутствии квалификации. Затем выполнить в порядке: fresh/upgrade PostgreSQL и роли; unit/typecheck; DB+MinIO/gateway integration; браузерные и native клиентские сценарии; фото-корпус и human quality; restore/GC/races; длительную смешанную нагрузку. Текущая аппаратная проверка — Metal с лимитом видеопамяти 10 ГБ. RTX 3060 квалифицировать отдельно; результаты Metal не переносить на него автоматически.
