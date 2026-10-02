from __future__ import annotations
from contextlib import contextmanager
import gc
from pathlib import Path
import threading
from .install import ModelError,verify_model
from .lock import ModelLock

class ModelPool:
    """One borrowed model at a time; no silent network or weight substitution."""
    def __init__(self,root:Path,lock:ModelLock,factories:dict):
        self.root=root;self.lock=ModelLock.model_validate_json(lock.model_dump_json());self.factories=dict(factories)
        self._mutex=threading.Lock();self._key=None;self._model=None

    def _evict(self):
        self._model=None;self._key=None;gc.collect()
        import torch
        if torch.cuda.is_available():torch.cuda.empty_cache()
        if torch.backends.mps.is_available():torch.mps.empty_cache()

    @contextmanager
    def acquire(self,model_id:str,backend:str,*,allow_unqualified:bool=False):
        # allow_unqualified is a doctor-only code path, never exposed in task params.
        entries=[m for m in self.lock.models if m.id==model_id]
        if len(entries)!=1 or model_id not in self.factories:raise ModelError('MODEL_UNAVAILABLE')
        entry=entries[0]
        if backend not in entry.backends or entry.backends[backend].status=='unsupported':raise ModelError('BACKEND_UNAVAILABLE')
        if not allow_unqualified and entry.backends[backend].status!='passed':raise ModelError('BACKEND_NOT_QUALIFIED')
        if not entry.approved_for_install:raise ModelError('MODEL_NOT_APPROVED')
        with self._mutex:
            key=(self.lock.lock_id,model_id,backend)
            if key!=self._key:
                self._evict()
                files=verify_model(self.root/self.lock.lock_id/model_id,entry)
                self._model=self.factories[model_id](entry,files,backend)
                self._key=key
            try:yield self._model
            except BaseException:
                self._evict()
                raise

    def close(self):
        with self._mutex:self._evict()
