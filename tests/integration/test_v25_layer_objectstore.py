"""Actual MinIO version reads and full composition proof; SQL authority separate."""
from importlib.util import spec_from_file_location,module_from_spec
from pathlib import Path
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import ArtifactRef,ArtifactEvidence,AdoptionInvalid
from photo_worker.v2.adoption_store import PostgresObjectEvidenceReader,_Location
from photo_worker.v2.layer_adoption import verify_layer_composition
from test_v25_lens_objectstore import lens_objects


@pytest.mark.parametrize("legacy_alpha",[False,True])
def test_composition_proof_pins_all_object_versions(lens_objects,tmp_path,legacy_alpha):
    client,bucket=lens_objects
    spec=spec_from_file_location('layer_bytes_fixture',Path(__file__).parents[1]/'unit/test_layer_adoption_bytes.py')
    module=module_from_spec(spec);spec.loader.exec_module(module)
    _,params,root,objects,pixels=module.fixture(legacy_alpha=legacy_alpha)
    reader=PostgresObjectEvidenceReader(SimpleNamespace(autocommit=True),client,
        tenant_id=uuid4(),task_id=uuid4(),attempt=1,fence=1,allowed_bucket=bucket)
    # SQL proof is deliberately outside this test: use exact fixture catalog metadata.
    for artifact,(body,media,scope,role) in objects.items():
        from hashlib import sha256
        ref=ArtifactRef(artifact,sha256(body).hexdigest());key='composition/'+str(artifact)
        version=client.put_object(bucket,key,BytesIO(body),len(body),content_type=media).version_id
        assert version not in ('',None,'null')
        reader._locations[ref]=_Location(bucket,key,version,ArtifactEvidence(ref,media,len(body),role,scope))
    mode="normalize_uint16" if legacy_alpha else "require_float32"
    verify_layer_composition(reader,params,root,scratch_root=tmp_path,max_bytes=1000000,alpha_mode=mode)
    assert not list(tmp_path.iterdir())
    # Corrupt latest objects for both a document and a pixel array. Pinned versions remain valid.
    references=[root,ArtifactRef(pixels.artifact_id,pixels.sha256)]
    from photo_worker.operations.optical.contracts import ImageResult,ImageReport
    output_document=ImageResult.model_validate_json(objects[root.artifact_id][0])
    output_report=ImageReport.model_validate_json(objects[output_document.report.artifact_id][0])
    assert output_report.parent_reports
    references.extend(ArtifactRef(ref.artifact_id,ref.sha256) for ref in output_report.parent_reports)

    if legacy_alpha:
        from photo_worker.contracts.images import WorkingImage
        from photo_worker.operations.layers.contracts import LayerComposeParams
        from photo_worker.operations.optical.contracts import ImageResult
        typed=LayerComposeParams.model_validate_json(objects[params.artifact_id][0])
        source=WorkingImage.model_validate_json(objects[typed.base.source.artifact_id][0])
        output=ImageResult.model_validate_json(objects[root.artifact_id][0])
        image=WorkingImage.model_validate_json(objects[output.working_image.artifact_id][0])
        references.extend(ArtifactRef(ref.artifact_id,ref.sha256) for ref in (source.alpha,image.alpha))
        with pytest.raises(AdoptionInvalid):
            verify_layer_composition(reader,params,root,scratch_root=tmp_path,max_bytes=1000000)
        assert not list(tmp_path.iterdir())
    for reference in references:
        location=reader._locations[reference]
        newer=b'not the pinned composition artifact'
        new_version=client.put_object(bucket,location.key,BytesIO(newer),len(newer),content_type=location.evidence.media_type).version_id
        assert new_version!=location.version
    verify_layer_composition(reader,params,root,scratch_root=tmp_path,max_bytes=1000000,alpha_mode=mode)
    assert not list(tmp_path.iterdir())
    # An incorrect version pin must fail even with the original catalog hash.
    ref=references[-1];location=reader._locations[ref]
    reader._locations[ref]=_Location(location.bucket,location.key,new_version,location.evidence)
    with pytest.raises(AdoptionInvalid):
        verify_layer_composition(reader,params,root,scratch_root=tmp_path,max_bytes=1000000,alpha_mode=mode)
    assert not list(tmp_path.iterdir())
