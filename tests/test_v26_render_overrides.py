from contextlib import contextmanager
from io import BytesIO
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.override import OverrideV1
from photo_worker.contracts.style import StyleV2
from photo_worker.v2.render_overrides import load_render_overrides,merge_render_override_modules


def source():return SimpleNamespace(canvas=SimpleNamespace(source_sha256='a'*64),transform_chain=SimpleNamespace(hash='b'*64))
def override(strength,sha='a'*64):return OverrideV1.model_validate_json(canonical_bytes(dict(schema_version=1,input_sha256=sha,modules=dict(strength=strength),revision=1,accepted_candidate_ids=['candidate'])))


def test_ordered_module_merge_preserves_other_style_fields_and_original_documents():
    base=StyleV2.model_validate_json((Path(__file__).parents[1]/'docs/implementation/contracts/examples/style-fixed.example.json').read_bytes())
    first,last=override(.4),override(.7)
    merged=merge_render_override_modules(base,(first,last),source())
    assert merged.strength==.7 and base.strength==1
    assert merged.global_==base.global_ and merged.quality==base.quality
    assert first.accepted_candidate_ids==['candidate']
    with pytest.raises(ValueError,match='SOURCE_CHANGED'):merge_render_override_modules(base,(override(.5,'c'*64),),source())


def test_override_loader_preserves_order_deduplicates_io_and_enforces_budget():
    document=override(.4);raw=canonical_bytes(document);ref=ArtifactRef.model_validate_json(canonical_bytes(dict(artifact_id=str(uuid4()),sha256=canonical_hash(document))))
    class Artifacts:
        reads=0
        @contextmanager
        def stream(self,reference):
            self.reads+=1
            with BytesIO(raw) as stream:yield stream
    artifacts=Artifacts();inputs=SimpleNamespace(recipe=SimpleNamespace(recipe=SimpleNamespace(overrides=[ref,ref])),source=source())
    result=load_render_overrides(artifacts,inputs,max_document_bytes=len(raw))
    assert artifacts.reads==1 and len(result)==2 and result[0].accepted_candidate_ids==['candidate']
    result[0].accepted_candidate_ids.append('new');assert result[1].accepted_candidate_ids==['candidate']
    with pytest.raises(ValueError,match='DOCUMENT_BUDGET'):load_render_overrides(artifacts,inputs,max_document_bytes=len(raw)-1)


def test_complete_global_module_replacement_preserves_authored_nonscalar_fields():
    base=StyleV2.model_validate_json((Path(__file__).parents[1]/'docs/implementation/contracts/examples/style-fixed.example.json').read_bytes())
    color=base.global_.model_dump(mode='json',by_alias=True);color['gains_rgb']=[1.1,1,1];color['saturation']=.9
    patch=OverrideV1.model_validate_json(canonical_bytes(dict(schema_version=1,input_sha256='a'*64,modules={'global':color},revision=1)))
    merged=merge_render_override_modules(base,(patch,override(.5)),source())
    assert merged.global_.gains_rgb==[1.1,1,1] and merged.global_.saturation==.9
    assert merged.global_.model_dump(mode='json',by_alias=True)==color
    assert base.global_.gains_rgb==[1,1,1] and merged.local==base.local
