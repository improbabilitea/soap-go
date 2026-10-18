.PHONY: lint test test-fuzz deps tidy

lint:
	golangci-lint run ./...

test:
	go test -race ./...

# Short fuzz pass over the answer parser — the only code that reads input from
# outside. CI runs the same target; locally this is the pre-commit check.
test-fuzz:
	go test -run=NONE -fuzz=FuzzDecodeResponse -fuzztime=30s .

# The module takes no dependency outside the standard library. `go list -m all`
# prints this module first, so anything on a later line is an acquired one.
deps:
	@test -z "$$(go list -m all | tail -n +2)" \
		|| { echo "external dependencies found:"; go list -m all | tail -n +2; exit 1; }

tidy:
	go mod tidy
