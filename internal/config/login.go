package config

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

var ErrCurrentPassword = errors.New("current password is incorrect")

type AdminCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func ValidateAdminCredentials(c AdminCredentials) error {
	if utf8.RuneCountInString(c.Username) < 1 || utf8.RuneCountInString(c.Username) > 64 || !utf8.ValidString(c.Username) || strings.TrimSpace(c.Username) != c.Username || strings.Contains(c.Username, ":") {
		return errors.New("username must be 1–64 characters, without colons or surrounding spaces")
	}
	for _, r := range c.Username {
		if unicode.IsControl(r) {
			return errors.New("username contains an invalid character")
		}
	}
	if len(c.Password) < 8 || len(c.Password) > 1024 || !utf8.ValidString(c.Password) {
		return errors.New("password must be 8–1,024 bytes")
	}
	for _, r := range c.Password {
		if unicode.IsControl(r) {
			return errors.New("password contains an invalid character")
		}
	}
	return nil
}

// Custom credentials are stored atomically. The bootstrap password supplies
// the initial login until the user saves their own credentials.
func ReadAdminCredentials(root string) (AdminCredentials, error) {
	b, e := os.ReadFile(filepath.Join(root, "secrets/admin-login.json"))
	if os.IsNotExist(e) {
		pw, e := AdminPassword(root)
		return AdminCredentials{Username: "admin", Password: pw}, e
	}
	if e != nil {
		return AdminCredentials{}, e
	}
	var c AdminCredentials
	if json.Unmarshal(b, &c) != nil || ValidateAdminCredentials(c) != nil {
		return c, errors.New("admin login file is invalid")
	}
	return c, nil
}

type AdminLogin struct {
	mu          sync.RWMutex
	root        string
	credentials AdminCredentials
}

func OpenAdminLogin(root string) (*AdminLogin, error) {
	c, e := ReadAdminCredentials(root)
	if e != nil {
		return nil, e
	}
	return &AdminLogin{root: root, credentials: c}, nil
}
func (l *AdminLogin) Current() AdminCredentials {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.credentials
}
func (l *AdminLogin) Change(currentPassword, username, password string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	// These ephemeral digests equalise constant-time comparison lengths; they
	// are not persisted password hashes or a password-storage KDF.
	got := sha256.Sum256([]byte(currentPassword))
	want := sha256.Sum256([]byte(l.credentials.Password))
	if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
		return ErrCurrentPassword
	}
	next := AdminCredentials{Username: username, Password: password}
	if password == "" {
		next.Password = l.credentials.Password
	}
	if e := ValidateAdminCredentials(next); e != nil {
		return e
	}
	raw, e := json.Marshal(next)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Join(l.root, "secrets"), "login-*.tmp")
	if e != nil {
		return errors.New("could not save login")
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return errors.New("could not save login")
	}
	if e = os.Rename(name, filepath.Join(l.root, "secrets/admin-login.json")); e != nil {
		return errors.New("could not save login")
	}
	l.credentials = next
	return nil
}
