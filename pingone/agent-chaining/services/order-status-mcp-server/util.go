package main

import (
	"context"
	"log"
	"os"
	"regexp"
)

var orderIDPattern = regexp.MustCompile(`^ORD-[0-9]+$`)

type ctxKeyCallerSub struct{}

// requireEnv returns the value of the given environment variable or fatally exits if unset.
func requireEnv(name string) string {
	val := os.Getenv(name)
	if val == "" {
		log.Fatalf("required environment variable %s is not set", name)
	}
	return val
}

// callerSubFromCtx returns the caller sub the middleware put in the request
// context.
func callerSubFromCtx(ctx context.Context) string {
	caller, _ := ctx.Value(ctxKeyCallerSub{}).(string)
	return caller
}

func validOrderID(value string) bool {
	return orderIDPattern.MatchString(value)
}
