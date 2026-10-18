# soap-go

A small SOAP 1.1 client for Go: requests and responses are ordinary
`encoding/xml` structs, a fault comes back as a typed `*soap.Fault`, and binary
content travels inline as base64 or as an MTOM/XOP attachment. Standard
library only.

> **v0 — the API is not stable yet.** It may change between minor versions
> until it has served more than one integration. Pin a version.

## Why

Services that still speak SOAP — public administrations above all — publish
WSDLs that other ecosystems turn into clients with a generator. In Go the
envelope, the fault, the multipart body and the attachment references are
written by hand, every time. This module is that part, written once and tested
against a real JAX-WS service. A generator from WSDL is planned on top of it.

## Use

```go
type sendFile struct {
	XMLName  xml.Name    `xml:"http://example.org/ws sendFile"`
	FileName string      `xml:"fileName"`
	Document soap.Binary `xml:"document"`
}

type sendFileResponse struct {
	XMLName  xml.Name `xml:"http://example.org/ws sendFileResponse"`
	Protocol string   `xml:"return>protocol"`
}

c := &soap.Client{Auth: soap.BasicAuth(user, password)}
var resp sendFileResponse
err := c.Call(ctx, endpoint, "", &sendFile{FileName: "f.zip", Document: soap.Binary{Data: zip}}, &resp, soap.WithMTOM())

var fault *soap.Fault
if errors.As(err, &fault) { /* fault.Code, fault.String, fault.Detail */ }
```

- **Namespaces.** The request's `XMLName` names the operation and its
  namespace. The operation element is written under a prefix of its own, so
  children with no namespace in their tag stay unqualified and children with
  one are qualified — both shapes occur, often in the same family of services.
  In a qualified schema, spell nesting as structs: a path tag (`ns a>b`)
  qualifies only `b`.
- **MTOM.** `WithMTOM()` sends every non-empty `soap.Binary` as an attachment
  referenced by `xop:Include`. Responses are read either way.
- **Errors.** `*soap.Fault` for a SOAP fault, `*soap.HTTPError` for an answer
  that is not an envelope. Neither carries the request or a credential.
- **Limits.** Answers are read up to `MaxResponseBytes` (32 MiB by default).

## Using it before v1

While the module is at v0 it is fetched straight from GitHub rather than
through the public Go proxy, so a pre-release version is not indexed on
pkg.go.dev by our own builds:

```sh
export GOPRIVATE=github.com/improbabilitea/soap-go
```

No token is needed: the repository is public.

## Development

```sh
make test       # go test -race ./...
make lint       # golangci-lint run ./...
make test-fuzz  # short fuzz pass over the answer parser
make deps       # fails if go.mod acquires any dependency
```

Commits follow [Conventional Commits](https://www.conventionalcommits.org/);
[release-please](https://github.com/googleapis/release-please) turns them into
the release PR, the changelog and the tag.

## License

[Apache-2.0](LICENSE).
