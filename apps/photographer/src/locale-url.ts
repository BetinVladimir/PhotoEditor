export const urlLocales=['en','ru','bg'] as const;
export type URLLocale=typeof urlLocales[number];
export function isURLLocale(value:unknown):value is URLLocale{return typeof value==='string'&&(urlLocales as readonly string[]).includes(value);}
/** Only app-local absolute paths. Never convert external/auth/storage origins. */
export function splitLocaleURL(value:string):{locale:URLLocale|null;path:string;suffix:string}{
 if(!value.startsWith('/')||value.startsWith('//')||/[\\\u0000-\u0020]/.test(value))throw new Error('LOCAL_ROUTE_REQUIRED');
 const at=value.search(/[?#]/),pathname=at<0?value:value.slice(0,at),suffix=at<0?'':value.slice(at);
 const segments=pathname.split('/');
 // Reject traversal before any URL normalisation, including percent-encoded forms.
 for(const segment of segments){let decoded:string;try{decoded=decodeURIComponent(segment)}catch{throw new Error('ROUTE_ENCODING_INVALID')}
  if(decoded==='.'||decoded==='..'||decoded.includes('/')||decoded.includes('\\'))throw new Error('ROUTE_SEGMENT_INVALID');}
 const first=segments[1],locale=isURLLocale(first)?first:null;
 return {locale,path:locale?('/'+segments.slice(2).join('/')||'/'):pathname,suffix};
}
export function localizedURL(value:string,locale:URLLocale):string{
 if(!isURLLocale(locale))throw new Error('LOCALE_INVALID');
 const {path,suffix}=splitLocaleURL(value);return '/'+locale+(path==='/'?'':path)+suffix;
}
/** Locale prefix is presentation only; callers must still run original route ACL. */
export function unlocalizedRoute(value:string):string{return splitLocaleURL(value).path;}
/** Merge one presentation param into the existing route; never replace screen keys. */
export function localeParamAction(state:unknown,locale:URLLocale):{type:'SET_PARAMS';payload:{params:{locale:URLLocale}};source:string;target:string}{
 if(!isURLLocale(locale))throw new Error('LOCALE_INVALID');
 let current=state,insideLocale=false,active:{source:string;target:string}|null=null;
 for(let depth=0;depth<32;depth++){
  if(!current||typeof current!=='object')break;
  const node=current as {key?:unknown;index?:unknown;routes?:unknown};
  if(!Array.isArray(node.routes)||typeof node.key!=='string')break;
  const index=node.index??0;if(!Number.isInteger(index)||typeof index!=='number'||index<0||index>=node.routes.length)break;
  const route=node.routes[index] as {name?:unknown;key?:unknown;state?:unknown};
  if(route?.name==='[locale]')insideLocale=true;
  // Expo inherits dynamic params on the active child. Updating only the parent
  // leaves the child's previous locale shadowing it during URL serialization.
  if(insideLocale&&typeof route?.key==='string')active={source:route.key,target:node.key};
  current=route?.state;
 }
 if(active)return {type:'SET_PARAMS',payload:{params:{locale}},...active};
 throw new Error('ACTIVE_LOCALE_ROUTE_MISSING');
}
