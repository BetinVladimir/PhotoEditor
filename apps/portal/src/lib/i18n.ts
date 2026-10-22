import en from '../catalogs/gallery.en.json';
import ru from '../catalogs/gallery.ru.json';
import bg from '../catalogs/gallery.bg.json';
export const locales = ['ru','bg','en'] as const;
export type Locale = typeof locales[number];
export const isLocale = (value:string):value is Locale => locales.includes(value as Locale);
export const messages = {en,ru,bg} as const;
