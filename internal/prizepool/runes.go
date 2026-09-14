package prizepool

import "encoding/json"

// RuneCommands creates new, independent service runes using the node's local
// CLI. Each inner array is an OR of exact method matches; no spending or rune
// creation permission is delegated to the website.
func RuneCommands() (monitor, provision string) {
	command := func(methods ...string) string {
		alternatives := make([]string, len(methods))
		for i, method := range methods {
			alternatives[i] = "method=" + method
		}
		restrictions, _ := json.Marshal([][]string{alternatives})
		return "lightning-cli createrune -k restrictions='" + string(restrictions) + "'"
	}
	return command("getinfo", "waitanyinvoice", "listinvoices"),
		command("getinfo", "offer", "disableoffer", "clnurl-list", "clnurl-add", "clnurl-remove")
}
