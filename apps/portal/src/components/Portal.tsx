"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import * as api from "@/lib/api";
import { messages, type Locale } from "@/lib/i18n";
import { MediaCache } from "@/lib/media-cache";
import { virtualWindow } from "@/lib/virtual-grid";
import type { Collection, DownloadStatus, GuestItem, MediaLink, Preset, Upload } from "@/lib/types";

const terminal = new Set(["completed", "failed", "cancelled", "expired"]);
function can(collection: Collection | null, name: string) {
  const value = collection?.capabilities;
  return Array.isArray(value) ? value.includes(name) : Boolean(value?.[name]);
}
function presetID(preset: Preset) { return typeof preset === "string" ? preset : preset.id; }

export function Portal({ locale, shareID }: { locale: Locale; shareID: string }) {
  const t = messages[locale];
  const cache = useMemo(() => new MediaCache(), []);
  const [phase, setPhase] = useState<"loading" | "ready" | "invalid" | "error">("loading");
  const [csrf, setCSRF] = useState("");
  const [collection, setCollection] = useState<Collection | null>(null);
  const [items, setItems] = useState<GuestItem[]>([]);
  const [cursor, setCursor] = useState<string>();
  const [links, setLinks] = useState<Record<string, MediaLink>>({});
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [lightbox, setLightbox] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [download, setDownload] = useState<DownloadStatus | null>(null);
  const uploadRef = useRef<Upload | null>(null);
  const gridRef = useRef<HTMLElement | null>(null);
  const [grid, setGrid] = useState({ columns: 2, rowHeight: 260, scrollTop: 0, viewportHeight: 900 });
  const windowed = useMemo(() => virtualWindow(items.length, grid.columns, grid.rowHeight, grid.scrollTop, grid.viewportHeight), [grid, items.length]);
  const visibleItems = useMemo(() => items.slice(windowed.start, windowed.end), [items, windowed.end, windowed.start]);

  const fetchMedia = useCallback(async (batch: GuestItem[], variant: "thumb640" | "preview2560") => {
    if (!csrf) return;
    const missing = batch.filter(i => !cache.get(i.id, variant));
    if (missing.length) {
      const result = await api.getMediaLinks(csrf, missing.slice(0, 100).map(i => i.id), [variant]);
      cache.put(result.items);
    }
    setLinks(current => {
      const next = { ...current };
      for (const item of batch) { const link = cache.get(item.id, variant); if (link) next[`${item.id}:${variant}`] = link; }
      const entries=Object.entries(next);return entries.length<=512?next:Object.fromEntries(entries.slice(-512));
    });
  }, [cache, csrf]);

  const load = useCallback(async () => {
    setPhase("loading"); setNotice(""); cache.clear(); setLinks({}); setItems([]); setSelected(new Set());
    try {
      let token = sessionStorage.getItem(`photo:portal:${shareID}:csrf`) || "";
      const secret = api.secretFromFragment(location.hash);
      if (secret) {
        const session = await api.exchange(shareID, secret, locale);
        token = session.csrf_token;
        sessionStorage.setItem(`photo:portal:${shareID}:csrf`, token);
        history.replaceState(history.state, "", location.pathname + location.search);
      }
      if (!token) { setPhase("invalid"); return; }
      setCSRF(token);
      const meta = await api.getCollection();
      setCollection(meta);
      if (can(meta, "view")) {
        const page = await api.getItems(); setItems(page.items.slice(0, 5000)); setCursor(page.next_cursor);
      }
      setPhase("ready");
    } catch (error) {
      if (error instanceof api.APIError && (error.status === 401 || error.status === 403 || error.status === 404)) setPhase("invalid");
      else setPhase("error");
    }
  }, [cache, locale, shareID]);

  useEffect(() => { void load(); }, [load]);
  useEffect(() => { if (phase === "ready" && csrf && can(collection, "view")) void fetchMedia(visibleItems, "thumb640").catch(() => setNotice(t.error)); }, [collection, csrf, fetchMedia, phase, t.error, visibleItems]);
  useEffect(() => {
    if (phase !== "ready" || !can(collection, "view")) return;
    const measure = () => {
      const element = gridRef.current; if (!element) return;
      const width = element.clientWidth; const gap = 12; const columns = Math.max(2, Math.floor((width + gap) / (220 + gap)));
      const tileWidth = (width - gap * (columns - 1)) / columns; const rowHeight = tileWidth * .75 + 58 + gap;
      setGrid({ columns, rowHeight, scrollTop: Math.max(0, window.scrollY - element.getBoundingClientRect().top - window.scrollY), viewportHeight: window.innerHeight });
    };
    const onScroll = () => { const element = gridRef.current; if (!element) return; setGrid(current => ({ ...current, scrollTop: Math.max(0, window.scrollY - (element.getBoundingClientRect().top + window.scrollY)), viewportHeight: window.innerHeight })); };
    measure(); window.addEventListener("resize", measure); window.addEventListener("scroll", onScroll, { passive: true });
    return () => { window.removeEventListener("resize", measure); window.removeEventListener("scroll", onScroll); };
  }, [collection, phase]);
  useEffect(() => {
    if (lightbox === null || !items[lightbox]) return;
    void fetchMedia([items[lightbox]], "preview2560").catch(() => setNotice(t.error));
  }, [fetchMedia, items, lightbox, t.error]);
  useEffect(() => {
    if (!download || terminal.has(download.status)) return;
    const timer = window.setTimeout(() => { void api.getDownload(download.id).then(setDownload).catch(() => setNotice(t.error)); }, 1500);
    return () => window.clearTimeout(timer);
  }, [download, t.error]);

  async function more() {
    if (!cursor || items.length >= 5000) return;
    setBusy(true);
    try { const page = await api.getItems(cursor); setItems(current => [...current, ...page.items].slice(0, 5000)); setCursor(page.next_cursor); }
    catch { setNotice(t.error); } finally { setBusy(false); }
  }
  function toggle(id: string) { setSelected(current => { const next = new Set(current); next.has(id) ? next.delete(id) : next.add(id); return next; }); }
  async function createDownload() {
    if (!collection || (can(collection, "view") && selected.size === 0)) { setNotice(t.selectionRequired); return; }
    const preset = collection.allowed_presets[0]; if (!preset) { setNotice(t.noPermission); return; }
    setBusy(true); setNotice(t.downloadPreparing);
    try { const snapshot = await api.createSelection(csrf, collection.revision, can(collection, "view") ? [...selected] : undefined); const accepted = await api.startDownload(csrf, snapshot.id, presetID(preset)); setDownload({ id: accepted.download_job_id, status: accepted.status, files_done: 0, files_total: snapshot.item_count, bytes_written: 0, volumes: [] }); }
    catch { setNotice(t.error); } finally { setBusy(false); }
  }
  async function downloadVolume(volume: number) {
    try { const link = await api.redeemDownload(csrf, download!.id, volume); location.assign(link.url); } catch { setNotice(t.error); }
  }
  async function downloadItem(item: GuestItem) {
    const preset = collection?.allowed_presets[0]; if (!preset) { setNotice(t.noPermission); return; }
    setBusy(true);
    try { const link = await api.downloadItem(csrf, item.id, presetID(preset)); location.assign(link.url); }
    catch { setNotice(t.error); } finally { setBusy(false); }
  }
  async function uploadFiles(files: FileList | null) {
    if (!files?.length) return;
    setBusy(true); setNotice(t.uploading);
    try {
      for (const file of Array.from(files)) {
        const upload = await api.beginUpload(csrf, file); uploadRef.current = upload;
        const numbers = Array.from({ length: upload.parts_total }, (_, i) => i + 1);
        const completed: { part_no: number; etag: string }[] = [];
        for (let offset = 0; offset < numbers.length; offset += 100) {
          const signed = await api.signParts(csrf, upload.id, numbers.slice(offset, offset + 100));
          for (const part of signed.items) {
            const start = (part.part_no - 1) * upload.part_bytes;
            const response = await fetch(part.url, { method: "PUT", body: file.slice(start, Math.min(start + upload.part_bytes, file.size)) });
            if (!response.ok) throw new Error("part upload failed");
            const etag = response.headers.get("etag"); if (!etag) throw new Error("missing etag");
            completed.push({ part_no: part.part_no, etag });
          }
        }
        completed.sort((a, b) => a.part_no - b.part_no);
        await api.completeUpload(csrf, upload, completed); uploadRef.current = null;
      }
      setNotice(t.uploadDone);
    } catch { if (uploadRef.current) void api.abortUpload(csrf, uploadRef.current.id); setNotice(t.error); }
    finally { setBusy(false); }
  }

  if (phase !== "ready") return <main className="center"><section className="card" role="status"><h1>PhotoEditor</h1><p>{phase === "loading" ? t.loading : phase === "invalid" ? t.invalid : t.error}</p>{phase !== "loading" && <button onClick={() => void load()}>{t.retry}</button>}</section></main>;
  return <main>
    <header className="header"><div><p className="eyebrow">PhotoEditor</p><h1>{collection?.name}</h1>{collection?.description && <p>{collection.description}</p>}</div><nav aria-label={t.language}>{(["ru", "bg", "en"] as const).map(code => <a key={code} aria-current={code === locale ? "page" : undefined} href={`/${code}/s/${shareID}`}>{code.toUpperCase()}</a>)}</nav></header>
    <section className="toolbar" aria-label={t.photos}>{can(collection, "view") && <><strong>{items.length} {t.photos.toLowerCase()}</strong><span>{selected.size} {t.selected}</span>{selected.size > 0 && <button className="quiet" onClick={() => setSelected(new Set())}>{t.clear}</button>}</>}{can(collection, "download") && <button onClick={() => void createDownload()} disabled={busy || (can(collection, "view") && !selected.size)}>{t.download}</button>}{can(collection, "upload") && <label className="button">{t.upload}<input hidden multiple type="file" accept="image/jpeg,image/png,image/tiff,.dng,.cr2,.nef,.arw,.raf,.orf,.rw2" disabled={busy} onChange={e => void uploadFiles(e.target.files)} /></label>}</section>
    {notice && <p className="notice" role="status">{notice}</p>}
    {can(collection, "view") && (collection?.empty ? <p className="empty">{t.empty}</p> : <section ref={gridRef} className="virtual-grid" aria-label={t.photos} style={{height:windowed.totalHeight}}>{Array.from({length:windowed.endRow-windowed.startRow},(_,offset)=>{const row=windowed.startRow+offset;return <div className="virtual-row" style={{top:row*grid.rowHeight,gridTemplateColumns:`repeat(${grid.columns},minmax(0,1fr))`}} key={row}>{items.slice(row*grid.columns,Math.min(items.length,(row+1)*grid.columns)).map((item,column) => { const index=row*grid.columns+column;const image = links[`${item.id}:thumb640`]; return <article className={selected.has(item.id) ? "tile chosen" : "tile"} key={item.id}><button className="image" onClick={() => setLightbox(index)} aria-label={item.title || `${t.photos} ${index + 1}`}>{image ? <img src={image.url} width={image.width} height={image.height} alt={item.title || ""} loading="lazy" /> : <span>{t.unavailable}</span>}</button><label><input type="checkbox" checked={selected.has(item.id)} onChange={() => toggle(item.id)} /> {t.select}</label></article>; })}</div>})}</section>)}
    {cursor && items.length < 5000 && <div className="more"><button onClick={() => void more()} disabled={busy}>{t.loadMore}</button></div>}
    {lightbox !== null && items[lightbox] && <div className="lightbox" role="dialog" aria-modal="true" aria-label={items[lightbox].title}><button className="close" onClick={() => setLightbox(null)} aria-label={t.close}>×</button>{links[`${items[lightbox].id}:preview2560`] ? <img src={links[`${items[lightbox].id}:preview2560`].url} alt={items[lightbox].title || ""} /> : <p>{t.unavailable}</p>}<div className="light-controls"><button disabled={lightbox === 0} onClick={() => setLightbox(v => Math.max(0, (v || 0) - 1))}>{t.previous}</button><span>{lightbox + 1} / {items.length}</span>{can(collection, "download") && <button disabled={busy} onClick={() => void downloadItem(items[lightbox])}>{t.download}</button>}<button disabled={lightbox === items.length - 1} onClick={() => setLightbox(v => Math.min(items.length - 1, (v || 0) + 1))}>{t.next}</button></div></div>}
    {download && <aside className="download" aria-live="polite"><strong>{t.progress}: {download.files_done}/{download.files_total}</strong><p>{download.status}</p>{download.status === "completed" && download.volumes.map(v => <button key={v.number} onClick={() => void downloadVolume(v.number)}>{t.downloadReady} {v.number}</button>)}{!terminal.has(download.status) && <button className="quiet" onClick={() => void api.cancelDownload(csrf, download.id).then(setDownload)}>{t.cancel}</button>}</aside>}
  </main>;
}
