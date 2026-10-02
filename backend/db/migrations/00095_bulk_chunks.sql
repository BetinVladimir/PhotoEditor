-- +goose Up
ALTER TABLE photo.bulk_operations ADD COLUMN next_ordinal bigint NOT NULL DEFAULT 0 CHECK(next_ordinal>=0);
ALTER TABLE photo.bulk_operations ADD COLUMN target_collection_id uuid;
ALTER TABLE photo.bulk_operations ADD COLUMN target_expected_next_revision bigint CHECK(target_expected_next_revision>0);
ALTER TABLE photo.bulk_operations ADD COLUMN pending_event_id uuid;
ALTER TABLE photo.bulk_operations ADD CONSTRAINT bulk_target_pair CHECK((target_collection_id IS NULL)=(target_expected_next_revision IS NULL));
ALTER TABLE photo.bulk_operations ADD FOREIGN KEY(tenant_id,target_collection_id) REFERENCES photo.collections(tenant_id,id);

-- +goose StatementBegin
CREATE FUNCTION photo.enqueue_bulk_chunk()
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.bulk_operations%ROWTYPE;event uuid;
BEGIN
 SELECT * INTO b FROM photo.bulk_operations WHERE status IN ('queued','running','cancelling') AND pending_event_id IS NULL
 ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN false;END IF;
 event:=gen_random_uuid();
 UPDATE photo.bulk_operations SET pending_event_id=event WHERE id=b.id;
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(b.tenant_id,event,'photo.commands.v1','collection.mutate',jsonb_build_object('schema_version',1,'event_id',event,'bulk_id',b.id));
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.enqueue_bulk_chunk() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.enqueue_bulk_chunk() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.enqueue_bulk_chunk() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.process_bulk_chunk(p_bulk uuid,p_event uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate uuid;run_ref uuid;b photo.bulk_operations%ROWTYPE;r photo.runs%ROWTYPE;c photo.collections%ROWTYPE;t photo.collections%ROWTYPE;
 item record;changed_source boolean:=false;changed_target boolean:=false;processed integer:=0;failures integer;next_value bigint;
 tag_value text;tag_mode text;publish_value boolean;event uuid;params_valid boolean:=false;
BEGIN
 IF p_bulk IS NULL OR p_event IS NULL THEN RAISE EXCEPTION 'INVALID_BULK_COMMAND' USING ERRCODE='22023';END IF;
 candidate:=p_bulk;
 SELECT run_id INTO run_ref FROM photo.bulk_operations WHERE id=candidate;
 IF run_ref IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE id=run_ref FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.bulk_operations WHERE id=candidate FOR UPDATE;
 IF NOT FOUND OR b.run_id<>r.id OR b.pending_event_id IS DISTINCT FROM p_event OR b.status IN ('completed','completed_with_errors','failed','cancelled') THEN RETURN false;END IF;
 PERFORM id FROM photo.collections WHERE tenant_id=b.tenant_id AND id IN (b.collection_id,b.target_collection_id) ORDER BY id FOR UPDATE;
 SELECT * INTO c FROM photo.collections WHERE tenant_id=b.tenant_id AND id=b.collection_id;
 IF b.target_collection_id IS NOT NULL THEN SELECT * INTO t FROM photo.collections WHERE tenant_id=b.tenant_id AND id=b.target_collection_id;END IF;
 IF b.cancel_requested OR r.cancel_requested OR b.status='cancelling' THEN
  INSERT INTO photo.bulk_item_results(tenant_id,bulk_operation_id,snapshot_ordinal,status,error_code,result)
   SELECT b.tenant_id,b.id,si.ordinal,'skipped','CANCELLED','{}'::jsonb FROM photo.snapshot_items si
   WHERE si.tenant_id=b.tenant_id AND si.snapshot_id=b.snapshot_id AND si.ordinal>=b.next_ordinal ON CONFLICT DO NOTHING;
  UPDATE photo.bulk_operations SET status='cancelled',cancel_requested=true,pending_event_id=NULL WHERE id=b.id;
  UPDATE photo.runs SET status='cancelled',cancel_requested=true,revision=revision+1 WHERE id=r.id;
  RETURN true;
 END IF;
 IF c.id IS NULL OR c.state='archived' OR c.revision<>b.expected_next_revision
 OR (b.target_collection_id IS NOT NULL AND (t.id IS NULL OR t.state='archived' OR t.revision<>b.target_expected_next_revision)) THEN
  INSERT INTO photo.bulk_item_results(tenant_id,bulk_operation_id,snapshot_ordinal,status,error_code,result)
   SELECT b.tenant_id,b.id,si.ordinal,'failed','REVISION_CONFLICT','{}'::jsonb FROM photo.snapshot_items si
   WHERE si.tenant_id=b.tenant_id AND si.snapshot_id=b.snapshot_id AND si.ordinal>=b.next_ordinal ON CONFLICT DO NOTHING;
  UPDATE photo.bulk_operations SET status='completed_with_errors',pending_event_id=NULL WHERE id=b.id;
  UPDATE photo.runs SET status='completed_with_errors',graph_sealed=true,revision=revision+1 WHERE id=r.id;
  RETURN true;
 END IF;
 IF b.action NOT IN ('rating','flag','tag','hide','publish','copy','move','remove','retry_media') OR jsonb_typeof(b.params)<>'object' THEN
  UPDATE photo.bulk_operations SET status='failed',pending_event_id=NULL WHERE id=b.id;UPDATE photo.runs SET status='failed',revision=revision+1 WHERE id=r.id;RETURN true;
 END IF;
 BEGIN
  params_valid:=CASE b.action
   WHEN 'rating' THEN b.params ? 'rating' AND jsonb_typeof(b.params->'rating')='number' AND (b.params->>'rating')::integer BETWEEN 0 AND 5 AND b.params=jsonb_build_object('rating',(b.params->>'rating')::integer)
   WHEN 'flag' THEN b.params ? 'flag' AND b.params->>'flag' IN ('none','pick','reject') AND b.params=jsonb_build_object('flag',b.params->>'flag')
   WHEN 'tag' THEN b.params ?& ARRAY['tag','mode'] AND length(b.params->>'tag') BETWEEN 1 AND 64 AND b.params->>'tag'=btrim(b.params->>'tag') AND b.params->>'mode' IN ('add','remove') AND b.params=jsonb_build_object('mode',b.params->>'mode','tag',b.params->>'tag')
   WHEN 'hide' THEN b.params='{}'::jsonb
   WHEN 'remove' THEN b.params='{}'::jsonb
   WHEN 'publish' THEN b.params ? 'publish' AND jsonb_typeof(b.params->'publish')='boolean' AND b.params=jsonb_build_object('publish',(b.params->>'publish')::boolean)
   WHEN 'copy' THEN b.target_collection_id IS NOT NULL AND b.params=jsonb_build_object('target_collection_id',b.target_collection_id)
   WHEN 'move' THEN b.target_collection_id IS NOT NULL AND b.params=jsonb_build_object('target_collection_id',b.target_collection_id)
   WHEN 'retry_media' THEN jsonb_typeof(b.params->'variants')='array' AND jsonb_array_length(b.params->'variants') BETWEEN 1 AND 2
    AND NOT EXISTS(SELECT FROM jsonb_array_elements_text(b.params->'variants') v(x) WHERE x NOT IN ('thumbnail','preview'))
    AND (SELECT count(*)=count(DISTINCT x) FROM jsonb_array_elements_text(b.params->'variants') v(x))
    AND b.params=jsonb_build_object('variants',b.params->'variants')
   ELSE false END;
 EXCEPTION WHEN OTHERS THEN params_valid:=false;
 END;
 IF NOT params_valid THEN
  UPDATE photo.bulk_operations SET status='failed',pending_event_id=NULL WHERE id=b.id;UPDATE photo.runs SET status='failed',revision=revision+1 WHERE id=r.id;RETURN true;
 END IF;
 UPDATE photo.bulk_operations SET status='running' WHERE id=b.id;UPDATE photo.runs SET status='running',revision=revision+1 WHERE id=r.id AND status='queued';
 tag_value:=b.params->>'tag';tag_mode:=b.params->>'mode';publish_value:=CASE WHEN b.action='publish' THEN (b.params->>'publish')::boolean ELSE false END;
 FOR item IN SELECT si.ordinal,si.item_id,si.asset_version_id FROM photo.snapshot_items si
  WHERE si.tenant_id=b.tenant_id AND si.snapshot_id=b.snapshot_id AND si.ordinal>=b.next_ordinal
  ORDER BY si.ordinal LIMIT 500
 LOOP
  processed:=processed+1;next_value:=item.ordinal+1;
  BEGIN
   IF NOT EXISTS(SELECT FROM photo.collection_items i WHERE i.tenant_id=b.tenant_id AND i.collection_id=b.collection_id AND i.id=item.item_id AND i.asset_version_id=item.asset_version_id) THEN
    RAISE EXCEPTION 'snapshot item stale' USING ERRCODE='P0001';
   END IF;
   CASE b.action
   WHEN 'rating' THEN UPDATE photo.collection_items SET rating=(b.params->>'rating')::integer WHERE tenant_id=b.tenant_id AND collection_id=b.collection_id AND id=item.item_id;
   WHEN 'flag' THEN UPDATE photo.collection_items SET flag=b.params->>'flag' WHERE tenant_id=b.tenant_id AND collection_id=b.collection_id AND id=item.item_id;
   WHEN 'tag' THEN
    IF tag_mode='add' THEN UPDATE photo.collection_items SET tags=CASE WHEN tags ? tag_value THEN tags ELSE tags||jsonb_build_array(tag_value) END WHERE tenant_id=b.tenant_id AND collection_id=b.collection_id AND id=item.item_id;
    ELSIF tag_mode='remove' THEN UPDATE photo.collection_items SET tags=coalesce((SELECT jsonb_agg(x ORDER BY ord) FROM jsonb_array_elements_text(tags) WITH ORDINALITY q(x,ord) WHERE x<>tag_value),'[]'::jsonb) WHERE tenant_id=b.tenant_id AND collection_id=b.collection_id AND id=item.item_id;
    ELSE RAISE EXCEPTION 'invalid tag mode' USING ERRCODE='22023';END IF;
   WHEN 'hide' THEN
    UPDATE photo.collection_items SET visibility='hidden' WHERE tenant_id=b.tenant_id AND collection_id=b.collection_id AND id=item.item_id;
    UPDATE photo.publications SET state='revoked',policy_version=policy_version+1 WHERE tenant_id=b.tenant_id AND item_id=item.item_id AND state<>'revoked';
   WHEN 'publish' THEN
    IF publish_value THEN
     IF NOT EXISTS(SELECT FROM photo.collection_items i JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.review_state='accepted'
      JOIN photo.assets a ON a.tenant_id=i.tenant_id AND a.id=i.asset_id AND a.upload_state='verified' AND a.deleted_at IS NULL
      JOIN photo.artifacts ar ON ar.tenant_id=v.tenant_id AND ar.id=v.master_artifact_id AND ar.state='ready' AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())
      WHERE i.tenant_id=b.tenant_id AND i.collection_id=b.collection_id AND i.id=item.item_id AND i.moderation_state='accepted') THEN RAISE EXCEPTION 'item not publishable' USING ERRCODE='P0001';END IF;
     UPDATE photo.collection_items SET visibility='visible' WHERE tenant_id=b.tenant_id AND collection_id=b.collection_id AND id=item.item_id;
     INSERT INTO photo.publications(tenant_id,item_id,asset_version_id,state,published_by,published_at) VALUES(b.tenant_id,item.item_id,item.asset_version_id,'published',b.created_by,clock_timestamp())
      ON CONFLICT(tenant_id,item_id) DO UPDATE SET state='published',published_by=EXCLUDED.published_by,published_at=EXCLUDED.published_at,policy_version=photo.publications.policy_version+1;
    ELSE UPDATE photo.collection_items SET visibility='hidden' WHERE tenant_id=b.tenant_id AND collection_id=b.collection_id AND id=item.item_id;UPDATE photo.publications SET state='revoked',policy_version=policy_version+1 WHERE tenant_id=b.tenant_id AND item_id=item.item_id AND state<>'revoked';END IF;
   WHEN 'copy','move' THEN
    INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,section_id,position,visibility,moderation_state,rating,flag,tags,source_item_id)
     SELECT i.tenant_id,b.target_collection_id,i.asset_id,i.asset_version_id,NULL,coalesce((SELECT max(position)+1024 FROM photo.collection_items WHERE tenant_id=b.tenant_id AND collection_id=b.target_collection_id),0),'hidden',i.moderation_state,i.rating,i.flag,i.tags,i.id
     FROM photo.collection_items i WHERE i.tenant_id=b.tenant_id AND i.collection_id=b.collection_id AND i.id=item.item_id;
    IF b.action='move' THEN UPDATE photo.collections SET cover_item_id=NULL WHERE tenant_id=b.tenant_id AND id=b.collection_id AND cover_item_id=item.item_id;DELETE FROM photo.collection_items WHERE tenant_id=b.tenant_id AND collection_id=b.collection_id AND id=item.item_id;END IF;
   WHEN 'remove' THEN UPDATE photo.collections SET cover_item_id=NULL WHERE tenant_id=b.tenant_id AND id=b.collection_id AND cover_item_id=item.item_id;DELETE FROM photo.collection_items WHERE tenant_id=b.tenant_id AND collection_id=b.collection_id AND id=item.item_id;
   WHEN 'retry_media' THEN event:=gen_random_uuid();INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(b.tenant_id,event,'photo.commands.v1','media.retry',jsonb_build_object('schema_version',1,'event_id',event,'bulk_id',b.id,'item_id',item.item_id,'asset_version_id',item.asset_version_id,'variants',b.params->'variants'));
   END CASE;
   INSERT INTO photo.bulk_item_results(tenant_id,bulk_operation_id,snapshot_ordinal,status,result) VALUES(b.tenant_id,b.id,item.ordinal,'succeeded','{}') ON CONFLICT DO NOTHING;
   IF b.action IN ('rating','flag','tag','hide','publish','move','remove') THEN changed_source:=true;END IF;
   IF b.action IN ('copy','move') THEN changed_target:=true;END IF;
  EXCEPTION WHEN unique_violation THEN INSERT INTO photo.bulk_item_results(tenant_id,bulk_operation_id,snapshot_ordinal,status,error_code,result) VALUES(b.tenant_id,b.id,item.ordinal,'failed','ALREADY_EXISTS','{}') ON CONFLICT DO NOTHING;
  WHEN OTHERS THEN INSERT INTO photo.bulk_item_results(tenant_id,bulk_operation_id,snapshot_ordinal,status,error_code,result) VALUES(b.tenant_id,b.id,item.ordinal,'failed','ITEM_CONFLICT','{}') ON CONFLICT DO NOTHING;
  END;
 END LOOP;
 IF changed_source THEN UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=b.tenant_id AND id=b.collection_id;UPDATE photo.bulk_operations SET expected_next_revision=expected_next_revision+1 WHERE id=b.id;END IF;
 IF changed_target THEN UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=b.tenant_id AND id=b.target_collection_id;UPDATE photo.bulk_operations SET target_expected_next_revision=target_expected_next_revision+1 WHERE id=b.id;END IF;
 IF processed>0 THEN UPDATE photo.bulk_operations SET next_ordinal=next_value WHERE id=b.id;END IF;
 IF NOT EXISTS(SELECT FROM photo.snapshot_items si WHERE si.tenant_id=b.tenant_id AND si.snapshot_id=b.snapshot_id AND si.ordinal>=coalesce(next_value,b.next_ordinal)) THEN
  SELECT count(*) INTO failures FROM photo.bulk_item_results WHERE tenant_id=b.tenant_id AND bulk_operation_id=b.id AND status='failed';
  UPDATE photo.bulk_operations SET status=CASE WHEN failures=0 THEN 'completed' ELSE 'completed_with_errors' END WHERE id=b.id;
  UPDATE photo.runs SET status=CASE WHEN failures=0 THEN 'completed' ELSE 'completed_with_errors' END,graph_sealed=true,revision=revision+1 WHERE id=r.id;
 END IF;
 UPDATE photo.bulk_operations SET pending_event_id=NULL WHERE id=b.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.process_bulk_chunk(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.process_bulk_chunk(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.process_bulk_chunk(uuid,uuid) TO photo_worker;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
