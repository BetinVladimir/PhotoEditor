"""Completed canonical person origin plus original typed PersonParams replay."""
from copy import deepcopy
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.person import PersonParams
from .adoption import AdoptionInvalid,ArtifactRef
from .relight_adoption import _document
from .relight_adoption_source import load_relight_adoption_source,relight_adoption_source_checkpoint


def verify_relight_adoption_person(connection,reader,tenant_id,task_id,context,params,person,*,check_active):
    context=deepcopy(context)
    body=canonical_bytes(params)
    source=load_relight_adoption_source(connection,tenant_id,task_id,context,body,check_active=check_active)
    checkpoint=relight_adoption_source_checkpoint(connection,tenant_id,task_id,context,body,source,check_active=check_active)
    person_raw=canonical_bytes(person)
    if canonical_hash(person)!=params.subject.sha256:raise AdoptionInvalid('RELIGHT_ADOPTION_PERSON_MISMATCH')
    def origin():
        checkpoint();check_active()
        row=connection.execute('SELECT photo.v28_relight_adoption_person_origin(%s,%s,%s,%s,%s,%s,%s)',
            (tenant_id,task_id,context.attempt,context.fence,context.result_id,body.decode(),person_raw.decode())).fetchone()
        check_active()
        if row is None or row[0] is None:raise AdoptionInvalid('RELIGHT_ADOPTION_PERSON_STALE')
        value=row[0]
        if (not isinstance(value,dict) or set(value)!={'artifact_id','sha256','bucket','object_key','object_version','bytes','media_type'}
                or type(value['bytes']) is not int or not 2<=value['bytes']<=65536 or value['media_type']!='application/json'
                or any(not isinstance(value[key],str) or not value[key] for key in ('bucket','object_key','object_version'))
                or value['object_version']=='null'):
            raise AdoptionInvalid('RELIGHT_ADOPTION_PERSON_INVALID')
        return value
    record=origin();ref=ArtifactRef.parse({key:record[key] for key in ('artifact_id','sha256')})
    evidence=reader.evidence(ref)
    if evidence.bytes!=record['bytes']:raise AdoptionInvalid('RELIGHT_ADOPTION_PERSON_INVALID')
    original=_document(reader,ref,PersonParams,'input')
    if original.image!=params.base.source:raise AdoptionInvalid('RELIGHT_ADOPTION_PERSON_SOURCE_MISMATCH')
    if canonical_bytes(origin())!=canonical_bytes(record):raise AdoptionInvalid('RELIGHT_ADOPTION_PERSON_CHANGED')
    checkpoint();return True
