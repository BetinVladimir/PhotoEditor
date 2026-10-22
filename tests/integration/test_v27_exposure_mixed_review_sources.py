"""Own original exposure review prerequisites for mixed Apply; unexecuted."""
import json
import pytest
from exposure_review_documents_fixture import prepared_exposure_reviews


@pytest.mark.parametrize('count,alpha,dense', [(1,False,0),(2,False,0),(3,True,0),(3,True,1)])
def test_exposure_mixed_review_retains_each_original_proposal(db,count,alpha,dense):
    reviews = prepared_exposure_reviews(db,target_count=count,with_alpha=alpha,dense_warps=dense)
    first_args,parent,scope,_ = reviews[0]
    common = json.loads(first_args[5])
    seen = set()
    for args,own_parent,own,target in reviews:
        params = json.loads(args[5])
        assert own_parent == parent and args[:4] == first_args[:4]
        assert target not in seen
        seen.add(target)
        assert params['plan'] == common['plan'] and params['intent'] == common['intent']
        assert own['review_documents'] == scope['review_documents']
        assert json.loads(own['candidate_body'])['target'] == params['source']
        intent = json.loads(own['intent_body'])
        assert intent['kind'] == 'local_exposure' and intent['ev'] == .5
        assert db.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE tenant_id=%s AND id=%s',
                          (args[0],target)).fetchone() == ('proposed',None,None)
        current = (args[0],args[1],target,args[2],args[3],int(params['workflow_revision']),
                   int(params['target_revision']),params['plan']['sha256'],params['candidate']['sha256'])
        assert db.execute('SELECT photo.semantic_preapply_preview_current('+','.join(['%s']*9)+')',current).fetchone()[0] is True
    assert len(seen) == count
