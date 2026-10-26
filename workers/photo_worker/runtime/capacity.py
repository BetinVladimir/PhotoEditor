"""CPU registration liveness and conservative host capacity reporting."""
from __future__ import annotations
from dataclasses import dataclass
from pathlib import Path
import shutil
from threading import Event
from time import monotonic
from typing import Callable
import psutil

@dataclass(frozen=True)
class Capacity:
    ram_bytes:int
    scratch_bytes:int
    vram_bytes:int=0


def measure_cpu(root:Path)->Capacity:
    ram=psutil.virtual_memory().available
    disk=shutil.disk_usage(root).free
    if any(type(value) is not int or not 0<=value<=2**53-1 for value in (ram,disk)):
        raise ValueError('Invalid measured capacity')
    return Capacity(ram,max(0,disk-64*1024*1024))


def measure_accelerator(root:Path,backend:str)->Capacity:
    """Single visible device per physical node; never sum independent GPUs.

    Metal uses unified memory. Divide currently available host memory between
    RAM and VRAM so reporting cannot count the same bytes twice. Driver memory
    includes cached Metal allocations; current tensor allocations alone do not.
    """
    host=measure_cpu(root)
    if backend=='cpu':return host
    import torch
    def count(value):
        if type(value) is not int or not 0<=value<=2**53-1:raise ValueError('Invalid device capacity')
        return value
    if backend=='cuda':
        if not torch.cuda.is_available() or torch.cuda.device_count()!=1:raise ValueError('Exactly one visible CUDA device required')
        free,total=torch.cuda.mem_get_info(0)
        free,total=count(free),count(total)
        if free>total:raise ValueError('Invalid device capacity')
        return Capacity(host.ram_bytes,host.scratch_bytes,max(0,free-64*1024*1024))
    if backend=='mps':
        if not torch.backends.mps.is_available():raise ValueError('MPS unavailable')
        recommended=count(torch.mps.recommended_max_memory())
        allocated=count(torch.mps.driver_allocated_memory())
        shared=max(0,host.ram_bytes-64*1024*1024)
        half=shared//2
        vram=min(half,max(0,recommended-allocated-64*1024*1024))
        return Capacity(half,host.scratch_bytes,vram)
    raise ValueError('Unknown worker backend')


class RegistrationRejected(RuntimeError):
    """Server explicitly refused the worker: always fail closed."""


# W-2: transient control-plane outages (VPN blip, DB restart) are tolerated for
# this long since the last successful report before the worker aborts.
CAPACITY_OUTAGE_GRACE_SECONDS = 90.0


class CapacityReporter:
    def __init__(self,report:Callable[[Capacity],bool],sample:Callable[[],Capacity],abort:Callable[[],None],*,interval_seconds:float=10):
        if not 0<interval_seconds<=10:raise ValueError('Invalid capacity report interval')
        self.report,self.sample,self.abort=report,sample,abort
        self.interval=interval_seconds
        self.failed=Event()
        self.grace=CAPACITY_OUTAGE_GRACE_SECONDS

    def report_once(self):
        if self.report(self.sample()) is not True:raise RegistrationRejected('Worker registration rejected')

    def run(self,stop:Event):
        # Startup executes report_once synchronously before consuming any message.
        last_ok=monotonic()
        while not stop.wait(self.interval):
            try:
                self.report_once()
                last_ok=monotonic()
                continue
            except Exception as exc:
                if not isinstance(exc,RegistrationRejected) and monotonic()-last_ok<self.grace:
                    try:
                        from photo_worker.runtime.diagnostics import emit_failure_metadata
                        emit_failure_metadata(exc, 'DEPENDENCY_UNAVAILABLE', 'capacity_retry')
                    except BaseException:
                        pass
                    continue
                self.failed.set()
                stop.set()
                try:
                    from photo_worker.runtime.diagnostics import emit_failure_metadata
                    emit_failure_metadata(exc, 'INTERNAL_ERROR', 'heartbeat')
                except BaseException:
                    # Diagnostics must never prevent fail-closed shutdown.
                    pass
                self.abort()
                return
