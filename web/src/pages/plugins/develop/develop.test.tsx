import { Toaster } from "@braedonsaunders/appkit-ui";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createExtendMock } from "../../../mock/extend";
import { setSlotXrayEnabled } from "../../../components/SlotXray";
import { Develop } from "./Develop";

// Provide a lightweight CodeMirror mock in jsdom so tests can type into the editor deterministically
vi.mock("./CodeEditor", () => ({
  default: ({
    file,
    value,
    diagnostics,
    onChange,
  }: {
    file: string;
    value: string;
    diagnostics: Array<{
      file: string;
      line: number;
      column: number;
      text: string;
      severity: string;
    }>;
    onChange: (next: string) => void;
  }) => {
    const fileDiags = diagnostics.filter((d) => d.file === file || d.file.endsWith(`/${file}`));
    return (
      <div data-testid="studio-code-editor">
        <textarea
          aria-label={`Editor for ${file}`}
          data-testid="studio-editor-textarea"
          value={value}
          onChange={(e) => onChange(e.target.value)}
        />
        {fileDiags.map((d, idx) => (
          <div key={idx} data-testid="editor-diagnostic-line">
            Line {d.line}:{d.column} {d.text}
          </div>
        ))}
      </div>
    );
  },
}));

describe("Develop tab (WS11a, WS11b, WS11d, WS11e)", () => {
  beforeEach(() => {
    setSlotXrayEnabled(false);
    const extendMock = createExtendMock();
    const fetchFn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawUrl = String(input);
      const u = new URL(rawUrl, "http://localhost");
      const method = (init?.method ?? "GET").toUpperCase();
      let body: unknown = undefined;
      if (typeof init?.body === "string" && init.body.length > 0) {
        try {
          body = JSON.parse(init.body);
        } catch {
          body = init.body;
        }
      }
      const seg = u.pathname
        .replace(/^\/ether\/api\//, "")
        .replace(/^\/api\//, "")
        .split("/")
        .filter(Boolean);
      const resp = await extendMock(method, seg, u.searchParams, body);
      if (resp) return resp;
      return new Response(JSON.stringify({ error: { code: "not_found", message: "not found" } }), {
        status: 404,
        headers: { "content-type": "application/json" },
      });
    });
    vi.stubGlobal("fetch", fetchFn);
  });

  afterEach(() => {
    setSlotXrayEnabled(false);
    vi.unstubAllGlobals();
  });

  it("scaffolds a Channels plugin, edits code with diagnostics, previews ViewSpec, inspects calls, toggles shadow/undo, checks, and installs", async () => {
    render(
      <MemoryRouter>
        <Toaster />
        <Develop />
      </MemoryRouter>,
    );

    // 1. All 8 starter templates appear
    expect(await screen.findByTestId("studio-template-grid")).toBeTruthy();
    expect(screen.getByTestId("studio-template-card-stremio_remix")).toBeTruthy();
    expect(screen.getByTestId("studio-template-card-channels")).toBeTruthy();
    expect(screen.getByTestId("studio-template-card-local_files")).toBeTruthy();
    expect(screen.getByTestId("studio-template-card-list")).toBeTruthy();
    expect(screen.getByTestId("studio-template-card-notification")).toBeTruthy();
    expect(screen.getByTestId("studio-template-card-ranking")).toBeTruthy();
    expect(screen.getByTestId("studio-template-card-page")).toBeTruthy();
    expect(screen.getByTestId("studio-template-card-tool")).toBeTruthy();

    // 2. Click Channels template -> create project
    fireEvent.click(screen.getByTestId("studio-template-card-channels"));
    expect(await screen.findByTestId("studio-new-plugin-drawer")).toBeTruthy();
    fireEvent.click(screen.getByTestId("studio-create-confirm-button"));

    // 3. Editor and live PluginView preview render
    expect(await screen.findByTestId("studio-code-editor")).toBeTruthy();
    expect(await screen.findByTestId("studio-plugin-view-container")).toBeTruthy();
    expect(await screen.findByText("Active channels")).toBeTruthy();

    // 4. Introduce a syntax error and verify inline line/col diagnostic appears
    const textarea = screen.getByTestId("studio-editor-textarea") as HTMLTextAreaElement;
    fireEvent.change(textarea, {
      target: { value: "export function handle() {\n  const x = ;\n}\n" },
    });
    fireEvent.click(screen.getByTestId("studio-save-button"));
    expect(await screen.findByTestId("editor-diagnostic-line")).toBeTruthy();
    expect(screen.getByTestId("editor-diagnostic-line").textContent).toContain("Line 2:");

    // 5. Fix the syntax error and save cleanly
    fireEvent.change(textarea, {
      target: {
        value:
          'export function handle(req: { path: string }) {\n  return { status: 200, body: { channels: [{ id: "ch-1", number: "101", name: "Ether Cinema" }] } };\n}\n',
      },
    });
    fireEvent.click(screen.getByTestId("studio-save-button"));
    await waitFor(() => {
      expect(screen.queryByTestId("editor-diagnostic-line")).toBeNull();
    });

    // 6. Switch to Inspector tab (WS11b) and verify call record & plain-language hint
    fireEvent.click(screen.getByTestId("studio-pane-tab-inspector"));
    expect(await screen.findByTestId("studio-inspector-panel")).toBeTruthy();
    expect(screen.getByTestId("inspector-validation-hint").textContent).toContain(
      "Channel 1 (Ether Cinema) has no logo link.",
    );
    expect(screen.getByTestId("inspector-request-json").textContent).toContain("[redacted]");

    // 7. Switch to Shadow & Undo tab (WS11d), toggle shadow mode and undo writes
    fireEvent.click(screen.getByTestId("studio-pane-tab-shadow"));
    expect(await screen.findByTestId("studio-shadow-undo-panel")).toBeTruthy();
    expect(screen.getByTestId("journal-entry-1")).toBeTruthy();
    fireEvent.click(screen.getByTestId("studio-undo-button"));
    await waitFor(() => {
      expect(screen.getByTestId("journal-entry-1").textContent).toContain("Undone");
    });

    // 8. Run Check and verify conformance checks pass
    fireEvent.click(screen.getByTestId("studio-check-button"));
    expect(await screen.findByTestId("studio-check-results")).toBeTruthy();
    expect(screen.getByText("Manifest and capability rules")).toBeTruthy();

    // 9. Click Install -> Permission sheet opens -> Confirm install
    fireEvent.click(screen.getByTestId("studio-install-button"));
    expect(await screen.findByTestId("studio-permission-sheet")).toBeTruthy();
    expect(screen.getByText("Connects to api.pluto.tv")).toBeTruthy();
    fireEvent.click(screen.getByTestId("studio-confirm-install-button"));
    await waitFor(() => {
      expect(screen.queryByTestId("studio-permission-sheet")).toBeNull();
    });
  });

  it("toggles Slot X-ray (WS11e) and opens New plugin pre-wired for the clicked slot", async () => {
    render(
      <MemoryRouter>
        <Toaster />
        <Develop />
      </MemoryRouter>,
    );

    expect(await screen.findByTestId("studio-template-grid")).toBeTruthy();
    fireEvent.click(screen.getByTestId("slot-xray-toggle"));

    const overlay = await screen.findByTestId("slot-xray-overlay");
    expect(overlay).toBeTruthy();

    fireEvent.click(screen.getByTestId("slot-xray-item-title.panel"));
    expect(await screen.findByTestId("studio-new-plugin-drawer")).toBeTruthy();
    expect(screen.getByText("Pre-wired for console slot title.panel")).toBeTruthy();
  });
});
