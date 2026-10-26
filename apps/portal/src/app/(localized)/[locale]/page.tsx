import type {Metadata} from 'next';
import {notFound} from 'next/navigation';
import {Landing} from '@/components/Landing';
import {isLocale,locales} from '@/lib/i18n';
import {siteMessages} from '@/lib/site-i18n';
export async function generateMetadata({params}:{params:Promise<{locale:string}>}):Promise<Metadata>{const {locale}=await params;if(!isLocale(locale))notFound();const t=siteMessages[locale];const origin=process.env.NEXT_PUBLIC_SITE_ORIGIN??'https://framevela.com';return {title:t.landing_001,description:t.landing_002,robots:{index:true,follow:true},alternates:{canonical:`${origin}/${locale}`,languages:Object.fromEntries([...locales.map(l=>[l,`${origin}/${l}`]),['x-default',`${origin}/ru`]])},icons:{icon:'/favicon.svg'}};}
export default async function Home({params}:{params:Promise<{locale:string}>}){const {locale}=await params;if(!isLocale(locale))notFound();return <Landing locale={locale}/>;}
