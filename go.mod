module github.com/evanphx/chell

go 1.14

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/hashicorp/go-hclog v0.13.0
	github.com/lab47/exprcore v0.0.0-20200613041620-1564c8223b52
	github.com/mholt/archiver/v3 v3.3.0
	github.com/mitchellh/hashstructure v1.0.0
	github.com/mr-tron/base58 v1.1.3
	github.com/oklog/ulid v1.3.1
	github.com/pkg/errors v0.9.1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.2.2
	golang.org/x/crypto v0.0.0-20200510223506-06a226fb4e37
)

replace github.com/lab47/exprcore => ../exprcore
