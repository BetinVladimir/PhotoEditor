"use client";
import {useEffect,useState} from 'react';
import {locales,type Locale} from '../lib/i18n';
import {languageNames} from '../lib/site-i18n';
export function localeURL(href:string,locale:Locale){
 const url=new URL(href); const segments=url.pathname.split('/');segments[1]=locale;
 return '/'+segments.slice(1).join('/')+url.search+url.hash;
}
export function LocaleLinks({locale,path,label}:{locale:Locale,path:string,label:string}){
 const [href,setHref]=useState((process.env.NEXT_PUBLIC_SITE_ORIGIN||'http://localhost')+path);
 useEffect(()=>{const sync=()=>setHref(window.location.href);sync();window.addEventListener('hashchange',sync);window.addEventListener('popstate',sync);window.addEventListener('portal-location-change',sync);return()=>{window.removeEventListener('hashchange',sync);window.removeEventListener('popstate',sync);window.removeEventListener('portal-location-change',sync);};},[path]);
 return <nav aria-label={label}>{locales.map(code=><a key={code} lang={code} hrefLang={code} aria-current={code===locale?'page':undefined} href={localeURL(href,code)} onClick={event=>{event.currentTarget.href=localeURL(window.location.href,code);}}>{languageNames[code]} <span aria-hidden="true">↗</span></a>)}</nav>;
}
