# Принятый порядок завершения разработки — 2026-10-17

Пользователь подтвердил ускоренный план как основной и явно разрешил параллельных агентов. Нормативные требования модулей implementation-v2 сохраняются.

1. Завершать кодирование целых функциональных модулей, одновременно писать необходимые модульные и интеграционные сценарии.
2. До завершения кодирования выполнять статическую сверку связей и контрактов. Глубокое тестирование и исправления по его результатам — следующая фаза. Авторство сценария не означает его успешное выполнение.
3. Параллельные владельцы: quota/reservation lifecycle; Lens и состояния экранов; Region execution и authority. Основной агент выполняет межмодульное ревью и idempotency. Не редактировать одновременно файлы другого владельца без согласования.
4. Сохранять качество обработки, исходные данные, ICC/RAW/alpha/protected pixels и tenant isolation. CUDA ограничена 10 000 000 000 байт VRAM; Metal без искусственного лимита 10 ГБ, с учётом фактической вместимости. При наличии совместимой квалифицированной Metal альтернативы использовать её; замены и недоступные операции описывать в README.
5. Прогресс считать по закрытым пунктам актуального реестра mvp-coding-progress.json. На момент принятия: 42/70 = 60% остаточного плана кодирования. Это не процент готовности релиза. Частичные изменения не закрывают целый пункт.

Все работы ограничены репозиторием PhotoEditor. Настоящая запись не подтверждает выполненные тесты, миграции, квалификацию GPU или готовность выпуска.

## Повторное подтверждение основного плана

Пользователь повторно принял план и разрешил параллельных агентов. Текущие владельцы: идемпотентность и транзакционная авторизация; клиентские состояния и восстановление операций; Region и совместимая обработка на Metal. Основной агент закрывает OTP и проверяет межмодульные связи. На момент повторного подтверждения закрыто 44/70 пунктов остаточного плана кодирования (62,857143%). Глубокие проверки не выполнялись; новые сценарии только написаны. Разрешение параллельной работы не меняет критерии завершения и не разрешает считать частичные изменения закрытым модулем.

## Текущее подтверждение и распределение работ

Основным остаётся ускоренный план: закончить кодирование целых модулей и написать необходимые тесты, затем перейти к глубокому тестированию и багфиксу. Пользователь явно подтвердил параллельных агентов. На текущем срезе реестра — 45/70 (64,285714%) пунктов кодирования; эта цифра не подтверждает готовность релиза.

Параллельные контуры: lifecycle задач, lease и выходных файлов; клиентские состояния и восстановление операций; Region replay и ресурсные ограничения CUDA/Metal. Основной агент отвечает за независимое ревью, межмодульные связи и согласование реестра. Новые дефекты записываются явно; при опровержении закрытого пункта он переоткрывается с пересчётом процента. Частичный результат агента не закрывает модуль. Все изменения выполняются только внутри этого репозитория.


## Latest acceptance and current authoritative metric

User again accepted the accelerated plan and explicitly authorized parallel agents.
Continue whole-module implementation with authored tests, followed by deep testing
and fixes. Independent source review remains required before coding credit.
Current authoritative residual metric: **43/70 = 61.428571%**, coding only, unverified.
Earlier percentages above describe historical snapshots; reopened quota and GC
findings account for the current lower figure. No tests/builds/migrations were run.
Parallel owners: task/output lifecycle and GC review; client recovery/screens; Region
execution. Parent owns cross-module acceptance and the authoritative progress register.

## Действующий основной план после последнего подтверждения

Последнее указание пользователя подтверждает ускоренный план как основной и разрешает параллельную работу агентов. Распределение независимых областей:

- основной агент: межмодульные связи, независимое принятие результатов и единый реестр прогресса;
- агент клиентских контрактов: полная сверка API/DTO, Lens/OpenEyes/Reflection и делегатов редактора;
- агент качества обработки: RAW/ICC/alpha, версия алгоритма производных изображений, совместимость cache/profile hash и исторических restore;
- агент жизненного цикла хранения: независимое ревью quota/reservation, GC, исторического учёта и физической очистки.

У каждого изменения один владелец. Найденные межмодульные зависимости согласуются до редактирования общих файлов. Частичные исправления не дают зачёта целому пункту. Текущий подтверждённый показатель остаётся **43/70 = 61,428571% кодирования остаточного плана**, до тестирования и багфикса.

Сначала завершаем реализацию и пишем необходимые тесты. Запуск тестов, сборок, миграций и аппаратной квалификации относится к следующей фазе. Сохраняются требования к качеству фотографий, мультитенантности, CUDA с лимитом 10 ГБ и Metal без искусственного лимита, с совместимыми альтернативами там, где они доступны.

## Latest authoritative coding acceptance

Parent accepted quota/lifecycle/GC P0.6.04/06/07 once after independent source review. Current metric **46/70 =65.714286%**, coding_closed_unverified only. Earlier figures are historical snapshots. Tests/builds/migrations were not run. Evidence: audit/storage-lifecycle-parent-acceptance-2026-10-17.md.

Current authoritative metric after whole role/tenant coding acceptance: **47/70 =67.142857%**, unverified coding only. Earlier snapshots remain historical; deep execution deferred.

## Повторное разрешение параллельной реализации

Основным остаётся принятый ускоренный план. Пользователь явно разрешил подключение параллельных агентов. Текущее распределение: API/DTO и делегаты редактора; RAW/ICC/alpha и сохранение исторической политики обработки; навигация всех экранов и переход к сохранённой версии. Основной агент согласует общие файлы, проверяет межмодульные связи и принимает целые пункты реестра.

Текущий показатель: **47/70 = 67,142857%** остаточного плана кодирования. Частичные изменения не увеличивают показатель. Сначала заканчиваем код и пишем сценарии, затем выполняем глубокое тестирование и багфикс. Ограничения качества, мультитенантности и совместимости CUDA/Metal сохраняются.

## Основной план и активные владельцы — текущий срез

Пользователь повторно подтвердил основной план и явно разрешил параллельных агентов. Действующий порядок: закончить реализацию целых модулей и написать тестовые сценарии; после завершения кодирования перейти к выполнению глубоких проверок и исправлениям. Написанные сценарии не считаются выполненными.

Текущий реестр: **49/70 = 70% остаточного плана кодирования**, до тестирования и багфикса. Предыдущие цифры в этом документе — исторические срезы; источник текущего показателя — `mvp-coding-progress.json`.

Активное распределение: основной агент — независимое принятие и межмодульные зависимости; агент клиентских состояний — loading/cancel/retry и восстановление каталогов/interchange; агент API — полнота фактических делегатов, DTO и capability gates; агент совместимости — SQL admission и CUDA/Metal capability/recovery редактора. Общие файлы редактируются только после согласования владельцев.

Выявленные разрывы, сохраняемые открытыми до исправления и source review: Apply должен получать требования выбранного стиля из серверного контракта; восстановление ранее принятой операции не должно блокироваться исчезновением worker; новые операции обязаны проходить действующий серверный admission. Отмена ожидания не означает отмену серверной операции и не удаляет durable receipt.

## Подтверждение основного плана и параллельной работы — актуальное распределение

Пользователь принял ускоренный план как основной и явно разрешил параллельных агентов. Основной агент принимает результаты по исходному коду и согласует общие файлы. Независимые области: составные capability-контракты Editor/Training/Apply/Capture; полная проверка preset metadata DTO и восстановление журнала; остаточные AI-трассы, replay/cancel/GC и решение фотографа. Владельцы общих файлов согласуют узкие изменения до записи.

Авторство новых сценариев продолжается одновременно с реализацией. Выполнение тестов, сборок, миграций и аппаратной квалификации остаётся следующей фазой. Текущий показатель не изменён: **49/70 = 70% остаточного плана кодирования**. Кандидат на закрытие принимается только после независимой сверки всей единицы; частичная готовность не увеличивает процент.

## Текущий показатель после принятия Editor

Независимая source-сверка переоткрытого Editor завершена: **50/70 =71,428571% остаточного плана кодирования**. Кодирование принято без заявления об успешном выполнении тестов. Текущие параллельные области: durable Capture create recovery; финальный инвентарь оставшихся AI; SQL admission inventory и его независимое принятие. Глубокие проверки остаются следующей фазой.

## Текущий показатель после принятия SQL admission

P0.2.07 принят по независимой сверке общих SQL-границ и полной карты семейств операций: **51/70 =72,857143% остаточного плана кодирования**. SQL и тесты не выполнялись. Параллельно завершаются durable Capture create, весь инвентарь состояний41 экрана и остаточные AI/replay/cancel/GC трассы. Требования V20–V32 и качество обработки не сокращены.

## Актуальное подтверждение и назначение владельцев

Пользователь повторно подтвердил основной план и явно разрешил параллельных агентов. Текущий учтённый показатель: **53/70 = 75,714286% кодирования остаточного плана**, без подтверждения тестированием.

Основной агент отвечает за миграцию 02551, завершение дочерних semantic scene операций и освобождение резервов ровно один раз. Параллельные владельцы: полная интеграция semantic transfer и миграция 02555; независимое статическое ревью её SQL/Go контрактов; клиентские модули Collection/Capture и восстановление операций. Общие файлы имеют одного согласованного владельца.

Сначала завершаются реализация и написание сценариев. Запуск тестов, сборок, миграций и аппаратная квалификация остаются следующей фазой. Узкие исправления и независимое чтение отдельных функций не закрывают целый модуль и не увеличивают процент автоматически.

### Уточнение активных параллельных задач

Подтверждение пользователя распространяется на текущую параллельную работу. Владельцы: composition_quality — полный ordinary parent → scene/depth → semantic transfer и восстановление принятой операции; lens_integration — Collection, история публикаций версий и её интеграционные сценарии; backup_shutdown — независимая сверка lifecycle, cleanup/refund и согласованности сценариев. Основной агент выполняет межмодульное ревью и принимает завершённые пункты реестра. Общие файлы передаются одному владельцу явно.

Ближайший подтверждённый разрыв: повторная публикация после замены версии должна создавать запись для новой версии, сохраняя неизменяемую историю прежней публикации. Требуются согласованные изменения ограничения уникальности, обычного и пакетного Publish, читателей и сценариев конкурентного доступа. До полного закрытия связанной единицы показатель остаётся **53/70 = 75,714286%**, только кодирование без выполненных проверок.

### Текущий показатель после принятия Collection

P0.4.03 принят по полной текущей карте функций, mounted-связям и независимой сверке исходного кода с написанными сценариями. Устранены разрывы отбора/групп, замены версии, обычной и пакетной публикации, удаления элемента с историей запросов и установки функций во всех трёх процессах. Основание: `audit/collection-coding-parent-acceptance-2026-10-17.md`.

Текущий показатель: **54/70 = 77,142857% остаточного плана кодирования**, прирост1,428571 п.п. P0.4 —10/10. Тесты, миграции и сервисы не запускались. Полная цель V20–V32 активна; semantic transfer, общие endpoint/screen/capability gates и дальнейшее глубокое тестирование остаются в охвате.


## Current coding acceptance:55/70

P0.2.10 consolidated105-operation capability trace accepted as coding_closed_unverified; see audit/capability-consolidated-parent-acceptance-2026-10-17.md. Current coding progress78.571428571428571429%, delta+1.428571428571428571 percentage points from54/70. Pool/signer expiry and unsupported semantic worker early selection corrections included. Separate full semantic, API/state/authority/adoption and hardware/quality criteria remain open. No tests/builds/generators/migrations/services executed.


## Current coding acceptance:56/70

P0.5.01 whole semantic current criterion map accepted coding_closed_unverified; see audit/semantic-whole-parent-coding-acceptance-2026-10-17.md. Current coding progress80%, delta+1.428571428571428571 percentage points; P0.5 is10/10. No tests/builds/generators/migrations/services or physical qualification executed. Full V20–V32 goal remains active.


## Current coding acceptance:57/70

P0.2.09 accepted coding_closed_unverified; see audit/client-availability-parent-coding-acceptance-2026-10-17.md. Current81.428571428571428571%, delta+1.428571428571428571 points. Whole-screen/Portal abandonment and endpoint contracts remain open. No execution or qualification.

## P0.7.09 — 58/70

Parent source acceptance: six accepted deferred activation families have explicit installation/request/consumer gates and authored scenarios; cancelled paid provisioner calls fenced. Coding only: 58/70 = 82.857143%, P0.7 3/10. No test/build/migration execution; full goal remains active. Evidence: audit/deferred-activation-parent-acceptance-2026-10-17.md.

## Mandatory Portal recovery installation

Added API startup check for02561 on the Portal role so old/overprivileged installations fail before serving the recovery action. Authored bounded/cancelled/error scenarios only; no executions. No additional coding-unit credit:58/70 remains82.857143%. Evidence: audit/portal-preparation-startup-boundary-2026-10-17.md.

## P0.3.08 —59/70

Whole route/nested state coding accepted after Portal receipt protocol, durable journal, corrected accepted wire shape and five nested AI lifetimes.59/70 =84.285714%; P0.3 7/10. No executions or release claim. Evidence: audit/screen-state-parent-coding-acceptance-2026-10-17.md.

## P0.7.03 and P0.7.05 —61/70

Corrected actual106-operation entry/authority trace accepted (59→60); current106 result/adoption/accounting trace and precise missing-dependency retry correction accepted (60→61).61/70 =87.142857%; P0.7 5/10. Coding/source only, no executions or quality qualification. Parent acceptance audits retained separately; full goal remains active.

## P0.7.04 —62/70

Whole106-operation/97-constructor worker dispatch coding accepted after delegated complete constructor review and independent parent high-risk/shared boundary review.62/70 =88.571429%; P0.7 6/10. No executions or quality/hardware qualification. Evidence: audit/operation-worker-parent-coding-acceptance-2026-10-17.md. Other endpoint/Run/UI criteria remain open.

## P0.7.06 —63/70

Whole106-operation+Bulk UI/recovery coding accepted after current source review and parent-discovered retained authority/culling draft repairs.63/70 =90%; P0.7 7/10. Authored mounted scenarios remain unexecuted. Evidence: audit/operation-ui-parent-coding-acceptance-2026-10-17.md. Endpoint/Run/packaging criteria remain open.

## P0.7.07 —64/70

Whole323-endpoint contract/caller coding accepted after canonical packaging, legacy/executable provenance, OpenAPI and current safe revision repairs.64/70 =91.428571%; P0.7 8/10. Manual artifacts are not generator-verified. Exact DTO/auth/service consolidation remains open. Evidence: audit/endpoint-contract-parent-coding-acceptance-2026-10-17.md. No executions.

## Принятое ускорение глубокого тестирования — 2026-10-18

Пользователь подтвердил порядок: точная диагностика отказа → исправление → полный затронутый функциональный модуль → полный регресс на стабильном snapshot. Быстрые contract/unit проверки отделяются от реальной БД, HTTP/UI, фото-корпуса и длительной нагрузки. Подготовленные среды и build caches переиспользуются при сохранении изоляции данных. На текущем Mac тяжёлые SQL/Go/Metal и полный UI прогон имеют одного владельца; независимые read-only ревью и отдельные документы выполняются параллельно. Внешний observation timeout не является причиной рестарта подтверждённо живого процесса.

Таймауты и authority/quality assertions не ослабляются ради ускорения. Отказы разбираются по конкретным установленным предикатам; fixture-only диагностические изменения в изолированной БД не засчитываются за приёмку и должны быть сняты перед полным повтором. Каждый итог сохраняет команду, exit/status, исходный snapshot, XML/log и cleanup. RTX остаётся отложенным до предоставления сервера, Metal без искусственного10ГБ cap. Это уточнение порядка исполнения не сокращает требования V20–V32.
