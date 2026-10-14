# Пересчёт реализации до тестирования — 2026-10-09

**Актуальное состояние (2026-10-10):** 90 входных точек: 1 missing_runtime_path, 16 partial_runtime_path, 73 requires_semantic_review. Наличие runtime-кода: 89/90 = 98,89%; это не процент реальной реализации. Полнота всей реализации не установлена; исторические цифры ниже не являются текущей оценкой.


Тесты, сборки и миграции не запускались. Это статическая инвентаризация реализации, отдельная от приёмки.

Актуальный function-index содержит **90 entrypoints** (старый аудит использовал87). Из них **17 имеют незавершённый/отсутствующий runtime путь**: V30 delivery/interchange, V31 signer и V29 perspective/panorama/focus_stack. Поэтому даже при полном выполнении остальных пунктов потолок покрытия этого каталога составляет **81.1%**. Это верхняя граница каталога, а не оценка фактической готовности продукта.

Остальные 73 пунктов требуют семантической сверки маршрута, сервиса, SQL, worker, adoption, клиента и unit-тестов. Частичный код V28 не равен завершённому модулю. Вспомогательные функции, инварианты и требования V20/V32 в каталоге entrypoints полностью не представлены. Поэтому реальный процент всей реализации пока **не установлен**, и указанную верхнюю границу нельзя публиковать как процент готовности.

Машиночитаемый реестр: implementation-progress-2026-10-09.json. Для каждого пункта оставлены исходные ссылки на нормативные документы. Баллы не начисляются по количеству файлов, миграций или наличию имени функции.

Подтверждённые открытые участки: V28 завершение сквозного automatic depth/person сценария и production qualification (SQL transport/registration/seal/admission и начальная editor integration уже добавлены, но не проверены); V29 три advanced optical runtime; V30 полный delivery/interchange runtime; V31 production signer. Эти разрывы остаются в исходном объёме работы.

## Повторная сверка текущего продолжения

Просмотрены актуальные work-packages, function-index, предыдущий реестр и текущие perspective source/task/qualification компоненты. Наличие perspective worker и backend-компиляторов не закрывает entrypoint: durable publication/seal/admission/recovery, auto backend planning и публичный клиентский путь ещё не закончены. Поэтому ранее выявленные 17 незавершённых путей остаются незакрытыми; оснований увеличить верхнюю границу 73/90 = 81,1% нет. Остальные 73 пути всё ещё не прошли полную семантическую сверку; точный процент реализации не рассчитан. Это ограниченный повторный аудит, не полная новая инвентаризация требований V20–V32.

Продолжение: добавлены физический потоковый verifier source NPY и coordinator повторной проверки версий. Concrete SQL source-array catalog и включение verifier в admission ещё требуются. Все новые изменения и unit-тесты NOT RUN.

## Повторная сверка после публичного perspective-пути

Добавлены deployment dispatch, HTTP, durable client attempt, ручные corner controls и drag canvas. Старое описание отсутствующих API/client устарело для ручного perspective. Однако auto planning/line producer, полный review/Apply и производственная квалификация не закрыты: entrypoint остаётся частичным. Арифметическая верхняя граница прежнего каталога: (90−17)/90×100 = 81,1%. Это НЕ фактический процент реализации: остальные 73 пункта не получили полного семантического зачёта. Полный процент сохраняется неизвестным; прежние 55% не восстанавливаются. Проверки не запускались; исторические PASS не относятся к текущему дереву. Основные незавершённые направления: V29 multi-frame/auto, V30 delivery/interchange, V31 signer, V32 lifecycle/restore/capacity; также сквозная полнота V20–V28.


## Текущая статическая сверка после adopted perspective API

Точный процент всей реализации до тестирования по-прежнему **не установлен**. Старый каталог:90 пунктов,17 незакрытых,73 не получили полного семантического зачёта; `(90−17)/90×100 = 81,1%` — только его арифметическая верхняя граница. Новые вспомогательные API/компоненты не повышают процент автоматически. Исторические55% в completion-review-current.json отозваны, completion-assessment.md помечен историческим.

Уточнение V29: теперь присутствуют source-bound публикация явно заданных линий, auto/manual подготовка и durable admission-код, recovery, клиентский журнал/редактор направляющих и scoped GET принятого adopter-ом результата. Это написанная, НЕ проверенная реализация. Автоматический детектор линий отсутствует; пользовательское подтверждение/Apply с новым canvas и визуальный preview не завершены. Same-geometry composition не подходит для изменяющего canvas perspective. Panorama/focus_stack, V30 delivery/interchange, V31 signer и V32 завершающая интеграция остаются открытыми; полнота V20–V28 требует дальнейшей семантической сверки.

Продолжение: добавлен строгий клиентский parser perspective proposal и OwnerAPI GET, с привязкой к tenant/project/task/accepted base, проверками статуса, canvas, artifact-role aliases и identity. Юнит-тесты написаны, NOT RUN. Тестирование, сборки, typecheck и миграции не запускались.


## Повторный расчёт после encoded preview adoption

Фактический процент всей реализации не определён: знаменатель полного перечня требований и семантический зачёт реализации остаются неполными. Старый ограниченный каталог:90 входных точек,17 незавершённых,73 без полного зачёта;81,1% — его верхняя граница, не процент продукта. Новая приёмка encoded perspective preview не закрывает V29: coordinator ещё не подключён, отсутствуют завершённые finalization/cleanup/UI preview/Apply, automatic line detection, panorama/focus_stack. В этом продолжении прочитаны актуальные контракты исполнения/приёмки, реестр пакетов и текущий код preview; это ограниченная статическая сверка, не полный повторный аудит всех модулей. Новые unit-тесты написаны, NOT RUN; миграции не применялись. Оснований повысить подтверждённую готовность нет.

## Продолжение: lifecycle perspective preview

Написаны success finalization/recovery и failure/stale-source/quota reconciliation, ограниченные страницы обработки и unit-тесты. Код ещё не подключён к рабочему scheduler/adopter, миграции01073–01076 не применены, тесты NOT RUN. Это продвижение реализации внутри незакрытого V29, а не закрытие модуля. Точный процент всей системы остаётся неопределённым;81,1% нельзя использовать как фактическую готовность.

## Продолжение: подключение preview maintenance к adopter

Добавлен рабочий путь вызова lifecycle/receipt/completion runner в Adopter.run_once при установленном perspective runtime и доверенных ICC; startup проверяет SQL ABI. Глобальный scheduler-only поиск возвращает один tenant UUID, дальнейшая обработка ограничена tenant-scoped транзакциями и страницами32. Это написанное подключение, не доказательство запущенной системы: тесты NOT RUN,01077 NOT APPLIED. Go scheduler ещё должен создавать preview-планы; UI preview/Apply и оставшийся V29 не закрыты. Общий процент не пересчитан в числовую оценку из-за неполного семантического зачёта требований.

## Продолжение: Go scheduler создаёт perspective preview

Написано подключение producer discovery/Prepare к отдельному циклу scheduler с startup-gate, readiness, shutdown и счётчиками. Draft intent после частичной публикации остаётся кандидатом на повтор; завершённый/sealed preview повторно не создаётся для той же пары result/ICC. Unit-тесты написаны, NOT RUN;01078 NOT APPLIED. Это закрывает ранее отсутствовавший путь вызова планирования в исходном коде, но не подтверждает сквозную работоспособность. UI preview/Apply и остальные требования V29 остаются открытыми. Численный процент всей реализации по-прежнему не установлен.


## Текущий пересчёт после quality admission

Полный процент реализации **не установлен**. Пересчитана арифметика прежнего каталога: 90 пунктов, 17 незакрытых, 73 без полного семантического зачёта; верхняя граница 81,1%. Это ограниченная статическая сверка, а не новый полный аудит требований. Новых полных зачётов нет. Наличие написанных unit-тестов не засчитывается как результат их выполнения.

В V29 добавлены preview runtime/API/client и quality intent/quota/Params, а теперь blocked materialization и witness seal. Не закончены quality adoption/installation, явный review/Apply для нового canvas, автоматический детектор линий, panorama/focus_stack; также остаются V30 delivery/interchange, V31 production signer и V32 завершение lifecycle/restore/deployment. Полнота V20–V28 требует семантического аудита. Тесты, сборки, typecheck и миграции НЕ запускались.

## Актуализация каталога после повторного чтения runtime — 2026-10-09

Исправлен устаревший статус optical.perspective.execute: условный путь установки реально присутствует в runtime/service.py и perspective_installation.py. Статус partial_runtime_path, полного зачёта нет. В каталоге90 пунктов теперь16 missing_runtime_path,73 requires_semantic_review и1 partial_runtime_path. Арифметическая верхняя граница каталога82,2%; прежние81,1% устарели. **Это не процент реальной реализации продукта.** Общий процент остаётся не установлен: полный знаменатель требований и семантический зачёт отсутствуют. Новых полностью закрытых требований в этом пересчёте нет.

Продолжена реализация V29: CPU admission поиска линий до pixel IO,200MP ceiling, учёт float32 alpha/mmap/download и detector workspace, нулевой VRAM, отдельные RAM/scratch/output отказы. Написаны unit-тесты admission, source/canvas/input-order/ICC preflight и отмены. NOT RUN. Factory поиска линий ещё не подключена к runtime/registry/backend/client. Тесты, сборки, typecheck и миграции не запускались.

## Текущее продолжение: политика автоматического детектора

Статически обнаружено и исправлено несоответствие: draft line-detection использовал default continue_items, тогда как V2 workflow требует fail_fast. В ещё не применённой01104 явно установлена fail_fast, проверяется replay и запрещается смена политики. Добавлены owned JCS execution-policy compiler и unit-тесты: runtime SHA, ICC, CPU/float32, v2.2, 200MP, proxy1600,128lines. Это фиксация исполнения, не квалификация качества; SQL workflow persistence/registration binding и runtime installation ещё не завершены. Новых полностью закрытых требований не начислено. Тесты/сборки/typecheck/миграции NOT RUN.

## Продолжение: immutable workflow автоматического поиска линий

Добавлены01117 и Go-адаптер создания V2 workflow с immutable original execution-policy, source IO witness recheck и SHA политики. Dedicated registration теперь требует соответствие workflow сохранённой политике. Unit-тесты написаны, NOT RUN; миграция NOT APPLIED. Seal/runtime/adoption/API/UI/scheduler ещё не завершены. Статус каталога не изменён:16 отсутствующих,1 частичный,73 требуют сверки;82,2% только потолок каталога, реальный процент неизвестен.

## Продолжение: transport seal V29

Написаны01118 и Go-адаптер фиксации typed closure/wire manifest+Params/profile на blocked-задаче, с повторной source/policy authority и SQL readiness gate. Registry остаётся disabled, запуск графа не добавлен. Replay/recovery уже sealed-задачи и последующие activation/runtime/adoption/UI остаются незавершёнными. Unit-тесты написаны, NOT RUN; миграции NOT APPLIED. Полностью закрытых требований не прибавлено, реальный процент остаётся неизвестным.

## Продолжение: независимый OpenCV runtime-lock

Написан отдельный lock автоматического детектора с проверкой версий/байтов/загруженного cv2 и bundled native libraries; correction runtime не изменён. Unit-тесты написаны, NOT RUN. Подключение lock к factory/worker, seal recovery/activation/adoption/UI остаётся открытым. Процент всей реализации не установлен; новых полностью закрытых модулей нет.

## Продолжение: pinning factory и CPU worker

Добавлена явная установка детектора в build_worker/run_worker с проверкой CPU и рабочего ICC. Factory владеет lock bytes и до artifact IO проверяет pin runtime/policy, повторяет runtime verification и cancellation. В01118 добавлен execution_policy_sha; lock включает код installation. Unit-тесты написаны, NOT RUN; миграции NOT APPLIED. Registry отключён, recovery/activation/adoption/UI и environment loader ещё открыты. Реальный процент реализации неизвестен; зачёта полного модуля нет.

## Продолжение: deployment config и CLI детектора

Добавлен bounded SHA-pinned canonical config loader и подключение к CLI через PHOTO_PERSPECTIVE_LINES_ENABLED/CONFIG/CONFIG_SHA. По умолчанию отключён; проверяет CPU, runtime и ICC. Unit-тесты написаны, NOT RUN. Registry не включён, миграции не применены. Recovery/activation/adoption/UI ещё открыты; общий процент реализации не установлен.

## Продолжение: receipt и повтор seal

Добавлены01119 durable immutable receipt и повторный вызов для blocked-задачи с exact proof/witness/profile сверкой. Receipt подтверждает сохранённый seal, не разрешение на запуск; свежая source authority перед activation ещё обязательна и не реализована. Unit adapter-тесты написаны, NOT RUN;01119 NOT APPLIED. Registry отключён. Полный recovery coordinator/activation/adoption/UI открыты, общего процента реализации пока нет.

## Продолжение: свежая source authority после seal

Добавлены01120 и transaction-only Go helper: актуальные principal/snapshot/source, RGB/alpha версии, dense provenance, квота и control artifacts. Gate не активирует задачу; будущая activation обязана вызвать его в той же транзакции. Unit-тесты написаны, NOT RUN;01120 NOT APPLIED. Registry/adoption/UI/scheduler открыты. Общий процент реализации не установлен.

## Продолжение: dedicated activation

Добавлены01121/Go activation adapter: source gate в общей транзакции, exact single-task graph и делегирование существующему V2 seal/outbox/events. Registry остаётся disabled до adoption; запуска задачи не было. Unit-тесты написаны, NOT RUN;01121 NOT APPLIED. Recovery coordinator/adoption/UI/scheduler открыты. Реальный процент всей реализации не установлен.

## Продолжение: pixel replay результата детектора

Написан pinned verifier повторного вычисления линий из verified source NPY, exact bytes/SHA сравнение available/unsupported, memory gates и проверки runtime до/после IO. Unit-тесты написаны, NOT RUN. SQL task/attempt/fence authority, receipt/persistence/adoption/UI ещё открыты. Общий процент реализации не установлен.

## Продолжение: adoption task/fence evidence

Написаны01122 и bounded Python evidence loader/checkpoint: exact Params/root/result/attempt/fence, execution policy и текущий исходник. Исправлена совместимость input Params role=None с действующим DB-reader; SQL typed/artifact role требования сохранены. Unit-тесты написаны, NOT RUN;01122 NOT APPLIED. Final receipt/write, array authority, adoption integration/UI ещё открыты. Реальный процент реализации не установлен.

## Продолжение: array authority и adoption preparation

Расширен01122 текущими RGB/alpha locators с seal-сверкой. Написан preparation snapshot→checkpoint→pinned pixel replay→checkpoint с owned evidence/result bytes. Unit-тесты array locations написаны, NOT RUN; миграции NOT APPLIED. Final SQL write и подключение adopter ещё открыты, registry disabled. Общий процент реализации не установлен.

## Продолжение: финальный adoption commit

Добавлены01123 immutable receipt/dedicated commit/generic bypass guard и Python statement builder с exact SHA/task/fence проверкой. Unit-тесты написаны, NOT RUN;01123 NOT APPLIED. Coordinator, retry recovery, contract registry/catalog/UI ещё открыты; registry disabled. Реальный процент реализации не установлен.

## Продолжение: общий contract registry и coordinator

Добавлены schemas/operation metadata, обновлён schema pin с сохранением предыдущего,01124 Params discovery и ветка coordinator replay→closure→dedicated commit. Explicit adopter installation валидирует runtime/scratch/лимиты; unit-тесты написаны, NOT RUN. Service/environment propagation, retry recovery/catalog/UI/scheduler ещё открыты. Миграции NOT APPLIED, SQL registry disabled. Общий процент реализации не установлен.

## Продолжение: adopter deployment wiring

Написаны environment loader, startup SQL ABI gate и CLI→service→coordinator propagation детектора, с pinned runtime/ICC и safe resource limits. Unit-тесты написаны, NOT RUN. Registry disabled, миграции NOT APPLIED; retry recovery/catalog/API/UI/scheduler открыты. Реальный процент реализации не установлен.

## Продолжение: durable adoption recovery

Написаны01125 read-only outcome recovery и coordinator preflight; receipt01123 расширен digest/Params для exact direct commit replay. Recovery сверяет task/fence/receipt/inbox/event без повторного IO. Unit-тесты написаны, NOT RUN; миграции NOT APPLIED. Catalog/API/UI/scheduler/lifecycle открыты; registry disabled, реальный процент неизвестен.

## Продолжение: adopted lines read

Добавлены01126 authorized receipt read и strict Go parser available/unsupported с SHA/source-version/line bounds. Unit-тесты написаны, NOT RUN;01126 NOT APPLIED. Service/HTTP/UI/correction flow ещё открыты, registry disabled. Реальный процент реализации не установлен.

## Продолжение: adopted detector GET

Написаны service и no-store GET версии/producer результата детектора, подключены server/main. Scoped SQL authorization и strict result parser сохранены. Unit-тесты написаны, NOT RUN; миграции NOT APPLIED. Submission/UI/scheduler/lifecycle открыты, registry disabled. Реальный процент всей реализации не установлен.

## Продолжение: клиент результата детектора

Написаны strict descriptor parser, OwnerAPI GET и available→editable line publication intent. Unit-тесты написаны, NOT RUN. Editor UI/submission/scheduler/lifecycle открыты; registry disabled, общий процент неизвестен.

## Продолжение: conditional editor handoff

Добавлена загрузка найденных линий в editable controls при переданном producer, с cancellation/scope fencing и RU/EN/BG labels. Parent пока не получает producer через submission; полный UI flow не закрыт. Unit-тесты написаны, NOT RUN; registry disabled. Реальный процент неизвестен.

## Пересчёт и продолжение: draft orchestration детектора

Повторно прочитаны текущие execution contracts, module inventory, progress registry и реализации V29 workflow/intent/quota/publication/source/seal/activation. Полный нормативный аудит V20–V32 в этом проходе не выполнен. Текущий реестр: 90 entrypoints, 16 missing, 1 partial, 73 требуют semantic review. Арифметическая верхняя граница каталога: 74/90 = 82,22%; это НЕ процент реальной реализации всей системы. Реальный процент остаётся неизвестным; новых полностью зачтённых модулей нет.

Добавлен PerspectiveLinesPreparation.Prepare: preflight dependencies/intent/policy → save draft → reserve quota → publish Params → materialize blocked task → publish closure → verify source RGB/alpha → freeze workflow policy. PreparedPerspectiveLinesDraft хранит private capability для отдельного Seal; preparation не активирует graph и не включает registry. Ошибки сохраняют causal stage и не освобождают квоту под потенциально параллельной публикацией. Retry после seal требует отдельного durable recovery пути и не объявляется реализованным. Написаны unit tests отказов/cancellation/пустой capability, NOT RUN. Это не полный тестовый цикл orchestration.

Открыты: submission и parent UI producer, recovery scheduler после seal, terminal lifecycle/quota cleanup, activation readiness; panorama/focus stack, V30 delivery/interchange, V31 production signer, V32 эксплуатационные контуры и полный semantic audit других модулей. Tests/builds/typecheck NOT RUN; migrations NOT APPLIED.

## Продолжение: sealed scheduler recovery

Написаны01127 read-only descriptor и ResumeSealed: exact saved intent/task reconstruction, deployment policy match, fresh source/NPY IO, existing transactional activation; sealed documents не перепубликуются. Unit tests написаны, NOT RUN;01127 NOT APPLIED. Это adapter, автоматический scheduler loop/discovery ещё не подключён. Submission/parent UI/lifecycle также открыты; registry disabled. Новых полностью зачтённых модулей нет; реальный процент всей реализации неизвестен,82,2% остаётся лишь ранее установленным потолком90-entrypoint каталога.

## Продолжение: discovery и recovery dispatcher

Написаны01128 bounded keyset discovery и ExpansionJob-compatible dispatcher с продвижением cursor после ошибок, wrap и timeout. Написаны unit tests, NOT RUN;01128 NOT APPLIED. Startup/config/source pool/readiness/loop wiring ещё открыты; registry disabled. Полных новых зачётов нет, реальный процент реализации неизвестен.

## Продолжение: scheduler service wiring

Recovery dispatcher подключён к scheduler startup/config/readiness/loop/shutdown; runtime pin задан явно, source role сохранена photo_scheduler. Написаны unit tests env/dependencies/cancellation и deployment notes, NOT RUN. Миграции NOT APPLIED; registry disabled. Submission/UI/lifecycle ещё открыты. Реальный общий процент неизвестен; новых полностью зачтённых модулей нет.

## Продолжение: detector lifecycle

Написаны01129 successful receipt-backed finalization и terminal quota pass, Go processor и scheduler loop/readiness/startup gates. Unit tests написаны, NOT RUN;01129 NOT APPLIED. Это не полный lifecycle audit: failed/cancelled transitions, abandoned draft cleanup, submission/parent UI открыты. Реальный процент неизвестен, новых полных зачётов нет.

## Продолжение: generic finalization guard и отказ детектора

Выявлен и закрыт кодом конфликт generic V2 quota finalization:01130 выделяет flagged line runs, добавляет terminal failure + atomic run events/outbox без раннего release. Success01129 также дополнен событиями и revision/cursor bounds. Lifecycle processor/tests/startup gate обновлены. Tests NOT RUN; migrations NOT APPLIED. Полных новых зачётов нет, реальный процент неизвестен; cancellation/abandoned drafts/submission/UI остаются в охвате.

## Продолжение: abandoned draft expiry

Написан01131 wrapper generic expiry для lines runs и bounded next expiry, сохранение run reservation до settlement, cancellation events/outbox; lifecycle/startup wiring и unit tests обновлены. Tests NOT RUN;01131 NOT APPLIED. Submission/UI/concurrency/physical cleanup verification остаются открыты. Реальный процент неизвестен; новых полностью зачтённых модулей нет.

## Продолжение: submission preparation

Написаны bounded strict detector intent, stable run key derivation и server-authorized accepted-source→blocked preparation. Unit tests написаны, NOT RUN. HTTP/queued receipt/idempotency records, session reauthorization и parent UI остаются открыты. Реальный процент неизвестен; новых полных зачётов нет.

## Продолжение: submission state и retry

Добавлены01132 authorized state reader и submission orchestration с post-seal recovery, exact request conflict и admitted-only receipt. Unit tests написаны, NOT RUN;01132 NOT APPLIED. HTTP/deployment/client/idempotency record integration открыты, registry disabled. Реальный общий процент неизвестен, новых полных зачётов нет.

## Продолжение: detector HTTP/deployment

Добавлены POST route, scoped receipt validator и API deployment builder/main pool lifetime. Unit tests написаны, NOT RUN. API flag explicit; registry disabled, migrations NOT APPLIED. Request budget propagation/durable response idempotency/client scenario остаются открытыми. Реальный процент неизвестен, full module credit не добавлен.

## Продолжение: request timeout/client submit

Добавлены full-chain submission timeout с API config propagation и client OwnerAPI POST/strict parsers/cancellation. Unit tests написаны, NOT RUN. Exact durable response idempotency и editor producer handoff остаются открыты. Общий процент неизвестен, новых полных зачётов нет.

## Продолжение: editor detector action

Добавлены Detect→producer→Load editable lines и persist-before-POST retry journal, unit tests NOT RUN. Reload snapshot recovery, polling/status и exact durable response idempotency открыты. Реальный процент неизвестен, полных новых зачётов нет; registry disabled.

## Продолжение: reload snapshot recovery

Editor читает scoped detection journal перед созданием snapshot и восстанавливает saved snapshot только для exact source/canvas/version. Добавлены strict parser/cancellation и unit tests, NOT RUN. Status polling/producer recovery/expired-draft restart и server exact idempotent response ещё открыты. Реальный общий процент неизвестен.

## Продолжение: persistent producer recovery

Добавлены schema2 local admitted receipt journal и editor startup task recovery, backward schema1; retry после receipt не делает POST. Unit tests написаны, NOT RUN. Server GET authority сохранена; polling/terminal UX/server response idempotency открыты. Реальный общий процент неизвестен; новых полных зачётов нет.

## Продолжение: detector status UI

Написаны workflow-scoped status observer и bounded editor polling/status labels, cancellation/scope fencing; unit tests NOT RUN. Polling wall-clock/per-request bounds и terminal restart/server response idempotency открыты. Реальный общий процент неизвестен.

## Продолжение: status deadlines

Добавлены15sec deadlines каждого status/descriptor GET и10min wall-clock polling deadline, cleanup/cancellation при завершении. Fake-timer unit tests написаны, NOT RUN. Server durable idempotency response и terminal restart открыты. Реальный общий процент неизвестен, новых полных зачётов нет.

## Продолжение: immutable admission receipt

Добавлена01133 atomic activation receipt, immutable guard и original acceptance replay независимо от current task status при повторной source/principal authorization. Startup gate/integration authority test написаны, NOT RUN;01133 NOT APPLIED. Legacy activated rows compatibility и full idempotency-record HTTP contract открыты. Реальный общий процент неизвестен.

## Продолжение: idempotency records

Добавлены01134 request/key/hash/run binding и atomic202 response persistence при activation, Go submission binding/startup gate. Unit tests написаны, NOT RUN;01134 NOT APPLIED. Legacy/backfill/replay/concurrency verification открыты, общий реальный процент неизвестен.

## Продолжение: stored response replay

Добавлены01135 authorized persisted-response read и submission replay до подготовки/после activation, strict scoped receipt parser; unit tests NOT RUN,01135 NOT APPLIED. Legacy admitted runs без response fail closed; compatibility/concurrency/TTL и terminal restart открыты. Реальный общий процент неизвестен.

## Продолжение: terminal restart

Добавлены explicit restart после fresh terminal observation, archive old journal, new snapshot/key, parent editor snapshot sync; cancelling не считается terminal. Unit tests написаны, NOT RUN. Expired pre-admission restart и legacy/concurrency/idempotency compatibility открыты. Реальный процент неизвестен.

## Продолжение: concurrent local journals

Добавлена сериализация submit/restart shared journal key и owned request до queue. Unit tests написаны, NOT RUN. Cross-tab/process coordination, queued-wait cancellation latency, legacy/expired-draft flow открыты; реальный общий процент неизвестен.

## Продолжение: queued cancellation/Web Locks

Добавлены immediate queued cancellation с сохранением очереди и browser same-origin Web Locks coordination; unit tests написаны, NOT RUN. Реальный cross-tab/native evidence и fallback multiprocess coordination открыты. Общий реальный процент неизвестен.

## Продолжение: source size mismatch

Закрыт кодом detector1MiB versus shared reader/SQL64KiB mismatch: common limit +01136 source/document/child gates. Params/NPY header bounds сохранены. Tests NOT RUN;01136 NOT APPLIED. Остальные correction-only caps ещё требуют аудита, реальный процент неизвестен.

## Продолжение: downstream source bounds и пересчёт

Статическая сверка сохраняет 90 entrypoints: 16 missing, 1 partial, 73 без полного semantic review. 82.2% — только потолок покрытия каталога, не процент реализации продукта; whole-product percentage неизвестен. Добавлена forward-only 01137: correction и explicit-lines draft принимают source до 1MiB; envelope 5MiB учитывает escaping source/lines/Params. Остальные checks и grants сохранены. Миграция NOT APPLIED, tests/build/typecheck NOT RUN. Новых полных semantic credits нет.

## Продолжение: long-source unit coverage

Написан unit-тест валидной истории из128 matrix transforms с canonical WorkingImage больше64KiB: original-source verification, explicit-line draft preservation, отказ при изменении bytes и превышении1MiB. Статически подтверждено, что оставшиеся Go64KiB bounds относятся к Params/profile/receipt/transport descriptors и NPY overhead; они не расширялись. Tests NOT RUN,01137 NOT APPLIED. Общий процент неизвестен; каталог ceiling82.2% без новых полных зачётов.

## Продолжение: proven pre-admission expiry

Добавлены01138 state wrapper и dedicated HTTP410 PERSPECTIVE_DETECTION_DRAFT_EXPIRED: только cancelled/unsealed run без admission receipt, с draft_expired event и без attempted/non-expiry tasks после current source/principal checks. Generic snapshot/legacy expiry сохраняет прежнюю ошибку. Parser/receipt denial и error mapping unit tests написаны, NOT RUN.01138 NOT APPLIED. Client restart для этого кода ещё не подключён; общий процент неизвестен.

## Продолжение: client expired-draft restart

Receipt-less restart повторяет original POST/key под journal lock; только typed APIError410 PERSPECTIVE_DETECTION_DRAFT_EXPIRED разрешает archive/new snapshot/key. Generic expiry/timeout/infrastructure сохраняют journal. Recovered admission записывается schema2 без ротации. Editor показывает restart после dedicated expiry. Unit tests authored NOT RUN; UI recovery после recovered admission требует дальнейшей сверки. Общий процент неизвестен.

## Продолжение: recovered admission UI handoff

Restart carries validated immutable recovered receipt through dedicated error; mounted editor immediately restores producer/polling without reload or snapshot/key rotation. Added ownership and malformed-success unit cases; NOT RUN. Общий процент неизвестен, полных module credits не добавлено.

## Продолжение: multi-input registration kernel

Добавлен algorithms/multi_registration.py: shared locked ORB/BF, lexicographic bounded pairs, calibrated RANSAC, half-pixel proxy→source mapping, singular/condition/horizon checks, deterministic maximum spanning tree, direct-reference focus registration, cycle review, immutable matrices/cancellation. Unit tests написаны NOT RUN. Adapter/proxy color qualification, cylindrical projection, blend/focus pyramids, lineage/results/adoption/backend/client остаются открыты; это не runtime entrypoint и новых полных credits нет. Общий процент неизвестен.

## Продолжение: panorama geometry

Добавлены pre-allocation canvas bounds/caps, immutable translated source matrices и tiled cylindrical inverse field writer (half-pixel original centres, explicit coverage, finite invalid zeros, cancellation, caller-owned scratch buffers). Unit tests authored NOT RUN. Sampling/alpha composition, blending, lineage and full runtime wiring остаются открыты; новых полных credits нет, общий процент неизвестен.

## Продолжение: multi-input field sampling

Добавлен tiled premultiplied bilinear sampler для linear float32 HDR, normalized alpha и retained half-pixel fields. Проверяет full original finite/range, сохраняет HDR без clamp, zero RGB вне coverage/при zero alpha, immutable tile outputs/cancellation. Unit tests authored NOT RUN. ICC-aware adapter, matrix field composition, blending/focus, persistence/adoption/runtime wiring открыты. Общий процент неизвестен; новых полных credits нет.

## Продолжение: panorama feather/motion kernel

Добавлен ordinal linear-HDR feather blend с exact Euclidean distance capped64, bounded tile halos, alpha-premultiplied accumulation, zero uncovered, pairwise overlap RGB range и exact radius3 disk motion dilation. Caller-owned outputs/alias rejection/full finite input validation/cancellation. Unit tests authored NOT RUN. Dilation row-cache RAM должен войти в adapter resource estimate; adapters/qualification/lineage/adoption/runtime remain open. Общий процент неизвестен, новых полных credits нет.

## Продолжение: focus pyramid primitives

Добавлены scratch-buffer focus weights, pyramid shapes, tiled Gaussian downsample/even centres и exact-shape bilinear upsample. HDR weights float64, global reflect boundaries, no full-frame temporary, cancellation/alias gates. Numerical support clarified in16-imaging-algorithms. Unit tests authored NOT RUN. Pyramid normalized composition/reconstruction/motion policy, scratch resource admission and complete operation wiring ещё открыты. Общий процент неизвестен.

## Продолжение: focus level blend/reconstruction

Добавлены tiled float64 Laplacian band construction, normalized ordinal level blend с reference fallback при zero weights и coarse-to-fine level reconstruction. Caller-owned arrays, finite/nonnegative/alias gates, cancellation. Unit tests authored NOT RUN. Full scratch orchestration, motion policy, alpha finalization and qualified operation/backend/client integration remain open. Общий процент неизвестен; module credit не добавлен.

## Продолжение: focus motion policy

Добавлены reference-relative overlap motion detection и explicit reject/review policy; review копирует reference RGB/alpha только внутри dilated disk3 mask, reject не меняет outputs. Shared disk morphology вынесена из panorama без изменения алгоритма. Unit tests authored NOT RUN. Scratch orchestration, reference alpha contract/qualification, report/adoption/runtime/backend/client остаются открыты. Общий процент неизвестен.

## Продолжение: focus scratch orchestration

Добавлены focus_scratch_bytes и prepare_focus_stack: upfront disk reservation, memmap pyramids/weights/bands/blend/reconstruction, motion gate, reference opacity/no-coverage fallback, bounded final conversion, close-before-staging и cleanup. Unit tests authored NOT RUN. RAM admission/qualification/ICC conversion/registered inputs/lineage/result/adoption and public workflow остаются открыты; общий процент неизвестен.

## Продолжение: focus resource admission

Добавлен focus_resources с conservative live-mapped-pages/input/tile/OpenCV/row-cache headroom и обязательный ram_max_bytes gate до scratch reservation. Coarsest reconstruction copy теперь cancellable tiled. Unit tests authored NOT RUN. Это estimate, не measurement/qualification; larger stacks могут отклоняться при32GiB, без скрытого превышения бюджета. Source downloads/ICC registration/adoption/public wiring remain open. Общий процент неизвестен.

## Продолжение: panorama scratch outputs

Добавлены panorama_blend_resources и prepare_panorama_blend: RAM gate перед allocation, reserved NPY RGB/alpha/motion, close-before-staging, validate/cleanup и conservative native/tile/row-cache estimate. Unit tests authored NOT RUN. Это blend-stage, original/projection/registration storage must be admitted separately. Lineage/report/qualification/adoption/backend/client ещё открыты; общий процент неизвестен.

## Продолжение: registered-frame preparation

Добавлены tiled integer-centre matrix→half-pixel inverse field и prepare_registered_frame: RAM/scratch admission, existing premultiplied HDR sampler, retained exact immutable matrix, owned closed NPY RGB/alpha/coverage и temporary field. Unit tests authored NOT RUN. Aggregate multi-frame budgeting/ICC/projection registration orchestration/lineage/adoption/public runtime remain open; общий процент неизвестен.

## Продолжение: aggregate planar stack

Добавлен prepare_planar_stack: exact reference identity, panorama canvas/focus reference canvas, aggregate RAM/scratch gate, retained ordered registered frames, read-only mapped stage inputs, ExitStack output/registration cleanup. Unit tests authored NOT RUN. Матрицы пока caller-supplied: authoritative register_pairs/ICC/proxy/projection/qualification and result/lineage/adoption/public workflow wiring still required. Не считать helper runtime endpoint; общий процент неизвестен.

## Продолжение: automatic planar registration wiring

Добавлены linear-luma/opacity-safe proxies и automatic planar coordinator: ORB/RANSAC→ordered matrices→aggregate preparation, direct focus vs all-pairs panorama, retained inconsistent-cycle evidence, conservative proxy RAM admission. Shared matcher supports optional owned masks (default unchanged); runtime qualification pins must include changed dependency. Unit tests authored NOT RUN. ICC/source authority/cylindrical path/result/adoption/public workflow remain open; общий процент неизвестен.

## Продолжение: cylindrical projection preparation

Добавлен prepare_cylindrical_frame с retained original inverse field, same-size RGB/alpha/coverage files, premultiplied sampling, RAM allowance for64 full-width coordinate rows, reserved scratch и cleanup. Unit tests authored NOT RUN. Aggregate cylindrical→registration coordinator и dense-field lineage/report/adoption/runtime remain open; общий процент неизвестен.

## Продолжение: cylindrical panorama coordinator

Добавлен cylindrical→automatic registration→panorama context с сохранением ordered original inverse fields и двухэтапным aggregate admission (проекции до файлов, panorama после определения canvas с remaining budgets). Failure unwinds all mappings/reservations. Unit tests authored NOT RUN. Dense lineage/result publication/qualification/adoption/backend/client remain open; общий процент неизвестен.

## Продолжение: multi-source lineage compiler

Добавлены RegisteredTransform schema1 и compile_multi_lineage: exact source/canvas hashes, ordered bindings/registrations, deterministic output canvas, reference matrix chain, explicit cylindrical dense step/half_pixel plus all per-source fields, output/root alias gates. Unit tests authored NOT RUN. Pixel/ICC/source SQL authority, staged closure/report/result/adoption/public workflow still required; compiler alone не runtime authority. Общий процент неизвестен.

## Продолжение: multi output color restoration

Добавлен prepare_multi_encoding с exact source ICC verification before/after IO, explicit supported profile/encoding, tiled HDR transfer restoration, owned RGB/alpha/changed NPY и RAM/scratch gate. Geometry flag должен derive из retained reference transforms; unchanged canvas compares encoded bits. Unit tests authored NOT RUN. Final coordinator/color-source conversion/report/result/adoption/public workflow remain open; общий процент неизвестен.

## Продолжение: ICC-aware multi-source conversion

Добавлены prepare_multi_sources и aggregate source resource estimate: owned WorkingImage metadata, exact common ICC identity, ICC gate before/after conversion, normalized alpha including uint16, tiled HDR linear copies, primary-specific luma и cleanup. Unit tests authored NOT RUN. Exact SQL/object source authority, full source→result coordinator/staging/adoption/public workflow still open; общий процент неизвестен.

## 2026-10-10: source-to-encoded multi-image coordinator

Добавлен prepare_multi_image: exact binding hashes before files, ICC-aware source conversion, automatic planar/cylindrical preparation, derived reference geometry flag, encoded final files/changed mask, cumulative remaining RAM/scratch и full lifetime cleanup. Shared proxy RAM formula extracted. Unit tests authored NOT RUN (positive coordinator test controls registration, не доказывает ORB/quality). SQL source authority/report/result staging/adoption/public workflow remain open; общий процент неизвестен.

## 2026-10-10: multi result staging

Добавлен stage_multi_result: pre-stage final NPY shape/finite/alpha/mask checks, encoded pixels/alpha, all cylindrical fields, WorkingImage, ordered RegisteredTransforms, changed MaskRef, review ImageReport with motion/cycle reason and ImageResult(generated_mask=null). Required original roots returned separately; adopter must replay/authorize closure. Unit tests authored NOT RUN. Full staged positive/closure proof, runtime registration/qualification/SQL adoption/public workflow remain open; общий процент неизвестен.

## 2026-10-10: Params and focus30 contract correction

Normative OpticalFocus_StackParams допускает30frames, panorama20. Исправлены общие source/registration/focus/lineage/report coordinators до30; all-pairs panorama и panorama canvas сохраняют20/190 caps. Добавлены strict Params contracts и unit cases focus30 direct reference/resources, panorama21/focus31 rejection, duplicate/version/reference gates. Tests NOT RUN. Full staged closure positive case ещё открытый; общий процент неизвестен.

## 2026-10-10: full staged closure unit scenario

Написан positive source→encoded→staged ImageResult unit scenario с controlled registration, exact original bindings, ordered registrations, digest traversal и reachability всех staged outputs/byte accounting. Stage preflight теперь повторно derives geometry flag и rejects invalid focus reference/shape/motion evidence before writes. Tests NOT RUN; это не physical/object/SQL/adoption or quality proof. Cylindrical staged closure/replay/runtime/public workflow ещё открыты; общий процент неизвестен.

## 2026-10-10: cylindrical staged closure/replay coverage

Stage now independently replays exact cylindrical field/coverage formula tiled before first write. Full staged closure unit scenario expanded to cylindrical panorama with controlled registration, distinct retained per-source warp refs and reference DenseWarp half_pixel link. Tampered-field rejection unit authored. Tests NOT RUN; no current-build/runtime/SQL/adoption proof claimed. Общий процент неизвестен.

## 2026-10-10: frozen multi Params/Calibration boundary

Добавлены canonical bounded Params/Calibration decode с exact calibration SHA, installed model/dataset identity, qualified flag/pixel cap и immutable byte snapshot. prepare_frozen_multi_request повторно validates bytes и передаёт exact ordered frames/reference/projection/motion policy/calibrated thresholds. Unit tests authored NOT RUN. Qualified flag не заменяет independent qualification/runtime/SQL authority; worker adapter/adopter/public workflow remain open. Общий процент неизвестен.

## 2026-10-10: multi-input runtime byte identity

Добавлен MultiImageRuntimeLock с independent identity и inventory всех multi-input kernels/preparation/contracts/shared ORB-RANSAC/color/runtime files плюс OpenCV Python/native bundled dependencies. Verify exact digest/platform/package versions/origin/component bytes, reject conflicting OpenCV distributions. Unit tests authored NOT RUN. Byte identity не photographic qualification; installation/admission/worker/adopter SQL/public workflow remain open. Общий процент неизвестен.

## 2026-10-10: пересчёт и multi-image admission

Повторный подсчёт существующего реестра: 90 точек входа; сохранённый потолок покрытия 82,2% не является процентом полной реализации. Полная семантическая сверка требований не выполнена, поэтому whole_product_implementation_percent остаётся null. Новых полностью зачтённых модулей: 0.

Добавлен PinnedMultiImageAdmission: отдельный operation/runtime pin, canonical snapshot всех Params и порядка входов, Calibration/report/policy/model/dataset binding, CPU resource gate, ICC каждого входа и повторное разрешение live authority. Runtime inventory расширен. Unit tests на ранние отказы написаны, NOT RUN; положительный admission сценарий и интеграция adapter/SQL/adoption/public workflow остаются открыты. Миграции не применялись.

## 2026-10-10: qualification-bound multi preparation

Добавлен prepare_admitted_multi: owned Params/WorkingImages, admission before processing, RAM no greater than leased estimate, повторный admission before handing off prepared output, cleanup on revocation. Runtime inventory includes bridge. Expanded authored unit tests: panorama/focus positive admission, substitution/order/report/calibration/ICC/budget/live revocation, prepare ordering/ownership/cleanup/resource refusal. Tests NOT RUN; operations still disabled. Exact versioned pixel loading, SQL closure authority, adapter/staging/adoption and public workflow remain open. Whole-product percentage unknown; no new full-module credit.

## 2026-10-10: multi-image worker adapter

Добавлены verify_multi_source_closure и multi_adapter.execute: exact ordered source/canvas digests, declared WorkingImage/NpyArray children including retained dense fields, child alias refusal; CPU/v2.2 and leased RAM gates; independent admission before pixel IO; exact-shape float32 RGB/legacy-alpha input contexts; admitted preparation with renewed grant; stage ImageResult; owned lifetime cleanup and OperationResult byte accounting. Runtime inventory expanded. Authored unit scenarios missing/refused admission before IO, undeclared child and source reorder. Tests NOT RUN. Adapter is not registered/enabled; installation/SQL authority/adoption/public workflow and positive full adapter unit coverage remain open. No whole-module credit, product implementation percentage remains unknown.

## 2026-10-10: multi installation and original evidence loader

Добавлены load_multi_evidence / multi_evidence_resolver: exact canonical leased Params digest, frozen CPU qualification profile, dedicated live SQL authority, bounded report/Calibration reads from exact object versions, byte/size/hash/canonical checks, scope rechecks around IO and response cleanup. Добавлены MultiImageRuntimeOptions/Installation и multi_operations factory with explicit SQL capability gate, operation-specific pin/policy and command/runtime checks. Runtime inventory expanded. Factory is not connected to default registry; TaskDatabase multi authority/capability methods and SQL implementation remain absent/open.

Authored unit coverage: original evidence/provenance/scope refusal, installation early refusal, complete worker adapter source NPY→prepare→staged document for panorama/focus using controlled identity registration, progress/bytes/scratch cleanup checks. Tests NOT RUN. No photographic quality or current runtime proof; no module completion credit. Whole-product percentage remains unknown.

## 2026-10-10: multi SQL qualification authority

Authored forward-only 01139: private canonical multi policy SHA, operation-specific qualification lock, worker lease/fence/resource-slot/typed-closure gate, ordered multi source snapshot scope with original artifact availability, Params count/reference/pixel/projection/motion-policy constraints, leased qualification report/Calibration object locators. Added TaskDatabase multi capability/evidence methods and authored unit argument/bool cases. SQL NOT APPLIED; tests NOT RUN. Source producer/graph admission, complete adoption, runtime environment hookup and clients remain open; migration semantic/integration audit still required. No new module completion credit, whole-product percent unknown.

## 2026-10-10: multi worker configuration hookup

Добавлен runtime/multi_config.py: bounded no-symlink SHA-pinned config, strict keys/operation/policy/runtime checks, independent PHOTO_PANORAMA and PHOTO_FOCUS_STACK enablement/config digests, CPU/ICC requirements. build_worker/run_worker accept explicit multi_installations, reject duplicate/invalid operations and install qualified operation factories. Defaults remain disabled; CLI environment dispatch and V2 adoption still open. Authored config refusal/independent enablement tests NOT RUN. Unapplied 01139 source binding shape tightened. No migration execution/build/test; no whole-module completion credit.

## 2026-10-10: multi adoption metadata proof

Added verify_multi_result_metadata: canonical input/output documents, exact ordered source registrations, independently rebuilt canvas/reference lineage and MaskRef, projection/focus geometry constraints, pixel cap, canonical review report/motion reason, unique output IDs and exact output evidence roles. Authored positive metadata scenario after full adapter staging for both planar operations plus role-substitution refusal. Tests NOT RUN. Metadata proof is not pixel/registration replay or live SQL adoption authority; those remain mandatory/open, along with commit and client workflows. Runtime inventory expanded; no module completion credit.

## 2026-10-10: multi independent pixel replay

Added verify_multi_pixel_replay and compare_multi_prepared: original source/calibration decode, digest-verified arrays, independent automatic registration/geometry/color preparation, exact matrix/motion/cycle checks and bitwise final RGB/alpha/changed/projection-field comparisons. Materialized arrays charged against combined RAM/scratch before preparation. ArrayEvidenceSession permits output inverse fields only with explicit warp_field role. Runtime inventory expanded. Authored replay comparison/tamper/signed-zero/cylindrical-field unit tests NOT RUN. Full reader/session replay unit, qualification wrapper, SQL adoption commit/producer and clients remain open; no whole-module completion credit.

## 2026-10-10: qualified replay orchestration and physical reader scenario

Added verify_admitted_multi_replay: independently installed admission around replay, runtime/live grant/ICC checks before and after, exact frozen Params comparison; completed-task resolver must supply adoption authority and final SQL transaction still rechecks. Admission now owns/revalidates Params and WorkingImages before callbacks. Authored runtime/grant ordering and mid-replay revocation scenarios. Full adapter unit scenario now uses real input NPY hashes, original Calibration/input evidence and streaming Reader through ArrayEvidenceSession plus qualified replay (controlled registration). Tests NOT RUN. Completed-task SQL authority/commit, producer and clients remain open; no module credit.

## 2026-10-10: completed multi authority and reconstruction

Authored forward-only 01140: operation-specific qualification object locators and succeeded-task adoption evidence bound to attempt/fence/canonical result, ordered authorized snapshot sources, CPU profile, exact Params/Calibration and review_required=true. Added bounded autocommit evidence loader/checkpoint and qualified reconstruction preparation that reads exact original report/Calibration, replays and checkpoints current authority. Authored stale/missing/read-configuration/checkpoint unit cases NOT RUN. Migration NOT APPLIED. Final commit integration, richer positive completed-task fixture, producer/client workflows remain open; no full-module completion credit.

## 2026-10-10: multi adoption coordinator and SQL commit

Authored 01141: completed Params discovery, generic adoption guard for panorama/focus, dedicated adopt_v29_multi re-locking tenant/usage/project/run/task and comparing current evidence before generic atomic closure publication. AdoptionCoordinator accepts validated unique operation installations, runs reconstruction outside write transaction and uses dedicated commit. Generic closure bindings now include ImageReport.registrations and RegisteredTransform.inverse_field. Full adapter unit scenario extended through generic inventory with all staged outputs. Tests NOT RUN, migrations NOT APPLIED. Commit SQL/integration verification, adopter service/env/CLI hookup, producer/graph admission and clients remain open. No module completion credit.

## 2026-10-10: multi adopter/worker CLI and service wiring

Added multi_adopter_from_environment with per-operation flags/config digests/resource budgets/ICC/scratch validation, explicit operation binding and disabled defaults. Worker CLI loads independent multi installations; adopter CLI and service propagate validated installations to AdoptionCoordinator. Added require_multi_adoption_schema checking exact SQL ABI/SECURITY DEFINER/owner/EXECUTE under scheduler role before adopter startup. Runtime inventory expanded through wiring. Authored config rejection unit cases NOT RUN; migrations remain unapplied. Producer/source snapshot/graph admission and public clients remain open, plus current-build verification and SQL commit integration tests. No full-module completion credit.

## 2026-10-10: multi Go Params and original metadata compiler

Added PanoramaParams/FocusStackParams Compile+Parse with canonical bounded bytes, ordered frames, 20/30 limits, exact reference/pixel/calibration/binding validation and duplicate-version/calibration alias refusal. Added VerifyMultiWorkingImages preserving frame order, exact original document SHA/canonical canvas, common supported ICC/encoding, float32 and source dimension bounds. Params boundary/order/noncanonical tests authored NOT RUN; gofmt only. Original metadata positive tests, multi qualification/resource plan, SQL producer/source closure publication/graph admission and clients remain open. No full-module completion credit.

## 2026-10-10: multi physical input closure compiler

Added MultiImageInputs: retains every ordered frame/manifest occurrence for RGB/alpha/dense inverse fields, deduplicates only transport entries, rejects child/JSON-root aliases, conflicting SHA for same artifact ID and pixel/alpha alias. Shared original document/pixels across different asset versions remains supported with distinct per-frame bindings. Authored original digest/order/color and child-conflict/shared-occurrence unit cases; tests NOT RUN, gofmt only. Physical object header/size/dimension verification and live per-edge SQL authority, qualification/resource/task plan, producer admission and clients still open. No whole-module completion credit.

## 2026-10-10: multi physical NPY verification path

Added MultiSourceArrays and VerifyMultiSourceObjects: ordered per-frame specs for pixels/alpha/dense fields; exact immutable object streaming via shared bounded NPY verifier, locator rechecks after every read and again after all inputs; fresh owned Params/source bytes on catalog callbacks, cancellation and stream closure. Extracted shared sourceImageArraySpecs preserving perspective behavior. Authored per-frame spec/cancellation unit cases NOT RUN; gofmt only. Dedicated SQL multi array catalog/authority, richer stream/ownership unit cases, qualification/resource/task plan and producer/client integration remain open. No full-module completion credit.

## 2026-10-10: multi SQL physical source catalog

Added SQLMultiSourceArrayCatalog validating exact ordered occurrence/source canonical SHA/canvas/array spec before SQL, immutable locator bounds/bucket/version/SHA/media and short committed transaction. Authored forward-only 01142 v29_multi_array_source binds operation/Params/ordinal and delegates exact source-child scope to existing shared authority lock; no object IO in SQL. Authored locator/order/binding refusal unit cases NOT RUN, migration NOT APPLIED. Qualification/resource/task plan, producer admission/public workflow and full integration verification remain open. No full-module completion credit.

## 2026-10-10: multi Go qualification evidence

Added MultiQualificationPin, operation-specific MultiPolicySHA and VerifiedMultiQualification: owned canonical Params/all originals/report/Calibration and copied deployment locks, exact per-operation policy/runtime/model/device/dataset/issuer binding, original Calibration ref and qualification flag, common source ICC and CPU zero-VRAM report gate. Pin/Calibration access revalidates evidence and returns copies. Authored positive panorama/focus ownership and substitution/order tests NOT RUN; gofmt only. Resource estimator/profile/task plan, producer admission, public workflow and full verification remain open. No full-module completion credit.

## 2026-10-10: multi producer resource/profile compiler

Added MultiResources and ValidateMultiCapacity: conservative total original download pages/files, linear copies, proxy buffers, all registered frames, optional cylindrical projections, exact focus pyramid allocation formula, blend/encoding and metadata allowance; CPU VRAM=0. Panorama reserves max_pixels upper bound and worst-case row width before ORB; focus uses reference canvas. Added CompileMultiExecutionProfile binding exact owned ordered sources/Params and qualification evidence, report metrics within budget, runtime/pin/resources. Authored resource/capacity/projection/profile/order tests NOT RUN; gofmt only. Cross-language budget fixtures and measurement still needed; task plan/producer admission/public workflows remain open. No whole-module completion credit.

## 2026-10-10: multi task plan and immutable transport inventory

Added CompileMultiTaskPlan: operation-specific exact Params/profile/resources, owned bytes, deduplicated sorted direct JSON roots and full physical occurrences with ordered frame bindings. MultiTaskInputEntries reconstructs and compares the complete plan against qualified originals before adding exact published Params and typed inventory; refuses aliases and any mutated profile/refs/reservation/occurrences. Authored panorama/focus inventory, mutation and cancellation unit cases NOT RUN; gofmt only. SQL producer draft/publication/graph seal, request recovery/idempotency and clients remain open. No full-module completion credit.

## 2026-10-10: immutable multi producer draft/descriptor

Added MultiDraft/Snapshot and CompileMultiDraft with deterministic per-operation/project/actor/idempotency run/workflow/task/Params IDs, owned ordered source recovery bytes and exact compiled profile/reservation. Descriptor validates IDs/published Params, rebuilds qualified task plan/inventory and returns canonical blocked-task descriptor/input digest. No runnable task or quota mutation occurs in these pure compilers. Authored deterministic/ownership/recovery tamper unit cases NOT RUN; gofmt only. SQL draft storage/publication/graph seal and recovery/idempotency orchestration, public clients and full verification remain open. No full-module completion credit.

## 2026-10-10: preliminary multi SQL draft persistence

Authored forward-only 01143 save_v29_multi_draft: tenant/actor/session binding, deterministic child IDs, bounded ordered originals, current source/physical-child and qualification locks, immutable existing-draft match, draft run/preview workflow only. Added SaveMultiDraft scheduler transaction wrapper. Draft profile checks are preliminary numeric bounds only; exact SQL resource recomputation MUST be implemented before graph seal. No tasks/quota/queue transition added. Migration NOT APPLIED; tests NOT RUN. Dedicated persistence tests, Params publication, exact resource seal/recovery/idempotency and clients remain open; no module credit.

## 2026-10-10: exact SQL multi resource mirror

Authored 01144 v29_multi_resource_estimate mirroring Go total resource formulas including retained source/download/projection/registration/pyramid/encoding pages and files. Added save_v29_multi_draft wrapper requiring exact profile/resource/reserved-output equality before preliminary persistence; prior save helper made private. Authored SQL↔Go integration cases planar/cylindrical/focus2/focus30 gated by PHOTO_TEST_DATABASE_URL. Tests NOT RUN; migration NOT APPLIED. Graph seal must repeat resource gate with current source/profile authority; Params publication, transport registration and public workflow still open. No module credit.

## 2026-10-10: multi Params publication and ledger recount

Recounted the existing 90-entrypoint ledger: 16 missing runtime paths, 1 partial path and 73 requiring semantic review. 82.2% remains only a catalogue upper bound; whole-product implementation percentage is unknown, with no new full semantic completion credits. This is not a fresh exhaustive requirement audit.

Added PublishMultiParams and operation-specific reservation/completion repository: immutable draft save, exact tenant/run/task/session/Params proof, short scheduler transactions around versioned object IO, replay/conflict/quota/stale handling and deterministic document identity. Authored forward-only 01145 binding publication to stored v29_multi_plan and repeating current save/resource/source/qualification authority at both reservation and completion. Proof bound is 64 MiB to accommodate multi-source recovery originals, rather than the perspective 512 KiB bound. Added owned-proof/foreign-scope/session/size unit cases. Tests/build/typecheck NOT RUN; migration NOT APPLIED; gofmt only. Transport registration, graph seal, recovery/idempotency, HTTP/client paths and full verification remain open. No whole-module completion credit.

## 2026-10-10: multi blocked task staging and transport compiler

Added StageMultiTask: rebuild immutable qualified descriptor, exact tenant/actor/session/Params proof, short scheduler transaction and SQL blocked-stage authority. Authored forward-only 01146 with exact multi-source typed inventory/digest/profile comparison, ready versioned Params/input artifacts, current draft/resource/source/qualification gate, output quota reservation and immutable blocked task. Generic document/input/graph interfaces reject v29_multi_plan to prevent bypassing the dedicated authority. Added BuildMultiTaskTransport using bounded standard chunks/manifest with frozen replay cursor and input/transport alias refusal. Worker source closure discovery now permits 30 WorkingImage roots and 3900 physical arrays (30 × (pixels + alpha + 128 dense fields)), rather than rejecting valid closures above 512 arrays.

Authored transport cursor ownership, mutated-plan/alias refusal and stage foreign-session/Params/cancellation unit tests. Tests/build/typecheck NOT RUN; migration NOT APPLIED; gofmt only. Dedicated transport publication repository, input registration/seal, final current graph admission, recovery/idempotency and public API/client workflow remain open. No full-module completion credit; overall implementation percentage remains unknown and 82.2% remains catalogue ceiling only.

## 2026-10-10: multi versioned transport publication and recovery

Added PublishMultiTransport and dedicated repository/Objects bridge: blocked staging before object IO, immutable Params proof with caller identity, bounded canonical chunk purposes 0..7 plus manifest, reservation/replay/conflict/quota/stale handling and exact versioned completion. Authored forward-only 01147 SQL transport authority checking each 512-entry slice against saved descriptor, all ready versioned chunks before manifest, repeat current source/qualification/resource staging authority and quota accounting.

Added RestoreMultiTransport and ReadMultiPublishedTransport, plus 01148 recovery of all 1..8 chunks: current existing blocked task/live quota required, exact reconstructed chunk and manifest SHA/size, artifact/version/role/expiry checks, alias rejection and qualification-bound descriptor reconstruction. Recovery creates no missing task or quota reservation; publication remains separate. Authored positive panorama/focus recovery and substituted count/digest/chunk/manifest/unknown-field/duplicate-chunk refusal and purpose bounds unit cases NOT RUN. gofmt only; migrations NOT APPLIED. Dedicated input registration/seal, final graph authority, producer orchestration/idempotency and public clients remain open. No whole-module completion credit; real completion percentage unknown, catalogue ceiling 82.2%.

## 2026-10-10: multi input registration and graph admission

Authored 01149 stage replay correction: after input seal, only the four standard closure/profile fields may differ, and only when v2_input_closure_ready is true; original execution profile remains exact. Added 01150 dedicated multi registration: complete published transport comparison through the current versioned read authority, exact typed ordinal inventory, deduplicated direct roots, bounded 512-entry batches and atomic input seal; replay requires complete exact registered rows and task metadata.

Added RegisterMultiInputs and AdmitMultiTask. Authored 01151 final graph gate: draft/descriptor/task/workflow/no-edge checks, current transport/source/qualification/resource authority and exact registration replay, enabled CPU registry/policy locks, existing generic activation/event/outbox chain, atomic queued receipt and original auth-session metadata for worker/adopter gates. This does not enable either operation or manufacture deployment qualification. Authored transaction-order, registration refusal, graph refusal and ambiguous-commit propagation unit cases NOT RUN. Migrations NOT APPLIED; gofmt only. Real runtime/deployment compatibility remains unverified; public orchestration/idempotency/receipt recovery, disabled registry/output-role provisioning, HTTP/client and full testing remain open. No full-module completion credit; real implementation percent unknown, catalogue ceiling 82.2%.

## 2026-10-10: multi historical queued receipt and disabled registry

Added MultiReceipt parser and ReadMultiQueuedReceipt: canonical exact fields, immutable qualified descriptor/operation/actor/session/task/workflow pin binding and complete chunk/manifest reconstruction. Historical acknowledgement grants no activation or current execution-status authority. Authored 01152 read-only receipt authority with exact stored plan, current source scope/all physical child locks for every frame, current operation-specific qualification, exact registered closure/profile/transport and ready original artifacts. It creates no quota/tasks and changes no execution state. Authored positive panorama/focus receipt, foreign actor/session/run/task/operation/state/qualification/descriptor/transport/unknown-field/cancellation unit cases NOT RUN.

Authored 01153 CPU registry and all nine output-role policies with 200 MP array bounds and frame-specific 20/30 registration/field count bounds. Both operations remain v2_enabled=false; no qualification or runtime installation is manufactured. Migrations NOT APPLIED; gofmt only. Producer submission orchestration/recovery/idempotency, current qualification object loading, public API/client and full verification remain open. No whole-module completion credit; real implementation percentage unknown and 82.2% only catalogue ceiling.

## 2026-10-10: multi producer original qualification IO

Added SQLMultiQualificationCatalog with exact operation-specific current authority, bounded original Report/Calibration locator decoding and short scheduler transactions. Added LoadMultiQualificationEvidence: own Params/all source originals/deployment arrays before callbacks, strict source identity/common color, operation/policy/runtime/model/calibration pins before IO, versioned bounded read/close/size/SHA/canonical checks and live uncached locator checkpoints before/after each read and final content verification. Added LocatedMultiQualification, owned metadata access, LockMultiQualification and LockLocatedMultiQualification to retain current qualification plus exact original locators through a caller commit. Authored forward-only 01154 exact qualification-object SQL gate.

Authored panorama/focus original-stream corruption/oversize/version-change refusal, stream close and immutable returned metadata/content cases, plus exact operation/locator lock and stale locator cases. Tests/build/typecheck NOT RUN; migration NOT APPLIED; gofmt only. These producer IO/lock primitives still require coordinator/public orchestration integration; no whole-module credit. Submission orchestration/recovery/idempotency, public API/client and full testing remain open. Real implementation percentage unknown; 82.2% remains catalogue ceiling.

## 2026-10-10: multi producer coordinator and submission protocol

Added deployment-owned MultiCoordinator/MultiPrepareRequest/PreparedMulti. Rebuild owns ordered originals, validates operation/max-pixel/resource ceilings before IO, reads current original qualification and physical source arrays, and deterministically recompiles the full draft. Deployment Capacity.PixelCount is treated as an upper bound and normalized to exact request pixels only after checking that bound. Prepare publishes Params; transport/admission revalidate draft, Params and exact original qualification locations. Receipt recovery rebuilds authorized originals without publishing or activation.

Added SubmitMulti with per-stage owned nested source buffers, stable request identity and read-only historical acknowledgement recovery after ambiguous preparation/publication/admission errors; cancellation never reactivates. Coordinator final graph admission and receipt reads now retain exact LocatedMultiQualification policy/object locks through commit. Lower-level content primitives remain usable with optional locator binding; coordinator always supplies it. Authored stage sequence/recovery/cancellation/nested mutation tests and deployment-bound refusals before IO. Tests/build/typecheck NOT RUN; gofmt only. Full coordinator/database/object integration, durable HTTP idempotency/request recovery, public API/client workflows and downstream review/Apply remain open. No full-module completion credit; real implementation percentage unknown and 82.2% remains catalogue ceiling.

## 2026-10-10: multi snapshot document reader, intent service and HTTP scaffold

Added SQLMultiSourceReader and 01155 accepted-snapshot source document authority: ordered bounded versioned original WorkingImage reads, canonical SHA/size validation, per-read and final uncached locator rechecks, common source/canvas/color verification and closed streams. Added MultiIntent strict per-operation decoding (schema/snapshot/canonical Params, no client source bytes or deployment pins), deployment-owned NewMultiService and SubmitProjectIntent resolving all originals before coordinator submission.

Added HTTP panorama-intents/focus-stack-intents handlers, tenant/project/write identity and idempotency-key shape validation, bounded body, operation-specific intent decoding and exact queued ticket actor/session/workflow/task/operation checks. Server dependencies default nil and operation registry remains disabled. This is an HTTP scaffold, NOT a complete durable HTTP-idempotency implementation: request-hash claim/conflict/recovery and production service/config selection are still required before activation. Authored intent order/ownership/foreign fields and receipt ticket substitution unit cases NOT RUN. Migration NOT APPLIED; gofmt only. Source reader full SQL/object integration, durable idempotency, production wiring, client workflows and review/Apply remain open; no module completion credit and whole-product implementation percent unknown.

## 2026-10-10: multi durable HTTP intent claim

Authored 01156 claim_v29_multi_intent: bounded canonical request, operation/frame count, active tenant and current accepted snapshot/source session authority, durable tenant/user/project/operation/key request hash (including original auth session and ordered intent), deterministic run ID and conflict refusal. Added ClaimMultiIntent scheduler wrapper and integrated it into MultiService before any source/object reads. The claim commits independently of long IO, creates no task/quota/activation and permits the same canonical retry; changed intent/session under the same key maps to v2input.ErrConflict/HTTP409.

Authored exact canonical identity/SQL ABI/transaction commit/refusal unit cases for both operations. Tests/build/typecheck NOT RUN; migration NOT APPLIED; gofmt only. This is durable request binding only: atomic idempotency response completion at graph admission, claim-bound receipt recovery, production dependency selection/config, clients and real SQL/object verification remain open. No whole-module credit; real implementation percentage unknown and 82.2% remains catalogue ceiling.

## 2026-10-10: claim-bound graph activation and atomic idempotency response

Authored 01157: private prior graph/receipt implementations, public final graph gate requires exactly one matching durable tenant/user/project/operation/resource claim and canonical intent/session hash before activation. Existing graph activation/events/outbox/queued receipt and idempotency response_status=202/full receipt commit in the same transaction. Prior function execution revoked from scheduler. Historical receipt reads repeat original current source/qualification/closure authority and require the same claim hash; completed responses must equal the independently verified stored receipt. A recreated expired pending claim may recover that historical receipt without any execution or response mutation.

Added shared multiRunID and compileMultiIntentIdentity aligning Go intent/session hash, route and stable key-derived resource identity with SQL. Authored session/snapshot same-key conflict identity and project scope tests, plus optional real SQL canonical hash/UUID parity integration cases (not RLS/activation proof). Tests/build/typecheck NOT RUN; migration NOT APPLIED; gofmt only. Full claimed admission/rollback/response-recovery SQL fixtures, production configuration/service selection, clients and review/Apply remain open. No whole-module completion credit; real implementation percentage unknown, 82.2% catalogue ceiling only.

## 2026-10-10: multi producer deployment and backend wiring

Added immutable operation-specific MultiDeployment/MultiProjects decoding and exact tenant/project service selection. Added backend buildMulti with separate SHA-pinned panorama/focus configuration/DSNs, bounded regular file reads, 15-second startup context, dedicated scheduler pool, 16 exact SECURITY DEFINER/owner/EXECUTE function checks and denied scheduler access to private pre-claim admission/receipt helpers. Wired optional producer dependencies into main HTTP Server; absent configuration leaves each dependency nil and registry remains disabled. No runtime qualification or registry activation is inferred from configuration presence.

Authored deployment unknown-field/duplicate-scope/foreign-operation/policy/budget/ownership, unconfigured scope and optional/bootstrap DSN refusal unit cases NOT RUN. Added audit/v29-multi-deployment.md with concrete configuration/HTTP semantics and outstanding rollout evidence. Tests/build/typecheck NOT RUN; gofmt only. Full startup/SQL/object/claimed activation integration, photographic qualification, client review/Apply workflow and broader V20–V32 gaps remain open. No full-module completion credit; real implementation percent unknown; 82.2% catalogue ceiling only.

## 2026-10-10: multi client intent, API and durable attempt journal

Added strict ordered panorama/focus client contracts and freezeMultiIntent: exact keys/operation-specific options, 2..20/30 frames, distinct version bindings, nonzero refs/digests, reference/max-pixel/calibration bounds and owned nested data. Shared original source refs across distinct versions remain permitted consistently with backend. Added OwnerAPI.submitMulti selecting exact operation route with owned body/key/cancellation and validated queued ticket.

Added MultiAttempt/MultiAttemptStore: persist before IO and navigation, exact operation/body/key retries after ambiguous response or restart, immutable accepted ticket, unresolved replacement refusal, origin/user/tenant/project/snapshot/operation scope and logout generation/epoch fencing. Hooked clearMultiAttempts into AuthProvider logout. Authored intent/authority/order, restart/retry/transport mutation, journal operation separation/logout and API route/key/cancellation unit cases NOT RUN. Tests/build/typecheck NOT RUN. These client primitives are not a completed screen flow: snapshot selection, multi-frame authoring UI, result review/Apply and full current build verification remain open. No module credit; real implementation percentage unknown, catalogue ceiling 82.2% only.

## 2026-10-10: multi authoring capabilities and ordered selection

Added 01158 registry-enabled/current-qualification capabilities gate, MultiProjects.DescribeProject and tenant/project-bound HTTP panorama-capabilities/focus-stack-capabilities. Returns deployment Calibration ref and pixel/frame ceilings only after scheduler current authority; no source originals/runtime secrets or admission granted. Bootstrap now checks 17 exact public SQL ABIs. Added strict client capability scope decoding/API method and MultiSelection with owned ordered frames, stable reference version across reordering, removal/ref fallback, operation-specific projection/motion controls and frozen request construction.

Authored capabilities current/disabled authority, selection/reference/order/ownership/frame limits/operation constraints/foreign scope unit cases NOT RUN. Migration NOT APPLIED; tests/build/typecheck NOT RUN; gofmt only. Authoring screen integration, snapshot/attempt recovery handoff, result review/Apply and full SQL/client verification remain open. No full-module credit; real implementation percentage unknown, 82.2% catalogue ceiling only.

## 2026-10-10: reconciled catalogue and durable multi preparation

Reconciled panorama/focus stack entries with current conditional worker installation: missing→partial, not complete. Current 90-entry ledger: 14 missing, 3 partial, 73 require semantic review. Catalogue ceiling = (90−14)/90 = 84.4%. Real whole-product implementation percentage remains unknown; no complete-module credits awarded. This is a ledger reconciliation plus selected source review, not an exhaustive current audit.

Added MultiPreparationStore in the same module queue, origin generation and logout namespace as MultiAttemptStore. Persists ordered params, snapshot key and separate intent key before network IO; validates snapshot digest/count and exact attempt binding; refuses unresolved replacement/stage regression; only acknowledged records can be cleared. Added beginMultiPreparation/MultiPreparation coordinating original snapshot and admission retries, durable ticket before return, restart recovery and cancellation. Authored lost-response retry/order/key, accepted no-resend, replacement refusal, logout fencing and cancellation unit cases.

Tests/build/typecheck NOT RUN; migrations NOT APPLIED. UI authoring wiring, review/Apply, qualification and broad V20–V32 semantic audit remain open. Metal/MPS later verification budget remains 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: multi authoring route and localization — NOT RUN

Added /collections/[id]/multi and navigation from collections for panorama/focus stack. Loads authenticated user, exact tenant/collection/project, scoped capabilities and all revision-consistent collection pages. Resolves each selected asset binding without silently substituting its newer current version; owns bindings and aborts stale route reads. Supports ordered frames, stable reference, projection/motion options, frame caps and frozen preparation submission. Saved unresolved requests remain locked, recover independently of fresh authoring availability and retry original keys/body. Acknowledged ticket is durable before navigation; another selection requires acknowledged journal clearing. Journal read/fence failure disables authoring until reload. Virtualized collection rows load only thumb640 links; no master downloads.

New strings use common RU/BG/EN catalogues, errors store translation keys, locale changes do not recreate the scope/request. Added shared multi.submit keyboard command Ctrl/Cmd+Shift+Enter with editing-target refusal. Added frame-binding and route DOM tests for order/reference, durable remount retry, unavailable capabilities recovery, foreign current version refusal, both operation options, late binding/acknowledgement after route change and locale changes. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Updated incremental screen/component/state inventory.

No completed-module credit. Real implementation percentage remains unknown; catalogue ceiling remains 84.4% (14 missing, 3 partial, 73 semantic-review entries of 90). Multi result review/Apply, current photographic qualification, SQL/object/client deep verification and other V20–V32 gaps remain open. Later Metal/MPS checks retain 10,000,000,000-byte VRAM cap; RTX deferred.

## 2026-10-10: adopted multi root read authority — NOT RUN

Authored forward-only 01159: photo_api-only v29_adopted_multi_binding/object; private originals-ready predicate. The read gate binds exact panorama/focus producer task, run/workflow/snapshot, original canonical Params metadata, successful current attempt/fence, canonical result root, ready versioned produced document, workflow link and adoption inbox/event. It rechecks every original in frozen ordinal order, accepted source version, original upload lineage, source document SHA and strict integer Canvas hash, and every declared source NPY child with typed task input/current ready version. Current project editor authorization is used independently of producer author's session; private snapshot ownership remains bound to frozen author. No registry activation, generic artifact permission, child IO or Apply granted.

Added SQLAdoptedMultiSource.Binding/LoadObject, comparable owned AdoptedMultiBinding with strict operation Params and deterministic workflow/task identity validation, and LoadAdoptedMultiResult. Reads exact versioned bounded canonical ImageResult bytes outside SQL transactions, validates size/SHA/root references, refuses generated-mask and source/calibration/root aliases, closes streams and repeats current binding plus exact locator comparison after IO. Inputs parse into fresh owned ordered bindings.

Authored both-operation root read/input ownership, producer/operation/Params/object metadata refusal before IO, post-read authority/locator substitution, malformed length/SHA/noncanonical bytes, close failure/cancellation, source reuse/generated-mask unit cases. These mocks are not SQL/RLS or photographic proof. Tests/build/typecheck NOT RUN; migration NOT APPLIED; gofmt only.

This is adopted root proof only. Exact typed child catalogue including all original source/registration occurrences, geometry and quality review, preview and final explicit Apply remain open, along with full real SQL/object fixtures and other V20–V32 gaps. No completed-module credit; whole-product implementation percentage unknown, catalogue ceiling 84.4% unchanged. Metal/MPS verification later retains 10,000,000,000-byte VRAM cap; RTX deferred.

## 2026-10-10: typed multi children and ordered report contracts — NOT RUN

Authored forward-only 01160 with photo_api-only adopted multi child and original-object functions. Produced output access requires exact adopted root/current attempt/fence and task-qualified first-visit inventory purpose, strict contract/path/role/MIME/byte allowlist; literal `*` is the pinned schema array marker, not a wildcard query. Original inputs resolve separately from frozen source ordinal and literal WorkingImage/pixels/alpha/InverseField role, exact current root/source authority and typed task input. Original legacy octet-stream pixels/alpha are explicitly bounded; produced NPY/fields remain strict. No generic artifact/workflow grant and no Apply authority.

Added SQLAdoptedMultiSource.LoadChildObject/LoadOriginalObject, exact path/role bounds, current photo_api tenant metadata helper, and LoadAdoptedMultiJSONChild. JSON child loading checks current root binding/locator before IO, exact version/length/SHA/canonical bytes, closes streams and rechecks child metadata plus root binding/locator after IO. It is byte proof only; contract/geometry validation remains a caller obligation.

Added ParseMultiImageReport with complete ordered sources/one unique registration per frame, mandatory review/no omitted frames, producer operation and finite motion-reason validation, preserving measured reasons/metrics. Added ParseMultiRegistration with exact canonical fields, expected source/target canvas, nonlinear-field presence, finite bounded matrix and determinant/H33 guards. Registration parser does not independently replay geometry, condition number, arrays or image quality; these remain required in the catalogue/quality pipeline.

Authored exact path/contract/role allowlist, legacy media/version refusal, JSON child ownership/repeated authority/locator substitution, stream SHA/length/close/cancellation, ordered report/missing/reordered/duplicate/foreign evidence and registration source/canvas/projection/singular-matrix unit cases. Tests/build/typecheck NOT RUN; migration NOT APPLIED; gofmt only. These tests provide no actual SQL/RLS or photographic evidence.

Remaining: complete typed multi catalogue including independently loaded original source documents and fields, output geometry/registration consistency, review/quality/preview and final explicit Apply, plus real SQL/object and photographic qualification. No completed-module credit; catalogue ceiling 84.4% unchanged, actual whole-product implementation percentage unknown. Later Metal/MPS verification uses 10,000,000,000-byte VRAM cap; RTX deferred.

## 2026-10-10: bounded original inventory and verified multi source documents — NOT RUN

Authored forward-only 01161: photo_api-only v29_adopted_multi_original_inventory. A single current adopted-root/original-scope gate returns the complete frozen original occurrence set (4..3930 entries), sorted ordinal/role, with exact typed task inputs, role/MIME/size and ready versioned metadata. Source artifact rows lock in artifact-ID order. This avoids repeating the whole original gate separately for every physical child. Inventory retains each frame occurrence even when refs are shared; it grants no generic artifact reads or Apply.

Added strict 8-MiB inventory decoding and owned MultiOriginalInventory, exact root/binding/locator/ordinal/role checks, mandatory per-frame WorkingImage/pixels, rejection of source/calibration/root aliases, inconsistent shared artifact metadata and conflicting physical roles. SQLAdoptedMultiSource.LoadOriginalInventory commits the short photo_api tenant transaction before object IO. Added LoadAdoptedMultiOriginals and VerifiedMultiOriginals: preflight/cancellation before callbacks, own callback inventory, read each exact versioned original JSON with bounded size/SHA/canonical bytes/stream cleanup, run existing VerifyMultiWorkingImages for ordered source/canvas/dtype/bounds/common ICC, compare every manifest child occurrence, then re-read and compare the entire inventory/root/binding after IO. Returned documents/images/inventory own their data; saved metadata is not cached authority.

Authored both-operation source loading, strict wire/unknown fields/order/missing input/foreign bucket/role/version/ref/cross-role alias refusal, document/image/inventory ownership, changed original/array/root/binding metadata, callback mutation during IO, SHA/close failure/extra child/foreign arrays and invalid/cancelled preflight unit cases. Test fixtures are synthetic metadata; no NPY bytes are read by this source-document loader.

Tests/build/typecheck NOT RUN; migration NOT APPLIED; gofmt only. NPY header/content and exact output/source geometry proof, complete typed result catalogue, review/quality/preview/final Apply and photographic qualification remain open. No completed-module credit; whole-product implementation percentage unknown, catalogue ceiling 84.4% unchanged. Later Metal/MPS verification retains 10,000,000,000-byte VRAM cap; RTX deferred.

## 2026-10-10: catalogue recount and streaming original NPY verifier — NOT RUN

Recounted existing ledger: 90 entries, statuses {'requires_semantic_review': 73, 'missing_runtime_path': 14, 'partial_runtime_path': 3}; catalogue ceiling 84.4%. This is an existing-catalogue recount, not a new exhaustive module audit. Whole-product implementation percentage remains unknown; no complete-module credit is awarded.

Authored VerifyAdoptedMultiOriginalArrays. It derives each occurrence from verified source documents and frozen Params, validates exact inventory edges, streams SHA-bound NPY through the existing bounded-memory parser, closes readers on failure/success and reloads current full inventory before/after every object read plus a final check. RGB finite HDR remains unclamped; alpha and dense-field semantics reuse existing source verifier. Shared refs are checked for every occurrence and its expected dimensions. Saved metadata is not authority. No new migration or Apply grant.

Authored stale-inventory-before-open cases for both operations, invalid NPY/stream closure and cancellation-before-callback cases. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. This helper is not yet integrated into the full typed result catalogue. Positive end-to-end fixtures and detailed corruption cases remain open. Full inventory reload per array can be quadratic in edge count; a selected-edge SQL checkpoint is still needed for efficient large collections. Complete result geometry/quality/preview/explicit Apply and photographic qualification remain open. Metal/MPS later: 10,000,000,000-byte VRAM cap; RTX deferred.

## 2026-10-10: physical multi-array fixtures — NOT RUN

Previous goal turn made progress: authored the adopted-original array verifier, unit cases and current catalogue recount. This increment authors actual float32 NPY payload fixtures, with SHA and byte sizes bound through original source JSON, frozen Params, and every inventory occurrence. Positive panorama/focus tests require all four shared-array occurrences to be read/closed, nine fresh inventory reads including the final sweep, and finite negative/above-one RGB HDR to remain accepted. Corruption cases bind actual corrupt bytes into manifests to exercise shape, dtype, NaN, alpha range and truncation semantics independently of SHA; a separate case corrupts bytes after binding to exercise SHA. Additional cases cover current version replacement during IO, cancellation and close errors, with exact stream cleanup expectations.

These are authored assertions only, not execution evidence. Tests/build/typecheck NOT RUN and migrations NOT APPLIED, per deferred-testing plan. Dense-field and shared-ref conflicting-shape fixtures remain open, as do full typed output catalogue integration, geometry/quality/preview, explicit Apply, SQL/object integration and photographic qualification. No complete-module credit and no claimed actual whole-product percent. Catalogue ceiling remains 84.4%. Full-inventory checks per array remain conservative but potentially quadratic; selected-edge SQL checkpoints are a remaining scaling requirement. Future Metal/MPS validation cap remains 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: complete multi source-read pipeline — NOT RUN

Previous turn made progress by adding actual SHA-bound NPY fixtures and corruption/authority tests. Current increment introduces LoadAdoptedMultiSourceClosure: the loader sequentially verifies original JSON/current inventory and then every physical source-array occurrence before returning an opaque VerifiedMultiSourceClosure. Document-only evidence cannot be returned through this API as a complete source closure. The zero value refuses Binding/Documents/Images access; document/image outputs remain owned. Cancellation after physical verification refuses proof. The proof describes a completed current-scope read, not reusable permission, output geometry, installed ICC, photographic qualification or Apply authority.

Authored pipeline tests for both panorama and focus stack: actual source JSON and physical arrays, required six object reads/closes and eleven fresh inventory reads, corrupt physical arrays refusing any returned proof, owned document bytes and invalid zero value. Tests/build/typecheck NOT RUN; no migrations applied. No module-completion credit. Whole-product implementation percentage remains unknown; existing catalogue ceiling remains 84.4%. Full typed adopted-output catalogue integration, registration/output geometry, quality/preview, explicit Apply and qualification remain outstanding; full inventory per array also still requires a selected-edge optimization for scale. Later Metal/MPS validation retains 10,000,000,000-byte VRAM cap; RTX deferred.

## 2026-10-10: multi output geometry cross-check — NOT RUN

Previous goal turn made progress by adding the complete source-read pipeline and typed nonzero proof. Current increment authors ValidateMultiOutputGeometry: requires physically verified original-source proof; parses exact canonical WorkingImage/MaskRef, checks float32/color/alpha/reference source lineage/orientation/max pixels, focus reference dimensions, changed-mask metadata/exact shared canvas and transform chain, every ordered registration source/target/projection, original reference transform prefix and appended matrix equality with reference registration. Cylindrical branch binds the appended dense projection step to reference field and original dimensions/lineage/half-pixel convention. Rejects output pixels/alpha/mask and projection field aliases against all original inventory refs, calibration and adopted root.

Authored planar panorama/focus positive and foreign color/canvas/matrix/mask/source-alias/missing-registration cases plus zero-proof refusal. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. This cross-check is not a numerical replay or photographic qualification; condition number/horizon, deterministic canvas naming, projection field values, output arrays and quality still require further work. Cylindrical positive fixtures and complete adopted-output catalogue integration/review/Apply remain open. No complete-module credit, actual whole-product percent unknown, catalogue ceiling unchanged at 84.4%. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: integrated adopted multi JSON catalogue — NOT RUN

Previous goal turn made progress with output geometry validation and authored unit cases. Current increment introduces LoadAdoptedMultiDocuments and MultiResultDocuments: loads exact adopted root, physically verified original closure, typed WorkingImage/changed MaskRef/report and every ordered registration through scoped versioned JSON child reads; validates geometry; rejects cross-contract aliases among all document/physical output refs, originals, calibration and root. Pins exact root/binding and each path/contract/ref locator through every repeated lookup, then performs final sweep of all earlier children, original inventory and root after later object IO. Returned typed JSON accessors reparse owned bytes, and zero values refuse access. This type explicitly does not certify output NPY bytes, quality or Apply.

Authored root version/binding and child version/byte mutation pin tests, stable pin deduplication and zero/invalid preflight refusal. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Full successful catalogue integration fixture, output NPY/warp/mask contents, numerical registration replay/quality, HTTP review/preview/explicit Apply and photographic qualification remain open. No complete-module credit, whole-product percent unknown, catalogue ceiling 84.4% unchanged. Later Metal/MPS cap remains 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: adopted multi output NPY verification — NOT RUN

Previous turn made progress with integrated typed JSON catalogue and locator pin tests. Current increment retains original root/JSON child locators in the owned catalogue and authors VerifyAdoptedMultiOutputArrays: derives RGB/alpha/changed mask from geometry-checked documents and projection fields from ordered registrations, resolves only exact adopted path/contracts, enforces current strict NPY metadata/size, reads outside SQL transactions and streams SHA/shape/dtype/finite checks with closure on every stream outcome. RGB remains unclamped finite HDR, output alpha is strict float32 unit range, and changed mask uses a new VerifyBinaryMaskNPY requiring exact zero/one values. Projection arrays require float32 HWC2 with original projection canvas dimensions. Current root and selected child are rechecked around IO; final sweep covers retained JSON plus array locators and fresh original inventory/root.

Authored binary-mask zero/one/fractional/out-of-range/nonfinite cases and zero/cancelled output verifier preflight. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Full successful catalogue/output-array fixtures remain open, as do geometry-aware all-one changed coverage, projection equations/registration conditioning/accuracy, pixel replay/quality, review/preview/explicit Apply and photographic qualification. No completed module credit; actual whole-product percent unknown, catalogue ceiling remains 84.4%. Future Metal/MPS cap 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: complete multi content-read pipeline fixtures — NOT RUN

Previous turn made progress with output-array streaming verifier and binary mask validation. Current increment authors a complete synthetic planar catalogue fixture for both operations: actual source/output NPY and source/image/mask/registration/report/root JSON, SHA-bound root/child refs, exact path/contract locator mock, current original inventory and exact versioned object reader. Tests describe successful fifteen object reads, semantic SHA-bound wrong RGB shape/alpha range/fractional mask, post-binding SHA corruption and earlier report version mutation while reading later output pixels; every case asserts stream cleanup.

Added LoadAdoptedMultiContent and opaque MultiResultContent: mandatory typed document/source and output-array validation before any content proof is returned. Tests cover complete success, corrupt mask refusing partial proof, retained review obligation and invalid zero accessors. This is completed-read content evidence, never cached authority or Apply/quality/ICC approval.

Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Fixtures are synthetic unit data, not SQL/RLS/runtime/photographic evidence. Cylindrical fixtures, deterministic canvas naming, homography conditioning/horizon, projection equations, complete geometry-changed mask coverage, numerical replay/quality and HTTP review/preview/explicit Apply remain open. No completed-module credit; actual whole-product implementation percent unknown; catalogue ceiling 84.4% unchanged. Later Metal/MPS cap remains 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: multi registration numerical safety — NOT RUN

Previous goal turn made progress with complete actual-byte pipeline fixtures and the mandatory content loader. Current increment extracts the existing one-sided-Jacobi spectral matrix safety calculation into a private shared helper while preserving perspective's original corner domain/epsilon. Adds VerifyMultiHomographySafety with owned normalized matrix, bounded finite input, worker H33 normalization, determinant/spectral condition <=1e8 and worker integer-centre horizon domain [0,W-1]x[0,H-1] with 1e-8 margin. Typed multi output geometry now checks every registration, including nonreference frames, against its source dimensions. This adds numerical safety, not feature matching/photographic accuracy.

Authored identity/scaling/translation, ill-conditioning, singularity/nonfinite, crossing/near horizon and last-pixel-centre convention cases, input ownership/dimensions/cancellation, and unsafe nonreference geometry rejection. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Existing perspective tests also remain unrun; shared-helper extraction requires later regression testing. Deterministic canvas naming, actual projection fields, geometry-changed mask coverage, cylindrical fixture coverage, image quality/replay and HTTP review/preview/Apply remain open. No completed-module credit; actual whole-product percent unknown, catalogue ceiling unchanged 84.4%. Future Metal/MPS cap remains 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: full changed coverage for changed geometry — NOT RUN

Previous goal turn made progress by wiring numerical homography safety. Current increment adds VerifyFullMaskNPY on the existing bounded-memory, SHA-bound exact-shape float32 parser. Every payload sample must be exactly one; unit/binary checks and finite checks remain in effect. Output array derivation now mirrors worker geometry_changed: output dimensions differ from reference, a reference nonlinear field exists, or exact reference homography differs from identity. Such results require full changed coverage; unchanged geometry still requires binary zero/one mask. The mode is derived from verified documents/registrations, never supplied by a caller flag.

Authored full-mask zero/fractional/one cases and extended full actual-byte pipeline fixtures for both operations with translated-reference/all-one success and translated-reference/zero-mask refusal. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Shared NPY parser changes require later regression testing; synthetic fixtures provide no photographic or SQL evidence. Deterministic canvas naming, projection-field equations and cylindrical fixtures, quality/ICC installation, HTTP review/preview/explicit Apply and production qualification remain open. No completed-module credit; actual whole-product percent unknown; catalogue ceiling unchanged 84.4%. Future Metal/MPS cap remains 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: deterministic multi canvas identities — NOT RUN

Previous goal turn made progress with geometry-aware full changed-mask coverage. Current increment adds output canvas naming from worker-equivalent canonical seed: operation/reference, complete ordered VersionBindings, all normalized registration matrices, every nullable inverse field and exact output dimensions. Every registration H33 must equal one, matching worker-normalized publication. Typed output geometry compares the resulting multi: hash prefix to actual canvas. Cylindrical reference projection canvas must exactly match source dimensions/lineage/orientation and cylindrical: hash of source canvas plus exact field. This binds naming to evidence; it does not validate actual field equations or photographic accuracy.

Updated planar/focus geometry and complete content fixtures with independently built worker-style seed maps. Authored foreign space-ID refusal, matrix/field sensitivity, unnormalized matrix refusal and projection seed parity cases. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Full cylindrical integration fixtures, projection equations, quality/ICC installation, HTTP review/preview/explicit Apply and production qualification remain open. No completed-module credit; actual whole-product percent unknown, catalogue ceiling unchanged 84.4%. Future Metal/MPS cap 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: cylindrical field coordinate replay — NOT RUN

Previous turn made progress with deterministic canvas naming. Current increment adds optional private per-sample validation to the existing bounded-memory SHA/NPY parser (existing generic callers pass nil), and VerifyCylindricalFieldNPY. Every HWC2 float32 inverse coordinate is reconstructed from worker focal=max(H,W), half-pixel centres, tan/cos projection and valid-source domain; coordinates outside valid coverage must be exact positive zero, valid coordinates require exact float32 bit equality. Adopted multi output DenseWarpField reads now use this replay rather than only finite array checks. No image clamp, precision relaxation or quality approval is introduced.

Authored coordinate/coverage corruption, finite all-zero field refusal and a fixed 3x3 centre-column golden with explicit expected coordinates. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Go math versus NumPy transcendental parity is UNQUALIFIED and must be established on runtime fixtures before operation enablement; fail-closed exact bits may reject legitimate cross-runtime differences until qualification resolves them. Full cylindrical pipeline fixtures, photographic quality/ICC, HTTP review/preview/explicit Apply and production qualification remain open. No completed-module credit; actual whole-product percent unknown; catalogue ceiling 84.4% unchanged. Future Metal/MPS cap 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: complete cylindrical content pipeline fixture — NOT RUN

Previous goal turn made progress with streaming cylindrical coordinate verification. Current increment extends complete multi pipeline fixture with actual cylindrical Params, two independently staged inverse-field artifacts, ordered registration refs, worker-seeded output/projection canvases, dense-warp then matrix reference chain and full changed mask. Success test requires seventeen object reads and complete cleanup. Additional cases bind a wrong finite field coordinate to a valid SHA, reject zero changed coverage for nonlinear geometry, and mutate the earlier field version while reading the later field; final catalogue sweep must reject that mutation.

Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Synthetic field generation uses Go math, so these fixtures do not establish NumPy/Go bit parity; cross-runtime qualification remains mandatory before enablement. No photographic or SQL/RLS proof is claimed. ICC installation/quality and runtime qualification, HTTP review/preview/explicit Apply and remaining V20-V32 modules stay open. No completed-module credit; actual whole-product percent unknown; catalogue ceiling 84.4% unchanged. Later Metal/MPS cap remains 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: multi review candidate service and HTTP read wiring — NOT RUN

Previous goal turn made progress with full cylindrical fixture coverage. Current increment adds MultiProposalService.Describe using a dedicated current Binding source, mandatory complete multi content loader, exact binding equality before/after IO and final current binding read. Returns tenant/project/producer/operation, exact adopted root/child refs, complete ordered sources/reference/registrations, canvas and retained producer reason with status=review/requires_review=true. It never executes Apply or converts producer review to PASS. Invalid/cancelled preflight refuses callbacks.

Wired GET /api/v1/tenants/{t}/projects/{id}/panorama-proposals/{task} and focus-stack-proposals/{task} through existing tenant authentication/error handling, no-store headers and injectable service. API bootstrap installs SQLAdoptedMultiSource plus versioned object store. No new grants/migrations or automatic capability enablement. Complete object scans are intentionally retained; scale work remains rather than caching authority.

Authored both-operation Describe success/corrupt mask/final binding mutation cases using actual complete pipeline fixture, exact fifteen reads and stream cleanup, plus invalid/cancelled preflight. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. HTTP integration tests, SQL/RLS/object qualification and NumPy/Go parity remain unproven. Preview/quality/explicit Apply and client review UI remain open. No completed-module credit; actual whole-product percent unknown; catalogue ceiling unchanged 84.4%. Future Metal/MPS cap remains 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: multi client review contract and HTTP auth cases — NOT RUN

Previous turn made progress by wiring Describe into HTTP/bootstrap. Current increment authors strict owned MultiProposal descriptor validation against a frozen complete MultiIntent: exact tenant/project/producer/operation, ordered exact source bindings/reference, result/child/registration nonalias refs (including source/calibration), required review status, bounded deterministic canvas shape and finite measured worker reason with declared optional inconsistency suffix. No unknown fields, locators or implicit acceptance. OwnerAPI.multiProposal freezes scope before asynchronous GET, selects operation-specific proposal route and rejects cancellation after transport.

Authored both-operation ownership/scope/order/downgrade/alias/geometry/reason/extra-field tests, asynchronous request mutation/route/options and malformed scope before network. Added HTTP tests for both registered review GET paths requiring 401 and no-store on unauthenticated reads. Authenticated routing/error cases were not covered by these auth-only tests and remain open; no broader HTTP proof claimed.

Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Client review screen, preview/quality/Apply and cross-runtime/SQL/RLS/photographic qualification remain outstanding. No completed-module credit; actual whole-product percent unknown; catalogue ceiling unchanged 84.4%. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: durable multi result handoff — NOT RUN

Previous goal turn made progress with client descriptor/OwnerAPI binding and HTTP auth cases. Current increment adds multiWorkflowID/multiProducerTask matching Go UUIDv5 run->workflow and operation:0 node key, and readMultiAttemptResult. It freezes request/ticket before IO, rejects forged run/workflow pairing before transport, reads the exact workflow, checks exact returned run/workflow, keeps draft/queued/running pending without proposal IO, accepts result lookup only for waiting_review/completed and refuses failed/cancelled/unknown states. Derived producer and frozen complete MultiIntent are passed to operation-specific proposal lookup and response is revalidated. Cancellation before/after each async boundary refuses late result delivery. No resubmission or source order mutation.

Authored panorama pending/terminal/frozen-frame/scope/forged-ticket/late-cancel cases and focus complete handoff with exact operation/task/motion policy. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Reader still needs review screen integration, and authenticated HTTP tests/preview/independent quality/explicit Apply/qualification remain open. No completed-module credit; actual whole-product implementation percent unknown; catalogue ceiling unchanged 84.4%. Future Metal/MPS cap remains 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: connected multi review panel — NOT RUN

Previous goal turn made progress with durable result handoff. Current increment adds MultiReviewView and mounts it for acknowledged attempts on the collection multi screen, including restored journals. It reads current workflow/result via frozen attempt, renders loading/pending/error/available states, explicit refresh and source/reference/dimensions/motion/inconsistent registration evidence, preserving requires-review meaning and no automatic Apply. State is scoped to API instance plus complete attempt; effect cleanup aborts replaced/unmounted reads and disposed results cannot render. Missing ticket refuses result IO. Added all new text in RU/BG/EN. This is metadata review, not a substitute for actual image preview or independent quality.

Authored DOM cases for evidence display, read-only refresh, pending->available, error retry, replaced-scope abort/late-result refusal and missing ticket. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Existing multi route DOM mocks and locale/accessibility checks need later full regression execution. Preview/independent quality/explicit Apply, authenticated HTTP/SQL/RLS/object and cross-runtime photographic qualification remain open. No completed-module credit; actual whole-product percent unknown, catalogue ceiling unchanged 84.4%. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: current scheduler multi preview source — NOT RUN

Previous goal turn made progress with connected client review panel. Current increment authors forward-only 01162 scheduler-only v29_multi_preview_source: current tenant, exact succeeded producer/run/workflow/plan, full adopted multi/original binding under frozen author principal, current attempt/fence and immutable root/Params/snapshot. Parent must be waiting_review/completed and uncancelled. This intentionally remains an observation; no dispatch, quota, publication or Apply authority. The SQL gate requires current frozen-author session authorization; an expired author session currently makes automatic preview unavailable rather than bypassing authority.

Adds SQLMultiPreviewSource with short committed photo_scheduler transaction and timeout, strict owned source identity validation, and MultiPreviewRunID binding operation/producer/result ID/SHA/attempt/fence/sRGB to a distinct deterministic run. Authored both-operation stable identity and mutation/dimension/invalid principal/producer/Params/ICC/preflight cases. Tests/build/typecheck NOT RUN; migration NOT APPLIED. SQL/RLS integration evidence absent. Preview still needs dedicated multi-source durable intent/quota/publication/materialization/seal/adoption/output route and client image display; no preview readiness claimed. Quality/Apply and qualification remain open. No completed-module credit; actual whole-product percent unknown; catalogue ceiling unchanged 84.4%. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: compiled multi preview plan and durable intent — NOT RUN

Previous turn made progress with scheduler source observation and fenced preview identity. Current increment adds tenant/project provenance to opaque MultiResultContent and PreviewDocuments bridge requiring exact source binding; returns owned canonical root/image bytes with root SHA check only after complete source/output validation. Adds MultiPreviewPlan compiler using existing media derivative contracts for fixed preview1600/preview2560, deterministic IDs/profile hashes/Params, owned outputs and exact plan validation. No task admission implied.

Authored forward-only 01163 with scheduler-only save function and immutable separate preview draft marker, FK/exclusivity constraints and result/attempt/fence/ICC uniqueness. Under tenant/project/parent/producer/root locks it repeats current complete adopted-source authority, verifies exact fenced root and UUIDv5 identity, stores complete original multi Params/snapshot/principal/variants and supports exact idempotent draft replay. Adds short-transaction MultiPreviewIntentStore.Save. No quota, object publication or queue activation.

Authored content bridge scope/SHA/ownership and zero/unverified/cancelled plan/store cases. Positive complete workflow-plan fixtures and actual SQL tests remain open. Tests/build/typecheck NOT RUN; migration NOT APPLIED. Preview quota/publication/materialization/seal/adoption/output delivery/client image display, quality/Apply and qualification remain outstanding. No completed-module credit; actual whole-product percent unknown; catalogue ceiling unchanged 84.4%. Future Metal/MPS cap 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: multi preview quota lifecycle — NOT RUN

Previous goal turn made progress with compiled preview plan and immutable draft intent. Current increment authors 01164 private current saved-intent resolver requiring exact source identity/attempt/fence/root/principal/complete multi Params/snapshot and fixed variants. Scheduler reserve locks tenant usage/draft and parent lifecycle, repeats current adopted source, enforces exact 111149056-byte budget for two preview artifacts/Params, zero files, user ownership and one-hour reservation; exact live replay does not double reserve. Storage/JSON-safe numeric ceilings fail closed. Adds 01165 release limited to terminal separate multi-preview runs with all tasks settled and uploads/backend documents ready or cleaned; usage and reservation update atomically, already released/missing reservation replay is idempotent, inconsistent usage raises error. Multi release explicitly checks current tenant scope and can settle after original source revocation without recreating source authority.

Adds short scheduler transaction Reserve/Release methods. Authored invalid/unconfigured/cancelled preflight tests plus isolated migrated-DB privilege/private-resolver/foreign-tenant refusal integration test; no migration application in tests. Successful SQL accounting/replay/cleanup scenarios remain to be authored. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Preview publication/materialization/seal/adoption/output delivery, quality/Apply and qualification remain open. No completed-module credit; actual whole-product percent unknown; catalogue ceiling unchanged 84.4%. Later Metal/MPS cap remains 10,000,000,000 bytes; RTX deferred.

## 2026-10-10: durable multi preview Params publication — NOT RUN

Previous goal turn made progress with quota reserve/release. Current increment authors 01166 private exact Params pin with dedicated backend_documents body column/byte-SHA constraint. Requires current saved multi intent, source result root/image bytes, profile hash for fixed variant, exact working source/null suffix/matte, nonnull alpha, current adopted WorkingImage/RGB/alpha typed paths, ready versioned artifacts, matching bucket, exact live run quota and separately bounded document capacity. Durable document identity uses derivative UUIDv5; exact replay reuses the same document. Source is repeated after parent/producer/root locks.

01167 reserves pending/ready immutable publication tickets and checks ready artifact/versioned path. 01168 completes only exact token/SHA/bytes/bucket/version, repeats current pin and source/quota checks, creates ready task_params artifact and atomically moves document bytes from reserved to storage. Added MultiPreviewDocuments repository and PublishMultiPreviewTasks: freeze evidence before IO, rebuild plan from verified multi content, publish both Params through existing immutable publisher outside short scheduler transactions, return prepared but inactive tasks.

Authored repository byte ownership/foreign scope refusal using generic media derivative bytes and missing-dependency/cancellation publisher preflight. These tests do not prove positive multi relational SQL/publication success; those fixtures remain open. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Materialization, closure sealing, adoption, preview delivery/client images, quality/Apply and qualification remain open. No completed-module credit; actual whole-product percent unknown, catalogue ceiling unchanged 84.4%. Later Metal/MPS cap remains 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: recount and blocked multi-preview materialization — NOT RUN

Current ledger recounted: 90 catalogue entrypoints = 14 missing runtime paths + 3 partial paths + 73 requiring semantic review. Current HTTP/runtime registration sources still have no delivery/interchange/provenance entries corresponding to the missing rows. Catalogue coverage ceiling is (90-14)/90 = 84.4%; this is NOT actual product implementation completion. Actual whole-product implementation percentage remains unknown because no exhaustive requirement-level semantic audit/weighted denominator is complete. Historical PASS and authored tests are not accepted as current evidence; no new whole-module credit.

Authored forward-only 01169: scheduler-only materialization repeats tenant/current source, published Params and live quota authority via the dedicated pin; exact WorkingImage/RGB/nonnull-alpha input order, fixed CPU float32 resource estimate, media registry/backend, canonical input digest and immutable blocked replay. Both task insertions execute in one short scheduler transaction. Adds MaterializeMultiPreviewTasks rebuilding the plan from opaque validated multi content before SQL. Shared renderDerivativeTaskBodies prevents altered task operation/version/class/timeout/inputs/node/Params/profile/digest from being registered. Existing perspective helper delegates to the shared implementation.

Authored generic media task substitution/pair completeness and cancelled/unconfigured preflight tests; fixture is not evidence of successful multi relational admission. Tests/build/typecheck NOT RUN, migration NOT APPLIED. No graph activation: current full source/output locator closure seal, scheduler orchestration/adoption and preview delivery/client image display remain open, as do explicit Apply, photographic qualification and broader V20–V32 gaps. Later Metal/MPS memory cap remains 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: retain completed multi-output read witnesses — NOT RUN

Previous goal turn changed authoritative code with blocked preview materialization (01169), so classified as progress. Current increment preserves the final pinned output catalogue from array verification inside opaque MultiResultContent. Capture occurs only after all array byte/shape/range/projection checks, final child locators, original inventory and root authority checks succeed, followed by cancellation check. Existing public verifier keeps its error-only API. No partial evidence is returned on failure.

PreviewWitnesses requires exact verified tenant/project/frozen binding via PreviewDocuments and nonempty completed capture; returns owned canonical root plus every typed output JSON/array locator with exact ref/path/contract/key/version/SHA/size/MIME. Includes registration inverse fields. This is evidence of a completed read only, not current authority; complete original source inventory remains a separate mandatory SQL gate. No graph activation is added.

Authored actual planar panorama/focus-stack/cylindrical multi fixtures checking required output arrays and projection fields, complete root/catalogue, owned serialization, foreign scope/missing capture refusal, plus failed RGB shape and cylindrical field cases preventing capture. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. SQL current witness gate/seal, scheduler orchestration, adoption/delivery/client preview images, explicit Apply and photographic qualification remain open. Whole-product implementation percent unknown; catalogue ceiling unchanged 84.4%, no new module credit. Later Metal/MPS VRAM cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: private current multi-preview output witness gate — NOT RUN

Previous turn changed opaque verified content and authored owned final witness capture, so classified as progress. Current increment authors forward-only 01170 private v29_multi_preview_witnesses_current. Current tenant/active tenant/usage/preview draft, saved source attempt/fence/root and author authority are repeated under lifecycle locks, with artifact UUID lock order. Hashed root/image bind required ImageResult/WorkingImage/MaskRef/ImageReport/RGB/nonnull-alpha singleton refs. Exact total, unique artifact identities, one mask array, one registration per frame and cylindrical field count prevent missing/duplicate categories. Every occurrence is resolved through current adopted root/typed-child authority and exact key/version/SHA/bytes/MIME/bucket compared. Unexpected paths/roles and stale locators fail closed. No direct scheduler/API/worker/portal/gateway EXECUTE; fnowner-owned private helper only.

This gate is not a complete seal: original inventory read witnesses still require comparison; mask/report/registration body relationships and full geometry proof must remain bound through the verified-content interface and final admission design. No dispatch or activation added. Authored isolated migrated-DB function privilege test only; positive success, malformed witnesses, source revocation/attempt/fence change and locator mutation SQL scenarios remain to be authored. Tests/build/typecheck NOT RUN; migration NOT APPLIED. Scheduler orchestration/seal/adoption/preview delivery/client images, explicit Apply and qualification remain open. Actual whole-product percent unknown; catalogue ceiling 84.4%, no new completed module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: exact multi-preview original inventory evidence — NOT RUN

Previous turn authored private output locator comparison (01170), classified as progress. Current increment adds PreviewOriginalInventory on opaque verified content: validates exact scope/binding and complete original inventory, requires root locator equality with verified output root, serializes owned canonical evidence using existing SQL-compatible wire. Includes all ordered original occurrences, WorkingImage/RGB/optional alpha/inverse fields, bucket and immutable locators, exact binding and root metadata.

Authored forward-only 01171 private v29_multi_preview_originals_current. Repeats active tenant/usage/draft saved intent and source attempt/fence under project/parent/producer/root locks, obtains current complete original inventory through existing author/project/frozen-source authority and UUID-ordered artifact locks, compares exact JSONB binding/root/ordered occurrence list. Omission/addition/reordering/changed version fail equality. Private helper only, no direct service-role EXECUTE or activation.

Authored actual panorama/focus verified-content serialization round-trip against strict decoder, ownership, foreign tenant/unversioned source/zero-content refusal; expanded private-gate privilege integration case. Tests/build/typecheck NOT RUN; migration NOT APPLIED. SQL success/stale/revoked fixtures, full body-bound atomic seal and scheduler orchestration/adoption/delivery/client preview images/explicit Apply/qualification remain open. Whole-product implementation percent unknown; catalogue ceiling unchanged 84.4%, no new completed-module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: atomic draft-only multi-preview graph seal — NOT RUN

Previous turn authored exact original inventory evidence and private current comparison (01171), classified as progress. Current increment authors forward-only 01172 scheduler-only seal_v29_multi_preview: current tenant, active tenant/usage/preview run locks, exact root hash, draft/unsealed/uncancelled only; both private current-original-inventory and current-output-witness gates must return true. Exactly two blocked uncancelled tasks and two ready Params documents required; each task is rebuilt from stored fields and rechecked through exact materialization/pin, including current source/quota, fixed media registry/backend/profile/inputs/digest. Only after both pass does one transaction seal/run the graph and set both tasks ready. Sealed replay is refused, not reactivated; future orchestration must observe existing state after unknown outcome.

Adds SealMultiPreview short scheduler transaction, rebuilding plan from opaque fully verified multi content and serializing owned original/output evidence before SQL. Content interface establishes typed body/geometry and physical byte proof; SQL independently checks locators/current scope, not image bytes. No public arbitrary-body HTTP entrypoint added. Authored invalid/unconfigured/cancellation preflight tests and scheduler-only seal privilege/foreign-tenant refusal integration cases. Positive activation, stale/race/rollback/current-session scenarios remain to be authored. Tests/build/typecheck NOT RUN; migration NOT APPLIED. Scheduler orchestration, adoption, output access/delivery/client images, explicit Apply and photographic qualification remain open. Whole-product implementation percent unknown; catalogue ceiling remains 84.4%, no new completed-module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: fenced multi-preview graph receipt — NOT RUN

Previous turn authored draft-only atomic seal (01172), classified as progress. Current source inspection confirms existing SQLAdoptedMultiSource/child/inventory readers explicitly require photo_api; scheduler coordinator cannot reuse those with its scheduler pool. Do not bypass this role check. Dedicated scoped scheduler read adapter remains required before production orchestration/bootstrap.

Authored forward-only 01173 scheduler-only v29_multi_preview_receipt with current tenant, exact operation/attempt/fence, current adopted source/root/result/ICC and saved source binding, graph sealed and supported running/terminal states, exactly two media tasks and matching ready dedicated Params documents. Adds typed MultiPreviewReceipt and short committed ReadReceipt plus exact run/result/deterministic task/state validation. This observes a graph only, not image content acceptance or preview availability; intended for unknown-outcome recovery without reactivation.

Authored synthetic identity/state substitution and cancelled/zero-plan preflight tests; synthetic plan is not positive source/SQL admission evidence. Tests/build/typecheck NOT RUN; migration NOT APPLIED. Scheduler scoped object adapter/coordinator/discovery/bootstrap, adoption/delivery/client image display, explicit Apply and quality qualification remain open. Whole-product implementation percent unknown; catalogue ceiling unchanged 84.4%, no new completed module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: dedicated scheduler multi catalogue and preparation coordinator — NOT RUN

Previous turn authored fenced graph receipt (01173), classified as progress. Authored 01174 scheduler-only root/typed-child/original-inventory readers. Each derives project/author/session from current frozen producer observation, enforces exact root/attempt/fence and delegates to existing strict adopted/source authority. API role checks and existing API function permissions remain unchanged. SQLMultiPreviewCatalogue binds caller scope/principal to one validated observation; short scheduler transactions commit before object IO, strict locators and inventory decoder remain mandatory.

Added MultiPreviewCoordinator.Prepare: resolve current source, fully verify original/output content through dedicated scheduler catalogue, enforce working ICC digest, compile fixed plan, observe sealed receipt, save/reserve/publish/materialize/seal/read receipt. Failures leave resumable draft work; no unsafe quota release while uploads/documents/tasks remain unsettled. Concurrent/unknown-outcome recovery after Save/materialize/seal failure still needs receipt observation logic; current next preparation observes receipt before resuming. No runtime discovery/bootstrap yet.

Authored foreign principal/project/root-alias and cancellation catalogue preflight, coordinator missing config/operation/cancellation tests. Tests/build/typecheck NOT RUN; migration NOT APPLIED. Positive SQL/IO/races/privileges, discovery/bootstrap, adoption/output delivery/client preview images, explicit Apply/photographic qualification remain open. Actual whole-product percent unknown; catalogue ceiling 84.4%, no new module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: multi-preview discovery and scheduler loop — NOT RUN

Previous turn authored dedicated scoped scheduler catalogue/coordinator (01174), classified as progress. Authored forward-only 01175 bounded next_v29_multi_preview_producer: active tenants, succeeded canonical uncancelled panorama/focus producers in sealed live v2 parents, UUID cursor order, exact operation/result/attempt/fence/ICC exclusion of already sealed/cancelled/terminal preview. Discovery is cross-tenant scheduler observation only; subsequent source/byte/admission gates remain mandatory.

Adds SQLMultiPreviewProducers with short committed scheduler discovery transaction, strict returned operation/scope/cursor; MultiPreviewProcessor advances cursor before preparation so one revoked/broken source does not starve others, wraps after complete scan, bounds preparation to five minutes and requires exact receipt task IDs/state. Scheduler service now requires new discovery/seal/receipt/source-inventory functions, constructs coordinator/processor, starts cancellable joined expansion loop, includes readiness and materialized/failure metrics. Existing optical registry remains disabled by default; no qualification override.

Authored processor failure/advance/wrap, operation forwarding, terminal/invalid state, scope, foreign operation, cancellation and unavailable SQL source tests. Tests/build/typecheck NOT RUN; migration NOT APPLIED. Positive SQL/bootstrap/runtime/races evidence, successful/failed preview result finalization and cleanup/quota lifecycle loop, preview delivery/client images, explicit Apply and photographic qualification remain open. Whole-product implementation percent unknown; catalogue ceiling unchanged 84.4%, no module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: multi-preview media result observations — NOT RUN

Previous turn authored multi-preview discovery/processor/service loop (01175), classified as progress. Authored 01176–01178 dedicated scheduler result/evidence/context reads. Result requires current tenant and source binding/attempt/fence/root, sealed running preview and succeeded uncancelled media task, canonical media result from its current attempt/fence, ready exact Params artifact, exactly one encoded derivative and one manifest with exact ready upload metadata. Evidence only returns exact output or declared task input occurrences for that current result; context binds Params and manifest IDs/SHA for later physical verification.

Adds MultiPreviewResults.Read using short committed scheduler transaction and strict pair validation: deterministic variant task, same result/attempt/fence/Params, distinct artifacts and roles, configured bucket/version/SHA/size/MIME. Empty set means no current result; no encoded byte acceptance/download grant implied. Authored pair substitution/partial-pair/unconfigured/cancellation cases. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Physical encoded/manifest verification, durable acceptance receipts, success/failure finalization, cleanup/quota lifecycle, delivery/client image display, explicit Apply and qualification remain open. Whole-product implementation percent unknown; catalogue ceiling unchanged 84.4%, no new completed module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: multi-preview physical manifest and encoded hash read — NOT RUN

Previous turn authored current result/evidence/context and strict metadata pair (01176–01178), classified as progress. Inspection found shared strict RenderDerivativeManifest parser, but no complete Go encoded-format/ICC/metadata validator. Current increment adds LoadMultiPreviewResult: rebuild exact fixed plan from opaque verified content, read current pair, require exact published Params, hash-read canonical manifest and validate source/profile/geometry/alpha/MIME through existing parser, stream encoded bytes with bounded memory/exact SHA/length/close/cancellation, then re-read and compare full current pair after IO. No open SQL rows during object IO.

This is deliberately not final acceptance: encoded decoder/container validity, embedded ICC and stripped metadata still need independent verification before acceptance receipt/finalization. Hash-only proof must not be treated as image validity. Authored synthetic arbitrary byte hash/short/long/corruption/close/cancel tests, explicitly not encoded image qualification. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Complete encoded validation, acceptance receipts, lifecycle/cleanup/quota release, delivery/client image display, explicit Apply/photographic qualification remain open. Whole-product implementation percent unknown; catalogue ceiling 84.4%, no new module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: shared full-decode multi-preview acceptance path — NOT RUN

Previous turn authored manifest/hash reads, classified as progress. Current authoritative inspection located full existing Python encoded validation in v2/render_derivative_adoption.py and render_derivative.py; Go hash loader remains explicitly non-acceptance. Added dedicated PostgresMultiPreviewReader with scheduler role/tenant/idle TX checks, fresh result/evidence on every locator read, changed-version refusal, closed object streams and exactly four owned locator witnesses. Added MultiPreviewAdoptionCoordinator using the existing shared verify_render_derivative_result (schema/source/working ICC/target ICC/full encoded decode/metadata policy/geometry). No replacement/simplified codec validator. Validation completes outside SQL transaction; acceptance/rejection commits through dedicated fenced functions.

Authored 01179 immutable dedicated receipt column/guard and record function with current original author/source and producer/result/attempt/fence locks, exact manifest and final four locator evidence checks. 01180 rejects invalid encoded/schema result only while current and without accepted receipt. 01181 completes preview only when both succeeded exact tasks/Params have acceptance receipts revalidated under locks. These are components only: no new maintenance runner/bootstrap yet; no current runtime acceptance claimed.

Authored dedicated reader and coordinator boundary tests adapted from existing perspective tests (role, transactions, locator substitutions, commit/rejection paths). Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Positive SQL/full real-image integration, maintenance lifecycle/cleanup/quota loop and installation, client delivery/images, explicit Apply and photographic qualification remain open. Whole-product percent unknown; catalogue ceiling unchanged 84.4%, no new module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: multi-preview maintenance and adopter installation — NOT RUN

Previous turn authored full-decode acceptance components (01179–01181), classified as progress. Authored 01182–01185 bounded receipt/completion/lifecycle pages and tenant discovery. Dedicated lifecycle uses frozen complete multi Params/snapshot/source attempt/fence/root/principal instead of single-source perspective base; live reservation requires exact multi budget/files/user/expiry. Revoked source/expired quota requests cancellation only; failed settled task pairs become failed. Terminal quota releases only through existing multi release settlement gate; does not delete objects or release active writers.

Added MultiPreviewReceiptMaintenance/Runner/Schema using independent constant-memory lifecycle/receipt/completion cursors, per-row retry continuation, bounded tenant pages and post-receipt completion recovery. Exact ABI/owner/secdef/EXECUTE startup gate added. Python adopter installs full-decode coordinator/runner when multi installations exist and trusted ICC hashes are configured; run_once merges adoption/rejection/finalization/retry counts. Registry/default qualification restrictions retained. Generic cancelling-state settlement and pending upload/document cleanup remain dependencies to verify; no end-to-end readiness claimed.

Authored dedicated maintenance/runner/schema boundary tests adapted from existing perspective counterparts. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Positive migrated SQL/races/full-image/runtime installation, cleanup settlement, preview delivery/client images, explicit Apply and photographic qualification remain open. Whole-product implementation percent unknown; catalogue ceiling unchanged 84.4%, no new completed module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: accepted multi-preview output access and signing service — NOT RUN

Previous turn authored receipt/completion/lifecycle runtime installation (01182–01185), classified as progress. Authored 01186 photo_api-only current caller/project/operation/result-bound output lookup. Requires completed sealed uncancelled multi-preview with exact original producer attempt/fence/ICC/root, both accepted receipts, current task canonical result and media attempt/fence, all four accepted locations still ready/versioned/unexpired and exact upload/result membership. Single-source asset-version dependency replaced by operation and full adopted multi authority.

Added MultiProposalService.Preview and metadata object validator: current content-verified Describe, scoped SQL output lookup, sign exact version outside transaction with one-minute TTL, repeat full authorization/locator comparison after signing before disclosure. Dedicated response identifies tenant/project/operation/producer/result, variant/artifact/SHA/size/MIME/dimensions/expiry. Missing DB/signer/profile fails closed. Service fields added but HTTP/bootstrap/client are not yet connected. Authored strict metadata mutation/unconfigured preflight tests. Tests/build/typecheck NOT RUN; migration NOT APPLIED. Positive migrated SQL/signing/revocation/version races, HTTP/client display, explicit Apply and photographic qualification remain open. Actual whole-product percent unknown; catalogue ceiling unchanged 84.4%, no completed module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: multi-preview HTTP and API bootstrap — NOT RUN

Previous turn authored accepted preview output lookup/signing (01186), classified as progress. Adds GET /api/v1/tenants/{t}/projects/{id}/{panorama-proposals|focus-stack-proposals}/{task}/previews/{variant}. Current authenticated tenant request, nonzero project/task and fixed variants required; no-store set before failures. Optional MultiPreviewReader capability preserves existing Describe-only test/service implementations; missing capability yields dependency unavailable. Service handles current full source/content authorization, accepted receipt-bound locator and exact-version signing with post-sign recheck.

API bootstrap configures DB, storage signer and trusted deployed sRGB digest on MultiProposalService. Authored all four route variant authentication/no-store tests. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Authenticated tenant/project/revocation/signing/race integration and client strict parser/download/image display states remain open, as do explicit Apply and photographic qualification. Whole-product implementation percent unknown; catalogue ceiling remains 84.4%, no completed module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: client multi-preview parser and verified download — NOT RUN

Previous turn wired authenticated HTTP/bootstrap, classified as progress. Adds strict MultiPreview response parser with exact fields, tenant/project/operation/producer/result/variant identity, artifact SHA/nonzero UUID and no alias with source/JSON/registration roles, bounded size/dimensions, exact no-upscale ties-to-even media dimensions, WebP required for multi nonnull alpha, short unexpired URL with explicit immutable version and no credentials/hash. Returns owned response.

OwnerAPI.multiPreview validates/freeze-copies proposal against original MultiIntent before async transport, checks variant and cancellation before/after request, selects exact panorama/focus route, parses response against owned scope. downloadMultiPreview freezes response, omits credentials/referrer/cache, refuses redirects, streams bounded exact bytes with MIME/length/SHA and cancellation cleanup. Reuses established behavior by dedicated adaptation; does not treat download SHA as photographic qualification. Authored response scope/expiry/alias/dimension/URL substitution and byte-stream corruption/overflow/credential cases adapted from perspective suite. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. OwnerAPI integration and image UI states, authenticated runtime evidence, explicit Apply and qualification remain open. Whole-product percent unknown; catalogue ceiling unchanged 84.4%, no new module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: mounted multi-preview review image UI — NOT RUN

Previous turn authored strict client descriptor/API/download, classified as progress. Added MultiPreviewView mounted for available MultiReviewView proposal with original MultiIntent. Downloads verified bytes through API scope, decodes via web createImageBitmap or native Image.getSize, requires exact response dimensions, creates owned blob/data URI only after verification. Active scope/variant/API guards suppress stale images, unmount/refresh/scope cleanup aborts requests and revokes URLs; expiry removes display. Adds fixed 1600/2560 switch and reload, loading/unavailable states and RU/BG/EN strings. Uses existing shared base64 helper for native output.

Authored DOM failure/retry/variant and pending-unmount cancellation tests; parent review metadata tests explicitly mock child preview to preserve their scope. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Positive real decoded image, expiry/late completion/native/web visual runtime and authenticated full system evidence remain open; no quality PASS or Apply implied. Explicit Apply/photographic qualification and broader V20–V32 gaps remain open. Whole-product percent unknown; catalogue ceiling 84.4%, no new module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: fenced multi-quality plan — NOT RUN

Previous turn mounted multi-preview UI, classified as progress. Re-read multi geometry/review requirements and existing perspective quality plan. Adds MultiQualityRunID binding operation/producer/result ID/root SHA/attempt/fence to separate immutable run. CompileMultiQualityPlan requires opaque fully verified multi content matching source; target exact float32 output WI/RGB/nonnull alpha, master_v1 working target, CPU float32 RAM/scratch budget and zero VRAM. Baseline null/protected/seams empty preserve truth: original reference cannot be relabeled as same-pixel baseline after geometry change, and no synthetic source-space mask is asserted. Generic quality task does not prove seam/registration/motion quality; original multi report and human review remain mandatory.

PrepareMultiQualityTask repeats fenced run/task, exact resource/profile/Params/inputs/digest validation, rejects aliases and requires exactly three target inputs. Authored both-operation identity stability/attempt/fence/result/SHA and zero/unverified content/plan cases. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Positive complete workflow compiler fixture, dedicated durable quality lifecycle/acceptance/review, explicit Apply transaction/client integration and photographic qualification remain open. Whole-product implementation percent unknown; catalogue ceiling 84.4%, no new module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: durable multi-quality draft intent — NOT RUN

Previous turn authored quality identity/compiler, classified as progress. Authored forward-only 01187 separate runs.v29_multi_quality_task_id with tenant task FK, preview-kind/source marker exclusivity and exact result/attempt/fence unique index. Immutable insert/update guard requires fnowner creation and prohibits scope/config changes. Scheduler Save repeats common current complete adopted multi source under tenant/project/parent/producer/root locks, requires exact result/attempt/fence/root and deterministic V29.MultiQuality UUIDv5, stores complete multi Params/snapshot/frozen principal and fixed master/master_v1. Exact replay returns same run, changed or cancelled intent fails closed. No quota/object publication/task activation.

Adds MultiQualityIntentStore.Save with exact compiler/task validation and committed short scheduler transaction; null/wrong identity is stale. Authored unverified/zero and cancelled preflight. Tests/build/typecheck NOT RUN; migration NOT APPLIED. Positive SQL identity/privilege/race cases and complete quality quota/Params/materialization/seal/acceptance/review/Apply remain open. Whole-product implementation percent unknown; catalogue ceiling remains 84.4%, no new module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: multi-quality quota lifecycle — NOT RUN

Previous turn authored immutable multi-quality draft (01187), classified as progress. Authored 01188 private exact saved-source resolver with original multi Params/snapshot/actor/session/root/attempt/fence and fixed master/master_v1. Scheduler reserve requires draft/unsealed/uncancelled, repeats source after tenant/usage/run/project/parent/producer/root locks, reserves exact 2097152 bytes/files0/user for one hour, capacity/JSON-safe bounds, exact live replay without double charge. 01189 terminal-only release waits all tasks settled and uploads/backend documents ready/cleaned, supports missing/already released replay and atomically decrements usage/reservation with inconsistency refusal. Source revocation does not prevent terminal settlement.

Adds MultiQualityIntentStore Reserve/Release with short scheduler transactions and plan validation. Authored budget/preflight/cancellation tests only. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Positive quota accounting/privilege/replay/race SQL evidence and quality Params publication/materialization/seal/adoption/review/Apply remain open. Whole-product percent unknown; catalogue ceiling unchanged 84.4%, no new module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: immutable multi-quality Params publication — NOT RUN

Previous turn authored quality quota (01188–01189), classified as progress. Authored 01190 dedicated body column/byte-SHA constraint and private Params pin. Requires current saved multi quality intent under lifecycle locks, exact root/image SHA and float32 nonnull alpha, exact master/master_v1/null baseline/empty protected/seams Params, current adopted WI/RGB/alpha typed paths and configured bucket, exact live 2MiB run reservation/user/files. Deterministic quality task owner; exact replay; separately capacity-bounded document reservation.

01191–01192 dedicated reserve/completion tickets repeat pin and exact immutable document path/token/SHA/bytes/bucket/version, ready replay and atomic reserved→stored accounting. MultiQualityDocuments rebuilds plan from opaque verified content, checks exact Params/profile/inputs, owns evidence strings; PublishMultiQualityTask uses existing immutable publisher outside short SQL transactions and returns inactive prepared task. Authored foreign scope/unverified constructor/unconfigured/cancelled publisher tests only. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Positive SQL/object publication/privileges/races and quality materialization/seal/adoption/review/Apply remain open. Whole-product implementation percent unknown; catalogue ceiling 84.4%, no new module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: повторный статический расчёт и blocked multi-quality materialization — NOT RUN

Пересчитаны строки текущего реестра: 90 entrypoints, 14 missing_runtime_path, 3 partial_runtime_path, 73 requires_semantic_review. (90−14)/90×100 = 84,4% — верхняя граница покрытия каталога, НЕ реальная готовность системы. Частичные и непроверенные по полноте пути не получают полного зачёта. Полный знаменатель нормативных требований и семантический зачёт отсутствуют; whole_product_implementation_percent=null. Текущая сборка не подтверждена историческими PASS.

Добавлена forward-only 01193: scheduler-only materialize_v29_multi_quality_task повторяет pin актуального multi quality intent, ready Params/object version, точные три WI/RGB/nonnull-alpha inputs, task identity, registry, master CPU float32 profile, ресурсные лимиты и digest; создаёт только blocked task, точный blocked replay. Go MaterializeMultiQualityTask пересобирает план из opaque verified content, сравнивает Params/profile/inputs и полный PreparedTask перед короткой SQL-транзакцией. Отдельный seal/dispatch не разрешён этим шагом. Написаны unit-тесты отказа unverified plan/content и отмены до IO. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Положительные SQL/dispatch substitution/race случаи, полный quality seal/adoption/review и multi Apply остаются открытыми. Metal/MPS для будущей проверки, cap 10,000,000,000 bytes; RTX отложен.


## 2026-10-10: multi-quality witness seal — NOT RUN

Previous turn changed authoritative code (01193 blocked quality materialization): progress. Added dedicated 01194 output locator witness gate and 01195 original occurrence inventory gate bound exclusively to current draft multi-quality intent, producer/result/attempt/fence and frozen actor/session. No preview marker substitution. Gates lock and re-resolve exact artifact versions, hashes, roles and complete original occurrence inventory. Opaque MultiResultContent remains responsible for prior physical NPY/JSON/geometry verification; SQL locator comparison alone is not a byte proof.

01196 scheduler-only draft seal requires exactly one blocked quality.check:master and one ready Params document, repeats both gates and exact materialization/profile/digest/quota checks, then atomically sets sealed/running and ready. Sealed replay refused; later coordinator must observe receipt. SealMultiQuality recompiles verified content, compares full plan Params/profile/inputs and passes owned originals/witnesses in short transaction. Authored cancellation/unverified preflight unit tests only. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Positive SQL race/privilege/geometry witness cases, receipt/coordinator/adoption/lifecycle/review and Apply remain open. Whole-product percent unknown; 84.4% is catalogue ceiling only, no module credit. Later Metal/MPS 10,000,000,000-byte cap; RTX deferred.


## 2026-10-10: multi-quality acknowledgement and coordinator — NOT RUN

Previous turn implemented dedicated original/output gates and seal: progress. Added 01197 scheduler-only read acknowledgement for sealed multi-quality run, exact operation/producer/result/root/attempt/fence, fixed master/master_v1, one CPU quality.check task and one ready dedicated Params. This is graph acknowledgement, not report acceptance or quality PASS. Go ReadReceipt validates run/result/deterministic task and allowed running/terminal states.

MultiQualityCoordinator resolves current frozen producer authority, loads full opaque verified multi content outside transactions using existing scheduler catalogue, enforces trusted Working ICC, compiles quality plan, observes receipt before mutations, saves/reserves/publishes/materializes/seals and reads receipt. Mutation errors attempt same-plan receipt recovery for committed concurrent/unknown outcomes; failed observation preserves original error, cancellation preserved. No source-authority, byte-proof or quota bypass. Authored identity/state substitution and coordinator cancellation/unconfigured preflight unit tests. Positive full workflow/database/object/unknown-outcome race tests remain open. Coordinator is not yet installed in scheduler discovery. Tests/build/typecheck NOT RUN; migration NOT APPLIED. Quality report adoption/lifecycle/review/Apply still open. Whole-product percent unknown; catalogue ceiling 84.4%, no module completion credit. Later Metal/MPS 10,000,000,000-byte cap, RTX deferred.


## 2026-10-10: installed multi-quality scheduler discovery — NOT RUN

Previous turn authored coordinator/acknowledgement: progress. Added 01198 bounded scheduler-only cross-tenant discovery of succeeded panorama/focus producers under active tenant/live sealed v2 parent. Excludes sealed/cancelled/terminal quality runs for exact result/operation/attempt/fence; incomplete drafts remain retry candidates. Scoped coordinator repeats complete live authority before writes; discovery itself grants no artifact access.

SQLMultiQualityProducers commits short role-checked discovery transaction before object IO. MultiQualityProcessor advances cursor before bounded five-minute Prepare, wraps at end and validates deterministic quality task plus allowed acknowledgement state. Installed in scheduler startup SQL-presence requirements, dedicated joined expansion loop, readiness and materialized/failure metrics. Existing quality registry/optical qualification policy unchanged. Authored fairness/retry/wrap/deadline/scope/state/cancellation/unconfigured/foreign-operation tests by dedicated adaptation. Tests/build/typecheck NOT RUN; migration NOT APPLIED. Startup privileges/ABI and full current integration evidence still required. Quality report adoption, terminal cleanup, review/Apply and broad V20–V32 completeness remain open. Whole-product implementation percent unknown; catalogue ceiling 84.4%, no new completed module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: multi-quality report read/validation/immutable acceptance — NOT RUN

Previous turn installed scheduler processor: progress. Added 01199–01201 scheduler-only current canonical quality result, exact output/upload locator evidence and target/Params/report context. Uses common adopted multi producer source with operation and saved result/root/attempt/fence, dedicated quality marker, fixed master/master_v1. Result requires succeeded current canonical task result, one ready versioned JSON report ≤1MiB and exact ready output upload. No broad table read grants.

01202 dedicated immutable backend-document receipt with fnowner write guard and locked result/task/source/output/Params locator rechecks; binds manifest bytes/SHA, target/profile/status and exact two validator read witnesses. 01203 rejects only exact canonical attempt/fence with allowlisted contract/decision/binding failures and no accepted receipt. Dedicated Python multi quality reader/adopter/validator checks current evidence before/after IO, idle short scheduler transactions, canonical strict schema/bytes, no alias, target/profile binding and recomputed decision from reported technical metrics. Valid fail/review retained, never promoted. This is technical evidence validation, not independent metric recomputation or proof of seam/motion/registration quality; multi report/human review still required. Adopted existing perspective behavior with dedicated names and scope; authored corresponding reader/validator/adoption unit suites. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Adopter installation/ABI, maintenance/finalization/quota cleanup and explicit review/Apply remain open; positive SQL role/race/revocation evidence still missing. Whole-product percent unknown; catalogue ceiling 84.4%, no completed module credit. Later Metal/MPS 10,000,000,000-byte cap; RTX deferred.


## 2026-10-10: installed multi-quality completion/lifecycle runner — NOT RUN

Previous turn authored report acceptance: progress. Added 01204–01208 success finalization with locked immutable receipt replay, recovery completion scan, source/quota loss cancellation request, settled task failure and guarded terminal quota release, bounded pending receipt/lifecycle/tenant discovery. Multi source comparison uses exact original snapshot/Params plus current producer/result/attempt/fence/frozen author; removed perspective single-source base fields. No object deletion or early live-writer quota release.

Dedicated Python maintenance/runner/schema gate installed in Adopter when qualified multi installations and trusted ICC are present, exact SQL ABI/secdef/fnowner/EXECUTE checked on both connections. Three independent bounded cursors, per-row retry continuation, cancellation propagation and merged adoption/finalization/retry counts. Authored adapted maintenance/runner/schema tests and explicit defaults in lightweight Adopter runner fixtures. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Static inspection found v2 foundation cancellation settlement is restricted to kind=v2, whereas multi-quality is kind=preview: preview-specific cancelling settlement/cleanup still requires inspection/implementation before lifecycle can be claimed complete. Current cancel path intentionally requests only and does not release live writer quota. Positive DB race/role/revocation/accounting and full runtime evidence remain open, as do review/Apply. Whole-product percent unknown; catalogue ceiling 84.4%, no module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: cancellation correction and caller-authorized multi-quality review — NOT RUN

Previous turn installed lifecycle/adopter runner: progress. Re-read authoritative generic cancellation chain: 00073 cancel_idle_task handles all run kinds and fences idle attempts; workflow/recovery.go calls it plus finalize_run_cancellation. Latest replacement 00419 finalize_run_cancellation is not restricted to v2; it requires terminal tasks and batch readiness. Prior statement that preview required its own cancellation finalizer was premature, based only on v2 foundation-specific paths. No duplicate finalizer added. Object publication cleanup/accounting and actual cancellation race behavior still require deep tests; terminal quota release remains gated on ready/cleaned uploads/documents.

Added 01209 photo_api-only multi-quality review read: caller-authorized adopted multi binding, exact operation/project/current producer result/root/attempt/fence, completed sealed dedicated quality run, master/master_v1 and one immutable receipt. Rechecks quality canonical result attempt/fence, both validator locator versions, ready report artifact and matching output upload before returning report. Does not approve/apply or turn review/fail into pass. Adapted perspective query with no single-source version/base fields and explicit tenant scope. Tests/build/typecheck NOT RUN; migration NOT APPLIED. Go/API/client quality integration, SQL privileges/revocation/race coverage and explicit multi Apply remain open. Whole-product percent unknown; catalogue ceiling 84.4%, no completed module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: multi-quality backend/API review and Apply decision gates — NOT RUN

Previous turn added caller-authorized review SQL/corrected cancellation finding: progress. MultiProposalService.QualityReview first loads current fully verified proposal, then reads 01209 with photo_api role and tenant Editor authorization; no-row stale, checks report SHA/size, target WorkingImage/master/master_v1, deterministic quality task and allowed decision. Exposes GET panorama-proposals/{task}/quality and focus-stack-proposals/{task}/quality through optional interface, explicit UUID validation, authorization, no-store and mapped errors; existing Describe-only mocks retained.

Added MultiApplyDecision exact source result/root/quality run/result/report binding, mandatory geometry reset confirmation, hard fail rejection and review warning acknowledgement. This is decision validation only, not Apply authority or mutation; commit still owes locked current source/quality/CAS/multi-lineage/new asset/version transaction. Authored adapted report/task/hash/target substitution and pass/review/fail/confirmation unit cases plus HTTP unauthorized/no-store routes. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Authenticated SQL/API races and full schema/decision integration evidence, client review UI and multi Apply remain open. Whole-product percent unknown; catalogue ceiling 84.4%, no module completion credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: client multi-quality parser/API — NOT RUN

Previous turn added backend/API quality read and decision gates: progress. Added MultiQuality strict exact-field parser, revalidates expected/returned MultiProposal against original MultiIntent, exact scope/result/root/WI, changed mask/report/ordered registrations/reason/canvas, nonzero report/run/task/result refs and report no-alias against sources/calibration/proposal closure. Validates finite bounded metrics, master/master_v1/working/null reference and zero unmeasured seams; recomputes technical decision/reasons and retains valid pass/review/fail. Returns owned data. This does not certify seam/motion/registration quality or independently remeasure pixels. Report SHA format validated; canonical report hash validation is not yet implemented on client (backend validates stored bytes).

OwnerAPI.multiQuality freezes intent/proposal before asynchronous request, exact panorama/focus-stack quality route and cancellation before/after transport. Authored both-operation ownership/status/substitution/source-alias/stale-geometry/fabricated-pass/async mutation and pre-cancelled transport tests. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Mounted quality UI, client Apply and authoritative multi-lineage/CAS Apply transaction remain open; current authenticated runtime/SQL/visual evidence absent. Whole-product percent unknown; catalogue ceiling 84.4%, no module completion credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: mounted multi-quality review UI — NOT RUN

Previous turn authored client parser/API: progress. Added MultiQualityView mounted beside multi-preview for available current proposal. Scope includes entire proposal/original intent and API identity; effect owns request inputs, aborts on refresh/scope/unmount, disposed/scope/API guards suppress late stale responses. Shows technical pass/review/fail, localized measured reasons/clipping, pending/unavailable/reload states. RU/BG/EN multi-specific limitation explicitly states technical checks do not certify stitching/motion/registration and image review is required. Uses existing localized technical quality labels. No Apply action enabled by this display.

Authored DOM unavailable/retry/unmount and late-response scope tests; parent metadata review suite mocks child quality component to keep its scope. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Positive current-report display/refresh/status/localization/native/web runtime coverage and full visual evidence remain open. Client canonical report SHA and authoritative multi-lineage/CAS Apply transaction still open. Whole-product percent unknown; catalogue ceiling 84.4%, no module completion credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: client canonical multi-quality report digest — NOT RUN

Previous turn mounted quality UI: progress. Re-read shared packages/api-client canonicalJSON (RFC8785/JCS) and worker contracts/base canonical_bytes using rfc8785. MultiQuality parser now serializes validated report through existing shared canonicalizer, UTF-8 encodes, enforces ≤1MiB and compares SHA256 using existing noble library to immutable quality_report.sha256 before returning owned data. Thus OwnerAPI/UI do not display a changed-but-still-pass technical report under an old digest. No new hashing/canonicalization implementation forked.

Updated authored positive fixture digests and added changed metric retaining pass, changed SHA and reordered transport fields cases. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Cross-language real report vectors/runtime evidence still required in deep-test phase. Authoritative multi-lineage/CAS Apply transaction, client Apply workflow and complete V20–V32 semantic coverage remain open. Whole-product percent unknown; catalogue ceiling 84.4%, no module completion credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: durable explicit multi Apply decision — NOT RUN

Previous turn added canonical report digest: progress. Read current optical lineage contract and perspective Apply/version implementation. Added 01210 dedicated immutable decision column/constraint/guard and photo_api-only save function. Requires exact fields/schema, geometry reset confirmation, warning boolean, current caller-authorized completed multi quality review and exact source result/root/quality result/report. Locks tenant/project/quality and parent runs, producer/quality tasks, all frozen source asset_versions in UUID order and quality artifacts; retains ordered frozen frames plus actor/session/operation. Hard fail cannot be overridden; review requires explicit warnings. Exact stored replay only, no source version mutation or new asset/version created by this step. Intended to execute inside later atomic Apply transaction, with rollback on any refusal.

Added strict DecodeMultiApplyDecision (size, JCS/duplicate/extra fields, IDs/digests/schema/confirmation) and authored valid roundtrip/invalid field/duplicate/extra tests. Tests/build/typecheck NOT RUN; migration NOT APPLIED. Multi output creation/complete lineage/collection CAS and source geometry reset transaction still open; must not reuse single-source perspective parent assumptions. Client Apply flow and positive SQL privilege/concurrency/revocation evidence remain open. Whole-product percent unknown; catalogue ceiling 84.4%, no module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: multi reference-asset version creation — NOT RUN

Previous turn authored immutable Apply decision: progress. Added 01211 atomic version function intended inside tenant caller transaction: repeats saved explicit decision and current quality, verifies exact output WorkingImage bytes/hash and adopted child locator, uses frozen reference frame as parent/asset, deterministic UUIDv5 quality-run version ID, serializes asset version number, creates accepted geometry version and all ordered source_version lineage with operation role. Exact replay checks full lineage count/ordinals, target/metadata/run/parent. Original versions/drafts/coordinate edits untouched; output starts without inherited draft. Reference-asset version is current implementation choice using existing catalog, not a separate new asset.

Go MultiProposalService.Apply obtains current quality and full byte-verified content before SQL, serializes decision, performs bounded photo_api Editor transaction and refuses unexpected/nil version with error to rollback inner decision+version/lineage. Returns version/reference parent/WorkingImage. Authored unconfigured/cancelled preflight tests only. Tests/build/typecheck NOT RUN; migration NOT APPLIED. Full positive SQL transaction/rollback/replay/role/source-deletion races, GC retention of registrations/root, collection CAS and HTTP/client Apply still open. Whole-product percent unknown; catalogue ceiling 84.4%, no module completion credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: multi Apply retained output SQL wrapper — NOT RUN

Previous turn authored version creation: progress. Current GC v2_artifact_has_references (00732) recognizes v2_artifact_links. Added 01212 dedicated completed-run output witness gate: current operation/producer/result/attempt/fence, all typed output JSON/NPY/registration locator versions and complete expected output role set under locks. Unlike draft seal gate this accepts only completed sealed quality run. Still depends on opaque physical proof supplied by trusted backend; locator comparison alone is not byte verification.

01213 retained Apply wrapper invokes version creation then output gate, retains every exact verified output artifact using existing source workflow v2_artifact_links with deterministic version purpose/ordinal and exact replay. Retention conflict/stale raises rollback. No new table/purge allowlist required. Revokes API access to incomplete inner version function; only retained wrapper granted. Go Apply still calls revoked inner function and must be wired to retained wrapper with full witness payload before public HTTP Apply is exposed. This intentionally leaves Apply unavailable until required evidence is wired. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Wrapper unit/integration/role/GC/purge/race evidence, Go wiring, source occurrence recheck, collection CAS and HTTP/client Apply remain open. Whole-product percent unknown; catalogue ceiling 84.4%, no module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: backend retained Apply wiring and owned evidence — NOT RUN

Previous turn authored retention SQL gates/wrapper: progress. Refactored private output witness serialization shared by preview and API Apply; preview still validates full scheduler source first. New private MultiResultContent.applyEvidence requires completed content, exact tenant/project/producer/full binding, canonical root digest and complete captured versioned output catalogue; returns owned root/image/witness bytes without fabricating scheduler attempt/fence. SQL retained wrapper repeats authoritative saved producer result/attempt/fence. MultiProposalService.Apply now calls retained wrapper with all evidence in bounded tenant Editor transaction, no revoked inner-function call.

Authored positive physical fixtures for planar/focus/cylindrical, complete witness count/root/image, ownership, foreign project/producer, missing capture and zero content refusal. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Full original occurrence locator recheck under Apply locks, collection CAS, HTTP/client Apply and SQL retention/GC/purge/concurrency integration remain open. Whole-product percent unknown; catalogue ceiling 84.4%, no module completion credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: locked original occurrence gate in complete multi Apply — NOT RUN

Previous turn wired retained output Apply: progress. Refactored private original inventory serialization shared with preview while retaining preview scope preflight. Go Apply passes full physically verified inventory alongside root/image/output witnesses, no invented scheduler fencing. Added 01214 completed sealed quality source original gate, exact saved result/root/attempt/fence and fresh original occurrence inventory resolution under lifecycle/artifact locks. Ordered JSONB equality rejects added/omitted/reordered roles or locator versions. Added 01215 complete Apply wrapper: version/decision/lineage/output links plus original gate in same transaction; refusal raises rollback. Revokes API grant to intermediate retained function, Go calls complete wrapper.

Extended authored planar/focus/cylindrical physical evidence unit fixtures with owned original inventory and foreign producer refusal. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Actual DB role/rollback/GC/source-expiry/race proofs and lock ordering stress remain unverified. Collection CAS, HTTP/client Apply and broad V20–V32 scope remain open. Whole-product percent unknown; catalogue ceiling 84.4%, no completed module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: HTTP/client multi Apply version path — NOT RUN

Previous turn added original inventory Apply gate: progress. Added POST panorama/focus-stack proposals/{task}/apply with mutation authorization/CSRF through tenantRequest(true), no-store, exact UUID/strict decision decode, optional writer dependency and mapped errors. Calls complete evidence/retention backend transaction. Client prepareMultiApply revalidates canonical-hashed quality+original intent, rejects fail, requires geometry reset and review warning confirmation. readMultiApplied requires new non-source UUID, exact reference parent and WorkingImage. OwnerAPI.applyMulti freezes validated scope/quality before request, fixed route and cancellation before/after POST.

Authored client confirmation/status/parent/source/extra-field cases and unauthenticated/no-store POST route cases. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. This endpoint creates accepted version only; does not mutate collection selection. Existing selection CAS/journal flow requires dedicated multi integration, as does durable client unknown-outcome recovery and mounted Apply UI. Positive authenticated HTTP/SQL/rollback/replay evidence remains open. Whole-product percent unknown; catalogue ceiling 84.4%, no module credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: durable client multi Apply and selection CAS recovery — NOT RUN

Previous turn exposed HTTP/client version Apply: progress. Added MultiApplyAttempt with owned original MultiIntent+canonical-hashed review, explicit warnings and frozen optional reference-parent collection selection. Persists intent before network and applied version before collection CAS. Resume repeats exact Apply if outcome unknown; then reconciles collection state, refuses conflict without changing expected revision, handles lost CAS response through reread, persists selected only after acknowledgement. Separate version creation and collection selection remain two operations, not one SQL transaction.

MultiApplyStore scopes origin/user/tenant/project/producer, monotonic intent/review/warnings/selection/version/selected writes, journal serialization/locking and persisted logout generation fence. Logout cleanup integrated. Only acknowledged attempts may clear. Authored adapted lost-CAS/reload/conflict and scope/substitution/logout store unit tests using valid multi quality fixtures and report SHA. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Mounted Apply UI/caller selection propagation, positive replay/runtime/native/web and SQL role/race/GC/purge evidence remain open. Whole-product percent unknown; catalogue ceiling 84.4%, no module completion credit. Later Metal/MPS cap 10,000,000,000 bytes; RTX deferred.


## 2026-10-10: повторный расчёт и mounted multi Apply — NOT RUN

Статическая сверка текущего реестра:90 entrypoints;14 missing,3 partial,73 requires_semantic_review. (90−14)/90×100=84,4% — только потолок ограниченного каталога, не реальный процент продукта. Семантический зачёт всех требований V20–V32 не выполнен; whole_product_implementation_percent=null. Наличие кода/тестов не означает полноту реализации. Исторические PASS не использованы.

Добавлен MultiApplyView и включён через MultiQualityView/MultiReviewView в collection multi route. Требует подтверждения geometry reset, предупреждений для review; fail блокирует Apply. Сохраняет и восстанавливает durable attempt с точным JCS-сопоставлением текущих intent/review/selection/warnings, включая повтор после ошибки. Несовпадение запрещает применение сохранённого чужого решения. Scope remount сбрасывает подтверждения при смене review. После версии открывает editor; collection selection здесь не изменяется, отдельная интеграция selection/CAS остаётся открытой. DOM unit tests авторизованного подтверждения и fail-block написаны. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Runtime, DB concurrency/GC/privileges и полная семантическая инвентаризация всё ещё требуются. Metal/MPS later, VRAM≤10,000,000,000 bytes; RTX deferred.


## 2026-10-10: multi collection CAS UI and recovery — NOT RUN

Previous turn mounted version-only Apply: progress. Collection multi route now freezes the unique matching reference item, asset, expected version and collection revision, passes selection through review/quality into durable Apply; missing or ambiguous reference disables fresh Apply. Existing MultiApplyAttempt persists selection before version creation and uses reconcile/CAS without rebasing. Editor navigation now includes asset/collection/item. Added shared freezeCollectionVersionIntent validation; no sentinel version or fake mutation.

Added MultiApplyRecovery, scoped to API/origin/tenant/project/producer/current collection, restoring original persisted decision and selection independently of refreshed quality/revision; foreign-collection attempt refuses resume. Completion-only journal clearing retains existing store restrictions. Recovery currently appears when the producer proposal remains available; recovery after expired proposal still needs a dedicated route. New helper unit tests cover owned reference selection, ambiguity/foreign/missing and invalid revision. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Positive native/web, DB privilege/GC/race/rollback evidence and full V20–V32 semantic inventory remain open. Real product percent unknown; catalogue ceiling84.4% unchanged. Later Metal/MPS VRAM cap10,000,000,000 bytes, RTX deferred.


## 2026-10-10: ticket-bound Apply recovery without proposal — NOT RUN

Previous turn added collection CAS propagation/recovery: progress. Re-read execution contracts/acceptance and current result/attempt/recovery code. Added multiTicketProducer: validates exact queued ticket shape and deterministic run→workflow binding, derives operation-specific producer without transport. MultiReviewView now mounts recovery from acknowledged attempt outside the available-proposal branch, including failed/expired/pending result reads. No ticket/forged workflow selects no journal.

Recovery rechecks original operation and JCS intent as well as current collection, origin/user/tenant/project/producer scope. Saved applied version can proceed to original collection reconciliation/CAS without proposal fetch; an unacknowledged Apply still calls server with exact saved decision and remains subject to expiry/authority refusal. This is recovery of existing state, not bypassing source/quality expiration. Authored helper derivation/forgery, result-unavailable/no-ticket mount, intent mismatch and late tenant-scope delivery tests. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Standalone recovery after preparation journal removal, concurrency across two recovery controls, physical DB/native/web/race/GC evidence and complete V20–V32 inventory remain open. Whole-product percent unknown; limited catalogue upper bound84.4%, no completion credit. Later Metal/MPS VRAM≤10,000,000,000 bytes; RTX deferred.


## 2026-10-10: serialized multi Apply recovery and logout fence — NOT RUN

Previous turn made ticket-bound recovery independent of proposal availability: progress. Re-read attempt/store/shared Web Locks implementation. MultiApplyStore now serializes full recovery per durable key using a local queue plus separate cross-tab Web Lock namespace; it does not hold the short origin journal lock over network IO. MultiApplyAttempt refreshes validated durable state under execution lock, rejects removed/substituted/regressed records, then skips already-persisted version/selection. Store monotonic comparisons use shared JCS rather than object insertion order. Added assertCurrent generation checks before network stages and final return. Logout remains independent of execution lock; in-flight request cannot be recalled but its late response cannot persist or trigger a subsequent collection mutation after fence.

Replaced synthetic quality-report-ID selection validation with shared freezeCollectionVersionIntent. Authored store unit scenarios with two independent controllers (one Apply/one CAS, freshest state reused) and logout during blocked Apply response (no persistence/CAS). Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Cross-tab real Web Locks/native lifecycle/server concurrency remain unverified; standalone attempt discovery after preparation removal, full V20–V32 semantic inventory and physical DB/GC/role/quality evidence remain open. Whole-product percent unknown; catalogue ceiling84.4% unchanged. Later Metal/MPS cap10,000,000,000 bytes; RTX deferred.


## 2026-10-10: independent collection Apply inbox — NOT RUN

Previous turn added execution serialization/logout fencing: progress. Added read-only listMultiApplyAttempts with exact origin/user/tenant/project filtering, canonical key check, validated immutable record scope and collection selection, stable producer cursor, ≤20 returned records / ≤200 parsed records per page and explicit continuation. Matching malformed records refuse discovery rather than silently vanishing; foreign-user/project records are not read. Epoch and persisted-generation checks reject logout during scan. AsyncStorage getAllKeys still enumerates all local keys; a durable indexed storage adapter is not implemented.

MultiApplyInbox mounted in collection multi route independently of preparation/ticket, so start-another no longer hides retained collection attempts. Uses existing scoped MultiApplyRecovery and execution locks, routes successful recovery to collection; fresh Apply still requires current proposal quality. RU/BG/EN labels added. Authored discovery/no-preparation, user/collection isolation, corruption and21-record pagination tests. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Version-only legacy attempts with selection=null are not discoverable by collection inbox; current route creates collection-bound attempts. Real browser/native pagination/accessibility, cross-tab/logout/server races and DB/GC/role/quality evidence remain unverified; full V20–V32 semantic inventory still open. Whole-product percent unknown; catalogue ceiling84.4% unchanged. Later Metal/MPS VRAM≤10,000,000,000 bytes; RTX deferred.


## 2026-10-10: inbox loading/pagination scope and strict journal envelope — NOT RUN

Previous turn added independent collection inbox: progress. Re-read current inbox/recovery/store and normative README. Inbox now remounts pagination scope on origin/tenant/project/collection changes, binds rendered page to API + request(cursor/refresh), ignores late scoped reads via cancellation, exposes translated loading/empty/error states and lists operation/source count/canvas size. Refresh resets pagination; next-page navigation cannot retain old page controls. Uses RN accessibilityLiveRegion for loading.

Tightened MultiApplyRecord to exact durable envelope fields before semantic restore. Authored DOM unit scenarios for discovery failure/retry, next page and reset, collection change during pending page with late delivery; authored unexpected/missing journal envelope refusal. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Real native/web UI, storage indexing, legacy version-only discovery, SQL/server/GC/quality evidence and full V20–V32 semantic inventory remain open. Real product percentage unknown; catalogue upper bound84.4% unchanged. Later Metal/MPS cap10,000,000,000 bytes; RTX deferred.


## 2026-10-10: V30 delivery matrix core — NOT RUN

Previous turn implemented V29 inbox states/tests: progress. Read V30 normative delivery Cartesian matrix contract and exact schema definitions. Added worker DeliveryPolicy/FrozenPreset/Slot/Shard/AssemblyInput/Manifest contracts and assemble_delivery core. Verifies canonical JCS/hash of assembly and every ≤500-slot shard; exact N×P order/count, unique disjoint policy/presets, required flags/SHA, same per-ordinal binding across shard boundaries, exact snapshot digest, optional omission rules, nonzero IDs and cancellation. Returns owned manifest referencing exactly original shards; processes one shard at a time. Provenance required explicitly unavailable pending qualified V31; JSON state is not proof of physical export readiness or metadata.

Authored matrix success/ownership, duplicate/missing/required omission/binding/preset/snapshot/failure/provenance/cancel/content-hash tests. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. This is a pure core, not installed leased runtime/handler; V30 entrypoint status and catalogue ceiling unchanged. Requires server frozen snapshot/config proof, exports readiness/provenance validation, params/closure, scheduler/adoption/retention/GC, SQL/API/UI/publication and cross-language schema drift coverage. V29 still has qualification/runtime evidence gaps; no package marked complete. Full product percentage unknown; limited catalogue upper bound84.4%. Later Metal/MPS cap10,000,000,000 bytes; RTX deferred.


## 2026-10-10: V30 frozen delivery preparation core — NOT RUN

Previous turn added delivery Cartesian assembly core: progress. Read FrozenDeliveryInput/DeliveryPreparation/prepare+assemble params schemas, existing exportpreset canonical config hashing and SessionDescribe snapshot digest algorithm. Added matching worker contracts and prepare_delivery pure preflight: exact canonical/hash frozen input and snapshot, snapshot digest via existing canonical_hash exclusion rule, contiguous ordinals/nonzero bindings, unique/disjoint exact presets and required flags, config content SHA equals frozen preset SHA, canonical config bytes and mandatory caller-supplied existing preset semantic validator. Bounded snapshot8MiB/config1MiB; cancellation between reads/validation; required provenance unavailable pending qualified V31. Does not read mutable presets, create exports or claim metadata/physical readiness.

Authored preflight count/config-validator invocation, config/snapshot substitution, required mismatch/duplicate/provenance refusal and validator error propagation tests. Validator in tests is explicitly a double; production adapter not yet implemented. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Cores not installed as leased runtime; preparation→child exports→assembly server coordinator, immutable publication, output adoption/GC/SQL/API/UI and full V29–V32 qualification/inventory remain open. Whole-product percentage unknown; limited catalogue upper bound84.4% unchanged. Later Metal/MPS VRAM cap10,000,000,000 bytes; RTX deferred.


## 2026-10-10: shared export preset validation and frozen decoder — NOT RUN

Previous turn added frozen preparation core with mandatory validator hook: progress. Re-read actual existing Go exportpreset validator/hash path and pytest config. Refactored authoring to shared exported ValidateConfig without duplicating export rules; rejects non-finite nested config via existing canonical serializer. Added DecodeFrozenConfig: ≤1MiB, exact SHA/canonical bytes, strict unknown fields and roundtrip shape, same existing semantic validation. Returns independently decoded typed config; does not load mutable presets or claim tenant/ACL/watermark authority. Authoring name validation remains separate. Current exportpreset metadata policy remains strip-only; delivery copyright/approved_exif support still requires actual export pipeline integration.

Authored Go frozen success/substitution/unknown/duplicate/noncanonical/unsupported/null/nonfinite cases. Moved new delivery assembly/preparation Python tests from workers/tests into configured tests/platform; earlier NOT RUN status unchanged. Only gofmt/static reads performed. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Go decoder is available for future server admission; no Go↔Python bridge fabricated, Python preparation validator hook still needs runtime adapter. Coordinator/closure/leased runtime/adoption/SQL/API/UI/provenance and full V20–V32 semantic inventory remain open. Whole-product percentage unknown; catalogue ceiling84.4% unchanged. Later Metal/MPS VRAM≤10,000,000,000 bytes; RTX deferred.


## 2026-10-10: Go frozen delivery input compiler — NOT RUN

Previous turn exposed existing export config validator/frozen decoder: progress. Read authoritative V30 freeze/observe/assembly contract and existing SessionInputSnapshot compiler. Added backend/internal/delivery CompileFrozenInput and typed Policy/FrozenPreset/FrozenInput/PresetContent. Verifies ≤8MiB exact physical snapshot SHA and canonical bytes through existing CompilePinnedSessionInputSnapshot, exact unique/disjoint ≤8 required/≤8 optional policy, exact supplied preset inventory, frozen config refs/SHA and DecodeFrozenConfig. Canonically sorts owned preset/policy ID arrays and returns owned immutable document bytes/hash; no implicit contact sheet or mutable-preset read. Required provenance503; copyright/approved_exif503 until actual metadata support exists. These are explicit remaining requirements, not completion.

Authored deterministic/owned result and changed snapshot/missing/foreign/duplicate/config/provenance/metadata refusals. Only gofmt/static reads performed. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Compiler has no DB/auth authority, does not publish/admit: tenant/project ACL, watermark closure, frozen config publication/retention, SQL full item matrix, child exports observation, leased prepare/assemble, adoption/publication/API/UI remain required. Python validator adapter and full V20–V32 inventory/evidence also open. Whole-product percentage unknown; limited catalogue upper bound84.4% unchanged. Later Metal/MPS VRAM≤10,000,000,000 bytes; RTX deferred.


## 2026-10-10: V30 export observation state core — NOT RUN

Previous turn added Go frozen-input compiler: progress. Read existing export status service and normative delivery observe/matrix rules. Added SlotExpectation/ExportObservation/Slot and ObserveExport pure state transition: exact expected export ID/snapshot digest/source binding/preset SHA, unique/disjoint policy membership and required flag, pending child retention, completed+ready+artifact readiness requirement, required failure/package failure and optional omission only allow_omitted. Final slot owns artifact/ID/reason; failure diagnostics constrained to safe terminal code. Required provenance remains unavailable. MatrixState rejects unknown/empty state, never assembles pending and gives failure precedence.

Authored ready ownership/pending, required/optional×omission policy, foreign binding/provenance and aggregate pending/failure tests. Only gofmt/static reads; tests/build/typecheck NOT RUN, migrations NOT APPLIED. This is state core, not a DB observer: scoped locked repository, final encoded physical verification, immutable snapshot/preset inventory completeness and child run admission/retention must supply authority. SQL coordinator/runtime/adoption/API/UI/metadata/provenance and full V20–V32 semantic inventory/evidence remain open. Whole-product percentage unknown; catalogue upper bound84.4% unchanged. Later Metal/MPS VRAM≤10,000,000,000 bytes; RTX deferred.


## 2026-10-10: server BuildAssemblyInput and exact500-slot shards — NOT RUN

Previous turn added export observation core: progress. Re-read normative BuildAssemblyInput contract and existing artifactjson publication interface. Added Go SlotShard/AssemblyInput/PublishDocument and BuildAssemblyInput: exact physical snapshot SHA/canonical snapshot/digest, complete Cartesian matrix against snapshot bindings and exact policy/preset inventory, deterministic ordinal/preset sorting, required/optional state invariants and ≤200-character omission reason. Entire matrix preflight precedes first publication; pending/failed/duplicate/missing/foreign slots refuse. Owns nested caller pointers before callbacks, publishes ≤500-slot canonical shards under stable ordinal purposes, verifies returned IDs/digests/uniqueness and returns compact root bytes/hash referencing exact shards. Context cancellation checked throughout.

Authored501-slot split, input mutation during publication, exact purpose, zero publication on invalid preflight, publisher digest substitution and cancellation tests. Only gofmt/static reads; tests/build/typecheck NOT RUN, migrations NOT APPLIED. PublishDocument is a mandatory interface, not yet backed by production artifactjson scope/allowlist/reservation; compiler is not an installed entrypoint. Coordinator must attach complete output atomically; interrupted publication can leave GC-able orphan objects. Scoped frozen config authority, immutable child exports physical proof, SQL/runtime/adoption/retention/API/UI/metadata/provenance and full V20–V32 inventory/evidence remain open. Whole-product percentage unknown; limited catalogue upper bound84.4% unchanged. Later Metal/MPS VRAM≤10,000,000,000 bytes; RTX deferred.


## 2026-10-10: optional leased delivery worker adapters — NOT RUN

Previous turn added server shard builder: progress. Read TaskArtifacts typed closure/gateway/staging, ExecutionContext cancellation and service installation patterns. Added DeliveryInstallation/DeliveryAdmission and prepare+assemble adapters using exact normative v2.delivery.* operation IDs/version1/CPU/v2.2 profile. Mandatory admission before input processing and again before root staging; prepare requires supplied strict preset contract and semantic validator. Reads only declared typed TaskArtifacts inputs; pure cores retain canonical/hash checks and cancellation. Stages only document role with byte accounting, review_required=false (package review acceptance belongs to server atomic publication transaction).

build_worker/run_worker optionally accept delivery_installation, reject non-CPU and register both operations only when installation supplied. No default allow-all admission or readiness added. Production SQL admission/preset contract+validator factory not supplied yet, so deployment activation remains unavailable. Authored adapter prepare/assemble/staging/accounting, second-admission revocation and invalid installation/profile tests with explicit doubles. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Actual SQL admission/closures/adoption, worker installation factory/CLI/readiness, publisher allowlist/retention/GC, API/UI/metadata/provenance and full V20–V32 inventory/evidence remain open. Catalogue ceiling84.4% unchanged; whole-product percent unknown. Later Metal/MPS VRAM≤10,000,000,000 bytes; RTX deferred.


## 2026-10-10: production frozen preset worker validator — NOT RUN

Previous turn added optional leased delivery adapters: progress. Read existing Go exportpreset.Config/ValidateConfig and worker EncodeParams/ExportSize. Added FrozenExportPresetConfig with exact Go wire settings, nonzero watermark asset, bounded watermark/sharpen, UTF8 naming bytes and strict format options. Extracted existing EncodeParams option checks into shared validate_format_options with unchanged semantics; reused existing ExportSize. validate_frozen_export_preset enforces duplicate-safe canonical JSON and exact typed roundtrip excluding optional nulls; rejects default insertion and explicit null/unknown fields. DeliveryInstallation defaults now use this production contract/validator while SQL admission remains mandatory/injected. No invented ICC hashes or watermark access grants.

Authored valid JPEG/effects/PNG, quality/alpha/metadata/size/unknown/null/missing-default/naming/watermark/duplicate refusals and default-installation prepare adapter test. All tests/build/typecheck NOT RUN; migrations NOT APPLIED. Cross-language executed parity vectors pending; metadata remains strip-only, ICC/watermark closure and SQL admission must prove authority. Production SQL installation/readiness/adoption/publisher/API/UI, qualified provenance, full V20–V32 inventory/evidence remain open. Whole-product percent unknown; catalogue ceiling84.4% unchanged. Later Metal/MPS VRAM≤10,000,000,000 bytes; RTX deferred.


## 2026-10-10: V30 SQL live lease/closure gate and combined admission — NOT RUN

Previous turn added production preset validator: progress. Read actual lens/perspective lease gates, TaskDatabase lease identity, current migration head01215 and normative delivery schema. Added01216 v30_delivery_lease_ready: registered session_user worker, active tenant, sealed v2 run, CPU/v2.2 prepare/assemble task, current attempt/fence/worker/params/input digest/lease/deadline, resource slot + worker_authorized, v2 workflow/closure, exact two-field params/schema/typed root, ready backend task_params and declared versioned live input artifacts under locks. Worker-only execution grant; no API table grants/new tables. Explicitly a lease/closure gate, not package/source proof.

TaskDatabase method binds all10 lease values. SQLDeliveryAdmission validates operation/profile/typed params/JCS hash, invokes live gate and mandatory separate source verifier with strict True result. No default verifier or automatic activation. Authored query-binding, false/null/nonboolean gate, source-refusal and changed-params pre-SQL tests using explicit database doubles. Tests/build/typecheck NOT RUN; migration NOT APPLIED. Real SQL role/fresh+upgrade/locking/revocation evidence missing. Frozen-source SQL authority, package/item tables with purge/GC integration, full admission/publication/adoption/coordinator/API/UI and V20–V32 inventory/evidence remain open. Whole-product percentage unknown; catalogue ceiling84.4% unchanged. Later Metal/MPS VRAM≤10,000,000,000 bytes; RTX deferred.


## 2026-10-10: V30 prepare frozen-source SQL gate — NOT RUN

Previous turn added live lease gate: progress. Read v25 snapshot/current-source authority, v2_editor_authorized and v26 recipe-scope functions. Added01217 immutable bounded v30_delivery_proof on existing backend_documents with trusted-only first write and immutable trigger, using existing lifecycle/purge table rather than new unregistered table. Prepare-only source gate first repeats01216 in same SQL transaction, then checks current run author/session editor ACL, exact workflow project/download permission, saved frozen/snapshot/config raw SHA, ready typed input IDs, private nonexpiring snapshot/ordinal/current accepted versions and existing recipe scope. Saved proof layout: schema_version/project_id/auth_session_id/frozen_input_jcs/snapshot_jcs/configs_jcs[{preset_id,config_jcs}]. Immutable bytes are trusted admission inputs, worker repeats semantic/JCS validation.

Assembly deliberately false until child export physical/retention/package proof exists; watermark configs false until nested authority implemented. No proof writer/admission publication is supplied yet, so no automatic deployment activation. TaskDatabase delivery_source_ready binds same10 lease parameters; SQLDeliveryAdmission defaults to real source RPC where available, retains explicit verifier injection for tests. Authored default-source SQL call binding/refusal/success-double tests. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Actual SQL role/locks/fresh+upgrade evidence missing; trusted proof writer and frozen source byte/canvas/recipe closure witness retention, assembly authority, package/item DB/coordinator/adoption/publisher/API/UI and full V20–V32 inventory remain open. Real product percentage unknown; catalogue ceiling84.4% unchanged. Later Metal/MPS VRAM≤10,000,000,000 bytes; RTX deferred.


## 2026-10-10: compiled prepare proof and trusted SQL writer — NOT RUN

Previous turn added source gate without writer: progress. Read current tenant/auth identity, V23 recovery save pattern and V30 compiler/proof columns. Added CompilePrepareProof: exact frozen recompilation/config validation, scope IDs, owned raw frozen/snapshot/config strings, deterministic proof/params bytes. Watermark unavailable until nested authority. Added01218 reusable private prepare-source predicate, worker wrapper retaining live lease, and API-only save function: authenticated tenant/editor/session/project download ACL, current author v2 run/task/backend params under locks, first write only draft/unsealed blocked attempt0, exact immutable replay, source check after write with exception rollback on refusal. Existing backend_documents lifecycle retained.

SavePrepareProof compiles internally, verifies exact params artifact SHA and binds caller identity/scope in caller-owned photo_api transaction; does not publish params or create/admit task. Authored compiler owned/substitution and repository exact binding/refusal/pre-SQL mismatch tests. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. SQL concurrency/role/rollback/replay real evidence missing. Full task/run creation/publication/sealing/coordinator, snapshot canvas/physical nested closure witness, package/items with GC/purge, assembly/watermark source authority, API/UI/metadata/provenance and full V20–V32 inventory remain open. Real percent unknown; catalogue ceiling84.4% unchanged. Later Metal/MPS VRAM≤10,000,000,000 bytes; RTX deferred.


## 2026-10-10 — пересчёт и delivery prepare closure

Повторный арифметический пересчёт текущего реестра: 90 точек входа; 14 missing_runtime_path, 3 partial_runtime_path, 73 requires_semantic_review. Верхняя граница каталога 84,4%; процент реальной реализации всей системы не установлен. Это не полный аудит требований: текущий реестр не содержит доказанного семантического зачёта всех функций, состояний, экранов и транзакций. Исторические PASS не использованы.

Добавлен `backend/internal/v2input/delivery_closure.go`: детерминированная идентичность prepare task, проверка Params SHA, обязательных frozen/snapshot/config roots, конфликтов контрактов; общий формат digest и повторно открываемый принадлежащий компилятору inventory. Написан `delivery_closure_test.go` на отсутствующие корни, конфликт типов, Params SHA, стабильность порядка и владение входами.

Ограничение: данный компилятор не доказывает полноту транзитивного разрешения и права на зависимости. Производственный resolver, создание и sealing задач, дочерние экспорты, публикация и delivery API/UI остаются открытыми. Модуль V30 не зачтён. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ. Выполнено только форматирование Go.


## 2026-10-10 — frozen delivery prepare plan

Добавлен `backend/internal/delivery/prepare_plan.go`: CompilePreparePlan повторяет проверку proof/frozen/snapshot/configs, связывает Params SHA с детерминированной prepare task, самостоятельно выводит обязательные roots включая WorkingImage/EditingRecipeV3 каждого snapshot item. Возвращаемые recovery bytes и roots принадлежат плану; внешняя мутация не меняет план. InputClosure проверяет resolved inventory; PublishInputs формирует транспортный GraphTask через существующий BuildInputClosure после blocked shell.

Написан prepare_plan_test.go: идентичность, пропущенный recipe root, владение bytes/roots, zero plan и nil publisher. Тесты НЕ ЗАПУСКАЛИСЬ, миграции НЕ ПРИМЕНЯЛИСЬ. Выполнен gofmt. Полнота транзитивных зависимостей, production resolver/ACL, resource profile, shell creation, registration/sealing и orchestration остаются открытыми. Процент всей реализации unknown; верхняя граница каталога 84,4% не изменена; V30 не зачтён.


## 2026-10-10 — prepare resource reservation and blocked task

Добавлен prepare_resources.go: CPU/v2.2 profile, VRAM=0, pixel_count=0; RAM резервирует 512 MiB overhead + 32×суммарные bytes frozen/snapshot/configs/Params + 1024×closure entries; scratch 32 MiB + document bytes + 1024×entries; output 8 MiB. Это консервативная политика admission, не измеренный peak. Ограничения проверяются до возврата DraftTask. DraftTask связывает task identity, Params, inventory digest и профиль; само создание SQL shell и текущая проверка ACL остаются обязательными внешними шагами.

Написан prepare_resources_test.go: task/digest identity, CPU/zero-VRAM, отказ при недостаточных RAM/scratch/output, владение профилем. Тесты, сборка, typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. Производственный resolver, оркестрация и калибровка ресурсов остаются открытыми; V30 не зачтён. Реальный процент всей реализации unknown, верхняя граница каталога остаётся 84,4%.


## 2026-10-10 — frozen prepare task lifecycle

Добавлен FrozenPrepareTask: FreezeTask один раз фиксирует принадлежащий компилятору closure, digest и resource profile; DraftTask возвращает копию профиля, PublishInputs использует этот же frozen cursor после commit blocked shell. PreparePlan.DraftTask делегирует FreezeTask. Production orchestration должно сохранять этот frozen value через shell commit/publication, а не повторно разрешать mutable inputs. Существующий низкоуровневый PreparePlan.PublishInputs оставлен; ему по-прежнему необходимо внешнее сопоставление с shell digest.

Дополнены unit-тесты на владение frozen profile, zero task и nil publisher. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ, миграции НЕ ПРИМЕНЯЛИСЬ; только gofmt. SQL orchestration/resolver/ACL/registration/seal и ресурсная калибровка остаются открытыми. V30 не зачтён; реальная готовность unknown, каталог ceiling 84,4%.


## 2026-10-10 — prepare result validation

Добавлены Preparation и PreparePlan.ValidatePreparation. Ожидаемые input ref, snapshot digest, item_count и slot_count выводятся при компиляции из проверенного frozen input/snapshot. Передача результата требует точного physical SHA и точных canonical bytes ожидаемого документа; подмена матрицы, root, schema, лишние поля, null и неканонический JSON отклоняются. Это чистая проверка payload: adoption TX ещё должна проверить task fence/output provenance/current ACL/quota и атомарно открыть expansion barrier.

Написан preparation_result_test.go с положительным примером и подменами counts/digest/input/schema/hash, extra field/whitespace/null, zero plan. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ, миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. SQL adoption/orchestration остаётся открытым; модуль V30 не зачтён; whole-product completion unknown.


## 2026-10-10 — paginated frozen export child intents

Добавлен PreparePlan.ExportChildren: row-major N×P slot inventory, детерминированный child run UUIDv5(parent_run,"child:export:"+slot_index), immutable binding/recipe/config SHA, проверка parent→workflow, cursor от0 до slot_count и page limit1..100. Cursor считает slots, не фотографии; страница владеет результатами. Export record ID не приравнивается к run ID. Написаны unit-тесты на replay/ownership/foreign parent/invalid cursor и переход страницы100→120 с двумя presets.

Это план admission, не SQL expansion: текущий fenced lease, accepted prepare provenance, ACL/quota, фактическое создание export children и durable cursor ещё должны быть связаны production orchestrator. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. V30 не зачтён; реальный процент завершённости unknown.


## 2026-10-10 — immutable export admission request

Статическое чтение exportjob.Create и DownloadExportLoader подтвердило legacy selection/decode-master контур: он не принимает V30 WorkingImage/recipe binding. Добавлен FrozenExportRequest/PreparePlan.ExportRequest: повторная сверка child intent с вычисленным slot, parent/workflow/run identity, точный recipe/binding/preset/snapshot digest; config bytes берутся только из frozen proof и проверяются DecodeFrozenConfig. Запрос канонический, bounded2MiB, strip-only/no-watermark до соответствующей реализации. Mutable preset не читается.

Написаны unit-тесты стабильности frozen config при внешней мутации и подмен run/binding/recipe/preset/ordinal/slot/digest. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. Это payload для будущего V30 admission, не подключённая production интеграция: SQL parent fence/ACL/quota/source checks и создание child run/export остаются открытыми. V30 не зачтён; whole-product completion unknown.


## 2026-10-10 — delivery export encode compiler

Добавлен workflow.CompileDeliveryExport: получает проверенный ExportRequest из PreparePlan, проверяет physical WorkingImage SHA/metadata/canvas hash, повторно декодирует frozen preset и компилирует один existing export.encode node под детерминированным child run. Используется текущий encode compiler с nested pixels/optional alpha. Naming остаётся packaging; sharpening явно DELIVERY_SHARPEN_UNAVAILABLE вместо потери настройки. Watermark/non-strip ранее блокируются frozen request. Current ACL/recipe/source/pixel/ICC authority, SQL child admission, resource profile и publication ещё обязательны внешне.

Написан delivery_export_test.go: полный frozen snapshot→prepare plan→child→encoder путь на unit fixture и отказ при изменённых source bytes. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ, миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. V30 не зачтён; whole-product completion unknown.


## 2026-10-10 — delivery first presentation stage

Статическое чтение подтвердило существующий derivative.sharpen (linear luma unsharp с max_delta0.03) и derivative.resize. Добавлен CompileDeliveryPresentation: exact frozen source SHA/canvas/dtype/color, frozen preset, resize-before-sharpen, deterministic first-stage task, CPU registry checks и exact nested input refs. Без derivatives возвращает nil. Последующие stages нельзя компилировать до accepted output; будущие refs не создаются. CompileDeliveryExport пока сохраняет отказ на sharpening, пока adoption chain не подключён.

Дополнен существующий frozen fixture test на отсутствие ненужного derivative stage и отказ при подмене source bytes. Positive resize/sharpen stage tests, subsequent stage adoption, resource admission, SQL orchestration и qualification ещё открыты. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. V30 не зачтён; real implementation completion unknown.


## 2026-10-10 — positive presentation compiler unit coverage

Вынесен общий hash-verified frozen delivery fixture. Добавлен delivery_presentation_test.go: положительные resize/sharpen/combined presets, resize-before-sharpen, детерминированный task UUID/params, точная передача long_edge/allow_upscale и sharpen strength/algorithm/max_delta, отказ при отсутствующей операции в registry. Existing export source test использует тот же fixture.

Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. Это authored coverage, не доказательство PASS. Subsequent presentation adoption, resource admission, SQL child/expansion orchestration и квалификация остаются открытыми. V30 не зачтён; whole-product completion unknown.


## 2026-10-10 — sharpen result payload adoption checks

Добавлен ValidateDeliverySharpenResult: physical source/output/report SHA, canonical output/report, exact source input list, output ref separation, unchanged canvas/color/dtype/transform history and alpha presence, report operation/source/dimensions/applied/warnings, bounded max_channel_delta. JSON checks не доказывают physical alpha equality или photographic quality: task fence/output provenance/current ACL и pixel checks ещё обязательны в production adoption.

Написан delivery_sharpen_result_test.go с положительным payload и отказами при excess delta/color drift. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ, миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. Resize adoption и production chain/orchestration ещё открыты; V30 не зачтён; real completion unknown.


## 2026-10-10 — derivative alpha quality preservation

Исправлен существующий derivative resize: opaque linear-light path сохранён; translucent RGB resize использует linear premultiplied alpha с безопасным unpremultiply, чтобы hidden RGB не загрязнял видимые границы. Вынесен _resize_pixels с проверкой формы/float32/finite/range alpha и owning no-resize copies. Общий derivative.open_image теперь читает uint16/float32 alpha через существующий read_alpha_array, как export encoder, вместо uint16-only.

Написан test_derivative_resize_transparency.py: hidden-blue transparent edge рядом с red, completely transparent finite black, opaque parity, no-resize ownership, invalid alpha. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ. Фотографическая квалификация и direct common reader dual-dtype tests ещё нужны. Production V30 orchestration/SQL adoption не завершены; whole-product completion unknown.


## 2026-10-10 — preserve float32 intermediate opacity

Общий derivative stage_arrays больше не квантует alpha до uint16: записывает float32 NPY, сохраняя промежуточную точность. Scratch reservations resize/sharpen/watermark увеличены с14 до16 bytes/pixel при alpha. Общий reader принимает legacy uint16 и v2 float32; export encoder уже использует dual-format reader.

Добавлен test_derivative_alpha_precision.py: прямое common.open_image чтение uint16/float32 с реальными NPY, stage_arrays float32 с sub-uint16 значениями и owning staged output. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ. Downstream adoption/qualification и resource admission whole-chain ещё требуют проверки на этапе тестирования. V30 production orchestration остаётся открытым; real completion unknown.


## 2026-10-10 — correct transparent watermark compositing

Исправлен derivative.watermark: mark ресемплируется в linear premultiplied alpha, затем source-over учитывает destination alpha и обновляет coverage. Straight RGB получается безопасным unpremultiply; hidden RGB прозрачного mark не загрязняет границы. Нулевой opacity и пиксели вне mark сохраняют исходные encoded RGB без повторной gamma-конверсии; alpha сохраняется float32. Для opaque source выход остаётся без alpha.

Добавлен test_derivative_watermark_transparency.py: mark поверх прозрачного hidden-blue фона, alpha=.5, hidden mark RGB при downsample, opaque base и exact zero-opacity noop, неизменность входных буферов. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ. Требуются adapter regression/physical qualification в последующей фазе. V30 orchestration/SQL adoption остаются открытыми; real completion unknown.


## 2026-10-10 — derivative resource compiler

Добавлен CompileDeliveryDerivativeProfile для resize/sharpen: проверяет source SHA/WorkingImage/nested input refs, derives exact target dimensions с RoundToEven (Python round parity), non-upscale policy и200M pixel cap. CPU/v2.2, VRAM0; RAM256MiB+192×(source+target), scratch32MiB+channels×(source+target), output8MiB+channels×target; channels12/16 с float32 alpha. Это conservative admission policy, не измеренные peaks.

В положительные presentation tests добавлены resource profile parsing/target pixel counts/zero VRAM и отказы ниже RAM/scratch/output лимитов. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. Профиль пока не подключён к production SQL task admission; runtime RAM enforcement/калибровка и orchestration остаются открытыми. V30 не зачтён; real completion unknown.


## 2026-10-10 — runtime derivative RAM admission

Добавлен derivative.memory: та же RAM policy256MiB+192×(source+target), validated dimensions/200M cap, strict safe-integer RAM. V2.2 требует explicit resource_estimate; legacy small images сохраняют bounded512MiB fallback. resize/sharpen вызывают admission после проверки metadata/params и до open_image/pixel IO. Scratch/output продолжают проверяться TaskArtifacts/reservation.

Написан test_derivative_memory_admission.py: exact budget/server formula, insufficient/invalid budgets, V2.2 no-estimate refusal, bounded legacy, dimensions, adapter early rejection before pixel IO для обеих операций. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ. SQL admission integration/calibration и V30 orchestration остаются открытыми; real completion unknown.


## 2026-10-10 — resize result geometry validation

Добавлен ValidateDeliveryResizeResult: physical source/output/report SHA, exact nested inputs, output root separation, deterministic target dimensions (RoundToEven), resize canvas identity из source ref/target dimensions, appended resize transform и chain hash, сохранение color/dtype/alpha presence, exact report applied/dimensions/warnings. Geometry helper воспроизводит existing worker contract, включая новый canvas даже при no-op size. Physical arrays/producer/fence/current ACL ещё обязательны в adoption TX.

Написан delivery_resize_result_test.go с200×100 из400×200, положительным payload, color substitution и report mismatch. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. Subsequent-stage compiler/SQL adoption/orchestration и qualification ещё открыты; V30 не зачтён; real completion unknown.


## 2026-10-10 — resize-to-sharpen transition compiler

Добавлен CompileDeliveryAfterResize: повторно вычисляет ожидаемую first resize task из frozen plan, проверяет физические hashes/canvas/chain/report через ValidateDeliveryResizeResult, затем строит deterministic sharpen task по реальным output WorkingImage/pixels/alpha refs и исходным frozen sharpen параметрам. Нет sharpen→nil. CPU registry/model/version проверяются; будущие refs не создаются. SQL accepted output/fence/current ACL всё ещё обязательны внешне.

Дополнен resize fixture test: combined preset, next sharpen inputs=actual resized refs, resource profile для resized source, refusal of changed bytes. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ, миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. Final encode after presentation/SQL chain adoption/orchestration и qualification остаются открытыми; V30 не зачтён; real completion unknown.


## 2026-10-10 — final encode from verified presentation sequence

Добавлен CompileDeliveryFinalEncode: повторно компилирует ожидаемую первую task и проверяет ordered result payloads resize→optional sharpen либо sharpen-only; missing/extra/reordered stages отклоняются. Финальный encoder получает последний реальный WorkingImage/pixels/alpha. Frozen size/sharpen убираются из encode params только после проверки полной цепочки, предотвращая двойную обработку. Без derivatives используется исходный frozen encoder path.

Дополнен combined fixture test: resize/sharpen reports→final encode exact last refs, no double resize, refusal missing sharpen/reordered outputs. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ, миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. SQL accepted producer/fence/physical arrays/current ACL/retention и production orchestration остаются обязательными открытыми шагами. V30 не зачтён; whole-product completion unknown.


## 2026-10-10 — final encoder resource profile

Добавлен CompileDeliveryEncodeProfile: exact final WorkingImage SHA/nested refs/encode params, target dimensions и existing format rules; RAM совпадает с worker presentation_ram_bytes (source24/96+target96+128MiB), scratch включает source NPY и output, output учитывает RGB16/RGBA16 и8MiB overhead. CPU/v2.2/zero VRAM. Worker export memory теперь требует explicit estimate дляV2.2 и safe-integer RAM; bounded legacy fallback сохранён.

Дополнены unit-тесты server RAM parity, insufficient RAM/scratch/output, final sharpen output profile и refusal original source substitution. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. SQL task profile integration/physical qualification и V30 orchestration остаются открытыми; whole-product completion unknown.


## 2026-10-10 — encoded manifest payload validation

Добавлен DeliveryEncodedImage/ValidateDeliveryEncodedManifest: exact canonical worker manifest, physical manifest SHA, output/input root separation, existing source/params validation, expected dimensions/format/ICC/alpha/strip policy из encode task. Encoded raster bytes ещё должны быть проверены independently вместе с producer fence/retention/current ACL перед ready state; JSON сам не доказывает заголовок/ICC/metadata содержимого.

Дополнены unit-тесты положительного encoded manifest и подмен dimensions/ICC/alpha/metadata/artifact. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. Physical encoded verifier и SQL adoption/expansion orchestration остаются открытыми. V30 не зачтён; whole-product completion unknown.


## 2026-10-10 — worker physical encoded checks

Усилен existing export._verify перед staging: JPEG/PNG exact embedded ICC SHA и отказ при EXIF/XMP/IPTC/Photoshop/comment; TIFF требует одну страницу, RGB/RGBA uint16 dimensions и exact ICC SHA, запрещает source description/artist/copyright/EXIF/GPS/XMP/IPTC/Photoshop/preview tags. Это worker-local physical validation, не независимое серверное adoption proof. TIFF decoded raster integrity/codec-specific metadata exhaustion, libvips writer regression и independent verifier ещё требуют работы.

Добавлен test_export_physical_verification.py: реальные JPEG/TIFF fixtures, wrong ICC, EXIF/description и multi-page TIFF refusal. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ. Existing encoder writer compatibility должна быть подтверждена в глубоком тестировании; результат не объявлен PASS. SQL adoption/orchestration остаются открытыми; real completion unknown.


## 2026-10-10 — physical PNG/TIFF layout and decode checks

export._verify теперь проверяет PNG IHDR16-bit RGB/RGBA dimensions и запрещает текстовые chunks через Pillow text metadata; Pillow load декодирует raster после verify. TIFF запрещает subIFDs/non-RGB, требует unassociated alpha для RGBA и декодирует page.asarray для проверки raster. Existing ICC/hash/strip checks сохранены. RAM policy target96bytes/pixel покрывает дополнительный decoded buffer по консервативному расчёту; peaks ещё не измерены.

Добавлены tests8-bit PNG refusal, production _write→16-bit PNG verification с реальным sRGB profile, wrong dimensions и associated-alpha TIFF refusal. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ, миграции НЕ ПРИМЕНЯЛИСЬ. Independent server physical verifier и SQL adoption/production orchestration ещё открыты; V30 не зачтён; real completion unknown.


## 2026-10-10 — durable delivery tables and purge allowlist

Подготовлена01219_v30_delivery_storage.sql: нормативные delivery_packages/items, scoped project/workflow/snapshot/artifact/export/package FKs,5000 ordinal cap, required omission refusal, page/export indexes, forced RLS, trusted-function-only access. Direct runtime/API grants отсутствуют до scoped entrypoints. Existing finish_tenant_deletion из последнего00728 сохранена с prepend explicit delivery_items/packages, DELETE allowlist расширен только ими. Миграция forward-only; не применена.

Написан storage_integration_test.go: direct runtime read denial42501 для API/worker/scheduler/gateway/maintenance и forced RLS, isolated migrated DB only. Не запускает migrations. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ. FK/state behavior, purge/GC/retention integration tests, scoped write/read functions и orchestration ещё открыты. Хранение не активировано, V30 не зачтён; real completion unknown.


## 2026-10-10 — delivery GC references and attachment guards

Подготовлена01220: existing latest v2_artifact_has_references сохранена, добавлены policy/manifest packages и export artifacts через delivery_items. Ссылки защищают объекты пока relational rows существуют, включая failed/cancelled packages; lifecycle detach/purge нужен до GC. Package refs используют existing ready-artifact share-lock trigger. Новый export attachment trigger share-locks export/artifact, ready item требует ready export и verified/versioned/unexpired artifact. Это не project/source authorization. Возможны retryable deadlocks с GC opposite lock order; production TX retry и race evidence ещё нужны.

Дополнен isolated DB integration test на отказ missing policy artifact через guard. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ, миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. Positive GC reference/expiry/detach/race tests, lifecycle retention и scoped API/adoption/orchestration ещё открыты. V30 не зачтён; real completion unknown.


## 2026-10-10 — delivery state integrity guards

Подготовлена01221: frozen package/item identity refs immutable, review/ready требует manifest, reason shape pending/ready=null и failed/omitted=nonblank. Package transitions building→review/ready/failure, review→ready/failure; unpublished ready может failed/cancelled; published row immutable, manifest once-set immutable. Terminal items immutable и assigned export immutable; material update требует revision+1. Это integrity, не authorization/human acceptance. Publication must still be atomic with CompleteReview/audit.

Написана isolated DB transition-matrix integration test (positive review/ready, terminal/backward/unknown/published rejection). Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ, миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. Trigger mutation/CAS/replay/purge regression, retention detach policy и scoped APIs/orchestration ещё открыты. V30 не зачтён; real completion unknown.


## 2026-10-10 — scoped delivery package read service

Подготовлена01222 get_v30_delivery: current authorize_tenant/session, app.tenant_id, live project workspace/restricted ACL (viewer allowed), Resource envelope revision string, optional verified/versioned/unexpired manifest ref; no bucket/key/version/downloadURL. Function API-only. Добавлены ReadPackage/Service.Get с tenant.WithTenant(Viewer), scope validation, notfound/null handling и owning response bytes. HTTP route пока не подключён.

Написан read_test.go на identity binding, invalid scope no-query, missing/null/malformed response, ownership и DB error propagation. Тесты/сборка/typecheck НЕ ЗАПУСКАЛИСЬ, миграции НЕ ПРИМЕНЯЛИСЬ; выполнен gofmt. Реальные restricted-project/session integration tests и HTTP/API/UI/creation/publication/orchestration ещё открыты. V30 не зачтён; real completion unknown.


## 2026-10-10 — пересчёт и подключение GetDelivery

После подключения GET /api/v1/tenants/{t}/delivery-packages/{id}: из90 точек входа13 отсутствуют,4 частичны,73 требуют семантической сверки. Верхняя граница наличия реализации каталога85,6%; это НЕ процент реализации всей системы. Реальный процент всей реализации неизвестен: нет полного взвешенного реестра требований со сверкой текущего кода. Новых полностью закрытых требований0. Исторические PASS не использованы.

Маршрут использует существующую аутентификацию, tenantRequest и delivery.Service; SQL повторяет session/tenant/project authorization. Resource возвращается JSON-объектом. Исправлен отсутствующий resource_type=delivery_packages в ещё не применённой01222. Добавлены HTTP unit-тесты отказа без аутентификации, передачи identity/tenant/id, malformed/zero UUID,404/503 и отсутствия двойного JSON-кодирования. Положительная SQL-интеграция, полный schema review, создание/публикация пакета и orchestration остаются открытыми. Тесты/сборки/typecheck NOT RUN; миграции NOT APPLIED.


## 2026-10-10 — стандартное подключение delivery CPU-worker

Добавлены строгий PHOTO_DELIVERY_ENABLED=true|false (default false) и создание DeliveryInstallation(SQLDeliveryAdmission) на том же TaskDatabase в build_worker. CPU-only, конфликт с explicit override запрещён; source/lease SQL gates сохранены, assembly gate остаётся закрытым. Это завершает участок configuration→runtime installation, но не registry/admission/orchestration/adoption. Написаны unit-тесты env/config/installation/source refusal. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Документация: delivery-runtime-deployment.md. Процент полной реализации неизвестен; новых закрытых требований0;85,6% остаётся только потолком каталога.


## 2026-10-10 — admission metadata delivery package

Написаны01223 и PreparePlan.AdmitPackage: сохранение building package только в существующем draft/unsealed delivery workflow с единственной blocked CPU prepare-задачей без edges. SQL повторяет session/editor/source authority, proof и physical-ready policy artifact; policy JSON должен совпадать с frozen proof. Tenant lock сериализует admission, exact replay сохраняет id/name/snapshot/policy и не создаёт новый package. Replay после dispatch не реализован этим writer. Slots создаются лишь после принятия prepare result — этот следующий участок ещё отсутствует. Go проверяет workflow/session/имя/hash до SQL; unit-тесты identity/params/refusal/foreign-session/cancel/invalid-input написаны. Tests/build/typecheck NOT RUN;01223 NOT APPLIED. HTTP CreateDelivery, workflow/task shell creation, публикация policy/frozen docs, регистрация/seal и adoption/expansion остаются открытыми. Реальный процент всей реализации неизвестен; новых полностью закрытых требований0.


## 2026-10-10 — prepare adoption и атомарная матрица delivery

Написана01224: scheduler-only Params discovery; generic adoption запрещён для v2.delivery.prepare; dedicated adopt_v30_delivery_prepared повторно проверяет текущие task/result/attempt/fence, workflow/package, frozen source proof и совпадение physical result SHA/body с ожидаемыми snapshot/counts. Делегирует существующему generic commit проверку output uploads, readiness, closure, links и inbox/event/outbox. В той же транзакции создаёт точную N×P матрицу pending items, открывает v2.delivery/1 barrier с cursor0 и снимает graph_sealed. Ошибка после adoption откатывает общий блок. Generic exact replay возвращает false без повторной матрицы; после progression этого writer нельзя считать API recovery receipt.

Добавлен независимый Python reconstruction frozen input/snapshot/production preset configs/result и evidence/physical SHA/source/role/extra-output проверки. Подключён в generic prepare_v2_adoption и dedicated ветку scheduler adoption_service. Unit-тесты подмены count/source/role/bytes/noncanonical params/extra outputs/cancel написаны; SQL privilege integration test написан. Tests/build/typecheck NOT RUN;01224 NOT APPLIED. Нет положительного SQL/process доказательства: package/source inventory до adoption, rollback race, SQL result substitution, exact replay, cancellation, GC и quota требуют глубокой проверки. Открыты initial workflow creation/registration/seal, fenced child admission/expansion completion, assembly source/adoption/review/publication и UI. Новых полностью закрытых требований0; процент всей реализации неизвестен.


## 2026-10-10 — fenced delivery expansion lease

Написана01225: partial pending index, private readiness predicate, scheduler-only claim/update lease. Claim блокирует tenant→run→expansion, исключает занятую/действующую lease, выдаёт новый token на60s. Проверяет adopted prepare, текущий frozen source proof/ACL, building package, unsealed running parent, slot_count и допустимый decimal cursor. Renew повторяет source gate; release допускается для текущей неистёкшей lease без повторного source gate, чтобы отдать lease после отзыва источника. Release задаёт5s backoff; expired token не продлевается и не освобождает новую lease.

Go ExpansionLeaseStore Claim/Renew/Release, unit-тесты empty queue, DB errors, invalid cursor/token, identity binding и cancellation написаны. Добавлены SQL privilege assertions для функций lease. Tests/build/typecheck NOT RUN;01225 NOT APPLIED. Положительная конкуренция двух scheduler, lease expiry, revoke/cancel races и bounded queue scan ещё не подтверждены. Создание child export runs, fenced cursor commit, final barrier completion/assembly остаются открытыми; scheduler loop не подключён без этих стадий. Процент полной реализации неизвестен; новых закрытых требований0.


## 2026-10-10 — восстановление frozen expansion plan

Написана01226: scheduler-only v30_delivery_expansion_plan под действующим token/lease, tenant→run→expansion locks и свежим source-ready gate возвращает исходные immutable proof/Params ref/FrozenDeliveryInput ref. Go LoadPlan ограничивает payload64MiB, запрещает неизвестные поля/дополнительные JSON values, повторно компилирует snapshot/config hashes/preset sets и Params, проверяет derived workflow/parent task/slot count и точное совпадение пересобранного proof. Возвращает owned PreparePlan; это не разрешение писать после истечения lease.

Написаны unit-тесты восстановления ExportChildren/ExportRequest после изменения DB buffer, token binding, foreign parent/task/count, подмены input/params/snapshot/config, missing/unknown/trailing payload; SQL privilege assertion добавлен. Tests/build/typecheck NOT RUN;01226 NOT APPLIED. Положительные SQL/lease/restart проверки открыты. Выяснено: legacy exportjob.Create читает mutable preset и создаёт download graph для selection; напрямую подменять им frozen single-slot V30 admission нельзя. Child admission/cache semantics, fenced cursor commit, registry and initial orchestration, assembly/adoption/publication остаются открытыми. Новых полностью закрытых требований0; реальный процент реализации неизвестен.


## 2026-10-10 — child binding и fenced cursor commit

Написана01227 и Go CommitChild/ChildConfig. SQL под текущей lease повторяет frozen proof/source gate, выводит child UUIDv5(parent,"child:export:"+slot), row-major ordinal/preset UUID order и exact FrozenExportRequest. Проверяет существующий sealed v2 child workflow/run, того же actor/project/snapshot и exact config {project_id,v30_delivery_export,export_id}; требует CPU presentation/encode task. Export version/preset hash и task ownership проверяются. Для этого нового writer metadata_policy exports фиксируется закрытым JSON object {"metadata_policy":"strip"}; будущий V30 export creator должен использовать именно этот формат, legacy records не переписываются.

В одной транзакции назначает immutable export_id pending item и cursor+1; skip-ahead запрещён. Повтор прошлого slot допускается только с той же сохранённой связью и текущей source/lease authority. Не выставляет ready и не доказывает физическое качество результата. Unit-тесты config/hash, exact request/token binding, SQL refusal, foreign run/slot/preset/version и missing export написаны; privilege assertion добавлен. Tests/build/typecheck NOT RUN;01227 NOT APPLIED. Child run/export creation и quota admission ещё отсутствуют; reuse export от другого child требует отдельной проверенной cache authority (этот writer не разрешает такой task ownership). Dynamic derivative→encode graph, cursor race/replay SQL fixtures и barrier finalization остаются открытыми. Общий процент реализации неизвестен; новых полностью закрытых требований0.


## 2026-10-10 — first-stage frozen V2 child task

Добавлены CompileDeliveryInitialNode/FreezeDeliveryInitialTask и CompileDeliveryChildClosure. Начальный stage из immutable preset: resize, иначе sharpen, иначе encode; future physical refs не выдумываются. В V2 orchestration task/node identity переводится в UUIDv5(child workflow,operation:0); legacy node compilers сохраняют прежние ID. Профиль CPU/v2.2 рассчитывается существующими derivative/encode resource policies. Params physical SHA/JCS, actual image ref и обязательные typed roots image/pixels/recipe/config (+alpha) фиксируются вместе с caller-resolved closure. Возвращаются owned DraftTask и PublishInputs на том же frozen cursor. Transitive completeness, source/ICC bytes and ACL остаются обязанностью будущего authorized resolver.

Unit-тесты выбора encode/resize/sharpen/both, V2 identity, profile ownership, missing source, nil publisher, closure ownership/hash/foreign operation написаны. Tests/build/typecheck NOT RUN. SQL creation child/export/quota writer и restart coordinator ещё не подключены; этот compiler сам по себе не создаёт executable run. Resource policies не являются измеренным peak/SLA. Миграционный head по написанным файлам остаётся01227. Общий процент реализации неизвестен; новых закрытых требований0.


## 2026-10-10 — child draft/export admission с общей квотой

Написана01228: private frozen child request derivation и scheduler-only create_v30_delivery_child_draft. Под действующим expansion lease/source gate выводит deterministic child/workflow/task IDs, проверяет exact frozen request/config/initial resize|sharpen|encode Params, CPU/v2.2/VRAM0 и storage reservation не меньше output+config+params. Создаёт existing V2 draft shell через create_v2_draft_shell с общей quota reservation и queued export record в той же транзакции. Replay существующего draft требует v2_pending_shell_matches и ту же export identity; не запускает graph, не продвигает cursor, не выдаёт readiness. Финальный fence check откатывает новую запись при истечении lease.

Go FrozenDeliveryInitialTask.CreateDraft использует owned task/profile/Params и canonical ChildConfig, проверяет derived child task, достаточную reservation, context; передаёт frozen bytes в SQL. Unit-тест identity/token/Params binding, refusal, quota preflight, foreign child/cancel написан; privilege test расширен. Tests/build/typecheck NOT RUN;01228 NOT APPLIED. Положительные SQL fresh/replay/quota/rollback/fence tests ещё отсутствуют. Cache export другого child пока не поддерживается, такой конфликт отказан; existing records не переписываются. Params artifact должен быть уже ready; production publication/recovery для него, authorized transitive resolver, fenced registration/seal, dynamic derivative adoption/encode, coordinator и full initial package API остаются открытыми. Общий процент реализации неизвестен; новых полностью закрытых требований0.


## 2026-10-10 — атомарная активация child и cursor

Написана01229: generic seal_v2_graph отказывает run с v30_delivery_export; dedicated activate_v30_delivery_child под source/lease locks проверяет exact frozen config и initial graph, вызывает сохранённый shared seal (registry/qualification/closure/quota/outbox), затем commit_v30_delivery_child в той же SQL транзакции. Refusal после seal вызывает exception и откатывает dispatch/outbox/graph/cursor вместе. SQL sealed acknowledgement branch проверяет сохранённую slot binding; это не отдельный durable API receipt.

Go ActivateInTransaction повторно загружает fresh frozen proof, сверяет его с plan, регистрирует собственные frozen cursor/direct roots (не доверяет GraphTask.Open/Direct от вызывающего кода), затем вызывает dedicated activation. Вызывающий обязан rollback при любой ошибке. Unit-тест порядка source-read→batch→task-input-seal→dedicated-activation, замены cursor, refusal и foreign graph написан; privilege assertion расширен. Tests/build/typecheck NOT RUN;01229 NOT APPLIED. Positive SQL atomicity/rollback/races и bypass tests ещё не выполнены/не доказаны. Go initial activation helper не является recovery already-running task: повторная input registration после dispatch требует отдельного receipt/status пути coordinator. Params publication, authorized resolver, coordinator/cache, dynamic derivative→encode adoption, assembly/publication и initial package API остаются открытыми. Процент полной реализации неизвестен; новых закрытых требований0.


## 2026-10-10 — recovery receipt принятого child

Написана01230 и FrozenDeliveryInitialTask.RecoveryReceipt. Scheduler-only SQL под действующей parent lease/source authority проверяет frozen request, cursor>slot, сохранённый delivery_item.export_id и exact initial task descriptor (Params SHA, input digest, profile без v2_input_manifest) с child config. Возвращает scoped run/task/export/current status и graph_sealed, не требует current graph_sealed после dynamic expansion. Это acknowledgement первоначальной admission, не readiness/успех export; допускает failed/cancelled current child. NULL означает unconfirmed, включая revoked/stale lease, а не отсутствие child.

Go ограничивает response4096bytes, проверяет закрытую форму/IDs/cursor/status/bool и fence binding. Unit-сценарии acknowledged failed/unsealed child, NULL, malformed/trailing и foreign export добавлены в existing compiled frozen-task fixture; SQL privilege assertion расширен. Tests/build/typecheck NOT RUN;01230 NOT APPLIED. Подключение receipt в production coordinator, положительная SQL idempotency/race проверка и restart/unknown-commit evidence ещё открыты. Params publication/resolver, dynamic derivative→encode/cache, assembly и API/UI остаются открытыми. Общий процент реализации неизвестен; новых закрытых требований0.


## 2026-10-10 — pre-shell Params publication

Написана01231: private frozen initial Params scope и scheduler-only reserve/complete. Params хранятся в backend_documents под существующим parent run с owner=derived child task UUID, purpose=task_params; child run ещё не нужен. SQL повторяет current expansion lease/source authority, exact resize/sharpen/encode Params и переданные tenant/run/owner. Stable artifact identity, conflict on changed content/bucket, token rotation on pending retry, ready artifact/version replay, reserved→storage accounting выполняются в existing backend_documents/artifacts/tenant_usage; финальные mutation fence checks дают rollback при lease expiry.

PublishDeliveryInitialParams использует pgxpool (не caller transaction), scoped Repository и общий artifactjson.Publisher с immutable PUT/recovery; object key/SHA/size/media/version и оба token проверяются. Parentscope снимает циклическую зависимость Params-ready→child-shell. Unit adapter-тест parent scope, scope/lease/document token, foreign scope/key, quota/conflict/stale, ready replay написан; SQL privilege assertion расширен. Tests/build/typecheck NOT RUN;01231 NOT APPLIED. Actual S3/SQL publication/crash/recovery/fencing/GC/quota evidence ещё отсутствует. Конструктор production coordinator/resolver, transport publication/seal recovery, cache, dynamic stages/adoption и remaining API/UI/assembly остаются открытыми. Общий процент реализации неизвестен; новых закрытых требований0.


## 2026-10-10 — пересчёт каталога и stable new export identity

Статически перечитаны текущие installation.py/runtime и delivery adoption. Устаревшая классификация v2.delivery.prepare.execute изменена missing→partial: conditional CPU runtime/adoption существуют, но initial API/coordinator не замкнут. Текущий каталог: 90 entries, 12 missing, 5 partial, 73 требуют semantic review. Верхняя граница наличия реализации 86,7%; это НЕ процент реализации всей системы. Реальный процент остаётся unknown/null: веса/полная requirement-level проверка отсутствуют, presence не даёт semantic credit. Исторические пересчёты сохраняют исторический смысл. Полностью закрытых новых требований: 0.

Добавлен PreparePlan.NewChildExportID: UUIDv5(child run, v30:delivery-export:1) только после проверки exact frozen ExportRequest. Стабильность при retry предотвращает выбор нового случайного ID для нового export в будущем coordinator. Это не cache lookup, не authorization и не доказательство crash recovery. Unit-кейсы stable identity, separate child/export ID, foreign parent и все существующие варианты подмены intent написаны. Helper ещё не подключён production coordinator. Tests/build/typecheck NOT RUN; миграции NOT APPLIED. Продолжение: authorized source resolver и coordinator публикации Params→draft→transport→activation с receipt recovery.


## 2026-10-10 — frozen child admission coordinator

Добавлены FrozenDeliveryInitialTask.AdmitNewChild, DeliveryChildStore и SQLDeliveryChildStore. Stable new export identity и reservation output+config+Params рассчитываются из frozen plan с exact-integer overflow guard. Протокол: receipt → DB-only draft transaction → transport publication вне transaction → DB-only activation transaction. SQL adapter внутри каждой transaction повторяет receipt перед draft/registration; acknowledged child, включая failed current status, не регистрируется повторно. Unknown commit возвращает ошибку; повтор с теми же frozen inputs обращается к той же export identity. NextCursor возвращается только после acknowledgement или успешно committed activation и не считается readiness.

Unit-сценарии порядка, точной reservation, failed-child recovery без publication, foreign receipt, receipt/draft/publication/activation failure и cancellation написаны. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. SQL race/unknown-commit positive evidence отсутствует. Coordinator работает с уже готовыми Params и authorized frozen closure; production source resolver, Params pipeline, lease scheduling/renewal, bounded expansion driver, cache reuse, dynamic stages/adoption, assembly и full API/UI ещё не подключены. Это конкретный reusable orchestration path с SQL adapter, но не установленный scheduler runtime. Полных модульных credits 0; реальный процент реализации unknown/null, каталог без изменения.


## 2026-10-10 — bounded expansion driver и child pipeline

Добавлены DeliveryExpansionDriver.ProcessOne и DeliveryChildPipeline.ProcessChild. Driver claim→LoadPlan→bounded ExportChildren(page1..100), renewal перед каждым child, продвижение local cursor только после exact returned child/export/cursor acknowledgement, best-effort fenced release. Ошибка release не перекрывает processing error; при cancellation нет background cleanup без timeout, lease восстанавливается expiry. Нет параллельного renewal goroutine: истечение во время длительного IO должно отказываться SQL fence и приводить к reacquire/retry. Admitted slots не означают completed exports и не разрешают assembly.

Pipeline требует DeliveryChildResolver; берёт owned exact image bytes и typed inventory, публикует initial Params parent-scoped общим immutable publisher, freeze first-stage task, затем SQLDeliveryChildStore + child-scoped transport publisher. Все object IO вне admission transactions. Concrete SQL/object resolver, scheduler installation и target ICC qualification пока отсутствуют: нет permissive source fallback. Cache reuse и dynamic result adoption/assembly также не реализованы этим изменением. Driver page boundedness не устраняет отдельно открытый unbounded SQL claim queue scan.

Unit-сценарии driver success/order/cursor, empty queue, claim/plan/renew/child/release errors, foreign cursor и invalid page limits написаны. Tests/build/typecheck NOT RUN, migrations NOT APPLIED. Pipeline full SQL/object positive evidence и renewal/crash/race tests остаются для глубокого тестирования. Каталог без новых full credits; whole-product implementation percent unknown/null.


## 2026-10-10 — lease-bound inherited source resolver

Написана01232 v30_delivery_child_sources: scheduler-only, exact private frozen child request + current expansion authority, paginated1000 rows by immutable parent input ordinal. Наследует typed inventory adopted prepare task, исключая его Params/FrozenDeliveryInput/InputSnapshot; не даёт arbitrary artifact lookup. Отказывает при любом missing/unready/unverified/expired/unversioned artifact во всём parent inventory. Current lease expiry проверяется перед возвратом. Полнота inherited inventory зависит от ранее sealed prepare closure; эта функция не строит зависимости заново из документов.

SQLDeliveryChildResolver получает страницы, проверяет закрытую форму, возрастающие ordinal/next, bucket/version/ref/unique IDs/limits. Читает exact versioned image/recipe/config JSON, проверяет physical SHA/size/canonical bytes; preset bytes равны frozen proof. TargetICC выбирается из deployment map по frozen config ICC ID. Binary arrays остаются physical worker verification, не заявляется физическая проверка всех inherited binary bytes на scheduler. Unit physical JSON valid/hash/size/limit/noncanonical/object error написаны; scheduler-only privilege assertion расширен. Tests/build/typecheck NOT RUN;01232 NOT APPLIED. Positive SQL source/closure/pagination/revocation/GC races и deployment ICC qualification не доказаны. Resolver/pipeline/driver ещё не установлены в scheduler; inherited full-parent inventory может быть слишком большим для child direct-input constraints, требуется per-child dependency projection до production enablement. Cache/dynamic stages/assembly/API/UI остаются открытыми. Whole-module credits0, реальный процент unknown/null.


## 2026-10-10 — per-child reachable input projection

SQLDeliveryChildResolver теперь проецирует inherited authorized inventory от точных image/recipe/config roots. Читает physical canonical JSON всех достижимых JSON документов, находит nested artifact_id+sha256 и id+sha256 refs, проверяет exact SHA против SQL inventory, отказывает missing/substituted refs, cycles/depth>64/4096 artifacts/64MiB aggregate JSON. Binary leaves не загружаются на scheduler, worker physical verification сохраняется. Результат owned, unique, deterministic sorted; unrelated parent inputs не включаются в child. Generic ref walker консервативен: malformed ref-shaped fields отказываются, неизвестная документная семантика этим не валидируется. Полнота зависит от явных JSON artifact refs; hidden/non-ref dependencies должны задаваться contract-specific decoder при их обнаружении.

CompileDeliveryChildClosure теперь оставляет в Direct только independently compiled roots+Params, сохраняя полную transitive inventory в cursor/chunks/input digest. Это снимает ошибочное ограничение127 на все transitive entries без ослабления retention/registration. Unit projection cases shared root, unrelated exclusion, missing/hash substitution, two wire formats/malformed refs написаны. Existing closure test расширен200 transitive entries, проверкой short direct list и complete cursor. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. SQL/object full integration, contract-specific dependency audit и scheduler installation ещё открыты; новых full module credits0, реальный процент unknown/null.


## 2026-10-10 — frozen source contracts и tenant context

Resolver теперь проверяет ParseWorkingImage float32/work_rec2020_gamma24_v1/gamma24, exact physical source SHA/canvas и deployment WorkingICCSHA. Recipe root с исторической меткой EditingRecipeV3 декодируется как FrozenRenderRecipe: именно зарегистрированный wrapper требуется существующим v26_render_recipe_scope. Проверяются exact frozen source binding и ICC lock; bare authored recipe отказывается. Unit valid wrapper/bare recipe/foreign version/ICC/noncanonical wrapper написаны, NOT RUN.

Написана01233: wrappers существующих v30_delivery_prepare_source_ready и worker v30_delivery_source_ready устанавливают transaction-local app.tenant_id из p_tenant на время fenced/source проверок и восстанавливают прежнее значение при success/exception. Это устраняет отказ вложенного v26_render_recipe_scope при scheduler pool calls без tenant session context. Старые predicates сохраняются private, source/lease/current ACL не обходятся; worker-only grant восстановлен только для public worker predicate. Migration NOT APPLIED; SQL context restoration/role/fence integration evidence ещё отсутствует. Scheduler installation, contract-specific nested validation и дальнейшая processing chain остаются открытыми. Tests/build/typecheck NOT RUN. Whole-module credits0, процент unknown/null.


## 2026-10-10 — opt-in Delivery scheduler installation

RunScheduler теперь конструирует SQLDeliveryChildResolver→DeliveryChildPipeline→DeliveryExpansionDriver при PHOTO_DELIVERY_SCHEDULER_ENABLED=true. Отдельный RunExpansionLoop lifecycle с graceful cancellation, conditional readiness и v30_delivery_pages_total/failures_total metrics. DeliveryExpansionJob ограничивает общий проход45s; SQL lease renew/fences остаются authoritative. Default disabled. Config CPU admission budgets RAM8GiB/scratch8GiB/output2GiB/page10, переопределения PHOTO_DELIVERY_RAM_BYTES, PHOTO_DELIVERY_SCRATCH_BYTES, PHOTO_DELIVERY_OUTPUT_BYTES (positive safe integer), PHOTO_DELIVERY_PAGE_SIZE1..100. Это бюджеты, не измеренные peak/SLA; GPU не используется.

Unit config cases default/enablement/bad flag/negative-zero-overflow budgets/unbounded page/overrides/nil reader написаны, NOT RUN. Scheduler использует уже loaded deployment profile SHAs и копирует ICC map. New initial workflow/API path, dynamic derivative→encode result adoption, cache reuse, final assembly и publication ещё открыты; opt-in не означает завершённый Delivery. Positive runtime scheduling, startup qualification, SQL/object/race/cancellation evidence отсутствуют. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Whole module credits0, real completion unknown/null.


## 2026-10-10 — presentation continuation compiler

CompileDeliveryContinuation выводит следующий реальный stage из frozen plan и checked presentation prefix: initial resize|sharpen|encode, после resize required sharpen или encode, после полной цепочки encode. Повторно использует существующие source/output/report geometry/color/sharpen validators; отвергает reordered/extra results. Нормализует task/node IDs в V2 operation:0, выводит predecessor task ID, stage index и CPU resource profile от actual current image, возвращает owned source bytes. Не выдумывает future refs и не применяет resize/sharpen дважды.

Existing resize/sharpen integration fixture расширен unit-сценариями prefix0/1/2→resize/sharpen/encode, V2 task IDs, predecessor, resource profile и reordered refusal. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Это compiler next task, не подтверждение adopted producer/fence/physical pixels; SQL stage adoption/append transaction, dynamic transport/Params publication и runtime result observation ещё нужны. Full-module credit0, реализация unknown/null.


## 2026-10-10 — frozen continuation transport task

Добавлен FreezeDeliveryContinuationTask: checked presentation prefix→next-stage V2 DraftTask/CPU profile/Params pin/complete caller-authorized closure. Independently required roots current WorkingImage/pixels/alpha, original WorkingImage, recipe/config и все intermediate WorkingImage+DerivativeReport. Direct roots остаются bounded, полная transitive inventory сохраняется в transport cursor. Owned descriptor/profile, predecessor/stage и PublishInputs доступны; initial-shell admission намеренно не экспортируется для continuation, требуется dedicated adopted-producer append transaction.

Existing resize+sharpen fixture расширен frozen sharpen/encode descriptor, predecessor/stage, profile ownership и missing adopted roots refusal. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. SQL stage source/producer/fence/physical array adoption, dynamic Params publication, append/seal/runtime observation и финальная assembly ещё не реализованы этим изменением. Inventory supplied caller не является proof adoption; full module credit0, реальный процент unknown/null.


## 2026-10-10 — fenced derivative producer discovery

Написана01234 scheduler-only v30_delivery_stage_result под current parent expansion/frozen child authority. Проверяет child config, exact package item/export binding, derived V2 derivative task, succeeded canonical result, CPU/v2.2, exact output-upload producer/attempt/fence evidence через existing v2_adoption_artifact. Требует working_image/derivative_report/working_rgb и optional working_alpha, не принимает чужие роли. Read-only: не пишет adoption receipt, не меняет cursor/graph. ReadDeliveryStageResult/strict parser проверяют scoped task/result/fence, unique output IDs/roles, media/version/bucket/SHA и limits; NULL остаётся unconfirmed, не completion.

Unit metadata valid/foreign task/missing report/duplicate output/role/bucket/version/SHA/media/fence cases написаны; SQL privilege assertion расширен. Tests/build/typecheck NOT RUN;01234 NOT APPLIED. Физические output bytes/NPY shape/color/alpha и adopted producer commit ещё не проверяются этим reader. Dynamic append/Params/transport и result runtime остаются открытыми. Whole module credit0, completion unknown/null.


## 2026-10-10 — independent physical derivative verification

VerifyDeliveryStageOutput проверяет scoped producer metadata, exact versioned canonical image/report JSON, existing resize/sharpen geometry/color/report invariants и exact RGB/alpha refs к producer outputs. Binary NPY читаются потоково shared masknpy verifier: SHA/bytes/shape, float32 RGB finiteness без отбрасывания HDR значений, alpha uint16|unit-float32. Размер ограничен expected canvas. Readers закрываются; errors от physical verification/Close возвращаются. Это не pixel equivalence replay и не SQL adoption commit/current authority.

Unit cases valid sharpen output with real float32 NPY, physical corruption, NaN, wrong shape, foreign pixel ref написаны. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Integration adoption/append/Params/transport/result observation/final assembly остаются открытыми. Whole-module credit0, real implementation unknown/null.


## 2026-10-10 — verified derivative stage loader

LoadVerifiedDeliveryStage компилирует pending stage из exact frozen plan/prefix, получает fenced producer metadata, проверяет physical JSON/NPY вне SQL, затем повторяет current-authority producer read и сравнивает весь canonical envelope. NULL/revocation или изменение task/result/attempt/fence/output version/bytes/ref отказывается. Возвращает private VerifiedDeliveryStage с owned Output/Producer copies. Это local evidence, не durable receipt; финальный read-to-commit race требует повторной проверки внутри adoption transaction.

Real-NPY fixture расширен unit unchanged producer→two reads+owned evidence, NULL after IO, changed fence refusal. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Dedicated adoption/append/Params/result runtime ещё открыты; full module credit0, real percent unknown/null.


## 2026-10-10 — durable stage verification receipt

Написана01235 record_v30_delivery_stage_verification: current frozen child/lease producer read и exact envelope equality, locked task attempt/fence/canonical result, image/report physical SHA+size, exact produced RGB/alpha refs, operation/dtype/profile. Пишет идемпотентный inbox consumer v30-stage-verified с marker(task,result), проверяет expiry перед возвратом; не пишет v2-adopt и не разрешает generic finalization до continuation graph commit. Physical geometry/NPY attestation доверена scheduler-only verifier, не клиенту. VerifiedDeliveryStage.RecordInTransaction передаёт private evidence в caller-owned SQL transaction без IO; refusal409, scoped preflight.

Real-NPY loader fixture расширен record acceptance/refusal/foreign bucket before SQL, privilege assertion расширен. Tests/build/typecheck NOT RUN;01235 NOT APPLIED. Positive SQL atomicity/idempotency/fence evidence отсутствует; dedicated adoption+append/Params/runtime остаются открытыми. Receipt — physical verification stage, не full adoption и не completed export. Full module credit0, real percent unknown/null.


## 2026-10-10 — initial derivative continuation barrier

Написана01236 begin_v30_delivery_continuation: current frozen child/source/lease, exact first required derivative, locked running child, deterministic task/barrier ID. Atomically calls stage verification receipt, inserts graph_expansion v30.delivery.export/1 expected remaining1|2 and unseals child. Existing generic v2-adopt marker не создаётся, completed export не заявляется. Exact open barrier replay проверяет current producer и verification marker. Это предотвращает разрешение финализации до материализации next tasks. Runtime вызов пока не установлен; отсутствие Params/append/seal driver иначе оставило бы child pending.

Go BeginContinuationInTransaction передаёт только private verified evidence через dedicated SQL в caller-owned transaction без object IO. Unit dedicated-query/acceptance/refusal добавлены в physical fixture; privilege assertion расширен. Tests/build/typecheck NOT RUN;01236 NOT APPLIED. Positive SQL rollback/idempotency/expiry/dispatch/finalization race evidence отсутствует; next Params/task append/seal/runtime remain open. Whole module credit0, real percent unknown/null.


## 2026-10-10 — continuation Params authorization/publication

Написана01237: private v30_delivery_params_scope сохраняет initial predicate и расширяет shared reserve/complete протокол для continuation. Current parent lease/frozen child request, locked running unsealed child/open continuation barrier, exact predecessor producer+v30-stage-verified marker. Sharpen выводится только после resize из frozen sharpen config; encode после last required derivative из frozen encoder fields без повторных size/sharpen. Params image равен actual predecessor working_image, owner derived next V2 task; parent-run scope/quota/stable document/token/object recovery остаются в существующем01231 протоколе.

PublishDeliveryContinuationParams компилирует checked prefix, публикует owned Params через existing repository/shared immutable publisher вне SQL transactions. Resize+sharpen fixture расширен unit scope/owner/actual Params binding для обоих continuation stages. Tests/build/typecheck NOT RUN;01237 NOT APPLIED. SQL positive scope/receipt/revocation/idempotency/quota evidence отсутствует; next task append/seal and runtime observation не подключены. Full module credit0, completion unknown/null.


## 2026-10-10 — blocked continuation registration and quota

Написана01238 create_v30_delivery_continuation_task: current parent lease/frozen continuation Params authority, running unsealed child, open barrier exact stage cursor, derived next task ID/key, CPU/v2.2/VRAM0, exact verified predecessor, ready parent-scoped Params. Создаёт blocked task+success edge и увеличивает existing nonexpired run reservation/tenant reserved bytes на exact profile output_bytes under quota locks; exact blocked replay не резервирует повторно. Не продвигает cursor, не пишет v2-adopt, не dispatch. Scope helper currently handles common tenant/usage/source locks; concurrent lock-upgrade races требуют SQL evidence/retry review.

FrozenDeliveryContinuationTask.CreateInTransaction передаёт owned task/Params/stage/predecessor в caller-owned transaction. Existing continuation fixture расширен accept/refuse/stage/predecessor binding, SQL privilege assertion расширен. Tests/build/typecheck NOT RUN;01238 NOT APPLIED. Positive SQL quota/idempotency/rollback/fence evidence отсутствует. Transport registration, dedicated activation/seal, export.task_id assignment and runtime observation остаются открытыми. Whole module credit0, completion unknown/null.


## 2026-10-10 — continuation transport registration

FrozenDeliveryContinuationTask.RegisterInputsInTransaction revalidates current lease/Params/descriptor/predecessor through idempotent create predicate, then shared RegisterInTransaction with owned frozen cursor/direct refs. Caller GraphTask.Open/Direct ignored; graph task ID preflight enforced. Caller owns rollback on any registration failure. No run seal or task dispatch occurs here; recovery after committed input registration requires dedicated receipt/profile normalization and is still open. Written01238 quota predicate tightened to require reservation.state=reserved before increment.

Existing continuation fixture expanded with real transport publication double, caller cursor replacement, exact SQL order create→batch→input-seal, refusal before registration. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Shared run seal requires draft/all-blocked graph and cannot be reused for running continuation without dedicated dynamic activation; registry/output policy qualification and dispatch path remain to implement. Full-module credit0, percent unknown/null.


## 2026-10-10 — blocked continuation transport recovery

Написана01239 scheduler-only v30_delivery_continuation_transport_ready: current frozen Params/source/parent lease authority, child barrier stage/cursor, exact blocked task descriptor (profile normalized without v2_input_manifest), backend Params SHA и complete stored v2 input closure, succeeded prerequisite edge. false означает unconfirmed, включая stale authority. Go TransportRegistered получает receipt, RegisterInputsInTransaction проверяет его до idempotent create/batch; acknowledged committed registration не повторяет input batch. Это recovery только blocked registered task, не dispatched activation/readiness/export success.

Existing continuation test обновлён receipt→create→batch→input-seal order, refused create, acknowledged registered retry with one SQL call; privilege assertion расширен. Tests/build/typecheck NOT RUN;01239 NOT APPLIED. Positive SQL closure/receipt/restart/current-authority evidence отсутствует. Dynamic activation/seal and dispatched receipt, next-stage runtime/assembly remain open. Full module credit0, completion unknown/null.


## 2026-10-10 — dynamic continuation activation and reopen

Написана01240 activate_v30_delivery_continuation: current transport receipt/lease/source, locked child/barrier/tasks, exact verified predecessor, live CPU v2-enabled operation registry, complete closure, reserved quota/no other open barriers. Atomically close current barrier and advance stage cursor, seal running child, assign encode export.task_id, shared activate_bound_task, acknowledge predecessor v2-adopt marker only with runnable successor. Task remains subject to shared dispatch resource admission. Reopen function for resize+sharpen path verifies sharpen physical evidence then opens stage1 barrier/unseals child for final encode. Other v2-adoption audit/provenance retention semantics and full qualification parity need explicit audit before enabling this runtime path; marker alone must not be interpreted as whole-module acceptance.

Go transaction adapters ActivateInTransaction/ReopenContinuationInTransaction use private frozen/verified evidence, caller rollback, no IO. Existing continuation fixture activation accept/refuse/stage/predecessor tests authored; privilege assertions extended. Tests/build/typecheck NOT RUN;01240 NOT APPLIED. Positive SQL atomicity/rollback/finalization/dispatch races not proven. Dynamic runtime observer/coordinator and dispatched recovery receipt remain open; final encoded adoption/assembly/API also open. Full module credit0, real percent unknown/null.


## 2026-10-10 — dispatched continuation activation recovery

Написана01241 v30_delivery_continuation_activated: current parent/frozen child authority, required operation/stage, barrier cursor>=stage, exact task descriptor/Params SHA+bytes/profile without transport manifest, retained complete closure, derived predecessor edge+v2-adopt marker and encode export ownership. Acknowledges activation with later task failure/cancellation or reopened graph; это не successful export. Go ActivateInTransaction проверяет receipt перед mutation SQL, предотвращая повторный seal/activation при uncertain committed outcome.

Unit activation order receipt→mutation accept/refuse и acknowledged replay one SQL call добавлены, privilege assertion расширен. Tests/build/typecheck NOT RUN;01241 NOT APPLIED. Positive SQL unknown-commit/reopen/failure/revocation evidence absent; runtime coordinator должен проверять activation receipt также до Params/source pipeline для full restart recovery, это ещё открыто. Full adoption audit/qualification parity, final encoded verification/adoption и assembly остаются открытыми. Full-module credit0, real completion unknown/null.


## 2026-10-10 — recoverable continuation admission coordinator

AdmitContinuation joins already pinned Params/physical prefix to activation receipt→registered receipt→blocked task creation transaction→transport publication outside TX→atomic input registration+activation transaction. Registered recovery skips object publication; activated recovery skips mutations. SQL adapter repeats both receipts inside DB-only retry callbacks, no object IO. Nil graph allowed only when registered transport acknowledged. Uncertain activation commit returns error and same frozen descriptor retry uses persisted receipt. Recovered flag means prior activation observed, not export success.

Existing frozen sharpen/encode fixture invokes unit coordinator scenarios fresh phase order/publication, activated/registered recovery, create/publication/commit errors. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Scheduler result observer/physical prefix assembly and pre-Params restart recovery still need connection; full qualification/adoption audit, final encode verification/assembly remain open. Full-module credit0, real percent unknown/null.


## 2026-10-10 — observation candidates and saved Params recovery

Написана01242 current parent lease/frozen authority read functions. Pending child slots: running admitted child with succeeded derivative and no v2-adopt marker, ordered unique LIMIT1..100. Saved continuation Params: derived next-task owner under parent run, exact compiled SHA, ready physical artifact/version. Allows recovery before publication when child graph is sealed after activation. Functions do not mutate adoption/cursor; NULL remains unconfirmed. Result page bounded; underlying candidate scan/query-plan/fairness not yet proven.

Go scoped readers reject NULL/oversized/duplicate/out-of-range/unordered candidate lists and substituted saved Params identity/SHA. Unit slot parser cases and real compiled sharpen/encode saved-ref/mismatched-SHA cases authored; privilege assertions extended. Tests/build/typecheck NOT RUN;01242 NOT APPLIED. Observer pipeline/source-prefix construction/runtime scheduling still open, as are final encoded adoption/assembly and qualification parity. Full module credit0, real percent unknown/null.


Продолжение: scheduler driver поддерживает ограниченную страницу наблюдения ранее admitted children под той же арендой, проверяет порядок/диапазон слотов до побочных действий, продлевает lease перед каждым child и отдельно считает Observed без изменения admission cursor. Job учитывает наблюдаемый прогресс. Unit-кейсы написаны, NOT RUN. Concrete физический observer ещё не подключён; зачёта полного модуля нет.


V30: concrete DeliveryChildPipeline observer подключён в NewDeliveryExpansionJob. Восстанавливает до двух физических derivative-стадий; расширяет inventory только точными producer refs, отклоняет SHA/contract alias conflicts; проверяет сохранённую активацию до begin/reopen barrier и публикации Params; затем использует continuation coordinator. Object IO вне SQL TX. Unit-тесты inventory и отказа до IO написаны, NOT RUN; сквозной observer integration ещё требует тестирования. Encode adoption, terminal child failures, fairness и финальная assembly остаются открытыми. Полного зачёта модуля нет; процент реализации не повышен.


V30 observer: continuation handoff выделен в используемый production-кодом recovery coordinator. Написаны сценарии fresh/saved Params, recovered activation, ошибок проверки/open/publish/admit, отмены после открытия barrier и recovery при admit. При отмене до publication/admission следующие побочные действия не вызываются. Тесты NOT RUN; SQL-сверка begin/reopen выполнена чтением текущих миграций, не исполнением. Encode adoption/final assembly остаются открытыми; процент не повышен.


V30 encode: написан независимый delivery_encoded_verify для физического JPEG/PNG16/TIFF16: SHA/bytes, geometry от source+Params, ICC/alpha/manifest, metadata strip, file mutation guard и pixel bound до decode. Unit-сценарии JPEG/TIFF и подмены/лимитов/cancellation написаны, NOT RUN. Модуль пока не подключён к SQL-authorized staging/final adoption; photographic pixel replay не доказан. Процент не повышен.


V30 encode preparation: добавлен prepare_delivery_encode поверх task-bound evidence reader: canonical Params/source/manifest, exact роли и output closure, ограниченный SHA-verified stream, приватный temporary scratch с cleanup, независимый raster verifier и повторное чтение evidence после IO. Написаны unit-сценарии valid TIFF, short/overflow, foreign role, extra output, revoked evidence и cleanup. NOT RUN. SQL final adoption и её runtime-включение ещё отсутствуют; photographic replay и финальная assembly не закрыты. Процент не повышен.


V30 SQL encode context: миграция01243 описывает отдельный read-only context двух выходов с parent/item/export/Params/attempt/fence/upload binding. Generic adoption context/reader требуют standalone workflow, которого child delivery не имеет. Поэтому generic v2_adoption_artifact не использован в новом context; существующий derivative stage_result всё ещё использует его и требует исправления. Новая миграция NOT APPLIED; privilege case добавлен NOT RUN. Final adoption/runtime reader остаются открытыми.


V30 derivative read исправлен forward-only миграцией01244: вместо generic workflow-dependent reader проверяются ready output_upload, точные tenant/task/attempt/fence/SHA/role и producer поля artifact, verified version и expiry. Добавлены canonical result attempt/fence и unique output IDs. Negative SQL integration для неизвестных stage/encode authority написан NOT RUN. Миграция NOT APPLIED. Encode evidence reader и final adoption остаются открытыми.


V30 encode reader: миграция01245 добавляет task/result/attempt/fence-bound artifact/staged SQL readers. DeliveryEncodeEvidenceReader использует свежий SQL context при каждом обращении, хранит только immutable location pin и отвергает изменение версии/key/metadata; staged inventory требует ровно два ready выхода. Unit-сценарии revocation/location change/inventory написаны NOT RUN, privilege cases добавлены. Миграция NOT APPLIED. Final adoption/runtime dispatch ещё открыты; процент не повышен.


V30 prepare service: prepare_current_delivery_encode связывает SQL context01243 с fresh reader01245 и физической подготовкой; повторно проверяет exact result/fence/root/Params после IO. Autocommit обязателен до первого SQL, IO вне commit-транзакции. Написаны unit-кейсы revocation/fence/result/root/Params change и transaction refusal, NOT RUN. Final atomic export/item adoption ещё не реализована; runtime dispatch не подключён. Полного зачёта нет.


V30 миграция01246: SQL receipt физической encode verification под tenant/run/task/artifact/upload locks, точные SHA/bytes Params/source/manifest/raster, geometry/ICC/alpha и повторный context. Добавлен record_prepared_delivery_encode без object IO/implicit commit. Receipt v30-encode-verified не является v2-adopt: export/item ready и финализация ещё не реализованы. Privilege case написан NOT RUN; миграция NOT APPLIED.


V30 encode receipt adapter: подготовленное доказательство теперь содержит immutable tenant/task, receipt запрещает cross-scope reuse до SQL. Написаны unit-кейсы exact SQL arguments, strict bool true (1 не принимается), null/empty/multicolumn SQL responses, tenant/task/root substitution и cancellation. NOT RUN. Receipt остаётся только verification; atomic export/item ready adoption ещё открыта. Процент не повышен.


V30 миграция01247: dedicated atomic encode adopter повторяет verification/current authority, сохраняет input/output links на parent workflow, переводит export/item ready и записывает inbox/event/outbox. Replay проверяет точный task/result/Params/manifest/export binding. Добавлен Python caller-owned transaction adapter и unit-case, privilege case. NOT RUN/NOT APPLIED; полная SQL интеграция/lock-order stress не проверены. Runtime dispatch, child finalization и package publication остаются открытыми.


V30 encode runtime coordinator: DeliveryEncodeAdoptionService требует отдельные autocommit read/write connections и валидные scratch/limits; физическая подготовка до TX, dedicated adoption внутри TX, отмена до commit откатывает изменения. Unit-сценарии success/prepare failure/SQL failure/cancel after SQL написаны NOT RUN. Pending candidate discovery и регистрация scheduler loop ещё открыты.


V30 pending encode: миграция01248 и process_pending(limit1..64) добавляют bounded candidate page, проверку UUID/duplicate/length до physical IO, изоляцию AdoptionInvalid с report и propagation infrastructure/cancellation. Unit-кейсы написаны NOT RUN, миграция NOT APPLIED. SQL LIMIT ограничивает выдачу, не доказывает bounded scan; durable rejection/backoff/fairness и scheduler registration ещё открыты.


V30 encode runtime подключён в реальный v2 Adopter.run_once до finalization, deployment run_adopter устанавливает coordinator по PHOTO_ADOPTER_DELIVERY_ENCODE_ENABLED=true и PHOTO_ADOPTER_DELIVERY_ENCODE_MAX_PIXELS; обязательны layer scratch root/bytes. Default disabled, S3 получает ограниченные connect/read timeouts при включении. Config/order unit-кейсы написаны NOT RUN. Durable backoff/fairness/schema preflight и сквозная интеграция остаются открытыми; полного зачёта нет.


V30 encode startup preflight: require_delivery_encode_schema проверяет exact ABI всех6 функций, photo_scheduler, SECURITY DEFINER/photo_fn_owner/EXECUTE, idle autocommit и15s timeout; подключён к обоим соединениям до создания adopter при включении encode. Unit-кейсы каждой missing signature, wrong role и ambient TX написаны NOT RUN. Backoff/fairness и package assembly/publication остаются открытыми.


V30 retry: миграция01249 добавляет result-bound task scheduling hints и exponential delay5..300s, pending selection пропускает текущий отложенный result, новый result не наследует delay. Повторные deferrals во время delay не увеличивают count. Coordinator сохраняет delay в отдельной TX после AdoptionInvalid; callback не меняет producer state и не является terminal rejection. Schema/privilege/unit cases обновлены NOT RUN; миграция NOT APPLIED. Full tenant fairness/terminal policy/package assembly остаются открытыми.


V30 assembly composition: PreparePlan.ObserveMatrix сверяет полную row-major матрицу с frozen child intents и deterministic current export IDs; BuildObservedAssemblyInput вызывает существующий shard publisher только при состоянии assemble. Partial/substituted/pending/failed matrices не публикуют документы. Unit-кейсы identity/ownership/no-publication написаны NOT RUN. Scoped SQL observation repository, cache-binding support и assembly scheduling/publication ещё открыты.


V30 миграция01250 и ReadExportObservation: lease-bound exact frozen child/export/item, run/export states, ready artifact producer/fence/version/adoption marker. Go reader отклоняет stale/foreign identities/unknown JSON. Unit-кейсы написаны NOT RUN, privilege case добавлен; миграция NOT APPLIED. Complete matrix repository и assembly scheduling/publication остаются открытыми.


V30 full matrix reader: ReadObservedMatrix требует admission cursor==slotcount, продлевает parent lease по страницам100, копирует artifact refs и повторяет полный terminal matrix read с canonical equality. Pending/failed матрицы возвращаются без второго прохода; lost lease/change отказывают без публикации. Unit-кейсы stable/change/pending/lost lease написаны NOT RUN. Текущий per-slot SQL требует batch optimization для80000 slots; публикация обязана закрывать read-to-commit race SQL fence. Assembly runtime/publication ещё открыты.


V30 observation page: миграция01251 и ReadObservationPage сокращают network roundtrips до одного на100 slots; full matrix reader использует этот путь. Проверяются exact длина/order/identity каждого slot и lease послеSQL page; partial page не принимается. Unit cases missing/duplicate/slot/foreign/null написаны NOT RUN, миграция NOT APPLIED. SQL сохраняет per-slot authority loops (не set-based оптимизация); стоимость повторного proof validation и assembly publication ещё открыты.


Исправление аудита V30: child создаётся через create_v2_draft_shell (01228), а commit_v30_delivery_child (01227) явно требует child v2_workflows. Прежнее утверждение об отсутствии workflow неверно. Generic context имеет ограничение single output, поэтому encode с2 outputs всё равно требует dedicated context. Прямые upload проверки01244 остаются валидным отдельным путём, но прежний диагноз их необходимости отозван.

V30 publisher key allowlist: разрешены только v30_delivery_slots_000..159 и v30_delivery_assembly, обязательна AssemblyLease. Это только key construction, не SQL publication authority. Unit-кейсы boundaries/unfenced/unknown purpose написаны NOT RUN. Dedicated lease-fenced SQL repository и assembly admission ещё открыты.


V30 shards SQL:01252 проверяет exact row-major shard до500 slots, frozen policy/metadata/provenance, ready/omitted semantics и parentlease;01253 добавляет stable reservation/replay/complete по готовности/версии/квоте с повторной authority. Публикация root assembly этим не разрешена. Privilege cases написаны NOT RUN; миграции NOT APPLIED. Go repository, root assembly authority и task admission ещё открыты.


V30 Go shard publisher: NewDeliveryShardPublisher связывает полный parent admission lease с assemble-task owner; repository использует01253 reserve/complete, owned body, exact scope/token/purpose/SHA/bytes/key/version. Проверяет ordinal shard против реального slotcount. Unit-кейсы exact args и отказов доSQL написаны NOT RUN. Root assembly authority/publication/admission и runtime matrix handoff остаются открытыми.

## Current recount and V30 assembly preparation — 2026-10-10

Recount of current ledger: 90 entrypoints; {'requires_semantic_review': 73, 'missing_runtime_path': 12, 'partial_runtime_path': 5}. Catalogue presence ceiling 86.7%; whole-product real implementation percentage remains unknown. No exhaustive normative denominator or full semantic credits exist, so this number must not be presented as product completion. Static source review confirms current partial matrix/publication primitives; no new complete-module credit.

Added PrepareObservedAssembly: complete current matrix observation precedes writes; pending/failed returns state without publication; lease renews before each shard and after preparation; cancellation, changed observations, publication failures and lost authority return no root result. Tests authored for ready/pending/changed/lost/publication-error/post-publication lease-loss/cancellation, NOT RUN. This helper is not yet runtime wiring or root SQL publication/admission. Root authority/publication, retained assembly closure, assemble dispatch/adoption, public CreateDelivery, interchange, signer, V29 review/Apply and remaining V32 integration stay open. Tests/build/typecheck NOT RUN; migrations NOT APPLIED.

## V30 root assembly publication — 2026-10-10

Added forward-only01254 root scope: exact schema/policy/snapshot/counts/sorted frozen presets; exact ordered unique refs; each exact shard JSON body revalidated through current terminal slot authority and matched to ready verified nonexpired version-pinned backend document/artifact. Root-only01255 reserve/complete repeats scope before and after external object IO, with existing durable identity/token/quota rules; scheduler-only entrypoints, private predicate.

Added NewDeliveryAssemblyPublisher with owned shard bytes and dedicated SQL calls; shard publisher still denies root without explicit proof. PublishDeliveryAssemblyInput joins complete two-pass observation, fenced shard publication, capture of successfully published exact shard bodies, root publication and final cancellation/lease checks. Return is a published root ref, NOT admitted assembly. No runtime driver hookup yet. Dedicated source gate, persisted/retained closure, task Params/admission/seal/activation, assembly adoption/final package state and public CreateDelivery remain open. Root authorization repeats up to80000 slot checks and needs later performance testing; shard bytes proof is currently in-memory, recover by rebuilding immutable matrix, not a durable admission witness.

Authored tests: root proof arguments at reserve/complete; foreign purpose or missing proof refuses before SQL; ready/pending paths; loss before/after root publication; exact published shard bytes carried to root. Extended SQL privilege expectations for new scheduler endpoints. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Presence ceiling remains86.7%, whole-product real implementation percentage unknown; no complete module awarded.

## V30 restart-safe assembly evidence — 2026-10-10

01256 adds bounded exact v30_delivery_document_jcs to existing backend_documents, bound by SHA/bytes constraints and trusted immutable-write trigger. Dedicated shard/root reserve writers persist bytes and exact replay compares them; completion requires persisted bytes. Existing rows acquire bytes only by re-running dedicated reserve under current source authority, not an unverified bulk backfill. Tenant purge uses existing backend_documents lifecycle; no new tenant table.

01257 provides scheduler-only saved root read: root must be ready/verified/version-pinned/nonexpired with exact owner/run/key/SHA/size; rebuilds proof from ordered immutable saved shards and calls full current root/shard/terminal-export authority before returning exact root text. Go ReadPublishedAssembly adds current lease/plan preflight, strict bounded JSON, hash, unique refs, exact frozen policy/presets/snapshot/item and slot counts and canonical bytes. Result is evidence, not subsequent SQL mutation authority.

Unit cases authored for valid recovery, stale authority, hash corruption, policy/count substitution, unknown fields, noncanonical bytes, wrong parent, cancellation and SQL failure. SQL privilege expectations extended. All tests/build/typecheck NOT RUN;01256/01257 NOT APPLIED. Retained artifact closure, task Params/admission/seal/activation, source gate, final adoption and driver integration remain open. No new whole-module credit; whole-product percentage still unknown.

## V30 assembly closure retention — 2026-10-10

Authored01258 retain_v30_delivery_assembly: scheduler-only, reconstructs published root under current saved-assembly authority, derives parent workflow and exact closure (root, ordered shards, frozen preset configs, ready encoded exports, parent task inputs), rejects conflicting hashes per artifact, locks ready/verified/versioned/nonexpired artifacts in UUID order, retains via existing v2_artifact_links with deterministic ordinals and exact replay checks. Uses existing tenant purge lifecycle. Parent lease fenced before transaction success; no object IO in transaction. Publication function now retains before returning root; failure returns no usable root result, leaving replayable published documents.

Authored Go RetainPublishedAssembly and unit cases for accepted/stale/SQL failure/wrong parent/incomplete cursor/invalid hash/cancellation. Updated publication test expectations and scheduler SQL privilege list. Tests/build/typecheck NOT RUN;01258 NOT APPLIED. This is retained-closure preparation, not task admission/activation or worker dispatch. Source gate/admission/runtime/final adoption remain open. Whole-product actual implementation percent unknown; no full-module credit.

## V30 assembly input compiler — 2026-10-10

Added exact schema DeliveryAssembleParams{schema_version,assembly} and CompileDeliveryAssemblyClosure in v2input. Deterministic assemble task identity, Params SHA, root contract, independently required roots, shard/config presence and bounds, conflicting artifact/contract identity, missing dependencies and duplicate required roots checked before common canonical inventory compilation. Inventory is copied/owned before sorting. Caller still must derive required roots from verified documents, supply full transitive inventory and prove current physical/project/SQL authority; compiler is not that proof.

Unit cases authored for stable ordering/digest, missing config, contract substitution, alias, wrong Params SHA/root, duplicate required root and owned cursor. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Complete authority-backed resolver, Params publication, task shell/admission/source gate/activation and driver dispatch remain open. Whole-product percentage remains unknown; no complete V30 credit.

## V30 dedicated assembly Params publication — 2026-10-10

01259 private Params scope validates exact schema/root ref and repeats saved assembly authority plus idempotent complete closure retention.01260 scheduler-only reserve/complete uses dedicated scope on both sides of immutable object IO, owner=deterministic assemble task in parent run, purpose=task_params, existing quota/version/replay protocol. No generic publication or worker gate broadened.

Go PublishDeliveryAssemblyParams compiles canonical V2DeliveryAssembleParams and publishes through dedicated repository with owned bytes and exact parent lease/scope/hash/size/key/version checks. Unit cases authored for reserve/complete arguments and foreign scope/purpose/incomplete cursor/hash/version/key refusal. Scheduler privilege expectations extended. Tests/build/typecheck NOT RUN;01259/01260 NOT APPLIED. Task shell/admission, complete inventory registration, source gate/activation and scheduler driver wiring remain open. Real whole-product implementation percentage unknown; no whole-module credit.

## V30 blocked assemble admission shell — 2026-10-10

01261 creates only blocked assemble task under current complete expansion/root/retained closure/Params authority. Checks deterministic owner/node/operation/CPU renderer/Params/digest, ready exact versioned Params artifact and succeeded prepare predecessor; adds success edge. Fixed manifest output reservation8MiB matches operations contract; exact blocked replay does not increase quotas. Uses tenant/run/expansion/task/quota locks and final lease fence. No worker source gate widened, no activation performed.

Go CreateBlockedAssemblyTask repeats preflight identity/current-plan/full-cursor/canonical Params/hash/CPU output estimate and calls dedicated SQL. Unit cases authored for accepted/stale/foreign task/incomplete cursor/changed Params/profile/cancellation and exact reserve arguments; SQL scheduler privilege list extended. Tests/build/typecheck NOT RUN;01261 NOT APPLIED. Full transport inventory resolution/registration/seal, worker source gate, activation, driver wiring and final adoption remain open. Whole-product real implementation percentage unknown, no full-module credit.

## V30 assembly transport receipt — 2026-10-10

01262 scheduler-only v30_delivery_assembly_transport_ready checks current Params/root/retained closure authority, blocked exact task descriptor and sealed v2 input closure, exact Params/root/shard/config contracts, ready export refs in input inventory, every additional non-Params input retained on the same parent workflow, succeeded prepare edge and current lease. It acknowledges transport only; does not register missing inputs, activate task or seal graph. Complete independently derived inventory/resolver and final source gate remain mandatory.

Go AssemblyTransportReady reuses canonical task/Params/lease preflight with CreateBlockedAssemblyTask. Extended unit cases for ready/missing receipt and invalid identity/cursor/Params/profile/cancel; scheduler privilege expected. Tests/build/typecheck NOT RUN;01262 NOT APPLIED. Execution profile seal/replay consistency must be verified in future integration; no runtime acceptance claim. Registration coordinator, source gate, activation, scheduler dispatch and final adoption still open. Real implementation percentage unknown; no full-module credit.

## V30 actual seal/receipt profile mismatch — 2026-10-10

Static review of actual seal_v2_task_inputs from00534 found execution_profile receives three seal fields, while delivery initial/continuation/assembly receipts removed only v2_input_manifest. This contradicts exact replay comparison for frozen pre-seal descriptors. Authored01263 private v30_delivery_profile_matches: actual requires all three generated fields; expected must contain none; only generated fields are removed, all remaining intent including backend/resources/contracts SHA compares exactly. Initial child receipt still compares full DraftTask identity, now additionally uses this predicate. Continuation transport/activation and assembly transport use same check. Source readiness/closure validation remains mandatory; predicate does not validate manifest bytes by itself.

Added isolated SQL integration cases invoking actual predicate for sealed match, missing seal field, backend/resource substitution, added field and caller-supplied generated field. NOT RUN. Migration01263 NOT APPLIED. No actual admission/activation evidence or full-module credit. Registry contracts_sha vs seal pin compatibility still requires later end-to-end verification. Registration coordinator/source gate/activation/runtime dispatch/final adoption remain open. Real implementation percent unknown.

## V30 assembly registration coordinator — 2026-10-10

Added FrozenDeliveryAssemblyTask: deterministic CPU assemble descriptor, exact Params SHA, bounded immutable input inventory and resource profile requiring fixed8MiB output. Constructor accepts independently verified required roots/resolved inventory and already compiled execution profile; it does not derive/authorize them. Owns profile/cursor/Params bytes. PublishInputs executes transport IO outside admission TX. CreateInTransaction invokes dedicated blocked-shell writer. RegisterInputsInTransaction checks current transport receipt, retries without append if already sealed, otherwise creates exact shell/registers owned cursor/seals inputs/rechecks receipt atomically in caller transaction. Never uses supplied graph.Open or graph.Direct; failure requires rollback. Does not activate task.

Authored unit cases for fresh register/seal/receipt sequence, ready receipt replay, refused shell, foreign task/cancellation preflight, owned profile/roots and hostile caller graph cursor. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Independent canonical root/shard required-root derivation, complete authority-backed resolver, assembly profile compiler, worker source gate, activation, scheduler driver hookup and final package adoption remain open. Real implementation percentage unknown; no whole-module credit.

## V30 independently derived assembly roots — 2026-10-10

Added PreparePlan.AssemblyRequiredInputs: bounded root/shard bytes and hashes; exact frozen Cartesian matrix, row ordering and deterministic admitted-child export IDs; reuse canonical BuildAssemblyInput to require exact canonical shard/root bytes with no actual publication; derive root/shard/config/ready export/source/recipe roots; deduplicate exact same references, reject incompatible hash/contract aliases, deterministic inventory ordering. EncodedExport is the binary raster slot reference, not EncodedImage JSON manifest. This is byte/intent validation only, not current SQL/physical authority. Transitive pixels/masks/other recipe dependencies still require existing current-authority resolver.

Added FreezePublishedDeliveryAssemblyTask deriving mandatory roots before frozen inventory compilation. Unit cases authored for required refs and corrupted root/shard bytes, missing shard, foreign parent/export identities, root/binary alias and cancellation. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Complete resolver and assembly resource profile compiler/source gate/activation/runtime driver/final adoption remain open. No whole-module credit, real completion percentage unknown.

## V30 current-authority shard recovery — 2026-10-10

Added01264 scheduler-only v30_delivery_saved_shard: exact current complete expansion lease, deterministic assembly owner, root ready/verified/versioned/nonexpired identity, exact shard position/ref/purpose, ready saved shard metadata and current shard slot authority; returns at most1MiB text. Does not expose arbitrary artifact locations. Uses existing durable document proof lifecycle.

ReadPublishedAssemblyInputs reads/revalidates full root, renews lease per shard, reads exact saved bytes with scoped SQL and hash/size checks, renews/rechecks full root after pass, derives independent required inputs. No SQL authority caching or partial root success. Transitive recipe/pixel dependencies and physical input resolver still separate; this is recovery of mandatory roots only. Unit scenarios authored for full recovery, missing/corrupt shard, lost lease and stale final root; scheduler privilege list extended. Tests/build/typecheck NOT RUN;01264 NOT APPLIED. Profile/source gate/activation/runtime/final adoption remain open; no full-module credit, actual whole-product percent unknown.

## V30 assembly resource compiler — 2026-10-10

CompileDeliveryAssemblyProfile first derives required roots from exact verified document bytes, validates full supplied inventory/required coverage, computes CPU-only reservations: RAM128MiB+16×document bytes+1KiB per supplied input plus Params; scratch=document bytes+8MiB; output8MiB; VRAM0; pixels0. Checks configured caps before canonical profile. These are conservative planning estimates, not measured capacity/SLA. Worker assembly reads JSON only and does not decode exported rasters. Full transitive input count is charged, not just mandatory roots. CompilePublishedDeliveryAssemblyTask composes this calculation with independent-root frozen task compiler.

Legacy workflow Registry does not contain V2 assemble and was not silently broadened; normative V30 contract fixes CPU/output cap. Later activation/source gate must validate profile independently. Unit cases authored for exact accounting, extra transitive inventory cost, RAM/scratch/output limits, corrupt root and missing required inventory. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Full authority-backed resolver, source gate/activation/runtime dispatch/final adoption remain open. No whole-module credit; real whole-product percent unknown.

## V30 independent SQL assembly resource gate — 2026-10-10

01265 adds private immutable v30_delivery_assembly_resources_ready and requires it at end of current-authority assembly transport receipt. Computes saved root+shards byte total and actual registered input count in SQL; compares CPU renderer profile against RAM128MiB+16×docs+1024×inputs, scratch docs+8MiB, VRAM/pixels0, output8MiB. Bounds docs≤161MiB and inputs≤1million; integer safe-range fields and exact resource keys mandatory. Go estimate now has independent SQL floor before transport acknowledgement; no measured-capacity claim.

Isolated actual SQL predicate cases authored: exact/larger valid profiles; RAM/scratch under-reserve, GPU/output change, fractional/missing/extra fields, excessive input count refuse. Tests/build/typecheck NOT RUN;01265 NOT APPLIED. Full source resolver, worker source gate, activation/driver dispatch and final adoption remain open. No whole-module credit; actual product completion percentage unknown.

## V30 large delivery transport bound — 2026-10-10

Static review found compileCaptureInventory returns every inventory ref as Direct, whereas admission validBuilt and SQL seal cap direct roots127. Prepare/assemble wrappers inherited that behavior, preventing larger sessions/matrices from registering even though indexed inventory supports1million refs. Fixed both wrappers to expose only Params+FrozenDeliveryInput / Params+DeliveryAssemblyInput in direct transport. All snapshot/recipe/image/config/shard/export dependencies remain in exact indexed inventory and input digest; no dependency dropped and global compiler unchanged. Worker TaskArtifacts enforces nested access through V2 index.

Added unit regressions for prepare inventory1003 inputs and assembly160 shards+1000 export refs: direct roots exactly2 while cursor contains all dependencies+Params. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Actual large transport/worker integration still unverified; source resolver/gate/activation/runtime/final adoption remain open. No whole-module credit; real completion percentage unknown.

## V30 server-derived assembly inventory — 2026-10-10

01266 scheduler-only inventory reader revalidates saved complete root and gathers exact root/shard/config/ready encoded export refs plus sealed prepare transitive input entries (excluding old Params/transport documents). Deduplicates exact entries, rejects conflicting hash/contract per artifact, bounds999999 to leave new Params slot, requires every ref retained on parent workflow and currently ready/verified/versioned/nonexpired under artifact locks. No storage keys returned. Complete source dependency claim inherits already admitted/sealed prepare inventory; does not invent recipe dependencies from live latest versions.

ResolvePublishedAssemblyInputs first independently derives mandatory roots from current exact root/shards; reads server inventory with256MiB/entry bounds and strict JSON, exact sorted unique UUID/hash/contract validation, rejects old Params/transport entries and missing/mismatched mandatory roots, final lease renewal. Unit cases authored for valid inventory, missing root, duplicate, wrong contract, unknown fields and null authority; SQL privilege expectations extended. Tests/build/typecheck NOT RUN;01266 NOT APPLIED. SQL worker source gate, activation, scheduler driver integration and final adoption remain open. No whole-module credit, actual product completion percentage unknown.

## V30 exact registered inventory gate — 2026-10-10

01267 replaces assembly transport receipt with an additional full inventory equality check: current server inventory from01266 must equal UUID-ordered registered non-Params entries with exact refs/SHA/contracts. Previous mandatory-root and retained-membership checks could accept a subset missing inherited recipe/pixel dependencies or added retained inputs. Exact equality closes that discrepancy without weakening current source/resource/lease checks. Params remains separately exact checked. No activation or worker gate widened.

Authored isolated scheduler-role integration refusal cases: fabricated descriptor/current-parent/root authority, malformed/missing/null assembly refs, foreign inventory request. NOT RUN;01267 NOT APPLIED. Positive full registered inventory PostgreSQL/object-store integration remains unverified. Worker source gate, activation, scheduler dispatch and final adoption remain open. No full-module credit; whole-product actual completion percentage unknown.

## V30 current-evidence preparation coordinator — 2026-10-10

Refactored existing current root/shard reader to expose owned validated documents while retaining old required-input API. ReadAssemblyPreparation obtains exact documents/mandatory roots/server typed inventory using one document pass with existing initial/final current root checks; shared private resolver avoids a redundant document pass. AssemblyPreparationEvidence is local evidence, not authorization for later mutation.

PrepareCurrentDeliveryAssemblyTask accepts SQL-backed preparation reader, current lease/plan/root/Params and caps only; obtains docs+inventory, computes profile and freezes independently derived task inputs, renews lease before returning. No caller-supplied document or inventory arguments in this coordinator. Tests authored for ready task, reader error, lost final lease, corrupted root and cancellation; failures return no usable frozen task. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. No activation/runtime/adoption completed; no whole-module credit, real completion percentage unknown.

## V30 complete blocked registration pipeline — 2026-10-10

Added DeliveryAssemblyPipeline.PrepareAndRegister: dedicated current-authority Params publication, SQL preparation evidence/inventory recovery, resource/frozen descriptor compilation, SQLDeliveryAssemblyStore committed blocked shell, scoped input transport publication outside transactions, second TX register/seal/final transport receipt. RegisterPreparedAssembly checks sealed receipt first and skips object publication on exact registered replay; renews lease before and after IO; failures return no task ID but preserve replayable committed shell/documents. The returned task ID denotes blocked registered admission, not runnable/dispatched work. Pipeline is not installed on scheduler yet.

Unit cases authored for fresh order, ready replay/no publication, create/renew/register/publication failures. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Worker source gate, graph/task activation, scheduler installation, final assembly adoption/package lifecycle and public delivery API remain open. No whole-module credit; actual product completion percentage unknown.

## V30 lease-independent assembly source context — 2026-10-10

Static inspection confirms current scheduler root readers require open unsealed parent expansion, unsuitable for worker validation after parent seal. Added01268 private v30_delivery_assembly_source_context independent of expansion token/open state. Checks active tenant/current V2 delivery parent/task identities, Params SHA/exact ready document, building package, succeeded adopted prepare result, immutable prepare proof and live prepare source/editor/snapshot authority, exact ready/version-pinned/nonexpired assembly root in sealed task inventory and parent retention. Returns owned database source documents for later full matrix validation. Does not establish worker/resource lease or validate entire child matrix; cannot be used as source-ready gate alone. No runtime-role execution grant, no public worker gate broadened.

Authored isolated SQL tests for private privilege boundaries and foreign scope refusal. Tests/build/typecheck NOT RUN;01268 NOT APPLIED. Full runtime matrix authority, worker wrapper, activation, scheduler wiring and final adoption remain open. No whole-module credit; real completion percentage unknown.

## V30 current export observation for sealed runtime — 2026-10-10

01269 private v30_delivery_current_export_observation removes scheduler lease dependency from the observation primitive for later runtime matrix validation. Requires independently reconstructed frozen request, deterministic parent/slot/child ID, same actor/snapshot/project delivery workflow, exact stored request and deterministic export ID/preset/source, explicit building package/item binding and required flag. Ready raster checks current producer attempt/fence, succeeded uncancelled encode task, adopted result marker and verified/versioned/nonexpired encoded artifact. It is not independent frozen-request authorization and not a worker gate; outer source context/full matrix/worker lease remain mandatory. No runtime role execution granted.

Extended isolated SQL tests for both private predicate privilege boundaries and malformed/out-of-bound child request refusal. Tests/build/typecheck NOT RUN;01269 NOT APPLIED. Full runtime matrix validation/worker wrapper, activation/driver and final adoption remain open. No full-module credit; actual completion percentage unknown.

## V30 runtime matrix and fenced source gate — 2026-10-10

01270 independently reconstructs every frozen child request and exact ready/omitted slot from saved root/shard bytes and current child export authority. Requires inherited input coverage, parent retention and CPU resource floor. 01271 connects this private predicate to the existing public worker wrapper, checks worker/resource lease before and after matrix inspection and preserves the outer tenant-context restoration. Private helpers remain inaccessible to runtime roles.

Static recount of the 90 entrypoint ledger:12 missing,5 partial,73 require semantic review. Presence ceiling86.7%, whole-product implementation percentage unknown. This recount does not establish complete normative requirement coverage. Tests and migrations NOT RUN/APPLIED. Positive full-matrix integration, 80000-slot latency and lease behavior unmeasured. Activation, scheduler dispatch, final adoption/package lifecycle and public delivery API remain open. No whole-module credit.

## V30 atomic assembly activation — 2026-10-10

01272 adds scheduler-only activate_v30_delivery_assembly. Revalidates complete registered transport and runtime frozen matrix before closing expansion/sealing parent graph and activating bound task in one transaction. Requires current open fully advanced expansion fence, uncancelled running V2 parent/blocked first-attempt task, enabled CPU operation and live sufficient run quota. Tenant context restored around private source predicate. Any activation failure after mutation or expired final lease/quota raises to roll back the transition.

Go ActivateAssembly uses the existing independently validated descriptor; transaction-backed caller must roll back on error. Authored unit scenarios cover valid dispatch, SQL refusal, wrong identity/cursor/Params/profile and cancelled context without SQL. NOT RUN;01272 NOT APPLIED. This is a first transition, not a replay receipt: post-commit acknowledgement recovery and scheduler installation still required. Positive SQL integration and lock/performance behavior unverified. Final adoption/public delivery lifecycle remain open. No module completion credit; whole-product percentage unknown.

## V30 committed activation acknowledgement — 2026-10-10

01273 replaces activation with same atomic checks plus a durable inbox marker v30-assembly-activated, deterministic per task. Scheduler-only receipt requires closed complete expansion with exact token, deterministic task, exact descriptor/profile and saved Params SHA/bytes, sealed V2 parent and predecessor edge. Accepts already running or terminal task without requiring expired expansion lease or mutable source readiness; acknowledgement never authorizes execution. Existing purge owns inbox rows; no new tenant table. Does not backfill activation markers.

Go AssemblyActivated and SQLDeliveryAssemblyStore.Activate read receipt before first activation in one transaction. Commit errors remain retryable with the owned descriptor. Added receipt accepted/refused/preflight/cancelled unit cases. Tests/build/typecheck NOT RUN;01273 NOT APPLIED. Closed-expansion discovery and descriptor recovery after process restart, positive SQL integration, scheduler installation and final adoption remain open. No module credit; whole-product percentage unknown.

## V30 driver assembly coordinator — 2026-10-10

DeliveryAssemblyPipeline.ProcessAssembly composes current full matrix observation, dedicated immutable shard/root publication and retention, current Params/evidence/profile preparation, blocked transport registration and transactional activation. Refactored existing PrepareAndRegister to share preparation/registration helpers. Optional DeliveryExpansionDriver.Assembly invokes this only when all child slots admitted; pending and failed states remain explicit. Successful activation suppresses release of already closed expansion; errors release open lease best effort.

Authored driver tests for pending, failed, activated, unknown state and processor errors, cursor preservation and closed/open release behavior. NOT RUN; migrations NOT APPLIED. Hook is not yet installed in production main. Required-export failed package lifecycle, restart recovery discovery, full integration and final assembly adoption remain open. No full-module credit or product completion percentage established.

## V30 scheduler assembly installation — 2026-10-10

NewDeliveryExpansionJob now installs DeliveryAssemblyPipeline using the same pool/object publisher/bucket/resource caps as the child pipeline. Existing workflow/service.go constructor path uses this job when PHOTO_DELIVERY_SCHEDULER_ENABLED=true; default remains false pending remaining package lifecycle. Job credits successful assembly activation as work even with no new child admission/observation. ICC pin syntax now uses strict lower-case SHA256 validation rather than length only.

Authored no-IO constructor test confirms pipeline/caps/bucket installation and invalid digest rejection; scheduler job tests distinguish activated from pending/failed work credit. Tests/build/typecheck NOT RUN; migrations NOT APPLIED. Required-failed/cancelled package completion, post-process-restart receipt discovery, final physical assembly adoption and public create-delivery API remain open. Installation is authored, not runtime evidence. No whole-module credit; completion percentage unknown.

## V30 failure witness preparation — 2026-10-10

Execution contract95 requires parent/package failure for required terminal child failure or forbidden optional omission even with pending peers. Added PreparePlan.FindFailureWitness: independently validates complete ordered frozen matrix through ObserveMatrix, selects first failed slot and recomputes deterministic child/export identity with bounded reason. Local witness never authorizes SQL mutation; repository must recheck current child terminal failure and frozen omission policy under expansion lease. Pending/success matrices return no witness.

Authored failed/cancelled/pending/ready/foreign/missing/cancelled-context cases. NOT RUN. SQL failure transition, run/package event and quota cleanup are not yet implemented by this increment; no scheduler failure finalization credit. Existing assembly path unchanged. Whole-product completion percentage unknown.

## V30 SQL failure witness authority — 2026-10-10

01274 private v30_delivery_failure_witness re-derives frozen child request/policy through current expansion proof, locks expansion and building package, requires admitted slot/current token and lease, refuses optional omissions permitted by policy, and obtains current exact child/export observation with package required binding through01269. Only terminal non-ready forbidden slots yield deterministic reason/child/export/package IDs. Ready or pending children yield no witness. All runtime roles denied direct execute; predicate is intended for subsequent fenced finalization only.

Authored SQL privilege and unknown/out-of-range slot refusal cases. NOT RUN;01274 NOT APPLIED. Positive real required/optional SQL scenarios, parent/package terminal mutation, events and quota/output cleanup remain open. No module completion credit; real implementation percentage unknown.

## V30 fenced failure transition — 2026-10-10

01275 fail_v30_delivery revalidates current private witness under tenant/run/expansion locks, refuses active parent worker tasks or other open parent barriers, fails unpublished building package and exact item, cancels blocked/ready parent tasks, seals failed parent and closes expansion. Publishes run event/outbox with deterministic event identity; final expired fence raises transaction rollback. Parent quota deliberately retained until output uploads/backend documents settle. Child runs retain independent lifecycle.

Go FailPackage validates lease/plan and deterministic witness identities before SQL; caller rolls back on error. Authored accepted/refused/wrong child/export/slot/reason/cancelled context cases. NOT RUN;01275 NOT APPLIED. Positive PostgreSQL cases, scheduler hookup, replay acknowledgement and cleanup-gated quota settlement remain open. No full-module credit; real completion percentage unknown.

## V30 scheduler failure finalization wiring — 2026-10-10

FinalizeCurrentDeliveryFailure re-reads current frozen matrix, derives first forbidden failure witness, renews authority and calls transaction-backed writer. SQLDeliveryAssemblyStore.FailPackage commits dedicated01275 function and rolls back errors. ProcessAssembly invokes coordinator for failed matrix; only successful commit returns finalized_failed. A changed/nonfailed matrix yields pending for retry. Driver skips release for committed failed transition and job counts it as work. Quota remains reserved for cleanup settlement.

Authored coordinator failed/pending/foreign/lost lease/write error/cancel cases; expanded driver/job state tests. NOT RUN; migrations NOT APPLIED. Full positive SQL integration, failed receipt/restart handling, cancellation lifecycle, cleanup-gated quota settlement, final assembly adoption and public API remain open. No module credit or whole-product percentage established.

## V30 cleanup-gated quota settlement — 2026-10-10

01276 adds release_v30_delivery_quota for sealed failed/cancelled V2 delivery with matching unpublished package state. Requires terminal parent tasks, ready/cleaned output uploads and backend documents; locks tenant usage/run/reservation, validates user owner and subtracts remaining bytes exactly once. Selector skips unsettled candidates. Ready artifacts remain in existing physical storage accounting; no object deletion or child reservation settlement implied. Wraps generic finalize_v2_run so delivery must use dedicated lifecycle and cannot finalize through generic graph success.

Go ReleaseNextQuota and optional job cleanup hook installed in existing opt-in constructor; release counts as scheduler work. Authored true/false settlement, nil store and cancellation preflight cases. NOT RUN;01276 NOT APPLIED. SQL concurrency/accounting and cleanup integration unverified. Failed commit receipt, cancellation/package synchronization and successful manifest adoption remain open. No module completion credit; product percentage unknown.

## V30 scheduler cleanup control flow — 2026-10-10

Job now rejects already cancelled context before any cleanup/claim call. Authored cleanup job tests assert cleanup-before-claim, no claim on cleanup error, one shared bounded45s context, no work credit when empty, retained release credit even when subsequent claim fails, and no calls under cancelled context. Constructor test explicitly requires installed cleanup hook.

Tests/build/typecheck NOT RUN; migrations NOT APPLIED. No live settlement or product completion evidence inferred from authored tests. Cancellation/package synchronization, failed receipt/restart recovery and successful assembly adoption remain open. Whole-product implementation percentage unknown.

## V30 cancellation/package synchronization — 2026-10-10

01277 sync_v30_delivery_cancellation locks tenant/cancelled sealed V2 parent, requires terminal parent tasks and delivery workflow, transitions only unpublished building/review package to cancelled with revision guard and deterministic outbox event. Retains immutable refs; never changes ready/published or failed packages. cleanup_next_v30_delivery selects one eligible cancellation then attempts one settled quota release. Existing job method now calls combined cleanup and credits either committed change; both operations execute within one SQL statement transaction.

Authored runtime privilege and unknown scope SQL refusal cases. NOT RUN;01277 NOT APPLIED. Positive cancellation/concurrency/cleanup/accounting integration unverified. Child runs remain independent; required cascading cancellation policy, failure replay and successful assembly adoption still need implementation/review. No whole-module credit; actual completion percentage unknown.

## V30 failure commit acknowledgement — 2026-10-10

01278 stores a deterministic per-slot failure marker in the same transaction as parent/package failure and outbox event. Receipt requires closed delivery expansion with exact preserved token/admitted slot, marker, failed sealed V2 parent and unpublished failed package/item bound to deterministic child export. fail_v30_delivery acknowledges this committed receipt before fresh source/lease checks; a same-descriptor retry does not emit duplicate events or mutate quota. Marker not backfilled.

Authored runtime role and unknown/out-of-range receipt/finalization SQL refusal cases. NOT RUN;01278 NOT APPLIED. Positive commit-loss replay and process-restart discovery not proven. Contract search found explicit independent semantic children but no explicit delivery cascading cancellation requirement; previous audit's mandatory-cascade wording was unsupported. Delivery child lifecycle remains independent pending normative clarification/review. Successful final assembly adoption and API remain open. No module credit; product completion unknown.

## V30 manifest output validation — 2026-10-10

Added Go Manifest matching worker DeliveryManifest and PreparePlan.ValidateManifest. Bounds output8MiB, verifies result SHA, independently validates complete frozen assembly root/shards through AssemblyRequiredInputs, derives ready/omitted counts and requires exact canonical output bytes with identical ordered shard refs/assembly SHA/snapshot digest. No unknown fields, noncanonical JSON, substituted counts or shard roots accepted. Returns owned ref slice. Pure validator does not authorize DB adoption or establish physical producer/version/current readiness.

Authored valid and corrupted count/assembly/shards/unknown/noncanonical/hash/source/parent/cancellation cases. NOT RUN; migrations NOT APPLIED. Physical output recovery, SQL attempt/fence adoption, package review/publication and successful quota settlement remain open. No full-module credit; real completion percentage unknown.

## V30 assembly adoption input recovery — 2026-10-10

01279 adds scheduler-only recovery for exact succeeded CPU assembly/result/attempt/fence and current V2 adoption context. Revalidates whole frozen source matrix under restored tenant context then returns saved exact root/shard strings and proof/package/Params identity, independent of closed expansion lease. This is input evidence only, not output verification or package completion. Generic adopt_v2_plan now refuses assembly while preserving prior wrapper chain for other operations. Dedicated commit must call the private prior chain after its own validation.

Authored role-boundary and unknown/invalid-attempt recovery refusal cases. NOT RUN;01279 NOT APPLIED. Positive PostgreSQL/object recovery, physical output identity/bytes, dedicated package transition and review/publication remain open. No full-module credit or whole-product completion percentage.

## V30 physical manifest reconstruction primitive — 2026-10-10

Added Python verify_delivery_assembly on existing adoption reader evidence/read_json interface. Checks scoped input/output evidence, JSON MIME/byte bounds/SHA, canonical Params and assembly/shard semantics via independent assemble_delivery reconstruction, output alias against Params/root/shards/config IDs, exact canonical result bytes and singleton staged output closure. Returns frozen verified Params/result bytes. Existing reader remains responsible for exact physical object version and producer metadata.

Authored valid reconstruction and role/source/hash/extra-output/count/cancellation refusal cases. NOT RUN. Primitive not yet installed in adoption dispatcher; SQL package transition, replay and review/publication remain open. No runtime physical verification claim or module credit; product completion unknown.

## V30 mandatory adoption dispatcher reconstruction — 2026-10-10

prepare_v2_adoption now requires Params and invokes independent verify_delivery_assembly before generic output graph traversal for v2.delivery.assemble.01280 provides scheduler-only exact Params ref lookup scoped to succeeded uncancelled task, canonical result, attempt/fence, ready verified versioned/nonexpired JSON artifact and declared Params input plus current adoption context.

Authored dispatcher missing-Params refusal and SQL privilege/unknown-scope Params lookup cases. NOT RUN;01280 NOT APPLIED. Service branch/dedicated SQL assembly commit still absent; generic SQL assembly adoption remains denied by01279. Positive full physical reconstruction/dispatcher integration and package lifecycle remain open. No module credit or actual completion percentage.

## V30 SQL manifest authority — 2026-10-10

01281 private v30_delivery_manifest_authority recovers current source evidence via01279, recomputes ready/omitted/total counts from saved shards, requires exact manifest JSON semantics with original ordered shard refs and assembly SHA, and binds singleton canonical result output to manifest SHA/byte length/MIME/document role, producer task/attempt/fence and verified nonexpired physical version. Runtime roles denied. Predicate does not adopt output or attach package manifest; generic private adoption must still check staged upload readiness inside dedicated commit. Python reconstruction supplies exact canonical byte validation.

Authored private role boundaries and foreign/empty/null-body refusal cases. NOT RUN;01281 NOT APPLIED. Positive SQL semantics/physical output checks, dedicated package state/review commit and service dispatcher integration remain open. No module credit; product completion unknown.

## V30 dedicated assembly commit — 2026-10-10

01282 adopt_v30_delivery_assembled combines01281 full current manifest authority with prior private generic adoption physical upload/inventory retention checks, requires succeeded parent tasks/sealed graph/closed barriers and building unpublished package. Materializes exact ready/omitted items with revision/count gates, attaches manifest, creates deterministic pending delivery review when policy requires and sets package review/ready and parent waiting_review/completed respectively. Emits assembled run/outbox events in same transaction; publication timestamp untouched. Quota retained pending successful settlement path.

Authored scheduler-only privilege and unknown-scope commit refusal cases. NOT RUN;01282 NOT APPLIED. Positive full SQL transaction, review contract behavior, replay acknowledgement, service branch and successful quota/publication remain open. No module completion credit; product percentage unknown.

## V30 assembly adoption service installation — 2026-10-10

AdoptionCoordinator now resolves assembly Params through01280, verifies physical manifest reconstruction outside write transaction, runs prepare_v2_adoption with exact Params and selects01282 dedicated commit passing owned verified Params/result bytes. SQL scope/inventory/output/source/package authority remains rechecked at commit; no generic fallback. Existing bounded SQL retry behavior preserved.

Authored service tests for correct SQL/result/attempt/fence routing, physical failure/stale Params before transaction, and SQL refusal propagation. NOT RUN; migrations NOT APPLIED. Positive real evidence-reader/SQL/object-store integration and package review contract unverified; adoption replay recovery, success quota settlement and publication remain open. No module credit or real completion percentage.

## V30 closed-context assembly recovery — 2026-10-10

01283 adds exact committed adoption receipt requiring generic and dedicated inbox markers, succeeded exact task/result, attached deterministic manifest producer attempt/fence and review/ready/cancelled package binding. Dedicated marker commits with package transition; does not depend on outbox retention and is not backfilled. Receipt acknowledges historical commit only, not current publication/source authority. AdoptionCoordinator falls back on exact V2_ADOPTION_CONTEXT_STALE, returns true on strict boolean receipt without object IO, and rethrows stale on absent receipt.

Authored strict boolean/null/malformed receipt and closed-context service acknowledgement cases. NOT RUN;01283 NOT APPLIED. Positive SQL replay/marker retention integration and impacts on existing stale-context tests unverified; success quota, publication and public create API remain open. No full-module credit; product completion unknown.

## V30 successful assembly quota settlement — 2026-10-10

01284 extends existing release and selector to waiting_review/review and completed/ready pairs only with attached manifest and durable exact assembly adoption receipt. Parent tasks must remain terminal; all output uploads and backend documents ready/cleaned. Only remaining q.bytes deducted from reserved_bytes; storage accounting untouched and child reservations independent. Existing cleanup scheduler path picks these reservations without new job or execution flag. Failure/cancellation gates preserved.

Authored role boundaries and unknown quota/receipt scope SQL cases. NOT RUN;01284 NOT APPLIED. Positive successful/review/cleanup race/accounting scenarios remain unverified. Publication human acceptance/CAS/current source checks and public CreateDelivery remain open; whole module incomplete and product completion percentage unknown.

## V30 current publication source authority — 2026-10-10

01285 extends only private source context/current export observation to exact running/building, waiting_review/review and completed/ready pairs so publication can revalidate post-assembly sources. Existing worker lease gate still requires running leased task. New private publication authority requires active tenant/editor session/project authority, exact package revision/unpublished review or ready, durable attached assembly receipt, ready verified/versioned/nonexpired manifest and independently reconstructed canonical Params followed by full current frozen source matrix gate. No public execution grants or publication mutation added.

Authored private-role and unknown/out-of-range revision refusal cases. NOT RUN;01285 NOT APPLIED. Positive post-adoption source/ACL/revocation and canonical Params reconstruction integration unverified. Publish CAS/human acceptance/publication/audit transaction and public API remain open. No full-module credit; product completion unknown.

## V30 publication transaction — 2026-10-10

01286 API-only publish_v30_delivery requires current editor session/CAS and01285 complete current publication source authority. For review, locks exact deterministic pending manifest review and atomically records human accept, parent completed, ready package with published_at, audit manifest SHA and run/outbox events. Ready/completed path publishes without another review. Exact same actor/request/expected-revision retry returns deterministic original publication ID without mutation. No messaging integration.

Architecture decision: legacy photo.publications has mandatory collection item/asset_version foreign keys and cannot represent multi-export package. Delivery publication is immutable delivery_packages row with published_at; UUIDv5(package,delivery:publication:1) and audit details form receipt. Existing package guard prevents modification after publication, existing tenant purge owns rows; no new tenant table.

Authored API-role foreign scope/invalid revision refusal cases. NOT RUN;01286 NOT APPLIED. Positive publication/review/CAS/replay/ACL race SQL integration, HTTP/client and public CreateDelivery remain open. No whole-module credit; product completion unknown.

## V30 PublishDelivery HTTP/service — 2026-10-10

Added normative POST /delivery-packages/{id}/publish. Session/CSRF checked before handler; strict JSON CAS decoded; Service.Publish uses editor WithTenant transaction. PublishPackage validates canonical safe decimal revision and bounded trimmed Idempotency-Key, derives deterministic actor/package/key request UUID, calls01286 and reads standard Resource before transaction commit. Null publication maps409; no storage locations returned.

Authored invalid CAS/key/cancelled preflight unit cases. NOT RUN; migrations NOT APPLIED. Positive HTTP/auth/SQL/replay/client integration remains unverified; exact saved response idempotency and conflict-code classification require further review. Entrypoint ledger intentionally not promoted from missing without complete semantic coverage. Public CreateDelivery and remaining V30/V31/V32 scope remain open. Whole-product percentage unknown.

## V30 publication response idempotency — 2026-10-10

Service.Publish now uses existing idempotency.Use in editor tenant transaction with actor/package route scope, CAS request hash,24h record and saved exact canonical Resource. Same key/changed request conflicts before domain mutation; replay avoids Resource re-projection.01287 current project editor authorization executes before ledger replay. Shared ValidKey syntax replaces local200-byte/trim-only rule. First mutation+response ledger commit atomically.

Authored HTTP request decoding/scope/key propagation cases for valid string revision, numeric revision, unknown/duplicate fields. NOT RUN;01287 NOT APPLIED. Full session/CSRF/editor role, positive ledger replay/conflict and SQL transaction integration unverified. Client integration and public CreateDelivery remain open; no whole-module credit or real completion percentage.


## Пересчёт по текущему исходному коду — 2026-10-10

90 точек: 11 отсутствуют, 6 частично реализованы, 73 требуют семантической сверки. Исправлена устаревшая missing-отметка v2.delivery.assemble.execute: adapter.py регистрирует операцию в условном CPU runtime. 87,8% — только верхняя граница наличия путей, не процент полной реализации. Процент всей системы не установлен: нет полного взвешенного реестра требований с текущим семантическим зачётом.

Продолжена публикация V30: PublishWithReceipt сохраняет признак повтора общего idempotency ledger; HTTP возвращает Idempotency-Replayed при повторе. Проверки не запускались; миграции не применялись. CreateDelivery, interchange, остаток review/Apply, production signer и эксплуатационные сценарии остаются открытыми.


## V30 клиент чтения и публикации — 2026-10-10

Добавлены delivery-package.ts и OwnerAPI.deliveryPackage/publishDelivery. Перед IO проверяется UUID scope; ответ связывается с tenant/project/package, envelope revision и manifest artifact. Публичная модель не копирует произвольные поля сервера. Publish отправляет только строковую CAS-ревизию и исходный idempotency key, сохраняет intent до завершения IO и проверяет ready/published_at/manifest/revision+1. Истёкший document допускается в metadata, но не подтверждает успешную публикацию. prepareDeliveryPublication требует явного acceptance для review и запрещает building/failed/cancelled/повторное действие по уже опубликованному пакету.

Unit-тесты authored в delivery-package.test.ts: scope/envelope/manifest substitution, mutable transport snapshots, safe decimal CAS/key domain, exact request shape, cancellation before/after IO, review confirmation, terminal refusal. NOT RUN. Экран Delivery, public CreateDelivery и сквозная интеграция остаются открытыми. Общий процент реализации не установлен; каталог по-прежнему 11 missing/6 partial/73 semantic-review, верхняя граница наличия87,8%.


## V30 экран пакета — 2026-10-10

Добавлен /deliveries/[id] (tenant/project query) и DeliveryPackageScreen. Чтение/refresh, ready publication, исходный CAS/key при неизвестном результате, refresh reconciliation опубликованного состояния, очистка при потере доступа, abort при смене scope и игнорирование позднего ответа. Локализации EN/RU/BG. DOM tests authored для публикации, отказа blind review/terminal actions, стабильного retry, reconciliation, access loss, scope cancellation. NOT RUN.

Review-публикация в UI остаётся запрещена до просмотра полного frozen manifest и явного acceptance. Экран не выдаётся за полный V30: list/CreateDelivery navigation, manifest items/review, interchange и интеграционные сценарии открыты. Реальный процент всей системы неизвестен; новых полных зачётов нет.


## V30 revision-bound items API — 2026-10-10

Authored01288 get_v30_delivery_items: scoped viewer read, package shared lock, exact revision, bounded keyset pages of metadata, no storage addresses. Added Service.Items/ReadItems and GET /delivery-packages/{id}/items, strict query/cursor bounds and409 stale revision mapping. Unit cases authored for scope/order/owned bytes/invalid query/stale/cancellation and HTTP parsing/defaults. Tests NOT RUN;01288 NOT APPLIED. Client page aggregation/full frozen-manifest review UI remain open. No whole-module credit.


## V30 клиент полной пагинации состава — 2026-10-10

Добавлен delivery-items.ts: exact-shape item/page decoder, package/revision/manifest binding, strict `(ordinal,preset_id)` order/cursor progress, repeated identity refusal,80000-item aggregate ceiling, cancellation before/after IO, no partial success. OwnerAPI.deliveryItems copies package snapshot and loads pages with original revision. Экран показывает виртуализированный список только после terminal page, сбрасывает его при refresh/scope change и очищает package при потере доступа.

Unit/DOM tests authored for ordered pages/owned bytes/foreign revision+manifest/omission/cursor/duplicates/page failure/cancellation/request shape/list/reset/access loss. NOT RUN. Полная сверка списка с frozen manifest и review acceptance ещё открыта; publication из review не включена. Общий процент неизвестен, полного зачёта V30 нет.


## V30 semantic inspection API — 2026-10-10

Authored01289 inspect_v30_delivery: editor/current publication authority, complete saved frozen shard-to-delivery_items equality, count reconciliation and expected manifest reconstruction. Go validates canonical expected manifest SHA against attached artifact, shard bounds and response scope; GET /inspection exposes bounded counts+manifest proof. Unit cases authored for canonical hash/count/scope/CAS/cancel/stale binding. NOT RUN;01289 NOT APPLIED. Positive SQL/current ACL/provenance/80k inventory checks and HTTP/client proof integration remain unverified/open. Review acceptance UI remains disabled. No whole-module credit.


## V30 клиент inspection и явный review acceptance — 2026-10-10

Добавлен delivery-inspection.ts: exact response shape, package/revision/manifest binding, complete item count, ordered duplicate-free all-ready/optional-omitted set and ready/omitted reconciliation. OwnerAPI.inspectDelivery copies displayed package/items before IO and propagates abort. DeliveryPackageScreen requires loaded complete list, successful inspection and explicit checkbox before review publication; refresh/reload resets proof and consent. Retry preserves the original CAS/key even after a refresh. Ready publication continues through its existing command; SQL always repeats live authority. EN/RU/BG labels updated.

Unit/DOM scenarios authored for foreign/stale/partial/mismatched proof, item policy/order/count, mutable IO inputs, cancellation, review checkbox sequence and failed inspection refusal. NOT RUN.01289 NOT APPLIED. This is authored UI/API integration, not verified review acceptance. Full CreateDelivery/list navigation, interchange, runtime qualification and deep tests remain open; no whole-module credit.


## V30 CreateDelivery immutable intent — 2026-10-10

Добавлены CreateRequest/FreezeCreateIntent/CreateIdentity: обязательные contract fields и policy booleans/arrays, disjoint unique preset allowlist, UTF-8/rune name bounds, shared idempotency key syntax, owned canonical request bytes/hash. Graph/package/prepare/input/params/policy IDs детерминированы tenant+actor+key; изменение payload сохраняет graph identity и меняет request hash для будущего durable conflict check. Session rotation не создаёт второй граф; разрешения должны перепроверяться при admission. Valid currently unsupported metadata/provenance modes явно возвращают503, не подменяются strip/off.

Unit tests authored for immutable request/output, identity scope/payload hash/session rotation and required/null/unknown fields, preset/key/name/mode boundaries. NOT RUN. Это preflight/identity слой, не публичный CreateDelivery: SQL snapshot/config resolution, durable pending snapshot, document publication/shell/register/activation/replay coordinator ещё открыты. No whole-module credit.


## V30 CreateDelivery catalog preflight — 2026-10-10

Authored01290 preflight_v30_delivery_create: current editor/project download policy, actor-owned retained snapshot/source inventory, accepted versions, distinct requested active presets in intent order. CreateIntent.ResolveCatalog binds tenant/actor to the frozen intent, copies catalog data, validates preset revision/config semantics/canonical SHA, and explicitly refuses unsupported watermark/metadata. Unit cases authored for scope/canonical bytes/stale/cancel/hash/ID/revision/count corruption. NOT RUN;01290 NOT APPLIED.

This resolves catalog metadata only. Recipe selection/source document physical IO, complete dependency closure, durable pending freeze/first-writer conflict, document publication and shell/register/activation coordinator remain open. No public CreateDelivery endpoint or full module credit.


## V30 CreateDelivery document compiler — 2026-10-10

Добавлен CreateIntent.CompileDocuments: owned canonical snapshot/config/policy/FrozenInput/Params publication intents and exact PreparePlan, deterministic document IDs and matching SHA. Requires independently authorized pinned SessionSnapshotItem recipe/source bindings; does not choose latest recipe, grant authority or upload objects. Unit cases authored for deterministic identity/bytes/plan binding/input ownership/wrong inventory/config/recipe/cancellation. NOT RUN.

Snapshot catalog can be source-only (e.g. render input snapshots). Recipe pinning needs an explicit authorized producer/source path before use of this compiler; no arbitrary most-recent recipe fallback added. Document purpose labels are compilation intents and still require backend publication allowlist/reservation integration. Pending snapshot, source recipe resolution, complete closure and shell/register/activation remain open. No public CreateDelivery route or full-module credit.


## V30 explicit recipe selection authority — 2026-10-10

Authored01291 resolve_v30_delivery_selection: validates complete ordered explicit recipe refs against retained actor-owned snapshot inventory, accepted versions and live v26_render_recipe_scope, returns canonical SessionSnapshotItem inputs with exact catalog source/canvas metadata. CreateIntent.ResolveSelection binds tenant/actor/session/snapshot, validates returned snapshot/item schema and exact recipe list. No arbitrary latest selection and no public request extension. Unit cases authored for recipe/scope binding, invalid/stale/substituted/cancelled requests. NOT RUN;01291 NOT APPLIED.

Still requires a durable server recipe-selection producer/pinner for source-only snapshots; this function checks already selected recipes and does not choose them. Physical document/source IO, pending freeze and publication/admission coordinator remain open. No public CreateDelivery route or full-module credit.


## V30 durable pending creation evidence — 2026-10-10

Added CreatePending strict bounded canonical codec and Service.PinCreatePending using existing idempotency_records under tenant/editor authorization. Scope is POST /delivery-packages, original canonical request hash,24h lifetime. Saved pending wins, changed request conflicts, expired pending410. Fresh catalog must still match the compiled observation; pinned recipe/source selection reauthorized and exact item bytes compared. No object IO, run creation or dispatch. Unit codec cases authored for roundtrip/session rotation/ownership/scope/hash/schema/recipe/cancellation. NOT RUN.

DB transaction/concurrency/replay integration not tested. Early saved-pending reader before fresh compilation and final public ticket/replay coordinator still required. Recipe producer/pinner, physical closure, document publication and graph admission remain open. No public CreateDelivery or full-module credit.


## V30 early pending recovery — 2026-10-10

Added CreateIntent.ReadPending/Service.RecoverCreate before mutable catalog resolution: current editor reauthorization, ledger scope/hash/expiry handling, completed-state marker, bounded pending decode and live exact source/recipe selection recheck without current preset reread. Private evidence is never returned as a public JobTicket; completed replay still requires final coordinator ticket validation. Unit cases authored for absent/empty/completed/conflict/expired/denied/foreign/cancelled paths. NOT RUN. Positive saved-pending SQL/concurrency and final CreateDelivery admission/publication coordinator remain open. No whole-module credit.


## V30 creation publication sequence — 2026-10-10

Added CreateIntent.PublishDocuments: validates complete canonical byte/hash/ID/purpose sequence and private PreparePlan proof bindings before any publisher callback; owns bytes against concurrent caller mutation; publishes snapshot/preset/policy/frozen input before Params; rejects changed returned refs and stops on cancellation/error. Unit cases authored for ordering/ownership/no-IO preflight/error/foreign receipt/cancellation. NOT RUN.

This is the publication coordinator boundary, not an installed storage adapter: CreateDocumentPublisher still requires dedicated pending-authorized reserve/complete SQL, purpose allowlist and quota integration. Generic artifactjson allowlist was not widened without authority. Final shell/register/activation and public CreateDelivery remain open. No full-module credit.


## V30 pending document authority — 2026-10-10

Pending snapshot now pins project and exact ordered document purpose/ref/body_jcs entries; DecodePending recompiles every document and rejects changed bytes/IDs/purposes/missing records. Bounded envelope ceiling64MiB includes both catalog config bytes and exact publication bodies (raw JSONB decode ceiling80MiB). Added private01292 document scope: exact pending hash/actor/key/project/document binding, live recipe/source selection, deterministic run/prepare owner. It grants no reservation/upload/dispatch authority by itself and is revoked from runtime roles. Unit document-substitution cases authored NOT RUN;01292 NOT APPLIED. Dedicated reserve/complete wrappers, run shell lifecycle and installed storage adapter remain open.


## V30 pending-authorized draft run — 2026-10-10

Authored01293 begin_v30_delivery_create_run and CreateIntent.BeginRun: lock pending ledger before tenant, reauthorize exact pending Params/source inventory, derive run/workflow IDs, bind canonical config/project/snapshot/policy, create only unsealed draft run/workflow. No task, dispatch or graph/output reservation yet; document reservations must account separately. Exact draft replay allowed; foreign/cancelled/changed config refused. Unit cases authored for scope/config/refusal/cancellation, NOT RUN;01293 NOT APPLIED.

Static review also corrected CreateDocuments.Policy to use the compiler-normalized frozen policy (preset UUID lists sorted), matching AdmitPackage rather than the original request order. Publication checks the same frozen policy. Reserve/complete/storage integration, quota+blocked task materialization and activation remain open. No public CreateDelivery/full-module credit.


## V30 creation document reserve/complete SQL — 2026-10-10

Authored01294: extends existing purpose/byte constraints by preserving installed predicates; creation snapshot≤8MiB, other inputs≤1MiB, Params≤64KiB. Dedicated API-only reservation validates exact pending document/live selection/current draft run, charges per-document reserved bytes, preserves supplied artifact ID, rotates pending token and replays verified ready rows without recharging. Complete binds token/SHA/bytes/bucket/version, atomically registers artifact and moves reserved bytes to stored bytes.01292 now uses artifact-specific owner for non-Params documents so multiple preset docs do not collide; Params owner remains deterministic prepare task.

NOT APPLIED; SQL positive/race/quota/fence/cleanup cases NOT RUN and not yet authored. Go object-storage adapter and graph quota/materialization/activation remain open. No public CreateDelivery/full-module credit.


## V30 Go create storage adapter — 2026-10-10

Added CreateStoragePublisher and SQLCreateDocumentStore: canonical byte/hash/purpose limits preflight, tenant/editor reserve/complete in separate transactions, object IO outside SQL, exact supplied artifact ID, ready replay without object IO, recovery of same immutable key on Exists/Unavailable, strict physical key/hash/size/MIME/version checks and exact completion receipt. Unit cases authored for success/recovery/ready replay/corrupt object/cancellation/stale completion/invalid preflight. NOT RUN. SQL-to-storage positive/race/quota integration unverified; construction in public CreateDelivery coordinator, complete closure/graph materialization/activation still open. No whole-module credit.


## V30 blocked preparation materialization — 2026-10-10

Authored01295 materialize_v30_delivery_prepare: exact pending identity/Params, current source selection/draft run, every pinned backend document physically ready, CPU profile floors matching Go document-bytes/direct-count estimate, zero VRAM/pixels and8MiB output reservation. Creates only one blocked prepare task and run quota atomically, exact blocked retry checks existing reservation. CreateIntent.MaterializePrepare accepts owned FrozenPrepareTask, binds identity/scope and maps SQL refusal409. Unit cases authored for typed task/scope/refusal/empty/cancel preflight. NOT RUN;01295 NOT APPLIED.

Still open: positive/race/expired reservation recovery and draft cleanup SQL integration, complete transitive closure producer, transport registration+proof/package/seal/activation, and public coordinator. No full-module credit.

## V30 pinned proof/package admission — 2026-10-10

Current ledger recounted: 90 entries, 11 missing, 6 partial, 73 awaiting semantic review. 87.8% remains a catalogue presence ceiling; whole-product implementation percentage remains unknown. No new whole-module credit.

Added CreateIntent.AdmitPreparedPackage: validates owned pending bytes and actor/tenant, recompiles documents for the current session, saves exact preparation proof then admits the deterministic package using the same caller transaction. Caller must roll back on either error. SQL independently enforces ready documents and registered task inputs. No graph sealing, dispatch or idempotency completion is implied. Unit cases authored for ordering, deterministic package scope, both SQL refusals, foreign scope, invalid pending and cancellation. Tests NOT RUN; migrations NOT APPLIED. Complete closure producer, transport registration, activation and public coordinator remain open.

## V30 transactional creation admission — 2026-10-10

Added Service.AdmitPreparedCreate: tenant/editor transaction, current project authorization, authoritative pending row locked before proof/package admission, exact request hash, live TTL and incomplete-phase requirement. Errors roll back proof and package together via tenant.WithTenant. Completed public replay remains the future coordinator responsibility. Generic input registration remains scheduler-only; API requires a dedicated narrow registration path rather than widened generic grants. Added unit preflight and reverse-order multi-preset canonical-policy/pending-session-rotation regression cases. Tests NOT RUN; migrations NOT APPLIED. Full CreateDelivery remains partial, with closure producer, dedicated registration, sealing/activation and public coordinator open. Catalogue credit unchanged.

## V30 per-version preparation closure resolution — 2026-10-10

Added SQLCreateRecipeInputResolver using the existing v2draft scoped catalog and canonical object traversal for each version-bound FrozenRenderRecipe/WorkingImage pair. PreparePlan and assembly required roots now use FrozenRenderRecipe, matching SQL v26_render_recipe_scope; previous EditingRecipeV3 labels could not enter the concrete resolver. ResolvePrepareTask validates saved pending, resolves each item, requires both exact roots per result, merges identical shared entries, rejects UUID/SHA/contract conflicts and injected Params/transport, caps inventory and freezes owned task/profile. Service.ResolveCreatePrepare recovers authoritative pending before IO, reads objects outside transactions, then recovers/rechecks the unchanged pinned request and live selection after IO. Final SQL admission remains a required independent fence. Unit cases authored for scope, transitive leaf inclusion, duplicate merging, ownership, missing roots, collision, injected Params, resolver failure and cancellation. NOT RUN. No positive real SQL/object traversal or activation evidence; dedicated transport registration, initial recipe producer, graph activation and public coordinator remain open. No full-module credit.

## V30 dependency occurrence recheck — 2026-10-10

Concrete recipe input resolution now uses ResolveTaskInputEvidence and rechecks every scoped occurrence after document IO using its original LayerRef, including repeated references with distinct parent/frame contexts and binary leaves. The current object key/version/SHA/MIME/size must equal traversal metadata. Revocation, physical version changes, lookup errors and cancellation fail without returning a partial inventory. Existing resolver traversal/witness bounds remain authoritative; no arbitrary lower occurrence limit added. Unit cases authored for repeated occurrence scope, changed version, denial, cancellation and missing catalog. Tests NOT RUN. This does not establish an atomic multi-artifact snapshot or replace the final SQL registration fence; dedicated transport registration and activation remain open. No whole-module credit.

## V30 blocked creation staging coordinator — 2026-10-10

Added Service.StageCreatePrepare with concrete configured SQL/object-store adapters: recover existing authoritative pinned request, resolve each recipe closure with post-IO occurrence rechecks, compile exact owned documents, BeginRun in a short tenant transaction, PublishDocuments via independent reserve/object-write/complete steps, then MaterializePrepare in a new tenant transaction. Exact identities and saved pending support retry after partial publication. Public request preset slices are copied before asynchronous work, including in ResolveCreatePrepare. Task stays blocked; method does not complete public idempotency, seal or dispatch. Unit preflight cases authored for cancellation/invalid scope/unavailable dependencies. Tests NOT RUN; positive SQL/storage stage/retry/quota/race acceptance not established. Dedicated transport publication/registration, recipe selection producer, activation and public CreateDelivery coordinator remain open. No whole-module credit.

## V30 transport publication and inventory resource accounting — 2026-10-10

Added StageCreateTransport: validates configured scheduler document publisher, performs blocked staging, publishes exact frozen task chunks/manifest using existing scoped backend-document reservations, rechecks live pending/source authority after IO, returns owned frozen task plus published GraphTask. No generic API grants widened; trusted scheduler publisher must be supplied by application wiring. Does not register/seal/activate graph or complete public response. Fixed FreezeTask resource accounting from two direct roots to all unique indexed inventory refs including Params, charging shared duplicate entries once; existing SQL profile floors remain lower bounds. Unit cases authored for nil publisher/cancellation, transitive inventory increments and memory limit refusal. Tests NOT RUN; positive storage/SQL transport lifecycle unverified. Dedicated registration, initial recipe selection producer, activation and public coordinator remain open. No whole-module credit.

## V30 SQL full-inventory resource fence — 2026-10-10

Authored01296 wrapping the existing private prepare source gate while preserving its tenant-context/current-source checks. After the registered inventory exists, count all indexed task inputs including Params, calculate exact proof JSON bytes (Params+frozen+snapshot+each config), require the matching Go RAM/scratch floors, integer safe bounds, zero GPU/pixels and8MiB output. A pure private validator supports isolated owner-DB tests without media fixtures. Authored SQL cases for exact floor, RAM/scratch underflow, direct-root-only underestimate, GPU/output mismatch, invalid counts and document bound. Tests NOT RUN;01296 NOT APPLIED. Does not itself register/seal/activate tasks; dedicated registration, source selection producer and public coordinator remain open. No full-module credit.

## V30 blocked transport replay — 2026-10-10

Authored01297 replacing materialization replay comparison: accepts original unsealed profile, or exact frozen profile with only the three input-seal fields when v2_input_closure_ready and full registered-inventory resource floors hold. Still requires current pending/source documents, draft/unsealed run, blocked attempt0 task, exact operation/version/node/resource identity, Params/digest and live8MiB reservation. Prevents staging retry from failing merely because input sealing added transport metadata; does not accept activated workflows or waive quota expiry. SQL comparator cases authored for exact sealed profile, missing seal fields and changed backend/renderer/unknown fields. Tests NOT RUN;01297 NOT APPLIED. Full materialization/closure/quota retry fixture remains unverified. Dedicated initial registration, activation and public coordinator remain open; no whole-module credit.

## V30 frozen transport binding — 2026-10-10

Added FrozenPrepareTask.BindPublishedInputs: rebuild canonical chunks/manifest from the owned immutable cursor through a no-IO receipt verifier; validate task/count/entries digest/chunk+manifest hashes/exact direct roots and nonduplicated transport IDs. Never invokes externally supplied GraphTask.Open and returns owned receipt slices/direct roots with private frozen cursor. StageCreateTransport now binds published transport before returning. Unit cases authored for substituted task/count/digest/chunk/manifest/direct roots, duplicate transport ID, callback noninvocation, receipt ownership and cancellation. Tests NOT RUN. Physical ready receipts, complete current source authority and SQL registration/activation remain separate required gates; no whole-module credit.

## V30 scheduler preparation input registration — 2026-10-10

Authored01298 scheduler-only blocked transport receipt: lock draft run/task, compare exact task definition/Params/digest/frozen profile, verify closure readiness and exact manifest/count/SHA/chunk rows. Added FrozenPrepareTask.RegisterInputsInTransaction: verify run-derived task ID, bind published transport to owned cursor, skip exact ready replay, otherwise register/seal inputs and require receipt; caller rollback required. No graph activation or package/source authorization implied. Corrected shared sealed profile comparator to include and strip contracts_sha (existing sealing adds four fields); current digest acceptance remains v2_input_closure_ready responsibility. Corrected unapplied01296/01297 resource counts to exclude transport rows, matching Go indexed inventory. Unit ready-replay/run-binding/substitution/cancellation cases and SQL comparator missing/invalid contracts hash cases authored NOT RUN.01298 NOT APPLIED. First registration/rollback/concurrency positive SQL fixture unverified; API atomic final admission/activation and initial recipe producer remain open. No whole-module credit.

## V30 explicit recipe freeze batch — 2026-10-10

Added CreateRecipePin and CreateIntent.FreezeRecipeSelection using existing V26 RenderFreeze service. Validates the complete ordered unique-version batch before first callback, copies AssumeProfile pointers, derives stable run/ordinal/asset child keys, validates exact200/resource/document/draft revision/source binding and returns no partial selection after any error. Never chooses latest recipe; caller must supply server-authorized pinned drafts and durably store that ordered list before IO, then call ResolveSelection for snapshot authority. Unit cases authored for stable retries, wrong source/revision/status/resource, all-batch preflight and cancellation. Tests NOT RUN. Initial SQL draft pin capture and durable pre-IO selection phase are still open, so public CreateDelivery is not yet connected. Final API atomic admission/activation also open. No whole-module credit.

## V30 current draft selection capture — 2026-10-10

Authored01299 API-only capture of ordered current draft ID/revision and source bindings under retained snapshot/project/download/editor authority. Joins accepted current asset versions and ready verified private EditingRecipeV3 documents; locks selected metadata and requires full contiguous snapshot inventory. Current-source/nonapproved-draft restrictions mirror existing V26 fresh freeze capabilities; historical sources and approved-draft selection remain implementation gaps, not completed support. Added typed CaptureDraftSelection with strict bounded decoding, full scope and unique-version/revision/hash validation; observation must be durably pinned before IO. Unit cases authored for valid scope, null/empty/ordinal/revision/duplicate/source/unknown-field refusal and cancellation. Tests NOT RUN;01299 NOT APPLIED. Durable pre-IO selection ledger, historical/approved recipe producer, final API admission/activation and public coordinator remain open. No whole-module credit.

## V30 durable pre-IO recipe selection — 2026-10-10

Added private CreateSelectionPending and Service.PinCreateRecipeSelection using existing idempotency_records with internal route Delivery.Selection, same tenant/actor/key/request hash and24h TTL. Current retained snapshot/editor/download scope checked before ledger replay, row locked before capture, exact draft pins and preset catalog saved before object IO. Replay decodes saved evidence without rereading mutable drafts/presets. Strict16MiB codec binds actor/tenant/request/snapshot/count, ordered unique version pins, revision/profile validation and exact canonical config bytes through existing catalog validator. Unit codec cases authored for valid pin and altered actor/tenant/hash/snapshot/count/config/revision/ordinal/unknown/cancellation. Tests NOT RUN; positive DB race/restart/expiry fixture unverified. Transition from selection ledger to complete CreatePending still needs implementation; historical/approved draft support and final activation/public coordinator remain open. No whole-module credit.

## V30 selection-to-pending promotion — 2026-10-10

Added PrepareCreatePending: recover complete pending/public completion marker first; otherwise pin durable selection, freeze exact selected revisions through existing V26 service outside transactions, resolve returned recipe refs under current snapshot authority, then promote into complete document evidence. Private pinCreatePending branch locks original selection ledger, validates original source bindings and deterministic registered v2_draft_upload document identities, preserves frozen preset configurations instead of rereading mutable catalog, and retains existing final ResolveSelection checks. Legacy direct PinCreatePending behavior remains intact. Unit cases authored for saved catalog/scope, unregistered upload refusal and source substitution before upload lookup. Tests NOT RUN; positive SQL promotion/races/partial freeze restart unverified. Historical/approved draft support and final API atomic admission/graph activation/public coordinator remain open. No whole-module credit.

## V30 promotion freeze receipt binding — 2026-10-10

Promotion now requires a completed exact V26 Render.Freeze child ledger receipt under tenant/actor/asset/key, canonical pinned request hash,200 status, expected deterministic resource ID and live TTL. Strictly decodes saved freeze response and requires original draft ID/revision/source binding plus exact returned recipe before registered upload lookup. This closes the gap where deterministic upload identity alone did not prove the selected authored revision. Updated unit promotion cases for valid receipt and refusal before upload lookup on malformed receipt. Tests NOT RUN. Positive SQL/race/expiry promotion acceptance remains unverified; final API admission/activation and historical/approved drafts remain open. No whole-module credit.

## V30 preparation pipeline composition — 2026-10-10

Added CreateDeliveryDependencies and Service.PrepareDelivery: explicit separate API/scheduler pools and existing V26 freezer/object/document ports, owned request, durable selection+complete pending recovery, blocked creation+transport staging, tenant-local scheduler input registration transaction, then authorized API proof/package admission transaction. Returns internal PreparedCreateDelivery only after all phases; object IO remains outside transactions, partial durable draft evidence resumes on exact retry. No HTTP success/public JobTicket/idempotency completion or graph activation implied. Preflight unit cases authored for cancellation/invalid scope/unconfigured dependencies. Tests NOT RUN; positive multi-pool SQL/storage pipeline/restart/race verification still missing. Atomic activation/public replay, historical/approved draft support and production route/configuration remain open. No whole-module credit.

## V30 initial activation and service ticket — 2026-10-10

Authored01300 API-only activate_v30_delivery_create: lock primary idempotency ledger, derive exact run/workflow/task/package identities, current editor/download scope, validate single blocked draft graph, saved Params proof/current source/full input closure and building package, invoke existing generic seal/outbox checks, save202 queued JobTicket/resource package atomically. Authorized completed replay validates exact saved response before returning; no requalification/object IO on completed replay. Added typed ActivateCreate receipt validation and Service.CreateDelivery connecting preparation and final transaction. Unit cases authored for fresh/replay ticket identity, substituted run/workflow/state, missing replay marker/unknown/null receipt and foreign/cancel preflight. Tests NOT RUN;01300 NOT APPLIED. Positive SQL activation/rollback/restart/concurrency and multi-pool pipeline unverified; production HTTP route/configuration, historical/approved drafts, session-rotation proof recovery and full module acceptance remain open. No whole-module credit.

## V30 CreateDelivery HTTP route — 2026-10-10

Added POST /api/v1/tenants/{t}/delivery-packages using tenantRequest mutation session/CSRF path,16KiB strict JSON decode, shared idempotency key validation and owned CreateIntent validation. DeliveryCreator/DeliveryCreateService supplies concrete API service plus explicitly configured dependency ports. Handler validates202, deterministic package resource and exact queued run/workflow ticket, then emits replay marker when applicable. Default DB adapter allows completed replay but cannot prepare fresh requests without configured scheduler/freezer/storage dependencies; production wiring remains open and no broad runtime grants added. HTTP cases authored for fresh/replay, wrong ticket, missing key/policy, unknown/duplicate fields and missing session. Tests NOT RUN. Historical/approved sources, session proof recovery, full SQL/storage lifecycle and whole-module acceptance remain open. Catalogue status not promoted solely on route presence.

## V30 client creation API contract — 2026-10-10

Added DeliveryCreateRequest/DeliveryCreateTicket with strict runtime request field/policy/unique disjoint preset/key/UUID/Unicode codepoint length validation and owned arrays. OwnerAPI.createDelivery validates tenant/project/snapshot scope, forwards stable caller key and AbortSignal, checks cancellation before/after request and rejects nonexact queued ticket responses. Unit contract cases authored for ownership, Unicode names, duplicate presets, unknown fields, invalid keys/null arrays and malformed/extra/nonqueued tickets. Tests/typecheck/build NOT RUN. Creation UI/navigation and backend production dependencies remain open; historical/approved drafts, session proof recovery and full lifecycle acceptance remain open. No whole-module credit.


## Повторный пересчёт и клиентский поток создания delivery — 2026-10-10

Статически подтверждено наличие POST create/publish и concrete service: две прежние missing отметки переведены в partial. Реестр:9 missing,8 partial,73 требуют семантической сверки;81/90=90,0% лишь потолок наличия путей. Полный процент реализации неизвестен; новых полных зачётов нет. Добавлены экран/route создания, пагинация пресетов, frozen intent/key retry, очистка при потере доступа, отмена на смене scope, повтор загрузки и пять DOM unit-сценариев. Тесты, сборки, typecheck и миграции NOT RUN. Production wiring, upstream navigation, historical/approved sources, session rotation и полная приемка остаются открытыми.


## V30 editor-to-delivery navigation — 2026-10-10

Added EditorDelivery action to the scoped editor: resolves latest binding, refuses a different asset version, freezes one-photo render input snapshot with a stable request/key across lost-response retry, and opens /deliveries/new with tenant/project/snapshot. Aborts/ignores late results on scope/API changes. Four DOM unit cases authored (pin, exact retry, changed source refusal, scope cancellation), NOT RUN. This is a one-photo entry path; full batch-selection UX and production backend creation dependencies remain open. No whole-module credit; whole-product percent remains unknown, catalogue presence ceiling90%.


## V30 pre-activation session recovery — 2026-10-10

Authored01301 replacing save_v30_delivery_prepare_proof: current editor session/actor/project and ready Params required; differing saved proof can update only auth_session_id, only for draft unsealed run and blocked attempt0 task, followed by current source/closure validation with exception rollback on refusal. Exact same proof retains prior replay behavior. Added unit case proving session rotation preserves immutable Params/content while rebinding witness. NOT RUN; migration NOT APPLIED. Positive SQL session rotation, unauthorized actor, sealed/attempted refusal and rollback tests remain required. Production scheduler bridge remains open; no whole-module credit.


## V30 activation refusal and recovery role boundaries — 2026-10-10

ActivateCreate now maps only PostgreSQL23514 with exact DELIVERY_INITIAL_ACTIVATION_REFUSED message to DELIVERY_CREATE_STALE409; wrapped errors supported, unrelated integrity/connection causes preserved and no success receipt emitted. Three unit scenarios authored. Added isolated-database rollback-only prepare-proof role/foreign-tenant test for API versus worker/scheduler/gateway/portal. NOT RUN. This role test does not prove positive session rotation, immutable-proof substitution refusal or sealed-run recovery; those require full SQL fixtures. Production bridge and full module acceptance remain open.


## V30 durable client creation recovery — 2026-10-10

Added DeliveryCreateAttemptStore with strict owned request/ticket decode, origin/user/tenant/project/snapshot scope, serialized cross-tab lock, immutable intent and monotonic ticket, logout generation fencing and acknowledged-only removal. Creation route resolves current user before opening journal; screen restores intent/ticket, writes intent before network IO and saves ticket before navigation. Auth cleanup clears/fences delivery journals. Four unit cases authored for remount ownership, concurrent replacement/ticket regression, user isolation/logout stale writer and unacknowledged removal. Tests NOT RUN. Full UI persistence failure/reload matrix, batch selection and backend production bridge remain open; no whole-module credit.


## V30 journal recovery gate — 2026-10-10

Creation form now blocks selection/submission until durable journal read succeeds, allows explicit recovery reload on storage failure and remounts/aborts on API client or journal identity changes. Retry rereads authoritative local record; conflicting intent blocks sending and a previously saved ticket navigates without a new POST or ticket regression. Three DOM cases authored for failed recovery/noIO, restored exact request/key and stored acknowledgement/noPOST. NOT RUN. Full persistence and multi-tab UI matrix remains unverified; production bridge and whole V30 still open.


## V30 API production dependency wiring — 2026-10-10

Added buildDeliveryCreate and main.go injection: opt-in V30_DELIVERY_SCHEDULER_DATABASE_URL separate scheduler pool, max4 connections/30s statement timeout/15s startup bound, role and selected exact SECURITY DEFINER ABI/owner/EXECUTE checks. Uses existing API pool, V26 SQLRenderFreezeService, object store, artifactjson scheduler publisher and delivery creation pipeline. No grants/migrations at startup. Configured unavailable dependencies fail startup; absent variable preserves completed replay and refuses fresh work through default adapter. Added two unit config preflight cases and deploy instructions. NOT RUN. Startup checks are selected ABI checks, not full migration/runtime verification; positive multipool storage flow, history/approved drafts, race lifecycle and whole module remain open.


## V30 concurrent creation completion recovery — 2026-10-10

CreateDelivery uses a phase coordinator: initial authorized recovery, optional preparation, then transactional activation. A preparation error triggers one fresh authorized recovery; only a completed ledger permits final exact ticket replay, otherwise original preparation error or current authorization/recovery error is returned. Context cancellation prevents further recovery/activation. Unit matrix authored for fresh, completed, concurrent completion, incomplete storage failure, revoked recovery and cancellation. Tests NOT RUN. This is coordinator source behavior, not positive PostgreSQL concurrency proof. No retries of arbitrary activation errors and no fabricated ticket. Full multipool/object/race lifecycle acceptance remains open; no whole-module credit.


## V30 interchange archive byte verifier — 2026-10-10

Added worker-side verify_archive with NFC relative POSIX path/casefold uniqueness, regular-file/no encryption/stored-or-deflate restrictions, ZIP64 directory budget before eager parser allocation (64MiB central metadata), physical archive reservation, per-file/total expanded limits and100:1 ratio, bounded1MiB reads, CRC/actual byte count/SHA verification, cancellation and short-staging-write failure. Caller supplies staging sink and must discard whole batch on failure; returned inventory is not canonical ingest/approved recipe authority. Unit scenarios authored for paths, byte inventory/staging, collisions, symlink, bomb/quota, CRC/cancel and short writes. NOT RUN. Worker handler, manifest/XML mapping, staged output/adoption, canonical ingest/review/API remain open; interchange entrypoint not credited.


## V30 bundle manifest physical inventory — 2026-10-10

Added strict BundleManifest/Entry/Mask/Inventory models and verify_bundle_manifest: bounded strict JSON, depth32, original manifest byte/hash verification, unique physical NFC paths, exact inventory coverage/SHA/UUID identity, unique entry/external/image identities and exact image/recipe/mask references. manifest.json excluded from its own inventory and verified separately. Contract labels remain untrusted, not approved input authority. Unit cases authored for owned success, hash/path substitution, recipe pairing, duplicates, invalid crop, unknown fields, unlisted files and duplicate JSON keys. NOT RUN. Parse handler/staging registration, contract validation+UUID remap, XML support, canonical ingest and review/API remain open; no interchange entrypoint credit.


## V30 bundle parse staged file composition — 2026-10-10

Added ParsedFile/ParsedInterchange and stage_photoeditor_bundle using declared TaskArtifacts input, physical verifier+manifest gate, actual-expanded scratch reservation, generated local names, second complete byte verification before any upload, imported_file staging and gateway hash/identity checks. Output refs are gateway-issued; original archive UUIDs remain untrusted metadata. Scratch reservations release on failure/success; partial uploaded outputs remain attempt-staged for existing lifecycle handling, no adoption implied. Unit cases authored for valid staged inventory/new IDs/byte accounting, manifest failure before upload and uploaded SHA substitution refusal. NOT RUN. Worker operation/admission/dispatcher, staged output role SQL, canonical ingest/remap, XML and review/API remain open; no module credit.


## V30 namespace-exact XMP subset parser — 2026-10-10

Added bounded expat event parser: XML1MiB/depth32/nodes30000, DTD/entity/external resolution refusal independent of input encoding, duplicate property rejection, full namespace URI matching. Maps rating[-1,5], PhotoEditor pick, EXIF orientation and inverse orientation normalized crop; arbitrary-angle/incomplete/invalid crop and unknown/label fields explicitly unsupported. Unit cases authored for all8 inverse orientations, valid mappings, foreign namespace, duplicate property, DTD/external/budget refusal and invalid values. NOT RUN. XMP sidecar normalization into manifest, exporter, task admission/dispatcher and ingest/review/API remain open; no module credit.


## V30 deterministic XMP subset export — 2026-10-10

Added export_xmp and forward_orientation_crop: deterministic UTF8 namespace-exact sidecar, supported rating/pick/orientation only, canonical crop mapped into oriented normalized coordinates. Invalid/unsupported mappings reject rather than silently drop; absent fields stay absent. Unit roundtrip matrix authored for8 orientations, deterministic bytes, invalid enum/rating/orientation/nonfinite crop and unsupported mapping refusal. NOT RUN. TIFF pairing/normalized sidecar manifest, ZIP exporter, worker operation/admission/dispatcher and ingest/review/API still open; no whole module credit.


## V30 streaming deterministic bundle export — 2026-10-10

Added export_bundle: owned frozen metadata/inventory preflight before source access, path-sorted ZIP64 entries, fixed1980 timestamps, no UID/GID, stored images/deflate6 sidecars, bounded source reads with exact byte/SHA verification, archive-write quota including directory metadata, expansion/count/ratio limits and cancellation. Output must be caller-owned empty seekable scratch; any failure invalidates whole output before staging. Unit cases authored for deterministic bytes/metadata and verifier interoperability, truncated/hash/extra source substitution, quota/cancel and unlisted-source preflight. NOT RUN. Frozen export plan compiler, operation/admission/root report staging and API/review remain open; no module credit.


## V30 leased bundle parse operation adapter — 2026-10-10

Added strict import/parse Params and opt-in interchange_parse_operation factory. Adapter checks CPU/v2.2/op/version, Params digest, nonzero scope and declared bundle; requires explicit admission returning authoritative ArchiveLimits, intersects Params limits, runs photoeditor_bundle staging and repeats authority before/after root staging. Returns document+imported outputs and complete byte accounting; parse itself requests no human review. Unsupported XMP/captureone format fails explicitly pending normalization. Protocol unit cases authored with declared parse/admission doubles for profile/Params refusal, unsupported format, output/quotas and changed authority. NOT RUN. No production registration/SQL admission or whole-entrypoint credit; sidecar normalization, ingest/remap and review/API remain open.


## V30 parse SQL source gate — 2026-10-10

Authored01302 worker-only v30_interchange_parse_ready using existing live worker registration/lease/fence/resource slot/full closure checks, exact parse Params/backend document, saved project/session/bundle proof, current editor ACL/download scope, retained one-item ArtifactSnapshot and ready bundle. Requires admitted output/scratch/RAM/zeroVRAM capacity and returns bounded archive/expanded/files quotas. Added nullable backend-document proof column; API proof writer is intentionally still missing, so unproven tasks refuse. Worker database bridge and SQLParseAdmission decoder authored with malformed/foreign-shaped quotas and exact lease/Params unit cases. NOT RUN;01302 NOT APPLIED. Positive SQL tests, API proof writer/creation coordinator, dispatcher and ingest/review remain open; no production enablement or entrypoint credit.


## V30 API parse proof writer — 2026-10-10

Authored01303 API-only save_v30_interchange_parse_proof: current tenant/session/editor/download, creator-owned run, CPU parse task/ready exact Params, full input closure, exact retained artifact snapshot/bundle and output/scratch/RAM budgets. Proof replay exact; session-only rebind permitted only draft/unsealed blockedattempt0. Added Go interchange ParseParams compiler and transaction-local SaveParseProof checking Params hash and scoped identity; unit cases for exact SQL args, Params substitution beforeSQL, SQL refusal and cancellation. NOT RUN;01302/01303 NOT APPLIED. API create coordinator still must construct/publish/register run/task and invoke writer before seal. Positive SQL fixtures, dispatcher/ingest/remap/review and full V30 remain open.


## V30 CreateInterchange request and identity compiler — 2026-10-10

Added strict import/export tagged union decode: exact required fields/non-null/duplicate keys, nested ref canonical validation, supported format names and nonzero IDs/hash. FreezeCreateIntent owns pointer inputs, validates actor/session/key and derives tenant/actor/key-stable run/workflow/task/Params IDs; import snapshot derived, export retains requested snapshot. Canonical body/hash distinguish conflicts without creating another graph, session rotation keeps identity. Unit cases authored for union/unknown/null/duplicate/nested duplicate refusal, ownership, conflict hash/session and tenant scoping. NOT RUN. Compiler is not a creation endpoint: durable ledger/snapshot/object/task materialization/seal coordinator remains open.


## V30 frozen parse task/resource compiler — 2026-10-10

Added CompileParsePlan: exact authoritative BundleEvidence ref/size, tenant/actor/key identity, immutable Params bytes/ref/direct input inventory and CPU profile. Reserves expanded+8MiB output, archive+expanded+8MiB scratch and1GiB+2KiB/file RAM; zeroVRAM/pixels. Caller limits strictly bound requests; pure compiler does not confer artifact authority. DraftTask requires valid published input digest and returns detached profile. Unit scenarios authored for pinned scope/bytes, getter ownership, budgets, foreign evidence and empty-plan refusal. NOT RUN. Snapshot/ledger/materialization/publication/transport registration+seal coordinator still open; no module credit.


## V30 import artifact snapshot and pending codec — 2026-10-10

Added ParsePlan.CompileSnapshot one-item ArtifactSnapshot (InterchangeArchive,owner_id=null), deterministic snapshot document ID, canonical semantic digest excluding snapshot_digest and independent body SHA. Added bounded strict ParsePending encode/decode pinning tenant/actor/requestSHA, original BundleEvidence and resource limits; recompiles exact plan after session rotation, rejects unknown/missing/null/foreign/inconsistent evidence. New recovery fields use explicit snake_case wire names. Unit cases authored for digest/null-owner/ownership, session/identity/resource recovery and substituted/malformed records. NOT RUN. Durable ledger storage and actual snapshot/document registration still require creation coordinator/migrations; no module credit.


## V30 durable linked-bundle pending ledger — 2026-10-10

Added project-scoped ready ZIP resolver using server v2_artifact_links and exact tenant/ref/SHA/version/expiry. PinParsePending rechecks editor/download, locks actor/key internal ledger with24h TTL, validates request hash before source resolution, owns pending evidence and preserves original budgets on restart/concurrent exact retry. Current source bytes rechecked; updates require exactly1 row. Service wraps tenant transaction; no object IO or public ticket implied. Unit protocol cases authored for fresh/replay budget pinning, exact source scope, changed size, access/hash/expiry refusal before source read. NOT RUN. This source path accepts already project-linked archives; new desktop bundle upload authorization remains open, along with snapshot/run/document materialization and final coordinator.


## V30 owned parse input transport — 2026-10-10

Added ParsePlan.PublishInputs using exact sorted Params+InterchangeArchive inventory and existing BuildInputClosure canonical transport; validates publication receipts/SHA and root/chunk/direct ID collisions. ParseTransport owns inventory/Built evidence, produces task with published input digest and registers only owned cursor/receipts in caller scheduler TX with exact tenant/run scope. No public caller-supplied cursor, API proof, seal or dispatch. Unit cases authored for exact2 publications/task/digest, bad SHA/colliding IDs, cancellation and foreign/nil registration preflight. NOT RUN. Positive SQL registration/replay and creation shell/document coordinator remain open; no whole module credit.


## V30 parse draft run/snapshot shell — 2026-10-10

Pending codec now also pins exact tagged request and verifies its canonical hash on recovery; older incomplete records fail closed. Authored01304 API-only begin_v30_interchange_parse_run: lock original pending, reauthorize project and linked ZIP bytes, verify exact unsigned snapshot/semantic digest/config and deterministic IDs, create expiring one-item artifact snapshot plus draft/unsealed run/workflow. Replay requires exact existing draft identity/config. Snapshot root stays unattached until physical publication, avoiding immutable-retained root substitution. Added transaction-local Go BeginParseRun and unit scope/config/refusal cases. NOT RUN;01304 NOT APPLIED. Document reservation/publication/root attachment, blocked task/quota and final coordinator remain open; no module credit.


## V30 interchange immutable document publication — 2026-10-10

Authored01305 private exact document scope from pending request/limits and run config, with session/project ACL and deterministic IDs. Authored01306 API-only reservation/complete using existing quota/fence/versioned ready-artifact accounting; purpose allowlist extended only snapshot. Added detached Params/snapshot document compiler and SQL-backed storage publisher: owned canonical bytes, reserve before IO, immutable put/recover outside TX, exact object/ref validation, current authorized completion. Unit scenarios authored for normal/ambiguous/existing object publication, ready replay without IO and altered bytes refused before reservation. NOT RUN;01305/01306 NOT APPLIED. Snapshot root attachment/task materialization/quota/final coordinator remain open. No entrypoint credit.


### Пересчёт и продолжение 2026-10-10: snapshot attachment

Текущие 90 строк: 9 missing_runtime_path, 8 partial_runtime_path, 73 requires_semantic_review. 90% — потолок наличия путей, не процент реализации требований. Общая завершённость неизвестна; старые вложенные 87,8% исправлены по строкам.

Добавлены 01307 и ParsePlan.AttachSnapshot: текущая авторизация/pending, draft scope, точные root bytes/SHA/object version, атомарное закрепление snapshot. Unit cases написаны, не запущены. SQL не применён. Финальный coordinator и production parse installation остаются открыты. Нового зачёта целого модуля нет.


### V30: materialization задачи разбора

Добавлены 01308, ParseTransport.MaterializeTask и unit cases (не запущены). SQL вычисляет точный профиль CPU/RAM/scratch/output из pending, требует опубликованные Params и закреплённый snapshot, резервирует output storage и создаёт только blocked task. Replay допускает только ту же задачу и действующую квоту; transport seal fields проверяются через готовность closure. Миграция не применена. Координатор, HTTP и production installation остаются открыты. Процент полной реализации не установлен; новых зачтённых модулей нет.


### V30: PrepareParse coordinator

Service.PrepareParse соединяет pin pending, draft run, публикацию snapshot/Params, attachment и materialization в API-транзакции, публикацию/регистрацию transport через scheduler и сохранение API proof. Object IO вне SQL-транзакций. Digest вычисляется из отсортированного owned inventory до создания задачи; добавлены unit cases на совпадение с BuildInputClosure, владение срезами, отсутствие зависимостей и отмену. Не запускались. Финальная активация/JobTicket, HTTP, production installation и import adoption открыты; полного зачёта модуля нет.


### V30: final parse activation source

01309 и ParsePlan.ActivateParse добавляют финальный API-переход: текущая авторизация, сохранённый pending, blocked task/closure/quota, повторная проверка API proof, общий seal и отдельный ticket ledger в одной транзакции. Pending сохраняется для восстановления. Unit cases декодирования ticket/identity, отказа seal, DB cause и preflight написаны, не запущены. Миграция не применена. Совместимость всей цепочки seal ещё не подтверждена; HTTP/recovery/dispatcher/adoption открыты. Полного зачёта модуля нет.


### V30: recovery и create coordinator

RecoverParse читает private pending/ticket только после текущего project ACL, проверяет tenant/actor/body hash/TTL/resource/ticket identity и восстанавливает прежний plan. CreateParse выполняет recovery до подготовки и после неё; конкурентное завершение даёт replay только через актуальную авторизацию. Незавершённая запись сохраняет исходную ошибку подготовки; cancellation останавливает цепочку. Unit cases для переходов и ACL написаны, не запускались. HTTP, production runtime и adoption остаются открыты; общего процента завершённости эти файлы не доказывают.


### V30: HTTP create и bootstrap

POST interchange-jobs подключён к CreateParse через Server.InterchangeCreator, с session/mutation guard, 16KiB body limit, strict tagged union/key и проверкой ticket identity. Отдельный scheduler pool на V30_INTERCHANGE_SCHEDULER_DATABASE_URL, выбранные SQL install/role checks, CPU resource limits, main wiring. HTTP/config unit cases написаны, не запускались. Dispatcher/adoption, upload, export/GET/review и shared seal compatibility остаются открыты; полного зачёта V30 нет.


### V30: CPU parser runtime installation

WorkerConfig/from_env/build_worker/run_worker подключают ParseInstallation через PHOTO_INTERCHANGE_ENABLED (default false, strict true/false, CPU/backend guard). SQLParseAdmission использует worker lease authority. Конфликт env/custom installation отклоняется. Installation unit cases написаны, не запущены. Dedicated adoption/reconstruction/remapping/proposals пока открыты, сквозная готовность не заявляется.


### V30: independent parsed-result verifier

verify_parsed_interchange повторно скачивает и хеширует исходный ZIP в owned bounded scratch, проверяет physical manifest, сравнивает canonical result manifest, проверяет sorted paths/contracts/SHA/size и потоковые bytes каждого imported_file, требует exact staged output closure. UUID/contract labels остаются untrusted; canonical ingest не выполняется. Unit cases valid/corrupt/extra/altered/cancelled написаны, не запускались. В AdoptionCoordinator пока не подключено; SQL adoption/remapping/proposal lifecycle открыты.


### V30: bounded document remapping

BundleRemapper проверяет original JSON/hash, разрешает exact inventory refs, преобразует зависимости раньше родителей, заново вычисляет canonical SHA родителей, использует scope UUIDv5 и authoritative canonical identity mapping. Типы задаются server validators/binary allowlist; foreign authorities и learning блокируются. Unit cases written/not run. Компонент пока не устанавливает реальные recipe validators, не выполняет canonical ingest и не создаёт proposals; integration/adoption открыты.


### V30: typed recipe proposal compiler

compile_imported_recipe проверяет original SHA/EditingRecipeV3 schema, сохраняет parameter_delta и rebinding на canonical VersionBinding. Каждая dependency требует exact original/type/new ref и canonical source binding; learning блокируется. Missing layers/masks не отбрасываются. Unit cases written/not run. Реальный dependency resolver/ingest, SQL adoption/publication/review открыты; helper не считается готовым импортом.


### V30: existing canonical ingest bridge

CompileImportedIngest использует существующий CompileIngest/PrepareInspection, сохраняет ordinals и RAW/ICC/decode options, не угадывает assume_profile, формирует unsupported/oversize report. Проверяет exact original/source SHA, UUID, UTF8/NFC/safe paths/casefold collisions. Unit cases written/not run. Source evidence loader, DB run/snapshot linkage и orchestration после adoption ещё открыты; canonical asset_version этим планом не объявляется созданной.


### V30: opaque parse adoption inventory и Params gate

prepare_parsed_interchange_adoption строит exact root+imported file inventory/digest только после independent reconstruction. Archive contract labels не управляют typed traversal и не дают approval. Добавлена01310 scheduler-only Params getter с attempt/fence/result/current ACL/object checks. Positive inventory unit assertions written/not run; SQL not applied. Existing ingest_upload_bindings имеет обязательный upload FK: требуется dedicated imported-source binding и catalog integration, подмена артефакта upload session не допускается. Adoption commit/coordinator/ingest binding ещё открыты.


### V30: dedicated imported-source binding

01311 добавляет RLS immutable v30_interchange_ingest_bindings и scheduler-only bind_v30_interchange_ingest: current tenant/project/actor ACL, exact adopted parser root bytes/SHA, adopted imported file/project workflow link, ingest snapshot ordinal и deterministic asset/version/item identities. Go BindImportedIngest и preflight unit cases written/not run; SQL not applied. Legacy upload FK не меняется. Imported run construction, dedicated catalog commit/rejection/finish, adoption commit/coordinator и purge inclusion новой таблицы ещё открыты; end-to-end readiness не заявляется.


### V30: imported ingest run/snapshot и purge

01312 включает новую binding table в начало существующего explicit finish_tenant_deletion allowlist, сохраняя preconditions.01313 и ImportedIngestPlan.BeginRun создают ingest snapshot/run и все imported-source bindings атомарно: parsed root exact bytes/hash/adopted link, current ACL, SQL-derived supported image list, ordinals, ready source versions, stable identities. Unit cases written/not run; migrations not applied. Dedicated imported catalog commit/reject/finish и adoption integration открыты. Создание run само по себе не доказывает canonical ingest completion.


### V30: imported image catalog success

01314 создаёт independent outcomes и success commit с current actor/project ACL, exact WorkingImage SHA и producer inspect/decode output evidence. Original parse source становится provenance original без повторной копии/учёта bytes; asset version pending_review. Go ImportedCatalogAdopter повторно использует ReadDecodedImage.01315 добавляет outcomes перед bindings в purge. Unit cases written/not run, migrations not applied. Reject/finish, production coordinator/adoption ещё открыты.


### V30: imported reject/finish

01316 и ImportedCatalogAdopter.RejectFile/Finish реализуют полный интерфейс IngestCatalogWriter. Reject подтверждает exact source и failed/cancelled node; finish требует exact snapshot/bindings/outcome cardinality, terminal tasks, closed expansions, отсутствие pending review, matching provenance и однократное run.finished. Unit cases written/not run; migrations not applied. Processor branch, parse adoption commit/coordinator и proposal lifecycle ещё открыты; завершённость модуля не доказана.


### Пересчёт и imported catalog router — 2026-10-10

90 точек входа: 73 требуют семантического аудита, 10 частично реализованы, 7 отсутствуют. CreateInterchange и v2.interchange.parse.execute переклассифицированы по текущему API/runtime wiring. Присутствие точек входа: 83/90 = 92,22%; это верхняя граница каталога, не процент реализации всей системы. Полный процент реализации остаётся неизвестным.

01317 и IngestCatalogRouter подключены в workflow service: principal выбирается по SQL evidence; denied imported scope не переходит в legacy; catalog mutations выполняются в коротких tenant-транзакциях; imported ветка сохраняет batch validation и порядок settlement, пропускает повторное копирование original. Написаны unit tests порядка, invalid batch и partial failure. Тесты/сборка не запускались, миграция не применялась. Parse adoption commit, запуск imported ingest после adoption и proposal lifecycle остаются открыты.

### V30: fenced parse adoption preparation

prepare_current_interchange_parse использует scheduler-only 01310 до и после independent physical reconstruction. Проверяет exact operation/version/attempt/fence/result, требует autocommit reader, отказывает при revoked/missing/changed Params; cancellation останавливает чтение. Context фиксируется собственной копией. Unit cases valid/revoked/changed/initial-denied/transactional-reader/cancellation написаны, не запускались. Функция ещё не подключена к AdoptionCoordinator: dedicated atomic SQL adoption commit и recovery необходимы до подключения; принятие через generic adoption не допускается. Процент реализации не увеличен.

### V30: dedicated parse adoption commit — 01318

Общий adopt_v2_plan теперь отказывает для interchange.parse; отдельный adopt_v30_interchange_parsed проверяет current Params authority под tenant/run/task locks, exact Params/root SHA, current declared ZIP source, exact derived root+files inventory/purposes/ordinals, unique output IDs, producer attempt/fence и opaque output roles. Existing core adoption проверяет readiness/uploads/closure и атомарно создаёт project links/events; dedicated marker записывается в той же транзакции. Python commit statement сохраняет verified bytes и fencing; unit test написан, не запущен. Миграция не применялась. Production AdoptionCoordinator wiring, committed recovery и parent lifecycle barrier до canonical ingest остаются открыты. Presence ceiling остаётся 92,22%; whole-product completion неизвестен.

### V30: recovery, parent barrier и scheduler adoption wiring

01319 подтверждает historical commit только при exact task result/attempt/fence, обоих inbox markers, adoption event receipt/count, root link и полном output upload/project link составе. Python recovery строго принимает boolean и проверяет cancellation до/после SQL.01320 удерживает successful interchange run от generic завершения после parse; failure/cancel продолжает существующий finalizer chain. AdoptionCoordinator подключён к fenced parse preparation, bounded owned ScratchSpace, dedicated commit statement и recovery как при running context, так и stale context. Используется существующая scheduler scratch configuration; без неё fail-closed. Unit recovery cases написаны; тесты не запускались, миграции не применялись. Запуск canonical ingest после принятия и последующий review lifecycle ещё открыты. Presence ceiling не увеличен.

### V30: atomic parse → canonical ingest handoff — 01321

Новый dedicated wrapper атомарно принимает opaque ZIP outputs и вызывает existing begin_v30_interchange_ingest с independently prepared canonical snapshot. Если admission ingest отказал, исключение откатывает parse links/markers вместе с run. Raw/ICC defaults unchanged: assume_profile null, analyze false, tile512; исходные ordinals сохранены, unsupported/oversize не подменяются. All-unsupported допускается только при отсутствии поддерживаемого файла по SQL evidence и остаётся для report/review без пустого ingest run. AdoptionCoordinator устанавливает локальный tenant context в commit transaction; recovery теперь требует handoff marker. Низкоуровневый parse-only commit закрыт для scheduler. Snapshot unit cases и updated commit test написаны, не запускались; миграции не применялись. Mapping/typed recipe publication/import decisions/review lifecycle ещё открыты.

### V30: bounded mapping report publication

Добавлены strict MappingField/Entry/Shard/Report и publish_mapping_report: exact ordered source identity inventory, unique entry/external/version/proposal IDs, fail-closed publication SHA/UUID checks, immutable owned input copies, shards≤100 entries/max50, report counts по entries с rejected/unsupported fields. Publisher получает только canonical document bytes/contract/ordinal; task ownership и final adoption остаются обязательными у вызывающего контура. Approval не меняется. Unit cases 201 entries/order/counts, mismatch-before-IO, bad publication SHA, missing reason написаны, не запускались. Производственная import stage orchestration/typed dependency resolver/SQL report adoption/review ещё открыты; модуль не объявляется завершённым.

### V30: authoritative post-ingest sources — 01322

v30_interchange_import_sources проверяет текущий tenant/project/editor/session, exact committed parsed body, handoff receipt, все terminal ingest outcomes и matching versions/master artifacts. Возвращает полный ordered entries inventory с adopted/rejected/unsupported, без proposal/approval authority; при pending или stale возвращает NULL. Python strict loader сверяет parsed ref, exact entry cardinality/ordinals/identity и state shape. Unit cases terminal shape/pending/substitution-before-SQL написаны, не запускались; миграция не применялась. Loader предназначен для tenant-scoped scheduler SQL context; scheduling import task, real typed dependency resolution, draft publication/review остаются открыты.

### V30: per-entry import stage orchestration

prepare_import_stage связывает полный current source inventory с independent canonical WorkingImage hash/canvas/source/profile checks, metadata qualification port, typed compile_imported_recipe и bounded mapping publication. Rejected/unsupported ingest entries сохраняются в отчёте без доступа к recipe IO; отсутствующий recipe явно unsupported. Только UnsupportedDependency переводится в unsupported; IO/cancellation/publication errors не маскируются. Proposal publication требует exact deterministic scoped ref, не меняет approval. Unit terminal-source full-report case написан, не запускался. Реальные metadata/dependency/publisher installations и leased operation/scheduler stage ещё требуются; helper не засчитывается как завершённый runtime.

### V30: isolated draft collection — 01323

prepare_v30_interchange_draft_collection повторно требует current full post-ingest inventory и создаёт отдельную deterministic draft collection/new hidden pending_review items. Existing project assets/items не обновляются. Supported ratings0..5 и picked/rejected/unmarked → pick/reject/none переносятся; rating−1 не поддерживается локальным rating domain, исходное значение остаётся в immutable parsed metadata и должно быть unsupported в mapping. Повтор проверяет identities/count/metadata и не перезаписывает изменённые items. Go adapter проверяет bytes SHA, pending и deterministic collection ID; unit cases authored/not run. Migration not applied. Scheduler вызов этого adapter и квалификация metadata receipts в import stage ещё не подключены; crop recipe mapping, typed dependencies и review остаются открыты.

### V30: production draft collection processor — 01324

Scheduler discovery отбирает current authorized committed parse и terminal/no-child ingest, пропускает уже созданную deterministic collection. InterchangeCollectionProcessor читает version-pinned ParsedInterchange вне SQL TX (≤8MiB, exact bytes/SHA/JCS, bucket/cancellation checks), затем короткая tenant TX вызывает01323 и повторяет authority. Подключён отдельный bounded scheduler loop с readiness/shutdown/startup dependency checks. Concurrent schedulers опираются на idempotent tenant-serialized SQL, отдельная lease table не вводится. Unit valid/hash/size/bucket/cancelled/idle cases написаны, не запускались. Migration not applied. Metadata receipt qualification и leased import task generation/review ещё открыты.

### V30: qualified local metadata receipts — 01325

Scheduler-only v30_interchange_metadata_receipts повторно проверяет full source authority и exact new draft collection/items, возвращает actual persisted rating/flag/master/version. qualified_metadata_mapper требует exact scope, adopted coverage, deterministic collection/item IDs и binding; exact rating/pick допускается только при совпадении saved values. Rating−1 явно unsupported; ненулевой crop пока unsupported до qualified recipe transform, отсутствие crop exact. Receipt не approval и требует повторного SQL recheck при final adoption. Unit rating0/3/5/−1, changed pick и foreign receipt cases написаны, не запускались; migration not applied. Передача receipts в frozen import task, crop transform, dependency installations и review ещё открыты.

### V30: metadata receipt enforcement in import stage

prepare_import_stage больше не принимает произвольный metadata callback: для adopted images обязательны MetadataReceipts, validated qualified_metadata_mapper подключается внутри стадии до object IO. Mapper владеет копиями receipts и sources. Added positive canonical WorkingImage → mapping test и missing-receipts-before-IO case; tests not run. Non-null crop пока explicit unsupported: отдельного crop поля в EditingRecipeV3 нет, нужен квалифицированный geometry/dependency путь; exact не заявлен. Leased import operation/task scheduling и final report adoption/review ещё открыты.

### V30: typed dependency resolver adapter

BundleRecipeDependencyResolver подключает реальные StyleV2/OverrideV1/CorrectionPlan/RenderLayerStack/MaskRef validators к BundleRemapper, требует явные qualification ports для canonical bindings/binary/runtime docs, запрещает override built-in validators, проверяет requested root contract/SHA и source binding. VersionBinding перепривязывается целиком через qualified rewriter; бинарный contract не даёт authority без qualifier. Qualification выполняется до dependency-first publication, deterministic refs проверяются; repeat не публикует повторно и не выдаёт cached root для foreign binding. Unit CorrectionPlan values/rebind/cache/foreign-binding/type-before-IO cases authored/not run. Deployment-specific real binding/binary/model qualification ports и leased import worker installation остаются открыты, standalone adapter не считается готовым импортом.

### V30: canonical source binding qualification

CanonicalImportBindings связывает old VersionBinding только через physically verified recipe/WorkingImage inventory и exact original encoded SHA/canvas hash; target WorkingImage берётся исключительно из canonical ingest, проверяются hash/JCS/source/profile/float32. Старые пиксели не становятся canonical authority. Полное original binding identity требуется при lookup, неоднозначный old asset_version_id блокируется. Owned byte receipts возвращают новые copies. Negative unit absent-recipe/source-substitution/cancellation cases written/not run; positive full recipe+image evidence и production installation ещё требуются. Typed binary/model qualification и import worker/review остаются открыты.

### V30: physical imported MaskArray qualification

ImportedMaskQualifier связывает exact manifest mask/path/SHA с canonical canvas, повторно хеширует declared opaque input в owned bounded scratch и использует existing runtime.read_array для exact NPY header/EOF, float32, shape и finite[0,1]. Нет unpickle/dtype conversion/resampling; cache хранит immutable receipt, не mmap. Contract label не даёт authority без canvas и physical verification. Unit valid/cache/wrong-shape/dtype/NaN/range cases authored/not run. Deployment qualification wiring/import worker и final review остаются открыты; не засчитано как завершённый runtime.

### V30: per-entry unsupported isolation and root binding gate

CanonicalImportBindings теперь сохраняет per-entry semantic failures вместо abort всего inventory; ambiguous old identity отзывается для всех затронутых entries и не восстанавливается случайным поздним кандидатом. Physical SHA/IO/cancellation и invalid canonical ingest document остаются fatal. prepare_import_stage требует qualification original recipe.source и exact target binding перед compile, включая recipes без source-bound dependencies. Unit unsupported recipe record vs storage/hash fatal cases added/not run. Production import worker/task/review ещё открыты.

### V30: completed import result root packaging

InterchangeResult strict model и publish_import_result формируют import root bundle=null только из completed MappingReport/shards/proposals; exact report bytes/count/shards, shard SHA и no alias проверяются. imported_files сохраняет полный уникальный parsed.files closure, включая opaque dependencies без прямого report ref. Enforced root8MiB/output5052 cap, root publisher SHA/UUID проверяется. Unit full closure/null bundle/no alias case written/not run. Typed dependency documents должны быть server-prepared declared inputs: worker output roles разрешают только EditingRecipeV3 draft, не произвольный Style/Mask документ. Server dependency publication, frozen task inputs и leased import worker/adoption/review ещё открыты.

### V30: GetInterchange API — 01326

GET /api/v1/tenants/{t}/interchange-jobs/{id} подключён к viewer-scoped tenant transaction и SQL current tenant/project ACL. Resource projection покрывает установленную photoeditor_bundle import ветку, скрывает draft unsealed jobs, не раскрывает bucket/key/session. Mapping/report/bundle pointers пока null: финальная report adoption/persistence ещё не реализована; состояние run читается существующим Run API по ticket.run_id, RowInterchangeJobs не расширялся неописанным state. Unit read scope/null/malformed/owned-bytes cases written/not run; migration not applied. GetInterchange переклассифицирован missing→partial; это наличие пути, не полная готовность чтения результата.

### V30: job inspection and client screen — 01327

Viewer-scoped inspection API возвращает actual parent run/status/revision, ingest run и draft collection, stage отражает graph state без выдуманного процента. OwnerAPI и strict scope/state decoders подключены. Новый route /interchange/[id] отображает loading/error/pending/terminal, обновляет nonterminal status каждые5s, прекращает запросы/таймеры при unmount/scope change и не сохраняет stale data при denied errors. Run/collection links используют server inspection IDs. Read decoder/scope/cancellation unit tests authored/not run; UI DOM cycle ещё не выполнен. Migration not applied. Screen зарегистрирован файловым маршрутом, входная навигация из create flow и review/report actions ещё открыты.

### V30: interchange screen unit lifecycle coverage

Добавлены DOM unit cases loading→poll→terminal, server-bound run navigation,403 clears data/stops polling, scope navigation abort+late response isolation, unmount timer cleanup и explicit retry после transient error. Error branch теперь отменяет оба parallel GET через общий controller и очищает timer, не оставляя sibling request после отказа. Все тесты authored/not run по текущему implementation-first режиму; browser/runtime verification не выполнены. Новый экран включён в охват unit lifecycle, полного module PASS нет. Import worker/report adoption/review остаются открыты.

### Пересчёт 2026-10-10 и согласованное чтение V30

90 catalogue entrypoints: 73 требуют semantic review, 11 частичные, 6 отсутствуют. Наличие путей: 84/90 = 93,33%; это верхняя граница наличия, не процент реализации требований. Исправлены устаревшие вложенные значения 90%. Whole-product implementation percent остаётся неизвестным: полный requirement-level denominator и current semantic evidence не закрыты.

OwnerAPI.interchangeSnapshot ограниченно повторяет чтение двух проекций при разных revision (до трёх пар запросов), не передаёт смешанные данные экрану и учитывает abort между попытками. Добавлены unit cases восстановления после revision race и исчерпания лимита; исправлены типы options в DOM mocks. Тесты/сборка/typecheck не запускались; миграции не применялись. Полный модуль не засчитан.

### V30: current import metadata loader

Добавлен load_import_metadata: внутри caller-owned tenant transaction повторно получает full post-ingest sources и persisted collection metadata через SQL01325, проверяет complete coverage и deterministic collection/item/version/master identities. Pending/denied возвращает None, отмена и ошибки не превращаются в пустой успешный результат. Object IO и управление транзакцией отсутствуют. Unit scope/ownership/pending/substitution/cancellation cases authored, NOT RUN. Loader ещё не установлен в frozen import task compiler; модуль V30 не завершён, процент реализации требований не повышен.

### V30: normative InterchangePlan and Params decoder

Добавлены strict InterchangeEntry/InterchangePlan/import-export Params по schemas.json, bounded duplicate-safe JSON decoding, depth32, scoped project/direction/snapshot digest, nonzero identities и unique entry/external/casefold paths. Unit ownership/collision/scope/path/duplicate-key cases authored, NOT RUN. Схема не расширялась: entries min1 с обязательными binding/recipe остаётся несовместимой с all-unsupported import и требует явного решения в compiler contract; пустой/фиктивный recipe не создаётся. Production frozen task/compiler/worker/adoption/review остаются открыты.

### V30: terminal import plan contract resolved

Согласованы schemas.json и Python InterchangePlan: import entries допускают nullable binding/recipe, recipe требует binding; export требует оба. Entries сохраняются полностью, all-unsupported не становится пустым планом и не получает fake references. Нормативно определены current ImportSources evidence, report-only stage и final adoption recheck. Unit terminal/no-recipe/export constraint cases authored, NOT RUN. Frozen task compiler и runtime wiring ещё не реализованы; весь модуль не засчитан.

### V30: import plan compiler

compile_import_plan формирует полный ordered plan из exact ParsedInterchange/current ImportSources и server-prepared proposal receipts. Canonical WorkingImage повторно проверяется по actual bytes SHA/JCS/original content SHA/float32/work profile; terminal entries не выполняют object IO. Проверяются ordinal/entry identity, original staged file, proposal body hash/source binding, foreign proposal keys и plan8MiB cap. Unit mixed/all-terminal/master substitution/foreign proposal/reordering cases authored, NOT RUN. Compiler ещё не подключён к task materialization, prepared dependencies publication и final adoption; whole-module credit отсутствует.

### V30: frozen plan/Params publication

Добавлены immutable byte receipts ImportTaskDocuments, deterministic workflow→task→plan/Params UUIDs и exact plan SHA в Params. Plan публикуется до Params с current-source fence до/после каждого IO; mismatch/cancellation/revocation не допускают продолжения. Publisher port обязан обеспечивать reserve/complete/exact replay ownership. Unit order/replay/receipt substitution/revocation/strictbool cases authored, NOT RUN. Production publisher adapter, full closure resolution/reservation, scheduler task materialization и import worker/adoption/review ещё открыты. Это не runnable task и не full-module credit.

### V30: SQL import document authorization scope — 01328

Добавлен отдельный private SECURITY DEFINER scope для import Plan/Params после canonical ingest (parent sealed running). Он повторяет current full sources + collection metadata ACL, сверяет полный ordered original состав/metadata, adopted version/master, terminal null shape, snapshot digest и deterministic document identities. Params допускаются только после ready Plan с verified artifact/version/SHA/bytes. Это storage authorization, не доказательство typed recipe closure/canvas qualification или approval. Reserve/complete wrapper и publisher adapter пока отсутствуют; migration NOT APPLIED, SQL tests NOT RUN.

### V30: import document reserve/complete — 01329

Добавлены scheduler-only reserve/complete поверх private SQL01328 scope. Используют существующий backend_documents lifecycle, tenant storage quota/reserved accounting, exact replay/conflict, token rotation для pending и version/token/body fence для complete. Parent должен оставаться sealed running; ACL/current sources повторно проверяются на каждом этапе. Tenant lock берётся до shared source locks, quota не списывается повторно при replay. Plan≤8MiB, Params≤64KiB. Migration NOT APPLIED; SQL/runtime tests NOT RUN. Publisher transport, task closure/materialization и worker/adoption/review остаются открыты; no module completion credit.

### V30: Go import storage publisher and SQL adapter

ImportStoragePublisher и SQLImportDocumentStore подключают reserve/complete01329 с короткими TenantTransaction; immutable Put/Recover выполняется вне SQL transaction. Проверяются deterministic workflow/task document IDs, actual canonical body/SHA/size, run ownership, exact reservation/ready receipt, object key/version/MIME/bytes/SHA и complete receipt. Recover после exists/unavailable, готовый replay без object IO, cancellation не вызывает complete. Unit publication/recovery/replay/identity/object substitution/cancellation cases authored, NOT RUN. Caller orchestration из compiler и scheduler task materialization ещё открыты; миграции не применены, full module credit отсутствует.

### V30: Go plan→Params publication orchestration

FreezeImportDocuments фиксирует owned canonical Plan bytes, deterministic task/plan/Params IDs и Params с exact parsed/plan refs; запрещает alias и malformed SHA. ImportPreparedDocuments.Publish использует существующий ImportStoragePublisher: preflight обоих документов, Plan complete перед Params reserve, abort/error/receipt mismatch останавливает цепочку. Documents() отдаёт copies. Это byte/publication orchestration, не typed compiler/closure admission: production SQL01328 выполняет текущую scope проверку, final materialization должна повторить closure/current evidence. Unit order/ownership/replay/failure/cancellation cases authored, NOT RUN; migration NOT APPLIED. Runnable scheduler task/import worker/final adoption/review ещё открыты; completion percent не повышен.

### V30: blocked import task materialization — 01330

Добавлена scheduler-only materialize_v30_interchange_import: tenant/run lock, repeated current source/Plan/Params scope, ready document evidence, committed parse predecessor, exact deterministic task shell/profile/input digest и atomic output reservation increment. Task остаётся blocked; closure registration/activation отсутствуют и не имитируются. Existing parent finalization barrier сохраняется. Replay не повторяет quota increment. Reserve учитывает report/root/shard caps и exact prepared recipe bytes. Migration NOT APPLIED; SQL tests NOT RUN. Dedicated late continuation appends blocked node under locked sealed running parent, without ready dispatch or graph expansion lease; это требует отдельного activation contract и полного integration coverage. Compiler orchestration/closure/worker/adoption/review остаются открыты.

### V30: Go blocked-task coordinator and SQL adapter

PrepareImportTask теперь связывает preflight exact task identity/params/digest/CPU profile, Plan→Params publication и SQLImportTaskMaterializer. SQL adapter сверяет parsed bytes SHA с frozen Params, workflow→run scope и вызывает01330 в короткой TenantTransaction. Foreign task и publication failure не создают shell. Unit order/foreign identity/failure cases authored, NOT RUN. Call-site production loop, full transport closure registration/activation, import operation/adoption/review ещё отсутствуют; migration NOT APPLIED, overall completion не повышен.

### V30: input inventory transport and registration

Добавлены FreezeImportInputs/ImportInputInventory/ImportTransport через existing v2input BuildInputClosure/RegisterInTransaction. Owned sorted inventory, exact Params/Plan/Parsed root contract/SHA membership, duplicate identity rejection, stable entry digest, manifest/chunk alias checks и scope-bound registration. Root membership не доказывает recursive closure: authoritative typed resolver должен передать полный состав (production call-site ещё отсутствует). Unit owned inventory/roots/type/hash/duplicate/publication alias cases authored, NOT RUN. Publication предполагает уже существующий blocked shell; final activation/worker/adoption/review всё ещё открыты. Миграции не применены, completion не повышен.

### V30: bounded recursive closure traversal / Params correction

Исправлен root transport contract конкретной Params schema на общий Params, как требует existing V2 protocol. ResolveImportInputInventory обходит scoped typed resolver nodes, проверяет exact document SHA/JCS, actual ArtifactRef set vs declared children, повторные identity/type/hash конфликты, cycles/depth32/count1M и отмену. Parsed manifest inline inventory IDs не считаются ссылками; их semantics остаются обязанностью typed parser/resolver. Binary leaf byte/MIME/provenance qualification обязан выполнять scoped resolver; production implementation этого порта ещё не подключена. Unit full roots/omitted dependency/physical substitution/cancellation cases authored, NOT RUN. Activation/worker/adoption/review ещё открыты, whole-module credit отсутствует.

### V30: independent root verification before closure resolution

Закрыт обход через nil Body: Params/Plan/ParsedInterchange не могут быть объявлены binary leaves. Local frozen Plan/Params preflight выполняется до resolver IO, returned root bytes должны точно совпадать с local receipt; nested identity/hash/contract format проверяется до resolver call. Unit disguised binary roots и malformed local root/no-IO cases authored, NOT RUN. Scoped typed/binary qualification implementation, production orchestration/activation/worker/adoption/review ещё открыты. Completion percent не повышен.

### V30: physical JSON dependency resolver

ImportJSONNodeResolver выполняет bounded actual object read≤8MiB, exact length/SHA/JCS/MIME/version checks, typed children validation и repeated current catalog lookup after IO. Validator получает copy bytes; invalid/binary/substituted documents не проходят typed stage. Unit read/ownership/physical substitution/version change/binary refusal cases authored, NOT RUN. Scoped SQL catalog и concrete per-contract validator/binary qualifier ещё должны быть установлены, production integration/activation/worker/adoption/review остаются открыты. Процент реализации не повышен.

### V30: scoped root/master catalog — 01331

SQLImportDocumentCatalog подключён к scheduler-only SQL01331: current full sources + collection metadata ACL, exact Params/Plan owner/document identities, exact parsed ref или adopted canonical master. Возвращает только ready verified JSON objects с version/expiry/size; Go проверяет configured bucket и scope/cancellation. Manifest labels не авторизуют foreign typed recipe dependencies. Unit incomplete-scope/cancellation cases authored, NOT RUN; migration NOT APPLIED. Catalog пока покрывает roots/canonical masters; remapped dependencies/binary qualification и concrete validator/runtime orchestration/activation/worker/adoption/review ещё открыты. Full-module credit отсутствует.

### V30: malformed ArtifactRef closure coverage

Reference discovery теперь отклоняет partial/extra-field ArtifactRef вместо молчаливого исключения из closure. Единственное исключение: exact four-field inline archive inventory entry в ParsedInterchange.manifest.inventory[index]; другой contract/path не получает это исключение. Unit malformed/partial/misplaced/foreign-contract cases authored, NOT RUN. Это структурная полнота ссылок, не замена typed document schema validation; concrete validators/runtime остаются открыты. Completion не повышен.

Inline ParsedInterchange.manifest.masks[index] (path/SHA/canvas_hash) также сохраняется как metadata evidence, без read authority; exact path/shape исключение и unit case добавлены, NOT RUN.

### V30: concrete typed Plan/Params validator

ImportPlanDocumentValidator реализует strict typed Go decode + exact canonical re-encoding (unknown/missing required fields, включая nullable, не теряются), Params literal/ref checks, authoritative project/snapshot scope, full entry bounds/identity/path NFC/casefold uniqueness/metadata/normalized crop/binding→recipe rule. Возвращает actual child refs с typed contracts и conflict checks. Unit terminal plan/scope/duplicate/path/recipe-without-binding/missing nullable/Params roots cases authored, NOT RUN. ParsedInterchange/WorkingImage/recipe validators и binary dependencies всё ещё требуют concrete installation; нет утверждения полной schema parity/runtime/module PASS.

### V30: concrete ParsedInterchange validator

Typed Go ParsedInterchange/BundleManifest validator сверяет полный staged files↔inventory count/path/SHA/contract, unique staged IDs, original inventory IDs/NFC/casefold paths, entry/image/optional recipe/mask links, metadata bounds/crop и exact required fields. Все parse files выдаются как Artifact opaque inputs: untrusted contract labels не дают typed approval. Подключён к ImportPlanDocumentValidator dispatch. Unit full opaque closure/hash/count/type/entry substitution cases authored, NOT RUN. WorkingImage/recipe validators, opaque binary catalog/qualification, production activation/worker/adoption/review остаются открыты. Completion не повышен.

### V30: existing WorkingImage contract integrated

ImportScopedDocumentValidator связывает owned typed Plan/Parsed entry order/content/source evidence, проверяет canonical WorkingImage через existing review.ParseWorkingImage и strict typed roundtrip. Требует exact source ref/SHA/canvas/original encoded SHA, float32/work_rec2020_gamma24_v1/gamma24; пиксели/alpha получают existing WorkingPixels/WorkingAlpha роли. Все остальные chain refs сохраняются как opaque Artifact до отдельной qualification, не теряются. Unit existing Python image fixture/binary roles/foreign original/canvas/uint16 refusal cases authored, NOT RUN. SQL catalog для binary leaves/remapped recipe dependencies и runtime activation/worker/adoption/review ещё открыты; full module credit отсутствует.

### V30: scoped opaque files and streaming physical verification — 01332

SQLImportOpaqueCatalog/SQL01332 разрешают exact parsed.files content membership после current sources/metadata ACL. ImportOpaqueNodeResolver хеширует stream с256KiB buffer, exact length/EOF/quota bound, cancellation и repeated version/scope lookup; не загружает весь файл в RAM. JSON/opaque router сохраняет разные qualification paths; Artifact не превращается в MaskPixels/WorkingPixels по archive label. Unit valid/hash/short/extra/limit/version/typed refusal cases authored, NOT RUN. Migration NOT APPLIED. Canonical pixel/alpha evidence, recipe dependencies и production activation/worker/adoption/review остаются открыты; completion не повышен.

### V30: canonical pixel provenance witnesses — 01333

После validated WorkingImage сохраняются owned exact master bytes + child role receipts в synchronized ImportWorkingWitnesses; lookup возвращает copies, conflict не перезаписывается. SQL01333 повторяет current sources/metadata ACL, exact master bytes SHA/adopted ref, exact pixels/alpha ref и matching canonical ingest producer task/attempt/fence/output_role. Manifest inventory не даёт такой authority. Unit witness ownership/role substitution/conflict cases authored, NOT RUN; migration NOT APPLIED. Go pixel catalog/stream integration ещё не подключены; recipe dependencies/activation/worker/adoption/review остаются открыты. Whole-module credit отсутствует.

### V30: canonical pixel Go catalog and resolver installation

SQLImportPixelCatalog использует owned verified master witness и SQL01333, проверяет configured bucket/version/size/SHA. Canonical pixel reader сохраняет WorkingPixels/WorkingAlpha роли, повторно потоково проверяет actual bytes и current scope. Общий streaming engine переиспользован без разрешения typed roles в opaque reader. NewSQLImportResolver связывает concrete JSON/opaque/pixel catalogs, validators и общий witness cache; это constructor, production processing loop ещё не вызывает его. Unit pixel roles/opaque refusal/missing installation cases authored, NOT RUN. Recipe dependency installation, activation/worker/adoption/review ещё открыты; migration NOT APPLIED, no whole-module credit.

### V30: transport replay correction and sealed-graph registration gap

Static inspection of existing foundation register_v2_task_input_batch/seal_v2_task_inputs confirms both require unsealed run. Current ImportTransport.RegisterInputs delegates generic registration against sealed parent and therefore cannot succeed in production. Do not count its existence as integration completion. Next required change: dedicated fenced import registration/seal transaction that temporarily changes graph state only while holding parent lock, restores sealed state, repeats current sources and never activates task.

Unapplied01330 replay check corrected: accepts only the exact three transport augmentation fields with v2_input_closure_ready(task), preserving base profile equality; reserved quota is not incremented again. No migration applied, no test/build/validation executed. Activation/worker/adoption/review and production orchestration remain open.

### V30: dedicated sealed-parent input registration — 01334

Добавлены scheduler-only register/seal wrappers с current transport scope, tenant/run/task locks, temporary graph unseal только внутри atomic SQL call, restore и повторным current-source check. Ошибки пробрасываются и откатывают call; Go caller обязан откатить весь input registration TX. Exact completed-closure replay проверяет rows/manifest/chunks/direct roots без повторной записи. v2input shared registration получил fixed dedicated dialect, ImportTransport требует exact parsed body/SHA и использует его. Replay base profile учитывает также contracts_sha, добавляемый existing seal. Migration NOT APPLIED, tests/build/SQL NOT RUN. Positive integration tests и transport document publisher availability под sealed parent ещё требуют проверки; final activation/worker/adoption/review не завершены.


## Продолжение: границы регистрации import inputs

В ещё не применённой 01334 добавлен явный запрет выхода batch за миллион записей, включая replay. Наличие путей не даёт полного зачёта модулю; активация import и полный review/export остаются открытыми. Тесты и сборка не запускались, миграции не применялись.


## V30: последовательность подготовки import execution

Добавлен ImportExecutionPreparation.Prepare: preflight parsed/task, публикация Plan/Params, scoped физическое разрешение полного closure, проверка input digest, blocked materialization, публикация transport и атомарная регистрация batch+seal через SQLImportInputRegistrar. Object IO не выполняется внутри регистрационной транзакции. Ошибки останавливают последующие стадии; успешный возврат не активирует задачу и не означает accepted job. Unit-тесты порядка, отказов и cancellation написаны, NOT RUN. Production scheduler loop и recipe dependency installation ещё не подключены; полного зачёта V30 нет.


## V30: typed current import sources в Go

Добавлен SQLImportSourceReader: короткая tenant transaction читает 01322, NULL означает stale/pending; immutable snapshot сохраняет terminal reason/state. Проверяются parsed SHA, project/workflow, полный ordinal/entry состав, adopted/terminal nullable shape и deterministic canonical ingest run. Snapshot не является полномочием, текущая authority проверяется позже повторно. Scoped validator теперь также требует точное сохранение rating/pick/crop из ParsedInterchange. Unit-тесты ownership, scope/state/ingest substitution, cancellation и metadata preservation написаны, NOT RUN. Scheduler/compiler installation, recipe qualification и activation ещё открыты; зачёт полного V30 отсутствует.


## V30: Go import plan compiler

Добавлен CompileImportPlan: повторная typed проверка owned SQL source snapshot, полный ordinal manifest, terminal entries без IO, adopted WorkingImage SHA/JCS/typed contract/original source/float32/color и canonical canvas hash. Non-null original recipe требует явного квалифицирующего resolver; отсутствие resolver запрещено, рецепты молча не отбрасываются. CompiledImportPlan сохраняет terminal evidence отдельно и создаёт deterministic Plan/Params. Unit-тесты terminal preservation, ownership, foreign workflow, adopted binding, hash/cancellation написаны, NOT RUN. Production master reader, recipe resolver и scheduler wiring остаются открытыми; полного зачёта нет.


## V30: production master reader и plan preparation

Добавлены ImportObjectMasterReader/NewSQLImportMasterReader: scoped WorkingImage catalog, bounded immutable read до 1MiB, length/SHA/JCS/typed verification, close errors и post-IO current object recheck. ImportPlanPreparation/NewSQLImportPlanPreparation связывают SQL sources и master reader с компилятором и повторно сверяют полный current sources snapshot после object IO. Нет общей DB транзакции вокруг IO. Unit-тесты bytes/version/MIME/budget/typed/cancellation и current-source refusal написаны, NOT RUN. Recipe resolver, scheduler loop и activation остаются открытыми; полного зачёта V30 нет.


## V30: worker stage для frozen prepared recipes

Добавлен prepare_frozen_import_stage/output: exact Plan/Parsed SHA/JCS/scope/order/metadata и current-source binding, terminal reasons, actual metadata receipts, canonical master и ready recipe как declared input. Никакого повторного remap/publish recipe из архива на worker. Non-null recipe требует explicit full dependency qualifier с exact True; absent recipe при наличии оригинала требует frozen compiler diagnostic. Report/root публикуются только после полной подготовки. Unit-тесты mixed/all-terminal, preflight mismatches, prepared recipe qualifier refusal и propagation написаны, NOT RUN. Persisted diagnostics/sources transport, concrete qualifier и runtime/adoption installation ещё открыты; полный V30 не завершён.


## V30: declared prepared recipe closure verifier

Добавлен PreparedRecipeClosureQualifier для порта frozen-stage: owned scheduler-authenticated inventory, SHA-pinned schemas, strict immediate dependency contracts/MIME, actual JSON SHA/JCS/schema, schema-proven recursive ArtifactRefs, cycle/depth/ref/aggregate-byte budgets и exact-True semantic/binary qualification. Каждый непосредственный dependency проверяется независимо от schema traversal; повторные refs читаются один раз внутри одного binding. Unit-тесты полного обхода, missing/type/hash/semantic/truthy отказов, schema pin и duplicate inventory написаны, NOT RUN. Installed semantic/binary ports и runtime wiring ещё не добавлены; структурная проверка сама по себе не квалифицирует операторы или NPY. Полного зачёта V30 нет.


## V30: concrete prepared document semantics

Добавлен PreparedRecipeDocumentQualifier: owned canonical binding/image inventory и установленный work ICC SHA; typed exact-field validation EditingRecipeV3/WorkingImage/StyleV2/Override/MaskRef/LayerStack/CorrectionPlan. Проверяются source, learning prohibition, original SHA и inline mask geometry через existing render override validator, mask canvas+transform chain, layer source, full correction canonical identities/ordinals/target. Runtime/operator qualification остаётся mandatory exact True; extra operator validators не могут заменить builtins. Unit-тесты ownership/source/mask/binding/runtime/ICC/validator override написаны, NOT RUN. Binary physical qualification и concrete runtime installation ещё открыты. Полного зачёта V30 нет.


## V30: prepared recipe physical array qualifier

Добавлен PreparedImportArrayQualifier: owned canonical image witnesses, typed mask geometry registration, exact input evidence, scratch-bounded streaming SHA/length, existing NPY mmap shape/float32/finite/EOF validation. RGB допускает HDR finite значения; alpha/mask требуют [0,1]. Unknown binary roles не принимаются. build_prepared_import_recipe_qualifier связывает schema closure, concrete document semantics и array qualification; inline Override masks регистрируются только после runtime gate. Unit-тесты RGB/mask/layout/dtype/nan/range/hash/witness/scope написаны, NOT RUN. Production runtime operator gate, frozen evidence transport и task installation остаются открытыми. Полного зачёта V30 нет.


## V30: shared multi-image recipe closure

Canonical pixel/alpha witnesses теперь доступны для соседних canonical images внутри общего CorrectionPlan и проверяются по собственным shape/role/SHA. Это не распространяется на masks: они остаются target-bound. Конфликт одинакового artifact ID с различными shape/role/SHA запрещён. Unit-тесты разных canvas, relabelled mask/foreign target, conflicting geometry и complete shared correction binding/ordinal/target написаны, NOT RUN. Scheduler, frozen evidence transport и runtime installation остаются открытыми; полного зачёта V30 нет.


## V30: frozen evidence contract/compiler/consumer

Добавлены typed FrozenInterchangeImportEvidence и normative schemas, deterministic ID, exact snapshot/metadata/diagnostics coverage, owned immutable bytes и reader SHA/JCS recompile verification. prepare_import_from_evidence связывает документ с frozen worker report/root stage. Unit-тесты terminal preservation/ownership/scope/metadata/order/hash написаны, NOT RUN. SQL publication и Params/closure integration ещё открыты: worker dispatch не включён. Полного зачёта V30 нет.


## V30: SQL frozen evidence publication

Написана forward-only 01335: private scope wrapper добавляет explicit evidence purpose и deterministic ID, требует exact current sources/metadata, ready pinned Plan object, ordered adopted-entry diagnostic subset и exact outer fields. Existing scheduler reserve/complete повторяют scope под tenant/run/quota locks с token/version replay; object IO остаётся вне TX. Storage scope не доказывает full diagnostic coverage без Plan bytes — typed compiler/worker делают это отдельно. Go publisher принимает dedicated evidence ID; unit storage-wire тест написан NOT RUN. Migration NOT APPLIED. Params/closure transport и dispatch ещё открыты; полного зачёта V30 нет.


## V30: scoped evidence catalogue и Go typed closure

Написана01336 для scheduler-only чтения ready immutable evidence по deterministic identity с текущими sources/metadata ACL gates. Go SQL catalog допускает новый contract; scoped validator владеет Plan/Parsed bytes и проверяет evidence identity/hash/JCS, full source inventory, deterministic collection/items и actual metadata, ordered exact diagnostics coverage. Возвращает typed Plan/Parsed/master children без archive label authority. Unit-тест terminal evidence/closure/scope/substitution написан NOT RUN. Миграции NOT APPLIED. Params/root closure integration и dispatch остаются открытыми; полного зачёта V30 нет.


## V30: executable Params и evidence closure

Go/Python execution document compilers публикуют Plan/Evidence/Params; Params pin и Go declared closure/direct roots включают evidence. 01337 сохраняет fenced evidence proof и требует current sources/metadata equality при Params scope/admission; legacy four-field Params не admitted. Normative schema требует evidence. Unit-тесты порядка/ownership/missing input/revocation написаны NOT RUN; миграции NOT APPLIED. Scheduler/runtime dispatch и final activation/adoption остаются открытыми.


## V30: registration evidence fence и Params worker entry

01338 сохраняет fenced Params→Evidence pin; registration scope требует ready immutable Params/evidence и exact current source/metadata proof. Dedicated seal требует exact ordered Params/Plan/Parsed/Evidence roots и зарегистрированный typed evidence input; legacy paths не дают bypass. prepare_import_from_params читает strict executable Params и exact pinned root bytes, затем выполняет frozen evidence stage/report/root. Unit-тесты полного Params→result порта и preflight/hash refusals написаны, NOT RUN. Миграции NOT APPLIED. Concrete leased worker/runtime installation, activation и final adoption ещё открыты; полного зачёта V30 нет.


## V30: leased import operation adapter

Добавлен execute_import/interchange_import_operation: CPU/v2.2/zero-VRAM profile, mandatory authority result и output reservation, bounded aggregate declared JSON streaming/SHA, prepared frozen stage, mapping_shard/report/document outputs, exact receipts и repeated current admission вокруг publication. Prepared recipe inputs не публикуются заново. Unit-тесты complete no-recipe stage, post-publication revocation, evidence substitution и profile preflight написаны, NOT RUN. SQL leased admission, production recipe factory, runtime registry/activation и final adoption ещё не installed; dispatch не включён. Полного зачёта V30 нет.


## V30: leased SQL import admission

01339 worker-only admission проверяет principal/attempt/fence/lease/deadline/slot, full ready closure, exact four roots, frozen proof/current ACL/metadata и resource/quota. В неприменённой01337 evidence completion дополнительно сохраняет exact parsed JCS для SQL source verification без object IO. Scratch admission учитывает только typed processing arrays, не opaque originals неподдерживаемых entries. TaskDatabase и SQLImportAdmission возвращают строго bounded immutable ImportAuthority. Unit-тесты точного ответа/UUID/budgets/modified Params написаны NOT RUN; миграции NOT APPLIED. Runtime installation, activation и final adoption остаются открытыми.


## 2026-10-10: пересчёт и подключение import runtime

По текущему реестру: 90 entrypoints; 73 требуют semantic review (81,11%), 11 имеют частичный runtime path (12,22%), 6 не имеют runtime path (6,67%). 93,33% — потолок присутствия кода, не процент реализации. Реальная полнота реализации всей системы пока не установлена: для числовой оценки нужна поэлементная сверка требований целых модулей с текущими исполняемыми цепочками. Непроверенные элементы не объявляются завершёнными. Это пересчёт реестра, а не новый полный аудит всех модулей.

Добавлена явная ImportInstallation в build_worker/run_worker с ограничением CPU resource/backend; default None. Parser flag не активирует import. SQL factory требует callable recipe qualifier, custom installation проверяет admission/qualifier ports до регистрации операции. Unit tests написаны, не запускались по указанию пользователя. Миграции не применялись.

V30 остаётся незавершённым: требуются production recipe factory, публикация подготовленных зависимостей, final adoption/recovery, activation/scheduler, review actions и export. Подключение адаптера не закрывает весь модуль и не повышает подтверждённый процент целого продукта.


### V30: independent import adoption reconstruction

Добавлен verify_import_output: до commit независимо восстанавливает полный mapping через executable Params, Plan, Parsed, frozen evidence и квалификацию рецептов. Сравнивает каждый байт shard/report/root с scheduler-verified staged outputs; проверяет counters, порядок/shard cardinality, imported_files, отсутствие лишних/дублирующихся staged outputs, input/output alias, SHA/JCS и aggregate budgets. Proof содержит owned immutable bytes/pins без mutation API. Порты чтения обязаны проверять declared/staged inventory и immutable versions; SQL adoption по-прежнему должна атомарно перепроверять fence/result/ACL и текущие sources/metadata. Это pre-commit verifier, ещё не подключённый commit; V30 остаётся partial.

Написаны unit tests против подмены worker claims с пересчитанными SHA (status/reason, counters, imported inventory, drafts), лишних outputs, alias, input hash, бюджета и отмены. Тесты не запускались, миграции не применялись.


### V30: fenced scheduler preparation for import adoption

01340_v30_interchange_import_adoption_context.sql предоставляет только scheduler read authority для exact succeeded import task/attempt/fence/result; проверяет ready Params, closure, adoption context и frozen sources/metadata против текущих SQL данных. prepare_current_interchange_import использует autocommit read connection, independently проверяет source/role/MIME/size/SHA каждого читаемого JSON, ограничивает inventory, запускает независимую реконструкцию, затем повторяет authority read. Объектное IO не удерживает commit transaction. Добавлены тесты revocation/authority change, чужого source, отказа initial authority, transaction preflight и отмены. Тесты не запускались; 01340 не применена. Финальный SQL commit/recovery и подключение scheduler остаются незавершёнными.


### V30: dedicated artifact adoption commit and recovery

01341 запрещает generic adopt_v2_plan для import, выделяет adopt_v30_interchange_import: tenant/run/task locks, текущий succeeded attempt/fence/result и authority, actual Params/root/report/shard SHA, imported inventory из stored Parsed, размеры/counts и output budget, повторная document scope validation, existing full closure adoption и специальный inbox marker в одной транзакции. 01342 подтверждает исторический exact commit по двум markers, event и root artifact link; свежую processing authority не выдаёт.

AdoptionCoordinator подключён к dedicated prepare/commit/recovery; optional operator recipe qualifier factory передаётся независимой реконструкции. При отсутствии квалификатора prepared recipes не принимаются. Semantic proof теперь сохраняет owned Params bytes/pin для SQL commit. Написаны unit tests commit argument binding, context/hash/digest mismatch и strict recovery booleans. Тесты не запускались; 01341–01342 не применялись.

Граница результата: это atomic artifact adoption, ещё не полное открытие import review. Persist review targets/draft streams, waiting_review transition, DecideImport/CloseReview, production recipe factory и scheduler compilation/activation остаются незавершёнными. Parent success barrier сохранён.


### V30: deterministic review materialization packet

prepare_import_review преобразует независимо восстановленный report в owned canonical packet для будущего atomic review materializer. Проверяет executable Params и Plan SHA/project/snapshot, root/report/shards SHA, ordered entry/version/recipe bindings и exact proposal pins. Для proposals детерминированно назначает отдельные stream/document UUID от workflow+entry; не создаёт approval. Entries без proposal terminal rejected/unsupported. Packet ограничен 8 MiB и поддерживает cancellation. Unit tests написаны для детерминизма, отсутствия implicit approval, чужого project/workflow и модифицированного Plan/root/proposal evidence; не запускались. SQL storage/transition и coordinator invocation этого packet ещё не реализованы.


### V30: atomic review storage

01343 сохраняет review targets/root/report в backend_documents.v30_import_review в той же транзакции, что и dedicated artifact adoption. v2_workflows остаётся immutable: после чтения trigger выбрана существующая mutable backend_documents запись Params, включённая в tenant purge. Для каждого ordered mapping проверяются frozen source entry/version, declared EditingRecipeV3 pin, назначаются deterministic stream/document IDs; extra/missing targets и conflicting replay вызывают exception с rollback всей adoption. Новых tenant tables/DELETE privileges нет. Draft stream rows и waiting_review transition ещё не создаются; до них parent barrier остаётся закрытым. Миграция не применялась; тесты не запускались.


### V30: exact qualified recipe closure receipts for draft GC

PreparedRecipeClosureQualifier сохраняет immutable dependency pins только после полного успешного semantic/physical обхода. qualified_document_refs(ref,binding) выдаёт fresh owned SQL refs со stable purpose/ordinal, исключая recipe root (он удерживается самим document). Receipt связан с exact root SHA и полным canonical binding; failed requalification его аннулирует. Это необходимый вход для атомарной регистрации v2_document_refs при создании draft. Написаны unit tests success-only выдачи, exact inventory, ownership, foreign binding и failed requalification. Тесты не запускались. Draft materializer и waiting_review transition остаются незавершёнными; существующий SQL register_v2_document_refs ограничен 512, полноценный import materializer должен поддержать заявленный closure до 30000 без усечения.


### V30: draft materialization and opening review

VerifiedImportOutput передаёт owned canonical recipe closure receipts, собранные непосредственно после независимой полной qualification каждого proposed recipe. Требуется qualified_document_refs; нельзя заменить proof произвольным True. Проверяются exact purposes/ordinals, duplicates, root/output alias, max30000 refs/recipe и max1M aggregate. Dedicated commit arguments теперь содержат отдельный 12-й аргумент receipts, matched с exact proposal pins.

01344 закрывает scheduler доступ к старой 11-аргументной функции и в новой 12-аргументной функции выполняет artifact adoption + review storage + отдельные draft streams/documents + exact declared/ready GC refs + waiting_review transition + run event/outbox в одной транзакции. Не использует ограниченный 512-ref registrar и не обрезает closure. Approved pointers/approvals не создаются. Extra/missing closures, stream conflicts и revoked artifacts откатывают всю транзакцию.

Миграция не применялась, unit tests commit boundary обновлены/дополнены, не запускались. SQL integration tests и positive prepared-recipe end-to-end ещё нужны в тестовой фазе. DecideImport/CloseReview, API/client review actions, production recipe factory и compiler activation остаются незавершёнными. Реальная полнота всего продукта не установлена.


### V30: typed review command intents

Добавлены strict ImportReviewRequest/ImportReviewCloseRequest и FreezeImportReviewIntent: duplicate/unknown/null/missing fields отвергаются canonical typed roundtrip; batch1..100 unique nonzero entry IDs, approve/reject only; decimal revision без leading zero в safe mutable range; lowercase report SHA. Intent владеет canonical request и review_selection bytes, stable actor/workflow/request-scoped owner UUID, hashes и copy getters. Это подготовка publication/commit, не approval authority и ещё не HTTP handler. Unit tests strict decoding, revision bounds, stable ownership и copies написаны, не запускались. Следующая необходимая часть — fenced publication ledger и SQL decide/close с CAS, resolved FrozenRecipe для approve и immutable v2_review_batches history.


### V30: current editor review context

01345 добавляет v30_interchange_review_context: tenant context/editor authorization, exact waiting_review revision/report SHA, deterministic succeeded import task и historical dedicated adoption receipt, current ready report artifact. Возвращает scoped workflow/project/run/review без object locations. Это read authority для будущих publication/commit, не самостоятельная команда DecideImport/CloseReview. Миграция не применялась; тесты не запускались.


### V30: review selection publication ledger

01346 добавляет scoped reserve/complete review_selection с current editor/revision/report authority, deterministic owner совместимым с Go intent, storage quota, stable artifact ID, rotating reservation token, ready replay и immutable object version. SQLReviewDocumentRepository подключает эти функции к shared artifactjson.Publisher (reserve TX → PUT/recovery вне TX → complete TX). Репозиторий проверяет intent bytes SHA/size, run binding и exact object key/media/version. Это publication adapter; command orchestration/commit ещё нужны. Миграция не применялась, проверки/тесты не запускались.


### V30: CloseImportReview atomic commit

01347 commit_v30_interchange_review_close требует pending Interchange.CloseReview idempotency record с exact request SHA; current editor/revision/report context и отсутствие proposed targets; exact ready selection bytes/SHA/version/owner. Атомарно сохраняет close v2_review_batches, CAS completed/revision, idempotency response, audit/event/outbox. FrozenReviewIntent.CommitClose передаёт owned request/document pins и проверяет strict JobStatus receipt. Unit test argument binding, selection substitution и ownership написан, не запускался. Миграция не применялась. HTTP orchestration и authorized replay пока не подключены; approve/reject commit остаётся незавершённым.


### V30: CloseImportReview orchestration and authorized replay

01348 предоставляет close replay только после current editor authorization и exact idempotency hash/workflow/run/immutable close batch проверки; отсутствие прежней waiting_review revision не мешает historical replay. Service.CloseImportReview: freeze → authorized replay → current context/run → selection publish вне TX → authorized race recovery → idempotency.Use + dedicated CAS close commit. Final transaction повторно проверяет project editor authority также для generic idempotency replay. HTTP handler wiring ещё отсутствует. Миграция не применена; тесты/проверки не запускались. Approve/reject и production end-to-end остаются незавершёнными.


### V30: CloseImportReview HTTP wiring

Подключён POST /api/v1/tenants/{t}/interchange-jobs/{id}/close-review через mutation tenantRequest. Handler ограничивает body64KiB, strict typed request/idempotency key, проверяет completed JobStatus/resource/revision и replay header. Production main устанавливает InterchangeReviewCloseService с API pool/object storage/bucket. Написаны HTTP unit tests valid/replay, foreign receipt, invalid duplicate/unknown/key и unauthenticated route. Не запускались. Approve/reject и client review controls ещё не реализованы; весь V30 не объявляется завершённым.


### V30: bounded review projection and client close action

01349 расширяет ACL-filtered inspection nullable review summary (exact report pin, counts, editor-only can_close). Клиент strict reader проверяет counts/state/report; OwnerAPI.closeInterchangeReview фиксирует scoped snapshot, CAS request и validated completed receipt. Экран показывает remaining proposals и Close review только по can_close, защищает double submission, сохраняет idempotency key на retry того же revision/report, отменяет mutation при unmount, показывает conflict/access/retry errors. Миграция не применена, тесты/проверки не запускались. Approve/reject UI/backend остаются незавершёнными.


### V30: exact proposed-target batch selection

FrozenReviewIntent.SelectDecisionBatch читает current editor/revision/report SQL context, strictly валидирует owned review packet, deterministic task/stream/document identities и selected entry/version/recipe pins. Batch затрагивает только unique proposed targets; terminal/foreign/missing targets отвергаются. Результат сохраняет owned canonical selection bytes и количество требуемых FrozenRecipe approvals. Написаны unit tests stale revision/report, terminal/foreign target и ownership; не запускались. Это preflight, ещё не decide commit. Shared v2draft resolver найден для дальнейшей подготовки FrozenRecipe без premature approval.


### V30: atomic approve/reject commit

01350 commit_v30_interchange_decisions сохраняет batches1..100 с exact pending receipt/request/selection/report/revision. Все targets должны оставаться proposed, current draft stream/document/recipe неизменны. Approve требует exact prepared FrozenRecipe receipt и вызывает существующий approve_v23_draft_with_sources (source/upload/revision/approval guards); reject меняет только review target без approval. В одной TX обновляются targets, parent revision (status остаётся waiting_review), immutable batch, receipt, audit/event/outbox. Child всегда null. FrozenReviewIntent.CommitDecisions валидирует resolved receipt inventory и строгий ReviewBatchResult. Миграция не применена, тесты/проверки не запускались. FrozenRecipe preparation/orchestration, authorized batch replay, HTTP/client decisions ещё нужны.


### V30: preparing resolved import approvals without pointer mutation

PrepareImportApproval связывает exact selected target с current asset/stream/document, использует ReadApprovalDraft и full RenderRecipeSource.Load, сохраняющий ordered overrides/layers, квалифицированные ICC/model locks и source geometry. BuildFrozenRenderRecipeFromDocuments применяет whole-module overrides; scalar FrozenRecipe witness берётся из результата этого полного compiler, а не из нулевых override vectors. Исходный approved document остаётся полным immutable authored recipe. Witness публикуется существующим v2draft.Publisher с deterministic frozen ID; approval/pointer ещё не меняются. Возвращается exact stream revision + resolved artifact receipt для будущего batch CAS. Проверки/тесты не запускались. Batch orchestration/replay/HTTP пока не подключены; background policy и complete recipe qualification требуют общей end-to-end проверки.


### V30: DecideImport orchestration and replay

Service.DecideImport выполняет authorized replay, current batch selection, полную подготовку FrozenRecipe receipts для approve без pointer mutation, публикацию selection и post-IO race recovery, затем idempotency.Use + atomic CommitDecisions. Перед generic replay в final TX повторно проверяется project editor authority. 01351 replay проверяет current editor, exact hash/workflow/batch, immutable decide history и child=null, не требует исторической revision оставаться текущей. Написаны unit tests reject-only commit binding/empty resolved list, ownership, foreign approval receipt rejection before SQL. Тесты не запускались, миграция не применялась. HTTP/client decisions остаются незавершёнными.


### V30: DecideImport HTTP wiring

Подключён mutation POST /api/v1/tenants/{t}/interchange-jobs/{id}/decisions. Handler strict batch/body64KiB/idempotency parsing; response проверяет expected workflow, deterministic actor/request batch ID, revision+1, remaining0..5000, child=null и replay header. Production main устанавливает API service + complete v2draft dependencies. Unit tests valid/replay, foreign workflow, non-null child, duplicate nested key, missing key и session requirement написаны, не запускались. Клиентский batch review и текущие targets projection ещё нужны. Весь V30 остаётся partial до production import compiler/recipe factory/activation и полной проверки.


### V30: proposed targets projection and typed client batch API

01352 inspection возвращает ordered первые100 proposed targets (entry/version/external/field statuses), counts и current editor can_decide. После batch следующая выборка показывает следующий набор, без загрузки всех5000. Клиент копирует/проверяет target/field identities, bounds и status/reason shape. OwnerAPI.decideInterchangeReview допускает только unique entries текущей страницы, фиксирует revision/report/key, проверяет workflow/revision/child=null и exact remaining count receipt. Добавлены unit tests ownership/duplicates/invalid can_close, foreign selected entry, exact request и remaining mismatch. Не запускались; миграция не применена. UI выбора решений ещё не подключён.


### Пересчёт 2026-10-10: текущий код и batch review UI

Актуальная классификация каталога из90 entrypoints:73 requires_semantic_review,14 partial_runtime_path,3 missing_runtime_path. DecideImport и CloseImportReview имеют HTTP/service/SQL/client paths; import execute имеет explicit CPU installation и independent adoption, но production factory/activation остаются открытыми. Поэтому три позиции переведены из missing в partial, без кредита полной реализации.

87/90 =96.67% — исключительно покрытие каталога наличием полного или частичного кода, не процент реальной реализации системы. Whole-product implementation percent остаётся null: реестр не содержит актуального requirement-by-requirement semantic audit для73 позиций и всех UI/DB/worker требований. Процент доказанно завершённых целых модулей этим пересчётом не устанавливается; отсутствие доказательства не означает отсутствие кода. Предыдущие значения93.33% в исторических записях устарели.

InterchangeJobScreen теперь показывает текущие proposed targets/field statuses, approve/reject selection, bounded Save decisions и существующий Close review. Повтор неизменённого batch использует тот же idempotency key; revision/report change сбрасывает выбор. Mutation guard защищает двойное нажатие; unmount отменяет запрос. Generation fence отбрасывает результаты polling, начатого до mutation, включая поздние ошибки. Написаны DOM tests: reject→refresh→close availability, approve retry key, polling revision invalidation, double-submit/unmount abort.

Тесты, build, typecheck, validation scripts НЕ запускались; миграции НЕ применялись по принятому пользователем режиму. Новый UI не считается проверенным. Остаются production import compiler/recipe qualification factory/activation, полный export/signing, другие разрывы V29–V32 и последующий глубокий тестовый цикл Metal/MPS с лимитом10,000,000,000 bytes.

### V30: executable import compiler with current metadata evidence

Добавлен ImportExecutionCompiler.Compile: compile plan со snapshot sources → actual metadata receipts → compiler diagnostics → exact FrozenInterchangeImportEvidence + executable Params → повторный metadata/source read после всех IO. Byte-exact изменение snapshot возвращает ErrStale, отказ authority сохраняется. Все bytes для внешних ports копируются; неподдержанный рецепт без обязательной диагностики отклоняется существующим evidence validator. SQLImportMetadataReader читает dedicated scheduler helper01325 в короткой tenant transaction, проверяет JCS/scope/parsed/collection. Publication и admission не выдаются этим helper: их SQL guards остаются обязательны.

Unit tests написаны для изменения metadata/sources во время compiler IO, отзыва доступа, foreign metadata, extraneous diagnostics, ownership buffers, cancellation и отсутствующей зависимости. Не запускались. Production recipe remapping/typed dependency qualification, scheduler compiler orchestration/activation и export/signing остаются открытыми; whole-module completion credit не добавлен.


### V30: recipe compiler outcomes captured in executable evidence

CompileImportPlan сохраняет typed ImportRecipeUnsupported в manifest order; CompiledImportPlan.DiagnosticsBody возвращает owned canonical bytes. Unsupported reason1..200 Unicode symbols обязателен, nil receipt без typed outcome запрещён. Одновременно receipt+unsupported запрещены. Transport/ACL errors и cancellation остаются ошибками admission. ImportExecutionCompiler по умолчанию использует захваченные outcomes; optional independent diagnostics reader может лишь подтвердить byte-exact тот же результат, не заменить причины. Это устраняет разрыв между resolver output и frozen diagnostics, не добавляя фиктивных fallback recipes.

Написаны unit tests supported/unsupported, wrapped errors, malformed reason, ambiguous receipt, nil receipt, transport failure, cancellation, exact binding, owned bytes и автоматическое включение unsupported reason в executable evidence без внешнего reader. Тесты/build/typecheck не запускались, миграции не применялись. Production recipe resolver/remapping/qualification/publication и scheduler activation ещё не завершены.

### V30: prepared root recipe remapping and bounded archive JSON reader

PreparedImportRecipeResolver связывает recipe path с exact parsed staged file; проверяет SHA/JCS/EditingRecipeV3 до remap, квалифицирует old→new source, переносит StyleV2/CorrectionPlan/LayerStack/MaskRef и все overrides через обязательный dependency remapper, сохраняя override order и parameter_delta. Пересчитывает canonical parent SHA и deterministic workflow/entry root UUID, повторно валидирует recipe, требует full prepared qualifier перед публикацией и exact publication receipt. Foreign learning state не копируется и не удаляется молча: explicit unsupported dependency до квалифицированного materializer resolved values. BoundedImportArchiveReader читает exact immutable parsed opaque file с ограничением2..8MiB и повторным scoped catalog lookup после IO, не повышает архивные contract labels до processing authority.

Написаны unit tests переноса всех root dependency kinds и override order, сохранения scalar bytes, hash/source/dependency/qualification failures, неизменённого UUID, foreign learning outcome и подмены publication receipt. Не запускались. Recursive typed dependency remapper, source qualifier, prepared closure qualifier, SQL prepared-document publisher и production wiring ещё нужны; root helper не считается завершённым import module. Миграции не применялись.

### V30: concrete recipe source qualifier

ParsedImportRecipeSourceQualifier разрешает old WorkingImage UUID только через exact parsed manifest inventory→staged ref; архивный SHA проверяется до структурного чтения, старый canvas_hash и original encoded SHA сверяются. Current master читается отдельным SQL-authorized ImportMasterReader, проверяются hash/WorkingImage/new canvas/encoded source/float32/working color. Старый asset/master UUID не может остаться новым. Несовместимые canvas/color дают explicit unsupported reason, без неявного rescale/rotation или изменения масок. Квалификация source не заменяет full typed dependency closure и physical array qualification.

Unit tests написаны для exact staged lookup/current master, foreign entry/archive UUID, old/new canvas substitution, archive/current byte substitution, UUID reuse и cancellation. Не запускались. Рекурсивный dependency remapper и prepared publisher/qualifier production wiring остаются открытыми; module completion credit не добавлен.

### V30: recursive dependency graph remapping

RecursiveImportDependencyRemapper реализует manifest inventory→staged lookup, exact old UUID/SHA/contract, hash/JCS-before-remap, current source qualification, recursive child-before-parent publication, deterministic workflow/entry/contract/original IDs и пересчёт parent SHA. Бounded обход: depth32,30000 documents,8MiB/document,128MiB aggregate; cycles/type substitution/unreachable archive refs отклоняются. Shared DAG child публикуется один раз в invocation. Exact old WorkingImage source заменяется qualified current master; прочие dependencies требуют installed full typed codec. Новый typed документ проходит qualification до publication; receipt должен совпасть. Invocation cache не разделяется между source/tenant contexts.

Unit tests написаны для shared DAG publication order, deterministic replay, cycles, type/hash substitution, qualification failure и incorrect publication receipt. Не запускались. Full concrete contract codecs (включая physical binary arrays), SQL prepared dependency/root publishers, closure qualifier factory и scheduler activation ещё открыты. Generic recursive engine не заменяет эти обязательные parts; module completion credit не добавлен.

### V30: concrete CorrectionPlan dependency codec

ImportCorrectionPlanCodec валидирует полный old CorrectionPlan существующим v2draft validator и переносит все items по owned scheduler mapping old binding→new binding, не только текущую запись. Для каждой пары выполняется source qualification, включая повтор при qualification нового документа. Snapshot digest заменяется текущим pinned import snapshot; delta/ordinal/disposition/reason сохраняются. Missing source mapping возвращает explicit correction_source_not_imported, а не удаляет item; несовпадающая old binding, duplicate mappings/targets, foreign snapshot и current source failures отвергаются. Другие contracts требуют explicit fallback codec.

Unit tests написаны для двух-source переноса с manual_locked, byte-exact authored values, mapping ownership, missing source diagnostic, revocation, snapshot substitution и cancellation. Тесты/build/typecheck не запускались, миграции не применялись. StyleV2/Override/LayerStack/MaskRef и binary codecs, SQL publication и production wiring остаются незавершёнными.

### V30: concrete MaskRef codec

ImportMaskCodec валидирует old MaskRef и exact old/new canvas, переносит MaskPixels и все inverse_field DenseWarpField refs через recursive resolver, пересчитывает transform-chain hash после замены refs и сохраняет geometry/selector/confidence/protected/model-lock metadata. Object UUID переносится детерминированно в namespace нового asset без наследования person/consent/approval. Другой canvas требует explicit unsupported, не resampling. Новый MaskRef проходит обязательный physical/model/source qualifier до publication; JSON validity не заменяет проверку array bytes и model lock.

Unit tests написаны для metadata preservation/new UUID, canvas mismatch, unchanged dependency UUID, dependency/physical failure и cancellation. Не запускались. Physical binary codec и concrete mask qualifier, remaining document codecs, SQL publishers и production wiring ещё нужны. Whole-module completion credit не добавлен.

### V30: concrete LayerStack codec

ImportLayerStackCodec проверяет полный old LayerStack, сохраняет source canvas и все authored enabled/opacity/blend/operation/version поля и layer order. Layer UUID переносится в namespace нового asset. Params и MaskRef refs, включая nested invert/union/intersect/subtract, проходят recursive remapper; mask node/depth limits сохраняются. Disabled/zero-opacity layers не удаляются. Canvas mismatch explicit unsupported; новый stack повторно структурно проверяется и требует installed operation/Params/model/source qualifier перед публикацией. Никаких silent rescale/clamp/reorder.

Unit tests написаны для двух слоёв с вложенными mask operators, сохранения authored values/order и disabled layer, new layer UUIDs, canvas mismatch, unchanged dependency UUID, params/qualification failure и cancellation. Не запускались. Concrete operation Params codecs/Layer qualifier, binary codecs, SQL publishers и production wiring остаются открытыми. Whole-module completion credit не добавлен.

### V30: concrete Override codec

ImportOverrideCodec читает и проверяет current ingest master, exact canvas/original SHA, валидирует old/new Override существующим editor validator и mandatory full module/asset/model/source qualifier. Whole-module bytes, revision, manual landmarks и protected polygons сохраняются; object identifier переносится в namespace нового asset. Inline masks используют MaskRef codec, включая arrays/warp qualification. Архивные candidate review decisions и подтверждения масок требуют отдельного переноса authority: сейчас explicit unsupported, без их тихого удаления или наследования подтверждения. Optional отсутствующий confirmed трактуется false согласно контракту.

Unit tests написаны для module/manual field preservation, inline protected mask remap, new object identity, source reread, foreign candidate review/confirmation, wrong source SHA, qualification failure и cancellation. Не запускались. Concrete full module qualifier/StyleV2 codec, physical binary codecs, SQL publishers и production wiring остаются открытыми; module completion credit не добавлен.

### V30: concrete StyleV2 codec

ImportStyleCodec сохраняет все processing fields (color/raw/models/global curves/LUT/local/adaptation/beauty/defects/geometry/strength) и создаёт новый deterministic style_id. Архивный документ требует full schema/operator/model validation до side effects; optional background asset получает новый portable ID через explicit remapper, затем новый style требует full current qualification. Архивные quality/provenance claims не повышаются до локальных доказательств: status=needs_review, transfer_confidence=unvalidated, historical metrics=null, counters=0, manifest/runtime refs=null, warning imported_style_requires_validation; seed сохраняется. Это административные поля, параметры рендера не меняются.

Unit tests написаны для byte-exact processing field preservation, identity change, old/new validation ownership, explicit background remap/missing dependency, model qualification failure и cancellation. Тесты/build/typecheck не запускались, миграции не применялись. Full schema/operator/model implementation за qualifier port, background importer, physical codecs, SQL publishers и production wiring ещё открыты. Module completion credit не добавлен.

### V30: streaming MaskPixels physical remap

RecursiveImportDependencyRemapper получил отдельную binary branch с aggregate128GiB budget, deterministic ID/exact SHA/size receipts и shared-DAG reuse. JSON budget128MiB остаётся независимым. ImportMaskBinaryRemapper читает qualified current master, сверяет canvas, через scoped opaque catalog открывает exact immutable parsed object и использует существующий masknpy streaming verifier: NPY C-order little-endian float32 exact H×W, finite [0,1], bytes/hash/no trailing bytes. После проверки и после публикации повторяется object metadata lookup; master bytes повторно читаются после IO. Publisher обязан копировать exact pinned bytes; нормализация/resampling запрещены.

Unit tests написаны для exact transfer identity/bytes, wrong shape/range/NaN, version race, incorrect receipt, byte budget и hash substitution; не запускались. DenseWarpField и другие array kinds требуют occurrence-aware physical codecs (сейчас explicit unsupported), concrete SQL binary publisher, qualifiers и production wiring остаются открытыми. Whole-module completion credit не добавлен.

### V30: concrete physical imported-mask qualifier

ImportPhysicalMaskQualifier связывает новый MaskRef с exact current canonical master (SHA/JCS/canvas/float32/working color), требует manual selector при отсутствии model lock либо exact installed lock allowlist. Allowlist копируется при installation. Ready dependency catalog должен доказывать current workflow/source provenance. MaskPixels проверяются streaming NPY float32 H×W unit; каждый inverse_field DenseWarpField — float32 H×W×2 finite по to_canvas конкретного transform step. Суммарный byte budget обязателен; object metadata повторно читаются после array IO, master bytes — после всего qualifier. JSON validity/allowlist не даёт SQL authority автоматически.

Unit tests написаны для manual/installed person model, caller policy ownership, missing/unknown model, canvas mismatch, out-of-range values, version race, byte budget и cancellation. Тесты/build/typecheck не запускались; миграции не применялись. Concrete prepared-dependency SQL catalog и publisher, occurrence-aware binary warp remapping, остальные full schema/operation qualifiers и production wiring ещё открыты; whole-module completion credit не добавлен.

### V30: occurrence-aware DenseWarpField remap

ImportedDenseWarpRemapper связывает exact archived UUID/SHA/contract со staged parsed file, проверяет original source manifest, current canonical master и обе occurrence canvases. NPY проверяется потоково float32 H×W×2 finite по конкретному to_canvas (не final image); отрицательные/больше1 координаты не clamp. Новый UUID закреплён по workflow/entry/original/from/to/target, SHA и bytes сохраняются. Object lookup до/после verification и после publication, master reread после IO; exact publisher receipt обязателен. MaskRef codec теперь передаёт from/to из validated transform step в occurrence remapper и пересчитывает chain hash; generic бинарный вызов без occurrence больше не подменяет эту проверку.

Unit tests написаны для intermediate canvas, отличающегося от final frame, finite coordinate preservation, deterministic replay, wrong shape/NaN, foreign source, inventory gap, version race и incorrect receipt. Не запускались. SQL binary/root/dependency publishers/catalog, operation/schema qualifiers и production wiring остаются открытыми; module completion credit не добавлен.

### V30: prepared JSON/binary immutable publication

PreparedImportArtifactPublisher реализует root recipe, dependency JSON и binary publisher ports. Scope tenant/run/workflow/entry/binding обязателен; root recipe UUID и source contract валидируются. JSON exact JCS/SHA/8MiB; binary exact immutable source metadata/128GiB. Reserve предшествует IO; stream проверяется независимо по фактически прочитанным bytes/SHA; immutable Exists/Unavailable использует verified recovery. Complete вызывается только с exact object metadata/version и receipt, cancellation не завершает upload. Ready replay не читает/не пишет bytes. SQL store должен независимо enforce ACL/current source/parsed origin/deterministic identity/quota/fence — этот interface ещё не concrete authority.

Unit tests написаны для JSON upload/recovery/ready replay, pinned binary copy, changed bytes/entry/reservation/object и cancellation. Не запускались. Concrete SQL store/catalog, remaining full schema/operation qualifiers и production wiring ещё открыты; module completion credit не добавлен.


### V30: SQL prepared artifact store и повторный пересчёт

01353 добавляет scope certificate в существующий backend_documents, независимые reserve/complete SECURITY DEFINER функции для scheduler и Go SQLPreparedImportArtifactStore. Повторная SQL authority проверяет active tenant, running sealed workflow, exact parsed/source/metadata, manifest origin, deterministic artifact identity, quota и fencing token. Complete повторяет authority после object IO; ready replay требует exact object version. Добавлены unit-тесты преобразования SQL outcomes, foreign/missing identity и fence, replay, ошибок чтения и cancellation до DB.

Ограничения: SQL проверяет форму canvas hash; физическая/schema qualification остаётся обязанностью compiler. Occurrence-specific DenseWarpField UUID пока не поддержан SQL identity gate: такой путь отклоняется, до отдельного расширения authority. Scoped ready prepared catalog, полные operation/model qualifiers и production orchestration ещё нужны. Новый полный модуль не зачтён. Тесты, сборки, typecheck не запускались; миграция не применена.

Повторный подсчёт реестра: 73 требуют семантического аудита, 14 частичных, 3 отсутствующих из 90. 96,67% измеряет только наличие кода. Реальный общий процент нельзя вычислить из этого реестра: отсутствует полный взвешенный зачёт нормативных требований. Непроверенные пути не объявляются ни готовыми, ни отсутствующими.


### V30: scoped prepared ready catalog

01354 и SQLPreparedImportCatalog разрешают immutable reads только для exact tenant/run/workflow/entry/binding/contract/UUID/SHA prepared artifact. SQL восстанавливает receipt из ledger, повторяет current parsed/source/metadata authority через scope helper и требует byte-identical JSONB certificate, ready verified artifact, exact bucket/key/version/media/bytes и unexpired object. Go повторяет identity/size/media/key проверки; отсутствие строки отображается в ErrStale. Scope не выдаёт typed/model/processing approval.

Unit tests написаны для foreign bucket/key, missing/null object version, substituted SHA/media, JSON minimum/byte budget, SQL read error и cancellation до DB. Не запускались; сборки/typecheck не запускались; миграции не применены. Occurrence-specific DenseWarpField authority, full schema/operator/model qualifiers и production orchestration остаются открытыми. Зачёт полного модуля не добавлен; реальный процент неизвестен, наличие кода 87/90=96,67% не является полнотой продукта.


### V30: immutable prepared JSON reader

BoundedPreparedImportReader предоставляет контрактное чтение JSON-зависимостей через scoped prepared catalog: allowlist JSON contracts, bounded byte count, exact SHA/JCS/object root, immutable object version, cancellation на каждом Read и повторная authority lookup после IO. Open error закрывает возвращённый reader; read/close ошибки не возвращают документ. Форма JSON не является полной schema/operator/model qualification.

Unit tests написаны для exact successful read, changed catalog version, wrong hash/size, trailing bytes, noncanonical JSON, array root, wrong media, byte limit, binary contract и cancellation. Не запускались; сборки/typecheck/миграции не запускались. Full semantic qualifiers, occurrence-specific DenseWarpField SQL identity и production factory/orchestration остаются открытыми. Общий процент реализации не установлен; новых полностью закрытых модулей не начислено.


### V30: prepared LayerStack qualification coordinator

ImportPreparedLayerQualifier валидирует полный wire/source LayerStack, читает exact scoped Params для каждого слоя и передаёт operation/version/bytes/current binding обязательному full operation qualifier. Disabled/zero-opacity слои также проверяются. Все mask expressions обходятся штатным ValidateLayerStack; каждый MaskRef проходит typed ParseMaskRef и обязательную physical qualification. Shared documents читаются один раз, <=8MiB/document и <=128MiB aggregate; bytes копируются перед передачей внешнему validator. После всех IO каждый unique document повторно читается и сравнивается с исходными bytes. Наличие operation/version в реестре не заменяет full Params/runtime/model validation.

Unit tests написаны для disabled/shared dependencies, parameter ownership, operation/mask failure, final authority race, missing operation qualifier и cancellation. Не запускались; сборки/typecheck/миграции не запускались. Concrete operation-specific validator, остальные full qualifiers, occurrence warp SQL authority и production wiring ещё открыты. Полный модуль не зачтён; процент реальной реализации не установлен.


### V30: typed local portrait Params remap

ImportLocalPortraitParamsCodec переносит PortraitHairParams и PortraitClothingParams через существующие Compile: exact canonical schema/fields/ranges, old binding, same canvas, новый target source и новые MaskRef/protected identities. Все authored controls сохраняются. Mandatory operation qualifier получает конкретный operation/version1 и owned prepared bytes; остальные Params передаются fallback либо explicit unsupported. Наличие compile не объявляется доказательством installed runtime/model/physical dependencies.

Unit tests написаны для обоих типов, сохранения настроек, remap обеих масок, source mismatch, canvas mismatch, unmapped refs, qualification failure и cancellation. Не запускались; сборки/typecheck/миграции не запускались. Concrete operation runtime qualifier, прочие Params codecs и production wiring ещё открыты. Полный модуль не зачтён; реальный процент реализации не установлен.


### V30: concrete local portrait operation qualification

ImportLocalPortraitOperationQualifier проверяет exact typed Hair/Clothing Params, operation/version1/current binding, SHA/canvas/float32/working color master, штатный CompileLocalExecutionProfile и configured capacity. Обе scoped MaskRef проверяются typed+physical; после IO повторяются bytes обеих масок, master и runtime authority. SQLImportLocalRuntimeQualification использует существующий LockLocalRuntimeQualification с scheduler role/project/tenant и current V28 policy/report. Short transaction не заменяет qualification lock при последующем graph admission.

Unit tests написаны для hair/clothing success, operation/version mismatch, initial runtime failure, runtime revocation after IO, physical mask failure, capacity shortage, document race и cancellation/installation. Не запускались; сборки/typecheck/миграции не запускались. Production factory/admission wiring, другие operation qualifiers, Style/Override full qualification и occurrence warp authority ещё открыты. Полный модуль не зачтён; реальный процент реализации не установлен.


### V30: prepared Override source/mask qualification

ImportPreparedOverrideQualifier связывает exact canonical Override с SHA/current master canvas/float32/working color и существующим editor envelope validator. Candidate review и confirmed inline masks не наследуют чужую authority. Каждая inline MaskRef проходит typed/current canvas/physical qualification; master перечитывается после IO. Full module/manual geometry/landmark/model/asset validator обязателен перед physical IO и повторяется после IO с независимой копией bytes. Отсутствующий content validator запрещает путь; editor envelope не заменяет full schema validation.

Unit tests написаны для successful checks/ownership, initial и post-IO content failure, physical mask failure, wrong source, foreign review/confirmation, missing validator и cancellation. Не запускались; сборки/typecheck/миграции не запускались. Concrete full content validator, StyleV2 qualification, occurrence warp SQL authority и production wiring остаются открытыми. Полный модуль не зачтён; процент реальной реализации не установлен.


### V30: worker semantic callback ownership

Статическая сверка обнаружила существующий PreparedRecipeDocumentQualifier с полными Pydantic StyleV2/OverrideV1 models. Это не завершает Go/full production wiring. Закрыт разрыв ownership: caller canonical bytes фиксируются до extra validator, validator получает отдельный parsed buffer и сравнивается с immutable baseline. Runtime получает deep copy typed document/binding; array register получает отдельные typed mask/binding copies. Callback не может заменить уже проверенную mask geometry/ref перед physical registration или изменить caller input.

Три unit-теста написаны для runtime mask substitution перед registration, mutating extra validator и runtime mutation extra Params. Не запускались; сборки/typecheck/миграции не запускались. Полный модуль не зачтён; production runtime qualification factory, Go content bridge, прочие operation codecs и occurrence warp SQL authority ещё открыты. Реальный процент реализации не установлен.


### V30: closure caller identity snapshots

PreparedRecipeClosureQualifier фиксирует independent typed root/binding и полный EditingRecipeV3 до callback/IO. Ожидаемые dependency refs и receipt key больше не меняются при mutation caller recipe во время document read. qualified_document_refs фиксирует identity до active callback. При повторной проверке старый receipt удаляется до проверки recipe/active, включая invalid typed recipe. Повторный визит shared node не расходует лимит нового node; cycle/type checks сохранены.

Unit tests написаны для mutation root/source/base_style/overrides во время read, mutation caller identity в active при receipt lookup и invalid recipe requalification с отзывом старого GC receipt. Не запускались; сборки/typecheck/миграции не запускались. Production integration и оставшиеся V30 semantic/runtime/warp gaps открыты; общий процент реализации не установлен, полный модуль не зачтён.


### V30: worker DenseWarpArray occurrence qualification

PreparedImportArrayQualifier регистрирует DenseWarpArray из каждого dense_warp шага verified canonical WorkingImage. Shape закреплён по конкретному to_canvas H×W×2, а не final canvas; from/to <=200MP. Штатный physical reader проверяет little-endian float32/finite/exact NPY shape/hash/bytes; unit_interval=false сохраняет negative/>1 coordinates. Shared canonical CorrectionPlan source использует собственный immutable warp witness, masks остаются target-bound. Reference/binding копируются до active callback.

Три параметризованных случая написаны: корректный intermediate canvas перед resize, неверный final-grid shape, NaN. Не запускались; сборки/typecheck/миграции не запускались. Worker DenseWarpArray и Go DenseWarpField naming/SQL occurrence publication ещё требуют полного integration path; новый полный модуль не зачтён. Реальный процент реализации не установлен.


### V30: explicit Go/worker binary transport vocabulary

Worker PreparedImportArrayQualifier получил explicit mapping PixelArray→WorkingPixels, MaskPixels→MaskArray, DenseWarpField→DenseWarpArray с сохранением existing names. Normative WorkingImage alpha MaskArray принимается только при independent canonical WorkingAlpha witness. Generic NpyArray и подмена MaskRef массива ролью pixels/alpha/warp отклоняются до evidence/object IO. Shape/dtype/range/source checks не ослаблены.

Unit tests написаны для обеих warp transport names с valid/wrongshape/NaN, mask aliases, cross-role substitutions, unknown NpyArray и normative alpha witnessed role. Не запускались; сборки/typecheck/миграции не запускались. Это boundary adapter, не завершение SQL occurrence publication/full production wiring. Полный модуль не зачтён; реальный процент реализации не установлен.


### V30: post-IO array evidence check

PreparedImportArrayQualifier повторно получает evidence после streaming/scratch/physical NPY verification и требует equality исходному ref/media/bytes/role/source. Повторная metadata observation не доказывает object version: ArtifactEvidence не содержит version; immutable version должен независимо enforce reader/SQL adapter. Добавлены параметризованные unit tests unchanged и изменённых bytes/media/source/role/ref. Не запускались; сборки/typecheck/миграции не запускались. Production wiring и SQL occurrence path остаются открытыми, полный модуль не зачтён; реальный процент реализации не установлен.


### V30: owned local portrait Params before remapping

ImportLocalPortraitParamsCodec копирует authored raw до mask callbacks. Найденный source path повторно декодировал caller buffer после resolve; теперь mutation caller bytes во время remap не меняет controls и не ломает дальнейший typed Compile. Добавлен regression unit test с перезаписью исходного raw обоими mask callbacks и проверкой exact strength/detail/new binding. Не запускался; сборки/typecheck/миграции не запускались. SQL worker installation изучена: production factory требует authenticated declared-input catalog и runtime qualification adapters; этот wiring ещё не реализован. Полный модуль не зачтён; реальный процент реализации не установлен.


### V30: verified typed input snapshot for production factory

V2InputIndex.declared_entries возвращает owned typed entry refs/contracts из verified SQLite closure, только is_entry=1, deterministic order, <=30000 без truncation. TaskArtifacts.declared_input_entries проверяет active/closed session до и после получения. Transport-only allowlist refs исключены. Snapshot не устанавливает media/size/version; эти authoritative metadata ещё нужны для production qualifier factory.

Unit tests написаны для entry completeness, transport exclusion, ownership, overflow, closed index и invalid/bool limits. Не запускались; сборки/typecheck/миграции не запускались. Production factory/runtime/evidence adapters и SQL occurrence publication ещё открыты; полный модуль не зачтён, реальный процент реализации не установлен.


### V30: gateway immutable input metadata

Gateway download добавляет X-Photo-Media-Type и SHA256(object.VersionID) без раскрытия bucket/key/raw version. Worker Gateway.metadata использует existing authorized GET/open_stream headers и закрывает stream без материализации тела. ArtifactMetadata immutable, duplicate/partial/invalid media/version headers запрещены. Legacy bytes streaming сохранён, metadata qualification без headers отказывает. TaskArtifacts.input_metadata требует exact declared input и повторяет active check после IO.

Unit tests написаны для valid/legacy/partial/duplicate/malformed media/digest headers. Не запускались; сборки/typecheck/миграции не запускались. Это metadata transport prerequisite: production evidence adapter/factory и SQL occurrence publication остаются открытыми. Полный модуль не зачтён; процент реальной реализации не установлен.


### V30: concrete prepared input evidence reader

PreparedImportInputReader получает owned verified typed declared closure; reference вне snapshot отклоняется до gateway IO. Gateway media/bytes/version digest закрепляются при первом observation, повторяются перед stream и после IO; сам ArtifactStream metadata/size обязан совпадать. Stream bounded1MiB, exact size/SHA, aggregate read budget<=128GiB. JSON дополнительно требует application/json и <=8MiB. Reader не заменяет typed schema/semantic/runtime qualification. Gateway lease/ACL authority остаётся обязательной на каждом запросе.

Unit tests написаны для successful JSON, changed metadata/stream version, changed bytes/hash, undeclared ref, byte limit и wrong JSON media. Не запускались; сборки/typecheck/миграции не запускались. Production factory/runtime adapters, SQL occurrence publication и оставшиеся V30 codecs ещё открыты. Полный модуль не зачтён; реальный процент реализации не установлен.


### V30: concrete prepared recipe factory composition

PreparedImportRecipeFactory проверяет task contracts_sha==installed schema SHA, creates bounded declared-input reader, читает scoped InterchangePlan и unique canonical masters по bindings, собирает typed entries/media metadata и build_prepared_import_recipe_qualifier. JSON budget из admitted authority независим от aggregate binary IO cap; duplicate conflicting version bindings запрещены. Runtime port получает ctx/authority и обязателен; extra validators копируются. sql_prepared_import_installation композиционно связывает factory с existing SQLImportAdmission без auto-enablement.

Unit tests написаны для invalid schema/ICC/runtime/budget/validator installation и schema task mismatch до IO. Positive full factory integration ещё не доказана. Тесты/сборки/typecheck/миграции не запускались. Production runtime qualification adapter/activation, остальные Params codecs и SQL occurrence publication остаются открытыми. Полный модуль не зачтён; реальный процент реализации не установлен.


### V30: factory transport pins and positive unit composition

Factory повторно сверяет Params SHA с leased command, требует InterchangePlan/WorkingImage contracts в verified typed snapshot перед document IO и exact canonical master roundtrip. Inventory media observations проходят reader.evidence и закрепляются immutable version pin вместо обхода reader. Добавлены unit tests positive composition с mocked closure builder и Params/plan-contract/master-contract substitution failures до builder. Это unit composition, не full runtime/integration evidence.

Тесты/сборки/typecheck/миграции не запускались. Production runtime qualification adapter/activation, остальные Params codecs и SQL occurrence publication остаются открытыми; полный модуль не зачтён, реальный процент реализации не установлен.


### V30: operation-specific Params schema selection

Prepared closure больше не предполагает generic $defs.Params для local portrait layers. LayerStack operation/version1 выбирает PortraitHairParams/PortraitClothingParams full schema, включая disabled layers; conflicting reuse одного Params UUID разными operation schemas запрещён. Schema ref traversal использует выбранную схему и включает base/mask/protected refs. Document semantics использует existing full Pydantic Params models и exact current base binding; runtime qualification остаётся обязательной. Остальные layer operations явно отклоняются до добавления конкретных схем, без generic fallback.

Unit tests написаны для hair/clothing full schema/current binding/unknown fields и unknown layer operation/conflicting shared Params schema. Не запускались; сборки/typecheck/миграции не запускались. Other operation schemas/codecs, production runtime qualification и SQL occurrence path открыты. Полный модуль не зачтён; реальный процент реализации не установлен.


### V30: typed local layer child expectations

Closure walker передаёт expected child contract из selected parent schema: local Params.base.source→WorkingImage, mask/protected→MaskRef; LayerStack.source.source→WorkingImage, layers.params→Params, nested mask-expression leaf→MaskRef. Inventory label не может заменить эти JSON roles; unknown ref path в поддерживаемом local Params отклоняется. Existing exact SHA/declared membership/cycle checks сохранены.

Unit tests написаны для hair/clothing child roles, unknown local Params child и nested LayerStack mask roles. Не запускались; сборки/typecheck/миграции не запускались. Production runtime adapter, remaining operation support и SQL occurrence publication открыты. Полный модуль не зачтён; реальный процент реализации не установлен.


### V30: final input metadata recheck and GC receipt revocation

PreparedImportInputReader.recheck повторяет все ранее наблюдавшиеся immutable metadata pins после полного closure IO. Production factory передаёт callback в concrete qualifier; closure вызывает его до сохранения GC receipt и при каждом receipt retrieval. Ошибка повторной проверки отзывает ранее сохранённый receipt. Добавлен unit test successful final check, subsequent revocation и отсутствие receipt после неудачной повторной qualification. Не запускался; сборки/typecheck/миграции не запускались. Это fresh observation, не transaction lock через commit; финальная SQL admission/adoption authority остаётся обязательной. Production runtime adapter/activation, other operations и SQL occurrence publication открыты; полный модуль не зачтён, реальный процент реализации не установлен.


### V30: cancellation receipt revocation and evidence ref ownership

Receipt lookup initial active check перемещён внутрь invalidation guard: отмена отзывает ранее сохранённые GC pins. Prepared input reader snapshot exact ref до active callback, evidence возвращает immutable dataclass ref вместо caller Pydantic object. Regression tests написаны для cancellation followed by receipt refusal и mutation caller ref в active callback. Не запускались; сборки/typecheck/миграции не запускались. Production runtime/admission wiring и SQL occurrence publication остаются открытыми; полный модуль не зачтён, реальный процент реализации не установлен.


### V30: gateway metadata server contract

Download отказывает при absent/null object version или missing media type до object Open; SHA256(empty version) больше не выдаётся как immutable metadata pin. Existing download unit test дополнен проверками metadata media/version digest для full/range response, сохранения octet-stream transport и отказа absent/null version без storage IO/metadata headers. Не запускался; сборки/typecheck/миграции не запускались. Production runtime wiring и SQL occurrence publication ещё открыты; полный модуль не зачтён, реальный процент реализации не установлен.


### V30: occurrence warp certificate transport

PreparedImportArtifactPublisher.PublishImportWarp проверяет original SHA, обе canvas shapes/source identity и deterministic workflow/entry/original/geometry/target UUID; передаёт WarpOccurrence(original/from/to) в prepared scope/SQL receipt. DenseWarpRemapper использует occurrence publisher при его наличии; legacy injected publisher port сохранён. Ordinary receipt остаётся пяти-полевым, occurrence шестиполевой пока отклоняется существующей SQL01353 (не активирован обход authority). Требуется forward SQL extension с independent occurrence identity/origin validation и scoped catalog certificate reconstruction.

Unit test написан для exact certificate/ready replay, wrong derived UUID до reserve и отсутствия mutation caller publisher scope. Не запускался; сборки/typecheck/миграции не запускались. SQL extension, production runtime/admission wiring остаются открытыми; полный модуль не зачтён, процент реальной реализации не установлен.


### V30: forward SQL occurrence authority extension

01355 сохраняет plain scope helper и добавляет wrapper для шестиполевого DenseWarpField receipt. Wrapper проверяет exact occurrence/original shape, canvas<=200MP, original source SHA из parsed entry и unchanged binary SHA. Existing plain authority независимо проверяет original inventory/file/current workflow/source/metadata. Restricted JCS serializer SQL покрывает только пять Canvas fields с bounded integer/ASCII space/hex source; UUID binding fields закреплены plain helper. SQL independently derives occurrence hash/warp UUID и сохраняет occurrence в certificate. Reserve/complete definitions заменены для нового helper; scoped catalog реконструирует occurrence перед authority check. Quota/fence/ready logic сохранены. Physical NPY/schema qualification остаются обязательными.

Deferred integration test написан для SQL/Go canvas JCS byte parity и malformed canvas rejection. Не запускался; сборки/typecheck не запускались;01355 не применена. Полный positive reserve/complete/catalog occurrence integration ещё не доказан. Production runtime wiring и прочие operations открыты; полный модуль не зачтён, процент реальной реализации не установлен.

## Пересчёт и продолжение: сертификат Warp перед транзакцией (2026-10-10)

Повторно подсчитаны записи существующего каталога: 90 всего; 73 требуют семантической сверки, 14 частичных, 3 отсутствующих runtime-пути. 87/90 = 96,67% означает наличие кода, а не выполненные требования. Реальный процент всей реализации не установлен: полный знаменатель требований и их семантический зачёт пока не завершены. Этот пересчёт не является полным аудитом системы.

V30: общий валидатор occurrence-сертификата подключён к publisher и SQL store до транзакции. Проверяются контракт, UUID, SHA, размеры/лимит пикселей, orientation, space, lineage между canvas и UUID, производный от geometry/target/workflow. SQL независимо проверяет manifest и актуальную source lineage. Store копирует сертификат перед сериализацией. Добавлены unit-сценарии подмены identity/content/target/workflow, неверной геометрии и lineage. Тесты NOT RUN; миграции NOT APPLIED. Модуль V30 остаётся незавершённым.

## Продолжение: одинаковые правила review для backend и worker

Статическая сверка обнаружила, что Go import gate отклоняет чужие candidate decisions и confirmed masks, а worker prepared Override проверял только source geometry. Добавлен обязательный worker gate до runtime callback: непустые accepted/rejected candidate IDs и любое историческое mask confirmation отклоняются. Данные не переписываются автоматически; неподтверждённые допустимые маски сохраняют параметры и проходят дальнейшую физическую квалификацию. Unit-сценарии покрывают оба вида решений, confirmed/unconfirmed mask, порядок относительно runtime callback и неизменность caller document. NOT RUN.

Остаются незакрытыми production runtime qualifier/activation, полные manual geometry/landmark semantics и сквозная проверка import/adoption/resource accounting. Это частичное продвижение V30, без зачёта целого модуля и без повышения процента.

## Продолжение: единый учёт чтения импорта

Добавлен task-wide ImportReadBudget: лимит JSON из SQL authority теперь общий для adapter и prepared recipe factory. Reader начисляет байты на каждом блоке, включая повторные чтения; бинарные массивы входят в итоговый bytes_read и общий физический лимит 128GiB, но не расходуют JSON quota. Metadata позволяет отклонять заведомое превышение до открытия stream. Production factory реализует build_with_read_budget; adapter отклоняет установленную фабрику без этого порта, вместо неучтённого исполнения.

Unit-тесты написаны для повторных чтений, границ, некорректных charges, binary accounting, отказа до stream и общей квоты factory/adapter. NOT RUN. Миграций в этом шаге нет. Production runtime qualification, активация и полная приёмка V30 остаются незавершёнными; процент продукта не повышен.

## Продолжение: подготовка frozen export bundle

Добавлен prepare_export_bundle: копирование плана/refs, direction/format gate, точное сопоставление content и recipe с manifest inventory, обязательный квалифицированный closure каждого рецепта и canvas/type qualification масок. Незаявленные/лишние файлы, отсутствующие recipes, casefold collisions, подмена SHA и превышение file/expanded/tenant archive limits отклоняются до source IO. Все mask binaries требуют manifest mask descriptors. PreparedExportBundle.write подключает существующий deterministic streaming ZIP writer; выдаёт callback owned refs и отклоняет изменение prepared identities.

Unit-сценарии написаны для детерминизма, копирования refs, некорректных inventory/closure/format/quota и подмены подготовленного результата. NOT RUN. Это подготовительный компонент, не реализованный целиком export runtime: отсутствуют concrete qualification, SQL admission, leased adapter/dispatch, XMP mapping и adoption. Статус entrypoint и процент не повышались.

## Продолжение: frozen XMP sidecars и fidelity report

Добавлен prepare_export_sidecars для xmp/captureone_sidecar. Из квалифицированного encoded image evidence берутся exact content identity, bytes, MIME, EXIF orientation. Capture One subset требует TIFF MIME и .tif/.tiff; proprietary форматы не имитируются. Rating/pick/orientation/crop сериализуются существующим export_xmp, recipe и RAW development явно получают unsupported в MappingEntry. Проверяются коллизии image/sidecar paths с casefold, duplicate artifact IDs, file/expanded quotas; callback получает owned entry.

Написаны unit-сценарии всех EXIF orientations, deterministic output, mapping fidelity, неверных metadata/identity/quota, конфликтующих sidecar paths и callback mutation. NOT RUN. Остаются concrete image qualification, archive/report/root publication, SQL admission, runtime dispatch, adoption/recovery. Целый V30 не зачтён, процент не повышен.

## Продолжение: публикация export report/root

Добавлен publish_export_result: обязательный bundle receipt с SHA физически проверенного архива, точные entry/external/version identity в MappingEntry, запрет proposed_document для export. MappingShard/MappingReport публикуются в существующем формате с общей report quota; каждый receipt проверяется по SHA и alias относительно входов/bundle/предыдущих outputs. Корень содержит bundle и report, draft_shards=[] и imported_files=[]. Publisher получает отдельную копию root document; mutation не меняет зафиксированные bytes.

Unit-тесты написаны для root shape, JSON accounting, callback mutation, неверного bundle SHA, mapping identity/proposal, report quota и input aliases на каждом этапе. NOT RUN. Нет утверждения о production export runtime: qualification, sidecar packaging, SQL admission/dispatch и adoption/recovery ещё требуются. Процент и статус целого модуля не повышены.

## Продолжение: sidecar ZIP64 packaging

Добавлен write_sidecar_archive: deterministic packaging qualified encoded images, inline XMP и normalized manifest. Manifest recipes=null не заявляет перенос полного рецепта; XMP inventory UUID детерминирован от entry identity и content SHA, используется только внутри архива. Plan/sidecar metadata и exact image identity/path проверяются до открытия изображения; source callbacks получают fresh refs. Публикация bundle ещё не выполняется этим компонентом.

Использован существующий streaming ZIP writer: SHA/size, sorted paths, timestamps, compression и archive/file/expanded limits. Unit-сценарии описывают детерминизм, verify_archive+manifest roundtrip, изменённые metadata/identity/quota и corrupted source bytes. NOT RUN. SQL admission, runtime adapter/dispatch и export adoption/recovery остаются незавершёнными.

## Продолжение: CPU export adapter

Написан execute_export/operation factory: CPU/profile gate, Params SHA, mandatory admission+preparation installation, declared InterchangePlan с project/snapshot binding, shared JSON/input budget, bounded stream adapter, scratch archive reservation/build/hash, bundle upload и report/root publication. Повторные authority и pinned metadata проверки выполняются перед и после uploads; output quota и metrics включают bundle/JSON.

Unit-сценарии пока описывают wrong profile/installation и stream boundary/EOF verification; NOT RUN. Полный positive adapter scenario ещё требуется. SQL admission, concrete preparation factory, prepared bundle/plan correspondence, production dispatch, adoption/recovery не завершены. Наличие адаптера не означает работоспособный production export; целый entrypoint не зачтён.

## Продолжение: полная привязка prepared export к frozen plan

Подготовленные bundle/sidecar содержат plan SHA, вычисленный по canonical full InterchangePlan. Adapter проверяет pin и для bundle независимо сверяет manifest entries, image/recipe UUID+SHA, метаданные, path inventory/contracts, canonical manifest и лимиты. После сверки возвращается owned inventory, исключающий mutation через исходный результат factory во время subsequent authority callbacks. Sidecar archive также проверяет full plan pin.

Написаны unit-сценарии замены project/snapshot/binding/rating/path/recipe, manifest metadata, contract и лимитов, а также мутации исходных refs после binding. NOT RUN. Concrete qualification/SQL admission/dispatch/adoption/recovery и полный positive adapter scenario всё ещё открыты; module credit не присвоен.

## Продолжение: full positive export adapter unit scenarios

Написаны unit-сценарии orchestration для всех трёх форматов: actual ZIP writer, scratch API fixture, version-pinned reader, bundle→mapping shard→report→root, exact bytes_read/bytes_written. Qualification/admission в этих сценариях инъектированы и не доказывают production authority. Добавлены случаи bundle SHA mismatch и revoke после bundle upload: дальнейшая публикация запрещена.

В runtime добавлено полное readback verify_archive и bounded manifest verification перед bundle upload. Это production-код проверки, не выполненная проверка текущей сборки. Все новые unit-тесты NOT RUN. Concrete SQL admission/preparation qualification, dispatch и adoption/recovery остаются незавершёнными.

## Продолжение: concrete sidecar image preparation

Добавлены SidecarExportPreparationFactory и qualify_export_image. Factory читает declared WorkingImage, проверяет canonical SHA, canvas binding, float32 work Rec2020 gamma24 и installed ICC pin. Encoded image читается bounded/version-pinned reader в отдельную scratch reservation; проверяются declared EncodedImage contract, MIME, physical magic/suffix, metadata/ICC и полный raster decode. После native IO повторяется input evidence.

Existing asset.verify_decode делегирует прежний 512×512 decode loop общей verify_raster_decode; алгоритм и качество обработки не изменены. Новый exporter не вводит ingest-лимит 512MiB для encoded export: размер ограничен task/input/scratch quotas и pixel limit существующего decoder. Unit-сценарии используют mocked native decoder и не доказывают real-image qualification. NOT RUN. SQL source provenance/admission, bundle recipe/mask factory, dispatch, adoption/recovery остаются открытыми.

## Продолжение: worker SQL export admission client

Добавлен SQLExportAdmission и TaskDatabase.interchange_export_context. Transport передаёт tenant/run/task/attempt/fence/worker/params UUID/params SHA/canonical Params/input digest. Client до DB отвергает неправильный operation/resource/deterministic Params identity/hash; после DB строго проверяет field allowlist, UUID, digest и JSON/archive/output quotas. Callback result копируется до следующей active проверки.

Unit-сценарии написаны для immutable receipt, invalid field/value/identity/hash и всех десяти transport arguments. NOT RUN. Это только worker client: v30_interchange_export_ready ещё не реализована, export freeze/ledger также требуется. Подключение/auto-enablement отсутствует; SQL admission нельзя считать завершённым. Остальные незакрытые участки: bundle qualification, dispatch, adoption/recovery.

## Продолжение: server frozen export plan compiler

Добавлен CompileExportPlan из canonical SessionInputSnapshot: snapshot bytes SHA, повторное CompilePinnedSessionInputSnapshot/digest, deterministic task/plan/Params IDs, non-null exact bindings/portable recipes, path ordering, unique external/path casefold, metadata/file limits и typed artifact alias guards. InputRoots сохраняет snapshot, WorkingImage, исходный FrozenRenderRecipe, encoded image и переносимый EditingRecipeV3. Документы и refs выдаются как owned copies.

ExportFrameResolver обязателен и должен доказывать source provenance; конкретной SQL-реализации ещё нет. Unit-сценарии описывают deterministic/owned documents, invalid evidence до resolver и typed alias rejection. NOT RUN. Scoped publication, export ledger/admission/task seal, dispatch/adoption/recovery ещё требуются.

## Продолжение: typed export plan/Params closure

Добавлен ExportPlanDocumentValidator с обязательным внешним workflow scope: deterministic Params/plan UUID, exact plan ref/hash, project/snapshot, non-null binding/recipe. Общие canonical/path/metadata/quota/typed-alias проверки использованы из существующего plan validator; export content получает строгий EncodedImage role. Params возвращает только exact scoped InterchangePlan child.

Unit-сценарии написаны для ролей closure, Params identity, nullable binding/recipe и foreign project/snapshot. NOT RUN. Downstream encoded/portable recipe validators и authorized resolver, closure publication, ledger/admission, sealing/dispatch/adoption ещё не завершены.

## Продолжение: complete frozen export closure walk

Добавлен ResolveExportInputInventory для всех compiled roots, включая snapshot и frozen recipe provenance. Проверяются exact node identity, canonical bytes/SHA, child refs completeness, typed children plan/Params, cycle/alias, depth32, max entries и aggregate JSON128MiB. Только явные binary contracts допускают nil Body; JSON не маскируется под binary leaf. Inventory отсортирован и выдаётся как owned copy.

Unit-сценарии написаны для полного графа, unique resolution/ownership, omitted/mistyped children, JSON-as-binary и cancellation. Тестовый resolver не выполняет full schema/ACL validation: production resolver обязан квалифицировать каждый node независимо. NOT RUN. Concrete resolver, transport publication, ledger/admission/seal/dispatch/adoption остаются открытыми.

## Продолжение: export input transport publication

ExportInputInventory теперь сохраняет task identity и canonical entries digest. PublishInputs использует существующий BuildInputClosure/chunk512/manifest transport, сверяет count/digest, отклоняет aliases между input/chunks/manifest и выдаёт owned copies. Stable artifact IDs и scoped storage reservation остаются обязанностью конкретного publisher; новый компонент не активирует задачу.

Unit-сценарии написаны для точной публикации closure, owned refs, aliases, mutation и cancellation. NOT RUN. Scoped publisher, concrete resolver, export ledger/SQL admission/task activation, dispatch и adoption/recovery ещё требуются.

## Продолжение: export freeze recovery payload

Добавлен FreezeExportDocuments: exact canonical request/hash, deterministic workflow/task/Params identity, canonical snapshot/hash/digest и snapshot ID, plan/Params scope, format и полный состав version bindings. Payload сохраняет исходные canonical snapshot/plan/Params bytes и refs для будущего fenced ledger/recovery; выдаётся как owned copy. Его построение не даёт полномочий записи или task activation.

Unit-сценарии написаны для сохранности/ownership и cross-scope request/task/snapshot/root/format. NOT RUN. Persistent ledger/storage/migration ещё не реализованы; RLS/purge обход не добавлялся. Resolver, SQL admission/task seal/dispatch и adoption/recovery остаются открытыми.

## Продолжение: persistent export pending ledger/recovery

Добавлен PinExportPending на existing idempotency_records route Interchange.Export.Pending: current snapshot ACL/download permission, actor/tenant key scope, FOR UPDATE, request conflict/expiry/status guards. Exact private frozen payload сохраняется JSONB; embedded canonical document strings остаются точными. Replay восстанавливает первоначальный payload. DecodeFrozenExportDocuments заново компилирует plan/Params из snapshot и сохранённого frame inventory, сравнивает все refs/bytes и повторяет freeze invariants. Это internal persistence, не публичная активация задачи.

Unit-сценарии написаны для exact recovery/replay и access/conflict/expiry/status rejection. NOT RUN. Service/API, ticket recovery, scoped object storage/materialization, authorized resolver, SQL worker admission/seal/dispatch и adoption ещё не завершены. Новая таблица или grant не добавлялись.

## Продолжение: pending export recovery/service

Добавлены RecoverExportPending и Service wrappers с tenant.WithTenant/Editor. Recovery читает private pending ledger после current snapshot/download ACL, проверяет request conflict/expiry/status и восстанавливает exact documents без resolver/object IO или DB writes. Pin/recovery используют общий strict actor/tenant payload decoder; pending не считается completed ticket. Request snapshot pointer берётся из owned intent.

Unit-сценарии написаны для read-only recovery и denied/conflict/expired/invalid body. NOT RUN. HTTP orchestration/ticket, scoped storage/materialization, concrete resolver, SQL worker admission/sealing/dispatch и result adoption/recovery ещё открыты.

## Продолжение: internal SQL export document scope

Написана forward-only migration01356: internal SECURITY DEFINER helper проверяет pending actor/tenant/request, current snapshot/download ACL, deterministic identity, existing draft run/workflow, purpose allowlist и exact frozen document bytes/ref/SHA. EXECUTE отозван у PUBLIC и всех прикладных ролей; storage/activation этот helper не включает. Новые таблицы и DELETE grants отсутствуют.

Deferred negative integration test описывает denied roles и missing-ledger/tenant-setting failure. NOT RUN, migration NOT APPLIED. Positive authority/storage ещё не подтверждены; draft materialization, reserve/complete/publisher, worker admission/seal/dispatch и adoption/recovery остаются открытыми.

## Продолжение: scoped export document reserve/complete

Написана migration01357 на existing backend_documents: reserve tenant storage quota, replay/resume/fence, complete после повторного ledger/scope/ACL/run check, immutable object version и exact bytes/SHA, atomic reserved→storage accounting. Replay дополнительно сверяет object key/MIME/output role. EXECUTE только photo_api; другие прикладные роли запрещены.

Уточнена ещё не применённая01356: export публикует новые plan/Params, существующий frozen snapshot переиспользуется; повторная публикация его artifact UUID исключена. Deferred negative integration test описывает privileges и absent scope. NOT RUN, migrations NOT APPLIED. Go publisher/store, draft materialization, resolver, admission/seal/dispatch и adoption/recovery ещё требуются.

## Продолжение: Go scoped export storage

Добавлены ExportStoragePublisher/SQLExportDocumentStore: publication новых plan/Params через reserve/complete01357, exact frozen document comparison, deterministic document UUID, canonical SHA/limits/purpose, replay без повторного PUT, immutable PUT recovery, проверка key/SHA/size/media/version до complete. SQL store выполняет editor tenant transaction; object IO вне транзакции.

Unit-сценарии написаны для upload/replay и неверных purpose/identity/body/object receipt. NOT RUN; SQL migrations не применялись. Draft materialization, service orchestration, resolver, transport publisher, admission/seal/dispatch и adoption/recovery ещё открыты.


### 2026-10-10 — пересчёт и export draft containers

Реальный процент полной реализации не установлен: отсутствует завершённая сверка всех нормативных требований с кодом. Каталог: {'requires_semantic_review': 73, 'partial_runtime_path': 14, 'missing_runtime_path': 3}. 87/90 (96,67%) — только верхняя граница охвата точек входа кодом, не завершённость продукта.

Добавлены forward-only миграция 01358 создания/replay draft export run/workflow с повторной проверкой pending ledger и текущего доступа, Go-компиляция config/caller и unit-тесты. Используется существующий snapshot; задачи не активируются. Валидация scope после создания при отказе откатывает вставки внутри SQL exception block. Тесты НЕ ЗАПУСКАЛИСЬ; миграция НЕ ПРИМЕНЯЛАСЬ. Целый модуль V30 не засчитан. Остаются orchestration, concrete resolvers, transport publication, admission/sealing, dispatch/adoption/recovery.


### 2026-10-10 — atomic export draft service

Добавлен PrepareExportDraft: pending evidence и draft run/workflow создаются в одной tenant Editor транзакции. Replay возвращает исходные frozen документы; ошибка SQL/commit не выдаёт draft. Написаны unit-тесты scope/replay/denied/stale/DB failure и интеграционный тест прав SQL/отсутствующего ledger без создания run. Тесты НЕ ЗАПУСКАЛИСЬ. Положительный SQL lifecycle и rollback ещё требуют покрытия. Весь V30 не завершён; общий процент не повышен. Следующие этапы: concrete resolvers, HTTP orchestration, scoped transport publication, admission/sealing, production dispatch, adoption/recovery.


### 2026-10-10 — scoped export transport binding

Добавлен PublishScopedExportInputs на базе существующего v2input.ScopedPublisher. Перед публикацией проверяются frozen request identity, task, присутствие всех compiled roots, уникальность IDs и digest inventory. Storage получает tenant/run/task/assembly lease. Написаны unit-тесты отказов и передачи scope. НЕ ЗАПУСКАЛИСЬ. Тестовый root inventory синтетический, не доказывает полную физическую qualification. До production вызова нужны blocked task materialization, concrete resolvers и orchestration; затем admission/sealing/dispatch/adoption/recovery. Процент всего продукта не повышен.


### 2026-10-10 — export DraftTask compiler

Добавлен CompileExportTask: frozen Params, полный набор compiled roots, sorted unique inventory и digest, CPU/v2.2 профиль с VRAM=0, safe-integer bounds RAM/scratch/output и protocol floor archive+16MiB для output. Оценки поступают от доверенного preparation/qualification слоя, не HTTP; floor не доказывает native resource qualification. SQL должен независимо проверить оценки. Написаны unit-тесты binding/profile ownership/invalid resources/inventory/cancellation; НЕ ЗАПУСКАЛИСЬ. Task materialization/activation этим изменением не реализованы. Общий процент не повышен.


### 2026-10-10 — sidecar factory authority and ownership

SidecarExportPreparationFactory теперь копирует canonical InterchangePlan до callbacks и проверяет ExportAuthority: direction export, project, snapshot digest и archive bound до чтения master/image. Написаны positive XMP/CaptureOne тесты master qualification/mapping/plan mutation isolation и negative project/snapshot/archive/direction/contract/canvas/ICC/hash тесты. НЕ ЗАПУСКАЛИСЬ. Physical image qualifier замокан; это не evidence native decode или provenance encoded image. SQL materialization/qualified admission, concrete server resolvers, bundle factory, HTTP/dispatch и adoption/recovery остаются незавершёнными. Общий процент не повышен.


### 2026-10-10 — shared import/export recipe qualification

PreparedImportRecipeFactory получил build_for_plan для import/export с общим reader и aggregate JSON budget, canonical copy plan, schema/project/snapshot authority guard до IO. Существующая семантика recipe и физическая проверка arrays переиспользуются без изменения алгоритмов. Import путь делегирует общему методу. Обновлены import unit fixtures, добавлены export shared-budget/authority unit cases. НЕ ЗАПУСКАЛИСЬ. Bundle inventory/masks и production integration остаются незавершёнными; целый модуль не засчитан.


### 2026-10-10 — qualified bundle inventory

Добавлен compile_qualified_export_inventory: recipe hash/canonical validation, обязательная успешная closure qualification, inventory только из qualified_document_refs receipts + encoded images. Детерминированные пути, typed declaration/size guards, alias/file-count checks, owned immutable closure refs. Unit-тесты используют настоящий closure walker со stub semantic/binary ports и проверяют исключение unrelated input, ownership, отказ qualification/type/quota/hash. НЕ ЗАПУСКАЛИСЬ. Bundle masks/factory и production integration не завершены; общий процент не повышен.


### 2026-10-10 — bundle masks and preparation factory

Добавлены compile_export_masks и BundleExportPreparationFactory. Factory соединяет shared recipe qualification, inventory из receipts, физическую проверку encoded images, exact MaskRef/binary witnesses и prepare_export_bundle. Manifest masks требуют точного canvas hash; отсутствующие witnesses/refs/type конфликт отклоняются. Написаны unit-тесты mask declaration и отказов + installation guard; НЕ ЗАПУСКАЛИСЬ. Positive full factory orchestration coverage/native execution ещё требуются. SQL materialization/admission и production server/dispatch/adoption остаются открыты; процент не повышен.


### 2026-10-10 — concrete export installation composition

Добавлены ExportPreparationFactory (bundle/xmp/captureone routing), resolve_interchange_export_installation и sql_prepared_export_installation. Explicit installation требует полных admission/preparation ports; parser enablement не включает export. Unit-тесты format routing/plan ownership/installation guards и bundle orchestration с настоящим manifest preparation, но mocked qualification, написаны; НЕ ЗАПУСКАЛИСЬ. Production dispatch/SQL admission/HTTP/server resolvers/adoption остаются открыты. Whole-module credit не присвоен.


### 2026-10-10 — pinned export task preparation

CPU export task profile теперь содержит validated contracts_sha и max_input_json_bytes для shared recipe factory. Добавлен PrepareExportTask: validate/compile перед IO, publication plan→Params, exact receipts, затем materializer; owned callback buffers и intent snapshot. Unit-тесты pin/order/publication failure/receipt mismatch/ownership написаны, НЕ ЗАПУСКАЛИСЬ. SQL materializer пока interface, SQL admission и production wiring отсутствуют. Whole-module credit не повышен.


### 2026-10-10 — export blocked materialization

Добавлена forward-only миграция 01359 materialize_v30_interchange_export: pending scope, draft run locks, ready plan/Params physical receipts, exact task/profile shape, protocol resource floors, output quota reservation и replay. Создаёт только blocked task; native qualification/sealing/admission остаются обязательными незавершёнными gates. SQLExportTaskMaterializer независимо recompiles task и вызывает SQL в tenant Editor TX. Написаны privilege/absent authority/no task-or-quota integration test и missing DB guard; НЕ ЗАПУСКАЛИСЬ. Миграция НЕ ПРИМЕНЯЛАСЬ. Positive SQL lifecycle/concurrency/rollback coverage ещё нужно. Whole-module credit не присвоен.


### 2026-10-10 — export closure registration

Добавлен ExportTransport.RegisterInputs: проверка frozen task/compiled roots, owned inventory digest/count и transport receipts перед shared scheduler registration. Direct roots Params/Plan/Snapshot; descendants регистрируются полным inventory. Helper seals только task inputs, не graph. Caller обязан rollback на ошибке. Unit-тесты exact roots/receipt mismatch/SQL seal refusal написаны; НЕ ЗАПУСКАЛИСЬ. Generic SQL compatibility и positive lifecycle требуют integration coverage. Dedicated graph admission/native qualification/worker SQL admission/HTTP/dispatch/adoption ещё открыты. Whole-module credit не повышен.


### 2026-10-10 — export execution preparation orchestration

Добавлен ExportExecutionPreparation: публикация plan/Params → closure resolution → task compilation/materialization → scoped transport → registrar. SQLExportInputRegistrar сверяет tenant/actor/key identity и регистрирует всё в scheduler tenant TX. Нет object IO внутри TX; graph не seals и ticket не выдаётся. Написаны dependency/cancellation/missing DB guards; НЕ ЗАПУСКАЛИСЬ. Full positive orchestration/failure matrix и SQL compatibility ещё нужны. Concrete resolvers/qualification/admission/HTTP/dispatch/adoption открыты; whole-module credit не повышен.


### 2026-10-10 — full export orchestration unit scenarios

Написаны positive stage sequence, callback mutation isolation и failure matrix plan/Params/resolver/materializer/transport/registrar. Используются настоящий compiler/closure walker/transport builder с synthetic document nodes и stub IO/SQL. CreateIntent перед callback теперь owns request pointers и JCS bytes, включая PrepareExportTask materializer. НЕ ЗАПУСКАЛИСЬ. Это не production/native qualification evidence. Concrete resolvers/admission/calibration/HTTP/runtime/adoption остаются открыты. Общий процент не повышен.


### 2026-10-10 — export immutable typed JSON reader

Добавлен ExportJSONNodeResolver, переиспользующий bounded ImportJSONNodeResolver через export catalog/typed validator bridges. Обязательны immutable physical SHA/canonical checks и post-validation current catalog recheck. Binary contracts отклоняются до IO. Unit cases valid/hash/version/media/binary/cancelled написаны; НЕ ЗАПУСКАЛИСЬ. SQL catalog и full schema/source validator installation ещё отсутствуют; это reader component, не законченный production resolver. Whole-module credit не повышен.


### 2026-10-10 — export binary physical reader and router

Добавлены ExportBinaryNodeResolver/ExportNodeResolverRouter. Streaming buffer256KiB, bytes+SHA guard, encoded MIME TIFF/JPEG/PNG vs octet-stream arrays, mandatory physical qualifier, post-qualification exact catalog recheck. Unit cases image/array/hash/size/media/version/qualification/missing-port/contract написаны; НЕ ЗАПУСКАЛИСЬ. Physical qualifier пока required interface; concrete native/source qualification и SQL catalog ещё нужны. Общий процент не повышен.


### 2026-10-10 — scoped SQL export read catalog

Добавлена миграция 01360 v30_interchange_export_document и SQLExportDocumentCatalog. Проверка pending/frozen/draft/current snapshot ACL; plan/Params exact backend receipts; прочие inputs требуют project linkage или exact frozen snapshot artifact. Только photo_api EXECUTE, Go Editor tenant TX и bucket/object metadata guards. Read authority не доказывает requested contract/encoded provenance. Privilege/absent scope/missing DB tests написаны, НЕ ЗАПУСКАЛИСЬ; миграция НЕ ПРИМЕНЯЛАСЬ. Positive lifecycle/typed+physical validators/frame resolver/admission/production integration открыты. Whole-module credit не повышен.


### 2026-10-10 — scoped export roots and resolver installation

NewExportScopedDocumentValidator проверяет exact frozen Plan/Params/Snapshot; Snapshot recompiles digest и children WorkingImage/FrozenRenderRecipe с alias guards. Остальные JSON требуют отдельный dependency validator. NewSQLExportResolver соединяет catalog/readers/router только при mandatory dependency/binary qualifier installation. Root unit tests написаны; НЕ ЗАПУСКАЛИСЬ. Общие semantics/native qualifier implementations, frame lineage/admission/production integration ещё открыты. Whole-module credit не повышен.


### 2026-10-10 — portable export recipe root validator

Добавлен ExportRecipeDocumentValidator с frozen recipe/source binding registry, существующей ValidateEditingRecipeV3, explicit child roles WorkingImage/StyleV2/CorrectionPlan/LayerStack/MaskRef/Override, отказом foreign learning и role aliases. Остальные документы требуют mandatory delegated validators. Unit valid/binding/learning/alias/hash cases написаны, НЕ ЗАПУСКАЛИСЬ. Это recipe-root component, не полная schema/runtime/native qualification. Whole-module credit не повышен.


### 2026-10-10 — export canonical WorkingImage validator

Добавлен ExportWorkingDocumentValidator: frozen snapshot master ref/canvas witness, existing ParseWorkingImage/transform checks, float32/work_rec2020_gamma24_v1/installed ICC pin. Explicit child roles WorkingPixels/WorkingAlpha/DenseWarpField и alias refusal. Unit valid/canvas/ICC/identity/hash/delegate cases написаны; НЕ ЗАПУСКАЛИСЬ. Physical arrays/native/source encoded lineage и остальные dependency validators остаются обязательными незавершёнными gates. Whole-module credit не повышен.


### 2026-10-10 — frozen render export validator

Добавлен ExportFrozenDocumentValidator с exact snapshot recipe/source binding registry, existing DecodeFrozenRenderRecipe/DocumentChildren, ICC/model pins и typed dependency alias guards. Learning/background refs сохраняются; catalog allowlist расширен для project-linked CandidateProfile/DescriptorShard/StyleBackgroundAsset. Это read authority, не consent/runtime approval. Unit valid/binding/ICC/model/hash cases написаны; НЕ ЗАПУСКАЛИСЬ. Миграция 01360 НЕ ПРИМЕНЯЛАСЬ. Remaining dependency/native qualification/frame lineage/admission/production integration открыты. Whole-module credit не повышен.


### 2026-10-10 — typed export dependency context bridge

ExportDependencyDocumentValidator использует existing DocumentChildren для LayerStack/Params/CorrectionPlan/MaskRef/learning documents. Mandatory semantic qualifier получает deep-owned LayerRef context hints; canonical/SHA/alias guards обязательны. Style/Override/background и unsupported contracts требуют отдельный validator. Unit semantic refusal/missing/hash/context ownership cases написаны; НЕ ЗАПУСКАЛИСЬ. Concrete semantics/runtime/consent qualification ещё не реализована полностью. Whole-module credit не повышен.


### 2026-10-10 — fixed export validator composition

Добавлен NewExportDocumentValidators: фиксированная цепочка roots→WorkingImage→FrozenRenderRecipe→EditingRecipeV3→typed dependencies. Semantics и Other(schema для Style/Override/background и unsupported contracts) обязательны. Конструктор проверяет ICC/model pins и frozen payload через existing validators. Unit positive root routing/missing dependencies/invalid pins/corrupt frozen/cancellation cases написаны; НЕ ЗАПУСКАЛИСЬ. Composition не заменяет concrete schema/runtime/native qualification. Whole-module credit не повышен.


### 2026-10-10 — worker export profile/admission budget binding

execute_export owns copied profile before callbacks; validates contracts_sha, safe positive RAM/scratch/output, VRAM0, JSON budget1..128MiB and RAM floor before IO. Admission max_input_json_bytes must equal profile; archive/output fit admitted task estimates. Unit profile failure matrix и admission JSON/scratch/output mismatch-before-reader cases написаны; НЕ ЗАПУСКАЛИСЬ. Это protocol guards, не measured native qualification. SQL admission/production integration остаются открыты. Whole-module credit не повышен.


### 2026-10-10 — worker export SQL lease admission

Добавлена forward-only 01361 v30_interchange_export_ready, worker-only EXECUTE: worker principal/lease/fence/slot/deadline, sealed closure, exact frozen Params/Plan/direct typed roots, auth-session/current snapshot ACL, ready immutable artifact pins, quota/profile protocol bounds. 01359 сохраняет export proof на новом task materialization. Unit integration privilege/absent lease cases написаны, НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ. Native qualification/dedicated graph admission и positive SQL lifecycle/concurrency/revocation coverage остаются открыты. Whole-module credit не повышен.


### 2026-10-10 — explicit export worker runtime dispatch

build_worker/run_worker принимают explicit interchange_export_installation; CPU/backend guard, installation validation и export operation registration подключены к Executor. Unit non-CPU rejection и forwarding cases написаны, НЕ ЗАПУСКАЛИСЬ. Пересчёт статусов текущего каталога: {'requires_semantic_review': 73, 'partial_runtime_path': 14, 'missing_runtime_path': 3}. 87/90=96.67% остаётся только code-presence ceiling. Достоверный whole-product implementation percent не определён без полного нормативного denominator и semantic audit. Dedicated graph admission/native qualification/HTTP orchestration остаются открыты; whole-module credit не повышен.


### 2026-10-10 — export startup validation and dispatch unit coverage

Explicit export installation проверяется до TLS/DB/native constructors; malformed ports отклоняются WorkerConfigurationError. Написаны unit tests real build_worker/Executor/operation callback routing, absence without explicit injection, role allowlist и rejection-before-IO. Unrelated operation constructors/network заменены stubs. Тесты НЕ ЗАПУСКАЛИСЬ, production readiness не подтверждена, whole-module credit не повышен.


### 2026-10-10 — export result JSON inventory validation

ValidateExportResultDocuments сверяет canonical/hash-pinned InterchangeResult→MappingReport→MappingShard с frozen plan: exact sorted identities/versions, deterministic chunks≤100, totals/status reasons, unique fields, no proposals/imported/drafts, JSON quota, output aliases against direct frozen roots. Возвращает owned references без DB mutation. Positive/refusal/cancellation/ownership unit cases написаны, НЕ ЗАПУСКАЛИСЬ. Это JSON receipt validation, не physical ZIP proof, full input alias closure, task-scope/current ACL adoption или completed ticket. Dedicated final adoption/HTTP orchestration остаются открыты; whole-module credit не повышен.


### 2026-10-10 — immutable export output JSON reader

ExportResultReader читает root≤64KiB и report+shards≤8MiB, bounded cancellable reads, exact SHA/size/canonical schemas, strict output ref uniqueness. После ValidateExportResultDocuments повторно проверяет metadata всех прочитанных JSON. Dedicated ExportOutputCatalog должен доказать exact task/attempt/current ACL, draft input catalog не подходит. Unit positive/all-pin recheck/revocation/version/hash/missing ports/cancellation cases написаны, НЕ ЗАПУСКАЛИСЬ. Concrete SQL output catalogue, full input alias closure, physical ZIP и atomic adoption остаются открыты. Whole-module credit не повышен.


### 2026-10-10 — canonical export output SQL catalogue

Добавлена forward-only 01362 v30_interchange_export_output (photo_api-only EXECUTE), scoped pending/frozen identity/current snapshot+project download ACL, sealed active/succeeded run, succeeded export task canonical result attempt/fence, exact outputs membership+role, immutable verified object and full v2_task_inputs alias exclusion. Root должен быть first canonical output. SQLExportOutputCatalog подключает эту функцию отдельно от draft input scope, общий Go metadata guard сохранён. Missing DB/contract и integration privilege/absent authority tests написаны, НЕ ЗАПУСКАЛИСЬ. Миграция НЕ ПРИМЕНЯЛАСЬ. Positive lifecycle/revocation/concurrency, native ZIP proof/atomic final adoption/completed ticket recovery/production orchestration ещё открыты. Whole-module credit не повышен.


### 2026-10-10 — bounded physical export bundle bytes

ExportBundleReader проверяет output InterchangeArchive metadata, frozen archive quota, direct-root alias refusal, SHA/length через streaming buffer256KiB, cancellation/oversize/short/no-progress и close handling. После IO повторяет current output catalog pin. Receipt доказывает physical bytes, НЕ ZIP members/manifest/native semantics и НЕ final adoption. Positive/hash/short/long/version/media/quota/no-progress tests написаны, НЕ ЗАПУСКАЛИСЬ. Concrete native archive qualification, atomic adoption/recovery/production orchestration остаются открыты. Whole-module credit не повышен.


### 2026-10-10 — concrete bounded ZIP inventory verification

VerifyExportZIPInventory реализует ZIP/ZIP64 EOCD+directory budget до archive/zip parser (≤64MiB metadata), file/path/casefold/order/timestamp/method/ratio/expanded limits, streaming CRC-checked SHA inventory без extraction. Export requires manifest.json. Positive/hash/unsafe path/duplicate/order/method/manifest/quota/CRC/directory/cancellation unit tests написаны, НЕ ЗАПУСКАЛИСЬ. Concrete ReaderAt object staging и manifest/source/native qualification/atomic adoption остаются открыты. Deflate level6 невозможно достоверно восстановить из ZIP stream; writer уже задаёт его. Whole-module credit не повышен.


### 2026-10-10 — export manifest frozen/physical binding

ValidateExportManifest проверяет canonical manifest SHA/size against physical ZIP inventory, exact listed files/IDs/hashes, sorted inventory и frozen entries image path/ref/metadata/crop. Bundle recipe refs exact; sidecars require deterministic UUIDv5 XMP inventory и no extra files/masks. Mask declarations require matching binary inventory and hash-shaped canvas witnesses. Unit positive and identity/hash/contract/metadata/missing/extra/mask/cancel refusals написаны, НЕ ЗАПУСКАЛИСЬ. Dependency contract/native semantics и actual XMP metadata не доказаны этим helper; staging/atomic adoption/production orchestration открыты. Whole-module credit не повышен.


### 2026-10-10 — concrete export archive staging pipeline

ExportArchiveReader соединяет bounded object SHA read→private temp file→ZIP/ZIP64 CRC+inventory→bounded manifest→frozen validation→current metadata recheck. Shared ExportArchiveScratch reserves physical bytes+1 across concurrent reads; close/remove выполняются на всех paths, reservation сохраняется при failed removal. Scratch directory должен быть dedicated/process-shared с deployment filesystem quota. Unit positive/hash/version/quota/ZIP failure/cleanup/owned manifest/shared reservation tests написаны, НЕ ЗАПУСКАЛИСЬ. Fixture XMP intentionally byte-only; actual XMP metadata/native dependency semantics и atomic adoption не доказаны и остаются открыты. Whole-module credit не повышен.


### 2026-10-10 — XMP subset semantics integrated into archive read

ExportArchiveReader проверяет каждый sidecar≤1MiB через strict generated XML subset: exact namespace/shape, no DTD/entities/unknown fields, duplicate refusal, frozen rating/pick, orientation1..8 и crop back to original axes с tolerance1e-12. Archive positive fixture теперь настоящий XML. Unit namespaces/ratings/pick/orientation/DTD/duplicates/crop cases написаны, НЕ ЗАПУСКАЛИСЬ. EXIF orientation из XMP всё ещё требует независимого encoded metadata witness; native dependency semantics/atomic adoption/production orchestration открыты. Whole-module credit не повышен.


### 2026-10-10 — independent encoded orientation witness

exportEncodedOrientation читает TIFF/BigTIFF IFD0, JPEG EXIF APP1 и PNG eXIf через bounded ReaderAt, rejecting invalid/duplicate orientation и bad offsets. Archive reader сверяет XMP orientation с exact hash-verified stored image member, без extraction. TIFF/JPEG orientations1..8 и invalid metadata tests написаны, НЕ ЗАПУСКАЛИСЬ; archive fixture теперь minimal TIFF header (не native raster qualification). Full image raster/ICC validation остаётся worker native gate, BigTIFF/PNG positive/concurrency lifecycle coverage ещё открыты. Bundle dependency semantics/atomic adoption/production orchestration открыты. Whole-module credit не повышен.


### 2026-10-10 — encoded orientation boundary coverage

Добавлены TIFF IFD/header/table bounds, nil/size guard и PNG eXIf CRC через bounded streaming32KiB. Написаны BigTIFF little/big-endian, PNG orientations1..8/CRC refusal и independent expected XMP crop rectangles для всех EXIF orientations. Тесты НЕ ЗАПУСКАЛИСЬ. Fixtures metadata-only, не полный image decode. Native raster/ICC semantics, bundle dependency proof, atomic adoption и production orchestration остаются открыты; whole-module credit не повышен.


### 2026-10-10 — portable bundle qualified source inventory

ResolveExportBundleSourceInventory ограничивает roots encoded images+portable recipes, проходит exact typed descendants. ValidateExportBundleSourceInventory сверяет весь manifest inventory с qualified receipt по ID/SHA/contract/deterministic paths, no extras/omissions. Исправлен общий walker: frozen export Params rules применяются только к exact plan/Params refs, nested operation Params должны проверяться typed resolver. Unit exact/digest/path/contract/missing cases написаны, НЕ ЗАПУСКАЛИСЬ. Current adoption-phase qualified resolver/catalog installation и immutable receipt persistence ещё не подключены; draft input catalog не годится после activation. Native semantics/atomic adoption/production orchestration открыты. Whole-module credit не повышен.


### 2026-10-10 — result-phase typed input catalogue

Forward-only01363 v30_interchange_export_result_input требует canonical succeeded result root через01362/current session+snapshot+download ACL и exact registered v2_task_inputs contract/ref membership. Verified immutable object/media/expiry guards; только photo_api EXECUTE. SQLExportResultInputCatalog и NewSQLExportResultResolver подключают отдельный post-worker read scope с mandatory typed/native qualifiers. Unit missing authority/cancel и integration privileges/absent scope cases написаны, НЕ ЗАПУСКАЛИСЬ; миграция НЕ ПРИМЕНЯЛАСЬ. Positive lifecycle/races, concrete qualified production installation/evidence persistence/atomic adoption открыты. Whole-module credit не повышен.


### 2026-10-10 — composed export result pre-adoption evidence

ExportResultPreparation соединяет SQLExportOutputCatalog→result JSON→archive SHA/ZIP/manifest/XMP→result-phase qualified bundle closure и exact source inventory. Повторное чтение result JSON после archive/closure IO. PreparedExportAdoption хранит owned canonical identity/request/frozen/root/outputs/object/version/manifestSHA/ZIPfiles/source inventory bytes и digest, без DB finalization. Unit missing installation/cancel/owned receipt/request identity cases написаны, НЕ ЗАПУСКАЛИСЬ. Evidence serialization fixture synthetic, не end-to-end qualification. Atomic SQL adoption/persistence/replay/HTTP и concrete qualified production installation открыты. Whole-module credit не повышен.


### 2026-10-10 — immutable export pre-finalization evidence persistence

01364 создаёт per-run immutable evidence table и API-only save: tenant/ledger/run/task locking, canonical output refs/roles/attempt-fence/current ACL, exact object version/bucket/bytes, typed source refs and exact replay. Go PreparedExportAdoption.Save проверяет owned JCS/digest/frozen request identity перед SQL; PrepareAndSave исключает object IO внутри SQL TX. Исправлен01362 terminal run status: completed вместо succeeded (task status остаётся succeeded). Unit scope/SQL refusal и integration privilege/absent-scope/no-write cases написаны, НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ. Сохранение evidence НЕ завершает run;01320 finalization barrier сохраняется. Atomic final adoption/links/inbox/replay/completed ticket/production qualifiers/HTTP открыты. Whole-module credit не повышен.


### 2026-10-10 — generic export adoption guard and tenant purge consistency

01365 блокирует generic adopt_v2_plan для export до dedicated verified commit; остальные operations идут через прежнюю цепочку, predecessor scheduler EXECUTE revoked. 01366 расширяет explicit tenant purge allowlist evidence и ранее отсутствовавшими ingest_outcomes, сохраняя01312 cleanup lease/deleting tenant/no-live-run preconditions. DELETE granted photo_fn_owner, API не получает прямой доступ. Integration privilege/purge installation tests написаны, НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ. Dedicated export final adoption/links/inbox/events/finalizer и completed ticket recovery открыты. Это закрытие bypass/cleanup gaps, не завершение export. Whole-module credit не повышен.


### 2026-10-10 — dedicated atomic export adoption

01367 adopt_v30_interchange_export повторяет immutable evidence save/current authority/receipt checks под locks, публикует exact output+portable-source artifact links, v2-adopt/v30-interchange-export markers и v2.task.adopted event/outbox в одной TX. Exact evidence replay возвращает success; generic export adoption остаётся blocked. Go PreparedExportAdoption.Adopt и PrepareAndAdopt подключают функцию после object IO. Unit dispatch/refusal и integration privileges/absent authority/no links/inbox/outbox/evidence tests написаны, НЕ ЗАПУСКАЛИСЬ; миграция НЕ ПРИМЕНЯЛАСЬ. Positive lifecycle/concurrency/rollback/current ACL coverage ещё открыто.01320 всё ещё удерживает successful interchange finalization: dedicated export finalizer barrier/recovery/completed ticket/production HTTP+qualifiers открыты. Whole-module credit не повышен.


### 2026-10-10 — export committed proof and V2 finalizer gate

01368 internal v30_interchange_export_committed проверяет single sealed export task, immutable evidence hash/canonical result/attempt/fence, v2+export markers/event digest и exact output/source ready artifact links. Scheduler finalize_v2_run снимает только01320 success barrier при proved export adoption; terminal events/quota release/cancel/failure остаются в existing pre-interchange chain. Integration privileges/missing proof/wrapper gate cases написаны, НЕ ЗАПУСКАЛИСЬ; миграция НЕ ПРИМЕНЯЛАСЬ. Positive full lifecycle/concurrency/revocation/rollback, completed ticket recovery/HTTP/production qualified installation открыты. Whole-module credit не повышен.


### 2026-10-10 — export adoption recovery without archive reread

01369 recover_v30_interchange_export требует current actor/session/snapshot/project download authority и dedicated committed proof, возвращает exact run/workflow/task/result/root/report/bundle/evidence digest. Status adopted при running и completed только при реально completed run. Не зависит от истечения pending idempotency ledger, но требует текущую готовность snapshot/inputs/outputs; object locators не возвращаются. Go strict canonical identity/ref/status decode и Service Editor wrapper. Unit adopted/completed/mismatched identity/status/alias/hash/null/cancel/missing DB и integration privileges/absent adoption tests написаны, НЕ ЗАПУСКАЛИСЬ; миграция НЕ ПРИМЕНЯЛАСЬ. HTTP/create ticket idempotency integration, retention semantics и positive lifecycle/races открыты. Whole-module credit не повышен.


### 2026-10-10 — export job resource/inspection projection and client stages

01370 extends get/inspect with export direction/format/report+bundle IDs; preserves import predecessor branch and current viewer/project ACL. Refs exposed only with dedicated adoption markers, ready canonical output links and download_allowed; no object locators. Format added to immutable evidence (01364 expected17keys, Go save exact frozen format), so adopted resource does not depend on pending-ledger retention. Client accepts exporting/finalizing, rejects mixed import collections/ingest/review and adds labels. mapping_artifact_id remains null until its projection semantics are resolved (root is not silently relabeled as mapping). Client state tests/integration privileges/absent scope cases написаны, НЕ ЗАПУСКАЛИСЬ; миграции НЕ ПРИМЕНЯЛИСЬ. Positive export/import coexistence/revocation lifecycle tests, creation/admission/production qualifiers и download orchestration открыты. Whole-module credit не повышен.


### 2026-10-10 — adoption revision and client snapshot consistency

01367 adoption теперь atomic revision+1 с safe-integer exhaustion guard; replay возвращается до bump. Client assertInterchangeSnapshot сверяет exact scope/revision/direction, rejects mixed import/export stage/review/collection state и partial report/bundle pairs. Finalizing требует adopted refs, exporting не принимает refs; completed допускает revoked output access (null refs). OwnerAPI применяет сверку после revision race retries. Unit snapshot/API boundary и integration migration revision guard cases написаны, НЕ ЗАПУСКАЛИСЬ. Full SQL concurrency/replay/revocation lifecycle остаётся открытым. Whole-module credit не повышен.


# Пересчёт реализации — 2026-10-10

Полный процент реализации: **не установлен**. Знаменатель всех нормативных требований и актуальная семантическая трассировка пока отсутствуют; старые PASS не использованы.

Повторно пересчитан каталог 90 точек входа: 73 требуют семантического ревью, 15 имеют частичный runtime-путь, 2 не имеют реализации runtime (SignExport и provenance.sign.execute, V31). Исправлен устаревший статус v2.interchange.export.execute: операция регистрируется через explicit installation, но полный production-контур ещё не закрыт.

88/90 = **97,78% — только верхняя граница наличия runtime-кода**, не готовность продукта и не завершённость требований. Подтверждённых этим аудитом полностью завершённых точек входа: 0/90; это отсутствие полного доказательства, а не отсутствие реализации.

Открыты: V30 создание/активация export-графа и production qualification, интеграция adoption и UI download; V31 signer; V29 advanced optics; V32 lifecycle/restore/capacity. V20–V28 требуют полной семантической сверки. Перечень не исчерпывающий.

Продолжение реализации: добавлена миграция 01371 с выдачей только принятого экспортного архива через текущие ACL, download_allowed, committed proof и immutable artifact pins. Run Outputs подключён к функции и использует существующую подпись version-pinned URL. Написаны проверки привилегий и отсутствующего scope. UI download и положительный сквозной lifecycle остаются открыты.

Тесты, сборка, typecheck и миграции не запускались по согласованному порядку implementation-first. Последующее Metal-тестирование: лимит VRAM 10 000 000 000 байт.


## V30: клиент скачивания экспортного архива, 2026-10-10

Добавлены readInterchangeDownload, OwnerAPI.interchangeDownload и кнопка completed export archive. Повторная проверка snapshot после выдачи URL, exact bundle identity, revision/run/scope, MIME/size/SHA/expiry, отмена при unmount. Архив не буферизуется в JS. Unit-тесты положительного случая, отзыва bundle visibility, смены revision, отмены и malformed receipts написаны, не запускались. DOM и SQL positive lifecycle не проверены; полной готовности V30 не заявляем. Общий процент требований остаётся неизвестным; 97,78% относится только к наличию runtime-путей каталога.


## V30: DOM-сценарии скачивания, 2026-10-10

Написаны семь сценариев экрана: completed download, отсутствие кнопки для pending/import, отказ API и refresh, отзыв видимости после выдачи outputs, двойной клик/unmount, смена job с поздним ответом, ошибка Linking и retry. Refresh очищает старую ошибку скачивания. Тесты не запускались; это authored coverage, не PASS.

По текущему коду ExportExecutionPreparation заканчивается регистрацией task inputs. Регистрация не означает seal run graph. Атомарная export activation с текущими ACL/snapshot, квалификацией closure/resource estimates, quota, ticket/replay остаётся открытой; parse activation не является доказательством export activation. Общий процент требований не установлен.


## V30: database authority перед активацией export, 2026-10-10

Миграция 01372 добавляет API-only v30_interchange_export_draft_ready с tenant-first locking, live ledger/current document scope, blocked draft/task shape, exact Params/Plan/Snapshot roots, closure, physical artifact readiness и quota reservation. CheckExportDraftAdmission связывает frozen identity и inventory digest до SQL. Написаны unit preflight/refusal/error/cancel/tamper и SQL privilege/absent-scope тесты. Не запускались; миграция не применялась. Это только database authority, не native resource qualification, seal, activation или публичный ticket. Общий процент реализации не установлен.


## V30: qualification preparation и seal guard, 2026-10-10

01373 блокирует generic seal для export, предшественник недоступен app-ролям. ExportActivationPreparation повторно разрешает typed closure, требует native/resource qualifier, сравнивает exact recompiled task и сохраняет owned private receipt. Написаны unit boundary/mutation/refusal/cancellation и SQL privileges/installation тесты; не запускались. Production resource qualifier и атомарный activation/ticket ещё не реализованы. Receipt не считается доказательством актуальной DB authority. Общий процент реализации неизвестен.


## V30: атомарная export activation, 2026-10-10

01374 связывает current tenant/project/snapshot authority, draft-ready inputs/quota и exact task profile с dedicated seal и ticket ledger в одной транзакции. PreparedExportActivation.Activate принимает private receipt; Service.ActivateExport обеспечивает rollback при ошибке. API DB role — доверенная граница compiler/qualifier; SQL не квалифицирует native bytes. Написаны unit primitive scope/tamper/cancel/refusal/replay и SQL privileges/installation тесты; не запускались, миграция не применялась. Production qualifier, HTTP create orchestration/recovery, positive lifecycle/concurrency/rollback остаются открытыми. Общий процент реализации неизвестен.


## V30: export creation ticket recovery, 2026-10-10

RecoverExportTicket проверяет live project/editor/download access и exact sealed run/workflow/task/backend proof, request hash, ticket expiry/status/resource и strict queued receipt. Не зависит от pending ledger или срока исходных artifacts; не утверждает завершение run. Service wrapper сохраняет tenant boundary. Написаны unit valid/absent/denied/conflict/expired/malformed/foreign/uncommitted/database/cancelled случаи, не запускались. HTTP create coordinator/production qualifier/SQL lifecycle остаются открытыми. Общий процент реализации не установлен.


## V30: export create coordinator, 2026-10-10

Service.CreateExport связывает authorized recovery → installed preparation → recovery → activation; при ошибке activation повторяет authorized recovery для concurrent/uncertain commit. Callback получает owned request. HTTP adapter направляет export в новый сервис; import parse сохранён. Fresh export требует ExportCreatePreparer, отсутствие даёт unavailable; ticket replay доступен независимо от preparer. Unit concurrency/refusal/cancellation случаи написаны, не запускались. Concrete production preparer/resource qualifier/main installation и сквозные SQL/HTTP тесты ещё открыты. Общий процент требований неизвестен.


## V30: композиция подготовки export, 2026-10-10

ExportCreationPreparation реализует ExportCreatePreparer: validates proposal → committed draft/ledger winner → scoped installation → document/task/transport publish/register → repeated typed/native/resource qualification → private activation receipt. Service реализует draft adapter. Incomplete installation отклоняется до object publication; каждый callback получает собственные mutable buffers/refs. Написаны stage failure/ownership тесты, не запускались. Production snapshot/frame proposal source и native/resource installation ещё открыты; production capability не включалась. Общий процент реализации неизвестен.


## V30: SQL/object execution installation, 2026-10-10

SQLExportExecutionInstaller связывает API scoped catalog/Plan+Params publisher, qualified resolver, scheduler transport publisher/registrar и activation preparation. SQLResolvedExportTaskMaterializer повторно разрешает closure после публикации документов и сверяет digest до SQL mutation. Construction без IO, mutable request/frozen buffers изолированы. Boundary tests написаны, не запускались. Production proposal/source semantics/native qualification/main wiring ещё открыты; capability автоматически не включалась. Общий процент реализации неизвестен.


## V30: compiler-backed proposal source, 2026-10-10

ExportProposalSource реализует frozen proposal compilation: scoped immutable snapshot input → exact SHA/canonical pinned snapshot ID → bounded CompileExportPlan → FreezeExportDocuments. Обязателен scoped frame resolver, ownership request/body сохранён. Тесты valid/mutation/hash/foreign snapshot/missing resolver/source error/limits/cancellation написаны, не запускались. Concrete SQL/object-store snapshot source и encoded/portable frame lineage, production qualification/main wiring ещё открыты. Общий процент реализации неизвестен.


## V30: SQL/object snapshot proposal input, 2026-10-10

SQLExportProposalInputs читает snapshot artifact через current project/snapshot authority, exact bucket/version, bounded canonical SHA JSON; object IO вне SQL transaction. После mandatory frame factory повторяет physical pin/current access и сверяет ordinal/version/source refs с snapshot_items. Preflight тесты написаны, не запускались. Positive SQL/object IO/revocation coverage, concrete encoded/portable frame factory, production qualification/main wiring остаются открытыми. Общий процент реализации неизвестен.


## V30: exact portable recipe lineage, 2026-10-10

ExportLineageFrameResolver связывает exact snapshot item, FrozenRenderRecipe SHA/source и portable EditingRecipeV3 с точными authored bytes внутри frozen recipe. Запрещены substitution/alias и silent dropping learning. Это не доказательство encoded physical/native lineage: mandatory evidence source обязан его дать вместе с current authority. Unit cases написаны, не запускались. Concrete SQL/object evidence source/native qualification/main wiring остаются открытыми. Общий процент реализации неизвестен.


## V30: collection metadata provenance, 2026-10-10

SessionInputSnapshot и selection payload не замораживают rating/pick. ReadExportFrameMetadata читает exact selected collection item при revision=snapshot.source_revision, exact ordinal/version/source и current ACL; plan должен затем заморозить значения. Flag mapping строгий, defaults не выдумываются. Non-collection metadata provenance остаётся открытым. Boundary tests написаны, не запускались. Frame evidence composition/encoded native lineage/production wiring ещё открыты. Общий процент реализации неизвестен.


## V30: SQL/object recipe evidence, 2026-10-10

SQLExportRecipeEvidenceSource читает frozen recipe через render scope и portable recipe через document stream той же asset version, current snapshot/project scope; проверяет bounded canonical SHA bytes, exact authored recipe и post-read immutable pins. Написаны physical-reader и metadata positive/refusal тесты, не запускались. Full SQL/object lifecycle, encoded provenance/native source и complete frame factory ещё открыты. Общий процент реализации неизвестен.


## V30: encoded original lineage, 2026-10-10

SQLExportOriginalSource связывает exact asset version/source/snapshot с TIFF/JPEG/PNG original и current scope, потоково проверяет physical SHA через existing binary resolver, требует native qualifier и повторяет pin lookup. Path deterministic/media-correct. Boundary tests написаны, не запускались. Это только encoded-original component; RAW/replayed encoded producer, CaptureOne TIFF conversion, complete frame evidence factory/native production installation ещё открыты. Общий процент реализации неизвестен.


## V30: SQL frame evidence factory, 2026-10-10

SQLExportRecipeEvidenceSource.Resolve находит same-version portable document по exact frozen SHA, не stream head, затем повторяет full read/pin validation. SQLExportProposalFrameFactory связывает recipes, encoded original/native qualifier, collection metadata и exact snapshot lineage resolver. Constructor boundary tests написаны, не запускались. Positive SQL lifecycle, RAW/replayed producer, CaptureOne conversion, non-collection metadata и concrete native/resource production installation остаются открытыми. Общий процент реализации неизвестен.


## V30: selected catalog roots before workflow linkage, 2026-10-10

Подтверждён integration gap: 01360 требовала преимущественно project links, которые отсутствуют у selected original до export adoption. Forward-only 01375 добавляет selected original/master authority, exact frozen snapshot recipe + render scope и same-version portable document stream. Existing pending/draft/current snapshot/project/pin guards сохранены. Это только read authority, typed/native validators обязательны. SQL installation/privilege tests написаны, не запускались, миграция не применялась. Positive lifecycle/cross-tenant/revocation и production qualification/wiring ещё открыты. Общий процент реализации неизвестен.


## V30: shared native file qualification, 2026-10-10

Worker использует qualify_export_image_file: physical SHA/size streaming, regular non-symlink file, magic/suffix/media, existing ICC/metadata/full raster decode, post-decode inode/size/mtime pin. Native quality filters сохранены. Mocked-native filesystem tests написаны, не запускались; они не доказывают installed native decoding. Go admission bridge и qualified runtime/resource binding ещё открыты. Общий процент реализации неизвестен.


## V30: native probe wire contract, 2026-10-10

Python local probe вызывает shared native decoder и выдаёт bounded object-bound receipt с Python/pyvips/libvips fingerprint, без paths/diagnostics. Go декодирует strict canonical receipt, сверяет object/runtime pin/pixel/ICC/type limits. Contract tests с mocked native написаны, не запускались; installed native qualification не заявляется. Go process/scratch bridge и independently qualified runtime/resource installation ещё открыты. Общий процент реализации неизвестен.


## V30: Go native process/scratch bridge, 2026-10-10

NativeExportImageQualifier staged immutable EncodedImage под shared scratch budget, streaming SHA/size, strict runtime-bound receipt и cleanup. PythonExportNativeProbe: explicit absolute executable/module root, no shell/inherited credentials, timeout/bounded stdout/stderr discard. Unit staging/refusal/cleanup tests с mocked probe написаны, не запускались. Real process/native environment qualification, arrays/masks/resource estimator и production installation ещё открыты. Общий процент реализации неизвестен.
