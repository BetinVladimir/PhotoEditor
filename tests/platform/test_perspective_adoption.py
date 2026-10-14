from hashlib import sha256
from uuid import uuid4
import pytest
from test_perspective_adapter import source,params
from photo_worker.contracts.base import canonical_bytes,canonical_hash,ArtifactRef
from photo_worker.contracts.images import MaskRef
from photo_worker.operations.optical.contracts import ImageResult,ImageReport
from photo_worker.operations.optical.perspective_geometry import resolve_perspective_geometry
from photo_worker.operations.optical.perspective_result import perspective_working_image
from photo_worker.v2.perspective_adoption import verify_perspective_result_metadata
from photo_worker.v2.adoption import ArtifactEvidence
from photo_worker.v2.lens_bypass import _ref


def fixture(change=None,strength=1):
    docs={};roles={};image=source()
    def put(value,role,origin='output'):
        raw=canonical_bytes(value);ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
        docs[ref.artifact_id]=raw;roles[ref.artifact_id]=(role,origin);return ref
    def pixels(role):
        ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64);docs[ref.artifact_id]=b'array';roles[ref.artifact_id]=(role,'output');return ref
    source_ref=put(image,None,'input');intent=params(image,strength);intent.base.source=source_ref
    params_ref=put(intent,'Params','input')
    geometry=resolve_perspective_geometry(intent,image,None,max_pixels=8000)
    if strength==0:output=image;output_ref=source_ref;transform_ref=None
    else:
        output,transform=perspective_working_image(image,pixels('pixels'),pixels('alpha'),geometry)
        transform_ref=put(transform,'transform');output_ref=put(output,'working_image')
    changed=MaskRef(artifact=pixels('mask_pixels'),canvas=output.canvas,transform_chain=output.transform_chain,
        selector='changed',confidence=1.,model_lock_id=None,protected=False,object_id=None)
    if change=='mask':changed.protected=True
    changed_ref=put(changed,'mask')
    report=ImageReport(schema_version=1,status='pass' if strength==0 else 'review',operation='optical.perspective',
        skipped_count=0,reason=None if strength==0 else 'perspective_manual',outside_mask_max_error=0.,clipping_fraction=0.,
        identity=None,transform=transform_ref,sources=[intent.base],registrations=[])
    if change=='report':report.status='pass'
    report_ref=put(report,'report')
    result=ImageResult(schema_version=1,working_image=output_ref,report=report_ref,changed_mask=changed_ref,generated_mask=changed_ref if change=='generated' else None)
    root=put(result,'document')
    if change=='role':roles[root.artifact_id]=('report','output')
    class Reader:
        def evidence(self,ref):
            role,origin=roles[ref.artifact_id];return ArtifactEvidence(ref,'application/json',len(docs[ref.artifact_id]),role,origin)
        def read_json(self,ref,maximum):return docs[ref.artifact_id]
    return Reader(),_ref(params_ref),_ref(root)


@pytest.mark.parametrize('strength',[0,1])
def test_valid_metadata_and_exact_identity_source(strength):
    reader,params_ref,root=fixture(strength=strength)
    p,result=verify_perspective_result_metadata(reader,params_ref,root,max_pixels=8000,check_active=lambda:None)
    assert p.strength==strength and result.generated_mask is None


@pytest.mark.parametrize('change',['mask','report','generated','role'])
def test_tampered_metadata_refused(change):
    reader,params_ref,root=fixture(change)
    with pytest.raises(ValueError):verify_perspective_result_metadata(reader,params_ref,root,max_pixels=8000,check_active=lambda:None)
