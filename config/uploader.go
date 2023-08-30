package config

import (
	"github.com/cloudfoundry/bosh-bootloader/backends"
)

type Uploader struct {
	provider backends.Provider
}

func NewUploader(provider backends.Provider) Downloader {
	return Downloader{provider: provider}
}

func (d Downloader) UploadeState(flags GlobalFlags) error {
	backend, err := d.provider.Client(flags.IAAS)
	if err != nil {
		return err
	}

	var config backends.Config
	switch flags.IAAS {
	case "aws":
		config = backends.Config{
			Source:             flags.StateDir,
			Dest:               flags.StateDir,
			Bucket:             flags.StateBucket,
			Region:             flags.AWSRegion,
			AWSAccessKeyID:     flags.AWSAccessKeyID,
			AWSSecretAccessKey: flags.AWSSecretAccessKey,
		}

	case "gcp":
		config = backends.Config{
			Source:               flags.StateDir,
			Dest:                 flags.StateDir,
			Bucket:               flags.StateBucket,
			Region:               flags.GCPRegion,
			GCPServiceAccountKey: flags.GCPServiceAccountKey,
		}
	}

	return backend.SetState(config, flags.EnvID)
}

// // set logic from https://github.com/cloudfoundry/bbl-state-resource/blob/master/cmd/out/out.go#L48 where?
// // how to get globalflags?
// if globalFlags.StateBucket != "" {
// 	version, err := StorageClient.Upload(bblStateDir)
// 	if err != nil {
// 		return fmt.Errorf(os.Stderr, "failed to upload bbl state: %s\n", err)
// 	}

// 	return fmt.Errorf("successfully uploaded bbl state!\n")
// }
