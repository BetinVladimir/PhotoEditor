"""Exact positive child-Go proof for retained physical wrappers; no environment persisted.

Named-root checks follow specialized_go_child_evidence.run_named_go. This
separate helper additionally retains raw JSON, timeout and source pins without
changing the existing helper or the caller's domain assertions/bounds.
"""
from pathlib import Path
from hashlib import sha256
from uuid import uuid4
import inspect
import json
import os
import re
import subprocess


def run_evidenced_go(argv, **kwargs):
    argv=list(argv)
    assert argv[:2]==['go','test'] and kwargs.get('shell') is not True
    package=next(value for value in argv[2:] if value.startswith('./'))
    selector=argv[argv.index('-run')+1]
    assert re.fullmatch(r'\^Test[A-Za-z0-9_]+\$',selector), selector
    assert argv[argv.index('-count=1')]=='-count=1'
    name=selector[1:-1]
    expected_package='photoeditor/backend/'+package.removeprefix('./')
    if '-json' not in argv:argv.insert(2,'-json')
    root=Path(__file__).resolve().parents[2]
    evidence_root=Path(os.environ.get('PHOTO_TEST_RETAINED_CHILD_EVIDENCE_ROOT',str(root/'work/retained-physical-go-evidence'))).resolve()
    assert evidence_root.is_relative_to(root) and evidence_root!=root
    directory=evidence_root/uuid4().hex
    directory.mkdir(parents=True,mode=0o700)
    caller=Path(inspect.currentframe().f_back.f_code.co_filename).resolve()
    assert caller.is_relative_to(root)
    source_sha=sha256(caller.read_bytes()).hexdigest()
    current=os.environ.get('PYTEST_CURRENT_TEST','')
    assert current.endswith(' (call)'), 'actual pytest call ownership required'
    nodeid=current.removesuffix(' (call)')
    output=stderr='';code=None;timed_out=False;exception_type=None;original_error=None
    try:
        result=subprocess.run(argv,**kwargs)
        output,stderr,code=result.stdout,result.stderr,result.returncode
    except subprocess.TimeoutExpired as error:
        original_error=error;timed_out=True;exception_type=type(error).__name__
        output=error.stdout or '';stderr=error.stderr or ''
        raise
    except BaseException as error:
        original_error=error;exception_type=type(error).__name__
        raise
    finally:
        try:
            if isinstance(output,bytes):output=output.decode(errors='replace')
            if isinstance(stderr,bytes):stderr=stderr.decode(errors='replace')
            for filename,body in [('go.jsonl',output),('stderr.log',stderr)]:
                path=directory/filename;path.write_text(body);path.chmod(0o600)
            metadata=dict(package=expected_package,test=name,pytest_nodeid=nodeid,
                source=str(caller.relative_to(root)),source_sha256=source_sha,
                helper_sha256=sha256(Path(__file__).read_bytes()).hexdigest(),
                returncode=code,timed_out=timed_out,exception_type=exception_type,
                stdout_sha256=sha256(output.encode()).hexdigest(),stderr_sha256=sha256(stderr.encode()).hexdigest())
            path=directory/'result.json';path.write_text(json.dumps(metadata,sort_keys=True)+'\n');path.chmod(0o600)
        except BaseException as evidence_error:
            if original_error is None:raise
            # Never replace the original timeout/interrupt with evidence I/O failure.
            try:original_error.add_note('Child evidence persistence failed: '+type(evidence_error).__name__)
            except BaseException:pass
    assert code==0,output+stderr
    events=[json.loads(line) for line in output.splitlines() if line.startswith('{')]
    exact=[event for event in events if event.get('Package')==expected_package and event.get('Test')==name]
    assert sum(event.get('Action')=='run' for event in exact)==1,str(directory)
    assert [event['Action'] for event in exact if event.get('Action') in ('pass','fail','skip')]==['pass'],str(directory)
    assert not any(event.get('Action') in ('fail','skip') for event in events),str(directory)
    assert not any(event.get('Test')==name and event.get('Package')!=expected_package for event in events),str(directory)
    assert sha256(caller.read_bytes()).hexdigest()==source_sha,'source changed during child'
    return result
