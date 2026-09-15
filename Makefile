GO ?= go
REF ?= spec/parity/reference-ownscout

.PHONY: build gate test corpus corpus-check docs-check mutants mutants-check reference fuzz fuzz-sweep coverage verify-ports ports ports-test

build:
	$(GO) build -o bin/ownscout ./cmd/ownscout

test:
	$(GO) vet ./...
	$(GO) test ./...

corpus:
	$(GO) run ./tools/gen-corpus
	$(GO) run ./tools/gen-corpus-edge

# Fails when the committed corpora are stale relative to the reference.
corpus-check: corpus
	git diff --exit-code --quiet spec/parity/corpus.json spec/parity/corpus-edge.json || \
		{ echo "corpus is stale; commit the regenerated files"; exit 1; }

reference:
	$(GO) build -o $(REF) ./cmd/ownscout

# Build the port binaries the parity verifier expects. TypeScript needs no
# build step: ports/ts/bin/ownscout runs the source with Node.
ports:
	(cd ports/rust && cargo build --release)
	(cd ports/zig && zig build)

ports-test:
	(cd ports/ts && node --test)
	(cd ports/rust && cargo test)
	(cd ports/zig && zig build test)

fuzz: reference
	python3 spec/parity/fuzz.py --candidate $(REF)

# The corpora pin behaviours that have been seen; the fuzzer explores inputs no
# case names. A single seed is not a verification strategy: seven divergence
# classes in a row were reachable only at seeds the gate did not run, and each
# one was found by sweeping rather than by the seed that happened to be current.
# Seeds 3, 6, 8, 13, 20, 21 and 24 are the ones that have caught something.
FUZZ_SEEDS ?= 3 6 8 13 20 21 24
FUZZ_ITERATIONS ?= 200
PORT_BINARIES ?= ports/ts/bin/ownscout ports/rust/target/release/ownscout ports/zig/zig-out/bin/ownscout

fuzz-sweep: reference ports
	@for port in $(PORT_BINARIES); do \
		for seed in $(FUZZ_SEEDS); do \
			python3 spec/parity/fuzz.py --candidate $$port \
				--iterations $(FUZZ_ITERATIONS) --seed $$seed || exit 1; \
		done; \
	done

verify-ports:
	spec/parity/verify-all.sh

# Which reference code do the corpora actually exercise? A path no case reaches
# is a path a port can skip and still pass, which is how the append-time ledger
# cap and the symlinked-ancestor check stayed invisible. Build the reference with
# coverage, replay both corpora against it, and list what never ran.
#
# This lane now fails on a never-executed function that is not on the exemption
# list below. It used to only print, so an entry could sit there forever; two
# did. An exemption is a claim that no corpus case *can* reach the code, not
# that writing one would be inconvenient.
COVDIR ?= /tmp/ownscout-cov
COVREF ?= /tmp/ownscout-cover

# <file:line:func> entries that the corpus cannot reach, each justified:
#   VerifyPacket      a Go-level wrapper around VerifyPacketWithOptions. The CLI
#                     (the only thing the corpora drive) never calls it; the
#                     unit tests and benchmarks do. No case can call a Go API.
#   *ValidationError.Unwrap
#                     reached only when a ValidationError is wrapped inside
#                     another error. Nothing wraps it, so errors.As matches the
#                     concrete type directly. It stays so a future wrapper keeps
#                     working, and internal/ledger tests it.
COVER_EXEMPT ?= ownscout/internal/evidence/evidence.go:53:VerifyPacket \
                ownscout/internal/ledger/ledger.go:267:*ValidationError.Unwrap

coverage:
	rm -rf $(COVDIR) && mkdir -p $(COVDIR)
	$(GO) build -cover -o $(COVREF) ./cmd/ownscout
	GOCOVERDIR=$(COVDIR) python3 spec/parity/harness.py --bin $(COVREF) >/dev/null
	GOCOVERDIR=$(COVDIR) python3 spec/parity/harness.py --corpus spec/parity/corpus-edge.json --bin $(COVREF) >/dev/null
	$(GO) tool covdata percent -i=$(COVDIR)
	@$(GO) tool covdata func -i=$(COVDIR) | awk '$$1 ~ /^ownscout/ && $$NF == "0.0%" { print $$1 $$2 }' > $(COVDIR)/dead.txt
	@printf '%s\n' $(COVER_EXEMPT) > $(COVDIR)/exempt.txt
	@echo "--- never executed, exempt and justified ---"
	@grep -Fxf $(COVDIR)/exempt.txt $(COVDIR)/dead.txt | sed 's/^/  /' || true
	@echo "--- never executed, UNEXPLAINED ---"
	@grep -vFxf $(COVDIR)/exempt.txt $(COVDIR)/dead.txt > $(COVDIR)/unexplained.txt || true
	@if [ -s $(COVDIR)/unexplained.txt ]; then \
		sed 's/^/  /' $(COVDIR)/unexplained.txt; \
		echo "coverage: unexplained never-executed function: add a corpus case, delete the code, or add it to COVER_EXEMPT with a reason"; \
		exit 1; \
	fi
	@echo "coverage: every never-executed function is accounted for"

# Fails when a document restates a corpus count the corpus no longer has. The
# counts live in two badges, several prose sentences, a status table, the port
# contract and two diagrams, and every copy is hand-maintained; a diagram once
# said 93 edge beside a card saying 208, and a mutation table carried one
# denominator under a total using another.
#
# It also pins the mutation *count* - "five deliberately broken reference
# builds" in four documents - to the number of mutation rows in the port
# contract. That sentence said "three" above a four-row table and no gate
# noticed. The mutation numerators stay out of this target: they need five
# builds and ten corpus replays, which is `mutants-check`.
docs-check:
	$(GO) run ./tools/check-docs

# Re-measure the mutation table: stage a copy of the module per mutation, apply
# one exact edit, build, replay both corpora, and rewrite the governed region of
# spec/parity/PORT.md. The working tree is never mutated.
mutants:
	$(GO) run ./tools/mutants -write

# Fails when that table disagrees with a fresh measurement. Unlike corpus-check
# this compares against a measurement rather than `git diff`, so unrelated
# uncommitted work cannot make it fail. It costs about 33s - five builds and ten
# replays both corpora against five builds - which is why it is its own target and a CI job rather
# than part of `make gate`, whose warm run is under 10s.
mutants-check:
	$(GO) run ./tools/mutants -check

gate: test corpus-check docs-check
