package soap_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/improbabilitea/soap-go"
)

// The shapes below follow the services this package was first written for —
// a JAX-WS endpoint whose children are unqualified, and one whose children
// are qualified — without depending on either.

type sendFile struct {
	XMLName  xml.Name    `xml:"http://ejb.example.org/ sendFile"`
	FileName string      `xml:"fileName"`
	Pin      string      `xml:"pin"`
	Document soap.Binary `xml:"document"`
}

type sendFileResponse struct {
	XMLName xml.Name `xml:"http://ejb.example.org/ sendFileResponse"`
	Return  struct {
		Code     string `xml:"code"`
		Protocol string `xml:"protocol"`
	} `xml:"return"`
}

// insertDocument is qualified throughout: every element names the namespace,
// the nested one included. A path tag (`ns a>b`) would qualify b and leave a
// in no namespace, which is why nesting is spelt as structs here.
type insertDocument struct {
	XMLName xml.Name `xml:"http://doc.example.org insertRequest"`
	Pin     string   `xml:"http://doc.example.org pincode"`
	IDSpesa struct {
		VAT string `xml:"http://doc.example.org pIva"`
	} `xml:"http://doc.example.org idSpesa"`
}

type reportRequest struct {
	XMLName  xml.Name `xml:"http://errors.example.org/ Report"`
	Protocol string   `xml:"protocol"`
}

const sendFileAnswer = `<S:Envelope xmlns:S="http://schemas.xmlsoap.org/soap/envelope/"><S:Body>` +
	`<ns2:sendFileResponse xmlns:ns2="http://ejb.example.org/"><return>` +
	`<code>000</code><protocol>26101812305347573</protocol>` +
	`</return></ns2:sendFileResponse></S:Body></S:Envelope>`

func server(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestUnqualifiedChildrenStayUnqualified(t *testing.T) {
	var got []byte
	url := server(t, func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, sendFileAnswer)
	})

	var resp sendFileResponse
	err := (&soap.Client{}).Call(context.Background(), url, "", &sendFile{FileName: "f.zip", Pin: "secret"}, &resp)
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	if !strings.Contains(body, `<ns0:sendFile xmlns:ns0="http://ejb.example.org/"><fileName>f.zip</fileName>`) {
		t.Fatalf("the operation is not prefixed, or its children inherited its namespace:\n%s", body)
	}
	if resp.Return.Protocol != "26101812305347573" || resp.Return.Code != "000" {
		t.Fatalf("response not decoded: %+v", resp)
	}

	// The same request, decoded the way a schema-aware server reads it: the
	// children must be in no namespace at all.
	var check struct {
		Body struct {
			Op struct {
				FileName xml.Name `xml:"fileName"`
				Children []struct {
					XMLName xml.Name
				} `xml:",any"`
			} `xml:"http://ejb.example.org/ sendFile"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(got, &check); err != nil {
		t.Fatal(err)
	}
	for _, c := range check.Body.Op.Children {
		if c.XMLName.Space != "" {
			t.Errorf("child %s is in namespace %q, want none", c.XMLName.Local, c.XMLName.Space)
		}
	}
}

func TestQualifiedChildrenCarryTheirNamespace(t *testing.T) {
	var got []byte
	url := server(t, func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, `<S:Envelope xmlns:S="http://schemas.xmlsoap.org/soap/envelope/"><S:Body/></S:Envelope>`)
	})
	req := &insertDocument{Pin: "p"}
	req.IDSpesa.VAT = "12345678901"
	if err := (&soap.Client{}).Call(context.Background(), url, "insert", req, nil); err != nil {
		t.Fatal(err)
	}
	var check struct {
		Body struct {
			Op struct {
				Pin     string `xml:"http://doc.example.org pincode"`
				IDSpesa struct {
					VAT string `xml:"http://doc.example.org pIva"`
				} `xml:"http://doc.example.org idSpesa"`
			} `xml:"http://doc.example.org insertRequest"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(got, &check); err != nil {
		t.Fatal(err)
	}
	if check.Body.Op.Pin != "p" || check.Body.Op.IDSpesa.VAT != "12345678901" {
		t.Fatalf("qualified children not found in their namespace:\n%s", got)
	}
}

func TestHeadersAndAuthentication(t *testing.T) {
	url := server(t, func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "u" || pass != "p" {
			t.Errorf("basic auth = %q %q %v", user, pass, ok)
		}
		if got := r.Header.Get("SOAPAction"); got != `"inserimento.example"` {
			t.Errorf("SOAPAction = %s", got)
		}
		if got := r.Header.Get("Content-Type"); got != "text/xml; charset=utf-8" {
			t.Errorf("Content-Type = %s", got)
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, sendFileAnswer)
	})
	c := &soap.Client{Auth: soap.BasicAuth("u", "p")}
	if err := c.Call(context.Background(), url, "inserimento.example", &sendFile{}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMTOMRequest(t *testing.T) {
	payload := zipped(t, "veterinario.xml", "<precompilata/>")
	url := server(t, func(w http.ResponseWriter, r *http.Request) {
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/related" {
			t.Fatalf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		if params["type"] != "application/xop+xml" || params["start-info"] != "text/xml" {
			t.Errorf("params = %v", params)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		root, err := mr.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		if root.Header.Get("Content-ID") != params["start"] {
			t.Errorf("root Content-ID %q, start %q", root.Header.Get("Content-ID"), params["start"])
		}
		env, _ := io.ReadAll(root)
		href := between(string(env), `href="cid:`, `"`)
		if href == "" {
			t.Fatalf("no xop:Include in the envelope:\n%s", env)
		}
		if strings.Contains(string(env), base64.StdEncoding.EncodeToString(payload)) {
			t.Error("the bytes went inline as well as attached")
		}
		att, err := mr.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		if att.Header.Get("Content-ID") != "<"+href+">" {
			t.Errorf("attachment Content-ID %q, href %q", att.Header.Get("Content-ID"), href)
		}
		data, _ := io.ReadAll(att)
		if !bytes.Equal(data, payload) {
			t.Error("attachment bytes differ from the payload")
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, sendFileAnswer)
	})

	req := &sendFile{FileName: "veterinario.zip", Document: soap.Binary{Data: payload}}
	if err := (&soap.Client{}).Call(context.Background(), url, "", req, nil, soap.WithMTOM()); err != nil {
		t.Fatal(err)
	}

	// A reused request carries no stale reference: without MTOM, inline.
	var inline []byte
	url2 := server(t, func(w http.ResponseWriter, r *http.Request) {
		inline, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, sendFileAnswer)
	})
	if err := (&soap.Client{}).Call(context.Background(), url2, "", req, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(inline), base64.StdEncoding.EncodeToString(payload)) || strings.Contains(string(inline), "xop:Include") {
		t.Errorf("reused request not sent inline:\n%s", inline)
	}
}

type errorReport struct {
	XMLName xml.Name `xml:"http://errors.example.org/ ReportResponse"`
	Data    struct {
		Result string      `xml:"result"`
		CSV    soap.Binary `xml:"positive>detail>csv"`
	} `xml:"Data"`
}

func TestMTOMResponseAttachment(t *testing.T) {
	csv := "protocol;code;description\n1;WS19;CF non valido\n"
	url := server(t, func(w http.ResponseWriter, _ *http.Request) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		root, _ := mw.CreatePart(map[string][]string{"Content-ID": {"<root>"}, "Content-Type": {"application/xop+xml"}})
		_, _ = io.WriteString(root, `<S:Envelope xmlns:S="http://schemas.xmlsoap.org/soap/envelope/"><S:Header><x/></S:Header><S:Body>`+
			`<ns2:ReportResponse xmlns:ns2="http://errors.example.org/"><Data><result>0</result><positive><detail>`+
			`<csv><xop:Include xmlns:xop="http://www.w3.org/2004/08/xop/include" href="cid:csv%401"/></csv>`+
			`</detail></positive></Data></ns2:ReportResponse></S:Body></S:Envelope>`)
		att, _ := mw.CreatePart(map[string][]string{"Content-ID": {"<csv@1>"}})
		_, _ = io.WriteString(att, csv)
		_ = mw.Close()
		w.Header().Set("Content-Type", `multipart/related; type="application/xop+xml"; start="<root>"; boundary="`+mw.Boundary()+`"`)
		_, _ = w.Write(buf.Bytes())
	})
	var resp errorReport
	if err := (&soap.Client{}).Call(context.Background(), url, "", &reportRequest{Protocol: "1"}, &resp); err != nil {
		t.Fatal(err)
	}
	if string(resp.Data.CSV.Data) != csv {
		t.Fatalf("csv = %q", resp.Data.CSV.Data)
	}
}

func TestInlineBase64Response(t *testing.T) {
	url := server(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, `<S:Envelope xmlns:S="http://schemas.xmlsoap.org/soap/envelope/"><S:Body>`+
			`<ns2:ReportResponse xmlns:ns2="http://errors.example.org/"><Data><positive><detail><csv>`+
			"aGVs\n bG8=</csv></detail></positive></Data></ns2:ReportResponse></S:Body></S:Envelope>")
	})
	var resp errorReport
	if err := (&soap.Client{}).Call(context.Background(), url, "", &reportRequest{Protocol: "1"}, &resp); err != nil {
		t.Fatal(err)
	}
	if string(resp.Data.CSV.Data) != "hello" {
		t.Fatalf("csv = %q", resp.Data.CSV.Data)
	}
}

func TestAFaultIsTyped(t *testing.T) {
	url := server(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `<env:Envelope xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"><env:Body><env:Fault>`+
			`<faultcode>env:Client</faultcode><faultstring>cvc-complex-type.2.4.b: pincode expected</faultstring>`+
			`<detail><reason>schema</reason></detail></env:Fault></env:Body></env:Envelope>`)
	})
	err := (&soap.Client{Auth: soap.BasicAuth("u", "hunter2")}).Call(context.Background(), url, "", &sendFile{}, &sendFileResponse{})
	var f *soap.Fault
	if !errors.As(err, &f) {
		t.Fatalf("got %v, want *soap.Fault", err)
	}
	if f.Code != "env:Client" || !strings.Contains(f.String, "pincode expected") || !strings.Contains(string(f.Detail), "<reason>schema</reason>") {
		t.Errorf("fault = %+v", f)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Error("the error carries the credential")
	}
}

func TestAnAnswerThatIsNotSOAP(t *testing.T) {
	url := server(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "<html><body>Unauthorized</body></html>")
	})
	err := (&soap.Client{}).Call(context.Background(), url, "", &sendFile{}, &sendFileResponse{})
	var he *soap.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("got %v, want *soap.HTTPError", err)
	}
	if he.Status != http.StatusUnauthorized || !strings.Contains(he.Excerpt, "Unauthorized") {
		t.Errorf("error = %+v", he)
	}
}

func TestTheAnswerIsBounded(t *testing.T) {
	url := server(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write(bytes.Repeat([]byte("x"), 2048))
	})
	err := (&soap.Client{MaxResponseBytes: 1024}).Call(context.Background(), url, "", &sendFile{}, nil)
	if err == nil || !strings.Contains(err.Error(), "larger than 1024 bytes") {
		t.Fatalf("got %v", err)
	}
}

func TestATransportErrorDoesNotRepeatTheURLsSecrets(t *testing.T) {
	err := (&soap.Client{}).Call(context.Background(), "http://user:hunter2@127.0.0.1:1/ws?token=abc", "", &sendFile{}, nil)
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "token=abc") {
		t.Errorf("error leaks the URL's secrets: %v", err)
	}
}

func zipped(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(w, content)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func between(s, from, to string) string {
	_, rest, ok := strings.Cut(s, from)
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(rest, to)
	return v
}
