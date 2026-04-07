import "./globals.css";
import { ReactNode } from "react";
import { ClientProviders } from "../components/client-providers";
import { NavSidebar } from "../components/nav-sidebar";

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <body className="min-h-screen bg-slate-950 text-slate-100">
        <ClientProviders>
          <div className="flex min-h-screen">
            <NavSidebar />
            <section className="flex-1">{children}</section>
          </div>
        </ClientProviders>
      </body>
    </html>
  );
}
