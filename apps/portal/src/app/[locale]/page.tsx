import { redirect } from "next/navigation";
import { isLocale, messages } from "@/lib/i18n";

export default async function LocaleHome({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  if (!isLocale(locale)) redirect("/en");
  return <main className="center"><section className="card"><h1>PhotoEditor</h1><p>{messages[locale].openLink}</p></section></main>;
}
