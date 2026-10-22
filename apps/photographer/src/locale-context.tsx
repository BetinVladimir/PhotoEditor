import {createContext,useContext,useEffect,useMemo,useRef,useState,type ReactNode} from 'react';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {Platform} from 'react-native';
import {usePathname,useNavigationContainerRef,router} from 'expo-router';
import {localizedURL,splitLocaleURL,localeParamAction} from './locale-url';
import {localizeUIMessage,localizeUIValue,formatUIString} from './ui-localization';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';
export type Locale='en'|'ru'|'bg';
type Key=keyof typeof en;
const catalogs={en,ru,bg};
const Context=createContext<{locale:Locale;setLocale:(locale:Locale)=>void;t:(key:Key,params?:Record<string,string|number>)=>string;message:(value:unknown,failure?:boolean)=>string;value:(value:unknown)=>string}>({locale:'en',setLocale:()=>{},t:(key,params)=>formatUIString(en[key],params),message:(value,failure=false)=>localizeUIMessage(value,(key,params)=>formatUIString((en as Record<string,string>)[key],params),en.read_error,failure,[en,ru,bg]),value:value=>localizeUIValue(value,key=>(en as Record<string,string>)[key])});
export function LocaleProvider({children}:{children:ReactNode}){
 const chosen=useRef(false),persistence=useRef<Promise<void>>(Promise.resolve());
 const [preference,setValue]=useState<Locale>('en');const path=usePathname(),navigation=useNavigationContainerRef();
 const locale=Platform.OS==='web'?(splitLocaleURL(path).locale??'en'):preference;
 useEffect(()=>{if(Platform.OS==='web')return;let active=true;void AsyncStorage.getItem('photo.locale').then(value=>{if(active&&!chosen.current&&(value==='en'||value==='ru'||value==='bg'))setValue(value)}).catch(()=>{});return()=>{active=false}},[]);
 useEffect(()=>{if(Platform.OS==='web'&&typeof document!=='undefined')document.documentElement.lang=locale},[locale]);
 const value=useMemo(()=>({locale,setLocale:(next:Locale)=>{chosen.current=true;if(Platform.OS==='web'){
   const current=typeof window!=='undefined'?window.location.pathname+window.location.search+window.location.hash:path;
   if(splitLocaleURL(path).locale){const active=navigation.current;if(!active)throw new Error('LOCALE_NAVIGATION_NOT_READY');active.dispatch(localeParamAction(active.getRootState(),next) as never);}else router.replace(localizedURL(current,next) as never);
  }else setValue(next);
  persistence.current=persistence.current.catch(()=>{}).then(()=>AsyncStorage.setItem('photo.locale',next)).catch(()=>{});
 },message:(input:unknown,failure=false)=>localizeUIMessage(input,(key,params)=>{const text=(catalogs[locale] as Record<string,string>)[key];if(typeof text!=='string')throw new Error('LOCALE_MESSAGE_KEY_MISSING:'+locale+':'+key);return formatUIString(text,params)},catalogs[locale].read_error,failure,[catalogs.en,catalogs.ru,catalogs.bg]),value:(input:unknown)=>localizeUIValue(input,key=>{const text=(catalogs[locale] as Record<string,string>)[key];if(typeof text!=='string')throw new Error('LOCALE_ENUM_KEY_MISSING:'+locale+':'+key);return text}),t:(key:Key,params?:Record<string,string|number>)=>{const text=(catalogs[locale] as Record<string,string>)[key];if(typeof text!=='string')throw new Error('LOCALE_CATALOG_KEY_MISSING:'+locale+':'+key);return formatUIString(text,params)}}),[locale,path,navigation]);
 return <Context.Provider value={value}>{children}</Context.Provider>;
}
export const useLocale=()=>useContext(Context);
