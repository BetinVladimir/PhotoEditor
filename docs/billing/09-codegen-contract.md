# Однозначные контракты для начала кодогенерации

- Актуализация 21.10.2026. Раздел дополняет исправленные 01–08: технические решения ниже приняты для первой закрытой реализации.
- Непроверенные юридические/налоговые коммерческие решения не подменяются техническими defaults.
- Генерировать каркас данных, сервисов, Paper UI и закрытый Stripe Sandbox можно после чтения этого раздела.
- Включение продаж, тяжёлой операции или native purchase channel требует собственного evidence gate.

## 1 Граница кодогенерации и источники

| Пакет | Источник будущего изменения | Обязательный результат |
|---|---|---|
| Endpoints | docs/implementation/contracts/endpoints.json и docs/implementation-v2/contracts/endpoints.json | Новые identities в `/api/v1`, tenant param `{t}`. Все новые billing/privacy DTO — executable_schema. Не редактировать только packages/contracts/endpoints.json |
| DTO | docs/implementation-v2/contracts/schemas.json, $defs | additionalProperties=false, required list и nullable поля; bigint strings; запрос не принимает server-only amounts/actor/tenant |
| Operations | docs/implementation/contracts/operations.json и v2 counterpart по текущей projection | Billing policy — отдельный additive catalog, не подмена runtime v2_enabled/model qualification. Не добавлять billing keys в строгий operation schema без контракта |
| DB | Новый additive billing catalog в docs/billing/contracts/tables.json при реализации | Формат schema_version1: таблицы с явными columns/type/null/default/PK/FK/checks/indexes/ACL; companion functions.json для signatures и grant matrix. Контракты первого закрытого этапа созданы; фактическое покрытие и временные ограничения описаны в [11-implementation-progress.md](11-implementation-progress.md) |
| DDL | Новые forward-only миграции после фактического head | Не менять baseline1–9 и frozen source views. scripts/generate-db.py сейчас генерирует baseline, поэтому расширить additive generation отдельно, не перегенерировать исторические миграции |
| Go/clients | Новые billing/legal/privacy packages, httpapi routes, OwnerAPI/API-client | Wiring в текущем process/service installation и context cancellation; typed DTO drift tests; старые endpoint identities сохраняются |
| UI | apps/photographer/screens и localized app wrappers | Только React Native Paper, текущие auth/theme/locale/CAS/workspace boundaries |

- У каталогов есть отдельные `catalog_scope enum sandbox/live`, NOT NULL.
- Исходный seed создаёт только sandbox scope:7 plans,7 versions,6 price rows,45 offers; все commercial versions/offers draft.
- Live версии/цены/offers исходный seed не создаёт, они добавляются отдельным approved publication step с новым scope.
- Публикация sandbox версии разрешена с finite technical fixture limits и явно синтетическим tax scope; live версия не может использовать fixture-only политику.
- UNIQUE(plan_id,version,catalog_scope), UNIQUE(plan_id,offer_code,version,catalog_scope); все binding/quote/order snapshots фиксируют scope.
- Public catalog определяется server deployment scope, клиент не выбирает live/test.
- Studio Pro runtime capacity gate остаётся выключен независимо от catalog_scope, кроме отдельно помеченного mock-capacity теста отказа.

## 2 Точные роли и доверительные границы

- Реальные роли: owner/admin/editor/viewer.
- Только authenticated current `tenants.owner_user_id` + active owner membership меняет финансы, видит billing contact/invoices/portal.
- Admin получает read-only resource summary и buckets без финансовой PII.
- Editor/viewer видят capability с resource shortage в разрешённом им workflow; billing summary/buckets им 403. Public catalog не включает Custom; owner current Custom показывается через tenant summary.
- Обычная tenant membership не даёт owner billing права.

- SQL EXECUTE: `photo_api` — quote/order/change/summary и авторизованный issue в user admission;
- `photo_auth` — onboarding/recent-auth/profile privacy для своего authenticated user;
- `photo_scheduler` — metering settlement только с доказанным каноническим user-result и recovery;
- `photo_maintenance` — inbox/outbox/reconcile/expiry/erasure jobs.
- `photo_worker` не получает самостоятельный grant/settle произвольного result; существующая worker adoption функция вызывает закрытый helper, повторяющий task/attempt/fence/result authority.
- Общедоступных EXECUTE у helpers нет.
- Provider ingestion сохраняет inbox через узкую функцию без права самому выдавать entitlement.
- Внешний effect worker использует maintenance-scoped adapter с subject fence;
- SQL не принимает клиентское «Stripe paid=true».

API может вызывать только зарегистрированные statement signatures; новые maintenance allowlists и текущий application-role installation должны обновляться при реализации. Прямой DML финансовых таблиц не выдавать. Reference связи с tenant и user всегда проверяются до выдачи сохранённого replay body.

## 3 Уточнение полей и FK

- Базовая модель 03 обязательна вместе со следующими typed дополнениями.
- Все quantity/bytes bigint>=0; значения type/schema не выбираются генератором произвольно.
- PK у новых таблиц uuid, кроме явно составного ключа.
- Для row fields из 03, где type опущен: *_id uuid; *_at/start/end timestamptz; external/provider IDs text; state/type/kind enums из соответствующей строки; policy version int; деньги и counters bigint.
- Период только starts_at/ends_at, не смесь start/end aliases.
- `period_id` всегда FK billing_periods, `provider_account_id` всегда FK billing_provider_accounts.id (UUID), а внешний acct ID — external_account_id text.
- Это устраняет двусмысленность одноимённых внутренних и внешних IDs.


### Часть 1 из 2

| Сущность | Точное дополнение или исправление |
|---|---|
| billing_provider_accounts | id uuid PK; external_account_id text; environment test/live; sandbox_reference text?; UNIQUE(provider,environment,external_account_id,sandbox_reference) с NULLS NOT DISTINCT либо эквивалентной нормализацией. Не называть внешний acct ID локальным FK |
| billing_financial_subjects | id uuid PK; tenant_id uuid? UNIQUE FK tenants ON DELETE SET NULL; status active/deleting/retained/erased; lifecycle_epoch bigint>0; retention_policy_id uuid?; erased_at timestamptz?. Все financial_subject_id FK сюда; tenant erasure сохраняет только restricted subject |
| tenant_billing_profiles | financial_subject_id uuid UNIQUE FK; active_provider_account_id uuid? FK; lifecycle active/deleting/closed; lifecycle_epoch bigint>0; beta_policy_id uuid? FK; transition_at timestamptz?; current_price_id nullable только Trial/manual. UNIQUE(tenant,subject). Новый subject не создаётся при каждом входе |
| billing_contacts | financial_subject_id FK; status active/retained/erased. PII logical columns из 03 хранятся за encrypted envelope или column encryption, metadata/key refs отдельно; общий encryption_key_ref не является шифрованием сам по себе |
| billing_plan_prices | catalog_scope sandbox/live; amount_net_minor>=0; одна default цена по plan version/currency/interval/scope. Trial interval=once,0; платные interval=month |
| billing_operation_policies | registry operation_code/version совпадают точно с runtime registry; charge_context standard_root/standalone/preview/internal; policy_snapshot_hash bytea; UNIQUE(operation_code,operation_version,charge_context,policy_version). Нельзя по одному имени операции определить все контексты |
| billing_subscriptions | subject_id FK; provider_account_id FK; starts_at/ends_at живут в billing_periods; nullable period_id для incomplete; provider_subscription_id text UNIQUE(provider_account_id,provider_subscription_id). Local entitlement state отдельно от provider state |
| billing_subscription_items | provider_account_id FK; external_item_id text; UNIQUE(account,external_item_id); item starts_at/ends_at timestamptz; entitlement_status pending/active/ending/ended отдельно от provider presence |
| billing_invoices/payments/refunds | Сохранить имена provider_invoice_id/provider_payment_id/external_refund_id из 03; provider_account_id FK в каждой таблице; financial_subject_id FK; tenant_id nullable ON DELETE SET NULL; UNIQUE(account,external object ID); refund ссылается на payment composite(account,payment). Cross-subject/payment отказ |
| billing_order_lines | money_snapshot net/tax/gross/currency как typed columns; expiry_policy fixed_ttl/period_end/never/none; ttl_seconds bigint?; included_egress_bytes bigint; operation_policy_id uuid?; provider_price_binding_id uuid?; product label snapshot без PII |
| billing_invoice_allocations | Ровно одна provider invoice line; её идемпотентность UNIQUE(account,invoice_id,external_line_id,allocation_kind). Удалить business_grant_key из строки как универсальный ключ нескольких ресурсов; typed entitlement allocations ниже имеют собственный key |
| billing_entitlement_allocations | id; subject_id FK; tenant_id uuid?; invoice_allocation_id FK?; order_line_id FK?; custom_assignment_id FK?; bootstrap_grant_id FK?; resource_code FK; quantity bigint; kind credit/capacity; period_id FK?; starts_at; expires_at?; expiry_policy fixed/never/none; business_key text UNIQUE; state pending/applied/reversed. Хотя бы одно разрешённое typed основание, invoice и order line могут сосуществовать только при проверенной связи |
| billing_bootstrap_grants | id; tenant_id; reason trial_once/beta_transition; resource_code; quantity bigint; policy_snapshot_hash; UNIQUE(tenant,reason,resource). Trial once identity не зависит от active API idempotency receipt |
| billing_credit_buckets | entitlement_allocation_id uuid? FK; origin_bucket_id uuid? tenant/resource FK; transferred_quantity bigint=0; expires_at nullable только expiry_policy=never и source_type=trial и resource=standard_photo. Generation1 UNIQUE(origin_bucket_id,next period), partial increments через transfer event |
| billing_resource_ledger | quantity>0; события zero-cost не создают пустой финансовый debit. event_type дополнить transfer_out/transfer_in/refund_funding_mark; business_key unique(tenant,key). credit/debit/transfer имеет таблицу переходов ниже, направление не угадывать по signed quantity |
| billing_allowance_budgets | tenant/resource/period composite PK; configured_quantity bigint; outstanding_quantity bigint=0; settled_quantity bigint=0; policy_version int; revision bigint. CHECK outstanding+settled<=configured; expired/released unconsumed funding возвращается в outstanding pool только после terminal proof |
| billing_usage_tickets | request_id uuid FK billing_usage_requests; charge_item_id uuid FK; funding_kind paid/allowance/free; quantity>=0; free_slot_id uuid? FK; result_reference_id uuid? FK; unique(charge_item_id,resource_code); free требует quantity0 и slot, paid/allowance>0. resource ticket может иметь mixed allocations; funding_kind=mixed разрешить только при наличии обеих funding rows |
| billing_usage_requests | id; tenant_id; actor_user_id uuid?; request_key text; canonical_request_hash bytea; workflow_kind text; cost_quote_id uuid FK; status prepared/accepted/settled/canceled; UNIQUE(tenant,actor,request_key). Не использовать один run_id как request: пользовательский batch может создать много runs |

### Часть 2 из 2

| Сущность | Точное дополнение или исправление |
|---|---|
| billing_usage_charge_items | id; tenant_id; usage_request_id FK; ordinal int; photo_identity_id FK?; operation_policy_id FK; resource_code FK; max_quantity bigint; state prepared/accepted/succeeded/failed/canceled; user_charge_key text UNIQUE(tenant,key); UNIQUE(request,ordinal,resource). Root и технические дочерние runs ссылаются на один item |
| billing_usage_run_links | tenant_id; charge_item_id FK; run_id uuid FK существующего runs; relation root/technical_child; PRIMARY KEY(tenant,charge_item,run); result успеха child не создаёт отдельный charge item |
| billing_result_references | id; tenant_id; charge_item_id FK UNIQUE; canonical_run_id uuid FK runs; final_artifact_id uuid FK artifacts?; adopted_result_id uuid FK task_results?; adopted_at; authority_kind run_final/approved_workflow. Final success требует существующий видимый output и текущую authority chain, хотя бы один typed final artifact/result и validator по authority_kind |
| billing_photo_free_slots | id; tenant_id; photo_identity_id FK; slot_kind first_result/rerun_1/rerun_2; usage_charge_item_id uuid? FK; state available/held/consumed/expired; held_at?; window_ends_at?; UNIQUE(photo,slot_kind). First_result — метка единственного первого платного исходника, а не free credit: initial ticket остаётся paid |
| billing_cost_quotes | id; tenant_id; actor_id; request_hash bytea; snapshot_revision bigint; operation_policy_hash bytea; expires_at; state open/accepted/expired; hash bytea. lines в billing_cost_quote_lines: id,quote FK,ordinal,source digest/ref,resource,required max_quantity,free_slot eligibility,policy FK; unique(quote,ordinal,resource). Денег не списывает |
| billing_delivery_sessions | id; tenant_id; period_id FK; object identity immutable bucket/key/version/SHA/size; scope owner/portal; source authority ref; token_hash bytea UNIQUE; state active/revoked/closed; expires_at; revision. URL capability не содержит object key или customer PII |
| billing_delivery_chunks | delivery_id FK; attempt_id uuid; sequence bigint; ticket_id FK; allocated_bytes bigint; written_bytes bigint; confirmed_bytes bigint; state allocated/streaming/confirmed/uncertain/released; receipt_hash bytea?; PRIMARY KEY(delivery,attempt,sequence); confirmed<=written<=allocated; replay flush идемпотентен |
| billing_storage_usage | VIEW либо материализованная rebuildable projection, не новая mutable quota authority. tenant_usage остаётся admission truth; no separate bytes debit in billing settlement |
| billing_capacity_revisions | tenant_id; revision bigint; included_bytes/additional_bytes/effective_bytes bigint; entitlement_hash; applied_at; PRIMARY KEY(tenant,revision). projected policy change сохраняет existing JSON metadata и effective revision atomically |
| account_onboarding_states | user_id PK FK users; state pending/complete/reacceptance_required; required_document_set_hash bytea; accepted_document_set_hash bytea?; revision bigint; completed_at?; users.status не использовать для pending Terms |
| account_recent_auth_grants | id; user_id; session_id; auth_epoch bigint; purpose delete_profile/resolve_ownership; proof_hash bytea UNIQUE; verified_at; expires_at; used_at?; fresh OTP challenge ref. Перевыпуск refresh не создаёт fresh proof |
| privacy_cases | id; subject_ref; user_id uuid?; kind erasure/ownership_resolution; state open/resolving/closed; deadline_at; preferred scoped contact? encrypted; assigned_queue text; created_at. Запрос прав субъекта можно принять до ownership resolution |
| profile_deletion_requests | privacy_case_id FK; state preflight/ownership_action_required/erasure_requested/revoking/purging/external_pending/completed/partially_retained/failed; ownership_snapshot_hash; auth_epoch_at_acceptance bigint; subject_lifecycle_epoch bigint. Нет reuse obsolete user session для erasure worker |
| legal_document_versions | UNIQUE(document_key,locale,version); content_sha256 не заменяет text archive. archive_ref text и archive_version text обязательны для active, archived plain text доступен по versioned legal URL |

- Все source UUID FK, связывающие tenant entities, composite tenant-aware.
- Для финансовых retained rows child links идут по financial_subject_id, не через runtime tenant cascade.
- User identity для legal events nullable после erasure, subject_ref остаётся restricted.
- Глобальные inbox/provider/retained/privacy tables не получают фиктивный tenant_id: RLS/EXECUTE закрывает их как отдельный security scope.
- `FORCE RLS + tenant_id=app.tenant_id` применим к tenant rows, не к публичному каталогу и не к maintenance очереди без actor.

- Дата/nullable/enum defaults не наследуются из предположений генератора: created_at=server now; updated_at=now и revision=1 только mutable; bool flags=false, кроме is_active справочника=false до seed publication; quantity/counters=0 только где это допустимо CHECK; operational states выбираются явно INSERT.
- Финансовые суммы, policy/hash/period и provider environment не получают implicit default.

## 4 Conservation и state machine

- Paid bucket: `original = remaining + leased + consumed + revoked + transferred`.
- Все величины неотрицательные; remaining доступен только starts_at<=now<expires_at либо явный never.
- Проекция available складывает remaining, исключает leased и вычитает незакрытый overrun через отдельный cover debit, не рисует отрицательное значение.

| Событие | Предусловие | Атомарные изменения |
|---|---|---|
| Grant Q | verified typed entitlement allocation, unique business key | original+=Q,remaining+=Q; если есть overrun — covered часть remaining→consumed с overrun_cover reference. Не повторять после API receipt expiry |
| Allocate Q paid | active bucket, remaining>=Q | remaining-=Q,leased+=Q; lease allocation создаётся один раз |
| Issue ticket Q | active lease/fence, allocated not yet issued>=Q | issued+=Q и durable ticket/run links; bucket leased не меняется |
| Success Q paid | canonical user result, ticket accepted | leased-=Q,consumed+=Q,ticket settled; result unique |
| Failure/release Q | terminal no-adopt proof | leased-=Q; remaining+=Q либо revoked+=Q после expiry; eligible rollover transfer вместо возврата доступного old-period остатка |
| Transfer Q | generation0, next continuous paid period, amount не leased | parent remaining-=Q,transferred+=Q; child original+=Q,remaining+=Q; одна TX с unique transfer key |
| Expire/revoke Q free | Q<=remaining | remaining-=Q,revoked+=Q; reason expiry/refund различаются |
| Allocate allowance A | budget configured-outstanding-settled>=A | outstanding+=A; bucket отсутствует; не обнуляется от lease restart |
| Settle allowance A | accepted ticket, canonical result | outstanding-=A,settled+=A; overrun+=A. Settled budget не уменьшается после top-up или cover |
| Release allowance A | terminal no-result либо verified unissued | outstanding-=A; коммерческого grant нет |
| Refund issued funding | verified reversal, funding leased | Не делать revoked+=Q при сохранённом leased. Пометить allocation funding_revoked с reversal ID. Late result переводит leased→revoked и создаёт overrun; late failure leased→revoked без overrun |

- Expiry lease запрещает новые tickets, не снимает accepted обязательства.
- Recovery перед возвратом unissued берёт тот же lease lock, сверяет sum tickets; процесс-local decrement не финансовый факт.
- Leases не продлеваются дольше ближайшего expires_at funding bucket и accounting period_end.
- Never Trial photo всё равно использует конечный accounting period для allowance;
- Trial credits не истекают от закрытия этого периода.

- Rerun бесплатен при удержанном slot до конца окна.
- При нулевом paid balance free-slot admission проходит без paid allocation и без allowance; safety/storage/auth gates остаются.
- Initial first-result marker сериализует first successful paid result: пока initial in flight, повтор технической попытки использует тот же charge item; независимый новый user request до первого результата получает 409 PHOTO_INITIAL_IN_PROGRESS, а не второй initial charge.
- После success открываются два free slots.
- Для paid после исчерпания slots max_quantity=1. Slot release не переписывает цену уже accepted других requests.

- Batch known deficit проверяется до любых user run admissions.
- Все charge items, tickets и run stubs создаются одной existing admission TX для поддерживаемого batch entrypoint.
- Если current workflow имеет многотранзакционную prepare/seal цепочку, `billing_usage_requests.status=prepared` держит allocations без executable tasks, затем atomic seal accepted либо compensating release всех items; ни один task не публикуется до полного multi-resource gate.
- Child retry не создаёт новый user charge.
- Reused cached result за технический replay бесплатен, новая user paid operation — по pinned quote, а не по числу worker executions.

- Lock sequence: auth/lifecycle preflight и current receipt authority gate → API idempotency receipt → tenant lifecycle shared admission lock → billing usage request/summary+allowance locks в порядке resource_code → buckets по expiry/id → lease/ticket → existing runs/tasks adoption chain.
- Для maintenance/adoption путь без API receipt, но тот же tenant/billing→run order.
- Встраивание в existing adopters, уже захватившие run/task, требует внешнего wrapper, берущего billing locks раньше, либо durable result-success event с at-least-once settlement под тем же финальным authority; нельзя просто вызвать locking helper в конце с обратным порядком.
- Для первой реализации выбрать wrapper и atomic adoption+settlement; async projection только для UI.
- Изменение task authority API допустимо лишь forward-only reviewed extension с исходными guards.

## 5 Периоды планы и ручное хранение

- Один AI и storage subscription используют общий month anchor.
- Основной AI upgrade выдаёт delta standard_photo, delta included storage capacity и дополнительный egress `10×delta included storage` до current period end; egress delta полного AI upgrade не prorated, поскольку доплата полная.
- Одна paid change allocation может иметь несколько typed entitlement children.
- AI downgrade на границе выдаёт новый base план и новые included egress, не отзывает неистёкшие packs/carryover.
- Paid operation included grants на upgrade: только явная delta одинакового resource/unit, не перенос из standard_photo.

- Storage proration egress: floor(full monthly included egress × remaining_seconds / period_seconds).
- Provider invoice money берётся из принятого authoritative preview, не из независимо округлённой клиентской формулы.
- Quote сохраняет denominator/numerator, starts_at/end_at и tax breakdown.
- Начальная recurring подписка — полный period, не случайная calendar proration.
- Paid add-ons сохраняются при AI cancellation.
- Full recurring cancellation прекращает будущие grants; до paid end entitlement доступен, файлы не удаляются автоматически.

- Payment failed renewal: новые base/operation/egress grants и recurring capacity на неоплаченный период не выдаются.
- Ранее nonexpired photo packs остаются пригодны для обработки при наличии safety capacity; коммерческий debt gate не должен превращаться в тотальный запрет deletion/export.
- Storage становится overquota при уменьшившейся paid capacity, read grace по утверждённым условиям.
- API summary имеет entitlement_state отдельно от provider subscription state, не трактует active/past_due как факт оплаты нового периода.

- Существующий `Retention` DTO принимает только положительные processing_days/exports_days.
- Его нельзя записать 0/NULL как manual.
- В будущем добавить versioned `retention_mode=manual` policy и явные client asset classes.
- Для billing-controlled cohort legacy processing_days/exports_days остаются compatibility metadata, но archival GC читает manual mode и не age-purges originals/retained results/previews/gallery ZIP.
- Temporary/incomplete uploads/scratch/cache продолжают cleanup, это отдельные классы.
- До проверки ВСЕХ GC entrypoints enforcement manual считается неготовым; billing flag не исправляет существующий GC сам по себе.

- Старый admin tenant PATCH quotas/retention в billing-controlled cohort: любые переданные quotas либо archived retention overrides отклонить 422 BILLING_MANAGED_POLICY, даже если часть payload выглядит допустимой.
- Rename-only сохраняется.
- Beta cohort без billing сохраняет прежний contract до explicit transition; migration policy не тихо переносит 100GiB в 2GB.
- Правка safety quota через privileged operator additive config не становится обходом commercial entitlement.
- Сохранять неизвестную текущую JSON metadata через validated merge, не replace всего quotas/retention.

## 6 Контролируемый egress без изменения portal UI

- В enforce cohort возвращаемый существующим redemption `DownloadLink.url` ведёт на controlled delivery capability того же backend, остальные поля filename/bytes/sha256/expires_at сохраняются.
- Новая signing boundary не требует новой формы apps/portal.
- Авторизованный recipient продолжает обычную загрузку.
- Backend portal services и signing adapters можно менять при реализации; запрет пользователя относится к местоположению и технологии UI, не к серверному metering.

- Новый route `GET /api/v1/delivery/{capability}?versionId=<version>` и HEAD поддерживают attachment и finite single Range; bearer opaque, хранится hash, ответ private/no-store, логи редактируют URL.
- GET сверяет текущие share/owner/session/object-version/revoke proofs, а не только наличие paid quota.
- Existing valid old signed object URLs живут до конечного TTL; enforcement cohort включается после drain их максимального срока или rotation/gateway cutoff.
- Bucket приватный, новые direct signed URLs для клиентского чтения не выдаются.
- Инвентаризация охватывает owner download, portal item/ZIP, collection media-links, run outputs и previews; иначе sales egress gate закрыт. mTLS worker gateway оставлен внутренним и не billable.

- В отличие от общего HTTPServer WriteTimeout30s длинный download stream требует отдельной разрешённой streaming timeout policy с конечными idle/deadline/rate лимитами.
- Конкретные технические limits берутся из квалифицированного delivery policy, не unlimited.
- Chunk16MiB — внутренний budget, не требование клиенту дробить Range requests.
- Один HTTP response может последовательно использовать несколько allocations.

- Определение meter: bytes body, успешно принятые controlled response writer/edge delivery transport; не доказательство получения файла конечным человеком.
- HEAD/304/error до body=0. В live scope CDN bypass выключен; если включается CDN, нужен совместимый metering feed для cache hits.
- Written counters с durable spool receipts позволяют recovery; невозможно атомарно commit PostgreSQL и TCP send.
- При crash неопределённая часть остаётся `uncertain hold` в пределах ранее allocated chunk, не списывается как подтверждённо доставленная и не возвращается в available до reconciliation.
- Ограничить число outstanding uncertain chunks, quarantine при превышении, alert и операторское resolution.
- Не требовать недостижимого exactly-once network delivery; exactly-once применяется к учёту одного receipt, не к TCP.

## 7 DTO и transport

- Все новые schemas additionalProperties=false.
- UUID canonical lowercase nonzero, digests64 lower hex на wire/bytea32 внутри нового billing; conversion явный и lossless.
- Existing SHA text schemas не менять глобально.
- Decimal quantity regex `^(0|[1-9][0-9]*)$`, max bigint9223372036854775807, revision>=1. Server даты RFC3339 UTC.
- Unknown params/duplicate headers отклоняются.
- Limit1..100, default50; курсор signed tenant/resource/order scoped, не сырая строка Stripe.

Общие DTO:

| DTO | Обязательные поля и nullable |
|---|---|
| BillingMoney | currency=EUR; net_minor,tax_minor,gross_minor decimal strings; tax_mode enum test_bg20/approved; tax_lines array(code,rate_decimal,amount_minor); gross=sum(net,tax) |
| BillingResourceBalance | resource_code; unit credit/byte/count; granted,consumed,allocated,available,overrun decimal strings; blocked bool; blocked_reason nullable; next_expiry_at nullable; as_of timestamp. Storage не маскируется этой формой, отдельный DTO |
| BillingStorageBalance | included,additional,total,committed,reserved,free,over_capacity decimal strings; categories object(original,result,preview,archive); policy_revision string; as_of. free=max(total-committed-reserved,0) |
| BillingSummary | tenant_id,revision,entitlement_revision,plan object(code,version,name_key,custom bool),entitlement_state,billing_mode,period nullable(starts_at,ends_at),balances array,storage,recurring_total BillingMoney nullable,items array,pending_change nullable,actions object. Billing contacts не возвращать в admin view |
| BillingCatalog | catalog_revision,scope,currency,plans array(id,version_id,code,label,price BillingMoney,limits array,can_purchase,unavailable_reason nullable). Offers tenant eligible выдаёт отдельный GET, public catalog без Custom и без обещания eligibility |
| BillingOffer | id,code,version,resource_code,quantity,kind,expiry_policy,ttl_seconds nullable,bundled_egress_bytes,price BillingMoney,pack_min1,pack_max20. Total order packages max20 по всем lines, check multiplication bigint overflow |
| BillingQuote | id,kind,expires_at,summary_revision,request_hash,lines array(ordinal,target_id,pack_count,resource_code,quantity,expiry/effective dates,price),total BillingMoney,effective_at,provider_action checkout/new_invoice/none |
| BillingOrder | id,status,payment_status pending/succeeded/failed/refunded,fulfillment_status pending/applying/applied/compensating/closed,total,created_at,updated_at,action nullable(kind,url,expires_at). URLs owner-only, no-store, never replay expired capability из API receipt |
| BillingChange | id,type,status,effective_at,revision,order_id nullable. Provider effect state отдельно typed step status, не enum смешивающий internal/external версии |
| BillingUsagePage | items typed buckets/ledger records по endpoint, next_cursor nullable,summary_revision. bucket fields id,resource,remaining,allocated,consumed,expires_at nullable,expiry_policy,source_label; без provider account/payment IDs |
| LegalRequirementSet | set_hash,locale,documents array(id,key,version,url,content_sha256,required_action accepted/acknowledged),optional_purposes array(code,enabled_by_default=false) |
| LegalAcceptance | document_set_hash,events array(document_version_id,action),client_event_id. Optional preferences идут отдельным PATCH, не обязательным bool accepted_all |
| PrivacyPreferences | revision,analytics bool,marketing bool,updated_at |
| DeletionAccepted | request_id,status,deadline_at,status_capability (только первый безопасный ответ либо recovery через fresh auth),status_expires_at. Не возвращать PII или чужие tenant details |

- Маршруты в 04 имеют следующие уточнения.
- JSON key/expected_revision не используются;
- Idempotency-Key=1..128 ASCII33..126, If-Match='"revision"'.
- Missing mutation key400, missing required If-Match428, malformed422, conflict409.
- Финансовая dedup не зависит от API receipt TTL, исходные receipts сохраняются 24h minimum; бизнес order/grant uniqueness действует весь retention lifetime.

| Endpoint | Request/query | Success и schema |
|---|---|---|
| GET /api/v1/billing/catalog | locale en/ru/bg,currency EUR |200 BillingCatalog |
| GET /api/v1/tenants/{t}/billing/offers | resource_code? |200 items BillingOffer, catalog_revision; owner only |
| GET /api/v1/tenants/{t}/billing | none |200 BillingSummary, ETag billing profile revision |
| GET /api/v1/tenants/{t}/billing/usage | resource_code?,limit,cursor |200 balances/storage/as_of; owner/admin |
| GET /api/v1/tenants/{t}/billing/buckets | resource_code,limit,cursor |200 BillingUsagePage |
| POST /api/v1/tenants/{t}/billing/quotes | kind; target_plan_version_id?; lines array(offer_id,pack_count)?; subject kind/subscription change exactly one choice. If-Match summary revision, key |201 BillingQuote; stale quote не создаёт order |
| POST /api/v1/tenants/{t}/billing/orders | quote_id. key,If-Match profile revision |202 object order_id,status=created. Только stable receipt, без signed URL; GET order возвращает свежий action |
| GET /api/v1/tenants/{t}/billing/orders/{order} | none |200 BillingOrder |
| POST /api/v1/tenants/{t}/billing/changes | quote_id. key,If-Match profile revision |202 BillingChange; не допускается second order for same quote |
| POST /api/v1/tenants/{t}/billing/changes/{change}/cancel | empty object,key,If-Match change revision |200 BillingChange |
| POST /api/v1/tenants/{t}/billing/portal | return_route enum billing/profile; key |202 portal_action_id,status=pending. Новое GET /billing/portal-actions/{action} возвращает 200 action state+fresh URL, private/no-store; replay не сохраняет истёкший secret URL |
| POST /api/v1/tenants/{t}/billing/cost-quotes | workflow_kind,selection_snapshot_id,parameters_hash. key |201 cost quote id/revision/items/max cost. Existing workflow mutation принимает optional billing_cost_quote_id только для beta compatibility, mandatory enforce cohort |
| POST /api/v1/webhooks/stripe/{account} | raw provider body/signature, нет user cookies/CSRF exemption только здесь |200 received:true после durable inbox; duplicate тот же ответ;5xx при несохранении |
| GET /api/v1/legal/requirements | locale,account_kind business |200 LegalRequirementSet |
| POST /api/v1/me/legal/acceptances | LegalAcceptance,key |200 onboarding_state,accepted_set_hash,revision |
| PATCH /api/v1/me/privacy | analytics?,marketing?,хотя бы одно; key,If-Match preference revision |200 PrivacyPreferences |
| POST /api/v1/me/recent-auth | purpose,verified fresh OTP proof binding,key |201 opaque purpose-bound grant,expires_at; не создаёт вторую logged-in identity |
| POST /api/v1/me/deletion/preflight | key |200 privacy_case_id,revision,owned tenant choices,required recent-auth purpose |
| POST /api/v1/me/deletion | privacy_case_id,recent_auth_grant,ownership_resolution array(tenant_id,tenant_revision,action close/transferred,transfer_receipt_id?);key |202 DeletionAccepted; перед commit проверить ownership; revoke только после полного accepted plan |
| GET /api/v1/me/deletion/{request}/status | scoped status token в Authorization отдельной схемы, не query |200 state,steps categories,retained categories/deadlines; no owner API required |

- GET /api/v1/tenants/{t}/billing/portal-actions/{action} — tenant owner authority плюс action scope.
- Support таблица billing_portal_actions: id,tenant_id,owner_user_id,provider_account_id,state pending/ready/failed/expired,provider_request_key UNIQUE,encrypted_url nullable,expires_at nullable,error_code nullable; receipt не включает URL, erasure очищает envelope.

- Смена state/action после 202 не переписывает exact replay body.
- UI использует order/status resource, не ждёт «обновлённый replay».
- Quote и change command не два способа оплатить одно и то же: quotes kind plan_change принимаются только `/changes`, остальные только `/orders`; change создаёт единственный связанный order внутренне.
- Unique accepted_quote_id на order и на change исключает две покупки одного quote.
- Unit purchase денег в cost-quotes отсутствует, это usage confirmation.

## 8 Bootstrap legal erasure без разрыва auth

- OTP создаёт обычную active user session как сейчас.
- `account_onboarding_states=pending` закрывает tenant creation, paid checkout, uploads/новые processing commands, но разрешает `/me`, legal requirements/acceptances, privacy, fresh auth, deletion/preflight/status/logout и доступ к предусмотренному export прав.
- Для existing reacceptance read-only существующий архив доступен по policy, paid commands закрыты.
- AuthorizeAccess не должен отказывать pending user до возможности принять документы.
- Onboarding completion изменяет отдельный state и receipt, не хранит «privacy consent=true».

- Recent-auth proof создаётся только новой успешной OTP проверкой, привязан к user/session/auth_epoch/purpose, срок 5min, одноразовый.
- Existing refresh session.created_at/last refresh не proof.
- Формат verifier проектируется как extension текущего OTP recovery с новыми purpose/domain separation, не reuse onboarding bootstrap для другой identity.
- При deletion commit users.status=deleting, auth_epoch++, session revoke и invalidation OTP receipt epoch в одной TX.
- Очищать email_normalized можно через unique неPII tombstone `deleted+<random uuid>@invalid` на время FK cleanup; реальный email не оставлять в unique index.
- Новый signup тем же email после erasure — новая identity; прежние tenant/Trial/history не привязываются автоматически.

- Ownership transfer использует текущий единственный owner_user_id и active target membership.
- Вариант «другой owner уже есть» в общем виде неприменим: до deletion должен быть подтверждён TransferOwner.
- После erasure commit coordinator не действует как отозванный owner, а исполняет persisted privileged close plan с проверенным epoch.
- Многопользовательский tenant после transfer не получает deleting billing mode из-за удаления бывшего owner; заменить contact/person attribution и сохранить законные studio права.

- Privacy case регистрируется до решения ownership; deadline действует даже если erasure_requested ещё не принята.
- Пользователь не должен терять возможность заявить права.
- Если self-service resolution не выполнен, case направляется privacy operator с конечным SLA, без заявления «стёрто».
- Status capability только для собственного progress, rate-limited, хранится hash и короткий срок с безопасным recovery, не даёт купить/передать tenant/скачать фото.

## 9 Порядок начала реализации и закрытые gate

- Генерация 1: добавить executable DTO/source registry и additive schema catalog с signatures/ACL, запретом сетевых side effects из SQL; выполнить drift/schema checks.
- Генерация 2: M01–M05, idempotency/lease/financial skeleton и чистые state-machine tests.
- Генерация 3: default-disabled adapters в existing task admission/adoption и controlled delivery без обхода security.
- Генерация 4: Paper screens, legal/auth scoped flow и erasure coordinator.
- Генерация 5: isolated Stripe Sandbox provisioning, реальные provider/e2e + restore evidence.
- Не помечать готовыми operation pricing и live legal settings только потому, что scaffold сгенерирован.

| Неопределённое коммерческое решение | Безопасное поведение генерации сейчас | Условие включения |
|---|---|---|
| Тяжёлые operation prices/standard registry allowlist | Unknown policy denied в billing-enforced cohort, beta отдельный finite policy | Явный перечень context/canonical charge + benchmark; existing v2 disabled не активировать billing миграцией |
| Max file/output/concurrency | Переиспользовать qualified текущий safety profile; draft plan не обещает непроверенных значений | Per-plan published finite limits с evidence; no new unlimited fallback |
| Юридические сроки и PhotoEditor legal scope | Draft retention, синтетические fixtures; erasure необязательной PII не откладывать | Approved сроки/тексты/version archive и privacy review |
| Tax вне BG test fixture |422 до Stripe order | Approved TaxResolver и acceptance test страны/статуса покупателя |
| Native purchase distribution | Capability false, Paper UI read-only purchase explanation | Channel-specific implementation/qualification и проверка правил распространения |
| Долгая неоплата архива | Overquota, без auto age purge | Published уведомления/read-export grace/manual close process |

Это границы включения, а не просьба к кодогенератору придумать цены/налог/срок. Первый PR может быть полностью конкретным и reviewable без изменения этих продуктов. После него генерация следует ID BILL-01–14, E01–90 и дополнительному traceability разделу 10.

## 10 Снимки и внешние эффекты

- Provider HTTP recovery требует новой typed таблицы billing_provider_effects: id uuid PK,financial_subject_id FK,provider_account_id FK,order_id UUID nullable,change_request_id UUID nullable,deletion_step_id UUID nullable,operation create_customer/create_checkout/create_invoice/update_subscription/refund/cancel_subscription/create_portal_session/redact_customer,provider_request_key text UNIQUE(account,key),request_hash bytea32,request_envelope_encrypted bytea,state pending/running/succeeded/retry/failed/compensated,attempts int>=0,lease_token uuid nullable,lease_until timestamptz nullable,external_object_id text nullable,next_retry_at timestamptz nullable,last_error_code text nullable.
- Ровно один command owner(order/change/deletion step) либо customer bootstrap subject; ownership и account проверяются.
- Успешный effect replay возвращает refs, не повторяет HTTP с новым key.
- Отказ между provider success и сохранением refs требует retrieval/reconciliation того же effect.

- Immutable snapshots имеют schema_version1 и closed поля.
- ResourceSnapshot: resource_code,unit,quantity,expiry_policy,starts_at,expires_at nullable,operation_policy_id nullable,origin_period_id nullable.
- PriceSnapshot: local_price_id nullable,offer_id nullable,provider_binding_id nullable,currency,net_minor,tax_minor,gross_minor,tax_policy_version,tax_lines.
- ChangeSnapshot: source_plan_version,target_plan_version,source_entitlement_revision,effective_at,anchor_start,anchor_end,delta_resources array ResourceSnapshot,price PriceSnapshot.
- Nullable поля явно присутствуют; client JSON неизвестных полей не становится server snapshot.
- Hash — canonical JSON SHA256 через существующий canonical package, wire hex и internal bytea conversion явные.

- billing_invoice_allocations.provider_account_id обязателен FK, согласован composite FK с invoice; billing_entitlement_allocations.business_key включает account/invoice line/resource/period/allocation kind, для rollover parent bucket/next period/transfer reference.
- Один invoice line storage порождает capacity и egress children без двух денежных оплат.
- Late recurring payment после expiry не продлевает photo period автоматически: grant имеет исходный paid period/expiry и затем штатный rollover/expiry, future allowance не выдаётся задним числом.

- Anonymous cookie choice хранится device-local с purpose/version/timestamp до login, optional SDK по умолчанию отсутствуют.
- После login explicit выбор синхронизируется как privacy event; новый device без local выбора не активирует optional SDK только потому, что server preference true.
- Server withdrawal запрещает дальнейший SDK start на всех устройствах, локальный consent дополнительно необходим там, где применим.
- При отсутствии optional SDK banner не имитирует сбор несуществующих analytics.
- Это не влияет на обязательный onboarding contract gate.

## 11 Уточнения миграции и полноты контрактов

- Schema FK dependencies строятся в два прохода: создать supporting tables, затем composite FK/cyclic nullable links (free_slot ↔ charge_item, change ↔ order/subscription).
- У неподтверждённого поля ссылки допустимы только явные nullable состояния; не использовать orphan UUID вместо FK.
- Constraint validation выполняется до включения consumer функций. catalog_scope проверяется по composite FK/validator в version→price→offer→binding→quote цепочке; клиент не может совместить sandbox цену и live order.

- billing_orders/billing_change_requests.idempotency_key — внутренний business command key, полученный из action kind+principal+route+Idempotency-Key, а не raw header с глобальной tenant уникальностью. quote accepted identity уникальна независимо от повторного API key.
- Финансовый grant key никогда не зависит от пользователя, route alias или API receipt expiry.

- Для scope FREE operation `free_beta` нужны finite separate internal budget и safety gates; нельзя выдавать unlimited free tickets.
- Для `included_standard` дополнительный debit шага 0 разрешён только при наличии уже accepted standard root charge item.
- Standalone такой же runtime operation без root использует отдельный `charge_context` policy или denied.
- Внутренние шаги, exports и auth не становятся платными по отсутствующему policy: unknown commercial USER command блокируется в enforce cohort, internal execution продолжает только с валидным root admission proof.

- Accounting periods создаются и закрываются maintenance/paid period function, не каждым GET summary.
- Trial standard_photo никогда не выдаётся заново при новом period.
- Photo top-up90days не меняет allowance period.
- Передача tenant другому owner не обнуляет Trial eligibility/overrun/allowance и не создаёт новые buckets.

- В исходных документах фраза «stored function планируется» означает контракт для следующего этапа, а не обязанность вызвать SQL с ещё несуществующей сигнатурой.
- Первым implementation deliverable является machine-readable catalog, mechanical validation которого выводит exact typed function signatures из настоящих полей; actor/authority result обязателен.
- Runtime включение до создания/проверки этих schemas запрещено.
- Это позволяет начать кодогенерацию catalog и закрытого каркаса без незаявленных продуктовых решений.

## 12 Совместимость существующих download validators

- Проверены `apps/photographer/src/delivery-download.ts`, `apps/portal/src/lib/download-link.ts` и `apps/portal/src/lib/response.ts`: текущие download/media validators требуют ровно один непустой versionId в query, finite expiry примерно 60 секунд и безопасную схему URL.
- Поэтому controlled URL ОБЯЗАТЕЛЬНО сохраняет `?versionId=<exact bound physical version>`; capability привязан к этой версии, сервер отклоняет substituted/duplicate/null versionId.
- Версия не заменяет authority token.
- Не добавлять новые поля к текущему пяти-полевому DownloadLink и не менять его bytes с number на string: bigint strings относятся к НОВЫМ billing DTO, existing download DTO сохраняет свои safe integer bounds.

- Controlled origin и backend route policy валидируются новым узким delivery signer/validator; не ослаблять глобальный objectstore.ValidSignedReadURL до «любой https».
- Existing filename/SHA/bytes/TTL/current-read authority проверки остаются.
- HTTP60s capability TTL ограничивает начало отдачи; уже начатый stream живёт по finite transport policy с chunk renewal и revoke checks, а не теряет весь ZIP только из-за истечения initial URL.
- Старый redemption токен остаётся одноразовым, controlled GET retries создают отдельный delivery attempt и учитывают повторно отправленные bytes.
- Для media links сохранить исходный DTO shape и content-type/CORS/CSP: backend transport route входит в разрешённые origins, без необходимости менять UI apps/portal.
