package soap_test

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/improbabilitea/soap-go"
)

func Example() {
	// A stand-in for the service: it answers every call with a receipt.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<S:Envelope xmlns:S="http://schemas.xmlsoap.org/soap/envelope/"><S:Body>`+
			`<ns2:sendFileResponse xmlns:ns2="http://example.org/ws"><return><protocol>42</protocol></return>`+
			`</ns2:sendFileResponse></S:Body></S:Envelope>`)
	}))
	defer srv.Close()

	type sendFile struct {
		XMLName  xml.Name    `xml:"http://example.org/ws sendFile"`
		FileName string      `xml:"fileName"`
		Document soap.Binary `xml:"document"`
	}
	type sendFileResponse struct {
		XMLName  xml.Name `xml:"http://example.org/ws sendFileResponse"`
		Protocol string   `xml:"return>protocol"`
	}

	c := &soap.Client{Auth: soap.BasicAuth("user", "password")}
	req := &sendFile{FileName: "report.zip", Document: soap.Binary{Data: []byte("PK...")}}
	var resp sendFileResponse
	if err := c.Call(context.Background(), srv.URL, "", req, &resp, soap.WithMTOM()); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("protocol", resp.Protocol)
	// Output: protocol 42
}
