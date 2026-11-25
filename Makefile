.PHONY: ci check lint vet golangci test mutate measurements next-version snapshot

# Make 4 keeps each parallel job's output together; macOS ships 3.81.
SYNC := $(if $(filter 4.% 5.%,$(MAKE_VERSION)),--output-sync=target)
PAR := $(MAKE) --no-print-directory -j $(SYNC)

# The checks run side by side; mutate runs after them, alone: a kill
# counts only if it repeats within time limits, which other jobs' load
# can break.
ci:
	@$(PAR) check
	@$(MAKE) --no-print-directory mutate

check: vet golangci test

lint:
	@$(PAR) vet golangci

vet:
	go vet ./...

golangci:
	golangci-lint run ./...

test:
	XTESTING_COLOR=1 go test ./...

# allele on itself: the lines changed since BASE; fails when a mutant
# survives or no test reaches it.
BASE ?= main
FLAGS ?=
mutate:
	go run ./cmd/allele $(FLAGS) -strict -diff $(BASE) ./...

measurements:
	uv run --with marimo --with altair --with duckdb --with pandas marimo run docs/measurements.py

# The tag the next release takes, or nothing; see internal/nextversion.
next-version:
	@go run ./internal/nextversion

# The release archives and Homebrew cask, built into dist/, not published.
snapshot:
	goreleaser release --snapshot --clean
