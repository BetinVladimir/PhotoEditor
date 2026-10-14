"""Bounded NVML system version queries; CUDA build is not driver identity."""
import ctypes
from ctypes.util import find_library
import platform
import re
from photo_worker.runtime.executor import OperationFailure


def load_nvml():
    try:
        if platform.system()=='Windows':return ctypes.CDLL('nvml.dll',winmode=0x00000800)
        if platform.system()=='Linux':return ctypes.CDLL(find_library('nvidia-ml') or 'libnvidia-ml.so.1')
    except OSError as error:
        raise OperationFailure('PERSON_DRIVER_MEASUREMENT_UNAVAILABLE') from error
    raise OperationFailure('PERSON_DRIVER_MEASUREMENT_UNAVAILABLE')


def measure_nvidia_driver():
    library=load_nvml()
    try:
        init=library.nvmlInit_v2;shutdown=library.nvmlShutdown
        init.argtypes=[];shutdown.argtypes=[]
        init.restype=shutdown.restype=ctypes.c_int
        if init()!=0:raise OperationFailure('PERSON_DRIVER_MEASUREMENT_UNAVAILABLE')
        try:
            values={}
            for key,name in (('driver_version','nvmlSystemGetDriverVersion'),('nvml_version','nvmlSystemGetNVMLVersion')):
                query=getattr(library,name)
                query.argtypes=[ctypes.POINTER(ctypes.c_char),ctypes.c_uint]
                query.restype=ctypes.c_int
                output=ctypes.create_string_buffer(80)
                if query(output,80)!=0:raise OperationFailure('PERSON_DRIVER_MEASUREMENT_UNAVAILABLE')
                value=output.value.decode('ascii')
                if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]{0,78}',value):
                    raise OperationFailure('PERSON_DRIVER_MEASUREMENT_UNAVAILABLE')
                values[key]=value
            return values
        finally:
            if shutdown()!=0:raise OperationFailure('PERSON_DRIVER_MEASUREMENT_UNAVAILABLE')
    except (AttributeError,UnicodeError,ValueError) as error:
        raise OperationFailure('PERSON_DRIVER_MEASUREMENT_UNAVAILABLE') from error
