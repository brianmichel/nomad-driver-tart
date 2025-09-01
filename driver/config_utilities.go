package driver

import "strings"

// CleanValue cleans the input value by converting it to lowercase and trimming whitespace.
func CleanValue(value string) string {
	value = strings.ToLower(value)
	value = strings.TrimSpace(value)
	return value
}
