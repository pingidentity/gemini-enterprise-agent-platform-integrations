package main

import (
	"context"
	"log"
	"os"
)

const ctxKeyCallerEmail = "caller-email"

// requireEnv returns the value of the given environment variable or fatally exits if unset.
func requireEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return val
}

// callerEmailFromCtx returns the caller email the middleware put in the request
// context ("" for tool-discovery requests, which carry no X-User-Email).
func callerEmailFromCtx(ctx context.Context) string {
	email, _ := ctx.Value(ctxKeyCallerEmail).(string)
	return email
}
