# BRIEF 1A — Каталог

Карточка сгенерирована `tools/gen-briefs.py` из `contracts/`. Вручную не править.

## Что читать
- Правила: `RULES.md` (всегда).
- Описание этапа: `11-implementation-progress.md`.
- Зависит от этапов: —. Их файлы читать только если задача трогает их функции.

## Выпуск
- Миграции: 02598, 02599.
- Генератор: `scripts/generate-billing-catalog.py` (запуск с `--check`).
- Миграции forward-only; старые не править (R9).

## SQL-источники

- `contracts/tables.json`

## Блокировки (из RULES)

- R23: tenant SHARE → control SHARE → balance FOR UPDATE → allowance/buckets → lease/claim.
- R24: несколько ресурсов — по `resource_code`, до run/task/adoption locks.
- R25: не вызывать finalize/settle под task lock.

## Перед коммитом

1. `python3 scripts/generate-billing-catalog.py --check`
2. `python3 docs/billing/tools/lint-docs.py`
3. Запись `execute_roles` в JSON = `GRANT EXECUTE` в SQL.

## Таблицы (источник: `contracts/tables.json`)

### billing_plans
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); updated_at timestamptz DEFAULT now(); revision bigint DEFAULT 1; code text; name_key text; description_key text; is_public boolean DEFAULT false; self_service_enabled boolean DEFAULT false; is_active boolean DEFAULT true; sort_order integer
- PK: id
- UNIQUE(code)
### billing_plan_versions
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); plan_id uuid; version integer; catalog_scope text; effective_from timestamptz; status text DEFAULT 'draft'; policy_hash bytea NULL
- PK: id
- FOREIGN KEY(plan_id) REFERENCES photo.billing_plans(id)
- UNIQUE(plan_id,version,catalog_scope)
- UNIQUE(id,catalog_scope)
### billing_plan_prices
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); plan_version_id uuid; catalog_scope text; version integer; currency text DEFAULT 'EUR'; amount_net_minor bigint; interval text; tax_behavior text DEFAULT 'exclusive'; is_default boolean DEFAULT true
- PK: id
- FOREIGN KEY(plan_version_id,catalog_scope) REFERENCES photo.billing_plan_versions(id,catalog_scope)
- UNIQUE(plan_version_id,version,currency,interval,catalog_scope)
- UNIQUE(id,catalog_scope)
### billing_resource_types
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); code text; unit text; kind text; value_type text; description text
- PK: id
- UNIQUE(code)
- UNIQUE(code,value_type)
### billing_plan_limits
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); plan_version_id uuid; resource_code text; value_type text; integer_value bigint NULL; enum_value text NULL; grant_period text; rollover_periods integer DEFAULT 0
- PK: id
- FOREIGN KEY(plan_version_id) REFERENCES photo.billing_plan_versions(id)
- FOREIGN KEY(resource_code,value_type) REFERENCES photo.billing_resource_types(code,value_type)
- UNIQUE(plan_version_id,resource_code)
### billing_topup_offers
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); plan_id uuid; catalog_scope text; offer_code text; version integer; resource_code text; quantity bigint; amount_net_minor bigint; currency text DEFAULT 'EUR'; billing_kind text; interval text; ttl_days integer NULL; expiry_policy text; bundled_egress_bytes bigint DEFAULT 0; is_active boolean DEFAULT false
- PK: id
- FOREIGN KEY(plan_id) REFERENCES photo.billing_plans(id)
- FOREIGN KEY(resource_code) REFERENCES photo.billing_resource_types(code)
- UNIQUE(plan_id,offer_code,version,catalog_scope)
- UNIQUE(id,catalog_scope)
### billing_provider_accounts
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); updated_at timestamptz DEFAULT now(); revision bigint DEFAULT 1; provider text DEFAULT 'stripe'; environment text DEFAULT 'test'; catalog_scope text DEFAULT 'sandbox'; external_account_id text; sandbox_reference text NULL; api_version text; webhook_format text; enabled boolean DEFAULT false
- PK: id
- UNIQUE NULLS NOT DISTINCT(provider,environment,external_account_id,sandbox_reference)
- UNIQUE(id,catalog_scope)
### billing_provider_prices
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); provider_account_id uuid; catalog_scope text; price_id uuid NULL; offer_id uuid NULL; external_product_id text; external_price_id text; verified_at timestamptz
- PK: id
- FOREIGN KEY(provider_account_id,catalog_scope) REFERENCES photo.billing_provider_accounts(id,catalog_scope)
- FOREIGN KEY(price_id,catalog_scope) REFERENCES photo.billing_plan_prices(id,catalog_scope)
- FOREIGN KEY(offer_id,catalog_scope) REFERENCES photo.billing_topup_offers(id,catalog_scope)
- UNIQUE(provider_account_id,external_price_id)
### billing_operation_policies
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); operation_code text; operation_version text; charge_context text; policy_version integer; billing_class text; resource_code text NULL; units_per_success bigint; success_contract text; rerun_policy text; beta_budget bigint NULL; effective_from timestamptz; benchmark_ref text NULL; policy_snapshot_hash bytea
- PK: id
- FOREIGN KEY(operation_code,operation_version) REFERENCES photo.operation_registry(operation,version)
- FOREIGN KEY(resource_code) REFERENCES photo.billing_resource_types(code)
- UNIQUE(operation_code,operation_version,charge_context,policy_version)
### billing_admission_policies
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); resource_code text; policy_version integer; chunk_quantity bigint; lease_seconds integer; cache_ttl_seconds integer; optimistic_allowance bigint; max_local_leases integer; cold_timeout_ms integer; enabled boolean DEFAULT false
- PK: id
- FOREIGN KEY(resource_code) REFERENCES photo.billing_resource_types(code)
- UNIQUE(resource_code,policy_version)
