// Container trust policy for the October 2026 eMudhra CA compromise.
// Sources: Mozilla dev-security-policy incident notice;
// https://access.redhat.com/security/vulnerabilities/RHSB-2026-012
// https://linux.oracle.com/errata/ELSA-2026-500449.html
package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

func blocked(cert *x509.Certificate) bool {
	switch cert.Subject.CommonName {
	case "emSign ECC Root CA - G3", "emSign Root TLS CA - G3", "AAA Certificate Services":
		return true
	}
	return false
}

func checkFile(path string, prune bool) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	count := 0
	for {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}
		data = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return count, fmt.Errorf("%s: %w", path, err)
		}
		count++
		if blocked(cert) {
			if !prune {
				return count, fmt.Errorf("blocked CA still trusted: %s in %s", cert.Subject.CommonName, path)
			}
			fmt.Printf("Removing trust source: %s (%s)\n", path, cert.Subject.CommonName)
			return 0, os.Remove(path)
		}
	}
	return count, nil
}

func run(args []string) error {
	if len(args) < 2 || (args[0] != "prune" && args[0] != "verify") {
		return fmt.Errorf("usage: container-trust prune|verify DIRECTORY...")
	}
	prune := args[0] == "prune"
	count := 0
	for _, root := range args[1:] {
		if _, err := os.Stat(root); os.IsNotExist(err) && prune {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			// Follow certificate file symlinks, as Go's Linux root loader does.
			if prune && filepath.Ext(path) != ".crt" {
				return nil
			}
			n, err := checkFile(path, prune)
			count += n
			return err
		})
		if err != nil {
			return err
		}
	}
	if !prune && count == 0 {
		return fmt.Errorf("refusing empty certificate trust store")
	}
	fmt.Printf("Container trust %s passed (%d certificate entries checked)\n", args[0], count)
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
