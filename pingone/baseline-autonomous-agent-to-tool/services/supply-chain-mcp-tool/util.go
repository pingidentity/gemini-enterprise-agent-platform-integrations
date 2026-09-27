package main

import (
	"log"
	"os"
)

// requireEnv returns the value of the given environment variable or fatally exits if unset.
func requireEnv(name string) string {
	val := os.Getenv(name)
	if val == "" {
		log.Fatalf("required environment variable %s is not set", name)
	}
	return val
}
