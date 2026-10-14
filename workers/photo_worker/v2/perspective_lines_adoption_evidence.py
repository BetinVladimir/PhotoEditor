"""Current scheduler snapshots and equality checkpoints around detector IO."""
from copy import deepcopy
from hashlib import sha256
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes, canonical_hash, parse_json
from photo_worker.contracts.perspective_line_detection import PerspectiveLineDetectionParams
from photo_worker.operations.optical.perspective_lines_installation import execution_policy
from .adoption import AdoptionInvalid, ArtifactRef, require_adoption_active


def load_perspective_lines_adoption_evidence(connection, tenant_id, task_id, context, params_raw, *, check_active):
    if connection.autocommit is not True or not callable(check_active):
        raise ValueError('PERSPECTIVE_LINES_ADOPTION_READ_CONFIGURATION')
    supplied_check_active=check_active
    def check_active():
        require_adoption_active(supplied_check_active)
    check_active()
    if not isinstance(params_raw, bytes) or not 2 <= len(params_raw) <= 65536:
        raise AdoptionInvalid('PERSPECTIVE_LINES_PARAMS_INVALID')
    params = PerspectiveLineDetectionParams.model_validate_json(params_raw)
    if canonical_bytes(params) != params_raw:
        raise AdoptionInvalid('PERSPECTIVE_LINES_PARAMS_INVALID')
    ctx = deepcopy(context)
    check_active()
    row = connection.execute('SELECT photo.v29_perspective_lines_adoption_evidence(%s,%s,%s,%s,%s,%s)',
        (tenant_id, task_id, ctx.attempt, ctx.fence, ctx.result_id, params_raw.decode('utf-8'))).fetchone()
    check_active()
    if row is None or row[0] is None:
        raise AdoptionInvalid('PERSPECTIVE_LINES_ADOPTION_STALE')
    value = validate_perspective_lines_adoption_evidence(row[0],task_id,ctx,params_raw)
    check_active()
    return value


def validate_perspective_lines_adoption_evidence(raw, task_id, context, params_raw):
    """Validate the closed evidence envelope; this grants no SQL authority."""
    ctx = deepcopy(context)
    params = PerspectiveLineDetectionParams.model_validate_json(params_raw)
    if canonical_bytes(params) != params_raw:
        raise AdoptionInvalid('PERSPECTIVE_LINES_PARAMS_INVALID')
    value = parse_json(canonical_bytes(raw),262144)
    try:
        if (not isinstance(value, dict) or set(value) != {'schema_version','run_id','task_id','attempt','fence',
                'result_id','params_ref','root','params','profile','policy_jcs','source','arrays'}
                or type(value['schema_version']) is not int or value['schema_version'] != 1
                or UUID(value['run_id']).int == 0 or value['task_id'] != str(task_id)
                or value['result_id'] != str(ctx.result_id) or type(value['attempt']) is not int
                or value['attempt'] != ctx.attempt or not 1 <= value['attempt'] <= 6
                or type(value['fence']) is not int or value['fence'] != ctx.fence
                or not 1 <= value['fence'] <= 2**53-1):
            raise ValueError()
        ref = ArtifactRef.parse(value['params_ref'])
        if (ref.artifact_id.int == 0 or ref.sha256 != sha256(params_raw).hexdigest()
                or canonical_bytes(value['params']) != params_raw or ArtifactRef.parse(value['root']) != ctx.root):
            raise ValueError()
        policy_raw = value['policy_jcs']
        if not isinstance(policy_raw, str) or not 2 <= len(policy_raw.encode()) <= 65536:
            raise ValueError()
        policy = parse_json(policy_raw.encode(),65536)
        expected = execution_policy(policy['working_icc_sha'],policy['runtime_lock_sha'])
        if canonical_bytes(expected).decode() != policy_raw:
            raise ValueError()
        profile = value['profile']
        if (profile.get('backend') != 'cpu' or profile.get('precision') != 'float32'
                or profile.get('renderer_version') != 'v2.2'
                or profile.get('runtime_lock_sha') != policy['runtime_lock_sha']
                or profile.get('execution_policy_sha') != canonical_hash(expected)):
            raise ValueError()
        source = value['source']
        if (source['artifact_id'] != str(params.base.source.artifact_id)
                or source['sha256'] != params.base.source.sha256 or source['media_type'] != 'application/json'
                or type(source['bytes']) is not int or not 2 <= source['bytes'] <= 1048576
                or any(not isinstance(source[k],str) or not 1 <= len(source[k]) <= 1024
                    for k in ('bucket','object_key','object_version')) or source['object_version'] == 'null'):
            raise ValueError()
        validate_array_locations(value['arrays'],source)
    except (ValueError, TypeError, KeyError, AttributeError) as error:
        raise AdoptionInvalid('PERSPECTIVE_LINES_ADOPTION_INVALID') from error
    return value


def perspective_lines_adoption_checkpoint(connection,tenant_id,task_id,context,params_raw,evidence,*,check_active):
    ctx = deepcopy(context)
    expected = canonical_bytes(evidence)
    def checkpoint():
        current = load_perspective_lines_adoption_evidence(connection,tenant_id,task_id,ctx,params_raw,check_active=check_active)
        if canonical_bytes(current) != expected:
            raise AdoptionInvalid('PERSPECTIVE_LINES_ADOPTION_CHANGED')
        return current
    return checkpoint


def validate_array_locations(arrays, source):
    if not isinstance(arrays, list) or len(arrays) not in (1,2):
        raise ValueError('invalid source arrays')
    seen = {source['artifact_id']}
    for item, role in zip(arrays, ('WorkingPixels','WorkingAlpha')):
        if not isinstance(item,dict) or set(item) != {'role','object'} or item['role'] != role:
            raise ValueError('invalid array role')
        location = item['object']
        if not isinstance(location,dict) or set(location) != {'artifact_id','sha256','bucket','object_key','object_version','bytes','media_type'}:
            raise ValueError('invalid array location')
        ref = ArtifactRef.parse({'artifact_id':location['artifact_id'],'sha256':location['sha256']})
        if (ref.artifact_id.int == 0 or location['artifact_id'] in seen
                or location['bucket'] != source['bucket']
                or location['media_type'] not in ('application/x-npy','application/octet-stream')
                or type(location['bytes']) is not int or not 1 <= location['bytes'] <= 2**53-1
                or any(not isinstance(location[k],str) or not 1 <= len(location[k]) <= 1024
                    for k in ('object_key','object_version')) or location['object_version'] == 'null'):
            raise ValueError('invalid array location')
        seen.add(location['artifact_id'])
