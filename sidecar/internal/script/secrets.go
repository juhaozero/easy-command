package script

import (
	"errors"
	"path/filepath"

	"easy-command/sidecar/internal/secret"
)

type storedMeta struct {
	Meta
	Secrets []secret.Entry `json:"secrets,omitempty"`
}

func (l *Library) writeMeta(dir string, m Meta) error {
	disk := storedMeta{Meta: m, Secrets: m.secrets}
	disk.Params = append([]Param{}, m.Params...)
	for i := range disk.Params {
		disk.Params[i].PasswordStored = nil
		disk.Params[i].UpdatedAt = ""
	}
	return l.db.WriteJSON(filepath.Join(dir, "meta.json"), disk)
}

func binding(p Param) string {
	if p.SecretBinding != "" {
		return p.SecretBinding
	}
	return p.Key
}

func (m Meta) Password(p Param) ([]byte, error) {
	for _, entry := range m.secrets {
		if entry.Key == binding(p) {
			return secret.Unprotect(entry.Ciphertext)
		}
	}
	return nil, secret.ErrNotFound
}

func (m *Meta) applyPasswordStatus() {
	for i := range m.Params {
		p := &m.Params[i]
		p.PasswordStored = nil
		p.UpdatedAt = ""
		if p.Type != "password" {
			continue
		}
		stored := false
		p.PasswordStored = &stored
		for _, entry := range m.secrets {
			if entry.Key == binding(*p) {
				stored = true
				p.UpdatedAt = entry.UpdatedAt
				break
			}
		}
	}
}

func (l *Library) SetSecret(id, key string, plaintext []byte) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	m, bound, err := l.passwordTarget(id, key)
	if err != nil {
		return "", err
	}
	ciphertext, err := secret.Protect(plaintext)
	switch {
	case errors.Is(err, secret.ErrInvalid):
		return "", fail("INVALID_PARAM", err.Error())
	case err != nil:
		return "", fail("DPAPI_PROTECT_FAILED", secret.ErrProtect.Error())
	}
	now := timestamp()
	m.removeSecret(bound)
	m.secrets = append(m.secrets, secret.Entry{Key: bound, Ciphertext: ciphertext, UpdatedAt: now})
	if err := l.writeMeta(l.dir(id), m); err != nil {
		return "", fail("SECRET_WRITE_FAILED", "无法写入脚本密码设置，请重试")
	}
	return now, nil
}

func (l *Library) ClearSecret(id, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	m, bound, err := l.passwordTarget(id, key)
	if err != nil {
		return err
	}
	if !m.removeSecret(bound) {
		return nil
	}
	if err := l.writeMeta(l.dir(id), m); err != nil {
		return fail("SECRET_WRITE_FAILED", "无法写入脚本密码设置，请重试")
	}
	return nil
}

func (l *Library) passwordTarget(id, key string) (Meta, string, error) {
	m, err := l.meta(id)
	if err != nil {
		return m, "", err
	}
	for _, p := range m.Params {
		if p.Key == key && p.Type == "password" {
			return m, binding(p), nil
		}
	}
	return m, "", fail("INVALID_PARAM", "请先保存对应的密码参数定义")
}

func (m *Meta) removeSecret(key string) bool {
	for i, entry := range m.secrets {
		if entry.Key == key {
			m.secrets = append(m.secrets[:i], m.secrets[i+1:]...)
			return true
		}
	}
	return false
}
