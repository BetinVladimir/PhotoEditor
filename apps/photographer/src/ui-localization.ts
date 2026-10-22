import messageKeys from './ui-message-keys.json';
import patterns from './ui-message-patterns.json';
import enumKeys from './ui-enum-keys.json';
type Translate=(key:string,params?:Record<string,string|number>)=>string;
const escaped=(part:string)=>part.replace(/[.*+?^${}()|[\]\\]/g,'\\$&');
const compiled=patterns.map(item=>({...item,regex:new RegExp('^'+item.parts.map(part=>part===null?'([\\s\\S]*?)':escaped(part)).join('')+'$')})).sort((a,b)=>b.parts.join('').length-a.parts.join('').length);
/** Presentation only: callers still pass original typed values to API/journals. */
export function localizeUIValue(value:unknown,t:Translate):string {
 if(value==null)return '';const source=String(value),key=Object.hasOwn(enumKeys,source)?(enumKeys as Record<string,string>)[source]:undefined;return key?t(key):source;
}
export function localizeUIMessage(value:unknown,t:Translate,fallback:string,failure=false,catalogs:ReadonlyArray<Record<string,string>>=[]):string {
 if(value==null||value==='')return '';
 const source=typeof value==='string'?value:value instanceof Error?value.message:null;
 if(source===null||source.length>8192)return failure?fallback:'';
 const key=Object.hasOwn(messageKeys,source)?(messageKeys as Record<string,string>)[source]:undefined;if(key)return t(key);
 for(const item of compiled){const match=item.regex.exec(source);if(match){const params:Record<string,string>={};for(let i=1;i<match.length;i++)params['p'+(i-1)]=localizeUIValue(match[i],t);return t(item.key,params);}}
 // Legacy code may have stored a previously translated catalog message. Resolve
 // only unambiguous current text, never assume a user-provided name is a key.
 const keys=new Set<string>();for(const catalog of catalogs)for(const [k,text] of Object.entries(catalog))if(text===source)keys.add(k);
 if(keys.size){const choices=new Set([...keys].map(k=>t(k)));if(choices.size===1)return [...choices][0];}
 if(failure)return fallback;
 return localizeUIValue(source,t);
}
export function formatUIString(text:string,params?:Record<string,string|number>):string {
 return params?text.replace(/\{([a-zA-Z][a-zA-Z0-9_]*)\}/g,(match,name)=>Object.hasOwn(params,name)?String(params[name]):match):text;
}
