import { OperatorStateSchemaError } from "./operator-api";
import { isRangeNotReady } from "./range";

// Schema failures are deterministic: expose them immediately, not as loading
// during retry backoff. Range transitions refetch when ready. Network and other
// transient failures retain the portal's existing three-retry policy.
export function retryQuery(failureCount: number, error: Error): boolean {
  return !(error instanceof OperatorStateSchemaError) && !isRangeNotReady(error) && failureCount < 3;
}
