# Продуктовые правила и оптимистичные квоты

## Тарифы и seed

Источник чисел — существующий billing prelaunch plan от 18.10.2026. EUR, суммы в minor units без НДС. 20% — тестовый налоговый сценарий, не ставка для всех стран. GB=10⁹ bytes, TB=10¹² bytes; технические GiB явно подписываются отдельно.

| plan code | Фото | Storage bytes | Egress bytes за период | Net minor | Выдача |
|---|---:|---:|---:|---:|---|
| trial | 20 | 2000000000 | 20000000000 | 0 | Фото один раз; egress ежемесячно |
| start | 100 | 10000000000 | 100000000000 | 900 | Оплаченный месяц |
| photographer | 300 | 30000000000 | 300000000000 | 1900 | Оплаченный месяц |
| studio | 1000 | 100000000000 | 1000000000000 | 4900 | Оплаченный месяц |
| studio_plus | 2000 | 250000000000 | 2500000000000 | 8900 | Оплаченный месяц |
| studio_pro | 5000 | 500000000000 | 5000000000000 | 19900 | Покупка после capacity gate |
| custom | Явно | Явно | Явно | По договору | Только полное назначение |

Одна студия делит общие квоты между участниками. Trial без карты и без автоматического превращения в платный. Платный период определяется Stripe anchor, не календарным месяцем. До первой recurring subscription Trial egress использует UTC календарь. Переход на anchor закрывает неиспользованный Trial egress и выдаёт entitlement нового оплаченного периода; оплаченная строка не урезается вычитанием Trial. Trial egress уже использованный до перехода остаётся историческим расходом прежнего периода. Одновременно действует только один base egress grant; paid storage egress начисляется отдельно по своей invoice line. Переноса старого Trial egress нет.

| offer | Ресурс | Количество | Net minor | Тип и срок |
|---|---|---:|---:|---|
| photo_100 | standard_photo | 100 | 900 | Разово, 90×24 часа после подтверждённой оплаты |
| photo_500 | standard_photo | 500 | 3500 | То же |
| photo_1000 | standard_photo | 1000 | 6500 | То же |
| storage_100gb | storage_bytes | 100000000000 | 400 | Recurring, включает 10¹² egress/месяц |
| storage_500gb | storage_bytes | 500000000000 | 1600 | Recurring, включает 5×10¹² egress |
| storage_1tb | storage_bytes | 1000000000000 | 2900 | Recurring, включает 10¹³ egress |
| storage_2tb | storage_bytes | 2000000000000 | 5400 | Recurring, включает 2×10¹³ egress |
| egress_1tb | outgoing_bytes | 1000000000000 | 500 | Разово, до конца текущего периода |

Исходный seed: 7 plans, 6 стандартных price rows включая Trial=0, 15 photo offers для пяти платных планов, 24 storage offers для Trial и пяти платных, 6 egress offers. Всего 45. Custom скрыт, offers только явно назначенные. Предложения отдельных AI-операций расширяют этот каталог отдельной миграцией, а не меняют контрольную сумму исходного seed. Не создавать фиктивные платные operation offers до benchmark.

## Учёт фото и отдельных операций

`standard_photo` — первый успешный стандартный AI-результат исходника в tenant, доступный пользователю. Внутренние шаги, число лиц, формат экспорта, скачивание и технические retries не добавляют photo debit. Два успешных пользовательских AI-повтора в течение 7×24 часов от первого результата бесплатны; последующие успешные повторы расходуют по единице, новое платное списание не обнуляет окно. Бесплатные повторы также проходят safety/storage admission. Неудача не расходует успешный повтор.

Предлагаемая identity: tenant-scoped digest оригинальных загруженных bytes + алгоритм digest, проверенный ingest backend; project не входит в ключ. Повторный импорт тех же bytes в другую коллекцию/проект tenant не открывает новые бесплатные повторы. Изменённые bytes — другой исходник; отсутствие дедупликации семантически одинаковых перекодированных фото явно принято. Digest — персональные данные в соответствующем контексте и стирается при erasure, не используется для межtenant сопоставления. Бесплатные slots резервируются при user admission, под блокировкой photo identity, а не присваиваются по порядку завершения. Два slots действуют до window_end; failed/canceled освобождает slot для следующего запроса внутри окна. Принятый до window_end бесплатный run завершится бесплатно и позже него. Уже принятый платный run не становится бесплатно переоценённым после отказа другого run. Первый successful result фиксирует окно; конкурентные initial runs одного digest получают один общий first-result slot, остальные используют правила повторов. См. точную state machine в 09.

Операции делятся на `included_standard`, `paid_operation`, `free_beta`, `disabled`. Каждому operation registry ID и версии соответствует immutable billing policy. Для `paid_operation` задаётся отдельный ресурс `operation.<code>` и единица `successful_operation`; стоимость задаётся пакетами и included grants, а не скрытым пересчётом GPU milliseconds в деньги. Один принятый результат одной пользовательской операции на одном фото — одна единица, если policy явно не задаёт иную целую quantity. Preview платной операции имеет самостоятельный billing class; по умолчанию бесплатного тяжёлого preview нет.

Для операции должны быть определены included quantity по планам, цены/quantity/TTL top-up, eligibility, canonical success, rerun policy и charge scope. Пока этого нет, операция `disabled` для paid self-service либо явная ограниченная free_beta с budget. Стандартный pipeline не списывает второй operation credit за включённый шаг. Если пользователь выбрал отдельно платную операцию, quote показывает её отдельной строкой до запуска. Купленные operation credits не превращаются в standard_photo и обратно. Batch показывает стоимость каждого resource code и отказывается целиком, если уже известна нехватка любого ресурса.

## Остатки и смена плана

Для месячного фото-grant expiry первоначально равен period_end. Только свободные единицы generation=0 переносятся в generation=1 bucket после подтверждённой оплаты непрерывного следующего периода; expiry равен end следующего периода. Перед переносом закрыть старые leases и вернуть unissued allocations. Accepted tickets остаются funded исходным bucket и НЕ входят в rollover. Их late success списывает исходный leased balance, не трогая доступный rollover. При late failure исходный funding возвращается: если следующий непрерывный период уже оплачен и ещё действует, свободные единицы добавляются в тот же rollover bucket отдельным уникальным transfer event; иначе expire. Generation1 второй раз не переносится. При отмене без продления новый rollover не создаётся; уже выданные generation1 и top-ups сохраняются до expiry. Original bucket переходит в draining, пока есть accepted tickets, затем closed. Rollover — transfer, не повторное mint: original transferred_quantity и child original_quantity увеличиваются согласованно. Для operation ресурсов перенос допускается только при явно заданной policy, default=none.

Upgrade AI сразу после успешной оплаты: полная разница месячных net цен и delta фото, anchor прежний. Start→Photographer при уже потраченных 60: net доплата 1000, при тестовых 20% gross 1200, +200 фото, остаток 240. Квота и included capacity меняются согласованно, повтор оплаты/grant запрещён. Для исходного Trial первая подписка получает полную квоту нового периода; единственный уже выданный Trial bucket сохраняется без повторного grant. Trial photo bucket имеет expiry_policy=never и expires_at=NULL как явный режим только этого ресурса; storage при этом всегда конечен. Первую покупку после отменённой подписки считать новой AI-подпиской на текущем anchor storage-only либо новым anchor, если recurring items уже нет; месячные grants не начислять повторно за ту же invoice line.

Downgrade/отмена AI — на границе периода, без скрытого уменьшения уже выданных пакетов. Storage add-ons независимы: после отмены AI остаётся Trial included 2GB и оплаченный диск, новые trial photos не выдаются. Добавление storage оплачивается пропорционально остатку периода, egress той же строки также prorated с целочисленным округлением вниз. Сокращение диска — следующая граница; файлы не удаляются автоматически. При overquota блокировать увеличение объёма, дать read/export grace по опубликованным условиям и действия удаления/оплаты. Предложение grace=30 дней — продуктовый default, до продаж согласовать юридически.

## Дешёвый оптимистичный контроль

Требование владельца: минимизация проверок важнее абсолютного отсутствия небольшого превышения. Деньги и выдача оплаченных entitlement остаются точными. Optimistic применяется к коммерческому допуску фото, операций, пользовательскому egress и коммерческому порогу диска. Авторизация, tenant isolation, revoke, технический GPU/RAM/scratch budget и физический storage reservation остаются строгими.

Предлагаемая схема без дополнительного Redis: PostgreSQL — истина; каждый API instance получает небольшую порцию допуска tenant/resource/period. Выдача порции — короткая атомарная функция с блокировкой одной aggregate row и immutable bucket allocation; дальше локальная выдача ticket не читает billing DB на каждый шаг DAG. Ticket сохраняется в той же существующей транзакции, которая создаёт пользовательский run/request. Durable ticket и ledger пишутся в той же транзакции пользовательского admission; внешние billing эффекты и invalidation идут через billing_outbox. Существующий photo.outbox продолжает task dispatch, его message schema и consumers не меняются молча. Не нужно добавлять отдельный сетевой запрос для каждого model step.

Предлагаемые тестируемые defaults: entitlement cache TTL 5 секунд; один instance держит максимум одну неистёкшую порцию на tenant/resource; photo и operation порции до 5 единиц; egress порция 16MiB; порция живёт 10 секунд до выдачи tickets; cold billing query deadline 200ms. Это настройки commercial admission policy, не цены и не hard capacity. При остатке 1 выдаётся порция 1, а не 5. По достижении локального нуля instance получает новую порцию или отказывает. Revoked entitlement revision распространяется outbox и дополнительно истекает по TTL.

Порция дедуцируется из доступных buckets строго при получении; конкурирующие API не выдают один paid credit дважды. Мягкость состоит в принятии небольшого запроса сверх остатка через `optimistic_allowance`: суммарный бюджет на tenant/resource/accounting period конечен, его расход хранится в billing_allowance_budgets. Новый lease, restart, top-up, refund и погашение overrun не обнуляют этот бюджет. Размер выданной порции не больше chunk; одно ограничение «активен только один lease» недостаточно. Это явный положительный параметр policy, default 1 photo/operation и 16MiB egress; он не генерирует деньги и не создаёт новый grant. При allowance=0 порционный путь остаётся дешёвым, но без разрешённого overrun. Ledger хранит overrun отдельно от paid bucket, paid balance никогда не отрицателен. Нет автоматического счёта клиенту за превышение.

Нормальное превышение commercial quantity за accounting period не больше configured allowance: сумма settled allowance и ещё outstanding allowance никогда не превышает budget. Уже выделенные paid chunks не являются дополнительным overrun. Неопределённые после crash egress bytes остаются ограниченным hold внутри ранее выданной allocation, а не дают право бесконечно докупать бесплатный хвост. Для controlled egress каждый stream передаёт только уже выделенные chunks; максимальный хвост после закрытия entitlement ограничен суммой оставшихся allocations активных streams, а не размером полного ZIP. Их количество ограничено существующим admission/rate policy. В тесте фиксировать число instances/streams и вычислять предел из фактических allocations. TTL без rate/chunk limits сам по себе не ограничивает превышение.

Локальная выдача ticket уменьшает remaining allocation до постановки в очередь. Crash до commit возвращает ticket через recovery после сверки durable requests; crash после commit не освобождает выданную часть lease по одному лишь TTL. Истечение lease запрещает новые tickets, но не обнуляет обязательства accepted runs. Maintenance перечисляет committed tickets, возвращает только доказанно невыданный остаток. Split brain/fencing: lease epoch и instance generation; старый instance после потери lease не создаёт валидный новый ticket. DB atomic issued_quantity остаётся истиной: process-local counter — подсказка, не право на quota. Между preallocation и run transaction действуют один lock order и запрет lock upgrade; схема в 09. Каждый run admission проверяет ticket fence внутри уже существующей DB транзакции, без повторного чтения полного каталога.

На положительном свежем lease при временной недоступности сервиса summary UI может работать по cache, но без успешного commit run не считается принятым. После expiry новых коммерческих допусков нет; return 503 BILLING_STATE_UNAVAILABLE, а не бесконечный fail-open. Уже принятые задания завершаются. Auth/lifecycle/deletion failures никогда не обходятся cache.

Settlement в одной транзакции adoption + usage outbox: успешный billable user-result списывает один ticket один раз. Бесплатный rerun получает zero-cost authorization ticket и занятый free slot, без paid lease и без требования положительного баланса. Failure/cancel освобождает funding/slot только после terminal proof. Часть quantity может идти из paid bucket, часть — overrun. Дефицит от overrun погашается из следующего доступного соответствующего grant до нового допуска, отображается пользователю; никаких автоматических денег. Refund отзывает только неиспользованное; занятые tickets помечаются revoked funding, их финализация создаёт корректировку/overrun, а не отрицательный bucket.

## Диск и egress

Физические bytes продолжает резервировать существующая quota_reservations и tenant_usage. Включённая/купленная commercial capacity проецируется в tenant policy с revision. Нельзя отменить safety reserve, запустить GPU и потерять результат. Optimistic grace по коммерческому диску — отдельный явно ограниченный overflow budget в bytes, default 0 до измерения max file/output size. При изменении тарифа допускается завершение ранее принятых storage reservations. Не считать temporary scratch/cache/model cache/backup клиентским архивом. Одинаковая physical object version в tenant считается один раз; новое физическое содержимое export/ZIP считается отдельно. Место освобождается только после подтверждения purge.

Egress считает реально доставленные клиенту bytes: gallery previews, downloads, Range, повторы, CDN cache hit. HEAD/304/ошибка без body — 0; aborted transfer — фактически отправленный body. Внутренний worker I/O не коммерческий egress. Для хранения direct public object URL без controlled delivery/reliable access logs запрещено заявлять точное покрытие квоты. Стартовый обязательный вариант для enforce cohort — контролируемая серверная выдача с chunk accounting и durable delivery receipts; при CDN нужен надёжный billable-delivery feed, dedup и upper bound задержки. Один delivery event identity не равен object ID: повторное скачивание честно расходует трафик ещё раз.

## Блокировки

| Причина | Что закрывается | Что остаётся |
|---|---|---|
| Фото/operation остаток=0 и allowance исчерпан | Новые задания соответствующего ресурса | Принятые runs, готовые результаты, покупки, удаление |
| Storage overquota | Upload и output-producing admissions с ростом bytes | Чтение в пределах egress, deletion, billing |
| Egress исчерпан | Новые chunks тяжёлой отдачи | Малый интерфейс/metadata, billing, deletion; нельзя обходить heavy transfer через preview |
| Неподтверждённая оплата | Новые ещё неоплаченные entitlement | Ранее оплаченные права до их expiry |
| Удаление/lifecycle/revoke | Доступ согласно строгой security policy | Минимальный authenticated status удаления |

Недостаток квоты — HTTP 409 с resource_code, needed, available, deficit, summary_revision, next actions. 429 — rate/capacity, 503 — инфраструктура. UI не должен предлагать платёж как решение технического 429 или отсутствия согласия.
