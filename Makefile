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
COVDIR ?= /tmp/ownscout-cov
COVREF ?= /tmp/ownscout-cover

coverage:
	rm -rf $(COVDIR) && mkdir -p $(COVDIR)
	$(GO) build -cover -o $(COVREF) ./cmd/ownscout
	GOCOVERDIR=$(COVDIR) python3 spec/parity/harness.py --bin $(COVREF) >/dev/null
	GOCOVERDIR=$(COVDIR) python3 spec/parity/harness.py --corpus spec/parity/corpus-edge.json --bin $(COVREF) >/dev/null
	$(GO) tool covdata percent -i=$(COVDIR)
	@echo "--- never executed ---"
	@$(GO) tool covdata func -i=$(COVDIR) | awk '$$1 ~ /^ownscout/ && $$NF == "0.0%" { print "  " $$0 }'

# Fails when a document restates a corpus count the corpus no longer has. The
# counts live in two badges, several prose sentences, a status table, the port
# contract and two diagrams, and every copy is hand-maintained; a diagram once
# said 93 edge beside a card saying 208, and a mutation table carried one
# denominator under a total using another.
#
# It also pins the mutation *count* - "four deliberately broken reference
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
# replays of 278 cases - which is why it is its own target and a CI job rather
# than part of `make gate`, whose warm run is under 10s.
mutants-check:
	$(GO) run ./tools/mutants -check

gate: test corpus-check docs-check
