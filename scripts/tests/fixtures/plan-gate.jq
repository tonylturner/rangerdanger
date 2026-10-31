# Shared offline oracle for the US and EU live smoke gates. The backend's
# compile-response goldens own selections, labels, tuples and verdicts.
def plan_request:
  {packageId, revision, actionIds: [.actions[] | select(.chosen) | .id]};

def plan_expected:
  .expectations as $rows
  | all($rows[]; .planVerdict == .hardenedVerdict) as $accepted
  | {
      label,
      proofRows: [$rows[] | {
        id, expected: .planVerdict, observed: .planVerdict, pass: true
      }],
      acceptanceRows: [$rows[] | {
        id, expected: .hardenedVerdict, observed: .planVerdict,
        pass: (.planVerdict == .hardenedVerdict)
      }],
      acceptancePass: $accepted,
      preventionGaps: [$rows[]
        | select(.planVerdict == "allow" and .hardenedVerdict == "deny") | .id],
      summary: {
        authorized_total: ([$rows[] | select(.category == "authorized")] | length),
        authorized_pass: ([$rows[] | select(.category == "authorized"
          and .planVerdict == .hardenedVerdict)] | length),
        unauthorized_total: ([$rows[] | select(.category == "unauthorized")] | length),
        unauthorized_pass: ([$rows[] | select(.category == "unauthorized"
          and .planVerdict == .hardenedVerdict)] | length),
        skipped: 0,
        result: (if $accepted then "PASS" else "FAIL" end)
      }
    };

def plan_applied($golden):
  .applied == true and .warnings == [] and .plan.label == $golden.label
  and (.plan | plan_request) == ($golden | plan_request);

def plan_report($golden):
  ($golden | plan_expected) as $want
  | .planProof.applicable == true and .planProof.pass == true
    and .planProof.hashStable == true and .planProof.skipped == 0
    and .planProof.rows == $want.proofRows
    and .acceptance.pass == $want.acceptancePass and .acceptance.skipped == 0
    and .acceptance.rows == $want.acceptanceRows
    and .acceptance.preventionGaps == $want.preventionGaps
    and .summary == $want.summary;
