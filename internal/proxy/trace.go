package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"
)

// TraceHeader carries this request's gateway-wide id back to the client, so a
// report from the client side can be matched to its audit row (and to an
// upstream log line). It is set on every response the gateway produces —
// allowed, blocked or banned.
//
// This is a new header on responses that previously carried only X-JEV-Gateway
// and X-JEV-Score. Adding a response header is backwards compatible for clients
// (unknown headers are ignored), and it is deliberately not added to the request
// forwarded upstream: the gateway's own identity stays on the gateway's side of
// the hop.
const TraceHeader = "X-JEV-Request-Id"

// traceIDBytes is the entropy of a trace id: 6 bytes → 12 hex digits.
const traceIDBytes = 6

// newTraceID returns a fresh request id, "req_" plus 12 hex digits.
//
// The id is always generated here and never taken from the client (an inbound
// X-Request-Id is ignored): the point of the id is that it identifies one request
// *through this gateway*, and a client-supplied value would let a caller mint ids
// that collide with, or forge, other rows' ids. On the vanishing chance that
// crypto/rand fails, this falls back to a nanosecond timestamp so the request
// still gets an id rather than an empty one.
func newTraceID() string {
	var b [traceIDBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req_" + strconv.FormatInt(time.Now().UnixNano()&0xffffffffffff, 16)
	}
	return "req_" + hex.EncodeToString(b[:])
}
