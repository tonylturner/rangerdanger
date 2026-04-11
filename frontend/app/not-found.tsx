import Link from "next/link";

// Next.js renders this for any unmatched route. Centered raccoon +
// short message + escape hatches back to the most-used pages.
export default function NotFound() {
  return (
    <main className="flex min-h-[calc(100vh-3rem)] flex-col items-center justify-center px-6 py-12">
      <div className="flex flex-col items-center gap-6 text-center">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src="/rook-quarter-turn-wink-bg-web.png"
          alt=""
          className="h-48 w-48 rounded-full"
        />
        <div>
          <div className="text-[10px] font-bold uppercase tracking-[0.3em] text-amber-400">
            404 · Page not found
          </div>
          <h1 className="mt-2 text-3xl font-semibold text-white">
            Rook can&apos;t find that route
          </h1>
          <p className="mt-2 max-w-md text-sm text-slate-400">
            The URL you tried doesn&apos;t match any RangerDanger page. The lab is
            still running — head back to one of the entry points below.
          </p>
        </div>
        <div className="flex flex-wrap items-center justify-center gap-2">
          <Link
            href="/"
            className="rounded-md border border-sky-700 bg-sky-950/40 px-4 py-2 text-sm font-medium text-sky-300 transition-colors hover:bg-sky-900/60"
          >
            Overview
          </Link>
          <Link
            href="/exercises"
            className="rounded-md border border-slate-700 bg-slate-900/60 px-4 py-2 text-sm font-medium text-slate-300 transition-colors hover:bg-slate-800"
          >
            Exercises
          </Link>
          <Link
            href="/console"
            className="rounded-md border border-slate-700 bg-slate-900/60 px-4 py-2 text-sm font-medium text-slate-300 transition-colors hover:bg-slate-800"
          >
            Network Map
          </Link>
        </div>
      </div>
    </main>
  );
}
