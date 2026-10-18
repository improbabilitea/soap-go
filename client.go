package soap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// DefaultMaxResponseBytes is how much of an answer a Client reads when
// MaxResponseBytes is zero.
const DefaultMaxResponseBytes = 32 << 20

// Authenticator sets whatever a service wants on each request before it is
// sent: a header, a token. It is called once per call, before the request
// leaves.
type Authenticator func(*http.Request)

// BasicAuth sends HTTP Basic credentials on every request, preemptively:
// services that expect them often answer a request without them with an
// error page rather than a challenge.
func BasicAuth(user, password string) Authenticator {
	return func(r *http.Request) { r.SetBasicAuth(user, password) }
}

// Client calls SOAP 1.1 services. The zero value is usable: it sends with
// http.DefaultClient and no authentication. A Client holds no state between
// calls and is safe for concurrent use.
//
// A client certificate is set on HTTP's transport, not here; a Client only
// decides what goes into the request.
type Client struct {
	HTTP             *http.Client
	Auth             Authenticator
	MaxResponseBytes int64
}

// CallOption changes how one call is made.
type CallOption func(*callOptions)

type callOptions struct {
	mtom bool
}

// WithMTOM sends every non-empty Binary in the request as an MTOM attachment
// instead of inline base64. A request with nothing to attach goes as plain
// XML either way.
func WithMTOM() CallOption {
	return func(o *callOptions) { o.mtom = true }
}

// Call sends req as the body of a SOAP 1.1 envelope to endpoint, with
// SOAPAction set to action (empty is common), and decodes the first element
// of the answer's body into resp.
//
// req and resp are encoding/xml structs; the request's XMLName tag names the
// operation element and its namespace (see the package documentation for why
// the namespace matters to the children). resp may be nil for an operation
// whose answer is not needed. With WithMTOM, req must be a pointer.
//
// A fault in the answer is returned as *Fault; an answer that is not an
// envelope, or a non-200 status without a fault, as *HTTPError.
func (c *Client) Call(ctx context.Context, endpoint, action string, req, resp any, opts ...CallOption) error {
	var o callOptions
	for _, opt := range opts {
		opt(&o)
	}

	var parts []part
	if o.mtom && req != nil {
		var err error
		if parts, err = prepareMTOM(req); err != nil {
			return err
		}
		defer clearMTOM(req)
	}
	envelope, err := marshalEnvelope(req)
	if err != nil {
		return err
	}
	body, contentType := envelope, "text/xml; charset=utf-8"
	if len(parts) > 0 {
		if body, contentType, err = multipartBody(envelope, parts); err != nil {
			return fmt.Errorf("soap: build multipart: %w", err)
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("soap: %w", err)
	}
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("SOAPAction", `"`+action+`"`)
	if c.Auth != nil {
		c.Auth(httpReq)
	}

	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	httpResp, err := hc.Do(httpReq)
	if err != nil {
		return fmt.Errorf("soap: %s: %w", redact(endpoint), unwrapURLError(err))
	}
	defer func() { _ = httpResp.Body.Close() }()

	limit := c.MaxResponseBytes
	if limit <= 0 {
		limit = DefaultMaxResponseBytes
	}
	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, limit+1))
	if err != nil {
		return fmt.Errorf("soap: %s: read answer: %w", redact(endpoint), err)
	}
	if int64(len(raw)) > limit {
		return fmt.Errorf("soap: %s: answer larger than %d bytes", redact(endpoint), limit)
	}

	env, attachments, err := splitResponse(httpResp.Header.Get("Content-Type"), raw)
	if err != nil {
		return err
	}
	err = decodeEnvelope(env, resp)
	switch {
	case errors.Is(err, errNoEnvelope):
		return &HTTPError{Status: httpResp.StatusCode, Excerpt: excerpt(raw)}
	case err != nil:
		return err
	case httpResp.StatusCode != http.StatusOK:
		return &HTTPError{Status: httpResp.StatusCode, Excerpt: excerpt(raw)}
	}
	if resp != nil {
		return resolveAttachments(resp, attachments)
	}
	return nil
}

// redact drops the user info and query from an endpoint before it goes into
// an error message.
func redact(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "endpoint"
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return u.String()
}

// unwrapURLError keeps the cause of a transport failure and drops the
// *url.Error around it, whose message repeats the full URL.
func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
