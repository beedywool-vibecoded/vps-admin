// Command release-sign signs SHA256SUMS for the release workflow.
//
//	go run ./tools/release-sign -keygen            prints a new private key, then the public key
//	RELEASE_SIGNING_KEY=... go run ./tools/release-sign SHA256SUMS > SHA256SUMS.sig
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	keygen := flag.Bool("keygen", false, "generate a key pair")
	flag.Parse()
	if *keygen {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fail(err)
		}
		fmt.Println("private:", base64.StdEncoding.EncodeToString(priv.Seed()))
		fmt.Println("public: ", base64.StdEncoding.EncodeToString(pub))
		return
	}
	if flag.NArg() != 1 {
		fail(fmt.Errorf("usage: release-sign FILE"))
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("RELEASE_SIGNING_KEY")))
	if err != nil || len(seed) != ed25519.SeedSize {
		fail(fmt.Errorf("RELEASE_SIGNING_KEY must hold a base64 ed25519 seed"))
	}
	data, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fail(err)
	}
	fmt.Println(base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(seed), data)))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "release-sign:", err)
	os.Exit(1)
}
