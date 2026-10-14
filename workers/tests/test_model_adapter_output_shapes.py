"""Model adapters refuse malformed GPU output before copying to host."""
import pytest

from photo_worker.models.adapters import (
    TorchScriptFAN, TorchScriptSFD, TorchvisionKeypointRCNN,
    TransformersSegformer,
)


class TensorStub:
    def __init__(self, shape, floating=True):
        self.shape=shape
        self.floating=floating
        self.copied=False

    def is_floating_point(self):
        return self.floating

    def detach(self):
        self.copied=True
        return self


@pytest.mark.parametrize('adapter,shape', [
    (TorchScriptSFD,(100001,5)), (TorchScriptSFD,(1,100001,5)),
    (TorchScriptSFD,(10,6)), (TorchScriptFAN,(69,3)),
    (TorchScriptFAN,(1,68,4)),
    (TransformersSegformer,(1,150,0,512)),
    (TransformersSegformer,(1,150,1025,512)),
    (TransformersSegformer,(1,149,512,512)),
])
def test_invalid_device_output_does_not_transfer(adapter,shape):
    output=TensorStub(shape)
    check=(adapter._bounded_logits if adapter is TransformersSegformer
           else adapter._bounded_output)
    with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
        check(output).detach()
    assert not output.copied


@pytest.mark.parametrize('adapter,shape', [
    (TorchScriptSFD,(0,5)), (TorchScriptSFD,(1,100000,5)),
    (TorchScriptFAN,(68,3)), (TorchScriptFAN,(1,68,3)),
    (TransformersSegformer,(1,150,512,512)),
])
def test_valid_device_shape_is_retained(adapter,shape):
    output=TensorStub(shape)
    check=(adapter._bounded_logits if adapter is TransformersSegformer
           else adapter._bounded_output)
    assert check(output) is output


def _pose_fields(count):
    return {'scores':TensorStub((count,)), 'boxes':TensorStub((count,4)),
            'keypoints':TensorStub((count,17,3)),
            'keypoints_scores':TensorStub((count,17))}


def test_pose_output_checks_all_fields_before_any_host_copy():
    valid=_pose_fields(3)
    assert TorchvisionKeypointRCNN._bounded_pose_fields(valid)==valid
    for name,shape in [('scores',(10001,)), ('boxes',(3,5)),
                       ('keypoints',(3,18,3)),
                       ('keypoints_scores',(2,17))]:
        fields=_pose_fields(3)
        fields[name]=TensorStub(shape)
        with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
            TorchvisionKeypointRCNN._bounded_pose_fields(fields)
        assert not any(value.copied for value in fields.values())
    fields=_pose_fields(3)
    fields['keypoints'].floating=False
    with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
        TorchvisionKeypointRCNN._bounded_pose_fields(fields)
