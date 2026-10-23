# Таблицы этапа 1B1

Сгенерировано `tools/gen-briefs.py`. Вручную не править.

## Таблицы (источник: `contracts/quota-tables.json`)

### billing_accounting_controls
- Поля: created_at timestamptz DEFAULT now(); tenant_id uuid; mode text DEFAULT 'shadow'; lifecycle text DEFAULT 'active'; revision bigint DEFAULT 1
- PK: tenant_id
- FOREIGN KEY(tenant_id) REFERENCES photo.tenants(id)
- CHECK(mode='shadow')
- CHECK(lifecycle IN ('active','deleting','closed'))
- CHECK(revision>0)
### billing_periods
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); tenant_id uuid; starts_at timestamptz; ends_at timestamptz; anchor_kind text; state text DEFAULT 'open'
- PK: id
- FOREIGN KEY(tenant_id) REFERENCES photo.tenants(id)
- UNIQUE(tenant_id,id)
- CHECK(ends_at>starts_at)
- CHECK(anchor_kind IN ('trial_calendar','subscription'))
- CHECK(state IN ('open','closing','closed'))
### billing_resource_balances
- Поля: created_at timestamptz DEFAULT now(); tenant_id uuid; resource_code text; overrun_quantity bigint DEFAULT 0; revision bigint DEFAULT 1
- PK: tenant_id, resource_code
- FOREIGN KEY(tenant_id) REFERENCES photo.tenants(id)
- FOREIGN KEY(resource_code) REFERENCES photo.billing_resource_types(code)
- CHECK(overrun_quantity>=0)
- CHECK(revision>0)
### billing_allowance_budgets
- Поля: created_at timestamptz DEFAULT now(); tenant_id uuid; resource_code text; period_id uuid; configured_quantity bigint; outstanding_quantity bigint DEFAULT 0; settled_quantity bigint DEFAULT 0; policy_version integer; revision bigint DEFAULT 1
- PK: tenant_id, resource_code, period_id
- FOREIGN KEY(tenant_id,period_id) REFERENCES photo.billing_periods(tenant_id,id)
- FOREIGN KEY(tenant_id,resource_code) REFERENCES photo.billing_resource_balances(tenant_id,resource_code)
- FOREIGN KEY(resource_code,policy_version) REFERENCES photo.billing_admission_policies(resource_code,policy_version)
- CHECK(configured_quantity>=0 AND outstanding_quantity>=0 AND settled_quantity>=0)
- CHECK(outstanding_quantity::numeric+settled_quantity<=configured_quantity)
- CHECK(revision>0)
### billing_bootstrap_grants
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); tenant_id uuid; resource_code text; reason text; quantity bigint; starts_at timestamptz; expires_at timestamptz NULL; expiry_policy text; policy_snapshot_hash bytea
- PK: id
- FOREIGN KEY(tenant_id) REFERENCES photo.tenants(id)
- FOREIGN KEY(resource_code) REFERENCES photo.billing_resource_types(code)
- UNIQUE(tenant_id,id,resource_code)
- UNIQUE(tenant_id,reason,resource_code)
- CHECK(reason IN ('trial_once','beta_transition'))
- CHECK(quantity>0)
- CHECK(octet_length(policy_snapshot_hash)=32)
- CHECK((expiry_policy='fixed' AND expires_at IS NOT NULL AND expires_at>starts_at) OR (expiry_policy='never' AND expires_at IS NULL AND reason='trial_once' AND resource_code='standard_photo'))
- CHECK(reason<>'trial_once' OR (resource_code='standard_photo' AND quantity=20 AND expiry_policy='never'))
### billing_credit_buckets
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); tenant_id uuid; resource_code text; bootstrap_grant_id uuid; original_quantity bigint; remaining_quantity bigint; leased_quantity bigint DEFAULT 0; consumed_quantity bigint DEFAULT 0; revoked_quantity bigint DEFAULT 0; transferred_quantity bigint DEFAULT 0; starts_at timestamptz; expires_at timestamptz NULL; expiry_policy text; source_type text; state text DEFAULT 'active'
- PK: id
- FOREIGN KEY(tenant_id) REFERENCES photo.tenants(id)
- FOREIGN KEY(tenant_id,bootstrap_grant_id,resource_code) REFERENCES photo.billing_bootstrap_grants(tenant_id,id,resource_code)
- UNIQUE(tenant_id,id,resource_code)
- UNIQUE(tenant_id,bootstrap_grant_id)
- CHECK(original_quantity>0 AND remaining_quantity>=0 AND leased_quantity>=0 AND consumed_quantity>=0 AND revoked_quantity>=0 AND transferred_quantity>=0)
- CHECK(original_quantity::numeric=remaining_quantity::numeric+leased_quantity+consumed_quantity+revoked_quantity+transferred_quantity)
- CHECK(source_type IN ('trial','beta_transition'))
- CHECK((expiry_policy='fixed' AND expires_at IS NOT NULL AND expires_at>starts_at) OR (expiry_policy='never' AND expires_at IS NULL AND source_type='trial' AND resource_code='standard_photo'))
- CHECK(state IN ('active','draining','expired','closed'))
### billing_admission_leases
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); tenant_id uuid; resource_code text; period_id uuid; instance_id text; generation bigint; fence_token uuid DEFAULT gen_random_uuid(); request_key text; policy_version integer; allocated_quantity bigint; allowance_quantity bigint; issued_quantity bigint DEFAULT 0; returned_quantity bigint DEFAULT 0; expires_at timestamptz; state text DEFAULT 'active'
- PK: id
- FOREIGN KEY(tenant_id,period_id) REFERENCES photo.billing_periods(tenant_id,id)
- FOREIGN KEY(resource_code,policy_version) REFERENCES photo.billing_admission_policies(resource_code,policy_version)
- UNIQUE(tenant_id,id,resource_code,period_id)
- UNIQUE(tenant_id,resource_code,period_id,request_key)
- UNIQUE(tenant_id,resource_code,period_id,instance_id,generation)
- UNIQUE(fence_token)
- CHECK(length(instance_id) BETWEEN 1 AND 120 AND length(request_key) BETWEEN 1 AND 128)
- CHECK(generation>0 AND allocated_quantity>=0 AND allowance_quantity>=0 AND allocated_quantity::numeric+allowance_quantity>0)
- CHECK(issued_quantity>=0 AND returned_quantity>=0 AND issued_quantity::numeric+returned_quantity<=allocated_quantity::numeric+allowance_quantity)
- CHECK(state IN ('active','draining','closed'))
### billing_lease_allocations
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); tenant_id uuid; resource_code text; period_id uuid; lease_id uuid; bucket_id uuid NULL; source text; quantity bigint; issued_quantity bigint DEFAULT 0; returned_quantity bigint DEFAULT 0
- PK: id
- FOREIGN KEY(tenant_id,lease_id,resource_code,period_id) REFERENCES photo.billing_admission_leases(tenant_id,id,resource_code,period_id)
- FOREIGN KEY(tenant_id,bucket_id,resource_code) REFERENCES photo.billing_credit_buckets(tenant_id,id,resource_code)
- FOREIGN KEY(tenant_id,resource_code,period_id) REFERENCES photo.billing_allowance_budgets(tenant_id,resource_code,period_id)
- UNIQUE(tenant_id,id,resource_code,period_id,lease_id)
- UNIQUE NULLS NOT DISTINCT(tenant_id,lease_id,bucket_id,source)
- CHECK((source='paid' AND bucket_id IS NOT NULL) OR (source='allowance' AND bucket_id IS NULL))
- CHECK(quantity>0 AND issued_quantity>=0 AND returned_quantity>=0 AND issued_quantity::numeric+returned_quantity<=quantity)
### billing_funding_claims
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); tenant_id uuid; resource_code text; period_id uuid; lease_id uuid; request_key text; request_hash bytea; quantity bigint; state text DEFAULT 'accepted'; confirmed_quantity bigint NULL; terminal_proof_hash bytea NULL
- PK: id
- FOREIGN KEY(tenant_id,lease_id,resource_code,period_id) REFERENCES photo.billing_admission_leases(tenant_id,id,resource_code,period_id)
- UNIQUE(tenant_id,id,resource_code,period_id,lease_id)
- UNIQUE(tenant_id,resource_code,request_key)
- CHECK(quantity>0 AND length(request_key) BETWEEN 1 AND 128 AND octet_length(request_hash)=32)
- CHECK((state='accepted' AND confirmed_quantity IS NULL AND terminal_proof_hash IS NULL) OR (state IN ('settled','released') AND confirmed_quantity IS NOT NULL AND terminal_proof_hash IS NOT NULL AND octet_length(terminal_proof_hash)=32 AND confirmed_quantity BETWEEN 0 AND quantity AND ((state='released')=(confirmed_quantity=0))))
### billing_funding_claim_allocations
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); tenant_id uuid; resource_code text; period_id uuid; lease_id uuid; claim_id uuid; lease_allocation_id uuid; quantity bigint
- PK: id
- FOREIGN KEY(tenant_id,claim_id,resource_code,period_id,lease_id) REFERENCES photo.billing_funding_claims(tenant_id,id,resource_code,period_id,lease_id)
- FOREIGN KEY(tenant_id,lease_allocation_id,resource_code,period_id,lease_id) REFERENCES photo.billing_lease_allocations(tenant_id,id,resource_code,period_id,lease_id)
- UNIQUE(tenant_id,claim_id,lease_allocation_id)
- CHECK(quantity>0)
### billing_resource_ledger
- Поля: id uuid DEFAULT gen_random_uuid(); created_at timestamptz DEFAULT now(); tenant_id uuid; resource_code text; bucket_id uuid NULL; lease_id uuid NULL; period_id uuid NULL; claim_id uuid NULL; event_type text; quantity bigint; direction text; business_key text
- PK: id
- FOREIGN KEY(tenant_id,resource_code) REFERENCES photo.billing_resource_balances(tenant_id,resource_code)
- FOREIGN KEY(tenant_id,bucket_id,resource_code) REFERENCES photo.billing_credit_buckets(tenant_id,id,resource_code)
- FOREIGN KEY(tenant_id,lease_id,resource_code,period_id) REFERENCES photo.billing_admission_leases(tenant_id,id,resource_code,period_id)
- FOREIGN KEY(tenant_id,claim_id,resource_code,period_id,lease_id) REFERENCES photo.billing_funding_claims(tenant_id,id,resource_code,period_id,lease_id)
- UNIQUE(tenant_id,business_key)
- CHECK(event_type IN ('grant','allocate','issue','settle','release','expire','overrun','overrun_cover'))
- CHECK(quantity>0 AND length(business_key) BETWEEN 1 AND 300)
- CHECK((event_type='grant' AND direction='credit') OR (event_type IN ('allocate','issue','release') AND direction='transfer') OR (event_type IN ('settle','expire','overrun','overrun_cover') AND direction='debit'))
- CHECK((lease_id IS NULL AND period_id IS NULL AND claim_id IS NULL) OR (lease_id IS NOT NULL AND period_id IS NOT NULL))
