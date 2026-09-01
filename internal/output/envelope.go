package output

import (
	"encoding/json"
	"io"
	"time"
)

const SchemaVersion = "1"

type Error struct {
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	Retryable  bool           `json:"retryable"`
	Suggestion string         `json:"suggestion,omitempty"`
	Context    map[string]any `json:"context,omitempty"`
}

type Warning struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	ID        string `json:"id,omitempty"`
	Retryable bool   `json:"retryable"`
}

type Meta struct {
	Account    string `json:"account,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Truncated  bool   `json:"truncated"`
	Parser     string `json:"parser,omitempty"`
	SearchMode string `json:"search_mode,omitempty"`
	Skipped    int    `json:"skipped,omitempty"`
}

type Envelope struct {
	SchemaVersion string    `json:"schema_version"`
	Command       string    `json:"command"`
	OK            bool      `json:"ok"`
	Data          any       `json:"data"`
	Error         *Error    `json:"error"`
	Warnings      []Warning `json:"warnings"`
	Meta          Meta      `json:"meta"`
}

func Success(command string, data any, started time.Time) Envelope {
	return Envelope{SchemaVersion: SchemaVersion, Command: command, OK: true, Data: data, Warnings: []Warning{}, Meta: Meta{DurationMS: durationMS(started)}}
}

func Failure(command string, failure Error, started time.Time) Envelope {
	return Envelope{SchemaVersion: SchemaVersion, Command: command, OK: false, Data: nil, Error: &failure, Warnings: []Warning{}, Meta: Meta{DurationMS: durationMS(started)}}
}

// durationMS guards against a zero start time (a command that failed before
// its PersistentPreRun ever ran) so meta.duration_ms cannot become garbage.
func durationMS(started time.Time) int64 {
	if started.IsZero() {
		return 0
	}
	return time.Since(started).Milliseconds()
}

func WriteJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(value)
}
