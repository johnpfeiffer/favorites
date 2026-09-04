// Command fav is a batch-oriented helper CLI for the favorites repo workflow.
// Every subcommand is a deterministic evidence gatherer: it fetches,
// normalizes, and matches, then prints full records for a human or agent to
// judge. There is deliberately no concurrency: requests are sequential and
// rate-limited so batch runs stay respectful of the servers they touch.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "dedupe":
		err = cmdDedupe(os.Args[2:])
	case "wayback":
		err = cmdWayback(os.Args[2:])
	case "check":
		err = cmdCheck(os.Args[2:])
	case "date":
		err = cmdDate(os.Args[2:])
	case "lint":
		err = cmdLint(os.Args[2:])
	case "graph":
		err = cmdGraph(os.Args[2:])
	case "podcast":
		err = cmdPodcast(os.Args[2:])
	case "version", "--version":
		fmt.Println("fav", version)
		return
	default:
		fmt.Fprintf(os.Stderr, "fav: unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fav:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `fav - favorites repo workflow helper (deterministic evidence gatherers)

Usage:
  fav dedupe  [--content dir] [--json] <url> [title words...] ...
  fav wayback [--mode latest|earliest|both] [--delay 8s] [--json] <url> ...
  fav check   [--delay 2s] [--json] <url> ...
  fav date    [--offline] [--force] [--delay 2s] [--json] <url> ...
  fav lint    [--content dir] [--json]
  fav graph   add-edge [--repo dir] [--dry-run] [--json] <Source Type Target...>
  fav graph   bio [--delay 2s] [--json] <name> ...
  fav podcast lookup   [--repo dir] [--show slug-or-name] [--json] <keywords...>
  fav podcast refresh  [--repo dir] [--show slug-or-name] [--check] [--delay 2s]
  fav podcast delisted [--repo dir] [--show slug-or-name] [--delay 2s] [--json]

Batch input: pass items as arguments, or pipe one per line on stdin ("-" also
reads stdin). For dedupe, a line may be "URL optional title keywords"; the URL
is matched against stored url/alternate-url values and the keywords run a
fuzzy title search. Output preserves input order. Blank lines and lines
starting with '#' are ignored.
`)
}

// inputLine is one batch item: a target (usually a URL) plus optional trailing
// free text (e.g. title keywords for fuzzy matching).
type inputLine struct {
	Target string
	Rest   string
}

// readRawLines collects batch items from args verbatim, or from stdin when
// args are empty or a single "-". Use readInputs when items are
// "target [free text]"; use this when the whole line is meaningful (e.g.
// tab-separated triples).
func readRawLines(args []string) ([]string, error) {
	var raw []string
	if len(args) > 0 && !(len(args) == 1 && args[0] == "-") {
		raw = args
	} else {
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
		for sc.Scan() {
			raw = append(raw, sc.Text())
		}
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
	}
	var out []string
	for _, line := range raw {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no input items (pass arguments or pipe lines on stdin)")
	}
	return out, nil
}

// readInputs collects batch items as "target [optional free text]" pairs.
func readInputs(args []string) ([]inputLine, error) {
	raw, err := readRawLines(args)
	if err != nil {
		return nil, err
	}
	var out []inputLine
	for _, line := range raw {
		fields := strings.Fields(line)
		out = append(out, inputLine{Target: fields[0], Rest: strings.Join(fields[1:], " ")})
	}
	return out, nil
}
