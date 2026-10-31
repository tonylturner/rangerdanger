"use client";

import { EuWorkshopStatus } from "../../components/eu-overview";
import { OperatorViewSwitch } from "../../components/operator-view-switch";
import { UsWorkshopStatus } from "../../components/us-workshop-status";
import { useOperatorView } from "../../lib/operator-queries";

// The workshop status of the active package (lib/operator-view.ts).
export default function WorkshopPage() {
  return (
    <OperatorViewSwitch
      resolution={useOperatorView()}
      views={{
        "us-dnp3-substation": () => <UsWorkshopStatus />,
        "eu-iec104": (pkg) => <EuWorkshopStatus title={pkg.title} />,
      }}
    />
  );
}
