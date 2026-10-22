# Контракты функций и API

## Правила исполнения

Имена функций ниже целевые, до реализации сверить реальные сигнатуры и generator workflow. SQL функции не обращаются по HTTP в Stripe, storage или email. Одна локальная транзакция фиксирует состояние и outbox; внешнее действие выполняет сервис с persisted idempotency key. Повтор с тем же key и тем же canonical request hash возвращает сохранённый результат; другой hash даёт 409 IDEMPOTENCY_CONFLICT.

Actor/tenant берутся из authenticated context. Client-supplied actor не является доказательством прав. Для финансовых действий действующая роль owner; отдельный billing-admin можно включить только после расширения существующей role model и тестов. Для admin summary read-only без PII/invoices/payment links; editor/viewer имеют workflow capability hints, но не billing summary.

Перед доменными locks при mutation использовать текущий idempotency receipt lock после preflight authorization и регистрации authority revalidation gates; затем порядок обязан согласовываться с текущим lock order проекта: tenant lifecycle → billing entitlement/usage → run и quota holder → task и terminal adoption. Нельзя вставить billing lock после уже взятого run lock, если другая функция делает наоборот. До кодирования составить один полный lock graph всех затронутых entrypoints; settlement встраивать в canonical adoption, а не отдельным callback с обратным порядком. Stripe HTTP и object I/O никогда не держат SQL row locks.

Новые функции возвращают типизированный result с revision, business references и статусом. Финансовые ошибки не содержат secrets/PII. Неизвестный исход commit восстанавливается повтором того же запроса с тем же Idempotency-Key и canonical body, без новой покупки. Existing idempotency.Use возвращает точные сохранённые bytes/status после актуальной authority validation; финансовая business dedup живёт независимо от TTL API receipt.

## Хранимые функции каталога и финансов

| Функция | Вход → выход | Транзакционные действия и отказы | Повтор и права |
|---|---|---|---|
| billing_publish_plan_version | version_id, expected_revision → published version | Проверить полный набор лимитов, retention manual, price, tax mode, operation mappings и capacity gates; freeze hash; неполный Custom/selfservice отказ | Только catalog operator; published exact replay без изменений |
| billing_bind_provider_price | account_id, local_target, Stripe verified snapshot hash → binding | Проверить XOR target, EUR, amount/interval/tax_behavior, active account; secret не принимается | Operator provisioning; unique local target/account |
| billing_ensure_profile | tenant, bootstrap_policy, key → profile | Lock lifecycle; создать trial/beta profile и один trial grant только по выбранной migration policy; deleting отказ | Существующий profile возвращается; internal bootstrap |
| billing_create_quote | tenant, actor, selection, expected_revision, key → immutable quote | Читать только опубликованные разрешённые offers; проверить country/tax input; сохранить amounts, limits, effective date, expiry и policy hash | API owner; foreign offer/hidden plan/нулевой pack count отказ |
| billing_accept_quote | quote_id, actor, key, hash → order+change | Revalidate role, lifecycle, revision, expiry и price mapping; freeze order lines; поставить Checkout create outbox | Duplicate key возвращает order; смена цены требует нового quote |
| billing_attach_checkout | order_id, provider session snapshot, operation_key → order | Проверить account/customer/mode/currency/order ownership и ещё не привязанный другой session | Provider worker; unique session; no grant |
| billing_record_webhook | verified account+event envelope+hash → inbox_id/duplicate | Сохранить durable inbox; одинаковый event с другим hash quarantine/security alert | Только webhook ingress после signature verification; duplicate 2xx |
| billing_claim_webhook | worker_id, bounded batch → event+fence | SKIP LOCKED, ограниченный lease; attempts++; expired claim можно переполучить | Maintenance worker; stale fence не пишет результат |
| billing_apply_provider_snapshot | inbox_id,fence,verified object snapshot → applied/retry/ignored | Проверить account/customer/tenant/order и authoritative payment state; monotonic business state; обновить invoice/payment/subscription; outbox fulfillment | Один event может описывать уже применённую оплату: grant не дублируется |
| billing_grant_paid_allocation | invoice_allocation_id, payment proof → grant refs | Проверить фактическую оплату, line/resource/period, no pending erasure; создать bucket или capacity; cover overrun; изменить entitlement revision и outbox | Unique business grant key, не event_id; только fulfillment worker |
| billing_request_change | tenant, quote_id, expected_revision,key → change | Только один unresolved immediate change; downgrade future date; upgrade payment pending | Owner; concurrent tab conflict с current revision |
| billing_apply_paid_upgrade | change_id, verified invoice allocation → applied/applying | Проверить source/target и renewal race; atomic local delta quota+capacity+revision после подтверждения provider price update по saga | Exact replay no new quota; Stripe update не вызывается из SQL |
| billing_apply_period_boundary | subscription, paid_period allocation → grants+changes | Pending downgrade/cancel; close old period; next grant; generation 0 rollover только при непрерывной оплате; add-ons independent | Unique subscription/period/line/resource; failed renewal не даёт новую квоту |
| billing_apply_refund | refund_id, verified refund state → reversal refs | Отозвать unissued остатки пропорционально строкам refund; issued funding пометить для correction; policy consumed handling | Только provider finance worker; partial repeat не отзывает дважды |
| billing_assign_custom | tenant, complete assignment+limits,actor,reason,key → assignment | Полнота, dates, amount, manual/Stripe, no overlap; публичность false; atomic entitlement revision | Privileged audited operator; не owner public API |
| billing_claim_outbox / billing_finish_outbox | batch/fence → messages; message/fence/outcome → state | SKIP LOCKED claim; retry/backoff; stale finish отказ; dead-letter evidence | Только соответствующий service worker; at-least-once recipients |

## Хранимые функции квот

| Функция | Вход → выход | Обязательное поведение |
|---|---|---|
| billing_allocate_admission_lease | tenant,resource,period,instance,generation,policy_version,key → lease+fence+allocations | Lock aggregate/buckets; проверить live lifecycle; earliest-expiry allocations; уменьшить remaining и увеличить leased; allowance резервируется из billing_allowance_budgets; issued/settled allowance в period не сбрасывается после закрытия lease |
| billing_issue_ticket | lease_id,fence,request_id,run_id,quantity → ticket | В existing run admission TX проверить lease expiry/fence, tenant, operation policy, remaining local allocation и current security authority; сохранить ticket+allocation+outbox; никакого полного billing catalog read |
| billing_settle_ticket | ticket,canonical_result,terminal_proof,quantity → settlement | В adoption TX удостовериться в доступном accepted result; terminal CAS; перевести leased→consumed ровно один раз; allocation allowance в overrun; partial batch по каждому фото/operation |
| billing_release_ticket | ticket,terminal failure/cancel proof → release | Не отменять adopted result; вернуть funding в исходный bucket с исходным expiry; если expiry прошёл, сразу expire. Восстановленный technical retry сохраняет исходный ticket, не делает новый |
| billing_close_lease | lease,fence,recovery proof → returned+pending | Истёкший lease не разрешает новый issue; вернуть только невыданный quantity; issued stays attached until terminal proof; stale instance refused |
| billing_expire_buckets | tenant/resource,server time,cursor,batch → cursor/count | Только remaining истёкших buckets; issued commitments не уничтожать; без двойного expire; не full table scan в request path |
| billing_rollover_bucket | origin_bucket,next_paid_period → next bucket | Только generation0 remainder, один раз; outstanding tickets исключены из transferred quantity; late release может добавить transfer только до next period end; generation1 не переносить; no continuous payment=no rollover |
| billing_cover_overrun | tenant,resource,new_grant_ref → covered quantity | Погасить соответствующий resource overrun до открытия новых paid admissions; ledger обеих сторон, no negative bucket; не выставлять деньги |
| billing_record_egress_chunk | delivery_id,attempt_id,sequence,ticket,written_bytes,receipt_hash → consumed/uncertain | Проверить bounded allocated bytes, count body only; unique delivery/sequence; partial chunk terminal flush; failure повторяется через durable receipt |
| billing_project_usage | tenant,resource,cursor → summary | Вычислить paid grant/leased/consumed/overrun и blocked; revision монотонна; UI updated_at; rebuild и live projection дают одинаковый итог |
| billing_apply_storage_entitlement | paid allocation+period+revision → effective capacity | Обновить commercial ceiling в единственном tenant storage policy через действующий admission contract; active reservation не потерять; category projection отдельно |
| billing_reconcile_storage | tenant,cursor,verified object inventory → report | Сравнить tenant_usage/reservations/object versions; несовпадения quarantine/report, не произвольный отрицательный correction; maintenance не открывает новое entitlement |
| billing_register_photo_success | tenant,photo_identity,charge_item,result,policy → success audit | Проверить admission free-slot/paid decision; first_success открывает окно и два free slots; не менять согласованную цену по порядку завершения; technical retries не новые user requests; внутри settlement TX |

Коммерческая порция, run ticket и техническая quota_reservation — разные уровни одной admission цепочки. Tickets не отменяют reserve bytes/files. Сумма paid bucket remaining+leased+consumed+revoked сохраняется; при settlement consumed возрастает ровно на уменьшение leased. При allowance ledger bucket_id отсутствует и отдельный overrun увеличивается. Внешняя цена никогда не выводится из суммы технических reservations.

## Юридические функции

| Функция | Вход → выход | Правила |
|---|---|---|
| account_accept_legal | user,document versions,explicit actions,client_event → onboarding status | Требуемые active version/hash, locale и DPA для B2B; contract accepted отдельно от privacy acknowledged; optional false по умолчанию; atomic account activation |
| account_update_privacy_preferences | user,expected_revision,analytics/marketing,key → preferences | Event на каждое изменение; withdraw без потери основной услуги; новые consent-dependent SDK запрещены после commit |
| account_request_deletion | user,recent-auth proof,key,ownership choices → deletion request | Проверить свежий purpose-bound proof и завершённый ownership preflight; на erasure commit увеличить auth_epoch, отозвать сессии/refresh/device tokens и блокировать только closing subject payments; общую transferred студию не удалять |
| account_claim_deletion_step | bounded batch → step+lease+fence | Retry после crash, сохранённая saga; secrets в status API не возвращать |
| account_record_deletion_step | request,step,fence,verified evidence → progress | Внешний deletion подтверждён; retained reason+deadline обязательны; stale worker отказ |
| account_finalize_deletion | request,current proof set → completed/partially_retained | Проверить PII inventory, external tasks, backup tombstones и legally retained exceptions; физически не удалённые данные не называть полностью стёртыми |

## Сервисные обязанности

BillingCatalog отвечает за published данные и hidden Custom. QuoteService — налоги, цены, rounding и snapshots. CheckoutService — customer/session creation и recovery после неизвестного Stripe outcome. SubscriptionChangeSaga — paid upgrade, renewal race и compensation. WebhookIngress — raw signature verify и durable receipt. BillingProcessor — authoritative Stripe lookup, financial dedup и grant. MeteringService — lease cache/fence/tickets. ReconciliationService — периодическая сверка Stripe/DB и ledger/projections. LegalService — версии документов, события и onboarding gate. ErasureCoordinator — ownership, database/storage/Stripe/cache/backup шаги.

Предлагаемые интервалы: inbox/outbox retry exponential с jitter 1s→1min, затем до 15min; dead-letter после 24h с alert, не молчаливое прекращение. Reconciliation incremental каждые 5 минут с overlap 1 час; полный scoped audit ежедневно. Это configurable defaults. Мягкая недоступность Stripe не должна блокировать чтение готовых файлов по уже известным правам.

## Предлагаемые API операции

Пути — канонические целевые identities для внесения в source registry/contracts, не считаются существующими. Поля key и expected_revision в таблице — логические входы: Idempotency-Key и If-Match headers, а не дублирующие JSON поля. Точные DTO и success status определены в 09. Все tenant operations имеют authenticated tenant scope.

| Метод и путь | Вход | Ответ и доступ |
|---|---|---|
| GET /api/v1/billing/catalog | locale,currency | Public published plans/offers; Custom исключён; cache version |
| GET /api/v1/tenants/{t}/billing | none | Current plan, prices/status/period, changes, capability flags, PII только owner |
| GET /api/v1/tenants/{t}/billing/usage | resource,cursor? | used/leased/available/overrun, storage categories, egress, updated_at и revision; owner/admin read-only policy |
| GET /api/v1/tenants/{t}/billing/buckets | resource,cursor | Paginated buckets+expiry; owner/admin |
| POST /api/v1/tenants/{t}/billing/quotes | selection,counts,expected_revision,key | Quote id/hash/expires, строки, gross/net/tax, rights and effective dates |
| POST /api/v1/tenants/{t}/billing/orders | quote_id,Idempotency-Key,If-Match |202 stable order_id/status; hosted action URL через GET order; owner only |
| GET /api/v1/tenants/{t}/billing/orders/{order} | none | Local authoritative status, safe next action; foreign 404 |
| POST /api/v1/tenants/{t}/billing/changes | quote_id,key,expected_revision | Scheduled/applied/pending change; owner |
| POST /api/v1/tenants/{t}/billing/changes/{change}/cancel | expected_revision,key | Cancel future change; paid saga требует compensation, не простой delete |
| POST /api/v1/tenants/{t}/billing/portal | return_route enum,Idempotency-Key |202 portal_action_id; fresh short-lived URL через GET action; owner; no-store |
| POST /api/v1/webhooks/stripe/{account} | raw body + signature | 2xx только после durable inbox; invalid signature 400; storage failure 5xx |
| GET /api/v1/legal/requirements | locale,account kind | Active document URLs/versions/hash; no remote fetch per click |
| POST /api/v1/me/legal/acceptances | versions,explicit actions,key | Onboarding complete/pending; no auth activation without contract |
| PATCH /api/v1/me/privacy | expected_revision,optional preferences,key | Updated preference + event receipt |
| POST /api/v1/me/deletion | fresh auth,ownership resolution,key | 202 request_id + limited status capability; avoid email in URL |
| GET /api/v1/me/deletion/{request}/status | scoped status token | Own request progress/retention exceptions; no global lookup |

Ресурсные quantities и amounts на wire строки для bigint. Ошибка сохраняет существующий envelope: code, message_key, args, request_id. resource_code, needed, available, quote_revision и retryable размещаются в args с versioned schema. Не вводить параллельные top-level correlation_id/retryable. X-Request-ID соответствует request_id. Основные codes: QUOTA_EXCEEDED, STORAGE_OVER_QUOTA, EGRESS_EXCEEDED, OPERATION_NOT_PRICED, PLAN_NOT_AVAILABLE, OFFER_NOT_ELIGIBLE, QUOTE_EXPIRED, BILLING_REVISION_CONFLICT, BILLING_STATE_UNAVAILABLE, LEGAL_REACCEPTANCE_REQUIRED, ACCOUNT_DELETING. Sensitive ошибки внешнего Stripe нормализуются, raw messages остаются только в restricted diagnostics.

## Дополнительные функции обязательной цепочки

| Функция | Вход и результат | Атомарность и права |
|---|---|---|
| billing_create_cost_quote | actor/tenant,selection snapshot,workflow/params hash → finite max-cost lines | API с текущей workflow authority; не выдаёт ресурсы и не снимает деньги |
| billing_admit_usage_request | cost_quote_id,request hash,lease allocations,root/child links → accepted receipt | API/scheduler wrapper; multi-resource/batch gate до publish; free slots и tickets в одной TX; same request replay |
| billing_hold_photo_slot | tenant/photo,charge_item,server time → free slot либо paid decision | Внутри admission; current first-result/window check; initial in flight409; технический retry тот же item |
| billing_transfer_rollover_remainder | origin bucket,next paid period,unique terminal reference → transferred quantity | Maintenance/settlement; только nonleased свободный quantity; late failure добавляет к тому же child bucket один раз |
| billing_open_delivery | existing redemption authority+physical object proof → opaque capability | Private delivery adapter; no direct object URL; TTL/current permission повторяются |
| billing_reserve_delivery_chunk | delivery,attempt,sequence,requested bytes → ticket/finite allocation | Delivery maintenance-scoped adapter; allowance cumulative; no chunk without current delivery permission |
| billing_reconcile_uncertain_chunk | receipt hash,verified transport evidence → confirmed/released/quarantine | Maintenance only; без proof нет возврата в available и нет billable full object charge |
| account_prepare_deletion | user,recent authenticated session → privacy case+ownership choices | Auth boundary до revoke; case deadline обязателен; чужие tenant details не раскрываются |
| account_issue_recent_auth | verified fresh OTP purpose binding → opaque one-use proof | photo_auth, user/session/epoch/purpose bound; refresh не доказательство |

Полные SQL signatures в будущем functions.json включают explicit input/output PostgreSQL types, SECURITY DEFINER owner/search_path, allowed executor и вызываемые private helpers. Публичный API hash не равен свободной строке actor proof. New callable function без этих деклараций и negative ACL test не готова к подключению.
