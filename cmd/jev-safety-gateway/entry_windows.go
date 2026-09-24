//go:build windows

package main

import (
	"context"
	"log"

	"golang.org/x/sys/windows/svc"
)

// serviceName is the SCM name the installer registers. scripts/install-service.ps1
// creates the service with this exact name, and uninstall/stop scripts address
// it by it.
const serviceName = "jev-safety-gateway"

// runMain runs the gateway as an ordinary console process, or hands it to the
// Windows service control manager when the SCM is what started it. One binary
// serves both roles, so `sc.exe create` points straight at
// jev-safety-gateway.exe with no wrapper in between.
func runMain() error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if isService {
		return svc.Run(serviceName, serviceHandler{})
	}
	ctx, stop := consoleContext()
	defer stop()
	return serve(ctx)
}

// serviceHandler maps SCM control requests onto the context the servers already
// watch, so `net stop` gets the same graceful shutdown as Ctrl+C: in-flight
// requests, including SSE streams, are given the servers' full shutdown timeout
// instead of being cut off.
type serviceHandler struct{}

func (serviceHandler) Execute(_ []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown

	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for c := range r {
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				cancel()
				return
			}
		}
	}()

	status <- svc.Status{State: svc.Running, Accepts: accepted}

	err := serve(ctx)

	// StopPending is the window the SCM allows for the graceful shutdown above;
	// serve only returns once both listeners have drained.
	status <- svc.Status{State: svc.StopPending}
	if err != nil {
		log.Printf("service stopped with error: %v", err)
		return true, 1
	}
	return false, 0
}
