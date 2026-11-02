import { readFileSync } from "node:fs";
import { parseJsonEventStream, readUIMessageStream, uiMessageChunkSchema } from "ai";
import { describe, expect, it } from "vitest";

/**
 * Golden decode for Ask Ether (WS4): the fixture is SSE produced by the Go
 * agent encoder (internal/agent) from a fake-model turn. It must decode
 * through the real AI SDK parser that AppKit's AgentPanel uses.
 */
describe("agent stream golden", () => {
  it("decodes Go-produced SSE into UI message parts", async () => {
    const raw = readFileSync("src/test/fixtures/agent-golden.sse", "utf8");
    const body = new Response(raw).body;
    expect(body).not.toBeNull();

    const failures: unknown[] = [];
    const chunks = parseJsonEventStream({
      stream: body as ReadableStream<Uint8Array>,
      schema: uiMessageChunkSchema,
    }).pipeThrough(
      new TransformStream({
        transform(part, controller) {
          if (part.success && part.value) controller.enqueue(part.value);
          else failures.push(part);
        },
      }),
    );

    let lastParts: unknown[] = [];
    for await (const message of readUIMessageStream({ stream: chunks })) {
      lastParts = message.parts as unknown[];
    }
    expect(failures).toEqual([]);

    const simplified = lastParts.map((p) => {
      const part = p as Record<string, unknown>;
      if (part.type === "text") return { type: "text", text: part.text };
      if (typeof part.type === "string" && part.type.startsWith("tool-")) {
        return {
          type: part.type,
          toolCallId: part.toolCallId,
          state: part.state,
          input: part.input,
          output: part.output,
        };
      }
      return { type: part.type };
    });

    expect(simplified).toEqual([
      { type: "step-start" },
      { type: "text", text: "Checking the server. " },
      {
        type: "tool-server_status",
        toolCallId: "call_1",
        state: "output-available",
        input: {},
        output: { healthy: true },
      },
      { type: "step-start" },
      { type: "text", text: "The server is healthy." },
    ]);
  });
});
