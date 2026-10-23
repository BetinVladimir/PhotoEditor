# Аудит полноты и согласованности биллинга

Дата 21.10.2026. Проверены документы 01–08 и конкретные source entrypoints; исправления внесены в исходные разделы, точные дополнения собраны в 09. Это документальный аудит рабочей копии; installed DB, реальные Stripe settings, квалификация производительности и e2e execution не заявляются проверенными.

## Существенные противоречия и решения

| ID | Было или отсутствовало | Исправлено | Проверка |
|---|---|---|---|
| A01 | API без /api/v1, tenant param{id} | Canonical /api/v1/tenants/{t}, existing route registry inputs | E71 |
| A02 | correlation_id/retryable новый top-level error envelope | Сохранён code/message_key/args/request_id, args содержит resource detail | E71 |
| A03 | key/revision в JSON, generic hash replay | Headers Idempotency-Key/If-Match, точные receipt bytes/status и current authority revalidation | E72 |
| A04 | «member» как роль и гипотетические несколько owners | Реальные owner/admin/editor/viewer, один owner_user_id и existing TransferOwner | E50,E63,E78 |
| A05 | Один active allowance lease разрешал заново превышать лимит | Durable cumulative period budget, no reset при restart/topup/cover | E73 |
| A06 | Rollover мог включать funding незавершённых jobs | Accepted funding исключён; late success old leased, late failure eligible unique transfer | E23,E74 |
| A07 | Бесплатный rerun требовал положительный paid lease | Zero-cost ticket и admission free slots при нулевом балансе | E34,E75 |
| A08 | Стоимость конкурентного rerun определялась после результата | Free-slot allocation при admission, finite initial in-flight serialization | E75 |
| A09 | Global financial_subject_id без таблицы; полиморфные grant refs | financial_subjects, typed entitlement allocations, provider namespaces и retained scope | E76 |
| A10 | Billable result равнялся task/run | User usage request/charge item, root/child links, unique canonical visible result | E77 |
| A11 | Onboarding activation конфликтовала с OTP active user session | Отдельный onboarding state и разрешённые limited endpoints | E79 |
| A12 | После revoke пользователь должен был передать студию | Preflight/TransferOwner до erasure commit, coordinator после auth_epoch revoke | E78 |
| A13 | Fresh auth предполагалась существующей | Новая purpose/session/epoch-bound one-use OTP proof,5min | E80 |
| A14 | Фото/GDPR purge без точного контакта с auth receipts | auth_epoch++, challenges/OTP replay, local/snapshot PII inventory и tombstones | E65,E68,E80 |
| A15 | Meter фактических bytes поверх direct signed URL | Controlled backend URL с сохранением DownloadLink, current authority и receipt accounting | E81,E82 |
| A16 | Exactly-once TCP bytes недостижимо, crash двусмыслен | Bounded uncertain hold и reconciliation, no full object automatic charge | E82 |
| A17 | Billing outbox одновременно назывался существующим task outbox | Раздельные task и financial queues, atomic bridge и отдельные consumers | E83 |
| A18 | Max-file/manual retention можно было вывести из старого 7/30 DTO | Явная policy version/manual asset classes, запрет 0/NULL и no baseline DDL rewrite | E84,E85 |
| A19 | Storage Checkout мог создать вторую subscription | Existing subscription invoice saga; new subscription только при её отсутствии | E86 |
| A20 | Из оплаченного egress урезался Trial | Trial remainder закрывается, новый paid период получает полный grant | E87 |
| A21 | AI upgrade egress delta и renewal race не определены |10×delta included storage; no open renewal invoice и >24h guard; compensation при гонке | E18,E88 |
| A22 | Test/live недостаточно для catalog/Stripe isolation | catalog_scope плюс provider account/sandbox identity, fail closed неизвестного tax scope | E76,E89 |
| A23 | Истёкшие signed URLs в exact idempotency replay |202 stable receipt IDs, fresh action URL через GET status | E72,E90 |
| A24 | All legal documents одного submit конфликтовали с unique event key | Natural uniqueness включает document/purpose/action; exact archived version | E57,E60 |

## Проверенные источники реализации

| Источник относительно PhotoEditor | Подтверждённый факт |
|---|---|
| backend/internal/httpapi/catalog.go, revision/mutation | If-Match quoted positive revision,428 missing; origin+CSRF для web; текущие API param identities |
| backend/internal/httpapi/server.go, writeError | code/message_key/args/request_id, X-Request-ID |
| backend/internal/domain/domain.go | Роли owner/admin/editor/viewer и hierarchy |
| backend/internal/tenant/service.go, TransferOwner/Update/Create | Один owner; active target; admin policy PATCH;100GiB/source100000/GPU1; retention7/30/24 |
| backend/internal/auth/service.go, verifyOTP/AuthenticateAccess | bootstrap user + session; active/auth_epoch/revoke checks, encrypted OTP replay receipt |
| backend/internal/auth/user.go | Профиль читает только active, preferences CAS; deletion отдельного пользователя здесь нет |
| backend/internal/idempotency/mutation.go, Use | Scope tenant/principal/route/key, current receipt authority, exact canonical successful replay |
| backend/internal/database/transaction.go | Retry DB-only callbacks при serialization/deadlock; нельзя provider HTTP внутри |
| backend/internal/portal/service.go, RedeemDownloadIntent | Objects.Check, signing seconds, SignDownload, current proof, DownloadLink |
| backend/internal/httpapi/download_redemption.go | No-store redemption response, не streaming byte meter |
| backend/internal/download/service.go | Owner download jobs и intent API с существующими typed DTO |
| backend/internal/gateway/download.go | Внутренний authorized/mTLS/task-scoped I/O, не public customer egress |
| backend/db/migrations/00001_global_auth.sql | users email unique, auth_epoch, sessions/challenges/mail encrypted payload |
| backend/db/migrations/00002_tenancy.sql | JSON quotas/retention, mutable tenant_usage и RLS |
| backend/db/migrations/00004_ingest_sharing.sql | quota_reservations holder bytes/files/expiry |
| backend/db/migrations/00006_workflow.sql | runs/task_results и photo.outbox tenant event destination/routing schema |
| backend/db/migrations/02595_registry_nine_disabled_declarations.sql | Наличие declaration не v2 activation; ряд операций явно v2_enabled=false |
| packages/contracts/README.md, scripts/endpoint_packaging.py | Source union baseline/executable, aliases и conflicting duplicates forbidden |
| scripts/generate-contracts.py / generate-db.py | Source files и исторический baseline DDL generator, а не универсальный additive migration writer |
| apps/photographer/app/_layout.tsx и src/paper-provider.tsx | Paper provider, locale/auth/workspace boundaries |
| apps/photographer/screens/settings/quotas.tsx | Существующий Paper screen читает tenant settings/capabilities, commercial balance отсутствует |
| apps/photographer/package.json | react-native-paper^5.15.3 и Expo router; native Checkout capability не квалифицирована одним наличием зависимости |

- Идентифицированный source migration filename head02597 не является обещанием следующего номера; source bundles и установленная DB могут отличаться.
- Текущий packages route union323 identities, v2 source39 operations; baseline/SQL registry шире.
- Перед кодогенерацией записать commit/dirty source fingerprint, не сбрасывать пользовательские изменения.

## Дополнительные обязательные e2e E71–E90

Эти проверки дополняют E01–E70, а не заменяют их. Каждая требует положительный и отрицательный исход. Исторические tests здесь не объявляются executed.

| ID | Положительный сценарий | Отрицательный сценарий |
|---|---|---|
| E71 Transport parity | Registry/HTTP/OwnerAPI schemas совпадают с /api/v1 identities и existing envelope | Неверный alias{tenant},unknown fields,unquoted/missing If-Match отвергаются до effects |
| E72 Receipt lifecycle | Replay возвращает исходные stable202 bytes/status; GET выдаёт current action | Истёкший URL не извлекается из receipt; revoke/re-role не раскрывает старый owner response; expired API receipt не позволяет второй grant |
| E73 Cumulative allowance | Один allowance1 принят, следующий после grant/correct cover не переоткрывает period budget | Lease expiry/restart/другой instance/новый topup не получают ещё allowance1; сумма outstanding+settled<=configured |
| E74 Rollover held credit | Bucket100,consumed60,leased10 переносит 30; late success10 остаётся original | При переносе 40 тест падает; late failure10 добавляет child ровно один раз только пока paid next period eligible |
| E75 Free rerun zero balance | При available0 и свободном slot accepted free result без paid lease | Третий/expired slot отказывает без credit; initial same digest in-flight409; no quote price change от другого failure |
| E76 Finance FK scope | Invoice с двумя ресурсами создаёт два typed allocations и один денежный payment | Другой sandbox/subject/tenant/line ID не связывается; orphan polymorphic refs не дают grant |
| E77 One user charge | Один standard user request с несколькими child runs и retries списывает 1 | Каждый adopted model step не создаёт ещё credit debit; task-only result без видимого final output не charged |
| E78 Owner erase | Transfer активному admin до revoke сохраняет studio billing; former owner erases | Старая revision/неактивная цель/попытка transfer через revoked session отказ; deleting former owner не отменяет новую чужую subscription |
| E79 Onboarding auth | Pending user входит по OTP, принимает required set и создаёт tenant после complete | Нельзя создать tenant/Checkout до completion; нельзя закрыть сам accept endpoint users.status=pending |
| E80 Fresh proof/epoch | Новая purpose-bound OTP proof в 5min принимается один раз | Refresh/new access token,чужой purpose,старый auth_epoch,OTP recovery после erasure не открывают deletion/auth |
| E81 Controlled URL | Actual photographer и portal validators принимают controlled URL с единственным bound versionId, исходным DTO и finite TTL | New enforced link не выдаёт direct object URL; bucket public access/expired capability/revoked share отвергнуты |
| E82 Crash byte ambiguity | Подтверждённые chunks settled once; unresolved bounded hold виден и reconciled | Crash после write до DB не списывает весь ZIP и не возвращает unlimited allocation; HEAD0/no duplicate receipt |
| E83 Outbox isolation | Одно событие user result порождает нужный task/finance bridge atomic | Task publisher не запускает financial grant, billing consumer не повторяет DAG activation |
| E84 Retention migration | Manual cohort хранит retained results/previews/archive дольше 30 дней, temporary cleanup работает | Legacy DTO0/NULL не включает manual; ни один age-GC не удаляет клиентский архив в обход policy |
| E85 Source generation | Новые catalog/migrations additive, baseline hashes unchanged | Перегенерация billing не меняет DDL1–9, frozen source hashes и существующие parameter identities |
| E86 Single subscription | Storage add на existing AI и AI resume на storage-only используют тот же sub/anchor | Второй subscription_checkout запрещён; incomplete/failed invoice не даёт capacity |
| E87 Anchor conversion | Trial использовал 5GB,переход Start выдаёт 100GB paid и закрывает leftover Trial | Paid grant не уменьшается до 85/80GB, Trial20GB не остаётся вторым активным base entitlement |
| E88 Upgrade entitlements | Полная AI delta даёт photo/storage/egress10×storage delta один раз | Open renewal invoice/<=24h window не создают charge; unavoidable race компенсирует paid upgrade без двойного renewal |
| E89 Closed scopes | Sandbox BG synthetic20% pass,live flag off; unsupported country fails before Stripe | Fixture price/operation limits не публикуются live; account/scope/currency подмена отвергается |
| E90 Multi-resource admission | Batch с free и paid items сохраняет all tickets и root stubs до seal | Late shortage одного ресурса не публикует половину DAG; abort/releases идемпотентны; active old tickets не украдены |

## Матрица начала работ

- BILL-01 включает source inventory и function lock/adoption mapping;
- BILL-03 включает additive catalog и supported source generator;
- BILL-05 включает cumulative allowance/free slots/rollover;
- BILL-08 включает typed financial allocation/provider effects и отдельный outbox;
- BILL-10 включает actual Paper shell и fresh action URLs;
- BILL-11–12 включают onboarding state/recent-auth/privacy preflight;
- BILL-13 проверяет E01–E90 и recovery evidence.
- BILL-14 остаётся live gate.

Незакрытые продуктовые решения перечислены в 09 с default-disabled поведением. Они не требуют догадок при создании scaffold и не считаются готовыми к публикации. Ни юридическое одобрение, ни налоговая настройка, ни квалификация операции не выводятся из успешного unit test.

## Дополнительная проверка совместимости URL

Существующие photographer/portal validators требуют versionId и конкретный DTO shape; сохранение только имени DownloadLink недостаточно. Правило сохранения versionId, numeric safe bytes, срока и safe URL закреплено в 09, E81 проверяет actual client parsers. Код parser/UI в рамках аудита не меняется.

## Fingerprint обследованных источников

SHA256 относится к source working tree, а не к установленным production artifacts. При изменении любого source переснять соответствующий контракт и затронутые проверки.

| Источник | SHA256 |
|---|---|
| packages/contracts/endpoints.json | 7fb963559c8236995cb26c5be76bc6bbff2a2863feee452fbb1b84e106602b5c |
| docs/implementation-v2/contracts/operations.json | 2543a41d0689ffa9b180b2dd89601c088687cde05bae6aa4d0f4ad6940fbaade |
| backend/internal/httpapi/catalog.go | 5693a8959cedad8431d45139187aaad2aec52a19cfad2ddcdd593a9cf70124e6 |
| backend/internal/httpapi/server.go | 5e7a7832fdc39c5867f32f04821b44dd4c3ad6f573db2970deac1d54a538495a |
| backend/internal/tenant/service.go | a6074bc0980be4a935a83b78ed4feab3f9ec668757e85f04607025233ffa5795 |
| backend/internal/auth/service.go | 77ccd93b8d4822ffcf82cb56ba7578247cf641a68116104a7e2370a59902fa5c |
| backend/internal/idempotency/mutation.go | e78222b03ccd9b7e69477af6313acbdf09ad311b607a37f58f095a876ae86de9 |
| backend/internal/portal/service.go | cdf03bce5dbe87ebb5c398364c415cecb907705583903ae1c689a046e8d4bd06 |
| apps/photographer/src/delivery-download.ts | 6cba89e5edcc051efffd7111610e3c568cbd098cefaee5970f8dd7874293ff9b |
| apps/portal/src/lib/download-link.ts | 22dfc395f6c792814a1b7737272f9c2c9122113e9890fe001e7f31adffa6fe4a |
| scripts/generate-db.py | 509a247d0deba6ee6f9533b8dcc563bf5b6c3fdeda9d711bc1d73cfcaba08db6 |
