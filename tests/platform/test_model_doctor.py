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


def test_lraspp_doctor_requires_21_class_spatial_logits():
    assert smoke("lraspp_mobilenet_v3_large", Infer(np.zeros((21, 64, 64), np.float32)))["shape"] == [21, 64, 64]
    for shape in ((20, 64, 64), (21, 63, 64), (64, 64, 21)):
        with pytest.raises(ModelError, match='MODEL_OUTPUT_INVALID'):
            smoke("lraspp_mobilenet_v3_large", Infer(np.zeros(shape, np.float32)))


def test_mobile_sam_doctor_rejects_mismatched_candidate_inventory():
    class Prompt:
        def __init__(self, masks, scores):
            self.masks, self.scores = masks, scores

        def infer(self, *args):
            return self.masks, self.scores

    for masks, scores in (
        (np.zeros((1, 2, 16, 16), np.float32), np.ones((1,), np.float32)),
        (np.zeros((1, 1, 0, 16), np.float32), np.ones((1, 1), np.float32)),
        (np.zeros((17, 16, 16), np.float32), np.ones((17,), np.float32)),
    ):
        with pytest.raises(ModelError, match='MODEL_OUTPUT_INVALID'):
            smoke("mobilesam", Prompt(masks, scores))


def test_identity_doctor_uses_qualified_crop_and_normalized_embedding():
    class Identity:
        def __init__(self, embedding): self.embedding = embedding
        def infer(self, image):
            assert image.shape == (112, 112, 3)
            return self.embedding

    embedding = np.zeros((512,), np.float32)
    embedding[0] = 1
    assert smoke('identity512', Identity(embedding))['shape'] == [512]
    for invalid in (np.zeros((512,), np.float32), np.ones((512,), np.float32), np.zeros((511,), np.float32)):
        with pytest.raises(ModelError, match='MODEL_OUTPUT_INVALID'):
            smoke('identity512', Identity(invalid))


def test_capture_doctor_requires_scoped_probability_shapes():
    assert smoke('eye_open', Infer(np.array([0.2, 0.8], np.float32)))['shape'] == [2]
    assert smoke('glare_mask', Infer(np.zeros((64, 64), np.float32)))['shape'] == [64, 64]
    for model_id, invalid in (
        ('eye_open', np.zeros((64, 64), np.float32)),
        ('eye_open', np.array([0.0, 1.1], np.float32)),
        ('glare_mask', np.zeros((2,), np.float32)),
        ('glare_mask', np.full((64, 64), -0.1, np.float32)),
    ):
        with pytest.raises(ModelError, match='MODEL_OUTPUT_INVALID'):
            smoke(model_id, Infer(invalid))


def test_face_parse_doctor_uses_512_crop_and_19_logits():
    class FaceParse:
        def __init__(self, value): self.value = value
        def infer(self, crop):
            assert crop.shape == (512, 512, 3)
            return self.value

    assert smoke('bisenet_face19', FaceParse(np.zeros((512, 512, 19), np.float32)))['shape'] == [512, 512, 19]
    with pytest.raises(ModelError, match='MODEL_OUTPUT_INVALID'):
        smoke('bisenet_face19', FaceParse(np.zeros((512, 512, 18), np.float32)))


@pytest.mark.parametrize('model_id,scale', [('realesrgan_x2', 2), ('realesrgan_x4', 4)])
def test_upscale_doctor_requires_exact_scale_and_unit_rgb(model_id, scale):
    assert smoke(model_id, Infer(np.zeros((64 * scale, 64 * scale, 3), np.float32)))['shape'] == [64 * scale, 64 * scale, 3]
    for invalid in (np.zeros((64, 64, 3), np.float32),
                    np.full((64 * scale, 64 * scale, 3), 1.1, np.float32)):
        with pytest.raises(ModelError, match='MODEL_OUTPUT_INVALID'):
            smoke(model_id, Infer(invalid))


def test_embedding_segmentation_and_depth_doctor_shapes():
    vector = np.zeros((384,), np.float32)
    vector[0] = 1
    assert smoke('dinov2_vits14', Infer(vector))['shape'] == [384]
    assert smoke('segformer_b0_ade', Infer(np.zeros((150, 16, 16), np.float32)))['shape'] == [150, 16, 16]
    assert smoke('depth_anything_v2_small', Infer(np.zeros((32, 32), np.float32)))['shape'] == [32, 32]
    for model_id, invalid in (
        ('dinov2_vits14', np.zeros((384,), np.float32)),
        ('dinov2_vits14', np.ones((383,), np.float32)),
        ('segformer_b0_ade', np.zeros((149, 16, 16), np.float32)),
        ('segformer_b0_ade', np.zeros((150, 16), np.float32)),
        ('depth_anything_v2_small', np.zeros((1, 32, 32), np.float32)),
    ):
        with pytest.raises(ModelError, match='MODEL_OUTPUT_INVALID'):
            smoke(model_id, Infer(invalid))


def test_restoration_doctor_requires_working_rgb_geometry_and_range():
    good = np.zeros((64, 64, 3), np.float32)
    for model_id in ('scunet_color_real_psnr', 'big_lama'):
        assert smoke(model_id, Infer(good))['shape'] == [64, 64, 3]
        for invalid in (np.zeros((32, 32, 3), np.float32), np.full((64, 64, 3), -0.1, np.float32)):
            with pytest.raises(ModelError, match='MODEL_OUTPUT_INVALID'):
                smoke(model_id, Infer(invalid))


def test_generation_doctor_requires_exact_512_unit_rgb_and_known_model_id():
    class Generator:
        def __init__(self, value): self.value = value
        def generate(self, rgb, mask, **settings):
            assert rgb.shape == (512, 512, 3) and mask.shape == (512, 512)
            assert settings['steps'] == 25 and settings['scheduler'] == 'ddim_v1'
            return self.value

    good = np.zeros((512, 512, 3), np.float32)
    assert smoke('sd15_inpaint', Generator(good))['shape'] == [512, 512, 3]
    for invalid in (np.zeros((256, 256, 3), np.float32), np.full((512, 512, 3), 1.1, np.float32)):
        with pytest.raises(ModelError, match='MODEL_OUTPUT_INVALID'):
            smoke('sd15_inpaint', Generator(invalid))
    with pytest.raises(ModelError, match='MODEL_DOCTOR_UNSUPPORTED'):
        smoke('unregistered_model', Infer(good))
