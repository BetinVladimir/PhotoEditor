# Правило подробного отчёта о прогрессе — 2026-10-16

По запросу пользователя прогресс публикуется после каждого закрытого небольшого подпункта, без ожидания завершения всего модуля.

Формат отчёта: **общий процент → прирост в процентных пунктах → закрытый подпункт → локально закрыто / всего → следующий подпункт**.

Вес подпунктов фиксируется до начала выполнения. Разбиение пункта, написание отчёта и повторная проверка уже зачтённой работы сами по себе не увеличивают процент. Части учитываются один раз; при закрытии родительского пункта их вес не прибавляется повторно. Для новых участков фиксируются отдельные проверяемые результаты: структуры данных, миграции, серверная логика, worker, клиент, связи между ними и написанные сценарии тестов — по применимости.

Текущая база: **28,571429%** остаточного плана MVP до глубокого тестирования: **20 из 70** единиц. Relight04 **4/4**, итоговая трасса04.4 **4/4**; P0.2.03 целиком **10/10**. Процент не означает готовность всей системы или результат тестирования.

При дальнейшем разбиении открытого подпункта его исходный вес сохраняется: сумма весов новых частей равна весу подпункта. Перед началом каждой части фиксируются её результат, критерий закрытия и вес в `mvp-coding-progress.json`. Уже зачтённые части не переразбиваются ради нового прироста. В каждом сообщении о закрытии показываются предыдущий и новый общий процент, прирост, идентификатор результата, локальный счётчик и следующий шаг. Если часть ещё не закрыта, сообщается выполненная работа без изменения процента.

---

### Allocator/heartbeat — P0.2.08 закрыт по коду

**27,142857% → 28,571429% (+1,428571 п.п.)**. P0.2:6/10. Источники и написанные сценарии CPU/CUDA/Metal, атомарной публикации, residency и отказов сведены в audit/backend-heartbeat-final-coding-trace-2026-10-16.json. Тесты и миграции не запускались; аппаратная квалификация отложена. Далее P0.2.09 клиентские availability paths.

### Матрица операций/backend/model locks — P0.2.05 закрыт по коду

**25,714286% → 27,142857% (+1,428571 п.п.)**. Native matrix покрывает 19 моделей; ModelPool/worker/doctor/capture используют её без backend fallback. Исправлены Metal startup и seed, U2Net support и SQL activation/model-lock readiness. Сценарии написаны, не запускались. P0.2:5/10. Трасса: audit/operation-backend-matrix-coding-trace-2026-10-16.json. Далее установка и remote RTX path.

### Metal/RTX memory policy — P0.2.04 закрыт по коду

**24,285714% → 25,714286% (+1,428571 п.п.)**. CUDA allocator ограничен до загрузки моделей; live capacity учитывает reserved memory и бюджет не выше 10 000 000 000 байт. Metal сохраняет отдельный working-set budget без фиксированных 10 ГБ. Написаны allocator/startup/capacity сценарии, не запускались. P0.2:4/10. Трасса: audit/backend-memory-policy-coding-trace-2026-10-16.json. Далее матрица операций/backend/model locks.

### Вложенные действия редактора — итоговая трасса .10

**24,142857% → 24,285714% (+0,142857 п.п.)**. P0.2.03:10/10, родитель закрыт по коду без повторного начисления веса. Обновлён текущий source inventory; исправлен поздний conflict reload и написан route-сценарий. Runtime gates отложены, проверки не запускались. Трасса: audit/editor-nested-final-coding-trace-2026-10-16.json. Далее P0.2.04 Metal/RTX memory policy.


### Render/delivery — .09 закрыт по коду

**24,000000% → 24,142857% (+0,142857 п.п.)**. Исправлены live render.compose и editor.preview guards, включая серверный CPU admission нового preview и late-disable delivery selection. Сверены freeze/selected preview/delivery activation/worker связи. Unit/DOM сценарии написаны, не запускались. P0.2.03:9/10. Трасса: audit/render-delivery-coding-trace-2026-10-16.json. Далее итоговая трасса .10.


### Multi-frame/semantic — .08 закрыт по коду

**23,857143% → 24,000000% (+0,142857 п.п.)**. Actual multi route повторяет live project/operation допуск перед snapshot и submit; immutable retries/accepted receipt сохранены. Добавлены unit/route revocation/cancellation сценарии. Semantic kind-specific await guards и server/worker trace сверены. Проверки не запускались. P0.2.03:8/10. Трасса: audit/multi-semantic-coding-trace-2026-10-16.json. Далее render/delivery.


### Negative/perspective — .07 закрыт по коду

**23,714286% → 23,857143% (+0,142857 п.п.)**. Исправлены exact source recovery и отдельная доступность optical.perspective_lines; mutation gate повторяется после awaits. Написаны unit/DOM source-change и detector revocation/manual-edit сценарии. Server admission и CPU worker trace сохранены. Проверки не запускались. P0.2.03:7/10. Трасса: audit/negative-perspective-coding-trace-2026-10-16.json. Далее multi-frame/semantic.


### Reflection/glare — .06 закрыт по коду

**23,571429% → 23,714286% (+0,142857 п.п.)**. Исправлены source/principal scope, exact glare recovery и Metal startup исключением неподдерживаемых providers. Добавлены DOM/selector/build-worker сценарии; все не запускались. Реальная регистрация/capability convergence, модельная обработка и качество остаются тестовыми gates. P0.2.03:6/10. Трасса: audit/reflection-glare-coding-trace-2026-10-16.json. Далее negative/perspective.


### Portrait/open eyes — .05 закрыт по коду

**23,428571% → 23,571429% (+0,142857 п.п.)**. Замкнут nested availability/preparation/submission/recovery и CPU worker trace. Исправлены stale control scope и exact source recovery; добавлен сценарий отзыва hair capability во время подготовки. P0.2.03:5/10. Тесты не запускались; реальное качество и полный SQL/worker путь требуют тестовой фазы. Трасса: audit/portrait-open-eyes-coding-trace-2026-10-16.json. Далее reflection/glare.


### Relight итоговая совместимость — .8 закрыт по коду

**23,427455% → 23,428571% (+0,001116 п.п.)**. Сверена цепочка migration/adoption/recovery/manifest/current authority/API/client/startup. Aggregate04.4.4:8/8; relight04:4/4; P0.2.03:4/10. Исполнение и аппаратная квалификация остаются обязательными на тестовой фазе. Трасса: audit/relight-final-compatibility-coding-trace-2026-10-16.json. Начат portrait/open-eyes: исправлена смена scope исходника в контролах, написаны два DOM-сценария; частичный пункт не засчитан.


### Relight решение и завершение run — .3 закрыт по коду

**23,426339% → 23,427455% (+0,001116 п.п.)**. Сверены canonical decision, immutable update, viewed grant rollback, production scheduler и atomic completion events/outbox. Добавлены accept/reject/skip receipt и SQL pending/cancelled/privilege сценарии. Aggregate04.4.4:7/8. Выполнение, успешные qualified SQL и concurrency проверки отложены. Трасса: audit/relight-decision-finalization-coding-trace-2026-10-16.json. Далее совместимость .8.


### Relight preview и журнал решения — .2 и .4 закрыты по коду

**23,424107% → 23,426339% (+0,002232 п.п.)**. Добавлена текущая SQL authority во все pending preview read paths; повторная проверка после physical IO подтверждена чтением источников. Журнал сверяет неизменяемый actor-bound intent и точный receipt; добавлены сценарии storage removal failure и persistent logout generation. Aggregate04.4.4:6/8. Проверки не запускались. Трасса: audit/relight-preview-journal-coding-trace-2026-10-16.json. Далее .3 и .8.

### Relight publication — кодовый подпункт .1 закрыт без выполнения проверок

**23,422991% → 23,424107% (+0,001116 п.п.)**. Замкнуты dedicated adoption/private publication, immutable pins, replay и legacy queue recovery; дополнены Params bytes/version guards. Написаны8 SQL publication/replay/refusal/outer rollback сценариев и9 discovery/privilege сценариев. Aggregate04.4.4:4/8. Owner-seeded relational fixture не равен qualified physical adoption. Все проверки отложены; PASS не заявляется. Трасса: audit/relight-publication-coding-trace-2026-10-16.json.

### Relight viewed receipt — кодовый подпункт .6 закрыт без выполнения проверок

**23,421875% → 23,422991% (+0,001116 п.п.)**. Подключены issuer/decision/source binding, fresh credential reconstruction, private SQL gate/rollback, UI acknowledgement и endpoint contracts/startup. Token/service/physical-byte/owner-seeded SQL/client сценарии написаны. Aggregate04.4.4:3/8. Полный qualified live integration/post-update rollback fixture и аппаратные проверки остаются на тестовую фазу. Трасса: audit/relight-viewed-coding-trace-2026-10-16.json.

### Relight Apply — кодовый подпункт .7 закрыт без выполнения проверок

**23,420759% → 23,421875% (+0,001116 п.п.)**. Закрыты materialization выбранной version, физический source reader, API, review/editor, явный collection CAS, durable recovery и подтверждённое снятие конфликта; unit/DOM/SQL refusal сценарии написаны. Aggregate04.4.4:2/8. Успешные physical SQL/S3, upgrade, concurrency и quality проверки отложены на тестовую фазу; PASS не заявляется. Трасса: audit/relight-apply-coding-trace-2026-10-16.json.

# Relight: реальные pair-gate SQL сценарии — 2026-10-16

**23,420759% без прироста**. Написаны owner-seeded relational fixture и2 SQL tests для pair lineage/refusals и expiry после observed row-lock wait. Ничего не запускалось. Full successful adoption/decision и post-update rollback coverage остаются; подпункт04.4.4.6 открыт.

# Relight: legacy manifest defect исправлен — 2026-10-16

**23,420759% без прироста**. Найден text/object mismatch00483 → preview/selection. Написана02432 read-only projection и точные SQL patches; durable manifest bytes сохранены. Написаны15 SQL projection/privacy случаев без запуска. Full relight decision/lock-wait/rollback fixture всё ещё нужна.

# Relight: viewed profile/manifest исправления — 2026-10-16

**23,420759% без прироста**. SQL profile и manifest/upload lineage дополнены, обязательные JSON fields проверяются до casts; producer/run locks предшествуют artifact locks. Написаны4 service refusal и2 owner/search_path сценария. Ничего не запускалось. Открыты real SQL lock-wait/rollback/catalogue fixtures и final trace.

# Relight: forward-only viewed SQL — 2026-10-16

**23,420759% без прироста**. Написана02431 и ten-arg Go dispatch; pre-viewed bypass приватизирован, после UPDATE повторяется authority/pair/expiry с rollback exception. Написаны grant/refusal сценарии; ничего не запускалось.04.4.4.6 остаётся открыт до functional lock-wait/rollback/catalogue fixtures и profile/migration сверки.

# Relight: client viewed transport — 2026-10-16

**23,420759% без прироста**. Подключены exact displayed-pair tokens, acknowledgement перед pending mutation и viewed_token transport; already committed recovery не требует нового acknowledgement. Написаны reader/API и refusal DOM сценарии без запуска.04.4.4.6 открыт до SQL final-wait/catalogue gate и functional server fixtures.

# Relight: decision signature и locked pins — 2026-10-16

**23,420759% без прироста**. Добавлены обязательные Service/persist signature/source gates и derivative locks, private verified state не сериализуется. Написан binding unit сценарий; ничего не запускалось. SQL final-wait/catalogue boundary и клиент остаются открыты; до client wiring новые relight decisions fail closed.

# Relight: production viewed issuer — 2026-10-16

**23,420759% без прироста**. Подключены authenticated POST, live SQL snapshot и physical preview verification, response boundary сверяет requested pair/principal. Написан auth-refusal сценарий, ничего не запускалось.04.4.4.6 открыт: functional scenarios, decision gate и client wiring.

# Relight: подписанное подтверждение пары — 2026-10-16

**23,420759% без прироста**.04.4.4.6 implementing: написаны HMAC grant, strict pair/source/actor binding, issuer из authentic pending-preview tokens и unit scenarios. Остались production HTTP/catalogue, transactional decision gate и клиент. Ничего не запускалось; локально закрыто1/8.

# Relight review: восстановление на экране закрыто по кодированию — 2026-10-16

**23,419643 → 23,420759% (+0,001116 п.п.)**. Закрыт `P0.2.03.04.4.4.5`, вес1/1280; локально **1/8**. Manual current-source trace сохранён с SHA исходников. Исправлены потеря authored choice при первой storage failure и отсутствие повторного actor gate; cancellation ограждает effect scope. Написаны6 DOM route сценариев. Статус coding_closed_unverified: ничего не запускалось. Следом серверное подтверждение просмотра; Apply и остальные aggregate критерии остаются открыты.

# Relight review: journal подключён к экрану — 2026-10-16

**23,419643% без прироста**.04.4.4.5 implementing: scoped journal, сохранённый выбор, read-first recovery, matching receipt acknowledgement и unmount cancellation подключены к actual review route. Написаны2 DOM сценария, дополнительно4 store unit сценария. Ничего не запускалось. Ошибки storage, cancellation/stale scope scenarios и final source reconciliation остаются; локально закрыто0/8, вес не зачтён.

# Более мелкие части текущего пункта — 2026-10-16

Открытый `P0.2.03.04.4.4` заранее разбит на **8 результатов**: публикация review; preview и права; решение и завершение run; журнал решения; подключение восстановления к экрану; серверное подтверждение просмотра; Apply; итоговая совместимость и реестр. Критерии каждого результата записаны в JSON-реестре.

Каждая часть сохраняет **1/8 веса родителя**: `1/1280` единицы базового плана, или **+0,001116 п.п.** общего прогресса. Сумма частей равна исходному `1/160`; двойного учёта нет. Уже написанные изменения будут зачтены только после сверки с критерием соответствующей части. Сейчас **0/8**, общий процент **23,419643%**, без прироста за декомпозицию.

Для текущей незакрытой части дополнительно сообщается конкретно выполненная работа и оставшийся разрыв. Написание тестов учитывается в критерии кодирования; прохождение тестов и готовность продукта сообщаются отдельно.

# Relight review: immutable recovery helper — 2026-10-16

**23,419643% без прироста**. Написан actor-bound decision intent/read-first recovery, потерянный ответ сверяется через source-bound Detail без повторной mutation. OwnerAPI поддерживает optional key/signal. Написаны7 unit сценариев; ничего не запускалось. Journal/UI wiring, server viewed receipt, Apply и functional fixtures остаются открыты;04.4.4 не закрыт.

# Relight review: actual display gate — 2026-10-16

**23,419643% без прироста**. Relight decisions требуют Image.onLoad обеих verified картинок; error/pair change снимает готовность, submit handler также проверяет gate. Написаны3 DOM сценария. Ничего не запускалось. Server viewed receipt, uncertain recovery, Apply и functional fixtures открыты;04.4.4 не закрыт.

# Relight review: before-media recovery и finalization — 2026-10-16

**23,419643% без прироста**.02429 подключает pinned before-media jobs/recovery,02430 завершает выбранный relight run с event/outbox. Production scheduler вызывает обе операции, startup требует функции. Написаны10 Go combination/error и4 SQL privilege сценария. Ничего не запускалось. Viewed evidence/recovery/Apply, функциональные fixtures и aggregate trace остаются открыты;04.4.4 не закрыт.

# Relight selection: post-decision live authority — 2026-10-16

**23,419643% без прироста**.02428 закрывает обход current source/producer/qualification/session authority при decided selection; старое чтение приватно, selected-media повторяет проверку до/после IO. Написаны5 SQL grant/absence сценариев. Ничего не запускалось. Before-media producer/recovery, finalization, viewed evidence/recovery/Apply и functional fixtures остаются открыты;04.4.4 не закрыт.

# Relight review: HTTP/client preview selection — 2026-10-16

**23,419643% без прироста**. Подключены candidate/selected preview dispatch, dedicated selection SQL и strict client review binding. Relight сохраняет v26 derivative receipts, другие restoration schema не менялись. Исправлен no-effect result: новый pixels ID с прежним SHA допустим только relight. Дополнены Go/TS сценарии, ничего не запускалось. Post-decision live authority/viewed evidence/recovery/Apply и функциональные fixtures открыты;04.4.4 не закрыт.

# Relight review: dedicated decision authority — 2026-10-16

**23,419643% без прироста**.02427 добавляет dedicated live decision gate с source/producer/qualification/preview и final clock-based session expiry; owner UPDATE guard сохраняет immutable Params. Go persistence использует отдельный relight SQL и source-compatible WorkingImage pair. Написаны1 Go routing и4 SQL grants сценария. Ничего не запускалось. HTTP preview/selection/client/Apply и функциональные сценарии остаются открыты;04.4.4 не закрыт.

# Relight review: preview/selection SQL — 2026-10-16

**23,419643% без прироста**. Review хранит immutable bounded Params;02425 добавляет before/after preview gates и derivative source/dispatcher с сохранением open-eyes;02426 читает selected decided result. Написаны4 grant сценария без запуска. Dedicated decision пока запрещён; HTTP/client/Apply и functional сценарии остаются открыты.04.4.4 не закрыт.

# Relight review: atomic publication migration — 2026-10-16

**23,419643% без прироста**. Написана02424: private source-bound review publication внутри dedicated adoption transaction, publication failure rollback, уникальность task review и waiting_review/event/outbox. Generic decision UPDATE временно запрещён до dedicated gate. Написаны5 SQL metadata/grant сценариев. Миграция и тесты не запускались.04.4.4 остаётся открыт: preview/decision/Apply/client и functional scenarios.

# Relight aggregate: найден review/Apply разрыв — 2026-10-16

**23,419643% без прироста**.04.4.4 обнаружил отсутствие dedicated relight review publication и подключения image.relight к preview/decision/selected-result UI/server. Сохранён P0 план устранения в audit/relight-review-apply-gap-2026-10-16.md; aggregate закрывать нельзя. Worker/adoption04.4.3 остаётся отдельно закрытой source трассой. Тесты не запускались.

# Relight worker/scheduler/output: coding closed — 2026-10-16

**23,410714 → 23,419643% (+0,008929 п.п.)**. Закрыт04.4.3 с фиксированным весом1/160; итоговая трасса04.4 **3/4**. Сведены actual worker registration, alpha/minmax_valid, live runtime/cancellation, independent replay, current SQL evidence commit и атомарная публикация closure/event/outbox. Написаны boundary и build_worker сценарии; ничего не запускалось. Далее04.4.4 — итоговая сверка relight; runtime/quality gates остаются отдельными.

# Relight analysis: actual worker composition scenarios — 2026-10-16

**23,410714% без прироста**. Написаны6 сценариев production build_worker: person/depth × passed MPS, unsupported MPS, missing model. Используются реальные operation factories и registry executor; модель и runtime verifier изолированы. Проверяется сохранение pair.fit.color, omission unsupported operation до runtime builder и отказ missing configured model. Ничего не запускалось.04.4.3 остаётся открытым до окончательной migration/output трассы.

# Relight analysis: unsupported Metal registration — 2026-10-16

**23,410714% без прироста**. Исправлена регистрация person/depth при явно unsupported MPS evidence: операции исключаются до configured runtime construction. Существующий отказ missing installed model сохранён; untested/missing evidence не скрывается как unsupported. Написаны12 policy сценариев без запуска. Далее actual build_worker registration сценарии и окончательная migration/output трасса.04.4.3 остаётся открытым.

# Relight scheduler: cancellation/runtime boundary — 2026-10-16

**23,410714% без прироста**. Прослежены reconstruction, exact pixel replay, повтор evidence и dedicated SQL commit с tenant/task блокировками; generic adoption исключает relight bypass. Добавлены проверки отмены до/после начального измерения installed runtime, написаны2 сценария.04.4.3 остаётся открытым до backend registration и current migration/output inventory. Ничего не запускалось.

# Depth worker: live runtime до публикации — 2026-10-16

**23,410714% без прироста**. Исправлена единственная initial runtime проверка depth: authorize/check_active повторяются до/после inference, до NPY/manifest staging и перед выдачей OperationResult. Написаны3 execute сценария смены runtime и cancellation. Person аналогичные boundaries уже содержит.04.4.3 открыт: backend availability и scheduler/adoption/output trace остаются; ничего не запускалось.

# Relight worker: alpha/depth исправления — 2026-10-16

**23,410714% без прироста**. Начат04.4.3: depth worker использует strict reader для uint16/float32 alpha; minmax_valid теперь исключает полностью прозрачные пиксели из нормализации. Существующие no-alpha вызовы сохранены; fully invalid depth zero/flat и не становится доступной глубиной для relight. Написаны8 сценариев, ничего не запускалось. Далее authority/backend и scheduler/adoption/output continuations.

# Relight: server admission/replay coding closed — 2026-10-16

**23,401786 → 23,410714% (+0,008929 п.п.)**. Закрыт04.4.1 с исходным весом1/160; итоговая трасса04.4 **2/4**. Сведены current source и authored scenarios: stored pin/plan, receipt-only startup, strict outcomes/live authority, HTTP/client replay-only и02423 queue fix. Статус coding_closed_unverified: ничего не запускалось. Далее04.4.3 — worker/scheduler/output continuations;04.4.4 aggregate trace открыт.

# Relight: найден и исправлен V32 queue/config разрыв — 2026-10-16

**23,401786% без прироста**. Итоговая сверка выявила несовместимость01649 queue_class с exact config comparison01013/02419. Написана02423: исключается только внешняя interactive метка; foreign queue и unknown authored fields отклоняются. Startup проверяет наличие исправления. Написаны13 metadata/privacy cases.04.4.1 открыт до повторного final trace после исправления; ничего не запускалось.

# Relight: authenticated HTTP и qualified legacy wait — 2026-10-16

**23,401786% без прироста**. Написаны13 HTTP cases с real session middleware и explicit submitter stub; оба маршрута передают replay/key/identity, отказывают ambiguous headers/absent/denied/revoked. Добавлен qualified legacy observed-lock expiry сценарий: deadline наступает после начала reader transaction; receipt должен быть отклонён после освобождения tenant lock.04.4.1 открыт до final source trace и migration-head compatibility сверки; ничего не запускалось.

# Relight: bounded startup и SQL startup scenarios — 2026-10-16

**23,401786% без прироста**. Receipt-only startup ограничен15s с сохранением shorter deadline и early cancellation. Написаны сценарии отсутствия config/DSN, отказа invalid configured file и real PostgreSQL startup обоих routes без object publisher. Full SQL/S3 fixture подключает startup scenario. Остались qualified legacy lock-wait expiry, authenticated HTTP replay и final trace.04.4.1 открыт; ничего не запускалось.

# Analysis: real SQL/S3 admission recovery fixture — 2026-10-16

**23,401786% без прироста**. Написана полная integration fixture для depth.predict/mask.person: physical NPY/WorkingImage, synthetic imported qualification, production SubmitIntent, removed/replaced provider recovery, changed intent/absent key/revoked session refusal и stable task/event/outbox/artifact inventory. Это authored SQL/S3 scenario, не выполненное evidence и не GPU/quality qualification. Далее legacy wait expiry, startup/HTTP scenarios и final source trace.04.4.1 открыт; ничего не запускалось.

# Analysis: production-router adapter scenarios — 2026-10-16

**23,401786% без прироста**. Написаны7 сценариев actual production router с compiled receipt fixture и явным SQL stub: removed/replaced service, absent/denied/conflict, malformed receipt и commit failure. Проверяются exact intent, один stored read, отсутствие publication/admission queries и отказ выдавать receipt до commit. SQL qualification этим не доказана: полная admitted analysis PostgreSQL/object fixture ещё нужна.04.4.1 открыт; ничего не запускалось.

# Relight: legacy authority и qualified provider replay — 2026-10-16

**23,401786% без прироста**.02422 сохраняет исходные проверки legacy receipt и повторяет live authority/session expiry по clock_timestamp перед возвратом. В existing qualified keyed fixture написаны removed/replaced provider recovery и changed-request refusal сценарии с no-object-IO и single-event проверками; upgrade fixture дополнен02422. Далее qualified analysis replay/startup, legacy post-wait expiry и итоговая трасса.04.4.1 открыт; ничего не запускалось.

# Relight: receipt-only startup — 2026-10-16

**23,401786% без прироста**. При отсутствии deployment file и наличии scheduler DSN startup устанавливает receipt-only routers для обоих направлений. Publication services отсутствуют; проверяются scheduler role и EXECUTE entry point; invalid configured file остаётся fatal. Написаны constructor/dispatch/cancellation сценарии. Далее qualified replay/startup fixtures, legacy authority и final route trace.04.4.1 открыт; ничего не запускалось.

# Relight analysis: сохранённый план — 2026-10-16

**23,401786% без прироста**. Добавлены02421, stored-plan receipt reader и восстановление до поиска текущего analysis service. Написаны ещё13 metadata/grant/lock сценариев. Остались receipt-only startup при полностью отсутствующей deployment конфигурации, qualified replay fixtures, legacy authority и финальная трасса.04.4.1 открыт; ничего не запускалось.

# Relight: восстановление с сохранённым pin — 2026-10-16

**23,401786% без прироста**. Для keyed relight добавлены02420 и production чтение receipt с сохранённым серверным pin до поиска текущего project service. Текущие authority/source/qualification/closure проверки сохранены; replay-only не переходит к admission. Написаны ещё13 metadata/grant/lock сценариев. Analysis stored-plan recovery и qualified fixture трассы остаются открытыми; ничего не запускалось.

# Relight: transport и дефект замены provider — 2026-10-16

**23,401786% без прироста**. Написаны6 OwnerAPI сценариев normal/replay-only и1 CORS сценарий. Подтверждён по исходникам разрыв: отсутствие project service останавливает запрос до receipt read; текущий deployment pin также привязывает восстановление к новой конфигурации. Далее — независимое чтение сохранённого intent/pin с сохранением текущих authority/qualification checks.04.4.1 открыт; ничего не запускалось.

# Relight: клиентское uncertain восстановление — 2026-10-16

**23,401786% без прироста**. Relight/analysis UI позволяет восстановить сохранённую uncertain попытку без worker capability через replay-only, сохраняя body/key и hard gates. Возвращение capability во время journal IO не разрешает новый admission; отсутствие receipt сохраняет журнал. Написаны/обновлены6 сценариев.04.4.1 остаётся открытым: qualified fixtures, legacy unbound reader, missing provider и transport/final trace. Ничего не запускалось.

# Relight: серверный replay-only — 2026-10-16

**23,401786% без прироста**. Добавлен строгий HTTP режим восстановления без нового admission для relight/analysis, guards в обоих coordinator, CORS и опция OwnerAPI. Написаны4 сценария orchestration и8 header cases. UI пока не подключён; missing provider и qualified replay остаются открытыми. Ничего не запускалось.

# Relight: доступ после ожидания блокировки — 2026-10-16

**23,401786% без прироста**. Написаны8 сценариев: relight/analysis × истечение сессии, отзыв сессии, понижение роли, удаление tenant. Сценарии сначала наблюдают фактическую блокировку reader через PostgreSQL, затем меняют authority; expiry наступает после начала транзакции, поэтому проверяется отличие текущего времени от transaction now(). Сценарии не запускались.04.4.1 остаётся открытым: qualified replay, legacy unbound reader, missing provider и клиентское восстановление без capability.

# Relight: строгие исходы восстановления — 2026-10-16

**23,401786% без прироста**, итоговая трасса **1/4**. Добавлены отдельные исходы отсутствия, отказа, изменения запроса и найденного подтверждения; keyed/analysis чтение использует их без подмены отказа отсутствием. Срок сессии перепроверяется по текущему времени после ожидания. Написаны сценарии, ничего не запускалось.04.4.1 остаётся открытым до завершения qualified replay, lock-wait и восстановления при недоступном provider/capability.

# Relight server receipt-first — в работе, 2026-10-16

**23,401786% без прироста**, итоговая трасса **1/4**. Production SubmitKeyed сначала читает авторизованный receipt, затем при отсутствии переходит к draft/submit. Ошибки стадий вызывают один recovery read без повторной мутации; request/pin binding заморожен один раз. Написаны9 сценариев. Ничего не запускалось.04.4.1 ещё открыт: требуется разобраться в SQL NULL/absence/authority, analysis/provider paths и uncertain recovery без текущего worker capability.

# Прогресс кодирования — relight received receipt persistence, 2026-10-16

**23,392857 → 23,401786% (+0,008929 п.п.)**. До работы04.4 разделён на4 равные части с сохранением веса; закрыт04.4.2, итоговая трасса **1/4**. Полученный валидный ticket кешируется до записи журнала. Повтор после storage failure дописывает подтверждение без повторного admission, в том числе после потери worker capability. Написаны8 pure attempt и2 actual-controls сценария. Ничего не запускалось. Серверное восстановление после restart, worker и итоговая трасса остаются открыты.

# Прогресс кодирования — relight parent preparation, 2026-10-16

**23,357143 → 23,392857% (+0,035714 п.п.)**. Закрыт04.3, relight **3/4**. Latest hard gate проверяется до snapshot; прерванный snapshot повторяется с исходными binding/key, смена source evidence отклоняется. Hard gate change aborts old boot/prepare; epoch/controller ownership защищают подготовленный ответ и busy lock нового запроса. Написаны5 сценариев; ничего не запускалось. Далее04.4 — server admission/replay/worker и итоговая трасса.

# Прогресс кодирования — relight controls, 2026-10-16

**23,321429 → 23,357143% (+0,035714 п.п.)**. Закрыт04.2, relight **2/4**. Relight submit проверяет latest capability после journal IO; original request/key сохраняются. Локально принятый ticket открывается без worker; hard gate проверяется после verify. Написаны3 сценария controls. Ничего не запускалось. Parent bootstrap и server replay/final trace остаются04.3/04.4.

# Прогресс кодирования — relight analysis, 2026-10-16

**23,285714 → 23,321429% (+0,035714 п.п.)**. Закрыт04.1, relight **1/4**. Анализ глубины/person mask имеет независимый gate; latest capability после journal IO, локально принятый ticket открывается без worker после server verify. Написаны4 сценария анализа и отдельный parent scenario независимости. Ничего не запускалось. Parent bootstrap и server replay/final trace остаются04.3/04.4.

# Прогресс кодирования — generation final source trace, 2026-10-16

**23,276786 → 23,285714% (+0,008929 п.п.)**. Закрыт03.4.4, итоговая трасса **4/4**, generation03 **4/4**, P0.2.03 **3/10**. Atomic same-key abandonment сохраняет уже принятый run либо фиксирует долгоживущий tombstone без новой работы; editor очищает uncertain journal только по подтверждённому matching-key ответу. Написаны codec/store/API/route и server authority/refusal сценарии. Сведены38 текущих исходников/контрактов, включая02417/02418 и finalizer/output/worker связи. Ничего не запускалось; runtime/GPU/quality gates сохранены. Далее04 — nested relight.

# Generation final trace — серверный дефект, 2026-10-16

**23,276786% без прироста**;03.4 остаётся **3/4**. Обнаружено ожидание невозможного preview от failed/cancelled/skipped generation task. Написана forward-only02418: preview требуется успешным вариантам, частичный успех с verified canonical derivative возвращает completed_with_errors. Написаны7 SQL сценариев; ничего не запускалось. Итоговая трасса ещё открыта: требуется завершить inventory и восстановление uncertain receipt-less попытки после окончательного admission refusal без предположения об отсутствии receipt.

# Прогресс кодирования — generation client terminal contract, 2026-10-16

**23,267857 → 23,276786% (+0,008929 п.п.)**. Закрыт03.4.3, итоговая трасса **3/4**. Добавлены явное подтверждение failed/cancelled с повторным authoritative read, сохранение удачных вариантов completed_with_errors и повторная проверка пустого результата перед очисткой. Malformed receipt больше не кешируется вопреки durable codec. Контракт replay-only закреплён в JSON/15. Написаны22 сценария classifier/route/receipt; ничего не запускалось. Далее03.4.4 — итоговая сверка исходников и связей.

# Прогресс кодирования — generation disposable fixtures, 2026-10-16

**23,258929 → 23,267857% (+0,008929 п.п.)**. Закрыт03.4.2, итоговая трасса **2/4**. Написаны собственные минимальные SQL fixtures, Go success markers и Python runner: mutable admission и шесть потерь доступа после подтверждённой receipt-блокировки. Проверяется отсутствие pending receipt/tasks при replay-only отказе. Это сценарии SQL/Go, не GPU/quality qualification; ничего не запускалось. Далее03.4.3 — клиентский контракт и окончательные состояния.

# Прогресс кодирования — generation live authority, 2026-10-16

**23,250000 → 23,258929% (+0,008929 п.п.)**. Закрыт03.4.1, итоговая трасса **1/4**. Сервер повторно проверяет сессию, tenant и source/project после ожидания receipt lock; при отказе тело ответа не выдаётся. Написана forward-only02417 для текущего срока сессии и шесть сценариев реальной SQL-блокировки. Миграция и сценарии не запускались. Далее03.4.2 — собственные disposable fixtures/runner.

# Уточнение шага прогресса — 2026-10-16

Текущий процент сохраняется: **23,250000%**, прирост **0**. Открытая итоговая трасса generation03.4 заранее разделена на четыре равные части: серверная проверка доступа после ожидания; собственные изолированные данные серверных сценариев; клиентский контракт восстановления; итоговая сверка связей. Сейчас закрыто **0/4**. Каждая часть имеет вес **1/160** единицы исходного плана и даёт **+0,008929 п.п.** после закрытия. Родитель повторно не учитывается. Кодирование и написанные сценарии учитываются отдельно от их будущего запуска; тесты не запускались.

# Незакрытый участок — generation replay-only, 2026-10-16

**23,250000%, прирост0.**03.4 остаётся открытым. Добавлен end-to-end header Generation-Replay-Only: unavailable recovery читает прежний receipt, отсутствующая операция не допускается к созданию. CORS разрешает header только обычным configured origins. Написаны API/actual editor/preflight сценарии и расширен Go SQL suite. Own disposable fixtures, after-wait authority и контрактная запись ещё нужны. Ничего не запускалось.

# Незакрытый участок — generation server replay, 2026-10-16

**23,250000%, прирост0.**03.4 остаётся открытым. Исправлена серверная проверка mutable draft/runtime до idempotency replay: теперь она выполняется только для новой операции, source authorization остаётся перед replay. Написан Go сценарий для2 isolated fixtures × replay/changed body/new-key refusal. Собственный disposable fixture runner и client uncertain/unavailable reconciliation ещё нужны. Ничего не запускалось.

# Прогресс кодирования — generation foreground recovery, 2026-10-16

**23,241071 → 23,250000% (+0,008929 п.п.)**.03.3.4 coding_closed_unverified; recovery03.3 **4/4**, generation03 **3/4**. Добавлены background abort/detach, sequence guard, foreground journal reboot и abortable run/output reads; принятый cleanup сохраняется. Написаны4 сценария. Ничего не запускалось. Далее03.4.

# Прогресс кодирования — generation editor boot, 2026-10-16

**23,232143 → 23,241071% (+0,008929 п.п.)**.03.3.3 coding_closed_unverified; recovery03.3 **3/4**. Реальный editor загружает и attach-ит scoped journal, поддерживает accepted output retry без generation capability, transport проверяет latest gate после journal IO. Написаны2 actual-route сценария. Foreground/scope остаются03.3.4. Ничего не запускалось.

# Прогресс кодирования — generation durable store, 2026-10-16

**23,223214 → 23,232143% (+0,008929 п.п.)**.03.3.2 coding_closed_unverified; recovery03.3 **2/4**. Добавлены scoped journal, write-before-transport, receipt persistence, immutable conflicts, ack-before-delete и logout fencing. Написаны7 сценариев. Editor boot/attach/post-journal gate/foreground остаются открыты. Ничего не запускалось.

# Прогресс кодирования — generation restore codec, 2026-10-16

**23,214286 → 23,223214% (+0,008929 п.п.)**.03.3.1 coding_closed_unverified; recovery03.3 **1/4**. До изменений зафиксированы4 равные части внутри исходного веса. Добавлены versioned owned record/strict restore,11 сценариев uncertain/accepted/corrupt recovery. Durable store/boot/logout/foreground остаются открыты. Ничего не запускалось.

# Прогресс кодирования — generation immutable retry, 2026-10-16

**23,178571 → 23,214286% (+0,035714 п.п.)**. Generation03.2 coding_closed_unverified; **2/4**. EditorGenerationAttempt сохраняет исходные draft/mask/protected refs/prompt/seed/key, retry не пересохраняет draft; принятый run кешируется для повторного чтения output. OwnerAPI принимает explicit key/signal. Написаны5 сценариев. Durable restart/foreground/UI остаются03.3. Ничего не запускалось.

# Прогресс кодирования — generation admission fence, 2026-10-16

**23,142857 → 23,178571% (+0,035714 п.п.)**. P0.2.03.03.1 coding_closed_unverified; generation03 **1/4**, веса зафиксированы до изменений. Выбранные mask/prompt/seed фиксируются до save; latest capability и original route проверяются вокруг transport. Добавлены4 сценария. Random retry key и accepted-run recovery остаются03.2/03.3. Ничего не запускалось.

# Прогресс кодирования — editor masks admission, 2026-10-16

**23,000000 → 23,142857% (+0,142857 п.п.)**. P0.2.03.02 coding_closed_unverified; пункт **2/10**. Исправлен original route epoch после mask transport; latest capability проверяется перед отправкой, принятый результат не теряется только из-за смены availability. Написаны8 сценариев для prompt/manual mask. Ничего не запускалось. Далее03.

# Прогресс кодирования — nested editor inventory, 2026-10-16

**22,857143 → 23,000000% (+0,142857 п.п.)**. P0.2.03.01 coding_closed_unverified; пункт **1/10**. Составлен индекс всех вложенных editor компонентов/controls/actions и отдельного multi-frame маршрута. Подтверждён NESTED-DELIVERY-01: нет latest disabled проверки после binding IO перед snapshot; исправление закреплено09. Это закрытие inventory, остальные admission критерии открыты. Ничего не запускалось. Далее02.

# Прогресс кодирования — semantic module evidence closure, 2026-10-16

**22,821429 → 22,857143% (+0,035714 п.п.)**.10.4 coding_closed_unverified; итоговая трасса10 **4/4**, P0.1 **10/10**. Сведены10 критериев,222 существующие ссылки и текущие SHA; historical PASS не использованы. Runtime/role/migration/photo/visual gates явно остаются следующей фазе. Обновлён устаревший checklist. ДалееP0.2.03.

# Прогресс кодирования — client routes/states trace, 2026-10-16

**22,785714 → 22,821429% (+0,035714 п.п.)**.10.3 coding_closed_unverified; итоговая трасса10 **3/4**. Составлен inventory editor/run/review/accepted-result маршрутов, компонентов и состояний. Исправлены background target reads и потеря неопределённой non-journal попытки при исчезновении proposals после refresh. Написаны3 сценария. Ничего не запускалось. Далее10.4.

# Прогресс кодирования — SQL/worker trace, 2026-10-16

**22,750000 → 22,785714% (+0,035714 п.п.)**.10.2 coding_closed_unverified; итоговая трасса10 **2/4**. Сверены последние Up-определения SQL, установленный scheduler wrapper, canonical/adoption/terminal/quota/target связи. Написаны12 service сценариев:4 shown lanes × permanent/stale/transport failure, exact result rejection и lock release. Ничего не запускалось. Далее10.3.

# Прогресс кодирования — API/bootstrap trace, 2026-10-16

**22,714286 → 22,750000% (+0,035714 п.п.)**.10.1 coding_closed_unverified; итоговая трасса10 **1/4**. До работы зафиксированы4 равные части. Сверены production handler/provider связи; выбор Close-провайдера выделен в функцию, используемую main. Написаны3 сценария shared configured service/replay fallback/invalid fallback. Ничего не запускалось. Далее10.2.

# Прогресс кодирования — recovery final trace, 2026-10-16

**22,678571 → 22,714286% (+0,035714 п.п.)**.09.4 coding_closed_unverified; recovery09 **4/4**, P0.1.10 **9/10**. Исправлена потеря уведомления экрана после background во время journal cleanup: acknowledged attempt сохраняется до foreground completion. Написаны2 связанных сценария, включая screen persisted approval → failed cleanup → accepted result navigation. Сверена текущая цепочка journal/API/server/materialization/route. Ничего не запускалось. Далее10 — полная модульная трасса.

# Прогресс кодирования — accepted-version route, 2026-10-16

**22,642857 → 22,678571% (+0,035714 п.п.)**.09.3 coding_closed_unverified; recovery09 **3/4**. Сверены materialize POST → hash/deterministic version/editor authority → screen callback → editor params. Написаны5 новых сценариев: real route/screen, initial background, foreground retry/late result, tenant/client replacement. Ничего не запускалось. Далее09.4.

# Прогресс кодирования — durable recovery, 2026-10-16

**22,607143 → 22,642857% (+0,035714 п.п.)**.09.2 coding_closed_unverified; recovery09 **2/4**. Исправлен повтор после сбоя очистки подтверждённого журнала: cached receipt → clear без повторных write/Apply. Написаны8 сценариев, включая настоящий store с marker/delete partial failure и6 консервативных HTTP refusal retries. Без серверного доказательства отсутствия операции durable попытка сохраняется. Ничего не запускалось. Далее09.3.

# Прогресс кодирования — definitive non-journal refusal, 2026-10-16

**22,571429 → 22,607143% (+0,035714 п.п.)**.09.1 coding_closed_unverified; recovery09 **1/4**. До начала зафиксированы4 равные части. Написаны6 refusal/fresh-authority/new-key сценариев non-journal решений. Durable409 не считается доказанным отсутствием мутации и относится09.2. Ничего не запускалось. Далее09.2.

# Прогресс кодирования — client installation/close foreground, 2026-10-16

**22,535714 → 22,571429% (+0,035714 п.п.)**.08.4 coding_closed_unverified; client08 **4/4**. Сверены route authenticated journal scope и decision/close coordination.4 own close foreground сценария покрывают hung/late response, exact uncertain retry, initial background и completed-on-return. Ничего не запускалось. P0.1.10 **8/10**, далее09.

# Незакрытый участок — independent close foreground, 2026-10-16

Close control получил собственный AppState guard: background abort/detach/status reset, disabled controls, preserved uncertain attempt; foreground fresh authority. Написаны2 own hung-read/late-success/exact-retry сценария. Начальный background и route/journal installation audit ещё открыты. Ничего не запускалось. **22,535714%, прирост0; client08 3/4.**

# Прогресс кодирования — journal/close coordination, 2026-10-16

**22,500000 → 22,535714% (+0,035714 п.п.)**.08.3 coding_closed_unverified; client08 **3/4**.5 own control/screen сценариев для boot/failure/uncertain replay/clear и blocked close immutable retry. Pending foreground/journal/choices связан с close, confirmed submission снимает блокировку после journal clear. Ничего не запускалось. Далее08.4.

# Незакрытый участок — journal/close coordination, 2026-10-16

Добавлена связь pending journal/decision → screen → blocked close control. Close transport блокируется при journal boot/failure, busy/uncertain Apply и unsent choices; uncertain close key/body сохраняются. Написаны2 control-level blocked/unblocked replay сценария. Screen-level schedules остаются открыты. Ничего не запускалось. **22,500000%, прирост0; client08 2/4.**

# Прогресс кодирования — foreground/scope authority, 2026-10-16

**22,464286 → 22,500000% (+0,035714 п.п.)**.08.2 coding_closed_unverified; client08 **2/4**. Завершена сверка background reset/fresh foreground authority, remount scope и late response suppression. Написаны6 новых вариантов:3 authority refusals, hung old read, tenant/intent replacement. Durable uncertain journal сохраняется. Ничего не запускалось. Далее08.3.

# Незакрытый участок — foreground authority, 2026-10-16

Исправлены stale review fence после background и удержание aborted flight: background очищает status, отсоединяет отменённый flight, foreground перечитывает authority. Написаны3 access-loss401/403/404 сценария, прежние background assertions согласованы с отсутствующими controls. Durable uncertain attempt сохраняется. Scope/late response audit ещё открыт. Ничего не запускалось. **22,464286%, прирост0; client08 1/4.**

# Прогресс кодирования — отказ без preview, 2026-10-16

**22,428571 → 22,464286% (+0,035714 п.п.)**.08.1 coding_closed_unverified; client08 **1/4**. До реализации зафиксированы4 равные части. Написаны4 own operation-kind сценария с отсутствующим preview endpoint и недоступным AI; approval disabled, reject-only request без preview refs. Source approval/reject separation сверена. Ничего не запускалось. Далее08.2.

# Прогресс кодирования — target projections, 2026-10-16

**22,392857 → 22,428571% (+0,035714 п.п.)**.07.4 coding_closed_unverified; targets07 **4/4**. Сверены authenticated reader/HTTP/client/UI. Исправлена верхняя int64 граница target revision без Number; написаны10 boundary и1 completed-parent/pending-child DOM сценарий. Ничего не запускалось. P0.1.10 **7/10**, далее08.

# Прогресс кодирования — parent/child independence, 2026-10-16

**22,357143 → 22,392857% (+0,035714 п.п.)**. P0.1.10.07.3 coding_closed_unverified; targets07 **3/4**. Написаны6 own before/after explicit close x completed/failed/cancelled child metadata сценариев: full parent preservation, target outcomes, one parent revision increment, exact authenticated replay, unique close events/outbox. Close04 queued child assertions повторно не зачтены. Ничего не запускалось. Далее `.07.4` — итоговая трасса/projections.

# Прогресс кодирования — конкурентный target settlement, 2026-10-16

**22,321429 → 22,357143% (+0,035714 п.п.)**. P0.1.10.07.2 coding_closed_unverified; targets07 **2/4**. Написаны9 own two-target metadata вариантов:6 наблюдаемых run lock commit/rollback schedules и3 scope/full caller rollback/retry. Exact revision increment, rejected/omitted preservation и replay assertions authored. Текущие lifecycle/run/sorted target locks сверены. Ничего не запускалось. Далее `.07.3` — родительское review.

# Прогресс кодирования — target lifecycle, 2026-10-16

**22,285714 → 22,321429% (+0,035714 п.п.)**. P0.1.10.07.1 coding_closed_unverified; targets07 **1/4**. До реализации зафиксированы4 равные части. Написаны11 own SQL state/binding/revision/refusal сценариев. Привилегированные terminal child metadata изолируют target guards; canonical completion не подменяется. Текущие00537/00538/00539 сверены, миграция не требуется. Ничего не запускалось. Далее `.07.2` — конкурентный settlement/rollback.

# Прогресс кодирования — четыре child operations, 2026-10-16

**22,250000 → 22,285714% (+0,035714 п.п.)**. P0.1.10.06.4 coding_closed_unverified; child06 **4/4**. Добавлен собственный exposure completion fixture от preview/решения через Apply/dispatch/claim до dedicated adopter; canonical output metadata синтетические. Матрица12 вариантов четырёх операций, current discovery/coordinator/recovery/finalizer/settlement связи сверены. Ничего не запускалось. P0.1.10 **6/10**; далее `.07` — targets/parent settlement.

# Незакрытый участок — четыре child operations, 2026-10-16

Сверены pending discovery, собственные14-argument adopters и установленный recovery wrapper. Написаны9 own operation/outcome metadata сценариев removal/cleanup/retouch с реальными собственными Apply fixtures. Exposure требует отдельного canonical shown-copy fixture; подмена operation не применяется. Исполнения не было. **22,250000%, прирост0; child06 3/4.**

# Прогресс кодирования — child quota/rollback, 2026-10-16

**22,214286 → 22,250000% (+0,035714 п.п.)**. P0.1.10.06.3 coding_closed_unverified; child06 **3/4**. Написаны15 own disposable metadata сценариев completed/failed/cancelled accounting, quota underflow/revision/cursor full23-table rollback и caller rollback/retry. Сверены reservation uniqueness, usage/quota guards и общая транзакция статуса/events/outbox/targets. Ничего не запускалось. Далее `.06.4` — итоговая трасса четырёх операций.

# Прогресс кодирования — child rejection/cancellation, 2026-10-16

**22,178571 → 22,214286% (+0,035714 п.п.)**. P0.1.10.06.2 coding_closed_unverified; child06 **2/4**.02416 закрывает исключённый shown Apply permanent rejection. Написаны13 isolated metadata вариантов, включая оба порядка наблюдаемой lifecycle блокировки, active-worker cancellation и upgrade OID/ACL/data preservation. Сверен failure precedence текущего finalizer. Физическое выполнение и API cancellation qualification не заявляются. Ничего не запускалось. Далее `.06.3` — резервы/rollback.

# Незакрытый участок — показанный Apply adoption rejection, 2026-10-16

Найден фактический разрыв: worker вызывает reject_v2_adoption, но установленная функция исключала kind=apply. Добавлена forward-only02416 с сохранением V2/capture и строгим shown snapshot/target/canonical binding. Написаны девять disposable metadata сценариев отказа, failure precedence и user cancel. Конкурентные schedules и upgrade audit остаются открыты. Ничего не запускалось. **22,178571%, прирост 0; child06 1/4.**

# Прогресс кодирования — canonical child completion, 2026-10-16

**22,142857 → 22,178571% (+0,035714 п.п.)**. P0.1.10.06.1 coding_closed_unverified; child06 **1/4**. Сверены canonical result attempt/fence/inbox, общий finalizer четырёх shown operations и установленный recovery wrapper. Написаны шесть disposable canonical/scheduler и шесть caller lock lifetime сценариев. Реальные dispatch/claim/adoption используют синтетические output metadata; физическое исполнение не заявляется. Ничего не запускалось. Далее `.06.2` — отмена и приоритет ошибки.

# Незакрытый участок — child canonical completion, 2026-10-16

P0.1.10.06 разбит до зачёта на четыре равные части: canonical adoption, отмена/ошибка, резервы/rollback, итоговая трасса. Написаны шесть собственных disposable SQL сценариев для canonical attempt/fence/result/inbox и установленного scheduler wrapper; output metadata синтетические. Проверка времени удержания блокировок ещё открыта. Ничего не запускалось. **22,142857%, прирост 0; child06 0/4.**

# Прогресс кодирования — итоговая close replay матрица, 2026-10-16

**22,107143 → 22,142857% (+0,035714 п.п.)**. P0.1.10.05.4 coding_closed_unverified; close05 **4/4**. Сведены текущие service/HTTP/SQL пути, same-key post-wait authorization, competing/hash-conflict, rollback handoff и late cancellation. Зафиксированы границы disposable Go harness и внешнего HTTP fixture. Сценарии close04 повторно не зачтены. Evidence: audit/semantic-close-replay-matrix-2026-10-16.json. Ничего не запускалось. Далее `.06` — завершение дочерних задач.

# Прогресс кодирования — close rollback/retry, 2026-10-16

**22,071429 → 22,107143% (+0,035714 п.п.)**. P0.1.10.05.3 coding_closed_unverified; close05 **3/4**. Написаны новые real Go rollback handoff schedules для same/competing key: после rollback первого Use ожидающая попытка делает fresh mutation. Добавлен SQL late cancellation на наблюдаемом final event cursor blocker: восстановление baseline всех23 таблиц, absent receipt, same-key retry и единственные batch/event/outbox/receipt. Сверены Service final commit/deferred rollback и SQL порядок поздней записи. Ранее учтённые локальные cursor exhaustion/caller rollback сценарии повторно не кредитуются. Исполнения не было. Далее `.05.4` — consolidated matrix/trace.

# Прогресс кодирования — close competing keys, 2026-10-16

**22,035714 → 22,071429% (+0,035714 п.п.)**. P0.1.10.05.2 coding_closed_unverified; close05 **2/4**. Расширены disposable real Go Use/SQL schedules: competing key ждёт lifecycle/parent и получает stale40001; same-key changed fingerprint ждёт receipt и получает idempotency conflict без повторного callback. Написаны проверки единственного parent increment, batch/event/outbox/receipt и отсутствия проигравшего pending receipt. Сверены текущие unique key/hash и source parent FOR UPDATE fences после ожидания. Исполнения не было. Следующий `.05.3` — поздний отказ/rollback/retry.

# Прогресс кодирования — close same-key wait, 2026-10-16

**22,000000 → 22,035714% (+0,035714 п.п.)**. P0.1.10.05.1 coding_closed_unverified; close05 **1/4**. Исправлен bypass authenticated replay после конкурентного Use wait. Написаны own16 unit вариантов и два disposable real Go Use/SQL/shared validator schedules (успех и revoked session после wait), с наблюдаемой pg_blocking_pids блокировкой и единственными batch/receipt/event/outbox. Harness не принимает skip за PASS. Физический/HTTP end-to-end проход не заявляется; существующий внешний HTTP race fixture просмотрен отдельно. Ничего не запускалось. Следующий `.05.2` — competing keys.

# Незакрытый участок — concurrent close replay authority, 2026-10-16

P0.1.10.05 разбит до реализации на четыре равных результата (same-key, competing keys, late rollback, consolidated matrix), каждый сохраняет 1/40 веса единицы. Найден обход authenticated ReadCloseReplay при in-flight same-key возврате из idempotency.Use: CommitClose теперь повторяет SQL replay после ожидания и проверяет exact response/resource/parent/workflow. Написаны own success/ownership и 15 refusal/cancellation вариантов. Existing external HTTP race fixture терминальный и непригоден для повторного запуска двух schedules на одних данных: оставлен без такого изменения. Isolated real transaction schedule ещё требуется. Ничего не запускалось. **22,000000%, прирост 0; close05 0/4.**

# Прогресс кодирования — close service/routes/client, 2026-10-16

**21,964286 → 22,000000% (+0,035714 п.п.)**. P0.1.10.04.4 coding_closed_unverified; close04 **4/4**, P0.1.10 **4/10**. Согласованы safe request/status/terminal revision и миграция02415; сервис проверяет cancellation и принадлежность replay parent/workflow. Написаны client boundary/ownership, UI tenant/intent/newer-status exact retry, service cancellation и HTTP response/error сценарии. Сверены main/tenant installation, fallback replay, HTTP auth/idempotency/DTO/error gates и remount/retry/abort UI связи. Существующие HTTP race сценарии просмотрены, не запускались. Следующий `.05` отдельно требует полной concurrency/rollback/replay сверки; уже учтённые локальные сценарии повторно не повышают процент.

# Незакрытый участок — close client/status boundary, 2026-10-16

Клиент close/status ещё разрешал int64 версии вне JS-safe контракта, а SQL status включал can_close на неинкрементируемой safe maximum. Исправлены client request/status/terminal bounds, Go terminal revision=1 refusal; добавлена forward-only02415 advisory status. Написаны восемь client boundary/ownership вариантов, Go terminal boundaries и три SQL status/upgrade варианта. Сверены HTTP close route и tenant installation entrypoints; финальная service/HTTP/UI retry сверка остаётся открытой. Ничего не запускалось. **21,964286%, прирост 0; close04 3/4.**

# Прогресс кодирования — atomic close SQL/replay, 2026-10-16

**21,928571 → 21,964286% (+0,035714 п.п.)**. P0.1.10.04.3 coding_closed_unverified; close04 **3/4**. Forward-only02414 ограничивает incrementable safe revision и усиливает SQL replay canonical batch/response shape/revision. Own42 SQL сценария написаны: 20 transaction/upgrade (включая mixed retouch queued child), 5 locks, 17 replay corruption/authorization/expiry. Сверены receipt/source/target/publication locking, atomic parent/batch/receipt/audit/events/outbox, неизменность targets/child и авторизация replay без истёкших объектов. Сценарии не запускались, OID/ACL preservation пока authored assertion. Следующий `.04.4` — service/routes/client.

# Незакрытый участок — close SQL, 2026-10-16

Добавлена forward-only02414: ограничение incrementable JS-safe revision при сохранении SQL close mutation, OID/owner/ACL/search_path и событий. Own close fixture проходит настоящие reject-only decisions, затем canonical close publication/commit/replay. Написаны 19 transaction/boundary/refusal/rollback/upgrade вариантов и пять caller transaction lock вариантов. Сценарии не запускались. Проверка replay refusals и итоговая сверка source/lock контрактов ещё открыты. **21,928571%, прирост 0; close04 2/4.**

# Прогресс кодирования — close publication, 2026-10-16

**21,892857 → 21,928571% (+0,035714 п.п.)**. P0.1.10.04.2 coding_closed_unverified; close04 **2/4**. Сверены authenticated source/target preparation и SQL-bound artifactjson reserve/complete без транзакции вокруг object IO. Устранён возврат успеха после отмены ready-publication, добавлены непустой artifact ref, независимые proof bytes, отмена до IO и конфигурационный budget bound. Написаны ownership, девять failure/cancellation/refusal вариантов и cancelled entrypoint сценарии. Не запускались. Следующий пункт `.04.3` — SQL/replay, включая остающийся SQL safe revision дефект.

# Прогресс кодирования — close proof, 2026-10-16

**21,857143 → 21,892857% (+0,035714 п.п.)**. Закрыт P0.1.10.04.1 coding_closed_unverified; close04 **1/4**. Устранена неинкрементируемая JS-safe revision в close DTO/proof. Написаны boundary/refusal, canonical ownership/order/fingerprint и invalid target inventory сценарии; сохранены требования всех решений и независимости от завершения child. SQL всё ещё принимает слишком большой revision: исправление относится к открытому `.04.3`, не считается закрытым. Ничего не запускалось. Следующий подпункт `.04.2` — preparation/publication.

# Прогресс кодирования — закрыт mixed retouch, 2026-10-16

**21,828571 → 21,857143% (+0,028571 п.п.)**. P0.1.10.03.5 coding_closed_unverified; mixed03 **5/5**, весь критерий смешанных решений закрыт по кодированию. Исправлены physical raw ownership и общий qualification pin multi-target fixtures. Написаны 18 content/multi-target, 20 SQL transaction и 6 publication/inventory вариантов. Сверены собственные retouch resolver, compilation/publication, decision locks, draft/quota/seal/activation/receipt/events; существующие single/multi concurrency сценарии покрывают оба target, outputs/results и post-wait revocation. Текущая02399 уже содержит нужные блокировки; новой миграции не требуется. Evidence: audit/semantic-mixed-retouch-inventory-2026-10-16.json. Тесты, компиляторы и миграции не запускались. P0.1.10 **3/10**; следующий пункт `.04` — authenticated close review.

# Незакрытый участок — mixed retouch content matrix, 2026-10-16

Написаны 18 own retouch content/multi-target вариантов: mixed/reverse/omit, пропущенные и лишние inputs, повреждённые source/image/candidate, caller mutation, authority/cancellation, два approvals, обратный порядок inputs, повреждённый второй closure и отсутствующий второй retouch adapter context. Используются собственные retouch evidence, Params и qualification структуры; численные evidence и presentation SHA пересчитываются после объединения review. Private verified markers относятся только к content tests. Просмотрены production preparation rechecks, installation/routing и retouch commit SQL entry. Evidence сохранено в audit/semantic-mixed-retouch-in-progress-2026-10-16.json. Финальная сверка контрактов остаётся открытой; исполнения не было. **21,828571%, прирост 0; mixed 4/5.**

# Незакрытый участок — mixed retouch transaction matrix, 2026-10-16

Добавлен semantic_mixed_retouch_fixture: один фактический retouch review, 1/2 approved, 1 rejected, 1 omitted; preview создаётся только approved, Face68 adapter context сохраняется собственным retouch producer, child и copy qualification общие. Preview fixture принимает общий qualification pin для нескольких approved targets; публикационные сценарии также используют общий pin. Написаны 20 own retouch SQL вариантов: accepted/reverse, task inventory, rejected/session authority, поздний event cursor, caller rollback, quota и qualification refusal. Все сценарии используют retouch commit, а не cleanup authority. Ничего не запускалось. Content matrix и итоговая сверка ещё открыты. **21,828571%, прирост 0; mixed 4/5.**

# Незакрытый участок — mixed retouch SQL publication, 2026-10-16

Расширен own retouch publisher fixture: полный inventory approve/reject проверяется до SQL, rejected targets не вызывают seal и не содержат preview. Approved/copy/transport/admission fixtures принимают общий prepared approved tuple, child и qualification для настоящего multi-target Apply без повторной квалификации. Написаны два mixed/reverse SQL publication сценария и четыре отказа inventory до IO. Сценарии не запускались. Полная транзакционная матрица и content matrix ещё открыты. **21,828571%, прирост 0; mixed 4/5.**

# Незакрытый участок — mixed retouch ownership, 2026-10-16

P0.1.10.03.5: подготовка пакета теперь копирует physical ImageBytes и raw presentation/evidence до callback, вместе с source documents. К существующим retouch publisher сценариям добавлен явный mixed approve/reject сценарий мутации вложенного preview и rejected decision между publisher callbacks, а также отказы некорректного документа и отсутствующего publisher. В актуальной 02399 подтверждены отдельная проверка FOUND после task lookup и FOR SHARE для outputs и canonical task result; новая миграция для уже существующих блокировок не нужна. Полная mixed retouch content/transaction matrix остаётся открытой. Тесты и компиляторы не запускались. **21,828571%, прирост 0; mixed 4/5.**

# Прогресс кодирования — закрыт mixed cleanup, 2026-10-16

**21,800000 → 21,828571% (+0,028571 п.п.)**. Закрыт P0.1.10.03.4 coding_closed_unverified; mixed03 **4/5**. Собственные cleanup Params/evidence/source fixtures, 17 content/multi-target вариантов и 22 SQL transaction/upgrade варианта написаны. Исправлены physical/request ownership и selection race (forward-only02413). Актуальные installation/resolver/compiler/commit/SQL связи сведены в `audit/semantic-mixed-cleanup-inventory-2026-10-16.json`. Тесты/миграции/компиляторы не запускались, physical/quality qualification не заявляется. Следующий подпункт `.03.5` — retouch.

# Незакрытый участок — mixed cleanup ownership/locks, 2026-10-16

Начат P0.1.10.03.4. Исправлены общие physical preview raw slices и общий вложенный request approved/decision publisher'ов в cleanup. Собственный cleanup SQL selection также не удерживал output/result до commit: добавлена forward-only02413 с сохранением FOUND. Написаны mutation isolation/refusal и concurrent own cleanup output/result lock сценарии; проверены SQL сигнатуры/структуры источников. Multi-target mixed content/transaction matrix и upgrade02413 ещё впереди. Ничего не запускалось. **21,800000%, прирост 0; mixed 3/5.**

# Прогресс кодирования — закрыт mixed exposure, 2026-10-16

**21,771429 → 21,800000% (+0,028571 п.п.)**. Закрыт P0.1.10.03.3 в статусе coding_closed_unverified; mixed03 **3/5**. Собственные shown/legacy content scenarios и 22 transaction/upgrade варианта написаны; актуальные compiler/publication/resolver/commit/SQL связи сведены в `audit/semantic-mixed-exposure-inventory-2026-10-16.json`. Исправлены whole-batch raw preflight, callback cancellation, владение publisher/physical bytes и race output/result selection (forward-only02412). Upgrade-сценарии сохраняют pending/completed receipts и function OID/ACL. Тесты/миграции/компиляторы не запускались, физическая квалификация не заявляется. Следующий подпункт `.03.4` — background cleanup.

# Незакрытый участок — atomic mixed exposure и selection locks, 2026-10-16

Собраны собственные exposure decision/approved/copy/transport/qualification fixtures и 20 transaction-сценариев полного 02232 Apply, включая точные задачи/квоту, rejection/omission, replay, authority refusal и откат 23 таблиц. При сверке найден production race: 02208 не удерживал output artifacts/canonical result selection до commit. Добавлена forward-only 02412 с FOR SHARE и сохранением FOUND; написан concurrent lock сценарий. Остались upgrade-preservation сценарий и итоговая сверка перед зачётом `.03.3`. Ничего не исполнялось. **21,771429%, прирост 0; mixed 2/5.**

# Незакрытый участок — own exposure preview SQL prerequisites, 2026-10-16

Добавлены `exposure_preview_activation_fixture.py` и `exposure_preview_receipt_fixture.py`: собственный typed exposure Params, CPU budgets, transport, настоящий preapply preview commit и receipt с local_exposure_exact_replay_v1. Три написанных alpha/dense сценария связывают receipt с locked selection; из трёх исходных proposed targets только первый получает preview, остальные не получают preview/child/batch. Сверены SQL сигнатуры, wire profile и config path. Синтетические output/qualification metadata не являются физической проверкой. Полный mixed Apply ещё открыт, тесты не запускались. **21,771429%, прирост 0; mixed 2/5.**

# Незакрытый участок — исходный SQL review экспозиции, 2026-10-16

Создан `exposure_review_documents_fixture.py`: original transfer/assembly review сразу с local_exposure EV=.5, несколько настоящих proposed targets, полный проверяемый по SHA реестр документов. Общие builders расширены явной веткой local_exposure; готовые результаты других операций не переименовываются. Написаны четыре own source-authority сценария (1/2/3 targets, alpha/dense). Следующий шаг — preview admission/receipt и atomic mixed transaction на этом review. Тесты не запускались. **21,771429%, прирост 0; mixed 2/5.**

# Незакрытый участок — владение physical exposure bytes, 2026-10-16

При сверке транзакционных связей найден и исправлен дополнительный production разрыв: struct-copy `PhysicalExposurePreview` сохранял общие image/presentation/evidence raw slices после whole-batch preflight. Теперь эти три документа копируются до callback'ов. Добавлен успешный content-сценарий поздней мутации исходных preview/candidate/intent/source bytes: публикация должна использовать собственный замороженный пакет. Сценарий не запускался; SQL transaction fixture и полное закрытие критерия ещё впереди. **21,771429%, прирост 0; mixed 2/5.**

# Незакрытый участок — SQL mixed exposure binding, 2026-10-16

Написаны 16 SQL-вариантов привязки mixed exposure batch к собственному JCS inventory/chunk/manifest и один сценарий private ACL. Допустимы approve/reject/reverse/omit/two approvals; отказываются чужие/дублированные tasks, изменённые выбранные refs, child, operation и транспорт. Это pure binding gate, не транзакционный PASS. По исходникам прослежены 02222 target states, 02224 сумма approved tasks и 02232 атомарная оркестрация. Транзакционные quota/activation/replay/rollback fixtures остаются открыты. Тесты и миграции не запускались. **21,771429%, прирост 0; mixed 2/5.**

# Незакрытый участок — shown mixed exposure content, 2026-10-16

Добавлены собственные content fixtures экспозиции: EV intent/approved edit, ExposurePreviewEvidence exact replay, dense/alpha source geometry и пересчитанные immutable refs. Десять mixed-вариантов охватывают approve/reject, reverse/omit, coverage, raw source/image/candidate повреждения и потерю authority/отмену. Шесть whole-batch вариантов требуют проверки второго closure до первой публикации; допустимы два одобрения и обратный порядок inputs. Private verified markers — только content tests, не доказательство физической квалификации. Сценарии не запускались. SQL matrix и итоговое связывание ещё открыты. **21,771429%, прирост 0; mixed 2/5.**

# Незакрытый участок — legacy mixed exposure, 2026-10-16

Добавлен `exposure_mixed_batch_test.go`: собственный review local_exposure с несколькими предложенными кандидатами и сохранением их source bindings; шесть вариантов mixed/reverse/omission/missing/extra/substituted inputs проходят настоящий compiler, публикацию транспорта и подготовку commit. Для допустимых вариантов написан approval-only control: отказ не меняет task ID, Params SHA и execution/resource profile. Дублирование исполнения должно возвращать пустой commit; исходные rejected/omitted target остаются неизменными в памяти. Это authored coverage без исполнения и без доказательства SQL квоты. Shown content/SQL matrix ещё открыты. **21,771429%, прирост 0; mixed 2/5.**

# Незакрытый участок — preflight экспозиции, 2026-10-16

Написана матрица десяти отказов через настоящий `prepareShownExposureBatch` и `decisionProofKindFixture(local_exposure)`: неполный/чужой input, reject-only, stale revision, duplicate decision, cancellation и четыре отсутствующие зависимости. Проверяется отсутствие любых authority/publication callback'ов и частичного результата. Сверены показанный и legacy resolver: reject пропускается перед физическим preview, reject-only обходит бюджетный selector. Текущее evidence и открытые части сохранены в `audit/semantic-mixed-exposure-in-progress-2026-10-16.json`. Сценарии не запускались; успешный mixed content и SQL matrix ещё впереди. **21,771429%, прирост 0; mixed 2/5.**

# Незакрытый участок — изоляция публикации экспозиции, 2026-10-16

P0.1.10.03.3: production orchestration вынесена в private `prepareShownExposureBatch` с process-bound callback'ами approved/copy/decision. Оба publisher решения получают независимый strict decode замороженных canonical bytes; вложенные preview и список решений не разделяются между этапами. Написаны сценарии mutation isolation для mixed approve/reject, неверного документа и отсутствующего publisher. Исходники связей публичного entrypoint и внутренних callback'ов просмотрены; тесты/компиляторы не запускались. Полный mixed exposure/legacy/SQL остаётся незавершённым. **21,771429%, прирост 0; mixed 2/5.**

# Незакрытый участок — экспозиция, 2026-10-16

P0.1.10.03.3 остаётся открытым. В `shown_batch_preparation.go` добавлена проверка полного raw document bundle каждого одобренного кадра до первой публикации. Все batch authority recheck, включая переданный во внутреннюю подготовку, теперь проверяют отмену до и после callback. Написаны шесть сценариев authority fence: активный контекст, отсутствующий callback, отмена до/внутри callback, отказ и отказ одновременно с отменой. Сценарии не запускались. Полные mixed exposure/legacy/SQL и независимое владение запросами publisher ещё требуют завершения. Процент без изменения: **21,771429%**, mixed **2/5**; локальные исправления не выданы за закрытие всего критерия.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,771429%**. Закрыты P0.1.10.03.1 (общий immutable mixed proof/drafts) и `.03.2` (удаление: mixed compiler/publication/SQL). Пять частей `.03` зафиксированы до зачёта; сейчас **2/5**, по **+0,028571 п.п.** каждая. 21,714286→21,771429% (**+0,057143 п.п.**). Написаны16 SQL и10 Go-сценариев; отклонённый/пропущенный кадр без preview, точная квота/задачи и полный откат включены. P0.1.10:2 целых + .03 на2/5. Далее `.03.3` — экспозиция; cleanup/retouch также остаются открыты. Тесты не запускались.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,714286%**. Закрыт P0.1.10.02 — reject-only transaction без child/execution quota, точный replay и rollback. Исправлены игнорируемые лишние prepared child/legacy tasks и выход SQL revision за безопасный диапазон (forward-only02411). Написаны25 SQL и7 Go-сценариев; fresh/upgrade, поздний откат и сохранение receipts включены. P0.1.10 — **2/10**. 21,571429→21,714286% (**+0,142857 п.п.**). Далее `.03` — смешанные approve/reject решения. Тесты и миграции не запускались.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,571429%**. Закрыт P0.1.10.01 — строгий отказ/immutable decision proof без preview. Исправлено принятие сервером `preview:null` вопреки клиентскому контракту; написаны Go/client сценарии null/omission и exact rejection lineage. P0.1.10 заранее разбит на десять равных результатов; закрыт **1/10**. 21,428571→21,571429% (**+0,142857 п.п.**). Далее `.02` — reject-only transaction без child/quota, replay и rollback. Тесты не запускались.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,428571%**. Закрыт `.remove_object.10.5`, весь `.remove_object.10` (**5/5**) и P0.1.09 (**30/30**). Текущие исходники и расхождения исторических снимков сведены в `audit/removal-final-connections-inventory-2026-10-16.json`. 21,419048→21,428571% (**+0,009524 п.п.**); родительский вес учтён один раз. Далее P0.1.10 — окончательные отказы/активация/полная трасса semantic review. Тесты не запускались; аппаратные, фотографические и общесистемные ворота остаются открыты.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,419048%**. Закрыт `.remove_object.10.4`: worker → точная копия → SQL adoption → принятая версия. Восемь изменений исторического снимка сверены; исправлены отдельный референс/полнота dense-входов фикстуры и снимок отката (23 таблицы). Добавлены пять сценариев отмены и два multi-target сценария alpha/dense/reference. Финальная сверка — **4/5**. 21,409524→21,419048% (**+0,009524 п.п.**). Далее `.10.5` — полный актуальный реестр связей. Тесты не запускались.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,409524%**. Закрыт `.remove_object.10.3`: актуальные выбор/Apply/receipt и evidence сверены. Финальная сверка — **3/5**. 21,400000→21,409524% (**+0,009524 п.п.**). Далее `.10.4` — worker/принятая версия. Тесты не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,400000%**. Закрыт `.remove_object.10.2`: расчёт/preservation/staging/ledger/выдача preview. Финальная сверка — **2/5**. 21,390476→21,400000% (**+0,009524 п.п.**). Далее `.10.3` — выбор/Apply. Тесты не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,390476%**. Закрыт `.remove_object.10.1`: backend/startup/API/registry, CUDA10GB и Metal availability. Финальная сверка — **1/5**. 21,380952→21,390476% (**+0,009524 п.п.**). Далее `.10.2` — расчёт/preview. Тесты не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,380952%**. Закрыт `.remove_object.07.5` и весь `.07`: production API→resolver→publication→transaction/commit→receipt replay. Участок `.07` — **5/5**, P0.1.09 — **29/30**. 21,371429→21,380952% (**+0,009524 п.п.**). Далее `.remove_object.10` — финальная трасса удаления. Тесты не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,371429%**. Закрыт `.remove_object.07.4`: атомарная регистрация/seal/activation/receipt, rollback и replay; 18 собственных SQL-сценариев. Участок `.07` — **4/5**. 21,361905→21,371429% (**+0,009524 п.п.**). Далее `.07.5` — итоговая трасса Apply. Тесты не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,361905%**. Закрыт `.remove_object.07.3`: SQL draft/quota, точное повторное резервирование и rollback; 37 собственных сценариев, включая несколько задач одного review. Участок `.07` — **3/5**. 21,352381→21,361905% (**+0,009524 п.п.**). Далее `.07.4` — атомарные seal/activation/receipt. Тесты не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,352381%**. Закрыт `.remove_object.07.2`: проверка полного пакета до публикации, отдельные копии запроса publisher’ов и сценарии двух кадров одного review. Участок `.07` — **2/5**. 21,342857→21,352381% (**+0,009524 п.п.**). Далее `.07.3` — SQL draft/quota. Тесты не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,342857%**. Закрыт `.remove_object.07.1`: исходники/защитная маска, версия объектов, отмена и qualification report; SQL scope matrix — 60 написанных сценариев. Участок `.07` — **1/5**. 21,333333→21,342857% (**+0,009524 п.п.**). Далее `.07.2` — компиляция и публикация показанной копии. Тесты не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,333333%**. Закрыт `.remove_object.06.5` и весь `.06`: текущая production трасса выбора до SQL selection с учётом 02410. Участок — **5/5**, P0.1.09 — **28/30**. 21,323810→21,333333% (**+0,009524 п.п.**). Далее `.07` — подготовка и атомарный Apply. Тесты не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,323810%**. Закрыт `.remove_object.06.4`: клиентский выбор показанного результата, повторная проверка после журнала и неизменный повтор отправленного запроса. Участок `.06` — **4/5**. 21,314286→21,323810% (**+0,009524 п.п.**). Далее `.06.5` — итоговая трасса выбора. Сценарии написаны; тесты не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,314286%**. Закрыт `.remove_object.06.3`: Go whole-selection и actual SQL batch gate, исправлены multi-fixture SHA/qualification pins. Участок `.06` — **3/5**.21,304762→21,314286% (**+0,009524 п.п.**). Далее `.06.4` — решение фотографа в клиенте. Тесты не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,304762%**. Закрыт `.remove_object.06.2`: SQL receipt/selection, удержание output locks и сценарии отказов/конкуренции. Участок `.06` — **2/5**.21,295238→21,304762% (**+0,009524 п.п.**). Далее `.06.3` — Go transaction selection. Тесты/миграции не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,295238%**. Закрыт `.remove_object.06.1`: immutable shown decision identity, exact render/evidence refs и сценарии изменения выбора. Участок `.06` — **1/5**.21,285714→21,295238% (**+0,009524 п.п.**). Далее `.06.2` — SQL receipt/selection. Тесты не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,285714%**. Закрыт `.remove_object.05.5` и весь `.05`: итоговая трасса чтения/выдачи preview, исправлен nil-stream panic. Участок `.05` — **5/5**, P0.1.09 — **27/30**.21,276190→21,285714% (**+0,009524 п.п.**). Далее `.06` — показанный результат/решение фотографа. Тесты не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,276190%**. Закрыт `.remove_object.05.4`: API/client issuance, canonical producer reread, подписанные ссылки и сценарии отказов. Участок `.05` — **4/5**.21,266667→21,276190% (**+0,009524 п.п.**). Далее `.05.5` — итоговая трасса чтения. Тесты не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,266667%**. Закрыт `.remove_object.05.3`: физические NPY/PNG/ICC и сценарии отказов. Участок `.05` — **3/5**.21,257143→21,266667% (**+0,009524 п.п.**). Далее `.05.4` — выдача API/клиент. Тесты не запускались. Ниже — история.

# Прогресс кодирования — обновление 2026-10-16

**Кодирование 21,257143%**. Закрыт `.remove_object.05.2`: канонические документы и повторная проверка producer после чтения. Участок `.05` — **2/5**. 21,247619→21,257143% (**+0,009524 п.п.**). Далее `.05.3` — физические изображения. Тесты не запускались. Ниже — история.

# Последнее закрытие —2026-10-16

**Кодирование21,247619%**. Закрыт `.remove_object.05.1`: own SQL source/sidecar authorization и ACL. Участок `.05` —**1/5**.21,238095→21,247619% (+0,009524п.п.). Далее `.05.2` — документы/producer fencing. Tests/migrations не запускались. Ниже — история.

# Последнее закрытие —2026-10-16

**Кодирование21,238095%**. Закрыт `.remove_object.04.5` и весь `.04`: recovery после неопределённого commit, cancellation replay, итоговая admission трасса. Участок `.04` —**5/5**, P0.1.09 —**26/30**.21,228571→21,238095% (+0,009524п.п.). Далее `.05` — авторизованное чтение preview. Tests/migrations не запускались. Ниже — история.

# Последнее закрытие —2026-10-16

**Кодирование21,228571%**. Закрыт `.remove_object.04.4`: atomic SQL activation/authority/cancel/ACL. Участок `.04` —**4/5**.21,219048→21,228571% (+0,009524п.п.). Далее `.04.5` — recovery/uncertain commit. Tests/migrations не запускались. Ниже — история.

# Последнее закрытие —2026-10-16

**Кодирование21,219048%**. Закрыт `.remove_object.04.3`: quota replay/scope/atomic refusal/ACL. Участок `.04` —**3/5**.21,209524→21,219048% (+0,009524п.п.). Далее `.04.4` — атомарная активация. Tests/migrations не запускались. Ниже — история.

# Последнее закрытие —2026-10-16

**Кодирование21,209524%**. Закрыт `P0.1.09.remove_object.04.2`: server/compiler/source/worker/live SQL qualification binding и сценарии отзыва. Участок `.04` —**2/5**.21,200000→21,209524% (+0,009524п.п.). Следующий шаг `.04.3`: quota. Проверки выполнения отложены. Ниже — история.

# Последнее закрытие —2026-10-16

**Кодирование21,200000%**. Закрыт `P0.1.09.remove_object.04.1`: согласованы ресурсные границы Go/SQL/worker и RTX config, написаны boundary/refusal scenarios. Участок `.04` —**1/5**. Было21,190476%, прирост**0,009524п.п.** Вклад фиксированной базы: `14 + (25 + 1/5)/30`. Следующий шаг: `.04.2`, привязка квалификации и source/profile. Tests не запускались. Ниже — предыдущие записи.

# Актуальное правило публикации — 2026-10-16

Текущий процент кодирования остаточного MVP-плана: **21,190476%**. Тестирование и багфикс учитываются отдельно.

Закрыты `retouch_region.01.5` и `remove_object.01.5`: финальная статическая сверка схем, каталога, генераторов и worker/SQL pin rollout. Каждый участок `.01` — **5/5**; P0.1.09 — **25/30**. Статус coding_closed_unverified; тесты и миграции не запускались.

Фиксированная база:70 пунктов. Вклад: `14 + 25/30`. Последовательные закрытия: **21,171429→21,180952→21,190476%**, каждое **+0,009524п.п.** Подшаги заменены полным весом родительской части без двойного счёта.

После каждого закрытия публиковать ID и результат, локальный счётчик, процент до/после, прирост и следующий шаг. Декомпозиция сама по себе не увеличивает процент. До начала работы фиксировать веса и критерии; при обнаружении дефекта повторно открывать критерий и вычитать его вес. Промежуточную работу сообщать словами без численного зачёта. Шесть знаков отражают арифметику фиксированных весов, а не точность оценки всей системы. Ниже — исторический журнал.

---

# Детальный учёт прогресса кодирования

Дата: 2026-10-15. Источник итогового процента: `mvp-coding-progress.json`.

## Фиксированная база

Общий объём — 70 равновесных родительских пунктов обновлённого MVP-плана. Закрыты 14 родительских пунктов, 11 из 30 частей пункта P0.1.09 и 3 из 5 подшагов retouch_region.01. Общий процент: `100 × (14 + (11 + 3/5)/30) / 70 = 20,552381%` (публикация: 20,552%). Актуальный состав закрытых долей определяется JSON-реестром; исторические записи ниже отражают состояние на момент их добавления.

Закрытие одной из этих 30 частей добавляет 0,047619 процентного пункта. Декомпозиция не увеличивает вес родительского пункта и не даёт повторного зачёта уже выполненной работы.

## Более мелкие шаги

Перед работой над очередной частью фиксировать конечный список её подшагов, веса и критерии закрытия. Сумма весов равна весу части. Подшаг закрывается только при наличии конкретной реализации, написанных применимых тестовых сценариев и статической трассы требований к коду. Наличие файла или число строк не является критерием.

Для уже начатой P0.1.09.background_cleanup.08 использовать пять равных подшагов:

1. Получение lease и старт: реальные SQL claim/start, соответствие команды, операции, профиля, quota и отмены; сценарии отказов.
2. Исполнение: привязка показанных документов и копирование точных пикселей; ограничения ресурсов, отмена и fencing; сценарии отказов.
3. Проверка результата: согласованность опубликованных документов, объектов и inventory; повторная проверка полномочий; сценарии отказов.
4. Атомарное принятие: связка adopter с SQL commit, создание принятой asset version, идемпотентность и rollback; сценарии отказов.
5. Сквозная статическая трасса: связь всех переходов и обработка повторов/ошибок, перечень написанных сценариев и оставшихся ограничений.

Каждый подшаг имеет вес 1/150 родительского пункта, или 0,009524 процентного пункта общего плана. До отдельного подтверждения evidence все пять считаются открытыми; ранее выполненные изменения проверяются и засчитываются однократно. При переходе на этот учёт закрытие всей части заменяет её дробный вклад, а не добавляется к нему.

## Формат публикации

После закрытия подшага публиковать: идентификатор и конкретный результат; закрыто подшагов текущей части; общий процент кодирования с шестью знаками после запятой; прирост в процентных пунктах; ссылки на evidence.

Отдельно указывать статус тестирования. `coding_closed_unverified` означает написанный код, сценарии и статическую проверку, но не успешный запуск тестов. Глубокое тестирование, сборки, миграции и GPU-квалификация остаются отдельным этапом согласно указанию пользователя. Общий процент не переименовывать в готовность всей системы к эксплуатации.

## Текущая часть: background_cleanup.09

Предыдущая часть .08 закрыта по кодированию: 5/5 подшагов, без запуска тестов. Для .09 заранее фиксируются пять равных подшагов; каждый добавляет 0,009524 процентного пункта общего плана:

1. Регистрация и маршрутизация: согласованные catalog, operation ID, профили и маршруты preview/adoption; сценарии ошибочной регистрации.
2. Startup preview: проверка конфигурации, model/ICC/contract pins и зависимостей до запуска; сценарии отказа и успешной конфигурации.
3. Startup adoption: подключение копирования и adopter, scratch policy, безопасный отказ при неполной конфигурации; соответствующие сценарии.
4. Capability и клиент: согласованная доступность backend → API → экран, повторная проверка после асинхронных шагов, автоматическое отключение неподдерживаемых операций; сценарии потери доступности и восстановления.
5. Сквозная статическая сверка регистрации, запуска и capability: замкнутая трасса, evidence для каждого перехода и написанные сценарии отказов.

На момент фиксации все пять имеют статус pending_static_audit. Уже написанные изменения засчитываются только после сверки полного критерия подшага; сама декомпозиция процент не меняет. Публиковать общий процент с тремя десятичными знаками, точный прирост — с шестью. Открытый подшаг можно описывать как работу в процессе, но нельзя давать ему субъективный процент.

Актуализация: .09.1 закрыт по кодированию без запуска тестов; текущая часть 1/5. Итог `100 × (14 + (9 + 1/5)/30) / 70 = 20,438095%`, публикация 20,438%. Дальнейшие подшаги остаются открытыми.

Актуализация: .09.2 startup preview закрыт по кодированию без запуска тестов. Текущая часть 2/5; общий процент 20,447619%, публикация 20,448%.

Актуализация: .09.3 startup adoption закрыт по кодированию без запуска тестов. Текущая часть 3/5; общий процент 20,457143%, публикация 20,457%.

Актуализация: .09.4 capability/client закрыт по кодированию без запуска тестов. Текущая часть4/5; общий процент20,466667%, публикация20,467%.

Актуализация: .09.5 закрыт по кодированию; background_cleanup.09 целиком5/5 и считается один раз. Теперь10/30 частей P0.1.09; общий процент20,476190%, публикация20,476%. Тесты не запускались.

## Текущая детализация: background_cleanup.10

По запросу пользователя прогресс публикуется после каждого закрытого подшага. Зафиксированы пять равных подшагов:

1. Входная трасса: запрос, intent, snapshot и точные параметры операции.
2. Вычисление: сохранность изображения, ресурсы и отмена между чтениями.
3. Публикация, чтение и выбор точного показанного результата.
4. Apply: dispatch, adoption, материализация версии и повторные запросы.
5. Итоговая сверка всех переходов и написанных сценариев отказов.

Текущее состояние: 0/5. Каждый подшаг добавляет 0,009524 процентного пункта. Формула: `100 × (14 + (10 + k/5)/30) / 70`, где k — число закрытых подшагов .10. Декомпозиция сама по себе не повышает процент. Пример следующей публикации после подтверждённого закрытия: `20,476% → 20,486%; +0,009524 п.п.; .10 — 1/5`. Кодирование и фактическое прохождение тестов учитываются раздельно. Исторические записи выше сохраняются как журнал.

Актуализация .10: закрыт подшаг .10.2 (compute, preservation, resources, cancellation), текущая часть 1/5. Общий процент 20,485714% (20,486%); прирост +0,009524 п.п. Остальные четыре подшага открыты. Тесты не запускались.

Актуализация: закрыт .10.1 (входная трасса и save/replay/scope). Текущая часть 2/5; общий процент 20,495238% (20,495%), прирост +0,009524 п.п. Тесты не запускались.

Актуализация: .10.3 publication/read/shown selection закрыт по кодированию. .10 — 3/5; общий 20,504762% (20,505%), прирост +0,009524 п.п.; тесты не запускались.

Актуализация: .10.4 Apply/dispatch/copy/adoption/materialization закрыт по кодированию. .10 — 4/5; общий 20,514286% (20,514%), +0,009524 п.п. Тесты не запускались.


Актуализация .10.5: итоговая статическая карта отказов закрыта по кодированию, часть .10 —5/5. Теперь11/30 частей; weighted14+11/30 из70, общий20,523810% (20,524%), прирост+0,009524п.п. Тесты не запускались. Следующая часть retouch_region.01; её вес пока не засчитан.


Retouch_region.01 состоит из пяти фиксированных равных долей (audit/retouch-region-contract-coding-trace.md). Закрыт .01.1: own input и Go/Python exact-wire сценарии. Часть1/5; общий20,533333% (20,533%),+0,009524п.п.; тесты не запускались.


Закрыт retouch_region.01.4: own shown producer контракт, Go/Python shared maximum-fence wire. Текущая часть2/5, coding20,542857% (20,543%),+0,009524п.п. Runtime authority не засчитана; тесты не запускались.


Закрыт retouch_region.01.3: own evidence/presentation, Go/Python shared wire и pinned result binder. Текущая часть3/5; кодирование20,552381% (20,552%),+0,009524п.п. Тесты не запускались.


## Дополнительное уточнение по запросу пользователя

Прогресс показывать по минимальным заранее определённым проверяемым результатам: функция или связанная группа функций, SQL-переход, интеграционное подключение, контракт данных. Не ждать закрытия всего модуля для публикации прироста.

Если существующий подшаг слишком крупный, перед следующей работой разложить его на конечный список дочерних результатов с критериями и evidence в JSON-реестре. Сумма весов детей должна точно сохранять вес подшага (1/150 родительской единицы). Например, десять равных детей дают каждому вес 1/1500 родительской единицы и прирост 0,000952381 процентного пункта общего плана. Само разбиение ничего не засчитывает; прежние изменения можно зачесть только после отдельной статической сверки соответствующего критерия. Не учитывать один результат одновременно на уровне ребёнка и родителя.

В отчёте показывать общий процент до шести десятичных знаков, прирост, число закрытых минимальных результатов текущего участка и конкретный результат. Дополнительно отделять «код написан, статическая трасса ещё открыта» от «закрыто по кодированию» — первая категория не даёт субъективной дробной прибавки. Тестирование и багфикс остаются отдельным этапом.

Базовая точка уточнённого учёта: **20,552381%**, retouch_region.01 — **3/5**; изменение правил отображения даёт **+0 п.п.**


Закрыты retouch_region.03.1 и .03.2 по кодированию и статической трассе: attempt writer и numerical evidence внутри срока жизни compute. .03 — 2/5; вклад .01 — 3/5; weighted children P0.1.09 = 11 + 3/5 + 2/5 = 12. Общий процент `100 × (14 + 12/30) / 70 = 20,571429%`; прирост +0,019048 п.п. Тесты не запускались.


Закрыт retouch_region.03.3: полная attempt presentation с сохранением compute lifetime и общим RAM/output бюджетом; Go и forward-only SQL quota обновлены. .03 — 3/5; weighted children 12,2/30; общий кодинг **20,580952%**, прирост **+0,009524 п.п.** Сценарии написаны, не запускались.


Закрыт retouch_region.03.4: запись фактических stage-результатов и итоговая проверка полного связанного output closure. .03 — 4/5; weighted children 12,4/30; общий кодинг **20,590476%**, прирост **+0,009524 п.п.** Сценарии написаны, не запускались.


2026-10-16: закрыт retouch_region.07.1 по кодированию без запуска тестов. .07: 1/10. Общий процент 20,5905% → 20,5952%; точный прирост +0,004762 п.п. Критерий и evidence — в реестре и трассе.


2026-10-16: закрыт retouch_region.07.2 по кодированию без запуска тестов. .07 — 2/10. Общий процент 20,5952% → 20,6000%; прирост +0,004762 п.п. Входы/CPU-ресурсы и сценарии alpha/dense/dedup/qualification сверены статически.


2026-10-16 — повторное уточнение отображения по запросу пользователя: каждый отчёт содержит конкретный закрытый микрошаг, счётчик текущего участка, общий процент с шестью десятичными знаками и прирост в процентных пунктах. Текущий участок .07 — 2/10; общий процент 20,600000%. Следующее закрытие одного равновесного микрошагa .07 даст 20,604762% (+0,004762 п.п.). Изменение отображения не закрывает работу и даёт +0 п.п. Незавершённые изменения перечисляются отдельно без прибавки.


2026-10-16: retouch_region.08 заранее разделён на 5 равновесных результатов (lease/dispatch, физическое исполнение, locked adoption, accepted version/media, итоговая трасса). Пока 0/5 закрыты; каждый добавит 0,009524 п.п. после статического подтверждения полного критерия. Вес родителя сохранён, дроби не дублируются. Общий процент 20,600000%; разбиение даёт +0 п.п.


2026-10-16: закрыт retouch_region.08.2 (физическое исполнение копирования) по кодированию и статической трассе, без запуска тестов. .08 — 1/5; .07 — 2/10. 20,600000% → 20,609524%, +0,009524 п.п.; точный итог 20,6095238095%. Остальные переходы .08 ожидают положительных relational-сценариев и итоговой трассы.

2026-10-16: закрыт .08.1.4 по кодированию и полной статической трассе live IO. .08.1 — 4/5. 20,615238% → 20,617143%; +0,001905 п.п. Сценарии написаны, не запускались.

2026-10-16: закрыт .08.1.5 и родитель .08.1 по кодированию/статической трассе. .08.1 5/5; .08 2/5. 20,617143% →20,619048%, +0,001905 п.п. Сценарии написаны, не запускались.

2026-10-16: .08.3 разделён на5 равновесных результатов. Закрыт .08.3.1 (канонический результат/retention) по кодированию и статической трассе;1/5.20,619048%→20,620952%,+0,001905п.п. Сценарии написаны, не запускались.

2026-10-16: закрыт .08.3.2 (точный replay и конфликты полного inventory). .08.3 2/5.20,620952%→20,622857%,+0,001905 п.п. Сценарии написаны, не запускались.

2026-10-16: закрыт .08.3.3: исправлен отказ при поздней отмене preview/producer и добавлены сценарии потери полномочий. .08.3 3/5.20,622857%→20,624762%,+0,001905 п.п. Без запуска тестов.

2026-10-16: закрыт .08.3.4 (полный rollback после частичной записи и отсутствие обхода). .08.3 4/5.20,624762%→20,626667%,+0,001905 п.п. Без запуска тестов.

2026-10-16: закрыт .08.3.5 и родитель .08.3: статическая цепочка physical verifier/coordinator/SQL и authored сценарии остановки/rollback. .08.3 5/5;.08 3/5.20,626667%→20,628571%,+0,001905 п.п. Без запуска тестов.

2026-10-16: закрыты .08.4.1 (ожидание/отмена/освобождение квоты) и .08.4.2 (успех/settlement/consumed). .08.4 2/5.20,628571%→20,630476%→20,632381%, каждый+0,001905 п.п. Без запуска тестов.

2026-10-16: закрыт .08.4.3 (точная принятая версия/alpha/повтор/отказы). .08.4 3/5.20,632381%→20,634286%,+0,001905 п.п. Без запуска тестов.


2026-10-16: .08.4.4 закрыт по кодированию без исполнения: exact RGB/alpha media, once-only enqueue/retry, исправление позднего отзыва верификации02398, сценарии атомарной семьи и статическая трасса renderer/SQL. .08.4 4/5;20,634286%→20,636190%,+0,001905 п.п. Следующий .08.4.5 — итоговая сверка accepted-version/media authority. Тестирование не запускалось.


2026-10-16: .08.4.5 и .08.4 закрыты по кодированию: итоговая статическая трасса и написанные сценарии. .08.4 5/5;.08 4/5;20,636190%→20,638095%,+0,001905 п.п. Далее .08.5 — полный контур запуска. Тесты не запускались.


2026-10-16: .08.5 разбит на пять равных микрошагов без изменения суммарного веса. Закрыт только .08.5.1 — конфигурация/CPU startup/registry и сценарии отказа. .08.5 1/5;20,638095%→20,640000%,+0,001905 п.п. Далее .08.5.2 dispatch/live IO. Ничего не исполнялось.


2026-10-16: .08.5.2 закрыт по кодированию — dispatch/claim/live IO и сценарии поздней потери регистрации. .08.5 2/5;20,640000%→20,641905%,+0,001905 п.п. Далее .08.5.3 execute/result. Ничего не исполнялось.


2026-10-16: .08.5.3 закрыт по кодированию — exact execute/result и сценарии потери активности в callbacks. .08.5 3/5;20,641905%→20,643810%,+0,001905 п.п. Далее .08.5.4 adopter/polling/settlement. Тесты не запускались.


2026-10-16: .08.5.4 закрыт по кодированию — adopter/discovery/retry/settlement. .08.5 4/5;20,643810%→20,645714%,+0,001905 п.п. Далее .08.5.5 итоговый реестр сквозного контура. Тесты не запускались.


2026-10-16: .08.5.5/.08.5/.08 закрыты по кодированию: текущий хешированный реестр реализации/сценариев и границы непроверенного. .08.5 5/5;.08 5/5;20,645714%→20,647619%,+0,001905 п.п. Другие части ретуши открыты. Тесты не запускались.


2026-10-16: .07.3 закрыт по кодированию — exact transport publication/replay/late authority и статическая сверка IO. .07 3/10;20,647619%→20,652381%,+0,004762 п.п. Далее .07.4 admission документов. Тесты не запускались.


2026-10-16: .07.4 закрыт по кодированию — admission документов/ACL/current draft caller. .07 4/10;20,652381%→20,657143%,+0,004762 п.п. Далее .07.5 полный decision batch. Тесты не запускались.


2026-10-16: закрыт .07.5.1: pure decision/task membership, authored alpha/no-alpha single/multiple/mixed/replay и24 refusal-сценария. .07.5 —1/5; .07 —4 целых микрошагa плюс1/5 пятого.20,657143%→20,658095%,+0,000952 п.п. Relational multi-target locks и остальные критерии открыты. Тесты не запускались.


2026-10-16: закрыт .07.5.2: canonical published decision lock, alpha/no-alpha/replay,10 document/scope/revision refusals,2 metadata liveness refusals с rollback restoration и installed ACL/caller-сценарий. .07.5 —2/5;20,658095%→20,659048%,+0,000952 п.п. Тесты не запускались.


2026-10-16: закрыт .07.5.3:6 late selection/session/producer/artifact refusal-сценариев и alpha/no-alpha target progression через настоящий draft/state trigger с rollback restoration. .07.5 —3/5;20,659048%→20,660000%,+0,000952 п.п. Тесты не запускались.


2026-10-16: .07.5.4 в работе: найден и исправлен в forward-only02399 разрыв блокировок output metadata, написаны16 двухсоединительных single-target сценариев. Полный multi-target критерий ещё открыт; .07.5 —3/5, общий процент20,660000% без прибавки. Миграции и тесты не запускались.


2026-10-16: .07.6 — написаны4 alpha/no-alpha сценария exact draft/quota, once-only reservation replay и quota-failure atomic rollback. Публичный idempotency replay и полная матрица конфликтов ещё открыты; процента не добавлено:20,660000%. Тесты не запускались.


2026-10-16: закрыт .07.6 по кодированию: exact draft/quota, rollback,9 reservation conflict/scope scenarios и actual API-role saved-response replay. .07 —5/10 целых микрошагов, .07.5 —3/5.20,660000%→20,664762%,+0,004762 п.п. Multi-target locks, input sealing, atomic commit rollback и полный service контур открыты. Тесты не запускались.


2026-10-16: .07.7 в работе: exact alpha/no-alpha input registration/replay/seal и6 malformed projection refusal-сценариев. Final live metadata/persisted inventory/activation audit ещё открыт;20,664762% без прибавки. Тесты не запускались.


2026-10-16: закрыт .07.7: exact input registration/seal, late metadata/persisted inventory refusals и fail-closed activation preconditions. .07 —6/10 целых микрошагов, .07.5 —3/5.20,664762%→20,669524%,+0,004762 п.п. Тесты не запускались.


2026-10-16: закрыт .07.8: complete own SQL transaction, late cursor-exhaustion rollback across15 table families, same-key retry и API-role saved replay. .07 —7/10 целых микрошагов, .07.5 —3/5.20,669524%→20,674286%,+0,004762 п.п. Тесты не запускались.


2026-10-16: .07.9 в работе:12 typed catalog failures, independent tenants/foreign artifacts и8 mixed/incomplete route refusal-сценариев. Positive resolver/full preparation и startup/capability ещё открыты;20,674286% без прибавки. Тесты не запускались.


2026-10-16: .07.9 — выделены полные общие content fixtures и написаны9 callback-buffer mutation сценариев реального single-target preparation. Full SQL resolver/batch и startup/capability критерии ещё открыты;20,674286% без прибавки. Тесты не запускались.


2026-10-16: уточнение отчётности — каждый новый закрытый микрорезультат публикуется с ID, локальным счётчиком, общим процентом до/после и приростом. Открытая работа не получает субъективного процента. В .07.9 устранён aliasing запроса между callback публикациями; написан сценарий мутаций вложенных preview и target slice с повторными публикациями. Полный критерий .07.9 открыт; 20,674286% без прибавки. Тесты не запускались.


2026-10-16: .07.9 — ранняя проверка точного множества copy budgets реализована до SQL report/physical preview IO; написаны десять отказов и два допустимых порядка. Full SQL resolver/batch и startup/capability остаются открыты. Общий процент 20,674286% без прибавки; тесты не запускались.


2026-10-16: .07.9 — написаны пять сценариев pure service composition с запретом object IO/preview resolution/signing. Live SQL role verification и full SQL resolver/batch остаются открыты; общий процент 20,674286% без прибавки. Тесты не запускались.


2026-10-16: .07.9 — написан согласованный positive full content batch и шесть authority-loss сценариев по всей цепочке подготовки. SQL resolver/live roles ещё открыты; 20,674286% без прибавки. Тесты не запускались.


2026-10-16: .07.9 — написаны семь полных сценариев отказа публикации пакета, включая storage/SHA/artifact identity и поздний decision failure. SQL resolver/live role installation ещё открыты, 20,674286% без прибавки. Тесты не запускались.


2026-10-16: .07.9 — написан интеграционный driver реального SQL/physical resolver, live role installation и full batch publication. Автоматический disposable retouch fixture producer остаётся открытым; driver не запускался, 20,674286% без прибавки.


2026-10-16: закрыт .07.9.1 (own routing/selection/callback ownership): .07.9 1/5, 20,674286%→20,675238%, +0,000952 п.п. Закрыт .07.9.2 (complete content preparation и authored success/failure orchestration): .07.9 2/5, 20,675238%→20,676190%, +0,000952 п.п. .07 — семь целых микрошагов плюс .07.5 3/5 и .07.9 2/5, взвешенно 8/10. SQL/physical fixture, startup/capability и final service trace остаются открыты. Тесты не запускались.


2026-10-16: закрыт .07.9.4 (startup exact retouch commit authority + tenant capability isolation, actual ACL integration runner authored). .07.9 —3/5;20,676190%→20,677143%,+0,000952 п.п. В .07 семь целых микрошагов плюс .5 3/5 и .9 3/5, взвешенно8,2/10. Physical worker fixture и final full-service reproducibility ещё открыты. Тесты и миграции не запускались.


2026-10-16: закрыт .01.5 Python/Go/schema/catalog coherence после устранения missing runtime registrations и написания пяти shared wire сценариев. .01 —4/5;20,677143%→20,686667%,+0,009524 п.п. .01.2, multi-target locks и full physical fixture остаются открыты. Тесты/генераторы не запускались.


2026-10-16: .01.2 разделён на пять равновесных критериев (1/750 родительской единицы каждый), 0/5 закрыты. Найден и исправлен приём неканонического JSON landmarks с совпадающим SHA; написаны отдельные сценарии для Face68 и FaceLandmarks. Каждый будущий закрытый критерий добавит 0,001905 п.п. Общий процент 20,686667% без изменения. Тесты не запускались.


2026-10-16: частично .01.2.1/.2 — исправлены приём неканонических candidate/intent с совпадающим SHA и передача собственных refs в storage callback; добавлены два отказа и четыре сценария изменения ссылок при чтении. Полные критерии ещё открыты:0/5;20,686667% без изменения. Сценарии не запускались.


2026-10-16: закрыт .01.2.1 — точная привязка Params/candidate/intent/context и неизменный тип/SHA каждой ссылки. Исправлен конфликт NPY/landmarks и повторный SHA для одного artifact в worker и сервере; написаны положительный и отказные сценарии. .01.2:1/5; .01:4 целых подшага плюс1/5 последнего.20,686667%→20,688571%,+0,001905 п.п. Тесты не запускались. Далее .01.2.2 — полный declared closure/lease/provenance.


2026-10-16: закрыт .01.2.2 по кодированию и статической трассе declared inputs/lease/face provenance. Написаны11 сквозных сценариев реальных документов,2 отказа неполного face closure и3 отказа inventory. .01.2:2/5;20,688571%→20,690476%,+0,001905 п.п. Тесты не запускались. Далее .01.2.3 — qualification/runtime pins.


2026-10-16: закрыт .01.2.3 по кодированию/статической трассе qualification report/runtime pins. Исправлена проверка обязательных критериев по изменяемой исходной политике; обе проверки используют owned policy. Написаны13 новых сценариев. .01.2:3/5;20,690476%→20,692381%,+0,001905 п.п. Тесты и фотоквалификация не выполнялись. Далее .01.2.4 — SQL admission/task dispatch.


2026-10-16: частично .01.2.4 — написаны5 сценариев реального preview SQL dispatch/claim и сохранности резервов/outbox при отказах. Сверка полного worker admission ещё открыта. .01.2:3/5;20,692381% без изменения. Тесты не запускались.


2026-10-16: .01.2.4 частично — исправлен пропуск live quota/current-policy проверки в прямом compute admission; подготовлена forward-only02400 и10 сценариев действующего claim/admission. Осталась сверка повторной авторизации renderer при claim после dispatch. .01.2:3/5;20,692381% без изменения. Миграция и тесты не запускались.


2026-10-16: закрыт .01.2.4 — SQL admission/dispatch/claim. Подготовлена02401 с повторной locked авторизацией CPU renderer на частном preview участке существующей цепочки claim; сохранён публичный Apply wrapper. Написаны2 сценария отзыва renderer/полного rollback/повторного claim с alpha и без. .01.2:4/5;20,692381%→20,694286%,+0,001905 п.п. Тесты и миграции не запускались. Далее .01.2.5 — полный adapter execution/failure chain.


2026-10-16: частично .01.2.5 — исправлена передача собственных refs во внутреннем executor при чтении документов и открытии числовых входов. Написаны2 сценария изменения ссылок storage callback; добавлена проверка точных Params во всех phase/loss сценариях. .01.2:4/5;20,694286% без изменения. Тесты не запускались.


2026-10-16: закрыт .01.2.5 и родители .01.2/.01 по кодированию/статической трассе полного адаптера. Написаны12 сценариев cleanup/error propagation зарегистрированной операции и4 сценария реального CPU beauty на синтетическом изображении, strength0/.5 и consumer error. .01.2:5/5;.01:5/5;P0.1.09:13 целых частей.20,694286%→20,696190%,+0,001905 п.п. Родитель заменяет свои дроби без повторного зачёта. Тесты, миграции и фотоквалификация не запускались.


2026-10-16: .02 заранее разделён на5 равных критериев. Закрыт .02.1 — полная привязка документа WorkingImage и face geometry в compute. Исправлен пропуск SHA всего source документа, написаны2 отказа подмены pixels/ICC до beauty. .02:1/5;20,696190%→20,705714%,+0,009524 п.п. Тесты не запускались. Далее .02.2 — владение массивами и ресурсные границы.


2026-10-16: закрыт .02.2 — owned numeric arrays и ресурсные границы. Исправлены alpha budget без вычета resident RGB/масок и копирование после callbacks; добавлены2 сценария изменения буферов и4 отказа dtype/shape. .02:2/5;20,705714%→20,715238%,+0,009524 п.п. Тесты и измерение RAM не запускались. Далее .02.3 — алгоритм, strength и face reports.


2026-10-16: закрыт .02.3 — однократный strength/полный face ROI и строгие отчёты. Исправлена проверка только IDs/count: добавлены тип/поля/диапазоны/finite/applied-reason проверки; написаны5 отказов, реальные CPU сценарии расширены на0,25/1 (ещё4 случая). .02:3/5;20,715238%→20,724762%,+0,009524 п.п. Тесты не запускались. Далее .02.4 — точная сохранность пикселей и alpha.


2026-10-16: закрыт .02.4 — побитовая сохранность вне маски/защиты/alpha и независимый запрет изменений при нулевой силе. Написаны отказ malicious strength0,3 signed-zero случая и8 CPU alpha случаев. .02:4/5;20,724762%→20,734286%,+0,009524 п.п. Тесты не запускались. Далее .02.5 — lifetime/поздняя потеря полномочий и итоговая трасса compute.


2026-10-16: закрыт .02.5 и родитель .02 — lifetime/late authority/static compute trace. Написаны9 сценариев реального CPU алгоритма с отменой/lease/admission loss при patch/output/consumer exit и cleanup. .02:5/5;P0.1.09:14 целых частей;20,734286%→20,743810%,+0,009524 п.п. Родитель заменяет дроби без повторного зачёта. Тесты не запускались. Далее .03.5 — подключение publication и полный отказный путь.


2026-10-16: закрыт .03.5 и родитель .03 — подключение publication и полный отказный путь. Написаны5 дополнительных storage/late-admission сценариев. .03:5/5;P0.1.09:15 целых частей;20,743810%→20,753333%,+0,009524 п.п. Родитель заменяет дроби без повторного зачёта. Тесты не запускались. Далее .04 — backend admission/ресурсы/quota.


2026-10-16: .04 разделён на5 критериев. Закрыт .04.1 — ресурсный компилятор: исправлена недооценка64 одновременных face delta/weight scratch-пар, Go и forward-only02402 SQL согласованы. Написаны5 граничных случаев с точным расчётом и one-byte-short отказом; исправлен устаревший RAM интеграционного fixture. .04:1/5;20,753333%→20,762857%,+0,009524 п.п. Тесты и миграции не запускались. Далее .04.2 — scoped review/source preparation.


2026-10-16: .04.2 scoped review/source preparation закрыт coding_closed_unverified, .04 2/5; 20,762857% → 20,772381%, +0,009524 п.п. Тесты не запускались. Текущая формула: `(14 + (15 + 2/5 + (7 + 3/5 + 3/5)/10)/30)/70*100`. Следующий шаг .04.3 quota/profile.


2026-10-16: .04.3 quota/profile закрыт coding_closed_unverified; .04 3/5; 20,772381% → 20,781905%, +0,009524 п.п. Текущая формула: `(14 + (15 + 3/5 + (7 + 3/5 + 3/5)/10)/30)/70*100`. Тесты не запускались. Следующий шаг .04.4.


2026-10-16: .04.4 publication/activation закрыт coding_closed_unverified, .04 4/5; 20,781905% → 20,791429%, +0,009524 п.п. Формула: `(14 + (15 + 4/5 + (7 + 3/5 + 3/5)/10)/30)/70*100`. Тесты не запускались. Следующий .04.5.


2026-10-16: .04.5 и parent .04 закрыты coding_closed_unverified; 5/5, P0.1.09 whole16/30. 20,791429% → 20,800952%, +0,009524 п.п. Формула: `(14 + (16 + (7 + 3/5 + 3/5)/10)/30)/70*100`. Тесты не запускались.


2026-10-16: .05.1 reader закрыт coding_closed_unverified, .05 1/5; 20,800952% → 20,810476%, +0,009524 п.п. Формула `(14 + (16 + 1/5 + (7 + 3/5 + 3/5)/10)/30)/70*100`. Тесты не запускались. Следующий .05.2 sidecar linkage.


2026-10-16: .05.2 linkage закрыт coding_closed_unverified, .05 2/5; 20,810476% → 20,820000%, +0,009524 п.п. Формула `(14 + (16 + 2/5 + (7 + 3/5 + 3/5)/10)/30)/70*100`. Тесты не запускались. Следующий .05.3 physical image/display.


2026-10-16: .05.3 physical reader закрыт coding_closed_unverified, .05 3/5; 20,820000% → 20,829524%, +0,009524 п.п. Формула `(14 + (16 + 3/5 + (7 + 3/5 + 3/5)/10)/30)/70*100`. Тесты не запускались. Следующий .05.4.


2026-10-16: .05.4 signed response закрыт coding_closed_unverified, .05 4/5; 20,829524% → 20,839048%, +0,009524 п.п. Формула `(14 + (16 + 4/5 + (7 + 3/5 + 3/5)/10)/30)/70*100`. Тесты не запускались. Следующий .05.5.


2026-10-16: .05.5 и parent .05 закрыты coding_closed_unverified, 5/5. 20,839048% → 20,848571%, +0,009524 п.п. Формула `(14 + (17 + (7 + 3/5 + 3/5)/10)/30)/70*100`. Тесты не запускались. Следующий .06.


2026-10-16: закрыт .06.1 physical receipt sealing по кодированию и статической сверке; .06 — 1/5. Общий процент 20,848571% → 20,858095%, +0,009524 п.п. Следующий .06.2 — точный контракт показанного результата и явный выбор фотографа. Тесты не запускались.


2026-10-16: .06.2 exact shown-selection contract закрыт по кодированию: .06 2/5, общий 20,858095% → 20,867619%, +0,009524 п.п. Написаны 35 сценариев, не запускались. Далее .06.3 — транзакционная SQL authority выбора.


2026-10-16: .06.3 transactional selection закрыт по кодированию и статической трассе; .06 3/5. Общий 20,867619% → 20,877143%, +0,009524 п.п. Дополнены девять full-transaction rollback/retry сценариев без запуска. Следующий .06.4 — владение показанным результатом на клиенте.


2026-10-16: .06.4 client shown-result ownership закрыт по кодированию, .06 4/5; 20,877143% → 20,886667%, +0,009524 п.п. Написаны 13 клиентских сценариев без запуска. Далее .06.5 service/HTTP trace.


2026-10-16: .06.5 service/HTTP trace закрыт по кодированию. .06 целиком 5/5 учитывается один раз; 18 целых частей P0.1.09 и 0.82 дробных. 20,886667% → 20,896190%, +0,009524 п.п. Тесты не запускались. Далее незавершённая подготовка Apply (.07).


2026-10-16: .07.5.4 multi-target concurrency закрыт по кодированию и authored сценариям; .07.5 4/5, .07 7 целых микрошагов и 1.4 дробных. 20,896190% → 20,897143%, +0,000952 п.п. Написаны 30 конкурентных сценариев, не запускались. Далее .07.5.5 итоговая authority trace.


2026-10-16: .07.5.5 итоговая authority/ACL trace закрыта по кодированию; .07.5 5/5 учитывается один раз, .07 8 целых микрошагов плюс .07.9 3/5. 20,897143% → 20,898095%, +0,000952 п.п. Усилен startup ACL gate, написаны 13 ACL corruption/recovery сценариев, не запускались. Далее .07.9.3 воспроизводимый SQL/physical resolver fixture.


2026-10-16: закрыт .07.9.3 coding_closed_unverified, воспроизводимый физический CPU→Go resolver fixture; .07.9 4/5, .07 взвешенно8.8/10;20,898095%→20,899048% (+0,000952 п.п.). Без запуска тестов.


2026-10-16: закрыт .07.9.5 и .07.9 coding_closed_unverified; .07 9/10;20,899048%→20,900000% (+0,000952 п.п.). Полный сервисный сценарий и проверка неизменности replay написаны, не запускались.


2026-10-16: .07.10 разделён без увеличения веса; закрыт только .07.10.3 после исправления startup ACL overload inventory и написания18 refusal cases.20,900000%→20,900952%. Без запуска тестов.


2026-10-16: закрыт .07.10.1 coding_closed_unverified после согласования revision client/Go/SQL и карты preview→selection→HTTP. .07.10 2/5;20,900952%→20,901905%. Сценарии не запускались.


2026-10-16: закрыт .07.10.2 coding_closed_unverified; дополнен startup inventory input registration, исправлен queued expectation, зафиксирована карта publication→draft→quota→seal→activation. .07.10 3/5;20,901905%→20,902857%. Без запуска тестов.


2026-10-16: закрыт .07.10.4 coding_closed_unverified; .07.10 4/5;20,902857%→20,903810%. Добавлены cancelled replay и полный снимок19 таблиц; тесты не запускались.


2026-10-16: закрыт .07.10.5/.07.10/.07 coding_closed_unverified после итоговой сверки source registry;19/30 частей P0.1.09.20,903810%→20,904762%. Исполнение тестов остаётся отложено.

### Мелкий учёт следующего пункта `.09`

Зафиксированы пять равновесных результатов: регистрация операций; готовность при старте; маршрутизация и оборудование; доступность для tenant и клиента; итоговая сверка связей. Каждый имеет отдельный критерий в JSON-реестре и добавляет **0,009524 процентного пункта** после закрытия. Текущий счётчик: **0/5**. При необходимости каждый результат можно дополнительно разделить до начала его зачёта с сохранением веса. Само дробление не меняет текущие **20,904762%**.

2026-10-16: закрыт `.09.2` coding_closed_unverified — startup-конфигурация и точные SQL ABI/owner/definer/search_path/ACL проверки до установки retouch; unit и 67 integration-сценариев написаны, не запускались. Участок `.09`: **1/5**. **20,904762% → 20,914286% (+0,009524 п.п.)**. Далее `.09.1`: точная регистрация preview и Apply-copy.

2026-10-16: `.09.1` coding_closed_unverified — точная регистрация двух CPU/version-1 операций, защита от несогласованной установки, authored catalog/executor/forwarding/refusal scenarios. `.09`: **2/5**; **20,914286% → 20,923810% (+0,009524 п.п.)**. Далее `.09.3`: маршрутизация и оборудование. Тесты не запускались.

2026-10-16: закрыт `.09.3` coding_closed_unverified — pre-IO CPU routing, текущая SQL цепочка dispatch/claim/live IO и 12 late-compatibility rollback/recovery scenarios; без выполнения. `.09`: **3/5**; **20,923810% → 20,933333% (+0,009524 п.п.)**. Далее `.09.4`: tenant/client capability.

2026-10-16: `.09.4` coding_closed_unverified — исправлена отсутствовавшая retouch capability map, собственные stage/refusal и delayed-journal availability scenarios написаны; server tenant installation chain сверена. `.09`: **4/5**, **20,933333% → 20,942857% (+0,009524 п.п.)**. Тесты не запускались; далее `.09.5` итоговый реестр.

2026-10-16: закрыт `.09.5` и весь `.09` coding_closed_unverified — сохранён актуальный реестр исходников и отложенных проверок, каталоги совпадают. `.09`: **5/5**; **20,942857% → 20,952381% (+0,009524 п.п.)**. Далее `.10`: полный охват unit-сценариев и отказов. Исполнение тестов отложено.

2026-10-16: `.10` заранее разделён на пять равных критериев. Закрыт только `.10.1` coding_closed_unverified — lifecycle/error taxonomy обеих зарегистрированных операций; 28 authored combinations, не выполнялись. `.10`: **1/5**, **20,952381% → 20,961905% (+0,009524 п.п.)**. Далее `.10.2`: входы/контекст/полномочия.

2026-10-16: закрыт `.10.2` coding_closed_unverified — full-index copy defect исправлен, input/context/authority matrix и index-loss scenarios написаны. `.10`: **2/5**. **20,961905% → 20,971429% (+0,009524 п.п.)**. Тесты не запускались. Далее `.10.3`: пиксели и ресурсы.

2026-10-16: `.10.3` coding_closed_unverified — сохранена pixel/resource matrix, численные/побитовые и cancellation scenarios дополнены, тесты не выполнялись. `.10`: **3/5**. **20,971429% → 20,980952% (+0,009524 п.п.)**. Далее `.10.4`: SQL atomicity/retries.

2026-10-16: закрыт `.10.4` coding_closed_unverified — SQL rollback/replay assertions расширены до 19 таблиц; atomicity/retry matrix сохранена. `.10`: **4/5**, **20,980952% → 20,990476% (+0,009524 п.п.)**. Тесты не запускались; далее `.10.5` клиент/итоговый охват.

2026-10-16: `.10.5` и `.10` coding_closed_unverified — retouch client rejection/uncertain replay scenarios дополнены, текущий source/failure inventory сохранён. `.10`: **5/5**, **20,990476% → 21,000000% (+0,009524 п.п.)**. P0.1.09 **21/30**. Тесты не запускались; далее remove_object source audit.

2026-10-16: remove_object `.03` заранее разбит на пять равных частей; `.03.1` coding_closed_unverified — допуск/полный budget до compute, повторные проверки вокруг document IO, authored отказные сценарии. `.03`: **1/5**. **21,000000% → 21,009524% (+0,009524 п.п.)**. Тесты не запускались; далее WorkingImage/pixels публикация.


2026-10-16: частичный .09.3 — добавлен собственный big_lama backend startup gate, автоматическое исключение unsupported MPS removal executor и 28 authored сценариев. Полный критерий маршрутизации/ресурсов ещё открыт. Процент без изменения: 21,114286%; .09 — 2/5. Тесты не запускались.

2026-10-16: частичный .09.3 — startup теперь сверяет реальные файлы big_lama и model lock SHA до регистрации/installer IO; четыре дополнительных сценария написаны. Полный критерий открыт; 21,114286%, участок 2/5, без запуска тестов.

2026-10-16: .09.3 закрыт по кодированию/статической трассе; семь build/policy сценариев написаны. Участок3/5; 21,114286% → 21,123810% (+0,009524 п.п.). Тесты не запускались.

Частичный .01.2: исправлен Go/Python mismatch безопасной верхней границы ROI; схемы и собственные сценарии обновлены. .01 —1/5, 21,152381% без изменения. Тесты не запускались.


2026-10-17: **57,142857% → 58,571429% (+1,428571 п.п.)**. P0.6.10 закрыт по кодированию после независимого source review: preflight совместим с историческими Down, production вызывает только Up, итоговый каталог проверяет владельцев/search_path/PUBLIC EXECUTE. Написаны fresh/upgrade и сохранность tenant/ACL сценарии; baseline функция обязана существовать. P0.6:5/10; всего41/70. Запуски миграций, тестов и сборок отложены.


2026-10-17: **58,571429% → 60,000000% (+1,428571 п.п.)**. P0.6.03 закрыт по кодированию после независимого просмотра per-connection роли/login, transitive owner/admin privileges и сохранения initializer chain. Go/Python runtime connection guards и существенные tenant/role/reconnect сценарии написаны; проверок исполнения не было. P0.6:6/10, всего42/70.
