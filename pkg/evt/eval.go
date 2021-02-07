package evt

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type Evaluator struct {
	cwd          string
	env          []string
	outputPrefix string
}

func (e *Evaluator) Eval(n EVTNode) error {
	switch n := n.(type) {
	case *SetRoot:
		tgt := filepath.Join(e.cwd, n.Dir)

		sf, err := ioutil.ReadDir(tgt)
		if err != nil {
			return err
		}

		var (
			ent os.FileInfo
			cnt int
		)
		for _, e := range sf {
			if e.Name()[0] != '.' {
				cnt++
				ent = e
			}
		}
		if cnt == 1 && ent.IsDir() {
			tgt = filepath.Join(tgt, ent.Name())
		}

		e.cwd = tgt

	case *ChangeDir:
		defer func(dir string) {
			e.cwd = dir
		}(e.cwd)

		e.cwd = filepath.Join(e.cwd, n.Dir)

		return e.Eval(n.Body)
	case *MakeDir:
		err := os.MkdirAll(filepath.Join(e.cwd, n.Dir), 0755)
		if err != nil {
			return err
		}
	case *Shell:
		cmd := exec.Command("bash")
		cmd.Stdin = strings.NewReader(n.Code)
		cmd.Env = e.env
		cmd.Dir = e.cwd

		return e.runCmd(cmd)
	case *Patch:
		cmd := exec.Command("patch", "-p1")
		cmd.Stdin = strings.NewReader(n.Patch)
		cmd.Env = e.env
		cmd.Dir = e.cwd

		return e.runCmd(cmd)
	}

	return nil
}

func (e *Evaluator) runCmd(cmd *exec.Cmd) error {
	or, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	er, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		br := bufio.NewReader(or)
		for {
			line, err := br.ReadString('\n')
			if len(line) > 0 {
				fmt.Printf("%s │ %s\n", e.outputPrefix, strings.TrimRight(line, " \n\t"))
			}

			if err != nil {
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		br := bufio.NewReader(er)
		for {
			line, err := br.ReadString('\n')
			if len(line) > 0 {
				fmt.Printf("%s │ %s\n", e.outputPrefix, strings.TrimRight(line, " \n\t"))
			}

			if err != nil {
				return
			}
		}
	}()

	err = cmd.Start()
	if err != nil {
		return err
	}

	wg.Wait()

	err = cmd.Wait()
	if err != nil {
		return err
	}

	return nil
}
