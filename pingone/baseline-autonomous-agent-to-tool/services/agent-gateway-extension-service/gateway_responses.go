// The Envoy ext_proc protocol surface: the recv/dispatch/send loop and the
// response builders. Generic to every ext_proc service — when this service is
// repurposed for a new tool or policy, this file does not change. The
// per-request policy (including the requestState definition) lives in
// request_policy.go.
package main

import (
	"fmt"
	"io"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprochttp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Process is the ext_proc stream handler: the gateway sends one phase message
// at a time (request headers, request body, response headers, response body)
// and each is answered with exactly one response. The per-request state
// (requestState — defined in request_policy.go, so it can carry whatever this
// use case needs) flows between the phases through the local here.
func (s *processor) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	var state requestState

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, "recv: %v", err)
		}

		var resp *extprocv3.ProcessingResponse
		switch v := req.Request.(type) {
		case *extprocv3.ProcessingRequest_RequestHeaders:
			resp, state = s.handleRequestHeadersPhase(stream.Context(), v.RequestHeaders, state)
		case *extprocv3.ProcessingRequest_RequestBody:
			resp = s.handleRequestBodyPhase(v, state)
		case *extprocv3.ProcessingRequest_ResponseHeaders:
			resp = ackResponseHeaders()
		case *extprocv3.ProcessingRequest_ResponseBody:
			resp = passResponseBodyThrough(v.ResponseBody)
		default:
			resp = &extprocv3.ProcessingResponse{}
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}

// passthroughHeaders answers the header phase with no changes: the request
// continues to its destination as received. Used for non-governed traffic.
func passthroughHeaders() *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_RequestHeaders{RequestHeaders: &extprocv3.HeadersResponse{}},
	}
}

// bearerAuthHeader is an Authorization: Bearer <token> replacement.
func bearerAuthHeader(token string) *corev3.HeaderValueOption {
	return &corev3.HeaderValueOption{
		AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
		Header:       &corev3.HeaderValue{Key: "Authorization", RawValue: []byte("Bearer " + token)},
	}
}

// plainHeader sets one header to a value, overwriting any existing one.
func plainHeader(key, value string) *corev3.HeaderValueOption {
	return &corev3.HeaderValueOption{
		AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
		Header:       &corev3.HeaderValue{Key: key, RawValue: []byte(value)},
	}
}

// setHeadersAndRequestBody answers the header phase by replacing headers and
// requesting the buffered body for the Authorize check. The swap must happen
// here — header mutations are dropped in body-phase responses; a body-phase
// DENY still stops the request before it reaches the tool.
func setHeadersAndRequestBody(headers []*corev3.HeaderValueOption) *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_RequestHeaders{
			RequestHeaders: &extprocv3.HeadersResponse{
				Response: &extprocv3.CommonResponse{
					HeaderMutation: &extprocv3.HeaderMutation{SetHeaders: headers},
				},
			},
		},
		ModeOverride: &extprochttp.ProcessingMode{
			RequestBodyMode: extprochttp.ProcessingMode_BUFFERED,
		},
	}
}

// replyUnauthorized stops the request with 401: the bearer was missing or
// failed validation. WWW-Authenticate marks it as an OAuth token problem.
func replyUnauthorized(description string) *extprocv3.ProcessingResponse {
	return buildImmediateResponse(typev3.StatusCode_Unauthorized, "unauthorized", description,
		&corev3.HeaderValueOption{Header: &corev3.HeaderValue{Key: "www-authenticate", RawValue: []byte(`Bearer error="invalid_token"`)}})
}

// replyForbidden stops the request with 403 — token exchange or the PingOne
// Authorize policy said no.
func replyForbidden(description string) *extprocv3.ProcessingResponse {
	return buildImmediateResponse(typev3.StatusCode_Forbidden, "access_denied", description)
}

// buildImmediateResponse builds the ext_proc ImmediateResponse: the gateway
// returns this status/body straight to the caller instead of forwarding the
// request. The body is an RFC 6749-style error object; extra headers
// (e.g. WWW-Authenticate) ride along.
func buildImmediateResponse(code typev3.StatusCode, errorCode, description string, extra ...*corev3.HeaderValueOption) *extprocv3.ProcessingResponse {
	headers := append([]*corev3.HeaderValueOption{
		{Header: &corev3.HeaderValue{Key: "content-type", RawValue: []byte("application/json")}},
	}, extra...)
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ImmediateResponse{
			ImmediateResponse: &extprocv3.ImmediateResponse{
				Status:  &typev3.HttpStatus{Code: code},
				Headers: &extprocv3.HeaderMutation{SetHeaders: headers},
				Body:    fmt.Appendf(nil, `{"error":%q,"error_description":%q}`, errorCode, description),
				Details: errorCode,
			},
		},
	}
}

// passRequestBodyThrough answers the body phase with the request body
// unchanged — for bodies that need no policy check, and as the PERMIT outcome.
func passRequestBodyThrough(b *extprocv3.HttpBody) *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_RequestBody{
			RequestBody: &extprocv3.BodyResponse{Response: passBodyThrough(b)},
		},
	}
}

// replaceRequestBody substitutes the full request body with new content, when
// the body phase must change what the target receives (e.g. injecting a token
// into the body's metadata). Same StreamedResponse shape as passBodyThrough —
// required by the CONTENT_AUTHZ profile — plus an explicit Content-Length,
// since the replace can change the byte length.
func replaceRequestBody(body []byte, endOfStream bool) *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_RequestBody{
			RequestBody: &extprocv3.BodyResponse{
				Response: &extprocv3.CommonResponse{
					HeaderMutation: &extprocv3.HeaderMutation{
						SetHeaders: []*corev3.HeaderValueOption{
							plainHeader("content-length", fmt.Sprintf("%d", len(body))),
						},
					},
					BodyMutation: &extprocv3.BodyMutation{
						Mutation: &extprocv3.BodyMutation_StreamedResponse{
							StreamedResponse: &extprocv3.StreamedBodyResponse{Body: body, EndOfStream: endOfStream},
						},
					},
				},
			},
		},
	}
}

// passResponseBodyThrough answers the response-body phase with the tool's
// reply unchanged.
func passResponseBodyThrough(b *extprocv3.HttpBody) *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ResponseBody{
			ResponseBody: &extprocv3.BodyResponse{Response: passBodyThrough(b)},
		},
	}
}

// passBodyThrough copies the body chunk the gateway just sent, byte for byte.
// The StreamedResponse shape is required by this gateway's CONTENT_AUTHZ
// profile — a plain Body replacement silently corrupts bodies.
func passBodyThrough(b *extprocv3.HttpBody) *extprocv3.CommonResponse {
	return &extprocv3.CommonResponse{
		BodyMutation: &extprocv3.BodyMutation{
			Mutation: &extprocv3.BodyMutation_StreamedResponse{
				StreamedResponse: &extprocv3.StreamedBodyResponse{Body: b.Body, EndOfStream: b.EndOfStream},
			},
		},
	}
}

// ackResponseHeaders answers the response-header phase with no changes: the
// tool's reply headers continue back to the agent untouched.
func ackResponseHeaders() *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ResponseHeaders{ResponseHeaders: &extprocv3.HeadersResponse{}},
	}
}
