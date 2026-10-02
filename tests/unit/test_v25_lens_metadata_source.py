from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
from hashlib import sha256
from PIL import Image,TiffImagePlugin
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_hash
from photo_worker.contracts.images import Canvas,WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.operations.optical.metadata_source import OriginalLensBinding,ArtifactLensMetadataResolver
from photo_worker.runtime.executor import OperationFailure


def test_original_exif_resolution_requires_exact_compiler_binding(tmp_path):
    exif=Image.Exif();exif[272]='Camera'
    exif[34665]={42036:'Lens',37386:TiffImagePlugin.IFDRational(50,1),33437:TiffImagePlugin.IFDRational(2,1)}
    path=tmp_path/'original.jpg';Image.new('RGB',(4,3),'white').save(path,exif=exif)
    original=ArtifactRef(artifact_id=uuid4(),sha256=sha256(path.read_bytes()).hexdigest())
    binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),canvas_hash='b'*64)
    item=OriginalLensBinding(binding=binding,original=original,suffix='.jpg')
    class Artifacts:
        opened=0;closed=0
        @contextmanager
        def input(self,ref,suffix):
            assert ref==original and suffix=='.jpg';self.opened+=1
            try:yield NS(path=path)
            finally:self.closed+=1
    artifacts=Artifacts();resolver=ArtifactLensMetadataResolver(artifacts,[item])
    assert resolver(binding).focal_mm==50
    assert artifacts.opened==artifacts.closed==1
    with pytest.raises(OperationFailure):resolver(binding.model_copy(update={'canvas_hash':'c'*64}))
    assert artifacts.opened==1
    with pytest.raises(OperationFailure):ArtifactLensMetadataResolver(artifacts,[item,item])(binding)


def test_declared_binding_discovery_requires_exactly_one_document():
    canvas=Canvas(width=4,height=3,orientation=1,space_id='source',source_sha256='c'*64)
    binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),canvas_hash=canonical_hash(canvas))
    original=ArtifactRef(artifact_id=uuid4(),sha256='c'*64)
    item=OriginalLensBinding(binding=binding,original=original,suffix='.jpg')
    document=ArtifactRef(artifact_id=uuid4(),sha256='d'*64)
    class Artifacts:
        refs=(document,);reads=0
        def declared_contract_refs(self,name,maximum):
            assert name=='OriginalLensBinding' and maximum==1
            return self.refs
        def contract(self,ref,model):
            self.reads+=1
            if ref==document:
                assert model is OriginalLensBinding;return item
            assert ref==binding.source and model is WorkingImage
            return NS(canvas=canvas,dtype='float32')
    artifacts=Artifacts()
    resolver=ArtifactLensMetadataResolver.from_declared_inputs(artifacts)
    assert resolver.bindings==(item,) and artifacts.reads==2
    artifacts.refs=()
    with pytest.raises(OperationFailure):ArtifactLensMetadataResolver.from_declared_inputs(artifacts)
    assert artifacts.reads==2

    artifacts.refs=(document,)
    canvas=canvas.model_copy(update={'source_sha256':'e'*64})
    with pytest.raises(OperationFailure):ArtifactLensMetadataResolver.from_declared_inputs(artifacts)
