declare module "node:http" {
  export interface IncomingMessage extends AsyncIterable<Uint8Array> {
    method?: string;
    url?: string;
    headers: Record<string, string | string[] | undefined>;
  }

  export interface ServerResponse {
    writeHead(statusCode: number, headers?: Record<string, string>): this;
    end(data?: Uint8Array | string): this;
  }

  export interface AddressInfo {
    port: number;
    address: string;
    family: string;
  }

  export interface Server {
    once(event: "error", listener: (err: Error) => void): this;
    removeListener(event: "error", listener: (err: Error) => void): this;
    listen(port?: number, host?: string, listeningListener?: () => void): this;
    address(): AddressInfo | string | null;
    close(callback?: (err?: Error) => void): this;
  }

  export function createServer(
    requestListener: (req: IncomingMessage, res: ServerResponse) => void | Promise<void>,
  ): Server;

  const http: {
    createServer: typeof createServer;
  };
  export default http;
}

declare module "node:test" {
  export type TestFn = (
    name: string,
    fn: () => void | Promise<void>,
  ) => Promise<void>;
  const test: TestFn;
  export default test;
}

declare module "node:assert/strict" {
  export function equal<T>(actual: T, expected: T, message?: string): void;
  export function notEqual<T>(actual: T, expected: T, message?: string): void;
  export function deepEqual<T>(actual: T, expected: T, message?: string): void;
  export function ok(value: unknown, message?: string): void;
  export function throws(block: () => unknown, error?: RegExp | Error, message?: string): void;
  const assert: {
    equal: typeof equal;
    notEqual: typeof notEqual;
    deepEqual: typeof deepEqual;
    ok: typeof ok;
    throws: typeof throws;
  };
  export default assert;
}
