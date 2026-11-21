package types

import "strings"

type ShirtSizeOption struct {
	Code   string
	Label  string
	Legacy bool
}

var canonicalShirtSizes = []ShirtSizeOption{
	{Code: "LS", Label: "S"},
	{Code: "LM", Label: "M"},
	{Code: "LL", Label: "L"},
	{Code: "MS", Label: "S"},
	{Code: "MM", Label: "M"},
	{Code: "ML", Label: "L"},
	{Code: "MXL", Label: "XL"},
	{Code: "MXXL", Label: "XXL"},
	{Code: "MXXXL", Label: "XXXL", Legacy: true},
}

// Keep storage codes compatible while offering five simple size labels.
// A hidden legacy option preserves an existing XXXL selection on save.
func ShirtSizeOptions(current ...string) []ShirtSizeOption {
	options := append([]ShirtSizeOption(nil), canonicalShirtSizes[3:8]...)
	for _, value := range current {
		if ValidShirtSizeCode(value) == "MXXXL" {
			return append(options, canonicalShirtSizes[8])
		}
	}
	return options
}

func ShirtSizeChoiceCode(value string) string {
	switch value = ValidShirtSizeCode(value); value {
	case "LS":
		return "MS"
	case "LM":
		return "MM"
	case "LL":
		return "ML"
	default:
		return value
	}
}

func ValidShirtSizeCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	for _, option := range canonicalShirtSizes {
		if value == option.Code {
			return option.Code
		}
	}
	return ""
}

func ShirtSizeLabel(value string) string {
	value = ValidShirtSizeCode(value)
	for _, option := range canonicalShirtSizes {
		if value == option.Code {
			return option.Label
		}
	}
	return ""
}
