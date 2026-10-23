"""Bounded operator metadata only; never format an exception or its payload."""
from __future__ import annotations
import json
import logging
from functools import lru_cache
from pathlib import Path
import re
import sys
from types import CodeType, FunctionType, ModuleType

_LOG = logging.getLogger('photo_worker.operator')
_TRACEBACK = BaseException.__dict__['__traceback__']
_CAUSE = BaseException.__dict__['__cause__']
_CONTEXT = BaseException.__dict__['__context__']
_ROOT = Path(__file__).resolve().parents[1]
_IDENT = re.compile(r'^[A-Za-z_][A-Za-z0-9_]{0,95}$')
_MODULES = frozenset(('builtins', 'psycopg', 'psycopg.errors', 'ssl', 'socket',
                     'http.client', 'urllib.error', 'cv2', 'torch',
                     'photo_worker.color.icc', 'photo_worker.runtime.executor',
                     'photo_worker.runtime.gateway', 'photo_worker.runtime.materialize',
                     'photo_worker.runtime.artifacts', 'photo_worker.runtime.scratch',
                     'photo_worker.runtime.database'))
_CODES = frozenset(('INTERNAL_ERROR', 'RESOURCE_OOM', 'DEPENDENCY_UNAVAILABLE',
                    'ARTIFACT_MISSING', 'ARTIFACT_HASH_MISMATCH', 'DISK_QUOTA',
                    'CONFIG_INVALID', 'LEASE_LOST'))


def _codes(code: CodeType):
    yield code
    for value in code.co_consts:
        if isinstance(value, CodeType):
            yield from _codes(value)


@lru_cache(maxsize=32)
def _source_codes(filename: str, raw: bytes):
    # Compile for structural code comparison only: never execute source.
    return frozenset(_codes(compile(raw, filename, 'exec', dont_inherit=True)))


def _known_frame(frame, line):
    namespace = frame.f_globals
    name = namespace.get('__name__')
    module = sys.modules.get(name) if type(name) is str and name.startswith('photo_worker.') else None
    if not isinstance(module, ModuleType) or vars(module) is not namespace:
        return None
    filename = namespace.get('__file__')
    if type(filename) is not str:
        return None
    path = Path(filename).resolve()
    try:
        relative = path.relative_to(_ROOT)
    except ValueError:
        return None
    expected = _ROOT.joinpath(*name.split('.')[1:])
    if (path not in (expected.with_suffix('.py'), expected / '__init__.py')
            or not path.is_file() or Path(filename).is_symlink()
            or path.stat().st_size > 1_048_576
            or frame.f_code.co_filename != filename):
        return None
    raw = path.read_bytes()
    if len(raw) > 1_048_576 or frame.f_code not in _source_codes(filename, raw):
        return None
    function = frame.f_code.co_name
    if not _IDENT.fullmatch(function) or type(line) is not int or not 1 <= line <= raw.count(b'\n') + 1:
        return None
    # Only code belonging to real functions of this loaded source module is
    # admitted, including their compiled nested functions. A forged filename,
    # arbitrary eval/exec frame, imported class or module label is insufficient.
    for value in tuple(namespace.values()):
        members = (value,)
        if isinstance(value, type) and type.__getattribute__(value, '__module__') == name:
            members = tuple(type.__getattribute__(value, '__dict__').values())
        for member in members:
            if isinstance(member, (staticmethod, classmethod)):
                member = member.__func__
            if isinstance(member, FunctionType) and member.__globals__ is namespace:
                if any(code is frame.f_code for code in _codes(member.__code__)):
                    return {'path': 'photo_worker/' + relative.as_posix(),
                            'function': function, 'line': line}
    return None


def _exception_metadata(exc):
    cls = type(exc)
    module = type.__getattribute__(cls, '__module__')
    name = type.__getattribute__(cls, '__name__')
    loaded = sys.modules.get(module) if type(module) is str and module in _MODULES else None
    trusted = (isinstance(loaded, ModuleType) and type(name) is str
               and _IDENT.fullmatch(name) and vars(loaded).get(name) is cls)
    result = {'module': module if trusted else 'unknown',
              'class': name if trusted else 'unknown', 'frames': []}
    if trusted:
        state = type.__getattribute__(cls, '__dict__').get('sqlstate')
        if type(state) is str and re.fullmatch(r'[A-Z0-9]{5}', state):
            result['sqlstate'] = state
    tb = _TRACEBACK.__get__(exc, BaseException)
    scanned = 0
    while tb is not None and scanned < 64:
        scanned += 1
        value = _known_frame(tb.tb_frame, tb.tb_lineno)
        if value is not None:
            result['frames'].append(value)
            result['frames'] = result['frames'][-6:]
        tb = tb.tb_next
    return result


def emit_failure_metadata(exc: BaseException, code: str, boundary: str) -> None:
    """One JSON warning, at most three exceptions/six known frames each.

    This is deliberately separate from client TaskError and commit authority.
    Logging/formatting failures must not change the task's normal disposition.
    """
    try:
        if type(code) is not str or type(boundary) is not str or code not in _CODES or boundary not in ('operation', 'heartbeat'):
            return
        chain = []
        seen = set()
        while isinstance(exc, BaseException) and id(exc) not in seen and len(chain) < 3:
            seen.add(id(exc))
            chain.append(_exception_metadata(exc))
            cause = _CAUSE.__get__(exc, BaseException)
            exc = cause if cause is not None else _CONTEXT.__get__(exc, BaseException)
        record = {'event': 'worker_failure_metadata', 'boundary': boundary,
                  'mapped_code': code, 'exceptions': chain}
        text = json.dumps(record, separators=(',', ':'), ensure_ascii=True)
        if len(text) <= 16384:
            _LOG.warning('%s', text)
    except Exception:
        # No exception instance, message, args, local, query, URL or absolute
        # path is ever passed to logging, including this fallback.
        return
