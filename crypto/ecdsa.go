package crypto

import (
	"crypto/ecdsa"
	"crypto/md5"
	"crypto/x509"
	"encoding/hex"
	"math/big"

	"bitbucket.org/portainer/agent"
)

// ECDSAService is a service used to validate a digital signature.
// Use NewECDSAService to create a new service and ParsePublicKey to parse and
// associate the Portainer public key.
type ECDSAService struct {
	publicKey *ecdsa.PublicKey
}

// NewECDSAService returns a pointer to a ECDSAService.
func NewECDSAService() *ECDSAService {
	return &ECDSAService{}
}

// RequiresPublicKey returns true if a public key has not been associated to
// the service yet. It returns false if the service has a public key associated
// and therefore can be used to verify digital signatures.
func (service *ECDSAService) RequiresPublicKey() bool {
	if service.publicKey == nil {
		return true
	}
	return false
}

// ParsePublicKey decodes a hexadecimal encoded public key, parse the
// decoded DER data and associate the public key to the service.
func (service *ECDSAService) ParsePublicKey(key string) error {
	decodedKey, err := hex.DecodeString(key)
	if err != nil {
		return err
	}

	publicKey, err := x509.ParsePKIXPublicKey(decodedKey)
	if err != nil {
		return err
	}

	service.publicKey = publicKey.(*ecdsa.PublicKey)
	return nil
}

// ValidSignature returns true if the signature is valid.
func (service *ECDSAService) ValidSignature(signature []byte) bool {
	keySize := service.publicKey.Params().BitSize / 8

	if len(signature) != 2*keySize {
		return false
	}

	r := big.NewInt(0).SetBytes(signature[:keySize])
	s := big.NewInt(0).SetBytes(signature[keySize:])

	digest := md5.New()
	digest.Write([]byte(agent.PortainerAgentSignatureMessage))
	hash := digest.Sum(nil)

	return ecdsa.Verify(service.publicKey, []byte(hash), r, s)
}
