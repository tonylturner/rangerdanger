"use client";

import { EuDashboard } from "../components/eu-overview";
import { OperatorViewSwitch } from "../components/operator-view-switch";
import { UsDashboard } from "../components/us-dashboard";
import { useOperatorView } from "../lib/operator-queries";

// The overview of the active package (lib/operator-view.ts).
export default function DashboardPage() {
  return (
    <OperatorViewSwitch
      resolution={useOperatorView()}
      views={{
        "us-dnp3-substation": () => <UsDashboard />,
        "eu-iec104": (pkg) => <EuDashboard title={pkg.title} />,
      }}
    />
  );
}
