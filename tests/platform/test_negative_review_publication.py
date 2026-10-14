from contextlib import nullcontext
from hashlib import sha256
from io import BytesIO
from uuid import uuid4

import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.negative_review_publication import NegativeReviewPublisher


class Row:
    def __init__(self, value): self.value = value
    def fetchall(self): return self.value
    def fetchone(self): return self.value


@pytest.mark.parametrize('mode', ['valid', 'hash', 'truncated', 'wrong_bucket', 'stale'])
def test_negative_review_publication_binds_physical_params(mode):
    source = uuid4()
    raw = canonical_bytes(dict(schema_version=1, base=dict(asset_version_id=str(uuid4()),
        canvas_hash='c'*64, source=dict(artifact_id=str(source),sha256='a'*64)),
        strength=1,film_base_rgb=[.8,.5,.2],calibration=dict(artifact_id=str(uuid4()),sha256='b'*64),exposure_ev=0))
    tenant, task, root, candidate, review = [uuid4() for _ in range(5)]
    digest=sha256(raw).hexdigest()
    rows=[(tenant,task,root,candidate,digest,'other' if mode=='wrong_bucket' else 'photos','params','v1',len(raw))]
    calls=[]
    class Read:
        def execute(self, query, args): return Row(rows)
    class Write:
        def transaction(self): return nullcontext()
        def execute(self,query,args):
            calls.append(args)
            return Row((None if mode=='stale' else review,))
    class Response(BytesIO):
        released=False
        def read(self,n=-1):return super().read(min(n,7))
        def release_conn(self): self.released=True
    class Objects:
        response=None
        def get_object(self,bucket,key,*,version_id):
            assert (bucket,key,version_id)==('photos','params','v1')
            body=raw if mode not in ('hash','truncated') else raw[:-1]
            if mode=='hash':body+=b'x'
            self.response=Response(body);return self.response
    objects=Objects()
    publisher=NegativeReviewPublisher(Read(),Write(),objects,'photos',check_active=lambda:None)
    published,retries=publisher.process_pending()
    assert (published,retries)==((1,0) if mode=='valid' else (0,1))
    if mode in ('valid','stale'):
        assert calls==[(tenant,task,root,candidate,source,raw.decode('utf-8'))]
    else: assert not calls
    if objects.response is not None:assert objects.response.closed and objects.response.released
