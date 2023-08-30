package backends

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/araddon/gou"
	"github.com/cloudfoundry/bbl-state-resource/storage"
	"github.com/lytics/cloudstorage"
	"github.com/lytics/cloudstorage/awss3"
	"github.com/mholt/archiver/v4"
)

type Config struct {
	AWSAccessKeyID       string
	AWSSecretAccessKey   string
	GCPServiceAccountKey string
	Bucket               string
	Region               string
	Dest                 string
}

type Provider interface {
	Client(string) (Backend, error)
}

func NewProvider() Provider {
	return provider{}
}

type provider struct{}

func (p provider) Client(iaas string) (Backend, error) {
	switch iaas {
	case "aws":
		return cloudStorageBackend{}, nil
	case "gcp":
		return gcsStateBackend{}, nil
	default:
		return nil, fmt.Errorf("remote state storage is unsupported for %s environments", iaas)
	}
}

type Backend interface {
	GetState(Config, string) error
}

type cloudStorageBackend struct{}

func (c cloudStorageBackend) GetState(config Config, name string) error {
	awsAuthSettings := make(gou.JsonHelper)
	awsAuthSettings[awss3.ConfKeyAccessKey] = config.AWSAccessKeyID
	awsAuthSettings[awss3.ConfKeyAccessSecret] = config.AWSSecretAccessKey

	csConfig := cloudstorage.Config{
		Type:       awss3.StoreType,
		AuthMethod: awss3.AuthAccessKey,
		Bucket:     config.Bucket,
		Settings:   awsAuthSettings,
		Region:     config.Region,
	}

	store, err := cloudstorage.NewStore(&csConfig)
	if err != nil {
		return err
	}

	tarball, err := store.Get(context.Background(), name)
	if err != nil {
		return err
	}

	stateTar, err := tarball.Open(cloudstorage.ReadOnly)
	if err != nil {
		return err
	}

	format := archiver.CompressedArchive{
		Compression: archiver.Gz{},
		Archival:    archiver.Tar{},
	}

	handler := func(ctx context.Context, f archiver.File) error {
		hdr, ok := f.Header.(*tar.Header)

		if !ok {
			return nil
		}

		var fpath = filepath.Join(config.Dest, f.NameInArchive)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(fpath, 0755); err != nil {
				return fmt.Errorf("failed to make directory %s: %w", fpath, err)
			}
			return nil

		case tar.TypeReg, tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
				return fmt.Errorf("failed to make directory %s: %w", filepath.Dir(fpath), err)
			}

			out, err := os.Create(fpath)
			if err != nil {
				return fmt.Errorf("%s: creating new file: %v", fpath, err)
			}
			defer out.Close()

			err = out.Chmod(f.Mode())
			if err != nil && runtime.GOOS != "windows" {
				return fmt.Errorf("%s: changing file mode: %v", fpath, err)
			}

			in, err := f.Open()
			if err != nil {
				return err
			}

			_, err = io.Copy(out, in)
			if err != nil {
				return fmt.Errorf("%s: writing file: %v", fpath, err)
			}
			return nil

		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
				return fmt.Errorf("failed to make directory %s: %w", filepath.Dir(fpath), err)
			}

			err = os.Symlink(hdr.Linkname, fpath)
			if err != nil {
				return fmt.Errorf("%s: making symbolic link for: %v", fpath, err)
			}
			return nil

		case tar.TypeLink:
			if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
				return fmt.Errorf("failed to make directory %s: %w", filepath.Dir(fpath), err)
			}

			err = os.Link(filepath.Join(fpath, hdr.Linkname), fpath)
			if err != nil {
				return fmt.Errorf("%s: making symbolic link for: %v", fpath, err)
			}
			return nil

		case tar.TypeXGlobalHeader:
			return nil // ignore the pax global header from git-generated tarballs
		default:
			return fmt.Errorf("%s: unknown type flag: %c", hdr.Name, hdr.Typeflag)
		}
	}

	err = format.Extract(context.TODO(), stateTar, nil, handler)
	if err != nil {
		return fmt.Errorf("unable to untar state dir: %s", err)
	}

	return nil
}

type gcsStateBackend struct{}

func (g gcsStateBackend) GetState(config Config, name string) error {
	key, err := g.getGCPServiceAccountKey(config.GCPServiceAccountKey)
	if err != nil {
		return fmt.Errorf("could not read GCP service account key: %s", err)
	}

	gcsClient, err := storage.NewStorageClient(key, name, config.Bucket)
	if err != nil {
		return fmt.Errorf("could not create GCS client: %s", err)
	}

	_, err = gcsClient.Download(config.Dest)
	if err != nil {
		return fmt.Errorf("downloading remote state from GCS: %s", err)
	}

	return nil
}

func (g gcsStateBackend) getGCPServiceAccountKey(key string) (string, error) {
	if _, err := os.Stat(key); err != nil {
		return key, nil
	}

	keyBytes, err := os.ReadFile(key)
	if err != nil {
		return "", fmt.Errorf("Reading key: %v", err)
	}

	return string(keyBytes), nil
}
