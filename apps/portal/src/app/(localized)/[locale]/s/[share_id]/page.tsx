import { notFound } from "next/navigation";
import { Portal } from "@/components/Portal";
import { isLocale } from "@/lib/i18n";

export async function generateMetadata({params}:{params:Promise<{locale:string;share_id:string}>}){const {locale,share_id}=await params;if(!isLocale(locale)||!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(share_id))notFound();return {title:'Framevela',robots:{index:false,follow:false},referrer:'no-referrer' as const};}

export const dynamic = "force-dynamic";

export default async function SharePage({ params }: { params: Promise<{ locale: string; share_id: string }> }) {
  const { locale, share_id: shareID } = await params;
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(shareID)) notFound();
  if (!isLocale(locale)) notFound();
  // This server-rendered shell contains identifiers only. Protected collection data is fetched by the client.
  return <Portal locale={locale} shareID={shareID} />;
}
