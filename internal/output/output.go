package output

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Response is the standard JSON output envelope.
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Meta    *Meta       `json:"meta,omitempty"`
	Error   *APIError   `json:"error,omitempty"`
}

// Meta contains contextual info about the response.
type Meta struct {
	Account   string `json:"account,omitempty"`
	ElapsedMs int64  `json:"elapsed_ms,omitempty"`
	Cached    bool   `json:"cached,omitempty"`
}

// APIError represents a structured error.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// FormatText renders output as human-readable text.
var FormatText bool

// Success writes a successful JSON response to stdout.
func Success(data interface{}) {
	resp := Response{
		Success: true,
		Data:    data,
	}
	emit(resp)
}

// SuccessWithMeta writes a successful JSON response with metadata.
func SuccessWithMeta(data interface{}, meta *Meta) {
	resp := Response{
		Success: true,
		Data:    data,
		Meta:    meta,
	}
	emit(resp)
}

// Err writes an error JSON response to stderr and exits.
func Err(code, message string) {
	resp := Response{
		Success: false,
		Error: &APIError{
			Code:    code,
			Message: message,
		},
	}
	emit(resp)
	os.Exit(1)
}

// Fatal writes an error and exits (for setup errors before commands run).
func Fatal(code, message string) {
	Err(code, message)
}

func emit(resp Response) {
	if FormatText {
		// Simple text output
		if resp.Success {
			data, _ := json.MarshalIndent(resp.Data, "", "  ")
			fmt.Println(string(data))
		} else {
			fmt.Fprintf(os.Stderr, "ERROR [%s]: %s\n", resp.Error.Code, resp.Error.Message)
		}
		return
	}
	b, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"success":false,"error":{"code":"INTERNAL_ERROR","message":"failed to marshal response"}}`+"\n")
		os.Exit(1)
	}
	fmt.Println(string(b))
}

// Now returns current time as a millisecond timestamp.
func Now() int64 {
	return time.Now().UnixMilli()
}
