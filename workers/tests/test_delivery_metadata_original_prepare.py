from dataclasses import replace
from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import pytest
from PIL import Image
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.metadata import MetadataDocument
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.adoption import ArtifactEvidence,ArtifactRef as EvidenceRef,AdoptionInvalid
from photo_worker.v2.delivery_metadata_original_prepare import prepare_delivery_metadata_original


@pytest.mark.parametrize('mode',['valid','changed','digest','oversized','alias','approval'])
def test_original_preparation_exact_bytes_authority_and_cleanup(tmp_path,mode):
    stream=BytesIO();image=Image.new('RGB',(4,3));exif=Image.Exif();exif[315]='Author'
    image.save(stream,format='JPEG',exif=exif);body=stream.getvalue()
    original=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
    binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='b'*64),canvas_hash='c'*64)
    metadata_id=original.artifact_id if mode=='alias' else uuid4()
    evidence=ArtifactEvidence(EvidenceRef(original.artifact_id,original.sha256),'image/jpeg',len(body),None,'input')
    class Reader:
        reads=0;streams=0;closed=False
        def evidence(self,ref):
            self.reads+=1
            return replace(evidence,bytes=evidence.bytes+1) if mode=='changed' and self.reads>1 else evidence
        def iter_bytes(self,ref,size):
            self.streams+=1
            try:
                if mode=='oversized':yield b'x'*65537
                elif mode=='digest':yield b'x'*len(body)
                else:yield body
            finally:self.closed=True
    reader=Reader()
    def run():return prepare_delivery_metadata_original(reader,original,binding,metadata_id,'copyright',
        approved_fields=('gps',) if mode=='approval' else (),scratch_root=tmp_path,max_bytes=100000)
    if mode=='valid':
        result=run()
        assert result.source.original_jcs==canonical_bytes(original)
        assert result.source.metadata_jcs==canonical_bytes(MetadataDocument(author='Author'))
        assert reader.reads==2
    else:
        with pytest.raises(ValueError):run()
    if mode in ('alias','approval'):assert reader.streams==0
    else:assert reader.closed
    assert not list(tmp_path.iterdir())


def test_original_over_worker_limit_refused_before_download(tmp_path):
    from photo_worker.v2.delivery_metadata_source import MAX_METADATA_ORIGINAL_BYTES
    original=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    binding=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=uuid4(),sha256='b'*64),canvas_hash='c'*64)
    class Reader:
        def evidence(self,ref):
            return ArtifactEvidence(ref,'image/jpeg',MAX_METADATA_ORIGINAL_BYTES+1,None,'input')
        def iter_bytes(self,*args):
            raise AssertionError('oversized original must not be downloaded')
    with pytest.raises(AdoptionInvalid,match='DELIVERY_METADATA_ORIGINAL_EVIDENCE'):
        prepare_delivery_metadata_original(Reader(),original,binding,uuid4(),'copyright',
            scratch_root=tmp_path,max_bytes=2**53-1)
    assert not list(tmp_path.iterdir())


def test_source_over_worker_limit_refused_before_open(tmp_path):
    from photo_worker.v2.delivery_metadata_source import prepare_delivery_metadata_source,MAX_METADATA_ORIGINAL_BYTES
    original=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    # A missing path would raise FileNotFoundError if file IO preceded admission.
    with pytest.raises(ValueError,match='DELIVERY_METADATA_SOURCE_INPUT'):
        prepare_delivery_metadata_source(tmp_path/'missing.jpg',original,'image/jpeg',
            MAX_METADATA_ORIGINAL_BYTES+1,max_bytes=2**53-1)
