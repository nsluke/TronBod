.PHONY: build build-arm64 run capture mock test fmt vet pixlet-serve mitm

build:
	cd sync && go build -o ../bin/sync .

# Cross-compile a static linux/arm64 binary for the Raspberry Pi.
# Output: bin/sync-arm64 — scp to the Pi and run directly (no Docker needed).
build-arm64:
	cd sync && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
	  go build -trimpath -ldflags="-s -w" -o ../bin/sync-arm64 .

run:
	cd sync && DATA_DIR=../data CLASSES_FILE=../classes.yaml go run .

# One-shot sync: log in, dump raw JSON to data/raw/, exit. No HTTP server.
capture:
	cd sync && DATA_DIR=../data CLASSES_FILE=../classes.yaml go run . -capture

# Mock mode — serves stats derived from a fixture Snapshot. Useful for
# testing the LED display path without Fitbod credentials or MITM capture.
mock:
	cd sync && DATA_DIR=../data go run . -mock stats/testdata/sample_snapshot.json

test:
	cd sync && go test ./...

fmt:
	cd sync && gofmt -w .

vet:
	cd sync && go vet ./...

pixlet-serve:
	pixlet serve pixlet/fitbod_stats.star

# Run mitmproxy with the Fitbod schema-probe addon. Install the mitmproxy CA
# on your phone first, then route the phone's traffic through this machine.
mitm:
	mitmdump -s tools/mitm-fitbod.py
