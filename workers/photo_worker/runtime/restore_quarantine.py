"""Streaming restore catalogue comparison; produces a quarantine *plan* only.

Physical inventory must contain all non-delete-marker versions in installed
buckets, including historical versions. A staged plan is incomplete until this
function returns evidence; failures must never activate staged quarantine rows.
"""
from dataclasses import dataclass
from hashlib import sha256
from .restore_inventory import RestoreObject, _validate
from photo_worker.contracts.base import canonical_bytes
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestorePhysicalVersion:
    bucket: str
    key: str
    version: str
    size: int


@dataclass(frozen=True)
class RestoreQuarantineEvidence:
    schema_version: str
    referenced: int
    physical: int
    extra: int
    extra_bytes: int
    plan_sha256: str


def plan_restore_quarantine(expected, physical, stage_extra, *, check_active,
                            maximum_objects=10_000_000, maximum_extra_bytes=2**53-1):
    if (not callable(stage_extra) or not callable(check_active)
            or type(maximum_objects) is not int or not 1<=maximum_objects<=10_000_000
            or type(maximum_extra_bytes) is not int or not 0<=maximum_extra_bytes<=2**53-1):
        raise ValueError('RESTORE_QUARANTINE_LIMITS')
    require_restore_active(check_active)
    def rows(source, physical_rows):
        previous=None
        for count,row in enumerate(source):
            require_restore_active(check_active)
            if count>=maximum_objects:
                raise ValueError('RESTORE_QUARANTINE_LIMITS')
            if physical_rows:
                if not isinstance(row,RestorePhysicalVersion):
                    raise ValueError('RESTORE_PHYSICAL_INVALID')
                _validate(RestoreObject(row.bucket,row.key,row.version,'0'*64,row.size))
            else:
                _validate(row)
            identity=tuple(v.encode('utf-8') for v in (row.bucket,row.key,row.version))
            if previous is not None and identity<=previous:
                raise ValueError('RESTORE_CATALOGUE_ORDER_INVALID')
            previous=identity
            yield identity,row
        require_restore_active(check_active)
    wanted=rows(expected,False);actual=rows(physical,True)
    referenced=count=extra=extra_bytes=0
    digest=sha256(b'photoeditor.restore-quarantine-plan/v1\n')
    try:
        left=next(wanted,None);right=next(actual,None)
        while left is not None or right is not None:
            require_restore_active(check_active)
            if left is not None and (right is None or left[0]<right[0]):
                raise ValueError('RESTORE_REFERENCED_VERSION_MISSING')
            if left is not None and right is not None and left[0]==right[0]:
                if left[1].size!=right[1].size:
                    raise ValueError('RESTORE_REFERENCED_SIZE_MISMATCH')
                referenced+=1;count+=1
                left=next(wanted,None);right=next(actual,None)
                continue
            version=right[1]
            if version.size>maximum_extra_bytes-extra_bytes:
                raise ValueError('RESTORE_QUARANTINE_LIMITS')
            if stage_extra(version) is False:
                raise RuntimeError('RESTORE_QUARANTINE_STAGE_DENIED')
            require_restore_active(check_active)
            digest.update(canonical_bytes({'bucket':version.bucket,'key':version.key,
                'version':version.version,'size':version.size}));digest.update(b'\n')
            extra+=1;count+=1;extra_bytes+=version.size
            right=next(actual,None)
        require_restore_active(check_active)
        return RestoreQuarantineEvidence('restore-quarantine-plan/v1',referenced,count,
                                        extra,extra_bytes,digest.hexdigest())
    finally:
        wanted.close();actual.close()
