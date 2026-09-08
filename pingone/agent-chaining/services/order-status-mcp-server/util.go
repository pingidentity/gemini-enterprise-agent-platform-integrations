package main

import (
	"fmt"
	"os"
	"regexp"
)

var orderIDPattern = regexp.MustCompile(`^ORD-[0-9]+$`)

func requireEnv(name string) (string, error) {
	value := getenv(name)
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func getenv(name string) string {
	return os.Getenv(name)
}

func validOrderID(value string) bool {
	return orderIDPattern.MatchString(value)
}
