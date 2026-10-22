"""Exact installation response only; not runtime qualification."""
import pytest
from photo_worker.runtime.database import TaskDatabase

@pytest.mark.parametrize('operation',['editor.effects_seal','editor.layers_precompose'])
@pytest.mark.parametrize('row,ready',[(None,False),((False,),False),((None,),False),((True,),True),((1,),False),((True,False),False)])
def test_editor_installed_authority_probe_is_exact(operation,row,ready):
    database=object.__new__(TaskDatabase);calls=[]
    database._query=lambda query,args:calls.append((query,args)) or row
    assert database.editor_internal_dispatch_installed(operation) is ready
    assert calls==[('SELECT photo.editor_internal_dispatch_installed(%s)',(operation,))]

def test_editor_installation_probe_refuses_unknown_without_io():
    database=object.__new__(TaskDatabase)
    database._query=lambda *args:pytest.fail('unexpected IO')
    assert database.editor_internal_dispatch_installed('render.compose') is False
