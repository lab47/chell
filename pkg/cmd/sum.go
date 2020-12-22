package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/hashicorp/go-hclog"
	"github.com/lab47/chell/pkg/cleanhttp"
	"github.com/lab47/chell/pkg/config"
	"github.com/lab47/chell/pkg/event"
	"github.com/lab47/chell/pkg/loader"
	"github.com/lab47/chell/pkg/repo"
	"github.com/mr-tron/base58"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/blake2b"
)

var (
	sumCmd = &cobra.Command{
		Use:   "sum",
		Short: "Update sums based on used assets",
		Long:  ``,
		Args:  cobra.ExactArgs(1),
		Run:   sum,
	}
)

func sum(c *cobra.Command, args []string) {
	h, _ := blake2b.New256(nil)

	path := args[0]

	show := func(st, sv string) {
		fmt.Printf("file(\n  path: \"%s\",\n  sum: (\"%s\", \"%s\"),\n)", path, st, sv)
	}

	u, err := url.Parse(path)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		resp, err := cleanhttp.Get(path)
		if err != nil {
			log.Printf("url not available")
			return
		}

		defer resp.Body.Close()

		if etag := resp.Header.Get("Etag"); etag != "" && etag[0] == '"' {
			show("etag", etag[1:len(etag)-1])
			return
		}

		resp, err = http.Get(path)
		if err != nil {
			log.Printf("url not available")
			return
		}

		defer resp.Body.Close()

		io.Copy(h, resp.Body)
	} else {
		f, err := os.Open(path)
		if err != nil {
			log.Printf("unable to open file: %s", err)
			return
		}

		io.Copy(h, f)
	}

	show("b2", base58.Encode(h.Sum(nil)))
}

func oldsum(c *cobra.Command, args []string) {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("error opening repo: %s\n", err)
		os.Exit(1)
	}

	dir, err := repo.NewDirectory("./packages")
	if err != nil {
		fmt.Printf("error opening repo: %s\n", err)
		os.Exit(1)
	}

	L := hclog.L()

	loader, err := loader.NewLoader(L, cfg, dir)
	if err != nil {
		fmt.Printf("error creating loader: %s\n", err)
		os.Exit(1)
	}

	script, err := loader.LoadScript(args[0])
	if err != nil {
		fmt.Printf("error loading script: %s\n", err)
		os.Exit(1)
	}

	var r event.Renderer

	ctx := r.WithContext(context.Background())

	err = script.SaveSums(ctx)
	if err != nil {
		fmt.Printf("error loading script: %s\n", err)
		os.Exit(1)
	}

	sig, err := script.Signature()
	if err != nil {
		fmt.Printf("error calculate package signature: %s\n", err)
		os.Exit(1)
	}

	fmt.Printf("Signature: %s\n", sig)

	subs, err := script.Dependencies()
	if err != nil {
		fmt.Printf("error calculate package signature: %s\n", err)
		os.Exit(1)
	}

	for _, dep := range subs {
		err = dep.SaveSums(ctx)
		if err != nil {
			fmt.Printf("error loading script: %s\n", err)
			os.Exit(1)
		}

		sig, err := dep.Signature()
		if err != nil {
			fmt.Printf("error calculate package signature: %s\n", err)
			os.Exit(1)
		}
		fmt.Printf("Signature: %s\n", sig)

	}
}
