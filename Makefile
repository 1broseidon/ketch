BINARY := ketch

.PHONY: build build-check clean test lint install bench bench-check bench-live bench-archive

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

# Reproducible corpus tarball for bench/archive.json (publish it, then record its sha256 there).
bench-archive:
	mkdir -p bench/.runs
	cd bench/testdata && tar --sort=name --owner=0 --group=0 --numeric-owner --mtime='2026-09-19 00:00Z' -czf ../.runs/corpus-500.tar.gz $$(ls | sort)
	sha256sum bench/.runs/corpus-500.tar.gz

clean:
	rm -f $(BINARY)
