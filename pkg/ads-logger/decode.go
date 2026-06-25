package adslogger

import (
	"encoding/binary"
	"fmt"
	"time"
)

const (
	maskHint     = uint32(0x0001)
	maskWarn     = uint32(0x0002)
	maskError    = uint32(0x0004)
	maskLog      = uint32(0x0010)
	maskMsgbox   = uint32(0x0020)
	maskResource = uint32(0x0040)
	maskString   = uint32(0x0080)
	maskUTF8     = uint32(0x1000)
)

// decode parses an ADS logger notification payload into a LogEntry.
//
// TwinCAT sends variable-length notifications on IG 0x0001 / IO 0xFFFF when the
// subscription uses TransmissionMode=3 (ServerCycle). Each notification carries
// one log entry; the payload length equals the header (36 bytes) plus the actual
// message length plus a null terminator.
//
// Binary layout:
//
//	[0:8]   Windows FILETIME  (uint64 LE, 100ns intervals since 1601-01-01)
//	[8:12]  Message type mask (uint32 LE)
//	[12:16] Sender ADS port   (uint32 LE)
//	[16:32] Sender string     (null-terminated, zero-padded to 16 bytes)
//	[32:36] Message length    (uint32 LE — full length of message that follows)
//	[36:]   Message string    (null-terminated)
func decode(raw []byte) (LogEntry, error) {
	if len(raw) < 16 {
		return LogEntry{}, fmt.Errorf("adslogger: payload too short (%d bytes)", len(raw))
	}

	fileTime := binary.LittleEndian.Uint64(raw[0:8])
	ts := time.Unix(0, (int64(fileTime)-116444736000000000)*100).UTC()

	mask := binary.LittleEndian.Uint32(raw[8:12])
	isUTF8 := mask&maskUTF8 != 0

	types := decodeMask(mask)
	senderPort := binary.LittleEndian.Uint32(raw[12:16])

	sender, senderEnd := decodeString(raw, 16, isUTF8)

	pos := senderEnd + 1
	for pos < len(raw) && raw[pos] == 0 {
		pos++
	}
	pos += 4 // skip message length prefix

	var message string
	if pos < len(raw) {
		message, _ = decodeString(raw, pos, isUTF8)
	}

	return LogEntry{
		Timestamp:  ts,
		Types:      types,
		SenderPort: senderPort,
		Sender:     sender,
		Message:    message,
	}, nil
}

func decodeMask(mask uint32) []string {
	var types []string
	if mask&maskHint != 0 {
		types = append(types, "hint")
	}
	if mask&maskWarn != 0 {
		types = append(types, "warning")
	}
	if mask&maskError != 0 {
		types = append(types, "error")
	}
	if mask&maskLog != 0 {
		types = append(types, "log")
	}
	if mask&maskMsgbox != 0 {
		types = append(types, "msgbox")
	}
	if mask&maskResource != 0 {
		types = append(types, "resource")
	}
	if mask&maskString != 0 {
		types = append(types, "string")
	}
	return types
}

// decodeString reads a null-terminated string from buf starting at pos.
// Returns the string and the index of the null terminator.
func decodeString(buf []byte, pos int, isUTF8 bool) (string, int) {
	end := pos
	for end < len(buf) && buf[end] != 0 {
		end++
	}
	raw := buf[pos:end]
	if isUTF8 {
		return string(raw), end
	}
	// latin-1: each byte value equals its Unicode code point
	runes := make([]rune, len(raw))
	for i, b := range raw {
		runes[i] = rune(b)
	}
	return string(runes), end
}
