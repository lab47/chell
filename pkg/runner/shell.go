package runner

import (
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/lab47/exprcore/exprcore"
)

func (rc *RunCtx) runShell(thread *exprcore.Thread, parts []string) (exprcore.Value, error) {
	var (
		args       []string
		sb         strings.Builder
		inside     bool
		insideChar rune
	)

	spew.Dump("shell-start", parts)

	for _, p := range parts {
		for _, r := range p {
			switch r {
			case ' ', '\t', '\n', '\r':
				if !inside {
					if sb.Len() > 0 {
						args = append(args, sb.String())
						sb.Reset()
					}
					continue
				}
			case '"', '\'':
				if !inside {
					inside = true
					insideChar = r
					continue
				} else if insideChar == r {
					inside = false
					continue
				}
			}

			sb.WriteRune(r)
		}
	}

	if sb.Len() > 0 {
		args = append(args, sb.String())
	}

	spew.Dump(args)

	return exprcore.None, nil
}
