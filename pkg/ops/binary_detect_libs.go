package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type PackageDetectLibs struct {
	storeDir string
}

type DetectedLibs struct {
	ChellLibs  []string
	SystemLibs []string
}

func (p *PackageDetectLibs) Detect(id string) (*DetectedLibs, error) {
	var dl DetectedLibs

	seen := map[string]struct{}{}

	err := filepath.Walk(filepath.Join(p.storeDir, id), func(path string, info os.FileInfo, err error) error {
		if !info.Mode().IsRegular() {
			return nil
		}

		if info.Mode().Perm()&0111 != 0 {
			c := exec.Command("otool", "-L", path)
			c.Stderr = os.Stderr

			data, err := c.Output()
			if err != nil {
				return err
			}

			lines := strings.Split(string(data), "\n")

			for _, line := range lines[1:] {
				idx := strings.IndexByte(line, '(')
				if idx == -1 {
					continue
				}

				lib := strings.TrimSpace(line[:idx])

				if _, ok := seen[lib]; ok {
					continue
				}

				seen[lib] = struct{}{}

				if strings.HasPrefix(lib, p.storeDir) {
					dl.ChellLibs = append(dl.ChellLibs, lib)
				} else {
					dl.SystemLibs = append(dl.SystemLibs, lib)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &dl, nil
}
