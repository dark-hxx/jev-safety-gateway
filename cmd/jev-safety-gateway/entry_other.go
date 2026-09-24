//go:build !windows

package main

// runMain runs the gateway as an ordinary foreground process. Linux bare metal
// (via the systemd unit in deploy/systemd) and Docker both take this path.
func runMain() error {
	ctx, stop := consoleContext()
	defer stop()
	return serve(ctx)
}
