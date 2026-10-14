from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4, uuid5
import pytest
from photo_worker.contracts.base import ArtifactRef as Ref, canonical_bytes
from photo_worker.contracts.export import EncodedImage
from photo_worker.contracts.metadata import MetadataDocument, MetadataParams, MetadataReport
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.adoption import ArtifactRef, ArtifactEvidence, AdoptionInvalid
from photo_worker.v2.delivery_metadata_freeze import freeze_delivery_metadata
import photo_worker.v2.delivery_metadata_prepare_result as module


def test_prepare_metadata_result_fences_io_and_closure(tmp_path, monkeypatch):
    run_case(tmp_path, monkeypatch, 'valid')


@pytest.mark.parametrize('mode', ['extra-output', 'bad-digest', 'changed-evidence', 'bad-policy', 'oversized-block'])
def test_prepare_metadata_result_refuses_invalid_evidence(tmp_path, monkeypatch, mode):
    with pytest.raises(AdoptionInvalid):
        run_case(tmp_path, monkeypatch, mode)
    assert not list(tmp_path.iterdir())


def run_case(tmp_path, monkeypatch, mode):
    def ref(body):
        return Ref(artifact_id=uuid4(), sha256=sha256(body).hexdigest())
    def adopted(value):
        return ArtifactRef(value.artifact_id, value.sha256)
    task=uuid4()
    source=canonical_bytes(MetadataDocument(author='Author'))
    metadata=ref(source)
    binding=VersionBinding(asset_version_id=uuid4(), source=ref(b'original'), canvas_hash='b'*64)
    frozen=freeze_delivery_metadata(binding, metadata, source, 'copyright')
    projection=canonical_bytes(MetadataDocument(author='Author'))
    projected=Ref(artifact_id=uuid5(task, 'delivery:metadata:projection:1'), sha256=sha256(projection).hexdigest())
    before_bytes=b'before'; after_bytes=b'after'
    before=EncodedImage(artifact=ref(before_bytes), format='jpeg', width=4, height=3,
        icc_id='srgb_v1', icc_sha256='c'*64, alpha=False, metadata_policy='strip')
    encoded=ref(canonical_bytes(before))
    params=MetadataParams(encoded=encoded, document=projected, policy='copyright_only')
    if mode=='bad-policy': params=params.model_copy(update={'policy':'safe_camera'})
    after=before.model_copy(update={'artifact':ref(after_bytes), 'metadata_policy':'copyright_only'})
    report=MetadataReport(encoded=encoded, document=projected, policy='copyright_only',
        copied_fields=['author'], orientation=1, stale_previews_removed=True, icc_preserved=True)
    params_ref=ref(canonical_bytes(params)); root=ref(canonical_bytes(after)); report_ref=ref(canonical_bytes(report))
    bodies={}; evidence={}; reads={}; streamed=[]
    def add(value, body, origin, role, media='application/json'):
        key=adopted(value); bodies[key]=body
        evidence[key]=ArtifactEvidence(key, media, len(body), role, origin)
    add(params_ref, canonical_bytes(params), 'input', 'task_params')
    add(encoded, canonical_bytes(before), 'input', 'encoded_manifest')
    add(projected, projection, 'input', 'v30_metadata_projection')
    add(root, canonical_bytes(after), 'output', 'encoded_manifest')
    add(report_ref, canonical_bytes(report), 'output', 'metadata_report')
    add(before.artifact, before_bytes, 'input', 'encoded_image', 'image/jpeg')
    add(after.artifact, after_bytes, 'output', 'encoded_image', 'image/jpeg')
    outputs={adopted(root), adopted(report_ref), adopted(after.artifact)}
    class Reader:
        def evidence(self, key):
            reads[key]=reads.get(key,0)+1
            value=evidence[key]
            if mode=='changed-evidence' and reads[key]>1:
                return ArtifactEvidence(key, value.media_type, value.bytes+1, value.role, value.source)
            return value
        def read_json(self, key, limit): return bodies[key]
        def staged_outputs(self):
            return outputs | {adopted(ref(b'extra'))} if mode=='extra-output' else outputs
        def iter_bytes(self, key, limit):
            streamed.append(key)
            if mode=='oversized-block': yield b'x'*65537
            elif mode=='bad-digest': yield b'x'*len(bodies[key])
            else: yield bodies[key]
    def physical(before_path, after_path, *args, **kwargs):
        assert before_path.read_bytes()==before_bytes
        assert after_path.read_bytes()==after_bytes
        return SimpleNamespace(documents=SimpleNamespace(projection_jcs=projection))
    monkeypatch.setattr(module, 'verify_delivery_metadata_files', physical)
    try:
        result=module.prepare_delivery_metadata_result(Reader(), adopted(params_ref), adopted(root), adopted(report_ref),
            task, encoded, frozen, sha256(frozen).hexdigest(), binding, metadata, source,
            policy='copyright', scratch_root=tmp_path, max_bytes=100000, max_pixels=100)
    finally:
        if mode in ('extra-output', 'bad-policy'): assert streamed==[]
    assert result.root==adopted(root)
    assert result.raster==adopted(after.artifact)
    assert len(streamed)==2
    assert not list(tmp_path.iterdir())
