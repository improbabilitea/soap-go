package soap

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
)

const nsEnvelope = "http://schemas.xmlsoap.org/soap/envelope/"

// operationPrefix is the prefix the operation element is written with.
//
// encoding/xml cannot write a namespace prefix: an element in a namespace
// gets a default xmlns="..." declaration, which every child then inherits —
// so a schema whose children are unqualified (elementFormDefault left at its
// default) would see them qualified. Writing the operation element under a
// prefix of its own declares the namespace for that element alone: children
// without a namespace in their tag stay unqualified, children with one get
// their own xmlns="..." and are qualified. Both shapes occur in the same
// family of services, so both have to come out right.
const operationPrefix = "ns0"

// marshalEnvelope writes req inside a SOAP 1.1 envelope.
func marshalEnvelope(req any) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`<soapenv:Envelope xmlns:soapenv="` + nsEnvelope + `"><soapenv:Body>`)
	enc := xml.NewEncoder(&buf)
	if req != nil {
		ns, local := operationName(req)
		var err error
		if ns != "" {
			err = enc.EncodeElement(req, xml.StartElement{
				Name: xml.Name{Local: operationPrefix + ":" + local},
				Attr: []xml.Attr{{Name: xml.Name{Local: "xmlns:" + operationPrefix}, Value: ns}},
			})
		} else {
			err = enc.Encode(req)
		}
		if err != nil {
			return nil, fmt.Errorf("soap: marshal request: %w", err)
		}
		if err := enc.Flush(); err != nil {
			return nil, fmt.Errorf("soap: marshal request: %w", err)
		}
	}
	buf.WriteString(`</soapenv:Body></soapenv:Envelope>`)
	return buf.Bytes(), nil
}

// operationName reads the namespace and local name of the operation element
// from the XMLName field's tag: `xml:"http://example.org/ws sendFile"`.
func operationName(v any) (ns, local string) {
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return "", ""
	}
	f, ok := t.FieldByName("XMLName")
	if !ok || f.Type != reflect.TypeFor[xml.Name]() {
		return "", ""
	}
	tag, _, _ := strings.Cut(f.Tag.Get("xml"), ",")
	if ns, local, ok := strings.Cut(tag, " "); ok {
		return ns, local
	}
	return "", tag
}

// errNoEnvelope reports an answer that does not parse as a SOAP envelope.
var errNoEnvelope = errors.New("not a SOAP envelope")

// decodeEnvelope reads the Body of a SOAP 1.1 envelope: a *Fault when it
// carries one, otherwise its first element decoded into resp (when resp is
// not nil).
func decodeEnvelope(envelope []byte, resp any) error {
	dec := xml.NewDecoder(bytes.NewReader(envelope))
	if err := enterBody(dec); err != nil {
		return err
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: %v", errNoEnvelope, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "Fault" && t.Name.Space == nsEnvelope {
				var f struct {
					Code   string `xml:"faultcode"`
					String string `xml:"faultstring"`
					Actor  string `xml:"faultactor"`
					Detail struct {
						Inner []byte `xml:",innerxml"`
					} `xml:"detail"`
				}
				if err := dec.DecodeElement(&f, &t); err != nil {
					return fmt.Errorf("soap: decode fault: %w", err)
				}
				return &Fault{Code: f.Code, String: f.String, Actor: f.Actor, Detail: f.Detail.Inner}
			}
			if resp == nil {
				return nil
			}
			if err := dec.DecodeElement(resp, &t); err != nil {
				return fmt.Errorf("soap: decode response: %w", err)
			}
			return nil
		case xml.EndElement:
			// An empty Body: a one-way operation, or nothing to decode.
			return nil
		}
	}
}

// enterBody advances the decoder past <Envelope> and <Body>, skipping a
// Header if there is one.
func enterBody(dec *xml.Decoder) error {
	sawEnvelope := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) || err != nil {
			return errNoEnvelope
		}
		t, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch {
		case !sawEnvelope && t.Name.Local == "Envelope" && t.Name.Space == nsEnvelope:
			sawEnvelope = true
		case sawEnvelope && t.Name.Local == "Header" && t.Name.Space == nsEnvelope:
			if err := dec.Skip(); err != nil {
				return errNoEnvelope
			}
		case sawEnvelope && t.Name.Local == "Body" && t.Name.Space == nsEnvelope:
			return nil
		default:
			return errNoEnvelope
		}
	}
}
