"""Strict reflection_rgb tensor adapter; installation still needs a qualified lock."""
import cv2
import numpy as np

from photo_worker.runtime.executor import TaskCancelled
from .install import ModelError
from .onnx_session import create_session


class ONNXReflection:
    def __init__(self, path, backend: str):
        if backend not in ('cpu', 'cuda'):
            raise ModelError('BACKEND_UNAVAILABLE')
        try:
            import onnxruntime as ort
            provider = 'CUDAExecutionProvider' if backend == 'cuda' else 'CPUExecutionProvider'
            if provider not in ort.get_available_providers():
                raise ModelError('BACKEND_UNAVAILABLE')
            session = create_session(ort, path, backend)
            if session.get_providers() != [provider]:
                raise ModelError('BACKEND_UNAVAILABLE')
            inputs, outputs = session.get_inputs(), session.get_outputs()
            if len(inputs) != 1 or len(outputs) != 1:
                raise ModelError('MODEL_CONTRACT_ERROR')
            image, result = inputs[0], outputs[0]
            shape = image.shape
            if (image.name != 'image' or image.type != 'tensor(float)'
                    or not isinstance(shape, (tuple, list)) or len(shape) != 4
                    or list(shape[:2]) != [1, 3] or type(shape[2]) is not int
                    or not 128 <= shape[2] <= 2048 or shape[3] != shape[2]
                    or result.name != 'output' or result.type != 'tensor(float)'
                    or list(result.shape) != [1, 3, shape[2], shape[2]]):
                raise ModelError('MODEL_CONTRACT_ERROR')
            self.session, self.size = session, shape[2]
        except ModelError:
            raise
        except Exception as error:
            raise ModelError('MODEL_CONTRACT_ERROR') from error

    def infer(self, rgb, *, check_active=lambda: None):
        if not callable(check_active):
            raise ValueError('MODEL_INPUT_INVALID')
        def active():
            if check_active() is False:
                raise TaskCancelled('Reflection inference inactive')
        active()
        value = np.asarray(rgb)
        if (value.dtype != np.float32 or value.ndim != 3 or value.shape[2] != 3
                or min(value.shape[:2]) < 1 or value.shape[0]*value.shape[1] > 16_000_000
                or not np.isfinite(value).all() or np.any(value < 0) or np.any(value > 1)):
            raise ValueError('MODEL_INPUT_INVALID')
        height, width = value.shape[:2]
        scale = min(self.size / width, self.size / height)
        fitted_width = max(1, round(width * scale))
        fitted_height = max(1, round(height * scale))
        left = (self.size - fitted_width) // 2
        top = (self.size - fitted_height) // 2
        padded = np.zeros((self.size, self.size, 3), np.float32)
        active()
        padded[top:top+fitted_height, left:left+fitted_width] = cv2.resize(
            value, (fitted_width, fitted_height), interpolation=cv2.INTER_LINEAR)
        tensor = np.ascontiguousarray(padded.transpose(2, 0, 1)[None])
        active()
        try:
            values = self.session.run(['output'], {'image': tensor})
        except Exception as error:
            raise ModelError('MODEL_INFERENCE_ERROR') from error
        active()
        if not isinstance(values, list) or len(values) != 1:
            raise ValueError('MODEL_OUTPUT_INVALID')
        result = np.asarray(values[0])
        if (result.dtype != np.float32 or result.shape != (1, 3, self.size, self.size)
                or not np.isfinite(result).all() or np.any(result < 0) or np.any(result > 1)):
            raise ValueError('MODEL_OUTPUT_INVALID')
        cropped = result[0, :, top:top+fitted_height, left:left+fitted_width]
        proposal = cv2.resize(np.ascontiguousarray(cropped.transpose(1, 2, 0)),
                              (width, height), interpolation=cv2.INTER_LINEAR)
        active()
        if (proposal.dtype != np.float32 or proposal.shape != value.shape
                or not np.isfinite(proposal).all() or np.any(proposal < 0)
                or np.any(proposal > 1)):
            raise ValueError('MODEL_OUTPUT_INVALID')
        return proposal
