-- +goose Up
-- Create an empty, quota-reserved open-eyes draft before donor confirmation
-- and Params exist. All current editor, snapshot and donor qualification locks
-- are repeated at the later issuance, publication and admission boundaries.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v28_open_eyes_draft(p_config_jcs text,
 p_proposal text,p_policy_sha text,p_bytes bigint) RETURNS boolean LANGUAGE plpgsql
 SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
#variable_conflict use_variable
DECLARE config jsonb;intent jsonb;pin jsonb;proposal jsonb;tenant_id uuid;project_id uuid;
 snapshot_id uuid;actor_id uuid;session_id uuid;run_id uuid;workflow_id uuid;
 shoot_id uuid;source_observation uuid;donor_observation uuid;old photo.runs%ROWTYPE;
 tenant photo.tenants%ROWTYPE;usage photo.tenant_usage%ROWTYPE;config_sha text;
BEGIN
 IF p_config_jcs IS NULL OR octet_length(p_config_jcs) NOT BETWEEN 2 AND 1048576
 OR p_proposal IS NULL OR octet_length(p_proposal) NOT BETWEEN 2 AND 65536
 OR coalesce(p_policy_sha,'') !~ '^[0-9a-f]{64}$'
 OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2162688 AND 9007199254740991
 THEN RETURN false;END IF;
 config:=p_config_jcs::jsonb;intent:=config->'v28_open_eyes';proposal:=p_proposal::jsonb;
 IF jsonb_typeof(config) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(config) k)
    IS DISTINCT FROM ARRAY['input_snapshot_id','project_id','v28_open_eyes']::text[]
 OR jsonb_typeof(intent) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(intent) k)
    IS DISTINCT FROM ARRAY['actor_id','auth_session_id','base_asset_version_id','client_intent_sha',
      'donor_asset_version_id','donor_observation_id','operation','ordinal',
      'params_proposal_sha','project_id','qualification','run_id','schema_version',
      'shoot_session_id','snapshot_id','source_observation_id','tenant_id','workflow_id']::text[]
 OR intent->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR intent->>'operation' IS DISTINCT FROM 'portrait.open_eyes'
 OR coalesce(intent->>'ordinal','') !~ '^(0|[1-9][0-9]{0,3})$'
 OR (intent->>'ordinal')::integer>=5000
 OR jsonb_typeof(proposal) IS DISTINCT FROM 'object'
 OR intent->>'params_proposal_sha' IS DISTINCT FROM
    encode(sha256(convert_to(p_proposal,'UTF8')),'hex')
 OR coalesce(intent->>'client_intent_sha','') !~ '^[0-9a-f]{64}$'
 OR intent->>'base_asset_version_id' IS DISTINCT FROM
    proposal->'base'->>'asset_version_id'
 OR intent->>'donor_asset_version_id' IS DISTINCT FROM
    proposal->'donor'->>'asset_version_id'
 OR intent->'qualification'->>'policy_sha' IS DISTINCT FROM p_policy_sha
 THEN RETURN false;END IF;
 tenant_id:=(intent->>'tenant_id')::uuid;project_id:=(intent->>'project_id')::uuid;
 snapshot_id:=(intent->>'snapshot_id')::uuid;actor_id:=(intent->>'actor_id')::uuid;
 session_id:=(intent->>'auth_session_id')::uuid;run_id:=(intent->>'run_id')::uuid;
 workflow_id:=(intent->>'workflow_id')::uuid;shoot_id:=(intent->>'shoot_session_id')::uuid;
 source_observation:=(intent->>'source_observation_id')::uuid;
 donor_observation:=(intent->>'donor_observation_id')::uuid;
 pin:=intent->'qualification';
 IF tenant_id IS NULL OR project_id IS NULL OR snapshot_id IS NULL OR actor_id IS NULL
 OR session_id IS NULL OR run_id IS NULL OR workflow_id IS NULL OR shoot_id IS NULL
 OR source_observation IS NULL OR donor_observation IS NULL
 OR source_observation=donor_observation
 OR config->>'project_id' IS DISTINCT FROM project_id::text
 OR config->>'input_snapshot_id' IS DISTINCT FROM snapshot_id::text
 OR workflow_id IS DISTINCT FROM photo.semantic_uuid_v5(run_id,'workflow')
 OR proposal->'confirmation'->>'artifact_id' IS DISTINCT FROM
    photo.semantic_uuid_v5(workflow_id,'portrait.open_eyes:'||(intent->>'ordinal'))::text
 OR (intent->>'base_asset_version_id')::uuid IS NULL
 OR (intent->>'donor_asset_version_id')::uuid IS NULL
 OR intent->>'base_asset_version_id' = intent->>'donor_asset_version_id'
 OR photo.v2_editor_authorized(tenant_id,project_id,actor_id,session_id)
    IS DISTINCT FROM true
 THEN RETURN false;END IF;
 PERFORM 1 FROM photo.snapshots s WHERE s.tenant_id=tenant_id AND s.id=snapshot_id
  AND s.kind='v2' AND s.principal_type='user' AND s.principal_id=actor_id
  AND (s.expires_at IS NULL OR s.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF photo.lock_v28_donor_qualification(tenant_id,project_id,actor_id,
  session_id,pin) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO tenant FROM photo.tenants WHERE id=tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO usage FROM photo.tenant_usage WHERE photo.tenant_usage.tenant_id=tenant_id FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 config_sha:=encode(sha256(convert_to(p_config_jcs,'UTF8')),'hex');
 SELECT * INTO old FROM photo.runs WHERE photo.runs.tenant_id=tenant_id AND id=run_id FOR UPDATE;
 IF FOUND THEN RETURN old.kind='v2' AND old.status='draft' AND NOT old.graph_sealed
  AND NOT old.cancel_requested AND old.created_by=actor_id
  AND old.input_snapshot_id=snapshot_id AND old.config_hash=config_sha
  AND old.config_snapshot=config
  AND EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=tenant_id
   AND w.run_id=run_id AND w.id=workflow_id AND w.project_id=project_id
   AND w.input_snapshot_id=snapshot_id AND w.workflow_type='preview'
   AND w.renderer_version='v2.2' AND w.policy_sha=p_policy_sha)
  AND EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=tenant_id
   AND q.holder_type='run' AND q.holder_id=run_id AND q.state='reserved'
   AND q.bytes=p_bytes AND q.expires_at>clock_timestamp());END IF;
 IF tenant.quotas->>'storage_bytes' IS NULL OR
  usage.storage_bytes::numeric+usage.reserved_bytes+p_bytes>
   (tenant.quotas->>'storage_bytes')::numeric OR
  usage.storage_bytes::numeric+usage.reserved_bytes+p_bytes>9007199254740991
 THEN RETURN false;END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,
  config_hash,created_by,failure_policy,graph_sealed)
 VALUES(run_id,tenant_id,'v2','draft',snapshot_id,config,config_sha,actor_id,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,
  workflow_type,renderer_version,policy_sha)
 VALUES(workflow_id,tenant_id,project_id,run_id,snapshot_id,'preview','v2.2',p_policy_sha);
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,
  holder_type,holder_id,bytes,files,expires_at)
 VALUES(tenant_id,'user',actor_id,'run',run_id,p_bytes,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes
  WHERE photo.tenant_usage.tenant_id=tenant_id;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR
 invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_v28_open_eyes_draft(text,text,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v28_open_eyes_draft(text,text,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_v28_open_eyes_draft(text,text,text,bigint) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
