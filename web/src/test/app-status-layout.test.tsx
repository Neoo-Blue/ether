import { render, screen, fireEvent } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { StatusRow, AddressChip } from "../components/AppStatusList";
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
    <div>
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
  vi.restoreAllMocks();
});
