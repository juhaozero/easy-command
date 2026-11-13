package script

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"easy-command/sidecar/internal/secret"
)

type CLI struct {
	Mode    string `json:"mode"`
	EnvName string `json:"envName,omitempty"`
	Flag    string `json:"flag,omitempty"`
}

type Param struct {
	Key            string          `json:"key"`
	Label          string          `json:"label"`
	Type           string          `json:"type"`
	Required       bool            `json:"required"`
	Default        json.RawMessage `json:"default,omitempty"`
	PasswordStored *bool           `json:"passwordStored,omitempty"`
	UpdatedAt      string          `json:"updatedAt,omitempty"`
	SecretBinding  string          `json:"secretBinding,omitempty"`
	MaskInLog      bool            `json:"maskInLog"`
	CLI            CLI             `json:"cli"`
}

type Fields struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Enabled     bool     `json:"enabled"`
	Dangerous   bool     `json:"dangerous"`
	WorkingDir  string   `json:"workingDir,omitempty"`
	Params      []Param  `json:"params"`
	Tags        []string `json:"tags,omitempty"`
}

type Meta struct {
	Fields
	ID        string `json:"id"`
	FileName  string `json:"fileName"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	secrets   []secret.Entry
}

type Summary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Dangerous bool   `json:"dangerous"`
	UpdatedAt string `json:"updatedAt"`
}

func timestamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func (f *Fields) validate() error {
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" || len(f.Name) > 255 || strings.ContainsAny(f.Name, "/\\\x00\r\n") {
		return fail("INVALID_PARAM", "脚本名称无效")
	}
	if f.Params == nil {
		f.Params = []Param{}
	}
	keys := make(map[string]bool)
	for i := range f.Params {
		p := &f.Params[i]
		if strings.TrimSpace(p.Key) == "" || strings.TrimSpace(p.Label) == "" || keys[p.Key] {
			return fail("INVALID_PARAM", "参数键和标签不能为空，参数键必须唯一")
		}
		keys[p.Key] = true
		switch p.Type {
		case "string", "number", "boolean", "file", "dir":
			if len(p.Default) > 0 {
				var value any
				if err := json.Unmarshal(p.Default, &value); err != nil {
					return fail("INVALID_PARAM", "参数默认值无效")
				}
				valid := false
				switch value.(type) {
				case string:
					valid = p.Type == "string" || p.Type == "file" || p.Type == "dir"
				case float64:
					valid = p.Type == "number"
				case bool:
					valid = p.Type == "boolean"
				}
				if !valid {
					return fail("INVALID_PARAM", "参数默认值类型不匹配")
				}
			}
		case "password":
			if len(p.Default) > 0 {
				return fail("INVALID_PARAM", "密码参数不能保存默认值")
			}
			p.MaskInLog = true
		default:
			return fail("INVALID_PARAM", "未知参数类型")
		}
		p.PasswordStored = nil
		p.UpdatedAt = ""
		switch p.CLI.Mode {
		case "arg":
		case "env":
			if strings.TrimSpace(p.CLI.EnvName) == "" {
				return fail("INVALID_PARAM", "环境变量名称不能为空")
			}
		case "flag":
			if strings.TrimSpace(p.CLI.Flag) == "" {
				return fail("INVALID_PARAM", "标志名称不能为空")
			}
		default:
			return fail("INVALID_PARAM", "未知参数模式")
		}
	}
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if len(data) > maxMeta {
		return fail("SIZE_LIMIT", fmt.Sprintf("元数据不能超过 %d 字节", maxMeta))
	}
	return nil
}
