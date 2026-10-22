import ru from '../catalogs/ru.json';
import en from '../catalogs/en.json';
import bg from '../catalogs/bg.json';
import type {Locale} from './i18n';
export const siteMessages = {ru,en,bg};
export const languageNames={ru:ru.landing_050,bg:bg.landing_052,en:en.landing_051} as const;
export function statusLabel(locale:Locale,status:string){
 const t=siteMessages[locale];
 const labels:Record<string,string>={queued:t.status_queued,preparing:t.status_preparing,building:t.status_building,uploading:t.status_uploading,ready:t.status_ready,expired:t.status_expired,failed:t.status_failed,cancelling:t.status_cancelling,cancelled:t.status_cancelled};
 return Object.hasOwn(labels,status)?labels[status]:t.status_unknown;
}
