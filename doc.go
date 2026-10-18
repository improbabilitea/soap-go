// Package soap is a small SOAP 1.1 client for document/literal services:
// requests and responses are ordinary encoding/xml structs, a fault comes
// back as a typed *Fault, and binary content travels inline as base64 or as
// an MTOM/XOP attachment.
//
// It exists because the services that still speak SOAP — public
// administrations above all — publish WSDLs that other ecosystems turn into
// clients with a generator, while Go has to write envelopes by hand. This
// package is the part that every such client repeats: the envelope, the
// fault, the multipart body, the attachment references. A generator from
// WSDL is planned on top of it, not inside it.
//
// # A call
//
// The request struct names its operation element, namespace included, in its
// XMLName tag; its children name their own namespace when the schema wants
// them qualified, and none when it does not:
//
//	type sendFile struct {
//		XMLName  xml.Name    `xml:"http://example.org/ws sendFile"`
//		FileName string      `xml:"fileName"`  // unqualified child
//		Document soap.Binary `xml:"document"`  // base64, or MTOM with WithMTOM
//	}
//
//	type sendFileResponse struct {
//		XMLName  xml.Name `xml:"http://example.org/ws sendFileResponse"`
//		Protocol string   `xml:"return>protocol"`
//	}
//
//	c := &soap.Client{Auth: soap.BasicAuth(user, password)}
//	var resp sendFileResponse
//	err := c.Call(ctx, endpoint, "", &sendFile{...}, &resp, soap.WithMTOM())
//
// In a qualified schema every element carries the namespace, nested ones
// included, so nesting is spelt as structs. A path tag such as
// `xml:"http://example.org/ws a>b"` qualifies b and leaves a in no namespace:
// encoding/xml applies the namespace to the last name only.
//
// # Stability
//
// The module is at v0: its API may still change between minor versions, and
// it will until it has served more than one integration.
package soap
