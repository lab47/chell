package ops

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/lab47/chell/pkg/data"
)

const (
	infoSuffix = ".car-info.json"
	carSuffix  = ".car"
)

type CarUploadS3 struct {
	s3     *s3.S3
	bucket string
	dir    string
}

func (c *CarUploadS3) Upload(id string) error {
	seen := make(map[string]struct{})

	return c.uploadChecked(id, seen)
}

func (c *CarUploadS3) carPath(id string) string {
	return filepath.Join(c.dir, id+".car")
}

func (c *CarUploadS3) uploadChecked(id string, seen map[string]struct{}) error {
	var cinfo data.CarInfo

	infoPath := filepath.Join(c.dir, id+infoSuffix)

	cf, err := os.Open(infoPath)
	if err != nil {
		return err
	}

	defer cf.Close()

	err = json.NewDecoder(cf).Decode(&cinfo)
	if err != nil {
		return err
	}

	for _, dep := range cinfo.Dependencies {
		if _, ok := seen[dep.ID]; ok {
			continue
		}

		seen[dep.ID] = struct{}{}

		_, err := os.Stat(c.carPath(dep.ID))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return err
		}

		err = c.uploadChecked(dep.ID, seen)
		if err != nil {
			return err
		}
	}

	return c.uploadPackage(id)
}

func (c *CarUploadS3) uploadPackage(id string) error {
	infoPath := filepath.Join(c.dir, id+infoSuffix)

	cf, err := os.Open(infoPath)
	if err != nil {
		return err
	}

	defer cf.Close()

	cf.Seek(0, os.SEEK_SET)

	hf := md5.New()

	io.Copy(hf, cf)

	cf.Seek(0, os.SEEK_SET)

	out, err := c.s3.PutObject(&s3.PutObjectInput{
		Bucket:      &c.bucket,
		Key:         aws.String(id + infoSuffix),
		Body:        cf,
		ACL:         aws.String("public"),
		ContentMD5:  aws.String(base64.StdEncoding.EncodeToString(hf.Sum(nil))),
		ContentType: aws.String("application/chell-archive-info"),
		Metadata: map[string]*string{
			"chell-id": aws.String(id),
		},
	})
	if err != nil {
		return err
	}

	fmt.Printf("# %s%s => %s\n", id, infoSuffix, *out.ETag)

	f, err := os.Open(c.carPath(id))
	if err != nil {
		return err
	}

	defer f.Close()

	hf = md5.New()

	io.Copy(hf, f)

	f.Seek(0, os.SEEK_SET)

	out, err = c.s3.PutObject(&s3.PutObjectInput{
		Bucket:      &c.bucket,
		Key:         aws.String(id + carSuffix),
		Body:        f,
		ACL:         aws.String("public"),
		ContentMD5:  aws.String(base64.StdEncoding.EncodeToString(hf.Sum(nil))),
		ContentType: aws.String("application/chell-archive"),
		Metadata: map[string]*string{
			"chell-id": aws.String(id),
		},
	})
	if err != nil {
		return err
	}

	fmt.Printf("# %s%s => %s\n", id, carSuffix, *out.ETag)

	return nil
}

func (c *CarUploadS3) UploadExplicit(ids []string) error {
	for _, id := range ids {
		err := c.uploadPackage(id)
		if err != nil {
			return err
		}
	}

	return nil
}
