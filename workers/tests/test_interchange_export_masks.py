from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import MaskRef,Canvas,TransformChain
from photo_worker.contracts.override import OverrideV1
from photo_worker.v2.interchange_export_inventory import QualifiedExportInventory
from photo_worker.v2.interchange_export_prepare import ExportSource
from photo_worker.v2.interchange_export_masks import compile_export_masks
from photo_worker.v2.interchange_read_budget import ImportReadBudget


def mask_fixture():
    binary=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    mask=MaskRef(artifact=binary,canvas=Canvas(width=32,height=24,orientation=1,space_id='canonical',source_sha256='b'*64),
        transform_chain=TransformChain.create([]),selector='skin',confidence=1,model_lock_id=None,protected=False)
    raw=canonical_bytes(mask);reference=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    sources=(ExportSource(reference,'dependencies/mask.json','MaskRef',len(raw),True),
             ExportSource(binary,'dependencies/pixels.bin','MaskArray',4000,False))
    class Reader:
        read_budget=ImportReadBudget(128 << 20)
        rechecks=0
        def read_json(self,ref,maximum):assert ref==reference and len(raw)<=maximum;return raw
        def recheck(self):self.rechecks+=1
    return QualifiedExportInventory(sources,()),Reader(),mask


def test_bundle_masks_require_typed_witness_and_preserve_canvas():
    inventory,reader,mask=mask_fixture()
    declarations=compile_export_masks(inventory,reader)
    assert len(declarations)==1 and declarations[0].path=='dependencies/pixels.bin'
    assert declarations[0].canvas_hash==canonical_hash(mask.canvas) and declarations[0].sha256==mask.artifact.sha256
    assert reader.rechecks==1


@pytest.mark.parametrize('mode',['missing_binary','missing_witness','wrong_contract','hash'])
def test_bundle_masks_reject_missing_or_changed_witness(mode):
    inventory,reader,mask=mask_fixture();sources=list(inventory.sources)
    if mode=='missing_binary':sources=sources[:1]
    elif mode=='missing_witness':sources=sources[1:]
    elif mode=='wrong_contract':
        item=sources[1];sources[1]=ExportSource(item.reference,item.path,'PixelArray',item.bytes,item.sidecar)
    elif mode=='hash':
        original=reader.read_json;reader.read_json=lambda *args:original(*args)+b' '
    with pytest.raises(ValueError):compile_export_masks(QualifiedExportInventory(tuple(sources),()),reader)


def test_bundle_factory_refuses_missing_recipe_installation():
    from photo_worker.operations.interchange.export_bundle_factory import BundleExportPreparationFactory
    with pytest.raises(ValueError,match='INSTALLATION'):BundleExportPreparationFactory(None)


def test_inline_confirmed_person_mask_in_override_witnesses_pixels():
    binary=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    model='c'*64
    canvas=Canvas(width=32,height=24,orientation=1,space_id='canonical',source_sha256='b'*64)
    mask=MaskRef(artifact=binary,canvas=canvas,transform_chain=TransformChain.create([]),
        selector='person',confidence=1,model_lock_id=model,protected=False)
    override=OverrideV1.model_validate({'schema_version':1,'input_sha256':canvas.source_sha256,
        'modules':{'strength':0.7},'revision':1,'mask_refs':[{'binding_id':'subject',
        'purpose':'person_subject','mask':mask,'confirmed':True,'strength':1}]})
    raw=canonical_bytes(override)
    ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    inventory=QualifiedExportInventory((ExportSource(ref,'dependencies/override.json','Override',len(raw),True),
        ExportSource(binary,'dependencies/pixels.bin','MaskPixels',4000,False)),())
    class Reader:
        read_budget=ImportReadBudget(128 << 20)
        def read_json(self,selected,maximum):
            assert selected==ref and len(raw)<=maximum
            return raw
        def recheck(self):pass
    declarations=compile_export_masks(inventory,Reader())
    assert len(declarations)==1 and declarations[0].canvas_hash==canonical_hash(canvas)
