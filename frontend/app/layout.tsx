import "./globals.css";
import Link from "next/link";
import { ReactNode } from "react";
import { ClientProviders } from "../components/client-providers";

const navLinks = [
  { href: "/console", label: "Console" },
  { href: "/", label: "Dashboard" },
  { href: "/labs", label: "Labs" },
  { href: "/scenarios", label: "Scenarios" },
  { href: "/topology", label: "Topology" },
  { href: "/metrics", label: "Telemetry" }
];

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <body className="min-h-screen bg-slate-950 text-slate-100">
        <ClientProviders>
          <div className="flex min-h-screen">
            <aside className="hidden w-64 flex-col border-r border-slate-900 bg-slate-950/70 px-6 py-8 md:flex">
              <div className="mb-8 text-lg font-semibold">RangerDanger</div>
              <nav className="space-y-2 text-sm text-slate-400">
                {navLinks.map((link) => (
                  <Link
                    key={link.href}
                    href={link.href}
                    className="block rounded-lg px-3 py-2 font-medium hover:bg-slate-900 hover:text-white"
                  >
                    {link.label}
                  </Link>
                ))}
              </nav>
            </aside>
            <section className="flex-1">{children}</section>
          </div>
        </ClientProviders>
      </body>
    </html>
  );
}
