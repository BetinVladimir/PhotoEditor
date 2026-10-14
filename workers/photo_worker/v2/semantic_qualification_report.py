"""Exact-byte semantic report proof against a trusted frozen/current policy.

Live SQL scope, object version/size and issuer authentication remain mandatory
caller evidence. Neither a report nor its passed flag establishes those proofs.
"""
from hashlib import sha256
from math import isfinite
from datetime import datetime,timezone
import re
from photo_worker.contracts.base import parse_json,canonical_bytes,canonical_hash
from .adoption import AdoptionInvalid
from .semantic_qualification_pin import SemanticQualificationPin,_sha

POLICY_KEYS=frozenset(('schema_version','policy_id','operation','operation_version','backend',
    'model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','issuer_id','required_checks',
    'test_fixture_shas','calibration_shas','evidence_shas','license_cleared'))
REPORT_KEYS=frozenset(('schema_version','operation','version','model_lock_sha','runtime_lock_sha',
    'device_profile_sha','dataset_sha','input_fixture_shas','checks','metrics','status','issuer_id','created_at'))
METRIC_KEYS=frozenset(('p50_ms','p95_ms','peak_vram_bytes','peak_rss_bytes','scratch_bytes'))
TIMESTAMP=re.compile(r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$')


def _invalid():raise AdoptionInvalid('SEMANTIC_QUALIFICATION_REPORT_INVALID')


def _set(value,maximum,*,digests):
    if not isinstance(value,list) or not 1<=len(value)<=maximum:_invalid()
    if digests:
        if any(not _sha(item) for item in value):_invalid()
    elif any(not isinstance(item,str) or not 1<=len(item.encode('utf-8'))<=100 for item in value):_invalid()
    if len(set(value))!=len(value):_invalid()
    return sorted(value)


def _verify_qualification_report(raw,pin,policy,*,operation,backend,check_active):
    """Own and validate current policy, then exact canonical report bytes."""
    check_active()
    if not isinstance(pin,SemanticQualificationPin) or not isinstance(raw,bytes) or not 2<=len(raw)<=8*1024*1024:_invalid()
    try:
        policy=parse_json(canonical_bytes(policy),8*1024*1024)
        if not isinstance(policy,dict) or set(policy)!=POLICY_KEYS:_invalid()
        if type(policy['schema_version']) is not int or policy['schema_version']!=1 or policy['license_cleared'] is not True:_invalid()
        expected=dict(policy_id=str(pin.policy_id),operation=operation,operation_version='1',
            backend=backend,model_lock_sha=pin.model_lock_sha,runtime_lock_sha=pin.runtime_lock_sha,
            device_profile_sha=pin.device_profile_sha,dataset_sha=pin.dataset_sha,issuer_id=str(pin.issuer_id))
        if any(policy[key]!=value for key,value in expected.items()):_invalid()
        policy['required_checks']=_set(policy['required_checks'],1000,digests=False)
        for key in ('test_fixture_shas','calibration_shas','evidence_shas'):
            policy[key]=_set(policy[key],10000,digests=True)
        if pin.calibration.sha256 not in policy['calibration_shas'] or canonical_hash(policy)!=pin.policy_sha:_invalid()
        if sha256(raw).hexdigest()!=pin.report.sha256:_invalid()
        report=parse_json(raw,8*1024*1024)
        if not isinstance(report,dict) or set(report)!=REPORT_KEYS or canonical_bytes(report)!=raw:_invalid()
        if type(report['schema_version']) is not int or report['schema_version']!=1 or report['status']!='passed':_invalid()
        for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','issuer_id'):
            if report[key]!=expected[key]:_invalid()
        if report['operation']!=operation or report['version']!='1':_invalid()
        timestamp=report['created_at']
        if not isinstance(timestamp,str) or TIMESTAMP.fullmatch(timestamp) is None:_invalid()
        created=datetime.fromisoformat(timestamp.replace('Z','+00:00'))
        if created.astimezone(timezone.utc)==datetime.min.replace(tzinfo=timezone.utc):_invalid()
        fixtures=_set(report['input_fixture_shas'],10000,digests=True)
        if (not set(fixtures)<=set(policy['test_fixture_shas'])
                or set(fixtures)&set(policy['calibration_shas'])):_invalid()
        metrics=report['metrics']
        if not isinstance(metrics,dict) or set(metrics)!=METRIC_KEYS:_invalid()
        if any(type(value) not in (int,float) or not isfinite(value) or not 0<=value<=2**53-1 for value in metrics.values()):_invalid()
        if metrics['p95_ms']<metrics['p50_ms']:_invalid()
        checks=report['checks']
        if not isinstance(checks,list) or not 1<=len(checks)<=1000:_invalid()
        ids=set();evidence=set(policy['evidence_shas'])
        for check in checks:
            check_active()
            if not isinstance(check,dict) or set(check)!= {'id','passed','evidence_sha'}:_invalid()
            key=check['id']
            if (not isinstance(key,str) or not 1<=len(key.encode('utf-8'))<=100 or key in ids
                    or check['passed'] is not True or not _sha(check['evidence_sha']) or check['evidence_sha'] not in evidence):_invalid()
            ids.add(key)
        if not set(policy['required_checks'])<=ids:_invalid()
        check_active()
        return report
    except (ValueError,TypeError,OverflowError,UnicodeError) as error:
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_REPORT_INVALID') from error


def verify_semantic_qualification_report(raw,pin,policy,*,check_active=lambda:None):
    if type(pin) is not SemanticQualificationPin:
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_PIN_INVALID')
    return _verify_qualification_report(raw,pin,policy,operation='semantic.transfer',
        backend='cuda',check_active=check_active)


def verify_exposure_qualification_report(raw,pin,policy,*,check_active):
    from .exposure_qualification_pin import ExposureQualificationPin
    if type(pin) is not ExposureQualificationPin:
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_PIN_INVALID')
    return _verify_qualification_report(raw,pin,policy,operation='semantic.local_exposure',
        backend='cpu',check_active=check_active)
