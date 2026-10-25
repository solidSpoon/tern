package app

import (
	"testing"
	"time"
)

// Choosing a model applies it, without a second save.
func TestApplyModel(t *testing.T) {
	app := newTestApp(t)
	app.applyModel("deepseek-flash")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if app.settings.Get().Agent.Model == "deepseek-flash" {
			if got := app.agent.Status().Model; got != "deepseek-flash" {
				t.Fatalf("status model = %q", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the model did not change")
}

// Saving a port stores it and restarts; a bad one neither saves nor restarts.
func TestSavePort(t *testing.T) {
	app := newTestApp(t)
	called := make(chan struct{}, 1)
	app.restart = func() error {
		called <- struct{}{}
		return nil
	}

	app.port = 8787
	app.portText = "9123"
	app.savePort()

	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("saving a port did not restart the app")
	}
	if got := app.settings.Get().Port; got != 9123 {
		t.Errorf("port = %d, want 9123", got)
	}

	app.portText = "hello"
	app.savePort()
	select {
	case <-called:
		t.Error("a bad port restarted the app")
	case <-time.After(100 * time.Millisecond):
	}
}
