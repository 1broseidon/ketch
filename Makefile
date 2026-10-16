BINARY := ketch

.PHONY: build build-check clean test lint install vulncheck

build:
	go build -o $(BINARY) .

build-check:
	go build ./...

install:
	go install .

test:
	go test ./...

lint:
	golangci-lint run

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

clean:
	rm -f $(BINARY)
