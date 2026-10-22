"""Источники для паритетных проверок рендера (документ 24, раздел 9). Требуется ffmpeg 6.1.x.
Однотонные видео 320x180 (и 160x120), 25 к/с, 12 с, моно-синус (амплитуда 1/8 по умолчанию источника sine), AAC 48 кГц; два PNG 640x360."""
import subprocess, sys, os
out = sys.argv[1] if len(sys.argv) > 1 else '.'
os.makedirs(out, exist_ok=True)
def run(*a): subprocess.run(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-y', *a], check=True)
def vid(name, color, size, freq):
    run('-f', 'lavfi', '-i', f'color=c={color}:s={size}:r=25:d=12',
        '-f', 'lavfi', '-i', f'sine=f={freq}:r=48000:d=12,aformat=channel_layouts=mono',
        '-vf', 'format=rgb24,scale=out_color_matrix=bt709:out_range=tv,format=yuv420p',
        '-c:v', 'libx264', '-preset', 'ultrafast', '-crf', '12', '-colorspace', 'bt709', '-color_primaries', 'bt709',
        '-color_trc', 'bt709', '-color_range', 'tv', '-c:a', 'aac', '-b:a', '192k', os.path.join(out, name))
vid('red.mp4', 'red', '320x180', 440)
vid('green.mp4', 'lime', '320x180', 880)
vid('blue43.mp4', 'blue', '160x120', 220)
run('-f', 'lavfi', '-i', 'color=c=0xFF8000:s=640x360', '-frames:v', '1', os.path.join(out, 'photo_orange.png'))
run('-f', 'lavfi', '-i', 'gradients=s=640x360:c0=black:c1=white:x0=0:y0=0:x1=640:y1=0:nb_colors=2:d=1', '-frames:v', '1',
    '-pix_fmt', 'rgba', os.path.join(out, 'photo_grad.png'))
def hdr(name, trc):
    run('-f', 'lavfi', '-i', 'color=c=0x808080:s=320x180:r=25:d=3,format=rgb24',
        '-vf', f'zscale=rin=pc:t={trc}:tin=iec61966-2-1:p=bt2020:pin=bt709:m=bt2020nc:min=gbr:r=tv:npl=1000,format=yuv420p10le',
        '-c:v', 'libx264', '-preset', 'ultrafast', '-crf', '8', '-pix_fmt', 'yuv420p10le', '-colorspace', 'bt2020nc',
        '-color_primaries', 'bt2020', '-color_trc', trc, '-color_range', 'tv', os.path.join(out, name))
hdr('hdr_pq.mp4', 'smpte2084')
hdr('hdr_hlg.mp4', 'arib-std-b67')
