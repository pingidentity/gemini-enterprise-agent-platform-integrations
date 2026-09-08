package main

import (
	"fmt"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprochttp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_proc/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
)

func passthroughHeaders() *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_RequestHeaders{RequestHeaders: &extprocv3.HeadersResponse{}},
	}
}

// injectAuthAndRequestBody replaces the bearer token and requests a buffered body.
func injectAuthAndRequestBody(tok string) *extprocv3.ProcessingResponse {
	headers := []*corev3.HeaderValueOption{
		{
			AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
			Header:       &corev3.HeaderValue{Key: "Authorization", RawValue: []byte("Bearer " + tok)},
		},
	}
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

// injectGoogleAuth replaces Authorization with a Google-authenticated token
// for the outer call to a Google-hosted API surface (aiplatform.googleapis.com
// enforces its own IAM check on Authorization independent of gateway policy).
// The PingOne delegated bearer the caller validated travels in the request
// body instead of a header, since a custom header added here was observed to
// not reach the downstream agent — one of Envoy's ext_proc mutation policy or
// header-allowlisting somewhere between the gateway and the target dropped it.
func injectGoogleAuth(googleToken string) *extprocv3.ProcessingResponse {
	headers := []*corev3.HeaderValueOption{
		{
			AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
			Header:       &corev3.HeaderValue{Key: "Authorization", RawValue: []byte("Bearer " + googleToken)},
		},
	}
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

func denyUnauthorized(description string) *extprocv3.ProcessingResponse {
	return immediateJSON(typev3.StatusCode_Unauthorized, "unauthorized", description,
		&corev3.HeaderValueOption{Header: &corev3.HeaderValue{Key: "www-authenticate", RawValue: []byte(`Bearer error="invalid_token"`)}})
}

func denyForbidden(description string) *extprocv3.ProcessingResponse {
	return immediateJSON(typev3.StatusCode_Forbidden, "access_denied", description)
}

func immediateJSON(code typev3.StatusCode, errCode, description string, extra ...*corev3.HeaderValueOption) *extprocv3.ProcessingResponse {
	headers := append([]*corev3.HeaderValueOption{
		{Header: &corev3.HeaderValue{Key: "content-type", RawValue: []byte("application/json")}},
	}, extra...)
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ImmediateResponse{
			ImmediateResponse: &extprocv3.ImmediateResponse{
				Status:  &typev3.HttpStatus{Code: code},
				Headers: &extprocv3.HeaderMutation{SetHeaders: headers},
				Body:    []byte(fmt.Sprintf(`{"error":%q,"error_description":%q}`, errCode, description)),
				Details: errCode,
			},
		},
	}
}

func echoRequestBody(b *extprocv3.HttpBody) *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_RequestBody{
			RequestBody: &extprocv3.BodyResponse{Response: echoBody(b)},
		},
	}
}

// replaceRequestBody substitutes the full request body. Used when the
// gateway remints a delegated token into the body's metadata rather than a
// header, for targets where a custom header was observed not to survive.
// Uses the same StreamedResponse body-mutation shape as echoBody (proven to
// work against this gateway's full-duplex-streamed CONTENT_AUTHZ profile),
// with the mutated content instead of a passthrough, plus an explicit
// Content-Length since a full-body replace can change the byte length.
func replaceRequestBody(body []byte, endOfStream bool) *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_RequestBody{
			RequestBody: &extprocv3.BodyResponse{
				Response: &extprocv3.CommonResponse{
					HeaderMutation: &extprocv3.HeaderMutation{
						SetHeaders: []*corev3.HeaderValueOption{
							{
								AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
								Header:       &corev3.HeaderValue{Key: "content-length", RawValue: []byte(fmt.Sprintf("%d", len(body)))},
							},
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

func echoResponseBody(b *extprocv3.HttpBody) *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ResponseBody{
			ResponseBody: &extprocv3.BodyResponse{Response: echoBody(b)},
		},
	}
}

func echoBody(b *extprocv3.HttpBody) *extprocv3.CommonResponse {
	return &extprocv3.CommonResponse{
		BodyMutation: &extprocv3.BodyMutation{
			Mutation: &extprocv3.BodyMutation_StreamedResponse{
				StreamedResponse: &extprocv3.StreamedBodyResponse{Body: b.Body, EndOfStream: b.EndOfStream},
			},
		},
	}
}

func ackResponseHeaders() *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ResponseHeaders{ResponseHeaders: &extprocv3.HeadersResponse{}},
	}
}
