# Структура данных биллинга и приватности

## Общие правила

- Это целевая логическая схема для последующей реализации в существующей PostgreSQL схеме `photo`.
- Перед созданием сверить текущий каталог и переиспользовать существующую эквивалентную сущность.
- Имена здесь обязательны как предметные контракты; переименование требует traceability.
- Исполняемого SQL в документации нет.

- Все новые таблицы имеют `id uuid PK`, `created_at timestamptz NOT NULL` по серверному времени, кроме явно указанных natural PK.
- Mutable строки дополнительно имеют `updated_at timestamptz NOT NULL`, `revision bigint NOT NULL DEFAULT 1 CHECK >0`.
- Immutable rows не редактируются бизнес-ролями.
- Все поля ниже NOT NULL, если не отмечены `?`; отсутствие значения не означает unlimited.
- `enum` означает закрытый набор, реализуемый enum или CHECK.
- Деньги — bigint minor units >=0, currency char(3)=EUR на старте.
- Quantity и bytes — bigint >=0; дробных credit нет.
- SHA-256 — bytea длины 32, версии digest указываются явно.
- Timestamp UTC, interval=[start,end), expires_at исключительный.
- На wire bigint идут десятичными строками, а не JS float.
- Цены/лимиты опубликованной версии immutable.

- Все tenant scoped таблицы имеют `tenant_id uuid FK tenants`, `UNIQUE(tenant_id,id)`, FORCE RLS и cross-tenant FK с tenant_id.
- Даже UUID соседнего tenant не принимается.
- Финансовые сохранённые записи используют отдельный ограниченный retention scope и обезличенный subject reference после удаления tenant; им нельзя навязать CASCADE на все invoices.
- Ссылки пользователя после erasure становятся nullable/pseudonymous по утверждённой retention policy.
- Уникальность Stripe refs всегда включает provider_account_id и environment; нельзя смешивать два sandbox с одинаковым режимом test.

- Для каждого справочника ниже common fields действуют также.
- Цена в snapshots включает net, tax, gross и currency; gross=net+tax и сумма строк=order totals.
- Налог не всегда 20%; rounding сохраняется построчно.
- `policy_snapshot jsonb` допустим только с versioned JSON schema и hash; неизвестные поля отклоняются.
- Arbitrary JSON не заменяет typed FK и суммы.

## Каталог и политики

| Таблица | Поля кроме общих | Ограничения и индексы |
|---|---|---|
| billing_plans | code text; name_key text; description_key text; is_public bool; self_service_enabled bool; is_active bool; sort_order int | UNIQUE(code); Custom никогда public/self_service; self_service требует published complete version |
| billing_plan_versions | plan_id uuid FK; version int; effective_from timestamptz; status draft/published/retired enum; policy_hash bytea | UNIQUE(plan_id,version,catalog_scope); interval валидности не пересекается для default version; published immutable |
| billing_plan_prices | plan_version_id FK; version int; currency; amount_net_minor bigint; interval once/month enum; tax_behavior exclusive enum; is_default bool | UNIQUE(version,plan_version_id,currency,interval); не более одного default на сочетание версии/валюты/interval |
| billing_resource_types | code text; unit credit/byte/count/enum; kind consumable/capacity/policy; value_type integer/enum; description text | UNIQUE(code); operation codes с namespace; capacity не смешивается с credit |
| billing_plan_limits | plan_version_id FK; resource_code FK; integer_value bigint?; enum_value text?; grant_period once/month/none; rollover_periods int | UNIQUE(version,resource); ровно одно typed value; storage=capacity, retention_mode=manual; rollover 0 или 1 |
| billing_topup_offers | plan_id FK; offer_code text; version int; resource_code FK; quantity bigint; amount_net_minor bigint; currency; billing_kind one_time_credit/recurring_capacity; interval once/month; ttl_days int?; expiry_policy fixed_ttl/period_end/none; bundled_egress_bytes bigint; is_active bool | UNIQUE(plan_id,offer_code,version,catalog_scope); quantity>0; recurring без ttl; expiry_policy согласован с kind; index(plan_id,is_active) |
| billing_provider_accounts | provider stripe; environment test/live; external_account_id text; sandbox_reference text?; api_version text; webhook_format snapshot/thin; enabled bool | UNIQUE(provider,environment,external_account_id,sandbox_reference) NULLS NOT DISTINCT; только ссылки на secret store, самих секретов нет |
| billing_provider_prices | provider_account_id FK local account; price_id FK?; offer_id FK?; external_product_id text; external_price_id text; verified_at timestamptz | XOR price/offer; UNIQUE(account,external_price_id); unique binding на local target/account; соответствие currency/interval/amount проверяет provisioning |
| billing_operation_policies | operation_code text; operation_version text; policy_version int; billing_class included_standard/paid_operation/free_beta/disabled; resource_code FK?; units_per_success bigint; success_contract text; rerun_policy standard_window/none; beta_budget bigint?; effective_from timestamptz; benchmark_ref text? | UNIQUE(operation_code,operation_version,policy_version); paid requires resource+benchmark+success contract; disabled не создаёт ticket; включённая операция не имеет paid debit |
| billing_admission_policies | resource_code FK; policy_version int; chunk_quantity bigint; lease_seconds int; cache_ttl_seconds int; optimistic_allowance bigint; max_local_leases int; cold_timeout_ms int; enabled bool | Все bounds конечные; chunk>0; allowance>=0; UNIQUE(resource_code,policy_version); immutable version |

## Профиль и подписки

| Таблица | Поля | Инварианты |
|---|---|---|
| tenant_billing_profiles | tenant_id PK/FK; current_plan_version_id FK; current_price_id FK?; billing_mode beta/trial/stripe/manual/deleting; financial_subject_id uuid; billing_contact_id uuid?; entitlement_revision bigint; transition_at timestamptz? | Price относится к версии; нет Stripe customer ID без account namespace; beta переход только явный |
| billing_contacts | financial_subject_id uuid; email text?; legal_name text?; company_name text?; tax_id text?; country char(2)?; postal_address jsonb?; encryption_key_ref text; erased_at timestamptz? | PII отделён от ledger; адрес имеет закрытую schema; минимум для invoicing по налоговой конфигурации; restricted reads |
| billing_customers | tenant_id FK; provider_account_id FK; external_customer_id text; financial_subject_id uuid; state active/deleting/deleted | UNIQUE(account,external_customer_id); UNIQUE(tenant,account); metadata без email/фото |
| billing_periods | tenant_id FK; provider_account_id FK?; subscription_id uuid?; starts_at timestamptz; ends_at timestamptz; anchor_kind trial_calendar/subscription; state open/closing/closed; predecessor_id uuid? | end>start; исключить пересечение активных entitlement периодов одного anchor; index(tenant,ends_at); invoice allocation предотвращает двойной egress |
| billing_subscriptions | tenant_id FK; provider_account_id FK; provider_subscription_id text; state incomplete/trialing/active/past_due/unpaid/paused/canceled; period_id FK?; cancel_at_period_end bool; pending_change_id uuid?; provider_snapshot_hash bytea; synced_at timestamptz | UNIQUE(account,provider_subscription_id); максимум одна незакрытая subscription на tenant/account; incomplete не даёт paid grant |
| billing_subscription_items | tenant_id; subscription_id FK; kind ai/storage; price_id FK?; offer_id FK?; external_item_id text; quantity int; starts_at/end_at timestamptz; state active/pending/canceled | XOR local target; ai quantity=1 и максимум один active AI item; storage quantity>0; unique account/item |
| billing_custom_assignments | tenant_id; custom_plan_version_id FK; amount_net_minor; currency; billing_mode stripe/manual; starts_at; ends_at; state draft/active/ended; reason text; assigned_by uuid?; approved_at timestamptz? | Один active assignment, полный набор custom_limits; reason без лишней PII; publish только privileged process |
| billing_custom_limits | assignment_id FK; resource_code FK; integer_value bigint?; enum_value text?; grant_period once/month/none; rollover_periods int | UNIQUE(assignment,resource); typed XOR; обязательные ресурсы полностью заданы |
| billing_change_requests | tenant_id; subscription_id FK?; type upgrade/downgrade/cancel_ai/storage_add/storage_reduce; source_version_id FK?; target_version_id FK?; quote_id FK; effective_at; idempotency_key text; request_hash bytea; state quoted/payment_pending/paid/applying/applied/compensating/failed/canceled; order_id FK?; last_error_code text? | UNIQUE(tenant,idempotency_key); один unresolved immediate change на subscription; одинаковый key+другой hash=conflict |

## Финансы и доставка событий

| Таблица | Поля | Ограничения |
|---|---|---|
| billing_quotes | tenant_id; actor_id uuid?; kind text; plan_revision bigint; resource_snapshot jsonb; resource_snapshot_hash bytea; net_minor/tax_minor/gross_minor bigint; currency; tax_policy_version text; expires_at; accepted_at?; request_hash bytea | Immutable; default TTL=15 минут; membership, price и policy revalidation перед order |
| billing_orders | tenant_id; quote_id FK; kind subscription/topup/operation_topup/upgrade/storage/egress; state created/checkout_pending/payment_pending/paid/fulfilling/fulfilled/failed/canceled/refunded/partially_refunded; net_minor/tax_minor/gross_minor bigint; currency; idempotency_key text; request_hash bytea; environment account FK; provider_checkout_id text?; paid_at? | UNIQUE(tenant,idempotency_key); unique account/session; refund не удаляет fulfilled history |
| billing_order_lines | tenant_id; order_id FK; ordinal int; price_id FK?; offer_id FK?; resource_code FK; pack_count int; unit_net_minor bigint; net_minor/tax_minor/gross_minor bigint; resource_quantity bigint; snapshot jsonb; grant_starts_at?/expires_at? | UNIQUE(order,ordinal); pack_count>0; typed references; серверные snapshots; foreign offer текущего плана запрещён |
| billing_invoices | financial_subject_id uuid; tenant_id uuid?; provider_account_id FK; provider_invoice_id text; order_id uuid?; currency; net_minor/tax_minor/gross_minor bigint; state draft/open/paid/void/uncollectible; paid_at?; retention_policy_id uuid; hosted_url text? | UNIQUE(account,invoice); restricted retained scope после tenant erasure; URL сам является sensitive capability |
| billing_payments | financial_subject_id; tenant_id?; provider_account_id FK; provider_payment_id text; invoice_id FK?; order_id FK?; amount_minor/refunded_minor bigint; currency; state pending/succeeded/failed/refunded/disputed; settled_at? | UNIQUE(account,payment); refunded<=amount; несколько платежей могут погашать invoice, не начислять grant на каждый повтор |
| billing_invoice_allocations | tenant_id?; invoice_id FK; external_line_id text; order_line_id uuid?; period_id uuid?; allocation_kind renewal/upgrade/storage/egress; resource_snapshot jsonb; state pending/applied/compensated | UNIQUE(account,invoice,external_line_id,allocation_kind); несколько resources хранятся typed children в billing_entitlement_allocations с отдельными business keys |
| billing_refunds | financial_subject_id; payment_id FK; external_refund_id text; amount_minor bigint; state pending/succeeded/failed; resource_reversal_key text?; reason_code text | UNIQUE(account,refund); partial resource reversal по immutable line mapping; consumed usage не исчезает |
| billing_webhook_inbox | provider_account_id FK; event_id text; event_type text; object_id text; provider_created_at; payload_encrypted bytea?; payload_hash bytea; state received/processing/applied/ignored/retry/dead; attempts int; next_retry_at?; processing_token uuid?; lease_until?; last_error_code text? | UNIQUE(account,event_id); active retry index(state,next_retry_at); payload TTL; минимум PII, не вечный raw archive |
| billing_outbox | tenant_id?; topic text; business_key text; payload jsonb; state pending/processing/sent/retry/dead; attempts int; next_retry_at?; processing_token uuid?; lease_until? | UNIQUE(topic,business_key); SKIP LOCKED claims; тема erasure не содержит PII; delivery at-least-once |
| billing_reconciliation_runs | provider_account_id FK; scope payments/subscriptions/grants; cursor text?; window_start/end; state running/completed/failed; mismatch_count int; error_code text? | Пагинация, overlap window и business dedup; index(account,created_at) |

## Потребление и порции допуска

| Таблица | Поля | Инварианты |
|---|---|---|
| billing_credit_buckets | tenant_id; resource_code FK; source_type trial/invoice_line/topup/rollover/custom; source_id text; grant_key text; original_quantity bigint; remaining_quantity bigint; leased_quantity bigint; consumed_quantity bigint; revoked_quantity bigint; transferred_quantity bigint; starts_at timestamptz; expires_at timestamptz?; expiry_policy fixed/never; period_id FK?; rollover_generation int; origin_bucket_id FK?; state active/draining/expired/revoked/closed | UNIQUE(tenant,grant_key); quantities>=0; original=remaining+leased+consumed+revoked+transferred для закрытого определения allocation; expire переводит remaining в revoked с reason expired; generation 0/1; FK tenant/resource |
| billing_resource_ledger | tenant_id; resource_code; bucket_id FK?; ticket_id uuid?; event grant/allocate/settle/release/expire/revoke/rollover/overrun/overrun_cover; quantity bigint; direction credit/debit/transfer; business_key text; reference_type/id text; occurred_at; policy_version int | Append-only; UNIQUE(tenant,business_key); balance выводится по семантике event, не суммированием разных единиц; correction отдельным event |
| billing_admission_leases | tenant_id; resource_code; period_id FK; instance_id text; generation bigint; fence_token uuid; policy_version int; allocated_quantity bigint; issued_quantity bigint; returned_quantity bigint; allowance_quantity bigint; expires_at; state active/draining/closed | issued+returned<=allocated+allowance; не более max_local_leases; allowance дополнительно ограничен durable period budget; unique fence; expiry запрещает issue, не отменяет issued |
| billing_lease_allocations | tenant_id; lease_id FK; bucket_id FK?; source paid/allowance; quantity bigint; issued_quantity bigint; returned_quantity bigint | paid требует bucket; allowance bucket=NULL; UNIQUE(lease,bucket/source); earliest expiry first |
| billing_usage_tickets | tenant_id; lease_id FK?; fence_token uuid?; request_id uuid FK; charge_item_id uuid FK; run_id uuid?; resource_code; quantity bigint; issued_at; state accepted/settled/released; result_id uuid?; terminal_at?; policy_snapshot jsonb | UNIQUE(tenant,charge_item,resource); quantity>=0; zero только для подтверждённого free-slot ticket; accepted в TX run; event key canonical result/resource; cancellation не releases adoptioned success |
| billing_usage_ticket_allocations | tenant_id; ticket_id FK; lease_allocation_id FK; quantity bigint; settled_quantity bigint; released_quantity bigint | UNIQUE(ticket,allocation); суммы=quantity после terminal; allocations не позволяют fund дважды |
| billing_usage_summaries | tenant_id; resource_code; period_id FK; granted/consumed/leased/available/overrun bigint; blocked bool; entitlement_revision bigint; projected_at; projection_cursor text | UNIQUE(tenant,resource,period); rebuildable projection, не новый ledger; available никогда не отрицателен; overrun отдельно |
| billing_photo_identities | tenant_id; digest_algorithm text; source_digest bytea; first_success_at?; window_ends_at?; successful_user_reruns int; first_result_id uuid? | UNIQUE(tenant,algorithm,digest); no cross-tenant dedup; immutable first success window |
| billing_photo_runs | tenant_id; photo_identity_id FK; user_request_id text; run_id uuid; result_id uuid?; attempt_kind initial/user_rerun/technical_retry; billing_policy_version int; charged_quantity bigint; adopted_at? | UNIQUE(tenant,user_request); canonical adoption/result unique; retry refers same user_request; free-slot identity фиксируется при admission, success ordinal только audit и не меняет согласованную стоимость |
| billing_storage_addons | tenant_id; subscription_item_id FK; offer_id FK; quantity int; capacity_bytes bigint; period_id FK; state pending/active/ending/ended; cancel_at_period_end bool; paid_allocation_id FK? | Capacity только paid; recurring capacity не consumable bucket |
| billing_storage_objects | tenant_id; artifact_id uuid; bucket_name text; object_key text; object_version text; logical_size_bytes bigint; category original/result/preview/archive; state pending/ready/deleting/deleted; deleted_at? | UNIQUE(tenant,bucket,key,version); PII-bearing keys удаляются/обезличиваются по policy; readiness использует существующие object proofs |
| billing_storage_usage | tenant_id PK; included_bytes/additional_bytes bigint; committed_bytes/reserved_bytes bigint; original_bytes/result_bytes/preview_bytes/archive_bytes bigint; reconciled_at?; revision bigint | Представление/projection tenant_usage, не параллельный счётчик admission; category sum=committed |
| billing_egress_ledger | tenant_id; period_id FK; delivery_id uuid; chunk_sequence bigint; source gallery/preview/download; bytes_sent bigint; ticket_id FK; occurred_at; recorded_at | UNIQUE(tenant,delivery,chunk_sequence); реально отправленные bytes; повторный download новый delivery; partial flush dedup |
| billing_notification_marks | tenant_id; resource_code; period_id; threshold_percent int; channel in_app/email; delivered_at?; business_key text | UNIQUE(tenant,resource,period,threshold,channel); thresholds 80/95/100; repeat после нового периода |

Storage schema не должна требовать двух transactional updates независимых usage истин. Для category breakdown разрешена асинхронная проекция существующих physical lifecycle событий. Account deletion охватывает также cached DTO, outbox и эти object refs.

## Юридические записи и удаление

| Таблица | Поля | Ограничения |
|---|---|---|
| legal_document_versions | document_key terms/privacy/cookies/dpa/photoeditor_addendum; locale text; version text; url text; content_sha256 bytea; published_at; effective_at; state draft/active/retired; reacceptance_required bool | UNIQUE(key,locale,version); URL allowlist https://legal.beeloy.org; archived exact text/hash, не только изменяемая ссылка |
| account_legal_events | user_id uuid?; tenant_id uuid?; subject_ref uuid; document_version_id FK; purpose contract/privacy_notice/cookie_analytics/marketing; action accepted/acknowledged/granted/revoked; occurred_at; capture_channel web/native; client_event_id text; evidence_hash bytea | UNIQUE(subject,client_event,document_version_id,purpose,action); mandatory contract и optional consent различаются; restricted retention; не сохранять полный IP по умолчанию |
| user_privacy_preferences | user_id PK; analytics bool=false; marketing bool=false; preference_version int; last_event_id FK; updated_at; revision bigint | Нет prechecked optional; audit через events; withdrawal немедленно меняет effective preference |
| privacy_retention_policies | code text; version int; data_category text; legal_basis text; retention_days int?; retention_trigger text; backup_max_days int?; approval_ref text?; state draft/approved/retired | UNIQUE(code,version); сроки обязательны для approved; нет unlimited NULL; draft блокирует live, не erasure |
| profile_deletion_requests | user_id uuid?; subject_ref uuid; idempotency_key text; request_hash bytea; requested_at; reauthenticated_at; state preflight/ownership_action_required/erasure_requested/revoking/purging/external_pending/completed/partially_retained/failed; status_token_hash bytea; deadline_at; completed_at?; retention_policy_id FK | UNIQUE(subject,idempotency_key); минимум auth history; ownership не может удалить чужие данные |
| profile_deletion_steps | request_id FK; target auth/db/storage/cache/stripe/backup/analytics; target_ref text?; state pending/running/completed/retry/retained; processing_token uuid?; lease_until?; attempts int; next_retry_at?; evidence_hash bytea?; error_code text? | UNIQUE(request,target,target_ref); idempotent external jobs; evidence без удаляемой PII |
| privacy_retained_records | subject_ref uuid; record_type text; record_id uuid; retention_policy_id FK; legal_hold_ref text?; retain_until; restricted_at; erased_at? | Restricted financial/legal retention; policy+expiry обязательны; pseudonymous != anonymous |
| privacy_erasure_tombstones | subject_ref uuid; deletion_request_id FK; scope_type user/tenant/object; opaque_scope_ref text; erased_at; expires_at; backup_cutoff_at | Ни email, ни full object key; restore фильтрует до serving; хранение tombstone дольше самой старой допустимой backup |

## Доступ и индексы

- Каталог читается через public view только для public/published plans.
- Current Custom возвращается отдельно авторизованной summary.
- API не получает прямые INSERT/UPDATE/DELETE финансовых таблиц.
- Функции с SECURITY DEFINER имеют фиксированный search_path, владельца по действующей модели, REVOKE PUBLIC и точечные EXECUTE.
- Worker только settlement своего task/result через существующую adoption authority, без возможности grant/смены цены.
- Maintenance отдельно обрабатывает inbox/outbox/erasure.
- Финансовые retained записи доступны только уполномоченной support/accounting роли с аудитом.

- Обязательные индексы: active bucket(tenant,resource,expires_at,created_at,id); lease(tenant,resource,state,expires_at); ticket(tenant,run_id,state); ledger(tenant,resource,occurred_at,id); pending inbox/outbox(state,next_retry_at); subscription(tenant,state); order(tenant,created_at,id); erasure(state,deadline_at).
- PII и raw webhook payload не включать в индексы и telemetry labels.
- Крупные append-only tables партиционировать только по доказанной необходимости; business unique dedup должен сохраняться между partitions.

## Обязательное уточнение схемы для генерации

- Дополнительные typed поля, FK, natural identities, enum и новые supporting tables перечислены в разделе 09. Они входят в эту модель, а не являются optional расширением.
- В частности, вводятся financial_subjects, allowance_budgets, typed entitlement allocations, delivery receipts, onboarding state, recent-auth grants, privacy cases и capacity revisions.
- Ссылки source_type/source_id в ledger — только audit description; денежное основание выдачи имеет typed invoice/order allocation FK.

- Любая строка, участвующая в отзыве/погашении, изменяется только с сохранением conservation equation.
- Expire/revoke/rollover имеют отдельные reason и business event, не подменяют финансовый refund.
- Отсутствие срока допускается только с explicit expiry_policy=never для Trial photo; не применять этот режим к диску/трафику/платным packs.
