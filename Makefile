# ghillie — the customer-side terminal of the ghillie⇄facade protocol.
#
# make build   compile both binaries into ./bin
# make test    run the test suite, including the ledger-119 golden-vector check
# make vet     go vet
# make fmt     gofmt -l (fails if anything is unformatted)
# make check   fmt + vet + test
# make demo    run mockfacade + ghillie end to end (see scripts/demo.sh)
# make dist     build the release matrix into ./dist, version stamped in
# make dist-tar dist, then the five tarballs + SHA256SUMS-assets.txt
# make cores   run ONLY the proven-core mirror tests, with their counts shown
# make opsec   run the OPSEC gate over every client-visible string
# make golden  print how to regenerate the golden vectors from the proven core
# make clean   remove ./bin and demo artefacts

GO      ?= go
BIN     := bin
PKGS    := ./...
OPSEC   ?= $(HOME)/.ghillie/opsec-gate.sh  # estate use: make opsec OPSEC=<your gate script>

# cgo's C toolchain, pinned to the system compiler unless the operator names
# another one.
#
# ~/.alire/bin sits ahead of /usr/bin on the PATH of any seat with the Alire
# GNAT toolchain installed, so a bare `c++` resolves to Alire's g++. That
# compiler ships its own libstdc++ headers but no macOS SDK, so <cstdlib>'s
# `#include_next <stdlib.h>` finds nothing and every cgo package needing C++
# fails to build — here cmd/ghillie-gui, through webview. Since PKGS is ./...,
# that took `make check` red at HEAD for a reason outside this repo's code.
#
# The origin test matters: it overrides only Make's own built-in defaults (cc /
# g++), so `make CXX=... ` and an exported CXX both still win.
ifneq ($(wildcard /usr/bin/clang),)
ifeq ($(origin CC),default)
CC := /usr/bin/clang
endif
ifeq ($(origin CXX),default)
CXX := /usr/bin/clang++
endif
export CC
export CXX
endif

.PHONY: all build test vet fmt check demo cores opsec golden clean dist dist-tar

# THE VERSION IS STAMPED INTO THE BINARY, not written on the tin. Every command
# takes -version, and what it prints is this string, injected at link time. The
# default is whatever git says this tree is, so a binary built from a dirty
# working copy says "-dirty" rather than claiming a tag it does not have; the
# release path passes VERSION=vX.Y.Z explicitly.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# The release matrix: Windows, Mac and Linux, both common architectures where
# they exist. Pure Go, CGO off — the retirement of cmd/ghillie-gui removed the
# last cgo dependency, which is what makes this a one-command matrix.
#
# ⚠ HONEST BOUNDARY: this ships the TERMINAL. The proven Ada deciders it execs
# (delivery_policy_front, cv_disclosure_front, glass_bind_policy_front,
# brief_fill_policy_front, ...) are separate binaries built per-OS by the
# factory toolchain — a platform without its fronts runs fail-closed (fill
# refuses, cvgate refuses, glass refuses to bind), loudly, by design. Voice
# and ears are macOS-only by build tag and refuse honestly elsewhere.
DIST      := dist
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64

# mockfacade ships DELIBERATELY. It is a test double and says so in its own
# banner, but a stranger with three bare binaries and no facade to point them at
# has nothing to run; with it they have a working local demo in three lines, and
# the quickstart is the place that says which is which.
DISTBINS  := ghillie ghillie-post ghillie-wa mockfacade

# AGPL COMPLIANCE IS NOT OPTIONAL FURNITURE: LICENSE travels with the binaries,
# in the same tarball, because that is what the licence a stranger receives the
# program under actually requires. The quickstart is beside it for the same
# reason the licence is — a tarball nobody can start is a tarball nobody reads.
DISTDOCS  := LICENSE NOTICE README-QUICKSTART.md

dist:
	@mkdir -p $(DIST)
	@set -e; for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; out=$(DIST)/$$os-$$arch; rm -rf $$out; mkdir -p $$out; \
	  ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
	  for b in $(DISTBINS); do \
	    echo "  $$os/$$arch  $$b$$ext"; \
	    CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath \
	      -ldflags "-s -w -X main.version=$(VERSION)" \
	      -o $$out/$$b$$ext ./cmd/$$b; \
	  done; \
	  cp -R bundles/locales $$out/locales; \
	  for d in $(DISTDOCS); do cp $$d $$out/$$d; done; \
	done
	@echo "dist: $(words $(PLATFORMS)) platforms × $(words $(DISTBINS)) binaries, version $(VERSION), in $(DIST)/"

# dist-tar packs what dist built and writes the checksum file the release notes
# tell people to verify against. The tarball is FLAT — the binaries sit at its
# root — because that is what v0.1.0 and v0.1.1 shipped and a layout change
# between patch releases breaks every instruction anybody wrote down.
dist-tar: dist
	@set -e; cd $(DIST); rm -f SHA256SUMS-assets.txt; \
	for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; \
	  tar -czf ghillie-$$os-$$arch.tar.gz -C $$os-$$arch .; \
	  echo "  packed ghillie-$$os-$$arch.tar.gz"; \
	done; \
	shasum -a 256 ghillie-*.tar.gz > SHA256SUMS-assets.txt
	@echo "dist-tar: $(words $(PLATFORMS)) tarballs + SHA256SUMS-assets.txt in $(DIST)/"

all: check build


build:
	@mkdir -p $(BIN)
	$(GO) build -o $(BIN)/ghillie ./cmd/ghillie
	$(GO) build -o $(BIN)/mockfacade ./cmd/mockfacade

test:
	$(GO) test $(PKGS)

vet:
	$(GO) vet $(PKGS)

# fmt sweeps the PRODUCT tree. verification/ is excluded deliberately: the .go
# files under it are the verbatim inputs of a recorded verifier run (see
# verification/gobra/EVIDENCE.txt), a separate module outside the build, and
# reformatting evidence after the fact would break its correspondence to the
# run that produced it.
fmt:
	@unformatted=$$(find . -name '*.go' -not -path './verification/*' -exec gofmt -l {} +); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt: these files need formatting:"; echo "$$unformatted"; exit 1; \
	fi
	@echo "gofmt: clean"

check: fmt vet test

demo: build
	@./scripts/demo.sh

# cores runs the exhaustive sweeps that hold each transliterated proven core to
# its Ada postconditions, and prints the combination counts so a reviewer sees
# the coverage rather than a bare "ok".
#   ledger 112 Facade_Command_Pkg      216 combinations
#   ledger 113 Claw_Enrolment_Pkg       32 combinations
#   ledger 115 User_Access_Pkg         128 combinations
#   ledger 119 Facade_Instruction_Codec 31 golden vectors from the proven Encode
#   ledger 120 Poll_Freshness_Pkg       table
#   ledger 122 Turn_State_Pkg           12 combinations
#   ledger 123 Question_Ledger_Pkg      12 combinations
#   Attempt_Bound_Pkg (ledger pending)  132 combinations + 4356 monotonicity
cores:
	$(GO) test -v -run 'Exhaustive|Golden|Theorem|Yields|Answered|Mention|Ignored' \
		./internal/gate ./internal/conduct ./internal/frame | grep -E '^(=== RUN|--- |ok|FAIL|.*exhaustive|.*checking)'

# opsec runs the standing pre-publish gate over every string a CLIENT can see.
# Conduct and disclosure copy is client-visible by definition, so it goes
# through the same gate any public surface does.
opsec:
	@mkdir -p $(BIN)
	@grep -hoE '"[^"]{15,}"' internal/interview/*.go internal/credit/*.go internal/brief/*.go \
		| tr -d '"' > $(BIN)/client-visible-strings.txt
	@wc -l < $(BIN)/client-visible-strings.txt | xargs echo "client-visible strings:"
	@if [ -x "$(OPSEC)" ] || [ -f "$(OPSEC)" ]; then bash $(OPSEC) $(BIN)/client-visible-strings.txt; \
	else echo "opsec gate not present here ($(OPSEC)) — estate tooling; the string list above is still yours to read"; fi

# The golden vectors are NOT regenerated by the build: they are the output of
# building and running the proven Ada core, which lives in a READ-ONLY tree and
# is copied out to scratch first. internal/frame/testdata/README.md records the
# exact procedure, and the generator is committed beside it.
golden:
	@echo "Golden vectors come from the proven Ada cores. Two tables, two cores:"
	@echo
	@echo "  ledger 119  Facade_Instruction_Codec  internal/frame/testdata/"
	@echo "  ledger 112  Facade_Command            internal/gate/testdata/   (EXHAUSTIVE, 216 cases)"
	@echo
	@echo "Procedure + source hash: the README.md beside each table."
	@echo "Generators are committed beside them (gen_vectors.adb)."
	@echo
	@echo "~/dev/ada-factory is READ ONLY — copy the .ads out to scratch, never build in it."
	@echo "If a core is re-forged, regenerate its table and update the hash IN THE SAME COMMIT:"
	@echo "a stale table that still passes certifies the mirror against a core that no longer exists."

clean:
	rm -rf $(BIN) .demo
