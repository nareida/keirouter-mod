package tray

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"runtime"
	"sync"

	"github.com/gogpu/systray"
)

//go:embed icon.png
var DefaultIcon []byte

// Options configures the system tray.
type Options struct {
	Version      string
	DashboardURL string
	Port         int
	OnOpenURL    func(url string)
	OnQuit       func()
	Logger       *slog.Logger
}

// Tray manages the KeiRouter system tray lifecycle.
type Tray struct {
	opts    Options
	tray    *systray.SystemTray
	mu      sync.Mutex
	stopped bool
}

// New creates a new system tray instance with the given options.
func New(opts Options) *Tray {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Tray{
		opts: opts,
	}
}

// Run initializes and displays the system tray icon, builds the context menu,
// and blocks running the platform event loop. Callers should call this on the
// main OS thread (after runtime.LockOSThread()).
func (t *Tray) Run(ctx context.Context) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	st := systray.New()

	// Configure Icon
	if len(DefaultIcon) > 0 {
		st.SetIcon(DefaultIcon)
	}

	tooltip := fmt.Sprintf("KeiRouter v%s (%s)", t.opts.Version, t.opts.DashboardURL)
	st.SetTooltip(tooltip)

	// Build Context Menu
	menu := systray.NewMenu()

	// Title / Header item (disabled)
	titleItem := menu.Add(fmt.Sprintf("KeiRouter v%s", t.opts.Version), nil)
	if titleItem != nil {
		titleItem.SetDisabled(true)
	}

	statusItem := menu.Add(fmt.Sprintf("Status: Running (Port %d)", t.opts.Port), nil)
	if statusItem != nil {
		statusItem.SetDisabled(true)
	}

	menu.AddSeparator()

	// Open Dashboard
	menu.Add("Open Dashboard", func() {
		if t.opts.OnOpenURL != nil {
			t.opts.OnOpenURL(t.opts.DashboardURL)
		}
	})

	// Documentation
	menu.Add("Documentation", func() {
		if t.opts.OnOpenURL != nil {
			t.opts.OnOpenURL("https://github.com/mydisha/keirouter")
		}
	})

	menu.AddSeparator()

	// Quit
	menu.Add("Quit KeiRouter", func() {
		t.Stop()
		if t.opts.OnQuit != nil {
			t.opts.OnQuit()
		}
	})

	st.SetMenu(menu)

	// Left click on tray icon opens the dashboard
	st.OnClick(func() {
		if t.opts.OnOpenURL != nil {
			t.opts.OnOpenURL(t.opts.DashboardURL)
		}
	})

	st.Show()

	// Best-effort startup notification
	st.ShowNotification("KeiRouter", fmt.Sprintf("Running in background on %s", t.opts.DashboardURL))

	// Publish the native tray only after setup is complete. Stop may have won
	// the race while setup was in progress; in that case tear this instance down
	// immediately instead of entering an event loop that can no longer be stopped.
	if !t.attach(st) {
		st.Remove()
		return nil
	}

	// Listen for ctx cancellation in background to tear down tray loop
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			t.Stop()
		case <-done:
		}
	}()

	t.opts.Logger.Info("system tray initialized", "url", t.opts.DashboardURL)

	// Run message loop (blocks until t.Stop() / st.Remove())
	if err := st.Run(); err != nil {
		t.opts.Logger.Warn("tray event loop returned error", "err", err)
		return err
	}
	return nil
}

// attach publishes a fully initialized native tray unless Stop already won the
// startup race. It is kept separate so the state transition can be unit tested
// without starting a platform event loop.
func (t *Tray) attach(st *systray.SystemTray) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return false
	}
	t.tray = st
	return true
}

// Stop removes the tray icon and exits the event loop safely.
func (t *Tray) Stop() {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	t.stopped = true
	st := t.tray
	t.mu.Unlock()

	// Do not hold the state mutex while calling platform code. Remove is
	// idempotently guarded above and may dispatch work to the UI thread.
	if st != nil {
		st.Remove()
	}
}
