package ops

import (
	"crypto/ed25519"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/hashicorp/go-hclog"
	"github.com/lab47/chell/pkg/config"
)

type Ops struct {
	logger hclog.Logger

	path     []string
	storeDir string

	pub  ed25519.PublicKey
	priv ed25519.PrivateKey

	cfg *Config
}

func NewOps(logger hclog.Logger, cfg *config.Config) (*Ops, error) {
	o := &Ops{
		logger:   logger,
		path:     cfg.LoadPath(),
		storeDir: cfg.StorePath(),
		priv:     cfg.Private(),
		pub:      cfg.Public(),
	}

	err := o.findConfig()
	if err != nil {
		return nil, err
	}

	return o, nil
}

func (o *Ops) findConfig() error {
	var cl ConfigLoad
	cl.load = o.ScriptLoad()

	cfg, err := cl.LoadConfig("")
	if err != nil {
		return err
	}

	o.cfg = cfg
	return nil
}

func (o *Ops) ProjectLoad() *ConfigLoad {
	var cl ConfigLoad
	cl.load = o.ScriptLoad()
	return &cl
}

func (o *Ops) ScriptLoad() *ScriptLoad {
	var lookup ScriptLookup
	lookup.Path = o.path

	lookup.SetLogger(o.logger.Named("script-lookup"))

	var sl ScriptLoad
	sl.StoreDir = o.storeDir
	sl.lookup = &lookup
	sl.cfg = o.cfg

	sl.SetLogger(o.logger.Named("script-load"))

	return &sl
}

func (o *Ops) PackageCalcInstall() *PackageCalcInstall {
	// var carLookup CarLookup
	// carLookup.client = http.DefaultClient

	var pci PackageCalcInstall
	pci.StoreDir = o.storeDir
	pci.SetLogger(o.logger)

	// pci.carLookup = &carLookup

	return &pci
}

func (o *Ops) PackagesInstall(ienv *InstallEnv) *PackagesInstall {
	pi := &PackagesInstall{ienv: ienv}

	pi.SetLogger(o.logger.Named("packages-installer"))

	return pi
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

func (o *Ops) PackageDetectLibs() *PackageDetectLibs {
	return &PackageDetectLibs{storeDir: o.storeDir}
}

func (o *Ops) ScriptAllDeps() *ScriptCalcDeps {
	return &ScriptCalcDeps{
		storeDir: o.storeDir,
	}
}
