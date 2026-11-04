import { notFound } from "next/navigation";
import { VideoPortal } from "@/components/VideoPortal";
import { isLocale } from "@/lib/i18n";

const SHARE_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

export async function generateMetadata({ params }: { params: Promise<{ locale: string; share_id: string }> }) {
  const { locale, share_id } = await params;
  if (!isLocale(locale) || !SHARE_ID.test(share_id)) notFound();
  return { title: "Framevela", robots: { index: false, follow: false }, referrer: "no-referrer" as const };
}

export const dynamic = "force-dynamic";

export default async function VideoSharePage({ params }: { params: Promise<{ locale: string; share_id: string }> }) {
  const { locale, share_id: shareID } = await params;
  if (!SHARE_ID.test(shareID) || !isLocale(locale)) notFound();
  // Identifiers only; the delivery itself is fetched by the client with the portal session.
  return <VideoPortal locale={locale} shareID={shareID} />;
}
