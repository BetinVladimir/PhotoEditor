import {redirect} from 'next/navigation';
import {localeQuery} from '../../lib/locale-query';
export default async function Entry({searchParams}:{searchParams:Promise<Record<string,string|string[]|undefined>>}){redirect('/ru'+localeQuery(await searchParams));}
