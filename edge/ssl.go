package edge

import (
	"log"
	"os"
	"time"
)

// BlockUntilCertificateIsReady blocks the server start until the TLS certificates are ready
func BlockUntilCertificateIsReady(certPath, keyPath string, retryInterval time.Duration) {
	checkIfCertsReady := func() bool {
		if _, err := os.Stat(certPath); err != nil {
			return false
		}

		if _, err := os.Stat(keyPath); err != nil {
			return false
		}

		return true
	}

	for {
		if checkIfCertsReady() {
			break
		}

		log.Printf("[INFO] [edge] [message: Waiting for certificate to be ready]")
		time.Sleep(retryInterval)
	}
}
