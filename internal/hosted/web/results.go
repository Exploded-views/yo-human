package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Exploded-views/yo-human/internal/hosted/bus"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// envelope is the version every result carries: structured content is {"v": 1, ...}, and the
// same JSON is the text content.
type envelope struct {
	V int `json:"v"`
}

func v1() envelope { return envelope{V: 1} }

// errorBody is {"code", "what", "valid", "fix", "winner"?}: what names a field or a rule, never the
// caller's value, and winner rides only on already_decided.
type errorBody struct {
	Code   bus.Code    `json:"code"`
	What   string      `json:"what"`
	Valid  string      `json:"valid"`
	Fix    string      `json:"fix"`
	Winner *recordWire `json:"winner,omitempty"`
}

type errorOut struct {
	envelope
	Error errorBody `json:"error"`
}

// errorResult is a tool result with isError and the v 1 error object, as both kinds of content.
func errorResult(e *bus.Error) *mcp.CallToolResult {
	body := errorBody{Code: e.Code, What: e.What, Valid: e.Valid, Fix: e.Fix}
	if e.Winner != nil {
		w := recordOf(*e.Winner)
		body.Winner = &w
	}
	out := errorOut{v1(), body}
	raw, _ := json.Marshal(out) // cannot fail: strings and a struct of strings
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}, StructuredContent: out}
}

// asBusError is err as a bus error. Anything else is an internal error that says nothing of its cause.
func asBusError(err error) *bus.Error {
	if e, ok := bus.AsError(err); ok {
		return e
	}
	return &bus.Error{Code: bus.CodeInternal, What: "the bus", Valid: "a retry", Fix: "try again; if it keeps failing, tell the owner"}
}

// errDeadline is what a call gets when it outlives its deadline. A write that timed out may still
// have committed, so the fix says to read before repeating one.
var errDeadline = &bus.Error{Code: bus.CodeInternal, What: "the call outlived its deadline", Valid: "a retry",
	Fix: "try again; a write may have committed, so read before repeating a post that has no handle"}

var errPanic = errors.New("web: a tool panicked")

type outcome struct {
	out any
	err error
}

// run calls f as principal p under the call's deadline: the configured tool deadline plus wait,
// the call's own wait (answers). A call that outlives it is cut off here even when the store
// ignores its context, so the caller is never left waiting on a stuck database. An internal error
// is logged with its cause, which no field a caller sees carries.
func (s *service) run(ctx context.Context, req *mcp.CallToolRequest, tool string, wait time.Duration,
	f func(ctx context.Context, p bus.Principal) (any, error)) (*mcp.CallToolResult, any, error) {
	var p bus.Principal
	if req != nil && req.Extra != nil {
		p = principalOf(req.Extra.TokenInfo)
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ToolDeadline+wait)
	defer cancel()
	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- outcome{err: fmt.Errorf("%w: %v", errPanic, r)}
			}
		}()
		out, err := f(ctx, p)
		done <- outcome{out, err}
	}()
	select {
	case o := <-done:
		if o.err != nil {
			return s.fail(tool, o.err), nil, nil
		}
		return nil, o.out, nil
	case <-ctx.Done():
		return s.fail(tool, errDeadline), nil, nil
	}
}

func (s *service) fail(tool string, err error) *mcp.CallToolResult {
	e := asBusError(err)
	if e.Code == bus.CodeInternal && s.cfg.Log != nil {
		cause := e.Cause()
		if cause == nil {
			cause = err
		}
		s.cfg.Log.Error("tool failed", slog.String("tool", tool), slog.Any("cause", cause))
	}
	return errorResult(e)
}

// schemaErrors turns what the SDK says about arguments that fail the input schema (an isError
// result with plain text, which may repeat a value the caller sent) into the v 1 invalid error.
// Every result this server builds itself carries structured content, so one without is the SDK's.
func schemaErrors(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		res, err := next(ctx, method, req)
		if r, ok := res.(*mcp.CallToolResult); ok && err == nil && method == "tools/call" && r != nil && r.IsError && r.StructuredContent == nil {
			return errorResult(&bus.Error{Code: bus.CodeInvalid, What: "arguments", Valid: "the tool's input schema, from tools/list",
				Fix: "send every required argument, no others, each of the right type"}), nil
		}
		return res, err
	}
}
