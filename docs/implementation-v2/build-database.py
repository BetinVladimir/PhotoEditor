"""Generate a reference DDL inside documentation, never execute it."""
from pathlib import Path
import json
R=Path(__file__).resolve().parent; C=R/'contracts'
# DSL types: uuid -> tenant FK map; ? nullable; all other columns NOT NULL.
spec = {
'v2_workflows':(196,False,'run_id:uuid input_snapshot_id:uuid workflow_type:text renderer_version:text policy_sha:sha'),
'v2_documents':(196,False,'stream_id:uuid version_no:bigint document_type:text artifact_id:uuid sha256:sha'),
'v2_document_streams':(196,True,'asset_version_id:uuid? stream_type:text current_document_id:uuid? approved_document_id:uuid?'),
'v2_document_refs':(196,False,'document_id:uuid artifact_id:uuid sha256:sha purpose:text ordinal:int'),
'v2_artifact_links':(196,False,'workflow_id:uuid artifact_id:uuid sha256:sha purpose:text ordinal:int'),
'model_qualifications':(196,False,'operation:text operation_version:text model_lock_sha:sha backend:text device_profile_sha:sha report_artifact_id:uuid status:text'),
'shoot_sessions':(197,True,'collection_id:uuid name:text state:text policy:json capture_policy:json? consent_epoch:bigint'),
'session_analyses':(197,False,'session_id:uuid workflow_id:uuid snapshot_id:uuid plan_artifact_id:uuid policy_sha:sha model_lock_sha:sha'),
'scene_groups':(197,False,'analysis_id:uuid ordinal:int reference_version_id:uuid confidence:unit'),
'scene_members':(197,False,'group_id:uuid asset_version_id:uuid snapshot_ordinal:int'),
'project_persons':(197,True,'label:text state:text consent_epoch:bigint'),
'person_observations':(197,True,'person_id:uuid asset_version_id:uuid face_id:uuid evidence_artifact_id:uuid confirmed_by:uuid?'),
'consent_grants':(197,True,'purpose:text person_id:uuid? status:text epoch:bigint policy_version:text actor_id:uuid'),
'consent_events':(197,False,'consent_id:uuid epoch:bigint action:text actor_id:uuid policy_version:text'),
'workflow_consent_links':(197,False,'workflow_id:uuid consent_id:uuid consent_epoch:bigint'),
'correction_events':(198,False,'session_id:uuid asset_version_id:uuid base_document_id:uuid approved_document_id:uuid changed_paths:array reason:text learn:bool consent_id:uuid? consent_epoch:bigint? approved_by:uuid client_event_id:uuid person_id:uuid? scene_group_id:uuid?'),
'learning_profiles':(198,True,'name:text mode:text person_id:uuid? created_by:uuid active_version_id:uuid?'),
'learning_versions':(198,False,'profile_id:uuid version_no:bigint workflow_id:uuid training_artifact_id:uuid model_artifact_id:uuid evaluation_artifact_id:uuid parent_version_id:uuid?'),
'version_eligibility':(198,True,'version_id:uuid status:text reason:text?'),
'learning_example_links':(198,False,'version_id:uuid event_id:uuid consent_id:uuid consent_epoch:bigint'),
'model_use_links':(198,False,'document_id:uuid version_id:uuid'),
'profile_activations':(198,False,'profile_id:uuid version_id:uuid actor_id:uuid reason:text previous_version_id:uuid?'),
'consent_cleanup':(198,True,'consent_id:uuid artifact_id:uuid state:text attempts:int next_attempt_at:time'),
'capture_devices':(199,True,'label:text public_key:text credential_epoch:bigint created_by:uuid state:text last_seen_at:time?'),
'capture_challenges':(199,True,'device_id:uuid nonce_ciphertext:bytes key_version:text nonce_hash:sha credential_epoch:bigint expires_at:time consumed_at:time?'),
'capture_receipts':(199,False,'device_id:uuid session_id:uuid sequence:bigint content_sha:sha upload_id:uuid captured_at:time?'),
'capture_receipt_assets':(199,False,'receipt_id:uuid asset_version_id:uuid'),
'capture_alerts':(199,True,'session_id:uuid asset_version_id:uuid report_artifact_id:uuid severity:text receipt_id:uuid? qa_stale:bool acknowledged_by:uuid? acknowledged_at:time?'),
'shot_lists':(199,True,'session_id:uuid document_id:uuid'),
'semantic_intents':(202,False,'workflow_id:uuid reference_version_id:uuid snapshot_id:uuid intent_artifact_id:uuid intent_sha:sha'),
'semantic_targets':(202,True,'intent_id:uuid child_run_id:uuid? batch_id:uuid? asset_version_id:uuid candidate_artifact_id:uuid? review_id:uuid? override_id:uuid? state:text'),
'delivery_packages':(205,True,'workflow_id:uuid snapshot_id:uuid name:text policy_artifact_id:uuid manifest_artifact_id:uuid? published_at:time? state:text'),
'delivery_items':(205,True,'package_id:uuid ordinal:int preset_id:uuid preset_sha:sha required:bool export_id:uuid? state:text reason:text?'),
'interchange_jobs':(205,True,'workflow_id:uuid direction:text format:text mapping_artifact_id:uuid? report_artifact_id:uuid? bundle_artifact_id:uuid?'),
'provenance_manifests':(206,False,'export_id:uuid workflow_id:uuid manifest_artifact_id:uuid signed_artifact_id:uuid signer_key_id:text signing_policy_sha:sha'),
}
fk={'run_id':'runs','input_snapshot_id':'snapshots','snapshot_id':'snapshots','stream_id':'v2_document_streams','document_id':'v2_documents','current_document_id':'v2_documents','approved_document_id':'v2_documents','base_document_id':'v2_documents','asset_version_id':'asset_versions','reference_version_id':'asset_versions','collection_id':'collections','workflow_id':'v2_workflows','session_id':'shoot_sessions','analysis_id':'session_analyses','group_id':'scene_groups','scene_group_id':'scene_groups','person_id':'project_persons','consent_id':'consent_grants','profile_id':'learning_profiles','version_id':'learning_versions','active_version_id':'learning_versions','parent_version_id':'learning_versions','previous_version_id':'learning_versions','event_id':'correction_events','device_id':'capture_devices','receipt_id':'capture_receipts','intent_id':'semantic_intents','package_id':'delivery_packages','export_id':'exports','preset_id':'export_presets','upload_id':'upload_sessions','review_id':'reviews','override_id':'overrides'}
actors={'actor_id','approved_by','created_by','confirmed_by','acknowledged_by'}
enums={
('v2_workflows','workflow_type'):'session learning capture semantic delivery interchange provenance preview',
('v2_document_streams','stream_type'):'draft shot_list',
('v2_documents','document_type'):'EditingRecipeV3 LayerStack ShotList',
('model_qualifications','backend'):'cpu cuda mps',('model_qualifications','status'):'passed failed',
('shoot_sessions','state'):'open closed archived',('project_persons','state'):'active revoked',
('consent_grants','purpose'):'identity_matching learning_photographer learning_person',
('consent_grants','status'):'granted revoked',('consent_events','action'):'grant revoke',
('learning_profiles','mode'):'photographer person',
('correction_events','reason'):'exposure color crop retouch selection other',
('version_eligibility','status'):'eligible rejected disabled',('profile_activations','reason'):'promote rollback',
('consent_cleanup','state'):'pending deleting deleted blocked_shared',
('capture_devices','state'):'active revoked',('capture_alerts','severity'):'info warning critical',
('semantic_targets','state'):'proposed approved rejected unsupported applied',
('delivery_packages','state'):'building review ready failed cancelled',
('delivery_items','state'):'pending ready failed omitted',
('interchange_jobs','direction'):'import export',('interchange_jobs','format'):'xmp photoeditor_bundle captureone_sidecar'}
unique={
'v2_workflows':['run_id'],'v2_documents':['stream_id,version_no'],'v2_document_refs':['document_id,purpose,ordinal'],
'v2_artifact_links':['workflow_id,purpose,ordinal'],'session_analyses':['workflow_id'],'scene_groups':['analysis_id,ordinal'],
'scene_members':['group_id,asset_version_id'],'person_observations':['person_id,asset_version_id,face_id'],
'consent_events':['consent_id,epoch'],'workflow_consent_links':['workflow_id,consent_id'],
'correction_events':['client_event_id'],'learning_versions':['profile_id,version_no','workflow_id'],
'version_eligibility':['version_id'],'learning_example_links':['version_id,event_id'],'model_use_links':['document_id,version_id'],
'capture_challenges':['nonce_hash'],'capture_receipts':['device_id,sequence'],'capture_receipt_assets':['receipt_id'],
'shot_lists':['session_id'],'semantic_intents':['workflow_id'],'semantic_targets':['intent_id,asset_version_id'],
'delivery_packages':['workflow_id'],'delivery_items':['package_id,ordinal,preset_id'],
'interchange_jobs':['workflow_id'],'provenance_manifests':['export_id,signing_policy_sha'],
'consent_cleanup':['consent_id,artifact_id']}
types={'bytes':'bytea','uuid':'uuid','bigint':'bigint','int':'integer','text':'text','sha':'text','json':'jsonb','array':'jsonb','bool':'boolean','unit':'double precision','time':'timestamptz'}
extra={
'correction_events':["(learn AND consent_id IS NOT NULL AND consent_epoch IS NOT NULL) OR (NOT learn AND consent_id IS NULL AND consent_epoch IS NULL)"],
'capture_alerts':["(acknowledged_by IS NULL) = (acknowledged_at IS NULL)"],
'learning_profiles':["(mode='person') = (person_id IS NOT NULL)"],
'consent_grants':["(purpose='learning_photographer') = (person_id IS NULL)"],
'delivery_items':["state <> 'ready' OR export_id IS NOT NULL"]}

# Completion of review, immutable approval and delegated device authentication.
spec.update({
'v2_document_approvals':(196,False,'document_id:uuid actor_id:uuid recipe_sha:sha resolved_artifact_id:uuid'),
'v2_task_inputs':(196,False,'task_id:uuid artifact_id:uuid sha256:sha contract:text ordinal:int'),
'v2_review_batches':(196,False,'workflow_id:uuid child_run_id:uuid? decisions_artifact_id:uuid actor_id:uuid action:text'),
'learning_safety_reviews':(198,False,'version_id:uuid evaluation_sha:sha actor_id:uuid accept:bool reason:text'),
'interchange_targets':(205,True,'job_id:uuid entry_id:uuid proposed_document_id:uuid? state:text batch_id:uuid?'),
})
fk.update(task_id='tasks',child_run_id='runs',batch_id='v2_review_batches',job_id='interchange_jobs',proposed_document_id='v2_documents')
enums[('v2_review_batches','action')]='apply decide close'
enums[('interchange_targets','state')]='proposed approved rejected unsupported'
enums[('version_eligibility','status')]='eligible rejected needs_review disabled'
enums[('semantic_targets','state')]='proposed queued rejected unsupported applied failed cancelled'
unique['v2_task_inputs']=['task_id,artifact_id','task_id,ordinal']
unique['v2_document_approvals']=['document_id']
unique['person_observations']=['asset_version_id,face_id']
unique['interchange_targets']=['job_id,entry_id']
extra['v2_document_streams']=["(stream_type='draft') = (asset_version_id IS NOT NULL)"]
extra['delivery_packages']=["published_at IS NULL OR (state='ready' AND manifest_artifact_id IS NOT NULL)"]

tables=[];sql=['-- Reference schema, not a deployable migration. Function/trigger contracts: 03, 09, 11, 13.','-- Apply only after generating migration functions and all acceptance tests.']
relations=[]
for table,(migration,mutable,raw) in spec.items():
 cols=[{'name':'id','ddl':'uuid PRIMARY KEY DEFAULT gen_random_uuid()'},
 {'name':'tenant_id','ddl':'uuid NOT NULL REFERENCES photo.tenants(id)'},
 {'name':'project_id','ddl':'uuid NOT NULL'},
 {'name':'created_at','ddl':'timestamptz NOT NULL DEFAULT now()'}]
 if mutable:cols += [{'name':'revision','ddl':'bigint NOT NULL DEFAULT 1 CHECK (revision > 0)'},{'name':'updated_at','ddl':'timestamptz NOT NULL DEFAULT now()'}]
 for token in raw.split():
  name,typ=token.split(':');nullable=typ.endswith('?');typ=typ.rstrip('?');ddl=types[typ]+('' if nullable else ' NOT NULL')
  checks=[]
  if typ=='sha':checks += [name+" ~ '^[0-9a-f]{64}$'"]
  if typ=='text':checks += ['length('+name+') BETWEEN 1 AND '+str(200 if name=='name' else 256)]
  if typ in ('json','array'):checks += ["jsonb_typeof("+name+") = '"+('array' if typ=='array' else 'object')+"'",'octet_length('+name+'::text) <= 1048576']
  if typ in ('bigint','int'):checks += [name+(' >= 0' if name in ('ordinal','snapshot_ordinal','sequence','attempts') else ' > 0')]
  if typ=='unit':checks += [name+" BETWEEN 0 AND 1 AND "+name+" <> 'NaN'::double precision"]
  if (table,name) in enums:checks += [name+" IN ("+','.join("'"+v+"'" for v in enums[table,name].split())+")"]
  for check in checks:ddl+=' CHECK ('+check+')'
  cols.append({'name':name,'ddl':ddl,'nullable':nullable,'enum':enums.get((table,name),'').split() or None})
  target='artifacts' if name.endswith('artifact_id') else 'users' if name in actors else fk.get(name)
  if typ=='uuid' and target:relations.append((table,name,target))
 constraints=['UNIQUE (tenant_id,id)','UNIQUE (tenant_id,project_id,id)','FOREIGN KEY (tenant_id,project_id) REFERENCES photo.projects(tenant_id,id) DEFERRABLE INITIALLY DEFERRED']
 constraints += ['UNIQUE (tenant_id,'+v+')' for v in unique.get(table,[])]
 constraints += ['CHECK ('+c+')' for c in extra.get(table,[])]
 tables.append({'name':table,'migration':migration,'mutable':mutable,'columns':cols,'constraints':constraints})
 sql+=['CREATE TABLE photo.'+table+' (\n  '+',\n  '.join(c['name']+' '+c['ddl'] for c in cols)+',\n  '+',\n  '.join(constraints)+'\n);']
for table,name,target in relations:
 if target in spec: left='tenant_id,project_id,'+name;right='tenant_id,project_id,id'
 elif target=='users':left=name;right='id'
 else:left='tenant_id,'+name;right='tenant_id,id'
 sql+=['ALTER TABLE photo.'+table+' ADD FOREIGN KEY ('+left+') REFERENCES photo.'+target+'('+right+') DEFERRABLE INITIALLY DEFERRED;']
 sql+=['CREATE INDEX ON photo.'+table+' ('+left+');']
for t in tables:
 n=t['name']
 sql+=['ALTER TABLE photo.'+n+' ENABLE ROW LEVEL SECURITY;','ALTER TABLE photo.'+n+' FORCE ROW LEVEL SECURITY;',
 "CREATE POLICY tenant_scope ON photo."+n+" TO photo_api USING (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);",
 'CREATE POLICY trusted_functions ON photo.'+n+' TO photo_fn_owner USING (true) WITH CHECK (true);',
 'REVOKE ALL ON photo.'+n+' FROM PUBLIC, photo_api, photo_worker, photo_scheduler, photo_auth, photo_portal, photo_publisher, photo_maintenance, photo_gateway;',
 'GRANT SELECT ON photo.'+n+' TO photo_api;',
 'GRANT SELECT, INSERT'+(', UPDATE' if t['mutable'] else '')+', DELETE ON photo.'+n+' TO photo_fn_owner;',
 'CREATE INDEX ON photo.'+n+' (tenant_id,project_id,created_at,id);']
sql += [
"CREATE UNIQUE INDEX ON photo.consent_grants (tenant_id,project_id,purpose,person_id) WHERE person_id IS NOT NULL;",
"CREATE UNIQUE INDEX ON photo.consent_grants (tenant_id,project_id,purpose) WHERE person_id IS NULL;",
"CREATE UNIQUE INDEX ON photo.v2_document_streams (tenant_id,project_id,asset_version_id,stream_type) WHERE asset_version_id IS NOT NULL;"
]
(C/'database.json').write_text(json.dumps({'schema_version':1,'baseline':195,'tables':tables,'relations':[{'table':a,'column':b,'target':c} for a,b,c in relations],'api_write':'SECURITY DEFINER functions only','purge_allowlist':[t['name'] for t in tables]},ensure_ascii=False,indent=2)+'\n')
(C/'schema-reference.sql').write_text('\n\n'.join(sql)+'\n')
print(len(tables),'tables',len(relations),'foreign keys')
