"""Run each CPU planning capacity case in an isolated process; no DB or pixels."""
import argparse
import hashlib
import json
import platform
import resource
import subprocess
import sys
import time
import tracemalloc
from datetime import datetime, timedelta, timezone
from pathlib import Path
from uuid import UUID

ROOT = Path(__file__).resolve().parents[3]
CASES = ('unknown', 'known_sparse', 'known_dense', 'known_dense_64', 'known_burst_64')


def measure(case):
    sys.path.insert(0, str(ROOT / 'workers'))
    from photo_worker.operations.session.plan import (
        Frame, ScenePolicy, SceneCalibration, group_scenes, validate_scene_groups,
    )
    neighbors = 64 if case.endswith('_64') else 2
    now = datetime(2026, 1, 1, tzinfo=timezone.utc)
    tracemalloc.start()
    start = time.perf_counter()
    frames = [Frame(i, None if case == 'unknown' else now + timedelta(
        seconds=i * (61 if case == 'known_sparse' else 0 if case == 'known_burst_64' else 1)), None,
        (1.0,) + (0.0,) * 383, .4, None, 10.0, 0.0) for i in range(5000)]
    snapshot = UUID('00000000-0000-4000-8000-000000000001')
    groups = group_scenes(frames, snapshot, 'a' * 64, ScenePolicy(60, neighbors, {}),
                          SceneCalibration(.7, 1, .25, 10, 1))
    validate_scene_groups(groups, frames, snapshot, 'a' * 64)
    elapsed = time.perf_counter() - start
    _, peak = tracemalloc.get_traced_memory()
    tracemalloc.stop()
    rss = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    return dict(case=case, status='passed', frames=5000, neighbor_count=neighbors,
                groups=len(groups), elapsed_seconds=elapsed, traced_peak_bytes=peak,
                process_peak_rss_bytes=rss if sys.platform == 'darwin' else rss * 1024)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', choices=CASES)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    if args.case:
        print(json.dumps(measure(args.case)))
        return
    results = []
    for case in CASES:
        try:
            child = subprocess.run([sys.executable, __file__, '--case', case],
                                   check=True, capture_output=True, text=True, timeout=60)
            results.append(json.loads(child.stdout))
        except subprocess.TimeoutExpired:
            results.append(dict(case=case, status='timeout', timeout_seconds=60))
        except subprocess.CalledProcessError as error:
            results.append(dict(case=case, status='failed', exit_code=error.returncode,
                                diagnostic=error.stderr[-2000:]))
    result = dict(source_sha256=hashlib.sha256((ROOT / 'workers/photo_worker/operations/session/plan.py').read_bytes()).hexdigest(),
                  platform=platform.platform(), python=platform.python_version(), cases=results,
                  scope='CPU descriptors/planning/validation; isolated process RSS includes imports; no renderer/model/GPU or whole-worker qualification')
    if args.output:
        args.output.resolve().relative_to(ROOT)
        args.output.write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result))
    if any(case['status'] != 'passed' for case in results):
        sys.exit(1)


if __name__ == '__main__':
    main()
