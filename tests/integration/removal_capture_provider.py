"""Explicit deterministic test engine, never installed-model qualification."""
from contextlib import contextmanager
from types import SimpleNamespace
import numpy as np
import re


def controlled_pool(model_lock_sha):
    if not isinstance(model_lock_sha,str) or re.fullmatch(r'[0-9a-f]{64}',model_lock_sha) is None:
        raise ValueError('controlled fixture requires exact frozen lock identity')
    class Engine:
        def infer(self,image,mask):
            if (not isinstance(image,np.ndarray) or not isinstance(mask,np.ndarray)
                    or image.dtype!=np.float32 or image.ndim!=3 or image.shape[2]!=3
                    or mask.dtype!=np.float32 or mask.shape!=image.shape[:2]):
                raise ValueError('controlled model input invalid')
            # Model-boundary double only. Production ROI/delta/protection/ICC,
            # evidence/media staging, SQL admission and adoption remain real.
            return np.where(mask[...,None]>0,np.minimum(image+np.float32(.05),1),image).astype(np.float32)
    class Pool:
        lock=SimpleNamespace(lock_id=model_lock_sha)
        @contextmanager
        def acquire(self,model_id,backend):
            if model_id!='big_lama' or backend!='cpu':
                raise ValueError('controlled provider only supports explicit big_lama CPU fixture')
            yield Engine()
    return Pool()
