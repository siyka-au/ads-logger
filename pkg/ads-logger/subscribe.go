package adslogger

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jarmocluyse/ads-go/pkg/ads"
)

const (
	loggerPort  = uint16(100)
	indexGroup  = uint32(0x0001)
	indexOffset = uint32(0xFFFF)
	bufferSize  = uint32(1024)
	chanBuf     = 64

	// igLoggerConsumer is the ADS index group used to register as a full-message
	// logger consumer (TwinCAT proprietary, observed via Wireshark on port 100).
	// Sending a ReadWrite with this IG and our NetID:Port as write data causes
	// TwinCAT to switch from the fixed 73-byte compact notification format to
	// variable-length notifications that include the full message text.
	igLoggerConsumer = uint32(0x0000F090)

	// keepaliveInterval is how often to re-send the consumer registration.
	// TwinCAT XAE sends this every ~5–10 s; use 5 s to stay well within that window.
	keepaliveInterval = 5 * time.Second
)

// Options configures a Subscribe call.
type Options struct {
	// RawHook, if set, is called with the raw notification bytes before decoding.
	// Useful for debugging truncation: print len(raw) to see how many bytes
	// TwinCAT actually sent.
	RawHook func(raw []byte)

	// Logger receives diagnostics for conditions the caller cannot otherwise
	// observe: decode failures, dropped entries (consumer channel full), and
	// failed consumer-registration keepalives. Defaults to a no-op logger, so
	// this package never writes to a global log target uninvited.
	Logger *slog.Logger
}

// Subscribe registers an ADS notification on the TwinCAT logger port (100) and
// returns a channel that emits decoded log entries. Entries are dropped when the
// channel is full so a slow consumer does not block the ADS notification goroutine.
//
// TwinCAT delivers full-length log messages only when the client is registered
// as a consumer via IG 0x0000F090. Subscribe sends this registration immediately
// after subscribing and repeats it on a keepalive ticker so TwinCAT does not
// time out the registration.
//
// The channel is closed and the subscription is cancelled when ctx is done.
func Subscribe(ctx context.Context, client *ads.Client, opts ...Options) (<-chan LogEntry, error) {
	var opt Options
	if len(opts) > 0 {
		opt = opts[0]
	}
	logger := opt.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	ch := make(chan LogEntry, chanBuf)

	sub, err := client.SubscribeRaw(
		loggerPort, indexGroup, indexOffset, bufferSize,
		func(data ads.SubscriptionData) {
			if opt.RawHook != nil {
				opt.RawHook(data.RawValue)
			}
			entry, err := decode(data.RawValue)
			if err != nil {
				logger.Debug("adslogger: failed to decode notification", "raw_len", len(data.RawValue), "error", err)
				return
			}
			select {
			case ch <- entry:
			default: // drop — consumer is lagging
				logger.Debug("adslogger: dropped log entry, consumer channel full", "chan_cap", chanBuf)
			}
		},
		// Match TwinCAT XAE: ServerCycle (mode 3) with CycleTime=0 means TwinCAT
		// pushes the full log entry immediately on each event rather than polling
		// a fixed-size slot every N ms (ServerOnChange/mode 4 = 73-byte compact format).
		ads.SubscriptionSettings{
			CycleTime:    0,
			SendOnChange: false,
		},
	)
	if err != nil {
		return nil, err
	}

	// Build the 8-byte consumer registration payload: NetID (6 bytes) + Port (2 bytes LE).
	localAddr := client.LocalAmsAddr()
	regPayload, err := encodeAmsAddr(localAddr.NetID, localAddr.Port)
	if err != nil {
		client.Unsubscribe(sub)
		return nil, fmt.Errorf("adslogger: failed to encode local AMS address %q: %w", localAddr.NetID, err)
	}

	// sendConsumerReg sends the IG=0x0000F090 registration to the logger port.
	// TwinCAT responds with 4 bytes (consumer count); we discard the response.
	// A failure here means TwinCAT will silently revert to 73-byte compact
	// notifications instead of full-length messages once the registration lapses.
	sendConsumerReg := func() {
		if _, err := client.ReadWriteRawBinary(loggerPort, igLoggerConsumer, 0x00000000, 4, regPayload); err != nil {
			logger.Warn("adslogger: consumer registration failed, TwinCAT may revert to compact notifications", "error", err)
		}
	}

	// Register immediately so the first notifications already use the full format.
	sendConsumerReg()

	go func() {
		ticker := time.NewTicker(keepaliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				client.Unsubscribe(sub)
				close(ch)
				return
			case <-ticker.C:
				sendConsumerReg()
			}
		}
	}()

	return ch, nil
}

// encodeAmsAddr converts a NetID string ("10.10.20.159.1.1") and port to the
// 8-byte binary representation used in IG 0x0000F090 consumer registration:
// bytes 0–5 = NetID octets, bytes 6–7 = port (little-endian uint16).
func encodeAmsAddr(netID string, port uint16) ([]byte, error) {
	parts := strings.Split(netID, ".")
	if len(parts) != 6 {
		return nil, fmt.Errorf("expected 6 parts, got %d", len(parts))
	}
	buf := make([]byte, 8)
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || v > 255 {
			return nil, fmt.Errorf("invalid octet %q at position %d", p, i)
		}
		buf[i] = byte(v)
	}
	binary.LittleEndian.PutUint16(buf[6:], port)
	return buf, nil
}
