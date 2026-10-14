"""Bounded physical NPY verification using the production adoption validator."""
from dataclasses import dataclass
from types import SimpleNamespace
from .restore_inventory import RestoreObject,_validate
from .restore_objects import MinioRestoreObjects
from photo_worker.contracts.base import ArtifactRef
from photo_worker.v2.lens_adoption import verify_lens_array
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRenderArrayEvidence:
    schema_version: str
    object: RestoreObject
    shape: tuple[int,...]
    role: str


def verify_restore_render_array(client,buckets,reference,obj,shape,role,*,assert_fixture_scope,check_active):
    """Caller independently proves producer/input-reuse SQL authority.

    The validator's output-source tag is its physical validation interface,
    not a claim that this function established SQL output ownership.
    """
    _validate(obj)
    if (not isinstance(reference,ArtifactRef) or reference.artifact_id.int==0 or reference.sha256!=obj.sha256
            or not isinstance(shape,tuple) or len(shape) not in (2,3)
            or any(type(n) is not int or not 1<=n<=200000000 for n in shape)
            or shape[0]*shape[1]>200000000
            or role not in ('pixels','alpha','mask_pixels')
            or (role=='pixels' and (len(shape)!=3 or shape[2]!=3))
            or (role!='pixels' and len(shape)!=2)
            or obj.size>2400000128 or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_RENDER_ARRAY_INSTALLATION')
    def guard():require_restore_fixture(check_active,assert_fixture_scope,reference,obj,shape,role)
    guard();store=MinioRestoreObjects(client,buckets,check_active=guard)
    class Reader:
        def evidence(self,ref):
            if ref!=reference:raise ValueError('RESTORE_RENDER_ARRAY_REF')
            return SimpleNamespace(ref=reference,source='output',role=role,media_type='application/x-npy',bytes=obj.size)
        def iter_bytes(self,ref,size):
            if ref!=reference or size!=obj.size:raise ValueError('RESTORE_RENDER_ARRAY_REF')
            with store.open_version(obj) as stream:
                while True:
                    guard();data=stream.read(65536)
                    if not data:break
                    yield data
                guard()
    verify_lens_array(Reader(),reference,shape,role,unit=role!='pixels',binary=role=='mask_pixels',check_active=guard)
    guard()
    return RestoreRenderArrayEvidence('restore-render-array/v1',obj,shape,role)
