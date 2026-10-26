// Tern is a bilingual reading site of one's own. Paste a link on the desktop;
// an agent fetches the page, writes a bilingual template, and Tern lays it out
// as an English/Chinese read. The desktop is native UI (MyGo), articles open in
// a web window, and the phone reads the same pages from this machine's HTTP
// server.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/egoist/mygo"

	"github.com/solidSpoon/tern-go/internal/app"
	"github.com/solidSpoon/tern-go/internal/config"
	"github.com/solidSpoon/tern-go/internal/pi"
	"github.com/solidSpoon/tern-go/internal/publish"
	"github.com/solidSpoon/tern-go/internal/server"
	"github.com/solidSpoon/tern-go/internal/store"
	"github.com/solidSpoon/tern-go/internal/threads"
)

func main() {
	defaultHost := "127.0.0.1"
	if raw := os.Getenv("HOST"); raw != "" {
		defaultHost = raw
	}

	portFlag := flag.Int("port", 0, "port of the reading server (0: the port chosen in Settings, else $PORT, else 8787)")
	host := flag.String("host", defaultHost, "address the reading server listens on")
	headless := flag.Bool("headless", os.Getenv("TERN_HEADLESS") == "1", "run the reading server without the desktop window")
	root := flag.String("root", config.Root(), "data directory (default: the one chosen in Settings)")
	flag.Parse()

	settings, err := config.Load(*root)
	if err != nil {
		log.Fatalf("cannot load settings: %v", err)
	}
	paths := settings.Paths()

	// The port: an explicit flag wins (so a development run can move it), then
	// the one saved in Settings, then $PORT, then 8787. Settings must win over
	// the environment, or changing it in the app would do nothing after the
	// relaunch inherits $PORT.
	port := *portFlag
	if port == 0 {
		port = settings.Get().Port
	}
	if port == 0 {
		if raw := os.Getenv("PORT"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				port = parsed
			}
		}
	}
	if port == 0 {
		port = 8787
	}

	library, err := store.New(paths)
	if err != nil {
		log.Fatalf("cannot open the library: %v", err)
	}

	runtime := pi.NewRuntime(paths)
	agent := pi.NewAgent(settings, runtime)
	importer := &publish.Importer{Store: library, Assets: settings.AssetsDir}
	mgr, err := threads.New(library, agent, settings, importer, port)
	if err != nil {
		log.Fatalf("cannot open the conversations: %v", err)
	}

	// localURL is where this machine reaches the reader, for the embedded page.
	localURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	// baseURL is what the reader should type on the phone: TERN_PUBLIC_URL
	// when a tunnel or Surge Ponte gives it another name.
	baseURL := publicURL(os.Getenv("TERN_PUBLIC_URL"), port)

	// The server starts first: the article windows and the agent's callbacks
	// both need it, and the phone may already be reading.
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", *host, port))
	if err != nil {
		log.Fatalf("cannot listen on %s:%d: %v", *host, port, err)
	}
	srv := server.New(library, settings, agent, runtime, mgr, importer, port)
	if resources, err := mygo.App.Path(mygo.PathResources); err == nil {
		srv.UseDictionary(filepath.Join(resources, "dictionary.db"))
	}
	httpServer := &http.Server{
		Handler:           srv.Handler,
		ReadHeaderTimeout: 15 * time.Second,
	}
	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("the reading server stopped: %v", err)
		}
	}()

	fmt.Printf("\n  Tern · 双语阅读\n")
	fmt.Printf("  → %s\n", baseURL)
	fmt.Printf("  data: %s\n\n", paths.Root)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		mgr.CloseAll()
		_ = httpServer.Close()
		os.Exit(0)
	}()

	if *headless {
		select {}
	}

	// A changed port cannot be applied in place (the server, the agent's
	// callbacks and the open page all belong to it), so saving it restarts the
	// app: close the listener first, then start the same executable again.
	restart := func() error {
		if mgr.BusyAny() {
			return fmt.Errorf("a conversation is still running")
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		mgr.CloseAll()
		_ = httpServer.Close()
		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Dir, _ = os.Getwd()
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("cannot start the app again: %w", err)
		}
		_ = cmd.Process.Release()
		mygo.App.Quit()
		return nil
	}

	desktop := app.New(settings, library, agent, runtime, mgr, baseURL, localURL, port, restart)
	if err := desktop.Run(); err != nil {
		log.Fatalf("the window stopped: %v", err)
	}
	mgr.CloseAll()
}

// ponteName is what this Mac answers to in Surge Ponte: the phone reaches the
// reader at <name>.sgponte:<port>. TERN_PONTE gives another machine its own
// name; TERN_PUBLIC_URL replaces the whole address for a tunnel or another host.
const ponteName = "STUDIO"

// publicURL is the address shown for the phone. It follows the port the server
// actually listens on, so changing the port in Settings changes the address
// too, and the development port gets its own address for free.
func publicURL(configured string, port int) string {
	if configured != "" {
		if parsed, err := url.Parse(configured); err == nil && parsed.Hostname() != "" {
			parsed.Host = net.JoinHostPort(parsed.Hostname(), strconv.Itoa(port))
			return parsed.String()
		}
	}
	name := os.Getenv("TERN_PONTE")
	if name == "" {
		name = ponteName
	}
	return fmt.Sprintf("http://%s.sgponte:%d", name, port)
}
