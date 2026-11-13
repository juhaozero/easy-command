package exec

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"easy-command/sidecar/internal/script"
	"easy-command/sidecar/internal/secret"
)

func arguments(params []script.Param, values map[string]json.RawMessage, password func(script.Param) ([]byte, error)) ([]string, []string, error) {
	var args, env []string
	seen := make(map[string]bool)
	for _, p := range params {
		seen[p.Key] = true
		if p.Type == "password" {
			if _, provided := values[p.Key]; provided {
				return nil, nil, problem("INVALID_PARAM", "请通过密码设置保存此参数")
			}
			plain, err := password(p)
			if errors.Is(err, secret.ErrNotFound) {
				return nil, nil, problem("SECRET_NOT_FOUND", secret.ErrNotFound.Error())
			}
			if err != nil {
				return nil, nil, problem("SECRET_DECRYPT_FAILED", secret.ErrDecrypt.Error())
			}
			value := string(plain)
			clear(plain)
			switch p.CLI.Mode {
			case "arg":
				args = append(args, value)
			case "flag":
				args = append(args, p.CLI.Flag, value)
			case "env":
				if p.CLI.EnvName == "" || strings.ContainsAny(p.CLI.EnvName, "=\x00") {
					return nil, nil, problem("INVALID_PARAM", "环境变量名无效")
				}
				env = append(env, p.CLI.EnvName+"="+value)
			default:
				return nil, nil, problem("INVALID_PARAM", "参数模式无效")
			}
			continue
		}
		raw, provided := values[p.Key]
		if !provided {
			raw = p.Default
		}
		if len(raw) == 0 || string(raw) == "null" {
			if p.Required {
				return nil, nil, problem("INVALID_PARAM", "缺少参数 "+p.Label)
			}
			continue
		}
		var value any
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return nil, nil, problem("INVALID_PARAM", "参数值无效")
		}
		var text string
		switch typed := value.(type) {
		case string:
			if p.Type != "string" && p.Type != "file" && p.Type != "dir" {
				return nil, nil, problem("INVALID_PARAM", "参数类型错误："+p.Label)
			}
			text = typed
		case json.Number:
			if p.Type != "number" {
				return nil, nil, problem("INVALID_PARAM", "参数类型错误："+p.Label)
			}
			text = typed.String()
		case bool:
			if p.Type != "boolean" {
				return nil, nil, problem("INVALID_PARAM", "参数类型错误："+p.Label)
			}
			text = fmt.Sprint(typed)
		default:
			return nil, nil, problem("INVALID_PARAM", "参数类型错误："+p.Label)
		}
		if strings.ContainsRune(text, 0) {
			return nil, nil, problem("INVALID_PARAM", "参数不能含 NUL")
		}
		if text == "" {
			if p.Required {
				return nil, nil, problem("INVALID_PARAM", "缺少参数 "+p.Label)
			}
			continue
		}
		switch p.CLI.Mode {
		case "arg":
			args = append(args, text)
		case "flag":
			if p.Type != "boolean" {
				return nil, nil, problem("INVALID_PARAM", "flag 参数须为布尔值")
			}
			if value == true {
				args = append(args, p.CLI.Flag)
			}
		case "env":
			name := p.CLI.EnvName
			if name == "" || strings.ContainsAny(name, "=\x00") {
				return nil, nil, problem("INVALID_PARAM", "环境变量名无效")
			}
			env = append(env, name+"="+text)
		default:
			return nil, nil, problem("INVALID_PARAM", "参数模式无效")
		}
	}
	for key := range values {
		if !seen[key] {
			return nil, nil, problem("INVALID_PARAM", "未知参数键")
		}
	}
	return args, env, nil
}
