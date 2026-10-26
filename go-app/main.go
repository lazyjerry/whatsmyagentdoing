package main

// CLI entry point. Port of bin/cli.js: same flags, same output, same ports.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func helpText(version string) string {
	return fmt.Sprintf(`whatsmyagentdoing %s (Go)
See what every Claude Code, Codex, Gemini CLI and OpenCode agent on this machine is doing, live.

Usage: whatsmyagentdoing [options]

Options:
  -p, --port <n>    Port to serve on (default 4545)
  --hours <n>       Show sessions active in the last n hours (default %g)
  --no-open         Don't open the browser
  --private         Hide prompts, replies and titles; show status only
  --json            Print one snapshot as JSON and exit
  --demo            Show a made-up team instead of your sessions
  --stop            Stop a running dashboard
  --update          Update to the latest version (git pull + go build) and restart a running dashboard
  --scenes          List 3D scenes (installed and downloadable)
  --scene <id>      Download a scene if needed and open the office in it
  --remove-scene <id>  Delete a downloaded scene
  -h, --help        Show this help

Reads (never writes):
%s
Override with CLAUDE_CONFIG_DIR / CODEX_HOME / GEMINI_CLI_HOME.
The page and its assets are built into this binary; --update needs it to sit
in its go-app/ source folder or skills/myoffice/bin/ inside a git clone (or
WMAD_SRC pointing there).`, version, DefaultWindowHours, describeDirs())
}

func parseArgs(argv []string, version string) Options {
	o := Options{Port: 4545, Hours: DefaultWindowHours, Open: true}
	help := helpText(version)
	num := func(s string) float64 {
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, help)
			os.Exit(1)
		}
		return n
	}
	next := func(i *int) string {
		*i++
		if *i >= len(argv) {
			fmt.Fprintln(os.Stderr, help)
			os.Exit(1)
		}
		return argv[*i]
	}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "-h" || a == "--help":
			fmt.Println(help)
			os.Exit(0)
		case a == "-v" || a == "--version":
			fmt.Println(version)
			os.Exit(0)
		case a == "-p" || a == "--port":
			o.Port = int(num(next(&i)))
		case strings.HasPrefix(a, "--port="):
			o.Port = int(num(a[7:]))
		case a == "--hours":
			o.Hours = num(next(&i))
		case strings.HasPrefix(a, "--hours="):
			o.Hours = num(a[8:])
		case a == "--no-open":
			o.Open = false
		case a == "--private":
			o.Privacy = true
		case a == "--json":
			o.JSON = true
		case a == "--demo":
			o.Demo = true
		case a == "--stop":
			o.Stop = true
		case a == "--update":
			o.Update = true
		case a == "--scenes":
			o.Scenes = true
		case a == "--scene":
			o.Scene = next(&i)
		case strings.HasPrefix(a, "--scene="):
			o.Scene = a[8:]
		case a == "--remove-scene":
			o.Remove = next(&i)
		default:
			fmt.Fprintf(os.Stderr, "Unknown option: %s\n\n%s\n", a, help)
			os.Exit(1)
		}
	}
	return o
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	if err := cmd.Start(); err == nil {
		cmd.Process.Release()
	}
}

func alreadyRunning(port int) bool {
	client := &http.Client{Timeout: 800 * time.Millisecond}
	res, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/ping", port))
	if err != nil {
		return false
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	return strings.Contains(string(body), "whatsmyagentdoing")
}

func stopAt(port int) bool {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	req, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/api/shutdown", port), nil)
	req.Header.Set("x-whatsmyagentdoing", "stop")
	res, err := client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body)
	return res.StatusCode == 200
}

// Only with AutoUpdateCheck on (config.go). Agents read this line (see
// SKILL.md) and offer the update.
func printUpdateHint(u *Updater) {
	if !AutoUpdateCheck {
		return
	}
	u.CheckLatest(true)
	if i := u.Info(); i.UpdateAvailable {
		fmt.Printf("Update available: v%s → v%s. Run with --update (or /myoffice update), or click \"Update\" on the page.\n", i.Version, *i.Latest)
	}
}

func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "address already in use") || strings.Contains(msg, "Only one usage of each socket address")
}

func main() {
	updater := NewUpdater()
	version := updater.LocalVersion()
	o := parseArgs(os.Args[1:], version)
	scenes := Scenes{}

	if o.Update {
		before := updater.LocalVersion()
		after, err := updater.Update()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if after == before {
			fmt.Printf("Already up to date (v%s).\n", after)
		} else {
			fmt.Printf("Updated v%s → v%s.\n", before, after)
		}
		// a running dashboard keeps the old code until it restarts
		for p := o.Port; p <= o.Port+10; p++ {
			if alreadyRunning(p) {
				stopAt(p)
				fmt.Printf("Restart it with /myoffice (it was on port %d).\n", p)
			}
		}
		return
	}

	if o.Scenes {
		have := scenes.InstalledList()
		fmt.Println("3D scenes (pick one with --scene <id>, or from the scene menu in High 3D):")
		fmt.Println("  office      Office (built in)")
		for _, x := range have {
			fmt.Printf("  %-11s %s (installed)\n", x.ID, sceneName(x.Name, x.ID))
		}
		for _, x := range scenes.Catalog() {
			installed := false
			for _, h := range have {
				if h.ID == x.ID {
					installed = true
				}
			}
			if installed {
				continue
			}
			kb := (x.Size + 512) / 1024
			if kb < 1 {
				kb = 1
			}
			fmt.Printf("  %-11s %s (download, %d KB)\n", x.ID, sceneName(x.Name, x.ID), kb)
		}
		return
	}

	if o.Remove != "" {
		if scenes.Remove(o.Remove) {
			fmt.Printf("Removed %s.\n", o.Remove)
		} else {
			fmt.Printf("%s isn't a downloaded scene.\n", o.Remove)
		}
		return
	}

	if o.Scene != "" && o.Scene != "office" {
		had := scenes.SceneDir(o.Scene) != ""
		x, err := scenes.Install(o.Scene)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if had {
			fmt.Printf("Scene %s is ready.\n", x.ID)
		} else {
			fmt.Printf("Downloaded scene %s.\n", x.ID)
		}
	}

	if o.Stop {
		// It may have moved to a later port if the default was taken.
		var ports []string
		for p := o.Port; p <= o.Port+10; p++ {
			if alreadyRunning(p) {
				stopAt(p)
				ports = append(ports, fmt.Sprintf("http://localhost:%d", p))
			}
		}
		if len(ports) > 0 {
			fmt.Printf("Stopped %s\n", strings.Join(ports, ", "))
		} else {
			fmt.Println("Not running.")
		}
		return
	}

	if o.JSON {
		w := NewWatcher(o.Hours)
		w.Tick()
		w.ForceRescan()
		w.Tick()
		out, _ := json.MarshalIndent(snapshot(w, o).Sessions, "", "  ")
		fmt.Println(string(out))
		return
	}

	query := ""
	if o.Scene != "" {
		query = "/?view=office&quality=high&scene=" + url.QueryEscape(o.Scene)
	}
	if alreadyRunning(o.Port) {
		u := fmt.Sprintf("http://localhost:%d%s", o.Port, query)
		fmt.Printf("Already running at %s\n", u)
		printUpdateHint(updater)
		if o.Open {
			openBrowser(u)
		}
		return
	}

	var src Source
	if o.Demo {
		src = NewDemoWatcher()
	} else {
		src = NewWatcher(o.Hours)
	}
	src.Start()

	var httpServer *http.Server
	var app *Server
	app = NewServer(src, o, updater,
		func() { // onShutdown
			src.Stop()
			app.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			httpServer.Shutdown(ctx)
			os.Exit(0)
		},
		func() { // onRestart: after an update from the page, start the new binary on the same port
			src.Stop()
			app.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			httpServer.Shutdown(ctx)
			cancel()
			exe, _ := os.Executable()
			var args []string
			for _, a := range os.Args[1:] {
				if a == "--no-open" || strings.HasPrefix(a, "--scene") {
					continue
				}
				args = append(args, a)
			}
			args = append(args, "--no-open")
			cmd := exec.Command(exe, args...)
			if err := cmd.Start(); err == nil {
				cmd.Process.Release()
			}
			os.Exit(0)
		})

	var ln net.Listener
	var err error
	port := o.Port
	for tries := 10; ; tries-- {
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			break
		}
		if isAddrInUse(err) && tries > 0 {
			port++
			continue
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	u := fmt.Sprintf("http://localhost:%d", port)
	what := "demo team"
	if !o.Demo {
		n := len(src.Sessions())
		plural := "s"
		if n == 1 {
			plural = ""
		}
		what = fmt.Sprintf("%d session%s in the last %gh", n, plural, o.Hours)
	}
	fmt.Printf("whatsmyagentdoing v%s → %s  (%s, Ctrl+C to stop)\n", version, u, what)
	printUpdateHint(updater)
	if o.Open {
		openBrowser(u + query)
	}

	httpServer = &http.Server{Handler: app}
	if err := httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
