package cli

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// configure обещает проверить ключ в IAM. Раньше проверка шла через
// файловый кэш токена: был живой токен того же key_id — новый (неверный)
// секрет сохранялся без обращения к IAM, и всё ломалось через час.
func TestConfigure_ChecksTheSecretEvenWithACachedToken(t *testing.T) {
	fc := setupCLI(t)
	cache := auth.FileCache{Path: config.TokenCachePath("default")}
	if err := cache.Store(fakeKeyID, auth.Token{Value: "cached", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	fc.iamStatus = http.StatusUnauthorized
	code, _, stderr := runCLI(t, "wrong-secret\n", "configure", "--key-id", fakeKeyID, "--secret-stdin", "--project-id", manualProject)
	if code != clerrors.ExitAuth {
		t.Fatalf("exit %d, want 2: %s", code, stderr)
	}
	if fc.iamCalls.Load() == 0 {
		t.Error("configure did not ask IAM")
	}
	if _, err := os.Stat(config.CredentialsPath()); !os.IsNotExist(err) {
		t.Error("an unchecked secret was stored")
	}

	// С верным секретом свежий токен ложится в кэш для следующих команд.
	fc.iamStatus = 0
	if code, _, stderr := runCLI(t, fakeSecret+"\n", "configure", "--key-id", fakeKeyID, "--secret-stdin", "--project-id", manualProject); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if tok, ok := cache.Load(auth.CacheKey(auth.Credentials{KeyID: fakeKeyID, Secret: fakeSecret})); !ok || tok.Value == "cached" {
		t.Errorf("token cache not refreshed: %+v %v", tok, ok)
	}
}

// Ctrl-C на вопросе configure прерывает команду (код 130). Раньше чтение
// не смотрело на контекст: первый Ctrl-C не делал ничего, второй убивал
// процесс посреди term.ReadPassword — с выключенным эхом терминала.
func TestConfigure_InterruptAtPrompts(t *testing.T) {
	setupCLI(t)
	block := make(chan struct{})
	defer close(block)
	readPassword = func() ([]byte, error) { <-block; return nil, nil }
	var restored atomic.Bool
	oldSave := saveTerminal
	saveTerminal = func() func() { return func() { restored.Store(true) } }
	t.Cleanup(func() { saveTerminal = oldSave })

	printer := output.NewWithWriter(os.Stderr, output.ModePlain, false)
	pr, pw, _ := os.Pipe()
	defer pw.Close()
	defer pr.Close()

	for _, stage := range []string{"key", "secret"} {
		ctx, cancel := context.WithCancel(context.Background())
		in := pr
		var keyID string
		if stage == "secret" {
			keyID = fakeKeyID // the key ID is given: the secret prompt blocks
		}
		con := newConsole(in, &strings.Builder{}, true)
		done := make(chan error, 1)
		go func() { done <- runConfigure(ctx, configureOptions{keyID: keyID}, con, printer) }()
		time.Sleep(20 * time.Millisecond)
		cancel()
		select {
		case err := <-done:
			if clerrors.ExitCode(err) != clerrors.ExitInterrupted {
				t.Errorf("%s prompt: %v (exit %d), want 130", stage, err, clerrors.ExitCode(err))
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s prompt ignores Ctrl-C", stage)
		}
	}
	if !restored.Load() {
		t.Error("terminal state not restored after Ctrl-C at the secret prompt")
	}

	// --secret-stdin с терминалом вместо канала тоже прерывается.
	ctx, cancel := context.WithCancel(context.Background())
	con := newConsole(pr, &strings.Builder{}, false)
	done := make(chan error, 1)
	go func() {
		done <- runConfigure(ctx, configureOptions{keyID: fakeKeyID, secretStdin: true}, con, printer)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if clerrors.ExitCode(err) != clerrors.ExitInterrupted {
			t.Errorf("--secret-stdin: %v, want 130", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("--secret-stdin ignores Ctrl-C")
	}
}
