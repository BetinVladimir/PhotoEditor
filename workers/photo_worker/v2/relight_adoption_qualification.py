"""Completed scheduler catalog plus pinned qualification byte verification."""
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from .adoption import AdoptionInvalid, ArtifactRef
from .relight_qualification import verify_relight_qualification


def verify_completed_relight_qualification(connection, reader, tenant_id, task_id, context,
                                            params, calibration, *, expected_runtime_sha,
                                            check_active):
    if connection.autocommit is not True or not callable(check_active):
        raise ValueError('RELIGHT_ADOPTION_QUALIFICATION_CONFIGURATION')
    context = deepcopy(context)
    body = canonical_bytes(params)
    def catalog():
        check_active()
        row = connection.execute('SELECT photo.v28_relight_adoption_qualification(%s,%s,%s,%s,%s,%s)',
            (tenant_id,task_id,context.attempt,context.fence,context.result_id,body.decode())).fetchone()
        check_active()
        if row is None or row[0] is None:
            raise AdoptionInvalid('RELIGHT_ADOPTION_QUALIFICATION_STALE')
        value = row[0]
        if not isinstance(value,dict) or set(value) != {'pin','policy','report','calibration'}:
            raise AdoptionInvalid('RELIGHT_ADOPTION_QUALIFICATION_INVALID')
        return deepcopy(value)
    record = catalog(); bodies = []
    for label,maximum in (('report',8<<20),('calibration',1<<20)):
        location = record[label]
        if (not isinstance(location,dict)
                or set(location) != {'artifact_id','sha256','bucket','object_key','object_version','bytes','media_type'}
                or type(location['bytes']) is not int or not 2 <= location['bytes'] <= maximum
                or location['media_type'] != 'application/json'
                or any(not isinstance(location[key],str) or not location[key] for key in ('bucket','object_key','object_version'))
                or location['object_version'] == 'null'):
            raise AdoptionInvalid('RELIGHT_ADOPTION_QUALIFICATION_OBJECT_INVALID')
        ref = ArtifactRef.parse({key:location[key] for key in ('artifact_id','sha256')})
        evidence = reader.evidence(ref)
        if evidence.ref != ref or evidence.source != 'input' or evidence.bytes != location['bytes'] or evidence.media_type != 'application/json':
            raise AdoptionInvalid('RELIGHT_ADOPTION_QUALIFICATION_OBJECT_INVALID')
        check_active();raw = reader.read_json(ref,maximum);check_active()
        if not isinstance(raw,bytes) or len(raw) != location['bytes'] or sha256(raw).hexdigest() != ref.sha256:
            raise AdoptionInvalid('RELIGHT_ADOPTION_QUALIFICATION_BYTES_INVALID')
        bodies.append(raw)
    _,verified = verify_relight_qualification(record['pin'],*bodies,record['policy'],params=params,
        expected_runtime_sha=expected_runtime_sha,check_active=check_active)
    if canonical_bytes(verified) != canonical_bytes(calibration):
        raise AdoptionInvalid('RELIGHT_ADOPTION_CALIBRATION_CHANGED')
    if canonical_bytes(catalog()) != canonical_bytes(record):
        raise AdoptionInvalid('RELIGHT_ADOPTION_QUALIFICATION_CHANGED')
    return True
