import pytest
from photo_worker.operations.export.memory import admit_presentation,presentation_ram_bytes
from photo_worker.runtime.executor import OperationFailure


def test_presentation_admission_uses_both_canvases_and_alpha():
    opaque=presentation_ram_bytes(100,100,50,50,alpha=False)
    translucent=presentation_ram_bytes(100,100,50,50,alpha=True)
    assert translucent>opaque
    assert admit_presentation({'resource_estimate':{'ram_bytes':translucent}},100,100,50,50,alpha=True)==translucent
    with pytest.raises(OperationFailure):admit_presentation({'resource_estimate':{'ram_bytes':translucent-1}},100,100,50,50,alpha=True)
    with pytest.raises(OperationFailure):admit_presentation({},10000,10000,10000,10000,alpha=True)
    with pytest.raises(OperationFailure):admit_presentation({'resource_estimate':{'ram_bytes':True}},100,100,50,50,alpha=True)
    with pytest.raises(ValueError):presentation_ram_bytes(200001,1000,100,100,alpha=False)
