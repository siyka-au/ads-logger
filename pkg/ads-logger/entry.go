package adslogger

import "time"

// LogEntry is a decoded TwinCAT ADS logger message.
type LogEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	Types      []string  `json:"types"` // e.g. "hint", "warning", "error", "log", "msgbox", "resource", "string"
	SenderPort uint32    `json:"sender_port"`
	Sender     string    `json:"sender"`
	Message    string    `json:"message"`
}
