// signrelease signs release files with the Hestia release key, and can
// generate a new key pair.
//
//	signrelease -gen                      prints a new key pair
//	signrelease -key-env NAME file.txt    writes file.txt.sig (key from $NAME)
//	signrelease -verify PUBKEY file.txt   checks file.txt.sig
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
)

// signaturePrefix stops a signature made for releases being reused for
// anything else that might be signed with the same key one day.
const signaturePrefix = "hestia-release-v1\n"

func main() {
	gen := flag.Bool("gen", false, "generate a new key pair")
	keyEnv := flag.String("key-env", "HESTIA_SIGNING_KEY", "environment variable holding the private key (base64)")
	verify := flag.String("verify", "", "public key (base64) to verify FILE.sig against")
	flag.Parse()

	if *gen {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fail(err)
		}
		fmt.Printf("public key (embed in the app):\n%s\n\nprivate key (GitHub secret HESTIA_SIGNING_KEY, never commit it):\n%s\n",
			base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv.Seed()))
		return
	}
	if flag.NArg() != 1 {
		fail(fmt.Errorf("usage: signrelease [-gen] [-verify PUBKEY] FILE"))
	}
	file := flag.Arg(0)
	data, err := os.ReadFile(file)
	if err != nil {
		fail(err)
	}
	msg := append([]byte(signaturePrefix), data...)

	if *verify != "" {
		pub, err := base64.StdEncoding.DecodeString(*verify)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			fail(fmt.Errorf("bad public key"))
		}
		sigB64, err := os.ReadFile(file + ".sig")
		if err != nil {
			fail(err)
		}
		sig, err := base64.StdEncoding.DecodeString(string(sigB64))
		if err != nil || !ed25519.Verify(ed25519.PublicKey(pub), msg, sig) {
			fail(fmt.Errorf("signature does NOT verify"))
		}
		fmt.Println("signature OK")
		return
	}

	seed, err := base64.StdEncoding.DecodeString(os.Getenv(*keyEnv))
	if err != nil || len(seed) != ed25519.SeedSize {
		fail(fmt.Errorf("%s is not set to a valid signing key", *keyEnv))
	}
	priv := ed25519.NewKeyFromSeed(seed)
	sig := ed25519.Sign(priv, msg)
	if err := os.WriteFile(file+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		fail(err)
	}
	fmt.Println("signed", file)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "signrelease:", err)
	os.Exit(1)
}
