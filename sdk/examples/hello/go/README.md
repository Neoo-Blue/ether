# Hello plugin (Go)

Run the sidecar from the repository root:

```sh
ETHER_PLUGIN_PORT=8080 go run ./sdk/examples/hello/go/cmd
```

For a tunnel, set `ETHER_TUNNEL_URL` and `ETHER_PLUGIN_CREDENTIAL` instead.
Use a credential obtained by pairing with Ether. The HTTP mode defaults to a
random free port when `ETHER_PLUGIN_PORT` is not set.

Run its tests with `go test ./sdk/examples/hello/go`.
