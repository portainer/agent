package stack

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/transports/alltransports"
	"github.com/containers/image/v5/types"
)

// TODO: this code is copied in the portainer/portainer-ee PR too, should consolidate somewhere
func copyImage(ctx context.Context, cacheImage, stackImage string) error {

	// allow anything...
	policy := &signature.Policy{Default: []signature.PolicyRequirement{signature.NewPRInsecureAcceptAnything()}}
	policyContext, _ := signature.NewPolicyContext(policy)

	srcRef, err := alltransports.ParseImageName("docker://" + cacheImage)
	if err != nil {
		return fmt.Errorf("Invalid source name %s: %v", cacheImage, err)
	}
	destRef, err := alltransports.ParseImageName("docker-daemon:" + stackImage)
	if err != nil {
		return fmt.Errorf("Invalid destination name %s: %v", stackImage, err)
	}
	sourceCtx := types.SystemContext{
		DockerInsecureSkipTLSVerify: types.NewOptionalBool(true),
	}
	destinationCtx := types.SystemContext{
		DockerInsecureSkipTLSVerify: types.NewOptionalBool(true),
	}

	log.Printf(
		"Copy %v to %v",
		srcRef,
		destRef,
	)

	_, err = copy.Image(ctx, policyContext, destRef, srcRef, &copy.Options{
		//RemoveSignatures:      opts.removeSignatures,
		//SignBy:                opts.signByFingerprint,
		//SignPassphrase:        passphrase,
		ReportWriter:   os.Stdout,
		SourceCtx:      &sourceCtx,
		DestinationCtx: &destinationCtx,
		//ForceManifestMIMEType: manifestType,
		ImageListSelection: copy.CopySystemImage,
		//PreserveDigests:       opts.preserveDigests,
		// OciDecryptConfig:      decConfig,
		// OciEncryptLayers:      encLayers,
		// OciEncryptConfig:      encConfig,
	})
	if err != nil {
		return err
	}

	return nil
}
