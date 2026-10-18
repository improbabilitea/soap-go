package soap

import (
	"encoding/xml"
	"testing"
)

// FuzzDecodeResponse feeds arbitrary answers to the only code that reads
// input from outside: the multipart split, the envelope, the attachment
// references. It must return an error or a value, never panic.
func FuzzDecodeResponse(f *testing.F) {
	type resp struct {
		XMLName xml.Name `xml:"r"`
		Code    string   `xml:"code"`
		Data    Binary   `xml:"data"`
	}
	f.Add("text/xml", []byte(`<S:Envelope xmlns:S="http://schemas.xmlsoap.org/soap/envelope/"><S:Body><r><code>0</code><data>aGk=</data></r></S:Body></S:Envelope>`))
	f.Add("text/xml", []byte(`<S:Envelope xmlns:S="http://schemas.xmlsoap.org/soap/envelope/"><S:Body><S:Fault><faultcode>c</faultcode><faultstring>s</faultstring></S:Fault></S:Body></S:Envelope>`))
	f.Add(`multipart/related; start="<a>"; boundary="b"`, []byte("--b\r\nContent-ID: <a>\r\n\r\n<S:Envelope xmlns:S=\"http://schemas.xmlsoap.org/soap/envelope/\"><S:Body><r><data><xop:Include xmlns:xop=\"http://www.w3.org/2004/08/xop/include\" href=\"cid:x\"/></data></r></S:Body></S:Envelope>\r\n--b\r\nContent-ID: <x>\r\n\r\nbytes\r\n--b--\r\n"))
	f.Add("text/html", []byte("<html>error</html>"))

	f.Fuzz(func(t *testing.T, contentType string, raw []byte) {
		env, parts, err := splitResponse(contentType, raw)
		if err != nil {
			return
		}
		var r resp
		if err := decodeEnvelope(env, &r); err != nil {
			return
		}
		_ = resolveAttachments(&r, parts)
	})
}
