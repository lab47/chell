package repo

import "github.com/lab47/chell/pkg/sumfile"

type Entry interface {
	RepoId() string
	Script() (string, []byte, error)
	Asset(name string) (string, []byte, error)
	Sumfile() (*sumfile.Sumfile, error)
	SaveSumfile(sf *sumfile.Sumfile) error
}
