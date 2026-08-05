package types

import "strings"

type ShirtSizeOption struct {
	Code  string
	Label string
}

var canonicalShirtSizes = []ShirtSizeOption{
	{Code: "LS", Label: "Ladies Small"},
	{Code: "LM", Label: "Ladies Medium"},
	{Code: "LL", Label: "Ladies Large"},
	{Code: "MS", Label: "Men's Small"},
	{Code: "MM", Label: "Men's Medium"},
	{Code: "ML", Label: "Men's Large"},
	{Code: "MXL", Label: "Men's XL"},
	{Code: "MXXL", Label: "Men's XXL"},
	{Code: "MXXXL", Label: "Men's XXXL"},
}

func ShirtSizeOptions() []ShirtSizeOption {
	return append([]ShirtSizeOption(nil), canonicalShirtSizes...)
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
