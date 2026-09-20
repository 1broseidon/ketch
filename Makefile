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

lint:
	golangci-lint run

bench:
	go run ./bench run $(BENCH_ARGS)

bench-check:
	go run ./bench check $(BENCH_ARGS)

bench-live:
	go run ./bench live $(BENCH_ARGS)

clean:
	rm -f $(BINARY)
