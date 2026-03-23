package main

import (
	"crypto/ed25519"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"backend/cmd/kubectl-cybermesh/release/signutil"
)

func main() {
	var (
		mode        = flag.String("mode", "", "sign or verify")
		artifacts   = flag.String("artifacts-dir", "", "artifact directory containing SHA256SUMS")
		privateKey  = flag.String("private-key", "", "ed25519 private key PEM path for signing")
		publicKey   = flag.String("public-key", "", "ed25519 public key PEM path for verification")
		writePubKey = flag.String("write-public-key", "", "optional path to write the derived public key PEM during signing")
	)
	flag.Parse()

	switch *mode {
	case "keygen":
		if *privateKey == "" || *publicKey == "" {
			exitf("private-key and public-key are required for keygen mode")
		}
		pub, priv, err := signutil.GenerateKeypair()
		if err != nil {
			exitf("generate keypair: %v", err)
		}
		privPEM, err := signutil.MarshalPrivateKeyPEM(priv)
		if err != nil {
			exitf("marshal private key: %v", err)
		}
		pubPEM, err := signutil.MarshalPublicKeyPEM(pub)
		if err != nil {
			exitf("marshal public key: %v", err)
		}
		if err := os.WriteFile(*privateKey, privPEM, 0o600); err != nil {
			exitf("write private key: %v", err)
		}
		if err := os.WriteFile(*publicKey, pubPEM, 0o644); err != nil {
			exitf("write public key: %v", err)
		}
		fmt.Fprintf(os.Stdout, "generated keypair\n")
	case "sign":
		if *artifacts == "" {
			exitf("artifacts-dir is required")
		}
		if *privateKey == "" {
			exitf("private-key is required for sign mode")
		}
		checksumsPath := filepath.Join(*artifacts, "SHA256SUMS")
		signaturePath := filepath.Join(*artifacts, "SHA256SUMS.sig")
		priv, err := signutil.LoadPrivateKeyPEM(*privateKey)
		if err != nil {
			exitf("load private key: %v", err)
		}
		sig, err := signutil.SignFile(checksumsPath, priv)
		if err != nil {
			exitf("sign checksums: %v", err)
		}
		if err := os.WriteFile(signaturePath, append(signutil.EncodeSignature(sig), '\n'), 0o600); err != nil {
			exitf("write signature: %v", err)
		}
		if *writePubKey != "" {
			edPub, ok := priv.Public().(ed25519.PublicKey)
			if !ok {
				exitf("derived public key is not ed25519")
			}
			publicPEM, err := signutil.MarshalPublicKeyPEM(edPub)
			if err != nil {
				exitf("marshal public key: %v", err)
			}
			if err := os.WriteFile(*writePubKey, publicPEM, 0o644); err != nil {
				exitf("write public key: %v", err)
			}
		}
		fmt.Fprintf(os.Stdout, "signed %s\n", checksumsPath)
	case "verify":
		if *artifacts == "" {
			exitf("artifacts-dir is required")
		}
		if *publicKey == "" {
			exitf("public-key is required for verify mode")
		}
		checksumsPath := filepath.Join(*artifacts, "SHA256SUMS")
		signaturePath := filepath.Join(*artifacts, "SHA256SUMS.sig")
		pub, err := signutil.LoadPublicKeyPEM(*publicKey)
		if err != nil {
			exitf("load public key: %v", err)
		}
		sigData, err := os.ReadFile(signaturePath)
		if err != nil {
			exitf("read signature: %v", err)
		}
		sig, err := signutil.DecodeSignature(sigData)
		if err != nil {
			exitf("decode signature: %v", err)
		}
		if err := signutil.VerifyFile(checksumsPath, pub, sig); err != nil {
			exitf("verify signature: %v", err)
		}
		fmt.Fprintf(os.Stdout, "verified %s\n", checksumsPath)
	default:
		exitf("mode must be keygen, sign, or verify")
	}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
