-- +goose Up
INSERT INTO photo.billing_operation_policies(id,operation_code,operation_version,charge_context,policy_version,billing_class,units_per_success,success_contract,rerun_policy,effective_from,policy_snapshot_hash) VALUES ('a2e8d020-bcc2-5bfa-bab4-f942c678b301','image.local','1','standard_root',1,'included_standard',0,'apply_final_adoption_v1','standard_window',now(),sha256(convert_to('{"included_reruns":2,"policy_version":1,"rerun_window_hours":168,"schema_version":1,"success_contract":"apply_final_adoption_v1"}','UTF8')));
-- Explicit operator opt-in only; no existing tenant is enrolled by this migration.
CREATE TABLE photo.billing_workflow_controls (
 tenant_id uuid PRIMARY KEY REFERENCES photo.tenants(id),
 enabled boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 policy_version integer NOT NULL DEFAULT 1 CHECK(policy_version=1)
);
CREATE TABLE photo.billing_photo_identities (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL REFERENCES photo.tenants(id),created_at timestamptz NOT NULL DEFAULT now(),
 digest_algorithm text NOT NULL DEFAULT 'sha256' CHECK(digest_algorithm='sha256'),source_digest bytea NOT NULL CHECK(octet_length(source_digest)=32),
 first_success_at timestamptz,window_ends_at timestamptz,successful_user_reruns integer NOT NULL DEFAULT 0 CHECK(successful_user_reruns>=0),
 UNIQUE(tenant_id,id),UNIQUE(tenant_id,digest_algorithm,source_digest),
 CHECK((first_success_at IS NULL AND window_ends_at IS NULL) OR (first_success_at IS NOT NULL AND window_ends_at=first_success_at+interval '168 hours'))
);
CREATE TABLE photo.billing_cost_quotes (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL REFERENCES photo.tenants(id),created_at timestamptz NOT NULL DEFAULT now(),
 actor_id uuid NOT NULL REFERENCES photo.users(id),request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 snapshot_id uuid NOT NULL,policy_version integer NOT NULL CHECK(policy_version=1),max_quantity bigint NOT NULL CHECK(max_quantity>=0),
 expires_at timestamptz NOT NULL,state text NOT NULL CHECK(state='accepted'),
 UNIQUE(tenant_id,id),FOREIGN KEY(tenant_id,snapshot_id) REFERENCES photo.snapshots(tenant_id,id)
);
CREATE TABLE photo.billing_usage_requests (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL REFERENCES photo.tenants(id),created_at timestamptz NOT NULL DEFAULT now(),
 actor_user_id uuid NOT NULL REFERENCES photo.users(id),session_id uuid NOT NULL REFERENCES photo.sessions(id),request_key text NOT NULL CHECK(length(request_key) BETWEEN 1 AND 128),
 route text NOT NULL CHECK(route='Run.Apply'),canonical_request_hash bytea NOT NULL CHECK(octet_length(canonical_request_hash)=32),
 workflow_kind text NOT NULL DEFAULT 'standard_apply' CHECK(workflow_kind='standard_apply'),cost_quote_id uuid NOT NULL,
 run_id uuid NOT NULL,period_id uuid NOT NULL,status text NOT NULL DEFAULT 'accepted' CHECK(status IN ('accepted','settled','canceled')),
 UNIQUE(tenant_id,id),UNIQUE(tenant_id,run_id),UNIQUE(tenant_id,actor_user_id,route,request_key),
 FOREIGN KEY(tenant_id,cost_quote_id) REFERENCES photo.billing_cost_quotes(tenant_id,id),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id),FOREIGN KEY(tenant_id,period_id) REFERENCES photo.billing_periods(tenant_id,id)
);
CREATE TABLE photo.billing_usage_charge_items (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL REFERENCES photo.tenants(id),created_at timestamptz NOT NULL DEFAULT now(),
 usage_request_id uuid NOT NULL,ordinal bigint NOT NULL CHECK(ordinal>=0),photo_identity_id uuid NOT NULL,source_item_id uuid NOT NULL,source_version_id uuid NOT NULL,
 resource_code text NOT NULL DEFAULT 'standard_photo' CHECK(resource_code='standard_photo'),operation_policy_id uuid NOT NULL DEFAULT 'a2e8d020-bcc2-5bfa-bab4-f942c678b301' REFERENCES photo.billing_operation_policies(id),policy_version integer NOT NULL DEFAULT 1 CHECK(policy_version=1),
 charge_kind text NOT NULL CHECK(charge_kind IN ('initial','free','paid')),max_quantity bigint NOT NULL CHECK(max_quantity IN (0,1)),
 state text NOT NULL DEFAULT 'accepted' CHECK(state IN ('accepted','succeeded','failed')),
 UNIQUE(tenant_id,id),UNIQUE(tenant_id,id,usage_request_id,max_quantity),UNIQUE(tenant_id,usage_request_id,ordinal),UNIQUE(tenant_id,usage_request_id,photo_identity_id),
 FOREIGN KEY(tenant_id,usage_request_id) REFERENCES photo.billing_usage_requests(tenant_id,id),
 FOREIGN KEY(tenant_id,photo_identity_id) REFERENCES photo.billing_photo_identities(tenant_id,id),
 FOREIGN KEY(tenant_id,source_item_id) REFERENCES photo.collection_items(tenant_id,id),FOREIGN KEY(tenant_id,source_version_id) REFERENCES photo.asset_versions(tenant_id,id),
 CHECK((charge_kind='free')=(max_quantity=0))
);
CREATE TABLE photo.billing_photo_free_slots (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL REFERENCES photo.tenants(id),created_at timestamptz NOT NULL DEFAULT now(),
 photo_identity_id uuid NOT NULL,slot_kind text NOT NULL CHECK(slot_kind IN ('first_result','rerun_1','rerun_2')),
 usage_charge_item_id uuid,state text NOT NULL CHECK(state IN ('available','held','consumed','expired')),held_at timestamptz,window_ends_at timestamptz,
 UNIQUE(tenant_id,id),UNIQUE(tenant_id,photo_identity_id,slot_kind),
 FOREIGN KEY(tenant_id,photo_identity_id) REFERENCES photo.billing_photo_identities(tenant_id,id),FOREIGN KEY(tenant_id,usage_charge_item_id) REFERENCES photo.billing_usage_charge_items(tenant_id,id),
 CHECK((state='held')=(held_at IS NOT NULL)),CHECK(state NOT IN ('held','consumed') OR usage_charge_item_id IS NOT NULL)
);
ALTER TABLE photo.billing_funding_claims ADD CONSTRAINT billing_claim_ticket_scope UNIQUE(tenant_id,id,resource_code);
CREATE TABLE photo.billing_usage_tickets (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL REFERENCES photo.tenants(id),created_at timestamptz NOT NULL DEFAULT now(),
 request_id uuid NOT NULL,charge_item_id uuid NOT NULL,resource_code text NOT NULL DEFAULT 'standard_photo' CHECK(resource_code='standard_photo'),
 funding_claim_id uuid,free_slot_id uuid,quantity bigint NOT NULL CHECK(quantity IN (0,1)),state text NOT NULL DEFAULT 'accepted' CHECK(state IN ('accepted','settled','released')),
 FOREIGN KEY(tenant_id,charge_item_id,request_id,quantity) REFERENCES photo.billing_usage_charge_items(tenant_id,id,usage_request_id,max_quantity),
 UNIQUE(tenant_id,id),UNIQUE(tenant_id,charge_item_id,resource_code),UNIQUE(tenant_id,funding_claim_id),
 FOREIGN KEY(tenant_id,request_id) REFERENCES photo.billing_usage_requests(tenant_id,id),FOREIGN KEY(tenant_id,charge_item_id) REFERENCES photo.billing_usage_charge_items(tenant_id,id),
 FOREIGN KEY(tenant_id,funding_claim_id,resource_code) REFERENCES photo.billing_funding_claims(tenant_id,id,resource_code),
 FOREIGN KEY(tenant_id,free_slot_id) REFERENCES photo.billing_photo_free_slots(tenant_id,id),
 CHECK((quantity=1 AND funding_claim_id IS NOT NULL AND free_slot_id IS NULL) OR (quantity=0 AND funding_claim_id IS NULL AND free_slot_id IS NOT NULL))
);
CREATE TABLE photo.billing_usage_run_links (
 tenant_id uuid NOT NULL,charge_item_id uuid NOT NULL,run_id uuid NOT NULL,relation text NOT NULL CHECK(relation='root'),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,charge_item_id,run_id),FOREIGN KEY(tenant_id,charge_item_id) REFERENCES photo.billing_usage_charge_items(tenant_id,id),FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id)
);
CREATE TABLE photo.billing_result_references (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL REFERENCES photo.tenants(id),created_at timestamptz NOT NULL DEFAULT now(),
 charge_item_id uuid NOT NULL,canonical_run_id uuid NOT NULL,final_artifact_id uuid NOT NULL,adopted_result_id uuid NOT NULL,asset_version_id uuid NOT NULL,
 adopted_at timestamptz NOT NULL DEFAULT clock_timestamp(),authority_kind text NOT NULL DEFAULT 'run_final' CHECK(authority_kind='run_final'),
 UNIQUE(tenant_id,id),UNIQUE(tenant_id,charge_item_id),UNIQUE(tenant_id,canonical_run_id,asset_version_id),
 FOREIGN KEY(tenant_id,charge_item_id,canonical_run_id) REFERENCES photo.billing_usage_run_links(tenant_id,charge_item_id,run_id),
 FOREIGN KEY(tenant_id,charge_item_id) REFERENCES photo.billing_usage_charge_items(tenant_id,id),FOREIGN KEY(tenant_id,canonical_run_id) REFERENCES photo.runs(tenant_id,id),
 FOREIGN KEY(tenant_id,final_artifact_id) REFERENCES photo.artifacts(tenant_id,id),FOREIGN KEY(tenant_id,adopted_result_id) REFERENCES photo.task_results(tenant_id,id),FOREIGN KEY(tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id)
);
CREATE INDEX billing_request_recovery ON photo.billing_usage_requests(tenant_id,run_id) WHERE status='accepted';
CREATE INDEX billing_initial_inflight ON photo.billing_usage_charge_items(tenant_id,photo_identity_id) WHERE state='accepted' AND charge_kind='initial';
-- +goose StatementBegin
DO $$ DECLARE tab text;BEGIN
 FOREACH tab IN ARRAY ARRAY['billing_workflow_controls','billing_photo_identities','billing_cost_quotes','billing_usage_requests','billing_usage_charge_items','billing_photo_free_slots','billing_usage_tickets','billing_usage_run_links','billing_result_references'] LOOP
  EXECUTE format('REVOKE ALL ON photo.%I FROM PUBLIC',tab);
  EXECUTE format('ALTER TABLE photo.%I ENABLE ROW LEVEL SECURITY',tab);
  EXECUTE format('ALTER TABLE photo.%I FORCE ROW LEVEL SECURITY',tab);
  EXECUTE format('CREATE POLICY billing_functions ON photo.%I TO photo_fn_owner USING (true) WITH CHECK (true)',tab);
  EXECUTE format('GRANT SELECT,INSERT,UPDATE ON photo.%I TO photo_fn_owner',tab);
 END LOOP;
END $$;
-- +goose StatementEnd
CREATE TRIGGER billing_quote_immutable BEFORE UPDATE OR DELETE ON photo.billing_cost_quotes FOR EACH ROW EXECUTE FUNCTION photo.billing_quota_reject_immutable();
CREATE TRIGGER billing_result_immutable BEFORE UPDATE OR DELETE ON photo.billing_result_references FOR EACH ROW EXECUTE FUNCTION photo.billing_quota_reject_immutable();

CREATE INDEX billing_apply_expired_issuers ON photo.billing_admission_leases(tenant_id,resource_code,expires_at,id) WHERE state='active';
