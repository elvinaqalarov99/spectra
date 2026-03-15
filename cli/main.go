package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/elvinaqalarov99/spectra/inference"
	"github.com/elvinaqalarov99/spectra/proxy"
	"github.com/elvinaqalarov99/spectra/server"
)

const banner = `
  ____                 __
 / __/__  ___ ____/  /________ _
_\ \/ _ \/ -_) __/ __/ __/ _ '/
/___/ .__/\__/\__/\__/_/  \_,_/
   /_/  API Docs that can't lie.
`

func main() {
	// Subcommands
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "start":
		cmdStart(os.Args[2:])
	case "export":
		cmdExport(os.Args[2:])
	case "diff":
		cmdDiff(os.Args[2:])
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

// ---- start ----

func cmdStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	target := fs.String("target", "http://localhost:3000", "upstream server to proxy to")
	proxyAddr := fs.String("proxy", ":9999", "address for the proxy to listen on")
	uiAddr := fs.String("ui", ":7878", "address for the docs UI + spec API")
	title := fs.String("title", "My API", "API title in the generated spec")
	fs.Parse(args)

	fmt.Print(banner)
	log.Printf("target:  %s", *target)
	log.Printf("proxy:   http://localhost%s", *proxyAddr)
	log.Printf("docs:    http://localhost%s/docs/", *uiAddr)

	merger := inference.NewSpecMerger(*title)
	srv := server.New(merger)

	p, err := proxy.New(*target, merger)
	if err != nil {
		log.Fatalf("invalid target URL: %v", err)
	}
	p.OnObs = srv.NotifyUpdate

	// Start docs server
	go func() {
		if err := srv.Listen(*uiAddr); err != nil {
			log.Fatalf("docs server: %v", err)
		}
	}()

	// Start proxy
	log.Printf("proxy listening on %s → %s", *proxyAddr, *target)
	if err := http.ListenAndServe(*proxyAddr, p); err != nil {
		log.Fatalf("proxy: %v", err)
	}
}

// ---- export ----

func cmdExport(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	addr := fs.String("from", "http://localhost:7878", "running spectra server")
	out := fs.String("out", "openapi.json", "output file")
	fs.Parse(args)

	resp, err := http.Get(*addr + "/spec")
	if err != nil {
		log.Fatalf("cannot reach server: %v", err)
	}
	defer resp.Body.Close()

	var spec interface{}
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		log.Fatalf("invalid spec response: %v", err)
	}

	f, err := os.Create(*out)
	if err != nil {
		log.Fatalf("cannot create output file: %v", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.Encode(spec)
	log.Printf("spec written to %s", *out)
}

// ---- diff ----

func cmdDiff(args []string) {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	committed := fs.String("committed", "openapi.json", "committed spec file")
	live := fs.String("live", "http://localhost:7878", "running spectra server")
	fs.Parse(args)

	// Load committed spec
	committedData, err := os.ReadFile(*committed)
	if err != nil {
		log.Fatalf("cannot read committed spec: %v", err)
	}
	var committedSpec map[string]interface{}
	if err := json.Unmarshal(committedData, &committedSpec); err != nil {
		log.Fatalf("invalid committed spec: %v", err)
	}

	// Fetch live spec
	resp, err := http.Get(*live + "/spec")
	if err != nil {
		log.Fatalf("cannot reach live server: %v", err)
	}
	defer resp.Body.Close()
	var liveSpec map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&liveSpec); err != nil {
		log.Fatalf("invalid live spec: %v", err)
	}

	diffs := diffSpecs(committedSpec, liveSpec)
	if len(diffs) == 0 {
		fmt.Println("✓ No drift detected — spec is up to date.")
		os.Exit(0)
	}

	fmt.Printf("✗ Spec drift detected (%d changes):\n\n", len(diffs))
	for _, d := range diffs {
		fmt.Println(" •", d)
	}
	os.Exit(1)
}

func diffSpecs(committed, live map[string]interface{}) []string {
	var diffs []string

	committedPaths, _ := committed["paths"].(map[string]interface{})
	livePaths, _ := live["paths"].(map[string]interface{})

	for path := range livePaths {
		if _, ok := committedPaths[path]; !ok {
			diffs = append(diffs, fmt.Sprintf("NEW path: %s", path))
		}
	}
	for path := range committedPaths {
		if _, ok := livePaths[path]; !ok {
			diffs = append(diffs, fmt.Sprintf("REMOVED path: %s", path))
		}
	}
	return diffs
}

func printUsage() {
	fmt.Print(banner)
	fmt.Println(`
Usage: spectra <command> [flags]

Commands:
  start    Start the proxy and docs server
  export   Export the current spec to a file
  diff     Compare a committed spec against the live one

Examples:
  spectra start --target http://localhost:3000 --proxy :9999 --ui :7878
  spectra export --out openapi.json
  spectra diff --committed openapi.json --live http://localhost:7878
`)
}
