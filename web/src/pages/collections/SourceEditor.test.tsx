import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Source } from "../../lib/contract";
import type { SourceDescriptor } from "../../lib/rules";
import { SourceEditor } from "./SourceEditor";

const descriptor: SourceDescriptor = {
  kind: "collectionSource",
  id: "plugin:fixture/picks",
  owner: "fixture",
  label: { resolved: "Picks" },
  meta: {
    editor: "plugin",
    params: [
      { name: "genre", kind: "text", label: { resolved: "Genre" }, required: true },
      { name: "adult", kind: "bool", label: { resolved: "Adult" } },
    ],
  },
};

describe("plugin collection parameters", () => {
  it("edits and persists the params map", () => {
    function Harness() {
      const [source, setSource] = useState<Source>({
        kind: descriptor.id,
        params: { adult: "false" },
      });
      return (
        <>
          <SourceEditor
            source={source}
            onChange={setSource}
            onRemove={() => {}}
            index={0}
            region="US"
            removable={false}
            descriptor={descriptor}
            descriptors={[descriptor]}
          />
          <output>{JSON.stringify(source)}</output>
        </>
      );
    }
    render(<Harness />);
    const box = screen.getByRole("checkbox", { name: "Adult" });
    expect(box).not.toBeChecked();
    fireEvent.change(screen.getByRole("textbox", { name: "Source 1 Genre" }), {
      target: { value: "scifi" },
    });
    fireEvent.click(box);
    expect(JSON.parse(screen.getByText(/"kind":"plugin:/).textContent!)).toEqual({
      kind: descriptor.id,
      params: { genre: "scifi", adult: "true" },
    });
  });
});
