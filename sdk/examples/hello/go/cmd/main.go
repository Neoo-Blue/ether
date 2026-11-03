// Command hello runs the Go hello plugin as an HTTP or tunnel sidecar.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	hello "github.com/braedonsaunders/ether/sdk/examples/hello/go"
	sdk "github.com/braedonsaunders/ether/sdk/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := hello.NewHelloPlugin().Serve(ctx, sdk.ServeOptions{}); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
