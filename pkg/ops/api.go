package ops

import (
	"crypto/ed25519"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/lab47/chell/pkg/config"
)

type Ops struct {
	path     []string
	storeDir string

	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

func NewOps(cfg *config.Config) (*Ops, error) {
	o := &Ops{
		path:     cfg.LoadPath(),
		storeDir: cfg.StorePath(),
		priv:     cfg.Private(),
		pub:      cfg.Public(),
	}

	return o, nil
}

func (o *Ops) ScriptLoad() *ScriptLoad {
	var lookup ScriptLookup
	lookup.Path = o.path

	var sl ScriptLoad
	sl.StoreDir = o.storeDir
	sl.lookup = &lookup

	return &sl
}

func (o *Ops) PackageCalcInstall() *PackageCalcInstall {
	// var carLookup CarLookup
	// carLookup.client = http.DefaultClient

	var pci PackageCalcInstall
	// pci.carLookup = &carLookup

	return &pci
}

func (o *Ops) PackagesInstall(ienv *InstallEnv) *PackagesInstall {
	return &PackagesInstall{ienv: ienv}
}

func (o *Ops) StoreToCar(output string) *StoreToCar {
	var stc StoreToCar
	stc.storePath = o.storeDir
	stc.outputPath = output
	stc.pub = o.pub
	stc.priv = o.priv

	return &stc
}

func (o *Ops) CarUploadS3(bucket, dir string) (*CarUploadS3, error) {
	awscfg := aws.NewConfig()
	if ep := os.Getenv("AWS_ENDPOINT_S3"); ep != "" {
		awscfg.Endpoint = &ep
		awscfg.S3ForcePathStyle = aws.Bool(true)
	}

	sess, err := session.NewSession(awscfg)
	if err != nil {
		return nil, err
	}

	api := s3.New(sess)

	cu := &CarUploadS3{
		s3:     api,
		bucket: bucket,
		dir:    dir,
	}

	return cu, nil
}
