// Package crypto provides post-quantum cryptographic primitives for qBitcoin.
//
// Uses real CRYSTALS-Kyber-768 (KEM) via github.com/cloudflare/circl/kem/kyber/kyber768
// and real CRYSTALS-Dilithium3 (signatures) via github.com/cloudflare/circl/sign/dilithium.
package crypto

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"math/big"

	"github.com/cloudflare/circl/kem/kyber/kyber768"
	"github.com/cloudflare/circl/sign/dilithium"
)

// TODO: replace with real BLAKE3 (e.g. github.com/zeebo/blake3) for production.
func hash32(data []byte) []byte {
	h := sha512.New512_256()
	h.Write(data)
	return h.Sum(nil)
}

// BLAKE3Hash is kept for API compatibility. Uses SHA-512/256 internally.
func BLAKE3Hash(data []byte) []byte { return hash32(data) }

// BLAKE3HashHex returns the hex-encoded hash of data.
func BLAKE3HashHex(data []byte) string { return hex.EncodeToString(BLAKE3Hash(data)) }

// KyberKeyPair holds a composite post-quantum key pair.
// Layout:
//   PublicKey  = kyber768_pub  (1184 B) || dilithium3_pub
//   PrivateKey = kyber768_priv (2400 B) || dilithium3_priv
type KyberKeyPair struct {
	PublicKey  []byte
	PrivateKey []byte
}

const (
	KyberPublicKeySize  = kyber768.PublicKeySize  // 1184
	KyberPrivateKeySize = kyber768.PrivateKeySize // 2400
	KyberCiphertextSize = kyber768.CiphertextSize // 1088
	KyberSharedKeySize  = kyber768.SharedKeySize  // 32
)

var (
	kemScheme     = kyber768.Scheme()
	dilithiumMode = dilithium.ModeByName("Dilithium3")

	// CompositePublicKeySize = KyberPublicKeySize + Dilithium3 public key size.
	CompositePublicKeySize = KyberPublicKeySize + dilithium.ModeByName("Dilithium3").PublicKeySize()
	// CompositePrivateKeySize = KyberPrivateKeySize + Dilithium3 private key size.
	CompositePrivateKeySize = KyberPrivateKeySize + dilithium.ModeByName("Dilithium3").PrivateKeySize()
)

// GenerateKyberKeyPair generates a real Kyber-768 + Dilithium3 key pair.
func GenerateKyberKeyPair() (*KyberKeyPair, error) {
	// KEM keys
	kemPub, kemPriv, err := kyber768.GenerateKeyPair(rand.Reader)
	if err != nil {
		return nil, err
	}
	kemPubBytes, err := kemPub.MarshalBinary()
	if err != nil {
		return nil, err
	}
	kemPrivBytes, err := kemPriv.MarshalBinary()
	if err != nil {
		return nil, err
	}

	// Signature keys
	sigPub, sigPriv, err := dilithiumMode.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	sigPubBytes := sigPub.Bytes()
	sigPrivBytes := sigPriv.Bytes()

	pub := make([]byte, len(kemPubBytes)+len(sigPubBytes))
	copy(pub, kemPubBytes)
	copy(pub[len(kemPubBytes):], sigPubBytes)

	priv := make([]byte, len(kemPrivBytes)+len(sigPrivBytes))
	copy(priv, kemPrivBytes)
	copy(priv[len(kemPrivBytes):], sigPrivBytes)

	return &KyberKeyPair{PublicKey: pub, PrivateKey: priv}, nil
}

// Encapsulate produces a real Kyber-768 ciphertext and shared secret.
func Encapsulate(publicKey []byte) (ciphertext, sharedSecret []byte, err error) {
	if len(publicKey) < KyberPublicKeySize {
		return nil, nil, errors.New("invalid public key size")
	}
	kemPubKey, err := kemScheme.UnmarshalBinaryPublicKey(publicKey[:KyberPublicKeySize])
	if err != nil {
		return nil, nil, err
	}
	ciphertext, sharedSecret, err = kemScheme.Encapsulate(kemPubKey)
	return
}

// Decapsulate recovers the shared secret from a Kyber-768 ciphertext.
func Decapsulate(privateKey, ciphertext []byte) ([]byte, error) {
	if len(privateKey) < KyberPrivateKeySize {
		return nil, errors.New("invalid private key size")
	}
	if len(ciphertext) != KyberCiphertextSize {
		return nil, errors.New("invalid ciphertext size")
	}
	kemPrivKey, err := kemScheme.UnmarshalBinaryPrivateKey(privateKey[:KyberPrivateKeySize])
	if err != nil {
		return nil, err
	}
	return kemScheme.Decapsulate(kemPrivKey, ciphertext)
}

// Sign produces a real Dilithium3 signature.
func Sign(privateKey, message []byte) ([]byte, error) {
	if len(privateKey) <= KyberPrivateKeySize {
		return nil, errors.New("private key too short: missing Dilithium component")
	}
	sigPriv := dilithiumMode.PrivateKeyFromBytes(privateKey[KyberPrivateKeySize:])
	return dilithiumMode.Sign(sigPriv, message), nil
}

// Verify checks a real Dilithium3 signature.
func Verify(publicKey, message, signature []byte) bool {
	if len(publicKey) <= KyberPublicKeySize {
		return false
	}
	sigPub := dilithiumMode.PublicKeyFromBytes(publicKey[KyberPublicKeySize:])
	return dilithiumMode.Verify(sigPub, message, signature)
}

// AddressFromPublicKey derives a qBitcoin address from a public key.
func AddressFromPublicKey(publicKey []byte) string {
	h := hash32(append([]byte("addr"), publicKey...))
	return "qBTC" + hex.EncodeToString(h)[:40]
}

// RandomBigInt generates a cryptographically secure random big.Int < max.
func RandomBigInt(max *big.Int) (*big.Int, error) {
	return rand.Int(rand.Reader, max)
}
