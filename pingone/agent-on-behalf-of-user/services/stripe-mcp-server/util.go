package main

import (
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

