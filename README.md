# ADS Logger

ADS Logger is a Go library for subscribing to TwinCAT ADS log messages and streaming decoded entries over a channel, built on top of [jarmocluyse/ads-go](https://github.com/jarmocluyse/ads-go); it also ships `adslogdump`, a small CLI that uses the library to dump entries to timestamped JSONL files.