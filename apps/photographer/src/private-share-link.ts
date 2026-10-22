import {Platform,Share} from 'react-native';
import {linkUUID} from './collection-access-links-api';
/** Called only from an explicit user action. Capability remains in a URL
 * fragment and transient memory; never write it to a journal or telemetry. */
export function privateShareURL(origin:string|undefined,locale:string,id:string,secret:string){
 if(!origin||!['en','ru','bg'].includes(locale)||!linkUUID(id)||!/^[A-Za-z0-9_-]{43}$/.test(secret))throw Error('PRIVATE_LINK_INVALID');
 const base=new URL(origin);if(base.protocol!=='https:'||base.username||base.password||base.search||base.hash||base.pathname!=='/')throw Error('PORTAL_ORIGIN_REQUIRED');
 const url=new URL(`/${locale}/s/${id}`,base);url.hash='k='+secret;return url.toString();
}
export async function sharePrivateURL(url:string,check:()=>void){
 check();if(Platform.OS==='web'&&typeof navigator!=='undefined'&&navigator.clipboard?.writeText){await navigator.clipboard.writeText(url);}else{await Share.share({message:url});}check();
}
