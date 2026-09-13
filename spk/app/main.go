// Explicit legacy adoption: this caller retains embedded/provider APIs.
// Command jobui is the window a person opens from DSM: what this NAS is
// fetching, what it finished, and the two settings a NAS needs — where files
// land and how long they stay.
//
// It is a caller of the layer like any other. It holds no job state of its own,
// and the only thing it owns is settings.json beside it.
package main

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	download "github.com/openabstractions/abstraction-download/go"
	abstraction "github.com/openabstractions/abstraction-facade/go/legacy"
	job "github.com/openabstractions/abstraction-job/go"
)

//go:embed page.html
var page []byte

//go:embed locked.html
var locked []byte

const cookieName = "abstraction_ui"

func main() {
	addr := flag.String("addr", "127.0.0.1:8734", "address to listen on")
	etc := flag.String("etc", "/var/packages/AbstractionJobd/etc", "where this package keeps its key and settings")
	sweep := flag.Duration("sweep", time.Hour, "how often retention looks at the clock")
	flag.Parse()

	m, err := abstraction.Discover()
	if err != nil {
		log.Fatal(err)
	}
	a := &app{
		machine:   m,
		downloads: m.Download(),
		jobs:      m.Jobs(),
		reach:     download.DefaultRefusals(),
		settings:  settingsAt(filepath.Join(*etc, "settings.json")),
	}
	if sc, ok := a.jobs.(job.Scratch); ok {
		a.root = sc.Root()
	}
	if a.key, err = keyAt(filepath.Join(*etc, "ui.key")); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", a.guard(a.serveHTML))
	mux.HandleFunc("/state", a.guard(a.serveState))
	mux.HandleFunc("/events", a.guard(a.serveEvents))
	mux.HandleFunc("/act", a.guard(a.act))
	mux.HandleFunc("/settings", a.guard(a.saveSettings))

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	go a.keepSwept(*sweep)
	fmt.Println("jobui:", ln.Addr(), "store", a.root)
	log.Fatal(http.Serve(ln, mux))
}

type app struct {
	machine   abstraction.Machine
	downloads download.Client
	jobs      job.Store
	reach     download.Refusals
	settings  *settings
	root      string
	key       string
	swept     sweptCount
}

// guard is the whole authentication story and it is deliberately small. The
// launcher DSM draws carries the key, so clicking the icon — which needed a DSM
// login — is what admits you; everyone else on the network gets the same page
// asking for it. SameSite keeps another site's page from spending the cookie.
func (a *app) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		if k := r.URL.Query().Get("k"); k != "" && a.admits(k) {
			http.SetCookie(rw, &http.Cookie{
				Name: cookieName, Value: a.key, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 30 * 24 * 3600,
			})
			http.Redirect(rw, r, r.URL.Path, http.StatusSeeOther)
			return
		}
		if c, err := r.Cookie(cookieName); err == nil && a.admits(c.Value) {
			h(rw, r)
			return
		}
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		rw.WriteHeader(http.StatusUnauthorized)
		rw.Write(locked)
	}
}

func (a *app) admits(offered string) bool {
	return subtle.ConstantTimeCompare([]byte(offered), []byte(a.key)) == 1
}

func (a *app) serveHTML(rw http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(rw, r)
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Write(page)
}

func (a *app) serveState(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(a.snapshot())
}

// serveEvents pushes a snapshot whenever the collection changes, and on a slow
// tick besides: a lease quietly lapsing is news, and nothing writes a record to
// announce it. Server-sent events because the browser is the one transport the
// layer's watch cannot reach directly, and this direction only ever carries
// state outward — a socket both ways would be a second protocol to get right.
func (a *app) serveEvents(rw http.ResponseWriter, r *http.Request) {
	flush, ok := rw.(http.Flusher)
	if !ok {
		http.Error(rw, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("X-Accel-Buffering", "no")

	sub := a.downloads.Jobs()
	defer sub.Close()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		b, err := json.Marshal(a.snapshot())
		if err != nil {
			return
		}
		fmt.Fprintf(rw, "data: %s\n\n", b)
		flush.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-sub.Changes():
		case <-tick.C:
		}
	}
}

func (a *app) act(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "post only", http.StatusMethodNotAllowed)
		return
	}
	r.ParseForm()
	var err error
	switch r.FormValue("do") {
	case "pause":
		err = a.pause(r.FormValue("id"))
	case "resume":
		err = a.resume(r.FormValue("id"))
	case "collect":
		err = a.downloads.TakeDelivery(r.FormValue("id"))
	case "cancel":
		err = a.downloads.Open(r.FormValue("id")).Cancel()
	case "add":
		err = a.add(r.FormValue("source"), r.FormValue("name"))
	case "refuse":
		err = a.reach.Refuse(r.FormValue("host"), r.FormValue("reason"))
	case "allow":
		err = a.reach.Allow(r.FormValue("host"))
	default:
		err = errors.New("unknown action")
	}
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	rw.WriteHeader(http.StatusNoContent)
}

// add is where untrusted text meets the store, so the browser never names a
// path: it offers a source and at most a file name, and the folder is the one
// in settings. A name is reduced to a single element, which is the whole reason
// a sink typed into a page cannot aim at the store's own records.
func (a *app) add(source, name string) error {
	source = strings.TrimSpace(source)
	if source == "" {
		return errors.New("paste a link first")
	}
	if download.HostOf(source) == "" {
		return errors.New("that is not a link this NAS can fetch: it names no host")
	}
	set, err := a.settings.read()
	if err != nil {
		return err
	}
	if name = filename(name); name == "" {
		name = filename(path.Base(strings.SplitN(source, "?", 2)[0]))
	}
	if name == "" {
		return errors.New("that link ends in no file name — type one")
	}
	_, err = a.downloads.Submit(download.Spec{
		Sources: []download.Source{{Scheme: schemeOf(source), Locator: source}},
		Sink:    download.Sink{Final: path.Join(set.Folder, name)},
	})
	return err
}

func filename(s string) string {
	s = strings.TrimSpace(s)
	s = s[strings.LastIndexAny(s, `/\`)+1:]
	if s == "." || s == ".." {
		return ""
	}
	return s
}

func schemeOf(locator string) string {
	if i := strings.Index(locator, "://"); i > 0 {
		return strings.ToLower(locator[:i])
	}
	return ""
}

func (a *app) pause(id string) error {
	p, ok := a.downloads.Open(id).(job.Pausable)
	if !ok {
		return errors.New("this download cannot be paused")
	}
	return p.Pause()
}

// resume is two calls and both are needed. Clearing the intent makes the job
// eligible again; resubmitting is what offers somebody to do it, because a
// record nobody is watching stays still no matter what it says it wants.
func (a *app) resume(id string) error {
	p, ok := a.downloads.Open(id).(job.Pausable)
	if !ok {
		return errors.New("this download cannot be resumed")
	}
	if err := p.Resume(); err != nil {
		return err
	}
	rec, err := a.jobs.Load(id)
	if err != nil {
		return err
	}
	spec, err := download.SpecOf(rec)
	if err != nil {
		return err
	}
	_, _, err = a.downloads.ResumeOrSubmit(spec)
	return err
}

func (a *app) saveSettings(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "post only", http.StatusMethodNotAllowed)
		return
	}
	r.ParseForm()
	set, err := a.settings.write(r.FormValue("folder"), r.FormValue("keepFilesDays"))
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	a.sweepNow(set)
	rw.WriteHeader(http.StatusNoContent)
}

// keyAt is the package's own secret, made once and kept beside its settings.
// 0600 under the package account: reading it is the same privilege as reading
// the store, which is the privilege the window hands out.
func keyAt(p string) (string, error) {
	raw, err := os.ReadFile(p)
	if err == nil && len(strings.TrimSpace(string(raw))) >= 32 {
		return strings.TrimSpace(string(raw)), nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	k := hex.EncodeToString(b)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	return k, os.WriteFile(p, []byte(k+"\n"), 0o600)
}
