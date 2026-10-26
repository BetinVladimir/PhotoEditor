"use client";
import {AccessibleLightbox} from "./AccessibleLightbox";

import {uploadRecoverably} from '../lib/upload-recovery';
import {downloadFailureItems} from '../lib/download-failure';

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { prepareDownload, clearPreparation, hasPreparation, abandonPreparation, preparationScope, scopeLegacyPreparation } from "../lib/download-preparation";
import { readDownloadJob, rememberDownloadJob } from "../lib/download-recovery";
import { downloadReady, downloadTerminal, downloadNeedsPolling } from "../lib/download-state";
import * as api from "../lib/api";
import {LocaleLinks} from './LocaleLinks';
import {siteMessages,statusLabel} from '../lib/site-i18n';
import { messages, type Locale } from "../lib/i18n";
import { MediaCache } from "../lib/media-cache";
import { virtualWindow } from "../lib/virtual-grid";
import type { Collection, DownloadStatus, GuestItem, MediaLink, Preset } from "../lib/types";

function can(collection: Collection | null, name: string) {
  const value = collection?.capabilities;
  return Array.isArray(value) ? value.includes(name) : Boolean(value?.[name]);
}
function presetID(preset: Preset) { return typeof preset === "string" ? preset : preset.id; }

export function Portal({ locale, shareID }: { locale: Locale; shareID: string }) {
  const t = messages[locale];
  const st=siteMessages[locale];
  const galleryHeading=useRef<HTMLHeadingElement|null>(null);
  const readFlight=useRef<AbortController|null>(null);
  const mediaFlights=useRef(new Set<AbortController>());
  const authority=useRef({shareID,locale,epoch:0});
  if(authority.current.shareID!==shareID||authority.current.locale!==locale){
    authority.current={shareID,locale,epoch:authority.current.epoch+1};
    readFlight.current?.abort();for(const flight of mediaFlights.current)flight.abort();mediaFlights.current.clear();
  }

  const cache = useMemo(() => new MediaCache(), []);
  const [phase, setPhase] = useState<"loading" | "ready" | "invalid" | "error">("loading");
  const [readyAuthority,setReadyAuthority]=useState<typeof authority.current|null>(null);
  const [csrf, setCSRF] = useState("");
  const [collection, setCollection] = useState<Collection | null>(null);
  const [items, setItems] = useState<GuestItem[]>([]);
  const [cursor, setCursor] = useState<string>();
  const [links, setLinks] = useState<Record<string, MediaLink>>({});
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [lightbox, setLightbox] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [pendingPreparation, setPendingPreparation] = useState(false);
  const [download, setDownload] = useState<DownloadStatus | null>(null);
  const [downloadVisible,setDownloadVisible]=useState(false);
  const [downloadReceiptPending,setDownloadReceiptPending]=useState(false);
  const downloadCancellation = useRef<AbortController | null>(null);
  const downloadPreparation = useRef<AbortController | null>(null);
  const downloadFlight = useRef<AbortController | null>(null);
  const downloadKeys = useRef(new Map<string, string>());
  const downloadPoll = useRef<AbortController | null>(null);
  function refuseDownloadAccess(error: unknown) {
    if (!(error instanceof api.APIError) || ![401,403,404,410].includes(error.status)) return false;
    // A terminal server refusal invalidates the visible authority projection.
    // Keep immutable pending preparation; only an explicit authorized retry may
    // recover it. No new POST follows a refusal.
    authority.current={...authority.current,epoch:authority.current.epoch+1};setReadyAuthority(null);
    actionFlight.current?.abort();actionFlight.current=null;
    downloadPoll.current?.abort(); downloadPoll.current=null;
    downloadCancellation.current?.abort(); downloadCancellation.current=null;
    downloadPreparation.current?.abort(); downloadPreparation.current=null;
    downloadFlight.current?.abort(); downloadFlight.current=null;
    rememberDownloadJob(sessionStorage,shareID,null);
    setDownload(null);setDownloadReceiptPending(false);setCollection(null);
    setLightbox(null);setItems([]);setSelected(new Set());cache.clear();setLinks({});
    setBusy(false);setNotice(t.error);setPhase("invalid");
    return true;
  }
  useEffect(() => {
    const cancel = () => { downloadCancellation.current?.abort(); downloadCancellation.current = null; downloadPreparation.current?.abort(); downloadPreparation.current = null; downloadFlight.current?.abort(); downloadFlight.current = null; setBusy(false); };
    const hidden = () => { const visible=document.visibilityState === "visible";setDownloadVisible(visible);if (!visible) cancel(); };
    hidden();
    document.addEventListener("visibilitychange", hidden);
    window.addEventListener("pagehide", cancel);
    return () => { cancel(); document.removeEventListener("visibilitychange", hidden); window.removeEventListener("pagehide", cancel); };
  }, [shareID, csrf]);
  const actionFlight=useRef<AbortController|null>(null);
  const uploadFilesRef=useRef<File[]|null>(null);
  const displayedAuthority=readyAuthority;
  const actionCurrent=()=>phase==='ready'&&displayedAuthority!==null&&authority.current===displayedAuthority&&document.visibilityState==='visible';
  function detachAction(controller:AbortController){
    if(downloadPreparation.current===controller)downloadPreparation.current=null;
    if(downloadCancellation.current===controller)downloadCancellation.current=null;
    if(downloadFlight.current===controller)downloadFlight.current=null;
  }
  function stopAction(displayed:AbortController|null){
    if(!displayed||actionFlight.current!==displayed||!actionCurrent())return;
    actionFlight.current=null;detachAction(displayed);displayed.abort();setBusy(false);setNotice(t.error);
  }
  function beginAction(ms=15000){
    if(!actionCurrent()||actionFlight.current)return null;
    const controller=new AbortController();actionFlight.current=controller;
    const current=()=>!controller.signal.aborted&&actionFlight.current===controller&&authority.current===displayedAuthority&&document.visibilityState==='visible';
    const timeout=window.setTimeout(()=>{if(current()){actionFlight.current=null;detachAction(controller);controller.abort();setBusy(false);setNotice(t.error);}},ms);
    controller.signal.addEventListener('abort',()=>window.clearTimeout(timeout),{once:true});
    const finish=()=>{window.clearTimeout(timeout);if(actionFlight.current===controller){actionFlight.current=null;setBusy(false);}};
    return {controller,current,finish};
  }
  useEffect(()=>{
    const stop=()=>{actionFlight.current?.abort();actionFlight.current=null;setBusy(false);};
    const hidden=()=>{if(document.visibilityState!=='visible')stop();};
    document.addEventListener('visibilitychange',hidden);window.addEventListener('pagehide',stop);
    return()=>{stop();uploadFilesRef.current=null;document.removeEventListener('visibilitychange',hidden);window.removeEventListener('pagehide',stop);};
  },[shareID,locale,csrf]);
  const gridRef = useRef<HTMLElement | null>(null);
  const [grid, setGrid] = useState({ columns: 2, rowHeight: 260, scrollTop: 0, viewportHeight: 900 });
  const windowed = useMemo(() => virtualWindow(items.length, grid.columns, grid.rowHeight, grid.scrollTop, grid.viewportHeight), [grid, items.length]);
  const visibleItems = useMemo(() => items.slice(windowed.start, windowed.end), [items, windowed.end, windowed.start]);

  const fetchMedia = useCallback(async (batch: GuestItem[], variant: "thumb640" | "preview2560") => {
    if (!csrf) return;
    const expected=authority.current;
    const controller=new AbortController();mediaFlights.current.add(controller);
    const current=()=>!controller.signal.aborted&&authority.current===expected;
    const timeout=window.setTimeout(()=>controller.abort(),8000);
    try{
      const missing = batch.filter(i => !cache.get(i.id, variant));
      if(missing.length){
        const result=await api.getMediaLinks(csrf,missing.slice(0,100).map(i=>i.id),[variant],controller.signal);
        if(!current())return;
        cache.put(result.items);
      }
      if(!current())return;
      setLinks(previous=>{
        if(!current())return previous;
        const next={...previous};for(const item of batch){const link=cache.get(item.id,variant);if(link)next[`${item.id}:${variant}`]=link;}
        const entries=Object.entries(next);return entries.length<=512?next:Object.fromEntries(entries.slice(-512));
      });
    }catch(error){if(current())throw error;}finally{window.clearTimeout(timeout);mediaFlights.current.delete(controller);}

  }, [cache, csrf]);

  const load = useCallback(async (externalSignal?: AbortSignal) => {
    readFlight.current?.abort();for(const flight of mediaFlights.current)flight.abort();mediaFlights.current.clear();
    const controller=new AbortController();readFlight.current=controller;
    const signal=controller.signal;
    const externalAbort=()=>controller.abort();externalSignal?.addEventListener('abort',externalAbort,{once:true});
    if(externalSignal?.aborted)controller.abort();
    const expected={shareID,locale,epoch:authority.current.epoch+1};authority.current=expected;
    const current=()=>!signal.aborted&&readFlight.current===controller&&authority.current===expected;
    const timeout=window.setTimeout(()=>{if(current()){setCollection(null);setItems([]);setLinks({});setPhase('error');controller.abort();}},10000);

    actionFlight.current?.abort();actionFlight.current=null;
    downloadCancellation.current?.abort(); downloadPreparation.current?.abort(); downloadFlight.current?.abort();
    setPendingPreparation(false); setDownloadReceiptPending(false); setDownload(null); setCollection(null); setLightbox(null);
    setPhase("loading"); setNotice(""); cache.clear(); setLinks({}); setItems([]); setSelected(new Set());
    try {
      let token = sessionStorage.getItem(`photo:portal:${shareID}:csrf`) || "";
      scopeLegacyPreparation(sessionStorage,shareID,token);
      const secret = api.secretFromFragment(location.hash);
      if (secret) {
        rememberDownloadJob(sessionStorage, shareID, null);
        const session = await api.exchange(shareID, secret, locale, signal);
        if(!current())return;
        token = session.csrf_token;
        sessionStorage.setItem(`photo:portal:${shareID}:csrf`, token);
        history.replaceState(history.state, "", location.pathname + location.search);window.dispatchEvent(new Event("portal-location-change"));
      }
      if(!current())return;
      if (!token) { setPhase("invalid"); return; }
      setCSRF(token);
      const meta = await api.getCollection(signal);
      if(!current())return;
      setCollection(meta);
      if (can(meta, "view")) {
        const page = await api.getItems(undefined, undefined, signal,meta.revision); if(!current())return;setItems(page.items.slice(0, 5000)); setCursor(page.next_cursor);
      }
      if (can(meta, "download")) {
        const job = readDownloadJob(sessionStorage, shareID);
        if (job) {
          try { const recovered=await api.getDownload(job,signal);if(!current())return;setDownload(recovered); }
          catch (error) {
            if(!current())return;
            if (error instanceof api.APIError && [401, 403, 404, 410].includes(error.status)) rememberDownloadJob(sessionStorage, shareID, null);
            else throw error;
          }
        }
      }
      if(!current())return;
      setPendingPreparation(hasPreparation(sessionStorage, preparationScope(shareID,token)));
      setReadyAuthority(expected);setPhase("ready");
    } catch (error) {
      if(!current())return;
      if (error instanceof api.APIError && (error.status === 401 || error.status === 403 || error.status === 404 || error.status === 410)) setPhase("invalid");
      else setPhase("error");
    }finally{window.clearTimeout(timeout);externalSignal?.removeEventListener('abort',externalAbort);}
  }, [cache, locale, shareID]);

  useEffect(() => { const controller = new AbortController(); void load(controller.signal); return () => {controller.abort();readFlight.current?.abort();for(const flight of mediaFlights.current)flight.abort();mediaFlights.current.clear();}; }, [load]);
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
    if (phase !== "ready" || !download || !downloadNeedsPolling(download.status,downloadReceiptPending,downloadVisible)) return;
    const controller = new AbortController();downloadPoll.current=controller;
    let timer: number | undefined;
    let failures = 0;
    const poll = async () => {
      try {
        const value = await api.getDownload(download.id, controller.signal);
        if (!controller.signal.aborted) {setDownload(value);setDownloadReceiptPending(false);}
      } catch (error) {
        if (controller.signal.aborted) return;
        if (refuseDownloadAccess(error)) return;
        setNotice(t.error);
        failures += 1;
        timer = window.setTimeout(() => void poll(), Math.min(15000, 1500 * 2 ** Math.min(failures, 4)));
      }
    };
    timer = window.setTimeout(() => void poll(), 1500);
    return () => { controller.abort();if(downloadPoll.current===controller)downloadPoll.current=null; window.clearTimeout(timer); };
  }, [download, downloadVisible, downloadReceiptPending, phase, shareID, csrf, t.error]);

  async function more() {
    if (!cursor || items.length >= 5000 || busy || phase!=='ready'||readyAuthority!==authority.current) return;
    const expected=authority.current,requestedCursor=cursor,controller=new AbortController();mediaFlights.current.add(controller);
    const current=()=>!controller.signal.aborted&&authority.current===expected;
    const timeout=window.setTimeout(()=>{if(current()){setNotice(t.error);setBusy(false);controller.abort();}},8000);
    setBusy(true);
    try {
      const page=await api.getItems(requestedCursor,undefined,controller.signal,collection?.revision);if(!current())return;
      const ids=new Set(items.map(item=>item.id));
      if(page.next_cursor===requestedCursor||page.items.some(item=>ids.has(item.id))||new Set(page.items.map(item=>item.id)).size!==page.items.length)throw new Error('Invalid gallery page');
      setItems(previous=>current()?[...previous,...page.items].slice(0,5000):previous);setCursor(page.next_cursor);
    }catch(error){if(current()){if(!refuseDownloadAccess(error))setNotice(t.error);}}
    finally{window.clearTimeout(timeout);mediaFlights.current.delete(controller);if(current())setBusy(false);}
  }
  function toggle(id: string) { setSelected(current => { const next = new Set(current); next.has(id) ? next.delete(id) : next.add(id); return next; }); }
  async function createDownload() {
    if (phase !== "ready" || !can(collection, "download") || downloadPreparation.current || downloadFlight.current) return;
    if (!collection || (can(collection, "view") && selected.size === 0 && !pendingPreparation)) { setNotice(t.selectionRequired); return; }
    const preset = collection.allowed_presets[0]; if (!preset) { setNotice(t.noPermission); return; }
    const action=beginAction();if(!action)return;const {controller}=action;downloadPreparation.current=controller;
    setBusy(true); setNotice(t.downloadPreparing);
    try {
      const accepted = await prepareDownload(api, sessionStorage, preparationScope(shareID,csrf), csrf, { revision: collection.revision, preset: presetID(preset), items: can(collection, "view") ? [...selected] : undefined }, controller.signal);
      if (action.current() && downloadPreparation.current === controller) {
        rememberDownloadJob(sessionStorage, shareID, accepted.download_job_id);
        clearPreparation(sessionStorage, preparationScope(shareID,csrf));
        setDownloadReceiptPending(true);
        setDownload({ id: accepted.download_job_id, status: accepted.status, files_done: 0, files_total: accepted.item_count, bytes_written: 0, volumes: [] });
      }
    } catch (error) {
      if (action.current() && downloadPreparation.current === controller) {
        // An uncertain command keeps both keys even after a later typed refusal.
        setNotice(t.error);
      }
    }
    finally { if (downloadPreparation.current === controller) { downloadPreparation.current = null;if(action.current())setPendingPreparation(hasPreparation(sessionStorage, preparationScope(shareID,csrf))); }action.finish(); }
  }
  async function abandonPendingDownload(){
    if(!collection||!can(collection,"download"))return;
    const action=beginAction();if(!action)return;setBusy(true);
    try{
      const resolved=await abandonPreparation(api,sessionStorage,preparationScope(shareID,csrf),csrf,action.controller.signal);
      if(!action.current())return;
      if(resolved.result.status==='accepted'){
        const ticket=resolved.result.ticket;
        rememberDownloadJob(sessionStorage,shareID,ticket.download_job_id);
        setDownloadReceiptPending(true);
        setDownload({id:ticket.download_job_id,status:ticket.status,files_done:0,files_total:resolved.item_count,bytes_written:0,volumes:[]});
      }
      resolved.clear();setPendingPreparation(false);setNotice("");
    }catch(error){if(action.current()&&!refuseDownloadAccess(error))setNotice(t.error);}
    finally{action.finish();}
  }
  async function cancelDownloadJob() {
    if (!download || downloadTerminal(download.status) || downloadCancellation.current) return;
    const id = download.id;
    const action=beginAction();if(!action)return;const {controller}=action;downloadCancellation.current=controller;setBusy(true);
    try {
      const value = await api.cancelDownload(csrf, id, controller.signal);
      if (action.current() && downloadCancellation.current === controller) setDownload(current => current?.id === id ? value : current);
    } catch (error) { if (action.current() && downloadCancellation.current === controller && !refuseDownloadAccess(error)) setNotice(t.error); }
    finally { if (downloadCancellation.current === controller) downloadCancellation.current = null;action.finish(); }
  }
  async function openDownload(slot: string, fetchLink: (signal: AbortSignal, key: string) => Promise<{ url: string }>) {
    // PT-3: reuse one Idempotency-Key per logical download until it succeeds, so a retry after a network error cannot create a second intent.
    const key = downloadKeys.current.get(slot) ?? api.idempotencyKey();
    downloadKeys.current.set(slot, key);
    if (phase !== "ready" || !can(collection, "download") || downloadFlight.current || document.visibilityState !== "visible") return;
    const action=beginAction();if(!action)return;const {controller}=action;downloadFlight.current=controller;
    setBusy(true);
    try {
      const link = await fetchLink(controller.signal, key);
      downloadKeys.current.delete(slot);
      if (action.current() && downloadFlight.current === controller && document.visibilityState === "visible") location.assign(link.url);
    } catch (error) {
      if (action.current() && downloadFlight.current === controller && !refuseDownloadAccess(error)) setNotice(t.error);
    } finally {
      if (downloadFlight.current === controller) downloadFlight.current = null;action.finish();
    }
  }
  async function downloadVolume(volume: number) {
    if (!download || !downloadReady(download.status) || !download.volumes.some(v => v.number === volume && downloadReady(v.status))) return;
    const id = download.id;
    await openDownload(`vol:${id}:${volume}`, (signal, key) => api.redeemDownload(csrf, id, volume, signal, key));
  }
  async function downloadItem(item: GuestItem) {
    const preset = collection?.allowed_presets[0]; if (!preset) { setNotice(t.noPermission); return; }
    await openDownload(`item:${item.id}:${presetID(preset)}`, (signal, key) => api.downloadItem(csrf, item.id, presetID(preset), signal, key));
  }
  async function uploadFiles(files: FileList | File[] | null) {
    if(!files?.length||!can(collection,'upload'))return;
    const action=beginAction(300000);if(!action)return;
    const frozen=Array.from(files);uploadFilesRef.current=frozen;
    setBusy(true);setNotice(t.uploading);
    try {
      for(let index=0;index<frozen.length;index++){
        const file=frozen[index];
        if(!action.current())return;
        uploadFilesRef.current=frozen.slice(index);
        await uploadRecoverably(sessionStorage,shareID,csrf,file,action.controller.signal,action.current);
        if(!action.current())return;
      }
      uploadFilesRef.current=null;setNotice(t.uploadDone);
    } catch(error){if(action.current()&&!refuseDownloadAccess(error))setNotice(t.error);}
    finally{action.finish();}
  }
  const displayedAction=actionFlight.current;

  if (phase !== "ready"||readyAuthority!==authority.current) return <main className="center" lang={locale}><section className="card" role="status"><h1>Framevela</h1><p>{phase === "loading" ? t.loading : phase === "invalid" ? t.invalid : t.error}</p>{phase === "loading"?<button onClick={()=>{readFlight.current?.abort();setPhase("error");}}>{t.cancel}</button>:<button onClick={() => void load()}>{t.retry}</button>}<LocaleLinks locale={locale} path={`/${locale}/s/${shareID}`} label={t.language} /></section></main>;
  return <main lang={locale}>
    <header className="header"><div><p className="eyebrow">Framevela</p><h1 ref={galleryHeading} tabIndex={-1}>{collection?.name}</h1>{collection?.description && <p>{collection.description}</p>}</div><LocaleLinks locale={locale} path={`/${locale}/s/${shareID}`} label={t.language} /></header>
    <section className="toolbar" aria-label={t.photos}>{can(collection, "view") && <><strong>{items.length} {t.photos.toLowerCase()}</strong><span>{selected.size} {t.selected}</span>{selected.size > 0 && <button className="quiet" onClick={() => setSelected(new Set())}>{t.clear}</button>}</>}{can(collection, "download") && <button onClick={() => void createDownload()} disabled={busy || (can(collection, "view") && !selected.size && !pendingPreparation)}>{t.download}</button>}{can(collection, "upload") && <label className="button">{t.upload}<input className="visually-hidden" multiple type="file" accept="image/jpeg,image/png,image/tiff,.dng,.cr2,.nef,.arw,.raf,.orf,.rw2" disabled={busy} onChange={e => void uploadFiles(e.target.files)} /></label>}</section>
    {busy&&displayedAction&&<button onClick={()=>stopAction(displayedAction)}>{st.stop_waiting}</button>}
    {!busy&&uploadFilesRef.current&&can(collection,'upload')&&<button onClick={()=>{if(actionCurrent())void uploadFiles(uploadFilesRef.current);}}>{t.retry}</button>}
    {!busy&&pendingPreparation&&can(collection,'download')&&<button onClick={()=>void abandonPendingDownload()}>{st.resolve_pending_preparation}</button>}
    {notice && <p className="notice" role="status">{notice}</p>}
    {can(collection, "view") && (collection?.empty ? <p className="empty">{t.empty}</p> : <section ref={gridRef} className="virtual-grid" aria-label={t.photos} style={{height:windowed.totalHeight}}>{Array.from({length:windowed.endRow-windowed.startRow},(_,offset)=>{const row=windowed.startRow+offset;return <div className="virtual-row" style={{top:row*grid.rowHeight,gridTemplateColumns:`repeat(${grid.columns},minmax(0,1fr))`}} key={row}>{items.slice(row*grid.columns,Math.min(items.length,(row+1)*grid.columns)).map((item,column) => { const index=row*grid.columns+column;const image = links[`${item.id}:thumb640`]; return <article className={selected.has(item.id) ? "tile chosen" : "tile"} key={item.id}><button className="image" onClick={() => setLightbox(index)} aria-label={item.title || `${t.photos} ${index + 1}`}>{image ? <img src={image.url} width={image.width} height={image.height} alt={item.title || ""} loading="lazy" /> : <span>{t.unavailable}</span>}</button><label><input type="checkbox" checked={selected.has(item.id)} onChange={() => toggle(item.id)} /> {t.select}</label></article>; })}</div>})}</section>)}
    {cursor && items.length < 5000 && <div className="more"><button onClick={() => void more()} disabled={busy}>{t.loadMore}</button></div>}
    {lightbox !== null && items[lightbox] && <AccessibleLightbox name={items[lightbox].title?.trim() || `${t.photos} ${lightbox + 1}`} onClose={() => setLightbox(null)} restoreFallback={() => galleryHeading.current}><button className="close" onClick={() => setLightbox(null)} aria-label={t.close}>×</button>{links[`${items[lightbox].id}:preview2560`] ? <img src={links[`${items[lightbox].id}:preview2560`].url} alt={items[lightbox].title || ""} /> : <p>{t.unavailable}</p>}<div className="light-controls"><button disabled={lightbox === 0} onClick={() => setLightbox(v => Math.max(0, (v || 0) - 1))}>{t.previous}</button><span>{lightbox + 1} / {items.length}</span>{can(collection, "download") && <button disabled={busy} onClick={() => void downloadItem(items[lightbox])}>{t.download}</button>}<button disabled={lightbox === items.length - 1} onClick={() => setLightbox(v => Math.min(items.length - 1, (v || 0) + 1))}>{t.next}</button></div></AccessibleLightbox>}
    {download && <aside className="download" aria-live="polite"><strong>{t.progress}: {download.files_done}/{download.files_total} · {download.bytes_written} {st.bytes}</strong><p>{statusLabel(locale,download.status)}</p>{download.status === "failed" && downloadFailureItems(download.error).map(item => <p role="alert" key={item.ordinal}>{item.ordinal + 1}: {st.download_item_failure}</p>)}{downloadReady(download.status) && download.volumes.filter(v => downloadReady(v.status)).map(v => <button disabled={busy} key={v.number} onClick={() => void downloadVolume(v.number)}>{t.downloadReady} {v.number}</button>)}{!downloadTerminal(download.status) && <button className="quiet" onClick={() => void cancelDownloadJob()}>{t.cancel}</button>}</aside>}
  </main>;
}
