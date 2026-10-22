import en from '../../../../locales/en/common.json';
import ru from '../../../../locales/ru/common.json';
import bg from '../../../../locales/bg/common.json';
import {formatUIString,localizeUIMessage,localizeUIValue} from '../ui-localization';
/** Test-only adapter; behavioral mock locale/state remains the test authority. */
export function withUILocaleDouble(value:{locale?:string;t?:(key:any,...args:any[])=>unknown;[key:string]:unknown}){
 const t=(key:string,params?:Record<string,string|number>)=>{const actual=value.t?.(key,params);const generated=/^(ui_|registration_|login_title$|login_help$)/.test(key);const text=typeof actual==='string'&&(!generated||actual!==key)?actual:generated?(en as Record<string,string>)[key]??String(actual??key):String(actual??key);return formatUIString(text,params)};
 return {...value,t,message:(input:unknown,failure=false)=>localizeUIMessage(input,t,t('read_error'),failure,[en,ru,bg]),value:(input:unknown)=>localizeUIValue(input,t)};
}
