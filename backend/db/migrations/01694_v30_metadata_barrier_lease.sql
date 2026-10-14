-- +goose Up
CREATE INDEX v30_metadata_expansion_pending ON photo.graph_expansions(tenant_id,run_id,next_expansion_at)
 WHERE state='open' AND kind='v30.delivery.metadata/1';
-- +goose StatementBegin
CREATE FUNCTION photo.v30_metadata_barrier_ready(p_tenant uuid,p_child uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;e photo.graph_expansions%ROWTYPE;t photo.tasks%ROWTYPE;request jsonb;policy text;slot integer;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_child AND kind='v30.delivery.metadata/1';
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested OR r.graph_sealed
 OR e.id IS NULL OR e.state<>'open' OR e.expected_count<>1 OR e.cursor<>'0'
 OR e.id<>photo.v30_uuid5(p_child,'delivery:metadata')
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active') THEN RETURN false;END IF;
 request:=r.config_snapshot->'v30_delivery_export';policy:=((request->>'config_jcs')::jsonb)->>'metadata_policy';slot:=(request->>'slot_index')::integer;
 IF policy IS NULL OR policy NOT IN('copyright','approved_exif') OR slot NOT BETWEEN 0 AND 79999
 OR p_child<>photo.v30_uuid5((request->>'parent_run_id')::uuid,'child:export:'||slot::text) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions parent WHERE parent.tenant_id=p_tenant
 AND parent.run_id=(request->>'parent_run_id')::uuid AND parent.kind='v2.delivery/1' AND parent.state='open'
 AND slot<parent.cursor::integer AND photo.v30_delivery_expansion_ready(parent.id) IS TRUE) THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child AND id=e.parent_task_id;
 RETURN t.id=photo.v30_uuid5(photo.v30_uuid5(p_child,'workflow'),'export.encode:0')
 AND t.state='succeeded' AND NOT t.cancel_requested AND t.canonical_result_id IS NOT NULL
 AND EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.task_id=t.id
 AND z.id=t.canonical_result_id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token)
 AND EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v30-encode-verified'
 AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
 AND NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_barrier_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_barrier_ready(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;

CREATE FUNCTION photo.claim_v30_metadata_barrier(p_tenant uuid,p_child uuid)
RETURNS TABLE(expansion_id uuid,lease_token uuid,lease_until timestamptz,parent_task_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;token uuid:=gen_random_uuid();until_time timestamptz;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_child
 AND kind='v30.delivery.metadata/1' FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR e.next_expansion_at>clock_timestamp() OR e.expansion_lease_until>clock_timestamp()
 OR photo.v30_metadata_barrier_ready(p_tenant,p_child) IS DISTINCT FROM true THEN RETURN;END IF;
 until_time:=clock_timestamp()+interval '60 seconds';
 UPDATE photo.graph_expansions SET expansion_token=token,expansion_lease_until=until_time WHERE id=e.id;
 RETURN QUERY SELECT e.id,token,until_time,e.parent_task_id;
END $$;
ALTER FUNCTION photo.claim_v30_metadata_barrier(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v30_metadata_barrier(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.claim_v30_metadata_barrier(uuid,uuid) TO photo_scheduler;

CREATE FUNCTION photo.update_v30_metadata_barrier_lease(p_tenant uuid,p_child uuid,p_expansion uuid,p_token uuid,p_release boolean) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_token IS NULL OR p_release IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child FOR UPDATE;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_child AND id=p_expansion FOR UPDATE;
 IF NOT FOUND OR e.kind<>'v30.delivery.metadata/1' OR e.state<>'open' OR e.expansion_token IS DISTINCT FROM p_token
 OR e.expansion_lease_until IS NULL OR e.expansion_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 IF NOT p_release AND photo.v30_metadata_barrier_ready(p_tenant,p_child) IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.graph_expansions SET expansion_token=CASE WHEN p_release THEN NULL ELSE p_token END,
 expansion_lease_until=CASE WHEN p_release THEN NULL ELSE clock_timestamp()+interval '60 seconds' END,
 next_expansion_at=CASE WHEN p_release THEN clock_timestamp()+interval '5 seconds' ELSE next_expansion_at END WHERE id=e.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.update_v30_metadata_barrier_lease(uuid,uuid,uuid,uuid,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.update_v30_metadata_barrier_lease(uuid,uuid,uuid,uuid,boolean) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.update_v30_metadata_barrier_lease(uuid,uuid,uuid,uuid,boolean) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
