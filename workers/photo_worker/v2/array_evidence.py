"""Budgeted, hash-verified disk mappings for two-phase adoption proofs."""
from contextlib import ExitStack
from hashlib import sha256
from pathlib import Path
from tempfile import TemporaryDirectory
from photo_worker.runtime.arrays import read_array,read_alpha_array,read_inverse_field
from .adoption import AdoptionInvalid


class ArrayEvidenceSession:
    def __init__(self, reader, scratch_root: Path, max_bytes: int, check_active=lambda:None):
        if type(max_bytes) is not int or not 1<=max_bytes<=2**53-1:
            raise ValueError('ADOPTION_SCRATCH_INVALID')
        self.reader,self.root,self.limit,self.check=reader,Path(scratch_root),max_bytes,check_active
        self.used=0;self.cache={};self.stack=None;self.directory=None

    def __enter__(self):
        if self.stack is not None:raise ValueError('ADOPTION_SESSION_ALREADY_OPEN')
        self.check();self.stack=ExitStack()
        try:
            self.directory=Path(self.stack.enter_context(TemporaryDirectory(prefix='adoption-',dir=self.root)))
            return self
        except BaseException:
            self.stack.close();self.stack=None;raise

    def __exit__(self,*args):
        try:return self.stack.__exit__(*args)
        finally:self.stack=None;self.directory=None;self.cache.clear()

    def array(self,ref,shape,*,source,role=None,unit=False,legacy_alpha=False,legacy_input_npy=False,inverse_field=False):
        self.check()
        if type(inverse_field) is not bool or inverse_field and (source not in ('input','output') or source=='output' and role!='warp_field' or len(shape)!=3 or shape[2]!=2 or unit or legacy_alpha):
            raise AdoptionInvalid('ARRAY_FIELD_POLICY_INVALID')
        if type(legacy_input_npy) is not bool or legacy_input_npy and source!='input':
            raise AdoptionInvalid('ARRAY_MEDIA_POLICY_INVALID')
        if type(legacy_alpha) is not bool or legacy_alpha and (source!="input" or len(shape)!=2 or not unit):
            raise AdoptionInvalid("ARRAY_ALPHA_POLICY_INVALID")
        if self.stack is None:raise ValueError('ADOPTION_SESSION_CLOSED')
        evidence=self.reader.evidence(ref)
        if (evidence.ref!=ref or evidence.source!=source or source not in ('input','output')
                or role is not None and evidence.role!=role
                or (evidence.media_type!='application/x-npy' and not (legacy_input_npy and evidence.media_type=='application/octet-stream'))
                or type(evidence.bytes) is not int or evidence.bytes<1):
            raise AdoptionInvalid('ARRAY_EVIDENCE_INVALID')
        key=(ref,tuple(shape),source,role,unit,legacy_alpha,legacy_input_npy,inverse_field)
        if key in self.cache:return self.cache[key]
        # Reserve before reading; even failed materialization consumes this session's budget.
        if evidence.bytes>self.limit-self.used:raise AdoptionInvalid('ADOPTION_SCRATCH_EXCEEDED')
        self.used+=evidence.bytes
        path=self.directory/(str(len(self.cache))+'-'+str(self.used)+'.npy')
        digest=sha256();count=0
        iterator=iter(self.reader.iter_bytes(ref,evidence.bytes))
        try:
            with path.open('xb') as target:
                for block in iterator:
                    self.check()
                    if not isinstance(block,bytes) or not 1<=len(block)<=65536 or count+len(block)>evidence.bytes:
                        raise AdoptionInvalid('ARRAY_STREAM_INVALID')
                    target.write(block);digest.update(block);count+=len(block)
            if count!=evidence.bytes or digest.hexdigest()!=ref.sha256:
                raise AdoptionInvalid('ARRAY_BYTES_INVALID')
            mapped=self.stack.enter_context(read_alpha_array(path,tuple(shape),self.check) if legacy_alpha
                else read_inverse_field(path,tuple(shape[:2]),self.check) if inverse_field
                else read_array(path,tuple(shape),'float32',self.check,unit_interval=unit))
            self.cache[key]=mapped
            return mapped
        except BaseException:
            path.unlink(missing_ok=True);raise
        finally:
            close=getattr(iterator,'close',None)
            if close is not None:close()

    def alpha(self,ref,shape,*,mode):
        """Normalize only immutable input alpha under an explicit caller policy.

        Output alpha must still use array(..., source='output', unit=True),
        whose float32 validator cannot accept a legacy encoding.
        """
        import numpy as np
        if mode not in ('require_float32','normalize_uint16'):
            raise AdoptionInvalid('ARRAY_ALPHA_POLICY_INVALID')
        self.check()
        if self.stack is None:raise ValueError('ADOPTION_SESSION_CLOSED')
        if ref is None:return None
        raw=self.array(ref,shape,source='input',unit=True,legacy_alpha=mode=='normalize_uint16')
        if raw.dtype==np.float32:return raw
        key=('normalized_alpha',ref,tuple(shape))
        if key in self.cache:return self.cache[key]
        normalized=self.allocate_mask(shape)
        for y in range(0,shape[0],512):
            for x in range(0,shape[1],512):
                self.check()
                part=np.s_[y:y+512,x:x+512]
                normalized[part]=raw[part].astype(np.float32)/np.float32(65535)
        normalized.flush();self.check()
        normalized.flags.writeable=False
        self.cache[key]=normalized
        return normalized

    def allocate_mask(self,shape):
        import numpy as np
        from uuid import uuid4
        self.check()
        if self.stack is None or len(shape)!=2 or any(type(n) is not int or n<1 for n in shape) or shape[0]*shape[1]>200000000:
            raise ValueError('ADOPTION_MASK_SHAPE_INVALID')
        size=shape[0]*shape[1]*4+4096
        if size>self.limit-self.used:raise AdoptionInvalid('ADOPTION_SCRATCH_EXCEEDED')
        self.used+=size
        mapped=np.lib.format.open_memmap(self.directory/(uuid4().hex+'.npy'),mode='w+',dtype='<f4',shape=shape)
        self.stack.callback(mapped._mmap.close)
        return mapped
