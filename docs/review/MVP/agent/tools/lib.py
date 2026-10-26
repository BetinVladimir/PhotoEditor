"""Shared helpers for the small-context codegen workflow (see docs/review/MVP/agent/README.md).

Stdlib only, Python >= 3.8. Everything is deterministic: the same card and repo state
give the same prompt, and a prompt over budget is an error (split the card).
"""
import json
import os
import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parents[5]
AGENT_DIR = ROOT / 'docs' / 'review' / 'MVP' / 'agent'
CARDS = AGENT_DIR / 'tasks'
SYSTEM = AGENT_DIR / 'SYSTEM.md'
STATUS = AGENT_DIR / 'STATUS.json'
# Window 32k = prompt + output. Keep >= 12k for output, retries and tokenizer error.
PROMPT_BUDGET = int(os.environ.get('AGENT_PROMPT_BUDGET', '17000'))
CHARS_PER_TOKEN = float(os.environ.get('AGENT_CHARS_PER_TOKEN', '3.0'))  # conservative for code
MAX_EXCERPT_LINES = int(os.environ.get('AGENT_MAX_EXCERPT_LINES', '220'))
MAX_WHOLE_FILE_LINES = 300


class AgentError(Exception):
    pass


def est_tokens(text):
    return int(len(text) / CHARS_PER_TOKEN) + 1


# ---------------------------------------------------------------- cards
def parse_front(block):
    meta, key = {}, None
    for raw in block.splitlines():
        if not raw.strip() or raw.lstrip().startswith('#'):
            continue
        if raw.startswith((' ', '\t')) and raw.strip().startswith('- ') and key:
            meta[key].append(raw.strip()[2:].strip())
            continue
        k, _, v = raw.partition(':')
        key, v = k.strip(), v.strip()
        if v == '':
            meta[key] = []
        elif v.startswith('[') and v.endswith(']'):
            meta[key] = [x.strip() for x in v[1:-1].split(',') if x.strip()]
        else:
            meta[key] = v
    return meta


def load_card(card_id):
    path = CARDS / (card_id + '.md')
    if not path.exists():
        raise AgentError('no such card: %s' % path)
    m = re.match(r'---\n(.*?)\n---\n(.*)\Z', path.read_text(encoding='utf-8'), re.S)
    if not m:
        raise AgentError('%s: missing front matter' % path.name)
    meta = parse_front(m.group(1))
    for k in ('id', 'title', 'size', 'edit', 'verify'):
        if k not in meta:
            raise AgentError('%s: missing field %s' % (path.name, k))
    if meta['id'] != card_id:
        raise AgentError('%s: id mismatch (%s)' % (path.name, meta['id']))
    meta.setdefault('depends', [])
    meta.setdefault('context', [])
    meta['body'] = m.group(2).strip()
    return meta


def all_card_ids():
    return sorted(p.stem for p in CARDS.glob('*.md'))


def edit_targets(meta):
    """[(path, is_new)] from `edit:` entries like `path` or `path (new)`."""
    out = []
    for e in meta['edit']:
        new = e.endswith('(new)')
        out.append((e.replace('(new)', '').strip(), new))
    return out


# ---------------------------------------------------------------- excerpts
_STR = re.compile(r'"(?:\\.|[^"\\])*"|\'(?:\\.|[^\'\\])*\'|`[^`]*`')


def _depth_delta(line):
    s = _STR.sub('""', line)
    s = re.sub(r'//.*$', '', s)
    return sum(s.count(c) for c in '{([') - sum(s.count(c) for c in '})]')


def _patterns(ext, name, inner):
    n = re.escape(name)
    if ext == '.py':
        return [r'^(\s*)(?:async\s+)?(?:def|class)\s+%s\b' % n]
    if ext == '.go':
        return [r'^func\s+(?:\([^)]*\)\s*)?%s\s*[\[(]' % n, r'^(?:type|var|const)\s+%s\b' % n]
    pats = [r'^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?(?:function\*?|class|const|let|type|interface|enum)\s+%s\b' % n]
    if inner:
        pats.insert(0, r'^\s+(?:(?:public|private|protected|static|readonly|async)\s+)*%s\s*[(<=:]' % n)
    return pats


def _go_method(lines, recv, name, lo, hi):
    rx = re.compile(r'^func\s+\(\w*\s*\*?%s\)\s+%s\s*[\[(]' % (re.escape(recv), re.escape(name)))
    for i in range(lo, hi):
        if rx.match(lines[i]):
            return i
    return None


def _extent(lines, start, ext, lo, hi):
    if ext == '.py':
        m = re.match(r'(\s*)', lines[start])
        base = len(m.group(1))
        end = start + 1
        for j in range(start + 1, hi):
            ln = lines[j]
            if ln.strip() and (len(ln) - len(ln.lstrip())) <= base:
                break
            if ln.strip():
                end = j + 1
        s = start
        while s > lo and lines[s - 1].lstrip().startswith('@'):
            s -= 1
        return s, end
    depth, seen = 0, False
    end = start + 1
    for j in range(start, hi):
        d = _depth_delta(lines[j])
        depth += d
        if '{' in lines[j] or '(' in lines[j] or '[' in lines[j]:
            seen = True
        end = j + 1
        if seen and depth <= 0:
            break
        if not seen and lines[j].rstrip().endswith(';'):
            break
        if j - start > 2000:
            break
    s = start
    if ext == '.go':
        while s > lo and lines[s - 1].startswith('//'):
            s -= 1
    return s, end


def find_symbol(lines, ext, dotted):
    lo, hi = 0, len(lines)
    parts = dotted.split('.')
    if ext == '.go' and len(parts) == 2:
        i = _go_method(lines, parts[0], parts[1], 0, hi)
        if i is None:
            return None
        return _extent(lines, i, ext, 0, hi)
    s = e = None
    for idx, p in enumerate(parts):
        found = None
        for pat in _patterns(ext, p, idx > 0):
            rx = re.compile(pat)
            for i in range(lo, hi):
                if rx.match(lines[i]):
                    found = i
                    break
            if found is not None:
                break
        if found is None:
            return None
        s, e = _extent(lines, found, ext, lo, hi)
        lo, hi = found + 1, e
        if idx < len(parts) - 1:
            lo = found  # search inside the container body
    return s, e


def read_excerpt(spec):
    """Returns (label, text, language). spec: `path`, `path::Sym.sub`, `path:10-80`."""
    rng = re.search(r':(\d+)-(\d+)$', spec)
    sym = None
    if '::' in spec:
        spec, sym = spec.split('::', 1)
    elif rng:
        spec = spec[:rng.start()]
    path = ROOT / spec
    if not path.is_file():
        raise AgentError('context file not found: %s' % spec)
    lines = path.read_text(encoding='utf-8', errors='replace').splitlines()
    ext = path.suffix
    lang = {'.py': 'python', '.go': 'go', '.ts': 'ts', '.tsx': 'tsx', '.sql': 'sql', '.json': 'json'}.get(ext, '')
    if sym:
        r = find_symbol(lines, ext, sym)
        if r is None:
            raise AgentError('symbol not found: %s::%s' % (spec, sym))
        a, b = r
    elif rng:
        a, b = int(rng.group(1)) - 1, min(int(rng.group(2)), len(lines))
    else:
        if len(lines) > MAX_WHOLE_FILE_LINES:
            raise AgentError('%s has %d lines: give ::Symbol or :a-b' % (spec, len(lines)))
        a, b = 0, len(lines)
    note = ''
    if b - a > MAX_EXCERPT_LINES:
        note = '\n... [truncated: %d more lines]' % (b - a - MAX_EXCERPT_LINES)
        b = a + MAX_EXCERPT_LINES
    label = '%s%s (lines %d-%d)' % (spec, '::' + sym if sym else '', a + 1, b)
    return label, '\n'.join(lines[a:b]) + note, lang


# ---------------------------------------------------------------- prompt
def build_prompt(card_id, extra=None, planned=frozenset()):
    meta = load_card(card_id)
    parts = [SYSTEM.read_text(encoding='utf-8').strip()]
    parts.append('# TASK %s: %s\n\n%s' % (meta['id'], meta['title'], meta['body']))
    files = ['- %s (%s)' % (p, 'create' if new else 'edit') for p, new in edit_targets(meta)]
    parts.append('## Files you may touch (nothing else)\n' + '\n'.join(files))
    ctx = []
    for spec in meta['context']:
        if spec.split('::')[0] in planned and not (ROOT / spec.split('::')[0]).exists():
            ctx.append('### %s\n[created by an earlier card; placeholder for size estimate]\n```\n%s\n```' % (spec, 'x' * 2400))
            continue
        try:
            label, text, lang = read_excerpt(spec)
        except AgentError as e:
            if str(e).startswith('symbol not found') and spec.split('::')[0] in planned:
                ctx.append('### %s\n[symbol is created by an earlier card; size placeholder]\n```\n%s\n```' % (spec, 'x' * 1800))
                continue
            raise
        ctx.append('### %s\n```%s\n%s\n```' % (label, lang, text))
    if ctx:
        parts.append('## Current code (read-only context, may be partial)\n\n' + '\n\n'.join(ctx))
    parts.append('## Checks that will run after your edit\n' + '\n'.join('- `%s`' % v for v in meta['verify']))
    if extra:
        parts.append(extra)
    text = '\n\n'.join(parts) + '\n'
    n = est_tokens(text)
    if n > PROMPT_BUDGET:
        sizes = ', '.join('%s=%d' % (s.splitlines()[0][:40], est_tokens(s)) for s in parts)
        raise AgentError('prompt for %s is ~%d tokens > budget %d. Split the card or narrow context. [%s]'
                         % (card_id, n, PROMPT_BUDGET, sizes))
    return text, n


# ---------------------------------------------------------------- status
def load_status():
    try:
        return json.loads(STATUS.read_text(encoding='utf-8'))
    except (OSError, ValueError):
        return {}


def save_status(st):
    STATUS.write_text(json.dumps(st, indent=1, sort_keys=True, ensure_ascii=False) + '\n', encoding='utf-8')
