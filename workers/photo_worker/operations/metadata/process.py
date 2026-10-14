"""Lease-aware bounded-lifetime ExifTool execution, without captured output."""
import subprocess
from time import monotonic


def run_metadata_tool(args,check_active,*,timeout=120,popen=subprocess.Popen,clock=monotonic):
    if type(timeout) not in (int,float) or not 0<timeout<=120:
        raise ValueError('METADATA_TOOL_TIMEOUT')
    check_active()
    deadline=clock()+timeout
    process=popen(args,shell=False,stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,close_fds=True)
    try:
        while True:
            check_active()
            remaining=deadline-clock()
            if remaining<=0:raise subprocess.TimeoutExpired(args,timeout)
            try:
                code=process.wait(timeout=min(0.25,remaining))
            except subprocess.TimeoutExpired:
                continue
            check_active()
            return subprocess.CompletedProcess(args,code)
    finally:
        # Cancellation/lease loss/timeout cannot leave a writer mutating scratch.
        if process.poll() is None:
            try:process.kill()
            except ProcessLookupError:pass
        process.wait()
