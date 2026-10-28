// Command notifycat-keygen generates the admin key and the bcrypt hash the
// server stores.
//
// Usage:
//
//	notifycat-keygen
//
// The key goes to stdout; the NOTIFYCAT_ADMIN_KEY_HASH line goes to stderr.
// Keep the single quotes around the hash: it contains `$`, which an unquoted
// .env or shell value expands.
package main

import (
	"fmt"
	"io"
	"os"

	accessdomain "github.com/mptooling/notifycat/internal/access/domain"
	accessinfra "github.com/mptooling/notifycat/internal/access/infrastructure"
)

func main() {
	os.Exit(run(os.Stdout, os.Stderr, accessinfra.NewRandomSecret(), accessinfra.NewBcryptHasher()))
}

func run(stdout, stderr io.Writer, secrets accessdomain.SecretSource, hasher accessdomain.Hasher) int {
	secret, err := secrets.NewSecret()
	if err != nil {
		return fail(stderr, err)
	}
	adminKey := accessdomain.AdminKeyPrefix + secret
	adminKeyHash, err := hasher.Hash(adminKey)
	if err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "%s\n", adminKey)
	fmt.Fprintf(stderr, "NOTIFYCAT_ADMIN_KEY_HASH='%s'\n", adminKeyHash)
	return 0
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, "notifycat-keygen:", err)
	return 1
}
