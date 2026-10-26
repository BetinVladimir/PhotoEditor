#!/usr/bin/env python3
"""Task runner for a 32k-context code model.

  agent.py check                  validate all cards (front matter, deps, files, symbols, prompt budget)
  agent.py list                   cards with status and readiness
  agent.py next                   next ready card id
  agent.py prompt ID [--stats]    print the self-contained prompt
  agent.py apply ID FILE          apply model output (SEARCH/REPLACE blocks) from FILE ('-' = stdin)
  agent.py verify ID              run the card's checks
  agent.py run ID [--retries 3] [--from-file OUT]   prompt -> model -> apply -> verify, stateless retries

Model endpoint (OpenAI-compatible): LLM_BASE_URL, LLM_MODEL, LLM_API_KEY, LLM_MAX_TOKENS (default 6000).
"""
import argparse
import json
import os
import subprocess
import sys
import time
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import edits  # noqa: E402
from lib import (AgentError, ROOT, all_card_ids, build_prompt, edit_targets, est_tokens, load_card,  # noqa: E402
                 load_status, read_excerpt, save_status)

FEEDBACK_TAIL_CHARS = 7000  # ~2.3k tokens of the failing output


def check():
    bad, ids = 0, all_card_ids()
    metas = {}
    for cid in ids:
        try:
            metas[cid] = load_card(cid)
        except AgentError as e:
            print('FAIL %s' % e)
            bad += 1
    def closure(x, acc):
        for d in metas.get(x, {}).get('depends', []):
            if d not in acc:
                acc.add(d)
                closure(d, acc)
        return acc
    for cid, m in metas.items():
        errs = []
        # files created or edited by (transitive) dependencies may not exist yet / lack symbols yet
        planned = frozenset(p for d in closure(cid, set()) if d in metas for p, _ in edit_targets(metas[d]))
        for d in m['depends']:
            if d not in metas:
                errs.append('unknown dependency %s' % d)
        for p, new in edit_targets(m):
            if not new and not (ROOT / p).is_file() and p not in planned:
                errs.append('edit target missing: %s' % p)
        try:
            _, n = build_prompt(cid, planned=planned)
        except AgentError as e:
            errs.append(str(e))
            n = 0
        if m['size'] not in ('XS', 'S'):
            errs.append('size must be XS or S for codegen (got %s); split it' % m['size'])
        print('%s %-8s ~%5d tok  %s' % ('FAIL' if errs else 'ok  ', cid, n, m['title'][:60]))
        for e in errs:
            print('       - %s' % e)
        bad += bool(errs)
    # cycles
    seen = {}

    def visit(x, path):
        if seen.get(x) == 2:
            return
        if seen.get(x) == 1:
            raise AgentError('dependency cycle: %s' % ' -> '.join(path + [x]))
        seen[x] = 1
        for d in metas.get(x, {}).get('depends', []):
            visit(d, path + [x])
        seen[x] = 2
    try:
        for x in metas:
            visit(x, [])
    except AgentError as e:
        print('FAIL %s' % e)
        bad += 1
    print('%d cards, %d problems' % (len(ids), bad))
    return 1 if bad else 0


def ready(cid, st, metas):
    return st.get(cid, {}).get('status') != 'done' and all(st.get(d, {}).get('status') == 'done' for d in metas[cid]['depends'])


def order():
    idx = ROOT / 'docs/review/MVP/agent/INDEX.md'
    ids = all_card_ids()
    if idx.exists():
        seq = [t.strip('`|').strip() for t in idx.read_text(encoding='utf-8').split() if t.strip('`|') in ids]
        seen = []
        for s in seq:
            if s not in seen:
                seen.append(s)
        return seen + [i for i in ids if i not in seen]
    return ids


def cmd_list():
    st = load_status()
    metas = {c: load_card(c) for c in all_card_ids()}
    for cid in order():
        s = st.get(cid, {}).get('status', 'todo')
        print('%-8s %-8s %-5s %s%s' % (cid, s, metas[cid]['size'], metas[cid]['title'][:62],
                                       '' if s == 'done' or ready(cid, st, metas) else '  [blocked by deps]'))


def cmd_next():
    st = load_status()
    metas = {c: load_card(c) for c in all_card_ids()}
    for cid in order():
        if st.get(cid, {}).get('status') != 'blocked' and ready(cid, st, metas):
            print(cid)
            return 0
    print('none')
    return 1


def run_verify(meta):
    for cmd in meta['verify']:
        print('$ %s' % cmd, flush=True)
        try:
            r = subprocess.run(cmd, shell=True, cwd=str(ROOT), capture_output=True, text=True,
                               timeout=int(os.environ.get('AGENT_VERIFY_TIMEOUT', '900')))
        except subprocess.TimeoutExpired:
            return False, 'TIMEOUT: %s' % cmd
        out = (r.stdout or '') + (r.stderr or '')
        if r.returncode != 0:
            return False, '$ %s\n(exit %d)\n%s' % (cmd, r.returncode, out[-FEEDBACK_TAIL_CHARS:])
    return True, ''


def call_model(prompt):
    base = os.environ.get('LLM_BASE_URL')
    if not base:
        raise AgentError('LLM_BASE_URL is not set (or use --from-file)')
    body = json.dumps({
        'model': os.environ.get('LLM_MODEL', 'local'),
        'messages': [{'role': 'user', 'content': prompt}],
        'temperature': float(os.environ.get('LLM_TEMPERATURE', '0.1')),
        'max_tokens': int(os.environ.get('LLM_MAX_TOKENS', '6000')),
    }).encode()
    req = urllib.request.Request(base.rstrip('/') + '/chat/completions', data=body, headers={
        'Content-Type': 'application/json', 'Authorization': 'Bearer ' + os.environ.get('LLM_API_KEY', 'x')})
    with urllib.request.urlopen(req, timeout=int(os.environ.get('LLM_TIMEOUT', '900'))) as r:
        return json.loads(r.read().decode())['choices'][0]['message']['content']


def snapshot(meta):
    return {p: ((ROOT / p).read_bytes() if (ROOT / p).exists() else None) for p, _ in edit_targets(meta)}


def restore(snap):
    for p, data in snap.items():
        if data is None:
            if (ROOT / p).exists():
                try:
                    (ROOT / p).unlink()
                except OSError:  # deletion forbidden in this sandbox: leave an empty file, report it
                    (ROOT / p).write_bytes(b'')
                    print('WARNING: could not delete %s; emptied instead' % p)
        else:
            (ROOT / p).write_bytes(data)


def cmd_run(cid, retries, from_file):
    meta = load_card(cid)
    st = load_status()
    feedback = None
    for attempt in range(1, retries + 2):
        extra = None
        if feedback:
            extra = '## Previous attempt FAILED (it was reverted; start from the code above)\n```\n%s\n```\nFix the cause and output the complete edits again.' % feedback
        prompt, n = build_prompt(cid, extra)
        print('[%s] attempt %d, prompt ~%d tokens' % (cid, attempt, n), flush=True)
        if from_file:
            answer = open(from_file, encoding='utf-8').read() if from_file != '-' else sys.stdin.read()
        else:
            answer = call_model(prompt)
        (ROOT / 'work').mkdir(exist_ok=True)
        (ROOT / 'work' / ('agent-%s-%d.txt' % (cid, attempt))).write_text(answer, encoding='utf-8')
        snap = snapshot(meta)
        try:
            files = edits.apply(meta, answer)
            ok, out = run_verify(meta)
            if not ok:
                feedback = out
        except AgentError as e:
            ok, feedback = False, 'EDIT ERROR: %s' % e
        if ok:
            st[cid] = {'status': 'done', 'attempts': attempt, 'files': files, 'ts': int(time.time())}
            save_status(st)
            print('[%s] DONE: %s' % (cid, ', '.join(files)))
            return 0
        restore(snap)
        print('[%s] attempt %d failed:\n%s' % (cid, attempt, feedback[-1500:]))
        if from_file:
            break
    st[cid] = {'status': 'blocked', 'attempts': attempt, 'last_error': (feedback or '')[-800:], 'ts': int(time.time())}
    save_status(st)
    print('[%s] BLOCKED after %d attempts -> escalate to a human/larger model' % (cid, attempt))
    return 2


def main():
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest='cmd', required=True)
    sub.add_parser('check'), sub.add_parser('list'), sub.add_parser('next')
    p = sub.add_parser('prompt'); p.add_argument('id'); p.add_argument('--stats', action='store_true')
    p = sub.add_parser('apply'); p.add_argument('id'); p.add_argument('file'); p.add_argument('--dry-run', action='store_true')
    p = sub.add_parser('verify'); p.add_argument('id')
    p = sub.add_parser('run'); p.add_argument('id'); p.add_argument('--retries', type=int, default=3); p.add_argument('--from-file')
    p = sub.add_parser('done'); p.add_argument('id')
    a = ap.parse_args()
    try:
        if a.cmd == 'check':
            return check()
        if a.cmd == 'list':
            return cmd_list() or 0
        if a.cmd == 'next':
            return cmd_next()
        if a.cmd == 'prompt':
            text, n = build_prompt(a.id)
            if a.stats:
                print('~%d tokens (budget %s)' % (n, os.environ.get('AGENT_PROMPT_BUDGET', '17000')), file=sys.stderr)
            print(text)
            return 0
        if a.cmd == 'apply':
            txt = sys.stdin.read() if a.file == '-' else open(a.file, encoding='utf-8').read()
            print('\n'.join(edits.apply(load_card(a.id), txt, a.dry_run)))
            return 0
        if a.cmd == 'verify':
            ok, out = run_verify(load_card(a.id))
            print('PASS' if ok else out)
            return 0 if ok else 1
        if a.cmd == 'run':
            return cmd_run(a.id, a.retries, a.from_file)
        if a.cmd == 'done':
            st = load_status(); st[a.id] = {'status': 'done', 'manual': True, 'ts': int(time.time())}; save_status(st)
            return 0
    except AgentError as e:
        print('ERROR: %s' % e, file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
