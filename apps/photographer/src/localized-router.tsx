import {useMemo,type ComponentProps,type ReactNode} from 'react';
import {Platform} from 'react-native';
import {router as actual,Link as ActualLink,Redirect as ActualRedirect,useRouter as actualUseRouter,usePathname,useGlobalSearchParams,type Href} from 'expo-router';
import {localizedURL,splitLocaleURL,isURLLocale,type URLLocale} from './locale-url';
export * from 'expo-router';
function currentLocale():URLLocale {if(Platform.OS==='web'&&typeof window!=='undefined')return splitLocaleURL(window.location.pathname).locale??'en';return 'en';}
export function localeHref(href:Href,locale=currentLocale()):Href {
 if(Platform.OS!=='web')return href;
 if(typeof href==='string')return href.startsWith('/')&&!href.startsWith('//')?localizedURL(href,locale) as Href:href;
 if(!href.pathname?.startsWith('/')||href.pathname.startsWith('//'))return href;
 return {...href,pathname:localizedURL(href.pathname,locale),params:href.params} as Href;
}
// No global monkeypatch: all existing back/setParams/dismiss/authority semantics retained.
function localizedRouter<T extends typeof actual>(value:T):T{return {...value,push:(href:Href,...args:unknown[])=>Reflect.apply(value.push,value,[localeHref(href),...args]),replace:(href:Href,...args:unknown[])=>Reflect.apply(value.replace,value,[localeHref(href),...args]),navigate:(href:Href,...args:unknown[])=>Reflect.apply(value.navigate,value,[localeHref(href),...args]),dismissTo:(href:Href,...args:unknown[])=>Reflect.apply(value.dismissTo,value,[localeHref(href),...args])} as T;}
export const router=localizedRouter(actual);
export function useRouter(){const value=actualUseRouter();return useMemo(()=>localizedRouter(value),[value]);}
export function Link(props:ComponentProps<typeof ActualLink>){return <ActualLink {...props} href={localeHref(props.href)}/>;}
export function Redirect(props:ComponentProps<typeof ActualRedirect>){return <ActualRedirect {...props} href={localeHref(props.href)}/>;}
export function LegacyLocaleRoute({children}:{children:ReactNode}){
 const path=usePathname(),params=useGlobalSearchParams();
 if(Platform.OS!=='web')return children;
 // Browser location preserves duplicate queries and fragments exactly; native unchanged.
 const href=typeof window!=='undefined'?localizedURL(window.location.pathname+window.location.search+window.location.hash,'en'):{pathname:localizedURL(path,'en'),params};
 return <ActualRedirect href={href as Href}/>;
}
