# Декомпозиция миграции и сквозная приёмка

## Единицы реализации

- Ни одна задача не считается завершённой только по наличию кода.
- Для каждой нужны актуальные contracts, migrations при необходимости, реальный wiring, положительный и отрицательный тест, recovery evidence и ссылки на результаты.
- Ниже «владелец» означает будущую ответственную роль, а не назначение текущему пользователю.

| ID | Результат и зависимости | Критерии завершённости |
|---|---|---|
| BILL-01 | Инвентаризация текущих admissions, delivery, operations, auth и purge; без зависимости | Матрица operation→API→billing policy→canonical success; реальные current SQL signatures/head; полный data/PII inventory; lock graph без циклов |
| BILL-02 | Тарифные/operation policy и finite admission defaults; BILL-01 | Сохранены стандартные цены/лимиты; heavy pricing unqualified остаётся закрытым; отдельные max-file/concurrency decisions; approved vs draft различимы |
| BILL-03 | Каталог/профиль/seed/backfill, BILL-02 | M01–M03 и дополнительные зависимости 09; точные 7/6/45 counts; no duplicate trial; beta access не отнят; generated sources корректны |
| BILL-04 | Financial order/quote/invoice state и RLS, BILL-03 | Immutable snapshots, idempotency, role/tenant isolation, суммы совпадают; старый PATCH не увеличивает commercial limit |
| BILL-05 | Commercial leases/tickets/ledger, BILL-03 | Интеграция с существующим run/storage quota; bounded overrun доказан; settlement/release exactly once при crash |
| BILL-06 | Operation credits, BILL-02/05 | Отдельные resource balances и offers, immutable operation policy, no double charge included standard, unknown heavy закрыт |
| BILL-07 | Stripe Sandbox provisioning и adapter, BILL-04 | Реальные sandbox mappings и version manifest, no secrets in git, dev/CI изолированы; live off |
| BILL-08 | Inbox/outbox/fulfillment/reconciliation, BILL-05/07 | Signed real purchase даёт один grant; duplicate/out-of-order/lost event и restore восстанавливаются |
| BILL-09 | Upgrade/downgrade/storage saga, BILL-08 | Full delta AI, prorated storage, renewal race, SCA, compensation и storage-only проверены |
| BILL-10 | Paper billing и usage UI, BILL-04/05/08 | Только apps/photographer и Paper; все экраны/ошибки из 06; реальный API wiring; accessibility evidence |
| BILL-11 | Версии legal, onboarding и cookies, BILL-01/03 | Не приписывать consent; separate optional purposes; SDK absent before consent; exact archived document versions |
| BILL-12 | Profile erasure coordinator, BILL-08/11 | Fresh auth, ownership, active/external/backup steps, no resurrection; retained records minimal и ограничены policy |
| BILL-13 | Сквозная квалификация, BILL-06/09/10/11/12 | Все обязательные сценарии ниже executed и pass, без skipped как completed; artifacts без PII/секретов |
| BILL-14 | Live release gate, BILL-13 | Legal PhotoEditor texts, tax и retention approval, capacity benchmarks, monitoring/restore drill; отдельный осознанный выпуск live |

Обычный бесплатный технический режим не равен paid release. BILL-07 может выполняться до окончательных operation prices, но недоопределённые операции и live checkout остаются закрытыми. BILL-10 использует React Native Paper, это hard acceptance gate.

## План миграций

- Номера M01–M10 — логические этапы, не реальные числовые filenames.
- Перед генерацией определить актуальный head; не использовать следующий номер из устаревшей документации.
- Миграции forward-only и проходят существующие generator/catalog/security checks.
- Не редактировать старые миграции, frozen hashes или текущую рабочую реализацию при подготовке этого плана.

| Этап | Содержимое | Проверка |
|---|---|---|
| M01 Каталог | Plans/versions/prices/resources/limits/offers/operation/admission policies/provider mappings; default values | Empty install; typed limits; no unlimited null; no fake Stripe IDs |
| M02 Начальные данные | 7 plans, 6 price rows, 45 offers; trial once20; decimal storage и 10× egress; manual retention; reruns2/window7 | Exact seed values из 02; Custom hidden, Studio Pro self-service off; повтор provisioning/seed без дублей |
| M03 Backfill | One profile/tenant, beta mode + explicit transition policy; no historical usage debit; stable trial grant key | Dry-run counts и per-tenant diff; old uploads/results/links не теряются |
| M04 Финансы | Quotes/orders/lines/invoices/payments/refunds/change requests/account namespace/inbox/outbox | Unique financial business identity, immutable history, RLS/ACL, amount checks |
| M05 Metering | Credit buckets/lease allocation/tickets/ledger/summaries/photo identity, egress | Canonical settlement/failure/retry, generation fence, finite overrun; no second storage truth |
| M06 Storage integration | Paid entitlement projection в existing tenant policy; manual retention type; object category projection | Старый 7/30-day purge больше не удаляет клиентские originals/results; temporary cleanup сохранён; physical bytes reserve строго |
| M07 Legal/onboarding | Document versions, legal events/preferences/retention policy catalog | Existing users pending, никаких выдуманных old acceptances; no optional consent default true |
| M08 Erasure | Deletion request/steps/retained records/tombstones; актуальный purge dependency allowlist | Все новые tenant refs корректно очищаются; invoice retention переживает tenant delete без exposure |
| M09 Operation offers | Только измеренные опубликованные operation policies + per-plan offers | Resource codes/units/costs approved; test fixtures помечены и не публичны |
| M10 Публикация | Verified sandbox bindings и feature flags; live отдельной конфигурацией после gate | Draft не покупается; test/live/account guard; capability clients не обходят commercial policy |

### Defaults и нерешённые параметры

- Стандартные quotas берутся из раздела 02. Включённый диск не увеличивается каждый месяц.
- Retention client originals/results/archive=manual; temporary TTL может переиспользовать текущий 24h только после проверки актуальной конфигурации.
- Safety GPU concurrency не выводится из цены: initial single GPU default=1 допустим как действующее ограничение, не обещание dedicated slot.
- Source_files — технический счётчик, не photo credits.

- Max file/output bytes, per-tenant concurrency/rate, approved backup retention и legal financial years должны получить явные конечные значения до публикации.
- Не создавать бесконечные/null limits.
- В migration draft допускается отсутствие неполного paid config только с self_service=false и явным validation error.
- Test fixtures имеют отдельный environment/namespace и не становятся коммерческим seed.

- Backfill сначала report-only: tenants, physical bytes/reservations, existing retention, active runs и memberships.
- Выбрать transition_at, уведомления и beta entitlement для каждой категории existing tenant.
- Новый Trial default=2GB не должен молча выключить старый tenant на 100GiB.
- Сохранить beta capacity до объявленного перехода; затем overquota state без auto purge.
- Trial eligibility один раз на tenant, не на login/membership; не вводить вечный email fingerprint для противодействия повторной регистрации без отдельного основания/retention.

### Совместимость и развёртывание

- Expand schema → backfill малыми batches с resumable cursor → dual-read comparison/projection → shadow commercial gate → limited sandbox cohort → enforcement.
- Dual accounting не должен давать две независимые debit цепочки.
- Feature flag выбирает один commercial ledger, existing physical reservation остаётся общей.
- Перед enforcement сравнить проекции со storage/current ledger и alerts.

- При сбое приложения flags выключают новые продажи/admissions, не удаляют историю и не делают destructive rollback.
- Existing committed runs завершаются; reconciliation и deletion jobs продолжают работу.
- Backfill restart возвращается по cursor; неизменяемые исторические цены не переписываются.
- Data restore отдельно сверяет Stripe paid state и применяет tombstones до serving.
- План восстановления tested, не «Down migration удаляет таблицы».

## Тестовый контур

- Обязательная цепочка: реальный Paper экран apps/photographer → real auth/API → PostgreSQL с полной текущей цепочкой миграций → настоящая очередь и контролируемый worker → object storage → Stripe Sandbox для финансов.
- Подмена Stripe unit fixture полезна для fault injection, но не заменяет sandbox e2e.
- Для операций нужен хотя бы один реальный canonical successful result, а не ручная вставка consumed counters.
- Для тяжёлых imaging tests допускается маленький квалифицированный набор изображений.

- Fixtures: два tenant A/B; owner/member/unauthorized/sole owner/transferred owner;
- Trial, Start, Photographer, Studio, Studio Pro closed и Custom; partial consumed buckets; operation resource test-only с явной ценой fixture; storage-only; unpaid subscription; old beta с >2GB; account с PII canaries в auth/billing/invites/EXIF/logs/webhook; multi-instance A1/A2; active download streams.
- Для локального времени injectable server clock, для Stripe recurrence Test Clocks где поддерживается; синхронизировать ожидания, не считать fake local clock Stripe clock.

- Каждый сценарий проверяет UI + API + durable DB state + отсутствие лишних Stripe charges/grants/object copies, где применимо.
- Выполнение фиксирует commit/source snapshot, migration head, sandbox/account, API version, fixture policy, test IDs, timestamps и evidence paths.
- Screenshots/trace/netlogs очищаются от PII/keys/hosted capability URLs.
- NOT RUN/SKIPPED/BLOCKED не равны PASS.

## Каталог положительных и отрицательных e2e

Колонки «положительный» и «отрицательный» — оба обязательны, а не выбор одного. Recovery и гонки выполняются отдельными тестами в дополнение к обычному сценарию.


### Часть 1 из 3

| ID | Положительный сценарий | Отрицательный сценарий и ожидаемый результат |
|---|---|---|
| E01 Seed | Fresh DB исходного sandbox seed имеет 7 plans/6 prices/45 offers, точные bytes/цены | Неполный plan version не публикуется; Custom не попадает в public catalog |
| E02 Upgrade DB | Existing beta сохраняет архив/reservations и согласованный access | Новый Trial limit не уничтожает >2GB архив; no historical charge |
| E03 Replay миграции | Повтор backfill/provisioning не добавляет profile/grant/Price | Прерванный batch продолжает cursor, без дублирующего Trial |
| E04 Trial | Новый tenant один раз получает 20 фото без карты | Relogin/rejoin/new member не дают 20 снова |
| E05 Catalog | Публикуемые планы и eligible offers отображены Paper cards | Подставленный Custom/closed Studio Pro ID не создаёт Checkout |
| E06 VAT fixture | Start900+180=1080, Photographer1900+380=2280 | Client amount/tax/currency подмена отвергается; неподдержанный tax scope не получает безусловные 20% |
| E07 Quote | Актуальный quote создаёт immutable order | Expired quote/changed price/membership создаёт 409/403 без charge |
| E08 Idempotency | Двойной Paper click и retry возвращают один order/session | Тот же key с другим payload409, нет второго платежа |
| E09 Subscription | Реальная sandbox оплата выдаёт одну месячную квоту | Success URL без webhook/paid proof не даёт entitlement |
| E10 Async pay | Delayed successful payment позднее выдаёт grant | Pending/failure не дают grant; после deleting поздняя оплата не оживляет account |
| E11 SCA | Challenge success завершает purchase | Decline/canceled challenge сохраняют старый план/баланс |
| E12 Signatures | Signed real event durable сохранён и применён | Неверная signature/stale timestamp/raw-body modification400 без inbox grant |
| E13 Event dedup | Дубликат event и разные events об одном invoice дают один grant | Повтор с чужим customer/account/mode quarantined, нет cross-tenant изменения |
| E14 Out of order | subscription/update и invoice paid в обратном порядке сходятся к истине | Поздний старый canceled/update не откатывает более актуальную verified business state |
| E15 Lost webhook | Reconciliation находит реальный paid invoice и выдаёт missing grant | Повторный full reconciliation не повторяет grant/notifications |
| E16 Crash fulfillment | Crash после paid commit до grant восстанавливается | Crash после grant до ack не дублирует quota |
| E17 AI upgrade | Start→Photographer,60 used: gross1200,+200, available240, anchor прежний | Failure/SCA отказ не меняют plan; две вкладки не применяют две delta |
| E18 Renewal race | Оплаченный renewal рядом с upgrade согласует цену и quota по saga | Crash provider update/local apply восстанавливается без второго delta/charge |
| E19 Compensation | Paid upgrade с необратимым apply failure делает refund+correction | Нет бесконечного applying без alert и нет нового требования оплатить тот же upgrade |
| E20 Downgrade | Переход на границе сохраняет уже выданные packs до expiry | Immediate trim месячных/topup остатков запрещён |
| E21 Cancel AI | Storage-only и купленные packs продолжают по условиям | Cancel AI не отменяет storage items и не выдаёт новый Trial20 |
| E22 Rollover | Generation0 остаток переносится один раз после paid renewal | Generation1 повторно не переносится; unpaid gap не создаёт rollover |
| E23 Rollover pending | Late success расходует только исходный leased funding, late failure переносит освободившееся ровно один раз при eligible paid next period | Funding accepted tickets не входит в rollover и не создаёт искусственный overrun |
| E24 Photo topup | Пакеты 100/500/1000 даются на 90 суток после оплаты | Trial/foreign-plan offer/custom без assignment нельзя купить |
| E25 Expiry order | Расходуется ближайший expiry; до expiry accepted result завершается позже | Release после expiry не оживляет истёкший остаток |
| E26 Lease chunks | A1/A2 получают конечные разные paid allocations | Два instances не расходуют один paid bucket; allowance bound не превышен |
| E27 Optimistic overrun | Один разрешённый небольшой overrun виден отдельно без автоматического денежного списания | Дальнейшие requests после allowance rejected; no negative paid balance/no auto invoice |
| E28 Cache stale | Уже committed runs завершаются после entitlement update | Expired lease/stale fence/new generation не выдаёт новые valid tickets |
| E29 Lease crash | После crash recovery возвращает только unissued часть | TTL не возвращает credits accepted run и не допускает второго использования |
| E30 Billing outage | Принятые задачи завершаются, cached summary помечен stale | После finite lease expiry новые admissions503, не unlimited fail-open |
| E31 Standard photo | Один canonical successful output=1 photo | Много лиц/steps/технических retries/export format не умножают charge |
| E32 Reruns | Первые два user reruns в 7 дней бесплатны | Третий/после окна charge1; платный rerun не сбрасывает окно |
| E33 Identity | Повторный импорт тех же bytes в другом проекте tenant сохраняет identity | Нельзя получить новые бесплатные reruns копированием; чужой tenant не раскрывает digest membership |

### Часть 2 из 3

| ID | Положительный сценарий | Отрицательный сценарий и ожидаемый результат |
|---|---|---|
| E34 Concurrent reruns | Два free slots удерживаются при admission, успехи не меняют согласованную стоимость | Retry одного request не занимает новый slot; без paid balance бесплатный rerun разрешён |
| E35 Operation credits | Отдельный paid operation consuming свой resource, topup увеличивает его | Standard_photo credits не подменяют operation; included standard step не double charge |
| E36 Operation pricing | Published measured policy даёт точный quoted units/result | Unknown/heavy disabled не запускает paid job и не списывает; beta finite budget |
| E37 Batch | Достаточный multi-resource batch стартует, partial success списывает successful только | Known deficit любого ресурса блокирует весь batch до отправки |
| E38 Worker terminal | Failure/cancel освобождают allocation и bytes один раз | Duplicate terminal event и crash adoption не вызывают double settle/release |
| E39 GPU outage | Accepted queue resumes на том же ticket | Новая retry попытка не расходует отдельную коммерческую единицу |
| E40 Storage purchase | Paid add-on расширяет capacity и prorated egress один раз | Unpaid update не даёт capacity; storage не превращается в 90-day photo bucket |
| E41 Storage reserve | Upload declared/actual bytes и output reservations сходятся | Full disk/oversized object блокируют admission, GPU не стартует без safety reserve |
| E42 Storage decrease | Next-boundary decrease даёт overquota и понятный read grace | Ни cancel, ни возраст 30 дней автоматически не purges client originals/results |
| E43 Storage dedup | Одна physical version shared внутри tenant counted once | Отдельная ZIP/export copy counted; ссылка не удваивает bytes |
| E44 Purge bytes | После verified physical deletion committed bytes уменьшаются | Delete requested/failed object removal не освобождают место заранее |
| E45 Egress | Body/Range/preview/repeat download/CDN feed counts реальные bytes | HEAD/304/no-body=0; internal worker I/O не billable; invalid Range не тратит full object |
| E46 Egress bound | Active streams тратят выделенные chunks, bounded tail after cutoff | Большой ZIP/direct URL не обходят quota; превышение не равно неограниченному full stream |
| E47 Egress restart | Partial flush receipt replay без double consumption | Потеря метрики после crash не скрывает неограниченные bytes; ambiguous scope закрыт/reconciled |
| E48 Refund | Unused remainder полного/частичного refund отзывается один раз | Consumed history не удаляется; active tickets не создают negative bucket |
| E49 Dispute | Verified dispute отражает опубликованный доступ и history | Чужой refund/dispute не блокирует другой tenant; deletion остаётся доступным |
| E50 Roles | Owner purchase и member read-only usage | Member POST checkout/portal/change/custom assignment403; invoice URL не выдаётся |
| E51 Tenant isolation | Switching tenant показывает только новую summary | Foreign UUID/order/quote/bucket/result404/403, no cross-tenant RLS bypass |
| E52 Old settings | Технические настройки меняются только разрешённым процессом | Старый tenant PATCH/прямой SQL role не повышает paid entitlement |
| E53 Paper screens | Все billing/legal/deletion экраны используют реальные Paper components | Нет HTML substitute/нового framework; portal recipient не получает payment controls |
| E54 UI states | Mobile, keyboard, dark mode, bigint, timezone и decimal GB корректны | Loading/offline/stale/failed не выглядят successful/zero balance; focus восстанавливается |
| E55 Resume purchase | Reload/return from Stripe восстанавливает тот же order и batch selection | Tampered deep link/success param не дает платный доступ, новый charge не создаётся |
| E56 Notifications |80/95/100% один раз на resource/period | Re-render/replayed outbox не спамят; новая квота пересчитывает threshold корректно |
| E57 Onboarding | Явное принятие актуальных Terms/DPA и Privacy acknowledgement активирует account | Unchecked/подменённая версия/locale/hash/изменившийся текст не создают фиктивный consent |
| E58 Optional consent | Reject optional позволяет core service; accepted SDK только после выбора | До consent нет optional network/storage; closing banner не accept |
| E59 Withdrawal | Отзыв analytics/marketing применяется и документируется | Не отнимает paid core service и не открывает matching/learning consent |
| E60 Existing legal | Existing account явно принимает новую требуемую версию | Backfill не записывает accepted задним числом; obsolete version не покупает новый план |
| E61 Legal links | Все ссылки legal.beeloy.org, version archive/hash и translations проверены | Missing PhotoEditor scope/неподдержанный B2C блокируют live; loyalty terms не маскируют пробел |
| E62 Delete member | Fresh-auth member удаляет PII/сессии, общая студия сохраняется | Stale auth/CSRF/чужой request не удаляют профиль/tenant |
| E63 Delete owner | Transfer ownership или явный tenant close корректно завершает erasure | Sole owner не стирает чужую shared студию молча; запрос не висит без deadline |

### Часть 3 из 3

| ID | Положительный сценарий | Отрицательный сценарий и ожидаемый результат |
|---|---|---|
| E64 Delete payments | Deleting блокирует Checkout/grant и отменяет будущие charges по scope | Late webhook/reconcile не восстанавливают account и subscription; paid orphan компенсируется |
| E65 PII inventory | Canary PII очищена из auth/contact/invites/events/cache/local storage | Очистка только users.email не проходит; full IP/email в logs/raw payload выявляются |
| E66 Photo erasure | Все purge-scope object versions/multipart/EXIF/embeddings удалены с proof | Shared refs требуют явного retention reason, failed object DELETE не completed |
| E67 Stripe erasure | Customer PII redacted/deleted где применимо, invoices минимально retained | Stripe outage→external_pending+retry, не ложный completed |
| E68 Backup restore | Restore до serving применяет tombstones, данные не воскресают | Старый login/photo/hosted customer связь не становятся активными после restore |
| E69 Legal retention | Retained record имеет approved basis/expiry/restricted access | Нет blanket «GDPR всё удалить» для statutory invoice и нет бессрочного retain all |
| E70 Repeated erasure | Повтор запроса/worker restart возвращает тот же progress | Дубли jobs не отменяют чужую subscription и не посылают повторный refund |

## Дополнительные интеграционные проверки

- SQL: grants/ACL для каждой функции, FORCE RLS, tenant-aware FK, forbidden direct DML, fixed SECURITY DEFINER search_path, immutable version updates, negative amount/quantity, missing policy, expiration boundary ровно в timestamp.
- Migration checks на свежей БД и upgrade fixture с populated current schema.
- No deadlocks при grant/admission/adoption/refund/purge/renewal concurrency.

- Metering нагрузка: при 100 последовательных resource admissions одного tenant на одном instance с chunk5 число новых lease allocations не более 20 при отсутствии expiry; существующие DB transactions run admission допустимы, отдельного catalog/Stripe lookup на каждый model step нет.
- На N instances проверять реально выданные allocations и configured allowance, а не произвольный процент превышения.
- Отдельно измерить p50/p95 admission latency, DB round trips/locks и cache hit ratio; proposed цель p95 добавочной billing latency тёплого допуска <5ms в контролируемом тестовом стенде, cold <200ms.
- Это target до измерения, не обещание production SLA.

- Fault injection: DB unavailable до inbox commit, commit unknown, Stripe timeout после успешного create, object PUT/DELETE unknown outcome, worker crash до/после adoption, outbox duplicate, stale lease, renewal boundary, user deletion одновременно с refund/payment/result publication.
- Для каждого требуется доказательство conservation paid balance и отсутствия cross-tenant side effects.

## Мониторинг и release checklist

- Метрики без PII: webhook durable receive/processing lag, retry/dead letters, paid-but-unfulfilled age, reconciliation mismatches, quota overrun по resource, lease allocations/expiry/fence rejects, stale summary, storage discrepancy, overquota tenants, deletion deadline/external_pending age.
- Предлагаемые alerts: paid unfulfilled >5min; inbox lag>60s устойчиво 5min; erasure step retry>24h либо deadline approaching; ledger/projection mismatch любое ненулевое после reconcile.
- Реальные thresholds уточнить на стенде, не отключать alerts ради pass.

- Тестовый release требует BILL-01–13, все E01–E90 положительные и отрицательные кейсы PASS в заявленном scope, отсутствие нераспределённых критических рисков, документы и runtime config соответствуют друг другу.
- Live требует дополнительно BILL-14. Если операция, native Checkout channel или определённый tax scope не квалифицированы, capability явно закрыт; это не считается «все кейсы покрыты» для опубликованной функции.

- Финальный evidence manifest: requirement ID → source contracts → migrations → function/service/UI → positive test → negative test → recovery/concurrency test → executed result.
- При изменении policy обновляются связанные expected values и повторяются затронутые тесты, а не переписываются исторические invoices или grant events.

## Дополнение аудита перед кодогенерацией

- Обязательны E71–E90 из [10-audit-and-traceability.md](10-audit-and-traceability.md).
- Полные DTO, supporting tables, периодические бюджеты превышения, бесплатные slots и preserved transport описаны в [09-codegen-contract.md](09-codegen-contract.md).
- M04 создаёт billing_financial_subjects/provider_effects/typed allocations/portal_actions;
- M05 — allowance_budgets/usage_requests/charge_items/cost_quotes/free_slots/delivery receipts;
- M07 — onboarding_states/recent_auth_grants;
- M08 — privacy_cases и owner preflight.
- Все эти сущности создаются до функций ссылающегося этапа; late-added dependencies не создаются после CALL вызывающих их функций.

- Catalog publication validation data-only: обязательные full limits нужны для self-service sandbox/live, но retired и draft строки могут сосуществовать с текущими assignments.
- Новые safety defaults не уменьшают уже committed physical reservations и не создают contract обещаний без qualification.
- E2E fixture publication имеет sandbox scope и finite max-file/concurrency, не заменяет реальные product decisions.
