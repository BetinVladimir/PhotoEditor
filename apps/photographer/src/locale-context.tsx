import {createContext,useContext,useEffect,useMemo,useRef,useState,type ReactNode} from 'react';
import AsyncStorage from '@react-native-async-storage/async-storage';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';
export type Locale='en'|'ru'|'bg';
type Key=keyof typeof en;
const catalogs={en,ru,bg};
const Context=createContext<{locale:Locale;setLocale:(locale:Locale)=>void;t:(key:Key)=>string}>({locale:'en',setLocale:()=>{},t:key=>en[key]});
export function LocaleProvider({children}:{children:ReactNode}){
 const chosen=useRef(false);
 const [locale,setValue]=useState<Locale>('en');
 useEffect(()=>{let active=true;void AsyncStorage.getItem('photo.locale').then(value=>{if(active&&!chosen.current&&(value==='en'||value==='ru'||value==='bg'))setValue(value)}).catch(()=>{});return()=>{active=false}},[]);
 const value=useMemo(()=>({locale,setLocale:(next:Locale)=>{chosen.current=true;setValue(next);void AsyncStorage.setItem('photo.locale',next).catch(()=>{})},t:(key:Key)=>catalogs[locale][key]}),[locale]);
 return <Context.Provider value={value}>{children}</Context.Provider>;
}
export const useLocale=()=>useContext(Context);
