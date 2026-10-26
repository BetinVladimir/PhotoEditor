"""Parse and apply SEARCH/REPLACE edit blocks produced by the model (all-or-nothing)."""
import json
import re

from lib import ROOT, AgentError, edit_targets


def parse(text):
    """-> list of (kind, path, search, replace) ; kind in {'edit', 'new'}."""
    out, lines, i = [], text.splitlines(), 0
    cur = None
    while i < len(lines):
        ln = lines[i].rstrip()
        m = re.match(r'^(FILE|NEWFILE):\s*(\S.*?)\s*$', ln.strip('`* '))
        if m:
            cur = (m.group(1), m.group(2).strip('`'))
            i += 1
            continue
        if ln.startswith('BLOCKED:'):
            raise AgentError(ln)
        if cur and ln in ('<<<<<<< SEARCH', '<<<<<<< CONTENT'):
            kind = 'edit' if ln.endswith('SEARCH') else 'new'
            if (kind == 'edit') != (cur[0] == 'FILE'):
                raise AgentError('block type does not match header for %s' % cur[1])
            i += 1
            a = []
            while i < len(lines) and (lines[i].rstrip() != ('=======' if kind == 'edit' else '>>>>>>> END')):
                a.append(lines[i])
                i += 1
            if i >= len(lines):
                raise AgentError('unterminated block for %s' % cur[1])
            i += 1
            if kind == 'new':
                out.append(('new', cur[1], '', '\n'.join(a)))
                continue
            b = []
            while i < len(lines) and lines[i].rstrip() != '>>>>>>> REPLACE':
                b.append(lines[i])
                i += 1
            if i >= len(lines):
                raise AgentError('missing >>>>>>> REPLACE for %s' % cur[1])
            i += 1
            out.append(('edit', cur[1], '\n'.join(a), '\n'.join(b)))
            continue
        i += 1
    if not out:
        raise AgentError('no edit blocks found in model output')
    return out


def _find_once(content, search, path):
    if search == '':
        raise AgentError('%s: empty SEARCH block' % path)
    c = content.count(search)
    if c == 1:
        return content.index(search), len(search)
    if c > 1:
        raise AgentError('%s: SEARCH matches %d places, add more surrounding lines' % (path, c))
    # tolerate trailing-whitespace differences only
    cl = content.split('\n')
    sl = search.split('\n')
    norm = [x.rstrip() for x in cl]
    nsl = [x.rstrip() for x in sl]
    hits = [k for k in range(len(norm) - len(nsl) + 1) if norm[k:k + len(nsl)] == nsl]
    if len(hits) == 1:
        k = hits[0]
        start = sum(len(x) + 1 for x in cl[:k])
        length = sum(len(x) + 1 for x in cl[k:k + len(nsl)]) - 1
        return start, length
    first = sl[0].strip()
    raise AgentError('%s: SEARCH not found (first line: %r%s)' % (
        path, first[:80], ', that line exists elsewhere' if first and first in content else ''))


def _check_syntax(path, content):
    if path.endswith('.py'):
        try:
            compile(content, path, 'exec')
        except SyntaxError as e:
            raise AgentError('%s: Python syntax error line %s: %s' % (path, e.lineno, e.msg))
    elif path.endswith('.json'):
        try:
            json.loads(content)
        except ValueError as e:
            raise AgentError('%s: invalid JSON: %s' % (path, e))


def apply(meta, model_text, dry_run=False):
    allowed = dict(edit_targets(meta))
    state = {}
    for kind, path, search, repl in parse(model_text):
        if path not in allowed:
            raise AgentError('%s is not in the card edit list' % path)
        is_new = allowed[path]
        full = ROOT / path
        if kind == 'new':
            if not is_new and full.exists():
                raise AgentError('%s exists; use FILE/SEARCH edits' % path)
            state[path] = repl + ('\n' if not repl.endswith('\n') else '')
            continue
        if path not in state:
            if not full.is_file():
                raise AgentError('%s does not exist' % path)
            state[path] = full.read_text(encoding='utf-8')
        s, n = _find_once(state[path], search, path)
        state[path] = state[path][:s] + repl + state[path][s + n:]
    for p, c in state.items():
        _check_syntax(p, c)
    if not dry_run:
        for p, c in state.items():
            (ROOT / p).parent.mkdir(parents=True, exist_ok=True)
            (ROOT / p).write_text(c, encoding='utf-8')
    return sorted(state)
