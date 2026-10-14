import pytest
from test_perspective_adapter import source,params
from photo_worker.contracts.base import canonical_hash
from photo_worker.operations.optical.perspective_source import verify_perspective_source_closure


@pytest.mark.parametrize('missing',[None,'WorkingImage','NpyArray','sha'])
def test_source_child_and_type_binding(missing):
    image=source();intent=params(image,1);intent.base.source.sha256=canonical_hash(image)
    class Artifacts:
        def declared_contract_refs(self,contract,maximum):
            if contract==missing:return ()
            return (intent.base.source,) if contract=='WorkingImage' else (image.pixels,)
    if missing=='sha':intent.base.source.sha256='0'*64
    if missing is None:verify_perspective_source_closure(Artifacts(),intent,image,None)
    else:
        with pytest.raises(ValueError):verify_perspective_source_closure(Artifacts(),intent,image,None)
