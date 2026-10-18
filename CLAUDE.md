# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make test       # go test -race ./...
make lint       # golangci-lint run ./...
make test-fuzz  # 30s fuzz pass over the answer parser
make deps       # fails if go.mod acquires any external dependency
```

Pre-commit gate — all must pass:

```bash
go build ./... && go vet ./... && golangci-lint run ./... && go test -race ./... && make deps
```

## What this is

A SOAP 1.1 document/literal client on the standard library: envelope,
typed fault, MTOM/XOP attachments. Extracted from the Sistema Tessera
Sanitaria adapter of pourpet-pro-api, where it was proven against the real
test environment before it moved here (ADR 0024 there). The first consumer is
that adapter; keep anything specific to one service out of this module.

The reasoning behind each non-obvious choice lives in the code comments next
to it — read them before changing what they explain. The ones that matter
most:

- **The operation element is written under a prefix** (`envelope.go`,
  `operationPrefix`). encoding/xml cannot write prefixes; a default
  `xmlns="..."` on the operation would qualify every unqualified child.
  Changing this breaks services whose schemas leave `elementFormDefault`
  unqualified.
- **`Binary` carries a private content ID** (`mtom.go`): set on the way out by
  the MTOM pass and cleared after the call, set on the way in by an
  `xop:Include` until the attachment is resolved.
- **Errors never carry the request**: not the body, not credentials, not the
  endpoint's user info or query (`client.go`, `redact`).

## Constraints

- No dependency outside the standard library (`make deps` in CI).
- v0: the API may change, but every change is a Conventional Commit, and a
  breaking one says so (`feat!:` / `BREAKING CHANGE:`) — release-please turns
  it into a minor bump while pre-1.0.
- Commit messages in English.
- The fuzz target covers everything that parses an answer; a new parsing path
  gets a seed in `FuzzDecodeResponse`.
