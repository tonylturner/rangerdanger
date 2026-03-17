"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const navLinks = [
  { href: "/", label: "Overview" },
  { href: "/scenarios", label: "Exercises" },
  { href: "/substation", label: "Feeder HMI" },
  { href: "/console", label: "Network Map" },
  { href: "/labs", label: "Lab Instances" },
];

export function NavSidebar() {
  const pathname = usePathname();

  return (
    <aside className="hidden w-48 flex-col border-r border-slate-900 bg-slate-950/70 px-3 py-5 md:flex">
      <Link href="/" className="mb-5 px-3">
        <span className="text-sm font-bold text-white tracking-tight">RangerDanger</span>
        <span className="block text-[9px] uppercase tracking-widest text-slate-600 mt-0.5">Substation Lab</span>
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
