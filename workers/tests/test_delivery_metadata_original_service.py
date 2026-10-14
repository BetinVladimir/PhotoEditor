from contextlib import contextmanager
from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4
import pytest
from PIL import Image
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.adoption import ArtifactRef as EvidenceRef,AdoptionInvalid
from photo_worker.v2.delivery_metadata_original_reader import MetadataOriginalScope
from photo_worker.v2.delivery_metadata_original_service import prepare_authorized_metadata_original


@pytest.mark.parametrize('mode',['valid','version','source','digest','revoked'])
def test_authorized_original_composition(tmp_path,mode):
    buffer=BytesIO();image=Image.new('RGB',(4,3));exif=Image.Exif();exif[315]='Artist'
    image.save(buffer,format='JPEG',exif=exif);body=buffer.getvalue()
    scope=MetadataOriginalScope(uuid4(),uuid4(),uuid4(),EvidenceRef(uuid4(),'b'*64),
        uuid4(),uuid4(),EvidenceRef(uuid4(),sha256(body).hexdigest()))
    binding=VersionBinding(asset_version_id=uuid4() if mode=='version' else scope.version,
        source=ArtifactRef(artifact_id=uuid4() if mode=='source' else scope.source.artifact_id,
            sha256='c'*64 if mode=='digest' else scope.source.sha256),canvas_hash='d'*64)
    class Connection:
        autocommit=True;inside=False;reads=0
        @contextmanager
        def transaction(self):
            assert not self.inside
            self.inside=True
            try:yield
            finally:self.inside=False
        def execute(self,query,args=()):
            assert self.inside
            row=None
            if 'FROM photo.v30_delivery_metadata_original' in query:
                self.reads+=1
                if mode!='revoked' or self.reads==1:
                    row=(scope.original.artifact_id,scope.original.sha256,len(body),'photos','original','v1','image/jpeg')
            return SimpleNamespace(fetchone=lambda:row)
    connection=Connection()
    class Objects:
        calls=0
        def get_object(self,bucket,key,*,version_id):
            assert not connection.inside
            assert (bucket,key,version_id)==('photos','original','v1')
            self.calls+=1
            return BytesIO(body)
    objects=Objects()
    def run():
        return prepare_authorized_metadata_original(connection,objects,scope=scope,binding=binding,
            metadata_id=uuid4(),policy='copyright',allowed_bucket='photos',scratch_root=tmp_path,max_bytes=100000)
    if mode=='valid':
        result=run()
        assert result.scope==scope
        assert result.binding_jcs==canonical_bytes(binding)
        assert b'Artist' in result.prepared.source.metadata_jcs
        assert objects.calls==1 and connection.reads>=3
    else:
        with pytest.raises(AdoptionInvalid):run()
        assert objects.calls==0
    assert not list(tmp_path.iterdir())
