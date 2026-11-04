"use client";
import { useCallback, useEffect, useState } from "react";
import * as api from "../lib/api";
import { messages, type Locale } from "../lib/i18n";
import { durationLabel, type VideoDelivery } from "../lib/video-response";
import { LocaleLinks } from "./LocaleLinks";

type Phase = "loading" | "ready" | "invalid" | "error";

/** Client page of a published video delivery: the link secret is exchanged for the portal session, then the renders play from it. */
export function VideoPortal({ locale, shareID }: { locale: Locale; shareID: string }) {
  const t = messages[locale];
  const [phase, setPhase] = useState<Phase>("loading");
  const [delivery, setDelivery] = useState<VideoDelivery | null>(null);
  const [canDownload, setCanDownload] = useState(false);
  const [current, setCurrent] = useState(0);

  const load = useCallback(async (signal: AbortSignal) => {
    setPhase("loading");
    try {
      const secret = api.secretFromFragment(location.hash);
      if (secret) {
        const session = await api.exchange(shareID, secret, locale, signal);
        setCanDownload(session.capabilities.download === true);
        history.replaceState(history.state, "", location.pathname + location.search);
        window.dispatchEvent(new Event("portal-location-change"));
      }
      const next = await api.getVideoDelivery(signal);
      if (signal.aborted) return;
      setDelivery(next);
      setCurrent(0);
      setPhase("ready");
    } catch (error) {
      if (signal.aborted) return;
      setPhase(error instanceof api.APIError && [401, 403, 404, 410].includes(error.status) ? "invalid" : "error");
    }
  }, [shareID, locale]);

  useEffect(() => {
    const flight = new AbortController();
    void load(flight.signal);
    return () => flight.abort();
  }, [load]);

  const language = <LocaleLinks locale={locale} path={`/${locale}/v/${shareID}`} label={t.language} />;
  if (phase !== "ready" || !delivery) {
    return <main className="center" lang={locale}><section className="card" role="status">
      <h1>Framevela</h1>
      <p>{phase === "loading" ? t.loading : phase === "invalid" ? t.invalid : t.error}</p>
      {phase === "error" && <button onClick={() => void load(new AbortController().signal)}>{t.retry}</button>}
      {language}
    </section></main>;
  }
  const item = delivery.items[current];
  return <main className="center" lang={locale}><section className="card">
    <h1>{delivery.name}</h1>
    {item ? <>
      <video key={item.render_id} controls preload="metadata" playsInline aria-label={`${t.videoPlayer}: ${item.title}`}
        style={{ width: "100%", maxHeight: "70vh", background: "#000" }} src={api.videoContentURL(item.render_id)} />
      <p>{item.title} · {durationLabel(item.duration_ticks)} · {item.width}×{item.height}</p>
      {canDownload && <a href={api.videoContentURL(item.render_id, true)} download>{t.videoDownload}</a>}
      {delivery.items.length > 1 && <nav aria-label={t.videoList}><ul>
        {delivery.items.map((it, i) => <li key={it.render_id}><button aria-current={i === current ? "true" : undefined} onClick={() => setCurrent(i)}>{it.title}</button></li>)}
      </ul></nav>}
    </> : <p>{t.videoNoItems}</p>}
    {language}
  </section></main>;
}
