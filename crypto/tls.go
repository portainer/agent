package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"time"
)

type TLSService struct{}

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
	template.IPAddresses = append(template.IPAddresses, ip)

	keyPair, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	encodedCert, err := x509.CreateCertificate(rand.Reader, &template, &template, &keyPair.PublicKey, keyPair)
	if err != nil {
		return err
	}

	certFile, err := os.Create("cert.pem")
	if err != nil {
		return err
	}

	err = pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: encodedCert})
	if err != nil {
		return err
	}
	certFile.Close()

	keyFile, err := os.OpenFile("key.pem", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	err = pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(keyPair)})
	if err != nil {
		return err
	}
	keyFile.Close()

	return nil
}
