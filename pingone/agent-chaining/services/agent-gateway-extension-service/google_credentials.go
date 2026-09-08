package main

import (
	"context"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// googleTokenSource mints Google-authenticated access tokens for the
// extension's own Cloud Run service account. This is separate from the
// PingOne delegated bearer: Google-hosted API surfaces (aiplatform.googleapis.com)
// enforce their own IAM check on Authorization independent of Agent Gateway
// policy, so native A2A targets need a Google credential on the outer call in
// addition to the PingOne token the downstream agent validates.
type googleTokenSource struct {
	mu sync.Mutex
	ts oauth2.TokenSource
}

func newGoogleTokenSource(ctx context.Context, scopes ...string) (*googleTokenSource, error) {
	ts, err := google.DefaultTokenSource(ctx, scopes...)
	if err != nil {
		return nil, err
	}
	return &googleTokenSource{ts: oauth2.ReuseTokenSource(nil, ts)}, nil
}

func (g *googleTokenSource) Token() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	tok, err := g.ts.Token()
	if err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}
