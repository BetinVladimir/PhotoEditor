import type { Metadata } from "next";
import "./global.css";

export const metadata: Metadata = {
  title: "PhotoEditor Gallery",
  robots: { index: false, follow: false }
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <html suppressHydrationWarning><body>{children}</body></html>;
}
