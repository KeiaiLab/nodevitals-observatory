.PHONY: all fmt vet test build clean

all: fmt vet test

fmt:
	gofmt -l -w .

vet:
	go vet ./...

test:
	go test ./... -race

bench:
	go test ./internal/tsdb/ -bench=. -benchmem -run=^$$

clean:
	rm -rf dist/
