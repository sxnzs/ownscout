GO ?= go
REF ?= spec/parity/reference-ownscout

.PHONY: build gate test corpus corpus-check reference fuzz verify-ports ports ports-test

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

verify-ports:
	spec/parity/verify-all.sh

gate: test corpus-check
