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

// injectAuthAndEmailAndRequestBody injects the tool token as Authorization and
// the resolved user email as X-User-Email in the header phase, then requests
// the body (BUFFERED) for the downstream Authorize check. Header mutations must
// happen here — in FULL_DUPLEX_STREAMED mode, header mutations in body-phase
// responses are silently dropped.
func injectAuthAndEmailAndRequestBody(tok, email string) *extprocv3.ProcessingResponse {
	headers := []*corev3.HeaderValueOption{
		{
			AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
			Header:       &corev3.HeaderValue{Key: "Authorization", RawValue: []byte("Bearer " + tok)},
		},
	}
	if email != "" {
		headers = append(headers, &corev3.HeaderValueOption{
			AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
			Header:       &corev3.HeaderValue{Key: "X-User-Email", RawValue: []byte(email)},
		})
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
