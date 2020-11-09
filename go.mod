module github.com/lab47/chell

go 1.14

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/go-git/go-git/v5 v5.2.0
	github.com/hashicorp/go-getter v1.5.0
	github.com/hashicorp/go-hclog v0.13.0
	github.com/ipfs/go-ipfs v0.7.0
	github.com/lab47/exprcore v0.0.0-20200613041620-1564c8223b52
	github.com/mholt/archiver/v3 v3.3.0
	github.com/mitchellh/go-homedir v1.1.0
	github.com/mitchellh/hashstructure v1.0.0
	github.com/mr-tron/base58 v1.2.0
	github.com/oklog/ulid v1.3.1
	github.com/pkg/errors v0.9.1
	github.com/spf13/cobra v1.0.0
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.7.1
	github.com/stretchr/testify v1.6.1
	golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9
)

replace github.com/lab47/exprcore => ../exprcore
