package soap

import (
	"fmt"
	"strings"
)

// Fault is a SOAP 1.1 fault: the service refused the request as such — a
// schema violation, a credential it would not take — and said so in the
// envelope rather than in the body of an answer.
//
// It is returned as an error; errors.As recovers it.
type Fault struct {
	Code   string // faultcode, e.g. "env:Client" or "soap:Server"
	String string // faultstring, the service's own words
	Actor  string // faultactor, often empty
	Detail []byte // the detail element's inner XML, unparsed
}

func (f *Fault) Error() string {
	return fmt.Sprintf("soap: fault %s: %s", f.Code, f.String)
}

// HTTPError is an answer that was not a SOAP envelope, or a non-200 answer
// carrying no fault: a gateway's error page, an authentication refusal before
// the service was reached.
//
// Excerpt keeps the start of the body so the error stays readable in a log
// without carrying a whole HTML page into it. Nothing of the request is ever
// in it: not the body, not the credentials.
type HTTPError struct {
	Status  int
	Excerpt string
}

func (e *HTTPError) Error() string {
	if e.Excerpt == "" {
		return fmt.Sprintf("soap: HTTP %d", e.Status)
	}
	return fmt.Sprintf("soap: HTTP %d: %s", e.Status, e.Excerpt)
}

func excerpt(raw []byte) string {
	const max = 300
	s := strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(string(raw)))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
