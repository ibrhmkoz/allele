.PHONY: ci test lint mutate measurements

ci: lint test mutate

test:
	XTESTING_COLOR=1 go test ./...

lint:
	go vet ./...
	golangci-lint run ./...

# allele on itself: the lines changed since BASE; fails when a mutant
# survives or no test reaches it.
BASE ?= main
FLAGS ?=
mutate:
	go run ./cmd/allele $(FLAGS) -strict -diff $(BASE) ./...

measurements:
	uv run marimo run docs/measurements.py
