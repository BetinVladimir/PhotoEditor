#!/usr/bin/env python3
"""Extract a run of top-level statements of a long Python function into a new module-level function.

Deterministic "extract function" refactoring for cards W-9* (and any other long function). No model involved:
it computes the parameters (names the slice reads that are bound before it) and the results (names the
slice binds that are read afterwards), copies the statements verbatim, and replaces them by one call.

Usage (repo root):
  python3 docs/review/MVP/agent/tools/extract_stmts.py --file workers/photo_worker/runtime/service.py \
      --func build_worker --name _validate_learning_evaluation \
      --first 'if learning_evaluation_installation is not None:' \
      --last  'if learning_evaluation_installation is not None:' [--dry-run]

--first / --last  unique substrings of the line on which the first / last statement of the slice STARTS
                  (the slice covers whole top-level statements of the function body, from first to last)
Refuses (exit 2, nothing written) on: return/yield/await/global/nonlocal in the slice, break/continue that
would escape, names that may be unbound after the slice, ambiguous anchors.
"""
import argparse
import ast
import builtins
import pathlib
import sys


def die(msg):
    print('REFUSED: ' + msg, file=sys.stderr)
    sys.exit(2)


class Names(ast.NodeVisitor):
    """Loads and stores of names in a statement list, descending into nested scopes for loads only."""

    def __init__(self, nested=False):
        self.nested = nested
        self.loads, self.stores, self.first_store_top = set(), set(), {}
        self.closure_loads = set()
        self.bad = []
        self._depth = 0  # >0 inside nested def/lambda/class
        self._loops = 0

    def visit_Name(self, n):
        if isinstance(n.ctx, ast.Load):
            self.loads.add(n.id)
        elif self._depth == 0:
            self.stores.add(n.id)
        else:
            pass  # stores inside nested scopes are local to them
    def visit_AugAssign(self, n):
        if isinstance(n.target, ast.Name):
            self.loads.add(n.target.id)
            if self._depth == 0:
                self.stores.add(n.target.id)
        self.generic_visit(n)

    def _scope(self, n):
        """Visit a nested scope; only names it reads from the OUTER scope are added to self.loads."""
        child = Names(nested=True)
        local = set()
        if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef, ast.Lambda)):
            ar = n.args
            local |= {x.arg for x in ar.posonlyargs + ar.args + ar.kwonlyargs}
            for x in (ar.vararg, ar.kwarg):
                if x:
                    local.add(x.arg)
            for d in list(ar.defaults) + [d for d in ar.kw_defaults if d is not None]:
                self.visit(d)
            body = n.body if isinstance(n.body, list) else [n.body]
            for st in body:
                child.visit(st)
            local |= child.stores
        elif isinstance(n, ast.ClassDef):
            for st in n.body:
                child.visit(st)
            local |= child.stores
        else:  # comprehensions
            for g in n.generators:
                local |= target_names(g.target)
                child.visit(g.iter)
                for c in g.ifs:
                    child.visit(c)
            for part in ([n.key, n.value] if isinstance(n, ast.DictComp) else [n.elt]):
                child.visit(part)
        self.loads |= child.loads - local
        lazy = isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef, ast.Lambda, ast.GeneratorExp))
        self.closure_loads |= ((child.loads - local) if lazy else set()) | child.closure_loads

    def visit_FunctionDef(self, n):
        if self._depth == 0:
            self.stores.add(n.name)
        for d in n.decorator_list:
            self.visit(d)
        self._scope(n)

    visit_AsyncFunctionDef = visit_FunctionDef

    def visit_ClassDef(self, n):
        if self._depth == 0:
            self.stores.add(n.name)
        self._scope(n)

    def visit_Lambda(self, n):
        self._scope(n)

    def visit_ListComp(self, n):
        self._scope(n)

    visit_SetComp = visit_DictComp = visit_GeneratorExp = visit_ListComp

    def visit_Import(self, n):
        for a in n.names:
            if self._depth == 0:
                self.stores.add((a.asname or a.name).split('.')[0])

    visit_ImportFrom = visit_Import

    def visit_ExceptHandler(self, n):
        if n.name and self._depth == 0:
            self.stores.add(n.name)
        self.generic_visit(n)

    def visit_For(self, n):
        self._loops += 1
        self.generic_visit(n)
        self._loops -= 1

    visit_While = visit_AsyncFor = visit_For

    def visit_Return(self, n):
        if not self.nested:
            self.bad.append('return at line %d' % n.lineno)
        self.generic_visit(n)

    def visit_Yield(self, n):
        if not self.nested:
            self.bad.append('yield at line %d' % n.lineno)

    visit_YieldFrom = visit_Await = visit_Yield

    def visit_Global(self, n):
        self.bad.append('global at line %d' % n.lineno)

    visit_Nonlocal = visit_Global

    def visit_Break(self, n):
        if not self.nested and self._loops == 0:
            self.bad.append('break outside a loop of the slice at line %d' % n.lineno)

    visit_Continue = visit_Break


ALL = None  # "every name": used for branches that cannot fall through


def target_names(t):
    return {x.id for x in ast.walk(t) if isinstance(x, ast.Name)}


def definite(stmts):
    """Names that are certainly bound after the statement list ran normally (None = cannot fall through)."""
    out = set()
    for st in stmts:
        if isinstance(st, ast.Assign):
            for t in st.targets:
                out |= target_names(t)
        elif isinstance(st, ast.AugAssign) and isinstance(st.target, ast.Name):
            out.add(st.target.id)
        elif isinstance(st, ast.AnnAssign) and st.value is not None and isinstance(st.target, ast.Name):
            out.add(st.target.id)
        elif isinstance(st, (ast.Import, ast.ImportFrom)):
            out |= {(al.asname or al.name).split('.')[0] for al in st.names}
        elif isinstance(st, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
            out.add(st.name)
        elif isinstance(st, (ast.Raise, ast.Return)):
            return ALL
        elif isinstance(st, ast.If):
            x, y = definite(st.body), definite(st.orelse) if st.orelse else set()
            if x is ALL and y is ALL:
                return ALL
            if x is ALL:
                out |= y
            elif y is ALL:
                out |= x
            else:
                out |= (x & y)
        elif isinstance(st, ast.With):
            x = definite(st.body)
            if x is ALL:
                return ALL
            out |= x
        elif isinstance(st, ast.Try):
            if st.finalbody:
                x = definite(st.finalbody)
                if x is not ALL:
                    out |= x
    return out


def analyse(stmts):
    v = Names()
    for s in stmts:
        v.visit(s)
    return v


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--file', required=True)
    ap.add_argument('--func', required=True)
    ap.add_argument('--name', required=True)
    ap.add_argument('--first', required=True)
    ap.add_argument('--last', required=True)
    ap.add_argument('--first-nth', type=int, default=0, help='pick the Nth (1-based) match when the anchor is not unique')
    ap.add_argument('--last-nth', type=int, default=0)
    ap.add_argument('--dry-run', action='store_true')
    a = ap.parse_args()
    path = pathlib.Path(a.file)
    src = path.read_text()
    lines = src.split('\n')
    tree = ast.parse(src)
    fn = next((n for n in tree.body if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef)) and n.name == a.func), None)
    if fn is None:
        die('function %s not found at module level' % a.func)
    if any(isinstance(n, (ast.FunctionDef, ast.ClassDef)) and n.name == a.name for n in tree.body):
        die('%s already exists in the module' % a.name)

    def find(sub, what, nth=0):
        hits = [i for i, st in enumerate(fn.body) if sub in lines[st.lineno - 1]]
        if nth:
            if not 1 <= nth <= len(hits):
                die('%s anchor %r has %d matches, --%s-nth %d is out of range' % (what, sub, len(hits), what.strip('-'), nth))
            return hits[nth - 1]
        if len(hits) != 1:
            die('%s anchor %r matches %d top-level statements (need exactly 1)' % (what, sub, len(hits)))
        return hits[0]

    i0, i1 = find(a.first, '--first', a.first_nth), find(a.last, '--last', a.last_nth)
    if i1 < i0:
        die('--last is above --first')
    stmts = fn.body[i0:i1 + 1]
    start = stmts[0].lineno
    while start - 2 >= 0 and lines[start - 2].strip().startswith('#'):  # leading comments belong to the slice
        start -= 1
    end = stmts[-1].end_lineno
    before, after = fn.body[:i0], fn.body[i1 + 1:]

    inner = analyse(stmts)
    if inner.bad:
        die('; '.join(inner.bad))
    pre = analyse(before)
    bound_before = {x.arg for x in (fn.args.posonlyargs + fn.args.args + fn.args.kwonlyargs)}
    if fn.args.vararg:
        bound_before.add(fn.args.vararg.arg)
    if fn.args.kwarg:
        bound_before.add(fn.args.kwarg.arg)
    bound_before |= pre.stores
    post = analyse(after)
    params = [p for p in sorted(bound_before, key=lambda x: (x not in {y.arg for y in fn.args.args + fn.args.kwonlyargs}, x))
              if p in inner.loads or p in inner.stores]
    # keep original binding order: arguments first (in signature order), then assignments in order of first store
    sig = [x.arg for x in (fn.args.posonlyargs + fn.args.args + fn.args.kwonlyargs)]
    seen, ordered = set(), []
    for n in sig:
        if n in params and n not in seen:
            ordered.append(n)
            seen.add(n)
    for st in before:
        for x in ast.walk(st):
            if isinstance(x, ast.Name) and isinstance(x.ctx, ast.Store) and x.id in params and x.id not in seen:
                ordered.append(x.id)
                seen.add(x.id)
    ordered += [p for p in params if p not in seen]
    params = ordered
    # a name loaded in the slice that is neither bound before, nor stored in the slice, nor a module/builtin name
    # would be a NameError already in the original; ignore.
    rebound_later = post.stores & inner.closure_loads
    if rebound_later:
        die('a closure created in the slice reads %s which the function rebinds afterwards; choose another slice' % sorted(rebound_later))
    needed, bound_so_far = set(), set()
    for st in after:
        imported = {(al.asname or al.name).split('.')[0] for x in ast.walk(st) if isinstance(x, (ast.Import, ast.ImportFrom)) for al in x.names}
        for x in ast.walk(st):  # names bound by the statement itself before they are used inside it
            if isinstance(x, ast.ExceptHandler) and x.name:
                imported.add(x.name)
            elif isinstance(x, (ast.For, ast.AsyncFor)):
                imported |= target_names(x.target)
            elif isinstance(x, ast.With):
                for it in x.items:
                    if it.optional_vars is not None:
                        imported |= target_names(it.optional_vars)
            elif isinstance(x, ast.NamedExpr):
                imported |= target_names(x.target)
        needed |= analyse([st]).loads - imported - bound_so_far
        d = definite([st])
        if d is not ALL:
            bound_so_far |= d
    outs = sorted(n for n in inner.stores if n in needed)
    # possibly-unbound check: a result that is not bound before and not stored at the top level of the slice
    top_stores = definite(stmts)
    for o in outs:
        if o not in bound_before and not (top_stores is ALL or o in top_stores):
            die('result %r is bound only conditionally inside the slice (may be unbound afterwards); choose another slice' % o)
    # results that are bound before AND stored in the slice must also be parameters (so they are bound inside)
    for o in outs:
        if o in bound_before and o not in params:
            params.append(o)
    ret = ''
    call_lhs = ''
    if outs:
        ret = '    return ' + ', '.join(outs) + '\n' if len(outs) > 1 else '    return ' + outs[0] + '\n'
        call_lhs = ', '.join(outs) + ' = '
    body = '\n'.join(lines[start - 1:end])
    newfn = 'def %s(%s):\n%s\n%s' % (a.name, ', '.join(params), body, ret.rstrip('\n'))
    newfn = newfn.rstrip('\n') + '\n\n'
    indent = ' ' * stmts[0].col_offset
    call = '%s%s%s(%s)' % (indent, call_lhs, a.name, ', '.join(params))
    host = fn.decorator_list[0].lineno if fn.decorator_list else fn.lineno
    print('slice lines %d-%d (%d lines) of %s' % (start, end, end - start + 1, a.func))
    print('new function %s(%s)' % (a.name, ', '.join(params)))
    print('results     %s' % (', '.join(outs) or '(none)'))
    print('call        %s' % call.strip())
    if a.dry_run:
        print('dry-run: nothing written')
        return
    out = lines[:host - 1] + newfn.rstrip('\n').split('\n') + ['', ''] + lines[host - 1:start - 1] + [call] + lines[end:]
    new_src = '\n'.join(out)
    try:
        compile(new_src, str(path), 'exec')
    except SyntaxError as e:
        die('result does not compile: %s' % e)
    path.write_text(new_src)
    print('done. Now run the verify commands of the card (pytest).')


if __name__ == '__main__':
    main()
