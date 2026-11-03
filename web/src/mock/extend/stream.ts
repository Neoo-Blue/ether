/**
 * Mock console stream: GET /api/stream server-sent events (WS2).
 *
 * Every connection gets the retry hint and a heartbeat, then a heartbeat
 * every 25 seconds, so mock mode exercises the client's shared connection,
 * reconnect and visibility handling. Dashboard (and other) events never
 * arrive unsolicited: an unsolicited push would replace the mock dashboard
 * that other tests assert on. Tests and the browser console push events
 * explicitly with pushMockStreamEvent.
 */
import type { ExtendHandler } from "./index";

interface Peer {
  send: (text: string) => void;
  done: () => void;
}

const peers = new Set<Peer>();
let nextId = 0;

function frame(id: number, type: string, data: unknown): string {
  return `id: ${id}\nevent: ${type}\ndata: ${JSON.stringify(data)}\n\n`;
}

/**
 * Push one event to every open mock stream, as the server would after a
 * change. Tests use this to drive stream subscribers through renderApp.
 */
export function pushMockStreamEvent(type: string, data: unknown): void {
  nextId += 1;
  const text = frame(nextId, type, data);
  for (const peer of [...peers]) peer.send(text);
}

/** Test helper: drop every open mock stream between cases. */
export function resetMockStream(): void {
  for (const peer of [...peers]) peer.done();
  peers.clear();
}

export function createStreamMock(): { handle: ExtendHandler } {
  return {
    handle: (method, seg) => {
      if (method !== "GET" || seg[0] !== "stream") return null;
      const text = new TextEncoder();
      let timer: ReturnType<typeof setInterval> | null = null;
      let peer: Peer | null = null;
      const stream = new ReadableStream<Uint8Array>({
        start(controller) {
          const send = (s: string) => {
            try {
              controller.enqueue(text.encode(s));
            } catch {
              // The console went away; cancel() cleans up.
            }
          };
          peer = {
            send,
            done: () => {
              if (timer !== null) clearInterval(timer);
              peers.delete(peer as Peer);
              try {
                controller.close();
              } catch {
                // Already closed or cancelled.
              }
            },
          };
          peers.add(peer);
          send("retry: 10000\n\n");
          send(": heartbeat mock\n\n");
          timer = setInterval(() => send(`: heartbeat ${Date.now()}\n\n`), 25_000);
        },
        cancel() {
          if (timer !== null) clearInterval(timer);
          if (peer) peers.delete(peer);
        },
      });
      return new Response(stream, {
        headers: { "content-type": "text/event-stream" },
      });
    },
  };
}
