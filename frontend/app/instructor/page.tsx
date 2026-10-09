"use client";

import { InstructorView } from "../../components/instructor-view";
import { OperatorViewSwitch } from "../../components/operator-view-switch";
import { useOperatorView } from "../../lib/operator-queries";

// The out-of-band truth page exists for the EU package only.
export default function InstructorPage() {
  return (
    <OperatorViewSwitch
      resolution={useOperatorView()}
      views={{
        "us-dnp3-substation": (pkg) => (
          <main className="mx-auto w-full max-w-6xl px-6 py-10">
            <div className="rounded-lg border border-slate-800 bg-slate-900/60 px-4 py-3 text-sm text-slate-300">
              {pkg.title} has no instructor truth page. It belongs to the EU IEC 104 package.
            </div>
          </main>
        ),
        "eu-iec104": () => <InstructorView />,
      }}
    />
  );
}
