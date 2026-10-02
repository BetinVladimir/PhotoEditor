from hashlib import sha256
from uuid import UUID
import pytest
from photo_worker.contracts.base import ArtifactRef as Ref, canonical_bytes, canonical_hash
from photo_worker.contracts.images import Canvas, ColorSpace, WorkingImage, TransformChain
from photo_worker.operations.session.contracts import VersionBinding, FaceObservations
from photo_worker.operations.capture.contracts import CapturePrepareParams, CapturePrepared
from photo_worker.v2.adoption import ArtifactRef, ArtifactEvidence, AdoptionInvalid
from photo_worker.v2.capture_prepare_adoption import prepare_capture_inputs_adoption


@pytest.mark.parametrize("alpha_bytes", [None, 8396800, 16785408, 16785409])
def test_capture_prepare_adoption_requires_exact_attempt_outputs(alpha_bytes):
    bodies, evidence, outputs = {}, {}, set()
    def put(number, value, source, role=None, media='application/json'):
        body = canonical_bytes(value) if media == 'application/json' else b'array'
        ref = Ref(artifact_id=UUID(int=number), sha256=sha256(body).hexdigest())
        key = ArtifactRef(ref.artifact_id, ref.sha256)
        bodies[key] = body
        evidence[key] = ArtifactEvidence(key, media, len(body), role, source)
        if source == 'output': outputs.add(key)
        return ref
    pixels = Ref(artifact_id=UUID(int=100), sha256='a'*64)
    source = WorkingImage(pixels=pixels, alpha=(pixels if alpha_bytes is not None else None), dtype='float32',
        canvas=Canvas(width=2,height=2,orientation=1,space_id='work',source_sha256='b'*64),
        color=ColorSpace(profile_id='work_rec2020_gamma24_v1',icc_sha256='c'*64,encoding='gamma24'),
        transform_chain=TransformChain.create([]))
    source_ref = put(1,source,'input')
    binding = VersionBinding(asset_version_id=UUID(int=10),source=source_ref,canvas_hash=canonical_hash(source.canvas))
    params = CapturePrepareParams(schema_version=1,binding=binding,calibration=pixels,model_lock_sha='d'*64)
    params_ref = put(2,params,'input')
    child = put(3,None,'output','working_rgb','application/octet-stream')
    alpha = None
    if alpha_bytes is not None:
        alpha = put(7,None,'output','working_alpha','application/octet-stream')
        key = ArtifactRef(alpha.artifact_id, alpha.sha256)
        evidence[key] = ArtifactEvidence(key, 'application/octet-stream', alpha_bytes, 'working_alpha', 'output')
    proxy = source.model_copy(update={'pixels':child, 'alpha':alpha})
    proxy_ref = put(4,proxy,'output','working_image')
    faces_ref = put(5,FaceObservations(schema_version=1,binding=binding,faces=[]),'output','faces')
    root = put(6,CapturePrepared(schema_version=1,binding=binding,proxy=proxy_ref,faces=faces_ref),'output','document')
    class Reader:
        def evidence(self,ref): return evidence[ref]
        def read_json(self,ref,maximum): return bodies[ref]
        def staged_outputs(self): return outputs
    if alpha_bytes == 16785409:
        with pytest.raises(AdoptionInvalid, match='EVIDENCE_INVALID'):
            prepare_capture_inputs_adoption(UUID(int=20),Reader(),params_ref,root)
        return
    result = prepare_capture_inputs_adoption(UUID(int=20),Reader(),params_ref,root)
    assert len(result.inventory)==(7 if alpha_bytes is not None else 6)
    outputs.add(ArtifactRef(UUID(int=99),'e'*64))
    with pytest.raises(AdoptionInvalid,match='OUTPUT_CLOSURE'):
        prepare_capture_inputs_adoption(UUID(int=20),Reader(),params_ref,root)
