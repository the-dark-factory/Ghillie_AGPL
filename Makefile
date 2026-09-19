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
# make sign     codesign the darwin binaries in ./dist (Developer ID, hardened)
# make notarize sign, then submit each darwin arch to Apple and require Accepted
# make release  dist → notarize → tarballs: THE PUBLISHABLE PATH, in that order
# make publish  the guarded final mile: push, public sync, GitHub release
# make publish-check  publish's gate alone — a dry look at whether it would run
# make locale-demo  prove a shipped locale pack renders, from a tarball layout
# make cores   run ONLY the proven-core mirror tests, with their counts shown
# make admission-front  build the install-admission decider from the KingKlaw
#              source into ./bin (needs GNAT; installs are GATED on it)
# make opsec   run the OPSEC gate over every client-visible string
# make golden  print how to regenerate the golden vectors from the proven core
# make clean   remove ./bin and demo artefacts

GO      ?= go
BIN     := bin
PKGS    := ./...
# estate use: make opsec OPSEC=<your gate script>
#
# THE COMMENT SITS ON ITS OWN LINE ON PURPOSE. Make strips a trailing `#`
# comment from a variable definition but KEEPS THE SPACES BEFORE IT, so the
# old one-line form defined OPSEC as the path PLUS TWO TRAILING SPACES, and
# `[ -f "$(OPSEC)" ]` then tested a path that can never exist. The gate
# reported "not present here" and exited 0 even when it was installed at
# exactly the documented path: a gate whose absence read as a pass.
OPSEC   ?= $(HOME)/.ghillie/opsec-gate.sh

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

.PHONY: all build test vet fmt check demo cores admission-front opsec golden clean unguarded dist dist-tar tar-only sign notarize release publish publish-check locale-demo catalogue-check docs-check

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
DISTBINS  := ghillie ghillie-post ghillie-wa ghillie-slack ghillie-telegram ghillie-discord mockfacade

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

# NO MACOS CRUFT IN THE TARBALLS (v0.1.3). A tarball packed on a Mac with the
# defaults carries two kinds of passenger a stranger on Linux should never have
# to interpret: AppleDouble `._name` files (the resource fork, written as a
# sidecar because tar has nowhere else to put it) and SCHILY.xattr headers for
# extended attributes — on Sequoia every freshly built binary carries
# com.apple.provenance, so that is now every file. GNU tar prints a warning per
# header; the walkthroughs counted thirteen, mid-verification, immediately after
# the step that says "anything but OK means stop".
#
#   COPYFILE_DISABLE=1   kills the AppleDouble sidecars (libarchive's copyfile)
#   --no-mac-metadata    the same, said again in a flag, plus ACLs
#   --no-xattrs          kills the SCHILY.xattr headers
#
# GNU tar has neither flag and produces neither passenger, so the flags are
# probed rather than assumed: this stays a one-command release on Linux.
#
# The guard below checks BOTH symptoms, because they are not the same symptom
# and only one of them is visible in a listing. AppleDouble sidecars show up as
# `._name` entries — that is what the Windows tester saw litter an extraction,
# nine of them. The xattr headers do not appear in any listing at all: they are
# pax keys attached to the real entries, and what they produce is a GNU tar
# warning per file on Linux. The header check looks for LIBARCHIVE.xattr rather
# than SCHILY.xattr for one dull reason worth writing down: Go's own
# archive/tar package carries the literal string "SCHILY.xattr." in its string
# table, so every ghillie binary in the tarball matches it and the check would
# fire on a clean archive forever.
TARCRUFTFLAGS := $(shell tar --no-mac-metadata --no-xattrs --version >/dev/null 2>&1 && echo --no-mac-metadata --no-xattrs)

# dist-tar packs what dist built and writes the checksum file the release notes
# tell people to verify against. The tarball is FLAT — the binaries sit at its
# root — because that is what v0.1.0 and v0.1.1 shipped and a layout change
# between patch releases breaks every instruction anybody wrote down.
#
# ⚠ dist-tar REBUILDS the binaries, which discards any signature on them. The
# publishable path is `make release`, which signs and notarizes BETWEEN the
# build and the packing; tar-only exists so that path can pack without a
# rebuild. Packing by hand after signing means calling tar-only, never dist-tar.
dist-tar: dist tar-only

tar-only:
	@set -e; cd $(DIST); rm -f SHA256SUMS-assets.txt; \
	for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; \
	  COPYFILE_DISABLE=1 tar $(TARCRUFTFLAGS) -czf ghillie-$$os-$$arch.tar.gz -C $$os-$$arch .; \
	  dbl=$$(tar -tzf ghillie-$$os-$$arch.tar.gz | grep -c '\._' || true); \
	  xat=$$(gzip -dc ghillie-$$os-$$arch.tar.gz | strings | grep -c 'LIBARCHIVE\.xattr' || true); \
	  [ "$$dbl" -eq 0 ] && [ "$$xat" -eq 0 ] || { echo "tar-only: ghillie-$$os-$$arch.tar.gz carries macOS cruft ($$dbl AppleDouble entries, $$xat xattr headers) — REFUSING to ship it"; exit 1; }; \
	  echo "  packed ghillie-$$os-$$arch.tar.gz (AppleDouble: $$dbl, xattr headers: $$xat)"; \
	done; \
	shasum -a 256 ghillie-*.tar.gz > SHA256SUMS-assets.txt
	@echo "tar-only: $(words $(PLATFORMS)) tarballs + SHA256SUMS-assets.txt in $(DIST)/"

# ─── SIGNING AND NOTARIZATION ────────────────────────────────────────────────
#
# THE RELEASE PATH IS A TARGET, NOT A MEMORY. From v0.1.3 the macOS binaries
# are signed with the company's Developer ID and notarized by Apple, which is
# what lets a stranger who downloads them in a browser run them without being
# told to strip a quarantine attribute by hand — an instruction that reads,
# correctly, as "disable the safety check", and which we no longer give.
#
# ⚠ BARE MACH-O BINARIES CANNOT BE STAPLED. `xcrun stapler` works on bundles,
# disk images and installer packages; there is nowhere in a bare executable to
# put the ticket. That is not a gap in this: Gatekeeper fetches the ticket from
# Apple on first launch, so a quarantined download assesses as notarized with
# no stapling at all. It does mean first launch wants a network, which the
# release notes say.
#
# ⚠ AND `spctl -a -t exec` IS THE WRONG QUESTION TO ASK ABOUT A CLI TOOL. It
# assesses against the app-execution rules and answers "rejected (the code is
# valid but does not seem to be an app)" — which is true, alarming, and not a
# notarization failure. What it is really saying is that a bare executable is
# not a bundle. The assessment Gatekeeper actually applies to a downloaded file
# is the one below, and on these binaries it answers:
#
#     accepted
#     source=Notarized Developer ID
#     origin=Developer ID Application: The Dark Factory Ltd (85L96KL9LX)
#
# `codesign --verify -R="notarized" --check-notarization` is the second opinion
# and agrees.
#
# Every variable below is the DEFAULT FOR THIS COMPANY'S RELEASES and every one
# of them is overridable: `make notarize SIGN_ID=... SIGN_KEYCHAIN=...
# NOTARY_PROFILE=...`. Anyone rebuilding this source signs with their own
# Developer ID or does not sign at all — there is no secret here, only a path
# to one this machine holds. The identity string itself is public by
# construction; it is stamped into every binary we have ever shipped.
SIGN_ID        ?= Developer ID Application: The Dark Factory Ltd (85L96KL9LX)
SIGN_KEYCHAIN  ?= df-signing.keychain-db
SIGN_KEYPASS   ?= $(HOME)/dev/apple-signing/df-signing.keychain-pass
NOTARY_PROFILE ?= df-notary
DARWIN_ARCHES  := arm64 amd64

# sign expects `dist` to have run. Every darwin binary gets the hardened runtime
# (--options runtime, which notarization requires) and a secure timestamp
# (--timestamp, which is what keeps the signature valid after the certificate
# expires), then is verified before anything is submitted anywhere.
#
# ⚠ THE PARTITION LIST IS NOT OPTIONAL AND IS NOT OBVIOUS. An unlocked keychain
# is only half of what codesign needs: the private key also carries an ACL
# naming which tools may use it without asking a human. A key imported by hand
# has an empty list, so codesign asks — and in any session without a window to
# ask in (a script, a CI runner, an agent) the question cannot be put and the
# answer comes back as `errSecInternalComponent`, which says nothing about
# keys, ACLs or dialogs. This cost an afternoon once; it will not cost another.
# set-key-partition-list writes the list, and must run against an ALREADY
# UNLOCKED keychain, which is why the order below is the order below.
sign:
	@command -v codesign >/dev/null 2>&1 || { echo "sign: no codesign here — signing is macOS-only"; exit 1; }
	@security unlock-keychain -p "$$(cat $(SIGN_KEYPASS))" $(SIGN_KEYCHAIN)
	@security set-key-partition-list -S apple-tool:,apple:,codesign: -s \
	  -k "$$(cat $(SIGN_KEYPASS))" $(SIGN_KEYCHAIN) >/dev/null
	@set -e; for a in $(DARWIN_ARCHES); do \
	  for b in $(DISTBINS); do \
	    codesign --force --options runtime --timestamp -s "$(SIGN_ID)" $(DIST)/darwin-$$a/$$b; \
	    codesign --verify --strict $(DIST)/darwin-$$a/$$b; \
	    echo "  signed darwin-$$a/$$b"; \
	  done; \
	done
	@echo "sign: $(words $(DARWIN_ARCHES)) × $(words $(DISTBINS)) darwin binaries signed and verified"

# notarize submits one zip per darwin arch — the zip is a TRANSPORT for the
# submission and is never published; the tarballs carry the same signed bytes.
# Anything but Accepted fails the target, loudly: a release that assumed
# notarization and did not get it is worse than one that never claimed it.
#
# SUBMIT EVERYTHING FIRST, THEN WAIT. Apple's queue has taken over an hour for a
# single submission from a new team, and `submit --wait` per arch serialises
# those hours end to end for no reason: the second zip could have been queued
# while the first was still being scanned. So every arch is submitted (which
# returns an id at once), the ids are recorded in dist/notary-ids.txt where a
# human can read and quote them, and only then does the target block on
# `notarytool wait`. A rejection prints the notarization log on the spot rather
# than leaving somebody to go and ask for it.
notarize: sign
	@set -e; rm -f $(DIST)/notary-ids.txt; \
	for a in $(DARWIN_ARCHES); do \
	  z=ghillie-darwin-$$a-notarize.zip; \
	  rm -f $(DIST)/$$z; \
	  ( cd $(DIST)/darwin-$$a && zip -q -X ../$$z $(DISTBINS) ); \
	  id=$$(xcrun notarytool submit $(DIST)/$$z --keychain-profile $(NOTARY_PROFILE) \
	        --output-format json 2>/dev/null | sed -n 's/.*"id":"\([^"]*\)".*/\1/p'); \
	  [ -n "$$id" ] || { echo "notarize: darwin-$$a submission returned no id"; exit 1; }; \
	  echo "darwin-$$a $$id" >> $(DIST)/notary-ids.txt; \
	  echo "  submitted darwin-$$a  id $$id"; \
	done
	@set -e; while read -r a id; do \
	  echo "  waiting on $$a ($$id) — Apple's queue, not ours"; \
	  out=$$(xcrun notarytool wait $$id --keychain-profile $(NOTARY_PROFILE) 2>&1); \
	  echo "$$out" | sed 's/^/    /'; \
	  echo "$$out" | grep -q "status: Accepted" || { \
	    echo "notarize: $$a was NOT Accepted — nothing ships"; \
	    xcrun notarytool log $$id --keychain-profile $(NOTARY_PROFILE) 2>&1 | head -40; \
	    exit 1; }; \
	done < $(DIST)/notary-ids.txt
	@set -e; for a in $(DARWIN_ARCHES); do \
	  spctl -a -t open --context context:primary-signature -vv $(DIST)/darwin-$$a/ghillie 2>&1 | sed 's/^/    /'; \
	done
	@echo "notarize: every darwin arch Accepted by Apple — ids in $(DIST)/notary-ids.txt"

# release is the whole publishable path in one command and in the ONE order
# that is correct: build, then sign, then notarize, then pack. Pack before sign
# and the tarballs hold unsigned bytes; rebuild after sign and the signatures
# are gone. The sub-makes keep that order explicit rather than leaving it to
# prerequisite evaluation.
# THE RELEASE PATH RUNS ITS OWN GATES. Until 2026-09-11 it did not: `release`
# went straight to dist -> notarize -> tar-only, and `publish-check` verified
# the tree, the tag and the checksums but never ran a test or a leak scan.
# `make all: check build` existed and was on nobody's path to a release, so a
# tarball could be signed, notarised and published with a failing suite and
# unread client-visible strings.
#
# `opsec` REFUSES when no gate is installed (see its recipe). If you are
# releasing from a machine without the estate gate, say so out loud:
#   make release OPSEC=none
release: check opsec
	$(MAKE) dist
	$(MAKE) notarize
	$(MAKE) tar-only
	@echo "release: signed, notarized, packed — $(DIST)/ is publishable at version $(VERSION)"

# ─── PUBLISHING ──────────────────────────────────────────────────────────────
#
# THE FINAL MILE IS A TARGET, NOT A MEMORY. v0.1.3 and v0.1.4 were published by
# hand: push master and the tag, export the public tree, lay it over
# Ghillie_AGPL, `gh release create` with the notes and the six assets. Each of
# those steps was typed from recollection of the previous release, which is
# exactly how a step gets dropped. From here the whole path is two commands:
#
#     make release VERSION=vX.Y.Z
#     make publish VERSION=vX.Y.Z TITLE='the headline after the version dash'
#
# TITLE is the prose that names the release AND the public sync commit, the way
# v0.1.4 was "give him a mind, and the mind is yours". It has no default
# because a release headline is a decision, not a template.
#
# publish-check is the gate, and runs first — or alone, as a dry look. It
# REFUSES when:
#   - the working tree is dirty: the export ships HEAD, and a dirty tree means
#     HEAD is not what you are looking at;
#   - VERSION is not an existing tag, or HEAD is not at that tag: the public
#     sync archives HEAD (see export-public.sh), so publishing from anywhere
#     else ships source that is not the release's;
#   - the $(DIST) tarballs do not verify against SHA256SUMS-assets.txt, or that
#     file does not list exactly one tarball per platform: stale or hand-mixed
#     artefacts do not ship;
#   - dist/RELEASE-NOTES-$(VERSION).md is missing, or TITLE was not given;
#   - the release already exists on $(PUBLIC_REPO).
#
# ⚠ TAG EXISTENCE IS ASKED WITH show-ref, NOT rev-parse. `git rev-parse
# --verify refs/tags/v0.1.4-1-g27e58bf` SUCCEEDS: anything ending in
# -g<hash> is parsed as describe output and resolved to the hash, prefix and
# all — so the default VERSION of a tree one commit past its tag walks
# straight through a rev-parse gate. show-ref looks the ref up by name and
# parses nothing.
# THE PUBLIC REPO NEVER RECEIVES HISTORY (export-public.sh says why): the sync
# is a fresh export laid over a fresh clone — one commit, tagged. A tag already
# present on the public clone is accepted only when it points at this very
# sync; a public tag never moves. A rerun after a partial failure therefore
# converges instead of doubling: the sync commit comes up empty, the tag
# matches, and only the missing steps run.
PUBLIC_REPO ?= the-dark-factory/Ghillie_AGPL
TITLE       ?=
NOTES        = $(DIST)/RELEASE-NOTES-$(VERSION).md
TARBALLS     = $(foreach p,$(PLATFORMS),$(DIST)/ghillie-$(subst /,-,$(p)).tar.gz)

publish-check:
	@set -e; \
	command -v gh >/dev/null 2>&1 || { echo "publish: no gh here — the release is created with the GitHub CLI"; exit 1; }; \
	[ -z "$$(git status --porcelain)" ] || { echo "publish: the working tree is DIRTY — commit or discard first; a publish ships HEAD, and HEAD is not what is on disk:"; git status --short; exit 1; }; \
	git show-ref --verify --quiet "refs/tags/$(VERSION)" || { echo "publish: '$(VERSION)' is not a tag — tag the release commit, or pass VERSION=vX.Y.Z"; exit 1; }; \
	[ "$$(git rev-parse "$(VERSION)^{commit}")" = "$$(git rev-parse HEAD)" ] || { echo "publish: HEAD is not at $(VERSION) — the export ships HEAD, so check out the tag before publishing"; exit 1; }; \
	[ -f $(DIST)/SHA256SUMS-assets.txt ] || { echo "publish: $(DIST)/SHA256SUMS-assets.txt is missing — run make release first"; exit 1; }; \
	[ "$$(wc -l < $(DIST)/SHA256SUMS-assets.txt | tr -d ' ')" -eq $(words $(PLATFORMS)) ] || { echo "publish: SHA256SUMS-assets.txt does not list exactly $(words $(PLATFORMS)) tarballs — $(DIST)/ is not one release's output"; exit 1; }; \
	( cd $(DIST) && shasum -a 256 -c SHA256SUMS-assets.txt ) || { echo "publish: $(DIST)/ tarballs do NOT match SHA256SUMS-assets.txt — rerun make release; never hand-mix $(DIST)/"; exit 1; }; \
	[ -f "$(NOTES)" ] || { echo "publish: $(NOTES) is missing — the release notes are written before the release is created"; exit 1; }; \
	[ -n "$(TITLE)" ] || { echo "publish: no TITLE — make publish TITLE='the headline after the version dash'"; exit 1; }; \
	! gh release view "$(VERSION)" --repo $(PUBLIC_REPO) >/dev/null 2>&1 || { echo "publish: $(VERSION) already exists on $(PUBLIC_REPO) — a published release is not republished"; exit 1; }; \
	echo "publish-check: clean tree at $(VERSION), $(words $(PLATFORMS)) tarballs verified, notes and TITLE present — publishable"

publish: publish-check
	@set -e; root=$$(pwd); work=$$(mktemp -d); \
	echo "publish: pushing master and $(VERSION) to origin"; \
	git push origin master "$(VERSION)"; \
	echo "publish: exporting the public tree (work dir $$work — kept on failure for reading)"; \
	./scripts/export-public.sh "$$work/export"; \
	gh repo clone $(PUBLIC_REPO) "$$work/public" -- --quiet; \
	find "$$work/public" -mindepth 1 -maxdepth 1 ! -name .git -exec rm -rf {} +; \
	cp -R "$$work/export/." "$$work/public/"; \
	git -C "$$work/public" add -A; \
	if git -C "$$work/public" diff --cached --quiet; then \
	  echo "  public tree already matches this export — no sync commit"; \
	else \
	  git -C "$$work/public" commit -q -m "source sync: $(VERSION) — $(TITLE)"; \
	fi; \
	if git -C "$$work/public" show-ref --verify --quiet "refs/tags/$(VERSION)"; then \
	  [ "$$(git -C "$$work/public" rev-parse "$(VERSION)^{commit}")" = "$$(git -C "$$work/public" rev-parse HEAD)" ] \
	    || { echo "publish: $(PUBLIC_REPO) already carries a DIFFERENT $(VERSION) — a public tag never moves; resolve by hand"; exit 1; }; \
	else \
	  git -C "$$work/public" tag "$(VERSION)"; \
	fi; \
	git -C "$$work/public" push --quiet origin HEAD:master "$(VERSION)"; \
	echo "publish: creating the release on $(PUBLIC_REPO)"; \
	gh release create "$(VERSION)" --repo $(PUBLIC_REPO) --verify-tag \
	  --title "$(VERSION) — $(TITLE)" \
	  --notes-file "$(NOTES)" \
	  $(TARBALLS) $(DIST)/SHA256SUMS-assets.txt; \
	rm -rf "$$work"; \
	echo "publish: $(VERSION) is live on $(PUBLIC_REPO) — source synced, $(words $(PLATFORMS)) tarballs + checksums attached"

# locale-demo proves the v0.1.3 claim that a shipped pack renders with nothing
# copied anywhere: it lays out a tarball's shape in a scratch directory, points
# GHILLIE_HOME at an empty home, and shows the same command in two languages.
locale-demo:
	@set -e; d=$$(mktemp -d); \
	$(GO) build -ldflags "-X main.version=$(VERSION)" -o $$d/ghillie ./cmd/ghillie; \
	cp -R bundles/locales $$d/locales; \
	echo "--- English (no pack asked for)"; \
	GHILLIE_HOME=$$d/home $$d/ghillie -version; \
	echo "--- German (pack read from $$d/locales, nothing copied)"; \
	GHILLIE_HOME=$$d/home GHILLIE_LANG=de $$d/ghillie -h 2>&1 | head -3; \
	rm -rf $$d

all: check build

# THE CONTROL ARM — ghillie with its admission gate REMOVED. Not a product.
# Deliberately absent from `all` and from `build`: you have to ask for it by
# name, and the binary carries UNGUARDED in its filename so it can never be
# confused with the real one on disk. See internal/gate/unguarded.go for why it
# exists (measurement control for the leakage experiment) and the four
# safeguards on it. It also refuses to start without an env acknowledgement.
unguarded:
	@mkdir -p $(BIN)
	$(GO) build -tags unguarded -o $(BIN)/ghillie-unguarded ./cmd/ghillie
	@echo "built $(BIN)/ghillie-unguarded — GATE DISABLED, measurement control only" 

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
	@if [ "$(OPSEC)" = "none" ]; then \
	  echo "opsec: OPSEC=none — no gate asked for; the string list above is yours to read"; \
	elif [ -x "$(OPSEC)" ] || [ -f "$(OPSEC)" ]; then bash $(OPSEC) $(BIN)/client-visible-strings.txt; \
	else \
	  echo "opsec: NO GATE at $(OPSEC) — REFUSING." >&2; \
	  echo "  A missing decider is a REFUSAL, not a shrug. This target used to print a notice and" >&2; \
	  echo "  exit 0, so an absent gate read exactly like a passing one — and on this machine the" >&2; \
	  echo "  gate has never been installed at all." >&2; \
	  echo "  Name your gate:        make opsec OPSEC=/path/to/gate.sh" >&2; \
	  echo "  Or say so explicitly:  make opsec OPSEC=none" >&2; \
	  exit 1; \
	fi

# The golden vectors are NOT regenerated by the build: they are the output of
# building and running the proven Ada core, which lives in a READ-ONLY tree and
# is copied out to scratch first. internal/frame/testdata/README.md records the
# exact procedure, and the generator is committed beside it.

# CATALOGUE vs TREE — what is SERVED must still exist here.
#
# On 2026-09-02 commit 861c5a8 removed bundles/the-old-words after df-opsec
# flagged its provenance.md for naming never-public codenames. Nine days later

# WHAT SHIPS MUST BE DOCUMENTED. DISTBINS grew to seven binaries while
# README-QUICKSTART.md still opens "This tarball holds four binaries" and
# documents four. The shipped v0.1.4 tarball does hold four, so the prose is
# right about what was published and wrong about what `make dist` would build
# today — the config ran ahead of the page that explains it. A stranger's first
# act is to untar and read that table; three binaries it does not mention is
# three things nobody can tell are optional, read-only, or safe.
docs-check:
	@set -e; \
	missing=""; \
	for b in $(DISTBINS); do \
	  grep -qw "$$b" README-QUICKSTART.md || missing="$$missing $$b"; \
	done; \
	if [ -n "$$missing" ]; then \
	  echo "docs-check: these ship in DISTBINS and are named nowhere in README-QUICKSTART.md:" >&2; \
	  for b in $$missing; do echo "    $$b" >&2; done; \
	  echo "  Either document them, or take them out of DISTBINS before the next dist." >&2; \
	  exit 1; \
	fi; \
	echo "docs-check: every binary in DISTBINS is named in the quickstart"
# the identical archive was still listed in the published index and still
# returned 200: every file byte-for-byte the flagged one. Removing a bundle
# from the tree is not a recall, and nothing anywhere noticed the difference.
#
# This target asks the one question nobody was asking: does every ability the
# public catalogue OFFERS still have a bundle in this tree? It is a drift
# check, not a leak scan — `opsec` is the leak scan, and it has never been
# pointed at the catalogue either.
CATALOGUE ?= https://thereef.ink/catalogue

catalogue-check:
	@set -e; \
	echo "== catalogue drift: $(CATALOGUE) vs bundles/ =="; \
	idx=$$(mktemp); \
	curl -fsS --max-time 30 "$(CATALOGUE)/index.json" -o "$$idx" || { echo "catalogue-check: cannot fetch the index — REFUSING (a check that cannot look is not a pass)" >&2; exit 1; }; \
	names=$$(sed -n 's/.*"name"[[:space:]]*:[[:space:]]*"\([a-zA-Z0-9_-]*\)".*/\1/p' "$$idx"); \
	[ -n "$$names" ] || { echo "catalogue-check: the index named no abilities — REFUSING rather than reporting clean" >&2; rm -f "$$idx"; exit 1; }; \
	withdrawn=0; \
	for n in $$names; do \
	  if [ -d "bundles/$$n" ]; then echo "  ok         $$n"; \
	  elif [ -n "$$(git log --oneline --all -- "bundles/$$n" 2>/dev/null | head -1)" ]; then \
	    echo "  WITHDRAWN  $$n — was in this tree, was removed, and is STILL SERVED"; \
	    withdrawn=$$((withdrawn+1)); \
	  else \
	    echo "  external   $$n — never in this repo's history; built and published elsewhere"; \
	  fi; \
	done; \
	rm -f "$$idx"; \
	if [ "$$withdrawn" -gt 0 ]; then \
	  echo "catalogue-check: $$withdrawn ability(ies) were REMOVED from this tree and are still being served." >&2; \
	  echo "  Removing a bundle is not a recall. Either restore it, or withdraw it from" >&2; \
	  echo "  $(CATALOGUE) — a withdrawal that stops at the working tree leaves the artefact public." >&2; \
	  exit 1; \
	fi; \
	echo "catalogue-check: nothing withdrawn-but-served"
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

# admission-front builds the proven install-admission decider.
#
# ★ INSTALLS ARE GATED ON THIS BINARY. `ghillie -install-ability` and
# `-get-ability` both consult it over the destination copy before the ledger
# entry is written, and a missing decider is a REFUSAL, not a shrug — a gate
# that quietly stops gating when its decider goes missing is worse than no
# gate, because the ledger goes on looking the same.
#
# The source is KingKlaw's admission member (AGPL-3.0-or-later, same as this
# tree), kept in its own repository on purpose so that anyone can take the gate
# without taking ghillie. Point KINGKLAW at your checkout; the build needs the
# free FSF GNAT + GNATprove package and nothing else.
#
# ⚠ This comment said "(MIT), kept out of this AGPL tree" until 2026-09-09.
# Both halves were wrong: KingKlaw is AGPL-3.0-or-later (its LICENSE, its
# alire.toml `licenses =`, and the SPDX header in every source file agree), and
# there was never a reason to keep AGPL source out of an AGPL tree. The reason
# it lives apart is reuse, not licence compatibility.
KINGKLAW ?= $(HOME)/dev/kingklaw

admission-front:
	@test -d "$(KINGKLAW)/admission" || { \
	  echo "no KingKlaw checkout at $(KINGKLAW) — set KINGKLAW=<path>"; exit 1; }
	@mkdir -p $(BIN)
	cd "$(KINGKLAW)/admission/front" && gprbuild -q -p -P edge.gpr
	cp "$(KINGKLAW)/admission/front/extension_admission_front" $(BIN)/
	@echo "the decider is in $(BIN). Prove it before you trust it:"
	@echo "  cd $(KINGKLAW)/admission/src && gnatprove -P proof.gpr -f -U --level=2"
	@echo "  cd $(KINGKLAW)/admission && go run ./tools/tablegen -out front/TABLE.tsv -check front/extension_admission_front"

clean:
	rm -rf $(BIN) .demo
