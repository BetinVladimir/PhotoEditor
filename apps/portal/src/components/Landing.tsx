import Link from 'next/link';
import {LocaleLinks} from './LocaleLinks';
import {siteMessages} from '../lib/site-i18n';
import type {Locale} from '../lib/i18n';
import '../app/landing.css';
export function Landing({locale}:{locale:Locale}){
const t=siteMessages[locale];
const steps = [
  ["01", t.landing_003, t.landing_004],
  ["02", t.landing_005, t.landing_006],
  ["03", t.landing_007, t.landing_008],
];

  return <div className="fv-landing" lang={locale}>
    <a className="fv-skip" href="#main">{t.landing_013}</a>
    <header className="fv-nav">
      <Link href={`/${locale}`} className="fv-brand" aria-label={t.landing_009}><img className="fv-brand-mark" src="/brand/mark-light.svg" width="32" height="32" alt="" aria-hidden="true" />framevela<span className="fv-brand-dot">.</span></Link>
      <nav aria-label={t.landing_010}><a href="#approach">{t.landing_014}</a><a href="#gallery">{t.landing_015}</a><a href={`https://app.framevela.com/${locale}/login`}>{t.login}</a><a className="fv-nav-cta" href={`https://app.framevela.com/${locale}/register`}>{t.register} <span aria-hidden="true">↗</span></a></nav>
    </header>
    <main id="main">
      <section className="fv-hero" aria-labelledby="hero-title">
        <div className="fv-hero-copy"><p className="fv-eyebrow"><span aria-hidden="true" /> {t.landing_017}</p>
          <h1 id="hero-title">{t.landing_018}<br />{t.landing_019} <em>{t.landing_020}</em></h1>
          <p className="fv-lead">{t.landing_021}</p>
          <a className="fv-primary" href="#approach">{t.landing_022} <span aria-hidden="true">↗</span></a>
          <p className="fv-hero-note">{t.landing_023}</p>
        </div>
        <div className="fv-art" role="img" aria-label={t.landing_011}>
          <div className="fv-art-grid" />
          <div className="fv-art-label">{t.landing_024} <span>{t.landing_025}</span></div>
          <div className="fv-photo fv-photo-back"><div className="fv-landscape fv-landscape-muted" /><span>{t.landing_026}</span></div>
          <div className="fv-photo fv-photo-main"><div className="fv-landscape"><span className="fv-sun" /><span className="fv-hill fv-hill-far" /><span className="fv-hill fv-hill-near" /><span className="fv-tree" /></div><div className="fv-photo-caption"><span>{t.landing_027}</span><span>01 — 03</span></div></div>
          <div className="fv-adjust"><span className="fv-adjust-title">{t.landing_028} <span>✳</span></span><div className="fv-swatches"><i /><i /><i /><i /><i /></div><div className="fv-slider"><span /></div><span className="fv-adjust-note">{t.landing_029}</span></div>
          <div className="fv-art-stamp">{t.landing_030}<br /><em>{t.landing_031}</em></div>
        </div>
      </section>
      <div className="fv-principles"><span>{t.landing_032}</span><span>{t.landing_033}</span><span>{t.landing_034}</span></div>
      <section className="fv-approach" id="approach" aria-labelledby="approach-title">
        <div className="fv-section-heading"><p className="fv-eyebrow">{t.landing_035}</p><h2 id="approach-title">{t.landing_036}<br /><em>{t.landing_037}</em></h2><p>{t.landing_038}</p></div>
        <div className="fv-steps">{steps.map(([number, title, text]) => <article key={number}><span className="fv-step-number">{number}</span><h3>{title}</h3><p>{text}</p></article>)}</div>
      </section>
      <section className="fv-gallery" id="gallery" aria-labelledby="gallery-title"><div><p className="fv-eyebrow">{t.landing_039}</p><h2 id="gallery-title">{t.landing_040}<br /><em>{t.landing_041}</em></h2></div><div className="fv-gallery-copy"><p>{t.landing_042}</p><p>{t.landing_043}</p><a href="#portal">{t.landing_044} <span aria-hidden="true">↗</span></a></div></section>
      <section className="fv-portal" id="portal" aria-labelledby="portal-title"><p className="fv-eyebrow">{t.landing_045}</p><h2 id="portal-title">{t.landing_046}<br /><em>{t.landing_047}</em></h2><p>{t.portal_language_prompt}<br />{t.landing_049}</p><LocaleLinks locale={locale} path={`/${locale}`} label={t.landing_012} /></section>
    </main>
    <footer className="fv-footer"><Link href={`/${locale}`} className="fv-brand">framevela<span className="fv-brand-dot">.</span></Link><p>{t.landing_053}</p><a href="#main">{t.landing_054}</a></footer>
  </div>;
}
