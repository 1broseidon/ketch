BINARY := ketch

.PHONY: build build-check clean test lint install bench bench-check bench-live

build:
	go build -o $(BINARY) .

build-check:
	go build ./...

install:
	go install .

test:
	go test ./...
	go -C bench test ./...

lint:
	golangci-lint run

bench:
	go -C bench run . run $(BENCH_ARGS)

bench-check:
	go -C bench run . check $(BENCH_ARGS)

bench-live:
	go -C bench run . live $(BENCH_ARGS)

clean:
	rm -f $(BINARY)
