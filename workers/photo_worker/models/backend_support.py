"""Native adapter support, independent from installed qualification evidence."""
from types import MappingProxyType

_ALL = frozenset({'cpu', 'cuda', 'mps'})
_ONNX = frozenset({'cpu', 'cuda'})
# Additions require an explicit adapter decision. Unknown models fail closed;
# a lock cannot create native support or authorize a silent CPU fallback.
MODEL_BACKENDS = MappingProxyType({
    'lraspp_mobilenet_v3_large': _ALL,
    'sfd': _ALL,
    'fan2d': _ALL,
    'identity512': _ONNX,
    'eye_open': _ONNX,
    'glare_mask': _ONNX,
    'reflection_rgb': _ONNX,
    'keypointrcnn_resnet50_fpn': _ALL,
    'dinov2_vits14': _ALL,
    'segformer_b0_ade': _ALL,
    'bisenet_face19': _ALL,
    'u2net': frozenset({'cpu'}),
    'mobilesam': _ALL,
    'depth_anything_v2_small': _ALL,
    'scunet_color_real_psnr': _ALL,
    'realesrgan_x2': _ALL,
    'realesrgan_x4': _ALL,
    'big_lama': _ALL,
    'sd15_inpaint': _ALL,
})


def model_backend_supported(model_id, backend):
    return backend in MODEL_BACKENDS.get(model_id, frozenset())
