package soap

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"reflect"
	"strings"
)

const nsXOP = "http://www.w3.org/2004/08/xop/include"

// Binary is an xs:base64Binary element. Without MTOM it is written as base64
// text; in a call made WithMTOM it is written as an xop:Include pointing at a
// MIME part that carries the bytes as they are. Reading accepts both, so a
// response's binary content arrives in Data whichever way the service chose.
type Binary struct {
	Data []byte
	// ContentType is the MIME type of the attachment when sent with MTOM;
	// application/octet-stream when empty. It has no effect inline.
	ContentType string

	// contentID is set on the way out by the MTOM pass, and on the way in by
	// an xop:Include until the attachment is resolved.
	contentID string
}

// MarshalXML writes the bytes inline, or the reference to their attachment.
func (b Binary) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	if b.contentID != "" {
		include := xml.StartElement{
			Name: xml.Name{Local: "xop:Include"},
			Attr: []xml.Attr{
				{Name: xml.Name{Local: "xmlns:xop"}, Value: nsXOP},
				{Name: xml.Name{Local: "href"}, Value: "cid:" + url.PathEscape(b.contentID)},
			},
		}
		if err := e.EncodeToken(include); err != nil {
			return err
		}
		if err := e.EncodeToken(include.End()); err != nil {
			return err
		}
	} else if len(b.Data) > 0 {
		if err := e.EncodeToken(xml.CharData(base64.StdEncoding.EncodeToString(b.Data))); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}

// UnmarshalXML reads base64 text, or an xop:Include whose attachment the call
// resolves once the whole answer is read.
func (b *Binary) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var text strings.Builder
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.CharData:
			text.Write(t)
		case xml.StartElement:
			if t.Name.Local == "Include" && t.Name.Space == nsXOP {
				for _, a := range t.Attr {
					if a.Name.Local == "href" {
						id, err := contentIDFromHref(a.Value)
						if err != nil {
							return err
						}
						b.contentID = id
					}
				}
			}
			if err := d.Skip(); err != nil {
				return err
			}
		case xml.EndElement:
			if b.contentID != "" {
				return nil
			}
			clean := strings.Map(func(r rune) rune {
				if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
					return -1
				}
				return r
			}, text.String())
			if clean == "" {
				b.Data = nil
				return nil
			}
			data, err := base64.StdEncoding.DecodeString(clean)
			if err != nil {
				return fmt.Errorf("soap: base64 in <%s>: %w", start.Name.Local, err)
			}
			b.Data = data
			return nil
		}
	}
}

// contentIDFromHref turns href="cid:..." into the Content-ID it names. A cid
// URL is percent-encoded (RFC 2392) where the header is not.
func contentIDFromHref(href string) (string, error) {
	rest, ok := strings.CutPrefix(href, "cid:")
	if !ok {
		return "", fmt.Errorf("soap: xop:Include href %q is not a cid: URL", href)
	}
	id, err := url.PathUnescape(rest)
	if err != nil {
		return "", fmt.Errorf("soap: xop:Include href %q: %w", href, err)
	}
	return id, nil
}

// binaries calls fn for every Binary reachable from v through pointers,
// structs, slices and arrays. v must be a pointer for the Binaries to be
// settable.
func binaries(v any, fn func(*Binary) error) error {
	seen := map[uintptr]bool{}
	var walk func(rv reflect.Value) error
	walk = func(rv reflect.Value) error {
		switch rv.Kind() {
		case reflect.Pointer, reflect.Interface:
			if rv.IsNil() {
				return nil
			}
			if rv.Kind() == reflect.Pointer {
				if seen[rv.Pointer()] {
					return nil
				}
				seen[rv.Pointer()] = true
			}
			return walk(rv.Elem())
		case reflect.Struct:
			if rv.Type() == reflect.TypeFor[Binary]() {
				if !rv.CanAddr() {
					return errors.New("soap: a Binary must be reached through a pointer to be sent or received")
				}
				return fn(rv.Addr().Interface().(*Binary))
			}
			for i := range rv.NumField() {
				if !rv.Type().Field(i).IsExported() {
					continue
				}
				if err := walk(rv.Field(i)); err != nil {
					return err
				}
			}
		case reflect.Slice, reflect.Array:
			if rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() == reflect.Uint8 {
				return nil // []byte: data, not a container
			}
			for i := range rv.Len() {
				if err := walk(rv.Index(i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(reflect.ValueOf(v))
}

type part struct {
	contentID   string
	contentType string
	data        []byte
}

// prepareMTOM gives every non-empty Binary in req a Content-ID and returns the
// parts that carry them. It returns no parts when there is nothing to attach,
// and the request then goes as plain XML.
func prepareMTOM(req any) ([]part, error) {
	var parts []part
	err := binaries(req, func(b *Binary) error {
		if len(b.Data) == 0 {
			return nil
		}
		id, err := newContentID()
		if err != nil {
			return err
		}
		b.contentID = id
		ct := b.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		parts = append(parts, part{contentID: id, contentType: ct, data: b.Data})
		return nil
	})
	return parts, err
}

// clearMTOM undoes prepareMTOM, so a request struct reused for another call
// does not carry stale references.
func clearMTOM(req any) {
	_ = binaries(req, func(b *Binary) error { b.contentID = ""; return nil })
}

func newContentID() (string, error) {
	var r [8]byte
	if _, err := rand.Read(r[:]); err != nil {
		return "", fmt.Errorf("soap: content id: %w", err)
	}
	return hex.EncodeToString(r[:]) + "@soap-go", nil
}

const rootContentID = "root.message@soap-go"

// multipartBody writes the MTOM request: the envelope as the root part, then
// one part per attachment.
func multipartBody(envelope []byte, parts []part) (body []byte, contentType string, err error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	root, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              []string{`application/xop+xml; charset=UTF-8; type="text/xml"`},
		"Content-Transfer-Encoding": []string{"8bit"},
		"Content-ID":                []string{"<" + rootContentID + ">"},
	})
	if err != nil {
		return nil, "", err
	}
	if _, err := root.Write(envelope); err != nil {
		return nil, "", err
	}
	for _, p := range parts {
		w, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              []string{p.contentType},
			"Content-Transfer-Encoding": []string{"binary"},
			"Content-ID":                []string{"<" + p.contentID + ">"},
		})
		if err != nil {
			return nil, "", err
		}
		if _, err := w.Write(p.data); err != nil {
			return nil, "", err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	contentType = fmt.Sprintf(`multipart/related; type="application/xop+xml"; start="<%s>"; start-info="text/xml"; boundary="%s"`,
		rootContentID, mw.Boundary())
	return buf.Bytes(), contentType, nil
}

// splitResponse returns the envelope of an answer and, when it came as a
// multipart, its other parts keyed by Content-ID without angle brackets.
func splitResponse(contentType string, raw []byte) ([]byte, map[string][]byte, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return raw, nil, nil
	}
	start := strings.Trim(params["start"], "<>")
	mr := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	parts := map[string][]byte{}
	var envelope []byte
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("soap: multipart answer: %w", err)
		}
		data, err := io.ReadAll(p)
		if err != nil {
			return nil, nil, fmt.Errorf("soap: multipart answer: %w", err)
		}
		id := strings.Trim(p.Header.Get("Content-ID"), "<>")
		if envelope == nil && (start == "" || id == start) {
			envelope = data
			continue
		}
		parts[id] = data
	}
	if envelope == nil {
		return nil, nil, errors.New("soap: multipart answer without its root part")
	}
	return envelope, parts, nil
}

// resolveAttachments fills the Data of every Binary in resp that arrived as an
// xop:Include.
func resolveAttachments(resp any, parts map[string][]byte) error {
	return binaries(resp, func(b *Binary) error {
		if b.contentID == "" {
			return nil
		}
		data, ok := parts[b.contentID]
		if !ok {
			return fmt.Errorf("soap: attachment %q is not in the answer", b.contentID)
		}
		b.Data, b.contentID = data, ""
		return nil
	})
}
