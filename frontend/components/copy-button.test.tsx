// @vitest-environment jsdom
import { fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CopyButton } from "./copy-button";
import { MarkdownProse } from "./markdown-prose";

afterEach(() => vi.unstubAllGlobals());

describe("copy feedback", () => {
  it("copies the exact multiline template and announces success", async () => {
    const template = "Test ID: ___\n  Source: ___\nResult: ___";
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    const tree = render(<MarkdownProse>{`\`\`\`text\n${template}\n\`\`\``}</MarkdownProse>);
    fireEvent.click(tree.getByRole("button", { name: "Copy" }));
    await waitFor(() => expect(tree.getByRole("status").textContent).toBe("Copied"));
    expect(writeText).toHaveBeenCalledWith(template);
  });

  it.each([undefined, { writeText: vi.fn().mockRejectedValue(new Error("Permission denied")) }])("announces unavailable or rejected clipboard access %#", async (clipboard) => {
    vi.stubGlobal("navigator", { clipboard });
    const tree = render(<CopyButton text="command" quiet />);
    fireEvent.click(tree.getByRole("button", { name: "Copy" }));
    await waitFor(() => expect(tree.getByRole("status").textContent).toContain("Copy failed"));
    expect(tree.getByRole("button", { name: "Copy" }).className).toContain("focus:opacity-100");
  });

  it("opens in-text application destinations in a second tab", () => {
    const tree = render(<MarkdownProse>{"[Control Centre](/substation) · [Map](/console) · [Knowledge](/knowledge#capture) · [containd](/containd/)"}</MarkdownProse>);
    for (const link of tree.getAllByRole("link")) {
      expect(link.getAttribute("target")).toBe("_blank");
      expect(link.getAttribute("rel")).toContain("noopener");
    }
  });
});
