# Golden render graphs (verified with ffmpeg)

> **Статус (документ 24).** Графы G1/G2 ниже (`concat`/`xfade`/`acrossfade`) оставлены как исторический эталон длительности и числа кадров (5.000 с / 125 кадров, 6.000 с / 150 кадров) и для фикстур `g1.mp4`/`g2.mp4`. Компилятор рендера строит **другие** графы (холст + `overlay`, [24](../24-render-compiler.md), D-37); их строки и проверки — `compile.json → graphs` (G1…G15). L1 (громкость) сохраняет силу.


Sources (lavfi, regenerate with fixtures script): srcA.mp4, srcB.mp4 = testsrc 320x180@25 10s + sine, h264/aac, GOP 25.

## G1 — two clips, hard cut, gain -6 dB, fade-in 0.5 s on first audio
Expected: format duration 5.000000 s; video nb_read_frames = 125; audio duration 5.0 s.
```
ffmpeg -y -i srcA.mp4 -i srcB.mp4 -filter_complex "
[0:v]trim=start=2:end=5,setpts=PTS-STARTPTS,fps=25,scale=320:180:force_original_aspect_ratio=decrease,pad=320:180:(ow-iw)/2:(oh-ih)/2,setsar=1,format=yuv420p[v0];
[1:v]trim=start=1:end=3,setpts=PTS-STARTPTS,fps=25,scale=320:180:force_original_aspect_ratio=decrease,pad=320:180:(ow-iw)/2:(oh-ih)/2,setsar=1,format=yuv420p[v1];
[v0][v1]concat=n=2:v=1:a=0[vout];
[0:a]atrim=start=2:end=5,asetpts=PTS-STARTPTS,aresample=48000,volume=-6dB,afade=t=in:st=0:d=0.5[a0];
[1:a]atrim=start=1:end=3,asetpts=PTS-STARTPTS,aresample=48000,volume=-6dB[a1];
[a0][a1]concat=n=2:v=0:a=1[aout]" -map "[vout]" -map "[aout]" -c:v libx264 -preset veryfast -pix_fmt yuv420p -b:v 1M -maxrate 1.2M -bufsize 2.4M -c:a aac -b:a 192k -movflags +faststart -map_metadata -1 g1.mp4
```
## G2 — centered 1 s crossfade, 3 s + 3 s clips, total 6.000 s, 150 frames
Clip A source 0..3.5 (handle 0.5 s after out), clip B source 4.5..8 (handle 0.5 s before in). xfade offset = B.at - d/2 + 0 = 2.5 (A length 3.5 - d 1.0).
```
ffmpeg -y -i srcA.mp4 -i srcB.mp4 -filter_complex "
[0:v]trim=start=0:end=3.5,setpts=PTS-STARTPTS,fps=25,format=yuv420p[v0];
[1:v]trim=start=4.5:end=8,setpts=PTS-STARTPTS,fps=25,format=yuv420p[v1];
[v0][v1]xfade=transition=fade:duration=1:offset=2.5[vout];
[0:a]atrim=start=0:end=3.5,asetpts=PTS-STARTPTS,aresample=48000[a0];
[1:a]atrim=start=4.5:end=8,asetpts=PTS-STARTPTS,aresample=48000[a1];
[a0][a1]acrossfade=d=1:c1=tri:c2=tri[aout]" -map "[vout]" -map "[aout]" -c:v libx264 -preset veryfast -pix_fmt yuv420p -c:a aac -b:a 192k g2.mp4
```
## L1 — loudness
Input: sine 1 kHz, 48 kHz, 20 s, volume -20dB (measured input_i = -41.05 LUFS on stereo).
`ffmpeg -i sine20.wav -af loudnorm=I=-14:TP=-1:LRA=11:print_format=json -f null -`
Measured (single pass): output_i = -14.05. Tolerance in tests: |output_i - (-14)| <= 0.5 on second (measured) pass.
