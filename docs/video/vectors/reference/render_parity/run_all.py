"""Паритетные проверки рендера (документ 24, раздел 9): компилятор -> реальный ffmpeg -> сравнение с composeAt (FN-E-04).
Запуск: python3 run_all.py [рабочий_каталог]. Каждый сценарий печатает 'BAD <n>'; итог 0 = успех. Требуется ffmpeg 6.1.x в PATH."""
import os, re, subprocess, sys
here = os.path.dirname(os.path.abspath(__file__))
work = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else 'work')
os.makedirs(work, exist_ok=True)
subprocess.run([sys.executable, os.path.join(here, 'gen_sources.py'), work], check=True)
env = dict(os.environ, PYTHONPATH=os.pathsep.join([here, os.path.join(here, '..')]))
total = 0
for name in sorted(f for f in os.listdir(here) if re.match(r'g\d\d_.*\.py$', f)):
    r = subprocess.run([sys.executable, os.path.join(here, name)], cwd=work, env=env, capture_output=True, text=True)
    bad = [int(x) for x in re.findall(r'^BAD (\d+)\s*$', r.stdout, re.M)]
    n = sum(bad) if bad else (0 if r.returncode == 0 and 'g14' not in name else 0)
    ok = r.returncode == 0
    print(f'{name}: {"ok" if ok and n == 0 else "FAIL"} (BAD={n}, rc={r.returncode})')
    if not ok:
        print(r.stdout[-800:], r.stderr[-800:])
    total += n + (0 if ok else 1)
print('TOTAL FAIL', total)
sys.exit(1 if total else 0)
