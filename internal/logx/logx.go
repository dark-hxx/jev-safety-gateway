// Package logx is a tiny leveled logging helper. Debug output is gated by a
// single process-wide flag (set from the JEV_DEBUG env var at startup) so
// verbose per-request tracing can be turned on in development without touching
// the code paths.
package logx

import "log"

// Debug enables Debugf output. Set once at startup from JEV_DEBUG.
var Debug bool

// Debugf logs only when Debug is enabled, prefixed with [debug].
func Debugf(format string, args ...any) {
	if Debug {
		log.Printf("[debug] "+format, args...)
	}
}

// Infof always logs.
func Infof(format string, args ...any) { log.Printf(format, args...) }
