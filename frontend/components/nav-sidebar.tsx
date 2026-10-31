"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useOperatorView } from "../lib/operator-queries";
import { operatorNavLabel, type OperatorViewResolution } from "../lib/operator-view";
import { RangeControl } from "./range-control";

type NavLink = { href: string; label: string };

// The operator screen's label follows the active
// package's operator view (lib/operator-view.ts).
export function navLinksFor(resolution: OperatorViewResolution): NavLink[] {
  const links = [
    { href: "/", label: "Overview" },
    { href: "/exercises", label: "Exercises" },
    { href: "/substation", label: operatorNavLabel(resolution) },
    { href: "/console", label: "Network Map" },
    { href: "/labs", label: "Workshop Status" },
    { href: "/knowledge", label: "Knowledge" },
  ];
  return links;
}

export function NavSidebar() {
  const pathname = usePathname();
  const navLinks = navLinksFor(useOperatorView());

  return (
    <aside className="flex w-full min-w-0 shrink-0 flex-col border-b border-slate-900 bg-slate-950/70 px-3 py-3 md:w-48 md:border-b-0 md:border-r md:py-5">
      <Link href="/" className="mb-5 hidden px-2 md:block">
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

      <nav aria-label="Main navigation" className="flex flex-wrap gap-1 text-[13px] md:block md:space-y-0.5">
        {navLinks.map((link) => {
          const active = link.href === "/" ? pathname === "/" : pathname.startsWith(link.href);
          return (
            <Link
              key={link.href}
              href={link.href}
              aria-current={active ? "page" : undefined}
              className={`block rounded-md px-3 py-2 font-medium transition-colors md:py-1.5 ${
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

      <div className="mt-3 border-t border-slate-900 pt-3 md:mt-auto md:pt-4">
        <RangeControl />
      </div>

      <div className="mt-2 border-t border-slate-900 pt-2 md:mt-4 md:pt-4">
        {/* Same-origin proxy path (nginx rewrites containd's internal */}
        {/* URLs back to /containd/* for iframe-safe navigation), so this */}
        {/* works under any host binding without hardcoding localhost. */}
        <a
          href="/containd/"
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
