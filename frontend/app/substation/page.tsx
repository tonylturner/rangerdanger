"use client";

import { ControlCentreView } from "../../components/control-centre-view";
import { OperatorViewSwitch } from "../../components/operator-view-switch";
import { UsSubstationView } from "../../components/us-substation-view";
import { useOperatorView } from "../../lib/operator-queries";

// The operator screen of the active package (lib/operator-view.ts).
export default function SubstationPage() {
  return (
    <OperatorViewSwitch
      resolution={useOperatorView()}
      views={{
        "us-dnp3-substation": () => <UsSubstationView />,
        "eu-iec104": () => <ControlCentreView />,
      }}
    />
  );
}
