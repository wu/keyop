// Package webSocketProtocol defines the shared constants and wire types for the
// keyop WebSocket protocol v1.  Both webSocketClient and webSocketServer import
// this package so that constants never drift between implementations.
//
//nolint:revive
package webSocketProtocol

import "time"

// ProtocolVersion is the single supported wire-protocol version.
// Every frame on the wire must carry "v": ProtocolVersion.
const ProtocolVersion = 1

// Timing constants used by both client and server.
const (
	PingInterval     = 30 * time.Second
	PongTimeout      = 45 * time.Second
	HandshakeTimeout = 10 * time.Second
	WelcomeTimeout   = 5 * time.Second
	AckTimeout       = 60 * time.Second
)

// Error codes sent in {"v":1,"type":"error",...} frames.
const (
	CodeUnsupportedVersion = "UNSUPPORTED_VERSION"
	CodeBadHandshake       = "BAD_HANDSHAKE"
)

// UnsupportedVersionMsg returns the human-readable message for a version-mismatch
// error, embedding expectedV and gotV so recipients don't need to format their own.
func UnsupportedVersionMsg(expectedV, gotV int) string {
	return "protocol version mismatch: expected v" +
		itoa(expectedV) + ", got v" + itoa(gotV)
}

// BadHandshakeMsg is the standard message for a BAD_HANDSHAKE error frame.
const BadHandshakeMsg = "expected hello as first frame"

// itoa is a zero-allocation integer-to-string helper for small positive numbers.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
