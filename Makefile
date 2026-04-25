.PHONY: build run capture mock test fmt vet pixlet-serve

build:
	cd sync && go build -o ../bin/sync .

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
