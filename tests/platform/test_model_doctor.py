import numpy as np
import pytest

from photo_worker.models.doctor import smoke
from photo_worker.models.install import ModelError


class Infer:
    def __init__(self, value): self.value=value
    def infer(self, *args): return self.value


def test_generic_and_shape_specific_smoke_are_finite():
    assert smoke("scunet_color_real_psnr", Infer(np.zeros((64,64,3),np.float32)))["shape"] == [64,64,3]
    assert smoke("fan2d", Infer(np.zeros((68,3),np.float32)))["shape"] == [68,3]
    assert smoke("sfd", Infer(np.zeros((0,5),np.float32)))["shape"] == [0,5]


def test_doctor_rejects_nonfinite_or_wrong_adapter_output():
    with pytest.raises(ModelError): smoke("fan2d", Infer(np.zeros((67,3),np.float32)))
    with pytest.raises(ModelError): smoke("scunet_color_real_psnr", Infer(np.full((2,2,3),np.nan,np.float32)))


def test_mobile_sam_smoke_uses_prompt_adapter_contract():
    class Prompt:
        def infer(self,rgb,points,labels,box):
            assert rgb.shape==(1024,1024,3) and points.shape==(1,2) and labels.tolist()==[1]
            return np.zeros((1,1,256,256),np.float32),np.ones((1,1),np.float32)
    assert smoke("mobilesam",Prompt())["shape"] == [65537]
