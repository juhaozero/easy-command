package script

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"time"
	"unicode/utf8"
)

type upload struct {
	id, revision string
	data         []byte
	expires      time.Time
}

func (l *Library) content(id string) (Meta, []byte, string, error) {
	m, err := l.meta(id)
	if err != nil {
		return m, nil, "", err
	}
	data, err := l.db.Read(filepath.Join(l.dir(id), m.FileName), MaxContent)
	if err != nil {
		return m, nil, "", err
	}
	if len(data) > MaxContent {
		return m, nil, "", fail("SIZE_LIMIT", "脚本不能超过 2 MiB")
	}
	digest := sha256.New()
	digest.Write([]byte(m.UpdatedAt))
	digest.Write(data)
	return m, data, fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func (l *Library) Get(id string, offset int, revision string) (Meta, []byte, *int, string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	m, data, current, err := l.content(id)
	if err != nil {
		return m, nil, nil, "", err
	}
	if (revision != "" && revision != current) || (offset != 0 && revision == "") {
		return m, nil, nil, "", fail("INVALID_PARAM", "脚本已更新，请重新读取")
	}
	if offset < 0 || offset > len(data) {
		return m, nil, nil, "", fail("INVALID_PARAM", "读取偏移无效")
	}
	end := min(offset+ChunkSize, len(data))
	var next *int
	if end < len(data) {
		next = &end
	}
	return m, data[offset:end], next, current, nil
}

func (l *Library) UpdateContent(id string, part []byte, offset int, revision, uploadID string, final bool) (string, int, *Meta, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for key, u := range l.uploads {
		if now.After(u.expires) {
			delete(l.uploads, key)
		}
	}
	if len(part) > ChunkSize || offset > MaxContent || (offset >= 0 && len(part) > MaxContent-offset) {
		return "", 0, nil, fail("SIZE_LIMIT", "分块或脚本超过大小上限")
	}
	if offset < 0 || (!final && len(part) == 0) {
		return "", 0, nil, fail("INVALID_PARAM", "上传偏移或分块无效")
	}
	m, old, current, err := l.content(id)
	if err != nil {
		return "", 0, nil, err
	}
	if revision != current || !utf8.Valid(old) {
		return "", 0, nil, fail("INVALID_PARAM", "脚本已更新或使用了非 UTF-8 编码")
	}
	u := l.uploads[id]
	if uploadID == "" && offset == 0 {
		if len(l.uploads) >= 8 && u == nil {
			return "", 0, nil, fail("INVALID_PARAM", "并行上传过多，请稍后重试")
		}
		token, err := NewID()
		if err != nil {
			return "", 0, nil, err
		}
		u = &upload{id: token, revision: revision}
	} else if u == nil || u.id != uploadID || u.revision != revision || len(u.data) != offset {
		return "", 0, nil, fail("INVALID_PARAM", "上传会话或偏移无效，请重新保存")
	}
	u.data = append(u.data, part...)
	u.expires = now.Add(2 * time.Minute)
	if !final {
		l.uploads[id] = u
		return u.id, len(u.data), nil, nil
	}
	delete(l.uploads, id)
	if !utf8.Valid(u.data) {
		return "", 0, nil, fail("INVALID_PARAM", "脚本正文必须是 UTF-8")
	}
	m.UpdatedAt = timestamp()
	if err := l.publish(m, u.data, true); err != nil {
		return "", 0, nil, err
	}
	return u.id, len(u.data), &m, l.rebuild()
}
