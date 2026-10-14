"""Complete required restore probe orchestration; no writer authorization.

Concrete probe runners and independent current-authority checks must be installed
by the operator. Returned declarations alone are not production drill evidence.
"""
from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path
from photo_worker.contracts.base import canonical_bytes, parse_json
from .restore_key_installation import _read_private
from datetime import datetime, timezone
import re
from uuid import UUID,uuid5
from .restore_replay_receipt import VerifiedRestoreReplay, verify_restore_replay_files

PROBE_CHECKS = (
    ('legacy_apply', ('preview_ready', 'apply_committed', 'download_verified')),
    ('v2_workflow', ('workflow_completed', 'review_completed', 'download_verified')),
    ('purge_negative', ('objects_absent', 'metadata_absent', 'access_denied')),
    ('revoke_negative', ('consent_current', 'processing_denied', 'access_denied')),
)


@dataclass(frozen=True)
class RestoreProbeEvidence:
    kind: str
    run_id: UUID
    database: str
    build_sha256: str
    fixture_sha256: str
    started_at: datetime
    finished_at: datetime
    checks: tuple[tuple[str, bool], ...]
    evidence_sha256: str


@dataclass(frozen=True)
class RestoreProbeReceipt:
    path: Path
    sha256: str


def read_restore_probe_report(receipt, replay, *, check_active):
    """Read the runner's actual private artifact; no synthetic in-memory result."""
    if (not isinstance(receipt,RestoreProbeReceipt) or not isinstance(replay,VerifiedRestoreReplay)
            or not isinstance(receipt.sha256,str) or re.fullmatch('[0-9a-f]{64}',receipt.sha256) is None
            or not callable(check_active)):
        raise ValueError('RESTORE_PROBE_RECEIPT')
    def guard():_require_restore_guard(check_active,'RESTORE_PROBE_READER_INACTIVE')
    raw=_read_private(receipt.path,262144,guard)
    if sha256(raw).hexdigest()!=receipt.sha256:raise ValueError('RESTORE_PROBE_PIN')
    value=parse_json(raw,max_bytes=262144)
    keys={'schema_version','kind','run_id','database','build_sha256','fixture_sha256',
          'recovery_sha256','replay_sha256','started_at','finished_at','checks','observations'}
    if (not isinstance(value,dict) or set(value)!=keys or canonical_bytes(value)!=raw
            or value['schema_version']!='restore-probe-report/v1'
            or value['kind'] not in tuple(kind for kind,_ in PROBE_CHECKS)
            or not isinstance(value['fixture_sha256'],str)
            or re.fullmatch('[0-9a-f]{64}',value['fixture_sha256']) is None
            or value['run_id']!=str(replay.run_id) or value['database']!=replay.database
            or value['build_sha256']!=replay.build_sha256
            or value['recovery_sha256']!=replay.recovery_sha256 or value['replay_sha256']!=replay.replay_sha256
            or not isinstance(value['observations'],dict) or not value['observations']
            or not isinstance(value['checks'],list)
            or any(not isinstance(pair,list) or len(pair)!=2 or not isinstance(pair[0],str)
                   or type(pair[1]) is not bool for pair in value['checks'])):
        raise ValueError('RESTORE_PROBE_REPORT')
    def timestamp(key):
        text=value[key]
        if not isinstance(text,str):raise ValueError('RESTORE_PROBE_TIME')
        result=datetime.fromisoformat(text.replace('Z','+00:00'))
        if result.tzinfo is None:raise ValueError('RESTORE_PROBE_TIME')
        return result
    result=RestoreProbeEvidence(value['kind'],replay.run_id,replay.database,replay.build_sha256,
        value['fixture_sha256'],timestamp('started_at'),timestamp('finished_at'),
        tuple(tuple(pair) for pair in value['checks']),receipt.sha256)
    validate_restore_probe_observations(value['kind'],value['observations'],replay,value['fixture_sha256'])
    if _read_private(receipt.path,262144,guard)!=raw:raise ValueError('RESTORE_PROBE_CHANGED')
    guard()
    return result


def validate_restore_probe_observations(kind,observations,replay,fixture_sha256):
    """Validate the report body before exclusive publication and on every read."""
    validators={'v2_workflow':_validate_v2_workflow_observations,
        'legacy_apply':_validate_legacy_apply_observations,
        'purge_negative':_validate_purge_negative_observations,
        'revoke_negative':_validate_revoke_negative_observations}
    if kind not in validators or not isinstance(observations,dict):
        raise ValueError('RESTORE_PROBE_OBSERVATIONS')
    validators[kind](observations,replay,fixture_sha256)


def _validate_v2_workflow_observations(observations,replay,fixture_sha256):
    required={'fixture','batch_id','child_run_id','review_id','candidate_sha256',
        'before_preview_sha256','after_preview_sha256','after_manifest_sha256',
        'download_sha256','download_manifest_sha256','batch_polls','terminal_polls'}
    if set(observations)!=required or not isinstance(observations['fixture'],dict):
        raise ValueError('RESTORE_V2_PROBE_OBSERVATIONS')
    fixture=observations['fixture']
    fixture_keys={'schema_version','restore_run_id','database','build_sha256',
        'recovery_sha256','replay_sha256','tenant_id','project_id','snapshot_id',
        'policy_sha256','idempotency_key_sha256','actor_id','asset_version_id',
        'source_artifact_id','source_sha256','canvas_hash','frozen_document_id',
        'recipe_artifact_id','recipe_sha256','session_id','api_origin',
        'object_origin','buckets','srgb_sha256','maximum_seconds'}
    if (set(fixture)!=fixture_keys
            or fixture['schema_version']!='restore-v2-workflow-fixture/v1'
            or fixture['restore_run_id']!=str(replay.run_id)
            or fixture['database']!=replay.database
            or fixture['build_sha256']!=replay.build_sha256
            or fixture['recovery_sha256']!=replay.recovery_sha256
            or fixture['replay_sha256']!=replay.replay_sha256
            or sha256(canonical_bytes(fixture)).hexdigest()!=fixture_sha256):
        raise ValueError('RESTORE_V2_PROBE_FIXTURE')
    try:
        ids=[UUID(observations[key]) for key in ('batch_id','child_run_id','review_id')]
        fixture_ids=[UUID(fixture[key]) for key in ('tenant_id','project_id','snapshot_id',
            'actor_id','asset_version_id','source_artifact_id','frozen_document_id',
            'recipe_artifact_id','session_id')]
    except (ValueError,TypeError,AttributeError,KeyError):
        raise ValueError('RESTORE_V2_PROBE_IDENTITY') from None
    if (any(item.int==0 for item in ids+fixture_ids) or len(set(ids))!=3
            or any(type(observations[key]) is not int or observations[key]<1
                   for key in ('batch_polls','terminal_polls'))
            or type(fixture['maximum_seconds']) is not int
            or not 1<=fixture['maximum_seconds']<=1200
            or not isinstance(fixture['buckets'],list) or not fixture['buckets']
            or fixture['buckets']!=sorted(set(fixture['buckets']))
            or any(not isinstance(bucket,str) or not bucket for bucket in fixture['buckets'])):
        raise ValueError('RESTORE_V2_PROBE_IDENTITY')
    hashes=('candidate_sha256','before_preview_sha256','after_preview_sha256',
        'after_manifest_sha256','download_sha256','download_manifest_sha256')
    fixture_hashes=('policy_sha256','idempotency_key_sha256','source_sha256',
        'canvas_hash','recipe_sha256','srgb_sha256')
    if (any(not isinstance(observations[key],str) or re.fullmatch('[0-9a-f]{64}',observations[key]) is None
            for key in hashes)
            or any(not isinstance(fixture[key],str) or re.fullmatch('[0-9a-f]{64}',fixture[key]) is None
                   for key in fixture_hashes)
            or observations['download_sha256']!=observations['after_preview_sha256']
            or observations['download_manifest_sha256']!=observations['after_manifest_sha256']):
        raise ValueError('RESTORE_V2_PROBE_DIGEST')


def _validate_purge_negative_observations(observations,replay,fixture_sha256):
    error='RESTORE_PURGE_PROBE_OBSERVATIONS'
    if set(observations)!={'fixture','baseline','completion','purge'}:
        raise ValueError(error)
    fixture=observations['fixture'];baseline=observations['baseline']
    completion=observations['completion'];purge=observations['purge']
    if any(not isinstance(item,dict) for item in (fixture,baseline,completion,purge)):
        raise ValueError(error)
    if (fixture.get('schema_version')!='restore-purge-fixture/v1'
            or fixture.get('restore_run_id')!=str(replay.run_id)
            or fixture.get('database')!=replay.database
            or fixture.get('build_sha256')!=replay.build_sha256
            or fixture.get('recovery_sha256')!=replay.recovery_sha256
            or fixture.get('replay_sha256')!=replay.replay_sha256
            or sha256(canonical_bytes(fixture)).hexdigest()!=fixture_sha256):
        raise ValueError('RESTORE_PURGE_PROBE_FIXTURE')
    try:
        tenant=UUID(fixture['tenant_id']);job=UUID(completion['deletion_job_id'])
    except (KeyError,ValueError,TypeError,AttributeError):
        raise ValueError(error) from None
    if (tenant.int==0 or job.int==0 or str(tenant)!=fixture['tenant_id']
            or str(job)!=completion['deletion_job_id']
            or type(fixture.get('revision')) is not int or fixture['revision']<1
            or type(completion.get('polls')) is not int or completion['polls']<1
            or completion.get('database')!=replay.database
            or completion.get('tenant_id')!=fixture['tenant_id']
            or baseline.get('schema_version')!='restore-purge-baseline/v1'
            or baseline.get('tenant_id')!=fixture['tenant_id']
            or purge.get('schema_version')!='restore-purge-observation/v1'):
        raise ValueError(error)
    buckets=fixture.get('buckets');refs=fixture.get('references')
    if (not isinstance(buckets,list) or not 1<=len(buckets)<=256
            or any(not isinstance(bucket,str) or not bucket for bucket in buckets)
            or buckets!=sorted(set(buckets)) or not isinstance(refs,list)
            or not 3<=len(refs)<=512 or any(not isinstance(ref,dict)
                or set(ref)!={'bucket','key','version','sha256','size'}
                or not isinstance(ref['bucket'],str) or ref['bucket'] not in buckets
                or not isinstance(ref['key'],str) or not ref['key']
                or not isinstance(ref['version'],str) or not ref['version']
                or not isinstance(ref['sha256'],str)
                or re.fullmatch('[0-9a-f]{64}',ref['sha256']) is None
                or type(ref['size']) is not int or ref['size']<0 for ref in refs)):
        raise ValueError(error)
    inventory=baseline.get('inventory')
    if not isinstance(inventory,dict):raise ValueError(error)
    identities=[(ref['bucket'],ref['key'],ref['version']) for ref in refs]
    prefixes=[part+'tenants/'+fixture['tenant_id']+'/' for part in ('','staging/','originals/')]
    if (identities!=sorted(set(identities))
            or {prefix for ref in refs for prefix in prefixes if ref['key'].startswith(prefix)}!=set(prefixes)):
        raise ValueError('RESTORE_PURGE_PROBE_BASELINE')
    digest=sha256(b'photoeditor.restore-object-inventory/v1\n')
    for ref in refs:
        digest.update(canonical_bytes(ref));digest.update(b'\n')
    if (inventory.get('schema_version')!='restore-object-inventory/v1'
            or inventory.get('objects')!=len(refs)
            or inventory.get('bytes_verified')!=sum(ref['size'] for ref in refs)
            or inventory.get('inventory_sha256')!=digest.hexdigest()):
        raise ValueError('RESTORE_PURGE_PROBE_BASELINE')
    metadata=purge.get('metadata');storage=purge.get('storage')
    api=purge.get('api_access');s3=purge.get('s3_access')
    before_api=baseline.get('api');before_s3=baseline.get('s3')
    if any(not isinstance(item,dict) for item in (metadata,storage,api,s3,before_api,before_s3)):
        raise ValueError(error)
    if (metadata.get('schema_version')!='restore-purge-metadata/v1'
            or metadata.get('database')!=replay.database
            or metadata.get('tenant_id')!=fixture['tenant_id']
            or metadata.get('deletion_job_id')!=completion['deletion_job_id']
            or not isinstance(metadata.get('checked_tables'),list)
            or any(not isinstance(table,str) for table in metadata['checked_tables'])
            or not {'artifacts','tasks','outbox','consent_grants'}<=set(metadata['checked_tables'])
            or storage.get('schema_version')!='restore-purge-storage/v1'
            or storage.get('tenant_id')!=fixture['tenant_id']
            or storage.get('buckets')!=buckets or storage.get('prefixes')!=prefixes
            or storage.get('passes')!=2):
        raise ValueError('RESTORE_PURGE_PROBE_ABSENCE')
    denial=fixture.get('api_denial')
    if (not isinstance(denial,list) or len(denial)!=2
            or type(denial[0]) is not int or not isinstance(denial[1],str)):
        raise ValueError('RESTORE_PURGE_PROBE_ACCESS')
    selected=[ref for ref in refs if (ref['bucket'],ref['key'],ref['version'])==
        (fixture.get('s3_bucket'),fixture.get('s3_key'),fixture.get('s3_version'))]
    if (len(selected)!=1 or before_api.get('response_sha256')!=fixture.get('api_sha256')
            or before_s3.get('tenant_id')!=fixture['tenant_id']
            or before_s3.get('sha256')!=selected[0]['sha256']
            or before_s3.get('size')!=selected[0]['size']
            or api.get('status')!=denial[0] or api.get('code')!=denial[1]
            or s3.get('tenant_id')!=fixture['tenant_id']
            or any(s3.get(k)!=fixture.get('s3_'+k) for k in ('bucket','key','version'))
            or s3.get('code') not in ('NoSuchKey','NoSuchVersion')):
        raise ValueError('RESTORE_PURGE_PROBE_ACCESS')


def _validate_revoke_negative_observations(observations,replay,fixture_sha256):
    error='RESTORE_REVOKE_PROBE_OBSERVATIONS'
    if set(observations)!={'fixture','running','admission'}:
        raise ValueError(error)
    fixture=observations['fixture'];running=observations['running'];admission=observations['admission']
    if (not isinstance(fixture,dict) or set(fixture)!={'schema_version','admission','running'}
            or fixture['schema_version']!='restore-combined-revoke-fixture/v1'
            or not isinstance(running,dict) or not isinstance(admission,dict)
            or sha256(canonical_bytes(fixture)).hexdigest()!=fixture_sha256):
        raise ValueError(error)
    pinned_admission=fixture['admission'];pinned_running=fixture['running']
    if not isinstance(pinned_admission,dict) or not isinstance(pinned_running,dict):
        raise ValueError(error)
    for descriptor,schema in ((pinned_admission,'restore-revoke-fixture/v1'),
                              (pinned_running,'restore-running-revoke-fixture/v1')):
        if (descriptor.get('schema_version')!=schema
                or descriptor.get('run_id')!=str(replay.run_id)
                or descriptor.get('database')!=replay.database
                or descriptor.get('build_sha256')!=replay.build_sha256
                or descriptor.get('recovery_sha256')!=replay.recovery_sha256
                or descriptor.get('replay_sha256')!=replay.replay_sha256):
            raise ValueError('RESTORE_REVOKE_PROBE_FIXTURE')
    af=pinned_admission.get('fixture');rf=pinned_running.get('fixture')
    if not isinstance(af,dict) or not isinstance(rf,dict):raise ValueError(error)
    try:
        aid=UUID(af['consent_id']);rid=UUID(rf['consent_id'])
        tenant=UUID(af['tenant_id']);project=UUID(af['project_id']);actor=UUID(af['actor_id'])
        child=UUID(pinned_running['child_run_id']);workflow=UUID(pinned_running['workflow_id'])
        task=UUID(pinned_running['task_id'])
    except (KeyError,ValueError,TypeError,AttributeError):
        raise ValueError(error) from None
    if (any(item.int==0 for item in (aid,rid,tenant,project,actor,child,workflow,task))
            or aid==rid or af['tenant_id']!=rf.get('tenant_id')
            or af['project_id']!=rf.get('project_id')
            or af['actor_id']!=rf.get('actor_id')
            or workflow!=uuid5(child,'workflow')
            or task!=uuid5(workflow,'render.compose:0')
            or pinned_running.get('barrier_required') is not True
            or type(pinned_running.get('maximum_seconds')) is not int
            or not 1<=pinned_running['maximum_seconds']<=300):
        raise ValueError('RESTORE_REVOKE_PROBE_IDENTITY')
    def consent(value,source,revoked):
        return (isinstance(value,dict)
            and value.get('schema_version')=='restore-revoke-consent/v1'
            and value.get('database')==replay.database
            and value.get('tenant_id')==source['tenant_id']
            and value.get('consent_id')==source['consent_id']
            and value.get('revoked') is revoked
            and type(value.get('epoch')) is int
            and value['epoch']==source['epoch']+int(revoked)
            and type(value.get('revision')) is int
            and value['revision']==source['revision']+int(revoked)
            and type(value.get('affected_versions')) is int
            and value['affected_versions']>=1)
    if (any(type(source.get(k)) is not int or source[k]<1
            for source in (af,rf) for k in ('epoch','revision'))
            or not consent(running.get('committed_consent'),rf,True)
            or not consent(admission.get('final_consent'),af,True)):
        raise ValueError('RESTORE_REVOKE_PROBE_CONSENT')
    witness=running.get('running');cancelled=running.get('cancelled')
    receipt=running.get('receipt')
    if (not isinstance(witness,dict) or not isinstance(cancelled,dict)
            or not isinstance(receipt,dict)
            or witness.get('database')!=replay.database
            or witness.get('tenant_id')!=rf['tenant_id']
            or witness.get('child_run_id')!=pinned_running['child_run_id']
            or witness.get('workflow_id')!=pinned_running['workflow_id']
            or witness.get('task_id')!=pinned_running['task_id']
            or any(type(witness.get(k)) is not int or witness[k]<1 for k in ('attempt','fence'))
            or cancelled.get('running')!=witness
            or cancelled.get('state')!='cancelled'
            or cancelled.get('result_absent') is not True
            or running.get('barrier_cancelled') is not True
            or type(running.get('polls')) is not int or running['polls']<1
            or receipt.get('consent_id')!=rf['consent_id']
            or receipt.get('epoch')!=rf['epoch']+1
            or receipt.get('revision')!=rf['revision']+1):
        raise ValueError('RESTORE_REVOKE_PROBE_RUNNING')
    revoked=admission.get('revoke')
    if (not isinstance(revoked,dict)
            or not consent(revoked.get('baseline_consent'),af,False)
            or not consent(revoked.get('committed_consent'),af,True)
            or revoked.get('final_consent')!=revoked['committed_consent']
            or admission['final_consent']!=revoked['committed_consent']
            or not isinstance(revoked.get('revoke_receipt'),dict)
            or revoked['revoke_receipt'].get('consent_id')!=af['consent_id']
            or revoked['revoke_receipt'].get('epoch')!=af['epoch']+1
            or revoked['revoke_receipt'].get('revision')!=af['revision']+1
            or admission.get('schema_version')!='restore-revoke-freeze-lifecycle/v1'):
        raise ValueError('RESTORE_REVOKE_PROBE_ADMISSION')
    denied_freeze=admission.get('denied_freeze');negative_batch=admission.get('negative_batch')
    positive_batch=admission.get('positive_batch')
    if (not isinstance(denied_freeze,list) or len(denied_freeze)!=3
            or denied_freeze[0]!=denied_freeze[2]
            or not isinstance(denied_freeze[0],list) or len(denied_freeze[0])!=2
            or not consent(denied_freeze[0][0],af,True)
            or not isinstance(denied_freeze[1],dict)
            or denied_freeze[1].get('schema_version')!='restore-freeze-denial/v1'
            or not isinstance(negative_batch,list) or len(negative_batch)!=3
            or negative_batch[0]!=negative_batch[2]
            or not isinstance(negative_batch[0],list) or len(negative_batch[0])!=2
            or not consent(negative_batch[0][0],af,True)
            or not isinstance(negative_batch[1],dict)
            or negative_batch[1].get('schema_version')!='restore-render-batch-refusal/v1'
            or not isinstance(positive_batch,dict)
            or positive_batch.get('schema_version')!='restore-completed-batch-baseline/v1'
            or type(positive_batch.get('polls')) is not int or positive_batch['polls']<1):
        raise ValueError('RESTORE_REVOKE_PROBE_PROCESSING')

    binding=pinned_admission.get('binding');frozen=admission.get('baseline_freeze')
    physical=admission.get('physical_freeze')
    if (not isinstance(binding,dict) or not isinstance(frozen,dict)
            or not isinstance(physical,dict)
            or frozen.get('schema_version')!='restore-freeze-baseline/v1'
            or physical.get('schema_version')!='restore-frozen-result/v1'
            or physical.get('database')!=replay.database
            or physical.get('result')!=frozen
            or not isinstance(physical.get('object'),dict)
            or physical['object'].get('sha256')!=frozen.get('recipe_sha256')
            or physical.get('authored_sha256')!=pinned_admission.get('document_sha256')
            or binding.get('actor_id')!=af['actor_id']
            or binding.get('source_sha256')!=pinned_admission.get('source_sha256')
            or not isinstance(binding.get('asset_version_id'),str)
            or not isinstance(binding.get('source_artifact_id'),str)
            or not isinstance(binding.get('canvas_hash'),str)
            or frozen.get('recipe_artifact_id')==binding['source_artifact_id']):
        raise ValueError('RESTORE_REVOKE_PROBE_FREEZE')
    asset=pinned_admission.get('asset_id');document=pinned_admission.get('document_id')
    revision=pinned_admission.get('revision')
    freeze_request=sha256(canonical_bytes({'document_id':document,
        'expected_revision':str(revision),'assume_profile':None})).hexdigest()
    freeze_catalogue=denied_freeze[0][1]
    denied=denied_freeze[1]
    if (type(revision) is not int or revision<1
            or not isinstance(freeze_catalogue,dict)
            or freeze_catalogue.get('schema_version')!='restore-freeze-catalogue/v1'
            or freeze_catalogue.get('database')!=replay.database
            or freeze_catalogue.get('tenant_id')!=af['tenant_id']
            or freeze_catalogue.get('document_id')!=document
            or freeze_catalogue.get('asset_version_id')!=binding['asset_version_id']
            or freeze_catalogue.get('revision')!=revision
            or frozen.get('request_sha256')!=freeze_request
            or denied.get('tenant_id')!=af['tenant_id']
            or denied.get('asset_id')!=asset
            or denied.get('document_id')!=document
            or denied.get('revision')!=revision
            or denied.get('request_sha256')!=freeze_request):
        raise ValueError('RESTORE_REVOKE_PROBE_FREEZE')
    before_access=revoked.get('baseline_access');after_access=revoked.get('denied_access')
    if (revoked.get('schema_version')!='restore-revoke-observation/v1'
            or not isinstance(before_access,dict) or not isinstance(after_access,dict)
            or before_access.get('schema_version')!='restore-access-readable/v1'
            or before_access.get('response_sha256')!=pinned_admission.get('document_sha256')
            or after_access.get('schema_version')!='restore-access-denial/v1'
            or [after_access.get('status'),after_access.get('code')]!=pinned_admission.get('access_denial')):
        raise ValueError('RESTORE_REVOKE_PROBE_ACCESS')
    catalogue=negative_batch[0][1];refusal=negative_batch[1]
    ticket=positive_batch.get('ticket');adoption=positive_batch.get('adoption')
    positive_physical=positive_batch.get('physical')
    try:
        adopted_child=UUID(adoption['child_run_id']) if isinstance(adoption,dict) else None
        adopted_workflow=UUID(adoption['workflow_id']) if isinstance(adoption,dict) else None
    except (KeyError,ValueError,TypeError,AttributeError):
        raise ValueError('RESTORE_REVOKE_PROBE_BATCH') from None
    if (not isinstance(catalogue,dict) or not isinstance(ticket,dict)
            or not isinstance(adoption,dict) or not isinstance(positive_physical,dict)
            or adopted_child is None or adopted_child.int==0
            or adopted_workflow is None or adopted_workflow.int==0
            or catalogue.get('schema_version')!='restore-batch-catalogue/v1'
            or catalogue.get('database')!=replay.database
            or catalogue.get('snapshot_id')!=pinned_admission.get('snapshot_id')
            or adoption.get('schema_version')!='restore-batch-adoption/v1'
            or adoption.get('database')!=replay.database
            or ticket.get('schema_version')!='restore-render-batch-baseline/v1'
            or ticket.get('batch_id')!=adoption.get('batch_id')
            or ticket.get('child_run_id')!=adoption.get('child_run_id')
            or catalogue.get('batch_id')==adoption.get('batch_id')
            or adoption.get('workflow_id')!=str(uuid5(adopted_child,'workflow'))
            or positive_physical.get('schema_version')!='restore-batch-physical/v1'):
        raise ValueError('RESTORE_REVOKE_PROBE_BATCH')
    request_body={'input_snapshot_id':pinned_admission['snapshot_id'],
        'policy_sha':pinned_admission['policy_sha256'],
        'selection':[{'source':{'asset_version_id':binding['asset_version_id'],
            'source':{'artifact_id':binding['source_artifact_id'],
                      'sha256':binding['source_sha256']},
            'canvas_hash':binding['canvas_hash']},
            'recipe':{'artifact_id':frozen['recipe_artifact_id'],
                      'sha256':frozen['recipe_sha256']}}]}
    batch_request=sha256(canonical_bytes(request_body)).hexdigest()
    if (ticket.get('request_sha256')!=batch_request
            or refusal.get('request_sha256')!=batch_request
            or refusal.get('code')!='PROFILE_CONSENT_REVOKED'):
        raise ValueError('RESTORE_REVOKE_PROBE_BATCH')

    physical_result=positive_physical.get('result')
    image_result=positive_physical.get('image_result')
    working_image=positive_physical.get('working_image')
    children=positive_physical.get('children')
    inventory=positive_physical.get('inventory')
    semantics=positive_physical.get('semantics')
    if (not isinstance(physical_result,dict) or not isinstance(image_result,dict)
            or not isinstance(working_image,dict) or not isinstance(children,list)
            or not isinstance(inventory,dict) or not isinstance(semantics,dict)
            or physical_result.get('schema_version')!='restore-batch-result/v1'
            or physical_result.get('database')!=replay.database
            or physical_result.get('task_id')!=str(uuid5(adopted_workflow,'render.compose:0'))
            or not isinstance(physical_result.get('object'),dict)
            or physical_result['object'].get('sha256')!=sha256(canonical_bytes(image_result)).hexdigest()
            or physical_result['object'].get('size')!=len(canonical_bytes(image_result))
            or not isinstance(physical_result['object'].get('key'),str)
            or not physical_result['object']['key'].startswith('tenants/'+af['tenant_id']+'/')
            or not isinstance(physical_result['object'].get('bucket'),str)
            or not isinstance(physical_result['object'].get('version'),str)
            or physical_result['object']['version'] in ('','null')
            or type(physical_result.get('attempt')) is not int or physical_result['attempt']<1
            or type(physical_result.get('fence')) is not int or physical_result['fence']<1
            or image_result.get('schema_version')!=1
            or not 3<=len(children)<=4
            or (image_result.get('generated_mask') is None)!=(len(children)==3)
            or semantics.get('schema_version')!='restore-render-semantic/v1'
            or semantics.get('status') not in ('pass','review')
            or semantics.get('canvas_sha256')!=binding['canvas_hash']
            or not isinstance(positive_physical.get('arrays'),list)
            or not positive_physical['arrays']
            or positive_physical.get('parents') is None):
        raise ValueError('RESTORE_REVOKE_PROBE_PHYSICAL')
    refs=[image_result.get(k) for k in ('working_image','report','changed_mask')]
    if len(children)==4:refs.append(image_result.get('generated_mask'))
    if (any(not isinstance(ref,dict) or not isinstance(child,dict)
            or ref.get('sha256')!=child.get('sha256')
            or not isinstance(ref.get('artifact_id'),str)
            or not isinstance(ref.get('sha256'),str)
            or re.fullmatch('[0-9a-f]{64}',ref['sha256']) is None
            or ref.get('artifact_id')==physical_result.get('artifact_id')
            or not isinstance(child.get('bucket'),str)
            or not isinstance(child.get('key'),str)
            or not child['key'].startswith('tenants/'+af['tenant_id']+'/')
            or not isinstance(child.get('version'),str)
            or child['version'] in ('','null')
            or type(child.get('size')) is not int or not 2<=child['size']<=1048576
            for ref,child in zip(refs,children))
            or len({ref['artifact_id'] for ref in refs})!=len(refs)
            or working_image.get('dtype')!='float32'
            or not isinstance(working_image.get('pixels'),dict)
            or image_result['working_image']['sha256']!=sha256(canonical_bytes(working_image)).hexdigest()
            or semantics.get('pixel_artifact_id')!=working_image['pixels'].get('artifact_id')):
        raise ValueError('RESTORE_REVOKE_PROBE_PHYSICAL')
    try:
        result_ids=[UUID(physical_result[key]) for key in ('result_id','artifact_id')]
        child_ids=[UUID(ref['artifact_id']) for ref in refs]
    except (KeyError,ValueError,TypeError,AttributeError):
        raise ValueError('RESTORE_REVOKE_PROBE_PHYSICAL') from None
    if any(item.int==0 for item in result_ids+child_ids):
        raise ValueError('RESTORE_REVOKE_PROBE_PHYSICAL')
    ordered=sorted(children,key=lambda row:(row['bucket'].encode(),row['key'].encode(),row['version'].encode()))
    identities=[(row['bucket'],row['key'],row['version']) for row in ordered]
    digest=sha256(b'photoeditor.restore-object-inventory/v1\n')
    for row in ordered:
        digest.update(canonical_bytes(row));digest.update(b'\n')
    if (len(set(identities))!=len(children)
            or inventory.get('schema_version')!='restore-object-inventory/v1'
            or inventory.get('objects')!=len(children)
            or inventory.get('bytes_verified')!=sum(row['size'] for row in children)
            or inventory.get('inventory_sha256')!=digest.hexdigest()):
        raise ValueError('RESTORE_REVOKE_PROBE_PHYSICAL')

def _validate_legacy_apply_observations(observations,replay,fixture_sha256):
    required={'fixture','dynamic_selection_export','dynamic_selection_export_sha256',
        'apply_run_id','apply_output_collection_id','apply_document_artifact_id',
        'selection_snapshot_id','selection_digest','encoded_artifact_id','encoded_sha256',
        'volume_artifact_id','volume_sha256','download_sha256','delivery_kind',
        'archive_sha256','preview_sha256','apply_document_sha256',
        'preview_polls','apply_polls','export_polls'}
    if (set(observations)!=required or not isinstance(observations['fixture'],dict)
            or not isinstance(observations['dynamic_selection_export'],dict)):
        raise ValueError('RESTORE_LEGACY_PROBE_OBSERVATIONS')
    fixture=observations['fixture'];dynamic=observations['dynamic_selection_export']
    outer={'schema_version','legacy','preset_id','preset_revision','preset_sha256',
        'selection_key_sha256','export_key_sha256','intent_key_sha256',
        'encode_params_template','encode_params_template_sha256','scratch_root',
        'buckets','export_maximum_seconds','maximum_seconds','pepper_sha256'}
    if (set(fixture)!=outer or fixture['schema_version']!='restore-legacy-complete-fixture/v1'
            or sha256(canonical_bytes(fixture)).hexdigest()!=fixture_sha256
            or not isinstance(fixture['legacy'],dict)
            or set(fixture['legacy'])!={'schema_version','preview','apply'}
            or fixture['legacy']['schema_version']!='restore-legacy-fixture/v1'
            or not isinstance(fixture['encode_params_template'],dict)
            or sha256(canonical_bytes(fixture['encode_params_template'])).hexdigest()!=fixture['encode_params_template_sha256']
            or not isinstance(fixture['buckets'],list) or not fixture['buckets']
            or fixture['buckets']!=sorted(set(fixture['buckets']))
            or type(fixture['maximum_seconds']) is not int or not 1<=fixture['maximum_seconds']<=1200
            or type(fixture['export_maximum_seconds']) is not int
            or not 1<=fixture['export_maximum_seconds']<=300):
        raise ValueError('RESTORE_LEGACY_PROBE_FIXTURE')
    for side in ('preview','apply'):
        item=fixture['legacy'][side]
        if (not isinstance(item,dict) or item.get('restore_run_id')!=str(replay.run_id)
                or item.get('database')!=replay.database
                or item.get('build_sha256')!=replay.build_sha256
                or item.get('recovery_sha256')!=replay.recovery_sha256
                or item.get('replay_sha256')!=replay.replay_sha256):
            raise ValueError('RESTORE_LEGACY_PROBE_REPLAY')
    try:
        ids={name:UUID(observations[name]) for name in ('apply_run_id',
            'apply_output_collection_id','apply_document_artifact_id',
            'selection_snapshot_id','encoded_artifact_id','volume_artifact_id')}
        preset=UUID(fixture['preset_id'])
    except (ValueError,TypeError,AttributeError,KeyError):
        raise ValueError('RESTORE_LEGACY_PROBE_IDENTITY') from None
    if (preset.int==0 or any(item.int==0 for item in ids.values())
            or type(fixture['preset_revision']) is not int or fixture['preset_revision']<1
            or any(type(observations[name]) is not int or observations[name]<1
                   for name in ('preview_polls','apply_polls','export_polls'))):
        raise ValueError('RESTORE_LEGACY_PROBE_IDENTITY')
    hashes=('dynamic_selection_export_sha256','selection_digest','encoded_sha256',
        'volume_sha256','download_sha256','preview_sha256','apply_document_sha256')
    pins=('preset_sha256','selection_key_sha256','export_key_sha256',
          'intent_key_sha256','encode_params_template_sha256','pepper_sha256')
    if (any(not isinstance(observations[name],str)
            or re.fullmatch('[0-9a-f]{64}',observations[name]) is None for name in hashes)
            or any(not isinstance(fixture[name],str)
                   or re.fullmatch('[0-9a-f]{64}',fixture[name]) is None for name in pins)
            or sha256(canonical_bytes(dynamic)).hexdigest()!=observations['dynamic_selection_export_sha256']
            or observations['volume_sha256']!=observations['download_sha256']):
        raise ValueError('RESTORE_LEGACY_PROBE_DIGEST')
    kind=observations['delivery_kind'];archive=observations['archive_sha256']
    if (kind=='single' and (archive is not None
            or ids['encoded_artifact_id']!=ids['volume_artifact_id']
            or observations['encoded_sha256']!=observations['volume_sha256'])):
        raise ValueError('RESTORE_LEGACY_PROBE_DELIVERY')
    if (kind=='zip' and (not isinstance(archive,str)
            or archive!=observations['volume_sha256']
            or ids['encoded_artifact_id']==ids['volume_artifact_id'])):
        raise ValueError('RESTORE_LEGACY_PROBE_DELIVERY')
    if kind not in ('single','zip'):
        raise ValueError('RESTORE_LEGACY_PROBE_DELIVERY')
    selection=dynamic.get('selection');ticket=dynamic.get('apply_ticket')
    template=fixture['encode_params_template']
    params={'image':{'artifact_id':observations['apply_document_artifact_id'],
                     'sha256':observations['apply_document_sha256']},**template}
    if (dynamic.get('schema_version')!='restore-selection-export-fixture/v1'
            or dynamic.get('apply_fixture')!=fixture['legacy']['apply']
            or dynamic.get('preset_id')!=fixture['preset_id']
            or dynamic.get('request_key_sha256')!=fixture['export_key_sha256']
            or dynamic.get('intent_key_sha256')!=fixture['intent_key_sha256']
            or dynamic.get('selection_key_sha256')!=fixture['selection_key_sha256']
            or dynamic.get('scratch_root')!=fixture['scratch_root']
            or dynamic.get('encode_params_sha256')!=sha256(canonical_bytes(params)).hexdigest()
            or not isinstance(selection,dict)
            or selection.get('snapshot_digest')!=observations['selection_digest']
            or selection.get('preset_revision')!=fixture['preset_revision']
            or selection.get('preset_sha256')!=fixture['preset_sha256']
            or not isinstance(ticket,dict)
            or ticket.get('run_id')!=observations['apply_run_id']
            or ticket.get('output_collection_id')!=observations['apply_output_collection_id']
            or dynamic.get('selection_collection_id')!=observations['apply_output_collection_id']):
        raise ValueError('RESTORE_LEGACY_PROBE_CHAIN')


@dataclass(frozen=True)
class RestoreAcceptanceEvidence:
    schema_version: str
    replay: VerifiedRestoreReplay
    probes: tuple[RestoreProbeEvidence, ...]
    finished_at: datetime
    writers_authorized: bool = False
    probe_receipts: tuple[RestoreProbeReceipt, ...] = ()


def _require_restore_guard(callback, error_code):
    if callback() is False:
        raise ValueError(error_code)


def run_restore_acceptance(recovery_path, recovery_sha256, replay_path, replay_sha256, *,
                           evidence, build_sha256, fixture_pins, run_probe,
                           assert_probe_isolation, verify_current_authorities, check_active):
    """Verify actual pinned files, run every probe, then reverify files/authority.

    The phase guard must attest isolated fixture-only writes and blocked user
    access/outgoing; ordinary production services must remain fenced. The current
    authority verifier must reconcile independent deletion/consent/broker history
    and keys, not merely return a boolean declaration. No cutover occurs here.
    """
    kinds=tuple(kind for kind,_ in PROBE_CHECKS)
    if (not isinstance(fixture_pins,dict) or set(fixture_pins)!=set(kinds)
            or any(not isinstance(pin,str) or re.fullmatch('[0-9a-f]{64}',pin) is None
                   for pin in fixture_pins.values())
            or not all(callable(f) for f in (run_probe,assert_probe_isolation,verify_current_authorities,check_active))):
        raise ValueError('RESTORE_ACCEPTANCE_INSTALLATION')
    pins=dict(fixture_pins)
    def guard():
        _require_restore_guard(check_active,'RESTORE_ACCEPTANCE_INACTIVE')
        _require_restore_guard(assert_probe_isolation,'RESTORE_ACCEPTANCE_NOT_ISOLATED')
        _require_restore_guard(check_active,'RESTORE_ACCEPTANCE_INACTIVE')
        _require_restore_guard(verify_current_authorities,'RESTORE_ACCEPTANCE_AUTHORITY')
        _require_restore_guard(check_active,'RESTORE_ACCEPTANCE_INACTIVE')
    def read():
        return verify_restore_replay_files(recovery_path,recovery_sha256,replay_path,replay_sha256,
            evidence=evidence,build_sha256=build_sha256,check_active=guard)
    guard();replay=read();previous=replay.completed_at;results=[];receipts=[]
    for kind,checks in PROBE_CHECKS:
        guard();invoked=datetime.now(timezone.utc)
        receipt=run_probe(kind,replay,pins[kind],guard)
        result=read_restore_probe_report(receipt,replay,check_active=guard)
        guard();observed=datetime.now(timezone.utc)
        if (not isinstance(result,RestoreProbeEvidence) or result.kind!=kind
                or result.run_id!=replay.run_id or result.database!=replay.database
                or result.build_sha256!=replay.build_sha256 or result.fixture_sha256!=pins[kind]
                or not isinstance(result.evidence_sha256,str)
                or re.fullmatch('[0-9a-f]{64}',result.evidence_sha256) is None
                or not isinstance(result.checks,tuple) or len(result.checks)!=len(checks)
                or any(not isinstance(pair,tuple) or len(pair)!=2 or pair[0]!=expected or pair[1] is not True
                       for pair,expected in zip(result.checks,checks))):
            raise ValueError('RESTORE_ACCEPTANCE_PROBE')
        if (not isinstance(result.started_at,datetime) or result.started_at.tzinfo is None
                or not isinstance(result.finished_at,datetime) or result.finished_at.tzinfo is None
                or not previous<=invoked<=result.started_at<=result.finished_at<=observed):
            raise ValueError('RESTORE_ACCEPTANCE_TIME')
        results.append(result);receipts.append(receipt);previous=result.finished_at
    if read()!=replay:raise ValueError('RESTORE_ACCEPTANCE_REPLAY_CHANGED')
    for receipt,result in zip(receipts,results):
        if read_restore_probe_report(receipt,replay,check_active=guard)!=result:
            raise ValueError('RESTORE_ACCEPTANCE_PROBE_CHANGED')
    guard()
    return RestoreAcceptanceEvidence('restore-acceptance/v1',replay,tuple(results),datetime.now(timezone.utc),probe_receipts=tuple(receipts))


def run_restore_acceptance_and_report(root, recovery_path, recovery_sha256, replay_path, replay_sha256, **installation):
    from .restore_acceptance_report import persist_restore_acceptance
    result=run_restore_acceptance(recovery_path,recovery_sha256,replay_path,replay_sha256,**installation)
    receipt=persist_restore_acceptance(root,result,recovery_path,replay_path,
        recovery_evidence=installation['evidence'],check_active=installation['check_active'],
        assert_probe_isolation=installation['assert_probe_isolation'],
        verify_current_authorities=installation['verify_current_authorities'])
    return result,receipt
