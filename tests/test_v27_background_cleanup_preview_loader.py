"""Real NPY source-to-private-compute scenarios; no installed route or GPU proof."""
from hashlib import sha256
from types import SimpleNamespace

import numpy as np
import pytest

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.operations.semantic.preview_background_cleanup_loader import load_background_cleanup_preview_arrays
from photo_worker.operations.semantic.preview_background_cleanup import execute_artifact_background_cleanup_preview
from test_v27_preview_loader import setup
from test_v27_removal_preview import Engine


def background_documents(tmp_path):
    params, artifacts, events = setup(tmp_path)
    intent_id = params['intent']['artifact_id']
    intent = parse_json(artifacts.contract_bytes(SimpleNamespace(artifact_id=intent_id),None))
    intent.update(kind='background_cleanup',ev=None)
    raw = canonical_bytes(intent)
    params['intent']['sha256'] = sha256(raw).hexdigest()
    params.update(kind='background_cleanup',model_id='big_lama',context_pixels=128,feather_pixels=8)
    class Documents:
        input = artifacts.input
        def contract_bytes(self,ref,model):
            return raw if str(ref.artifact_id)==intent_id else artifacts.contract_bytes(ref,model)
    return params, Documents(), events


@pytest.mark.parametrize('failure',[None,'admission','origin','color','budget'])
def test_background_loader_owns_arrays_and_denies_before_array_io(tmp_path,failure):
    params, documents, events = background_documents(tmp_path)
    seen=[]
    def admission(value):
        seen.append(value.kind)
        return failure!='admission'
    options=dict(verify_admission=admission,verify_origin=lambda *args:failure!='origin',
        verify_color=lambda value:failure!='color',
        max_bytes=64*1048576 if failure=='budget' else 128*1048576,check_active=lambda:None)
    if failure:
        with pytest.raises(ValueError):
            load_background_cleanup_preview_arrays(documents,params,**options)
        assert events==[]
    else:
        result=load_background_cleanup_preview_arrays(documents,params,**options)
        np.testing.assert_array_equal(result.original,np.full((8,8,3),.5,np.float32))
        assert all(value.flags.owndata for value in (result.original,result.mask,result.protected))
        assert events==['open','open','open','close','close','close']
    assert seen and set(seen)=={'background_cleanup'}


@pytest.mark.parametrize('revoked',[False,True])
def test_artifact_background_preview_uses_verified_source_and_closes_mappings(tmp_path,revoked):
    params, documents, events = background_documents(tmp_path)
    engine=Engine()
    options=dict(verify_admission=lambda _:not(revoked and 'close' in events),
        verify_origin=lambda *args:True,verify_color=lambda _:True,
        max_bytes=1024**3,check_active=lambda:None)
    if revoked:
        with pytest.raises(ValueError,match='ADMISSION_DENIED'):
            execute_artifact_background_cleanup_preview(documents,params,engine,**options)
        assert engine.calls==0
    else:
        result=execute_artifact_background_cleanup_preview(documents,params,engine,**options)
        assert engine.calls==1 and result.preservation.changed_pixels>0
        assert result.original.shape==result.candidate.shape==(8,8,3)
    assert events==['open','open','open','close','close','close']


@pytest.mark.parametrize('operation',['background_cleanup','remove_object'])
def test_artifact_inpaint_preview_denies_before_any_document_io(operation):
    from test_v27_background_cleanup_preview import background_fixture
    from test_v27_removal_preview_binding import removal_fixture
    from photo_worker.operations.semantic.preview_removal import execute_artifact_removal_preview
    params,_,_=background_fixture() if operation=='background_cleanup' else removal_fixture()
    execute=execute_artifact_background_cleanup_preview if operation=='background_cleanup' else execute_artifact_removal_preview
    class Forbidden:
        def contract_bytes(self,*args):
            raise AssertionError('document IO before admission')
    engine=Engine()
    with pytest.raises(ValueError,match='ADMISSION_DENIED'):
        execute(Forbidden(),params,engine,verify_admission=lambda _:False,
            verify_origin=lambda *args:True,verify_color=lambda _:True,
            max_bytes=1024**3,check_active=lambda:None)
    assert engine.calls==0
