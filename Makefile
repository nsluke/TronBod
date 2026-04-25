.PHONY: build run capture test fmt vet pixlet-serve

build:
	cd sync && go build -o ../bin/sync .

run:
	cd sync && go run .

# One-shot sync: log in, dump raw JSON to data/raw/, exit. No HTTP server.
capture:
	cd sync && go run . -capture

test:
	cd sync && go test ./...

fmt:
	cd sync && gofmt -w .

vet:
	cd sync && go vet ./...

pixlet-serve:
	pixlet serve pixlet/fitbod_stats.star
