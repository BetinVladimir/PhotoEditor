-- +goose Up
-- Bounded relational discovery only; original bytes must still be verified.
-- +goose StatementBegin
CREATE FUNCTION photo.list_v28_relight_candidates(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,p_base jsonb,p_kind text,p_after uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;selected jsonb;origin jsonb;items jsonb:='[]';scanned integer:=0;last_id uuid;next_id uuid;
BEGIN
 IF p_kind IS NULL OR p_kind NOT IN('depth','person','manual_target','manual_guard')
 OR photo.lock_v28_relight_source(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 FOR candidate IN
 SELECT a.id,a.sha256 FROM photo.artifacts a
 JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 WHERE a.tenant_id=p_tenant AND (p_after IS NULL OR a.id>p_after)
 AND a.output_role=CASE p_kind WHEN 'depth' THEN 'relative_depth_ref' WHEN 'person' THEN 'person_mask_ref' WHEN 'manual_target' THEN 'mask_target_ref' ELSE 'mask_guard_ref' END
 AND a.state='ready' AND t.state='succeeded' AND NOT t.cancel_requested AND NOT r.cancel_requested
 AND EXISTS(SELECT 1 FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=r.input_snapshot_id
 AND i.asset_version_id=(p_base->>'asset_version_id')::uuid AND i.artifact_id=(p_base->'source'->>'artifact_id')::uuid AND i.sha256=p_base->'source'->>'sha256')
 ORDER BY a.id LIMIT 101
 LOOP
  scanned:=scanned+1;
  IF scanned=101 THEN next_id:=last_id;EXIT;END IF;
  last_id:=candidate.id;
  selected:=jsonb_build_object('artifact_id',candidate.id,'sha256',candidate.sha256);
  origin:=photo.v28_relight_preintent_producer(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base,selected,p_kind);
  IF origin IS NOT NULL THEN items:=items||jsonb_build_array(jsonb_build_object('ref',selected,'kind',p_kind));END IF;
 END LOOP;
 RETURN jsonb_build_object('items',items,'next_cursor',next_id);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.list_v28_relight_candidates(uuid,uuid,uuid,uuid,uuid,jsonb,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.list_v28_relight_candidates(uuid,uuid,uuid,uuid,uuid,jsonb,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.list_v28_relight_candidates(uuid,uuid,uuid,uuid,uuid,jsonb,text,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
