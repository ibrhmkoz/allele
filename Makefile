.PHONY: ci test lint mutate measurements next-version snapshot

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

# The tag the next release takes, or nothing; see internal/nextversion.
next-version:
	@go run ./internal/nextversion

# The release archives and Homebrew cask, built into dist/, not published.
snapshot:
	goreleaser release --snapshot --clean
