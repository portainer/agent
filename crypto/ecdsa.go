package crypto

import (
	"crypto/ecdsa"
	"crypto/md5"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"math/big"

	"github.com/portainer/agent"
)

// ECDSAService is a service used to validate a digital signature.
// An optional secret can be associated to this service
type ECDSAService struct {
	publicKey *ecdsa.PublicKey
	secret    string
}

// NewECDSAService returns a pointer to a ECDSAService.
// An optional secret can be specified
func NewECDSAService(secret string) *ECDSAService {
	return &ECDSAService{
		secret: secret,
	}
}

// IsAssociated tells if the service is associated with a public key
// or if it's secured behind  a secret
func (service *ECDSAService) IsAssociated() bool {
	return service.publicKey != nil || service.secret != ""
}

// VerifySignature is used to verify a digital signature using a specified public
// key. The public key specified as a parameter must be hexadecimal encoded.
// The public key will be decoded and parsed as DER data. If the service is not
// using a secret, the public key will be associated to the service so that
// only signatures associated to this key will be considered valid.
// When a secret is associated to the service, the specified key will be
// decoded and parsed each time.
// NOTE: this could have an impact on performance.
// After parsing the public key, it will decode the signature (base64 encoded)
// and verify the signature based on the secret associated to the service
// or using the default signature if no secret is specified.
func (service *ECDSAService) VerifySignature(signature, key string) (bool, error) {
	publicKey, err := service.decodeAndParsePublicKey(key)
	if err != nil {
		return false, err
	}

	return service.decodeAndVerifySignature(signature, publicKey)
}

func (service *ECDSAService) decodeAndParsePublicKey(key string) (*ecdsa.PublicKey, error) {
	if service.publicKey != nil {
		return service.publicKey, nil
	}

	decodedKey, err := hex.DecodeString(key)
	if err != nil {
		return nil, err
	}

	publicKey, err := x509.ParsePKIXPublicKey(decodedKey)
	if err != nil {
		return nil, err
	}

	if service.secret == "" {
		service.publicKey = publicKey.(*ecdsa.PublicKey)
	}

	return publicKey.(*ecdsa.PublicKey), nil
}

func (service *ECDSAService) decodeAndVerifySignature(signature string, publicKey *ecdsa.PublicKey) (bool, error) {
	decodedSignature, err := base64.RawStdEncoding.DecodeString(signature)
	if err != nil {
		return false, err
	}

	keySize := publicKey.Params().BitSize / 8

	if len(decodedSignature) != 2*keySize {
		return false, nil
	}

	r := big.NewInt(0).SetBytes(decodedSignature[:keySize])
	s := big.NewInt(0).SetBytes(decodedSignature[keySize:])

	validSignature := agent.PortainerAgentSignatureMessage
	if service.secret != "" {
		validSignature = service.secret
	}

	digest := md5.New()
	digest.Write([]byte(validSignature))
	hash := digest.Sum(nil)

	valid := ecdsa.Verify(publicKey, []byte(hash), r, s)

	return valid, nil
}
