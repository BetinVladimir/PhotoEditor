"""Scheduler-only current qualification locators; no object IO in transaction."""
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes,parse_json
from .negative_qualification import NegativeQualificationPin
from .adoption import AdoptionInvalid


def resolve_negative_qualification_objects(connection,tenant,project,actor,session,pin_value,*,check_active):
    if (getattr(connection,'autocommit',None) is not True or not callable(check_active)
            or any(not isinstance(v,UUID) or v.int==0 for v in (tenant,project,actor,session))):
        raise ValueError('NEGATIVE_CATALOG_CONFIGURATION')
    pin=NegativeQualificationPin.from_value(pin_value)
    owned=parse_json(canonical_bytes(pin_value),16384);check_active()
    with connection.transaction():
        connection.execute("SET LOCAL statement_timeout = '15s'")
        role=connection.execute('SELECT current_user').fetchone()
        if role!=('photo_scheduler',):raise AdoptionInvalid('NEGATIVE_CATALOG_ROLE')
        connection.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        row=connection.execute('SELECT photo.v29_negative_qualification_sources(%s,%s,%s,%s,%s::jsonb)',
            (tenant,project,actor,session,canonical_bytes(owned).decode())).fetchone()
        check_active()
        if row is None or len(row)!=1 or row[0] is None:raise AdoptionInvalid('NEGATIVE_QUALIFICATION_STALE')
        value=parse_json(canonical_bytes(row[0]),65536)
        keys={'contract','artifact_id','sha256','bucket','object_key','object_version','bytes'}
        if not isinstance(value,list) or len(value)!=2:raise AdoptionInvalid('NEGATIVE_CATALOG_INVALID')
        for item,(contract,ref,limit) in zip(value,[('NegativeCalibration',pin.calibration,65536),('QualificationReport',pin.report,8<<20)]):
            if (not isinstance(item,dict) or set(item)!=keys or item['contract']!=contract
                    or item['artifact_id']!=str(ref.artifact_id) or item['sha256']!=ref.sha256
                    or type(item['bytes']) is not int or not 2<=item['bytes']<=limit
                    or not isinstance(item['bucket'],str) or not 1<=len(item['bucket'])<=255
                    or not isinstance(item['object_key'],str) or not 1<=len(item['object_key'])<=1024
                    or not isinstance(item['object_version'],str) or not 1<=len(item['object_version'])<=1024
                    or item['object_version']=='null'):
                raise AdoptionInvalid('NEGATIVE_CATALOG_INVALID')
        if value[0]['bucket']!=value[1]['bucket']:raise AdoptionInvalid('NEGATIVE_CATALOG_INVALID')
    check_active();return value
