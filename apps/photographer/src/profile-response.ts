import type {UserProfile} from './catalog-types';

type Preferences=Partial<Pick<UserProfile,'locale'|'theme'|'timezone'>>;
const keys=['id','email','locale','theme','timezone','revision'];
function fail():never{throw Error('PROFILE_RESPONSE_INVALID');}
function preferences(v:Record<string,unknown>):void{
 if(v.locale!==undefined&&!['en','ru','bg'].includes(v.locale as string))fail();
 if(v.theme!==undefined&&!['light','dark','system'].includes(v.theme as string))fail();
 // The server's Go timezone database is authoritative. Browser Intl may omit
 // valid aliases, so it must not replace that database with a narrower list.
 if(v.timezone!==undefined&&(typeof v.timezone!=='string'||!v.timezone||v.timezone==='Local'))fail();
}
export function readUserProfile(raw:unknown):UserProfile{
 if(!raw||typeof raw!=='object'||Array.isArray(raw))fail();const v=raw as Record<string,unknown>;
 if(Object.keys(v).sort().join(',')!==[...keys].sort().join(',')||typeof v.id!=='string'||! /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v.id)||v.id==='00000000-0000-0000-0000-000000000000'||typeof v.email!=='string'||!v.email||typeof v.revision!=='string'||! /^[1-9][0-9]{0,18}$/.test(v.revision)||BigInt(v.revision)>9223372036854775807n||typeof v.locale!=='string'||typeof v.theme!=='string'||typeof v.timezone!=='string')fail();
 preferences(v);return {...v} as UserProfile;
}
export function freezeUserPreferences(raw:Preferences):Preferences{
 if(!raw||typeof raw!=='object'||Array.isArray(raw)||Object.keys(raw).some(k=>!['locale','theme','timezone'].includes(k)))fail();
 preferences(raw as Record<string,unknown>);
 // Explicit undefined has the same wire meaning as an omitted Go pointer.
 return Object.fromEntries(Object.entries(raw).filter(([,value])=>value!==undefined)) as Preferences;
}
export function readUserProfileUpdate(raw:unknown,before:UserProfile,patch:Preferences):UserProfile{
 const value=readUserProfile(raw),expected={...before,...patch};
 if(value.id!==before.id||value.email!==before.email||BigInt(value.revision)!==BigInt(before.revision)+1n||value.locale!==expected.locale||value.theme!==expected.theme||value.timezone!==expected.timezone)throw Error('PROFILE_RECEIPT_INVALID');
 return value;
}
