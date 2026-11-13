import { expect, it } from "vitest";
import { coursePresentationProjection } from "./course-parity.test-util";

// Captured before the course cutover. This assertion and its snapshot must not
// change during migration; only the representation accessors may change.
it("preserves every activity's displayed proofs and expanded dynamic steps", () => {
  expect(coursePresentationProjection()).toMatchSnapshot();
});
