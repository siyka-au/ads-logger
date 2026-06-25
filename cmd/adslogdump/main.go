package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/jarmocluyse/ads-go/pkg/ads"
	adsconstants "github.com/jarmocluyse/ads-go/pkg/ads/constants"
	adslogger "github.com/siyka-au/ads-logger/pkg/ads-logger"
)

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	defaultTargetNetID := envOrDefault("ADS_TARGET_NET_ID", "127.0.0.1.1.1")
	defaultRouterHost := envOrDefault("ADS_ROUTER_HOST", "127.0.0.1")
	defaultRouterPort := adsconstants.ADSDefaultTCPPort
	if v := os.Getenv("ADS_ROUTER_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			defaultRouterPort = p
		}
	}

	targetNetID := flag.String("target-net-id", defaultTargetNetID, "AMS NetID of the target TwinCAT runtime (env: ADS_TARGET_NET_ID)")
	routerHost := flag.String("router-host", defaultRouterHost, "Hostname or IP of the AMS router (env: ADS_ROUTER_HOST)")
	routerPort := flag.Int("router-port", defaultRouterPort, "TCP port of the AMS router (env: ADS_ROUTER_PORT)")
	timeout := flag.Duration("timeout", 2*time.Second, "Connection timeout (env: ADS_TIMEOUT)")
	output := flag.String("o", "", "Output JSONL file (default: adslogdump-<timestamp>.jsonl)")
	debug := flag.Bool("debug", false, "Print raw notification byte count and hex prefix to stderr")
	flag.Parse()

	outPath := *output
	if outPath == "" {
		outPath = fmt.Sprintf("adslogdump-%s.jsonl", time.Now().UTC().Format("20060102T150405Z"))
	}

	f, err := os.Create(outPath)
	if err != nil {
		slog.Error("Failed to create output file", "path", outPath, "error", err)
		os.Exit(1)
	}
	defer f.Close()

	enc := json.NewEncoder(f)

	client := ads.NewClient(ads.ClientSettings{
		TargetNetID: *targetNetID,
		RouterHost:  *routerHost,
		RouterPort:  *routerPort,
		Timeout:     *timeout,
	}, nil)

	if err := client.Connect(); err != nil {
		slog.Error("Failed to connect to ADS router", "error", err)
		os.Exit(1)
	}
	defer client.Disconnect()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	var opts adslogger.Options
	if *debug {
		opts.RawHook = func(raw []byte) {
			preview := raw
			if len(preview) > 64 {
				preview = preview[:64]
			}
			fmt.Fprintf(os.Stderr, "[DEBUG] raw notification: %d bytes | %s\n", len(raw), hex.EncodeToString(preview))
		}
	}

	ch, err := adslogger.Subscribe(ctx, client, opts)
	if err != nil {
		slog.Error("Failed to subscribe to ADS logger", "error", err)
		os.Exit(1)
	}

	slog.Info("adslogdump: capturing", "file", outPath, "target", *targetNetID)

	count := 0
	for entry := range ch {
		fmt.Printf("[%s] [%s] Port %d (%s): %s\n",
			entry.Timestamp.Format("15:04:05.000"),
			strings.Join(entry.Types, ","),
			entry.SenderPort,
			entry.Sender,
			entry.Message,
		)
		if err := enc.Encode(entry); err != nil {
			slog.Warn("Failed to encode entry", "error", err)
		}
		count++
	}

	slog.Info("adslogdump: done", "entries", count, "file", outPath)
}
