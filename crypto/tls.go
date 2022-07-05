package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"time"

	"github.com/portainer/agent"
)

var TLS12CipherSuites = []uint16{
	tls.TLS_AES_128_GCM_SHA256,
	tls.TLS_AES_256_GCM_SHA384,
	tls.TLS_CHACHA20_POLY1305_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
	tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
}

// TLSService is a service used to generate TLS cert and key files
// to setup HTTPS.
type TLSService struct{}

// GenerateCertsForHost will generate a cert and key based on the specified host.
func (service *TLSService) GenerateCertsForHost(host string) error {

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber:          serialNumber,
		NotAfter:              time.Now().AddDate(1, 0, 0),
		NotBefore:             time.Now(),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	ip := net.ParseIP(host)
	template.DNSNames = append(template.DNSNames, "localhost")
	template.IPAddresses = append(template.IPAddresses, ip)

	keyPair, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	encodedCert, err := x509.CreateCertificate(rand.Reader, &template, &template, &keyPair.PublicKey, keyPair)
	if err != nil {
		return err
	}

	err = createPEMEncodedFile(agent.TLSCertPath, "CERTIFICATE", encodedCert)
	if err != nil {
		return err
	}

	return createPEMEncodedFile(agent.TLSKeyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(keyPair))
}

func createPEMEncodedFile(path, header string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	return pem.Encode(file, &pem.Block{Type: header, Bytes: data})
}
