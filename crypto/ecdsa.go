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

type ECDSAService struct {
	publicKey *ecdsa.PublicKey
}

func NewECDSAService() *ECDSAService {
	return &ECDSAService{}
}

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
