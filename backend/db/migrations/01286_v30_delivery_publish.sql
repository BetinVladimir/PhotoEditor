-- +goose Up
-- Delivery publication is the immutable published package plus audit receipt.
-- Legacy publications are collection-item scoped and cannot represent a package.
-- +goose StatementBegin
CREATE FUNCTION photo.publish_v30_delivery(p_tenant uuid,p_package uuid,p_actor uuid,p_session uuid,p_revision bigint,p_request uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.delivery_packages%ROWTYPE;r photo.runs%ROWTYPE;review photo.reviews%ROWTYPE;
 authority jsonb;publication uuid;next_cursor bigint;event_id uuid;
BEGIN
 IF p_request IS NULL OR p_revision IS NULL OR p_revision NOT BETWEEN 1 AND 9007199254740990 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=p_tenant AND id=p_package FOR UPDATE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 publication:=photo.v30_uuid5(d.id,'delivery:publication:1');
 IF d.published_at IS NOT NULL THEN
  IF d.state='ready' AND d.revision=p_revision+1 AND EXISTS(SELECT 1 FROM photo.audit_log WHERE tenant_id=p_tenant
  AND actor_id=p_actor AND request_id=p_request AND action='delivery.published' AND resource_id=d.id
  AND details->>'publication_id'=publication::text AND details->>'expected_revision'=p_revision::text) THEN RETURN publication;END IF;
  RETURN NULL;
 END IF;
 authority:=photo.v30_delivery_publication_authority(p_tenant,p_package,p_actor,p_session,p_revision);
 IF authority IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=(authority->>'run_id')::uuid FOR UPDATE;
 IF r.cancel_requested OR r.revision>=9007199254740991 THEN RETURN NULL;END IF;
 IF d.state='review' THEN
  SELECT * INTO review FROM photo.reviews WHERE tenant_id=p_tenant AND id=photo.v30_uuid5(r.id,'delivery:review:1') FOR UPDATE;
  IF NOT FOUND OR review.state<>'pending' OR review.kind<>'delivery' OR review.run_id<>r.id
  OR review.task_id IS DISTINCT FROM (authority->>'task_id')::uuid OR review.candidate_artifact_id IS DISTINCT FROM d.manifest_artifact_id
  OR review.candidate_hash IS DISTINCT FROM authority->>'manifest_sha' OR review.revision>=9007199254740991
  OR r.status<>'waiting_review' THEN RETURN NULL;END IF;
  UPDATE photo.reviews SET state='decided',decision='accept',decided_by=p_actor,decided_at=clock_timestamp(),revision=revision+1
  WHERE tenant_id=p_tenant AND id=review.id;
  UPDATE photo.runs SET status='completed',revision=revision+1 WHERE tenant_id=p_tenant AND id=r.id;
 ELSIF d.state<>'ready' OR r.status<>'completed' THEN RETURN NULL;END IF;
 UPDATE photo.delivery_packages SET state='ready',published_at=clock_timestamp(),revision=revision+1,updated_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND id=d.id;
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details)
 VALUES(p_tenant,'user',p_actor,'delivery.published','delivery_package',d.id,p_request,
 jsonb_build_object('publication_id',publication,'manifest_sha',authority->>'manifest_sha','expected_revision',p_revision,'review_accepted',d.state='review'));
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 event_id:=photo.v30_uuid5(d.id,'delivery:published:event:1');
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,next_cursor,r.id,r.created_by,'principal','delivery.published',jsonb_build_object('run_id',r.id,'package_id',d.id,'publication_id',publication,'manifest_sha',authority->>'manifest_sha'));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,event_id,'photo.events.v1','delivery.published',jsonb_build_object('schema_version',1,'event_id',event_id,'type','delivery.published','tenant_id',p_tenant,'run_id',r.id,'package_id',d.id,'publication_id',publication));
 RETURN publication;
END $$;
ALTER FUNCTION photo.publish_v30_delivery(uuid,uuid,uuid,uuid,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.publish_v30_delivery(uuid,uuid,uuid,uuid,bigint,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.publish_v30_delivery(uuid,uuid,uuid,uuid,bigint,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
