package cfprovision

import (
	"fmt"
	"strings"
)

// BlockedError means the run refused to start: nothing was written.
type BlockedError struct{ Reasons []string }

func (e *BlockedError) Error() string {
	return "setup did not start, and changed nothing:\n  - " + strings.Join(e.Reasons, "\n  - ")
}

// PlanChangedError means the account no longer matches the plan the run was given (the
// preview), so the run stopped before the first write.
type PlanChangedError struct{ Changes []string }

func (e *PlanChangedError) Error() string {
	return "the Cloudflare account changed since the preview, so setup changed nothing; run the preview again:\n  - " + strings.Join(e.Changes, "\n  - ")
}

// PermissionError is a request Cloudflare refused because the setup token lacks a
// permission (401 with code 1001, or 403).
type PermissionError struct {
	Permission   string
	Method, Path string
	Cause        string
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("the setup token lacks the %q permission: Cloudflare refused %s %s (%s). Add it to the token and run setup again; it continues where it stopped",
		e.Permission, e.Method, e.Path, e.Cause)
}

// UncertainError is a write whose outcome is unknown: a 5xx, a 429, a timeout or a lost
// connection. The resource may exist. Setup reads the account first on every run and
// matches what exists, so running it again creates no second copy.
type UncertainError struct {
	Step, Method, Path, Cause string
}

func (e *UncertainError) Error() string {
	return fmt.Sprintf("%s: %s %s did not complete (%s); it may or may not have taken effect. Setup stopped here. Run it again: it reads the account first and will not create a second copy",
		e.Step, e.Method, e.Path, e.Cause)
}

// RejectedError is a write Cloudflare answered with a client error other than a
// permission error: the request body or the account state was not what setup expected.
type RejectedError struct {
	Step, Method, Path, Cause string
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("%s: Cloudflare refused %s %s (%s). Setup stopped here; nothing after this step was changed", e.Step, e.Method, e.Path, e.Cause)
}

// VerifyError is a write whose read-back does not match what setup asked for.
type VerifyError struct {
	Step       string
	ResourceID string
	Expected   string
	Got        []string
	// Advice is what the person should do now, when the resource is risky as created.
	Advice string
}

func (e *VerifyError) Error() string {
	id := e.ResourceID
	if id == "" {
		id = "(no ID)"
	}
	msg := fmt.Sprintf("%s: the write succeeded, but reading it back does not match. Resource %s; expected %s; Cloudflare now shows:\n    %s\nSetup stopped here; nothing after this step was changed",
		e.Step, id, e.Expected, strings.Join(e.Got, "\n    "))
	if e.Advice != "" {
		msg += ".\n" + e.Advice
	}
	return msg
}

// SinkError means a secret Cloudflare handed out could not be kept on the host. The run
// stops: the secret is in this process's memory only. Recovery says how to get a new one.
type SinkError struct {
	Secret     SecretName
	ResourceID string
	Cause      string
	Recovery   string
}

func (e *SinkError) Error() string {
	return fmt.Sprintf("could not keep the %s for %s on the host (%s). The secret was never printed and is now lost. %s",
		e.Secret, e.ResourceID, e.Cause, e.Recovery)
}

// ShapeError is a create whose response lacked a field setup depends on.
type ShapeError struct {
	Step, ResourceID, Missing, Recovery string
}

func (e *ShapeError) Error() string {
	return fmt.Sprintf("%s: Cloudflare's response did not include %s (resource %s). Setup stopped here. %s", e.Step, e.Missing, orNone(e.ResourceID), e.Recovery)
}

func orNone(s string) string {
	if s == "" {
		return "(no ID)"
	}
	return s
}
