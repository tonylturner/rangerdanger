import { describe, expect, it } from "vitest";
import { ApiError } from "./api";
import { errorMessage } from "./utils";

describe("errorMessage", () => {
  it("prints an Error's message once, without its name", () => {
    expect(errorMessage(new Error("Request failed: 500"))).toBe("Request failed: 500");
    expect(errorMessage(new TypeError("Failed to fetch"))).toBe("Failed to fetch");
  });

  it("shows the backend's {error} text of a failed request", () => {
    expect(errorMessage(new ApiError(502, "containd unreachable"))).toBe("502 containd unreachable");
    expect(errorMessage(new ApiError(500, ""))).toBe("Request failed: 500");
  });

  it("stringifies anything else", () => {
    expect(errorMessage("boom")).toBe("boom");
    expect(errorMessage(42)).toBe("42");
  });
});
