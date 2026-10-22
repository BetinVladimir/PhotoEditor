# План завершения кодирования MVP до глубокой тестовой фазы

Дата среза: 2026-10-15. Основа: `mvp-release-scope-2026-10-15.md`, `implementation-remaining-2026-10-14.md`, `audit/trace-review-2026-10-15.md`, `audit/static-link-defects-2026-10-15.md`, `contracts/operations.json`, `contracts/endpoints.json`, текущее дерево кода поверх HEAD `1787516`. В рабочем дереве есть незакоммиченные изменения Metal/capability; они учитываются как **написанный, но не проверенный код**. Исторические PASS не перенесены. План не утверждает, что 109 трасс содержат 109 дефектов, и не выводит процент реализации из числа объявлений.

## Процент закрытия обновлённого плана

Текущая метрика: `100 × закрытые кодовые подпункты / 70`. Текущий срез на 2026-10-17: **25/70 = 35,714286%**. Это остаточный MVP-план до тестирования, а не процент всей системы и не оценка трудозатрат. Все 70 подпунктов перечислены с индивидуальными статусами и ссылками в [mvp-coding-progress.json](mvp-coding-progress.json); читаемый список — [mvp-coding-checklist.md](mvp-coding-checklist.md).

Подпункт засчитывается только при наличии конкретного кода и написанных сценариев, без требования запуска тестов: `coding_closed_unverified`. Наличие части реализации без завершённой трассы не засчитывается. Общие реализации считаются один раз. Детализация подпункта сохраняет его суммарный вес; изменение scope требует явной новой версии baseline, чтобы дроблением задач нельзя было повышать процент. Равные веса отражают количество плановых результатов, а не одинаковую сложность. Публиковать закрытые/всего, процент, изменение за шаг и оставшиеся открытые подпункты. Крупные блоки остаются контрольными воротами: **1/7**, их прежние проценты в журнале исторические.

Дополнение текущего дерева: для D-01 написаны [pre-Apply контракт и клиентская блокировка](semantic-preapply-preview-contract.md); provider/producer/Apply receipt gate ещё отсутствуют. Новый GET preview добавлен в нормативные HTTP-контракты: теперь 80 endpoint; прежний статический срез 79 endpoint остаётся историческим, новая строка требует отдельной трассы (совокупный нормативный объём — 30 операций и 80 endpoint).

## Граница

Сейчас выполняется **кодирование и написание unit-сценариев**, без запуска unit/integration/e2e, миграций, typecheck, Metal/CUDA benchmark и фото-квалификации. После выполнения приведённых ниже кодовых ворот начинается отдельная глубокая тестовая фаза и багфикс. Статусы вести раздельно: `code_gap`, `coding_closed_unverified`, `configured`, `qualified`, `accepted`. `coding_closed_unverified` не означает работоспособность или готовность к выпуску.

MVP охватывает поддержанный путь фотографа: import → session/collection → editor/Apply → review → export → ordinary delivery/download, сохранность оригиналов/цвета/RAW/alpha/защищённых областей, tenant/security/recovery и весь заявленный доступный AI V27–V29 с окончательным решением фотографа. Выпускать CUDA-only AI можно только после последующей реальной квалификации на выделенном RTX 3060 12 ГБ / RAM 32 ГБ / Ryzen 5600 при потолке проверки 10 000 000 000 байт. Apple Silicon / Metal остаётся поддерживаемым для квалифицированных MPS/CPU путей без фиксированного лимита 10 ГБ. Mac-only deployment автоматически скрывает/отключает CUDA-only операции; Mac-клиент может пользоваться удалённым квалифицированным RTX worker.

## Выполненный кодовый срез, который нельзя считать PASS

- Контрактный статический обход нашёл объявления для 30/30 операций и 79/79 HTTP endpoint. Содержимое downstream переходов остаётся `not_verified`.
- В рабочем дереве убран MPS-потолок 10 ГБ из relight analysis и Metal capacity profile/benchmark; capture на MPS требует явного измеренного VRAM budget. Соответствующие Go/Python unit-сценарии изменены, но не запускались.
- В editor UI операции с серверным capability теперь разрешаются только при `available=true`; capabilities обновляются каждые 10 секунд и сбрасываются при ошибке запроса. Код ещё требует unit-сценариев и позднее UI/real-worker проверки. Серверный `tenant_capabilities()` берёт доступность из живых квалифицированных регистраций; worker сохраняет отказ неподдерживаемому backend.
- Scheduler V30 interchange export factory подключена к executable; прежние записи об отсутствии bootstrap устарели. Расширенный interchange остаётся вне MVP и за закрытым gate.

## Порядок кодирования

| Приоритет и блок | Конкретная работа до тестирования | Кодовое условие завершения |
|---|---|---|
| P0.1 Semantic review D-01 | Частично написаны DTO, HTTP gate, клиентский validator и before/after/overlay display gate; production renderer/provider и transactional adoption ещё открыты. Для каждого proposed target доставить авторизованные pinned original/candidate preview, affected/protected overlay и quality/limitations evidence. Связать preview с точным `plan_sha`, target, version и revision; approve блокировать до загрузки и совпадения. Reject оставить доступным. Добавить unit-сценарии stale/expired/revoked URL, чужой target, повторное решение, изменение revision. | UI не даёт принять невиденный или устаревший кандидат; Apply несёт тот же fenced plan/revision. |
| P0.2 Capability/Metal/CUDA | Зафиксировать по каждой операции `supported_backends`, model lock, worker installation и UI gate. Закрыть оставшиеся UI пути, где отсутствующая/ошибочная capability может запустить запрос, включая вложенные controls; написать unit-сценарии загрузки, потери worker, Mac-only и Mac+remote RTX. Сверить Metal policy/allocator/heartbeat и RTX-only 10 ГБ test cap без скрытых MPS ceilings. | Для CUDA-only на Mac-only никакой клиентский action не активен; сервер/worker отказывает независимо от UI; положительный remote-RTX путь не блокируется по ОС клиента. Наличие кода без физической qualification остаётся `coding_closed_unverified`. |
| P0.3 Полный инвентарь экранов D-03 | В рабочем дереве `apps/photographer/src/routes.ts` дополнен до 40/40 файловых экранов, написан unit-сценарий сравнения с деревом Expo (не запущен). Далее для каждой новой записи проверить точные API, роль, capability и навигационные переходы; три старых API-ссылки на delivery в `/runs/[id]` отсутствуют в нормативных endpoint JSON и требуют согласования контракта с реализацией. Новые экраны должны попадать в карту автоматически через проверку. | Карта отражает все текущие экраны и не расходится при добавлении следующего; это подготовка охвата, а не доказательство работы навигации. |
| P0.4 Трассы базового пути | Для import, session, collection, editor, Apply, review, export, delivery и download заполнить реальные переходы request/client → auth/tenant → frozen plan → admission/dispatch → worker → canonical result → atomic adoption → UI decision/download → cancel/retry/cleanup. Для найденного отсутствующего звена исправить код и написать unit-сценарий; сохранять Apply v4/ICC/RAW/alpha/protected pixels. | У каждого включённого базового сценария нет `code_gap`, есть ссылки на конкретный код и unit-сценарии; ordinary delivery не может публиковать неполный required bundle. |
| P0.5 AI V27–V29 | Так же пройти semantic transfer, open-eyes, glare, hair, clothing, reflection, relight, panorama, focus stack, negative, perspective и остальные заявленные AI-подсказки. Для каждого — target/donor/model authority, preview evidence, решение фотографа, Apply/отказ, отказ при unsupported/unavailable, replay/cancel/GC. Не считать synthetic fixture или SHA модели квалификацией. | Каждая заявленная операция имеет закрытую кодовую трассу и unit-сценарии; неподдерживаемая ветка честно выключена, без фиктивного результата и автоматического Apply. |
| P0.6 Tenant и эксплуатационный минимум | Статически довести ACL/RLS, quota, idempotency, leases/fences, output registry, object GC, физический purge, backup/restore, readiness и startup policy для включённых MVP веток. Новая миграция — только forward-only с владельцем, EXECUTE/allowlist, fresh/upgrade/denied-role сценариями; `photo_fn_owner` DELETE только на согласованном allowlist для `finish_tenant_deletion`. | Нет известного обхода tenant/role/retention; кодовые startup и recovery пути установлены, сценарии написаны. Реальные роли/миграции/restore проверяются в следующей фазе. |
| P0.7 Реестр и freeze | Для 30 операций заполнить 8 ворот, для текущих 80 endpoint — 6 ворот в `audit/trace-inventory.json` с путями/символами и статусом. MVP-строки не оставлять `not_verified`; вне MVP явно указывать отключённую capability. Пересчитать объём/процент лишь по верифицированным воротам, а не по количеству файлов. | У всех включённых MVP строк `coding_closed_unverified`, у отложенных есть доказанный gate; D-01/D-03 закрыты исходниками и unit-сценариями. |

Порядок P0.1–P0.3 уменьшает риск ложного Apply и неполного охвата; P0.4–P0.6 закрывают функциональные цепочки; P0.7 фиксирует доказательства и запрещает переход по одной общей отметке `PASS`. Общие нижние слои проверять один раз с точными caller/consumer ссылками, затем ссылаться на них из каждой строки.

## Что не блокирует начало тестирования MVP

V23 `/learning/[id]` и `showIdentityReport` (D-02), персональное обучение и полная identity activation; V24 прямой hardware tethering; расширенный V30 interchange, сложная доставка/автоматическая отправка; V31 публичная C2PA-подпись; многоузловой V32. Их код сохраняется за закрытыми capability/feature gates, и UI не обещает их как готовые. Если такую функцию решено показать в MVP, она немедленно переходит в P0 с полной трассой и unit-сценариями.

## Вход в следующую фазу и выпуск

**Начало глубокой тестовой фазы:** D-01 и D-03 исправлены; P0.2–P0.6 закрыты кодом; у каждого включённого MVP workflow есть трасса и написанные unit-сценарии; все невключённые ветки fail closed; миграции имеют порядок и сценарии, но ещё не заявлены применёнными. Открытый `not_verified` или `code_gap` на включённом пути запрещает переход.

**После перехода:** применять fresh/upgrade миграции и real-role отрицательные проверки; запускать unit, integration, e2e, браузер/native, Metal и RTX 3060; измерять 10 ГБ потолок только на RTX; выполнять реальный фото-корпус и photographer review, затем багфикс и полный повтор. Release gate D-04 — положительная CUDA/model/quality/Apply qualification каждого объявленного CUDA-only пути на выделенном RTX. Никакой кодовый статус или исторический Metal PASS этот gate не заменяет.

### Дополнение P0.2: вложенные controls

В `apps/photographer/src/editor-panels.tsx` поля включённых geometry/composition/background теперь блокируются при отсутствующей или потерянной capability; aspect ratio также требует Apply v4 и всех трёх composition/crop capabilities. Обработчики числового ввода, выбора и outpaint prompt дополнительно проверяют блокировку. Написаны `editor-panels.test.tsx` для отсутствующей capability, потери worker, готового удалённого CUDA и отсутствующего Apply v4. Сценарии не запускались. Это частичное закрытие P0.2; показатель остаётся **0/7 = 0%**.

Mask prompt/rasterize и generation теперь проверяют live capability до сохранения и повторно после асинхронного save перед admission; при смене route поздний generation result игнорируется. `operation-availability.test.ts` описывает отсутствующую, противоречивую, дублированную и remote CUDA capability. Тесты не запускались; P0.2 остаётся открытым до полного обхода остальных caller.

P0.2: `editor-local-portrait.tsx` передаёт в подтверждённую и восстановленную форму блокировку точной выбранной операции hair/clothing; готовность другой операции больше не разрешает отправку недоступной. Добавлен `editor-local-portrait.test.tsx` с потерей/возвратом hair при доступном clothing. Не запускался. Полный P0.2 не закрыт.

P0.2: вспомогательные relight actions `depth.predict` и `mask.person` теперь имеют независимые gates из live server capabilities; отсутствие собственной capability блокирует action даже при доступном `image.relight`. Unit-сценарий независимой потери/возврата availability добавлен в `editor-relight.test.tsx`, не запускался. Серверный `capability.Service.Get` подтверждён исходниками как источник `ready`/`available_backends`; это статическая сверка, не runtime qualification.

P0.2: `local-portrait-controls`, `relight-controls`, `relight-analysis-action` повторно проверяют актуальную блокировку непосредственно в callback сетевой отправки после journal IO. Local portrait также проверяет её после prepare. Unit-сценарий delayed journal / capability loss / exact body-key retry написан в `relight-controls.test.tsx`; не запускался. Показатель остаётся 0/7.

P0.2: повторная live-блокировка в сетевом callback добавлена также для reflection/open-eyes/glare donor/negative/perspective; первые три проверяют её после prepare. В `editor-reflection.dom.test.tsx` добавлен сценарий потери capability при незавершённом prepare, запрещающий submit/navigation. Не запускался. Полный аудит capability/worker/model installation остаётся открытым; 0/7.

P0.2: `use-editor-capabilities.ts` очищает availability при смене client/source scope и background/inactive, игнорирует поздние ответы предыдущего scope/foreground, refresh при возвращении и каждые 10 секунд. Editor использует актуальный ref для admission guards. Написаны unit-сценарии background/late response/foreground, смены scope и poll failure; не запускались. 0/7.

P0.2: editor и typed panels используют общий `operationAvailable`: ровно одна запись, ready/available и непустой supported available backend. Неполный runtime JSON блокируется без исключения рендера. Добавлены unit-сценарии malformed metadata и available=true с пустым backend. Не запускались; 0/7.

P0.2: серверный `classifyBackends` больше не принимает unsupported/пустые/повторяющиеся available backend как ready; reason `invalid_backend_evidence`. Написаны Go-сценарии CUDA-only с MPS, квалифицированного Metal, корректного CUDA и противоречивого evidence. SQL admission и физическая qualification остаются самостоятельными gates. Тесты не запускались; 0/7.

P0.2: capabilities HTTP refresh ограничен 8 секундами, передаёт AbortSignal и отменяется при background/scope/unmount. Timeout сбрасывает прежнюю доступность и fence отклоняет поздний success; очередной polling может восстановить её. Unit-сценарий hung-refresh/abort/late-success/recovery написан, не запускался. 0/7.

P0.2: `OwnerAPI.capabilities` декодирует/копирует JSON через `readCapabilities` перед UI; nullable model/reason нормализуются, дубли операций/backend и противоречивые status/availability отклоняются. Написаны сценарии malformed response, ownership, CUDA-only unavailable и qualified MPS; не запускались. Статический поиск фиксированных 10 ГБ не выявил нового MPS ceiling в просмотренных production matches: learning guards явно CUDA-only, archive/download — byte-volume limits. Полный P0.2 открыт; 0/7.

P0.3: добавлены ранее отсутствовавшие контракты GET delivery by-run (viewer/200), GET items (viewer/200), POST download-intent (editor/201) и DTO slots/intent. Route param `{run}` приведён к серверному. Нормативный объём теперь **83 endpoint**, historical 79/80 snapshots остаются историческими. Написан unit-сценарий точных roles/status. Query items: revision обязательна; limit 1..200 (default 100); after_ordinal/after_preset передаются вместе, порядок ordinal/preset. DTO JSON schema описывает форму; relational state/cursor constraints дополнительно проверяются серверным `ValidItemsResource`. Полный инвентарь D-03 ещё открыт, 0/7.

P0.3: нормативный реестр дополнен GET delivery list/by-workflow/inspection (viewer/200), schemas DeliverySummaryPage/DeliveryInspection. Текущий объём **86 endpoint**. Route `/deliveries` ссылается на list; detail включает items/inspection/download; run включает by-workflow. Unit-сценарий roles/status дополнен. List требует project_id и использует limit 1..100/cursor; inspection требует revision и проверяет весь manifest/slots до публикации. DTO schema не заменяет серверные relational проверки. Тесты не запускались; D-03 остаётся открыт, 0/7.

P0.3: replacement review screens больше не имеют пустой API-карты: указаны GET authoritative replacement scope и v2-workflow status, сверенные с OwnerAPI и replacement-result readers. Unit-сценарий включения этих связей написан. Карты пока частичные: nested proposal/preview/quality/Apply и role/capability требуют следующего прохода. Это не закрывает D-03, 0/7.

P0.3/P0.5: при обходе perspective replacement найден pending без автоматического обновления (multi уже обновлялся). Добавлен 2-секундный polling через повторный authoritative scope/result read; прекращается при ошибке, выходе из pending, Apply или unmount. Написаны DOM-сценарии повторного scope read, остановки при отказе и unmount. Не запускались. Nested API карта остаётся неполной; 0/7.

P0.5: perspective replacement cancelling остаётся pending для polling; failed/cancelled/completed_with_errors возвращают terminal без proposal IO. UI показывает локализованное terminal/cancelling состояние и не монтирует Apply для него. Unknown workflow всё ещё fail closed. Unit-сценарии обновлены/добавлены, не запускались. 0/7.

P0.3: perspective proposal/preview/quality/Apply внесены в нормативные HTTP contracts с DTO формы и roles viewer/editor, status 200; replacement route включает их. Текущий объём **90 endpoint**. Unit contract scenario написан. JSON schema формы не заменяет checks конкретного scope, hash, geometry, signed URL expiry, quality authority и transactional adoption в существующих validators/services. Полный D-03 открыт, 0/7.

P0.3: panorama/focus-stack proposal/preview/quality/Apply включены в нормативные endpoint (8 маршрутов), Multi DTO и route map. Sources ограничены 20 panorama/30 focus-stack как в frozen intent. Текущий нормативный объём **98 endpoint**. Unit route/role scenario написан, не запускался. Relational source/registration equality, signed URL и quality/adoption остаются ответственностью validators/services. Полный D-03 открыт; 0/7.

P0.5: multi/perspective replacement review использует `useReviewForeground`: inactive/background размонтирует Scope, aborts reads/polling/media и UI actions; foreground remount читает актуальный scope/result заново. Durable Apply journals не удаляются. Написан hook unit scenario, DOM mocks актуализированы; тесты не запускались. 0/7.

P0.5: perspective replacement теперь монтирует durable Apply recovery с base version/producer из authoritative scope. Recovery отклоняет журнал чужого producer и синхронно скрывает entry от другого API/scope; старый finally не снимает новую блокировку. Написан producer-mismatch DOM scenario; не запускался. P0.5 остаётся открыт; 0/7.

P0.5: PerspectiveApplyRecovery проверяет collection scope сохранённого selection аналогично MultiApplyRecovery. Editor передаёт текущую collection (либо standalone), replacement review — standalone. Несовпадающий журнал сохраняется, resume не показывается. Написан foreign-collection DOM scenario, не запускался. 0/7.

P0.5: perspective replacement refresh больше не сбрасывает подтверждённый Apply в текущем scope; saved version отображается независимо от очередного result read, preview/quality/recovery action после успеха скрыты. Смена scope/foreground использует remount и durable recovery. Написан refresh-after-recovery DOM scenario; не запускался. 0/7.

P0.5: multi attempt/replacement readers трактуют completed_with_errors как terminal failure без proposal IO; UI отличает cancellation от failure. Исправлены противоречивые unit expectations failed/cancelled в replacement reader, добавлены cancelling/unknown проверки. Тесты не запускались; это статическое обнаружение рассогласования, не PASS. 0/7.

P0.3: пустые API entries теперь имеют apiInventory local/pending с причиной. Interchange format/import-format подтверждены исходниками как локальные selectors; prepare/prepared/masks/sign/root требуют отдельных дочерних трасс и помечены pending. Unit invariant запрещает анонимный пустой список. Это не засчитывает pending как завершённую карту. Тесты не запускались; 0/7.

P0.3: exports/sign API trace заполнена me/signing-policies/provenance. Добавлен только отсутствующий policy-list endpoint (viewer/200); существующий provenance (editor/202) уточнён ExportSigningTicket. Нормативный объём **99 endpoint**. V31 остаётся post-MVP; API запись не означает qualification C2PA. Unit route scenario написан, не запускался. 0/7.

P0.3: interchange/prepare API trace заполнена me + export snapshots (editor/201); DTO формы и unit route scenario добавлены. Нормативный объём **100 endpoint**. Sequential ordinal/unique versions/source-working equality/CAS/digest/idempotency проверяются существующими validators/services сверх формы JSON schema. Расширенный V30 остаётся отложенным. Тесты не запускались; 0/7.

P0.3: root screen подтверждён как локальный Redirect; AuthProvider restore относится к shared layout trace. При сверке signing исправлен handler guard: новый запрос требует selected ID в текущих policies, saved ambiguous attempt сохраняет immutable retry. DOM scenario удалённой после refresh policy написан, не запускался. V31 остаётся отложенным; 0/7.

P0.3/P0.6: prepared interchange и person mask review теперь remount при замене client identity даже на том же origin. Старый Scope aborts reads/media/mutation и очищает UI choices. Написан old-account read cancellation DOM scenario; не запускался. API карты этих двух экранов пока pending, 0/7.

### P0.3: дочерние запросы interchange review

В `routes.ts` заполнены API для prepared review и person-mask review: страницы предложений и сохранённых решений, grant/revoke, entries/candidates, PNG preview и photo media links. Написан unit-сценарий охвата обоих экранов. Пустых pending API-инвентарей для этих экранов больше нет; нормативные endpoint DTO/roles и остальные навигационные связи ещё требуют сверки. Тесты не запускались. Полностью закрыто **0/7 = 0%**.

### P0.3: editor authority импортированных масок

В нормативные контракты добавлены GET entries и GET confirmations с bounded DTO и ролью editor: сервисы используют `tenant.WithTenant(..., domain.Editor, ...)`. Написан unit-сценарий роли. Текущее число endpoint: **102**. Остальные prepared/mask candidate/decision контракты остаются открыты. Тесты не запускались; закрыто **0/7 = 0%**.

### P0.3/P0.6: byte limits person-mask grant

Статическая сверка `canonicalImportDecisionDocument` и клиента выявила расхождение UTF-8 bytes / JS string length. В `freezeImportPersonMaskDecision` добавлена проверка UTF-8 размера parsed/recipe/override/mask JCS с прежними лимитами 8/1/8/1 MiB. Написан сценарий кириллицы, превышающей byte limit при допустимой длине строки. Это предварительная проверка размера; каноничность и связи документов проверяет сервер. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.3: mask candidate и decision contracts

Добавлены GET candidate page и POST confirmation: editor, 200, раздельные grant/revoke shapes, точный receipt workflow/entry/status/epoch. JCS schema документирует дополнительную серверную проверку UTF-8 размера, каноничности и artifact bindings. Написан unit-сценарий endpoint DTO/role. Нормативных endpoint **104**. PNG binary preview и prepared review контракты ещё открыты. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.3/P0.6: PNG response boundary

HTTP mask preview раньше принимал даже одну восьмибайтовую PNG signature. Теперь `png.DecodeConfig` проверяет заголовок и размеры 1..1024; добавлены `private, no-store` и `nosniff`. Unit fixture заменена настоящим PNG, добавлены сценарии signature-only и cache headers. Полное декодирование по-прежнему делает клиент перед разрешением решения; renderer проверяет source и candidate повторно. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.3: binary mask preview contract

PNG маршрут внесён в нормативный endpoint JSON как raw binary `image/png`, editor, 200, максимум 8 MiB. DTO явно описывает размеры 1..1024, повторную серверную проверку candidate/object и клиентское декодирование перед grant. Добавлен unit-сценарий binary route contract. Нормативных endpoint **105**. Prepared review контракты остаются открыты; OpenAPI projection требует сверки. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: selection during prepared decision

Устранён путь, где выбор другой proposal прерывал уже отправленное решение и очищал retry attempt, хотя сервер мог его применить. Пока mutation в полёте, выбор заблокирован кнопкой и синхронным ref guard. Написан сценарий сохранения AbortSignal и отсутствия второго media read. Durable recovery после перезапуска остаётся отдельным открытым пунктом. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: ambiguous prepared decision fence

После ошибки сети pending attempt больше нельзя заменить другим fingerprint, очистить выбором/пагинацией/refresh. Повтор сохраняет body/key; только подтверждённый ответ очищает attempt и обновляет страницы. Написан exact retry сценарий saved revoke. Durable recovery после перезапуска ещё открыто. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: prepared photo expiry

Готовность preview сбрасывается по expires_at; admission дополнительно проверяет время синхронно, поздний onLoad не оживляет истёкший proof. Повтор уже отправленного immutable attempt не требует нового решения фотографа. Написан сценарий истечения между загрузкой и click. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: prepared photo identity fence

Фото загружается с ключом selection+URL; поздние load/error от предыдущего selection не меняют готовность нового. Admission нового решения сверяет photoScope с точной proposal identity. Написан сценарий смены предложения и повторной загрузки фото. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: prepared lifecycle

Prepared review использует foreground lifecycle: в фоне media скрыта, proof сброшен, новые decision/revoke запрещены; foreground повторно запрашивает media и требует load. Scoped component не размонтируется, поэтому unresolved immutable attempt не теряется. Написан lifecycle сценарий. Durable restart recovery ещё открыто. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.3: prepared decision contract

POST prepared review и receipt внесены в нормативный контракт: editor, 200, ветки grant/revoke × defects/dust, label map только defects, expected_epoch, bounded candidate IDs. Дополнительные service invariants и Idempotency-Key описаны явно. Unit-сценарий написан. Нормативных endpoint **106**; GET pages и OpenAPI ещё открыты. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: prepared receipt epoch

SQL grant/revoke увеличивает expected_epoch ровно на 1; idempotent replay возвращает ту же epoch. Service, HTTP boundary и клиент теперь требуют exact expected_epoch+1, исключая чужой старый/перескочивший receipt при совпавшей proposal. Написан unit-сценарий epochs. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: server receipt scenarios

HTTP boundary prepared decision дополнительно отклоняет epoch вне клиентского safe диапазона и неизвестный action. Написаны server-сценарии stale/equal/skipped/unsafe epoch и корректного expected+1. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: saved prepared selections

Saved decoder теперь требует сортированные уникальные непересекающиеся candidate IDs и пустые выборы для reject/skip. Возвращает собственные копии refs/arrays вместо исходного mutable JSON. Написаны сценарии противоречивых выборов и изменения исходного DTO после чтения. Saved page contract ещё открыт. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.3: saved prepared page contract

GET saved page внесён в нормативный контракт: SQL editor authority, до 500 строк, optional next_after с точной курсорной семантикой, kind-specific label_map и выборы. Написан unit-сценарий endpoint. Нормативных endpoint **107**. Live page/OpenAPI ещё открыты. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.3: live prepared page contract

GET live page внесён в контракт: editor, 50 rows, cursor и decision epoch/status. Документы используют существующие generated defect/dust schemas с локализованными refs. Дополнительные relational/source invariants описаны отдельно. Написан unit-сценарий endpoint. Нормативных endpoint **108**. OpenAPI projection ещё требует сверки. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.3: incremental OpenAPI projection

Добавлен `scripts/sync-v2-openapi.py`: обновляет operation/role/package/request/response из текущего endpoint реестра, сохраняет существующие query и специализированные capability routes, поддерживает binary media/max bytes. Для prepared pages добавляет after, для decision UUID Idempotency-Key. OpenAPI обновлён этим генератором без запуска общего build-spec, который пересоздаёт остальные контракты из исторических списков. Написаны unit-сценарии binary/idempotence/retry-key. Полная семантика path/query новых операций и generator source parity ещё требуют аудита. Генерация артефакта выполнена; тесты не запускались. Закрыто **0/7 = 0%**.

### P0.3: interchange OpenAPI parameters

Interchange review path IDs теперь UUID по серверному routeID; prepared after имеет cursor shape, existing Idempotency-Key нормализуется к required UUID без дублей. OpenAPI повторно сгенерирован; unit-сценарии написаны. Остальные новые маршруты ещё требуют сверки параметров. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.3: replacement preview parameters

Perspective/panorama/focus-stack proposal routes в OpenAPI теперь имеют UUID t/id/version/task и точный preview variant allowlist preview1600/preview2560 по server handlers. Projection обновлена, unit-сценарий написан. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.3: replacement Apply retry contract

Perspective/multi Apply handlers не читают Idempotency-Key: используют exact decision body result/root/quality. OpenAPI явно описывает immutable replay и убирает возможный исторический обязательный header на этих маршрутах. Генератор/артефакт обновлены, unit-сценарий написан. Это не утверждение проверенной runtime idempotency. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: imported mask lifecycle

В фоне импортированные photo/mask скрыты, scope proof инвалидируется, grant/revoke запрещены. Foreground повторно получает media и требует обе загрузки. Во время mutation смена entry/candidate больше не прерывает отправленный запрос. Lifecycle unit-сценарий расширен. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: imported mask synchronous expiry

Visible preview хранит expiresAt; grant/revoke проверяет его непосредственно перед отправкой независимо от таймера, load не оживляет expired proof. Media refresh больше не очищает подтверждённое decision state. Unit-сценарий expiry-before-timer добавлен. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: mask display generation

Каждый показ mask preview получает уникальный proofID; load/error и admission проверяют текущую generation. Это отличает повторный показ того же candidate после foreground от предыдущего изображения. Images keyed по proofID, lifecycle unit-сценарий расширен поздними событиями старого фото. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: prepared display generation

Уникальный photoProof отделяет повторный показ того же selection/URL от старого поколения. Images keyed по proof, load/error проверяют текущую generation. Lifecycle unit-сценарий расширен поздними событиями предыдущего фото. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: durable prepared decision decoder

Добавлен строгий decoder для будущего durable journal: exact fields, UUID/refs, различающиеся artifacts, action/kind/epoch, сортированные непересекающиеся IDs, собственная immutable body copy. Написаны сценарии повреждённой journal body. Сам storage и UI recovery ещё не подключены; durable recovery не считается реализованным. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: prepared durable store

Добавлен PreparedReviewAttemptStore: scope origin/user/tenant/project/workflow, сериализация и межконтекстный journal lock, immutable body/key conflict, очистка только по exact acknowledged receipt. Logout очищает записи и меняет generation, запрещая старым writers восстановить данные. Написаны restore/conflict/receipt/logout unit-сценарии. Screen write-before-send/recovery ещё не подключены. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: prepared journal screen integration

Screen получает principal через me, восстанавливает journal до admission, пишет immutable attempt до HTTP, очищает по receipt. Локализованная retry-кнопка восстанавливает запрос независимо от live catalogue и выбранного media. После journal IO повторно проверяются abort/foreground. Написан recovery/write-before-send unit-сценарий. GET me добавлен в route inventory. Это кодовая интеграция без runtime доказательства; broader P0.6 остаётся открытым. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: journal load failure recovery

Ошибка загрузки prepared journal теперь имеет локализованный reload, не очищающий сохранённую операцию. Добавлены unit-сценарии повторного чтения после storage failure и размонтирования во время write (HTTP/clear запрещены). Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: full-size prepared journal

Исправлен inherited 256 KiB journal limit, который блокировал допустимые 4096 SHA-256 IDs (~268 KiB без metadata). Лимит 1 MiB UTF-8 соответствует HTTP decision budget и покрывает максимальную валидную body вместе с journal envelope. Написаны maximum-candidates и oversized-corruption сценарии. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: strict enum restoration

Устранено String-coercion принятие enum arrays в restored prepared decision и mask status. Поля проверяются exact string comparisons; написаны corruption unit-сценарии. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: API prepared admission snapshot

OwnerAPI теперь валидирует decision и создаёт собственную snapshot до HTTP; receipt сверяется с этой snapshot, поэтому изменение объекта вызывающим кодом не подменяет binding. Написан сценарий malformed enum до HTTP и caller mutation во время запроса. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: mask grant SQL variable fence

В единственном grant definition 02004 обнаружено совпадение column/variable mask_id, включая `mask_id=mask_id`. Forward-only 02168 заменяет функцию, используя v_mask_id в точных candidate predicates и VALUES; signature/owner/ACL сохраняются. Написан static migration unit-сценарий. Миграция не применялась, тесты не запускались. Дополнительный разрыв: delayed grant после чужого revoke нельзя автоматически replay без отдельной CAS/request fence; durable mask recovery пока не включать. Закрыто **0/7 = 0%**.

### P0.6: server mask status boundary

Выдача статусов масок проверяет safe integer epochs и уникальность exact scope tuple перед JSON; malformed evidence отклоняется на серверной границе. Написаны unit-сценарии epochs/duplicates/status/nil target. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: receipt transport ownership

Mask decision HTTP boundary отклоняет epochs за пределом safe integer; prepared receipt decoder возвращает собственную snapshot вместо mutable transport object. Написаны unsafe epoch и mutation-after-decode unit-сценарии. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: readable vs writable epoch

Prepared receipt/status может иметь MAX_SAFE_INTEGER, но expected_epoch следующей операции должен быть меньше. Клиент/HTTP/DTO согласованы: последний safe receipt принимается, дальнейший grant/revoke отклоняется до отправки. Написан boundary unit-сценарий. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: mask entries response boundary

Manifest inventory artifact_id подтверждён codec. HTTP entries теперь проверяет лимит 5000, уникальность entry_id, ненулевые target/asset и external_id 1..240 bytes до выдачи JSON. Написаны malformed/duplicate/oversized unit-сценарии. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: mask candidates response boundary

Candidate HTTP page ограничивает 5000, проверяет external_id, exact entry и уникальность override/mask/target tuple. Fixture обновлена валидным external_id; написаны duplicate/oversize/foreign entry unit-сценарии. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: external ID byte parity

Существующий mask entries service ограничивает external_id 240 UTF-8 bytes. Client entries/candidates теперь используют тот же byte budget вместо одной JS length. Написан сценарий 120/121 кириллических символов. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.6: complete PNG decode

После bounded DecodeConfig HTTP preview выполняет png.Decode: валидный IHDR без pixel payload не выдаётся как PNG. Лимит 8 MiB и 1024×1024 проверяется до allocation. Написан truncated-after-IHDR unit-сценарий. Тесты не запускались. Закрыто **0/7 = 0%**.

### P0.3: registry projection unit coverage

Написан unit-файл, сравнивающий весь клиентский endpoint registry с нормативным, OpenAPI operation/role/success DTO/media и разрешимость всех local JSON pointers, включая вложенные defect/dust schemas. Это будущая проверка связности, не текущий PASS: тесты не запускались. Закрыто **0/7 = 0%**.

### Уточнение контракта доставки

P0.3: OpenAPI delivery UUID paths, required revision, list limits/cursor and paired item pagination follow HTTP validators; unit scenario authored, not executed. No block closed.
Метрика закрытых блоков остаётся 0/7 (0% остаточного плана кодирования).

### Ключи повторов доставки

P0.3: delivery create/publish/download-intent OpenAPI now requires Idempotency-Key with exact server printable ASCII length 1..128, replacing incorrect UUID restriction. Replay/idempotent projection unit scenarios authored, not run; block remains open.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Обработчик скачивания доставки

P0.3: extracted delivery download-intent HTTP handler with key/nonzero package admission and cancellation checks before service and before bearer response; unit scenarios authored for success/replay, cancellation and invalid admission, not executed.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Проверка ответа download-intent

P0.3: download-intent HTTP output rejects malformed/noncanonical 32-byte bearer tokens and expired receipts with 503 before replay header or token disclosure. Unit scenarios authored, not run.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Общая валидация выбора скачивания

P0.3: shared DownloadIntentRequest.Validate now enforces manifest versus exact slot selection in both HTTP admission and service, ordinal 0..4999 and nonzero preset. Boundary and invalid-selection-before-service unit scenarios authored, not executed.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Срок действия и отмена скачивания

P0.3: server download-intent response lifetime capped at 60 seconds to match client; authored server overlong receipt and client cancellation-after-issue/after-redeem tests. No tests executed; block remains open.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Восстановление публикации по точной ревизии

P0.3: delivery refresh validates published package against saved expected_revision+1 before clearing in-memory publication attempt, preserving ambiguous intent when another revision is returned. DOM scenario authored, not executed.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Активность приложения и доставка

P0.3: delivery screen hides actions in background, rejects background callback admission, fences URL opening and post-journal publication submission, clears inspection confirmation and refreshes authority on foreground return. Durable attempts retained. Lifecycle DOM scenario authored, not run.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Поздний ответ инспекции доставки

P0.3: background transition aborts current delivery transport, retains publication attempt, clears inspection authority. Late inspection response cannot restore proof after foreground reload. DOM race scenario authored, not run.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Активность списка доставок

P0.3: delivery list now clears metadata/cursors and aborts pending reads in background, blocks background pagination/navigation, reloads project authority on foreground return. Lifecycle DOM scenario authored, not run.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Фоновая отмена создания доставки

P0.3: delivery creation hides background actions, aborts transport but retains exact immutable attempt, rechecks foreground after storage and before navigation, refreshes presets/signing policies on return. DOM cancellation/exact-retry scenario authored, not run.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Сохранение запроса перед фоновой отменой

P0.3: authored delivery-create journal-write/background race scenario ensuring no HTTP mutation after durable write completes in background; corrected journal mock scope API and retry readiness in lifecycle scenarios. Tests not executed.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Строгие привязки semantic preview

P0.1: semantic pre-Apply preview query now uses strict ParseQuery rejecting malformed escapes rather than silently dropping them; checks cancellation before/after provider and sets private no-store. Binding query unit scenarios authored, not run. Production renderer/provider remains missing; block open.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Целостность транспорта semantic preview

P0.1: preview transport forbids document/media artifact aliases on server/client; client quality enum no longer accepts array coercion, limitation character count matches Go Unicode runes, read rejects cancelled transport responses. Unit scenarios authored, not run; canonical producer/SQL adoption still open.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Общая численная проверка exposure

P0.1: extracted verify_exposure_pixels from Apply report evaluator as shared authority-neutral numerical verifier; Apply wrapper invokes it and retains source-bound ImageReport. Exact replay, protected bit equality, ICC, memory and cancellation preserved. Shared verifier unit scenarios authored, not run; preview producer not installed.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Независимый вход exposure preview

P0.1: added standalone SemanticExposurePreviewInput and owned exact-byte candidate/intent binder without ApprovedSemanticEdit or approval fields. Enforces source/snapshot/revision/hash/proposed-state and distinct document/mask/transform IDs. Unit scenarios authored, not run. SQL authority/producer/provider/adoption remain open.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### WorkingImage binding для preview

P0.1: extracted shared bind_exposure_working_source preserving Apply source checks; added preview-specific source binder without ApprovedSemanticEdit, exact raw SHA/canvas/ICC/dtype/bounds and document/pixel alias rejection. Unit scenarios authored, not run; SQL/producer not connected.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Вычисление exposure preview

P0.1: implemented separate execute_exposure_preview with required preview admission/verified loader, pre-IO compute budget, owned arrays, qualified color, shared exposure/encoding and independent exact-pixel quality replay. No approval/child/publication fabricated. Unit scenarios authored, not run; production loader/SQL/publication/provider still absent.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Геометрия маски preview

P0.1: extracted shared exact geometry verifier and integrated existing Apply wrapper; preview binder now verifies candidate mask/transform bytes, reference/source canvases, confidence and safe homography without ApprovedSemanticEdit. Pixel/document aliases rejected. Unit scenario authored, not run; production loader still open.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Protection binding для preview

P0.1: shared protection-source binder now used by Apply and separate preview binder; exact source/protection SHA/canvas/transform and strict canonical-origin proof remain mandatory. Preview rejects protection payload aliases. Origin strict-boolean unit scenarios authored, not run.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Artifact loader exposure preview

P0.1: added artifact-backed preview array loader using separate input authority, exact source/candidate/intent/mask/transform/protection binders, mandatory origin/ICC, pre-NPY budget, nonnegative RGB and protected overlap checks, owned arrays and post-IO reauthorization. Loader unit scenarios authored, not run. SQL callback installation/task publication/provider remain open.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Сквозной entry point вычисления preview

P0.1: execute_artifact_exposure_preview now connects exact artifact metadata -> verified NPY loader -> independent pixel executor/quality replay, with admission before document IO and no Apply receipt. Real-NPY end-to-end compute and denied-before-IO unit scenarios authored, not run. Production SQL/task publisher/provider/adoption remain open.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Staging WorkingImage preview

P0.1: shared attempt-scoped WorkingImage staging extracted and existing Apply wrapper connected; separate preview staging preserves source metadata and requires preview admission, rejects input/output aliases, writes NPY/WorkingImage only without receipt. Unit scenarios authored, not run.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Compute и staging preview

P0.1: execute_and_stage_exposure_preview connects artifact compute/independent quality to WorkingImage staging, validates output budget before NPY IO and subtracts resident encoded pixels from staging RAM. Real NPY compute-stage and insufficient-output-budget unit scenarios authored, not run. No durable receipt or SQL authority installation yet.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Численное evidence preview

P0.1: compute-stage entry now stages schema-bound ExposurePreviewNumericalEvidence linking exact params/model locks/target revisions and output image/pixels only after independent replay. Evidence hash/mutation/alias checks and admission before/after staging; numerical proof grants no Apply authority. Existing unit scenario extended, not run.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Строгие типы и отказ публикации evidence

P0.1: preview input/evidence schema_version now strict integer; protection evidence rejects boolean coercion. Added staging evidence hash/mutation/alias and post-stage revocation scenarios, plus schema type cases, all authored not executed.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Worker adapter exposure preview

P0.1: added explicit semantic.preview_local_exposure worker adapter with independent installer, immutable lease/params/runtime admission, CPU resource/scratch checks and preview origin fence, staged evidence root output. Not globally registered or advertised: production SQL installer/output registry still pending. Unit admission scenarios authored, not run.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Task scope и qualification preview adapter

P0.1: preview adapter now independently verifies tenant against TaskCommand, exact params SHA/canonical bytes and CPU ExposureQualificationPin lock identities before source IO; required external runtime verifier still measures deployment. Unit denied-before-source scenarios authored, not run.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### Declared inputs preview worker

P0.1: preview adapter requires exact plan/intent/candidate/source/protection roots in unique nonzero TaskCommand.inputs before source IO; missing/duplicate/substituted SHA fails closed. Unit scenarios authored, not run. SQL implementation still open.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### SQL current authority preview

P0.1: forward-only 02169 adds authenticated live pre-Apply preview read fence over editor identity, parent/target revisions, proposed state, exact ready candidate and canonical live plan-source inventory. HTTP invokes Go authority before/after provider IO. No mutation/approval/adoption granted. Migration not applied; static unit authored, not run.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### SQL admission preview worker

P0.1: forward-only 02170 adds separate worker preview admission with preview run/lease/slot/worker principal, exact params/config snapshot, live editor/session/current plan-target gate and qualification locks. Database adapter added. Migration not applied; static unit scenarios authored, not run. Producer/protection-origin/publisher/provider/adoption remain open.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### SQL protection origin preview

P0.1: forward-only 02171 adds preview-specific protection-origin SQL using independent preview admission, exact params root pin, canonical semantic.transfer producer/node/declaration and semantic_protection_producer_ready binary/source proof. Database adapter and static unit authored. Migration not applied; tests not run.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### SQL и qualification installer preview

P0.1: added SQL preview report-evidence 02172/database adapter and explicit preview installer composing lease SQL admission/origin, measured deployment locks, locked ICC and exact-version qualification report loader with post-IO evidence recheck. Not globally enabled. Migration not applied; static unit authored, tests not run.
Закрыто 0/7 блоков (0% остаточного плана кодирования).

### P0.1: scope installer до внешнего IO

Installer preview отклоняет чужой tenant и отсутствующие, повторённые либо подменённые declared roots до загрузки qualification report и SQL-вызовов. Добавлены четыре отрицательных unit-сценария; не запускались. Producer/publication/provider/adoption остаются открытыми. Закрыто **0/7 — 0%** остаточного плана.

### P0.1: locks params и qualification report

До report IO installer сравнивает model/runtime/device locks params с квалифицированным deployment. Написаны отрицательные сценарии трёх locks и сценарии installer с реальным qualification report: exact-version, отзыв evidence после IO, усечённые байты, SQL denial/nonboolean, смена lease во время IO. Тесты не запускались. Полный P0.1 не закрыт; **0/7 — 0%**.

### P0.1: backend params producer

Добавлен Go DTO независимого preview и canonical freeze/SHA builder, совместимый с worker SemanticExposurePreviewInput. Проверяются decimal revision, nonzero/distinct roots, source и runtime locks; approval/child поля отсутствуют. Unit-сценарии написаны, не запускались. SQL producer, publication, provider и Apply receipt остаются открытыми. **0/7 — 0%**.

### P0.1: компиляция preview из канонического плана

CompileExposurePreviewParams повторно валидирует private documents и формирует params только для точного proposed target/candidate/revision и local_exposure intent. Изменённые public projections не используются; child/decided/stale отклоняются. Написаны unit-сценарии, не запускались. Protection origin должен независимо проверяться SQL; queue/publication/provider/Apply receipt открыты. **0/7 — 0%**.

### P0.1: зависимости preview из pinned документов

Добавлен независимый resolver source/reference, RGB/alpha/geometry, mask/protection pixels и размеров из hash-pinned proposal/intent/image/mask документов без ApprovedEdit. Написаны positive projection и пять сценариев подмены; не запускались. Catalog authority, transform и binary/ICC gates остаются обязательными. Queue/publication/provider/receipt ещё открыты. **0/7 — 0%**.

### P0.1: полный frozen input inventory

Добавлены deterministic preview task/Params IDs и frozen closure со всеми plan/intent/candidate/source/reference/mask/transform/pixels/alpha/geometry зависимостями. Transport builder копирует caller state и отвергает конфликтующие IDs. Написаны сценарии coverage/replay/namespace/ownership; не запускались. SQL queue/publication/provider/receipt открыты. **0/7 — 0%**.

### P0.1: task compiler и ресурсы

Компилятор preview объединяет pinned source resolution, inventory и CPU qualification profile. Отдельный resource admission учитывает 512P+64MiB compute, measured runtime, одновременный input/output scratch и два JSON outputs. Написаны budget/overflow unit-сценарии; не запускались. SQL publication/queue/provider/receipt ещё открыты. **0/7 — 0%**.

### P0.1: публикация input manifest

Frozen preview task скрывает изменяемое состояние, DraftTask возвращает копию profile. Добавлена cancellable публикация input chunks/manifest через scoped Publisher с проверкой SHA. Написаны сценарии compilation/ownership/publication/cancel/bad SHA; не запускались. Конкретный durable SQL publisher, Params publication, queue/provider/receipt ещё открыты. **0/7 — 0%**.

### P0.1: Params-first publication

Добавлена композиция exact Params publication → chunks → manifest с проверкой ID/SHA и отмены после IO, без возврата частичного результата. Написаны unit-сценарии порядка, storage failure, SHA/ID mismatch и cancel; не запускались. Production durable SQL publisher/queue/provider/Apply receipt ещё открыты. **0/7 — 0%**.

### P0.1: SQL publication lock

Forward-only 02173 вводит private lock saved draft preview: parent/target row locks, live editor identity, exact params SHA/config/actor/session/snapshot и canonical plan/intent IDs. Public EXECUTE и dispatch/approval mutation отсутствуют. Статический unit написан, не запускался; миграция не применялась. Reservation/completion/producer/provider/receipt открыты. **0/7 — 0%**.

### P0.1: durable SQL document publisher

Forward-only 02174 и Go preview publisher реализуют Params/chunk/manifest tickets за saved preview с deterministic IDs, quota reservation/accounting, retry tokens, exact object version и повторной publication authority. Написаны scope/namespace/contract negative units и статические SQL-сценарии; не запускались, миграция не применена. Producer saved preview/queue, output publication/provider и Apply receipt открыты. **0/7 — 0%**.

### P0.1: saved preview run и исправление workflow identity

Исправлено смешение workflow_id/run_id в compiler: workflow передаётся отдельной authenticated identity и независимо сверяется SQL. Forward-only 02175 и Go SaveExposurePreview создают/replay deterministic actor/session/params-bound draft с live source/target locks, без dispatch. Статические и отрицательные unit-сценарии написаны; не запускались, миграция не применена. Queue/sealing/result publication/provider/Apply receipt открыты. **0/7 — 0%**.

### P0.1: internal operation catalog

Forward-only 02176 регистрирует отдельную internal CPU preview operation и output policies pixels/working_image/preview_evidence, совпадающие с worker. v2_enabled=false до полного admission/adoption. Добавлены static catalog и compiler profile/resource unit assertions; не запускались, миграция не применена. Queue/quota/sealing открыты. **0/7 — 0%**.

### P0.1: result quota и frozen profile

Forward-only 02177 и Go quota adapter связывают saved params, ready source metadata, pixel-derived минимальные RAM/scratch/output, live CPU qualification и immutable execution profile. Резервируется exact result storage с bounded accounting/replay. Static units написаны, не запускались; миграция не применена. Queue/sealing/output publication/provider/receipt открыты. **0/7 — 0%**.

### P0.1: quota release lifecycle

Forward-only 02178 освобождает только residual quota terminal preview после settlement всех tasks/uploads/backend documents и проверки user principal. Bounded discovery пропускает незавершённых owners. Подключено к действующему cleanup и scheduler readiness. Написаны runtime/static unit-сценарии; не запускались, миграция не применена. Queue/sealing/output publication/provider/receipt открыты. **0/7 — 0%**.

### P0.1: stale cancellation

Forward-only 02179 отменяет preview при потере parent/target/session authority, expired quota или abandoned unreserved draft. Bounded discovery и повторная проверка под publication-order locks; run/tasks получают cancellation без удаления объектов/раннего release. Подключено перед quota release и в readiness. Unit-сценарии написаны, не запускались; миграция не применена. Queue/sealing/result provider/receipt открыты. **0/7 — 0%**.

### P0.1: связанная production preparation

Добавлена композиция compile/private documents → authenticated current fence → saved run → frozen task → quota → concrete SQL Params/manifest publisher → post-IO authority. Source bytes принадлежат preparation; частичный результат не возвращается. Общий recovery уже завершает cancellation, отдельный finalizer не добавлен. Negative/static units написаны, не запускались. Relational materialization/sealing/output provider/receipt открыты. **0/7 — 0%**.

### P0.1: replay publication перед materialization

Preparation теперь независимо replay-верифицирует published Params/ref, draft/profile/digest, manifest/chunk SHAs и direct inventory по private frozen compiler; внешний cursor игнорируется и заменяется owned cursor. Shared transport verifier не требует ApprovedEdit. Написаны пять mutation и private-cursor unit assertions; не запускались. Materialization/sealing/output provider/receipt открыты. **0/7 — 0%**.

### P0.1: SQL materialization payload

Preparation возвращает owned node/transport/inventory/Params projections, заново построенные по private verified cursor: максимум 512 entries, точный ordinal/direct inventory и canonical entries SHA. Изменённый published count отвергается без частичного payload. Unit-сценарии projection написаны, не запускались. SQL materialization/sealing/output provider/receipt открыты. **0/7 — 0%**.

### P0.1: независимый SQL transport verifier

Forward-only 02180/02181 независимо восстанавливают sorted inventory/input digest, task/Params/chunk/manifest IDs и canonical transport SHAs; максимум 512 entries, обязательны plan/intent/candidate/source/protection roots. Это private pure predicates, без admission/mutation. Static units написаны, не запускались; миграции не применены. Materialization/sealing/output provider/receipt открыты. **0/7 — 0%**.

### P0.1: readiness физических input documents

Forward-only 02182 проверяет exact physical versions всех inputs и preview-owned Params/chunk/manifest tickets: SHA/bytes/bucket/key/role/expiry и frozen profile. Private readiness predicate не разрешает произвольные tenant dependencies: provenance остаётся отдельным обязательным gate. Static units написаны, не запускались; миграция не применена. Dependency provenance/materialization/sealing/output provider/receipt открыты. **0/7 — 0%**.

### P0.1: pre-lease protection provenance

Исправлено media type несовпадение: канонические protection payloads application/octet-stream разрешены наряду с application/x-npy; реальные bytes по-прежнему проверяются worker. Forward-only 02183 добавляет private pre-lease protection provenance exact workflow/intent/candidate canonical result/attempt/fence, frozen protection root и source/binary producer. Static units написаны, не запускались; миграции не применены. Full dependency provenance/materialization/sealing/provider/receipt открыты. **0/7 — 0%**.

### P0.1: transfer sidecar provenance

Forward-only 02184 проверяет candidate target, exact mask/transform documents и same-attempt/fence mask binary; каждый exact sidecar обязателен в inventory. Используются existing proposed-target readers, которые не создают approval/Apply. Static unit написан, не запускался; миграция не применена. Source/reference dependency provenance/materialization/sealing/provider/receipt открыты. **0/7 — 0%**.

### P0.1: source/reference image provenance

Forward-only 02185 проверяет source/reference WorkingImage по proposed target и intent reference versions, exact intent/image bytes, pixels/alpha/dense inverse-field children через authenticated metadata readers. Каждая dependency обязательна в inventory; approval/Apply mutation отсутствуют. Static unit написан, не запускался; миграция не применена. Exact inventory exclusion/materialization/sealing/provider/receipt открыты. **0/7 — 0%**.

### P0.1: exact inventory exclusion

Forward-only 02186 независимо восстанавливает полный input projection из params/proposal/intent/images/masks, включая alpha/dense inverse fields. Sorted dedup разрешает одинаковые dependencies, отвергает conflicting identities, пропуски и все лишние артефакты. Private pure predicate; static unit написан, не запускался, миграция не применена. Materialization/sealing/provider/receipt открыты. **0/7 — 0%**.


### Продолжение P0.1 — общий gate входов preview

P0.1: migration 02187 adds private aggregate preview admission input gate combining current locked authority, physical/transport readiness, exact sidecar/image/protection provenance and byte hashes, exact dependency inventory, live CPU qualification and locked unexpired actor output quota. Static contract scenario authored; not executed. No materialization/sealing/provider wiring yet; block remains open.

Закрыто 0/7 блоков (0% остаточного плана кодирования). Тесты, сборка и миграции не запускались.


### P0.1 — явное отклонение SQL NULL при admission

P0.1 static review: aggregate preview admission now requires explicit true from all six subordinate predicates (SQL NULL rejects), rejects null/non-object node/transport/inventory before authority work, and returns false rather than unknown for incomplete quota/profile evidence. Authored negative SQL-contract scenario; not executed. Materialization and provider remain open.

Закрыто 0/7 (0% остаточного плана). Тестирование и применение миграций не выполнялись.


### P0.1 — создание заблокированной preview-задачи

P0.1: migration 02188 adds private transactional preview draft task creation after aggregate live admission. Stable deterministic task is blocked, exact replay checks tenant/run/node/operation/profile/params/digest/attempt/cancellation; extra tasks and all incident graph edges reject. No role grant, approval, queue or sealing. Static contract scenario authored, not executed. Remaining: relational inputs, sealing/activation and provider integration.

Закрыто 0/7 (0% остаточного плана кодирования). Тесты и миграции не запускались.


### P0.1 — область допустимых связей preview

P0.1: migration 02189 adds narrow draft preapply-preview relational input scope and extends artifact-link trigger while retaining workflow/Apply/document scopes. Requires proposed unadopted target, active project/tenant, blocked attempt-zero exact CPU task, immutable profile and ready durable Params, current authenticated preview authority; parent scope explicitly true. Registration/sealing/activation still pending. Static contract scenario authored, not run.

Закрыто 0/7 (0% остаточного плана кодирования). Тесты и миграции не запускались.


### P0.1 — регистрация точных входов preview

P0.1: migration 02190 registers exact preview dependency inventory plus chunk/manifest at reserved ordinals after full admission and blocked-task creation in the same transaction. Exact complete replay accepted; partial/extra/altered or foreign-project links reject without repair. No sealing/activation/role grants. Static contract scenario authored; not executed. Existing person-inventory and negative-consumer barriers inspected and retained.

Закрыто 0/7 (0% остаточного плана кодирования). Тесты и миграции не запускались.


### P0.1 — запечатывание входов preview

P0.1: migration 02191 adds private same-transaction input sealing following exact registration. Persists manifest/direct roots and renderer/contracts/count/digest profile fields, verifies exact stored projection with Params/input digest; task remains blocked and run draft. Final activation/worker closure/provider still open. Static contract scenario authored, not executed.

Закрыто 0/7 (0% остаточного плана кодирования). Тесты и миграции не запускались.


### P0.1 — проверка сохранённого closure preview

P0.1: migration 02192 adds private persisted preview closure reconstruction: exact contiguous dependency rows and two transport rows, project fence, immutable profile, ready/unexpired artifact versions and reconstructed canonical transport/digests. Claim wiring/activation not yet connected. Static contract scenario authored, not run.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — активация preview и admission worker

P0.1: migrations 02193-02194 add private atomic full preview materialization/activation (seal, persisted closure, current authority and exact unexpired user quota, task ready, run sealed queued with bounded revision) and wire persisted closure into existing worker admission while retaining original lease/session/source/qualification gates. Operation remains disabled; claim/start/dispatch, HTTP provider and finalization remain open. Static contract scenarios authored, not executed.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — claim и старт preview

P0.1: migration 02195 wraps installed claim chain, retaining existing signer/series/source gates. Successful preview claims require sealed current run, exact persisted closure, live local-exposure CPU qualification and actor output quota; queued run starts with bounded revision atomically with lease, stale checks raise rollback. Operation remains disabled; dispatch enablement/provider/finalization still open. Static contract scenario authored, not executed.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — backend commit preview

P0.1: added narrow API SQL commit entry point 02196 and CommitExposurePreviewRun Go adapter invoking full admission/registration/seal/activation atomically. Validates frozen Params equality, bounded document bodies, source metadata and exact returned task identity; documents uncertain DB outcome requiring authoritative inspection. Negative unit scenarios authored, not executed. Preparation-to-HTTP orchestration/replay inspection/provider/finalization remain open; operation disabled.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — orchestration подготовки и commit preview

P0.1: AdmitExposurePreviewRun now joins owned server-resolved review/source/transform preparation, publication and atomic SQL commit; transform hash validated before IO. Returns stable run identity and commit-attempt marker on ambiguous commit failure to support authoritative recovery without re-submission. Negative unit scenarios authored, not executed. HTTP resolver/provider/recovery inspection/output finalization remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — чтение состояния preview для recovery

P0.1: migration 02197 and InspectExposurePreviewRun add read-only exact-request recovery state (run/task/seal/presence), requiring original actor/session, frozen Params hash and live proposed review authority. Missing/inaccessible/failed reads are not treated as commit rollback. Negative unit scenarios authored, not run. HTTP recovery orchestration and result provider remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — восстановление admission без повторной публикации

P0.1: admission now saves/inspects stable preview identity before publication, reuses sealed queued/running/terminal results without restarting, resumes only inspected empty draft and rejects partial/unknown states. Resource/source validation precedes DB mutations; run ID retained on subsequent failures. Unit recovery scenarios authored, not executed. HTTP/provider/finalization remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — backend decoder численного evidence

P0.1: added backend DecodeExposurePreviewEvidence with bounded exact bytes/SHA, required non-null fields, strict schema/algorithm/protection/zero-error, exact frozen Params and distinct input/output/evidence IDs. Numerical evidence explicitly not producer authority or photographic qualification. Positive/negative unit scenarios authored, not run. Canonical result reader, presentation rendering/provider/finalization remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — canonical evidence source

P0.1: migration 02198 exposes authenticated canonical preview-evidence object metadata only for exact original session/request and succeeded task result with matching current attempt/fence, single evidence output, ready pinned unexpired object and persisted closure. Closure predicate now permits completed preview reads; activation/claim retain independent state fences. Static contract scenario authored, not run. Object IO/recheck and presentation/provider remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — version-pinned evidence IO с повторной авторизацией

P0.1: ReadExposurePreviewEvidence now loads authenticated canonical metadata, reads bounded pinned object version, verifies length/SHA/strict evidence and repeats exact producer/session metadata after IO. Rejects changed fence/result/object or revoked scope and cancellation with no partial evidence. Unit scenarios authored for valid/changed/revoked/cancelled/truncated cases, not executed. Physical output/presentation/provider/finalization remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — exact WorkingImage/pixels binding

P0.1: VerifyExposurePreviewImageBinding verifies exact source/output WorkingImage hashes and canvas/color/geometry, requires evidence pixels equal output and distinct from source, and rejects every metadata change except pixels reference. Physical NPY and producer authority remain mandatory separate gates. Positive/tampered color/pixels/source unit scenarios authored, not run. Provider/presentation/finalization still open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — provenance выходов preview

P0.1: migration 02199 adds authenticated evidence-byte-bound WorkingImage/pixels metadata lookup. Both sidecars must match canonical evidence producer task/current attempt/fence, exact roles/media, verified immutable live versions and output bounds; no arbitrary tenant lookup. Static contract scenario authored, not run. Physical NPY verification and presentation/provider/finalization remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — reader output WorkingImage

P0.1: ReadExposurePreviewImage joins canonical evidence with exact two provenance-bound output metadata rows, pinned bounded WorkingImage IO, unchanged-source metadata/pixel binding, then repeats both output/session/canonical fences after IO. Keeps owned evidence bytes for SQL hash binding. Physical NPY and client presentation remain explicitly unverified. Negative unit scenarios authored, not executed; provider/finalization open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — потоковая физическая проверка pixels

P0.1: preview image reader now streams exact pinned pixels through bounded NPY shape/dtype/size/SHA/finite/nonnegative validation and closes stream before repeated producer/session output checks. Added reusable nonnegative float32 RGB verifier preserving HDR values above one; unit cases zero/HDR/negative/Inf/NaN authored, not executed. Presentation/provider/finalization remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — консервативные display overlays

P0.1: worker prepare_preview_mask_overlay adds bounded RGBA display-only affected/protection masks, max-coverage downsampling and ceil alpha preserving tiny nonzero regions, strict float32 probabilities/size/budget/cancellation. Existing editing masks and ICC presentation left intact. Unit cases preservation/source immutability/invalid values/budget/cancellation authored, not executed. PNG publication/ICC before-after/provider/finalization remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — проверяемое PNG-кодирование overlays

P0.1: write_preview_mask_overlay adds bounded scratch PNG encoding, owned RGBA input, pre-IO RAM/output worst-case admission, stripped metadata and exact decoded-pixel/alpha verification with cancellation fences. Unit scenarios exact transparency/immutability/metadata and output-budget-before-write authored, not run. Artifact publication/ICC before-after/provider/finalization remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — qualified ICC display conversion

P0.1: prepare_exposure_display reuses existing gamma-aware linear/premultiplied resize and LCMS conversion under explicit live runtime qualification, locked ICC file hash, fixed working profile identity and pinned sRGB bytes. Bounded RAM/input validation; display-only clipping preserves full-range editing outputs. Negative runtime/budget unit scenarios authored, not run. Writer/publication/provider/finalization remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — lossless before/after PNG

P0.1: write_exposure_display encodes lossless before/after PNG with pinned sRGB ICC and optional exact alpha, strips other metadata, admits RAM/output worst-case budgets, requires live runtime qualification and verifies decoded pixels/ICC after encoding. RGB/RGBA unit scenarios authored, not run. Artifact publication, provider/HTTP orchestration and finalization remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — staging четырёх preview media

P0.1: stage_exposure_preview_media connects four validated display encoders to attempt-scoped scratch/staging, pre-admits aggregate output/RAM, repeats live runtime and preview admission around every object, verifies staged SHA and distinct IDs, returns no partial bundle. Negative pre-IO admission/budget unit scenarios authored, not run. Presentation operation/catalog/quota orchestration and provider/finalization remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — presentation document linkage

P0.1: ExposurePreviewPresentation and stage_preview_presentation bind four media to exact numerical evidence, frozen request/output refs, dimensions and ICC/resize/overlay pins; enforce distinct IDs and budget, repeat admission around contract publication and verify staged SHA. Contract unit alias/roundtrip/dimension cases authored, not run. Operation/catalog/schema projection/provider/finalization integration remains open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — backend presentation decoder

P0.1: backend presentation DTO/strict decoder mirrors worker contract, binds exact numerical evidence and frozen Params/output refs, requires all non-null fields, pinned ICC/resize/overlay rules, bounded dimensions and distinct media/document IDs. Positive and tampered evidence/alias/ICC/dimension/algorithm/params unit cases authored, not executed. Schema projection, canonical presentation producer, physical PNG/provider/finalization remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — targeted schema projection generator

P0.1: added targeted generate-semantic-preview-contracts.py for three executable preview schemas and namespaced dependency projection into normative schemas.json without overwriting unrelated definitions. Generation attempted, unavailable: repository .venv missing rfc8785; system Python missing pydantic. No schema outputs produced, projection remains open; no tests executed. Provider/operation/finalization remain open.

Закрыто 0/7 (0% остаточного плана). JSON Schema generation требует восстановления зависимостей runtime; тесты и миграции не запускались.


### P0.1 — preview JSON Schema сгенерированы

P0.1: resolved schema-generation environment by bootstrapping pip and installing existing locked rfc8785==0.1.4 only in repository .venv; dependency manifests unchanged. Targeted generator successfully emitted three standalone preview JSON Schemas and projected executable definitions/namespaced dependencies into normative schemas.json, preserving unrelated definitions. Generation only, no tests. Operation/provider/finalization remain open.

Предыдущий локальный разрыв генерации устранён. Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — inherited alpha preview

P0.1: load_exposure_preview_alpha reuses strict read_alpha_array for pinned inherited opacity, normalizes legacy uint16 exactly as existing media path and owns float32 output under bounded budget/admission/cancellation. Opaque source returns None without alpha IO. Negative admission and opaque unit scenarios authored, not run. Full presentation operation/provider/finalization remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — единая подготовка четырёх media

P0.1: prepare_exposure_preview_media joins owned original/edited/mask/protection/alpha arrays, independent exact replay, qualified ICC before-after and conservative overlays into one complete media preparation. Conservative total RAM admission (640P+128MiB) and live fences between stages; existing 2560 no-upscale convention. Negative budget-before-runtime unit scenario authored, not run. Resource/profile alignment and executable operation/provider/finalization remain open.

Закрыто 0/7 (0% остаточного плана). Тесты и миграции не запускались.


### P0.1 — ресурсный профиль полного preview

P0.1: full presentation resource admission aligned in Go and forward-only migration 02200: RAM 640P+128MiB plus runtime, scratch includes largest ICC PNG, output includes four bounded PNGs and JSON allowance. Immutable prior profiles are not rewritten; incompatible saved profile fails admission. Resource and SQL contract unit scenarios authored, not executed. Executable worker presentation, API provider and finalization remain open; closed blocks 0/7 (0%).


P0.1: execute_complete_exposure_preview now composes verified compute, provenance-checked reload, inherited alpha, independent replay/ICC display preparation, WorkingImage/numerical evidence, four PNGs and presentation staging under aggregate output/RAM admission. Returns only a complete bundle after final live fences. Authored pre-compute RAM/output rejection scenarios; not run. Factory/operation catalog/canonical output reader/API integration remains open, 0/7 closed.


P0.1: explicit SQL installation accepts typed absolute ICC presentation pins; worker adapter selects complete preview staging and presentation-only canonical result when these pins are installed, with full scratch gate and repeated lease/runtime fences. Attempt staging roles include the four PNGs and presentation. Legacy numerical path remains explicit without pins. Catalog stays disabled pending canonical SQL evidence/presentation readers and API integration. Installation pin rejection unit scenarios authored, not run; 0/7 blocks closed.


P0.1: forward-only migrations 02201/02202 add authenticated canonical presentation source and exact-hash-bound seven-sidecar lookup. Same canonical task/result/attempt/fence, ready/versioned/nonexpired artifacts, distinct refs, media/size/dimension limits and current session/closure required. No arbitrary artifact lookup or activation. Static SQL contract scenarios authored, not executed. Backend byte reader, catalog and API integration remain open; 0/7 closed.


P0.1: backend presentation document reader now performs bounded version-pinned IO, exact byte length/SHA checks and a second current session/canonical producer attempt-fence query after IO. Returns owned raw bytes internally and explicitly does not grant validated display evidence or URLs. Authored changed-fence/revocation/cancellation/truncation/same-length corruption scenarios, not run. Semantic sidecar/PNG reader and API/catalog integration remain open; 0/7 closed.


P0.1: ReadExposurePreviewPresentationLinkage loads exact seven-role sidecar set, rejects duplicate/aliased/foreign-bucket/unversioned/oversized entries, verifies numerical evidence SHA/params and strict presentation ICC/ref linkage, then rechecks sidecars and canonical document task/result/attempt/fence after IO. No URL signing before physical PNG/WorkingImage/pixel verification. Cancellation/missing-authority scenarios authored, not run. Physical presentation read/catalog/API still open; 0/7 closed.


P0.1: ReadExposurePresentationImage binds full-resolution WorkingImage to source/numerical evidence, streams exact SHA/size/finite nonnegative float32 RGB NPY and rechecks canonical presentation/linkage after pixel IO. Display dimensions match no-upscale 2560 nearest-even worker convention; tie/aspect/thin-image scenarios authored, not run. Physical PNG validation/catalog/API remain open; 0/7 closed.


P0.1: physical PNG verifier checks exact SHA, bounded 8-bit RGB/RGBA dimensions, every chunk CRC, only IHDR/iCCP/IDAT/IEND, bounded decompressed exact ICC SHA for before/after, metadata-free overlays, full PNG decode and affected/protected tint including transparent pixels. This is format/profile proof, not photographic or exact overlay replay. Authored valid overlay/role/profile/CRC/cancellation scenarios, not run. Reader wiring/catalog/API remain open; 0/7 closed.


P0.1: ReadPhysicalExposurePreview composes full-resolution WorkingImage/NPY verification and four sequential bounded version-pinned PNG checks, followed by canonical producer/sidecar revalidation; only complete read gets private verified marker. Migration 02203 adds five presentation output policies without enabling dispatch. Negative authority/cancellation scenarios authored, not run. API/provider/finalization and installation wiring remain open; 0/7 closed.


P0.1: BuildExposurePreviewResponse accepts only opaque physically verified preview state, checks request fences, signs exact immutable PNG versions for one minute, constructs review-only DTO with explicit photographer-review limitation, validates transport and repeats live authority after signing. Physical proof fields made private to prevent external mutation/forgery. Unverified-before-signing unit scenario authored, not run. Concrete provider/current-preview lookup/finalization and startup wiring remain open; 0/7 closed.


P0.1: concrete ExposurePreviewProvider implements HTTP ReadPreview contract, validates dependencies/pins, checks live authority before/after trusted resolver, rebinds run params to authenticated request, performs full physical read and produces short-lived review-only response with final authority checks. Resolver/current-run SQL and startup attachment remain required; no nil fallback or Apply authority borrowing. Dependency/cancellation scenarios authored, not run; 0/7 closed.


P0.1: migration 02204 resolves an existing running/completed sealed preview under live editor/session/current-plan fences, rejects multiple matches, returns exact ready version-pinned source metadata. SQLExposurePreviewResolver validates frozen params SHA/request/source binding, reads bounded source bytes and repeats authority. Lookup never silently triggers Apply or chooses an arbitrary run. Startup wiring/admission trigger/finalization remain open; 0/7 closed. No tests or migrations executed.


P0.1: API startup now installs concrete semantic preview provider with SQL resolver and version-pinned storage SignRead. ICC lock pin is read bounded from validated deployed registry with before/after registry validation and raw-hash stability checks. Existing GET no longer has an unwired provider in production startup. Incomplete registry unit scenarios authored, not run. Preview creation/admission trigger, worker startup installation, finalization and Apply receipt remain open; 0/7 closed.


P0.1: CPU worker CLI/build_worker/run_worker now accepts exposure_preview_installation_factory; explicit PHOTO_EXPOSURE_PREVIEW_ENABLED=true installs full presentation path with measured model/runtime/device/dataset hashes, validated ICC registry/raw lock and sRGB pins, bounded storage transport and SQL admission/qualification. Disabled default does not probe GPU/storage. Catalog remains disabled. Authored disabled/CPU-only/invalid-enable scenarios, not run. Admission trigger/finalization/Apply receipt remain open; 0/7 closed.

Worker configuration: `PHOTO_EXPOSURE_PREVIEW_ENABLED=true`, `PHOTO_EXPOSURE_PREVIEW_MODEL_LOCK_SHA`, `PHOTO_EXPOSURE_PREVIEW_RUNTIME_LOCK_SHA`, `PHOTO_EXPOSURE_PREVIEW_DEVICE_PROFILE_SHA`, `PHOTO_EXPOSURE_PREVIEW_DATASET_SHA`; existing `PHOTO_ICC_ROOT` and S3 configuration required. CPU worker only; this does not restrict Metal for other operations.


P0.1 static defect fixed: semantic quota release incorrectly required v26_derivative_task_id, never assigned by new preview activation. Forward-only migration 02205 uses semantic_preapply_preview config plus immutable v27 preview profile, preserving terminal/task/upload/document/usage guards. Static regression scenario authored, not run. Success/failure finalization and request admission remain open; 0/7 closed.


P0.1: migration 02206 finalizes single-task semantic preview execution to completed/failed with tenant-parent-target-preview lock order, canonical attempt/fence ready presentation and current closure gates for success. Does not approve photographic result/Apply or release quota early. Scheduler cleanup and startup function checks wired; unit/static contract scenarios updated/authored, not run. Request admission/Apply receipt and dispatch enablement remain open; 0/7 closed.


P0.1 static finalization hardening: eligibility (active tenant, deterministic single CPU task, canonical ready presentation, live closure/authority) is now filtered before LIMIT and repeated under locks, so an ineligible first success cannot starve later terminal previews. Regression SQL scenario authored, not run. Admission trigger/Apply receipt remain open; 0/7 closed.


P0.1: PrepareScopedPreviewReview now resolves current authenticated review documents outside transactions, then rechecks locked canonical source rows and selects exact proposed target under revision/plan fences, returning trusted workflow identity without synthesizing a close or Apply decision. Final live authority rechecked after transaction. Missing-authority scenario authored, not run. Source/protection preparation and request admission wiring remain open; 0/7 closed.


P0.1: LoadPreviewSourceDocuments hydrates exact source/reference/mask/transform/protection JSON via authenticated proposed-target SQL metadata helpers, validates raw size/SHA, compiles proposal params from private review and protection pin, derives dependency dimensions/resources and repeats current authority. No synthetic approval; binary/origin authority remains mandatory in atomic preview commit. Negative unscoped IO scenario authored, not run. Request orchestration/Apply receipt remain open; 0/7 closed.


P0.1: ExposurePreviewAdmissionService now composes authenticated scoped review, exact source/protection document hydration and atomic preview preparation/publication/commit. Deployment-owned review/runtime/qualification/resource settings are captured before IO; client cannot supply them. Preserves stable recovery RunID/commit ambiguity from admission. Cancellation/uninstalled-service unit scenarios authored, not run. HTTP admission endpoint/startup config, Apply receipt and dispatch enablement remain open; 0/7 closed.


P0.1: POST target/preview HTTP admission route added separately from read-only GET; accepts only four strict query fences and empty body, requires authenticated editor, returns 202 stable run/task/status recovery metadata. X-Preview-Run-ID preserves known identity on commit errors. Endpoint registry mirrors updated (109 endpoints), admission response schema added and OpenAPI projected without full spec rebuild. Admission startup config/client call and Apply receipt remain open; 0/7 closed. No tests/migrations executed.


P0.1: API startup now installs tenant-isolated preview admission from SHA-pinned ExposurePreparationCatalog via PHOTO_SEMANTIC_PREVIEW_CONFIG_FILE/SHA256. Exact per-tenant runtime budgets and qualification reused, no admission-role escalation. Disabled default/partial configuration/foreign tenant scenarios authored, not run. OpenAPI projection now declares four required preview query fences for GET/POST and empty POST body/recovery header semantics. Client launch/Apply receipt/dispatch remain open; 0/7 closed.


P0.1: API client now validates/adopts POST preview admission; photographer preview invokes it then polls only explicit 503-not-ready every 8s under 10-minute abort/deadline and existing foreground/unmount/query fences. Terminal admission states fail immediately; malformed/auth/conflict errors are not polled. Existing preview-only test doubles remain compatible. SQL missing current render maps to dependency unavailable rather than generic error. Route inventory and unit rejection scenario updated, not run. Apply receipt/dispatch and recovery UX remain open; 0/7 closed.


P0.1: APIError preserves validated X-Preview-Run-ID only for POST semantic-preview errors; CORS exposes recovery header. Photographer continues read-only result polling after a server error with known run identity, without issuing another mutation or presuming rollback. Auth/conflict/unidentified failures still stop. Empty-body immutable-query client scenario authored, not run. Durable recovery UX/Apply receipt/dispatch remain open; 0/7 closed.


P0.1: migration 02207 introduces durable immutable run preview receipt and API-only seal under parent/target/preview locks; exact canonical presentation/seven sidecars/numerical and WorkingImage raw SHA/size/linkage required, same result/attempt/fence preserved, conflicts fail. Physical verifier invocation and transactional Apply adoption still open. Corrected normative packages/contracts/v2-endpoints.json mirror to 109 endpoints (additional endpoints.json retained). SQL unit scenario authored, not run; 0/7 closed.


P0.1: provider now seals durable preview receipt only after full physical verification and before URL signing. Backend validates SQL response against exact canonical task/result/attempt/fence, frozen params SHA and presentation/render/pixels/evidence refs; opaque physical state required. Receipt mismatch unit scenarios authored, not run. Transactional Apply adoption and dispatch enablement remain open; 0/7 closed.


P0.1: backend/client Apply request decoders now support optional exact preview render/evidence selection, rejecting aliases/malformed SHA or preview on reject. Omitted field preserves historical canonical request/replay shape. Client UI deliberately not switched until transactional SQL gate/adoption accepts selected render; this is incomplete contract groundwork, not D-01 closure. Selection unit scenarios and normative schema added, not run; 0/7 closed.


P0.1: migration 02208 adds admission-role-only transactional selection lock: exact displayed render/presentation receipt, actor/session/current plan-target revisions, unique preview, canonical task/result/attempt/fence and four ready version-pinned edit/evidence objects. Parent/target/preview/task locks retained for caller adoption; helper does not approve or rerender. Atomic adoption integration remains open; SQL regression scenario authored, not run; 0/7 closed.


P0.1: LockApplyPreviewSelections implements sorted target receipt locking for new Apply mutations after replay, requiring selected preview for every approve and decoding exact canonical run/task/result/attempt/fence/render/presentation response. No object IO or rerender; returned selections still need atomic adoption integration. Updated positional TargetDecision test literals after adding optional field (static compatibility defect). Cancellation scenario authored, not run; 0/7 closed.


P0.1: stage_shown_preview_image adds receipt-admission callback guarded byte-for-byte pinned NPY copy, exact SHA/size/finite nonnegative float32 validation and WorkingImage clone changing only pixel artifact ref. No exposure/generative kernel or photographic changes; inherited metadata/alpha retained. Not globally registered; requires approved receipt installer and atomic adoption integration. Wrong-document-hash-before-IO unit scenario authored, not run; 0/7 closed.


P0.1: ShownExposureAdoptionInput freezes approved-edit ref plus exact preview run/task/result/attempt/fence/presentation/numerical/render/pixels and measured locks for a distinct copy/adoption path. Strict zero/alias/integer/deterministic task binding checks; it does not itself prove approval. Targeted generator emitted new schema and preserved unrelated normative definitions. Roundtrip/foreign-task/alias/fence unit scenarios authored, not run. Adapter/SQL atomic adoption remain open; 0/7 closed.


P0.1: shown-adoption binder checks exact raw SHA for approved edit/presentation/numerical/image, original candidate/intent/source bindings, EV/strength/prompt/protection policy, measured locks and unchanged WorkingImage metadata except pixel ref. execute_shown_exposure_adoption repeats explicit SQL/runtime authority before document IO and byte-copy staging, with qualified ICC. No global registration until atomic adoption installed. Denied/None/truthy authority scenarios authored, not run; 0/7 closed.


P0.1: explicit semantic.adopt_shown_exposure CPU adapter validates operation/profile resource ownership, exact frozen Params SHA/tenant/child and declared input roots, repeated unchanged lease and installed SQL/runtime checks. Actual image dimensions must match admitted pixel_count. Only byte-copy executor is invoked; returns WorkingImage, no device allocation. Adapter remains unregistered pending SQL installation/atomic commit. Wrong-operation/backend/installer scenarios authored, not run; 0/7 closed.


P0.1: backend ShownExposureAdoptionParams now freezes the exact byte-copy worker contract with deterministic preview task, producer attempt/fence, five distinct artifact roots and three measured locks. Rejects zero/aliased IDs, invalid SHA, attempt overflow and foreign producer task before emitting bytes. Unit scenarios authored for exact roundtrip/fence and malformed bindings; not run. Atomic Apply installation and closure integration remain open; 0/7 closed.


P0.1: CompileShownExposureAdoption joins a locked photographer selection with opaque physically verified preview evidence and SHA-verified ApprovedSemanticEdit. Exact receipt/producer fence/source/intent/candidate must match; deterministic copy-task identity and separate owned Params bytes cannot enter the existing rerender compiler. Unit scenarios for rejecting unverified preview and publication byte ownership authored, not run. Full closure, SQL installer and atomic Apply wiring remain open; 0/7 closed.


P0.1: CompileShownExposureAdoptionResources now charges both pinned input and staged NPY, bounded validation RAM plus measured runtime/index overhead, no GPU allocation, safe dimension/overflow and every quota limit. Adoption compiler unit scenarios expanded to deterministic compilation, exact selected output/fence and rejection of changed result/run/pixels/source/body/child. Tests authored, not executed. SQL copy-operation installation, full closure and atomic Apply remain open; 0/7 closed.


P0.1: added adoption inventory compiler: rebuilds immutable copy Params against locked receipt and physical preview, resolves SHA-bound proposal source/mask/protection/geometry/alpha dependencies, checks approved EV/strength/prompt/mask/transform against the original intent/candidate, includes selected image/pixels/numerical/presentation and four media roots. Added forward-only 02209 copy operation/output catalog, disabled until SQL lease/atomic admission installed. Missing-closure and altered-frozen-bytes tests authored, not run. 0/7 closed.


P0.1: FrozenShownExposureTask compiles copy-specific profile/resources and complete immutable adoption inventory, checks CPU qualification lock pins, owns DraftTask profile projection, and publishes exact Params/chunks/manifest via scoped durable publisher. Common transport publication extracted without changing preview behavior; child SQL authority still mandatory. Empty/wrong-operation/cancelled publication tests authored, not run. Atomic Apply and worker SQL installer remain open; 0/7 closed.


P0.1: copy-task publication verifier independently rebuilds transport from frozen compiler-owned inventory, rejects changed operation/profile/Params/direct roots/manifest, ignores caller cursors and emits distinct owned ShownExposureCommitInputs for future atomic SQL adoption. Shared verified projection helper extracted for preview without widening admission. Tamper/cursor/ownership unit scenarios authored, not executed. SQL installer and Apply transaction remain open; 0/7 closed.


P0.1: forward-only 02210 reconstructs copy-operation JCS inventory/digest with adoption and display document contracts; 02211 privately validates exact copy Params against canonical receipt, preview scope, producer attempt/fence/result, measured locks and distinct nonzero refs with integer bounds and fail-closed NULL handling. Pure helpers grant no worker/API authority or approval. Static SQL contract scenarios authored, not run; migrations not applied. Live lease/atomic Apply integration remains open; 0/7 closed.


P0.1: 02212 independently derives exact adoption inventory including four shown media and all original source/mask/protection/alpha/geometry children; 02213 checks every input against same-tenant ready verified versioned artifact metadata, exact SHA, expiry and bounded JSON/NPY/PNG MIME/size. Helpers remain private and do not substitute for producer provenance or atomic approval. SQL contract scenarios authored, not run; migrations not applied. Atomic Apply/lease admission remain open; 0/7 closed.


P0.1: 02214 independently rebuilds deterministic copy-task Params/chunk/manifest identities and SHA, exact direct roots/count and child namespace; 02215 binds ready transport to same-tenant durable backend document owner/purpose/publication run and exact version/bytes/SHA. Server-selected parent/child publication scope still requires atomic caller authority. SQL contract scenarios authored, not run; migrations not applied. Full Apply/worker lease installation remains open; 0/7 closed.


P0.1: private 02216 independently checks copy execution profile, exact shown pixels/dimensions, CPU-only zero VRAM and resource bounds aligned with Go/worker, plus measured lock SHA parity. Hardened copy staging to reject generated pixel/WorkingImage IDs aliasing preserved alpha. SQL/staging contract scenarios authored, not run; migration not applied. Live qualification, worker lease and atomic Apply integration remain open; 0/7 closed.


P0.1: 02217 private admission-role lock reauthorizes canonical selected preview under existing parent/target/preview locks and binds exact raw-SHA copy Params to that receipt and measured locks. Go LockShownExposureParams validates frozen deterministic task/Params and returned exact receipt, retains caller transaction locks and requires post-idempotency use. SQL contract scenarios authored, not run; migration not applied. This gate creates no child/task/approval; final atomic Apply/worker integration remains open; 0/7 closed.


P0.1: added lock adapter unit scenarios for exact returned receipt, changed run/fence/pixels, unknown fields, SQL stale error, cancellation after IO and tampered frozen Params before SQL. Private 02218 binds exact approved-edit raw SHA/source/intent/candidate/mask/transform/EV/strength/prompt/protection policy to preview proposal; decision-batch membership remains mandatory caller authority. Tests authored, not run; migration not applied. Atomic Apply and worker lease integration remain open; 0/7 closed.


P0.1: private 02219 joins canonical locked preview/receipt with bounded exact raw bytes for ten required documents, independent SHA/physical-size checks, unchanged WorkingImage metadata except pixels, exact numerical evidence, approved-edit binding, inventory, CPU resources, durable transport and live copy-operation qualification. It creates no task/approval and still requires caller decision-batch/publication-scope/quota authority and atomic graph installation. SQL scenarios authored, not run; migration not applied. 0/7 closed.


P0.1: PrepareShownExposure now composes verified selected-preview copy compilation, full inventory/profile, exact SHA/UTF-8/JSON-bounded owned ten-document bundle, repeated live authority around durable publication and verified SQL projection. CheckPreparedShownExposure repeats locked canonical Params/receipt and calls 02219 in caller Apply transaction; narrow admission-role EXECUTE granted. Missing-authority/incomplete-bundle/cancellation scenarios authored, not run. Decision-batch/quota/atomic graph and worker installation remain open; 0/7 closed.


P0.1: immutable decision document now preserves optional exact shown render/evidence selection (omitted historical field keeps old bytes), binding both decision SHA and request hash. VerifyShownExposureBatch rebuilds approved edits from private review evidence, requires exactly one copy task per explicit approval, rejects duplicate/extra/missing tasks and mismatched selection/scope/revision/source, and owns publication payloads before SQL waits. Unit scenarios authored, not run. Legacy SQL still rejects preview-aware new decisions until atomic copy path installed; UI remains unconnected. 0/7 closed.


P0.1: 02220 adds separate preview-aware decision-batch lock preserving original source initialization, target locks, publication owner/raw SHA/size and request/plan/source checks. Each approval requires exact matching request/document preview refs and canonical selected receipt locked in target order; rejections cannot carry preview. Original legacy gate unchanged, new gate private until atomic copy commit installed. SQL contract scenarios authored, not run; migration not applied. 0/7 closed.


P0.1: PrepareShownBatchCommitInputs now binds publication-owned decision proof and private review initialization documents to owned exact copy-task rows. 02221 independently enforces one deterministic copy task per explicit approval, no duplicate/extra/rejected-target tasks, selected render/evidence equality, exact Params raw SHA and transport reconstruction. Reject-only requests stay outside copy-child path. Unit/SQL contract scenarios authored, not run; migration not applied. Quota, atomic shell/graph activation and worker installation remain open; 0/7 closed.


P0.1: private 02222 creates shown-copy Apply draft only after full decision/selected-receipt/document validation; all tasks blocked, no dispatch, dedicated shown_semantic exact JCS config, complete target-row update count. 02223 independently verifies persisted draft config/hash/task/profile/transport equality and queued target membership in actor/project Apply batch before quota/seal. Both require final same-transaction quota/closure/activation/receipt completion; no runtime grants. SQL scenarios authored, not run; migrations not applied. 0/7 closed.


P0.1: 02224 reserves aggregate shown-copy output budget from exact persisted draft/profile and live qualification under tenant/run/task/usage/quota locks; strict capacity/safe integer/principal checks, replay requires exact unexpired reservation with no renewal/double-accounting. 02225 composes shell+quota so refusal rolls back draft/target mutations in one statement. Both private/non-dispatchable until closure/seal/activation/final receipt installed. SQL scenarios authored, not run; migrations not applied. 0/7 closed.


P0.1: 02226 adds narrow shown-copy v2 input scope requiring actor/project decision batch, queued target, blocked draft task, exact Params publication and live quota; extends guard preserving all existing scopes. 02227 registers full inventory plus chunk/manifest ordinals after persisted quota/transport checks, exact replay only, partial/foreign-project inputs rejected without repair. Private same-transaction components, no dispatch. SQL scenarios authored, not run; migrations not applied. Sealing/activation/final receipt/worker integration remain open; 0/7 closed.


P0.1: private 02228 independently checks sealed Params/manifest/direct roots/profile augmentation and every registered project/ordinal/ref/contract row against exact inventory plus ready objects. 02229 registers then seals complete batch, compares total counts, rechecks persisted result and keeps tasks blocked/child draft; stale/partial state raises and rolls back caller transaction. SQL scenarios authored, not run; migrations not applied. Activation/final receipt/runtime installer remain open; 0/7 closed.


P0.1: private 02230 activates shown-copy child only after persisted full sealed inventory, actor/project Apply batch, live qualification and exact unexpired quota; verifies complete ready-task count before sealing/queueing run. 02231 composes activation, bounded parent revision, durable idempotency response, audit/event/outbox under same transaction and exact selected batch binding. No runtime grants until complete commit/runtime installation. SQL scenarios authored, not run; migrations not applied. API commit orchestration/runtime installer remain open; 0/7 closed.


P0.1: 02232 complete admission-role atomic copy commit locks exact new idempotency receipt before reserved draft, full input registration/seal and activation/parent/receipt/audit/events; any failure raises whole-statement rollback. CommitShownExposure Go adapter retains completed replay before new source checks and submits publication-bound copy batch via one SQL call, never rerender compiler. Unit/SQL contract scenarios authored, not run; migration not applied. Runtime copy installation/claim/output adoption and API/UI preparation wiring remain open; 0/7 closed.


P0.1: 02233 rebuilds shown-copy persisted closure/transport and exact project/versioned inputs; 02234 worker lease admission checks unchanged run/task/slot/worker principal, frozen Params SHA, committed queued target/batch, exact original preview result/attempt/fence/receipt, live editor/CPU qualification/locks and quota. It intentionally does not require old proposal revision after Apply. Database adapter added; operation remains disabled/unregistered until installer/claim/output integration. SQL scenarios authored, not run; migrations not applied. 0/7 closed.


P0.1: distinct ShownExposureQualificationPin/report verifier requires semantic.adopt_shown_exposure CPU report, never local-exposure report. Explicit SQL installer owns lease/Params/scope/locks, exact-version report loader repeats live metadata before/after IO and returns byte-copy callbacks. 02235/DB adapter expose report only through admitted live copy lease. Scope-before-IO unit scenarios authored, not run; migration not applied. Runtime registration/claim/output and API/UI preparation wiring remain open; 0/7 closed.

P0.1: shown-copy operation now has explicit CPU-only build_worker/run_worker registration and CLI startup wiring. Separate PHOTO_SHOWN_EXPOSURE_ENABLED (default false) and four measured PHOTO_SHOWN_EXPOSURE_* pins prevent preview qualification from silently enabling copy. Startup unit scenarios authored, not run. Claim/output adoption and API/UI preparation remain open; 0/7 closed.

P0.1: independent shown-copy adoption verifier binds SHA-verified Params and shown WorkingImage to staged output, permits only pixel artifact ID replacement, requires identical pixel SHA/bytes and preserved metadata/alpha, repeats live receipt admission and rejects inactive scope before IO. Context unit tests authored, not run. Verifier is not yet wired into generic adoption; registry, dispatcher and SQL finalization remain open; 0/7 closed.

P0.1: normative and package operation registries now describe shown-copy WorkingImage/pixels outputs. Generic prepare_v2_adoption invokes mandatory live shown receipt verifier, handles working_image root role and preserves admitted legacy alpha/geometry input MIME handling. Missing authority fails before evidence IO; unit scenario authored, not run. AdoptionService SQL context and finalization remain open; operation stays disabled in SQL; 0/7 closed.

P0.1: forward-only 02236/02237 add scheduler-only shown-copy Params and canonical-result discovery, fenced by exact attempt/result/fence, active tenant/project, sealed committed child and shown_semantic task/Params binding. Pending discovery is bounded and read-only; SQL unit checks authored, not run. Migrations not applied; read admission, coordinator and final transactional adoption still open; 0/7 closed.

P0.1: 02238 adds result-scoped shown-copy admission independent of expired worker lease, repeating canonical attempt/Params SHA, immutable preview receipt/producer, full closure, editor session, live qualification and output quota. Python adapter owns context/raw Params and repeats exact SQL authority for adoption. Unit scenario authored, not run; migration not applied. Coordinator and final locked publication remain open; 0/7 closed.

P0.1: AdoptionCoordinator accepts explicit shown-copy installation, discovers its canonical result, prepares through mandatory receipt+qualification callback and generic typed closure. Dedicated finalization is explicitly fail-closed; it cannot fall through to generic adopt_v2_plan. Result-scoped installer owns context and requires operator qualification callback; unit cases authored, not run. Locked SQL finalizer and exact-version report installation remain open; 0/7 closed.

P0.1: 02239 adds private shown-child inventory retention with existing canonical attempt/fence checks, verified output uploads, declared inputs, original workflow links and atomic inbox/event/outbox. 02240 preserves the installed generic adoption chain but denies shown-copy there, enforcing dedicated finalization at SQL level too. Unit checks authored, not run; migrations not applied. Dedicated proof-carrying commit remains open; 0/7 closed.

P0.1: private 02241 output proof binds original pinned shown/output JSON SHA+size, unchanged metadata/alpha, identical pixel SHA+bytes, distinct nonzero output IDs, canonical result, ready versioned artifacts and exact attempt/fence upload roles. SQL unit checks authored, not run; migration not applied. Locked commit and coordinator payload wiring remain open; 0/7 closed.

P0.1: 02242 dedicated shown-copy commit locks tenant/project, producer/child runs and tasks, target and immutable input/output artifacts; repeats exact output proof around private atomic inventory adoption. Coordinator now supplies owned SHA-checked Params/output/shown JSON through dedicated 14-argument commit, never generic adoption. SQL unit checks authored, not run; migration not applied. Qualification installation, artifact-link scope, claim/start, final Apply projection and API/UI wiring still need integration; 0/7 closed.

P0.1: statically confirmed existing v2_artifact_links workflow scope already accepts original-workflow retained shown outputs; no trigger broadening needed. 02243 and versioned shown adoption installer now validate exact completed-result report coordinates/policy and own-operation report bytes, with metadata checks around object IO and three Params lock hashes. Unit checks authored, not run; migration not applied. Scheduler installation, claim/start, projection and API/UI integration remain open; 0/7 closed.

P0.1: adopter service independently enables shown-copy with PHOTO_ADOPTER_SHOWN_EXPOSURE_ENABLED=false default, installs own exact-version report authority and polls bounded canonical shown candidates. Candidate tuples are deduplicated before existing advisory locking. CPU exposure enablement does not activate shown-copy. Unit installation/config cases authored, not run. Claim/start, final Apply projection and API/UI remain open; 0/7 closed.

P0.1: 02244 preserves installed claim chain and atomically starts shown Apply only after persisted batch/target/Params scope, ready exact-version declared inputs, live own-operation CPU qualification and exact aggregate unexpired output reservation. Full raw receipt/closure proof remains mandatory worker admission before image IO. Unit checks authored, not run; migration not applied. Final Apply projection and API/UI still open; 0/7 closed.

P0.1: 02245 adds shown-child terminal finalization using the existing cancellation/failure, exact task/target mapping, all-canonical-adoptions, quota settlement, events/outbox and target settlement semantics. Existing scheduler finalization entrypoint preserves old behavior then polls shown children. Unit checks authored, not run; migration not applied. This completes terminal state code only, not rendered-version projection or full P0.1; API/UI and projection remain open; 0/7 closed.

P0.1: 02246 and LoadShownAppliedImage add authenticated read projection of completed/applied shown-copy WorkingImage, requiring canonical attempt/fence, adoption marker, original workflow retention and current editor session. Pointer is not a download grant or new asset version; UI/version integration remains open. SQL unit cases authored, not run; migration not applied; 0/7 closed.

P0.1: 02247 private pre-child publication lock joins authenticated current review, exact selected receipt, deterministic copy-task identity and ready approved-edit document published under original parent. Rejects existing child/task and never creates approval/run/task. Ticket reserve/complete integration remains next; SQL unit checks authored, not run; migration not applied; 0/7 closed.

P0.1: 02248 adds shown-copy Params/chunk/manifest reserve+complete tickets with live selected-receipt publication lock before/after object IO, deterministic per-task IDs, exact Params SHA/byte count, token fencing, immutable ready replay and storage quota accounting. Uses existing backend_documents/artifacts, no child/task dispatch. Unit checks authored, not run; migration not applied. Concrete publisher and API orchestration remain open; 0/7 closed.

P0.1: NewShownExposurePublisher now concretely publishes exact frozen copy Params/chunk/manifest through 02248 tickets and immutable object-store Publisher, retaining parent document scope, deterministic IDs and version/token checks. Constructor owns review fences/plan/candidate SHAs and frozen Params; negative/no-IO unit cases authored, not run. API preparation/registration and version/UI projection remain open; 0/7 closed.

P0.1: ApplyService now routes fully selected previews and prepared shown tasks into dedicated CommitShownExposure after existing idempotency replay; rejects partial selection, missing copy tasks, mixed rerender/copy payloads and unsolicited shown tasks. Historical no-preview branch remains separate. Unit routing cases authored, not run. Concrete deployment preparation must still populate ShownTasks, and UI/version integration remains open; 0/7 closed.

P0.1: PrepareShownExposureWithPublisher compiles once and constructs concrete publication from the exact owned frozen closure after full document ownership and live recheck; shared preparation verifies the same closure for SQL payload. Existing injected-publisher API retained. Missing-authority factory/no-IO case authored, not run. Deployment orchestration and UI/version integration remain open; 0/7 closed.

P0.1: PrepareShownApplyBatch concretely publishes approved Params, exact shown-copy closures and decision, rebuilding approved drafts and checking every target selection/closure before publication. Owns candidate/intent/source document bytes, rechecks authority between phases and verifies final batch before return; creates no child/task. Unit cases authored, not run. Deployment must still resolve physical previews/receipt/source inputs and install this preparation; version/UI integration open; 0/7 closed.

P0.1: installation accepts explicit ShownApplyResolver and wraps preparation: fully selected approvals resolve physically scoped inputs for a fresh child then use concrete shown batch publication; no-preview/rejection requests use historical preparation, partial selection rejects. Original request bytes are re-decoded after resolver callbacks. Resolver implementation in API deployment remains open, so startup does not implicitly enable shown path. Unit case authored, not run; 0/7 closed.

P0.1: ExposurePreviewProvider now exposes ReadPhysicalPreview for server-side Apply resolution: repeats authenticated lookup, full version-pinned physical/presentation validation, receipt sealing and final current-authority recheck without issuing URLs. HTTP ReadPreview shares this physical path before signing. Unit cancellation case authored, not run. Remaining resolver must compose source loading/budgets/batch and deployment installation; 0/7 closed.

P0.1: NewSQLShownApplyResolver concretely composes scoped Apply review, deployment budgets, physical preview+receipt revalidation, exact client render/evidence selection, original source/protection documents and per-target authority rechecks. Rejects missing/duplicate/extra budgets and selection drift. Unit constructor case authored, not run. Deployment must provide copy-operation qualification/budgets and install resolver; source loader currently also applies preview resource bounds, requiring correction before enabling; UI/version integration open; 0/7 closed.

P0.1: source loader now shares all scoped proposal/SHA/version/protection checks but selects operation-specific resource compiler. Shown resolver uses LoadShownSourceDocuments and byte-copy RAM/scratch/output bounds; preview path keeps full replay/display bounds. Added unit case where copy fits but preview does not; authored, not run. Deployment qualification selection/installation and UI/version projection remain open; 0/7 closed.

P0.1: 02249+VerifyShownReviewReport provide authenticated own-operation copy report verification with exact-version body/policy checks around IO. SQL shown resolver now verifies each copy qualification before physical preparation. NewShownBudgetSelection validates copy-specific resources rather than renderer budget. Unit SQL check authored, not run; migration not applied. Deployment config and UI/version projection remain open; 0/7 closed.

P0.1: tenant preparation catalog accepts optional shown_execution with copy resource/pin validation and cross-tenant report/calibration ownership checks. Service installation constructs concrete SQL shown resolver from shown execution + shared physical preview provider, rejecting ambiguous custom resolver/config. Existing catalogs unchanged when omitted. Main startup provider injection and UI/version integration still open; no tests run; 0/7 closed.

P0.1: main startup builds shared physical preview provider before tenant Apply installation and injects it into shown_execution resolver configuration. Existing builder callers remain valid; ambiguous provider injection rejects. Copy budget ownership/cancellation/runtime-substitution tests and startup ambiguity test authored, not run. Operations remain disabled until complete qualification/activation; UI/version projection still open; 0/7 closed.

P0.1: photographer UI now freezes exact displayed render/evidence artifact refs into every approval request, rechecks current preview before capture and preserves the same selected refs/idempotency body on uncertain in-memory retry. Rejections carry no preview. Display gate expectation updated and retry-selection case authored, not run. Durable restart recovery/version projection and operation activation remain open; 0/7 closed.

P0.1: extracted strict decodeSemanticApplyRequest for persisted recovery payloads; shared API submission uses the same decoder and deep-owned selected refs. Exact nested keys, ref nonaliasing, approve-only preview and duplicate target checks retained. Unit recovery-payload ownership/invalid cases authored, not run. Durable journal wiring remains open; 0/7 closed.

P0.1: durable SemanticApplyAttemptStore persists strict owned request/selected preview refs, workflow and idempotency key per origin/user/tenant/intent. Cannot replace unresolved operation; clears only matching acknowledged revision/workflow/child receipt; generation fence prevents post-logout writes. Auth logout now clears this journal. Recovery/conflict/tenant/logout cases authored, not run. UI store initialization/write-before-send/recovery wiring remains open; 0/7 closed.


P0.1: production semantic route resolves authenticated user and supplies scoped durable Apply journal. Decision UI restores frozen body/key before editing, writes before network submission and clears only after receipt verification. Recovery remains reachable with no proposed targets. Added restart/empty-proposals and failed-storage no-send unit cases, authored not run. Definitive-rejection reconciliation, editor/version projection and activation remain open; 0/7 closed.


P0.1: durable Apply journal now records scoped receipt generation before deleting attempt. Other mounted writers are fenced after acknowledgement; restart ignores acknowledged raw record if removal failed. Same store can author a fresh key; previously queued writes cannot cross acknowledgement. Logout removes scope markers. Added competing-writer and crash-between-marker/removal unit cases, not run. Definitive rejection reconciliation and version/editor integration remain open; 0/7 closed.


P0.1: 02250 adds authenticated idempotent materialization of a completed/adopted shown-copy WorkingImage into an accepted asset version with semantic_source lineage, exact raw document SHA and original preservation. Lifecycle/asset locks serialize numbering; deterministic task-derived identity verifies replays. Go materializer owns/validates bytes, checks current retained projection and expected version ID. Boundary unit cases authored, not run; migration not applied. HTTP/object-loading/editor/export integration and broader lifecycle cases remain open; 0/7 closed.


P0.1: 02251 exposes exact-version WorkingImage object evidence only through authenticated completed/adopted result projection. ReadShownAppliedDocument checks configured bucket, byte bounds, SHA and typed image, then repeats authenticated evidence after object IO; version drift rejects. Added object-version race and foreign-bucket/no-IO unit scenarios, not run. HTTP deployment and editor/export navigation remain open; 0/7 closed.


P0.1: ShownVersionService concretely composes authenticated exact-version object read outside transactions, then tenant.WithTenant editor mutation and repeated SQL completed-result authority inside the final transaction. Installation rejects absent dependencies. Nil/cancellation boundary unit cases authored, not run. HTTP mounting/deployment, client action/editor navigation and transaction lifecycle tests remain open; 0/7 closed.


P0.1: bodyless POST /semantic-targets/{target}/version mounted with authenticated mutation request, strict empty body/query, no-store and typed nonzero response. Production startup installs ShownVersionService. API client adds strict scoped call/response ownership; normative endpoint and schema added (110 endpoints). Auth and malformed-client-response unit cases authored, not run. UI action/navigation, export integration and lifecycle coverage remain open; 0/7 closed.


P0.1: applied semantic targets expose localized Open edited photo action. Production route materializes the accepted version and navigates to /editor/[version] with tenant. Action fences duplicate requests, aborts on background/unmount and ignores late navigation; uncertain failures retry the same target. Three navigation/cancellation/retry unit cases authored, not run. Export-source/lineage compatibility, broader AI preview support, activation and lifecycle coverage remain open; 0/7 closed.


P0.1: static render catalog inspection found copied pixels use role pixels, while historical WorkingPixels scope requires working_rgb and same-producer alpha. 02252 preserves the historical scope chain and adds exact accepted semantic-version array authorization: deterministic version/result identity, lineage, metadata child refs, canonical copied pixels and retained NpyArray alpha input, current editor/tenant/object scope. Not applied; database allow/deny lifecycle scenarios and downstream source rechecks still open. 0/7 closed.


P0.1: editor route inspection found asset is required to load drafts/media; initial semantic navigation omitted it. Version service now resolves authorized asset_id from the created accepted version within final tenant transaction, HTTP/client require both nonzero IDs, and production navigation passes asset+version+tenant. Client/UI tests updated; added missing/zero asset rejection scenario, not run. Latest-version versus pinned-version editor semantics still require tracing; 0/7 closed.


P0.1: traced editor draft and V2 load: both already reject route/source version mismatch rather than silently opening latest asset version. Added target-screen regression for applied-only action and exact version+asset navigation; added materializer fixture scenarios for exact raw bytes, deterministic version identity rejection and SHA mismatch stopping before mutation. Authored not run; SQL lifecycle/source-array and full render/export traversal still open; 0/7 closed.


P0.1: first-open tracing identifies explicit remaining gaps: new accepted semantic version has no V2 draft stream/base_style initialization and no standard media derivatives; opening editor alone does not complete render/export. These require supported initialization and derivative generation, not copied foreign drafts. Fixed stale/missing retained projection to ErrConflict (HTTP409), and nullable SQL materialization UUID to conflict rather than scan error/500. Added missing-projection/no-storage unit cases; not run. 0/7 closed.


P0.1: corrected first-V2-draft gap: existing style-based initialization already exists but styles were loaded only for collection routes. Semantic navigation now carries server target project, editor loads styles for project-only entry, and createV2Draft freezes/checks expected opened version+source SHA before publishing recipe. Added changed-source/no-write and mutation-during-binding tests, authored not run. Standard media derivatives and SQL/render/export lifecycle remain open; 0/7 closed.


P0.1: authoritative 00156 already enqueues accepted Apply versions into durable media derivatives; no new origin needed. Editor previously treated MEDIA_NOT_READY as permanent. Added sequential bounded 2s readiness polling (90 attempts), foreground/scope cancellation, exact version selection and terminal handling of auth/unrelated failures. Added pending-to-ready, denial/conflict-stop and late-scope-result unit scenarios, not run. Worker array/input and render/export lifecycle tracing remains open; 0/7 closed.


P0.1: media compiler and worker already bind ordered WorkingImage/RGB/optional alpha; gateway grants only command inputs. Found SQL media registration previously checked only master and general readiness. 02253 preserves old registration chain and restricts shown-version jobs to exact persisted metadata refs, deterministic source/target/task/version identity, completed canonical result, adoption marker and lineage before publication. Migration not applied; SQL allow/deny tests and claim-time lifecycle still open. 0/7 closed.


P0.1: authored media coordinator regression carrying independent copied RGB + retained alpha through all four thumbnail/preview tasks to sealed plan. It also rejects replacement of each root/RGB/alpha input during task preparation. This verifies compiler contract only when later executed; it does not qualify SQL grants or worker rendering. No tests run. SQL lifecycle/claim checks, full export and wider AI preview support remain open; 0/7 closed.


P0.1: 02254 extracts read-only shown-media live-source check and repeats it for every node immediately before media plan activation under tenant/project/asset lifecycle locks; validates active objects, completed canonical source/adoption/lineage and exact RGB/alpha refs. Historical seal chain retained and direct bypass revoked. Migration not applied; worker claim-time gate and SQL allow/deny coverage remain open. 0/7 closed.


P0.1: 02255 wraps complete claim chain for shown-version media tasks. Acquires tenant/project/asset lifecycle locks before task claim, validates running job, exact node/task inputs and live canonical source, then repeats source after claim. Stale exception rolls back lease/capacity mutations. Other operations delegate unchanged; private predecessor worker grant revoked. Not applied; SQL concurrency/denied-source scenarios and worker IO/result-time rechecks remain open; 0/7 closed.


P0.1: 02256 repeats live shown-media source at every gateway input grant and heartbeat. Preserves prior gateway certificate/lease/input allowlist and heartbeat device/qualification chains; stale source returns no input grant and denied renewal. Worker existing lease-loss checks stop work. Private predecessor direct grants revoked. Not applied; commit/finalize source checks and SQL lifecycle/role tests remain open; 0/7 closed.


P0.1: 02257 wraps full canonical success chain for running shown-source media nodes. Locks tenant/project/asset lifecycle, rejects stale source before result commit, and rechecks after committed outcome with transactional rollback on drift. Already-succeeded replay delegates to historical validator; previous device/qualification/manifest/fence checks preserved. Not applied; derivative finalizer and SQL lifecycle/role tests remain open; 0/7 closed.


P0.1: 02258 finalizes shown-version derivative families under tenant/project/asset/job/task locks, with live source checks, exact node/task count and canonical attempt/fence/versioned verified output validation. All family cache writes use one subtransaction; any invalid member rolls back the family and marks job/run failed. Other origins retain historical finalizer chain. Not applied; SQL success/denial/partial-family/concurrency tests remain open, as do render/export and wider AI previews. 0/7 closed.


P0.1: authored rollback-only PostgreSQL probes for 02258 using explicit private completed physical shown-media event fixture, not fabricated successful worker results. Covers whole-family success, deleted asset, expired final derivative (zero partial cache writes), and cancelled media run under actual scheduler role. Requires PHOTO_TEST_V27_SHOWN_MEDIA_EVENT and isolated migrated DB; fixture is not provisioned/qualified. Tests not run. Wider SQL lifecycle/roles, render/export and AI preview scope remain open; 0/7 closed.

### Ограничение ожидания editor preview

P0.4 editor source preview: added independent 8-second request deadline, abort and late-result fence; authored hung-transport and unmount deadline scenarios. No tests executed; full editor trace remains open, 13/70 = 18.57%.

### Срок действия editor source preview

P0.4 editor: source preview now validates expires_at, clears expired bearer before serialized refresh, cancels refresh/deadline on foreground scope exit; authored expiry/refresh/malformed-link scenarios, not run. Complete editor trace remains open; 13/70 = 18.57%.

### Editor source response binding

P0.4 editor source: strict owned decoder rejects foreign/duplicate/missing preview rows, invalid dimensions/MIME/expiry and unsafe URLs; route wired, unit scenarios authored, not executed. 13/70 = 18.57%; complete editor trace remains open.

### Восстановление editor preview после отказа signer

P0.4 editor media response confirmed against media.Link/HTTP handler; source preview now recovers 502/503/504 with bounded exponential retry (five failures maximum), removes old bearer, cancels retry on scope exit. Outage/recovery/exhaustion/cancellation scenarios authored, not executed. 13/70 = 18.57%; full editor trace remains open.

### CAS imported mask decisions: SQL preparation

P0.6.02: confirmed delayed grant after revoke in legacy SQL. Added forward-only 02259 CAS entry points with expected epoch, serialized absent/existing row, exact +1 replay and safe-integer bound. API/client cutover, legacy ACL removal and scenarios remain OPEN; migration not applied. 13/70 = 18.57%.

### Imported mask CAS: server cutover

P0.6.02: server decision HTTP/service now require explicit writable expected_epoch, call 02259 CAS functions, require exact +1 receipt and return 409 on stale CAS. Authored missing/negative/unsafe/max-writable epoch cases. Client/status admission, contract synchronization, legacy API ACL cutover and existing HTTP fixture updates remain open. Tests/migrations not run; 13/70 = 18.57%.

### Imported mask CAS: client and ACL cutover

P0.6.02: client decision admission requires explicit writable expected_epoch, exact successor receipt; candidate templates stay separate from mutation admission. UI derives epoch from loaded confirmation status and saved revoke; missing status blocks new decision. Forward-only 02260 revokes photo_api legacy unfenced functions. Unit epoch scenarios authored. Contract/OpenAPI synchronization, HTTP/SQL fixtures, ambiguous retry and full CAS trace still open; 13/70 = 18.57%. No tests or migrations executed.

### Imported mask CAS: normative schema and HTTP scenarios

P0.6.02: normative mask mutation schemas now require expected_epoch with writable safe bound; grant candidate template remains unfenced/read-only via separate GrantDecision schema. HTTP fixtures updated, authored missing/null/negative/max epoch admission and non-successor receipt scenarios. Existing OpenAPI refs resolve updated schema without projection changes. SQL role/replay integration and ambiguous-retry recovery remain open. No tests/migrations executed; 13/70 = 18.57%.

### Imported mask CAS: SQL role scenarios

P0.6.02: SQL integration scenarios updated for 02260: API may execute only CAS functions, all service roles denied legacy entry points; absent/foreign scopes and nil/negative/zero/unsafe epochs covered. Service snapshots expected_epoch before transaction IO. No tests or migrations run; real grant/revoke/regrant fixture and ambiguous retry remain open, 13/70 = 18.57%.

### Imported mask CAS: immutable uncertain attempt

P0.6.02: mask screen now retains owned immutable CAS attempt across request failures, rejects a different body/epoch until exact acknowledged retry, and clears only after validated receipt and uncancelled signal. Shared attempt helper and mutation/receipt ownership scenario authored. Durable restart recovery and retry independent of expired preview remain open; no tests executed, 13/70 = 18.57%.

### Imported mask exact retry without preview

P0.6.02: dedicated localized saved-mask retry works without preview/candidate/status catalogue and replays owned exact epoch/body; new decisions blocked while unresolved. Added DOM lost-revoke-response/exact-retry scenario. Durable restart journal and real SQL grant/revoke/regrant remain open. No tests/migrations run; 13/70 = 18.57%.

### Durable import-mask journal

P0.6.02: authored durable import-mask journal scoped by origin/principal/tenant/project/workflow, serialized origin lock, immutable body/key writes, successor-receipt-only clear, 20 MiB bounded decode and logout generation fencing. Added restore/principal/conflict/receipt/logout scenarios; logout cleanup installed. Screen startup/write-before-submit wiring still open; no tests run, 13/70 = 18.57%.

### Imported mask durable screen wiring

P0.6.02: mask screen loads authenticated principal and durable journal before mutation admission, restores saved immutable attempt/key, writes before HTTP with cancellation/foreground recheck, clears only acknowledged receipt. Journal unavailable blocks submission; DOM mocks updated. Real SQL lifecycle fixture, durable screen race scenarios and definitive-conflict reconciliation remain open. Tests/migrations not run; 13/70 = 18.57%.

### Imported mask recovery lifecycle scenarios

P0.6.02: authored durable restore with unavailable catalogue and background-during-journal-write DOM scenarios; fixed older request expectations to include expected_epoch. Screen now aborts in-flight mutation on background while retaining journal/immutable retry. No tests/migrations run; SQL lifecycle and definitive-conflict reconciliation remain open, 13/70 = 18.57%.

### Imported mask journal acknowledgement fence

P0.6.02: durable mask journal acknowledgement now writes scoped generation/tombstone before removing bytes; old instances and acknowledged keys cannot resurrect a completed attempt, stale bytes after interrupted removal are ignored. Authored two-instance and interrupted-removal scenarios. No tests run; SQL lifecycle and definitive conflict remain open, 13/70 = 18.57%.

### Imported mask CAS SQL lifecycle

P0.6.02: authored rollback-only SQL integration lifecycle on explicit isolated imported fixture: first grant/exact replay, revoke/replay, delayed grant denial, explicit regrant, delayed revoke denial, final revoke and invalid epoch denial. Uses explicit fixture DSN rather than fresh empty suite DB. Not run; fixture not provisioned/qualified. Definitive-conflict reconciliation still open, 13/70 = 18.57%.

### Imported mask superseded CAS reconciliation

P0.6.02: added superseded-attempt reconciliation only after exact domain 409 plus authenticated status read; journal retires exact tuple/key only when status epoch strictly exceeds expected+1, writes tombstone before removal. Missing/equal/foreign status cannot clear attempt; unit scenarios authored. HTTP success not fabricated, user must make a new decision. Screen conflict DOM/trace audit remain open. No tests run; 13/70 = 18.57%.

### Imported mask conflict DOM and coding trace

P0.6.02: authored DOM exact-conflict/newer-status reconciliation scenario without fake success; route inventory includes new authenticated me dependency. Saved source trace identifies remaining equal-successor conflict and journal-load error recovery UI. No tests executed; 13/70 = 18.57%.

### Imported mask consumed epoch and journal reload

P0.6.02: journal error now exposes localized reload preserving saved bytes; authored DOM storage-failure/reload scenario. Conflict retirement permits exact matching authenticated status at >= expected+1 because this CAS epoch is consumed; never invents success receipt. Unit boundary scenario updated. Final caller audit/no-op decision admission remains open; 13/70 = 18.57%, tests/migrations not run.

### Imported mask no-op admission

P0.6.02: no-op mask grant/revoke no longer creates durable attempt; admission uses exact current tuple/status/epoch and rejects exhausted epoch. Authenticated conflict retirement handles same expected epoch only if desired status already holds, since CAS cannot mutate that state. Added transition/no-op/exhaustion and same-epoch retirement unit scenarios. Final static caller/fixture review pending; no tests run, 13/70 = 18.57%.

### P0.6.02 кодово закрыт, без тестового PASS

P0.6.02 coding_closed_unverified after current-source caller/ACL/schema/journal trace review; authored repeated grant DOM admission scenario. CAS/delayed replay, durable exact retry, conflict/no-op reconciliation and logout fencing have code plus authored scenarios. 14/70 = 20.00% (+1 unit, +1.43 percentage points). No tests/typecheck/migrations executed; real fixture must be provisioned in deep test phase. Larger P0.6 block remains open.

### Imported mask acknowledgement cancellation race

P0.6.02 follow-up static audit fixed acknowledgement/background race: after durable receipt/superseded clear succeeds, memory attempt retires before cancellation check, preventing tombstoned retry deadlock. Authored disk-full write/no-HTTP/exact retry and background-during-clear DOM scenarios. 14/70 = 20.00%, no extra unit counted. Tests not run.

### Semantic Apply durable lifecycle fences

P0.1 semantic Apply: added foreground admission for reject/retry/refresh, background transport abort, post-journal foreground check, retained retry visibility on cancellation; acknowledged journal clear now retires memory before cancellation check. Authored background-during-durable-write recovered Apply scenario. No tests run; 14/70 = 20.00%, no additional unit counted.

### Semantic Apply journal reload

P0.1 semantic Apply: authored localized durable-journal reload after factory/read failure; mutation stays gated until successful restoration, saved request/key preserved, no automatic submission. DOM storage outage/reload scenario authored. No tests executed; 14/70 = 20.00%, no additional unit counted.

### Shown-media fixture and manifest failure scenario

P0.1 shown-media finalizer SQL scenarios: corrected fixture connection to explicit isolated PHOTO_TEST_DATABASE_URL rather than fresh empty conftest DB; rollback outer scope preserved, missing fixture skips before DB access. Added expired-last-manifest full-family rollback case. No tests/migrations executed; 14/70 = 20.00%, no additional unit counted.

### Shown-media variant publication bounds

P0.1 shown-media: static finalizer audit found missing variant-specific dimension cap and node asset-version check. Forward-only 02261 adds both before atomic family publication, preserving existing fences and ACL. Authored oversized-last-output full-family rollback scenario. Historical finalizer has no separate quota settlement, so no unsupported new quota claim made. No tests/migrations run; 14/70 = 20.00%.

### Non-exposure semantic candidate preservation

P0.1.09: current contracts contain remove_object/background_cleanup/retouch_region beyond local_exposure. Added authority-neutral candidate preservation verifier with bounded tile workspace, cancellation, finite nonnegative HDR, bit-exact outside-mask/protected RGB and alpha preservation; authored changed-region/HDR/protection/alpha/admission/cancellation scenarios. This is a building block, not an installed preview producer, quality PASS or model qualification; full non-exposure preview remains open. 14/70 = 20.00%, tests not run.

### Production inpaint candidate preservation proof

P0.1.09: connected candidate preservation verifier to production image.inpaint before scratch output flush/staging; full-canvas outside/protected proof and independent changed-pixel count now required. Binary masks accepted without full float copies; bounded 8 MiB verifier workspace uses existing fixed frame allowance. Added bool-mask preservation scenario. Existing model/color/blend path preserved; broader semantic preview producer/admission still open. Tests not run; 14/70 = 20.00%.

### Shared pre-Apply candidate blend kernel

P0.1.09: confirmed InpaintParams requires confirmed=true, so it cannot be fabricated for pre-Apply preview. Extracted common numerical blend_candidate_region without approval fields; production inpaint imports it under compatible _blend_selected name. Existing arithmetic preserved, ROI bounds/weight checks added; authored HDR/zero-bit preservation/ROI/invalid-weight scenarios. Separate preview contracts/admission/producer remain open; 14/70 = 20.00%, tests not run.

### Separate removal proposal preview contract

P0.1.09: added typed SemanticRemovalPreviewInput (remove_object/Big-LaMa/context/feather, no confirmation/approval) over shared proposal scope base. Extracted common exact-byte candidate/intent binder, preserving exposure wrapper wire format; separate removal binder rejects exposure intent. Authored no-approval/model-policy/strict-int/hash scenarios. SQL/Go contract projection, loader and producer remain open; no tests run, 14/70 = 20.00%.

### Removal preview source geometry protection binding

P0.1.09: generalized exact source/geometry/protection binders behind typed exposure/removal wrappers, preserving old exposure signatures. Removal validates exact WorkingImage, transferred mask/homography/reference bytes and strict canonical protection origin without approved edit. Authored geometry hash and origin substitution cases. Array loader/compute/publisher/SQL still open; no tests run, 14/70 = 20.00%.

### Removal preview verified array loader

P0.1.09: extracted common artifact/NPY preview loader and kept exposure wrapper; added typed removal array loader with source/geometry/protection/ICC checks, owned arrays, pre-array RAM admission, post-IO reauthorization and mapped-owner cleanup. Authored real-NPY success and admission/origin/color/budget denial scenarios. Compute/producer/publisher/SQL remain open; no tests run, 14/70 = 20.00%.

### Shared removal candidate inference

P0.1.09: extracted shared numerical Big-LaMa inference and mask ROI from production inpaint, preserving padding, padded-pixel admission, model-output validation and compatibility aliases. Authored padding/crop, pre-inference budget denial, invalid outputs and ROI boundary scenarios. Removal compute/producer/publication remain open. Tests not run; 14/70 = 20.00%.

### Shared removal region computation

P0.1.09: extracted shared production region compute (wide-gamut model delta, global pass, feather/protection) and wired confirmed inpaint through it; cancellation fences surround model inference, padded ROI budget checked before numerical allocation. Authored protection/weight, cancellation and padding-admission scenarios. Preview orchestrator/publication still open; no tests run; 14/70 = 20.00%.

### Private removal preview computation

P0.1.09: added byte-backed removal preview orchestrator and private compute with conservative RAM admission, model-call admission fencing, no HDR clipping, protected/outside exact-bit proof and changed-count agreement. Authored preservation, budget, post-model admission loss, HDR and fractional protection scenarios. Publication/installation/SQL/Go and other AI operation previews remain open. Tests not run; 14/70 = 20.00%.

### Removal intent strength binding

P0.1.09: static trace found removal preview ignored EditIntent.strength; fixed artifact orchestrator to bind exact intent bytes and pass strength to numerical blend. Added zero/fractional/invalid-strength scenarios, retained-loader memory accounting and strict loader budget metadata. Publication and operation installation remain open; no tests run; 14/70 = 20.00%.

### Removal attempt pixel staging

P0.1.09: added removal-specific attempt WorkingImage staging using existing bounded numerical writer, exact source binding, independent admission before/after writes and input/source-pixel/alpha artifact alias rejection. Authored NPY-byte values, source metadata retention and pre-write denial scenarios. Evidence/presentation/publication/installation remain open; tests not run; 14/70 = 20.00%.

### Removal numerical evidence staging

P0.1.09: added removal-specific numerical evidence contract and compute-to-pixels/evidence staging with exact evidence SHA, distinct output IDs, budget and post-write admission fencing. Evidence includes changed count/ROI/global pass and strict preservation flags; does not claim photo qualification or Apply. Authored contract invalid-proof/ROI/count/alias scenarios. Presentation/publication/installation remain open; tests not run; 14/70 = 20.00%.

### Removal presentation linkage

P0.1.09: added removal-specific presentation contract and typed staging wrapper over shared exact-evidence/media linkage writer; exposure signature preserved. Authored exact evidence SHA/output linkage and duplicate-media rejection scenario. Display-media preparation and end-to-end publication/installation remain open; tests not run; 14/70 = 20.00%.

### Removal display media preparation

P0.1.09: added removal before/after display-media preparation using existing locked ICC conversion and no-upscale 2560 policy; exact preservation rechecked on discrete mask/protection before display, conservative overlays use same editing region. Authored owned before/after/overlay and outside-mask rejection scenarios. Complete bundle/publication/installation remain open; tests not run; 14/70 = 20.00%.

### Complete removal attempt bundle

P0.1.09: assembled complete removal attempt path from verified compute through WorkingImage/evidence, before-after/overlays and presentation staging, with conservative full-bundle budgets. Extracted typed shared alpha loader preserving exposure API and inherited opacity parsing. Authored complete-bundle pre-compute resource denial scenarios. Canonical SQL/Go publication, worker installation and other operation previews remain open; tests not run; 14/70 = 20.00%.

### Explicit removal worker adapter

P0.1.09: added explicit removal worker adapter/registration with distinct installation and qualification-pin types, backend/profile/scope/input/lock checks, mandatory complete presentation and qualified ModelPool borrow (no unqualified fallback). CPU/CUDA/MPS supported by adapter; SQL/report installer and production activation still open. Authored cross-installer rejection and explicit backend registration scenarios. No tests run; 14/70 = 20.00%.

### Removal report qualification policy

P0.1.09: added exact removal qualification-report verifier with mandatory real-photo/preservation/color/global-pass/resource/cancellation checks and complete policy fixture coverage. CUDA report cap 10,000,000,000 bytes; Metal has no fixed cap; CPU device bytes zero. Authored backend memory and repinned-policy downgrade scenarios. No reports qualified or tests run; SQL installer/publication remain open; 14/70 = 20.00%.

### Removal runtime installer factory

P0.1.09: added removal-specific SQL/runtime installer factory, exact canonical params/task pins and backend matching, distinct SQL admission/protection/report methods, exact-version report loader with removal verifier and post-IO authority recheck. Mandatory presentation; no exposure installer fallback. Authored missing-authority/pin/backend and exposure-method substitution rejection scenarios. SQL methods/migrations and production wiring still open; tests not run; 14/70 = 20.00%.

### Removal worker database bridge

P0.1.09: wired TaskDatabase removal admission/report/protection methods with dedicated SQL names and bound exact lease/fence/body/digest/root/binary arguments; strict true admission only. Authored SQL-argument and truth-coercion scenarios. SQL implementations not yet present, so production installation remains disabled; no tests run; 14/70 = 20.00%.

### Removal canonical protection SQL

P0.1.09: added forward-only removal protection-origin SQL using existing full canonical transfer/result/task/target/source protection provenance, dedicated removal admission and worker-only ACL. Authored explicit-DSN role/definer integration scenario. This migration depends on forthcoming removal admission function and is not independently deployable; no migrations/tests run. Admission/closure/transport/producer/publication remain open; 14/70 = 20.00%.

### Removal persisted input closure

P0.1.09: added dedicated removal SQL inventory digest, transport reconstruction and persisted closure predicates. Removal operation/UUID namespace/resource class/config are distinct; complete input SHA/order/manifest/chunk/source checks retained. Authored operation-digest cross-substitution integration case. Admission/producer/publication still open; migrations/tests not run; 14/70 = 20.00%.

### Removal worker SQL admission and report

P0.1.09: added forward-only removal worker SQL admission and exact report-evidence functions. Admission preserves tenant/run/task/slot/session/revision/source/model qualification gates, supports backend-specific policy CPU/CUDA/MPS, validates removal controls and requires persisted removal closure. Protection function dependency now authored. Added unscoped/invalid lease denial integration scenarios. Producer/registry/claim/publication remain open; migrations/tests not run; 14/70 = 20.00%.

### Normative removal preview schemas

P0.1.09: added normative JSON schemas for removal preview input, preservation evidence and complete presentation with separate references/algorithm and strict controls; existing exposure schema unchanged. Worker scenarios already cover invalid controls/evidence/aliases. Backend producer/DTO readers, SQL activation and publication remain open; tests not run; 14/70 = 20.00%.

### Backend removal parameter DTO

P0.1.09: added Go removal preview parameter DTO, canonical freeze and bounded strict canonical parser; shared source/ref/revision validation retained, removal controls and model fixed. Authored canonical roundtrip, noncanonical/unknown/boolean control rejection and unsupported model/bounds/alias cases. Producer/publication still open; tests not run; 14/70 = 20.00%.

### Backend removal dependency inventory

P0.1.09: added Go removal document resolver and deterministic complete input inventory (separate operation UUID/digest), sharing source/mask/protection dependency resolution without ApprovedEdit. Extended authored real WorkingImage fixture scenario for removal namespace/closure and exposure-intent rejection. Resource/profile task builder and producer/publication remain open; tests not run; 14/70 = 20.00%.

### Backend removal resource and task profile

P0.1.09: added Go removal resource compiler and frozen task/profile builder with separate qualification type, complete dependency inventory, model-lock ID and measured device budget. CUDA qualification cap 10 GB; Metal measured admission without fixed cap; CPU zero device allocation. Authored backend/cap/insufficient-device-budget scenarios. Producer atomic commit/claim/publication remain open; tests not run; 14/70 = 20.00%.

### Removal typed publication and commit projection

P0.1.09: introduced distinct FrozenRemovalPreviewTask and explicit publication/replay verification/commit-input wrappers over shared compiler-owned transport. Authored real document/task compile on Metal >10GB plus empty/cancelled publication rejection. Durable removal-scoped publisher, run/quota atomic materialization and result publication remain open; tests not run; 14/70 = 20.00%.

### Removal durable document publisher

P0.1.09: added removal-scoped durable document publisher using existing ticketed immutable object IO, distinct removal reserve/complete SQL names, exact Params bytes and deterministic task/object identity checks. Authored foreign task/contract/ordinal/cancellation pre-IO rejection. SQL publication lock/ticket implementations and run materialization remain open; tests not run; 14/70 = 20.00%.

### Removal SQL durable publication tickets

P0.1.09: added removal publication-lock and durable document reserve/complete SQL migrations. Live review/session/target/run authority, exact Params/config, deterministic removal owner IDs, versioned ready artifacts and storage quota accounting preserved. Authored API-only ticket ACL and exposure-task namespace rejection integration scenarios. Run save/quota/atomic graph admission and result publication remain open; no migrations/tests run; 14/70 = 20.00%.

### Saved removal preview run

P0.1.09: added authenticated deterministic saved removal preview run SQL and Go bridge with distinct run/config namespace, exact params/session/target/review fences and immutable replay conflict behavior. Authored pre-IO cancellation/scope and exposure-param SQL rejection scenarios. Resource reservation/atomic graph admission and result publication remain open; no migrations/tests run; 14/70 = 20.00%.

### Removal quota reservation

P0.1.09: added removal quota SQL/Go with frozen backend profile, full source/display budgets and atomic storage reservation/replay. CPU zero device bytes; CUDA <=10GB; MPS measured without fixed cap. Static trace corrected report SQL downstream helper class argument to gpu (not backend). Authored pre-IO quota scope/cancel scenarios. Atomic graph admission/claim/result publication remain open; no migrations/tests run; 14/70 = 20.00%.

### Backend removal preparation orchestration

P0.1.09: added removal review-to-params compiler and complete backend preparation orchestrator: owned canonical review/source docs, current authority, saved run, resources/quota, durable input publication, independent replay verification and commit projection. Controls/runtime costs remain server-selected. Authored invalid-scope/cancel-before-IO preparation scenarios. Atomic graph commit/activation and result provider remain open; tests not run; 14/70 = 20.00%.

### Complete removal SQL input admission gate

P0.1.09: authored complete removal SQL pre-materialization aggregate gate and physical documents/protection/sidecar/image/exact-inventory helpers. Dedicated removal transport/config/intent with null EV; source canonical producer provenance and storage quota locks retained. Authored incomplete graph rejection integration scenario. Task materialization/sealing/activation and result provider remain open; no migrations/tests run; 14/70 = 20.00%.

### Removal task materialization and input registration

P0.1.09: added removal SQL run-current fence, private draft task materialization, input scope and exact registration; extended latest shared artifact-link guard retaining existing workflow/Apply/exposure/shown paths. Authored materialization private-ACL scenario. Sealing/activation/claim and result provider remain open; no migrations/tests run; 14/70 = 20.00%.

### Removal atomic graph activation

P0.1.09: added same-transaction removal registration/sealing/activation and narrow API commit SQL entrypoint plus Go commit bridge with exact params/documents and deterministic returned task identity. Authored commit scope/cancel and narrow-entrypoint ACL scenarios. Claim/readiness hooks, recovery/service wiring and result provider remain open; no migrations/tests run; 14/70 = 20.00%.

### Removal uncertain commit recovery

P0.1.09: added removal SQL/Go exact-request inspection and admission orchestrator with stable run identity, empty-draft-only resume and persisted sealed-run recovery; commit attempted identity retained on errors. Authored invalid transform/review/cancellation before-IO scenarios. Service source loader, claim/registry and result provider wiring remain open; no migrations/tests run; 14/70 = 20.00%.

### Removal admission service and source loader

P0.1.09: added removal admission service and source-document loader using authenticated canonical object metadata, exact bytes/SHA and repeated current review authority. Server-owned backend pins, measured costs and context/feather controls feed complete removal admission/recovery. Authored uninstalled-service and cancellation scenarios. HTTP routing/worker startup/registry/claim and result provider remain open; tests not run; 14/70 = 20.00%.

### Removal operation registry and claim gate

P0.1.09: added disabled removal operation/output catalog (Big-LaMa CPU/CUDA/MPS) and claim wrapper around latest installed claim chain, preserving prior media/signing gates. Claim repeats removal closure/current authority/backend profile/quota and starts run atomically. Authored disabled-registry and private delegated-claim ACL scenarios. Worker startup/heartbeat/gateway/finalizer/result provider remain open; no migrations/tests run; 14/70 = 20.00%.

### 2026-10-15: детализация отчётности по запросу пользователя

Закреплена политика взвешенных подпунктов в mvp-coding-progress.json и mvp-coding-checklist.md. Дробление сохраняет суммарный вес исходного пункта, включает оставшуюся интеграцию и требует свидетельств кода и написанных сценариев. Частичная P0.1.09 пока без дополнительного числового зачёта: декомпозиция и аудит свидетельств ещё не зафиксированы. Текущий зачёт **14/70 = 20,00%**; тесты не запускались.

### P0.1.09 — live IO удаления объектов

Добавлена 02276_v27_removal_preview_live_io.sql: проверка текущей review authority, sealed running run, отмены, closure, deployment profile и срока квоты перед gateway_input и heartbeat_task. Предыдущие делегаты сохранены и закрыты от прямого вызова прикладными ролями. Написаны SQL integration-сценарии ACL и отсутствующей задачи. Миграция не применялась, тесты не запускались. Finalizer/result provider/worker startup остаются открытыми. Числовой зачёт не изменён: 14/70 = 20,00%; полный пункт P0.1.09 ещё не закрыт.

### P0.1.09 — canonical removal result

P0.1.09: added canonical removal presentation/sidecar metadata readers (same task/attempt/fence, exact presentation bytes/hash, seven distinct outputs) and result commit wrapper retaining previous chain. Fresh result publication locks review lifecycle, target and run and repeats live authority; successful replay delegated unchanged. Authored narrow API/source and private-result-delegate ACL scenarios. No tests/migrations executed; result Go provider/startup and remaining operation previews open; 14/70 = 20.00%.

### P0.1.09 — removal presentation reader

P0.1.09: added Go removal presentation document reader using canonical removal params and dedicated SQL source. Shared bounded byte/hash and before/after producer-fence verification extracted with explicit expected task UUID. Authored changed producer, revocation, cancellation, corrupt/truncated object and wrong exposure-operation namespace scenarios. This is document verification only, not complete display provider. Tests not run; 14/70 = 20.00%.

### P0.1.09 — removal numerical evidence decoder

P0.1.09: added strict Go removal numerical-evidence decoder: exact parameter digest, algorithm, preservation flags, integer changed count and overflow-safe ROI area check, distinct input/output refs and object SHA. Authored request/alias/flag/ROI/strict boolean/missing/null/unknown/hash rejection scenarios. Numerical declaration is not independent photo qualification. No tests run; remaining presentation linkage/provider/startup open; 14/70 = 20.00%.

### P0.1.09 — removal presentation linkage

P0.1.09: added removal presentation decoder and canonical sidecar/evidence linkage reader. Exact params, ICC/sRGB, numerical refs, distinct output IDs, bounded role/media/version metadata and post-IO canonical result/attempt/fence recheck. Authored decoder mutations and cancellation/missing-authority scenarios. Physical image/PNG verification and complete provider/startup remain open. No tests run; 14/70 = 20.00%.

### P0.1.09 — removal physical RGB

P0.1.09: added removal WorkingImage binding and streaming physical float32 RGB NPY verification. Source metadata must remain identical except pixels; ROI bounded to source canvas, changed count bounded to ROI; exact versions/SHA, display dimensions and post-IO canonical linkage rechecked. Authored metadata/pixels/source/ROI rejection and cancellation scenarios. Physical PNG reconstruction, full provider and startup remain open. No tests run; 14/70 = 20.00%.

### P0.1.09 — removal display set and response

P0.1.09: added removal physical display-set reader reusing strict PNG integrity/encoding/profile/dimensions/role verifier one image at a time, with final canonical linkage recheck. Added response builder signing exact immutable versions for one minute with repeated live authority; quality status remains photographer review and limitation explicitly model-generated. Authored no-authority physical read and unverified-before-signing scenarios. PNG pixel reconstruction is not independently proved by this verifier; complete provider/receipt/adoption/startup remain open. No tests run; 14/70 = 20.00%.

### P0.1.09 — removal display provider and resolver

P0.1.09: added display-only removal provider and SQL resolver/lookup. Lookup binds authenticated request to an existing sealed current run, rejects ambiguity, pins source object; provider rebinds tenant/intent/target/revisions/digests, verifies full physical set and signs with repeated live authority. Apply receipt remains explicitly separate and unfinished. Authored dependency/nil/cancellation provider scenarios. No tests/migrations run; 14/70 = 20.00%.

### P0.1.09 — removal canonical receipt

P0.1.09: added distinct removal receipt column, narrow API sealer and Go immutable receipt validation. Sealer locks review/target/run, verifies presentation/evidence/image exact SHA and seven canonical sidecars; strict removal algorithm/preservation/global/count/ROI checks with numeric-safe area. Receipt replay rejects producer or digest changes. Authored receipt mutation and sealer ACL scenarios. Provider-to-receipt integration and shown Apply/adoption remain open. No tests/migrations run; 14/70 = 20.00%.

### P0.1.09 — provider seals removal receipt

P0.1.09: removal provider now seals the exact canonical receipt after physical verification and before signing, returns a distinct server-resolved removal selection for future Apply transaction locks, then repeats live authority. Hardened removal receipt decoder against nil/invalid/aliased artifact refs; updated valid fixture and authored rejection before DB IO. Apply transaction integration/adoption/startup remain open. Tests not run; 14/70 = 20.00%.

### P0.1.09 — removal selection transaction locks

P0.1.09: added removal-specific Apply selection lock bridge and narrow admission SQL precondition. Locks current review/target/run/task, resolves exactly one shown receipt, matches canonical result/attempt/fence and physical artifact readiness, rejects ambiguity/stale authority. Go validates request/review/candidate and distinct receipt refs. Authored cancellation-before-TX and admission-only ACL scenarios. Not connected to complete batch/adoption yet; no tests/migrations run; 14/70 = 20.00%.

### P0.1.09 — shown removal adoption contract

P0.1.09: added distinct shown-removal copy Params contract in Go/Python/normative schema and compiler joining approved remove_object edit, physical preview and exact locked receipt. Separate deterministic removal task namespace; distinct identities/refs, immutable canonical Params SHA, no rerender. Authored Params and compiler ownership/producer mutation scenarios adapted from shown exposure path. Copy task closure/worker/SQL batch integration remain open. No tests run; 14/70 = 20.00%.

### P0.1.09 — removal shown-copy worker

P0.1.09: added independent removal shown-copy binding/executor/explicit CPU operation adapter, reusing exact-byte stage without inference. Binds approved edit kind/strength/prompt/geometry and canonical removal evidence; repeated admission/runtime/color checks. Added independent output verifier module (not yet connected to generic adopter). Authored deny-before-document and wrong-operation/backend/no-installer scenarios. SQL closure/batch, qualification installer and startup still open; no tests run; 14/70 = 20.00%.

### P0.1.09 — shown removal frozen task and inventory

P0.1.09: added shown-removal full input inventory, frozen CPU task compiler and publication transport bridge. Closure includes approved edit, preview/evidence, all original/source/reference/mask/protection/geometry/alpha and display artifacts; rebuilds immutable compilation and checks removal strength/prompt with EV absent. Shared exact-byte resource budget reused. Authored altered/missing closure, wrong operation, empty task and cancelled publication scenarios. SQL batch/installer/startup/adopter remain open; no tests run; 14/70 = 20.00%.

### P0.1.09 — shown removal qualification installer

P0.1.09: added separate shown-removal CPU qualification pin/report verifier and explicit copy installer with exact lease/Params/backend/model/runtime/device/dataset and declared-input checks before report IO. Added distinct database bridges for shown-removal admission/report SQL. Those SQL functions are not implemented yet: installer remains unregistered/unusable until that dependency closes. Authored six invalid-scope-before-IO scenarios and wrong-pin rejection. No tests run; 14/70 = 20.00%.

### P0.1.09 — shown removal SQL worker admission

P0.1.09: implemented shown-removal SQL dependencies: immutable Params/receipt binding, digest and transport reconstruction, persisted closure, lease admission and operation-specific qualification report evidence. Dedicated removal namespaces/contracts/receipt; shared shown_semantic node container reserved for atomic Apply. Checks tenant/worker principal/slot/fence/deadline, approved target batch, editor session, canonical preview result and quota. Authored narrow worker ACL and invalid-body rejection scenarios. No activation/materialization/batch integration yet; no tests/migrations run; 14/70 = 20.00%.

### P0.1.09 — independent removal adopter path

P0.1.09: connected removal output verifier to generic adopter with mandatory distinct admission callback, staged WorkingImage root and inherited alpha/geometry handling. Added opt-in adoption-service removal installer, discovery and dedicated commit route with owned pinned JSON. Added result-scoped admission/qualification/commit modules. Required SQL adoption_context/params/ready/report/adopt_shown_removal still missing; installation defaults absent, so path is not activated. Authored missing-context bypass rejection and inactive-before-IO scenarios. No tests run; 14/70 = 20.00%.

### P0.1.09 — removal adoption discovery/readiness

P0.1.09: implemented removal-copy adoption result Params lookup, discovery/pending readers, result-scoped readiness and qualification evidence SQL. Scheduler-only entrypoints bind exact succeeded canonical attempt/fence, active project/tenant, shown batch node, original removal receipt, editor session, profile and quota; no running worker lease required after success. Authored five scheduler-only ACL checks and missing-task discovery rejection. Dedicated final commit/admission batch/startup remain open. No tests/migrations run; 14/70 = 20.00%.

### P0.1.09 — dedicated removal adoption commit

P0.1.09: implemented dedicated shown-removal adoption commit with exact WorkingImage/pixel-copy SHA/size/provenance binding, lifecycle/artifact locks, canonical inventory retention and inbox/event/outbox idempotency. Wrapped latest generic adopt_v2_plan chain to reject removal bypass while preserving exposure guard. Private output/inventory delegates have no application ACL; narrow commit scheduler-only. Authored ACL/bypass and absent-task rejection scenarios. Atomic Apply batch/materialization/startup remain open; no tests/migrations run; 14/70 = 20.00%.

### P0.1.09 — removal shown-copy document publication

P0.1.09: added separate removal shown-copy document publisher and SQL pre-child publication locks/reserve/complete functions. Exact Params SHA, child/task namespace, canonical receipt and ready approved-edit artifact are reauthorized around object IO; durable tickets retain quota/idempotent replay. Authored empty scope/wrong task/cancel-before-IO scenarios. Atomic batch/materialization/startup remain open; no tests/migrations run; 14/70 = 20.00%.

### P0.1.09 — prepared shown removal batch content

P0.1.09: added prepared shown-removal task composition, publication verification and SQL payload projection, owned exact document bundle, and full batch content verifier against explicit approvals/private review. Publication waits are fenced by live rechecks; all JSON owned before callbacks. Authored profile/operation/transport/body mutation projection and no-authority/incomplete-bundle scenarios. Atomic SQL batch commit/materialization and application routing remain open; no tests run; 14/70 = 20.00%.

### P0.1.09 — removal SQL content preflight

P0.1.09: added removal SQL content preflight: exact dependency projection, physical input/transport readiness, CPU profile/resource shape, approved removal edit binding (EV null, exact strength/prompt/geometry) and locked document admission with canonical hashes. Numerical proof must match sealed receipt documents; no activation granted. Authored admission-only ACL and empty-image profile rejection scenarios. Atomic Apply batch/materialization/startup remain open; no tests/migrations run; 14/70 = 20.00%.

### P0.1.09 — removal batch commit payload

P0.1.09: added distinct removal batch commit payload projection. Rebuilds private approved task drafts and verifies decision publication, selected preview, prepared copy tasks and owned document/transport payloads before serializing rows. Authored missing-approved-copy and unpublished-decision rejection scenarios. Atomic SQL materialization/activation and service routing remain open; no tests run; 14/70 = 20.00%.

### P0.1.09 — removal decision batch locks

P0.1.09: added private removal decision-batch lock and exact copy-node membership gate. Decision publication/request/private source proof, proposed targets and each canonical removal preview selection lock before child creation; one task per approval, no tasks for rejects, deterministic removal namespace and exact transport reconstruction. Authored private ACL and empty/unmatched-node rejection scenarios. Draft/quota/registration/activation/final review commit still open; no tests/migrations run; 14/70 = 20.00%.

### P0.1.09 — removal reserved draft

P0.1.09: added private removal Apply draft shell, persisted draft equality gate, exact aggregate output storage quota and atomic shell+reservation component. Complete decision/receipt/document batch checked before blocked task/target mutations; quota refusal rolls statement back. Replay never extends quota expiry. Authored private-component ACL and empty-draft rejection scenarios. Input registration/seal/activation/final review commit still open; no tests/migrations run; 14/70 = 20.00%.

### P0.1.09 — removal input registration/seal

P0.1.09: added removal Apply registration scope, exact input registration, persisted sealed-input equality gate and seal function. Extended latest artifact-link guard retaining all previous scopes. Complete ordinal inventory + chunk/manifest validated, altered/partial replay rejected; tasks remain blocked, run draft. Authored private registration/seal ACL and missing-task scope rejection. Activation/final review commit/routing remain open; no tests/migrations run; 14/70 = 20.00%.

### P0.1.09 — removal atomic SQL review commit

P0.1.09: completed removal-only SQL atomic review mutation composition: idempotency lock, decision/receipt/document checks, reserved draft, input registration/seal, activation, parent revision, response/audit/events in one statement. Added Go CommitShownRemoval with idempotent replay and exact published batch proof. Private activation/finish and admission-only complete commit ACL; authored ACL and missing-TX scenarios. Application routing/mixed-operation batches/runtime claim and activation still need integration; no tests/migrations run; 14/70 = 20.00%.

### P0.1.09 — removal full batch preparation

P0.1.09: added removal-only full batch preparation composing approved Params publication, exact copy task compilation/publication/verification and immutable decision publication. Every target validated before first write; owned documents and repeated live recheck around IO. Distinct PreparedRemovalApply prevents accidental exposure/rerender dispatch. Authored cancellation/missing-dependencies scenarios. Application routing/mixed batch/runtime startup/accepted version integration remain open; no tests run; 14/70 = 20.00%.


### Детальный учёт кодирования — 2026-10-15

Общий знаменатель сохраняется: 70 единиц, закрыто 14, **20,00%** до глубокого тестирования. Для активного пункта P0.1.09 зафиксированы 30 равновесных подпунктов: remove_object, background_cleanup, retouch_region × 10 стадий. Каждая стадия даёт **0,047619 процентного пункта** общего плана; в публикации округляем итог до двух знаков. Список и статусы хранятся в `mvp-coding-progress.json`, поле `subdivisions`.

Стадии: контракты; вычисление preview; публикация; admission/quota; provider; выбор показанного результата; атомарный Apply; adoption/asset version; runtime/UI wiring; статическая трасса и authored сценарии отказов. Десятая стадия закрывает полноту трассы, а не повторно начисляет вес за сценарии остальных стадий. Каждый подпункт закрывается только при наличии кода и соответствующих написанных сценариев; запуск тестов остаётся отдельным этапом.

На момент фиксации 0/30 подпунктов получили отдельное подтверждение по новой шкале: имеющийся код требует статической сверки. Это статус учёта, а не утверждение об отсутствии реализации. Родитель остаётся open до закрытия всех детей. Детализация остальных единиц выполняется до начисления частичного веса с сохранением веса каждой исходной единицы.


### Removal Apply: исправления статического аудита — 2026-10-15

Исправлены дубли имён Go-тестов и ожидаемый namespace задачи копирования. Компилятор removal Apply теперь отклоняет exposure-only EV; добавлен сценарий с повторно вычисленным SHA, проверяющий семантический отказ. Миграция 02291 регистрирует CPU semantic.adopt_shown_removal с ограниченными output policies и v2_enabled=false. Регистрация не означает подключение startup или квалификацию. Статический поиск повторов top-level функций semanticreview после правки не выявил дублей. Тесты, сборка и миграции не запускались. Общий зачёт 14/70 (20,00%) не изменён: частичные стадии требуют отдельной полной сверки evidence.


### Removal Apply: claim/start — 2026-10-15

Миграция 02292 добавляет проверку CPU profile/qualification, связи задачи с выбранным target и review batch, готовности versioned inputs и живой суммарной quota. Начало run выполняется атомарно с claim; цепочка предыдущих проверок сохранена, delegate закрыт для прикладных ролей. Добавлены сценарии ACL и отключённого CPU-каталога/output policies. Миграция не применялась, тесты не запускались. Остаются removal-specific heartbeat/gateway, финализация и startup wiring. Зачёт 14/70 = 20,00% без изменения.


### Removal Apply: leased IO — 2026-10-15

Миграция 02293 повторяет persisted authority перед heartbeat и gateway input: active tenant/run, editor session, CPU qualification, canonical producer receipt и его четыре refs в зарегистрированном closure, готовые versioned inputs и суммарная живая quota. Обёртки сохраняют предыдущие проверки lease/certificate/fence. Отсутствующая задача отклоняется. Добавлены сценарии ACL приватных helpers/delegates и missing-task отказа heartbeat/gateway. Raw Params остаётся независимой проверкой worker admission. Миграции и сценарии не запускались. Финализация/startup wiring остаются открытыми, зачёт 20,00% без изменения.


### Removal Apply: child finalization — 2026-10-15

Обнаружен и закрыт code gap: прежний shown-finalizer допускал только exposure и оставлял removal child без терминального перехода. Миграция 02294 расширяет allowlist на exposure/removal, проверяет operation-specific task namespace, уникальность task/target и canonical result attempt/fence перед успешной финализацией. Сохраняются scheduler entrypoint, транзакционные quota/events/settlement. Добавлены ACL и missing tenant/run сценарии; положительный lifecycle fixture ещё необходим. SQL не применялся, сценарии не запускались. Startup wiring/accepted version API требуют дальнейшей сверки. Процент 20,00% без нового зачёта.


### Removal Apply: accepted version и media — 2026-10-15

Закрыты exposure-only ограничения accepted-image projection и media-source guards. 02295 добавляет removal copy к авторизованной выдаче retained result; Go проверяет точный task UUID двух разрешённых copy-операций. Написаны сценарии materialization removal и отказа ordinary producer. 02296 расширяет media registration/live-source на removal с точной связью version-to-task и сохранением lineage/RGB/alpha/delegate проверок. Добавлены API-only ACL и unknown authority сценарии. SQL и тесты не запускались. Полные положительные SQL/media lifecycle fixtures и startup routing остаются открытыми; 20,00% без изменения.


### Removal Apply: общая сервисная маршрутизация — 2026-10-15

PreparedApply получил отдельный ShownRemovalTasks; ApplyService направляет его в CommitShownRemoval. ShownApplyResolution/подготовка принимает отдельные removal inputs и переводит полностью подготовленный removal batch в общий результат. Написаны сценарии корректного выбора removal и отказов для unselected/extra/rerender/mixed tasks. Смешанный exposure/removal atomic commit и production SQL resolver остаются обязательными открытыми работами; текущий отказ предотвращает потерю части batch. Тесты/сборка/typecheck не запускались. Wiring подпункт не закрыт, 20,00% без нового зачёта.


### Removal resolver: qualification report — 2026-10-15

Добавлены 02297 и VerifyShownRemovalReviewReport: собственная operation binding removal copy, versioned report IO и повторная авторизация metadata после чтения. Написаны valid/large-policy/scope-change/tampered/oversize/wrong-operation сценарии и API-only ACL. Production resolver ещё требует removal source loader: exposure loader не подходит по контракту. Тесты и SQL не запускались, зачёт 20,00% без изменения.


### Removal Apply: source loader и SQL resolver — 2026-10-15

Добавлены LoadShownRemovalSourceDocuments и NewSQLShownRemovalApplyResolver. Точные Params показанного preview сверяются с текущими canonical review/candidate/source/protection; проверяется отдельный copy qualification report, selected receipt и copy resource budget. Recheck повторяет authority до публикации. Написаны cancellation/missing dependency/identity before-IO сценарии; поиск top-level function дублей пуст. Startup kind routing и полные успешные resolver fixtures открыты. Тесты/сборка/миграции не запускались, 20,00% без изменения.


### Removal Apply: server intent routing — 2026-10-15

Добавлен NewSQLShownOperationResolver: маршрутизация по актуальному authenticated intent, независимая повторная сверка выбранным resolver, отказ при изменении kind или отсутствующей установке без fallback. ExposureApplyInstallation принимает отдельные removal copy budget/provider и собирает общий preparer. Написаны exact-kind/unsupported/missing/no-fallback unit-сценарии. Tenant catalog и main startup ещё должны передать removal-конфигурацию. Тесты/сборка/SQL не запускались; зачёт 20,00% без изменения.


### Removal Apply: tenant catalog и main — 2026-10-15

Tenant catalog получил optional shown_removal_execution: copy budget validation и изоляция report/calibration refs между tenants. Установка переносит removal budget отдельно; main создаёт removal preview provider и передаёт общему shown Apply builder. Написаны valid/owned decode, non-CPU rejection и foreign removal ref сценарии. Открыты HTTP preview/admission routing, worker installation и положительные lifecycle fixtures. Тесты/сборка/миграции не запускались; 20,00% без нового зачёта.


### Removal HTTP GET preview — 2026-10-15

02298 предоставляет API-only intent object metadata под current preview fence. SQLPreviewKindResolver проверяет bounded exact-version SHA и metadata до/после IO; PreviewOperationRouter выбирает exposure/removal без fallback. Main GET preview теперь использует router. Написаны dispatch/unknown/missing/scope-error/cancellation сценарии и SQL ACL. POST admission, worker wiring и положительные lifecycle fixtures остаются открытыми. Тесты/сборка/SQL не запускались, зачёт 20,00% без изменения.


### Removal POST preview admission — 2026-10-15

POST tenant service выбирает exposure/removal admission по authenticated versioned intent без fallback. Optional removal_preview в deployment catalog задаёт qualification, measured resources, context/feather; проверяются backend-specific limits и изоляция refs между tenants. Написаны Metal >10GB, CUDA 10GB cap, CPU zero-device и invalid controls/resources сценарии. Worker startup/qualification activation и полные lifecycle fixtures остаются открытыми. Тесты/сборка/SQL не запускались, 20,00% без нового зачёта.


### Removal worker startup — 2026-10-15

CLI/build/run принимают и регистрируют removal preview и shown removal factories. Добавлены отдельные disabled-by-default env installers с bounded storage transport/ICC/qualification pins. Preview требует GPU resource class, cpu/cuda/mps backend и big_lama pool; shown copy требует CPU. Написаны disabled/ambiguous/backend/own-pins конфигурационные сценарии. Открыты scheduler adoption startup, нормативные operation contracts и полные lifecycle fixtures. Тесты/сборка не запускались, зачёт 20,00% без изменения.


### Removal scheduler adoption startup — 2026-10-15

AdopterConfig/run/Adopter получили strict disabled-by-default PHOTO_ADOPTER_SHOWN_REMOVAL_ENABLED. Устанавливается отдельный versioned removal qualification authority, передаётся AdoptionCoordinator, обнаруживаются removal-copy candidates с дедупликацией. Написаны independent authority/disabled baseline/ambiguous enablement сценарии. Нормативные operation contracts, полные lifecycle fixtures и статическая сверка подпунктов ещё открыты. Тесты/сборка не запускались; 20,00% без изменения.


### Removal normative operation contracts — 2026-10-15

Runtime и normative catalogs получили removal preview/copy с worker entrypoints, params/output names и SQL output caps. Подготовлены четыре standalone schema из текущих нормативных definitions; генератор расширен removal-моделями. Написаны cross-registry/entrypoint/output-budget/strict-schema сценарии. Общий contracts lock и активация не менялись; полная schema parity, lifecycle и fine trace audit ещё открыты. Тесты/сборка/GPU qualification не запускались, 20,00% без изменения.


### Первый детальный зачёт — 2026-10-15

P0.1.09.remove_object.02 (private numerical preview compute) закрыт по статическому source + authored scenario trace: `audit/removal-preview-compute-coding-trace.md`. Дописаны global/local-pass, cancellation-between-passes и empty/all-protected-mask сценарии. Тесты/сборка/photo qualification не выполнялись. В активной задаче закрыт 1/30 детей; родитель остаётся open. Общий вес закрытого кодирования: 14 + 1/30 из 70, **20,05%** (точно 20,047619%). Полностью закрытых исходных единиц по-прежнему 14/70; дробный зачёт не меняет знаменатель и не дублирует общие helpers.


### Removal publication: исправление SHA proof — 2026-10-15

Статическая сверка выявила отсутствие проверки returned staged SHA в общем WorkingImage writer. Добавлены bounded NPY SHA scan до upload и точная проверка JSON SHA. Fixtures exposure/removal обновлены на реальные digest; написаны pixel/manifest SHA mismatch сценарии. Исправлено имя protected_pixels_unchanged в schema-сценарии. Publication child остаётся open: полные bundle success/failure сценарии ещё нужны. Тесты/сборка не запускались; зачёт 20,05% без изменения.


### Removal complete bundle scenarios — 2026-10-15

Написаны full-attempt orchestration сценарии с реальными NPY/вычислением/PNG и точными WorkingImage/evidence/presentation refs, bytes accounting и mapping closure. ICC conversion явно заменена test double; квалификация цвета/модели не заявляется. Сценарии upload failure, SHA mismatch, alias и final admission revocation предотвращают возврат успешного bundle. Сценарии не запускались. Publication child остаётся open до backend/canonical publication trace review; 20,05% без изменения.


### Removal preview scheduler finalization — 2026-10-15

Выявлен разрыв scheduler lifecycle: существующий entrypoint завершал только exposure preview. Миграция 02299 добавляет отдельный removal finalizer и подключает его через прежний scheduler entrypoint. Success требует current authority, sealed single-task graph, canonical attempt/fence, ready versioned presentation и closure; terminal failures переводят preview run в failed. Порядок блокировок сохранён. Private delegate недоступен прикладным ролям; написан ACL сценарий. Миграция не применялась, сценарии не выполнялись. Publication child остаётся open; 20,05% без изменения.


### Removal canonical bundle commit — 2026-10-15

Статическая сверка цепочки commit выявила отсутствие обязательного single presentation root и наличия всех семи sidecar при записи canonical success. Forward-only 02300 добавляет post-delegate проверку в той же транзакции: tenant/task/attempt/fence, роли, MIME, размеры, ready/verified/version/expiry. Неполный набор вызывает exception и откат task success/resources/outbox. Replay/conflict/stale семантика предшественника сохранена. Эта проверка метаданных не заменяет byte/hash/schema/ref verification presentation при физическом чтении (02277 + Go provider). Написан private-delegate ACL сценарий; DB bundle success/failure fixture ещё требуется. Миграция не применялась, тесты не запускались. Publication child open; кодирование 20,05% без изменения.


### Removal bundle metadata scenarios — 2026-10-15

Проверка 02300 выделена в private semantic_removal_preview_bundle_ready, вызываемый commit после предшественника в той же транзакции. Написаны реальные SQL metadata fixtures на explicit PHOTO_TEST_DATABASE_URL: complete bundle, exact task/result/attempt/fence, repeated inspection; 7 ролей × 8 повреждений метаданных; empty/extra/wrong-SHA/sidecar-root canonical outputs; ACL isolation. Fixtures используют существующий semantic fixture и rollback. Это проверка production predicate, не полный commit/lease/outbox rollback/replay fixture; последние остаются открытыми. Сценарии не запускались, миграция не применялась. Publication child open; 20,05% без изменения.


### Removal commit transaction isolation scenarios — 2026-10-15

Написаны SQL wrapper isolation сценарии: неполный bundle вызывает CheckViolation и откат predecessor task-success/outbox, полный bundle пропускается, replay не дублирует outbox. Подмена live/current/publication-lock и predecessor body выполняется транзакционно на explicit test database с восстановлением через rollback. Это намеренно изолированные сценарии новой production wrapper + metadata predicate, НЕ доказательство реальной lease/authorization/resource-release цепочки. Full-authority lease fixture остаётся открытым. При статическом просмотре исправлены типы test-double bodies: current — plpgsql boolean, publication lock — uuid. Сценарии не запускались; кодирование 20,05% без нового зачёта.


### Removal presentation contract edge parity — 2026-10-15

Выявлено расхождение worker contract (унаследованные 4096) с Go/SQL reader и фактическим renderer (2560). RemovalPreviewPresentation теперь переопределяет strict width/height 1..2560; собственная standalone schema и нормативный RemovalPreviewPresentation синхронизированы. Написаны schema parity и runtime boundary/type сценарии: 2560 допускается; 0/2561/4096/bool/string/float отклоняются по обеим координатам. Exposure contract не менялся. Полный contracts parity/lock rollout остаётся открытым; общий lock и активация не менялись. Тесты не запускались; P0.1.09.remove_object.01 остаётся open, общий зачёт 20,05%.


### General contract regeneration preservation — 2026-10-15

Найден destructive regeneration drift: generate-contracts.py копировал старый canonical каталог 67 операций поверх runtime 70, удаляя shown exposure/removal и removal preview, и не проецировал восемь preview schemas. Canonical docs/implementation/contracts/operations.json и embedded workflow catalog синхронизированы с уже добавленными runtime записями. Основной генератор теперь включает все восемь preview моделей. Написаны byte-identical catalog/duplicate keys/preserved operations и dual-generator/model/current-standalone-schema parity сценарии. Генераторы и тесты не запускались; operation activation/qualification и contracts lock не менялись. Полная parity/lock rollout остаётся открытой, кодирование 20,05%.


### Shared preview schema projection — 2026-10-15

Оба генератора используют semantic_preview_projection: общий список восьми моделей, standalone metadata, namespaced refs и нормативный bundle. Основной генератор теперь также проецирует нормативные preview definitions, а не только standalone. Projection создаёт deep copy, сохраняет чужие definitions и отклоняет конфликт shared dependency между моделями до записи. Написаны ownership/preservation/conflict и byte-identical dual-generator outputs сценарии. Источник worker digest — v2/contract_lock.py; 13f037 является историческим SCENE_BOARD pin, а не текущим SCHEMAS_SHA256. SQL transport использует исторические совместимые pins. Совместимость всей текущей схемы ещё не доказана, lock не обновлялся вслепую. Генераторы/тесты не запускались; 20,05% без зачёта.


### Background cleanup private numerical preview — 2026-10-15

Execution contract §V27 требует remove/background_cleanup через inpaint, retouch_region через отдельную retouch. Добавлены SemanticBackgroundCleanupPreviewInput с собственным Literal kind и exact document binder. Численное тело removal выделено в preview_inpaint_compute, removal wrapper сохраняет прежнюю сигнатуру и closed model. Новый background_cleanup wrapper вызывает то же вычисление без преобразования kind/approval. Написаны exact source/protection/outside bits, own admission kind, чужие kinds, admission loss и zero-strength сценарии. Loader/publication/qualification/SQL/HTTP/Apply ещё не подключены; retouch не подменяется inpaint. Тесты не запускались. Background compute child остаётся pending_static_audit до полного bounded trace; кодирование 20,05%.


### Background cleanup artifact loader and pre-IO gate — 2026-10-15

Добавлены own-kind geometry/protection binders и artifact loader через существующий общий exact-source path: byte/hash document binding, WorkingImage/mask/transform/reference/protection origin, ICC callback, conservative RAM admission, NPY ownership/closure и повтор admission. Artifact orchestrator берёт strength из проверенного intent и передаёт его compute. При статическом сравнении с exposure найдено чтение candidate/intent до admission в removal orchestrator; обе inpaint preview entrypoints теперь проверяют callbacks/cancellation/admission до document IO. Написаны real-NPY success и admission/origin/color/budget denial, post-IO revocation/no-model, mapping closure и no-document-IO denial для обеих операций. Тесты не запускались. Background publication/SQL/qualification/Apply остаются открытыми; кодирование 20,05%.


### Background evidence/staging и второй детальный зачёт — 2026-10-15

Добавлены own BackgroundCleanupPreviewNumericalEvidence/Presentation, standalone/normative schemas и общий generator projection. Staging сохраняет WorkingImage metadata/alpha refs, точный pixels/manifest/evidence SHA, own-kind admission и artifact alias fences. Написаны staging сценарии. Общие numerical сценарии параметризованы на remove_object/background_cleanup, включая global/local, cancellation, RAM/HDR/strength/protection/empty-mask. По audit/background-cleanup-preview-compute-coding-trace.md закрыт только P0.1.09.background_cleanup.02; publication/staging целиком ещё не зачтены. Итог: 2/30 детей, weighted units 14+2/30 из 70, **20,10%** (точно 20,095238%). Тесты/генераторы не запускались, qualification/активация не выполнены.


### Background cleanup full preview attempt bundle — 2026-10-15

Добавлены own media/complete orchestration и presentation-stage wrapper: exact source reload, own alpha loader wrapper, независимый preservation proof, pinned ICC callbacks, premultiplied display resize/overlays, WorkingImage/evidence/PNG/presentation SHA и alias fences, output accounting, final admission. Complete вычисляет candidate один раз и публикует полный attempt bundle из восьми артефактов. Написаны own evidence strict-proof/ROI, media preservation/admission, presentation linkage/alias, complete budget-before-compute и real-NPY/PNG bundle сценарии с upload/SHA/alias/final-revocation отказами. ICC conversion в bundle fixture явно test double; качество модели/цвета не квалифицируется. Тесты не запускались. SQL canonical publication, qualification/install, provider/Apply ещё открыты; publication child не зачтён, кодирование 20,10%.


### Background cleanup independent qualification and installer — 2026-10-15

Добавлены отдельные BackgroundCleanupQualificationPin, report verifier с обязательным background_cleanup floor и exact operation/dataset/backend pins, worker adapter и SQL installation factory. Adapter/installer требуют own deterministic preview task UUID и собственные SQL admission/origin/report methods; removal/exposure installation types не подходят. CUDA peak cap 10_000_000_000, CPU peak=0, Metal без фиксированного cap при measured admission. Написаны backend/memory/mandatory-check, foreign removal pin/report и installer-type/missing-authority сценарии. SQL методы, TaskDatabase binding, runtime config/registration и операция в disabled catalog ещё нужны; installer не подключён. Тесты и реальная qualification не запускались; кодирование 20,10% без нового зачёта.


### Background cleanup disabled catalog и Go params/resources — 2026-10-15

Forward-only 02301 добавляет semantic.preview_background_cleanup v1/gpu/Big-LaMa с CPU/CUDA/MPS и v2_enabled=false, восемь output policies с прежними bounded размерами. Runtime canonical/embedded catalogs и normative V2 operation синхронизированы. Добавлены own canonical Go params parser/freezer (kind только background_cleanup) и resource compiler через общий inpaint/display accounting без наследования qualification. Написаны canonical/type/alias/control/foreign-kind и CPU/CUDA/Metal/budget сценарии, cross-registry worker entrypoint coverage, explicit-DB disabled catalog/exact output-policy parity сценарий. SQL admission/input closure/publication, TaskDatabase и runtime config ещё открыты. Выполнен только gofmt и статическая сверка; тесты/сборка/миграции не запускались. Кодирование 20,10% без нового зачёта.


### Background cleanup SQL lease authority — 2026-10-15

Forward-only 02302–02305 добавляют operation-specific inventory JCS digest/transport/persisted closure, worker lease admission, canonical transfer protection provenance и qualification report metadata. Собственные semantic.preview_background_cleanup/config namespace/kind; общие review-current/tenant/lease/worker/source/qualification primitives сохранены. TaskDatabase получил exact-lease/body/root/binary методы. Написаны query binding/strict boolean и explicit-DB role allowlist, own-vs-removal digest и invalid/unscoped admission сценарии. Saved run, quota, input sealing/claim/runtime config и canonical publication пока не подключены; операция disabled. Тесты/миграции не запускались, кодирование 20,10% без нового зачёта.


### Background cleanup saved run, documents, quota and exact input authority — 2026-10-15

Forward-only 02306–02310 добавляют own publication lifecycle lock, Params/transport document reservation-completion с replay/conflict, deterministic actor/session/params-bound draft preview run, immutable measured execution profile/output quota и exact source/reference/candidate/mask/transform/protection inventory predicates. Scope/target/current review/plan/intent/snapshot/source/versioned-object/provenance checks сохраняются, authority не выдаётся по одному digest. Ни одна функция не создаёт approval/Apply. Написаны API-only ACL, foreign exposure/removal task namespace и foreign intent kind denial сценарии. Task materialization/sealing, claim/live IO/canonical result/finalizer и Go admission wiring остаются открытыми. Тесты/миграции не запускались; кодирование 20,10%.


### Background cleanup SQL materialization, sealing, atomic commit and inspection — 2026-10-15

Forward-only 02311–02313 добавляют own run-current/input-scope, blocked single-task materialization, exact input registration/replay conflict, sealed transport/profile, atomic queued preview commit и exact request/session inspection. При обновлении shared guard использовано последнее тело 02289, а не устаревший 02271: сохранены workflow, semantic Apply, exposure/removal preview, shown exposure и shown removal scopes; добавлена одна background ветка. Private components не выданы прикладным ролям, наружу API-only commit/inspection. Написаны component ACL, prior guard-scope retention и missing authority denial сценарии. Claim/live IO/result/finalizer и Go/runtime wiring ещё открыты. Тесты/миграции не запускались; кодирование 20,10% без нового зачёта.


### Background cleanup execution chain, canonical bundle and finalizer — 2026-10-15

Forward-only 02314–02319 расширяют текущие claim/gateway/heartbeat/commit/scheduler entrypoints через private predecessor chain: own start/current/closure/quota/qualification checks; live IO revocation; exact canonical presentation/sidecar sources; current decision publication fence; same-transaction single root + seven verified role bundle; scheduler-only finalization. Предыдущие exposure/removal/shown проверки остаются в цепочке. Длины новых identifiers статически сверены с PostgreSQL 63-byte limit, finalizer delegate использует короткое before_bg. Написаны own bundle metadata (7×8 повреждений)/wrapper rollback-replay isolation и private delegate/result-source/finalizer ACL сценарии. Реальная lease/resource/outbox integration без doubles, Go admission/provider и runtime startup пока открыты. Тесты/миграции не запускались; кодирование 20,10%.


### Background cleanup Go task/inventory/preparation/admission — 2026-10-15

Добавлены own Go compile/inventory/task/source preparation, saved run/document publisher/quota, preparation/admission/commit/inspection и admission service. Typed BackgroundCleanupQualificationPin/Profile и private FrozenBackgroundCleanupPreviewTask держат отдельную operation identity; common source resolver получает строго background_cleanup kind. Inventory регистрирует исходные document/NPY/alpha/geometry зависимости; все SQL calls используют новые собственные функции. Написаны аналоги invalid scope/cancellation/no-IO/publisher/quota/commit сценариев и положительный source/inventory/Metal 12 GB measured task scenario в общем document fixture. HTTP provider/readiness/config и worker runtime install ещё открыты, полная трасса admission не зачтена. Выполнены gofmt и static review; тесты/сборка не запускались; кодирование 20,10%.


### Background cleanup physical provider and independent receipt — 2026-10-15

Forward-only 02320–02321 добавляют own lookup и immutable v27_background_cleanup_preview_receipt. Go reader/provider имеет собственные evidence/presentation/linkage/NPY WorkingImage/PNG/receipt types и SQL calls, exact producer/attempt/fence/version/hash linkage, повтор current authority после IO и response signing. Добавлена отдельная optional ветка PreviewOperationRouter без fallback. Написаны source/image/evidence/presentation/linkage/receipt/provider/response negative сценарии, router exact dispatch/missing provider и API-only lookup/receipt ACL. PNG verification доказывает integrity/profile/dimensions, не независимую художественную эквивалентность. Main/config/readiness и Apply adoption ещё открыты; provider не установлен автоматически. Тесты/сборка/миграции не запускались, кодирование 20,10% без нового зачёта.


### Background cleanup worker startup and CLI — 2026-10-15

Добавлена own PHOTO_BACKGROUND_CLEANUP_PREVIEW_* environment configuration (disabled default), CLI forwarding, build_worker validation/Big-LaMa registry and run_worker forwarding. Только matching GPU-class CPU/CUDA/MPS worker с установленным model pool и own SQL/runtime factory регистрирует semantic.preview_background_cleanup; чужие REMOVAL flags не включают операцию. Storage access остаётся bounded timeout/no internal retries, ICC pins обязательны. Написаны disabled/invalid flag/backend/missing own pins, foreign enablement и invalid installation before DB construction сценарии. Main/provider/admission/readiness и Apply adoption ещё открыты. Тесты/сборка/qualification не запускались; кодирование 20,10% без нового зачёта.


### Уточнение отображения прогресса — 2026-10-15

По запросу пользователя показывать общий процент кодирования до трёх знаков после запятой и изменение в процентных пунктах после каждого закрытого подпункта. Для активного пункта указывать ID, название и число закрытых дочерних частей. Вес исходных 70 пунктов сохраняется; дробление не увеличивает выполненный объём. P0.1.09: 2/30; общий показатель 20,095%; одна дочерняя часть даёт +0,048 процентного пункта (точно 100/2100). Новый формат отчётности сам по себе не закрывает кодирование. Написанные, но не запущенные тесты не являются доказательством прохождения тестирования.


### Background cleanup: backend installation — 2026-10-15

Добавлены собственная tenant-конфигурация background_cleanup_preview, проверка qualification и измеренных ресурсов, проверка принадлежности qualification artifacts tenant-у. Provider подключён в main и маршрутизатор чтения; POST admission использует отдельную tenant-карту без fallback на removal. Написаны unit-сценарии маршрутизации, изоляции tenant, отсутствия fallback и отмены. Выполнены статический просмотр и git diff --check; тесты не запускались. Readiness и полная трасса остаются открыты; процент без изменения: 20,095%, активный пункт 2/30.


### Background cleanup: installation admission — 2026-10-15

При прямом создании admission service добавлена повторная проверка installation resources до authority/object IO. Добавлены unit-сценарии владения конфигурацией, отключения при отсутствии конфигурации, неверных backend/report/бюджетов, межтенантных report/calibration, CUDA 10 ГБ и Metal с измеренным бюджетом 12 ГБ. Статически подтверждено: migration 02301 оставляет каталог отключённым; построенный provider не доказывает qualification или активацию. Unit-сценарии не запускались. git diff --check выполнен без ошибок. Процент 20,095%, 2/30; полная трасса остаётся открытой.


### Background cleanup: selection lock — 2026-10-15

Добавлены LockApplyBackgroundCleanupPreviewSelections и forward-only migration 02322: собственные namespace/receipt, live tenant/session/revision fences, canonical result и проверенные artifacts. SQL доступен только photo_semantic_admission, без права исполнения API/worker/scheduler/portal/gateway. Написаны сценарии отмены, отсутствия транзакции, ACL и отказа admission при неверном бюджете до I/O. Статически сверены вызываемые SQL helpers. Подключение lock к атомарному Apply остаётся открытым; миграция и тесты не запускались. git diff --check без ошибок. Процент 20,095%, 2/30, без нового зачёта.


### Background cleanup: shown adoption compiler — 2026-10-15

Реализованы отдельные ShownBackgroundCleanupAdoptionParams, compiler, input inventory, CPU copy resources и FrozenShownBackgroundCleanupTask. Контракты сохраняют выбранные preview producer UUID, result, attempt/fence и refs; inventory включает proposal provenance, display images и alpha/geometry. Написаны unit-сценарии владения байтами, детерминизма, изменённых selection, повторно хешированных чужих approvals, чужого producer namespace, отмены и отсутствующего closure. SQL batch admission, worker adoption и подключение Apply ещё впереди. Тесты не запускались; git diff --check без ошибок. Процент 20,095%, 2/30, без зачёта незавершённых подпунктов.


### Background cleanup: adoption preparation and batch proof — 2026-10-15

Добавлены PrepareShownBackgroundCleanup, варианты publisher/factory, exact document bundle, проверка публикации transport/closure и проекция commit payload. VerifyShownBackgroundCleanupBatch связывает copy-задачи с human approval и private review, исключает смешение операций и дубли target rows; payload владеет собственными bytes. Добавлены unit-сценарии отмены, отсутствия authority, неполных документов, изменённого transport/profile/params, недоверенного cursor и неопубликованного решения. SQL publisher и атомарный commit ещё не подключены. Тесты не запускались; статический просмотр и git diff --check выполнены. Кодирование: 20,095%, 2/30; нового зачёта нет.


### Background cleanup: adoption publisher — 2026-10-15

Добавлены собственные publisher и batch preparation; forward-only migrations 02323 (worker admission/closure/report predicates) и 02324 (pre-child transport tickets с live selection locks). Написаны сценарии отмены, отсутствующих dependencies, неправильного task и ACL transport-функций. Статически сверены namespace и длина SQL identifiers; git diff --check без ошибок. Не применялись миграции и не запускались тесты. Atomic Apply commit, worker adoption и operation registration остаются открыты; 20,095%, 2/30, без нового зачёта.


### Background cleanup: atomic Apply foundation — 2026-10-15

Добавлены forward-only migrations 02325–02329: admission documents/profile/input closure, batch locks, draft shell/quota, registration/sealing, activation и atomic commit. Добавлен CommitShownBackgroundCleanup с idempotency и проверкой receipt. Новый guard основан на актуальном 02311 и сохраняет все предыдущие scopes. Написаны ACL и guard-preservation integration scenarios, missing-transaction unit scenario. Статически сверены определения новых SQL dependencies и длина identifiers; git diff --check без ошибок. ApplyService routing, worker adoption и operation registration остаются открыты. Миграции и тесты не запускались. 20,095%, 2/30; нового зачёта нет.


### Background cleanup: shown resolver and ApplyService — 2026-10-15

Добавлены собственные SQL resolver, source loader и exact qualification report reader с повторной авторизацией; migration 02330 report metadata. Shown preparation и ApplyService направляют cleanup-задачи в собственный atomic commit и запрещают смешение семейств/rerender. Написаны unit-сценарии resolver/report/routing и integration ACL. Tenant startup/operation resolver ещё не подключены к новой ветке; worker adoption остаётся открытым. Тесты и миграции не запускались; git diff --check без ошибок. Кодирование 20,095%, 2/30, без нового зачёта.


### Background cleanup: tenant Apply startup — 2026-10-15

Подключены собственный optional shown_background_cleanup_execution CPU budget и preview provider из main в tenant installation. Единый scoped operation resolver выбирает cleanup/exposure/removal по авторизованному intent без fallback. Добавлены unit-сценарии маршрутизации, отсутствующей/неоднозначной установки и CPU-only copy budget. Worker adoption и каталог операции ещё не реализованы; qualification/activation не заявляются. Тесты и миграции не запускались; git diff --check без ошибок. 20,095%, 2/30, без нового зачёта.


### Background cleanup: worker shown-copy — 2026-10-15

Добавлены ShownBackgroundCleanupAdoptionInput с собственным producer UUID namespace, binding/execution/adapter/SQL factory, отдельный qualification pin/report verifier и TaskDatabase admission/report methods. Написаны сценарии запрета I/O до admission, scope/backend/qualification отказов. Общий exact-byte staging сохраняет выбранный WorkingImage и pixels. Runtime/CLI registration, schemas/catalog и scheduler adoption ещё открыты; операция не активирована. Тесты не запускались; git diff --check без ошибок. 20,095%, 2/30, без нового зачёта.


### Background cleanup: worker registration/contracts — 2026-10-15

Добавлены own PHOTO_SHOWN_BACKGROUND_CLEANUP config, runtime CPU factory admission, registration и run/CLI forwarding. Обновлены canonical/runtime operation catalogs и schema projections, standalone input schema; migration 02331 оставляет operation disabled. Написаны config unit-сценарии. Генераторы не запускались, contract lock не менялся, qualification/activation не заявляются. Scheduler adoption, claim/live fences и полная трасса ещё открыты. Тесты не запускались; git diff --check без ошибок. 20,095%, 2/30, без нового зачёта.


### Background cleanup: result-scoped scheduler adoption — 2026-10-15

Добавлены migrations 02332 discovery/report/receipt и 02333 output-bound atomic adoption, отдельные scheduler installer/verifier/commit и dispatch adoption service. Существующая adopt_v2_plan цепочка сохраняется через переименованный predecessor. Написаны missing-context/inactive-context scenarios. Статический просмотр выявил и устранил дублирование блока dispatch при редактировании. Claim/live/finalization и scheduler startup ещё открыты. Миграции и тесты не запускались; git diff --check без ошибок. 20,095%, 2/30, без нового зачёта.


### Background cleanup: claim/live/finalization — 2026-10-15

Добавлены migrations 02334 claim/start, 02335 gateway/heartbeat live gates с сохранением актуальной predecessor chain. Migration 02336 расширяет текущий shown child finalizer на cleanup, сохраняя exposure/removal, quota settlement и события. Написаны ACL/live-chain integration scenarios. Scheduler startup и полная статическая трасса остаются открыты. Тесты и миграции не запускались; git diff --check без ошибок. 20,095%, 2/30, без нового зачёта.


### Background cleanup: scheduler startup — 2026-10-15

Добавлен default-false PHOTO_ADOPTER_SHOWN_BACKGROUND_CLEANUP_ENABLED с отдельной проверкой bool и собственным versioned installer. Coordinator, discovery и run forwarding подключены независимо от removal/exposure. Написаны сценарии независимой установки, отсутствия exposure fallback, отключённого baseline и неоднозначного enablement. Выдача принятой версии API, parity контрактов и полная статическая трасса требуют дальнейшей сверки. Тесты не запускались; git diff --check без ошибок. 20,095%, 2/30, без нового зачёта.


### Background cleanup: accepted-version/media gap — 2026-10-15

Статически выявлен реальный разрыв: accepted image API и media source/retry guards допускали только exposure/removal. Добавлены cleanup task UUID в Go и migrations 02337 applied image projection, 02338 media sources. Сохранены старые namespaces и проверки canonical producer/inbox/source authority. Написан unit-сценарий точной copy identity и запрета inference producer. Полная contract/static trace ещё открыта; тесты и миграции не запускались. git diff --check без ошибок. 20,095%, 2/30, без нового зачёта.


### Закрыт контрактный подпункт background cleanup — 2026-10-15

P0.1.09.background_cleanup.01 переведён в coding_closed_unverified по audit/background-cleanup-preview-contract-coding-trace.md. Сверены собственные params, evidence/presentation, canonical SHA/alias правила и ограничения; написаны display-boundary сценарии. Генераторный parity PASS, runtime readiness и квалификация не заявляются. Активный пункт 2/30 → 3/30; взвешенный процент 20,095% → 20,143%, прирост точно 100/2100 процентного пункта. Тесты не запускались; git diff --check без ошибок.


### Закрыт publication подпункт background cleanup — 2026-10-15

P0.1.09.background_cleanup.03 coding_closed_unverified по audit/background-cleanup-preview-publication-coding-trace.md. Исправлена проверка evidence budget до staging; добавлены SHA/alias unit-сценарии. Статически сверены восемь output roles, refs/SHA, budget/admission и canonical metadata gate; ограничения ICC/SQL test doubles явно записаны. Активный пункт 3/30 → 4/30; 20,143% → 20,190%, прирост 100/2100 п.п. Тесты/миграции не запускались; git diff --check без ошибок.


### Background cleanup: quota/resource hardening — 2026-10-15

В migration 02309 downstream profile gate изменён на IS DISTINCT FROM true: NULL тоже отклоняется. Добавлены resource unit-сценарии exact measured limits и нехватки каждого бюджета, неверных размеров/pixel count. Replay/activation trace остаётся открытой, подпункт .04 не закрыт. Тесты и миграции не запускались; git diff --check без ошибок. 20,190%, 4/30, без нового зачёта.


### Background cleanup: recovery trace — 2026-10-15

Сверен Save → Inspect → classifyPreviewRecovery: sealed states сохраняют identity без повторной публикации; только empty draft допускает resume. Добавлены own-namespace lifecycle unit-сценарии и запрет partial/missing-task resume. В migration 02308 authority predicate переведён на IS DISTINCT FROM true. Полный admission/activation fixture и статическая трасса .04 ещё требуют доработки. Тесты и миграции не запускались; git diff --check без ошибок. 20,190%, 4/30, без нового зачёта.


### Background cleanup: admission rejection scenarios — 2026-10-15

Transport gate migration 02310 требует явно true; NULL отклоняется. Добавлены unit-сценарии canonical Params mutation до DB, недоступной и отменённой inspection без фиктивного rollback/recovery. Полный положительный admission/quota/activation fixture ещё не написан; .04 остаётся открытым. Тесты/миграции не запускались; git diff --check без ошибок. 20,190%, 4/30, без нового зачёта.


### Background cleanup: числовые границы ресурсов — 2026-10-15

Добавлены authored unit-сценарии безопасного целого 9007199254740991 для измеренной Metal памяти, переполнения runtime/index overhead, минимального scratch index, отрицательных и превышающих безопасный диапазон бюджетов каждого типа. При отказе сценарии требуют пустую оценку ресурсов. Metal не ограничен 10 ГБ; CUDA policy сохранена. Это дополнение к .04, полный positive admission/quota/activation fixture остаётся незавершённым. Тесты, сборки, миграции и hardware qualification не запускались. Кодирование 20,190%, активный пункт 4/30; нового зачёта нет.


### Background cleanup: publication authority и quota replay — 2026-10-15

Статически найден и исправлен NULL fail-open в publication lock 02306: semantic_preapply_preview_current теперь требует IS DISTINCT FROM true для отказа. Quota replay 02309 требует old.share_id IS NULL, как activation 02312; share-scoped reservation больше не признаётся подходящим повтором user reservation. Добавлены authored integration regression-сценарии определения установленных функций и SQL truth table; они явно не заменяют positive admission fixture и не доказывают quota IO. Тесты и миграции не запускались. .04 остаётся открытым; 20,190%, 4/30, без нового зачёта.


### Background cleanup: ранний source scope и admission trace — 2026-10-15

AdmitBackgroundCleanupPreviewRun теперь отклоняет несовпадающий Source.TargetID до Save/Inspect; ранее проверка была только в Prepare. Добавлены authored missing/foreign target сценарии. В audit/background-cleanup-preview-admission-coding-trace.md зафиксирована трасса и точные требования к положительному fixture без authority doubles. Тесты/миграции не запускались. .04 открыт, 20,190%, 4/30.


### Background cleanup: response composition — 2026-10-15

Добавлены authored unit-сценарии положительной сборки response после physical verification: четыре точных version-pinned объекта, expiry 1 минута, проверки authority до/после подписания, quality_status review. Добавлены stale revision до signing, revoke до/после signing, signer failure без частичного DTO, cancellation после final authority. Fixture намеренно синтетический verified и не доказывает физическую проверку PNG/ICC или SQL ACL. Тесты не запускались. .05 не закрыт, .04 positive admission fixture ещё требуется. Кодирование 20,190%, активный пункт 4/30, без дополнительного зачёта.


### Background cleanup: повторная canonical identity после IO — 2026-10-15

Добавлены authored сценарии общего presentation reader с own background_cleanup task namespace: positive read, изменения task/result/attempt/fence/artifact/SHA/version/key/bytes при повторном lookup, revoke, cancellation, corrupt bytes, foreign removal producer до object IO. Проверяется отсутствие частичного документа и владение копией raw buffer. Loader является double; SQL ACL и PNG/ICC не покрыты. Тесты не запускались. Кодирование 20,190%, 4/30; .05 остаётся открытым до полной трассы физического чтения.


### Background cleanup: SQL sidecars boundary — 2026-10-15

Создан authored integration fixture выдачи семи sidecars через реальную функцию 02316: positive exact role refs, 7×8 отказов по producer/attempt/fence/verification/version/expiry/role/media и изменение presentation bytes. Только upstream presentation_source заменён transaction-local double; это не evidence авторизации или физического PNG/ICC. Explicit test database обязателен, fixture откатывает изменения. Тесты/миграции не запускались. .05 остаётся открытым, 20,190%, 4/30.


### Background cleanup: закрытые SQL sidecar refs — 2026-10-15

В 02316 перед UUID cast добавлен закрытый ref object gate: ровно artifact_id/sha256, string types, lowercase UUID/SHA formats. Устраняется расхождение SQL с закрытым Go contract; unknown fields и некорректные типы отклоняются с 22023. Добавлены 7 authored integration сценариев malformed/extra/missing/scalar/alias refs, с пересчитанными canonical metadata hash/size, чтобы проверить schema gate. Upstream authority double сохранён и явно обозначен. Тесты и миграции не запускались. 20,190%, 4/30, .05 открыт.


### Background cleanup: presentation schema parity — 2026-10-15

SQL 02316 теперь требует полный закрытый набор полей presentation, запрещает unknown fields и числовые ICC/SRGB SHA, ранее принимавшиеся через ->> как текст. Это соответствует string/closed Go и worker contracts. Добавлены 8 authored integration сценариев с актуальным hash/size presentation: extra field, numeric ICC/SRGB, width string/zero/oversized, missing resize, null params. Upstream authority double сохранён, полного auth evidence нет. Тесты/миграции не запускались. 20,190%, 4/30, .05 открыт.


### Background cleanup: qualification policy scenarios — 2026-10-15

Расширены authored unit-сценарии квалификации: запрет исключения каждой из 10 обязательных проверок даже после repin policy, missing/extra photo fixture SHA после repin report, propagation точного отказа live authority на первом/втором callback. Existing own operation/type и CPU/CUDA/Metal memory policy сохранены. Тесты и photo/hardware qualification не запускались. Это сценарии верификатора, а не реальный квалификационный отчёт. 20,190%, 4/30, без дополнительного зачёта.


### Background cleanup: JSON artifact size parity — 2026-10-15

Устранён конфликт лимитов: evidence/presentation допускают 1 МиБ, но decodeStrict имел общий 64 КиБ предел. Добавлен bounded decodeStrictWithLimit с абсолютным максимумом 1 МиБ; обычный decodeStrict сохраняет 64 КиБ. Только own background cleanup evidence/presentation используют 1 МиБ, со всеми прежними strict/canonical/schema проверками. Добавлены authored evidence сценарии >64 КиБ допустимого JSON и >1 МиБ отказа с корректным SHA. Тесты/сборки/миграции не запускались. 20,190%, 4/30, .05 открыт.


### Background cleanup: receipt evidence schema и binding — 2026-10-15

SQL seal receipt 02321 теперь требует закрытый полный numerical evidence object, согласованный с Go decoder (unknown/missing fields отвергаются). Расширены authored receipt decoder unit-сценарии task/presentation/working image/pixels/numerical evidence substitution и alias refs; прежние attempt/fence/params/result проверки сохранены. Тесты/миграции не запускались. Это bounded receipt validation, не полный selection/Apply lifecycle. Кодирование 20,190%, активный пункт 4/30, без дополнительного зачёта.


### Background cleanup: real SQL saved-draft authority fixture — 2026-10-15

Написан положительный metadata fixture без подмены SQL authority функций: реальный current review/plan sources, Save deterministic draft UUID и replay без задач; negative revoked session/stale target/cancelled parent. Используется существующий synthetic metadata final-producer fixture; минимальный saved-intent payload не объявляется compiled worker Params или physical/quality evidence. Это первый этап требуемого .04 positive fixture; quota/transport/activation ещё не покрыты. Тесты/миграции не запускались. 20,190%, 4/30, .04 открыт.


### Background cleanup: real SQL recovery inspection — 2026-10-15

Расширен real authority fixture вызовами 02313: positive empty draft inspection до/после replay; tenant/actor/session/repinned params/missing run отклоняются, а не возвращаются как empty draft. Revoked session/stale target/cancelled parent отклоняют inspection существующего run. Никаких SQL authority doubles; fixture по-прежнему metadata и minimal saved-intent payload. Quota/transport/activation остаются вне этого fixture. Тесты/миграции не запускались. Кодирование 20,190%, 4/30; .04 открыт.


### Background cleanup: real qualification metadata quota fixture — 2026-10-15

Написан authored positive SQL quota fixture без authority function doubles: real saved review authority, собственная CPU qualification policy/report metadata с 10 обязательными checks, source metadata SHA, resource floors, один storage reserve, frozen profile replay без двойного reserved_bytes, отказ changed profile. Общий frozen_qualification fixture получил необязательный required_checks, default quality сохранён. Source payload минимален для quota boundary, не полный WorkingImage; qualification metadata синтетические и не являются реальной photographic qualification. Transport/activation ещё не покрыты. Тесты/миграции не запускались. .04 открыт, 20,190%, 4/30.


### Background cleanup: quota отказ без побочных изменений — 2026-10-15

Positive real authority quota fixture дополнен 11 authored negative сценариями: 5 resource mismatches, source SHA/verification, report verification, policy admission revocation, expired reservation и capacity на один байт ниже. Проверяются unchanged reserved_bytes, отсутствие task, отсутствие frozen profile/reservation при первичном resource отказе. Source/report остаются synthetic metadata; full transport/activation не написан. Тесты/миграции не запускались. 20,190%, 4/30, .04 открыт.


### Background cleanup: transport publication scenarios — 2026-10-15

Добавлен own-operation authored transport fixture из 13 synthetic dependency refs и canonical Params: positive Params/chunk/manifest publication, exact commit inventory/operation/digest projection, rejection изменённого count, storage/SHA/identity/cancel отказы без partial task. Fixture начинается с frozen inventory, не доказывает Resolve/Compile source documents, SQL authority или activation. Тесты не запускались. Full .04 positive activation остаётся незавершённым. 20,190%, 4/30, без дополнительного зачёта.


### Background cleanup: source compiler → transport projection — 2026-10-15

Расширен existing WorkingImage/MaskRef/EditIntent fixture: реальный ResolveBackgroundCleanupPreviewDocuments и CompileBackgroundCleanupPreviewTask теперь связаны с Publish/PrepareCommitInputs. Authored assertions проверяют полный набор зависимостей с contract types, alpha/geometry refs, task/operation identity, canonical Params и transport digest/count. Publisher остаётся byte/hash recorder, SQL quota/activation и physical object verification этим сценарием не доказываются. Тесты не запускались. .04 открыт, 20,190%, 4/30.


### Background cleanup: canvas/resource SQL parity — 2026-10-15

Исправлена quota 02309: произвольный side cap 200000 заменён source contract 8..200000000 при площади <=200000000. Regex допускает девятизначную сторону; произведение двух девятизначных чисел остаётся в bigint. Fixture ресурсов теперь учитывает min(pixels,2560²) display bound. Добавлены long/narrow positive metadata cases в обеих ориентациях и 5 invalid dimension/area scenarios. Admission aggregate 02310 также требует quota.share_id IS NULL, согласованно с replay/activation. Тесты/миграции не запускались. .04 открыт, 20,190%, 4/30.


### Background cleanup: SQL exact inventory scenarios — 2026-10-15

Написаны authored pure SQL projection scenarios 02310: четыре комбинации alpha/geometry, omission каждой обязательной/optional dependency, extra/duplicate/reordered entries, contract/SHA substitution, cross-contract artifact alias. Function вызывается owner, authority/physical/activation не подменяются и не заявляются: тестируется только чистая inventory projection. Тесты не запускались. .04 full activation остаётся открытым. 20,190%, 4/30, без дополнительного зачёта.


### Background cleanup: uniform explicit-true SQL gates — 2026-10-15

Усилены оставшиеся NOT function-call guards собственных preview/shown background cleanup миграций 02302/03/04/05/23/26/29: inventory digest, worker authorization, current preview, downstream qualification/profile, report/protection admission и editor authorization требуют явно true. NULL означает отказ; successful true unchanged. Добавлены authored installed-definition regression сценарии трёх worker функций, явно не full worker/lease evidence. Тесты/миграции не запускались. 20,190%, 4/30, .04 full activation открыт.


### Full admission fixture: explicit document metadata mode — 2026-10-15

Общий semantic final-producer fixture расширен opt-in metadata_bodies=False (default сохранён). В явном true режиме source/calibration/protection builders могут регистрировать точные raw hash/size без MinIO: SHA пересчитывается, object version помечена metadata-only-v1. Upload branch сохраняет прежнее поведение. Это инфраструктура для сквозного SQL admission fixture; никакой физической верификации/qualification не заявлено. Тесты не запускались. .04 activation ещё открыт. 20,190%, 4/30.


### Full admission fixture: schema-valid source documents — 2026-10-15

Добавлен reusable WorkingImage builder с float32 RGB NPY, optional float32 alpha, canonical canvas/transform chain и SHA реального ICC profile. Real-authority saved fixture теперь опционально принимает source builder/metadata_bodies и binds source ref/canvas hash; authored positive cases RGB и RGB+alpha. Builders выполняются только внутри теста; object storage/ICC transform/quality этим metadata mode не доказаны. Тесты не запускались. Full protection/candidate/transport activation fixture ещё открыт. 20,190%, 4/30.


### Full admission fixture: canonical protection provenance — 2026-10-15

Добавлен reusable protection builder с реальными interactive rasterize/task/attempt/canonical result rows, MaskRef/NPY SHA/size и preserved raw docs в scope. Saved-authority fixture принимает optional protection builder. Authored positive scenario проверяет semantic_protection_producer_ready и отказ после producer fence change без function doubles. Metadata mode не является object/photographic qualification. Тесты не запускались. Candidate/transport/activation полного fixture ещё требуют соединения. 20,190%, 4/30, .04 открыт.


### Full admission fixture: own EditIntent/selection inputs — 2026-10-15

Добавлен reusable background_cleanup EditIntent/selection MaskRef/NPY/SemanticTransferParams builder с canonical bytes, source canvas binding и capture raw docs. Saved SQL authority fixture принимает transfer builder и использует собственный intent artifact вместо source placeholder, регистрируя transfer input dependencies. Добавлен authored positive save/inspect сценарий own intent SHA/kind. Candidate result остаётся synthetic metadata; его mask/transform и transport/activation ещё нужно соединить. Тесты не запускались. 20,190%, 4/30, .04 открыт.


### Full admission fixture: canonical proposed candidate sidecars — 2026-10-15

Общий producer fixture получил optional transfer_output_builder (default unchanged). Reusable builder создаёт proposed TransferCandidate с canonical MaskRef/NPY и canvas-bound identity ResolvedTransform, own producer/attempt/fence и output roles; raw docs сохранены. Authored positive test вызывает real semantic_approved_candidate_child для mask/transform без function doubles. Source/intent/protection/candidate теперь доступны для соединения с Params/transport/activation. Тесты не запускались, physical/model qualification не заявлена. 20,190%, 4/30, .04 открыт.


### Full admission fixture: unified canonical Params + quota — 2026-10-15

Создан test_v27_background_cleanup_preview_activation.py с unified prepared_params_fixture: schema-valid worker Params, own intent, canonical candidate/protection provenance, WorkingImage и own CPU qualification policy со всеми required checks. Authored RGB/RGB+alpha сценарии связывают real Save/Inspect/Reserve/replay на одном run без task dispatch. Synthetic metadata, no function doubles; full transport publication/activation ещё не написаны. Тесты/миграции не запускались. 20,190%, 4/30, .04 открыт.


### Full admission fixture: real document tickets and transport — 2026-10-15

Unified fixture связан с real reserve/complete document tickets для deterministic Params/chunk/manifest. Inventory из фактических source/candidate/protection refs с optional alpha, canonical entries/input digests и node/transport projections. Authored RGB/alpha scenarios требуют documents_ready=true и draft/no task inspection. Document completion использует metadata-only version без actual upload, явно не physical verification. Полный commit activation ещё не добавлен. Тесты не запускались. 20,190%, 4/30, .04 открыт.


### Full admission fixture: atomic commit and rollback — 2026-10-15

Написаны full SQL register/seal/activate positive RGB/alpha cases с exact input rows/profile/quota, preserved proposed target и same run recovery. Добавлены expired quota/revoked policy/source и final revision overflow rollback scenarios (последний проверяет откат после task ready). Без authority doubles; synthetic metadata completion не physical evidence. Окончательная статическая сверка fixture и share-reservation rejection scenario ещё открыты; .04 не зачтён. Тесты не запускались. 20,190%, 4/30.


### Quota replay: чужой principal — 2026-10-15

Добавлены authored сценарии отказа replay при другом principal_id и principal_type=share: reservation, usage и frozen profile не изменяются, task не создаётся. Проверены исходные DDL/FK; отдельный non-null share_id требует существующего share_links fixture и остаётся открытым. Тесты не запускались, .04 открыт; 20,190%, 4/30.


### Admission: существующий share scope — 2026-10-15

Добавлен attach_share_scope: реальные collection и share_links в tenant/project исходного intent, permissions с обязательными boolean keys, FK-valid share_id. Authored replay case сохраняет user principal, изолируя share_id guard, и требует неизменной usage/reservation. Full activation failure matrix расширена share_quota: commit не оставляет task, input rows или sealed run. Нет SQL function doubles. Статически сверены исходные DDL/FK и import/helper call; тесты/миграции не запускались. Share-сценарий написан; окончательная сверка full fixture dependencies остаётся открытой. .04 пока без credit; 20,190%, 4/30.


### Admission: transport/registration static audit — 2026-10-15

Сверены fixture projections с 02302 transport_ready, 02310 inventory_exact, 02311 register inputs и 02312 atomic activation: closed node fields, six transport fields, JCS entries/chunk/manifest, UUIDv5 purpose names, deduplicated reference=source roots, ordinals 0..N-1 / 1000000 / 1000001 совпадают. Добавлены 6 authored corrupt-transport scenarios (input digest, entries SHA, order, missing entry, manifest SHA, direct roots), требующие CheckViolation и отсутствия task/inputs/sealing/usage changes с recoverable draft. Окончательная сверка source/candidate/protection producer provenance ещё не завершена. .04 остаётся открыт, 20,190%, 4/30. Тесты/миграции не запускались.


### Admission: protection provenance fixture defect — 2026-10-15

Статическая сверка 02310 protection_ready → 00570 semantic_protection_producer_ready выявила реальный дефект synthetic builder: mask_protected binary был application/x-npy, но legacy protected-rasterizer branch требует application/octet-stream. Исправлен только media type этой ветки; NPY bytes, candidate mask_pixels x-npy и алгоритмы обработки сохранены. Добавлены authored positive/private-predicate и negative producer fence/canonical result/root role/binary media/binary fence cases. Candidate sidecar gates 00597/00598 сверены: они требуют canonical candidate root, а sidecars — same producer attempt/fence/role, не дополнительного включения sidecars в result.outputs. Final source provenance сверка ещё открыта. Тесты не запускались. .04 открыт, 20,190%, 4/30.


### Итог coding audit — 2026-10-15

Подпункт .04 закрыт только по кодированию, без runtime PASS. Source trace сверена: fixture asset_versions.master_artifact_id/content_hash указывает exact WorkingImage; intent.reference_version_id совпадает с source version; pixels/optional alpha зарегистрированы; 00596/00599 требуют живой asset и exact SHA/bytes/media/verification/expiry, а 02310 проверяет exact dependency inventory. Добавлены activation rollback cases deleted_asset, unverified_pixels, expired_alpha. Все ранее перечисленные authored-fixture gaps (full commit/recovery, quota principal/share, transport registration, protection/candidate/source provenance) закрыты на уровне написанного кода и статической трассы. Для этапа тестирования остаются выполнение всей матрицы, реальная object-store verification, timeout/concurrency injection и hardware/photo qualification. Данные fixture синтетические, миграции не применялись.

Прогресс: 5/30 подпунктов P0.1.09, 14 полных единиц из 70; weighted 14+5/30, 20,238095% (20,238%); прирост 0,047619 п.п.


### Background cleanup .05: авторизованная выдача — 2026-10-15

Начата статическая трасса response после physical verification. Добавлены authored stale request cases для workflow revision, target revision, plan SHA и candidate SHA с требованием нулевого signer/authority IO. SHA mutation гарантированно отличается от исходной. Добавлен canceled-before-IO case. Дальше требуется сквозная сверка presentation reader/physical verifier/live SQL receipt и route wiring. .05 открыт; 20,238%, 5/30. Тесты не запускались.


### Background cleanup .05: physical read cancellation — 2026-10-15

Сверен provider → presentation reader → physical PNG/linkage reread → seal receipt → repeated authority → response. Добавлен final ctx.Err guard перед physical selection return и authored cancellation на каждой из 4 media signatures. Начата read coding trace с явными оставшимися SQL/physical fixture gaps. .05 открыт; 20,238%, 5/30. Тесты не запускались.


### Physical PNG stage — 2026-10-15

Добавлены authored real PNG byte scenarios для affected/protected через readAndVerifyPreviewPNG: valid overlay, truncated/oversized/corrupt payload и Close failure. Проверяется exact object key/version/SHA/bytes/media и однократное закрытие stream. Сверена shared reader: bounded read source.Bytes+1, Close до validation, exact length/hash/CRC/role colors. Synthetic store recorder, не SQL/remote object-store proof. Полный WorkingImage/evidence/SQL presentation positive pipeline ещё открыт, .05 без credit. 20,238%, 5/30. Тесты не запускались.


### Physical document chain — 2026-10-15

Написан positive authored document chain: repository Python WorkingImage fixture → exact source pin/canvas hash → actual float32 RGB NPY header/payload and streaming verifier → changed pixels ref only → own numerical evidence decode → image binding → presentation decode. Вызовы используют настоящие hash/schema/NPY validators; проверены сигнатуры width/height int64. Display refs в этом scenario синтетические, SQL/provider не вызывается; полный authorized physical provider fixture остаётся открыт. .05 без credit, 20,238%, 5/30. Тесты не запускались.


### Authenticated presentation_source fixture — 2026-10-15

Добавлен test_v27_background_cleanup_preview_presentation_source.py: real saved review/admission/commit closure, synthetic canonical task result и presentation metadata без SQL function doubles. Authored positive exact task/result/attempt/fence/root/SHA/version; 6 stale producer/closure cases; 3 foreign/revoked editor refusals. Сверены 02313 inspect, 02316 source и 02302 closure requirements. Не вызывает worker/actual object upload; root JSON намеренно минимальный, schema/physical checks покрываются отдельно. Full physical provider + source/sidecars fixture composition ещё открыт. .05 без credit, 20,238%, 5/30. Тесты/миграции не запускались.


### Real source → sidecars SQL chain — 2026-10-15

Добавлен authorized_sidecars_fixture поверх real published_transport/commit/canonical result и исходного presentation_source; нет authority function replacements. Closed presentation JSON hash/length связан с canonical output; 7 exact role/ref/media/version. Authored positive seven-sidecar read, 7 wrong-fence отказов и session revocation между read. 02316 columns/order/required fields сверены. Sidecar bytes metadata synthetic и не считаются physical proof. До закрытия .05 требуется соединённый authored Go physical reader scenario; SQL/PNG/document stages написаны отдельно. 20,238%, 5/30. Тесты не запускались.


### Итог coding audit .05 — 2026-10-15

Написан объединённый Go pipeline ReadPhysicalBackgroundCleanupPreview → BuildBackgroundCleanupPreviewResponse: фактические source/output WorkingImage, streaming float32 RGB NPY, own evidence/presentation, PNG before/after с repository srgb_v1.icc iCCP, цветные affected/protected overlays. Version/SHA/length pinned object recorder и query recorder; положительный сценарий требует verified image и четыре signed URLs с двумя authority checks. Отказы corrupt pixels/after, changed canonical result после последнего PNG, revoked metadata и cancellation возвращают unverified empty result, все streams закрываются. Типы Row/Rows, query column order и NPY dimensions сверены статически; gofmt выполнен. SQL source/sidecars проверяются отдельными authored real-authority fixtures, без function replacements; Go recorders не доказывают SQL ACL. main.go:403 и semantic_preview.go provider factory связывают background_cleanup router/provider.

Подпункт закрыт по написанному коду и статической трассе. Обязательный этап тестирования сохраняет исполнение всех сценариев, combined real PostgreSQL+object-store+HTTP lifecycle, runtime concurrency/revocation и photo/hardware qualification. Ни PASS, ни physical preservation recomputation не заявлены. 6/30 подпунктов, weighted 14+6/30 из 70 = 20,285714% (20,286%); прирост 0,047619 п.п.


### Background cleanup .06: real receipt SQL fixture — 2026-10-15

Добавлены authored positive seal/replay exact receipt и unchanged proposed target, три altered-document byte refusals и revoked editor. Без authority doubles, поверх настоящей activation/source/sidecars цепочки. Synthetic metadata явно не physical evidence. Начата selection coding trace; conflict/photographer selection wiring ещё открыты. 20,286%, 6/30; тесты не запускались.


### Receipt conflict и exact shown selection — 2026-10-15

Добавлены authored 4 stored receipt conflict cases с сохранением existing value (owner corruption fixture, не worker rotation). Реальный private selection gate 02322 вызывается ролью photo_semantic_admission: positive exact run/receipt без approval; отказы other render/presentation, producer fence, target revision, unverified closure source, missing receipt. Статически сверены 13 args, scope и role grants. Client decisions требует onReady preview; candidate preview onLoad собирает 4 роли, resolver принимает approve только с decision.Preview и matching target/candidate. Полная сверка клиентского proof lifecycle ещё открыта. .06 открыт, 20,286%, 6/30. Тесты не запускались.


### Итог coding audit .06 — 2026-10-15

Client trace: CandidatePreviewScope clear снимает ready proof при load/error/expiry/background; Set role требует все 4 distinct onLoad, проверяет active/live/current identity и повторный decoder. Shared decisions canApprove и submit сверяют fresh semanticPreviewQuery/revision/candidate/expiry, передают exact render/evidence refs. SQLShownBackgroundCleanupApplyResolver:84–96 повторно физически читает preview и сравнивает обе decision refs с receipt. Private 02322 selection подтверждает immutable receipt/canonical attempt/current closure, не утверждая target. Добавлены authored duplicate role onLoad и replacement preview pending revocation tests. Existing authored suite покрывает error/revision/candidate/background и uncertain exact request replay. Signature fixture уточнена SemanticPreviewQuery/optional AbortSignal, без typecheck.

.06 закрыт по кодированию/статической трассе, не runtime PASS. Реальные SQL receipt/selection cases, Go decoder и shared client scenarios написаны; execution, concurrency, full HTTP/object-store lifecycle отложены до тестирования. 7/30 подпунктов, weighted 14+7/30 из 70 = 20,333333% (20,333%); +0,047619 п.п.


### Background cleanup .07: cancellation guards — 2026-10-15

Начата Apply coding trace. Добавлены ранний/финальный ctx guards в CommitShownBackgroundCleanup и финальный guard prepared batch; authored canceled-before-DB cases. Atomic full fixture и SQL transitions audit открыты. 20,333%, 7/30; тесты не запускались.


### .07: own copy preparation composition — 2026-10-15

Source documents fixture расширен цепочкой CompileShownBackgroundCleanupAdoption → CompileShownBackgroundCleanupTask → PrepareShownBackgroundCleanupWithPublisher → commit payload. Authored valid CPU adopt_shown_background_cleanup, revoked-before (factory/publication не вызываются), revoked-after (empty prepared payload) и publisher-factory failure. Candidate/intent/source/mask/protection реальные hash-bound fixture documents; verified physical selection здесь synthetic precondition, presentation/evidence/image bundle JSON minimal — явно не physical/photo evidence. Full real SQL atomic Apply fixture и transitions/idempotency audit ещё открыты. 20,333%, 7/30. Тесты не запускались.


### .07: private commit receipt refusal matrix — 2026-10-15

Написан test_v27_background_cleanup_apply_receipt_gate.py поверх real sealed preview receipt. Private commit вызывается photo_semantic_admission: missing receipt / wrong hash / wrong principal должны вернуть 40001 до shell; valid pending receipt с пустыми documents должен вернуть 22023 через initialize_semantic_review coverage gate. Authored assertions требуют отсутствие child/tasks, unchanged target/usage/preview receipt и незаполненный Apply response receipt. Это narrow rejection evidence, не full atomic positive/late rollback proof. SQL 02329 order receipt→reserved draft→seal→finish сверена; полная positive fixture остаётся открытой. 20,333%, 7/30; тесты не запускались.


### .07: full review document prerequisite — 2026-10-15

Common semantic producer fixture получил opt-in assembly_output_builder; defaults сохранены. Saved cleanup authority fixture forwards optional calibration/snapshot/assembly builders. Добавлены schema-valid own cleanup Calibration, InputSnapshot с canonical snapshot_digest и SemanticPlan с actual candidate/intent/calibration. Новый prepared_review_fixture собирает exact six documents из real semantic_review_plan_sources, требует SHA/bytes agreement. Authored positive initialize_semantic_review replay должен вернуть тот же intent и единственный unchanged target. Это prerequisite, не full Apply success/rollback proof; static audit новой композиции и последующая подготовка decision/copy transport ещё открыты. 20,333%, 7/30. Тесты/миграции не запускались.


### .07: six review docs → preview activation → sealed selection — 2026-10-15

Опциональный review_documents=True связан через prepared_params/published_transport/presentation_source/sidecars/receipt fixtures; defaults unchanged. Full review builder поддерживает RGB/alpha. Новый authored scenario проводит full six-document fixture через real preview commit, seal и exact selection lock, затем повторный initialize с теми же review bytes; требует unchanged receipt/proposed target. Никаких function authority doubles, physical bytes остаются synthetic metadata. Decision/approved params/copy transport публикация и atomic Apply finish ещё открыты. 20,333%, 7/30. Тесты не запускались.


### .07: published decision → exact selection gate — 2026-10-15

Добавлен published_decision_fixture поверх full six-document preview/receipt: реальные reserve/complete review_selection, SHA точных request/body, parent run из workflow. Authored gate scenarios: повторный lock exact shown selection без target transition, altered document bytes → 23514, revoked session → 42501 с rollback и unchanged target. Private gate вызывается владельцем fixture; это не доказательство public ACL. Object version metadata synthetic, не object-store proof. Approved params/copy transport и full atomic finish/late rollback остаются открытыми. 20,333%, 7/30; тесты не запускались.


### .07: own approved edit publication inputs/retry — 2026-10-15

Добавлен approved_params_fixture: exact source/candidate/mask/transform/intent, semantic.edit child+target UUIDv5 owner и Params UUIDv5, canonical-shaped approved body с nullable ev/prompt. Authored API reserve→resume token rotation→stale token refusal→complete/replay→version conflict; storage increment ровно один раз, target unchanged, child/task отсутствуют. SQL signatures 15/17 и Go ApprovedEdit fields сверены статически. Это публикационный prerequisite, не full Apply proof; copy transport и atomic finish/late rollback открыты. 20,333%, 7/30. Тесты не запускались.


### .07: shown copy Params publication — 2026-10-15

Добавлен shown_copy_params_fixture: реальная approved edit metadata publication, exact sealed receipt producer refs/attempt/fence и original model/runtime/device pins; deterministic own copy task/Params UUIDv5. Authored positive own transport task_params reserve/complete/replay, однократный storage increment и отсутствие Apply child/target transition. Authored altered attempt/fence/runtime refusals до reservation с unchanged usage/no copy documents. Сверены SQL 02323 params_bound и 02324 signatures 18/20. Chunk/manifest transport/full inventory и atomic finish ещё открыты. 20,333%, 7/30; тесты не запускались.


### .07: full copy inventory / chunk / manifest — 2026-10-15

Добавлен exact dependency projection fixture по 02325: copy Params, ApprovedSemanticEdit, presentation/numerical/image/pixels, четыре EncodedImage, plan/intent/candidate/source/protection/mask/transform/reference и binary roots, optional alpha/dense warp. Dedup требует одинаковый contract/hash для UUID; UUID sorted canonical JSON определяет entries_sha/input_digest и chunk/manifest SHA. Реальные reserve/complete публикуют все три own transport documents до child. Authored pure exact inventory/transport positive и manifest hash/order/missing entry refusals. Execution profile пока пустой и явно не квалифицирует исполнение: admission/profile gate, full document gate и atomic finish/rollback открыты. 20,333%, 7/30. Тесты не запускались.


### .07: full WorkingImage and CPU profile boundaries — 2026-10-15

Статический аудит выявил gap fixture: receipt image содержал только pixels, тогда как 02325 требует image-minus-pixels == source-minus-pixels и color/canvas/dtype. Для review_documents=True теперь клонируется полный source WorkingImage с заменой pixels; default narrow receipt fixture сохранён. Authored CPU profile shape/resource boundary cases: минимальные RAM/scratch/output, запрет VRAM, exact pixel_count и unchanged non-pixel fields. Empty qualification явно shape-only, не dispatch grant. Live qualification/profile composition и atomic commit/rollback открыты. 20,333%, 7/30; тесты не запускались.


### .07: own CPU admission documents / full atomic positive scenario — 2026-10-15

Добавлен admission_task_fixture с complete ten raw documents, published own transport и own-operation imported CPU qualification metadata. Profile использует exact pins и resource floors; qualification fixture synthetic (default quality policy), не measured/photo/deployment qualification. Зафиксирован bytes→UTF8 protection body для Jsonb. Authored full admission_documents_ready и private atomic commit positive: exact copy ready task, queued sealed child/target, parent revision increment и completed idempotency response. Сценарии пока не исполнены; static composition audit, late rollback и replay остаются открытыми, .07 не закрыт. 20,333%, 7/30.


### .07: late rollback / private duplicate / revision wire parity — 2026-10-15

Authored real EVENT_CURSOR_EXHAUSTED scenario after activation/parent/receipt updates: exact exception, unchanged target/parent/usage/events/outbox, no child/task/batch/quota and pending receipt. Positive scenario расширен duplicate private mutation refusal preserving completed receipt/task cardinality; явно не API replay (его обрабатывает Go idempotency.Use). Static audit Go decision.go/decision_document.go выявил numeric revision в fixture: request expected_revision и decision expected/target_revision исправлены на wire strings. Full composition audit и Go API replay authored coverage остаются открытыми; .07 не закрыт. 20,333%, 7/30; тесты не запускались.


### .07: Go completed receipt replay — 2026-10-15

Authored CommitShownBackgroundCleanup replay recorder поддерживает только authenticated identity, set_config, idempotency INSERT/locked SELECT. Valid completed receipt должен возвращать canonical response при пустых prepared/decision/tasks и не вызывать source/resource/admission queries. Wrong workflow/resource и changed request hash должны вернуть empty response/error. pgx Row Scan pointer types и Exec CommandTag сверены с idempotency.Use; gofmt выполнен. Recorder не доказывает live SQL authorization; SQL atomic/rollback authored отдельно. Full fixture/transitions static audit всё ещё открыт; 20,333%, 7/30. Тесты не запускались.


### .07: complete document refusal matrix / canonical numeric parity — 2026-10-15

Authored full preflight loop изменяет raw bytes каждого из 10 documents и требует false; disabled own admission policy false с восстановлением true, no target/child mutation. Static audit 00544 initializer, 00560 profile и 02325 document binding: review snapshot/model/calibration/reference/candidate coverage сопоставлены builders. Approved edit encoder исправлен на canonical_bytes (strength float 1.0 обязан совпадать с Go JCS 1); same source/reference и candidate target assumptions теперь explicit assertions. Runtime qualification fixture по-прежнему synthetic default quality policy и не deployment/photo evidence. Полный transitions/input sealing audit ещё открыт; 20,333%, 7/30; тесты не запускались.


### .07: persisted seal/quota/event assertions and transition audit — 2026-10-15

Full positive atomic scenario теперь сверяет каждый persisted v2_task_inputs ref/contract/ordinal, chunk 1000000/manifest 1000001, task.inputs manifest+direct_roots, sealed execution_profile pins, exact user output quota и review run_event/outbox response. Static audit 02327–02329 прослеживает shell(all-doc checks before writes)→quota→register inputs→seal→activation→revision/receipt/events; late cursor failure расположен после mutations и обязан откатить statement. Остаток .07 уточнён: объединённый PrepareShownBackgroundCleanupApplyBatch публикационный сценарий и итоговый audit recheck branches. Отдельные compile/prepare-copy и SQL commit scenarios не подменяют batch preparation coverage. 20,333%, 7/30; тесты не запускались.


### .07: testable batch publication orchestration — 2026-10-15

PrepareShownBackgroundCleanupApplyBatch делегирует private core с approved/copyPublisher/decision callbacks; production wrapper всегда binds real authenticated publishers и передаёт core-owned decision в approved/decision publication. Compile-all-before-first-publication и recheck порядок сохранены. Authored core invalid decision/no callbacks и canceled-before-dependencies сценарии; gofmt выполнен, static signatures сверены. Positive complete core publication/recheck scenario ещё открыт. Это подготовка покрытия, не закрытие .07; 20,333%, 7/30; тесты не запускались.


### .07: combined positive batch preparation authored — 2026-10-15

cleanupReviewForSource строит шесть actual hash-bound review docs из existing validated Calibration и exact own candidate/intent/source; compile snapshot и PrepareReviewDocuments реальные. Existing source-document scenario теперь вызывает private complete batch core: approved callback→real copy compilation/recording publication→VerifyBatch→real BuildDecisionProof; assertions approved before copy, decision after copy, 5 rechecks, one task and exact child. Physical selection остаётся synthetic verified precondition, publishers recorders; SQL publication/commit покрыты отдельными authored fixtures. New positive composition static audit и failure branches ещё открыты, .07 не закрыт. 20,333%, 7/30; тесты не запускались.


### .07: batch recheck matrix / fixture revision fix — 2026-10-15

Static VerifyShownBackgroundCleanupBatch audit выявил default preview revisions (большие decimal fixtures) против review revision1: combined fixture исправлен exact workflow/target revision1 и receipt params SHA recompute. Authored отказ на каждом из пяти rechecks: empty batch, no later approved/copy/decision publication, partial already-published artifacts не трактуются admitted. Production core добавил ctx.Err непосредственно перед approved/decision callbacks после recheck. Positive/failure scenario по-прежнему synthetic physical precondition; no runtime PASS. Publisher failure/cancellation matrix и итоговый audit ещё открыты. 20,333%, 7/30; тесты не запускались.


### .07: publisher/cancellation matrix and batch commit projection — 2026-10-15

Authored full core modes approved/factory/copy/decision errors и cancel-before-approved/before-decision/during-copy: empty result, no later publication/decision. Positive core теперь использует real decisionDocumentOwner с actor/request SHA и проходит PrepareShownBackgroundCleanupBatchCommitInputs, требуя nonempty initialization documents/tasks. Static signatures/cancellation guards сверены; gofmt выполнен. Recorders не подтверждают durable uploads; SQL fixture separately covers publication/atomicity. Итоговая сверка нового покрытия ещё открыта; 20,333%, 7/30; тесты не запускались.


### Итог coding audit background_cleanup .07 — 2026-10-15

Исправлен production preflight gap: full raw document bundle теперь проверяется для каждого draft до первой approved publication, вместе с closure compilation. Authored substituted raw WorkingImage scenario требует no callbacks. Сверены exact six review docs, canonical edit, receipt producer pins, own copy inventory/transport/profile, SQL 02324–02329 публикация→shell/quota→register/seal→activate→receipt/events; late rollback и duplicate mutation guard authored. Go full batch positive→commit projection, five recheck refusals, publisher/cancellation matrix и completed replay authored. gofmt и scoped git diff --check выполнены; это не runtime PASS. .07 закрыт как coding_closed_unverified; реальное исполнение всех сценариев, live PostgreSQL/object-store/HTTP lifecycle, concurrency и hardware/photo qualification обязательны в этапе тестирования. Следующий .08: worker execution/adoption. 8/30, weighted 14+8/30 из70 = 20,380952% (20,381%); прирост 0,047619 п.п.


### Начат background_cleanup .08 — 2026-10-15

Worker/adoption audit: исправлены RAM floor и exact estimate keys до object IO, authored refusal matrix. Полный execution/staging/SQL adoption lifecycle открыт. 20,381%, 8/30; тесты не запускались.


### .08: final progress lease fence — 2026-10-15

Worker adapter теперь вызывает active после final ctx.progress(1,1) до OperationResult: rotation/revocation callback не может вернуть stale success. Authored adapter-boundary valid zero-device result и rotated fencing token отказ. Execute/staging заменён только в данном adapter scope test; это не physical pipeline evidence. Static staging trace: NPY bounded finite/nonnegative read, streaming size/SHA exact copy, new pixel/image IDs и repeated admission before/after stages. Full own worker/adoption lifecycle audit остаётся открыт. 20,381%, 8/30; тесты не запускались.


### .08: actual NPY copy staging authored — 2026-10-15

Shared stage_shown_preview_image получил authored scenarios с настоящим float32 RGB NPY 400x300, реальными read_array/stream copy/hash и canonical WorkingImage: exact source bytes, unchanged non-pixel fields, new SHA pins, pixel/image staging SHA refusals, authority revoked after pixels and balanced input context. Local scratch/publish recorders, не durable object store/SQL evidence; ICC fixture metadata не photo qualification. Own execute→staging→adoption composition открыт. 20,381%, 8/30; тесты не запускались.


### .08: own complete copy document fixture — 2026-10-15

Добавлен copy_documents_fixture: 8x8 actual NPY, exact source/candidate/intent pins, own cleanup approved edit, typed evidence/presentation, selected receipt refs and deterministic preview task. Authored full bind_shown_background_cleanup_adoption positive и altered bytes каждого из семи documents refusals. Изображение same pixels/source with separate document identity; color metadata synthetic, не actual ICC/quality proof. Execute+stage composition и SQL adopter audit открыты. 20,381%, 8/30; тесты не запускались.


### .08: own execute→NPY staging / adopter active fence — 2026-10-15

Authored complete execute_shown_background_cleanup_adoption chain с own seven hash-bound documents, actual NPY, actual bind/stage/read_array/stream copy. Positive exact pixel SHA/new outputs; expected_pixel_count/color refusal before stage; revoked admission after pixel upload forbids WorkingImage stage and closes input. Storage/scratch/admission are recorders, no durable SQL/object claim. Output verifier теперь active() между original/copy pixel evidence reads (до следующего IO после revocation). Adopter full evidence/SQL chain и coverage новой проверки открыты. 20,381%, 8/30; тесты не запускались.


### .08: complete output verifier evidence scenarios — 2026-10-15

Authored own output verifier с exact params/shown/output JSON SHA/length evidence и original/copied NPY metadata. Positive requires same pixels hash, two live admissions; refusal matrix changed ICC/non-pixel document, changed pixel SHA, wrong output role/size, late admission, revocation between original/copy metadata reads. Последний сценарий требует copied ref not read после active fence. Evidence reader recorder не доказывает DB provenance/durable bytes; generic adoption/SQL commit/claim audit открыты. 20,381%, 8/30; тесты не запускались.


### .08: persisted worker closure real SQL fixture — 2026-10-15

Добавлен reusable committed_copy_fixture через actual private atomic Apply entry point; synthetic uploaded/qualification metadata, no function doubles. Authored semantic_shown_background_cleanup_closure_ready positive и missing registered chunk/unverified root/task cancellation refusals с rollback restoration и unchanged ready attempt0. Static inspected 02334 claim/start(full frozen inputs/profile/quota) и AdoptionService own installer/14-arg commit routing; claim lease and dedicated SQL adoption positive/rollback ещё открыты. 20,381%, 8/30; тесты не запускались.

### Детализация публикации прогресса — 2026-10-15

Применять [правила детального учёта](coding-progress-reporting.md): текущую часть background_cleanup.08 разбить на пять фиксированных подшагов по 0,009524 процентного пункта общего плана. Зачёт только после подтверждения реализации и evidence; без повторного зачёта родительской части. Базовый процент при введении детализации остаётся **20,381%**. Тестирование отдельно, сейчас не запускается.

### Детальный прогресс: background_cleanup.08.1

Lease/start закрыт как coding_closed_unverified: реальный scheduler dispatch, worker claim, fencing, quota/start и написанные сценарии отказов/rollback. Evidence: [worker trace](audit/background-cleanup-worker-adoption-coding-trace.md). Закрыт 1 из 5 подшагов .08. Прирост 0,009524 п. п.; общий процент **20,390%**. Остальные подшаги .08 открыты. Тесты не запускались.

### Детальный прогресс: background_cleanup.08.2

Точное исполнение копирования закрыто coding_closed_unverified: bounded NPY validation, сохранение HDR/байтов, SHA, ресурсы и fencing; сценарии написаны, не выполнены. Закрыты 2/5 подшагов .08. Общий процент **20,400%**, прирост 0,009524 п. п. Evidence: [worker trace](audit/background-cleanup-worker-adoption-coding-trace.md).

### Детальный прогресс: background_cleanup.08.3

Независимая проверка результата закрыта coding_closed_unverified: metadata/JSON, physical NPY proof, renewed admission и generic closure. Закрыты 3/5 подшагов .08, общий процент **20,410%**, прирост 0,009524 п. п. Тесты не запускались. Evidence: [worker trace](audit/background-cleanup-worker-adoption-coding-trace.md).

### Детальный прогресс: background_cleanup.08.4

Атомарное принятие и asset version закрыты coding_closed_unverified: положительная SQL-цепочка, live refusal, late rollback, replay и lineage. Закрыты 4/5 подшагов .08. Общий процент **20,419%**, прирост 0,009524 п. п. Тесты не запускались. Evidence: [worker trace](audit/background-cleanup-worker-adoption-coding-trace.md).

### Детальный прогресс: background_cleanup.08 завершён по кодированию

08.5 и весь .08 закрыты coding_closed_unverified; пять дробных долей заменены весом целой части 1/30 без двойного зачёта. Закрыты 9/30 детей P0.1.09. Общий процент **20,429%**, прирост 0,009524 п. п. Никакие тесты не запускались. Далее .09: registration/startup/capability и глобальные contract pins. Evidence: [worker trace](audit/background-cleanup-worker-adoption-coding-trace.md).

### background_cleanup.09 завершён по кодированию

Регистрация/startup/capability:5/5 подшагов, coding_closed_unverified; evidence — audit/background-cleanup-startup-coding-trace.md. Исправлены schema pin, startup ICC/config boundaries, scratch environment guard, array/object qualification, activation и tenant API installation readiness, review kind/HTTP/SDK/UI/live async gates/recovery. Tests/migrations/builds NOT RUN. Следующий пункт background_cleanup.10; общий coding20,476%,10/30 частей, без двойного зачёта substeps.

### Прогресс background_cleanup.10.2

Закрыт по кодированию подшаг compute/preservation/resources/cancellation, evidence: audit/background-cleanup-full-coding-trace.md. Вся часть .10 остаётся открытой: 1/5. Общий процент кодирования 20,486%, без запуска тестов. Далее — .10.1 входная трасса и .10.3 публикация/чтение/выбор; .10.4 Apply и .10.5 итоговая сверка ещё открыты.

Закрыт background_cleanup.10.1 по кодированию: HTTP/query, scoped review/source/params, SQL saved run/replay/tenant scope. Evidence: audit/background-cleanup-full-coding-trace.md. .10 — 2/5; coding 20,495%. Далее .10.3 publication/read/selection. Тесты и миграции не запускались.

Закрыт background_cleanup.10.3 по кодированию: canonical bundle → physical read/receipt → показ и точный выбор. Evidence audit/background-cleanup-full-coding-trace.md. .10 — 3/5; coding 20,505%. Далее .10.4 Apply/adoption/materialization и .10.5 итоговая сверка. Тесты не запускались.

Закрыт background_cleanup.10.4 по кодированию; evidence audit/background-cleanup-full-coding-trace.md. .10 — 4/5, coding 20,514%. Далее .10.5: итоговая межмодульная статическая сверка. Все runtime тесты/миграции остаются непроведёнными.


Закрыт background_cleanup.10.5 и весь .10 по кодированию: карта отказов и production installer/adopter wiring сверены; исправлены result admission size, qualification cancellation и physical metadata/EOF fences. Evidence: audit/background-cleanup-full-coding-trace.md. Теперь11/30 частей P0.1.09, coding20,524%, прирост+0,009524п.п. Тесты/сборки/миграции не запускались. Далее retouch_region.01 по исходному контракту операции; полный V20–V32 scope сохраняется.


Закрыт retouch_region.01.1 по кодированию: own input/Go freeze+strict parse, shared Go/Python wire fixture и authored негативные сценарии. Evidence audit/retouch-region-contract-coding-trace.md. Часть1/5; кодирование20,533%,+0,009524п.п. Далее qualified retouch adapter policy и binder (.01.2). Schema/catalog/production operation ещё открыты. Тесты не запускались.


Закрыт retouch_region.01.4 по кодированию: own shown producer input, Go freeze/strict parse, общий wire и authored negative scenarios. Evidence audit/retouch-region-contract-coding-trace.md. Часть2/5; coding20,543%,+0,009524п.п. Qualified adapter/binder, output publication и schema/catalog остаются открыты. Тесты не запускались.


Закрыт retouch_region.01.3 по кодированию: independent result linkage и Go/Python output contracts с shared fixtures. Evidence audit/retouch-region-contract-coding-trace.md. Часть3/5, coding20,552%,+0,009524п.п. Qualified adapter policy и schema/catalog открыты; физическая публикация/authority относится к последующим частям. Тесты не запускались.
