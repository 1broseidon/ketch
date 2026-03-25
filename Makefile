BINARY := ketch

.PHONY: build clean test lint install

build:
	go build -o $(BINARY) .

install:
	go install .

test:
	go test ./...

lint:
	golangci-lint run

clean:
	rm -f $(BINARY)
