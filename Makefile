## pg_timetable project Makefile
##
## Targets:
##   build        build pg_timetable scheduler binary
##   build-pgtt   build pgtt management CLI binary
##   build-all    build both binaries
##   release      build both binaries with version ldflags (uses git tags)
##   test         run all tests
##   lint         run golangci-lint
##   clean        remove built binaries

GO      := go
VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    := $(shell git log -1 --format=%cI 2>/dev/null || echo "unknown")

# Scheduler ldflags (main package vars)
SCHED_LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
# pgtt ldflags (cmd/pgtt/cmd package vars)
PGTT_PKG      := github.com/cybertec-postgresql/pg_timetable/cmd/pgtt/cmd
PGTT_LDFLAGS  := -X $(PGTT_PKG).version=$(VERSION) -X $(PGTT_PKG).commit=$(COMMIT) -X $(PGTT_PKG).date=$(DATE)

.PHONY: build build-pgtt build-all release test lint clean

build:
	$(GO) build -o pg_timetable .

build-pgtt:
	$(GO) build -o pgtt ./cmd/pgtt

build-all: build build-pgtt

release:
	$(GO) build -buildvcs=false -ldflags "$(SCHED_LDFLAGS)" -o pg_timetable .
	$(GO) build -buildvcs=false -ldflags "$(PGTT_LDFLAGS)"  -o pgtt ./cmd/pgtt
	@echo "Built pg_timetable $(VERSION) ($(COMMIT))"
	@echo "Built pgtt         $(VERSION) ($(COMMIT))"

test:
	$(GO) test -failfast -p 1 -timeout=300s ./...

lint:
	golangci-lint run ./...

clean:
	rm -f pg_timetable pgtt
