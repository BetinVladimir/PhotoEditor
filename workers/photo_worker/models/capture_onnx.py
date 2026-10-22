"""Qualified V24 ONNX eye/glare interfaces with exact tensor contracts."""

import cv2
import numpy as np

from .install import ModelError
from .onnx_session import create_session


class ONNXCapture:
    def __init__(self, path, backend: str, family: str):
        if family not in ('eye_open', 'glare_mask') or backend not in ('cpu', 'cuda'):
            raise ModelError('BACKEND_UNAVAILABLE')
        try:
            import onnxruntime as ort
            provider = 'CUDAExecutionProvider' if backend == 'cuda' else 'CPUExecutionProvider'
            if provider not in ort.get_available_providers():
                raise ModelError('BACKEND_UNAVAILABLE')
            self.session = create_session(ort, path, backend)
            if self.session.get_providers() != [provider]:
                raise ModelError('BACKEND_UNAVAILABLE')
            inputs, outputs = self.session.get_inputs(), self.session.get_outputs()
            if len(inputs) != 1 or len(outputs) != 1:
                raise ModelError('MODEL_CONTRACT_ERROR')
            shape = inputs[0].shape
            if (len(shape) != 4 or shape[:2] != [1, 3] or type(shape[2]) is not int
                or not 32 <= shape[2] <= 2048 or shape[3] != shape[2]
                or inputs[0].name != 'image' or inputs[0].type != 'tensor(float)'):
                raise ModelError('MODEL_CONTRACT_ERROR')
            self.size, self.family = shape[2], family
            self.output_name = 'probabilities' if family == 'eye_open' else 'mask'
            expected = [1, 2] if family == 'eye_open' else [1, 1, self.size, self.size]
            if outputs[0].name != self.output_name or outputs[0].shape != expected or outputs[0].type != 'tensor(float)':
                raise ModelError('MODEL_CONTRACT_ERROR')
        except ModelError:
            raise
        except Exception as error:
            raise ModelError('MODEL_CONTRACT_ERROR') from error

    def infer(self, rgb):
        value = np.asarray(rgb)
        if (value.dtype != np.float32 or value.ndim != 3 or value.shape[2] != 3
            or min(value.shape[:2]) < 1 or value.shape[0] * value.shape[1] > 16_000_000
            or not np.isfinite(value).all() or np.any(value < 0) or np.any(value > 1)):
            raise ValueError('MODEL_INPUT_INVALID')
        height, width = value.shape[:2]
        scale = min(self.size / width, self.size / height)
        w, h = max(1, round(width * scale)), max(1, round(height * scale))
        left, top = (self.size - w) // 2, (self.size - h) // 2
        padded = np.zeros((self.size, self.size, 3), np.float32)
        padded[top:top+h, left:left+w] = cv2.resize(value, (w, h), interpolation=cv2.INTER_LINEAR)
        tensor = np.ascontiguousarray(padded.transpose(2, 0, 1)[None])
        result = np.asarray(self.session.run([self.output_name], {'image': tensor})[0])
        expected = (1, 2) if self.family == 'eye_open' else (1, 1, self.size, self.size)
        if (result.dtype != np.float32 or result.shape != expected or not np.isfinite(result).all()
            or np.any(result < 0) or np.any(result > 1)):
            raise ValueError('MODEL_OUTPUT_INVALID')
        if self.family == 'eye_open':
            return result[0].copy()
        cropped = result[0, 0, top:top+h, left:left+w]
        return cv2.resize(cropped, (width, height), interpolation=cv2.INTER_LINEAR)
