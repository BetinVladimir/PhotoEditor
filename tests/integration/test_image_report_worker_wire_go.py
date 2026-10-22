"""Non-DB canonical ImageReport wire: actual repo Python model and exact Go root."""
import json
import os
from pathlib import Path
import signal
import subprocess
import time
from hashlib import sha256
from uuid import uuid4

ROOT = Path(__file__).resolve().parents[2]
NAME = 'TestImageReportWorkerWire'
PACKAGE = 'photoeditor/backend/internal/v2draft'


def _output_text(value):
    return value.decode(errors='replace') if isinstance(value, bytes) else value


def test_image_report_worker_wire_go():
    python = ROOT/'.venv-platform/bin/python'
    assert python.is_file(), 'Repository Python runtime is required; never skip wire coverage'
    paths = {name: ROOT/path for name, path in {
        'GOCACHE': 'work/go-cache', 'GOMODCACHE': '.cache/go-mod',
        'GOPATH': 'work/go-path', 'TMPDIR': 'work/tmp',
    }.items()}
    for path in paths.values():
        path.mkdir(parents=True, exist_ok=True)
    # No inherited database/objectstore/qualification/runtime fixture can activate another lane.
    env = {key: value for key, value in os.environ.items() if not key.startswith('PHOTO_')}
    env.update({key: str(path) for key, path in paths.items()})
    env.update(PHOTO_WORKER_PYTHON=str(python), GOMAXPROCS='2', GOPROXY='off',
               PYTHONDONTWRITEBYTECODE='1', GOTOOLCHAIN='local')
    command = ['go', 'test', '-json', '-p', '1', './internal/v2draft',
               '-run', '^'+NAME+'$', '-count=1', '-timeout=90s']
    directory = ROOT/'work/specialized-image-report-wire-evidence'/uuid4().hex
    directory.mkdir(parents=True)
    metadata = dict(command=command, cwd='backend', python='.venv-platform/bin/python',
                    timeout_seconds=120, go_timeout_seconds=90,
                    source_sha256={str(path.relative_to(ROOT)): sha256(path.read_bytes()).hexdigest()
                                   for path in (Path(__file__), ROOT/'backend/internal/v2draft/image_report_test.go',
                                                ROOT/'workers/photo_worker/operations/optical/contracts.py')})
    phases=[]
    stdout=stderr=''
    timed_out=False
    cleanup_complete=True
    process=None
    owned_pgid=None
    launch_error=None
    def remember_timeout(error):
        nonlocal stdout,stderr
        # communicate retries return cumulative output; do not concatenate it.
        if error.stdout is not None:stdout=_output_text(error.stdout)
        if error.stderr is not None:stderr=_output_text(error.stderr)
    def signal_owned_group(sig,phase):
        nonlocal cleanup_complete
        assert owned_pgid==process.pid and owned_pgid>1
        try:
            os.killpg(owned_pgid,sig)
        except ProcessLookupError:
            phases.append(dict(phase=phase,signal=sig.name,outcome='group_absent'))
        except OSError as error:
            cleanup_complete=False
            phases.append(dict(phase=phase,signal=sig.name,outcome='signal_error',errno=error.errno))
        else:
            phases.append(dict(phase=phase,signal=sig.name,outcome='sent'))
    interrupted = None
    def cleanup_after_interrupt(original):
        nonlocal stdout, stderr, cleanup_complete
        # Keep the first exception; a second Ctrl-C/cleanup error must not skip
        # the kill attempt or replace the original pytest interruption.
        def record_cleanup_error(phase, error):
            nonlocal cleanup_complete
            cleanup_complete=False
            phases.append(dict(phase=phase,outcome='cleanup_error',error_type=type(error).__name__))
            original.add_note('Owned Go group cleanup error during '+phase+': '+type(error).__name__)
        try:
            signal_owned_group(signal.SIGINT,'cancel_interrupt_owned_group')
            out,err=process.communicate(timeout=5)
            stdout,stderr=_output_text(out),_output_text(err)
            phases.append(dict(phase='cancel_interrupt_grace',outcome='completed',returncode=process.returncode))
        except subprocess.TimeoutExpired as error:
            remember_timeout(error)
            phases.append(dict(phase='cancel_interrupt_grace',outcome='timeout',seconds=5))
        except BaseException as error:
            record_cleanup_error('cancel_interrupt_grace',error)
        finally:
            try:
                # Descendants may survive a completed group leader.
                signal_owned_group(signal.SIGKILL,'cancel_kill_owned_group')
            except BaseException as error:
                record_cleanup_error('cancel_kill_owned_group',error)
        try:
            out,err=process.communicate(timeout=5)
            stdout,stderr=_output_text(out),_output_text(err)
            phases.append(dict(phase='cancel_kill_drain',outcome='completed',returncode=process.returncode))
        except subprocess.TimeoutExpired as error:
            remember_timeout(error)
            cleanup_complete=False
            phases.append(dict(phase='cancel_kill_drain',outcome='timeout',seconds=5))
        except BaseException as error:
            record_cleanup_error('cancel_kill_drain',error)
        # Never leave evidence collection waiting on a descendant-held pipe.
        for pipe in (process.stdout,process.stderr):
            if pipe is not None:
                try: pipe.close()
                except BaseException as error: record_cleanup_error('cancel_close_pipe',error)
        deadline=time.monotonic()+5
        while True:
            try:
                os.killpg(owned_pgid,0)
            except ProcessLookupError:
                phases.append(dict(phase='cancel_group_absence_check',outcome='absent'))
                break
            except BaseException as error:
                record_cleanup_error('cancel_group_absence_check',error)
                break
            if time.monotonic()>=deadline:
                cleanup_complete=False
                phases.append(dict(phase='cancel_group_absence_check',outcome='still_present',seconds=5))
                break
            try: time.sleep(.05)
            except BaseException as error:
                record_cleanup_error('cancel_group_absence_wait',error)
                break
    try:
        # Defer Python SIGINT only while ownership is being established. Do not
        # block OS signals or change the child's inherited mask/preexec state.
        previous_sigint=signal.getsignal(signal.SIGINT)
        pending_sigint=[]
        def defer_sigint(signum, frame):
            if previous_sigint != signal.SIG_IGN and not pending_sigint:
                pending_sigint.append((signum,frame))
        initialization_error=None
        signal.signal(signal.SIGINT,defer_sigint)
        try:
            try:
                process=subprocess.Popen(command,cwd=ROOT/'backend',env=env,
                    stdout=subprocess.PIPE,stderr=subprocess.PIPE,start_new_session=True)
            except OSError as error:
                launch_error=error
                metadata.update(launch_error_type=type(error).__name__,launch_errno=error.errno)
                phases.append(dict(phase='launch',outcome='failed'))
            if process is not None:
                # All cleanup functions already exist before the child starts.
                owned_pgid=process.pid
                metadata.update(pid=process.pid,owned_pgid=owned_pgid,
                    interrupt_grace_seconds=5,kill_drain_seconds=5)
        except BaseException as error:
            initialization_error=error
        finally:
            signal.signal(signal.SIGINT,previous_sigint)
        if pending_sigint:
            signum,frame=pending_sigint[0]
            handler=previous_sigint if callable(previous_sigint) else signal.default_int_handler
            handler(signum,frame)
        if initialization_error is not None:
            raise initialization_error
        if launch_error is not None:
            raise launch_error
        try:
            out,err=process.communicate(timeout=120)
            stdout,stderr=_output_text(out),_output_text(err)
            phases.append(dict(phase='initial_wait',outcome='completed',returncode=process.returncode))
        except subprocess.TimeoutExpired as error:
            timed_out=True
            remember_timeout(error)
            phases.append(dict(phase='initial_wait',outcome='timeout',seconds=120))
            signal_owned_group(signal.SIGINT,'interrupt_owned_group')
            try:
                out,err=process.communicate(timeout=5)
                stdout,stderr=_output_text(out),_output_text(err)
                phases.append(dict(phase='interrupt_grace',outcome='completed',returncode=process.returncode))
            except subprocess.TimeoutExpired as error:
                remember_timeout(error)
                phases.append(dict(phase='interrupt_grace',outcome='timeout',seconds=5))
            # Also kill remaining descendants when the leader exited during grace.
            # ESRCH is an expected successful no-op for an already absent group.
            signal_owned_group(signal.SIGKILL,'kill_owned_group')
            try:
                out,err=process.communicate(timeout=5)
                stdout,stderr=_output_text(out),_output_text(err)
                phases.append(dict(phase='kill_drain',outcome='completed',returncode=process.returncode))
            except subprocess.TimeoutExpired as error:
                remember_timeout(error)
                cleanup_complete=False
                phases.append(dict(phase='kill_drain',outcome='timeout',seconds=5,
                                   returncode=process.poll()))
                # Never hang evidence collection on a descendant-held pipe.
                for pipe in (process.stdout,process.stderr):
                    if pipe is not None:pipe.close()
            deadline=time.monotonic()+5
            while True:
                try:
                    os.killpg(owned_pgid,0)
                except ProcessLookupError:
                    phases.append(dict(phase='group_absence_check',outcome='absent'))
                    break
                except OSError as error:
                    cleanup_complete=False
                    phases.append(dict(phase='group_absence_check',outcome='probe_error',errno=error.errno))
                    break
                if time.monotonic()>=deadline:
                    cleanup_complete=False
                    phases.append(dict(phase='group_absence_check',outcome='still_present',seconds=5))
                    break
                time.sleep(.05)
    except BaseException as error:
        interrupted=error
        phases.append(dict(phase='runner_interrupted',outcome='interrupted',error_type=type(error).__name__))
        if process is not None:
            try:
                if owned_pgid is None:owned_pgid=process.pid
                cleanup_after_interrupt(error)
            except BaseException as cleanup_error:
                cleanup_complete=False
                error.add_note('Owned Go cleanup unexpectedly failed: '+type(cleanup_error).__name__)
        raise
    finally:
        try:
            events = []
            parse_error = False
            for line in stdout.splitlines():
                if not line.startswith('{'):
                    continue
                try:
                    event = json.loads(line)
                    events.append({key: event[key] for key in ('Action', 'Package', 'Test', 'Elapsed') if key in event})
                except (ValueError, TypeError):
                    parse_error = True
            (directory/'result.json').write_text(json.dumps(dict(metadata,
                exit_code=process.poll() if process is not None else None,
                timed_out=timed_out, interruption_type=type(interrupted).__name__ if interrupted is not None else None,
                child_reaped=process is not None and process.poll() is not None,
                cleanup_complete=cleanup_complete, phases=phases,
                descendant_cleanup_status=('verified_absent' if cleanup_complete else 'incomplete') if (timed_out or interrupted is not None) else 'not_verified_normal_exit',
                escaping_descendants_or_oom='not_proven_by_process_group_protocol',
                events=events, json_parse_error=parse_error,
                stdout_sha256=sha256(stdout.encode()).hexdigest(),
                stderr_sha256=sha256(stderr.encode()).hexdigest()), indent=2)+'\n')
        except BaseException as evidence_error:
            if interrupted is None:
                raise
            interrupted.add_note('Go evidence write failed: '+type(evidence_error).__name__)
    evidence = str(directory/'result.json')
    assert not timed_out and process.returncode == 0 and not parse_error and cleanup_complete, evidence
    exact = [event for event in events if event.get('Package') == PACKAGE and event.get('Test') == NAME]
    assert sum(event.get('Action') == 'run' for event in exact) == 1, evidence
    assert [event['Action'] for event in exact if event.get('Action') in ('pass', 'fail', 'skip')] == ['pass'], evidence
    assert not any(event.get('Action') in ('skip', 'fail') for event in events), evidence
    assert not any(event.get('Test') == NAME and event.get('Package') != PACKAGE for event in events), evidence
