"""Ether tunnel client for Python sidecars (standard library only).

A sidecar connects OUT to Ether over one websocket, so it needs no
inbound ports and works behind NAT. Ether then sends HTTP-shaped
requests (SPEC section 5.1.2) multiplexed over that socket; this
module dispatches them to handler functions and can serve byte
ranges of local files for `file` streams.

    from ether_tunnel import Tunnel

    def handle(req):                      # req: Request
        if req.path == "/manifest.json":
            return Response.json(MANIFEST)
        if req.path == "/ether/health":
            return Response.json({"status": "ok"})
        return Response(404, b'{"error":{"code":"not_found"}}')

    tunnel = Tunnel("wss://ether.example/ether/plugins/tunnel",
                    credential="eg1_...", handler=handle,
                    file_root="/media/movies")   # optional
    tunnel.serve_forever()                # reconnects with backoff

The credential is minted when the owner pairs the plugin in the
Ether console (an identity_grants row on surface "plugin"). Keep it
secret; it is sent once, on the upgrade request, never in a frame.

Wire protocol "ether-tunnel-v1": JSON text frames. Bodies are
base64. Requests carry an id; replies may be one "response" frame
or a "response_start"/"response_chunk"/"response_end" stream, which
this module uses automatically for file ranges.
"""

import base64
import hashlib
import json
import os
import socket
import ssl
import struct
import threading
import time
import urllib.parse

_CHUNK = 64 * 1024
_GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"


class Request:
    """One HTTP-shaped request Ether pulled from the plugin."""

    def __init__(self, frame):
        self.method = frame.get("method") or "GET"
        self.path = frame.get("path") or "/"
        self.query = frame.get("query") or {}
        self.headers = frame.get("headers") or {}
        raw = frame.get("body") or ""
        self.body = base64.b64decode(raw) if raw else b""
        self.request_id = frame.get("requestId") or ""
        self.deadline = frame.get("deadline") or ""

    def header(self, name, default=""):
        for key, values in self.headers.items():
            if key.lower() == name.lower() and values:
                return values[0]
        return default

    def json(self):
        return json.loads(self.body or b"null")


class Response:
    """A buffered reply. For files use file_response (streamed)."""

    def __init__(self, status=200, body=b"", headers=None):
        self.status = status
        self.body = body if isinstance(body, bytes) else str(body).encode()
        self.headers = headers or {}

    @staticmethod
    def json(value, status=200):
        return Response(status, json.dumps(value).encode(),
                        {"Content-Type": ["application/json"]})


def _parse_range(header, size):
    """Parse one 'bytes=' range. Returns (start, end, ranged) or raises."""
    if not header:
        return 0, size - 1, False
    if not header.startswith("bytes=") or "," in header or size == 0:
        raise ValueError("unsupported range")
    lo, _, hi = header[6:].partition("-")
    if lo == "" and hi == "":
        raise ValueError("unsatisfiable range")
    if lo == "":
        suffix = int(hi)
        if suffix <= 0:
            raise ValueError("unsatisfiable range")
        suffix = min(suffix, size)
        return size - suffix, size - 1, True
    start = int(lo)
    if start < 0 or start >= size:
        raise ValueError("unsatisfiable range")
    end = size - 1 if hi == "" else min(int(hi), size - 1)
    if end < start:
        raise ValueError("unsatisfiable range")
    return start, end, True


class _WS:
    """A minimal RFC 6455 websocket client (stdlib sockets only)."""

    def __init__(self, url, headers):
        parsed = urllib.parse.urlparse(url)
        secure = parsed.scheme in ("wss", "https")
        host = parsed.hostname
        port = parsed.port or (443 if secure else 80)
        path = parsed.path or "/"
        if parsed.query:
            path += "?" + parsed.query
        sock = socket.create_connection((host, port), timeout=15)
        if secure:
            ctx = ssl.create_default_context()
            sock = ctx.wrap_socket(sock, server_hostname=host)
        self.sock = sock
        self.buf = b""
        key = base64.b64encode(os.urandom(16)).decode()
        lines = [
            f"GET {path} HTTP/1.1",
            f"Host: {host}:{port}" if parsed.port else f"Host: {host}",
            "Upgrade: websocket",
            "Connection: Upgrade",
            f"Sec-WebSocket-Key: {key}",
            "Sec-WebSocket-Version: 13",
            "Sec-WebSocket-Protocol: ether-tunnel-v1",
        ]
        for name, value in headers.items():
            lines.append(f"{name}: {value}")
        sock.sendall(("\r\n".join(lines) + "\r\n\r\n").encode())
        head = self._read_until(b"\r\n\r\n").decode("iso-8859-1")
        first = head.split("\r\n", 1)[0]
        if " 101 " not in first:
            status = first.split(" ", 2)[1] if " " in first else "?"
            raise ConnectionError(f"tunnel refused: HTTP {status} "
                                  f"({'credential rejected' if status == '401' else first})")
        expect = base64.b64encode(hashlib.sha1((key + _GUID).encode()).digest()).decode()
        if expect.lower() not in head.lower():
            raise ConnectionError("tunnel handshake failed: bad accept key")

    def _read_until(self, marker):
        while marker not in self.buf:
            chunk = self.sock.recv(65536)
            if not chunk:
                raise ConnectionError("tunnel closed during handshake")
            self.buf += chunk
        head, self.buf = self.buf.split(marker, 1)
        return head + marker

    def _read_exact(self, n):
        while len(self.buf) < n:
            chunk = self.sock.recv(65536)
            if not chunk:
                raise ConnectionError("tunnel closed")
            self.buf += chunk
        out, self.buf = self.buf[:n], self.buf[n:]
        return out

    def send_text(self, text):
        payload = text.encode()
        mask = os.urandom(4)
        header = bytearray([0x81])  # FIN + text
        if len(payload) < 126:
            header.append(0x80 | len(payload))
        elif len(payload) < 65536:
            header.append(0x80 | 126)
            header += struct.pack("!H", len(payload))
        else:
            header.append(0x80 | 127)
            header += struct.pack("!Q", len(payload))
        masked = bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
        self.sock.sendall(bytes(header) + mask + masked)

    def _send_control(self, opcode, payload=b""):
        mask = os.urandom(4)
        masked = bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
        self.sock.sendall(bytes([0x80 | opcode, 0x80 | len(payload)]) + mask + masked)

    def recv_text(self):
        """Return one complete text message, handling ping/close/fragmentation."""
        message = b""
        while True:
            b0, b1 = self._read_exact(2)
            fin = b0 & 0x80
            opcode = b0 & 0x0F
            length = b1 & 0x7F
            if length == 126:
                length = struct.unpack("!H", self._read_exact(2))[0]
            elif length == 127:
                length = struct.unpack("!Q", self._read_exact(8))[0]
            if b1 & 0x80:  # servers do not mask, but tolerate it
                mask = self._read_exact(4)
                payload = bytes(b ^ mask[i % 4] for i, b in enumerate(self._read_exact(length)))
            else:
                payload = self._read_exact(length)
            if opcode == 0x9:      # ping -> pong
                self._send_control(0xA, payload)
                continue
            if opcode == 0x8:      # close
                raise ConnectionError("tunnel closed by Ether")
            if opcode in (0x1, 0x2, 0x0):
                message += payload
                if fin:
                    return message.decode()

    def close(self):
        try:
            self._send_control(0x8)
        except OSError:
            pass
        try:
            self.sock.close()
        except OSError:
            pass


class Tunnel:
    """Connects a handler to Ether and serves requests until stopped."""

    def __init__(self, url, credential, handler, file_root=None, plugin=None):
        self.url = url
        self.credential = credential
        self.handler = handler
        self.file_root = os.path.realpath(file_root) if file_root else None
        self.plugin = plugin or ""
        self._send_lock = threading.Lock()
        self._stopped = threading.Event()
        self._ws = None

    def stop(self):
        self._stopped.set()
        if self._ws:
            self._ws.close()

    def serve_forever(self):
        backoff = 0.5
        while not self._stopped.is_set():
            started = time.time()
            try:
                self.serve_once()
            except ConnectionError as err:
                if "401" in str(err) or "credential" in str(err):
                    raise  # a rejected credential will not fix itself
            except OSError:
                pass
            if self._stopped.is_set():
                return
            if time.time() - started > 60:
                backoff = 0.5
            self._stopped.wait(backoff)
            backoff = min(backoff * 2, 30)

    def serve_once(self):
        ws = _WS(self.url, {"X-Ether-Plugin-Credential": self.credential})
        self._ws = ws
        try:
            self._send({"type": "hello", "protocol": "ether-tunnel-v1",
                        "plugin": self.plugin, "features": ["range", "stream"]})
            welcome = json.loads(ws.recv_text())
            if welcome.get("type") != "welcome":
                raise ConnectionError("expected a welcome frame")
            chunk = int(welcome.get("maxChunkBytes") or _CHUNK)
            while not self._stopped.is_set():
                frame = json.loads(ws.recv_text())
                if frame.get("type") == "request":
                    thread = threading.Thread(
                        target=self._serve_request, args=(frame, chunk), daemon=True)
                    thread.start()
                # "cancel" frames need no action: a handler that keeps
                # working after its deadline has its reply dropped.
        finally:
            ws.close()
            self._ws = None

    def _send(self, frame):
        with self._send_lock:
            self._ws.send_text(json.dumps(frame))

    def _serve_request(self, frame, chunk):
        req_id = frame.get("id") or ""
        try:
            req = Request(frame)
            if self.file_root and req.path.startswith("/ether/file/"):
                self._serve_file(req, req_id, chunk)
                return
            resp = self.handler(req)
            self._send({"type": "response", "id": req_id,
                        "status": resp.status, "headers": resp.headers,
                        "body": base64.b64encode(resp.body).decode() if resp.body else ""})
        except Exception as err:  # a handler bug must fail one call, not the tunnel
            try:
                self._send({"type": "error", "id": req_id,
                            "error": {"code": "error", "message": str(err)}})
            except Exception:
                pass

    def _serve_file(self, req, req_id, chunk):
        rel = urllib.parse.unquote(req.path[len("/ether/file/"):])
        full = os.path.realpath(os.path.join(self.file_root, rel))
        if full != self.file_root and not full.startswith(self.file_root + os.sep):
            raise ValueError("path escapes the file root")
        if not os.path.isfile(full):
            self._send({"type": "error", "id": req_id,
                        "error": {"code": "not_found", "message": "no such file"}})
            return
        size = os.path.getsize(full)
        try:
            start, end, ranged = _parse_range(req.header("Range"), size)
        except ValueError:
            self._send({"type": "response", "id": req_id, "status": 416,
                        "headers": {"Content-Range": [f"bytes */{size}"]}, "body": ""})
            return
        headers = {"Accept-Ranges": ["bytes"],
                   "Content-Length": [str(end - start + 1)]}
        status = 206 if ranged else 200
        if ranged:
            headers["Content-Range"] = [f"bytes {start}-{end}/{size}"]
        self._send({"type": "response_start", "id": req_id,
                    "status": status, "headers": headers})
        if req.method != "HEAD":
            left = end - start + 1
            with open(full, "rb") as handle:
                handle.seek(start)
                while left > 0:
                    data = handle.read(min(chunk, left))
                    if not data:
                        break
                    left -= len(data)
                    self._send({"type": "response_chunk", "id": req_id,
                                "data": base64.b64encode(data).decode()})
        self._send({"type": "response_end", "id": req_id})
