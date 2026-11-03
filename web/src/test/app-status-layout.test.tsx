import { render, screen, fireEvent } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { StatusRow, AddressChip, ConnectionStatusRow } from "../components/AppStatusList";
import { SubtabNav } from "../components/layout";

it.each([
  ["reachable", "text-success"],
  ["unreachable", "text-danger"],
  ["unknown", "text-warning"],
])("shows %s with its status icon", (state, color) => {
  const { container } = render(<StatusRow state={state} title="Connection" description={state} />);
  expect(container.querySelector("svg")).toHaveClass(color);
  expect(screen.getByText(state)).toBeVisible();
});
it("renders a link and copies the approve URL", async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  render(<AddressChip address="https://ether.example/ether/me" />);
  expect(screen.getByRole("link")).toHaveAttribute("href", "https://ether.example/ether/me");
  fireEvent.click(screen.getByRole("button", { name: "Copy address" }));
  expect(writeText).toHaveBeenCalledWith("https://ether.example/ether/me");
});
it("restores the scroll container after selecting a tab", () => {
  let frame: FrameRequestCallback | undefined;
  vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
    frame = callback;
    return 1;
  });
  const { container } = render(
    <div className="app-scroll">
      <SubtabNav
        active="one"
        tabs={[
          { key: "one", label: "One" },
          { key: "two", label: "Two" },
        ]}
        onSelect={() => {
          container.firstElementChild!.scrollTop = 0;
        }}
      />
    </div>,
  );
  container.firstElementChild!.scrollTop = 180;
  fireEvent.click(screen.getByRole("tab", { name: "Two" }));
  frame!(0);
  expect(container.firstElementChild!.scrollTop).toBe(180);
  expect((container.firstElementChild as HTMLElement).style.overflowAnchor).toBe("none");
  vi.restoreAllMocks();
});

it("keeps connection details in help and the address in a separate copyable chip", () => {
  render(
    <ConnectionStatusRow
      state="unknown"
      title="At home"
      detail="Detailed explanation"
      address="http://192.168.1.2:8096"
    />,
  );
  expect(screen.getByText("· Not checked yet")).toBeVisible();
  expect(screen.queryByText("Detailed explanation")).not.toBeInTheDocument();
  expect(screen.getByRole("link")).toHaveTextContent("http://192.168.1.2:8096");
  fireEvent.focus(screen.getByRole("button", { name: "More information" }));
  expect(screen.getByRole("tooltip")).toHaveTextContent("Detailed explanation");
});
