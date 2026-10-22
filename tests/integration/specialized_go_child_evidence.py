"""Exact Go child evidence; records no output payload, environment, DSN or URL."""
import json
import os
import re
import subprocess
from hashlib import sha256
from pathlib import Path
from uuid import uuid4


def run_named_go(argv, *, expect_failure=False, **kwargs):
    argv = list(argv)
    assert argv[:2] == ['go', 'test']
    package = next(value for value in argv[2:] if value.startswith('./'))
    expected_package = 'photoeditor/backend/' + package[2:]
    selector = argv[argv.index('-run') + 1]
    assert selector.startswith('^') and selector.endswith('$'), selector
    body = selector[1:-1]
    # Existing exact selectors and the explicit two-root native content selector.
    match = re.fullmatch(r'([A-Za-z0-9_]+)\(([A-Za-z0-9_|]+)\)', body)
    names = [match[1] + suffix for suffix in match[2].split('|')] if match else [body]
    assert all(re.fullmatch(r'Test[A-Za-z0-9_]+', name) for name in names), selector
    if '-json' not in argv:
        argv.insert(2, '-json')
    result = subprocess.run(argv, **kwargs)
    events = []
    for line in result.stdout.splitlines():
        if line.startswith('{'):
            event = json.loads(line)
            events.append({key: event[key] for key in ('Action', 'Package', 'Test', 'Elapsed') if key in event})
    directory = Path(__file__).resolve().parents[2]/'work/specialized-v27-go-evidence'
    directory.mkdir(parents=True, exist_ok=True)
    evidence_id = names[0]+'-'+uuid4().hex
    path = directory/(evidence_id+'.json')
    # Preserve failed child diagnostics before assertions, without exposing credentials.
    # The public summary below contains hashes only; raw streams remain owner-only.
    private = directory/(evidence_id+'.private')
    private.mkdir(mode=0o700)
    for filename, value in (('stdout.log', result.stdout), ('stderr.log', result.stderr)):
        fd = os.open(private/filename, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'w') as stream:
            stream.write(value)
    nodeid=os.environ.get('PYTEST_CURRENT_TEST','').split(' (',1)[0]
    owner=nodeid.split('::',1)[0]
    owner_path=Path(__file__).resolve().parents[2]/owner
    owner_sha=sha256(owner_path.read_bytes()).hexdigest() if owner and owner_path.is_file() else None
    path.write_text(json.dumps(dict(pytest_nodeid=nodeid,source=owner,source_sha256=owner_sha,
        helper_sha256=sha256(Path(__file__).read_bytes()).hexdigest(),
        package=expected_package, roots=names, expected_failure=expect_failure,
        exit_code=result.returncode, events=events, stdout_sha256=sha256(result.stdout.encode()).hexdigest(),
        stderr_sha256=sha256(result.stderr.encode()).hexdigest()), indent=2)+'\n')
    assert not any(event.get('Action') == 'skip' for event in events), str(path)
    for name in names:
        exact = [event for event in events if event.get('Package') == expected_package and event.get('Test') == name]
        assert sum(event.get('Action') == 'run' for event in exact) == 1, str(path)
        assert not any(event.get('Test') == name and event.get('Package') != expected_package for event in events), str(path)
        terminal = [event['Action'] for event in exact if event.get('Action') in ('pass', 'fail', 'skip')]
        assert terminal == (['fail'] if expect_failure else ['pass']), str(path)
    if expect_failure:
        assert result.returncode != 0, str(path)
    else:
        assert result.returncode == 0, str(path)
        assert not any(event.get('Action') == 'fail' for event in events), str(path)
    return result
