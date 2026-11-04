// Package worker runs video jobs: it claims them from photo.video_jobs, runs
// ffprobe/ffmpeg and settles the result through the SQL functions of migrations
// 02700-02703.
package worker

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/video/color"
	"photoeditor/backend/internal/video/probe"
	"photoeditor/backend/internal/video/render"
)

const (
	leaseSeconds    = 120
	filmstripFrames = 16
	waveformBuckets = 800
)

type Worker struct {
	Pool    *pgxpool.Pool
	Store   Store
	FFmpeg  string
	FFprobe string
	Encoder string // libx264 or h264_nvenc
	Bucket  string
	WorkDir string
}

type job struct {
	ID, Tenant, Project uuid.UUID
	Kind                string
	Token               uuid.UUID
	Attempt             int
}

type mediaIn struct {
	ID        uuid.UUID       `json:"id"`
	Kind      string          `json:"kind"`
	Name      string          `json:"name"`
	Bucket    string          `json:"bucket"`
	ObjectKey string          `json:"object_key"`
	SHA256    string          `json:"sha256"`
	Bytes     int64           `json:"bytes"`
	Probe     json.RawMessage `json:"probe"`
}

type objRef struct {
	Bucket    string `json:"bucket"`
	ObjectKey string `json:"object_key"`
	SHA256    string `json:"sha256"`
	Bytes     int64  `json:"bytes"`
}

type inputs struct {
	Media  []mediaIn `json:"media"`
	Render *struct {
		ID     uuid.UUID       `json:"id"`
		Preset json.RawMessage `json:"preset"`
		DocSHA string          `json:"document_sha256"`
	} `json:"render"`
	Document *objRef `json:"document"`
}

// failure is a job-level error with a stable code (<=64 chars).
type failure struct {
	Code   string
	Detail string
}

func (f failure) Error() string { return f.Code + ": " + f.Detail }

// RunOnce claims and processes at most one job. It reports whether a job was found.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	for _, pool := range []string{"video_heavy", "video_light"} {
		var j job
		err := w.Pool.QueryRow(ctx, `SELECT job_id,tenant_id,project_id,kind,claim_token,attempt FROM photo.claim_video_job($1,$2)`, pool, leaseSeconds).
			Scan(&j.ID, &j.Tenant, &j.Project, &j.Kind, &j.Token, &j.Attempt)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		return true, w.process(ctx, j)
	}
	return false, nil
}

// Loop polls until ctx is done.
func (w *Worker) Loop(ctx context.Context, idle time.Duration) error {
	for ctx.Err() == nil {
		found, err := w.RunOnce(ctx)
		if err != nil {
			return err
		}
		if !found {
			select {
			case <-ctx.Done():
			case <-time.After(idle):
			}
		}
	}
	return ctx.Err()
}

func (w *Worker) process(ctx context.Context, j job) error {
	dir, err := os.MkdirTemp(w.WorkDir, "job-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	jctx, cancel := context.WithCancel(ctx)
	defer cancel()
	switch j.Kind {
	case "ingest":
		err = w.ingest(jctx, j, dir)
	case "render":
		err = w.render(jctx, j, dir, cancel)
	default:
		err = failure{"VIDEO_JOB_KIND_UNSUPPORTED", j.Kind}
	}
	if err == nil {
		return nil
	}
	var f failure
	if !errors.As(err, &f) {
		if ctx.Err() != nil {
			return ctx.Err() // shutdown: the lease expires and the job is requeued
		}
		f = failure{"VIDEO_WORKER_ERROR", err.Error()}
	}
	if len(f.Detail) > 500 {
		f.Detail = f.Detail[:500]
	}
	_, ferr := w.Pool.Exec(context.WithoutCancel(ctx), `SELECT photo.finish_video_job($1,$2,$3,'failed',$4,$5,NULL,'[]'::jsonb)`, j.Tenant, j.ID, j.Token, f.Code, f.Detail)
	return ferr
}

func (w *Worker) inputs(ctx context.Context, j job) (inputs, error) {
	var raw []byte
	if err := w.Pool.QueryRow(ctx, `SELECT photo.get_video_job_inputs($1,$2,$3)::text`, j.Tenant, j.ID, j.Token).Scan(&raw); err != nil {
		return inputs{}, err
	}
	var in inputs
	return in, json.Unmarshal(raw, &in)
}

func (w *Worker) key(j job, name string) string {
	return fmt.Sprintf("tenants/%s/video/jobs/%s/%s", j.Tenant, j.ID, name)
}

func (w *Worker) register(ctx context.Context, j job, role, name, mediaType, file string, width, height int) (uuid.UUID, int64, error) {
	key := w.key(j, name)
	sha, size, err := w.Store.Put(ctx, w.Bucket, key, file)
	if err != nil {
		return uuid.Nil, 0, err
	}
	var id uuid.UUID
	var wd, ht *int
	if width > 0 {
		wd, ht = &width, &height
	}
	err = w.Pool.QueryRow(ctx, `SELECT photo.register_video_artifact($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		j.Tenant, j.ID, j.Token, role, w.Bucket, key, sha, size, mediaType, wd, ht).Scan(&id)
	return id, size, err
}

func (w *Worker) ingest(ctx context.Context, j job, dir string) error {
	in, err := w.inputs(ctx, j)
	if err != nil || len(in.Media) != 1 {
		return fmt.Errorf("ingest inputs: %v (%d media)", err, len(in.Media))
	}
	m := in.Media[0]
	src := filepath.Join(dir, "source")
	if err := w.Store.Fetch(ctx, m.Bucket, m.ObjectKey, src); err != nil {
		return err
	}
	if sha, _, err := HashFile(src); err != nil || sha != m.SHA256 {
		return failure{"VIDEO_SOURCE_HASH_MISMATCH", ""}
	}
	if m.Kind == "lut" {
		return w.ingestLUT(ctx, j, m, src, dir)
	}
	facts, raw, perr := probe.Run(ctx, w.FFprobe, src)
	if perr != nil {
		// A bad file is a domain outcome, not a job failure: the media is rejected.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		code := perr.Error()
		if _, err := w.Pool.Exec(ctx, `SELECT photo.finish_video_ingest($1,$2,'rejected',$3,NULL,NULL,NULL,NULL,NULL)`, j.Tenant, m.ID, code); err != nil {
			return err
		}
		_, err := w.Pool.Exec(ctx, `SELECT photo.finish_video_job($1,$2,$3,'succeeded',NULL,NULL,'cpu','[]'::jsonb)`, j.Tenant, j.ID, j.Token)
		return err
	}
	probeFile := filepath.Join(dir, "probe.json")
	if err := os.WriteFile(probeFile, raw, 0o640); err != nil {
		return err
	}
	probeID, _, err := w.register(ctx, j, "video_probe", "probe.json", "application/json", probeFile, 0, 0)
	if err != nil {
		return err
	}
	var proxyID, filmID, waveID *uuid.UUID
	if facts.HasVideo {
		proxy := filepath.Join(dir, "proxy.mp4")
		args := []string{"-hide_banner", "-nostdin", "-y", "-i", src, "-map", "0:v:0", "-vf", "scale=-2:360:force_divisible_by=2", "-c:v", "libx264", "-preset", "veryfast", "-crf", "28", "-pix_fmt", "yuv420p"}
		if facts.HasAudio {
			args = append(args, "-map", "0:a:0", "-c:a", "aac", "-b:a", "96k")
		}
		args = append(args, "-movflags", "+faststart", proxy)
		if out, err := exec.CommandContext(ctx, w.FFmpeg, args...).CombinedOutput(); err != nil {
			return failure{"VIDEO_PROXY_FAILED", tail(out)}
		}
		id, _, err := w.register(ctx, j, "video_proxy", "proxy.mp4", "video/mp4", proxy, 0, 0)
		if err != nil {
			return err
		}
		proxyID = &id

		// Filmstrip: up to 16 evenly spaced frames tiled into one JPEG sprite.
		secs := float64(facts.DurationTicks) / render.TicksPerSecond
		n := int(secs) + 1
		if n > filmstripFrames {
			n = filmstripFrames
		}
		film := filepath.Join(dir, "filmstrip.jpg")
		vf := fmt.Sprintf("fps=%d/%s,scale=160:-2,tile=%dx1", n, strconv.FormatFloat(secs, 'f', 6, 64), n)
		if out, err := exec.CommandContext(ctx, w.FFmpeg, "-hide_banner", "-nostdin", "-y", "-i", src, "-an", "-vf", vf, "-frames:v", "1", "-q:v", "4", film).CombinedOutput(); err != nil {
			return failure{"VIDEO_FILMSTRIP_FAILED", tail(out)}
		}
		fid, _, err := w.register(ctx, j, "video_filmstrip", "filmstrip.jpg", "image/jpeg", film, 0, 0)
		if err != nil {
			return err
		}
		filmID = &fid
	}
	if facts.HasVideo || facts.HasAudio {
		peaks, err := w.waveform(ctx, src, facts.HasAudio)
		if err != nil {
			return failure{"VIDEO_WAVEFORM_FAILED", err.Error()}
		}
		wf := filepath.Join(dir, "waveform.json")
		if err := os.WriteFile(wf, peaks, 0o640); err != nil {
			return err
		}
		wid, _, err := w.register(ctx, j, "video_waveform", "waveform.json", "application/json", wf, 0, 0)
		if err != nil {
			return err
		}
		waveID = &wid
	}
	pj, _ := json.Marshal(facts)
	var ok bool
	if err := w.Pool.QueryRow(ctx, `SELECT photo.finish_video_ingest($1,$2,'ready',NULL,$3::jsonb,$4,$5,$6,$7)`, j.Tenant, m.ID, string(pj), probeID, proxyID, filmID, waveID).Scan(&ok); err != nil {
		return err
	}
	// Derivatives are charged to the upload reservation by finish_video_ingest, so the job itself has no outputs.
	return w.finishJob(ctx, j, "cpu", nil)
}

// ingestLUT validates a .cube file (FN-W-12): a bad file rejects the media, a good one needs only its probe record.
func (w *Worker) ingestLUT(ctx context.Context, j job, m mediaIn, src, dir string) error {
	text, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	l, perr := color.ParseCube(string(text))
	if perr != nil {
		if _, err := w.Pool.Exec(ctx, `SELECT photo.finish_video_ingest($1,$2,'rejected','VIDEO_UNSUPPORTED_FORMAT',NULL,NULL,NULL,NULL,NULL)`, j.Tenant, m.ID); err != nil {
			return err
		}
		_, err := w.Pool.Exec(ctx, `SELECT photo.finish_video_job($1,$2,$3,'succeeded',NULL,NULL,'cpu','[]'::jsonb)`, j.Tenant, j.ID, j.Token)
		return err
	}
	pj, _ := json.Marshal(map[string]any{"kind": "lut", "lut_size": l.Size})
	probeFile := filepath.Join(dir, "probe.json")
	if err := os.WriteFile(probeFile, pj, 0o640); err != nil {
		return err
	}
	probeID, _, err := w.register(ctx, j, "video_probe", "probe.json", "application/json", probeFile, 0, 0)
	if err != nil {
		return err
	}
	var ok bool
	if err := w.Pool.QueryRow(ctx, `SELECT photo.finish_video_ingest($1,$2,'ready',NULL,$3::jsonb,$4,NULL,NULL,NULL)`, j.Tenant, m.ID, string(pj), probeID).Scan(&ok); err != nil {
		return err
	}
	return w.finishJob(ctx, j, "cpu", nil)
}

func (w *Worker) finishJob(ctx context.Context, j job, backend string, outs []map[string]any) error {
	if outs == nil {
		outs = []map[string]any{}
	}
	ob, _ := json.Marshal(outs)
	var ok bool
	if err := w.Pool.QueryRow(context.WithoutCancel(ctx), `SELECT photo.finish_video_job($1,$2,$3,'succeeded',NULL,NULL,$4,$5::jsonb)`, j.Tenant, j.ID, j.Token, backend, string(ob)).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return failure{"VIDEO_JOB_FENCE_LOST", ""}
	}
	return nil
}

func tail(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 400 {
		s = s[len(s)-400:]
	}
	return s
}

type preset struct {
	Encoder string `json:"encoder"`
}

func (w *Worker) render(ctx context.Context, j job, dir string, cancel context.CancelFunc) error {
	in, err := w.inputs(ctx, j)
	if err != nil {
		return err
	}
	if in.Render == nil || in.Document == nil {
		return failure{"VIDEO_RENDER_INPUTS_MISSING", ""}
	}
	docFile := filepath.Join(dir, "doc.json")
	if err := w.Store.Fetch(ctx, in.Document.Bucket, in.Document.ObjectKey, docFile); err != nil {
		return err
	}
	if sha, _, err := HashFile(docFile); err != nil || sha != in.Document.SHA256 {
		return failure{"VIDEO_DOCUMENT_HASH_MISMATCH", ""}
	}
	rawDoc, _ := os.ReadFile(docFile)
	var doc render.Doc
	if err := json.Unmarshal(rawDoc, &doc); err != nil {
		return failure{"VIDEO_DOCUMENT_INVALID", err.Error()}
	}
	sources, paths := map[string]render.Source{}, map[string]string{}
	luts := map[string]*color.Lut3D{}
	for _, m := range in.Media {
		if m.Kind != "lut" {
			continue
		}
		dst := filepath.Join(dir, "lut-"+m.ID.String())
		if err := w.Store.Fetch(ctx, m.Bucket, m.ObjectKey, dst); err != nil {
			return err
		}
		text, err := os.ReadFile(dst)
		if err != nil {
			return err
		}
		if luts[m.ID.String()], err = color.ParseCube(string(text)); err != nil {
			return failure{"VIDEO_LUT_INVALID", m.ID.String()}
		}
	}
	for _, m := range in.Media {
		if m.Kind == "lut" {
			continue
		}
		var f probe.Facts
		if json.Unmarshal(m.Probe, &f) != nil {
			continue
		}
		sources[m.ID.String()] = render.Source{DurationTicks: f.DurationTicks, HasVideo: f.HasVideo, HasAudio: f.HasAudio, Channels: f.Channels}
	}
	plan, err := render.CompileWith(doc, sources, luts)
	if err != nil {
		var ce render.Error
		if errors.As(err, &ce) {
			return failure{ce.Code, ce.Clip}
		}
		return err
	}
	byID := map[string]mediaIn{}
	for _, m := range in.Media {
		byID[m.ID.String()] = m
	}
	for _, p := range plan.Inputs {
		if _, done := paths[p.Source]; done {
			continue
		}
		m := byID[p.Source]
		dst := filepath.Join(dir, "src-"+p.Source)
		if err := w.Store.Fetch(ctx, m.Bucket, m.ObjectKey, dst); err != nil {
			return err
		}
		paths[p.Source] = dst
	}
	encoder := w.Encoder
	var ps preset
	_ = json.Unmarshal(in.Render.Preset, &ps)
	if ps.Encoder != "" {
		encoder = ps.Encoder
	}
	v := doc["video"].(map[string]any)
	fps := v["fps"].(map[string]any)
	fn, fd := int64(fps["num"].(float64)), int64(fps["den"].(float64))
	outFile := filepath.Join(dir, "out.mp4")
	if err := render.WriteLUTs(plan, dir); err != nil {
		return err
	}
	loud := "none"
	if t, tp, ok := loudnessTarget(doc); ok {
		m, err := w.measureLoudness(ctx, plan, paths, t, tp)
		switch {
		case err != nil:
			return err
		case m.Silent():
			loud = "skipped_silent"
		default:
			plan = render.WithLoudness(plan, t, tp, m)
			loud = fmt.Sprintf("measured_%.1f_lufs", m.InputI)
		}
	}
	args, err := render.Args(plan, paths, outFile, encoder, fn, fd)
	if err != nil {
		return err
	}
	if err := w.runFFmpeg(ctx, j, args, plan.DurationTicks, cancel, dir); err != nil {
		return err
	}
	facts, _, err := probe.Run(ctx, w.FFprobe, outFile)
	if err != nil {
		return failure{"VIDEO_RENDER_OUTPUT_UNREADABLE", err.Error()}
	}
	outID, _, err := w.register(ctx, j, "video_render", "render.mp4", "video/mp4", outFile, int(facts.Width), int(facts.Height))
	if err != nil {
		return err
	}
	qc, _ := json.Marshal(map[string]any{"checks": map[string]any{
		"duration_ticks_expected": plan.DurationTicks, "duration_ticks_actual": facts.DurationTicks,
		"width": facts.Width, "height": facts.Height, "has_audio": facts.HasAudio, "audio_channels": facts.Channels,
		"loudness": loud, "video_codec": facts.VideoCodec, "audio_codec": facts.AudioCodec, "compiler": plan.Version, "encoder": encoder}})
	qcFile := filepath.Join(dir, "qc.json")
	if err := os.WriteFile(qcFile, qc, 0o640); err != nil {
		return err
	}
	qcID, _, err := w.register(ctx, j, "video_qc_report", "qc.json", "application/json", qcFile, 0, 0)
	if err != nil {
		return err
	}
	fp := sha256.Sum256([]byte(plan.Version + "\n" + plan.FilterComplex + "\n" + in.Document.SHA256 + "\n" + string(in.Render.Preset)))
	backend := "cpu"
	if encoder == "h264_nvenc" {
		backend = "cuda"
	}
	var ok bool
	if err := w.Pool.QueryRow(ctx, `SELECT photo.finish_video_render($1,$2,'ready',$3,$4,$5,$6,$7,$8)`,
		j.Tenant, in.Render.ID, outID, qcID, facts.DurationTicks, encoder, backend, hex.EncodeToString(fp[:])).Scan(&ok); err != nil {
		return err
	}
	return w.finishJob(ctx, j, backend, []map[string]any{
		{"role": "video_render", "ordinal": 0, "artifact_id": outID}, {"role": "video_qc_report", "ordinal": 0, "artifact_id": qcID}})
}

// loudnessTarget reads master.audio.loudness of the document.
func loudnessTarget(doc render.Doc) (target, truePeak float64, ok bool) {
	m, _ := doc["master"].(map[string]any)
	a, _ := m["audio"].(map[string]any)
	l, _ := a["loudness"].(map[string]any)
	t, ok1 := l["target_lufs"].(float64)
	tp, ok2 := l["true_peak_db"].(float64)
	return t, tp, ok1 && ok2
}

// measureLoudness is pass 1 of section 7.
func (w *Worker) measureLoudness(ctx context.Context, plan render.Plan, paths map[string]string, t, tp float64) (render.Measured, error) {
	args, err := render.LoudnessPass1(plan, paths, t, tp)
	if err != nil {
		return render.Measured{}, err
	}
	out, err := exec.CommandContext(ctx, w.FFmpeg, args...).CombinedOutput()
	if err != nil {
		return render.Measured{}, failure{"VIDEO_RENDER_FFMPEG_FAILED", tail(out)}
	}
	m, err := render.ParseMeasured(out)
	if err != nil {
		return render.Measured{}, failure{"VIDEO_RENDER_FFMPEG_FAILED", err.Error()}
	}
	return m, nil
}

// runFFmpeg runs the command, forwarding -progress output as job progress. When the
// lease is lost or a cancel is requested the process is killed.
func (w *Worker) runFFmpeg(ctx context.Context, j job, args []string, total int64, cancel context.CancelFunc, dir string) error {
	cmd := exec.CommandContext(ctx, w.FFmpeg, args...)
	cmd.Dir = dir // lut_*.cube files are read relative to it
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	lost := false
	sc := bufio.NewScanner(stdout)
	var last time.Time
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), "=")
		if k != "out_time_us" || time.Since(last) < 2*time.Second {
			continue
		}
		us, err := strconv.ParseInt(v, 10, 64)
		if err != nil || us < 0 {
			continue
		}
		last = time.Now()
		p := float64(us) * 1e-6 * render.TicksPerSecond / float64(total)
		if p > 1 {
			p = 1
		}
		var cont bool
		if err := w.Pool.QueryRow(ctx, `SELECT photo.report_video_job_progress($1,$2,$3,'encoding',$4,NULL,$5)`, j.Tenant, j.ID, j.Token, strconv.FormatFloat(p, 'f', 4, 64), leaseSeconds).Scan(&cont); err == nil && !cont {
			lost = true
			cancel()
			break
		}
	}
	_, _ = io.Copy(io.Discard, stdout)
	werr := cmd.Wait()
	if lost {
		return failure{"VIDEO_JOB_CANCELLED_OR_LOST", ""}
	}
	if werr != nil {
		return failure{"VIDEO_RENDER_FFMPEG_FAILED", tail(stderr.Bytes())}
	}
	return nil
}

// waveform returns {"buckets":N,"peaks":[0..1,...]} computed from 8 kHz mono PCM; a source
// without audio yields all-zero peaks so that every video has the artifact.
func (w *Worker) waveform(ctx context.Context, src string, hasAudio bool) ([]byte, error) {
	peaks := make([]float64, waveformBuckets)
	if hasAudio {
		out, err := exec.CommandContext(ctx, w.FFmpeg, "-hide_banner", "-nostdin", "-v", "error", "-i", src, "-vn", "-ac", "1", "-ar", "8000", "-f", "s16le", "pipe:1").Output()
		if err != nil {
			return nil, err
		}
		n := len(out) / 2
		for i := 0; i < n; i++ {
			v := int16(uint16(out[2*i]) | uint16(out[2*i+1])<<8)
			a := float64(v)
			if a < 0 {
				a = -a
			}
			a /= 32768
			if b := i * waveformBuckets / n; a > peaks[b] {
				peaks[b] = a
			}
		}
	}
	for i := range peaks {
		peaks[i] = float64(int(peaks[i]*1000+0.5)) / 1000
	}
	return json.Marshal(map[string]any{"buckets": waveformBuckets, "peaks": peaks})
}
