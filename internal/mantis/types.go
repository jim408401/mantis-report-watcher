// Package mantis talks to a MantisBT server through its official web
// service APIs (SOAP "MantisConnect" and the REST API). No HTML scraping.
package mantis

import (
	"errors"
	"fmt"
	"time"
)

// Ref is an enum-like reference (status, priority, severity, project...).
type Ref struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`  // REST: English enum name. SOAP: localized label.
	Label string `json:"label"` // Localized label (REST); same as Name for SOAP.
	Color string `json:"color,omitempty"`
}

// User is a Mantis account.
type User struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	RealName string `json:"realName,omitempty"`
	Email    string `json:"email,omitempty"`
}

// Display returns the best human readable name.
func (u User) Display() string {
	if u.RealName != "" {
		return u.RealName
	}
	return u.Name
}

// Note is an issue note (comment).
type Note struct {
	ID       int       `json:"id"`
	Reporter User      `json:"reporter"`
	Text     string    `json:"text"`
	Private  bool      `json:"private"`
	Created  time.Time `json:"created"`
	Modified time.Time `json:"modified"`
}

// Issue is the subset of Mantis issue data the app uses.
type Issue struct {
	ID             int       `json:"id"`
	Summary        string    `json:"summary"`
	Project        Ref       `json:"project"`
	Category       string    `json:"category"`
	Status         Ref       `json:"status"`
	Priority       Ref       `json:"priority"`
	Severity       Ref       `json:"severity"`
	Resolution     Ref       `json:"resolution"`
	Reporter       User      `json:"reporter"`
	Handler        User      `json:"handler"`
	Version        string    `json:"version,omitempty"`
	TargetVersion  string    `json:"targetVersion,omitempty"`
	FixedInVersion string    `json:"fixedInVersion,omitempty"`
	Description    string    `json:"description,omitempty"`
	Steps          string    `json:"steps,omitempty"`
	AdditionalInfo string    `json:"additionalInfo,omitempty"`
	Created        time.Time `json:"created"`
	Updated        time.Time `json:"updated"`
	DueDate        time.Time `json:"dueDate,omitempty"`
	Notes          []Note    `json:"notes,omitempty"`
	Tags           []string  `json:"tags,omitempty"`
}

// Filter is a saved Mantis filter.
type Filter struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ProjectID int    `json:"projectId"`
	Public    bool   `json:"public"`
}

// Standard scopes (in addition to numeric saved filter IDs).
const (
	ScopeAssigned  = "assigned"
	ScopeReported  = "reported"
	ScopeMonitored = "monitored"
)

// ErrorKind classifies failures so the UI can show a helpful message.
type ErrorKind string

const (
	KindAuth        ErrorKind = "auth"        // wrong username/password/token
	KindNetwork     ErrorKind = "network"     // cannot reach server
	KindTLS         ErrorKind = "tls"         // certificate problem
	KindNotMantis   ErrorKind = "notMantis"   // URL does not look like Mantis
	KindUnavailable ErrorKind = "unavailable" // this API flavour is disabled
	KindServer      ErrorKind = "server"      // other server-side error
)

// Error is returned by all client operations.
type Error struct {
	Kind ErrorKind
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Err)
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

// KindOf returns the ErrorKind of err (KindServer when unknown).
func KindOf(err error) ErrorKind {
	var me *Error
	if errors.As(err, &me) {
		return me.Kind
	}
	return KindServer
}

func newErr(kind ErrorKind, msg string, err error) *Error {
	return &Error{Kind: kind, Msg: msg, Err: err}
}
