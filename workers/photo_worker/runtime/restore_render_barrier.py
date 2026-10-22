"""Explicit in-process barrier for isolated restore worker qualification only."""
from threading import Event,Lock
from time import monotonic
from uuid import UUID
from photo_worker.v2.render_task import ProfiledTaskRenderer
from .executor import OperationFailure,TaskCancelled


class RestoreBarrierRenderer(ProfiledTaskRenderer):
    """Keep real profiled admission/provider dispatch and executor heartbeats.

    Install explicitly via build_worker(render_renderer=...). No environment
    switch or production default enables this adapter. Cache hits bypass execute
    and therefore cannot satisfy the barrier witness.
    """
    def __init__(self,renderer,tenant_id,task_id,*,maximum_seconds):
        if (not isinstance(renderer,ProfiledTaskRenderer)
                or any(not isinstance(v,UUID) or v.int==0 for v in (tenant_id,task_id))
                or type(maximum_seconds) is not int or not 1<=maximum_seconds<=300):
            raise ValueError('RESTORE_RENDER_BARRIER_INSTALLATION')
        self._renderer=renderer;self._tenant=tenant_id;self._task=task_id
        self._maximum=maximum_seconds;self._lock=Lock();self._entered=Event();self._release=Event()
        self._generation=None;self._finished=Event()
        self._cancelled=Event()

    def __getattr__(self,name):return getattr(self._renderer,name)

    def verify_runtime(self):return self._renderer.verify_runtime()

    @property
    def provider_peak(self):return self._renderer.provider_peak

    def held_generation(self):
        with self._lock:
            if not self._entered.is_set() or self._release.is_set() or self._finished.is_set():
                raise ValueError('RESTORE_RENDER_BARRIER_NOT_HELD')
            return self._generation

    def release(self):self._release.set()

    def assert_held(self,witness):
        if ((witness.tenant_id,witness.task_id)!=(self._tenant,self._task)
                or self.held_generation()!=(witness.attempt,witness.fence)):
            raise ValueError('RESTORE_RENDER_BARRIER_GENERATION')

    def assert_cancelled(self,witness):
        with self._lock:
            if ((witness.tenant_id,witness.task_id)!=(self._tenant,self._task)
                    or self._generation!=(witness.attempt,witness.fence)
                    or not self._finished.is_set() or not self._cancelled.is_set() or self._release.is_set()):
                raise ValueError('RESTORE_RENDER_BARRIER_NOT_CANCELLED')

    def execute(self,ctx,artifacts,inputs,*,authoritative_inputs):
        command=ctx.lease.command
        if (command.tenant_id,command.task_id)!=(self._tenant,self._task):
            # The embedded worker also runs the other installed fixture tasks.
            # Only the exact revoke target is held; ordinary work retains the
            # same real admission/provider execution and never obtains a witness.
            return self._renderer.execute(ctx,artifacts,inputs,authoritative_inputs=authoritative_inputs)
        with self._lock:
            if self._entered.is_set():raise OperationFailure('CONFIG_INVALID')
            self._generation=(command.attempt,ctx.lease.fence)
            self._entered.set()
        deadline=monotonic()+self._maximum
        def require_active():
            if ctx.check_active() is False:
                raise TaskCancelled('Restore barrier inactive')
        try:
            while not self._release.is_set():
                require_active()
                if monotonic()>=deadline:raise OperationFailure('DEPENDENCY_UNAVAILABLE',retryable=True)
                self._release.wait(min(.05,max(0,deadline-monotonic())))
            require_active()
            if monotonic()>=deadline:raise OperationFailure('DEPENDENCY_UNAVAILABLE',retryable=True)
            return self._renderer.execute(ctx,artifacts,inputs,authoritative_inputs=authoritative_inputs)
        except TaskCancelled:
            self._cancelled.set()
            raise
        finally:self._finished.set()
