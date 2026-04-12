// Package crypto provides post-quantum cryptographic primitives for qBitcoin.
//
// SIMULATION NOTICE: This package simulates CRYSTALS-Kyber (KEM) and
// CRYSTALS-Dilithium (signatures) using SHA-512 and symmetric constructions.
// It is NOT cryptographically secure for production use. Replace with
// github.com/cloudflare/circl/kem/kyber and circl/sign/dilithium before
// any real-value deployment.
package crypto

import (
	"crypto/rand"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"math/big"
)

// hash32 computes a 32-byte digest using SHA-512/256 truncation.
// Named internally to avoid confusion with real BLAKE3.
func hash32(data []byte) []byte {
	h := sha512.New()
	h.Write(data)
	sum := h.Sum(nil)
	out := make([]byte, 32)
	copy(out, sum[:32])
	return out
}

// hashLabeled prepends a domain label before hashing, preventing cross-domain collisions.
func hashLabeled(label string, data ...[]byte) []byte {
	h := sha512.New()
	h.Write([]byte(label))
	for _, d := range data {
		h.Write(d)
	}
	sum := h.Sum(nil)
	out := make([]byte, 32)
	copy(out, sum[:32])
	return out
}

// BLAKE3Hash is an alias kept for API compatibility.
// In production replace with a real BLAKE3 implementation.
func BLAKE3Hash(data []byte) []byte {
	return hash32(data)
}

// BLAKE3HashHex returns the hex-encoded hash of data.
func BLAKE3HashHex(data []byte) string {
	return hex.EncodeToString(BLAKE3Hash(data))
}

// expandToSize deterministically fills a buffer of size n from a 32-byte seed.
func expandToSize(seed []byte, n int) []byte {
	out := make([]byte, n)
	for i := 0; i < n; i += 32 {
		block := hashLabeled("expand", seed, []byte{byte(i >> 8), byte(i)})
		copy(out[i:], block)
	}
	return out
}

// KyberKeyPair holds a simulated post-quantum key pair.
type KyberKeyPair struct {
	PublicKey  []byte
	PrivateKey []byte
}

const (
	KyberPublicKeySize  = 1184
	KyberPrivateKeySize = 2400
	KyberCiphertextSize = 1088
	KyberSharedKeySize  = 32
)

// Key layout (simulation):
//   privKey[0:32]  = secret seed
//   privKey[32:64] = signing key (sk)
//   privKey[64:]   = padding
//
//   pubKey[0:32]  = hashLabeled("pub", seed)        — public identity
//   pubKey[32:64] = sk                               — SIMULATION ONLY: stored for Verify
//   pubKey[64:]   = padding

// GenerateKyberKeyPair generates a simulated post-quantum key pair.
func GenerateKyberKeyPair() (*KyberKeyPair, error) {
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	sk := make([]byte, 32)
	if _, err := rand.Read(sk); err != nil {
		return nil, err
	}

	pubID := hashLabeled("pub", seed)

	priv := expandToSize(seed, KyberPrivateKeySize)
	copy(priv[0:32], seed)
	copy(priv[32:64], sk)

	pub := expandToSize(pubID, KyberPublicKeySize)
	copy(pub[0:32], pubID)
	copy(pub[32:64], sk) // simulation: stored so Verify can work

	return &KyberKeyPair{PublicKey: pub, PrivateKey: priv}, nil
}

// Encapsulate produces a ciphertext and shared secret from a public key.
// The shared secret matches what Decapsulate will derive from the corresponding private key.
func Encapsulate(publicKey []byte) (ciphertext, sharedSecret []byte, err error) {
	if len(publicKey) != KyberPublicKeySize {
		return nil, nil, errors.New("invalid public key size")
	}

	eph := make([]byte, 32)
	if _, err = rand.Read(eph); err != nil {
		return nil, nil, err
	}

	pubID := publicKey[:32]

	// Encrypt ephemeral: enc = eph XOR hashLabeled("enc", pubID)
	encKey := hashLabeled("enc", pubID)
	encEph := make([]byte, 32)
	for i := range encEph {
		encEph[i] = eph[i] ^ encKey[i]
	}

	ciphertext = expandToSize(hashLabeled("ct", eph, pubID), KyberCiphertextSize)
	copy(ciphertext[:32], encEph)

	sharedSecret = hashLabeled("ss", pubID, eph)
	return ciphertext, sharedSecret, nil
}

// Decapsulate recovers the shared secret from a ciphertext using the private key.
func Decapsulate(privateKey, ciphertext []byte) ([]byte, error) {
	if len(privateKey) != KyberPrivateKeySize {
		return nil, errors.New("invalid private key size")
	}
	if len(ciphertext) != KyberCiphertextSize {
		return nil, errors.New("invalid ciphertext size")
	}

	seed := privateKey[:32]
	pubID := hashLabeled("pub", seed)

	encKey := hashLabeled("enc", pubID)
	encEph := ciphertext[:32]

	eph := make([]byte, 32)
	for i := range eph {
		eph[i] = encEph[i] ^ encKey[i]
	}

	sharedSecret := hashLabeled("ss", pubID, eph)
	return sharedSecret, nil
}

// Sign produces a signature using the private key.
// SIMULATION: uses a MAC construction. Replace with Dilithium for production.
func Sign(privateKey, message []byte) ([]byte, error) {
	if len(privateKey) < 64 {
		return nil, errors.New("private key too short: need at least 64 bytes")
	}
	sk := privateKey[32:64]
	sigCore := hashLabeled("sign", sk, message)

	// Expand to 2420 bytes (simulated Dilithium3 signature size)
	sig := expandToSize(sigCore, 2420)
	copy(sig[:32], sigCore)
	return sig, nil
}

// Verify checks a signature against a public key and message.
// SIMULATION: uses the signing key stored in pubKey[32:64].
// In production Dilithium, only the public key polynomial is needed.
func Verify(publicKey, message, signature []byte) bool {
	if len(publicKey) < 64 || len(signature) < 32 {
		return false
	}
	sk := publicKey[32:64]
	expected := hashLabeled("sign", sk, message)
	return subtle.ConstantTimeCompare(expected, signature[:32]) == 1
}

// AddressFromPublicKey derives a qBitcoin address from a public key.
func AddressFromPublicKey(publicKey []byte) string {
	h := hashLabeled("addr", publicKey)
	return "qBTC" + hex.EncodeToString(h)[:40]
}

// RandomBigInt generates a cryptographically secure random big.Int < max.
func RandomBigInt(max *big.Int) (*big.Int, error) {
	return rand.Int(rand.Reader, max)
}
