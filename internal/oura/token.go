package oura

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// refreshMargin is how close to expiry a token may get before it is refreshed.
//
// Oura access tokens last 30 days. Refreshing a day early costs nothing and
// means a run never starts with a token that dies halfway through.
const refreshMargin = 24 * time.Hour

// Token is an OAuth2 token pair as Oura returns it, plus when it was obtained.
//
// Oura reports lifetime as a relative expires_in, which is meaningless once
// written to disk unless we also remember when it was issued — hence
// ObtainedAt, which is ours, not Oura's.
type Token struct {
	TokenType    string `json:"token_type"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds, relative to ObtainedAt
	Scope        string `json:"scope"`
	ObtainedAt   int64  `json:"obtained_at,omitempty"` // unix seconds
}

// Expiry is when the access token stops working, or the zero time if Oura did
// not say.
func (t *Token) Expiry() time.Time {
	if t.ExpiresIn <= 0 || t.ObtainedAt <= 0 {
		return time.Time{}
	}
	return time.Unix(t.ObtainedAt+t.ExpiresIn, 0)
}

// needsRefresh reports whether the access token is expired or about to be.
// A token with unknown expiry is trusted until the API rejects it.
func (t *Token) needsRefresh(now time.Time) bool {
	exp := t.Expiry()
	return !exp.IsZero() && now.Add(refreshMargin).After(exp)
}

// LoadToken reads a token file written by SaveToken.
//
// A file written by hand (a curl of the token endpoint) has no obtained_at.
// The file's modification time is the best stand-in: it was written moments
// after the token was issued.
func LoadToken(path string) (*Token, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if t.AccessToken == "" {
		return nil, fmt.Errorf("%s has no access_token", path)
	}
	if t.ObtainedAt == 0 {
		if fi, err := os.Stat(path); err == nil {
			t.ObtainedAt = fi.ModTime().Unix()
		}
	}
	return &t, nil
}

// SaveToken writes t to path, readable only by the owner.
//
// The write goes to a temp file in the same directory, which is then renamed
// over the old one. Rename is atomic, so a crash mid-write leaves either the
// old file or the new one, never half of each. That matters more than usual
// here: Oura refresh tokens are single-use, so a torn write after a refresh
// would lose the only valid one and force a fresh browser login.
func SaveToken(path string, t *Token) error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".oura-token-*.tmp")
	if err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	// clean up the temp file on any failure; a no-op after a successful rename
	defer os.Remove(tmp.Name())

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("save token: %w", err)
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("save token: %w", err)
	}
	// flush to disk before the rename makes it the real file
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("save token: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	return nil
}

// ErrNoToken means the token file does not exist yet: run the auth command.
var ErrNoToken = errors.New("no Oura token yet")

// FileTokenSource hands out a valid access token backed by a token file,
// refreshing it — and rewriting the file — when it nears expiry.
type FileTokenSource struct {
	path  string
	oauth *OAuth

	mu  sync.Mutex
	tok *Token
	now func() time.Time // swappable for tests
}

// NewFileTokenSource loads the token at path. It returns ErrNoToken (wrapped)
// if the file does not exist.
func NewFileTokenSource(path string, oauth *OAuth) (*FileTokenSource, error) {
	tok, err := LoadToken(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s not found", ErrNoToken, path)
	}
	if err != nil {
		return nil, err
	}
	return &FileTokenSource{path: path, oauth: oauth, tok: tok, now: time.Now}, nil
}

// Token returns an access token, refreshing first if it is about to expire.
func (s *FileTokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tok.needsRefresh(s.now()) {
		if err := s.refreshLocked(ctx); err != nil {
			return "", err
		}
	}
	return s.tok.AccessToken, nil
}

// Refresh forces a refresh regardless of expiry. The client calls this when
// the API answers 401, which means the token died early (revoked, or the
// expiry guess from file mtime was wrong).
func (s *FileTokenSource) Refresh(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshLocked(ctx)
}

func (s *FileTokenSource) refreshLocked(ctx context.Context) error {
	if s.tok.RefreshToken == "" {
		return errors.New("token expired and has no refresh token: run `wolfe-health auth` again")
	}
	next, err := s.oauth.Refresh(ctx, s.tok.RefreshToken)
	if err != nil {
		return fmt.Errorf("refresh token (if this persists, run `wolfe-health auth` again): %w", err)
	}
	// Oura rotates refresh tokens, so the response should carry a new one.
	// If it ever does not, the old one is all we have — keep it.
	if next.RefreshToken == "" {
		next.RefreshToken = s.tok.RefreshToken
	}
	// Save before using. The old refresh token is already spent, so if the
	// save failed after we'd used the new access token, the next run would
	// hold only a dead refresh token.
	if err := SaveToken(s.path, next); err != nil {
		return err
	}
	s.tok = next
	return nil
}

// StaticToken is a TokenSource that always returns the same token. Useful for
// tests, or for a token obtained some other way.
type StaticToken string

// Token returns the token.
func (t StaticToken) Token(context.Context) (string, error) { return string(t), nil }
