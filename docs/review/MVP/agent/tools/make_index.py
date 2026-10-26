#!/usr/bin/env python3
"""Regenerate agent/INDEX.md from the cards (stages by id prefix, order = topological by `depends`).

  python3 docs/review/MVP/agent/tools/make_index.py            # rewrite INDEX.md
  python3 docs/review/MVP/agent/tools/make_index.py --check    # exit 1 if INDEX.md is stale
"""
import pathlib, re, sys

AGENT = pathlib.Path(__file__).resolve().parent.parent
TASKS = AGENT / 'tasks'
STAGES = [
 ('A', 'Проверить то, что уже внесено в волне 1 (тесты и рефакторинг без изменения поведения)', r'^(W-1[ab]|W-3a|B-0a|PT-1t|PT-3t|P-1[b-f]|P-4b)$'),
 ('B', 'Надёжность worker по VPN', r'^W-(2|3[bcd]|4|5|6|12)'),
 ('C', 'Backend: лимиты, метрики, профиль, аудит', r'^(B-1|B-2|B-4|B-5a$|B-8a|O-2a|X-7a)'),
 ('C2', 'Backend: механическое разрезание `catalogRoutes` (B-5, инструмент `move_routes.py`, цепочка снизу вверх)', r'^B-5(p|m-)'),
 ('D', 'Портал', r'^(PT-)'),
 ('E', 'Фотограф: инструменты, темы, журнал мутаций, `OwnerAPI`', r'^(P-3a|P-6|P-7a|P-8a|P-5|P-4)'),
 ('F', 'Worker: `build_worker` (W-9, инструмент `extract_stmts.py`) и control-plane (W-7/W-8)', r'^W-(9|7|8)'),
 ('G', 'Эксплуатация: сети, мониторинг, бэкапы, runbooks, секреты, baseline миграций', r'^(O-|X-8|B-3|T-1[ab]$)'),
 ('H', 'E2E: каркас и сценарии (recon → spec)', r'^T-'),
 ('I', 'Chaos-тесты сети (нужен стенд для реального прогона, иначе skip)', r'^W-11'),
 ('J', 'Крупные функции по плану (анализ кусками → план → карточки генерирует `cards_from_plan.py`)', r'^(P-9|B-6|B-7)'),
]

def load():
    cards = {}
    for p in sorted(TASKS.glob('*.md')):
        t = p.read_text(encoding='utf-8', errors='replace')
        m = re.match(r'---\n(.*?)\n---\n', t, re.S)
        meta = {}
        for l in m.group(1).splitlines():
            if re.match(r'^[a-z]+:', l):
                k, v = l.split(':', 1); meta[k] = v.strip()
        deps = re.findall(r'[A-Za-z0-9-]+', meta.get('depends', '').strip('[]'))
        cards[meta['id']] = dict(title=meta['title'], size=meta.get('size', '?'), deps=deps)
    return cards

def topo(cards):
    done, out = set(), []
    pending = sorted(cards)
    while pending:
        progress = False
        for c in list(pending):
            if all(d in done or d not in cards for d in cards[c]['deps']):
                out.append(c); done.add(c); pending.remove(c); progress = True
        if not progress: out += pending; break
    return out

def render():
    cards = load(); order = topo(cards)
    lines = ['# Очередь задач для кодогенерации (порядок выполнения)', '',
             'Файл сгенерирован `tools/make_index.py` (порядок внутри этапа = топологический по `depends`). Всего карточек: **%d**.' % len(cards), '',
             'Размер: **XS** ≈ 1 правка / ≤ 60 строк, **S** ≈ 1–3 файла / ≤ 200 строк. Карточки с инструментом (`move_routes.py`, `move_methods.py`, `extract_stmts.py`, `cards_from_plan.py`) модель не переписывает код, а запускает команду. Что остаётся человеку - в [NOT-CODEGEN.md](NOT-CODEGEN.md).', '',
             'Карточка запускается, когда выполнены все `depends`; после каждой зелёной - `git commit -am "agent(<id>): <title>"`.', '']
    used = set()
    for key, title, rx in STAGES:
        ids = [c for c in order if re.search(rx, c) and c not in used]
        if not ids: continue
        used.update(ids)
        lines += ['## Этап %s — %s' % (key, title), '', '| Карточка | Размер | Что | После |', '|---|---|---|---|']
        for c in ids:
            lines.append('| `%s` | %s | %s | %s |' % (c, cards[c]['size'], cards[c]['title'].replace('|', '/'), ' '.join('`%s`' % d for d in cards[c]['deps']) or '—'))
        lines.append('')
    rest = [c for c in order if c not in used]
    if rest:
        lines += ['## Прочее', '', '| Карточка | Размер | Что |', '|---|---|---|'] + ['| `%s` | %s | %s |' % (c, cards[c]['size'], cards[c]['title']) for c in rest] + ['']
    lines += ['## Ворота для человека внутри очереди', '',
              '- `P-4d` не стартует, пока в `notes/P-4-contract.md` нет строки `APPROVED-BY: <имя>` (чек-лист H-P4).',
              '- `P-9-plan`, `B-6-plan`, `B-7-plan` создают план; человек проверяет его и запускает `cards_from_plan.py` (чек-листы H-PLAN) - только после этого появляются карточки `P-9x-*`, `B-6x-*`, `B-7x-*`.',
              '- Карточки W-11c…k и T-2 (spec) без стенда завершаются как «skip»/«UNKNOWN»: это нормальный результат, стенд поднимает человек (H-W11, H-T1).',
              '']
    return '\n'.join(lines)

if __name__ == '__main__':
    text = render()
    target = AGENT / 'INDEX.md'
    if '--check' in sys.argv:
        sys.exit(0 if target.read_text() == text else 1)
    target.write_text(text); print('INDEX.md written,', text.count('\n| `'), 'rows')
