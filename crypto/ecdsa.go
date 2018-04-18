package crypto

import (
	"crypto/ecdsa"
	"crypto/md5"
	"crypto/x509"
	"encoding/hex"
	"fmt"
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
func (service *ECDSAService) ValidSignature(signature string) bool {
	sign, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}

	r := new(big.Int).SetBytes(sign[:len(sign)/2])
	s := new(big.Int).SetBytes(sign[len(sign)/2:])
	hash := fmt.Sprintf("%x", md5.Sum([]byte(agent.PortainerAgentSignatureMessage)))

	return ecdsa.Verify(service.publicKey, []byte(hash), r, s)
}
