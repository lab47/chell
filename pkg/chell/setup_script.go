package chell

import (
	"fmt"
	"os"
	"path/filepath"
)

const setup = `#!/bin/sh

export PATH=$PATH:%[1]s
export MANPATH=$MANPATH:%[2]s
export CPATH=$CPATH:%[3]s
`

func WriteSetup(path string) error {
	sp := filepath.Join(path, "setup.sh")

	f, err := os.Create(sp)
	if err != nil {
		return err
	}

	fmt.Fprintf(f, setup,
		filepath.Join(path, "bin"),
		filepath.Join(path, "share", "man"),
		filepath.Join(path, "include"),
	)

	return nil
}
