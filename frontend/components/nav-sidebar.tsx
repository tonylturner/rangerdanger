"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const navLinks = [
  { href: "/", label: "Overview" },
  { href: "/exercises", label: "Exercises" },
  { href: "/substation", label: "Feeder HMI" },
  { href: "/console", label: "Network Map" },
  { href: "/labs", label: "Workshop Status" },
  { href: "/knowledge", label: "Knowledge" },
];

export function NavSidebar() {
  const pathname = usePathname();

  return (
    <aside className="hidden w-48 flex-col border-r border-slate-900 bg-slate-950/70 px-3 py-5 md:flex">
      <Link href="/" className="mb-5 block px-2">
        {/* Brand lockup. Native <img> rather than next/image because
            the SVG wraps an embedded PNG and we want it to scale
            cleanly without any optimization pass. */}
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src="/rangerdanger-lockup-web.png"
          alt="RangerDanger"
          className="block w-full"
          width={170}
          height={78}
        />
        <span className="mt-1 block px-1 text-[9px] uppercase tracking-widest text-slate-600">
          Substation Lab
        </span>
      </Link>

      <nav className="space-y-0.5 text-[13px]">
        {navLinks.map((link) => {
          const active = link.href === "/" ? pathname === "/" : pathname.startsWith(link.href);
          return (
            <Link
              key={link.href}
              href={link.href}
              className={`block rounded-md px-3 py-1.5 font-medium transition-colors ${
                active
                  ? "bg-sky-950/60 text-sky-400"
                  : "text-slate-500 hover:bg-slate-900 hover:text-slate-200"
              }`}
            >
              {link.label}
            </Link>
          );
        })}
      </nav>

      <div className="mt-auto pt-4 border-t border-slate-900">
        <a
          href="http://localhost:9080"
          target="_blank"
          rel="noopener noreferrer"
          className="block rounded-md px-3 py-1.5 text-[11px] text-slate-600 hover:text-amber-400 transition-colors"
        >
          containd Firewall
        </a>
      </div>
    </aside>
  );
}
