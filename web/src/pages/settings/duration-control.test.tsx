import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { SchemaField } from "./SchemaForm";
it("keeps the selected unit while editing and serializes Go durations", async () => {
  function Harness() {
    const [value, setValue] = useState<unknown>("6h0m0s");
    return (
      <>
        <SchemaField
          def={{ key: "duration", kind: "duration", label: "Remember" }}
          path="duration"
          value={value}
          onChange={setValue}
        />
        <output>{String(value)}</output>
      </>
    );
  }
  render(<Harness />);
  expect(screen.getByLabelText("Remember")).toHaveValue(6);
  fireEvent.click(screen.getByRole("button", { name: "Remember unit" }));
  fireEvent.click(await screen.findByRole("option", { name: "Days" }));
  expect(screen.getByRole("status")).toHaveTextContent("144h");
  expect(screen.getByRole("button", { name: "Remember unit" })).toHaveTextContent("Days");
  fireEvent.change(screen.getByLabelText("Remember"), { target: { value: "" } });
  expect(screen.getByRole("status")).toBeEmptyDOMElement();
  expect(screen.getByRole("button", { name: "Remember unit" })).toHaveTextContent("Days");
});
