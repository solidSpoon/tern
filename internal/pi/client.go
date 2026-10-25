package pi

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Event is one JSONL record Pi writes to stdout.
type Event map[string]any

// ClientOptions says how to start a Pi child process.
type ClientOptions struct {
	Cwd        string
	SessionDir string
	Provider   string
	Model      string
	APIKey     string
	// SessionID pins the session, so a recycled process or a restarted server
	// continues the same conversation.
	SessionID string
	// NoSession runs a one-off question without saving a session.
	NoSession bool
	// Extension is a TypeScript file to load: Tern's own tools.
	Extension string
	// ExtraEnv carries the publish and image URLs to the extension.
	ExtraEnv map[string]string
	Tools    []string
	OnEvent  func(Event)
	OnExit   func(code int, stderr string)
}

type pendingCall struct {
	resolve func(data any)
	reject  func(err error)
}

// Client speaks Pi's RPC mode: commands on stdin, JSONL events on stdout.
type Client struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	opts  ClientOptions

	mu      sync.Mutex
	writeMu sync.Mutex
	buffer  string
	pending map[string]pendingCall
	seq     int
	stderr  string
	closed  bool

	assistantText  string
	assistantError string

	idle        bool
	idleWaiters []chan struct{}
}

// Start launches Pi in RPC mode.
func (r *Runtime) Start(opts ClientOptions) (*Client, error) {
	binary, err := r.Binary()
	if err != nil {
		return nil, err
	}

	args := []string{"--mode", "rpc", "--provider", opts.Provider, "--model", opts.Model}
	keyEnv := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_", "/", "_").Replace(opts.Provider)) + "_API_KEY"

	if opts.NoSession {
		args = append(args, "--no-session")
	} else {
		args = append(args, "--session-dir", opts.SessionDir)
		if opts.SessionID != "" {
			args = append(args, "--session-id", opts.SessionID)
		}
	}
	// Turn off every kind of auto-discovery, so that behavior is reproducible
	// and nothing in ~/.pi leaks into a run.
	args = append(args, "-ne", "--no-mcp", "--no-skills", "--no-prompt-templates", "--no-context-files")
	if opts.Extension != "" {
		args = append(args, "-e", opts.Extension)
	}
	if len(opts.Tools) > 0 {
		args = append(args, "--tools", strings.Join(opts.Tools, ","))
	}

	cmd := exec.Command(binary, args...)
	cmd.Dir = opts.Cwd
	env := append(os.Environ(),
		keyEnv+"="+opts.APIKey,
		"PI_NO_UPDATE_CHECK=1",
		"NO_COLOR=1",
	)
	for key, value := range opts.ExtraEnv {
		env = append(env, key+"="+value)
	}
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cannot start Pi: %w", err)
	}

	client := &Client{
		cmd:     cmd,
		stdin:   stdin,
		opts:    opts,
		pending: map[string]pendingCall{},
		idle:    true,
	}

	go client.readStdout(stdout)
	go client.readStderr(stderr)
	go func() {
		waitErr := cmd.Wait()
		code := 0
		if waitErr != nil {
			code = -1
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				code = exitErr.ExitCode()
			}
		}
		client.mu.Lock()
		client.closed = true
		for _, call := range client.pending {
			call.reject(fmt.Errorf("Pi exited (code=%d) %s", code, tail(client.stderr, 300)))
		}
		client.pending = map[string]pendingCall{}
		waiters := client.idleWaiters
		client.idleWaiters = nil
		stderrTail := client.stderr
		client.mu.Unlock()
		for _, wake := range waiters {
			close(wake)
		}
		if opts.OnExit != nil {
			opts.OnExit(code, stderrTail)
		}
	}()
	return client, nil
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func (c *Client) readStdout(pipe io.ReadCloser) {
	defer pipe.Close()
	reader := bufio.NewReaderSize(pipe, 1<<16)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			c.consume(strings.TrimRight(line, "\r\n"))
		}
		if err != nil {
			return
		}
	}
}

func (c *Client) readStderr(pipe io.ReadCloser) {
	defer pipe.Close()
	buf := make([]byte, 4096)
	for {
		n, err := pipe.Read(buf)
		if n > 0 {
			c.mu.Lock()
			c.stderr = tail(c.stderr+string(buf[:n]), 4000)
			c.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (c *Client) consume(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		return
	}
	kind, _ := record["type"].(string)

	if kind == "response" {
		id, _ := record["id"].(string)
		c.mu.Lock()
		call, ok := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ok {
			if success, _ := record["success"].(bool); !success {
				message := "command failed"
				if text, _ := record["error"].(string); text != "" {
					message = text
				}
				call.reject(errors.New(message))
			} else {
				call.resolve(record["data"])
			}
		}
		return
	}

	switch kind {
	case "agent_start":
		c.mu.Lock()
		c.idle = false
		c.mu.Unlock()
	case "agent_settled":
		c.mu.Lock()
		c.idle = true
		waiters := c.idleWaiters
		c.idleWaiters = nil
		c.mu.Unlock()
		for _, wake := range waiters {
			close(wake)
		}
	case "message_end":
		if message, ok := record["message"].(map[string]any); ok {
			if role, _ := message["role"].(string); role == "assistant" {
				if text := messageText(message); text != "" {
					c.mu.Lock()
					c.assistantText = text
					c.mu.Unlock()
				}
				if stop, _ := message["stopReason"].(string); stop == "error" {
					reason := "model call failed"
					if text, _ := message["errorMessage"].(string); text != "" {
						reason = text
					}
					c.mu.Lock()
					c.assistantError = reason
					c.mu.Unlock()
				}
			}
		}
	}

	if c.opts.OnEvent != nil {
		c.opts.OnEvent(record)
	}
}

func messageText(message map[string]any) string {
	parts, ok := message["content"].([]any)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, part := range parts {
		item, ok := part.(map[string]any)
		if !ok {
			continue
		}
		if kind, _ := item["type"].(string); kind == "text" {
			if text, _ := item["text"].(string); text != "" {
				b.WriteString(text)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func (c *Client) request(kind string, payload map[string]any, timeout time.Duration) (any, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("the Pi process has exited")
	}
	c.seq++
	id := fmt.Sprintf("r%d", c.seq)
	channel := make(chan struct {
		data any
		err  error
	}, 1)
	timer := time.AfterFunc(timeout, func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		select {
		case channel <- struct {
			data any
			err  error
		}{nil, fmt.Errorf("waiting for %s timed out", kind)}:
		default:
		}
	})
	c.pending[id] = pendingCall{
		resolve: func(data any) {
			timer.Stop()
			channel <- struct {
				data any
				err  error
			}{data, nil}
		},
		reject: func(err error) {
			timer.Stop()
			channel <- struct {
				data any
				err  error
			}{nil, err}
		},
	}
	c.mu.Unlock()

	payload["id"] = id
	payload["type"] = kind
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	c.writeMu.Lock()
	_, err = c.stdin.Write(append(raw, '\n'))
	c.writeMu.Unlock()
	if err != nil {
		return nil, err
	}
	result := <-channel
	return result.data, result.err
}

// WaitIdle waits until Pi says it has no more work to do, or times out.
func (c *Client) WaitIdle(timeout time.Duration) error {
	c.mu.Lock()
	if c.idle {
		c.mu.Unlock()
		return nil
	}
	if c.closed {
		c.mu.Unlock()
		return errors.New("the Pi process has exited")
	}
	done := make(chan struct{})
	c.idleWaiters = append(c.idleWaiters, done)
	c.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-done:
		return nil
	case <-timer.C:
		c.mu.Lock()
		for i, waiter := range c.idleWaiters {
			if waiter == done {
				c.idleWaiters = append(c.idleWaiters[:i], c.idleWaiters[i+1:]...)
				break
			}
		}
		c.mu.Unlock()
		return fmt.Errorf("this round took longer than %s", timeout)
	}
}

// Prompt sends a message and starts a turn.
func (c *Client) Prompt(message string) error {
	c.mu.Lock()
	c.assistantText = ""
	c.assistantError = ""
	// Mark busy before the response arrives: it can come before agent_start,
	// which would make WaitIdle return an empty answer.
	c.idle = false
	c.mu.Unlock()
	_, err := c.request("prompt", map[string]any{"message": message}, 60*time.Second)
	return err
}

// Abort stops the current turn.
func (c *Client) Abort() {
	_, _ = c.request("abort", map[string]any{}, 30*time.Second)
}

// Close stops the process.
func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

// Alive reports whether the process still runs.
func (c *Client) Alive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed
}

// LastAssistantText is the last complete assistant message.
func (c *Client) LastAssistantText() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.assistantText
}

// LastAssistantError is why the last message failed, if it did.
func (c *Client) LastAssistantError() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.assistantError
}

// StderrTail is the end of Pi's stderr, for error messages.
func (c *Client) StderrTail() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stderr
}
