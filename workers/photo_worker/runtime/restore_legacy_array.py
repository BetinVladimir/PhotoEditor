"""Stream exact legacy NPY bytes; caller separately proves SQL ownership."""
from io import BytesIO
from hashlib import sha256
import math
import numpy as np
from photo_worker.contracts.base import ArtifactRef
from .restore_inventory import RestoreObject,_validate
from .restore_objects import MinioRestoreObjects
from .restore_authority import require_restore_active


def verify_legacy_array_bytes(client,buckets,reference,obj,shape,dtype,role,*,check_active):
    _validate(obj)
    if (not isinstance(reference,ArtifactRef) or reference.artifact_id.int==0 or reference.sha256!=obj.sha256
            or not isinstance(shape,tuple) or len(shape) not in (2,3)
            or any(type(n) is not int or n<1 for n in shape) or shape[0]*shape[1]>200000000
            or role not in ('working_rgb','working_alpha') or dtype not in ('float32','uint16')
            or role=='working_rgb' and (len(shape)!=3 or shape[2]!=3)
            or role=='working_alpha' and len(shape)!=2 or not callable(check_active)):
        raise ValueError('RESTORE_LEGACY_ARRAY_INSTALLATION')
    descriptor='<f4' if dtype=='float32' else '<u2';width=4 if dtype=='float32' else 2
    header=BytesIO();np.lib.format.write_array_header_1_0(header,{'descr':descriptor,'fortran_order':False,'shape':shape})
    expected=header.getvalue();size=len(expected)+math.prod(shape)*width
    if obj.size!=size:raise ValueError('RESTORE_LEGACY_ARRAY_SIZE')
    require_restore_active(check_active)
    store=MinioRestoreObjects(client,buckets,check_active=lambda:require_restore_active(check_active))
    digest=sha256();offset=0;pending=b''
    with store.open_version(obj) as stream:
        while True:
            require_restore_active(check_active);block=stream.read(min(65536,size-offset+1))
            if not block:break
            if offset+len(block)>size:raise ValueError('RESTORE_LEGACY_ARRAY_SIZE')
            digest.update(block);head=min(len(block),max(0,len(expected)-offset))
            if block[:head]!=expected[offset:offset+head]:raise ValueError('RESTORE_LEGACY_ARRAY_HEADER')
            offset+=len(block);pending+=block[head:];aligned=len(pending)//width*width
            values=np.frombuffer(pending[:aligned],dtype=descriptor)
            if dtype=='float32' and (not np.isfinite(values).all()
                    or role=='working_alpha' and ((values<0).any() or (values>1).any())):
                raise ValueError('RESTORE_LEGACY_ARRAY_VALUES')
            pending=pending[aligned:]
    if offset!=size or pending or digest.hexdigest()!=obj.sha256:raise ValueError('RESTORE_LEGACY_ARRAY_SHA')
    require_restore_active(check_active);return obj
