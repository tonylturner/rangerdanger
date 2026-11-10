# Shared offline oracle for the US and EU live smoke gates. The backend's
# compile-response goldens own selections, labels, tuples and verdicts.
def plan_request:
  {packageId, revision, actionIds: [.actions[] | select(.chosen) | .id]};

def plan_expected:
  .expectations as $rows
  | all($rows[]; .planVerdict == .hardenedVerdict) as $accepted
  | {
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
  and .plan.policyHash == $golden.policyHash
  and (.plan | plan_request) == ($golden | plan_request);

# Derive the reference judgment locally from the very same measured rows.
def matrix_reference($golden):
  .rows as $measured
  | [$golden.expectations | to_entries[] | .key as $i | .value + {observed: $measured[$i].observed}] as $rows
  | all($rows[]; .observed == .hardenedVerdict) as $accepted
  | {
      acceptanceRows: [$rows[] | {id, expected: .hardenedVerdict, observed, pass: (.observed == .hardenedVerdict)}],
      acceptancePass: $accepted,
      preventionGaps: [$rows[] | select(.observed == "allow" and .hardenedVerdict == "deny") | .id],
      summary: {
        authorized_total: ([$rows[] | select(.category == "authorized")] | length),
        authorized_pass: ([$rows[] | select(.category == "authorized" and .observed == .hardenedVerdict)] | length),
        unauthorized_total: ([$rows[] | select(.category == "unauthorized")] | length),
        unauthorized_pass: ([$rows[] | select(.category == "unauthorized" and .observed == .hardenedVerdict)] | length),
        skipped: 0, result: (if $accepted then "PASS" else "FAIL" end)
      }
    };

def plan_matrix($golden):
  ($golden.expectations | map(.id)) as $ids
  | .policyStable == true and .policySource == "plan"
    and (.policyHash | type == "string" and test("^[0-9a-f]{64}$"))
    and .policyHash == $golden.policyHash
    and all(.policyChangedAt, .startedAt, .finishedAt; type == "string" and length > 0)
    and ($ids | length > 0 and length == (unique | length))
    and (.rows | type == "array") and [.rows[].id] == $ids
    and all(.rows[]; (.observed == "allow" or .observed == "deny") and .error == "")
    and [.rows[].observed] == [$golden.expectations[].planVerdict]
    and (matrix_reference($golden) == ($golden | plan_expected));
