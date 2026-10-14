-- +goose Up
-- Records admission of an already sealed child, not readiness of its raster.
-- +goose StatementBegin
CREATE FUNCTION photo.commit_v30_delivery_child(p_expansion uuid,p_token uuid,p_slot integer,p_request text,p_export uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;parent photo.runs%ROWTYPE;child photo.runs%ROWTYPE;
 w photo.v2_workflows%ROWTYPE;d photo.delivery_packages%ROWTYPE;i photo.delivery_items%ROWTYPE;x photo.exports%ROWTYPE;
 b photo.backend_documents%ROWTYPE;frozen jsonb;snapshot jsonb;preset jsonb;item jsonb;cfg jsonb;request jsonb;expected jsonb;
 presets integer;v_ordinal integer;child_id uuid;h bytea;
BEGIN
 IF p_token IS NULL OR p_slot IS NULL OR p_slot NOT BETWEEN 0 AND 79999 OR p_export IS NULL
 OR p_request IS NULL OR octet_length(p_request) NOT BETWEEN 2 AND 2097152 THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.delivery/1';
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=e.tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=e.tenant_id AND id=e.run_id FOR UPDATE;
 SELECT z.* INTO e FROM photo.graph_expansions z WHERE z.id=p_expansion AND z.tenant_id=e.tenant_id FOR UPDATE;
 IF e.expansion_token IS DISTINCT FROM p_token OR e.expansion_lease_until IS NULL OR e.expansion_lease_until<=clock_timestamp()
 OR photo.v30_delivery_expansion_ready(e.id) IS DISTINCT FROM true OR p_slot>=e.expected_count OR p_slot>e.cursor::integer THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=e.tenant_id AND run_id=e.run_id;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=e.tenant_id AND workflow_id=w.id FOR UPDATE;
 SELECT * INTO b FROM photo.backend_documents WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND owner_id=e.parent_task_id AND purpose='task_params' AND state='ready';
 frozen:=(b.v30_delivery_proof->>'frozen_input_jcs')::jsonb;snapshot:=(b.v30_delivery_proof->>'snapshot_jcs')::jsonb;
 presets:=jsonb_array_length(frozen->'presets');v_ordinal:=p_slot/presets;item:=snapshot->'items'->v_ordinal;
 SELECT value INTO preset FROM jsonb_array_elements(frozen->'presets') ORDER BY value->>'preset_id' OFFSET (p_slot%presets) LIMIT 1;
 SELECT value INTO cfg FROM jsonb_array_elements(b.v30_delivery_proof->'configs_jcs') WHERE value->>'preset_id'=preset->>'preset_id';
 h:=public.digest(uuid_send(e.run_id)||convert_to('child:export:'||p_slot::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 child_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 expected:=jsonb_build_object('schema_version',1,'parent_run_id',e.run_id,'child_run_id',child_id,'slot_index',p_slot,'ordinal',v_ordinal,
 'binding',item->'binding','recipe',item->'recipe','snapshot_digest',frozen->>'snapshot_digest','preset',preset,'config_jcs',cfg->>'config_jcs');
 request:=p_request::jsonb;
 IF request IS DISTINCT FROM expected THEN RETURN false;END IF;
 SELECT * INTO child FROM photo.runs WHERE tenant_id=e.tenant_id AND id=child_id FOR SHARE;
 IF child.id IS NULL OR child.kind<>'v2' OR child.created_by IS DISTINCT FROM parent.created_by
 OR child.input_snapshot_id IS DISTINCT FROM parent.input_snapshot_id OR child.cancel_requested OR NOT child.graph_sealed
 OR child.status NOT IN('queued','running','completed')
 OR child.config_snapshot IS DISTINCT FROM jsonb_build_object('project_id',w.project_id,'v30_delivery_export',expected,'export_id',p_export)
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows cw WHERE cw.tenant_id=e.tenant_id AND cw.run_id=child_id
 AND cw.project_id=w.project_id AND cw.input_snapshot_id=w.input_snapshot_id AND cw.workflow_type='delivery' AND cw.renderer_version='v2.2')
 OR NOT EXISTS(SELECT 1 FROM photo.tasks ct WHERE ct.tenant_id=e.tenant_id AND ct.run_id=child_id
 AND ct.resource_class='cpu' AND ct.operation IN('derivative.resize','derivative.sharpen','export.encode')) THEN RETURN false;END IF;
 SELECT * INTO x FROM photo.exports WHERE tenant_id=e.tenant_id AND id=p_export FOR SHARE;
 IF x.id IS NULL OR x.asset_version_id::text IS DISTINCT FROM item->'binding'->>'asset_version_id'
 OR x.preset_hash IS DISTINCT FROM preset->>'preset_sha' OR x.metadata_policy IS DISTINCT FROM jsonb_build_object('metadata_policy','strip')
 OR (x.task_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM photo.tasks xt WHERE xt.tenant_id=e.tenant_id AND xt.id=x.task_id
 AND xt.run_id=child_id AND xt.operation='export.encode' AND xt.operation_version='1')) THEN RETURN false;END IF;
 SELECT * INTO i FROM photo.delivery_items WHERE tenant_id=e.tenant_id AND package_id=d.id AND ordinal=v_ordinal
 AND preset_id=(preset->>'preset_id')::uuid FOR UPDATE;
 IF i.id IS NULL OR i.preset_sha IS DISTINCT FROM preset->>'preset_sha' OR i.required IS DISTINCT FROM (preset->>'required')::boolean THEN RETURN false;END IF;
 IF p_slot<e.cursor::integer THEN RETURN i.export_id IS NOT DISTINCT FROM p_export;END IF;
 IF i.state<>'pending' OR (i.export_id IS NOT NULL AND i.export_id<>p_export) THEN RETURN false;END IF;
 IF i.export_id IS NULL THEN
  UPDATE photo.delivery_items SET export_id=p_export,revision=revision+1,updated_at=clock_timestamp()
  WHERE tenant_id=e.tenant_id AND id=i.id AND revision<9007199254740991;
  IF NOT FOUND THEN RAISE EXCEPTION 'DELIVERY_ITEM_REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 END IF;
 UPDATE photo.graph_expansions SET cursor=(p_slot+1)::text WHERE id=e.id AND expansion_token=p_token AND expansion_lease_until>clock_timestamp();
 IF NOT FOUND THEN RAISE EXCEPTION 'DELIVERY_CURSOR_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.commit_v30_delivery_child(uuid,uuid,integer,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.commit_v30_delivery_child(uuid,uuid,integer,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.commit_v30_delivery_child(uuid,uuid,integer,text,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
