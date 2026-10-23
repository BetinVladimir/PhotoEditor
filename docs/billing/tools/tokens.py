import re, os
SCALE = float(os.environ.get("TOKEN_SCALE", "1.0"))  # поправка: реальные токены Qwen / оценка (см. QWEN-PROMPT.md §5)
def est(t):
    """Консервативная оценка токенов: кириллица ~2.2 симв/токен, остальное ~3.2. Токенайзер Qwen не проверялся."""
    cyr = len(re.findall(r'[А-Яа-яЁё]', t))
    return int((cyr / 2.2 + (len(t) - cyr) / 3.2) * SCALE) + 1
def sections(text):
    """[(заголовок, уровень, текст)] по заголовкам ## и ###; текст включает сам заголовок."""
    out = []; cur = None; incode = False
    for line in text.split("\n"):
        if line.strip().startswith("```"): incode = not incode
        m = None if incode else re.match(r'^(#{1,3}) (.+)$', line)
        if m:
            if cur: out.append(cur)
            cur = [m.group(2).strip(), len(m.group(1)), line + "\n"]
        elif cur: cur[2] += line + "\n"
        else: cur = ["(начало)", 0, line + "\n"]
    if cur: out.append(cur)
    return [tuple(x) for x in out]
