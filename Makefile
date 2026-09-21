BINARY := ketch

.PHONY: build build-check clean test lint install

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

clean:
	rm -f $(BINARY)
