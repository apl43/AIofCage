package vault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNotFound    = errors.New("钥匙串中没有这个条目")
	servicePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

// Keychain stores generic passwords in the user's login keychain through /usr/bin/security.
// Values are passed on stdin (interactive mode), never as command-line arguments, so other
// processes cannot read them from the process list.
type Keychain struct {
	Account string
	binary  string
}

func NewKeychain() *Keychain {
	account := os.Getenv("USER")
	if account == "" {
		account = "aiofcage"
	}
	return &Keychain{Account: account, binary: "/usr/bin/security"}
}

func (k *Keychain) Read(ctx context.Context, service string) ([]byte, error) {
	if !servicePattern.MatchString(service) {
		return nil, errors.New("钥匙串条目名称格式无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, k.binary, "find-generic-password", "-a", k.Account, "-s", service, "-w")
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 44 {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("读取钥匙串失败（%s）", service)
	}
	value := bytes.TrimRight(stdout.Bytes(), "\n")
	if len(value) == 0 || bytes.ContainsAny(value, "\r\n") {
		return nil, fmt.Errorf("钥匙串条目 %s 内容无效", service)
	}
	return value, nil
}

// Put creates or replaces an item. The value may not contain whitespace or quotes, which API keys
// never do; this keeps the interactive command unambiguous.
func (k *Keychain) Put(ctx context.Context, service string, value []byte) error {
	if !servicePattern.MatchString(service) {
		return errors.New("钥匙串条目名称格式无效")
	}
	if len(value) == 0 || len(value) > 4096 || bytes.ContainsAny(value, " \t\r\n\"'\\") {
		return errors.New("密钥不能为空，也不能包含空格、引号或换行")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	command := fmt.Sprintf("add-generic-password -U -a %s -s %s -l %s -w %s\n", k.Account, service, service, value)
	cmd := exec.CommandContext(ctx, k.binary, "-i")
	cmd.Stdin = strings.NewReader(command)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil || stderr.Len() > 0 {
		return fmt.Errorf("写入钥匙串失败（%s）", service)
	}
	// Interactive mode does not report failures through the exit code; confirm by reading back.
	stored, err := k.Read(ctx, service)
	if err != nil || !bytes.Equal(stored, value) {
		return fmt.Errorf("写入钥匙串后校验失败（%s）", service)
	}
	return nil
}

func (k *Keychain) Delete(ctx context.Context, service string) error {
	if !servicePattern.MatchString(service) {
		return errors.New("钥匙串条目名称格式无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, k.binary, "delete-generic-password", "-a", k.Account, "-s", service).Run()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 44) {
		return fmt.Errorf("删除钥匙串条目失败（%s）", service)
	}
	return nil
}
