package lobby

import "fmt"

// Error codes sent to clients.
const (
	CodeBadRequest = "bad_request" // malformed message or parameters
	CodeForbidden  = "forbidden"   // not allowed for this player
	CodeNotFound   = "not_found"   // no such lobby, player, card or pile
	CodeInvalid    = "invalid"     // allowed, but not valid right now
	CodeStale      = "stale"       // based on an old view of the table; refresh and retry
	CodeFull       = "full"        // the lobby has no room
)

// Error is an error a player can be shown.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

func errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}
