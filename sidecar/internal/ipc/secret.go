package ipc

import "easy-command/sidecar/internal/script"

func routeSecret(l *script.Library, req request) (any, error) {
	invalid := &script.Error{Code: "INVALID_PARAM", Message: "密码设置请求无效"}
	switch req.Action {
	case "secret.set":
		var p struct {
			ScriptID  string  `json:"scriptId"`
			ParamKey  string  `json:"paramKey"`
			Plaintext *string `json:"plaintext"`
		}
		if decode(req.Payload, &p) != nil || p.Plaintext == nil {
			return nil, invalid
		}
		plain := []byte(*p.Plaintext)
		defer clear(plain)
		updated, err := l.SetSecret(p.ScriptID, p.ParamKey, plain)
		return map[string]string{"updatedAt": updated}, err
	case "secret.clear":
		var p struct {
			ScriptID string `json:"scriptId"`
			ParamKey string `json:"paramKey"`
		}
		if decode(req.Payload, &p) != nil {
			return nil, invalid
		}
		return struct{}{}, l.ClearSecret(p.ScriptID, p.ParamKey)
	}
	return nil, invalid
}
