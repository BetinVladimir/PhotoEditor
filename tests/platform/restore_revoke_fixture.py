"""Report-only paired consent fixture; does not stand in for a live revoke drill."""
from hashlib import sha256
from copy import deepcopy
from uuid import uuid4,uuid5
from photo_worker.contracts.base import canonical_bytes


def revoke_probe_observations(replay):
    tenant=uuid4();project=uuid4();actor=uuid4()
    def identity():
        return {'tenant_id':str(tenant),'project_id':str(project),'actor_id':str(actor),
            'consent_id':str(uuid4()),'epoch':1,'revision':2,'purpose':'learning_photographer',
            'person_id':None,'policy_version':'v1','learning_version_id':str(uuid4())}
    af=identity();rf=identity();child=uuid4();workflow=uuid5(child,'workflow')
    common={'run_id':str(replay.run_id),'database':replay.database,
        'build_sha256':replay.build_sha256,'recovery_sha256':replay.recovery_sha256,
        'replay_sha256':replay.replay_sha256}
    asset=uuid4();document=uuid4();source=uuid4();version=uuid4();recipe=uuid4()
    snapshot=uuid4();positive_batch_id=uuid4();positive_child=uuid5(positive_batch_id,'render.batch:0')
    negative_batch_id=uuid4();revision=3
    binding={'actor_id':str(actor),'asset_version_id':str(version),
        'source_artifact_id':str(source),'source_sha256':'b'*64,'canvas_hash':'c'*64}
    pinned_admission={'schema_version':'restore-revoke-fixture/v1',**common,'fixture':af,
        'binding':binding,'asset_id':str(asset),'document_id':str(document),
        'document_sha256':'d'*64,'revision':revision,'source_sha256':'b'*64,
        'snapshot_id':str(snapshot),'policy_sha256':'e'*64,
        'access_denial':[409,'PROFILE_CONSENT_REVOKED']}
    pinned_running={'schema_version':'restore-running-revoke-fixture/v1',**common,
        'fixture':rf,'child_run_id':str(child),'workflow_id':str(workflow),
        'task_id':str(uuid5(workflow,'render.compose:0')),
        'maximum_seconds':30,'barrier_required':True}
    fixture={'schema_version':'restore-combined-revoke-fixture/v1',
        'admission':pinned_admission,'running':pinned_running}
    def consent(source,revoked):
        return {'schema_version':'restore-revoke-consent/v1','database':replay.database,
            'tenant_id':source['tenant_id'],'consent_id':source['consent_id'],
            'revoked':revoked,'epoch':source['epoch']+int(revoked),
            'revision':source['revision']+int(revoked),'affected_versions':1}
    ar=consent(af,True);rr=consent(rf,True);ac=consent(af,False)
    running_witness={'database':replay.database,'tenant_id':str(tenant),
        'child_run_id':str(child),'workflow_id':str(workflow),
        'task_id':pinned_running['task_id'],'attempt':1,'fence':1}
    def receipt(source):
        return {'schema_version':'restore-consent-revoke-receipt/v1',
            'consent_id':source['consent_id'],'epoch':source['epoch']+1,
            'revision':source['revision']+1,'response_sha256':'a'*64,'replayed':False}
    freeze_request=sha256(canonical_bytes({'document_id':str(document),
        'expected_revision':str(revision),'assume_profile':None})).hexdigest()
    frozen={'schema_version':'restore-freeze-baseline/v1','frozen_document_id':str(uuid4()),
        'recipe_artifact_id':str(recipe),'recipe_sha256':'f'*64,
        'request_sha256':freeze_request,'response_sha256':'1'*64,
        'observed_at':'2026-10-12T00:00:00+00:00'}
    physical={'schema_version':'restore-frozen-result/v1','database':replay.database,
        'result':frozen,'object':{'bucket':'fixture','key':'object','version':'v1',
            'sha256':'f'*64,'size':100},'authored_sha256':'d'*64}
    freeze_catalogue={'schema_version':'restore-freeze-catalogue/v1',
        'database':replay.database,'tenant_id':str(tenant),'document_id':str(document),
        'asset_version_id':str(version),'revision':revision}
    denied_freeze=[[ar,freeze_catalogue],{'schema_version':'restore-freeze-denial/v1',
        'tenant_id':str(tenant),'asset_id':str(asset),'document_id':str(document),
        'revision':revision,'request_sha256':freeze_request},[deepcopy(ar),deepcopy(freeze_catalogue)]]
    batch_catalogue={'schema_version':'restore-batch-catalogue/v1',
        'database':replay.database,'batch_id':str(negative_batch_id),'snapshot_id':str(snapshot)}
    request_body={'input_snapshot_id':str(snapshot),'policy_sha':'e'*64,
        'selection':[{'source':{'asset_version_id':str(version),
            'source':{'artifact_id':str(source),'sha256':'b'*64},'canvas_hash':'c'*64},
            'recipe':{'artifact_id':str(recipe),'sha256':'f'*64}}]}
    batch_request=sha256(canonical_bytes(request_body)).hexdigest()
    negative_batch=[[ar,batch_catalogue],
        {'schema_version':'restore-render-batch-refusal/v1',
         'request_sha256':batch_request,'code':'PROFILE_CONSENT_REVOKED'},
        [deepcopy(ar),deepcopy(batch_catalogue)]]
    result_refs=[{'artifact_id':str(uuid4()),'sha256':str(index)*64}
                 for index in (2,3,4)]
    children=[{'bucket':'fixture','key':'tenants/'+str(tenant)+'/output-'+str(index),
        'version':'v1','sha256':ref['sha256'],'size':100}
        for index,ref in enumerate(result_refs)]
    inventory_digest=sha256(b'photoeditor.restore-object-inventory/v1\n')
    for row in sorted(children,key=lambda item:(item['bucket'],item['key'],item['version'])):
        inventory_digest.update(canonical_bytes(row));inventory_digest.update(b'\n')
    positive_workflow=uuid5(positive_child,'workflow')
    image_result={'schema_version':1,'working_image':result_refs[0],
        'report':result_refs[1],'changed_mask':result_refs[2],'generated_mask':None}
    positive_physical={'schema_version':'restore-batch-physical/v1',
        'result':{'schema_version':'restore-batch-result/v1','database':replay.database,
            'task_id':str(uuid5(positive_workflow,'render.compose:0')),'result_id':str(uuid4()),
            'artifact_id':str(uuid4()),'attempt':1,'fence':1,
            'object':{'bucket':'fixture','key':'tenants/'+str(tenant)+'/result','version':'v1',
                'sha256':sha256(canonical_bytes(image_result)).hexdigest(),
                'size':len(canonical_bytes(image_result))}},
        'image_result':image_result,
        'working_image':{'dtype':'float32','pixels':{'artifact_id':str(uuid4()),
            'sha256':'6'*64}},'children':children,
        'inventory':{'schema_version':'restore-object-inventory/v1','objects':3,
            'bytes_verified':300,'inventory_sha256':inventory_digest.hexdigest()},
        'semantics':{'schema_version':'restore-render-semantic/v1','status':'pass',
            'canvas_sha256':'c'*64},'arrays':[{'role':'pixels'}],'parents':{'verified':True}}
    positive_physical['semantics']['pixel_artifact_id']=positive_physical['working_image']['pixels']['artifact_id']
    result_refs[0]['sha256']=sha256(canonical_bytes(positive_physical['working_image'])).hexdigest()
    children[0]['sha256']=result_refs[0]['sha256']
    inventory_digest=sha256(b'photoeditor.restore-object-inventory/v1\n')
    for row in sorted(children,key=lambda item:(item['bucket'],item['key'],item['version'])):
        inventory_digest.update(canonical_bytes(row));inventory_digest.update(b'\n')
    positive_physical['inventory']['inventory_sha256']=inventory_digest.hexdigest()
    positive_physical['result']['object']['sha256']=sha256(canonical_bytes(image_result)).hexdigest()
    positive_physical['result']['object']['size']=len(canonical_bytes(image_result))
    positive_batch={'schema_version':'restore-completed-batch-baseline/v1','polls':1,
        'ticket':{'schema_version':'restore-render-batch-baseline/v1',
            'batch_id':str(positive_batch_id),'child_run_id':str(positive_child),
            'request_sha256':batch_request},
        'adoption':{'schema_version':'restore-batch-adoption/v1','database':replay.database,
            'batch_id':str(positive_batch_id),'child_run_id':str(positive_child),
            'workflow_id':str(positive_workflow)},
        'physical':positive_physical}
    observations={'fixture':fixture,
        'running':{'running':running_witness,'receipt':receipt(rf),
            'committed_consent':rr,
            'cancelled':{'running':running_witness,'state':'cancelled','result_absent':True},
            'polls':1,'barrier_cancelled':True},
        'admission':{'schema_version':'restore-revoke-freeze-lifecycle/v1',
            'baseline_freeze':frozen,'physical_freeze':physical,
            'revoke':{'schema_version':'restore-revoke-observation/v1',
                'baseline_access':{'schema_version':'restore-access-readable/v1',
                    'response_sha256':'d'*64},
                'baseline_consent':ac,'revoke_receipt':receipt(af),
                'committed_consent':ar,'denied_access':{
                    'schema_version':'restore-access-denial/v1','status':409,
                    'code':'PROFILE_CONSENT_REVOKED'},
                'final_consent':ar},'denied_freeze':denied_freeze,'final_consent':ar,
            'positive_batch':positive_batch,
            'negative_batch':negative_batch}}
    return sha256(canonical_bytes(fixture)).hexdigest(),observations
