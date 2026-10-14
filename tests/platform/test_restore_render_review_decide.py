from datetime import datetime,timezone
from types import SimpleNamespace as NS
from uuid import uuid4
import ssl
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.runtime import restore_render_review_decide as m


@pytest.mark.parametrize('mode',['valid','foreign_review','foreign_run','wrong_revision','reject',
                                 'refused','extra_field','unknown_outcome','scope_changed','fixture_false','activity_false'])
def test_native_render_review_decision_is_one_shot_and_bound(monkeypatch,mode):
    tenant,child,review=uuid4(),uuid4(),uuid4()
    candidate=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    target=m.RestoreRenderReviewTarget('https://restore.test',tenant,child,review,3,
                                       candidate,'Bearer private')
    view={'id':str(review),'run_id':str(child),'kind':'render.compose','state':'decided',
          'revision':4,'candidate_hash':candidate.sha256,'decision':'accept',
          'decision_route':'candidate','reasons':{},
          'created_at':datetime.now(timezone.utc).isoformat()}
    if mode=='foreign_review':view['id']=str(uuid4())
    if mode=='foreign_run':view['run_id']=str(uuid4())
    if mode=='wrong_revision':view['revision']=5
    if mode=='reject':view['decision']='reject'
    if mode=='extra_field':view['extra']=True
    sent=[];closed=[];checks=0
    class Connection:
        def __init__(self,host,port,*,context,timeout):
            assert (host,port,timeout)==('restore.test',443,5)
        def request(self,method,path,body,headers):
            sent.append((method,path,body,headers))
            if mode=='unknown_outcome':raise OSError('socket lost')
        def getresponse(self):
            return NS(status=409 if mode=='refused' else 200,
                read=lambda size:canonical_bytes(view),
                getheader=lambda name,default=None:'application/json' if name=='Content-Type' else default)
        def close(self):closed.append(True)
    monkeypatch.setattr(m.http.client,'HTTPSConnection',Connection)
    def scope():
        nonlocal checks
        checks+=1
        if mode=='scope_changed' and checks==2:raise RuntimeError('fixture changed')
        if mode=='fixture_false':return False
    def run():return m.request_fixture_render_review_decision(target,
        tls_context=ssl.create_default_context(),assert_fixture_scope=scope,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':
        result=run();assert result.review_id==review and result.revision==4
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('fixture_false','activity_false'):
        assert sent==[] and closed==[]
        return
    assert len(sent)==1 and closed==[True]
    method,path,body,headers=sent[0]
    assert method=='POST' and path.endswith('/reviews/'+str(review)+'/decision')
    assert body==canonical_bytes({'expected_revision':3,'candidate_hash':'a'*64,'decision':'accept'})
    assert headers['Authorization']=='Bearer private'


def test_render_review_decision_rejects_untrusted_installation_before_post(monkeypatch):
    calls=[];candidate=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    monkeypatch.setattr(m.http.client,'HTTPSConnection',lambda *a,**k:calls.append(True))
    for origin,revision in [('http://restore.test',1),('https://restore.test/path',1),
                            ('https://restore.test',True)]:
        target=m.RestoreRenderReviewTarget(origin,uuid4(),uuid4(),uuid4(),revision,
                                           candidate,'Bearer private')
        with pytest.raises(ValueError):
            m.request_fixture_render_review_decision(target,tls_context=ssl.create_default_context(),
                assert_fixture_scope=lambda:None,check_active=lambda:None)
    assert calls==[]
