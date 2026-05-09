<!--
Thanks for contributing to RangerDanger.

Before opening this PR:
  - Read CONTRIBUTING.md for the local-test commands.
  - Confirm CI passes locally (go test ./..., npm run lint, npm run build).
  - If your change is user-visible, add a CHANGELOG.md entry under
    [Unreleased].
-->

## What does this change?

<!-- One or two sentences. Pretend the reviewer hasn't seen the issue. -->

## Why?

<!-- The motivation. If it fixes a bug, link the issue. If it changes
     lab content (an exercise YAML, a simulator), explain the
     pedagogical reason or the gap it fills. -->

## How was this tested?

<!-- Pick the boxes that apply.

If your change touches lab content (exercises, simulators, firewall
configs), please walk through the affected exercise(s) end-to-end in
the running stack - unit tests don't catch playthrough regressions. -->

- [ ] `(cd backend && go test ./...)` clean
- [ ] `(cd services && go test ./...)` clean
- [ ] `(cd dnp3go && go test ./...)` clean
- [ ] `(cd frontend && npm run lint && npm run build)` clean
- [ ] `docker compose config -q` clean
- [ ] Spun up the affected exercise end-to-end in `docker compose up -d --build`
- [ ] Other (describe):

## Anything reviewers should know?

<!-- Anything subtle: a behavior change you noticed but didn't introduce,
     a performance regression you accept, an open question. -->
