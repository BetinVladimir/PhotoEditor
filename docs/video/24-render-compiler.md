# 24. Компилятор рендера: точные цепочки ffmpeg и проверенные эталоны

Статус: спроектировано, **эталон исполнен и проверен на реальном ffmpeg 6.1.1** (2026-10-22). Нормативный документ. Начало: [README](README.md). Закрывает пробел, найденный четвёртым ревью пригодности: в [07](07-timeline-edl-and-render-spec.md) раздел 6 и FN-R-01…R-03 компилятор графа был определён только для простых клипов (эталоны G1 «склейка + усиление» и G2 «кроссфейд» без преобразований), а перекладка, поворот, обрезка, движение фото, скорость, цвет, наложение дорожек и ducking сводились к фразе «по правилам». Слабая модель выбрала бы произвольные цепочки, и рендер разошёлся бы с предпросмотром ([23](23-editor-playback-timeline.md): `composeAt`).

| Пробел | Раздел |
|---|---|
| нет схемы «несколько дорожек → один кадр» и порядка слоёв | 4, 5 |
| нет цепочек для подгонки, преобразования, обрезки, поворота, Кена Бёрнса | 5 |
| переходы (`crossfade`, `dip_to_black`, `fade_in`) выражались через `xfade` без учёта слоёв | 5.5 |
| нет цепочек скорости, звуковых огибающих, шины и ducking | 6 |
| нет двухпроходной громкости на графе, а не на файле | 7 |
| нет проверки «рендер = предпросмотр» | 9 |

Приоритет: схема документа — [07](07-timeline-edl-and-render-spec.md) и DS-09; **состав кадра и звука в момент `t` — `composeAt` ([23](23-editor-playback-timeline.md), FN-E-04)**; строки графа ffmpeg — этот документ. При расхождении с текстом G1/G2 в [`vectors/golden_render.md`](vectors/golden_render.md) и с FN-R-02/R-03 побеждает этот документ; графы G1/G2 теперь порождаются компилятором (раздел 8).

Обозначения: `TPS = 705 600 000`; `F` — тики кадра монтажа (FN-T-01); `W×H` и `fps` — из пресета; `sec(t)` — `fmtSeconds(t)` (раздел 3); `S` — число в формате `fmtNum`.

## 1. Решения

| ID | Решение | Почему |
|---|---|---|
| D-37 | Один компилятор для всех документов: **чёрный холст + цепочка `overlay`** по слоям в порядке дорожек и клипов. `concat` и `xfade` не используются. | `xfade` склеивает только два соседних потока и не умеет слои, прозрачные поля и переход на верхней дорожке; одна схема для склейки, перехода и наложения исключает ветвление. |
| D-38 | Один `-i` на **использование клипа**, опции входа `-ss`/`-t` в секундах источника (раздел 4); неиспользуемый поток отключается (`-vn`/`-an`); фото — `-loop 1 -framerate fps -t`. | Нет `trim`/`atrim` внутри графа и повторного декодирования всего файла; клип с речью и фоновая музыка из одного файла — два входа. |
| D-39 | Слой начинается на сетке кадров монтажа: начало округляется **вверх** до кратного `F`; точка конца берётся из `voice_span` без округления. | Иначе `overlay` показывает первый кадр слоя полупрозрачным на полкадра раньше; остаточная погрешность — не более половины кадра прозрачности (раздел 9, допуск 9 уровней). |
| D-40 | Прозрачность — настоящий альфа-канал: слой переводится в `yuva420p` (в `rgba` для Кена Бёрнса), поля `fit` — `color=black@0`. Переход `crossfade`/`fade_in` — `fade=…:alpha=1` у входящего слоя. | Совпадает с `composeAt`: непрозрачность слоя = коэффициент перехода, исходящий слой остаётся под ним до конца. |
| D-41 | `dip_to_black` — `fade` по **яркости**, но на время фильтра диапазон растягивается (`scale=in_range=tv:out_range=pc` … `scale=in_range=pc:out_range=tv`). | `fade` умножает Y и (U−128, V−128); без растяжения чёрный получается Y=0 вместо 16 (брак вещания); измерено. |
| D-42 | Порядок геометрии: `crop` → возврат `pad` на размер холста → `scale` → `rotate` (`c=none`) → смещение в `overlay`. Смещение `position` — доля размера холста. | Вращение вокруг центра слоя после масштаба; тот же порядок в мониторе ([23](23-editor-playback-timeline.md) раздел 4). |
| D-43 | Кен Бёрнс — `zoompan` с `d=1` на суперсэмплированном холсте (коэффициент 4, но сторона ≤ 8192); выражения `z`, `x`, `y` порождаются из тех же параметров, что FN-E-03; окно зажато в изображение. | `zoompan` с `d>1` создаёт собственный темп кадров и дрожание; `d=1` + `on` даёт ровно один выходной кадр на входной кадр. |
| D-44 | Цвет: 3D-LUT применяется в RGB с **явными** преобразованиями Rec.709 ограниченного диапазона (`scale=in_color_matrix=bt709:in_range=tv,format=rgba,lut3d…,scale=out_color_matrix=bt709:out_range=tv`). Имя файла — `lut_<16 hex>.cube`, где 16 hex = SHA-256 канона включённых `color.*` эффектов клипа. HDR (`hdr = pq|hlg`) — тональное отображение `zscale`+`tonemap=hable:desat=0` до всех остальных шагов, решение `HDR_TONEMAPPED` в `decisions`; прочие значения `hdr` — `COMPILE_COLOR_UNSUPPORTED`. | Неявные преобразования ffmpeg дают сдвиг цвета (измерено); [07](07-timeline-edl-and-render-spec.md) п. 3 требовал тональное отображение, теперь оно задано цепочкой. |
| D-45 | Звук: **голос** на использование клипа: `asetpts`, `aresample=48000`, моно → стерео `pan=stereo\|c0=c0\|c1=c0`, `aformat`, `atempo`, `volume`, `afade` по таблице FN-E-05, `adelay=<отсчёты>S:all=1`. Смешивание `amix=…:normalize=0:duration=first` с тихой базой. Звуковой кроссфейд — пара `afade … curve=tri` (вход B и выход A), **не** `acrossfade`. | Моно → стерео через `pan` не даёт −3 дБ (проверено против Web Audio); `normalize=0` исключает автоматическое деление громкости; `adelay` в отсчётах — точное положение; `acrossfade` сдвигает позицию и не работает со слоями. Уточняет D-28. |
| D-46 | Ducking — `sidechaincompress`. Боковая цепь — **шина** из голосов дорожек `sidechain_tracks` *до* их собственного ducking. Параметры из `duck_params` (раздел 6.3). Допуск измеренного ослабления для `depth_db = 12`: [8; 13] дБ. | Один эффект нельзя применить к самому себе; фильтр имеет предел `ratio ≤ 20`, поэтому глубина 12 дБ достижима с допуском. |
| D-47 | Громкость: проход 1 строит **аудио-граф** (`mode = 'audio'`: без видеоцепочек), проход 2 — полный граф + `loudnorm` с измеренными значениями (`linear=true`) + `alimiter`. | Проход 1 не должен кодировать видео; одни и те же голоса в обоих проходах. |
| D-48 | Эталон проверяется на **ffmpeg 6.1.x** (`tool_versions.ffmpeg`), `compiler_version = "render-compiler/1"`; смена минорной версии ffmpeg требует повторного прогона `render_parity` (раздел 9). Необходимые фильтры: `overlay`, `fade`, `scale`, `pad`, `crop`, `rotate`, `zoompan`, `lut3d`, `zscale`, `tonemap`, `fps`, `setpts`, `format`, `color`, `setparams`, `asetpts`, `aresample`, `aformat`, `pan`, `atempo`, `volume`, `afade`, `adelay`, `amix`, `anullsrc`, `atrim`, `asplit`, `sidechaincompress`, `loudnorm`, `alimiter`. | Поведение `fade`, `zoompan`, `sidechaincompress` менялось между 4.x и 6.x; на устройстве разработчика может стоять 4.4 — паритетный прогон на нём недействителен. |

## 2. Вход и выход

`compile_graph(doc, preset, runtime, mode = 'full') → plan`; FN-R-01 теперь — тонкая обёртка над FN-R-15.

- `doc` — документ монтажа (DS-09), уже прошедший проверку VT; `preset` — `{width, height, fps?}`; `fps` пресета обязан равняться `doc.video.fps`, иначе `COMPILE_FPS_UNSUPPORTED` (смена частоты — V2).
- `runtime.sources[id]` для `media_id` или `asset_version_id`: `{duration: тики, has_audio: bool, has_video?: bool, channels?: 1|2, matrix?: bt709|bt601|smpte170m|bt470bg, hdr?: pq|hlg, path (не входит в граф)}`. Значения берёт воркер из `video_probe` (DS-12).
- `plan`: `inputs[{index, clip, options}]`, `filter_complex` (строки соединены `;\n`), `maps` (`["[vout]","[abus]"]`, в режиме `audio` — `["[abus]"]`), `decisions`, `luts` (имена файлов; содержимое строят FN-L-05…08 из `color.*` эффектов клипа или мастера), `duration_ticks = doc_end(doc)`. Канонический JSON плана — часть `render_fingerprint` (FN-R-05).

DS-15 дополняется: `PlanInput.options: list[str]` (опции входа до `-i`); `RenderPlan.luts: list[str]`; `compiler_version: str`. Путь файла в плане не хранится: воркер подставляет его при сборке команды (FN-R-17).

## 3. Числа и время (FN-R-08)

Строки графа обязаны быть одинаковыми на любой машине, поэтому вещественная арифметика запрещена.

- `fmtSeconds(ticks)` — `ticks / TPS` как точная десятичная дробь, 9 знаков, округление «половина вверх», без хвостовых нулей и точки (`705600000 → "1"`, `1764000000 → "2.5"`, `1 → "0.000000001"`).
- `fmtNum(x, digits = 6)` — число с `digits` знаками, «половина вверх», без экспоненты и хвостовых нулей, `-0 → "0"`. Вещественное `x` сначала переводится в десятичную строку по `repr` (Python) / кратчайшему круговому представлению (TS).
- `fpsExpr(fps)` — `"25"` при `den = 1`, иначе `"30000/1001"`.
- `samplesAt(ticks)` — `floor(ticks × 48000 / TPS + ½)` целочисленно.
- `atempoChain(num, den)` — список множителей из `[0.5; 2]`: пока `t > 2` — `2`, пока `t < ½` — `0.5`, затем остаток `fmtNum(t, 9)`; для `1/1` пусто. Скорость вне `[¼; 4]` документ не допускает (VT).

Векторы: `compile.json → fmt_seconds` (13), `fmt_num` (10), `samples_at` (6), `atempo_chain` (8).

## 4. Входы (FN-R-09)

`planItems(doc, srcDur, sources)` обходит дорожки сверху вниз по порядку документа и клипы по порядку. Для каждого клипа: `(s, e) = voice_span(doc, ti, ci, srcDur)` — отрезок во времени монтажа с запасами перехода (FN-E-05). Клип попадает в план, если нужно видео или звук:

- видео нужно: `source.kind ∈ {video, photo}`, дорожка видео и `visible ≠ false`, у источника есть видеопоток; при `hdr ∉ {pq, hlg, пусто}` — `COMPILE_COLOR_UNSUPPORTED:<clip>`;
- звук нужен: `source.kind ∈ {video, audio}`, у источника есть звук, не `audio.mute`, звуковая дорожка не `muted`. Скрытая видеодорожка звук **не** отключает.

Нет источника в `sources` — `COMPILE_SOURCE_MISSING:<clip>`. Клипов в плане больше `RENDER_MAX_INPUTS = 128` — `COMPILE_TOO_MANY_INPUTS`.

`inputOptions` (в этом порядке):

1. фото: `-loop 1 -framerate <fpsExpr> -t <sec(e − s)>` — других опций нет;
2. иначе: `-vn` (если видео не нужно), `-an` (если звук не нужен), `-ss <sec(max(0, srcTicksAt(c, s)))>`, `-t <sec(srcTicksAt(c, e) − ss)>`.

Режим `mode = 'audio'`: берутся только клипы со звуком, видео везде отключено.

## 5. Видео

### 5.1 Холст и порядок слоёв

Холст: `color=c=black:s=<W>x<H>:r=<fpsExpr>:d=<sec(D)>,format=yuv420p,setparams=colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv[bg]`, `D = doc_end(doc)`. Далее для каждого видео-использования в порядке плана: `[prev][vcN]overlay=x='(W-w)/2+(<px>)*W':y='(H-h)/2+(<py>)*H':eof_action=pass:enable='between(t,<sec(s')>,<sec(e)>)':format=auto[ovN]`, где `s'` — `s`, округлённое вверх до кратного `F` (D-39), `(px, py) = transform.position` (0, 0 без преобразования), `fmtNum` — для долей. Выход: `[ovLast]format=yuv420p[vout]` или, при мастер-LUT, раздел 5.6. Пустой документ (`D = 0`) не компилируется: `COMPILE_EMPTY`.

Слоёв одновременно больше 8 в какой-либо точке (проверка на границах клипов и переходов через `compose_at.over_limit`) — `COMPILE_TOO_MANY_LAYERS`.

### 5.2 Цепочка клипа (FN-R-10)

Шаги по порядку; каждый присутствует только когда его условие верно. Вход `[i:v]`, выход `[vc<i>]`.

1. **Время.** `setpts=PTS-STARTPTS`; для видео с скоростью `num/den ≠ 1` — `setpts=(PTS-STARTPTS)*<den>/<num>`; `fps=<fpsExpr>`.
2. **HDR** (только `hdr ∈ {pq, hlg}` и видео): `zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p`; матрица далее — `bt709`.
3. **Подгонка** (`fit`), с `cm = in_color_matrix=<m>:out_color_matrix=bt709:out_range=tv` (`m` из `matrix`; неизвестная — `COMPILE_COLOR_UNSUPPORTED`):
   - `fit`: `scale=<CW>:<CH>:force_original_aspect_ratio=decrease:force_divisible_by=2:<cm>`;
   - `fill`: `scale=<CW>:<CH>:force_original_aspect_ratio=increase:force_divisible_by=2:<cm>,crop=<CW>:<CH>`;
   - `none`: `scale=<cm>` (при суперсэмплировании `scale=w=trunc(iw*S/2)*2:h=trunc(ih*S/2)*2:<cm>`), затем `crop=w='min(iw,<CW>)':h='min(ih,<CH>)'`.

   `CW×CH = W×H`, а для фото с движением — `W·S × H·S`, где `S = kbSupersample(W, H)`: 4, уменьшается на 1, пока `max(W,H)·S > 8192`.
4. **Формат и поля.** `format=yuva420p` (`rgba` для фото с движением), затем `pad=<CW>:<CH>:(ow-iw)/2:(oh-ih)/2:color=black@0`.
5. **Движение** (только фото с `motion`): `zoompan=z='<z>':x='<x>':y='<y>':d=1:s=<W>x<H>:fps=<fpsExpr>,scale=out_color_matrix=bt709:out_range=tv,format=yuva420p`. Выражения (раздел 5.4).
6. **Цвет клипа.** Если у клипа есть включённые `color.*` — `scale=in_color_matrix=bt709:in_range=tv,format=rgba,lut3d=file='<lut>':interp=tetrahedral,scale=out_color_matrix=bt709:out_range=tv,format=yuva420p`.
7. **Преобразование** `transform` (раздел 5.3).
8. **Переход** (раздел 5.5).
9. **Положение во времени.** `setpts=PTS-STARTPTS+<sec(s')>/TB`.

### 5.3 Преобразование

- Обрезка `crop = [l, t, r, b]` (доли, не все нули): `crop=w=iw*(1-<l>-<r>):h=ih*(1-<t>-<b>):x=iw*<l>:y=ih*<t>`, затем `pad=<W>:<H>:<W>*<l>:<H>*<t>:color=black@0` — слой возвращается на холст в то же место.
- Масштаб `scale ≠ 1`: `scale=w='trunc(iw*<s>/2)*2':h='trunc(ih*<s>/2)*2'`.
- Поворот `rotate ≠ 0` (градусы): `rotate=a=<a>*PI/180:ow='rotw(<a>*PI/180)':oh='roth(<a>*PI/180)':c=none` — свободные углы прозрачны.
- Смещение — в `overlay` (раздел 5.1).

### 5.4 Кен Бёрнс (FN-R-11)

`kbExpr(clip, headFrames, nFrames)` возвращает `z`, `x`, `y`. `headFrames = (clip.at − s) / F` (кадры запаса перед клипом), `nFrames = dur(clip) / F`.

```
u = min(max((on-<head>)/<n>,0),1)
e = u                        при easing = linear
e = (u*u*(3-2*u))            при easing = ease_in_out        (FN-E-03: 3u² − 2u³)
lerp(a,b) = (<a>+(<b>-(<a>))*e)
z = lerp(from.scale, to.scale)
x = max(0,min(iw-iw/zoom,<lerp(from.center[0], to.center[0])>*iw-iw/zoom/2))
y = max(0,min(ih-ih/zoom,<lerp(from.center[1], to.center[1])>*ih-ih/zoom/2))
```

`<a>`, `<b>` — `fmtNum`; `u` записывается раскрытым в каждом месте (без переменных). Векторы: `compile.json → kb_expr` (2), `kb_supersample` (4).

### 5.5 Переходы

`et = effectiveTransition(doc, ti, ci, srcDur)` (FN-E-02), `d = et.duration`; следующий клип — `en` (тот же вызов для `ci + 1`).

| Вид | Входящий слой (этот клип) | Исходящий слой (предыдущий клип) |
|---|---|---|
| `cut` | — | — |
| `crossfade` | `fade=t=in:st=0:d=<sec(d)>:alpha=1` | без изменений, остаётся под входящим до своего конца |
| `fade_in` | `fade=t=in:st=0:d=<sec(d)>:alpha=1` | — (слой снизу или холст) |
| `dip_to_black` | `fadeBlack('in', 0, d/2)` | `fadeBlack('out', en.start − s', d/2)` |

`fadeBlack(dir, st, dur)` = `scale=in_range=tv:out_range=pc,fade=t=<dir>:st=<sec(st)>:d=<sec(dur)>,scale=in_range=pc:out_range=tv` (D-41). Если `en.kind = dip_to_black`, её исходящая половина добавляется к **текущему** клипу после его собственного перехода.

### 5.6 Мастер-цвет

Если у `doc.master.effects` есть включённые `color.*`: вместо `[ovLast]format=yuv420p[vout]` — `[ovLast]scale=in_color_matrix=bt709:in_range=tv,format=rgba,lut3d=file='<lut>':interp=tetrahedral,scale=out_color_matrix=bt709:out_range=tv,format=yuv420p[vout]`. Клип и мастер с одной LUT дают двойное применение (проверено: инверсия дважды = исходный цвет).

### 5.7 Имя файла LUT (FN-R-14)

`lutName(effects)`: выбрать эффекты `type` на `color.` с `enabled ≠ false`; пусто → нет LUT; иначе `"lut_" + sha256(canon(список))[:16] + ".cube"` (`canon` — JCS, FN-C-01). Содержимое строит `foldEffectsToLut` (FN-L-08) и записывает рядом с командой; имена уникальны по содержимому, поэтому кэш безопасен.

## 6. Звук

### 6.1 Голос (FN-R-12)

Вход `[i:a]`. Шаги: `asetpts=PTS-STARTPTS`, `aresample=48000`, при `channels = 1` — `pan=stereo|c0=c0|c1=c0`, `aformat=sample_fmts=fltp:channel_layouts=stereo`, `atempo=<f>` для каждого множителя `atempoChain`, `volume=<g>dB:precision=float` (`g` = `audio.gain_db` + `track.gain_db` для звуковой дорожки; `0` — шаг пропускается; `fmtNum(g, 4)`), собственные затухания клипа, затухания переходов, `adelay=<samplesAt(s)>S:all=1`.

Собственные затухания (`s` — начало отрезка голоса): `afade=t=in:st=<sec(c.at − s)>:d=<sec(fade_in)>:curve=tri`; `afade=t=out:st=<sec(end(c) − fade_out − s)>:d=<sec(fade_out)>:curve=tri`. Затухания переходов видеодорожки: у входящего `crossfade` — `afade=t=in:st=0:d=<sec(d)>:curve=tri`, у исходящего (по `en`) — `afade=t=out:st=<sec(en.start − s)>:d=<sec(d)>:curve=tri`; у `dip_to_black` — по половине `d/2` на вход и выход. Это та же огибающая, что FN-E-05; сумма линейных входящей и исходящей ветвей постоянна (D-28).

### 6.2 Смешивание

Каждый голос получает метку `[av<i>]`. Тишина: `anullsrc=r=48000:cl=stereo,atrim=duration=<sec(D)>,asetpts=PTS-STARTPTS[silN]` (новая метка на каждое использование). Итог: `[sil][av0][av1]…amix=inputs=<n+1>:normalize=0:duration=first[abus]`. Порядок голосов — порядок плана; метки после ducking идут вместо исходных.

### 6.3 Ducking (FN-R-13)

Эффект `audio.duck` у клипа `X` с `sidechain_tracks = [T…]`:

1. Каждый голос, лежащий на дорожке из `T`, разветвляется: `<голос>,asplit=2[av<i>][bs<i>]`.
2. Шина дорожки `T`: `[silN][bs…]amix=inputs=<k+1>:normalize=0:duration=first,asplit=<n>[bus_T_0]…[bus_T_{n-1}]`, где `n` — число клипов, ссылающихся на `T`.
3. Если сторон несколько — свои шины смешиваются ещё раз: `[silN][bus…]amix=inputs=<m+1>:normalize=0:duration=first[scm<i>]`.
4. `[<голос X>][<шина>]sidechaincompress=threshold=<thr>:ratio=<r>:attack=<a>:release=<rel>:makeup=1:level_sc=1[du…]`.

`duckParams(threshold_db, depth_db, attack_ms, release_ms)`:
`thr = clamp(10^(threshold_db/20), 0.000976563, 1)`; `ratio = 1` при `depth ≤ 0`, `20` при `depth ≥ 11.4`, иначе `1/(1 − depth/12)`; `attack = clamp(attack_ms, 0.01, 2000)`, `release = clamp(release_ms, 0.01, 9000)`; `thr` — `fmtNum(·, 9)`, `ratio` — `fmtNum(·, 6)`, времена — `fmtNum(·, 3)`. Векторы: `compile.json → duck_params` (6).

Измерено на G8 (музыка 880 Гц, речь 220 Гц с 2 по 5 с, порог −30 дБ, глубина 12 дБ, атака 20 мс, восстановление 250 мс): без речи 0,0443, под речью 0,0131 — **ослабление 10,6 дБ**; после речи уровень возвращается к 0,0443 (допуск ±0,002).

## 7. Громкость

Цель задана (`LUFS`, `TP`) → FN-R-07 на графе:

1. Проход 1: `compile_graph(…, mode='audio')`; к графу добавляется `;[abus]loudnorm=I=<T>:TP=<TP>:LRA=11:print_format=json[aout]`; команда — `-map [aout] -f null -`; читается последний JSON из stderr.
2. Проход 2: полный граф + `;[abus]loudnorm=I=<T>:TP=<TP>:LRA=11:measured_I=<input_i>:measured_TP=<input_tp>:measured_LRA=<input_lra>:measured_thresh=<input_thresh>:offset=<target_offset>:linear=true:print_format=summary,alimiter=limit=<10^(TP/20), 6 знаков>:level=disabled[aout]`, `-map [vout] -map [aout]`.

Измерено на G14 (документ G8, цель −14 LUFS, −1 дБTP): итог −13,95 LUFS (допуск ±0,5). Без цели: `-map [abus]` без `loudnorm`.

## 8. Ошибки, решения, сборка команды

Ошибки компилятора (исключение `CompileError`, код — до двоеточия, далее идентификатор): `COMPILE_SOURCE_MISSING`, `COMPILE_COLOR_UNSUPPORTED`, `COMPILE_TOO_MANY_INPUTS`, `COMPILE_TOO_MANY_LAYERS`, `COMPILE_FPS_UNSUPPORTED`, `COMPILE_EMPTY`. Воркер отображает их в `VIDEO_RENDER_PRESET_INVALID` / `GRAPH_TOO_COMPLEX` ([04](04-data-model-and-api.md)): `COMPILE_TOO_MANY_*` → `GRAPH_TOO_COMPLEX`, остальные → `VIDEO_RENDER_PRESET_INVALID`.

`decisions` (VID-T-07): `TRANSITION_SHORTENED` (из FN-E-02), `AUDIO_EFFECT_NOT_RENDERED` (эффекты `audio.gain`/`audio.fade` не рендерятся — звук задают поля `audio`), `HDR_TONEMAPPED` (клип, `pq|hlg`).

**FN-R-17 `buildArgv(plan, paths, codecArgs, out)`**: `ffmpeg -hide_banner -y`, затем для каждого входа по порядку `plan.inputs[i].options` + `-i <paths[i]>`, затем `-filter_complex <plan.filter_complex>`, `-map` по `plan.maps`, `codec_args` пресета, `out`. Рабочий каталог — каталог задания (там лежат `lut_*.cube`). Порядок аргументов не менять: опции входа относятся к следующему `-i`.

## 9. Эталоны и проверка паритета

Эталонные документы — `vectors/reference/ref_compile_vectors.py` (функция `goldens`); готовые строки графов — [`vectors/compile.json`](vectors/compile.json) → `graphs` (G1…G15, 24 случая). Каждый граф **исполнен реальным ffmpeg 6.1.1**, а результат сравнён с `composeAt`.

| ID | Что проверяет | Итог проверки |
|---|---|---|
| G1 | склейка двух клипов, −6 дБ, затухание 0,5 с | 5,000 с, 125 кадров; цвет и RMS звука = `composeAt` |
| G2 | кроссфейд 1 с по центру (A 0…3,5, B 4,5…8) | 6,000 с, 150 кадров |
| G3 | `dip_to_black` + `fade_in` | чёрный Y=16 в середине dip |
| G4a…G4g | поля 4:3, PiP (`scale 0.5`, `position`), поворот 90°, обрезка, `fill`, `none`, кроссфейд на верхней дорожке | пиксели в заданных точках |
| G5a | Кен Бёрнс 1→2, центр 0,5→0,75, `ease_in_out`, 2 с | ряд Y градиента на 5 моментах × 3 точках |
| G5b, G5c | фото→видео кроссфейд, `fade_in` фото на верхней дорожке | пиксели в 12 моментах |
| G6 | скорость 2× и ¼ | частота тона 440 и 880 Гц соответствует |
| G7a…G7d | LUT клипа, мастера, обоих, полосы остаются прозрачными | инверсия LUT и двойная инверсия |
| G8 | ducking музыки речью | ослабление 10,6 дБ |
| G9 | скрытая видеодорожка, заглушённая звуковая, затухания | видео чёрное, звук только у `n` |
| G10 | 29,97 к/с, кроссфейд 30 кадров | 270 кадров, 9,009 с |
| G11 | пресет 1280×720 из 320×180 | поля 4:3 прозрачные |
| G12 | 100 клипов по 0,5 с | 1 250 кадров, 50 с, компиляция и кодирование 3,3 с (320×180, ultrafast) |
| G13 | только фото без звука | видеопоток и стерео-звук 48 кГц из тишины |
| G14 | двухпроходная громкость | −13,95 LUFS |
| G15 | HDR PQ и HLG → SDR | Y=184 для серого 0x80 при PQ 1000 нит; шкала 16…235 |

**Метод.** Источники — однотонные видео (красный, зелёный, синий 4:3) с моно-синусом 220/440/880 Гц и два PNG (`gen_sources.py`); цвет ожидается как `rgb2yuv` по BT.709 ограниченный; ожидаемое по моменту — `composeAt` (слои, непрозрачность, яркость, голоса). Допуск: **9 уровней YUV** (полкадра альфы на переходе), RMS звука — `|Δ| ≤ 0,006 + 5 %`. Кадр момента `t` читается как кадр с `pts ≥ t − 1 мс`. Запуск: `python3 vectors/reference/render_parity/run_all.py <рабочий_каталог>` (нужен ffmpeg 6.1.x в `PATH`); результат — 9 сценариев `ok`, `TOTAL FAIL 0`.

**Тест единственности.** Юнит-векторы `compile.json` (числа, выражения, имена LUT, ошибки, все графы построчно) выполняются без ffmpeg и обязаны совпасть побайтно; паритет — отдельный тест с ffmpeg, помечен `requires_ffmpeg_6_1`.

### 9.1 Граф G2 целиком (кроссфейд)

Входы: `-ss 0 -t 3.5 -i A`, `-ss 4.5 -t 3.5 -i B`.

```
[0:v]setpts=PTS-STARTPTS,fps=25,scale=320:180:force_original_aspect_ratio=decrease:force_divisible_by=2:in_color_matrix=bt709:out_color_matrix=bt709:out_range=tv,format=yuva420p,pad=320:180:(ow-iw)/2:(oh-ih)/2:color=black@0,setpts=PTS-STARTPTS+0/TB[vc0];
[1:v]setpts=PTS-STARTPTS,fps=25,scale=320:180:force_original_aspect_ratio=decrease:force_divisible_by=2:in_color_matrix=bt709:out_color_matrix=bt709:out_range=tv,format=yuva420p,pad=320:180:(ow-iw)/2:(oh-ih)/2:color=black@0,fade=t=in:st=0:d=1:alpha=1,setpts=PTS-STARTPTS+2.52/TB[vc1];
color=c=black:s=320x180:r=25:d=6,format=yuv420p,setparams=colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv[bg];
[bg][vc0]overlay=x='(W-w)/2+(0)*W':y='(H-h)/2+(0)*H':eof_action=pass:enable='between(t,0,3.5)':format=auto[ov0];
[ov0][vc1]overlay=x='(W-w)/2+(0)*W':y='(H-h)/2+(0)*H':eof_action=pass:enable='between(t,2.52,6)':format=auto[ov1];
[ov1]format=yuv420p[vout];
[0:a]asetpts=PTS-STARTPTS,aresample=48000,pan=stereo|c0=c0|c1=c0,aformat=sample_fmts=fltp:channel_layouts=stereo,afade=t=out:st=2.5:d=1:curve=tri,adelay=0S:all=1[av0];
[1:a]asetpts=PTS-STARTPTS,aresample=48000,pan=stereo|c0=c0|c1=c0,aformat=sample_fmts=fltp:channel_layouts=stereo,afade=t=in:st=0:d=1:curve=tri,adelay=120000S:all=1[av1];
anullsrc=r=48000:cl=stereo,atrim=duration=6,asetpts=PTS-STARTPTS[sil1];
[sil1][av0][av1]amix=inputs=3:normalize=0:duration=first[abus]
```

Начало слоя B — `2,5 с` округлено вверх до кадра: `2.52`; звук B начинается точно в `120 000` отсчётов (2,5 с).

### 9.2 Ключевые фрагменты остальных графов

- Движение фото и суперсэмплирование (G5a, 320×180 → холст 1280×720): `…scale=1280:720:force_original_aspect_ratio=decrease:force_divisible_by=2:…,format=rgba,pad=1280:720:(ow-iw)/2:(oh-ih)/2:color=black@0,zoompan=z='…':x='…':y='…':d=1:s=320x180:fps=25,scale=out_color_matrix=bt709:out_range=tv,format=yuva420p`.
- Ducking (G8, звуковой граф): `[2:a]…adelay=96000S:all=1,asplit=2[av2][bs2]`; `[sil1][bs2]amix=inputs=2:normalize=0:duration=first,asplit=1[bus_A2_0]`; `[av1][bus_A2_0]sidechaincompress=threshold=0.031622777:ratio=20:attack=20:release=250:makeup=1:level_sc=1[du1_1]`; `[sil2][av0][du1_1][av2]amix=inputs=4:normalize=0:duration=first[abus]`.
- Скорость (G6): видео `setpts=(PTS-STARTPTS)*1/2` и `(PTS-STARTPTS)*4/1`; звук `atempo=2` и `atempo=0.5,atempo=0.5`.
- Поворот (G4c): `rotate=a=90*PI/180:ow='rotw(90*PI/180)':oh='roth(90*PI/180)':c=none`.

## 10. Пределы и открытые вопросы

- Альфа перехода на границе кадров даёт отклонение до полукадра прозрачности (допуск 9 уровней, D-39).
- Предельное число входов 128 (`RENDER_MAX_INPUTS`): длинный таймлайн из сотен клипов требует сегментного рендера — V2 (O-14).
- Эталон не проверяет субъективное качество цвета тонового отображения и ducking, только числа (O-15).
- Версия ffmpeg зафиксирована как 6.1.x; перенос на другую минорную версию — только после прогона паритета (D-48, O-16).
- `audio.gain`/`audio.fade` как эффекты не рендерятся (решение `AUDIO_EFFECT_NOT_RENDERED`); поля `audio` — единственный источник усиления и затуханий.
- Живой предпросмотр ([23](23-editor-playback-timeline.md) D-27) приблизителен: рендер — истина для цвета и ducking.

## 11. Трассировка

Функции FN-R-08…R-17 ([14](14-function-catalog.md)); векторы `compile.json` (раздел 9, юнит-секции 6 + `graphs` 24); карточки TK-639…643 ([15](15-task-cards.md)); приёмка ACC-80…82 ([17](17-acceptance-and-dod.md)); сценарий UC-51, поток F-45, e2e E2E-38; находки R-35…R-40 и вопросы O-14…O-16 ([21](21-review-report.md)).
