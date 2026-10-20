package control

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

type SetupAnswers struct {
	Database DatabaseConfig
	Redis    RedisConfig
	Email    string
	Password string
	Confirm  bool
}

func ValidateSetupAccount(email, password string) error {
	if !validEmail(email) || len(password) < 12 || len(password) > 72 {
		return errors.New("strong password and valid email required")
	}
	return nil
}

func TestDatabase(cfg DatabaseConfig) error {
	if cfg.Port < 1 || cfg.Port > 65535 {
		return errors.New("invalid port")
	}
	db, _, err := openDatabase(cfg)
	if err != nil {
		return errors.New("database connection failed")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		return errors.New("database connection failed")
	}
	return nil
}

func TestRedis(cfg RedisConfig) error {
	redis := redisClient(cfg)
	defer redis.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if redis.Ping(ctx).Err() != nil {
		return errors.New("redis connection failed")
	}
	return nil
}

func ApplySetup(path string, answers SetupAnswers) error {
	if err := ValidateSetupAccount(answers.Email, answers.Password); err != nil {
		return err
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}
	if cfg.Installed {
		return errors.New("already installed")
	}
	if err = TestDatabase(answers.Database); err != nil {
		return err
	}
	if err = TestRedis(answers.Redis); err != nil {
		return err
	}
	if !answers.Confirm {
		return errors.New("installation cancelled")
	}
	cfg.Database = answers.Database
	cfg.Redis = answers.Redis
	if cfg.EncryptionKey == "" {
		cfg.EncryptionKey = randomToken(32)
	}
	server := &Server{Config: cfg, Path: path}
	if err = server.connect(cfg); err != nil {
		return err
	}
	defer server.Close()
	installCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return server.finishInstall(installCtx, answers.Email, answers.Password)
}

func RunSetupCLI(path string, in io.Reader, password func(string) (string, error)) error {
	cfg, err := LoadConfig(path)
	if err == nil && cfg.Installed {
		return errors.New("already installed")
	}
	reader := bufio.NewReader(in)
	var answers SetupAnswers
	fmt.Fprintln(os.Stderr, "MiGate setup")
	answers.Database.Host = promptLine(reader, "PostgreSQL Host", "127.0.0.1")
	answers.Database.Port = promptPort(reader, "PostgreSQL Port", 5432)
	answers.Database.User = promptLine(reader, "PostgreSQL User", "migate")
	secret, err := password("PostgreSQL Password")
	if err != nil {
		return err
	}
	answers.Database.Password = secret
	answers.Database.Database = promptLine(reader, "Database Name", "gate")
	answers.Database.SSLMode = promptLine(reader, "SSL Mode", "disable")
	fmt.Fprint(os.Stderr, "Testing database connection... ")
	if err = TestDatabase(answers.Database); err != nil {
		fmt.Fprintln(os.Stderr, "FAILED")
		return err
	}
	fmt.Fprintln(os.Stderr, "OK")
	answers.Redis.Address = promptLine(reader, "Redis Address", "127.0.0.1:6379")
	secret, err = password("Redis Password")
	if err != nil {
		return err
	}
	answers.Redis.Password = secret
	answers.Redis.TLS = promptYes(reader, "Enable Redis TLS?")
	fmt.Fprint(os.Stderr, "Testing Redis connection... ")
	if err = TestRedis(answers.Redis); err != nil {
		fmt.Fprintln(os.Stderr, "FAILED")
		return err
	}
	fmt.Fprintln(os.Stderr, "OK")
	for {
		answers.Email = promptLine(reader, "Admin Email", "")
		secret, err = password("Admin Password")
		if err != nil {
			return err
		}
		answers.Password = secret
		if err = ValidateSetupAccount(answers.Email, answers.Password); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			continue
		}
		confirm, err := password("Confirm Password")
		if err != nil {
			return err
		}
		if confirm != answers.Password {
			fmt.Fprintln(os.Stderr, "passwords do not match")
			continue
		}
		break
	}
	fmt.Fprintf(os.Stderr, "Database: %s@%s:%d/%s\nRedis: %s\nAdmin: %s\n", answers.Database.User, answers.Database.Host, answers.Database.Port, answers.Database.Database, answers.Redis.Address, answers.Email)
	if !promptYes(reader, "Proceed with installation?") {
		return errors.New("installation cancelled")
	}
	answers.Confirm = true
	return ApplySetup(path, answers)
}

func promptLine(reader *bufio.Reader, label, fallback string) string {
	if fallback != "" {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", label, fallback)
	} else {
		fmt.Fprintf(os.Stderr, "%s: ", label)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.TrimRight(line, "\r"))
	if line == "" {
		return fallback
	}
	return line
}

func promptPort(reader *bufio.Reader, label string, fallback int) int {
	for {
		text := promptLine(reader, label, strconv.Itoa(fallback))
		port, err := strconv.Atoi(text)
		if err == nil && port >= 1 && port <= 65535 {
			return port
		}
		fmt.Fprintln(os.Stderr, "invalid port")
	}
}

func promptYes(reader *bufio.Reader, label string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", label)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	return line == "y" || line == "Y"
}

func ReadSecret(prompt string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", prompt)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		value, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		return string(value), err
	}
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	return strings.TrimSpace(line), err
}
