package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/elvinaqalarov99/specula/inference"
	"github.com/elvinaqalarov99/specula/proxy"
	"github.com/elvinaqalarov99/specula/server"
)

const banner = `
  ____              __
 / __/___  ___ ____/ /__ _
_\ \/ _ \/ -_) __/ / // /
/___/ .__/\__/\__/_/\_,_/
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
	case "reset":
		cmdReset(os.Args[2:])
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
	target := fs.String("target", "", "upstream server to proxy to (proxy mode)")
	proxyAddr := fs.String("proxy", ":9999", "address for the proxy to listen on")
	uiAddr := fs.String("ui", ":7878", "address for the docs UI + spec API")
	title := fs.String("title", "My API", "API title in the generated spec")
	apiURL := fs.String("api", "", "base URL of your API shown in Swagger UI Execute (SDK mode)")
	fs.Parse(args)

	fmt.Print(banner)
	log.Printf("docs:    http://localhost%s/docs/", *uiAddr)

	merger := inference.NewSpecMerger(*title)

	// Determine the server URL for Swagger UI Execute:
	// --api takes precedence, then --target, then nothing (Execute disabled)
	serverURL := *apiURL
	if serverURL == "" {
		serverURL = *target
	}
	if serverURL != "" {
		merger.SetServer(serverURL)
		log.Printf("api:     %s  (proxied via localhost%s/api-proxy)", serverURL, *uiAddr)
	}

	srv := server.New(merger)
	if serverURL != "" {
		srv.SetTarget(serverURL)
	}

	// Start docs server (always)
	go func() {
		if err := srv.Listen(*uiAddr); err != nil {
			log.Fatalf("docs server: %v", err)
		}
	}()

	// Start proxy only when --target is provided
	if *target != "" {
		p, err := proxy.New(*target, merger)
		if err != nil {
			log.Fatalf("invalid target URL: %v", err)
		}
		p.OnObs = srv.NotifyUpdate
		log.Printf("proxy:   http://localhost%s → %s", *proxyAddr, *target)
		if err := http.ListenAndServe(*proxyAddr, p); err != nil {
			log.Fatalf("proxy: %v", err)
		}
	} else {
		log.Printf("SDK mode — waiting for observations on /ingest")
		select {} // block forever
	}
}

// ---- export ----

func cmdExport(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	addr := fs.String("from", "http://localhost:7878", "running specula server")
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
	live := fs.String("live", "http://localhost:7878", "running specula server")
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

// ---- reset ----

func cmdReset(args []string) {
	fs := flag.NewFlagSet("reset", flag.ExitOnError)
	addr := fs.String("from", "http://localhost:7878", "running specula server")
	fs.Parse(args)

	req, err := http.NewRequest(http.MethodDelete, *addr+"/spec", nil)
	if err != nil {
		log.Fatalf("failed to build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("cannot reach server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("✓ Spec cleared — all observations reset.")
	} else {
		fmt.Fprintf(os.Stderr, "✗ Server returned %d\n", resp.StatusCode)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(banner)
	fmt.Print(`
Usage: specula <command> [flags]

Commands:
  start    Start the proxy and docs server
  export   Export the current spec to a file
  diff     Compare a committed spec against the live one
  reset    Clear all observations and start fresh

Examples:
  specula start --target http://localhost:3000 --proxy :9999 --ui :7878
  specula start --api https://api.example.test --ui :7878
  specula export --out openapi.json
  specula diff --committed openapi.json --live http://localhost:7878
  specula reset
`)
}
